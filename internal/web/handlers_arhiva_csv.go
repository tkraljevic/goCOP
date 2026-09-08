package web

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
)

// Izvoz spojenog niza u CSV, po godini.
//
// Izvozi se ono što program pokazuje — spojeni niz — a ne pojedini izvor, jer
// se ispravlja ono što se vidi. Uz svaku vrijednost ide izvor i odstupanje, pa
// se u tablici zna što se dira: ispravljati ovjereni podatak DHMZ-a nije isto
// što i ispravljati rekonstrukciju.

// HandleArhivaIzvoz šalje godinu spojenog niza kao CSV.
func (h *ReadingsHandler) HandleArhivaIzvoz(w http.ResponseWriter, r *http.Request) {
	a := h.arh()
	if a == nil {
		http.Error(w, "Arhiva nije dostupna", http.StatusNotFound)
		return
	}
	station, _ := h.gauge(r, r.PathValue("id"), "")
	if station == nil || station.Code == "" {
		http.NotFound(w, r)
		return
	}
	velicina := r.URL.Query().Get("v")
	if velicina == "" {
		velicina = "vodostaj"
	}
	korak := r.URL.Query().Get("korak")
	if korak != "satni" {
		korak = "dnevni"
	}
	god, _ := strconv.Atoi(r.URL.Query().Get("god"))
	if god < 1800 || god > 2200 {
		http.Error(w, "Godina nije zadana", http.StatusBadRequest)
		return
	}
	od := time.Date(god, 1, 1, 0, 0, 0, 0, time.UTC)
	do := od.AddDate(1, 0, 0).Add(-time.Second)
	vals, err := a.SpojRaspon(r.Context(), station.Code, velicina, korak, od, do, 20000, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// od najstarije prema novijoj: tako se čita i tako se ispravlja
	for i, j := 0, len(vals)-1; i < j; i, j = i+1, j-1 {
		vals[i], vals[j] = vals[j], vals[i]
	}

	ime := fmt.Sprintf("%s_%s_%s_%d.csv", station.Code, velicina, korak, god)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+ime+`"`)
	w.Write([]byte{0xEF, 0xBB, 0xBF}) // BOM, da Excel prepozna hrvatska slova

	cw := csv.NewWriter(w)
	cw.Comma = ';'
	defer cw.Flush()
	stupac := stupacVelicine(velicina)
	_ = cw.Write([]string{"vrijeme_utc", stupac, "izvor", "tocnost", "ispravak", "razlog"})
	dec := decimalaVelicine(velicina)
	for _, v := range vals {
		_ = cw.Write([]string{
			v.Kad.Format("2006-01-02 15:04:05"),
			unos(v.Vrijednost, dec),
			v.Izvor,
			unos(v.Tocnost, 0),
			"", // korisnik upisuje ispravljenu vrijednost ovdje
			"", // i razlog zašto
		})
	}
}

// stupacVelicine je naziv stupca s vrijednošću, isti kao u datotekama arhive.
func stupacVelicine(v string) string {
	switch v {
	case "protok":
		return "protok_m3s"
	case "temperatura":
		return "temperatura_c"
	case "koncentracija":
		return "koncentracija_gm3"
	case "pronos":
		return "pronos_t"
	}
	return "vodostaj_cm"
}

// RedakIspravka je jedan redak vraćene datoteke koji nešto mijenja.
type RedakIspravka struct {
	Kad    time.Time
	Staro  float64
	Novo   float64
	Izvor  string // izvor stare vrijednosti
	Razlog string
	Redak  int
	Greska string
}

// Promjena govori je li redak stvarno mijenja vrijednost.
func (r RedakIspravka) Promjena() bool { return r.Greska == "" && r.Staro != r.Novo }

