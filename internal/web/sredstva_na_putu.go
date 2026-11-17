package web

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// PosiljkaNaPutu je roba na putu kako je vidi osoba: smije li je zaprimiti
// i radi li to kao dežurni COP-a umjesto odredišta (Odluke 16. 11. 2026.,
// Na putu)
type PosiljkaNaPutu struct {
	models.Posiljka
	SmijeZaprimiti bool
	Umjesto        bool
}

// Poslano je količina koja je otišla s izvora
func (p PosiljkaNaPutu) Poslano() float64 { return -p.Slanje.Kolicina }

// Odakle je ime izvora pošiljke: skladište ili mjesto na terenu
func (p PosiljkaNaPutu) Odakle() string { return p.Slanje.MjestoNaziv() }

// Kamo je ime odredišta pošiljke: skladište ili mjesto na terenu
func (p PosiljkaNaPutu) Kamo() string {
	if p.Slanje.PremaSkladisteID != "" {
		return p.PremaSkladisteNaziv
	}
	m := p.Slanje.PremaTerenu()
	return models.Promet{AreaID: m.AreaID, AreaName: p.PremaAreaName, SectionCode: m.SectionCode, Mjesto: m.Mjesto}.MjestoNaziv()
}

// Povratak je stranica odredišta, kamo se ide poslije zaprimanja
func (p PosiljkaNaPutu) Povratak() string {
	if p.Slanje.PremaSkladisteID != "" {
		return "/sredstva/skladista/" + p.Slanje.PremaSkladisteID
	}
	return "/sredstva/na-terenu?sektor=" + p.PremaSektor
}

// uzPravo dodaje pošiljkama pravo zaprimanja osobe
func (h *MtsHandler) uzPravo(r *http.Request, perms *models.UserPermissions, sve []models.Posiljka) ([]PosiljkaNaPutu, error) {
	out := make([]PosiljkaNaPutu, 0, len(sve))
	for _, p := range sve {
		smije, umjesto, err := h.svc().SmijeZaprimiti(r.Context(), perms, p)
		if err != nil {
			return nil, err
		}
		out = append(out, PosiljkaNaPutu{Posiljka: p, SmijeZaprimiti: smije, Umjesto: umjesto})
	}
	return out, nil
}

// naPutuSkladista puni kartice „Stiže” i „Poslano, na putu” skladišta
func (h *MtsHandler) naPutuSkladista(r *http.Request, data *MtsPageData, skladisteID string) error {
	stize, gStize := h.svc().NaPutu(r.Context(), repository.FiltarPosiljki{PremaSkladisteID: skladisteID})
	poslano, gPoslano := h.svc().NaPutu(r.Context(), repository.FiltarPosiljki{SkladisteID: skladisteID})
	if err := errors.Join(gStize, gPoslano); err != nil {
		return err
	}
	var err error
	if data.Stize, err = h.uzPravo(r, data.Permissions, stize); err != nil {
		return err
	}
	data.Poslano, err = h.uzPravo(r, data.Permissions, poslano)
	return err
}

// ucitajPosiljku puni stranicu pošiljkom iz adrese; javlja je li stranica
// već odgovorila (pošiljke nema, ili je osoba ne smije zaprimiti)
func (h *MtsHandler) ucitajPosiljku(w http.ResponseWriter, r *http.Request, data *MtsPageData) bool {
	sve, err := h.svc().NaPutu(r.Context(), repository.FiltarPosiljki{VezaID: r.PathValue("veza")})
	if err == nil && len(sve) == 0 {
		redirectWith(w, r, "/sredstva", "error", "Te pošiljke više nema na putu: zaprimljena je ili otpisana.")
		return false
	}
	var p []PosiljkaNaPutu
	if err == nil {
		p, err = h.uzPravo(r, data.Permissions, sve[:1])
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	if !p[0].SmijeZaprimiti {
		http.Error(w, "Pošiljku zaprima odredište: u skladište skladištar ili uprava, na terenu rukovoditelj dionice ili vodočuvar. Dežurni COP-a smije umjesto njih.", http.StatusForbidden)
		return false
	}
	data.Posiljka, data.Sektor = &p[0], p[0].PremaSektor
	return true
}

// ShowZaprimi prikazuje obrazac zaprimanja pošiljke (crtež 19a, 7b)
func (h *MtsHandler) ShowZaprimi(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if !h.ucitajPosiljku(w, r, &data) {
		return
	}
	h.render(w, "zaprimi.html", data)
}

// HandleZaprimi upisuje što je stiglo; razlika ostaje na putu
func (h *MtsHandler) HandleZaprimi(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if !h.ucitajPosiljku(w, r, &data) {
		return
	}
	u := noviUpisaniBrojevi(r)
	z := service.Zaprimanje{VezaID: data.Posiljka.Slanje.VezaID, Preuzeo: u.tekst("preuzeo"), Dokument: u.tekst("dokument"), Napomena: u.tekst("napomena")}
	if t, err := time.ParseInLocation("2006-01-02", u.tekst("datum"), models.Zagreb); err == nil {
		z.Datum = t
	}
	k, ok := parseBroj(u.tekst("kolicina"))
	if !ok {
		u.Neispravno["kolicina"] = true
		data.vratiUpisano(u, "Količina nije broj; upišite je brojkama, npr. 480.")
		h.render(w, "zaprimi.html", data)
		return
	}
	z.Kolicina = k
	red, err := h.svc().Zaprimi(r.Context(), data.CurrentUser, data.Permissions, z)
	if err != nil {
		data.vratiUpisano(u, err.Error())
		h.render(w, "zaprimi.html", data)
		return
	}
	poruka := "Zaprimljeno: " + kolicinaHR(red.Kolicina) + " " + data.Posiljka.Slanje.Jedinica + " " + data.Posiljka.Slanje.VrstaNaziv + "."
	if ostalo := data.Posiljka.NaPutu() - red.Kolicina; ostalo > 1e-9 {
		poruka += " Na putu je ostalo " + kolicinaHR(ostalo) + "."
	}
	redirectWith(w, r, data.Posiljka.Povratak(), "success", poruka)
}

// HandleManjak otpisuje ostatak pošiljke koji nije stigao, uz razlog
func (h *MtsHandler) HandleManjak(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if !h.ucitajPosiljku(w, r, &data) {
		return
	}
	razlog := strings.TrimSpace(r.FormValue("razlog"))
	if _, err := h.svc().OtpisiManjak(r.Context(), data.CurrentUser, data.Permissions, data.Posiljka.Slanje.VezaID, razlog); err != nil {
		u := noviUpisaniBrojevi(r)
		u.tekst("razlog")
		data.vratiUpisano(u, err.Error())
		h.render(w, "zaprimi.html", data)
		return
	}
	redirectWith(w, r, data.Posiljka.Povratak(), "success", "Manjak pri prijevozu je otpisan: "+kolicinaHR(data.Posiljka.NaPutu())+" "+data.Posiljka.Slanje.Jedinica+" "+data.Posiljka.Slanje.VrstaNaziv+".")
}
