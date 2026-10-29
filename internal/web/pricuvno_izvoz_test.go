package web

import (
	"archive/zip"
	"bytes"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"
	"gocop/internal/xlsxw"
)

// Nazivi letvi kakve vraća registar; šifra bez naziva ostaje šifra, kao u
// IzvoziPricuvno.
func imePricuvnoTest(kod string) string {
	if n, ima := map[string]string{
		"batina": "Batina", "aljmas": "Aljmaš", "osijek": "Osijek", "belisce": "Belišće",
		"bezdan": "Bezdan", "apatin": "Apatin", "bogojevo": "Bogojevo", "mohacs": "Mohács",
	}[kod]; ima {
		return n
	}
	return kod
}

func brojPricuvno(v float64) *float64 { return &v }

// daniPricuvno: četvrtak 29. 10. 2026. i šest dana iza, u 7 h.
func daniPricuvno() []time.Time {
	danas := time.Date(2026, 10, 29, 7, 0, 0, 0, models.Zagreb)
	var d []time.Time
	for i := 0; i <= prognoza.PricuvniDana; i++ {
		d = append(d, danas.AddDate(0, 0, i))
	}
	return d
}

// redUlaza složi dane redom; nil je neupisan dan.
func redUlaza(v ...*float64) [prognoza.PricuvniDana + 1]*float64 {
	var r [prognoza.PricuvniDana + 1]*float64
	copy(r[:], v)
	return r
}

func nizUlaza(od float64, dana int) [prognoza.PricuvniDana + 1]*float64 {
	var r [prognoza.PricuvniDana + 1]*float64
	for d := 0; d < dana; d++ {
		r[d] = brojPricuvno(od + float64(d))
	}
	return r
}

// jednaLetvaPricuvno: samo Batina, iz Bezdana (četiri dana) pa iz Mohácsa.
func jednaLetvaPricuvno() prognoza.PricuvniPodaci {
	return prognoza.PricuvniPodaci{
		Sada: time.Date(2026, 10, 29, 6, 30, 0, 0, time.UTC),
		Dani: daniPricuvno(),
		Veze: map[string][]prognoza.PricuvnaVeza{"batina": {
			{Letva: "batina", Izvori: []string{"bezdan"}, Koef: []float64{1.234, 0.98764}, Rasap: 4.26, Pomak: -3, Dana: 3650, Od: "2016-10-29"},
			{Letva: "batina", Izvori: []string{"mohacs"}, Koef: []float64{-200.123456, 1.05}},
		}},
		Ulazi: map[string][prognoza.PricuvniDana + 1]*float64{
			"bezdan": redUlaza(brojPricuvno(100), brojPricuvno(102), brojPricuvno(104), brojPricuvno(106)),
			"mohacs": nizUlaza(280, 7),
		},
		Nase:   map[string]float64{"batina": 95},
		Izvori: map[string]string{"bezdan": prognoza.PodrijetloHidmet},
	}
}

