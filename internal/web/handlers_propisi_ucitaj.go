package web

import (
	"errors"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Učitavanje propisa i izmjena (Odluke (25. 11. 2026.)): obrazac, pregled
// podjele koji čeka u cekaonici, spremanje i uklanjanje. Rute su samo za
// globalnog administratora (samoAdmin), a servis to provjerava još jednom.

// Rečenice učitavanja koje slaže web
const (
	porukaPropisNeSprema    = "Program sada ne može spremiti propis. Ništa nije promijenjeno. Pokušajte ponovno za koji trenutak."
	porukaPregledIstekao    = "Pregled je istekao nakon 30 minuta, pa propis nije spremljen. Učitajte ga ponovno."
	porukaPregledIzmjene    = "Pregled je istekao nakon 30 minuta, pa izmjena nije spremljena. Dohvatite je ponovno."
	porukaDatotekaVelika    = "Datoteka je veća od 16 MB, pa je program nije pročitao. Zalijepite tekst akta."
	porukaDatotekaNijeUTF8  = "Datoteka nije spremljena kao UTF-8, pa bi slova s kvačicama bila kriva. Spremite je kao UTF-8 ili zalijepite tekst."
	porukaDatotekaNeCitaSe  = "Datoteka se ne može pročitati. Izaberite je ponovno ili zalijepite tekst."
	najveciTekstPropisa     = 16 << 20 // zalijepljen tekst ili datoteka .txt
	izborNoveSkupine        = "nova"
	prefiksCiljaIzmjene     = "cilj-"
	putUcitavanja           = "/propisi/ucitaj"
	putProvjereUcitavanja   = "/propisi/ucitaj/provjera?p="
	predlozakUcitavanja     = "propisi_ucitaj.html"
	predlozakProvjere       = "propisi_provjera.html"
	predlozakProvjereIzmjen = "propisi_provjera_izmjene.html"
)

// ObrazacPropisa je ono što je upisano u obrazac učitavanja; kad
// učitavanje ne uspije, obrazac se vraća popunjen
type ObrazacPropisa struct {
	BrojNN, BrojAkta, Poveznica     string
	Naziv, Donositelj, Datum, Tekst string
	Skupina                         string
	IzTeksta, ZaIzmjenu             bool
}

func (o ObrazacPropisa) ulaz() service.UlazPropisa {
	return service.UlazPropisa{BrojNN: o.BrojNN, BrojAkta: o.BrojAkta, Poveznica: o.Poveznica, Tekst: o.Tekst,
		Naziv: o.Naziv, Donositelj: o.Donositelj, Datum: o.Datum, Skupina: o.Skupina, IzTeksta: o.IzTeksta}
}

// PropisiUcitajHandler je učitavanje, dodavanje izmjena i uklanjanje
// propisa
type PropisiUcitajHandler struct {
	*PropisiHandler
	tUcitaj, tProvjera, tProvjeraIzmjene *template.Template
	propisi                              *cekaonica[*service.PregledPropisa]
	izmjene                              *cekaonica[*service.PregledIzmjene]
}

// NewPropisiUcitajHandler sastavlja rukovatelja učitavanja uz rukovatelja
// čitanja propisa
func NewPropisiUcitajHandler(citanje *PropisiHandler, ucitaj, provjera, provjeraIzmjene *template.Template) *PropisiUcitajHandler {
	return &PropisiUcitajHandler{PropisiHandler: citanje, tUcitaj: ucitaj, tProvjera: provjera, tProvjeraIzmjene: provjeraIzmjene,
		propisi: novaCekaonica[*service.PregledPropisa](), izmjene: novaCekaonica[*service.PregledIzmjene]()}
}

// korisnikPregleda je osoba uz koju se veže pregled na čekanju
func korisnikPregleda(d PropisiPageData) string {
	if d.CurrentUser == nil {
		return ""
	}
	return d.CurrentUser.ID.String()
}

// porukaUcitavanja je rečenica za grešku servisa; kvar ide u dnevnik
func porukaUcitavanja(err error) string {
	var g service.GreskaPropisa
	if errors.As(err, &g) {
		if g.Error() != err.Error() {
			log.Printf("propisi: %v", err)
		}
		return g.Error()
	}
	log.Printf("propisi: %v", err)
	return porukaPropisNeSprema
}

// nemaPropisa: propisa ili izmjene nema, pa odgovor daje stranica čitanja
func nemaPropisa(err error) bool {
	return errors.Is(err, service.ErrNemaPropisa) || errors.Is(err, service.ErrNemaIzmjene)
}

// ObrazacUcitaj je obrazac „Učitaj iz Narodnih novina”
func (h *PropisiUcitajHandler) ObrazacUcitaj(w http.ResponseWriter, r *http.Request) {
	if h.servis(w) == nil {
		return
	}
	d := h.podaci(r)
	// „Natrag” s provjere vraća broj NN i akta, da se ne upisuju iznova
	q := r.URL.Query()
	d.Obrazac = &ObrazacPropisa{BrojNN: strings.TrimSpace(q.Get("broj_nn")), BrojAkta: strings.TrimSpace(q.Get("broj_akta"))}
	iscrtajPropise(w, h.tUcitaj, predlozakUcitavanja, http.StatusOK, d)
}

// Ucitaj dohvaća akt ili čita zalijepljen tekst, a pregled podjele stavlja
// na čekanje i vodi na provjeru
func (h *PropisiUcitajHandler) Ucitaj(w http.ResponseWriter, r *http.Request) {
	svc := h.servis(w)
	if svc == nil {
		return
	}
	d := h.podaci(r)
	o, err := procitajObrazacPropisa(r)
	var pregled *service.PregledPropisa
	if err == nil {
		pregled, err = svc.Procitaj(r.Context(), d.Permissions, o.ulaz())
	}
	if err != nil {
		h.ponoviObrazac(w, r, svc, d, o, err)
		return
	}
	http.Redirect(w, r, putProvjereUcitavanja+h.propisi.stavi(korisnikPregleda(d), pregled), http.StatusSeeOther)
}

// ponoviObrazac vraća popunjen obrazac s rečenicom zašto nije uspjelo
func (h *PropisiUcitajHandler) ponoviObrazac(w http.ResponseWriter, r *http.Request, svc *service.PropisiService, d PropisiPageData, o ObrazacPropisa, err error) {
	if o.ZaIzmjenu {
		st, errP := svc.Propis(r.Context(), r.PathValue("propis"))
		if errP != nil {
			h.greska(w, r, svc, errP, "")
			return
		}
		d.Stranica = st
	}
	d.Obrazac, d.ErrorMessage = &o, porukaUcitavanja(err)
	iscrtajPropise(w, h.tUcitaj, predlozakUcitavanja, http.StatusUnprocessableEntity, d)
}

// procitajObrazacPropisa čita obrazac učitavanja; tekst iz datoteke .txt
// ima prednost pred zalijepljenim
func procitajObrazacPropisa(r *http.Request) (ObrazacPropisa, error) {
	if err := r.ParseMultipartForm(1 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		return ObrazacPropisa{}, service.GreskaPropisa(porukaDatotekaNeCitaSe)
	}
	o := ObrazacPropisa{BrojNN: r.FormValue("broj_nn"), BrojAkta: r.FormValue("broj_akta"), Poveznica: r.FormValue("poveznica"),
		Naziv: r.FormValue("naziv"), Donositelj: r.FormValue("donositelj"), Datum: r.FormValue("datum_objave"),
		Tekst: r.FormValue("tekst"), Skupina: skupinaIzObrasca(r), IzTeksta: r.FormValue("nacin") == "tekst",
		ZaIzmjenu: r.PathValue("propis") != ""}
	datoteka, err := tekstIzDatoteke(r)
	if datoteka != "" {
		o.Tekst, o.IzTeksta = datoteka, true
	}
	return o, err
}

// skupinaIzObrasca: izabrana skupina ili upisan naziv nove
func skupinaIzObrasca(r *http.Request) string {
	if s := r.FormValue("skupina"); s != izborNoveSkupine {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(r.FormValue("nova_skupina"))
}

// tekstIzDatoteke je sadržaj izabrane datoteke .txt; bez datoteke prazno
func tekstIzDatoteke(r *http.Request) (string, error) {
	f, _, err := r.FormFile("datoteka")
	if errors.Is(err, http.ErrMissingFile) || errors.Is(err, http.ErrNotMultipart) {
		return "", nil
	}
	if err != nil {
		return "", service.GreskaPropisa(porukaDatotekaNeCitaSe)
	}
	defer f.Close()
	b, err := procitajDatoteku(f, najveciTekstPropisa)
	switch {
	case err != nil:
		return "", service.GreskaPropisa(porukaDatotekaVelika)
	case !utf8.Valid(b):
		return "", service.GreskaPropisa(porukaDatotekaNijeUTF8)
	}
	return string(b), nil
}

// Provjera je pregled podjele koji čeka potvrdu
func (h *PropisiUcitajHandler) Provjera(w http.ResponseWriter, r *http.Request) {
	if h.servis(w) == nil {
		return
	}
	d := h.podaci(r)
	d.Kljuc = r.URL.Query().Get("p")
	pregled, ok := h.propisi.uzmi(d.Kljuc, korisnikPregleda(d))
	if !ok {
		redirectWith(w, r, putUcitavanja, "error", porukaPregledIstekao)
		return
	}
	d.Pregled = pregled
	iscrtajPropise(w, h.tProvjera, predlozakProvjere, http.StatusOK, d)
}

// SpremiPropis sprema propis iz pregleda s upisanom adresom i skupinom
func (h *PropisiUcitajHandler) SpremiPropis(w http.ResponseWriter, r *http.Request) {
	svc := h.servis(w)
	if svc == nil {
		return
	}
	d := h.podaci(r)
	kljuc := r.PostFormValue("p")
	pregled, ok := h.propisi.uzmi(kljuc, korisnikPregleda(d))
	if !ok {
		redirectWith(w, r, putUcitavanja, "error", porukaPregledIstekao)
		return
	}
	p, err := svc.Spremi(r.Context(), d.Permissions, d.CurrentUser, pregled, service.OdabirPropisa{Adresa: r.PostFormValue("adresa"), Skupina: skupinaIzObrasca(r)})
	if err != nil {
		redirectWith(w, r, putProvjereUcitavanja+kljuc, "error", porukaUcitavanja(err))
		return
	}
	h.propisi.makni(kljuc)
	redirectWith(w, r, "/propisi/"+p.Adresa, "success", "Propis „"+p.Naziv+"” je spremljen i stiže na sva računala u mreži.")
}

// putPropisa je stranica propisa iz adrese u ruti
func putPropisa(r *http.Request) string {
	return "/propisi/" + url.PathEscape(r.PathValue("propis"))
}

// ObrazacIzmjene je obrazac „Dodaj izmjenu” s izmjenama koje Narodne
// novine navode, a nisu učitane
func (h *PropisiUcitajHandler) ObrazacIzmjene(w http.ResponseWriter, r *http.Request) {
	svc := h.servis(w)
	if svc == nil {
		return
	}
	st, err := svc.Propis(r.Context(), r.PathValue("propis"))
	if err != nil {
		h.greska(w, r, svc, err, "")
		return
	}
	d := h.podaci(r)
	d.Stranica, d.Obrazac = st, &ObrazacPropisa{ZaIzmjenu: true}
	iscrtajPropise(w, h.tUcitaj, predlozakUcitavanja, http.StatusOK, d)
}

// UcitajIzmjenu dohvaća ili čita izmjenu, a pregled odredbi stavlja na
// čekanje i vodi na provjeru
func (h *PropisiUcitajHandler) UcitajIzmjenu(w http.ResponseWriter, r *http.Request) {
	svc := h.servis(w)
	if svc == nil {
		return
	}
	d := h.podaci(r)
	o, err := procitajObrazacPropisa(r)
	var pregled *service.PregledIzmjene
	if err == nil {
		pregled, err = svc.ProcitajIzmjenu(r.Context(), d.Permissions, r.PathValue("propis"), o.ulaz())
	}
	if err != nil {
		h.ponoviObrazac(w, r, svc, d, o, err)
		return
	}
	http.Redirect(w, r, putPropisa(r)+"/dodaj-izmjenu/provjera?p="+h.izmjene.stavi(korisnikPregleda(d), pregled), http.StatusSeeOther)
}

// pregledIzmjene je pregled na čekanju koji pripada osobi i propisu iz
// rute
func (h *PropisiUcitajHandler) pregledIzmjene(kljuc string, d PropisiPageData, r *http.Request) (*service.PregledIzmjene, bool) {
	p, ok := h.izmjene.uzmi(kljuc, korisnikPregleda(d))
	adresa := r.PathValue("propis")
	return p, ok && (p.Propis.Adresa == adresa || p.Propis.ID == adresa)
}

// ProvjeraIzmjene je tablica odredbi izmjene koja čeka potvrdu
func (h *PropisiUcitajHandler) ProvjeraIzmjene(w http.ResponseWriter, r *http.Request) {
	if h.servis(w) == nil {
		return
	}
	d := h.podaci(r)
	d.Kljuc = r.URL.Query().Get("p")
	pregled, ok := h.pregledIzmjene(d.Kljuc, d, r)
	if !ok {
		redirectWith(w, r, putPropisa(r)+"/dodaj-izmjenu", "error", porukaPregledIzmjene)
		return
	}
	d.PregledIzmjene = pregled
	iscrtajPropise(w, h.tProvjeraIzmjene, predlozakProvjereIzmjen, http.StatusOK, d)
}

// SpremiIzmjenu sprema izmjenu iz pregleda, s odredbama koje je
// administrator sam povezao s člankom
func (h *PropisiUcitajHandler) SpremiIzmjenu(w http.ResponseWriter, r *http.Request) {
	svc := h.servis(w)
	if svc == nil {
		return
	}
	d := h.podaci(r)
	kljuc := r.PostFormValue("p")
	pregled, ok := h.pregledIzmjene(kljuc, d, r)
	if !ok {
		redirectWith(w, r, putPropisa(r)+"/dodaj-izmjenu", "error", porukaPregledIzmjene)
		return
	}
	iz, err := svc.SpremiIzmjenu(r.Context(), d.Permissions, d.CurrentUser, pregled, ciljeviIzObrasca(r.PostForm))
	switch {
	case nemaPropisa(err):
		h.greska(w, r, svc, err, "")
	case err != nil:
		redirectWith(w, r, putPropisa(r)+"/dodaj-izmjenu/provjera?p="+kljuc, "error", porukaUcitavanja(err))
	default:
		h.izmjene.makni(kljuc)
		redirectWith(w, r, "/propisi/"+pregled.Propis.Adresa, "success", porukaSpremljeneIzmjene(iz))
	}
}

// ciljeviIzObrasca su ručni izbori „Članak propisa” po rednom broju
// odredbe (polja cilj-<redni>)
func ciljeviIzObrasca(f url.Values) map[int]string {
	out := map[int]string{}
	for k, v := range f {
		broj, ok := strings.CutPrefix(k, prefiksCiljaIzmjene)
		if redni, err := strconv.Atoi(broj); ok && err == nil && len(v) > 0 {
			out[redni] = v[0]
		}
	}
	return out
}

// porukaSpremljeneIzmjene: „Izmjena NN 47/2023 je spremljena. Oznaka stoji
// uz 17 članaka.”
func porukaSpremljeneIzmjene(iz *models.IzmjenaPropisa) string {
	ciljevi, clanci := map[string]bool{}, true
	for _, u := range iz.Upute {
		if !u.MijenjaPropis() {
			continue
		}
		for _, c := range u.Cilj.Clanci {
			ciljevi[c] = true
			clanci = clanci && strings.HasPrefix(c, models.DioClanak+"-")
		}
	}
	poruka := "Izmjena " + iz.NN.Puno() + " je spremljena."
	n := len(ciljevi)
	switch {
	case n == 0:
		return poruka
	case clanci:
		return poruka + " Oznaka stoji uz " + strconv.Itoa(n) + " " + models.UzBrojHR(n, "članak", "članka", "članaka") + "."
	}
	return poruka + " Oznaka stoji uz " + strconv.Itoa(n) + " " + models.UzBrojHR(n, "dio", "dijela", "dijelova") + "."
}

// UkloniPropis uklanja propis i izmjene, uz obavezan razlog
func (h *PropisiUcitajHandler) UkloniPropis(w http.ResponseWriter, r *http.Request) {
	svc := h.servis(w)
	if svc == nil {
		return
	}
	d := h.podaci(r)
	p, err := svc.Ukloni(r.Context(), d.Permissions, d.CurrentUser, r.PathValue("propis"), r.PostFormValue("razlog"))
	switch {
	case nemaPropisa(err):
		h.greska(w, r, svc, err, "")
	case err != nil:
		redirectWith(w, r, putPropisa(r), "error", porukaUcitavanja(err))
	default:
		redirectWith(w, r, "/propisi", "success", "Propis „"+p.Naziv+"” je uklonjen. U povijesti zapisa ostaje.")
	}
}

// UkloniIzmjenu uklanja izmjenu propisa, uz obavezan razlog
func (h *PropisiUcitajHandler) UkloniIzmjenu(w http.ResponseWriter, r *http.Request) {
	svc := h.servis(w)
	if svc == nil {
		return
	}
	d := h.podaci(r)
	nnID := r.PathValue("izmjena")
	iz, err := svc.UkloniIzmjenu(r.Context(), d.Permissions, d.CurrentUser, r.PathValue("propis"), nnID, r.PostFormValue("razlog"))
	switch {
	case nemaPropisa(err):
		h.greska(w, r, svc, err, "izmjene „"+nnID+"”")
	case err != nil:
		redirectWith(w, r, putPropisa(r)+"/izmjene/"+url.PathEscape(nnID), "error", porukaUcitavanja(err))
	default:
		redirectWith(w, r, putPropisa(r), "success", "Izmjena "+iz.NN.Puno()+" je uklonjena.")
	}
}
