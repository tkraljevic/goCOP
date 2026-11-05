package peers

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
)

// Paket može imati valjan potpis manifesta, a pogrešne bajtove karte.
// Odbijanje takvog paketa ne smije obrisati kartu koju čvor već koristi.
func TestPaketKarteNeBrisePostojecuKartuKadOtisakNeOdgovara(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	mapaA, mapaB := t.TempDir(), t.TempDir()
	k := spremiProbnuKartu(t, mapaA)
	put := izveziKartu(t, a, k, mapaA)
	postojeciBajtovi := append(probnaKarta(1), 'x')
	postojeca, err := SpremiKartu(bytes.NewReader(postojeciBajtovi), mapaB, "Postojeća", "proba")
	if err != nil {
		t.Fatal(err)
	}
	pogresan := prepakiraj(t, put, nil, postojeciBajtovi)
	if _, err := b.UveziKartu(ctxRub, pogresan, mapaB); err == nil || !strings.Contains(err.Error(), "ne odgovara otisku") {
		t.Fatalf("paket s pogrešnom kartom: %v", err)
	}
	provjeriJedinuKartu(t, mapaB, postojeca.Otisak, postojeciBajtovi)
}

// Potpisani opis ima točan otisak, a krivu veličinu. Odbijanje ne smije
// obrisati kartu tog otiska koja je već na čvoru.
func TestKrivaVelicinaNeBriseKartuIstogOtiska(t *testing.T) {
	mapa := t.TempDir()
	k := spremiProbnuKartu(t, mapa)
	paket := zipCopa(t, datotekaCopa{ime: "karta.pmtiles", b: probnaKarta(1)})
	zr, err := zip.NewReader(bytes.NewReader(paket), int64(len(paket)))
	if err != nil {
		t.Fatal(err)
	}
	opis := k
	opis.Bajtova++
	if _, err := raspakirajKartu(zr.File[0], opis, mapa); err == nil || !strings.Contains(err.Error(), "ne odgovara otisku") {
		t.Fatalf("kriva veličina: %v", err)
	}
	provjeriJedinuKartu(t, mapa, k.Otisak, probnaKarta(1))
}

// Ponovljen uvoz istog paketa ostavlja jednu potpunu kartu, bez novih
// privremenih datoteka, s istim opisom i otiscima dijelova.
func TestPonovljeniUvozKarteNeOstavljaDuplikate(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	mapaA, mapaB := t.TempDir(), t.TempDir()
	k := spremiProbnuKartu(t, mapaA)
	put := izveziKartu(t, a, k, mapaA)
	for i := range 2 {
		dobio, err := b.UveziKartu(ctxRub, put, mapaB)
		if err != nil || !reflect.DeepEqual(dobio, k) {
			t.Fatalf("uvoz %d: %+v, %v", i+1, dobio, err)
		}
		provjeriJedinuKartu(t, mapaB, k.Otisak, probnaKarta(1))
	}
}

// Prekid čitanja uklanja samo nedovršeno učitavanje. Ista mapa već može
// sadržavati ispravnu kartu koja mora ostati čitljiva.
func TestPrekinutoUcitavanjeNeDiraPostojecuKartu(t *testing.T) {
	mapa := t.TempDir()
	k := spremiProbnuKartu(t, mapa)
	b := probnaKarta(1)
	r := io.MultiReader(bytes.NewReader(b[:len(b)/2]), iotest.ErrReader(os.ErrClosed))
	_, err := SpremiKartu(r, mapa, "Nova", "")
	if err == nil || !strings.Contains(err.Error(), "nije do kraja stigla") {
		t.Fatalf("prekinuto učitavanje: %v", err)
	}
	provjeriJedinuKartu(t, mapa, k.Otisak, b)
}

func provjeriJedinuKartu(t *testing.T, mapa, otisak string, zeli []byte) {
	t.Helper()
	put := PutKarte(mapa, otisak)
	b, err := os.ReadFile(put)
	if err != nil || !bytes.Equal(b, zeli) {
		t.Fatalf("postojeća karta nije sačuvana: %v", err)
	}
	f, err := os.ReadDir(mapa)
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 1 || f[0].Name() != otisak+".pmtiles" {
		t.Fatalf("umjesto jedne karte ostalo: %v", f)
	}
}
