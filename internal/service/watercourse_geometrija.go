package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// PripremiGeometrijuVode provjerava GeoJSON toka prije upisa i vraća ga kao
// zbirku značajki, kakvu karta vodotoka čita; zbirka ostaje kakva je stigla.
//
// Datoteka dolazi iz raznih alata — QGIS, geojson.io, izvoz iz OSM-a — pa se
// prima zbirka, jedna značajka ili gola geometrija. Traži se bar jedna crta
// (LineString ili MultiLineString) u zemljopisnim koordinatama WGS84
// (EPSG:4326), redom dužina pa širina. Izvoz u HTRS96/TM daje metre, a neki
// alati zamijene redoslijed; oba bi ucrtala rijeku izvan karte, pa se
// odbijaju s objašnjenjem umjesto da se tiho upišu.
func PripremiGeometrijuVode(geojson string) (string, error) {
	var root map[string]any
	if err := json.Unmarshal([]byte(geojson), &root); err != nil {
		return "", errors.New("datoteka nije ispravan GeoJSON")
	}
	vrsta, _ := root["type"].(string)
	izvorno := vrsta == "FeatureCollection" // zbirka se upisuje kakva je stigla
	switch vrsta {
	case "FeatureCollection":
	case "Feature":
		root = map[string]any{"type": "FeatureCollection", "features": []any{root}}
	case "LineString", "MultiLineString":
		root = map[string]any{"type": "FeatureCollection", "features": []any{
			map[string]any{"type": "Feature", "properties": map[string]any{}, "geometry": root},
		}}
	default:
		return "", fmt.Errorf("GeoJSON vrste %q nije tok — očekuje se crta (LineString ili MultiLineString)", vrsta)
	}

	features, _ := root["features"].([]any)
	crta := 0
	for _, f := range features {
		fm, _ := f.(map[string]any)
		g, _ := fm["geometry"].(map[string]any)
		gv, _ := g["type"].(string)
		var nizovi []any
		switch gv {
		case "LineString":
			nizovi = []any{g["coordinates"]}
		case "MultiLineString":
			nizovi, _ = g["coordinates"].([]any)
		default:
			continue
		}
		for _, n := range nizovi {
			tocke, _ := n.([]any)
			if len(tocke) < 2 {
				return "", errors.New("crta toka ima manje od dvije točke")
			}
			for _, t := range tocke {
				if err := provjeriTocku(t); err != nil {
					return "", err
				}
			}
		}
		crta++
	}
	if crta == 0 {
		return "", errors.New("u datoteci nema crte toka (LineString ili MultiLineString)")
	}
	if izvorno {
		return geojson, nil
	}
	out, err := json.Marshal(root)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// provjeriTocku traži [dužina, širina] u stupnjevima, na našem dijelu Europe
func provjeriTocku(t any) error {
	p, _ := t.([]any)
	if len(p) < 2 {
		return errors.New("točka nema dvije koordinate")
	}
	lon, ok1 := p[0].(float64)
	lat, ok2 := p[1].(float64)
	if !ok1 || !ok2 {
		return errors.New("koordinate nisu brojevi")
	}
	if lon < -180 || lon > 180 || lat < -90 || lat > 90 {
		return fmt.Errorf("koordinate %.0f, %.0f nisu u stupnjevima — izvezite sloj u WGS84 (EPSG:4326), ne u HTRS96/TM", lon, lat)
	}
	if lon > 40 && lon < 50 && lat > 10 && lat < 25 {
		return fmt.Errorf("koordinate %.4f, %.4f izgledaju zamijenjeno — GeoJSON traži dužinu pa širinu", lon, lat)
	}
	if lon < 5 || lon > 30 || lat < 40 || lat > 52 {
		return fmt.Errorf("točka %.4f, %.4f je daleko izvan Podunavlja — provjerite koordinatni sustav", lon, lat)
	}
	return nil
}

// NapomenaToka je napomena uz crtu toka koju karta prikazuje ispod naziva
// (svojstvo stacionaza_napomena prve crte); prazno kad je nema.
func NapomenaToka(geojson string) string {
	var fc struct {
		Features []struct {
			Geometry struct {
				Type string `json:"type"`
			} `json:"geometry"`
			Properties map[string]any `json:"properties"`
		} `json:"features"`
	}
	if json.Unmarshal([]byte(geojson), &fc) != nil {
		return ""
	}
	for _, f := range fc.Features {
		if f.Geometry.Type == "LineString" || f.Geometry.Type == "MultiLineString" {
			s, _ := f.Properties["stacionaza_napomena"].(string)
			return s
		}
	}
	return ""
}

// SNapomenomToka upisuje napomenu uz prvu crtu toka; prazan tekst je briše.
// Ostatak geometrije ostaje kakav jest.
func SNapomenomToka(geojson, tekst string) (string, error) {
	var fc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(geojson), &fc); err != nil {
		return "", errors.New("geometrija toka nije ispravan GeoJSON")
	}
	var features []map[string]json.RawMessage
	if err := json.Unmarshal(fc["features"], &features); err != nil {
		return "", errors.New("geometrija toka nema značajki")
	}
	for i, f := range features {
		var g struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(f["geometry"], &g)
		if g.Type != "LineString" && g.Type != "MultiLineString" {
			continue
		}
		props := map[string]any{}
		if len(f["properties"]) > 0 && string(f["properties"]) != "null" {
			if err := json.Unmarshal(f["properties"], &props); err != nil {
				return "", err
			}
		}
		if tekst == "" {
			delete(props, "stacionaza_napomena")
		} else {
			props["stacionaza_napomena"] = tekst
		}
		b, _ := json.Marshal(props)
		features[i]["properties"] = b
		fb, _ := json.Marshal(features)
		fc["features"] = fb
		out, err := json.Marshal(fc)
		return string(out), err
	}
	return "", errors.New("u geometriji nema crte toka")
}

