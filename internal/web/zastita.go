package web

import (
	"net/http"
	"time"
)

// Handler je cijeli web poslužitelj sa zaštitnim slojevima, izvana prema
// unutra: porijeklo zahtjeva, sigurnosna zaglavlja, zaštita od tuđih
// stranica (CSRF), rokovi i veličine, priručna memorija, pa rute.
func (s *Server) Handler() http.Handler {
	var h http.Handler = s.mux
	h = bezPriruckeMemorije(h)
	h = s.rokovi(h)
	h = s.zastitaOdTudjihStranica(h)
	h = zaglavlja(h)
	h = s.klijentSloj(h)
	return h
}

// Start pokreće web poslužitelj. Rokovi čitanja i pisanja nisu zajednički
// (tok događaja i tunel razmjene traju satima, izvoz i uvoz minutama), pa
// ih postavlja sloj rokova po ruti; ovdje su samo zaglavlja i mirovanje.
// IdleTimeout mora biti dulji od cloudflaredovih 90 s.
func (s *Server) Start() error {
	srv := &http.Server{
		Addr:              s.addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	return srv.ListenAndServe()
}
