package his2000

import (
	"strings"
	"testing"
	"time"
)

const naslovSylka = "ID;PWXL;N;E\r\nC;Y1;X1;K\"Datum mjerenja\"\r\nC;X2;K\"Protok\"\r\n"

// procitajUnutarRoka čita tablicu i javlja ako čitanje ne završi brzo.
func procitajUnutarRoka(t *testing.T, sylk string) ([]Mjerenje, error) {
	t.Helper()
	type ishod struct {
		m   []Mjerenje
		err error
	}
	gotovo := make(chan ishod, 1)
	go func() {
		m, err := ProcitajMjerenja([]byte(sylk))
		gotovo <- ishod{m, err}
	}()
	select {
	case i := <-gotovo:
		return i.m, i.err
	case <-time.After(5 * time.Second):
		t.Fatal("čitanje tablice nije završilo u 5 s")
		return nil, nil
	}
}

// Red ili stupac koji se ne da pročitati, nula ili izvan granice radne
// tablice: datoteka nije ispravna tablica.
func TestMjerenjaPolozajIzvanTablice(t *testing.T) {
	for _, p := range []struct{ ime, redak, greska string }{
		{"red se ne da pročitati", "C;Y99999999999999999999;X1;K44197", "red 99999999999999999999"},
		{"red nula", "C;Y0;X1;K44197", "red 0"},
		{"red iza granice", "C;Y1048577;X1;K44197", "red 1048577"},
		{"stupac se ne da pročitati", "C;Y2;X99999999999999999999;K44197", "stupac 99999999999999999999"},
		{"stupac iza granice", "C;Y2;X16385;K44197", "stupac 16385"},
		{"položaj u zapisu oblika", "F;P0;Y99999999999999999999", "red 99999999999999999999"},
	} {
		_, err := procitajUnutarRoka(t, naslovSylka+p.redak+"\r\nE\r\n")
		if err == nil || !strings.Contains(err.Error(), p.greska) {
			t.Errorf("%s: %v, očekivano %q", p.ime, err, p.greska)
		}
	}
}

// Zadnji red radne tablice čita se odmah, a tekst vrijednosti koji izgleda
// kao položaj ne pomiče ćeliju.
func TestMjerenjaRubniPolozaji(t *testing.T) {
	sylk := naslovSylka +
		"C;Y1048576;X1;K44197\r\nC;X2;K250.04\r\n" +
		"C;Y3;X1;K44198\r\nC;X2;K\"Y99999999999999999999;X7\"\r\nC;X2;K12.5\r\nE\r\n"
	m, err := procitajUnutarRoka(t, sylk)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 2 || m[0].Protok != "12,5" || m[1].Protok != "250,0" ||
		!m[1].Datum.Equal(time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("mjerenja: %+v", m)
	}
}

// Tablica s više ćelija nego što tablica vodomjerenja može imati ne čita se
// dalje; ista ćelija upisana više puta broji se jednom, pa tablica s
// dopuštenim brojem ćelija čita se kao i prije.
func TestMjerenjaNajviseCelija(t *testing.T) {
	staro := najviseCelijaSylka
	najviseCelijaSylka = 6
	t.Cleanup(func() { najviseCelijaSylka = staro })
	redovi := "C;Y2;X1;K44197\r\nC;X2;K250.04\r\nC;X2;K250.05\r\nC;Y3;X1;K44198\r\nC;X2;K12.5\r\n"
	m, err := procitajUnutarRoka(t, naslovSylka+redovi+"E\r\n")
	if err != nil || len(m) != 2 {
		t.Fatalf("šest ćelija: %+v %v", m, err)
	}
	_, err = procitajUnutarRoka(t, naslovSylka+redovi+"C;Y4;X1;K44199\r\nE\r\n")
	if err == nil || !strings.Contains(err.Error(), "više od 6 ćelija") {
		t.Errorf("sedam ćelija: %v", err)
	}
}
