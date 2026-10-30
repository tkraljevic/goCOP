package web

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/arhiva"
)

// Snimka korita kojoj je nestala datoteka stoji na istom popisu kao niz bez
// datoteke, s danom snimke, brojem točaka i gumbom Makni (Odluke
// (13. 10. 2026.)).
func TestSnimkaBezDatotekeSeVidiNaPopisu(t *testing.T) {
	d := vrataZaTest()
	d.Sirotani = []arhiva.Sirotan{
		{Letva: "batina", Izvor: "cop-rucno", Velicina: "vodostaj", Vrsta: "jutarnji", Zapisa: 1},
		{Letva: "vodograd", Snimka: true, Datum: "2010-03-22", Zapisa: 2},
	}
	html := iscrtaj(t, "uvoz_niza.html", d)
	for _, want := range []string{"Nizovi bez datoteke u stablu", "snimke korita i krivulje protoka kojima je",
		"snimka korita 2010-03-22", "2 točke", "/administracija/uvoz-niza/makni-snimku",
		`name="datum" value="2010-03-22"`, "Maknuti snimku korita 2010-03-22 za vodograd iz arhive?",
		"/administracija/uvoz-niza/makni-niz", "cop-rucno"} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica nema %q", want)
		}
	}
	if strings.Contains(html, `name="izvor" value=""`) {
		t.Error("snimka je iscrtana kao niz bez izvora")
	}
}

// Gumb Makni uz snimku: smije samo onaj tko uređuje arhivu, čvor koji ne
// uređuje arhivu to kaže, a uspjeh i greška pišu se cijelom rečenicom.
func TestMicanjeSnimkeBezDatoteke(t *testing.T) {
	o := novaOkolinaVrata(t)
	const putanja = "/administracija/uvoz-niza/makni-snimku"
	forma := url.Values{"letva": {"primjerovo"}, "datum": {"2010-03-22"}}

	if w := o.zovi(o.h.MakniSnimkuBezDatoteke, o.marko, o.markoOvl, putanja, forma); w.Code != http.StatusForbidden {
		t.Errorf("bez prava: %d", w.Code)
	}
	if w := o.zovi(o.h.MakniSnimkuBezDatoteke, o.pero, o.peroOvl, putanja, forma); !strings.Contains(w.Body.String(), "Ovaj čvor ne može uređivati arhivu.") {
		t.Errorf("čvor bez micanja: %d", w.Code)
	}

	var pozvano []string
	greska := error(nil)
	o.h.SetMakniSnimku(func(letva, datum string) (int, error) {
		pozvano = append(pozvano, letva+"|"+datum)
		return 2, greska
	})
	if w := o.zovi(o.h.MakniSnimkuBezDatoteke, o.pero, o.peroOvl, putanja, url.Values{"datum": {"2010-03-22"}}); !strings.Contains(w.Body.String(), "Nije zadano koju snimku korita maknuti.") {
		t.Errorf("bez letve: %d", w.Code)
	}
	w := o.zovi(o.h.MakniSnimkuBezDatoteke, o.pero, o.peroOvl, putanja, forma)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("micanje: %d %s", w.Code, w.Body.String())
	}
	poruka, _ := url.QueryUnescape(w.Header().Get("Location"))
	if !strings.Contains(poruka, "Snimka korita primjerovo od 2010-03-22 maknuta je iz arhive (2 točke).") {
		t.Errorf("poruka o uspjehu: %s", poruka)
	}
	greska = errors.New("snimka korita primjerovo od 2010-03-22 nije u arhivi")
	if w := o.zovi(o.h.MakniSnimkuBezDatoteke, o.pero, o.peroOvl, putanja, forma); !strings.Contains(w.Body.String(),
		"Snimka korita nije maknuta: snimka korita primjerovo od 2010-03-22 nije u arhivi") {
		t.Errorf("greška pri micanju: %d", w.Code)
	}
	if len(pozvano) != 2 || pozvano[0] != "primjerovo|2010-03-22" {
		t.Errorf("micanje je zvano s %v", pozvano)
	}
}

