package service

import (
	"context"

	"gocop/internal/models"
)

// KolacicRacunala je ime kolačića zapamćenog računala: jedan nasumični
// token po pregledniku, za sve osobe koje se s njega prijavljuju
const KolacicRacunala = "__Host-gocop_racunalo"

// AdresaZaPIN vraća maskiranu adresu na koju bi osobi išao PIN, za prikaz
// na profilu; kad ga ne bi bilo kamo poslati, vraća razlog (ErrNemaAdrese,
// ErrAdresaNijeDopustena, ErrZajednickaAdresa). Ništa ne šalje.
func (d *DrugiKorak) AdresaZaPIN(ctx context.Context, u *models.User) (string, error) {
	if d == nil || d.repo == nil {
		return "", ErrNemaKljucaPrijave
	}
	if u == nil {
		return "", ErrUserNotFound
	}
	a, err := d.adresaZaPIN(ctx, u)
	if err != nil {
		return "", err
	}
	return MaskirajAdresu(a), nil
}
