package web

import (
	"archive/zip"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"gocop/internal/repository"
	"math"
	"net/http"
	"net/url"
	"regexp"

	"bytes"
	"html/template"
	"io"
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
//
// Kota nule se pritom mora ispisati na tri decimale. Sa dvije bi Batinina kota
// od 80,189 m u polju postala 80,19 i spremanjem bi se doista promijenila —
// milimetar, ali pomiče cijelu ljestvicu pragova, i to bez ijedne poruke.
func TestObrazacLetvePiseZarezBezTisucica(t *testing.T) {
	kota, milimetarska := 1234.56, 80.189
	html := iscrtaj(t, "station_form.html", StationPageData{
		CurrentUser: &models.User{FullName: "Provjera"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     models.Station{Name: "Belišće", ZeroDatum: &kota, ZeroDatumNew: &milimetarska},
		IsEdit:      true,
	})
	if !strings.Contains(html, `value="1234,560"`) {
		t.Error("polje ne sadrži 1234,560")
	}
	if !strings.Contains(html, `value="80,189"`) {
		t.Error("kota nule se u obrascu skraćuje — spremanjem bi se promijenila")
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

// Historijat letve nosi obrane svih dionica koje se po njoj vode, pa uz svaku
// mora stajati i o kojoj se dionici radi.
func TestHistorijatPrikazujeObraneSvihDionica(t *testing.T) {
	poc := time.Date(2024, 9, 17, 5, 0, 0, 0, time.UTC)
	kraj := time.Date(2024, 10, 22, 5, 0, 0, 0, time.UTC)
	vrh := 703
	vrhAt := time.Date(2024, 9, 25, 5, 0, 0, 0, time.UTC)
	html := iscrtaj(t, "station_history.html", StationPageData{
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
			t.Errorf("u historijatu letve nema %q", want)
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
func TestHistorijatCrtaArhivu(t *testing.T) {
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
	html := iscrtaj(t, "station_history.html", StationPageData{
		CurrentUser:  &models.User{FullName: "Provjera"},
		Permissions:  &models.UserPermissions{IsGlobalAdmin: true},
		Station:      models.Station{ID: uuid.MustParse("c625fa9d-0425-5115-8c49-8819cbb17bbd"), Name: "Batina", Code: "batina"},
		Profili:      []models.ProfilKorita{profil},
		Profil:       &profil,
		Crtez:        crtez,
		Zadnji:       &models.HidroTocka{Kad: kad, Vrijednost: 300},
		ZadnjiIzvor:  "his2000",
		ZadnjiProtok: 2939,
		// Protok stoji uz trenutno stanje, a taj odjeljak traži i spojeni niz —
		// prije je protok dolazio iz natpisa uz presjek, kojeg na kartici više nema.
		Sada: &models.SpojenaVrijednost{Kad: kad, Vrijednost: 300, Izvor: "his2000", Vrsta: "srednjak"},
		Spojevi: []models.SpojDoseg{{Letva: "batina", Velicina: "vodostaj", Korak: "satni",
			Od: "2001-03-09", Do: "2026-07-31", Zapisa: 222624}},
		Nizovi: []models.HidroNiz{
			{ID: 1, Letva: "batina", Izvor: "his2000", Velicina: "vodostaj", Vrsta: "satni",
				Od: "2001-03-09", Do: "2026-07-31", Zapisa: 222624},
			{ID: 2, Letva: "batina", Izvor: "preracun-mohacs", Velicina: "vodostaj", Vrsta: "srednjak",
				Od: "1.1.1901.", Do: "2001-03-08", Zapisa: 36493},
		},
		NizID: 1,
		Pregled: &models.HidroPregled{
			Niz: models.HidroNiz{ID: 1, Izvor: "his2000", Velicina: "vodostaj", Vrsta: "satni"},
			Max: 772, MaxNa: "2013-06-13", Min: -128, MinNa: "2003-09-01", Srednjak: 201.4,
			Godine: []models.HidroGodina{{Godina: 2013, Zapisa: 8760, Max: 772, MaxNa: "2013-06-13", Min: 12, MinNa: "2013-12-30", Srednjak: 244.1}},
		},
		Krivulje: []models.HQKrivulja{{VrijediOd: "2024-01-01", Izvor: "DHMZ, HIS-2000",
			Odsjecci: []models.HQOdsjecak{
				{OdCm: -85, DoCm: 300, Oblik: models.OblikPolinom, P1: 0.1, P2: 512.754, P3: 1210.186},
				{OdCm: 300, DoCm: 560, Oblik: models.OblikPolinom, P1: 66.063, P2: 158.727, P3: 1678.604},
				{OdCm: 560, DoCm: 800, Oblik: models.OblikPolinom, P1: 218.145, P2: -1441.644, P3: 5871.3921},
			}}},
	})
	for _, want := range []string{
		"DHMZ, ovjereno", "2.939 m³/s",
		"Izvorni nizovi", "222.624", "nije mjereno ovdje",
		// predložak plus ispisuje kao &#43;, pa se traži oblik kakav vidi preglednik
		"Krivulje protoka", "DHMZ, HIS-2000", "-85 do 300 cm",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("u historijatu letve nema %q", want)
		}
	}
}

// Godišnji hod, trajanje i zbroj. Zbroj se ispisuje samo za veličine koje se
// gomilaju — pronos nanosa se zbraja, vodostaj nema smisla zbrajati.
func TestHistorijatPrikazujeHodTrajanjeIZbroj(t *testing.T) {
	osnovno := func(p *models.HidroPregled) string {
		return iscrtaj(t, "station_history.html", StationPageData{
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
func TestHistorijatPrikazujeSpojeniNiz(t *testing.T) {
	kad := time.Date(2026, 9, 7, 6, 0, 0, 0, time.UTC)
	html := iscrtaj(t, "station_history.html", StationPageData{
		CurrentUser: &models.User{FullName: "Provjera"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     models.Station{ID: uuid.MustParse("c625fa9d-0425-5115-8c49-8819cbb17bbd"), Name: "Batina", Code: "batina"},
		Sada:        &models.SpojenaVrijednost{Kad: kad, Vrijednost: -129, Izvor: "letva-dhmz", Vrsta: "trenutna", Tocnost: 1},
		Spojevi: []models.SpojDoseg{
			{Velicina: "vodostaj", Korak: "dnevni", Od: "1.1.1901.", Do: "2026-09-06", Zapisa: 45806,
				Dijelovi: []models.SpojDio{
					{Izvor: "preracun-mohacs", Vrsta: "srednjak", Zapisa: 36493, Od: "1.1.1901.", Do: "2001-03-08", Tocnost: 14},
					{Izvor: "his2000", Vrsta: "srednjak", Zapisa: 9276, Od: "2001-03-09", Do: "2026-07-31", Tocnost: 0},
					{Izvor: "cop", Vrsta: "jutarnji", Zapisa: 37, Od: "2026-08-01", Do: "2026-09-06", Tocnost: 3},
				}},
		},
	})
	for _, want := range []string{
		"Vodostaj i protok", "-129 cm", "±1", "telemetrija, DHMZ",
		"1.1.1901.", "45.806", "preračunato iz Mohácsa", "±14", "ovjereno",
		"79 %", // udio rekonstrukcije u spojenom dnevnom nizu
		"jutarnje očitanje, nije srednjak",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("u historijatu nema %q", want)
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
func TestKarticaRazdvajaIzmjereniOdZabiljezenog(t *testing.T) {
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
	// Razliku nosi tablica ekstrema: obje vrijednosti sa svojim datumom i
	// oznakom podrijetla. Uz pragove su nekad stajale i kao pilule, pa se ista
	// brojka čitala dvaput.
	for _, want := range []string{
		"&#43;775 cm", "14.6.2013.", "izmjereno",
		"&#43;795 cm", "24.6.1965.", "rekonstruirano",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("na kartici nema %q", want)
		}
	}
	if strings.Contains(html, "Zabilježene krajnosti") {
		t.Error("krajnosti su se vratile uz pragove; cijele stoje u tablici ekstrema")
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
	html := iscrtaj(t, "station_history.html", StationPageData{
		CurrentUser: &models.User{FullName: "Provjera"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     models.Station{Name: "Batina", Code: "batina"},
		ArhivaPogled: ArhivaPogled{ArhVelicine: []string{"vodostaj", "protok", "temperatura", "pronos"},
			ArhVelicina: "vodostaj",
			ArhKorak:    "dnevni",
			ArhJedinica: "cm",
			ArhGodine:   []int{2013, 2012, 1956},
			ArhGodina:   2013,
			ArhPager:    pagerZa(&http.Request{URL: &url.URL{Path: "/readings/station/x"}}, "ap", 8760, 100),
			ArhChart:    crtajNiz(nizZaGraf(kad), "vodostaj", &models.Station{Name: "Batina"}, nil),
			ArhSada:     &models.SpojenaVrijednost{Kad: kad, Vrijednost: 771, Izvor: "his2000", Tocnost: 0},
			ArhNiz: []models.SpojenaVrijednost{
				{Kad: kad, Vrijednost: 771, Izvor: "his2000", Vrsta: "srednjak", Tocnost: 0},
				{Kad: kad.AddDate(0, 0, -1), Vrijednost: 758, Izvor: "letva-dhmz", Vrsta: "srednjak", Tocnost: 1},
			},
			ArhSazetak: []models.SazetakVelicine{
				{Velicina: "vodostaj", Od: "1.1.1901.", Do: "2026-09-06", Srednjak: 205, Max: 797, MaxNa: "13.3.1956.", Min: -308, MinNa: "1947-09-20"},
				{Velicina: "pronos", Od: "2018-05-01", Do: "2025-12-31", Srednjak: 6005.7, Max: 145575, Min: 37.5},
			},
		},
	})
	for _, want := range []string{
		"Povijest iz arhive", "Vodostaj", "Protok", "Temperatura vode", "Pronos nanosa",
		"1956", "2013", "771", "ovjereno", "telemetrija, DHMZ", "±1",
		"771 cm",            // zadnja vrijednost na vrhu, odmah iznad tablice
		"797", "13.3.1956.", // sažetak
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
	prazna := iscrtaj(t, "station_history.html", StationPageData{
		CurrentUser: &models.User{FullName: "Provjera"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     models.Station{Name: "Nešto", Code: "nesto"},
	})
	if strings.Contains(prazna, "Povijest iz arhive") {
		t.Error("letva bez arhive ne smije prikazivati odjeljak arhive")
	}
}

// nizZaGraf daje nekoliko vrijednosti za provjeru crtanja
func nizZaGraf(kad time.Time) []models.SpojenaVrijednost {
	return []models.SpojenaVrijednost{
		{Kad: kad, Vrijednost: 771},
		{Kad: kad.AddDate(0, 0, -1), Vrijednost: 758},
		{Kad: kad.AddDate(0, 0, -2), Vrijednost: 690},
	}
}

// Klik na redak sažetka mora namjestiti izbornik na tu veličinu, jer inače
// tablica i izbornik govore o različitim stvarima.
func TestRedakSazetkaOtvaraTuVelicinu(t *testing.T) {
	kad := time.Date(2013, 6, 14, 6, 0, 0, 0, time.UTC)
	sazetak := []models.SazetakVelicine{
		{Velicina: "vodostaj", Od: "1.1.1901.", Do: "2026-09-06", Srednjak: 205, Max: 797, Min: -308},
		{Velicina: "protok", Od: "1.1.1901.", Do: "2025-12-31", Srednjak: 2383, Max: 8450, Min: 284},
	}

	// stranica povijesti: redak vodi na istu stranicu, samo drugu veličinu
	html := iscrtaj(t, "station_history.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     models.Station{ID: uuid.New(), Name: "Batina", Code: "batina"},
		ArhivaPogled: ArhivaPogled{ArhVelicine: []string{"vodostaj", "protok"}, ArhVelicina: "vodostaj",
			ArhKorak: "dnevni", ArhGodina: 2013, ArhGodine: []int{2013},
			ArhNiz:     []models.SpojenaVrijednost{{Kad: kad, Vrijednost: 771, Izvor: "his2000", Vrsta: "srednjak"}},
			ArhSazetak: sazetak,
			ArhRed:     "vrh",
			ArhPager:   pagerZa(&http.Request{URL: &url.URL{Path: "/x"}}, "ap", 365, 100),
		},
	})
	// Veza nosi i poredak, inače bi prijelaz na drugu veličinu tiho vratio
	// listanje na datum.
	for _, want := range []string{"?v=protok&amp;korak=dnevni&amp;god=2013&amp;mj=0&amp;red=vrh#niz", `id="niz"`} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica povijesti nema %q", want)
		}
	}

	id := uuid.MustParse("c625fa9d-0425-5115-8c49-8819cbb17bbd")
	// Sažetak i preglednik stoje na istoj stranici, pa redak vodi na nju samu.
	// Dok je preglednik bio na očitanjima, redak je vodio onamo — i tko je
	// htio usporediti dvije veličine, hodao je između dviju stranica.
	if strings.Contains(html, "/readings/station/"+id.String()+"?v=") {
		t.Error("redak sažetka vodi na očitanja, gdje arhive više nema")
	}
}

// Graf mora raditi za svaku veličinu, sa svojom jedinicom i decimalama.
// Pragovi obrane crtaju se samo uz vodostaj: temperatura nema pripremno stanje.
func TestGrafZaSvakuVelicinu(t *testing.T) {
	cm := func(v int) *int { return &v }
	letva := &models.Station{Name: "Batina",
		Prep: models.Threshold{Cm: cm(300)}, Regular: models.Threshold{Cm: cm(500)},
		Emergency: models.Threshold{Cm: cm(650)}, State: models.Threshold{Cm: cm(800)}}
	poc := time.Date(2013, 6, 1, 0, 0, 0, 0, time.UTC)
	niz := func(v ...float64) []models.SpojenaVrijednost {
		var out []models.SpojenaVrijednost
		for i, x := range v {
			out = append(out, models.SpojenaVrijednost{Kad: poc.AddDate(0, 0, i), Vrijednost: x})
		}
		return out
	}

	vod := crtajNiz(niz(120, 340, 560, 772, 610), "vodostaj", letva, nil)
	if vod == nil {
		t.Fatal("graf vodostaja se nije izgradio")
	}
	if len(vod.Thresholds) == 0 {
		t.Error("uz vodostaj moraju stajati pragovi obrane")
	}
	if got := vod.Points[3].Oznaka; got != "772 cm" {
		t.Errorf("oznaka točke %q, očekivano 772 cm", got)
	}

	temp := crtajNiz(niz(0.4, 12.7, 24.9, 29.7), "temperatura", letva, nil)
	if temp == nil {
		t.Fatal("graf temperature se nije izgradio")
	}
	if len(temp.Thresholds) != 0 {
		t.Error("temperatura nema pragove obrane, a graf ih crta")
	}
	if got := temp.Points[3].Oznaka; got != "29,7 °C" {
		t.Errorf("oznaka temperature %q, očekivano 29,7 stupnjeva", got)
	}

	pron := crtajNiz(niz(37.5, 6005, 145575), "pronos", letva, nil)
	if pron == nil {
		t.Fatal("graf pronosa se nije izgradio")
	}
	if got := pron.Points[2].Oznaka; got != "145.575 t" {
		t.Errorf("oznaka pronosa %q, očekivano 145.575 t", got)
	}

	// prorjeđivanje spojenog niza čuva vrh, kao i ono za očitanja
	var dug []models.SpojenaVrijednost
	for i := 0; i < 8760; i++ {
		v := float64(100 + i%40)
		if i == 5000 {
			v = 8450
		}
		dug = append(dug, models.SpojenaVrijednost{Kad: poc.Add(time.Duration(i) * time.Hour), Vrijednost: v})
	}
	kraci := prorijediNiz(dug, 700)
	if len(kraci) > 900 {
		t.Errorf("prorijeđeno na %d točaka", len(kraci))
	}
	najv := 0.0
	for _, v := range kraci {
		najv = math.Max(najv, v.Vrijednost)
	}
	if najv != 8450 {
		t.Errorf("vrh izgubljen: %v", najv)
	}
}

// Graf godine dobiva oznake po mjesecima, ne tri na cijelu godinu, i korak
// osi koji se čita. Točke za pokazivač idu u podatak, ne u crtež.
func TestGrafImaMjeseceIKorakOsi(t *testing.T) {
	cm := func(v int) *int { return &v }
	letva := &models.Station{Name: "Batina",
		Prep: models.Threshold{Cm: cm(300)}, Regular: models.Threshold{Cm: cm(500)},
		Emergency: models.Threshold{Cm: cm(650)}, State: models.Threshold{Cm: cm(800)}}

	poc := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	var godina []models.SpojenaVrijednost
	for i := 0; i < 366; i++ {
		v := 100.0 + float64(i%300)
		if i == 200 {
			v = 708
		}
		godina = append(godina, models.SpojenaVrijednost{Kad: poc.AddDate(0, 0, i), Vrijednost: v})
	}
	g := crtajNiz(godina, "vodostaj", letva, nil)
	if g == nil {
		t.Fatal("graf se nije izgradio")
	}
	if len(g.XTicks) < 10 {
		t.Errorf("oznaka na vremenskoj osi %d, za godinu se očekuje po mjesecima", len(g.XTicks))
	}
	if g.XTicks[0].Label != "sij 24" {
		t.Errorf("prva oznaka %q, očekivano „sij 24“", g.XTicks[0].Label)
	}
	// korak od 100 cm: između susjednih oznaka mora biti 100
	if len(g.YTicks) < 5 {
		t.Fatalf("oznaka na okomitoj osi %d", len(g.YTicks))
	}
	a, b := atof(strings.ReplaceAll(g.YTicks[0].Label, ".", "")), atof(strings.ReplaceAll(g.YTicks[1].Label, ".", ""))
	if d := b - a; d != 100 && d != -100 {
		t.Errorf("korak osi %v, očekivano 100", d)
	}
	if g.Tocke == "" || !strings.HasPrefix(g.Tocke, "[[") {
		t.Error("točke za pokazivač nisu pripremljene")
	}
	if !strings.Contains(g.Tocke, "708 cm") {
		t.Error("vrh vala nema svoju oznaku među točkama")
	}

	// kratko razdoblje ne dobiva mjesece nego dane
	kratko := godina[:9]
	k := crtajNiz(kratko, "vodostaj", letva, nil)
	if strings.Contains(k.XTicks[0].Label, "sij") {
		t.Errorf("kratko razdoblje označeno mjesecima: %q", k.XTicks[0].Label)
	}
}

// Ispravak arhive: vraćena datoteka se prvo pročita i usporedi, ništa se ne
// upisuje bez pregleda. Ispravak bez razloga nije ispravak.
func TestCitanjeIspravakaIzDatoteke(t *testing.T) {
	// izvoz i uvoz idu u zagrebačkom vremenu, jer se datoteka uspoređuje s onim
	// što letva pokazuje na zaslonu
	kad := func(d int) time.Time { return time.Date(2013, 6, d, 6, 0, 0, 0, models.Zagreb).UTC() }
	postojece := map[int64]models.SpojenaVrijednost{
		kad(13).Unix(): {Kad: kad(13), Vrijednost: 758, Izvor: "his2000"},
		kad(14).Unix(): {Kad: kad(14), Vrijednost: 771, Izvor: "his2000"},
		kad(15).Unix(): {Kad: kad(15), Vrijednost: 769, Izvor: "letva-hv"},
	}
	csv := "\ufeffvrijeme;vodostaj_cm;izvor;tocnost;ispravak;razlog\n" +
		"2013-06-13 06:00:00;758;his2000;0;;\n" + // nediran
		"2013-06-14 06:00:00;771;his2000;0;775;ovjereni maksimum iz elaborata\n" + // promjena
		"2013-06-15 06:00:00;769;letva-hv;5;800;\n" + // bez razloga
		"2013-06-16 06:00:00;700;his2000;0;705;dan kojeg nema\n" + // izvan arhive
		"2013-06-14 06:00:00;771;his2000;0;xyz;nečitljivo\n" // vrijednost nije broj

	redci, err := citajIspravke([]byte(csv), postojece, 0)
	if err != nil {
		t.Fatalf("čitanje: %v", err)
	}
	if len(redci) != 4 {
		t.Fatalf("redaka s upisom %d, očekivano 4", len(redci))
	}
	var promjena, greske int
	for _, r := range redci {
		if r.Greska != "" {
			greske++
		} else if r.Promjena() {
			promjena++
		}
	}
	if promjena != 1 {
		t.Errorf("stvarnih promjena %d, očekivano 1", promjena)
	}
	if greske != 3 {
		t.Errorf("grešaka %d, očekivano 3 (bez razloga, izvan arhive, nije broj)", greske)
	}
	if redci[0].Novo != 775 || redci[0].Staro != 771 {
		t.Errorf("promjena %v → %v, očekivano 771 → 775", redci[0].Staro, redci[0].Novo)
	}
	if redci[0].Izvor != "his2000" {
		t.Errorf("izvor stare vrijednosti %q", redci[0].Izvor)
	}

	// datoteka bez potrebnih stupaca se odbija, umjesto da tiho ne učini ništa
	if _, err := citajIspravke([]byte("datum;vodostaj\n2013-06-14;771\n"), postojece, 0); err == nil {
		t.Error("datoteka bez stupca „ispravak“ mora biti odbijena")
	}
	// datoteka izvezena prije preimenovanja stupca i dalje se čita
	stara := "vrijeme_utc;vodostaj_cm;izvor;tocnost;ispravak;razlog\n" +
		"2013-06-14 06:00:00;771;his2000;0;775;iz starijeg izvoza\n"
	if r, err := citajIspravke([]byte(stara), postojece, 0); err != nil || len(r) != 1 || !r[0].Promjena() {
		t.Errorf("stari naziv stupca se ne čita: %v, %v", r, err)
	}
}

// Ispravak se stavlja preko arhive, ali izvorna vrijednost ostaje uz njega.
func TestIspravakNePrepisujeArhivu(t *testing.T) {
	kad := time.Date(2013, 6, 14, 6, 0, 0, 0, time.UTC)
	niz := []models.SpojenaVrijednost{
		{Kad: kad, Vrijednost: 771, Izvor: "his2000"},
		{Kad: kad.AddDate(0, 0, -1), Vrijednost: 758, Izvor: "his2000"},
	}
	primijeniIspravke(niz, map[int64]models.ArhivaIspravak{
		kad.Unix(): {Vrijednost: 775, Razlog: "ovjereni maksimum"},
	})
	if !niz[0].Ispravljeno {
		t.Fatal("ispravak nije primijenjen")
	}
	if niz[0].Vrijednost != 775 {
		t.Errorf("vrijednost %v, očekivano 775", niz[0].Vrijednost)
	}
	if niz[0].Izvorno != 771 {
		t.Errorf("izvorna vrijednost izgubljena: %v, očekivano 771", niz[0].Izvorno)
	}
	if niz[0].Razlog != "ovjereni maksimum" {
		t.Errorf("razlog %q", niz[0].Razlog)
	}
	if niz[1].Ispravljeno {
		t.Error("neispravljena vrijednost označena kao ispravljena")
	}
}

// Rukovatelj se sastavlja prije nego što poslužitelj dobije bazu, pa mu se
// pohrane predaju kao dohvatnici. Ovo drži da rukovatelj bez pohrane radi
// umjesto da padne — dvaput je ista greška srušila stranicu.
func TestRukovateljBezPohraneNePada(t *testing.T) {
	h := &ReadingsHandler{}
	if h.arh() != nil {
		t.Error("arhiva bez dohvatnika mora biti prazna")
	}
	if h.isp() != nil {
		t.Error("pohrana ispravaka bez dohvatnika mora biti prazna")
	}
	// dohvatnik koji vraća prazno je isto valjan odgovor
	h.SetArhiva(func() *repository.ArhivaRepository { return nil })
	h.SetIspravci(func() *repository.IspravakRepository { return nil }, nil)
	if h.arh() != nil || h.isp() != nil {
		t.Error("prazan dohvatnik mora vratiti prazno")
	}
	if m := ispravciIz(context.Background(), nil, "batina", "vodostaj", "dnevni",
		time.Now().AddDate(-1, 0, 0), time.Now()); m != nil {
		t.Error("bez pohrane ispravaka ne smije se ništa dohvaćati")
	}

	// prazan repozitorij bez baze ne pada, nego vraća prazno
	var prazan *repository.IspravakRepository
	if m, err := prazan.ZaNiz(context.Background(), "batina", "vodostaj", "dnevni",
		time.Now().AddDate(-1, 0, 0), time.Now()); err != nil || m != nil {
		t.Errorf("prazan repozitorij: %v, %v", m, err)
	}
}

// Pragovi obrane vrijede i na grafu protoka, samo preračunati krivuljom koja
// je tada vrijedila. Bez krivulje se ne crtaju: pogađati prag u protoku bilo bi
// izmišljanje.
func TestPragoviNaGrafuProtoka(t *testing.T) {
	cm := func(v int) *int { return &v }
	letva := &models.Station{Name: "Batina",
		Prep: models.Threshold{Cm: cm(300)}, Regular: models.Threshold{Cm: cm(500)},
		Emergency: models.Threshold{Cm: cm(650)}, State: models.Threshold{Cm: cm(800)}}
	krivulje := []models.HQKrivulja{
		{VrijediOd: "2016-01-01", Odsjecci: []models.HQOdsjecak{
			{OdCm: -85, DoCm: 800, Oblik: models.OblikPolinom, P1: 18.933, P2: 538.85, P3: 1258.8}}},
		{VrijediOd: "2001-01-01", VrijediDo: "2009-12-31", Odsjecci: []models.HQOdsjecak{
			{OdCm: -70, DoCm: 250, Oblik: models.OblikPolinom, P1: 9.097, P2: 517.41, P3: 1139},
			{OdCm: 250, DoCm: 500, Oblik: models.OblikPolinom, P1: 44.278, P2: 360.15, P3: 1312.3},
			{OdCm: 500, DoCm: 800, Oblik: models.OblikPolinom, P1: 214.6, P2: -1463.1, P3: 6170.2998}}},
	}
	poc := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	niz := func(v ...float64) []models.SpojenaVrijednost {
		var out []models.SpojenaVrijednost
		for i, x := range v {
			out = append(out, models.SpojenaVrijednost{Kad: poc.AddDate(0, 0, i*30), Vrijednost: x})
		}
		return out
	}

	g := crtajNiz(niz(1200, 3100, 5000, 6200, 2000), "protok", letva, krivulje)
	if g == nil {
		t.Fatal("graf protoka se nije izgradio")
	}
	if len(g.Thresholds) == 0 {
		t.Fatal("na grafu protoka nema pragova obrane")
	}
	// pripremno stanje na 300 cm daje po službenoj krivulji 2.749 m³/s
	nasao := false
	for _, p := range g.Thresholds {
		if p.Label == "pripremno" {
			nasao = true
		}
	}
	if !nasao {
		t.Error("pripremno stanje nije preračunato u protok")
	}

	// bez krivulje nema pragova na protoku, a vodostaj ih i dalje ima
	bez := crtajNiz(niz(1200, 3100, 5000), "protok", letva, nil)
	if len(bez.Thresholds) != 0 {
		t.Error("bez krivulje se pragovi u protoku ne smiju crtati")
	}
	vod := crtajNiz(niz(120, 340, 560), "vodostaj", letva, nil)
	if len(vod.Thresholds) == 0 {
		t.Error("vodostaj ima pragove i bez krivulje")
	}

	// krivulja se bira po razdoblju koje se gleda
	if k := krivuljaZa(krivulje, time.Date(2005, 6, 1, 0, 0, 0, 0, time.UTC)); k == nil || k.VrijediOd != "2001-01-01" {
		t.Errorf("za 2005. odabrana kriva krivulja: %v", k)
	}
	if k := krivuljaZa(krivulje, time.Date(2013, 6, 1, 0, 0, 0, 0, time.UTC)); k != nil {
		t.Error("za 2013. nema krivulje u ovom popisu, a odabrana je")
	}
}

// Kartica letve pokazuje iste stupnjeve i u protoku, ali samo kad krivulja
// postoji — bez nje bi to bila izmišljena brojka.
func TestKarticaPokazujePragoveUProtoku(t *testing.T) {
	cm := func(v int) *int { return &v }
	st := models.Station{ID: uuid.New(), Name: "Batina", Code: "batina",
		Prep: models.Threshold{Cm: cm(300)}, Regular: models.Threshold{Cm: cm(500)},
		Emergency: models.Threshold{Cm: cm(650)}, State: models.Threshold{Cm: cm(800)}}
	krivulje := []models.HQKrivulja{models.HQKrivulja{VrijediOd: "2026-01-01", Izvor: "DHMZ, HIS-2000",
		Odsjecci: []models.HQOdsjecak{
			{OdCm: -160, DoCm: 300, Oblik: models.OblikPolinom, P1: 0.1, P2: 512.754, P3: 1210.186},
			{OdCm: 300, DoCm: 560, Oblik: models.OblikPolinom, P1: 66.063, P2: 158.727, P3: 1678.604},
			{OdCm: 560, DoCm: 800, Oblik: models.OblikPolinom, P1: 218.145, P2: -1441.644, P3: 5871.3921},
		}}}

	q := pragoviUProtoku(st, krivulje)
	if len(q) != 4 {
		t.Fatalf("pragova u protoku %d, očekivano 4", len(q))
	}
	// 300 cm po službenoj krivulji daje 2.749 m³/s
	if q[0].Q < 2700 || q[0].Q > 2800 {
		t.Errorf("pripremno stanje %v m³/s, očekivano oko 2749", q[0].Q)
	}
	if q[3].Q <= q[2].Q || q[2].Q <= q[1].Q {
		t.Error("protoci pragova moraju rasti s vodostajem")
	}
	if len(pragoviUProtoku(st, nil)) != 0 {
		t.Error("bez krivulje se pragovi u protoku ne smiju računati")
	}

	// Protok stoji uz vodostaj istoga stupnja, u istoj boji — ne u zasebnom
	// popisu, gdje se naziv stupnja i vodostaj ponavljao.
	html := iscrtaj(t, "station_detail.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, Krivulje: krivulje, PragoviQ: q,
		PragoviKote: sProtokom(pragoviUKotama(st), q),
	})
	for _, want := range []string{"Pripremno stanje", "300 cm", "2.749 m³/s",
		`<span class="threshold-pill prep protok">`} {
		if !strings.Contains(html, want) {
			t.Errorf("na kartici nema %q", want)
		}
	}
	if strings.Contains(html, "Isti stupnjevi u protoku") {
		t.Error("zaseban popis protoka ostao je uz pragove")
	}
}

// Očitanja su operativa i ne nose arhivu. Dok su stajale zajedno, dvije su
// stranice pokazivale različit „zadnji vodostaj" i nije bilo jasno koji vrijedi.
func TestOcitanjaNeNoseArhivu(t *testing.T) {
	kad := time.Now().Add(-2 * time.Hour)
	cm := -128
	html := iscrtaj(t, "reading_history.html", ReadingHistoryData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     &models.Station{ID: uuid.New(), Name: "Batina", Code: "batina"},
		GaugeName:   "Batina",
		Readings:    []models.Reading{{MeasuredAt: kad, LevelCm: &cm, Observer: "letva"}},
		Count:       1,
	})
	if strings.Contains(html, "Povijest iz arhive") {
		t.Error("arhiva se vratila na očitanja")
	}
	if !strings.Contains(html, "Svježa očitanja") {
		t.Error("nema svježih očitanja")
	}
	// graf kretanja stoji iznad popisa očitanja
	iOcitanja := strings.Index(html, "Svježa očitanja")
	if i := strings.Index(html, "Kretanje vodostaja"); i > 0 && i > iOcitanja {
		t.Error("graf kretanja stoji ispod popisa očitanja")
	}
}

// Na historijatu graf ide prije tablice vrijednosti — prvo se vidi oblik
// godine, pa tek onda pojedinačni dani.
func TestUArhiviGrafIdePrijeTablice(t *testing.T) {
	kad := time.Date(2013, 6, 14, 6, 0, 0, 0, time.UTC)
	html := iscrtaj(t, "station_history.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     models.Station{ID: uuid.New(), Name: "Batina", Code: "batina"},
		ArhivaPogled: ArhivaPogled{
			ArhVelicine: []string{"vodostaj"}, ArhVelicina: "vodostaj",
			ArhKorak: "dnevni", ArhGodina: 2013, ArhGodine: []int{2013},
			ArhChart: crtajNiz(nizZaGraf(kad), "vodostaj", &models.Station{Name: "Batina"}, nil),
			ArhNiz:   []models.SpojenaVrijednost{{Kad: kad, Vrijednost: 771, Izvor: "his2000"}},
			ArhPager: pagerZa(&http.Request{URL: &url.URL{Path: "/x"}}, "ap", 365, 100),
		},
	})
	iGraf := strings.Index(html, `aria-label="Graf:`)
	iTablica := strings.Index(html, "Novije prvo. Svaka vrijednost")
	if iGraf <= 0 || iTablica <= 0 {
		t.Fatal("nedostaje graf ili tablica")
	}
	if iGraf > iTablica {
		t.Error("tablica stoji iznad grafa")
	}
}

// Zalijepljeni ispis s letva.voda.hr mora se pročitati kakav jest, sa
// zaglavljima i svime. Nečitljiv redak se prijavljuje, ne preskače tiho.
func TestCitanjeZalijepljenihOcitanja(t *testing.T) {
	ispis := `Vodostaj (cm) - satni podaci za razdoblje 07.09.2026. - 07.09.2026.

Dunav - Batina (DHMZ)
07.09.2026. 00 h    -118
07.09.2026. 01 h    -118
07.09.2026. 02 h    -119
07.09.2026. 23 h    -122`

	redci, satni, err := citajZalijepljeno(ispis)
	if err != nil {
		t.Fatalf("čitanje: %v", err)
	}
	if !satni {
		t.Error("ispis sa satima nije prepoznat kao satni")
	}
	if len(redci) != 4 {
		t.Fatalf("pročitano %d redaka, očekivano 4 (zaglavlja se preskaču)", len(redci))
	}
	// vrijeme s letve je lokalno, pa se sprema pretvoreno u UTC
	if l := redci[0].Kad.In(models.Zagreb); l.Hour() != 0 || redci[0].Vrijedi != -118 {
		t.Errorf("prvi redak %v %v", l, redci[0].Vrijedi)
	}
	if l := redci[3].Kad.In(models.Zagreb); l.Hour() != 23 || redci[3].Vrijedi != -122 {
		t.Errorf("zadnji redak %v %v", l, redci[3].Vrijedi)
	}
	if d := redci[0].Kad.In(models.Zagreb); d.Day() != 7 || d.Month() != time.September || d.Year() != 2026 {
		t.Errorf("datum %v", d)
	}
	// 7.9.2026. je ljetno vrijeme, dakle UTC+2
	if u := redci[0].Kad.UTC(); u.Day() != 6 || u.Hour() != 22 {
		t.Errorf("00 h lokalno spremljeno kao %v, očekivano 6.9. 22:00 UTC", u)
	}

	// dnevni oblik, bez sata
	dn, satni2, err := citajZalijepljeno("14.06.2013.\t771\n15.06.2013.\t769")
	if err != nil || satni2 || len(dn) != 2 {
		t.Errorf("dnevni oblik: %d redaka, satni=%v, %v", len(dn), satni2, err)
	}

	// nečitljiva vrijednost se prijavljuje
	los, _, err := citajZalijepljeno("07.09.2026. 00 h    x118")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if len(los) != 1 || los[0].Greska == "" {
		t.Error("nečitljiva vrijednost mora dati grešku, ne tiho otpasti")
	}

	// ispis bez ijednog prepoznatljivog retka se odbija
	if _, _, err := citajZalijepljeno("Vodostaj (cm)\nDunav - Batina"); err == nil {
		t.Error("ispis bez podataka mora biti odbijen")
	}
}

// Usporedba s postojećim očitanjima: novo, isto i različito moraju se
// razlikovati, jer se upisuje samo novo.
func TestUsporedbaSPostojecim(t *testing.T) {
	kad := func(h int) time.Time { return time.Date(2026, 9, 7, h, 0, 0, 0, time.UTC) }
	redci := []ZalijepljenoOcitanje{
		{Kad: kad(0), Vrijedi: -118},
		{Kad: kad(1), Vrijedi: -118},
		{Kad: kad(2), Vrijedi: -125},
		{Kad: kad(3), Greska: "nečitljivo"},
	}
	postojece := map[int64]models.SpojenaVrijednost{
		kad(1).Unix(): {Vrijednost: -118},
		kad(2).Unix(): {Vrijednost: -119},
	}
	novih, istih, razlicitih := usporedi(redci, postojece)
	if novih != 1 || istih != 1 || razlicitih != 1 {
		t.Errorf("novih %d, istih %d, različitih %d — očekivano 1/1/1", novih, istih, razlicitih)
	}
	if !redci[2].Razlicit || redci[2].Staro != -119 {
		t.Errorf("razlika nije zabilježena: %+v", redci[2])
	}
	if redci[0].Postoji {
		t.Error("novo očitanje označeno kao postojeće")
	}
}

// Vodostaj se pretvara u apsolutnu kotu vodne plohe, u svakom visinskom
// sustavu koji letva ima. Na Batini se sustavi razlikuju 26,1 cm — tko na
// terenu nivelira prema reperu u jednom, a čita kotu iz drugoga, promaši za
// tu razliku, a po toj se brojci određuje koliko vreća treba nadvisiti.
func TestApsolutnaKotaVodneP1ohe(t *testing.T) {
	kota := func(v float64) *float64 { return &v }
	st := models.Station{
		Name: "Batina", Code: "batina",
		ZeroDatum: kota(80.450), ZeroDatumSystem: "TRST",
		ZeroDatumNew: kota(80.189), ZeroDatumNewSystem: "HVRS71",
	}
	if !st.ImaKotuNule() {
		t.Fatal("letva ima kotu nule, a javlja da nema")
	}
	k := st.Kote(-123)
	if len(k) != 2 {
		t.Fatalf("kota %d, očekivano 2 sustava", len(k))
	}
	// novi sustav dolazi prvi
	if !k[0].Nova || k[0].Sustav != "HVRS71" {
		t.Errorf("prvi sustav %+v, očekivano HVRS71", k[0])
	}
	if math.Abs(k[0].Kota-78.959) > 0.0005 {
		t.Errorf("HVRS71: %.3f, očekivano 78,959", k[0].Kota)
	}
	// isti broj koji daje i letvin list APS.KOTE, koji računa u starom sustavu
	if math.Abs(k[1].Kota-79.220) > 0.0005 {
		t.Errorf("TRST: %.3f, očekivano 79,220", k[1].Kota)
	}
	if r := k[1].Kota - k[0].Kota; math.Abs(r-0.261) > 0.0005 {
		t.Errorf("razlika sustava %.3f m, očekivano 0,261", r)
	}

	// vrh vala 2013.
	if v := st.Kote(775); math.Abs(v[0].Kota-87.939) > 0.0005 {
		t.Errorf("775 cm u HVRS71: %.3f, očekivano 87,939", v[0].Kota)
	}

	// letva bez ijedne kote ne izmišlja
	prazna := models.Station{Name: "Nešto"}
	if prazna.ImaKotuNule() || len(prazna.Kote(100)) != 0 {
		t.Error("letva bez kote nule ne smije davati apsolutnu visinu")
	}

	// samo stari sustav: daje jednu kotu, i to označenu
	samoStari := models.Station{ZeroDatum: kota(80.450), ZeroDatumSystem: "TRST"}
	k2 := samoStari.Kote(0)
	if len(k2) != 1 || k2[0].Sustav != "TRST" || k2[0].Nova {
		t.Errorf("samo stari sustav: %+v", k2)
	}
}

// Uz svako očitanje stoji i protok po krivulji koja je tada vrijedila. Dežurni
// tako uz visinu vidi i koliko vode prolazi. Bez krivulje se ne piše ništa —
// izmišljen protok gori je od nikakvog.
func TestOcitanjeDobivaProtokIzKrivulje(t *testing.T) {
	kad := time.Date(2026, 9, 7, 8, 0, 0, 0, models.Zagreb)
	cm := 300
	krivulje := []models.HQKrivulja{models.HQKrivulja{VrijediOd: "2026-01-01", Izvor: "DHMZ, HIS-2000",
		Odsjecci: []models.HQOdsjecak{
			{OdCm: -160, DoCm: 300, Oblik: models.OblikPolinom, P1: 0.1, P2: 512.754, P3: 1210.186},
			{OdCm: 300, DoCm: 560, Oblik: models.OblikPolinom, P1: 66.063, P2: 158.727, P3: 1678.604},
			{OdCm: 560, DoCm: 800, Oblik: models.OblikPolinom, P1: 218.145, P2: -1441.644, P3: 5871.3921},
		}}}
	podaci := ReadingHistoryData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     &models.Station{ID: uuid.New(), Name: "Batina", Code: "batina"},
		GaugeName:   "Batina",
		Readings:    []models.Reading{{MeasuredAt: kad.UTC(), LevelCm: &cm}},
		Latest:      &models.Reading{MeasuredAt: kad.UTC(), LevelCm: &cm},
		Count:       1,
		Krivulje:    krivulje,
	}
	html := iscrtaj(t, "reading_history.html", podaci)
	// 300 cm po službenoj krivulji za 2026. daje 2.749 m³/s
	if strings.Count(html, "2.749 m³/s") != 2 {
		t.Errorf("protok se očekuje uz zadnje očitanje i uz redak u tablici, nađeno %d puta",
			strings.Count(html, "2.749 m³/s"))
	}

	podaci.Krivulje = nil
	if strings.Contains(iscrtaj(t, "reading_history.html", podaci), "m³/s") {
		t.Error("bez krivulje se protok ne smije pisati")
	}

	// očitanje starije od svih krivulja ostaje bez protoka
	staro := models.Reading{MeasuredAt: time.Date(1965, 6, 24, 6, 0, 0, 0, time.UTC), LevelCm: &cm}
	podaci.Krivulje, podaci.Readings, podaci.Latest = krivulje, []models.Reading{staro}, &staro
	if strings.Contains(iscrtaj(t, "reading_history.html", podaci), "m³/s") {
		t.Error("krivulja se ne smije protezati izvan razdoblja za koje vrijedi")
	}
}

// Graf za telefon crta se u vlastitom, užem koordinatnom sustavu. Široki graf
// stisnut sa 1.200 na 340 slikovnih točaka smanji oznake ispod čitljivog, pa
// stranica nosi oba i CSS bira koji se pokazuje.
func TestUskiGrafZaTelefon(t *testing.T) {
	var vals []models.SpojenaVrijednost
	pocetak := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 360; i++ {
		vals = append(vals, models.SpojenaVrijednost{
			Kad: pocetak.AddDate(0, 0, i), Vrijednost: float64(100 + i%200), Izvor: "his2000"})
	}
	cm := func(v int) *int { return &v }
	st := &models.Station{ID: uuid.New(), Name: "Batina", Code: "batina",
		Prep: models.Threshold{Cm: cm(300)}, Regular: models.Threshold{Cm: cm(500)}}

	sirok := crtajNiz(vals, "vodostaj", st, nil)
	uzak := crtajNizUzak(vals, "vodostaj", st, nil)
	if uzak.Width >= sirok.Width {
		t.Errorf("uski graf %d nije uži od širokog %d", uzak.Width, sirok.Width)
	}
	if !uzak.Uzak || sirok.Uzak {
		t.Error("graf mora znati je li uzak")
	}
	// Na uskom nema desnog ruba za natpise pragova, pa idu iznad crte, unutar
	// slike. Natpis izvan viewBoxa bio bi nevidljiv.
	if uzak.PragX() >= float64(uzak.Width) {
		t.Errorf("natpis praga na %v izlazi iz slike široke %d", uzak.PragX(), uzak.Width)
	}
	if sirok.PragX() <= sirok.DesnoX() {
		t.Error("na širokom grafu natpis praga stoji u desnom rubu")
	}
	// Godina ima dvanaest mjeseci; na uskoj osi nema mjesta za sve.
	if len(uzak.XTicks) >= len(sirok.XTicks) {
		t.Errorf("uski graf ima %d oznaka, široki %d — mora ih imati manje",
			len(uzak.XTicks), len(sirok.XTicks))
	}
	// Oznaka uz rub priljubi se uz njega; inače bi pola natpisa bilo izvan slike.
	for _, p := range []struct {
		pos  float64
		zeli string
	}{{80, "start"}, {300, "middle"}, {590, "end"}} {
		if got := poravnanjeOznake(p.pos, 76, 594); got != p.zeli {
			t.Errorf("oznaka na %v: poravnanje %q, očekivano %q", p.pos, got, p.zeli)
		}
	}
	for _, tk := range uzak.XTicks {
		if tk.Anchor == "" {
			t.Error("svaka oznaka mora imati poravnanje")
		}
	}
}

// Stranica letve nosi i široki i uski graf, a tablice na telefonu prelaze u
// kartice — svaka vrijednost tada mora znati iz kojeg je stupca.
func TestStranicaLetveNosiObaGrafa(t *testing.T) {
	kad := time.Date(2013, 6, 14, 6, 0, 0, 0, time.UTC)
	html := iscrtaj(t, "station_history.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     models.Station{ID: uuid.New(), Name: "Batina", Code: "batina"},
		ArhivaPogled: ArhivaPogled{
			ArhVelicine: []string{"vodostaj"}, ArhVelicina: "vodostaj", ArhJedinica: "cm",
			ArhKorak: "dnevni", ArhGodina: 2013, ArhGodine: []int{2013},
			ArhNiz:       []models.SpojenaVrijednost{{Kad: kad, Vrijednost: 771, Izvor: "his2000"}},
			ArhChart:     crtajNiz(nizZaGraf(kad), "vodostaj", nil, nil),
			ArhChartUzak: crtajNizUzak(nizZaGraf(kad), "vodostaj", nil, nil),
			ArhSazetak: []models.SazetakVelicine{
				{Velicina: "vodostaj", Od: "1.1.1901.", Do: "2026-09-06", Srednjak: 205, Max: 797, Min: -308},
			},
			ArhPager: pagerZa(&http.Request{URL: &url.URL{Path: "/x"}}, "ap", 365, 100),
		},
	})
	if !strings.Contains(html, `viewBox="0 0 1600 420"`) || !strings.Contains(html, `viewBox="0 0 620 460"`) {
		t.Error("stranica mora nositi i široki i uski graf")
	}
	// Naslove stupaca uz vrijednosti dodaje skripta iz zaglavlja tablice, pa
	// se ne ponavljaju u predlošku; ovdje se traži samo da zaglavlje postoji.
	for _, want := range []string{"<thead>", "Razdoblje", "Odakle"} {
		if !strings.Contains(html, want) {
			t.Errorf("tablica nema %s", want)
		}
	}
}

// Gumb za čuvanje povijesti stoji uz očitanja, a ne u zaglavlju stranice:
// tiče se samo tog odjeljka.
func TestCuvajPovijestStojiUzOcitanja(t *testing.T) {
	html := iscrtaj(t, "reading_history.html", ReadingHistoryData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     &models.Station{ID: uuid.New(), Name: "Batina", Code: "batina"},
		GaugeName:   "Batina", Pogled: "30", PogledOpis: "zadnjih 30 dana",
	})
	iGlava := strings.Index(html, `class="detail-actions"`)
	iOcitanja := strings.Index(html, "Svježa očitanja")
	iGumb := strings.Index(html, "Čuvaj povijest")
	if iGumb < 0 {
		t.Fatal("gumba nema")
	}
	if iGumb < iOcitanja {
		t.Errorf("gumb je iznad očitanja: %d < %d", iGumb, iOcitanja)
	}
	if iGlava > 0 && iGumb < strings.Index(html[iGlava:], "</div>")+iGlava {
		t.Error("gumb je i dalje u zaglavlju stranice")
	}
}

// Znak "?" u zaglavlju vodi na odjeljak pomoći za stranicu na kojoj stojiš.
// Sidro je ActiveNav, pa svaka vrijednost koju stranice koriste mora na
// stranici pomoći imati svoje mjesto — inače znak vodi u prazno.
func TestPomocImaSidroZaSvakuStranicu(t *testing.T) {
	html := iscrtaj(t, "pomoc.html", PomocPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		ActiveNav:   "pomoc",
	})
	// vrijednosti ActiveNav koje stranice postavljaju
	for _, nav := range []string{
		"admin", "dashboard", "journals", "maintenance", "moduli", "organizacija",
		"pomoc", "profile", "readings", "registers", "sections", "settings",
		"stations", "structures", "sync", "teren", "territories", "users", "watercourses",
	} {
		if !strings.Contains(html, `id="`+nav+`"`) {
			t.Errorf("pomoć nema sidro %q — znak ? s te stranice vodi u prazno", nav)
		}
	}
	// znak u zaglavlju vodi na sidro trenutne stranice
	if !strings.Contains(html, `href="/pomoc#pomoc"`) {
		t.Error("znak ? u zaglavlju ne vodi na odjeljak stranice")
	}
	for _, want := range []string{"Kota nule", "HQ krivulja", "Dnevni srednjak", "Pojmovnik"} {
		if !strings.Contains(html, want) {
			t.Errorf("pojmovnik nema %q", want)
		}
	}
}

// Široki graf je pri izdvajanju u zajednički predložak morao ostati isti do
// koordinate: rubovi su prije stajali kao brojke u HTML-u, sad ih računa graf.
func TestSirokiGrafZadrzavaKoordinate(t *testing.T) {
	c := crtajNiz(nizZaGraf(time.Date(2013, 6, 14, 6, 0, 0, 0, time.UTC)), "vodostaj", nil, nil)
	for _, p := range []struct {
		ime  string
		imas float64
		zeli float64
	}{
		{"lijevi rub", c.LijevoX(), 96},
		{"desni rub", c.DesnoX(), 1484},
		{"oznake okomite osi", c.OsY(), 88},
		{"oznake vodoravne osi", c.OsX(), 406},
		{"natpis praga", c.PragX(), 1490},
		{"dno plohe", c.DnoY(), 374},
		{"visina plohe", c.VisinaPlohe(), 352},
	} {
		if p.imas != p.zeli {
			t.Errorf("%s: %v, prije %v", p.ime, p.imas, p.zeli)
		}
	}
}

// Arhivska tablica nema svoj okvir za listanje. Prozorčić unutar stranice
// znači dva klizača jedan u drugome; stranica se lista sama, a koliko se
// redaka pokazuje odlučuje listanje ispod tablice.
func TestArhivskaTablicaNemaSvojKlizac(t *testing.T) {
	kad := time.Date(2013, 6, 14, 6, 0, 0, 0, time.UTC)
	html := iscrtaj(t, "station_history.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     models.Station{ID: uuid.New(), Name: "Batina", Code: "batina"},
		ArhivaPogled: ArhivaPogled{
			ArhVelicine: []string{"vodostaj"}, ArhVelicina: "vodostaj", ArhJedinica: "cm",
			ArhKorak: "dnevni", ArhGodina: 2013, ArhGodine: []int{2013},
			ArhNiz:   []models.SpojenaVrijednost{{Kad: kad, Vrijednost: 771, Izvor: "his2000"}},
			ArhPager: pagerZa(&http.Request{URL: &url.URL{Path: "/x"}}, "ap", 365, 100),
		},
	})
	if strings.Contains(html, "overflow-y:auto") || strings.Contains(html, "max-height:28rem") {
		t.Error("arhivska tablica opet ima vlastiti klizač")
	}
}

// Modul se u izborniku zove Očitanja, a ne Vodostaji: iste stranice nose i
// protok, temperaturu i nanos, pa ime po jednoj veličini vara. Ključ modula
// ostaje "vodostaji" — mijenja se natpis, ne ovlasti.
func TestModulSeZoveOcitanja(t *testing.T) {
	var nasao bool
	for _, m := range models.Modules {
		if m.ID == models.ModuleReadings {
			nasao = true
			if m.Label != "Očitanja" {
				t.Errorf("modul se zove %q", m.Label)
			}
		}
	}
	if !nasao {
		t.Fatal("modula nema u katalogu")
	}
	if models.ModuleReadings != "vodostaji" {
		t.Errorf("ključ modula je %q — mijenjanjem bi svi računi izgubili ovlast",
			models.ModuleReadings)
	}
}

// Presjek korita ispod grafa. Korito je na Batini 1.643 cm, a graf niske vode
// 16 — pod istu os ne idu, pa presjek ima svoju, a veže ih to što obje govore
// u centimetrima na letvi.
func TestPresjekKoritaIspodGrafa(t *testing.T) {
	// pojednostavljen profil: dno na 72,8 m, obale na 89,2 m, kota nule 80,45
	p := models.ProfilKorita{Datum: "2020-08-18", KotaNule: 80.45, Tocke: []models.TockaProfila{
		{Stacionaza: 0, Visina: 89.2}, {Stacionaza: 60, Visina: 78.0},
		{Stacionaza: 200, Visina: 72.8}, {Stacionaza: 340, Visina: 78.0},
		{Stacionaza: 400, Visina: 89.2},
	}}
	pragovi := []PragKorita{{300, "pripremno", "prep"}, {800, "izvanredno stanje", "crit"}}
	c := crtajKoritoP(p, 612, KoritoPostavke{Sirina: 900, Visina: 340, OsUCm: true,
		Pragovi: pragovi, PojasOd: 180, PojasDo: 640, ImaPojas: true})
	if c == nil {
		t.Fatal("crtež nije nastao")
	}
	if c.DnoCm != -765 || c.LijevaCm != 875 || c.DesnaCm != 875 {
		t.Errorf("dno %d, obale %d/%d cm — očekivano -765 i 875", c.DnoCm, c.LijevaCm, c.DesnaCm)
	}
	// Viši vodostaj mora biti više na slici: y raste prema dolje.
	if c.Pragovi[1].Y >= c.Pragovi[0].Y {
		t.Error("izvanredno stanje nacrtano ispod pripremnog")
	}
	if c.YVode >= c.Pragovi[0].Y || c.YVode <= c.Pragovi[1].Y {
		t.Errorf("voda na 612 cm mora biti između pragova 300 i 800: %v", c.YVode)
	}
	// Pojas pokriva raspon razdoblja i obuhvaća vodnu plohu.
	if !c.ImaPojas || c.PojasY > c.YVode || c.PojasY+c.PojasH < c.YVode {
		t.Errorf("pojas %v..%v ne obuhvaća vodu na %v", c.PojasY, c.PojasY+c.PojasH, c.YVode)
	}
	// Podjele su okrugle u centimetrima: kota nule je 80,45 m, pa bi okrugli
	// metar dao −845, −445, −45.
	for _, k := range c.KoteY {
		if k.Cm%50 != 0 {
			t.Errorf("podjela na %d cm nije okrugla", k.Cm)
		}
	}
	// Prag iznad krune obala ipak mora stati u sliku, jer je upravo to podatak
	// koji se traži.
	visok := crtajKoritoP(p, 100, KoritoPostavke{Sirina: 900, Visina: 340, OsUCm: true,
		Pragovi: []PragKorita{{1200, "hipotetski", "crit"}}})
	if visok.Pragovi[0].Y < 0 {
		t.Errorf("prag iznad obale ispao iz slike: y %v", visok.Pragovi[0].Y)
	}
}

// Kartica letve i dalje ima svoje podjele u metrima nad morem — ondje se
// presjek čita uz apsolutne kote, ne uz graf.
func TestKarticaLetveDrziKoteUMetrima(t *testing.T) {
	p := models.ProfilKorita{Datum: "2020-08-18", KotaNule: 80.45, Tocke: []models.TockaProfila{
		{Stacionaza: 0, Visina: 89.2}, {Stacionaza: 200, Visina: 72.8}, {Stacionaza: 400, Visina: 89.2},
	}}
	c := crtajKorito(p, 612)
	for _, k := range c.KoteY {
		if k.Kota != float64(int(k.Kota)) {
			t.Errorf("kota %v nije okrugli metar", k.Kota)
		}
	}
}

// Snimak ne seže uvijek do vrha obale: Batinin iz 2015. počinje tek na 110.
// metru, a iz 2020. na koti +166 cm. Kad je voda iznad niže obale, iz crteža
// se ne smije čitati koliko korita ostaje — to se mora reći, ne prešutjeti.
func TestOdrezanSnimakSePriznaje(t *testing.T) {
	// lijeva obala presječena na 82,11 m = +166 cm, desna do 89,24 m = +879
	p := models.ProfilKorita{Datum: "2020-08-18", KotaNule: 80.45, Tocke: []models.TockaProfila{
		{Stacionaza: 0, Visina: 82.11}, {Stacionaza: 30, Visina: 72.81},
		{Stacionaza: 300, Visina: 75.0}, {Stacionaza: 398.9, Visina: 89.24},
	}}
	nisko := crtajKoritoP(p, -128, KoritoPostavke{Sirina: 900, Visina: 340, OsUCm: true})
	if nisko.LijevaCm != 166 || nisko.DesnaCm != 879 {
		t.Errorf("obale %d/%d cm — očekivano 166 i 879", nisko.LijevaCm, nisko.DesnaCm)
	}
	if nisko.NizaObalaCm != 166 {
		t.Errorf("niža obala %d cm", nisko.NizaObalaCm)
	}
	if nisko.OdrezanSnimak {
		t.Error("voda na -128 cm je duboko unutar snimka")
	}
	visoko := crtajKoritoP(p, 612, KoritoPostavke{Sirina: 900, Visina: 340, OsUCm: true})
	if !visoko.OdrezanSnimak {
		t.Error("voda na 612 cm izlazi iz snimka preko lijeve obale na 166 cm")
	}
}

// Broj oznaka na vremenskoj osi ovisi o tome što na njoj piše. Šest mjeseci
// stane i na uskom grafu, šest datuma sa satom ne — natpisi se preklope.
func TestOznakeOsiNePreklapajuSe(t *testing.T) {
	// dva dana po satu: oznake su oblika „7.9. 05h“
	var dva []models.SpojenaVrijednost
	poc := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 48; i++ {
		dva = append(dva, models.SpojenaVrijednost{Kad: poc.Add(time.Duration(i) * time.Hour),
			Vrijednost: float64(-118 - i%12)})
	}
	// godina po danu: oznake su oblika „ožu“
	var godina []models.SpojenaVrijednost
	for i := 0; i < 365; i++ {
		godina = append(godina, models.SpojenaVrijednost{Kad: poc.AddDate(0, 0, i-364),
			Vrijednost: float64(100 + i%300)})
	}
	provjeri := func(ime string, c *Chart, font float64) {
		t.Helper()
		plotW := c.DesnoX() - c.LijevoX()
		for i := 1; i < len(c.XTicks); i++ {
			razmak := c.XTicks[i].Pos - c.XTicks[i-1].Pos
			sirina := float64(len([]rune(c.XTicks[i].Label))) * font * 0.55
			if razmak < sirina {
				t.Errorf("%s: %q i %q na razmaku %.0f, a natpis je širok %.0f",
					ime, c.XTicks[i-1].Label, c.XTicks[i].Label, razmak, sirina)
			}
		}
		if len(c.XTicks) < 2 {
			t.Errorf("%s: os je ostala bez oznaka", ime)
		}
		_ = plotW
	}
	provjeri("uski, sati", crtajNizUzak(dva, "vodostaj", nil, nil), uskiGraf.Font)
	provjeri("uski, godina", crtajNizUzak(godina, "vodostaj", nil, nil), uskiGraf.Font)
	provjeri("široki, sati", crtajNiz(dva, "vodostaj", nil, nil), sirokiGraf.Font)
	provjeri("široki, godina", crtajNiz(godina, "vodostaj", nil, nil), sirokiGraf.Font)

	// Duži natpisi moraju dati manje oznaka nego kratki na istoj osi.
	sati := crtajNizUzak(dva, "vodostaj", nil, nil)
	mjeseci := crtajNizUzak(godina, "vodostaj", nil, nil)
	if len(sati.XTicks) > len(mjeseci.XTicks) {
		t.Errorf("datumi sa satom (%d oznaka) ne smiju biti gušći od mjeseci (%d)",
			len(sati.XTicks), len(mjeseci.XTicks))
	}
}

// Presjek korita ima uski oblik za telefon, kao i graf: crtež od 900 jedinica
// stisnut na 340 slikovnih točaka daje oznake od tri točke.
func TestUskiPresjekKorita(t *testing.T) {
	p := models.ProfilKorita{Datum: "2020-08-18", KotaNule: 80.45, Tocke: []models.TockaProfila{
		{Stacionaza: 0, Visina: 82.11}, {Stacionaza: 30, Visina: 72.81},
		{Stacionaza: 300, Visina: 75.0}, {Stacionaza: 398.9, Visina: 89.24},
	}}
	pragovi := []PragKorita{{300, "pripremno", "prep"}}
	sirok := crtajKoritoP(p, -128, sirokoKorito.sKoritom(pragovi, -131, -117))
	uzak := crtajKoritoP(p, -128, uskoKorito.sKoritom(pragovi, -131, -117))
	if uzak.Sirina >= sirok.Sirina {
		t.Errorf("uski presjek %d nije uži od širokog %d", uzak.Sirina, sirok.Sirina)
	}
	// Brojke osi na užem crtežu su veće, pa im treba širi rub.
	if uzak.Lijevo <= sirok.Lijevo {
		t.Errorf("uski presjek ima rub %v, široki %v — mora biti širi", uzak.Lijevo, sirok.Lijevo)
	}
	// Oznake i natpisi moraju stati unutar slike.
	if uzak.OsX() <= 0 || uzak.NatpisX() >= float64(uzak.Sirina) {
		t.Errorf("oznake izlaze iz slike: os %v, natpis %v", uzak.OsX(), uzak.NatpisX())
	}
	if uzak.SirinaPlohe != float64(uzak.Sirina)-uzak.Lijevo {
		t.Error("pojas se ne proteže od ruba do kraja slike")
	}
	// Oba crteža moraju reći isto o koritu; razlikuju se samo mjerama.
	if uzak.DnoCm != sirok.DnoCm || uzak.LijevaCm != sirok.LijevaCm {
		t.Error("uski i široki presjek govore različito o istom koritu")
	}
}

// Službena krivulja protoka: niz odsječaka, svaki sa svojim rasponom
// vodostaja. Izvan raspona se ne računa ništa — DHMZ ga objavljuje s
// razlogom, a protok izvan njega bio bi produljenje krivulje ondje gdje je
// nitko nije mjerio.
func TestSluzbenaKrivuljaPoOdsjeccima(t *testing.T) {
	// DHMZ, Batina, 2026.
	k := models.HQKrivulja{VrijediOd: "2026-01-01", Izvor: "DHMZ, HIS-2000",
		Odsjecci: []models.HQOdsjecak{
			{OdCm: -160, DoCm: 300, Oblik: models.OblikPolinom, P1: 0.1, P2: 512.754, P3: 1210.186},
			{OdCm: 300, DoCm: 560, Oblik: models.OblikPolinom, P1: 66.063, P2: 158.727, P3: 1678.604},
			{OdCm: 560, DoCm: 800, Oblik: models.OblikPolinom, P1: 218.145, P2: -1441.644, P3: 5871.3921},
		}}

	// vrijednosti prepisane iz DHMZ-ove tablice
	for _, p := range []struct {
		cm   int
		zeli float64
	}{{-128, 554}, {0, 1210}, {300, 2749}, {560, 4639}, {800, 8300}} {
		q, ok := k.Protok(p.cm)
		if !ok {
			t.Fatalf("%d cm: protok nije izračunat", p.cm)
		}
		if math.Abs(q-p.zeli) > math.Max(2, p.zeli*0.01) {
			t.Errorf("%d cm → %.0f m³/s, u tablici %.0f", p.cm, q, p.zeli)
		}
	}
	// Izvan raspona krivulja šuti.
	if _, ok := k.Protok(-161); ok {
		t.Error("ispod donjeg ruba krivulja je dala protok")
	}
	if _, ok := k.Protok(801); ok {
		t.Error("iznad gornjeg ruba krivulja je dala protok")
	}
	if od, do, ima := k.Raspon(); !ima || od != -160 || do != 800 {
		t.Errorf("raspon %d..%d", od, do)
	}
	// Protok raste s vodostajem i preko granica odsječaka.
	var zadnji float64
	for cm := -160; cm <= 800; cm += 7 {
		q, _ := k.Protok(cm)
		if q <= zadnji {
			t.Fatalf("pri %d cm protok %.0f nije veći od prethodnog %.0f", cm, q, zadnji)
		}
		zadnji = q
	}
	// DHMZ ne traži da se odsječci točno poklope, ali skok mora ostati sitan.
	if s := k.NajveciSkok(); s > 1 {
		t.Errorf("skok na granici odsječaka %.2f %%", s)
	}
	// Zapis mora pokazati sve odsječke s rasponima.
	z := k.Zapis()
	for _, want := range []string{"-160 do 300 cm", "560 do 800 cm", "·H²"} {
		if !strings.Contains(z, want) {
			t.Errorf("zapis nema %q: %s", want, z)
		}
	}

	// Potencijski oblik i dalje radi — njime su preračunati nizovi ondje gdje
	// službene krivulje nema.
	pot := models.HQKrivulja{Odsjecci: []models.HQOdsjecak{
		{OdCm: -142, DoCm: 772, Oblik: models.OblikPotencija, P1: 19.4830, P2: 2.2128, P3: 6.65}}}
	if q, ok := pot.Protok(300); !ok || q < 2900 || q > 2980 {
		t.Errorf("potencijski oblik dao %.0f m³/s pri 300 cm", q)
	}
}

// Zabilježeni ekstremi: izmjereno i rekonstruirano stoje jedno uz drugo, ali
// se ne miješaju. Datum se piše hrvatski, a način i napomena razdvojeno —
// inače se čita kao jedna rečenica.
func TestZabiljezeniEkstremiRazlikujuPodrijetlo(t *testing.T) {
	cm := func(v int) *int { return &v }
	st := models.Station{ID: uuid.New(), Name: "Batina", Code: "batina",
		Extremes: []models.StationExtreme{
			{Kind: models.ExtremeMax, LevelCm: cm(775), OnDate: "2013-06-14",
				Quality: models.QualityMeasured, Source: "DHMZ"},
			{Kind: models.ExtremeMin, LevelCm: cm(-127), OnDate: "1909-01-07",
				Quality: models.QualityReconstructed, Source: "postaja Bezdan",
				Method: "preračun iz vodostaja Bezdana", Note: "Bezdan je 740 m uzvodno."},
			{Kind: models.ExtremeMin, LevelCm: cm(-151), OnDate: "2026-08-22",
				Quality: models.QualityMeasured, Source: "telemetrija, DHMZ",
				Method: "satno očitanje u 4 sata"},
		}}
	html := iscrtaj(t, "station_detail.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st,
	})
	for _, want := range []string{
		"14.6.2013.", "7.1.1909.", "22.8.2026.", // datumi hrvatski
		"-151 cm", "-127 cm",
		"izmjereno", "rekonstruirano",
		"— preračun iz vodostaja Bezdana", // način odvojen crticom
	} {
		if !strings.Contains(html, want) {
			t.Errorf("u tablici ekstrema nema %q", want)
		}
	}
	// način i napomena ne smiju se slijepiti u jednu rečenicu
	if strings.Contains(html, "Bezdana Bezdan je") {
		t.Error("način i napomena su slijepljeni")
	}
	// Ekstremi se ispisuju dvaput: tablicom za širok zaslon i karticama za
	// uzak. Oba minimuma moraju biti u oba oblika.
	if strings.Count(html, "najniži") != 4 {
		t.Errorf("dva minimuma u dva oblika daju četiri spomena, nađeno %d",
			strings.Count(html, "najniži"))
	}
	for _, oblik := range []string{"ekstremi-siroko", "ekstremi-usko"} {
		if !strings.Contains(html, oblik) {
			t.Errorf("nedostaje oblik %q", oblik)
		}
	}
	// Način i napomena su objašnjenje, ne vrijednost — ukoso i sitnije.
	if !strings.Contains(html, "ekstrem-nacin") {
		t.Error("način nije izdvojen od izvora")
	}
}

// Uz najviše stoje i najniži vodostaji, i to oba: izmjereni i rekonstruirani.
// Batina ih ima dva različita svijeta — -151 cm izmjeren u kolovozu 2026. i
// -127 cm preračunat za 7.1.1909., kad letva nije ni postojala.
func TestNajniziVodostajiStojeJedanUzDrugi(t *testing.T) {
	cm := func(v int) *int { return &v }
	st := models.Station{ID: uuid.New(), Name: "Batina", Code: "batina",
		Prep: models.Threshold{Cm: cm(300)},
		Extremes: []models.StationExtreme{
			{Kind: models.ExtremeMax, LevelCm: cm(775), OnDate: "2013-06-14",
				Quality: models.QualityMeasured, Source: "DHMZ"},
			{Kind: models.ExtremeMin, LevelCm: cm(-127), OnDate: "1909-01-07",
				Quality: models.QualityReconstructed, Source: "postaja Bezdan"},
			{Kind: models.ExtremeMin, LevelCm: cm(-151), OnDate: "2026-08-22",
				Quality: models.QualityMeasured, Source: "telemetrija, DHMZ"},
		}}
	if iz := st.NajnizeIzmjereno(); iz == nil || *iz.LevelCm != -151 {
		t.Errorf("najniži izmjereni: %v", iz)
	}
	if re := st.NajnizeRekonstruirano(); re == nil || *re.LevelCm != -127 {
		t.Errorf("najniži rekonstruirani: %v", re)
	}
	// rekonstruirani se ne smije proglasiti izmjerenim ni obrnuto
	if st.NajnizeIzmjereno() == st.NajnizeRekonstruirano() {
		t.Error("izmjereni i rekonstruirani su isti zapis")
	}

	html := iscrtaj(t, "station_detail.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st,
	})
	// Oba najniža stoje u tablici ekstrema, svaki sa svojim datumom i
	// podrijetlom — izmjereni i rekonstruirani ne smiju se stopiti u jedan.
	for _, want := range []string{
		"-151 cm", "22.8.2026.", "izmjereno",
		"-127 cm", "7.1.1909.", "rekonstruirano",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("na kartici nema %q", want)
		}
	}

	// letva bez zabilježenih najnižih ne dobiva prazne retke
	bez := models.Station{ID: uuid.New(), Name: "Bez", Code: "bez", Prep: models.Threshold{Cm: cm(300)}}
	if bez.ImaNajnize() {
		t.Error("letva bez ekstrema tvrdi da ih ima")
	}
	prazna := iscrtaj(t, "station_detail.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     bez,
	})
	if strings.Contains(prazna, "Najniži izmjereni") {
		t.Error("letva bez ekstrema dobila je prazan redak")
	}
}

// Položaj letve: stupnjevi i minute kako stoji u zapisniku DHMZ-a, decimalni
// oblik za strojeve, i poveznica na kartu — vanjska, jer program radi bez
// interneta.
func TestPolozajLetveNaKartici(t *testing.T) {
	lat, lon := 45.845833, 18.854722
	st := models.Station{ID: uuid.New(), Name: "Batina", Code: "batina",
		Latitude: &lat, Longitude: &lon}
	if !st.ImaKoordinate() {
		t.Fatal("letva s koordinatama tvrdi da ih nema")
	}
	if got := st.KoordinateHR(); got != "45° 50′ 45″ S  18° 51′ 17″ I" {
		t.Errorf("koordinate ispisane kao %q", got)
	}
	if u := st.KartaURL(); !strings.Contains(u, "45.845833") || !strings.Contains(u, "18.854722") {
		t.Errorf("poveznica na kartu: %s", u)
	}

	html := iscrtaj(t, "station_detail.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st,
	})
	for _, want := range []string{"45° 50′ 45″ S", "45,845833", "Otvori u pregledniku", "openstreetmap.org"} {
		if !strings.Contains(html, want) {
			t.Errorf("na kartici nema %q", want)
		}
	}
	// poveznica se otvara izvan programa i ne nosi referrer
	if !strings.Contains(html, `rel="noopener noreferrer"`) {
		t.Error("vanjska poveznica bez noopener")
	}

	// letva bez koordinata ne dobiva prazan okvir
	bez := models.Station{ID: uuid.New(), Name: "Bez", Code: "bez"}
	if bez.ImaKoordinate() || bez.KoordinateHR() != "" || bez.KartaURL() != "" {
		t.Error("letva bez koordinata vraća položaj")
	}
	prazna := iscrtaj(t, "station_detail.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     bez,
	})
	if strings.Contains(prazna, "Otvori u pregledniku") {
		t.Error("letva bez koordinata dobila je gumb za kartu")
	}
}

