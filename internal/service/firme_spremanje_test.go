package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Testovi OrgService.SaveContractor: tko smije upisati licenciranu firmu i
// gdje radi, provjera polja, mjesta rada koja korisnik ne vodi i greške
// baze. Podaci su izmišljeni: sektor F (područja 41 i 42) i sektor E
// (područje 43), firme Primjergradnja i Probnoradnja.

type okolinaFirmi struct {
	baza   *sql.DB
	svc    *OrgService
	repo   *repository.OrgRepository
	dogadj chan string
}

func novaOkolinaFirmi(t *testing.T) *okolinaFirmi {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "firme.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('F', 'Sektor F', 'VGO Primjerovo', 'COP Primjerovo'), ('E', 'Sektor E', 'VGO Drugdje', 'COP Drugdje')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (41, 'F', 'Mali sliv Primjerica', 'VGI Primjerica', ''),
			(42, 'F', 'Mali sliv Probni', 'VGI Probni', ''), (43, 'E', 'Mali sliv Drugdje', 'VGI Drugdje', '')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	sse := NewSSEBroker()
	repo := repository.NewOrgRepository(baza, ledger.New(baza, "test"))
	return &okolinaFirmi{baza: baza, svc: NewOrgService(repo, sse), repo: repo, dogadj: sse.Subscribe()}
}

var (
	firmeGlobalni   = &models.UserPermissions{IsGlobalAdmin: true}
	firmeSektorF    = &models.UserPermissions{AdminSectors: map[string]bool{"F": true}}
	firmePodrucje41 = &models.UserPermissions{AdminAreas: map[int]bool{41: true}}
)

// postojecaFirma upisuje firmu izravno u repozitorij, mimo provjera servisa
func (o *okolinaFirmi) postojecaFirma(t *testing.T, id string, gdje ...models.ContractorAssignment) {
	t.Helper()
	c := &models.Contractor{ID: id, Name: "Probnoradnja d.o.o.", Active: true}
	if err := o.repo.SaveContractor(context.Background(), c, gdje); err != nil {
		t.Fatal(err)
	}
}

