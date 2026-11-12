package web

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Kartica dionice po crtežu 17b: vode u podnaslovu, Podaci, letve i nasipi
// kao retci s poveznicama, obrana kao ploča i zaduženi kao jedan redak.
func TestKarticaDionicePoCrtezu(t *testing.T) {
	duljina, nasip := 2.971, 1.85
	branjin := models.Station{ID: uuid.MustParse("9f1c0b2e-0000-7000-8000-000000000001"), Name: "Branjin Vrh",
		Watercourse: "Karašica", Stationing: "pkm 23,37",
		Prep: models.Threshold{Cm: cm(120)}, Regular: models.Threshold{Cm: cm(200)}, Emergency: models.Threshold{Cm: cm(250)}}
	popovac := models.Station{ID: uuid.MustParse("9f1c0b2e-0000-7000-8000-000000000002"), Name: "Popovac"}
	nasipi := []models.PartEmbankment{
		{StructureID: "n1", Name: "Usporni nasip uz l.o. K. Travnik", LengthKm: &nasip},
		{StructureID: "n2", Name: "Usporni nasip uz d.o. K. Travnik", LengthKm: &nasip},
	}
	opcina := models.PartTerritory{CountyID: 14, MunicipalityID: 1}
	dijelovi := []models.SectionPart{
		{Seq: 1, WatercourseName: "kanal Travnik", LengthKm: &duljina, Embankments: nasipi, Territories: []models.PartTerritory{opcina}},
		{Seq: 2, WatercourseName: "kanal Hatvan", Embankments: []models.PartEmbankment{
			{StructureID: "n3", Name: "Usporni nasip uz d.o. k. Hatvan"}, {Name: "Nasip bez registra", LengthKm: &nasip}}},
		{Seq: 3, WatercourseName: "potok Borza", Embankments: []models.PartEmbankment{{StructureID: "n1", Name: "Usporni nasip uz l.o. K. Travnik", LengthKm: &nasip}, {StructureID: "n5", Name: "Usporni nasip uz d.o. p. Borza"}}},
	}
	sec := models.Section{Code: "B.16.1", AreaID: 16, AreaName: "Mali sliv Baranja", SectorID: "B", SectorName: "Sektor B — Dunav i donja Drava",
		Parts: dijelovi, UpdatedAt: "2026-11-04T09:00:00Z",
		Personnel: []models.SectionOfficer{
			{FullName: "Ana Anić", Rank: 2, RoleGroup: "Razina 2"},
			{FullName: "Ivo Ivić", Rank: 4, RoleGroup: "Razina 4"},
		}}
	pogledi := []PartView{
		{SectionPart: dijelovi[0], Stations: []models.Station{branjin}},
		{SectionPart: dijelovi[1], Stations: []models.Station{branjin}},
		{SectionPart: dijelovi[2], Stations: []models.Station{popovac}},
	}
	terr := map[string]models.SectionTerritory{opcina.Key(): {CountyName: "Osječko-baranjska županija", MunicipalityName: "Popovac"}}
	d := SectionPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		CanEdit:     true,
		Section:     sec,
		Parts:       pogledi,
	}
	d.Sazetak = sazetakDionice(d.Section, d.Parts)
	d.Kartica = karticaDionice(d.Section, d.Parts, terr)

	html := iscrtaj(t, "section_detail.html", d)
	for _, want := range []string{
		`class="povratna-poveznica">← Dionice`,
		"BP 16 Mali sliv Baranja · kanal Travnik, kanal Hatvan i potok Borza",
		`href="/sections/B.16.1/dionica.xlsx"`, `href="/sections/B.16.1/edit"`,
		`class="dio-plocice"`, "Poddionice (3)",
		"izmijenjen 4. 11. 2026.", "<dd>B · Dunav i donja Drava</dd>",
		"Osječko-baranjska županija · Popovac",
		"Karašica pkm 23,37 · P 120 · R 200 · I 250 cm · poddionice 1–2",
		`href="/stations/` + popovac.ID.String() + `"`, "poddionica 3",
		`href="/structures/n3"`, "nije vezan uz registar", "i još 1", `href="/structures/n5"`,
		"Nema proglašene obrane na dionici",
		"2 osobe na 2 razine", "Razina 4 — dionica",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("na kartici nema %q", want)
		}
	}
	// Isti nasip uz dvije poddionice stoji jednom, sa zbrojem duljina.
	if n := strings.Count(html, `href="/structures/n1"`); n != 1 {
		t.Errorf("nasip n1 je u desnom stupcu %d puta, a ne jednom", n)
	}
	if len(d.Kartica.Nasipi) != 5 || d.Kartica.Nasipi[0].Duljina != 2*nasip {
		t.Errorf("nasipi nisu spojeni: %+v", d.Kartica.Nasipi)
	}
	// Duljina nije upisana pa je zbroj poddionica; nasipi isto.
	for _, want := range []string{"km, zbroj poddionica", "km, zbroj nasipa"} {
		if !strings.Contains(html, want) {
			t.Errorf("u Podacima nema %q", want)
		}
	}

	// Upisana duljina nije zbroj.
	upisana := 19.565
	d.Section.LengthKm = &upisana
	if html = iscrtaj(t, "section_detail.html", d); strings.Contains(html, "zbroj poddionica") {
		t.Error("upisana duljina dionice piše se kao zbroj poddionica")
	}
}

