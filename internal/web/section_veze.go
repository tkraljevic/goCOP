package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"gocop/internal/hydro"
	"gocop/internal/models"
)

// PrijedlogVeze je objekt ili voda iz registra koju naziv retka dionice
// spominje. Obrazac ga nudi kao prijedlog; veza nastaje tek kad je čovjek
// ostavi i spremi dionicu.
type PrijedlogVeze struct {
	StructureID     string `json:"structure_id,omitempty"`
	WatercourseCode string `json:"watercourse_code,omitempty"`
	Naziv           string `json:"naziv"`
}

// rijecNaziva je riječ naziva i je li u zapisu skraćena točkom ("Dun.").
type rijecNaziva struct {
	r       string
	kratica bool
}

// rijeciNaziva rastavlja naziv na riječi bez dijakritike; „crpna stanica"
// svodi se na „cs", kako plan i piše.
func rijeciNaziva(s string) []rijecNaziva {
	s = hydro.FoldDiacritics(strings.ToLower(s))
	var out []rijecNaziva
	var b strings.Builder
	zatvori := func(kratica bool) {
		if b.Len() > 0 {
			out = append(out, rijecNaziva{b.String(), kratica})
			b.Reset()
		}
	}
	for _, c := range s {
		if unicode.IsLetter(c) || unicode.IsDigit(c) {
			b.WriteRune(c)
			continue
		}
		zatvori(c == '.')
	}
	zatvori(false)
	for i := 0; i+1 < len(out); i++ {
		if out[i].r == "crpna" && out[i+1].r == "stanica" {
			out = append(append(out[:i:i], rijecNaziva{r: "cs"}), out[i+2:]...)
		}
	}
	return out
}

