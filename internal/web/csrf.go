package web

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Zaštita od tuđih stranica (CSRF). Preglednik uz svaki zahtjev javlja
// odakle je pokrenut: Sec-Fetch-Site, a stariji ili na običnom HTTP-u
// Origin. Izmjena (POST) koju je pokrenula druga stranica odbija se, pa
// kolačić sesije prijavljenog korisnika tuđoj stranici ne vrijedi ništa.
//
// Što prolazi:
//   - GET, HEAD i OPTIONS uvijek (tunel razmjene i tok događaja su GET);
//   - zahtjev s iste stranice: https://cop-osijek.com kroz tunel (cloudflared
//     čuva Host), http://localhost:8080 i adresa u lokalnoj mreži, gdje
//     preglednik ne šalje Sec-Fetch-Site, ali Origin odgovara Hostu;
//   - zahtjev bez obaju zaglavlja: ne šalje ga preglednik (alati, drugi
//     čvorovi), pa ga ni tuđa stranica ne može podmetnuti.
//
// Druga stranica na istom računalu ili istoj adresi s drugim vratima
// (druga aplikacija na Unraidu, drugi razvojni poslužitelj na localhostu)
// također je tuđa: SameSite=Lax je takvima kolačić slao, ova zaštita ne.

// porukaTudjaStranica je razlog odbijanja koji vidi korisnik
const porukaTudjaStranica = "Zahtjev je odbijen: poslala ga je druga stranica, a ne goCOP. Otvorite goCOP izravno i pokušajte ponovno."

// zastitaOdTudjihStranica odbija izmjene koje je pokrenula tuđa stranica
func (s *Server) zastitaOdTudjihStranica(next http.Handler) http.Handler {
	cop := http.NewCrossOriginProtection()
	cop.SetDenyHandler(http.HandlerFunc(odbijTudjuStranicu))
	return cop.Handler(next)
}

// odbijTudjuStranicu javlja 403; skripti koja čeka JSON odgovara JSON-om
func odbijTudjuStranicu(w http.ResponseWriter, r *http.Request) {
	if jsonTijelo(r) || strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": porukaTudjaStranica, "message": porukaTudjaStranica})
		return
	}
	http.Error(w, porukaTudjaStranica, http.StatusForbidden)
}
