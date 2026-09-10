package web

import (
	"bytes"
	"context"
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
		Sections:  data.Sections,
		Zaglavlje: zaglavljeIzvjesca(models.Terms(), h.sektorLetve(r.Context(), data)),
	}

	// Karta se slaže samo za izvješće kartice, jer ondje i stoji. Traži mrežu;
	// bez nje dokument nastaje bez karte.
	if dio == izvjesceKartica && data.Station.ImaKoordinate() && h.karta != nil {
		iz.Karta = slozKartu(r.Context(), h.karta(),
			*data.Station.Latitude, *data.Station.Longitude, odakleZahtjev(r))
	}

	var b bytes.Buffer
	if err := iz.Sastavi().Zapisi(&b); err != nil {
		http.Error(w, "izvješće se nije dalo sastaviti: "+err.Error(), http.StatusInternalServerError)
		return
	}

	ime := imeIzvjesca(data.Station, iz.Kad, dio)
	w.Header().Set("Content-Type",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	w.Header().Set("Content-Disposition", `attachment; filename="`+ime+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(b.Len()))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(b.Bytes())
}

// sektorLetve nalazi centar iz kojeg dokument izlazi: sektor dionica na
// kojima je letva mjerodavna. Letva može biti mjerodavna za više dionica, ali
// sve su u istom sektoru — dionice su ustrojene po sektorima, ne po vodama.
// Bez dionica ili bez zapisa sektora izvješće nastaje bez memoranduma.
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

// odakleZahtjev gradi adresu stranice koja je zatražila kartu. Poslužitelji
// pločica traže Referer, a poštenije je poslati stranicu koja je zahtjev
// doista izazvala nego izmišljenu adresu.
func odakleZahtjev(r *http.Request) string {
	shema := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		shema = "https"
	}
	if r.Host == "" {
		return ""
	}
	return shema + "://" + r.Host + "/"
}

var nijeZaIme = regexp.MustCompile(`[^a-z0-9]+`)

// imeIzvjesca gradi ime datoteke bez dijakritike i razmaka: ide kroz e-poštu i
// dijeljene mape, gdje "Izvješće — Batina.docx" zna doći s pokvarenim imenom.
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
	// Dva izvješća iste letve ne smiju dobiti isto ime: u mapi preuzimanja bi
	// se drugo tiho zvalo „…(1)" ili prepisalo prvo.
	switch dio {
	case izvjesceHistorijat:
		osnova += "-historijat"
	case izvjesceOcitanja:
		osnova += "-ocitanja"
	}
	return "izvjesce-" + osnova + "-" + kad.In(models.Zagreb).Format("2006-01-02") + ".docx"
}

var zamjene = strings.NewReplacer(
	"č", "c", "ć", "c", "đ", "d", "š", "s", "ž", "z",
	"Č", "C", "Ć", "C", "Đ", "D", "Š", "S", "Ž", "Z")

func bezDijakritike(s string) string { return zamjene.Replace(s) }

// IzvjesceOcitanjaDocx sastavlja Wordov dokument o očitanjima letve — ono što
// stoji na operativnoj stranici, za odabrano razdoblje. Vodne građevine ga
// nemaju: izvješće je o letvi.
func (h *ReadingsHandler) IzvjesceOcitanjaDocx(w http.ResponseWriter, r *http.Request) {
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
		Dio: izvjesceOcitanja, Station: *station,
		Sastavio: sastavio, Kad: time.Now(),
		Krivulje:     data.Krivulje,
		Ocitanja:     data.Svi,
		OcitanjaOpis: data.PogledOpis,
		Zaglavlje:    zaglavljeIzvjesca(models.Terms(), h.sektorZaLetvu(r.Context(), station)),
	}
	posaljiIzvjesce(w, iz, *station)
}

// posaljiIzvjesce sastavlja dokument u memoriji pa ga tek onda šalje: kad bi se
// pisao ravno u odgovor, greška usred sastavljanja ostavila bi korisniku pola
// datoteke koju Word odbija otvoriti, a poslužitelj bi to prijavio kao uspjeh.
func posaljiIzvjesce(w http.ResponseWriter, iz IzvjesceLetve, st models.Station) {
	var b bytes.Buffer
	if err := iz.Sastavi().Zapisi(&b); err != nil {
		http.Error(w, "izvješće se nije dalo sastaviti: "+err.Error(), http.StatusInternalServerError)
		return
	}
	ime := imeIzvjesca(st, iz.Kad, iz.Dio)
	w.Header().Set("Content-Type",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	w.Header().Set("Content-Disposition", `attachment; filename="`+ime+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(b.Len()))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(b.Bytes())
}
