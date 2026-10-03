package service

// Postavljanje svježeg čvora: prvi administrator nove mreže.
//
// Na svježem čvoru (nijedan djelatnik osim početnog računa admin) vlasnik na
// stranici Postavljanje (web/handlers_postavljanje.go) napravi svoj račun
// globalnog administratora i lozinku, pa početna lozinka iz uputa ne treba.
// Početni račun admin se pritom isključuje: njegova je lozinka javna.

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"gocop/internal/db"
	"gocop/internal/models"
)

// ErrNijeSvjez: čvor već ima djelatnike, postavljanje je samo za svjež čvor
var ErrNijeSvjez = errors.New("čvor već ima djelatnike; postavljanje je samo za svjež čvor")

var oblikKorisnickogImena = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,39}$`)

// PrviAdministratorZahtjev su podaci s obrasca Postavljanje
type PrviAdministratorZahtjev struct {
	Korisnik string
	Ime      string
	Email    string
	Lozinka  string
}

// PrviAdministrator stvara prvog globalnog administratora svježeg čvora i
// isključuje početni račun admin
func (s *UserService) PrviAdministrator(z PrviAdministratorZahtjev) (*models.User, error) {
	z.Korisnik = strings.ToLower(strings.TrimSpace(z.Korisnik))
	z.Ime = strings.TrimSpace(z.Ime)
	z.Email = strings.TrimSpace(z.Email)

	svi, err := s.userRepo.ListUsers("", 0, "", "", "")
	if err != nil {
		return nil, err
	}
	var pocetni *models.User
	for i := range svi {
		if svi[i].Username == "admin" && svi[i].MustChangePassword {
			pocetni = &svi[i]
		}
	}
	if len(svi) > 1 || (len(svi) == 1 && pocetni == nil) {
		return nil, ErrNijeSvjez
	}

	switch {
	case !oblikKorisnickogImena.MatchString(z.Korisnik):
		return nil, fmt.Errorf("%w: korisničko ime ima 2–40 znakova: mala slova, brojke, točku, crticu ili podvlaku (npr. tkraljevic)", ErrInvalidUserData)
	case z.Korisnik == "admin":
		return nil, fmt.Errorf("%w: izaberite osobno korisničko ime, ne admin", ErrInvalidUserData)
	case z.Ime == "":
		return nil, fmt.Errorf("%w: ime i prezime su obavezni", ErrInvalidUserData)
	case z.Lozinka == db.ZadanaLozinka:
		return nil, fmt.Errorf("%w: početna lozinka iz uputa je javna; izaberite svoju", ErrInvalidUserData)
	}
	if err := ProvjeriNovuLozinku("", z.Lozinka); err != nil {
		return nil, err
	}
	if len(z.Lozinka) < 10 {
		// osnivač mreže drži sve: dulja lozinka nego za ostale račune
		return nil, fmt.Errorf("%w: lozinka prvog administratora ima najmanje 10 znakova", ErrInvalidUserData)
	}
	hash, err := s.auth.HashPassword(z.Lozinka)
	if err != nil {
		return nil, err
	}
	u := &models.User{
		Username: z.Korisnik, PasswordHash: hash, FullName: z.Ime, Email: z.Email,
		IsGlobalAdmin: true, IsActive: true, OrgType: models.OrgHrvatskeVode,
	}
	duty := &models.Duty{Title: "Glavni administrator", Role: models.RoleGlobalAdmin,
		ScopeType: models.ScopeAll, IsPrimary: true, IsActive: true}
	if err := s.userRepo.CreateUser(u, duty); err != nil {
		return nil, err
	}
	if pocetni != nil {
		pocetni.IsActive = false
		if err := s.userRepo.UpdateUser(pocetni); err != nil {
			return u, fmt.Errorf("administrator je napravljen, ali početni račun admin nije isključen: %w", err)
		}
	}
	return u, nil
}
