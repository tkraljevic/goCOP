package web

import "net/http"

// Ono što pripada samo ovom računalu čuva isto pravilo kao postavke čvora
// (smijeMijenjatiCvor), a rečenice kažu o čemu je riječ (Odluke
// (30. 10. 2026.), 10 i 13): izvori arhive, čuvanje povijesti letve, stara
// .db arhiva i paket s diska dok računalo još nije u mreži.
var (
	zaIzvore       = func(z *zabranaCvora) string { return z.izvori }
	zaPovijest     = func(z *zabranaCvora) string { return z.povijest }
	zaStaruArhivu  = func(z *zabranaCvora) string { return z.staraArhiva }
	zaNeprovjereno = func(z *zabranaCvora) string { return z.neprovjeren }
	zaKnjigu       = func(z *zabranaCvora) string { return z.knjiga }
)

// porukaZabrane vraća rečenicu kojom se zahtjev odbija, ili prazno kad ga
// smijeMijenjatiCvor pušta
func porukaZabrane(r *http.Request, poruka func(*zabranaCvora) string) string {
	if z := smijeMijenjatiCvor(r); z != nil {
		return poruka(z)
	}
	return ""
}
