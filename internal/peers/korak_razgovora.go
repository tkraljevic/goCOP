package peers

// Dodatni korak razgovora (GRANICA §7, sučelje 6): aplikacija prijavi
// oznaku mogućnosti i korak koji uz nju ide. Korak nema svoju poruku ni
// svoje mjesto u exchange, nego proširuje korake želja i sadržaja: njegovi
// otisci idu iza želja spremišta, a njegove stavke iza stavki spremišta.
// Radi samo kad obje strane objave oznaku, razmjena.Dogovor(moja,
// njihova)[ime] == N.
//
// Prijavljena oznaka ide u granicu i u pozdrav WEB synca (mojeMogucnosti),
// a pri čišćenju onoga što je druga strana javila vrijedi kao oznaka ovog
// programa (prednost, znam). Stavka prijavljene vrste koju druga strana
// pošalje nikad ne ide u spremište sadržaja.
//
// U razgovoru (razgovor.go) korak počne iza upisa verzija (zapocniKorak),
// a završi na svakom izlazu iz sadrzajIKraj. Kad korak traži nešto, ograda
// poruke sadržaja je ona za sadržaj (ogradaSadrzaja). Kad korak javi da ima
// još, pozivatelj odmah ponovi razgovor, najviše NajviseDodatnih puta po
// krugu (dalje); pozvani ne ponavlja ništa.
//
// Korak prijavi aplikacija (PrijaviKorak), poslije PostaviProtokol i prije
// Serve; bez prijave razgovor nema dodatnog koraka. Ono što korak javlja
// drugoj strani u znanju ide kroz PRIVREMENU dopunu znanja (DopuniZnanje),
// dok se ne odluči kako korak javlja svoje znanje.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"gocop/internal/razmjena"
)

// PrijavaKoraka je dodatni korak razgovora kakav ga prijavi aplikacija
type PrijavaKoraka struct {
	// Oznaka je ime/N; ide u mogućnosti granice i pozdrava WEB synca
	Oznaka string
	// Vrsta je vrsta stavke u sadržaju; primljena stavka te vrste nikad ne
	// ide u spremište. Zato ne smije biti vrsta koju aplikacija drži u
	// spremištu sadržaja (npr. application/pdf): takav sadržaj druge strane
	// tada ne bi stigao u spremište.
	Vrsta string
	// NajviseDodatnih je koliko dodatnih razgovora po krugu smije doći zbog
	// ImaJos; ponavlja samo pozivatelj
	NajviseDodatnih int
	Korak           DodatniKorak
}

// DodatniKorak je ono što aplikacija daje uz prijavu.
//
// Korak ne smije paničariti, ni ovdje ni u TijekKoraka. Na dolaznom
// razgovoru panika prekine samo taj razgovor (recover u razmjena:
// primiTLS, primiQUIC i tok WEB synca). Na odlaznom razgovoru biblioteka
// paniku ne hvata, pa ona ide do onoga tko je razmjenu pokrenuo. Kad
// razmjena ide kroz syncPeers (krug u pozadini, SyncDue, SyncAll), to
// sruši cijeli čvor. Isto je danas i za OnApplied.
type DodatniKorak interface {
	// Zapocni se zove iza upisa verzija, samo kad Dogovor prođe. Netipiziran
	// nil znači da korak u ovom razgovoru ne radi.
	Zapocni(ctx context.Context, v VezaKoraka) TijekKoraka
	// Odbaci dobije stavke vrste koraka iz razgovora u kojem korak ne radi;
	// zove se samo kad je stigla barem jedna. Ni ove stavke biblioteka ne
	// provjerava (TijekKoraka.Primi).
	Odbaci(stavke []StavkaKoraka)
}

