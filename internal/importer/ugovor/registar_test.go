package ugovor

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Zaključava pomoćne korake uvoza ugovora: vrstu održavanja po broju
// odjeljka, razvrstavanje zaglavlja popisa lokacija, ključ za druge
// uvoznike, kazalo kandidata, greške registra i sažetak.

func TestVrstaPoBrojuOdjeljka(t *testing.T) {
	for broj, vrsta := range map[string]string{
		"1": models.MaintenanceKindWatercourse,
		"2": models.MaintenanceKindReservoir,
		"3": models.MaintenanceKindTorrent,
		"4": models.MaintenanceKindDrainage,
		"5": "",
		"":  "",
	} {
		if got := kindByNumber(broj); got != vrsta {
			t.Errorf("odjeljak %q → %q, očekivano %q", broj, got, vrsta)
		}
	}
}

// Zaglavlje popisa kaže red vode i skupinu; „II. reda” se ne smije pročitati
// kao „I. reda”.
func TestRazvrstavanjeZaglavlja(t *testing.T) {
	slucajevi := []struct {
		zaglavlje, red, skupina string
	}{
		{"VODE II. REDA", models.WaterOrderSecond, ""},
		{"vode II reda", models.WaterOrderSecond, ""},
		{"MEĐUDRŽAVNE VODE", models.WaterOrderFirst, models.WaterGroupInterstate},
		{"OSTALE VODE I. REDA", models.WaterOrderFirst, models.WaterGroupOtherState},
		{"Druge veće vode", models.WaterOrderFirst, models.WaterGroupOtherState},
		{"VODE I. REDA", models.WaterOrderFirst, ""},
		{"VODE I REDA", models.WaterOrderFirst, ""},
		{"NASIPI", "", ""},
	}
	for _, s := range slucajevi {
		red, skupina := classifyHeader(s.zaglavlje)
		if red != s.red || skupina != s.skupina {
			t.Errorf("%q → (%q, %q), očekivano (%q, %q)", s.zaglavlje, red, skupina, s.red, s.skupina)
		}
	}
}

// Drugi uvoznici uspoređuju nazive istim ključem kao ugovor.
func TestKljucZaDrugeUvoznike(t *testing.T) {
	if MatchKey("G.D.K. za CS Puškaš (Baranja)") != "glavni dovodni kanal za cs puskas" {
		t.Errorf("ključ %q", MatchKey("G.D.K. za CS Puškaš (Baranja)"))
	}
}

// Prazan ključ ne ulazi u kazalo, a isti kandidat pod istim ključem samo
// jednom.
func TestKazaloKandidata(t *testing.T) {
	ix := &index{}
	m := map[string][]candidate{}
	ix.add(m, "", candidate{code: "a"})
	ix.add(m, "karasica", candidate{code: "a"})
	ix.add(m, "karasica", candidate{code: "a"})
	ix.add(m, "karasica", candidate{code: "b"})
	if len(m) != 1 || len(m["karasica"]) != 2 {
		t.Errorf("kazalo: %+v", m)
	}
}

// Kad se vode ili nasipi registra ne mogu pročitati, uparivanje ne kreće.
func TestKazaloGreskaRegistra(t *testing.T) {
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
		return Deps{Waters: repository.NewWatercourseRepository(baza, rec), Structures: repository.NewStructureRepository(baza, rec)}
	}
	if _, err := buildIndex(ctx, deps(t, false, ""), podrucjePrimjerica); err == nil {
		t.Error("registar bez voda je prošao")
	}
	if _, err := buildIndex(ctx, deps(t, true, "structures"), podrucjePrimjerica); err == nil {
		t.Error("registar bez objekata je prošao")
	}
	ix, err := buildIndex(ctx, deps(t, true, ""), podrucjePrimjerica)
	if err != nil || len(ix.byCode) != 0 || ix.area.ID != podrucjePrimjerica.ID {
		t.Errorf("prazan registar: %+v, %v", ix, err)
	}
}

// Sažetak izvješća i neobavezan dnevnik.
func TestSazetakIDnevnikUgovora(t *testing.T) {
	r := Report{Area: 16, Locations: make([]Match, 5), Existing: 2, Created: 1, Suggested: 1, Ambiguous: 1,
		ItemsTotal: 9, ItemsNew: 4, ItemsExisting: 5, Blocks: 3}
	ocekivano := "BP 16, 5 lokacija (2 postojećih, 1 novih, 1 prijedloga, 1 dvoznačnih), 9 stavki radova (4 novih, 5 već upisanih), 3 ugovornih stavki"
	if got := r.Summary(); got != ocekivano {
		t.Errorf("sažetak:\n%s", got)
	}
	(&Options{}).logf("ništa")
	var poruke []string
	o := &Options{Log: func(f string, a ...any) { poruke = append(poruke, fmt.Sprintf(f, a...)) }}
	o.logf("lokacija %d", 4)
	if len(poruke) != 1 || poruke[0] != "lokacija 4" {
		t.Errorf("poruke: %v", poruke)
	}
}
