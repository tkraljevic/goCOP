package peers

// Granica odbijenih nakon uvoza (G4 u docs/plan-web-sync.md §3). Granica
// odbijenih vrijedi jer pošiljatelj u svakom ključu šalje neprekinut niz
// svojih verzija iznad granice primatelja (granica_odbijenih.go), ali samo
// za niz koji je imao kad je slao. Verziju koju poslije dobije ispod
// granice odbijenih (kasno) primatelj nije ni vidio ni odbio, a po granici
// odbijenih ona se preskoči. Kad u istom ključu stigne i novija verzija koju
// primatelj drži, njegova granica knjige prijeđe preko kasnog, pa ono ne
// stigne nikad.
//
// Pošiljatelj takvu verziju dobije na četiri načina: uvozom (arhiva knjige,
// ImportFile, i COP paket, UveziCop), razmjenom kad primljeno popuni rupu u
// nizu (dva istodobna razgovora donesu isti ključ, a prvi se upiše onaj
// kojemu nedostaje verzija iz sredine), ponovnim praćenjem kanala nakon
// brisanja kanala (PurgeChannel), kad se niz popuni iznova, i s onim čega
// prije nije bilo, i razmjenom nakon pospremanja uloženih očitanja
// (ulaganje.Zaboravi), koje verzije briše mimo razmjene pa granica knjige
// može pasti.
//
// Zato pošiljatelj broji takve upise: narastaj uvoza raste sa svakim uvozom
// koji upiše barem jednu verziju, s upisom razmjene koji popuni rupu
// (popunjavaRupu) i s brisanjem kanala ili pospremanjem koje obriše barem
// jednu verziju (zabiljeziUvoz, ObrisanoIzKnjige). Za svako računalo kojem šalje pamti, po ključu granice,
// dokle mu je u tom narastaju poslao (dosad). Granicu odbijenih tog
// računala poštuje najviše do toga, a ključ u kojem mu u tom narastaju nije
// poslao ništa šalje od njegove granice knjige. Unutar narastaja pošiljatelj
// ispod onoga što je poslao ne dobiva ništa novo (granica knjige mu ne pada,
// a popunjenu rupu bi brojao), pa je preskok siguran, a ključ s tisućama
// odbijenih ne zapne kao prije G4. Pamti se tek kad stigne
// poruka želja druge strane: šalje je tek nakon što je primljeno upisala i
// zapamtila što je odbila (zapamtiUvoz). Razgovor koji je pao ili u kojem
// je upis druge strane bio zauzet tako se ne broji. Kad druga strana
// primljeno namjerno ne upiše (bezPreskocenog), zaboravi svoj zapis
// odbijenih od ovog čvora, pa ga sljedeći razgovor i ne javlja.
//
// Pamti se samo u memoriji, po javnom ključu druge strane, kao i granica
// odbijenih. Nakon ponovnog pokretanja ništa nije zapamćeno, pa je prvi
// razgovor sa svakim računalom kao nakon uvoza: jedno ponovno slanje, kao
// kad se ponovno pokrene primatelj. Zato ni brisanje kanala prije ponovnog
// pokretanja ne može sakriti ono što ponovno praćenje donese. Narastaj
// razgovor uzme prije prve poruke (pocetakOdbijenih), pa uvoz usred
// razgovora vrijedi za sljedeći.

import (
	"context"
	"encoding/json"
	"sync"

	"gocop/internal/ledger"
)

// pamcenjeUvoza je narastaj uvoza i, po javnom ključu računala kojem ovaj
// čvor šalje, što je to računalo dobilo nakon uvoza. Granica u zapisu ne
// mijenja se na mjestu, nego se zamijeni, pa je razgovor čita i bez brave.
type pamcenjeUvoza struct {
	mu       sync.Mutex
	narastaj uint64
	zaCvor   map[string]nakonUvoza
}

// nakonUvoza je ono što je jedno računalo dobilo u narastaju uvoza
// narastaj: po ključu granice najveća poslana ili neispravna verzija (dosad)
type nakonUvoza struct {
	narastaj uint64
	dosad    map[string]string
}

// zabiljeziUvoz digne narastaj uvoza kad je upis koji se broji (broji)
// dodao ili obrisao barem jednu verziju. Uvoz i brisanje kanala broje se
// uvijek, a upis razmjene kad popuni rupu (popunjavaRupu). Zove se bez
// uvjeta, tek nakon upisa (primijeniPodBravom, primijeniAkoStigne,
// PurgeChannel), pa razgovor koji vidi novi narastaj u delti vidi i upisano.
func (s *Service) zabiljeziUvoz(upisano int, broji bool) {
	if upisano == 0 || !broji {
		return
	}
	s.uvozi.mu.Lock()
	defer s.uvozi.mu.Unlock()
	s.uvozi.narastaj++
}

