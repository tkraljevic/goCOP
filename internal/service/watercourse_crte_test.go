package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// spremljeni tok s izvornom točkom rkm i napomenom uz crtu
const spremljeniTok = `{"type":"FeatureCollection","name":"Dunav","crs":{"type":"name"},"features":[` +
	`{"type":"Feature","properties":{"stacionaza_napomena":"moja napomena"},"geometry":{"type":"LineString","coordinates":[[18.8,45.8],[18.9,45.5]]}},` +
	`{"type":"Feature","properties":{"tip":"rkm","rkm":1350},"geometry":{"type":"Point","coordinates":[18.85,45.7]}}]}`

// s karte stiže pomaknuta crta s izračunatom napomenom i izračunatim točkama
const crteSKarte = `{"type":"FeatureCollection","features":[` +
	`{"type":"Feature","properties":{"stacionaza_napomena":"Stacionaža kalibrirana prema ENC-u"},"geometry":{"type":"LineString","coordinates":[[18.81,45.8],[18.91,45.5]]}},` +
	`{"type":"Feature","properties":{"tip":"rkm","rkm":1351,"status":"kalibrirano"},"geometry":{"type":"Point","coordinates":[18.86,45.7]}}]}`

type tokZaProvjeru struct {
	Name     string
	Crs      map[string]any
	Features []struct {
		Properties map[string]any
		Geometry   struct {
			Type        string
			Coordinates json.RawMessage
		}
	}
}

func procitajTok(t *testing.T, s string) tokZaProvjeru {
	t.Helper()
	var tok tokZaProvjeru
	if err := json.Unmarshal([]byte(s), &tok); err != nil {
		t.Fatalf("spojeni tok nije GeoJSON: %v\n%s", err, s)
	}
	return tok
}

// Dunav i Drava: s karte se uzima samo crta, točke rkm, vrh zbirke i
// napomena ostaju iz zapisa.
func TestSpojiCrteTokaRacunaStacionazu(t *testing.T) {
	out, err := SpojiCrteToka(spremljeniTok, crteSKarte, true)
	if err != nil {
		t.Fatal(err)
	}
	tok := procitajTok(t, out)
	if tok.Name != "Dunav" || tok.Crs == nil {
		t.Errorf("vrh zbirke nije sačuvan: %s", out)
	}
	if len(tok.Features) != 2 {
		t.Fatalf("htio crtu i jednu točku, dobio %d značajki: %s", len(tok.Features), out)
	}
	crta, tocka := tok.Features[0], tok.Features[1]
	if crta.Geometry.Type != "LineString" || !strings.Contains(string(crta.Geometry.Coordinates), "18.81") {
		t.Errorf("crta nije ona s karte: %s", out)
	}
	if crta.Properties["stacionaza_napomena"] != "moja napomena" {
		t.Errorf("napomena = %v, htio onu iz zapisa", crta.Properties["stacionaza_napomena"])
	}
	if tocka.Properties["rkm"] != float64(1350) || tocka.Properties["status"] != nil {
		t.Errorf("točka rkm nije izvorna: %v", tocka.Properties)
	}
}

// Kad zapis nema napomene, izračunata napomena s karte se ne upiše.
func TestSpojiCrteTokaBezNapomeneUZapisu(t *testing.T) {
	zapis := strings.Replace(spremljeniTok, `"stacionaza_napomena":"moja napomena"`, `"naziv":"Dunav"`, 1)
	out, err := SpojiCrteToka(zapis, crteSKarte, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "kalibrirana") {
		t.Errorf("izračunata napomena upisana: %s", out)
	}
}

// Ostale vode: napomena s karte je ona koju korisnik vidi i ostaje.
func TestSpojiCrteTokaBezIzracuna(t *testing.T) {
	out, err := SpojiCrteToka(spremljeniTok, crteSKarte, false)
	if err != nil {
		t.Fatal(err)
	}
	tok := procitajTok(t, out)
	if tok.Features[0].Properties["stacionaza_napomena"] != "Stacionaža kalibrirana prema ENC-u" {
		t.Errorf("napomena s karte nije ostala: %s", out)
	}
}

