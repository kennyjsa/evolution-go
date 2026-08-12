package chatwoot_handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	chatwoot_model "github.com/evolution-foundation/evolution-go/pkg/chatwoot/model"
	chatwoot_service "github.com/evolution-foundation/evolution-go/pkg/chatwoot/service"
	config_pkg "github.com/evolution-foundation/evolution-go/pkg/config"
	logger_wrapper "github.com/evolution-foundation/evolution-go/pkg/logger"
	"github.com/gin-gonic/gin"
)

type repoFalso struct {
	config     *chatwoot_model.ChatwootConfig
	salva      *chatwoot_model.ChatwootConfig
	apagadas   []string
	porToken   map[string]*chatwoot_model.ChatwootConfig
	processada *chatwoot_model.MensagemProcessada
}

func (r *repoFalso) GetConfig(string) (*chatwoot_model.ChatwootConfig, error) {
	return r.config, nil
}
func (r *repoFalso) GetConfigByWebhookToken(token string) (*chatwoot_model.ChatwootConfig, error) {
	return r.porToken[token], nil
}
func (r *repoFalso) UpsertConfig(c chatwoot_model.ChatwootConfig) error {
	r.salva = &c
	r.config = &c
	return nil
}
func (r *repoFalso) DeleteConfig(id string) error {
	r.apagadas = append(r.apagadas, id)
	return nil
}
func (r *repoFalso) MarcaProcessada(chatwoot_model.MensagemProcessada) error { return nil }
func (r *repoFalso) BuscaProcessadaPorWaid(string) (*chatwoot_model.MensagemProcessada, error) {
	return r.processada, nil
}
func (r *repoFalso) BuscaProcessadaPorChatwootId(int) (*chatwoot_model.MensagemProcessada, error) {
	return r.processada, nil
}
func (r *repoFalso) RegistraLid(string, string) error     { return nil }
func (r *repoFalso) TelefoneDoLid(string) (string, error) { return "", nil }

func servidor(t *testing.T, repo *repoFalso, enviados *[]string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	log := logger_wrapper.NewLoggerManager(&config_pkg.Config{LogDirectory: t.TempDir()})

	saida := chatwoot_service.NewSaida(repo, func(_, numero, texto string) (string, error) {
		*enviados = append(*enviados, numero+":"+texto)
		return "WAID1", nil
	})

	r := gin.New()
	h := New(repo, saida, log)
	h.RegisterRoutes(r)
	h.RegisterConfigRoutes(r, func(c *gin.Context) { c.Next() })
	return r
}

func post(r *gin.Engine, metodo, url, corpo string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(metodo, url, strings.NewReader(corpo))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

const hookAgente = `{"event":"message_created","message_type":"outgoing","content":"oi","id":9,
	"conversation":{"id":3,"meta":{"sender":{"identifier":"5511999999999"}}}}`

// O token é a única autenticação da rota: token errado não pode mandar mensagem
// pelo número de ninguém.
func TestWebhookRecusaTokenInvalido(t *testing.T) {
	var enviados []string
	repo := &repoFalso{porToken: map[string]*chatwoot_model.ChatwootConfig{}}

	w := post(servidor(t, repo, &enviados), http.MethodPost, "/webhooks/chatwoot/errado", hookAgente)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("esperado 401, veio %d: %s", w.Code, w.Body.String())
	}
	if len(enviados) != 0 {
		t.Errorf("token inválido enviou mensagem: %v", enviados)
	}
}

func TestWebhookComTokenValidoEnvia(t *testing.T) {
	var enviados []string
	repo := &repoFalso{porToken: map[string]*chatwoot_model.ChatwootConfig{
		"tok-bom": {InstanceId: "i1", Enabled: true},
	}}

	w := post(servidor(t, repo, &enviados), http.MethodPost, "/webhooks/chatwoot/tok-bom", hookAgente)

	if w.Code != http.StatusOK {
		t.Fatalf("esperado 200, veio %d: %s", w.Code, w.Body.String())
	}
	if len(enviados) != 1 || enviados[0] != "5511999999999:oi" {
		t.Errorf("envio errado: %v", enviados)
	}
}

// Trocar o webhook_token a cada salvamento quebraria a URL já configurada na
// inbox do Chatwoot; apagar o token da conta desligaria o conector em silêncio.
func TestUpsertPreservaWebhookTokenECredencial(t *testing.T) {
	var enviados []string
	repo := &repoFalso{config: &chatwoot_model.ChatwootConfig{
		InstanceId: "i1", WebhookToken: "tok-antigo", AccountToken: "segredo",
		Url: "http://chatwoot", AccountId: "1", InboxIdentifier: "ident",
	}}

	w := post(servidor(t, repo, &enviados), http.MethodPut, "/chatwoot/i1",
		`{"url":"http://chatwoot","accountId":"1","inboxIdentifier":"ident","inboxId":"2"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("esperado 200, veio %d: %s", w.Code, w.Body.String())
	}
	if repo.salva.WebhookToken != "tok-antigo" {
		t.Errorf("webhook_token trocado: %q", repo.salva.WebhookToken)
	}
	if repo.salva.AccountToken != "segredo" {
		t.Errorf("credencial apagada no salvamento: %q", repo.salva.AccountToken)
	}
}

// A tela não precisa da credencial, e devolvê-la espalharia o token do Chatwoot
// por log e histórico de request.
func TestGetConfigNaoDevolveCredencial(t *testing.T) {
	var enviados []string
	repo := &repoFalso{config: &chatwoot_model.ChatwootConfig{
		InstanceId: "i1", Enabled: true, AccountToken: "segredo", WebhookToken: "tok",
	}}

	w := post(servidor(t, repo, &enviados), http.MethodGet, "/chatwoot/i1", "")

	if strings.Contains(w.Body.String(), "segredo") {
		t.Errorf("credencial vazou na resposta: %s", w.Body.String())
	}
	var corpo map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &corpo); err != nil {
		t.Fatalf("resposta não é JSON: %v", err)
	}
	if corpo["webhookUrl"] != "/webhooks/chatwoot/tok" {
		t.Errorf("webhookUrl errada: %v", corpo["webhookUrl"])
	}
}

// Config incompleta que salva vira conector ligado que falha só na primeira
// mensagem real.
func TestUpsertRecusaConfigIncompleta(t *testing.T) {
	var enviados []string
	repo := &repoFalso{}

	w := post(servidor(t, repo, &enviados), http.MethodPut, "/chatwoot/i1",
		`{"url":"http://chatwoot"}`)

	if w.Code != http.StatusBadRequest {
		t.Errorf("esperado 400, veio %d: %s", w.Code, w.Body.String())
	}
	if repo.salva != nil {
		t.Errorf("config incompleta foi salva: %+v", repo.salva)
	}
}
