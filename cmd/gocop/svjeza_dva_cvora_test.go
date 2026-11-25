package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/arhiva"
	"gocop/internal/db"
	"gocop/internal/javnivodostaji"
	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/svjeza"
)

// upisiLetvuIOcitanja stavi u registar letvu „proba” i očitanja s javne
// stranice od 20. do 24. 11. svakih šest sati. Brojevi su izmišljeni.
func upisiLetvuIOcitanja(t *testing.T, baza *sql.DB) {
	t.Helper()
	if _, err := baza.Exec(`INSERT INTO stations (id, code, name, created_at, updated_at)
		VALUES ('st-proba', 'proba', 'Letva Proba', datetime('now'), datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	od := time.Date(2026, 11, 20, 0, 0, 0, 0, models.Zagreb)
	for h := 0; h < 5*24; h += 6 {
		kad := od.Add(time.Duration(h) * time.Hour)
		if _, err := baza.Exec(`INSERT INTO readings (id, station_id, measured_at, level_cm, source, origin, created_at, updated_at)
			VALUES (?, 'st-proba', ?, ?, ?, ?, datetime('now'), datetime('now'))`,
			"r-"+kad.Format(time.RFC3339), kad.UTC(), 150+h/6, models.ReadingSourceImport, javnivodostaji.Podrijetlo); err != nil {
			t.Fatal(err)
		}
	}
}

// Svježa arhiva između dva čvora: kućni server preuzima vodostaje, nakon
// ponoći uloži završene dane u svoje stablo, izgradi letvu i izda paket;
// laptop, koji izvore ne preuzima, iste vodostaje dobije paketom arhive i
// vidi ih u spojenom nizu.
func TestSvjezaArhivaDvaCvora(t *testing.T) {
	ctx := context.Background()
	sada := time.Date(2026, 11, 25, 0, 45, 0, 0, models.Zagreb)
	bazaA, recA, _ := cvorZaTest(t, "kucni-server")
	bazaB, recB, _ := cvorZaTest(t, "laptop")
	upisiLetvuIOcitanja(t, bazaA)

	dirA := t.TempDir()
	podaci := filepath.Join(dirA, "stablo")
	if err := os.MkdirAll(filepath.Join(podaci, "dunav", "proba"), 0o755); err != nil {
		t.Fatal(err)
	}
	putA, paketiA := filepath.Join(dirA, "vodostaji.db"), filepath.Join(dirA, "pakete")
	spA := spremisteZaTest(t)
	ra := &razmjenaArhive{baza: bazaA, rec: recA, sp: spA, arhivaPut: putA, paketiDir: paketiA,
		javljeno: map[string]bool{}, potakni: make(chan struct{}, 1)}
	s := novaSvjezaArhiva(bazaA, nil, podaci, nil, nil, ra)
	s.uloge = func() peers.Uloge { return peers.Uloge{Preuzima: true} }
	s.arh = arhivaIzStabla{podaci: podaci, put: putA, paketi: paketiA, cvor: recA.Cvor()}
	s.jednomDnevno(sada)
	s.jednomDnevno(sada.Add(time.Hour)) // drugi krug istog dana ne radi ništa
	if len(ra.potakni) != 1 {
		t.Error("izdani paket nije potaknuo krug razmjene arhive")
	}
	if n, err := ra.objavi(ctx); err != nil || n != 1 {
		t.Fatalf("objava paketa letve: %d %v", n, err)
	}
	stA := stanjeSvjezeArhive(bazaA, putA, podaci, s.uloge)
	if !stA.Ulaze || stA.Letvi != 1 || stA.Paketa != 1 || !strings.Contains(stA.PaketiOstalima(), "(1 paket)") {
		t.Errorf("stanje kućnog servera: %+v", stA)
	}

	// razmjena: kazalo stiže laptopu, a on traži i ugradi paket
	verzije, err := recA.Since(ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recB.Apply(ctx, verzije); err != nil {
		t.Fatal(err)
	}
	putB := filepath.Join(t.TempDir(), "vodostaji.db")
	spB := spremisteZaTest(t)
	rb := &razmjenaArhive{baza: bazaB, rec: recB, sp: spB, arhivaPut: putB, javljeno: map[string]bool{},
		preuzima: func() bool { return false },
		zeli:     func(context.Context, string, string) bool { return true },
		ugradi: func(s *arhiva.Sadrzaj) error {
			d, err := sql.Open("sqlite", putB)
			if err != nil {
				return err
			}
			defer d.Close()
			if err := arhiva.PripremiPraznu(d); err != nil {
				return err
			}
			return arhiva.Ugradi(d, putB, s)
		}}
	rb.primi(ctx)
	z, err := spB.Zeljeni(ctx, 10)
	if err != nil || len(z) != 1 {
		t.Fatalf("laptop traži %d paketa (%v), želim 1", len(z), err)
	}
	bajtovi, vrsta, err := spA.Citaj(ctx, z[0].Otisak)
	if err != nil {
		t.Fatal(err)
	}
	if err := spB.UpisiProvjereno(ctx, z[0].Otisak, vrsta, bajtovi, "kucni-server"); err != nil {
		t.Fatal(err)
	}
	rb.primi(ctx)

	arhB := bazaZaTest(t, db.SamoZaCitanje(putB))
	var n int
	var izvor string
	if err := arhB.QueryRow(`SELECT count(*), max(izvor) FROM spoj WHERE letva = 'proba' AND velicina = 'vodostaj' AND korak = 'satni'`).
		Scan(&n, &izvor); err != nil {
		t.Fatal(err)
	}
	if n != 5*4 || izvor != "hv-uzivo" {
		t.Errorf("laptop ima %d satnih vodostaja letve iz %q, želim 20 iz hv-uzivo", n, izvor)
	}
	stB := stanjeSvjezeArhive(bazaB, putB, "", func() peers.Uloge { return peers.Uloge{} })
	if stB.Ulaze || stB.PrimljenoLetvi != 1 || stB.Primljeno.IsZero() {
		t.Errorf("stanje laptopa: %+v", stB)
	}
}

// Čvor bez stabla ne ulaže i to javi jednom; čvor bez uloge ne radi ništa.
func TestSvjezaArhivaBezStablaIUloge(t *testing.T) {
	baza, _, _ := cvorZaTest(t, "laptop")
	dnevnik := zapisDnevnika(t)
	s := novaSvjezaArhiva(baza, nil, t.TempDir(), nil, nil, nil)
	s.uloge = func() peers.Uloge { return peers.Uloge{Preuzima: true} }
	s.arh = &laznaArhiva{}
	s.jednomDnevno(time.Date(2026, 11, 25, 1, 0, 0, 0, models.Zagreb))
	s.jednomDnevno(time.Date(2026, 11, 26, 1, 0, 0, 0, models.Zagreb))
	if n := strings.Count(dnevnik.String(), "nema stabla"); n != 1 {
		t.Errorf("„nema stabla” javljeno %d puta: %q", n, dnevnik.String())
	}
	s.uloge = func() peers.Uloge { return peers.Uloge{} }
	s.jednomDnevno(time.Date(2026, 11, 27, 1, 0, 0, 0, models.Zagreb))
	if strings.Contains(dnevnik.String(), "uloženo") {
		t.Errorf("čvor bez uloge je ulagao: %q", dnevnik.String())
	}
}

// Letva kojoj gradnja ili izdavanje ne uspije ostaje na redu i pokuša se
// idući krug istog dana; kad sve prođe, idući krug ne radi ništa.
func TestSvjezaArhivaPonavljaNeuspjelo(t *testing.T) {
	baza, _, _ := cvorZaTest(t, "kucni-server")
	upisiLetvuIOcitanja(t, baza)
	if _, err := baza.Exec(`INSERT INTO stations (id, code, name, created_at, updated_at)
		VALUES ('st-druga', 'druga', 'Letva Druga', datetime('now'), datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO readings (id, station_id, measured_at, level_cm, source, origin, created_at, updated_at)
		VALUES ('r-druga', 'st-druga', ?, 90, ?, ?, datetime('now'), datetime('now'))`,
		time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC), models.ReadingSourceImport, javnivodostaji.Podrijetlo); err != nil {
		t.Fatal(err)
	}
	podaci := t.TempDir()
	for _, l := range []string{"proba", "druga"} {
		if err := os.MkdirAll(filepath.Join(podaci, "dunav", l), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	la := &laznaArhiva{neGradi: "proba", neIzdaje: "druga"}
	s := novaSvjezaArhiva(baza, nil, podaci, nil, nil, nil)
	s.uloge = func() peers.Uloge { return peers.Uloge{Preuzima: true} }
	s.arh = la
	sada := time.Date(2026, 11, 25, 0, 45, 0, 0, models.Zagreb)
	s.jednomDnevno(sada)
	if !s.ostalo {
		t.Fatal("neuspjelo nije ostalo za idući krug")
	}
	la.neGradi, la.neIzdaje = "", ""
	s.jednomDnevno(sada.Add(time.Hour))
	if s.ostalo || len(la.izdane) != 2 {
		t.Fatalf("drugi krug: ostalo %v, izdane %v", s.ostalo, la.izdane)
	}
	s.jednomDnevno(sada.Add(2 * time.Hour))
	if len(la.izdane) != 2 {
		t.Errorf("treći krug je opet izdavao: %v", la.izdane)
	}
	// kvar evidencije: popis za paket se ne da pročitati
	pokvarena := bazaZaTest(t, filepath.Join(t.TempDir(), "p.db"))
	if _, err := pokvarena.Exec(`CREATE TABLE svjeza_arhiva (kljuc TEXT)`); err != nil {
		t.Fatal(err)
	}
	if (&svjezaArhiva{ulagac: &svjeza.Ulagac{Baza: pokvarena}}).izdajNaRedu(sada) {
		t.Error("kvar evidencije javljen kao uspjeh")
	}
}

// Stanje za stranicu: bez arhive i s arhivom koje nema nema primljenog paketa.
func TestStanjeSvjezeBezArhive(t *testing.T) {
	baza, _, _ := cvorZaTest(t, "laptop")
	bez := func() peers.Uloge { return peers.Uloge{} }
	for _, put := range []string{"", filepath.Join(t.TempDir(), "nema.db")} {
		if s := stanjeSvjezeZa(baza, put, "", bez)(); s.Ulaze || !s.Primljeno.IsZero() {
			t.Errorf("%q: %+v", put, s)
		}
	}
}

// Greška ulaganja ide u dnevnik programa, a bez greške ništa.
func TestJaviGresku(t *testing.T) {
	dnevnik := zapisDnevnika(t)
	javiGresku("svježa arhiva, ulaganje", nil)
	javiGresku("svježa arhiva, ulaganje", errors.New("stablo nije dostupno"))
	if dnevnik.String() == "" || strings.Count(dnevnik.String(), "svježa arhiva") != 1 {
		t.Errorf("dnevnik %q", dnevnik.String())
	}
}

// brojacArhive broji gradnje i izdavanja po letvi.
type brojacArhive struct{ gradnje, izdavanja map[string]int }

func (b *brojacArhive) IzgradiLetvu(letva string, _ io.Writer) (arhiva.Izvjestaj, error) {
	b.gradnje[letva]++
	return arhiva.Izvjestaj{}, nil
}

func (b *brojacArhive) IzdajArhivu(letva string, _ bool, _ io.Writer) (arhiva.IzvjestajIzdanja, error) {
	b.izdavanja[letva]++
	return arhiva.IzvjestajIzdanja{Promijenjenih: 1}, nil
}

// Gradi se svaki dan nakon ulaganja, a paket ide prvi put pa tek za tjedan:
// arhiva ovog čvora ima nove dane sutradan, a ostali ih dobiju tjedno.
// Brojevi su izmišljeni.
func TestSvjezaArhivaGradiDnevnoIzdajeTjedno(t *testing.T) {
	baza, _, _ := cvorZaTest(t, "kucni-server")
	upisiLetvuIOcitanja(t, baza)
	podaci := t.TempDir()
	if err := os.MkdirAll(filepath.Join(podaci, "dunav", "proba"), 0o755); err != nil {
		t.Fatal(err)
	}
	b := &brojacArhive{gradnje: map[string]int{}, izdavanja: map[string]int{}}
	s := novaSvjezaArhiva(baza, nil, podaci, func() peers.Uloge { return peers.Uloge{Preuzima: true} }, b, nil)
	for d := 25; d <= 33; d++ {
		dan := time.Date(2026, 11, d, 0, 45, 0, 0, models.Zagreb)
		if _, err := baza.Exec(`INSERT INTO readings (id, station_id, measured_at, level_cm, source, origin, created_at, updated_at)
			VALUES (?, 'st-proba', ?, 170, ?, ?, datetime('now'), datetime('now'))`,
			fmt.Sprintf("r-dan-%d", d), dan.Add(-12*time.Hour).UTC(), models.ReadingSourceImport, javnivodostaji.Podrijetlo); err != nil {
			t.Fatal(err)
		}
		s.jednomDnevno(dan)
		s.jednomDnevno(dan.Add(time.Hour))
	}
	if b.gradnje["proba"] != 9 || b.izdavanja["proba"] != 2 {
		t.Errorf("gradnji %d (želim 9, svaki dan), paketa %d (želim 2: prvi i tjedan poslije)", b.gradnje["proba"], b.izdavanja["proba"])
	}
}
