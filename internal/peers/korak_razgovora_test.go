package peers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/razmjena"
	"gocop/internal/sadrzaj"
)

// probniKorak je korak razgovora bez ijedne riječi karte koji u razgovoru
// nikad ne počne
type probniKorak struct{}

func (*probniKorak) Zapocni(context.Context, VezaKoraka) TijekKoraka { return nil }
func (*probniKorak) Odbaci([]StavkaKoraka)                           {}

// probnaPrijava je ispravna prijava probnog koraka
func probnaPrijava() PrijavaKoraka {
	return PrijavaKoraka{Oznaka: "probni-korak/1", Vrsta: "probna-stavka", NajviseDodatnih: 3, Korak: &probniKorak{}}
}

// bezPrijave je čvor bez prijavljenog koraka. NewService ne prijavljuje
// nijedan korak, pa se tu samo provjeri da ga nema.
func bezPrijave(t *testing.T, id string) *Service {
	t.Helper()
	s := rubniCvor(t, id)
	if p := s.korak.Load(); p != nil {
		t.Fatalf("NewService je prijavio korak: %+v", p)
	}
	return s
}

// bezKarte je opis zadane karte kad karte nema
func bezKarte() *models.VektorskaKarta { return nil }

// Kartu više ne prijavljuje NewService, nego PostaviKarte: bez nje korak
// nije prijavljen, a oznake u granici su iste, jer je dijelovi-karte/1 još
// u razmjena.NaseMogucnosti. Prijava karte ima oznaku koja je i prije išla
// u granicu, pa se popis oznaka ne mijenja, druga prijava ne prolazi, a
// drugi PostaviKarte ne prijavljuje ponovno.
func TestPrijaviKorakNewServicePrijaviKartu(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	if p := s.korak.Load(); p != nil {
		t.Fatalf("NewService je prijavio korak: %+v", p)
	}
	if got := s.mojeMogucnosti(); !slices.Equal(got, razmjena.NaseMogucnosti()) {
		t.Errorf("mogućnosti bez prijave karte: %v", got)
	}
	s.PostaviKarte(t.TempDir(), bezKarte, nil)
	p := s.korak.Load()
	if p == nil || p.Oznaka != razmjena.OznakaDijeloviKarte || p.Vrsta != VrstaDijelaKarte || p.NajviseDodatnih != najviseRazgovoraZaKartu {
		t.Fatalf("prijava iz PostaviKarte: %+v", p)
	}
	if err := s.provjeriPrijavu(*p); err != nil {
		t.Errorf("prijava iz PostaviKarte ne prolazi provjeru: %v", err)
	}
	if k, ok := p.Korak.(korakKarte); !ok || k.p != s.karte.Load() {
		t.Errorf("korak iz PostaviKarte: %T", p.Korak)
	}
	if got := s.mojeMogucnosti(); !slices.Equal(got, razmjena.NaseMogucnosti()) {
		t.Errorf("mogućnosti uz prijavu karte: %v", got)
	}
	if got := s.prednost(); !slices.Equal(got, razmjena.NaseMogucnosti()) {
		t.Errorf("prednost: %v", got)
	}
	if got := s.znam(); !slices.Equal(got, append(razmjena.NaseMogucnosti(), razmjena.OznakaWeb)) {
		t.Errorf("znam: %v", got)
	}
	if s.vrstaKoraka() != VrstaDijelaKarte {
		t.Errorf("vrsta: %q", s.vrstaKoraka())
	}
	err := s.PrijaviKorak(probnaPrijava())
	if !errors.Is(err, ErrKorakPrijavljen) || s.korak.Load() != p {
		t.Errorf("druga prijava: %v, prijavljeno %+v", err, s.korak.Load())
	}
	prijenos := s.karte.Load()
	s.PostaviKarte(t.TempDir(), bezKarte, nil)
	if s.korak.Load() != p || s.karte.Load() != prijenos {
		t.Error("drugi PostaviKarte je prijavio kartu ponovno")
	}
	// kad je korak već prijavljen, PostaviKarte kartu ne prijavi, nego to
	// zapiše u dnevnik; prijenos karte ipak postoji
	drugi := bezPrijave(t, "laptop-baranja")
	probni := probnaPrijava()
	if err := drugi.PrijaviKorak(probni); err != nil {
		t.Fatal(err)
	}
	drugi.PostaviKarte(t.TempDir(), bezKarte, nil)
	if drugi.korak.Load().Oznaka != probni.Oznaka || drugi.karte.Load() == nil || drugi.dopunaZnanja.Load() != nil {
		t.Errorf("PostaviKarte uz prijavljen korak: %+v", drugi.korak.Load())
	}
	// Odbaci samo zapiše redak u dnevnik
	korakKarte{&PrijenosKarte{}}.Odbaci([]StavkaKoraka{{Otisak: "a"}})
}

