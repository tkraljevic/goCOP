package service

// Kraj obrane za privremena imenovanja: rješenje o privremenom imenovanju
// prestaje važiti prestankom redovne i izvanredne obrane (i izvanrednog
// stanja) na dionicama, odnosno u branjenom području imenovanja. Kraj se čita
// iz ovjerenih, neponištenih akata, isto kao stanje obrane na kartici dionice.

import (
	"context"
	"log"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// KrajObraneDuznosti je kraj redovne i izvanredne obrane na dosegu dužnosti
// (models.KrajRedovneObrane) od trenutka dodjele. nil dok obrana traje, kad je
// nema ili kad se doseg ili akti ne daju pročitati: imenovanje tada vrijedi
// dalje, do zadanog datuma ili opoziva.
func (s *AktService) KrajObraneDuznosti(d models.Duty) *time.Time {
	dionice, err := s.dioniceDosega(d)
	if err != nil || len(dionice) == 0 {
		return nil
	}
	f := repository.FiltarAkata{Status: models.AktOvjeren}
	if d.SectorID != nil {
		f.Sektor = *d.SectorID
	}
	akti, err := s.repo.ListAkti(context.Background(), f)
	if err != nil {
		log.Printf("kraj obrane za privremeno imenovanje %s: %v", d.ID, err)
		return nil
	}
	od := d.CreatedAt
	if od.IsZero() {
		od = time.Now()
	}
	return models.KrajRedovneObrane(akti, dionice, od)
}

// dioniceDosega su dionice na kojima dužnost vrijedi: upisane dionice, inače
// sve dionice njezina branjenog područja, odnosno sektora
func (s *AktService) dioniceDosega(d models.Duty) ([]string, error) {
	if sifre := sifreDionica(d.SectionCodes); len(sifre) > 0 {
		return sifre, nil
	}
	sektor, podrucje := "", 0
	if d.AreaID != nil {
		podrucje = *d.AreaID
	} else if d.SectorID != nil {
		sektor = *d.SectorID
	}
	dionice, err := s.sections.ListSections(sektor, podrucje, "")
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(dionice))
	for _, x := range dionice {
		out = append(out, x.Code)
	}
	return out, nil
}

// uskladiPrivremene: ovjera ili poništenje akta mijenja kraj obrane, pa i
// istek privremenih imenovanja
func (s *AktService) uskladiPrivremene() []string {
	if s.users == nil {
		return nil
	}
	if err := s.users.UskladiPrivremene(); err != nil {
		return []string{"privremena imenovanja nisu usklađena: " + err.Error()}
	}
	return nil
}
