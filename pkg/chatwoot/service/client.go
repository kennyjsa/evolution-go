package chatwoot_service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client fala com o Chatwoot por duas APIs distintas, e a diferença importa:
//
//   - a API pública (`/public/api/v1/inboxes/<identifier>/...`) autentica pelo
//     `inbox_identifier` e é a única que cria mensagem já como `incoming`, isto
//     é, atribuída ao contato e não ao agente;
//   - a API de aplicação (`/api/v1/accounts/<id>/...`) exige o token da conta e
//     serve para o que a pública não expõe: busca de contato e anexo.
type Client struct {
	baseUrl         string
	accountId       string
	accountToken    string
	inboxIdentifier string
	http            *http.Client
}

func NewClient(baseUrl, accountId, accountToken, inboxIdentifier string) *Client {
	return &Client{
		baseUrl:         strings.TrimRight(baseUrl, "/"),
		accountId:       accountId,
		accountToken:    accountToken,
		inboxIdentifier: inboxIdentifier,
		http:            &http.Client{Timeout: 30 * time.Second},
	}
}

type Contato struct {
	Id         int    `json:"id"`
	SourceId   string `json:"source_id"`
	Identifier string `json:"identifier"`
	Name       string `json:"name"`
}

type Conversa struct {
	Id int `json:"id"`
}

type Mensagem struct {
	Id int `json:"id"`
}

func (c *Client) do(req *http.Request, destino any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	corpo, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// O corpo entra no erro porque o Chatwoot devolve a causa real ali
		// (inbox errada, token inválido, contato duplicado) — sem ele o 422
		// não diz nada.
		return fmt.Errorf("chatwoot %s %s: %d %s",
			req.Method, req.URL.Path, resp.StatusCode, strings.TrimSpace(string(corpo)))
	}
	if destino == nil || len(corpo) == 0 {
		return nil
	}
	return json.Unmarshal(corpo, destino)
}

func (c *Client) requisicaoJson(metodo, endereco string, corpo any, autenticaConta bool) (*http.Request, error) {
	var leitor io.Reader
	if corpo != nil {
		bruto, err := json.Marshal(corpo)
		if err != nil {
			return nil, err
		}
		leitor = bytes.NewReader(bruto)
	}

	req, err := http.NewRequest(metodo, endereco, leitor)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if autenticaConta {
		req.Header.Set("api_access_token", c.accountToken)
	}
	return req, nil
}

