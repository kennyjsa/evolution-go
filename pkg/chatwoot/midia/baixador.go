// Package chatwoot_midia liga o conector do Chatwoot ao download de mídia do
// whatsmeow, sem que o serviço de entrada precise conhecer o WhatsApp.
package chatwoot_midia

import (
	"context"
	"fmt"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/encoding/protojson"
)

// Baixador resolve o conteúdo de um anexo a partir do bloco `Message` do evento.
//
// O evento publicado no NATS carrega o waE2E.Message serializado em JSON do
// protobuf, e é dele que saem as chaves de descriptografia — por isso o
// download precisa do cliente conectado da instância, e não de uma URL.
type Baixador struct {
	clientes map[string]*whatsmeow.Client
}

func NovoBaixador(clientes map[string]*whatsmeow.Client) *Baixador {
	return &Baixador{clientes: clientes}
}

func (b *Baixador) Baixa(instanceId string, mensagemBruta []byte) ([]byte, error) {
	cliente := b.clientes[instanceId]
	if cliente == nil {
		return nil, fmt.Errorf("instância %s sem cliente conectado", instanceId)
	}

	var msg waE2E.Message
	// DiscardUnknown: o whatsmeow evolui mais rápido que este recorte, e um
	// campo novo no payload não pode impedir o download de uma foto.
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(mensagemBruta, &msg); err != nil {
		return nil, fmt.Errorf("payload de mídia ilegível: %w", err)
	}

	ctx := context.Background()
	switch {
	case msg.GetImageMessage() != nil:
		return cliente.Download(ctx, msg.GetImageMessage())
	case msg.GetVideoMessage() != nil:
		return cliente.Download(ctx, msg.GetVideoMessage())
	case msg.GetAudioMessage() != nil:
		return cliente.Download(ctx, msg.GetAudioMessage())
	case msg.GetDocumentMessage() != nil:
		return cliente.Download(ctx, msg.GetDocumentMessage())
	case msg.GetStickerMessage() != nil:
		return cliente.Download(ctx, msg.GetStickerMessage())
	}
	return nil, fmt.Errorf("mensagem sem mídia para baixar")
}
