package svjeza

import (
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gocop/internal/arhiva"
	"gocop/internal/db"
	"gocop/internal/javnivodostaji"
	"gocop/internal/models"
	"gocop/internal/oborine"

	_ "modernc.org/sqlite"
)

// Svi brojevi u ovim provjerama su izmišljeni.

// cvor je baza čvora sa zadanom shemom, stablo s mapama letve „proba” i
// točke „k-proba-1” te baza oborina.
func cvor(t *testing.T) (*Ulagac, string) {
	t.Helper()
	dir := t.TempDir()
	put := filepath.Join(dir, "gocop.db")
	if err := db.UpisiPredlozak(put); err != nil {
		t.Fatal(err)
	}
	baza, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	ob, err := oborine.Otvori(filepath.Join(dir, "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ob.Close() })
	koren := filepath.Join(dir, "stablo")
	for _, m := range []string{"dunav/proba", "drava/k-proba-1"} {
		if err := os.MkdirAll(filepath.Join(koren, m), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range []string{"proba", "bez-mape"} {
		if _, err := baza.Exec(`INSERT INTO stations (id, code, name, created_at, updated_at)
			VALUES (?, ?, ?, datetime('now'), datetime('now'))`, "st-"+s, s, "Letva "+s); err != nil {
			t.Fatal(err)
		}
	}
	return &Ulagac{Baza: baza, Oborine: ob, Koren: koren}, koren
}

// ocitanje upiše jedno preuzeto očitanje letve.
func upisi(t *testing.T, u *Ulagac, letva, podrijetlo string, kad time.Time, cm int, sirovo *int, protok *float64, biljeska string) {
	t.Helper()
	id := letva + podrijetlo + kad.Format(time.RFC3339)
	if _, err := u.Baza.Exec(`INSERT INTO readings (id, station_id, measured_at, level_cm, sirovo_cm, flow_m3s, flow_note,
		source, origin, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))`,
		id, "st-"+letva, kad.UTC(), cm, sirovo, protok, biljeska, models.ReadingSourceImport, podrijetlo); err != nil {
		t.Fatal(err)
	}
}

// dani upiše satna očitanja letve od dana od, n dana, s vodostajem po danu.
func dani(t *testing.T, u *Ulagac, letva, podrijetlo string, od time.Time, razine ...int) {
	t.Helper()
	for i, cm := range razine {
		for h := 0; h < 24; h += 6 {
			upisi(t, u, letva, podrijetlo, od.AddDate(0, 0, i).Add(time.Duration(h)*time.Hour), cm, nil, nil, "")
		}
	}
}

func redci(t *testing.T, koren, sliv, letva, izvor, velicina string) []arhiva.Redak {
	t.Helper()
	r, err := arhiva.PostojeciRedci(koren, sliv, letva, izvor, velicina, "satni")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func zg(g int, m time.Month, d, h int) time.Time {
	return time.Date(g, m, d, h, 0, 0, 0, models.Zagreb)
}

// Završeni dani idu u stablo pod arhivskim izvorom živog izvora; današnji
// dan, ručno uloženo, sumnjivo i protok iz naše krivulje ne idu. Vodostaj je
// onaj koji je poslao uređaj, a ne ispravljen po ručnom.
func TestUlaganjeLetve(t *testing.T) {
	u, koren := cvor(t)
	sada := zg(2026, 11, 25, 0).Add(45 * time.Minute)
	sirovo, protok, nasProtok := 205, 812.5, 900.0
	upisi(t, u, "proba", javnivodostaji.Podrijetlo, zg(2026, 11, 24, 10), 210, &sirovo, &protok, "")
	upisi(t, u, "proba", javnivodostaji.Podrijetlo, zg(2026, 11, 24, 11), 212, nil, &nasProtok, models.BiljeskaNaseKrivulje)
	upisi(t, u, "proba", javnivodostaji.Podrijetlo, zg(2026, 11, 24, 11).Add(30*time.Minute), 213, nil, nil, "")
	upisi(t, u, "proba", javnivodostaji.PodrijetloHidroView, zg(2026, 11, 24, 12), 214, nil, nil, "")
	upisi(t, u, "proba", javnivodostaji.Podrijetlo, zg(2026, 11, 25, 0), 220, nil, nil, "") // dan još traje
	upisi(t, u, "proba", "letva.voda.hr — zalijepljeno", zg(2026, 11, 24, 13), 230, nil, nil, "")
	upisi(t, u, "bez-mape", javnivodostaji.Podrijetlo, zg(2026, 11, 24, 10), 100, nil, nil, "")
	if _, err := u.Baza.Exec(`UPDATE readings SET quality = ? WHERE level_cm = 212`, models.QualityUncertain); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Baza.Exec(`UPDATE readings SET temp_c = 8.5 WHERE level_cm = 214`); err != nil {
		t.Fatal(err)
	}

	iz, err := u.Ulozi(sada, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(iz.Letve, []string{"proba"}) || !slices.Equal(iz.BezMape, []string{"bez-mape"}) {
		t.Fatalf("uloženo %v, bez mape %v", iz.Letve, iz.BezMape)
	}
	hv := redci(t, koren, "dunav", "proba", "hv-uzivo", "vodostaj")
	if len(hv) != 1 || hv[0].Vrijednost != 205 || !hv[0].Vrijeme.Equal(zg(2026, 11, 24, 10)) {
		t.Errorf("hv-uzivo: %+v", hv)
	}
	if p := redci(t, koren, "dunav", "proba", "hv-uzivo", "protok"); len(p) != 1 || p[0].Vrijednost != 812.5 {
		t.Errorf("protok: %+v", p)
	}
	if g := redci(t, koren, "dunav", "proba", "geolux-uzivo", "vodostaj"); len(g) != 1 || g[0].Vrijednost != 214 {
		t.Errorf("HydroView ide pod geolux-uzivo: %+v", g)
	}
	if tv := redci(t, koren, "dunav", "proba", "geolux-uzivo", "temperatura"); len(tv) != 1 || tv[0].Vrijednost != 8.5 {
		t.Errorf("temperatura vode: %+v", tv)
	}

	// drugo ulaganje istog dana ne udvostručuje, a novi dan se nadoda
	upisi(t, u, "proba", javnivodostaji.Podrijetlo, zg(2026, 11, 25, 6), 222, nil, nil, "")
	if _, err := u.Ulozi(zg(2026, 11, 26, 1), true, false); err != nil {
		t.Fatal(err)
	}
	hv = redci(t, koren, "dunav", "proba", "hv-uzivo", "vodostaj")
	if len(hv) != 3 {
		t.Errorf("nakon drugog dana %d redaka, želim 3: %+v", len(hv), hv)
	}
}

// Računalo ugašeno danima: kad se upali, uloži sve dane od zadnjeg
// ulaganja, ne samo jučer.
func TestRupaSeNadoknadi(t *testing.T) {
	u, koren := cvor(t)
	dani(t, u, "proba", javnivodostaji.Podrijetlo, zg(2026, 11, 10, 0), 100, 101)
	if _, err := u.Ulozi(zg(2026, 11, 12, 1), true, false); err != nil {
		t.Fatal(err)
	}
	dani(t, u, "proba", javnivodostaji.Podrijetlo, zg(2026, 11, 12, 0), 102, 103, 104, 105, 106)
	if _, err := u.Ulozi(zg(2026, 11, 17, 1), true, false); err != nil {
		t.Fatal(err)
	}
	if n := len(redci(t, koren, "dunav", "proba", "hv-uzivo", "vodostaj")); n != 7*4 {
		t.Errorf("uloženo %d očitanja, želim %d", n, 7*4)
	}
}

// Paket ide prvi put odmah, zatim najviše jednom tjedno, a odmah kad je
// promjena velika.
func TestPaketJednomTjedno(t *testing.T) {
	u, _ := cvor(t)
	dani(t, u, "proba", javnivodostaji.Podrijetlo, zg(2026, 11, 1, 0), 100, 100, 100)
	sada := zg(2026, 11, 4, 1)
	if _, err := u.Ulozi(sada, true, false); err != nil {
		t.Fatal(err)
	}
	if l, err := NaRedu(u.Baza, sada); err != nil || !slices.Equal(l, []string{"proba"}) {
		t.Fatalf("prvo ulaganje: na redu %v %v", l, err)
	}
	if err := Izdano(u.Baza, []string{"proba"}, 1, sada); err != nil {
		t.Fatal(err)
	}
	// mala promjena: čeka tjedan
	for d := 4; d < 11; d++ {
		dani(t, u, "proba", javnivodostaji.Podrijetlo, zg(2026, 11, d, 0), 100+d%2)
		sada = zg(2026, 11, d+1, 1)
		if _, err := u.Ulozi(sada, true, false); err != nil {
			t.Fatal(err)
		}
		l, err := NaRedu(u.Baza, sada)
		if err != nil {
			t.Fatal(err)
		}
		if tjedan := d+1 >= 11; tjedan != (len(l) == 1) {
			t.Errorf("%d. 11.: na redu %v", d+1, l)
		}
	}
	if err := Izdano(u.Baza, []string{"proba"}, 1, sada); err != nil {
		t.Fatal(err)
	}
	// porast od 40 cm u danu: odmah
	dani(t, u, "proba", javnivodostaji.Podrijetlo, zg(2026, 11, 11, 0), 140)
	sada = zg(2026, 11, 12, 1)
	iz, err := u.Ulozi(sada, true, false)
	if err != nil || !slices.Equal(iz.Velike, []string{"proba"}) {
		t.Fatalf("velika promjena: %v %v", iz.Velike, err)
	}
	if l, err := NaRedu(u.Baza, sada); err != nil || len(l) != 1 {
		t.Errorf("velika promjena nije na redu: %v %v", l, err)
	}
	s, err := ProcitajStanje(u.Baza)
	if err != nil || s.Letvi != 1 || s.Paketa != 1 || !s.Ulozeno.Equal(sada) {
		t.Errorf("stanje %+v %v", s, err)
	}
}

func TestVelikaPromjena(t *testing.T) {
	d := func(vrijednosti ...float64) map[int64]float64 {
		m := map[int64]float64{}
		for i, v := range vrijednosti {
			m[int64(100+i)] = v
		}
		return m
	}
	for _, s := range []struct {
		ime   string
		dani  map[int64]float64
		od    int64
		zelim bool
	}{
		{"prvo ulaganje", d(100, 200), 0, false},
		{"mirno", d(100, 102, 105, 103), 102, false},
		{"skok od 31 cm", d(100, 100, 131), 102, true},
		{"novi vrh 11 cm iznad", d(150, 100, 120, 140, 161), 104, true},
		{"vrh samo 5 cm iznad", d(150, 140, 135, 140, 155), 104, false},
	} {
		if got := VelikaPromjena(s.dani, s.od); got != s.zelim {
			t.Errorf("%s: %v", s.ime, got)
		}
	}
}

// Prošli sati Open-Meteo točke idu pod izvor openmeteo; sati iz prognoze i
// današnji ne idu. Dan s puno kiše je velika promjena.
func TestUlaganjeTocke(t *testing.T) {
	u, koren := cvor(t)
	sada := zg(2026, 11, 25, 0).Add(45 * time.Minute)
	upisiSat := func(kad time.Time, mm float64, prognoza int) {
		if _, err := u.Oborine.Exec(`INSERT INTO satne (kisomjer, sat, oborina, snijeg, temperatura, prognoza, preuzeto)
			VALUES ('k-proba-1', ?, ?, 0, 4.5, ?, 0)`, kad.Unix()/3600, mm, prognoza); err != nil {
			t.Fatal(err)
		}
	}
	upisiSat(zg(2026, 11, 23, 10), 1, 0)
	if _, err := u.Ulozi(zg(2026, 11, 24, 1), false, true); err != nil {
		t.Fatal(err)
	}
	for h := 1; h <= 24; h++ {
		upisiSat(zg(2026, 11, 24, 0).Add(time.Duration(h)*time.Hour), 1.5, 0)
	}
	upisiSat(zg(2026, 11, 25, 3), 2, 0)
	upisiSat(zg(2026, 11, 25, 9), 9, 1)
	iz, err := u.Ulozi(sada, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(iz.Tocke, []string{"k-proba-1"}) || !slices.Equal(iz.Velike, []string{"k-proba-1"}) {
		t.Fatalf("točke %v, velike %v", iz.Tocke, iz.Velike)
	}
	ob := redci(t, koren, "drava", "k-proba-1", IzvorOpenMeteo, "oborina")
	if len(ob) != 25 {
		t.Errorf("oborina: %d sati, želim 25", len(ob))
	}
	if tz := redci(t, koren, "drava", "k-proba-1", IzvorOpenMeteo, "temperatura-zraka"); len(tz) != 25 || tz[0].Vrijednost != 4.5 {
		t.Errorf("temperatura zraka: %+v", tz)
	}
	// sutra malo kiše: nije velika promjena
	upisiSat(zg(2026, 11, 25, 12), 2, 0)
	if iz, err := u.Ulozi(zg(2026, 11, 26, 1), false, true); err != nil || len(iz.Velike) != 0 || len(iz.Tocke) != 1 {
		t.Errorf("malo kiše: %+v %v", iz, err)
	}
	// samo letve: točke ne
	u2, _ := cvor(t)
	if iz, err := u2.Ulozi(sada, true, false); err != nil || len(iz.Tocke) != 0 {
		t.Errorf("bez uloge točke uložene: %v %v", iz.Tocke, err)
	}
}

// Novi izvori uživo su iza ERA5 i iza svih operativnih izvora, a uključeni.
func TestIzvoriUzivoIzaOperativnih(t *testing.T) {
	d, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := arhiva.PripremiPraznu(d); err != nil {
		t.Fatal(err)
	}
	izvori, err := arhiva.Izvori(d)
	if err != nil {
		t.Fatal(err)
	}
	po := map[string]arhiva.Izvor{}
	for _, i := range izvori {
		po[i.Naziv] = i
	}
	imena := []string{IzvorOpenMeteo}
	for _, a := range IzvoriUzivo {
		imena = append(imena, a)
	}
	for _, n := range imena {
		i, ima := po[n]
		if !ima || !i.Ukljucen {
			t.Errorf("%s nije uključen među izvorima arhive", n)
			continue
		}
		if strings.HasSuffix(n, "-uzivo") || n == IzvorOpenMeteo {
			if i.Red <= po["letva-hv"].Red || i.Red <= po["openmeteo-era5"].Red {
				t.Errorf("%s ima red %d, mora iza operativnih i ERA5", n, i.Red)
			}
		}
	}
}

// Letva s umjerenjem mjernog uređaja: u arhivu ide vrijednost uređaja
// preračunata umjerenjem, a ne ona ispravljena po ručnom očitanju.
// Umjerenje je izmišljeno: uređaj = 10 + 0,8 · letva.
func TestUlaganjeUmjereneLetve(t *testing.T) {
	u, koren := cvor(t)
	if _, err := u.Baza.Exec(`UPDATE stations SET umjerenje_odsjecak = 10, umjerenje_nagib = 0.8 WHERE code = 'proba'`); err != nil {
		t.Fatal(err)
	}
	uredjaj := 170                                                                                // na letvi (170 − 10) / 0,8 = 200
	upisi(t, u, "proba", javnivodostaji.Podrijetlo, zg(2026, 11, 24, 10), 200, &uredjaj, nil, "") // samo umjereno
	upisi(t, u, "proba", javnivodostaji.Podrijetlo, zg(2026, 11, 24, 11), 205, &uredjaj, nil, "") // umjereno pa ispravljeno +5
	if _, err := u.Ulozi(zg(2026, 11, 25, 1), true, false); err != nil {
		t.Fatal(err)
	}
	r := redci(t, koren, "dunav", "proba", "hv-uzivo", "vodostaj")
	if len(r) != 2 || r[0].Vrijednost != 200 || r[1].Vrijednost != 200 {
		t.Errorf("umjerena letva: %+v", r)
	}
}
