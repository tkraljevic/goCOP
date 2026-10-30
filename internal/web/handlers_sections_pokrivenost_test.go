package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// okolinaDionica je izmišljeni sektor P s dionicama P.1.1–P.1.3, P.2.1 i
// E.3.1, letvama i naseljima. Pero Perić je globalni administrator, Ana Anić
// piše u području 2, a Marko Markić nema prava.
type okolinaDionica struct {
	h        *SectionsHandler
	exec     func(string) error
	epizode  *repository.EpisodeRepository
	korisnci *repository.UserRepository
	pero     *models.User
	peroOvl  *models.UserPermissions
	anaOvl   *models.UserPermissions
	markOvl  *models.UserPermissions
}

func novaOkolinaDionica(t *testing.T) *okolinaDionica {
	t.Helper()
	db.KoristiIzmisljenePodatke(t)
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "dionice.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if _, err := db.SeedInitialData(baza); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "cop-primjerovo")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	sectionRepo := repository.NewSectionRepository(baza, rec)
	sections := service.NewSectionService(sectionRepo, service.NewSSEBroker())
	stationRepo := repository.NewStationRepository(baza, rec)
	stations := service.NewStationService(stationRepo, sections, service.NewSSEBroker())
	territories := service.NewTerritoryService(repository.NewTerritoryRepository(baza, rec), sections)
	epRepo := repository.NewEpisodeRepository(baza, rec)

	h := NewSectionsHandler(sections, users, predlozakStranice(t, "sections.html"))
	h.SetPageTemplates(predlozakStranice(t, "section_detail.html"), predlozakStranice(t, "section_form.html"), stations, territories)
	h.SetStructureService(service.NewStructureService(repository.NewStructureRepository(baza, rec)))
	h.SetWatercourseService(service.NewWatercourseService(repository.NewWatercourseRepository(baza, rec)))
	h.episodeService = service.NewEpisodeService(epRepo, repository.NewReadingRepository(baza, rec), stationRepo)

	pero := &models.User{ID: uuid.New(), FullName: "Pero Perić", IsGlobalAdmin: true}
	ana := models.User{ID: uuid.New(), FullName: "Ana Anić"}
	marko := models.User{ID: uuid.New(), FullName: "Marko Markić"}
	return &okolinaDionica{h: h, epizode: epRepo, korisnci: userRepo, pero: pero,
		exec:    func(q string) error { _, err := baza.Exec(q); return err },
		peroOvl: &models.UserPermissions{User: *pero, IsGlobalAdmin: true},
		anaOvl:  &models.UserPermissions{User: ana, AllowedAreas: map[int]bool{2: true}},
		markOvl: &models.UserPermissions{User: marko},
	}
}

// zovi šalje zahtjev rukovatelju u ime osobe; p nil je zahtjev bez prijave
func (o *okolinaDionica) zovi(h http.HandlerFunc, p *models.UserPermissions, r *http.Request, put map[string]string) *httptest.ResponseRecorder {
	for k, v := range put {
		r.SetPathValue(k, v)
	}
	var u *models.User
	if p != nil {
		k := p.User
		u = &k
	}
	w := httptest.NewRecorder()
	h(w, zahtjevSKorisnikom(r, u, p))
	return w
}

func dohvati(putanja string) *http.Request { return httptest.NewRequest(http.MethodGet, putanja, nil) }

// Registar dionica filtrira po sektoru, području i upitu; dodavanje se nudi
// samo onome tko negdje smije pisati, a baza koja se ne čita javlja grešku.
func TestRegistarDionicaFiltriraIPrava(t *testing.T) {
	o := novaOkolinaDionica(t)
	w := o.zovi(o.h.ShowSections, o.peroOvl, dohvati("/sections"), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("registar: %d %s", w.Code, w.Body.String())
	}
	for _, sifra := range []string{"P.1.1", "P.1.2", "P.1.3", "P.2.1", "E.3.1", "Nova dionica"} {
		if !strings.Contains(w.Body.String(), sifra) {
			t.Errorf("registar nema %q", sifra)
		}
	}
	w = o.zovi(o.h.ShowSections, o.markOvl, dohvati("/sections?sector=E"), nil)
	if html := w.Body.String(); !strings.Contains(html, "E.3.1") || strings.Contains(html, "P.1.1") || strings.Contains(html, "Nova dionica") {
		t.Error("sektor E mora pokazati samo E.3.1, bez gumba za novu dionicu")
	}
	w = o.zovi(o.h.ShowSections, o.anaOvl, dohvati("/sections?area=2"), nil)
	if html := w.Body.String(); !strings.Contains(html, "P.2.1") || strings.Contains(html, "P.1.2") || !strings.Contains(html, "Nova dionica") {
		t.Error("područje 2 mora pokazati P.2.1, a Ani gumb za novu dionicu")
	}
	if err := o.exec(`ALTER TABLE sections RENAME TO nema_sections`); err != nil {
		t.Fatal(err)
	}
	w = o.zovi(o.h.ShowSections, o.peroOvl, dohvati("/sections"), nil)
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Greška pri dohvatu dionica: ") {
		t.Errorf("baza bez dionica: %d %s", w.Code, w.Body.String())
	}
}