// Karta se crta samo kad ima i koordinate i izvor pločica. Bez izvora se ne
// prikazuje prazan okvir: čvor bez interneta i bez preuzetih pločica nema što
// nacrtati, a koordinate iznad karte vrijede i dalje.
func TestKartaSeCrtaSamoKadImaOboje(t *testing.T) {
	lat, lon := 45.845833, 18.854722
	st := models.Station{ID: uuid.New(), Name: "Batina", Code: "batina",
		Watercourse: "Dunav", Latitude: &lat, Longitude: &lon}
	karta := KartaPostavke{
		Plocice:  "https://maps.wikimedia.org/osm-intl/{z}/{x}/{y}.png",
		Zasluge:  "© OpenStreetMap, pločice Wikimedia",
		NajviseZ: 17,
	}
	if !karta.Ima() || (KartaPostavke{}).Ima() {
		t.Error("postavke karte krivo javljaju ima li izvora")
	}

	sKartom := iscrtaj(t, "station_detail.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, Karta: karta,
	})
	for _, want := range []string{
		`class="karta-letve"`, `data-lat="45.845833"`, `data-lon="18.854722"`,
		"maps.wikimedia.org", "pločice Wikimedia", `data-najvise-z="17"`,
		"Batina · Dunav",
	} {
		if !strings.Contains(sKartom, want) {
			t.Errorf("karta nema %q", want)
		}
	}

	// bez izvora pločica nema okvira
	bezIzvora := iscrtaj(t, "station_detail.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st,
	})
	if strings.Contains(bezIzvora, "karta-letve") {
		t.Error("bez izvora pločica nacrtan je prazan okvir karte")
	}
	// bez koordinata isto
	bezKoord := iscrtaj(t, "station_detail.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     models.Station{ID: uuid.New(), Name: "Bez", Code: "bez"}, Karta: karta,
	})
	if strings.Contains(bezKoord, "karta-letve") {
		t.Error("letva bez koordinata dobila je kartu")
	}
}