// TijekKoraka je jedan razgovor koraka. Biblioteka ga zove iz goroutine
// razgovora ovim redom: Trazi; Izdvoji i Daj (pozivatelj prije primanja,
// pozvani nakon svog Primi); Primi; ImaJos. Zavrsi se zove uvijek, odmah
// iza Zapocni kao defer. Za paniku vrijedi isto što i uz DodatniKorak.
type TijekKoraka interface {
	// Trazi su otisci koje korak traži, iza želja spremišta; od njih ide
	// najviše o.Stavki, a višak biblioteka odreže. Odrezani otisci nisu
	// traženi, a korak to ne dozna: kad pamti što je tražio, neka da
	// najviše o.Stavki.
	Trazi(o Ostatak) []string
	// Izdvoji odvoji otiske koraka od ostalih; ostali idu spremištu
	Izdvoji(otisci []string) (moji, ostali []string)
	// Daj su stavke koraka za tražene otiske, iza stavki spremišta. Ide ih
	// redom dok stanu u o (bajtovi po len(Podaci) i broj stavki); od prve
	// koja ne stane biblioteka ostatak odreže. Ni to korak ne dozna: kad
	// broji poslano, neka da samo ono što stane u o.
	Daj(otisci []string, o Ostatak) []StavkaKoraka
	// Primi dobije primljene stavke koraka nakon upisa spremišta, i kad
	// nije stigla nijedna. Biblioteka stavke ne provjerava. Dobije sve
	// stavke vrste koraka koje su stigle: i one koje je Trazi dao, a
	// biblioteka ih je odrezala iz želja, i one koje nitko nije tražio.
	// Korak sam provjeri je li stavku tražio, odgovaraju li Podaci otisku i
	// je li duljina dobra.
	Primi(stavke []StavkaKoraka)
	// ImaJos se pita samo kad je sadržaj razmijenjen, prije kraja
	ImaJos() bool
	Zavrsi()
}

// Ostatak je ono što od ograde razmjene (najviseBajtovaPoRazmjeni,
// najviseSadrzajaPoRazmjeni) ostane nakon spremišta. Bajtova smije biti
// manje od nule.
type Ostatak struct {
	Bajtova int64
	Stavki  int
}

// StavkaKoraka je stavka sadržaja koja pripada koraku. Vrstu i Bajtova =
// len(Podaci) upisuje biblioteka, i pri slanju i pri primanju: koliko je
// druga strana najavila, korak ne vidi. Kanala nema.
type StavkaKoraka struct {
	Otisak  string
	Bajtova int
	Podaci  []byte
}

// VezaKoraka je opis veze kojom razgovor ide. Sučelje je da test
// aplikacije podmetne svoju vezu.
type VezaKoraka interface {
	Put() razmjena.Put
	// Pozivatelj: ovo računalo je nazvalo
	Pozivatelj() bool
	// LokalnaMreza: veza ide lokalnom mrežom (putULokalnojMrezi)
	LokalnaMreza() bool
	// IstaLokalnaMreza: druga strana je u istoj lokalnoj mreži
	// (istaLokalnaMreza); računa se tek kad se pita
	IstaLokalnaMreza() bool
}

// imenaBiblioteke su imena oznaka koje daje ili čuva sama biblioteka:
// imena iz NaseMogucnosti osim dijelova karte, web i sadrzaj (rezervirano
// za roj, razmjena/mogucnosti.go). Popis je stalan i ne gleda
// NaseMogucnosti, jer je u njoj zasad i oznaka dijelova karte; da ne
// zaostane za njom, provjerava test.
var imenaBiblioteke = []string{
	strings.TrimSuffix(razmjena.OznakaRazmjena, "/1"), razmjena.ImeGranicaOdbijenih, razmjena.ImeRupe,
	razmjena.ImePotpisVerzije, razmjena.ImeWeb, "sadrzaj",
}

// Greške prijave koraka
var (
	ErrKorakPrijavljen   = errors.New("korak razgovora je već prijavljen")
	ErrOznakaBiblioteke  = errors.New("oznaka koraka ima ime oznake biblioteke")
	ErrOznakaSProtokolom = errors.New("oznaka koraka sadrži ime protokola")
	ErrNeispravanKorak   = errors.New("prijava koraka nije potpuna")
)

