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
	Event       string `json:"event"`
	MessageType string `json:"message_type"`
	Content     string `json:"content"`
	Private     bool   `json:"private"`
	// is_private marca a digitação numa nota interna: o cliente não pode ver
	// "digitando" enquanto o time conversa entre si.
	IsPrivate   bool   `json:"is_private"`
	Id          int    `json:"id"`
	SourceId    string `json:"source_id"`
	Attachments []struct {
		// file_type é a classificação do Chatwoot: image, audio, video, file.
		FileType string `json:"file_type"`
		DataUrl  string `json:"data_url"`
	} `json:"attachments"`
	Sender struct {
		Name string `json:"name"`
		// available_name é o nome de exibição do agente; quando o time usa
		// apelido no atendimento, é ele que o cliente reconhece.
		AvailableName string `json:"available_name"`
		Type          string `json:"type"`
	} `json:"sender"`
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

// EnviaMidia manda um anexo pelo WhatsApp. `tipo` é o do evolution-go
// (image, video, audio, document).
type EnviaMidia func(instanceId, numero, tipo, legenda, arquivo string, conteudo []byte) (waid string, err error)

// BaixaAnexo busca o arquivo que o agente anexou no Chatwoot e devolve o
// conteúdo e o nome do arquivo.
type BaixaAnexo func(url string) (conteudo []byte, arquivo string, err error)

// EnviaPresenca reproduz no WhatsApp o "digitando" do agente. `estado` é
// composing ou paused.
type EnviaPresenca func(instanceId, numero, estado string) error

type Saida struct {
	repo          chatwoot_repository.ChatwootRepository
	envia         EnviaTexto
	enviaMidia    EnviaMidia
	baixa         BaixaAnexo
	enviaPresenca EnviaPresenca
}

func NewSaida(repo chatwoot_repository.ChatwootRepository, envia EnviaTexto) *Saida {
	return &Saida{repo: repo, envia: envia}
}

// ComMidia liga o envio de anexos. Sem isso a saída só manda texto, e o arquivo
// que o agente anexou no Chatwoot nunca chega ao cliente.
func (s *Saida) ComMidia(baixa BaixaAnexo, envia EnviaMidia) *Saida {
	s.baixa = baixa
	s.enviaMidia = envia
	return s
}

// ComPresenca liga o "digitando". Sem isso o cliente fica sem sinal nenhum
// enquanto o agente escreve, o que numa negociação parece abandono.
func (s *Saida) ComPresenca(envia EnviaPresenca) *Saida {
	s.enviaPresenca = envia
	return s
}

// presenca reproduz o "digitando" do agente no WhatsApp.
//
// Falha aqui nunca vira erro: presença é enfeite, e um 500 faria o Chatwoot
// reentregar um evento efêmero que já passou.
func (s *Saida) presenca(config *chatwoot_model.ChatwootConfig, hook *WebhookChatwoot) (Resultado, error) {
	if s.enviaPresenca == nil {
		return Resultado{Ignorado: true, Motivo: "presença não configurada"}, nil
	}
	if hook.IsPrivate {
		return Resultado{Ignorado: true, Motivo: "digitação em nota privada"}, nil
	}

	numero := telefoneDaConversa(hook)
	if numero == "" {
		return Resultado{Ignorado: true, Motivo: "conversa sem telefone do contato"}, nil
	}

	estado := "paused"
	if hook.Event == "conversation_typing_on" {
		estado = "composing"
	}

	if err := s.enviaPresenca(config.InstanceId, numero, estado); err != nil {
		return Resultado{Ignorado: true, Motivo: "falha ao enviar presença: " + err.Error()}, nil
	}
	return Resultado{Ignorado: true, Motivo: "presença " + estado + " enviada"}, nil
}

// telefoneDaConversa tira o telefone do contato do payload, em qualquer um dos
// dois campos em que o Chatwoot o entrega.
func telefoneDaConversa(hook *WebhookChatwoot) string {
	numero := hook.Conversation.Meta.Sender.Identifier
	if numero == "" {
		numero = hook.Conversation.Meta.Sender.PhoneNumber
	}
	// O identifier criado pela Evolution Node vem como JID completo.
	if i := strings.Index(numero, "@"); i > 0 {
		numero = numero[:i]
	}
	return strings.TrimPrefix(strings.TrimSpace(numero), "+")
}

