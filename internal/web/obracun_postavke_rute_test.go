package web

import (
	"context"
	"html/template"
	"io/fs"
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

	"gocop/internal/obracun"
	"time"
)

// Postavke obračuna kroz rute, kao administrator: stranica dolazi napunjena
// zakonom i obrascem, dan žalosti se doda, koeficijent promijeni, i oboje
// se odmah vidi.
func TestPostavkeObracunaKrozRute(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewObracunRepository(baza, ledger.New(baza, "test"))
	if err := repo.Osiguraj(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc := service.NewObracunService(repo)
	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tmpl, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("obracun_postavke.html")...)
	if err != nil {
		t.Fatal(err)
	}
	h := NewObracunPostavkeHandler(func() *service.ObracunService { return svc }, tmpl)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /administracija/obracun", h.ShowPostavke)
	mux.HandleFunc("POST /administracija/obracun/blagdani", h.HandleSpremiBlagdan)
	mux.HandleFunc("POST /administracija/obracun/blagdani/{id}/makni", h.HandleMakniBlagdan)
	mux.HandleFunc("POST /administracija/obracun/koeficijenti", h.HandleSpremiKoeficijente)
	mux.HandleFunc("POST /administracija/obracun/radno-vrijeme", h.HandleSpremiRadnoVrijeme)

	admin := &models.UserPermissions{IsGlobalAdmin: true}
	zovi := func(metoda, putanja string, obrazac url.Values) *httptest.ResponseRecorder {
		t.Helper()
		var r *http.Request
		if obrazac != nil {
			r = httptest.NewRequest(metoda, putanja, strings.NewReader(obrazac.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			r = httptest.NewRequest(metoda, putanja, nil)
		}
		ctx := context.WithValue(r.Context(), contextKeyUser, &models.User{FullName: "Admin"})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(context.WithValue(ctx, contextKeyPerms, admin)))
		return w
	}
	mora := func(w *httptest.ResponseRecorder, kod int, sto string, dijelovi ...string) {
		t.Helper()
		if w.Code != kod {
			t.Fatalf("%s: %d, a mora biti %d\n%s", sto, w.Code, kod, w.Body.String())
		}
		for _, d := range dijelovi {
			if !strings.Contains(w.Body.String(), d) {
				t.Errorf("%s: nema %q", sto, d)
			}
		}
	}

	mora(zovi(http.MethodGet, "/administracija/obracun?godina=2026", nil), http.StatusOK, "stranica",
		"Nova godina", "Tijelovo", "60 dana", "do 2019.", "Četvrtak 4.6.", `value="1,85"`, `value="2,55"`)

	w := zovi(http.MethodPost, "/administracija/obracun/blagdani", url.Values{
		"naziv": {"Dan žalosti"}, "vrsta": {"JEDNOKRATNI"}, "datum": {"2026-03-03"}})
	if l := w.Header().Get("Location"); w.Code != http.StatusSeeOther || !strings.Contains(l, "success=") {
		t.Fatalf("dan žalosti: %d %s", w.Code, l)
	}
	mora(zovi(http.MethodGet, "/administracija/obracun?godina=2026", nil), http.StatusOK, "s danom žalosti", "Dan žalosti", "3.3.2026.", "Utorak 3.3.")

	obrazac := url.Values{}
	for _, m := range []string{"URED", "TEREN"} {
		for _, r := range []string{"RRV", "DRD", "NRD", "VID", "VIN", "BLD", "BLN"} {
			obrazac.Set("k_"+m+"_"+r, "1")
		}
	}
	obrazac.Set("k_URED_BLD", "2,5")
	w = zovi(http.MethodPost, "/administracija/obracun/koeficijenti", obrazac)
	if l := w.Header().Get("Location"); w.Code != http.StatusSeeOther || !strings.Contains(l, "success=") {
		t.Fatalf("koeficijenti: %d %s", w.Code, l)
	}
	mora(zovi(http.MethodGet, "/administracija/obracun", nil), http.StatusOK, "novi koeficijenti", `value="2,5"`)
	if k := svc.Koeficijenti(context.Background()); k["URED"]["BLD"] != 2.5 || k["TEREN"]["BLN"] != 1 {
		t.Errorf("koeficijenti poslije upisa: %v", k)
	}

	// Radno vrijeme: zadano 7:30–15:30, upis 8–16 mijenja legendu i razvrstavanje, krivo se odbija
	mora(zovi(http.MethodGet, "/administracija/obracun", nil), http.StatusOK, "radno vrijeme", `value="07:30"`, "redovno 7:30–15:30")
	w = zovi(http.MethodPost, "/administracija/obracun/radno-vrijeme", url.Values{"od": {"08:00"}, "do": {"16:00"}})
	if l := w.Header().Get("Location"); w.Code != http.StatusSeeOther || !strings.Contains(l, "success=") {
		t.Fatalf("radno vrijeme: %d %s", w.Code, l)
	}
	mora(zovi(http.MethodGet, "/administracija/obracun", nil), http.StatusOK, "novo radno vrijeme", `value="08:00"`, "redovno 8–16", "dnevni 6–8 i 16–22")
	if rv := svc.RadnoVrijeme(context.Background()); rv.Od != 8*60 || rv.Do != 16*60 {
		t.Errorf("radno vrijeme poslije upisa: %+v", rv)
	}
	if sati := obracun.Razvrstaj(time.Date(2026, 9, 11, 7, 0, 0, 0, models.Zagreb), time.Date(2026, 9, 11, 16, 0, 0, 0, models.Zagreb), svc.Kalendar(context.Background())); sati[obracun.RRV] != 8*time.Hour {
		t.Errorf("kalendar ne nosi radno vrijeme: %v", sati)
	}
	w = zovi(http.MethodPost, "/administracija/obracun/radno-vrijeme", url.Values{"od": {"16:00"}, "do": {"08:00"}})
	if l := w.Header().Get("Location"); !strings.Contains(l, "error=") {
		t.Errorf("obrnuto radno vrijeme prošlo: %s", l)
	}

	// Krivo: stalni bez dana
	w = zovi(http.MethodPost, "/administracija/obracun/blagdani", url.Values{"naziv": {"Nešto"}, "vrsta": {"STALNI"}})
	if l := w.Header().Get("Location"); !strings.Contains(l, "error=") {
		t.Errorf("stalni bez dana prošao: %s", l)
	}
}
