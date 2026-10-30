package web

// Gumbi za obnovu članstva na stranici Ovo računalo i mreža, pod Mreža (G9a): Obnovi članstvo
// za ovo računalo i Obnovi uz člana na računalu koje prima; i šifrirana
// kopija ključa mreže (G9b). Samo globalni administrator.

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/razmjena"
)

// HandleObnoviClanstvo je gumb Obnovi članstvo: računalo s ključem mreže
// obnovi sebe odmah, a drugo upiše zahtjev koji ide uz razmjenu
func (h *SettingsHandler) HandleObnoviClanstvo(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminSvojimOcima(r); err != nil {
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
	if err := requireAdminSvojimOcima(r); err != nil {
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

// najvecaKopijaKljuca ograđuje tijelo zahtjeva s kopijom ključa mreže
const najvecaKopijaKljuca = 64 << 10

// SetZastitaKljuca daje provjeru trenutne lozinke računa
// (AuthService.ProvjeriLozinku) za spremanje kopije ključa mreže
func (h *SettingsHandler) SetZastitaKljuca(lozinka func(uuid.UUID, string) error) {
	h.lozinkaRacuna = lozinka
}

// HandleKopijaKljuca je gumb Spremi šifriranu kopiju: vraća datoteku kopije
// ključa mreže za preuzimanje. Tko ima kopiju i njezinu lozinku, ima ključ
// mreže, a on se ne da opozvati. Zato samo administrator svojim očima, s
// ovog računala ili iz lokalne mreže, uz trenutnu lozinku računa; događaj
// ide u knjigu i administratorima e-poštom (javiKljuc).
func (h *SettingsHandler) HandleKopijaKljuca(w http.ResponseWriter, r *http.Request) {
	if z := smijeMijenjatiCvor(r); z != nil {
		http.Error(w, z.kljuc, http.StatusForbidden)
		return
	}
	var req struct {
		Lozinka       string `json:"lozinka"`
		LozinkaRacuna string `json:"lozinka_racuna"`
	}
	if err := decodeBody(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	u, _ := stvarnaOsoba(r)
	if err := h.provjeriLozinkuRacuna(r.Context(), u, req.LozinkaRacuna); err != nil {
		http.Error(w, "Kopija nije spremljena: "+err.Error(), http.StatusBadRequest)
		return
	}
	raw, ime, err := h.peers.KopijaKljucaMreze(r.Context(), req.Lozinka)
	if err != nil {
		http.Error(w, "Kopija nije spremljena: "+err.Error(), http.StatusBadRequest)
		return
	}
	h.javiKljuc(r, u, "spremio je kopiju ključa mreže")
	writeJSON(w, map[string]any{"success": true, "kopija": string(raw), "datoteka": ime})
}

// provjeriLozinkuRacuna provjerava trenutnu lozinku osobe, uz isto
// ograničenje krivih upisa kao pri promjeni lozinke
func (h *SettingsHandler) provjeriLozinkuRacuna(ctx context.Context, u *models.User, lozinka string) error {
	return ponovnaLozinkaRacuna(ctx, h.lozinkaRacuna, u, lozinka)
}

// HandleUcitajKopijuKljuca je gumb Učitaj kopiju ključa: nakon njega ovo
// računalo drži ključ mreže. Samo administrator svojim očima, s ovog
// računala ili iz lokalne mreže; događaj ide u knjigu i administratorima
// e-poštom (javiKljuc).
func (h *SettingsHandler) HandleUcitajKopijuKljuca(w http.ResponseWriter, r *http.Request) {
	if z := smijeMijenjatiCvor(r); z != nil {
		http.Error(w, z.kljuc, http.StatusForbidden)
		return
	}
	var req struct {
		Kopija  string `json:"kopija"`
		Lozinka string `json:"lozinka"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, najvecaKopijaKljuca)
	if err := decodeBody(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.peers.UcitajKopijuKljuca(r.Context(), []byte(req.Kopija), req.Lozinka); err != nil {
		http.Error(w, "Ključ mreže nije učitan: "+err.Error(), http.StatusBadRequest)
		return
	}
	u, _ := stvarnaOsoba(r)
	h.javiKljuc(r, u, "učitao je kopiju ključa mreže")
	writeJSON(w, map[string]any{"success": true, "message": "Ključ mreže je učitan. Ovo računalo sad prima u mrežu i obnavlja članstva i ovlasti."})
}

// javiKljuc zapisuje u dnevnik i u knjigu tko je i odakle napravio što s
// kopijom ključa mreže. E-poštu administratorima pošalje program
// (cmd/gocop, obavijesti), odmah s ovog računala ako ima račun za slanje,
// a inače s drugog kojem događaj stigne razmjenom.
func (h *SettingsHandler) javiKljuc(r *http.Request, u *models.User, sto string) {
	tko := opisOsobe(u)
	odakle := klijentIz(r).String()
	log.Printf("razmjena: %s %s s adrese %s", tko, sto, odakle)
	if err := h.peers.ZabiljeziDogadjajKljuca(r.Context(), tko, odakle, sto); err != nil {
		log.Printf("razmjena: događaj s ključem mreže nije upisan u knjigu: %v", err)
	}
}

// mjesnoVrijeme je kad u mjesnom vremenu, a nula kad ga nema
func mjesnoVrijeme(kad *time.Time) time.Time {
	if kad == nil {
		return time.Time{}
	}
	return kad.Local()
}
