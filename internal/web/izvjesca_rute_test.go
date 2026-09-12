package web

import (
	"context"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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

// Dnevno izvješće rukovoditelja dionice kroz rute: popis, prazan obrazac s
// vodotokom iz dionice, spremanje nacrta, dokument, predaja, pa isti dan
// vodi na uređivanje umjesto na novi obrazac.
func TestDnevnaIzvjescaKrozRute(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "izv.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (16, 'B', 'Baranja', 'VGI Baranja', 'Osijek')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('B.16.3', 16, 'B', 'Dunav, lijeva obala', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	svc := service.NewIzvjescaService(repository.NewIzvjescaRepository(baza, rec), repository.NewSectionRepository(baza, rec),
		repository.NewStationRepository(baza, rec), repository.NewReadingRepository(baza, rec), repository.NewEpisodeRepository(baza, rec),
		repository.NewJournalRepository(baza, rec))

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tmpl := func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	h := NewIzvjescaHandler(func() *service.IzvjescaService { return svc }, tmpl("izvjesca.html"), tmpl("izvjesce_form.html"), tmpl("izvjesce.html"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /izvjesca", h.ShowPopis)
	mux.HandleFunc("GET /izvjesca/novo", h.ShowNovo)
	mux.HandleFunc("POST /izvjesca", h.HandleSpremi)
	mux.HandleFunc("GET /izvjesca/{id}", h.ShowIzvjesce)
	mux.HandleFunc("GET /izvjesca/{id}/uredi", h.ShowUredi)
	mux.HandleFunc("POST /izvjesca/{id}", h.HandleSpremi)
	mux.HandleFunc("POST /izvjesca/{id}/predaj", h.HandlePredaj)
	mux.HandleFunc("POST /izvjesca/{id}/obrisi", h.HandleObrisi)
	mux.HandleFunc("GET /izvjesca/{id}/izvjesce.xlsx", h.IzvoziIzvjesce)
	h.SetZaglavlje(func(sektor string) ZaglavljeIzvoza {
		return ZaglavljeIzvoza{Organizacija: "Hrvatske vode", Odjel: "VGO Osijek", Centar: "COP Osijek", Sektor: sektor, Mjesto: "Osijek", Datum: time.Now()}
	})

	rukovoditelj := &models.User{ID: uuid.New(), FullName: "Rukovoditelj Dionice"}
	prava := &models.UserPermissions{AllowedSections: map[string]bool{"B.16.3": true}}
	zovi := func(metoda, putanja string, obrazac url.Values) *httptest.ResponseRecorder {
		t.Helper()
		var r *http.Request
		if obrazac != nil {
			r = httptest.NewRequest(metoda, putanja, strings.NewReader(obrazac.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			r = httptest.NewRequest(metoda, putanja, nil)
		}
		ctx := context.WithValue(r.Context(), contextKeyUser, rukovoditelj)
		ctx = context.WithValue(ctx, contextKeyPerms, prava)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(ctx))
		return w
	}
	mora := func(w *httptest.ResponseRecorder, kod int, sto string, dijelovi ...string) {
		t.Helper()
		if w.Code != kod {
			t.Fatalf("%s: %d, a mora biti %d\n%s", sto, w.Code, kod, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "template:") {
			t.Errorf("%s: predložak puknuo: %s", sto, w.Body.String()[strings.Index(w.Body.String(), "template:"):])
		}
		for _, d := range dijelovi {
			if !strings.Contains(w.Body.String(), d) {
				t.Errorf("%s: nema %q", sto, d)
			}
		}
	}

	mora(zovi(http.MethodGet, "/izvjesca", nil), http.StatusOK, "popis", "Nema izvješća", `href="/izvjesca/novo"`)
	mora(zovi(http.MethodGet, "/izvjesca/novo", nil), http.StatusOK, "obrazac",
		`name="dan"`, `<option value="B.16.3" selected>`, "Stadij obrane", "nagli porast", "Spremi i predaj")

	danas := time.Now().In(models.Zagreb).Format("2006-01-02")
	obrazac := url.Values{
		"dan": {danas}, "dionica": {"B.16.3"}, "stadij": {"REDOVNA"}, "vodotok": {"Dunav"}, "tendencija": {"PORAST"},
		"vodostaj_station": {"", ""}, "vodostaj_postaja": {"Dunav – Batina", ""}, "vodostaj_sat": {"07:00", ""},
		"vodostaj_vrijednost": {"551", ""}, "vodostaj_jedinica": {"cm", "cm"},
		"pregled": {"Nasip pregledan, bez oštećenja."}, "radnje": {"Nadvišenje nasipa 120 m."}, "vrece": {"1500"},
		"pravne_ljudi": {"12"}, "pravne_kamioni": {"2"}, "ostali_vatrogasci": {"8"},
		"popl_naselja": {"Batina"}, "popl_ljudi": {"0"}, "popl_sumske": {"12,5"},
	}
	w := zovi(http.MethodPost, "/izvjesca", obrazac)
	mora(w, http.StatusSeeOther, "spremanje")
	kamo := w.Header().Get("Location")
	if !strings.HasPrefix(kamo, "/izvjesca/") || strings.Contains(kamo, "error=") {
		t.Fatalf("spremanje vodi na %q", kamo)
	}
	id := strings.SplitN(strings.TrimPrefix(kamo, "/izvjesca/"), "?", 2)[0]
	mora(zovi(http.MethodGet, "/izvjesca/"+id, nil), http.StatusOK, "dokument",
		"Dunav – Batina", "551", "porast", "Nasip pregledan", "Batina", "12,5", "Rukovoditelj Dionice", "nacrt", `action="/izvjesca/`+id+`/predaj"`)

	// Isti dan i dionica: novi obrazac vodi na uređivanje postojećeg.
	w = zovi(http.MethodGet, "/izvjesca/novo?dionica=B.16.3&dan="+danas, nil)
	mora(w, http.StatusSeeOther, "ponovno novo")
	if w.Header().Get("Location") != "/izvjesca/"+id+"/uredi" {
		t.Errorf("ponovno novo vodi na %q", w.Header().Get("Location"))
	}
	if p := os.Getenv("GOCOP_DUMP"); p != "" {
		_ = os.WriteFile(p, zovi(http.MethodGet, "/izvjesca/"+id+"/uredi", nil).Body.Bytes(), 0o644)
	}
	mora(zovi(http.MethodGet, "/izvjesca/"+id+"/uredi", nil), http.StatusOK, "uređivanje", `value="Dunav"`, `value="1500"`, `value="12"`, `checked> porast`)

	mora(zovi(http.MethodPost, "/izvjesca/"+id+"/predaj", url.Values{}), http.StatusSeeOther, "predaja")
	mora(zovi(http.MethodGet, "/izvjesca/"+id, nil), http.StatusOK, "predano", "predano u podcentar")
	mora(zovi(http.MethodGet, "/izvjesca?dionica=B.16.3", nil), http.StatusOK, "popis poslije", "B.16.3", "Dunav", ">R<", "predano")

	// Izvoz je valjana .xlsx datoteka s obrascem.
	w = zovi(http.MethodGet, "/izvjesca/"+id+"/izvjesce.xlsx", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Disposition"), "dnevno-izvjesce-B-16-3-"+danas) {
		t.Fatalf("izvoz: %d %s", w.Code, w.Header().Get("Content-Disposition"))
	}
	if p := os.Getenv("GOCOP_IZVOZ_IZVJESCE"); p != "" {
		_ = os.WriteFile(p, w.Body.Bytes(), 0o644)
	}
	redci, err := procitajXLSX(w.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	var sve []string
	for _, r := range redci {
		sve = append(sve, strings.Join(r, "|"))
	}
	list := strings.Join(sve, "\n")
	for _, want := range []string{"DNEVNO IZVJEŠĆE RUKOVODITELJA DIONICE", "B.16.3", "Dunav – Batina", "07:00", "☒ porast", "☒ R", "Nasip pregledan", "Nadvišenje nasipa 120 m.", "Batina", "Rukovoditelj Dionice"} {
		if !strings.Contains(list, want) {
			t.Errorf("izvoz nema %q", want)
		}
	}

	// Predano rukovoditelj ne briše.
	w = zovi(http.MethodPost, "/izvjesca/"+id+"/obrisi", url.Values{})
	if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "error=") {
		t.Errorf("brisanje predanog: %d %s", w.Code, w.Header().Get("Location"))
	}
}

// Sektorsko izvješće kroz rute: s popisa se sastavi za dan, obrazac nudi
// izvješća dionica i zapise dnevnika, spremi se sa snimkom, dokument i
// izvoz nose zbrojeve; isti dan vodi na uređivanje.
func TestSektorskoIzvjesceKrozRute(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "sekt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (16, 'B', 'Baranja', 'VGI Baranja', 'Darda')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('B.16.1', 16, 'B', 'Dunav l.o.', '2026-01-01', '2026-01-01')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('B.16.2', 16, 'B', 'Drava l.o.', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	journals := repository.NewJournalRepository(baza, rec)
	sections := repository.NewSectionRepository(baza, rec)
	svc := service.NewIzvjescaService(repository.NewIzvjescaRepository(baza, rec), sections, nil, nil, nil, journals)
	svc.SetSektorska(repository.NewSektorskaIzvjescaRepository(baza, rec))

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tmpl := func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	h := NewIzvjescaHandler(func() *service.IzvjescaService { return svc }, tmpl("izvjesca.html"), tmpl("izvjesce_form.html"), tmpl("izvjesce.html"))
	h.SetSektorsko(tmpl("sektorsko_form.html"), tmpl("sektorsko.html"))
	h.SetZaglavlje(func(sektor string) ZaglavljeIzvoza {
		return ZaglavljeIzvoza{Organizacija: "Hrvatske vode", Odjel: "VGO Osijek", Centar: "COP Osijek", Sektor: sektor, Mjesto: "Osijek", Datum: time.Now(),
			Potpisnici: []PotpisnikIzvoza{{Funkcija: "voditelj Centra", Ime: "V"}, {Funkcija: "rukovoditelj obrane od poplava sektora B", Ime: "Rukovoditelj Sektora"}}}
	})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /izvjesca", h.ShowPopis)
	mux.HandleFunc("GET /sektorsko-izvjesce/novo", h.ShowSektorskoNovo)
	mux.HandleFunc("POST /sektorsko-izvjesce", h.HandleSektorskoSpremi)
	mux.HandleFunc("GET /sektorsko-izvjesce/{id}", h.ShowSektorsko)
	mux.HandleFunc("GET /sektorsko-izvjesce/{id}/uredi", h.ShowSektorskoUredi)
	mux.HandleFunc("POST /sektorsko-izvjesce/{id}", h.HandleSektorskoSpremi)
	mux.HandleFunc("POST /sektorsko-izvjesce/{id}/predaj", h.HandleSektorskoPredaj)
	mux.HandleFunc("POST /sektorsko-izvjesce/{id}/obrisi", h.HandleSektorskoObrisi)
	mux.HandleFunc("GET /sektorsko-izvjesce/{id}/izvjesce.xlsx", h.IzvoziSektorsko)

	voditelj := &models.User{ID: uuid.New(), FullName: "Voditelj Centra"}
	uprava := &models.UserPermissions{AdminSectors: map[string]bool{"B": true}, AllowedSectors: map[string]bool{"B": true}}
	zovi := func(metoda, putanja string, obrazac url.Values) *httptest.ResponseRecorder {
		t.Helper()
		var r *http.Request
		if obrazac != nil {
			r = httptest.NewRequest(metoda, putanja, strings.NewReader(obrazac.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			r = httptest.NewRequest(metoda, putanja, nil)
		}
		ctx := context.WithValue(r.Context(), contextKeyUser, voditelj)
		ctx = context.WithValue(ctx, contextKeyPerms, uprava)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(ctx))
		return w
	}
	mora := func(w *httptest.ResponseRecorder, kod int, sto string, dijelovi ...string) {
		t.Helper()
		if w.Code != kod {
			t.Fatalf("%s: %d, a mora biti %d\n%s", sto, w.Code, kod, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "template:") {
			t.Errorf("%s: predložak puknuo: %s", sto, w.Body.String()[strings.Index(w.Body.String(), "template:"):])
		}
		for _, d := range dijelovi {
			if !strings.Contains(w.Body.String(), d) {
				t.Errorf("%s: nema %q", sto, d)
			}
		}
	}

	// dnevnik s jednim zapisom i dva izvješća dionica (jedno nacrt)
	ctx := context.Background()
	dan := time.Now().In(models.Zagreb)
	dan = time.Date(dan.Year(), dan.Month(), dan.Day(), 0, 0, 0, 0, models.Zagreb)
	danas := dan.Format("2006-01-02")
	pocetak := dan.AddDate(0, 0, -1)
	obrana := &models.Journal{Kind: models.JournalKindDefense, CentarSektor: "B", Title: "Dnevnik COP-a", Year: dan.Year(), StartedAt: &pocetak}
	if err := journals.SaveJournal(ctx, obrana); err != nil {
		t.Fatal(err)
	}
	js := service.NewJournalService(journals, nil, nil)
	pod := 16
	h1 := dan.Add(6*time.Hour + 40*time.Minute)
	if err := js.DodajZapisCOP(ctx, voditelj, uprava, models.Opseg{Sektor: "B", Podrucja: []int{16}}, obrana,
		&models.JournalEntry{JournalID: obrana.ID, Date: dan.Add(7 * time.Hour), HappenedAt: &h1, Kind: models.EntryKindReport, Text: "Procjeđivanje kod Zmajevca.", ReportedBy: "vodočuvar", Podrucje: &pod}); err != nil {
		t.Fatal(err)
	}
	sec1, _ := sections.GetSectionByCode("B.16.1")
	sec2, _ := sections.GetSectionByCode("B.16.2")
	iz1 := &models.DnevnoIzvjesce{SectionCode: "B.16.1", Dan: dan, Stadij: models.PhaseEmergency, Sadrzaj: models.IzvjesceSadrzaj{Vodotok: "Dunav", Tendencija: models.TendencijaPorast,
		Pregled: "Procjeđivanje kod Zmajevca.", Radnje: "Kontranasip 45 m.", Vrece: "1 800", Pravne: models.SudioniciPravne{Ljudi: 14, Kamioni: 3}, Ostali: models.SudioniciOstali{Vatrogasci: 12}}}
	if err := svc.Spremi(ctx, voditelj, uprava, sec1, iz1); err != nil {
		t.Fatal(err)
	}
	if err := svc.Predaj(ctx, voditelj, uprava, sec1, iz1.ID); err != nil {
		t.Fatal(err)
	}
	iz2 := &models.DnevnoIzvjesce{SectionCode: "B.16.2", Dan: dan, Stadij: models.PhaseRegular, Sadrzaj: models.IzvjesceSadrzaj{Vodotok: "Drava", Pregled: "Bez oštećenja.", Pravne: models.SudioniciPravne{Ljudi: 6}}}
	if err := svc.Spremi(ctx, voditelj, uprava, sec2, iz2); err != nil {
		t.Fatal(err)
	}

	// popis nudi sastavljanje; obrazac nudi oba izvješća (nacrt neoznačen) i zapis
	mora(zovi(http.MethodGet, "/izvjesca", nil), http.StatusOK, "popis", "Izvješće sektora", `action="/sektorsko-izvjesce/novo"`, "Sastavi za dan", "Još nema izvješća sektora")
	w := zovi(http.MethodGet, "/sektorsko-izvjesce/novo?sektor=B&dan="+danas, nil)
	mora(w, http.StatusOK, "obrazac", `value="`+iz1.ID+`" checked`, `value="`+iz2.ID+`">`, "Procjeđivanje kod Zmajevca.", "vodočuvar", "Baranja — B.16.1 (Dunav): Procjeđivanje", "[vreće 1 800]")
	zapisID := ""
	if i := strings.Index(w.Body.String(), `name="zapis" value="`); i >= 0 {
		rest := w.Body.String()[i+len(`name="zapis" value="`):]
		zapisID = rest[:strings.Index(rest, `"`)]
	}
	if zapisID == "" {
		t.Fatal("obrazac ne nudi zapis dnevnika")
	}

	w = zovi(http.MethodPost, "/sektorsko-izvjesce", url.Values{"sektor": {"B"}, "dan": {danas}, "hidrometeo": {"Kiša na cijelom sektoru."},
		"ostecenja": {"Baranja — B.16.1: procjeđivanje."}, "mjere": {"Kontranasip 45 m."}, "izvjesce": {iz1.ID, iz2.ID}, "zapis": {zapisID}, "napomena": {"Vodostaj raste, sutra izvanredna."}})
	mora(w, http.StatusSeeOther, "spremanje")
	kamo := w.Header().Get("Location")
	if !strings.HasPrefix(kamo, "/sektorsko-izvjesce/") || strings.Contains(kamo, "error=") {
		t.Fatalf("spremanje vodi na %q", kamo)
	}
	id := strings.SplitN(strings.TrimPrefix(kamo, "/sektorsko-izvjesce/"), "?", 2)[0]
	mora(zovi(http.MethodGet, "/sektorsko-izvjesce/"+id, nil), http.StatusOK, "dokument",
		"Kiša na cijelom sektoru.", "Baranja", "B.16.1", "B.16.2", ">20<", ">12<", "Procjeđivanje kod Zmajevca.", "Izvanredna obrana", "Vodostaj raste", "nacrt", "Predaj GCOP-u")
	w = zovi(http.MethodGet, "/sektorsko-izvjesce/novo?sektor=B&dan="+danas, nil)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/sektorsko-izvjesce/"+id+"/uredi" {
		t.Errorf("ponovno novo: %d %s", w.Code, w.Header().Get("Location"))
	}
	mora(zovi(http.MethodGet, "/sektorsko-izvjesce/"+id+"/uredi", nil), http.StatusOK, "uređivanje", "Kiša na cijelom sektoru.", `value="`+iz2.ID+`" checked`, `value="`+zapisID+`" checked`)
	mora(zovi(http.MethodPost, "/sektorsko-izvjesce/"+id+"/predaj", url.Values{}), http.StatusSeeOther, "predaja")
	mora(zovi(http.MethodGet, "/izvjesca", nil), http.StatusOK, "popis poslije", `href="/sektorsko-izvjesce/`+id+`"`, "predano")

	w = zovi(http.MethodGet, "/sektorsko-izvjesce/"+id+"/izvjesce.xlsx", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Disposition"), "dnevno-izvjesce-sektor-B-"+danas) {
		t.Fatalf("izvoz: %d %s", w.Code, w.Header().Get("Content-Disposition"))
	}
	if p := os.Getenv("GOCOP_IZVOZ_SEKTORSKO"); p != "" {
		_ = os.WriteFile(p, w.Body.Bytes(), 0o644)
	}
	redci, err := procitajXLSX(w.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	var sve []string
	for _, r := range redci {
		sve = append(sve, strings.Join(r, "|"))
	}
	list := strings.Join(sve, "\n")
	for _, want := range []string{"DNEVNO IZVJEŠĆE RUKOVODITELJA SEKTORA", "Kiša na cijelom sektoru.", "Dunav|I — Izvanredna obrana", "UKUPNO SEKTOR|20|3", "Procjeđivanje kod Zmajevca.", "Rukovoditelj Sektora", "Voditelj Centra"} {
		if !strings.Contains(list, want) {
			t.Errorf("izvoz nema %q", want)
		}
	}
}
