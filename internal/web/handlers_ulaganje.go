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

	"gocop/internal/models"
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
			zad.Zavrsi(fmt.Sprintf("%d %s; %s je ponovno izgrađena. "+
				"Ništa još nije obrisano iz operative.", iz.Provjereno,
				models.UzBrojHR(iz.Provjereno, "vrijednost je uložena", "vrijednosti su uložene", "vrijednosti je uloženo"), letva), ishod)
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
			h.nakonPospremanja(iz.Verzija)
			zad.Zavrsi(fmt.Sprintf("Obrisano za %s: %d %s i %d %s; "+
				"prostor se vraća nakon VACUUM na Održavanju baze.",
				iz.Letva, iz.Obrisano, models.UzBrojHR(iz.Obrisano, "očitanje", "očitanja", "očitanja"),
				iz.Verzija, models.UzBrojHR(iz.Verzija, "verzija", "verzije", "verzija")), nil)
			return nil
		})
	d.PosaoID, d.PosaoNaziv = posao.ID, posao.Naziv
	h.pisi(w, d)
}

// discard je io.Writer koji ne zna primiti korak — probno izdavanje usred posla
// ne smije prepisati korak koji posao već javlja.
type discard struct{}

func (discard) Write(b []byte) (int, error) { return len(b), nil }

// MakniSirotana briše niz koji je ostao u arhivi iako mu datoteke u stablu
// više nema.
//
// Gradnja to ne radi sama: izvor sa svojom mapom na vanjskom disku izgleda isto
// tako kad disk nije priključen, pa bi tiho brisanje odnijelo godine podataka.
// Ovdje čovjek vidi o kojem je nizu riječ, koliko nosi vrijednosti i koliko ih
// je ušlo u spojeni niz — pa odlučuje.
func (h *UvozHandler) MakniSirotana(w http.ResponseWriter, r *http.Request) {
	d := h.pageData(r)
	if !h.smije(d) {
		http.Error(w, "Arhivu uređuje administrator", http.StatusForbidden)
		return
	}
	if h.makniNiz == nil {
		d.ErrorMessage = "Ovaj čvor ne može uređivati arhivu."
		h.pisi(w, d)
		return
	}
	letva := strings.TrimSpace(r.FormValue("letva"))
	izvor := strings.TrimSpace(r.FormValue("izvor"))
	velicina := strings.TrimSpace(r.FormValue("velicina"))
	vrsta := strings.TrimSpace(r.FormValue("vrsta"))
	if letva == "" || izvor == "" || velicina == "" || vrsta == "" {
		d.ErrorMessage = "Nije zadano koji niz maknuti."
		h.pisi(w, d)
		return
	}
	n, err := h.makniNiz(letva, izvor, velicina, vrsta)
	if err != nil {
		d.ErrorMessage = "niz se nije dao maknuti: " + err.Error()
		h.pisi(w, d)
		return
	}
	redirectWith(w, r, "/administracija/uvoz-niza", "success",
		fmt.Sprintf("Maknut niz %s · %s · %s · %s (%d %s). U stablu ga ionako nema, "+
			"pa se arhiva i dalje da izgraditi iznova.", letva, izvor, velicina, vrsta, n, models.UzBrojHR(n, "vrijednost", "vrijednosti", "vrijednosti")))
}

// MakniSnimkuBezDatoteke briše snimku korita koja je ostala u arhivi iako joj
// datoteke u stablu više nema. Kao i niz, ni snimku gradnja ne miče sama
// (Odluke (13. 10. 2026.)), nego čovjek, s istog popisa.
func (h *UvozHandler) MakniSnimkuBezDatoteke(w http.ResponseWriter, r *http.Request) {
	d := h.pageData(r)
	if !h.smije(d) {
		http.Error(w, "Arhivu uređuje administrator", http.StatusForbidden)
		return
	}
	if h.makniSnimku == nil {
		d.ErrorMessage = "Ovaj čvor ne može uređivati arhivu."
		h.pisi(w, d)
		return
	}
	letva := strings.TrimSpace(r.FormValue("letva"))
	datum := strings.TrimSpace(r.FormValue("datum"))
	if letva == "" {
		d.ErrorMessage = "Nije zadano koju snimku korita maknuti."
		h.pisi(w, d)
		return
	}
	n, err := h.makniSnimku(letva, datum)
	if err != nil {
		d.ErrorMessage = "Snimka korita nije maknuta: " + err.Error()
		h.pisi(w, d)
		return
	}
	redirectWith(w, r, "/administracija/uvoz-niza", "success",
		fmt.Sprintf("Snimka korita %s od %s maknuta je iz arhive (%d %s). U stablu je ionako nema, "+
			"pa se arhiva i dalje da izgraditi iznova.", letva, datum, n, models.UzBrojHR(n, "točka", "točke", "točaka")))
}

// MakniKrivuljeBezDatoteke briše krivulje protoka letve kojoj je iz stabla
// nestala datoteka krivulja. Kao ni snimke, ni krivulje gradnja ne miče sama
// (Odluke (13. 10. 2026.)), nego čovjek, s istog popisa.
func (h *UvozHandler) MakniKrivuljeBezDatoteke(w http.ResponseWriter, r *http.Request) {
	d := h.pageData(r)
	if !h.smije(d) {
		http.Error(w, "Arhivu uređuje administrator", http.StatusForbidden)
		return
	}
	if h.makniKrivulje == nil {
		d.ErrorMessage = "Ovaj čvor ne može uređivati arhivu."
		h.pisi(w, d)
		return
	}
	letva := strings.TrimSpace(r.FormValue("letva"))
	if letva == "" {
		d.ErrorMessage = "Nije zadano kojoj letvi maknuti krivulje protoka."
		h.pisi(w, d)
		return
	}
	n, err := h.makniKrivulje(letva)
	if err != nil {
		d.ErrorMessage = "Krivulje protoka nisu maknute: " + err.Error()
		h.pisi(w, d)
		return
	}
	redirectWith(w, r, "/administracija/uvoz-niza", "success", porukaMaknutihKrivulja(letva, n))
}

// porukaMaknutihKrivulja je poruka o uspjehu micanja krivulja, s glagolom i
// riječju u obliku koji traži broj.
func porukaMaknutihKrivulja(letva string, n int) string {
	return fmt.Sprintf("Iz arhive %s %d %s protoka letve %s. Datoteke krivulja u stablu ionako nema, "+
		"pa se arhiva i dalje da izgraditi iznova.", models.UzBrojHR(n, "je maknuta", "su maknute", "je maknuto"),
		n, models.UzBrojHR(n, "krivulja", "krivulje", "krivulja"), letva)
}