// Obrazac postaje mora nuditi sve što se o postaji vodi. Ono što se ne može
// upisati završi kao popravak u kodu — a podaci jedne letve ne pripadaju
// programu nego bazi.
func TestObrazacPostajeNudiSvaPolja(t *testing.T) {
	lat, lon := 45.845833, 18.854722
	kota := 80.450
	html := iscrtaj(t, "station_form.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		IsEdit:      true,
		Station: models.Station{
			ID: uuid.New(), Code: "batina", Name: "Batina", Watercourse: "Dunav",
			ZeroDatum: &kota, ZeroDatumSystem: "TRST",
			ZeroDatumSource:     "Geodetski elaborat 250 BATINA",
			ZeroDatumMethod:     "transformirano u HVRS71",
			ZeroDatumSurveyDate: "2024-09-10", ZeroDatumDocumentDate: "2025-01",
			Latitude: &lat, Longitude: &lon,
			SourceName: "BATINA", NeedsReview: true, ReviewNote: "kota nije potvrđena",
		},
		ExtremesJSON: "[]", ZeroDatumHistoryJSON: "[]",
	})
	for _, polje := range []string{
		"latitude", "longitude",
		"zero_datum_source", "zero_datum_method",
		"zero_datum_survey_date", "zero_datum_document_date",
		"source_name", "needs_review", "review_note",
	} {
		if !strings.Contains(html, `name="`+polje+`"`) {
			t.Errorf("obrazac nema polje %q", polje)
		}
	}
	// upisane vrijednosti moraju se i vidjeti, inače ih spremanje briše
	for _, v := range []string{"45,845833", "18,854722", "Geodetski elaborat 250 BATINA", "2024-09-10", "BATINA"} {
		if !strings.Contains(html, v) {
			t.Errorf("obrazac ne prikazuje %q", v)
		}
	}
	if !strings.Contains(html, `name="needs_review" value="1" checked`) {
		t.Error("oznaka za provjeru se ne prenosi u obrazac")
	}
	// Obrazac nosi tablice s osam stupaca; u uskoj kartici se prelijevaju
	// preko ruba, pa mora biti širok.
	if !strings.Contains(html, `class="form-page form-wide"`) {
		t.Error("obrazac postaje nije širok — redci ekstrema će se prelijevati")
	}
}

