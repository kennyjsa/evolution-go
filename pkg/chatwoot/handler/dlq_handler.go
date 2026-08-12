package chatwoot_handler

import (
	"net/http"

	chatwoot_consumer "github.com/evolution-foundation/evolution-go/pkg/chatwoot/consumer"
	"github.com/gin-gonic/gin"
)

// filaMorta é o recorte do consumidor que o handler usa; interface para o teste
// não precisar de NATS.
type filaMorta interface {
	Parados() ([]chatwoot_consumer.EventoParado, error)
	Reprocessa() (int, int, error)
}

// RegisterDLQRoutes publica a fila morta para monitoração e reprocessamento.
//
// Atrás da apikey global: o conteúdo é mensagem de cliente, e o reprocessamento
// escreve no Chatwoot.
func (h *Handler) RegisterDLQRoutes(r *gin.Engine, fila filaMorta, authAdmin gin.HandlerFunc) {
	grupo := r.Group("/chatwoot/dlq")
	grupo.Use(authAdmin)
	{
		grupo.GET("", func(ctx *gin.Context) { h.listaDLQ(ctx, fila) })
		grupo.POST("/reprocessar", func(ctx *gin.Context) { h.reprocessaDLQ(ctx, fila) })
	}
}

func (h *Handler) listaDLQ(ctx *gin.Context, fila filaMorta) {
	parados, err := fila.Parados()
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// `total` no topo para o alerta poder decidir com um campo só, sem varrer a
	// lista.
	ctx.JSON(http.StatusOK, gin.H{
		"total":     len(parados),
		"mensagens": parados,
	})
}

func (h *Handler) reprocessaDLQ(ctx *gin.Context, fila filaMorta) {
	recuperados, restantes, err := fila.Reprocessa()
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.logger.GetLogger("system").LogInfo(
		"Fila morta reprocessada: %d recuperados, %d ainda falhando", recuperados, restantes)
	ctx.JSON(http.StatusOK, gin.H{
		"recuperados": recuperados,
		"restantes":   restantes,
	})
}