// PrijaviKorak prijavi dodatni korak razgovora. Zove se poslije
// PostaviProtokol i prije Serve: ime protokola gleda se u trenutku
// prijave. Zasad smije biti samo jedan korak, pa je druga prijava greška;
// više koraka može doći bez promjene sučelja. Greška je i neispravna
// oznaka (razmjena.ProvjeriOznaku), ime oznake biblioteke, oznaka s imenom
// protokola (ime protokola kao cijeli dio imena oznake, imaDio, osim
// oznake koju biblioteka već objavljuje, oznakaSProtokolom), prazna
// vrsta, NajviseDodatnih manji od nule i korak bez vrijednosti. Vrsta ne
// smije biti vrsta sadržaja iz spremišta (PrijavaKoraka.Vrsta); to se ne
// provjerava.
//
// Ime protokola je ono iz protokol(), isto kojim idu CN, ALPN,
// pronalaženje i potpis verzije: zadano s PostaviProtokol, a bez njega
// Protocol. PRIJELAZNO do N05c, dok biblioteka ima zadano ime protokola.
// Uz zadano ime Protocol se ne gleda, jer tada ne ide u mrežu, pa uz
// gocop-sim prolazi gocop-korak/1.
func (s *Service) PrijaviKorak(p PrijavaKoraka) error {
	if err := s.provjeriPrijavu(p); err != nil {
		return err
	}
	if !s.korak.CompareAndSwap(nil, &p) {
		return fmt.Errorf("%w: %s", ErrKorakPrijavljen, s.korak.Load().Oznaka)
	}
	return nil
}

// provjeriPrijavu odbije prijavu koja ne smije u razgovor
func (s *Service) provjeriPrijavu(p PrijavaKoraka) error {
	ime, _, err := razmjena.ProvjeriOznaku(p.Oznaka)
	switch {
	case err != nil:
		return err
	case slices.Contains(imenaBiblioteke, ime):
		return fmt.Errorf("%w: %s", ErrOznakaBiblioteke, p.Oznaka)
	case s.oznakaSProtokolom(ime, p.Oznaka):
		return fmt.Errorf("%w: %s", ErrOznakaSProtokolom, p.Oznaka)
	case p.Vrsta == "":
		return fmt.Errorf("%w: nema vrste stavke", ErrNeispravanKorak)
	case p.NajviseDodatnih < 0:
		return fmt.Errorf("%w: NajviseDodatnih je %d", ErrNeispravanKorak, p.NajviseDodatnih)
	case p.Korak == nil:
		return fmt.Errorf("%w: nema koraka", ErrNeispravanKorak)
	}
	return nil
}

// oznakaSProtokolom: ime protokola je cijeli dio imena oznake (imaDio), a
// oznaka nije iz razmjena.NaseMogucnosti. Oznaku iz NaseMogucnosti
// biblioteka objavljuje i bez prijave, pa za nju ime protokola u oznaci ne
// vrijedi. Inače bi uz ime protokola koje je cijeli dio imena te oznake
// (npr. "karte" ili "dijelovi" za dijelovi-karte/1) oznaka i dalje išla u
// granicu, a korak ne bi radio; prije nego što je korak išao kroz
// PrijaviKorak, radio je uz svako ime (Odluke (28. 11. 2026.)).
// PRIVREMENO: pitanje se vraća u PR-u 8, kad oznaka izlazi iz
// NaseMogucnosti; tada treba odlučiti vrijedi li iznimka i dalje.
func (s *Service) oznakaSProtokolom(ime, oznaka string) bool {
	return imaDio(ime, s.protokol()) && !slices.Contains(razmjena.NaseMogucnosti(), oznaka)
}

// DopuniZnanje je PRIVREMENO, dok se ne odluči kako korak javlja svoje
// znanje: f upiše u prazno znanje ono što daje aplikacija, a biblioteka iz
// njega uzme samo polje karta, očisti ga kao kartu koju javi druga strana
// (neispravan otisak daje nil, brojevi se ograde) i doda ga znanju ovog
// čvora (mojeZnanje). Ostala polja biblioteka piše sama, pa ih f ne može
// promijeniti. Znanje ide u granicu i u pozdrav WEB synca. f se zove za
// svaku granicu i svaki pozdrav, možda istodobno iz više goroutina, pa mora
// biti siguran za istodobne pozive. Ne smije čekati na mrežu ni dugo
// držati bravu, jer granica i pozdrav dotle čekaju. goCOP-ova dopuna
// (karta bez interneta) radi pod bravom prijenosa karte: pogleda kartu na
// disku, a kad se opis karte promijeni, pospremi i mapu karata. Na tu
// bravu može i pričekati dok je drugi dio prijenosa karte kratko drži, npr.
// dok preimenuje složenu kartu.
//
// f ne smije paničariti. Za granicu vrijedi isto što i uz DodatniKorak.
// Pozdrav WEB synca priprema se izvan razgovora (WebSync.zovi, novaSesija),
// gdje paniku ništa ne hvata, pa ona sruši cijeli čvor.
//
// Nova dopuna zamijeni staru, a nil je makne.
func (s *Service) DopuniZnanje(f func(*razmjena.Znanje)) {
	if f == nil {
		s.dopunaZnanja.Store(nil)
		return
	}
	s.dopunaZnanja.Store(&f)
}

