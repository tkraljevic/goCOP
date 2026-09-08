package web

import (
	"bytes"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
)

// IzvjesceLetveDocx sastavlja Wordov dokument o letvi i šalje ga na preuzimanje.
//
// Dokument se sastavlja u memoriji pa tek onda šalje: kad bi se pisao ravno u
// odgovor, greška usred sastavljanja ostavila bi korisniku pola datoteke koju
// Word odbija otvoriti, a poslužitelj bi to prijavio kao uspjeh.
func (h *StationsHandler) IzvjesceLetveDocx(w http.ResponseWriter, r *http.Request) {
	data, ok := h.podaciLetve(w, r)
	if !ok {
		return
	}
	sastavio := "goCOP"
	if data.CurrentUser != nil && data.CurrentUser.FullName != "" {
		sastavio = data.CurrentUser.FullName
	}
	iz := IzvjesceLetve{
		Station: data.Station, Sastavio: sastavio, Kad: time.Now(),
		PragoviKote: data.PragoviKote, PragoviQ: data.PragoviQ,
		Krivulje: data.Krivulje, Profili: data.Profili, Profil: data.Profil,
		Crtez: data.Crtez, Sazetak: data.Sazetak, Spojevi: data.Spojevi,
		Zadnji: data.Zadnji, ZadnjiIzvor: data.ZadnjiIzvor,
		Valovi: data.ValoviSvi, ValoviZbroj: data.ValoviZbroj, ValoviNiz: data.ValoviNiz,
		Sections: data.Sections,
	}

	var b bytes.Buffer
	if err := iz.Sastavi().Zapisi(&b); err != nil {
		http.Error(w, "izvješće se nije dalo sastaviti: "+err.Error(), http.StatusInternalServerError)
		return
	}

	ime := imeIzvjesca(data.Station, iz.Kad)
	w.Header().Set("Content-Type",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	w.Header().Set("Content-Disposition", `attachment; filename="`+ime+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(b.Len()))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(b.Bytes())
}

var nijeZaIme = regexp.MustCompile(`[^a-z0-9]+`)

// imeIzvjesca gradi ime datoteke bez dijakritike i razmaka: ide kroz e-poštu i
// dijeljene mape, gdje "Izvješće — Batina.docx" zna doći s pokvarenim imenom.
func imeIzvjesca(st models.Station, kad time.Time) string {
	osnova := st.Code
	if osnova == "" {
		osnova = st.Name
	}
	osnova = strings.ToLower(bezDijakritike(osnova))
	osnova = strings.Trim(nijeZaIme.ReplaceAllString(osnova, "-"), "-")
	if osnova == "" {
		osnova = "postaja"
	}
	return "izvjesce-" + osnova + "-" + kad.In(models.Zagreb).Format("2006-01-02") + ".docx"
}

var zamjene = strings.NewReplacer(
	"č", "c", "ć", "c", "đ", "d", "š", "s", "ž", "z",
	"Č", "C", "Ć", "C", "Đ", "D", "Š", "S", "Ž", "Z")

func bezDijakritike(s string) string { return zamjene.Replace(s) }
