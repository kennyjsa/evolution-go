package chatwoot_service

import (
	"errors"
	"testing"
)

func TestIdentificaSeparaTelefoneELid(t *testing.T) {
	casos := []struct {
		nome          string
		info          Info
		telefone, lid string
	}{
		{
			nome:     "ordem esperada",
			info:     Info{Sender: "5511999998888@s.whatsapp.net", SenderAlt: "123456789@lid"},
			telefone: "5511999998888", lid: "123456789",
		},
		{
			// Conta migrada para LID: o whatsmeow inverte o par. Assumir a
			// ordem fixa criaria contato com o LID no lugar do telefone.
			nome:     "ordem invertida",
			info:     Info{Sender: "123456789@lid", SenderAlt: "5511999998888@s.whatsapp.net"},
			telefone: "5511999998888", lid: "123456789",
		},
		{
			nome:     "device no jid",
			info:     Info{Sender: "5511999998888:70@s.whatsapp.net"},
			telefone: "5511999998888", lid: "",
		},
		{
			nome:     "so lid",
			info:     Info{Sender: "123456789@lid"},
			telefone: "", lid: "123456789",
		},
		{
			nome:     "vazio",
			info:     Info{},
			telefone: "", lid: "",
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			telefone, lid := Identifica(c.info)
			if telefone != c.telefone {
				t.Errorf("telefone: esperado %q, veio %q", c.telefone, telefone)
			}
			if lid != c.lid {
				t.Errorf("lid: esperado %q, veio %q", c.lid, lid)
			}
		})
	}
}

func TestResolveTelefoneUsaOEventoQuandoTemTelefone(t *testing.T) {
	consultado := false
	mapa := func(string) (string, error) {
		consultado = true
		return "5500000000000", nil
	}

	telefone, err := ResolveTelefone(
		Info{Sender: "5511999998888@s.whatsapp.net", SenderAlt: "123456789@lid"}, mapa)

	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if telefone != "5511999998888" {
		t.Errorf("esperado telefone do evento, veio %q", telefone)
	}
	if consultado {
		t.Error("mapa consultado à toa: o evento já trazia o telefone")
	}
}

// É o caso que faz o conector funcionar onde a Evolution Node falha: mensagem
// que chega só com LID ainda vira contato certo, via par visto antes.
func TestResolveTelefoneCaiNoMapaQuandoSoTemLid(t *testing.T) {
	mapa := func(lid string) (string, error) {
		if lid != "123456789" {
			t.Errorf("lid consultado errado: %q", lid)
		}
		return "5511999998888", nil
	}

	telefone, err := ResolveTelefone(Info{Sender: "123456789@lid"}, mapa)

	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if telefone != "5511999998888" {
		t.Errorf("esperado telefone do mapa, veio %q", telefone)
	}
}

// LID nunca visto não tem telefone conhecido. Devolver o LID como se fosse
// telefone criaria contato falso — o defeito que este conector evita.
func TestResolveTelefoneVazioQuandoLidDesconhecido(t *testing.T) {
	mapa := func(string) (string, error) { return "", nil }

	telefone, err := ResolveTelefone(Info{Sender: "999@lid"}, mapa)

	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if telefone != "" {
		t.Errorf("esperado vazio, veio %q", telefone)
	}
}

func TestResolveTelefonePropagaErroDoMapa(t *testing.T) {
	falha := errors.New("banco fora")
	mapa := func(string) (string, error) { return "", falha }

	if _, err := ResolveTelefone(Info{Sender: "999@lid"}, mapa); !errors.Is(err, falha) {
		t.Errorf("esperado erro do mapa, veio %v", err)
	}
}

func TestResolveTelefoneNaoConsultaMapaSemRemetente(t *testing.T) {
	mapa := func(string) (string, error) {
		t.Error("mapa não devia ser consultado sem remetente")
		return "", nil
	}

	if telefone, err := ResolveTelefone(Info{}, mapa); err != nil || telefone != "" {
		t.Errorf("esperado vazio sem erro, veio %q / %v", telefone, err)
	}
}
