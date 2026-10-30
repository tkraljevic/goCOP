package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// pravaIzDuznosti gradi prava iz jedne aktivne dužnosti u sektoru P
func pravaIzDuznosti(uloga models.Role, doseg models.ScopeType, podrucje int, dionice string) *models.UserPermissions {
	sektor := "P"
	d := models.Duty{ID: uuid.New(), Role: uloga, ScopeType: doseg, SectorID: &sektor, SectionCodes: dionice, IsActive: true}
	if podrucje > 0 {
		d.AreaID = &podrucje
	}
	return models.NewUserPermissions(models.User{ID: uuid.New(), Duties: []models.Duty{d}})
}

// Postaju za izmjenu daje samo onome tko ima pravo na jednu od njezinih
// dionica, i to s identifikatorom kako ga baza zapisuje. Greška prava nosi
// ErrNemaOvlasti; neispravan identifikator, postaja koje nema i dionica
// koje nema nisu pitanje prava.
func TestPostajaZaIzmjenuTraziPravo(t *testing.T) {
	o := novaOkolinaPostaja(t)
	ctx := context.Background()
	st, err := o.svc.GetStationByCode(ctx, "primjerovo")
	if err != nil || st == nil {
		t.Fatalf("letva: %v %v", st, err)
	}
	sDionice := pravaIzDuznosti(models.RoleOperator, models.ScopeSection, 1, "P.1.1")
	drugaDionica := pravaIzDuznosti(models.RoleOperator, models.ScopeSection, 1, "P.1.2")
	promatrac := pravaIzDuznosti(models.RoleViewer, models.ScopeSector, 0, "")

	id, err := o.svc.PostajaZaIzmjenu(ctx, sDionice, " "+strings.ToUpper(st.ID.String())+" ")
	if err != nil || id != st.ID.String() {
		t.Errorf("osoba s dionice: %q %v", id, err)
	}
	for opis, perms := range map[string]*models.UserPermissions{"promatrač": promatrac, "druga dionica": drugaDionica, "bez prijave": nil} {
		if id, err := o.svc.PostajaZaIzmjenu(ctx, perms, st.ID.String()); !errors.Is(err, service.ErrNemaOvlasti) || id != "" {
			t.Errorf("%s: %q %v", opis, id, err)
		}
	}
	for _, zapis := range []string{"nije-uuid", uuid.NewString()} {
		if _, err := o.svc.PostajaZaIzmjenu(ctx, sDionice, zapis); err == nil || errors.Is(err, service.ErrNemaOvlasti) {
			t.Errorf("%s: %v", zapis, err)
		}
	}

	if err := o.svc.ProvjeriUnos(promatrac, ""); !errors.Is(err, service.ErrNemaOvlasti) {
		t.Errorf("promatrač, unos izvan dionice: %v", err)
	}
	if err := o.svc.ProvjeriUnos(promatrac, "P.1.1"); !errors.Is(err, service.ErrNemaOvlasti) {
		t.Errorf("promatrač, unos na dionicu: %v", err)
	}
	if err := o.svc.ProvjeriUnos(globalAdmin(), "P.9.9"); !errors.Is(err, service.ErrSectionNotFound) || errors.Is(err, service.ErrNemaOvlasti) {
		t.Errorf("dionica koje nema: %v", err)
	}

	o.pokvari(t, "stations")
	if _, err := o.svc.PostajaZaIzmjenu(ctx, sDionice, st.ID.String()); err == nil {
		t.Error("postaja se ne da pročitati, a prošlo je")
	}
}

// Izmjena letve koju baza odbije vraća grešku baze.
func TestIzmjenaPostajeGreskaBaze(t *testing.T) {
	o := novaOkolinaPostaja(t)
	ctx := context.Background()
	st, err := o.svc.GetStationByCode(ctx, "primjerovo")
	if err != nil || st == nil {
		t.Fatalf("letva: %v %v", st, err)
	}
	if _, err := o.baza.Exec(`CREATE TRIGGER izmjena_odbijena BEFORE UPDATE ON stations BEGIN SELECT RAISE(ABORT, 'izmjena odbijena'); END`); err != nil {
		t.Fatal(err)
	}
	st.Notes = "nova bilješka"
	if err := o.svc.UpdateStation(ctx, globalAdmin(), st); err == nil || !strings.Contains(err.Error(), "izmjena odbijena") {
		t.Errorf("izmjena: %v", err)
	}
}

// Vodotok postaje mijenja tko smije mijenjati postaju, a upis ide na
// postaju čije je pravo provjereno. Bez provjere postaja vodotok mijenja
// samo globalni administrator, a bez prijave nitko.
func TestVodotokPostajeUServisu(t *testing.T) {
	o := novaOkolinaPostaja(t)
	ctx := context.Background()
	st, err := o.svc.GetStationByCode(ctx, "primjerovo")
	if err != nil || st == nil {
		t.Fatalf("letva: %v %v", st, err)
	}
	waterRepo := repository.NewWatercourseRepository(o.baza, ledger.New(o.baza, "test"))
	if err := waterRepo.CreateWatercourse(ctx, &models.Watercourse{Code: "rijeka-primjerica", OfficialName: "rijeka Primjerica", Name: "Primjerica"}); err != nil {
		t.Fatal(err)
	}
	sDionice := pravaIzDuznosti(models.RoleOperator, models.ScopeSection, 1, "P.1.1")
	promatrac := pravaIzDuznosti(models.RoleViewer, models.ScopeSector, 0, "")
	vodotok := func() string {
		t.Helper()
		natrag, err := o.repo.GetStationByID(ctx, st.ID)
		if err != nil || natrag == nil {
			t.Fatalf("letva se ne čita: %v", err)
		}
		return natrag.WatercourseCode
	}

	waters := service.NewWatercourseService(waterRepo)
	waters.SetPostaje(o.svc)
	for opis, perms := range map[string]*models.UserPermissions{"promatrač": promatrac, "bez prijave": nil} {
		if err := waters.SetStationWatercourse(ctx, perms, st.ID.String(), "rijeka-primjerica"); !errors.Is(err, service.ErrNemaOvlasti) {
			t.Errorf("%s: %v", opis, err)
		}
	}
	if v := vodotok(); v != "" {
		t.Fatalf("vodotok bez prava: %q", v)
	}
	if err := waters.SetStationWatercourse(ctx, sDionice, strings.ToUpper(st.ID.String()), "rijeka-primjerica"); err != nil {
		t.Fatalf("osoba s dionice: %v", err)
	}
	if v := vodotok(); v != "rijeka-primjerica" {
		t.Errorf("vodotok nije upisan: %q", v)
	}
	if err := waters.SetStationWatercourse(ctx, sDionice, st.ID.String(), "nema-je"); err == nil || errors.Is(err, service.ErrNemaOvlasti) {
		t.Errorf("voda koje nema u registru: %v", err)
	}

	samo := service.NewWatercourseService(waterRepo)
	if err := samo.SetStationWatercourse(ctx, sDionice, st.ID.String(), ""); !errors.Is(err, service.ErrNemaOvlasti) {
		t.Errorf("bez provjere postaja, osoba s dionice: %v", err)
	}
	if err := samo.SetStationWatercourse(ctx, globalAdmin(), st.ID.String(), ""); err != nil {
		t.Errorf("bez provjere postaja, administrator: %v", err)
	}
	if v := vodotok(); v != "" {
		t.Errorf("administrator nije obrisao vodotok: %q", v)
	}
}
