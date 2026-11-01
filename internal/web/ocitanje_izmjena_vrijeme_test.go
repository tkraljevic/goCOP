package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Izmjena postojećeg očitanja čuva njegovo vrijeme: obrazac nema oznaku
// zadanog vremena, pa ga skripta pri spremanju ne pomiče na trenutak
// ispravka. Novo očitanje oznaku ima.
func TestIzmjenaOcitanjaCuvaVrijeme(t *testing.T) {
	cm := -122
	rd := &models.Reading{MeasuredAt: time.Date(2026, 10, 24, 21, 0, 0, 0, models.Zagreb), LevelCm: &cm}
	podaci := func(izmjena bool) ReadingFormData {
		return ReadingFormData{CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
			Reading: rd, IsEdit: izmjena, GaugeName: "Donji Miholjac", BackURL: "/readings", LocalValue: "2026-10-24T21:00"}
	}
	uredi := iscrtaj(t, "reading_form.html", podaci(true))
	if strings.Contains(uredi, "data-zadano") || strings.Contains(uredi, "Zadano je sad") {
		t.Error("izmjena očitanja ne smije imati zadano vrijeme koje se osvježava na sada")
	}
	if !strings.Contains(uredi, `value="2026-10-24T21:00"`) {
		t.Error("izmjena mora nositi vrijeme očitanja")
	}
	novo := iscrtaj(t, "reading_form.html", podaci(false))
	if !strings.Contains(novo, `data-zadano="2026-10-24T21:00"`) {
		t.Error("novo očitanje mora imati zadano vrijeme")
	}
}

// Spremanje izmjene čuva datum i vrijeme: nepromijenjeno polje daje izvorni
// trenutak (sa sekundama i u drugom od dva sata 2 h na dan pomaka sata),
// promijenjeno polje daje novo vrijeme, a izmjena bez vremena se odbija.
func TestSpremanjeIzmjeneCuvaDatumIVrijeme(t *testing.T) {
	zahtjev := func(polja url.Values) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/readings/update", strings.NewReader(polja.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return r
	}
	id := "3bdd96db-6697-5517-bef1-a4ec2a0cbb21"
	drugi := time.Date(2026, 10, 25, 1, 30, 17, 0, time.UTC) // 02:30:17 po zimskom vremenu, drugi put taj dan
	for _, tc := range []struct {
		ime, polje, izvorno string
		want                time.Time
	}{
		{"nepromijenjeno, dvostruki sat", "2026-10-25T02:30", drugi.Format(time.RFC3339Nano), drugi},
		{"nepromijenjeno, sa sekundama", "2026-10-24T21:00", "2026-10-24T19:00:42Z", time.Date(2026, 10, 24, 19, 0, 42, 0, time.UTC)},
		{"promijenjen sat", "2026-10-24T20:00", "2026-10-24T19:00:42Z", time.Date(2026, 10, 24, 18, 0, 0, 0, time.UTC)},
		{"promijenjen datum", "2026-10-23T21:00", "2026-10-24T19:00:42Z", time.Date(2026, 10, 23, 19, 0, 0, 0, time.UTC)},
		{"bez izvornog", "2026-10-24T21:00", "", time.Date(2026, 10, 24, 19, 0, 0, 0, time.UTC)},
	} {
		rd, err := readingFromForm(zahtjev(url.Values{"id": {id}, "measured_at": {tc.polje}, "measured_at_utc": {tc.izvorno}, "level_cm": {"-123"}}))
		if err != nil || !rd.MeasuredAt.Equal(tc.want) {
			t.Errorf("%s: %v, %v; želim %v", tc.ime, rd, err, tc.want)
		}
	}
	if _, err := readingFromForm(zahtjev(url.Values{"id": {id}, "level_cm": {"-123"}})); err == nil {
		t.Error("izmjena bez datuma i vremena mora se odbiti")
	}
	if rd, err := readingFromForm(zahtjev(url.Values{"level_cm": {"-123"}})); err != nil || time.Since(rd.MeasuredAt) > time.Minute {
		t.Errorf("novo očitanje bez vremena je sada: %v, %v", rd, err)
	}
}