// tipoDoChatwoot traduz o file_type do Chatwoot para o tipo do evolution-go.
// O que não é reconhecido vai como documento: entregar o arquivo com o tipo
// genérico é melhor do que não entregar.
func tipoDoChatwoot(fileType string) string {
	switch strings.ToLower(fileType) {
	case "image":
		return "image"
	case "audio":
		return "audio"
	case "video":
		return "video"
	default:
		return "document"
	}
}

// assina prefixa o texto com o nome de quem respondeu, como o Evolution faz.
//
// O negrito é o mesmo do WhatsApp (*nome*): numa inbox compartilhada o cliente
// fala com várias pessoas da agência, e sem a assinatura todas viram um
// interlocutor só.
func assina(config *chatwoot_model.ChatwootConfig, hook *WebhookChatwoot, texto string) string {
	if !config.SignMsg || texto == "" {
		return texto
	}

	nome := strings.TrimSpace(hook.Sender.AvailableName)
	if nome == "" {
		nome = strings.TrimSpace(hook.Sender.Name)
	}
	// Sem nome não há o que assinar — e "**: texto" seria pior que texto puro.
	if nome == "" {
		return texto
	}

	delimitador := config.SignDelimiter
	if delimitador == "" {
		delimitador = "\n"
	}
	return "*" + nome + "*" + delimitador + texto
}

// entrega manda texto, anexos, ou os dois, e devolve o WAID da última mensagem
// enviada — é ele que fecha o par contra a reentrega do webhook.
//
// Falha de anexo é erro de verdade, não fallback silencioso: um "segue a foto"
// entregue sem a foto é pior para o cliente do que uma reentrega.
func (s *Saida) entrega(
	instanceId, numero, texto string,
	temAnexo bool,
	hook *WebhookChatwoot,
) (string, error) {
	if !temAnexo {
		waid, err := s.envia(instanceId, numero, texto)
		if err != nil {
			return "", fmt.Errorf("falha ao enviar para o WhatsApp: %w", err)
		}
		return waid, nil
	}

	var ultimoWaid string
	for i, anexo := range hook.Attachments {
		if anexo.DataUrl == "" {
			continue
		}

		conteudo, arquivo, err := s.baixa(anexo.DataUrl)
		if err != nil {
			return "", fmt.Errorf("falha ao baixar anexo do Chatwoot: %w", err)
		}

		// A legenda acompanha só o primeiro anexo: repeti-la em cada arquivo
		// mandaria o mesmo texto várias vezes para o cliente.
		legenda := ""
		if i == 0 {
			legenda = texto
		}

		waid, err := s.enviaMidia(instanceId, numero, tipoDoChatwoot(anexo.FileType),
			legenda, arquivo, conteudo)
		if err != nil {
			return "", fmt.Errorf("falha ao enviar anexo para o WhatsApp: %w", err)
		}
		ultimoWaid = waid
	}

	// Anexo declarado mas sem URL utilizável: manda ao menos o texto, se houver.
	if ultimoWaid == "" && texto != "" {
		waid, err := s.envia(instanceId, numero, texto)
		if err != nil {
			return "", fmt.Errorf("falha ao enviar para o WhatsApp: %w", err)
		}
		return waid, nil
	}
	return ultimoWaid, nil
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
	if hook.Event == "conversation_typing_on" || hook.Event == "conversation_typing_off" {
		return s.presenca(config, hook)
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
	// Anexo sem legenda é o caso normal de foto de hotel e áudio de negociação;
	// exigir texto descartaria justamente essas mensagens.
	temAnexo := len(hook.Attachments) > 0 && s.enviaMidia != nil && s.baixa != nil
	if texto == "" && !temAnexo {
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

	numero := telefoneDaConversa(hook)
	if numero == "" {
		return Resultado{Ignorado: true, Motivo: "conversa sem telefone do contato"}, nil
	}

	waid, err := s.entrega(config.InstanceId, numero, assina(config, hook, texto), temAnexo, hook)
	if err != nil {
		return Resultado{}, err
	}

	// Marcar depois do envio: marcar antes perderia a mensagem se o envio
	// falhasse, porque a reentrega a veria como já enviada.
	if waid != "" {
		if err := s.repo.MarcaProcessada(chatwoot_model.MensagemProcessada{
			Waid:       waid,
			InstanceId: config.InstanceId,
			// A conversa é gravada junto porque o recibo de leitura precisa dela
			// para achar a mensagem: o endpoint de status é aninhado na conversa.
			ChatwootMessageId:  hook.Id,
			ChatwootConversaId: hook.Conversation.Id,
			Direcao:            "saida",
		}); err != nil {
			return Resultado{}, err
		}
	}

	return Resultado{ChatwootMessageId: hook.Id}, nil
}