// Poslužitelj snimku miče iz prave arhive i odmah je otvara iznova; bez
// arhive ili kad snimke ili niza nema, javlja grešku.
func TestPosluziteljMiceSnimku(t *testing.T) {
	put := filepath.Join(t.TempDir(), "arhiva.db")
	db, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := arhiva.PripremiPraznu(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO profili (id, letva, datum) VALUES (1, 'vodograd', '2010-03-22');
		INSERT INTO profil_tocke (profil, stacionaza, visina) VALUES (1, 0, 90), (1, 6, 87)`); err != nil {
		t.Fatal(err)
	}

	if _, err := (&Server{}).MakniSnimku("vodograd", "2010-03-22"); err == nil {
		t.Error("micanje bez arhive prošlo je")
	}
	// Bez stabla se ne zna je li snimka bez datoteke, pa se ne miče.
	if _, err := (&Server{arhivaPut: put}).MakniSnimku("vodograd", "2010-03-22"); err == nil ||
		!strings.Contains(err.Error(), "nije na popisu snimki bez datoteke") {
		t.Errorf("micanje bez stabla: %v", err)
	}
	koren := t.TempDir()
	if err := os.MkdirAll(filepath.Join(koren, "dunav", "vodograd"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := &Server{arhivaPut: put, podaciDir: koren}
	n, err := s.MakniSnimku("vodograd", "2010-03-22")
	if err != nil || n != 2 {
		t.Fatalf("micanje: %d, %v", n, err)
	}
	if s.arhiva == nil {
		t.Error("arhiva nije otvorena iznova")
	}
	defer s.arhiva.Close()
	var ostalo int
	if err := db.QueryRow(`SELECT count(*) FROM profili`).Scan(&ostalo); err != nil || ostalo != 0 {
		t.Errorf("u arhivi je ostalo %d snimki (%v)", ostalo, err)
	}
	if _, err := s.MakniSnimku("vodograd", "2010-03-22"); err == nil || !strings.Contains(err.Error(), "u arhivi više nema") {
		t.Errorf("micanje snimke koje nema: %v", err)
	}
	// Niz ide istim putem kroz arhivu.
	if _, err := s.MakniNiz("vodograd", "his2000", "vodostaj", "satni"); err == nil || !strings.Contains(err.Error(), "u arhivi više nema") {
		t.Errorf("micanje niza kojeg nema: %v", err)
	}
}

// Datoteka snimke korita koja se ne da pročitati pokaže se kao zaseban redak,
// s imenom datoteke i razlogom, bez gumba Makni, a ostatak popisa ostaje
// (Odluke (13. 10. 2026.)). Prije je stranica umjesto popisa pisala samo
// grešku.
func TestNecitljivaSnimkaNaPopisuBezGumba(t *testing.T) {
	o := novaOkolinaVrata(t)
	put := filepath.Join(o.koren, "primjerica", "primjerovo", "profil", "primjerovo_profil_2010-03-22.csv")
	if err := os.MkdirAll(filepath.Dir(put), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(put, []byte("stacionaza_m;visina_m\n0;9\"0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", o.arhivaPut)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO profili (letva, datum) VALUES ('primjerovo', '2010-03-22'), ('drugovo', '2015-05-05')`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	html := odgovor(o.h.ShowUvoz, httptestGet("/administracija/uvoz-niza"), o.pero, o.peroOvl).Body.String()
	stranica := strings.Join(strings.Fields(htmlUTekst(html)), " ")
	if g := porukaGreske(stranica); g != "" {
		t.Errorf("stranica javlja grešku umjesto popisa: %q", g)
	}
	for _, treba := range []string{"Nizovi bez datoteke u stablu", "snimka korita 2015-05-05",
		`Datoteka snimke korita <span class="mono">primjerovo_profil_2010-03-22.csv</span> ne da se pročitati: parse error`,
		"Dok se ne popravi, ovdje se ne vidi koja snimka te letve nema datoteku."} {
		if !strings.Contains(stranica, treba) {
			t.Errorf("stranica nema %q: %s", treba, stranica)
		}
	}
	if !strings.Contains(html, `name="letva" value="drugovo"`) || strings.Contains(html, `name="letva" value="primjerovo"`) {
		t.Error("gumb Makni stoji uz pogrešnu letvu")
	}
}