// imaDio kaže je li ime protokola cijeli dio imena oznake: imena se
// rastave na dijelove odvojene crticom, a ime protokola mora biti jedan ili
// više uzastopnih cijelih dijelova (Odluke (28. 11. 2026.)). Broj iza '/'
// ne gleda se, pa uz ime protokola „1” prolazi x/1; uz „karte” pada
// karte-x, a prolazi kartesko.
func imaDio(ime, protokol string) bool {
	return strings.Contains("-"+ime+"-", "-"+protokol+"-")
}

// prednost su oznake koje ovaj program objavljuje, bez web/1: oznake
// biblioteke (razmjena.NaseMogucnosti) i oznaka prijavljenog koraka, bez
// ponavljanja. U ogradi onoga što je druga strana javila one imaju
// prednost (Znanje.OciscenoUz).
func (s *Service) prednost() []string {
	out := razmjena.NaseMogucnosti()
	if p := s.korak.Load(); p != nil && !slices.Contains(out, p.Oznaka) {
		out = append(out, p.Oznaka)
	}
	return out
}

// znam su oznake čija imena ovaj program zna: prednost i web/1
// (razmjena.NepoznateOznakeUz)
func (s *Service) znam() []string {
	return append(s.prednost(), razmjena.OznakaWeb)
}

// vrstaKoraka je vrsta stavke prijavljenog koraka; "" bez prijave
func (s *Service) vrstaKoraka() string {
	if p := s.korak.Load(); p != nil {
		return p.Vrsta
	}
	return ""
}

// najviseDodatnih je koliko dodatnih razgovora po krugu smije doći zbog
// koraka; 0 bez prijave
func (s *Service) najviseDodatnih() int {
	if p := s.korak.Load(); p != nil {
		return p.NajviseDodatnih
	}
	return 0
}

// zapocniKorak počne prijavljeni korak u ovom razgovoru, kad obje strane
// imaju njegovu oznaku (Dogovor daje baš N iz oznake). nil: korak u ovom
// razgovoru ne radi.
func (r *razgovorSCvorom) zapocniKorak(ctx context.Context) TijekKoraka {
	p := r.s.korak.Load()
	if p == nil {
		return nil
	}
	ime, n, err := razmjena.ProvjeriOznaku(p.Oznaka)
	if err != nil || razmjena.Dogovor(r.moja.Mogucnosti, r.theirs.Mogucnosti)[ime] != n {
		return nil
	}
	return p.Korak.Zapocni(ctx, vezaRazgovora{c: r.c, zvao: r.initiator})
}

// zavrsiKorak završi korak koji je ovaj razgovor počeo
func (r *razgovorSCvorom) zavrsiKorak() {
	if r.korak != nil {
		r.korak.Zavrsi()
	}
}

// stavkeZaKorak su primljene stavke koraka kako ih korak dobije: Bajtova
// je len(Podaci), a ne ono što je druga strana najavila
func stavkeZaKorak(stavke []stavkaSadrzaja) []StavkaKoraka {
	var out []StavkaKoraka
	for _, st := range stavke {
		out = append(out, StavkaKoraka{Otisak: st.Otisak, Bajtova: len(st.Podaci), Podaci: st.Podaci})
	}
	return out
}

// stavkeKoraka su stavke koraka kako idu u poruku sadržaja: vrsta
// prijavljenog koraka i Bajtova = len(Podaci), bez kanala
func (s *Service) stavkeKoraka(stavke []StavkaKoraka) []stavkaSadrzaja {
	vrsta := s.vrstaKoraka()
	var out []stavkaSadrzaja
	for _, st := range stavke {
		out = append(out, stavkaSadrzaja{Otisak: st.Otisak, Vrsta: vrsta, Bajtova: len(st.Podaci), Podaci: st.Podaci})
	}
	return out
}

