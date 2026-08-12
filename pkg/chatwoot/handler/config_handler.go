package chatwoot_handler

import (
	"net/http"

	chatwoot_model "github.com/evolution-foundation/evolution-go/pkg/chatwoot/model"
	"github.com/gin-gonic/gin"
)

// configBody é o corpo aceito no upsert. O webhook_token não entra: ele é
// gerado pelo servidor e sobrescrevê-lo pela API deixaria o webhook antigo
// válido para quem já o conhecia.
type configBody struct {
	Enabled         *bool  `json:"enabled"`
	Url             string `json:"url"`
	AccountId       string `json:"accountId"`
	AccountToken    string `json:"accountToken"`
	InboxId         string `json:"inboxId"`
	InboxIdentifier string `json:"inboxIdentifier"`
	HmacToken       string `json:"hmacToken"`
	MarkAsRead      bool   `json:"markAsRead"`
	SyncGroups      bool   `json:"syncGroups"`
	// Ponteiro para distinguir "false" de ausente: a assinatura é ligada por
	// padrão, e um corpo sem o campo não pode desligá-la em silêncio.
	SignMsg       *bool  `json:"signMsg"`
	SignDelimiter string `json:"signDelimiter"`
}

// RegisterConfigRoutes registra o CRUD da config. Diferente do webhook, estas
// rotas mexem em credencial do Chatwoot e ficam atrás da apikey global.
func (h *Handler) RegisterConfigRoutes(r *gin.Engine, authAdmin gin.HandlerFunc) {
	grupo := r.Group("/chatwoot")
	grupo.Use(authAdmin)
	{
		grupo.GET("/:instanceId", h.GetConfig)
		grupo.PUT("/:instanceId", h.UpsertConfig)
		grupo.DELETE("/:instanceId", h.DeleteConfig)
	}
}

func (h *Handler) GetConfig(ctx *gin.Context) {
	config, err := h.repo.GetConfig(ctx.Param("instanceId"))
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if config == nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "config não encontrada"})
		return
	}

	// O token da conta não volta na leitura: a tela não precisa dele e devolvê-lo
	// espalharia a credencial do Chatwoot por log e histórico de request.
	ctx.JSON(http.StatusOK, gin.H{
		"instanceId":      config.InstanceId,
		"enabled":         config.Enabled,
		"url":             config.Url,
		"accountId":       config.AccountId,
		"accountTokenSet": config.AccountToken != "",
		"inboxId":         config.InboxId,
		"inboxIdentifier": config.InboxIdentifier,
		"markAsRead":      config.MarkAsRead,
		"syncGroups":      config.SyncGroups,
		"signMsg":         config.SignMsg,
		"signDelimiter":   config.SignDelimiter,
		"webhookUrl":      "/webhooks/chatwoot/" + config.WebhookToken,
	})
}

func (h *Handler) UpsertConfig(ctx *gin.Context) {
	instanceId := ctx.Param("instanceId")

	var body configBody
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	atual, err := h.repo.GetConfig(instanceId)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	config := chatwoot_model.ChatwootConfig{
		InstanceId:      instanceId,
		Enabled:         body.Enabled == nil || *body.Enabled,
		Url:             body.Url,
		AccountId:       body.AccountId,
		AccountToken:    body.AccountToken,
		InboxId:         body.InboxId,
		InboxIdentifier: body.InboxIdentifier,
		HmacToken:       body.HmacToken,
		MarkAsRead:      body.MarkAsRead,
		SyncGroups:      body.SyncGroups,
		SignMsg:         body.SignMsg == nil || *body.SignMsg,
		SignDelimiter:   body.SignDelimiter,
	}
	if atual != nil {
		// Preserva o token do webhook: trocá-lo a cada salvamento quebraria a
		// URL já configurada na inbox do Chatwoot.
		config.WebhookToken = atual.WebhookToken
		// Salvar sem repetir a credencial não pode apagá-la — é assim que a tela
		// consegue editar os outros campos sem nunca receber o token de volta.
		if config.AccountToken == "" {
			config.AccountToken = atual.AccountToken
		}
	}

	if config.Url == "" || config.AccountId == "" || config.AccountToken == "" ||
		config.InboxIdentifier == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": "url, accountId, accountToken e inboxIdentifier são obrigatórios"})
		return
	}

	if err := h.repo.UpsertConfig(config); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Releitura: o webhook_token de uma config nova é gerado no BeforeCreate, e
	// é justamente o que a tela precisa mostrar para configurar a inbox.
	salva, err := h.repo.GetConfig(instanceId)
	if err != nil || salva == nil {
		ctx.JSON(http.StatusOK, gin.H{"status": "saved"})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{
		"status":     "saved",
		"webhookUrl": "/webhooks/chatwoot/" + salva.WebhookToken,
	})
}

func (h *Handler) DeleteConfig(ctx *gin.Context) {
	if err := h.repo.DeleteConfig(ctx.Param("instanceId")); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "deleted"})
}
