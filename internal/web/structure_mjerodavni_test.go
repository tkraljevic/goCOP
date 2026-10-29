package web

import (
	"context"
	"fmt"
	gohtml "html"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"
)

// okolinaNasipa je baza s jednim područjem, letvama i dionicama, i
// rukovatelj stranice objekta nad njom
type okolinaNasipa struct {
	t          *testing.T
	ctx        context.Context
	admin      *models.UserPermissions
	stations   *repository.StationRepository
	sections   *repository.SectionRepository
	structures *service.StructureService
	h          *StructuresHandler
}

func novaOkolinaNasipa(t *testing.T) *okolinaNasipa {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "nasipi.db"))
	if err != nil {
		t.Fatal(err)
	}
	// test je gotov, greška zatvaranja ne mijenja ishod
	t.Cleanup(func() { _ = baza.Close() })
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	sectionRepo := repository.NewSectionRepository(baza, rec)
	sections := service.NewSectionService(sectionRepo, service.NewSSEBroker())
	stationRepo := repository.NewStationRepository(baza, rec)
	o := &okolinaNasipa{t: t, ctx: context.Background(), admin: &models.UserPermissions{IsGlobalAdmin: true},
		stations: stationRepo, sections: sectionRepo,
		structures: service.NewStructureService(repository.NewStructureRepository(baza, rec))}
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("structure_detail.html")...)
	if err != nil {
		t.Fatal(err)
	}
	o.h = NewStructuresHandler(o.structures, service.NewStationService(stationRepo, sections, service.NewSSEBroker()), sections, nil, nil, tmpl, nil)
	return o
}

func (o *okolinaNasipa) letva(ime string, p, r, i, is int) *models.Station {
	o.t.Helper()
	cm := func(v int) *int { return &v }
	l := &models.Station{ID: uuid.New(), Code: strings.ToLower(strings.ReplaceAll(ime, " ", "-")), Name: ime, Watercourse: "Primjerica",
		Stationing: "rkm 12+300", Prep: models.Threshold{Cm: cm(p)}, Regular: models.Threshold{Cm: cm(r)},
		Emergency: models.Threshold{Cm: cm(i)}, State: models.Threshold{Cm: cm(is)}}
	if err := o.stations.CreateStation(o.ctx, l); err != nil {
		o.t.Fatal(err)
	}
	return l
}

func (o *okolinaNasipa) objekt(ime, vrsta string, letva *models.Station, napomena string) *models.Structure {
	o.t.Helper()
	st := &models.Structure{Code: strings.ToLower(strings.ReplaceAll(ime, " ", "-")), Name: ime, Kind: vrsta, SectorID: "P", AreaID: 1, Notes: napomena}
	if letva != nil {
		st.StationID = letva.ID.String()
	}
	if err := o.structures.Create(o.ctx, o.admin, st); err != nil {
		o.t.Fatal(err)
	}
	return st
}

func (o *okolinaNasipa) dionica(code string, parts ...models.SectionPart) {
	o.t.Helper()
	if err := o.sections.SaveSection(o.ctx, &models.Section{Code: code, AreaID: 1, Parts: parts}); err != nil {
		o.t.Fatal(err)
	}
}

func (o *okolinaNasipa) stranica(st *models.Structure) string {
	o.t.Helper()
	r := httptest.NewRequest(http.MethodGet, st.Path(), nil)
	r.SetPathValue("id", st.ID.String())
	ctx := context.WithValue(r.Context(), contextKeyPerms, o.admin)
	ctx = context.WithValue(ctx, contextKeyUser, &models.User{FullName: "Ana Primjerić"})
	w := httptest.NewRecorder()
	o.h.ShowStructure(w, r.WithContext(ctx))
	if w.Code != http.StatusOK {
		o.t.Fatalf("%s: status %d\n%s", st.Name, w.Code, w.Body.String())
	}
	return gohtml.UnescapeString(w.Body.String())
}

// nasipNa je poddionica s nasipom i zadanim letvama i zapisima vodomjera
func nasipNa(nasip *models.Structure, letve []*models.Station, zapisi ...models.GaugeItem) models.SectionPart {
	p := models.SectionPart{Embankments: []models.PartEmbankment{{StructureID: nasip.ID.String(), Name: nasip.Name}}, Gauges: zapisi}
	for _, l := range letve {
		p.StationIDs = append(p.StationIDs, l.ID.String())
	}
	return p
}

