package hvpovijest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gocop/internal/hidroview"
	"gocop/internal/models"
)

// Zaključava preuzimanje povijesti letve s HydroViewa na lažnom poslužitelju
// koji test sam podigne: traženje zapisivača po nazivu, ime izvora po
// opremi, izbor veličine iz naknadne obrade i dopis u arhivu po komadima.

var pocetak = time.Date(2026, 3, 1, 0, 0, 0, 0, models.Zagreb)

const postajeJSON = `{"status":"ok","sites_get":{"groups_sites":[{"group":{"name":"Drava"},"sites":[
 {"site_id":"s2","logger_id":"L2","last_timestamp":0,"site":{"name":"Tikveš","scan_interval_min":15,"station_id":"T2"}},
 {"site_id":"s1","logger_id":"L1","last_timestamp":2000,"site":{"name":"TIKVEŠ","scan_interval_min":15,"station_id":"T1"}},
 {"site_id":"s3","logger_id":"L3","last_timestamp":3000,"site":{"name":"Drugo","scan_interval_min":15,"station_id":"D"}}
]}]}}`

// oprema po postaji: s1 ima radar i vodostaj iz naknadne obrade, protok koji
// poslužitelj ne da i pragove; s2 ima samo sirovi radar; s3 se ne da pročitati
var opremaJSON = map[string]string{
	"s1": `{"status":"ok","installed_equipment":{
		"communicator":{"measurements":[{"msr_id":"napon1","qty_id":"#napon"}]},
		"instruments":[{"name":"LX-80 radar","measurements":[{"msr_id":"sirovo1","qty_id":"#hydro-$7"},{"msr_id":"q1","qty_id":"#hydro--$$$$25"}]}],
		"postprocessing":[{"msr_id":"m1","qty_id":"#hydro-$7","description":"vodostaj letve","param_sets":[{"valid_from":1,"settings":{"offset":"1.20"}}]}],
		"alarms":[{"msr_id":"m1","description":"Redovno stanje","relational_operator":">","threshold_value":1.5},
		          {"msr_id":"napon1","description":"Napon","relational_operator":"<","threshold_value":11}]}}`,
	"s2": `{"status":"ok","installed_equipment":{"instruments":[{"name":"LX-80","measurements":[{"msr_id":"m2","qty_id":"#hydro-$7"}]}]}}`,
	"s3": `{"status":"greska"}`,
}

// vrijednosti mjerenja: sekunde od 1970. i vrijednost u osnovnoj jedinici
var podaci = map[string][][2]float64{
	"m1": {
		{float64(pocetak.Add(17*time.Hour + 45*time.Minute + 56*time.Second).Unix()), 0.42401},
		{float64(pocetak.Add(24*time.Hour + 20*time.Second).Unix()), 0.43},
		{float64(pocetak.Add(50 * time.Hour).Unix()), 0.5},
		{float64(pocetak.Add(80 * time.Hour).Unix()), 0.987}, // izvan razdoblja
	},
	"m2": nil,
}

