package peers

// Rubovi arhive na disku (SQLite datoteka s kanalima): izvoz bez verzija ili
// mjesta za datoteku, i uvoz datoteke kojoj nedostaje knjiga, čiji se zapis
// ne da pročitati, koja je prazna ili čije verzije čvor ne može upisati.

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/ledger"
)

// Izvoz bez ijedne verzije, s otkazanim čitanjem knjige ili u mapu koje nema
// ne ostavlja datoteku s verzijama.
func TestIzvozArhiveOdbija(t *testing.T) {
	s := rubniCvor(t, "ured-vukovar")
	put := filepath.Join(t.TempDir(), "arhiva.db")
	if _, err := s.ExportFile(ctxRub, put, []string{"ocitanja/16/2026"}); err == nil || !strings.Contains(err.Error(), "nema nijedne verzije") {
		t.Errorf("prazan kanal: %v", err)
	}
	upisiOcitanja(t, s, "ocitanja/16/2026", "r-1")
	otkazan, otkazi := context.WithCancel(ctxRub)
	otkazi()
	if _, err := s.ExportFile(otkazan, put, []string{"ocitanja/16/2026"}); err == nil {
		t.Error("otkazan izvoz je prošao")
	}
	if _, err := s.ExportFile(ctxRub, filepath.Join(t.TempDir(), "nema", "mape", "arhiva.db"), []string{"ocitanja/16/2026"}); err == nil || !strings.Contains(err.Error(), "shema arhive") {
		t.Errorf("izvoz u mapu koje nema: %v", err)
	}
}

// arhivaSaShemom slaže datoteku arhive s oznakom goCOP-a i zadanom tablicom
// knjige (prazno: bez knjige)
func arhivaSaShemom(t *testing.T, tablica string, upiti ...string) string {
	t.Helper()
	put := filepath.Join(t.TempDir(), "arhiva.db")
	baza, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	q := append([]string{`CREATE TABLE archive_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`INSERT INTO archive_meta (key, value) VALUES ('program', 'goCOP'), ('node_id', 'ured-vukovar')`}, upiti...)
	if tablica != "" {
		q = append([]string{tablica}, q...)
	}
	for _, u := range q {
		if _, err := baza.Exec(u); err != nil {
			t.Fatal(err)
		}
	}
	return put
}

// Arhiva bez knjige, sa zapisom koji se ne da pročitati ili bez ijedne
// verzije se ne uvozi i ne mijenja knjigu čvora.
func TestUvozArhiveOdbija(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	labava := `CREATE TABLE record_versions (version_id TEXT, entity TEXT, entity_id TEXT, node_id TEXT, supersedes TEXT,
		archived INTEGER, payload TEXT, created_at DATETIME, schema_version INTEGER, channel TEXT)`
	for _, p := range []struct {
		ime    string
		put    string
		greska string
	}{
		{"bez knjige", arhivaSaShemom(t, ""), "čitanje arhive"},
		{"prazna", arhivaSaShemom(t, ledger.Schema), "arhiva je prazna"},
		{"zapis bez entiteta", arhivaSaShemom(t, labava, `INSERT INTO record_versions VALUES
			('0190a000-0000-7000-8000-000000000001', NULL, 'r-1', 'ured-vukovar', '', 0, '{}', '2026-10-13 08:00:00', 1, 'ocitanja/16/2026')`), "entity"},
	} {
		rep, err := s.ImportFile(ctxRub, p.put)
		if err == nil || !strings.Contains(err.Error(), p.greska) {
			t.Errorf("%s: %v, očekivano %q", p.ime, err, p.greska)
		}
		if rep.Applied != 0 {
			t.Errorf("%s: primijenjeno %d", p.ime, rep.Applied)
		}
	}
	if c, _ := s.rec.CountByChannel(ctxRub); len(c) != 0 {
		t.Errorf("knjiga nakon odbijenih arhiva: %v", c)
	}
}

// Kad čvor verzije iz arhive ne može upisati, uvoz javi grešku, a izvješće
// kaže koliko ih je u datoteci.
func TestUvozArhiveKadUpisPadne(t *testing.T) {
	ured := rubniCvor(t, "ured-vukovar")
	upisiOcitanja(t, ured, "ocitanja/16/2026", "r-1", "r-2")
	put := filepath.Join(t.TempDir(), "gocop-ocitanja-bp16-2026.db")
	if n, err := ured.ExportFile(ctxRub, put, []string{"ocitanja/16/2026"}); err != nil || n != 2 {
		t.Fatalf("izvoz: %d %v", n, err)
	}
	laptop := rubniCvor(t, "laptop-baranja")
	kvarUpisa(t, laptop, "INSERT", "record_versions", "")
	rep, err := laptop.ImportFile(ctxRub, put)
	if err == nil || rep.Versions != 2 || rep.Applied != 0 || rep.From != "ured-vukovar" || rep.Channels["ocitanja/16/2026"] != 2 {
		t.Errorf("uvoz koji padne: %+v %v", rep, err)
	}
}

// Izvoz na mjesto gdje već stoji arhiva koja se ne da ni obrisati ni
// pisati (npr. štapić zaštićen od pisanja) javlja grešku, s oznakom
// arhive ili bez nje.
func TestIzvozArhivePrekoZaticeneDatotekeSamoZaCitanje(t *testing.T) {
	s := rubniCvor(t, "ured-vukovar")
	upisiOcitanja(t, s, "ocitanja/16/2026", "r-1")
	for _, sOznakom := range []bool{false, true} {
		mapa := filepath.Join(t.TempDir(), "stapic")
		if err := os.Mkdir(mapa, 0o755); err != nil {
			t.Fatal(err)
		}
		put := filepath.Join(mapa, "gocop-ocitanja-bp16-2026.db")
		baza, err := sql.Open("sqlite", put)
		if err != nil {
			t.Fatal(err)
		}
		upiti := []string{ledger.Schema}
		if sOznakom {
			upiti = append(upiti, `CREATE TABLE archive_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`)
		}
		for _, u := range upiti {
			if _, err := baza.Exec(u); err != nil {
				t.Fatal(err)
			}
		}
		baza.Close()
		if err := os.Chmod(put, 0o444); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(mapa, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(mapa, 0o755) })
		if f, err := os.Create(filepath.Join(mapa, "proba")); err == nil {
			f.Close()
			t.Skip("mapa samo za čitanje ipak prima datoteke (pokrenuto kao root?)")
		}
		if n, err := s.ExportFile(ctxRub, put, []string{"ocitanja/16/2026"}); err == nil || n != 0 || !strings.Contains(err.Error(), "readonly") {
			t.Errorf("s oznakom %v: %d %v", sOznakom, n, err)
		}
	}
}
