package peers

// Krug razgovora s istim čvorom (razgovoriZaRedom) ide dalje odmah dok je
// razgovor pun ili skraćen. Kad primatelj sve poslano preskoči (npr.
// verzije s vremenom ispred njegova sata), granica mu se ne pomakne, pa bi
// sljedeći razgovor poslao isto, i tako do najviseKrugovaZaRedom. Zato se
// pamti otisak stanja obiju strana na početku razgovora: granice, granice
// odbijenih i tražene rupe. Kad je isti kao na početku prethodnog razgovora,
// prethodni nije ništa pomaknuo, pa ni sljedeći ne bi, i krug staje. Otisak
// ne putuje i ništa ne mijenja na žici.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// stanjeRazgovora je ono od čega ovisi što razgovor pošalje i primi
type stanjeRazgovora struct {
	Moja            map[string]string `json:"moja"`
	Njihova         map[string]string `json:"njihova"`
	MojeOdbijene    map[string]string `json:"moje_odbijene"`
	NjihoveOdbijene map[string]string `json:"njihove_odbijene"`
	Trazim          map[string]raspon `json:"trazim"`
	Traze           map[string]raspon `json:"traze"`
}

// otisakStanja je otisak stanja obiju strana nakon granica; JSON slaže
// ključeve mapa redom, pa isto stanje daje isti otisak
func (r *razgovorSCvorom) otisakStanja() string {
	b, _ := json.Marshal(stanjeRazgovora{ // mape stringova i raspona uvijek se daju zapisati
		Moja: r.moja.Frontier, Njihova: r.theirs.Frontier,
		MojeOdbijene: r.odbijene.moje, NjihoveOdbijene: r.njihoveOdbijene,
		Trazim: r.trazeno, Traze: r.njihovaTrazenja,
	})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// josVerzija javlja da je razgovor bio pun ili skraćen, a stanje se od
// početka prethodnog razgovora pomaknulo (pomaklo)
func (s *Service) josVerzija(nodeID string, applied, sent int, otisak *string) bool {
	skraceno := s.imaJos(nodeID)
	pomak := s.pomaklo(nodeID, otisak)
	return pomak && (skraceno || applied >= NajviseVerzijaPoRazmjeni || sent >= NajviseVerzijaPoRazmjeni)
}

// pomaklo usporedi otisak s početka zadnjeg razgovora s čvorom s otiskom
// prethodnog (prethodni) i zapamti novi. Bez otiska (razgovor nije stigao
// do granica) javlja da se pomaklo, kao i prije otiska.
func (s *Service) pomaklo(nodeID string, prethodni *string) bool {
	v, ok := s.otisci.LoadAndDelete(nodeID)
	novi, _ := v.(string) // spremaju se samo stringovi
	if !ok || novi == "" {
		return true
	}
	isto := novi == *prethodni
	*prethodni = novi
	return !isto
}
