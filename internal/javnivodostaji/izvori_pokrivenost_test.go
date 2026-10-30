package javnivodostaji

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Čitači izvora od adrese do redaka: koju stranicu traže, kako čitaju što
// dobiju i što javljaju kad stranica ne odgovori. Odgovara rukovatelj u
// testu (httptest), bez mreže, i za adrese koje su zadane u kodu.

// lazniPrijenos odgovara na svaki zahtjev iz rukovatelja i pamti što je
// traženo.
type lazniPrijenos struct {
	h       http.HandlerFunc
	mu      sync.Mutex
	trazeno []string
}

func (p *lazniPrijenos) RoundTrip(r *http.Request) (*http.Response, error) {
	p.mu.Lock()
	p.trazeno = append(p.trazeno, r.Method+" "+r.URL.String())
	p.mu.Unlock()
	rec := httptest.NewRecorder()
	p.h(rec, r)
	return rec.Result(), nil
}

func (p *lazniPrijenos) zahtjevi() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.trazeno...)
}

// klijentIz je Client čiji zahtjevi idu rukovatelju
func klijentIz(h http.HandlerFunc) (*Client, *lazniPrijenos) {
	p := &lazniPrijenos{h: h}
	return &Client{HTTP: &http.Client{Transport: p}}, p
}

// stranice odgovara sadržajem po putanji (s upitom); ostalo je 404
func stranice(m map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		kljuc := r.URL.Host + r.URL.Path
		if r.URL.RawQuery != "" {
			kljuc += "?" + r.URL.RawQuery
		}
		if s, ok := m[kljuc]; ok {
			_, _ = w.Write([]byte(s))
			return
		}
		if s, ok := m[r.URL.Host+r.URL.Path]; ok {
			_, _ = w.Write([]byte(s))
			return
		}
		http.NotFound(w, r)
	}
}

// Svaki čitač pod svojim imenom stoji uz očitanje kao podrijetlo.
func TestNaziviIzvora(t *testing.T) {
	for want, iz := range map[string]Izvor{
		Podrijetlo: hvIzvor{&Client{}}, PodrijetloARSO: ARSO{}, PodrijetloEHYD: EHYD{}, PodrijetloGKD: GKD{}, PodrijetloHidmet: Hidmet{},
		PodrijetloPegelOnline: PegelOnline{}, PodrijetloSHMU: SHMU{}, PodrijetloVizugy: Vizugy{}, PodrijetloDanubeHIS: DanubeHIS{},
		PodrijetloNOEL: NOEL{}, PodrijetloHidroView: &HidroView{}, "mletva.voda.hr": &MLetva{},
	} {
		if got := iz.Naziv(); got != want {
			t.Errorf("%T: %q, želim %q", iz, got, want)
		}
	}
	if (&Client{}).Naziv() != Podrijetlo {
		t.Error("Client je Hrvatske vode")
	}
}

// ARSO: prvo tablica postaje (bez fragmenta adrese), a kad nje nema ili je
// prazna, službeni XML sa zadnjim očitanjem.
func TestARSOTablicaPaXML(t *testing.T) {
	adresa := AdresaARSO("2150")
	c, p := klijentIz(stranice(map[string]string{
		"www.arso.gov.si/vode/podatki/amp/H2150_t_1.html":   probnaARSOTablica,
		"www.arso.gov.si/xml/vode/hidro_podatki_zadnji.xml": probniARSO,
	}))
	r, err := ARSO{Client: c}.Ocitanja(context.Background(), adresa)
	if err != nil || len(r) != 2 || *r[1].LevelCm != 54 {
		t.Fatalf("tablica: %+v %v", r, err)
	}
	if z := p.zahtjevi(); len(z) != 1 || z[0] != "GET https://www.arso.gov.si/vode/podatki/amp/H2150_t_1.html" {
		t.Errorf("zahtjevi: %v", z)
	}

	// bez tablice: XML, a fragment se ne šalje
	c, p = klijentIz(stranice(map[string]string{"www.arso.gov.si/xml/vode/hidro_podatki_zadnji.xml": probniARSO}))
	r, err = ARSO{Client: c}.Ocitanja(context.Background(), adresa)
	if err != nil || len(r) != 1 || *r[0].LevelCm != 58 || *r[0].FlowM3s != 123.4 {
		t.Fatalf("XML: %+v %v", r, err)
	}
	if z := p.zahtjevi(); len(z) != 2 || z[1] != "GET https://www.arso.gov.si/xml/vode/hidro_podatki_zadnji.xml" {
		t.Errorf("zahtjevi: %v", z)
	}

	// tablica bez ijednog očitanja: opet XML
	prazna := `<table><thead><tr><th>Datum</th><th>Vodostaj [cm]</th></tr></thead><tbody><tr><td>27.09.2026 21:20</td><td>-</td></tr></tbody></table>`
	c, _ = klijentIz(stranice(map[string]string{
		"www.arso.gov.si/vode/podatki/amp/H2150_t_1.html":   prazna,
		"www.arso.gov.si/xml/vode/hidro_podatki_zadnji.xml": probniARSO,
	}))
	if r, err := (ARSO{Client: c}).Ocitanja(context.Background(), adresa); err != nil || len(r) != 1 {
		t.Errorf("prazna tablica: %+v %v", r, err)
	}

	// ni tablice ni XML-a: greška s adresom
	c, _ = klijentIz(stranice(nil))
	if _, err := (ARSO{Client: c}).Ocitanja(context.Background(), adresa); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("ništa: %v", err)
	}
	if _, err := (ARSO{Client: c}).Ocitanja(context.Background(), "https://www.arso.gov.si/xml/vode/hidro_podatki_zadnji.xml"); err == nil {
		t.Error("adresa bez šifre mora biti greška")
	}
}

