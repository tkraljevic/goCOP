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

// Krug privremenih imenovanja radi odmah i zatim u razmaku, grešku baze
// zapiše u dnevnik i ide dalje, a staje kad stane čvor
func TestKrugPrivremenihDuznosti(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "krug.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewUserRepository(baza, ledger.New(baza, "test"))
	us := service.NewUserService(repo, service.NewAuthService(repo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	baza.Close() // svaki krug javlja grešku

	var zapis bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&zapis)
	t.Cleanup(func() { log.SetOutput(stari) })

	ctx, stop := context.WithCancel(context.Background())
	gotovo := make(chan struct{})
	go func() {
		pratiPrivremeneDuznosti(ctx, us, time.Millisecond)
		close(gotovo)
	}()
	time.Sleep(20 * time.Millisecond)
	stop()
	select {
	case <-gotovo:
	case <-time.After(time.Second):
		t.Fatal("krug nije stao kad je stao čvor")
	}
	if n := strings.Count(zapis.String(), "privremena imenovanja:"); n < 2 {
		t.Errorf("očekivano više krugova s greškom, zapisano %d:\n%s", n, zapis.String())
	}
}
