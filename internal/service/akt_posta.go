package service

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"slices"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/repository"
)

// Slanje ovjerenog akta primateljima "na znanje" preko poslužitelja e-pošte
// tvrtke (Exchange), s adrese i računa prijavljenog korisnika. Šalje se tek
// kad akt ima izvornik: PDF potpisan u SIGNATOR-u ili sken s potpisom i žigom.

// SmijeSlati javlja smije li korisnik slati akt: tko smije pripremati akt
func (s *AktService) SmijeSlati(perms *models.UserPermissions, u *models.User, a *models.Akt) bool {
	return s.smijePripremiti(perms, u, a)
}

// SetPosta daje servisu postavke poslužitelja e-pošte
func (s *AktService) SetPosta(p posta.Postavke) { s.posta = p }

// PostaPodesena javlja je li slanje e-poštom uključeno na ovom čvoru
func (s *AktService) PostaPodesena() bool { return s.posta.Podesena() }

// PostaSpremaPoslano javlja ostaje li poslana poruka u korisnikovoj mapi Poslano (EWS)
func (s *AktService) PostaSpremaPoslano() bool { return s.posta.SpremaPoslano() }

// PostaDomena je domena sustava Windows za prijavu
func (s *AktService) PostaDomena() string { return s.posta.Domena }

// PostaPosluzitelj je naziv poslužitelja, za prikaz
func (s *AktService) PostaPosluzitelj() string { return s.posta.Posluzitelj }

func (s *AktService) kljucPoste() []byte {
	if len(s.kljuc) == 0 {
		return nil
	}
	return posta.Kljuc(s.kljuc.Seed())
}

// RacunPoste vraća korisničko ime i vrijeme zadnje promjene lozinke; prazno kad lozinka nije upisana
func (s *AktService) RacunPoste(ctx context.Context, userID string) (string, time.Time) {
	r, err := s.repo.GetRacunPoste(ctx, userID)
	if err != nil || r == nil {
		return "", time.Time{}
	}
	return r.Korisnik, r.UpdatedAt
}

// SpremiRacunPoste provjeri prijavu na poslužitelju i spremi lozinku
// šifrirano. Kad poslužitelj odbije lozinku, ne sprema se ništa; kad nije
// dostupan (npr. izvan mreže tvrtke), sprema se uz upozorenje.
func (s *AktService) SpremiRacunPoste(ctx context.Context, u *models.User, korisnik, lozinka string) (string, error) {
	korisnik = strings.TrimSpace(korisnik)
	if korisnik == "" {
		korisnik = u.Email
	}
	if korisnik == "" || lozinka == "" {
		return "", fmt.Errorf("upišite korisničko ime i lozinku")
	}
	k := s.kljucPoste()
	if k == nil {
		return "", fmt.Errorf("ključ čvora nije učitan; lozinka se ne može sigurno spremiti")
	}
	upozorenje := ""
	if s.posta.Podesena() {
		// pokušaj upisano ime, pa DOMENA\korisnik; spremi ono koje prođe
		var err error
		for _, ime := range s.posta.Imena(korisnik) {
			err = posta.Provjeri(ctx, s.posta, posta.Racun{Korisnik: ime, Lozinka: lozinka})
			if err == nil {
				korisnik = ime
				break
			}
			if !errors.Is(err, posta.ErrPrijava) {
				break
			}
		}
		if errors.Is(err, posta.ErrPrijava) {
			return "", fmt.Errorf("poslužitelj %s je odbio korisničko ime ili lozinku (pokušano: %s); ništa nije spremljeno", s.posta.Posluzitelj, strings.Join(s.posta.Imena(korisnik), ", "))
		}
		if err != nil {
			upozorenje = "Lozinka je spremljena, ali prijava nije provjerena: " + err.Error()
		}
	}
	z, err := posta.Zakljucaj(k, lozinka)
	if err != nil {
		return "", err
	}
	if err := s.repo.SaveRacunPoste(ctx, &repository.RacunPoste{UserID: u.ID.String(), Korisnik: korisnik, Lozinka: z}); err != nil {
		return "", err
	}
	return upozorenje, nil
}

// ObrisiRacunPoste briše spremljenu lozinku s ovog čvora
func (s *AktService) ObrisiRacunPoste(ctx context.Context, u *models.User) error {
	return s.repo.DeleteRacunPoste(ctx, u.ID.String())
}

// Adresat je jedna adresa primatelja "na znanje"
type Adresat struct {
	Naziv, Skupina, Adresa string
	Zadnje                 *models.SlanjeAkta // zadnji pokušaj slanja na ovu adresu
}

// Poslano javlja je li akt na ovu adresu već stigao do poslužitelja
func (a Adresat) Poslano() bool { return a.Zadnje != nil && a.Zadnje.Uspjelo }

