package web

import (
	"context"
	"encoding/json"
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
)

// Registar kišomjera: upis obrascem, premještanje s karte, ovlasti i JSON popis.
func TestSlivoviAPI(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "web_kisomjeri.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test-node")
	svc := service.NewKisomjerService(repository.NewKisomjerRepository(baza, rec))
	h := NewSlivoviHandler(func() *service.KisomjerService { return svc }, nil, nil, nil, nil)
	ctx := context.Background()
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	obican := &models.UserPermissions{}

	posalji := func(perms *models.UserPermissions, fn http.HandlerFunc, put, vrsta, tijelo string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", put, strings.NewReader(tijelo))
		req.Header.Set("Content-Type", vrsta)
		req = req.WithContext(context.WithValue(ctx, contextKeyPerms, perms))
		w := httptest.NewRecorder()
		fn(w, req)
		return w
	}
	const obrazac = "application/x-www-form-urlencoded"

	// sliv pa točka obrascem; obrazac dobiva preusmjeravanje
	w := posalji(admin, h.HandleSliv, "/api/slivovi/sliv", obrazac, "oznaka=A&naziv=Drava+iznad+Borla&km2=14649")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("sliv: status %d: %s", w.Code, w.Body.String())
	}
	w = posalji(admin, h.HandleCreate, "/api/slivovi/kisomjer/create", obrazac,
		"naziv=A1+pobrđe&sliv=A&pojas=pobrđe+(300–1000+m)&lat=46,7&lon=14,0&visina=689&km2=2993&tezina=0,204&aktivan=1")
	if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "success=") {
		t.Fatalf("create: status %d, Location %q: %s", w.Code, w.Header().Get("Location"), w.Body.String())
	}
	tocke, err := svc.ListKisomjeri(ctx)
	if err != nil || len(tocke) != 1 {
		t.Fatalf("nakon upisa: %d točaka, err %v", len(tocke), err)
	}
	k := tocke[0]
	if k.Code != "a-a1-pobrde" || k.Latitude != 46.7 || k.Longitude != 14 || k.Tezina == nil || *k.Tezina != 0.204 || !k.Aktivan {
		t.Errorf("upisana točka: %+v", k)
	}

	// običan korisnik ne smije ni upisati ni premjestiti
	w = posalji(obican, h.HandleCreate, "/api/slivovi/kisomjer/create", "application/json", `{"naziv":"x","latitude":46,"longitude":16}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("običan korisnik, create: status %d", w.Code)
	}
	w = posalji(obican, h.HandlePolozaji, "/api/slivovi/kisomjer/polozaji", "application/json", `{"tocke":[{"code":"a-a1-pobrde","lat":46.71,"lon":14.01}]}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("običan korisnik, položaji: status %d", w.Code)
	}

	// premještanje s karte mijenja samo koordinate; nepomaknuta točka se ne broji
	w = posalji(admin, h.HandlePolozaji, "/api/slivovi/kisomjer/polozaji", "application/json",
		`{"tocke":[{"code":"a-a1-pobrde","lat":46.71,"lon":14.01}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("položaji: status %d: %s", w.Code, w.Body.String())
	}
	var odg struct {
		Premjesteno int `json:"premjesteno"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &odg)
	if odg.Premjesteno != 1 {
		t.Errorf("premješteno = %d, želim 1", odg.Premjesteno)
	}
	k2, _ := svc.GetKisomjer(ctx, "a-a1-pobrde")
	if k2 == nil || k2.Latitude != 46.71 || k2.Longitude != 14.01 || k2.Tezina == nil || *k2.Tezina != 0.204 || k2.Pojas != k.Pojas {
		t.Errorf("nakon premještanja: %+v", k2)
	}
	w = posalji(admin, h.HandlePolozaji, "/api/slivovi/kisomjer/polozaji", "application/json",
		`{"tocke":[{"code":"a-a1-pobrde","lat":46.71,"lon":14.01}]}`)
	_ = json.Unmarshal(w.Body.Bytes(), &odg)
	if w.Code != http.StatusOK || odg.Premjesteno != 0 {
		t.Errorf("nepomaknuta točka: status %d, premješteno %d", w.Code, odg.Premjesteno)
	}
	// koordinate izvan Europe su zamjena širine i dužine
	w = posalji(admin, h.HandlePolozaji, "/api/slivovi/kisomjer/polozaji", "application/json",
		`{"tocke":[{"code":"a-a1-pobrde","lat":14.01,"lon":46.71}]}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("zamijenjene koordinate: status %d", w.Code)
	}

	// JSON popis bez geometrije sliva, s točkama
	req := httptest.NewRequest("GET", "/api/slivovi", nil)
	req = req.WithContext(context.WithValue(ctx, contextKeyPerms, obican))
	rw := httptest.NewRecorder()
	h.HandleListAPI(rw, req)
	var popis struct {
		Kisomjeri []models.Kisomjer `json:"kisomjeri"`
		Slivovi   []models.Sliv     `json:"slivovi"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &popis); err != nil || len(popis.Kisomjeri) != 1 || len(popis.Slivovi) != 1 {
		t.Fatalf("popis: %v %s", err, rw.Body.String())
	}
	if popis.Slivovi[0].Km2 == nil || *popis.Slivovi[0].Km2 != 14649 {
		t.Errorf("sliv u popisu: %+v", popis.Slivovi[0])
	}

	// brisanje arhivira, popis ostaje prazan
	w = posalji(admin, h.HandleDelete, "/api/slivovi/kisomjer/delete", obrazac, "code=a-a1-pobrde")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("delete: status %d: %s", w.Code, w.Body.String())
	}
	if tocke, _ := svc.ListKisomjeri(ctx); len(tocke) != 0 {
		t.Errorf("nakon brisanja %d točaka", len(tocke))
	}
}
