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
	"gocop/internal/sadrzaj"
)

// List s fotografijom koju ovaj čvor nema: korak pri pokretanju fotografiju
// veže uz list, stavi je na popis za dohvat i to zapiše u dnevnik. Drugi put
// ne piše ništa, a grešku zapiše. Vodočuvar Ivo Ivić je izmišljen.
func TestFotografijeListovaPriPokretanju(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "prilozi.db"))
	if err != nil {
		t.Fatal(err)
	}
	sp, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "sadrzaj.db"))
	if err != nil {
		t.Fatal(err)
	}
	prijasnje := repository.Spremiste()
	repository.SetSpremiste(sp)
	t.Cleanup(func() {
		repository.SetSpremiste(prijasnje)
		_ = sp.Zatvori()
	})
	ctx := context.Background()
	foto := []byte("\xff\xd8 nasip kod ustave")
	l := &models.VodocuvarskiList{UserID: "01900000-0000-7000-8000-0000000017a1", Ime: "Ivo Ivić", Sektor: "B", AreaID: 16,
		Datum: time.Date(2026, time.October, 14, 0, 0, 0, 0, models.Zagreb), Rekonstrukcija: true,
		Prilozi: []models.PrilogLista{{ID: "foto-1", Vrsta: "image/jpeg", Bajtova: len(foto), Sadrzaj: sadrzaj.Otisak(foto)}}}
	if err := repository.NewVodocuvarRepository(baza, ledger.New(baza, "cop-proba")).Save(ctx, l); err != nil {
		t.Fatal(err)
	}

	var zapis bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&zapis)
	t.Cleanup(func() { log.SetOutput(stari) })

	popraviPrilogeListova(ctx, baza)
	if !strings.Contains(zapis.String(), "Fotografije uz dnevne listove: veza uz list upisana za 1 fotografiju; na popisu za dohvat 1") {
		t.Errorf("dnevnik ne kaže što je popravljeno:\n%s", zapis.String())
	}
	zapis.Reset()
	popraviPrilogeListova(ctx, baza)
	if zapis.Len() != 0 {
		t.Errorf("korak bez popravka piše u dnevnik:\n%s", zapis.String())
	}
	baza.Close()
	popraviPrilogeListova(ctx, baza)
	if !strings.Contains(zapis.String(), "nisu do kraja popravljene") {
		t.Errorf("greška nije u dnevniku:\n%s", zapis.String())
	}
}
