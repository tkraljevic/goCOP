package web

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// okolinaOdrzavanja je izmišljeni sektor P (područja 1 i 2 u sektoru P, 3 u
// sektoru E) s registrom voda i objekata. Pero Perić je globalni
// administrator, Ana Anić piše samo u području 2, a Marko Markić nigdje.
type okolinaOdrzavanja struct {
	h       *MaintenanceHandler
	odrz    *repository.MaintenanceRepository
	pero    *models.User
	peroOvl *models.UserPermissions
	anaOvl  *models.UserPermissions
	markOvl *models.UserPermissions
}

func novaOkolinaOdrzavanja(t *testing.T) (*okolinaOdrzavanja, func(string) error) {
	t.Helper()
	db.KoristiIzmisljenePodatke(t)
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "odrzavanje.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.SeedInitialData(baza); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "cop-primjerovo")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	odrz := repository.NewMaintenanceRepository(baza, rec)
	vodeRepo := repository.NewWatercourseRepository(baza, rec)
	objektiRepo := repository.NewStructureRepository(baza, rec)
	m := service.NewMaintenanceService(odrz, vodeRepo, objektiRepo)
	h := NewMaintenanceHandler(m, users, service.NewWatercourseService(vodeRepo), service.NewStructureService(objektiRepo),
		predlozakStranice(t, "odrzavanje.html"))
	pero := &models.User{ID: uuid.New(), FullName: "Pero Perić", IsGlobalAdmin: true}
	ana := models.User{ID: uuid.New(), FullName: "Ana Anić"}
	marko := models.User{ID: uuid.New(), FullName: "Marko Markić"}
	o := &okolinaOdrzavanja{h: h, odrz: odrz, pero: pero,
		peroOvl: &models.UserPermissions{User: *pero, IsGlobalAdmin: true},
		anaOvl:  &models.UserPermissions{User: ana, AllowedAreas: map[int]bool{2: true}},
		markOvl: &models.UserPermissions{User: marko},
	}
	return o, func(q string) error { _, err := baza.Exec(q); return err }
}

// posalji šalje obrazac rukovatelju i vraća odredište preusmjeravanja
func (o *okolinaOdrzavanja) posalji(h http.HandlerFunc, p *models.UserPermissions, putanja string, forma url.Values, put map[string]string) string {
	r := obicniObrazac(putanja, forma.Encode())
	for k, v := range put {
		r.SetPathValue(k, v)
	}
	u := p.User
	w := odgovor(h, r, &u, p)
	if w.Code != http.StatusSeeOther {
		return fmt.Sprintf("kod %d: %s", w.Code, w.Body.String())
	}
	return mustUnescape(w.Header().Get("Location"))
}