// lazniHydroView odgovara kao HydroView API; data/get daje samo vrijednosti
// unutar traženog komada, a za nepoznato mjerenje odgovara greškom.
func lazniHydroView(t *testing.T, zahtjeva *atomic.Int32) *hidroview.Klijent {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case !strings.HasPrefix(r.URL.Path, "/api/v1/"):
			http.NotFound(w, r)
		case strings.HasSuffix(r.URL.Path, "sites/get_verbose"):
			_, _ = fmt.Fprint(w, postajeJSON)
		case strings.HasSuffix(r.URL.Path, "installed_equipment/get"):
			_, _ = fmt.Fprint(w, opremaJSON[r.URL.Query().Get("site_id")])
		case strings.HasSuffix(r.URL.Path, "data/get"):
			id := r.URL.Query().Get("id0")
			if zahtjeva != nil && id == "m1" {
				zahtjeva.Add(1)
			}
			niz, ima := podaci[id]
			if !ima {
				http.Error(w, "nema", http.StatusInternalServerError)
				return
			}
			od, _ := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
			do, _ := strconv.ParseInt(r.URL.Query().Get("end"), 10, 64)
			_, _ = fmt.Fprintf(w, "timestamp,%s\n", id)
			for _, v := range niz {
				if int64(v[0]) >= od && int64(v[0]) < do {
					_, _ = fmt.Fprintf(w, "%d,%g\n", int64(v[0]), v[1])
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &hidroview.Klijent{Adresa: srv.URL, HTTP: srv.Client()}
}

// Zapisivači se traže po dijelu naziva bez obzira na velika slova, onaj
// koji se javio zadnji prvi; kad nijedan ne nosi naziv, greška kaže među
// koliko ih je traženo.
func TestNadjiPostaje(t *testing.T) {
	k := lazniHydroView(t, nil)
	ctx := context.Background()
	postaje, err := NadjiPostaje(ctx, k, "tikv")
	if err != nil {
		t.Fatal(err)
	}
	if len(postaje) != 2 || postaje[0].LoggerID != "L1" || postaje[1].LoggerID != "L2" || postaje[0].Grupa != "Drava" {
		t.Errorf("nađeno %+v", postaje)
	}
	if _, err := NadjiPostaje(ctx, k, "Botovo"); err == nil || !strings.Contains(err.Error(), "„Botovo“ nije nađena među 3 postaja") {
		t.Errorf("nepostojeća postaja: %v", err)
	}
	pokvaren := &hidroview.Klijent{Adresa: k.Adresa + "/pogresno", HTTP: k.HTTP}
	if _, err := NadjiPostaje(ctx, pokvaren, "Tikveš"); err == nil {
		t.Error("neuspio popis postaja mora javiti grešku")
	}
}

// Kad istu veličinu daje više izvora, prednost ima naknadna obrada.
func TestNadjiMjerenje(t *testing.T) {
	mjerenja := []hidroview.Mjerenje{
		{ID: "t", Velicina: hidroview.VelicinaTempVode, Odakle: "HydroTemp"},
		{ID: "sirovo", Velicina: hidroview.VelicinaSrednjiVodostaj, Odakle: "LX-80"},
		{ID: "drugo", Velicina: hidroview.VelicinaSrednjiVodostaj, Odakle: "Bubbler"},
		{ID: "obrada", Velicina: hidroview.VelicinaSrednjiVodostaj, Odakle: "obrada: vodostaj letve"},
	}
	if m := nadjiMjerenje(mjerenja, hidroview.VelicinaSrednjiVodostaj); m == nil || m.ID != "obrada" {
		t.Errorf("vodostaj iz %+v, očekivana naknadna obrada", m)
	}
	if m := nadjiMjerenje(mjerenja[:3], hidroview.VelicinaSrednjiVodostaj); m == nil || m.ID != "sirovo" {
		t.Errorf("bez obrade %+v, očekivan prvi izvor", m)
	}
	if m := nadjiMjerenje(mjerenja, hidroview.VelicinaProtok); m != nil {
		t.Errorf("protok koji nitko ne mjeri: %+v", m)
	}
}

// Izvor se imenuje po opremi; kad se uređaj ne vidi, ostaje samo geolux.
func TestPrepoznajIzvor(t *testing.T) {
	slucajevi := []struct {
		odakle []string
		izvor  string
	}{
		{[]string{"zapisivač", "LX-80"}, "geolux-radar"},
		{[]string{"Radar razine"}, "geolux-radar"},
		{[]string{"Bubbler"}, "geolux-seba"},
		{[]string{"zapisivač", "SEBA tlačni"}, "geolux-seba"},
		{[]string{"zapisivač", "HydroTemp"}, "geolux"},
		{nil, "geolux"},
	}
	for _, s := range slucajevi {
		var m []hidroview.Mjerenje
		for _, o := range s.odakle {
			m = append(m, hidroview.Mjerenje{Odakle: o})
		}
		if got := PrepoznajIzvor(m); got != s.izvor {
			t.Errorf("%v → %q, očekivano %q", s.odakle, got, s.izvor)
		}
	}
	if vrijemeIliNikad(0) != "nikad" || vrijemeIliNikad(-5) != "nikad" {
		t.Error("zapisivač koji se nije javio je „nikad“")
	}
	if got := vrijemeIliNikad(1790005556); got != time.Unix(1790005556, 0).Format("2006-01-02 15:04") {
		t.Errorf("zadnje javljanje %q", got)
	}
}

// Dva zapisivača iste vrste dobiju broj zapisivača u imenu izvora. Vodostaj
// stiže u metrima i zapisuje se u centimetrima, vrijeme se svodi na mjerni
// korak, a razdoblje se traži u komadima. Proba samo broji.
func TestPreuzmiDopisujeArhivu(t *testing.T) {
	var zahtjeva atomic.Int32
	k := lazniHydroView(t, &zahtjeva)
	koren := t.TempDir()
	z := Zadatak{Koren: koren, Sliv: "drava", Letva: "tikves", Naziv: "Tikveš",
		Od: pocetak, Do: pocetak.Add(72 * time.Hour), Komad: 1, Zaokruzi: -1, Probno: true}
	var dnevnik strings.Builder
	z.Dnevnik = &dnevnik
	ishod, err := Preuzmi(context.Background(), k, z)
	if err != nil {
		t.Fatal(err)
	}
	if ishod.Vrijednosti != 3 || len(ishod.Zapisano) != 0 {
		t.Errorf("proba: %+v", ishod)
	}
	if n := zahtjeva.Load(); n != 3 {
		t.Errorf("zahtjeva za podatke %d, očekivana tri dnevna komada", n)
	}
	for _, dio := range []string{
		"nađeno zapisivača: 2",
		"TIKVEŠ (T1), zapisivač L1, zadnje javljanje ",
		"-> izvor geolux-radar-L1",
		"Tikveš (T2), zapisivač L2, zadnje javljanje nikad -> izvor geolux-radar-L2",
		"njihov prag: Redovno stanje",
		"> 1.50 m = 150 cm",
		"temperatura  ovaj zapisivač tu veličinu ne mjeri",
		"protok       data/get",
		"vodostaj     nema vrijednosti u zadanom razdoblju",
		"probni prolaz — ništa nije zapisano",
	} {
		if !strings.Contains(dnevnik.String(), dio) {
			t.Errorf("u dnevniku nema %q:\n%s", dio, dnevnik.String())
		}
	}
	if strings.Contains(dnevnik.String(), "Napon") {
		t.Error("prag napona nije prag obrane i ne ispisuje se")
	}
	if stavke, _ := os.ReadDir(koren); len(stavke) != 0 {
		t.Errorf("proba je zapisala %d stavki", len(stavke))
	}

	z.Probno, z.Dnevnik, z.Zaokruzi, z.Komad = false, nil, 15, 0
	ishod, err = Preuzmi(context.Background(), k, z)
	if err != nil {
		t.Fatal(err)
	}
	if len(ishod.Zapisano) != 1 || !strings.HasPrefix(ishod.Zapisano[0], filepath.Join(koren, "drava", "tikves", "tikves_geolux-radar-L1_vodostaj_satni_2026")) {
		t.Fatalf("zapisano %v", ishod.Zapisano)
	}
	b, err := os.ReadFile(ishod.Zapisano[0])
	if err != nil {
		t.Fatal(err)
	}
	// vrijeme svedeno na korak od 15 minuta, u centimetrima, bez vrijednosti
	// izvan razdoblja
	ocekivano := "vrijeme_utc;vodostaj_cm\n2026-03-01 17:45:00;42.401\n2026-03-02 00:00:00;43\n2026-03-03 02:00:00;50\n"
	if string(b) != ocekivano {
		t.Errorf("niz:\n%s", b)
	}
}

// Izvor zadan rukom vrijedi za sve zapisivače, a uz dva dobiva broj
// zapisivača; greške opreme i zapisa zaustavljaju preuzimanje.
func TestPreuzmiGreske(t *testing.T) {
	k := lazniHydroView(t, nil)
	ctx := context.Background()
	osnova := Zadatak{Koren: t.TempDir(), Sliv: "drava", Letva: "tikves", Naziv: "Tikveš",
		Od: pocetak, Do: pocetak.Add(72 * time.Hour)}

	for ime, s := range map[string]struct {
		z      Zadatak
		greska string
	}{
		"bez sliva":         {Zadatak{Letva: "tikves"}, "trebaju sliv i letva"},
		"bez letve":         {Zadatak{Sliv: "drava"}, "trebaju sliv i letva"},
		"nema postaje":      {Zadatak{Sliv: "drava", Letva: "botovo"}, "nije nađena"},
		"oprema se ne čita": {Zadatak{Sliv: "drava", Letva: "drugo"}, "oprema postaje"},
	} {
		if _, err := Preuzmi(ctx, k, s.z); err == nil || !strings.Contains(err.Error(), s.greska) {
			t.Errorf("%s: %v", ime, err)
		}
	}

	z := osnova
	z.Izvor = "geolux_radar" // donja crta ne smije u naziv niza
	if _, err := Preuzmi(ctx, k, z); err == nil || !strings.Contains(err.Error(), "donju crtu") {
		t.Errorf("neispravan izvor: %v", err)
	}

	z = osnova
	z.Naziv, z.Izvor, z.Vrsta = "TIKVEŠ", "rucni", "satni"
	ishod, err := Preuzmi(ctx, k, z)
	if err != nil {
		t.Fatal(err)
	}
	if len(ishod.Zapisano) != 1 || !strings.Contains(ishod.Zapisano[0], "tikves_rucni-L1_vodostaj_satni_") {
		t.Errorf("zadani izvor uz dva zapisivača: %v", ishod.Zapisano)
	}
}