// viseLetviPricuvno pokriva put iz dviju letvi, put bez pravca, put iz naše
// letve, mađarsku prognozu iste letve i letve bez ijedne veze.
func viseLetviPricuvno() prognoza.PricuvniPodaci {
	p := jednaLetvaPricuvno()
	p.Veze["aljmas"] = []prognoza.PricuvnaVeza{
		{Letva: "aljmas", Izvori: []string{"apatin", "bogojevo"}, Koef: []float64{5, 0.4, 0.55554}, Rasap: 6.04, Pomak: 2, Dana: 3000, Od: "2017-01-01"},
		{Letva: "aljmas", Izvori: []string{"bogojevo"}, Greska: "premalo zajedničkih dana"},
		{Letva: "aljmas", Izvori: []string{"batina"}, Koef: []float64{-10, 1}, Rasap: 3, Dana: 100, Od: "2026-01-01"},
	}
	p.Veze["belisce"] = []prognoza.PricuvnaVeza{{Letva: "belisce", Izvori: []string{prognoza.Madjarska + "belisce"}, Koef: []float64{0, 1}}}
	p.Veze["osijek"] = []prognoza.PricuvnaVeza{
		{Letva: "osijek", Izvori: []string{prognoza.Madjarska + "osijek"}, Koef: []float64{0, 1}},
		{Letva: "osijek", Izvori: []string{"belisce", "aljmas"}, Koef: []float64{1, 0.5, 0.5}, Rasap: 2.5, Dana: 10, Od: "2026-10-19"},
	}
	p.Ulazi["apatin"] = nizUlaza(200, 2)
	p.Ulazi["bogojevo"] = nizUlaza(300, 4)
	p.Ulazi[prognoza.Madjarska+"osijek"] = nizUlaza(80, 3)
	p.Ulazi[prognoza.Madjarska+"botovo"] = nizUlaza(50, 7)
	p.Nase["osijek"] = 78
	p.Izvori[prognoza.Madjarska+"botovo"] = prognoza.Podrijetlo
	return p
}

func listPricuvno(t *testing.T, k *xlsxw.Knjiga, naziv string) *xlsxw.List {
	t.Helper()
	for _, l := range k.Listovi {
		if l.Naziv == naziv {
			return l
		}
	}
	t.Fatalf("nema lista %q", naziv)
	return nil
}

// xmlListaPricuvno vraća zapisani XML lista i knjige: spajanja, visine i
// ispis nisu izvan paketa xlsxw vidljivi drukčije.
func xmlListaPricuvno(t *testing.T, k *xlsxw.Knjiga, list string) string {
	t.Helper()
	var b bytes.Buffer
	if err := k.Zapisi(&b); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range z.File {
		if f.Name != list {
			continue
		}
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		s, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		return string(s)
	}
	t.Fatalf("nema %s u knjizi", list)
	return ""
}

func usporediCelije(t *testing.T, gdje string, dobiveno, ocekivano []xlsxw.Celija) {
	t.Helper()
	if !reflect.DeepEqual(dobiveno, ocekivano) {
		t.Errorf("%s:\n dobiveno %#v\n očekivano %#v", gdje, dobiveno, ocekivano)
	}
}

func praznePricuvno(n int, stil int) []xlsxw.Celija {
	c := make([]xlsxw.Celija, n)
	for i := range c {
		c[i] = xlsxw.T("", stil)
	}
	return c
}

