package web

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"gocop/internal/hydro"
	"gocop/internal/models"
	"gocop/internal/xlsxw"
)

// Popis dionica u Excelu: sve dionice koje popis pokazuje, složene po
// sektoru i branjenom području, s onim što se o dionici traži na prvi
// pogled — voda, obala, stacionaža, duljine, mjerodavni vodomjeri s
// pragovima, ugroženo područje i tko je vodi.

// DionicaUPopisu je redak izvoza: dionica s vodomjerima i rukovoditeljima
type DionicaUPopisu struct {
	models.Section
	Vodomjeri    string // "Batina (P 500 · R 600 · I 700 · IS 800); Vukovar (…)"
	Rukovoditelj string
	Zamjenik     string
}

// IzvoziPopisDionica piše popis kao .xlsx, uz iste filtre kao stranica
func (h *SectionsHandler) IzvoziPopisDionica(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sectorFilter := strings.TrimSpace(r.URL.Query().Get("sector"))
	searchQuery := strings.TrimSpace(r.URL.Query().Get("q"))
	areaFilter, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("area")))
	sve, err := h.sectionService.ListSections(sectorFilter, areaFilter, searchQuery)
	if err != nil {
		http.Error(w, "Greška pri dohvatu dionica: "+err.Error(), http.StatusInternalServerError)
		return
	}
	var redci []DionicaUPopisu
	for _, s := range sve {
		d := DionicaUPopisu{Section: s}
		if h.stationService != nil {
			if st, err := h.stationService.GetSectionStations(ctx, s.Code); err == nil {
				d.Vodomjeri = vodomjeriSPragovima(st)
			}
		}
		if det, err := h.sectionService.GetSectionWithDetails(s.Code); err == nil && det != nil {
			d.Rukovoditelj, d.Zamjenik = voditeljiDionice(det.Personnel)
		}
		redci = append(redci, d)
	}
	sektori, _ := h.userService.ListSectors()
	podrucja, _ := h.userService.ListAreas("")

	t := models.Terms()
	z := ZaglavljeIzvoza{Organizacija: t.OrgName, Datum: time.Now().In(models.Zagreb)}
	if t.HasLogo() && t.LogoMime == "image/png" {
		z.LogoPNG = t.Logo
	}
	ime := "dionice"
	if sectorFilter != "" {
		ime += "-" + strings.ToLower(sectorFilter)
		for _, sk := range sektori {
			if sk.ID == sectorFilter {
				z.Odjel, z.Centar = sk.VgoName, sk.CenterCop
			}
		}
	}
	if areaFilter > 0 {
		ime += "-bp" + strconv.Itoa(areaFilter)
	}
	filtar := ""
	if searchQuery != "" {
		filtar = "pretraga: " + searchQuery
	}
	posaljiXLSX(w, ime+".xlsx", KnjigaPopisaDionica(redci, sektori, podrucja, z, filtar))
}

// vodomjeriSPragovima piše letve dionice s pragovima u cm
func vodomjeriSPragovima(st []models.Station) string {
	var out []string
	for _, s := range st {
		var pr []string
		for _, x := range []struct {
			o string
			t models.Threshold
		}{{"P", s.Prep}, {"R", s.Regular}, {"I", s.Emergency}, {"IS", s.State}} {
			if x.t.Cm != nil {
				pr = append(pr, x.o+" "+strconv.Itoa(*x.t.Cm))
			}
		}
		n := s.Name
		if len(pr) > 0 {
			n += " (" + strings.Join(pr, " · ") + ")"
		}
		out = append(out, n)
	}
	return strings.Join(out, "; ")
}

// voditeljiDionice vadi rukovoditelja i zamjenika dionice iz zaduženih
func voditeljiDionice(osoblje []models.SectionOfficer) (ruk, zam string) {
	var r, zz []string
	for _, o := range osoblje {
		switch models.Role(o.Role) {
		case models.RoleSectionLeader:
			r = append(r, o.FullName)
		case models.RoleSectionDeputy:
			zz = append(zz, o.FullName)
		}
	}
	return strings.Join(r, "; "), strings.Join(zz, "; ")
}

// brojSifre daje brojeve iz šifre "B.34.10" → [34, 10], za prirodan poredak
func brojSifre(code string) []int {
	var out []int
	for _, d := range strings.Split(code, ".")[1:] {
		n, _ := strconv.Atoi(d)
		out = append(out, n)
	}
	return out
}

