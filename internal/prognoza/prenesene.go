package prognoza

// Prenesena prognoza srpskih letvi. Bezdan, Apatin, Bogojevo i Bačka Palanka
// stoje nasuprot ili između naših letvi, pa im prognozu ne treba učiti
// lancem — za Apatin i Bačku Palanku satne povijesti ni nema — nego se prenosi
// s naše prognoze pravcem izmjerenim na dnevnim vrijednostima:
//
//	Bezdan   ← Batina             (740 m, druga obala; ±4 cm)
//	Apatin   ← Batina i Aljmaš    (između njih; ±7 cm)
//	Bogojevo ← Aljmaš i Dalj      (između njih; ±5 cm)
//	B. Palanka ← Ilok             (nasuprot)
//
// Odnos je manji od pogreške same prognoze, pa je prenesena prognoza gotovo
// jednako dobra kao naša na izvornim letvama — i može se mjeriti prema
// prognozi srpske službe za isti termin.

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// PreneseneLetve su letve s prenesenom prognozom i letve s kojih se prenosi.
var PreneseneLetve = []struct {
	Letva  string
	Izvori []string
}{
	{"bezdan", []string{"batina"}},
	{"apatin", []string{"batina", "aljmas"}},
	{"bogojevo", []string{"aljmas", "dalj"}},
	{"backa-palanka", []string{"ilok"}},
}

// PrijenosGodina je koliko godina dnevnih vrijednosti ulazi u odnos: korito se
// mijenja, pa stari odnos ne vrijedi zauvijek.
const PrijenosGodina = 10

// PrijenosNajmanjeDana je koliko zajedničkih dana odnos traži. Letva bez
// arhive (Bačka Palanka) uči se iz evidencije očitanja, koja je kratka.
const PrijenosNajmanjeDana = 7

// PrijenosNagibDana je koliko dana treba da se uči i nagib: kraći niz pokriva
// samo trenutnu vodu (Bačka Palanka, 11 dana male vode), pa bi nagib naučen na
// njemu pri velikoj vodi odlutao. Tada se uči samo pomak, uz nagib 1 — za
// letvu nasuprot našoj to je i fizikalno točno.
const PrijenosNagibDana = 180

// PrijenosIzEvidencijeDana je koliko dana operativnih očitanja dopunjuje arhivu
// (arhiva kasni za živim očitanjima nekoliko tjedana).
const PrijenosIzEvidencijeDana = 90

// Prijenos je naučen odnos letve prema izvorima: Koef[0] je odsječak, a
// Koef[i] nagib po Izvori[i-1].
type Prijenos struct {
	Letva  string
	Izvori []string
	Koef   []float64
	Rasap  float64 // standardno odstupanje ostatka, cm
	Dana   int
	Od     string // prvi dan u odnosu
}

// U računa vrijednost letve iz vrijednosti izvora.
func (p Prijenos) U(izvori []float64) float64 {
	v := p.Koef[0]
	for i, x := range izvori {
		v += p.Koef[i+1] * x
	}
	return v
}

// Opis je rečenica za karticu: odakle se prenosi i koliko odnos drži.
func (p Prijenos) Opis(imena map[string]string) string {
	var iz []string
	for _, l := range p.Izvori {
		if ime, ima := imena[l]; ima {
			l = ime
		}
		iz = append(iz, l)
	}
	return fmt.Sprintf("prognoza prenesena s naše (%s), odnos ±%.0f cm na %d dana", strings.Join(iz, " i "), p.Rasap, p.Dana)
}

