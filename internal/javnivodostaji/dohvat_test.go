package javnivodostaji

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"

	"gocop/internal/mletva"
)

// biljeznik pamti adrese zahtjeva i vraća zadani odgovor, bez mreže
type biljeznik struct {
	mu      sync.Mutex
	adrese  []string
	odgovor string
	status  int
	kamo    string // Location za preusmjeravanje
}

func (b *biljeznik) RoundTrip(r *http.Request) (*http.Response, error) {
	b.mu.Lock()
	b.adrese = append(b.adrese, r.URL.String())
	b.mu.Unlock()
	status := b.status
	if status == 0 {
		status = http.StatusOK
	}
	h := http.Header{}
	if b.kamo != "" {
		h.Set("Location", b.kamo)
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(b.odgovor)), Header: h, Request: r}, nil
}

// Adresa letve prepoznaje se samo kad je stvarno na poslužitelju izvora.
// Ime izvora negdje u adresi nije dovoljno.
func TestAdresaLetveSamoSPosluziteljaIzvora(t *testing.T) {
	tude := []string{
		"http://192.168.1.1/?x=shmu.sk&station_id=1",
		"http://127.0.0.1:1160/?vizugy.hu&AllomasVOA=16496059-97AB-11D4-BB62-00508BA24287",
		"http://10.0.0.5:8080/?hidmet.gov.rs&hm_id=1",
		"http://intranet/danubehis.org/results/HU1_HYDRO",
		"https://www.shmu.sk:8443/sk/?page=765&station_id=5140",
		"https://pero@www.hidmet.gov.rs/latin/osmotreni/nrt_tabela_grafik.php?hm_id=42010",
		"ftp://www.vizugy.hu/?AllomasVOA=16496059-97AB-11D4-BB62-00508BA24287",
		"https://shmu.sk.primjer.hr/?station_id=5140",
		"https://pegelonline.wsv.de:8080/webservices/rest-api/v2/stations/a6ee8177-107b-47dd-bcfd-30960ccc6e9c/W/measurements.json",
		"https://www.noel.gv.at:22/wasserstand/kidata/stationdata/207241_Wasserstand_3Tage.csv",
		"https://user@gkd.bayern.de/de/fluesse/wasserstand/donau/passau-ilzstadt-10091008/messwerte",
		"https://www.arso.gov.si:8080/xml/vode/hidro_podatki_zadnji.xml#5078",
	}
	for _, a := range tude {
		if PrepoznataAdresa(a) {
			t.Errorf("prepoznata je adresa izvan poslužitelja izvora: %s", a)
		}
	}
	prave := []string{
		AdresaSHMU(5140),
		"https://shmu.sk/sk/?page=765&station_id=5140",
		AdresaHidmet(42010),
		AdresaVizugy("16496059-97ab-11d4-bb62-00508ba24287"),
		AdresaDanubeHIS("HU442522_HYDRO"),
		"https://www.danubehis.org/results/HU442522_HYDRO",
		AdresaPostaje(Postaja{ID: 424, Sektor: 2}),
		AdresaARSO("5078"),
		AdresaPegelOnline("a6ee8177-107b-47dd-bcfd-30960ccc6e9c"),
		"https://www.noel.gv.at/wasserstand/kidata/stationdata/207241_Wasserstand_3Tage.csv",
		"https://www.gkd.bayern.de/de/fluesse/wasserstand/donau/passau-ilzstadt-10091008/messwerte",
		"https://gis.lfrz.gv.at/api/geodata/i000501/ogc/features/v1/collections/i000501:pegel_aktuell/items/211912",
	}
	for _, a := range prave {
		if !PrepoznataAdresa(a) {
			t.Errorf("nije prepoznata prava adresa: %s", a)
		}
	}
	if a := zaDohvat("http://192.168.1.1/?x=shmu.sk", "shmu.sk"); a != "" {
		t.Errorf("adresa za dohvat izvan poslužitelja izvora: %q", a)
	}
}

