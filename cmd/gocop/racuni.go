package main

// Prijave čvora na zatvorene stranice: Geolux HydroView (po letvi),
// mletva.voda.hr i letva.voda.hr (jedan račun čvora za sve postaje). Računi
// stoje na ovom čvoru, šifrirani ključem čvora, i čitaju se pri svakom
// preuzimanju — tako promjena lozinke odmah vrijedi, bez ponovnog
// pokretanja.

import (
	"context"
	"database/sql"
	"log"

	"gocop/internal/mletva"
	"gocop/internal/posta"
	"gocop/internal/repository"
)

// hidroViewRacun daje prijavu za letvu na HydroViewu. Letva može imati svoj
// račun; kad nema, vrijedi račun čvora.
func hidroViewRacun(database *sql.DB, hidroviewRepo *repository.HidroViewRepository, hidroviewKljuc []byte) func(adresa string) (string, string, bool) {
	return func(adresa string) (string, string, bool) {
		// Letva se traži i po adresi javne stranice i po šifri postaje na
		// telemetriji, jer se adresa u drugom slučaju sastavlja iz šifre.
		var letva string
		_ = database.QueryRow(`SELECT code FROM stations
			WHERE javni_url = ? OR (telemetrija_site <> '' AND instr(?, telemetrija_site) > 0)
			LIMIT 1`, adresa, adresa).Scan(&letva)
		r, err := hidroviewRepo.Racun(context.Background(), letva)
		if err != nil || r == nil {
			return "", "", false
		}
		lozinka, err := posta.Otkljucaj(hidroviewKljuc, r.Lozinka)
		if err != nil {
			log.Printf("HydroView: lozinka za %q se ne da otključati: %v", letva, err)
			return "", "", false
		}
		return r.Korisnik, lozinka, true
	}
}

// racunSustava daje prijavu računom čvora za mletva.voda.hr; istim se
// računom čvor prijavljuje i na letva.voda.hr. oznaka je ime stranice u
// dnevniku kad se lozinka ne da otključati.
func racunSustava(racuni *repository.RacuniSustavaRepository, kljuc []byte, oznaka string) func() (string, string, bool) {
	return func() (string, string, bool) {
		r, err := racuni.Racun(context.Background(), mletva.Podrijetlo)
		if err != nil || r == nil {
			return "", "", false
		}
		lozinka, err := posta.Otkljucaj(kljuc, r.Lozinka)
		if err != nil {
			log.Printf("%s: lozinka se ne da otključati: %v", oznaka, err)
			return "", "", false
		}
		return r.Korisnik, lozinka, true
	}
}
