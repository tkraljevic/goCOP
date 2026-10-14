package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"

	"gocop/internal/disk"
)

// Prate se mapa baze, mapa arhive, stablo izvornih datoteka i mapa paketa;
// premjestiti se nudi arhiva, izvorne datoteke i paketi, baza ne. Relativne
// putanje postaju apsolutne, od mape iz koje je čvor pokrenut.
func TestMjestaDiska(t *testing.T) {
	m := mjestaDiska(filepath.Join("data", "gocop.db"), filepath.Join("arhiva", "vodostaji.db"), "vodostaji", "")
	tekuca, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	putevi := []string{filepath.Join(tekuca, "data"), filepath.Join(tekuca, "arhiva"), filepath.Join(tekuca, "vodostaji"), ""}
	if len(m) != len(putevi) {
		t.Fatalf("mjesta: %+v", m)
	}
	for i, p := range putevi {
		if m[i].Put != p || m[i].Naziv == "" {
			t.Errorf("mjesto %d: %+v, očekivana mapa %q", i, m[i], p)
		}
	}
	if m[0].Premjesti != "" || m[1].Premjesti != "arhivu" {
		t.Errorf("premještanje: baza %q, arhiva %q", m[0].Premjesti, m[1].Premjesti)
	}
}

// Nadzor mjeri pravi disk čim čvor krene i stane kad čvor stane
func TestPokreniNadzorDiska(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		ctx, stani := context.WithCancel(context.Background())
		n := pokreniNadzorDiska(ctx, filepath.Join(dir, "gocop.db"), filepath.Join(dir, "vodostaji.db"), filepath.Join(dir, "nema"), "")
		synctest.Wait()
		mj, err := disk.Sustav{}.Izmjeri(dir)
		if err != nil {
			t.Fatal(err)
		}
		// baza i arhiva su u istoj mapi, izvornih datoteka nema, paketi se ne prate
		st := n.Stanje()
		if len(st) != 1 || st[0].Disk != mj.Disk || st[0].Ukupno != mj.Ukupno || strings.Join(st[0].Nazivi, ", ") != "bazom, arhivom" {
			t.Errorf("stanje %+v, a disk je %+v", st, mj)
		}
		stani()
		synctest.Wait()
	})
}
