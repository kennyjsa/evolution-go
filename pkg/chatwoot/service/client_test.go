package chatwoot_service

import (
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func servidor(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	// Barra sobrando na URL é o que o usuário digita na tela de configuração;
	// o cliente tem que tolerar sem gerar "//api/v1".
	return NewClient(srv.URL+"/", "7", "token-da-conta", "ident-inbox"), srv
}

func TestBuscaContatoCasaSoOIdentifierExato(t *testing.T) {
	cliente, _ := servidor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/accounts/7/contacts/search" {
			t.Errorf("caminho inesperado: %s", r.URL.Path)
		}
		if got := r.Header.Get("api_access_token"); got != "token-da-conta" {
			t.Errorf("token da conta ausente: %q", got)
		}
		if q := r.URL.Query().Get("q"); q != "5511999998888" {
			t.Errorf("busca errada: %q", q)
		}
		json.NewEncoder(w).Encode(map[string]any{"payload": []map[string]any{
			{"id": 1, "identifier": "5511999998", "source_id": "prefixo"},
			{"id": 2, "identifier": "5511999998888", "source_id": "certo"},
		}})
	})

	contato, err := cliente.BuscaContato("5511999998888")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if contato == nil || contato.SourceId != "certo" {
		t.Fatalf("esperado o contato exato, veio %+v", contato)
	}
}

// A busca do Chatwoot casa por prefixo: aceitar o primeiro resultado colaria a
// mensagem na conversa de outro contato.
func TestBuscaContatoIgnoraCasamentoParcial(t *testing.T) {
	cliente, _ := servidor(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"payload": []map[string]any{
			{"id": 1, "identifier": "5511999998", "source_id": "outro"},
		}})
	})

	contato, err := cliente.BuscaContato("5511999998888")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if contato != nil {
		t.Fatalf("esperado nil, veio %+v", contato)
	}
}

func TestCriaContatoUsaApiPublicaEIdentifierComoNomeReserva(t *testing.T) {
	var corpo map[string]any
	cliente, _ := servidor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/public/api/v1/inboxes/ident-inbox/contacts" {
			t.Errorf("caminho inesperado: %s", r.URL.Path)
		}
		// A API pública autentica pelo inbox_identifier; mandar o token da
		// conta aqui vazaria segredo administrativo à toa.
		if r.Header.Get("api_access_token") != "" {
			t.Error("token da conta não deve ir na API pública")
		}
		json.NewDecoder(r.Body).Decode(&corpo)
		json.NewEncoder(w).Encode(map[string]any{"id": 9, "source_id": "src-9"})
	})

	contato, err := cliente.CriaContato("5511999998888", "")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if contato.SourceId != "src-9" {
		t.Errorf("source_id errado: %q", contato.SourceId)
	}
	if corpo["name"] != "5511999998888" {
		t.Errorf("sem pushName o nome deve cair no identifier, veio %v", corpo["name"])
	}
	if corpo["phone_number"] != "+5511999998888" {
		t.Errorf("phone_number precisa do + do E.164, veio %v", corpo["phone_number"])
	}
}

func TestConversaAbertaIgnoraResolvida(t *testing.T) {
	cliente, _ := servidor(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": 10, "status": "resolved"},
			{"id": 11, "status": "open"},
		})
	})

	conversa, err := cliente.ConversaAberta("src-9")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if conversa == nil || conversa.Id != 11 {
		t.Fatalf("esperado a conversa aberta, veio %+v", conversa)
	}
}

func TestConversaAbertaNilQuandoTodasResolvidas(t *testing.T) {
	cliente, _ := servidor(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{{"id": 10, "status": "resolved"}})
	})

	conversa, err := cliente.ConversaAberta("src-9")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if conversa != nil {
		t.Fatalf("esperado nil, veio %+v", conversa)
	}
}

func TestCriaMensagemUsaCaminhoDaConversa(t *testing.T) {
	cliente, _ := servidor(t, func(w http.ResponseWriter, r *http.Request) {
		esperado := "/public/api/v1/inboxes/ident-inbox/contacts/src-9/conversations/11/messages"
		if r.URL.Path != esperado {
			t.Errorf("caminho inesperado: %s", r.URL.Path)
		}
		var corpo map[string]any
		json.NewDecoder(r.Body).Decode(&corpo)
		if corpo["content"] != "oi" {
			t.Errorf("conteúdo errado: %v", corpo["content"])
		}
		json.NewEncoder(w).Encode(map[string]any{"id": 555})
	})

	mensagem, err := cliente.CriaMensagem("src-9", 11, "oi")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if mensagem.Id != 555 {
		t.Errorf("id errado: %d", mensagem.Id)
	}
}

func TestCriaMensagemComAnexoMandaMultipartIncoming(t *testing.T) {
	var campos map[string]string
	var arquivo []byte
	cliente, _ := servidor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/accounts/7/conversations/11/messages" {
			t.Errorf("anexo precisa da API de conta, veio %s", r.URL.Path)
		}
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("content-type inválido: %v", err)
		}
		leitor := multipart.NewReader(r.Body, params["boundary"])
		form, err := leitor.ReadForm(1 << 20)
		if err != nil {
			t.Fatalf("multipart inválido: %v", err)
		}
		campos = map[string]string{}
		for k, v := range form.Value {
			campos[k] = v[0]
		}
		f, _ := form.File["attachments[]"][0].Open()
		defer f.Close()
		arquivo, _ = io.ReadAll(f)
		json.NewEncoder(w).Encode(map[string]any{"id": 777})
	})

	mensagem, err := cliente.CriaMensagemComAnexo(11, "áudio", "audio.ogg", []byte("bytes-do-audio"))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if mensagem.Id != 777 {
		t.Errorf("id errado: %d", mensagem.Id)
	}
	// Sem message_type=incoming o Chatwoot atribui a mídia ao agente, e a
	// conversa passa a mostrar o cliente como se fosse a gente.
	if campos["message_type"] != "incoming" {
		t.Errorf("message_type errado: %q", campos["message_type"])
	}
	if string(arquivo) != "bytes-do-audio" {
		t.Errorf("conteúdo do anexo errado: %q", arquivo)
	}
}

// Erro do Chatwoot sem o corpo vira "422" sem causa; o corpo é o que diz se foi
// inbox errada, token inválido ou contato duplicado.
func TestErroCarregaCorpoDaResposta(t *testing.T) {
	cliente, _ := servidor(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"message":"Inbox not found"}`))
	})

	_, err := cliente.CriaContato("5511999998888", "Kenny")
	if err == nil {
		t.Fatal("esperado erro")
	}
	if !strings.Contains(err.Error(), "422") || !strings.Contains(err.Error(), "Inbox not found") {
		t.Errorf("erro sem causa: %v", err)
	}
}
