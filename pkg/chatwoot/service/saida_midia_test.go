package chatwoot_service

import (
	"errors"
	"testing"
)

type envioMidia struct {
	tipo, legenda, arquivo string
	conteudo               []byte
}

func saidaComMidia(repo *repoFalso, erroEnvio error) (*Saida, *[]envioMidia, *[]envio) {
	var midias []envioMidia
	var textos []envio

	s := NewSaida(repo, func(instanceId, numero, texto string) (string, error) {
		textos = append(textos, envio{instanceId, numero, texto})
		return "WAID-TEXTO", nil
	}).ComMidia(
		func(url string) ([]byte, string, error) {
			return []byte("conteudo:" + url), "hotel.jpg", nil
		},
		func(_, _, tipo, legenda, arquivo string, conteudo []byte) (string, error) {
			if erroEnvio != nil {
				return "", erroEnvio
			}
			midias = append(midias, envioMidia{tipo, legenda, arquivo, conteudo})
			return "WAID-MIDIA", nil
		},
	)
	return s, &midias, &textos
}

func hookComAnexo(texto, fileType, url string) *WebhookChatwoot {
	h := hookDoAgente(texto)
	h.Attachments = append(h.Attachments, struct {
		FileType string `json:"file_type"`
		DataUrl  string `json:"data_url"`
	}{FileType: fileType, DataUrl: url})
	return h
}

// Agente anexando foto de hotel: o arquivo tem que chegar ao cliente, não só a
// legenda.
func TestSaidaEnviaAnexoDoAgente(t *testing.T) {
	repo := novoRepo()
	s, midias, textos := saidaComMidia(repo, nil)

	res, err := s.Processa(configAtiva(), hookComAnexo("segue o hotel", "image", "http://cw/foto.jpg"))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if res.Ignorado {
		t.Fatalf("anexo foi ignorado: %s", res.Motivo)
	}
	if len(*midias) != 1 {
		t.Fatalf("anexo não enviado: %+v (textos: %+v)", *midias, *textos)
	}
	m := (*midias)[0]
	if m.tipo != "image" || m.legenda != "segue o hotel" || m.arquivo != "hotel.jpg" {
		t.Errorf("envio de mídia errado: %+v", m)
	}
	if string(m.conteudo) != "conteudo:http://cw/foto.jpg" {
		t.Errorf("conteúdo errado: %q", m.conteudo)
	}
	if len(*textos) != 0 {
		t.Errorf("legenda foi mandada de novo como texto: %+v", *textos)
	}
}

// Áudio de negociação vai sem legenda; exigir texto descartaria a mensagem.
func TestSaidaEnviaAnexoSemTexto(t *testing.T) {
	repo := novoRepo()
	s, midias, _ := saidaComMidia(repo, nil)

	res, err := s.Processa(configAtiva(), hookComAnexo("", "audio", "http://cw/audio.ogg"))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if res.Ignorado {
		t.Fatalf("áudio sem legenda foi ignorado: %s", res.Motivo)
	}
	if len(*midias) != 1 || (*midias)[0].tipo != "audio" {
		t.Errorf("áudio não enviado: %+v", *midias)
	}
}

// "file" é o tipo genérico do Chatwoot para PDF/lâmina; cair em documento
// entrega o arquivo, ignorar não entrega nada.
func TestSaidaTraduzTipoDesconhecidoParaDocumento(t *testing.T) {
	repo := novoRepo()
	s, midias, _ := saidaComMidia(repo, nil)

	if _, err := s.Processa(configAtiva(),
		hookComAnexo("lâmina", "file", "http://cw/roteiro.pdf")); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(*midias) != 1 || (*midias)[0].tipo != "document" {
		t.Errorf("tipo traduzido errado: %+v", *midias)
	}
}

// Um "segue a foto" entregue sem a foto é pior que a reentrega do webhook.
func TestSaidaFalhaDeAnexoNaoViraTexto(t *testing.T) {
	repo := novoRepo()
	s, _, textos := saidaComMidia(repo, errors.New("upload recusado"))

	if _, err := s.Processa(configAtiva(),
		hookComAnexo("segue a foto", "image", "http://cw/foto.jpg")); err == nil {
		t.Fatalf("esperado erro para o Chatwoot reentregar")
	}
	if len(*textos) != 0 {
		t.Errorf("mandou só o texto no lugar do anexo: %+v", *textos)
	}
	if len(repo.marcadas) != 0 {
		t.Errorf("marcou como enviada apesar da falha: %+v", repo.marcadas)
	}
}

// Sem mídia ligada a saída continua entregando texto.
func TestSaidaSemMidiaAindaEnviaTexto(t *testing.T) {
	repo := novoRepo()
	s, enviados := saidaDeTeste(repo, "WAID1", nil)

	res, err := s.Processa(configAtiva(), hookComAnexo("segue o hotel", "image", "http://cw/f.jpg"))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if res.Ignorado || len(*enviados) != 1 {
		t.Errorf("texto não foi entregue: %+v (%s)", *enviados, res.Motivo)
	}
}
