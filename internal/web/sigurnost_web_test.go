package web

import (
	"context"
	"encoding/json"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
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

// probniPosluzitelj je poslužitelj sa svim zaštitnim slojevima i dvije rute
func probniPosluzitelj() http.Handler {
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /promjena", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("upisano")) })
	s.mux.HandleFunc("GET /citanje", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("procitano")) })
	s.mux.HandleFunc("GET /privitak", func(w http.ResponseWriter, r *http.Request) {
		posluziTudjuDatoteku(w, "image/png", "", true)
		_, _ = w.Write([]byte("png"))
	})
	return s.Handler()
}

// Izmjenu koju je pokrenula tuđa stranica preglednik javlja zaglavljima
// Sec-Fetch-Site ili Origin; takva se odbija. Prolaze zahtjevi s iste
// stranice u sva tri načina rada (tunel, localhost, lokalna mreža), alati
// bez tih zaglavlja i svako čitanje.
func TestZastitaOdTudjihStranica(t *testing.T) {
	h := probniPosluzitelj()
	slucajevi := []struct {
		ime     string
		metoda  string
		put     string
		host    string
		udaljen string
		zag     map[string]string
		kod     int
	}{
		{"tuđa stranica (Sec-Fetch-Site)", "POST", "/promjena", "cop-osijek.com", "127.0.0.1:5000",
			map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://zlo.hr", "CF-Connecting-IP": "203.0.113.9", "X-Forwarded-Proto": "https"}, 403},
		{"ista lokacija, drugi poslužitelj (Sec-Fetch-Site)", "POST", "/promjena", "192.168.1.2:1160", "192.168.1.10:5000",
			map[string]string{"Sec-Fetch-Site": "same-site"}, 403},
		{"tuđi Origin bez Sec-Fetch-Site", "POST", "/promjena", "cop-osijek.com", "127.0.0.1:5000",
			map[string]string{"Origin": "https://zlo.hr"}, 403},
		{"drugi poslužitelj na localhostu", "POST", "/promjena", "localhost:8080", "127.0.0.1:5000",
			map[string]string{"Origin": "http://localhost:3000"}, 403},
		{"tunel, ista stranica", "POST", "/promjena", "cop-osijek.com", "127.0.0.1:5000",
			map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "https://cop-osijek.com", "CF-Connecting-IP": "203.0.113.9", "X-Forwarded-Proto": "https"}, 200},
		{"tunel, stari preglednik bez Sec-Fetch-Site", "POST", "/promjena", "cop-osijek.com", "127.0.0.1:5000",
			map[string]string{"Origin": "https://cop-osijek.com", "CF-Connecting-IP": "203.0.113.9", "X-Forwarded-Proto": "https"}, 200},
		{"localhost:8080", "POST", "/promjena", "localhost:8080", "127.0.0.1:5000",
			map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://localhost:8080"}, 200},
		{"lokalna mreža preko HTTP-a (bez Sec-Fetch-Site)", "POST", "/promjena", "192.168.1.2:1160", "192.168.1.10:5000",
			map[string]string{"Origin": "http://192.168.1.2:1160"}, 200},
		{"adresa upisana ručno (Sec-Fetch-Site: none)", "POST", "/promjena", "cop-osijek.com", "127.0.0.1:5000",
			map[string]string{"Sec-Fetch-Site": "none"}, 200},
		{"alat bez zaglavlja preglednika", "POST", "/promjena", "cop-osijek.com", "127.0.0.1:5000", nil, 200},
		{"čitanje s tuđe stranice", "GET", "/citanje", "cop-osijek.com", "127.0.0.1:5000",
			map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://zlo.hr"}, 200},
		{"tunel razmjene: GET s Originom druge domene", "GET", "/citanje", "cop-osijek.com", "127.0.0.1:5000",
			map[string]string{"Origin": "https://drugi-cvor.hr"}, 200},
	}
	for _, c := range slucajevi {
		r := httptest.NewRequest(c.metoda, "http://"+c.host+c.put, strings.NewReader("a=1"))
		r.RemoteAddr = c.udaljen
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for k, v := range c.zag {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.kod {
			t.Errorf("%s: %d, očekivano %d (%s)", c.ime, w.Code, c.kod, w.Body.String())
			continue
		}
		if c.kod == 403 && !strings.Contains(w.Body.String(), "druga stranica") {
			t.Errorf("%s: odbijanje bez objašnjenja: %q", c.ime, w.Body.String())
		}
	}

	// skripta koja čeka JSON dobije JSON
	r := httptest.NewRequest("POST", "http://cop-osijek.com/promjena", strings.NewReader(`{"approved":true}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var odg struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if w.Code != 403 || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") || json.Unmarshal(w.Body.Bytes(), &odg) != nil || odg.Success || odg.Error == "" {
		t.Errorf("JSON odbijanje: %d %q %s", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
}

// Svaki odgovor nosi sigurnosna zaglavlja; HSTS samo kad je zahtjev stigao
// HTTPS-om kroz pouzdanog posrednika, a rukovatelj smije zadati svoja.
func TestSigurnosnaZaglavlja(t *testing.T) {
	h := probniPosluzitelj()
	zovi := func(udaljen string, zag map[string]string, put string) http.Header {
		r := httptest.NewRequest("GET", "http://cop-osijek.com"+put, nil)
		r.RemoteAddr = udaljen
		for k, v := range zag {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Header()
	}
	z := zovi("192.168.1.10:5000", nil, "/citanje")
	for ime, want := range map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "same-origin",
		"X-Frame-Options":         "DENY",
		"Content-Security-Policy": "frame-ancestors 'none'; object-src 'none'; base-uri 'self'; form-action 'self'",
		"Permissions-Policy":      "camera=(), microphone=(), payment=(), usb=(), geolocation=(self)",
	} {
		if z.Get(ime) != want {
			t.Errorf("%s = %q, očekivano %q", ime, z.Get(ime), want)
		}
	}
	if z.Get("Strict-Transport-Security") != "" {
		t.Error("HSTS na običnom HTTP-u")
	}
	// tunel: cloudflared javlja HTTPS
	if v := zovi("127.0.0.1:5000", map[string]string{"CF-Connecting-IP": "203.0.113.9", "X-Forwarded-Proto": "https"}, "/citanje").Get("Strict-Transport-Security"); v != "max-age=31536000" {
		t.Errorf("HSTS kroz tunel: %q", v)
	}
	// X-Forwarded-Proto s javne adrese nije pouzdan
	if v := zovi("203.0.113.7:5000", map[string]string{"X-Forwarded-Proto": "https"}, "/citanje").Get("Strict-Transport-Security"); v != "" {
		t.Errorf("HSTS na podmetnut X-Forwarded-Proto: %q", v)
	}
	// rukovatelj privitka zadaje strožu politiku
	if v := zovi("192.168.1.10:5000", nil, "/privitak").Get("Content-Security-Policy"); v != politikaDatoteke {
		t.Errorf("politika privitka: %q", v)
	}
}

// JSON je samo application/json: tuđa stranica bez pitanja smije poslati
// text/plain, pa "text/plain; x=application/json" ne smije proći kao JSON.
func TestDecodeBodySamoPraviJSON(t *testing.T) {
	for vrsta, json := range map[string]bool{
		"application/json":                                    true,
		"application/json; charset=utf-8":                     true,
		"Application/JSON":                                    true,
		"text/plain; x=application/json":                      false,
		"text/plain":                                          false,
		"application/jsonx":                                   false,
		"application/x-www-form-urlencoded; application/json": false,
	} {
		r := httptest.NewRequest("POST", "/api/uparivanje/confirm", strings.NewReader(`{"approved":true}`))
		r.Header.Set("Content-Type", vrsta)
		var v struct {
			Approved bool `json:"approved"`
		}
		_ = decodeBody(r, &v)
		if v.Approved != json {
			t.Errorf("%q: pročitano kao JSON = %v, očekivano %v", vrsta, v.Approved, json)
		}
	}
}

func bazaVoda(t *testing.T) (*service.WatercourseService, *repository.WatercourseRepository, *WatercoursesHandler) {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "voda.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test-node")
	repo := repository.NewWatercourseRepository(baza, rec)
	svc := service.NewWatercourseService(repo)
	h := NewWatercoursesHandler(svc, service.NewSectionService(repository.NewSectionRepository(baza, rec), service.NewSSEBroker()), nil)
	return svc, repo, h
}

// Geometriju vode upisuje samo JSON iz skripte stranice; običan obrazac,
// koji može poslati i tuđa stranica, ne upisuje ništa.
func TestGeometrijaVodeSamoJSONom(t *testing.T) {
	svc, repo, h := bazaVoda(t)
	ctx := context.WithValue(context.Background(), contextKeyPerms, &models.UserPermissions{IsGlobalAdmin: true})
	stara := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":{"type":"LineString","coordinates":[[18.5,45.7],[18.8,45.8]]}}]}`
	if err := repo.CreateWatercourse(context.Background(), &models.Watercourse{Code: "potok-proba", OfficialName: "potok Proba", Name: "Proba", Geometry: stara}); err != nil {
		t.Fatal(err)
	}
	nova := `{"type":"FeatureCollection","features":[]}`
	for _, vrsta := range []string{"application/x-www-form-urlencoded", "text/plain; x=application/json"} {
		tijelo := "code=potok-proba&geojson=" + nova
		if strings.HasPrefix(vrsta, "text/") {
			tijelo = `{"code":"potok-proba","geojson":"{}"}`
		}
		r := httptest.NewRequest("POST", "/api/watercourses/geometry", strings.NewReader(tijelo)).WithContext(ctx)
		r.Header.Set("Content-Type", vrsta)
		w := httptest.NewRecorder()
		h.HandleUpdateWatercourseGeometryAPI(w, r)
		if w.Code != http.StatusUnsupportedMediaType {
			t.Errorf("%s: %d %s", vrsta, w.Code, w.Body.String())
		}
	}
	if v, _ := svc.GetWatercourse(context.Background(), "potok-proba"); v == nil || v.Geometry != stara {
		t.Errorf("geometrija je promijenjena bez JSON-a: %+v", v)
	}
}

// Geometrija je tekst kako je stigao (datoteka, razmjena); niz "</script>"
// u svojstvu ne smije zatvoriti podatkovnu oznaku na stranici vode.
func TestGeometrijaVodeNeIzvrsavaSkriptu(t *testing.T) {
	_, repo, h := bazaVoda(t)
	zlo := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"</script><script>alert(1)</script>","opis":"a & b <!--"},"geometry":{"type":"LineString","coordinates":[[18.5,45.7],[18.8,45.8]]}}]}`
	if err := repo.CreateWatercourse(context.Background(), &models.Watercourse{Code: "potok-zlo", OfficialName: "potok Zlo", Name: "Zlo", Geometry: zlo}); err != nil {
		t.Fatal(err)
	}
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("watercourse_detail.html")...)
	if err != nil {
		t.Fatal(err)
	}
	h.SetPageTemplates(tmpl, nil, nil)
	h.karta = func() KartaPostavke { return KartaPostavke{Plocice: "/karta/{z}/{x}/{y}.png", NajviseZ: 17} }
	r := httptest.NewRequest("GET", "/watercourses/potok-zlo", nil)
	r.SetPathValue("code", "potok-zlo")
	r = r.WithContext(context.WithValue(context.WithValue(r.Context(), contextKeyPerms, &models.UserPermissions{IsGlobalAdmin: true}), contextKeyUser, &models.User{FullName: "P"}))
	w := httptest.NewRecorder()
	h.ShowWatercourse(w, r)
	stranica := w.Body.String()
	if w.Code != 200 || !strings.Contains(stranica, `class="karta-geometrija-podaci"`) {
		t.Fatalf("stranica vode: %d\n%.600s", w.Code, stranica)
	}
	if strings.Contains(stranica, "<script>alert(1)") || strings.Contains(stranica, "a & b <!--") {
		t.Fatal("geometrija je izašla iz podatkovne oznake")
	}
	// podatak je i dalje isti JSON
	i := strings.Index(stranica, `class="karta-geometrija-podaci">`)
	podatak := stranica[i+len(`class="karta-geometrija-podaci">`):]
	podatak = podatak[:strings.Index(podatak, "</script>")]
	var fc struct {
		Features []struct {
			Properties map[string]string `json:"properties"`
		} `json:"features"`
	}
	if err := json.Unmarshal([]byte(podatak), &fc); err != nil || len(fc.Features) == 0 || fc.Features[0].Properties["name"] != "</script><script>alert(1)</script>" {
		t.Errorf("podatak geometrije: %v %.300s", err, podatak)
	}
	if _, ok := jsonZaSkriptu([]byte(`{"a":"</script>`)); ok {
		t.Error("neispravan JSON se prikazuje")
	}
}

// Tuđa datoteka se u stranici prikazuje samo kao obična slika; SVG, HTML i
// ostalo se preuzima kao application/octet-stream, uvijek s politikom
// koja ne da izvršiti ništa.
func TestPosluziTudjuDatoteku(t *testing.T) {
	slucajevi := []struct {
		vrsta   string
		ugradi  bool
		tip     string
		inline  bool
		opisano string
	}{
		{"image/png", true, "image/png", true, "png u pismu"},
		{"image/jpeg", true, "image/jpeg", true, "jpeg"},
		{"IMAGE/GIF", true, "image/gif", true, "gif velikim slovima"},
		{"image/webp", true, "image/webp", true, "webp"},
		{"image/png", false, "image/png", false, "png za preuzimanje"},
		{"image/svg+xml", true, "application/octet-stream", false, "svg"},
		{"text/html; charset=utf-8", true, "application/octet-stream", false, "html"},
		{"application/xhtml+xml", true, "application/octet-stream", false, "xhtml"},
		{"application/pdf", false, "application/pdf", false, "pdf"},
		{"application/pdf", true, "application/pdf", false, "pdf se ne ugrađuje"},
		{"", true, "application/octet-stream", false, "bez vrste"},
		{"nevaljalo//", true, "application/octet-stream", false, "neispravna vrsta"},
	}
	for _, c := range slucajevi {
		w := httptest.NewRecorder()
		posluziTudjuDatoteku(w, c.vrsta, "ime.x", c.ugradi)
		h := w.Header()
		if h.Get("Content-Type") != c.tip || strings.HasPrefix(h.Get("Content-Disposition"), "inline") != c.inline ||
			h.Get("Content-Security-Policy") != politikaDatoteke || h.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: %q %q %q", c.opisano, h.Get("Content-Type"), h.Get("Content-Disposition"), h.Get("Content-Security-Policy"))
		}
	}
	if vrstaZnaka("image/svg+xml") != "image/svg+xml" || vrstaZnaka("text/html") != "application/octet-stream" {
		t.Error("vrsta znaka organizacije")
	}
	if pathExt(`zlo.pdf"; x=`) != "" || pathExt("Obavijest.PDF") != ".pdf" {
		t.Error("nastavak imena privitka")
	}
}