// SpojiCrteToka slaže zapis toka nakon uređivanja na karti: crte uzima s
// karte, a točke uz tok (stacionažu rkm) i vrh zbirke (naziv, crs) iz
// spremljenog zapisa. Karta Dunava i Drave prikazuje stacionažu koju program
// sam izračuna prema ENC-u; da se ona ne upiše umjesto izvornih točaka, s
// karte se uzimaju samo crte. Kad program i napomenu uz tok računa sam
// (racunaNapomenu), napomena s karte se odbacuje, a ostaje ona iz zapisa.
func SpojiCrteToka(spremljeno, crte string, racunaNapomenu bool) (string, error) {
	_, saKarte, err := razloziZbirku(crte)
	if err != nil {
		return "", errors.New("crte s karte nisu ispravan GeoJSON")
	}
	vrh, stare, err := razloziZbirku(spremljeno)
	if err != nil {
		// pokvaren zapis nema točaka koje bi se sačuvale; tok se piše iznova
		vrh, stare = map[string]json.RawMessage{}, nil
	}
	vrh["type"] = json.RawMessage(`"FeatureCollection"`)
	vrh["features"], err = json.Marshal(crteIOznake(saKarte, stare, racunaNapomenu))
	if err != nil {
		return "", err
	}
	out, err := json.Marshal(vrh)
	if err != nil {
		return "", err
	}
	if napomena := NapomenaToka(spremljeno); racunaNapomenu && napomena != "" {
		return SNapomenomToka(string(out), napomena)
	}
	return string(out), nil
}

// crteIOznake vraća crte s karte i ostale značajke (točke) iz zapisa
func crteIOznake(saKarte, stare []json.RawMessage, racunaNapomenu bool) []json.RawMessage {
	znacajke := make([]json.RawMessage, 0, len(saKarte)+len(stare))
	for _, f := range saKarte {
		if !jeCrtaToka(f) {
			continue
		}
		if racunaNapomenu {
			f = bezNapomene(f)
		}
		znacajke = append(znacajke, f)
	}
	for _, f := range stare {
		if !jeCrtaToka(f) {
			znacajke = append(znacajke, f)
		}
	}
	return znacajke
}

// razloziZbirku vraća vrh zbirke GeoJSON-a i njezine značajke; prazan tekst
// je prazna zbirka
func razloziZbirku(s string) (map[string]json.RawMessage, []json.RawMessage, error) {
	vrh := map[string]json.RawMessage{}
	if strings.TrimSpace(s) == "" {
		return vrh, nil, nil
	}
	if err := json.Unmarshal([]byte(s), &vrh); err != nil {
		return nil, nil, err
	}
	var znacajke []json.RawMessage
	if len(vrh["features"]) > 0 {
		if err := json.Unmarshal(vrh["features"], &znacajke); err != nil {
			return nil, nil, err
		}
	}
	return vrh, znacajke, nil
}

// jeCrtaToka kaže je li značajka crta (LineString ili MultiLineString)
func jeCrtaToka(f json.RawMessage) bool {
	var z struct {
		Geometry struct {
			Type string `json:"type"`
		} `json:"geometry"`
	}
	_ = json.Unmarshal(f, &z) // neispravna značajka nije crta
	return z.Geometry.Type == "LineString" || z.Geometry.Type == "MultiLineString"
}

// bezNapomene vraća značajku bez napomene o stacionaži; ostala svojstva ostaju
func bezNapomene(f json.RawMessage) json.RawMessage {
	var z, svojstva map[string]json.RawMessage
	if json.Unmarshal(f, &z) != nil || json.Unmarshal(z["properties"], &svojstva) != nil {
		return f
	}
	if _, ima := svojstva["stacionaza_napomena"]; !ima {
		return f
	}
	delete(svojstva, "stacionaza_napomena")
	z["properties"], _ = json.Marshal(svojstva) // mapa sirovih JSON-a uvijek se zapiše
	out, err := json.Marshal(z)
	if err != nil {
		return f
	}
	return out
}
