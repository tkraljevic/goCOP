package web

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/xlsxw"
)

// Izvoz nosi zaglavlje s izdavačem, letve u stupcima, našu vrijednost s
// rasponom i protokom, tuđe prognoze u svojim recima, te potpise voditelja
// centra i zamjenika. Srpska letva ulazi sa svojim mjerenjem i srpskom
// prognozom; država stoji u svom retku, a kilometar u jednom obliku.
// Brojevi su izmišljeni.
func TestIzvozPrognozeUExcel(t *testing.T) {
	v := func(x float64) *float64 { return &x }
	data := PrognozePageData{
		Udio:   70,
		Izdano: "24.9.2026. u 08:00", Bliski: BliziDosezi, Dani: []string{"čet 24.9.", "pet 25.9."},
	}
	tab := TablicaPrognoza{Naslov: "Dunav", Letve: []LetvaPrognoze{
		{Kod: "bratislava", Naziv: "Bratislava (Slovačka)", Voda: "Dunav", Pregledna: true, SadaCmV: v(300)},
		{Kod: "komarom", Naziv: "Komárom (Mađarska)", Voda: "Dunav", TudiVrh: true, SadaCmV: v(80),
			Dani: []CelijaDana{{Tude: []TudaCelija{{Oznaka: "HU", CmV: 85}}}, {}}},
		{Kod: "bezdan", Naziv: "Bezdan (Srbija)", Voda: "Dunav", Stacionaza: "rkm 1.425,59", Pregledna: true,
			SadaCmV: v(120), Dani: []CelijaDana{{Tude: []TudaCelija{{Oznaka: "RS", CmV: 131}}}, {}}},
		{Kod: "batina", Naziv: "Batina", Voda: "Dunav", Stacionaza: "rkm 1424+850", Racuna: "vodostaj", SadaCmV: v(150), SadaQV: v(2100),
			Vrijednosti: []VrijednostPrognoze{{DosegH: 6, Ima: true, CmV: v(152), CmRaspon: "±3"}, {DosegH: 12, Ima: true, CmV: v(155)}},
			Dani: []CelijaDana{
				{CmV: v(160.4), Raspon: "±5", QV: v(2249.7), Tude: []TudaCelija{{Oznaka: "HU", CmV: 158}}},
				{CmV: v(171), Raspon: "±9", QV: v(2400), Dnevna: true},
			}},
	}}
	z := ZaglavljeIzvoza{Organizacija: "Hrvatske vode", Odjel: "VGO za Dunav i donju Dravu, Osijek",
		Centar: "COP Osijek", Mjesto: "Osijek", Datum: time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC),
		Potpisnici: []PotpisnikIzvoza{{Funkcija: "zamjenik voditelja Centra obrane od poplava", Ime: "Pero Perić"}}}
	k := &xlsxw.Knjiga{}
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
		r, _ := f.Open()
		x, _ := io.ReadAll(r)
		r.Close()
		sve.Write(x)
	}
	txt := sve.String()
	for _, nema := range []string{">Bratislava<", ">Komárom<", "hidmet", "Naša prognoza", "1424+850"} {
		if strings.Contains(txt, nema) {
			t.Errorf("u izvozu je %s, a za nju nemamo prognozu", nema)
		}
	}
	for _, want := range []string{"PROGNOZA VODOSTAJA — Dunav", "VGO za Dunav i donju Dravu, Osijek", "COP Osijek",
		"Batina", "čet 24.9. 07 h", "±5", ">160<", "Prognoza COP Osijek.", "metoda analognih situacija", "u 70 % slučajeva", "obuhvaća isti udio (70 %)", "dnevni", "satni", ">2250<", ">158<",
		"zamjenik voditelja Centra obrane od poplava", "Pero Perić",
		">Bezdan<", ">131<", ">RS<", ">RH<", ">država<", ">km<", ">1.425,6<", // srpska letva s prognozom svoje službe
		`s="15"`, `s="16"`, `s="17"`, `s="20"`} { // raspon sivo, protok ukošeno, početak razdoblja crtom
		if !strings.Contains(txt, want) {
			t.Errorf("u izvozu nema %q", want)
		}
	}
}

