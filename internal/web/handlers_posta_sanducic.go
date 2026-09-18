package web

import (
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/service"
)

// Sandučić: korisnikova ulazna pošta iz Exchangea, čitanje pisama i
// privitaka, i učitavanje potpisanog PDF-a iz SIGNATOR-a ravno u nacrt akta.

const pisamaPoStranici = 50

// SetSanducic daje rukovatelju predloške sandučića
func (h *AktiHandler) SetSanducic(popis, pismo, novo *template.Template) {
	h.tmplSanducic, h.tmplPismo, h.tmplNovoPismo = popis, pismo, novo
}

// SanducicData je stranica popisa pisama ili jednog pisma
type SanducicData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	ActiveNav   string
	ViewAsBanner
	SuccessMessage string
	ErrorMessage   string

	Pisma           []posta.Pismo
	Mapa            string
	Mape            []posta.Mapa
	Trazi           string
	NazivMape       string
	Ukupno          int
	Stranica        int
	Stranica_       int // sljedeća, 0 kad je nema
	Prethodna       int
	Pismo           *posta.Pismo
	Nacrti          []models.Akt // nacrti koji čekaju potpisani PDF
	OdabraniAkt     string
	Novo            service.NovoPismo // obrazac novog pisma
	Nacin           string            // odgovori, svima, proslijedi ili prazno
	Izvorno         *posta.Pismo      // pismo na koje se odgovara
	TrebaLozinku    bool
	NijeUkljuceno   bool
	LozinkaOdbijena bool
}

func (h *AktiHandler) sanducicData(r *http.Request) (*service.AktService, SanducicData) {
	u, perms, _ := h.base(r)
	q := r.URL.Query()
	return h.akti(), SanducicData{CurrentUser: u, Permissions: perms, ActiveNav: "posta", ViewAsBanner: viewBanner(r),
		SuccessMessage: q.Get("success"), ErrorMessage: q.Get("error"), OdabraniAkt: q.Get("akt")}
}

// greskaSanducica pretvori grešku u stanje stranice
func (d *SanducicData) greska(err error) {
	switch {
	case errors.Is(err, service.ErrNemaLozinkePoste):
		d.TrebaLozinku = true
	case errors.Is(err, posta.ErrPrijava):
		d.LozinkaOdbijena = true
	case strings.Contains(err.Error(), "nije uključena"):
		d.NijeUkljuceno = true
	default:
		d.ErrorMessage = err.Error()
	}
}

