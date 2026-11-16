package web

import (
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	webassets "gocop/web"
)

// Kartica Dežurstva slaže plan: tko dežura sada, što je bilo danas i jučer
// (najnovije prvo), a ostalo ne pokazuje; vrijeme piše kao dnevnik COP-a.
func TestRasporedDezurstava(t *testing.T) {
	sad := time.Date(2026, 10, 26, 9, 30, 0, 0, models.Zagreb)
	bp := 16
	plan := []models.Dezurstvo{
		{UserName: "Marko Primjer", Od: sad.Add(-3 * time.Hour), Do: sad.Add(9 * time.Hour), Opis: "Dežurstvo u COP-u", Mjesto: models.MjestoUred},
		{UserName: "Iva Primjer", Od: sad.Add(-15 * time.Hour), Do: sad.Add(-3 * time.Hour), Opis: "Dežurstvo u COP-u", Potvrdio: "Marko Primjer"},
		{UserName: "Ivan Primjer", Od: sad.Add(-26 * time.Hour), Do: sad.Add(-18 * time.Hour), Opis: "Obilazak", Mjesto: models.MjestoTeren, Podrucje: &bp},
		{UserName: "Stari Primjer", Od: sad.AddDate(0, 0, -5), Do: sad.AddDate(0, 0, -5).Add(8 * time.Hour)},
	}
	sada, danas := rasporediDezurstva(plan, []models.Area{{ID: 16, Name: "Mali sliv Primjer"}}, sad, nil, nil)
	if len(sada) != 1 || sada[0].Osoba != "Marko Primjer" || !sada[0].Sad {
		t.Fatalf("sada: %+v", sada)
	}
	if len(danas) != 3 || danas[0].Osoba != "Marko Primjer" || danas[2].Osoba != "Ivan Primjer" {
		t.Fatalf("danas i jučer: %+v", danas)
	}
	if danas[2].Za != "Mali sliv Primjer" || !danas[2].Teren || danas[0].Za != "cijeli sektor" {
		t.Errorf("za i mjesto: %+v", danas)
	}
	if danas[0].OdTekst != "pon 26. 10. 06:30" || danas[0].DoTekst != "18:30" || danas[1].DoTekst != "26. 10. 06:30" {
		t.Errorf("vrijeme: %q–%q, %q", danas[0].OdTekst, danas[0].DoTekst, danas[1].DoTekst)
	}
}

func TestSektorKarticeDezurstava(t *testing.T) {
	b := "B"
	sektori := []models.Sector{{ID: "DIREKCIJA", Level: 1}, {ID: "A", Name: "Sektor A"}, {ID: "B", Name: "Sektor B"}}
	if got := sektorKartice("A", nil, sektori); got != "A" {
		t.Errorf("iz adrese: %s", got)
	}
	if got := sektorKartice("", &models.User{Duties: []models.Duty{{SectorID: &b}}}, sektori); got != "B" {
		t.Errorf("iz zaduženja: %s", got)
	}
	if got := sektorKartice("", &models.User{}, sektori); got != "A" {
		t.Errorf("prvi sektor: %s", got)
	}
	c := cipSektoraDezurstava("B", sektori)
	if !strings.HasSuffix(c.Natpis, ": B") || len(c.Izbor) != 3 || !c.Izbor[2].Odabran || c.Izbor[2].URL != "/users/dezurstva?sector=B" {
		t.Errorf("čip: %+v", c)
	}
}

