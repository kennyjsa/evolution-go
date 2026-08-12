package chatwoot_service

import (
	"errors"
	"strings"
	"testing"

	chatwoot_model "github.com/evolution-foundation/evolution-go/pkg/chatwoot/model"
)

func hookAssinado(texto, nome, apelido string) *WebhookChatwoot {
	h := hookDoAgente(texto)
	h.Sender.Id = 1
	h.Sender.Name = nome
	h.Sender.Type = "user"
	_ = apelido
	return h
}

// comApelidos liga a consulta de nomes de exibição sem falar com o Chatwoot.
func comApelidos(s *Saida, nomes map[int]string, erro error) *Saida {
	return s.ComNomesDeExibicao(
		func(*chatwoot_model.ChatwootConfig) (map[int]string, error) {
			return nomes, erro
		})
}

func configComAssinatura() *chatwoot_model.ChatwootConfig {
	c := configAtiva()
	c.SignMsg = true
	return c
}

// Numa inbox compartilhada o cliente fala com várias pessoas da agência; sem a
// assinatura todas viram um interlocutor só.
func TestSaidaAssinaComNomeDoAgente(t *testing.T) {
	repo := novoRepo()
	s, enviados := saidaDeTeste(repo, "WAID1", nil)

	if _, err := s.Processa(configComAssinatura(),
		hookAssinado("bom dia", "Emilly Galeno", "")); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(*enviados) != 1 {
		t.Fatalf("nada enviado")
	}
	if (*enviados)[0].texto != "*Emilly Galeno:*\nbom dia" {
		t.Errorf("assinatura errada: %q", (*enviados)[0].texto)
	}
}

// O apelido é o nome que o cliente reconhece quando o time usa nome de guerra.
func TestSaidaPrefereApelidoDoAgente(t *testing.T) {
	repo := novoRepo()
	s, enviados := saidaDeTeste(repo, "WAID1", nil)
	s = comApelidos(s, map[int]string{1: "Emilly"}, nil)

	if _, err := s.Processa(configComAssinatura(),
		hookAssinado("oi", "Emilly Galeno", "Emilly")); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if !strings.HasPrefix((*enviados)[0].texto, "*Emilly:*\n") {
		t.Errorf("apelido ignorado: %q", (*enviados)[0].texto)
	}
}

func TestSaidaRespeitaDelimitadorConfigurado(t *testing.T) {
	repo := novoRepo()
	config := configComAssinatura()
	config.SignDelimiter = ": "
	s, enviados := saidaDeTeste(repo, "WAID1", nil)

	if _, err := s.Processa(config, hookAssinado("oi", "Emilly", "")); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if (*enviados)[0].texto != "*Emilly:*: oi" {
		t.Errorf("delimitador ignorado: %q", (*enviados)[0].texto)
	}
}

// Com signMsg desligado o texto vai cru, como quem desligou espera.
func TestSaidaNaoAssinaQuandoDesligado(t *testing.T) {
	repo := novoRepo()
	s, enviados := saidaDeTeste(repo, "WAID1", nil)

	if _, err := s.Processa(configAtiva(), hookAssinado("oi", "Emilly", "")); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if (*enviados)[0].texto != "oi" {
		t.Errorf("assinou com signMsg desligado: %q", (*enviados)[0].texto)
	}
}

// Webhook sem nome de remetente (automação, bot) não pode virar "**: texto".
func TestSaidaSemNomeNaoAssina(t *testing.T) {
	repo := novoRepo()
	s, enviados := saidaDeTeste(repo, "WAID1", nil)

	if _, err := s.Processa(configComAssinatura(), hookAssinado("oi", "", "")); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if (*enviados)[0].texto != "oi" {
		t.Errorf("assinou sem nome: %q", (*enviados)[0].texto)
	}
}

// A legenda do anexo também é do agente e segue a mesma regra.
func TestSaidaAssinaLegendaDoAnexo(t *testing.T) {
	repo := novoRepo()
	s, midias, _ := saidaComMidia(repo, nil)

	h := hookComAnexo("segue o hotel", "image", "http://cw/foto.jpg")
	h.Sender.Name = "Emilly"

	if _, err := s.Processa(configComAssinatura(), h); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(*midias) != 1 || (*midias)[0].legenda != "*Emilly:*\nsegue o hotel" {
		t.Errorf("legenda sem assinatura: %+v", *midias)
	}
}

// O nome de exibição ("Josieli Sanches") é o que o cliente reconhece; o nome
// completo do cadastro não. O sender da mensagem só traz o completo, então o
// apelido tem que vir do assignee da conversa.
func TestSaidaAssinaComNomeDeExibicao(t *testing.T) {
	repo := novoRepo()
	s, enviados := saidaDeTeste(repo, "WAID1", nil)
	s = comApelidos(s, map[int]string{1: "Josieli Sanches"}, nil)

	h := hookDoAgente("olá")
	h.Sender.Id = 1
	h.Sender.Name = "Josieli Arante Sanches"

	if _, err := s.Processa(configComAssinatura(), h); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if (*enviados)[0].texto != "*Josieli Sanches:*\nolá" {
		t.Errorf("assinatura errada: %q", (*enviados)[0].texto)
	}
}

// A assinatura é de quem escreveu, não de quem é responsável pela conversa.
func TestSaidaAssinaComQuemEscreveu(t *testing.T) {
	repo := novoRepo()
	s, enviados := saidaDeTeste(repo, "WAID1", nil)
	s = comApelidos(s, map[int]string{1: "Josieli Sanches", 2: "Emilly"}, nil)

	h := hookDoAgente("olá")
	h.Sender.Id = 2
	h.Sender.Name = "Emilly Galeno"

	if _, err := s.Processa(configComAssinatura(), h); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if (*enviados)[0].texto != "*Emilly:*\nolá" {
		t.Errorf("assinou com o agente errado: %q", (*enviados)[0].texto)
	}
}

// A API do Chatwoot piscar não pode derrubar a resposta do agente: cai para o
// nome completo do payload, que é pior que o apelido mas melhor que nada.
func TestSaidaCaiParaNomeDoPayloadSeConsultaFalha(t *testing.T) {
	repo := novoRepo()
	s, enviados := saidaDeTeste(repo, "WAID1", nil)
	s = comApelidos(s, nil, errors.New("chatwoot fora"))

	if _, err := s.Processa(configComAssinatura(),
		hookAssinado("olá", "Josieli Arante Sanches", "")); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if (*enviados)[0].texto != "*Josieli Arante Sanches:*\nolá" {
		t.Errorf("fallback errado: %q", (*enviados)[0].texto)
	}
}
