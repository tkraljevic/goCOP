package geometrija

import (
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"sync"
)

//go:embed sidra/*.json
var encSidra embed.FS

type IzvorSidara struct {
	Izvor   string  `json:"izvor"`
	Izdanje string  `json:"izdanje"`
	Sidra   []Sidro `json:"sidra"`
}

// SluzbenaSidra čita verzionirani izvadak ENC-a, ne podatke postaja.
func SluzbenaSidra(code string) (IzvorSidara, error) {
	var izvor IzvorSidara
	if code != "rijeka-drava" && code != "rijeka-dunav" {
		return izvor, nil
	}
	b, err := encSidra.ReadFile("sidra/" + code + ".json")
	if err != nil {
		return izvor, err
	}
	err = json.Unmarshal(b, &izvor)
	return izvor, err
}

type encCacheEntry struct {
	hash [32]byte
	data []byte
}

var encCache = struct {
	sync.Mutex
	items map[string]encCacheEntry
}{items: make(map[string]encCacheEntry)}

// PrimijeniStacionazu izvodi prikaz iz geometrije baze, diska ili ugrađenog
// paketa. Ne piše u bazu: linija i položaji postaja ostaju nepromijenjeni.
// Cache čuva samo posljednju geometriju svake od dvije podržane rijeke.
func PrimijeniStacionazu(code string, data []byte) ([]byte, error) {
	if len(data) == 0 || (code != "rijeka-drava" && code != "rijeka-dunav") {
		return data, nil
	}
	hash := sha256.Sum256(data)
	encCache.Lock()
	defer encCache.Unlock()
	if item, ok := encCache.items[code]; ok && item.hash == hash {
		return append([]byte(nil), item.data...), nil
	}
	izvor, err := SluzbenaSidra(code)
	if err != nil {
		return nil, err
	}
	out, err := kalibrirajGeoJSON(data, izvor)
	if err == nil {
		encCache.items[code] = encCacheEntry{hash, append([]byte(nil), out...)}
	}
	return out, err
}

func kalibrirajGeoJSON(data []byte, izvor IzvorSidara) ([]byte, error) {
	var fc map[string]json.RawMessage
	if err := json.Unmarshal(data, &fc); err != nil {
		return nil, err
	}
	var features []map[string]json.RawMessage
	if err := json.Unmarshal(fc["features"], &features); err != nil {
		return nil, err
	}
	var linija [][2]float64
	lineIndex := -1
	for i, f := range features {
		var g struct {
			Type        string
			Coordinates json.RawMessage
		}
		if err := json.Unmarshal(f["geometry"], &g); err != nil {
			return nil, err
		}
		if g.Type == "LineString" && lineIndex < 0 {
			if err := json.Unmarshal(g.Coordinates, &linija); err != nil {
				return nil, err
			}
			lineIndex = i
		}
	}
	if lineIndex < 0 {
		return data, nil
	}
	k, err := NovaKalibracija(linija, izvor.Sidra)
	greska := err
	for _, s := range izvor.Sidra {
		p, err := Projektiraj(linija, s.Tocka)
		if err != nil {
			return nil, err
		}
		if p.UdaljenostKM > 1 {
			greska = fmt.Errorf("ENC sidro %.3f je %.3f km od linije; potreban pregled", s.Rkm, p.UdaljenostKM)
			break
		}
	}
	minR, maxR := 0.0, 0.0
	if greska == nil {
		minR, maxR = k.sidra[0].rkm, k.sidra[len(k.sidra)-1].rkm
	}
	if minR > maxR {
		minR, maxR = maxR, minR
	}
	napomena := fmt.Sprintf("Stacionaža kalibrirana prema ENC-u (%s), rkm %.3f–%.3f. Izvan tog raspona oznake su orijentacijske. Nije za navigaciju.", izvor.Izdanje, minR, maxR)
	if greska != nil {
		napomena = "Stacionaža nije kalibrirana: " + greska.Error() + ". Oznake su orijentacijske."
	}
	result := make([]map[string]json.RawMessage, 0, len(features)+len(izvor.Sidra))
	for i, f := range features {
		var props map[string]json.RawMessage
		if len(f["properties"]) > 0 {
			if err := json.Unmarshal(f["properties"], &props); err != nil {
				return nil, err
			}
		}
		if props == nil {
			props = make(map[string]json.RawMessage)
		}
		var tip string
		_ = json.Unmarshal(props["tip"], &tip)
		if tip == "rkm" {
			var rkm float64
			if err := json.Unmarshal(props["rkm"], &rkm); err != nil {
				return nil, err
			}
			if greska == nil && rkm >= minR && rkm <= maxR {
				continue
			}
			props["status"] = rawJSON("orijentacijski")
			props["oznaka"] = rawJSON(fmt.Sprintf("rkm %g · orijentacijski (nije kalibrirano)", rkm))
		}
		if i == lineIndex {
			props["stacionaza_napomena"] = rawJSON(napomena)
		}
		f["properties"] = rawJSON(props)
		result = append(result, f)
	}
	for _, s := range izvor.Sidra {
		if greska != nil {
			break
		}
		u, _ := k.Uzduz(s.Rkm)
		result = append(result, map[string]json.RawMessage{
			"type":     rawJSON("Feature"),
			"geometry": rawJSON(map[string]any{"type": "Point", "coordinates": TockaNaUzduz(linija, u)}),
			"properties": rawJSON(map[string]any{"tip": "rkm", "rkm": s.Rkm, "status": "kalibrirano",
				"oznaka": fmt.Sprintf("rkm %g · kalibrirano prema ENC-u %s", s.Rkm, izvor.Izdanje),
				"izvor":  izvor.Izvor, "celija": s.Izvor, "izvorna_tocka": s.Tocka}),
		})
	}
	fc["features"] = rawJSON(result)
	fc["stacionaza"] = rawJSON(map[string]any{"min_rkm": minR, "max_rkm": maxR, "izvor": izvor.Izvor, "izdanje": izvor.Izdanje, "metoda": "između susjednih ENC sidara po OSM liniji"})
	if greska != nil {
		fc["stacionaza"] = rawJSON(map[string]any{"status": "nije_kalibrirano", "razlog": greska.Error()})
	}
	return json.Marshal(fc)
}

func rawJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
