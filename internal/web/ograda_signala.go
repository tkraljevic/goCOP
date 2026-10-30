package web

// Sastajalište za WEB sync (plan WEB synca, F2a). Računala se prijavljuju na
// /razmjena/signal i drže vezu satima, kao i tunel, bez prijave korisnika:
// tko je s druge strane, dokazuje se ključem unutar TLS-a koji teče kroz
// WebSocket. Ruta odgovara samo na računalu koje je sastajalište
// (posluzi_sastajaliste = true, uz prijavu za WEB sync) i kad mu je program
// dao rukovatelja (F2b); inače je 404, kao da je nema.
//
// Ograda je šira od tunelske: 64 veze ukupno, koliko računala drži
// registar, i 8 po adresi, jer cijeli ured izlazi s jedne ili dvije adrese.
// Rukovanja unutar veze ograđuje sam rukovatelj (2 po adresi, rok 2 s).

import (
	"net/http"

	"gocop/internal/razmjena"
)

const (
	najviseSignala           = 64
	najviseSignalaPoKlijentu = 8
)

// SetSignal daje poslužitelju rukovatelja sastajališta; zove se prije Start
func (s *Server) SetSignal(h http.Handler) { s.signal = h }

// posluziSastajaliste javlja je li ovo računalo sastajalište
func (s *Server) posluziSastajaliste() bool {
	if s.postavke == nil {
		return false
	}
	p := s.postavke.snimka().Vrijedi.Povezivost
	return p.PosluziSastajaliste && p.WebSyncPrijava
}

// upozorenjeSastajalista: računalo je postavljeno kao sastajalište, ali bez
// prijave za WEB sync ruta vraća 404, pa se kod njega nitko ne prijavi
func (s *Server) upozorenjeSastajalista() string {
	if s.postavke == nil {
		return ""
	}
	if p := s.postavke.snimka().Vrijedi.Povezivost; !p.PosluziSastajaliste || p.WebSyncPrijava {
		return ""
	}
	return "Ovo računalo je postavljeno kao sastajalište za WEB sync (posluzi_sastajaliste), ali prijava za WEB sync je " +
		"isključena, pa se druga računala kod njega ne mogu prijaviti. Administrator je uključuje u Administraciji, na stranici Ovo računalo i mreža, u kartici WEB sync i dohvatljivost."
}

// handleSignal predaje vezu rukovatelju sastajališta, ili 404. Uz vezu ide
// klijent kako ga vidi web (adresa iza posrednika, mreža /48 za IPv6), po
// kojem sastajalište ograđuje rukovanja.
func (s *Server) handleSignal(w http.ResponseWriter, r *http.Request) {
	if s.signal == nil || !s.posluziSastajaliste() {
		http.NotFound(w, r)
		return
	}
	klijent := kljucKlijentaTunela(klijentIz(r).Adresa).String()
	s.signal.ServeHTTP(w, r.WithContext(razmjena.SKlijentom(r.Context(), klijent)))
}

// ograditiSignal omata rukovatelja sastajališta; mjesto se pušta kad veza
// završi
func ograditiSignal(next http.Handler) http.Handler {
	return novaOgradaTunela(najviseSignala, najviseSignalaPoKlijentu).omotaj(next, "Sastajalište već drži najviše veza; pokušajte kasnije.")
}
