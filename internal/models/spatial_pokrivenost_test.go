package models

import (
	"strings"
	"testing"
)

func TestRazinaSektoraIKoordinatePodrucja(t *testing.T) {
	if !(Sector{Level: 1}).IsLevel1() || (Sector{Level: 2}).IsLevel1() || (Sector{}).IsLevel1() {
		t.Error("IsLevel1 ne prati razinu")
	}
	for _, s := range []struct {
		lat, lon float64
		want     bool
	}{{45.55, 18.69, true}, {0, 18.69, false}, {45.55, 0, false}, {0, 0, false}} {
		if got := (Area{Latitude: s.lat, Longitude: s.lon}).ImaKoordinate(); got != s.want {
			t.Errorf("područje %v,%v: %v", s.lat, s.lon, got)
		}
	}
}

// Popisi čitaju obalu i raspon iz prve poddionice; dionica bez poddionica
// ne smije pasti.
func TestPrvaPoddionicaZaPopise(t *testing.T) {
	var prazna Section
	if prazna.Bank() != "" || prazna.RkmFrom() != nil || prazna.RkmTo() != nil || prazna.FirstPart().Seq != 0 {
		t.Error("dionica bez poddionica")
	}
	s := Section{Parts: []SectionPart{
		{Seq: 1, Bank: "L", KmFrom: broj(1433.06), KmTo: broj(1421.77)},
		{Seq: 2, Bank: "D"},
	}}
	if s.Bank() != "L" || *s.RkmFrom() != 1433.06 || *s.RkmTo() != 1421.77 || s.FirstPart().Seq != 1 {
		t.Errorf("prva poddionica: %+v", s.FirstPart())
	}
}

func TestDuljinaDioniceINasipa(t *testing.T) {
	s := Section{Parts: []SectionPart{
		{KmFrom: broj(10), KmTo: broj(4.5), Embankments: []PartEmbankment{{LengthKm: broj(2.5)}, {}}},
		{LengthKm: broj(3), Embankments: []PartEmbankment{{LengthKm: broj(1.25)}}},
		{KmFrom: broj(1)},
	}}
	if got := s.Length(); got != 8.5 {
		t.Errorf("zbroj poddionica %v, očekivano 8,5", got)
	}
	if got := s.EmbankmentLength(); got != 3.75 {
		t.Errorf("zbroj nasipa %v, očekivano 3,75", got)
	}
	s.LengthKm, s.EmbankmentKm = broj(9), broj(4)
	if s.Length() != 9 || s.EmbankmentLength() != 4 {
		t.Errorf("upisane duljine nisu zadržane: %v, %v", s.Length(), s.EmbankmentLength())
	}
	if got := (SectionPart{KmFrom: broj(0), KmTo: broj(32.49)}).Length(); got != 32.49 {
		t.Errorf("raspon poddionice %v", got)
	}
}

// Ručni opis ima prednost; prazan ili od razmaka znači složeni.
func TestEffectiveDescription(t *testing.T) {
	s := Section{Parts: []SectionPart{{WatercourseName: "Vuka", Bank: "D", Extent: "ušće – Nemetin"}}}
	if got := s.EffectiveDescription(); got != "Vuka, d.o.; ušće – Nemetin" {
		t.Errorf("složeni opis %q", got)
	}
	s.Description = "   "
	if got := s.EffectiveDescription(); got != "Vuka, d.o.; ušće – Nemetin" {
		t.Errorf("opis od razmaka %q", got)
	}
	s.Description = "Ručno upisan opis"
	if got := s.EffectiveDescription(); got != "Ručno upisan opis" {
		t.Errorf("ručni opis %q", got)
	}
}

