package web

import (
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
	txt := tekstKnjige(t, k)
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

// Sažetak ima Vuku ispod Mure, pa Karašicu i Vučicu, samo s letvama koje imaju našu prognozu:
// letva s mjernim uređajem u kvaru (bez dana) se preskače. Brojevi su
// izmišljeni.
func TestSazetakSVukom(t *testing.T) {
	v := func(x float64) *float64 { return &x }
	data := PrognozePageData{Tablice: []TablicaPrognoza{{Naslov: "Pritoke", Letve: []LetvaPrognoze{
		{Kod: "bracevci", Naziv: "Bračevci", Voda: "Vuka", SadaCmV: v(100), DnevniOpis: "dnevni model: kiša",
			Dani: []CelijaDana{{CmV: v(104), Naslov: "pet 25.9."}}},
		{Kod: "beketinci", Naziv: "Beketinci", Voda: "Vuka", SadaCmV: v(51), DnevniOpis: "dnevni model: kiša",
			Dani: []CelijaDana{{Naslov: "pet 25.9."}}},
		{Kod: "vuka", Naziv: "Vuka", Voda: "Vuka", SadaCmV: v(200), DnevniOpis: "dnevni model: Bračevci i kiša",
			Dani: []CelijaDana{{CmV: v(190), Naslov: "pet 25.9."}}},
	}}}}
	var imena []string
	for _, p := range postajeSazetka(data)["Vuka"] {
		imena = append(imena, p.Ime)
	}
	if strings.Join(imena, ",") != "Bračevci,Vuka" {
		t.Errorf("Vuka u sažetku: %v, a trebaju Bračevci i Vuka", imena)
	}
	var vode []string
	for _, g := range sazetakSkupine {
		vode = append(vode, g.Voda)
	}
	if strings.Join(vode, ",") != "Dunav,Drava,Mura,Vuka,Karašica,Vučica" {
		t.Errorf("redoslijed voda u sažetku: %v", vode)
	}
	if pritoka("Drava") || !pritoka("Vučica") {
		t.Error("Drava nije pritoka u sažetku, a Vučica jest")
	}
}

// Postaja bez svoje krivulje dobiva protok susjedne postaje s njezinim
// imenom; postaja bez susjeda ostaje kakva je. Letva koja je samo u popisu
// izdanja, a ne u tablicama, ulazi u sažetak. Brojevi su izmišljeni.
func TestProtokSaSusjednihISazetakIzPopisa(t *testing.T) {
	v := func(x float64) *float64 { return &x }
	data := PrognozePageData{Letve: []LetvaPrognoze{
		{Kod: "vukovar", Naziv: "Vukovar", SadaCmV: v(100), Racuna: "vodostaj", Dani: []CelijaDana{{CmV: v(101)}}},
		{Kod: "ilok", Naziv: "Ilok", SadaCmV: v(90), SadaQV: v(1500), Racuna: "vodostaj", Dani: []CelijaDana{{CmV: v(91), QV: v(1510)}}},
	}}
	out := postajeSazetka(data)
	if len(out["Dunav"]) != 2 {
		t.Fatalf("Dunav u sažetku: %+v", out["Dunav"])
	}
	po := map[string]LetvaPrognoze{"ilok": data.Letve[1]}
	protokSaSusjednih(out, po, map[string]string{"vukovar": "ilok"})
	vu, il := out["Dunav"][0], out["Dunav"][1]
	if vu.QPrema != "Ilok" || vu.SadaQ == nil || *vu.SadaQ != 1500 || len(vu.DaniQ) != 1 || *vu.DaniQ[0] != 1510 {
		t.Errorf("Vukovar s protokom Iloka: %+v", vu)
	}
	if il.QPrema != "" {
		t.Errorf("Ilok ima svoj protok: %+v", il)
	}
}