// Ekstrem ima i način i napomenu. Dok je obrazac imao jedno polje za oboje,
// otvaranje i spremanje brisalo je napomenu — tiho, jer se nigdje nije vidjela.
func TestEkstremNeGubiNapomenuKrozObrazac(t *testing.T) {
	cm := 795
	ex := []models.StationExtreme{{Kind: models.ExtremeMax, LevelCm: &cm, OnDate: "1965-06-24",
		Quality: models.QualityReconstructed, Source: "postaja Bezdan",
		Method: "preračun iz vodostaja Bezdana",
		Note:   "Neovisna potvrda: preračun iz Mohácsa daje 790 cm."}}
	b, _ := json.Marshal(ex)

	html := iscrtaj(t, "station_form.html", StationPageData{
		CurrentUser:  &models.User{FullName: "P"},
		Permissions:  &models.UserPermissions{IsGlobalAdmin: true},
		IsEdit:       true,
		Station:      models.Station{ID: uuid.New(), Code: "batina", Name: "Batina", Extremes: ex},
		ExtremesJSON: template.JS(b), ZeroDatumHistoryJSON: "[]",
	})
	if !strings.Contains(html, `data-field="note"`) {
		t.Error("ekstrem nema zasebno polje za napomenu")
	}
	if !strings.Contains(html, "Neovisna potvrda") {
		t.Error("napomena se ne prenosi u obrazac")
	}

	// isto i pri čitanju natrag iz obrasca
	vraceno := parseExtremes(`[{"kind":"MAX","level_cm":"795","on_date":"1965-06-24",
		"quality":"REKONSTRUIRANO","source":"postaja Bezdan","method":"preračun","note":"potvrda"}]`)
	if len(vraceno) != 1 {
		t.Fatalf("pročitano %d ekstrema", len(vraceno))
	}
	if vraceno[0].Method != "preračun" || vraceno[0].Note != "potvrda" {
		t.Errorf("način %q, napomena %q", vraceno[0].Method, vraceno[0].Note)
	}
}

