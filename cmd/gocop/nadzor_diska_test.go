package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

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
		n := pokreniNadzorDiska(ctx, filepath.Join(dir, "gocop.db"), filepath.Join(dir, "vodostaji.db"), filepath.Join(dir, "nema"), "",
			func() string { return "Probni" }, func(context.Context, string, string) (int, error) { return 0, nil })
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

// probnaObavijest broji slanja i retke dnevnika; sat se pomiče rukom
type probnaObavijest struct {
	*obavijestDiska
	sat      time.Time
	tekstovi []string
	dnevnik  []string
	poslano  int
	greska   error
}

func novaProbnaObavijest(t *testing.T, biljeg string) *probnaObavijest {
	p := &probnaObavijest{sat: time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC), poslano: 1}
	p.obavijestDiska = &obavijestDiska{ctx: context.Background(), ime: func() string { return "Laptop Ana" }, biljeg: biljeg,
		sad:  func() time.Time { return p.sat },
		javi: func(format string, args ...any) { p.dnevnik = append(p.dnevnik, fmt.Sprintf(format, args...)) },
		posalji: func(_ context.Context, predmet, tekst string) (int, error) {
			if predmet != predmetObavijestiDiska {
				t.Errorf("predmet %q", predmet)
			}
			p.tekstovi = append(p.tekstovi, tekst)
			return p.poslano, p.greska
		}}
	return p
}

var (
	diskMalo     = disk.Disk{Mjera: disk.Mjera{Disk: "sda", Ukupno: 128e9, Slobodno: 8_290_000_000}, Nazivi: []string{"bazom", "arhivom"}, Premjesti: "arhivu"}
	diskDovoljno = disk.Disk{Mjera: disk.Mjera{Disk: "sda", Ukupno: 128e9, Slobodno: 60e9}, Nazivi: []string{"bazom"}}
)

// Dok je mjesta malo, e-pošta ide jednom na dan, i kad se čvor u
// međuvremenu ponovno pokrene. Kad mjesta opet ima, sljedeći put ide odmah.
func TestObavijestDiskaJednomNaDan(t *testing.T) {
	biljeg := filepath.Join(t.TempDir(), biljegObavijestiDiska)
	p := novaProbnaObavijest(t, biljeg)
	p.nakonProvjere([]disk.Disk{diskDovoljno})
	p.nakonProvjere([]disk.Disk{diskDovoljno, diskMalo})
	if len(p.tekstovi) != 1 {
		t.Fatalf("poslano %d puta", len(p.tekstovi))
	}
	for _, want := range []string{"Na disku čvora Laptop Ana s bazom i arhivom ostalo je 8,2 GB (6 % diska). Oslobodite mjesto ili premjestite arhivu na drugi disk.",
		"Ako ne znate kako, javite administratoru. Proslijedite mu ovu poruku.", "s vašeg računa e-pošte", "jednom na dan"} {
		if !strings.Contains(p.tekstovi[0], want) {
			t.Errorf("e-pošta ne kaže %q:\n%s", want, p.tekstovi[0])
		}
	}
	if strings.Contains(p.tekstovi[0], "60 GB") {
		t.Errorf("e-pošta spominje disk s dovoljno mjesta:\n%s", p.tekstovi[0])
	}
	p.sat = p.sat.Add(23 * time.Hour)
	p.nakonProvjere([]disk.Disk{diskMalo})
	// ponovno pokretanje: biljeg uz bazu pamti zadnju e-poštu
	q := novaProbnaObavijest(t, biljeg)
	q.sat = p.sat
	q.nakonProvjere([]disk.Disk{diskMalo})
	if len(p.tekstovi) != 1 || len(q.tekstovi) != 0 {
		t.Fatalf("unutar dana: %d i %d slanja", len(p.tekstovi), len(q.tekstovi))
	}
	p.sat = p.sat.Add(time.Hour)
	p.nakonProvjere([]disk.Disk{diskMalo})
	if len(p.tekstovi) != 2 {
		t.Fatalf("nakon dana: %d slanja", len(p.tekstovi))
	}
	p.nakonProvjere([]disk.Disk{diskDovoljno})
	if _, err := os.Stat(biljeg); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("biljeg ostaje kad mjesta opet ima: %v", err)
	}
	p.nakonProvjere([]disk.Disk{diskMalo})
	if len(p.tekstovi) != 3 || strings.Join(p.dnevnik, "|") != strings.Repeat("|disk: e-pošta o disku poslana je (primatelja: 1)", 3)[1:] {
		t.Errorf("%d slanja, dnevnik %q", len(p.tekstovi), p.dnevnik)
	}
}

