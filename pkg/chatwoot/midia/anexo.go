package chatwoot_midia

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// limiteAnexo protege a memória do processo: o arquivo é lido inteiro antes de
// ir para o WhatsApp, e o próprio WhatsApp não aceita muito mais que isso.
const limiteAnexo = 64 << 20 // 64 MiB

var clienteHttp = &http.Client{Timeout: 60 * time.Second}

// BaixaAnexoDoChatwoot busca o arquivo que o agente anexou. A URL vem do
// payload do webhook (`data_url`) e aponta para o storage do próprio Chatwoot.
func BaixaAnexoDoChatwoot(endereco string) ([]byte, string, error) {
	resp, err := clienteHttp.Get(endereco)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("chatwoot devolveu %d ao baixar o anexo", resp.StatusCode)
	}

	// LimitReader com +1: se vier exatamente o limite+1, o arquivo estourou e
	// truncá-lo em silêncio entregaria um anexo corrompido ao cliente.
	conteudo, err := io.ReadAll(io.LimitReader(resp.Body, limiteAnexo+1))
	if err != nil {
		return nil, "", err
	}
	if len(conteudo) > limiteAnexo {
		return nil, "", fmt.Errorf("anexo maior que o limite de %d MiB", limiteAnexo>>20)
	}

	return conteudo, nomeDoAnexo(endereco), nil
}

// nomeDoAnexo tira o nome do arquivo da URL. O WhatsApp mostra esse nome em
// documento, então "arquivo.bin" para todo PDF de roteiro seria uma perda real.
func nomeDoAnexo(endereco string) string {
	u, err := url.Parse(endereco)
	if err != nil {
		return "anexo"
	}

	nome := path.Base(u.Path)
	if nome == "" || nome == "." || nome == "/" || strings.Contains(nome, "..") {
		return "anexo"
	}
	return nome
}
