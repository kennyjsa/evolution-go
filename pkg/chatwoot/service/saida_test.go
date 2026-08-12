package chatwoot_service

import (
	"errors"
	"testing"

	chatwoot_model "github.com/evolution-foundation/evolution-go/pkg/chatwoot/model"
)

type envio struct {
	instanceId, numero, texto string
}

func saidaDeTeste(repo *repoFalso, waid string, erro error) (*Saida, *[]envio) {
	var enviados []envio
	s := NewSaida(repo, func(instanceId, numero, texto string) (string, error) {
		enviados = append(enviados, envio{instanceId, numero, texto})
		return waid, erro
	})
	return s, &enviados
}

func configAtiva() *chatwoot_model.ChatwootConfig {
	return &chatwoot_model.ChatwootConfig{InstanceId: "i1", Enabled: true}
}

func hookDoAgente(texto string) *WebhookChatwoot {
	h := &WebhookChatwoot{
		Event:       "message_created",
		MessageType: "outgoing",
		Content:     texto,
		Id:          77,
	}
	h.Conversation.Id = 5
	h.Conversation.Meta.Sender.Identifier = "5511999999999"
	return h
}

func TestSaidaEnviaMensagemDoAgente(t *testing.T) {
	repo := novoRepo()
	s, enviados := saidaDeTeste(repo, "WAID1", nil)

	res, err := s.Processa(configAtiva(), hookDoAgente("oi"))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if res.Ignorado {
		t.Fatalf("mensagem do agente foi ignorada: %s", res.Motivo)
	}
	if len(*enviados) != 1 || (*enviados)[0].texto != "oi" ||
		(*enviados)[0].numero != "5511999999999" {
		t.Fatalf("envio errado: %+v", *enviados)
	}
	if len(repo.marcadas) != 1 || repo.marcadas[0].Direcao != "saida" {
		t.Errorf("mensagem não foi marcada como enviada: %+v", repo.marcadas)
	}
}

// `incoming` é o eco da mensagem que a entrada acabou de criar; reenviá-la
// devolveria ao cliente o que ele mesmo mandou.
func TestSaidaIgnoraEcoDaEntrada(t *testing.T) {
	repo := novoRepo()
	s, enviados := saidaDeTeste(repo, "WAID1", nil)

	h := hookDoAgente("oi")
	h.MessageType = "incoming"

	res, err := s.Processa(configAtiva(), h)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if !res.Ignorado {
		t.Errorf("eco da entrada deveria ser ignorado")
	}
	if len(*enviados) != 0 {
		t.Errorf("nada deveria ter sido enviado: %+v", *enviados)
	}
}

// Nota privada é conversa interna do time e não pode vazar para o cliente.
func TestSaidaIgnoraNotaPrivada(t *testing.T) {
	repo := novoRepo()
	s, enviados := saidaDeTeste(repo, "WAID1", nil)

	h := hookDoAgente("combinar desconto")
	h.Private = true

	res, _ := s.Processa(configAtiva(), h)
	if !res.Ignorado {
		t.Errorf("nota privada deveria ser ignorada")
	}
	if len(*enviados) != 0 {
		t.Errorf("nota privada vazou para o WhatsApp: %+v", *enviados)
	}
}

// O Chatwoot reentrega o webhook quando a resposta demora.
func TestSaidaNaoReenviaMensagemJaEnviada(t *testing.T) {
	repo := novoRepo()
	repo.processada = &chatwoot_model.MensagemProcessada{Waid: "WAID1", ChatwootMessageId: 77}
	s, enviados := saidaDeTeste(repo, "WAID2", nil)

	res, _ := s.Processa(configAtiva(), hookDoAgente("oi"))
	if !res.Ignorado {
		t.Errorf("reentrega deveria ser ignorada")
	}
	if len(*enviados) != 0 {
		t.Errorf("mensagem enviada em duplicata: %+v", *enviados)
	}
}

// Falha de envio precisa virar erro para o handler responder 500 e o Chatwoot
// reentregar — e não pode marcar como enviada.
func TestSaidaFalhaDeEnvioNaoMarcaProcessada(t *testing.T) {
	repo := novoRepo()
	s, _ := saidaDeTeste(repo, "", errors.New("instância desconectada"))

	if _, err := s.Processa(configAtiva(), hookDoAgente("oi")); err == nil {
		t.Fatalf("esperado erro no envio")
	}
	if len(repo.marcadas) != 0 {
		t.Errorf("marcou como enviada apesar da falha: %+v", repo.marcadas)
	}
}

func TestSaidaIgnoraInstanciaDesabilitada(t *testing.T) {
	repo := novoRepo()
	s, enviados := saidaDeTeste(repo, "WAID1", nil)

	res, _ := s.Processa(&chatwoot_model.ChatwootConfig{InstanceId: "i1"}, hookDoAgente("oi"))
	if !res.Ignorado {
		t.Errorf("instância desabilitada deveria ser ignorada")
	}
	if len(*enviados) != 0 {
		t.Errorf("enviou com o conector desligado: %+v", *enviados)
	}
}
