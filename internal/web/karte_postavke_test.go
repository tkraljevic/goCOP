package web

import (
	"bytes"
	"context"
	"html/template"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"
)

// Administracija › Karte: stranica pokazuje stil svake vrste i podloge s
// praznim retkom za novu; spremanje iz obrasca vrijedi odmah, loša podloga
// vraća poruku, a „Vrati zadano” vraća narančaste nasipe i satelit. Postavke
// stižu u svaku stranicu, kao JSON za app.js.
func TestStranicaPostavkiKarata(t *testing.T) {
	prije := models.PostavkeKartePrograma()
	t.Cleanup(func() { models.SetPostavkeKarte(prije) })
	models.SetPostavkeKarte(models.ZadanePostavkeKarte())

	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "karte.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	rec := ledger.New(baza, "test")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	stationRepo, readingRepo := repository.NewStationRepository(baza, rec), repository.NewReadingRepository(baza, rec)
	akti := service.NewAktService(repository.NewAktiRepository(baza, rec), stationRepo, repository.NewSectionRepository(baza, rec),
		repository.NewTerritoryRepository(baza, rec), readingRepo, users, service.NewEpisodeService(repository.NewEpisodeRepository(baza, rec), readingRepo, stationRepo), "test")

	templatesFS, _ := fs.Sub(webassets.Files, "templates") // mapa je ugrađena, ParseFS niže javlja ako je nema
	tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("administracija_karte.html")...)
	if err != nil {
		t.Fatal(err)
	}
	h := NewAktiHandler(func() *service.AktService { return akti }, users, nil, nil, nil, nil, nil)
	h.SetPostavkeKarte(tp)
	uprava := &models.User{FullName: "Uprava", IsGlobalAdmin: true}
	zovi := func(metoda string, v url.Values, rukovatelj http.HandlerFunc) *httptest.ResponseRecorder {
		r := httptest.NewRequest(metoda, "/administracija/karte", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		c := context.WithValue(context.WithValue(r.Context(), contextKeyUser, uprava), contextKeyPerms, &models.UserPermissions{IsGlobalAdmin: true})
		w := httptest.NewRecorder()
		rukovatelj(w, r.WithContext(c))
		return w
	}

	// bez prijave i bez akata stranica se ne prikazuje
	r := httptest.NewRequest(http.MethodGet, "/administracija/karte", nil)
	w := httptest.NewRecorder()
	h.ShowPostavkeKarte(w, r)
	if strings.Contains(w.Body.String(), "Objekti na karti") {
		t.Error("stranica bez prijave")
	}
	w = httptest.NewRecorder()
	NewAktiHandler(func() *service.AktService { return nil }, users, nil, nil, nil, nil, nil).ShowPostavkeKarte(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("bez akata: %d", w.Code)
	}
	NewAktiHandler(func() *service.AktService { return nil }, users, nil, nil, nil, nil, nil).HandlePostavkeKarte(httptest.NewRecorder(), r)
	h2 := NewAktiHandler(func() *service.AktService { return akti }, users, nil, nil, nil, nil, nil)
	h2.SetPostavkeKarte(template.Must(template.New("prazan").Parse(`{{define "x"}}{{end}}`)))
	if w := zovi(http.MethodGet, nil, h2.ShowPostavkeKarte); w.Code != http.StatusInternalServerError {
		t.Errorf("bez predloška: %d", w.Code)
	}

	// stranica pokazuje kartu bez interneta iz zasebne postavke
	prijeK := models.VektorskaKartaPrograma()
	t.Cleanup(func() { models.SetVektorskaKarta(prijeK) })
	opis := models.VektorskaKarta{Naziv: "Baranja", Otisak: strings.Repeat("ab", 32), Bajtova: 150, Obuhvat: [4]float64{18.3, 45.5, 18.95, 45.95}, NajviseZ: 15}
	if err := akti.SpremiKartuBezInterneta(context.Background(), &models.UserPermissions{IsGlobalAdmin: true}, &opis); err != nil {
		t.Fatal(err)
	}
	if s := zovi(http.MethodGet, nil, h.ShowPostavkeKarte).Body.String(); !strings.Contains(s, "<td>Baranja</td>") || !strings.Contains(s, "nije učitana") {
		t.Error("stranica ne pokazuje kartu bez interneta")
	}

	w = zovi(http.MethodGet, nil, h.ShowPostavkeKarte)
	stranica := w.Body.String()
	for _, zeli := range []string{`name="boja_NASIP" value="#e8590c"`, `name="podloga_0_naziv" value="Satelit"`, `name="podloga_1_naziv" value=""`,
		`<option value="crpna-stanica" selected>`, `id="postavke-karte" type="application/json"`} {
		if !strings.Contains(stranica, zeli) {
			t.Errorf("stranica nema %s", zeli)
		}
	}

	// obrazac: brana smeđa s ikonom, ustava u boji teme ostaje bez svoje boje,
	// satelit ostaje, nova WMS podloga; prazan redak se preskače
	v := url.Values{
		"boja_BRANA": {"#8B5A2B"}, "debljina_BRANA": {"7"}, "ikona_BRANA": {"brana"}, "tocka": {"krug"}, "velicina_tocke": {"40"},
		"boja_USTAVA": {models.ZadanaSvijetla.Glavna}, "debljina_USTAVA": {"5"},
		"podloga_0_naziv": {"Satelit"}, "podloga_0_vrsta": {"xyz"}, "podloga_0_url": {models.SatelitEsri.URL}, "podloga_0_najvise_z": {"19"},
		"podloga_1_naziv": {"DOF"}, "podloga_1_vrsta": {"wms"}, "podloga_1_url": {"https://wms.example/wms"}, "podloga_1_slojevi": {"OI"},
	}
	w = zovi(http.MethodPost, v, h.HandlePostavkeKarte)
	if loc := mustUnescape(w.Header().Get("Location")); !strings.Contains(loc, "success=") {
		t.Fatalf("spremanje: %s", loc)
	}
	p := models.PostavkeKartePrograma()
	if p.Stil(models.StructureKindDam) != (models.StilObjekta{Boja: "#8b5a2b", Debljina: 7, Ikona: "brana"}) {
		t.Errorf("brana: %+v", p.Stil(models.StructureKindDam))
	}
	if _, ima := p.Objekti[models.StructureKindSluice]; ima {
		t.Errorf("ustava u boji teme spremljena kao svoja boja: %+v", p.Objekti[models.StructureKindSluice])
	}
	if !p.SKrugom || p.Velicina() != 40 {
		t.Errorf("točka: krug %v, %d px", p.SKrugom, p.Velicina())
	}
	if len(p.Podloge) != 2 || p.Podloge[1].Slojevi != "OI" {
		t.Errorf("podloge: %+v", p.Podloge)
	}
	if js := postavkeKarteZaStranicu(); js == nil {
		t.Error("stranica ne dobiva postavke")
	}

	v.Set("podloga_1_slojevi", "")
	w = zovi(http.MethodPost, v, h.HandlePostavkeKarte)
	if loc := mustUnescape(w.Header().Get("Location")); !strings.Contains(loc, "DOF: za WMS upišite imena slojeva") {
		t.Errorf("loša podloga: %s", loc)
	}

	w = zovi(http.MethodPost, url.Values{"radnja": {"zadano"}}, h.HandlePostavkeKarte)
	if loc := mustUnescape(w.Header().Get("Location")); !strings.Contains(loc, "zadane postavke") || models.PostavkeKartePrograma().Podloge[0] != models.SatelitEsri {
		t.Errorf("vrati zadano: %s", loc)
	}

	// vlastita ikona: učitava se iz SVG-a, bira uz vrstu, poslužuje
	// pročišćena, a brisanjem vrsta dobiva zadanu ikonu
	ucitaj := func(naziv, svg string) string {
		var b bytes.Buffer
		mw := multipart.NewWriter(&b)
		_ = mw.WriteField("naziv", naziv) // bytes.Buffer ne vraća grešku
		fw, err := mw.CreateFormFile("datoteka", "ikona.svg")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(svg)); err != nil {
			t.Fatal(err)
		}
		_ = mw.Close()
		r := httptest.NewRequest(http.MethodPost, "/administracija/karte/ikona", &b)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		c := context.WithValue(context.WithValue(r.Context(), contextKeyUser, uprava), contextKeyPerms, &models.UserPermissions{IsGlobalAdmin: true})
		w := httptest.NewRecorder()
		h.HandleVlastitaIkona(w, r.WithContext(c))
		return mustUnescape(w.Header().Get("Location"))
	}
	if loc := ucitaj("Crpka velika", `<svg viewBox="0 0 24 24"><circle cx="9" cy="13" r="6"/><script>x()</script></svg>`); !strings.Contains(loc, "je učitana") {
		t.Fatalf("učitavanje: %s", loc)
	}
	if loc := ucitaj("Prazna", `<svg></svg>`); !strings.Contains(loc, "Ikona nije učitana: u ikoni nema nijednog oblika") {
		t.Errorf("prazna ikona: %s", loc)
	}
	v.Set("podloga_1_slojevi", "OI")
	v.Set("ikona_CRPNA_STANICA", "v-crpka-velika")
	if loc := mustUnescape(zovi(http.MethodPost, v, h.HandlePostavkeKarte).Header().Get("Location")); !strings.Contains(loc, "success=") {
		t.Fatalf("izbor vlastite ikone: %s", loc)
	}
	if st := models.PostavkeKartePrograma().Stil(models.StructureKindPumpingStation); st.Ikona != "v-crpka-velika" {
		t.Errorf("crpna stanica: %+v", st)
	}
	if !strings.Contains(zovi(http.MethodGet, nil, h.ShowPostavkeKarte).Body.String(), `<option value="v-crpka-velika" selected>Crpka velika</option>`) {
		t.Error("vlastita ikona nije u izboru")
	}
	sw := httptest.NewRecorder()
	ServeVlastiteIkone(sw, httptest.NewRequest(http.MethodGet, adresaVlastitihIkona(models.PostavkeKartePrograma()), nil))
	if tijelo := sw.Body.String(); !strings.Contains(tijelo, `<symbol id="v-crpka-velika"`) || strings.Contains(tijelo, "script") ||
		sw.Header().Get("Content-Type") != "image/svg+xml; charset=utf-8" || !strings.Contains(sw.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("vlastite ikone: %v %s", sw.Header(), tijelo)
	}
	sw = httptest.NewRecorder()
	ServeVlastiteIkone(sw, httptest.NewRequest(http.MethodGet, "/karta/ikone.svg?v=stari", nil))
	if sw.Header().Get("Cache-Control") != "no-cache" {
		t.Error("stari otisak se drži u pregledniku")
	}
	w = zovi(http.MethodPost, url.Values{"ime": {"v-crpka-velika"}}, h.HandleObrisiIkonu)
	if loc := mustUnescape(w.Header().Get("Location")); !strings.Contains(loc, "maknuta") {
		t.Errorf("brisanje ikone: %s", loc)
	}
	if p := models.PostavkeKartePrograma(); len(p.Ikone) != 0 || p.Stil(models.StructureKindPumpingStation).Ikona != "crpna-stanica" {
		t.Errorf("nakon brisanja: %+v", p.Stil(models.StructureKindPumpingStation))
	}
	bez := NewAktiHandler(func() *service.AktService { return nil }, users, nil, nil, nil, nil, nil)
	for _, f := range []http.HandlerFunc{bez.HandleVlastitaIkona, bez.HandleObrisiIkonu} {
		if w := zovi(http.MethodPost, nil, f); w.Code != http.StatusServiceUnavailable {
			t.Errorf("bez akata: %d", w.Code)
		}
	}
	if loc := mustUnescape(zovi(http.MethodPost, url.Values{}, h.HandleVlastitaIkona).Header().Get("Location")); !strings.Contains(loc, "nije stigla") {
		t.Errorf("bez datoteke: %s", loc)
	}

	// bez baze postavki: greška se vraća na stranicu
	if _, err := baza.Exec(`ALTER TABLE postavke RENAME TO postavke_nema`); err != nil {
		t.Fatal(err)
	}
	for _, x := range []url.Values{{"radnja": {"zadano"}}, v} {
		if loc := mustUnescape(zovi(http.MethodPost, x, h.HandlePostavkeKarte).Header().Get("Location")); !strings.Contains(loc, "error=") {
			t.Errorf("nedostupna postavka: %s", loc)
		}
	}
	if loc := mustUnescape(zovi(http.MethodPost, url.Values{"ime": {"v-x"}}, h.HandleObrisiIkonu).Header().Get("Location")); !strings.Contains(loc, "error=") {
		t.Errorf("brisanje ikone bez baze: %s", loc)
	}
}
