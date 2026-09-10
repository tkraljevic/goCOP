package web

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"gocop/internal/arhiva"
)

// Cijeli lanac na stvarnoj datoteci: tuđi oblik → pogađanje → upis → gradnja.
func TestLanacOdTudjeDatotekeDoArhive(t *testing.T) {
	koren := t.TempDir()
	// Tuđi oblik: hrvatski datumi, decimalni zarez, suvišan stupac ispred i
	// komentar na vrhu — dakle ništa od onoga što naša arhiva piše.
	sadrzaj := "# izvoz iz nekog sustava\nPostaja;Datum;Vodostaj (cm)\n" +
		"Batina;13.06.2013.;771,0\nBatina;14.06.2013.;776,0\nBatina;15.06.2013.;763,5\n"

	p, tijelo, err := pogodi("izvoz.csv", []byte(sadrzaj))
	if err != nil {
		t.Fatal(err)
	}
	if p.Razdjelnik != ";" || p.StupacVrijeme != 1 || p.StupacVrijednost != 2 {
		t.Fatalf("pogođeno krivo: razdjelnik %q, stupci %d i %d", p.Razdjelnik, p.StupacVrijeme, p.StupacVrijednost)
	}

	u := UvozNiza{Sliv: "dunav", Letva: "batina", Izvor: "probni", Velicina: "vodostaj",
		Vrsta: "srednjak", Zona: "Europe/Zagreb",
		StupacVrijeme: p.StupacVrijeme, StupacVrijednost: p.StupacVrijednost}
	redci, presk, err := pretvori(tijelo, u)
	if err != nil {
		t.Fatal(err)
	}
	if len(redci) != 3 || presk != 0 {
		t.Fatalf("pretvoreno %d redaka, preskočeno %d", len(redci), presk)
	}

	put, err := arhiva.Upisi(koren, u.Sliv, u.Letva, u.Izvor, u.Velicina, u.Vrsta, redci)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(put) != "batina_probni_vodostaj_srednjak_2013.csv" {
		t.Errorf("ime %q", filepath.Base(put))
	}
	b, err := os.ReadFile(put)
	if err != nil {
		t.Fatal(err)
	}
	// Hrvatski datum i decimalni zarez izlaze kao ISO i decimalna točka —
	// jedan oblik ulazi u stablo, bez obzira što je stiglo.
	ocekivano := "datum;vodostaj_cm\n2013-06-13;771\n2013-06-14;776\n2013-06-15;763.5\n"
	if string(b) != ocekivano {
		t.Errorf("zapisano:\n%q\nočekivano:\n%q", string(b), ocekivano)
	}

	baza := filepath.Join(t.TempDir(), "a.db")
	iz, err := arhiva.Izgradi(koren, baza, "batina", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	// Spojenih je nula jer je "probni" nov izvor, a nepoznat izvor ulazi
	// isključen — namjerno. Sučelje na to mora upozoriti.
	if iz.Ocitanja != 3 || iz.Spojenih != 0 {
		t.Errorf("očitanja %d, spojenih %d", iz.Ocitanja, iz.Spojenih)
	}
}
