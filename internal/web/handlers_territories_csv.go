package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"gocop/internal/models"
)

// Izvoz i uvoz teritorijalnih jedinica u CSV. Županije, gradovi i općine te
// naselja mijenjaju se rijetko, pa se najlakše održavaju u tablici: izvezi,
// uredi u Excelu, uvezi natrag. Redak s brojem koji već postoji se osvježi,
// redak bez broja se doda; ništa se ne briše.

// ExportCountiesCSV daje županije kao CSV
func (h *TerritoriesHandler) ExportCountiesCSV(w http.ResponseWriter, r *http.Request) {
	counties, err := h.territoryService.ListCounties(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rows := [][]string{{"Broj", "Šifra", "Naziv", "Sjedište", "Župan", "Površina km2", "Stanovnika", "E-pošta", "Telefon", "Web"}}
	for _, c := range counties {
		rows = append(rows, []string{strconv.Itoa(c.ID), c.Code, c.Name, c.Seat, c.Prefect,
			strconv.Itoa(c.AreaSqKm), strconv.Itoa(c.Population), c.Email, c.Phone, c.Website})
	}
	writeCSV(w, "zupanije.csv", rows)
}

// ExportMunicipalitiesCSV daje gradove i općine kao CSV
func (h *TerritoriesHandler) ExportMunicipalitiesCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	muns, err := h.territoryService.ListMunicipalities(ctx, 0, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	names := h.countyNames(r)
	rows := [][]string{{"Broj", "Županija broj", "Županija", "Naziv", "Vrsta", "Titula čelnika", "Čelnik", "Poštanski broj",
		"Površina km2", "Stanovnika", "E-pošta", "Telefon", "Web"}}
	for _, m := range muns {
		rows = append(rows, []string{strconv.Itoa(m.ID), strconv.Itoa(m.CountyID), names[m.CountyID], m.Name, m.Type,
			m.HeadTitle, m.HeadName, m.PostalCode, csvFloat(m.AreaSqKm), strconv.Itoa(m.Population),
			m.Email, m.Phone, m.Website})
	}
	writeCSV(w, "gradovi-i-opcine.csv", rows)
}

// ExportSettlementsCSV daje naselja kao CSV
func (h *TerritoriesHandler) ExportSettlementsCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sets, err := h.territoryService.ListSettlements(ctx, 0, 0, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	muns, _ := h.territoryService.ListMunicipalities(ctx, 0, "", "")
	munName := map[int]string{}
	for _, m := range muns {
		munName[m.ID] = m.Name
	}
	names := h.countyNames(r)
	rows := [][]string{{"Broj", "Grad/općina broj", "Grad/općina", "Županija broj", "Županija", "Naziv", "Poštanski broj", "Stanovnika"}}
	for _, s := range sets {
		rows = append(rows, []string{strconv.Itoa(s.ID), strconv.Itoa(s.MunicipalityID), munName[s.MunicipalityID],
			strconv.Itoa(s.CountyID), names[s.CountyID], s.Name, s.PostalCode, strconv.Itoa(s.Population)})
	}
	writeCSV(w, "naselja.csv", rows)
}

// countyNames su nazivi županija po broju, za stupac u izvozu
func (h *TerritoriesHandler) countyNames(r *http.Request) map[int]string {
	out := map[int]string{}
	counties, err := h.territoryService.ListCounties(r.Context())
	if err != nil {
		return out
	}
	for _, c := range counties {
		out[c.ID] = c.Name
	}
	return out
}

// HandleImportTerritoriesCSV uvozi županije, gradove i općine ili naselja
func (h *TerritoriesHandler) HandleImportTerritoriesCSV(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		redirectWith(w, r, "/territories", "error", "Neispravan zahtjev ili prevelika datoteka")
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	file, _, err := r.FormFile("csv")
	if err != nil {
		redirectWith(w, r, "/territories", "error", "Odaberite CSV datoteku")
		return
	}
	defer file.Close()
	rows, err := readCSV(file)
	if err != nil {
		redirectWith(w, r, "/territories", "error", err.Error())
		return
	}
	if len(rows) > 0 && strings.EqualFold(cell(rows[0], 0), "Broj") {
		rows = rows[1:]
	}
	ctx := r.Context()
	var added, updated int
	var errs []string
	for i, row := range rows {
		id, _ := strconv.Atoi(cell(row, 0))
		var err error
		switch r.FormValue("kind") {
		case "opcine":
			m := &models.Municipality{ID: id, CountyID: atoi(cell(row, 1)), Name: cell(row, 3), Type: cell(row, 4),
				HeadTitle: cell(row, 5), HeadName: cell(row, 6), PostalCode: cell(row, 7),
				AreaSqKm: atof(cell(row, 8)), Population: atoi(cell(row, 9)),
				Email: cell(row, 10), Phone: cell(row, 11), Website: cell(row, 12)}
			if id > 0 {
				err = h.territoryService.UpdateMunicipality(ctx, perms, m)
			} else {
				err = h.territoryService.CreateMunicipality(ctx, perms, m)
			}
		case "naselja":
			s := &models.Settlement{ID: id, MunicipalityID: atoi(cell(row, 1)), CountyID: atoi(cell(row, 3)),
				Name: cell(row, 5), PostalCode: cell(row, 6), Population: atoi(cell(row, 7))}
			if s.CountyID == 0 && s.MunicipalityID > 0 {
				if m, e := h.territoryService.GetMunicipalityByID(ctx, s.MunicipalityID); e == nil && m != nil {
					s.CountyID = m.CountyID
				}
			}
			if id > 0 {
				err = h.territoryService.UpdateSettlement(ctx, perms, s)
			} else {
				err = h.territoryService.CreateSettlement(ctx, perms, s)
			}
		default:
			c := &models.County{ID: id, Code: cell(row, 1), Name: cell(row, 2), Seat: cell(row, 3), Prefect: cell(row, 4),
				AreaSqKm: atoi(cell(row, 5)), Population: atoi(cell(row, 6)), Email: cell(row, 7), Phone: cell(row, 8),
				Website: cell(row, 9)}
			if id > 0 {
				err = h.territoryService.UpdateCounty(ctx, perms, c)
			} else {
				err = h.territoryService.CreateCounty(ctx, perms, c)
			}
		}
		if err != nil {
			errs = append(errs, fmt.Sprintf("redak %d: %s", i+1, err.Error()))
			continue
		}
		if id > 0 {
			updated++
		} else {
			added++
		}
	}
	msg := fmt.Sprintf("Uvoz: %d novih, %d osvježenih.", added, updated)
	if len(errs) > 0 {
		if len(errs) > 5 {
			errs = append(errs[:5], fmt.Sprintf("… i još %d", len(errs)-5))
		}
		redirectWith(w, r, "/territories", "error", msg+" Preskočeno: "+strings.Join(errs, "; "))
		return
	}
	redirectWith(w, r, "/territories", "success", msg)
}
