package web

import (
	"errors"
	"net/http"
	"time"

	"gocop/internal/service"
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

// Kolačići drugog koraka prijave izvana (PIN). Prefiks __Host- preglednik
// prihvaća samo sa Secure, Path=/ i bez Domain, pa ga ne može podmetnuti ni
// poddomena ni nešifrirana veza. Secure zato stoji uvijek: bez njega
// preglednik kolačić tiho odbaci i prijava se vrti u krug. Drugi korak radi
// samo kad je preglednik došao preko HTTPS-a, izravno ili do posrednika
// (tunel); izvana preko nešifriranog http-a prijava s PIN-om se odbija
// prije slanja PIN-a (sigurnoIzvana).
//
//   - prijava na čekanju: lozinka je točna, a PIN ili kod još nije upisan.
//     SameSite=Strict (s tuđe stranice ne ide uopće), rok kao PIN-u;
//   - zapamćeno računalo: jedan nasumičan token po pregledniku, zajednički
//     svima koji se s njega prijavljuju (u bazi redak po osobi). SameSite=Lax
//     kao sesija; odjava ga ne briše, jer pamti računalo, a ne prijavu.
const (
	imeKolacicaPrijave  = "__Host-gocop_prijava"
	imeKolacicaRacunala = "__Host-gocop_racunalo"
)

// sigurnoIzvana javlja može li preglednik primiti kolačiće drugog koraka:
// zahtjev je stigao preko HTTPS-a, ili posrednik javlja da je preglednik
// do njega došao preko HTTPS-a. Tu se posredniku ne mora vjerovati:
// podmetnuto zaglavlje šteti samo onome tko ga šalje (njegov preglednik
// kolačić ne primi). Posrednik preko nešifriranog http-a ne prolazi, jer
// bi se prijava vrtjela u krug.
func sigurnoIzvana(r *http.Request) bool {
	return klijentIz(r).HTTPS || proslijedenHTTPS(r)
}

// vrijednostKolacica vraća vrijednost kolačića ili prazno
func vrijednostKolacica(r *http.Request, ime string) string {
	if c, err := r.Cookie(ime); err == nil {
		return c.Value
	}
	return ""
}

// postaviPrijavuNaCekanju upisuje token prijave koja čeka PIN ili kod
func postaviPrijavuNaCekanju(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     imeKolacicaPrijave,
		Value:    token,
		Path:     "/",
		MaxAge:   int(service.TrajanjePrijaveNaCekanju / time.Second),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

// obrisiPrijavuNaCekanju briše kolačić prijave na čekanju istim svojstvima
func obrisiPrijavuNaCekanju(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     imeKolacicaPrijave,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

// postaviRacunalo upisuje token zapamćenog računala; rok se upotrebom ne
// produljuje, pa kolačić postavlja samo novo pamćenje
func postaviRacunalo(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     imeKolacicaRacunala,
		Value:    token,
		Path:     "/",
		MaxAge:   int(service.TrajanjeRacunala / time.Second),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// obrisiNevaljanuSesiju briše kolačić sesije koja više ne vrijedi (istekla,
// nepoznata ili račun isključen). Kod greške baze kolačić ostaje: sesija možda
// vrijedi, a prijava se ne gubi zbog prolaznog kvara.
func obrisiNevaljanuSesiju(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, service.ErrSessionExpired) || errors.Is(err, service.ErrAccountInactive) {
		obrisiSesiju(w, r)
	}
}