// Iz adrese letve uzima se samo oznaka postaje; stranica se čita s
// poznatog poslužitelja, adresom koju program sam sastavi.
func TestStranicaSeCitaSPoznateAdrese(t *testing.T) {
	ctx := context.Background()
	slucajevi := []struct {
		naziv  string
		citaj  func(c *Client) error
		ocekuj string
	}{
		{"shmu", func(c *Client) error {
			_, err := SHMU{Client: c}.Ocitanja(ctx, "http://www.shmu.sk/drugo/mjesto?x=1&station_id=5140")
			return err
		}, AdresaSHMU(5140)},
		{"hidmet", func(c *Client) error {
			_, err := Hidmet{Client: c}.Ocitanja(ctx, "http://hidmet.gov.rs/admin.php?hm_id=42010&x=1")
			return err
		}, AdresaHidmet(42010)},
		{"vizugy", func(c *Client) error {
			_, err := Vizugy{Client: c}.Ocitanja(ctx, "http://vizugy.hu/drugo?AllomasVOA=16496059-97AB-11D4-BB62-00508BA24287")
			return err
		}, AdresaVizugy("16496059-97AB-11D4-BB62-00508BA24287")},
		{"danubehis", func(c *Client) error {
			_, err := DanubeHIS{Client: c}.Ocitanja(ctx, "http://danubehis.org/results/HU442522_HYDRO/drugo")
			return err
		}, AdresaDanubeHIS("HU442522_HYDRO")},
		{"gkd", func(c *Client) error {
			_, err := GKD{Client: c}.Ocitanja(ctx, "http://www.gkd.bayern.de/de/fluesse/wasserstand/donau/passau-10091008/messwerte/tabelle#x")
			return err
		}, "https://www.gkd.bayern.de/de/fluesse/wasserstand/donau/passau-10091008/messwerte/tabelle"},
		{"pegelonline", func(c *Client) error {
			_, err := PegelOnline{Client: c}.Ocitanja(ctx, "http://www.pegelonline.wsv.de/webservices/rest-api/v2/stations/a6ee8177-107b-47dd-bcfd-30960ccc6e9c/W/measurements.json")
			return err
		}, "https://www.pegelonline.wsv.de/webservices/rest-api/v2/stations/a6ee8177-107b-47dd-bcfd-30960ccc6e9c/W/measurements.json"},
		{"ehyd", func(c *Client) error {
			_, err := EHYD{Client: c}.Ocitanja(ctx, "http://gis.lfrz.gv.at/api/collections/i000501:pegel_aktuell/items/211912")
			return err
		}, "https://gis.lfrz.gv.at/api/collections/i000501:pegel_aktuell/items/211912"},
		{"noel", func(c *Client) error {
			_, err := NOEL{Client: c}.Ocitanja(ctx, "http://www.noel.gv.at/wasserstand/kidata/stationdata/207241_Wasserstand_3Tage.csv#x")
			return err
		}, "https://www.noel.gv.at/wasserstand/kidata/stationdata/207241_Wasserstand_3Tage.csv"},
		{"arso", func(c *Client) error {
			_, err := ARSO{Client: c, BaseTablice: "https://www.arso.gov.si/nema/%s"}.Ocitanja(ctx, "http://www.arso.gov.si/xml/vode/hidro_podatki_zadnji.xml#5078")
			return err
		}, "https://www.arso.gov.si/xml/vode/hidro_podatki_zadnji.xml"},
	}
	for _, s := range slucajevi {
		b := &biljeznik{odgovor: "<html></html>"}
		_ = s.citaj(&Client{HTTP: &http.Client{Transport: b}}) // prazna stranica javlja grešku; ovdje se gleda samo adresa
		if len(b.adrese) == 0 || b.adrese[len(b.adrese)-1] != s.ocekuj {
			t.Errorf("%s: čitano s %v, očekivano %s", s.naziv, b.adrese, s.ocekuj)
		}
	}
}