// Datum se u bazi vodi kao 2024-09-10, ali elaborat zna nositi samo mjesec
// ili samo godinu. Prikaz mora podnijeti sve tri, a nepoznat oblik ostaviti
// kakav jest umjesto da ga izmisli.
func TestDatumHRPodnosiNepotpunDatum(t *testing.T) {
	f := templateFuncs()["datumHR"].(func(string) string)
	for _, p := range []struct{ ulaz, zeli string }{
		{"2024-09-10", "10.9.2024."},
		{"2025-01", "1.2025."},
		{"1965", "1965."},
		{"", ""},
		{"prije rata", "prije rata"},
	} {
		if got := f(p.ulaz); got != p.zeli {
			t.Errorf("datumHR(%q) = %q, očekivano %q", p.ulaz, got, p.zeli)
		}
	}
}

// Šifra letve nije naziv: „preračunato iz mohacs" nije rečenica koju itko
// piše. Nepoznatoj letvi ostaje šifra, ali s velikim slovom.
func TestNazivIzvoraImenujeLetvu(t *testing.T) {
	for _, p := range []struct{ izvor, zeli string }{
		{"preracun-mohacs", "preračunato iz Mohácsa"},
		{"preracun-bezdan", "preračunato iz Bezdana"},
		{"preracun-hq", "preračunato iz krivulje protoka"},
		{"preracun-mohacs-izvan", "preračunato iz Mohácsa, izvan mjerenog odnosa"},
		{"preracun-neznana", "preračunato iz Neznana"},
		{"his2000", "DHMZ, ovjereno"},
		{"letva-dhmz", "telemetrija, DHMZ"},
	} {
		if got := models.NazivIzvora(p.izvor); got != p.zeli {
			t.Errorf("NazivIzvora(%q) = %q, očekivano %q", p.izvor, got, p.zeli)
		}
	}
	if models.NazivLetve("") != "" {
		t.Error("prazna šifra mora dati prazan naziv")
	}
}

