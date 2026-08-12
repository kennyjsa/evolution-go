package chatwoot_service

import (
	"encoding/json"
	"strings"

	chatwoot_model "github.com/evolution-foundation/evolution-go/pkg/chatwoot/model"
	chatwoot_repository "github.com/evolution-foundation/evolution-go/pkg/chatwoot/repository"
)

// EventoRecibo é o recorte do evento `Receipt` publicado pelo evolution-go.
//
// `state` vem do próprio evolution-go ("Delivered", "Read", "ReadSelf"); os
// campos de Data são os do types.Receipt do whatsmeow, serializado sem tags —
// daí os nomes em maiúscula.
type EventoRecibo struct {
	Event      string `json:"event"`
	State      string `json:"state"`
	InstanceId string `json:"instanceId"`
	Data       struct {
		MessageIDs []string `json:"MessageIDs"`
		Chat       string   `json:"Chat"`
		Sender     string   `json:"Sender"`
		Type       string   `json:"Type"`
	} `json:"data"`
}

// statusDoChatwoot traduz o estado do recibo para o enum do Chatwoot.
//
// "ReadSelf" é leitura feita em outro aparelho do próprio número, não pelo
// cliente — marcar como lida ali diria ao agente que o cliente leu quando não
// leu.
func statusDoChatwoot(state string) string {
	switch strings.ToLower(state) {
	case "delivered":
		return "delivered"
	case "read":
		return "read"
	default:
		return ""
	}
}

// clienteStatus é o recorte do cliente usado aqui; separado para o teste rodar
// sem Chatwoot.
type clienteStatus interface {
	AtualizaStatus(conversaId, mensagemId int, status string) error
}

type Recibo struct {
	repo    chatwoot_repository.ChatwootRepository
	fabrica func(config *chatwoot_model.ChatwootConfig) clienteStatus
}

func NewRecibo(repo chatwoot_repository.ChatwootRepository) *Recibo {
	return &Recibo{
		repo: repo,
		fabrica: func(config *chatwoot_model.ChatwootConfig) clienteStatus {
			return NewClient(config.Url, config.AccountId, config.AccountToken, config.InboxId, config.InboxIdentifier)
		},
	}
}

// Processa leva o recibo do WhatsApp para o status da mensagem no Chatwoot, de
// modo que o agente veja se o cliente recebeu e leu o que ele mandou.
//
// Como no resto do conector, erro é só o que vale reentregar; recibo de
// mensagem que não passou por aqui volta como Ignorado.
func (r *Recibo) Processa(bruto []byte) (Resultado, error) {
	var evento EventoRecibo
	if err := json.Unmarshal(bruto, &evento); err != nil {
		return Resultado{Ignorado: true, Motivo: "payload de recibo ilegível"}, nil
	}

	status := statusDoChatwoot(evento.State)
	if status == "" {
		return Resultado{Ignorado: true, Motivo: "recibo fora do escopo"}, nil
	}
	if len(evento.Data.MessageIDs) == 0 {
		return Resultado{Ignorado: true, Motivo: "recibo sem waid"}, nil
	}

	config, err := r.repo.GetConfig(evento.InstanceId)
	if err != nil {
		return Resultado{}, err
	}
	if config == nil || !config.Enabled {
		return Resultado{Ignorado: true, Motivo: "chatwoot desabilitado na instância"}, nil
	}

	var cliente clienteStatus
	var atualizadas int

	for _, waid := range evento.Data.MessageIDs {
		processada, err := r.repo.BuscaProcessadaPorWaid(waid)
		if err != nil {
			return Resultado{}, err
		}
		// Só mensagem que o agente mandou tem status a mostrar no Chatwoot; o
		// recibo da mensagem do cliente não descreve nada que ele veja.
		if processada == nil || processada.Direcao != "saida" ||
			processada.ChatwootMessageId == 0 || processada.ChatwootConversaId == 0 {
			continue
		}

		if cliente == nil {
			cliente = r.fabrica(config)
		}
		if err := cliente.AtualizaStatus(
			processada.ChatwootConversaId, processada.ChatwootMessageId, status); err != nil {
			return Resultado{}, err
		}
		atualizadas++
	}

	if atualizadas == 0 {
		return Resultado{Ignorado: true, Motivo: "nenhuma mensagem do agente no recibo"}, nil
	}
	return Resultado{}, nil
}
