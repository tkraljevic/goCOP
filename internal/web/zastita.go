package web

import (
	"net"
	"net/http"
	"time"
)

// Handler je cijeli web poslužitelj sa zaštitnim slojevima, izvana prema
// unutra: porijeklo zahtjeva, sigurnosna zaglavlja, skrivanje od tražilica
// (i robots.txt, koji tako nosi sigurnosna zaglavlja), zaštita od tuđih
// stranica (CSRF), rokovi i veličine, priručna memorija, pa rute.
func (s *Server) Handler() http.Handler {
	var h http.Handler = s.mux
	h = bezPriruckeMemorije(h)
	h = s.rokovi(h)
	h = s.zastitaOdTudjihStranica(h)
	h = skrivenoOdTrazilica(h)
	h = zaglavlja(h)
	return s.klijentSloj(h)
}

// Start pokreće web poslužitelj. Rokovi čitanja i pisanja nisu zajednički
// (tok događaja i tunel razmjene traju satima, izvoz i uvoz minutama), pa
// ih postavlja sloj rokova po ruti; ovdje su samo zaglavlja i mirovanje.
// IdleTimeout mora biti dulji od cloudflaredovih 90 s.
func (s *Server) Start() error {
	addr := s.addr
	if addr == "" {
		addr = ":http" // kao ListenAndServe
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.slusaJednom.Do(func() { close(s.slusa) })
	srv := &http.Server{
		Addr:              s.addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	return srv.Serve(ln)
}

// Slusa se zatvori kad web prvi put sluša. Po njemu WEB sync otvara UDP
// utičnicu tek kad Postava već vidi da je novo izdanje krenulo.
func (s *Server) Slusa() <-chan struct{} { return s.slusa }
