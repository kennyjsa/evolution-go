package chatwoot_service

import (
	"regexp"
	"strings"
)

// Info é o subconjunto do evento do whatsmeow de que a identificação precisa.
type Info struct {
	Sender    string
	SenderAlt string
	Chat      string
}

var separadorJid = regexp.MustCompile(`[:@]`)

// SoNumero extrai a parte identificadora do JID, descartando o device e o
// domínio: "5511999998888:70@s.whatsapp.net" -> "5511999998888".
func SoNumero(jid string) string {
	if jid == "" {
		return ""
	}
	return separadorJid.Split(jid, -1)[0]
}

// EhLid diz se o JID identifica por Linked ID em vez de telefone.
func EhLid(jid string) bool {
	return strings.Contains(jid, "@lid")
}

// Identifica separa telefone e LID do par `Sender`/`SenderAlt`, em qualquer
// ordem.
//
// O whatsmeow entrega as duas identidades do remetente nesses campos, mas não
// garante qual vem em qual: em conta migrada para LID, `Sender` vem `<id>@lid`
// e o telefone é que vai no `SenderAlt`. Assumir a ordem fixa cria contato com
// o LID no lugar do telefone — o defeito que este conector existe para não
// repetir.
//
// Decide pelo sufixo do JID, nunca pela posição. Qualquer um dos dois pode
// voltar vazio: uma conta pode revelar só o LID.
func Identifica(info Info) (telefone string, lid string) {
	for _, jid := range []string{info.Sender, info.SenderAlt} {
		if jid == "" {
			continue
		}
		if EhLid(jid) {
			if lid == "" {
				lid = SoNumero(jid)
			}
			continue
		}
		if telefone == "" {
			telefone = SoNumero(jid)
		}
	}
	return telefone, lid
}

// ResolveTelefone devolve o telefone do remetente, consultando o mapa
// persistido quando o evento traz só o LID.
//
// O LID de um contato é estável, então um telefone visto uma vez vale para as
// próximas mensagens — é o que evita perder o contato quando o WhatsApp para de
// mandar o par completo.
func ResolveTelefone(info Info, telefoneDoLid func(string) (string, error)) (string, error) {
	telefone, lid := Identifica(info)

	if telefone != "" {
		return telefone, nil
	}
	if lid == "" {
		return "", nil
	}
	return telefoneDoLid(lid)
}
