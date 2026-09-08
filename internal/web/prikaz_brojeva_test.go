package web

import (
	"github.com/google/uuid"
	"net/http"
	"net/url"

	"bytes"
	"html/template"
	"io/fs"
	"strings"
	"testing"
	"time"

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
		t.Error("površina sliva ostala je bez razdjelnika tisućica")
	}
}

func TestDetaljVodotokaRenderiraMarkdownNapomenu(t *testing.T) {
	html := iscrtaj(t, "watercourse_detail.html", WatercoursePageData{
		CurrentUser: &models.User{FullName: "Provjera"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Water: models.Watercourse{
			Code: "rijeka-dunav", Name: "Dunav",
			Notes: "## Izvor\n\n[Wikipedija](https://hr.wikipedia.org/wiki/Dunav)",
		},
	})
	for _, want := range []string{"<h2>Izvor</h2>", `<a href="https://hr.wikipedia.org/wiki/Dunav">Wikipedija</a>`} {
		if !strings.Contains(html, want) {
			t.Errorf("Markdown napomena nije ispravno prikazana; nema %q", want)
		}
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

// Obrazac dionice ide redom Privitka — nasipi, objekti, ugroženo područje,
// vodomjeri — jer se naselje pripisuje nasipu, pa nasip mora biti upisan
// prije. Uz unos naselja stoji izbor nasipa, skriven dok nasipa nema.
func TestObrazacDioniceIdeRedomPrivitka(t *testing.T) {
	html := iscrtaj(t, "section_form.html", SectionPageData{
		CurrentUser: &models.User{FullName: "Provjera"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Section:     models.Section{Code: "B.34.1", AreaID: 34, SectorID: "B"},
		IsEdit:      true,
	})
	tpl := izmedju(html, `<template id="tpl-part">`, "</template>")
	if tpl == "" {
		t.Fatal("predložak poddionice nije nađen")
	}
	redoslijed := []string{"Nasipi i brane", "Objekti <span", "Ugroženo područje", "Mjerodavni vodomjeri"}
	zadnji := -1
	for _, r := range redoslijed {
		i := strings.Index(tpl, r)
		if i < 0 {
			t.Fatalf("u obrascu nema bloka %q", r)
		}
		if i < zadnji {
			t.Errorf("blok %q dolazi prije prethodnog; redoslijed mora biti %v", r, redoslijed)
		}
		zadnji = i
	}
	if !strings.Contains(tpl, `data-role="terr-emb"`) {
		t.Error("uz unos naselja nema izbora nasipa")
	}
	if !strings.Contains(tpl, `data-role="terr-emb-group" hidden`) {
		t.Error("izbor nasipa mora biti skriven dok nasipa nema")
	}
}

// Obrazac dionice nosi tablice sa sedam stupaca pa dobiva punu širinu; redak
// objekta ima tri stupca — obalu i vrstu stacionaže izvodi iz poddionice i
// zapisa — a "na nasipu" je izbornik nasipa, ne slobodan tekst.
func TestObrazacDioniceSiriIObjektiUTriStupca(t *testing.T) {
	html := iscrtaj(t, "section_form.html", SectionPageData{
		CurrentUser: &models.User{FullName: "Provjera"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Section:     models.Section{Code: "B.34.1", AreaID: 34, SectorID: "B"},
		IsEdit:      true,
	})
	if !strings.Contains(html, `class="form-page form-wide"`) {
		t.Error("obrazac dionice nije na punu širinu")
	}
	obj := izmedju(html, `<template id="tpl-obj">`, "</template>")
	if obj == "" {
		t.Fatal("predložak retka objekta nije nađen")
	}
	if !strings.Contains(obj, `<select class="form-control" data-field="on_embankment"`) {
		t.Error("„na nasipu\" nije izbornik")
	}
	if !strings.Contains(obj, `data-role="obj-extra" hidden`) {
		t.Error("obala i vrsta stacionaže moraju biti skrivene dok ne odstupaju")
	}
	glava := izmedju(html, `<div class="rows-obj">`, `<div data-role="obj-rows">`)
	for _, ne := range []string{"<div>Obala</div>", "<div>Po</div>"} {
		if strings.Contains(glava, ne) {
			t.Errorf("zaglavlje objekata još nosi stupac %s", ne)
		}
	}
}

// Stranica Uvozi nosi sve CSV registre na jednom mjestu: izvoz i uvoz u
// istom obliku, uvoz s oznakom "kind" ondje gdje jedan put prima više registara.
func TestStranicaUvoziNosiSveCSVRegistre(t *testing.T) {
	html := iscrtaj(t, "uvozi.html", AdminPageData{
		CurrentUser: &models.User{FullName: "Provjera"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		CSV:         csvRegisters(),
	})
	for _, want := range []string{
		`href="/territories/zupanije.csv"`, `href="/firme.csv"`, `action="/firme/uvoz"`,
		`<input type="hidden" name="kind" value="opcine">`, `<input type="hidden" name="kind" value="naselja">`,
		`<input type="hidden" name="kind" value="podrucja">`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("na stranici Uvozi nema %s", want)
		}
	}
	if n := strings.Count(html, `enctype="multipart/form-data"`); n != 6 {
		t.Errorf("obrazaca za uvoz ima %d, očekivano 6", n)
	}
}

// Epizoda se pamti po vrhu vala, a ne po zadnjem vodostaju; otvorena epizoda
// mora se vidjeti da još traje.
// Povijest obrana vodi se uz letvu, jer ista letva nosi više dionica. Kartica
// dionice na nju upućuje, a sama je ne prepisuje.
func TestKarticaDioniceUpucujeNaPovijestUzLetvu(t *testing.T) {
	poc := time.Date(2024, 9, 17, 5, 0, 0, 0, time.UTC)
	kraj := time.Date(2024, 10, 22, 5, 0, 0, 0, time.UTC)
	vrh := 703
	vrhAt := time.Date(2024, 9, 25, 5, 0, 0, 0, time.UTC)
	letva := models.Station{ID: uuid.MustParse("c625fa9d-0000-4000-8000-000000000001"), Name: "Batina"}
	html := iscrtaj(t, "section_detail.html", SectionPageData{
		CurrentUser: &models.User{FullName: "Provjera"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Section:     models.Section{Code: "B.34.1", AreaID: 34, SectorID: "B"},
		Gauge:       &letva,
		Episodes: []models.DefenseEpisode{
			{StartedAt: poc, EndedAt: &kraj, Phase: models.PhaseEmergency, PeakCm: &vrh, PeakAt: &vrhAt},
			{StartedAt: poc, Phase: models.PhasePrep},
		},
	})
	for _, want := range []string{"Obrana od poplava", "/stations/" + letva.ID.String() + "#obrane", "Batina", "zabilježeno 2"} {
		if !strings.Contains(html, want) {
			t.Errorf("na kartici nema %q", want)
		}
	}
	// tablica s vrhovima i trajanjima pripada kartici letve, ne dionici
	for _, ne := range []string{"703 cm", "36 dana"} {
		if strings.Contains(html, ne) {
			t.Errorf("kartica dionice prepisuje povijest obrana: %q", ne)
		}
	}
}

// Kartica letve nosi obrane svih dionica koje se po njoj vode, pa uz svaku
// mora stajati i o kojoj se dionici radi.
func TestKarticaLetvePrikazujeObraneSvihDionica(t *testing.T) {
	poc := time.Date(2024, 9, 17, 5, 0, 0, 0, time.UTC)
	kraj := time.Date(2024, 10, 22, 5, 0, 0, 0, time.UTC)
	vrh := 703
	vrhAt := time.Date(2024, 9, 25, 5, 0, 0, 0, time.UTC)
	html := iscrtaj(t, "station_detail.html", StationPageData{
		CurrentUser: &models.User{FullName: "Provjera"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     models.Station{ID: uuid.MustParse("c625fa9d-0000-4000-8000-000000000001"), Name: "Batina"},
		Episodes: []models.DefenseEpisode{
			{SectionCode: "B.34.1", StartedAt: poc, EndedAt: &kraj, Phase: models.PhaseEmergency,
				PeakCm: &vrh, PeakAt: &vrhAt, Basis: models.BasisThreshold},
			{SectionCode: "B.34.2", StartedAt: poc, Phase: models.PhasePrep},
		},
	})
	for _, want := range []string{"Obrane vođene po ovoj letvi", "B.34.1", "B.34.2",
		"703 cm", "Izvanredna obrana", "traje", "36 dana", "prijeđen prag"} {
		if !strings.Contains(html, want) {
			t.Errorf("na kartici letve nema %q", want)
		}
	}
}

// Obranu proglašava čovjek, pa kartica dionice mora ponuditi upis. Bez otvorene
// epizode nudi se proglašenje, a dok obrana traje podizanje i prekid.
func TestKarticaDioniceNudiProglasenjeObrane(t *testing.T) {
	osnovno := SectionPageData{
		CurrentUser: &models.User{FullName: "Provjera"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Section:     models.Section{Code: "B.34.1", AreaID: 34, SectorID: "B"},
		CanDeclare:  true,
		NowLocal:    "2024-09-17T05:00",
		Phases:      []models.DefensePhase{models.PhasePrep, models.PhaseRegular, models.PhaseEmergency, models.PhaseState},
		Bases:       models.BasisOptions(),
	}

	html := iscrtaj(t, "section_detail.html", osnovno)
	for _, want := range []string{"Proglasi obranu", "/sections/B.34.1/obrana/proglasi", "prognoza", "Pripremno stanje"} {
		if !strings.Contains(html, want) {
			t.Errorf("obrazac za proglašenje nema %q", want)
		}
	}

	// Obrana proglašena prije nego što je vodostaj došao do praga: kartica to
	// mora reći, jer inače izgleda kao da je epizoda počela bez razloga.
	poc := time.Date(2024, 9, 17, 5, 0, 0, 0, time.UTC)
	prag := poc.Add(30 * time.Hour)
	otvorena := models.DefenseEpisode{
		SectionCode: "B.34.1", StartedAt: poc, Phase: models.PhaseRegular,
		Basis: models.BasisForecast, ThresholdAt: &prag,
		DeclaredByName: "Željko Kovačević", Origin: models.EpisodeFromOperator,
	}
	sOtvorenom := osnovno
	sOtvorenom.OpenEpisode = &otvorena
	sOtvorenom.Episodes = []models.DefenseEpisode{otvorena}

	html = iscrtaj(t, "section_detail.html", sOtvorenom)
	for _, want := range []string{"na snazi od", "Željko Kovačević", "prognoza",
		"30 sati prije", "/sections/B.34.1/obrana/prekini", "/sections/B.34.1/obrana/podigni"} {
		if !strings.Contains(html, want) {
			t.Errorf("kartica s otvorenom obranom nema %q", want)
		}
	}
	// Stupanj se ne spušta, pa se ne smiju nuditi niži od trenutnog
	if strings.Contains(html, `<option value="PRIPREMNO">`) {
		t.Error("nudi se spuštanje stupnja ispod onog na snazi")
	}
}

// Kartica letve crta korito s vodom u njemu i računa iz arhive. Provjerava se
// da se vodna ploha nacrta i da brojevi ispod crteža stoje.
func TestKarticaLetveCrtaKoritoIArhivu(t *testing.T) {
	profil := models.ProfilKorita{
		Datum: "2020-08-18", Vodostaj: 165, KotaNule: 80.45,
		Tocke: []models.TockaProfila{
			{Stacionaza: 0, Visina: 84.0}, {Stacionaza: 20, Visina: 78.0},
			{Stacionaza: 60, Visina: 74.5}, {Stacionaza: 120, Visina: 76.0},
			{Stacionaza: 180, Visina: 83.5},
		},
	}
	crtez := crtajKorito(profil, 300)
	if crtez == nil {
		t.Fatal("korito se nije nacrtalo")
	}
	if !crtez.ImaVode {
		t.Error("pri 300 cm voda mora biti u koritu")
	}
	// kota nule 80,45 + 3,00 m = 83,45 m; dno 74,50 → dubina 8,95 m
	if v := crtez.KotaVode; v < 83.44 || v > 83.46 {
		t.Errorf("kota vodne plohe %.2f, očekivano 83,45", v)
	}
	if d := crtez.DubinaM; d < 8.94 || d > 8.96 {
		t.Errorf("dubina %.2f m, očekivano 8,95", d)
	}

	// suho korito: vodostaj ispod dna ne smije dati vodnu plohu
	if suho := crtajKorito(profil, -700); suho != nil && suho.ImaVode {
		t.Error("pri vodostaju ispod dna ne smije biti vodne plohe")
	}

	kad := time.Date(2026, 7, 31, 6, 0, 0, 0, time.UTC)
	html := iscrtaj(t, "station_detail.html", StationPageData{
		CurrentUser:  &models.User{FullName: "Provjera"},
		Permissions:  &models.UserPermissions{IsGlobalAdmin: true},
		Station:      models.Station{ID: uuid.MustParse("c625fa9d-0425-5115-8c49-8819cbb17bbd"), Name: "Batina", Code: "batina"},
		Profili:      []models.ProfilKorita{profil},
		Profil:       &profil,
		Crtez:        crtez,
		Zadnji:       &models.HidroTocka{Kad: kad, Vrijednost: 300},
		ZadnjiIzvor:  "his2000",
		ZadnjiProtok: 2939,
		Nizovi: []models.HidroNiz{
			{ID: 1, Letva: "batina", Izvor: "his2000", Velicina: "vodostaj", Vrsta: "satni",
				Od: "2001-03-09", Do: "2026-07-31", Zapisa: 222624},
			{ID: 2, Letva: "batina", Izvor: "preracun-mohacs", Velicina: "vodostaj", Vrsta: "srednjak",
				Od: "1901-01-01", Do: "2001-03-08", Zapisa: 36493},
		},
		NizID: 1,
		Pregled: &models.HidroPregled{
			Niz: models.HidroNiz{ID: 1, Izvor: "his2000", Velicina: "vodostaj", Vrsta: "satni"},
			Max: 772, MaxNa: "2013-06-13", Min: -128, MinNa: "2003-09-01", Srednjak: 201.4,
			Godine: []models.HidroGodina{{Godina: 2013, Zapisa: 8760, Max: 772, MaxNa: "2013-06-13", Min: 12, MinNa: "2013-12-30", Srednjak: 244.1}},
		},
		Krivulje: []models.HQKrivulja{
			{VrijediOd: "2016-01-01", A: 19.483, B: 2.2128, H0: 6.65, Mjerenja: 50, Odstupanje: 4.27},
		},
	})
	for _, want := range []string{
		"Korito i voda u njemu", "<polygon", "<polyline",
		"DHMZ, ovjereno", "2.939 m³/s",
		"Izvorni nizovi", "222.624", "nije mjereno ovdje",
		// predložak plus ispisuje kao &#43;, pa se traži oblik kakav vidi preglednik
		"Krivulje protoka", "Q = 19,4830 · (H &#43; 6,65)^2,2128",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("na kartici letve nema %q", want)
		}
	}
}

// Godišnji hod, trajanje i zbroj. Zbroj se ispisuje samo za veličine koje se
// gomilaju — pronos nanosa se zbraja, vodostaj nema smisla zbrajati.
func TestKarticaLetvePrikazujeHodTrajanjeIZbroj(t *testing.T) {
	osnovno := func(p *models.HidroPregled) string {
		return iscrtaj(t, "station_detail.html", StationPageData{
			CurrentUser: &models.User{FullName: "Provjera"},
			Permissions: &models.UserPermissions{IsGlobalAdmin: true},
			Station:     models.Station{ID: uuid.MustParse("c625fa9d-0425-5115-8c49-8819cbb17bbd"), Name: "Batina", Code: "batina"},
			Nizovi:      []models.HidroNiz{p.Niz},
			NizID:       p.Niz.ID,
			Pregled:     p,
		})
	}

	vodostaj := &models.HidroPregled{
		Niz: models.HidroNiz{ID: 1, Izvor: "his2000", Velicina: "vodostaj", Vrsta: "satni"},
		Min: -128, Max: 772, Srednjak: 201.4,
		Godine: []models.HidroGodina{{Godina: 2013, Zapisa: 8760, Max: 772, Min: 12, Srednjak: 244.1}},
		Mjeseci: []models.HidroMjesec{
			{Mjesec: 1, Zapisa: 18600, Min: -60, Max: 632, Srednjak: 168.5},
			{Mjesec: 6, Zapisa: 18000, Min: -20, Max: 772, Srednjak: 262.0},
		},
		Trajanje: []models.TrajanjeTocka{{Postotak: 5, Vrijednost: 457}, {Postotak: 95, Vrijednost: -6}},
	}
	html := osnovno(vodostaj)
	for _, want := range []string{"Godišnji hod", "siječanj", "lipanj", "168,5", "Trajanje", "457", "-6"} {
		if !strings.Contains(html, want) {
			t.Errorf("nema %q", want)
		}
	}
	if strings.Contains(html, "ukupno kroz razdoblje") {
		t.Error("vodostaj se ne zbraja, a zbroj se ispisuje")
	}

	pronos := &models.HidroPregled{
		Niz: models.HidroNiz{ID: 2, Izvor: "his2000", Velicina: "pronos", Vrsta: "dnevni"},
		Min: 37.5, Max: 145575, Srednjak: 6005.7,
		ZbrojIma: true, Zbroj: 15470641,
		Godine: []models.HidroGodina{{Godina: 2019, Zapisa: 365, Max: 145575, Min: 40, Srednjak: 7000, Zbroj: 2555000}},
	}
	html = osnovno(pronos)
	for _, want := range []string{"Pronos nanosa", "ukupno kroz razdoblje", "15.470.641", "2.555.000"} {
		if !strings.Contains(html, want) {
			t.Errorf("nema %q", want)
		}
	}
	if !models.SeZbraja("pronos") || models.SeZbraja("vodostaj") {
		t.Error("pravilo o zbrajanju nije ispravno")
	}
}

// Spojeni niz mora reći od čega je sastavljen. Broj bez podrijetla je broj
// kojem se poslije ne može provjeriti odakle je došao.
func TestKarticaLetvePrikazujeSpojeniNiz(t *testing.T) {
	kad := time.Date(2026, 9, 7, 6, 0, 0, 0, time.UTC)
	html := iscrtaj(t, "station_detail.html", StationPageData{
		CurrentUser: &models.User{FullName: "Provjera"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     models.Station{ID: uuid.MustParse("c625fa9d-0425-5115-8c49-8819cbb17bbd"), Name: "Batina", Code: "batina"},
		Sada:        &models.SpojenaVrijednost{Kad: kad, Vrijednost: -129, Izvor: "letva-dhmz", Vrsta: "trenutna", Tocnost: 1},
		Spojevi: []models.SpojDoseg{
			{Velicina: "vodostaj", Korak: "dnevni", Od: "1901-01-01", Do: "2026-09-06", Zapisa: 45806,
				Dijelovi: []models.SpojDio{
					{Izvor: "preracun-mohacs", Vrsta: "srednjak", Zapisa: 36493, Od: "1901-01-01", Do: "2001-03-08", Tocnost: 14},
					{Izvor: "his2000", Vrsta: "srednjak", Zapisa: 9276, Od: "2001-03-09", Do: "2026-07-31", Tocnost: 0},
					{Izvor: "cop", Vrsta: "jutarnji", Zapisa: 37, Od: "2026-08-01", Do: "2026-09-06", Tocnost: 3},
				}},
		},
	})
	for _, want := range []string{
		"Vodostaj i protok", "-129 cm", "±1", "telemetrija, DHMZ",
		"1901-01-01", "45.806", "preračunato iz mohacs", "±14", "ovjereno",
		"79 %", // udio rekonstrukcije u spojenom dnevnom nizu
		"jutarnje očitanje, nije srednjak",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("na kartici nema %q", want)
		}
	}

	// udio se računa iz stvarnog broja zapisa, ne procjenjuje
	d := models.SpojDio{Zapisa: 9276}
	if u := d.Udio(45806); u != 20 {
		t.Errorf("udio %d %%, očekivano 20", u)
	}
	if u := d.Udio(0); u != 0 {
		t.Errorf("udio bez ukupnog broja mora biti 0, dobiveno %d", u)
	}
}

// Batina ima dva najviša vodostaja: +775 cm izmjereno 2013. i +795 cm iz
// 1965., preračunato s Bezdana. Oba su točna i moraju stajati jedan uz drugi,
// ali samo izmjereni smije ulaziti u pragove.
func TestKarticaLetveRazdvajaIzmjereniOdZabiljezenog(t *testing.T) {
	cm := func(v int) *int { return &v }
	st := models.Station{
		ID: uuid.MustParse("c625fa9d-0425-5115-8c49-8819cbb17bbd"), Name: "Batina", Code: "batina",
		Extremes: []models.StationExtreme{
			{Kind: models.ExtremeMax, LevelCm: cm(775), OnDate: "2013-06-14", Quality: models.QualityMeasured, Source: "DHMZ"},
			{Kind: models.ExtremeMax, LevelCm: cm(795), OnDate: "1965-06-24", Quality: models.QualityReconstructed, Source: "postaja Bezdan"},
			{Kind: models.ExtremeMin, LevelCm: cm(-127), OnDate: "1909-01-07", Quality: models.QualityMeasured},
		},
	}
	if !st.RekordSeRazlikuje() {
		t.Fatal("Batina ima različit izmjereni i zabilježeni maksimum")
	}
	if v := st.NajviseIzmjereno(); v == nil || *v.LevelCm != 775 {
		t.Errorf("najviši izmjereni %v, očekivano 775", v)
	}
	if v := st.NajviseZabiljezeno(); v == nil || *v.LevelCm != 795 {
		t.Errorf("najviši zabilježeni %v, očekivano 795", v)
	}

	html := iscrtaj(t, "station_detail.html", StationPageData{
		CurrentUser: &models.User{FullName: "Provjera"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st,
	})
	for _, want := range []string{
		"Najviši izmjereni", "&#43;775 cm", "2013-06-14",
		"Najviši zabilježeni", "&#43;795 cm", "1965-06-24", "rekonstruirano",
		"U pragove i u izračun faze ulazi samo izmjereni",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("na kartici nema %q", want)
		}
	}

	// letva bez rekonstruiranog maksimuma prikazuje samo jedan redak
	jedan := models.Station{Name: "Osijek", Code: "osijek", Extremes: []models.StationExtreme{
		{Kind: models.ExtremeMax, LevelCm: cm(514), OnDate: "2013-06-16", Quality: models.QualityMeasured},
	}}
	if jedan.RekordSeRazlikuje() {
		t.Error("letva s jednim maksimumom ne smije prikazivati dva")
	}
}

// Stranica povijesti letve mora raditi i kad operativnih očitanja nema: povijest
// se tada čita iz arhive, s biranjem veličine, koraka i godine.
func TestPovijestLetveCitaArhivu(t *testing.T) {
	kad := time.Date(2013, 6, 14, 6, 0, 0, 0, time.UTC)
	html := iscrtaj(t, "reading_history.html", ReadingHistoryData{
		CurrentUser: &models.User{FullName: "Provjera"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     &models.Station{Name: "Batina", Code: "batina"},
		GaugeName:   "Batina",
		ArhVelicine: []string{"vodostaj", "protok", "temperatura", "pronos"},
		ArhVelicina: "vodostaj",
		ArhKorak:    "dnevni",
		ArhJedinica: "cm",
		ArhGodine:   []int{2013, 2012, 1956},
		ArhGodina:   2013,
		ArhPager:    pagerZa(&http.Request{URL: &url.URL{Path: "/readings/station/x"}}, "ap", 8760, 100),
		ArhChart:    buildChart(nizZaGraf(kad), &models.Station{Name: "Batina"}, false),
		ArhNiz: []models.SpojenaVrijednost{
			{Kad: kad, Vrijednost: 771, Izvor: "his2000", Vrsta: "srednjak", Tocnost: 0},
			{Kad: kad.AddDate(0, 0, -1), Vrijednost: 758, Izvor: "letva-dhmz", Vrsta: "srednjak", Tocnost: 1},
		},
		ArhSazetak: []models.SazetakVelicine{
			{Velicina: "vodostaj", Od: "1901-01-01", Do: "2026-09-06", Srednjak: 205, Max: 797, MaxNa: "1956-03-13", Min: -308, MinNa: "1947-09-20"},
			{Velicina: "pronos", Od: "2018-05-01", Do: "2025-12-31", Srednjak: 6005.7, Max: 145575, Min: 37.5},
		},
	})
	for _, want := range []string{
		"Povijest iz arhive", "Vodostaj", "Protok", "Temperatura vode", "Pronos nanosa",
		"1956", "2013", "771", "ovjereno", "telemetrija, DHMZ", "±1",
		"797", "1956-03-13", // sažetak
		"8.760", // ukupno vrijednosti u godini, iz listanja
		"?ap=2", // listanje kroz satni niz
		// Graf crta putanju, ne polyline: Chart.Path je "d" atribut. Kad je
		// stajao u points="", os se crtala a crta nije — pa se to ovdje drži.
		`class="line" d="M`,
		`class="area" d="M`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("na stranici povijesti nema %q", want)
		}
	}

	// bez arhive stranica se i dalje mora iscrtati
	prazna := iscrtaj(t, "reading_history.html", ReadingHistoryData{
		CurrentUser: &models.User{FullName: "Provjera"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     &models.Station{Name: "Nešto", Code: "nesto"},
		GaugeName:   "Nešto",
	})
	if strings.Contains(prazna, "Povijest iz arhive") {
		t.Error("letva bez arhive ne smije prikazivati odjeljak arhive")
	}
}

// nizZaGraf daje nekoliko očitanja za provjeru crtanja
func nizZaGraf(kad time.Time) []models.Reading {
	cm := func(v int) *int { return &v }
	return []models.Reading{
		{MeasuredAt: kad, LevelCm: cm(771)},
		{MeasuredAt: kad.AddDate(0, 0, -1), LevelCm: cm(758)},
		{MeasuredAt: kad.AddDate(0, 0, -2), LevelCm: cm(690)},
	}
}

// Klik na redak sažetka mora namjestiti izbornik na tu veličinu, jer inače
// tablica i izbornik govore o različitim stvarima.
func TestRedakSazetkaOtvaraTuVelicinu(t *testing.T) {
	kad := time.Date(2013, 6, 14, 6, 0, 0, 0, time.UTC)
	sazetak := []models.SazetakVelicine{
		{Velicina: "vodostaj", Od: "1901-01-01", Do: "2026-09-06", Srednjak: 205, Max: 797, Min: -308},
		{Velicina: "protok", Od: "1901-01-01", Do: "2025-12-31", Srednjak: 2383, Max: 8450, Min: 284},
	}

	// stranica povijesti: redak vodi na istu stranicu, samo drugu veličinu
	html := iscrtaj(t, "reading_history.html", ReadingHistoryData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     &models.Station{ID: uuid.New(), Name: "Batina", Code: "batina"},
		GaugeName:   "Batina",
		ArhVelicine: []string{"vodostaj", "protok"}, ArhVelicina: "vodostaj",
		ArhKorak: "dnevni", ArhGodina: 2013, ArhGodine: []int{2013},
		ArhNiz:     []models.SpojenaVrijednost{{Kad: kad, Vrijednost: 771, Izvor: "his2000", Vrsta: "srednjak"}},
		ArhSazetak: sazetak,
		ArhPager:   pagerZa(&http.Request{URL: &url.URL{Path: "/x"}}, "ap", 365, 100),
	})
	for _, want := range []string{"?v=protok&amp;korak=dnevni&amp;god=2013#niz", `id="niz"`} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica povijesti nema %q", want)
		}
	}

	// kartica letve: redak vodi na povijest, gdje preglednik i živi
	id := uuid.MustParse("c625fa9d-0425-5115-8c49-8819cbb17bbd")
	html = iscrtaj(t, "station_detail.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     models.Station{ID: id, Name: "Batina", Code: "batina"},
		Sazetak:     sazetak,
		Spojevi: []models.SpojDoseg{{Velicina: "vodostaj", Korak: "dnevni",
			Od: "1901-01-01", Do: "2026-09-06", Zapisa: 45806}},
	})
	if want := "/readings/station/" + id.String() + "?v=protok#niz"; !strings.Contains(html, want) {
		t.Errorf("kartica letve ne vodi na %q", want)
	}
}

// Graf satne godine mora pokriti cijelu godinu, ne samo dio. Prorjeđivanje
// smije smanjiti broj točaka, ali ne smije pojesti vrh vala ni odrezati
// početak razdoblja.
func TestProrjedivanjeCuvaVrhIRaspon(t *testing.T) {
	cm := func(v int) *int { return &v }
	poc := time.Date(2013, 1, 1, 0, 0, 0, 0, time.UTC)
	var sati []models.Reading
	for i := 0; i < 8760; i++ {
		v := 100 + i%50
		if i == 4000 { // vrh vala usred godine
			v = 772
		}
		if i == 7000 { // najniža voda
			v = -128
		}
		sati = append(sati, models.Reading{MeasuredAt: poc.Add(time.Duration(i) * time.Hour), LevelCm: cm(v)})
	}

	out := prorijedi(sati, 700)
	if len(out) > 900 {
		t.Errorf("prorijeđeno na %d točaka, očekivano oko 700", len(out))
	}
	if len(out) < 100 {
		t.Errorf("prorijeđeno na svega %d točaka", len(out))
	}

	var najv, najn int = -9999, 9999
	prvi, zadnji := out[0].MeasuredAt, out[len(out)-1].MeasuredAt
	for _, r := range out {
		if *r.LevelCm > najv {
			najv = *r.LevelCm
		}
		if *r.LevelCm < najn {
			najn = *r.LevelCm
		}
	}
	if najv != 772 {
		t.Errorf("vrh vala izgubljen: najviše %d, očekivano 772", najv)
	}
	if najn != -128 {
		t.Errorf("najniža voda izgubljena: najniže %d, očekivano -128", najn)
	}
	if !prvi.Equal(poc) {
		t.Errorf("graf počinje %s, a godina počinje %s", prvi, poc)
	}
	if zadnji.Before(poc.AddDate(1, 0, 0).Add(-2 * time.Hour)) {
		t.Errorf("graf završava %s, prerano za punu godinu", zadnji)
	}

	// kratak niz ostaje netaknut
	kratak := sati[:50]
	if got := prorijedi(kratak, 700); len(got) != 50 {
		t.Errorf("kratak niz prorijeđen na %d, očekivano 50", len(got))
	}
}
