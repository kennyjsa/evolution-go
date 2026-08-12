package chatwoot_handler

import (
	"net/http"

	chatwoot_repository "github.com/evolution-foundation/evolution-go/pkg/chatwoot/repository"
	chatwoot_service "github.com/evolution-foundation/evolution-go/pkg/chatwoot/service"
	logger_wrapper "github.com/evolution-foundation/evolution-go/pkg/logger"
	"github.com/gin-gonic/gin"
)

// Handler expõe a rota que o Chatwoot chama quando um agente responde.
//
// A rota é pública por natureza (o Chatwoot não manda apikey), então quem
// autentica é o token opaco no path, casado contra a config da instância.
type Handler struct {
	repo   chatwoot_repository.ChatwootRepository
	saida  *chatwoot_service.Saida
	logger *logger_wrapper.LoggerManager
}

func New(
	repo chatwoot_repository.ChatwootRepository,
	saida *chatwoot_service.Saida,
	logger *logger_wrapper.LoggerManager,
) *Handler {
	return &Handler{repo: repo, saida: saida, logger: logger}
}

func (h *Handler) RegisterRoutes(r *gin.Engine) {
	r.POST("/webhooks/chatwoot/:token", h.Webhook)
}

func (h *Handler) Webhook(ctx *gin.Context) {
	config, err := h.repo.GetConfigByWebhookToken(ctx.Param("token"))
	if err != nil {
		h.logger.GetLogger("system").LogError("Webhook Chatwoot: falha ao ler config: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if config == nil {
		// Mesma resposta para token errado e inexistente: distinguir os dois
		// permitiria descobrir tokens válidos por tentativa.
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}

	var hook chatwoot_service.WebhookChatwoot
	if err := ctx.ShouldBindJSON(&hook); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	resultado, err := h.saida.Processa(config, &hook)
	if err != nil {
		h.logger.GetLogger(config.InstanceId).LogError(
			"[%s] Webhook Chatwoot falhou na mensagem %d: %v", config.InstanceId, hook.Id, err)
		// 500 faz o Chatwoot reentregar, que é o que se quer no erro transitório.
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if resultado.Ignorado {
		h.logger.GetLogger(config.InstanceId).LogInfo(
			"[%s] Webhook Chatwoot ignorou mensagem %d: %s",
			config.InstanceId, hook.Id, resultado.Motivo)
		ctx.JSON(http.StatusOK, gin.H{"status": "ignored", "reason": resultado.Motivo})
		return
	}

	h.logger.GetLogger(config.InstanceId).LogInfo(
		"[%s] Webhook Chatwoot enviou mensagem %d ao WhatsApp", config.InstanceId, hook.Id)
	ctx.JSON(http.StatusOK, gin.H{"status": "sent"})
}
