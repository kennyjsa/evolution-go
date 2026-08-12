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
	dados   []byte
	acks    int
	naks    int
	subject string
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
