package chatwoot_model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ChatwootConfig guarda a ligação de uma instância com uma inbox do Chatwoot.
// Uma instância tem no máximo uma config — daí InstanceId ser a chave.
type ChatwootConfig struct {
	InstanceId string `json:"instanceId" gorm:"type:uuid;primaryKey"`
	Enabled    bool   `json:"enabled" gorm:"default:false"`

	Url          string `json:"url"`
	AccountId    string `json:"accountId"`
	AccountToken string `json:"-"`

	// A inbox do Chatwoot é do tipo API: o Identifier é o segredo que autentica
	// a criação de mensagem, e o Id serve para as chamadas administrativas.
	InboxId         string `json:"inboxId"`
	InboxIdentifier string `json:"-"`

	// Segredo da rota de entrada do Chatwoot
	// (`/webhooks/chatwoot/:instanceName/:token`). Fica na URL porque o webhook
	// de conta do Chatwoot não permite header customizado.
	WebhookToken string `json:"-"`
	// Quando preenchido, o corpo do webhook é validado por HMAC-SHA256 — sem
	// isso, quem descobrir a URL consegue injetar mensagem.
	HmacToken string `json:"-"`

	// Marca no WhatsApp como lida a mensagem que o agente já respondeu.
	MarkAsRead bool `json:"markAsRead" gorm:"default:false"`
	SyncGroups bool `json:"syncGroups" gorm:"default:false"`

	CreatedAt time.Time `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updatedAt" gorm:"autoUpdateTime"`
}

func (ChatwootConfig) TableName() string {
	return "chatwoot_configs"
}

func (m *ChatwootConfig) BeforeCreate(tx *gorm.DB) (err error) {
	if m.WebhookToken == "" {
		m.WebhookToken = uuid.New().String()
	}
	return
}

// MensagemProcessada dá a idempotência do conector: o WhatsApp reentrega
// mensagem, e o JetStream reentrega evento cujo ack se perdeu. Sem esta tabela,
// as duas coisas viram mensagem duplicada na conversa do agente.
type MensagemProcessada struct {
	Waid       string `gorm:"primaryKey"`
	InstanceId string `gorm:"type:uuid;index"`
	// Id da mensagem no Chatwoot. Permite ligar o recibo de leitura do WhatsApp
	// à mensagem correspondente, e vice-versa.
	ChatwootMessageId int
	// Conversa a que a mensagem pertence. O endpoint de status do Chatwoot é
	// aninhado na conversa, então sem isto o recibo de leitura não teria como
	// localizar a mensagem para atualizar.
	ChatwootConversaId int
	Direcao            string
	CriadoEm           time.Time `gorm:"autoCreateTime"`
}

func (MensagemProcessada) TableName() string {
	return "chatwoot_mensagens_processadas"
}

// MapaLid guarda a correspondência LID -> telefone.
//
// Em conta migrada para LID o WhatsApp identifica o remetente por um `@lid`, e
// o telefone só aparece quando a outra ponta o revela. Sem persistir esse par,
// o contato do Chatwoot seria criado com o LID no lugar do telefone — que é
// exatamente o defeito da Evolution Node que motivou este conector.
type MapaLid struct {
	Lid      string `gorm:"primaryKey"`
	Telefone string `gorm:"index"`
	VistoEm  time.Time
}

func (MapaLid) TableName() string {
	return "chatwoot_mapa_lid"
}
