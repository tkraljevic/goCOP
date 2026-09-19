package web

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"html/template"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"
)

func probnaSlika(w, h int, boja color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, boja)
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// Prijava s terena kroz rute: vodočuvar sastavi nacrt s fotografijama i
// mjestom, objavi ga i potpiše svojim ključem; prijava dobije broj, potpisani
// PDF s ugrađenim slikama postaje izvornik, upis ide na njegov dnevni list;
// rukovoditelj je vidi tek objavljenu i arhivira; izvorne slike se brišu
// nakon roka, a PDF ih zadrži; strojar u prijave ne ulazi.
func TestPrijaveSTerenaKrozRute(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "prijave.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop, address, phone, email) VALUES ('B', 'Sektor B', 'VGO za Dunav i donju Dravu, Osijek', 'COP Osijek', 'Splavarska 2a, 31000 Osijek', '031/252-802', 'copos@voda.hr')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter, latitude, longitude) VALUES (34, 'B', 'međudržavne rijeke Drava i Dunav', 'VGI Baranja', 'Osijek', 45.7, 18.8)`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('B.34.1', 34, 'B', 'd.o. r. Dunav', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "cop-osijek")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	ctx := context.Background()
	b, bp := "B", 34
	kunac := &models.User{ID: uuid.New(), Username: "mkunac", FullName: "Mile Kunac", IsActive: true}
	if err := userRepo.CreateUser(kunac, &models.Duty{Title: "Rukovoditelj BP 34", Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, SectorID: &b, AreaID: &bp, IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	seit := &models.User{ID: uuid.New(), Username: "seit", FullName: "Seit Vodočuvar", IsActive: true}
	if err := userRepo.CreateUser(seit, &models.Duty{Title: "Vodočuvar Batina", Role: models.RoleWaterGuard, ScopeType: models.ScopeSection, SectorID: &b, AreaID: &bp, SectionCodes: "B.34.1", IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	strojar := &models.User{ID: uuid.New(), Username: "strojar", FullName: "Stipe Strojar", IsActive: true}
	if err := userRepo.CreateUser(strojar, &models.Duty{Title: "Strojar", Role: models.RoleMachinist, ScopeType: models.ScopeSection, SectorID: &b, AreaID: &bp, SectionCodes: "B.34.1", IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	orgRepo := repository.NewOrgRepository(baza, rec)
	vod := service.NewVodocuvarService(repository.NewVodocuvarRepository(baza, rec), users, "cop-osijek")
	vod.SetOrg(orgRepo)
	prijaveRepo := repository.NewPrijavaRepository(baza, rec)
	prijave := service.NewPrijavaService(prijaveRepo, users, vod, "cop-osijek")
	_, kljucCvora, _ := ed25519.GenerateKey(nil)
	potpisi := service.NewPotpisService(repository.NewPotpisRepository(baza, rec), users, "cop-osijek", kljucCvora, nil)
	if err := potpisi.Pokreni(ctx); err != nil {
		t.Fatal(err)
	}

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tmpl := func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	vodH := NewVodocuvarHandler(func() *service.VodocuvarService { return vod }, users, func() *repository.OrgRepository { return orgRepo }, tmpl("vodocuvar.html"), tmpl("vodocuvar_list.html"))
	vodH.SetPotpis(func() *service.PotpisService { return potpisi })
	vodH.SetOpcije(func(context.Context) models.Opcije { return models.Opcije{} })
	h := NewPrijaveHandler(func() *service.PrijavaService { return prijave }, users, vodH, tmpl("prijave.html"), tmpl("prijava_form.html"), tmpl("prijava.html"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /prijave", h.ShowPopis)
	mux.HandleFunc("GET /prijave/nova", h.ShowForm)
	mux.HandleFunc("POST /prijave", h.HandleSpremi)
	mux.HandleFunc("GET /prijave/{id}", h.ShowPrijava)
	mux.HandleFunc("GET /prijave/{id}/uredi", h.ShowForm)
	mux.HandleFunc("POST /prijave/{id}/radnja", h.HandleRadnja)
	mux.HandleFunc("GET /prijave/{id}/slika/{sid}", h.Slika)
	mux.HandleFunc("GET /prijave/{id}/prijava.pdf", h.IzvoziPDF)
	mux.HandleFunc("GET /vodocuvar/{id}", vodH.ShowList)
	kao := func(u *models.User) *models.UserPermissions {
		cijeli, _ := users.GetUserByID(u.ID)
		return models.NewUserPermissions(*cijeli)
	}
	zovi := func(u *models.User, r *http.Request) *httptest.ResponseRecorder {
		cijeli, _ := users.GetUserByID(u.ID)
		c := context.WithValue(context.WithValue(r.Context(), contextKeyUser, cijeli), contextKeyPerms, kao(u))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(c))
		return w
	}
	forma := func(u *models.User, metoda, putanja string, v url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest(metoda, putanja, strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return zovi(u, r)
	}
	get := func(u *models.User, putanja string) *httptest.ResponseRecorder {
		return zovi(u, httptest.NewRequest(http.MethodGet, putanja, nil))
	}
	loc := func(w *httptest.ResponseRecorder) string {
		l, _ := url.QueryUnescape(w.Header().Get("Location"))
		return l
	}

	// strojar u prijave ne ulazi; vodočuvar otvara obrazac
	if w := get(strojar, "/prijave"); w.Code != http.StatusForbidden {
		t.Errorf("strojar na prijavama: %d", w.Code)
	}
	if s := get(seit, "/prijave/nova").Body.String(); !strings.Contains(s, "Nova prijava s terena") || !strings.Contains(s, "karta-izbor") {
		t.Fatal("obrazac nove prijave")
	}

	// nacrt s dvije fotografije i mjestom
	var tijelo bytes.Buffer
	mw := multipart.NewWriter(&tijelo)
	danas := time.Now().In(models.Zagreb).Format("2006-01-02")
	for k, v := range map[string]string{"vrsta": "PRIJAVA", "naslov": "Oštećena rampa na dravskom nasipu", "opis": "Rampa na st. 25+500 je odrezana brava; prolaz vozila slobodan.", "datum": danas,
		"vodotok": "Drava", "dionica": "B.34.1", "stacionaza": "25+500", "lat": "45.6512", "lon": "18.7734"} {
		_ = mw.WriteField(k, v)
	}
	for i, boja := range []color.RGBA{{200, 30, 30, 255}, {30, 30, 200, 255}} {
		fw, _ := mw.CreateFormFile("slike", []string{"rampa.png", "brava.png"}[i])
		_, _ = fw.Write(probnaSlika(2400, 1800, boja))
	}
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/prijave", &tijelo)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	l := loc(zovi(seit, r))
	if !strings.Contains(l, "success") || !strings.Contains(l, "2 fotografija") {
		t.Fatalf("nacrt: %s", l)
	}
	id := strings.TrimPrefix(strings.SplitN(l, "?", 2)[0], "/prijave/")
	p, _ := prijave.Get(ctx, kao(seit), id)
	if p == nil || p.Status != models.PrijavaNacrt || len(p.Slike) != 2 || p.Slike[0].Sirina != 1600 || p.Slike[0].Visina != 1200 || p.Slike[0].Bajtova > 300<<10 || !p.ImaKoordinate() || p.AreaID != 34 {
		t.Fatalf("nacrt: %+v", p)
	}
	if s := get(seit, "/prijave/"+id).Body.String(); !strings.Contains(s, "Oštećena rampa") || !strings.Contains(s, "Objavi i potpiši") || strings.Count(s, "/slika/") < 2 || !strings.Contains(s, "karta-letve") {
		t.Error("stranica nacrta")
	}
	if w := get(seit, "/prijave/"+id+"/slika/"+p.Slike[0].ID); w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("slika: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	// nacrt vidi samo vodočuvar
	if w := get(kunac, "/prijave/"+id); w.Code != http.StatusNotFound {
		t.Errorf("rukovoditelj vidi tuđi nacrt: %d", w.Code)
	}
	if w := forma(kunac, http.MethodPost, "/prijave/"+id+"/radnja", url.Values{"radnja": {"objavi"}}); !strings.Contains(loc(w), "error") {
		t.Error("rukovoditelj objavio tuđi nacrt")
	}

	// objava: s ključem traži lozinku; potpisani PDF je izvornik, upis na listu
	if _, err := potpisi.Novi(ctx, seit, "tajnaseit"); err != nil {
		t.Fatal(err)
	}
	if l := loc(forma(seit, http.MethodPost, "/prijave/"+id+"/radnja", url.Values{"radnja": {"objavi"}})); !strings.Contains(l, "lozinku") {
		t.Fatalf("objava bez lozinke: %s", l)
	}
	if l := loc(forma(seit, http.MethodPost, "/prijave/"+id+"/radnja", url.Values{"radnja": {"objavi"}, "lozinka": {"tajnaseit"}})); !strings.Contains(l, "potpisana") {
		t.Fatalf("objava: %s", l)
	}
	p, _ = prijave.Get(ctx, kao(seit), id)
	if p == nil || !p.Objavljena() || p.Broj != 1 || p.Oznaka() != "B-T-1/"+danas[:4] || p.ListID == "" || p.ListBroj != 1 {
		t.Fatalf("objavljena prijava: %+v", p)
	}
	iz, _ := prijaveRepo.Izvornik(ctx, id)
	if iz == nil || len(iz.PDF) < 20<<10 {
		t.Fatalf("izvornik: %v", iz != nil)
	}
	if ps := potpisi.Provjeri(ctx, iz.PDF); len(ps) != 1 || !ps[0].Valjan || !ps[0].Cijeli || ps[0].Ime != "Seit Vodočuvar" || !strings.Contains(ps[0].Razlog, "B-T-1") {
		t.Fatalf("potpis izvornika: %+v", ps)
	}
	if tekst := pdfTekst(t, iz.PDF); strings.Contains(tekst, "DNEVNI LIST") == false && (!strings.Contains(tekst, "Oštećena rampa") || !strings.Contains(tekst, "Fotografije (2)") || !strings.Contains(tekst, "25+500") || !strings.Contains(tekst, "dnevni list 001")) {
		t.Errorf("tekst PDF-a: %.400s", tekst)
	}
	listovi, _ := vod.Moji(ctx, seit, time.Now().In(models.Zagreb).Year())
	if len(listovi) != 1 || len(listovi[0].Prijave) != 1 || listovi[0].Prijave[0].Oznaka != p.Oznaka() || listovi[0].Broj != 1 {
		t.Fatalf("dnevni list nakon objave: %+v", listovi)
	}
	if s := get(seit, "/vodocuvar/"+listovi[0].ID).Body.String(); !strings.Contains(s, "Prijava s terena B-T-1") {
		t.Error("list ne pokazuje prijavu")
	}
	// objavljena se ne mijenja; PDF s rute je izvornik
	if l := loc(get(seit, "/prijave/"+id+"/uredi")); !strings.Contains(l, "error") {
		t.Error("objavljena prijava se uređuje")
	}
	if w := get(kunac, "/prijave/"+id+"/prijava.pdf"); !bytes.Equal(w.Body.Bytes(), iz.PDF) {
		t.Error("PDF s rute nije izvornik")
	}

	// rukovoditelj sad vidi prijavu, na popisu i pojedinačno, i arhivira je
	if s := get(kunac, "/prijave").Body.String(); !strings.Contains(s, "B-T-1") || !strings.Contains(s, "Oštećena rampa") {
		t.Error("popis rukovoditelja")
	}
	if s := get(kunac, "/prijave/"+id).Body.String(); !strings.Contains(s, "Pregledano, arhiviraj") || !strings.Contains(s, "elektronički potpisao") {
		t.Error("stranica objavljene prijave za rukovoditelja")
	}
	if l := loc(forma(seit, http.MethodPost, "/prijave/"+id+"/radnja", url.Values{"radnja": {"arhiviraj"}})); !strings.Contains(l, "error") {
		t.Error("vodočuvar arhivirao")
	}
	if l := loc(forma(kunac, http.MethodPost, "/prijave/"+id+"/radnja", url.Values{"radnja": {"arhiviraj"}})); !strings.Contains(l, "arhivirana") {
		t.Fatalf("arhiviranje: %s", l)
	}

	// izvorne slike se brišu nakon roka; PDF ih zadrži, stranica to kaže
	if n, _ := prijave.ObrisiStareSlike(ctx, 0); n != 0 {
		t.Errorf("rok 180 dana obrisao svježe slike: %d", n)
	}
	if n, _ := prijaveRepo.ObrisiStareSlike(ctx, time.Now().Add(time.Hour)); n != 2 {
		t.Errorf("brisanje nakon roka: %d", n)
	}
	if w := get(kunac, "/prijave/"+id+"/slika/"+p.Slike[0].ID); w.Code != http.StatusNotFound {
		t.Errorf("obrisana slika još služi: %d", w.Code)
	}
	if iz2, _ := prijaveRepo.Izvornik(ctx, id); iz2 == nil || !bytes.Equal(iz2.PDF, iz.PDF) {
		t.Error("PDF promijenjen brisanjem slika")
	}
	if s := get(kunac, "/prijave/"+id).Body.String(); !strings.Contains(s, "nema-slike") {
		t.Error("stranica bez napomene o slikama u PDF-u")
	}
}
