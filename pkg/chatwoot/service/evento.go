package chatwoot_service

import (
	"encoding/json"
	"strings"
)

// Evento é o recorte do payload que o evolution-go publica na fila para o
// evento `Message`. Só os campos que o conector usa são declarados: o resto do
// evento do whatsmeow é grande e volátil, e decodificar tudo só criaria
// acoplamento com campos que não lemos.
type Evento struct {
	Event        string `json:"event"`
	InstanceId   string `json:"instanceId"`
	InstanceName string `json:"instanceName"`
	Data         struct {
		Info struct {
			ID        string `json:"ID"`
			Sender    string `json:"Sender"`
			SenderAlt string `json:"SenderAlt"`
			Chat      string `json:"Chat"`
			PushName  string `json:"PushName"`
			IsFromMe  bool   `json:"IsFromMe"`
			IsGroup   bool   `json:"IsGroup"`
		} `json:"Info"`
		Message json.RawMessage `json:"Message"`
	} `json:"data"`
}

func (e *Evento) InfoIdentidade() Info {
	return Info{
		Sender:    e.Data.Info.Sender,
		SenderAlt: e.Data.Info.SenderAlt,
		Chat:      e.Data.Info.Chat,
	}
}

func (e *Evento) EhGrupo() bool {
	return e.Data.Info.IsGroup || strings.Contains(e.Data.Info.Chat, "@g.us")
}

func (e *Evento) EhStatus() bool {
	return strings.Contains(e.Data.Info.Chat, "@broadcast") ||
		strings.Contains(e.Data.Info.Chat, "status@")
}

// conteudo é a estrutura mínima do waE2E.Message que carrega texto legível.
type conteudo struct {
	Conversation        string `json:"conversation"`
	ExtendedTextMessage *struct {
		Text string `json:"text"`
	} `json:"extendedTextMessage"`
	ImageMessage *struct {
		Caption  string `json:"caption"`
		Mimetype string `json:"mimetype"`
	} `json:"imageMessage"`
	VideoMessage *struct {
		Caption  string `json:"caption"`
		Mimetype string `json:"mimetype"`
	} `json:"videoMessage"`
	DocumentMessage *struct {
		Caption  string `json:"caption"`
		FileName string `json:"fileName"`
		Mimetype string `json:"mimetype"`
	} `json:"documentMessage"`
	AudioMessage *struct {
		Mimetype string `json:"mimetype"`
	} `json:"audioMessage"`
	StickerMessage *struct {
		Mimetype string `json:"mimetype"`
	} `json:"stickerMessage"`
	LocationMessage *struct {
		DegreesLatitude  float64 `json:"degreesLatitude"`
		DegreesLongitude float64 `json:"degreesLongitude"`
	} `json:"locationMessage"`
}

// Texto extrai o que vai virar o corpo da mensagem no Chatwoot.
//
// Mídia sem legenda devolve um marcador em vez de vazio: o Chatwoot recusa
// mensagem sem conteúdo, e uma mensagem faltando na conversa é pior para o
// agente do que uma linha dizendo que chegou áudio.
func (e *Evento) Texto() string {
	if len(e.Data.Message) == 0 {
		return ""
	}

	var c conteudo
	if err := json.Unmarshal(e.Data.Message, &c); err != nil {
		return ""
	}

	switch {
	case c.Conversation != "":
		return c.Conversation
	case c.ExtendedTextMessage != nil && c.ExtendedTextMessage.Text != "":
		return c.ExtendedTextMessage.Text
	case c.ImageMessage != nil:
		return ouMarcador(c.ImageMessage.Caption, "[imagem]")
	case c.VideoMessage != nil:
		return ouMarcador(c.VideoMessage.Caption, "[vídeo]")
	case c.DocumentMessage != nil:
		if c.DocumentMessage.Caption != "" {
			return c.DocumentMessage.Caption
		}
		return ouMarcador(c.DocumentMessage.FileName, "[documento]")
	case c.AudioMessage != nil:
		return "[áudio]"
	case c.StickerMessage != nil:
		return "[figurinha]"
	case c.LocationMessage != nil:
		return "[localização]"
	}
	return ""
}

// Midia descreve o anexo de um evento. Para uma agência de viagens é conteúdo
// principal — foto de hotel, lâmina de roteiro, áudio de negociação — e não uma
// borda do texto.
type Midia struct {
	Tem      bool
	Tipo     string // image, video, audio, document, sticker
	Arquivo  string
	Mimetype string
}

// TemMidia devolve o anexo do evento, se houver.
func (e *Evento) TemMidia() Midia {
	if len(e.Data.Message) == 0 {
		return Midia{}
	}

	var c conteudo
	if err := json.Unmarshal(e.Data.Message, &c); err != nil {
		return Midia{}
	}

	switch {
	case c.ImageMessage != nil:
		return Midia{true, "image", nomeArquivo(e, c.ImageMessage.Mimetype, "jpg"), c.ImageMessage.Mimetype}
	case c.VideoMessage != nil:
		return Midia{true, "video", nomeArquivo(e, c.VideoMessage.Mimetype, "mp4"), c.VideoMessage.Mimetype}
	case c.AudioMessage != nil:
		return Midia{true, "audio", nomeArquivo(e, c.AudioMessage.Mimetype, "ogg"), c.AudioMessage.Mimetype}
	case c.StickerMessage != nil:
		return Midia{true, "sticker", nomeArquivo(e, c.StickerMessage.Mimetype, "webp"), c.StickerMessage.Mimetype}
	case c.DocumentMessage != nil:
		// O nome original importa: o Chatwoot mostra o anexo por ele, e um
		// "arquivo" genérico esconde qual roteiro foi enviado ao cliente.
		nome := strings.TrimSpace(c.DocumentMessage.FileName)
		if nome == "" {
			nome = nomeArquivo(e, c.DocumentMessage.Mimetype, "bin")
		}
		return Midia{true, "document", nome, c.DocumentMessage.Mimetype}
	}
	return Midia{}
}

// nomeArquivo monta um nome estável a partir do WAID: o WhatsApp não manda nome
// para foto e áudio, e nomes repetidos viram anexos indistinguíveis na conversa.
func nomeArquivo(e *Evento, mimetype, padrao string) string {
	ext := padrao
	if i := strings.Index(mimetype, "/"); i != -1 {
		if bruta := strings.SplitN(mimetype[i+1:], ";", 2)[0]; bruta != "" {
			ext = bruta
		}
	}

	id := e.Data.Info.ID
	if len(id) > 12 {
		id = id[:12]
	}
	if id == "" {
		id = "anexo"
	}
	return id + "." + ext
}

// marcadores são os textos de fallback devolvidos por Texto() quando a mídia
// não tem legenda.
var marcadores = map[string]bool{
	"[imagem]": true, "[vídeo]": true, "[documento]": true,
	"[áudio]": true, "[figurinha]": true, "[localização]": true,
}

func ehMarcador(texto string) bool { return marcadores[texto] }

func ouMarcador(texto, marcador string) string {
	if strings.TrimSpace(texto) != "" {
		return texto
	}
	return marcador
}
