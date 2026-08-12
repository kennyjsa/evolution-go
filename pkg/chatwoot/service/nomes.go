package chatwoot_service

import (
	"sync"
	"time"

	chatwoot_model "github.com/evolution-foundation/evolution-go/pkg/chatwoot/model"
)

// validadeDosNomes: nome de exibição muda raramente, então uma hora evita ida à
// API a cada resposta do agente. Apelido trocado passa a valer no máximo uma
// hora depois, sozinho, sem reiniciar o evolution-go.
const validadeDosNomes = time.Hour

// CacheDeNomes evita uma chamada a /agents por mensagem enviada. O nome de
// exibição muda raramente, e o custo de errar por alguns minutos é o cliente ver
// o apelido antigo — bem menor que uma ida à API a cada resposta do agente.
type CacheDeNomes struct {
	mu       sync.Mutex
	nomes    map[string]map[int]string
	expiraEm map[string]time.Time
	busca    func(config *chatwoot_model.ChatwootConfig) (map[int]string, error)
}

func NovoCacheDeNomes() *CacheDeNomes {
	return &CacheDeNomes{
		nomes:    map[string]map[int]string{},
		expiraEm: map[string]time.Time{},
		busca: func(config *chatwoot_model.ChatwootConfig) (map[int]string, error) {
			return NewClient(config.Url, config.AccountId, config.AccountToken,
				config.InboxId, config.InboxIdentifier).NomesDeExibicao()
		},
	}
}

// Nomes devolve os apelidos por id de agente, servindo do cache quando possível.
func (c *CacheDeNomes) Nomes(config *chatwoot_model.ChatwootConfig) (map[int]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	chave := config.InstanceId
	if nomes, ok := c.nomes[chave]; ok && time.Now().Before(c.expiraEm[chave]) {
		return nomes, nil
	}

	nomes, err := c.busca(config)
	if err != nil {
		// Devolve o que houver em cache mesmo vencido: assinar com o apelido
		// antigo é melhor que cair para o nome completo porque a API piscou.
		if antigos, ok := c.nomes[chave]; ok {
			return antigos, nil
		}
		return nil, err
	}

	c.nomes[chave] = nomes
	c.expiraEm[chave] = time.Now().Add(validadeDosNomes)
	return nomes, nil
}