// ObrisanoIzKnjige javlja da je ovaj čvor mimo razmjene obrisao verzije iz
// knjige (pospremanje uloženih očitanja, ulaganje.Zaboravi). Granica knjige
// tada može pasti, pa razmjena poslije donese i ono čega prije nije bilo;
// broji se kao uvoz. Čvor bez mreže (nil) nema kome slati.
func (s *Service) ObrisanoIzKnjige(obrisano int) {
	if s == nil {
		return
	}
	s.zabiljeziUvoz(obrisano, true)
}

// rupaUNizu: neka od verzija (json_each, polja oznaka, autor i kanal) nije u
// knjizi, a u knjizi je novija verzija istog autora u istom kanalu
const rupaUNizu = `SELECT EXISTS (SELECT 1 FROM json_each(?) j
	WHERE NOT EXISTS (SELECT 1 FROM record_versions r WHERE r.version_id = j.value ->> 0)
	  AND EXISTS (SELECT 1 FROM record_versions g WHERE g.node_id = j.value ->> 1 AND g.channel = j.value ->> 2 AND g.version_id > j.value ->> 0))`

// popunjavaRupu javlja da primljeno popunjava rupu u nizu: neka verzija još
// nije u knjizi, a ispod je najveće verzije svog ključa (ledger.FrontierKey).
// Verzija koja je već u knjizi se ne broji, inače bi narastaj rastao svakim
// razgovorom koji ponovi poslano. Kad se ne da pročitati, javlja da
// popunjava: zaboraviti previše košta jedno ponovno slanje. Zove se pod
// bravom upisa, prije upisa.
func (s *Service) popunjavaRupu(ctx context.Context, verzije []ledger.Version) bool {
	polja := make([][3]string, len(verzije))
	for i, v := range verzije {
		polja[i] = [3]string{v.VersionID, v.NodeID, v.Channel}
	}
	sirovo, _ := json.Marshal(polja)
	var popunjava bool
	err := s.db.QueryRowContext(ctx, rupaUNizu, string(sirovo)).Scan(&popunjava)
	return popunjava || err != nil
}

// uvozNaPocetku je narastaj uvoza i ono što je računalo kljuc nakon uvoza
// dobilo, oboje pod istom bravom
func (s *Service) uvozNaPocetku(kljuc string) (uint64, nakonUvoza) {
	s.uvozi.mu.Lock()
	defer s.uvozi.mu.Unlock()
	return s.uvozi.narastaj, s.uvozi.zaCvor[kljuc]
}

// preskok je ono preko čega delta za drugu stranu smije preskočiti, po
// ključu granice: njezina granica odbijenih (njihove) najviše do onoga što
// je u ovom narastaju uvoza već dobila (ograđeno kao u doVlastite). Nakon
// novog uvoza to je ništa, pa delta ide od njezine granice knjige.
func (p pocetakOdbijenih) preskok(njihove map[string]string) map[string]string {
	if p.nakonUvoza.narastaj != p.uvoz {
		return nil
	}
	return doVlastite(njihove, p.nakonUvoza.dosad)
}

// pokriveno je ono što je delta pokrila: po ključu granice najveća poslana
// ili neispravna verzija
func pokriveno(moja deltaMsg, neispravne []ledger.Version) map[string]string {
	return pomakniGranicu(pomakniGranicu(nil, moja.Versions), neispravne)
}

// zapamtiUvoz zapamti što je druga strana u razgovoru dobila (poslano).
// Zove se tek kad je stigla njezina poruka želja, jer je šalje tek nakon
// što je primljeno upisala. Razgovor koji je počeo prije zadnjeg uvoza
// zapamti svoj narastaj, pa sljedeći razgovor granicu odbijenih opet
// zanemari.
func (s *Service) zapamtiUvoz(p pocetakOdbijenih, poslano map[string]string) {
	s.uvozi.mu.Lock()
	defer s.uvozi.mu.Unlock()
	z := s.uvozi.zaCvor[p.kljuc]
	if z.narastaj != p.uvoz {
		z = nakonUvoza{narastaj: p.uvoz}
	}
	z.dosad = spojiGranice(z.dosad, poslano)
	if s.uvozi.zaCvor == nil {
		s.uvozi.zaCvor = map[string]nakonUvoza{}
	}
	s.uvozi.zaCvor[p.kljuc] = z
}