// AtualizaStatus reflete no Chatwoot o recibo do WhatsApp (entregue/lido).
//
// O próprio Chatwoot recusa a transição de `read` de volta para `delivered`,
// então recibo fora de ordem não desfaz o que já foi marcado como lido.
func (c *Client) AtualizaStatus(conversaId, mensagemId int, status string) error {
	endereco := c.urlConta(fmt.Sprintf("/conversations/%d/messages/%d", conversaId, mensagemId))
	req, err := c.requisicaoJson(http.MethodPatch, endereco,
		map[string]string{"status": status}, true)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

func (c *Client) urlPublica(caminho string) string {
	return fmt.Sprintf("%s/public/api/v1/inboxes/%s%s", c.baseUrl, c.inboxIdentifier, caminho)
}

func (c *Client) urlConta(caminho string) string {
	return fmt.Sprintf("%s/api/v1/accounts/%s%s", c.baseUrl, c.accountId, caminho)
}

// BuscaContato procura pelo `identifier`, que é o telefone em formato E.164 sem
// o "+". Devolve nil quando não existe — criar é decisão de quem chamou.
func (c *Client) BuscaContato(identifier string) (*Contato, error) {
	endereco := c.urlConta("/contacts/search?q=" + url.QueryEscape(identifier))
	req, err := c.requisicaoJson(http.MethodGet, endereco, nil, true)
	if err != nil {
		return nil, err
	}

	var resposta struct {
		Payload []Contato `json:"payload"`
	}
	if err := c.do(req, &resposta); err != nil {
		return nil, err
	}

	// A busca é textual e casa por prefixo: só o identifier idêntico é o
	// contato certo, senão "5511" traria qualquer número de São Paulo.
	for _, contato := range resposta.Payload {
		if contato.Identifier == identifier {
			achado := contato
			return &achado, nil
		}
	}
	return nil, nil
}

// CriaContato registra o contato na inbox e devolve o `source_id`, que é o que
// identifica o contato nas chamadas seguintes da API pública.
func (c *Client) CriaContato(identifier, nome string) (*Contato, error) {
	if nome == "" {
		nome = identifier
	}

	req, err := c.requisicaoJson(http.MethodPost, c.urlPublica("/contacts"), map[string]any{
		"identifier":   identifier,
		"name":         nome,
		"phone_number": "+" + identifier,
	}, false)
	if err != nil {
		return nil, err
	}

	var contato Contato
	if err := c.do(req, &contato); err != nil {
		return nil, err
	}
	return &contato, nil
}

// ConversaAberta devolve a conversa em aberto do contato, ou nil.
//
// Reusar a conversa é o que mantém o histórico do lead num fio só; criar uma
// por mensagem espalharia a negociação por dezenas de conversas.
func (c *Client) ConversaAberta(sourceId string) (*Conversa, error) {
	req, err := c.requisicaoJson(http.MethodGet,
		c.urlPublica("/contacts/"+url.PathEscape(sourceId)+"/conversations"), nil, false)
	if err != nil {
		return nil, err
	}

	var conversas []struct {
		Id     int    `json:"id"`
		Status string `json:"status"`
	}
	if err := c.do(req, &conversas); err != nil {
		return nil, err
	}

	for _, conversa := range conversas {
		if conversa.Status != "resolved" {
			return &Conversa{Id: conversa.Id}, nil
		}
	}
	return nil, nil
}

func (c *Client) CriaConversa(sourceId string) (*Conversa, error) {
	req, err := c.requisicaoJson(http.MethodPost,
		c.urlPublica("/contacts/"+url.PathEscape(sourceId)+"/conversations"), map[string]any{}, false)
	if err != nil {
		return nil, err
	}

	var conversa Conversa
	if err := c.do(req, &conversa); err != nil {
		return nil, err
	}
	return &conversa, nil
}

// CriaMensagem publica a mensagem recebida do WhatsApp como `incoming`, ou
// seja, vinda do contato.
func (c *Client) CriaMensagem(sourceId string, conversaId int, texto string) (*Mensagem, error) {
	endereco := c.urlPublica(fmt.Sprintf("/contacts/%s/conversations/%d/messages",
		url.PathEscape(sourceId), conversaId))

	req, err := c.requisicaoJson(http.MethodPost, endereco, map[string]any{
		"content": texto,
	}, false)
	if err != nil {
		return nil, err
	}

	var mensagem Mensagem
	if err := c.do(req, &mensagem); err != nil {
		return nil, err
	}
	return &mensagem, nil
}

// CriaMensagemComAnexo sobe mídia (áudio, imagem, documento) pela API de conta:
// a API pública não aceita anexo, e é justamente a mídia que o roteiro de
// recuperação por SQL nunca conseguiu resgatar.
func (c *Client) CriaMensagemComAnexo(conversaId int, texto, nomeArquivo string, conteudo []byte) (*Mensagem, error) {
	var corpo bytes.Buffer
	form := multipart.NewWriter(&corpo)

	if err := form.WriteField("content", texto); err != nil {
		return nil, err
	}
	if err := form.WriteField("message_type", "incoming"); err != nil {
		return nil, err
	}
	arquivo, err := form.CreateFormFile("attachments[]", nomeArquivo)
	if err != nil {
		return nil, err
	}
	if _, err := arquivo.Write(conteudo); err != nil {
		return nil, err
	}
	if err := form.Close(); err != nil {
		return nil, err
	}

	endereco := c.urlConta(fmt.Sprintf("/conversations/%d/messages", conversaId))
	req, err := http.NewRequest(http.MethodPost, endereco, &corpo)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("api_access_token", c.accountToken)

	var mensagem Mensagem
	if err := c.do(req, &mensagem); err != nil {
		return nil, err
	}
	return &mensagem, nil
}