// Bez ičega knjiga i dalje ima sva četiri lista, svaki red ulaza i svaku
// našu letvu, s praznim žutim poljima i „nema ulaza”.
func TestPricuvnoPrazniPodaci(t *testing.T) {
	k := knjigaPricuvno(prognoza.PricuvniPodaci{}, imePricuvnoTest)
	var nazivi []string
	for _, l := range k.Listovi {
		nazivi = append(nazivi, l.Naziv)
	}
	if want := []string{"Naše postaje", "Unos", "Veze", "Kako"}; !reflect.DeepEqual(nazivi, want) {
		t.Fatalf("listovi %v, očekivano %v", nazivi, want)
	}

	unos := listPricuvno(t, k, "Unos")
	if len(unos.Redci) != 4+len(prognoza.PricuvniUlazi) {
		t.Fatalf("Unos ima %d redaka", len(unos.Redci))
	}
	usporediCelije(t, "zaglavlje Unosa bez dana", unos.Redci[3], []xlsxw.Celija{xlsxw.T("Letva", xlsxw.Zaglavlje),
		xlsxw.T("Država", xlsxw.Zaglavlje), xlsxw.T("Upisano iz", xlsxw.Zaglavlje)})
	ocekivano := append([]xlsxw.Celija{xlsxw.T("Bezdan", xlsxw.TablicaPod), xlsxw.T("RS", xlsxw.TablicaSredina),
		xlsxw.T("—", xlsxw.TablicaSivo)}, praznePricuvno(prognoza.PricuvniDana+1, xlsxw.Unos)...)
	usporediCelije(t, "Bezdan bez upisa", unos.Redci[4], ocekivano)

	veze := listPricuvno(t, k, "Veze")
	if len(veze.Redci) != 4 {
		t.Errorf("Veze bez veza imaju %d redaka, očekivano 4", len(veze.Redci))
	}

	nase := listPricuvno(t, k, "Naše postaje")
	if len(nase.Redci) != 4+2*len(prognoza.PricuvniPutovi) {
		t.Fatalf("Naše postaje imaju %d redaka", len(nase.Redci))
	}
	for i, pp := range prognoza.PricuvniPutovi {
		vr, iz := nase.Redci[4+2*i], nase.Redci[5+2*i]
		ocekivanoVr := []xlsxw.Celija{xlsxw.T(imePricuvnoTest(pp.Letva), xlsxw.TablicaPodRub), xlsxw.T("", xlsxw.TablicaSredina), xlsxw.T("cm", xlsxw.TablicaSivo)}
		ocekivanoIz := []xlsxw.Celija{xlsxw.T("iz", xlsxw.TablicaKurziv), xlsxw.T("", xlsxw.Tablica), xlsxw.T("", xlsxw.Tablica)}
		for d := 0; d <= prognoza.PricuvniDana; d++ {
			ocekivanoVr = append(ocekivanoVr, xlsxw.FT(`""`, "", xlsxw.TablicaPod))
			ocekivanoIz = append(ocekivanoIz, xlsxw.FT(`"nema ulaza"`, "nema ulaza", xlsxw.TablicaSivo))
		}
		usporediCelije(t, pp.Letva+" vrijednosti", vr, ocekivanoVr)
		usporediCelije(t, pp.Letva+" iz", iz, ocekivanoIz)
	}

	if kako := listPricuvno(t, k, "Kako"); len(kako.Redci) != 6 {
		t.Errorf("Kako ima %d redaka, očekivano 6", len(kako.Redci))
	}
}