func manjaSifra(a, b string) bool {
	sa, sb := strings.SplitN(a, ".", 2), strings.SplitN(b, ".", 2)
	if sa[0] != sb[0] {
		return sa[0] < sb[0]
	}
	na, nb := brojSifre(a), brojSifre(b)
	for i := 0; i < len(na) && i < len(nb); i++ {
		if na[i] != nb[i] {
			return na[i] < nb[i]
		}
	}
	return len(na) < len(nb)
}

// KnjigaPopisaDionica slaže popis na A4 vodoravno: sektor, pa područje, pa
// dionice redom šifre; uz svako područje i sektor zbroj duljina
func KnjigaPopisaDionica(redci []DionicaUPopisu, sektori []models.Sector, podrucja []models.Area, z ZaglavljeIzvoza, filtar string) *xlsxw.Knjiga {
	k := &xlsxw.Knjiga{LogoPNG: z.LogoPNG}
	B := xlsxw.T
	const stupaca = 12 // A..L
	l := k.NoviList("Dionice")
	l.Vodoravno = true
	l.Sirine = []float64{9, 34, 12, 9, 15, 8, 8, 9, 30, 30, 16, 16}
	podnaslov := fmt.Sprintf("%d %s · stanje na dan %s", len(redci), uzBrojHR(len(redci), "dionica", "dionice", "dionica"), z.Datum.Format("02.01.2006."))
	if filtar != "" {
		podnaslov += " · " + filtar
	}
	pocetak := zaglavljeLista(l, z, "POPIS ŠTIĆENIH DIONICA PO SEKTORIMA I BRANJENIM PODRUČJIMA", podnaslov, stupaca)

	imeSektora := map[string]string{}
	for _, s := range sektori {
		imeSektora[s.ID] = s.Name
	}
	imePodrucja := map[int]models.Area{}
	for _, a := range podrucja {
		imePodrucja[a.ID] = a
	}
	sort.SliceStable(redci, func(i, j int) bool {
		a, b := redci[i], redci[j]
		if a.SectorID != b.SectorID {
			return a.SectorID < b.SectorID
		}
		if a.AreaID != b.AreaID {
			return a.AreaID < b.AreaID
		}
		return manjaSifra(a.Code, b.Code)
	})

	red := func(celije map[int]xlsxw.Celija, stil int, spojevi [][2]int) int {
		r := l.Redak()
		out := make([]xlsxw.Celija, stupaca)
		for c := range out {
			out[c] = B("", stil)
		}
		for c, cel := range celije {
			out[c] = cel
		}
		l.Dodaj(out...)
		for _, sp := range spojevi {
			l.Spoji(sp[0], r, sp[1], r)
		}
		return r
	}
	zaglavlje := func() {
		r := red(map[int]xlsxw.Celija{0: B("šifra", xlsxw.Zaglavlje), 1: B("opis dionice", xlsxw.Zaglavlje), 2: B("vodotok", xlsxw.Zaglavlje), 3: B("obala", xlsxw.Zaglavlje), 4: B("stacionaža", xlsxw.Zaglavlje),
			5: B("duljina (km)", xlsxw.Zaglavlje), 6: B("nasipa (km)", xlsxw.Zaglavlje), 7: B("nasipa / objekata", xlsxw.Zaglavlje), 8: B("mjerodavni vodomjeri (pragovi u cm)", xlsxw.Zaglavlje),
			9: B("ugroženo područje", xlsxw.Zaglavlje), 10: B("rukovoditelj dionice", xlsxw.Zaglavlje), 11: B("zamjenik", xlsxw.Zaglavlje)}, xlsxw.Zaglavlje, nil)
		l.Visina(r, 28)
	}
	zaglavlje()
	l.PonoviRetke(pocetak, pocetak)

	type zbroj struct {
		n            int
		duljina, nas float64
	}
	zbrojRed := func(oznaka string, zb zbroj) {
		red(map[int]xlsxw.Celija{0: B(oznaka, xlsxw.TablicaPod), 1: B(fmt.Sprintf("%d %s", zb.n, uzBrojHR(zb.n, "dionica", "dionice", "dionica")), xlsxw.TablicaPod),
			5: xlsxw.N(zb.duljina, xlsxw.TablicaBrojPod), 6: xlsxw.N(zb.nas, xlsxw.TablicaBrojPod)}, xlsxw.TablicaPod, [][2]int{{1, 4}, {7, 11}})
	}
	var zbS, zbP zbroj
	sektor, podrucje := "", -1
	for i, d := range redci {
		if d.SectorID != sektor {
			if i > 0 {
				zbrojRed("ukupno BP "+strconv.Itoa(podrucje), zbP)
				zbrojRed("ukupno sektor "+sektor, zbS)
			}
			sektor, podrucje, zbS, zbP = d.SectorID, d.AreaID, zbroj{}, zbroj{}
			ime := imeSektora[sektor]
			if ime == "" {
				ime = models.Terms().Sector + " " + sektor
			}
			l.Dodaj()
			l.Visina(l.Redak()-1, 6)
			r := red(map[int]xlsxw.Celija{0: B(strings.ToUpper(ime), xlsxw.Zaglavlje)}, xlsxw.Zaglavlje, [][2]int{{0, stupaca - 1}})
			l.Visina(r, 20)
			podrucje = -1
		}
		if d.AreaID != podrucje {
			if podrucje >= 0 {
				zbrojRed("ukupno BP "+strconv.Itoa(podrucje), zbP)
			}
			podrucje, zbP = d.AreaID, zbroj{}
			a := imePodrucja[d.AreaID]
			ime := fmt.Sprintf("BP %d", d.AreaID)
			if a.Name != "" {
				ime += " — " + a.Name
			}
			if a.Subcenter != "" {
				ime += " · " + a.Subcenter
			}
			red(map[int]xlsxw.Celija{0: B(ime, xlsxw.TablicaPod)}, xlsxw.TablicaPod, [][2]int{{0, stupaca - 1}})
		}
		p := d.FirstPart()
		voda := p.WatercourseName
		if voda == "" {
			voda = d.WatercourseName
		}
		if voda == "" {
			voda = p.WatercourseCode
		}
		if n := len(d.Parts); n > 1 {
			var vode []string
			vidjeno := map[string]bool{}
			for _, pp := range d.Parts {
				v := pp.WatercourseName
				if v == "" {
					v = pp.WatercourseCode
				}
				if v != "" && !vidjeno[v] {
					vidjeno[v] = true
					vode = append(vode, v)
				}
			}
			voda = strings.Join(vode, ", ")
		}
		obala := ""
		if p.Bank != "" {
			obala = hydro.BankLabel(p.Bank)
		}
		celije := map[int]xlsxw.Celija{0: B(d.Code, xlsxw.TablicaPod), 1: B(d.EffectiveDescription(), xlsxw.TablicaTekst), 2: B(voda, xlsxw.TablicaTekst), 3: B(obala, xlsxw.TablicaTekst), 4: B(p.RangeLabel(), xlsxw.TablicaTekst),
			7: B(fmt.Sprintf("%d / %d", d.EmbankmentCount(), d.ObjectCount()), xlsxw.TablicaSredina), 8: B(d.Vodomjeri, xlsxw.TablicaTekst), 9: B(d.ProtectedSummary(), xlsxw.TablicaTekst),
			10: B(d.Rukovoditelj, xlsxw.TablicaTekst), 11: B(d.Zamjenik, xlsxw.TablicaTekst)}
		if d.Length() > 0 {
			celije[5] = xlsxw.N(d.Length(), xlsxw.TablicaBroj)
		}
		if d.EmbankmentLength() > 0 {
			celije[6] = xlsxw.N(d.EmbankmentLength(), xlsxw.TablicaBroj)
		}
		r := red(celije, xlsxw.Tablica, nil)
		h := visinaTeksta(d.EffectiveDescription(), 40, 18, visinaTeksta(d.Vodomjeri, 36, 0, visinaTeksta(d.ProtectedSummary(), 36, 0, visinaTeksta(d.Rukovoditelj, 18, 0, visinaTeksta(d.Zamjenik, 18, 0, 0)))))
		l.Visina(r, h)
		zbP.n++
		zbP.duljina += d.Length()
		zbP.nas += d.EmbankmentLength()
		zbS.n++
		zbS.duljina += d.Length()
		zbS.nas += d.EmbankmentLength()
	}
	if len(redci) > 0 {
		zbrojRed("ukupno BP "+strconv.Itoa(podrucje), zbP)
		zbrojRed("ukupno sektor "+sektor, zbS)
	}
	napomenaLista(l, "Popis iz registra dionica programa goCOP. Pragovi P/R/I/IS su pripremno stanje, redovna obrana, izvanredna obrana i izvanredno stanje na mjerodavnom vodomjeru, u cm. Duljine su iz Privitka Državnog plana obrane od poplava.", stupaca, 24)
	return k
}