// Nulta vrijednost PrijenosKarte nema ograda po putu ni stanja: nema
// stanja ni znanja, a Prijavi je odbije, i kad joj je Postavi dao mapu i
// opis. Inače bi oznaka i polje karta išli u granicu, a nijedan dio ne bi
// išao ni u jednom smjeru.
func TestPrijenosKarteNultaVrijednost(t *testing.T) {
	prazan := &PrijenosKarte{}
	if st, z := prazan.Stanje(ctxRub), prazan.Znanje(); st != (StanjeKarte{}) || z != nil {
		t.Errorf("prijenos bez stanja: %+v, znanje %+v", st, z)
	}
	s := bezPrijave(t, "cop-osijek")
	if err := prazan.Prijavi(s); !errors.Is(err, errNijeNoviPrijenos) {
		t.Errorf("Prijavi nulte vrijednosti: %v", err)
	}
	prazan.Postavi(t.TempDir(), bezKarte, nil)
	if err := prazan.Prijavi(s); !errors.Is(err, errNijeNoviPrijenos) {
		t.Errorf("Prijavi nulte vrijednosti uz Postavi: %v", err)
	}
	if s.korak.Load() != nil || s.dopunaZnanja.Load() != nil {
		t.Errorf("odbijena prijava je ostavila korak %+v ili dopunu", s.korak.Load())
	}
	if err := NoviPrijenosKarte(t.TempDir(), bezKarte, nil).Prijavi(s); err != nil {
		t.Errorf("Prijavi prijenosa iz NoviPrijenosKarte: %v", err)
	}
}

// PostaviKarte ne uzima s.mu, a istodobni pozivi spreme jedan prijenos,
// prijave ga jednom i ne zapišu u dnevnik da karta ne ide razmjenom
func TestPostaviKarteIstodobno(t *testing.T) {
	var dnevnik bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&dnevnik)
	t.Cleanup(func() { log.SetOutput(stari) })
	s := rubniCvor(t, "cop-osijek")
	mape := map[string]bool{}
	for range 8 {
		mape[t.TempDir()] = true
	}
	s.mu.Lock()
	var wg sync.WaitGroup
	for mapa := range mape {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.PostaviKarte(mapa, bezKarte, nil)
		}()
	}
	gotovo := make(chan struct{})
	go func() { wg.Wait(); close(gotovo) }()
	select {
	case <-gotovo:
	case <-time.After(10 * time.Second):
		s.mu.Unlock()
		t.Fatal("PostaviKarte čeka na s.mu")
	}
	s.mu.Unlock()
	p := s.karte.Load()
	k := s.korak.Load()
	if p == nil || k == nil || k.Oznaka != razmjena.OznakaDijeloviKarte {
		t.Fatalf("prijenos %v, prijava %+v", p, k)
	}
	if kk, ok := k.Korak.(korakKarte); !ok || kk.p != p {
		t.Errorf("prijavljen je drugi prijenos: %T", k.Korak)
	}
	if s.dopunaZnanja.Load() == nil {
		t.Error("nema dopune znanja")
	}
	if d := p.d.Load(); d == nil || !mape[d.mapa] {
		t.Errorf("stanje prijenosa: %+v", d)
	}
	if strings.Contains(dnevnik.String(), "karta bez interneta") {
		t.Errorf("dnevnik: %s", dnevnik.String())
	}
}

