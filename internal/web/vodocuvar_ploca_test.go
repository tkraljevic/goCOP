package web

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/repository"
)

const (
	oznakaPloce  = `class="ploca-stranice list-ploca"`
	oznakaStupci = `class="stupci-stranice list-stupci"`
)

// predanListPloce sprema predan list vodočuvara za jučer, uz dorade
func predanListPloce(t *testing.T, o *okolinaDnevnika, izmijeni func(l *models.VodocuvarskiList)) *models.VodocuvarskiList {
	t.Helper()
	dan := time.Now().In(models.Zagreb).AddDate(0, 0, -1)
	kad := time.Date(dan.Year(), dan.Month(), dan.Day(), 15, 32, 0, 0, models.Zagreb)
	l := &models.VodocuvarskiList{ID: uuid.Must(uuid.NewV7()).String(), UserID: o.vodocuvar.ID.String(), Ime: "Pero Perić", Sektor: "P", AreaID: 1,
		Datum: time.Date(dan.Year(), dan.Month(), dan.Day(), 0, 0, 0, 0, models.Zagreb), Broj: 38, Od: "07:00", Do: "15:00", PredanoAt: &kad, Opis: "obilazak nasipa"}
	if izmijeni != nil {
		izmijeni(l)
	}
	if err := o.repo.Save(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	return l
}

// Rukovoditelj na predanom listu dobiva ploču: ovjeru, odvojenu parafu i upis, iste
// obrasce i imena polja kao prije. Redom u kodu je mobitel: list, ploča, zadaci.
// Vodočuvar na svom listu ploču nema.
func TestPlocaRadnjiNaPredanomListu(t *testing.T) {
	o := novaOkolinaDnevnika(t)
	l := predanListPloce(t, o, nil)
	s := o.zovi(t, o.rukovodi, http.MethodGet, "/vodocuvar/"+l.ID, nil).Body.String()
	for _, x := range []string{
		"list-stranica-ploca", oznakaStupci, oznakaPloce,
		"<h2>Ovjera lista</h2>",
		"List je predao Pero Perić " + l.PredanoAt.Format("2. 1. 2006.") + " u 15:32.",
		`action="/vodocuvar/` + l.ID + `/radnja"`, `name="radnja" value="ovjeri"`, `name="radnja" value="parafiraj"`,
		"Parafa je zasebna i ne traži lozinku.",
		`list-parafa-odvojena`, `class="btn btn-secondary">Parafiraj`, `class="btn btn-primary">Ovjeri list`,
		"<h2>Upis u dnevnik: Pero Perić, ", `action="/vodocuvar/upis"`, `name="vodocuvar" value="` + l.UserID + `"`, `name="datum" value="` + l.Datum.Format("2006-01-02") + `"`,
		`name="list" value="` + l.ID + `"`, `name="natrag" value="/vodocuvar/` + l.ID + `"`, `name="tekst" required`,
		`<label class="form-label" for="upis-tekst">Napomena ili nalaz obilaska</label>`,
		"Upis stoji na ovom listu pod naredbama, s vašim imenom, funkcijom i vremenom. Upisuje se i na predan list.",
	} {
		if !strings.Contains(s, x) {
			t.Errorf("rukovoditeljica: na listu nema %q", x)
		}
	}
	red := -1
	for _, x := range []string{"<dt>Radno vrijeme</dt>", oznakaPloce, `value="ovjeri"`, `value="parafiraj"`, `action="/vodocuvar/upis"`} {
		i := strings.Index(s, x)
		if i < 0 || i < red {
			t.Errorf("%q nije na svom mjestu", x)
		}
		red = i
	}
	if strings.Contains(s, "Drugi list za isti dan") || strings.Contains(s, "dvostruki list") {
		t.Error("list bez dvojnika nosi oznaku dvostrukog lista")
	}

	moj := o.zovi(t, o.vodocuvar, http.MethodGet, "/vodocuvar/"+l.ID, nil).Body.String()
	for _, x := range []string{"list-stranica-ploca", oznakaPloce, oznakaStupci, `value="ovjeri"`, `value="parafiraj"`, `action="/vodocuvar/upis"`} {
		if strings.Contains(moj, x) {
			t.Errorf("vodočuvar: na svom listu ima %q", x)
		}
	}
}

// Na ovjerenom listu ostaju parafa, sada glavna radnja, s imenom tko je ovjerio, i
// upis. Tko je već parafirao, parafu više ne dobiva, a upis ostaje.
func TestPlocaRadnjiNaOvjerenomListu(t *testing.T) {
	o := novaOkolinaDnevnika(t)
	kad := time.Now().Add(-time.Hour)
	l := predanListPloce(t, o, func(l *models.VodocuvarskiList) { l.Potvrdio, l.PotvrdenoAt = "Ana Anić", &kad })
	s := o.zovi(t, o.rukovodi, http.MethodGet, "/vodocuvar/"+l.ID, nil).Body.String()
	for _, x := range []string{"<h2>Parafa</h2>", "Ovjerio Ana Anić. Parafom potvrđujete da ste list vidjeli.", `class="btn btn-primary">Parafiraj`, `action="/vodocuvar/upis"`} {
		if !strings.Contains(s, x) {
			t.Errorf("na ovjerenom listu nema %q", x)
		}
	}
	for _, x := range []string{`value="ovjeri"`, "Ovjera lista", "list-parafa-odvojena"} {
		if strings.Contains(s, x) {
			t.Errorf("na ovjerenom listu stoji %q", x)
		}
	}

	parafiran := predanListPloce(t, o, func(l *models.VodocuvarskiList) {
		l.Broj, l.Potvrdio, l.PotvrdenoAt = 39, "Ana Anić", &kad
		l.Datum = l.Datum.AddDate(0, 0, -1)
		l.Parafe = []models.Parafa{{UserID: o.rukovodi.ID.String(), Ime: "Ana Anić", Kad: kad}}
	})
	s = o.zovi(t, o.rukovodi, http.MethodGet, "/vodocuvar/"+parafiran.ID, nil).Body.String()
	if strings.Contains(s, `value="parafiraj"`) || strings.Contains(s, "<h2>Parafa</h2>") {
		t.Error("tko je parafirao, opet dobiva parafu")
	}
	if !strings.Contains(s, oznakaPloce) || !strings.Contains(s, `action="/vodocuvar/upis"`) {
		t.Error("parafirani list bez ploče s upisom")
	}
}

// Rukovoditelj sektora ovjerava umjesto rukovoditelja područja: uz gumb stoji kućica,
// koju mora označiti. Rukovoditelj područja kućicu nema.
func TestPlocaRadnjiOvjeraUmjesto(t *testing.T) {
	o := novaOkolinaDnevnika(t)
	sektor := "P"
	sef := &models.User{ID: uuid.New(), Username: "iivic", FullName: "Iva Ivić", IsActive: true}
	if err := repository.NewUserRepository(o.baza, o.rec).CreateUser(sef, &models.Duty{Title: "Rukovoditeljica sektora", Role: models.RoleSectorLeader, ScopeType: models.ScopeSector, SectorID: &sektor, IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	l := predanListPloce(t, o, nil)
	const kucica = `<input type="checkbox" name="umjesto" value="1" required> Ovjeravam umjesto rukovoditelja branjenog područja`
	s := o.zovi(t, sef, http.MethodGet, "/vodocuvar/"+l.ID, nil).Body.String()
	if !strings.Contains(s, kucica) || !strings.Contains(s, `value="ovjeri"`) {
		t.Error("ovjera umjesto bez kućice")
	}
	if i, j := strings.Index(s, kucica), strings.Index(s, ">Ovjeri list<"); i < 0 || j < i || i < strings.Index(s, oznakaPloce) {
		t.Error("kućica ne stoji u ploči iznad gumba")
	}
	if s := o.zovi(t, o.rukovodi, http.MethodGet, "/vodocuvar/"+l.ID, nil).Body.String(); strings.Contains(s, `name="umjesto"`) {
		t.Error("rukovoditeljica područja dobiva kućicu „umjesto”")
	}
}

// Ono što osoba na listu smije dolazi iz rukovatelja, a predložak po tome slaže ploču:
// lozinka samo uz ključ, simulirani ključ bez lozinke, i bez ključa samo gumb.
// Bilješka dežurnog stoji pod zapažanjima, ne pod naredbama.
func TestPlocaRadnjiPoPodacima(t *testing.T) {
	o := novaOkolinaDnevnika(t)
	kad := time.Date(2026, time.October, 21, 13, 32, 0, 0, time.UTC)
	osnova := func() *models.VodocuvarskiList {
		return &models.VodocuvarskiList{ID: "list-1", UserID: "v1", Ime: "Pero Perić", AreaID: 1, Datum: time.Date(2026, time.October, 21, 0, 0, 0, 0, models.Zagreb), Broj: 38, Od: "07:00", Do: "15:00", PredanoAt: &kad}
	}
	const lozinka = `id="lozinka-ovjera" name="lozinka" autocomplete="current-password" placeholder="upišite lozinku" required`
	for _, x := range []struct {
		ime     string
		d       VodocuvarPageData
		ima, ne []string
	}{
		{"s ključem", VodocuvarPageData{SmijeOvjeriti: true, ImaKljuc: true}, []string{lozinka, "List je predao Pero Perić 21. 10. 2026. u 15:32."}, []string{"simulirani ključ", `value="parafiraj"`}},
		{"simulacija", VodocuvarPageData{SmijeOvjeriti: true, ImaKljuc: true, Simulacija: true}, []string{"simulirani ključ, bez lozinke"}, []string{lozinka}},
		{"bez ključa", VodocuvarPageData{SmijeOvjeriti: true}, []string{`value="ovjeri"`}, []string{lozinka, "simulirani ključ"}},
		{"samo parafa", VodocuvarPageData{SmijeParafirati: true}, []string{"<h2>Parafa</h2>", "Parafom potvrđujete da ste list vidjeli."}, []string{"Ovjerio ", "Ovjera lista"}},
		{"dežurni operater", VodocuvarPageData{SmijeUpisati: true}, []string{"Upis stoji na ovom listu pod posebnim zapažanjima, kao bilješka, a ne pod naredbama, s vašim imenom"}, []string{"<h2>Parafa</h2>", `value="ovjeri"`}},
		{"bez ičega", VodocuvarPageData{}, nil, []string{oznakaPloce, oznakaStupci, "list-stranica-ploca"}},
	} {
		x.d.List = osnova()
		var b strings.Builder
		if err := o.tmpl("vodocuvar_list.html").ExecuteTemplate(&b, "vodocuvar_list.html", x.d); err != nil {
			t.Fatalf("%s: %v", x.ime, err)
		}
		for _, da := range x.ima {
			if !strings.Contains(b.String(), da) {
				t.Errorf("%s: nema %q", x.ime, da)
			}
		}
		for _, ne := range x.ne {
			if strings.Contains(b.String(), ne) {
				t.Errorf("%s: stoji %q", x.ime, ne)
			}
		}
	}
}

// Dvostruki list: traka i oznaka s dvotočkom, a rukovoditelj drugi list vidi i u ploči.
// Vodočuvar traku i oznaku vidi, ali mu ploča zbog dvojnika ne nastaje.
func TestPlocaDrugiListZaIstiDan(t *testing.T) {
	o := novaOkolinaDnevnika(t)
	prvi := predanListPloce(t, o, nil)
	drugi := predanListPloce(t, o, func(l *models.VodocuvarskiList) { l.Broj, l.Do, l.Opis = 37, "14:30", "pregled ustave Primjerica" })

	s := o.zovi(t, o.rukovodi, http.MethodGet, "/vodocuvar/"+prvi.ID, nil).Body.String()
	for _, x := range []string{
		`<span class="oznaka-stanja list-oznaka-dvostruki">dvostruki list</span>`,
		`Dvostruki list: vidi i <a href="/vodocuvar/` + drugi.ID + `">list br. 37</a>. Za isti dan predano je više listova. Nijedan se ne briše, jer su potpisani dokumenti.`,
		"<h2>Drugi list za isti dan</h2>",
		`<a class="moj-list ceka" href="/vodocuvar/` + drugi.ID + `">`,
		`List 37 · `, drugi.DatumKratko() + " · 07:00–14:30 · 7,5 h", "pregled ustave Primjerica",
		"Ovjeravate svaki list zasebno. Oba ostaju s oznakom „dvostruki list”.",
	} {
		if !strings.Contains(s, x) {
			t.Errorf("rukovoditeljica: na dvostrukom listu nema %q", x)
		}
	}
	if strings.Contains(s, "Dvostruki list –") {
		t.Error("na stranici lista stoji crtica umjesto dvotočke")
	}
	if i, j := strings.Index(s, "Drugi list za isti dan"), strings.Index(s, "<h2>Ovjera lista</h2>"); i < strings.Index(s, oznakaPloce) || j < i {
		t.Error("drugi list ne stoji u ploči iznad ovjere")
	}

	moj := o.zovi(t, o.vodocuvar, http.MethodGet, "/vodocuvar/"+prvi.ID, nil).Body.String()
	if !strings.Contains(moj, "Dvostruki list: vidi i") || !strings.Contains(moj, "list-oznaka-dvostruki") {
		t.Error("vodočuvar ne vidi traku i oznaku dvostrukog lista")
	}
	if strings.Contains(moj, "Drugi list za isti dan") || strings.Contains(moj, oznakaPloce) {
		t.Error("vodočuvar zbog dvojnika dobiva ploču")
	}

	// tko list ne ovjerava, ne čita da ga ovjerava
	var b strings.Builder
	l := *prvi
	if err := o.tmpl("vodocuvar_list.html").ExecuteTemplate(&b, "vodocuvar_list.html", VodocuvarPageData{List: &l, DrugiPredani: []models.VodocuvarskiList{*drugi, *drugi}}); err != nil {
		t.Fatal(err)
	}
	if x := b.String(); strings.Contains(x, "Ovjeravate svaki list") || !strings.Contains(x, "Svi ostaju s oznakom") || !strings.Contains(x, "<h2>Drugi listovi za isti dan</h2>") {
		t.Error("ploča bez radnji ili s više dvojnika krivo govori o ovjeri")
	}
}

// Dva stupca na širokom, jedan na uskom. Višak visine ploče ide u zadnji red, a kvačica
// ovjerenog lista ne ide na oznaku dvostrukog lista.
func TestPlocaRadnjiStil(t *testing.T) {
	stil := procitajStil(t)
	for _, x := range []string{"@container (min-width: 940px)", "grid-template-rows: auto auto 1fr", ".list-stupci > .list-ploca { grid-column: 2; grid-row: 1 / -1; }",
		".list-glava-ovjeren .oznaka-stanja:not(.list-oznaka-dvostruki)::before", ".list-ploca .btn, .list-ovjera .checkbox-label { min-height: 48px; }"} {
		if !strings.Contains(stil, x) {
			t.Errorf("u stilu nema %q", x)
		}
	}
}