// predajKoraku preda primljene stavke koraka koraku koji u razgovoru radi,
// i kad nije stigla nijedna. Kad korak ne radi, a stavke su stigle, ide mu
// Odbaci. Stavke se ovdje ne provjeravaju: idu i one koje je
// otisciUOstatku izrezao iz želja i one koje nitko nije tražio. Traženo,
// otisak i duljinu provjerava korak (TijekKoraka.Primi).
func (r *razgovorSCvorom) predajKoraku(stavke []stavkaSadrzaja) {
	if r.korak != nil {
		r.korak.Primi(stavkeZaKorak(stavke))
		return
	}
	if p := r.s.korak.Load(); p != nil && len(stavke) > 0 {
		p.Korak.Odbaci(stavkeZaKorak(stavke))
	}
}

// zapamtiKorak: korak javlja da ima još, pa pozivatelj razgovor s istim
// čvorom ponavlja odmah (josKoraka, dalje)
func (r *razgovorSCvorom) zapamtiKorak() {
	if r.korak != nil && r.korak.ImaJos() {
		r.s.korakTece.Store(r.c, true)
	}
}

// ostatakOgrade je ono što od ograde razmjene ostane nakon bajtova i
// stavki iz spremišta
func ostatakOgrade(bajtova, stavki int) Ostatak {
	return Ostatak{Bajtova: int64(najviseBajtovaPoRazmjeni - bajtova), Stavki: najviseSadrzajaPoRazmjeni - stavki}
}

// mojeZelje su otisci sadržaja iz spremišta koje ovaj čvor traži, a iza
// njih otisci koje traži dodatni korak, u onome što od ograde ostane
func (r *razgovorSCvorom) mojeZelje(ctx context.Context) []string {
	zelje, bajtova := r.s.zeljeniSBajtovima(ctx, r.myWants)
	r.mojiSadrzaji = poOtisku(zelje)
	otisci := otisciZelja(zelje)
	if r.korak == nil {
		return otisci
	}
	o := ostatakOgrade(bajtova, len(otisci))
	koraka := otisciUOstatku(r.korak.Trazi(o), o)
	r.trazioKorak = len(koraka) > 0
	return append(otisci, koraka...)
}

// sadrzajZaDrugu: od traženoga najprije sadržaj iz spremišta, pa stavke
// dodatnog koraka u onome što od ograde ostane
func (r *razgovorSCvorom) sadrzajZaDrugu(ctx context.Context, otisci []string) []stavkaSadrzaja {
	if r.korak == nil {
		return r.s.sadrzajZa(ctx, otisci, r.theirs.Wants)
	}
	koraka, ostali := r.korak.Izdvoji(otisci)
	out := r.s.sadrzajZa(ctx, ostali, r.theirs.Wants)
	bajtova := 0
	for _, st := range out {
		bajtova += st.Bajtova
	}
	o := ostatakOgrade(bajtova, len(out))
	return append(out, r.s.stavkeKoraka(stavkeUOstatku(r.korak.Daj(koraka, o), o))...)
}

// otisciUOstatku: od otisaka koje korak traži ide najviše o.Stavki, redom
// kako ih je korak dao
func otisciUOstatku(otisci []string, o Ostatak) []string {
	return otisci[:min(len(otisci), max(o.Stavki, 0))]
}

// stavkeUOstatku: od stavki koje korak daje idu one redom dok stanu u o,
// po len(Podaci) i po broju stavki; prva koja ne stane prekida popis. Tako
// poruka sadržaja ostaje unutar ograde razmjene i kad korak da više.
func stavkeUOstatku(stavke []StavkaKoraka, o Ostatak) []StavkaKoraka {
	bajtova := o.Bajtova
	for i, st := range stavke {
		if i >= o.Stavki || int64(len(st.Podaci)) > bajtova {
			return stavke[:i]
		}
		bajtova -= int64(len(st.Podaci))
	}
	return stavke
}

// razdvojiPrimljeno odvoji primljene stavke prijavljene vrste koraka od
// ostalog sadržaja: takva stavka nikad ne ide u spremište sadržaja. Bez
// prijave (vrsta "") sve je ostalo.
func razdvojiPrimljeno(stavke []stavkaSadrzaja, vrsta string) (koraka, ostalo []stavkaSadrzaja) {
	for _, st := range stavke {
		if vrsta != "" && st.Vrsta == vrsta {
			koraka = append(koraka, st)
		} else {
			ostalo = append(ostalo, st)
		}
	}
	return koraka, ostalo
}
