package web

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Žig centra mijenja uprava sektora, a ona modul Administracija zadano nema.
// Zato je žig na stranici sektora (Odluke (30. 10. 2026.)): rukovoditelj
// sektora ga ondje vidi i sprema, kroz pravi poslužitelj, s prijavom i
// vidljivošću modula. Tko nije uprava sektora, vidi samo podatke sektora.
func TestUpravaSektoraSpremaZigSaStraniceSektora(t *testing.T) {
	o := novaOkolinaSastavljaca(t)
	sektor := "P"
	uprava := &models.User{ID: uuid.New(), Username: "ppavic", FullName: "Petra Pavić", IsActive: true}
	if err := o.userRepo.CreateUser(uprava, &models.Duty{Title: "Rukovoditelj sektora P", Role: models.RoleSectorLeader,
		ScopeType: models.ScopeSector, SectorID: &sektor, IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	o.osobe["uprava sektora"] = uprava
	s, err := NewServer("", o.auth, o.users, nil, nil, o.stations, nil, o.structures, o.readings, nil, o.moduli, nil, nil, nil,
		SupportContact{}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.SetAkti(o.akti)
	posalji := func(tko string, r *http.Request) *httptest.ResponseRecorder {
		t.Helper()
		sesija := &models.Session{UserID: o.osobe[tko].ID, IPAddress: "192.168.1.50", UserAgent: "test", ExpiresAt: time.Now().Add(time.Hour)}
		if err := o.sesije.CreateSession(sesija); err != nil {
			t.Fatal(err)
		}
		r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: sesija.ID.String()})
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, r)
		return w
	}
	zig := func(tko string) *httptest.ResponseRecorder {
		t.Helper()
		slika := image.NewRGBA(image.Rect(0, 0, 120, 120))
		for x := 0; x < 120; x++ {
			slika.Set(x, 60, color.RGBA{0, 0, 200, 255})
		}
		var slikaPNG bytes.Buffer
		if err := png.Encode(&slikaPNG, slika); err != nil {
			t.Fatal(err)
		}
		var tijelo bytes.Buffer
		mw := multipart.NewWriter(&tijelo)
		f, err := mw.CreateFormFile("slika", "zig.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(slikaPNG.Bytes()); err != nil {
			t.Fatal(err)
		}
		if err := mw.Close(); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "/organizacija/sektori/P/zig", &tijelo)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		return posalji(tko, r)
	}

	// polazište: Administracija se upravi sektora ne otvara
	if w := posalji("uprava sektora", httptest.NewRequest(http.MethodGet, "/administracija", nil)); w.Code != http.StatusForbidden {
		t.Fatalf("uprava sektora bez modula Administracija otvorila je Administraciju: %d", w.Code)
	}
	w := posalji("uprava sektora", httptest.NewRequest(http.MethodGet, "/organizacija/sektori/P", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("stranica sektora: %d %s", w.Code, w.Body.String())
	}
	for _, mora := range []string{"Sektor P", "COP Primjerovo", `id="zig"`, `action="/organizacija/sektori/P/zig"`, "Žig još nije skeniran"} {
		if !strings.Contains(w.Body.String(), mora) {
			t.Errorf("stranica sektora upravi sektora nema %q", mora)
		}
	}
	if w := zig("uprava sektora"); w.Code != http.StatusSeeOther || !strings.Contains(mustUnescape(w.Header().Get("Location")), "success=") {
		t.Fatalf("spremanje žiga: %d %s", w.Code, mustUnescape(w.Header().Get("Location")))
	}
	if z := o.akti.Zig(t.Context(), "P"); z == nil || z.Uredio != "Petra Pavić" {
		t.Fatalf("žig nije spremljen: %+v", z)
	}
	if w := posalji("uprava sektora", httptest.NewRequest(http.MethodGet, "/organizacija/sektori/P", nil)); !strings.Contains(w.Body.String(), "/organizacija/sektori/P/zig/slika?t=") {
		t.Error("stranica sektora ne pokazuje spremljeni žig")
	}
	if w := posalji("uprava sektora", httptest.NewRequest(http.MethodGet, "/organizacija/sektori/P/zig/slika", nil)); w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" {
		t.Errorf("slika žiga upravi sektora: %d %s", w.Code, w.Header().Get("Content-Type"))
	}

	// rukovoditelj područja vidi podatke sektora, ali ne i žig
	w = posalji("rukovoditelj", httptest.NewRequest(http.MethodGet, "/organizacija/sektori/P", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "COP Primjerovo") || strings.Contains(w.Body.String(), `id="zig"`) {
		t.Errorf("stranica sektora rukovoditelju područja: %d, žig %v", w.Code, strings.Contains(w.Body.String(), `id="zig"`))
	}
	if w := posalji("rukovoditelj", httptest.NewRequest(http.MethodGet, "/organizacija/sektori/P/zig/slika", nil)); w.Code != http.StatusForbidden {
		t.Errorf("slika žiga rukovoditelju područja: %d", w.Code)
	}
	if w := zig("rukovoditelj"); !strings.Contains(mustUnescape(w.Header().Get("Location")), "error=") {
		t.Errorf("rukovoditelj područja spremio je žig: %s", mustUnescape(w.Header().Get("Location")))
	}
	if w := posalji("uprava sektora", httptest.NewRequest(http.MethodGet, "/organizacija/sektori/NEMA", nil)); w.Code != http.StatusNotFound {
		t.Errorf("nepostojeći sektor: %d", w.Code)
	}

	// uklanjanje: rukovoditelj područja ne smije, uprava sektora smije
	obrazac := func(tko string, polja map[string]string) string {
		t.Helper()
		var tijelo bytes.Buffer
		mw := multipart.NewWriter(&tijelo)
		for k, v := range polja {
			if err := mw.WriteField(k, v); err != nil {
				t.Fatal(err)
			}
		}
		if err := mw.Close(); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "/organizacija/sektori/P/zig", &tijelo)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		return mustUnescape(posalji(tko, r).Header().Get("Location"))
	}
	if loc := obrazac("uprava sektora", nil); !strings.Contains(loc, "error=Odaberite sliku žiga") || !strings.HasPrefix(loc, "/organizacija/sektori/P?") {
		t.Errorf("obrazac bez slike: %s", loc)
	}
	if loc := obrazac("rukovoditelj", map[string]string{"obrisi": "1"}); !strings.Contains(loc, "error=") || o.akti.Zig(t.Context(), "P") == nil {
		t.Errorf("rukovoditelj područja uklonio je žig: %s", loc)
	}
	if loc := obrazac("uprava sektora", map[string]string{"obrisi": "1"}); !strings.Contains(loc, "success=Žig je uklonjen") || o.akti.Zig(t.Context(), "P") != nil {
		t.Errorf("uklanjanje žiga: %s", loc)
	}
	if w := posalji("uprava sektora", httptest.NewRequest(http.MethodGet, "/organizacija/sektori/P/zig/slika", nil)); w.Code != http.StatusNotFound {
		t.Errorf("slika uklonjenog žiga: %d", w.Code)
	}
}

