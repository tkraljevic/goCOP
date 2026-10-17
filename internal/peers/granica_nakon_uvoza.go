package peers

// Granica odbijenih nakon uvoza (G4 u docs/plan-web-sync.md §3). Granica
// odbijenih vrijedi jer pošiljatelj u svakom ključu šalje neprekinut niz
// svojih verzija iznad granice primatelja (granica_odbijenih.go). Uvozom
// (arhiva knjige, ImportFile, i COP paket, UveziCop) pošiljatelj može
// dobiti i verziju ispod granice odbijenih koju prije nije imao (kasno).
// Primatelj je nije ni vidio ni odbio, a po granici odbijenih ona se
// preskoči. Kad u istom ključu stigne i novija verzija koju primatelj drži,
// njegova granica knjige prijeđe preko kasnog, pa ono ne stigne nikad.
//
// Zato pošiljatelj broji uvoze: narastaj uvoza raste sa svakim uvozom koji
// upiše barem jednu verziju (zabiljeziUvoz). Za svako računalo kojem šalje
// pamti je li ono dobilo sve što je pošiljatelj imao nakon zadnjeg uvoza.
// Dok nije, delta ide od granice knjige tog računala, a preskače samo ono
// što je ono i odbilo i nakon uvoza već dobilo (dosad), da ključ s tisućama
// odbijenih ne zapne kao prije G4. Računalo je dobilo sve kad je delta
// prošla neskraćena (pokriveno) i kad je stigla njegova poruka želja: šalje
// je tek nakon što je primljeno upisalo i zapamtilo što je odbilo
// (zapamtiUvoz). Razgovor koji je pao ili u kojem je upis druge strane bio
// zauzet tako se ne broji.
//
// Pamti se samo u memoriji, po javnom ključu druge strane, kao i granica
// odbijenih. Nakon ponovnog pokretanja ništa nije zapamćeno, pa prvi
// razgovor sa svakim računalom granicu zanemari: to je jedno ponovno slanje,
// kao kad se ponovno pokrene primatelj. Narastaj razgovor uzme prije prve
// poruke (pocetakOdbijenih), pa uvoz usred razgovora vrijedi za sljedeći.
//
// Razmjenom pošiljatelj dobiva verzije iznad svoje granice knjige s početka
// razgovora, pa ispod granice odbijenih samo kad je prije imao niz s rupom
// (izvor nije imao verziju iz sredine niza), a rupu sad popuni drugi izvor.
// To može kad dva istodobna razgovora donesu isti ključ i kad se kanal
// obrisan brisanjem kanala (PurgeChannel) opet počne pratiti. Ovo to ne
// pokriva.

import (
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

// nakonUvoza je ono što je jedno računalo dobilo nakon uvoza narastaja
// narastaj: sve, ili po ključu granice dosad, do koje verzije je dobilo sve
type nakonUvoza struct {
	narastaj uint64
	sve      bool
	dosad    map[string]string
}

// pokrivenoDeltom je ono što je delta jednog razgovora pokrila: po ključu
// granice najveća poslana ili neispravna verzija, i je li nosila sve
type pokrivenoDeltom struct {
	granica map[string]string
	sve     bool
}

// zabiljeziUvoz digne narastaj uvoza kad je uvoz upisao barem jednu
// verziju. Zove se bez uvjeta, tek nakon upisa (primijeniPodBravom), pa
// razgovor koji vidi novi narastaj u delti vidi i uvezeno.
func (s *Service) zabiljeziUvoz(upisano int) {
	if upisano == 0 {
		return
	}
	s.uvozi.mu.Lock()
	defer s.uvozi.mu.Unlock()
	s.uvozi.narastaj++
}

// uvozNaPocetku je narastaj uvoza i ono što je računalo kljuc nakon uvoza
// dobilo, oboje pod istom bravom
func (s *Service) uvozNaPocetku(kljuc string) (uint64, nakonUvoza) {
	s.uvozi.mu.Lock()
	defer s.uvozi.mu.Unlock()
	return s.uvozi.narastaj, s.uvozi.zaCvor[kljuc]
}

// preskok je ono preko čega delta za drugu stranu smije preskočiti, po
// ključu granice. Kad je druga strana dobila sve nakon zadnjeg uvoza, to je
// njezina granica odbijenih (njihove). Dok nije, to je ta granica najviše do
// onoga što je nakon uvoza već dobila (ograđeno kao u doVlastite), a nakon
// novog uvoza ništa, pa delta ide od njezine granice knjige.
func (p pocetakOdbijenih) preskok(njihove map[string]string) map[string]string {
	switch {
	case p.nakonUvoza.narastaj != p.uvoz:
		return nil
	case p.nakonUvoza.sve:
		return njihove
	}
	return doVlastite(njihove, p.nakonUvoza.dosad)
}

// pokriveno je ono što je delta pokrila. Sve je nosila kad nije skraćena po
// bajtovima (Skraceno) ni po broju verzija u ključu: uz manje od
// NajviseVerzijaPoRazmjeni verzija ukupno nijedan ključ nije pun.
func pokriveno(moja deltaMsg, neispravne []ledger.Version) pokrivenoDeltom {
	return pokrivenoDeltom{
		granica: pomakniGranicu(pomakniGranicu(nil, moja.Versions), neispravne),
		sve:     !moja.Skraceno && len(moja.Versions)+len(neispravne) < NajviseVerzijaPoRazmjeni,
	}
}

// zapamtiUvoz zapamti što je druga strana u razgovoru dobila. Zove se tek
// kad je stigla njezina poruka želja, jer je šalje tek nakon što je
// primljeno upisala. Razgovor koji je počeo prije zadnjeg uvoza zapamti
// svoj narastaj, pa sljedeći razgovor granicu odbijenih opet zanemari.
func (s *Service) zapamtiUvoz(p pocetakOdbijenih, d pokrivenoDeltom) {
	s.uvozi.mu.Lock()
	defer s.uvozi.mu.Unlock()
	z := s.uvozi.zaCvor[p.kljuc]
	if z.narastaj != p.uvoz {
		z = nakonUvoza{narastaj: p.uvoz}
	}
	if !z.sve {
		z = nakonUvoza{narastaj: p.uvoz, sve: d.sve, dosad: spojiGranice(z.dosad, d.granica)}
	}
	if s.uvozi.zaCvor == nil {
		s.uvozi.zaCvor = map[string]nakonUvoza{}
	}
	s.uvozi.zaCvor[p.kljuc] = z
}