// Zamjenske adrese (Base) idu na lokalni poslužitelj; zadani klijent na
// lokalnu adresu ne ide, pa test daje svoj.
func TestARSOZamjenskeAdrese(t *testing.T) {
	srv := httptest.NewServer(stranice(map[string]string{
		"/tablica/2150": "<p>nema tablice</p>",
		"/xml":          probniARSO,
	}))
	t.Cleanup(srv.Close)
	a := ARSO{Client: &Client{HTTP: srv.Client()}, Base: srv.URL + "/xml", BaseTablice: srv.URL + "/tablica/%s"}
	r, err := a.Ocitanja(context.Background(), "https://www.arso.gov.si/xml/vode/hidro_podatki_zadnji.xml?postaja=2150")
	if err != nil || len(r) != 1 || *r[0].LevelCm != 58 {
		t.Errorf("zamjenske adrese: %+v %v", r, err)
	}
}

// ARSO XML i tablica: nevaljan XML, nepoznato vrijeme, postaja koje nema,
// tablica bez poznatih stupaca i redci koji se ne daju pročitati.
func TestARSORubovi(t *testing.T) {
	if _, err := CitajARSO([]byte("<arso"), "1"); err == nil || !strings.Contains(err.Error(), "nije valjan XML") {
		t.Errorf("nevaljan XML: %v", err)
	}
	if _, err := CitajARSO([]byte(`<a><postaja sifra="1"><datum>jučer</datum></postaja></a>`), "1"); err == nil || !strings.Contains(err.Error(), "nepoznato vrijeme") {
		t.Errorf("vrijeme: %v", err)
	}
	if _, err := CitajARSO([]byte(probniARSO), "9999"); err == nil || !strings.Contains(err.Error(), "nema postaju 9999") {
		t.Errorf("nema postaje: %v", err)
	}
	if PostajaARSOIzAdrese("https://www.arso.gov.si/xml/vode/hidro_podatki_zadnji.xml#Mura") != "" ||
		PostajaARSOIzAdrese("https://www.arso.gov.si/xml/vode/drugo.xml#2150") != "" ||
		PostajaARSOIzAdrese("https://www.arso.gov.si/xml/vode/hidro_podatki_zadnji.xml?postaja=2150") != "2150" {
		t.Error("šifra iz adrese")
	}
	if _, err := CitajARSOTablicu([]byte("<table></table>")); err == nil {
		t.Error("tablica bez zaglavlja mora biti greška")
	}
	if _, err := CitajARSOTablicu([]byte(`<thead><th>Datum</th><th>Padavine</th></thead><tbody></tbody>`)); err == nil || !strings.Contains(err.Error(), "poznatih stupaca") {
		t.Errorf("nepoznati stupci: %v", err)
	}
	t2 := `<thead><th>Datum</th><th>Pretok</th><th>Vodostaj</th></thead><tbody>` +
		`<tr class="x"></tr>` + // bez ćelija
		`<tr><td>nije datum</td><td>1</td><td>2</td></tr>` +
		`<tr><td>2026-09-27 22:00</td><td>41</td><td>54.6</td></tr>` + // najnovije prvo, kao na stranici
		`<tr><td>27.09.2026 21:00</td><td>40,5</td></tr></tbody>` // vodostaja nema u retku
	r, err := CitajARSOTablicu([]byte(t2))
	if err != nil || len(r) != 2 {
		t.Fatalf("redci: %+v %v", r, err)
	}
	if r[0].LevelCm != nil || *r[0].FlowM3s != 40.5 || *r[1].LevelCm != 55 || *r[1].FlowM3s != 41 {
		t.Errorf("redci: %+v / %+v", r[0], r[1])
	}
}