func TestKarticaDioniceBezPodrucjaIUredivanja(t *testing.T) {
	sec := models.Section{Code: "B.34.14", SectorID: "B", UpdatedAt: "nije vrijeme"}
	k := karticaDionice(sec, nil, nil)
	if k.Podrucje != "izravno sektoru" || k.Izmijenjeno != "" || k.Vode != "" {
		t.Errorf("dionica izravno sektoru: %+v", k)
	}
	d := SectionPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{},
		Section:     sec,
		Kartica:     k,
	}
	html := iscrtaj(t, "section_detail.html", d)
	for _, want := range []string{"Dionica nema upisanu letvu iz registra.", "Dokumentacija ne navodi nasipe.",
		"Nitko nije zadužen za ovu dionicu."} {
		if !strings.Contains(html, want) {
			t.Errorf("prazna kartica nema %q", want)
		}
	}
	if strings.Contains(html, "/sections/B.34.14/edit") {
		t.Error("tko ne smije uređivati vidi Uredi")
	}
}

func TestPopisPoddionica(t *testing.T) {
	for _, c := range []struct {
		seq  []int
		want string
	}{
		{[]int{4}, "poddionica 4"},
		{[]int{1, 2, 3}, "poddionice 1–3"},
		{[]int{1, 3, 5}, "poddionice 1, 3 i 5"},
	} {
		if got := popisPoddionica(c.seq); got != c.want {
			t.Errorf("popisPoddionica(%v) = %q, a treba %q", c.seq, got, c.want)
		}
	}
}

// Poddionice se otvaraju jedna po jedna: prva je otvorena, ostale su redak
// sa sažetkom; letva je jedan redak, a pragovi su sklopljeni ispod nje.
func TestPoddioniceJednaPoJedna(t *testing.T) {
	duljina, nasip := 2.971, 1.85
	od, do := 0.0, 2.971
	letva := models.Station{ID: uuid.MustParse("9f1c0b2e-0000-7000-8000-000000000003"), Name: "Branjin Vrh",
		Stationing: "pkm 23,37", Prep: models.Threshold{Cm: cm(120)}, State: models.Threshold{Cm: cm(300)},
		Extremes: []models.StationExtreme{{Kind: models.ExtremeMax, LevelCm: cm(338), OnDate: "2010-06-04"}}}
	prva := models.SectionPart{Seq: 1, WatercourseName: "kanal Travnik", Bank: "LD", Extent: "Ušće u p. Karašicu - državna granica",
		StationingKind: "kkm", KmFrom: &od, KmTo: &do, LengthKm: &duljina,
		Embankments: []models.PartEmbankment{{Name: "Usporni nasip uz l.o. K. Travnik", LengthKm: &nasip}},
		Objects:     []models.PartObject{{Name: "ploč.propust 7,0 m", StationingKind: "kkm", StationingText: "kkm 0+340"}}}
	druga := models.SectionPart{Seq: 2, WatercourseName: "kanal Hatvan"}
	pogledi := []PartView{
		{SectionPart: prva, Rows: embankmentRows(prva), Stations: []models.Station{letva}},
		{SectionPart: druga},
	}
	napuniLetveBlokove(pogledi)
	d := SectionPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{},
		Section:     models.Section{Code: "B.16.1", AreaID: 16, SectorID: "B", Parts: []models.SectionPart{prva, druga}},
		Parts:       pogledi,
	}
	html := iscrtaj(t, "section_detail.html", d)
	if n := strings.Count(html, `class="list-kartica dio-poddionica" open>`); n != 1 {
		t.Errorf("otvorenih poddionica je %d, a treba jedna", n)
	}
	if strings.Index(html, "kanal Hatvan") < strings.Index(html, `dio-poddionica" open>`) {
		t.Error("otvorena je druga poddionica, a ne prva")
	}
	for _, want := range []string{
		"Ušće u p. Karašicu - državna granica · kkm 0&#43;000 – 2&#43;971 · 2,971 km · 1 nasip · 1 objekt",
		"Branjin Vrh</a> · pkm 23,37 · P 120 · IS 300 cm · najviši &#43;338 cm (2010-06-04)",
		"<summary>Pragovi i kota nule</summary>",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("na kartici nema %q", want)
		}
	}
}