// stranica slaže podatke stranice održavanja za upit i osobu
func (o *okolinaOdrzavanja) stranica(t *testing.T, p *models.UserPermissions, upit string) MaintenancePageData {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/odrzavanje?"+upit, nil)
	var u *models.User
	if p != nil {
		k := p.User
		u = &k
	}
	d, err := o.h.pageFor(zahtjevSKorisnikom(r, u, p))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// Područje se uzima iz upita ili putanje; bez njega prvo u kojem osoba ima
// ovlasti, a bez ovlasti prvo uopće. Nepoznat broj stranica kaže, a osoba
// bez prava vidi popis, ali ne i ono što služi za uređivanje.
func TestOdrzavanjeBiraPodrucjeIPrava(t *testing.T) {
	o, _ := novaOkolinaOdrzavanja(t)
	if d := o.stranica(t, o.peroOvl, "area=1"); d.Area == nil || d.Area.ID != 1 || !d.CanEdit || len(d.Candidates) == 0 || d.ActiveNav != "maintenance" {
		t.Errorf("administrator u području 1: %+v", d.Area)
	}
	if d := o.stranica(t, o.anaOvl, ""); d.Area == nil || d.Area.ID != 2 || !d.CanEdit {
		t.Errorf("Ana bez zadanog područja mora dobiti svoje područje 2, dobila %+v", d.Area)
	}
	if d := o.stranica(t, nil, ""); d.Area == nil || d.Area.ID != d.Areas[0].ID {
		t.Errorf("bez ovlasti prvo područje: %+v", d.Area)
	}
	d := o.stranica(t, o.markOvl, "area=1")
	if d.Area == nil || d.CanEdit || d.Candidates != nil || d.Embankments != nil {
		t.Errorf("Marko ne smije uređivati, a dobio je CanEdit=%v kandidata %d", d.CanEdit, len(d.Candidates))
	}
	if d := o.stranica(t, o.peroOvl, "area=99"); d.Area != nil || d.ErrorMessage != "Nema branjenog područja s tim brojem." {
		t.Errorf("nepoznato područje: %+v %q", d.Area, d.ErrorMessage)
	}

	// broj područja iz putanje
	r := httptest.NewRequest(http.MethodGet, "/odrzavanje/3", nil)
	r.SetPathValue("area", "3")
	if a, _ := o.h.area(r, o.markOvl); a == nil || a.ID != 3 {
		t.Errorf("područje iz putanje: %+v", a)
	}

	w := odgovor(o.h.ShowMaintenance, httptest.NewRequest(http.MethodGet, "/odrzavanje?area=1", nil), o.pero, o.peroOvl)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `id="nova-lokacija"`) {
		t.Errorf("stranica administratora: %d, nema obrasca nove lokacije", w.Code)
	}
	marko := o.markOvl.User
	w = odgovor(o.h.ShowMaintenance, httptest.NewRequest(http.MethodGet, "/odrzavanje?area=1", nil), &marko, o.markOvl)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), `id="nova-lokacija"`) {
		t.Errorf("stranica bez prava: %d, ili nudi obrazac nove lokacije", w.Code)
	}
}