// Zaglavlja dana, upute s vremenom izdanja, širine stupaca i ispis.
func TestPricuvnoIzgledListova(t *testing.T) {
	k := knjigaPricuvno(jednaLetvaPricuvno(), imePricuvnoTest)
	dani := []string{"danas 29.10.", "pet 30.10.", "sub 31.10.", "ned 1.11.", "pon 2.11.", "uto 3.11.", "sri 4.11."}
	var naslovi []xlsxw.Celija
	for _, d := range dani {
		naslovi = append(naslovi, xlsxw.T(d, xlsxw.Zaglavlje))
	}

	unos := listPricuvno(t, k, "Unos")
	usporediCelije(t, "naslov Unosa", unos.Redci[0], []xlsxw.Celija{xlsxw.T("Pričuvni izračun — unos tuđih prognoza", xlsxw.Naslov)})
	if s := unos.Redci[1][0].Tekst; !strings.Contains(s, "popunio onim što je znao 29.10.2026. u 07:30. Nepoznato") {
		t.Errorf("napomena Unosa: %q", s)
	}
	if len(unos.Redci[2]) != 0 {
		t.Errorf("treći redak Unosa nije prazan: %#v", unos.Redci[2])
	}
	usporediCelije(t, "zaglavlje Unosa", unos.Redci[3], append([]xlsxw.Celija{xlsxw.T("Letva", xlsxw.Zaglavlje),
		xlsxw.T("Država", xlsxw.Zaglavlje), xlsxw.T("Upisano iz", xlsxw.Zaglavlje)}, naslovi...))
	if want := []float64{34, 9, 14, 11, 11, 11, 11, 11, 11, 11}; !reflect.DeepEqual(unos.Sirine, want) {
		t.Errorf("širine Unosa %v", unos.Sirine)
	}

	nase := listPricuvno(t, k, "Naše postaje")
	usporediCelije(t, "zaglavlje Naših postaja", nase.Redci[3], append([]xlsxw.Celija{xlsxw.T("Letva", xlsxw.Zaglavlje),
		xlsxw.T("Izmjereno danas", xlsxw.Zaglavlje), xlsxw.T("", xlsxw.Zaglavlje)}, naslovi...))
	if s := nase.Redci[1][0].Tekst; !strings.Contains(s, "(upisani 29.10.2026. u 07:30)") {
		t.Errorf("napomena Naših postaja: %q", s)
	}
	if want := []float64{20, 11, 5, 16, 16, 16, 16, 16, 16, 16}; !reflect.DeepEqual(nase.Sirine, want) || !nase.Vodoravno {
		t.Errorf("Naše postaje: širine %v, vodoravno %v", nase.Sirine, nase.Vodoravno)
	}

	veze := listPricuvno(t, k, "Veze")
	if want := []float64{16, 6, 38, 10, 9, 9, 7, 11, 7, 11, 44}; !reflect.DeepEqual(veze.Sirine, want) {
		t.Errorf("širine Veza %v", veze.Sirine)
	}
	if len(veze.Redci[3]) != 11 || veze.Redci[3][7].Tekst != "Pomak danas" {
		t.Errorf("zaglavlje Veza %#v", veze.Redci[3])
	}

	kako := listPricuvno(t, k, "Kako")
	if !strings.HasPrefix(kako.Redci[1][0].Tekst, "1. Na listu „Unos”") || !strings.Contains(kako.Redci[5][0].Tekst, "upisani 29.10.2026. u 07:30 iz") {
		t.Errorf("upute: %q … %q", kako.Redci[1][0].Tekst, kako.Redci[5][0].Tekst)
	}
	if want := []float64{12, 12, 12, 12, 12, 12, 12, 12, 12}; !reflect.DeepEqual(kako.Sirine, want) {
		t.Errorf("širine Kako %v", kako.Sirine)
	}

	for list, dijelovi := range map[string][]string{
		"xl/worksheets/sheet1.xml": {`<mergeCell ref="A2:J2"/>`, `<row r="2" ht="42.0" customHeight="1">`, `orientation="landscape"`},
		"xl/worksheets/sheet2.xml": {`<mergeCell ref="A2:J2"/>`, `<row r="2" ht="30.0" customHeight="1">`},
		"xl/worksheets/sheet3.xml": {`<mergeCell ref="A2:K2"/>`, `<row r="2" ht="42.0" customHeight="1">`},
		"xl/worksheets/sheet4.xml": {`<mergeCells count="5">`, `<mergeCell ref="A2:I2"/>`, `<mergeCell ref="A6:I6"/>`, `<row r="6" ht="44.0" customHeight="1">`},
		"xl/workbook.xml":          {`'Naše postaje'!$4:$4`},
	} {
		x := xmlListaPricuvno(t, k, list)
		for _, d := range dijelovi {
			if !strings.Contains(x, d) {
				t.Errorf("%s nema %s", list, d)
			}
		}
	}
}

