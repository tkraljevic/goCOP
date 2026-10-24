package prognoza

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// dan je 07 h zadanog dana po našem vremenu.
func dan(g int, m time.Month, d int) time.Time { return time.Date(g, m, d, 7, 0, 0, 0, models.Zagreb) }

// Datumi idu dan za danom po kalendaru, i preko pomaka sata (25 i 23 sata)
// i preko nove godine; ponovljen, preskočen ili obrnut dan prekida niz.
func TestUrednihDana(t *testing.T) {
	for _, tc := range []struct {
		ime    string
		datumi []time.Time
		zelim  int
	}{
		{"prazno", nil, 0},
		{"jesenski pomak sata", []time.Time{dan(2026, 10, 24), dan(2026, 10, 25), dan(2026, 10, 26), dan(2026, 10, 27), dan(2026, 10, 28)}, 5},
		{"proljetni pomak sata", []time.Time{dan(2027, 3, 27), dan(2027, 3, 28), dan(2027, 3, 29)}, 3},
		{"nova godina", []time.Time{dan(2026, 12, 30), dan(2026, 12, 31), dan(2027, 1, 1), dan(2027, 1, 2)}, 4},
		{"RHMZ 24. 10. 2026.", []time.Time{dan(2026, 10, 24), dan(2026, 10, 25), dan(2026, 10, 25), dan(2026, 10, 26), dan(2026, 10, 27)}, 2},
		{"ponovljen prvi", []time.Time{dan(2026, 10, 25), dan(2026, 10, 25), dan(2026, 10, 26)}, 1},
		{"preskočen", []time.Time{dan(2026, 10, 24), dan(2026, 10, 26)}, 1},
		{"obrnut", []time.Time{dan(2026, 10, 25), dan(2026, 10, 24)}, 1},
	} {
		if got := urednihDana(tc.datumi); got != tc.zelim {
			t.Errorf("%s: %d, želim %d", tc.ime, got, tc.zelim)
		}
	}
}

// RHMZ je 24. 10. 2026. objavio zaglavlje 24., 25., 25., 26. i 27. 10.:
// ponoć 24. 10. plus n × 24 h preko pomaka sata. To je prognoza za 25.–28.
// 10., pa se čita redom, uz napomenu; isto dan kasnije, kad prvi dan
// prognoze ponavlja dan mjerenja.
func TestCitajHidmetPomakSata(t *testing.T) {
	sada := time.Date(2026, 10, 24, 13, 0, 0, 0, models.Zagreb)
	for _, tc := range []struct {
		datumi []string
		sada   time.Time
		prvi   time.Time
		opis   string
	}{
		{[]string{"24.10.", "25.10.", "25.10.", "26.10.", "27.10."}, sada, dan(2026, 10, 25),
			"datumi u tablici pogrešni su zbog pomaka sata (24. 10., 25. 10., 25. 10., 26. 10., 27. 10.); njihova prognoza stoji redom za dane od 25. 10. do 28. 10."},
		{[]string{"25.10.", "25.10.", "26.10.", "27.10.", "28.10."}, sada.AddDate(0, 0, 1), dan(2026, 10, 26),
			"datumi u tablici pogrešni su zbog pomaka sata (25. 10., 25. 10., 26. 10., 27. 10., 28. 10.); njihova prognoza stoji redom za dane od 26. 10. do 29. 10."},
		{[]string{"22.10.", "23.10.", "24.10.", "25.10.", "25.10."}, sada.AddDate(0, 0, -2), dan(2026, 10, 23), ""},
	} {
		l, err := CitajHidmet(tablicaHidmeta(tc.datumi...), tc.sada)
		var nd *NeuredniDatumi
		if !errors.As(err, &nd) || !nd.PoRedu || nd.Urednih != 5 || len(nd.Datumi) != 5 {
			t.Fatalf("%v: greška %v (%+v), želim pomak sata", tc.datumi, err, nd)
		}
		if len(l) != 1 || !l[0].ImaDanas || l[0].Danas != 100 || len(l[0].Dani) != 4 {
			t.Fatalf("%v: letve %+v", tc.datumi, l)
		}
		for i, d := range l[0].Dani {
			if zelim := tc.prvi.AddDate(0, 0, i); d.Cm != 101+i || !d.Kad.Equal(zelim) {
				t.Errorf("%v: dan %d %+v, želim %d u %v", tc.datumi, i, d, 101+i, zelim.UTC())
			}
		}
		if tc.opis != "" && nd.Error() != tc.opis {
			t.Errorf("greška\n %q\nželim\n %q", nd.Error(), tc.opis)
		}
		if u := nd.upozorenje(); u.Tezina != TezinaNapomena {
			t.Errorf("%v: težina %d, želim napomenu", tc.datumi, u.Tezina)
		}
	}
}