// Razlika visinskih sustava računa se iz same postaje. Dok je stajala u
// podacima stranice, jedan pogrešan redoslijed u rukovatelju značio je da na
// letvi piše 0,000 m premda su obje kote upisane.
func TestRazlikaVisinskihSustavaDolaziIzPostaje(t *testing.T) {
	kota := func(v float64) *float64 { return &v }
	st := models.Station{ID: uuid.New(), Name: "Batina", Code: "batina",
		ZeroDatum: kota(80.450), ZeroDatumSystem: "TRST",
		ZeroDatumNew: kota(80.189), ZeroDatumNewSystem: "HVRS71",
		Prep: models.Threshold{Cm: func(v int) *int { return &v }(300)}}
	// Kote se vraćaju novi sustav pa stari, pa je razlika TRST − HVRS71
	if got := st.RazlikaVisinskihSustava(); got < 0.2605 || got > 0.2615 {
		t.Errorf("razlika %v, očekivano 0,261", got)
	}
	// letva s jednom kotom nema razliku
	jedna := models.Station{ZeroDatum: kota(80.450), ZeroDatumSystem: "TRST"}
	if jedna.RazlikaVisinskihSustava() != 0 {
		t.Error("s jednom kotom razlika mora biti nula")
	}

	// Razlika stoji uz kotu nule, u kartici pragova — ondje gdje se i koristi.
	html := iscrtaj(t, "station_detail.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, PragoviKote: pragoviUKotama(st),
	})
	if !strings.Contains(html, "Razlika sustava") || !strings.Contains(html, "0,261 m") {
		t.Error("razlika visinskih sustava ne stiže na karticu")
	}

	// I u izvješću, koje se prilaže i čita izvan programa.
	iz := IzvjesceLetve{Station: st, Sastavio: "P", Kad: time.Now()}
	var b bytes.Buffer
	if err := iz.Sastavi().Zapisi(&b); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var doc string
	for _, f := range z.File {
		if f.Name == "word/document.xml" {
			r, _ := f.Open()
			raw, _ := io.ReadAll(r)
			r.Close()
			doc = string(raw)
		}
	}
	if !strings.Contains(doc, "0,261 m") {
		t.Error("razlika visinskih sustava ne stiže do korisnika")
	}
}

