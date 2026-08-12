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
	CriaMensagemComAnexo(conversaId int, texto, nomeArquivo, mimetype string, conteudo []byte) (*Mensagem, error)
	SourceIdDaInbox(contatoId int) (string, error)
	CriaVinculoInbox(contatoId int) (string, error)
}

// BaixaMidia devolve o conteúdo do anexo de um evento do WhatsApp. Recebe o
// bloco `Message` cru porque só ele carrega as chaves de descriptografia da
// mídia; o download é feito pelo cliente conectado da instância.
type BaixaMidia func(instanceId string, mensagemBruta []byte) ([]byte, error)

// MarcaLida manda o recibo de leitura de volta ao WhatsApp — o segundo tique
// azul do lado do cliente, quando a instância está configurada com markAsRead.
type MarcaLida func(instanceId, chat, remetente, waid string) error

type Entrada struct {
	repo      chatwoot_repository.ChatwootRepository
	fabrica   fabricaCliente
	baixa     BaixaMidia
	marcaLida MarcaLida
}

func NewEntrada(repo chatwoot_repository.ChatwootRepository) *Entrada {
	return &Entrada{
		repo: repo,
		fabrica: func(config *chatwoot_model.ChatwootConfig) chatwootClient {
			return NewClient(config.Url, config.AccountId, config.AccountToken, config.InboxId, config.InboxIdentifier)
		},
	}
}

// ComBaixadorDeMidia liga o download de anexos. Sem ele a entrada continua
// funcionando, só que mídia vira marcador ("[imagem]") em vez do arquivo.
func (e *Entrada) ComBaixadorDeMidia(baixa BaixaMidia) *Entrada {
	e.baixa = baixa
	return e
}

// ComMarcadorDeLida liga o envio do recibo de leitura ao cliente. Só age nas
// instâncias com markAsRead ligado na config.
func (e *Entrada) ComMarcadorDeLida(marca MarcaLida) *Entrada {
	e.marcaLida = marca
	return e
}

// criaMensagem entrega o anexo quando há mídia e cai para texto quando não há.
//
// Falha de download não vira erro: numa agência a foto do hotel importa, mas
// perder a conversa inteira porque um anexo não baixou é pior — a mensagem
// entra com o marcador e o atendente ao menos sabe que algo chegou.
func (e *Entrada) criaMensagem(
	cliente chatwootClient,
	evento *Evento,
	sourceId string,
	conversaId int,
	texto string,
) (*Mensagem, error) {
	midia := evento.TemMidia()
	if !midia.Tem || e.baixa == nil {
		return cliente.CriaMensagem(sourceId, conversaId, texto)
	}

	conteudo, err := e.baixa(evento.InstanceId, evento.Data.Message)
	if err != nil || len(conteudo) == 0 {
		return cliente.CriaMensagem(sourceId, conversaId, texto)
	}

	// A legenda vai no corpo; o marcador é só o texto de fallback de quando não
	// há arquivo, e repeti-lo ao lado do anexo é ruído.
	legenda := texto
	if ehMarcador(legenda) {
		legenda = ""
	}
	return cliente.CriaMensagemComAnexo(conversaId, legenda, midia.Arquivo, midia.Mimetype, conteudo)
}

// resolveSourceId devolve o id que liga o contato a esta inbox, reaproveitando
// o vínculo que já existe.
//
// A API pública de contatos cria um contact_inbox novo a cada chamada, e o
// Chatwoot abre uma conversa por contact_inbox: usá-la para contato existente
// espalha a negociação do cliente em uma conversa por mensagem.
func (e *Entrada) resolveSourceId(cliente chatwootClient, telefone, nome string) (string, error) {
	contato, err := cliente.BuscaContato(telefone)
	if err != nil {
		return "", err
	}

	if contato == nil {
		// Contato inexistente: aí sim a API pública serve, porque ela cria o
		// contato e o vínculo de uma vez.
		novo, err := cliente.CriaContato(telefone, nome)
		if err != nil {
			return "", err
		}
		if novo == nil || novo.SourceId == "" {
			return "", fmt.Errorf("chatwoot devolveu contato sem source_id")
		}
		return novo.SourceId, nil
	}

	sourceId, err := cliente.SourceIdDaInbox(contato.Id)
	if err != nil {
		return "", err
	}
	if sourceId != "" {
		return sourceId, nil
	}

	// Contato existe mas nunca falou por esta inbox.
	sourceId, err = cliente.CriaVinculoInbox(contato.Id)
	if err != nil {
		return "", err
	}
	if sourceId == "" {
		return "", fmt.Errorf("chatwoot nao devolveu source_id ao vincular a inbox")
	}
	return sourceId, nil
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

	sourceId, err := e.resolveSourceId(cliente, telefone, evento.Data.Info.PushName)
	if err != nil {
		return Resultado{}, err
	}

	conversa, err := cliente.ConversaAberta(sourceId)
	if err != nil {
		return Resultado{}, err
	}
	if conversa == nil {
		conversa, err = cliente.CriaConversa(sourceId)
		if err != nil {
			return Resultado{}, err
		}
	}

	mensagem, err := e.criaMensagem(cliente, evento, sourceId, conversa.Id, texto)
	if err != nil {
		return Resultado{}, err
	}

	// A marcação vem depois do envio: marcar antes perderia a mensagem se o
	// Chatwoot falhasse, porque a reentrega veria o WAID como já processado.
	if err := e.repo.MarcaProcessada(chatwoot_model.MensagemProcessada{
		Waid:               waid,
		InstanceId:         evento.InstanceId,
		ChatwootMessageId:  mensagem.Id,
		ChatwootConversaId: conversa.Id,
		Direcao:            "entrada",
	}); err != nil {
		return Resultado{}, err
	}

	// A marcação de lida vem por último e não derruba nada: ela é cortesia ao
	// cliente, e falhar nela não pode desfazer a mensagem já criada na conversa
	// nem provocar reentrega (que duplicaria o trabalho todo).
	if config.MarkAsRead && e.marcaLida != nil {
		// Erro descartado de propósito: quem implementa marcaLida registra a
		// falha, e propagá-la aqui viraria nak — reprocessando uma mensagem que
		// já está na conversa.
		_ = e.marcaLida(evento.InstanceId, evento.Data.Info.Chat,
			evento.Data.Info.Sender, waid)
	}

	return Resultado{ChatwootMessageId: mensagem.Id}, nil
}