// Odluke (28. 11. 2026.): ime protokola koje je dio oznake
// dijelovi-karte/1 ne smeta prijavi karte, jer tu oznaku biblioteka
// objavljuje i bez prijave. Uz svako ime je isto kao uz zadano gocop:
// korak i dopuna su postavljeni, vrsta je karta-dio, granica i pozdrav WEB
// synca nose polje karta, a primljena stavka karta-dio ne ide u spremište,
// ni s otiskom PDF-a.
func TestPrijavaKarteUzImeProtokolaIzOznake(t *testing.T) {
	for _, ime := range []string{Protocol, "karte", "dijelovi", "e", "1", "dijelovi-karte"} {
		k, _, izvor := kartaUDijelovima(t, 1000, 128, 46)
		o := &opisKarte{}
		o.postavi(&k)
		s := rubniCvor(t, "cop-osijek")
		s.PostaviProtokol(ime)
		s.PostaviKarte(izvor, o.citaj, nil)
		smiri(s.karte.Load().d.Load())
		p := s.korak.Load()
		if p == nil || p.Oznaka != razmjena.OznakaDijeloviKarte || p.Vrsta != VrstaDijelaKarte || p.NajviseDodatnih != najviseRazgovoraZaKartu {
			t.Fatalf("%s: prijava karte: %+v", ime, p)
		}
		if kk, ok := p.Korak.(korakKarte); !ok || kk.p != s.karte.Load() {
			t.Errorf("%s: korak: %T", ime, p.Korak)
		}
		if s.dopunaZnanja.Load() == nil || s.vrstaKoraka() != VrstaDijelaKarte {
			t.Errorf("%s: dopuna %v, vrsta %q", ime, s.dopunaZnanja.Load() != nil, s.vrstaKoraka())
		}
		if got := s.mojeMogucnosti(); !slices.Equal(got, razmjena.NaseMogucnosti()) {
			t.Errorf("%s: mogućnosti: %v", ime, got)
		}
		ocekivano := s.znanjeKarte()
		if ocekivano == nil || ocekivano.Otisak != k.Otisak || !ocekivano.Slozena {
			t.Fatalf("%s: znanje o karti: %+v", ime, ocekivano)
		}
		if z := s.mojeZnanje(ctxRub); !reflect.DeepEqual(z.Karta, ocekivano) {
			t.Errorf("%s: u granici: %+v, a znanjeKarte %+v", ime, z.Karta, ocekivano)
		}
		if z := s.znanjeZaPozdrav(); !reflect.DeepEqual(z.Karta, ocekivano) {
			t.Errorf("%s: u pozdravu WEB synca: %+v, a znanjeKarte %+v", ime, z.Karta, ocekivano)
		}
		stavka := stavkaSadrzaja{Otisak: otisakTudji, Vrsta: VrstaDijelaKarte, Bajtova: len(pdfTudji), Podaci: []byte(pdfTudji)}
		if koraka, ostalo := razdvojiPrimljeno([]stavkaSadrzaja{stavka}, s.vrstaKoraka()); len(koraka) != 1 || len(ostalo) != 0 {
			t.Errorf("%s: karta-dio ide u spremište: %v", ime, ostalo)
		}
	}
}

