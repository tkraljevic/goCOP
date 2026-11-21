package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// redom provjerava da se dijelovi u HTML-u pojavljuju zadanim redom.
func redom(t *testing.T, html string, dijelovi ...string) {
	t.Helper()
	prije := -1
	for _, d := range dijelovi {
		i := strings.Index(html, d)
		if i < 0 {
			t.Errorf("nema %q", d)
			continue
		}
		if i < prije {
			t.Errorf("%q stoji prije prethodnog odjeljka", d)
		}
		prije = i
	}
}

// O prognozi po dizajnu (goCOP-m3, 13 · Prognoze): kartice redom Lanac
// postaja, Provjera na poplavnim valovima, Postaje, opis metode, Detalji
// (sklopljeni) i Suradnja i podaci; gore povratak na Prognoze te Izvoz u
// Excel i Pričuvni izračun.
func TestOPrognoziRedoslijedKartica(t *testing.T) {
	html := iscrtaj(t, "prognoze_metoda.html", PrognozeMetodaData{
		CurrentUser: &models.User{FullName: "Petra Probić"}, Permissions: &models.UserPermissions{},
		ActiveNav: "prognoze", Izdaje: "COP Osijek",
		Odjeljci: OpisMetode(68, "COP Osijek"), RasponDo: DoseziRaspona,
		Suradnja: suradnja, SuradnjaVeza: suradnjaVeza,
		Letve: []LetvaMetode{{Naziv: "Donja"}},
		Lanac: &SlikaLanca{Width: 400, Height: 120, Cvorovi: []CvorLanca{{Naziv: "Gornja"}}},
		Valovi: &ProvjeraValova{Datoteka: "valovi.csv", Valova: 3,
			Skupine: []SkupinaValova{{Rijeka: "Drava", Model: "satni lanac", Valova: 3, Dosezi: []DosegValova{{Doseg: 24}}}}},
	})
	redom(t, html, `href="/prognoze" class="povratna-poveznica"`, "<h1>O prognozi</h1>",
		`class="btn prog-obrub"`, "Izvoz u Excel", "Pričuvni izračun",
		`<h2>Lanac postaja</h2>`, `<h2>Provjera na poplavnim valovima</h2>`, `<h2>Postaje</h2>`,
		`<h2>Kako čitati prognozu</h2>`, `<h2 class="prog-sklop-naslov">Detalji</h2>`,
		`<details class="prog-detalji">`, `<h2>Suradnja i podaci</h2>`)
	if !strings.Contains(html, `<details class="letva-ploca prog-ploca prog-sklop" id="detalji">`) {
		t.Error("Detalji moraju biti sklopljeni")
	}
	if n := strings.Count(html, `class="letva-ploca prog-ploca`); n < 6 {
		t.Errorf("odjeljaka kao kartica: %d", n)
	}
	if !strings.Contains(html, `<table class="prog-retci prog-metoda-tablica">`) || strings.Contains(html, "data-table") {
		t.Error("tablice nemaju retke na plohi")
	}
}

// Bez baze prognoza, bez izdanja ili sa zatvorenom bazom zaglavlje postavki
// ostaje bez zadnjeg izdanja.
func TestZadnjeIzdanjeBezBaze(t *testing.T) {
	if izdano, l := (&PrognozeHandler{}).zadnjeIzdanje(); izdano != "" || l != nil {
		t.Error("bez baze prognoza")
	}
	if izdano, _ := (&PrognozeHandler{citac: func() *CitacPrognoza { return nil }}).zadnjeIzdanje(); izdano != "" {
		t.Error("bez izdanja")
	}
	c, err := OtvoriPrognoze(bazaPrognoza(t, time.Date(2026, 9, 23, 5, 0, 0, 0, time.UTC).Unix()/3600))
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if izdano, _ := (&PrognozeHandler{citac: func() *CitacPrognoza { return c }}).zadnjeIzdanje(); izdano != "" {
		t.Error("zatvorena baza prognoza")
	}
}

// Postavke po dizajnu: kartica Model prognoze s poveznicom na stranicu
// modela, između izbora modela i ranije prognoze. Izbor modela su
// segmentirani gumbi s istim poljem kao prije.
func TestPostavkeVodeNaModelPrognoze(t *testing.T) {
	h := rukovateljSektora(t)
	h.SetPostavke(predlozakStranice(t, "prognoze_postavke.html"))
	w := httptest.NewRecorder()
	h.ShowPostavke(w, zahtjevOsobe(http.MethodGet, "/prognoze/postavke", "", &models.UserPermissions{IsGlobalAdmin: true}))
	if w.Code != http.StatusOK {
		t.Fatalf("postavke: %d", w.Code)
	}
	redom(t, w.Body.String(), `href="/prognoze" class="povratna-poveznica"`, "<h1>Postavke prognoze</h1>", `href="/prognoze/o-prognozi" class="btn prog-obrub"`,
		`<h2>Satni ili dnevni model</h2>`, `<input type="radio" name="prognoza_prikaz" value=""`,
		`<h2>Model prognoze</h2>`, `<a href="/prognoze/model" class="btn prog-obrub">Otvori model prognoze</a>`,
		`<h2>Ranija prognoza</h2>`, `class="btn prog-obrub">Pronađi izdanje</button>`)
	if strings.Contains(w.Body.String(), "branjenim područjima") {
		t.Error("stara kartica područja je ostala")
	}
}

// Izbor „Tablica, grafovi i izvoz”: označen je samo gumb spremljenog načina,
// a tko ne smije mijenjati dobiva zaključan izbor.
func TestPostavkeOznacenPrikaz(t *testing.T) {
	gumb := func(v string) string { return `name="prognoza_prikaz" value="` + v + `"` }
	for _, nacin := range []string{"", models.PrognozaSatni, models.PrognozaDnevni} {
		html := iscrtaj(t, "prognoze_postavke.html", PrognozePageData{
			CurrentUser: &models.User{FullName: "Petra Probić"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
			ActiveNav: "prognoze", SmijeMijenjati: true, Opcije: models.Opcije{PrognozaPrikaz: nacin},
		})
		if n := strings.Count(html, " checked>"); n != 1 {
			t.Errorf("način %q: označenih gumba %d", nacin, n)
		}
		if !strings.Contains(html, gumb(nacin)+" checked>") {
			t.Errorf("način %q nije označen", nacin)
		}
		if strings.Contains(html, `<fieldset class="prog-segment" disabled>`) {
			t.Errorf("način %q: izbor zaključan, a smije mijenjati", nacin)
		}
	}
	html := iscrtaj(t, "prognoze_postavke.html", PrognozePageData{
		CurrentUser: &models.User{FullName: "Petra Probić"}, Permissions: &models.UserPermissions{},
		ActiveNav: "prognoze", Opcije: models.Opcije{PrognozaPrikaz: models.PrognozaDnevni},
	})
	if !strings.Contains(html, `<fieldset class="prog-segment" disabled>`) || !strings.Contains(html, gumb(models.PrognozaDnevni)+" checked>") ||
		strings.Contains(html, `<button type="submit" class="btn btn-primary">Spremi</button>`) {
		t.Error("bez prava izbor mora biti zaključan, s označenim spremljenim načinom i bez gumba Spremi")
	}
}
