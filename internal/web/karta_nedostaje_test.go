package web

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Kanal Proba teče na istok: kkm 10 je na zapadnom kraju, kkm 0 na ušću.
const tokKanalaProba = `{"type":"FeatureCollection","features":[
	{"type":"Feature","properties":{"tip":"vodotok"},"geometry":{"type":"LineString","coordinates":[[18.0,45.0],[18.1,45.0],[18.2,45.0]]}},
	{"type":"Feature","properties":{"tip":"rkm","rkm":10,"status":"kalibrirano"},"geometry":{"type":"Point","coordinates":[18.0,45.0]}},
	{"type":"Feature","properties":{"tip":"rkm","rkm":0,"status":"kalibrirano"},"geometry":{"type":"Point","coordinates":[18.2,45.0]}}]}`

type probaNedostaje struct {
	objekti  []models.Structure
	vodotoci []models.Watercourse
	letve    []models.Station
	dionice  []models.Section
}

func (p probaNedostaje) slozi(t *testing.T) podaciKarte {
	t.Helper()
	h := NewKartaHandler(
		func(context.Context) ([]models.Structure, error) { return p.objekti, nil },
		func(context.Context) ([]models.Watercourse, error) { return p.vodotoci, nil },
		func(context.Context) ([]models.Station, error) { return p.letve, nil },
		func(context.Context) ([]models.Section, error) { return p.dionice, nil }, nil, nil)
	k, err := h.slozi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return k.zaKartu
}

func redak(t *testing.T, popis []nedostajeNaKarti, naziv string) nedostajeNaKarti {
	t.Helper()
	for _, r := range popis {
		if r.Naziv == naziv {
			return r
		}
	}
	t.Fatalf("%q nije na popisu što nedostaje", naziv)
	return nedostajeNaKarti{}
}

func priblizno(t *testing.T, zbirka geoZbirka, adresa string) (string, [][2]float64, map[string]any) {
	t.Helper()
	for _, f := range zbirka.Features {
		if f.Properties["adresa"] != adresa {
			continue
		}
		var g struct {
			Type        string          `json:"type"`
			Coordinates json.RawMessage `json:"coordinates"`
		}
		if err := json.Unmarshal(f.Geometry, &g); err != nil {
			t.Fatal(err)
		}
		var tocke [][2]float64
		if g.Type == "Point" {
			var t1 [2]float64
			if err := json.Unmarshal(g.Coordinates, &t1); err != nil {
				t.Fatal(err)
			}
			tocke = [][2]float64{t1}
		} else if err := json.Unmarshal(g.Coordinates, &tocke); err != nil {
			t.Fatal(err)
		}
		return g.Type, tocke, f.Properties
	}
	return "", nil, nil
}