// Jedna letva: dva puta, formule redom prednosti i nastavak drugog puta na
// jučerašnju vrijednost.
func TestPricuvnoJednaLetva(t *testing.T) {
	p := jednaLetvaPricuvno()
	k := knjigaPricuvno(p, imePricuvnoTest)

	unos := listPricuvno(t, k, "Unos")
	usporediCelije(t, "Bezdan", unos.Redci[4], []xlsxw.Celija{xlsxw.T("Bezdan", xlsxw.TablicaPod), xlsxw.T("RS", xlsxw.TablicaSredina),
		xlsxw.T(prognoza.PodrijetloHidmet, xlsxw.TablicaSivo), xlsxw.N(100, xlsxw.Unos), xlsxw.N(102, xlsxw.Unos), xlsxw.N(104, xlsxw.Unos),
		xlsxw.N(106, xlsxw.Unos), xlsxw.T("", xlsxw.Unos), xlsxw.T("", xlsxw.Unos), xlsxw.T("", xlsxw.Unos)})
	if c := unos.Redci[8]; c[0].Tekst != "Mohács" || c[1].Tekst != "HU" || c[2].Tekst != "—" || c[9] != xlsxw.N(286, xlsxw.Unos) {
		t.Errorf("Mohács %#v", c)
	}

	veze := listPricuvno(t, k, "Veze")
	if len(veze.Redci) != 6 {
		t.Fatalf("Veze imaju %d redaka", len(veze.Redci))
	}
	usporediCelije(t, "prvi put Batine", veze.Redci[4], []xlsxw.Celija{xlsxw.T("Batina", xlsxw.TablicaPod), xlsxw.N(1, xlsxw.TablicaSredina),
		xlsxw.T("Bezdan", xlsxw.Tablica), xlsxw.N(1.23, xlsxw.TablicaBroj), xlsxw.N(0.9876, xlsxw.TablicaBroj), xlsxw.T("", xlsxw.Tablica),
		xlsxw.N(4.3, xlsxw.TablicaSredina), xlsxw.N(-3, xlsxw.Unos), xlsxw.N(3650, xlsxw.TablicaSredina), xlsxw.T("2016-10-29", xlsxw.TablicaSredina),
		xlsxw.T("", xlsxw.TablicaSivo)})
	usporediCelije(t, "drugi put Batine", veze.Redci[5], []xlsxw.Celija{xlsxw.T("Batina", xlsxw.TablicaPod), xlsxw.N(2, xlsxw.TablicaSredina),
		xlsxw.T("Mohács", xlsxw.Tablica), xlsxw.N(-200.12, xlsxw.TablicaBroj), xlsxw.N(1.05, xlsxw.TablicaBroj), xlsxw.T("", xlsxw.Tablica),
		xlsxw.T("", xlsxw.TablicaSredina), xlsxw.N(0, xlsxw.Unos), xlsxw.T("", xlsxw.TablicaSredina), xlsxw.T("", xlsxw.TablicaSredina),
		xlsxw.T("", xlsxw.TablicaSivo)})

	nase := listPricuvno(t, k, "Naše postaje")
	vr, iz := nase.Redci[4], nase.Redci[5]
	if vr[0].Tekst != "Batina" || vr[0].Stil != xlsxw.TablicaPodRub || vr[1] != xlsxw.N(95, xlsxw.TablicaSredina) {
		t.Errorf("početak retka Batine %#v", vr[:3])
	}
	bezdan := func(s string) string { return "Veze!$D$5+Veze!$H$5+Veze!$E$5*Unos!" + s + "5" }
	mohacs := func(s string) string { return "Veze!$D$6+Veze!$H$6+Veze!$E$6*Unos!" + s + "9" }
	danas := "IF(ISNUMBER(Unos!D5),ROUND(" + bezdan("D") + ",0),IF(ISNUMBER(Unos!D9),ROUND(" + mohacs("D") + ",0),\"\"))"
	if vr[3] != xlsxw.F(danas, 97, xlsxw.TablicaPod) {
		t.Errorf("danas:\n %#v\n %s", vr[3], danas)
	}
	sutra := "IF(ISNUMBER(Unos!E5),ROUND(" + bezdan("E") + ",0),IF(ISNUMBER(Unos!E9),IF(AND(ISNUMBER(D5),ISNUMBER(Unos!D9)),ROUND(D5+(" +
		mohacs("E") + ")-(" + mohacs("D") + "),0),ROUND(" + mohacs("E") + ",0)),\"\"))"
	if vr[4].Formula != sutra {
		t.Errorf("sutra:\n %s\n %s", vr[4].Formula, sutra)
	}
	opis := "IF(ISNUMBER(Unos!E5),\"Bezdan ±4\",IF(ISNUMBER(Unos!E9),\"Mohács\",\"nema ulaza\"))"
	if iz[4] != xlsxw.FT(opis, "Bezdan ±4", xlsxw.TablicaSivo) {
		t.Errorf("iz sutra %#v", iz[4])
	}
	// Peti dan nema Bezdana: Mohács nastavlja na četvrti dan svojom promjenom.
	vrijednosti, _ := prognoza.PricuvnoRacunaj(p)
	for d := 0; d <= prognoza.PricuvniDana; d++ {
		if vr[3+d].Broj != *vrijednosti["batina"][d] || !vr[3+d].JeBroj {
			t.Errorf("dan %d: %v, očekivano %v", d, vr[3+d].Broj, *vrijednosti["batina"][d])
		}
	}
	if vr[7].Broj != 104 || iz[7].Tekst != "Mohács" || iz[6].Tekst != "Bezdan ±4" {
		t.Errorf("prijelaz na Mohács: %v %q %q", vr[7].Broj, iz[6].Tekst, iz[7].Tekst)
	}
}

