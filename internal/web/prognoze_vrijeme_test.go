package web

import (
	"context"
	"database/sql"
	stdhtml "html"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gocop/internal/dhmz"
	"gocop/internal/kisomjeri"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/prognoza"
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"
)

// Svaka županija pripada jednoj od šest regija, a glavno mjesto regije leži
// u županiji te regije: inače bi čip regije pokazao vrijeme druge regije.
func TestRegijeVremenaIZupanije(t *testing.T) {
	if len(regijeVremena) != 6 {
		t.Fatalf("regija: %d", len(regijeVremena))
	}
	for _, z := range dhmz.Zupanije {
		if _, ok := regijaVremena(z.Regija); !ok {
			t.Errorf("%s: regija %q nije među šest", z.Naziv, z.Regija)
		}
	}
	zupanije := map[string]string{"istocna": "Osječko-baranjska", "sredisnja": "Grad Zagreb", "gorska": "Ličko-senjska",
		"sjjadran": "Primorsko-goranska", "istra": "Istarska", "dalmacija": "Splitsko-dalmatinska"}
	for _, r := range regijeVremena {
		m := mjestoU(r.Mjesto, r.Lat, r.Lon)
		if m.Regija.Kljuc != r.Kljuc || m.Zupanija.Naziv != zupanije[r.Kljuc] {
			t.Errorf("%s: %s je u županiji %s, regija %s", r.Naziv, r.Mjesto, m.Zupanija.Naziv, m.Regija.Kljuc)
		}
		if dhmz.NazivRegije[r.Kljuc] == "" {
			t.Errorf("%s: DHMZ nema regije %q", r.Naziv, r.Kljuc)
		}
	}
	if _, ok := regijaVremena("marsovac"); ok {
		t.Error("nepoznata regija postoji")
	}
}

// izmisljenaPodrucja su dva ureda s koordinatama i jedan bez njih
func izmisljenaPodrucja() []models.Area {
	return []models.Area{
		{ID: 7, Name: "Mali sliv Proba", Latitude: 45.33, Longitude: 14.44},
		{ID: 8, Name: "Mali sliv Bez točke"},
		{ID: 9, Name: "Mali sliv Jug", Latitude: 43.51, Longitude: 16.44},
	}
}

// Mjesto iz adrese: regija vodi na glavno mjesto, područje na ured, lat i
// lon na lokaciju preglednika; nepoznato daje zadano (Osijek, istočna).
func TestMjestoVremenaIzAdrese(t *testing.T) {
	h := &VrijemeHandler{}
	pod := izmisljenaPodrucja()
	slucajevi := []struct {
		upit, naziv, regija, zupanija, dio string
		lokacija                           bool
		podrucje                           int
	}{
		{"", "Osijek", "istocna", "Osječko-baranjska županija", "", false, 0},
		{"?regija=dalmacija", "Split", "dalmacija", "Splitsko-dalmatinska županija", "regija=dalmacija", false, 0},
		{"?regija=sredisnja", "Zagreb", "sredisnja", "Grad Zagreb", "regija=sredisnja", false, 0},
		{"?regija=marsovac", "Osijek", "istocna", "Osječko-baranjska županija", "", false, 0},
		{"?podrucje=7", "Mali sliv Proba", "sjjadran", "Primorsko-goranska županija", "podrucje=7", false, 7},
		{"?podrucje=8", "Osijek", "istocna", "Osječko-baranjska županija", "", false, 0},
		{"?podrucje=99", "Osijek", "istocna", "Osječko-baranjska županija", "", false, 0},
		{"?podrucje=sedam", "Osijek", "istocna", "Osječko-baranjska županija", "", false, 0},
		{"?lat=45.24&lon=13.94", "vaša lokacija", "istra", "Istarska županija", "lat=45.24&lon=13.94", true, 0},
		{"?lat=55&lon=13.94", "Osijek", "istocna", "Osječko-baranjska županija", "", false, 0},
	}
	for _, s := range slucajevi {
		m := h.mjestoVremena(httptest.NewRequest(http.MethodGet, "/prognoze/vrijeme"+s.upit, nil), pod)
		if m.Naziv != s.naziv || m.Regija.Kljuc != s.regija || m.NaslovZupanije() != s.zupanija ||
			m.upit.Encode() != s.dio || m.Lokacija != s.lokacija || m.Podrucje != s.podrucje {
			t.Errorf("%q: %+v, županija %q, dio %q", s.upit, m, m.NaslovZupanije(), m.upit.Encode())
		}
	}
	if m := h.mjestoVremena(httptest.NewRequest(http.MethodGet, "/prognoze/vrijeme?lat=45.5&lon=18.7", nil), nil); m.NaslovMjesta() != "Vaša lokacija" {
		t.Errorf("naslov lokacije: %q", m.NaslovMjesta())
	}
}

// predlozakVremena je stranica Prognoze › Vrijeme sa svojim dijelom
func predlozakVremena(t *testing.T) *template.Template {
	t.Helper()
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	return template.Must(template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("prognoze_vrijeme.html")...))
}

// zahtjevAdmina je zahtjev glavnog administratora
func zahtjevAdmina(adresa string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, adresa, nil)
	ctx := context.WithValue(r.Context(), contextKeyUser, &models.User{FullName: "Ivka Proba"})
	ctx = context.WithValue(ctx, contextKeyPerms, &models.UserPermissions{IsGlobalAdmin: true})
	return r.WithContext(ctx)
}