// Nasip bez crte dobije približno mjesto od km do km iz dionice, nizvodno i
// uz svoju obalu; ono što mjesta nema kaže zašto.
func TestKartaNedostajeNasipi(t *testing.T) {
	lijevi := models.Structure{ID: uuid.New(), Name: "Nasip uz l.o. kanala Proba", Kind: models.StructureKindEmbankment, AreaID: 16}
	bezKm := models.Structure{ID: uuid.New(), Name: "Nasip bez stacionaže", Kind: models.StructureKindEmbankment, AreaID: 16}
	bezToka := models.Structure{ID: uuid.New(), Name: "Nasip uz neucrtan potok", Kind: models.StructureKindEmbankment, AreaID: 17}
	izvan := models.Structure{ID: uuid.New(), Name: "Nasip izvan km", Kind: models.StructureKindEmbankment, AreaID: 16}
	izPoddionice := models.Structure{ID: uuid.New(), Name: "Nasip po poddionici", Kind: models.StructureKindEmbankment, AreaID: 16}
	nigdje := models.Structure{ID: uuid.New(), Name: "Nasip nigdje", Kind: models.StructureKindEmbankment}
	nepoznatVodotok := models.Structure{ID: uuid.New(), Name: "Ustava na vodi izvan registra", Kind: models.StructureKindSluice, AreaID: 18}
	p := probaNedostaje{
		objekti: []models.Structure{lijevi, bezKm, bezToka, izvan, izPoddionice, nigdje, nepoznatVodotok},
		vodotoci: []models.Watercourse{
			{Code: "kanal-proba", Name: "Proba", OfficialName: "kanal Proba", Geometry: tokKanalaProba},
			{Code: "potok-neucrtan", Name: "Neucrtan", OfficialName: "potok Neucrtan"},
		},
		dionice: []models.Section{
			{Code: "B.16.1", AreaID: 16, AreaName: "Mali sliv Proba", Parts: []models.SectionPart{{
				WatercourseCode: "kanal-proba", StationingKind: "kkm",
				Embankments: []models.PartEmbankment{
					{StructureID: lijevi.ID.String(), Name: "Nasip uz l.o.", WaterFrom: km(2), WaterTo: km(4)},
					{StructureID: bezKm.ID.String(), Name: "Nasip bez stacionaže"},
					{StructureID: izvan.ID.String(), Name: "Nasip izvan km", WaterFrom: km(12), WaterTo: km(15)},
				},
			}, {
				WatercourseCode: "kanal-proba", StationingKind: "kkm", Bank: "D", KmFrom: km(9), KmTo: km(12),
				Embankments: []models.PartEmbankment{{StructureID: izPoddionice.ID.String(), Name: "Nasip po poddionici"}},
			}}},
			{Code: "B.17.2", AreaID: 17, Parts: []models.SectionPart{{
				WatercourseCode: "potok-neucrtan", StationingKind: "pkm",
				Embankments: []models.PartEmbankment{{StructureID: bezToka.ID.String(), Name: "Nasip", WaterFrom: km(0), WaterTo: km(1.25)}},
			}}},
			{Code: "B.17.3", AreaID: 17, Parts: []models.SectionPart{{
				WatercourseCode: "potok-neucrtan", StationingKind: "pkm",
				Embankments: []models.PartEmbankment{{StructureID: bezToka.ID.String(), Name: "Nasip", WaterFrom: km(2), WaterTo: km(3)}},
			}, {
				WatercourseCode: "kanal-proba", StationingKind: "kkm",
				Embankments: []models.PartEmbankment{{StructureID: izvan.ID.String(), Name: "Nasip izvan km"}},
			}}},
			{Code: "B.18.1", AreaID: 18, Parts: []models.SectionPart{{
				WatercourseCode: "nema-ga-u-registru", StationingKind: "rkm",
				Objects: []models.PartObject{{StructureID: nepoznatVodotok.ID.String(), Stationing: km(3)}},
			}}},
		},
	}
	d := p.slozi(t)

	r := redak(t, d.Nedostaje, lijevi.Name)
	if r.Sloj != "nasipi" || r.Skupina != "BP 16 · Mali sliv Proba" || r.Gdje != "B.16.1 · kanal Proba · kkm 2+000 – 4+000" || !r.Mjesto || r.Adresa != lijevi.Path() {
		t.Errorf("nasip s mjestom: %+v", r)
	}
	vrsta, crta, svojstva := priblizno(t, d.Priblizno, lijevi.Path())
	if vrsta != "LineString" || len(crta) < 2 || svojstva["obala"] != "L" || svojstva["sloj"] != "nasipi" {
		t.Fatalf("približno mjesto: %s %v %v", vrsta, crta, svojstva)
	}
	// kkm 4 je uzvodno (zapadnije), kkm 2 nizvodno: crta ide na istok
	if crta[0][0] > crta[len(crta)-1][0] || crta[0][0] < 18.11 || crta[len(crta)-1][0] > 18.17 {
		t.Errorf("crta treba ići nizvodno od kkm 4 do kkm 2: %v", crta)
	}

	for naziv, gdje := range map[string]string{
		bezKm.Name:   "B.16.1 · kanal Proba · bez stacionaže u dionici, nema mjesta na karti",
		bezToka.Name: "B.17.2 · potok Neucrtan · pkm 0+000 – 1+250; B.17.3 · pkm 2+000 – 3+000 · vodotok nije ucrtan",
		izvan.Name:   "B.16.1 · kanal Proba · kkm 12+000 – 15+000 · izvan km ucrtanih na vodotoku; B.17.3 · bez stacionaže u dionici, nema mjesta na karti",
		nigdje.Name:  "nije ni u jednoj dionici, nema mjesta na karti",
	} {
		if r := redak(t, d.Nedostaje, naziv); r.Gdje != gdje || r.Mjesto {
			t.Errorf("%s: %q (mjesto %v), htio %q", naziv, r.Gdje, r.Mjesto, gdje)
		}
	}
	if r := redak(t, d.Nedostaje, nepoznatVodotok.Name); r.Gdje != "B.18.1 · rkm 3+000 · vodotok nije ucrtan" || r.Sloj != "ustave" {
		t.Errorf("voda izvan registra: %+v", r)
	}
	if r := redak(t, d.Nedostaje, bezToka.Name); r.Skupina != "BP 17" {
		t.Errorf("područje bez naziva: %q", r.Skupina)
	}
	if r := redak(t, d.Nedostaje, nigdje.Name); r.Skupina != "Bez branjenog područja" {
		t.Errorf("bez područja: %q", r.Skupina)
	}

	// km poddionice kad nasip nema svoj, obala poddionice, dio izvan km se odreže
	r = redak(t, d.Nedostaje, izPoddionice.Name)
	_, crta, svojstva = priblizno(t, d.Priblizno, izPoddionice.Path())
	if !r.Mjesto || svojstva["obala"] != "D" || len(crta) < 2 || crta[0][0] > 18.001 {
		t.Errorf("nasip po poddionici: %+v, crta %v, %v", r, crta, svojstva)
	}

	// popis nasipa ide po području: BP 16, BP 17, pa bez područja
	var redom []string
	for _, r := range d.Nedostaje {
		if r.Sloj != "nasipi" {
			continue
		}
		if len(redom) == 0 || redom[len(redom)-1] != r.Skupina {
			redom = append(redom, r.Skupina)
		}
	}
	if strings.Join(redom, " | ") != "BP 16 · Mali sliv Proba | BP 17 | Bez branjenog područja" {
		t.Errorf("redoslijed skupina: %v", redom)
	}
}