// Lokacija se ručno dodaje, veže na vodu ili objekt iz registra i uklanja;
// popis je složen po programu, redu i vrsti, a lokacije bez veze se broje.
func TestOdrzavanjeLokacije(t *testing.T) {
	o, _ := novaOkolinaOdrzavanja(t)
	dodaj := func(p *models.UserPermissions, area string, polja ...string) string {
		v := url.Values{"area": {area}}
		for i := 0; i+1 < len(polja); i += 2 {
			v.Set(polja[i], polja[i+1])
		}
		return o.posalji(o.h.HandleAddWater, p, "/odrzavanje/lokacije", v, nil)
	}
	if l := dodaj(o.peroOvl, "1", "name", "  Potok   Primjerni ", "order", models.WaterOrderSecond, "kind", models.MaintenanceKindWatercourse); l != "/odrzavanje?area=1&success=Lokacija „Potok Primjerni“ je dodana u popis." {
		t.Errorf("dodavanje: %s", l)
	}
	if l := dodaj(o.peroOvl, "1", "name", "Rijeka Primjerica", "order", models.WaterOrderFirst, "group", models.WaterGroupInterstate,
		"kind", models.MaintenanceKindWatercourse, "target", "rijeka-primjerica"); !strings.Contains(l, "success=") {
		t.Errorf("dodavanje s vezom: %s", l)
	}
	if l := dodaj(o.peroOvl, "1", "name", "Kanal K-9", "order", models.WaterOrderThird, "kind", models.MaintenanceKindDrainage); !strings.Contains(l, "success=") {
		t.Errorf("kanal III. reda: %s", l)
	}
	for _, s := range []struct {
		opis, area string
		p          *models.UserPermissions
		polja      []string
		ceka       string
	}{
		{"bez naziva", "1", o.peroOvl, []string{"name", " ", "order", "II", "kind", models.MaintenanceKindWatercourse}, "/odrzavanje?area=1&error=naziv lokacije je obvezan#nova-lokacija"},
		{"isti naziv", "1", o.peroOvl, []string{"name", "Potok Primjerni", "order", "II", "kind", models.MaintenanceKindWatercourse}, "error=lokacija s tim nazivom već je u popisu"},
		{"nepoznata šifra", "1", o.peroOvl, []string{"name", "Potok Treći", "order", "II", "kind", models.MaintenanceKindWatercourse, "target", "nema-je"}, "error=nema vode ni objekta s tom šifrom"},
		{"tuđe područje", "1", o.anaOvl, []string{"name", "Potok Anin", "order", "II", "kind", models.MaintenanceKindWatercourse}, "error=nemate pravo uređivati popis lokacija tog područja"},
		{"nepoznato područje", "99", o.peroOvl, []string{"name", "Potok"}, "/odrzavanje?error=Nepoznato područje"},
	} {
		if l := dodaj(s.p, s.area, s.polja...); !strings.Contains(l, s.ceka) {
			t.Errorf("%s: %s", s.opis, l)
		}
	}

	d := o.stranica(t, o.peroOvl, "area=1")
	if d.WaterCount != 3 || d.Unlinked != 2 {
		t.Fatalf("u popisu %d lokacija, bez veze %d; očekivano 3 i 2", d.WaterCount, d.Unlinked)
	}
	var skupine []string
	for _, g := range d.Groups {
		for _, k := range g.Kinds {
			skupine = append(skupine, g.Label+" / "+k.Label)
		}
	}
	if got := strings.Join(skupine, "; "); got != "A.02 · Vode I. reda – Međudržavne vode / Vodotoci; A.02 · Vode II. reda / Vodotoci; A.03 · Vode III. reda / Osnovne melioracijske građevine za odvodnju" {
		t.Errorf("skupine popisa: %s", got)
	}

	potok := repository.MaintainedWaterID(1, "Potok Primjerni")
	veza := func(p *models.UserPermissions, id, cilj string) string {
		return o.posalji(o.h.HandleLinkWater, p, "/odrzavanje/lokacije/"+id+"/veza", url.Values{"area": {"1"}, "target": {cilj}}, map[string]string{"id": id})
	}
	if l := veza(o.peroOvl, potok, "P-CS-PROBNI"); l != "/odrzavanje?area=1&success=Veza je spremljena." {
		t.Errorf("veza na objekt: %s", l)
	}
	if m, _ := o.odrz.GetWater(context.Background(), potok); m == nil || m.StructureID == "" || m.WatercourseCode != "" {
		t.Errorf("lokacija nije vezana na objekt: %+v", m)
	}
	if l := veza(o.peroOvl, uuid.NewString(), "rijeka-primjerica"); !strings.Contains(l, "error=lokacija nije pronađena") {
		t.Errorf("veza nepostojeće lokacije: %s", l)
	}
	if l := veza(o.anaOvl, potok, ""); !strings.Contains(l, "error=nemate pravo") {
		t.Errorf("veza bez prava: %s", l)
	}
	if l := o.posalji(o.h.HandleLinkWater, o.peroOvl, "/", url.Values{"area": {"99"}}, map[string]string{"id": potok}); l != "/odrzavanje?error=Nepoznato područje" {
		t.Errorf("veza u nepoznatom području: %s", l)
	}
	if d := o.stranica(t, o.peroOvl, "area=1"); d.Unlinked != 1 {
		t.Errorf("nakon vezanja bez veze %d, očekivana jedna", d.Unlinked)
	}

	brisi := func(p *models.UserPermissions, area, id string) string {
		return o.posalji(o.h.HandleDeleteWater, p, "/odrzavanje/lokacije/"+id+"/delete", url.Values{"area": {area}}, map[string]string{"id": id})
	}
	// lokacija područja 1 ne briše se kroz područje 2, ni s pravom u njemu
	if l := brisi(o.anaOvl, "2", potok); !strings.Contains(l, "error=lokacija nije pronađena") {
		t.Errorf("brisanje kroz tuđe područje: %s", l)
	}
	if l := brisi(o.peroOvl, "1", potok); l != "/odrzavanje?area=1&success=Lokacija je uklonjena iz popisa." {
		t.Errorf("brisanje: %s", l)
	}
	if l := brisi(o.peroOvl, "99", potok); l != "/odrzavanje?error=Nepoznato područje" {
		t.Errorf("brisanje u nepoznatom području: %s", l)
	}
	if d := o.stranica(t, o.peroOvl, "area=1"); d.WaterCount != 2 {
		t.Errorf("nakon brisanja %d lokacija", d.WaterCount)
	}
}

