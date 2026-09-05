package peers_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/ledger"
)

// Kanal izvezen u datoteku uvozi se na drugi čvor bez mreže, kao razmjena;
// ponovljeni uvoz ne mijenja ništa.
func TestIzvozIUvozKanalaKrozDatoteku(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ured := startNode(t, ctx, "ured")
	laptop := startNode(t, ctx, "laptop")

	ch := ledger.ChannelFor(ledger.ChannelReadings, 16, 2024)
	for i, id := range []string{"r-1", "r-2", "r-3"} {
		if _, err := ured.rec.RecordIn(ctx, ured.db, ch, "probni", id, map[string]int{"cm": 100 + i}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ured.rec.Record(ctx, ured.db, "probni", "zajednicki", map[string]int{"cm": 1}); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "gocop-ocitanja-bp16-2024.db")
	n, err := ured.svc.ExportFile(ctx, path, []string{ch})
	if err != nil || n != 3 {
		t.Fatalf("izvoz: %d, %v", n, err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Size() == 0 {
		t.Fatal("datoteka arhive nije nastala")
	}

	rep, err := laptop.svc.ImportFile(ctx, path)
	if err != nil {
		t.Fatalf("uvoz: %v", err)
	}
	if rep.Versions != 3 || rep.Applied != 3 || rep.From != "ured" || rep.Channels[ch] != 3 {
		t.Errorf("izvješće uvoza: %+v", rep)
	}
	counts, _ := laptop.rec.CountByChannel(ctx)
	if counts[ch] != 3 || counts[""] != 0 {
		t.Errorf("laptop nakon uvoza drži %v, očekivan samo kanal iz datoteke", counts)
	}

	again, err := laptop.svc.ImportFile(ctx, path)
	if err != nil || again.Applied != 0 {
		t.Errorf("ponovljeni uvoz mora biti bez učinka: %+v %v", again, err)
	}

	if _, err := laptop.svc.ImportFile(ctx, filepath.Join(t.TempDir(), "nema.db")); err == nil {
		t.Error("nepostojeća ili tuđa datoteka mora biti odbijena")
	}
}
