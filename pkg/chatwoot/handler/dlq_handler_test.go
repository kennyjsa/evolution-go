package chatwoot_handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	chatwoot_consumer "github.com/evolution-foundation/evolution-go/pkg/chatwoot/consumer"
	chatwoot_service "github.com/evolution-foundation/evolution-go/pkg/chatwoot/service"
	config_pkg "github.com/evolution-foundation/evolution-go/pkg/config"
	logger_wrapper "github.com/evolution-foundation/evolution-go/pkg/logger"
	"github.com/gin-gonic/gin"
)

type filaFalsa struct {
	parados     []chatwoot_consumer.EventoParado
	erro        error
	recuperados int
	restantes   int
	reprocessou bool
}

func (f *filaFalsa) Parados() ([]chatwoot_consumer.EventoParado, error) {
	return f.parados, f.erro
}
func (f *filaFalsa) Reprocessa() (int, int, error) {
	f.reprocessou = true
	return f.recuperados, f.restantes, f.erro
}

func servidorDLQ(t *testing.T, fila filaMorta) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	log := logger_wrapper.NewLoggerManager(&config_pkg.Config{LogDirectory: t.TempDir()})

	r := gin.New()
	h := New(&repoFalso{}, chatwoot_service.NewSaida(&repoFalso{},
		func(_, _, _ string) (string, error) { return "", nil }), log)
	h.RegisterDLQRoutes(r, fila, func(c *gin.Context) { c.Next() })
	return r
}

// O alerta decide por um campo só; varrer a lista para saber se há algo seria
// trabalho do lado errado.
func TestListaDaFilaMortaTrazTotal(t *testing.T) {
	fila := &filaFalsa{parados: []chatwoot_consumer.EventoParado{
		{Waid: "W1", InstanceId: "i1", Erro: "chatwoot fora", Quando: time.Now()},
		{Waid: "W2", InstanceId: "i1", Erro: "token invalido", Quando: time.Now()},
	}}

	w := post(servidorDLQ(t, fila), http.MethodGet, "/chatwoot/dlq", "")

	if w.Code != http.StatusOK {
		t.Fatalf("esperado 200, veio %d: %s", w.Code, w.Body.String())
	}
	var corpo struct {
		Total     int `json:"total"`
		Mensagens []struct {
			Waid string `json:"waid"`
			Erro string `json:"erro"`
		} `json:"mensagens"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &corpo); err != nil {
		t.Fatalf("resposta ilegível: %v", err)
	}
	if corpo.Total != 2 {
		t.Errorf("total errado: %d", corpo.Total)
	}
	// O motivo importa: é o que diz ao operador se vale reprocessar ou corrigir
	// a causa antes.
	if corpo.Mensagens[0].Erro != "chatwoot fora" {
		t.Errorf("motivo da falha perdido: %+v", corpo.Mensagens[0])
	}
}

// Fila vazia é o estado normal e não pode parecer erro para o monitor.
func TestFilaMortaVaziaResponde200(t *testing.T) {
	w := post(servidorDLQ(t, &filaFalsa{}), http.MethodGet, "/chatwoot/dlq", "")

	if w.Code != http.StatusOK {
		t.Fatalf("esperado 200, veio %d", w.Code)
	}
	var corpo map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &corpo)
	if corpo["total"].(float64) != 0 {
		t.Errorf("total deveria ser 0: %v", corpo["total"])
	}
}

func TestReprocessarDevolveContagem(t *testing.T) {
	fila := &filaFalsa{recuperados: 3, restantes: 1}

	w := post(servidorDLQ(t, fila), http.MethodPost, "/chatwoot/dlq/reprocessar", "")

	if w.Code != http.StatusOK {
		t.Fatalf("esperado 200, veio %d: %s", w.Code, w.Body.String())
	}
	if !fila.reprocessou {
		t.Errorf("não chamou o reprocessamento")
	}
	var corpo map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &corpo)
	if corpo["recuperados"].(float64) != 3 || corpo["restantes"].(float64) != 1 {
		t.Errorf("contagem errada: %v", corpo)
	}
}

// NATS fora não pode virar "fila vazia": o monitor concluiria que está tudo bem.
func TestFalhaNaFilaViraErro(t *testing.T) {
	w := post(servidorDLQ(t, &filaFalsa{erro: errors.New("nats fora")}),
		http.MethodGet, "/chatwoot/dlq", "")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("esperado 500, veio %d: %s", w.Code, w.Body.String())
	}
}