// Objekt bez položaja dobije točku na svom km; km po nasipu ne kaže gdje je
// objekt uz vodu.
func TestKartaNedostajeObjekti(t *testing.T) {
	sifon := models.Structure{ID: uuid.New(), Name: "Sifon Proba", Kind: models.StructureKindSiphon, AreaID: 16}
	cep := models.Structure{ID: uuid.New(), Name: "Čep na nasipu", Kind: models.StructureKindPlug, AreaID: 16}
	ucrtana := models.Structure{ID: uuid.New(), Name: "CS ucrtana", Kind: models.StructureKindPumpingStation, Latitude: kartaTocka(45), Longitude: kartaTocka(18)}
	d := probaNedostaje{
		objekti:  []models.Structure{sifon, cep, ucrtana},
		vodotoci: []models.Watercourse{{Code: "kanal-proba", OfficialName: "kanal Proba", Geometry: tokKanalaProba}},
		dionice: []models.Section{{Code: "B.16.1", AreaID: 16, Parts: []models.SectionPart{{
			WatercourseCode: "kanal-proba", StationingKind: "kkm",
			Objects: []models.PartObject{
				{StructureID: sifon.ID.String(), StationingText: "kkm 5+000"},
				{StructureID: cep.ID.String(), StationingKind: "nkm", Stationing: km(1.2)},
				{Name: "most, nije naš"},
			},
		}}}},
	}.slozi(t)
	if len(d.Nedostaje) != 2 {
		t.Fatalf("nedostaju sifon i čep, a popis je %+v", d.Nedostaje)
	}
	r := redak(t, d.Nedostaje, sifon.Name)
	vrsta, tocke, _ := priblizno(t, d.Priblizno, sifon.Path())
	if r.Sloj != "ostali-objekti" || r.Gdje != "B.16.1 · kanal Proba · kkm 5+000" || !r.Mjesto || vrsta != "Point" || tocke[0][0] < 18.09 || tocke[0][0] > 18.11 {
		t.Errorf("sifon na kkm 5: %+v, %s %v", r, vrsta, tocke)
	}
	if r := redak(t, d.Nedostaje, cep.Name); r.Mjesto || !strings.Contains(r.Gdje, "bez stacionaže") {
		t.Errorf("čep po nasipu: %+v", r)
	}
}

