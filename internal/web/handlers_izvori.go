package web

import (
	"database/sql"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gocop/internal/arhiva"
	"gocop/internal/models"
)

// Izvori arhive: tko javlja, koliko mu se vjeruje i ulazi li u spojeni niz.
// Dok je taj popis stajao u kodu, niz iz izvora kojeg na njemu nema učitao bi
// se u bazu i nikad se ne bi vidio — pa se nije dalo razlikovati namjeru od
// propusta. Ovdje se vidi oboje, i tko je što odlučio.

type IzvoriHandler struct {
	arhivaPut func() string
	podaciDir func() string // zajedničko stablo s izvornim datotekama
	postavi   func(arhiva.Izvor) ([]string, error)
	tmpl      *template.Template
}

func NewIzvoriHandler(arhivaPut, podaciDir func() string, postavi func(arhiva.Izvor) ([]string, error),
	tmpl *template.Template) *IzvoriHandler {
	return &IzvoriHandler{arhivaPut: arhivaPut, podaciDir: podaciDir, postavi: postavi, tmpl: tmpl}
}

// IzvorURedu je izvor s onim što se o njemu vidi iz arhive i s diska.
type IzvorURedu struct {
	arhiva.Izvor
	Nizova int
	Zapisa int
	Letve  []string

	// Odakle se čitaju datoteke ovog izvora i što je ondje sada. Arhiva pamti
	// samo imena datoteka koje su u nju ušle; da se vidi je li stiglo nešto
	// novo — ili je disk otkvačen — mora se pogledati na disk.
	Stablo    string // stvarno stablo: vlastita mapa ili zajedničko
	Vlastito  bool
	Dostupno  bool
	Datoteka  int
	GreskaPut string
}

// ImaPodatke javlja stoji li iza izvora išta. Izvor bez ijednog niza je zapis
// o odluci, ne o podacima.
func (i IzvorURedu) ImaPodatke() bool { return i.Nizova > 0 }

// Neiskoristen je izvor koji ima podatke a ne ulazi u spoj — dakle nešto što
// leži u bazi i nigdje se ne vidi.
func (i IzvorURedu) Neiskoristen() bool { return i.Nizova > 0 && !i.Ukljucen }

type IzvoriPageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions

	Izvori     []IzvorURedu
	Zanemareno int    // zapisa koji leže u arhivi a ne ulaze u spoj
	PodaciDir  string // zajedničko stablo, ono koje vrijedi kad izvor nema svoje

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

func (h *IzvoriHandler) pageData(r *http.Request) IzvoriPageData {
	ctx := r.Context()
	u, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	return IzvoriPageData{
		CurrentUser: u, Permissions: perms,
		SuccessMessage: r.URL.Query().Get("success"), ErrorMessage: r.URL.Query().Get("error"),
		ActiveNav: "admin", ViewAsBanner: viewBanner(r),
	}
}

