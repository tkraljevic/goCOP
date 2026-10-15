package prognoza

import (
	"bytes"
	"database/sql"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// satnaArhivaRacuna je arhiva sa satnim nizovima letvi u zadanoj veličini,
// od sata od do sata do (sati od epohe), uključivo.
func satnaArhivaRacuna(t *testing.T, velicina string, od, do int64, nizovi map[string]func(sat int64) float64) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "satna.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE spoj (letva TEXT, velicina TEXT, korak TEXT, vrijeme INTEGER, vrijednost REAL)`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for letva, f := range nizovi {
		for sat := od; sat <= do; sat++ {
			if _, err := tx.Exec(`INSERT INTO spoj VALUES (?, ?, 'satni', ?, ?)`, letva, velicina, sat*3600, f(sat)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return db
}

// Zadane postavke provjere su one s kojima se zapisuju promašaji za živu
// prognozu: 2023.–2025., izdanje svakih 12 sati do 96 sati unaprijed,
// glačanje šest sati na svaku stranu, bez zapisa i bez ispravka.
func TestZadaneOpcijeProvjere(t *testing.T) {
	o := ZadaneOpcijeProvjere()
	if !o.Od.Equal(time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)) || !o.Do.Equal(time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)) ||
		o.Korak != 12 || o.Najdalje != 96 || o.Glacenje != 6 || o.Zapisi || o.Ispravi || o.CSV != nil || o.Udjeli != nil {
		t.Errorf("zadane opcije %+v", o)
	}
}

// Na valovitoj vodi lanac pobjeđuje postojanost, pa tablica to kaže; uz
// udjele ispisuje se polovina raspona po udjelu, a udio od UdioURasponu je
// sam sebi mjera (×1,00).
func TestProvjeraUnatragBoljaOdPostojanostiIUdjeli(t *testing.T) {
	bezIspravka(t)
	var dnevnik bytes.Buffer
	o := opcijeStalne(&dnevnik)
	o.Zapisi, o.Udjeli = false, []float64{0.5, UdioURasponu, 1}
	gornja := func(sat int64) float64 { return 100 + 20*math.Sin(float64(sat)/10) }
	od, do := o.Od.Unix()/3600-12, o.Do.Unix()/3600+13
	arhiva := satnaArhivaRacuna(t, "vodostaj", od, do, map[string]func(int64) float64{
		"gornja": gornja,
		"donja":  func(sat int64) float64 { return gornja(sat - 3) },
	})
	n, err := ProvjeriUnatrag(arhiva, probnaBazaPrognoza(t), o)
	if err != nil || n != 0 {
		t.Fatalf("%d, %v", n, err)
	}
	ispis := dnevnik.String()
	bolje := map[string]string{}
	for _, l := range strings.Split(ispis, "\n") {
		if p := strings.Fields(l); len(p) > 3 && p[0] == "donja" && p[2] == "h" && p[3] == "121" {
			bolje[p[1]] = l
		}
	}
	for _, d := range []string{"6", "12"} {
		if !strings.HasSuffix(bolje[d], "% bolje") {
			t.Errorf("doseg %s h: %q, želim „bolje” na 121 slučaju", d, bolje[d])
		}
	}
	if !strings.Contains(ispis, "polovina raspona po udjelu (brojanjem), i koliko je šira od one za 70 %") ||
		!strings.Contains(ispis, "letva            doseg          50 %          70 %         100 %") {
		t.Errorf("zaglavlje udjela:\n%s", ispis)
	}
	redova := 0
	for _, l := range strings.Split(ispis, "\n") {
		if !strings.HasPrefix(l, "donja") || !strings.Contains(l, "×") {
			continue
		}
		redova++
		polja := strings.Fields(l)
		// donja, doseg, h, pa po dva polja za svaki udio
		if len(polja) != 9 || polja[6] != "×1.00" {
			t.Errorf("redak udjela %q", l)
			continue
		}
		var pola, cijeli float64
		if _, err := fmt.Sscan(polja[3], &pola); err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Sscan(polja[7], &cijeli); err != nil {
			t.Fatal(err)
		}
		if pola > cijeli || cijeli <= 0 {
			t.Errorf("polovina raspona za 50 %% (%v) mora biti manja od one za sve (%v): %q", pola, cijeli, l)
		}
	}
	if redova != 2 {
		t.Errorf("%d redaka udjela, želim 2 (6 i 12 h):\n%s", redova, ispis)
	}
	if !strings.Contains(ispis, "ništa nije zapisano") {
		t.Errorf("bez zapisa dnevnik mora to reći:\n%s", ispis)
	}
}