// ShowSanducic prikazuje ulaznu poštu
func (h *AktiHandler) ShowSanducic(w http.ResponseWriter, r *http.Request) {
	s, d := h.sanducicData(r)
	if s == nil || d.CurrentUser == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	q := r.URL.Query()
	d.Stranica, _ = strconv.Atoi(q.Get("stranica"))
	if d.Stranica < 1 {
		d.Stranica = 1
	}
	d.Mapa, d.Trazi = q.Get("mapa"), strings.TrimSpace(q.Get("trazi"))
	if d.Mapa == "" {
		d.Mapa = "inbox"
	}
	pisma, ukupno, err := s.Sanducic(r.Context(), d.CurrentUser, d.Mapa, d.Trazi, d.Stranica, pisamaPoStranici)
	if err != nil {
		d.greska(err)
	} else {
		if d.Mape, err = s.MapeSanducica(r.Context(), d.CurrentUser); err != nil {
			d.Mape = posta.Mape
		}
	}
	d.NazivMape = d.Mapa
	for _, m := range d.Mape {
		if m.ID == d.Mapa {
			d.NazivMape = m.Naziv
		}
	}
	d.Pisma, d.Ukupno = pisma, ukupno
	if d.Stranica*pisamaPoStranici < ukupno {
		d.Stranica_ = d.Stranica + 1
	}
	d.Prethodna = d.Stranica - 1
	if err := h.tmplSanducic.ExecuteTemplate(w, "posta_sanducic.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ShowPismo prikazuje jedno pismo
func (h *AktiHandler) ShowPismo(w http.ResponseWriter, r *http.Request) {
	s, d := h.sanducicData(r)
	if s == nil || d.CurrentUser == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	pismo, err := s.Pismo(r.Context(), d.CurrentUser, r.URL.Query().Get("id"))
	if err != nil {
		d.greska(err)
	}
	d.Pismo = pismo
	d.Mapa = r.URL.Query().Get("mapa")
	if pismo != nil {
		if !pismo.Procitano {
			// otvoreno pismo je pročitano, kao u Outlooku
			if s.OznaciProcitano(r.Context(), d.CurrentUser, pismo.ID, pismo.ChangeKey, true) == nil {
				pismo.Procitano = true
			}
		}
		for _, p := range pismo.Privitci {
			if p.JePDF() {
				d.Nacrti = s.NacrtiZaPotpis(r.Context(), d.Permissions, d.CurrentUser)
				break
			}
		}
	}
	if err := h.tmplPismo.ExecuteTemplate(w, "posta_pismo.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Privitak daje datoteku privitka
func (h *AktiHandler) Privitak(w http.ResponseWriter, r *http.Request) {
	u, _, _ := h.base(r)
	s := h.svc(w)
	if s == nil || u == nil {
		return
	}
	p, podaci, err := s.Privitak(r.Context(), u, r.URL.Query().Get("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	vrsta := p.Vrsta
	if vrsta == "" {
		vrsta = "application/octet-stream"
	}
	w.Header().Set("Content-Type", vrsta)
	w.Header().Set("Content-Disposition", `attachment; filename="`+sigurnoIme(strings.TrimSuffix(p.Ime, ".pdf"))+pathExt(p.Ime)+`"`)
	_, _ = w.Write(podaci)
}

func pathExt(ime string) string {
	if i := strings.LastIndex(ime, "."); i >= 0 {
		return strings.ToLower(ime[i:])
	}
	return ""
}

// HandlePotpisaniIzPoste učita PDF privitak iz sandučića kao potpisani akt
func (h *AktiHandler) HandlePotpisaniIzPoste(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s := h.svc(w)
	if s == nil || u == nil {
		return
	}
	pismoID, privitakID, aktID := r.FormValue("pismo"), r.FormValue("privitak"), r.FormValue("akt")
	natrag := "/posta/pismo?" + url.Values{"id": {pismoID}}.Encode()
	if aktID == "" {
		redirectWith(w, r, natrag, "error", "Odaberite nacrt akta u koji se potpisani PDF učitava")
		return
	}
	_, pdf, err := s.Privitak(r.Context(), u, privitakID)
	if err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	a, upozorenja, err := s.UcitajPotpisani(r.Context(), perms, u, aktID, pdf)
	if err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	poruka := "Akt " + a.Oznaka() + " je ovjeren kvalificiranim potpisom (" + a.Kvalificirani.Ime + ") iz sandučića; potpisani PDF je izvornik."
	if len(upozorenja) > 0 {
		poruka += " Stanje obrane na dionicama: " + strings.Join(upozorenja, "; ")
	}
	redirectWith(w, r, "/akti/"+a.ID, "success", poruka)
}

// HandlePismoRadnja: označi nepročitano ili obriši
func (h *AktiHandler) HandlePismoRadnja(w http.ResponseWriter, r *http.Request) {
	u, _, _ := h.base(r)
	s := h.svc(w)
	if s == nil || u == nil {
		return
	}
	id, ck := r.FormValue("id"), r.FormValue("ck")
	natrag := "/posta/pismo?" + url.Values{"id": {id}}.Encode()
	switch r.FormValue("radnja") {
	case "neprocitano":
		if err := s.OznaciProcitano(r.Context(), u, id, ck, false); err != nil {
			redirectWith(w, r, natrag, "error", err.Error())
			return
		}
		redirectWith(w, r, "/posta", "success", "Pismo je označeno kao nepročitano.")
	case "obrisi":
		if r.FormValue("mapa") == "deleteditems" {
			if err := s.ObrisiTrajno(r.Context(), u, []string{id}); err != nil {
				redirectWith(w, r, natrag, "error", err.Error())
				return
			}
			redirectWith(w, r, "/posta?mapa=deleteditems", "success", "Pismo je trajno obrisano. Exchange ga još neko vrijeme čuva u oporavljivim stavkama.")
			return
		}
		if err := s.ObrisiPismo(r.Context(), u, id); err != nil {
			redirectWith(w, r, natrag, "error", err.Error())
			return
		}
		redirectWith(w, r, "/posta", "success", "Pismo je premješteno u Obrisano.")
	default:
		redirectWith(w, r, natrag, "error", "Nepoznata radnja")
	}
}

// ShowNovoPismo prikazuje obrazac novog pisma, odgovora ili prosljeđivanja
func (h *AktiHandler) ShowNovoPismo(w http.ResponseWriter, r *http.Request) {
	s, d := h.sanducicData(r)
	if s == nil || d.CurrentUser == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	q := r.URL.Query()
	d.Nacin = q.Get("nacin")
	d.Novo = service.NovoPismo{Za: q.Get("za"), Predmet: q.Get("predmet"), Tekst: q.Get("tekst")}
	if id := q.Get("id"); id != "" {
		izv, err := s.Pismo(r.Context(), d.CurrentUser, id)
		if err != nil {
			d.greska(err)
		} else {
			d.Izvorno = izv
			d.Novo = pripremiOdgovor(izv, d.Nacin, d.CurrentUser)
		}
	}
	if err := h.tmplNovoPismo.ExecuteTemplate(w, "posta_novo.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// pripremiOdgovor puni obrazac za odgovor, odgovor svima ili prosljeđivanje
func pripremiOdgovor(izv *posta.Pismo, nacin string, u *models.User) service.NovoPismo {
	n := service.NovoPismo{OdgovorNa: izv.MessageID}
	navod := "\n\n" + strings.Repeat("-", 40) + "\nOd: " + izv.Od
	if izv.OdAdresa != "" && izv.OdAdresa != izv.Od {
		navod += " <" + izv.OdAdresa + ">"
	}
	navod += "\nPoslano: " + izv.Kad.In(models.Zagreb).Format("02.01.2006. 15:04") + "\nPredmet: " + izv.Predmet + "\n\n" + izv.Tekst + "\n"
	predmet := izv.Predmet
	switch nacin {
	case "proslijedi":
		if !strings.HasPrefix(strings.ToUpper(predmet), "FW:") {
			predmet = "FW: " + predmet
		}
		n.Predmet, n.Tekst = predmet, navod
		for _, p := range izv.Privitci {
			n.Proslijedi = append(n.Proslijedi, p.ID)
		}
	default:
		if !strings.HasPrefix(strings.ToUpper(predmet), "RE:") {
			predmet = "RE: " + predmet
		}
		n.Predmet, n.Tekst, n.Za = predmet, navod, izv.OdAdresa
		if nacin == "svima" {
			var kopija []string
			for _, a := range append(append([]string{}, izv.Za...), izv.Kopija...) {
				adr := posta.Adrese(a)
				if len(adr) == 1 && !strings.EqualFold(adr[0], u.Email) && !strings.EqualFold(adr[0], izv.OdAdresa) {
					kopija = append(kopija, adr[0])
				}
			}
			n.Kopija = strings.Join(kopija, ", ")
		}
	}
	return n
}

// HandlePosaljiPismo šalje pismo iz obrasca
func (h *AktiHandler) HandlePosaljiPismo(w http.ResponseWriter, r *http.Request) {
	u, _, _ := h.base(r)
	s := h.svc(w)
	if s == nil || u == nil {
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		redirectWith(w, r, "/posta/novo", "error", "Neispravan obrazac ili prevelik privitak (najviše 32 MB)")
		return
	}
	n := service.NovoPismo{Za: r.FormValue("za"), Kopija: r.FormValue("kopija"), Predmet: r.FormValue("predmet"), Tekst: r.FormValue("tekst"),
		OdgovorNa: r.FormValue("odgovor_na"), Proslijedi: r.Form["proslijedi"]}
	if r.MultipartForm != nil {
		for _, fh := range r.MultipartForm.File["privitak"] {
			f, err := fh.Open()
			if err != nil {
				continue
			}
			podaci, _ := io.ReadAll(io.LimitReader(f, 32<<20))
			f.Close()
			if len(podaci) > 0 {
				n.Privitci = append(n.Privitci, posta.Privitak{Ime: fh.Filename, Vrsta: fh.Header.Get("Content-Type"), Podaci: podaci})
			}
		}
	}
	if err := s.PosaljiPismo(r.Context(), u, n); err != nil {
		natrag := "/posta/novo?" + url.Values{"za": {n.Za}, "predmet": {n.Predmet}, "tekst": {n.Tekst}}.Encode()
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	redirectWith(w, r, "/posta?mapa=sentitems", "success", "Pismo je poslano.")
}

// HandleSkupnaRadnja izvodi radnju nad označenim pismima: obriši, arhiviraj,
// premjesti, pročitano, nepročitano
func (h *AktiHandler) HandleSkupnaRadnja(w http.ResponseWriter, r *http.Request) {
	u, _, _ := h.base(r)
	s := h.svc(w)
	if s == nil || u == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/posta", "error", "Neispravan obrazac")
		return
	}
	natrag := "/posta?" + url.Values{"mapa": {r.FormValue("mapa")}, "trazi": {r.FormValue("trazi")}}.Encode()
	var ids []string
	var stavke []posta.Stavka
	for _, v := range r.Form["p"] {
		dio := strings.SplitN(v, "|", 2)
		if len(dio) != 2 || dio[0] == "" {
			continue
		}
		ids = append(ids, dio[0])
		stavke = append(stavke, posta.Stavka{ID: dio[0], ChangeKey: dio[1]})
	}
	if len(ids) == 0 {
		redirectWith(w, r, natrag, "error", "Označite barem jedno pismo")
		return
	}
	n := strconv.Itoa(len(ids)) + " " + pismoRijec(len(ids))
	var err error
	var poruka string
	switch r.FormValue("radnja") {
	case "obrisi":
		if r.FormValue("mapa") == "deleteditems" {
			// iz Obrisanog se briše trajno, kao u Outlooku
			err, poruka = s.ObrisiTrajno(r.Context(), u, ids), n+" trajno obrisano. Exchange ih još neko vrijeme čuva u oporavljivim stavkama."
		} else {
			err, poruka = s.PremjestiPisma(r.Context(), u, ids, "deleteditems"), n+" premješteno u Obrisano."
		}
	case "arhiviraj":
		err, poruka = s.PremjestiPisma(r.Context(), u, ids, "archive"), n+" premješteno u Arhivu."
	case "premjesti":
		mapa := r.FormValue("mapa_u")
		if mapa == "" {
			redirectWith(w, r, natrag, "error", "Odaberite mapu u koju se pisma premještaju")
			return
		}
		err, poruka = s.PremjestiPisma(r.Context(), u, ids, mapa), n+" premješteno."
	case "procitano":
		err, poruka = s.OznaciProcitanoVise(r.Context(), u, stavke, true), n+" označeno kao pročitano."
	case "neprocitano":
		err, poruka = s.OznaciProcitanoVise(r.Context(), u, stavke, false), n+" označeno kao nepročitano."
	default:
		redirectWith(w, r, natrag, "error", "Nepoznata radnja")
		return
	}
	if err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	redirectWith(w, r, natrag, "success", poruka)
}

func pismoRijec(n int) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return "pismo"
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		return "pisma"
	}
	return "pisama"
}
