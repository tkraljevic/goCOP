package peers

// Rupe uz uvoz (rupe.go): zapis rupa mijenja se pod bravom upisa, kao i
// upis primljenoga.

import "context"

// rupePodBravom promijeni zapis rupa pod bravom upisa, koju čeka koliko treba
func (s *Service) rupePodBravom(ctx context.Context, p promjenaRupa, korak korakRupa) error {
	if err := s.uzmiBravu(ctx); err != nil {
		return err
	}
	defer s.pustiBravu()
	return s.promijeniRupe(ctx, p, korak)
}
