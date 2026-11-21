package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/sadrzaj"
)

// Spremište sadržaja zadržava pakete zadnja dva izdanja svake letve iz
// kazala u knjizi: i one izdane ovdje i one primljene. Stariji paketi i
// želje za njima nestaju. Bez kazala se ne briše ništa.
func TestPospremanjeStarihPaketa(t *testing.T) {
	ctx := context.Background()
	baza, rec, _ := cvorZaTest(t, "laptop")
	sp, err := sadrzaj.Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	a := &razmjenaArhive{baza: baza, rec: rec, sp: sp, javljeno: map[string]bool{}}

	// bez kazala: ništa se ne briše
	bez := []byte("paket bez kazala")
	if _, err := sp.Upisi(ctx, VrstaPaketa, bez, "server"); err != nil {
		t.Fatal(err)
	}
	a.pospremi(ctx)
	if !sp.Ima(ctx, bez2otisak(bez)) {
		t.Fatal("bez kazala obrisan je paket")
	}

	otisci := map[string]string{}
	kazalo := func(letva string, izdanje int, cvor string, upisi bool) {
		t.Helper()
		b := []byte(fmt.Sprintf("%s v%d", letva, izdanje))
		o := bez2otisak(b)
		otisci[fmt.Sprintf("%s v%d", letva, izdanje)] = o
		if upisi {
			if _, err := sp.Upisi(ctx, VrstaPaketa, b, cvor,
				sadrzaj.Veza{Entitet: EntitetArhive, EntitetID: letva, Uloga: "paket"}); err != nil {
				t.Fatal(err)
			}
		} else if err := sp.Zeli(ctx, o, VrstaPaketa, len(b), "arhiva", ""); err != nil {
			t.Fatal(err)
		}
		p, err := json.Marshal(paketUKnjizi{Letva: letva, Izdanje: izdanje, Otisak: "o", Sadrzaj: o, Izdao: cvor})
		if err != nil {
			t.Fatal(err)
		}
		id := fmt.Sprintf("%08x-0000-7000-8000-%012x", izdanje, len(otisci))
		if _, err := rec.Apply(ctx, []ledger.Version{{VersionID: id, Entity: EntitetArhive, EntityID: letva,
			NodeID: cvor, Payload: p, SchemaVersion: ledger.SchemaVersion}}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 4; i++ {
		kazalo("kisa", i, "server", true)
	}
	kazalo("drava", 1, "server", true)
	kazalo("drava", 2, "server", false) // još nije stigao, traži se

	var dnevnik bytes.Buffer
	log.SetOutput(&dnevnik)
	t.Cleanup(func() { log.SetOutput(nilPisac{}) })
	a.pospremi(ctx)

	for ime, htio := range map[string]bool{"kisa v1": false, "kisa v2": false, "kisa v3": true, "kisa v4": true, "drava v1": true} {
		if sp.Ima(ctx, otisci[ime]) != htio {
			t.Errorf("%s: na računalu %v, očekivano %v", ime, !htio, htio)
		}
	}
	if !sp.Zeljen(ctx, otisci["drava v2"]) {
		t.Error("paket zadnjeg izdanja više se ne traži")
	}
	if sp.Ima(ctx, bez2otisak(bez)) {
		t.Error("paket kojeg nema u kazalu ostao je u spremištu")
	}
	if !strings.Contains(dnevnik.String(), "obrisano 3 starih paketa") {
		t.Errorf("dnevnik: %q", dnevnik.String())
	}

	// zatvoreno spremište: greška ide u dnevnik, krug ide dalje
	dnevnik.Reset()
	if err := sp.Zatvori(); err != nil {
		t.Fatal(err)
	}
	a.pospremi(ctx)
	if !strings.Contains(dnevnik.String(), "pospremanje starih paketa") {
		t.Errorf("greška spremišta nije javljena: %q", dnevnik.String())
	}
	// zatvorena knjiga: greška ide u dnevnik
	dnevnik.Reset()
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	a.pospremi(ctx)
	if !strings.Contains(dnevnik.String(), "pospremanje starih paketa") {
		t.Errorf("greška knjige nije javljena: %q", dnevnik.String())
	}
	// čvor bez spremišta ne radi ništa
	(&razmjenaArhive{}).pospremi(ctx)
}

func bez2otisak(b []byte) string { return sadrzaj.Otisak(b) }

// nilPisac vraća dnevnik u prazno nakon testa
type nilPisac struct{}

func (nilPisac) Write(p []byte) (int, error) { return len(p), nil }