// eHYD: službeni zapis jedne postaje, s protokom, vodostajem ili
// temperaturom; drugo je greška koja kaže što nije podržano.
func TestEHYDOcitanjaIRubovi(t *testing.T) {
	adresa := "https://gis.lfrz.gv.at/api/geodata/collections/i000501:pegel_aktuell/items/211789"
	odgovor := `{"features":[{"properties":{"wert":342.5,"einheit":"m³/s","zeitpunkt":"2026-10-30T07:00:00+01:00","parameter":"Q"}}]}`
	c, p := klijentIz(stranice(map[string]string{"gis.lfrz.gv.at/api/geodata/collections/i000501:pegel_aktuell/items/211789": odgovor}))
	r, err := EHYD{Client: c}.Ocitanja(context.Background(), adresa)
	if err != nil || len(r) != 1 || *r[0].FlowM3s != 342.5 || !r[0].Kad.Equal(time.Date(2026, 10, 30, 6, 0, 0, 0, time.UTC)) {
		t.Fatalf("protok: %+v %v", r, err)
	}
	if z := p.zahtjevi(); len(z) != 1 || z[0] != "GET "+adresa {
		t.Errorf("zahtjevi %v", z)
	}
	if _, err := (EHYD{Client: c}).Ocitanja(context.Background(), "https://gis.lfrz.gv.at/drugo"); err == nil {
		t.Error("tuđa adresa mora biti greška")
	}
	c404, _ := klijentIz(stranice(nil))
	if _, err := (EHYD{Client: c404}).Ocitanja(context.Background(), adresa); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("404: %v", err)
	}

	for _, c := range []struct{ json, want string }{
		{`nije json`, "nije valjan JSON"},
		{`{"features":[]}`, "nije vratio postaju"},
		{`{"features":[{"properties":{"zeitpunkt":"jučer"}}]}`, "nepoznato vrijeme"},
		{`{"features":[{"properties":{"wert":1,"einheit":"m","zeitpunkt":"2026-10-30T07:00:00Z","parameter":"W"}}]}`, `parametar "W" u jedinici "m"`},
	} {
		if _, err := CitajEHYD([]byte(c.json)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.json, err)
		}
	}
	r, err = CitajEHYD([]byte(`{"features":[{"properties":{"wert":288.6,"einheit":"cm","zeitpunkt":"2026-10-30T07:00:00Z","parameter":"h"}}]}`))
	if err != nil || r[0].LevelCm == nil || *r[0].LevelCm != 288 {
		t.Errorf("vodostaj: %+v %v", r, err)
	}
	r, err = CitajEHYD([]byte(`{"features":[{"properties":{"wert":9.4,"einheit":"°C","zeitpunkt":"2026-10-30T07:00:00Z","parameter":"TW"}}]}`))
	if err != nil || r[0].TempC == nil || *r[0].TempC != 9.4 {
		t.Errorf("temperatura: %+v %v", r, err)
	}
}