// Više letvi: put iz dviju letvi (AND), put bez pravca, ulaz koji je naša
// letva (adresa na istom listu), mađarska prognoza iste letve i letve bez veza.
func TestPricuvnoViseLetvi(t *testing.T) {
	p := viseLetviPricuvno()
	k := knjigaPricuvno(p, imePricuvnoTest)

	unos := listPricuvno(t, k, "Unos")
	if c := unos.Redci[12]; c[0].Tekst != "botovo — mađarska prognoza" || c[1].Tekst != "HU → RH" || c[2].Tekst != prognoza.Podrijetlo {
		t.Errorf("mađarska prognoza Botova %#v", c[:3])
	}
	if c := unos.Redci[16]; c[0].Tekst != "Osijek — mađarska prognoza" || c[3] != xlsxw.N(80, xlsxw.Unos) || c[6] != xlsxw.T("", xlsxw.Unos) {
		t.Errorf("mađarska prognoza Osijeka %#v", c)
	}

	veze := listPricuvno(t, k, "Veze")
	if len(veze.Redci) != 12 {
		t.Fatalf("Veze imaju %d redaka", len(veze.Redci))
	}
	usporediCelije(t, "Aljmaš iz dviju letvi", veze.Redci[6], []xlsxw.Celija{xlsxw.T("Aljmaš", xlsxw.TablicaPod), xlsxw.N(1, xlsxw.TablicaSredina),
		xlsxw.T("Apatin + Bogojevo", xlsxw.Tablica), xlsxw.N(5, xlsxw.TablicaBroj), xlsxw.N(0.4, xlsxw.TablicaBroj), xlsxw.N(0.5555, xlsxw.TablicaBroj),
		xlsxw.N(6, xlsxw.TablicaSredina), xlsxw.N(2, xlsxw.Unos), xlsxw.N(3000, xlsxw.TablicaSredina), xlsxw.T("2017-01-01", xlsxw.TablicaSredina),
		xlsxw.T("", xlsxw.TablicaSivo)})
	usporediCelije(t, "Aljmaš bez pravca", veze.Redci[7], append([]xlsxw.Celija{xlsxw.T("Aljmaš", xlsxw.TablicaPod), xlsxw.N(2, xlsxw.TablicaSredina),
		xlsxw.T("Bogojevo", xlsxw.Tablica)}, append(praznePricuvno(7, xlsxw.Tablica), xlsxw.T("nema pravca: premalo zajedničkih dana", xlsxw.TablicaSivo))...))
	if c := veze.Redci[10]; c[0].Tekst != "Osijek" || c[2].Tekst != "Osijek — mađarska prognoza" || c[10].Tekst != "mađarska prognoza iste letve, kakva je objavljena" {
		t.Errorf("Osijek iz mađarske prognoze %#v", c)
	}
	if c := veze.Redci[9]; c[0].Tekst != "Belišće" || c[2].Tekst != "Belišće — mađarska prognoza" {
		t.Errorf("Belišće %#v", c[:3])
	}

	nase := listPricuvno(t, k, "Naše postaje")
	aljmas, aljmasIz := nase.Redci[6], nase.Redci[7]
	dvije := "Veze!$D$7+Veze!$H$7+Veze!$E$7*Unos!D6+Veze!$F$7*Unos!D7"
	danas := "IF(AND(ISNUMBER(Unos!D6),ISNUMBER(Unos!D7)),ROUND(" + dvije + ",0),IF(ISNUMBER(D5),ROUND(Veze!$D$9+Veze!$H$9+Veze!$E$9*D5,0),\"\"))"
	if aljmas[3] != xlsxw.F(danas, 254, xlsxw.TablicaPod) || aljmas[1] != xlsxw.T("", xlsxw.TablicaSredina) {
		t.Errorf("Aljmaš danas:\n %#v\n %s", aljmas[3], danas)
	}
	tekst := "IF(AND(ISNUMBER(Unos!D6),ISNUMBER(Unos!D7)),\"Apatin i Bogojevo ±6\",IF(ISNUMBER(D5),\"Batina ±3\",\"nema ulaza\"))"
	if aljmasIz[3] != xlsxw.FT(tekst, "Apatin i Bogojevo ±6", xlsxw.TablicaSivo) {
		t.Errorf("Aljmaš iz danas %#v", aljmasIz[3])
	}
	// Treći dan nema Apatina; opis je s trećeg puta iz podataka, iako je u
	// formulama to drugi put (put bez pravca se preskače).
	if aljmasIz[5].Tekst != "Batina ±3" || !strings.Contains(aljmas[5].Formula, "IF(AND(ISNUMBER(E7),ISNUMBER(E5)),ROUND(E7+(Veze!$D$9+Veze!$H$9+Veze!$E$9*F5)-(") {
		t.Errorf("Aljmaš treći dan %#v %#v", aljmas[5], aljmasIz[5])
	}

	osijek := nase.Redci[22]
	if osijek[1] != xlsxw.N(78, xlsxw.TablicaSredina) {
		t.Errorf("Osijek izmjereno %#v", osijek[1])
	}
	if !strings.Contains(osijek[3].Formula, "AND(ISNUMBER(D21),ISNUMBER(D7))") || !strings.HasPrefix(osijek[3].Formula, "IF(ISNUMBER(Unos!D17),") {
		t.Errorf("Osijek danas %s", osijek[3].Formula)
	}
	if nase.Redci[23][6].Tekst != "nema ulaza" || osijek[6] != xlsxw.FT(osijek[6].Formula, "", xlsxw.TablicaPod) {
		t.Errorf("Osijek četvrti dan bez ulaza %#v", osijek[6])
	}
	if b := nase.Redci[20][3]; !strings.HasPrefix(b.Formula, "IF(ISNUMBER(Unos!D16),") || b.JeBroj {
		t.Errorf("Belišće bez upisane mađarske prognoze %#v", b)
	}
	if d := nase.Redci[8]; d[0].Tekst != "dalj" || d[3] != xlsxw.FT(`""`, "", xlsxw.TablicaPod) {
		t.Errorf("Dalj bez veza %#v", d)
	}
}

// PricuvniExcel je isto što i knjiga za izvoz, i knjiga se zapisuje.
func TestPricuvniExcelIstiKaoIzvoz(t *testing.T) {
	p := viseLetviPricuvno()
	k := PricuvniExcel(p, imePricuvnoTest)
	if !reflect.DeepEqual(k, knjigaPricuvno(p, imePricuvnoTest)) {
		t.Fatal("PricuvniExcel se razlikuje od knjige izvoza")
	}
	if x := xmlListaPricuvno(t, k, "xl/worksheets/sheet1.xml"); !strings.Contains(x, "Apatin i Bogojevo ±6") {
		t.Error("zapisana knjiga nema opisa puta")
	}
}