// Stavka radova dodaje se na kraj, mijenja i briše; isključena se ne
// prikazuje dok se ne zatraže sve.
func TestOdrzavanjeStavkeRadova(t *testing.T) {
	o, _ := novaOkolinaOdrzavanja(t)
	spremi := func(p *models.UserPermissions, id string, polja ...string) string {
		v := url.Values{"area": {"1"}}
		for i := 0; i+1 < len(polja); i += 2 {
			v.Set(polja[i], polja[i+1])
		}
		putanja := "/odrzavanje/stavke"
		var put map[string]string
		if id != "" {
			putanja += "/" + id
			put = map[string]string{"id": id}
		}
		return o.posalji(o.h.HandleSaveItem, p, putanja, v, put)
	}
	if l := spremi(o.peroOvl, "", "number", "PR-1", "description", "Košnja trave", "unit", "ha"); l != "/odrzavanje?area=1&success=Stavka je dodana.#stavke" {
		t.Fatalf("nova stavka: %s", l)
	}
	if l := spremi(o.peroOvl, "", "number", "PR-2", "description", "Čišćenje šaša", "unit", "m2"); !strings.Contains(l, "success=Stavka je dodana.") {
		t.Fatalf("druga stavka: %s", l)
	}
	stavke, err := o.odrz.ListItems(context.Background(), 1, true)
	if err != nil || len(stavke) != 2 {
		t.Fatalf("stavke: %v %d", err, len(stavke))
	}
	if stavke[0].SortOrder >= stavke[1].SortOrder || stavke[0].Origin != models.WorkItemOriginManual {
		t.Errorf("nova stavka ide na kraj i ručna je: %+v", stavke)
	}
	id := stavke[1].ID
	if l := spremi(o.peroOvl, id, "number", "PR-2", "description", "Čišćenje šaša i trske", "unit", "m2", "active", "0"); l != "/odrzavanje?area=1&success=Stavka je spremljena.#stavke" {
		t.Errorf("izmjena: %s", l)
	}
	for _, s := range []struct {
		opis  string
		p     *models.UserPermissions
		id    string
		polja []string
		ceka  string
	}{
		{"bez opisa", o.peroOvl, "", []string{"unit", "ha"}, "/odrzavanje?area=1&error=opis stavke je obvezan"},
		{"bez jedinice", o.peroOvl, "", []string{"description", "Košnja"}, "error=jedinica mjere je obvezna"},
		{"nepostojeća", o.peroOvl, uuid.NewString(), []string{"description", "Košnja", "unit", "ha"}, "error=stavka nije pronađena"},
		{"bez prava", o.markOvl, "", []string{"description", "Košnja", "unit", "ha"}, "error=nemate pravo uređivati stavke tog područja"},
	} {
		if l := spremi(s.p, s.id, s.polja...); !strings.Contains(l, s.ceka) {
			t.Errorf("%s: %s", s.opis, l)
		}
	}

	d := o.stranica(t, o.peroOvl, "area=1")
	if d.InactiveItems != 1 || len(d.Items) != 1 || d.Items[0].Number != "PR-1" {
		t.Errorf("isključena stavka se ne prikazuje: isključenih %d, prikazano %+v", d.InactiveItems, d.Items)
	}
	if d := o.stranica(t, o.peroOvl, "area=1&sve=1"); !d.ShowAll || len(d.Items) != 2 {
		t.Errorf("uz sve=1 prikazano %d stavki", len(d.Items))
	}

	brisi := func(p *models.UserPermissions, area, id string) string {
		return o.posalji(o.h.HandleDeleteItem, p, "/odrzavanje/stavke/"+id+"/delete", url.Values{"area": {area}}, map[string]string{"id": id})
	}
	if l := brisi(o.anaOvl, "1", id); !strings.Contains(l, "error=nemate pravo") {
		t.Errorf("brisanje bez prava: %s", l)
	}
	if l := brisi(o.peroOvl, "1", uuid.NewString()); !strings.Contains(l, "error=stavka nije pronađena") {
		t.Errorf("brisanje nepostojeće: %s", l)
	}
	if l := brisi(o.peroOvl, "99", id); l != "/odrzavanje?error=Nepoznato područje" {
		t.Errorf("brisanje u nepoznatom području: %s", l)
	}
	if l := brisi(o.peroOvl, "1", id); l != "/odrzavanje?area=1&success=Stavka je obrisana.#stavke" {
		t.Errorf("brisanje: %s", l)
	}
	if d := o.stranica(t, o.peroOvl, "area=1&sve=1"); len(d.Items) != 1 {
		t.Errorf("nakon brisanja %d stavki", len(d.Items))
	}
	if l := spremi(o.peroOvl, "", "area", "99", "description", "x", "unit", "ha"); l != "/odrzavanje?error=Nepoznato područje" {
		t.Errorf("stavka u nepoznatom području: %s", l)
	}
}

