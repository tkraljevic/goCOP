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
)

// Jezero, akumulacija i retencija smiju imati površinu; tok vode ne smije
// biti samo površina, a površina mora imati prsten od bar četiri točke.
func TestPripremiPovrsinuVode(t *testing.T) {
	poligon := `{"type":"Polygon","coordinates":[[[18,45],[18.1,45],[18.1,45.1],[18,45]]]}`
	if out, err := PripremiPovrsinuVode(poligon); err != nil || !strings.Contains(out, `"FeatureCollection"`) {
		t.Errorf("površina: %v %s", err, out)
	}
	multi := `{"type":"Feature","properties":{},"geometry":{"type":"MultiPolygon","coordinates":[[[[18,45],[18.1,45],[18.1,45.1],[18,45]]]]}}`
	if _, err := PripremiPovrsinuVode(multi); err != nil {
		t.Errorf("više površina: %v", err)
	}
	if _, err := PripremiGeometrijuVode(poligon); err == nil {
		t.Error("tok ne smije biti samo površina")
	}
	for _, los := range []string{
		`{"type":"Polygon","coordinates":[[[18,45],[18.1,45],[18,45]]]}`,
		`{"type":"MultiPolygon","coordinates":[[[[18,45],[18.1,45],[18,45]]]]}`,
		`{"type":"Point","coordinates":[18,45]}`,
		`{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"Point","coordinates":[18,45]}}]}`,
		`{"type":"Polygon","coordinates":[[[455000,5050000],[456000,5050000],[456000,5051000],[455000,5050000]]]}`,
	} {
		if _, err := PripremiPovrsinuVode(los); err == nil {
			t.Errorf("trebala je greška: %s", los)
		}
	}
	if _, err := PripremiPovrsinuVode(`{"type":"LineString","coordinates":[[18,45],[18.1,45.1]]}`); err != nil {
		t.Errorf("crta uz površinu smije ostati: %v", err)
	}
}

// Akumulacija sprema površinu iz datoteke, a vodotok istu površinu odbije.
func TestSetWatercourseGeometryPovrsina(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "povrsina.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	repo := repository.NewWatercourseRepository(baza, ledger.New(baza, "test-node"))
	svc := NewWatercourseService(repo)
	ctx := context.Background()
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	for _, w := range []models.Watercourse{
		{Code: "akumulacija-proba", OfficialName: "akumulacija Proba", Name: "Proba", Kind: "akumulacija"},
		{Code: "potok-proba", OfficialName: "potok Proba", Name: "Proba", Kind: "potok"},
	} {
		if err := repo.CreateWatercourse(ctx, &w); err != nil {
			t.Fatal(err)
		}
	}
	poligon := `{"type":"Polygon","coordinates":[[[18,45],[18.1,45],[18.1,45.1],[18,45]]]}`
	if err := svc.SetWatercourseGeometry(ctx, admin, "akumulacija-proba", poligon); err != nil {
		t.Fatalf("akumulacija: %v", err)
	}
	w, err := svc.GetWatercourse(ctx, "akumulacija-proba")
	if err != nil || !strings.Contains(w.Geometry, "Polygon") {
		t.Errorf("spremljeno: %v %+v", err, w)
	}
	if err := svc.SetWatercourseGeometry(ctx, admin, "potok-proba", poligon); err == nil {
		t.Error("potok ne smije biti površina")
	}
}