// Iznimka vrijedi samo za oznaku iz razmjena.NaseMogucnosti: tuđa oznaka s
// imenom protokola i dalje pada, i kad joj je ime ono iz NaseMogucnosti, a
// broj drugi. Oznaka iz NaseMogucnosti prolazi i uz probni korak, bez
// karte.
func TestPrijaviKorakImeProtokolaUTudjojOznaci(t *testing.T) {
	for _, ime := range []string{"karte", "dijelovi", "e", "1", "dijelovi-karte"} {
		s := bezPrijave(t, "cop-osijek")
		s.PostaviProtokol(ime)
		for _, oznaka := range []string{ime + "-korak/1", "dijelovi-karte/21"} {
			p := probnaPrijava()
			p.Oznaka = oznaka
			if err := s.PrijaviKorak(p); !errors.Is(err, ErrOznakaSProtokolom) {
				t.Errorf("%s, %s: %v", ime, oznaka, err)
			}
			if s.korak.Load() != nil {
				t.Fatalf("%s, %s: prijava je ostala", ime, oznaka)
			}
		}
		// ostale provjere vrijede i za oznaku iz NaseMogucnosti
		p := probnaPrijava()
		p.Oznaka, p.Vrsta = razmjena.OznakaDijeloviKarte, ""
		if err := s.PrijaviKorak(p); !errors.Is(err, ErrNeispravanKorak) {
			t.Errorf("%s: oznaka biblioteke bez vrste: %v", ime, err)
		}
		// a ispravna prijava s oznakom iz NaseMogucnosti prolazi i bez
		// karte, uz probni korak
		p = probnaPrijava()
		p.Oznaka = razmjena.OznakaDijeloviKarte
		if err := s.PrijaviKorak(p); err != nil {
			t.Errorf("%s: probni korak s oznakom iz NaseMogucnosti: %v", ime, err)
		}
		if k := s.korak.Load(); k == nil || k.Oznaka != p.Oznaka || k.Korak != p.Korak {
			t.Errorf("%s: prijavljeno %+v", ime, k)
		}
	}
}

// Neispravna prijava se odbije i ne ostavlja trag
func TestPrijaviKorakOdbija(t *testing.T) {
	s := bezPrijave(t, "cop-osijek")
	s.PostaviProtokol("proba-n04")
	for _, k := range []struct {
		ime     string
		izmjena func(*PrijavaKoraka)
		greska  error
	}{
		{"bez broja", func(p *PrijavaKoraka) { p.Oznaka = "probni-korak" }, razmjena.ErrNeispravnaOznaka},
		{"nula", func(p *PrijavaKoraka) { p.Oznaka = "probni-korak/0" }, razmjena.ErrNeispravnaOznaka},
		{"velika slova", func(p *PrijavaKoraka) { p.Oznaka = "Probni/1" }, razmjena.ErrNeispravnaOznaka},
		{"prazna", func(p *PrijavaKoraka) { p.Oznaka = "" }, razmjena.ErrNeispravnaOznaka},
		{"razmjena", func(p *PrijavaKoraka) { p.Oznaka = "razmjena/2" }, ErrOznakaBiblioteke},
		{"granica odbijenih", func(p *PrijavaKoraka) { p.Oznaka = razmjena.OznakaGranicaOdbijenih }, ErrOznakaBiblioteke},
		{"rupe", func(p *PrijavaKoraka) { p.Oznaka = "rupe/2" }, ErrOznakaBiblioteke},
		{"potpis verzije", func(p *PrijavaKoraka) { p.Oznaka = razmjena.OznakaPotpisVerzije }, ErrOznakaBiblioteke},
		{"web", func(p *PrijavaKoraka) { p.Oznaka = razmjena.OznakaWeb }, ErrOznakaBiblioteke},
		{"sadrzaj", func(p *PrijavaKoraka) { p.Oznaka = "sadrzaj/1" }, ErrOznakaBiblioteke},
		{"ime protokola", func(p *PrijavaKoraka) { p.Oznaka = "proba-n04-korak/1" }, ErrOznakaSProtokolom},
		{"bez vrste", func(p *PrijavaKoraka) { p.Vrsta = "" }, ErrNeispravanKorak},
		{"manje od nule", func(p *PrijavaKoraka) { p.NajviseDodatnih = -1 }, ErrNeispravanKorak},
		{"bez koraka", func(p *PrijavaKoraka) { p.Korak = nil }, ErrNeispravanKorak},
	} {
		p := probnaPrijava()
		k.izmjena(&p)
		if err := s.PrijaviKorak(p); !errors.Is(err, k.greska) {
			t.Errorf("%s: %v", k.ime, err)
		}
		if s.korak.Load() != nil {
			t.Fatalf("%s: prijava je ostala", k.ime)
		}
	}
	// ime protokola gleda se u trenutku prijave
	s.PostaviProtokol(Protocol)
	p := probnaPrijava()
	p.Oznaka = "proba-n04-korak/1"
	if err := s.PrijaviKorak(p); err != nil {
		t.Errorf("uz drugo ime protokola: %v", err)
	}
	// PRIJELAZNO do N05c: bez PostaviProtokol vrijedi zadano ime gocop
	if err := bezPrijave(t, "laptop-baranja").PrijaviKorak(PrijavaKoraka{Oznaka: "gocop-korak/1", Vrsta: "x", Korak: &probniKorak{}}); !errors.Is(err, ErrOznakaSProtokolom) {
		t.Errorf("uz zadano ime protokola: %v", err)
	}
}

