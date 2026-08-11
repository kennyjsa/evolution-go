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
		Caption string `json:"caption"`
	} `json:"imageMessage"`
	VideoMessage *struct {
		Caption string `json:"caption"`
	} `json:"videoMessage"`
	DocumentMessage *struct {
		Caption  string `json:"caption"`
		FileName string `json:"fileName"`
	} `json:"documentMessage"`
	AudioMessage    *struct{} `json:"audioMessage"`
	StickerMessage  *struct{} `json:"stickerMessage"`
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

func ouMarcador(texto, marcador string) string {
	if strings.TrimSpace(texto) != "" {
		return texto
	}
	return marcador
}
