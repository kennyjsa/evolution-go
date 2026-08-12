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
	"strings"
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

	// Recibos alimentam o status da mensagem no Chatwoot (entregue/lido).
	subjectReciboGlobal    = "evolution.receipt"
	subjectReciboInstancia = "evolution.*.receipt"

	// Durável: o consumidor guarda a posição no servidor, então evento que
	// chegou com o evolution-go fora do ar é entregue quando ele volta.
	nomeDurable = "chatwoot-entrada"

	maxDeliver        = 5
	publishTimeoutDLQ = 10 * time.Second
	ackWait           = 30 * time.Second

	// Fila morta: depois de maxDeliver tentativas a mensagem para de ser
	// reentregue e some. Guardá-la aqui é o que separa "falhou e alguém vê" de
	// "sumiu calada" — foi assim que uma mensagem real se perdeu quando o token
	// do Chatwoot estava errado.
	streamDLQ   = "CHATWOOT_DLQ"
	subjectDLQ  = "evolution.chatwoot.dlq"
	validadeDLQ = 30 * 24 * time.Hour
)

// processador é o recorte da Entrada que o consumidor usa — declarado aqui para
// o teste rodar sem banco nem Chatwoot.
type processador interface {
	Processa(evento *chatwoot_service.Evento) (chatwoot_service.Resultado, error)
}

// processadorRecibo recebe o payload cru: o recibo tem forma própria e nada a
// ver com o evento de mensagem.
type processadorRecibo interface {
	Processa(bruto []byte) (chatwoot_service.Resultado, error)
}

type Consumer struct {
	conn *nats.Conn
	js   jetstream.JetStream
	// publicaNaFilaMorta existe para o teste exercitar a decisão de ack sem
	// subir um servidor NATS.
	publicaNaFilaMorta func(dados []byte) error
	entrada            processador
	recibo             processadorRecibo
	logger             *logger_wrapper.LoggerManager
	ctx                jetstream.ConsumeContext
}

func New(entrada processador, logger *logger_wrapper.LoggerManager) *Consumer {
	return &Consumer{entrada: entrada, logger: logger}
}

// ComRecibos liga o status de entrega/leitura. Sem isto o consumidor continua
// assinando só as mensagens.
func (c *Consumer) ComRecibos(recibo processadorRecibo) *Consumer {
	c.recibo = recibo
	return c
}

func (c *Consumer) subjects() []string {
	s := []string{subjectGlobal, subjectInstancia}
	if c.recibo != nil {
		s = append(s, subjectReciboGlobal, subjectReciboInstancia)
	}
	return s
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

	c.js = js

	// A fila morta é um stream próprio, com retenção longa: ela precisa
	// sobreviver ao MaxAge curto do stream de eventos, senão a mensagem perdida
	// desapareceria antes de alguém olhar.
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:        streamDLQ,
		Subjects:    []string{subjectDLQ},
		Storage:     jetstream.FileStorage,
		Retention:   jetstream.LimitsPolicy,
		MaxAge:      validadeDLQ,
		Description: "Eventos que o conector Chatwoot nao conseguiu processar",
	}); err != nil {
		// Sem DLQ o conector ainda funciona; só volta a perder o que falha
		// demais. Não é motivo para deixar o WhatsApp fora do ar.
		c.logger.GetLogger("system").LogError(
			"Conector Chatwoot: falha ao preparar a fila morta: %v", err)
	}

	stream, err := js.Stream(ctx, streamName)
	if err != nil {
		return fmt.Errorf("conector chatwoot: stream %s indisponível: %w", streamName, err)
	}

	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:        nomeDurable,
		FilterSubjects: c.subjects(),
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
		"Conector Chatwoot consumindo %s de %s (%s)",
		nomeDurable, streamName, strings.Join(c.subjects(), ", "))
	return nil
}

// trata decide entre ack e reentrega. A regra: só volta para a fila o que uma
// nova tentativa pode resolver. Payload inválido reentregue é reentregue para
// sempre — isso trava o consumidor numa mensagem que nunca vai passar.
func (c *Consumer) trata(msg jetstream.Msg) {
	// Recibo tem forma própria; roteia pelo subject para não tentar lê-lo como
	// evento de mensagem.
	if strings.HasSuffix(msg.Subject(), ".receipt") {
		c.trataRecibo(msg)
		return
	}

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

		// Última tentativa: em vez de deixar o JetStream descartar em silêncio,
		// a mensagem vai para a fila morta e o ack libera a fila.
		if c.ultimaTentativa(msg) {
			c.paraFilaMorta(msg, err)
			return
		}

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

// trataRecibo atualiza o status da mensagem no Chatwoot.
//
// Status é informação acessória: se falhar, vale uma retentativa, mas nunca ao
// ponto de travar a fila — por isso o mesmo critério de ack da mensagem.
func (c *Consumer) trataRecibo(msg jetstream.Msg) {
	if c.recibo == nil {
		_ = msg.Ack()
		return
	}

	resultado, err := c.recibo.Processa(msg.Data())
	if err != nil {
		c.logger.GetLogger("system").LogError(
			"Conector Chatwoot falhou ao atualizar status: %v", err)
		_ = msg.Nak()
		return
	}
	if resultado.Ignorado {
		c.logger.GetLogger("system").LogInfo(
			"Conector Chatwoot ignorou recibo: %s", resultado.Motivo)
	}
	_ = msg.Ack()
}

// ultimaTentativa diz se esta entrega é a última que o JetStream fará.
func (c *Consumer) ultimaTentativa(msg jetstream.Msg) bool {
	meta, err := msg.Metadata()
	if err != nil {
		// Sem metadados não dá para saber a tentativa; reentregar é mais seguro
		// que mandar para a fila morta cedo demais.
		return false
	}
	return meta.NumDelivered >= maxDeliver
}

// paraFilaMorta guarda o evento com o motivo da falha e dá ack, para a fila não
// travar. O ack aqui não é "deu certo": é "parou de tentar, e está guardado".
func (c *Consumer) paraFilaMorta(msg jetstream.Msg, causa error) {
	log := c.logger.GetLogger("system")

	publica := c.publicaNaFilaMorta
	if publica == nil {
		publica = func(dados []byte) error {
			if c.js == nil {
				return fmt.Errorf("jetstream indisponivel")
			}

			cabecalho := nats.Header{}
			cabecalho.Set("Chatwoot-Erro", causa.Error())
			cabecalho.Set("Chatwoot-Subject-Original", msg.Subject())
			cabecalho.Set("Chatwoot-Tentativas", fmt.Sprint(maxDeliver))

			ctx, cancel := context.WithTimeout(context.Background(), publishTimeoutDLQ)
			defer cancel()

			_, err := c.js.PublishMsg(ctx, &nats.Msg{
				Subject: subjectDLQ,
				Data:    dados,
				Header:  cabecalho,
			})
			return err
		}
	}

	if err := publica(msg.Data()); err != nil {
		// Não deu para guardar: melhor reentregar do que dar ack e perder.
		log.LogError("Conector Chatwoot: falha ao gravar na fila morta (%v); reentregando", err)
		_ = msg.Nak()
		return
	}

	log.LogError("Conector Chatwoot: evento movido para a fila morta apos %d tentativas: %v",
		maxDeliver, causa)
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
