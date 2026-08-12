package chatwoot_consumer

import (
	"errors"
	"testing"
	"time"

	chatwoot_service "github.com/evolution-foundation/evolution-go/pkg/chatwoot/service"
	config "github.com/evolution-foundation/evolution-go/pkg/config"
	logger_wrapper "github.com/evolution-foundation/evolution-go/pkg/logger"
	"github.com/nats-io/nats.go/jetstream"
)

// msgFake implementa jetstream.Msg só no que o consumidor usa; o resto existe
// para satisfazer a interface e não deve ser chamado.
type msgFake struct {
	jetstream.Msg
	dados    []byte
	acks     int
	naks     int
	subject  string
	entregas uint64
	semMeta  bool
}

func (m *msgFake) Metadata() (*jetstream.MsgMetadata, error) {
	if m.semMeta {
		return nil, errors.New("sem metadados")
	}
	entregas := m.entregas
	if entregas == 0 {
		entregas = 1
	}
	return &jetstream.MsgMetadata{NumDelivered: entregas}, nil
}

func (m *msgFake) Data() []byte    { return m.dados }
func (m *msgFake) Subject() string { return m.subject }
func (m *msgFake) Ack() error      { m.acks++; return nil }
func (m *msgFake) Nak() error      { m.naks++; return nil }

type entradaFake struct {
	resultado chatwoot_service.Resultado
	err       error
	chamadas  int
}

func (e *entradaFake) Processa(*chatwoot_service.Evento) (chatwoot_service.Resultado, error) {
	e.chamadas++
	return e.resultado, e.err
}

func novo(t *testing.T, e processador) *Consumer {
	t.Helper()
	return New(e, logger_wrapper.NewLoggerManager(&config.Config{LogDirectory: t.TempDir()}))
}

// Erro de Chatwoot/banco é transitório: reentregar é a única chance de a
// mensagem chegar na conversa.
func TestFalhaTransitoriaPedeReentrega(t *testing.T) {
	e := &entradaFake{err: errors.New("chatwoot fora")}
	m := &msgFake{dados: []byte(`{"event":"Message","instanceId":"i1"}`)}

	novo(t, e).trata(m)

	if m.naks != 1 || m.acks != 0 {
		t.Errorf("esperado nak, veio acks=%d naks=%d", m.acks, m.naks)
	}
}

// Payload ilegível reentregue é reentregue para sempre e trava o consumidor
// numa mensagem que nunca vai passar.
func TestPayloadInvalidoEhDescartado(t *testing.T) {
	e := &entradaFake{}
	m := &msgFake{dados: []byte("{isso nao e json")}

	novo(t, e).trata(m)

	if m.acks != 1 || m.naks != 0 {
		t.Errorf("esperado ack, veio acks=%d naks=%d", m.acks, m.naks)
	}
	if e.chamadas != 0 {
		t.Errorf("payload inválido não deveria chegar na Entrada")
	}
}

// Ignorado não é falha: grupo, status e waid repetido nunca vão passar numa
// retentativa, então travariam a fila.
func TestEventoIgnoradoRecebeAck(t *testing.T) {
	e := &entradaFake{resultado: chatwoot_service.Resultado{Ignorado: true, Motivo: "grupo"}}
	m := &msgFake{dados: []byte(`{"event":"Message","instanceId":"i1"}`)}

	novo(t, e).trata(m)

	if m.acks != 1 || m.naks != 0 {
		t.Errorf("esperado ack, veio acks=%d naks=%d", m.acks, m.naks)
	}
}

func TestSucessoRecebeAck(t *testing.T) {
	e := &entradaFake{resultado: chatwoot_service.Resultado{ChatwootMessageId: 42}}
	m := &msgFake{dados: []byte(`{"event":"Message","instanceId":"i1"}`)}

	novo(t, e).trata(m)

	if m.acks != 1 || m.naks != 0 {
		t.Errorf("esperado ack, veio acks=%d naks=%d", m.acks, m.naks)
	}
}

