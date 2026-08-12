package chatwoot_service

import (
	"encoding/json"
	"errors"
	"testing"

	chatwoot_model "github.com/evolution-foundation/evolution-go/pkg/chatwoot/model"
)

type repoFalso struct {
	config     *chatwoot_model.ChatwootConfig
	processada *chatwoot_model.MensagemProcessada
	lids       map[string]string
	marcadas   []chatwoot_model.MensagemProcessada
	erroConfig error
}

func novoRepo() *repoFalso {
	return &repoFalso{
		config: &chatwoot_model.ChatwootConfig{
			InstanceId: "inst-1", Enabled: true, Url: "http://chatwoot",
			AccountId: "7", AccountToken: "tok", InboxIdentifier: "ident",
		},
		lids: map[string]string{},
	}
}

func (r *repoFalso) GetConfig(string) (*chatwoot_model.ChatwootConfig, error) {
	return r.config, r.erroConfig
}
func (r *repoFalso) GetConfigByWebhookToken(string) (*chatwoot_model.ChatwootConfig, error) {
	return r.config, nil
}
func (r *repoFalso) UpsertConfig(chatwoot_model.ChatwootConfig) error { return nil }
func (r *repoFalso) DeleteConfig(string) error                        { return nil }
func (r *repoFalso) MarcaProcessada(m chatwoot_model.MensagemProcessada) error {
	r.marcadas = append(r.marcadas, m)
	return nil
}
func (r *repoFalso) BuscaProcessadaPorWaid(string) (*chatwoot_model.MensagemProcessada, error) {
	return r.processada, nil
}
func (r *repoFalso) BuscaProcessadaPorChatwootId(int) (*chatwoot_model.MensagemProcessada, error) {
	return r.processada, nil
}
func (r *repoFalso) RegistraLid(lid, telefone string) error {
	r.lids[lid] = telefone
	return nil
}
func (r *repoFalso) TelefoneDoLid(lid string) (string, error) { return r.lids[lid], nil }

type clienteFalso struct {
	contato       *Contato
	conversa      *Conversa
	criouContato  bool
	criouConversa bool
	textos        []string
	anexos        []anexo
	sourceId      string
	criouVinculo  bool
	erroMensagem  error
}

func (c *clienteFalso) BuscaContato(string) (*Contato, error) { return c.contato, nil }
func (c *clienteFalso) CriaContato(identifier, nome string) (*Contato, error) {
	c.criouContato = true
	c.contato = &Contato{Id: 1, SourceId: "src-1", Identifier: identifier, Name: nome}
	return c.contato, nil
}
func (c *clienteFalso) SourceIdDaInbox(int) (string, error) { return c.sourceId, nil }
func (c *clienteFalso) CriaVinculoInbox(int) (string, error) {
	c.criouVinculo = true
	c.sourceId = "src-novo"
	return c.sourceId, nil
}
func (c *clienteFalso) ConversaAberta(string) (*Conversa, error) { return c.conversa, nil }
func (c *clienteFalso) CriaConversa(string) (*Conversa, error) {
	c.criouConversa = true
	c.conversa = &Conversa{Id: 11}
	return c.conversa, nil
}
func (c *clienteFalso) CriaMensagem(_ string, _ int, texto string) (*Mensagem, error) {
	if c.erroMensagem != nil {
		return nil, c.erroMensagem
	}
	c.textos = append(c.textos, texto)
	return &Mensagem{Id: 555}, nil
}

func monta(repo *repoFalso, cliente *clienteFalso) *Entrada {
	e := NewEntrada(repo)
	e.fabrica = func(*chatwoot_model.ChatwootConfig) chatwootClient { return cliente }
	return e
}

func evento(t *testing.T, info map[string]any, mensagem map[string]any) *Evento {
	t.Helper()
	bruto, err := json.Marshal(map[string]any{
		"event": "Message", "instanceId": "inst-1", "instanceName": "viajosy",
		"data": map[string]any{"Info": info, "Message": mensagem},
	})
	if err != nil {
		t.Fatalf("payload inválido: %v", err)
	}
	var e Evento
	if err := json.Unmarshal(bruto, &e); err != nil {
		t.Fatalf("decode falhou: %v", err)
	}
	return &e
}