// Bez plana koji osoba smije vidjeti kartica to kaže, a uvijek vodi na
// vlastite sate na Profilu; s planom pokazuje sve dijelove.
func TestKarticaDezurstavaIscrtava(t *testing.T) {
	bez := iscrtaj(t, "djelatnici_dezurstva.html", DezurstvaPageData{CurrentUser: &models.User{FullName: "Vodočuvar"}, Permissions: &models.UserPermissions{}, Sektor: "B"})
	if !strings.Contains(bez, "još nema dnevnika COP-a") || !strings.Contains(bez, `href="/profile#dezurstva"`) {
		t.Error("kartica bez plana")
	}
	s := iscrtaj(t, "djelatnici_dezurstva.html", DezurstvaPageData{CurrentUser: &models.User{FullName: "Uprava"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true}, Sektor: "B",
		Sada:       []RedakDezurstva{{Osoba: "Marko Primjer", Opis: "Dežurstvo u COP-u", Za: "cijeli sektor", OdTekst: "pon 26. 10. 06:00", DoTekst: "18:00", Sad: true}},
		DanasJucer: []RedakDezurstva{{Osoba: "Iva Primjer", OdTekst: "ned 25. 10. 18:00", DoTekst: "26. 10. 06:00", Potvrdio: "Marko Primjer"}},
		Planovi:    []PlanDezurstava{{Naslov: "Dnevnik COP-a, 2026.", Razdoblje: "od 12. 10. 2026.", Stanje: "otvoren", URL: "/dnevnici/x/dezurstva", Dezurstava: 47}},
		URLPlana:   "/dnevnici/x/dezurstva"})
	for _, want := range []string{">Dežura sada</h2>", "<strong>Marko Primjer</strong>", "potvrdio Marko Primjer", "47 dežurstava", `href="/dnevnici/x/dezurstva"`, `aria-current="page">Dežurstva</span>`} {
		if !strings.Contains(s, want) {
			t.Errorf("kartica nema %s", want)
		}
	}
}

// Kroz rutu, s pravim dnevnikom COP-a: operater sektora centra vidi da
// dežura i plan, a rukovoditelj drugog sektora ne vidi plan centra P.
func TestKarticaDezurstavaKrozRutu(t *testing.T) {
	p := novaCOPProba(t)
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("djelatnici_dezurstva.html")...)
	if err != nil {
		t.Fatal(err)
	}
	k := NewDezurstvaDjelatnika(p.h.journals, p.users, tp)
	k.sada = func() time.Time { return time.Date(2026, 10, 2, 10, 0, 0, 0, models.Zagreb) }
	p.mux.HandleFunc("GET /users/dezurstva", k.Show)
	if _, err := p.baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('D', 'Sektor D', 'VGO Drugovo', 'COP Drugovo')`); err != nil {
		t.Fatal(err)
	}
	operater := p.osoba(t, "ddezurni", "Dora Dežurna", "", models.Duty{Title: "Dežurni operater", Role: models.RoleOperator, ScopeType: models.ScopeSector})
	plan := url.Values{"user_id": {operater.ID.String()}, "od_date": {"2026-10-02"}, "od_time": {"08:00"}, "do_time": {"16:00"}, "opis": {models.OpisiRada[0].Opis}}
	if got := ishod(p.salji(t, operater, http.MethodPost, "/dnevnici/"+p.cop.ID+"/dezurstva", plan)); got != "success" {
		t.Fatalf("upis u plan: %s", got)
	}
	w := p.salji(t, operater, http.MethodGet, "/users/dezurstva?sector=P", nil)
	tijelo := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(tijelo, "<strong>Dora Dežurna</strong>") || !strings.Contains(tijelo, `href="/dnevnici/`+p.cop.ID+`/dezurstva"`) {
		t.Errorf("operater na kartici: %d %q", w.Code, tijelo)
	}
	d := "D"
	drugi := &models.User{ID: uuid.New(), Username: "rdrugi", FullName: "Rudi Drugi", IsActive: true}
	if err := p.userRepo.CreateUser(drugi, &models.Duty{Title: "Rukovoditelj sektora", Role: models.RoleSectorLeader, ScopeType: models.ScopeSector, SectorID: &d, IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	if tijelo := p.salji(t, drugi, http.MethodGet, "/users/dezurstva?sector=P", nil).Body.String(); !strings.Contains(tijelo, "nema plana dežurstava koji smijete vidjeti") || strings.Contains(tijelo, "Dora Dežurna") {
		t.Error("rukovoditelj drugog sektora vidi plan centra P")
	}
}
