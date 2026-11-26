package web

import (
	"net/http"

	"gocop/internal/service"
)

// SetPropisi spaja propise (Dokumenti › Propisi)
func (s *Server) SetPropisi(p *service.PropisiService) { s.propisi = p }

// rutePropisa su rute Dokumenti › Propisi. Propise čita svaki prijavljeni
// (Odluke (25. 11. 2026.), O2), pa /propisi nije u modulePaths: vodočuvar
// čita svoj pravilnik i bez modula Dokumenti. Učitava, dopunjuje i uklanja
// ih samo globalni administrator (samoAdmin); doslovni segmenti (ucitaj,
// dodaj-izmjenu, izmjene, ukloni) određeniji su od {propis} i {dio}, pa se
// rute ne sudaraju.
func (s *Server) rutePropisa() {
	h := NewPropisiHandler(func() *service.PropisiService { return s.propisi },
		s.templates["propisi.html"], s.templates["propis.html"], s.templates["propis_izmjena.html"])
	s.mux.Handle("GET /propisi", s.authMiddleware(http.HandlerFunc(h.Popis)))
	s.mux.Handle("GET /propisi/{propis}", s.authMiddleware(http.HandlerFunc(h.Propis)))
	s.mux.Handle("GET /propisi/{propis}/{dio}", s.authMiddleware(http.HandlerFunc(h.Dio)))
	s.mux.Handle("GET /propisi/{propis}/izmjene/{izmjena}", s.authMiddleware(http.HandlerFunc(h.Izmjena)))

	u := NewPropisiUcitajHandler(h, s.templates["propisi_ucitaj.html"], s.templates["propisi_provjera.html"], s.templates["propisi_provjera_izmjene.html"])
	s.mux.Handle("GET /propisi/ucitaj", s.samoAdmin(http.HandlerFunc(u.ObrazacUcitaj)))
	s.mux.Handle("POST /propisi/ucitaj", s.samoAdmin(http.HandlerFunc(u.Ucitaj)))
	s.mux.Handle("GET /propisi/ucitaj/provjera", s.samoAdmin(http.HandlerFunc(u.Provjera)))
	s.mux.Handle("POST /propisi/ucitaj/spremi", s.samoAdmin(http.HandlerFunc(u.SpremiPropis)))
	s.mux.Handle("GET /propisi/{propis}/dodaj-izmjenu", s.samoAdmin(http.HandlerFunc(u.ObrazacIzmjene)))
	s.mux.Handle("POST /propisi/{propis}/dodaj-izmjenu", s.samoAdmin(http.HandlerFunc(u.UcitajIzmjenu)))
	s.mux.Handle("GET /propisi/{propis}/dodaj-izmjenu/provjera", s.samoAdmin(http.HandlerFunc(u.ProvjeraIzmjene)))
	s.mux.Handle("POST /propisi/{propis}/dodaj-izmjenu/spremi", s.samoAdmin(http.HandlerFunc(u.SpremiIzmjenu)))
	s.mux.Handle("POST /propisi/{propis}/ukloni", s.samoAdmin(http.HandlerFunc(u.UkloniPropis)))
	s.mux.Handle("POST /propisi/{propis}/izmjene/{izmjena}/ukloni", s.samoAdmin(http.HandlerFunc(u.UkloniIzmjenu)))
}