// GKD: stranica postaje s tablicom; tablica bez punih sati i adresa bez
// broja postaje su greške, a ponovljeni puni sat (jesenski pomak) ulazi
// jednom.
func TestGKDOcitanjaIRubovi(t *testing.T) {
	adresa := "https://www.gkd.bayern.de/de/fluesse/wasserstand/isar/muenchen-16005701/messwerte/tabelle"
	html := `<tr><td >25.10.2026 02:00 Uhr</td><td class="center">170</td></tr><tr><td >25.10.2026 02:00 Uhr</td><td class="center">171</td></tr><tr><td >25.10.2026 01:00 Uhr</td><td class="center">168</td></tr>`
	c, _ := klijentIz(stranice(map[string]string{"www.gkd.bayern.de/de/fluesse/wasserstand/isar/muenchen-16005701/messwerte/tabelle": html}))
	r, err := GKD{Client: c}.Ocitanja(context.Background(), adresa)
	if err != nil || len(r) != 2 || *r[0].LevelCm != 168 || *r[1].LevelCm != 170 {
		t.Fatalf("redci: %+v %v", r, err)
	}
	if _, err := (GKD{Client: c}).Ocitanja(context.Background(), "https://www.gkd.bayern.de/de/fluesse/abfluss"); err == nil {
		t.Error("adresa bez broja mora biti greška")
	}
	if PostajaGKDIzAdrese("https://www.gkd.bayern.de/de/fluesse/wasserstand/isar/muenchen/messwerte") != "" {
		t.Error("putanja bez broja postaje")
	}
	c404, _ := klijentIz(stranice(nil))
	if _, err := (GKD{Client: c404}).Ocitanja(context.Background(), adresa); err == nil {
		t.Error("404 mora biti greška")
	}
	if _, err := CitajGKD(`<th>Wasserstand [cm]</th><tr><td>25.10.2026 02:15 Uhr</td><td class="center">170</td></tr>`); err == nil || !strings.Contains(err.Error(), "nijedno valjano očitanje") {
		t.Errorf("bez punih sati: %v", err)
	}
	if _, err := CitajGKD(`<p>prazno</p>`); err == nil || !strings.Contains(err.Error(), "na GKD stranici nema tablice") {
		t.Errorf("bez naslova: %v", err)
	}
}

// Hidmet: zamjenska adresa zadržava upit postaje.
func TestHidmetOcitanja(t *testing.T) {
	c, p := klijentIz(stranice(map[string]string{"www.hidmet.gov.rs/latin/osmotreni/nrt_tabela_grafik.php": ispisHidmet}))
	r, err := Hidmet{Client: c}.Ocitanja(context.Background(), AdresaHidmet(42010))
	if err != nil || len(r) != 3 {
		t.Fatalf("redci: %+v %v", r, err)
	}
	if z := p.zahtjevi(); z[0] != "GET https://www.hidmet.gov.rs/latin/osmotreni/nrt_tabela_grafik.php?hm_id=42010&period=7" {
		t.Errorf("zahtjev %v", z)
	}
	c2, p2 := klijentIz(stranice(map[string]string{"lokalno/hidmet": ispisHidmet}))
	if _, err := (Hidmet{Client: c2, Base: "http://lokalno/hidmet"}).Ocitanja(context.Background(), AdresaHidmet(42010)); err != nil {
		t.Fatal(err)
	}
	if z := p2.zahtjevi(); z[0] != "GET http://lokalno/hidmet?hm_id=42010&period=7" {
		t.Errorf("zamjenska adresa: %v", z)
	}
	if _, err := (Hidmet{Client: c}).Ocitanja(context.Background(), "https://www.hidmet.gov.rs/latin/"); err == nil {
		t.Error("adresa bez broja mora biti greška")
	}
	c404, _ := klijentIz(stranice(nil))
	if _, err := (Hidmet{Client: c404}).Ocitanja(context.Background(), AdresaHidmet(1)); err == nil {
		t.Error("404 mora biti greška")
	}
	if _, err := CitajHidmet(`<th>Vodostaj</th>`); err == nil || !strings.Contains(err.Error(), "tablica je prazna") {
		t.Errorf("prazna tablica: %v", err)
	}
}

// PEGELONLINE: JSON mjerenja; nevaljan JSON i niz bez punih sati su greške.
func TestPegelOnlineOcitanja(t *testing.T) {
	const id = "47a3e4f9-4d6f-4d28-9e2e-2f7e3c1a0b11"
	adresa := AdresaPegelOnline(id)
	odgovor := `[{"timestamp":"2026-10-30T07:00:00+01:00","value":412.4},{"timestamp":"2026-10-30T07:15:00+01:00","value":413},{"timestamp":"2026-10-30T06:00:00+01:00","value":410.6}]`
	c, p := klijentIz(stranice(map[string]string{"www.pegelonline.wsv.de/webservices/rest-api/v2/stations/" + id + "/W/measurements.json": odgovor}))
	r, err := PegelOnline{Client: c}.Ocitanja(context.Background(), adresa)
	if err != nil || len(r) != 2 || *r[0].LevelCm != 411 || *r[1].LevelCm != 412 {
		t.Fatalf("redci: %+v %v", r, err)
	}
	if z := p.zahtjevi(); z[0] != "GET "+adresa {
		t.Errorf("zahtjev %v", z)
	}
	if _, err := (PegelOnline{Client: c}).Ocitanja(context.Background(), "https://www.pegelonline.wsv.de/stations/x"); err == nil {
		t.Error("adresa bez UUID-a mora biti greška")
	}
	c404, _ := klijentIz(stranice(nil))
	if _, err := (PegelOnline{Client: c404}).Ocitanja(context.Background(), adresa); err == nil {
		t.Error("404 mora biti greška")
	}
	if _, err := CitajPegelOnline([]byte(`{`)); err == nil || !strings.Contains(err.Error(), "nije valjan JSON") {
		t.Errorf("JSON: %v", err)
	}
}

