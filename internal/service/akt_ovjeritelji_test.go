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

// Tko smije ovjeriti akt, tko se nudi kao potpisnik skena, tko briše nacrt
// i tko vidi akte u popisu.

// duznostOvjere je aktivna dužnost zadane uloge u sektoru i području
func duznostOvjere(uloga models.Role, sektor string, podrucje int) models.Duty {
	d := models.Duty{Role: uloga, IsActive: true}
	if sektor != "" {
		d.SectorID = &sektor
	}
	if podrucje > 0 {
		d.AreaID = &podrucje
	}
	return d
}

func ovlastiDuznosti(duznosti ...models.Duty) *models.UserPermissions {
	return &models.UserPermissions{User: models.User{Duties: duznosti}}
}

// Akt ovjeravaju rukovoditelji obrane po Državnom planu: pripremno i
// redovnu rukovoditelj branjenog područja ili razina sektora, izvanrednu
// obranu samo razina sektora, izvanredno stanje i područje (hitni slučaj).
// Zamjenik rukovoditelja sektora za područje je razina sektora samo za svoje
// područje. Neaktivna i istekla dužnost, dužnost drugog sektora ili
// područja, dionica i samo upravljanje područjem ne daju ovjeru.
func TestTkoSmijeOvjeritiAkt(t *testing.T) {
	akti := service.NewAktService(nil, nil, nil, nil, nil, nil, nil, "test")
	akt := func(stupanj models.DefensePhase) *models.Akt {
		return &models.Akt{Sektor: "P", AreaID: 1, Stupanj: stupanj}
	}
	istekla := duznostOvjere(models.RoleAreaLeader, "P", 1)
	rok := time.Date(2020, 1, 1, 0, 0, 0, 0, models.Zagreb)
	istekla.ExpiresAt = &rok
	neaktivna := duznostOvjere(models.RoleSectorLeader, "P", 0)
	neaktivna.IsActive = false

	sviStupnjevi := []models.DefensePhase{models.PhasePrep, models.PhaseRegular, models.PhaseEmergency, models.PhaseState}
	for _, x := range []struct {
		opis  string
		perms *models.UserPermissions
		smije []models.DefensePhase
	}{
		{"bez ovlasti", nil, nil},
		{"uprava organizacije", &models.UserPermissions{IsGlobalAdmin: true}, sviStupnjevi},
		{"rukovoditelj sektora", ovlastiDuznosti(duznostOvjere(models.RoleSectorLeader, "P", 0)), sviStupnjevi},
		{"zamjenik rukovoditelja sektora", ovlastiDuznosti(duznostOvjere(models.RoleSectorDeputy, "P", 0)), sviStupnjevi},
		{"zamjenik glavnog rukovoditelja u sektoru", ovlastiDuznosti(duznostOvjere(models.RoleSectorMainDeputy, "P", 0)), sviStupnjevi},
		{"zamjenik sektora za područje 1", ovlastiDuznosti(duznostOvjere(models.RoleSectorAreaDeputy, "P", 1)), sviStupnjevi},
		{"zamjenik sektora za područje 2", ovlastiDuznosti(duznostOvjere(models.RoleSectorAreaDeputy, "P", 2)), nil},
		{"rukovoditelj drugog sektora", ovlastiDuznosti(duznostOvjere(models.RoleSectorLeader, "Q", 0)), nil},
		{"rukovoditelj područja 1", ovlastiDuznosti(duznostOvjere(models.RoleAreaLeader, "P", 1)),
			[]models.DefensePhase{models.PhasePrep, models.PhaseRegular, models.PhaseState}},
		{"zamjenik rukovoditelja područja 1", ovlastiDuznosti(duznostOvjere(models.RoleAreaDeputy, "P", 1)),
			[]models.DefensePhase{models.PhasePrep, models.PhaseRegular, models.PhaseState}},
		{"rukovoditelj područja 2", ovlastiDuznosti(duznostOvjere(models.RoleAreaLeader, "P", 2)), nil},
		{"istekla dužnost rukovoditelja područja", ovlastiDuznosti(istekla), nil},
		{"neaktivna dužnost rukovoditelja sektora", ovlastiDuznosti(neaktivna), nil},
		{"rukovoditelj dionice", ovlastiDuznosti(duznostOvjere(models.RoleSectionLeader, "P", 1)), nil},
		{"uprava područja bez dužnosti obrane", &models.UserPermissions{AdminAreas: map[int]bool{1: true}, AllowedAreas: map[int]bool{1: true}}, nil},
	} {
		for _, stupanj := range sviStupnjevi {
			zeli := false
			for _, s := range x.smije {
				zeli = zeli || s == stupanj
			}
			if got := akti.SmijeOvjeriti(x.perms, akt(stupanj)); got != zeli {
				t.Errorf("%s, %s: smije %v, želi %v", x.opis, stupanj, got, zeli)
			}
		}
	}
	if akti.SmijeOvjeriti(&models.UserPermissions{IsGlobalAdmin: true}, nil) {
		t.Error("ovjera bez akta")
	}
}