// citajIspravke čita vraćenu datoteku i uspoređuje je sa spojenim nizom.
// Ništa se ne upisuje: vraća se popis onoga što bi se promijenilo, da čovjek
// vidi prije nego što potvrdi. Excel zna sam prepraviti zarez u točku ili
// datum u nešto treće, pa je taj pogled jedina obrana od tihog prepisivanja.
func citajIspravke(sadrzaj []byte, postojece map[int64]models.SpojenaVrijednost, dec int) ([]RedakIspravka, error) {
	cr := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(sadrzaj), "\ufeff")))
	cr.Comma = ';'
	cr.FieldsPerRecord = -1
	redci, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("datoteka nije čitljiva: %w", err)
	}
	if len(redci) < 2 {
		return nil, fmt.Errorf("datoteka nema nijedan redak s podacima")
	}
	glava := redci[0]
	stupac := func(naziv string) int {
		for i, g := range glava {
			if strings.EqualFold(strings.TrimSpace(g), naziv) {
				return i
			}
		}
		return -1
	}
	iVrijeme, iIspravak, iRazlog := stupac("vrijeme_utc"), stupac("ispravak"), stupac("razlog")
	if iVrijeme < 0 || iIspravak < 0 {
		return nil, fmt.Errorf("datoteci nedostaje stupac „vrijeme_utc“ ili „ispravak“ — je li izvezena odavde?")
	}

	var out []RedakIspravka
	for i, r := range redci[1:] {
		if len(r) <= iVrijeme || strings.TrimSpace(r[iVrijeme]) == "" {
			continue
		}
		novoTekst := ""
		if len(r) > iIspravak {
			novoTekst = strings.TrimSpace(r[iIspravak])
		}
		if novoTekst == "" {
			continue // redak nije diran
		}
		red := RedakIspravka{Redak: i + 2}
		if len(r) > iRazlog && iRazlog >= 0 {
			red.Razlog = strings.TrimSpace(r[iRazlog])
		}
		t, err := time.Parse("2006-01-02 15:04:05", strings.TrimSpace(r[iVrijeme]))
		if err != nil {
			red.Greska = "vrijeme nije čitljivo: " + r[iVrijeme]
			out = append(out, red)
			continue
		}
		red.Kad = t.UTC()
		v, ok := parseBroj(novoTekst)
		if !ok {
			red.Greska = "vrijednost nije broj: " + novoTekst
			out = append(out, red)
			continue
		}
		red.Novo = v
		staro, ima := postojece[t.UTC().Unix()]
		if !ima {
			red.Greska = "tog trenutka nema u arhivi"
			out = append(out, red)
			continue
		}
		red.Staro, red.Izvor = staro.Vrijednost, staro.Izvor
		if red.Razlog == "" {
			red.Greska = "ispravak bez razloga — upiši zašto se mijenja"
		}
		out = append(out, red)
	}
	return out, nil
}

// PregledIspravaka je ono što se pokaže prije nego što se išta upiše.
type PregledIspravaka struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	Station     *models.Station
	GaugeName   string
	Velicina    string
	Korak       string
	Godina      int
	Jedinica    string
	Decimala    int
	Redci       []RedakIspravka
	Promjena    int
	Greske      int
	Netaknuto   int
	Datoteka    string

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