// Zadani klijent ne spaja se na lokalnu adresu, ni kad bi ju izvor vratio.
func TestZadaniKlijentNeIdeNaLokalnuAdresu(t *testing.T) {
	stiglo := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { stiglo = true }))
	defer srv.Close()
	_, err := SHMU{Client: &Client{}, Base: srv.URL}.Ocitanja(context.Background(), AdresaSHMU(5140))
	if !errors.Is(err, errLokalnaAdresa) {
		t.Errorf("greška %v, očekivana lokalna adresa", err)
	}
	if stiglo {
		t.Error("zahtjev je stigao na lokalnu adresu")
	}
	_, err = NOEL{Base: srv.URL + "/kidata/stationdata/1_Wasserstand_3Tage.csv"}.Ocitanja(context.Background(),
		"https://www.noel.gv.at/wasserstand/kidata/stationdata/1_Wasserstand_3Tage.csv")
	if !errors.Is(err, errLokalnaAdresa) || stiglo {
		t.Errorf("noel.gv.at: greška %v, zahtjev stigao %v", err, stiglo)
	}
	_, err = Vizugy{Base: srv.URL}.Ocitanja(context.Background(), AdresaVizugy("16496059-97AB-11D4-BB62-00508BA24287"))
	if !errors.Is(err, errLokalnaAdresa) || stiglo {
		t.Errorf("vizugy.hu: greška %v, zahtjev stigao %v", err, stiglo)
	}
}

// Zadani klijent slijedi preusmjeravanje samo unutar istog poslužitelja.
func TestPreusmjeravanjeSamoNaIstiPosluzitelj(t *testing.T) {
	for _, k := range []*http.Client{zadaniKlijent(), noelKlijent(), vizugyKlijent()} {
		if k.Timeout <= 0 || k.CheckRedirect == nil {
			t.Fatalf("klijent bez roka ili bez provjere preusmjeravanja: %+v", k)
		}
		b := &biljeznik{status: http.StatusFound, kamo: "https://127.0.0.1/"}
		kopija := *k
		kopija.Transport = b
		req, _ := http.NewRequest(http.MethodGet, AdresaSHMU(5140), nil) // adresa je stalna i ispravna
		resp, err := kopija.Do(req)
		if err == nil {
			resp.Body.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "drugi poslužitelj") || len(b.adrese) != 1 {
			t.Errorf("preusmjeravanje na drugi poslužitelj: greška %v, zahtjevi %v", err, b.adrese)
		}
	}
	b := &biljeznik{status: http.StatusMovedPermanently, kamo: "https://shmu.sk/sk/?page=765&station_id=5140"}
	kopija := *zadaniKlijent()
	kopija.Transport = &prviPreusmjeri{b: b}
	resp, err := kopija.Get(AdresaSHMU(5140))
	if err != nil {
		t.Fatalf("preusmjeravanje bez www ne smije pasti: %v", err)
	}
	resp.Body.Close()
}

// prviPreusmjeri na prvi zahtjev vrati preusmjeravanje, a dalje 200
type prviPreusmjeri struct {
	b   *biljeznik
	bio bool
}

func (p *prviPreusmjeri) RoundTrip(r *http.Request) (*http.Response, error) {
	if !p.bio {
		p.bio = true
		return p.b.RoundTrip(r)
	}
	return (&biljeznik{odgovor: "ok"}).RoundTrip(r)
}

func TestIstiPosluzitelj(t *testing.T) {
	zahtjev := func(a string) *http.Request {
		r, _ := http.NewRequest(http.MethodGet, a, nil) // adrese u testu su ispravne
		return r
	}
	prvi := []*http.Request{zahtjev("https://www.noel.gv.at/a")}
	slucajevi := map[string]bool{
		"https://www.noel.gv.at/b":     true,
		"https://noel.gv.at/b":         true,
		"https://data.noel.gv.at/b":    true,
		"http://www.noel.gv.at/b":      false,
		"https://gv.at/b":              false,
		"https://www.noel.gv.at:8443/": false,
		"https://primjer.hr/":          false,
	}
	for a, smije := range slucajevi {
		if err := istiPosluzitelj(zahtjev(a), prvi); (err == nil) != smije {
			t.Errorf("%s: greška %v, smije %v", a, err, smije)
		}
	}
	puno := make([]*http.Request, 10)
	for i := range puno {
		puno[i] = prvi[0]
	}
	if err := istiPosluzitelj(zahtjev("https://www.noel.gv.at/b"), puno); err == nil {
		t.Error("deset preusmjeravanja mora javiti grešku")
	}
}