// Preusmjeravanje nakon obrasca vodi samo na putanju u goCOP-u
func TestSigurnaPovratnaAdresa(t *testing.T) {
	for v, ok := range map[string]bool{
		"/vodocuvar/12":           true,
		"/readings?x=1#arhiva":    true,
		"/":                       true,
		"":                        false,
		"//zlo.hr":                false,
		"/\\zlo.hr":               false,
		"/\t/zlo.hr":              false,
		"/\n/zlo.hr":              false,
		"https://zlo.hr/":         false,
		"javascript:alert(1)":     false,
		"zlo.hr":                  false,
		"/a\\b":                   false,
		"http://cop-osijek.com/x": false,
	} {
		if sigurnaPutanja(v) != ok {
			t.Errorf("sigurnaPutanja(%q) = %v", v, !ok)
		}
	}
	ref := func(v string) *http.Request {
		r := httptest.NewRequest("POST", "http://cop-osijek.com/profile", nil)
		r.Header.Set("Referer", v)
		return r
	}
	for v, want := range map[string]string{
		"https://cop-osijek.com/users/5/edit?error=x": "/users/5/edit",
		"https://zlo.hr/users":                        "/",
		"https://cop-osijek.com//zlo.hr":              "/",
		"https://zlo.hr/territories/municipalities/3": "/",
		"": "/",
	} {
		if got := sigurnaPovratnaAdresa(ref(v), "/"); got != want {
			t.Errorf("Referer %q: %q, očekivano %q", v, got, want)
		}
	}
	if got := backToMunicipality(ref("https://zlo.hr/territories/municipalities/3")); got != "/territories?tab=municipalities" {
		t.Errorf("povratak na općinu s tuđeg Referera: %q", got)
	}
	if got := backToMunicipality(ref("https://cop-osijek.com/territories/municipalities/3?x=1")); got != "/territories/municipalities/3" {
		t.Errorf("povratak na općinu: %q", got)
	}
	if got := povratnaPutanja("//zlo.hr", "/vodocuvar"); got != "/vodocuvar" {
		t.Errorf("natrag: %q", got)
	}
}

// Područja (tvrtke, telefoni) su samo za prijavljene, a ne za svakoga kroz
// tunel; obrazac djelatnika ih treba i bez modula Registri.
func TestPodrucjaSamoPrijavljenima(t *testing.T) {
	s, err := NewServer("", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, SupportContact{}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/areas?sector=B", nil))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Errorf("neprijavljen dobiva područja: %d %q", w.Code, w.Header().Get("Location"))
	}
	if m := moduleForPath("/api/areas"); m != "" {
		t.Errorf("područja traže modul %q; obrazac djelatnika bi bez njega dobio 403", m)
	}
}