// Značka uz izvor spojenog niza kaže koliko odstupa. Za izvor po kojem se
// ostali mjere pisalo je „ovjereno" uz natpis koji već glasi „DHMZ, ovjereno".
// A dio koji postoji, a zaokruži se na nulu, ne smije pisati „0 %".
func TestOznakeSpojenogNiza(t *testing.T) {
	referenca := models.SpojDio{Izvor: "his2000", Tocnost: 0, Zapisa: 200}
	if got := referenca.TocnostOznaka(); got != "mjerilo" {
		t.Errorf("izvor bez odstupanja: %q", got)
	}
	telemetrija := models.SpojDio{Izvor: "letva-dhmz", Tocnost: 1, Zapisa: 3}
	if got := telemetrija.TocnostOznaka(); got != "±1 cm" {
		t.Errorf("telemetrija: %q", got)
	}
	if got := telemetrija.UdioHR(1000); got != "<1 %" {
		t.Errorf("sitan udio: %q, očekivano <1 %%", got)
	}
	if got := referenca.UdioHR(1000); got != "20 %" {
		t.Errorf("udio: %q", got)
	}
	prazan := models.SpojDio{Zapisa: 0}
	if got := prazan.UdioHR(1000); got != "0 %" {
		t.Errorf("dio bez zapisa: %q", got)
	}
}

// Datumi se u bazi vode kao 2013-06-14, a na stranici se čitaju hrvatski.
// Sirovi ISO oblik ostao je na šest mjesta na kartici letve i primijetio ga
// je korisnik, ne ja — pa se odsad traži da ga nigdje nema.
func TestKarticaLetveNemaSirovihDatuma(t *testing.T) {
	cm := func(v int) *int { return &v }
	kota := func(v float64) *float64 { return &v }
	st := models.Station{ID: uuid.New(), Name: "Batina", Code: "batina",
		ZeroDatum: kota(80.450), ZeroDatumSystem: "TRST",
		ZeroDatumNew: kota(80.189), ZeroDatumNewSystem: "HVRS71",
		ZeroDatumSource: "Geodetski elaborat", ZeroDatumSurveyDate: "2024-09-10",
		ZeroDatumDocumentDate: "2025-01",
		Prep:                  models.Threshold{Cm: cm(300)},
		Extremes: []models.StationExtreme{
			{Kind: models.ExtremeMax, LevelCm: cm(775), OnDate: "2013-06-14",
				Quality: models.QualityMeasured, Source: "DHMZ"},
			{Kind: models.ExtremeMin, LevelCm: cm(-151), OnDate: "2026-08-22",
				Quality: models.QualityMeasured, Source: "telemetrija, DHMZ"},
		},
		ZeroDatumHistory: []models.ZeroDatumChange{
			{ValidFrom: "2001-03-09", Datum: kota(80.450), System: "TRST", Note: "prva izmjera"},
		},
	}
	podaci := StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, PragoviKote: pragoviUKotama(st),
	}
	kartica := iscrtaj(t, "station_detail.html", podaci)
	povijest := iscrtaj(t, "station_history.html", podaci)

	// ISO datum u tekstu stranice; u atributima obrasca je u redu. Traži se na
	// obje stranice — podaci su se selili među njima, a pravilo vrijedi za oba.
	iso := regexp.MustCompile(`>[^<]*\b\d{4}-\d{2}-\d{2}\b`)
	for ime, html := range map[string]string{"kartici": kartica, "historijatu": povijest} {
		if nasao := iso.FindAllString(html, -1); len(nasao) > 0 {
			t.Errorf("na %s je ostao sirovi datum: %q", ime, nasao[0])
		}
	}
	for _, want := range []string{"14.6.2013.", "22.8.2026."} {
		if !strings.Contains(kartica, want) {
			t.Errorf("na kartici nema hrvatskog datuma %q", want)
		}
	}
	if !strings.Contains(povijest, "9.3.2001.") {
		t.Error("u historijatu nema hrvatskog datuma promjene kote nule")
	}
}