// Tablicu potpisuje jedan: onaj od voditelja i zamjenika koji je izvozi;
// kad je izvozi netko treći, potpis stoji na voditelju centra.
func TestIzdavacPrognoze(t *testing.T) {
	b := "B"
	zamjenik := &models.User{FullName: "Pero Perić", Title: "dipl.ing.građ.",
		Duties: []models.Duty{{Role: models.RoleCopDeputy, SectorID: &b}}}
	h := &PrognozeHandler{}
	p := h.izdavac(zamjenik, "B")
	if p.Funkcija != "zamjenik voditelja Centra obrane od poplava" || p.Ime != "Pero Perić, dipl.ing.građ." {
		t.Errorf("zamjenik izdaje: %+v", p)
	}
	drugi := &models.User{FullName: "Ivo Ivić", Duties: []models.Duty{{Role: models.RoleOperator, SectorID: &b}}}
	if p := h.izdavac(drugi, "B"); p.Funkcija != "voditelj Centra obrane od poplava" || p.Ime == "Ivo Ivić" {
		t.Errorf("izvozi operater: %+v — potpis treba stajati na voditelju centra", p)
	}
}

// Uz tablice izvoz nosi graf svake hrvatske letve (strane nemaju svoj) s
// podacima na skrivenom listu, i godišnje vodostaje s označenim rekordima.
// Pripremna obrana blizu vode ulazi u graf; izvanredna daleko iznad ide u
// naslov, a ne u mjerilo. Brojevi su izmišljeni.
func TestIzvozGrafovaIGodisnjih(t *testing.T) {
	v := func(x float64) *float64 { return &x }
	cm := func(x int) *int { return &x }
	izdanoSat := time.Date(2026, 9, 27, 21, 0, 0, 0, time.UTC).Unix() / 3600
	satno := map[int64]PregledVrijednost{}
	for h := int64(1); h <= 96; h++ {
		satno[izdanoSat+h] = PregledVrijednost{Vrijednost: 300 + float64(h), Dolje: 290 + float64(h), Gore: 310 + float64(h)}
	}
	data := PrognozePageData{Izdano: "27.9.2026. u 23:00", IzdanoSat: izdanoSat,
		Tablice: []TablicaPrognoza{{Naslov: "Dunav", Letve: []LetvaPrognoze{
			{Kod: "bezdan", Naziv: "Bezdan (Srbija)", SadaCmV: v(100)},
			{Kod: "batina", Naziv: "Batina", Racuna: "vodostaj", SadaCmV: v(300)},
		}}},
		pregled: []PregledLetve{{Letva: "batina", Satno: satno}},
		postajeM: map[string]models.Station{"batina": {Code: "batina", Name: "Batina",
			Prep: models.Threshold{Cm: cm(420)}, Emergency: models.Threshold{Cm: cm(900)}}},
	}
	h := &PrognozeHandler{}
	grafovi := h.nizoviGrafova(context.Background(), data)
	if len(grafovi) != 1 || grafovi[0].kod != "batina" {
		t.Fatalf("grafovi %+v: treba samo Batina", grafovi)
	}
	k := &xlsxw.Knjiga{}
	listGrafova(k, ZaglavljeIzvoza{}, data, grafovi)
	listGodisnjih(k, ZaglavljeIzvoza{}, "Dunav", grafovi, map[string][]repository.GodinaVodostaja{"batina": {
		{Godina: 2013, Srednjak: 275, Min: 19, Max: 772, Dana: 365, ImaVrijednost: true},
		{Godina: 2026, Srednjak: -2, Min: -160, Max: 425, Dana: 252, ImaVrijednost: true},
		{Godina: 1956, Srednjak: 400, Min: 100, Max: 797, Dana: 366, ImaVrijednost: true, Preracunata: true},
	}}, godisnjiVodostajiVrsta)
	listGodisnjih(k, ZaglavljeIzvoza{}, "Dunav", grafovi, map[string][]repository.GodinaVodostaja{"batina": {
		{Godina: 2013, Srednjak: 5.34, Min: 1.26, Max: 88.47, Dana: 365, ImaVrijednost: true},
	}}, godisnjiProtociVrsta)
	var b bytes.Buffer
	if err := k.Zapisi(&b); err != nil {
		t.Fatal(err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
	d := map[string]string{}
	for _, f := range zr.File {
		r, _ := f.Open()
		x, _ := io.ReadAll(r)
		r.Close()
		d[f.Name] = string(x)
	}
	graf := d["xl/charts/chart1.xml"]
	for _, want := range []string{"Batina · P 420 · I 900", "<c:v>P 420</c:v>", "Podaci grafova",
		"<c:v>prognoza</c:v>", "<c:v>raspon 70 %</c:v>"} {
		if !strings.Contains(graf, want) {
			t.Errorf("graf nema %q", want)
		}
	}
	if d["xl/charts/chart2.xml"] != "" {
		t.Errorf("srpska letva ne dobiva graf")
	}
	if !strings.Contains(d["xl/workbook.xml"], `name="Podaci grafova" sheetId="2" state="hidden"`) {
		t.Errorf("podaci grafova nisu na skrivenom listu: %s", d["xl/workbook.xml"])
	}
	if strings.Contains(graf, "<c:v>I 900</c:v>") {
		t.Errorf("izvanredna obrana daleko iznad vode ne ulazi u graf, samo u naslov")
	}
	god := d["xl/worksheets/sheet3.xml"]
	// Preračunata 1956. (797) viša je od 2013., ali nije mjerenje: kurziv,
	// a rekord ostaje 772.
	// Karakteristične vrijednosti: srednje (SV · SNV · SVV) samo iz pune 2013.,
	// krajnje (NNV · VVV) iz svih izmjerenih, bez preračunate 1956.
	for _, want := range []string{">GODIŠNJI VODOSTAJI — Dunav<", `s="25"><v>772</v>`, `s="26"><v>-160</v>`, `s="15"><v>-2</v>`,
		`s="16"><v>797</v>`, ">2026<", ">2013<", ">Karakteristične vrijednosti 1991.–2020. (standardno razdoblje)<",
		">Karakteristične vrijednosti cijelog izmjerenog niza<", ">SV · SNV · SVV<", ">NNV · VVV<", `s="28"><v>275</v>`,
		`s="28"><v>19</v>`, ">1 g.<", ">2026.<", "SVV srednji visoki"} {
		if !strings.Contains(god, want) {
			t.Errorf("godišnji list nema %s", want)
		}
	}
	if strings.Index(god, ">2026<") > strings.Index(god, ">2013<") {
		t.Errorf("najnovija godina ide prva")
	}
	if strings.Contains(god, "<v>797</v>") && strings.Count(god, `s="25"><v>797</v>`) > 0 {
		t.Errorf("preračunata 1956. ne smije biti VVV")
	}
	q := d["xl/worksheets/sheet4.xml"]
	for _, want := range []string{">GODIŠNJI PROTOCI — Dunav<", ">Qsr<", ">NQ<", ">VQ<", ">Qsr · SNQ · SVQ<",
		">NNQ · VVQ<", "<v>5.3</v>", "<v>88.5</v>", "VVQ najveći"} {
		if !strings.Contains(q, want) {
			t.Errorf("list protoka nema %s", want)
		}
	}
}

// Zagrada je država samo kad je s popisa država; „(nizvodno)" ostaje u imenu.
func TestImeIDrzava(t *testing.T) {
	for naziv, want := range map[string][2]string{
		"Komárom (Mađarska)":         {"Komárom", "HU"},
		"Bezdan (Srbija)":            {"Bezdan", "RS"},
		"Ustava Zmajevac (nizvodno)": {"Ustava Zmajevac (nizvodno)", ""},
		"Batina":                     {"Batina", ""},
	} {
		if ime, d := imeIDrzava(naziv); ime != want[0] || d != want[1] {
			t.Errorf("%q → %q, %q; želim %q, %q", naziv, ime, d, want[0], want[1])
		}
	}
}
