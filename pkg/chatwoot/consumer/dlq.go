package chatwoot_consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	chatwoot_service "github.com/evolution-foundation/evolution-go/pkg/chatwoot/service"
	"github.com/nats-io/nats.go/jetstream"
)

// EventoParado descreve um evento que ficou na fila morta, no formato que a
// monitoração consome.
type EventoParado struct {
	Waid       string    `json:"waid"`
	InstanceId string    `json:"instanceId"`
	Telefone   string    `json:"telefone"`
	Erro       string    `json:"erro"`
	Quando     time.Time `json:"quando"`
}

const (
	loteDLQ        = 50
	esperaLoteDLQ  = 2 * time.Second
	durableRedrive = "chatwoot-dlq-redrive"
)

// Parados lista o que está na fila morta sem consumir.
//
// AckNone de propósito: listar não pode remover. Quem decide o destino da
// mensagem é o reprocessamento, não a tela que a mostra.
func (c *Consumer) Parados() ([]EventoParado, error) {
	if c.js == nil {
		return nil, fmt.Errorf("jetstream indisponivel")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := c.js.Stream(ctx, streamDLQ)
	if err != nil {
		return nil, err
	}

	cons, err := stream.CreateConsumer(ctx, jetstream.ConsumerConfig{
		AckPolicy:     jetstream.AckNonePolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return nil, err
	}

	lote, err := cons.Fetch(loteDLQ, jetstream.FetchMaxWait(esperaLoteDLQ))
	if err != nil {
		return nil, err
	}

	parados := []EventoParado{}
	for msg := range lote.Messages() {
		parados = append(parados, descreve(msg))
	}
	return parados, lote.Error()
}

func descreve(msg jetstream.Msg) EventoParado {
	var evento chatwoot_service.Evento
	_ = json.Unmarshal(msg.Data(), &evento)

	parado := EventoParado{
		Waid:       evento.Data.Info.ID,
		InstanceId: evento.InstanceId,
		Telefone:   evento.Data.Info.Sender,
		Erro:       msg.Headers().Get("Chatwoot-Erro"),
	}
	if meta, err := msg.Metadata(); err == nil {
		parado.Quando = meta.Timestamp
	}
	return parado
}

// Reprocessa tenta de novo os eventos da fila morta.
//
// Existe porque uma fila morta sem reprocessamento é só um cemitério com
// inventário: o operador descobre a perda e continua sem a mensagem. Depois de
// corrigir a causa (token errado, Chatwoot fora), isto recoloca as conversas no
// lugar.
//
// O ack só vem quando o evento entra de fato; o que continuar falhando fica na
// fila para a próxima tentativa.
func (c *Consumer) Reprocessa() (recuperados int, restantes int, err error) {
	if c.js == nil {
		return 0, 0, fmt.Errorf("jetstream indisponivel")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream, err := c.js.Stream(ctx, streamDLQ)
	if err != nil {
		return 0, 0, err
	}

	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       durableRedrive,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       ackWait,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return 0, 0, err
	}

	lote, err := cons.Fetch(loteDLQ, jetstream.FetchMaxWait(esperaLoteDLQ))
	if err != nil {
		return 0, 0, err
	}

	log := c.logger.GetLogger("system")
	for msg := range lote.Messages() {
		var evento chatwoot_service.Evento
		if err := json.Unmarshal(msg.Data(), &evento); err != nil {
			// Payload ilegível não melhora com o tempo; ack tira do caminho.
			log.LogError("Fila morta: payload ilegivel descartado: %v", err)
			_ = msg.Ack()
			continue
		}

		resultado, err := c.entrada.Processa(&evento)
		if err != nil {
			log.LogError("Fila morta: waid %s ainda falha: %v", evento.Data.Info.ID, err)
			_ = msg.Nak()
			restantes++
			continue
		}

		if !resultado.Ignorado {
			recuperados++
			log.LogInfo("Fila morta: waid %s recuperado como mensagem %d",
				evento.Data.Info.ID, resultado.ChatwootMessageId)
		}
		_ = msg.Ack()
	}
	return recuperados, restantes, lote.Error()
}
