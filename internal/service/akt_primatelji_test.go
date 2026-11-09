package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Popis primatelja akta (Odluke (8. 10. 2026.), pitanje 19): izvor koji se ne
// da pročitati i dionica bez teritorija daju upozorenje, ovjera dok ono
// stoji traži potvrdu, a pri ovjeri se popis slaže ponovno. Akt je uvijek za
// jedno branjeno područje (Odluke (9. 11. 2026.)), pa i primatelji.

// uzTeritorij upisuje okolini teritorij dionice P.1.1 (P.1.2 ostaje bez
// njega), službu civilne zaštite županije i drugo branjeno područje s
// dionicom P.2.1 na istoj letvi
func uzTeritorij(t *testing.T, o *okolinaAkta) {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO counties (id, code, name, seat, email) VALUES (16, 'VS', 'Vukovarsko-srijemska županija', 'Vukovar', 'zupan@primjer.hr')`,
		`INSERT INTO municipalities (id, county_id, name, type, email) VALUES (501, 16, 'Primjerovo', 'GRAD', 'grad@primjer.hr')`,
		`INSERT INTO section_territories (id, section_code, county_id, municipality_id, created_at) VALUES ('t1', 'P.1.1', 16, 501, '2026-01-01')`,
		`INSERT INTO sluzbe (id, county_id, vrsta, naziv, email, updated_at) VALUES ('cz', 16, '` + models.SluzbaCivilnaZastita + `', 'Civilna zaštita Vukovar', 'cz@primjer.hr', '2026-01-01')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, contractor_name) VALUES (2, 'P', 'Drugo područje', 'VGI Drugo', 'Ugovorna osoba područja 2')`,
		`UPDATE areas SET contractor_name = 'Ugovorna osoba područja 1' WHERE id = 1`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.2.1', 2, 'P', 'nasip Probni', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := o.baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := o.baza.Exec(`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES (?, 'P.2.1', ?, ?)`, uuid.NewString(), o.letva.ID.String(), time.Now()); err != nil {
		t.Fatal(err)
	}
}

func imena(p []models.AktPrimatelj) string {
	var out []string
	for _, x := range p {
		out = append(out, x.Naziv)
	}
	return strings.Join(out, "; ")
}

// Letva pokriva dionice dvaju područja, a akt je za jedno (Odluke
// (9. 11. 2026.)): firme, rukovoditelji, ispostave i unosi registra drugog
// područja nisu na popisu akta prvoga, i obrnuto
func TestPrimateljiPodrucjaAkta(t *testing.T) {
	o := novaOkolinaAkta(t)
	uzTeritorij(t, o)
	ctx := context.Background()
	rec := ledger.New(o.baza, "test")
	for _, q := range []string{
		`INSERT INTO contractors (id, name, email, active, updated_at) VALUES ('f2', 'Vodogradnja Drugo d.o.o.', 'firma@primjer.hr', 1, '2026-01-01')`,
		`INSERT INTO contractor_assignments (id, contractor_id, sector_id, area_id, updated_at) VALUES ('fa2', 'f2', 'P', 2, '2026-01-01')`,
		// primatelj se upisuje samo za područje koje je u sektoru
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (3, 'P', 'Treće područje', 'VGI Treće')`,
	} {
		if _, err := o.baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	sektor, drugo := "P", 2
	ana := &models.User{ID: uuid.New(), Username: "aanic", FullName: "Ana Anić", IsActive: true}
	if err := repository.NewUserRepository(o.baza, rec).CreateUser(ana, &models.Duty{Title: "Rukovoditeljica BP 2", Role: models.RoleAreaLeader,
		ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &drugo, IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	uprava := &models.UserPermissions{IsGlobalAdmin: true, User: *o.rukovod}
	for _, p := range []models.Primatelj{
		{Sektor: "P", Naziv: "Glavni centar obrane od poplava", Aktivan: true},
		{Sektor: "P", AreaID: 2, Naziv: "Registar samo za područje 2", Aktivan: true},
		{Sektor: "P", AreaID: 2, Skupina: models.SkupinaIspostava, Naziv: "Izvođač područja 2 iz registra", Email: "izvodjac2@primjer.hr", Aktivan: true},
		{Sektor: "P", AreaID: 3, Skupina: models.SkupinaIspostava, Naziv: "Izvođač područja 3 iz registra", Aktivan: true},
		{Sektor: "P", AreaID: 2, Skupina: models.SkupinaIspostava, Naziv: "Izvođač područja 2 od izvanredne", OdStupnja: models.PhaseEmergency, Aktivan: true},
	} {
		p := p
		if err := o.akti.SpremiPrimatelja(ctx, uprava, &p); err != nil {
			t.Fatal(err)
		}
	}
	akt := func(podrucje int) *models.Akt {
		t.Helper()
		a, err := o.akti.Pripremi(ctx, o.dezOvl, o.dezurni, service.ZahtjevAkta{StationID: o.letva.ID.String(),
			Radnja: models.AktUspostava, Stupanj: models.PhasePrep, Podrucje: podrucje})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	provjeri := func(ime, popis string, ima, nema []string) {
		t.Helper()
		for _, d := range ima {
			if !strings.Contains(popis, d) {
				t.Errorf("%s: na popisu nema %q: %s", ime, d, popis)
			}
		}
		for _, d := range nema {
			if strings.Contains(popis, d) {
				t.Errorf("%s: na popisu ne smije biti %q: %s", ime, d, popis)
			}
		}
	}

	// bez izabranog područja akt se ne sastavlja
	if _, err := o.akti.Pripremi(ctx, o.dezOvl, o.dezurni, service.ZahtjevAkta{StationID: o.letva.ID.String(),
		Radnja: models.AktUspostava, Stupanj: models.PhasePrep}); err == nil || !strings.Contains(err.Error(), "BP 1 i BP 2") {
		t.Errorf("letva dvaju područja bez izbora: %v", err)
	}
	prvo := akt(1)
	if prvo.AreaID != 1 || strings.Join(prvo.Sifre(), ",") != "P.1.1,P.1.2" {
		t.Fatalf("akt BP 1: područje %d, dionice %v", prvo.AreaID, prvo.Sifre())
	}
	provjeri("BP 1", imena(prvo.Primatelji),
		[]string{"Glavni centar obrane od poplava", "Ugovorna osoba područja 1", "Pero Perić, Rukovoditelj BP 1", "Civilna zaštita Vukovar", "Pismohrana"},
		[]string{"Ana Anić", "Vodogradnja Drugo d.o.o.", "Ugovorna osoba područja 2", "Registar samo za područje 2", "Izvođač područja 2", "Izvođač područja 3"})
	if len(prvo.UpozorenjaPrimatelja) != 1 || !strings.Contains(prvo.UpozorenjaPrimatelja[0], "bez upisanog teritorija (naselja i općine): P.1.2") ||
		strings.Contains(prvo.UpozorenjaPrimatelja[0], "P.2.1") {
		t.Errorf("upozorenje o dionicama bez teritorija: %v", prvo.UpozorenjaPrimatelja)
	}

	drugi := akt(2)
	if drugi.AreaID != 2 || strings.Join(drugi.Sifre(), ",") != "P.2.1" {
		t.Fatalf("akt BP 2: područje %d, dionice %v", drugi.AreaID, drugi.Sifre())
	}
	provjeri("BP 2", imena(drugi.Primatelji),
		[]string{"Glavni centar obrane od poplava", "Ana Anić, Rukovoditeljica BP 2", "Vodogradnja Drugo d.o.o.",
			"Registar samo za područje 2", "Izvođač područja 2 iz registra", "Pismohrana"},
		// ispostava iz registra zamjenjuje ugovornu osobu upisanu uz područje
		[]string{"Pero Perić", "Ugovorna osoba područja", "Izvođač područja 3", "Izvođač područja 2 od izvanredne"})

	// izvanredno stanje: župan i grad ugroženog područja, s e-poštom, a
	// izvođač upisan od izvanredne obrane samo uz akt svog područja
	prvo.Stupanj = models.PhaseState
	p := o.akti.PregledPrimatelja(ctx, prvo)
	if s := imena(p.Primatelji); !strings.Contains(s, "Župan, Vukovarsko-srijemska županija") || !strings.Contains(s, "Grad Primjerovo") ||
		strings.Contains(s, "Izvođač područja 2 od izvanredne") {
		t.Errorf("samouprava izvanrednog stanja: %s", s)
	}
	if len(p.Dodani) != 2 || p.Dodani[0].Email != "zupan@primjer.hr" || p.Dodani[1].Email != "grad@primjer.hr" || len(p.Uklonjeni) != 0 {
		t.Errorf("razlika prema nacrtu BP 1: +%v −%v", p.Dodani, p.Uklonjeni)
	}
	drugi.Stupanj = models.PhaseState
	if s := imena(o.akti.PregledPrimatelja(ctx, drugi).Primatelji); !strings.Contains(s, "Izvođač područja 2 od izvanredne") {
		t.Errorf("izvođač od izvanredne uz akt BP 2: %s", s)
	}
}

// Izvor koji se ne da pročitati ne preskače se šutke: nacrt se sastavi, a
// upozorenje kaže što nije pročitano
func TestUpozorenjaKadSeIzvorPrimateljaNeDaProcitati(t *testing.T) {
	for _, c := range []struct {
		opis, kvar, dionica, upozorenje string
	}{
		{"teritorij", `DROP TABLE section_territories`, "", "teritorij dionice P.1.1 nije pročitan"},
		{"službe županije", `DROP TABLE sluzbe`, "", "službe županije Vukovarsko-srijemska županija nisu pročitane"},
		{"registar primatelja", `DROP TABLE primatelji`, "", "registar primatelja sektora P nije pročitan"},
		{"registar firmi", `DROP TABLE contractor_assignments`, "", "registar licenciranih firmi za branjeno područje 1 nije pročitan"},
		{"branjena područja", `ALTER TABLE areas DROP COLUMN vgi_phone`, "", "branjena područja nisu pročitana: nema ugovorne osobe područja 1"},
		{"županija", `ALTER TABLE counties DROP COLUMN website`, "", "županija Vukovarsko-srijemska županija nije pročitana"},
		{"općine", `ALTER TABLE municipalities DROP COLUMN website`, "", "općine županije Vukovarsko-srijemska županija nisu pročitane"},
		{"rukovoditelji", `ALTER TABLE users DROP COLUMN org_name`, "", "rukovoditelji dionice P.1.1 nisu pročitani"},
		{"dionica", `ALTER TABLE sections DROP COLUMN description`, "", "dionica P.1.1 nije pročitana iz registra dionica"},
		{"dionica izvan registra", ``, "P.9.9", "dionice P.9.9 nema u registru dionica"},
	} {
		o := novaOkolinaAkta(t)
		uzTeritorij(t, o)
		ctx := context.Background()
		a := &models.Akt{Sektor: "P", AreaID: 1, Stupanj: models.PhaseState, Dionice: []models.AktDionica{{Code: "P.1.1"}}}
		if c.dionica != "" {
			a.Dionice = append(a.Dionice, models.AktDionica{Code: c.dionica})
		}
		if c.kvar != "" {
			if _, err := o.baza.Exec(c.kvar); err != nil {
				t.Fatalf("%s: %v", c.opis, err)
			}
		}
		p := o.akti.PregledPrimatelja(ctx, a)
		if !strings.Contains(strings.Join(p.Upozorenja, " | "), c.upozorenje) {
			t.Errorf("%s: nema upozorenja %q: %v", c.opis, c.upozorenje, p.Upozorenja)
		}
		if imena(p.Primatelji) == "" || p.Otisak == "" {
			t.Errorf("%s: popis se sastavi i bez izvora: %+v", c.opis, p)
		}
	}
}

// Ovjera dok stoji upozorenje traži potvrdu; pri ovjeri se popis slaže
// ponovno, a ovjera ne prolazi ako se popis promijenio otkad ga je osoba
// vidjela
func TestOvjeraIPopisPrimatelja(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	a, err := o.akti.Pripremi(ctx, o.dezOvl, o.dezurni, service.ZahtjevAkta{StationID: o.letva.ID.String(), Radnja: models.AktUspostava, Stupanj: models.PhasePrep, Vrijedi: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := o.akti.Spremi(ctx, o.dezOvl, a); err != nil {
		t.Fatal(err)
	}
	if len(a.UpozorenjaPrimatelja) == 0 {
		t.Fatal("dionice okoline nemaju teritorija: nacrt mora nositi upozorenje")
	}
	vidio := o.akti.PregledPrimatelja(ctx, a)
	var pitanje *service.TrebaPotvrduError
	if _, _, err := o.akti.Ovjeri(ctx, o.ovlasti, o.rukovod, a.ID, service.PotvrdaOvjere{Otisak: vidio.Otisak}); !errors.As(err, &pitanje) || !strings.Contains(err.Error(), "P.1.1, P.1.2") {
		t.Fatalf("ovjera bez potvrde dok stoji upozorenje: %v", err)
	}
	// registar se u međuvremenu dopuni: stranica koju je osoba vidjela više ne vrijedi
	uprava := &models.UserPermissions{IsGlobalAdmin: true, User: *o.rukovod}
	if err := o.akti.SpremiPrimatelja(ctx, uprava, &models.Primatelj{Sektor: "P", Naziv: "Ravnateljstvo civilne zaštite", Email: "rcz@primjer.hr", Aktivan: true}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := o.akti.Ovjeri(ctx, o.ovlasti, o.rukovod, a.ID, service.PotvrdaOvjere{Otisak: vidio.Otisak, Primatelji: true}); err == nil || !strings.Contains(err.Error(), "otkad je stranica otvorena") {
		t.Fatalf("ovjera sa zastarjelim popisom: %v", err)
	}
	if nacrt, _ := o.akti.Get(ctx, a.ID); nacrt.Ovjeren() || strings.Contains(imena(nacrt.Primatelji), "Ravnateljstvo") {
		t.Fatalf("odbijena ovjera promijenila je nacrt: %+v", nacrt)
	}
	sada := o.akti.PregledPrimatelja(ctx, a)
	if len(sada.Dodani) != 1 || sada.Dodani[0].Naziv != "Ravnateljstvo civilne zaštite" || !sada.Razlikuje() {
		t.Fatalf("razlika prema nacrtu: %+v", sada)
	}
	ovjeren, _, err := o.akti.Ovjeri(ctx, o.ovlasti, o.rukovod, a.ID, service.PotvrdaOvjere{Otisak: sada.Otisak, Primatelji: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(imena(ovjeren.Primatelji), "Ravnateljstvo civilne zaštite") || !ovjeren.ImaPotvrdu(models.PotvrdaPrimatelji) || len(ovjeren.UpozorenjaPrimatelja) == 0 {
		t.Errorf("ovjeren akt nosi popis složen pri ovjeri i potvrdu: %v, %+v", imena(ovjeren.Primatelji), ovjeren.Potvrde)
	}
	if service.ProvjeriPotpis(ovjeren) != service.PotpisNema && ovjeren.Potpis != "" {
		t.Error("okolina nema ključa čvora")
	}
	if procitan, _ := o.akti.Get(ctx, a.ID); procitan.Potvrde[0].Tko != "Pero Perić" || !strings.Contains(procitan.Potvrde[0].Tekst, "bez upisanog teritorija") {
		t.Errorf("zabilježena potvrda: %+v", procitan.Potvrde)
	}
}

// Ponovno slaganje popisa u nacrtu mijenja nacrt, pa smije samo njegov
// autor dok smije sastavljati nacrte; vodočuvar i rukovoditelj koji akt
// ovjerava više ne (namjerno promijenjeno prema Odlukama (8. 10. 2026.),
// pitanje 18). Ovjeren akt zadržava popis s kojim je ovjeren.
func TestPonovnoSlaganjePrimatelja(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	a, err := o.akti.Pripremi(ctx, o.dezOvl, o.dezurni, service.ZahtjevAkta{StationID: o.letva.ID.String(), Radnja: models.AktUspostava, Stupanj: models.PhasePrep, Vrijedi: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := o.akti.Spremi(ctx, o.dezOvl, a); err != nil {
		t.Fatal(err)
	}
	uzTeritorij(t, o)
	stranac := &models.User{ID: uuid.New(), FullName: "Marko Markić"}
	if _, err := o.akti.PonovnoSloziPrimatelje(ctx, models.NewUserPermissions(*stranac), stranac, a.ID); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("tko ne radi s nacrtom: %v", err)
	}
	if _, err := o.akti.PonovnoSloziPrimatelje(ctx, o.dezOvl, o.dezurni, "nema-ga"); err == nil {
		t.Error("akt kojeg nema")
	}
	for ime, z := range map[string]struct {
		perms *models.UserPermissions
		u     *models.User
	}{
		"vodočuvar dionice akta":     {o.vodOvl, o.vodocuv},
		"rukovoditelj koji ovjerava": {o.ovlasti, o.rukovod},
	} {
		if _, err := o.akti.PonovnoSloziPrimatelje(ctx, z.perms, z.u, a.ID); !errors.Is(err, service.ErrUnauthorized) {
			t.Errorf("%s: %v", ime, err)
		}
	}
	nov, err := o.akti.PonovnoSloziPrimatelje(ctx, o.dezOvl, o.dezurni, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(imena(nov.Primatelji), "Civilna zaštita Vukovar") || len(nov.UpozorenjaPrimatelja) != 1 || !strings.Contains(nov.UpozorenjaPrimatelja[0], "P.1.2") {
		t.Errorf("ponovno složen popis: %s, upozorenja %v", imena(nov.Primatelji), nov.UpozorenjaPrimatelja)
	}
	if spremljen, _ := o.akti.Get(ctx, a.ID); imena(spremljen.Primatelji) != imena(nov.Primatelji) {
		t.Error("ponovno složen popis nije spremljen u nacrt")
	}
	if _, _, err := o.akti.Ovjeri(ctx, o.ovlasti, o.rukovod, a.ID, service.PotvrdaOvjere{Primatelji: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := o.akti.PonovnoSloziPrimatelje(ctx, o.dezOvl, o.dezurni, a.ID); err == nil || !strings.Contains(err.Error(), "ovjeren") {
		t.Errorf("ovjeren akt: %v", err)
	}
	if _, err := ledger.New(o.baza, "test").Latest(ctx, repository.EntityAkti, a.ID); err != nil {
		t.Error(err)
	}
}

// Sken nosi popis s ispisa nacrta: popis se ne mijenja, a potvrda treba dok
// stoji upozorenje nacrta ili kad bi popis složen sada bio drukčiji
func TestSkenIPopisPrimatelja(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	a, err := o.akti.Pripremi(ctx, o.dezOvl, o.dezurni, service.ZahtjevAkta{StationID: o.letva.ID.String(), Radnja: models.AktUspostava, Stupanj: models.PhasePrep, Vrijedi: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := o.akti.Spremi(ctx, o.dezOvl, a); err != nil {
		t.Fatal(err)
	}
	ispis := imena(a.Primatelji)
	uprava := &models.UserPermissions{IsGlobalAdmin: true, User: *o.rukovod}
	if err := o.akti.SpremiPrimatelja(ctx, uprava, &models.Primatelj{Sektor: "P", Naziv: "Ravnateljstvo civilne zaštite", Aktivan: true}); err != nil {
		t.Fatal(err)
	}
	sken := []byte("%PDF-1.4\n% sken\n%%EOF\n")
	var pitanje *service.TrebaPotvrduError
	if _, _, err := o.akti.UcitajSkenirani(ctx, o.ovlasti, o.rukovod, a.ID, sken, o.rukovod.ID.String(), service.PotvrdaOvjere{}); !errors.As(err, &pitanje) ||
		!strings.Contains(err.Error(), "drukčiji od onoga na ispisu (dodano 1, uklonjeno 0)") {
		t.Fatalf("sken bez potvrde: %v", err)
	}
	ovjeren, _, err := o.akti.UcitajSkenirani(ctx, o.ovlasti, o.rukovod, a.ID, sken, o.rukovod.ID.String(), service.PotvrdaOvjere{Primatelji: true})
	if err != nil {
		t.Fatal(err)
	}
	if imena(ovjeren.Primatelji) != ispis || !ovjeren.ImaPotvrdu(models.PotvrdaPrimatelji) || !strings.Contains(ovjeren.Potvrde[0].Tekst, "Sken ovjeren s popisom primatelja s ispisa") {
		t.Errorf("sken ovjerava popis s ispisa: %s, %+v", imena(ovjeren.Primatelji), ovjeren.Potvrde)
	}
	if _, _, err := o.akti.UcitajSkenirani(ctx, o.ovlasti, o.rukovod, a.ID, nil, o.rukovod.ID.String(), service.PotvrdaOvjere{Primatelji: true}); err == nil {
		t.Error("ponovni sken ovjerenog akta")
	}
}

// Nacrt bez upisanog upozorenja (sastavljen prije nadogradnje ili na čvoru
// 0.0.34) ne ovjerava se skenom bez potvrde dok upozorenje uz popis složen
// sada stoji, i kad je popis jednak onome na ispisu: sken pita isto što i
// ovjera u goCOP-u (Odluke (8. 10. 2026.), pitanje 19)
func TestSkenNacrtaBezUpisanogUpozorenjaTraziPotvrdu(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	a, err := o.akti.Pripremi(ctx, o.dezOvl, o.dezurni, service.ZahtjevAkta{StationID: o.letva.ID.String(), Radnja: models.AktUspostava, Stupanj: models.PhasePrep, Vrijedi: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := o.akti.Spremi(ctx, o.dezOvl, a); err != nil {
		t.Fatal(err)
	}
	if _, err := o.baza.Exec(`UPDATE akti SET upozorenja_primatelja = '' WHERE id = ?`, a.ID); err != nil {
		t.Fatal(err)
	}
	nacrt, _ := o.akti.Get(ctx, a.ID)
	pregled := o.akti.PregledPrimatelja(ctx, nacrt)
	if len(nacrt.UpozorenjaPrimatelja) != 0 || len(pregled.Upozorenja) == 0 || pregled.Razlikuje() {
		t.Fatalf("nacrt bez upozorenja, a upozorenje sada stoji uz isti popis: %v, %+v", nacrt.UpozorenjaPrimatelja, pregled)
	}
	sken := []byte("%PDF-1.4\n% sken\n%%EOF\n")
	var pitanje *service.TrebaPotvrduError
	if _, _, err := o.akti.UcitajSkenirani(ctx, o.ovlasti, o.rukovod, a.ID, sken, o.rukovod.ID.String(), service.PotvrdaOvjere{}); !errors.As(err, &pitanje) ||
		!strings.Contains(err.Error(), "bez upisanog teritorija") {
		t.Fatalf("sken bez potvrde dok upozorenje stoji: %v", err)
	}
	if x, _ := o.akti.Get(ctx, a.ID); x.Ovjeren() {
		t.Fatal("sken bez potvrde ovjerio je akt")
	}
	ovjeren, _, err := o.akti.UcitajSkenirani(ctx, o.ovlasti, o.rukovod, a.ID, sken, o.rukovod.ID.String(), service.PotvrdaOvjere{Primatelji: true})
	if err != nil {
		t.Fatal(err)
	}
	if !ovjeren.Ovjeren() || !ovjeren.ImaPotvrdu(models.PotvrdaPrimatelji) || !strings.Contains(ovjeren.Potvrde[0].Tekst, "bez upisanog teritorija") {
		t.Errorf("potvrda uz sken: %+v", ovjeren.Potvrde)
	}
}