// Os subjects assinados precisam bater com os que o producer publica; se
// divergirem o consumidor sobe saudável e não recebe nada.
func TestSubjectsAssinadosBatemComOProducer(t *testing.T) {
	if subjectGlobal != "evolution.message" {
		t.Errorf("subject global divergente do producer: %q", subjectGlobal)
	}
	if subjectInstancia != "evolution.*.message" {
		t.Errorf("subject por instância divergente do producer: %q", subjectInstancia)
	}
	if ackWait < 10*time.Second {
		t.Errorf("ackWait curto demais para uma chamada HTTP ao Chatwoot: %s", ackWait)
	}
}

// filaFalsa registra o que foi para a fila morta.
type filaFalsa struct {
	guardados [][]byte
	erro      error
}

func (f *filaFalsa) publica(dados []byte) error {
	if f.erro != nil {
		return f.erro
	}
	f.guardados = append(f.guardados, dados)
	return nil
}

// consumidorComFila troca a publicação real na DLQ por um registro em memória.
func consumidorComFila(t *testing.T, e processador, fila *filaFalsa) *Consumer {
	c := novo(t, e)
	c.publicaNaFilaMorta = fila.publica
	return c
}

// Sem fila morta, a mensagem que falha 5 vezes some calada — foi assim que uma
// mensagem real se perdeu quando o token do Chatwoot estava errado.
func TestUltimaTentativaVaiParaFilaMorta(t *testing.T) {
	fila := &filaFalsa{}
	e := &entradaFake{err: errors.New("chatwoot fora")}
	m := &msgFake{dados: []byte(`{"event":"Message","instanceId":"i1"}`), entregas: maxDeliver}

	consumidorComFila(t, e, fila).trata(m)

	if len(fila.guardados) != 1 {
		t.Fatalf("evento não foi guardado na fila morta: %+v", fila.guardados)
	}
	if m.acks != 1 || m.naks != 0 {
		t.Errorf("esperado ack apos guardar, veio acks=%d naks=%d", m.acks, m.naks)
	}
}

// Antes da última tentativa, reentregar continua sendo o certo.
func TestTentativaIntermediariaAindaReentrega(t *testing.T) {
	fila := &filaFalsa{}
	e := &entradaFake{err: errors.New("chatwoot fora")}
	m := &msgFake{dados: []byte(`{"event":"Message","instanceId":"i1"}`), entregas: 2}

	consumidorComFila(t, e, fila).trata(m)

	if len(fila.guardados) != 0 {
		t.Errorf("guardou cedo demais: %+v", fila.guardados)
	}
	if m.naks != 1 {
		t.Errorf("esperado nak, veio naks=%d", m.naks)
	}
}

// Ack sem ter guardado é exatamente a perda que a fila morta existe para
// impedir: se a gravação falhar, é melhor reentregar.
func TestFalhaAoGuardarNaoDaAck(t *testing.T) {
	fila := &filaFalsa{erro: errors.New("nats fora")}
	e := &entradaFake{err: errors.New("chatwoot fora")}
	m := &msgFake{dados: []byte(`{"event":"Message","instanceId":"i1"}`), entregas: maxDeliver}

	consumidorComFila(t, e, fila).trata(m)

	if m.acks != 0 {
		t.Errorf("deu ack sem guardar: acks=%d", m.acks)
	}
	if m.naks != 1 {
		t.Errorf("esperado nak para reentrega, veio naks=%d", m.naks)
	}
}

// Sem metadados não dá para saber a tentativa; reentregar é mais seguro que
// mandar para a fila morta cedo demais.
func TestSemMetadadosReentrega(t *testing.T) {
	fila := &filaFalsa{}
	e := &entradaFake{err: errors.New("chatwoot fora")}
	m := &msgFake{dados: []byte(`{"event":"Message"}`), semMeta: true}

	consumidorComFila(t, e, fila).trata(m)

	if len(fila.guardados) != 0 || m.naks != 1 {
		t.Errorf("esperado nak sem guardar: guardados=%d naks=%d", len(fila.guardados), m.naks)
	}
}
