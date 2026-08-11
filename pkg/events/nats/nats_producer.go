package nats_producer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	producer_interfaces "github.com/evolution-foundation/evolution-go/pkg/events/interfaces"
	logger_wrapper "github.com/evolution-foundation/evolution-go/pkg/logger"
	"github.com/gomessguii/logger"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Mesmo mapa usado pelo producer do RabbitMQ: evento global -> subjects.
// Mantido aqui para declarar o stream com a lista exata de subjects, em vez de
// um wildcard que capturaria subjects de outros sistemas no mesmo servidor.
var eventMap = map[string][]string{
	"MESSAGE":       {"message"},
	"SEND_MESSAGE":  {"sendmessage"},
	"READ_RECEIPT":  {"receipt"},
	"PRESENCE":      {"presence"},
	"HISTORY_SYNC":  {"historysync"},
	"CHAT_PRESENCE": {"chatpresence", "archive"},
	"CALL":          {"calloffer", "callaccept", "callterminate", "calloffernotice", "callrelaylatency"},
	"CONNECTION":    {"connected", "pairsuccess", "temporaryban", "loggedout", "connectfailure", "disconnected"},
	"LABEL":         {"labeledit", "labelassociationchat", "labelassociationmessage"},
	"CONTACT":       {"contact", "pushname"},
	"GROUP":         {"groupinfo", "joinedgroup"},
	"NEWSLETTER":    {"newsletterjoin", "newsletterleave"},
	"QRCODE":        {"qrcode", "qrtimeout", "qrsuccess"},
}

const (
	maxRetries     = 3
	publishTimeout = 10 * time.Second
)

type natsProducer struct {
	conn              *nats.Conn
	js                jetstream.JetStream
	url               string
	jetStreamEnabled  bool
	streamName        string
	streamMaxAge      time.Duration
	natsGlobalEnabled bool
	natsGlobalEvents  []string
	loggerWrapper     *logger_wrapper.LoggerManager
}

func NewNatsProducer(
	url string,
	natsGlobalEnabled bool,
	natsGlobalEvents []string,
	jetStreamEnabled bool,
	streamName string,
	streamMaxAge time.Duration,
	loggerWrapper *logger_wrapper.LoggerManager,
) producer_interfaces.Producer {
	p := &natsProducer{
		url:               url,
		jetStreamEnabled:  jetStreamEnabled,
		streamName:        streamName,
		streamMaxAge:      streamMaxAge,
		natsGlobalEnabled: natsGlobalEnabled,
		natsGlobalEvents:  natsGlobalEvents,
		loggerWrapper:     loggerWrapper,
	}

	if url == "" {
		return p
	}

	// Falha de conexão no boot não pode matar o producer para sempre: sem isso,
	// um NATS que sobe depois da aplicação deixa o producer inerte até o
	// próximo restart. `ensureConnection` refaz a conexão sob demanda, como o
	// producer do RabbitMQ já faz.
	if err := p.ensureConnection(); err != nil {
		logger.LogError("Failed to connect to NATS: %v", err)
	}

	return p
}

func (p *natsProducer) ensureConnection() error {
	if p.url == "" {
		return fmt.Errorf("NATS URL is empty - check NATS_URL configuration")
	}
	if p.conn != nil && p.conn.IsConnected() {
		return nil
	}

	conn, err := nats.Connect(p.url,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			logger.LogWarn("NATS disconnected: %v", err)
		}),
		nats.ReconnectHandler(func(c *nats.Conn) {
			logger.LogInfo("NATS reconnected to %s", c.ConnectedUrl())
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS: %v", err)
	}
	p.conn = conn

	if !p.jetStreamEnabled {
		return nil
	}

	js, err := jetstream.New(conn)
	if err != nil {
		return fmt.Errorf("failed to create JetStream context: %v", err)
	}
	p.js = js
	return nil
}

