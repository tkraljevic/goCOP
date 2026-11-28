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
// Razgovor koraka zasad još vodi karta_razgovor.go, a jedini prijavljeni
// korak je privremena prijava karte koju napravi NewService (korakKarte u
// karta_razgovor.go).

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

// DodatniKorak je ono što aplikacija daje uz prijavu
type DodatniKorak interface {
	// Zapocni se zove iza upisa verzija, samo kad Dogovor prođe. Netipiziran
	// nil znači da korak u ovom razgovoru ne radi.
	Zapocni(ctx context.Context, v VezaKoraka) TijekKoraka
	// Odbaci dobije stavke vrste koraka iz razgovora u kojem korak ne radi;
	// zove se samo kad je stigla barem jedna
	Odbaci(stavke []StavkaKoraka)
}

// TijekKoraka je jedan razgovor koraka. Biblioteka ga zove iz goroutine
// razgovora ovim redom: Trazi; Izdvoji i Daj (pozivatelj prije primanja,
// pozvani nakon svog Primi); Primi; ImaJos. Zavrsi se zove uvijek, odmah
// iza Zapocni kao defer.
type TijekKoraka interface {
	// Trazi su otisci koje korak traži, iza želja spremišta
	Trazi(o Ostatak) []string
	// Izdvoji odvoji otiske koraka od ostalih; ostali idu spremištu
	Izdvoji(otisci []string) (moji, ostali []string)
	// Daj su stavke koraka za tražene otiske, iza stavki spremišta
	Daj(otisci []string, o Ostatak) []StavkaKoraka
	// Primi dobije primljene stavke koraka nakon upisa spremišta, i kad
	// nije stigla nijedna
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

// StavkaKoraka je stavka sadržaja koja pripada koraku. Vrstu i, pri
// slanju, Bajtova = len(Podaci) upisuje biblioteka; kanala nema.
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

// imenaBiblioteke su imena oznaka koje daje sama biblioteka. Popis je
// stalan i ne gleda NaseMogucnosti, jer je u njoj zasad i oznaka dijelova
// karte.
var imenaBiblioteke = []string{
	strings.TrimSuffix(razmjena.OznakaRazmjena, "/1"), razmjena.ImeGranicaOdbijenih, razmjena.ImeRupe,
	razmjena.ImePotpisVerzije, razmjena.ImeWeb,
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
// protokola, prazna vrsta, NajviseDodatnih manji od nule i korak bez
// vrijednosti. Vrsta ne smije biti vrsta sadržaja iz spremišta
// (PrijavaKoraka.Vrsta); to se ne provjerava.
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
	case strings.Contains(p.Oznaka, s.protokol()):
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
