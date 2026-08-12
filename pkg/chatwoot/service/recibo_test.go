package chatwoot_service

import (
	"encoding/json"
	"testing"

	chatwoot_model "github.com/evolution-foundation/evolution-go/pkg/chatwoot/model"
)

type statusFalso struct {
	chamadas []string // "conversa/mensagem/status"
	erro     error
}

func (s *statusFalso) AtualizaStatus(conversaId, mensagemId int, status string) error {
	if s.erro != nil {
		return s.erro
	}
	s.chamadas = append(s.chamadas,
		string(rune('0'+conversaId))+"/"+string(rune('0'+mensagemId))+"/"+status)
	return nil
}

func montaRecibo(repo *repoFalso, cliente *statusFalso) *Recibo {
	r := NewRecibo(repo)
	r.fabrica = func(*chatwoot_model.ChatwootConfig) clienteStatus { return cliente }
	return r
}

func eventoRecibo(t *testing.T, state string, ids ...string) []byte {
	t.Helper()
	bruto, err := json.Marshal(map[string]any{
		"event": "Receipt", "state": state, "instanceId": "inst-1",
		"data": map[string]any{"MessageIDs": ids, "Chat": "5511999999999@s.whatsapp.net"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return bruto
}

// O agente precisa ver se o cliente recebeu e leu o que ele mandou.
func TestReciboAtualizaStatusDaMensagemDoAgente(t *testing.T) {
	repo, cliente := novoRepo(), &statusFalso{}
	repo.processada = &chatwoot_model.MensagemProcessada{
		Waid: "W1", ChatwootMessageId: 7, ChatwootConversaId: 3, Direcao: "saida",
	}

	res, err := montaRecibo(repo, cliente).Processa(eventoRecibo(t, "Read", "W1"))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if res.Ignorado {
		t.Fatalf("recibo ignorado: %s", res.Motivo)
	}
	if len(cliente.chamadas) != 1 || cliente.chamadas[0] != "3/7/read" {
		t.Errorf("status errado: %v", cliente.chamadas)
	}
}

func TestReciboEntregueViraDelivered(t *testing.T) {
	repo, cliente := novoRepo(), &statusFalso{}
	repo.processada = &chatwoot_model.MensagemProcessada{
		Waid: "W1", ChatwootMessageId: 7, ChatwootConversaId: 3, Direcao: "saida",
	}

	if _, err := montaRecibo(repo, cliente).Processa(eventoRecibo(t, "Delivered", "W1")); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(cliente.chamadas) != 1 || cliente.chamadas[0] != "3/7/delivered" {
		t.Errorf("status errado: %v", cliente.chamadas)
	}
}

// "ReadSelf" é leitura em outro aparelho do próprio número, não pelo cliente;
// marcar como lida diria ao agente que o cliente leu quando não leu.
func TestReciboReadSelfNaoMarcaComoLida(t *testing.T) {
	repo, cliente := novoRepo(), &statusFalso{}
	repo.processada = &chatwoot_model.MensagemProcessada{
		Waid: "W1", ChatwootMessageId: 7, ChatwootConversaId: 3, Direcao: "saida",
	}

	res, _ := montaRecibo(repo, cliente).Processa(eventoRecibo(t, "ReadSelf", "W1"))
	if !res.Ignorado {
		t.Errorf("ReadSelf deveria ser ignorado")
	}
	if len(cliente.chamadas) != 0 {
		t.Errorf("marcou status indevidamente: %v", cliente.chamadas)
	}
}

// Recibo da mensagem do próprio cliente não descreve nada que o agente veja.
func TestReciboDeMensagemDeEntradaEhIgnorado(t *testing.T) {
	repo, cliente := novoRepo(), &statusFalso{}
	repo.processada = &chatwoot_model.MensagemProcessada{
		Waid: "W1", ChatwootMessageId: 7, ChatwootConversaId: 3, Direcao: "entrada",
	}

	res, _ := montaRecibo(repo, cliente).Processa(eventoRecibo(t, "Read", "W1"))
	if !res.Ignorado {
		t.Errorf("recibo de entrada deveria ser ignorado")
	}
	if len(cliente.chamadas) != 0 {
		t.Errorf("atualizou status de mensagem de entrada: %v", cliente.chamadas)
	}
}

// Recibo de mensagem que nunca passou pelo conector não tem o que atualizar, e
// não pode virar erro (viraria reentrega infinita).
func TestReciboDeMensagemDesconhecidaNaoFalha(t *testing.T) {
	repo, cliente := novoRepo(), &statusFalso{}
	repo.processada = nil

	res, err := montaRecibo(repo, cliente).Processa(eventoRecibo(t, "Read", "W-DESCONHECIDO"))
	if err != nil {
		t.Fatalf("não deveria falhar: %v", err)
	}
	if !res.Ignorado {
		t.Errorf("esperado ignorado, veio %+v", res)
	}
}

func TestReciboIgnoraPayloadIlegivel(t *testing.T) {
	repo, cliente := novoRepo(), &statusFalso{}

	res, err := montaRecibo(repo, cliente).Processa([]byte("{nao json"))
	if err != nil {
		t.Fatalf("payload ilegível não deveria virar erro: %v", err)
	}
	if !res.Ignorado {
		t.Errorf("esperado ignorado")
	}
}
