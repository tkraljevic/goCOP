package service

import (
	"context"
	"encoding/json"
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

// SetPosta daje servisu zadane postavke poslužitelja e-pošte iz gocop.toml;
// postavke spremljene u programu (Administracija) imaju prednost
func (s *AktService) SetPosta(p posta.Postavke) { s.posta = p }

// Posta vraća važeće postavke poslužitelja: iz baze ako su spremljene, inače zadane
func (s *AktService) Posta(ctx context.Context) posta.Postavke {
	v, err := s.repo.GetPostavka(ctx, repository.PostavkaPosta)
	if err != nil || v == "" {
		return s.posta
	}
	var p posta.Postavke
	if json.Unmarshal([]byte(v), &p) != nil {
		return s.posta
	}
	p.TLS, p.DopustiBasic, p.Istek = s.posta.TLS, s.posta.DopustiBasic, s.posta.Istek
	return p
}

// PostaSpremljena javlja jesu li postavke spremljene u programu (a ne samo u datoteci)
func (s *AktService) PostaSpremljena(ctx context.Context) bool {
	v, _ := s.repo.GetPostavka(ctx, repository.PostavkaPosta)
	return v != ""
}

// SpremiPostu sprema postavke poslužitelja; smije samo administrator
func (s *AktService) SpremiPostu(ctx context.Context, perms *models.UserPermissions, p posta.Postavke) error {
	if perms == nil || !perms.IsGlobalAdmin {
		return ErrUnauthorized
	}
	p.Posluzitelj = strings.TrimSpace(p.Posluzitelj)
	p.Domena = strings.TrimSpace(p.Domena)
	if p.Nacin != posta.NacinSMTP {
		p.Nacin = posta.NacinEWS
	}
	if p.Sigurnost != posta.TLS {
		p.Sigurnost = posta.STARTTLS
	}
	if p.Port < 0 || p.Port > 65535 {
		return fmt.Errorf("port mora biti između 1 i 65535")
	}
	b, err := json.Marshal(struct {
		Nacin, Posluzitelj, Sigurnost, Domena string
		Port                                  int
	}{p.Nacin, p.Posluzitelj, p.Sigurnost, p.Domena, p.Port})
	if err != nil {
		return err
	}
	return s.repo.SavePostavka(ctx, repository.PostavkaPosta, string(b))
}

// PostaPodesena javlja je li slanje e-poštom uključeno
func (s *AktService) PostaPodesena(ctx context.Context) bool { return s.Posta(ctx).Podesena() }

// PostaSpremaPoslano javlja ostaje li poslana poruka u korisnikovoj mapi Poslano (EWS)
func (s *AktService) PostaSpremaPoslano(ctx context.Context) bool {
	return s.Posta(ctx).SpremaPoslano()
}

// PostaDomena je domena sustava Windows za prijavu
func (s *AktService) PostaDomena(ctx context.Context) string { return s.Posta(ctx).Domena }

// PostaPosluzitelj je naziv poslužitelja, za prikaz
func (s *AktService) PostaPosluzitelj(ctx context.Context) string { return s.Posta(ctx).Posluzitelj }

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
	pp := s.Posta(ctx)
	if pp.Podesena() {
		// pokušaj upisano ime, pa DOMENA\korisnik; spremi ono koje prođe
		var err error
		var pokusano []string
		for _, ime := range pp.Imena(korisnik) {
			var proslo string
			proslo, err = posta.Prijavi(ctx, pp, posta.Racun{Korisnik: ime, Lozinka: lozinka})
			pokusano = append(pokusano, ime)
			if err == nil {
				korisnik = proslo
				break
			}
			if !errors.Is(err, posta.ErrPrijava) {
				break
			}
		}
		if errors.Is(err, posta.ErrPrijava) {
			return "", fmt.Errorf("poslužitelj %s je odbio korisničko ime ili lozinku (pokušano: %s, i s domenom poslužitelja); ništa nije spremljeno. Provjerite lozinku prijavom na https://%s u pregledniku; više krivih pokušaja zaključava račun", pp.Posluzitelj, strings.Join(pokusano, ", "), pp.Posluzitelj)
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
	pp := s.Posta(ctx)
	if !pp.Podesena() {
		return nil, fmt.Errorf("slanje e-poštom nije uključeno: administrator upisuje poslužitelj u Administraciji › E-pošta")
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
	greske, err := posta.Posalji(ctx, pp, posta.Racun{Korisnik: racun.Korisnik, Lozinka: lozinka}, poruke)
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