// mjestaRada vraća ključeve veza firme, abecedno
func (o *okolinaFirmi) mjestaRada(t *testing.T, id string) []string {
	t.Helper()
	veze, err := o.repo.ListAssignments(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, v := range veze {
		out = append(out, v.Key())
	}
	slices.Sort(out)
	return out
}

// verzije broji zapise knjige po entitetu i arhiviranosti, npr. "contractor_assignments/1"
func (o *okolinaFirmi) verzije(t *testing.T) map[string]int {
	t.Helper()
	rows, err := o.baza.Query(`SELECT entity, archived, COUNT(*) FROM record_versions WHERE entity LIKE 'contractor%' GROUP BY entity, archived`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var e string
		var a, n int
		if err := rows.Scan(&e, &a, &n); err != nil {
			t.Fatal(err)
		}
		out[e+"/"+strconv.Itoa(a)] = n
	}
	return out
}

func (o *okolinaFirmi) brojFirmi(t *testing.T) int {
	t.Helper()
	var n int
	if err := o.baza.QueryRow(`SELECT COUNT(*) FROM contractors`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// zadnjiDogadjaj vraća vrstu, poruku i oznaku zadnjeg objavljenog događaja
func (o *okolinaFirmi) zadnjiDogadjaj() string {
	var zadnji string
	for {
		select {
		case poruka := <-o.dogadj:
			_, podaci, _ := strings.Cut(poruka, "data: ") // poruka uvijek ima redak podataka
			var e SSEEvent
			if err := json.Unmarshal([]byte(strings.TrimSpace(podaci)), &e); err != nil {
				return poruka
			}
			oznaka, _ := e.Payload.(string) // oznaka firme je tekst
			zadnji = e.Type + ":" + e.Message + ":" + oznaka
		default:
			return zadnji
		}
	}
}

func uSektoru(s string) models.ContractorAssignment { return models.ContractorAssignment{SectorID: s} }
func uPodrucju(id int) models.ContractorAssignment  { return models.ContractorAssignment{AreaID: id} }

func TestSpremiFirmuNeispravanUlaz(t *testing.T) {
	o := novaOkolinaFirmi(t)
	ctx := context.Background()
	for _, s := range []struct {
		ime    string
		perms  *models.UserPermissions
		c      models.Contractor
		gdje   []models.ContractorAssignment
		greska string
	}{
		{"bez prijave", nil, models.Contractor{Name: "Primjergradnja"}, nil, "prijava je obavezna"},
		{"bez naziva", firmeGlobalni, models.Contractor{Name: "   "}, nil, "naziv firme je obavezan"},
		{"kratak OIB", firmeGlobalni, models.Contractor{Name: "Primjergradnja", OIB: "1234"}, nil, "OIB ima 11 znamenki"},
		{"slovo u OIB-u", firmeGlobalni, models.Contractor{Name: "Primjergradnja", OIB: "1234567890a"}, nil, "OIB ima 11 znamenki"},
		{"nepostojeće područje", firmeGlobalni, models.Contractor{Name: "Primjergradnja"}, []models.ContractorAssignment{uPodrucju(99)}, "branjeno područje 99 ne postoji"},
		{"nepostojeći sektor", firmeGlobalni, models.Contractor{Name: "Primjergradnja"}, []models.ContractorAssignment{uSektoru(" x ")}, `sektor "X" ne postoji`},
		{"prazan sektor", firmeGlobalni, models.Contractor{Name: "Primjergradnja"}, []models.ContractorAssignment{{}}, `sektor "" ne postoji`},
		{"bez mjesta rada", firmeSektorF, models.Contractor{Name: "Primjergradnja"}, nil, "dopušten je samo globalnom administratoru"},
		{"tuđi sektor", firmeSektorF, models.Contractor{Name: "Primjergradnja"}, []models.ContractorAssignment{uSektoru("F"), uSektoru("E")}, "samo za sektor ili branjeno područje koje vodite"},
		{"tuđe područje", firmePodrucje41, models.Contractor{Name: "Primjergradnja"}, []models.ContractorAssignment{uPodrucju(41), uPodrucju(42)}, "samo za sektor ili branjeno područje koje vodite"},
		{"cijeli sektor uprave područja", firmePodrucje41, models.Contractor{Name: "Primjergradnja"}, []models.ContractorAssignment{uSektoru("F")}, "samo za sektor ili branjeno područje koje vodite"},
		{"nepostojeća firma", firmeSektorF, models.Contractor{ID: "nema-je", Name: "Primjergradnja"}, []models.ContractorAssignment{uSektoru("F")}, "firma ne postoji"},
	} {
		c := s.c
		err := o.svc.SaveContractor(ctx, s.perms, &c, s.gdje)
		if err == nil || !strings.Contains(err.Error(), s.greska) {
			t.Errorf("%s: očekivano „%s”, dobiveno %v", s.ime, s.greska, err)
		}
	}
	if n := o.brojFirmi(t); n != 0 {
		t.Errorf("odbijene firme su upisane: %d", n)
	}
	if d := o.zadnjiDogadjaj(); d != "" {
		t.Errorf("odbijeno spremanje objavilo je događaj %q", d)
	}
}

func TestSpremiFirmuGlobalniNova(t *testing.T) {
	o := novaOkolinaFirmi(t)
	ctx := context.Background()
	c := &models.Contractor{Name: "  Primjergradnja d.d. ", ShortName: " Primjer ", OIB: " 12345678901 ", Address: " Probna 1 ",
		Phone: " 031 000 000 ", Email: " ured@primjer.test ", Contact: " Ivo Izmišljeni ", Notes: " A.02 ", Active: true}
	if err := o.svc.SaveContractor(ctx, firmeGlobalni, c, nil); err != nil {
		t.Fatal(err)
	}
	// polja bez rubnih razmaka; globalni smije firmu bez mjesta rada
	if c.ID == "" || c.Name != "Primjergradnja d.d." || c.ShortName != "Primjer" || c.OIB != "12345678901" || c.Address != "Probna 1" ||
		c.Phone != "031 000 000" || c.Email != "ured@primjer.test" || c.Contact != "Ivo Izmišljeni" || c.Notes != "A.02" {
		t.Errorf("nakon spremanja: %+v", c)
	}
	upisana, err := o.repo.GetContractor(ctx, c.ID)
	if err != nil || upisana == nil || upisana.Name != "Primjergradnja d.d." || upisana.OIB != "12345678901" || len(upisana.Assignments) != 0 {
		t.Fatalf("upisana firma: %+v, %v", upisana, err)
	}
	if d := o.zadnjiDogadjaj(); d != "organization_changed:Licencirana firma: Primjergradnja d.d.:"+c.ID {
		t.Errorf("događaj: %q", d)
	}
	if v := o.verzije(t); v["contractors/0"] != 1 || len(v) != 1 {
		t.Errorf("knjiga: %v", v)
	}
}

func TestSpremiFirmuMjestaRada(t *testing.T) {
	o := novaOkolinaFirmi(t)
	ctx := context.Background()
	// područje nosi svoj sektor bez obzira na upisani; dvostruko mjesto se
	// upisuje jednom, sektor se piše velikim slovima
	c := &models.Contractor{Name: "Primjergradnja"}
	gdje := []models.ContractorAssignment{
		{SectorID: "E", AreaID: 41, Note: "prvi"}, uPodrucju(41), uSektoru(" f "), uSektoru("F"), uPodrucju(43),
	}
	if err := o.svc.SaveContractor(ctx, firmeGlobalni, c, gdje); err != nil {
		t.Fatal(err)
	}
	if m := o.mjestaRada(t, c.ID); !slices.Equal(m, []string{"E|43", "F|0", "F|41"}) {
		t.Errorf("mjesta rada: %v", m)
	}
	veze, err := o.repo.ListAssignments(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range veze {
		if v.AreaID == 41 && v.Note != "prvi" {
			t.Errorf("od dvostrukih ostaje prvo mjesto: %+v", v)
		}
	}
	if v := o.verzije(t); v["contractor_assignments/0"] != 3 {
		t.Errorf("knjiga: %v", v)
	}

	// izmjena: F|0 ostaje bez nove verzije, F|41 i E|43 se arhiviraju, F|42 se dodaje
	c.Name = "Primjergradnja d.d."
	if err := o.svc.SaveContractor(ctx, firmeGlobalni, c, []models.ContractorAssignment{uSektoru("F"), uPodrucju(42)}); err != nil {
		t.Fatal(err)
	}
	if m := o.mjestaRada(t, c.ID); !slices.Equal(m, []string{"F|0", "F|42"}) {
		t.Errorf("mjesta rada nakon izmjene: %v", m)
	}
	if v := o.verzije(t); v["contractors/0"] != 2 || v["contractor_assignments/0"] != 4 || v["contractor_assignments/1"] != 2 {
		t.Errorf("knjiga nakon izmjene: %v", v)
	}

	// micanje svih mjesta rada smije samo globalni
	if err := o.svc.SaveContractor(ctx, firmeGlobalni, c, nil); err != nil {
		t.Fatal(err)
	}
	if m := o.mjestaRada(t, c.ID); len(m) != 0 {
		t.Errorf("mjesta rada nakon micanja: %v", m)
	}
}

func TestSpremiFirmuUpravaPodrucja(t *testing.T) {
	o := novaOkolinaFirmi(t)
	ctx := context.Background()
	c := &models.Contractor{Name: "Primjergradnja"}
	if err := o.svc.SaveContractor(ctx, firmePodrucje41, c, []models.ContractorAssignment{uPodrucju(41)}); err != nil {
		t.Fatal(err)
	}
	if m := o.mjestaRada(t, c.ID); !slices.Equal(m, []string{"F|41"}) {
		t.Errorf("mjesta rada: %v", m)
	}
	if d := o.zadnjiDogadjaj(); !strings.HasPrefix(d, "organization_changed:Licencirana firma: Primjergradnja:") {
		t.Errorf("događaj: %q", d)
	}
}

func TestSpremiFirmuTudaMjestaOstaju(t *testing.T) {
	o := novaOkolinaFirmi(t)
	ctx := context.Background()
	o.postojecaFirma(t, "firma-1", uSektoru("F"), models.ContractorAssignment{SectorID: "E", AreaID: 43, Note: "ugovor E"})
	// uprava sektora F mijenja svoje mjesto (cijeli F u područje 42), a mjesto
	// u sektoru E koje ne vodi ostaje kakvo je bilo
	c := &models.Contractor{ID: "firma-1", Name: "Probnoradnja d.o.o."}
	if err := o.svc.SaveContractor(ctx, firmeSektorF, c, []models.ContractorAssignment{uPodrucju(42)}); err != nil {
		t.Fatal(err)
	}
	if m := o.mjestaRada(t, "firma-1"); !slices.Equal(m, []string{"E|43", "F|42"}) {
		t.Errorf("mjesta rada: %v", m)
	}
	veze, err := o.repo.ListAssignments(ctx, "firma-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range veze {
		if v.AreaID == 43 && v.Note != "ugovor E" {
			t.Errorf("tuđe mjesto je promijenjeno: %+v", v)
		}
	}
	// E|43 nije dobio novu verziju, F|0 je arhiviran
	if v := o.verzije(t); v["contractor_assignments/0"] != 3 || v["contractor_assignments/1"] != 1 {
		t.Errorf("knjiga: %v", v)
	}
}

func TestSpremiFirmuBezPostojecihMjesta(t *testing.T) {
	o := novaOkolinaFirmi(t)
	ctx := context.Background()
	// firmu bez ijednog mjesta rada smije preuzeti svaka uprava
	o.postojecaFirma(t, "firma-1")
	c := &models.Contractor{ID: "firma-1", Name: "Probnoradnja d.o.o."}
	if err := o.svc.SaveContractor(ctx, firmePodrucje41, c, []models.ContractorAssignment{uPodrucju(41)}); err != nil {
		t.Fatal(err)
	}
	if m := o.mjestaRada(t, "firma-1"); !slices.Equal(m, []string{"F|41"}) {
		t.Errorf("mjesta rada: %v", m)
	}
}

// Uprava miče svoje jedino mjesto rada s firme koja radi i drugdje: tuđe
// mjesto ostaje. Firma koja radi samo kod nje ne ostaje bez mjesta, a nova
// firma bez mjesta i dalje se ne upisuje.
func TestSpremiFirmuBezSvogMjesta(t *testing.T) {
	o := novaOkolinaFirmi(t)
	ctx := context.Background()
	o.postojecaFirma(t, "firma-1", models.ContractorAssignment{SectorID: "F", AreaID: 41}, models.ContractorAssignment{SectorID: "E", AreaID: 43})
	c := &models.Contractor{ID: "firma-1", Name: "Probnoradnja d.o.o."}
	if err := o.svc.SaveContractor(ctx, firmePodrucje41, c, nil); err != nil {
		t.Fatalf("uprava područja 41 miče svoje mjesto: %v", err)
	}
	if m := o.mjestaRada(t, "firma-1"); !slices.Equal(m, []string{"E|43"}) {
		t.Errorf("mjesta rada: %v", m)
	}

	o.postojecaFirma(t, "firma-2", models.ContractorAssignment{SectorID: "F", AreaID: 41})
	c2 := &models.Contractor{ID: "firma-2", Name: "Primjergradnja d.o.o."}
	if err := o.svc.SaveContractor(ctx, firmePodrucje41, c2, nil); err == nil || !strings.Contains(err.Error(), "bez ijednog mjesta rada") {
		t.Errorf("firma samo u području 41 ostala je bez mjesta: %v", err)
	}
	if m := o.mjestaRada(t, "firma-2"); !slices.Equal(m, []string{"F|41"}) {
		t.Errorf("mjesta rada nakon odbijenog upisa: %v", m)
	}
	// tko ne vodi nijedno mjesto, ne uređuje firmu ni s praznim popisom
	o.postojecaFirma(t, "firma-3", models.ContractorAssignment{SectorID: "E", AreaID: 43})
	c3 := &models.Contractor{ID: "firma-3", Name: "Treća d.o.o."}
	if err := o.svc.SaveContractor(ctx, firmePodrucje41, c3, nil); err == nil || !strings.Contains(err.Error(), "barem jedno od njenih postojećih mjesta") {
		t.Errorf("tuđa firma s praznim popisom: %v", err)
	}
	if err := o.svc.SaveContractor(ctx, firmePodrucje41, &models.Contractor{Name: "Nova d.o.o."}, nil); err == nil || !strings.Contains(err.Error(), "samo globalnom administratoru") {
		t.Errorf("nova firma bez mjesta rada: %v", err)
	}
}

func TestSpremiFirmuSamoTudaMjesta(t *testing.T) {
	o := novaOkolinaFirmi(t)
	ctx := context.Background()
	o.postojecaFirma(t, "firma-1", uPodrucju(43))
	c := &models.Contractor{ID: "firma-1", Name: "Preoteta d.o.o."}
	err := o.svc.SaveContractor(ctx, firmeSektorF, c, []models.ContractorAssignment{uSektoru("F")})
	if err == nil || err.Error() != "ovu firmu smije uređivati samo tko vodi barem jedno od njenih postojećih mjesta rada" {
		t.Errorf("greška: %v", err)
	}
	upisana, err := o.repo.GetContractor(ctx, "firma-1")
	if err != nil || upisana.Name != "Probnoradnja d.o.o." {
		t.Errorf("firma je promijenjena: %+v, %v", upisana, err)
	}
	if d := o.zadnjiDogadjaj(); d != "" {
		t.Errorf("odbijeno spremanje objavilo je događaj %q", d)
	}
}

func TestSpremiFirmuGreskeBaze(t *testing.T) {
	ctx := context.Background()
	for _, s := range []struct {
		ime    string
		kvar   string
		perms  *models.UserPermissions
		c      models.Contractor
		gdje   []models.ContractorAssignment
		greska string
	}{
		{"čitanje područja", `ALTER TABLE areas RENAME TO podrucja_stara`, firmeGlobalni, models.Contractor{Name: "Primjergradnja"}, nil, "no such table: areas"},
		{"čitanje sektora", `ALTER TABLE sectors RENAME TO sektori_stari`, firmeGlobalni, models.Contractor{Name: "Primjergradnja"},
			[]models.ContractorAssignment{uSektoru("F")}, "no such table: sectors"},
		{"čitanje firme", `ALTER TABLE contractors RENAME TO firme_stare`, firmeSektorF, models.Contractor{ID: "firma-1", Name: "Primjergradnja"},
			[]models.ContractorAssignment{uSektoru("F")}, "no such table: contractors"},
		{"upis firme", `CREATE TRIGGER bez_upisa BEFORE INSERT ON contractors BEGIN SELECT RAISE(ABORT, 'upis zabranjen'); END`, firmeGlobalni,
			models.Contractor{Name: "Primjergradnja"}, []models.ContractorAssignment{uPodrucju(41)}, "upis licencirane firme: constraint failed: upis zabranjen"},
	} {
		o := novaOkolinaFirmi(t)
		if _, err := o.baza.Exec(s.kvar); err != nil {
			t.Fatal(err)
		}
		c := s.c
		err := o.svc.SaveContractor(ctx, s.perms, &c, s.gdje)
		if err == nil || !strings.Contains(err.Error(), s.greska) {
			t.Errorf("%s: očekivano „%s”, dobiveno %v", s.ime, s.greska, err)
		}
		if d := o.zadnjiDogadjaj(); d != "" {
			t.Errorf("%s: neuspjelo spremanje objavilo je događaj %q", s.ime, d)
		}
	}
}