func TestComposePoddionice(t *testing.T) {
	for _, s := range []struct {
		ime  string
		p    SectionPart
		want string
	}{
		{"šifra kad nema naziva", SectionPart{WatercourseCode: "rijeka-vuka", Bank: "LD"}, "rijeka-vuka, l.o. i d.o."},
		{"nepoznata obala", SectionPart{WatercourseName: "Vuka", Bank: "X"}, "Vuka"},
		{"bez polja ostaje izvorni opis", SectionPart{Description: "iz Privitka"}, "iz Privitka"},
		{"samo duljina", SectionPart{LengthKm: broj(2)}, "(2,000 km)"},
		{"stacionaža i duljina", SectionPart{WatercourseName: "Drava", StationingKind: "rkm", KmFrom: broj(12.45), KmTo: broj(0)},
			"Drava; rkm 12+450 - 0+000; (12,450 km)"},
	} {
		if got := s.p.Compose(); got != s.want {
			t.Errorf("%s: %q, očekivano %q", s.ime, got, s.want)
		}
	}
	for b, want := range map[string]string{"L": "l.o.", "D": "d.o.", "LD": "l.o. i d.o.", "X": "", "": ""} {
		if got := BankShort(b); got != want {
			t.Errorf("BankShort(%q) = %q, očekivano %q", b, got, want)
		}
	}
}

func TestSkupoviPoddionicaBezPonavljanja(t *testing.T) {
	naselje := 7
	s := Section{Parts: []SectionPart{
		{
			StationIDs:    []string{"bat", "", "vuk"},
			Objects:       []PartObject{{StructureID: "cs-draz"}, {Name: "most"}},
			Embankments:   []PartEmbankment{{StructureID: "nasip-1"}},
			Territories:   []PartTerritory{{CountyID: 14, MunicipalityID: 3}, {CountyID: 14, MunicipalityID: 3, SettlementID: &naselje}},
			ProtectedText: " Batina, Draž ",
			Unaligned:     false,
		},
		{
			StationIDs:    []string{"vuk", "bez"},
			Objects:       []PartObject{{StructureID: "nasip-1"}, {StructureID: "cs-budzak"}},
			Embankments:   []PartEmbankment{{StructureID: "cs-draz"}, {}},
			Territories:   []PartTerritory{{CountyID: 14, MunicipalityID: 3}},
			ProtectedText: "Batina, Draž",
		},
		{ProtectedText: "  ", Unaligned: true},
	}}
	if got := strings.Join(s.AllStationIDs(), ","); got != "bat,vuk,bez" {
		t.Errorf("vodomjeri %q", got)
	}
	if got := strings.Join(s.AllStructureIDs(), ","); got != "cs-draz,nasip-1,cs-budzak" {
		t.Errorf("objekti %q", got)
	}
	ter := s.AllTerritories()
	if len(ter) != 2 || ter[0].Key() != "14/3" || ter[1].Key() != "14/3/7" {
		t.Errorf("teritoriji %+v", ter)
	}
	if got := s.ProtectedSummary(); got != "Batina, Draž" {
		t.Errorf("ugroženo %q", got)
	}
	if s.ObjectCount() != 4 || s.EmbankmentCount() != 3 || s.GaugeCount() != 3 {
		t.Errorf("brojevi: objekti %d, nasipi %d, vodomjeri %d", s.ObjectCount(), s.EmbankmentCount(), s.GaugeCount())
	}
	if !s.Unaligned() {
		t.Error("treća poddionica je neporavnana")
	}
	var prazna Section
	if prazna.AllStationIDs() != nil || prazna.AllStructureIDs() != nil || prazna.AllTerritories() != nil ||
		prazna.ProtectedSummary() != "" || prazna.GaugeCount() != 0 || prazna.Unaligned() {
		t.Error("dionica bez poddionica")
	}
}