func TestLokalnaAdresa(t *testing.T) {
	lokalne := []string{"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.110.205.90",
		"0.0.0.0", "224.0.0.1", "255.255.255.255", "::1", "fe80::1", "fc00::1", "::ffff:127.0.0.1", "::ffff:192.168.0.1"}
	for _, a := range lokalne {
		if !lokalnaAdresa(netip.MustParseAddr(a)) {
			t.Errorf("%s nije prepoznata kao lokalna", a)
		}
	}
	for _, a := range []string{"194.152.213.69", "8.8.8.8", "2a00:1450:4001::1"} {
		if lokalnaAdresa(netip.MustParseAddr(a)) {
			t.Errorf("%s je javna, a prepoznata je kao lokalna", a)
		}
	}
	if !lokalnaAdresa(netip.Addr{}) {
		t.Error("prazna adresa mora biti odbijena")
	}
	if err := provjeriSpoj("tcp", "nije-adresa", nil); err == nil {
		t.Error("neispravna adresa spoja mora javiti grešku")
	}
	if err := provjeriSpoj("tcp", "194.152.213.69:443", nil); err != nil {
		t.Errorf("javna adresa: %v", err)
	}
}

// Posrednik iz okoline i poslužitelji Hrvatskih voda smiju biti u lokalnoj
// mreži; sve ostalo ide kroz provjeru.
func TestSpajacPosrednikIHrvatskeVode(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	s := noviSpajac()
	s.posrednik = func(*http.Request) (*url.URL, error) { return nil, nil }
	if _, err := s.DialContext(context.Background(), "tcp", l.Addr().String()); !errors.Is(err, errLokalnaAdresa) {
		t.Errorf("spoj na lokalnu adresu: %v", err)
	}
	if _, err := s.DialContext(context.Background(), "tcp", "bez-porta"); err == nil {
		t.Error("adresa bez porta mora javiti grešku")
	}
	s.posrednik = func(*http.Request) (*url.URL, error) { return url.Parse("http://" + l.Addr().String()) }
	c, err := s.DialContext(context.Background(), "tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("spoj na posrednika: %v", err)
	}
	c.Close()
	if s.cuva("mvodostaji.voda.hr") || s.cuva("vodostaji.voda.hr") {
		t.Error("poslužitelji Hrvatskih voda ne idu kroz provjeru lokalne adrese")
	}
	if !s.cuva("www.shmu.sk") || !s.cuva("voda.hr.primjer.hr") {
		t.Error("tuđi poslužitelj mora ići kroz provjeru")
	}
	s.posrednik = func(*http.Request) (*url.URL, error) { return nil, fmt.Errorf("krivi posrednik") }
	if !s.cuva("127.0.0.1") {
		t.Error("bez posrednika lokalna adresa ide kroz provjeru")
	}
}

// Odgovor noel.gv.at čita se do granice, i kad stigne sažet.
func TestNOELOdgovorDoGranice(t *testing.T) {
	var sazeto bytes.Buffer
	gz := gzip.NewWriter(&sazeto)
	redak := []byte("2026-10-20 12:00:00;150\n")
	for n := 0; n <= najvecaDatotekaNOEL; n += len(redak) {
		_, _ = gz.Write(redak) // pisanje u memoriju ne pada
	}
	gz.Close() // pisanje u memoriju ne pada
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Write(sazeto.Bytes()) // test ne provjerava slanje
	}))
	defer srv.Close()
	adresa := "https://www.noel.gv.at/wasserstand/kidata/stationdata/207241_Wasserstand_3Tage.csv"
	_, err := NOEL{Base: srv.URL, Client: &Client{HTTP: srv.Client()}}.Ocitanja(context.Background(), adresa)
	if err == nil || !strings.Contains(err.Error(), "veći od 4 MB") {
		t.Errorf("prevelik odgovor: %v", err)
	}
	if _, err := citajDoGranice(errCitac{}, 10); err == nil {
		t.Error("greška čitanja mora se javiti")
	}
	b, err := citajDoGranice(strings.NewReader("12345"), 5)
	if err != nil || string(b) != "12345" {
		t.Errorf("odgovor točno do granice: %q %v", b, err)
	}
}

type errCitac struct{}

func (errCitac) Read([]byte) (int, error) { return 0, errors.New("prekinuto") }