// Stranica: kartica Vrijeme odabrana, šest čipova i izbornik regije, mjesto
// sa županijom, izbor ureda i okvir za dio s istim upitom.
func TestStranicaVremena(t *testing.T) {
	h := &VrijemePrognozaHandler{tmpl: predlozakVremena(t),
		glava: func(r *http.Request) PrognozePageData {
			return PrognozePageData{CurrentUser: &models.User{FullName: "Ivka Proba"}, ActiveNav: "prognoze"}
		},
		podrucja: func(context.Context) ([]models.Area, error) { return izmisljenaPodrucja(), nil }}
	w := httptest.NewRecorder()
	h.ShowVrijeme(w, zahtjevAdmina("/prognoze/vrijeme?podrucje=9"))
	s := stdhtml.UnescapeString(w.Body.String())
	for _, trazi := range []string{
		`<a href="/prognoze/vrijeme" class="prog-odjel" aria-current="page">Vrijeme</a>`,
		`<a href="/prognoze" class="prog-odjel">Vodostaji</a>`,
		`href="/prognoze/vrijeme?regija=dalmacija" class="uzd-cip prog-cip prog-cip-vodotok" aria-current="true">Dalmacija</a>`,
		`<option value="dalmacija" selected>Dalmacija</option>`, `<span class="prog-polje-natpis">Regija</span>`,
		"Mali sliv Jug<span class=\"pv-samo-siroko\"> · Splitsko-dalmatinska županija</span>",
		"Moja lokacija", "<span>Promijeni<span class=\"pv-samo-siroko\"> mjesto</span></span>",
		`<option value="9" selected>Mali sliv Jug</option>`, `<option value="7">Mali sliv Proba</option>`,
		`data-url="/prognoze/vrijeme/dio?podrucje=9"`,
	} {
		if !strings.Contains(s, trazi) {
			t.Errorf("stranica nema %q", trazi)
		}
	}
	if strings.Contains(s, "Mali sliv Bez točke") || strings.Count(s, "uzd-cip prog-cip") != 6 {
		t.Error("ured bez koordinata u izboru ili krivi broj čipova")
	}

	// županija iz popisa upozorenja drugdje: naslov je samo županija, regija
	// njezina; dolazak s Naslovne prenosi otvoren popis u dio
	w = httptest.NewRecorder()
	h.ShowVrijeme(w, zahtjevAdmina("/prognoze/vrijeme?zupanija=LS&drugdje=1"))
	s = stdhtml.UnescapeString(w.Body.String())
	for _, trazi := range []string{`<h2 class="pv-mjesto-naslov">Ličko-senjska županija</h2>`, `aria-current="true">Gorska Hrvatska</a>`,
		`data-url="/prognoze/vrijeme/dio?drugdje=1&zupanija=LS"`} {
		if !strings.Contains(s, trazi) {
			t.Errorf("županija: nema %q", trazi)
		}
	}

	// bez registra i bez rukovatelja vremena: zadano mjesto, izbor ureda u izradi
	h.podrucja, h.glava = nil, nil
	w = httptest.NewRecorder()
	h.ShowVrijeme(w, zahtjevAdmina("/prognoze/vrijeme"))
	s = w.Body.String()
	if !strings.Contains(s, `data-url="/prognoze/vrijeme/dio"`) || !strings.Contains(s, "U izradi: mjesto se zasad bira regijom ili lokacijom.") ||
		!strings.Contains(s, `aria-current="true">Istočna Hrvatska</a>`) {
		t.Errorf("zadano: %s", s)
	}
	h.tmpl = template.Must(template.New("prazan").Parse(`{{define "drugo"}}{{end}}`))
	w = httptest.NewRecorder()
	h.ShowVrijeme(w, zahtjevAdmina("/prognoze/vrijeme"))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("predložak bez stranice: %d", w.Code)
	}
}

// Kartica Vodostaji na /prognoze je odabrana, a Vrijeme je poveznica.
func TestKarticeNaPrognozama(t *testing.T) {
	s := iscrtaj(t, "prognoze.html", PrognozePageData{CurrentUser: &models.User{FullName: "Ivka Proba"}, ActiveNav: "prognoze"})
	if !strings.Contains(s, `<a href="/prognoze" class="prog-odjel" aria-current="page">Vodostaji</a>`) ||
		!strings.Contains(s, `<a href="/prognoze/vrijeme" class="prog-odjel">Vrijeme</a>`) {
		t.Errorf("kartice: %s", s)
	}
}

// Naslovna: kartica Vrijeme vodi na Prognoze › Vrijeme.
func TestNaslovnaVrijemeVodiNaPrognoze(t *testing.T) {
	k := karticaVremena(PlocaVremena{Mjesto: "Osijek"})
	if k.Poveznica != "Prognoze" || k.URL != "/prognoze/vrijeme" {
		t.Errorf("poveznica: %q %q", k.Poveznica, k.URL)
	}
}