// dnevneVrijednosti čita dnevne vodostaje letve: arhivu (izmjereno, bez
// preračuna) zadnjih PrijenosGodina godina, dopunjenu srednjacima operativnih
// očitanja zadnjih PrijenosIzEvidencijeDana dana.
func dnevneVrijednosti(ctx context.Context, arhiva, ocitanja *sql.DB, letva string, sada time.Time) map[string]float64 {
	out := map[string]float64{}
	if arhiva != nil {
		od := sada.AddDate(-PrijenosGodina, 0, 0).Unix()
		if rows, err := arhiva.QueryContext(ctx, `SELECT date(vrijeme, 'unixepoch'), vrijednost FROM spoj
			WHERE letva = ? AND velicina = 'vodostaj' AND korak = 'dnevni' AND izvor NOT LIKE 'preracun-%'
			AND vrijeme >= ?`, letva, od); err == nil {
			for rows.Next() {
				var d string
				var v float64
				if rows.Scan(&d, &v) == nil {
					out[d] = v
				}
			}
			rows.Close()
		}
	}
	if ocitanja != nil {
		od := sada.AddDate(0, 0, -PrijenosIzEvidencijeDana).UTC().Format("2006-01-02")
		if rows, err := ocitanja.QueryContext(ctx, `SELECT substr(r.measured_at, 1, 10), avg(r.level_cm)
			FROM readings r JOIN stations s ON s.id = r.station_id
			WHERE s.code = ? AND r.level_cm IS NOT NULL AND r.measured_at >= ?
			GROUP BY 1 HAVING count(*) >= 2`, letva, od); err == nil {
			for rows.Next() {
				var d string
				var v float64
				if rows.Scan(&d, &v) == nil {
					if _, ima := out[d]; !ima {
						out[d] = v
					}
				}
			}
			rows.Close()
		}
	}
	return out
}

// NamjestiPrijenos uči odnos letve prema izvorima najmanjim kvadratima na
// danima kad su sve mjerene.
func NamjestiPrijenos(ctx context.Context, arhiva, ocitanja *sql.DB, letva string, izvori []string, sada time.Time) (Prijenos, error) {
	cilj := dnevneVrijednosti(ctx, arhiva, ocitanja, letva, sada)
	iz := make([]map[string]float64, len(izvori))
	for i, l := range izvori {
		iz[i] = dnevneVrijednosti(ctx, arhiva, ocitanja, l, sada)
	}
	var dani []string
	for d := range cilj {
		ok := true
		for _, m := range iz {
			if _, ima := m[d]; !ima {
				ok = false
				break
			}
		}
		if ok {
			dani = append(dani, d)
		}
	}
	if len(dani) < PrijenosNajmanjeDana {
		return Prijenos{}, fmt.Errorf("%s: premalo zajedničkih dana s %s (%d)", letva, strings.Join(izvori, ", "), len(dani))
	}
	sort.Strings(dani)
	if len(dani) < PrijenosNagibDana {
		if len(izvori) != 1 {
			return Prijenos{}, fmt.Errorf("%s: za dva izvora treba barem %d dana (%d)", letva, PrijenosNagibDana, len(dani))
		}
		var zbroj float64
		for _, d := range dani {
			zbroj += cilj[d] - iz[0][d]
		}
		p := Prijenos{Letva: letva, Izvori: izvori, Koef: []float64{zbroj / float64(len(dani)), 1}, Dana: len(dani), Od: dani[0]}
		var ss float64
		for _, d := range dani {
			e := cilj[d] - p.U([]float64{iz[0][d]})
			ss += e * e
		}
		p.Rasap = math.Max(math.Sqrt(ss/float64(len(dani))), 2) // kratak niz ne smije obećati više od 2 cm
		return p, nil
	}
	n := len(izvori) + 1
	// normalne jednadžbe (XᵀX) b = Xᵀy
	a := make([][]float64, n)
	for i := range a {
		a[i] = make([]float64, n)
	}
	b := make([]float64, n)
	red := make([]float64, n)
	for _, d := range dani {
		red[0] = 1
		for i, m := range iz {
			red[i+1] = m[d]
		}
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				a[i][j] += red[i] * red[j]
			}
			b[i] += red[i] * cilj[d]
		}
	}
	koef, err := rijesi(a, b)
	if err != nil {
		return Prijenos{}, fmt.Errorf("%s: %w", letva, err)
	}
	p := Prijenos{Letva: letva, Izvori: izvori, Koef: koef, Dana: len(dani), Od: dani[0]}
	var ss float64
	vr := make([]float64, len(izvori))
	for _, d := range dani {
		for i, m := range iz {
			vr[i] = m[d]
		}
		e := cilj[d] - p.U(vr)
		ss += e * e
	}
	p.Rasap = math.Sqrt(ss / float64(len(dani)))
	return p, nil
}

