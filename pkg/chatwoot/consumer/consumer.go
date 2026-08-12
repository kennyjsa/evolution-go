// Package chatwoot_consumer liga o stream do JetStream ao conector do Chatwoot.
//
// Sem ele o fork publica os eventos e ninguém os lê: o conector existe, compila
// e é testado, mas nenhuma mensagem chega à conversa. Este é o consumidor
// durável que fecha esse caminho.
package chatwoot_consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	chatwoot_service "github.com/evolution-foundation/evolution-go/pkg/chatwoot/service"
	logger_wrapper "github.com/evolution-foundation/evolution-go/pkg/logger"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Os subjects espelham o que o producer publica: `evolution.<evento>` na
// publicação global e `evolution.<instanceId>.<evento>` na por instância.
// Assinar os dois evita depender de qual das duas está ligada na instância; a
// duplicata que isso possa gerar morre na idempotência por WAID da Entrada.
const (
	subjectGlobal    = "evolution.message"
	subjectInstancia = "evolution.*.message"

	// Durável: o consumidor guarda a posição no servidor, então evento que
	// chegou com o evolution-go fora do ar é entregue quando ele volta.
	nomeDurable = "chatwoot-entrada"

	maxDeliver = 5
	ackWait    = 30 * time.Second
)

// processador é o recorte da Entrada que o consumidor usa — declarado aqui para
// o teste rodar sem banco nem Chatwoot.
type processador interface {
	Processa(evento *chatwoot_service.Evento) (chatwoot_service.Resultado, error)
}

type Consumer struct {
	conn    *nats.Conn
	entrada processador
	logger  *logger_wrapper.LoggerManager
	ctx     jetstream.ConsumeContext
}

func New(entrada processador, logger *logger_wrapper.LoggerManager) *Consumer {
	return &Consumer{entrada: entrada, logger: logger}
}

// Start conecta, cria/atualiza o consumidor durável e começa a consumir em
// background. Devolve erro só no que impede subir; falha de rede depois disso é
// tratada pela reconexão do próprio cliente NATS.
func (c *Consumer) Start(url, streamName string) error {
	conn, err := nats.Connect(url,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return fmt.Errorf("conector chatwoot: falha ao conectar no NATS: %w", err)
	}
	c.conn = conn

	js, err := jetstream.New(conn)
	if err != nil {
		return fmt.Errorf("conector chatwoot: falha ao criar contexto JetStream: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := js.Stream(ctx, streamName)
	if err != nil {
		return fmt.Errorf("conector chatwoot: stream %s indisponível: %w", streamName, err)
	}

	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:        nomeDurable,
		FilterSubjects: []string{subjectGlobal, subjectInstancia},
		AckPolicy:      jetstream.AckExplicitPolicy,
		AckWait:        ackWait,
		MaxDeliver:     maxDeliver,
		DeliverPolicy:  jetstream.DeliverNewPolicy,
		Description:    "Entrada do conector Chatwoot",
	})
	if err != nil {
		return fmt.Errorf("conector chatwoot: falha ao criar consumidor durável: %w", err)
	}

	consumeCtx, err := cons.Consume(func(msg jetstream.Msg) { c.trata(msg) })
	if err != nil {
		return fmt.Errorf("conector chatwoot: falha ao iniciar consumo: %w", err)
	}
	c.ctx = consumeCtx

	c.logger.GetLogger("system").LogInfo(
		"Conector Chatwoot consumindo %s de %s (%s, %s)",
		nomeDurable, streamName, subjectGlobal, subjectInstancia)
	return nil
}

// trata decide entre ack e reentrega. A regra: só volta para a fila o que uma
// nova tentativa pode resolver. Payload inválido reentregue é reentregue para
// sempre — isso trava o consumidor numa mensagem que nunca vai passar.
func (c *Consumer) trata(msg jetstream.Msg) {
	var evento chatwoot_service.Evento
	if err := json.Unmarshal(msg.Data(), &evento); err != nil {
		c.logger.GetLogger("system").LogError(
			"Conector Chatwoot: payload ilegível em %s, descartado: %v", msg.Subject(), err)
		_ = msg.Ack()
		return
	}

	log := c.logger.GetLogger(evento.InstanceId)

	resultado, err := c.entrada.Processa(&evento)
	if err != nil {
		log.LogError("[%s] Conector Chatwoot falhou no waid %s: %v",
			evento.InstanceId, evento.Data.Info.ID, err)
		// Nak explícito reentrega já, em vez de esperar o AckWait vencer.
		_ = msg.Nak()
		return
	}

	if resultado.Ignorado {
		log.LogInfo("[%s] Conector Chatwoot ignorou waid %s: %s",
			evento.InstanceId, evento.Data.Info.ID, resultado.Motivo)
	} else {
		log.LogInfo("[%s] Conector Chatwoot criou mensagem %d no Chatwoot (waid %s)",
			evento.InstanceId, resultado.ChatwootMessageId, evento.Data.Info.ID)
	}
	_ = msg.Ack()
}

func (c *Consumer) Stop() {
	if c.ctx != nil {
		c.ctx.Stop()
	}
	if c.conn != nil {
		c.conn.Close()
	}
}
