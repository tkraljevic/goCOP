package web

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Kartice Sudionici, Vidljivost modula i Nazivi nose broj, a brojke na
// kartici Djelatnici vode na filtrirani popis (Odluke (18. 11. 2026.),
// Administracija, novo ponašanje 4)
func TestBrojeviNaKarticamaUlaza(t *testing.T) {
	o := novaOkolinaAdministracije(t)
	ctx := context.Background()

	html := o.prikazi(t, administrator, nil)
	for _, treba := range []string{
		`id="cip-sudionici">` + itoa(len(models.RoleCatalog)) + ` uloga · bez vlastitih naziva<`,
		`id="cip-nazivi">nije spremljeno<`,
	} {
		if !strings.Contains(html, treba) {
			t.Errorf("prije spremanja nema %q", treba)
		}
	}
	if strings.Contains(html, `id="cip-vidljivost"`) {
		t.Error("vidljivost bez izvora ne smije imati broj")
	}

	t0, err := o.org.GetTerms(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t0.OrgName = "Hrvatske vode"
	t0.RoleLabels = map[string]string{string(models.RoleNationalLeader): "Glavna rukovoditeljica", string(models.RoleGuest): " "}
	if err := o.org.SaveTerms(ctx, t0); err != nil {
		t.Fatal(err)
	}
	o.h.SetVidljivost(func(context.Context) (int, int, error) { return 2, 1, nil })
	html = o.prikazi(t, administrator, nil)
	for _, treba := range []string{
		`id="cip-sudionici">` + itoa(len(models.RoleCatalog)) + ` uloga · 1 vlastiti naziv<`,
		`id="cip-vidljivost">2 prilagođene uloge · 1 iznimka računa<`,
		`id="cip-nazivi">Hrvatske vode · spremljeno `,
	} {
		if !strings.Contains(html, treba) {
			t.Errorf("nema %q", treba)
		}
	}

	o.h.SetVidljivost(func(context.Context) (int, int, error) { return 0, 0, errors.New("baza zauzeta") })
	if strings.Contains(o.prikazi(t, administrator, nil), `id="cip-vidljivost"`) {
		t.Error("nepročitana vidljivost ne smije imati broj")
	}
	o.h.SetVidljivost(func(context.Context) (int, int, error) { return 0, 0, nil })
	if !strings.Contains(o.prikazi(t, administrator, nil), `id="cip-vidljivost">sve uloge zadano · bez iznimki<`) {
		t.Error("vidljivost bez promjena")
	}
}

func TestBrojkeDjelatnikaVodeNaPopis(t *testing.T) {
	o := novaOkolinaAdministracije(t)
	for _, ime := range []string{"aanic", "iivic"} {
		u := &models.User{Username: ime, FullName: ime, IsActive: ime == "aanic", MustChangePassword: true, OrgType: models.OrgHrvatskeVode}
		if err := o.korisnci.CreateUser(u, nil); err != nil {
			t.Fatal(err)
		}
	}
	html := o.prikazi(t, administrator, nil)
	for _, treba := range []string{`<a href="/users?status=CEKA_LOZINKU">`, `<a href="/users?status=DISABLED">1 je isključen</a>`} {
		if !strings.Contains(html, treba) {
			t.Errorf("nema %q", treba)
		}
	}
}

// Brojač za karticu Vidljivost modula: uloge drukčije od zadanog i iznimke
func TestStanjeVidljivosti(t *testing.T) {
	if _, _, err := (&Server{}).stanjeVidljivosti(context.Background()); err == nil {
		t.Error("bez servisa modula bez greške")
	}
	o := novaOkolinaPrijave(t)
	ctx := context.Background()
	moduli := service.NewModuleService(repository.NewModuleRepository(o.baza, ledger.New(o.baza, "ured")))
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	if err := moduli.SetRoleRule(ctx, admin, models.RoleNationalLeader, []string{models.ModuleField}); err != nil {
		t.Fatal(err)
	}
	if err := moduli.SetUserOverride(ctx, admin, "racun-1", []string{models.ModuleAdmin}, nil); err != nil {
		t.Fatal(err)
	}
	p, i, err := (&Server{moduleService: moduli}).stanjeVidljivosti(ctx)
	if err != nil || p != 1 || i != 1 {
		t.Errorf("prilagođenih %d, iznimki %d, %v", p, i, err)
	}
	o.baza.Close()
	if _, _, err := (&Server{moduleService: moduli}).stanjeVidljivosti(ctx); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}