// sadrziRijeci kaže stoji li niz riječi b u a jedna do druge, i gdje počinje.
func sadrziRijeci(a, b []rijecNaziva, isto func(zapis, registar rijecNaziva) bool) int {
	if len(b) == 0 {
		return -1
	}
	for i := 0; i+len(b) <= len(a); i++ {
		ok := true
		for j := range b {
			if !isto(a[i+j], b[j]) {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

// istaRijec uspoređuje objekte: naziv iz registra stoji u zapisu doslovce.
func istaRijec(zapis, registar rijecNaziva) bool { return zapis.r == registar.r }

// istaVoda uspoređuje vode, gdje zapis sklanja i krati: „p. Karašice" je
// potok Karašica, „Šarkanjskog Dun." Šarkanjski Dunavac. Sklonjeni oblik je
// korijen imena s padežnim nastavkom; „Dunavac" nije Dunav, jer „ac" nije
// padež, a „kraj" nije Krajna.
func istaVoda(zapis, registar rijecNaziva) bool {
	z, r := zapis.r, registar.r
	if z == r {
		return true
	}
	if zapis.kratica && len(z) >= 3 && strings.HasPrefix(r, z) {
		return true
	}
	if len(r) < 4 {
		return false
	}
	korijen := r
	switch {
	case strings.HasSuffix(r, "ac"): // Dunavac → Dunavca, nepostojano a
		korijen = r[:len(r)-2] + "c"
	case strings.ContainsRune("aeiou", rune(r[len(r)-1])):
		korijen = r[:len(r)-1]
	}
	if !strings.HasPrefix(z, korijen) {
		return false
	}
	return padezniNastavak[z[len(korijen):]]
}

// padezniNastavak su nastavci kojima se ime vode sklanja u zapisima plana
var padezniNastavak = map[string]bool{
	"": true, "a": true, "e": true, "i": true, "o": true, "u": true,
	"om": true, "em": true, "og": true, "oga": true, "oj": true, "ome": true, "omu": true,
	"ih": true, "im": true, "ima": true, "ama": true,
}

// uvodVode su riječi iza kojih u zapisu slijedi ime vode: „ušće u Vuku",
// „spoj s O.k. Karašica". Pred imenom bez njih ili vrste vode stoji nešto
// drugo — „Stara Drava", „Gaboška Vučica", „c.m. Čačinci-Bukvik" — pa to
// nije ta voda.
var uvodVode = map[string]bool{
	"usce": true, "utok": true, "sifon": true, "spoj": true,
	"u": true, "s": true, "sa": true, "iz": true, "ispod": true, "na": true, "do": true, "od": true,
}

// vrstaIspred čita vrstu vode iz riječi pred nazivom: „p.", „rijeke", „kanala".
func vrstaIspred(rijeci []rijecNaziva, i int) string {
	if i <= 0 {
		return ""
	}
	r := rijeci[i-1]
	if r.kratica {
		return hydro.NormalizeWaterKind(r.r)
	}
	for _, v := range []string{"rijeka", "potok", "kanal"} {
		if strings.HasPrefix(r.r, v) || strings.HasPrefix(r.r, strings.TrimSuffix(v, "a")) {
			return v
		}
	}
	return ""
}

// predloziVezu traži u nazivu retka objekt iz registra, a kad ga nema, vodu.
//
// Objekt se prepoznaje po nazivu iz registra koji stoji u zapisu („ustava
// Draž,Q=1,50m3/s" → Ustava Draž). Više pogodaka razrješava područje dionice
// pa duži naziv. Voda poddionice ne predlaže se — „r. Drava postaje granična
// rijeka" na Dravi nije veza. Kad izbor ostane nejasan, prijedloga nema.
func predloziVezu(naziv string, areaID int, areaText, vodaPoddionice string, objekti []models.Structure, vode []models.Watercourse) *PrijedlogVeze {
	zapis := rijeciNaziva(naziv)
	if len(zapis) == 0 {
		return nil
	}

	var najbolji *models.Structure
	bodovi := func(s *models.Structure) int {
		b := len(rijeciNaziva(s.Name)) * 2
		if s.AreaID == areaID {
			b++
		}
		return b
	}
	nejasno := false
	for i := range objekti {
		s := &objekti[i]
		if s.Kind == models.StructureKindEmbankment || s.Kind == models.StructureKindDam {
			continue
		}
		if sadrziRijeci(zapis, rijeciNaziva(s.Name), istaRijec) < 0 {
			continue
		}
		switch {
		case najbolji == nil || bodovi(s) > bodovi(najbolji):
			najbolji, nejasno = s, false
		case bodovi(s) == bodovi(najbolji):
			nejasno = true
		}
	}
	if najbolji != nil && !nejasno {
		return &PrijedlogVeze{StructureID: najbolji.ID.String(), Naziv: najbolji.Name}
	}
	if najbolji != nil {
		return nil
	}

	type pogodak struct {
		w      *models.Watercourse
		rijeci int
		vrsta  string
	}
	var pogoci []pogodak
	for i := range vode {
		w := &vode[i]
		if w.Code == vodaPoddionice {
			continue
		}
		ime := rijeciNaziva(w.Name)
		at := sadrziRijeci(zapis, ime, istaVoda)
		if at <= 0 {
			continue
		}
		vrsta := vrstaIspred(zapis, at)
		if vrsta == "" && !uvodVode[zapis[at-1].r] {
			continue
		}
		pogoci = append(pogoci, pogodak{w, len(ime), vrsta})
	}
	if len(pogoci) == 0 {
		return nil
	}
	// duže ime pokriva kraće: „Spojni kanal CS Draž-p. Karašica" nije potok Karašica
	najdulje := 0
	for _, p := range pogoci {
		najdulje = max(najdulje, p.rijeci)
	}
	index := map[string][]hydro.Candidate{}
	var kljuc, vrsta string
	for _, p := range pogoci {
		if p.rijeci != najdulje {
			continue
		}
		k := hydro.WatercourseKey(p.w.Name)
		if kljuc != "" && k != kljuc {
			return nil // dvije različite vode istog opsega — ne pogađa se
		}
		kljuc, vrsta = k, p.vrsta
		index[k] = append(index[k], hydro.Candidate{Code: p.w.Code, Kind: p.w.Kind, Qualifier: hydro.Qualifier(p.w.OfficialName)})
	}
	code := hydro.ResolveWatercourse(index, kljuc, vrsta, areaText)
	if code == "" {
		return nil
	}
	for _, p := range pogoci {
		if p.w.Code == code {
			return &PrijedlogVeze{WatercourseCode: code, Naziv: p.w.OfficialName}
		}
	}
	return nil
}

// HandlePrijedlogVezeAPI predlaže vezu za naziv retka objekta u obrascu dionice
func (h *SectionsHandler) HandlePrijedlogVezeAPI(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	areaID, _ := strconv.Atoi(q.Get("area"))
	var objekti []models.Structure
	if h.structureService != nil {
		objekti, _ = h.structureService.List(r.Context(), "", 0, "", "")
	}
	var vode []models.Watercourse
	if h.watercourseService != nil {
		vode, _ = h.watercourseService.ListWatercourses(r.Context(), "", "", false)
	}
	areaText := ""
	if areas, err := h.userService.ListAreas(""); err == nil {
		for _, a := range areas {
			if a.ID == areaID {
				areaText = a.Name + " " + a.VgiName + " " + a.Subcenter
			}
		}
	}
	p := predloziVezu(q.Get("naziv"), areaID, areaText, q.Get("voda"), objekti, vode)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"prijedlog": p})
}
