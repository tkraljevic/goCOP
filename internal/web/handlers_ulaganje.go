package web

// Druga vrata u istu prostoriju: očitanja iz programa ulaze u arhivu.
//
// Isti posao kao datoteka s diska — nešto uđe u stablo, letva se izgradi
// iznova, ponudi se novo izdanje — samo je izvor drugi. Zato stoji na istoj
// stranici: dva mjesta koja oba pišu u stablo i grade letvu s vremenom bi se
// razišla, a razlika bi se vidjela tek kad dvije letve dobiju različit broj za
// isti sadržaj.
//
// Zaboravljanje je odvojeno i namjerno tromije: to je jedino mjesto u programu
// koje briše izmjerenu vrijednost.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"gocop/internal/poslovi"
	"gocop/internal/ulaganje"
)

// zahtjevIzObrasca slaže ono što ulaganje treba iz onoga što je čovjek zadao.
func (h *UvozHandler) zahtjevIzObrasca(r *http.Request) (ulaganje.Zahtjev, string, string, error) {
	letva := strings.TrimSpace(r.FormValue("ul_letva"))
	odS := strings.TrimSpace(r.FormValue("ul_od"))
	doS := strings.TrimSpace(r.FormValue("ul_do"))
	if letva == "" || odS == "" || doS == "" {
		return ulaganje.Zahtjev{}, odS, doS, fmt.Errorf("zadajte letvu i razdoblje")
	}
	od, err := time.ParseInLocation("2006-01-02", odS, time.UTC)
	if err != nil {
		return ulaganje.Zahtjev{}, odS, doS, fmt.Errorf("datum „od“ se ne čita: %w", err)
	}
	do, err := time.ParseInLocation("2006-01-02", doS, time.UTC)
	if err != nil {
		return ulaganje.Zahtjev{}, odS, doS, fmt.Errorf("datum „do“ se ne čita: %w", err)
	}
	if do.Before(od) {
		return ulaganje.Zahtjev{}, odS, doS, fmt.Errorf("„do“ je prije „od“")
	}
	// Zadnji dan ulazi cijeli.
	do = do.AddDate(0, 0, 1).Add(-time.Second)

	cvor := ""
	if h.cvor != nil {
		cvor = h.cvor()
	}
	return ulaganje.Zahtjev{
		Baza: h.baza(), ArhivaPut: h.arhivaPut(), Koren: h.podaciDir(), Cvor: cvor,
		Letva: letva, Od: od, Do: do,
	}, odS, doS, nil
}

// PregledUlaganja pokazuje što bi iz programa ušlo u arhivu. Ništa se ne mijenja.
func (h *UvozHandler) PregledUlaganja(w http.ResponseWriter, r *http.Request) {
	d := h.pageData(r)
	if !h.smije(d) {
		http.Error(w, "Očitanja u arhivu ulaže administrator", http.StatusForbidden)
		return
	}
	if !h.ulaganjeRadi() {
		d.ErrorMessage = "Ovaj čvor ne može ulagati — nema stablo s datotekama ili ne zna gdje arhiva stoji."
		h.pisi(w, d)
		return
	}
	z, odS, doS, err := h.zahtjevIzObrasca(r)
	d.UlOd, d.UlDo, d.UlLetva = odS, doS, strings.TrimSpace(r.FormValue("ul_letva"))
	if err != nil {
		d.ErrorMessage = err.Error()
		h.pisi(w, d)
		return
	}
	p, err := ulaganje.Pripremi(r.Context(), z)
	if err != nil {
		d.ErrorMessage = err.Error()
		h.pisi(w, d)
		return
	}
	d.Ulaganje = p
	if p.Ukupno == 0 {
		d.SuccessMessage = "U tom razdoblju nema očitanja koja još nisu uložena."
	}
	h.pisi(w, d)
}

