package web

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"gocop/internal/models"
)

// IzvjesceLetveXLSX sastavlja Excelovu karticu ili historijat letve.
func (h *StationsHandler) IzvjesceLetveXLSX(w http.ResponseWriter, r *http.Request) {
	data, ok := h.podaciLetve(w, r)
	if !ok {
		return
	}
	sastavio := "goCOP"
	if data.CurrentUser != nil && data.CurrentUser.FullName != "" {
		sastavio = data.CurrentUser.FullName
	}
	dio := izvjesceKartica
	if r.URL.Query().Get("dio") == izvjesceHistorijat {
		dio = izvjesceHistorijat
	}
	iz := IzvjesceLetve{
		Dio: dio, Episodes: data.Episodes,
		Station: data.Station, Sastavio: sastavio, Kad: time.Now(),
		PragoviKote: data.PragoviKote, PragoviQ: data.PragoviQ,
		Krivulje: data.Krivulje, Nizovi: data.Nizovi, Profili: data.Profili, Profil: data.Profil,
		Crtez: data.Crtez, Sazetak: data.Sazetak, Spojevi: data.Spojevi,
		Zadnji: data.Zadnji, ZadnjiIzvor: data.ZadnjiIzvor,
		Valovi: data.ValoviSvi, ValoviZbroj: data.ValoviZbroj, ValoviNiz: data.ValoviNiz,
		Sections: data.Sections,
	}
	z := zaglavljeIzvozaLetve(models.Terms(), h.sektorLetve(r.Context(), data))
	posaljiXLSX(w, imeIzvjesca(data.Station, iz.Kad, dio), KnjigaLetve(iz, z))
}

func (h *StationsHandler) sektorLetve(ctx context.Context, data StationPageData) *models.Sector {
	if h.sektor == nil {
		return nil
	}
	for _, s := range data.Sections {
		if s.SectorID != "" {
			return h.sektor(ctx, s.SectorID)
		}
	}
	return nil
}

var nijeZaIme = regexp.MustCompile(`[^a-z0-9]+`)

func imeIzvjesca(st models.Station, kad time.Time, dio string) string {
	osnova := st.Code
	if osnova == "" {
		osnova = st.Name
	}
	osnova = strings.ToLower(bezDijakritike(osnova))
	osnova = strings.Trim(nijeZaIme.ReplaceAllString(osnova, "-"), "-")
	if osnova == "" {
		osnova = "postaja"
	}
	switch dio {
	case izvjesceHistorijat:
		osnova += "-historijat"
	case izvjesceOcitanja:
		osnova += "-ocitanja"
	}
	return "izvjesce-" + osnova + "-" + kad.In(models.Zagreb).Format("2006-01-02") + ".xlsx"
}

var zamjene = strings.NewReplacer(
	"č", "c", "ć", "c", "đ", "d", "š", "s", "ž", "z",
	"Č", "C", "Ć", "C", "Đ", "D", "Š", "S", "Ž", "Z")

func bezDijakritike(s string) string { return zamjene.Replace(s) }

// IzvjesceOcitanjaXLSX sastavlja Excelov pregled operativnih očitanja za
// isto razdoblje koje je korisnik odabrao na stranici.
func (h *ReadingsHandler) IzvjesceOcitanjaXLSX(w http.ResponseWriter, r *http.Request) {
	data, station, ok := h.podaciOcitanja(w, r)
	if !ok {
		return
	}
	if station == nil {
		http.NotFound(w, r)
		return
	}
	sastavio := "goCOP"
	if data.CurrentUser != nil && data.CurrentUser.FullName != "" {
		sastavio = data.CurrentUser.FullName
	}
	iz := IzvjesceLetve{
		Dio: izvjesceOcitanja, Station: *station, Sastavio: sastavio, Kad: time.Now(),
		Krivulje: data.Krivulje, Ocitanja: data.Svi, OcitanjaOpis: data.PogledOpis,
	}
	z := zaglavljeIzvozaLetve(models.Terms(), h.sektorZaLetvu(r.Context(), station))
	posaljiXLSX(w, imeIzvjesca(*station, iz.Kad, iz.Dio), KnjigaLetve(iz, z))
}