// subjectsDoStream monta a lista exata de subjects publicados: o nome cru do
// evento (publicação global) e `*.<evento>` (publicação por instância, cujo
// subject é `<instanceId>.<evento>`).
func (p *natsProducer) subjectsDoStream() []string {
	var subjects []string
	vistos := map[string]bool{}

	adiciona := func(s string) {
		if !vistos[s] {
			vistos[s] = true
			subjects = append(subjects, s)
		}
	}

	eventos := p.natsGlobalEvents
	if len(eventos) == 0 {
		// Sem lista explícita, cobre todo o catálogo — um stream que não cobre
		// o subject publicado faz o publish falhar com "no responders".
		for evento := range eventMap {
			eventos = append(eventos, evento)
		}
	}

	for _, evento := range eventos {
		for _, subject := range eventMap[strings.ToUpper(evento)] {
			adiciona(subject)
			adiciona("*." + subject)
		}
	}
	return subjects
}

func (p *natsProducer) publish(subject string, payload []byte, userID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), publishTimeout)
	defer cancel()

	if !p.jetStreamEnabled {
		return p.conn.Publish(subject, payload)
	}

	// Msg-Id dá dedupe no servidor dentro da janela do stream: se o ack se
	// perder e a retentativa reenviar o mesmo evento, o JetStream descarta a
	// cópia em vez de entregar duas vezes ao consumidor.
	soma := sha256.Sum256(payload)
	msgID := hex.EncodeToString(soma[:])

	var err error
	for tentativa := 1; tentativa <= maxRetries; tentativa++ {
		_, err = p.js.Publish(ctx, subject, payload, jetstream.WithMsgID(msgID))
		if err == nil {
			return nil
		}
		p.loggerWrapper.GetLogger(userID).LogWarn(
			"[%s] Falha ao publicar no subject %s (tentativa %d/%d): %v",
			userID, subject, tentativa, maxRetries, err)

		if tentativa < maxRetries {
			time.Sleep(time.Second * time.Duration(tentativa))
			if reconErr := p.ensureConnection(); reconErr != nil {
				p.loggerWrapper.GetLogger(userID).LogError(
					"[%s] Falha ao reconectar ao NATS: %v", userID, reconErr)
			}
		}
	}
	return err
}

func (p *natsProducer) Produce(
	queueName string,
	payload []byte,
	natsEnable string,
	userID string,
) error {
	if natsEnable != "global" && natsEnable != "enabled" {
		return nil
	}

	if err := p.ensureConnection(); err != nil {
		p.loggerWrapper.GetLogger(userID).LogError(
			"[%s] NATS indisponível: %v", userID, err)
		return err
	}

	if err := p.publish(queueName, payload, userID); err != nil {
		p.loggerWrapper.GetLogger(userID).LogError(
			"[%s] Failed to publish message to subject %s: %v", userID, queueName, err)
		return err
	}

	p.loggerWrapper.GetLogger(userID).LogInfo(
		"[%s] Message published successfully to subject: %s", userID, queueName)
	return nil
}

// CreateGlobalQueues declara o stream do JetStream. Em NATS core os subjects
// são criados sozinhos e não há o que fazer aqui — mas aí a mensagem também não
// é persistida: se ninguém estiver escutando no instante do publish, ela some.
func (p *natsProducer) CreateGlobalQueues() error {
	if p.url == "" || !p.jetStreamEnabled {
		return nil
	}

	if err := p.ensureConnection(); err != nil {
		return err
	}

	subjects := p.subjectsDoStream()
	if len(subjects) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), publishTimeout)
	defer cancel()

	_, err := p.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:        p.streamName,
		Subjects:    subjects,
		Storage:     jetstream.FileStorage,
		Retention:   jetstream.LimitsPolicy,
		Discard:     jetstream.DiscardOld,
		MaxAge:      p.streamMaxAge,
		Duplicates:  5 * time.Minute,
		Description: "Eventos do evolution-go",
	})
	if err != nil {
		return fmt.Errorf("failed to create JetStream stream %s: %v", p.streamName, err)
	}

	p.loggerWrapper.GetLogger("system").LogInfo(
		"JetStream stream %s pronto com %d subject(s)", p.streamName, len(subjects))
	return nil
}
