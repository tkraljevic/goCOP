package csvlevels

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Zaključava čitanje letvi iz registra za uvoz tablice i pomoćne korake
// prepoznavanja stupaca: greške registra, nazive, razdjelnik i dnevnik.

// Kad se letve ili objekti iz registra ne mogu pročitati, uvoz ne pogađa
// nego javlja grešku.
func TestUcitavanjeLetviGreskaRegistra(t *testing.T) {
	ctx := context.Background()
	deps := func(t *testing.T, shema bool, ukloni string) Deps {
		t.Helper()
		baza, err := db.OpenDB(filepath.Join(t.TempDir(), "registar.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { baza.Close() })
		if shema {
			if err := db.InitSchema(baza); err != nil {
				t.Fatal(err)
			}
		}
		if ukloni != "" {
			if _, err := baza.Exec("DROP TABLE " + ukloni); err != nil {
				t.Fatal(err)
			}
		}
		rec := ledger.New(baza, "test")
		return Deps{Stations: repository.NewStationRepository(baza, rec), Structures: repository.NewStructureRepository(baza, rec)}
	}
	if _, err := loadGauges(ctx, deps(t, false, "")); err == nil {
		t.Error("registar bez letvi je prošao")
	}
	if _, err := loadGauges(ctx, deps(t, true, "structures")); err == nil {
		t.Error("registar bez objekata je prošao")
	}
	idx, err := loadGauges(ctx, deps(t, true, ""))
	if err != nil || len(idx.byName) != 0 || len(idx.byCode) != 0 {
		t.Errorf("prazan registar: %+v, %v", idx, err)
	}
}

// Prazan naziv se ne upisuje u kazalo, a ista letva pod istim nazivom samo
// jednom.
func TestKazaloLetvi(t *testing.T) {
	idx := &gaugeIndex{byName: map[string][]gauge{}, byCode: map[string]*gauge{}}
	g := gauge{name: "Batina", key: "station:1"}
	idx.add("  ", g)
	idx.add("(*)", g)
	idx.add("Batina", g)
	idx.add("BATINA", g)
	idx.add("batina", gauge{name: "Batina", key: "structure:2"})
	if len(idx.byName) != 1 || len(idx.byName["batina"]) != 2 {
		t.Errorf("kazalo: %+v", idx.byName)
	}
}

// Naziv za ispis je naziv stupca s tom letvom; letva bez stupca ostaje pod
// ključem.
func TestNazivLetveZaIspis(t *testing.T) {
	stupci := map[int]*Column{1: {Name: "Batina", Key: "station:a"}, 2: {Name: "Apatin", Key: "station:b"}}
	if got := gaugeName(stupci, models.Reading{StationID: "b"}); got != "Apatin" {
		t.Errorf("naziv %q, očekivano Apatin", got)
	}
	if got := gaugeName(stupci, models.Reading{StationID: "c"}); got != "station:c" {
		t.Errorf("letva bez stupca: %q", got)
	}
}

// Naziv stupca svodi se na mala slova bez dijakritike, a interpunkcija na
// razmak ili ništa.
func TestNormalizacijaNaziva(t *testing.T) {
	for ulaz, ocek := range map[string]string{
		" Dunav - Batina (DHMZ)* ": "dunav batina dhmz",
		"Đakovo/Čepin":             "dakovo cepin",
		"P._Karašica":              "p karasica",
		"":                         "",
	} {
		if got := normalize(ulaz); got != ocek {
			t.Errorf("normalize(%q) = %q, očekivano %q", ulaz, got, ocek)
		}
	}
}

// Razdjelnik se bira po prvom retku: onaj koji se najčešće javlja, a kad
// nijednog nema, točka-zarez.
func TestRazdjelnikTablice(t *testing.T) {
	for tekst, ocek := range map[string]rune{
		"Datum;Batina;Apatin\n01.01.;1,5;2,5":   ';',
		"Datum,Batina,Apatin\n01.01.,1;2":       ',',
		"Datum\tBatina\tApatin\r\n01.01.\t1\t2": '\t',
		"Datum":                                 ';',
		"\nDatum,Batina":                        ',',
	} {
		if got := detectSeparator(tekst); got != ocek {
			t.Errorf("%q → %q, očekivano %q", tekst, got, ocek)
		}
	}
}

// Dnevnik je neobavezan; kad je zadan, prima poruke uvoza.
func TestDnevnikUvozaTablice(t *testing.T) {
	(&Options{}).logf("ništa %d", 1)
	var poruke []string
	o := &Options{Log: func(f string, a ...any) { poruke = append(poruke, fmt.Sprintf(f, a...)) }}
	o.logf("stupac %s", "Batina")
	if len(poruke) != 1 || poruke[0] != "stupac Batina" {
		t.Errorf("poruke: %v", poruke)
	}
	// kraj datoteke usred čitanja nije greška tablice
	if !errIsPartial(io.EOF) || errIsPartial(errors.New("pokvaren redak")) {
		t.Error("samo kraj datoteke je djelomično čitanje")
	}
}
