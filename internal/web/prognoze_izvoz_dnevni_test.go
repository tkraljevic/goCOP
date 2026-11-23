package web

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"gocop/internal/xlsxw"
)

// Letve koje računa samo dnevni model (na Vuki) ulaze u Excel i u grafove,
// a letva bez ijednog izdanog dana (mjerni uređaj u kvaru) ne ulazi. Imena
// letvi i brojevi su izmišljeni.
func TestIzvozLetviSamoDnevnogModela(t *testing.T) {
	v := func(x float64) *float64 { return &x }
	dnevna := LetvaPrognoze{Kod: "gornji-brod", Naziv: "Gornji Brod", Voda: "Vuka", Stacionaza: "rkm 50,0", Vrh: true,
		DnevniOpis: "dnevni model: kiša", ZadnjeMjerenje: "Zadnje mjerenje: 412 cm, 24. 9. u 07:00",
		Dani: []CelijaDana{{CmV: v(415), Raspon: "±12", Dnevna: true}, {CmV: v(421), Raspon: "±15", Dnevna: true}}}
	druga := LetvaPrognoze{Kod: "donji-brod", Naziv: "Donji Brod", Voda: "Vuka", Vrh: true, SadaCmV: v(230),
		DnevniOpis: "dnevni model: Gornji Brod i kiša", Dani: []CelijaDana{{CmV: v(233), Dnevna: true}, {}}}
	kvar := LetvaPrognoze{Kod: "srednji-brod", Naziv: "Srednji Brod", Voda: "Vuka", Vrh: true, SadaCmV: v(51),
		DnevniOpis: "dnevni model: kiša", Dani: []CelijaDana{{}, {}}}
	vrh := LetvaPrognoze{Kod: "izvor", Naziv: "Izvor", Voda: "Vuka", Vrh: true, SadaCmV: v(10)}
	tab := TablicaPrognoza{Naslov: "Pritoke", Letve: []LetvaPrognoze{vrh, dnevna, kvar, druga}}
	data := PrognozePageData{Udio: 70, Izdano: "24.9.2026. u 20:00", Bliski: BliziDosezi,
		Dani: []string{"pet 25.9.", "sub 26.9."}, Tablice: []TablicaPrognoza{tab}}

	var kodovi []string
	for _, x := range hrvatskeLetve(data) {
		kodovi = append(kodovi, x.Kod)
	}
	if strings.Join(kodovi, ",") != "gornji-brod,donji-brod" {
		t.Errorf("grafovi za %v, a trebaju Gornji i Donji Brod", kodovi)
	}

	k := &xlsxw.Knjiga{}
	z := ZaglavljeIzvoza{Centar: "COP Osijek", Mjesto: "Osijek", Datum: time.Date(2026, 9, 24, 20, 0, 0, 0, time.UTC),
		Potpisnici: []PotpisnikIzvoza{{Funkcija: "voditelj Centra obrane od poplava", Ime: "Ivo Ivić"}}}
	listPrognoze(k, z, data, tab)
	var b bytes.Buffer
	if err := k.Zapisi(&b); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var sve strings.Builder
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		x, err := io.ReadAll(r)
		_ = r.Close() // samo čitanje iz memorije
		if err != nil {
			t.Fatal(err)
		}
		sve.Write(x)
	}
	txt := sve.String()
	for _, want := range []string{">Gornji Brod<", ">Donji Brod<", ">415<", ">421<", "±12", ">233<",
		"Gornji Brod i Donji Brod računa samo dnevni model, pa za +6 h i +12 h nema vrijednosti.",
		"redak „Sada” prazan. Gornji Brod: zadnje mjerenje 412 cm, 24. 9. u 07:00."} {
		if !strings.Contains(txt, want) {
			t.Errorf("u izvozu nema %q", want)
		}
	}
	for _, nema := range []string{">Srednji Brod<", ">Izvor<", "Donji Brod: zadnje"} {
		if strings.Contains(txt, nema) {
			t.Errorf("u izvozu je %q", nema)
		}
	}

	if napomenaDnevnih([]LetvaPrognoze{vrh, kvar}) != "" {
		t.Error("napomena bez letvi samo dnevnog modela treba biti prazna")
	}
}
