package service

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// NapomenaPodatakaNajvise je najveća duljina napomene o podacima, u znakovima.
const NapomenaPodatakaNajvise = 500

// NapomenaPodataka vraća tekst koji administrator pokazuje na vrhu stranice
// Prognoze, npr. kad izvor privremeno mijenja adrese; prazno kad ga nema.
func (s *AktService) NapomenaPodataka(ctx context.Context) string {
	v, _ := s.repo.GetPostavka(ctx, repository.PostavkaNapomenaPodataka) // bez zapisa ili baze napomene nema
	return v
}

// SpremiNapomenuPodataka sprema napomenu; smije samo globalni administrator.
// Prazan tekst briše napomenu. Razmjenom stiže i na ostala računala. Prikazuje
// se kao jedan odlomak, pa se novi redovi i višestruki razmaci sažimaju.
func (s *AktService) SpremiNapomenuPodataka(ctx context.Context, perms *models.UserPermissions, tekst string) error {
	if perms == nil || !perms.IsGlobalAdmin {
		return ErrUnauthorized
	}
	tekst = strings.Join(strings.Fields(tekst), " ")
	if n := utf8.RuneCountInString(tekst); n > NapomenaPodatakaNajvise {
		return fmt.Errorf("Napomena nije spremljena: ima %d znakova, a smije najviše %d. Skratite je pa spremite ponovno.", n, NapomenaPodatakaNajvise)
	}
	return s.repo.SavePostavka(ctx, repository.PostavkaNapomenaPodataka, tekst)
}
