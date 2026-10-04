package web

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Predlošci uvoza u docs/predlosci služe organizacijama koje registre pune
// same. Ovi testovi ih provode kroz iste rukovatelje kojima ih šalje stranica,
// pa predložak ne može tiho zastarjeti kad se uvoz promijeni.

const mapaPredlozaka = "../../docs/predlosci"

func predlozak(t *testing.T, put string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(mapaPredlozaka, put))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// zaglavljePredloska vraća prvi redak predloška kao stupce
func zaglavljePredloska(t *testing.T, sadrzaj []byte) []string {
	t.Helper()
	rows, err := readCSV(bytes.NewReader(sadrzaj))
	if err != nil || len(rows) == 0 {
		t.Fatalf("predložak se ne čita: %v", err)
	}
	return rows[0]
}

// zaglavljeIzvoza vraća prvi redak CSV-a koji je rukovatelj izvoza napisao
func zaglavljeIzvoza(t *testing.T, w *httptest.ResponseRecorder) []string {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("izvoz: %d %s", w.Code, w.Body.String())
	}
	cr := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(w.Body.Bytes(), []byte("\xEF\xBB\xBF"))))
	cr.Comma = ';'
	cr.FieldsPerRecord = -1
	red, err := cr.Read()
	if err != nil {
		t.Fatalf("izvoz se ne čita: %v", err)
	}
	return red
}

func prebroji(t *testing.T, baza *sql.DB, tablica string) int {
	t.Helper()
	var n int
	if err := baza.QueryRow("SELECT COUNT(*) FROM " + tablica).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPredlosciRegistaraProlazeUvoz(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "predlosci.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test")
	sse := service.NewSSEBroker()
	org := service.NewOrgService(repository.NewOrgRepository(baza, rec), sse)
	sections := service.NewSectionService(repository.NewSectionRepository(baza, rec), sse)
	ter := service.NewTerritoryService(repository.NewTerritoryRepository(baza, rec), sections)
	oh := NewOrgHandler(org, nil, nil, nil, nil, nil, nil, nil)
	th := NewTerritoriesHandler(ter, nil)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /organizacija/uvoz", oh.HandleImportCSV)
	mux.HandleFunc("POST /firme/uvoz", oh.HandleImportContractorsCSV)
	mux.HandleFunc("POST /territories/uvoz", th.HandleImportTerritoriesCSV)
	mux.HandleFunc("GET /organizacija/sektori.csv", oh.ExportSectorsCSV)
	mux.HandleFunc("GET /organizacija/podrucja.csv", oh.ExportAreasCSV)
	mux.HandleFunc("GET /firme.csv", oh.ExportContractorsCSV)
	mux.HandleFunc("GET /territories/zupanije.csv", th.ExportCountiesCSV)
	mux.HandleFunc("GET /territories/gradovi-i-opcine.csv", th.ExportMunicipalitiesCSV)
	mux.HandleFunc("GET /territories/naselja.csv", th.ExportSettlementsCSV)

	u := &models.User{ID: uuid.New(), FullName: "Pero Perić"}
	perms := &models.UserPermissions{IsGlobalAdmin: true, User: *u}
	zovi := func(r *http.Request) *httptest.ResponseRecorder {
		c := context.WithValue(r.Context(), contextKeyUser, u)
		c = context.WithValue(c, contextKeyPerms, perms)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(c))
		return w
	}
	posalji := func(putanja, vrsta string, sadrzaj []byte) {
		t.Helper()
		var tijelo bytes.Buffer
		mw := multipart.NewWriter(&tijelo)
		if vrsta != "" {
			_ = mw.WriteField("kind", vrsta)
		}
		fw, _ := mw.CreateFormFile("csv", "predlozak.csv")
		fw.Write(sadrzaj)
		mw.Close()
		r := httptest.NewRequest(http.MethodPost, putanja, &tijelo)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		w := zovi(r)
		loc, _ := url.QueryUnescape(w.Header().Get("Location"))
		if !strings.Contains(loc, "success") || strings.Contains(loc, "Preskočeno") {
			t.Fatalf("%s (%s): uvoz predloška nije prošao čist: %d %s", putanja, vrsta, w.Code, loc)
		}
	}

	// Redoslijed je onaj iz uputa: firma se veže na područje, naselje na općinu.
	koraci := []struct {
		datoteka, putanja, vrsta, izvoz string
	}{
		{"registri/sektori.csv", "/organizacija/uvoz", "sektori", "/organizacija/sektori.csv"},
		{"registri/branjena-podrucja.csv", "/organizacija/uvoz", "podrucja", "/organizacija/podrucja.csv"},
		{"registri/licencirane-firme.csv", "/firme/uvoz", "", "/firme.csv"},
		{"registri/zupanije.csv", "/territories/uvoz", "zupanije", "/territories/zupanije.csv"},
		{"registri/gradovi-i-opcine.csv", "/territories/uvoz", "opcine", "/territories/gradovi-i-opcine.csv"},
		{"registri/naselja.csv", "/territories/uvoz", "naselja", "/territories/naselja.csv"},
	}
	for _, k := range koraci {
		sadrzaj := predlozak(t, k.datoteka)
		if !bytes.HasPrefix(sadrzaj, []byte("\xEF\xBB\xBF")) {
			t.Errorf("%s: predložak treba oznaku BOM, da ga Excel otvori kao UTF-8", k.datoteka)
		}
		posalji(k.putanja, k.vrsta, sadrzaj)

		// Predložak ima iste stupce kao izvoz, pa se izvezena tablica može
		// urediti i vratiti. Predložak smije imati stupac više na kraju.
		izvoz := zaglavljeIzvoza(t, zovi(httptest.NewRequest(http.MethodGet, k.izvoz, nil)))
		zaglavlje := zaglavljePredloska(t, sadrzaj)
		if len(zaglavlje) < len(izvoz) {
			t.Errorf("%s: predložak ima %d stupaca, izvoz %d", k.datoteka, len(zaglavlje), len(izvoz))
			continue
		}
		for i := range izvoz {
			if zaglavlje[i] != izvoz[i] {
				t.Errorf("%s: stupac %d je %q, izvoz piše %q", k.datoteka, i+1, zaglavlje[i], izvoz[i])
			}
		}
	}

	for tablica, zeli := range map[string]int{
		"sectors": 2, "areas": 2, "contractors": 1, "counties": 1, "municipalities": 2, "settlements": 2,
	} {
		if n := prebroji(t, baza, tablica); n != zeli {
			t.Errorf("%s: %d zapisa nakon uvoza predloška, očekivano %d", tablica, n, zeli)
		}
	}
	var gdje int
	if err := baza.QueryRow(`SELECT COUNT(*) FROM contractor_assignments`).Scan(&gdje); err == nil && gdje != 2 {
		t.Errorf("firma iz predloška ima %d mjesta rada, očekivano 2 (BP 1 i Sektor P)", gdje)
	}
	var telefon string
	if err := baza.QueryRow(`SELECT vgi_phone FROM areas WHERE id = 1`).Scan(&telefon); err != nil || telefon != "000 000 004" {
		t.Errorf("telefon ispostave iz osmog stupca: %q (%v)", telefon, err)
	}
}

