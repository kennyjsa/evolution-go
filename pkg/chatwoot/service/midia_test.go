package chatwoot_service

import (
	"errors"
	"testing"
)

// anexo guarda o que foi entregue ao Chatwoot pelo caminho de mídia.
type anexo struct {
	texto, arquivo, mimetype string
	conteudo                 []byte
}

func (c *clienteFalso) CriaMensagemComAnexo(_ int, texto, arquivo, mimetype, waid string, conteudo []byte) (*Mensagem, error) {
	if c.erroMensagem != nil {
		return nil, c.erroMensagem
	}
	c.anexos = append(c.anexos, anexo{texto, arquivo, mimetype, conteudo})
	return &Mensagem{Id: 556}, nil
}

func infoPadrao() map[string]any {
	return map[string]any{
		"ID": "WAID-FOTO", "Sender": "5511999999999@s.whatsapp.net",
		"Chat": "5511999999999@s.whatsapp.net", "PushName": "Cliente",
	}
}

// Foto de hotel e lâmina de roteiro são o conteúdo da venda; entregar só
// "[imagem]" na conversa perde o que o cliente mandou.
func TestEntradaSobeImagemComoAnexo(t *testing.T) {
	repo, cliente := novoRepo(), &clienteFalso{}
	e := monta(repo, cliente).ComBaixadorDeMidia(
		func(string, []byte) ([]byte, error) { return []byte("bytes-da-foto"), nil })

	res, err := e.Processa(evento(t, infoPadrao(), map[string]any{
		"imageMessage": map[string]any{"caption": "hotel em Cancún", "mimetype": "image/jpeg"},
	}))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if res.Ignorado {
		t.Fatalf("imagem foi ignorada: %s", res.Motivo)
	}
	if len(cliente.anexos) != 1 {
		t.Fatalf("nenhum anexo criado: %+v (textos: %v)", cliente.anexos, cliente.textos)
	}
	if string(cliente.anexos[0].conteudo) != "bytes-da-foto" {
		t.Errorf("conteúdo errado: %q", cliente.anexos[0].conteudo)
	}
	if cliente.anexos[0].texto != "hotel em Cancún" {
		t.Errorf("legenda perdida: %q", cliente.anexos[0].texto)
	}
	if cliente.anexos[0].arquivo != "WAID-FOTO.jpeg" {
		t.Errorf("nome de arquivo errado: %q", cliente.anexos[0].arquivo)
	}
}

// Áudio de negociação não tem legenda; o marcador é fallback de texto e ao lado
// do arquivo vira ruído.
func TestEntradaAudioSobeSemMarcador(t *testing.T) {
	repo, cliente := novoRepo(), &clienteFalso{}
	e := monta(repo, cliente).ComBaixadorDeMidia(
		func(string, []byte) ([]byte, error) { return []byte("ogg"), nil })

	if _, err := e.Processa(evento(t, infoPadrao(), map[string]any{
		"audioMessage": map[string]any{"mimetype": "audio/ogg; codecs=opus"},
	})); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}

	if len(cliente.anexos) != 1 {
		t.Fatalf("áudio não virou anexo: %+v", cliente.anexos)
	}
	if cliente.anexos[0].texto != "" {
		t.Errorf("marcador foi junto do anexo: %q", cliente.anexos[0].texto)
	}
	if cliente.anexos[0].arquivo != "WAID-FOTO.ogg" {
		t.Errorf("extensão de áudio errada: %q", cliente.anexos[0].arquivo)
	}
}

// Documento traz nome próprio — "roteiro-bariloche.pdf" diz o que foi enviado,
// um nome gerado não.
func TestEntradaDocumentoPreservaNomeOriginal(t *testing.T) {
	repo, cliente := novoRepo(), &clienteFalso{}
	e := monta(repo, cliente).ComBaixadorDeMidia(
		func(string, []byte) ([]byte, error) { return []byte("pdf"), nil })

	if _, err := e.Processa(evento(t, infoPadrao(), map[string]any{
		"documentMessage": map[string]any{
			"fileName": "roteiro-bariloche.pdf", "mimetype": "application/pdf"},
	})); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}

	if len(cliente.anexos) != 1 || cliente.anexos[0].arquivo != "roteiro-bariloche.pdf" {
		t.Errorf("nome do documento perdido: %+v", cliente.anexos)
	}
}

// Perder a conversa inteira porque um anexo não baixou é pior que entregar o
// marcador: o atendente ao menos vê que algo chegou.
func TestEntradaCaiParaTextoQuandoDownloadFalha(t *testing.T) {
	repo, cliente := novoRepo(), &clienteFalso{}
	e := monta(repo, cliente).ComBaixadorDeMidia(
		func(string, []byte) ([]byte, error) { return nil, errors.New("mídia expirada") })

	res, err := e.Processa(evento(t, infoPadrao(), map[string]any{
		"imageMessage": map[string]any{"mimetype": "image/jpeg"},
	}))
	if err != nil {
		t.Fatalf("falha de download não deveria virar erro: %v", err)
	}
	if res.Ignorado {
		t.Fatalf("mensagem foi descartada: %s", res.Motivo)
	}
	if len(cliente.textos) != 1 || cliente.textos[0] != "[imagem]" {
		t.Errorf("esperado fallback para marcador, veio %v", cliente.textos)
	}
}

