package raspakiraj

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"errors"
	"io"
	"strings"
	"testing"
)

// zipSa slaže ZIP sa zadanim stavkama i vraća njegove stavke.
func zipSa(t *testing.T, stavke ...string) []*zip.File {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i, s := range stavke {
		w, err := zw.Create(string(rune('a'+i)) + ".csv")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	return zatvori(t, zw, &buf)
}

func zatvori(t *testing.T, zw *zip.Writer, buf *bytes.Buffer) []*zip.File {
	t.Helper()
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return zr.File
}

// sirovaStavka slaže stavku sa zadanim zaglavljem i sažetim sadržajem, kakav
// god bio.
func sirovaStavka(t *testing.T, metoda uint16, kaze int, stvarno []byte) *zip.File {
	t.Helper()
	var sazeto bytes.Buffer
	fw, err := flate.NewWriter(&sazeto, flate.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(stvarno); err != nil {
		t.Fatal(err)
	}
	if err := fw.Close(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.CreateRaw(&zip.FileHeader{Name: "niz.csv", Method: metoda,
		CompressedSize64: uint64(sazeto.Len()), UncompressedSize64: uint64(kaze)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(sazeto.Bytes()); err != nil {
		t.Fatal(err)
	}
	return zatvori(t, zw, &buf)[0]
}

// Broj stavki zbraja se preko svih ZIP-ova istog uvoza.
func TestStavkeSeZbrajaju(t *testing.T) {
	g := Nova(10, 100)
	if err := g.Stavke(6); err != nil {
		t.Fatalf("prvih 6: %v", err)
	}
	if err := g.Stavke(5); err == nil || !strings.Contains(err.Error(), "najviše 10") {
		t.Errorf("još 5 preko granice: %v", err)
	}
	if err := g.Stavke(4); err != nil {
		t.Errorf("još 4 do granice: %v", err)
	}
}

// Nesažete datoteke i stavke ZIP-ova troše isti ostatak.
func TestBajtoviSeZbrajaju(t *testing.T) {
	g := Nova(10, 100)
	if err := g.Potrosi(40); err != nil {
		t.Fatal(err)
	}
	stavke := zipSa(t, strings.Repeat("x", 50), strings.Repeat("y", 20))
	b, err := g.Procitaj(stavke[0])
	if err != nil || len(b) != 50 {
		t.Fatalf("stavka unutar granice: %d bajtova, %v", len(b), err)
	}
	if _, err := g.Procitaj(stavke[1]); err == nil || !strings.Contains(err.Error(), "veće od") {
		t.Errorf("stavka preko ostatka: %v", err)
	}
	if err := Nova(1, 10).Potrosi(11); err == nil || !strings.Contains(err.Error(), "kad se raspakiraju") {
		t.Errorf("datoteka preko granice: %v", err)
	}
}

// Zadana granica je granica jednog uvoza.
func TestZadana(t *testing.T) {
	g := Zadana()
	if g.najviseStavki != ZadanoStavki || g.najviseBajtova != ZadanoBajtova {
		t.Errorf("zadana granica %+v", g)
	}
}

// Stavka koja nosi više nego što joj zaglavlje kaže ne pročita se cijela, a
// stavka koja se ne da otvoriti javlja grešku čitača ZIP-a.
func TestStavkaSKrivimZaglavljem(t *testing.T) {
	g := Nova(10, 100)
	if _, err := g.Procitaj(sirovaStavka(t, zip.Deflate, 10, bytes.Repeat([]byte{'0'}, 5000))); !errors.Is(err, zip.ErrFormat) {
		t.Errorf("zaglavlje kaže manje: %v", err)
	}
	if _, err := g.Procitaj(sirovaStavka(t, 99, 10, []byte("x"))); !errors.Is(err, zip.ErrAlgorithm) {
		t.Errorf("nepoznato sažimanje: %v", err)
	}
	if _, err := g.Procitaj(sirovaStavka(t, zip.Deflate, 101, []byte("x"))); err == nil || !strings.Contains(err.Error(), "veće od") {
		t.Errorf("zaglavlje kaže više od granice: %v", err)
	}
}

// Čitač broji stvarno pročitane bajtove i kad mu zaglavlje ništa ne kaže.
func TestCitacBrojiProcitano(t *testing.T) {
	g := Nova(1, 5)
	c := &citac{rc: io.NopCloser(strings.NewReader("123456")), g: g}
	if _, err := io.ReadAll(c); err == nil || !strings.Contains(err.Error(), "veće od") {
		t.Errorf("šest bajtova uz granicu od pet: %v", err)
	}
	c = &citac{rc: io.NopCloser(strings.NewReader("12345")), g: Nova(1, 5)}
	if b, err := io.ReadAll(c); err != nil || string(b) != "12345" {
		t.Errorf("pet bajtova uz granicu od pet: %q, %v", b, err)
	}
	if err := c.Close(); err != nil {
		t.Error(err)
	}
}
