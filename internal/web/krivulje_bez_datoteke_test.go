package web

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/arhiva"
)

// Krivulje protoka letve kojoj je nestala datoteka krivulja stoje na istom
// popisu kao niz i snimka bez datoteke, s početkom najstarije krivulje, brojem
// krivulja i gumbom Makni (Odluke (13. 10. 2026.)).
func TestKrivuljeBezDatotekeSeVideNaPopisu(t *testing.T) {
	d := vrataZaTest()
	d.Sirotani = []arhiva.Sirotan{
		{Letva: "vodograd", Krivulje: true, Datum: "2001-01-01", Zapisa: 2},
		{Letva: "rijecica", Krivulje: true, Datum: "1926-01-01", Zapisa: 5},
	}
	html := iscrtaj(t, "uvoz_niza.html", d)
	for _, want := range []string{"Nizovi bez datoteke u stablu", "snimke korita i krivulje protoka kojima je",
		"krivulje protoka od 1.1.2001.", "2 krivulje", "5 krivulja", "/administracija/uvoz-niza/makni-krivulje",
		`name="letva" value="vodograd"`, "Maknuti krivulje protoka za vodograd iz arhive?"} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica nema %q", want)
		}
	}
	for _, ne := range []string{`name="izvor" value=""`, "snimka korita 2001-01-01"} {
		if strings.Contains(html, ne) {
			t.Errorf("krivulje su iscrtane kao niz ili snimka: %q", ne)
		}
	}
}

// Gumb Makni uz krivulje: smije samo onaj tko uređuje arhivu, čvor koji ne
// uređuje arhivu to kaže, a uspjeh i greška pišu se cijelom rečenicom.
func TestMicanjeKrivuljaBezDatoteke(t *testing.T) {
	o := novaOkolinaVrata(t)
	const putanja = "/administracija/uvoz-niza/makni-krivulje"
	forma := url.Values{"letva": {"primjerovo"}}

	if w := o.zovi(o.h.MakniKrivuljeBezDatoteke, o.marko, o.markoOvl, putanja, forma); w.Code != http.StatusForbidden {
		t.Errorf("bez prava: %d", w.Code)
	}
	if w := o.zovi(o.h.MakniKrivuljeBezDatoteke, o.pero, o.peroOvl, putanja, forma); !strings.Contains(w.Body.String(), "Ovaj čvor ne može uređivati arhivu.") {
		t.Errorf("čvor bez micanja: %d", w.Code)
	}

	var pozvano []string
	greska := error(nil)
	o.h.SetMakniKrivulje(func(letva string) (int, error) {
		pozvano = append(pozvano, letva)
		return 2, greska
	})
	if w := o.zovi(o.h.MakniKrivuljeBezDatoteke, o.pero, o.peroOvl, putanja, url.Values{}); !strings.Contains(w.Body.String(), "Nije zadano kojoj letvi maknuti krivulje protoka.") {
		t.Errorf("bez letve: %d", w.Code)
	}
	w := o.zovi(o.h.MakniKrivuljeBezDatoteke, o.pero, o.peroOvl, putanja, forma)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("micanje: %d %s", w.Code, w.Body.String())
	}
	poruka, _ := url.QueryUnescape(w.Header().Get("Location"))
	if !strings.Contains(poruka, "Iz arhive su maknute 2 krivulje protoka letve primjerovo.") {
		t.Errorf("poruka o uspjehu: %s", poruka)
	}
	greska = errors.New("letva primjerovo nema krivulja protoka u arhivi")
	if w := o.zovi(o.h.MakniKrivuljeBezDatoteke, o.pero, o.peroOvl, putanja, forma); !strings.Contains(w.Body.String(),
		"Krivulje protoka nisu maknute: letva primjerovo nema krivulja protoka u arhivi") {
		t.Errorf("greška pri micanju: %d", w.Code)
	}
	if len(pozvano) != 2 || pozvano[0] != "primjerovo" {
		t.Errorf("micanje je zvano s %v", pozvano)
	}
}

// Poruka o uspjehu slaže glagol i riječ s brojem maknutih krivulja.
func TestPorukaMaknutihKrivulja(t *testing.T) {
	for n, zeli := range map[int]string{
		1: "Iz arhive je maknuta 1 krivulja protoka letve vodograd.",
		2: "Iz arhive su maknute 2 krivulje protoka letve vodograd.",
		5: "Iz arhive je maknuto 5 krivulja protoka letve vodograd.",
	} {
		if p := porukaMaknutihKrivulja("vodograd", n); !strings.HasPrefix(p, zeli+" Datoteke krivulja u stablu ionako nema") {
			t.Errorf("%d: %q", n, p)
		}
	}
}

// Poslužitelj krivulje miče iz prave arhive i odmah je otvara iznova; bez
// arhive ili kad krivulja nema, javlja grešku.
func TestPosluziteljMiceKrivulje(t *testing.T) {
	put := filepath.Join(t.TempDir(), "arhiva.db")
	db, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := arhiva.PripremiPraznu(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO hq_krivulje (id, letva, vrijedi_od) VALUES (1, 'vodograd', '2001-01-01'), (2, 'vodograd', '2011-01-01');
		INSERT INTO hq_odsjecci (krivulja, od_cm, do_cm, p1, p2, p3) VALUES (1, 0, 100, 1, 1, 1), (2, 0, 100, 1, 1, 1)`); err != nil {
		t.Fatal(err)
	}

	if _, err := (&Server{}).MakniKrivulje("vodograd"); err == nil {
		t.Error("micanje bez arhive prošlo je")
	}
	s := &Server{arhivaPut: put}
	n, err := s.MakniKrivulje("vodograd")
	if err != nil || n != 2 {
		t.Fatalf("micanje: %d, %v", n, err)
	}
	if s.arhiva == nil {
		t.Error("arhiva nije otvorena iznova")
	}
	defer s.arhiva.Close()
	var ostalo int
	if err := db.QueryRow(`SELECT count(*) FROM hq_krivulje`).Scan(&ostalo); err != nil || ostalo != 0 {
		t.Errorf("u arhivi je ostalo %d krivulja (%v)", ostalo, err)
	}
	if _, err := s.MakniKrivulje("vodograd"); err == nil || !strings.Contains(err.Error(), "nema krivulja protoka u arhivi") {
		t.Errorf("micanje krivulja kojih nema: %v", err)
	}
}