func TestPredlozakOcitanjaLetveSeCita(t *testing.T) {
	tekst := strings.TrimPrefix(string(predlozak(t, "ocitanja/ocitanja-letve.csv")), "\uFEFF")
	redci, satni, err := citajZalijepljeno(tekst)
	if err != nil {
		t.Fatal(err)
	}
	if !satni || len(redci) != 4 {
		t.Fatalf("pročitano %d redaka (satni %v), očekivano 4 satna", len(redci), satni)
	}
	for _, r := range redci {
		if r.Greska != "" {
			t.Errorf("redak %d: %s", r.Redak, r.Greska)
		}
	}
	// 07:00 po zagrebačkom vremenu 1. ožujka je 06:00 UTC
	if got := redci[0].Kad.Format("2006-01-02 15:04"); got != "2026-03-01 06:00" || redci[0].Vrijedi != 245 {
		t.Errorf("prvi redak: %s %v", got, redci[0].Vrijedi)
	}
	if redci[3].Vrijedi != -12 {
		t.Errorf("negativan vodostaj: %v", redci[3].Vrijedi)
	}
}

func TestPredlozakNizaZaArhivuSeCita(t *testing.T) {
	sadrzaj := predlozak(t, "ocitanja/niz-za-arhivu.csv")
	p, tijelo, err := pogodi("niz-za-arhivu.csv", sadrzaj)
	if err != nil {
		t.Fatal(err)
	}
	if p.StupacVrijeme != 0 || p.StupacVrijednost != 1 || p.Redaka != 5 {
		t.Fatalf("pogodak: vrijeme %d, vrijednost %d, redaka %d", p.StupacVrijeme, p.StupacVrijednost, p.Redaka)
	}
	if strings.Join(p.Zaglavlje, ";") != "vrijeme;vodostaj_cm" {
		t.Errorf("zaglavlje: %v", p.Zaglavlje)
	}
	redci, preskoceno, err := pretvori(tijelo, UvozNiza{Velicina: "vodostaj", Zona: "Europe/Zagreb",
		StupacVrijeme: p.StupacVrijeme, StupacVrijednost: p.StupacVrijednost})
	if err != nil {
		t.Fatal(err)
	}
	if len(redci) != 5 || preskoceno != 0 {
		t.Fatalf("pretvoreno %d, preskočeno %d", len(redci), preskoceno)
	}
	if redci[4].Vrijednost != 240.5 {
		t.Errorf("decimalni zarez: %v", redci[4].Vrijednost)
	}
}