// Obrazac koji se ne da pročitati vraća na stranicu održavanja s porukom, za
// svaku radnju.
func TestOdrzavanjeNeispravanZahtjev(t *testing.T) {
	o, _ := novaOkolinaOdrzavanja(t)
	for ime, h := range map[string]http.HandlerFunc{
		"lokacija": o.h.HandleAddWater, "upis ugovora": o.h.HandleImportWrite, "stavka": o.h.HandleSaveItem,
		"brisanje stavke": o.h.HandleDeleteItem, "veza": o.h.HandleLinkWater, "brisanje lokacije": o.h.HandleDeleteWater,
	} {
		w := odgovor(h, obicniObrazac("/odrzavanje", "area=%zz"), o.pero, o.peroOvl)
		if l := mustUnescape(w.Header().Get("Location")); l != "/odrzavanje?error=Neispravan zahtjev" {
			t.Errorf("%s: %d %s", ime, w.Code, l)
		}
	}
	w := odgovor(o.h.HandleImportUpload, obicniObrazac("/odrzavanje/uvoz", "area=1"), o.pero, o.peroOvl)
	if l := mustUnescape(w.Header().Get("Location")); !strings.HasPrefix(l, "/odrzavanje?error=Datoteka nije primljena: ") {
		t.Errorf("uvoz bez višedijelnog obrasca: %s", l)
	}
}

// Stranica koja ne može pročitati popis lokacija ili stavki javlja grešku
// poslužitelja, a ne prazan popis.
func TestOdrzavanjeGreskaCitanja(t *testing.T) {
	o, exec := novaOkolinaOdrzavanja(t)
	otvori := func() *httptest.ResponseRecorder {
		return odgovor(o.h.ShowMaintenance, httptest.NewRequest(http.MethodGet, "/odrzavanje?area=1", nil), o.pero, o.peroOvl)
	}
	if err := exec(`ALTER TABLE work_items RENAME TO nema_work_items`); err != nil {
		t.Fatal(err)
	}
	if w := otvori(); w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "work_items") {
		t.Errorf("stavke koje se ne čitaju: %d %s", w.Code, w.Body.String())
	}
	if err := exec(`ALTER TABLE maintained_waters RENAME TO nema_maintained_waters`); err != nil {
		t.Fatal(err)
	}
	if w := otvori(); w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "maintained_waters") {
		t.Errorf("lokacije koje se ne čitaju: %d %s", w.Code, w.Body.String())
	}
}

var reTokenUgovora = regexp.MustCompile(`name="token" value="([^"]+)"`)

// ugovorSOznakom vraća radnu knjigu ugovora s jedinstvenom oznakom u
// komentaru zip arhive, da se njezine učitane kopije mogu naći
func ugovorSOznakom(t *testing.T, putanja string) ([]byte, string) {
	t.Helper()
	izvor, err := zip.OpenReader(putanja)
	if err != nil {
		t.Fatal(err)
	}
	defer izvor.Close()
	oznaka := "probni-ugovor-" + uuid.NewString()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range izvor.File {
		if err := zw.Copy(f); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.SetComment(oznaka); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), oznaka
}