// Za sken ručno potpisanog akta nude se osobe sektora koje akt smiju
// ovjeriti, poredane po imenu. Uprava organizacije se ne nudi ni kad ima
// dužnost u sektoru; vodočuvar i dežurni operater se ne nude. Kad se osobe
// ne daju pročitati, ne nudi se nitko.
func TestMoguPotpisatiSkeniraniAkt(t *testing.T) {
	o := novaOkolinaAkta(t)
	userRepo := repository.NewUserRepository(o.baza, ledger.New(o.baza, "test"))
	nova := func(korisnik, ime string, globalna bool, d models.Duty) {
		t.Helper()
		d.Title, d.IsPrimary = "Dužnost "+ime, true
		u := &models.User{ID: uuid.New(), Username: korisnik, FullName: ime, IsActive: true, IsGlobalAdmin: globalna}
		if err := userRepo.CreateUser(u, &d); err != nil {
			t.Fatal(err)
		}
	}
	nova("aanic", "Ana Anić", false, duznostOvjere(models.RoleAreaDeputy, "P", 1))
	nova("iivic", "Ivo Ivić", false, duznostOvjere(models.RoleSectorLeader, "P", 0))
	nova("mmarkic", "Marko Markić", true, duznostOvjere(models.RoleSectorLeader, "P", 0))

	imena := func(osobe []models.User) string {
		var out []string
		for _, u := range osobe {
			out = append(out, u.FullName)
		}
		return strings.Join(out, ", ")
	}
	pripremno := &models.Akt{Sektor: "P", AreaID: 1, Stupanj: models.PhasePrep}
	if got := imena(o.akti.MoguPotpisati(pripremno)); got != "Ana Anić, Ivo Ivić, Pero Perić" {
		t.Errorf("pripremno stanje: %s", got)
	}
	izvanredna := &models.Akt{Sektor: "P", AreaID: 1, Stupanj: models.PhaseEmergency}
	if got := imena(o.akti.MoguPotpisati(izvanredna)); got != "Ivo Ivić" {
		t.Errorf("izvanredna obrana: %s", got)
	}
	drugiSektor := &models.Akt{Sektor: "Q", AreaID: 7, Stupanj: models.PhasePrep}
	if got := o.akti.MoguPotpisati(drugiSektor); len(got) != 0 {
		t.Errorf("sektor bez osoba: %s", imena(got))
	}

	pokvariTablicu(t, o, "users")
	if got := o.akti.MoguPotpisati(pripremno); got != nil {
		t.Errorf("osobe se ne daju pročitati, a nude se: %s", imena(got))
	}
}

