package main

import (
	"bytes"
	"context"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Očitanje koje je u knjizi, a nema ga u tablici, vrati obnova pri
// pokretanju i to zapiše u dnevnik. Primljena razmjena ne pokreće obnovu: ona
// prolazi cijelu knjigu, a razmjena i uvoz idu u paketima. Kad nema što
// popraviti, obnova ne piše ništa, a grešku zapiše.
func TestObnovaPovrsineIdePriPokretanju(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "obnova.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	rec := ledger.New(baza, "cop-proba")
	repository.SetReadingHistoryPolicy(repository.ReadingHistoryPolicy{})
	cm := 140
	rd := &models.Reading{StationID: "letva-proba", MeasuredAt: time.Now().UTC().Add(-time.Hour), LevelCm: &cm,
		Source: models.ReadingSourceManual}
	if err := repository.NewReadingRepository(baza, rec).Create(ctx, rd); err != nil {
		t.Fatal(err)
	}
	// primjena se nije dovršila: verzija je u knjizi, a retka nema
	if _, err := baza.Exec(`DELETE FROM readings`); err != nil {
		t.Fatal(err)
	}
	ocitanja := func() int {
		t.Helper()
		var n int
		if err := baza.QueryRow(`SELECT count(*) FROM readings`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	var zapis bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&zapis)
	t.Cleanup(func() { log.SetOutput(stari) })

	// razmjena koja ne donosi ništa za ovo očitanje ne prolazi cijelu knjigu
	if err := primljeno(ctx, baza, rec, &razmjenaArhive{}, nil); err != nil {
		t.Fatal(err)
	}
	if n := ocitanja(); n != 0 || zapis.Len() != 0 {
		t.Fatalf("primljena razmjena je pokrenula obnovu: %d očitanja, dnevnik:\n%s", n, zapis.String())
	}

	obnoviPovrsinu(ctx, baza, rec)
	if !strings.Contains(zapis.String(), "Obnova površine: upisano što je bilo samo u knjizi: očitanja 1") || ocitanja() != 1 {
		t.Errorf("dnevnik ne kaže što je obnova upisala:\n%s", zapis.String())
	}
	zapis.Reset()
	obnoviPovrsinu(ctx, baza, rec)
	if zapis.Len() != 0 {
		t.Errorf("obnova bez popravka piše u dnevnik:\n%s", zapis.String())
	}
	baza.Close()
	obnoviPovrsinu(ctx, baza, rec)
	if !strings.Contains(zapis.String(), "Obnova površine nije dovršena") {
		t.Errorf("greška obnove nije u dnevniku:\n%s", zapis.String())
	}
}
