package web

import (
	"net/http"

	"gocop/internal/service"
)

// SetPropisi spaja propise (Dokumenti › Propisi)
func (s *Server) SetPropisi(p *service.PropisiService) { s.propisi = p }

// rutePropisa su rute Dokumenti › Propisi. Propise čita svaki prijavljeni
// (Odluke (25. 11. 2026.), O2), pa /propisi nije u modulePaths: vodočuvar
// čita svoj pravilnik i bez modula Dokumenti.
func (s *Server) rutePropisa() {
	h := NewPropisiHandler(func() *service.PropisiService { return s.propisi },
		s.templates["propisi.html"], s.templates["propis.html"], s.templates["propis_izmjena.html"])
	s.mux.Handle("GET /propisi", s.authMiddleware(http.HandlerFunc(h.Popis)))
	s.mux.Handle("GET /propisi/{propis}", s.authMiddleware(http.HandlerFunc(h.Propis)))
	s.mux.Handle("GET /propisi/{propis}/{dio}", s.authMiddleware(http.HandlerFunc(h.Dio)))
	s.mux.Handle("GET /propisi/{propis}/izmjene/{izmjena}", s.authMiddleware(http.HandlerFunc(h.Izmjena)))
}
