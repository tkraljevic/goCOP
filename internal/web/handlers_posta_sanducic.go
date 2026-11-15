package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/service"
)

// Sandučić: korisnikova ulazna pošta iz Exchangea, čitanje pisama i
// privitaka, pisanje i odgovaranje.

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

	Pisma     []posta.Pismo
	Mapa      string
	Mape      []posta.Mapa
	Trazi     string
	NazivMape string
	Ukupno    int
	Stranica  int
	Stranica_ int // sljedeća, 0 kad je nema
	Prethodna int
	Pismo     *posta.Pismo
	Novo      service.NovoPismo // obrazac novog pisma
	Nacin     string            // odgovori, svima, proslijedi ili prazno
	Izvorno   *posta.Pismo      // pismo na koje se odgovara
	Potpis    string            // korisnikov potpis e-pošte, za novo pismo
	// PrivitciPonovo su privitci pisma koje nije poslano: datoteke se ne
	// mogu vratiti u obrazac, pa se samo imenuju da se odaberu ponovno
	PrivitciPonovo  []string
	TrebaLozinku    bool
	NijeUkljuceno   bool
	LozinkaOdbijena bool
}

func (h *AktiHandler) sanducicData(r *http.Request) (*service.AktService, SanducicData) {
	u, perms, _ := h.base(r)
	q := r.URL.Query()
	return h.akti(), SanducicData{CurrentUser: u, Permissions: perms, ActiveNav: "posta", ViewAsBanner: viewBanner(r),
		SuccessMessage: q.Get("success"), ErrorMessage: q.Get("error")}
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
		mape, err := s.MapeSanducica(r.Context(), d.CurrentUser)
		if err != nil {
			mape = posta.Mape
		}
		d.Mape = bezSkica(mape)
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

// mapeUzPismo puni mape za „Premjesti u…” i „Arhiviraj” na pismu i naziv
// mape u koju se vraća; bez mapa (poslužitelj ne odgovara) nudi se samo
// povratak u Sandučić
func mapeUzPismo(ctx context.Context, s *service.AktService, d *SanducicData) {
	if d.Mapa == "" {
		d.Mapa = "inbox"
	}
	d.NazivMape = "Sandučić"
	if d.Pismo == nil {
		return
	}
	if mape, err := s.MapeSanducica(ctx, d.CurrentUser); err == nil {
		d.Mape = bezSkica(mape)
	}
	for _, m := range d.Mape {
		if m.ID == d.Mapa {
			d.NazivMape = m.Naziv
		}
	}
}

// bezSkica izbacuje mapu Skice: goCOP skicu ne zna ni spremiti ni nastaviti,
// pa je ne nudi (Odluke (15. 11. 2026.), Sandučić 5)
func bezSkica(mape []posta.Mapa) []posta.Mapa {
	out := make([]posta.Mapa, 0, len(mape))
	for _, m := range mape {
		if m.ID != "drafts" {
			out = append(out, m)
		}
	}
	return out
}

// ImaArhivu javlja ima li sandučić mapu Arhiva; bez nje se Arhiviraj ne nudi
func (d SanducicData) ImaArhivu() bool {
	for _, m := range d.Mape {
		if m.ID == "archive" {
			return true
		}
	}
	return false
}

// Raspon kaže koja su pisma na ovoj stranici popisa („1–50”)
func (d SanducicData) Raspon() string {
	if len(d.Pisma) == 0 {
		return ""
	}
	od := (d.Stranica-1)*pisamaPoStranici + 1
	return fmt.Sprintf("%d–%d", od, od+len(d.Pisma)-1)
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
	mapeUzPismo(r.Context(), s, &d)
	if pismo != nil {
		pismo.UgradiSlike(func(id string) string { return "/posta/privitak?" + url.Values{"id": {id}, "u": {"1"}}.Encode() })
		if !pismo.Procitano {
			// otvoreno pismo je pročitano, kao u Outlooku. To je izmjena na
			// GET, pa je može izazvati i tuđa stranica poveznicom; prihvaćeno:
			// zastavica „pročitano” je jedina posljedica, a vlasnik je vraća
			// jednim klikom (Označi nepročitano)
			if s.OznaciProcitano(r.Context(), d.CurrentUser, pismo.ID, pismo.ChangeKey, true) == nil {
				pismo.Procitano = true
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
	// vrstu bira pošiljatelj: u tijelu pisma (u=1) prikazuje se samo obična
	// slika, sve ostalo (i SVG) se preuzima
	ext := pathExt(p.Ime)
	posluziTudjuDatoteku(w, p.Vrsta, sigurnoIme(strings.TrimSuffix(p.Ime, ext))+ext, r.URL.Query().Get("u") == "1")
	_, _ = w.Write(podaci)
}

// pathExt je nastavak imena datoteke, samo slova i brojke, da ime u
// zaglavlju Content-Disposition ostane ime
func pathExt(ime string) string {
	i := strings.LastIndex(ime, ".")
	if i < 0 || len(ime)-i > 11 {
		return ""
	}
	ext := strings.ToLower(ime[i+1:])
	for _, c := range ext {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return ""
		}
	}
	if ext == "" {
		return ""
	}
	return "." + ext
}

// mapaPovratka je popis mape iz koje se pismo otvorilo; bez nje Ulazna pošta
func mapaPovratka(r *http.Request) string {
	if m := r.FormValue("mapa"); m != "" {
		return "/posta?" + url.Values{"mapa": {m}}.Encode()
	}
	return "/posta"
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
		redirectWith(w, r, mapaPovratka(r), "success", "Pismo je označeno kao nepročitano.")
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
		redirectWith(w, r, mapaPovratka(r), "success", "Pismo je premješteno u Obrisano.")
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
	d.Potpis = s.Potpis(r.Context(), d.CurrentUser.ID.String())
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

// vratiNovoPismo pokaže obrazac pisma koje nije poslano, s greškom i svim
// napisanim: primatelji, kopija, predmet, oblikovani tekst, veza s izvornim
// pismom i privitci koji se prosljeđuju. Preusmjeravanje bi izgubilo tekst.
func (h *AktiHandler) vratiNovoPismo(w http.ResponseWriter, r *http.Request, s *service.AktService, n service.NovoPismo, privitci []string, err error) {
	_, d := h.sanducicData(r)
	d.Nacin = r.FormValue("nacin")
	d.Potpis = s.Potpis(r.Context(), d.CurrentUser.ID.String())
	n.Privitci = nil
	d.Novo = n
	d.PrivitciPonovo = privitci
	d.greska(err)
	w.WriteHeader(http.StatusUnprocessableEntity)
	if err := h.tmplNovoPismo.ExecuteTemplate(w, "posta_novo.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// imenaPrivitaka su imena datoteka priloženih pismu
func imenaPrivitaka(privitci []posta.Privitak) []string {
	var imena []string
	for _, p := range privitci {
		imena = append(imena, p.Ime)
	}
	return imena
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
		HTML: r.FormValue("html"), OdgovorNa: r.FormValue("odgovor_na"), Proslijedi: r.Form["proslijedi"]}
	if n.HTML != "" {
		n.Tekst = "" // tekst se izvodi iz oblikovanog tijela
	}
	if r.MultipartForm != nil {
		for _, fh := range r.MultipartForm.File["privitak"] {
			f, err := fh.Open()
			if err != nil {
				continue
			}
			podaci, err := procitajDatoteku(f, 32<<20)
			f.Close()
			if err != nil {
				// odrezan privitak bi otišao primatelju kao da je cijeli
				h.vratiNovoPismo(w, r, s, n, nil, errors.New("Privitak "+fh.Filename+" nije poslan: "+err.Error()))
				return
			}
			if len(podaci) > 0 {
				n.Privitci = append(n.Privitci, posta.Privitak{Ime: fh.Filename, Vrsta: fh.Header.Get("Content-Type"), Podaci: podaci})
			}
		}
	}
	if err := s.PosaljiPismo(r.Context(), u, n); err != nil {
		h.vratiNovoPismo(w, r, s, n, imenaPrivitaka(n.Privitci), err)
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
	// glagol se slaže s brojem: 1 pismo je premješteno, 2 pisma su premještena
	k := len(ids)
	n := strconv.Itoa(k) + " "
	var err error
	var poruka string
	switch r.FormValue("radnja") {
	case "obrisi":
		if r.FormValue("mapa") == "deleteditems" {
			// iz Obrisanog se briše trajno, kao u Outlooku
			err, poruka = s.ObrisiTrajno(r.Context(), u, ids),
				n+models.UzBrojHR(k, "pismo je trajno obrisano.", "pisma su trajno obrisana.", "pisama je trajno obrisano.")+
					" Exchange obrisano još neko vrijeme čuva u oporavljivim stavkama."
		} else {
			err, poruka = s.PremjestiPisma(r.Context(), u, ids, "deleteditems"),
				n+models.UzBrojHR(k, "pismo je premješteno u Obrisano.", "pisma su premještena u Obrisano.", "pisama je premješteno u Obrisano.")
		}
	case "arhiviraj":
		err, poruka = s.PremjestiPisma(r.Context(), u, ids, "archive"),
			n+models.UzBrojHR(k, "pismo je premješteno u Arhivu.", "pisma su premještena u Arhivu.", "pisama je premješteno u Arhivu.")
	case "premjesti":
		mapa := r.FormValue("mapa_u")
		if mapa == "" {
			redirectWith(w, r, natrag, "error", "Odaberite mapu u koju se pisma premještaju")
			return
		}
		err, poruka = s.PremjestiPisma(r.Context(), u, ids, mapa),
			n+models.UzBrojHR(k, "pismo je premješteno.", "pisma su premještena.", "pisama je premješteno.")
	case "procitano":
		err, poruka = s.OznaciProcitanoVise(r.Context(), u, stavke, true),
			n+models.UzBrojHR(k, "pismo je označeno kao pročitano.", "pisma su označena kao pročitana.", "pisama je označeno kao pročitano.")
	case "neprocitano":
		err, poruka = s.OznaciProcitanoVise(r.Context(), u, stavke, false),
			n+models.UzBrojHR(k, "pismo je označeno kao nepročitano.", "pisma su označena kao nepročitana.", "pisama je označeno kao nepročitano.")
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

// AdreseJSON daje prijedloge adresa za obrazac pisma
func (h *AktiHandler) AdreseJSON(w http.ResponseWriter, r *http.Request) {
	u, _, _ := h.base(r)
	s := h.svc(w)
	if s == nil || u == nil {
		return
	}
	adrese := s.AdreseZaPismo(r.Context(), u, r.URL.Query().Get("q"))
	if adrese == nil {
		adrese = []service.PredlozenaAdresa{}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(adrese)
}

// PotpisData je stranica potpisa e-pošte
type PotpisData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	ActiveNav   string
	ViewAsBanner
	SuccessMessage string
	ErrorMessage   string

	Potpis string // spremljeni potpis (HTML)
	Zadani string // prijedlog iz profila (HTML)
}

// SetPotpis daje rukovatelju predložak stranice potpisa
func (h *AktiHandler) SetPotpis(t *template.Template) { h.tmplPotpis = t }

// ShowPotpis prikazuje uređivanje potpisa e-pošte
func (h *AktiHandler) ShowPotpis(w http.ResponseWriter, r *http.Request) {
	u, perms, base := h.base(r)
	s := h.svc(w)
	if s == nil || u == nil {
		return
	}
	cijeli, err := h.users.GetUserByID(u.ID)
	if err != nil || cijeli == nil {
		cijeli = u
	}
	d := PotpisData{CurrentUser: u, Permissions: perms, ActiveNav: "profile", ViewAsBanner: viewBanner(r), SuccessMessage: base.SuccessMessage, ErrorMessage: base.ErrorMessage,
		Potpis: s.Potpis(r.Context(), u.ID.String()), Zadani: s.ZadaniPotpis(cijeli)}
	if err := h.tmplPotpis.ExecuteTemplate(w, "posta_potpis.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandlePotpis sprema potpis
func (h *AktiHandler) HandlePotpis(w http.ResponseWriter, r *http.Request) {
	u, _, _ := h.base(r)
	s := h.svc(w)
	if s == nil || u == nil {
		return
	}
	if err := s.SpremiPotpis(r.Context(), u, r.FormValue("html")); err != nil {
		redirectWith(w, r, "/profile/potpis", "error", err.Error())
		return
	}
	if strings.TrimSpace(r.FormValue("html")) == "" {
		redirectWith(w, r, "/profile/potpis", "success", "Potpis je uklonjen.")
		return
	}
	redirectWith(w, r, "/profile/potpis", "success", "Potpis je spremljen i ubacuje se u svako novo pismo.")
}

// Logo daje znak organizacije za prikaz potpisa u uređivaču
func (h *AktiHandler) Logo(w http.ResponseWriter, r *http.Request) {
	t := models.Terms()
	if !t.HasLogo() {
		http.NotFound(w, r)
		return
	}
	zastitiDatoteku(w)
	w.Header().Set("Content-Type", vrstaZnaka(t.LogoMime))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(t.Logo)
}
