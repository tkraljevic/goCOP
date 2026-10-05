package service

import (
	"context"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// UskladiStupileNaSnagu izvodi povijest obrane dionica akata koji su stupili
// na snagu u razdoblju (od, do]: akt s kasnijim početkom (Vrijedi) tako ulazi
// u epizode kad stupi na snagu, bez nove ovjere ili storna u sektoru.
// Izvedene epizode imaju stalne identitete, a nepromijenjene se ne upisuju
// (UskladiIzAkata), pa čvorovi koji isto izvedu ne dodaju nove verzije.
// Poništen akt se preskače: njegovo je poništenje povijest već uskladilo.
// Upozorenja akata razdoblja i upozorenja privremenih imenovanja vraćaju se
// odvojeno: imenovanja se preračunaju jednom, ako je koji akt izveden, a nisu
// vezana uz akte razdoblja, pa krug po njima ne zadržava granicu.
func (s *AktService) UskladiStupileNaSnagu(ctx context.Context, od, do time.Time) (akti, privremene []string) {
	if s.episodes == nil {
		return nil, nil
	}
	ovjereni, err := s.repo.ListAkti(ctx, repository.FiltarAkata{Status: models.AktOvjeren})
	if err != nil {
		return []string{"povijest obrane nije usklađena: " + err.Error()}, nil
	}
	izvedeno := false
	for i := range ovjereni {
		a := &ovjereni[i]
		if !a.Storniran() && a.Vrijedi.After(od) && !a.Vrijedi.After(do) {
			akti = append(akti, s.uskladiPovijest(ctx, a)...)
			izvedeno = true
		}
	}
	if izvedeno {
		privremene = s.uskladiPrivremene()
	}
	return akti, privremene
}
