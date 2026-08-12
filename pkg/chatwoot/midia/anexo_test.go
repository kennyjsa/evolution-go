package chatwoot_midia

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBaixaAnexoTiraNomeDaUrl(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("pdf"))
	}))
	defer srv.Close()

	conteudo, arquivo, err := BaixaAnexoDoChatwoot(srv.URL + "/rails/blob/roteiro-bariloche.pdf")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if string(conteudo) != "pdf" {
		t.Errorf("conteúdo errado: %q", conteudo)
	}
	if arquivo != "roteiro-bariloche.pdf" {
		t.Errorf("nome do arquivo perdido: %q", arquivo)
	}
}

// Erro do storage não pode virar um anexo vazio entregue ao cliente.
func TestBaixaAnexoFalhaEmStatusRuim(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	if _, _, err := BaixaAnexoDoChatwoot(srv.URL + "/x.jpg"); err == nil {
		t.Errorf("403 deveria virar erro")
	}
}

// Truncar em silêncio entregaria um arquivo corrompido; melhor falhar.
func TestBaixaAnexoRecusaArquivoAcimaDoLimite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		grande := strings.Repeat("x", 1<<20)
		for i := 0; i < 65; i++ {
			w.Write([]byte(grande))
		}
	}))
	defer srv.Close()

	if _, _, err := BaixaAnexoDoChatwoot(srv.URL + "/grande.bin"); err == nil {
		t.Errorf("arquivo acima do limite deveria falhar")
	}
}

// URL sem nome utilizável não pode virar path traversal no nome do arquivo.
func TestNomeDoAnexoCaiParaPadraoSeguro(t *testing.T) {
	for _, url := range []string{"http://cw/", "http://cw/../../etc/passwd", ":://quebrada"} {
		if nome := nomeDoAnexo(url); nome != "anexo" && strings.Contains(nome, "..") {
			t.Errorf("nome inseguro para %q: %q", url, nome)
		}
	}
}