// Ponovljen dan koji pomak sata ne objašnjava: ostaju jutarnje mjerenje i
// dani do prvog neurednog, a ostali stupci se ne spremaju ni premještaju.
func TestCitajHidmetPonovljenDatum(t *testing.T) {
	sada := time.Date(2026, 10, 10, 13, 0, 0, 0, models.Zagreb)
	l, err := CitajHidmet(tablicaHidmeta("10.10.", "11.10.", "11.10.", "12.10.", "13.10."), sada)
	var nd *NeuredniDatumi
	if !errors.As(err, &nd) || nd.PoRedu {
		t.Fatalf("greška %v, želim neuredne datume", err)
	}
	if len(l) != 1 || !l[0].ImaDanas || l[0].Danas != 100 || len(l[0].Dani) != 1 {
		t.Fatalf("letve %+v", l)
	}
	if d := l[0].Dani[0]; d.Cm != 101 || !d.Kad.Equal(dan(2026, 10, 11)) {
		t.Errorf("prvi dan %+v", d)
	}
	if nd.Izvor != PodrijetloHidmet || nd.Urednih != 2 || len(nd.Datumi) != 5 || !nd.PrviJeMjerenje ||
		!nd.Izdano.Equal(time.Date(2026, 10, 10, 12, 0, 0, 0, models.Zagreb)) {
		t.Errorf("neuredno %+v", nd)
	}
	if got, zelim := nd.Error(), "datumi u tablici ne idu dan za danom (10. 10., 11. 10., 11. 10., 12. 10., 13. 10.); "+
		"uz našu prognozu njihova stoji samo do 11. 10., jer se za ostale stupce ne zna kojem danu pripadaju"; got != zelim {
		t.Errorf("greška\n %q\nželim\n %q", got, zelim)
	}
	if u := nd.upozorenje(); u.Tezina != TezinaUpozorenje {
		t.Errorf("težina %d, želim upozorenje", u.Tezina)
	}

	// Prvi dan prognoze ponavlja dan mjerenja: ostaje samo mjerenje.
	l, err = CitajHidmet(tablicaHidmeta("10.10.", "10.10.", "11.10.", "12.10.", "13.10."), sada)
	if !errors.As(err, &nd) || nd.PoRedu || len(l) != 1 || !l[0].ImaDanas || len(l[0].Dani) != 0 {
		t.Fatalf("ponovljen prvi: %+v, %v", l, err)
	}
	if !strings.Contains(nd.Error(), "uz našu prognozu ne stoji nijedan dan njihove") {
		t.Errorf("ponovljen prvi: %q", nd.Error())
	}

	// Preskočen dan preko pomaka sata nije greška pomaka sata.
	l, err = CitajHidmet(tablicaHidmeta("24.10.", "25.10.", "27.10.", "28.10.", "29.10."), time.Date(2026, 10, 24, 13, 0, 0, 0, models.Zagreb))
	if !errors.As(err, &nd) || nd.PoRedu || nd.Urednih != 2 || len(l[0].Dani) != 1 {
		t.Errorf("preskočen: %+v, %v", l, err)
	}
}

// Uredna tablica preko pomaka sata nije neuredna, iako od 24. do 25. 10.
// prođe 25 sati.
func TestCitajHidmetPrekoPomakaSata(t *testing.T) {
	l, err := CitajHidmet(tablicaHidmeta("24.10.", "25.10.", "26.10.", "27.10.", "28.10."),
		time.Date(2026, 10, 24, 13, 0, 0, 0, models.Zagreb))
	if err != nil || len(l) != 1 || len(l[0].Dani) != 4 {
		t.Fatalf("%+v, %v", l, err)
	}
	if d := l[0].Dani[3]; d.Cm != 104 || !d.Kad.Equal(time.Date(2026, 10, 28, 6, 0, 0, 0, time.UTC)) {
		t.Errorf("zadnji dan %+v", d)
	}
}

