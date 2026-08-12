package chatwoot_handler

import (
	_ "embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

// paginaUI é servida embutida no binário: o manager do upstream só existe
// compilado neste repositório (manager/dist), então não há onde encaixar uma
// tela React sem o código-fonte dele. Uma página própria evita depender disso.
//
//go:embed ui.html
var paginaUI string

// RegisterUIRoute publica a tela de configuração.
//
// O caminho é /chatwoot-ui, e não /chatwoot/ui: o grupo /chatwoot já tem
// /:instanceId, e o gin não aceita rota estática e parâmetro no mesmo nível.
//
// A página não é autenticada — ela é só HTML, sem segredo. Quem autentica é a
// API que ela chama, com a apikey que o operador digita e o navegador guarda
// apenas na sessão.
func (h *Handler) RegisterUIRoute(r *gin.Engine) {
	r.GET("/chatwoot-ui", func(ctx *gin.Context) {
		ctx.Data(http.StatusOK, "text/html; charset=utf-8", []byte(paginaUI))
	})
}
