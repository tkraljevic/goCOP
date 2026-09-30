package service

import (
	"strings"
	"testing"
)

func TestPripremiGeometrijuVode(t *testing.T) {
	crta := `{"type":"LineString","coordinates":[[18.53,45.79],[18.86,45.85]]}`
	for _, c := range []struct {
		naziv, ulaz, greska string
	}{
		{"gola crta", crta, ""},
		{"značajka", `{"type":"Feature","properties":{"name":"Karašica"},"geometry":` + crta + `}`, ""},
		{"zbirka s točkama rkm", `{"type":"FeatureCollection","features":[{"type":"Feature","geometry":` + crta + `},{"type":"Feature","geometry":{"type":"Point","coordinates":[18.8,45.8]}}]}`, ""},
		{"više crta", `{"type":"MultiLineString","coordinates":[[[18.5,45.7],[18.6,45.8]],[[18.6,45.8],[18.7,45.85]]]}`, ""},
		{"HTRS96/TM u metrima", `{"type":"LineString","coordinates":[[672345.2,5076543.1],[680000,5080000]]}`, "WGS84"},
		{"zamijenjen redoslijed", `{"type":"LineString","coordinates":[[45.79,18.53],[45.85,18.86]]}`, "zamijenjeno"},
		{"samo točka", `{"type":"Point","coordinates":[18.5,45.8]}`, "nije tok"},
		{"zbirka bez crte", `{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"Point","coordinates":[18.8,45.8]}}]}`, "nema crte"},
		{"nije JSON", `nije`, "nije ispravan"},
	} {
		out, err := PripremiGeometrijuVode(c.ulaz)
		switch {
		case c.greska == "" && err != nil:
			t.Errorf("%s: %v", c.naziv, err)
		case c.greska == "" && !strings.Contains(out, `"FeatureCollection"`):
			t.Errorf("%s: nije zbirka: %s", c.naziv, out)
		case c.greska != "" && (err == nil || !strings.Contains(err.Error(), c.greska)):
			t.Errorf("%s: htio grešku s %q, dobio %v", c.naziv, c.greska, err)
		}
	}
}

func TestNapomenaToka(t *testing.T) {
	g := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"tip":"rkm","rkm":0},"geometry":{"type":"Point","coordinates":[18.8,45.8]}},{"type":"Feature","properties":{"name":"Karašica","stacionaza_napomena":"duga napomena"},"geometry":{"type":"LineString","coordinates":[[18.5,45.7],[18.8,45.8]]}}]}`
	if n := NapomenaToka(g); n != "duga napomena" {
		t.Fatalf("pročitano %q", n)
	}
	novo, err := SNapomenomToka(g, "pkm od ušća u Dunav")
	if err != nil || NapomenaToka(novo) != "pkm od ušća u Dunav" || !strings.Contains(novo, `"name":"Karašica"`) || !strings.Contains(novo, `"tip":"rkm"`) {
		t.Fatalf("upis: %v %s", err, novo)
	}
	bez, err := SNapomenomToka(novo, "")
	if err != nil || NapomenaToka(bez) != "" || !strings.Contains(bez, `"name":"Karašica"`) {
		t.Fatalf("brisanje: %v %s", err, bez)
	}
	if _, err := SNapomenomToka(`{"type":"FeatureCollection","features":[]}`, "x"); err == nil {
		t.Error("geometrija bez crte prihvaćena")
	}
}
