package web

// Gumbi za obnovu članstva u Postavkama, pod Mreža (G9a): Obnovi članstvo
// za ovo računalo i Obnovi uz člana na računalu koje prima. Samo globalni
// administrator.

import (
	"net/http"

	"gocop/internal/models"
	"gocop/internal/razmjena"
)

// HandleObnoviClanstvo je gumb Obnovi članstvo: računalo s ključem mreže
// obnovi sebe odmah, a drugo upiše zahtjev koji ide uz razmjenu
func (h *SettingsHandler) HandleObnoviClanstvo(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	m, err := h.peers.ZatraziObnovu(r.Context())
	if err != nil {
		http.Error(w, "Obnova nije zatražena: "+err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"success": true, "message": porukaObnove(m)})
}

// porukaObnove je odgovor gumba Obnovi članstvo
func porukaObnove(m *razmjena.Membership) string {
	if m != nil {
		return "Članstvo ovog računala obnovljeno je i vrijedi do " + danHR(m.ExpiresAt.In(models.Zagreb))
	}
	return "Zahtjev za obnovu je upisan. Ide uz sljedeću razmjenu do računala koje prima u mrežu, a novo članstvo vraća se isto tako, obično za nekoliko minuta."
}

// HandleObnoviClana je gumb Obnovi uz člana: novo članstvo odmah, s istim
// imenom i ključem
func (h *SettingsHandler) HandleObnoviClana(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	m, err := h.peers.ObnoviClana(r.Context(), r.PathValue("node"))
	if err != nil {
		http.Error(w, "Članstvo nije obnovljeno: "+err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"success": true, "message": "Članstvo računala " + m.DeviceID + " obnovljeno je i vrijedi do " + danHR(m.ExpiresAt.In(models.Zagreb)) + " Do njega stiže razmjenom."})
}
