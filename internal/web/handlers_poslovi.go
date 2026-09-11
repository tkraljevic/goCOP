package web

import (
	"encoding/json"
	"net/http"

	"gocop/internal/models"
)

// Stanje dugog posla: stranica ga pita svakih sekundu dok traje.
//
// Odgovor je JSON, a ne komad stranice, jer isti posao prati više mjesta —
// vrata arhive, izdavanje, sažimanje baze — i svako ga crta na svoj način.

// StanjePosla javlja kako stoji posao koji je ovaj čovjek pokrenuo.
func (s *Server) StanjePosla(w http.ResponseWriter, r *http.Request) {
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	if u == nil {
		http.Error(w, "nije prijavljen", http.StatusUnauthorized)
		return
	}
	p, ima := s.poslovi.Nadi(r.PathValue("id"), u.ID.String())
	if !ima {
		// Posao koji je davno gotov više ne postoji, a stranica koja ga pita
		// mora to razlikovati od greške — inače bi zauvijek vrtjela traku.
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"stanje": "nepoznat"})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(p.Stanje())
}
