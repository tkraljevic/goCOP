package web

import (
	"net/http"
	"time"
)

// Kolačić sesije nosi token prijave. Postavlja se i briše samo ovdje, da
// svojstva budu ista na svakom mjestu: preglednik kolačić briše tek kad
// brisanje ima isto ime, putanju i domenu, a Secure i SameSite ne smiju ovisiti
// o tome koja ga je stranica postavila.
//
//   - HttpOnly: skripta na stranici ga ne čita, pa ga ni ubačena skripta ne
//     može poslati drugamo;
//   - SameSite=Lax: tuđa stranica ga ne šalje uz POST;
//   - Secure kad je izvorni zahtjev bio HTTPS (javni čvor kroz tunel), pa ga
//     preglednik ne šalje preko nešifrirane veze; računalo u lokalnoj mreži
//     radi na http i ondje Secure ne smije stajati, inače prijava ne bi radila.
//
// Ime ostaje gocop_session: preimenovanje (npr. __Host-) odjavilo bi sve.

const imeKolacicaSesije = "gocop_session"

// postaviSesiju upisuje token sesije u preglednik
func postaviSesiju(w http.ResponseWriter, r *http.Request, token string, istek time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     imeKolacicaSesije,
		Value:    token,
		Path:     "/",
		Expires:  istek,
		HttpOnly: true,
		Secure:   klijentIz(r).HTTPS,
		SameSite: http.SameSiteLaxMode,
	})
}

// obrisiSesiju briše kolačić sesije s istim svojstvima s kojima je postavljen
func obrisiSesiju(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     imeKolacicaSesije,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   klijentIz(r).HTTPS,
		SameSite: http.SameSiteLaxMode,
	})
}
