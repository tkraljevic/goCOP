package web

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"

	"github.com/google/uuid"
)

// Stranice registra postaja: jedna postaja i obrazac. Isti razlog kao kod
// vodotoka: puna stranica radi na telefonu i bez skripte.

// StationPageData je stranica jedne postaje ili njezina obrasca
type StationPageData struct {
	CurrentUser          *models.User
	Permissions          *models.UserPermissions
	Station              models.Station
	ZeroDatumHistoryJSON template.JS // promjene kote nule za obrazac, kao JS literal
	ExtremesJSON         template.JS // zabilježeni ekstremi za obrazac
	Sections             []models.Section
	Episodes             []models.DefenseEpisode   // obrane vođene po ovoj letvi, najnovija prva
	Nizovi               []models.HidroNiz         // što o ovoj letvi ima u arhivi
	Pregled              *models.HidroPregled      // karakteristične vrijednosti odabranog niza
	Profili              []models.ProfilKorita     // snimke poprečnog profila korita
	Profil               *models.ProfilKorita      // onaj koji se crta
	Krivulje             []models.HQKrivulja       // krivulje protoka po razdobljima
	NizID                int64                     // koji je niz odabran
	Spojevi              []models.SpojDoseg        // spojeni nizovi: jedan satni, jedan dnevni
	Sada                 *models.SpojenaVrijednost // zadnja vrijednost spojenog niza
	Crtez                *KoritoCrtez              // korito s vodom u njemu
	Zadnji               *models.HidroTocka        // zadnja vrijednost iz arhive
	ZadnjiProtok         float64                   // preračunat iz krivulje
	ZadnjiIzvor          string
	WaterRegistry        []models.Watercourse
	CanEdit              bool
	IsEdit               bool
	SuccessMessage       string
	ErrorMessage         string
	ActiveNav            string
	ViewAsBanner
}

// SetPageTemplates daje rukovatelju predloške stranica i servise koje one trebaju
func (h *StationsHandler) SetPageTemplates(detail, form *template.Template,
	sections *service.SectionService, waters *service.WatercourseService) {
	h.tmplDetail = detail
	h.tmplForm = form
	h.sectionService = sections
	h.watercourseService = waters
}

// SetArhiva daje rukovatelju hidrološku arhivu. Uzima se dohvatnik, a ne sama
// arhiva: poslužitelj se sastavlja prije nego što se arhiva otvori, pa bi
// vrijednost predana pri sastavljanju zauvijek ostala prazna.
//
// Arhive smije i ne biti — čvor koji je nije preuzeo prikazuje letvu bez
// povijesti.
func (h *StationsHandler) SetArhiva(f func() *repository.ArhivaRepository) {
	h.arhiva = f
}

// arh vraća arhivu ako je ima
func (h *StationsHandler) arh() *repository.ArhivaRepository {
	if h.arhiva == nil {
		return nil
	}
	return h.arhiva()
}

// SetEpisodeService daje rukovatelju epizode obrane, da se na kartici letve
// vidi tko je sve po njoj u obrani.
func (h *StationsHandler) SetEpisodeService(episodes *service.EpisodeService) {
	h.episodeService = episodes
}

func (h *StationsHandler) pageData(r *http.Request) StationPageData {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	return StationPageData{
		CurrentUser:    currUser,
		Permissions:    perms,
		SuccessMessage: r.URL.Query().Get("success"),
		ErrorMessage:   r.URL.Query().Get("error"),
		ActiveNav:      "stations",
		ViewAsBanner:   viewBanner(r),
	}
}

// canEditStation: globalni administrator sve; ostali postaju koja im je
// mjerodavna na dionici za koju smiju pisati (isto pravilo kao u servisu)
func (h *StationsHandler) canEditStation(perms *models.UserPermissions, st models.Station) bool {
	if perms == nil {
		return false
	}
	if perms.IsGlobalAdmin {
		return true
	}
	for _, code := range st.SectionCodes {
		if perms.AllowedSections[code] {
			return true
		}
	}
	return false
}