// Elektrana na vrhu lanca u provjeri dobiva budućnost iz modela ispuštanja,
// kao uživo; sati za koje arhiva nema mjerenja cilja ne ulaze u promašaje.
func TestProvjeraUnatragSModelomIspustanja(t *testing.T) {
	bezIspravka(t)
	baza := praznaBazaPrognoza(t)
	if err := Spremi(baza, []Pojas{{Letva: "donja", Velicina: "protok", Od: 0, Do: 10000, Rasap: 20,
		Ulazi: []Ulaz{{Letva: "gornja", Velicina: "protok", PomakH: 3, Sirina: 1, Nagib: 1}}}}, "proba"); err != nil {
		t.Fatal(err)
	}
	m := &OperaterModel{Letva: "gornja"}
	for k := 1; k <= OperaterDosezi; k++ {
		m.Koef[0][k] = make([]float64, 10) // jedinica, tri istjecanja, sat sada i ciljni sat
		m.Koef[0][k][0] = 500
		m.Koristi[0][k] = true
	}
	if err := SpremiOperatera(baza, m); err != nil {
		t.Fatal(err)
	}
	var dnevnik, tablica bytes.Buffer
	o := opcijeStalne(&dnevnik)
	o.Do, o.Zapisi, o.CSV = o.Od.Add(24*time.Hour), false, &tablica
	od := o.Od.Unix()/3600 - 48
	stalno := func(int64) float64 { return 400 }
	// Gornja do kraja, donja samo do 6 sati poslije zadnjeg izdanja.
	arhiva := satnaArhivaRacuna(t, "protok", od, o.Do.Unix()/3600+13, map[string]func(int64) float64{"gornja": stalno})
	tx, err := arhiva.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for sat := od; sat <= o.Do.Unix()/3600+6; sat++ {
		if _, err := tx.Exec(`INSERT INTO spoj VALUES ('donja', 'protok', 'satni', ?, 400)`, sat*3600); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := ProvjeriUnatrag(arhiva, baza, o); err != nil {
		t.Fatal(err)
	}
	redovi := redoviCSV(t, &tablica)
	po := map[string]int{}
	for _, red := range redovi {
		po[red[4]]++
		// gornja tri sata prije cilja već je iz modela: 500, a ne zadnjih 400
		if red[2] != "protok" || red[5] != "500" || red[8] != "400" {
			t.Errorf("redak %v, želim prognozu 500 prema izmjerenih 400", red)
		}
	}
	// 25 izdanja; na 12 h zadnjih šest nema mjerenja donje
	if po["6"] != 25 || po["12"] != 19 {
		t.Errorf("redaka po dosegu %v, želim 25 na 6 h i 19 na 12 h", po)
	}
	if !strings.Contains(dnevnik.String(), "m³/s") {
		t.Errorf("tablica protoka bez jedinice:\n%s", dnevnik.String())
	}
}

// Provjera javlja grešku svake baze koja se ne da pročitati: bez tablica
// pojasa, promašaja (kad se ispravlja), modela ispuštanja ili arhive, i kad
// se promašaji ne daju zapisati.
func TestProvjeraUnatragGreskeBaza(t *testing.T) {
	bezIspravka(t)
	var dnevnik bytes.Buffer
	o := opcijeStalne(&dnevnik)
	o.Do = o.Od
	arhiva := stalnaArhiva(t, o.Od.Add(-12*time.Hour), o.Do.Add(13*time.Hour))
	prazna, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "prazna.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { prazna.Close() })
	if _, err := ProvjeriUnatrag(arhiva, prazna, o); err == nil || !strings.Contains(err.Error(), "pojasi") {
		t.Errorf("baza prognoza bez tablica: %v", err)
	}
	bezTablice := func(tablica string) *sql.DB {
		baza := probnaBazaPrognoza(t)
		if _, err := baza.Exec(`DROP TABLE ` + tablica); err != nil {
			t.Fatal(err)
		}
		return baza
	}
	s := o
	s.Ispravi = true
	if _, err := ProvjeriUnatrag(arhiva, bezTablice("promasaji"), s); err == nil || !strings.Contains(err.Error(), "promasaji") {
		t.Errorf("ispravak bez tablice promašaja: %v", err)
	}
	if _, err := ProvjeriUnatrag(arhiva, bezTablice("operateri"), o); err == nil || !strings.Contains(err.Error(), "operateri") {
		t.Errorf("bez tablice modela ispuštanja: %v", err)
	}
	if _, err := ProvjeriUnatrag(prazna, probnaBazaPrognoza(t), o); err == nil || !strings.Contains(err.Error(), "spoj") {
		t.Errorf("arhiva bez spoja: %v", err)
	}
	s = o
	s.Zapisi = true
	if n, err := ProvjeriUnatrag(arhiva, bezTablice("promasaji"), s); err == nil || n != 0 || !strings.Contains(err.Error(), "brisanje zatečenih promašaja") {
		t.Errorf("zapis bez tablice promašaja: %d, %v", n, err)
	}
}

// Prazan zbroj nema raspona, postojanost bez slučaja nema promašaja, a
// udio 1 je najveći odmak.
func TestZbrojProvjereRubovi(t *testing.T) {
	var z zbrojProvjere
	if z.odstupanjeUdio(0.7) != 0 || z.rmsP() != 0 {
		t.Errorf("prazan zbroj: %v, %v", z.odstupanjeUdio(0.7), z.rmsP())
	}
	for _, p := range []float64{-2, 0, 2, 10} {
		z.dodaj(p, 3)
	}
	// pomak 2,5; odmaci 0,5, 2,5, 4,5 i 7,5
	if z.pomak() != 2.5 || z.odstupanjeUdio(1) != 7.5 || z.odstupanjeUdio(0.5) != 4.5 || z.rmsP() != 0 || z.uRasponu() != 0.75 {
		t.Errorf("zbroj %+v: pomak %v, udio 1 %v, udio 0,5 %v, postojanost %v, u rasponu %v",
			z, z.pomak(), z.odstupanjeUdio(1), z.odstupanjeUdio(0.5), z.rmsP(), z.uRasponu())
	}
}
