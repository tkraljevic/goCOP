package web

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Knjige vodočuvara po crtežu: bez dugog uvoda, izbori i polja s natpisima, a imena
// polja, sidro i adrese ostaju. Knjiga koja čeka ili ima zadatak je otvorena, a redak je
// isti kao u Mojoj knjizi. Osobe su izmišljene.
func TestKnjigeVodocuvaraPoCrtezu(t *testing.T) {
	o := novaOkolinaDnevnika(t)
	danas := time.Now().In(models.Zagreb)
	dan := time.Date(danas.Year(), danas.Month(), danas.Day(), 0, 0, 0, 0, models.Zagreb)
	kad := dan.Add(-time.Hour)
	spremi := func(pomak int, izmijeni func(l *models.VodocuvarskiList)) *models.VodocuvarskiList {
		t.Helper()
		l := &models.VodocuvarskiList{ID: uuid.Must(uuid.NewV7()).String(), UserID: o.vodocuvar.ID.String(), Ime: "Pero Perić", Sektor: "P", AreaID: 1,
			Datum: dan.AddDate(0, 0, pomak), Od: "07:00", Do: "15:00", Opis: "obilazak nasipa"}
		izmijeni(l)
		if err := o.repo.Save(context.Background(), l); err != nil {
			t.Fatal(err)
		}
		return l
	}
	spremi(-1, func(l *models.VodocuvarskiList) { l.Broj, l.PredanoAt = 41, &kad })
	spremi(-2, func(l *models.VodocuvarskiList) {
		l.Broj, l.PredanoAt, l.PotvrdenoAt, l.Potvrdio = 40, &kad, &kad, "Ana Anić"
		l.Parafe = []models.Parafa{{Ime: "Iva Ivić"}}
	})
	prenesen := spremi(-3, func(l *models.VodocuvarskiList) { l.Rekonstrukcija = true; l.Opis = "ustava pregledana" })

	anica, err := o.users.GetUserByID(o.rukovodi.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.vod.ZadajZadatak(context.Background(), models.NewUserPermissions(*anica), anica, o.vodocuvar.ID.String(), "obiđi nasip", dan); err != nil {
		t.Fatal(err)
	}

	godina := strconv.Itoa(prenesen.Datum.Year())
	s := o.zovi(t, o.rukovodi, http.MethodGet, "/vodocuvar?sektor=P&podrucje=1&godina="+godina, nil).Body.String()
	for _, x := range []string{
		`href="/dnevnici" class="povratna-poveznica"`, `href="/vodocuvar/kalendar"`, "Kalendar zadataka",
		"Sektor P · BP 1: Primjerica",
		`name="godina"`, `name="sektor"`, `name="podrucje"`, `name="ceka"`, "sva branjena područja", "samo listovi koji čekaju ovjeru",
		`id="zadaj-zadatak"`, `action="/vodocuvar/zadatak"`, `name="vodocuvar"`, `name="tekst"`, `name="za"`,
		"<span>Vodočuvar</span>", "<span>Što treba obići, provjeriti ili napraviti</span>", "<span>Za koji dan</span>",
		"Knjige vodočuvara · BP 1",
		`<details class="knjiga" open>`, "<strong>Pero Perić</strong>",
		"BP 1 · 3 lista, od toga 1 prenesen",
		`<span class="oznaka-stanja pozor">1 čeka ovjeru</span>`, `<span class="oznaka-stanja">1 otvoren zadatak</span>`,
		"/vodocuvar/kalendar?vodocuvar=" + o.vodocuvar.ID.String(), "/vodocuvar/knjiga.pdf?vodocuvar=" + o.vodocuvar.ID.String(),
		"Otvoreni zadaci, pojavit će se na sljedećem listu:", "obiđi nasip (zadao Ana Anić, " + danas.Format("2. 1. 2006.") + ")",
		`action="/vodocuvar/upis"`, "<span>Dan lista</span>", "<span>Upis u dnevnik</span>", `name="datum"`,
		`<a class="moj-list ceka"`, `<a class="moj-list ovjeren"`, "List 41 · ", dan.AddDate(0, 0, -1).Format("2. 1.") + " · 8 h",
		"ovjerio Ana Anić · parafa: Iva Ivić", "Preneseno · ", `<span class="oznaka-stanja pozor">ovjereno prijenosom</span>`,
		"iz ranije evidencije, bez potpisa vodočuvara",
	} {
		if !strings.Contains(s, x) {
			t.Errorf("Knjige bez %q", x)
		}
	}
	for _, x := range []string{"Dnevni list po uzoru", `class="breadcrumb"`, "posta-kartica", "Moj dnevnik", "moja-knjiga", "07:00–15:00"} {
		if strings.Contains(s, x) {
			t.Errorf("Knjige ne smiju imati %q", x)
		}
	}

	// Polje dana nema donju granicu, pa se bilješka može upisati i za dan prenesenog lista.
	_, poslije, _ := strings.Cut(s, `name="datum"`)
	polje, _, _ := strings.Cut(poslije, ">")
	if strings.Contains(polje, "min=") {
		t.Errorf("polje dana lista ne smije imati donju granicu: %s", polje)
	}
	upis := url.Values{"vodocuvar": {o.vodocuvar.ID.String()}, "datum": {prenesen.Datum.Format("2006-01-02")}, "tekst": {"obilazak potvrđen"}}
	w := o.zovi(t, o.rukovodi, http.MethodPost, "/vodocuvar/upis", upis)
	if l := mustUnescape(w.Header().Get("Location")); !strings.Contains(l, "/vodocuvar/"+prenesen.ID) || !strings.Contains(l, "na prenesenom listu") {
		t.Errorf("bilješka za dan prenesenog lista: %d %s", w.Code, l)
	}
	if l, err := o.repo.Get(context.Background(), prenesen.ID); err != nil || len(l.Upisi) != 1 {
		t.Errorf("prenesen list nakon bilješke: %+v, %v", l, err)
	}
}

// Stanja koja crtež ne pokazuje ostaju: prazna knjiga, nema vodočuvara, zaključena godina,
// poruka nakon povratka i osoba koja vodi svoju knjigu, a čita i tuđe.
func TestKnjigeStanjaIzvanCrteza(t *testing.T) {
	o := novaOkolinaDnevnika(t)
	s := o.zovi(t, o.rukovodi, http.MethodGet, "/vodocuvar?sektor=P&error=nema+lista", nil).Body.String()
	for _, x := range []string{`<details class="knjiga" >`, "Pero Perić", "Još nema listova:", `action="/vodocuvar/upis"`, "nema lista"} {
		if !strings.Contains(s, x) {
			t.Errorf("prazna knjiga bez %q", x)
		}
	}
	if n := strings.Count(s, "nema lista"); n != 1 {
		t.Errorf("poruka se na stranici vidi %d puta, a treba jednom", n)
	}
	s = o.zovi(t, o.rukovodi, http.MethodGet, "/vodocuvar?sektor=R", nil).Body.String()
	if !strings.Contains(s, "nema vodočuvara s terenskim zaduženjem") {
		t.Error("nema poruke kad u izboru nema vodočuvara")
	}

	p, bp := "P", 1
	duznost := &models.Duty{UserID: o.rukovodi.ID, Title: "Vodočuvar Primjerica", Role: models.RoleWaterGuard, ScopeType: models.ScopeArea, SectorID: &p, AreaID: &bp}
	if err := repository.NewUserRepository(o.baza, o.rec).AddDuty(duznost); err != nil {
		t.Fatal(err)
	}
	s = o.zovi(t, o.rukovodi, http.MethodGet, "/vodocuvar?sektor=P&godina=2020", nil).Body.String()
	for _, x := range []string{
		"Moj dnevnik 2020", `<span class="oznaka-stanja">arhiva</span>`, "je zaključena istekom godine", "U 2020. još nema listova.",
		"Današnji list", "/vodocuvar/knjiga.pdf?godina=2020", "Knjige vodočuvara", `id="zadaj-zadatak"`,
	} {
		if !strings.Contains(s, x) {
			t.Errorf("osoba sa svojom knjigom i tuđima bez %q", x)
		}
	}
	if strings.Contains(s, `class="moja-knjiga"`) {
		t.Error("osoba koja čita i tuđe knjige ne smije dobiti samo Moju knjigu")
	}
	dan := time.Date(2020, time.March, 10, 0, 0, 0, 0, models.Zagreb)
	if err := o.repo.Save(context.Background(), &models.VodocuvarskiList{ID: uuid.Must(uuid.NewV7()).String(), UserID: o.rukovodi.ID.String(), Ime: "Ana Anić",
		Sektor: "P", AreaID: 1, Datum: dan, Broj: 7, Od: "07:00", Do: "15:00", Opis: "obilazak nasipa"}); err != nil {
		t.Fatal(err)
	}
	s = o.zovi(t, o.rukovodi, http.MethodGet, "/vodocuvar?sektor=P&godina=2020", nil).Body.String()
	if !strings.Contains(s, "List 7 · ") || !strings.Contains(s, "10. 3. · 07:00–15:00 · 8 h") {
		t.Error("Moj dnevnik bez vlastitog lista")
	}
}

// Zadatak zadan iza ponoći po našem vremenu, a prije ponoći po UTC-u, nosi
// naš dan, a ne prethodni.
func TestDatumZadatkaPoNasemVremenu(t *testing.T) {
	f := templateFuncs()["localTime"].(func(time.Time) time.Time)
	kad := time.Date(2026, 11, 7, 23, 30, 0, 0, time.UTC) // 8. 11. u 00:30 kod nas
	if got := f(kad).Format("2. 1. 2006."); got != "8. 11. 2026." {
		t.Errorf("dan zadatka: %q", got)
	}
}
