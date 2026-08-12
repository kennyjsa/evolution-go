package chatwoot_service

import (
	"errors"
	"testing"
)

type presencaEnviada struct{ numero, estado string }

func saidaComPresenca(repo *repoFalso, erro error) (*Saida, *[]presencaEnviada) {
	var presencas []presencaEnviada
	s := NewSaida(repo, func(_, _, _ string) (string, error) { return "WAID1", nil }).
		ComPresenca(func(_, numero, estado string) error {
			if erro != nil {
				return erro
			}
			presencas = append(presencas, presencaEnviada{numero, estado})
			return nil
		})
	return s, &presencas
}

func hookDigitando(evento string) *WebhookChatwoot {
	h := &WebhookChatwoot{Event: evento}
	h.Conversation.Id = 12
	h.Conversation.Meta.Sender.Identifier = "556581607338"
	return h
}

// Sem "digitando" o cliente fica sem sinal enquanto o agente escreve, o que
// numa negociação parece abandono.
func TestPresencaLigaDigitando(t *testing.T) {
	repo := novoRepo()
	s, presencas := saidaComPresenca(repo, nil)

	if _, err := s.Processa(configAtiva(), hookDigitando("conversation_typing_on")); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(*presencas) != 1 || (*presencas)[0].estado != "composing" {
		t.Errorf("presença errada: %+v", *presencas)
	}
	if (*presencas)[0].numero != "556581607338" {
		t.Errorf("número errado: %q", (*presencas)[0].numero)
	}
}

func TestPresencaDesligaAoParar(t *testing.T) {
	repo := novoRepo()
	s, presencas := saidaComPresenca(repo, nil)

	if _, err := s.Processa(configAtiva(), hookDigitando("conversation_typing_off")); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(*presencas) != 1 || (*presencas)[0].estado != "paused" {
		t.Errorf("presença errada: %+v", *presencas)
	}
}

// O cliente não pode ver "digitando" enquanto o time conversa entre si numa
// nota interna.
func TestPresencaIgnoraNotaPrivada(t *testing.T) {
	repo := novoRepo()
	s, presencas := saidaComPresenca(repo, nil)

	h := hookDigitando("conversation_typing_on")
	h.IsPrivate = true

	if _, err := s.Processa(configAtiva(), h); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(*presencas) != 0 {
		t.Errorf("vazou digitação de nota privada: %+v", *presencas)
	}
}

// Presença é efêmera: falhar nela não pode virar 500, senão o Chatwoot
// reentrega um evento que já passou.
func TestPresencaFalhaNaoViraErro(t *testing.T) {
	repo := novoRepo()
	s, _ := saidaComPresenca(repo, errors.New("instância desconectada"))

	res, err := s.Processa(configAtiva(), hookDigitando("conversation_typing_on"))
	if err != nil {
		t.Fatalf("falha de presença não deveria virar erro: %v", err)
	}
	if !res.Ignorado {
		t.Errorf("esperado ignorado, veio %+v", res)
	}
}

// O identifier criado pela Evolution Node vem como JID; mandar presença para
// "556581607338@s.whatsapp.net" com o sufixo quebraria o parse do número.
func TestTelefoneDaConversaLimpaOJid(t *testing.T) {
	h := hookDigitando("conversation_typing_on")
	h.Conversation.Meta.Sender.Identifier = "556581607338@s.whatsapp.net"

	if got := telefoneDaConversa(h); got != "556581607338" {
		t.Errorf("telefone errado: %q", got)
	}
}
