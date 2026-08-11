package chatwoot_service

import (
	"fmt"

	chatwoot_model "github.com/evolution-foundation/evolution-go/pkg/chatwoot/model"
	chatwoot_repository "github.com/evolution-foundation/evolution-go/pkg/chatwoot/repository"
)

// Resultado descreve o que a entrada fez com o evento, para o chamador decidir
// entre dar ack e reentregar.
type Resultado struct {
	Ignorado          bool
	Motivo            string
	ChatwootMessageId int
}

// fabricaCliente existe para o teste trocar o cliente sem subir Chatwoot.
type fabricaCliente func(config *chatwoot_model.ChatwootConfig) chatwootClient

type chatwootClient interface {
	BuscaContato(identifier string) (*Contato, error)
	CriaContato(identifier, nome string) (*Contato, error)
	ConversaAberta(sourceId string) (*Conversa, error)
	CriaConversa(sourceId string) (*Conversa, error)
	CriaMensagem(sourceId string, conversaId int, texto string) (*Mensagem, error)
}

type Entrada struct {
	repo    chatwoot_repository.ChatwootRepository
	fabrica fabricaCliente
}

func NewEntrada(repo chatwoot_repository.ChatwootRepository) *Entrada {
	return &Entrada{
		repo: repo,
		fabrica: func(config *chatwoot_model.ChatwootConfig) chatwootClient {
			return NewClient(config.Url, config.AccountId, config.AccountToken, config.InboxIdentifier)
		},
	}
}

// Processa leva uma mensagem recebida do WhatsApp para a conversa do Chatwoot.
//
// Devolve erro só no que vale reentregar (Chatwoot fora, banco fora). O que não
// tem conserto em retentativa — evento de outro tipo, remetente sem telefone
// conhecido, mensagem já processada — volta como Ignorado, para o consumidor
// dar ack e não travar o stream numa mensagem que nunca vai passar.
func (e *Entrada) Processa(evento *Evento) (Resultado, error) {
	if evento.Event != "Message" || evento.Data.Info.IsFromMe {
		return Resultado{Ignorado: true, Motivo: "evento fora do escopo"}, nil
	}
	if evento.EhStatus() {
		return Resultado{Ignorado: true, Motivo: "status/broadcast"}, nil
	}

	config, err := e.repo.GetConfig(evento.InstanceId)
	if err != nil {
		return Resultado{}, err
	}
	if config == nil || !config.Enabled {
		return Resultado{Ignorado: true, Motivo: "chatwoot desabilitado na instância"}, nil
	}
	if evento.EhGrupo() && !config.SyncGroups {
		return Resultado{Ignorado: true, Motivo: "grupo"}, nil
	}

	waid := evento.Data.Info.ID
	if waid == "" {
		return Resultado{Ignorado: true, Motivo: "evento sem waid"}, nil
	}
	// A checagem antes do trabalho evita mensagem repetida na conversa quando o
	// JetStream reentrega um evento cujo ack se perdeu.
	processada, err := e.repo.BuscaProcessadaPorWaid(waid)
	if err != nil {
		return Resultado{}, err
	}
	if processada != nil {
		return Resultado{Ignorado: true, Motivo: "waid já processado",
			ChatwootMessageId: processada.ChatwootMessageId}, nil
	}

	texto := evento.Texto()
	if texto == "" {
		return Resultado{Ignorado: true, Motivo: "mensagem sem conteúdo legível"}, nil
	}

	// Registrar o par antes de resolver deixa a próxima mensagem só-LID desse
	// contato já resolvível — é assim que o mapa se popula sozinho.
	telefoneDoEvento, lid := Identifica(evento.InfoIdentidade())
	if lid != "" && telefoneDoEvento != "" {
		if err := e.repo.RegistraLid(lid, telefoneDoEvento); err != nil {
			return Resultado{}, err
		}
	}

	telefone, err := ResolveTelefone(evento.InfoIdentidade(), e.repo.TelefoneDoLid)
	if err != nil {
		return Resultado{}, err
	}
	if telefone == "" {
		// Sem telefone não dá para criar contato certo, e criar com o LID no
		// lugar dele é exatamente o defeito que este conector evita.
		return Resultado{Ignorado: true, Motivo: "remetente só com LID desconhecido"}, nil
	}

	cliente := e.fabrica(config)

	contato, err := cliente.BuscaContato(telefone)
	if err != nil {
		return Resultado{}, err
	}
	if contato == nil {
		contato, err = cliente.CriaContato(telefone, evento.Data.Info.PushName)
		if err != nil {
			return Resultado{}, err
		}
	}
	if contato == nil || contato.SourceId == "" {
		return Resultado{}, fmt.Errorf("chatwoot devolveu contato sem source_id")
	}

	conversa, err := cliente.ConversaAberta(contato.SourceId)
	if err != nil {
		return Resultado{}, err
	}
	if conversa == nil {
		conversa, err = cliente.CriaConversa(contato.SourceId)
		if err != nil {
			return Resultado{}, err
		}
	}

	mensagem, err := cliente.CriaMensagem(contato.SourceId, conversa.Id, texto)
	if err != nil {
		return Resultado{}, err
	}

	// A marcação vem depois do envio: marcar antes perderia a mensagem se o
	// Chatwoot falhasse, porque a reentrega veria o WAID como já processado.
	if err := e.repo.MarcaProcessada(chatwoot_model.MensagemProcessada{
		Waid:              waid,
		InstanceId:        evento.InstanceId,
		ChatwootMessageId: mensagem.Id,
		Direcao:           "entrada",
	}); err != nil {
		return Resultado{}, err
	}

	return Resultado{ChatwootMessageId: mensagem.Id}, nil
}