func TestStacionazaZaPrikaz(t *testing.T) {
	for _, s := range []struct {
		km   float64
		want string
	}{
		{0, "0+000"}, {12.45, "12+450"}, {1433.06, "1433+060"}, {12.9996, "13+000"}, {1.825, "1+825"},
	} {
		if got := fmtStationing(s.km); got != s.want {
			t.Errorf("fmtStationing(%v) = %q, očekivano %q", s.km, got, s.want)
		}
	}
	if got := FormatKm(36.9); got != "36,900" {
		t.Errorf("FormatKm %q", got)
	}
	if got := (SectionPart{StationingKind: "pkm", KmFrom: broj(0), KmTo: broj(32.49)}).RangeLabel(); got != "pkm 0+000 – 32+490" {
		t.Errorf("raspon %q", got)
	}
	if got := (SectionPart{KmFrom: broj(0), KmTo: broj(32.49)}).RangeLabel(); got != "0+000 – 32+490" {
		t.Errorf("raspon bez vrste %q", got)
	}
	if got := (SectionPart{KmFrom: broj(1)}).RangeLabel(); got != "" {
		t.Errorf("nepotpun raspon %q", got)
	}
	for _, s := range []struct {
		o    PartObject
		want string
	}{
		{PartObject{StationingText: "rkm 1+825", Stationing: broj(9)}, "rkm 1+825"},
		{PartObject{StationingKind: "rkm", Stationing: broj(1.825)}, "rkm 1+825"},
		{PartObject{Stationing: broj(1.825)}, "1+825"},
		{PartObject{}, ""},
	} {
		if got := s.o.StationingLabel(); got != s.want {
			t.Errorf("StationingLabel(%+v) = %q, očekivano %q", s.o, got, s.want)
		}
	}
}

// Upute (pravilnik, prognoza, pravila upravljanja) nisu mjerila.
func TestIsGauge(t *testing.T) {
	for _, s := range []struct {
		naziv string
		want  bool
	}{
		{"Vukovar , rkm 1.333,45", true},
		{"Kota na mostu Nemetin", true},
		{"", false},
		{"   ", false},
		{"R: +350", false},
		{"P = 120 m³/s", false},
		{"P=120", false},
		{"Prema prognozi DHMZ-a", false},
		{"V na brani Borovik", false},
		{"Po pravilniku retencije", false},
		{"upravljanje ustavom", false},
		{"  Prema pravilniku", false},
	} {
		if got := (GaugeItem{StationName: s.naziv}).IsGauge(); got != s.want {
			t.Errorf("IsGauge(%q) = %v, očekivano %v", s.naziv, got, s.want)
		}
	}
}

// Zaduženi se dijele po razini redom kojim stoje; razina dobiva i značenje
// iz naziva organizacije, a razina bez značenja samo svoj naziv.
func TestPersonnelByLevel(t *testing.T) {
	postaviNazive(t, DefaultTerms())
	s := Section{Personnel: []SectionOfficer{
		{FullName: "Ana Anić", RoleGroup: "Razina 1", Rank: 1},
		{FullName: "Ivo Ivić", RoleGroup: "Razina 2", Rank: 2},
		{FullName: "Eva Ević", RoleGroup: "Razina 2", Rank: 2},
		{FullName: "Pero Perić", RoleGroup: "Razina 3", Rank: 3},
		{FullName: "Lea Lekić", RoleGroup: "Razina 4", Rank: 4},
		{FullName: "Mia Mikić", RoleGroup: "Teren", Rank: 5},
		{FullName: "Tin Tinić", RoleGroup: "", Rank: 3},
	}}
	got := s.PersonnelByLevel()
	want := []struct {
		label   string
		clanova int
	}{
		{"Razina 1 — Hrvatske vode", 1},
		{"Razina 2 — sektor", 2},
		{"Razina 3 — branjeno područje", 1},
		{"Razina 4 — dionica", 1},
		{"Teren", 1},
		{"", 1},
	}
	if len(got) != len(want) {
		t.Fatalf("%d skupina, očekivano %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Label != w.label || len(got[i].Members) != w.clanova {
			t.Errorf("skupina %d: %q s %d članova, očekivano %q s %d", i, got[i].Label, len(got[i].Members), w.label, w.clanova)
		}
	}
	if got[1].Members[1].FullName != "Eva Ević" || got[5].Rank != 3 {
		t.Error("članovi nisu zadržali redoslijed")
	}
	if (Section{}).PersonnelByLevel() != nil {
		t.Error("dionica bez zaduženih")
	}
}
