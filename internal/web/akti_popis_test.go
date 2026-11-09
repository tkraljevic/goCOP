package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

var sektoriZaCipove = []models.Sector{
	{ID: "A", Name: "Sektor A — Mura i gornja Drava"},
	{ID: "B", Name: "Sektor B — Dunav i donja Drava"},
	{ID: "DIREKCIJA", Name: "Direkcija"},
	{ID: "F", Name: "Sektor F"},
}

// Čipovi sektora vode na isti popis: Direkcije nema, pretraga, stupanj, radnja,
// stanje i godina ostaju, a područje i poruke ne prelaze na drugi sektor.
func TestCipoviSektoraStranice(t *testing.T) {
	q := url.Values{"q": {"Batina"}, "stupanj": {"REDOVNA"}, "radnja": {"PREKID"}, "status": {"NACRT"}, "godina": {"2026"},
		"podrucje": {"16"}, "sektor": {"B"}, "success": {"Spremljeno."}, "error": {"Greška"}, "tudje": {"x"}}
	cipovi := cipoviSektoraStranice("/akti", sektoriZaCipove, "B", q)

	var nazivi []string
	for _, c := range cipovi {
		nazivi = append(nazivi, c.Naziv)
	}
	if got := strings.Join(nazivi, " | "); got != "Svi | A · Mura i gornja Drava | B · Dunav i donja Drava | F" {
		t.Errorf("čipovi: %s", got)
	}
	zajednicko := "godina=2026&q=Batina&radnja=PREKID"
	if want := "/akti?" + zajednicko + "&status=NACRT&stupanj=REDOVNA"; cipovi[0].URL != want {
		t.Errorf("„Svi” ne briše sektor: %s", cipovi[0].URL)
	}
	if want := "/akti?" + zajednicko + "&sektor=A&status=NACRT&stupanj=REDOVNA"; cipovi[1].URL != want {
		t.Errorf("čip A: %s", cipovi[1].URL)
	}
	if cipovi[0].Odabran || cipovi[1].Odabran || !cipovi[2].Odabran {
		t.Errorf("odabran mora biti samo B: %+v", cipovi)
	}

	bezIzbora := cipoviSektoraStranice("/akti", sektoriZaCipove, "", url.Values{})
	if !bezIzbora[0].Odabran || bezIzbora[0].URL != "/akti" || bezIzbora[2].URL != "/akti?sektor=B" {
		t.Errorf("bez izbora: %+v", bezIzbora)
	}
}

// Čipovi stanja mijenjaju samo status, pa dosadašnje adrese rade i izbor ostaje.
func TestCipoviStanjaAkata(t *testing.T) {
	q := url.Values{"sektor": {"B"}, "podrucje": {"16"}, "status": {"OVJEREN"}, "error": {"Greška"}}
	c := cipoviStanjaAkata("OVJEREN", q)
	if len(c) != 3 || c[0].Naziv != "Svi" || c[1].Naziv != "Čeka ovjeru" || c[2].Naziv != "Ovjereni" {
		t.Fatalf("čipovi stanja: %+v", c)
	}
	if c[0].URL != "/akti?podrucje=16&sektor=B" || c[1].URL != "/akti?podrucje=16&sektor=B&status=NACRT" || c[2].URL != "/akti?podrucje=16&sektor=B&status=OVJEREN" {
		t.Errorf("adrese: %+v", c)
	}
	if c[0].Odabran || c[1].Odabran || !c[2].Odabran {
		t.Errorf("odabran: %+v", c)
	}
	if c := cipoviStanjaAkata("", url.Values{}); !c[0].Odabran || c[0].URL != "/akti" {
		t.Errorf("bez izbora: %+v", c)
	}
}

func aktOznake(id string, broj int) models.Akt {
	return models.Akt{ID: id, Sektor: "B", Broj: broj, Godina: 2026}
}

// Traka govori samo o nerazriješenim sukobima redaka koji su na popisu, jer
// sukobi vrijede za cijelu bazu.
func TestRecenicaSukoba(t *testing.T) {
	a, b, c, d := aktOznake("a", 7), aktOznake("b", 7), aktOznake("c", 8), aktOznake("d", 8)
	sukobi := map[string]models.SukobOznake{
		"a": {Drugi: []models.Akt{b}},
		"b": {Drugi: []models.Akt{a}},
		"c": {Drugi: []models.Akt{d}, Rjesenje: &models.RjesenjeOznake{}},
		"e": {Ovjere: []models.Akt{a}},
		"t": {Drugi: []models.Akt{a, b}},
		"u": {Drugi: []models.Akt{c}},
	}
	e, tri, u := aktOznake("e", 9), aktOznake("t", 10), aktOznake("u", 11)
	for _, s := range []struct {
		ime  string
		akti []models.Akt
		zeli string
	}{
		{"bez sukoba", []models.Akt{d}, ""},
		{"prazan popis", nil, ""},
		{"riješen", []models.Akt{c}, ""},
		{"nerazriješen izvan popisa", []models.Akt{d}, ""},
		{"par", []models.Akt{a, b, d}, "Dva akta imaju istu oznaku B-7/2026."},
		{"jedan od para na popisu", []models.Akt{a}, "Dva akta imaju istu oznaku B-7/2026."},
		{"tri akta", []models.Akt{tri}, "Više akata ima istu oznaku B-10/2026."},
		{"dvije oznake", []models.Akt{a, u}, "Neki akti imaju istu oznaku: B-7/2026, B-11/2026."},
		{"druga ovjera", []models.Akt{e}, "Isti nacrt ovjeren je na više računala."},
		{"oznaka i druga ovjera", []models.Akt{a, e}, "Dva akta imaju istu oznaku B-7/2026. Isti nacrt ovjeren je na više računala."},
	} {
		if got := recenicaSukoba(s.akti, sukobi); got != s.zeli {
			t.Errorf("%s: %q, želim %q", s.ime, got, s.zeli)
		}
	}
}

