package web

// Provjera veze s računalom na stranici Sinkronizacija (plan WEB synca,
// F2a). Administrator izabere računalo, a program nazove svaku adresu koju
// za njega zna (peers.ProvjeriVezuS) i usporedno izmjeri mrežu ovog računala
// preko STUN poslužitelja, kao gumb Provjeri vezu. Na adresi stane čim se
// drugo računalo javi svojim ključem, pa se podaci ne razmjenjuju. Ishod
// nosi adrese i sirove greške, pa ga vidi samo administrator. Gumb radi samo
// svojim očima i izravno iz lokalne mreže, kao i Provjeri vezu. Zadnji ishod
// pamti se u memoriji, pa ga stranica iscrta nakon preusmjeravanja.

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"gocop/internal/models"
	"gocop/internal/peers"
)

// ProvjeraSRacunalom je jedna provjera veze s računalom
type ProvjeraSRacunalom struct {
	Racunalo peers.ProvjeraRacunala
	Mreza    *ProvjeraVeze // mreža ovog računala; nil kad je program ne mjeri
}

// zadnjaSRacunalom pamti zadnju provjeru veze s računalom
type zadnjaSRacunalom struct {
	mu sync.Mutex
	p  *ProvjeraSRacunalom
}

func (z *zadnjaSRacunalom) zapamti(p *ProvjeraSRacunalom) {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.p = p
}

func (z *zadnjaSRacunalom) zadnja() *ProvjeraSRacunalom {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.p
}

// mjeriMrezu je mjerenje gumba Provjeri vezu; nil kad ga program ne nudi
func (s *Server) mjeriMrezu(ctx context.Context) *ProvjeraVeze {
	if s.povezivost.provjeri == nil {
		return nil
	}
	p := s.povezivost.provjeri(ctx)
	return &p
}

// IzborRacunala je računalo u izborniku provjere
type IzborRacunala struct{ NodeID, Naziv string }

// RedakProvjere je jedna adresa u ispisu provjere
type RedakProvjere struct {
	Adresa, Izvor, Put, Ishod, Greska string
	Prosla                            bool
}

// ProvjeraRacunalaPrikaz je blok „Provjera veze s računalom”
type ProvjeraRacunalaPrikaz struct {
	Nudi     bool   // administrator, svojim očima, izravno iz lokalne mreže
	Zabrana  string // zašto ne s ovog zahtjeva
	Racunala []IzborRacunala
	Odabrano string // računalo zadnje provjere
	Zadnja   *ProvjeraSRacunalom
	Retci    []RedakProvjere
}

// rijeciIzvora je odakle ovo računalo zna adresu, riječima
var rijeciIzvora = map[peers.IzvorAdrese]string{
	peers.IzvorLokalnaMreza: "javilo se u lokalnoj mreži",
	peers.IzvorZapis:        "iz zapisa računala",
	peers.IzvorNaucena:      "ovdje je razmjena već prošla",
	peers.IzvorDolazna:      "s nje je stigla razmjena",
	peers.IzvorTailscale:    "Tailscale",
	peers.IzvorTunel:        "tunel",
}

// rijeciRazlogaAdrese je zašto adresa nije prošla, riječima
var rijeciRazlogaAdrese = map[peers.RazlogAdrese]string{
	peers.RazlogAdreseDrugiKljuc:  "javlja se drugo računalo (drugi ključ)",
	peers.RazlogAdreseNemaOdgovor: "nema odgovora",
	peers.RazlogAdreseOdbijeno:    "računalo je tu, ali na tom portu goCOP ne sluša",
	peers.RazlogAdreseNemaPuta:    "do adrese nema puta",
}

// redakProvjere je adresa provjere za ispis
func redakProvjere(a peers.AdresaProvjere) RedakProvjere {
	r := RedakProvjere{Adresa: a.Adresa, Izvor: rijeciIzvora[a.Izvor], Put: a.Put, Prosla: a.Prosla()}
	if r.Prosla {
		r.Ishod = fmt.Sprintf("javlja se pravim ključem, %d ms", max(1, a.RTT.Milliseconds()))
		return r
	}
	r.Ishod, r.Greska = rijeciRazlogaAdrese[a.Razlog], a.Greska
	if r.Ishod == "" {
		r.Ishod = "ne radi"
	}
	return r
}

