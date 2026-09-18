package web

import (
	"context"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/pdfpotpis"
	"gocop/internal/poslovi"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Akti: rješenja i obavijesti o stupnju obrane. Sastave se po vodomjeru,
// ovjere u programu, ispišu kao PDF; ovjerene vide svi.

type AktiHandler struct {
	akti                                         func() *service.AktService
	users                                        *service.UserService
	stations                                     *service.StationService
	tmplPopis, tmplForm, tmplAkt, tmplPrimatelji *template.Template
	tmplSpranca                                  *template.Template
	tmplPosta, tmplPostaAdmin                    *template.Template
	tmplSanducic, tmplPismo, tmplNovoPismo       *template.Template
	tmplImenik                                   *template.Template
	poslovi                                      *poslovi.Registar
}

// SetSpranca daje rukovatelju predložak stranice špranče
func (h *AktiHandler) SetSpranca(t *template.Template) { h.tmplSpranca = t }

func NewAktiHandler(akti func() *service.AktService, users *service.UserService, stations *service.StationService,
	popis, form, akt, primatelji *template.Template) *AktiHandler {
	return &AktiHandler{akti: akti, users: users, stations: stations, tmplPopis: popis, tmplForm: form, tmplAkt: akt, tmplPrimatelji: primatelji}
}

// AktiPageData je stranica popisa, obrasca ili jednog akta
type AktiPageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions

	Akti      []models.Akt
	Akt       *models.Akt
	Filtar    repository.FiltarAkata
	Sektori   []models.Sector
	Podrucja  []models.Area
	Godine    []int
	Stupnjevi []models.DefensePhase

	// obrazac
	Station    *models.Station
	Stanice    []models.Station
	Dionice    []models.Section
	Zadnje     *models.Reading
	Ocitanja   []models.Reading // za izbor očitanja na koje se akt poziva
	ZaPrekid   []models.Akt     // ovjereni akti o uspostavi po vodomjeru, za prekid
	Tendencije []struct{ Kod, Naziv string }
	LocalValue string
	Radnja     string
	Stupanj    models.DefensePhase

	// jedan akt
	Sektor          *models.Sector
	Podrucje        *models.Area
	SmijeOvjeriti   bool
	SmijeObrisati   bool
	Potpis          string            // stanje elektroničkog potpisa: VRIJEDI, NE_VRIJEDI, NEMA
	Izvornik        *pdfpotpis.Potpis // ponovna provjera potpisa na izvorniku iz SIGNATOR-a
	SmijePripremiti bool
	MoguPotpisati   []models.User // za izbor potpisnika uz sken
	Slanje          *SlanjeData   // slanje izvornika primateljima "na znanje"
	Upozorenja      []string

	// špranca
	Spranca models.Spranca
	Zadana  models.Spranca

	// registar primatelja
	Primatelji   []models.Primatelj
	Skupine      []string
	SektorID     string
	SmijeUrediti bool

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

func (h *AktiHandler) base(r *http.Request) (*models.User, *models.UserPermissions, AktiPageData) {
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	q := r.URL.Query()
	data := AktiPageData{CurrentUser: u, Permissions: perms, SuccessMessage: q.Get("success"), ErrorMessage: q.Get("error"),
		ActiveNav: "journals", ViewAsBanner: viewBanner(r), Stupnjevi: models.StupnjeviAkta, Skupine: models.SkupinePrimatelja}
	data.Sektori, _ = h.users.ListSectors()
	return u, perms, data
}

func (h *AktiHandler) svc(w http.ResponseWriter) *service.AktService {
	s := h.akti()
	if s == nil {
		http.Error(w, "akti nisu dostupni", http.StatusServiceUnavailable)
	}
	return s
}

// ShowPopis je popis akata s filtrima i pretragom
func (h *AktiHandler) ShowPopis(w http.ResponseWriter, r *http.Request) {
	_, perms, data := h.base(r)
	s := h.svc(w)
	if s == nil {
		return
	}
	q := r.URL.Query()
	f := repository.FiltarAkata{Sektor: q.Get("sektor"), Radnja: q.Get("radnja"), Status: q.Get("status"), Trazi: strings.TrimSpace(q.Get("q")), Limit: 500}
	f.AreaID, _ = strconv.Atoi(q.Get("podrucje"))
	f.Godina, _ = strconv.Atoi(q.Get("godina"))
	f.Stupanj = models.DefensePhase(q.Get("stupanj"))
	if f.Sektor == "" && perms != nil && !perms.IsGlobalAdmin && len(perms.AllowedSectors) == 1 {
		for s := range perms.AllowedSectors {
			f.Sektor = s
		}
	}
	data.Filtar = f
	data.Podrucja, _ = h.users.ListAreas(f.Sektor)
	akti, err := s.List(r.Context(), perms, f)
	if err != nil {
		data.ErrorMessage = err.Error()
	}
	data.Akti = akti
	g := time.Now().In(models.Zagreb).Year()
	for y := g; y >= g-5; y-- {
		data.Godine = append(data.Godine, y)
	}
	if err := h.tmplPopis.ExecuteTemplate(w, "akti.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ShowForm je obrazac za novi akt: vodomjer, radnja, stupanj, vrijeme
func (h *AktiHandler) ShowForm(w http.ResponseWriter, r *http.Request) {
	_, perms, data := h.base(r)
	if h.svc(w) == nil {
		return
	}
	q := r.URL.Query()
	data.Radnja = q.Get("radnja")
	if data.Radnja == "" {
		data.Radnja = models.AktUspostava
	}
	data.Stupanj = models.DefensePhase(q.Get("stupanj"))
	if data.Stupanj == "" {
		data.Stupanj = models.PhasePrep
	}
	data.LocalValue = time.Now().In(models.Zagreb).Format("2006-01-02T15:04")
	if id, err := uuid.Parse(q.Get("station")); err == nil {
		if st, err := h.stations.GetStation(r.Context(), id); err == nil && st != nil {
			data.Station = st
			data.Dionice = h.dioniceLetve(r.Context(), st)
			if s := h.akti(); s != nil {
				if zadnja, err := s.ZadnjeOcitanje(r.Context(), st.ID.String()); err == nil {
					data.Zadnje = zadnja
				}
				data.Ocitanja, _ = s.OcitanjaZaAkt(r.Context(), st.ID.String(), 200)
				data.ZaPrekid, _ = s.AktiZaPrekid(r.Context(), st.ID.String(), "")
				data.Tendencije = models.Tendencije
			}
		}
	}
	if data.Station == nil {
		sve, _ := h.stations.ListStations(r.Context(), "", "", false)
		for _, st := range sve {
			if len(st.SectionCodes) > 0 && (perms == nil || perms.IsGlobalAdmin || smijeLetvu(perms, st)) {
				data.Stanice = append(data.Stanice, st)
			}
		}
	}
	if err := h.tmplForm.ExecuteTemplate(w, "akt_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// smijeLetvu javlja može li osoba sastaviti akt po toj letvi: bar jedna od
// njezinih dionica je u dosegu
func smijeLetvu(perms *models.UserPermissions, st models.Station) bool {
	for _, c := range st.SectionCodes {
		if perms.HasWriteAccess("", 0, c) || perms.AllowedSections[c] {
			return true
		}
	}
	for s := range perms.AllowedSectors {
		if s != "" {
			return true
		}
	}
	return len(perms.AllowedAreas) > 0 || len(perms.AdminAreas) > 0 || len(perms.AdminSectors) > 0
}

func (h *AktiHandler) dioniceLetve(ctx context.Context, st *models.Station) []models.Section {
	s := h.akti()
	if s == nil {
		return nil
	}
	return s.DioniceLetve(ctx, st)
}

// HandleCreate sastavlja nacrt iz obrasca i sprema ga
func (h *AktiHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s := h.svc(w)
	if s == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/akti/novi", "error", "Neispravan zahtjev")
		return
	}
	z := service.ZahtjevAkta{
		StationID: r.FormValue("station_id"), Radnja: r.FormValue("radnja"), Stupanj: models.DefensePhase(r.FormValue("stupanj")),
		Prognoza: r.FormValue("prognoza"), Napomena: r.FormValue("napomena"), Dionice: r.Form["dionica"],
		OcitanjeID: r.FormValue("ocitanje_id"), Tendencija: r.FormValue("tendencija"), PrekidaAktID: r.FormValue("prekida_akt_id"),
	}
	if t, err := time.ParseInLocation("2006-01-02T15:04", r.FormValue("vrijedi"), models.Zagreb); err == nil {
		z.Vrijedi = t
	}
	natrag := "/akti/novi?station=" + z.StationID + "&radnja=" + z.Radnja + "&stupanj=" + string(z.Stupanj)
	a, err := s.Pripremi(r.Context(), perms, u, z)
	if err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	if err := s.Spremi(r.Context(), perms, a); err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	redirectWith(w, r, "/akti/"+a.ID, "success", "Nacrt je sastavljen. Pregledaj ga i ovjeri.")
}

func (h *AktiHandler) ucitaj(w http.ResponseWriter, r *http.Request) (*service.AktService, *models.Akt) {
	s := h.svc(w)
	if s == nil {
		return nil, nil
	}
	a, err := s.Get(r.Context(), r.PathValue("id"))
	if err != nil || a == nil {
		http.NotFound(w, r)
		return nil, nil
	}
	return s, a
}

// ShowAkt prikazuje akt: tekst kako će stajati na papiru, primatelje i ovjeru
func (h *AktiHandler) ShowAkt(w http.ResponseWriter, r *http.Request) {
	u, perms, data := h.base(r)
	s, a := h.ucitaj(w, r)
	if a == nil {
		return
	}
	data.Akt = a
	data.Potpis = service.ProvjeriPotpis(a)
	if a.Kvalificirani != nil {
		data.Izvornik = s.ProvjeriIzvornik(r.Context(), a)
	}
	data.SmijePripremiti = !a.Ovjeren() && u != nil && (a.IzradioID == u.ID.String() || (perms != nil && perms.HasWriteAccess(a.Sektor, a.AreaID, "")) || s.SmijeOvjeriti(perms, a))
	if data.SmijePripremiti {
		data.MoguPotpisati = s.MoguPotpisati(a)
	}
	data.Slanje = h.slanjeZaStranicu(r, s, perms, u, a)
	data.SmijeOvjeriti = !a.Ovjeren() && s.SmijeOvjeriti(perms, a)
	data.SmijeObrisati = !a.Ovjeren() && u != nil && (a.IzradioID == u.ID.String() || s.SmijeOvjeriti(perms, a))
	for i := range data.Sektori {
		if data.Sektori[i].ID == a.Sektor {
			data.Sektor = &data.Sektori[i]
		}
	}
	if areas, err := h.users.ListAreas(a.Sektor); err == nil {
		for i := range areas {
			if areas[i].ID == a.AreaID {
				data.Podrucje = &areas[i]
			}
		}
	}
	if err := h.tmplAkt.ExecuteTemplate(w, "akt.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleTekst sprema ispravljeni tekst nacrta
func (h *AktiHandler) HandleTekst(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s, a := h.ucitaj(w, r)
	if a == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/akti/"+a.ID, "error", "Neispravan zahtjev")
		return
	}
	if _, err := s.UrediTekst(r.Context(), perms, u, a.ID, r.FormValue("uvod"), r.FormValue("izvan_snage"), r.FormValue("zavrsno"), r.FormValue("napomena")); err != nil {
		redirectWith(w, r, "/akti/"+a.ID, "error", err.Error())
		return
	}
	redirectWith(w, r, "/akti/"+a.ID, "success", "Tekst nacrta je spremljen.")
}

// ShowSpranca prikazuje šprancu sektora za uređivanje
func (h *AktiHandler) ShowSpranca(w http.ResponseWriter, r *http.Request) {
	_, perms, data := h.base(r)
	s := h.svc(w)
	if s == nil {
		return
	}
	data.SektorID = r.URL.Query().Get("sektor")
	if data.SektorID == "" && perms != nil {
		for id := range perms.AllowedSectors {
			data.SektorID = id
			break
		}
		if data.SektorID == "" && len(data.Sektori) > 0 {
			data.SektorID = data.Sektori[0].ID
		}
	}
	data.Spranca, _ = s.Spranca(r.Context(), data.SektorID)
	data.Zadana = models.ZadanaSpranca(data.SektorID)
	data.SmijeUrediti = perms != nil && perms.CanAdminister(data.SektorID, 0)
	if err := h.tmplSpranca.ExecuteTemplate(w, "spranca.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleSpranca sprema šprancu sektora; "zadano" vraća zadani tekst
func (h *AktiHandler) HandleSpranca(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s := h.svc(w)
	if s == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/akti/spranca", "error", "Neispravan zahtjev")
		return
	}
	sektor := r.FormValue("sektor")
	natrag := "/akti/spranca?sektor=" + sektor
	sp := models.ZadanaSpranca(sektor)
	if r.FormValue("zadano") == "" {
		sp.Osnova, sp.Zavrsno, sp.Poveznice = r.FormValue("osnova"), r.FormValue("zavrsno"), strings.TrimSpace(r.FormValue("poveznice"))
		for _, p := range models.StupnjeviAkta {
			if c := strings.TrimSpace(r.FormValue("clanak_" + string(p))); c != "" {
				sp.Clanci[p] = c
			}
		}
	}
	if err := s.SpremiSprancu(r.Context(), perms, u, &sp); err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	redirectWith(w, r, natrag, "success", "Špranca je spremljena; vrijedi za nove nacrte.")
}

// HandleOvjeri ovjerava nacrt
func (h *AktiHandler) HandleOvjeri(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s, a := h.ucitaj(w, r)
	if a == nil {
		return
	}
	a, upozorenja, err := s.Ovjeri(r.Context(), perms, u, a.ID)
	if err != nil {
		redirectWith(w, r, "/akti/"+r.PathValue("id"), "error", err.Error())
		return
	}
	poruka := "Akt " + a.Oznaka() + " je ovjeren."
	if len(upozorenja) > 0 {
		poruka += " Stanje obrane na dionicama: " + strings.Join(upozorenja, "; ")
	}
	redirectWith(w, r, "/akti/"+a.ID, "success", poruka)
}

// HandleObrisi briše nacrt
func (h *AktiHandler) HandleObrisi(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s, a := h.ucitaj(w, r)
	if a == nil {
		return
	}
	if err := s.Obrisi(r.Context(), perms, u, a.ID); err != nil {
		redirectWith(w, r, "/akti/"+a.ID, "error", err.Error())
		return
	}
	redirectWith(w, r, "/akti", "success", "Nacrt je obrisan.")
}

// IzvoziPDF daje akt kao PDF
func (h *AktiHandler) IzvoziPDF(w http.ResponseWriter, r *http.Request) {
	s, a := h.ucitaj(w, r)
	if a == nil {
		return
	}
	// akt potpisan u SIGNATOR-u ili ručno pa skeniran: izvornik, bajt za bajt
	if a.ImaIzvornik() {
		if pdf, err := s.Izvornik(r.Context(), a.ID); err == nil && pdf != nil {
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Disposition", `inline; filename="`+imeDatotekeAkta(a)+`"`)
			_, _ = w.Write(pdf)
			return
		}
	}
	sek, area := h.sektorIPodrucje(a)
	ime := imeDatotekeAkta(a)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="`+ime+`"`)
	_, _ = w.Write(PDFAkta(a, models.Terms(), sek, area))
}

// IzvoziZaPotpis daje PDF nacrta za potpis u SIGNATOR-u i bilježi ga, da se
// potpisani PDF po povratku može prepoznati
func (h *AktiHandler) IzvoziZaPotpis(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s, a := h.ucitaj(w, r)
	if a == nil {
		return
	}
	sek, area := h.sektorIPodrucje(a)
	pdf := PDFAktaZaPotpis(a, models.Terms(), sek, area)
	if err := s.ZabiljeziZaPotpis(r.Context(), perms, u, a.ID, pdf); err != nil {
		redirectWith(w, r, "/akti/"+a.ID, "error", err.Error())
		return
	}
	ime := strings.TrimSuffix(imeDatotekeAkta(a), "_nacrt.pdf")
	ime = strings.TrimSuffix(ime, "-nacrt.pdf")
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.TrimSuffix(ime, ".pdf")+"-za-potpis.pdf"+`"`)
	_, _ = w.Write(pdf)
}

// IzvoziZaIspis daje PDF nacrta za ispis, vlastoručni potpis i žig
func (h *AktiHandler) IzvoziZaIspis(w http.ResponseWriter, r *http.Request) {
	_, a := h.ucitaj(w, r)
	if a == nil {
		return
	}
	sek, area := h.sektorIPodrucje(a)
	ime := strings.TrimSuffix(strings.TrimSuffix(imeDatotekeAkta(a), ".pdf"), "-nacrt")
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="`+ime+`-za-ispis.pdf"`)
	_, _ = w.Write(PDFAktaZaIspis(a, models.Terms(), sek, area))
}

// HandleUcitajSken prima sken ispisa potpisanog vlastoručno i ovjerenog žigom
func (h *AktiHandler) HandleUcitajSken(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s, a := h.ucitaj(w, r)
	if a == nil {
		return
	}
	natrag := "/akti/" + a.ID
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		redirectWith(w, r, natrag, "error", "Odaberite sken potpisanog akta")
		return
	}
	f, _, err := r.FormFile("sken")
	if err != nil {
		redirectWith(w, r, natrag, "error", "Odaberite sken potpisanog akta")
		return
	}
	defer f.Close()
	podaci, err := io.ReadAll(io.LimitReader(f, 32<<20))
	if err != nil {
		redirectWith(w, r, natrag, "error", "Sken nije čitljiv")
		return
	}
	a, upozorenja, err := s.UcitajSkenirani(r.Context(), perms, u, a.ID, podaci, r.FormValue("potpisnik_id"))
	if err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	poruka := "Akt " + a.Oznaka() + " je ovjeren: sken s potpisom i žigom (" + a.Rucno.Potpisnik + ") je izvornik."
	if len(upozorenja) > 0 {
		poruka += " Stanje obrane na dionicama: " + strings.Join(upozorenja, "; ")
	}
	redirectWith(w, r, natrag, "success", poruka)
}

// HandleUcitajPotpisani prima PDF potpisan u SIGNATOR-u i njime ovjerava akt
func (h *AktiHandler) HandleUcitajPotpisani(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s, a := h.ucitaj(w, r)
	if a == nil {
		return
	}
	natrag := "/akti/" + a.ID
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		redirectWith(w, r, natrag, "error", "Odaberite potpisani PDF")
		return
	}
	f, _, err := r.FormFile("potpisani")
	if err != nil {
		redirectWith(w, r, natrag, "error", "Odaberite potpisani PDF")
		return
	}
	defer f.Close()
	pdf, err := io.ReadAll(io.LimitReader(f, 32<<20))
	if err != nil {
		redirectWith(w, r, natrag, "error", "PDF nije čitljiv")
		return
	}
	a, upozorenja, err := s.UcitajPotpisani(r.Context(), perms, u, a.ID, pdf)
	if err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	poruka := "Akt " + a.Oznaka() + " je ovjeren kvalificiranim potpisom: " + a.Kvalificirani.Ime + ". Potpisani PDF je izvornik."
	if len(upozorenja) > 0 {
		poruka += " Stanje obrane na dionicama: " + strings.Join(upozorenja, "; ")
	}
	redirectWith(w, r, natrag, "success", poruka)
}

// sektorIPodrucje su podaci sektora i branjenog područja za zaglavlje akta
func (h *AktiHandler) sektorIPodrucje(a *models.Akt) (*models.Sector, *models.Area) {
	var sek *models.Sector
	if sektori, err := h.users.ListSectors(); err == nil {
		for i := range sektori {
			if sektori[i].ID == a.Sektor {
				sek = &sektori[i]
			}
		}
	}
	var area *models.Area
	if areas, err := h.users.ListAreas(a.Sektor); err == nil {
		for i := range areas {
			if areas[i].ID == a.AreaID {
				area = &areas[i]
			}
		}
	}
	return sek, area
}

// imeDatotekeAkta je naziv PDF-a po uzoru na dosadašnje: vodomjer, radnja,
// stupanj, datum
func imeDatotekeAkta(a *models.Akt) string {
	stupanj := map[models.DefensePhase]string{models.PhasePrep: "PS", models.PhaseRegular: "RO", models.PhaseEmergency: "IO", models.PhaseState: "IS"}[a.Stupanj]
	radnja := "uspostava"
	if a.Radnja == models.AktPrekid {
		radnja = "prekid"
	}
	ime := a.StationName + "_" + radnja + "_" + stupanj + "_" + a.Vrijedi.In(models.Zagreb).Format("2006_01_02")
	if !a.Ovjeren() {
		ime += "_nacrt"
	}
	return sigurnoIme(ime) + ".pdf"
}

// ---- registar primatelja ----

// ShowPrimatelji prikazuje registar primatelja sektora
func (h *AktiHandler) ShowPrimatelji(w http.ResponseWriter, r *http.Request) {
	_, perms, data := h.base(r)
	s := h.svc(w)
	if s == nil {
		return
	}
	data.SektorID = r.URL.Query().Get("sektor")
	if data.SektorID == "" && perms != nil {
		for id := range perms.AllowedSectors {
			data.SektorID = id
			break
		}
		if data.SektorID == "" && len(data.Sektori) > 0 {
			data.SektorID = data.Sektori[0].ID
		}
	}
	data.Podrucja, _ = h.users.ListAreas(data.SektorID)
	data.Primatelji, _ = s.Primatelji(r.Context(), data.SektorID)
	data.SmijeUrediti = perms != nil && perms.CanAdminister(data.SektorID, 0)
	if err := h.tmplPrimatelji.ExecuteTemplate(w, "primatelji.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleSavePrimatelj dodaje ili mijenja primatelja
func (h *AktiHandler) HandleSavePrimatelj(w http.ResponseWriter, r *http.Request) {
	_, perms, _ := h.base(r)
	s := h.svc(w)
	if s == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/akti/primatelji", "error", "Neispravan zahtjev")
		return
	}
	p := models.Primatelj{ID: r.FormValue("id"), Sektor: r.FormValue("sektor"), Naziv: r.FormValue("naziv"), Email: strings.TrimSpace(r.FormValue("email")),
		Skupina: r.FormValue("skupina"), OdStupnja: models.DefensePhase(r.FormValue("od_stupnja")), Aktivan: r.FormValue("aktivan") != "0"}
	p.AreaID, _ = strconv.Atoi(r.FormValue("area_id"))
	p.Redoslijed, _ = strconv.Atoi(r.FormValue("redoslijed"))
	natrag := "/akti/primatelji?sektor=" + p.Sektor
	if err := s.SpremiPrimatelja(r.Context(), perms, &p); err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	redirectWith(w, r, natrag, "success", "Primatelj je upisan.")
}

// HandleObrisiPrimatelja briše primatelja
func (h *AktiHandler) HandleObrisiPrimatelja(w http.ResponseWriter, r *http.Request) {
	_, perms, _ := h.base(r)
	s := h.svc(w)
	if s == nil {
		return
	}
	natrag := "/akti/primatelji?sektor=" + r.FormValue("sektor")
	if err := s.ObrisiPrimatelja(r.Context(), perms, r.PathValue("id")); err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	redirectWith(w, r, natrag, "success", "Primatelj je obrisan.")
}
