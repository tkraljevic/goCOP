package bp16

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Zaključava pomoćne funkcije uvoza iz stare evidencije Baranje (bez mreže
// i bez stvarnih datoteka): čitanje pristupnih podataka, preslikavanje stanja
// i zapornice, vrijeme očitanja, brojeve i tekst iz evidencije te pad uvoza
// dnevnika na zapisu bez datuma.

func TestCitanjePristupnihPodataka(t *testing.T) {
	put := filepath.Join(t.TempDir(), ".env")
	sadrzaj := "# komentar\n\nDIRECTUS_URL = \"https://primjer.example.com\"\nNEŠTO_DRUGO=1\nbez znaka jednakosti\nDIRECTUS_TOKEN='probni=token'\n"
	if err := os.WriteFile(put, []byte(sadrzaj), 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := LoadEnv(put)
	if err != nil {
		t.Fatal(err)
	}
	// navodnici otpadaju, a znak jednakosti u vrijednosti ostaje
	if src.URL != "https://primjer.example.com" || src.Token != "probni=token" {
		t.Errorf("pročitano: %+v", src)
	}

	if err := os.WriteFile(put, []byte("DIRECTUS_URL=https://primjer.example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadEnv(put); err == nil || !strings.Contains(err.Error(), "DIRECTUS_TOKEN") {
		t.Errorf("bez tokena: %v", err)
	}
	if _, err := LoadEnv(filepath.Join(t.TempDir(), "nema")); err == nil {
		t.Error("nepostojeća datoteka mora javiti grešku")
	}
}

func TestPreslikavanjeStanjaIZapornice(t *testing.T) {
	for ulaz, ocek := range map[string]string{
		"Mirovanje":       models.StructureStateIdle,
		" pokretanje CS ": models.StructureStateStarting,
		"zaustavljanje":   models.StructureStateStopping,
		"Sifoniranje cs":  models.StructureStateSiphoning,
		"KVAR":            models.StructureStateFault,
		"rad":             "", // nepoznato stanje se ne pogađa
		"":                "",
	} {
		if got := mapState(ulaz); got != ocek {
			t.Errorf("stanje %q → %q, očekivano %q", ulaz, got, ocek)
		}
	}
	tekst := func(s string) *string { return &s }
	for _, s := range []struct {
		ulaz      *string
		zapornica string
		dodatak   string
	}{
		{nil, "", ""},
		{tekst(" Z "), models.GateClosed, ""},
		{tekst("o"), models.GateOpen, ""},
		{tekst("DO"), models.GatePartial, ""},
		{tekst("dz"), models.GatePartial, ""},
		{tekst("  "), "", ""},
		// nepoznata oznaka ide u napomenu, s izvornim razmacima
		{tekst(" pola "), "", "zapornica:  pola "},
	} {
		z, d := mapGate(s.ulaz)
		if z != s.zapornica || d != s.dodatak {
			t.Errorf("zapornica %v → (%q, %q)", s.ulaz, z, d)
		}
	}
}

func TestVrijemeOcitanja(t *testing.T) {
	kad, err := measured("2026-03-01", "")
	if err != nil || kad.Location() != models.Zagreb || kad.Format("2006-01-02 15:04") != "2026-03-01 07:00" {
		t.Errorf("bez vremena: %v, %v", kad, err)
	}
	// vrijeme mora imati sekunde
	if _, err := measured("2026-03-01", "07:30"); err == nil {
		t.Error("vrijeme bez sekundi danas nije valjano")
	}
	if _, err := measured("01.03.2026.", "07:00:00"); err == nil {
		t.Error("datum mora biti u obliku 2006-01-02")
	}
	if c := created("2026-03-01T06:00:00+01:00"); c.Format("15:04") != "05:00" || c.Location().String() != "UTC" {
		t.Errorf("vrijeme upisa: %v", c)
	}
	// nečitljivo vrijeme upisa postaje trenutak uvoza
	if c := created("jučer"); c.IsZero() || c.Location().String() != "UTC" {
		t.Errorf("nečitljivo vrijeme upisa: %v", c)
	}
	for ulaz, ocek := range map[string]int{"07:00:00": 0, "05:59:00": 2, "19:00": 12, "": 7, "x": 7} {
		if got := distanceFromSeven(ulaz); got != ocek {
			t.Errorf("udaljenost od sedam za %q: %d, očekivano %d", ulaz, got, ocek)
		}
	}
	if sat("07:00:00") != "07:00" || sat(" 7:0 ") != "7:0" {
		t.Error("skraćivanje sata")
	}
}

func TestNapomenaIIzvorOcitanja(t *testing.T) {
	if got := joinNote(" a ", "", "  ", "b"); got != "a; b" {
		t.Errorf("spajanje napomena: %q", got)
	}
	if sourceFor(" Telemetrija ") != models.ReadingSourceAutomatic || sourceFor("očitao Pero Perić") != models.ReadingSourceManual {
		t.Error("izvor očitanja po napomeni")
	}
	napomena := "Očitao Pero Perić (voda mutna)."
	// zagrada se skida samo kad iza nje nema točke
	if osoba, ostatak := splitNote(&napomena); osoba != "Pero Perić" || ostatak != "(voda mutna)" {
		t.Errorf("osoba %q, napomena %q", osoba, ostatak)
	}
	nula := "0"
	if osoba, ostatak := splitNote(&nula); osoba != "" || ostatak != "" {
		t.Errorf("napomena „0”: %q %q", osoba, ostatak)
	}
}

func TestBrojeviITekstIzEvidencije(t *testing.T) {
	for ulaz, ocek := range map[string]float64{`12.5`: 12.5, `"3,25"`: 3.25, `" 7 km"`: 7, `""`: 0, `"oko pet"`: 0, `null`: 0, ``: 0, `true`: 0} {
		if got := broj(json.RawMessage(ulaz)); got != ocek {
			t.Errorf("broj(%s) = %v, očekivano %v", ulaz, got, ocek)
		}
	}
	if v := num("12,5"); v == nil || *v != 12.5 {
		t.Errorf("num: %v", v)
	}
	if num("dvanaest") != nil {
		t.Error("nečitljiv broj mora dati nil")
	}

	var list models.JournalSheet
	parseWeather(&list, "Vedro, temperatura zraka: -3,5 °C, brzina vjetra: 2-4,5 m/s, tlak zraka: 1013 hPa, oborine: 0")
	if list.Temperature == nil || *list.Temperature != -3.5 || list.WindFrom == nil || *list.WindTo != 4.5 || *list.Pressure != 1013 || *list.Precipitation != 0 {
		t.Errorf("prilike: %+v", list)
	}
	// velika slova se ne prepoznaju
	var drugi models.JournalSheet
	parseWeather(&drugi, "Temperatura zraka: 5")
	if drugi.Temperature != nil {
		t.Errorf("„Temperatura” velikim slovom danas se ne čita: %v", *drugi.Temperature)
	}

	// Sređivanje teksta gubi i nove retke, iako ih pokušava sačuvati.
	if got := sredi("  prvi\tred \n\n drugi   red  "); got != "prvi red drugi red" {
		t.Errorf("sredi: %q", got)
	}
	if geometrija(json.RawMessage(`null`)) != "" || geometrija(json.RawMessage(`{"coordinates":[1,2]}`)) != "" {
		t.Error("geometrija bez vrste nije geometrija")
	}
	if g := `{"type":"Point","coordinates":[18.6,45.8]}`; geometrija(json.RawMessage(g)) != g {
		t.Error("ucrtana točka mora ostati kakva jest")
	}
}

func TestRazvrstavanjeVodeIzEvidencije(t *testing.T) {
	tekst := func(s string) *string { return &s }
	for _, s := range []struct {
		red, vrsta, program        *string
		program2, red2, skup, vrst string
		nasip                      bool
	}{
		{tekst("I. red - Međudržavne"), tekst("Vodotok"), nil, models.ProgramA02, models.WaterOrderFirst, models.WaterGroupInterstate, models.MaintenanceKindWatercourse, false},
		{tekst("I. red - ostale"), tekst("bujica"), nil, models.ProgramA02, models.WaterOrderFirst, models.WaterGroupOtherState, models.MaintenanceKindTorrent, false},
		{tekst("II. red"), tekst("Nasip"), nil, models.ProgramA02, models.WaterOrderSecond, "", models.MaintenanceKindReservoir, true},
		// III. i IV. red bez programa idu u A.03
		{tekst("III. red"), tekst("kanal"), nil, models.ProgramA03, models.WaterOrderThird, "", models.MaintenanceKindDrainage, false},
		{tekst("IV. red"), nil, tekst("A.02 nešto"), models.ProgramA02, models.WaterOrderFourth, "", models.MaintenanceKindDrainage, false},
		{nil, tekst("akumulacija"), tekst("A.03"), models.ProgramA03, "", "", models.MaintenanceKindReservoir, false},
	} {
		p, r, g, k, n := classify(vodaRow{Red: s.red, Vrsta: s.vrsta, Program: s.program})
		if p != s.program2 || r != s.red2 || g != s.skup || k != s.vrst || n != s.nasip {
			t.Errorf("%v %v %v → %s %s %s %s %v", s.red, s.vrsta, s.program, p, r, g, k, n)
		}
	}
	for ime, ocek := range map[string]bool{" Upis ": true, "naziv kanala": true, "": true, "Kanal Probni": false} {
		if placeholder(ime) != ocek {
			t.Errorf("placeholder(%q)", ime)
		}
	}
}

func TestUvozDnevnikaBezDatumaPada(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "dnevnici.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test")
	deps := JournalDeps{
		Journals:    repository.NewJournalRepository(baza, rec),
		Maintenance: repository.NewMaintenanceRepository(baza, rec),
		Waters:      repository.NewWatercourseRepository(baza, rec),
		Structures:  repository.NewStructureRepository(baza, rec),
		Areas:       []models.Area{{ID: 1, SectorID: "P", Name: "Mali sliv Primjerica"}},
		AreaID:      1, DryRun: true,
	}
	src := probniIzvor{zbirke: map[string][]json.RawMessage{
		"vode": raw(`{"id":1,"voda":"Kanal Probni"}`), "a02_stavke": nil, "a03_stavke": nil,
		"a02_evidencija_radova": raw(`{"id":1,"datum":"2026-03-01"}`, `{"id":2,"datum":""}`),
	}}
	if _, err := RunJournals(context.Background(), src, JournalDeps{AreaID: 2, Areas: deps.Areas}); err == nil {
		t.Error("područje kojeg nema mora javiti grešku")
	}
	// Zapis bez datuma (ili kraći od četiri znaka) sruši uvoz umjesto da se
	// preskoči.
	defer func() {
		if recover() == nil {
			t.Error("uvoz zapisa bez datuma danas pada; ako više ne pada, ažuriraj test")
		}
	}()
	_, _ = RunJournals(context.Background(), src, deps)
}
