package peers

// Potvrda promjene prava ključem mreže (Odluke (30. 10. 2026.), dopuna 16):
// promjenu prava za cijelu mrežu potvrđuje ili odbija globalni administrator
// jednom, na računalu koje drži ključ mreže, a odluka potpisana tim ključem
// putuje razmjenom svima. Što potvrda znači i kad vrijedi, odlučuje
// repozitorij (repository/apply_potvrda_prava.go); ovdje je samo potpis.

import (
	"context"

	"gocop/internal/razmjena"
)

// PotpisiPotvrdu potpisuje odluku o promjeni prava ključem mreže, s ovim
// računalom kao mjestom odluke; false kad ovo računalo ključ mreže ne drži
func (s *Service) PotpisiPotvrdu(p razmjena.PotvrdaPrava) (razmjena.PotvrdaPrava, bool) {
	s.mu.Lock()
	n := s.network
	s.mu.Unlock()
	if n == nil {
		return p, false
	}
	p.Racunalo = s.node.ID
	potpisana, err := n.PotpisiPotvrdu(p)
	return potpisana, err == nil
}

// DrziKljucMreze javlja drži li ovo računalo ključ mreže
func (s *Service) DrziKljucMreze() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.network != nil && s.network.CanSign()
}

// NakonKljucaMreze zadaje što se radi kad ovo računalo dobije ključ mreže:
// osnuje mrežu ili učita kopiju ključa. Program tada potpiše potvrde stanja
// prava koje ovo računalo drži (repository.PotpisiStanjePrava).
func (s *Service) NakonKljucaMreze(f func(context.Context)) { s.nakonKljuca.Store(&f) }

// nakonUpisaKljuca javlja da ovo računalo drži ključ mreže, kad je upis
// prošao (err je greška upisa)
func (s *Service) nakonUpisaKljuca(ctx context.Context, err error) error {
	if err == nil {
		s.javiKljucMreze(ctx)
	}
	return err
}

// javiKljucMreze zove ono što je zadano s NakonKljucaMreze
func (s *Service) javiKljucMreze(ctx context.Context) {
	if f := s.nakonKljuca.Load(); f != nil {
		(*f)(ctx)
	}
}