// HandleArhivaUvoz prima vraćenu datoteku i pokazuje što bi se promijenilo.
// Ništa se ne upisuje dok čovjek ne potvrdi: Excel zna sam prepraviti zarez u
// točku ili datum u nešto treće, pa bi jedan pogrešan stupac tiho prepisao
// godinu.
func (h *ReadingsHandler) HandleArhivaUvoz(w http.ResponseWriter, r *http.Request) {
	a := h.arh()
	station, _ := h.gauge(r, r.PathValue("id"), "")
	if a == nil || station == nil || station.Code == "" {
		http.NotFound(w, r)
		return
	}
	back := "/readings/station/" + station.ID.String() + "#arhiva"
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	if perms == nil || !perms.IsGlobalAdmin {
		redirectWith(w, r, back, "error", "Ispravke arhive upisuje administrator")
		return
	}
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		redirectWith(w, r, back, "error", "Datoteka nije primljena: "+err.Error())
		return
	}
	f, zaglavlje, err := r.FormFile("datoteka")
	if err != nil {
		redirectWith(w, r, back, "error", "Odaberi datoteku")
		return
	}
	defer f.Close()
	sadrzaj, err := io.ReadAll(io.LimitReader(f, 32<<20))
	if err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}

	velicina, korak := r.FormValue("v"), r.FormValue("korak")
	if velicina == "" {
		velicina = "vodostaj"
	}
	if korak != "satni" {
		korak = "dnevni"
	}
	god, _ := strconv.Atoi(r.FormValue("god"))
	if god == 0 {
		redirectWith(w, r, back, "error", "Godina nije zadana")
		return
	}
	od := time.Date(god, 1, 1, 0, 0, 0, 0, time.UTC)
	do := od.AddDate(1, 0, 0).Add(-time.Second)
	vals, err := a.SpojRaspon(r.Context(), station.Code, velicina, korak, od, do, 20000, 0)
	if err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	postojece := make(map[int64]models.SpojenaVrijednost, len(vals))
	for _, v := range vals {
		postojece[v.Kad.Unix()] = v
	}

	redci, err := citajIspravke(sadrzaj, postojece, decimalaVelicine(velicina))
	if err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}

	u, p := h.base(r)
	data := PregledIspravaka{
		CurrentUser: u, Permissions: p, Station: station, GaugeName: station.Name,
		Velicina: velicina, Korak: korak, Godina: god,
		Jedinica: models.JedinicaVelicine(velicina), Decimala: decimalaVelicine(velicina),
		Redci: redci, Datoteka: zaglavlje.Filename,
		ActiveNav: "readings", ViewAsBanner: viewBanner(r),
	}
	for _, x := range redci {
		switch {
		case x.Greska != "":
			data.Greske++
		case x.Promjena():
			data.Promjena++
		default:
			data.Netaknuto++
		}
	}
	if err := h.tmplIspravci.ExecuteTemplate(w, "arhiva_ispravci.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleArhivaPotvrda upisuje ispravke koje je čovjek vidio i potvrdio.
func (h *ReadingsHandler) HandleArhivaPotvrda(w http.ResponseWriter, r *http.Request) {
	station, _ := h.gauge(r, r.PathValue("id"), "")
	if station == nil || h.ispravci == nil {
		http.NotFound(w, r)
		return
	}
	back := "/readings/station/" + station.ID.String() + "#arhiva"
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	user, _ := r.Context().Value(contextKeyUser).(*models.User)
	if perms == nil || !perms.IsGlobalAdmin || user == nil {
		redirectWith(w, r, back, "error", "Ispravke arhive upisuje administrator")
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, back, "error", "Neispravan zahtjev")
		return
	}
	velicina, korak := r.FormValue("v"), r.FormValue("korak")
	var ispravci []models.ArhivaIspravak
	for i, kad := range r.Form["kad"] {
		t, err := time.Parse(time.RFC3339, kad)
		if err != nil {
			continue
		}
		novo, ok1 := parseBroj(nth(r.Form["novo"], i))
		staro, ok2 := parseBroj(nth(r.Form["staro"], i))
		razlog := strings.TrimSpace(nth(r.Form["razlog"], i))
		if !ok1 || razlog == "" {
			continue
		}
		is := models.ArhivaIspravak{
			Letva: station.Code, Velicina: velicina, Korak: korak,
			Vrijeme: t.UTC(), Vrijednost: novo, Razlog: razlog, Ispravio: user.ID.String(),
		}
		if ok2 {
			s := staro
			is.Staro = &s
		}
		ispravci = append(ispravci, is)
	}
	if len(ispravci) == 0 {
		redirectWith(w, r, back, "error", "Nijedan ispravak nije potvrđen")
		return
	}
	n, err := h.ispravci.Spremi(r.Context(), ispravci)
	if err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	redirectWith(w, r, back, "success",
		fmt.Sprintf("Upisano %s. Arhiva je netaknuta — ispravci stoje uz nju i vide se u nizu.", ispravaka(n)))
}

func ispravaka(n int) string {
	switch {
	case n == 1:
		return "1 ispravak"
	case n < 5:
		return fmt.Sprintf("%d ispravka", n)
	}
	return fmt.Sprintf("%d ispravaka", n)
}

func nth(v []string, i int) string {
	if i < len(v) {
		return v[i]
	}
	return ""
}