// Ugovor se najprije učita i pokaže izvješće bez upisa; upis traži učitanu
// datoteku koja još čeka, a nakon upisa je više nema. Datoteka koja nije
// radna knjiga, ugovor drugog područja i istekla datoteka vraćaju poruku.
func TestOdrzavanjeUvozUgovora(t *testing.T) {
	o, _ := novaOkolinaOdrzavanja(t)
	ugovor, oznaka := ugovorSOznakom(t, filepath.Join("..", "..", "docs", "predlosci", "odrzavanje", "ugovor-a02.xlsx"))
	// učitane datoteke idu u zajedničku privremenu mapu; ovdje se prepoznaju
	// po oznaci u komentaru radne knjige i na kraju brišu
	cekaju := func() []string {
		t.Helper()
		var nadjene []string
		imena, _ := filepath.Glob(filepath.Join(importDir(), "*.xlsx"))
		for _, ime := range imena {
			if b, err := os.ReadFile(ime); err == nil && bytes.Contains(b, []byte(oznaka)) {
				nadjene = append(nadjene, ime)
			}
		}
		return nadjene
	}
	t.Cleanup(func() {
		for _, ime := range cekaju() {
			os.Remove(ime)
		}
	})
	ucitaj := func(p *models.UserPermissions, area string, dat ...datotekaObrasca) *httptest.ResponseRecorder {
		u := p.User
		return odgovor(o.h.HandleImportUpload, viseDijelni(t, "/odrzavanje/uvoz", map[string]string{"area": area}, dat...), &u, p)
	}
	lokacija := func(w *httptest.ResponseRecorder) string { return mustUnescape(w.Header().Get("Location")) }

	if l := lokacija(ucitaj(o.peroOvl, "1")); l != "/odrzavanje?area=1&error=Odaberite datoteku ugovora (xlsx).#uvoz" {
		t.Errorf("bez datoteke: %s", l)
	}
	if l := lokacija(ucitaj(o.peroOvl, "1", datotekaObrasca{"file", "ugovor.csv", "a;b"})); !strings.Contains(l, "error=Ugovor se uvozi iz radne knjige Excel (.xlsx)") {
		t.Errorf("datoteka koja nije xlsx: %s", l)
	}
	if l := lokacija(ucitaj(o.peroOvl, "99", datotekaObrasca{"file", "ugovor.xlsx", string(ugovor)})); l != "/odrzavanje?error=Nepoznato područje" {
		t.Errorf("nepoznato područje: %s", l)
	}
	// ugovor područja 1 ne ulazi u područje 2; učitana datoteka se tada briše
	if l := lokacija(ucitaj(o.peroOvl, "2", datotekaObrasca{"file", "ugovor.xlsx", string(ugovor)})); !strings.Contains(l, "error=datoteka je ugovor drugog branjenog područja (BP 1)") {
		t.Errorf("ugovor drugog područja: %s", l)
	}
	if l := lokacija(ucitaj(o.markOvl, "1", datotekaObrasca{"file", "ugovor.xlsx", string(ugovor)})); !strings.Contains(l, "error=nemate pravo uređivati popis lokacija tog područja") {
		t.Errorf("uvoz bez prava: %s", l)
	}
	if c := cekaju(); len(c) != 0 {
		t.Errorf("odbijeni ugovori i dalje čekaju: %v", c)
	}

	w := ucitaj(o.peroOvl, "1", datotekaObrasca{"file", "Ugovor-A02.XLSX", string(ugovor)})
	if w.Code != http.StatusOK {
		t.Fatalf("izvješće: %d %s", w.Code, lokacija(w))
	}
	m := reTokenUgovora.FindStringSubmatch(w.Body.String())
	if m == nil {
		t.Fatal("izvješće nema broj učitane datoteke")
	}
	token := m[1]
	if c := cekaju(); len(c) != 1 || filepath.Base(c[0]) != token+".xlsx" {
		t.Errorf("učitani ugovor ne čeka pod svojim brojem %s: %v", token, c)
	}
	if !strings.Contains(w.Body.String(), "Ugovor-A02.XLSX") {
		t.Error("izvješće ne nosi ime učitane datoteke")
	}
	if d := o.stranica(t, o.peroOvl, "area=1"); d.WaterCount != 0 {
		t.Errorf("izvješće je upisalo %d lokacija", d.WaterCount)
	}

	upisi := func(tok string) string {
		forma := url.Values{"area": {"1"}, "token": {tok}, "filename": {"ugovor.xlsx"}}
		return o.posalji(o.h.HandleImportWrite, o.peroOvl, "/odrzavanje/uvoz/upisi", forma, nil)
	}
	if l := upisi("nije-broj"); l != "/odrzavanje?area=1&error=Učitana datoteka više ne čeka; učitajte je ponovno.#uvoz" {
		t.Errorf("neispravan broj: %s", l)
	}
	if l := upisi(uuid.NewString()); !strings.Contains(l, "error=Učitana datoteka više ne čeka") {
		t.Errorf("datoteka koja ne čeka: %s", l)
	}
	if l := o.posalji(o.h.HandleImportWrite, o.peroOvl, "/", url.Values{"area": {"99"}, "token": {token}}, nil); l != "/odrzavanje?error=Nepoznato područje" {
		t.Errorf("upis u nepoznato područje: %s", l)
	}
	if l := upisi(token); l != "/odrzavanje?area=1&success=Ugovor je upisan: 2 lokacije, 3 nove stavke radova." {
		t.Errorf("upis: %s", l)
	}
	if d := o.stranica(t, o.peroOvl, "area=1"); d.WaterCount != 2 || len(d.Items) != 3 {
		t.Errorf("nakon upisa %d lokacija i %d stavki", d.WaterCount, len(d.Items))
	}
	if _, err := os.Stat(filepath.Join(importDir(), token+".xlsx")); !os.IsNotExist(err) {
		t.Errorf("upisana datoteka i dalje čeka: %v", err)
	}
	if l := upisi(token); !strings.Contains(l, "error=Učitana datoteka više ne čeka") {
		t.Errorf("drugi upis iste datoteke: %s", l)
	}
}