// dhmzProba je lažni DHMZ: postaja Osijek-Čepin, narančasto upozorenje za
// Osječko-baranjsku, prognoza istočne Hrvatske
func dhmzProba(t *testing.T, upozorenje string) *dhmz.Klijent {
	t.Helper()
	od := time.Now().Add(time.Hour).Truncate(time.Hour).In(dhmz.Zagreb)
	do := od.Add(6 * time.Hour)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v":
			w.Write([]byte(`<Hrvatska><DatumTermin><Datum>` + time.Now().In(dhmz.Zagreb).Format("02.01.2006") + `</Datum><Termin>8</Termin></DatumTermin>
<Grad><GradIme>RC Osijek-Čepin</GradIme><Lat>45.503</Lat><Lon>18.561</Lon><Podatci><Temp>12.0</Temp><Vlaga>88</Vlaga><Tlak>1012.4</Tlak><VjetarSmjer>N</VjetarSmjer><VjetarBrzina>5.6</VjetarBrzina><Vrijeme>slaba kiša</Vrijeme></Podatci></Grad></Hrvatska>`))
		case "/cap":
			w.Write([]byte(`<alert><info><language>hr</language><event>` + upozorenje + `</event><description>do 25 mm</description>
<onset>` + od.Format("2006-01-02 15:04:05-07:00") + `</onset><expires>` + do.Format("2006-01-02 15:04:05-07:00") + `</expires>
<parameter><valueName>awareness_level</valueName><value>3; orange; Severe</value></parameter><area><areaDesc>Osječko-baranjska</areaDesc></area><area><areaDesc>Zadarska</areaDesc></area></info></alert>`))
		case "/r":
			w.Write([]byte(`<regije_danas><datum>26.09.2026.</datum><istocna>Oblačno s kišom.</istocna></regije_danas>`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &dhmz.Klijent{Adrese: map[string]string{dhmz.AdresaVrijeme: srv.URL + "/v",
		dhmz.AdresaUpozorenjaDanas: srv.URL + "/cap", dhmz.AdresaUpozorenjaSutra: srv.URL + "/nema",
		dhmz.AdresaRegije: srv.URL + "/r", dhmz.AdresaBilten: srv.URL + "/nema"}}
}

// kisaRegije su tri međusliva: dva istočna (jedan iznad praga) i jedan
// slovenski, koji istočnoj regiji ne pripada
func kisaRegije() kisaSlivovaPodaci {
	kad := time.Date(2026, 11, 9, 7, 0, 0, 0, models.Zagreb) // ponedjeljak
	return kisaSlivovaPodaci{Slivovi: []SlivUKisi{
		{StanjeSliva: kisaStanje("A", 40, 10, 0, ""), Naziv: "Drava iznad Borla"},
		{StanjeSliva: kisaStanje("E", 3.2, 21, 0, ""), Naziv: "Terezino Polje → Donji Miholjac"},
		{StanjeSliva: kisaStanje("G", 18, 32, 2, "toliko padne u prosjeku jednom godišnje"), Naziv: "Belišće → Osijek", Klasa: "kisa-narancasto",
			Letve: []PorastLetve{{Naziv: "Osijek", Cm: 45, Dan: 3, Ima: true, Kad: kad}, {Naziv: "Belišće", Cm: 2, Dan: 1, Ima: true}}},
	}}
}

func kisaStanje(sliv string, palo, dolazi float64, razina int, ucestalost string) prognoza.StanjeSliva {
	return prognoza.StanjeSliva{Sliv: sliv, Palo24: palo, Dolazi48: dolazi, Ima: true, Razina: razina, Ucestalost: ucestalost}
}

// Dio za Osijek: upozorenje crtom i natpisom, Sada s vjetrom u km/h,
// kišomjer, samo međuslivovi regije i prognoza s danima.
func TestDioVremena(t *testing.T) {
	o := novaKisaOkolina(t)
	o.tocka("stv", models.KisomjerStvarni, 0.02, true)
	o.tocka("izv", models.KisomjerIzvedeni, 0.05, true)
	sada := stabilanSat()
	o.mjeri(mjerenje("stv", sada.Add(-30*time.Minute).Truncate(time.Second).UTC(), 1, 0.4), mjerenje("stv", sada.Add(5*time.Hour), 24, 6.2),
		mjerenje("stv", ponocDana(sada).UTC(), 24, 1.8))
	ponoc := ponocDana(sada)
	for s := ponoc.Unix() / 3600; s < ponoc.AddDate(0, 0, 5).Unix()/3600; s++ {
		if _, err := o.obo.Exec(`INSERT INTO satne (kisomjer, sat, oborina, snijeg, temperatura, prognoza, preuzeto) VALUES ('izv', ?, 0.5, 0, ?, 1, 0)`,
			s, float64(s%24)/2); err != nil {
			t.Fatal(err)
		}
	}
	o.h.dhmz = dhmzProba(t, "Narančasto upozorenje za kišu")
	h := &VrijemePrognozaHandler{tmpl: predlozakVremena(t), vrijeme: func() *VrijemeHandler { return o.h },
		kisa: func(context.Context) kisaSlivovaPodaci { return kisaRegije() }, sada: func() time.Time { return sada }}
	w := httptest.NewRecorder()
	h.ShowDio(w, zahtjevAdmina("/prognoze/vrijeme/dio"))
	s := stdhtml.UnescapeString(w.Body.String())
	for _, trazi := range []string{
		`class="pv-upozorenje dhmz-3"`, "DHMZ · Narančasto upozorenje za kišu", "do 25 mm", "pročitano u ",
		`<details class="pv-kartica pv-drugdje">`, "Drugdje u Hrvatskoj · 1 upozorenje u 1 području", "najviše narančasto: kiša",
		`<li class="pv-drugdje-podrucje dhmz-3">`, `href="/prognoze/vrijeme?zupanija=ZD"`, ">Zadarska<", "narančasto: kiša",
		"Sada · postaja Osijek-Čepin, 12 km", "12,0 °C", "slaba kiša, sjeverni vjetar 20 km/h", "vlaga 88 % · tlak 1.012 hPa · u 08:00",
		">Osijek-Čepin · 08:00<", ">Kiša · kišomjer</span>", ">Kišomjer</span> Točka stv, 2 km", "<dt>zadnji sat</dt><dd>0,4 mm</dd>", "<dt>danas od ponoći</dt><dd>6,2 mm</dd>", "<dt>jučer</dt><dd>1,8 mm</dd>",
		"javljeno u ", "· pljusak",
		"Terezino Polje → Donji Miholjac", "3 mm", "21 mm", ">3 · 21 mm<", "palo 24 h · očekivano 48 h", "uobičajeno", `<tr class="kisa-narancasto">`, "jednom godišnje", "Osijek +45 cm u pon.",
		"Prognoza za istočnu Hrvatsku", "DHMZ, 26. 9.", "Oblačno s kišom.", "Danas", "kiša 12 mm", "Po danima: Open-Meteo za točku Točka izv, 6 km.",
	} {
		if !strings.Contains(s, trazi) {
			t.Errorf("dio nema %q", trazi)
		}
	}
	for _, ne := range []string{"Drava iznad Borla", "Belišće +2", "Nema podataka", "vrijeme-orange", "badge"} {
		if strings.Contains(s, ne) {
			t.Errorf("dio ima %q", ne)
		}
	}
	if strings.Count(s, `class="pv-dan"`) != 5 {
		t.Errorf("dana: %d", strings.Count(s, `class="pv-dan"`))
	}
}

// Prazna stanja: DHMZ ne odgovara, kišomjera ni kiše po slivovima nema, a
// regija nema međuslivova. Svaki blok kaže zašto, nijedan nije gola „Nema
// podataka”.
func TestDioVremenaPrazno(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	nema := &dhmz.Klijent{Adrese: map[string]string{dhmz.AdresaVrijeme: srv.URL, dhmz.AdresaUpozorenjaDanas: srv.URL,
		dhmz.AdresaUpozorenjaSutra: srv.URL, dhmz.AdresaRegije: srv.URL, dhmz.AdresaBilten: srv.URL}}
	h := &VrijemePrognozaHandler{tmpl: predlozakVremena(t), vrijeme: func() *VrijemeHandler { return &VrijemeHandler{dhmz: nema} }}
	provjeri := func(upit string, traze ...string) {
		t.Helper()
		w := httptest.NewRecorder()
		h.ShowDio(w, zahtjevAdmina("/prognoze/vrijeme/dio"+upit))
		s := stdhtml.UnescapeString(w.Body.String())
		for _, trazi := range traze {
			if !strings.Contains(s, trazi) {
				t.Errorf("%s: dio nema %q", upit, trazi)
			}
		}
		if strings.Contains(s, "Nema podataka") {
			t.Errorf("%s: gola „Nema podataka”", upit)
		}
	}
	provjeri("?regija=gorska", "Upozorenja DHMZ-a trenutačno nisu dostupna.", "Podaci postaje DHMZ-a trenutačno nisu dostupni.",
		"Podaci kišomjera trenutačno nisu dostupni.", "U izradi: kiša po slivovima zasad se računa samo za slivove Drave, Mure i Dunava.",
		"Prognoza za gorsku Hrvatsku", "Prognoza DHMZ-a trenutačno nije dostupna.", "Prognoza po danima trenutačno nije dostupna.",
		`class="kartica-prazna nedostupno"`, `class="kartica-prazna izrada"`)
	// Osijek je u slivu Drave: bez izvora kiše dani nisu „samo za slivove”,
	// nego trenutačno nedostupni
	provjeri("", "Podaci o kiši po slivovima trenutačno nisu dostupni.", "Prognoza po danima trenutačno nije dostupna.")

	// izvor kiše po slivovima javi grešku; rukovatelja vremena nema
	h.vrijeme = nil
	h.kisa = func(context.Context) kisaSlivovaPodaci {
		return kisaSlivovaPodaci{Greska: "Podaci o kiši po slivovima trenutačno nisu dostupni."}
	}
	provjeri("", "Podaci o kiši po slivovima trenutačno nisu dostupni.", "Upozorenja DHMZ-a trenutačno nisu dostupna.")
}

// Bez upozorenja za županiju: potvrda s razdobljem bez upozorenja.
func TestUpozorenjaDijelaBezUpozorenja(t *testing.T) {
	sada := time.Date(2026, 11, 5, 10, 0, 0, 0, models.Zagreb)
	m := mjestoU("Osijek", osijekLat, osijekLon)
	u, pr := upozorenjaDijela(PlocaVremena{ZelenoDo: sada.Add(14 * time.Hour)}, m, sada)
	if u != nil || pr == nil || pr.Vrsta != PraznoGotovo || pr.Tekst != "Osječko-baranjska županija: DHMZ nema upozorenja do sutra u 0 h." {
		t.Errorf("bez upozorenja: %+v %+v", u, pr)
	}
	if _, pr := upozorenjaDijela(PlocaVremena{}, m, sada); pr.Tekst != "Osječko-baranjska županija: DHMZ nema upozorenja." {
		t.Errorf("bez razdoblja: %q", pr.Tekst)
	}
	u, _ = upozorenjaDijela(PlocaVremena{Upozorenja: []dhmz.Upozorenje{{Dogadjaj: "kiša", Razina: 2}}}, m, sada)
	if len(u) != 1 || u[0].Naslov != "DHMZ · žuto upozorenje: kiša" || u[0].Opis != "" {
		t.Errorf("samo događaj: %+v", u)
	}
}

// Razdoblje upozorenja jednostavnim riječima.
func TestRazdobljeUpozorenja(t *testing.T) {
	sada := time.Date(2026, 11, 5, 10, 0, 0, 0, models.Zagreb)
	u := func(d, h, m int) time.Time { return time.Date(2026, 11, d, h, m, 0, 0, models.Zagreb) }
	for _, s := range []struct {
		od, do time.Time
		tekst  string
	}{
		{u(5, 14, 0), u(5, 20, 0), "Danas od 14 do 20 h"},
		{u(5, 14, 30), u(6, 8, 0), "Danas od 14:30 h do sutra 8 h"},
		{u(7, 6, 0), u(7, 12, 0), "7. 11. od 6 do 12 h"},
		{time.Time{}, u(6, 8, 0), "Do sutra u 8 h"},
		{u(5, 14, 0), time.Time{}, ""},
	} {
		if g := razdobljeUpozorenja(s.od, s.do, sada); g != s.tekst {
			t.Errorf("%v–%v: %q, a treba %q", s.od, s.do, g, s.tekst)
		}
	}
	if g := satIliDan(u(4, 7, 30), sada); g != "4. 11. u 07:30" {
		t.Errorf("jučer: %q", g)
	}
	if datumDHMZ("05.11.2026.") != "5. 11." || datumDHMZ(" nepoznato ") != "nepoznato" || datumDHMZ("") != "" {
		t.Error("datum DHMZ-a")
	}
}

// Vjetar: smjer riječima, brzina iz m/s u km/h, tišina, nepoznat smjer.
func TestVjetarRijecima(t *testing.T) {
	v := func(x float64) *float64 { return &x }
	for _, s := range []struct {
		smjer  string
		brzina *float64
		tekst  string
	}{
		{"NE", v(5.6), "sjeveroistočni vjetar 20 km/h"},
		{"C", v(0), "bez vjetra"},
		{"SW", nil, "jugozapadni vjetar"},
		{"", nil, ""},
		{"-", v(2), "vjetar 7 km/h"},
	} {
		if g := vjetarRijecima(s.smjer, s.brzina); g != s.tekst {
			t.Errorf("%q: %q, a treba %q", s.smjer, g, s.tekst)
		}
	}
	p := PlocaVremena{Postaja: &dhmz.Postaja{Ime: "Zadar", Opis: "-", VjetarSmjer: "-"}}
	if s, pr := sadaDijela(p, time.Now()); pr != nil || s.Temp != "- °C" || s.Opis != "" || s.Ikona != "" || s.Detalji != "" {
		t.Errorf("postaja bez podataka: %+v", s)
	}
}

// Kišomjerski dan: satni kišomjer zbraja od 7 h (jučer od 7 do 7 h) i kaže
// koliko sati nedostaje; dnevni zbroj ide od ponoći.
func TestZbrojiDane(t *testing.T) {
	sada := time.Date(2026, 11, 5, 10, 30, 0, 0, models.Zagreb)
	sat := func(d, h int) time.Time { return time.Date(2026, 11, d, h, 0, 0, 0, models.Zagreb).UTC() }
	var m []kisomjeri.Mjerenje
	for h := 8; h <= 10; h++ { // danas 8, 9 i 10 h: tri sata od 7 h
		m = append(m, mjerenje("d", sat(5, h), 1, 1.1))
	}
	for h := 8; h <= 30; h++ { // jučer od 8 do 7 h, bez 23 h: 22 sata
		if h != 23 {
			m = append(m, mjerenje("d", sat(4, 0).Add(time.Duration(h)*time.Hour), 1, 0.5))
		}
	}
	m = append(m, mjerenje("d", sat(5, 11), 1, 9), mjerenje("d", sat(5, 6), 12, 9)) // budući sat i 12-satni ne broje se
	iz := &IzmjerenaKisa{}
	zbrojiDane(iz, m, sada)
	if iz.OdPonoci || iz.DanasOd7 == nil || *iz.DanasOd7 != 3.3 || iz.BezSatiDanas != 0 ||
		iz.Jucer == nil || *iz.Jucer != 11 || iz.BezSatiJucer != 2 {
		t.Errorf("satni: %+v danas %v jučer %v", iz, iz.DanasOd7, iz.Jucer)
	}
	// zadnji sat još nije objavljen (DHMZ oko :40): nije rupa; dva sata
	// šutnje jesu, ali bez zadnjega
	for _, s := range []struct {
		sati []int
		bez  int
	}{{[]int{8, 9}, 0}, {[]int{8}, 1}, {nil, 0}} {
		var mm []kisomjeri.Mjerenje
		for _, h := range s.sati {
			mm = append(mm, mjerenje("d", sat(5, h), 1, 1))
		}
		iz := &IzmjerenaKisa{}
		zbrojiDane(iz, mm, sada)
		if iz.BezSatiDanas != s.bez {
			t.Errorf("sati %v: bez %d, a treba %d", s.sati, iz.BezSatiDanas, s.bez)
		}
	}
	if ocekivanoDanas(sat(5, 7), sat(5, 7).Add(30*time.Minute), sat(5, 7)) != 0 {
		t.Error("prvi sat dana")
	}
	if !maxVrijeme(sat(5, 9), sat(5, 8)).Equal(sat(5, 9)) || !maxVrijeme(sat(5, 8), sat(5, 9)).Equal(sat(5, 9)) {
		t.Error("kasnije vrijeme")
	}
	// prije 7 h kišomjerski dan počeo je jučer u 7 h
	iz = &IzmjerenaKisa{}
	zbrojiDane(iz, nil, time.Date(2026, 11, 5, 6, 0, 0, 0, models.Zagreb))
	if iz.DanasOd7 != nil || iz.Jucer != nil || iz.OdPonoci {
		t.Errorf("bez mjerenja: %+v", iz)
	}
	if !zadnjeJutro(time.Date(2026, 11, 5, 6, 0, 0, 0, models.Zagreb)).Equal(time.Date(2026, 11, 4, 7, 0, 0, 0, models.Zagreb)) {
		t.Error("jutro prije 7 h")
	}
	// dnevni zbroj: jučer je zapis koji završava ponoćas
	iz = &IzmjerenaKisa{}
	zbrojiDane(iz, []kisomjeri.Mjerenje{mjerenje("p", sat(5, 0), 24, 4.5), mjerenje("p", sat(4, 0), 24, 9)}, sada)
	if !iz.OdPonoci || iz.Jucer == nil || *iz.Jucer != 4.5 || iz.DanasOd7 != nil {
		t.Errorf("dnevni: %+v", iz)
	}
}

// Kartica kišomjera: satni kišomjer s napomenom o satima, bez kišomjera
// neutralna rečenica, bez izvora crta upozorenja.
func TestKisomjerDijela(t *testing.T) {
	sada := time.Date(2026, 11, 5, 10, 30, 0, 0, models.Zagreb)
	v := 3.3
	k, pr := kisomjerDijela(PlocaVremena{Izmjereno: &IzmjerenaKisa{Naziv: "Jankovac", Izvor: "dhmz", Km: 12.4, DanasOd7: &v, BezSatiJucer: 1}}, true, sada)
	if pr != nil || k.Mjesto != "Jankovac, 12 km" || k.Javljeno != "DHMZ" || len(k.Redovi) != 3 ||
		k.Redovi[0].Vrijednost != "—" || k.Redovi[1] != (RedakKise{"danas od 7 h", "3,3 mm", ""}) ||
		k.Redovi[2] != (RedakKise{"jučer, od 7 do 7 h", "—", "bez 1 h"}) {
		t.Errorf("satni: %+v", k)
	}
	if _, pr := kisomjerDijela(PlocaVremena{}, true, sada); pr.Vrsta != PraznoCeka || pr.Tekst != "Do 40 km od mjesta nemamo kišomjer." {
		t.Errorf("bez kišomjera: %+v", pr)
	}
	if _, pr := kisomjerDijela(PlocaVremena{KisomjerBlizu: true}, true, sada); pr.Vrsta != PraznoNedostupno ||
		pr.Tekst != "Kišomjeri do 40 km od mjesta trenutačno se ne javljaju." {
		t.Errorf("kišomjeri se ne javljaju: %+v", pr)
	}
	if _, pr := kisomjerDijela(PlocaVremena{}, false, sada); pr.Vrsta != PraznoNedostupno {
		t.Errorf("bez izvora: %+v", pr)
	}
	for izvor, naziv := range map[string]string{"dhmz": "DHMZ", "pljusak": "pljusak", "danubehis": "DanubeHIS", "novi": "novi"} {
		if g := izvorKisomjera(izvor); g != naziv {
			t.Errorf("izvor %q: %q", izvor, g)
		}
	}
}

// Kiša po slivovima: međusliv bez podataka ima crtice, oznaka umjesto
// imena, porast bez dana dnevne piše redni dan.
func TestRedakSliva(t *testing.T) {
	r := redakSliva(SlivUKisi{StanjeSliva: prognoza.StanjeSliva{Sliv: "J"}, Letve: []PorastLetve{{Naziv: "Aljmaš", Cm: 12, Dan: 2, Ima: true}}})
	if r != (RedakSliva{Oznaka: "J", Naziv: "J", Palo24: "—", Dolazi48: "—", Ukratko: "—", Rijetko: "—", Raste: "Aljmaš +12 cm (2. dan)"}) {
		t.Errorf("bez podataka: %+v", r)
	}
	if mmSliva(0.4) != "0,4 mm" || mmSliva(0) != "0 mm" || mmSliva(17.6) != "18 mm" {
		t.Error("milimetri sliva")
	}
	if rijetkostKise(1, "", "") != "uobičajeno" || rijetkostKise(3, "toliko padne u prosjeku jednom u pet godina", "palo u zadnja 24 sata") != "jednom u pet godina" {
		t.Error("rijetkost")
	}
	// razinu daje kiša palih 72 sata: uz rijetkost piše razdoblje, jer ga
	// stupci (palo 24 h, očekivano 48 h) ne pokazuju
	r = redakSliva(SlivUKisi{StanjeSliva: prognoza.StanjeSliva{Sliv: "G", Ima: true, Palo24: 10, Dolazi48: 0, Palo72: 70, Razina: 2,
		Zasto: "palo u zadnja 72 sata", Ucestalost: "toliko padne u prosjeku jednom godišnje"}})
	if r.Rijetko != "jednom godišnje · palo u 72 h" || r.Ukratko != "10 · 0 mm" {
		t.Errorf("tri dana: %+v", r)
	}
	if g := rijetkostKise(1, "toliko padne u prosjeku tri puta godišnje", "palo i očekuje se u 72 sata"); g != "tri puta godišnje · palo i očekivano u 72 h" {
		t.Errorf("palo i očekivano: %q", g)
	}
	if s, pr := slivoviDijela(regijeVremena[0], &kisaSlivovaPodaci{}); s != nil || pr.Vrsta != PraznoNedostupno {
		t.Errorf("prazan izvor: %+v", pr)
	}
}

// Dan prognoze iz satnih: rupa u oborini je sat bez podatka, temperatura
// bez vrijednosti ne ulazi u raspon, snijeg se dopiše.
func TestDanTocke(t *testing.T) {
	var z zbrojDana
	f := func(v float64) sql.NullFloat64 { return sql.NullFloat64{Float64: v, Valid: true} }
	z.dodaj(sql.NullFloat64{}, f(1), f(30))
	z.dodaj(f(0.6), sql.NullFloat64{}, f(-0.4))
	z.dodaj(f(0.6), f(3/0.7), sql.NullFloat64{}) // Open-Meteo 3 cm snijega spremljen kao mm vode
	z.dodaj(f(0), f(0), f(4.6))
	d := z.dan(time.Date(2026, 11, 6, 0, 0, 0, 0, models.Zagreb), 1)
	if z.sati != 3 || d != (DanVremena{Naslov: "Pet 6. 11.", Ikona: "cloud-rain", Temp: "0 / 5 °C", Kisa: "kiša 1 mm · snijeg 3 cm"}) {
		t.Errorf("dan: %+v %+v", z, d)
	}
	if d := (zbrojDana{}).dan(time.Now(), 0); d.Naslov != "Danas" || d.Temp != "" || d.Ikona != "" {
		t.Errorf("prazan dan: %+v", d)
	}
	ponoc := time.Date(2026, 10, 25, 0, 0, 0, 0, models.Zagreb) // dan pomaka sata ima 25 sati
	if danOd(ponoc, ponoc.Add(24*time.Hour)) != 0 || danOd(ponoc, ponoc.Add(25*time.Hour)) != 1 || danOd(ponoc, ponoc.Add(-time.Hour)) != -1 {
		t.Error("dan od ponoći uz pomak sata")
	}
}

// Dani prognoze: bez točke do 40 km „U izradi”, a točka bez satnih, izvor
// koji ne odgovori i bez izvora crta upozorenja.
func TestDaniPrognozeBezTocke(t *testing.T) {
	sada := time.Now()
	if d := (&VrijemeHandler{}).daniPrognoze(context.Background(), osijekLat, osijekLon, sada); d.Dani != nil || d.Izvor != "" || d.Prazno != daniNedostupni {
		t.Error("bez izvora")
	}
	o := novaKisaOkolina(t)
	o.tocka("stv", models.KisomjerStvarni, 0.01, true)
	o.tocka("ugasena", models.KisomjerIzvedeni, 0.02, false)
	o.tocka("prazna", models.KisomjerIzvedeni, 0.1, true)
	if d := o.h.daniPrognoze(context.Background(), osijekLat, osijekLon, sada); d.Dani != nil || d.Prazno != daniNedostupni {
		t.Errorf("točka bez satnih: %+v", d)
	}
	if d := o.h.daniPrognoze(context.Background(), osijekLat+1, osijekLon, sada); d.Dani != nil || d.Prazno != daniUIzradi {
		t.Errorf("točka dalje od 40 km: %+v", d)
	}
	gotov, otkazi := context.WithCancel(context.Background())
	otkazi()
	if d := o.h.daniPrognoze(gotov, osijekLat, osijekLon, sada); d.Prazno != daniNedostupni {
		t.Errorf("izvor nije odgovorio: %+v", d)
	}
	if _, err := o.obo.Exec(`DROP TABLE satne`); err != nil {
		t.Fatal(err)
	}
	if daniTocke(o.sp, "prazna", sada) != nil {
		t.Error("bez tablice")
	}
	if _, err := o.reg.Exec(`DROP TABLE kisomjeri`); err != nil {
		t.Fatal(err)
	}
	if d := o.h.daniPrognoze(context.Background(), osijekLat, osijekLon, sada); d.Prazno != daniNedostupni {
		t.Error("bez registra")
	}
}

// Dan najvećeg porasta je kraj tog dana dnevne prognoze.
func TestKrajDana(t *testing.T) {
	d := []prognoza.DnevnaIzdana{{Dan: 0, Ciljni: 10}, {Dan: 2, Ciljni: 490000}}
	if !krajDana(d, 2).Equal(time.Unix(490000*3600, 0)) || !krajDana(d, 0).IsZero() || !krajDana(d, 3).IsZero() {
		t.Error("kraj dana")
	}
}

// Točke bez izvedene i satna koja se ne da pročitati: dana nema, a
// pročitani sati ostaju.
func TestDaniPrognozeSamoStvarni(t *testing.T) {
	o := novaKisaOkolina(t)
	o.tocka("stv", models.KisomjerStvarni, 0.01, true)
	sada := time.Now()
	if d := o.h.daniPrognoze(context.Background(), osijekLat, osijekLon, sada); d.Dani != nil || d.Prazno != daniUIzradi {
		t.Errorf("samo stvarni kišomjer: %+v", d)
	}
	ponoc := ponocDana(sada).Unix() / 3600
	for s := ponoc; s < ponoc+24; s++ {
		o.satna("izv", s, mmp(0))
	}
	if _, err := o.obo.Exec(`UPDATE satne SET oborina = 'ništa' WHERE sat = ?`, ponoc); err != nil {
		t.Fatal(err)
	}
	if d := daniTocke(o.sp, "izv", sada); len(d) != 1 || d[0].Naslov != "Danas" || d[0].Temp != "" {
		t.Errorf("dan s nečitljivim satom: %+v", d)
	}
}

// Izbor mjesta čita sva branjena područja; bez registra nijedno.
func TestPodrucjaVremena(t *testing.T) {
	if a, err := (&Server{}).podrucjaVremena(context.Background()); a != nil || err != nil {
		t.Errorf("bez registra: %v %v", a, err)
	}
	o := novaKisaOkolina(t)
	s := &Server{orgService: service.NewOrgService(repository.NewOrgRepository(o.reg, ledger.New(o.reg, "test-node")), service.NewSSEBroker())}
	if _, err := s.podrucjaVremena(context.Background()); err != nil {
		t.Errorf("registar: %v", err)
	}
}

// Ikona vremena iz DHMZ-ova opisa.
func TestIkonaVremena(t *testing.T) {
	for opis, ikona := range map[string]string{"grmljavina": "zap", "slaba kiša": "cloud-rain", "magla": "wind",
		"pretežno oblačno": "sun-moon", "vedro": "sun"} {
		if g := ikonaVremena(opis); g != ikona {
			t.Errorf("%q: %q, a treba %q", opis, g, ikona)
		}
	}
}

// Dan pomaka sata ima 23 sata, a sutra je i dalje sutra.
func TestImeDanaUzPomakSata(t *testing.T) {
	sada := time.Date(2026, 3, 28, 22, 0, 0, 0, models.Zagreb) // sutra se sat pomiče naprijed
	if g := imeDana(time.Date(2026, 3, 29, 23, 0, 0, 0, models.Zagreb), sada); g != "sutra" {
		t.Errorf("sutra uz pomak: %q", g)
	}
	if g := imeDana(time.Date(2026, 3, 30, 1, 0, 0, 0, models.Zagreb), sada); g != "30. 3." {
		t.Errorf("prekosutra: %q", g)
	}
}

// Kišomjer na stranici računa se u satu koji stranica zada: u 6:30 je
// kišomjerski dan počeo jučer u 7 h, pa sati od jučer 8 h idu u „danas”.
func TestDioVremenaPrije7(t *testing.T) {
	o := novaKisaOkolina(t)
	o.tocka("stv", models.KisomjerStvarni, 0.02, true)
	sada := time.Date(2026, 11, 5, 6, 30, 0, 0, models.Zagreb)
	for h := 8; h <= 30; h++ { // od 4. 11. u 8 h do 5. 11. u 6 h
		o.mjeri(mjerenje("stv", time.Date(2026, 11, 4, h, 0, 0, 0, models.Zagreb).UTC(), 1, 0.5))
	}
	o.mjeri(mjerenje("stv", time.Date(2026, 11, 4, 7, 0, 0, 0, models.Zagreb).UTC(), 1, 2))
	h := &VrijemePrognozaHandler{tmpl: predlozakVremena(t), vrijeme: func() *VrijemeHandler { return o.h }, sada: func() time.Time { return sada }}
	w := httptest.NewRecorder()
	h.ShowDio(w, zahtjevAdmina("/prognoze/vrijeme/dio"))
	s := stdhtml.UnescapeString(w.Body.String())
	for _, trazi := range []string{"<dt>zadnji sat</dt><dd>0,5 mm</dd>", "<dt>danas od 7 h</dt><dd>11,5 mm</dd>", "<dt>jučer, od 7 do 7 h <span class=\"pv-sitno\">(bez 23 h)</span></dt><dd>2,0 mm</dd>"} {
		if !strings.Contains(s, trazi) {
			t.Errorf("dio nema %q:\n%s", trazi, s)
		}
	}
}

// Kišomjer do 40 km koji se ne javlja nije isto što i mjesto bez kišomjera.
func TestKisomjerBlizuSeNeJavlja(t *testing.T) {
	o := novaKisaOkolina(t)
	o.tocka("nijemi", models.KisomjerStvarni, 0.02, true)
	if p := o.ploca(); p.Izmjereno != nil || !p.KisomjerBlizu {
		t.Errorf("nijemi kišomjer: %+v", p)
	}
}

// Poveznica kartice Naslovne u modul koji račun ne vidi se skida: voditelj
// usluga ima karticu Vrijeme, a Prognoze su u modulu Očitanja.
func TestPoveznicaKojuVidi(t *testing.T) {
	usluga := models.Visibility{}
	for _, m := range models.DefaultModules(models.RoleServiceLeaderForeman) {
		usluga[m] = true
	}
	if k := poveznicaKojuVidi(glaveKartica[kVrijeme], usluga); k.Poveznica != "" || k.URL != "" || k.Naslov != "Vrijeme" {
		t.Errorf("usluga: %+v", k)
	}
	if k := poveznicaKojuVidi(glaveKartica[kVrijeme], models.Visibility{models.ModuleReadings: true}); k.URL != "/prognoze/vrijeme" {
		t.Errorf("s Očitanjima: %+v", k)
	}
	if k := poveznicaKojuVidi(KarticaNaslovne{Poveznica: "Plan", URL: "/profile"}, usluga); k.URL != "/profile" {
		t.Errorf("izvan modula: %+v", k)
	}
	o := novaOkolinaNaslovne(t)
	r := httptest.NewRequest(http.MethodGet, "/naslovna/kartica/vrijeme", nil)
	perms := dozvoleNaslovne(true)
	ctx := context.WithValue(r.Context(), contextKeyUser, &perms.User)
	ctx = context.WithValue(ctx, contextKeyPerms, perms)
	ctx = context.WithValue(ctx, contextKeyModules, usluga)
	w := httptest.NewRecorder()
	o.mux.ServeHTTP(w, r.WithContext(ctx))
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "/prognoze/vrijeme") {
		t.Errorf("spora kartica bez modula: %d %s", w.Code, w.Body.String())
	}
}
