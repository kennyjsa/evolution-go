package chatwoot_service

import (
	"fmt"
	"strings"

	chatwoot_model "github.com/evolution-foundation/evolution-go/pkg/chatwoot/model"
	chatwoot_repository "github.com/evolution-foundation/evolution-go/pkg/chatwoot/repository"
)

// WebhookChatwoot é o recorte do payload que o Chatwoot manda na inbox de API.
// Só os campos usados são declarados: o payload completo é grande e muda entre
// versões, e ler o que não usamos só criaria acoplamento.
type WebhookChatwoot struct {
	Event        string `json:"event"`
	MessageType  string `json:"message_type"`
	Content      string `json:"content"`
	Private      bool   `json:"private"`
	Id           int    `json:"id"`
	SourceId     string `json:"source_id"`
	Conversation struct {
		Id   int `json:"id"`
		Meta struct {
			Sender struct {
				Identifier  string `json:"identifier"`
				PhoneNumber string `json:"phone_number"`
			} `json:"sender"`
		} `json:"meta"`
	} `json:"conversation"`
}

// EnviaTexto é o que a saída precisa do serviço de envio do WhatsApp.
type EnviaTexto func(instanceId, numero, texto string) (waid string, err error)

type Saida struct {
	repo  chatwoot_repository.ChatwootRepository
	envia EnviaTexto
}

func NewSaida(repo chatwoot_repository.ChatwootRepository, envia EnviaTexto) *Saida {
	return &Saida{repo: repo, envia: envia}
}

// Processa manda para o WhatsApp o que o agente escreveu no Chatwoot.
//
// Como na entrada, erro é só o que vale repetir. O resto — evento de outro
// tipo, nota privada, eco da própria mensagem que a entrada criou — volta como
// Ignorado, porque reenviar não muda o resultado e o Chatwoot reentrega webhook.
func (s *Saida) Processa(config *chatwoot_model.ChatwootConfig, hook *WebhookChatwoot) (Resultado, error) {
	if config == nil || !config.Enabled {
		return Resultado{Ignorado: true, Motivo: "chatwoot desabilitado na instância"}, nil
	}
	if hook.Event != "message_created" {
		return Resultado{Ignorado: true, Motivo: "evento fora do escopo"}, nil
	}
	// `incoming` é o eco da mensagem que a própria entrada criou; reenviá-la ao
	// WhatsApp devolveria ao cliente o que ele acabou de mandar.
	if hook.MessageType != "outgoing" {
		return Resultado{Ignorado: true, Motivo: "mensagem não é do agente"}, nil
	}
	if hook.Private {
		return Resultado{Ignorado: true, Motivo: "nota privada"}, nil
	}

	texto := strings.TrimSpace(hook.Content)
	if texto == "" {
		return Resultado{Ignorado: true, Motivo: "mensagem sem texto"}, nil
	}

	// O Chatwoot reentrega o webhook quando a resposta demora; sem esta guarda
	// o cliente receberia a mesma mensagem duas vezes.
	if hook.Id != 0 {
		processada, err := s.repo.BuscaProcessadaPorChatwootId(hook.Id)
		if err != nil {
			return Resultado{}, err
		}
		if processada != nil {
			return Resultado{Ignorado: true, Motivo: "mensagem já enviada",
				ChatwootMessageId: hook.Id}, nil
		}
	}

	numero := hook.Conversation.Meta.Sender.Identifier
	if numero == "" {
		numero = hook.Conversation.Meta.Sender.PhoneNumber
	}
	numero = strings.TrimPrefix(strings.TrimSpace(numero), "+")
	if numero == "" {
		return Resultado{Ignorado: true, Motivo: "conversa sem telefone do contato"}, nil
	}

	waid, err := s.envia(config.InstanceId, numero, texto)
	if err != nil {
		return Resultado{}, fmt.Errorf("falha ao enviar para o WhatsApp: %w", err)
	}

	// Marcar depois do envio: marcar antes perderia a mensagem se o envio
	// falhasse, porque a reentrega a veria como já enviada.
	if waid != "" {
		if err := s.repo.MarcaProcessada(chatwoot_model.MensagemProcessada{
			Waid:              waid,
			InstanceId:        config.InstanceId,
			ChatwootMessageId: hook.Id,
			Direcao:           "saida",
		}); err != nil {
			return Resultado{}, err
		}
	}

	return Resultado{ChatwootMessageId: hook.Id}, nil
}