// UloziOcitanja zapisuje očitanja u stablo, gradi letvu i provjerava da je sve
// stiglo. Ništa se ne briše — to je zaseban korak.
func (h *UvozHandler) UloziOcitanja(w http.ResponseWriter, r *http.Request) {
	d := h.pageData(r)
	if !h.smije(d) {
		http.Error(w, "Očitanja u arhivu ulaže administrator", http.StatusForbidden)
		return
	}
	if !h.ulaganjeRadi() {
		d.ErrorMessage = "Ovaj čvor ne može ulagati — nema stablo s datotekama ili ne zna gdje arhiva stoji."
		h.pisi(w, d)
		return
	}
	z, odS, doS, err := h.zahtjevIzObrasca(r)
	d.UlOd, d.UlDo, d.UlLetva = odS, doS, strings.TrimSpace(r.FormValue("ul_letva"))
	if err != nil {
		d.ErrorMessage = err.Error()
		h.pisi(w, d)
		return
	}
	// Pripremanje ide prije posla, da se greška u zadanom vidi odmah, a ne kroz
	// posao koji odmah padne.
	p, err := ulaganje.Pripremi(r.Context(), z)
	if err != nil {
		d.ErrorMessage = err.Error()
		h.pisi(w, d)
		return
	}
	if !p.Ima() {
		d.Ulaganje = p
		d.ErrorMessage = "U tom razdoblju nema nijednog očitanja koje bi ušlo u arhivu."
		h.pisi(w, d)
		return
	}

	korisnik := ""
	if d.CurrentUser != nil {
		korisnik = d.CurrentUser.ID.String()
	}
	letva := p.Postaja.Code
	posao := h.poslovi.Pokreni("Ulaganje očitanja — "+p.Postaja.Name, korisnik,
		"/administracija/uvoz-niza?posao={id}", func(zad *poslovi.Posao) error {
			// Kontekst zahtjeva umire kad stranica dobije odgovor, a posao se
			// nastavlja — zato vlastiti.
			ctx := context.Background()
			iz, err := p.Ulozi(ctx, z, zad)
			if err != nil {
				return err
			}
			ishod := &ishodUvoza{Put: strings.Join(iz.Putovi, ", "), Letva: letva}
			if h.izdavanjeRadi() {
				zad.Korak("gledam bi li se izdanje promijenilo", 0, 0)
				if pr, err := h.izdaj(letva, true, discard{}); err == nil {
					ishod.Izdanja = &pr
				}
			}
			zad.Zavrsi(fmt.Sprintf("Uloženo %d vrijednosti; %s je ponovno izgrađena. "+
				"Ništa još nije obrisano iz operative.", iz.Provjereno, letva), ishod)
			return nil
		})
	d.PosaoID, d.PosaoNaziv = posao.ID, posao.Naziv
	h.pisi(w, d)
}

// Pospremi briše uložena očitanja jedne letve — nakon što ponovno provjeri da
// je svaka vrijednost u arhivi.
func (h *UvozHandler) Pospremi(w http.ResponseWriter, r *http.Request) {
	d := h.pageData(r)
	if !h.smije(d) {
		http.Error(w, "Operativu posprema administrator", http.StatusForbidden)
		return
	}
	if !h.ulaganjeRadi() {
		d.ErrorMessage = "Ovaj čvor ne može pospremati — ne zna gdje arhiva stoji."
		h.pisi(w, d)
		return
	}
	stationID := strings.TrimSpace(r.FormValue("postaja"))
	if stationID == "" {
		d.ErrorMessage = "Nije zadano koju letvu pospremiti."
		h.pisi(w, d)
		return
	}
	korisnik := ""
	if d.CurrentUser != nil {
		korisnik = d.CurrentUser.ID.String()
	}
	baza, arhivaPut := h.baza(), h.arhivaPut()
	posao := h.poslovi.Pokreni("Pospremanje uloženih očitanja", korisnik,
		"/administracija/uvoz-niza?posao={id}", func(zad *poslovi.Posao) error {
			iz, err := ulaganje.Zaboravi(context.Background(), baza, arhivaPut, stationID, zad)
			if err != nil {
				return err
			}
			zad.Zavrsi(fmt.Sprintf("Obrisano %d očitanja i %d verzija za %s; "+
				"prostor se vraća nakon VACUUM na Održavanju baze.",
				iz.Obrisano, iz.Verzija, iz.Letva), nil)
			return nil
		})
	d.PosaoID, d.PosaoNaziv = posao.ID, posao.Naziv
	h.pisi(w, d)
}

// discard je io.Writer koji ne zna primiti korak — probno izdavanje usred posla
// ne smije prepisati korak koji posao već javlja.
type discard struct{}

func (discard) Write(b []byte) (int, error) { return len(b), nil }
