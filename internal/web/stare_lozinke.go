package web

// Računi sa starom zajedničkom početnom lozinkom.
//
// Računi iz izdanja do 0.0.46-alfa koji se nikad nisu prijavili još se
// otvaraju zajedničkom lozinkom iz starih uputa. Takav se račun ne
// prijavljuje ni s jedne adrese dok mu administrator ne poništi lozinku
// (Odluke (30. 10. 2026.); service.ErrStaraLozinka), ta lozinka ne
// potvrđuje radnje uz ponovni upis lozinke (service.CheckPassword), a
// otvorena prijava takvog računa prestaje pri prvom zahtjevu
// (service.AuthService.SetZakljucanaLozinka). Globalni administrator te
// račune vidi na stranici Djelatnici.
//
// Provjera jednog sažetka traje desetinku sekunde, pa se ishod pamti po
// sažetku. Uključeni računi pregledaju se jednom, u pozadini nakon
// pokretanja. Sažetak koji pregled nije vidio (prijava otvorena prije
// pregleda, račun stigao razmjenom ili ponovno uključen) provjeri se kad
// zatreba: pri zahtjevu prijavljene osobe i, nakon pregleda, na stranici
// Djelatnici. Čim se lozinka promijeni ili poništi, račun više nije na
// popisu.

import (
	"context"
	"log"
	"sync"

	"gocop/internal/models"
)

// stareLozinke pamti koje sažetke otvara stara zajednička početna lozinka
type stareLozinke struct {
	provjeri func(sazetak string) bool // sporo (bcrypt)

	mu         sync.Mutex
	otvara     map[string]bool // sažetak → otvara li ga stara lozinka
	prijava    map[string]bool // sažeci koje je stara lozinka otvorila pri prijavi nakon pregleda
	pregledano bool            // pregled pri pokretanju je gotov
}

// zabiljezi pamti da stara lozinka otvara račun u (pri odbijenoj prijavi)
func (s *stareLozinke) zabiljezi(u *models.User) {
	if s == nil || u == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.prijava == nil {
		s.prijava = map[string]bool{}
	}
	s.prijava[u.PasswordHash] = true
}

// poznato javlja zapamćen ishod za sažetak
func (s *stareLozinke) poznato(sazetak string) (otvara, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.prijava[sazetak] {
		return true, true
	}
	otvara, ok = s.otvara[sazetak]
	return otvara, ok
}

// otvaraLi javlja otvara li stara lozinka sažetak: zapamćen ishod, a
// sažetak koji još nije provjeren provjeri i zapamti. Zove se pri svakom
// zahtjevu prijavljene osobe (service.AuthService.SetZakljucanaLozinka).
func (s *stareLozinke) otvaraLi(sazetak string) bool {
	if s == nil {
		return false
	}
	if otvara, ok := s.poznato(sazetak); ok {
		return otvara
	}
	otvara := s.provjeri(sazetak)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.otvara == nil {
		s.otvara = map[string]bool{}
	}
	s.otvara[sazetak] = otvara
	return otvara
}

// zaPopis javlja otvara li stara lozinka sažetak, za popis na stranici
// Djelatnici: prije kraja pregleda samo zapamćen ishod, nakon njega i
// provjerom sažetka koji pregled nije vidio
func (s *stareLozinke) zaPopis(sazetak string) bool {
	otvara, ok := s.poznato(sazetak)
	if ok || !s.gotovPregled() {
		return otvara
	}
	return s.otvaraLi(sazetak)
}

// gotovPregled javlja je li pregled pri pokretanju gotov
func (s *stareLozinke) gotovPregled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pregledano
}

// pregledaj provjerava sažetke uključenih računa koji još nisu provjereni i
// vraća uključene račune koje otvara stara lozinka. Pamte se sažeci iz
// racuni i oni zabilježeni pri prijavi, pa promijenjene lozinke ne ostaju u
// memoriji. Isključen račun se ne provjerava: ne prijavljuje se, a ako ga
// netko uključi, provjeri se kad zatreba. Prekinut pregled pamti ono što je
// stigao provjeriti.
func (s *stareLozinke) pregledaj(ctx context.Context, racuni []models.User) []models.User {
	novo := make(map[string]bool, len(racuni))
	for _, u := range racuni {
		if ctx.Err() != nil {
			break
		}
		if _, vec := novo[u.PasswordHash]; vec || !u.IsActive {
			continue
		}
		otvara, ok := s.poznato(u.PasswordHash)
		if !ok {
			otvara = s.provjeri(u.PasswordHash)
		}
		novo[u.PasswordHash] = otvara
	}
	s.mu.Lock()
	for h := range s.prijava {
		novo[h] = true // i ono zabilježeno pri prijavi dok je pregled trajao
	}
	s.otvara, s.prijava = novo, nil
	s.pregledano = s.pregledano || ctx.Err() == nil
	s.mu.Unlock()
	return s.medju(racuni)
}

// medju vraća uključene račune iz racuni koje otvara stara lozinka, po
// njihovu sadašnjem sažetku (zaPopis)
func (s *stareLozinke) medju(racuni []models.User) []models.User {
	if s == nil {
		return nil
	}
	var nadjeni []models.User
	for _, u := range racuni {
		if u.IsActive && s.zaPopis(u.PasswordHash) {
			nadjeni = append(nadjeni, u)
		}
	}
	return nadjeni
}

// PregledajStareLozinke jednom pregleda uključene račune i pamti koje još
// otvara stara zajednička početna lozinka. Takvim računima gasi otvorene
// prijave i broj ih javlja u dnevnik, bez imena. Zove se u pozadini nakon
// pokretanja.
func (s *Server) PregledajStareLozinke(ctx context.Context) {
	racuni, err := s.userService.ListUsers("", 0, "", "", "")
	if err != nil {
		log.Printf("Prijava: računi sa starom zajedničkom lozinkom nisu pregledani: %v", err)
		return
	}
	nadjeni := s.stareLozinke.pregledaj(ctx, racuni)
	for _, u := range nadjeni {
		if err := s.authService.EndAllSessions(u.ID); err != nil {
			log.Printf("Prijava: otvorene prijave računa sa starom zajedničkom lozinkom nisu ugašene: %v", err)
		}
	}
	if len(nadjeni) > 0 {
		log.Printf("Prijava: %d računa još ima staru zajedničku početnu lozinku i ne prijavljuje se dok mu administrator ne poništi lozinku (popis je na stranici Djelatnici)", len(nadjeni))
	}
}

// racuniSaStaromLozinkom su uključeni računi koje stara zajednička lozinka
// još otvara, za globalnog administratora na stranici Djelatnici
func (s *Server) racuniSaStaromLozinkom() ([]models.User, error) {
	racuni, err := s.userService.ListUsers("", 0, "", "", "")
	if err != nil {
		return nil, err
	}
	return s.stareLozinke.medju(racuni), nil
}

// StaraLozinkaZakljucava javlja zaključava li sažetak račun jer ga otvara
// stara zajednička početna lozinka; za service.AuthService.SetZakljucanaLozinka
func (s *Server) StaraLozinkaZakljucava(sazetak string) bool {
	return s.stareLozinke.otvaraLi(sazetak)
}
