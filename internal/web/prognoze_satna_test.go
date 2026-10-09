package web

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"
	"gocop/internal/xlsxw"
)

// izmisljeniCiljevi zamijene dnevne ciljeve i srpske letve izmišljenima:
// donja i treca imaju dnevni model, preko prenosi s donje, nasuprot s donje
// i srednje, a srednja dnevnog modela nema.
func izmisljeniCiljevi(t *testing.T) {
	t.Helper()
	ciljevi, prenesene := prognoza.DnevniCiljevi, prognoza.PreneseneLetve
	t.Cleanup(func() { prognoza.DnevniCiljevi, prognoza.PreneseneLetve = ciljevi, prenesene })
	prognoza.DnevniCiljevi = []prognoza.DnevniCilj{{Letva: "donja"}, {Letva: "treca"}}
	prognoza.PreneseneLetve = []struct {
		Letva  string
		Izvori []string
	}{{"preko", []string{"donja"}}, {"nasuprot", []string{"donja", "srednja"}}}
}

// satnaPrvaCetiri je satna prognoza letve u prva četiri dana pregleda.
func satnaPrvaCetiri(ciljevi []int64) PregledLetve {
	l := PregledLetve{Satno: map[int64]PregledVrijednost{}}
	for k, t := range ciljevi[:4] {
		l.Satno[t] = PregledVrijednost{Vrijednost: 100 + float64(k), Dolje: 90, Gore: 110, BoljaOdPostojanosti: true}
	}
	return l
}

// Stranica čita dnevnu prognozu istog izdanja kao satnu: izdanje u kojem
// dnevni model nije računao nema dnevne, a starija se ne podmeće.
func TestDnevnoZaIstoIzdanje(t *testing.T) {
	izdano := int64(497635)
	put := bazaPrognoza(t, izdano)
	db, err := prognoza.Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	if err := prognoza.SpremiDnevne(db, []prognoza.DnevnaIzdana{
		{Letva: "belisce", Izdano: izdano - 1, Dan: 1, Ciljni: izdano + 23, Vrijednost: 80, Model: prognoza.ModelDnevni}}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	c, err := OtvoriPrognoze(put)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if d, err := c.DnevnoZa(time.Unix(izdano*3600, 0)); err != nil || len(d) != 0 {
		t.Errorf("izdanje bez dnevne: %v, %v", d, err)
	}
	if d, err := c.DnevnoZa(time.Unix((izdano-1)*3600, 0)); err != nil || len(d["belisce"]) != 1 {
		t.Errorf("izdanje s dnevnom: %v, %v", d, err)
	}
	var nema *CitacPrognoza
	if d, err := nema.DnevnoZa(time.Now()); d != nil || err != nil {
		t.Errorf("bez baze: %v, %v", d, err)
	}
}

// Izdanje bez dnevne prognoze: od dana od kojeg bi je davao dnevni model
// stoji satna u 07 h s oznakom s, iste brojke kao i dosad; dan do kojeg
// satna ne seže ostaje prazan, s objašnjenjem. Letve koje dnevnu ni inače
// nemaju ne dobivaju oznake.
func TestCelijeDanaBezDnevneStavljaSatnuSOznakom(t *testing.T) {
	izmisljeniCiljevi(t)
	_, ciljevi := daniPregleda(time.Date(2026, 10, 8, 19, 0, 0, 0, time.UTC))
	l := satnaPrvaCetiri(ciljevi)
	for _, letva := range []string{"donja", "preko"} {
		dani := celijeDana(letva, 2, l, ciljevi, nil, nil, models.Station{})
		if d := dani[0]; d.IzSatne || d.BezDnevne || d.Cm != "100" {
			t.Errorf("%s 1. dan %+v: satna bez oznake", letva, d)
		}
		for k := 1; k < 4; k++ {
			if d := dani[k]; !d.IzSatne || d.BezDnevne || d.Dnevna || d.Cm != brojHRf(100+float64(k), 0) {
				t.Errorf("%s %d. dan %+v: satna s oznakom", letva, k+1, d)
			}
		}
		for k := 4; k < len(dani); k++ {
			if d := dani[k]; !d.BezDnevne || d.IzSatne || d.Cm != "" {
				t.Errorf("%s %d. dan %+v: prazan, dnevne nema", letva, k+1, d)
			}
		}
	}
	// satni način: oznake s nema, ali dan bez satne ipak je prazan zbog dnevne
	if d := celijeDana("donja", 0, l, ciljevi, nil, nil, models.Station{}); d[2].IzSatne || !d[5].BezDnevne {
		t.Errorf("satni način: %+v, %+v", d[2], d[5])
	}
	for _, letva := range []string{"nasuprot", "srednja"} {
		for k, d := range celijeDana(letva, 2, l, ciljevi, nil, nil, models.Station{}) {
			if d.IzSatne || d.BezDnevne {
				t.Errorf("%s %d. dan %+v: letva dnevnu ni inače nema", letva, k+1, d)
			}
		}
	}
}

// Kad dnevna postoji, dani su kakvi su bili: dnevna od svog dana, bez oznake s.
func TestCelijeDanaSDnevnomBezOznakeS(t *testing.T) {
	izmisljeniCiljevi(t)
	izdano := time.Date(2026, 10, 8, 19, 0, 0, 0, time.UTC)
	_, ciljevi := daniPregleda(izdano)
	sat := izdano.Unix() / 3600
	var dnevne []prognoza.DnevnaIzdana
	for k := 0; k <= prognoza.DnevniDosezi; k++ {
		dnevne = append(dnevne, prognoza.DnevnaIzdana{Letva: "donja", Izdano: sat, Dan: k, Ciljni: sat + int64(24*k), Vrijednost: 200})
	}
	dani := celijeDana("donja", 2, satnaPrvaCetiri(ciljevi), ciljevi, dnevne, nil, models.Station{})
	for k, d := range dani {
		if d.IzSatne || d.BezDnevne || d.Dnevna != (k >= 1) {
			t.Errorf("%d. dan %+v", k+1, d)
		}
	}
}

func TestCelijaDanaModel(t *testing.T) {
	v := 1.0
	for _, tc := range []struct {
		c    CelijaDana
		want string
	}{
		{CelijaDana{}, ""},
		{CelijaDana{BezDnevne: true}, "nema (dnevni nije izdan)"},
		{CelijaDana{CmV: &v, Dnevna: true}, "dnevni"},
		{CelijaDana{CmV: &v, IzSatne: true}, "satni (dnevni nije izdan)"},
		{CelijaDana{CmV: &v}, "satni"},
	} {
		if got := tc.c.Model(); got != tc.want {
			t.Errorf("%+v: %q, želim %q", tc.c, got, tc.want)
		}
	}
}

// Kartica pokazuje oznaku s i objašnjava prazan dan.
func TestStranicaPrognozaSatnaUmjestoDnevne(t *testing.T) {
	html := iscrtaj(t, "prognoze.html", PrognozePageData{
		CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		ActiveNav: "prognoze", Izdano: "8.10.2026. u 21:00", Udio: 70,
		Tablice: []TablicaPrognoza{{Naslov: "Rijeka", Letve: []LetvaPrognoze{{Kod: "donja", Naziv: "Donja", Racuna: "vodostaj", ImaTermina: true,
			Dani: []CelijaDana{{Naslov: "pet 9.10.", Cm: "101", IzSatne: true}, {Naslov: "uto 13.10.", BezDnevne: true}}}}}},
	})
	for _, want := range []string{"prog-oznaka-satna", "dnevni model za ovo izdanje nije računao", ">s</sup>",
		"Dnevne prognoze za ovo izdanje nema, a satna seže do 96 h"} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica nema %q", want)
		}
	}
}