// SHMU: adresa sa zamjenskom osnovom nosi stranicu i broj postaje.
func TestSHMUOcitanja(t *testing.T) {
	c, p := klijentIz(stranice(map[string]string{"lokalno/sk/": ispisSHMU, "www.shmu.sk/sk/": ispisSHMU}))
	r, err := SHMU{Client: c}.Ocitanja(context.Background(), AdresaSHMU(5140))
	if err != nil || len(r) != 2 {
		t.Fatalf("redci: %+v %v", r, err)
	}
	if _, err := (SHMU{Client: c, Base: "http://lokalno/sk/"}).Ocitanja(context.Background(), AdresaSHMU(5140)); err != nil {
		t.Fatal(err)
	}
	if z := p.zahtjevi(); z[0] != "GET https://www.shmu.sk/sk/?page=765&station_id=5140" || z[1] != "GET http://lokalno/sk/?page=765&station_id=5140" {
		t.Errorf("zahtjevi %v", z)
	}
	if _, err := (SHMU{Client: c}).Ocitanja(context.Background(), "https://www.shmu.sk/sk/?page=765"); err == nil {
		t.Error("adresa bez broja mora biti greška")
	}
	c404, _ := klijentIz(stranice(nil))
	if _, err := (SHMU{Client: c404}).Ocitanja(context.Background(), AdresaSHMU(1)); err == nil {
		t.Error("404 mora biti greška")
	}
}

// Vizugy ide svojim klijentom (njihov međucertifikat), pa se u testu
// zamjenjuje adresa lokalnim poslužiteljem.
func TestVizugyOcitanja(t *testing.T) {
	const oznaka = "16A9B1B0-4C1E-4B5D-8F2A-0A1B2C3D4E5F"
	var upit string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upit = r.URL.RawQuery
		w.WriteHeader(status)
		_, _ = w.Write([]byte(ispisVizugy))
	}))
	t.Cleanup(srv.Close)
	v := Vizugy{Client: &Client{HTTP: srv.Client()}, Base: srv.URL + "/"}
	r, err := v.Ocitanja(context.Background(), AdresaVizugy(strings.ToLower(oznaka)))
	if err != nil || len(r) != 5 {
		t.Fatalf("redci: %+v %v", r, err)
	}
	if upit != "mapModule=OpGrafikon&AllomasVOA="+oznaka+"&mapData=OrasIdosor" {
		t.Errorf("upit %q", upit)
	}
	status = http.StatusServiceUnavailable
	if _, err := v.Ocitanja(context.Background(), AdresaVizugy(oznaka)); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("503: %v", err)
	}
	if _, err := v.Ocitanja(context.Background(), "https://www.vizugy.hu/?mapModule=OpGrafikon"); err == nil {
		t.Error("adresa bez oznake mora biti greška")
	}
	if _, err := (Vizugy{Base: "http://127.0.0.1:1/"}).Ocitanja(context.Background(), AdresaVizugy(oznaka)); err == nil {
		t.Error("nedostupan poslužitelj mora biti greška")
	}
}

// Vizugy polja: kraće polje vremena skraćuje niz, prazne i nečitljive
// vrijednosti i vremena se preskaču, a niz bez ijedne valjane je greška.
func TestVizugyPoljaRubovi(t *testing.T) {
	html := `Vizallas = new Array(10, , x, 13, 14); Idopont = new Array('2026.10.30. 01:00', '2026.10.30. 02:00', '2026.10.30. 03:00', 'nije vrijeme');`
	r, err := CitajVizugy(html)
	if err != nil || len(r) != 1 || *r[0].LevelCm != 10 || r[0].FlowM3s != nil {
		t.Fatalf("redci: %+v %v", r, err)
	}
	if _, err := CitajVizugy(`Vizallas = new Array(, x); Idopont = new Array('a', 'b');`); err == nil || !strings.Contains(err.Error(), "polja su prazna") {
		t.Errorf("prazna polja: %v", err)
	}
	if _, err := CitajVizugy(`Idopont = new Array('2026.10.30. 01:00');`); err == nil || !strings.Contains(err.Error(), "nema polja") {
		t.Errorf("bez vodostaja: %v", err)
	}
	if poljeNavodnika("x", "Vizhozam") != nil || poljeNiza("x", "Vizallas") != nil {
		t.Error("polja kojih nema su nil")
	}
	if _, ok := protokVizugy("-5,0 m3/s"); ok {
		t.Error("negativan protok nije protok")
	}
}

