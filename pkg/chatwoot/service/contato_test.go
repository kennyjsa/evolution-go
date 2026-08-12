package chatwoot_service

import "testing"

// Cada mensagem virando conversa nova foi o defeito mais visível do primeiro
// teste real: a API pública cria um contact_inbox por chamada, e o Chatwoot
// abre uma conversa por contact_inbox.
func TestEntradaReusaVinculoExistenteDaInbox(t *testing.T) {
	repo, cliente := novoRepo(), &clienteFalso{
		contato:  &Contato{Id: 322, Identifier: "5511999999999", SourceId: ""},
		sourceId: "src-existente",
	}

	if _, err := monta(repo, cliente).Processa(evento(t, infoPadrao(), map[string]any{
		"conversation": "oi",
	})); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if cliente.criouContato {
		t.Errorf("criou contato novo para contato existente")
	}
	if cliente.criouVinculo {
		t.Errorf("criou contact_inbox novo tendo um vínculo existente")
	}
}

// O contato criado pela Evolution Node guarda o JID inteiro no identifier; o
// telefone puro não casa por igualdade, e era isso que jogava toda mensagem no
// caminho de "criar contato".
func TestBuscaCasaIdentifierComSufixoDoWhatsapp(t *testing.T) {
	casos := []struct {
		contato Contato
		alvo    string
		casa    bool
	}{
		{Contato{Identifier: "556581607338@s.whatsapp.net"}, "556581607338", true},
		{Contato{Identifier: "556581607338"}, "556581607338", true},
		{Contato{PhoneNumber: "+556581607338"}, "556581607338", true},
		{Contato{Identifier: "5565816073380"}, "556581607338", false},
		{Contato{Identifier: "556599688810@s.whatsapp.net"}, "556581607338", false},
	}

	for _, caso := range casos {
		if got := ehOMesmoContato(caso.contato, caso.alvo); got != caso.casa {
			t.Errorf("%+v vs %q: esperado %v, veio %v",
				caso.contato, caso.alvo, caso.casa, got)
		}
	}
}

// Contato que existe mas nunca falou por esta inbox precisa de vínculo, e não
// de um contato novo — senão vira duplicata no Chatwoot.
func TestEntradaVinculaContatoSemInbox(t *testing.T) {
	repo, cliente := novoRepo(), &clienteFalso{
		contato:  &Contato{Id: 322, Identifier: "5511999999999"},
		sourceId: "",
	}

	if _, err := monta(repo, cliente).Processa(evento(t, infoPadrao(), map[string]any{
		"conversation": "oi",
	})); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if !cliente.criouVinculo {
		t.Errorf("não vinculou o contato à inbox")
	}
	if cliente.criouContato {
		t.Errorf("criou contato duplicado em vez de vincular")
	}
}

// Contato inexistente é o único caso em que a API pública serve: ela cria o
// contato e o vínculo de uma vez.
func TestEntradaCriaContatoQuandoNaoExiste(t *testing.T) {
	repo, cliente := novoRepo(), &clienteFalso{}

	if _, err := monta(repo, cliente).Processa(evento(t, infoPadrao(), map[string]any{
		"conversation": "oi",
	})); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if !cliente.criouContato {
		t.Errorf("não criou contato inexistente")
	}
}