// dodajProvjeruRacunala puni blok provjere, samo za administratora
func (h *SyncHandler) dodajProvjeruRacunala(r *http.Request, data *SyncPageData) {
	if data.Permissions == nil || !data.Permissions.IsGlobalAdmin {
		return
	}
	p := &ProvjeraRacunalaPrikaz{Nudi: true, Zadnja: h.provjera.zadnja()}
	if z := smijeMijenjatiCvor(r); z != nil {
		p.Nudi, p.Zabrana = false, z.provjera
	}
	if data.Status != nil {
		for _, ps := range data.Status.Peers {
			p.Racunala = append(p.Racunala, IzborRacunala{NodeID: ps.NodeID, Naziv: nazivIliOznaka(ps.Name, ps.NodeID)})
		}
	}
	if p.Zadnja != nil {
		p.Odabrano = p.Zadnja.Racunalo.NodeID
		for _, a := range p.Zadnja.Racunalo.Adrese {
			p.Retci = append(p.Retci, redakProvjere(a))
		}
	}
	data.ProvjeraRacunala = p
}

// nazivIliOznaka je naziv računala, a bez njega oznaka
func nazivIliOznaka(naziv, oznaka string) string {
	if strings.TrimSpace(naziv) == "" {
		return oznaka
	}
	return naziv
}

// handleProvjeriRacunalo: POST, samo administrator, svojim očima, izravno iz
// lokalne mreže. Traje do 10 s, koliko jedna adresa ima za odgovor.
func (h *SyncHandler) handleProvjeriRacunalo(w http.ResponseWriter, r *http.Request) {
	if z := smijeMijenjatiCvor(r); z != nil {
		http.Error(w, z.provjera, http.StatusForbidden)
		return
	}
	p, err := h.provjeriRacunalo(r.Context(), strings.TrimSpace(r.FormValue("cvor")))
	if err != nil {
		redirectWith(w, r, "/sinkronizacija#provjera-racunala", "error", "Provjera veze nije pokrenuta: "+err.Error())
		return
	}
	log.Printf("povezivost: provjeru veze s računalom %s pokrenuo %s s %s", p.Racunalo.Naziv, korisnikZahtjeva(r), klijentIz(r))
	redirectWith(w, r, "/sinkronizacija#provjera-racunala", "success", sazetakProvjere(p.Racunalo))
}

// provjeriRacunalo provjeri adrese računala i, usporedno, mrežu ovog
func (h *SyncHandler) provjeriRacunalo(ctx context.Context, cvor string) (*ProvjeraSRacunalom, error) {
	out := &ProvjeraSRacunalom{}
	var wg sync.WaitGroup
	if h.mjeriMrezu != nil {
		wg.Go(func() { out.Mreza = h.mjeriMrezu(ctx) })
	}
	rac, err := h.provjeriVezuS(ctx, cvor)
	wg.Wait()
	if err != nil {
		return nil, err
	}
	out.Racunalo = rac
	h.provjera.zapamti(out)
	return out, nil
}

// sazetakProvjere je ishod jednom rečenicom, bez adresa
func sazetakProvjere(p peers.ProvjeraRacunala) string {
	if len(p.Adrese) == 0 {
		return "Za računalo " + p.Naziv + " ovo računalo ne zna nijednu adresu."
	}
	prosle := 0
	for _, a := range p.Adrese {
		if a.Prosla() {
			prosle++
		}
	}
	return fmt.Sprintf("Provjera veze s računalom %s: javilo se na %d od %d %s.", p.Naziv, prosle, len(p.Adrese),
		models.UzBrojHR(len(p.Adrese), "adrese", "adrese", "adresa"))
}