// Adrese Hrvatskih voda, mletve i HydroViewa prepoznaju se samo na točno tim
// poslužiteljima. Drugi poslužitelj koji ime izvora nosi u upitu ili putu
// nije prepoznat.
func TestAdresaHrvatskihVodaSamoSNjihovihPosluzitelja(t *testing.T) {
	tude := []string{
		"https://primjer.test/prijava?mletva.voda.hr&postajaID=7",
		"https://primjer.test/?vodostaji.voda.hr&postajaID=424",
		"http://192.168.1.1/?vodostaji.voda.hr&postajaID=5",
		"https://mletva.voda.hr.primjer.test/Home/PregledProtokaPostaje?postajaID=662",
		"https://primjer.test/hydroview#/site/AAAAAAAAAAAAAAAAAAAA/latest",
		"https://primjer.test/?hdv.voda.hr&site_id=AAAAAAAAAAAAAAAAAAAA",
		"https://pero@hdv.voda.hr/#/site/AAAAAAAAAAAAAAAAAAAA/latest",
		"https://mletva.voda.hr:8443/Home/PregledProtokaPostaje?postajaID=662",
		"https://mvodostaji.voda.hr/Home/PregledVodostaja?sektorID=2",
		"https://hdv.voda.hr/#/site/",
	}
	for _, a := range tude {
		if PrepoznataAdresa(a) || Poveznica(a) != "" {
			t.Errorf("prepoznata je adresa izvan poslužitelja izvora: %s", a)
		}
	}
	prave := map[string]string{
		AdresaPostaje(Postaja{ID: 424, Sektor: 2}):                             "vodostaji.voda.hr",
		"https://vodostaji.voda.hr/Home/PregledVodostajaPostaje?postajaID=426": "vodostaji.voda.hr",
		AdresaMLetve(1, 662):                    mletva.Podrijetlo,
		AdresaHidroView("AAAAAAAAAAAAAAAAAAAA"): PodrijetloHidroView,
		"https://hdv.voda.hr/api/v1/installed_equipment/get?site_id=AAAAAAAAAAAAAAAAAAAA": PodrijetloHidroView,
	}
	u := &Uvoznik{Izvori: sviIzvori(&Client{})}
	for a, naziv := range prave {
		iz := u.IzvorZa(a)
		if iz == nil || iz.Naziv() != naziv {
			t.Errorf("%s: izvor %v, a treba %s", a, iz, naziv)
		}
		if p := Poveznica(a); p != a {
			t.Errorf("poveznica za %s je %q", a, p)
		}
	}
}

// Adresa upisana bez https:// čita se kao da ga ima, pa letva upisana tako
// prije i dalje ima svoj čitač.
func TestAdresaBezSheme(t *testing.T) {
	for _, a := range []string{
		"www.vizugy.hu/?mapModule=OpGrafikon&AllomasVOA=16496059-97AB-11D4-BB62-00508BA24287",
		"mvodostaji.voda.hr/Home/PregledVodostajaPostaje?sektorID=2&bpID=0&postajaID=424",
		"//www.shmu.sk/sk/?page=765&station_id=5140",
	} {
		if !PrepoznataAdresa(a) {
			t.Errorf("adresa bez sheme nije prepoznata: %s", a)
		}
		if p := Poveznica(a); !strings.HasPrefix(p, "https://") {
			t.Errorf("poveznica za %s je %q", a, p)
		}
	}
	if PrepoznataAdresa("") || Poveznica("") != "" {
		t.Error("prazna adresa je prepoznata")
	}
}

// Preusmjeravanje na zadani port sheme vrijedi kao i bez porta; drugi port
// se ne slijedi.
func TestPreusmjeravanjeZadaniPort(t *testing.T) {
	adresa := func(s string) *url.URL {
		u, err := url.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	for _, c := range []struct {
		prvi, novi string
		smije      bool
	}{
		{"https://www.vizugy.hu/", "https://www.vizugy.hu:443/drugo", true},
		{"http://www.vizugy.hu/", "https://vizugy.hu/", true},
		{"http://www.vizugy.hu:80/", "https://vizugy.hu:443/", true},
		{"https://www.vizugy.hu/", "https://www.vizugy.hu:8443/", false},
		{"http://www.vizugy.hu/", "http://www.vizugy.hu:443/", false},
	} {
		if got := srodanPosluzitelj(adresa(c.prvi), adresa(c.novi)); got != c.smije {
			t.Errorf("%s → %s: %v", c.prvi, c.novi, got)
		}
	}
}
