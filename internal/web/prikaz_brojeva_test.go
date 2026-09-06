package web

import (
	"bytes"
	"html/template"
	"io/fs"
	"strings"
	"testing"

	webassets "gocop/web"

	"gocop/internal/models"
)

// Iscrtava pravi predložak s pravim pomoćnicima i gleda što je ispalo. Testovi
// nad samim funkcijama ne bi uhvatili predložak koji je ostao na starom
// pomoćniku, a upravo je to bila greška na stranici grada.
func iscrtaj(t *testing.T, stranica string, data any) string {
	t.Helper()
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("base.html").Funcs(templateFuncs()).
		ParseFS(templatesFS, "base.html", stranica)
	if err != nil {
		t.Fatalf("predložak %s se ne učitava: %v", stranica, err)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, stranica, data); err != nil {
		t.Fatalf("predložak %s se ne iscrtava: %v", stranica, err)
	}
	return buf.String()
}

func TestRegistarLetviPiseBrojeveHrvatski(t *testing.T) {
	kota, kotaNova := 82.06, 1234.5
	html := iscrtaj(t, "stations.html", StationsPageData{
		CurrentUser: &models.User{FullName: "Provjera"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Stations: []models.Station{
			{Code: "VP1", Name: "Belišće", ZeroDatum: &kota},
			{Code: "VP2", Name: "Osijek", ZeroDatum: &kotaNova},
		},
		TotalStations: 1234,
		Pager:         Pager{Page: 1, PerPage: 20, Total: 1234, From: 1, To: 20, Pages: 62},
	})

	for _, want := range []string{"82,06", "1.234,5", "od 1.234"} {
		if !strings.Contains(html, want) {
			t.Errorf("na stranici nema %q", want)
		}
	}
	for _, notWant := range []string{"82.06", "1234.5", "od 1234<"} {
		if strings.Contains(html, notWant) {
			t.Errorf("na stranici je ostalo %q", notWant)
		}
	}
}

func TestRegistarVodotokaPiseBrojeveHrvatski(t *testing.T) {
	duljina, porjecje, protok := 1234.5, 96000.0, 620.0
	html := iscrtaj(t, "watercourses.html", WatercoursesPageData{
		CurrentUser: &models.User{FullName: "Provjera"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Watercourses: []models.Watercourse{
			{Name: "Dunav", LengthKm: &duljina, BasinKm2: &porjecje, AvgFlowM3S: &protok},
		},
	})
	for _, want := range []string{"1.234 km", "96.000 km²", "620 m³/s"} {
		if !strings.Contains(html, want) {
			t.Errorf("na stranici nema %q", want)
		}
	}
	if strings.Contains(html, "96000") {
		t.Error("porječje je ostalo bez razdjelnika tisućica")
	}
}

// U polju obrasca zarez da, razdjelnik tisućica ne — inače se vrijednost teško
// uređuje, a i čitanje bi je moralo raspetljavati bez potrebe.
func TestObrazacLetvePiseZarezBezTisucica(t *testing.T) {
	kota := 1234.56
	html := iscrtaj(t, "station_form.html", StationPageData{
		CurrentUser: &models.User{FullName: "Provjera"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     models.Station{Name: "Belišće", ZeroDatum: &kota},
		IsEdit:      true,
	})
	if !strings.Contains(html, `value="1234,56"`) {
		t.Error("polje ne sadrži 1234,56")
	}
	if strings.Contains(html, `value="1.234,56"`) {
		t.Error("u polju obrasca je razdjelnik tisućica")
	}
}

// Obrazac dionice: naslovi stupaca stoje jednom, u zaglavlju, a ne uz svaki
// redak — i uz dokumentaciju stoji gumb koji je prepisuje u veze na registar.
func TestObrazacDioniceImaZaglavljeIPrijepis(t *testing.T) {
	html := iscrtaj(t, "section_form.html", SectionPageData{
		CurrentUser: &models.User{FullName: "Provjera"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Section:     models.Section{Code: "B.34.1", AreaID: 34, SectorID: "B"},
		IsEdit:      true,
	})
	if n := strings.Count(html, `class="rows-head"`); n != 2 {
		t.Errorf("zaglavlja stupaca ima %d, očekivano 2 (nasipi i objekti)", n)
	}
	if strings.Count(html, "Prepiši iz dokumentacije") != 2 {
		t.Error("nema gumba za prijepis u oba bloka")
	}
	// Naslov stupca ne smije se ponavljati uz svaki redak: u predlošku retka
	// ostaju samo polja, a naslov nosi data-label, koji se vidi tek na uskom
	// zaslonu, i aria-label za čitače zaslona.
	redak := izmedju(html, `<template id="tpl-emb">`, "</template>")
	if redak == "" {
		t.Fatal("predložak retka nasipa nije nađen")
	}
	if strings.Contains(redak, "<label") {
		t.Error("redak nasipa još nosi vlastite naslove stupaca")
	}
	for _, treba := range []string{`data-label="Uz vodu od"`, `aria-label="Uz vodu od"`} {
		if !strings.Contains(redak, treba) {
			t.Errorf("redak nasipa nema %s", treba)
		}
	}
}

func izmedju(s, od, do string) string {
	i := strings.Index(s, od)
	if i < 0 {
		return ""
	}
	rest := s[i+len(od):]
	j := strings.Index(rest, do)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// Naziv sektora u registru već glasi "Sektor B — Dunav i donja Drava", pa ga
// stranica ne smije još jednom uvoditi svojim "Sektor B —".
func TestStranicaDioniceNePonavljaSektor(t *testing.T) {
	html := iscrtaj(t, "section_detail.html", SectionPageData{
		CurrentUser: &models.User{FullName: "Provjera"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Section: models.Section{
			Code: "B.34.1", AreaID: 34, SectorID: "B",
			SectorName: "Sektor B — Dunav i donja Drava",
		},
	})
	if strings.Count(html, "Sektor B — Dunav i donja Drava") != 1 {
		t.Error("naziv sektora ne stoji točno jednom")
	}
	if strings.Contains(html, "Sektor B — Sektor B") {
		t.Error("sektor je napisan dvaput")
	}
}

// Na kartici dionice traži se jedno ime — najčešće rukovoditelj dionice — pa
// zaduženi moraju biti razdvojeni po razinama, a ne u jednom popisu.
func TestZaduzeniSuRazdvojeniPoRazinama(t *testing.T) {
	html := iscrtaj(t, "section_detail.html", SectionPageData{
		CurrentUser: &models.User{FullName: "Provjera"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Section: models.Section{
			Code: "B.34.1", AreaID: 34, SectorID: "B",
			Personnel: []models.SectionOfficer{
				{FullName: "Rukovoditelj Sektora", Rank: 2, RoleGroup: "Razina 2", RoleLabel: "Rukovoditelj sektora"},
				{FullName: "Rukovoditelj Dionice", Rank: 4, RoleGroup: "Razina 4", RoleLabel: "Rukovoditelj dionice"},
				{FullName: "Zamjenik Dionice", Rank: 4, RoleGroup: "Razina 4", RoleLabel: "Zamjenik rukovoditelja dionice"},
				{FullName: "Vodočuvar Prvi", Rank: 5, RoleGroup: "Teren", RoleLabel: "Vodočuvar"},
			},
		},
	})
	for _, naslov := range []string{"Razina 2 — sektor", "Razina 4 — dionica", "Teren"} {
		if !strings.Contains(html, naslov) {
			t.Errorf("nema naslova skupine %q", naslov)
		}
	}
	// razina dionice mora doći prije terena, a svaka skupina jednom
	if strings.Index(html, "Razina 4 — dionica") > strings.Index(html, "Vodočuvar Prvi") {
		t.Error("teren stoji prije razine dionice")
	}
	if n := strings.Count(html, "Razina 4 — dionica"); n != 1 {
		t.Errorf("naslov razine 4 pojavljuje se %d puta, očekivano jednom za obje osobe", n)
	}
}

// Kartica dionice slaže se kao Privitak: vodomjer u jednom retku, a nasip je
// nosivi red s objektima koji na njemu leže — ne pet odvojenih popisa.
func TestKarticaDioniceSlazeObjektePoNasipima(t *testing.T) {
	kota := 80.45
	rkm := func(v float64) *float64 { return &v }
	part := models.SectionPart{
		Seq: 1, Bank: "D",
		Embankments: []models.PartEmbankment{
			{Name: "Nasip za zaštitu Batine", WaterKind: "rkm", WaterFrom: rkm(1425.77), WaterTo: rkm(1423.77), LengthKm: rkm(2.005)},
		},
		Objects: []models.PartObject{
			{Name: "vodokaz Batina", StationingKind: "rkm", StationingText: "rkm 1424+850"},
		},
	}
	html := iscrtaj(t, "section_detail.html", SectionPageData{
		CurrentUser: &models.User{FullName: "Provjera"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Section:     models.Section{Code: "B.34.1", AreaID: 34, SectorID: "B", Parts: []models.SectionPart{part}},
		Parts: []PartView{{
			SectionPart: part,
			Rows:        embankmentRows(part),
			Stations:    []models.Station{{Name: "Batina", Stationing: "rkm 1424+850", ZeroDatum: &kota}},
		}},
	})
	for _, want := range []string{
		`class="gauge-row"`, "Batina", "80,45 m",
		`part-table"`, "Nasip za zaštitu Batine", "2,005 km", "vodokaz Batina",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("na kartici nema %q", want)
		}
	}
	// objekt stoji u istom retku tablice kao njegov nasip
	red := izmedju(html, "Nasip za zaštitu Batine", "</tr>")
	if !strings.Contains(red, "vodokaz Batina") {
		t.Error("vodokaz Batina nije u retku svog nasipa")
	}
	if strings.Contains(html, "Na nasipu</th>") {
		t.Error("stupac „Na nasipu\" više ne treba — nasip je sam redak")
	}
}