func TestProcessaCriaContatoConversaEMensagem(t *testing.T) {
	repo, cliente := novoRepo(), &clienteFalso{}

	resultado, err := monta(repo, cliente).Processa(evento(t,
		map[string]any{"ID": "WAID1", "Sender": "5511999998888@s.whatsapp.net", "PushName": "Kenny"},
		map[string]any{"conversation": "oi"}))

	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if resultado.Ignorado {
		t.Fatalf("não devia ignorar: %s", resultado.Motivo)
	}
	if !cliente.criouContato || !cliente.criouConversa {
		t.Error("esperado criar contato e conversa")
	}
	if len(cliente.textos) != 1 || cliente.textos[0] != "oi" {
		t.Errorf("texto errado: %v", cliente.textos)
	}
	if len(repo.marcadas) != 1 || repo.marcadas[0].ChatwootMessageId != 555 {
		t.Errorf("idempotência não registrada: %+v", repo.marcadas)
	}
}

// Reentrega do JetStream não pode virar mensagem repetida na conversa.
func TestProcessaIgnoraWaidJaProcessado(t *testing.T) {
	repo, cliente := novoRepo(), &clienteFalso{}
	repo.processada = &chatwoot_model.MensagemProcessada{Waid: "WAID1", ChatwootMessageId: 42}

	resultado, err := monta(repo, cliente).Processa(evento(t,
		map[string]any{"ID": "WAID1", "Sender": "5511999998888@s.whatsapp.net"},
		map[string]any{"conversation": "oi"}))

	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if !resultado.Ignorado || resultado.ChatwootMessageId != 42 {
		t.Errorf("esperado ignorado com o id anterior, veio %+v", resultado)
	}
	if len(cliente.textos) != 0 {
		t.Error("mensagem duplicada enviada ao chatwoot")
	}
}

// A conta migrada para LID manda o par invertido; o telefone tem que sair do
// SenderAlt, não da posição.
func TestProcessaResolveTelefoneComParInvertido(t *testing.T) {
	repo, cliente := novoRepo(), &clienteFalso{}

	_, err := monta(repo, cliente).Processa(evento(t,
		map[string]any{"ID": "WAID1", "Sender": "123456@lid", "SenderAlt": "5511999998888@s.whatsapp.net"},
		map[string]any{"conversation": "oi"}))

	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if cliente.contato.Identifier != "5511999998888" {
		t.Errorf("contato criado com identifier errado: %q", cliente.contato.Identifier)
	}
	if repo.lids["123456"] != "5511999998888" {
		t.Errorf("par LID não persistido: %v", repo.lids)
	}
}

// É o caso que a Evolution Node perde: mensagem só com LID. Com o par já visto,
// o conector ainda entrega no contato certo.
func TestProcessaUsaMapaQuandoSoVemLid(t *testing.T) {
	repo, cliente := novoRepo(), &clienteFalso{}
	repo.lids["123456"] = "5511999998888"

	resultado, err := monta(repo, cliente).Processa(evento(t,
		map[string]any{"ID": "WAID1", "Sender": "123456@lid"},
		map[string]any{"conversation": "oi"}))

	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if resultado.Ignorado {
		t.Fatalf("não devia ignorar: %s", resultado.Motivo)
	}
	if cliente.contato.Identifier != "5511999998888" {
		t.Errorf("identifier errado: %q", cliente.contato.Identifier)
	}
}

// LID nunca visto: melhor não entregar do que criar contato falso com o LID
// como se fosse telefone.
func TestProcessaIgnoraLidDesconhecido(t *testing.T) {
	repo, cliente := novoRepo(), &clienteFalso{}

	resultado, err := monta(repo, cliente).Processa(evento(t,
		map[string]any{"ID": "WAID1", "Sender": "999@lid"},
		map[string]any{"conversation": "oi"}))

	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if !resultado.Ignorado {
		t.Error("esperado ignorado")
	}
	if cliente.criouContato {
		t.Error("contato falso criado a partir do LID")
	}
}

