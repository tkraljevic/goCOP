package web

import (
	"bytes"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// B.34.14, poddionica 2: tri nasipa i pet naselja upisanih za cijelu
// poddionicu, bez nasipa.
func poddionicaB3414(kvacice ...bool) (models.SectionPart, map[string]models.SectionTerritory) {
	imena := []string{"Donji Miholjac", "Blanje", "Ivanovo", "Viljevo", "Martinci Miholjački"}
	part := models.SectionPart{Embankments: []models.PartEmbankment{
		{Name: "Nasip Zabara-Hobođ"},
		{Name: "Sekundarni nasip Konopljište"},
		{Name: "Sekundarni nasip Karaula"},
	}}
	for i, k := range kvacice {
		part.Embankments[i].UgrozenoPoddionice = k
	}
	poKljucu := map[string]models.SectionTerritory{}
	for i, ime := range imena {
		t, s := naselje(100+i, ime, "")
		part.Territories = append(part.Territories, t)
		poKljucu[t.Key()] = s
	}
	return part, poKljucu
}

// slozi radi isto što i napuniDionicu za jednu poddionicu
func slozi(part models.SectionPart, poKljucu map[string]models.SectionTerritory) ([]EmbankmentRow, []models.SectionTerritory) {
	rows := embankmentRows(part)
	return rasporediUgrozeno(rows, part, poKljucu)
}

func naseljaRetka(r EmbankmentRow) []string {
	var out []string
	for _, t := range r.Territories {
		out = append(out, t.SettlementName)
	}
	return out
}

// Bez ijedne kvačice sve je točno kao prije: ista funkcija, isti retci.
func TestUgrozenoPoddioniceBezKvaciceKaoPrije(t *testing.T) {
	t1, s1 := naselje(1, "Draž", "Nasip Državna granica -Draž")
	t2, s2 := naselje(2, "Gajić", "")
	t3, s3 := naselje(3, "Topolje", "Nasip kojeg nema")
	poKljucu := map[string]models.SectionTerritory{t1.Key(): s1, t2.Key(): s2, t3.Key(): s3}
	nasipi := []models.PartEmbankment{{Name: "Nasip Državna granica -Draž"}, {Name: "Nasip Gomboš"}}
	izvan := []models.PartObject{{Name: "Ustava Draž"}}
	b3414, b3414Kljuc := poddionicaB3414()
	slucajevi := []struct {
		opis     string
		part     models.SectionPart
		poKljucu map[string]models.SectionTerritory
	}{
		{"bez nasipa i objekata", models.SectionPart{Territories: []models.PartTerritory{t2}}, poKljucu},
		{"bez nasipa, s objektom", models.SectionPart{Objects: izvan, Territories: []models.PartTerritory{t2}}, poKljucu},
		{"s nasipima, bez objekata izvan", models.SectionPart{Embankments: nasipi, Territories: []models.PartTerritory{t1, t2, t3}}, poKljucu},
		{"s nasipima i objektom izvan", models.SectionPart{Embankments: nasipi, Objects: izvan, Territories: []models.PartTerritory{t1, t2, t3}}, poKljucu},
		{"samo naselja uz nasip", models.SectionPart{Embankments: nasipi, Territories: []models.PartTerritory{t1}}, poKljucu},
		{"bez ugroženog", models.SectionPart{Embankments: nasipi}, poKljucu},
		{"B.34.14 bez kvačice", b3414, b3414Kljuc},
	}
	for _, c := range slucajevi {
		stari := embankmentRows(c.part)
		stariRetci, staroIznad := ugrozenoBezNasipa(stari, razvrstajUgrozeno(stari, c.part, c.poKljucu))
		retci, iznad := slozi(c.part, c.poKljucu)
		if !reflect.DeepEqual(retci, stariRetci) || !reflect.DeepEqual(iznad, staroIznad) {
			t.Errorf("%s: drukčije nego prije\n%+v %v\n%+v %v", c.opis, retci, iznad, stariRetci, staroIznad)
		}
	}
}

// Kvačica na dva od tri nasipa: ta dva pokazuju svih pet naselja, treći
// nijedno, a redak „Bez nasipa” samo radi naselja se ne dodaje.
func TestUgrozenoPoddioniceNekiNasipi(t *testing.T) {
	part, poKljucu := poddionicaB3414(true, false, true)
	retci, iznad := slozi(part, poKljucu)
	if len(retci) != 3 || len(iznad) != 0 {
		t.Fatalf("%d redaka, %d iznad tablice; očekivano 3 nasipa i ništa iznad", len(retci), len(iznad))
	}
	for i, want := range []int{5, 0, 5} {
		if got := len(retci[i].Territories); got != want {
			t.Errorf("nasip %d ima %d naselja, očekivano %d", i+1, got, want)
		}
	}
	if got := naseljaRetka(retci[0]); got[0] != "Donji Miholjac" || got[4] != "Martinci Miholjački" {
		t.Errorf("redoslijed naselja nije kao u upisu: %v", got)
	}
}

// Kvačica na svim nasipima: svaki pokazuje svih pet naselja.
func TestUgrozenoPoddioniceSviNasipi(t *testing.T) {
	part, poKljucu := poddionicaB3414(true, true, true)
	retci, iznad := slozi(part, poKljucu)
	if len(retci) != 3 || iznad != nil {
		t.Fatalf("%d redaka, iznad %v", len(retci), iznad)
	}
	for i, r := range retci {
		if r.Embankment == nil || len(r.Territories) != 5 {
			t.Errorf("redak %d: %v", i+1, naseljaRetka(r))
		}
	}
}

// Nasip s vlastitim naseljima i kvačicom: najprije svoja, pa naselja
// poddionice. Naselje koje je i uz nasip i uz poddionicu piše se jednom.
func TestUgrozenoPoddioniceUzVlastitaNaselja(t *testing.T) {
	part, poKljucu := poddionicaB3414(true)
	svoje, s := naselje(200, "Podravska Moslavina", "Nasip Zabara-Hobođ")
	isto, _ := naselje(101, "Blanje", "Nasip Zabara-Hobođ")
	part.Territories = append([]models.PartTerritory{svoje, isto}, part.Territories...)
	poKljucu[svoje.Key()] = s

	retci, _ := slozi(part, poKljucu)
	got := naseljaRetka(retci[0])
	want := []string{"Podravska Moslavina", "Blanje", "Donji Miholjac", "Ivanovo", "Viljevo", "Martinci Miholjački"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("nasip s kvačicom: %v, očekivano %v", got, want)
	}
	if got := naseljaRetka(retci[1]); len(got) != 0 {
		t.Errorf("nasip bez kvačice dobio je naselja poddionice: %v", got)
	}
}

// Mješovito kao B.34.12: naselje uz nasip, naselje uz cijelu poddionicu i
// objekt izvan nasipa. Redak „Bez nasipa” postoji zbog objekta i dalje
// pokazuje naselja bez nasipa; nasip s kvačicom ih pokazuje uz svoja.
func TestUgrozenoPoddioniceSObjektomIzvanNasipa(t *testing.T) {
	t1, s1 := naselje(1, "Draž", "Nasip Gomboš")
	t2, s2 := naselje(2, "Gajić", "")
	part := models.SectionPart{
		Embankments: []models.PartEmbankment{{Name: "Nasip Gomboš", UgrozenoPoddionice: true}, {Name: "Nasip za zaštitu Batine"}},
		Objects:     []models.PartObject{{Name: "Ustava Draž"}},
		Territories: []models.PartTerritory{t1, t2},
	}
	retci, iznad := slozi(part, map[string]models.SectionTerritory{t1.Key(): s1, t2.Key(): s2})
	if len(retci) != 3 || iznad != nil {
		t.Fatalf("%d redaka, iznad %v", len(retci), iznad)
	}
	for i, want := range [][]string{{"Draž", "Gajić"}, nil, {"Gajić"}} {
		if got := naseljaRetka(retci[i]); !reflect.DeepEqual(got, want) {
			t.Errorf("redak %d: %v, očekivano %v", i+1, got, want)
		}
	}
	if retci[2].Embankment != nil || len(retci[2].Objects) != 1 {
		t.Error("zadnji redak mora biti „Bez nasipa” s objektom")
	}
}

// Naselje upisano uz nasip kojeg više nema nije naselje cijele poddionice:
// nasip s kvačicom ga ne preuzima, nego ono ostaje vidljivo u retku „Bez
// nasipa”. Kad taj redak postoji zbog objekata, pokazuje sva naselja bez
// svog nasipa, kao i prije.
func TestUgrozenoPoddioniceNaseljeBezSvogNasipa(t *testing.T) {
	part, poKljucu := poddionicaB3414(true)
	ostalo, s := naselje(300, "Golinci", "Nasip kojeg nema")
	part.Territories = append(part.Territories, ostalo)
	poKljucu[ostalo.Key()] = s

	retci, iznad := slozi(part, poKljucu)
	if len(retci) != 4 || iznad != nil {
		t.Fatalf("%d redaka, iznad %v; očekivano 3 nasipa i „Bez nasipa”", len(retci), iznad)
	}
	if got := naseljaRetka(retci[0]); len(got) != 5 || got[4] != "Martinci Miholjački" {
		t.Errorf("nasip s kvačicom: %v", got)
	}
	if got := naseljaRetka(retci[3]); retci[3].Embankment != nil || !reflect.DeepEqual(got, []string{"Golinci"}) {
		t.Errorf("redak „Bez nasipa”: %v", got)
	}

	part.Objects = []models.PartObject{{Name: "Crpna stanica Golinci"}}
	retci, _ = slozi(part, poKljucu)
	if got := naseljaRetka(retci[len(retci)-1]); len(got) != 6 || got[5] != "Golinci" {
		t.Errorf("redak „Bez nasipa” s objektom: %v", got)
	}
}

// Kvačica bez naselja poddionice ne mijenja ništa; poddionica bez nasipa
// kvačice ni nema, pa ostaje kao prije.
func TestUgrozenoPoddioniceBezNaseljaIBezNasipa(t *testing.T) {
	t1, s1 := naselje(1, "Draž", "Nasip Gomboš")
	part := models.SectionPart{Embankments: []models.PartEmbankment{{Name: "Nasip Gomboš", UgrozenoPoddionice: true}}, Territories: []models.PartTerritory{t1}}
	retci, iznad := slozi(part, map[string]models.SectionTerritory{t1.Key(): s1})
	if len(retci) != 1 || len(retci[0].Territories) != 1 || iznad != nil {
		t.Errorf("nasip sa svojim naseljem: %d redaka, %v, iznad %v", len(retci), naseljaRetka(retci[0]), iznad)
	}

	t2, s2 := naselje(2, "Gajić", "")
	bez := models.SectionPart{Territories: []models.PartTerritory{t2}}
	retci, iznad = slozi(bez, map[string]models.SectionTerritory{t2.Key(): s2})
	if len(retci) != 1 || retci[0].Embankment != nil || len(retci[0].Territories) != 1 || iznad != nil {
		t.Errorf("poddionica bez nasipa: %+v, iznad %v", retci, iznad)
	}
}

// Kartica: naselja poddionice stoje uz nasip s kvačicom i broje se u
// njegovu „Ugroženo područje”; nasip bez kvačice ima nula.
func TestKarticaUgrozenoPoddioniceUzNasip(t *testing.T) {
	part, poKljucu := poddionicaB3414(true, false, false)
	retci, iznad := slozi(part, poKljucu)
	html := iscrtaj(t, "section_detail.html", SectionPageData{
		CurrentUser: &models.User{FullName: "Pero Perić"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Section: models.Section{Code: "B.34.14", AreaID: 34, SectorID: "B"},
		Parts:   []PartView{{SectionPart: part, Rows: retci, Territories: iznad}}})
	karte := strings.Split(html, `class="nasip-karta"`)
	if len(karte) != 4 {
		t.Fatalf("na kartici %d nasipa, očekivano 3", len(karte)-1)
	}
	if strings.Contains(karte[0], "Blanje") || strings.Contains(html, "Bez nasipa</span>") {
		t.Error("naselja i dalje stoje izvan nasipa")
	}
	prvi := karte[1]
	if !strings.Contains(prvi, "Blanje") || !strings.Contains(prvi, `Ugroženo područje <span class="reg-chip">5</span>`) {
		t.Error("nasip s kvačicom ne pokazuje naselja poddionice")
	}
	if strings.Contains(karte[2], "Blanje") || !strings.Contains(karte[2], `Ugroženo područje <span class="reg-chip">0</span>`) {
		t.Error("nasip bez kvačice pokazuje naselja poddionice")
	}
}

// Izvoz u Excel slaže naselja iz istih redaka kao kartica.
func TestKnjigaDioniceUgrozenoPoddioniceUzNasip(t *testing.T) {
	part, poKljucu := poddionicaB3414(false, true, false)
	part.Seq = 1
	for k, s := range poKljucu {
		s.CountyName, s.MunicipalityName, s.MunicipalityType = "Osječko-baranjska", "Donji Miholjac", "GRAD"
		poKljucu[k] = s
	}
	retci, iznad := slozi(part, poKljucu)
	d := SectionPageData{Section: models.Section{Code: "B.34.14", AreaID: 34, SectorID: "B"},
		Parts: []PartView{{SectionPart: part, Rows: retci, Territories: iznad}}}
	list := izvezenaKartica(t, d)
	i := strings.Index(list, "Sekundarni nasip Konopljište")
	j := strings.Index(list, "Sekundarni nasip Karaula")
	if i < 0 || j < 0 || !strings.Contains(list[i:j], "Blanje") {
		t.Errorf("naselja poddionice nisu uz nasip s kvačicom:\n%s", list)
	}
	if strings.Contains(list[:i], "Blanje") || strings.Contains(list[j:], "Blanje") {
		t.Errorf("naselja poddionice stoje i uz nasipe bez kvačice:\n%s", list)
	}
}

// Obrazac: svaki redak nasipa ima kvačicu, obrazac je puni iz zapisa i šalje
// pri spremanju, a rukovatelj je čuva.
func TestObrazacUgrozenoPoddioniceSeCuva(t *testing.T) {
	html := iscrtaj(t, "section_form.html", SectionPageData{
		CurrentUser: &models.User{FullName: "Pero Perić"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Section: models.Section{Code: "B.34.14", AreaID: 34, SectorID: "B"}, IsEdit: true,
	})
	red := izmedju(html, `<template id="tpl-emb">`, "</template>")
	if !strings.Contains(red, `<input type="checkbox" data-field="ugrozeno_poddionice" onchange="renderChips(partOf(this))">`) ||
		!strings.Contains(red, "Vrijedi i ugroženo područje cijele poddionice") {
		t.Error("redak nasipa nema kvačice za ugroženo područje poddionice")
	}
	for _, js := range []string{
		"row.querySelector('[data-field=ugrozeno_poddionice]').checked = !!e.ugrozeno_poddionice",
		"if (row.querySelector('[data-field=ugrozeno_poddionice]').checked) e.ugrozeno_poddionice = true",
		"'Cijela poddionica' + (uz.length ? ' — vrijedi i uz: ' + uz.join(', ') : '')",
	} {
		if !strings.Contains(html, js) {
			t.Errorf("obrazac ne čita ili ne šalje kvačicu: %s", js)
		}
	}

	forma := url.Values{"code": {"B.34.14"}, "area_id": {"34"},
		"parts_json": {`[{"seq":1,"embankments":[{"name":"Nasip Zabara-Hobođ","ugrozeno_poddionice":true},{"name":"Sekundarni nasip Karaula"}]}]`}}
	r := httptest.NewRequest("POST", "/sections/B.34.14", strings.NewReader(forma.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sec, err := sectionFromForm(r)
	if err != nil {
		t.Fatal(err)
	}
	e := sec.Parts[0].Embankments
	if len(e) != 2 || !e[0].UgrozenoPoddionice || e[1].UgrozenoPoddionice {
		t.Errorf("kvačica se izgubila pri čitanju obrasca: %+v", e)
	}
}

func izvezenaKartica(t *testing.T, d SectionPageData) string {
	t.Helper()
	var b bytes.Buffer
	if err := KnjigaDionice(d, ZaglavljeIzvoza{Datum: time.Now()}).Zapisi(&b); err != nil {
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
	return strings.Join(sve, "\n")
}
