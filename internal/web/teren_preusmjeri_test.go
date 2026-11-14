package web

import (
	"net/http"
	"testing"

	"gocop/internal/models"
)

// Stranica Teren je ukinuta (Odluke (14. 11. 2026.)): stare poveznice vode
// na Očitanja s istim područjem, a kartice Naslovne na Očitanja područja.
func TestTerenVodiNaOcitanja(t *testing.T) {
	p := noviPosluziteljOcitanja(t)
	for put, cilj := range map[string]string{
		"/teren":          "/readings",
		"/teren?area=16":  "/readings?area=16",
		"/teren?area=x":   "/readings",
		"/teren?sector=B": "/readings",
	} {
		w := p.posalji("dežurni", http.MethodGet, put, nil, "")
		if w.Code != http.StatusFound || w.Header().Get("Location") != cilj {
			t.Errorf("%s: %d %q, a treba %q", put, w.Code, w.Header().Get("Location"), cilj)
		}
	}
	if w := p.posalji("", http.MethodGet, "/teren", nil, ""); w.Code == http.StatusFound && w.Header().Get("Location") == "/readings" {
		t.Error("bez prijave /teren ne traži prijavu")
	}
	if ocitanjaPodrucja(nil) != "/readings" || ocitanjaPodrucja(&models.Area{ID: 16}) != "/readings?area=16" {
		t.Error("poveznica Naslovne na Očitanja područja")
	}
}