// Ime protokola odbija oznaku samo kad je cijeli dio imena oznake, ne kad
// se nađe bilo gdje u oznaci (Odluke (28. 11. 2026.)); prazno ime protokola
// je čvor bez PostaviProtokol, uz zadano ime gocop
func TestPrijaviKorakImeProtokolaKaoDio(t *testing.T) {
	for _, k := range []struct {
		protokol, oznaka string
		greska           error
	}{
		{"karte", "probni-korak/1", nil},
		{"karte", "kartesko-korak/1", nil},
		{"karte", "karte/1", ErrOznakaSProtokolom},
		{"karte", "x-karte/1", ErrOznakaSProtokolom},
		{"karte", "karte-x/1", ErrOznakaSProtokolom},
		{"karte", "x-karte-y/1", ErrOznakaSProtokolom},
		{"1", "x/1", nil},
		{"1", "x-1/1", ErrOznakaSProtokolom},
		{"proba-n04", "proba-korak/1", nil},
		{"proba-n04", "x-proba-n04/1", ErrOznakaSProtokolom},
		{"gocop-sim", "gocop-korak/1", nil},
		{"gocop", "gocop-korak/1", ErrOznakaSProtokolom},
		{"", "gocop-korak/1", ErrOznakaSProtokolom},
		{"", "probni-korak/1", nil},
	} {
		s := bezPrijave(t, "cop-osijek")
		if k.protokol != "" {
			s.PostaviProtokol(k.protokol)
		}
		p := probnaPrijava()
		p.Oznaka = k.oznaka
		err := s.PrijaviKorak(p)
		if k.greska == nil && err != nil || k.greska != nil && !errors.Is(err, k.greska) {
			t.Errorf("protokol %q, oznaka %s: %v, treba %v", k.protokol, k.oznaka, err, k.greska)
		}
		if prijavljeno := s.korak.Load() != nil; prijavljeno != (k.greska == nil) {
			t.Errorf("protokol %q, oznaka %s: prijavljeno %v", k.protokol, k.oznaka, prijavljeno)
		}
	}
}

// Popis imena biblioteke ne zaostaje za NaseMogucnosti: ima svako ime iz
// nje osim dijelova karte, koje prijavljuje aplikacija, i uz to web i
// sadrzaj
func TestImenaBibliotekeUzNaseMogucnosti(t *testing.T) {
	treba := []string{razmjena.ImeWeb, "sadrzaj"}
	for _, o := range razmjena.NaseMogucnosti() {
		ime, _, err := razmjena.ProvjeriOznaku(o)
		if err != nil {
			t.Fatal(err)
		}
		if ime != razmjena.ImeDijeloviKarte {
			treba = append(treba, ime)
		}
	}
	got := slices.Clone(imenaBiblioteke)
	slices.Sort(got)
	slices.Sort(treba)
	if !slices.Equal(got, treba) {
		t.Errorf("imenaBiblioteke: %v, treba %v", got, treba)
	}
}