// Ustava: zapornica je zatvorena ili otvorena, a uz otvorenu se upisuje
// koliko posto; polje postotka vidi se samo uz otvorenu, a stari zapis
// „djelomično” ostaje izabran
func TestObrazacUstaveImaPostotakOtvorenosti(t *testing.T) {
	pct := 40
	obrazac := func(rd *models.Reading) string {
		return iscrtaj(t, "reading_form.html", ReadingFormData{CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
			Reading: rd, IsEdit: true, IsSluice: true, ImaZapornicu: true, Gates: models.Gates, GaugeName: "Ustava Probna", BackURL: "/readings", LocalValue: "2026-11-01T08:00"})
	}
	otvorena := obrazac(&models.Reading{Gate: models.GateOpen, GatePct: &pct})
	if !strings.Contains(otvorena, `name="gate_pct"`) || !strings.Contains(otvorena, `value="40"`) || strings.Contains(otvorena, `data-zapornica-postotak hidden`) {
		t.Error("uz otvorenu zapornicu polje postotka nije vidljivo s upisanom vrijednošću")
	}
	if strings.Contains(otvorena, `value="DJELOMIČNO"`) {
		t.Error("„djelomično” se nudi i novom zapisu")
	}
	if !strings.Contains(obrazac(&models.Reading{Gate: models.GateClosed}), `data-zapornica-postotak hidden`) {
		t.Error("uz zatvorenu zapornicu polje postotka se vidi")
	}
	if !strings.Contains(obrazac(&models.Reading{Gate: models.GatePartial}), `value="DJELOMIČNO" selected`) {
		t.Error("stari zapis „djelomično” nije ostao izabran")
	}
}

