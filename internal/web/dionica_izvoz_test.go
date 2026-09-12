package web

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Kartica dionice u Excelu nosi sve što i stranica: opis, poddionice s
// letvama i pragovima, nasipe s objektima i naseljima, obranu koja traje,
// djelatnike i napomene.
func TestKnjigaDionice(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cm := func(v int) *int { return &v }
	st := models.Station{ID: uuid.New(), Name: "Batina", Stationing: "rkm 1425+500", ZeroDatum: f(78.94), ZeroDatumNew: f(78.63),
		Prep: models.Threshold{Cm: cm(500)}, Regular: models.Threshold{Cm: cm(600)}, Emergency: models.Threshold{Cm: cm(700)}, State: models.Threshold{Cm: cm(800)},
		Extremes: []models.StationExtreme{{Kind: models.ExtremeMax, LevelCm: cm(769), OnDate: "1965-06-26"}}}
	part := models.SectionPart{Seq: 1, WatercourseName: "Dunav", Bank: "L", KmFrom: f(1421.77), KmTo: f(1433.06), StationingKind: "rkm", Extent: "od državne granice do Zelenog otoka",
		Embankments: []models.PartEmbankment{{Name: "Lijevoobalni nasip Batina – Zmajevac", LengthKm: f(11.3)}},
		Objects:     []models.PartObject{{Name: "CS Zmajevac", StationingKind: "rkm", Stationing: f(1424.2), OnEmbankment: "Lijevoobalni nasip Batina – Zmajevac"}}}
	sec := models.Section{Code: "B.16.1", AreaID: 16, SectorID: "B", AreaName: "Baranja", SectorName: "Sektor B", Description: "rijeka Dunav, l.o.; Batina – Zmajevac", DescriptionCustom: true,
		Parts: []models.SectionPart{part}, Notes: "Ključ ustave kod vodočuvara.",
		Personnel: []models.SectionOfficer{{FullName: "Rukovoditelj Dionice", Title: "dipl. ing.", DutyTitle: "rukovoditelj dionice", Role: "SECTION_LEADER", RoleLabel: "Rukovoditelj dionice", RoleGroup: "Razina 3", Rank: 3, MobilePhone: "099 000 0000", OrgName: "Hrvatske vode"}}}
	pv := PartView{SectionPart: part, Stations: []models.Station{st}, Rows: embankmentRows(part),
		Territories: nil}
	pv.Rows[0].Territories = []models.SectionTerritory{{CountyName: "Osječko-baranjska", MunicipalityName: "Kneževi Vinogradi", MunicipalityType: "OPCINA", SettlementID: cm(1), SettlementName: "Zmajevac"}}
	parts := []PartView{pv}
	napuniLetveBlokove(parts)
	pocetak := time.Now().Add(-48 * time.Hour)
	d := SectionPageData{Section: sec, Parts: parts, Gauge: &st, OpenEpisode: &models.DefenseEpisode{Phase: models.PhaseRegular, StartedAt: pocetak, DeclaredByName: "Voditelj Centra", Basis: models.BasisThreshold}}
	z := ZaglavljeIzvoza{Organizacija: "Hrvatske vode", Odjel: "VGO Osijek", Centar: "COP Osijek · BP 16 Baranja", Sektor: "B", Datum: time.Now()}

	var b bytes.Buffer
	if err := KnjigaDionice(d, z).Zapisi(&b); err != nil {
		t.Fatal(err)
	}
	redci, err := procitajXLSX(b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	var sve []string
	for _, r := range redci {
		sve = append(sve, strings.Join(r, "|"))
	}
	list := strings.Join(sve, "\n")
	for _, want := range []string{"KARTICA ŠTIĆENE DIONICE B.16.1", "rijeka Dunav, l.o.; Batina – Zmajevac", "R — Redovna obrana, na snazi od", "Voditelj Centra",
		"PODDIONICA 1 — Dunav, lijeva obala", "Batina|rkm 1425+500|500\n83,63 m HVRS71", "78,630 HVRS71", "1965", "Lijevoobalni nasip Batina – Zmajevac", "CS Zmajevac", "Općina Kneževi Vinogradi: Zmajevac",
		"Rukovoditelj Dionice, dipl. ing.||rukovoditelj dionice||Hrvatske vode|099 000 0000", "Ključ ustave kod vodočuvara."} {
		if !strings.Contains(list, want) {
			t.Errorf("kartica nema %q\n%s", want, list)
		}
	}
}