// Presjek se sastavlja od svih snimaka: novija ima prednost svugdje gdje
// seže, starija popunjava ono što novija ne pokriva. Snimke se prije toga
// svode na zajedničku stacionažu — Batinina iz 2020. počinje 104,5 m desno
// od one iz 2010., pa bi bez poravnanja spajala dva različita mjesta.
func TestSpajanjeProfilaPoravnavaSnimke(t *testing.T) {
	stara := models.ProfilKorita{Datum: "2010-03-22", KotaNule: 80.45,
		Tocke: []models.TockaProfila{
			{Stacionaza: 0, Visina: 90.11},   // lijeva obala, samo u staroj
			{Stacionaza: 50, Visina: 85.50},  // terasa, samo u staroj
			{Stacionaza: 105, Visina: 82.11}, // odavde se preklapaju
			{Stacionaza: 200, Visina: 73.20},
			{Stacionaza: 500, Visina: 89.25}, // desna obala, samo u staroj
		}}
	nova := models.ProfilKorita{Datum: "2020-08-18", KotaNule: 80.45, PomakM: 104.5,
		Tocke: []models.TockaProfila{
			{Stacionaza: 0, Visina: 82.11},   // = 104,5 na zajedničkoj mreži
			{Stacionaza: 95, Visina: 72.81},  // = 199,5, dno — novije od staroga
			{Stacionaza: 294, Visina: 89.24}, // = 398,5
		}}

	sp := models.SpojiProfile([]models.ProfilKorita{stara, nova})
	if !sp.Spojen() {
		t.Fatal("profil ne zna da je spojen")
	}
	// Stacionaže moraju rasti i pokrivati cijeli raspon stare snimke.
	if sp.Tocke[0].Stacionaza != 0 || sp.Tocke[len(sp.Tocke)-1].Stacionaza != 500 {
		t.Errorf("raspon %v..%v, očekivano 0..500",
			sp.Tocke[0].Stacionaza, sp.Tocke[len(sp.Tocke)-1].Stacionaza)
	}
	for i := 1; i < len(sp.Tocke); i++ {
		if sp.Tocke[i].Stacionaza < sp.Tocke[i-1].Stacionaza {
			t.Fatalf("stacionaže nisu poredane: %v poslije %v",
				sp.Tocke[i].Stacionaza, sp.Tocke[i-1].Stacionaza)
		}
	}
	// Dno mora doći iz novije snimke, ne iz starije.
	if sp.Dno() != 72.81 {
		t.Errorf("dno %v — mora doći iz novije snimke (72,81)", sp.Dno())
	}
	// Lijeva obala mora doći iz starije, jer je novija ne pokriva.
	najvisa := sp.Tocke[0].Visina
	for _, tc := range sp.Tocke {
		if tc.Visina > najvisa {
			najvisa = tc.Visina
		}
	}
	if najvisa != 90.11 {
		t.Errorf("najviša točka %v — mora doći iz starije snimke (90,11)", najvisa)
	}
	// Poravnanje: nova točka sa stacionaže 95 mora sjesti na 199,5.
	nasao := false
	for _, tc := range sp.Tocke {
		if tc.Visina == 72.81 && tc.Stacionaza == 199.5 {
			nasao = true
		}
	}
	if !nasao {
		t.Error("pomak nije primijenjen na stacionažu novije snimke")
	}
	// Sastav mora reći iz čega je što.
	if len(sp.Sastav) != 2 {
		t.Fatalf("sastav ima %d dijelova", len(sp.Sastav))
	}

	// Jedna snimka ostaje kakva jest.
	sama := models.SpojiProfile([]models.ProfilKorita{stara})
	if sama.Spojen() || len(sama.Tocke) != len(stara.Tocke) {
		t.Error("jedna snimka ne smije se proglasiti spojenom")
	}
}

// Povratni vodostaj bez broja godina ne znači ništa, pa takav redak obrasca
// otpada. Granice pouzdanosti stižu u paru: jedna sama tvrdila bi interval
// koji nije izračunat.
func TestPovratniVodostajiIzObrasca(t *testing.T) {
	raw := `[{"years":"100","level_cm":"+795","low_cm":"762","high_cm":"813",
	           "method":"POT","series":"1902.–2026.","source":"COP","computed_on":"2026-09-08","note":"n"},
	          {"years":"25","level_cm":"755","low_cm":"739"},
	          {"years":"","level_cm":"900"},
	          {"years":"0","level_cm":"900"}]`
	got := parseReturnLevels(raw)
	if len(got) != 2 {
		t.Fatalf("upisana su dva valjana retka, pročitano %d: %+v", len(got), got)
	}
	if got[0].Years != 25 || got[1].Years != 100 {
		t.Errorf("redci se slažu po povratnom razdoblju: %d, %d", got[0].Years, got[1].Years)
	}
	if got[0].LowCm != nil || got[0].HighCm != nil {
		t.Error("polovica raspona se odbacuje, inače bi se prikazao interval koji nije izračunat")
	}
	if got[1].LevelCm == nil || *got[1].LevelCm != 795 {
		t.Errorf("vodostaj s predznakom: %+v", got[1].LevelCm)
	}
	if got[1].Method != "POT" || got[1].Series != "1902.–2026." ||
		got[1].Source != "COP" || got[1].ComputedOn != "2026-09-08" || got[1].Note != "n" {
		t.Errorf("izgubljen podatak o postanku: %+v", got[1])
	}
	if parseReturnLevels("") != nil || parseReturnLevels("[]") != nil || parseReturnLevels("{") != nil {
		t.Error("prazan ili neispravan unos daje prazno")
	}
}

// Kartica letve mora uz procjenu pokazati i čime je dobivena i koliko je
// nesigurna. Brojka bez toga izgleda kao mjerenje, a nije.
func TestKarticaLetvePokazujePovratneVodostaje(t *testing.T) {
	cm := func(v int) *int { return &v }
	st := models.Station{ID: uuid.New(), Name: "Batina", Code: "batina",
		ReturnLevels: []models.StationReturnLevel{
			{Years: 100, LevelCm: cm(795), LowCm: cm(762), HighCm: cm(813),
				Method: "POT, generalizirana Pareto", Series: "1902.–2026.",
				Source: "COP", ComputedOn: "2026-09-08"},
			{Years: 25, LevelCm: cm(755), Method: "POT, generalizirana Pareto", Series: "1902.–2026."},
		}}
	html := iscrtaj(t, "station_history.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, PragoviKote: pragoviUKotama(st),
	})
	for _, want := range []string{
		"Povratni vodostaji", "100 godina", "25 godina", "&#43;795 cm", "&#43;755 cm",
		"&#43;762 do &#43;813 cm", "1 % svake godine", "4 % svake godine",
		"POT, generalizirana Pareto", "1902.–2026.", "8.9.2026.",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("na kartici nema %q", want)
		}
	}
	// Procjena se ne smije čitati kao propisani prag.
	if !strings.Contains(html, "Pragovi obrane su propisani i ne izvode se iz ovih brojki.") {
		t.Error("nema ograde da povratni vodostaj nije prag obrane")
	}
	iso := regexp.MustCompile(`>[^<]*\b\d{4}-\d{2}-\d{2}\b`)
	if nasao := iso.FindAllString(html, -1); len(nasao) > 0 {
		t.Errorf("sirovi datum u prikazu povratnih vodostaja: %q", nasao[0])
	}
	// Postaja bez izračuna nema odjeljak.
	prazna := iscrtaj(t, "station_history.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     models.Station{ID: uuid.New(), Name: "Dalj", Code: "dalj"},
	})
	if strings.Contains(prazna, "Povratni vodostaji") {
		t.Error("postaja bez izračuna ne smije imati prazan odjeljak")
	}
}

// Pridruživanje vodotoka iz registra je postavka, a stajalo je na vrhu
// kartice koja se inače samo čita. Preseljeno je u Uredi; na kartici ostaje
// samo podatak — naziv vode, kako je utvrđena i poveznica na registar.
func TestPridruzivanjeVodotokaJeUObrascuANeNaKartici(t *testing.T) {
	st := models.Station{ID: uuid.New(), Name: "Batina", Code: "batina",
		Watercourse: "Dunav", WatercourseCode: "HR-D-1",
		WatercourseSource: models.WatercourseFromOperator}
	registar := []models.Watercourse{{Code: "HR-D-1", OfficialName: "rijeka Dunav"}}

	kartica := iscrtaj(t, "station_detail.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, PragoviKote: pragoviUKotama(st),
		CanEdit: true, WaterRegistry: registar,
	})
	for _, ne := range []string{
		"Vodotok na kojem postaja stoji", "/api/stations/watercourse",
		"Spremi vodotok", "Pridruži iz registra vodotoka",
	} {
		if strings.Contains(kartica, ne) {
			t.Errorf("kartica još nudi postavljanje vodotoka: %q", ne)
		}
	}
	// Podatak ne smije nestati zajedno s kontrolom.
	for _, want := range []string{"Dunav", "potvrdio operater", `href="/watercourses/HR-D-1"`} {
		if !strings.Contains(kartica, want) {
			t.Errorf("kartica je izgubila %q", want)
		}
	}

	obrazac := iscrtaj(t, "station_form.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, IsEdit: true, WaterRegistry: registar,
	})
	for _, want := range []string{
		"Vodotok iz registra", `action="/api/stations/watercourse"`,
		"Spremi vodotok", "rijeka Dunav", "potvrdio operater",
	} {
		if !strings.Contains(obrazac, want) {
			t.Errorf("u obrascu nema %q", want)
		}
	}
	// Obrazac u obrascu nije valjan HTML: glavni se mora zatvoriti prije ovoga.
	if strings.Index(obrazac, "</form>") > strings.Index(obrazac, `action="/api/stations/watercourse"`) {
		t.Error("pridruživanje vodotoka je ugniježđeno u glavni obrazac")
	}

	// Nova postaja još nema identifikator na koji bi se veza vezala.
	nova := iscrtaj(t, "station_form.html", StationPageData{
		CurrentUser:   &models.User{FullName: "P"},
		Permissions:   &models.UserPermissions{IsGlobalAdmin: true},
		WaterRegistry: registar,
	})
	if strings.Contains(nova, "/api/stations/watercourse") {
		t.Error("nova postaja ne smije nuditi pridruživanje vodotoka")
	}
}

// Valovi obrane su izračun iz niza, ne evidencija. Kartica mora pokazati oba
// vremena — kad bi stanje počelo i kad bi završilo — i trajanje u satima, kako
// se i evidentira u obrascu obrane.
func TestKarticaLetvePrikazujeValoveObrane(t *testing.T) {
	cm := func(v int) *int { return &v }
	st := models.Station{ID: uuid.New(), Name: "Batina", Code: "batina",
		Prep: models.Threshold{Cm: cm(300)}, Regular: models.Threshold{Cm: cm(500)},
		Emergency: models.Threshold{Cm: cm(650)}, State: models.Threshold{Cm: cm(800)}}
	pragovi := st.PragoviObrane()

	poc := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	var niz []models.HidroTocka
	dodaj := func(sati int, v float64) {
		for i := 0; i < sati; i++ {
			niz = append(niz, models.HidroTocka{Kad: poc.Add(time.Duration(len(niz)) * time.Hour), Vrijednost: v})
		}
	}
	dodaj(1, 100)
	dodaj(48, 400)
	dodaj(96, 600)
	dodaj(1, 100)
	valovi := models.Valovi(niz, pragovi)
	if len(valovi) != 1 {
		t.Fatalf("priprema testa: %d valova", len(valovi))
	}

	html := iscrtaj(t, "station_history.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, PragoviKote: pragoviUKotama(st),
		Valovi:        valovi,
		ValoviPragovi: pragovi,
		ValoviZbroj:   models.ZbrojValova(valovi, pragovi),
		ValoviNiz:     models.RazdobljeNiza{Od: niz[0].Kad, Do: niz[len(niz)-1].Kad, Zapisa: len(niz)},
		ValoviPager:   pagerZa(&http.Request{URL: &url.URL{Path: "/x"}}, "val", 1, 20),
	})
	for _, want := range []string{
		"Valovi obrane u nizu", "Pripremno stanje", "Redovna obrana",
		"Ukupno u stanju", "Ukupno iznad praga", "u nizu", "h",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("na kartici nema %q", want)
		}
	}
	// Izvanredno stanje nije dosegnuto: stupac postoji jer prag postoji, ali
	// val u njemu nema vrijednost.
	if !strings.Contains(html, "Izvanredno stanje") {
		t.Error("stupac za upisani prag mora postojati i kad nije dosegnut")
	}
	// Izračun se ne smije predstaviti kao proglašena obrana.
	if !strings.Contains(html, "ne iz proglašenih obrana") {
		t.Error("nema ograde da je riječ o izračunu, a ne o evidenciji")
	}
	iso := regexp.MustCompile(`>[^<]*\b\d{4}-\d{2}-\d{2}\b`)
	if n := iso.FindAllString(html, -1); len(n) > 0 {
		t.Errorf("sirovi datum među valovima: %q", n[0])
	}
}

// Postaja bez pragova u centimetrima ne može dati valove, i to mora reći, a ne
// prikazati prazan popis kao da valova nije bilo.
func TestBezPragovaHistorijatObjasnjavaIzostanakValova(t *testing.T) {
	st := models.Station{ID: uuid.New(), Name: "Dalj", Code: "dalj"}
	html := iscrtaj(t, "station_history.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, PragoviKote: pragoviUKotama(st),
		ValoviPager: pagerZa(&http.Request{URL: &url.URL{Path: "/x"}}, "val", 0, 20),
	})
	if !strings.Contains(html, "nijedan prag obrane nije upisan u centimetrima") {
		t.Error("izostanak pragova se mora objasniti")
	}
}