// API dionice vraća dionicu s pravom uređivanja za osobu koja pita; šifra
// stiže iz putanje ili upita, a nepoznata daje 404.
func TestDionicaAPI(t *testing.T) {
	o := novaOkolinaDionica(t)
	procitaj := func(p *models.UserPermissions, r *http.Request, put map[string]string) (int, map[string]any) {
		w := o.zovi(o.h.HandleGetSectionAPI, p, r, put)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	if kod, d := procitaj(o.peroOvl, dohvati("/api/sections/P.1.1"), map[string]string{"code": "P.1.1"}); kod != http.StatusOK || d["code"] != "P.1.1" || d["can_edit"] != true {
		t.Errorf("administrator: %d %v %v", kod, d["code"], d["can_edit"])
	}
	if kod, d := procitaj(o.markOvl, dohvati("/api/sections?code=P.1.2"), nil); kod != http.StatusOK || d["code"] != "P.1.2" || d["can_edit"] != false {
		t.Errorf("bez prava, šifra iz upita: %d %v %v", kod, d["code"], d["can_edit"])
	}
	if kod, d := procitaj(o.peroOvl, dohvati("/api/sections/X.9.9"), map[string]string{"code": "X.9.9"}); kod != http.StatusNotFound || d["error"] != "Dionica nije pronađena" {
		t.Errorf("nepoznata dionica: %d %v", kod, d)
	}
}

// Stranica dionice skuplja letve, naselja, epizode obrane s imenima onih koji
// su je proglasili i prekinuli, i stanje iz akata; dionice koje nema daje 404.
func TestStranicaDioniceSkupljaVeze(t *testing.T) {
	o := novaOkolinaDionica(t)
	ctx := context.Background()
	ivo := &models.User{ID: uuid.New(), Username: "iivic2", FullName: "Ivo Ivić", IsActive: true}
	if err := o.korisnci.CreateUser(ivo, nil); err != nil {
		t.Fatal(err)
	}
	pocetak := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)
	kraj := pocetak.Add(72 * time.Hour)
	for _, e := range []*models.DefenseEpisode{
		{ID: uuid.New(), SectionCode: "P.1.1", StartedAt: pocetak, EndedAt: &kraj, Phase: models.PhaseRegular, DeclaredBy: ivo.ID.String(), EndedBy: "nije-broj", Basis: models.BasisThreshold},
		{ID: uuid.New(), SectionCode: "P.1.1", StartedAt: pocetak.AddDate(0, 0, 10), Phase: models.PhaseRegular, DeclaredBy: ivo.ID.String(), Basis: models.BasisThreshold},
	} {
		if err := o.epizode.SaveEpisode(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	data := o.h.pageData(zahtjevSKorisnikom(dohvati("/sections/P.1.1"), o.pero, o.peroOvl))
	r := dohvati("/sections/P.1.1")
	r.SetPathValue("code", "P.1.1")
	if !o.h.napuniDionicu(r, &data) {
		t.Fatal("dionica P.1.1 nije nađena")
	}
	if !data.CanEdit || len(data.Parts) != 1 || data.Gauge == nil || data.Gauge.Name == "" {
		t.Fatalf("dionica: uređivanje %v, poddionica %d, letva %+v", data.CanEdit, len(data.Parts), data.Gauge)
	}
	p := data.Parts[0]
	if len(p.Stations) != 1 || len(p.LetveBlokovi) != 1 || p.LetveBlokovi[0].Ponovljena {
		t.Errorf("letve poddionice: %d, blokova %d", len(p.Stations), len(p.LetveBlokovi))
	}
	if len(p.Criteria) != 0 {
		t.Errorf("letva iz dokumentacije je već postaja, a ostala je u kriterijima: %+v", p.Criteria)
	}
	naselja := 0
	for _, red := range p.Rows {
		naselja += len(red.Territories)
	}
	if naselja+len(p.Territories) != 2 {
		t.Errorf("dionica P.1.1 štiti dva naselja, na stranici %d", naselja+len(p.Territories))
	}
	if len(data.Episodes) != 2 || data.OpenEpisode == nil || data.OpenEpisode.DeclaredByName != "Ivo Ivić" {
		t.Fatalf("epizode: %d, otvorena %+v", len(data.Episodes), data.OpenEpisode)
	}
	for _, e := range data.Episodes {
		if !e.IsOpen() && (e.DeclaredByName != "Ivo Ivić" || e.EndedByName != "") {
			t.Errorf("zatvorena epizoda: proglasio %q, prekinuo %q", e.DeclaredByName, e.EndedByName)
		}
	}
	if data.StanjeObrane.Traje() || data.NajavljeniAkti != nil {
		t.Error("bez akata stanje obrane mora biti prazno")
	}

	// stanje iz akata: kad se ne čita, prazno; inače ono što akti kažu
	o.h.stanjeObrane = func(context.Context, string, string) (models.StanjeObrane, []models.Akt, error) {
		return models.StanjeObrane{IzAkata: true, Aktivni: []models.AktivniStadij{{Stupanj: models.PhaseRegular}}}, []models.Akt{{}}, errors.New("akti su zaključani")
	}
	if s, n := o.h.obranaIzAkata(ctx, &data.Section); s.Traje() || s.IzAkata || len(s.Aktivni) != 0 || n != nil {
		t.Error("greška akata mora dati prazno stanje")
	}
	var traziSektor, traziDionicu string
	o.h.stanjeObrane = func(_ context.Context, sektor, dionica string) (models.StanjeObrane, []models.Akt, error) {
		traziSektor, traziDionicu = sektor, dionica
		return models.StanjeObrane{IzAkata: true, Aktivni: []models.AktivniStadij{{Stupanj: models.PhaseRegular}}}, []models.Akt{{}}, nil
	}
	w := o.zovi(o.h.ShowSection, o.peroOvl, dohvati("/sections/P.1.1"), map[string]string{"code": "P.1.1"})
	if w.Code != http.StatusOK || traziSektor != "P" || traziDionicu != "P.1.1" {
		t.Errorf("stranica dionice: %d, akti traženi za %q/%q", w.Code, traziSektor, traziDionicu)
	}
	if w := o.zovi(o.h.ShowSection, o.peroOvl, dohvati("/sections/X.9.9"), map[string]string{"code": "X.9.9"}); w.Code != http.StatusNotFound {
		t.Errorf("nepoznata dionica: %d", w.Code)
	}
}

// Kartica dionice u Excelu nosi zaglavlje sektora i područja; nepoznata
// dionica daje 404.
func TestIzvozKarticeDionice(t *testing.T) {
	o := novaOkolinaDionica(t)
	w := o.zovi(o.h.IzvoziDionicu, o.markOvl, dohvati("/sections/P.1.1/dionica.xlsx"), map[string]string{"code": "P.1.1"})
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Disposition"), "dionica-P-1-1.xlsx") {
		t.Fatalf("izvoz: %d %q", w.Code, w.Header().Get("Content-Disposition"))
	}
	redci, err := procitajXLSX(w.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	var sve []string
	for _, r := range redci {
		sve = append(sve, strings.Join(r, "|"))
	}
	list := strings.Join(sve, "\n")
	for _, treba := range []string{"KARTICA ŠTIĆENE DIONICE P.1.1", "VGO za Primjerski sliv, Primjerovo", "BP 1 Mali sliv Primjerica"} {
		if !strings.Contains(list, treba) {
			t.Errorf("kartica nema %q\n%s", treba, list)
		}
	}
	if w := o.zovi(o.h.IzvoziDionicu, o.markOvl, dohvati("/sections/X.9.9/dionica.xlsx"), map[string]string{"code": "X.9.9"}); w.Code != http.StatusNotFound {
		t.Errorf("izvoz nepoznate dionice: %d", w.Code)
	}
}

// Ime djelatnika se traži jednom i pamti; prazan ili neispravan broj i
// nepoznata osoba daju prazno ime.
func TestImeDjelatnikaPamti(t *testing.T) {
	o := novaOkolinaDionica(t)
	iva := &models.User{ID: uuid.New(), Username: "iivic3", FullName: "Iva Ivić", IsActive: true}
	if err := o.korisnci.CreateUser(iva, nil); err != nil {
		t.Fatal(err)
	}
	cache := map[string]string{}
	if ime := o.h.imeDjelatnika(cache, iva.ID.String()); ime != "Iva Ivić" {
		t.Errorf("ime: %q", ime)
	}
	if ime, ok := cache[iva.ID.String()]; !ok || ime != "Iva Ivić" {
		t.Errorf("ime nije zapamćeno: %q %v", ime, ok)
	}
	cache[iva.ID.String()] = "Zapamćeno"
	if ime := o.h.imeDjelatnika(cache, iva.ID.String()); ime != "Zapamćeno" {
		t.Errorf("drugi put ime mora doći iz pamćenja, a ne iz baze: %q", ime)
	}
	nepoznat := uuid.NewString()
	for _, id := range []string{"", "nije-broj", nepoznat} {
		if ime := o.h.imeDjelatnika(cache, id); ime != "" {
			t.Errorf("%q: ime %q", id, ime)
		}
	}
	// i prazno ime se pamti, da se ista osoba ne traži opet; prazan broj ne
	for _, id := range []string{"nije-broj", nepoznat} {
		if ime, ok := cache[id]; !ok || ime != "" {
			t.Errorf("%q nije zapamćen kao prazno ime: %q %v", id, ime, ok)
		}
	}
	if _, ok := cache[""]; ok {
		t.Error("prazan broj se ne pamti")
	}
	if ime := (&SectionsHandler{}).imeDjelatnika(map[string]string{}, iva.ID.String()); ime != "" {
		t.Errorf("bez registra osoba: %q", ime)
	}
}

// Obrazac postojeće dionice otvara samo onaj tko je smije uređivati, a novu
// onaj tko negdje smije pisati; nova u odabranom području dobiva sljedeću
// šifru i sektor područja.
func TestObrazacDionicePrava(t *testing.T) {
	o := novaOkolinaDionica(t)
	if w := o.zovi(o.h.ShowSectionForm, o.peroOvl, dohvati("/sections/P.1.1/edit"), map[string]string{"code": "P.1.1"}); w.Code != http.StatusOK ||
		!strings.Contains(w.Body.String(), `"code":"P.1.1"`) {
		t.Errorf("obrazac postojeće dionice: %d", w.Code)
	}
	if w := o.zovi(o.h.ShowSectionForm, o.anaOvl, dohvati("/sections/P.1.1/edit"), map[string]string{"code": "P.1.1"}); w.Code != http.StatusForbidden {
		t.Errorf("Ana uređuje dionicu tuđeg područja: %d", w.Code)
	}
	if w := o.zovi(o.h.ShowSectionForm, o.peroOvl, dohvati("/sections/X.9.9/edit"), map[string]string{"code": "X.9.9"}); w.Code != http.StatusNotFound {
		t.Errorf("obrazac nepoznate dionice: %d", w.Code)
	}
	if w := o.zovi(o.h.ShowSectionForm, o.markOvl, dohvati("/sections/new"), nil); w.Code != http.StatusForbidden {
		t.Errorf("nova dionica bez prava: %d", w.Code)
	}
	if w := o.zovi(o.h.ShowSectionForm, nil, dohvati("/sections/new"), nil); w.Code != http.StatusForbidden {
		t.Errorf("nova dionica bez prijave: %d", w.Code)
	}
	w := o.zovi(o.h.ShowSectionForm, o.peroOvl, dohvati("/sections/new?area=1"), nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `value="P.1.4"`) || !strings.Contains(w.Body.String(), `"sector_id":"P"`) {
		t.Errorf("nova dionica u području 1: %d, nema prijedloga P.1.4 ili sektora P", w.Code)
	}
	if w := o.zovi(o.h.ShowSectionForm, o.anaOvl, dohvati("/sections/new"), nil); w.Code != http.StatusOK {
		t.Errorf("Ana otvara obrazac nove dionice: %d", w.Code)
	}
}

// Nova dionica i izmjena vraćaju na dionicu s porukom (sektor E, jer šifra
// dionice dopušta samo sektore A–F); „dalje” vraća na
// prazan obrazac istog područja. Nečitljive poddionice, odbijen upis i
// obrazac koji se ne da pročitati vraćaju poruku.
func TestUpisIIzmjenaDionice(t *testing.T) {
	o := novaOkolinaDionica(t)
	posalji := func(h http.HandlerFunc, p *models.UserPermissions, tijelo string) string {
		w := o.zovi(h, p, obicniObrazac("/sections/create", tijelo), nil)
		if w.Code != http.StatusSeeOther {
			t.Fatalf("odgovor %d %s", w.Code, w.Body.String())
		}
		return mustUnescape(w.Header().Get("Location"))
	}
	nova := url.Values{"code": {"e.3.7"}, "area_id": {"3"}, "sector_id": {"e"}, "notes": {" Ključ kod vodočuvara. "},
		"parts_json": {`[{"bank":" ld ","extent":" od mosta ","watercourse_code":" rijeka-primjerica ","objects":[{"name":"  "},{"name":" CS Probni "}],"embankments":[{"name":""},{"name":" Nasip "}]}]`}}
	if l := posalji(o.h.HandleCreateSection, o.peroOvl, nova.Encode()); l != "/sections/E.3.7?success=Dionica E.3.7 je upisana." {
		t.Fatalf("nova dionica: %s", l)
	}
	sec, err := o.h.sectionService.GetSectionWithDetails("E.3.7")
	if err != nil || sec == nil {
		t.Fatalf("upisana dionica: %v", err)
	}
	if sec.SectorID != "E" || sec.Notes != "Ključ kod vodočuvara." || len(sec.Parts) != 1 {
		t.Fatalf("upisano: sektor %q, napomena %q, poddionica %d", sec.SectorID, sec.Notes, len(sec.Parts))
	}
	if p := sec.Parts[0]; p.Bank != "LD" || p.WatercourseCode != "rijeka-primjerica" || len(p.Objects) != 1 || len(p.Embankments) != 1 || p.Seq != 1 {
		t.Errorf("poddionica: %+v", p)
	}

	nova.Set("code", "E.3.8")
	nova.Set("dalje", "1")
	if l := posalji(o.h.HandleCreateSection, o.peroOvl, nova.Encode()); l != "/sections/new?area=3&success=Dionica E.3.8 je upisana. Na redu je sljedeća." {
		t.Errorf("upis u nizu: %s", l)
	}
	if l := posalji(o.h.HandleCreateSection, o.peroOvl, nova.Encode()); !strings.HasPrefix(l, "/sections/new?area=3&error=dionica sa šifrom 'E.3.8' već postoji") {
		t.Errorf("ista šifra drugi put: %s", l)
	}
	if l := posalji(o.h.HandleCreateSection, o.peroOvl, "code=E.3.9&area_id=3&parts_json=%5B%7B"); !strings.HasPrefix(l, "/sections/new?error=Poddionice nisu čitljive: ") {
		t.Errorf("nečitljive poddionice: %s", l)
	}
	if l := posalji(o.h.HandleCreateSection, o.peroOvl, "code=%zz"); l != "/sections/new?error=Neispravan zahtjev" {
		t.Errorf("nečitljiv obrazac: %s", l)
	}

	izmjena := url.Values{"code": {"E.3.7"}, "area_id": {"3"}, "sector_id": {"E"}, "notes": {"Nova napomena"}, "parts_json": {`[{"bank":"D","watercourse_code":"kanal-probni"}]`}}
	if l := posalji(o.h.HandleUpdateSection, o.peroOvl, izmjena.Encode()); l != "/sections/E.3.7?success=Izmjene su spremljene." {
		t.Errorf("izmjena: %s", l)
	}
	if sec, _ := o.h.sectionService.GetSectionWithDetails("E.3.7"); sec == nil || sec.Notes != "Nova napomena" || len(sec.Parts) != 1 || sec.Parts[0].Bank != "D" || sec.Parts[0].WatercourseCode != "kanal-probni" {
		t.Errorf("izmjena nije spremljena: %+v", sec)
	}
	if l := posalji(o.h.HandleUpdateSection, o.markOvl, izmjena.Encode()); !strings.HasPrefix(l, "/sections/E.3.7/edit?error=") {
		t.Errorf("izmjena bez prava: %s", l)
	}
	if l := posalji(o.h.HandleUpdateSection, o.peroOvl, "code=E.3.7&parts_json=nije"); !strings.HasPrefix(l, "/sections?error=Poddionice nisu čitljive: ") {
		t.Errorf("izmjena s nečitljivim poddionicama: %s", l)
	}
	if l := posalji(o.h.HandleUpdateSection, o.peroOvl, "code=%zz"); l != "/sections?error=Neispravan zahtjev" {
		t.Errorf("izmjena s nečitljivim obrascem: %s", l)
	}
}