// Bez zapisa (ili s pokvarenim) tok je samo ono što je nacrtano.
func TestSpojiCrteTokaBezZapisa(t *testing.T) {
	for _, zapis := range []string{"", "nije json", `{"features":"x"}`} {
		out, err := SpojiCrteToka(zapis, crteSKarte, true)
		if err != nil {
			t.Fatalf("%q: %v", zapis, err)
		}
		tok := procitajTok(t, out)
		if len(tok.Features) != 1 || tok.Features[0].Geometry.Type != "LineString" {
			t.Errorf("%q: htio samo crtu, dobio %s", zapis, out)
		}
		if _, err := PripremiGeometrijuVode(out); err != nil {
			t.Errorf("%q: spojeni tok ne prolazi provjeru: %v", zapis, err)
		}
	}
	if _, err := SpojiCrteToka(spremljeniTok, "{", true); err == nil {
		t.Error("pokvarene crte s karte su prošle")
	}
}

func TestBezNapomene(t *testing.T) {
	for _, f := range []string{
		`{"type":"Feature","geometry":null}`,
		`{"type":"Feature","properties":null}`,
		`{"type":"Feature","properties":{"naziv":"x"}}`,
		`[1]`,
	} {
		if got := string(bezNapomene(json.RawMessage(f))); got != f {
			t.Errorf("%s promijenjeno u %s", f, got)
		}
	}
	got := string(bezNapomene(json.RawMessage(`{"properties":{"naziv":"x","stacionaza_napomena":"y"}}`)))
	if got != `{"properties":{"naziv":"x"}}` {
		t.Errorf("napomena nije maknuta: %s", got)
	}
}

// Spremanje s karte: ovlast, nepostojeća voda i spajanje sa zapisom u bazi.
func TestSetWatercourseLines(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "crte.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	repo := repository.NewWatercourseRepository(baza, ledger.New(baza, "test-node"))
	svc := NewWatercourseService(repo)
	ctx := context.Background()
	admin := &models.UserPermissions{IsGlobalAdmin: true}

	if err := repo.CreateWatercourse(ctx, &models.Watercourse{Code: "rijeka-dunav", OfficialName: "rijeka Dunav", Name: "Dunav", Geometry: spremljeniTok}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetWatercourseLines(ctx, &models.UserPermissions{}, "rijeka-dunav", crteSKarte); err == nil {
		t.Error("korisnik bez prava spremio tok")
	}
	if err := svc.SetWatercourseLines(ctx, admin, "nema-je", crteSKarte); err == nil {
		t.Error("spremljen tok vode koje nema")
	}
	if err := svc.SetWatercourseLines(ctx, admin, "rijeka-dunav", "{"); err == nil {
		t.Error("pokvarene crte spremljene")
	}
	if err := svc.SetWatercourseLines(ctx, admin, "rijeka-dunav", crteSKarte); err != nil {
		t.Fatal(err)
	}
	w, err := svc.GetWatercourse(ctx, "rijeka-dunav")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.Geometry, "18.81") || !strings.Contains(w.Geometry, `"rkm":1350`) || strings.Contains(w.Geometry, "kalibrirano") {
		t.Errorf("u bazi nije pomaknuta crta s izvornom točkom: %s", w.Geometry)
	}
}

// Dok toka nema u bazi, točke se uzimaju iz paketa geometrija.
func TestZapisTokaIzPaketa(t *testing.T) {
	svc := NewWatercourseService(nil)
	if got := svc.zapisToka(&models.Watercourse{Code: "rijeka-dunav"}); !strings.Contains(got, "FeatureCollection") {
		t.Errorf("Dunav bez zapisa u bazi: htio zapis iz paketa, dobio %.80q", got)
	}
	if got := svc.zapisToka(&models.Watercourse{Code: "a/b"}); got != "" {
		t.Errorf("neispravna šifra: htio prazno, dobio %.80q", got)
	}
	if got := svc.zapisToka(&models.Watercourse{Code: "x", Geometry: "zapis"}); got != "zapis" {
		t.Errorf("zapis iz baze: %q", got)
	}
}