// ShowStation prikazuje jednu postaju s pragovima, kotama i dionicama
func (h *StationsHandler) ShowStation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.pageData(r)

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	st, err := h.stationService.GetStation(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if st == nil {
		http.NotFound(w, r)
		return
	}
	data.Station = *st
	data.CanEdit = h.canEditStation(data.Permissions, *st)

	if h.sectionService != nil {
		for _, code := range st.SectionCodes {
			if sec, err := h.sectionService.GetSectionWithDetails(code); err == nil && sec != nil {
				data.Sections = append(data.Sections, *sec)
			}
		}
	}
	// Ista letva mjerodavna je za više dionica — Batina za cijelo BP 34 — pa
	// se ovdje vide obrane svih njih, svaka sa svojim stupnjem.
	if h.episodeService != nil {
		data.Episodes, _ = h.episodeService.ByStation(ctx, st.ID.String(), 50)
	}
	// Hidrološka arhiva: nizovi, karakteristične vrijednosti, korito i krivulje.
	// Sve se računa pri čitanju, ništa se ne pamti — brojevi se tako ne mogu
	// razići s podacima iz kojih su nastali.
	if a := h.arh(); a != nil && st.Code != "" {
		data.Nizovi, _ = a.Nizovi(ctx, st.Code)
		data.Profili, _ = a.Profili(ctx, st.Code)
		data.Krivulje, _ = a.Krivulje(ctx, st.Code)
		if len(data.Profili) > 0 {
			data.Profil = &data.Profili[0]
		}
		data.Spojevi, _ = a.SpojDosezi(ctx, st.Code)
		data.Sada, _ = a.SpojZadnje(ctx, st.Code, "vodostaj", "satni")
		if data.Sada == nil {
			data.Sada, _ = a.SpojZadnje(ctx, st.Code, "vodostaj", "dnevni")
		}
		data.NizID = odabraniNiz(r, data.Nizovi)
		if data.NizID > 0 {
			data.Pregled, _ = a.Pregled(ctx, data.NizID)
		}
		if data.Sada != nil {
			data.Zadnji = &models.HidroTocka{Kad: data.Sada.Kad, Vrijednost: data.Sada.Vrijednost}
			data.ZadnjiIzvor = data.Sada.Izvor
		}
		if data.Zadnji != nil {
			cm := int(data.Zadnji.Vrijednost)
			if data.Profil != nil {
				data.Crtez = crtajKorito(*data.Profil, cm)
			}
			dan := data.Zadnji.Kad.Format("2006-01-02")
			for _, k := range data.Krivulje {
				if k.VrijediOd <= dan && (k.VrijediDo == "" || dan <= k.VrijediDo) {
					if q, ok := k.Protok(cm); ok {
						data.ZadnjiProtok = q
					}
					break
				}
			}
		}
	}

	if data.CanEdit && h.watercourseService != nil {
		if waters, err := h.watercourseService.ListWatercourses(ctx, "", "", false); err == nil {
			data.WaterRegistry = waters
		}
	}

	if err := h.tmplDetail.ExecuteTemplate(w, "station_detail.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ShowStationForm prikazuje obrazac za novu postaju ili izmjenu postojeće
func (h *StationsHandler) ShowStationForm(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)

	if raw := r.PathValue("id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		st, err := h.stationService.GetStation(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if st == nil {
			http.NotFound(w, r)
			return
		}
		if !h.canEditStation(data.Permissions, *st) {
			http.Error(w, "Nemate pravo uređivati ovu postaju", http.StatusForbidden)
			return
		}
		data.Station = *st
		data.IsEdit = true
	} else if !data.Permissions.IsGlobalAdmin && len(data.Permissions.AllowedSections) == 0 {
		http.Error(w, "Nemate pravo dodavati postaje", http.StatusForbidden)
		return
	}

	data.ExtremesJSON = template.JS("[]")
	if b, err := json.Marshal(data.Station.Extremes); err == nil && len(data.Station.Extremes) > 0 {
		data.ExtremesJSON = template.JS(b)
	}
	data.ZeroDatumHistoryJSON = template.JS("[]")
	if b, err := json.Marshal(data.Station.ZeroDatumHistory); err == nil && len(data.Station.ZeroDatumHistory) > 0 {
		data.ZeroDatumHistoryJSON = template.JS(b)
	}
	if err := h.tmplForm.ExecuteTemplate(w, "station_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// odabraniNiz bira niz čije se karakteristične vrijednosti prikazuju: onaj iz
// upita, inače najpouzdaniji vodostaj koji letva ima.
func odabraniNiz(r *http.Request, nizovi []models.HidroNiz) int64 {
	if s := r.URL.Query().Get("niz"); s != "" {
		if id, err := strconv.ParseInt(s, 10, 64); err == nil {
			for _, n := range nizovi {
				if n.ID == id {
					return id
				}
			}
		}
	}
	for _, n := range nizovi {
		if n.Velicina == "vodostaj" {
			return n.ID
		}
	}
	if len(nizovi) > 0 {
		return nizovi[0].ID
	}
	return 0
}