// blokMjerodavnih je isječak stranice od naslova bloka do dionica
func blokMjerodavnih(t *testing.T, html string) string {
	t.Helper()
	od := strings.Index(html, `id="mjerodavni-vodomjeri"`)
	do := strings.Index(html, `Dionice <span`)
	if od < 0 || do < od {
		t.Fatalf("nema bloka mjerodavnih vodomjera prije dionica")
	}
	return html[od:do]
}

// Nasip na dvije poddionice iste dionice i na drugoj dionici: ista letva
// stoji jednom sa svim oznakama dionica i s pragovima sa same letve, kao na
// stranici dionice, iako zapis dokumentacije u dionici navodi druge; zapis
// koji nije letva iz registra stoji kao redak bez poveznice.
func TestNasipMjerodavniVodomjeriIzDionica(t *testing.T) {
	o := novaOkolinaNasipa(t)
	gornji := o.letva("Gornji Primjerak", 300, 400, 500, 600)
	donji := o.letva("Donji Primjerak", 310, 410, 510, 610)
	nasip := o.objekt("Nasip Primjerica lijevo", models.StructureKindEmbankment, nil, "Nasip od zemlje, kruna 4 m.")
	zapis := models.GaugeItem{StationName: "Gornji Primjerak", PrepCm: "+350", RegularCm: "+450", EmergCm: "+550", CriticalCm: "+650"}
	o.dionica("P.1.1", nasipNa(nasip, []*models.Station{gornji}, zapis), nasipNa(nasip, []*models.Station{gornji}, zapis))
	drugi := zapis
	drugi.PrepCm = "+380"
	o.dionica("P.1.2", nasipNa(nasip, []*models.Station{gornji, donji}, drugi,
		models.GaugeItem{StationName: "Kota na brani Primjerak", PrepCm: "112,50 m n. m.", Notes: "čita čuvar brane"},
		models.GaugeItem{StationName: "Prema pravilniku retencije Primjerak"}))

	html := o.stranica(nasip)
	blok := blokMjerodavnih(t, html)
	for _, want := range []string{
		`href="/stations/` + gornji.ID.String() + `"`, `href="/stations/` + donji.ID.String() + `"`,
		"P.1.1, poddionica 1", "P.1.1, poddionica 2", `href="/sections/P.1.2"`,
		`href="/sections/P.1.1"`, "P.1.2",
		"+310 cm", "+410 cm", "+510 cm", "+610 cm",
		"Primjerica · rkm 12+300",
		"<strong>Kota na brani Primjerak</strong>", "P 112,50 m n. m.", "čita čuvar brane",
		"Prema pravilniku retencije Primjerak",
	} {
		if !strings.Contains(blok, want) {
			t.Errorf("blok nema %q\n%s", want, blok)
		}
	}
	if n := strings.Count(blok, `<a href="/stations/`+gornji.ID.String()+`"`); n != 1 {
		t.Errorf("Gornji Primjerak na %d kartica, htio 1 za sve tri poddionice", n)
	}
	// pragovi su isti kao u bloku letve na stranici dionice P.1.1
	for _, prag := range pragoviUKotama(*gornji) {
		if want := fmt.Sprintf("%+d cm", prag.Cm); !strings.Contains(blok, want) {
			t.Errorf("kartica nema prag %s s letve (%s)", want, prag.Naziv)
		}
	}
	for _, izZapisa := range []string{"+350 cm", "+380 cm", "+650 cm", "druge pragove"} {
		if strings.Contains(blok, izZapisa) {
			t.Errorf("kartica pokazuje %q iz zapisa dokumentacije umjesto praga letve", izZapisa)
		}
	}
	if kriteriji := blok[strings.Index(blok, "Kota na brani"):]; strings.Contains(kriteriji, "/stations/") {
		t.Error("kriterij bez letve dobio poveznicu na letvu")
	}
	if strings.Contains(html, "Objekt nema pridružen vodomjer") || strings.Contains(html, "Vodomjer na objektu") {
		t.Error("nasip bez vlastite letve, a s mjerodavnima, i dalje pokazuje blok vodomjera na objektu")
	}
	redoslijed := []string{"Podaci objekta", `id="mjerodavni-vodomjeri"`, "Detaljni opis", "Dionice <span"}
	zadnji := -1
	for _, dio := range redoslijed {
		i := strings.Index(html, dio)
		if i <= zadnji {
			t.Fatalf("%q nije na svom mjestu (redoslijed %v)", dio, redoslijed)
		}
		zadnji = i
	}
}