// AdresatiAkta vraća adrese primatelja "na znanje" sa stanjem slanja, i
// primatelje bez adrese
func (s *AktService) AdresatiAkta(ctx context.Context, a *models.Akt) ([]Adresat, []models.AktPrimatelj, []models.SlanjeAkta) {
	slanja, _ := s.repo.ListSlanja(ctx, a.ID)
	var out []Adresat
	var bez []models.AktPrimatelj
	vidjeno := map[string]bool{}
	for _, p := range a.Primatelji {
		adrese := posta.Adrese(p.Email)
		if len(adrese) == 0 {
			bez = append(bez, p)
			continue
		}
		for _, adr := range adrese {
			if vidjeno[adr] {
				continue
			}
			vidjeno[adr] = true
			x := Adresat{Naziv: p.Naziv, Skupina: p.Skupina, Adresa: adr}
			for i := range slanja {
				if slanja[i].Adresa == adr {
					x.Zadnje = &slanja[i]
				}
			}
			out = append(out, x)
		}
	}
	return out, bez, slanja
}

// PorukaAkta je predmet, tekst i privitak poruke kojom se akt šalje
type PorukaAkta struct {
	Predmet, Tekst, ImeDatoteke string
}

// IshodSlanja kaže koliko je adresa primilo akt i koje nisu
type IshodSlanja struct {
	Poslano int
	Greske  []string // "adresa: razlog"
	Kopija  bool     // kopija je stigla pošiljatelju
}

// PosaljiNaZnanje šalje izvornik akta odabranim adresama s popisa "na znanje"
func (s *AktService) PosaljiNaZnanje(ctx context.Context, perms *models.UserPermissions, u *models.User, id string, adrese []string, kopijaMeni bool, poruka PorukaAkta) (*IshodSlanja, error) {
	a, err := s.repo.GetAkt(ctx, id)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, fmt.Errorf("akt ne postoji")
	}
	if !a.Ovjeren() || !a.ImaIzvornik() {
		return nil, fmt.Errorf("akt se šalje tek kad je učitan izvornik: PDF potpisan u SIGNATOR-u ili sken s potpisom i žigom")
	}
	if !s.smijePripremiti(perms, u, a) {
		return nil, ErrUnauthorized
	}
	if !s.posta.Podesena() {
		return nil, fmt.Errorf("slanje e-poštom nije uključeno: upišite poslužitelj u gocop.toml, odjeljak [posta]")
	}
	if u.Email == "" {
		return nil, fmt.Errorf("u vašem profilu nema adrese e-pošte; s nje se akt šalje")
	}
	racun, err := s.repo.GetRacunPoste(ctx, u.ID.String())
	if err != nil {
		return nil, err
	}
	if racun == nil {
		return nil, fmt.Errorf("upišite lozinku e-pošte u profilu (Profil › E-pošta za slanje akata)")
	}
	lozinka, err := posta.Otkljucaj(s.kljucPoste(), racun.Lozinka)
	if err != nil {
		return nil, err
	}
	pdf, err := s.Izvornik(ctx, a.ID)
	if err != nil || pdf == nil {
		return nil, fmt.Errorf("izvornik akta nije pronađen na ovom čvoru")
	}

	// šalje se samo na adrese s popisa "na znanje"
	svi, _, _ := s.AdresatiAkta(ctx, a)
	var odabrani []Adresat
	for _, x := range svi {
		if slices.Contains(adrese, x.Adresa) {
			odabrani = append(odabrani, x)
		}
	}
	if len(odabrani) == 0 && !kopijaMeni {
		return nil, fmt.Errorf("odaberite barem jednu adresu")
	}
	od := mail.Address{Name: u.FullName, Address: u.Email}
	privitak := []posta.Privitak{{Ime: poruka.ImeDatoteke, Vrsta: "application/pdf", Podaci: pdf}}
	var poruke []posta.Poruka
	for _, x := range odabrani {
		poruke = append(poruke, posta.Poruka{Od: od, Za: mail.Address{Address: x.Adresa}, Predmet: poruka.Predmet, Tekst: poruka.Tekst, Privitci: privitak})
	}
	if kopijaMeni {
		poruke = append(poruke, posta.Poruka{Od: od, Za: od, Predmet: poruka.Predmet, Tekst: poruka.Tekst, Privitci: privitak})
	}
	greske, err := posta.Posalji(ctx, s.posta, posta.Racun{Korisnik: racun.Korisnik, Lozinka: lozinka}, poruke)
	if errors.Is(err, posta.ErrPrijava) {
		return nil, fmt.Errorf("poslužitelj je odbio vašu lozinku e-pošte; ako ste je promijenili, upišite novu u profilu")
	}
	if err != nil {
		return nil, err
	}
	ishod := &IshodSlanja{}
	kad := time.Now()
	var zapisi []models.SlanjeAkta
	for i, x := range odabrani {
		z := models.SlanjeAkta{AktID: a.ID, Adresa: x.Adresa, Naziv: x.Naziv, Skupina: x.Skupina, PoslaoID: u.ID.String(), Poslao: u.FullName,
			Posiljatelj: u.Email, Kad: kad, Uspjelo: greske[i] == nil, Cvor: s.cvor}
		if greske[i] != nil {
			z.Greska = greske[i].Error()
			ishod.Greske = append(ishod.Greske, x.Adresa+": "+z.Greska)
		} else {
			ishod.Poslano++
		}
		zapisi = append(zapisi, z)
	}
	if kopijaMeni {
		ishod.Kopija = greske[len(greske)-1] == nil
	}
	if err := s.repo.SaveSlanja(ctx, zapisi); err != nil {
		return ishod, fmt.Errorf("poslano, ali zapis slanja nije spremljen: %w", err)
	}
	return ishod, nil
}