// Kad nitko nije spremio sandučić ili slanje ne uspije, to piše u dnevniku i
// ne ponavlja se svakih deset minuta. Biljeg iz budućnosti ne vrijedi.
func TestObavijestDiskaBezPrimatelja(t *testing.T) {
	dir := t.TempDir()
	p := novaProbnaObavijest(t, filepath.Join(dir, biljegObavijestiDiska))
	p.poslano = 0
	p.nakonProvjere([]disk.Disk{diskMalo})
	p.sat = p.sat.Add(disk.Svakih)
	p.nakonProvjere([]disk.Disk{diskMalo})
	if len(p.tekstovi) != 1 || len(p.dnevnik) != 1 || !strings.Contains(p.dnevnik[0], "nitko na ovom računalu nije spremio lozinku sandučića") {
		t.Errorf("bez primatelja: %d slanja, dnevnik %q", len(p.tekstovi), p.dnevnik)
	}

	if err := os.WriteFile(filepath.Join(dir, biljegObavijestiDiska), []byte(p.sat.Add(48*time.Hour).Format(time.RFC3339)), 0o600); err != nil {
		t.Fatal(err)
	}
	q := novaProbnaObavijest(t, filepath.Join(dir, biljegObavijestiDiska))
	q.greska = errors.New("ana: poslužitelj nije dostupan")
	q.sat = p.sat
	q.nakonProvjere([]disk.Disk{diskMalo})
	if len(q.tekstovi) != 1 || len(q.dnevnik) != 1 || !strings.Contains(q.dnevnik[0], "nije poslana svima (poslano 1): ana: poslužitelj nije dostupan") {
		t.Errorf("greška slanja: %d slanja, dnevnik %q", len(q.tekstovi), q.dnevnik)
	}
}

// Na punom disku biljeg se ne da zapisati: zadnja e-pošta pamti se dok čvor
// radi, pa ne ide svakih deset minuta
func TestObavijestDiskaBezBiljega(t *testing.T) {
	dir := t.TempDir()
	p := novaProbnaObavijest(t, filepath.Join(dir, "nema", biljegObavijestiDiska))
	p.nakonProvjere([]disk.Disk{diskMalo})
	p.sat = p.sat.Add(disk.Svakih)
	p.nakonProvjere([]disk.Disk{diskMalo})
	if len(p.tekstovi) != 1 || len(p.dnevnik) != 2 || !strings.Contains(p.dnevnik[0], "nije spremljen") {
		t.Errorf("bez biljega: %d slanja, dnevnik %q", len(p.tekstovi), p.dnevnik)
	}
	// biljeg koji se ne da obrisati (mapa s datotekom) javi se u dnevniku
	mapa := filepath.Join(dir, "mapa")
	if err := os.MkdirAll(filepath.Join(mapa, "unutra"), 0o700); err != nil {
		t.Fatal(err)
	}
	q := novaProbnaObavijest(t, mapa)
	q.nakonProvjere([]disk.Disk{diskDovoljno})
	if len(q.dnevnik) != 1 || !strings.Contains(q.dnevnik[0], "ne da se obrisati") {
		t.Errorf("brisanje biljega: dnevnik %q", q.dnevnik)
	}
}