// Prijavljena oznaka ide u granicu poredana, bez ponavljanja, a uz WEB sync
// i web/1; bez prijave popis je onaj biblioteke
func TestPrijaviKorakMogucnosti(t *testing.T) {
	s := bezPrijave(t, "cop-osijek")
	if got := s.mojeMogucnosti(); !slices.Equal(got, razmjena.NaseMogucnosti()) {
		t.Errorf("bez prijave: %v", got)
	}
	if s.vrstaKoraka() != "" {
		t.Errorf("vrsta bez prijave: %q", s.vrstaKoraka())
	}
	if err := s.PrijaviKorak(probnaPrijava()); err != nil {
		t.Fatal(err)
	}
	ocekivano := append(razmjena.NaseMogucnosti(), "probni-korak/1")
	slices.Sort(ocekivano)
	if got := s.mojeMogucnosti(); !slices.Equal(got, ocekivano) {
		t.Errorf("uz prijavu: %v", got)
	}
	if got := s.mojeZnanje(ctxRub).Mogucnosti; !slices.Equal(got, ocekivano) {
		t.Errorf("u granici: %v", got)
	}
	s.web.Store(&webSyncServisa{ctx: ctxRub, ukljucen: true})
	uzWeb := append(slices.Clone(ocekivano), razmjena.OznakaWeb)
	slices.Sort(uzWeb)
	if got := s.mojeMogucnosti(); !slices.Equal(got, uzWeb) {
		t.Errorf("uz WEB sync: %v", got)
	}
	if got := s.prednost(); !slices.Equal(got, append(razmjena.NaseMogucnosti(), "probni-korak/1")) {
		t.Errorf("prednost: %v", got)
	}
}

// Prijavljena oznaka druge strane nije nepoznata i ne ispada iz ograde od
// 32 oznake, ni u ishodu razmjene ni u spremljenom znanju
func TestPrijaviKorakZnanjeDrugeStrane(t *testing.T) {
	s := bezPrijave(t, "cop-osijek")
	if err := s.PrijaviKorak(probnaPrijava()); err != nil {
		t.Fatal(err)
	}
	mnogo := []string{"probni-korak/1"}
	for i := range 40 {
		mnogo = append(mnogo, fmt.Sprintf("a%02d/1", i))
	}
	o := ishodRazmjene(0, 0, frontierMsg{Program: "x", Znanje: razmjena.Znanje{Mogucnosti: mnogo}}, s.prednost(), 0, nil)
	if !slices.Contains(o.znanje.Mogucnosti, "probni-korak/1") || len(o.znanje.Mogucnosti) != 32 {
		t.Errorf("u ishodu: %v", o.znanje.Mogucnosti)
	}
	if err := s.SavePeer(ctxRub, Peer{NodeID: "ured-vukovar", PublicKey: javniKljuc(t)}); err != nil {
		t.Fatal(err)
	}
	s.zapamtiZnanje(ctxRub, "ured-vukovar", o)
	st, err := s.syncStates(ctxRub)
	if err != nil || !slices.Contains(st["ured-vukovar"].Mogucnosti, "probni-korak/1") {
		t.Errorf("spremljeno: %+v %v", st["ured-vukovar"], err)
	}

	// i neočišćeno znanje spremi se uz prijavljenu oznaku
	s.zapamtiZnanje(ctxRub, "ured-vukovar", syncOutcome{program: "x", znanje: razmjena.Znanje{Mogucnosti: mnogo}})
	st, err = s.syncStates(ctxRub)
	if m := st["ured-vukovar"].Mogucnosti; err != nil || len(m) != 32 || !slices.Contains(m, "probni-korak/1") {
		t.Errorf("spremljeno neočišćeno: %v %v", m, err)
	}

	p := &Peer{NodeID: "ured-vukovar"}
	s.zabiljeziIshod(ctxRub, p, syncOutcome{program: "x", znanje: razmjena.Znanje{Mogucnosti: []string{razmjena.OznakaRazmjena, "probni-korak/1", "stranice/2"}}})
	zapis, err := s.Odbijeno(ctxRub, 10)
	if err != nil || len(zapis) != 1 || zapis[0].Razlog != RazlogOznaka+" stranice/2" {
		t.Errorf("nepoznate oznake: %+v %v", zapis, err)
	}
}

