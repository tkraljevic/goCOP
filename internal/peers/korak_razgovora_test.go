package peers

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"gocop/internal/ledger"
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

// bezPrijave je čvor kojem je prijava karte iz NewService maknuta
func bezPrijave(t *testing.T, id string) *Service {
	t.Helper()
	s := rubniCvor(t, id)
	s.korak.Store(nil)
	return s
}

// NewService prijavi kartu: oznaka je ona koja je i prije išla u granicu,
// pa se popis oznaka ne mijenja, a druga prijava ne prolazi
func TestPrijaviKorakNewServicePrijaviKartu(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	p := s.korak.Load()
	if p == nil || p.Oznaka != razmjena.OznakaDijeloviKarte || p.Vrsta != VrstaDijelaKarte || p.NajviseDodatnih != najviseRazgovoraZaKartu {
		t.Fatalf("prijava iz NewService: %+v", p)
	}
	if err := s.provjeriPrijavu(*p); err != nil {
		t.Errorf("prijava iz NewService ne prolazi provjeru: %v", err)
	}
	if _, ok := p.Korak.(korakKarte); !ok {
		t.Errorf("korak iz NewService: %T", p.Korak)
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
	// bez prijenosa karte (PostaviKarte) korak karte ne počne, a Odbaci
	// samo zapiše redak u dnevnik
	if tijek := (korakKarte{s}).Zapocni(ctxRub, nil); tijek != nil {
		t.Errorf("korak karte je počeo: %v", tijek)
	}
	korakKarte{s}.Odbaci([]StavkaKoraka{{Otisak: "a"}})
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
