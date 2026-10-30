package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// skladisteMetkovic upisuje skladište sektora F na području 40
func skladisteMetkovic(t *testing.T, s *MtsService, ctx context.Context) *models.Skladiste {
	t.Helper()
	sk := &models.Skladiste{Sektor: "F", AreaID: 40, Naziv: "Skladište Metković", Aktivno: true}
	if err := s.SpremiSkladiste(ctx, &models.UserPermissions{IsGlobalAdmin: true}, sk); err != nil {
		t.Fatal(err)
	}
	return sk
}

// skladistarPodrucja daje ovlasti skladištara jednog područja
func skladistarPodrucja(sektor string, areaID int) *models.UserPermissions {
	return &models.UserPermissions{User: models.User{Duties: []models.Duty{
		{Role: models.RoleWarehouseKeeper, AreaID: &areaID, SectorID: &sektor, IsActive: true}}}}
}

// Uprava drugog sektora ili drugog područja ne mijenja postojeće skladište,
// ni kad u obrascu pošalje svoj sektor i područje; uprava sektora i
// područja skladišta i dalje ga uređuje.
func TestPostojeceSkladisteUredjujeSamoNjegovaUprava(t *testing.T) {
	s, osijek, ctx, _, uprava, baza := pripremiMtsIBazu(t)
	if _, err := baza.Exec(`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (35, 'B', 'Karašica', 'VGI Karašica', 'Valpovo')`); err != nil {
		t.Fatal(err)
	}
	upravaF := &models.UserPermissions{AdminSectors: map[string]bool{"F": true}}
	izmjena := &models.Skladiste{ID: osijek.ID, Sektor: "F", AreaID: 40, Naziv: "Drugo ime", Aktivno: false, CreatedAt: osijek.CreatedAt}
	if err := s.SpremiSkladiste(ctx, upravaF, izmjena); err == nil {
		t.Fatal("uprava sektora F ne smije mijenjati skladište sektora B")
	}
	cur, err := s.Skladiste(ctx, osijek.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cur.Sektor != "B" || cur.AreaID != 34 || cur.Naziv != osijek.Naziv || !cur.Aktivno {
		t.Fatalf("skladište se promijenilo: %+v", cur)
	}

	// uprava područja 35 istog sektora ne seli skladište područja 34 k sebi
	upravaBP35 := &models.UserPermissions{AdminAreas: map[int]bool{35: true}}
	izmjena = &models.Skladiste{ID: osijek.ID, Sektor: "B", AreaID: 35, Naziv: "Drugo ime", Aktivno: true, CreatedAt: osijek.CreatedAt}
	if err := s.SpremiSkladiste(ctx, upravaBP35, izmjena); err == nil {
		t.Fatal("uprava područja 35 ne smije mijenjati skladište područja 34")
	}
	if cur, err := s.Skladiste(ctx, osijek.ID); err != nil || cur.AreaID != 34 || cur.Naziv != osijek.Naziv {
		t.Fatalf("skladište se promijenilo: %+v", cur)
	}

	// kontrola: uprava područja 34 preimenuje svoje skladište
	upravaBP34 := &models.UserPermissions{AdminAreas: map[int]bool{34: true}}
	svojePodrucje := &models.Skladiste{ID: osijek.ID, Sektor: "B", AreaID: 34, Naziv: "Skladište Splavarska", Aktivno: true, CreatedAt: osijek.CreatedAt}
	if err := s.SpremiSkladiste(ctx, upravaBP34, svojePodrucje); err != nil {
		t.Fatalf("uprava područja 34 uređuje svoje skladište: %v", err)
	}

	// kontrola: uprava sektora B preimenuje svoje skladište
	svoje := &models.Skladiste{ID: osijek.ID, Sektor: "B", AreaID: 34, Naziv: "Skladište Osijek", Aktivno: true, CreatedAt: osijek.CreatedAt}
	if err := s.SpremiSkladiste(ctx, uprava, svoje); err != nil {
		t.Fatalf("uprava sektora B uređuje svoje skladište: %v", err)
	}
	if cur, err := s.Skladiste(ctx, osijek.ID); err != nil || cur.Naziv != "Skladište Osijek" {
		t.Fatalf("naziv nije upisan: %q", cur.Naziv)
	}
}

// Sektor postojećeg skladišta mijenja samo globalni administrator, i kad
// osoba vodi oba sektora.
func TestSektorSkladistaMijenjaSamoGlobalniAdministrator(t *testing.T) {
	s, osijek, ctx, _, _ := pripremiMts(t)
	obaSektora := &models.UserPermissions{AdminSectors: map[string]bool{"B": true, "F": true}}
	seli := &models.Skladiste{ID: osijek.ID, Sektor: "F", AreaID: 40, Naziv: osijek.Naziv, Aktivno: true, CreatedAt: osijek.CreatedAt}
	err := s.SpremiSkladiste(ctx, obaSektora, seli)
	if err == nil || !strings.Contains(err.Error(), "globalni administrator") {
		t.Fatalf("uprava dvaju sektora ne mijenja sektor skladišta, greška: %v", err)
	}
	if cur, err := s.Skladiste(ctx, osijek.ID); err != nil || cur.Sektor != "B" {
		t.Fatalf("sektor se promijenio: %s", cur.Sektor)
	}

	// kontrola: globalni administrator seli skladište
	seli = &models.Skladiste{ID: osijek.ID, Sektor: "F", AreaID: 40, Naziv: osijek.Naziv, Aktivno: true, CreatedAt: osijek.CreatedAt}
	if err := s.SpremiSkladiste(ctx, &models.UserPermissions{IsGlobalAdmin: true}, seli); err != nil {
		t.Fatalf("globalni administrator mijenja sektor skladišta: %v", err)
	}
	if cur, err := s.Skladiste(ctx, osijek.ID); err != nil || cur.Sektor != "F" || cur.AreaID != 40 {
		t.Fatalf("skladište nije preseljeno: %+v", cur)
	}
}

// Branjeno područje skladišta mora biti u sektoru skladišta: uprava
// područja 40 (sektor F) ne upisuje skladište pod sektor B.
func TestPodrucjeSkladistaMoraBitiUSektoru(t *testing.T) {
	s, _, ctx, _, uprava := pripremiMts(t)
	upravaBP40 := &models.UserPermissions{AdminAreas: map[int]bool{40: true}}
	if err := s.SpremiSkladiste(ctx, upravaBP40, &models.Skladiste{Sektor: "B", AreaID: 40, Naziv: "Pod tuđim sektorom", Aktivno: true}); err == nil {
		t.Fatal("područje 40 nije u sektoru B")
	}
	if err := s.SpremiSkladiste(ctx, &models.UserPermissions{IsGlobalAdmin: true}, &models.Skladiste{Sektor: "B", AreaID: 40, Naziv: "Ni administratoru", Aktivno: true}); err == nil {
		t.Fatal("ni globalni administrator ne upisuje područje izvan sektora")
	}
	if err := s.SpremiSkladiste(ctx, uprava, &models.Skladiste{Sektor: "B", AreaID: 99, Naziv: "Nepostojeće područje", Aktivno: true}); err == nil {
		t.Fatal("nepostojeće područje mora biti greška")
	}
	sva, err := s.Skladista(ctx, "B", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(sva) != 1 {
		t.Fatalf("u sektoru B smije biti samo Osijek, a ima %d skladišta", len(sva))
	}

	// kontrola: uprava područja 40 upisuje skladište u svoj sektor
	if err := s.SpremiSkladiste(ctx, upravaBP40, &models.Skladiste{Sektor: "F", AreaID: 40, Naziv: "Skladište Opuzen", Aktivno: true}); err != nil {
		t.Fatalf("uprava područja 40 upisuje skladište sektora F: %v", err)
	}
	// skladište bez područja upisuje uprava sektora
	if err := s.SpremiSkladiste(ctx, uprava, &models.Skladiste{Sektor: "B", Naziv: "Skladište sektora", Aktivno: true}); err != nil {
		t.Fatalf("skladište bez područja: %v", err)
	}
	// novo skladište s unaprijed zadanim ID-em nema postojećeg zapisa
	if err := s.SpremiSkladiste(ctx, uprava, &models.Skladiste{ID: uuid.New().String(), Sektor: "B", AreaID: 34, Naziv: "Zadani ID", Aktivno: true}); err != nil {
		t.Fatalf("novo skladište sa zadanim ID-em: %v", err)
	}
}

// Bez registra dionica servis ne zna sektor područja, pa ga ne provjerava
func TestPodrucjeUSektoruBezRegistra(t *testing.T) {
	s, _, ctx, _, _ := pripremiMts(t)
	s.sections = nil
	if err := s.podrucjeUSektoru(ctx, 40, "B"); err != nil {
		t.Fatalf("bez registra nema provjere: %v", err)
	}
}

// Nacrt inventure ostaje na svom skladištu: skladište se uzima iz baze, a ne
// iz obrasca, pa skladištar i uprava drugog sektora ne mijenjaju tuđi nacrt.
func TestNacrtInventureOstajeNaSvomSkladistu(t *testing.T) {
	s, osijek, ctx, u, uprava := pripremiMts(t)
	metkovic := skladisteMetkovic(t, s, ctx)
	dan := pocetakDana(time.Now().In(models.Zagreb)).AddDate(0, 0, -3)
	p, err := s.PredlozakPopisa(ctx, osijek.ID, dan)
	if err != nil {
		t.Fatal(err)
	}
	p.Stavke[0].Utvrdjeno = 7
	if err := s.SpremiPopis(ctx, u, uprava, p); err != nil {
		t.Fatal(err)
	}
	drugi := &models.User{ID: uuid.New(), FullName: "Skladištar Metković"}
	for ime, perms := range map[string]*models.UserPermissions{
		"uprava sektora F":       {AdminSectors: map[string]bool{"F": true}},
		"skladištar područja 40": skladistarPodrucja("F", 40),
	} {
		tudji := &models.Popis{ID: p.ID, SkladisteID: metkovic.ID, Dan: dan}
		if err := s.SpremiPopis(ctx, drugi, perms, tudji); err == nil {
			t.Fatalf("%s ne smije mijenjati nacrt inventure Osijeka", ime)
		}
	}
	cur, err := s.Popis(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cur.SkladisteID != osijek.ID || cur.Sektor != "B" || len(cur.Stavke) == 0 || cur.Stavke[0].Utvrdjeno != 7 {
		t.Fatalf("nacrt se promijenio: skladište %s, sektor %s, stavki %d", cur.SkladisteID, cur.Sektor, len(cur.Stavke))
	}

	// kontrola: uprava B mijenja svoj nacrt; skladište iz obrasca se ne gleda
	p.Stavke[0].Utvrdjeno = 8
	p.SkladisteID = metkovic.ID
	if err := s.SpremiPopis(ctx, u, uprava, p); err != nil {
		t.Fatalf("uprava B mijenja svoj nacrt: %v", err)
	}
	if cur, err = s.Popis(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if cur.SkladisteID != osijek.ID || cur.Stavke[0].Utvrdjeno != 8 {
		t.Fatalf("nacrt mora ostati na Osijeku s novom količinom: %s, %v", cur.SkladisteID, cur.Stavke[0].Utvrdjeno)
	}

	// kontrola: skladištar područja 34 mijenja nacrt svog skladišta
	p.Stavke[0].Utvrdjeno = 9
	if err := s.SpremiPopis(ctx, u, skladistarPodrucja("B", 34), p); err != nil {
		t.Fatalf("skladištar područja 34 mijenja svoj nacrt: %v", err)
	}
	if cur, err = s.Popis(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if cur.SkladisteID != osijek.ID || cur.Stavke[0].Utvrdjeno != 9 {
		t.Fatalf("nacrt skladištara mora ostati na Osijeku: %s, %v", cur.SkladisteID, cur.Stavke[0].Utvrdjeno)
	}

	// nepostojeći popis
	if err := s.SpremiPopis(ctx, u, uprava, &models.Popis{ID: uuid.New().String(), SkladisteID: osijek.ID, Dan: dan}); err == nil ||
		!strings.Contains(err.Error(), "nije pronađen") {
		t.Fatalf("nepostojeći popis: %v", err)
	}
}

// S terena se vraća, troši i puni samo ono što je onamo izdao sektor
// skladišta. Izdavanje na teren drugog sektora ostaje kakvo je bilo.
func TestTerenSeBrojiPoSektoru(t *testing.T) {
	s, osijek, ctx, u, uprava := pripremiMts(t)
	metkovic := skladisteMetkovic(t, s, ctx)
	skladistarB := skladistarPodrucja("B", 34)
	for _, z := range []Zahvat{
		{Vrsta: models.PrometPocetno, SkladisteID: osijek.ID, VrstaID: "lopata", Kolicina: 10},
		{Vrsta: models.PrometPocetno, SkladisteID: osijek.ID, VrstaID: "vrece-50x80", Oblik: models.OblikPrazno, Kolicina: 5},
		{Vrsta: models.PrometIzdano, SkladisteID: osijek.ID, VrstaID: "lopata", Kolicina: 10, AreaID: 34, SectionCode: "B.34.1"},
		{Vrsta: models.PrometIzdano, SkladisteID: osijek.ID, VrstaID: "vrece-50x80", Oblik: models.OblikPrazno, Kolicina: 5, AreaID: 34},
	} {
		if _, err := s.Provedi(ctx, u, skladistarB, z); err != nil {
			t.Fatalf("%s sektora B: %v", z.Vrsta, err)
		}
	}

	skladistarF := skladistarPodrucja("F", 40)
	for _, z := range []Zahvat{
		{Vrsta: models.PrometPovrat, SkladisteID: metkovic.ID, VrstaID: "lopata", Kolicina: 10, AreaID: 34, SectionCode: "B.34.1"},
		{Vrsta: models.PrometUtrosak, SkladisteID: metkovic.ID, VrstaID: "lopata", Kolicina: 1, AreaID: 34, SectionCode: "B.34.1"},
		{Vrsta: models.PrometPunjenje, SkladisteID: metkovic.ID, VrstaID: "vrece-50x80", Kolicina: 1, AreaID: 34},
	} {
		_, err := s.Provedi(ctx, u, skladistarF, z)
		if err == nil || !strings.Contains(err.Error(), "na terenu stoji 0") {
			t.Fatalf("%s skladištara F sa sredstvima sektora B mora biti odbijen, greška: %v", z.Vrsta, err)
		}
	}
	if got := kolicinaU(t, s, ctx, metkovic.ID, "lopata"); got != 0 {
		t.Fatalf("u Metkoviću ne smije biti lopata, a ima %v", got)
	}

	// izdavanje na teren drugog sektora prolazi, a sektor F vraća svoje
	if _, err := s.Provedi(ctx, u, skladistarF, Zahvat{Vrsta: models.PrometPocetno, SkladisteID: metkovic.ID, VrstaID: "lopata", Kolicina: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Provedi(ctx, u, skladistarF, Zahvat{Vrsta: models.PrometIzdano, SkladisteID: metkovic.ID, VrstaID: "lopata", Kolicina: 2, AreaID: 34, SectionCode: "B.34.1"}); err != nil {
		t.Fatalf("izdavanje sektora F na teren sektora B: %v", err)
	}
	if _, err := s.Provedi(ctx, u, skladistarF, Zahvat{Vrsta: models.PrometPovrat, SkladisteID: metkovic.ID, VrstaID: "lopata", Kolicina: 3, AreaID: 34, SectionCode: "B.34.1"}); err == nil {
		t.Fatal("sektor F na terenu ima 2 lopate, ne 3")
	}
	if _, err := s.Provedi(ctx, u, skladistarF, Zahvat{Vrsta: models.PrometPovrat, SkladisteID: metkovic.ID, VrstaID: "lopata", Kolicina: 2, AreaID: 34, SectionCode: "B.34.1"}); err != nil {
		t.Fatalf("sektor F vraća svoje lopate: %v", err)
	}
	if got := kolicinaU(t, s, ctx, metkovic.ID, "lopata"); got != 2 {
		t.Fatalf("u Metkoviću moraju biti 2 lopate, a ima %v", got)
	}

	// redak sektora F na terenu sektora B ne ulazi u zalihu sektora B
	if err := s.repo.SavePromet(ctx, []models.Promet{{Datum: time.Now().In(models.Zagreb), VrstaID: "lopata", Oblik: models.OblikOsnovni,
		Vrsta: models.PrometIzdano, Sektor: "F", AreaID: 34, SectionCode: "B.34.1", Kolicina: 5, VezaID: uuid.New().String(),
		UserID: u.ID.String(), UserName: u.FullName}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPovrat, SkladisteID: osijek.ID, VrstaID: "lopata", Kolicina: 12, AreaID: 34, SectionCode: "B.34.1"}); err == nil {
		t.Fatal("sektor B na terenu ima 10 lopata, ne 15")
	}

	// kontrola: skladištar područja 34 puni, troši i vraća svoje sa svog terena
	for _, z := range []Zahvat{
		{Vrsta: models.PrometPunjenje, SkladisteID: osijek.ID, VrstaID: "vrece-50x80", Kolicina: 5, AreaID: 34},
		{Vrsta: models.PrometUtrosak, SkladisteID: osijek.ID, VrstaID: "lopata", Kolicina: 1, AreaID: 34, SectionCode: "B.34.1"},
		{Vrsta: models.PrometPovrat, SkladisteID: osijek.ID, VrstaID: "lopata", Kolicina: 9, AreaID: 34, SectionCode: "B.34.1"},
	} {
		if _, err := s.Provedi(ctx, u, skladistarB, z); err != nil {
			t.Fatalf("%s skladištara područja 34: %v", z.Vrsta, err)
		}
	}
	if got := kolicinaU(t, s, ctx, osijek.ID, "lopata"); got != 9 {
		t.Fatalf("u Osijeku mora biti 9 lopata, a ima %v", got)
	}
}
