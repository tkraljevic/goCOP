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
// stoji traži potvrdu, pri ovjeri se popis slaže ponovno, a firme i
// rukovoditelji idu za sva branjena područja dionica akta.

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

// Akt po letvi koja pokriva dionice dvaju područja: firme i rukovoditelji
// oba područja na popisu su, i ispostava i izvođač upisani u registar za
// drugo područje; ostali unosi registra za područje ostaju uz područje akta
// (Odluke (8. 10. 2026.), pitanje 19)
func TestPrimateljiSvihPodrucjaDionicaAkta(t *testing.T) {
	o := novaOkolinaAkta(t)
	uzTeritorij(t, o)
	ctx := context.Background()
	rec := ledger.New(o.baza, "test")
	for _, q := range []string{
		`INSERT INTO contractors (id, name, email, active, updated_at) VALUES ('f2', 'Vodogradnja Drugo d.o.o.', 'firma@primjer.hr', 1, '2026-01-01')`,
		`INSERT INTO contractor_assignments (id, contractor_id, sector_id, area_id, updated_at) VALUES ('fa2', 'f2', 'P', 2, '2026-01-01')`,
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

	a, err := o.akti.Pripremi(ctx, o.ovlasti, o.rukovod, service.ZahtjevAkta{StationID: o.letva.ID.String(), Radnja: models.AktUspostava, Stupanj: models.PhasePrep})
	if err != nil {
		t.Fatal(err)
	}
	if a.AreaID != 1 || len(a.Dionice) != 3 {
		t.Fatalf("akt letve s dionicama dvaju područja: područje %d, dionice %v", a.AreaID, a.Sifre())
	}
	popis := imena(a.Primatelji)
	for _, d := range []string{"Glavni centar obrane od poplava", "Ugovorna osoba područja 1", "Vodogradnja Drugo d.o.o.", "Pero Perić, Rukovoditelj BP 1",
		"Ana Anić, Rukovoditeljica BP 2", "Civilna zaštita Vukovar", "Pismohrana", "Izvođač područja 2 iz registra"} {
		if !strings.Contains(popis, d) {
			t.Errorf("na popisu nema %q: %s", d, popis)
		}
	}
	for _, ne := range []string{"Registar samo za područje 2", "Ugovorna osoba područja 2", "Izvođač područja 3 iz registra", "Izvođač područja 2 od izvanredne"} {
		if strings.Contains(popis, ne) {
			t.Errorf("na popisu ne smije biti %q: %s", ne, popis)
		}
	}
	if len(a.UpozorenjaPrimatelja) != 1 || !strings.Contains(a.UpozorenjaPrimatelja[0], "bez upisanog teritorija (naselja i općine): P.1.2, P.2.1") {
		t.Errorf("upozorenje o dionicama bez teritorija: %v", a.UpozorenjaPrimatelja)
	}
	// izvanredno stanje: župan i grad ugroženog područja, s e-poštom, i
	// izvođač drugog područja upisan od izvanredne obrane
	a.Stupanj = models.PhaseState
	p := o.akti.PregledPrimatelja(ctx, a)
	if s := imena(p.Primatelji); !strings.Contains(s, "Župan, Vukovarsko-srijemska županija") || !strings.Contains(s, "Grad Primjerovo") {
		t.Errorf("samouprava izvanrednog stanja: %s", s)
	}
	if len(p.Dodani) != 3 || p.Dodani[0].Naziv != "Izvođač područja 2 od izvanredne" || p.Dodani[1].Email != "zupan@primjer.hr" || p.Dodani[2].Email != "grad@primjer.hr" ||
		len(p.Uklonjeni) != 0 || p.OpisRazlike() != "dodano 3, uklonjeno 0" {
		t.Errorf("razlika prema nacrtu: +%v −%v", p.Dodani, p.Uklonjeni)
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
	a, err := o.akti.Pripremi(ctx, o.ovlasti, o.rukovod, service.ZahtjevAkta{StationID: o.letva.ID.String(), Radnja: models.AktUspostava, Stupanj: models.PhasePrep, Vrijedi: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := o.akti.Spremi(ctx, o.ovlasti, a); err != nil {
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

// Ponovno slaganje popisa u nacrtu: smije tko radi s nacrtom, ovjeren akt
// zadržava popis s kojim je ovjeren
func TestPonovnoSlaganjePrimatelja(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	a, err := o.akti.Pripremi(ctx, o.ovlasti, o.rukovod, service.ZahtjevAkta{StationID: o.letva.ID.String(), Radnja: models.AktUspostava, Stupanj: models.PhasePrep, Vrijedi: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := o.akti.Spremi(ctx, o.ovlasti, a); err != nil {
		t.Fatal(err)
	}
	uzTeritorij(t, o)
	stranac := &models.User{ID: uuid.New(), FullName: "Marko Markić"}
	if _, err := o.akti.PonovnoSloziPrimatelje(ctx, models.NewUserPermissions(*stranac), stranac, a.ID); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("tko ne radi s nacrtom: %v", err)
	}
	if _, err := o.akti.PonovnoSloziPrimatelje(ctx, o.ovlasti, o.rukovod, "nema-ga"); err == nil {
		t.Error("akt kojeg nema")
	}
	nov, err := o.akti.PonovnoSloziPrimatelje(ctx, o.vodOvl, o.vodocuv, a.ID)
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
	if _, err := o.akti.PonovnoSloziPrimatelje(ctx, o.ovlasti, o.rukovod, a.ID); err == nil || !strings.Contains(err.Error(), "ovjeren") {
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
	a, err := o.akti.Pripremi(ctx, o.ovlasti, o.rukovod, service.ZahtjevAkta{StationID: o.letva.ID.String(), Radnja: models.AktUspostava, Stupanj: models.PhasePrep, Vrijedi: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := o.akti.Spremi(ctx, o.ovlasti, a); err != nil {
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
	a, err := o.akti.Pripremi(ctx, o.ovlasti, o.rukovod, service.ZahtjevAkta{StationID: o.letva.ID.String(), Radnja: models.AktUspostava, Stupanj: models.PhasePrep, Vrijedi: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := o.akti.Spremi(ctx, o.ovlasti, a); err != nil {
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
