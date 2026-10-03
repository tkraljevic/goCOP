package service

import (
	"context"
	"errors"
	"time"

	"gocop/internal/models"
)

// KolacicRacunala je ime kolačića zapamćenog računala: jedan nasumični
// token po pregledniku, za sve osobe koje se s njega prijavljuju
const KolacicRacunala = "__Host-gocop_racunalo"

// Vrste stanja adrese za PIN (StanjeAdresePIN.Vrsta)
const (
	AdresaUDomeni     = "domena"      // u dopuštenoj domeni: PIN ide na nju
	AdresaPotvrdena   = "potvrdena"   // izvan domene, potvrdio ju je administrator: PIN ide na nju
	AdresaNepotvrdena = "nepotvrdena" // izvan domene, nije potvrđena: PIN se ne šalje
	AdresaZajednicka  = "zajednicka"  // istu adresu ima još jedan aktivni račun: PIN se ne šalje
	AdresaNema        = "nema"        // nema adrese
	AdresaNeispravna  = "neispravna"  // adresa nije ispravna
)

// StanjeAdresePIN kaže ide li PIN osobi na njezinu adresu i zašto, za
// prikaz na profilu i kod djelatnika.
type StanjeAdresePIN struct {
	Vrsta  string // AdresaUDomeni, AdresaPotvrdena…
	Domena string // dopuštena domena, bez @
	Adresa string // maskirana adresa na koju ide PIN; prazno kad ne ide nikamo
	// Potvrdio i Potvrdeno: tko je i kada potvrdio adresu izvan domene, dok
	// potvrda vrijedi (i uz zajedničku adresu)
	Potvrdio  string
	Potvrdeno *time.Time
	Razlog    error // zašto PIN ne ide nikamo; nil kad ide
}

// IdePIN javlja ide li PIN na adresu
func (s StanjeAdresePIN) IdePIN() bool { return s.Razlog == nil }

// PotvrdenoTekst je dan potvrde za prikaz (npr. 3.10.2026.)
func (s StanjeAdresePIN) PotvrdenoTekst() string {
	if s.Potvrdeno == nil {
		return ""
	}
	return s.Potvrdeno.In(models.Zagreb).Format("2.1.2006.")
}

// StanjeAdrese slaže stanje adrese osobe za PIN istim pravilima kao slanje
// (adresaZaPIN), s maskiranom adresom; kad PIN ne bi imao kamo, Razlog je
// ErrNemaAdrese, ErrNeispravnaAdresa, ErrAdresaNijeDopustena ili
// ErrZajednickaAdresa. Ništa ne šalje; greška je samo za kvar (baza).
func (d *DrugiKorak) StanjeAdrese(ctx context.Context, u *models.User) (*StanjeAdresePIN, error) {
	if d == nil || d.repo == nil {
		return nil, ErrNemaKljucaPrijave
	}
	if u == nil {
		return nil, ErrUserNotFound
	}
	s := &StanjeAdresePIN{Domena: d.Opcije(ctx).Domena}
	if u.PotvrdaAdreseVrijedi() {
		s.Potvrdio, s.Potvrdeno = u.PINAdresuPotvrdio, u.PINAdresaPotvrdenaKad
	}
	a, err := d.adresaZaPIN(ctx, u)
	switch {
	case err == nil:
		s.Adresa = MaskirajAdresu(a)
		s.Vrsta = AdresaUDomeni
		if d.DopustenaAdresa(ctx, a) != nil {
			s.Vrsta = AdresaPotvrdena
		}
		return s, nil
	case errors.Is(err, ErrNemaAdrese):
		s.Vrsta = AdresaNema
	case errors.Is(err, ErrAdresaNijeDopustena):
		s.Vrsta = AdresaNepotvrdena
	case errors.Is(err, ErrZajednickaAdresa):
		s.Vrsta = AdresaZajednicka
	case errors.Is(err, ErrNeispravnaAdresa):
		s.Vrsta = AdresaNeispravna
	default:
		return nil, err // baza
	}
	s.Razlog = err
	return s, nil
}