func (h *IzvoriHandler) ShowIzvori(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if data.Permissions == nil || !data.Permissions.IsGlobalAdmin {
		http.Error(w, "Izvore arhive uređuje administrator", http.StatusForbidden)
		return
	}
	if put := h.arhivaPut(); put == "" {
		data.ErrorMessage = "Nije poznato gdje arhiva stoji."
	} else if izvori, zanemareno, err := citajIzvoreZaStranicu(put, h.podaciDir()); err != nil {
		data.ErrorMessage = "Arhiva se ne čita: " + err.Error()
	} else {
		data.Izvori, data.Zanemareno = izvori, zanemareno
		data.PodaciDir = h.podaciDir()
	}
	if err := h.tmpl.ExecuteTemplate(w, "izvori.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// citajIzvoreZaStranicu spaja popis izvora s onim što o njima piše u nizovima.
// Namjerno ne broji vrijednosti u spoju: to je prolaz kroz šest milijuna
// redaka, a odgovor je ionako u stupcu "uključen".
func citajIzvoreZaStranicu(put, podaciDir string) ([]IzvorURedu, int, error) {
	db, err := sql.Open("sqlite", put+"?mode=ro&_pragma=busy_timeout(3000)")
	if err != nil {
		return nil, 0, err
	}
	defer db.Close()

	izvori, err := arhiva.Izvori(db)
	if err != nil {
		return nil, 0, err
	}
	type stat struct {
		nizova, zapisa int
		letve          []string
	}
	stanje := map[string]*stat{}
	rows, err := db.Query(`SELECT izvor, letva, zapisa FROM nizovi`)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var izvor, letva string
		var zapisa int
		if err := rows.Scan(&izvor, &letva, &zapisa); err != nil {
			return nil, 0, err
		}
		s := stanje[izvor]
		if s == nil {
			s = &stat{}
			stanje[izvor] = s
		}
		s.nizova++
		s.zapisa += zapisa
		if !sadrzi(s.letve, letva) {
			s.letve = append(s.letve, letva)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	out := make([]IzvorURedu, 0, len(izvori))
	zanemareno := 0
	for _, i := range izvori {
		red := IzvorURedu{Izvor: i}
		if s := stanje[i.Naziv]; s != nil {
			sort.Strings(s.letve)
			red.Nizova, red.Zapisa, red.Letve = s.nizova, s.zapisa, s.letve
		}
		if red.Neiskoristen() {
			zanemareno += red.Zapisa
		}
		red.Stablo, red.Vlastito = podaciDir, false
		if m := strings.TrimSpace(i.Mapa); m != "" {
			red.Stablo, red.Vlastito = m, true
		}
		red.Dostupno, red.Datoteka, red.GreskaPut = stanjeNaDisku(red.Stablo, i.Naziv)
		out = append(out, red)
	}
	return out, zanemareno, nil
}

// stanjeNaDisku broji datoteke tog izvora u stablu. Naziv datoteke je ugovor —
// letva_izvor_velicina_vrsta_razdoblje.csv — pa se izvor iz njega i prepoznaje.
func stanjeNaDisku(stablo, izvor string) (dostupno bool, datoteka int, greska string) {
	if stablo == "" {
		return false, 0, "nije zadano gdje datoteke stoje"
	}
	if st, err := os.Stat(stablo); err != nil || !st.IsDir() {
		return false, 0, "mapa nije dostupna"
	}
	puts, err := filepath.Glob(filepath.Join(stablo, "*", "*", "*_"+izvor+"_*.csv"))
	if err != nil {
		return true, 0, "putanja se ne da pročitati"
	}
	return true, len(puts), ""
}

func sadrzi(s []string, x string) bool {
	for _, v := range s {
		if v == x {
			return true
		}
	}
	return false
}

// SpremiIzvor mijenja jedan izvor i odmah ponovno spaja letve kojih se tiče.
// Spajanje ide u istom zahtjevu, jer polovična promjena — nova točnost a
// stari spoj — znači da stranica pokazuje jedno a arhiva drugo.
func (h *IzvoriHandler) SpremiIzvor(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if data.Permissions == nil || !data.Permissions.IsGlobalAdmin {
		http.Error(w, "Izvore arhive uređuje administrator", http.StatusForbidden)
		return
	}
	naziv := strings.TrimSpace(r.FormValue("naziv"))
	if naziv == "" {
		redirectWith(w, r, "/administracija/izvori", "error", "Izvor bez naziva.")
		return
	}
	tocnost, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(r.FormValue("tocnost")), ",", "."), 64)
	if err != nil || tocnost < 0 {
		redirectWith(w, r, "/administracija/izvori", "error", "Točnost mora biti broj, i ne može biti negativna.")
		return
	}
	red, err := strconv.Atoi(strings.TrimSpace(r.FormValue("red")))
	if err != nil || red < 1 {
		redirectWith(w, r, "/administracija/izvori", "error", "Red povjerenja mora biti cijeli broj veći od nule.")
		return
	}
	mapa := strings.TrimSpace(r.FormValue("mapa"))
	if mapa != "" {
		if st, err := os.Stat(mapa); err != nil || !st.IsDir() {
			redirectWith(w, r, "/administracija/izvori", "error",
				"Mape "+mapa+" nema, ili nije mapa. Ostavite prazno za zajedničko stablo arhive.")
			return
		}
	}
	i := arhiva.Izvor{
		Naziv: naziv, Tocnost: tocnost, Red: red,
		Ukljucen: r.FormValue("ukljucen") == "1",
		Mapa:     mapa,
		Napomena: strings.TrimSpace(r.FormValue("napomena")),
	}
	letve, err := h.postavi(i)
	if err != nil {
		redirectWith(w, r, "/administracija/izvori", "error", "Izvor se nije dao promijeniti: "+err.Error())
		return
	}
	poruka := fmt.Sprintf("Izvor %s je spremljen.", naziv)
	if n := len(letve); n > 0 {
		poruka = fmt.Sprintf("Izvor %s je spremljen; ponovno %s %d %s: %s.",
			naziv, uzBrojHR(n, "spojena", "spojene", "spojeno"), n,
			uzBrojHR(n, "letva", "letve", "letava"), strings.Join(letve, ", "))
	}
	redirectWith(w, r, "/administracija/izvori", "success", poruka)
}