// Stara adresa stranice žiga vodi na žig na stranici sektora; bez sektora
// u adresi na prvi sektor kojim korisnik upravlja, a bez ijednog na popis
func TestStaraAdresaZigaVodiNaSektor(t *testing.T) {
	o := novaOkolinaSastavljaca(t)
	h := NewAktiHandler(func() *service.AktService { return o.akti }, o.users, nil, nil, nil, nil, nil)
	for _, c := range []struct {
		put   string
		perms *models.UserPermissions
		zeli  string
	}{
		{"/administracija/zig?sektor=P", &models.UserPermissions{IsGlobalAdmin: true}, "/organizacija/sektori/P#zig"},
		{"/administracija/zig", &models.UserPermissions{AdminSectors: map[string]bool{"P": true}}, "/organizacija/sektori/P#zig"},
		{"/administracija/zig", &models.UserPermissions{}, "/organizacija"},
	} {
		r := httptest.NewRequest(http.MethodGet, c.put, nil)
		r = r.WithContext(context.WithValue(context.WithValue(r.Context(), contextKeyUser, o.osobe["dežurni"]), contextKeyPerms, c.perms))
		w := httptest.NewRecorder()
		h.ShowZig(w, r)
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != c.zeli {
			t.Errorf("%s uz %+v: %d %s, želi %s", c.put, c.perms, w.Code, w.Header().Get("Location"), c.zeli)
		}
	}
	// bez servisa akata žig se ne sprema ni ne pokazuje, a podaci sektora da
	bez := NewAktiHandler(func() *service.AktService { return nil }, o.users, nil, nil, nil, nil, nil)
	bez.SetZig(o.tmpl("sektor.html"))
	for _, rukovatelj := range []http.HandlerFunc{bez.HandleZig, bez.ZigSlika} {
		r := httptest.NewRequest(http.MethodGet, "/organizacija/sektori/P/zig/slika", nil)
		r.SetPathValue("id", "P")
		r = r.WithContext(context.WithValue(context.WithValue(r.Context(), contextKeyUser, o.osobe["dežurni"]), contextKeyPerms, &models.UserPermissions{IsGlobalAdmin: true}))
		w := httptest.NewRecorder()
		rukovatelj(w, r)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("bez servisa akata: %d", w.Code)
		}
	}
	r0 := httptest.NewRequest(http.MethodGet, "/organizacija/sektori/P", nil)
	r0.SetPathValue("id", "P")
	r0 = r0.WithContext(context.WithValue(context.WithValue(r0.Context(), contextKeyUser, o.osobe["dežurni"]), contextKeyPerms, &models.UserPermissions{IsGlobalAdmin: true}))
	w0 := httptest.NewRecorder()
	bez.ShowSektor(w0, r0)
	if w0.Code != http.StatusOK || !strings.Contains(w0.Body.String(), "COP Primjerovo") || strings.Contains(w0.Body.String(), `id="zig"`) {
		t.Errorf("stranica sektora bez servisa akata: %d", w0.Code)
	}
	// pokvaren obrazac na staroj adresi vraća na popis organizacije
	r := httptest.NewRequest(http.MethodPost, "/administracija/zig", strings.NewReader("x"))
	r.Header.Set("Content-Type", "multipart/form-data; boundary=nema")
	r = r.WithContext(context.WithValue(context.WithValue(r.Context(), contextKeyUser, o.osobe["dežurni"]), contextKeyPerms, &models.UserPermissions{IsGlobalAdmin: true}))
	w := httptest.NewRecorder()
	h.HandleZig(w, r)
	if loc := w.Header().Get("Location"); !strings.HasPrefix(loc, "/organizacija?error=") {
		t.Errorf("pokvaren obrazac: %s", loc)
	}
}