// Popis po crtežu: glava, čipovi, traka sukoba samo uz nerazriješen sukob, PDF
// nije u retku, poruke su jednom, a osobi s jednim sektorom nema čipa „Svi”.
func TestPopisAkataPoCrtezu(t *testing.T) {
	o := novaOkolinaOznake(t)
	batina := o.akt(t, "Batina", "laptop", "3FA9C21B0D", models.PhasePrep)
	vukovar := o.akt(t, "Vukovar", "server", "7C11E0A2B4", models.PhaseRegular)

	w := o.zovi(t, o.voditelj, o.vOvl, http.MethodGet, "/akti?error=Probna+gre%C5%A1ka&stupanj=PRIPREMNO", nil)
	imaNaStranici(t, w, "popis", `class="povratna-poveznica">← Dokumenti`, "/akti/spranca", "/akti/primatelji", `name="q"`, `name="podrucje"`, `name="stupanj"`, `name="radnja"`, `name="godina"`,
		`href="/akti?sektor=B&amp;stupanj=PRIPREMNO"`, "status=NACRT", "status=OVJEREN", "Čeka ovjeru", "Ovjereni", `<details class="akti-filtri" open>`)
	html := w.Body.String()
	if n := strings.Count(html, "Probna greška"); n != 1 {
		t.Errorf("poruka greške ispisana je %d puta", n)
	}
	if strings.Contains(html, "akt.pdf") {
		t.Error("PDF je na stranici akta, ne u retku popisa")
	}
	// Izbornik i podnožje dijele sve stranice, pa se gleda samo sadržaj popisa.
	if _, sadrzaj, ok := strings.Cut(html, `class="povratna-poveznica"`); !ok || strings.Contains(strings.ToLower(sadrzaj), "vodomjer") {
		t.Errorf("na zaslonu popisa piše letva, ne vodomjer: %v", ok)
	}
	if strings.Contains(html, "Probna+gre") {
		t.Error("poruka ne smije prijeći u poveznice čipova")
	}

	// Filtar stupnja ostavlja samo Batinu, a sukob joj je s Vukovarom: traka imenuje oznaku.
	imaNaStranici(t, w, "traka sukoba", `class="akti-traka-sukoba"`, "Dva akta imaju istu oznaku B-7/2026", "Sukob rješava voditelj COP-a ili zamjenik", "/akti/"+batina.ID, `name="stupanj"`)
	popis := o.zovi(t, o.voditelj, o.vOvl, http.MethodGet, "/akti?q=nepostoje%C4%87e", nil).Body.String()
	if strings.Contains(popis, `class="akti-traka-sukoba"`) {
		t.Error("traka ne smije stajati kad sukoba nema na popisu")
	}
	if !strings.Contains(popis, "s letve ili gumbom Novi akt") {
		t.Error("prazan popis nema poruku s letvom")
	}

	// Voditelj B ima jedan sektor: prazan izbor bi ga vratio na B, pa čipa „Svi” nema, a nema ni stupca S.
	if strings.Contains(html, `class="ap-s"`) {
		t.Error("stupac S. stoji samo kad su na popisu svi sektori")
	}
	_, sektori, _ := strings.Cut(html, `akti-sektori"`)
	sektori, _, _ = strings.Cut(sektori, "</div>")
	if strings.Contains(sektori, ">Svi<") || !strings.Contains(sektori, "sektor=B") {
		t.Errorf("osobi s jednim sektorom ne nudi se „Svi”: %s", sektori)
	}

	// Globalni administrator ima „Svi” i stupac sektora.
	admin := &models.User{IsActive: true, Username: "admin", FullName: "Iva Administratorić", Duties: []models.Duty{{Role: models.RoleGlobalAdmin, ScopeType: models.ScopeAll, IsActive: true}}}
	svi := o.zovi(t, admin, models.NewUserPermissions(*admin), http.MethodGet, "/akti", nil)
	imaNaStranici(t, svi, "svi sektori", `class="ap-s"`, `data-stupac="S."`, `href="/akti" aria-pressed="true">Svi`, "/akti/"+vukovar.ID)
}

// Prazno „Vrijedi od” ne smije na zaslonu postati godina 1.
func TestPopisBezVrijediOd(t *testing.T) {
	o := novaOkolinaOznake(t)
	a := o.akt(t, "Batina", "laptop", "3FA9C21B0D", models.PhasePrep)
	a.Vrijedi = time.Time{}
	if err := o.aktiRepo.SaveAkt(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	html := o.zovi(t, o.voditelj, o.vOvl, http.MethodGet, "/akti", nil).Body.String()
	if strings.Contains(html, "1. 1. 1.") || strings.Contains(html, "0001") {
		t.Error("prazno vrijeme ispisano je kao godina 1")
	}
}
