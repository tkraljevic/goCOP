package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gocop/internal/models"

	"github.com/google/uuid"
)

// PripremiGeometrijuObjekta provjerava GeoJSON objekta prije upisa i vraća
// ga kao zbirku značajki. Prima ono što daju QGIS i geojson.io: zbirku, jednu
// značajku ili golu geometriju. Koordinate se provjeravaju istim pravilom kao
// tok vode (WGS84, dužina pa širina, u Podunavlju). Nasip mora imati crtu;
// točke uz nju su stacionaža. Ostali objekti smiju biti i samo točka.
func PripremiGeometrijuObjekta(kind, geojson string) (string, error) {
	var root map[string]any
	if err := json.Unmarshal([]byte(geojson), &root); err != nil {
		return "", errors.New("datoteka nije ispravan GeoJSON")
	}
	zbirka, izvorno, err := kaoZbirka(root)
	if err != nil {
		return "", err
	}
	crta, ukupno, err := prebrojiGeometrije(zbirka)
	if err != nil {
		return "", err
	}
	if ukupno == 0 {
		return "", errors.New("u datoteci nema geometrije objekta")
	}
	if kind == models.StructureKindEmbankment && crta == 0 {
		return "", errors.New("u datoteci nema crte nasipa (LineString ili MultiLineString)")
	}
	if izvorno {
		return geojson, nil
	}
	out, err := json.Marshal(zbirka)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// kaoZbirka umata značajku ili golu geometriju u zbirku; zbirka koja je
// stigla kao zbirka upisuje se kakva jest
func kaoZbirka(root map[string]any) (map[string]any, bool, error) {
	vrsta, _ := root["type"].(string)
	switch vrsta {
	case "FeatureCollection":
		return root, true, nil
	case "Feature":
		return map[string]any{"type": "FeatureCollection", "features": []any{root}}, false, nil
	case "Point", "MultiPoint", "LineString", "MultiLineString", "Polygon", "MultiPolygon":
		return map[string]any{"type": "FeatureCollection", "features": []any{
			map[string]any{"type": "Feature", "properties": map[string]any{}, "geometry": root},
		}}, false, nil
	}
	return nil, false, fmt.Errorf("GeoJSON vrste %q nije geometrija objekta", vrsta)
}

// prebrojiGeometrije provjeri koordinate svake značajke i vrati koliko je
// crta i koliko geometrija ukupno; nepoznate vrste se preskaču
func prebrojiGeometrije(zbirka map[string]any) (crta, ukupno int, err error) {
	features, _ := zbirka["features"].([]any)
	for _, f := range features {
		fm, _ := f.(map[string]any)
		g, _ := fm["geometry"].(map[string]any)
		switch gv, _ := g["type"].(string); gv {
		case "LineString", "MultiLineString":
			crta++
		case "Point", "MultiPoint", "Polygon", "MultiPolygon":
		default:
			continue
		}
		if err := provjeriKoordinate(g["coordinates"]); err != nil {
			return 0, 0, err
		}
		ukupno++
	}
	return crta, ukupno, nil
}

// provjeriKoordinate silazi kroz ugniježđene nizove do točaka
func provjeriKoordinate(c any) error {
	niz, ok := c.([]any)
	if !ok || len(niz) == 0 {
		return errors.New("geometrija nema koordinata")
	}
	if _, broj := niz[0].(float64); broj {
		return provjeriTocku(niz)
	}
	for _, d := range niz {
		if err := provjeriKoordinate(d); err != nil {
			return err
		}
	}
	return nil
}

// SetGeometry upisuje ili briše (prazan tekst) ucrtani objekt. Smije onaj
// tko smije uređivati objekt.
func (s *StructureService) SetGeometry(ctx context.Context, perms *models.UserPermissions, id uuid.UUID, geojson string) error {
	current, err := s.repo.GetStructure(ctx, id)
	if err != nil {
		return err
	}
	if current == nil {
		return errors.New("objekt nije pronađen")
	}
	if !s.CanEdit(perms, current) {
		return errors.New("nemate pravo uređivati ovaj objekt")
	}
	if strings.TrimSpace(geojson) != "" {
		if geojson, err = PripremiGeometrijuObjekta(current.Kind, geojson); err != nil {
			return err
		}
	}
	return s.repo.SetStructureGeometry(ctx, id, geojson)
}