// Vodotok bez toka stoji uz dionice u kojima je; letva bez položaja dobije
// točku iz svoje stacionaže.
func TestKartaNedostajeVodotociILetve(t *testing.T) {
	naKmu := models.Station{ID: uuid.New(), Name: "Letva Proba", Watercourse: "kanal Proba", WatercourseCode: "kanal-proba", Stationing: "kkm 7+500"}
	bezStacionaze := models.Station{ID: uuid.New(), Name: "Letva bez stacionaže", Watercourse: "kanal Proba"}
	bezToka := models.Station{ID: uuid.New(), Name: "Letva na neucrtanom", WatercourseCode: "potok-neucrtan", Stationing: "pkm 1+000"}
	daleko := models.Station{ID: uuid.New(), Name: "Letva daleko", WatercourseCode: "kanal-proba", Watercourse: "kanal Proba", Stationing: "kkm 40+000"}
	bezKm := models.Station{ID: uuid.New(), Name: "Letva na toku bez km", WatercourseCode: "potok-bez-km", Stationing: "pkm 2+000"}
	d := probaNedostaje{
		vodotoci: []models.Watercourse{
			{Code: "kanal-proba", OfficialName: "kanal Proba", Geometry: tokKanalaProba},
			{Code: "potok-neucrtan", Name: "Neucrtan", OfficialName: "potok Neucrtan"},
			{Code: "potok-sam", Name: "Sam"},
			{Code: "potok-bez-km", Name: "Bez km", Geometry: `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":{"type":"LineString","coordinates":[[18,45],[18.1,45]]}}]}`},
		},
		letve: []models.Station{naKmu, bezStacionaze, bezToka, daleko, bezKm},
		dionice: []models.Section{
			{Code: "B.16.2", AreaID: 16, AreaName: "Mali sliv Proba", Parts: []models.SectionPart{{WatercourseCode: "potok-neucrtan"}, {WatercourseCode: "potok-neucrtan"}}},
			{Code: "B.16.10", AreaID: 16, Parts: []models.SectionPart{{WatercourseCode: "potok-neucrtan"}}},
		},
	}.slozi(t)

	if r := redak(t, d.Nedostaje, "potok Neucrtan"); r.Sloj != "vodotoci-bez-reda" || r.Gdje != "dionice B.16.2, B.16.10" || r.Skupina != "BP 16 · Mali sliv Proba" || r.Adresa != "/watercourses/potok-neucrtan" {
		t.Errorf("vodotok u dionicama: %+v", r)
	}
	if r := redak(t, d.Nedostaje, "Sam"); r.Gdje != "nije ni u jednoj dionici" || r.Skupina != "Nije ni u jednoj dionici" {
		t.Errorf("vodotok izvan dionica: %+v", r)
	}

	r := redak(t, d.Nedostaje, naKmu.Name)
	vrsta, tocke, _ := priblizno(t, d.Priblizno, "/stations/"+naKmu.ID.String())
	if r.Skupina != "kanal Proba" || r.Gdje != "kkm 7+500" || !r.Mjesto || vrsta != "Point" || tocke[0][0] < 18.04 || tocke[0][0] > 18.06 {
		t.Errorf("letva na kkm 7,5: %+v, %v", r, tocke)
	}
	for naziv, gdje := range map[string]string{
		bezStacionaze.Name: "bez stacionaže, nema mjesta na karti",
		bezToka.Name:       "pkm 1+000 · vodotok nije ucrtan",
		daleko.Name:        "kkm 40+000 · izvan km ucrtanih na vodotoku",
		bezKm.Name:         "pkm 2+000 · na vodotoku nisu ucrtani km",
	} {
		if r := redak(t, d.Nedostaje, naziv); r.Gdje != gdje || r.Mjesto {
			t.Errorf("%s: %q, htio %q", naziv, r.Gdje, gdje)
		}
	}
	if r := redak(t, d.Nedostaje, bezToka.Name); r.Skupina != "Bez vodotoka" {
		t.Errorf("letva bez naziva vodotoka: %q", r.Skupina)
	}
}

// Kad se dionice ne učitaju, karta se ipak složi, a greška kaže što fali.
func TestKartaGreskaDionica(t *testing.T) {
	o, v, l := probniIzvori()
	h := NewKartaHandler(o, v, l, func(context.Context) ([]models.Section, error) { return nil, errors.New("baza zaključana") }, nil, nil)
	k, err := h.slozi(context.Background())
	if err == nil || !strings.Contains(err.Error(), "dionice: ") {
		t.Fatalf("greška dionica: %v", err)
	}
	if r := redak(t, k.zaKartu.Nedostaje, "Nasip bez crte"); r.Gdje != "nije ni u jednoj dionici, nema mjesta na karti" {
		t.Errorf("bez dionica: %+v", r)
	}
}