// Mađarska tablica s ponovljenim danom: ostaje prvi dan prognoze.
func TestCitajPonovljenDatum(t *testing.T) {
	l, err := Citaj(`<tr><td>25.10. 07h</td><td>25.10. 07h</td><td>26.10. 07h</td></tr>
<tr><td>Drava Osijek (24.10.2026 08:59)</td><td>-151</td><td>-160</td><td>-162</td><td>-165</td></tr>`)
	var nd *NeuredniDatumi
	if !errors.As(err, &nd) || len(l) != 1 || l[0].Danas != -151 || len(l[0].Dani) != 1 || l[0].Dani[0].Cm != -160 {
		t.Fatalf("letve %+v, %v", l, err)
	}
	if nd.Izvor != Podrijetlo || nd.Urednih != 1 || nd.PrviJeMjerenje || nd.Izdano != l[0].Izdano ||
		!strings.HasSuffix(nd.Error(), "uz našu prognozu njihova stoji samo do 25. 10., jer se za ostale stupce ne zna kojem danu pripadaju") {
		t.Errorf("neuredno %+v: %q", nd, nd.Error())
	}
}

// Neuredne tablice pamte se u bazi: upozorenje stoji dok je izdanje mlađe
// od TudeUnatragSati, uredno čitanje istog izvora ga briše, a mađarske
// tablice pamte se svaka za sebe.
func TestZapamtiNeuredne(t *testing.T) {
	db := otvoriProbnu(t, "p.db")
	izdano := time.Date(2026, 10, 24, 12, 0, 0, 0, models.Zagreb)
	sat := izdano.Unix() / 3600
	nd := &NeuredniDatumi{Izvor: PodrijetloHidmet, Izdano: izdano, Urednih: 2, PrviJeMjerenje: true,
		Datumi: []time.Time{dan(2026, 10, 24), dan(2026, 10, 25), dan(2026, 10, 27), dan(2026, 10, 28), dan(2026, 10, 29)}}
	if err := ZapamtiNeuredne(db, PodrijetloHidmet, fmt.Errorf("omotano: %w", nd)); err != nil {
		t.Fatal(err)
	}
	zelim := []Upozorenje{{Vrsta: VrstaTudaDatumi, Tezina: TezinaUpozorenje,
		Opis: "srpska prognoza (hidmet.gov.rs) od 24. 10.: datumi u tablici ne idu dan za danom (24. 10., 25. 10., 27. 10., 28. 10., 29. 10.); " +
			"uz našu prognozu njihova stoji samo do 25. 10., jer se za ostale stupce ne zna kojem danu pripadaju"}}
	if got := upozorenjaTudih(db, sat+1); !reflect.DeepEqual(got, zelim) {
		t.Errorf("upozorenja\n %+v\nželim\n %+v", got, zelim)
	}
	if got := upozorenjaTudih(db, sat+TudeUnatragSati); len(got) != 1 {
		t.Errorf("do %d h: %v", TudeUnatragSati, got)
	}
	if got := upozorenjaTudih(db, sat+TudeUnatragSati+1); got != nil {
		t.Errorf("staro izdanje: %v", got)
	}

	dunav := &NeuredniDatumi{Izvor: Podrijetlo, Izdano: izdano, Urednih: 1, Tablica: "dunavska tablica",
		Datumi: []time.Time{dan(2026, 10, 25), dan(2026, 10, 25)}}
	drava := &NeuredniDatumi{Izvor: Podrijetlo, Izdano: izdano, Urednih: 2, Tablica: "dravska tablica",
		Datumi: []time.Time{dan(2026, 10, 25), dan(2026, 10, 26), dan(2026, 10, 26)}}
	if err := ZapamtiNeuredne(db, Podrijetlo, errors.Join(dunav, drava)); err != nil {
		t.Fatal(err)
	}
	got := upozorenjaTudih(db, sat)
	if len(got) != 3 || !strings.HasPrefix(got[1].Opis, "mađarska prognoza (hydroinfo.hu), dravska tablica, od 24. 10.: ") ||
		!strings.HasSuffix(got[1].Opis, "stoji samo do 26. 10., jer se za ostale stupce ne zna kojem danu pripadaju") ||
		!strings.HasPrefix(got[2].Opis, "mađarska prognoza (hydroinfo.hu), dunavska tablica, od 24. 10.: ") {
		t.Errorf("mađarske tablice: %+v", got)
	}
	if err := ZapamtiNeuredne(db, Podrijetlo, nil); err != nil {
		t.Fatal(err)
	}
	if got := upozorenjaTudih(db, sat); len(got) != 1 || !strings.HasPrefix(got[0].Opis, "srpska") {
		t.Errorf("nakon urednog mađarskog: %+v", got)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ZapamtiNeuredne(db, PodrijetloHidmet, nil); err == nil {
		t.Error("zatvorena baza: bez greške")
	}
	if got := upozorenjaTudih(db, sat); len(got) != 1 || got[0].Tezina != TezinaNapomena || !strings.Contains(got[0].Opis, "nije pročitan") {
		t.Errorf("zatvorena baza: %+v", got)
	}
}

// NeuredneIz nađe neuredne datume u grešci čitača, i omotane i spojene.
func TestNeuredneIz(t *testing.T) {
	a, b := &NeuredniDatumi{Tablica: "a"}, &NeuredniDatumi{Tablica: "b"}
	for _, tc := range []struct {
		err   error
		zelim []*NeuredniDatumi
	}{
		{nil, nil},
		{errors.New("ne odgovara"), nil},
		{a, []*NeuredniDatumi{a}},
		{fmt.Errorf("x: %w", a), []*NeuredniDatumi{a}},
		{errors.Join(a, b), []*NeuredniDatumi{a, b}},
	} {
		if got := NeuredneIz(tc.err); !reflect.DeepEqual(got, tc.zelim) {
			t.Errorf("%v: %v", tc.err, got)
		}
	}
}

// Ćelija „-” je dan bez vrijednosti: granice obrane iza prognoze ne
// smiju ući među dane. Postaja s jednim brojem se preskače, a uz neuredno
// zaglavlje postaja ostaje i s jednim spremljenim stupcem.
func TestCitajHidmetCrtica(t *testing.T) {
	zaglavlje := `<table><tr><td>24.10.</td><td>25.10.</td><td>26.10.</td><td>27.10.</td><td>28.10.</td></tr>`
	red := func(naziv string, celije ...string) string {
		return `<tr> <td>&nbsp;VELIKA MORAVA&nbsp;</td> <td><a href="x">` + naziv + `</a></td><td>` + strings.Join(celije, `</td><td>`) + `</td></tr>`
	}
	l, err := CitajHidmet(zaglavlje+red("VARVARIN", "-212", "-211", "-210", "-", "-", "400", "500")+
		red("JASIKA", "-147", "-", "-", "-", "-", "350", "450")+`</table>`, time.Date(2026, 10, 24, 13, 0, 0, 0, models.Zagreb))
	if err != nil || len(l) != 1 || l[0].Naziv != "VARVARIN" || l[0].Danas != -212 || len(l[0].Dani) != 2 ||
		l[0].Dani[1].Cm != -210 || !l[0].Dani[1].Kad.Equal(dan(2026, 10, 26)) {
		t.Fatalf("letve %+v, %v", l, err)
	}

	neuredno := `<table><tr><td>10.10.</td><td>10.10.</td><td>11.10.</td></tr>` + red("BEZDAN", "1", "2", "3") + `</table>`
	l, err = CitajHidmet(neuredno, time.Date(2026, 10, 10, 13, 0, 0, 0, models.Zagreb))
	var nd *NeuredniDatumi
	if !errors.As(err, &nd) || len(l) != 1 || !l[0].ImaDanas || l[0].Danas != 1 || len(l[0].Dani) != 0 {
		t.Errorf("neuredno: %+v, %v", l, err)
	}
}

// Mađarsko zaglavlje na prijelazu godine: prosinac viđen u siječnju je
// prošle godine, siječanj viđen u prosincu iduće.
func TestDanoviZaglavljaPrijelazGodine(t *testing.T) {
	d := danoviZaglavlja("31.12. 07h 01.01. 07h", time.Date(2027, 1, 1, 9, 0, 0, 0, models.Zagreb))
	if len(d) != 2 || d[0].Year() != 2026 || d[1].Year() != 2027 || urednihDana(d) != 2 {
		t.Errorf("u siječnju: %v", d)
	}
	d = danoviZaglavlja("31.12. 07h 01.01. 07h", time.Date(2026, 12, 30, 9, 0, 0, 0, models.Zagreb))
	if len(d) != 2 || d[0].Year() != 2026 || d[1].Year() != 2027 {
		t.Errorf("u prosincu: %v", d)
	}
}

// Novo čitanje istog izdanja zamijeni ono što je starija inačica krivo
// spremila: pomaknute vrijednosti se ispravljaju, a sati kojih u novom
// čitanju nema brišu se.
func TestSpremiTudeIspravljaStareRetke(t *testing.T) {
	db := otvoriProbnu(t, "p.db")
	upisi := func(izdano time.Time, dani ...int) {
		t.Helper()
		for i, d := range dani {
			if _, err := db.Exec(`INSERT INTO tude (izvor, letva, izdano, ciljni, vrijednost) VALUES (?,?,?,?,?)`,
				PodrijetloHidmet, "bezdan", izdano.UTC().Unix()/3600, dan(2026, 10, d).Unix()/3600, 100+[]int{0, 1, 3, 4}[i]); err != nil {
				t.Fatal(err)
			}
		}
	}
	// stara inačica: drugi isti dan preskočen, zadnja dva stupca dan prerano
	upisi(time.Date(2026, 10, 24, 12, 0, 0, 0, models.Zagreb), 24, 25, 26, 27)
	upisi(time.Date(2026, 10, 10, 12, 0, 0, 0, models.Zagreb), 10, 11, 12, 13)

	for _, tc := range []struct {
		datumi []string
		sada   time.Time
		zelim  int
	}{
		{[]string{"24.10.", "25.10.", "25.10.", "26.10.", "27.10."}, time.Date(2026, 10, 24, 13, 0, 0, 0, models.Zagreb), 3},
		{[]string{"10.10.", "11.10.", "11.10.", "12.10.", "13.10."}, time.Date(2026, 10, 10, 13, 0, 0, 0, models.Zagreb), 0},
	} {
		l, _ := CitajHidmet(tablicaHidmeta(tc.datumi...), tc.sada) // neuredno je ovdje očekivano
		if n, err := SpremiTude(db, PodrijetloHidmet, l, SifraSrpske); err != nil || n != tc.zelim {
			t.Errorf("%v: novih %d, %v; želim %d", tc.datumi, n, err, tc.zelim)
		}
	}
	r, err := db.Query(`SELECT ciljni, vrijednost FROM tude WHERE letva = 'bezdan' ORDER BY ciljni`)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var got [][2]int64
	for r.Next() {
		var c, v int64
		if err := r.Scan(&c, &v); err != nil {
			t.Fatal(err)
		}
		got = append(got, [2]int64{c, v})
	}
	zelim := [][2]int64{{dan(2026, 10, 10).Unix() / 3600, 100}, {dan(2026, 10, 11).Unix() / 3600, 101}}
	for i := range 5 {
		zelim = append(zelim, [2]int64{dan(2026, 10, 24+i).Unix() / 3600, int64(100 + i)})
	}
	if !reflect.DeepEqual(got, zelim) {
		t.Errorf("tuđe vrijednosti %v, želim %v", got, zelim)
	}
}

// Upozorenja tuđih prognoza ulaze u upozorenja izdanja.
func TestUpozoriNeuredneTude(t *testing.T) {
	u := Upozorenje{Vrsta: VrstaTudaDatumi, Tezina: TezinaUpozorenje, Opis: "srpska prognoza"}
	got := Upozori(&Ishod{NeuredneTude: []Upozorenje{u}}, nil, nil, nil)
	if !slices.ContainsFunc(got, func(x Upozorenje) bool { return reflect.DeepEqual(x, u) }) {
		t.Errorf("upozorenja %+v", got)
	}
}

// Izdanje postaje kojem sati ne idu naprijed ne zapisuje se: drugi isti sat
// propao bi bez traga. Zaglavlje RHMZ-a od 24. 10. 2026. kroz čitač daje
// jutarnje mjerenje i četiri dana redom, a neuredno koje pomak sata ne
// objašnjava samo uredni početak.
func TestSpremiTudeSatiNaprijed(t *testing.T) {
	db := otvoriProbnu(t, "p.db")
	izd := time.Date(2026, 10, 24, 12, 0, 0, 0, models.Zagreb)
	for _, l := range []Letva{
		{Naziv: "BEZDAN", Izdano: izd, Dani: []Dan{{Kad: dan(2026, 10, 25), Cm: 1}, {Kad: dan(2026, 10, 25), Cm: 2}}},
		{Naziv: "BEZDAN", Izdano: izd, Danas: 1, ImaDanas: true, Dani: []Dan{{Kad: dan(2026, 10, 24), Cm: 2}}},
	} {
		n, err := SpremiTude(db, PodrijetloHidmet, []Letva{l}, SifraSrpske)
		if err == nil || n != 0 || !strings.HasPrefix(err.Error(), "hidmet.gov.rs bezdan: sat ") {
			t.Errorf("%+v: %d, %v", l.Dani, n, err)
		}
	}
	var redaka int
	if err := db.QueryRow(`SELECT count(*) FROM tude`).Scan(&redaka); err != nil || redaka != 0 {
		t.Fatalf("zapisano %d, %v", redaka, err)
	}

	for _, tc := range []struct {
		datumi []string
		sada   time.Time
		zelim  int
	}{
		{[]string{"24.10.", "25.10.", "25.10.", "26.10.", "27.10."}, izd.Add(time.Hour), 5},
		{[]string{"10.10.", "11.10.", "11.10.", "12.10.", "13.10."}, time.Date(2026, 10, 10, 13, 0, 0, 0, models.Zagreb), 2},
	} {
		l, err := CitajHidmet(tablicaHidmeta(tc.datumi...), tc.sada)
		var nd *NeuredniDatumi
		if !errors.As(err, &nd) {
			t.Fatal(err)
		}
		if n, err := SpremiTude(db, PodrijetloHidmet, l, SifraSrpske); err != nil || n != tc.zelim {
			t.Fatalf("%v: zapisano %d, %v; želim %d", tc.datumi, n, err, tc.zelim)
		}
	}
	r, err := db.Query(`SELECT ciljni, vrijednost FROM tude WHERE letva = 'bezdan' ORDER BY ciljni`)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var got [][2]int64
	for r.Next() {
		var c, v int64
		if err := r.Scan(&c, &v); err != nil {
			t.Fatal(err)
		}
		got = append(got, [2]int64{c, v})
	}
	zelim := [][2]int64{{dan(2026, 10, 10).Unix() / 3600, 100}, {dan(2026, 10, 11).Unix() / 3600, 101}}
	for i := range 5 {
		zelim = append(zelim, [2]int64{dan(2026, 10, 24+i).Unix() / 3600, int64(100 + i)})
	}
	if !reflect.DeepEqual(got, zelim) {
		t.Errorf("tuđe vrijednosti %v, želim %v", got, zelim)
	}
}

// Stari srpski retci ispravljaju se jednom, pri prvom otvaranju baze ovim
// izdanjem: 23. 10. 2026. stupac pod 26. 10. pripada 27. 10., a 24. 10.
// stupci pod 26. i 27. 10. pripadaju 27. i 28. 10. Postaja kojoj je pravi
// dan već stigao razmjenom gubi stari redak, a granice obrane spremljene kao
// prognoza brišu se. Izdanja bez pomaka sata i druga ponovna otvaranja ne
// mijenjaju ništa.
func TestIspraviPomakSataRHMZ(t *testing.T) {
	put := filepath.Join(t.TempDir(), "p.db")
	db, err := Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	izdanje := func(d int) int64 { return time.Date(2026, 10, d, 12, 0, 0, 0, models.Zagreb).Unix() / 3600 }
	upisi := func(letva string, izdano int64, dani map[int]int) {
		t.Helper()
		for d, v := range dani {
			if _, err := db.Exec(`INSERT INTO tude (izvor, letva, izdano, ciljni, vrijednost) VALUES (?,?,?,?,?)`,
				PodrijetloHidmet, letva, izdano, dan(2026, 10, d).Unix()/3600, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	// kako ih je spremila starija inačica (drugi isti dan preskočen)
	upisi("bezdan", izdanje(10), map[int]int{10: 0, 11: 1, 12: 2, 13: 3, 14: 4})
	upisi("bezdan", izdanje(22), map[int]int{22: 0, 23: 1, 24: 2, 25: 3})
	upisi("bezdan", izdanje(23), map[int]int{23: 0, 24: 1, 25: 2, 26: 4})
	upisi("bezdan", izdanje(24), map[int]int{24: 0, 25: 1, 26: 3, 27: 4})
	// primatelj kojem je ispravljeni izdavač već donio 28. 10.
	upisi("apatin", izdanje(24), map[int]int{24: 0, 25: 1, 26: 3, 27: 4, 28: 4})
	// granice obrane iz ćelija „-” (Varvarin 400 i 500)
	upisi("varvarin", izdanje(10), map[int]int{10: -212, 11: -211, 12: -210, 13: 400, 14: 500})
	upisi("varvarin", izdanje(24), map[int]int{24: -212, 25: -211, 26: 400, 27: 500})
	if _, err := db.Exec(`DROP TABLE tude_neuredne`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	stanje := func() map[string]map[int]int {
		t.Helper()
		db, err := Otvori(put)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		r, err := db.Query(`SELECT letva, izdano, ciljni, vrijednost FROM tude`)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		out := map[string]map[int]int{}
		for r.Next() {
			var letva string
			var izdano, ciljni int64
			var v int
			if err := r.Scan(&letva, &izdano, &ciljni, &v); err != nil {
				t.Fatal(err)
			}
			k := fmt.Sprintf("%s %d", letva, time.Unix(izdano*3600, 0).In(models.Zagreb).Day())
			if out[k] == nil {
				out[k] = map[int]int{}
			}
			out[k][time.Unix(ciljni*3600, 0).In(models.Zagreb).Day()] = v
		}
		return out
	}
	zelim := map[string]map[int]int{
		"bezdan 10":   {10: 0, 11: 1, 12: 2, 13: 3, 14: 4},
		"bezdan 22":   {22: 0, 23: 1, 24: 2, 25: 3},
		"bezdan 23":   {23: 0, 24: 1, 25: 2, 27: 4},
		"bezdan 24":   {24: 0, 25: 1, 27: 3, 28: 4},
		"apatin 24":   {24: 0, 25: 1, 27: 3, 28: 4},
		"varvarin 10": {10: -212, 11: -211, 12: -210},
		"varvarin 24": {24: -212, 25: -211},
	}
	for i := range 2 { // drugo otvaranje ne preslaže ponovno
		if got := stanje(); !reflect.DeepEqual(got, zelim) {
			t.Errorf("otvaranje %d:\n %v\nželim\n %v", i+1, got, zelim)
		}
	}
}

// Ispravak koji ne uspije javi grešku i ne stvori tablicu, pa ide i pri
// idućem otvaranju: baza bez tablice tuđih prognoza, sat izdanja koji nije
// broj, brisanje ili premještanje koje baza odbije, ime tablice koje je
// zauzeto, zatvorena baza.
func TestUvediNeuredneGreske(t *testing.T) {
	izd := time.Date(2026, 10, 24, 12, 0, 0, 0, models.Zagreb).Unix() / 3600
	redak := func(d int) string {
		return fmt.Sprintf(`INSERT INTO tude (izvor, letva, izdano, ciljni, vrijednost) VALUES ('hidmet.gov.rs', 'bezdan', %d, %d, 1)`,
			izd, dan(2026, 10, d).Unix()/3600)
	}
	for _, tc := range []struct {
		ime   string
		upiti []string
	}{
		{"bez tablice", []string{`DROP TABLE tude`}},
		{"sat nije broj", []string{`INSERT INTO tude (izvor, letva, izdano, ciljni, vrijednost) VALUES ('hidmet.gov.rs', 'bezdan', 'x', 1, 1)`}},
		{"brisanje", []string{redak(27), redak(28), `CREATE TRIGGER ne BEFORE DELETE ON tude BEGIN SELECT RAISE(ABORT, 'ne'); END`}},
		{"premještanje", []string{redak(27), `CREATE TRIGGER ne BEFORE UPDATE ON tude BEGIN SELECT RAISE(ABORT, 'ne'); END`}},
		{"ime zauzeto", []string{`CREATE VIEW tude_neuredne AS SELECT 1`}},
		{"granice", []string{fmt.Sprintf(`INSERT INTO tude (izvor, letva, izdano, ciljni, vrijednost) VALUES ('hidmet.gov.rs', 'varvarin', %[1]d, %[2]d, -212), ('hidmet.gov.rs', 'varvarin', %[1]d, %[3]d, 400)`,
			izd, dan(2026, 10, 24).Unix()/3600, dan(2026, 10, 25).Unix()/3600), `CREATE TRIGGER ne BEFORE DELETE ON tude BEGIN SELECT RAISE(ABORT, 'ne'); END`}},
	} {
		db := otvoriProbnu(t, "p.db")
		izvrsi(t, db, append([]string{`DROP TABLE tude_neuredne`}, tc.upiti...)...)
		if err := uvediNeuredne(db); err == nil {
			t.Errorf("%s: bez greške", tc.ime)
		}
	}
	db := otvoriProbnu(t, "z.db")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := uvediNeuredne(db); err == nil {
		t.Error("zatvorena baza: bez greške")
	}
}

// Spremanje koje baza odbije javi grešku: upis tuđe vrijednosti i upis
// neuredne tablice; postaja bez dana ne dira ništa.
func TestSpremanjeTudihGreske(t *testing.T) {
	db := otvoriProbnu(t, "p.db")
	izd := time.Date(2026, 10, 24, 12, 0, 0, 0, models.Zagreb)
	if n, err := SpremiTude(db, PodrijetloHidmet, []Letva{{Naziv: "BEZDAN", Izdano: izd}}, SifraSrpske); err != nil || n != 0 {
		t.Errorf("bez dana: %d, %v", n, err)
	}
	izvrsi(t, db, `CREATE TRIGGER ne BEFORE INSERT ON tude BEGIN SELECT RAISE(ABORT, 'ne'); END`)
	if _, err := SpremiTude(db, PodrijetloHidmet, []Letva{{Naziv: "BEZDAN", Izdano: izd, Danas: 1, ImaDanas: true}}, SifraSrpske); err == nil ||
		!strings.HasPrefix(err.Error(), "hidmet.gov.rs bezdan: ") {
		t.Errorf("odbijen upis: %v", err)
	}

	nd := &NeuredniDatumi{Izvor: PodrijetloHidmet, Izdano: izd, Urednih: 1, Datumi: []time.Time{dan(2026, 10, 24), dan(2026, 10, 24)}}
	izvrsi(t, db, `CREATE TRIGGER ne2 BEFORE INSERT ON tude_neuredne BEGIN SELECT RAISE(ABORT, 'ne'); END`)
	if err := ZapamtiNeuredne(db, PodrijetloHidmet, nd); err == nil {
		t.Error("odbijen upis neuredne: bez greške")
	}
	izvrsi(t, db, `DROP TABLE tude_neuredne`)
	if err := ZapamtiNeuredne(db, PodrijetloHidmet, nd); err == nil {
		t.Error("bez tablice: bez greške")
	}
	izvrsi(t, db, `CREATE TABLE tude_neuredne (izvor TEXT, tablica TEXT, izdano INTEGER, tezina INTEGER, opis TEXT)`,
		fmt.Sprintf(`INSERT INTO tude_neuredne VALUES ('x', '', %d, 'nije broj', 'o')`, izd.Unix()/3600))
	if got := upozorenjaTudih(db, izd.Unix()/3600); len(got) != 1 || !strings.Contains(got[0].Opis, "nije pročitan") {
		t.Errorf("težina nije broj: %+v", got)
	}
}

// Postaja kojoj sati ne idu naprijed preskače se sama: ostale postaje istog
// izvora zapišu se, a greška ih imenuje.
func TestSpremiTudePreskaceSamoPostaju(t *testing.T) {
	db := otvoriProbnu(t, "p.db")
	izd := time.Date(2026, 10, 24, 9, 0, 0, 0, models.Zagreb)
	n, err := SpremiTude(db, Podrijetlo, []Letva{
		{Naziv: "Osijek", Izdano: izd, Danas: 1, ImaDanas: true, Dani: []Dan{{Kad: dan(2026, 10, 25), Cm: 2}}},
		{Naziv: "Aljmaš", Izdano: izd, Danas: 1, ImaDanas: true, Dani: []Dan{{Kad: dan(2026, 10, 24), Cm: 2}}},
	}, Sifra)
	var pr *PreskocenePostaje
	if !errors.As(err, &pr) || len(pr.Greske) != 1 || !strings.HasPrefix(err.Error(), "hydroinfo.hu aljmas: sat 24. 10. u 07 h") || n != 2 {
		t.Errorf("novih %d, %v", n, err)
	}
}
