package web

import (
	"context"
	"net/http"
	"strconv"

	"gocop/internal/models"
)

// Povezanost ustava i crpnih stanica: na jednoj stranici svi ti objekti s
// vodotocima i letvama obje strane i vezom „u sklopu”, da se mogu provjeriti
// i dopuniti jedan po jedan (Odluke (1. 11. 2026.)).

// RedakPovezanosti je jedan objekt na stranici povezanosti
type RedakPovezanosti struct {
	Objekt         models.Structure
	Vodotok        string // naziv vodotoka prve strane (uzvodno, kanal)
	VodotokDruge   string // naziv vodotoka druge strane
	Letva          string
	LetvaDruge     string
	USklopu        string // crpna stanica u čijem je sklopu ustava
	MozeUredjivati bool
}

// Nepotpun: nedostaje nešto što objekt treba za očitanje obje strane
func (r RedakPovezanosti) Nepotpun() bool {
	return r.Vodotok == "" || r.VodotokDruge == "" || r.Letva == "" || r.LetvaDruge == ""
}

// PovezanostPageData je stranica povezanosti
type PovezanostPageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	Redovi      []RedakPovezanosti
	Areas       []models.Area
	Podrucje    int
	Nepotpunih  int
	ActiveNav   string

	SuccessMessage string
	ErrorMessage   string
	ViewAsBanner
}

// ShowPovezanost prikazuje ustave i crpne stanice s vodotocima i letvama
func (h *StructuresHandler) ShowPovezanost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, perms := h.base(r)
	podrucje, err := strconv.Atoi(r.URL.Query().Get("area"))
	if err != nil {
		podrucje = 0 // bez izbora ili nečitljivo: sva područja
	}
	data := PovezanostPageData{CurrentUser: u, Permissions: perms, Podrucje: podrucje, ActiveNav: "structures"}
	if a, err := h.userService.ListAreas(""); err == nil {
		data.Areas = a
	}
	data.Redovi = h.redoviPovezanosti(ctx, perms, podrucje)
	for _, red := range data.Redovi {
		if red.Nepotpun() {
			data.Nepotpunih++
		}
	}
	if err := h.tmplPovezanost.ExecuteTemplate(w, "povezanost_objekata.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// redoviPovezanosti su crpne stanice pa ustave područja, s nazivima
// vodotoka, letava i crpne stanice umjesto šifri
func (h *StructuresHandler) redoviPovezanosti(ctx context.Context, perms *models.UserPermissions, podrucje int) []RedakPovezanosti {
	vodotoci := map[string]string{}
	for _, v := range h.vodotoci(ctx) {
		vodotoci[v.Code] = v.Name
	}
	letve := map[string]string{}
	if sve, err := h.stationService.ListStations(ctx, "", "", "", false); err == nil {
		for _, s := range sve {
			letve[s.ID.String()] = s.Name
		}
	}
	var objekti []models.Structure
	for _, vrsta := range []string{models.StructureKindPumpingStation, models.StructureKindSluice} {
		if l, err := h.structureService.List(ctx, "", podrucje, vrsta, ""); err == nil {
			objekti = append(objekti, l...)
		}
	}
	nazivi := map[string]string{}
	for _, o := range objekti {
		nazivi[o.ID.String()] = o.Name
	}
	redovi := make([]RedakPovezanosti, 0, len(objekti))
	for _, o := range objekti {
		redovi = append(redovi, RedakPovezanosti{Objekt: o,
			Vodotok: vodotoci[o.WatercourseCode], VodotokDruge: vodotoci[o.VodotokDrugeStrane],
			Letva: letve[o.StationID], LetvaDruge: letve[o.StationDownID], USklopu: nazivi[o.USklopuID],
			MozeUredjivati: h.structureService.CanEdit(perms, &o)})
	}
	return redovi
}

// imaVodotok: je li šifra vodotoka u ponudi; inače se na obrascu objekta
// prikaže uz napomenu da nije u registru
func imaVodotok(v []models.Watercourse, code string) bool {
	for _, w := range v {
		if w.Code == code {
			return true
		}
	}
	return false
}

// najniziZaSifon je najniži vodostaj vanjske strane za vakumiranje sifona;
// čita se samo uz kvačicu „Ima sifon”
func najniziZaSifon(r *http.Request, imaSifon bool) *int {
	if !imaSifon {
		return nil
	}
	return parseThresholdInput(r.FormValue("sifon_min_cm")).Cm
}
