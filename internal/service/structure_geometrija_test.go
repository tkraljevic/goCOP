package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"

	"github.com/google/uuid"
)

func TestPripremiGeometrijuObjekta(t *testing.T) {
	crta := `{"type":"LineString","coordinates":[[18.80,45.80],[18.81,45.79]]}`
	tocka := `{"type":"Point","coordinates":[18.80,45.80]}`
	slucajevi := []struct {
		ime, vrsta, geo, greska string
	}{
		{"nasip kao gola crta", models.StructureKindEmbankment, crta, ""},
		{"nasip bez crte", models.StructureKindEmbankment, tocka, "nema crte nasipa"},
		{"crpna stanica kao točka", models.StructureKindPumpingStation, tocka, ""},
		{"HTRS96 umjesto WGS84", models.StructureKindEmbankment, `{"type":"LineString","coordinates":[[656000,5070000],[657000,5071000]]}`, "nisu u stupnjevima"},
		{"zamijenjene koordinate", models.StructureKindPumpingStation, `{"type":"Point","coordinates":[45.80,18.80]}`, "zamijenjeno"},
		{"poligon s krivom točkom", models.StructureKindDam, `{"type":"Polygon","coordinates":[[[18.8,45.8],[18.9,45.8],[180.5,45.9],[18.8,45.8]]]}`, "nisu u stupnjevima"},
		{"nije GeoJSON", models.StructureKindEmbankment, `nasip`, "nije ispravan GeoJSON"},
		{"značajka", models.StructureKindEmbankment, `{"type":"Feature","properties":{},"geometry":` + crta + `}`, ""},
		{"nepoznata vrsta", models.StructureKindEmbankment, `{"type":"GeometryCollection","geometries":[]}`, "nije geometrija objekta"},
		{"prazna zbirka", models.StructureKindPumpingStation, `{"type":"FeatureCollection","features":[]}`, "nema geometrije objekta"},
		{"točka bez koordinata", models.StructureKindPumpingStation, `{"type":"Point","coordinates":[]}`, "nema koordinata"},
	}
	for _, s := range slucajevi {
		out, err := PripremiGeometrijuObjekta(s.vrsta, s.geo)
		switch {
		case s.greska == "" && err != nil:
			t.Errorf("%s: neočekivana greška %v", s.ime, err)
		case s.greska != "" && (err == nil || !strings.Contains(err.Error(), s.greska)):
			t.Errorf("%s: htio grešku s %q, dobio %v", s.ime, s.greska, err)
		case s.greska == "" && !strings.Contains(out, `"FeatureCollection"`):
			t.Errorf("%s: gola geometrija nije umotana u zbirku: %s", s.ime, out)
		}
	}
}

// Zbirka iz QGIS-a upisuje se kakva je stigla, s redoslijedom i svojstvima
func TestZbirkaObjektaOstajeKakvaJest(t *testing.T) {
	zbirka := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"tip":"km","km":"1+000"},"geometry":{"type":"Point","coordinates":[18.8,45.8]}},{"type":"Feature","properties":{"tip":"nasip"},"geometry":{"type":"LineString","coordinates":[[18.8,45.8],[18.9,45.7]]}}]}`
	out, err := PripremiGeometrijuObjekta(models.StructureKindEmbankment, zbirka)
	if err != nil || out != zbirka {
		t.Fatalf("zbirka je promijenjena ili odbijena: %v", err)
	}
}

func TestSetGeometryProvjeravaPravoIObjekt(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if _, err := baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'B', 'VGO', 'COP');
		INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (34, 'B', 'Drava i Dunav', 'VGI');`); err != nil {
		t.Fatal(err)
	}
	svc := NewStructureService(repository.NewStructureRepository(baza, ledger.New(baza, "cvor")))
	ctx := context.Background()
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	n := &models.Structure{Code: "nasip-proba", Name: "Nasip Proba", Kind: models.StructureKindEmbankment, SectorID: "B", AreaID: 34}
	if err := svc.Create(ctx, admin, n); err != nil {
		t.Fatal(err)
	}
	crta := `{"type":"LineString","coordinates":[[18.8,45.8],[18.9,45.7]]}`
	if err := svc.SetGeometry(ctx, admin, uuid.New(), crta); err == nil {
		t.Error("nepostojeći objekt primio je crtu")
	}
	if err := svc.SetGeometry(ctx, &models.UserPermissions{}, n.ID, crta); err == nil {
		t.Error("korisnik bez prava ucrtao je nasip")
	}
	if err := svc.SetGeometry(ctx, admin, n.ID, `{"type":"Point","coordinates":[18.8,45.8]}`); err == nil {
		t.Error("nasip bez crte je prihvaćen")
	}
	if err := svc.SetGeometry(ctx, admin, n.ID, crta); err != nil {
		t.Fatal(err)
	}
	if st, err := svc.Get(ctx, n.ID); err != nil || st == nil || !strings.Contains(st.Geometry, `"FeatureCollection"`) {
		t.Errorf("nasip nije ucrtan kao zbirka: %+v", st)
	}
	if err := svc.SetGeometry(ctx, admin, n.ID, " "); err != nil {
		t.Fatal(err)
	}
	if st, err := svc.Get(ctx, n.ID); err != nil || st == nil || st.HasGeometry() {
		t.Error("prazan unos nije obrisao crtu")
	}
}