// Popis se slaže redom kojim lokacije stižu; lokacija bez reda ide pod
// Nerazvrstano, a ista vrsta u istoj skupini ostaje zajedno.
func TestGroupWatersSlazePoRedu(t *testing.T) {
	g := groupWaters([]models.MaintainedWater{
		{Name: "Bez reda", Kind: models.MaintenanceKindWatercourse},
		{Name: "Kanal A", Order: models.WaterOrderSecond, Kind: models.MaintenanceKindDrainage, AreaID: 1},
		{Name: "Kanal B", Order: models.WaterOrderSecond, Kind: models.MaintenanceKindDrainage, AreaID: 1},
		{Name: "Bujica", Order: models.WaterOrderSecond, Kind: models.MaintenanceKindTorrent, AreaID: 1},
		{Name: "Kanal C", Order: models.WaterOrderFourth, Kind: models.MaintenanceKindDrainage, Program: models.ProgramA03, AreaID: 1},
	})
	if len(g) != 3 || g[0].Label != "A.02 · Nerazvrstano" || g[1].Label != "A.02 · Vode II. reda" || g[2].Label != "A.03 · Vode IV. reda" {
		t.Fatalf("skupine: %+v", g)
	}
	if len(g[1].Kinds) != 2 || len(g[1].Kinds[0].Waters) != 2 || g[1].Kinds[1].Label != "Bujični tokovi" {
		t.Errorf("vrste u skupini II. reda: %+v", g[1].Kinds)
	}
	if g[1].Kinds[0].PlanPosition != "A.02.01.01.02.04." || g[0].Kinds[0].PlanPosition != "" {
		t.Errorf("pozicija plana: %q / %q", g[1].Kinds[0].PlanPosition, g[0].Kinds[0].PlanPosition)
	}
	if (&MaintenanceHandler{}).back(nil) != "/odrzavanje" {
		t.Error("povratak bez područja")
	}
}