// DanubeHIS: stranica satnih vrijednosti postaje; pola sata, nečitljiv broj
// i stranica bez tablice.
func TestDanubeHISOcitanjaIRubovi(t *testing.T) {
	adresa := AdresaDanubeHIS("hu442522_hydro")
	c, p := klijentIz(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(danubeHISUzorak)) })
	r, err := DanubeHIS{Client: c}.Ocitanja(context.Background(), adresa)
	if err != nil {
		t.Fatal(err)
	}
	// 7 h i 8 h po bečkom ljetnom vremenu, najstarije prvo; protok se preskače
	if len(r) != 2 || *r[0].LevelCm != -36 || !r[0].Kad.Equal(time.Date(2026, 9, 30, 5, 0, 0, 0, time.UTC)) ||
		*r[1].LevelCm != -37 || !r[1].Kad.Equal(time.Date(2026, 9, 30, 6, 0, 0, 0, time.UTC)) || r[0].FlowM3s != nil {
		t.Errorf("redci: %+v", r)
	}
	if _, err := (DanubeHIS{Client: c, Base: "http://lokalno"}).Ocitanja(context.Background(), adresa); err != nil {
		t.Fatal(err)
	}
	if z := p.zahtjevi(); len(z) != 2 || z[0] != "GET https://www.danubehis.org/results/HU442522_HYDRO/h" || z[1] != "GET http://lokalno/results/HU442522_HYDRO/h" {
		t.Errorf("zahtjevi %v", z)
	}
	if _, err := (DanubeHIS{Client: c}).Ocitanja(context.Background(), "https://www.danubehis.org/"); err == nil {
		t.Error("adresa bez oznake mora biti greška")
	}
	c404, _ := klijentIz(stranice(nil))
	if _, err := (DanubeHIS{Client: c404}).Ocitanja(context.Background(), adresa); err == nil {
		t.Error("404 mora biti greška")
	}
	pola := strings.ReplaceAll(danubeHISUzorak, "08:00", "08:30")
	pola = strings.ReplaceAll(pola, "07:00", "07:30")
	if _, err := CitajDanubeHIS(pola); err == nil {
		t.Error("bez punih sati mora biti greška")
	}
}

// NOEL: datoteka preko zadanog klijenta; 404, adresa koja nije njihova i
// predug redak su greške.
func TestNOELOcitanjaRubovi(t *testing.T) {
	adresa := "https://www.noel.gv.at/wasserstand/kidata/stationdata/207241_Wasserstand_3Tage.csv"
	c, _ := klijentIz(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Datum;Wert\n2026-10-30 07:00:00;187.6\n2026-10-30 07:15:00;188\n"))
	})
	r, err := NOEL{Client: c}.Ocitanja(context.Background(), adresa)
	if err != nil || len(r) != 1 || *r[0].LevelCm != 188 || !r[0].Kad.Equal(time.Date(2026, 10, 30, 6, 0, 0, 0, time.UTC)) {
		t.Fatalf("redci: %+v %v", r, err)
	}
	if _, err := (NOEL{Client: c}).Ocitanja(context.Background(), "https://www.noel.gv.at/drugo.csv"); err == nil {
		t.Error("tuđa adresa mora biti greška")
	}
	c404, _ := klijentIz(stranice(nil))
	if _, err := (NOEL{Client: c404}).Ocitanja(context.Background(), adresa); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("404: %v", err)
	}
	dug, _ := klijentIz(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(strings.Repeat("x", 70*1024))) })
	if _, err := (NOEL{Client: dug}).Ocitanja(context.Background(), adresa); err == nil {
		t.Error("predug redak mora biti greška")
	}
	if _, err := (NOEL{Client: c, Base: "http://[::1"}).Ocitanja(context.Background(), adresa); err == nil {
		t.Error("neispravna zamjenska adresa mora biti greška")
	}
}
