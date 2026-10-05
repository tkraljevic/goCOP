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
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Krug akata koji stupaju na snagu radi odmah i zatim u razmaku, grešku
// zapiše u dnevnik i ide dalje, a staje kad stane čvor
func TestKrugAkataNaSnazi(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "krug-akata.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test")
	epizode := service.NewEpisodeService(repository.NewEpisodeRepository(baza, rec), nil, nil)
	akti := service.NewAktService(repository.NewAktiRepository(baza, rec), nil, nil, nil, nil, nil, epizode, "cvor-probni")
	baza.Close() // svaki krug javlja grešku

	var zapis bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&zapis)
	t.Cleanup(func() { log.SetOutput(stari) })

	ctx, stop := context.WithCancel(context.Background())
	gotovo := make(chan struct{})
	go func() {
		pratiAkteNaSnazi(ctx, akti, time.Millisecond)
		close(gotovo)
	}()
	time.Sleep(20 * time.Millisecond)
	stop()
	select {
	case <-gotovo:
	case <-time.After(time.Second):
		t.Fatal("krug nije stao kad je stao čvor")
	}
	if n := strings.Count(zapis.String(), "povijest obrane:"); n < 2 {
		t.Errorf("očekivano više krugova s greškom, zapisano %d:\n%s", n, zapis.String())
	}
}
