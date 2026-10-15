package web

import (
	"context"
	"database/sql"
	"html/template"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/arhiva"
	"gocop/internal/javnivodostaji"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// arhivaPrimjerova je izmišljena arhiva letve primjerovo: jedna snimka korita
// (kota nule 100,00 m, dno 98,20 m) i jedna HQ krivulja Q = 100·H, H u
// metrima, od 0 do 300 cm, otvorena od 2001.
func arhivaPrimjerova(t *testing.T) *repository.ArhivaRepository {
	t.Helper()
	put := filepath.Join(t.TempDir(), "arhiva.db")
	a, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	if err := arhiva.PripremiPraznu(a); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO profili (id, letva, datum, vodostaj, kota_nule) VALUES (1, 'primjerovo', '2020-03-01', 120, 100.0)`,
		`INSERT INTO profil_tocke (profil, stacionaza, visina) VALUES (1, 0, 102.5), (1, 5, 99.0), (1, 10, 98.2), (1, 15, 99.1), (1, 20, 102.8)`,
		`INSERT INTO hq_krivulje (id, letva, vrijedi_od, vrijedi_do, izvor) VALUES (1, 'primjerovo', '2001-01-01', '', 'DHMZ')`,
		`INSERT INTO hq_odsjecci (krivulja, od_cm, do_cm, oblik, p1, p2, p3, p4) VALUES (1, 0, 300, 'polinom', 0, 100, 0, 0)`,
	} {
		if _, err := a.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	a.Close()
	arh, err := repository.OpenArhiva(put)
	if err != nil || arh == nil {
		t.Fatalf("arhiva: %v", err)
	}
	t.Cleanup(func() { arh.Close() })
	return arh
}

func cmPtr(v int) *int { return &v }

// sOcitanjimaPoGodinama dodaje letvi primjerovo pragove obrane i očitanja po
// godinama: 2014. trideset pet satnih (zadnje 120 cm), 2013. jedno s
// izmjerenim protokom, 2012. jedno iznad raspona krivulje i 2011. jedno bez
// vodostaja.
func (o *okolinaPrava) sOcitanjimaPoGodinama(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	o.st.Prep, o.st.Regular = models.Threshold{Cm: cmPtr(150)}, models.Threshold{Cm: cmPtr(200)}
	o.st.Emergency, o.st.State = models.Threshold{Cm: cmPtr(250)}, models.Threshold{Cm: cmPtr(290)}
	if err := o.stanice.UpdateStation(ctx, o.st); err != nil {
		t.Fatal(err)
	}
	upisi := func(kad time.Time, cm *int, izmjereno, temp *float64) {
		t.Helper()
		if err := o.ocitanja.Create(ctx, &models.Reading{ID: uuid.New(), StationID: o.st.ID.String(), MeasuredAt: kad.UTC(),
			LevelCm: cm, FlowM3s: izmjereno, TempC: temp, Source: models.ReadingSourceManual, UserID: o.pero.ID.String()}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 35; i++ {
		upisi(zagreb(2014, 5, 1, 0).Add(time.Duration(i)*time.Hour), cmPtr(86+i), nil, nil)
	}
	q, temp := 55.4, 12.5
	upisi(zagreb(2013, 6, 1, 6), cmPtr(150), &q, nil)
	upisi(zagreb(2012, 6, 1, 6), cmPtr(300+models.ProsirenjeKrivuljeCm), nil, nil)
	upisi(zagreb(2011, 6, 1, 6), nil, nil, &temp)
}

// podaciLetve zove prikupljanje podataka stranice letve za putanju s upitom
func (o *okolinaPrava) podaciLetve(t *testing.T, putanja, id string) (ReadingHistoryData, *models.Station, *httptest.ResponseRecorder) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, putanja, nil)
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	data, st, ok := o.h.podaciOcitanja(w, zahtjevSKorisnikom(r, o.pero, o.peroOvl))
	if !ok && w.Code == http.StatusOK {
		t.Fatal("podaci nisu prikupljeni, a odgovor nije greška")
	}
	return data, st, w
}

// Pogled bira razdoblje i za graf i za tablicu: zadano trideset dana, godina
// po izboru, sve ili broj dana. Godina bez očitanja vraća se na zadano, a
// razdoblje dulje od jedne stranice istakne na grafu dio koji je u tablici.
func TestStranicaLetvePogledi(t *testing.T) {
	o := novaOkolinaPrava(t)
	o.sOcitanjimaPoGodinama(t)
	id := o.st.ID.String()
	put := "/readings/station/" + id
	for _, s := range []struct {
		pogled, zeli, opis string
		broj, dana, godina int
	}{
		{"", "30", "zadnjih 30 dana", 2, 30, 0},
		{"1999", "30", "zadnjih 30 dana", 2, 30, 0},
		{"7", "7", "zadnjih 7 dana", 2, 7, 0},
		{"99999", "30", "zadnjih 30 dana", 2, 30, 0},
		{"-5", "-5", "zadnjih 30 dana", 2, 30, 0},
		{"2014", "2014", "2014.", 35, 0, 2014},
		{"sve", "sve", "sve", 40, 0, 0},
	} {
		data, _, _ := o.podaciLetve(t, put+"?pogled="+s.pogled, id)
		if data.Pogled != s.zeli || data.PogledOpis != s.opis || data.Count != s.broj || data.Dana != s.dana || data.Year != s.godina {
			t.Errorf("pogled %q: %q %q, %d očitanja, %d dana, godina %d", s.pogled, data.Pogled, data.PogledOpis, data.Count, data.Dana, data.Year)
		}
	}
	data, _, _ := o.podaciLetve(t, put+"?pogled=2014", id)
	if len(data.Readings) != readingsPerPage || !data.Pager.Multi() || !data.Chart.Istaknuto || !data.ChartUzak.Istaknuto || len(data.Svi) != 35 {
		t.Errorf("listanje 2014.: %d na stranici, istaknuto %v/%v", len(data.Readings), data.Chart.Istaknuto, data.ChartUzak.Istaknuto)
	}
	if !slices.IsSortedFunc(data.Years, func(a, b int) int { return b - a }) || !slices.Contains(data.Years, 2011) || !slices.Contains(data.Years, 2014) {
		t.Errorf("godine: %v", data.Years)
	}
	if data.GaugeKey != "station:"+id || data.NewURL != "/readings/new?station="+id || !data.CanRecord || data.Latest == nil {
		t.Errorf("letva: %q %q %v", data.GaugeKey, data.NewURL, data.CanRecord)
	}
}

// Ispod grafa stoji presjek korita s vodom po zadnjem očitanju razdoblja i
// stupnjevima obrane, a uz vodu protok: izmjeren kad ga očitanje nosi, inače
// iz krivulje, s upozorenjem kad je vodostaj izvan njezina raspona. Bez
// ijednog vodostaja u razdoblju presjeka nema.
func TestStranicaLetveKoritoIProtok(t *testing.T) {
	o := novaOkolinaPrava(t)
	o.sOcitanjimaPoGodinama(t)
	arh := arhivaPrimjerova(t)
	o.h.SetArhiva(func() *repository.ArhivaRepository { return arh })
	id := o.st.ID.String()
	izvan := 300 + models.ProsirenjeKrivuljeCm
	for _, s := range []struct {
		godina, natpis, opis string
	}{
		{"2014", "Q ≈ 120 m³/s iz krivulje", "120 cm, 2.5.2014. 10:00"},
		{"2013", "Q 55 m³/s izmjereno", "150 cm, 1.6.2013. 06:00"},
		{"2012", "Q ≈ " + strconv.Itoa(izvan) + " m³/s " + UpozorenjeIzvanKrivulje, strconv.Itoa(izvan) + " cm, 1.6.2012. 06:00"},
	} {
		data, _, _ := o.podaciLetve(t, "/readings/station/"+id+"?pogled="+s.godina, id)
		if data.Korito == nil || data.KoritoUzak == nil {
			t.Fatalf("%s: nema presjeka korita", s.godina)
		}
		if data.Korito.ProtokNatpis != s.natpis || data.KoritoUzak.ProtokNatpis != s.natpis || data.KoritoOpis != s.opis {
			t.Errorf("%s: natpis %q / %q, opis %q", s.godina, data.Korito.ProtokNatpis, data.KoritoUzak.ProtokNatpis, data.KoritoOpis)
		}
		if len(data.Profili) != 1 || data.Profil == nil || len(data.Korito.Pragovi) == 0 || len(data.Krivulje) != 1 {
			t.Errorf("%s: snimki %d, pragova %d, krivulja %d", s.godina, len(data.Profili), len(data.Korito.Pragovi), len(data.Krivulje))
		}
	}
	data, _, _ := o.podaciLetve(t, "/readings/station/"+id+"?pogled=2011", id)
	if data.Korito != nil || data.KoritoOpis != "" || data.Count != 1 {
		t.Errorf("razdoblje bez vodostaja: korito %v, opis %q", data.Korito != nil, data.KoritoOpis)
	}

	// Presjek se ne crta bez arhive, bez snimke letve ni kad se snimke ne daju pročitati.
	var d ReadingHistoryData
	d.Station = o.st
	prikazano := []models.Reading{{LevelCm: cmPtr(120), MeasuredAt: time.Now()}}
	o.h.SetArhiva(func() *repository.ArhivaRepository { return nil })
	o.h.koritoUzGraf(context.Background(), &d, o.st, prikazano)
	o.h.SetArhiva(func() *repository.ArhivaRepository { return arh })
	o.h.koritoUzGraf(context.Background(), &d, &models.Station{Code: "drugovo"}, prikazano)
	bezSnimki := probnaArhivaNula(t, nil)
	o.h.SetArhiva(func() *repository.ArhivaRepository { return bezSnimki })
	o.h.koritoUzGraf(context.Background(), &d, o.st, prikazano)
	if d.Korito != nil || d.Profili != nil {
		t.Error("presjek je nacrtan bez snimke")
	}
}

// Stranica objekta: pragovi i vodomjer dolaze od pridružene letve, a čvor
// koji drži cijelu povijest to kaže. Objekt koji ne postoji i nečitljiv
// identifikator daju 404.
func TestStranicaObjektaIPracenje(t *testing.T) {
	o := novaOkolinaPrava(t)
	ctx := context.Background()
	objekt := &models.Structure{ID: uuid.New(), Code: "cs-primjerovo", Name: "CS Primjerovo", Kind: models.StructureKindPumpingStation,
		SectorID: "P", AreaID: 1, StationID: o.st.ID.String(), Origin: "RUČNI_UNOS"}
	if err := repository.NewStructureRepository(o.baza, ledger.New(o.baza, "test")).CreateStructure(ctx, objekt); err != nil {
		t.Fatal(err)
	}
	pracenje := repository.NewFollowRepository(o.baza)
	o.h.SetFollow(pracenje, nil)
	if err := pracenje.Add(ctx, "structure:"+objekt.ID.String(), "CS Primjerovo"); err != nil {
		t.Fatal(err)
	}
	put := "/readings/structure/" + objekt.ID.String()
	data, st, w := o.podaciLetve(t, put, objekt.ID.String())
	if w.Code != http.StatusOK || st != nil || data.Structure == nil || data.GaugeName != "CS Primjerovo" ||
		data.GaugeKey != "structure:"+objekt.ID.String() || data.NewURL != "/readings/new?structure="+objekt.ID.String() ||
		!data.Followed || !data.CanRecord {
		t.Errorf("stranica objekta: %d %+v", w.Code, data)
	}
	for _, id := range []string{"nije-uuid", uuid.NewString()} {
		if _, _, w := o.podaciLetve(t, "/readings/structure/"+id, id); w.Code != http.StatusNotFound {
			t.Errorf("objekt %s: %d", id, w.Code)
		}
	}
	// Letva koju čvor ne prati nije označena.
	if data, _, _ := o.podaciLetve(t, "/readings/station/"+o.st.ID.String(), o.st.ID.String()); data.Followed {
		t.Error("letva koju čvor ne prati označena je kao praćena")
	}
}

// Letva prebačena na telemetriju zove se tako na stranici i dobiva adresu
// HydroViewa; stanje zadnjeg preuzimanja stoji uz letvu kad ga ima.
func TestStranicaLetveTelemetrijaIStanjePreuzimanja(t *testing.T) {
	o := novaOkolinaPrava(t)
	ctx := context.Background()
	uvoznik := javnivodostaji.NoviUvoznik(repository.NewJavniSpremiste(o.baza, o.ocitanja), nil)
	o.h.SetJavniUvoz(func() *javnivodostaji.Uvoznik { return uvoznik })
	o.st.TelemetrijaUvoz, o.st.TelemetrijaSite, o.st.JavniURL = true, " PrimjerovoSite0001 ", "https://nepoznato.primjer.test/letva"
	if err := o.stanice.UpdateStation(ctx, o.st); err != nil {
		t.Fatal(err)
	}
	id := o.st.ID.String()
	data, _, _ := o.podaciLetve(t, "/readings/station/"+id, id)
	if !data.JavniTelemetrija || !data.JavniUvoz || data.JavniURL != javnivodostaji.AdresaHidroView("PrimjerovoSite0001") || data.JavniIzvor == "" || data.JavniStanje != nil {
		t.Errorf("telemetrija: %v %v %q %q %v", data.JavniTelemetrija, data.JavniUvoz, data.JavniURL, data.JavniIzvor, data.JavniStanje)
	}

	drugo := &models.Station{ID: uuid.New(), Code: "nepoznatovo", Name: "Nepoznatovo", JavniURL: "https://nepoznato.primjer.test/letva", JavniUvoz: true}
	if err := o.stanice.CreateStation(ctx, drugo); err != nil {
		t.Fatal(err)
	}
	uvoznik.Preuzmi(ctx, drugo)
	data, _, _ = o.podaciLetve(t, "/readings/station/"+drugo.ID.String(), drugo.ID.String())
	if data.JavniTelemetrija || data.JavniURL != drugo.JavniURL || data.JavniIzvor != "" || data.JavniStanje == nil || data.JavniStanje.Greska == "" {
		t.Errorf("stanje preuzimanja: %v %q %q %+v", data.JavniTelemetrija, data.JavniURL, data.JavniIzvor, data.JavniStanje)
	}
}

// Stranica letve koja se ne da iscrtati je greška poslužitelja.
func TestStranicaLetveBezPredloska(t *testing.T) {
	o := novaOkolinaPrava(t)
	o.h.tmplHistory = template.Must(template.New("prazan").Parse(""))
	w := o.zovi(o.pero, o.peroOvl, httptest.NewRequest(http.MethodGet, "/readings/station/"+o.st.ID.String(), nil))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("stranica bez predloška: %d", w.Code)
	}
}

// Obrazac očitanja: nečitljivo ili nepostojeće očitanje i letva koje nema daju
// 404, upis bez prava 403. Obrazac crpne stanice pita za stanje, a uz letvu s
// krivuljom stoji što bi krivulja rekla za zadnje očitanje.
func TestObrazacOcitanja(t *testing.T) {
	o := novaOkolinaPrava(t)
	ctx := context.Background()
	arh := arhivaPrimjerova(t)
	o.h.SetArhiva(func() *repository.ArhivaRepository { return arh })
	objekt := &models.Structure{ID: uuid.New(), Code: "cs-primjerovo", Name: "CS Primjerovo", Kind: models.StructureKindPumpingStation,
		SectorID: "P", AreaID: 1, Origin: "RUČNI_UNOS"}
	if err := repository.NewStructureRepository(o.baza, ledger.New(o.baza, "test")).CreateStructure(ctx, objekt); err != nil {
		t.Fatal(err)
	}
	if err := o.ocitanja.Create(ctx, &models.Reading{ID: uuid.New(), StructureID: objekt.ID.String(), MeasuredAt: time.Now().Add(-time.Hour).UTC(),
		LevelCm: cmPtr(77), Source: models.ReadingSourceManual, UserID: o.pero.ID.String()}); err != nil {
		t.Fatal(err)
	}
	iva, ivaOvl := bezPrava()
	obrazac := func(putanja, id string, u *models.User, p *models.UserPermissions) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, putanja, nil)
		if id != "" {
			r.SetPathValue("id", id)
		}
		w := httptest.NewRecorder()
		o.h.ShowForm(w, zahtjevSKorisnikom(r, u, p))
		return w
	}
	for _, s := range []struct {
		opis, putanja, id string
		u                 *models.User
		p                 *models.UserPermissions
		status            int
	}{
		{"nečitljivo očitanje", "/readings/edit/nije-uuid", "nije-uuid", o.pero, o.peroOvl, http.StatusNotFound},
		{"očitanje koje ne postoji", "/readings/edit/x", uuid.NewString(), o.pero, o.peroOvl, http.StatusNotFound},
		{"letva koje nema", "/readings/new?station=" + uuid.NewString(), "", o.pero, o.peroOvl, http.StatusNotFound},
		{"bez prava", "/readings/new?station=" + o.st.ID.String(), "", iva, ivaOvl, http.StatusForbidden},
	} {
		if w := obrazac(s.putanja, s.id, s.u, s.p); w.Code != s.status {
			t.Errorf("%s: %d, očekivan %d", s.opis, w.Code, s.status)
		}
	}
	w := obrazac("/readings/new?structure="+objekt.ID.String(), "", o.pero, o.peroOvl)
	if html := w.Body.String(); w.Code != http.StatusOK || !strings.Contains(html, "Stanje crpne stanice") || !strings.Contains(html, "CS Primjerovo") {
		t.Errorf("obrazac crpne stanice: %d", w.Code)
	}
	w = obrazac("/readings/new?station="+o.st.ID.String(), "", o.pero, o.peroOvl)
	if html := htmlUTekst(w.Body.String()); w.Code != http.StatusOK || !strings.Contains(html, "za zadnje očitanje daje <strong>101 m³/s</strong>") {
		t.Errorf("obrazac letve s krivuljom: %d", w.Code)
	}
	o.h.tmplForm = template.Must(template.New("prazan").Parse(""))
	if w := obrazac("/readings/new?station="+o.st.ID.String(), "", o.pero, o.peroOvl); w.Code != http.StatusInternalServerError {
		t.Errorf("obrazac bez predloška: %d", w.Code)
	}
}

// Pregled letvi: bez „sve” vide se samo letve s očitanjima, a sa „sve” one
// stoje prve. Filtar sektora, područja i tražilica sužavaju popis, nepoznato
// područje se zanemaruje, a uz letvu stoji protok iz krivulje. Kad se popis
// ne da pročitati, stranica to kaže.
func TestPregledLetviFiltri(t *testing.T) {
	o := novaOkolinaPrava(t)
	arh := arhivaPrimjerova(t)
	o.h.SetArhiva(func() *repository.ArhivaRepository { return arh })
	if err := o.stanice.CreateStation(context.Background(), &models.Station{ID: uuid.New(), Code: "praznovo", Name: "Praznovo"}); err != nil {
		t.Fatal(err)
	}
	o.h.tmplOverview = template.Must(template.New("readings.html").Parse(
		`{{range .Gauges}}{{.Name}};{{end}}|podrucje={{.SelectedArea}}|ukupno={{.TotalGauges}}/{{.WithReadings}}|greska={{.ErrorMessage}}|protok={{range $k, $v := .Protok}}{{$v.Q}};{{end}}`))
	pregled := func(upit string) (string, int) {
		w := httptest.NewRecorder()
		o.h.ShowOverview(w, zahtjevSKorisnikom(httptest.NewRequest(http.MethodGet, "/readings?"+upit, nil), o.pero, o.peroOvl))
		return w.Body.String(), w.Code
	}
	for upit, zeli := range map[string]string{
		"":                  "Primjerovo;|podrucje=0|ukupno=2/1|greska=|protok=101;",
		"all=1":             "Primjerovo;Praznovo;|podrucje=0|ukupno=2/1|greska=|protok=101;",
		"all=1&sector=P":    "Primjerovo;|podrucje=0|",
		"all=1&area=1":      "Primjerovo;|podrucje=1|",
		"area=99":           "Primjerovo;|podrucje=0|",
		"q=nepostojevo":     "|podrucje=0|",
		"all=1&q=PRAZNO":    "Praznovo;|podrucje=0|",
		"sector=R&area=1":   "|podrucje=0|",
		"all=1&sector=nema": "|podrucje=0|",
	} {
		if got, _ := pregled(upit); !strings.HasPrefix(got, zeli) {
			t.Errorf("%q: %s, očekivano %s…", upit, got, zeli)
		}
	}
	o.h.tmplOverview = template.Must(template.New("prazan").Parse(""))
	if _, status := pregled(""); status != http.StatusInternalServerError {
		t.Errorf("pregled bez predloška: %d", status)
	}
	o.h.tmplOverview = template.Must(template.New("readings.html").Parse(`greska={{.ErrorMessage}}`))
	if err := o.baza.Close(); err != nil {
		t.Fatal(err)
	}
	if got, _ := pregled(""); got == "greska=" {
		t.Error("popis letvi koji se ne da pročitati nije javljen")
	}
}

// Protok uz letvu na pregledu: izmjeren kad ga zadnje očitanje nosi, inače iz
// krivulje u arhivi; bez zadnjeg očitanja, vodostaja, arhive ili letve nema ga.
func TestProtokLetveNaPregledu(t *testing.T) {
	o := novaOkolinaPrava(t)
	ctx := context.Background()
	id := o.st.ID.String()
	q := 42.6
	latest := func(cm *int, izmjereno *float64) *models.Reading {
		return &models.Reading{MeasuredAt: time.Now(), LevelCm: cm, FlowM3s: izmjereno}
	}
	if p := o.h.protokLetve(ctx, models.GaugeSummary{StationID: id, Latest: latest(cmPtr(120), &q)}); p != (ProtokLetve{Q: "43", Izmjeren: true}) {
		t.Errorf("izmjeren protok: %+v", p)
	}
	if p := o.h.protokLetve(ctx, models.GaugeSummary{StationID: id, Latest: latest(cmPtr(120), nil)}); p != (ProtokLetve{}) {
		t.Errorf("bez arhive: %+v", p)
	}
	arh := arhivaPrimjerova(t)
	o.h.SetArhiva(func() *repository.ArhivaRepository { return arh })
	for _, s := range []struct {
		opis string
		g    models.GaugeSummary
		zeli ProtokLetve
	}{
		{"iz krivulje", models.GaugeSummary{StationID: id, Latest: latest(cmPtr(120), nil)}, ProtokLetve{Q: "120"}},
		{"izvan krivulje", models.GaugeSummary{StationID: id, Latest: latest(cmPtr(310), nil)}, ProtokLetve{Q: "310", Izvan: true}},
		{"bez zadnjeg očitanja", models.GaugeSummary{StationID: id}, ProtokLetve{}},
		{"objekt", models.GaugeSummary{StructureID: "o1", Latest: latest(cmPtr(120), nil)}, ProtokLetve{}},
		{"bez vodostaja", models.GaugeSummary{StationID: id, Latest: latest(nil, nil)}, ProtokLetve{}},
		{"nečitljiva letva", models.GaugeSummary{StationID: "nije-uuid", Latest: latest(cmPtr(120), nil)}, ProtokLetve{}},
		{"letve nema", models.GaugeSummary{StationID: uuid.NewString(), Latest: latest(cmPtr(120), nil)}, ProtokLetve{}},
	} {
		if p := o.h.protokLetve(ctx, s.g); p != s.zeli {
			t.Errorf("%s: %+v, očekivano %+v", s.opis, p, s.zeli)
		}
	}
}

// Prorjeđivanje za crtanje dijeli niz na košare i iz svake uzima najniži i
// najviši vodostaj, u vremenskom redu; košara bez vodostaja ili s jednakima
// daje jednu točku, a kratak niz ostaje kakav jest.
func TestProrjedjivanjeCuvaVrhove(t *testing.T) {
	pocetak := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	razine := []*int{cmPtr(10), nil, cmPtr(30), cmPtr(5), cmPtr(20), nil, cmPtr(7), cmPtr(1), cmPtr(9), cmPtr(8),
		nil, nil, nil, nil, nil, cmPtr(4), cmPtr(4)}
	var niz []models.Reading
	for i := len(razine) - 1; i >= 0; i-- { // obrnutim redom: prorjeđivanje samo slaže po vremenu
		niz = append(niz, models.Reading{MeasuredAt: pocetak.Add(time.Duration(i) * time.Hour), LevelCm: razine[i]})
	}
	out := prorijedi(niz, 6)
	var dobiveno []string
	for i, rd := range out {
		if i > 0 && rd.MeasuredAt.Before(out[i-1].MeasuredAt) {
			t.Errorf("točka %d nije po vremenu", i)
		}
		if rd.LevelCm == nil {
			dobiveno = append(dobiveno, "-")
		} else {
			dobiveno = append(dobiveno, strconv.Itoa(*rd.LevelCm))
		}
	}
	if got := strings.Join(dobiveno, ","); got != "30,5,1,9,-,4" {
		t.Errorf("prorijeđeno %s, očekivano 30,5,1,9,-,4", got)
	}
	if got := prorijedi(niz, 3); len(got) != len(niz) || &got[0] != &niz[0] {
		t.Error("premali cilj mora vratiti niz kakav jest")
	}
	if got := prorijedi(niz[:5], 6); len(got) != 5 || &got[0] != &niz[0] {
		t.Error("kratak niz mora ostati kakav jest")
	}
}

// Obrazac očitanja: svako brojčano polje koje nije broj i vrijeme koje se ne
// čita vraćaju grešku koja kaže koje je polje krivo; metoda mjerenja protoka
// prima se samo sa zatvorenog popisa.
func TestOcitanjeIzObrascaKrivaPolja(t *testing.T) {
	for polje, poruka := range map[string]string{
		"measured_at": "vrijeme očitanja nije čitljivo",
		"level_cm":    "vodostaj: „x“ nije broj",
		"level2_cm":   "nizvodni vodostaj: „x“ nije broj",
		"ag_hours_1":  "agregat 1: „x“ nije broj",
		"ag_hours_2":  "agregat 2: „x“ nije broj",
		"ag_hours_3":  "agregat 3: „x“ nije broj",
		"temp_c":      "temperatura: „x“ nije broj",
		"flow_m3s":    "protok: „x“ nije broj",
	} {
		r := obicniObrazac("/readings/create", polje+"=x")
		if rd, err := readingFromForm(r); err == nil || err.Error() != poruka || rd != nil {
			t.Errorf("%s: %v", polje, err)
		}
	}
	r := obicniObrazac("/readings/create", "level_cm=120&flow_m3s=42,5&flow_method=+adcp+&measured_at=2026-03-01T06:00")
	rd, err := readingFromForm(r)
	if err != nil || rd.FlowMethod != "ADCP" || *rd.FlowM3s != 42.5 || !rd.MeasuredAt.Equal(zagreb(2026, 3, 1, 6)) {
		t.Errorf("ispravan obrazac: %+v, %v", rd, err)
	}
	if rd, _ := readingFromForm(obicniObrazac("/", "flow_method=nepoznata")); rd.FlowMethod != "" {
		t.Errorf("metoda izvan popisa: %q", rd.FlowMethod)
	}
}
