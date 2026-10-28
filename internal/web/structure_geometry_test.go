package web

import (
	"context"
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

func TestStructureGeometryAPI(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "web_structure_geo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if _, err := baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'B', 'VGO', 'COP');
		INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (34, 'B', 'Drava i Dunav', 'VGI');`); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test-node")
	svc := service.NewStructureService(repository.NewStructureRepository(baza, rec))
	ctx := context.Background()
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	n := &models.Structure{Code: "nasip-proba", Name: "Nasip Proba", Kind: models.StructureKindEmbankment, SectorID: "B", AreaID: 34}
	if err := svc.Create(ctx, admin, n); err != nil {
		t.Fatal(err)
	}
	h := NewStructuresHandler(svc, nil, nil, nil, nil, nil, nil)
	posalji := func(tijelo, vrsta string, perms *models.UserPermissions) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/structures/geometry", strings.NewReader(tijelo))
		req.Header.Set("Content-Type", vrsta)
		req = req.WithContext(context.WithValue(ctx, contextKeyPerms, perms))
		w := httptest.NewRecorder()
		h.HandleGeometryAPI(w, req)
		return w
	}
	crta := `{\"type\":\"LineString\",\"coordinates\":[[18.8,45.8],[18.9,45.7]]}`
	slucajevi := []struct {
		ime, tijelo, vrsta string
		perms              *models.UserPermissions
		status             int
	}{
		{"običan obrazac", `id=` + n.ID.String(), "application/x-www-form-urlencoded", admin, http.StatusUnsupportedMediaType},
		{"pokvaren JSON", `{`, "application/json", admin, http.StatusBadRequest},
		{"bez objekta", `{"id":"nije-id","geojson":"` + crta + `"}`, "application/json", admin, http.StatusBadRequest},
		{"bez prava", `{"id":"` + n.ID.String() + `","geojson":"` + crta + `"}`, "application/json", &models.UserPermissions{}, http.StatusBadRequest},
		{"nasip bez crte", `{"id":"` + n.ID.String() + `","geojson":"{\"type\":\"Point\",\"coordinates\":[18.8,45.8]}"}`, "application/json", admin, http.StatusBadRequest},
		{"ucrtaj", `{"id":"` + n.ID.String() + `","geojson":"` + crta + `"}`, "application/json", admin, http.StatusOK},
	}
	for _, s := range slucajevi {
		if w := posalji(s.tijelo, s.vrsta, s.perms); w.Code != s.status {
			t.Errorf("%s: status %d, htio %d (%s)", s.ime, w.Code, s.status, strings.TrimSpace(w.Body.String()))
		}
	}
	if st, err := svc.Get(ctx, n.ID); err != nil || st == nil || !st.HasGeometry() {
		t.Fatal("nasip nije ucrtan")
	}
	if w := posalji(`{"id":"`+n.ID.String()+`","geojson":""}`, "application/json", admin); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "obrisana") {
		t.Errorf("brisanje crte: %d %s", w.Code, w.Body.String())
	}
	if st, err := svc.Get(ctx, n.ID); err != nil || st == nil || st.HasGeometry() {
		t.Error("crta nije obrisana")
	}
}

// Stranica objekta dobiva pločice i ucrtani objekt pripremljen za skriptu
func TestStranicaObjektaDobivaKartu(t *testing.T) {
	h := NewStructuresHandler(nil, nil, nil, nil, nil, nil, nil)
	prazna := StructurePageData{Structure: &models.Structure{}}
	h.napuniKartu(&prazna)
	if prazna.Karta.Ima() || prazna.GeometryJSON != "" {
		t.Errorf("bez karte i crte stranica ne smije dobiti kartu: %+v", prazna.Karta)
	}
	h.SetKarta(func() KartaPostavke { return KartaPostavke{Plocice: "https://plocice.test/{z}/{x}/{y}.png"} })
	d := StructurePageData{Structure: &models.Structure{Geometry: `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"naziv":"<b>"},"geometry":null}]}`}}
	h.napuniKartu(&d)
	if !d.Karta.Ima() || d.GeometryJSON == "" || strings.Contains(string(d.GeometryJSON), "<") {
		t.Errorf("karta ili ucrtani objekt nisu pripremljeni (ili nisu sigurni za skriptu): %q", d.GeometryJSON)
	}
}
