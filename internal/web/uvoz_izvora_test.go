package web

import (
	"bytes"
	"context"
	"html/template"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/arhiva"
	"gocop/internal/models"
	"gocop/internal/uvoz/izvori"
	webassets "gocop/web"
)

func predlozakVrata(t *testing.T) *template.Template {
	t.Helper()
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("base.html").Funcs(templateFuncs()).
		ParseFS(templatesFS, DijeloviPredloska("uvoz_niza.html")...)
	if err != nil {
		t.Fatal(err)
	}
	return tmpl
}

func zahtjevSKorisnikom(r *http.Request, u *models.User, p *models.UserPermissions) *http.Request {
	ctx := context.WithValue(r.Context(), contextKeyUser, u)
	ctx = context.WithValue(ctx, contextKeyPerms, p)
	return r.WithContext(ctx)
}

// Posebni izvor ide istim redom kao vrata: pregled ništa ne zapisuje, potvrda
// zapiše i izgradi baš tu letvu.
func TestPosebniIzvorPregledPaUpis(t *testing.T) {
	koren := t.TempDir()
	var izgradjene []string
	h := NewUvozHandler(func() string { return "" }, func() string { return koren },
		func(letva string, w io.Writer) (arhiva.Izvjestaj, error) {
			izgradjene = append(izgradjene, letva)
			return arhiva.Izvjestaj{}, nil
		}, predlozakVrata(t))
	u := &models.User{ID: uuid.New(), FullName: "Probni"}
	perms := &models.UserPermissions{IsGlobalAdmin: true}

	var tijelo bytes.Buffer
	mw := multipart.NewWriter(&tijelo)
	_ = mw.WriteField("format", "pegelonline")
	_ = mw.WriteField("letva", "kienstock")
	fw, _ := mw.CreateFormFile("datoteke", "kienstock.json")
	_, _ = fw.Write([]byte(`[{"timestamp":"2026-08-21T02:00:00+02:00","value":253},{"timestamp":"2026-08-21T03:00:00+02:00","value":255}]`))
	mw.Close()
	r := httptest.NewRequest("POST", "/administracija/uvoz-izvora/pregled", &tijelo)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	h.PregledIzvora(w, zahtjevSKorisnikom(r, u, perms))
	html := w.Body.String()
	if !strings.Contains(html, "što bi se upisalo") || !strings.Contains(html, "punih sati: 2") {
		t.Fatalf("pregled ne pokazuje što je pročitano:\n%s", html)
	}
	var zapisano []string
	_ = filepath.WalkDir(koren, func(p string, d os.DirEntry, _ error) error {
		if d != nil && !d.IsDir() {
			zapisano = append(zapisano, p)
		}
		return nil
	})
	if len(zapisano) != 0 {
		t.Fatalf("pregled je zapisao %v", zapisano)
	}
	id := regexp.MustCompile(`name="id" value="([^"]+)"`).FindStringSubmatch(html)
	if id == nil {
		t.Fatal("pregled nema broj za potvrdu")
	}

	r = httptest.NewRequest("POST", "/administracija/uvoz-izvora/upisi", strings.NewReader("id="+id[1]))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	h.UpisiIzvor(w, zahtjevSKorisnikom(r, u, perms))
	posao := regexp.MustCompile(`data-posao="([^"]+)"`).FindStringSubmatch(w.Body.String())
	if posao == nil {
		t.Fatalf("upis nije pokrenuo posao:\n%s", w.Body.String())
	}
	p, ima := h.poslovi.Nadi(posao[1], u.ID.String())
	if !ima {
		t.Fatal("posao se ne nalazi")
	}
	for i := 0; p.Traje() && i < 200; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if g := p.Greska(); g != "" {
		t.Fatalf("posao nije uspio: %s", g)
	}
	if len(izgradjene) != 1 || izgradjene[0] != "kienstock" {
		t.Errorf("izgrađeno %v, očekivano kienstock", izgradjene)
	}
	puts, _ := filepath.Glob(filepath.Join(koren, "dunav", "kienstock", "kienstock_pegelonline_vodostaj_satni_*.csv"))
	if len(puts) != 1 {
		t.Fatalf("niz nije zapisan pod zadanim slivom dunav: %v", puts)
	}
	// Potvrda vrijedi jednom.
	r = httptest.NewRequest("POST", "/administracija/uvoz-izvora/upisi", strings.NewReader("id="+id[1]))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	h.UpisiIzvor(w, zahtjevSKorisnikom(r, u, perms))
	if !strings.Contains(w.Body.String(), "Odabir je istekao") {
		t.Error("ista potvrda prošla je dvaput")
	}
}

// Godišnjak piše na više letava; smije se samo ako je svaka od njih tvoja.
func TestPosebniIzvorTraziPravoNaSveLetve(t *testing.T) {
	h := &UvozHandler{}
	h.SetOvlastiLetve(func(p *models.UserPermissions, letva string) bool { return letva == "bezdan" })
	podrucje := &models.UserPermissions{AllowedSections: map[string]bool{"x": true}}
	f := izvoriFormat(t, "godisnjak")
	if err := h.smijeSveLetve(podrucje, f, izvoriZadatak("42010=bezdan")); err != nil {
		t.Errorf("svoja letva odbijena: %v", err)
	}
	if err := h.smijeSveLetve(podrucje, f, izvoriZadatak("42010=bezdan,42015=apatin")); err == nil {
		t.Error("tuđa letva u popisu prošla je")
	}
}

// Stranica nudi sve posebne izvore i obrazac za HydroView; bez računa gumb
// za preuzimanje ne radi i piše gdje se račun upisuje.
func TestStranicaNudiPosebneIzvore(t *testing.T) {
	d := vrataZaTest()
	d.Formati = izvoriFormati()
	html := iscrtaj(t, "uvoz_niza.html", d)
	for _, want := range []string{"Posebni izvori", `value="his2000"`, `value="arso"`, `value="ehyd"`, `value="gkd"`,
		`value="pegelonline"`, `value="seba"`, `value="godisnjak"`, "/administracija/uvoz-izvora/pregled",
		"/administracija/uvoz-izvora/hidroview", "stranici Telemetrija"} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica nema %q", want)
		}
	}
}

func izvoriFormati() []izvori.Format { return izvori.Formati }

func izvoriFormat(t *testing.T, kod string) izvori.Format {
	t.Helper()
	f, ima := izvori.Nadji(kod)
	if !ima {
		t.Fatalf("nema formata %s", kod)
	}
	return f
}

func izvoriZadatak(postaje string) izvori.Zadatak { return izvori.Zadatak{Postaje: postaje} }