// Od primljenoga se odvajaju samo stavke prijavljene vrste; bez prijave
// nijedna, ni stavka bez vrste
func TestPrijaviKorakRazdvajaPoVrsti(t *testing.T) {
	stavke := []stavkaSadrzaja{{Otisak: "a", Vrsta: "probna-stavka"}, {Otisak: "b", Vrsta: VrstaDijelaKarte}, {Otisak: "c", Vrsta: "application/pdf"}, {Otisak: "d"}}
	koraka, ostalo := razdvojiPrimljeno(stavke, "probna-stavka")
	if len(koraka) != 1 || koraka[0].Otisak != "a" || len(ostalo) != 3 {
		t.Errorf("uz probnu vrstu: %v, %v", koraka, ostalo)
	}
	koraka, ostalo = razdvojiPrimljeno(stavke, "")
	if len(koraka) != 0 || len(ostalo) != 4 {
		t.Errorf("bez prijave: %v, %v", koraka, ostalo)
	}
	s := bezPrijave(t, "cop-osijek")
	if err := s.PrijaviKorak(probnaPrijava()); err != nil {
		t.Fatal(err)
	}
	if s.vrstaKoraka() != "probna-stavka" {
		t.Errorf("vrsta: %q", s.vrstaKoraka())
	}
}

// U razgovoru se primljena stavka prijavljene vrste odvoji od sadržaja i
// ne ide u spremište, i kad joj je otisak otisak PDF-a koji čvor traži.
// Za obje uloge. Oznake čvora ostaju one iz ostalih testova tijeka.
func TestPrijaviKorakRazgovorOdvajaPoVrsti(t *testing.T) {
	a, b := cistiPar(t)
	b.korak.Store(nil)
	if err := b.PrijaviKorak(probnaPrijava()); err != nil {
		t.Fatal(err)
	}
	b.mogucnosti = razmjena.NaseMogucnosti
	var sp *sadrzaj.Spremiste
	probna := salje(kindSadrzaj, sadrzajMsg{Stavke: []stavkaSadrzaja{
		{Otisak: otisakTudji, Vrsta: "probna-stavka", Bajtova: len(pdfTudji), Podaci: []byte(pdfTudji)},
	}})
	delta := salje(kindDelta, deltaMsg{Versions: []ledger.Version{dolaznaD1}})
	ishod := append(nakonDelte("nil"), "spremište ima tuđi=false, traži tuđi=true, zatražen=true")
	pripremi := func() { sp = sSadrzajem(t, b) }
	provjeriSlucajeve(t, a, b, true, []slucaj{{ime: "probna stavka", pripremi: pripremi, nakon: stanjeSadrzaja(&sp), ishod: ishod, potezi: []potez{
		cita(granicaV1), salje(kindFrontier, granicaDrugog),
		cita(deltaV1), delta,
		cita(zeljeCvora), salje(kindZelje, zeljeMsg{}),
		cita(sadrzajPrazan), probna,
		cita(krajCvora(1)), salje(kindDone, doneMsg{Applied: 1}),
		cita(krajVeze),
	}}})
	provjeriSlucajeve(t, a, b, false, []slucaj{{ime: "probna stavka", pripremi: pripremi, nakon: stanjeSadrzaja(&sp), ishod: ishod, potezi: []potez{
		salje(kindFrontier, granicaDrugog), cita(granicaV1),
		delta, cita(deltaV1),
		salje(kindZelje, zeljeMsg{}), cita(zeljeCvora),
		probna, cita(sadrzajPrazan),
		salje(kindDone, doneMsg{Applied: 1}), cita(krajCvora(1)),
		cita(krajVeze),
	}}})
}