// Sem baixador ligado o conector segue funcionando com texto.
func TestEntradaSemBaixadorUsaMarcador(t *testing.T) {
	repo, cliente := novoRepo(), &clienteFalso{}

	if _, err := monta(repo, cliente).Processa(evento(t, infoPadrao(), map[string]any{
		"imageMessage": map[string]any{"mimetype": "image/jpeg"},
	})); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(cliente.anexos) != 0 || len(cliente.textos) != 1 {
		t.Errorf("sem baixador não deveria haver anexo: %+v / %v", cliente.anexos, cliente.textos)
	}
}

// markAsRead devolve o tique azul ao cliente; sem ele o cliente não sabe se o
// atendimento viu a mensagem.
func TestEntradaMarcaComoLidaQuandoConfigurado(t *testing.T) {
	repo, cliente := novoRepo(), &clienteFalso{}
	repo.config.MarkAsRead = true

	var marcados []string
	e := monta(repo, cliente).ComMarcadorDeLida(
		func(_, _, _, waid string) error {
			marcados = append(marcados, waid)
			return nil
		})

	if _, err := e.Processa(evento(t, infoPadrao(), map[string]any{
		"conversation": "quanto custa o pacote?",
	})); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(marcados) != 1 || marcados[0] != "WAID-FOTO" {
		t.Errorf("mensagem não foi marcada como lida: %v", marcados)
	}
}

// Sem markAsRead na config, não manda recibo: quem desligou não quer que o
// cliente saiba que a mensagem foi vista.
func TestEntradaNaoMarcaLidaSemConfig(t *testing.T) {
	repo, cliente := novoRepo(), &clienteFalso{}

	var marcados []string
	e := monta(repo, cliente).ComMarcadorDeLida(
		func(_, _, _, waid string) error { marcados = append(marcados, waid); return nil })

	if _, err := e.Processa(evento(t, infoPadrao(), map[string]any{
		"conversation": "oi",
	})); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(marcados) != 0 {
		t.Errorf("marcou como lida com markAsRead desligado: %v", marcados)
	}
}

// Falha ao marcar lida não pode desfazer a mensagem já criada nem provocar
// reentrega.
func TestEntradaFalhaAoMarcarLidaNaoQuebraMensagem(t *testing.T) {
	repo, cliente := novoRepo(), &clienteFalso{}
	repo.config.MarkAsRead = true

	e := monta(repo, cliente).ComMarcadorDeLida(
		func(_, _, _, _ string) error { return errors.New("desconectado") })

	res, err := e.Processa(evento(t, infoPadrao(), map[string]any{"conversation": "oi"}))
	if err != nil {
		t.Fatalf("falha ao marcar lida virou erro: %v", err)
	}
	if res.Ignorado || res.ChatwootMessageId == 0 {
		t.Errorf("mensagem perdida por causa do recibo: %+v", res)
	}
}

// O Chatwoot decide o tipo do anexo pelo Content-Type da parte do upload: sem o
// mimetype real, a foto do hotel vira "arquivo para baixar" em vez de imagem na
// conversa — foi o que apareceu no primeiro teste com celular.
func TestEntradaEnviaMimetypeDoAnexo(t *testing.T) {
	repo, cliente := novoRepo(), &clienteFalso{}
	e := monta(repo, cliente).ComBaixadorDeMidia(
		func(string, []byte) ([]byte, error) { return []byte("jpg"), nil })

	if _, err := e.Processa(evento(t, infoPadrao(), map[string]any{
		"imageMessage": map[string]any{"mimetype": "image/jpeg"},
	})); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(cliente.anexos) != 1 || cliente.anexos[0].mimetype != "image/jpeg" {
		t.Errorf("mimetype não chegou ao upload: %+v", cliente.anexos)
	}
}

func TestEntradaMimetypeDeAudio(t *testing.T) {
	repo, cliente := novoRepo(), &clienteFalso{}
	e := monta(repo, cliente).ComBaixadorDeMidia(
		func(string, []byte) ([]byte, error) { return []byte("ogg"), nil })

	if _, err := e.Processa(evento(t, infoPadrao(), map[string]any{
		"audioMessage": map[string]any{"mimetype": "audio/ogg; codecs=opus"},
	})); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(cliente.anexos) != 1 || cliente.anexos[0].mimetype != "audio/ogg; codecs=opus" {
		t.Errorf("mimetype de áudio perdido: %+v", cliente.anexos)
	}
}
