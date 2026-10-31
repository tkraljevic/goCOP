package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Školjka je ono što je isto na svakoj stranici (base.html): izbornik sa
// strane, gornja traka s naslovom, oznakom razmjene, zvonom i osobom, te
// donja traka na mobitelu. Stavke izbornika i njihov redoslijed su iz
// sustava dizajna (Odluke (31. 10. 2026.)).

// navNaslov je naslov stranice u gornjoj traci: stavka izbornika kojoj
// stranica pripada, kako je vidi korisnik
func navNaslov(activeNav string) string {
	switch navSkupina(activeNav) {
	case "dashboard":
		return "Naslovna"
	case "readings":
		return "Očitanja"
	case "prognoze":
		return "Prognoze"
	case "journals":
		return "Dokumenti"
	case "teren":
		return "Teren"
	case "sredstva":
		return "Sredstva"
	case "registri":
		return "Registri"
	case "users":
		return "Djelatnici i dežurstva"
	case "admin":
		return "Administracija"
	case "pomoc":
		return "Pomoć"
	case "profile":
		return "Profil"
	case "posta":
		return "Sandučić"
	}
	return "goCOP"
}

// navSkupina je stavka izbornika kojoj stranica pripada: registri i
// administracija imaju više stranica, pa ih izbornik ističe kao jednu
func navSkupina(activeNav string) string {
	switch activeNav {
	case "", "dashboard":
		return "dashboard"
	case "registers", "organizacija", "sections", "territories", "stations", "structures",
		"watercourses", "slivovi", "maintenance", "firme":
		return "registri"
	case "admin", "sync", "moduli", "settings":
		return "admin"
	}
	return activeNav
}

// inicijali su slova za krug osobe u gornjoj traci: prvo slovo prvog i
// zadnjeg imena, najviše dva
func inicijali(ime string) string {
	rijeci := strings.Fields(ime)
	if len(rijeci) == 0 {
		return "?"
	}
	prvo := func(s string) string {
		r, _ := utf8.DecodeRuneInString(s)
		return string(unicode.ToUpper(r))
	}
	if len(rijeci) == 1 {
		return prvo(rijeci[0])
	}
	return prvo(rijeci[0]) + prvo(rijeci[len(rijeci)-1])
}

// ShowRazmjenaZnak je stanje razmjene za oznaku u gornjoj traci: samo
// jedna riječ, da svaka stranica ne crta cijelu pločicu. „uskladjeno” je
// zelena točka, „ceka” žuta, „nema” bez točke (čvor nije u mreži ili se
// razmjena ne prati).
func (s *Server) ShowRazmjenaZnak(w http.ResponseWriter, r *http.Request) {
	stanje := "nema"
	if s.razmjena != nil {
		ctx, otkazi := context.WithTimeout(r.Context(), 3*time.Second)
		defer otkazi()
		st := s.razmjena(ctx)
		switch {
		case !st.UMrezi:
			stanje = "nema"
		case st.Sredeno():
			stanje = "uskladjeno"
		default:
			stanje = "ceka"
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]string{"stanje": stanje})
}
