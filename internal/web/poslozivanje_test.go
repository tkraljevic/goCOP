package web

import (
	"net/http"
	"testing"
)

// Poslužitelj se sastavlja u koracima: rute se registriraju jednom, a
// SetDatabase, SetArhiva i SetKarta stižu poslije, jer se ono što daju ne zna
// prije. Nijedan od tih koraka ne smije registrirati rute drugi put —
// ServeMux na to panici i program se uopće ne podigne.
//
// Test je nastao nakon što se upravo to dogodilo: SetKarta je pozivao
// setupRoutes, program je padao pri pokretanju, a nijedan test to nije vidio
// jer nijedan nije prolazio kroz taj slijed.
func TestPostavljaciNeRegistrirajuRuteDvaput(t *testing.T) {
	s := &Server{mux: http.NewServeMux()}
	s.mux.Handle("GET /proba", http.NotFoundHandler())

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("postavljač je registrirao rute drugi put: %v", r)
		}
	}()
	s.SetKarta("https://primjer/{z}/{x}/{y}.png", "zasluge", 17)
	s.SetAddr(":8080")

	if !s.karta.Ima() || s.karta.NajviseZ != 17 {
		t.Errorf("postavke karte nisu spremljene: %+v", s.karta)
	}
	// ista ruta mora i dalje biti registrirana samo jednom
	s.mux.Handle("GET /proba2", http.NotFoundHandler())
}