// Nasip bez mjerodavnih vodomjera u dionici dobiva kratku uputu, a nasip s
// vlastitom letvom zadržava blok letve na objektu uz novi blok
func TestNasipBezMjerodavnihIVlastitaLetva(t *testing.T) {
	o := novaOkolinaNasipa(t)
	letva := o.letva("Srednji Primjerak", 200, 300, 400, 500)
	golo := o.objekt("Nasip bez letve", models.StructureKindEmbankment, nil, "")
	o.dionica("P.1.4", nasipNa(golo, nil))
	html := o.stranica(golo)
	blok := blokMjerodavnih(t, html)
	if !strings.Contains(blok, "Mjerodavni vodomjer nije upisan ni u jednoj dionici na kojoj nasip stoji") || !strings.Contains(blok, "obrascu dionice") {
		t.Errorf("nema upute za nasip bez mjerodavnih\n%s", blok)
	}
	if strings.Contains(html, "Objekt nema pridružen vodomjer") {
		t.Error("nasip bez ikakve letve pokazuje i staru poruku o vodomjeru na objektu")
	}

	svoja := o.objekt("Nasip sa svojom letvom", models.StructureKindEmbankment, letva, "")
	o.dionica("P.1.5", nasipNa(svoja, []*models.Station{letva},
		models.GaugeItem{StationName: "Srednji Primjerak", PrepCm: "+250", RegularCm: "+350"}))
	html = o.stranica(svoja)
	na, mj := strings.Index(html, "Vodomjer na objektu"), strings.Index(html, `id="mjerodavni-vodomjeri"`)
	if na < 0 || mj < na {
		t.Fatalf("nasip s vlastitom letvom: blok na objektu %d, mjerodavni %d", na, mj)
	}
	blok = blokMjerodavnih(t, html)
	if !strings.Contains(blok, "P.1.5") {
		t.Error("mjerodavna letva nasipa s vlastitom letvom nema oznaku dionice")
	}
	// ista letva u oba bloka ima iste pragove, one sa same letve
	if !strings.Contains(html[na:mj], "+200 cm") || !strings.Contains(blok, "+200 cm") || strings.Contains(blok, "+250 cm") {
		t.Errorf("ista letva u dva bloka nema iste pragove\n%s", blok)
	}
}

// Crpna stanica ostaje kakva je bila: opis pa vodomjer na objektu, bez
// mjerodavnih vodomjera iz dionica
func TestCrpnaStanicaBezMjerodavnih(t *testing.T) {
	o := novaOkolinaNasipa(t)
	cs := o.objekt("CS Primjerovo", models.StructureKindPumpingStation, nil, "Dvije crpke.")
	html := o.stranica(cs)
	if strings.Contains(html, "mjerodavni-vodomjeri") {
		t.Error("crpna stanica dobila blok mjerodavnih vodomjera")
	}
	opis, vod := strings.Index(html, "Detaljni opis"), strings.Index(html, "Objekt nema pridružen vodomjer")
	if opis < 0 || vod < opis {
		t.Errorf("crpna stanica: opis %d, poruka o vodomjeru %d", opis, vod)
	}
}

// Nasip se u dionici prepoznaje samo po vezi na registar; redak s istim
// nazivom bez veze ne ulazi
func TestPoddioniceNasipaSamoPoVezi(t *testing.T) {
	id := uuid.NewString()
	sec := models.Section{Parts: []models.SectionPart{
		{Seq: 1, Embankments: []models.PartEmbankment{{Name: "Nasip Primjerica lijevo"}}},
		{Seq: 2, Embankments: []models.PartEmbankment{{StructureID: id, Name: "Nasip Primjerica lijevo"}}},
	}}
	if p := poddioniceNasipa(sec, id); len(p) != 1 || p[0].Seq != 2 {
		t.Errorf("poddionice nasipa: %+v", p)
	}
	if !imaMjerodavneVodomjere(models.StructureKindDam) || imaMjerodavneVodomjere(models.StructureKindSluice) {
		t.Error("mjerodavni vodomjeri vrijede za nasip i branu, ne za ustavu")
	}
	if (OznakaDionice{Code: "B.34.19"}).Oznaka() != "B.34.19" || (OznakaDionice{Code: "B.34.19", Poddionica: 1}).Oznaka() != "B.34.19, poddionica 1" {
		t.Error("oznaka dionice nije složena kako treba")
	}
	var nema *MjerodavniNasipa
	if nema.Ima() {
		t.Error("prazan skup kaže da ima mjerodavne")
	}
	h := &StructuresHandler{}
	if len(h.letveDionice(context.Background(), "P.1.1")) != 0 {
		t.Error("bez servisa letvi nema letava dionice")
	}
}
