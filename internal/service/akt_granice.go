package service

import (
	"context"
	"fmt"
	"time"

	"gocop/internal/models"
)

// Granice „Vrijedi od” u aktu (Odluke (8. 10. 2026.), pitanje 17). Tvrdo se
// odbija nacrt koji bi vrijedio prije otvaranja Dnevnika COP-a aktivne
// obrane u sektoru ili više od 7 dana unaprijed: to su sigurno pogreške
// (tipfeler u godini). Više od 24 sata unatrag ili 3 dana unaprijed prolazi
// samo uz izričitu potvrdu, koja se bilježi uz akt: naknadno unesen akt i
// najava unaprijed ispravna su praksa. Godina oznake i dalje je godina iz
// „Vrijedi od” (odluka od 4. 10. 2026.).
const (
	vrijediNajviseUnaprijed = 7 * 24 * time.Hour
	vrijediPotvrdaUnatrag   = 24 * time.Hour
	vrijediPotvrdaUnaprijed = 3 * 24 * time.Hour
)

// TrebaPotvrduError: radnja prolazi tek uz izričitu potvrdu; Pitanje je
// kako program pita
type TrebaPotvrduError struct{ Pitanje string }

func (e *TrebaPotvrduError) Error() string { return e.Pitanje }

// vrijediTraziPotvrdu javlja je li „Vrijedi od” izvan uobičajenog raspona:
// više od 24 sata unatrag ili više od 3 dana unaprijed
func vrijediTraziPotvrdu(vrijedi, sad time.Time) bool {
	return vrijedi.Before(sad.Add(-vrijediPotvrdaUnatrag)) || vrijedi.After(sad.Add(vrijediPotvrdaUnaprijed))
}

// pitanjeVrijedi je pitanje uz „Vrijedi od” izvan uobičajenog raspona
func pitanjeVrijedi(vrijedi time.Time) string {
	return "„Vrijedi od” je " + vrijedi.In(models.Zagreb).Format("2. 1. 2006. u 15:04") + " – jeste li sigurni?"
}

// potvrdeVrijedi su potvrde koje nacrt dobiva kad je osoba potvrdila
// „Vrijedi od” izvan uobičajenog raspona; bez potvrde ili kad ona ne treba,
// nijedna
func potvrdeVrijedi(a *models.Akt, u *models.User, potvrdeno bool, sad time.Time) []models.PotvrdaAkta {
	if !potvrdeno || !vrijediTraziPotvrdu(a.Vrijedi, sad) {
		return nil
	}
	return []models.PotvrdaAkta{{Vrsta: models.PotvrdaVrijedi, Tekst: pitanjeVrijedi(a.Vrijedi) + " Potvrđeno.",
		TkoID: u.ID.String(), Tko: u.FullName, Kad: sad.UTC().Truncate(time.Second)}}
}

// provjeriVrijedi provjerava „Vrijedi od” novog nacrta prema otvorenom
// Dnevniku COP-a sektora (nil = pravilo aktivne obrane nije zadano) i
// trenutku sastavljanja
func provjeriVrijedi(a *models.Akt, dnevnik *models.Journal, sad time.Time) error {
	v := a.Vrijedi.In(models.Zagreb)
	if dnevnik != nil && dnevnik.StartedAt != nil && v.Before(*dnevnik.StartedAt) {
		return fmt.Errorf("„Vrijedi od” (%s) je prije otvaranja Dnevnika COP-a (%s): akt ne može vrijediti prije aktivne obrane u sektoru; provjerite dan i godinu",
			v.Format("2. 1. 2006. u 15:04"), dnevnik.StartedAt.In(models.Zagreb).Format("2. 1. 2006."))
	}
	if v.After(sad.Add(vrijediNajviseUnaprijed)) {
		return fmt.Errorf("„Vrijedi od” (%s) je više od 7 dana unaprijed; provjerite dan i godinu", v.Format("2. 1. 2006. u 15:04"))
	}
	if vrijediTraziPotvrdu(v, sad) && !a.ImaPotvrdu(models.PotvrdaVrijedi) {
		return &TrebaPotvrduError{Pitanje: pitanjeVrijedi(v)}
	}
	return nil
}

// provjeriNoviNacrt: novi nacrt nastaje samo u aktivnoj obrani i s
// „Vrijedi od” unutar granica; prazno „Vrijedi od” javlja Spremi
func (s *AktService) provjeriNoviNacrt(ctx context.Context, a *models.Akt, sad time.Time) error {
	dnevnik, err := s.trebaAktivnu(ctx, a)
	if err != nil || a.Vrijedi.IsZero() {
		return err
	}
	return provjeriVrijedi(a, dnevnik, sad)
}
