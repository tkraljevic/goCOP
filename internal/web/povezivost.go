package web

// WEB sync u ovom pokretanju (plan WEB synca, F1a). Kad je program dvaput
// zaredom zatečen kako se prekinuo dok je WEB sync radio, kreće bez njega
// (internal/peers/pad_povezivosti.go). Pločica razmjene to kaže svima, a
// administrator ga u Administraciji ponovno uključuje; to vrijedi od
// idućeg pokretanja, kao i promjena postavki.

import (
	"context"
	"log"
	"net/http"
)

// StanjePovezivosti je WEB sync u ovom pokretanju
type StanjePovezivosti struct {
	NakonPadova bool // radi bez WEB synca, jer se dvaput zaredom prekinuo
	UkljucujeSe bool // oznaka padova je obrisana: WEB sync kreće idućim pokretanjem
}

// povezivostCvora su stanje WEB synca, njegovo ponovno uključivanje i
// provjera veze (provjera_veze.go) i stanje prema sastajalištu
// (signal_natpis.go)
type povezivostCvora struct {
	stanje   func() StanjePovezivosti
	ukljuci  func() error
	provjeri func(context.Context) ProvjeraVeze
	zadnja   func() *ProvjeraVeze
	signal   func() SignalStanje
}

// SetPovezivost daje poslužitelju stanje WEB synca i ponovno uključivanje
// nakon padova; zove se prije Start
func (s *Server) SetPovezivost(stanje func() StanjePovezivosti, ukljuci func() error) {
	s.povezivost.stanje, s.povezivost.ukljuci = stanje, ukljuci
}

func (p povezivostCvora) trenutno() StanjePovezivosti {
	if p.stanje == nil {
		return StanjePovezivosti{}
	}
	return p.stanje()
}

// upozorenja su rečenice za pločicu razmjene; vidi ih svaki prijavljeni
// korisnik
func (p povezivostCvora) upozorenja() []string {
	switch st := p.trenutno(); {
	case st.UkljucujeSe:
		return []string{"WEB sync je isključen nakon padova programa i uključit će se idućim pokretanjem. Razmjena dotad ide kao i prije."}
	case st.NakonPadova:
		return []string{"WEB sync je isključen, jer se program dvaput zaredom prekinuo dok je WEB sync radio. Razmjena ide kao i prije. " +
			"Administrator ga ponovno uključuje u Administraciji, na stranici Ovo računalo i mreža, u kartici WEB sync i dohvatljivost."}
	}
	return nil
}

// WebSyncPrikaz je dio kartice Ovaj čvor kad je WEB sync isključen nakon
// padova
type WebSyncPrikaz struct {
	UkljucujeSe bool
	Nudi        bool   // gumb: administrator, svojim očima, izravno iz lokalne mreže
	Zabrana     string // zašto ne s ovog zahtjeva
}

// prikazWebSynca je nil kad WEB sync nije isključen nakon padova
func (s *Server) prikazWebSynca(zabrana *zabranaCvora) *WebSyncPrikaz {
	st := s.povezivost.trenutno()
	if !st.NakonPadova {
		return nil
	}
	p := &WebSyncPrikaz{UkljucujeSe: st.UkljucujeSe, Nudi: zabrana == nil}
	if zabrana != nil {
		p.Zabrana = zabrana.postavke
	}
	return p
}

// handleUkljuciWebSync: POST, samo administrator, izravno iz lokalne
// mreže. Obriše oznaku padova, pa WEB sync kreće idućim pokretanjem.
func (s *Server) handleUkljuciWebSync(w http.ResponseWriter, r *http.Request) {
	if z := smijeMijenjatiCvor(r); z != nil {
		http.Error(w, z.postavke, http.StatusForbidden)
		return
	}
	if !s.povezivost.trenutno().NakonPadova {
		redirectWith(w, r, "/settings#web-sync-padovi", "error", "WEB sync nije isključen nakon padova, pa ga nije trebalo ponovno uključiti.")
		return
	}
	if err := s.povezivost.ukljuci(); err != nil {
		redirectWith(w, r, "/settings#web-sync-padovi", "error", "WEB sync nije ponovno uključen: oznaka padova uz bazu ne da se obrisati ("+err.Error()+").")
		return
	}
	log.Printf("povezivost: WEB sync ponovno uključio %s s %s; vrijedi od idućeg pokretanja", korisnikZahtjeva(r), klijentIz(r))
	redirectWith(w, r, "/settings#web-sync-padovi", "success", "WEB sync se ponovno uključuje idućim pokretanjem čvora.")
}