// nacrtAkta upisuje nacrt dežurnog operatera, s nepromjenjivim vremenima
func nacrtAkta(t *testing.T, o *okolinaAkta) *models.Akt {
	t.Helper()
	kad := time.Date(2026, 10, 5, 8, 0, 0, 0, models.Zagreb)
	a := &models.Akt{Sektor: "P", AreaID: 1, Radnja: models.AktUspostava, Stupanj: models.PhasePrep,
		StationID: o.letva.ID.String(), StationName: o.letva.Name, Vrijedi: kad,
		Dionice: []models.AktDionica{{Code: "P.1.1"}}, Status: models.AktNacrt,
		IzradioID: o.dezurni.ID.String(), Izradio: o.dezurni.FullName, IzradenoAt: kad}
	if err := repoAkata(o).SaveAkt(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a
}

// Nacrt briše onaj tko ga je sastavio ili tko bi ga smio ovjeriti; drugi
// dobiju ErrUnauthorized i nacrt ostaje. Ovjeren akt se ne briše ni
// ovjeritelju. Brisanje nepostojećeg akta ne javlja grešku, a greška
// čitanja se vraća.
func TestBrisanjeNacrtaAkta(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	postoji := func(id string) bool {
		t.Helper()
		a, err := o.akti.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return a != nil
	}

	ovjeren := ovjerenAkt(t, o, o.rukovod.ID.String())
	if err := o.akti.Obrisi(ctx, o.ovlasti, o.rukovod, ovjeren.ID); err == nil || err.Error() != "ovjeren akt se ne briše" {
		t.Errorf("ovjeren akt: %v", err)
	}
	if !postoji(ovjeren.ID) {
		t.Fatal("ovjeren akt je obrisan")
	}

	nacrt := nacrtAkta(t, o)
	for _, x := range []struct {
		opis  string
		perms *models.UserPermissions
		u     *models.User
	}{
		{"bez osobe", o.ovlasti, nil},
		{"vodočuvar dionice", o.vodOvl, o.vodocuv},
		{"bez ovlasti", nil, &models.User{ID: uuid.New(), FullName: "Ivo Ivić"}},
	} {
		if err := o.akti.Obrisi(ctx, x.perms, x.u, nacrt.ID); !errors.Is(err, service.ErrUnauthorized) {
			t.Errorf("%s: greška %v, želi ErrUnauthorized", x.opis, err)
		}
		if !postoji(nacrt.ID) {
			t.Fatalf("%s: odbijeno brisanje obrisalo je nacrt", x.opis)
		}
	}
	if err := o.akti.Obrisi(ctx, o.dezOvl, o.dezurni, nacrt.ID); err != nil || postoji(nacrt.ID) {
		t.Fatalf("autor briše svoj nacrt: %v", err)
	}
	drugi := nacrtAkta(t, o)
	if err := o.akti.Obrisi(ctx, o.ovlasti, o.rukovod, drugi.ID); err != nil || postoji(drugi.ID) {
		t.Fatalf("ovjeritelj briše nacrt: %v", err)
	}
	if err := o.akti.Obrisi(ctx, o.ovlasti, o.rukovod, drugi.ID); err != nil {
		t.Errorf("brisanje nepostojećeg nacrta: %v", err)
	}
	pokvariTablicu(t, o, "akti")
	if err := o.akti.Obrisi(ctx, o.ovlasti, o.rukovod, drugi.ID); err == nil {
		t.Error("greška čitanja akta nije vraćena")
	}
}

// Popis akata pokazuje osobi akte sektora i područja u kojima radi ili
// kojima upravlja, i akte na čijoj dionici ima dužnost; akte drugih
// područja i dionica ne pokazuje.
func TestPopisAkataPoDosegu(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	a := nacrtAkta(t, o)
	for _, x := range []struct {
		opis  string
		perms *models.UserPermissions
		vidi  bool
	}{
		{"uprava organizacije", &models.UserPermissions{IsGlobalAdmin: true}, true},
		{"radi u sektoru", &models.UserPermissions{SektoriDuznosti: map[string]bool{"P": true}}, true},
		{"radi u području", &models.UserPermissions{PodrucjaDuznosti: map[int]bool{1: true}}, true},
		{"uprava sektora", upravaSektoraP, true},
		{"uprava područja", upravaPodrucja1, true},
		{"dužnost na dionici akta", &models.UserPermissions{AllowedSections: map[string]bool{"P.1.1": true}}, true},
		{"dužnost na drugoj dionici", &models.UserPermissions{AllowedSections: map[string]bool{"P.1.2": true}}, false},
		{"drugo područje", &models.UserPermissions{AllowedAreas: map[int]bool{2: true}, AdminAreas: map[int]bool{2: true}}, false},
		{"drugi sektor", &models.UserPermissions{AllowedSectors: map[string]bool{"Q": true}, AdminSectors: map[string]bool{"Q": true}}, false},
	} {
		akti, err := o.akti.List(ctx, x.perms, repository.FiltarAkata{})
		if err != nil {
			t.Fatal(err)
		}
		vidi := len(akti) == 1 && akti[0].ID == a.ID
		if vidi != x.vidi || (!x.vidi && len(akti) != 0) {
			t.Errorf("%s: popis %d akata, želi vidi=%v", x.opis, len(akti), x.vidi)
		}
	}
}