// rijesi rješava mali linearni sustav Gaussovom eliminacijom.
func rijesi(a [][]float64, b []float64) ([]float64, error) {
	n := len(b)
	for c := 0; c < n; c++ {
		p := c
		for r := c + 1; r < n; r++ {
			if math.Abs(a[r][c]) > math.Abs(a[p][c]) {
				p = r
			}
		}
		if math.Abs(a[p][c]) < 1e-9 {
			return nil, fmt.Errorf("izvori su linearno ovisni")
		}
		a[c], a[p] = a[p], a[c]
		b[c], b[p] = b[p], b[c]
		for r := 0; r < n; r++ {
			if r == c {
				continue
			}
			f := a[r][c] / a[c][c]
			for k := c; k < n; k++ {
				a[r][k] -= f * a[c][k]
			}
			b[r] -= f * b[c]
		}
	}
	out := make([]float64, n)
	for i := range out {
		out[i] = b[i] / a[i][i]
	}
	return out, nil
}

// prijenosi drži naučene odnose jednom na dan, kao dnevni modeli.
var prijenosi struct {
	sync.Mutex
	dan    string
	odnosi map[string]Prijenos
}

func (o *Osvjezivac) prijenosiZaDanas(ctx context.Context) map[string]Prijenos {
	prijenosi.Lock()
	defer prijenosi.Unlock()
	dan := time.Now().UTC().Format("2006-01-02")
	if prijenosi.dan == dan {
		return prijenosi.odnosi
	}
	odnosi := map[string]Prijenos{}
	for _, pl := range PreneseneLetve {
		if p, err := NamjestiPrijenos(ctx, o.Arhiva, o.Ocitanja, pl.Letva, pl.Izvori, time.Now()); err == nil {
			odnosi[pl.Letva] = p
		}
	}
	prijenosi.dan, prijenosi.odnosi = dan, odnosi
	return odnosi
}

// PrijenosiZaDanas vraća naučene odnose, za opis na stranici.
func PrijenosiZaDanas() map[string]Prijenos {
	prijenosi.Lock()
	defer prijenosi.Unlock()
	out := map[string]Prijenos{}
	for k, v := range prijenosi.odnosi {
		out[k] = v
	}
	return out
}

// PomakPrijenosaNajvise je koliko rasapa odnosa smije iznositi pomak na
// startu: veće odstupanje mjerenja od odnosa je vjerojatnije kvar nego voda.
const PomakPrijenosaNajvise = 4.0

