package nats_producer

import (
	"sort"
	"testing"
)

func subjects(t *testing.T, eventosGlobais []string) []string {
	t.Helper()
	p := &natsProducer{natsGlobalEvents: eventosGlobais}
	s := p.subjectsDoStream()
	sort.Strings(s)
	return s
}

func contem(lista []string, alvo string) bool {
	for _, s := range lista {
		if s == alvo {
			return true
		}
	}
	return false
}

// O subject global é o nome cru do evento e o de instância é
// `<instanceId>.<evento>`. O stream precisa cobrir os dois, senão o publish
// falha com "no responders" justo no caminho que interessa.
func TestSubjectsCobremGlobalEInstancia(t *testing.T) {
	s := subjects(t, []string{"MESSAGE", "READ_RECEIPT"})

	for _, esperado := range []string{"message", "*.message", "receipt", "*.receipt"} {
		if !contem(s, esperado) {
			t.Errorf("subject %q ausente em %v", esperado, s)
		}
	}
}

// Evento fora da lista não pode entrar no stream: um stream mais largo que o
// necessário captura subject de outro sistema no mesmo servidor NATS.
func TestSubjectsNaoIncluemEventoForaDaLista(t *testing.T) {
	s := subjects(t, []string{"MESSAGE"})

	if contem(s, "receipt") {
		t.Errorf("subject de READ_RECEIPT não deveria aparecer: %v", s)
	}
	if len(s) != 2 {
		t.Errorf("esperado 2 subjects para MESSAGE, veio %d: %v", len(s), s)
	}
}

// Sem lista explícita o produtor publica em qualquer evento do catálogo, então
// o stream precisa cobrir o catálogo inteiro.
func TestSubjectsSemListaCobremCatalogoInteiro(t *testing.T) {
	s := subjects(t, nil)

	var total int
	for _, nomes := range eventMap {
		total += len(nomes)
	}
	if len(s) != total*2 {
		t.Errorf("esperado %d subjects (%d eventos x global+instancia), veio %d",
			total*2, total, len(s))
	}
}

// A config aceita o evento em qualquer caixa; o mapa é indexado em maiúsculas.
func TestSubjectsAceitamEventoEmMinusculas(t *testing.T) {
	if s := subjects(t, []string{"message"}); !contem(s, "message") {
		t.Errorf("evento em minúsculas não resolveu: %v", s)
	}
}

// Evento desconhecido não existe no mapa e não deve gerar subject vazio — um
// subject "" faria o CreateOrUpdateStream falhar de forma opaca.
func TestSubjectsIgnoramEventoDesconhecido(t *testing.T) {
	if s := subjects(t, []string{"NAO_EXISTE"}); len(s) != 0 {
		t.Errorf("evento desconhecido gerou subjects: %v", s)
	}
}