// Postotak otvorenosti čita se iz obrasca samo uz otvorenu zapornicu
func TestOtvorenostIzObrasca(t *testing.T) {
	zahtjev := func(pct string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/readings", strings.NewReader(url.Values{"gate_pct": {pct}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return r
	}
	if p, err := otvorenostIzObrasca(zahtjev("40"), models.GateOpen); err != nil || p == nil || *p != 40 {
		t.Errorf("otvorena 40: %v, %v", p, err)
	}
	if p, err := otvorenostIzObrasca(zahtjev("40"), models.GateClosed); err != nil || p != nil {
		t.Errorf("zatvorena ne čita postotak: %v, %v", p, err)
	}
	if p, err := otvorenostIzObrasca(zahtjev(""), models.GateOpen); err != nil || p != nil {
		t.Errorf("otvorena bez postotka: %v, %v", p, err)
	}
	if _, err := otvorenostIzObrasca(zahtjev("četrdeset"), models.GateOpen); err == nil || !strings.Contains(err.Error(), "otvorenost zapornice") {
		t.Errorf("nečitljiv postotak: %v", err)
	}
	// cijeli obrazac s nečitljivim postotkom ne prolazi
	r := httptest.NewRequest(http.MethodPost, "/readings", strings.NewReader(url.Values{"gate": {models.GateOpen}, "gate_pct": {"puno"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, err := readingFromForm(r); err == nil || !strings.Contains(err.Error(), "otvorenost zapornice") {
		t.Errorf("obrazac s nečitljivim postotkom: %v", err)
	}
}

// Brojčana tipkovnica na mobitelu nema minus, pa uz polje vodostaja stoji
// gumb ± koji mijenja predznak; negativni vodostaj se pri izmjeni prikaže
// s minusom
func TestObrazacOcitanjaImaGumbZaPredznak(t *testing.T) {
	cm := -42
	html := iscrtaj(t, "reading_form.html", ReadingFormData{CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Reading: &models.Reading{MeasuredAt: time.Date(2026, 11, 1, 8, 0, 0, 0, models.Zagreb), LevelCm: &cm}, IsEdit: true, GaugeName: "Proba", BackURL: "/readings", LocalValue: "2026-11-01T08:00"})
	for _, zeli := range []string{`data-predznak-za="level_cm"`, `id="level_cm"`, `value="-42"`} {
		if !strings.Contains(html, zeli) {
			t.Errorf("obrazac očitanja nema %q", zeli)
		}
	}
}

// Crpna stanica bez ugrađene ustave nema zapornicu u očitanju, a stanica s
// ustavom je ima (CS Zmajevac, CS Bakanka)
func TestObrazacCrpneStaniceZapornicaSamoSUstavom(t *testing.T) {
	obrazac := func(ima bool) string {
		return iscrtaj(t, "reading_form.html", ReadingFormData{CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
			Reading: &models.Reading{}, IsPump: true, ImaZapornicu: ima, Gates: models.Gates, States: models.StructureStates, GaugeName: "CS Proba", BackURL: "/readings", LocalValue: "2026-11-01T08:00"})
	}
	if strings.Contains(obrazac(false), `name="gate"`) {
		t.Error("crpna stanica bez ustave nudi zapornicu")
	}
	s := obrazac(true)
	if !strings.Contains(s, `name="gate"`) || !strings.Contains(s, `name="gate_pct"`) || !strings.Contains(s, `name="structure_state"`) {
		t.Error("crpna stanica s ustavom nema zapornicu s postotkom uz stanje stanice")
	}
}

// Ustava u sklopu crpne stanice nema zapornicu u svom očitanju, nego
// upućuje na očitanje stanice
func TestObrazacUstaveUSklopuUpucujeNaStanicu(t *testing.T) {
	cs := &models.Structure{ID: uuid.New(), Name: "CS Proba", Kind: models.StructureKindPumpingStation}
	s := iscrtaj(t, "reading_form.html", ReadingFormData{CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Reading: &models.Reading{}, IsSluice: true, ZapornicaNa: cs, Gates: models.Gates, GaugeName: "Ustava Proba", BackURL: "/readings", LocalValue: "2026-11-01T08:00"})
	if strings.Contains(s, `name="gate"`) || !strings.Contains(s, "/readings/new?structure="+cs.ID.String()) || !strings.Contains(s, "CS Proba") {
		t.Error("ustava u sklopu ne upućuje na očitanje crpne stanice ili nudi svoju zapornicu")
	}
}

// Crpna stanica s letvom vanjske strane u očitanju ima oba vodostaja:
// unutarnju stranu (kanal) i vanjsku
func TestObrazacCrpneStaniceSDvijeLetve(t *testing.T) {
	obrazac := func(druga bool) string {
		return iscrtaj(t, "reading_form.html", ReadingFormData{CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
			Reading: &models.Reading{}, IsPump: true, DrugaLetva: druga, States: models.StructureStates, GaugeName: "CS Proba", BackURL: "/readings", LocalValue: "2026-11-01T08:00"})
	}
	s := obrazac(true)
	if !strings.Contains(s, `name="level2_cm"`) || !strings.Contains(s, "Unutarnja strana, kanal") || !strings.Contains(s, "Vanjska strana") {
		t.Error("crpna stanica s dvije letve nema oba vodostaja")
	}
	if strings.Contains(obrazac(false), `name="level2_cm"`) {
		t.Error("crpna stanica s jednom letvom nudi drugi vodostaj")
	}
}

// Obrazac crpne stanice nudi onoliko agregata koliko stanica ima crpki, a
// upisani sati drugog agregata ostaju vidljivi i na stanici s jednom crpkom
func TestObrazacCrpneStaniceAgregatiPoBrojuCrpki(t *testing.T) {
	obrazac := func(kapacitet string, rd *models.Reading) string {
		d := ReadingFormData{CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
			Structure: &models.Structure{Name: "CS Proba", Kind: models.StructureKindPumpingStation, CapacityText: kapacitet},
			Reading:   rd, GaugeName: "CS Proba", BackURL: "/readings", LocalValue: "2026-11-01T08:00"}
		d.postaviVrstuObjekta()
		return iscrtaj(t, "reading_form.html", d)
	}
	s := obrazac("1,5", &models.Reading{})
	if !strings.Contains(s, `name="ag_hours_1"`) || strings.Contains(s, `name="ag_hours_2"`) {
		t.Error("stanica s jednom crpkom ne nudi točno jedan agregat")
	}
	s = obrazac("2,5 + 2x1,75", &models.Reading{})
	if !strings.Contains(s, `name="ag_hours_3"`) || strings.Contains(s, "Broj crpki na stanici") {
		t.Error("stanica s tri crpke nema tri agregata ili javlja višak crpki")
	}
	sati := 7
	s = obrazac("1,5", &models.Reading{AgHours2: &sati})
	if !strings.Contains(s, `name="ag_hours_2"`) || !strings.Contains(s, `value="7"`) {
		t.Error("upisani sati drugog agregata nisu vidljivi pri izmjeni")
	}
	if s = obrazac("2x1 + 2x1", &models.Reading{}); !strings.Contains(s, "Broj crpki na stanici: 4.") || strings.Contains(s, `name="ag_hours_4"`) {
		t.Error("stanica s četiri crpke ne javlja da očitanje bilježi samo tri agregata")
	}
}
