package chatwoot_repository

import (
	"time"

	chatwoot_model "github.com/evolution-foundation/evolution-go/pkg/chatwoot/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ChatwootRepository interface {
	GetConfig(instanceId string) (*chatwoot_model.ChatwootConfig, error)
	GetConfigByWebhookToken(token string) (*chatwoot_model.ChatwootConfig, error)
	UpsertConfig(config chatwoot_model.ChatwootConfig) error
	DeleteConfig(instanceId string) error

	MarcaProcessada(msg chatwoot_model.MensagemProcessada) error
	BuscaProcessadaPorWaid(waid string) (*chatwoot_model.MensagemProcessada, error)
	BuscaProcessadaPorChatwootId(chatwootMessageId int) (*chatwoot_model.MensagemProcessada, error)

	RegistraLid(lid, telefone string) error
	TelefoneDoLid(lid string) (string, error)
}

type chatwootRepository struct {
	db *gorm.DB
}

func NewChatwootRepository(db *gorm.DB) ChatwootRepository {
	return &chatwootRepository{db: db}
}

func (r *chatwootRepository) GetConfig(instanceId string) (*chatwoot_model.ChatwootConfig, error) {
	var config chatwoot_model.ChatwootConfig
	err := r.db.Where("instance_id = ?", instanceId).First(&config).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &config, nil
}

func (r *chatwootRepository) GetConfigByWebhookToken(token string) (*chatwoot_model.ChatwootConfig, error) {
	// Token vazio casaria com toda config que ainda não gerou o seu — o que
	// transformaria a rota pública num acesso livre.
	if token == "" {
		return nil, nil
	}

	var config chatwoot_model.ChatwootConfig
	err := r.db.Where("webhook_token = ?", token).First(&config).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &config, nil
}

func (r *chatwootRepository) UpsertConfig(config chatwoot_model.ChatwootConfig) error {
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "instance_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"enabled", "url", "account_id", "account_token",
			"inbox_id", "inbox_identifier", "hmac_token",
			"mark_as_read", "sync_groups", "updated_at",
		}),
	}).Create(&config).Error
}

func (r *chatwootRepository) DeleteConfig(instanceId string) error {
	return r.db.Where("instance_id = ?", instanceId).
		Delete(&chatwoot_model.ChatwootConfig{}).Error
}

func (r *chatwootRepository) MarcaProcessada(msg chatwoot_model.MensagemProcessada) error {
	// DoNothing e não DoUpdates: quem chegou primeiro é a entrega válida, e a
	// segunda passagem pelo mesmo WAID é justamente a duplicata a ignorar.
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "waid"}},
		DoNothing: true,
	}).Create(&msg).Error
}

func (r *chatwootRepository) BuscaProcessadaPorWaid(waid string) (*chatwoot_model.MensagemProcessada, error) {
	var msg chatwoot_model.MensagemProcessada
	err := r.db.Where("waid = ?", waid).First(&msg).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &msg, nil
}

func (r *chatwootRepository) BuscaProcessadaPorChatwootId(chatwootMessageId int) (*chatwoot_model.MensagemProcessada, error) {
	var msg chatwoot_model.MensagemProcessada
	err := r.db.Where("chatwoot_message_id = ?", chatwootMessageId).
		Order("criado_em desc").First(&msg).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &msg, nil
}

func (r *chatwootRepository) RegistraLid(lid, telefone string) error {
	if lid == "" || telefone == "" {
		return nil
	}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "lid"}},
		DoUpdates: clause.AssignmentColumns([]string{"telefone", "visto_em"}),
	}).Create(&chatwoot_model.MapaLid{
		Lid:      lid,
		Telefone: telefone,
		VistoEm:  time.Now(),
	}).Error
}

func (r *chatwootRepository) TelefoneDoLid(lid string) (string, error) {
	if lid == "" {
		return "", nil
	}

	var mapa chatwoot_model.MapaLid
	err := r.db.Where("lid = ?", lid).First(&mapa).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", nil
		}
		return "", err
	}
	return mapa.Telefone, nil
}
