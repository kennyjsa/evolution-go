package chatwoot_service

import (
	"errors"
	"testing"
	"time"

	chatwoot_model "github.com/evolution-foundation/evolution-go/pkg/chatwoot/model"
)

func cacheDeTeste(nomes map[int]string, erro error, chamadas *int) *CacheDeNomes {
	c := NovoCacheDeNomes()
	c.busca = func(*chatwoot_model.ChatwootConfig) (map[int]string, error) {
		*chamadas++
		return nomes, erro
	}
	return c
}

// Sem cache seria uma ida à API do Chatwoot por resposta de agente.
func TestCacheNaoConsultaDuasVezesDentroDoTtl(t *testing.T) {
	var chamadas int
	c := cacheDeTeste(map[int]string{1: "Josieli Sanches"}, nil, &chamadas)
	config := &chatwoot_model.ChatwootConfig{InstanceId: "i1"}

	for i := 0; i < 5; i++ {
		if _, err := c.Nomes(config); err != nil {
			t.Fatalf("erro inesperado: %v", err)
		}
	}
	if chamadas != 1 {
		t.Errorf("esperada 1 consulta, houve %d", chamadas)
	}
}

// Apelido trocado no Chatwoot precisa valer sozinho, sem reiniciar o processo.
func TestCacheRenovaDepoisDoTtl(t *testing.T) {
	var chamadas int
	c := cacheDeTeste(map[int]string{1: "Josieli Sanches"}, nil, &chamadas)
	config := &chatwoot_model.ChatwootConfig{InstanceId: "i1"}

	if _, err := c.Nomes(config); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	c.expiraEm["i1"] = time.Now().Add(-time.Minute)

	if _, err := c.Nomes(config); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if chamadas != 2 {
		t.Errorf("cache vencido não renovou: %d consultas", chamadas)
	}
}

// Cada instância tem sua conta no Chatwoot; misturar os apelidos assinaria a
// mensagem com o nome de alguém de outra conta.
func TestCacheSeparaPorInstancia(t *testing.T) {
	var chamadas int
	c := cacheDeTeste(map[int]string{1: "Josieli Sanches"}, nil, &chamadas)

	if _, err := c.Nomes(&chatwoot_model.ChatwootConfig{InstanceId: "i1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Nomes(&chatwoot_model.ChatwootConfig{InstanceId: "i2"}); err != nil {
		t.Fatal(err)
	}
	if chamadas != 2 {
		t.Errorf("instâncias diferentes compartilharam cache: %d consultas", chamadas)
	}
}

// API fora do ar com cache vencido: o apelido antigo é melhor que cair para o
// nome completo do cadastro.
func TestCacheServeValorVencidoQuandoApiFalha(t *testing.T) {
	var chamadas int
	c := cacheDeTeste(map[int]string{1: "Josieli Sanches"}, nil, &chamadas)
	config := &chatwoot_model.ChatwootConfig{InstanceId: "i1"}

	if _, err := c.Nomes(config); err != nil {
		t.Fatal(err)
	}
	c.expiraEm["i1"] = time.Now().Add(-time.Minute)
	c.busca = func(*chatwoot_model.ChatwootConfig) (map[int]string, error) {
		return nil, errors.New("chatwoot fora")
	}

	nomes, err := c.Nomes(config)
	if err != nil {
		t.Fatalf("deveria servir o valor antigo: %v", err)
	}
	if nomes[1] != "Josieli Sanches" {
		t.Errorf("valor antigo perdido: %v", nomes)
	}
}

// TTL de uma hora foi decisão explícita: mais curto vira ida à API o tempo todo.
func TestValidadeDeUmaHora(t *testing.T) {
	if validadeDosNomes != time.Hour {
		t.Errorf("TTL inesperado: %s", validadeDosNomes)
	}
}
