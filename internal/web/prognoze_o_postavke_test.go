package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
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

// podrucjaModela: područja do kojih se dolazi preko dionica letve i
// upisana u obrazac letve, samo za letve iz izdanja, poredana po broju.
func TestPodrucjaModela(t *testing.T) {
	id := uuid.New()
	postaje := map[string]models.Station{
		"gornja": {ID: id},
		"donja":  {ID: uuid.New(), PratePodrucja: []int{34}},
		"izvan":  {ID: uuid.New(), PratePodrucja: []int{8}},
	}
	podrucja := []models.Area{{ID: 34, Name: "Područje primjer"}, {ID: 17, Name: "Područje proba"}, {ID: 8, Name: "Područje treće"}}
	got := podrucjaModela([]PregledLetve{{Letva: "donja"}, {Letva: "gornja"}, {Letva: "nepoznata"}}, postaje,
		map[string][]int{id.String(): {17}}, podrucja)
	if want := []PodrucjeModela{{17, "Područje proba"}, {34, "Područje primjer"}}; !slices.Equal(got, want) {
		t.Errorf("područja: %+v", got)
	}
	if podrucjaModela(nil, postaje, nil, podrucja) != nil {
		t.Error("bez letvi nema područja")
	}
}

// Bez registra, bez izdanja ili s pokvarenom bazom popisa područja nema, a
// zaglavlje postavki ostaje bez zadnjeg izdanja.
func TestPodrucjaPrognozeBezRegistra(t *testing.T) {
	ctx := context.Background()
	letve := []PregledLetve{{Letva: "donja"}}
	if (&PrognozeHandler{}).podrucjaPrognoze(ctx, letve) != nil {
		t.Error("bez registra korisnika")
	}
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "op.db"))
	if err != nil {
		t.Fatal(err)
	}
	korisnici := repository.NewUserRepository(baza, ledger.New(baza, "test"))
	h := &PrognozeHandler{users: service.NewUserService(korisnici, service.NewAuthService(korisnici, repository.NewSessionRepository(baza)), service.NewSSEBroker())}
	if h.podrucjaPrognoze(ctx, nil) != nil {
		t.Error("bez letvi izdanja")
	}
	baza.Close()
	if h.podrucjaPrognoze(ctx, letve) != nil {
		t.Error("baza registra zatvorena")
	}

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

// Postavke po dizajnu: kartica Model po branjenim područjima s područjima
// letvi iz izdanja i oznakom „U izradi”, između izbora modela i ranije
// prognoze; bez područja samo rečenica da je uređivanje u izradi. Izbor
// modela su segmentirani gumbi s istim poljem kao prije.
func TestPostavkeModelPoBranjenimPodrucjima(t *testing.T) {
	h := rukovateljSektora(t)
	h.SetPostavke(predlozakStranice(t, "prognoze_postavke.html"))
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	stranica := func() string {
		t.Helper()
		w := httptest.NewRecorder()
		h.ShowPostavke(w, zahtjevOsobe(http.MethodGet, "/prognoze/postavke", "", admin))
		if w.Code != http.StatusOK {
			t.Fatalf("postavke: %d", w.Code)
		}
		return w.Body.String()
	}
	html := stranica()
	if !strings.Contains(html, "<h2>Model po branjenim područjima</h2>") ||
		!strings.Contains(html, "<p>Uređivanje modela po branjenim područjima je u izradi.</p>") || strings.Contains(html, "U izradi</span>") {
		t.Error("bez područja na letvama mora stajati samo rečenica")
	}

	ctx := context.Background()
	st, err := h.stations.GetStationByCode(ctx, "donji-miholjac")
	if err != nil || st == nil {
		t.Fatalf("letva: %v", err)
	}
	st.PratePodrucja = []int{34}
	if err := h.stations.UpdateStation(ctx, admin, st); err != nil {
		t.Fatal(err)
	}
	html = stranica()
	redom(t, html, `href="/prognoze" class="povratna-poveznica"`, "<h1>Postavke prognoze</h1>", `href="/prognoze/o-prognozi" class="btn prog-obrub"`,
		`<h2>Satni ili dnevni model</h2>`, `<input type="radio" name="prognoza_prikaz" value=""`,
		`<h2>Model po branjenim područjima</h2>`, "Branjena područja s letvama u prognozi.",
		`<li><span>BP 34 · Područje primjer</span><span class="prog-bp-oznaka">U izradi</span></li>`,
		`<h2>Ranija prognoza</h2>`, `class="btn prog-obrub">Pronađi izdanje</button>`)
	for _, nema := range []string{"BP 20", "BP 8 "} {
		if strings.Contains(html, nema) {
			t.Errorf("područje bez letve iz izdanja: %q", nema)
		}
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