// prenesene računa prenesenu prognozu srpskih letvi iz izdane prognoze
// njihovih izvora: satne vrijednosti lanca i dnevne vrijednosti dnevnog
// modela. Prognoza kreće od stvarnog očitanja letve: odstupanje mjerenja od
// odnosa u satu izdanja drži se kroz cijelu prognozu.
func (o *Osvjezivac) prenesene(ctx context.Context, ishod *Ishod, od time.Time) ([]Izdana, []DnevnaIzdana, map[string]Izbor) {
	sada := ishod.Sada
	odnosi := o.prijenosiZaDanas(ctx)
	satne := map[string]map[int64]Izdana{}
	for _, i := range ishod.Izdane {
		if i.Velicina != "vodostaj" || i.Ciljni <= sada {
			continue
		}
		if satne[i.Letva] == nil {
			satne[i.Letva] = map[int64]Izdana{}
		}
		satne[i.Letva][i.Ciljni] = i
	}
	dnevne := map[string]map[int]DnevnaIzdana{}
	for _, d := range ishod.Dnevne {
		if dnevne[d.Letva] == nil {
			dnevne[d.Letva] = map[int]DnevnaIzdana{}
		}
		dnevne[d.Letva][d.Dan] = d
	}
	mjereno := func(letva string) (float64, bool) {
		n, err := o.ucitajNiz(ctx, Izvor{Letva: letva, Velicina: "vodostaj"}, od)
		if err != nil {
			return 0, false
		}
		z, ima := n.ZadnjiSatDo(sada)
		if !ima || sada-z > ZaostatakVrha {
			return 0, false
		}
		return n.U(z)
	}
	z := math.Sqrt2 * math.Erfinv(UdioURasponu) // raspon od UdioURasponu u jedinicama rasapa
	var izdane []Izdana
	var dnevneOut []DnevnaIzdana
	izbor := map[string]Izbor{}
	for _, pl := range PreneseneLetve {
		p, ima := odnosi[pl.Letva]
		if !ima {
			continue
		}
		// Pomak na startu: stvarno očitanje letve prema odnosu iz izvora.
		pomak := 0.0
		vt, imaT := mjereno(pl.Letva)
		izv := make([]float64, len(pl.Izvori))
		sviMjereni := imaT
		for i, l := range pl.Izvori {
			v, ok := mjereno(l)
			izv[i], sviMjereni = v, sviMjereni && ok
		}
		if sviMjereni {
			if d := vt - p.U(izv); math.Abs(d) <= PomakPrijenosaNajvise*p.Rasap {
				pomak = d
			}
		}
		if imaT {
			izdane = append(izdane, Izdana{Letva: pl.Letva, Velicina: "vodostaj", Izdano: sada, Ciljni: sada,
				Vrijednost: vt, Dolje: vt, Gore: vt, Model: o.Model})
		}
		sirina := func(polovine []float64) float64 {
			s := 0.0
			for i, h := range polovine {
				s += math.Abs(p.Koef[i+1]) * h
			}
			return math.Sqrt(s*s + z*z*p.Rasap*p.Rasap)
		}
		// Satno: sati u kojima svi izvori imaju prognozu.
		if prvi := satne[pl.Izvori[0]]; prvi != nil {
			for ciljni := range prvi {
				v := make([]float64, len(pl.Izvori))
				h := make([]float64, len(pl.Izvori))
				ok := true
				for i, l := range pl.Izvori {
					x, ima := satne[l][ciljni]
					if !ima {
						ok = false
						break
					}
					v[i], h[i] = x.Vrijednost, x.Raspon()
				}
				if !ok {
					continue
				}
				c := p.U(v) + pomak
				w := sirina(h)
				izdane = append(izdane, Izdana{Letva: pl.Letva, Velicina: "vodostaj", Izdano: sada, Ciljni: ciljni,
					Vrijednost: c, Dolje: c - w, Gore: c + w, Model: o.Model})
			}
		}
		// Dnevno: dani u kojima svi izvori imaju dnevnu prognozu.
		if prvi := dnevne[pl.Izvori[0]]; prvi != nil {
			for dan, d0 := range prvi {
				v := make([]float64, len(pl.Izvori))
				h := make([]float64, len(pl.Izvori))
				ok := true
				for i, l := range pl.Izvori {
					x, ima := dnevne[l][dan]
					if !ima {
						ok = false
						break
					}
					v[i], h[i] = x.Vrijednost, x.Raspon()
				}
				if !ok {
					continue
				}
				c := p.U(v) + pomak
				w := sirina(h)
				dnevneOut = append(dnevneOut, DnevnaIzdana{Letva: pl.Letva, Izdano: d0.Izdano, Dan: dan, Ciljni: d0.Ciljni,
					Vrijednost: c, Dolje: c - w, Gore: c + w, Model: d0.Model})
			}
		}
		// Inačica 1: zapisuje se samo izbor koji nije glavni račun, a
		// prenesena prognoza to i jest — stranica po njemu piše odakle je.
		izbor["preneseno:"+pl.Letva] = Izbor{Inacica: 1, Opis: fmt.Sprintf("%s|%.1f|%d|%.0f",
			strings.Join(pl.Izvori, ","), p.Rasap, p.Dana, pomak)}
	}
	return izdane, dnevneOut, izbor
}
