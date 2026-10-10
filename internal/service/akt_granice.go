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
// najava unaprijed ispravna su praksa. Tvrde granice provjeravaju se
// ponovno pri ovjeri (vrijediZaOvjeru), a potvrda se pri ovjeri traži kad je
// akt još nema (potvrdiVrijediPriOvjeri). Godina oznake i dalje je godina iz
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

// granicaVrijedi provjerava tvrde granice „Vrijedi od” prema otvorenom
// Dnevniku COP-a sektora (nil = pravilo aktivne obrane nije zadano) i
// trenutku sad: ne prije otvaranja dnevnika i ne više od 7 dana unaprijed
func granicaVrijedi(vrijedi time.Time, dnevnik *models.Journal, sad time.Time) error {
	v := vrijedi.In(models.Zagreb)
	if dnevnik != nil && dnevnik.StartedAt != nil && v.Before(*dnevnik.StartedAt) {
		return fmt.Errorf("„Vrijedi od” (%s) je prije otvaranja Dnevnika COP-a (%s): akt ne može vrijediti prije aktivne obrane u sektoru; provjerite dan i godinu",
			v.Format("2. 1. 2006. u 15:04"), dnevnik.StartedAt.In(models.Zagreb).Format("2. 1. 2006."))
	}
	if v.After(sad.Add(vrijediNajviseUnaprijed)) {
		return fmt.Errorf("„Vrijedi od” (%s) je više od 7 dana unaprijed; provjerite dan i godinu", v.Format("2. 1. 2006. u 15:04"))
	}
	return nil
}

// provjeriVrijedi provjerava „Vrijedi od” novog nacrta: tvrde granice
// (granicaVrijedi) i, izvan uobičajenog raspona, izričitu potvrdu
func provjeriVrijedi(a *models.Akt, dnevnik *models.Journal, sad time.Time) error {
	if err := granicaVrijedi(a.Vrijedi, dnevnik, sad); err != nil {
		return err
	}
	if vrijediTraziPotvrdu(a.Vrijedi, sad) && !a.ImaPotvrdu(models.PotvrdaVrijedi) {
		return &TrebaPotvrduError{Pitanje: pitanjeVrijedi(a.Vrijedi)}
	}
	return nil
}

// vrijediZaOvjeru: tvrde granice „Vrijedi od” vrijede i pri ovjeri, na oba
// puta, jer nacrt koji ih ne poštuje može doći s čvora sa starijim
// programom ili iz obrane čiji je dnevnik u međuvremenu zatvoren. Potvrdu
// za više od 24 sata unatrag ili 3 dana unaprijed provjerava
// potvrdiVrijediPriOvjeri.
func vrijediZaOvjeru(a *models.Akt, dnevnik *models.Journal, sad time.Time) error {
	if err := granicaVrijedi(a.Vrijedi, dnevnik, sad); err != nil {
		return fmt.Errorf("akt se ne može ovjeriti — %w; nacrtu se „Vrijedi od” ne mijenja, pa treba novi nacrt", err)
	}
	return nil
}

// PitanjeVrijediZaOvjeru je pitanje koje ovjera nacrta postavlja kad je
// „Vrijedi od” u trenutku sad više od 24 sata unatrag ili više od 3 dana
// unaprijed, a akt potvrdu još ne nosi (nacrt je sastavljen dok je vrijeme
// bilo u uobičajenom rasponu, ili na čvoru sa starijim programom). Prazno
// kad potvrda ne treba.
func PitanjeVrijediZaOvjeru(a models.Akt, sad time.Time) string {
	if a.Ovjeren() || a.ImaPotvrdu(models.PotvrdaVrijedi) || !vrijediTraziPotvrdu(a.Vrijedi, sad) {
		return ""
	}
	return pitanjeVrijedi(a.Vrijedi)
}

// potvrdiVrijediPriOvjeri: dok ovjera postavlja pitanje uz „Vrijedi od”
// (PitanjeVrijediZaOvjeru), prolazi samo uz izričitu potvrdu, koja se
// bilježi uz akt na onoga tko ovjeru izvodi
func potvrdiVrijediPriOvjeri(a *models.Akt, u *models.User, potvrdeno bool, sad time.Time) error {
	pitanje := PitanjeVrijediZaOvjeru(*a, sad)
	switch {
	case pitanje == "":
		return nil
	case !potvrdeno:
		return &TrebaPotvrduError{Pitanje: pitanje}
	}
	a.Potvrde = append(a.Potvrde, models.PotvrdaAkta{Vrsta: models.PotvrdaVrijedi, Tekst: pitanje + " Potvrđeno pri ovjeri.",
		TkoID: u.ID.String(), Tko: u.FullName, Kad: sad.UTC().Truncate(time.Second)})
	return nil
}

// potvrdeOvjere su izričite potvrde koje ovjera u goCOP-u traži prije
// upisa: „Vrijedi od” izvan uobičajenog raspona (pitanje 17) i popis
// primatelja složen pri ovjeri (pitanje 19)
func potvrdeOvjere(u *models.User, a *models.Akt, p PotvrdaOvjere, pr PregledPrimatelja, sad time.Time) error {
	if err := potvrdiVrijediPriOvjeri(a, u, p.Vrijedi, sad); err != nil {
		return err
	}
	return primateljiZaOvjeru(u, a, p, pr)
}

// potvrdeSkena su izričite potvrde koje traži ovjera skenom: „Vrijedi od”
// izvan uobičajenog raspona i popis primatelja s ispisa
func potvrdeSkena(u *models.User, a *models.Akt, p PotvrdaOvjere, pr PregledPrimatelja, sad time.Time) error {
	if err := potvrdiVrijediPriOvjeri(a, u, p.Vrijedi, sad); err != nil {
		return err
	}
	return primateljiSkena(u, a, p, pr)
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