// Skriveni redak „model” u izvozu kaže da je dan iz satne jer dnevne nema.
func TestIzvozModelBezDnevne(t *testing.T) {
	v := func(x float64) *float64 { return &x }
	data := PrognozePageData{Udio: 70, Izdano: "8.10.2026. u 21:00", Dani: []string{"pet 9.10.", "sub 10.10.", "uto 13.10."}}
	tab := TablicaPrognoza{Naslov: "Rijeka", Letve: []LetvaPrognoze{{Kod: "donja", Naziv: "Donja", Racuna: "vodostaj", SadaCmV: v(100),
		Dani: []CelijaDana{{CmV: v(101)}, {CmV: v(102), IzSatne: true}, {BezDnevne: true}}}}}
	k := &xlsxw.Knjiga{}
	listPrognoze(k, ZaglavljeIzvoza{Potpisnici: []PotpisnikIzvoza{{Funkcija: "voditelj Centra obrane od poplava", Ime: "Ana Anić"}}}, data, tab)
	txt := tekstKnjige(t, k)
	for _, want := range []string{">satni<", "satni (dnevni nije izdan)", "nema (dnevni nije izdan)"} {
		if !strings.Contains(txt, want) {
			t.Errorf("u izvozu nema %q", want)
		}
	}
}

// tekstKnjige je sav XML zapisane knjige, za traženje teksta.
func tekstKnjige(t *testing.T, k *xlsxw.Knjiga) string {
	t.Helper()
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
	return sve.String()
}
