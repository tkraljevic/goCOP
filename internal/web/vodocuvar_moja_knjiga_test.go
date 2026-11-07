package web

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Moja knjiga po crtežu: osoba koja ima samo svoju knjigu vidi jedan glavni
// gumb i poveznice, bez opisa stranice, a list je kartica s kratkim datumom i
// satima. Rukovoditeljica i dalje dobiva Knjige vodočuvara kakve jesu. Osobe su
// izmišljene: vodočuvar Pero Perić, rukovoditeljica Ana Anić.
func TestMojaKnjigaPoCrtezu(t *testing.T) {
	o := novaOkolinaDnevnika(t)
	dan := func(mjesec time.Month, d int) time.Time { return time.Date(2026, mjesec, d, 0, 0, 0, 0, models.Zagreb) }
	kad := dan(time.October, 26).Add(15 * time.Hour)
	spremi := func(datum time.Time, izmijeni func(l *models.VodocuvarskiList)) string {
		t.Helper()
		l := &models.VodocuvarskiList{ID: uuid.Must(uuid.NewV7()).String(), UserID: o.vodocuvar.ID.String(), Ime: "Pero Perić", Sektor: "P", AreaID: 1,
			Datum: datum, Od: "07:00", Do: "15:00", Opis: "obilazak nasipa"}
		izmijeni(l)
		if err := o.repo.Save(context.Background(), l); err != nil {
			t.Fatal(err)
		}
		return l.ID
	}
	spremi(dan(time.October, 26), func(l *models.VodocuvarskiList) { l.Broj, l.PredanoAt = 41, &kad })
	spremi(dan(time.October, 21), func(l *models.VodocuvarskiList) { l.Broj, l.PredanoAt, l.Do = 38, &kad, "14:30" })
	spremi(dan(time.October, 21), func(l *models.VodocuvarskiList) { l.Broj, l.PredanoAt = 37, &kad })
	spremi(dan(time.October, 19), func(l *models.VodocuvarskiList) {
		l.Broj, l.PredanoAt, l.PotvrdenoAt, l.Potvrdio = 35, &kad, &kad, "Ana Anić"
		l.Parafe = []models.Parafa{{Ime: "Iva Ivić"}}
	})
	spremi(dan(time.August, 14), func(l *models.VodocuvarskiList) { l.Rekonstrukcija = true; l.Opis = "ustava pregledana" })

	s := o.zovi(t, o.vodocuvar, http.MethodGet, "/vodocuvar?godina=2026", nil).Body.String()
	for _, x := range []string{
		`data-naziv-stranice="Vodočuvarski dnevnik"`,
		`href="/prijave"`, "Moje prijave",
		`href="/vodocuvar/kalendar"`, "Kalendar zadataka",
		`href="/vodocuvar/knjiga.pdf?godina=2026"`, "Cijela knjiga 2026.",
		"List 41 · ", "26. 10. · 07:00–15:00 · 8 h",
		"21. 10. · 07:00–14:30 · 7,5 h",
		`<span class="moj-list-sitno">14. 8.</span>`, "Preneseno · ",
		"iz ranije evidencije, bez potpisa vodočuvara",
		"ovjerio Ana Anić · parafa: Iva Ivić",
		"dvostruki list: vidi i list br. 37", "dvostruki list: vidi i list br. 38",
		`<span class="oznaka-stanja pozor">ovjereno prijenosom</span>`,
		`<a class="moj-list ceka"`, `<a class="moj-list ovjeren"`,
	} {
		if !strings.Contains(s, x) {
			t.Errorf("moja knjiga bez %q", x)
		}
	}
	for _, x := range []string{
		"Vaša knjiga", "Dnevni list po uzoru", `class="breadcrumb"`, "Moj dnevnik", "Knjige vodočuvara", "Zadaj zadatak", "sva branjena područja",
		"dvostruki list –", "8,0 h", "26.10.2026.",
	} {
		if strings.Contains(s, x) {
			t.Errorf("moja knjiga ne smije imati %q", x)
		}
	}
	sadrzaj, _, _ := strings.Cut(s[strings.Index(s, `class="moja-knjiga"`):], "</main>")
	if n := strings.Count(sadrzaj, "btn-primary"); n != 1 {
		t.Errorf("glavnih gumba na stranici: %d, očekivan jedan", n)
	}
	if !strings.Contains(sadrzaj, `href="/vodocuvar/dan?datum=`) {
		t.Error("nema gumba Današnji list")
	}

	// rukovoditeljica ostaje na Knjigama, s crticom i starim datumom
	k := o.zovi(t, o.rukovodi, http.MethodGet, "/vodocuvar?sektor=P&podrucje=1&godina=2026", nil).Body.String()
	for _, x := range []string{`class="breadcrumb"`, "Knjige vodočuvara", "dvostruki list – vidi i list br. 37", "19.10.2026."} {
		if !strings.Contains(k, x) {
			t.Errorf("Knjige bez %q", x)
		}
	}
	if strings.Contains(k, "moja-knjiga") {
		t.Error("rukovoditeljica ne smije dobiti Moju knjigu")
	}
}

// Stanja koja crtež ne pokazuje ostaju: prazna knjiga, zaključena godina i
// poruka nakon povratka na popis.
func TestMojaKnjigaStanjaIzvanCrteza(t *testing.T) {
	o := novaOkolinaDnevnika(t)
	s := o.zovi(t, o.vodocuvar, http.MethodGet, "/vodocuvar?godina=2020&error=nema+lista", nil).Body.String()
	for _, x := range []string{
		"U 2020. još nema listova.", `<span class="oznaka-stanja">arhiva</span>`, "je zaključena istekom godine",
		`<div class="alert alert-danger">nema lista</div>`, `href="/vodocuvar/knjiga.pdf?godina=2020"`,
	} {
		if !strings.Contains(s, x) {
			t.Errorf("stranica bez %q", x)
		}
	}
	// poruku ispisuje base.html, a predložak je ne smije ponoviti
	if n := strings.Count(s, "nema lista"); n != 1 {
		t.Errorf("poruka o grešci ispisana %d puta, očekivano jednom", n)
	}
	u := o.zovi(t, o.vodocuvar, http.MethodGet, "/vodocuvar?godina=2020&success=list+spremljen", nil).Body.String()
	if n := strings.Count(u, `<div class="alert alert-success">list spremljen</div>`); n != 1 {
		t.Errorf("poruka o uspjehu ispisana %d puta, očekivano jednom", n)
	}
}