func TestProcessaIgnoraGrupoSemSyncGroups(t *testing.T) {
	repo, cliente := novoRepo(), &clienteFalso{}

	resultado, _ := monta(repo, cliente).Processa(evento(t,
		map[string]any{"ID": "WAID1", "Sender": "5511999998888@s.whatsapp.net",
			"Chat": "12036@g.us", "IsGroup": true},
		map[string]any{"conversation": "oi"}))

	if !resultado.Ignorado {
		t.Error("esperado ignorar grupo")
	}
}

func TestProcessaIgnoraMensagemPropria(t *testing.T) {
	repo, cliente := novoRepo(), &clienteFalso{}

	resultado, _ := monta(repo, cliente).Processa(evento(t,
		map[string]any{"ID": "WAID1", "Sender": "5511999998888@s.whatsapp.net", "IsFromMe": true},
		map[string]any{"conversation": "oi"}))

	if !resultado.Ignorado {
		t.Error("esperado ignorar mensagem própria")
	}
}

func TestProcessaIgnoraQuandoDesabilitado(t *testing.T) {
	repo, cliente := novoRepo(), &clienteFalso{}
	repo.config.Enabled = false

	resultado, _ := monta(repo, cliente).Processa(evento(t,
		map[string]any{"ID": "WAID1", "Sender": "5511999998888@s.whatsapp.net"},
		map[string]any{"conversation": "oi"}))

	if !resultado.Ignorado {
		t.Error("esperado ignorar com chatwoot desabilitado")
	}
}

// Falha do Chatwoot tem que voltar como erro, sem marcar como processada —
// senão a reentrega descarta a mensagem como duplicata e ela some.
func TestProcessaNaoMarcaProcessadaQuandoChatwootFalha(t *testing.T) {
	repo := novoRepo()
	cliente := &clienteFalso{erroMensagem: errors.New("chatwoot fora")}

	_, err := monta(repo, cliente).Processa(evento(t,
		map[string]any{"ID": "WAID1", "Sender": "5511999998888@s.whatsapp.net"},
		map[string]any{"conversation": "oi"}))

	if err == nil {
		t.Fatal("esperado erro")
	}
	if len(repo.marcadas) != 0 {
		t.Errorf("marcou processada apesar da falha: %+v", repo.marcadas)
	}
}

func TestProcessaReusaConversaAberta(t *testing.T) {
	repo := novoRepo()
	cliente := &clienteFalso{
		contato:  &Contato{Id: 1, SourceId: "src-1", Identifier: "5511999998888"},
		conversa: &Conversa{Id: 77},
	}

	_, err := monta(repo, cliente).Processa(evento(t,
		map[string]any{"ID": "WAID1", "Sender": "5511999998888@s.whatsapp.net"},
		map[string]any{"conversation": "oi"}))

	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if cliente.criouContato || cliente.criouConversa {
		t.Error("esperado reusar contato e conversa existentes")
	}
}

func TestTextoCobreOsTiposDeMensagem(t *testing.T) {
	casos := []struct {
		nome     string
		mensagem map[string]any
		esperado string
	}{
		{"conversation", map[string]any{"conversation": "oi"}, "oi"},
		{"extendedText", map[string]any{"extendedTextMessage": map[string]any{"text": "link"}}, "link"},
		{"imagem com legenda", map[string]any{"imageMessage": map[string]any{"caption": "praia"}}, "praia"},
		{"imagem sem legenda", map[string]any{"imageMessage": map[string]any{}}, "[imagem]"},
		{"audio", map[string]any{"audioMessage": map[string]any{}}, "[áudio]"},
		{"documento", map[string]any{"documentMessage": map[string]any{"fileName": "voucher.pdf"}}, "voucher.pdf"},
		{"desconhecido", map[string]any{"reactionMessage": map[string]any{}}, ""},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			e := evento(t, map[string]any{"ID": "W"}, c.mensagem)
			if got := e.Texto(); got != c.esperado {
				t.Errorf("esperado %q, veio %q", c.esperado, got)
			}
		})
	}
}
