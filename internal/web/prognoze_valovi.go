package web

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gocop/internal/models"
)

// Provjera na poplavnim valovima, kako stoji na stranici „O prognozi”: čita
// se iz sažetka koji zapiše provjeri-valove (hindcast_valovi_sazetak.csv u
// mapi podataka), pa se brojke same obnove kad se provjera ponovi. Stranica
// ne računa ništa što bi se moglo shvatiti kao naš izbor: srednja apsolutna
// pogreška najavljenog vrha, pristranost i ista mjera za postojanost.

// DosegValova je jedan redak tablice: pogreška vrha na jednom dosegu.
type DosegValova struct {
	Doseg          int
	N              int
	MAE            float64 // srednja apsolutna pogreška vrha, cm
	Pristranost    float64 // srednja pogreška s predznakom; negativno = prognoza preniska
	MAEPostojanost float64 // ista mjera za pretpostavku da se ništa ne mijenja
}

// SkupinaValova su valovi jedne rijeke provjereni jednim modelom.
type SkupinaValova struct {
	Rijeka string
	Model  string // npr. „namješten na podacima od 2014.”
	Valova int
	Dosezi []DosegValova
}

// NajveciVal je jedan od najvećih valova, s pogreškom vrha po dosegu.
type NajveciVal struct {
	Val, Letva string
	Vrh        float64
	Pogreske   []string // po dosegu 24/48/72/96, s predznakom; „—” gdje prognoze nije bilo
}

// ProvjeraValova je sve što stranica o valovima pokazuje.
type ProvjeraValova struct {
	Datoteka  string
	Zapisano  string
	Valova    int
	Skupine   []SkupinaValova
	Najveci   []NajveciVal
	DoseziVrh []int
}

var doseziValova = []int{24, 48, 72, 96}

// sidraValova su letve po kojima se biraju najveći valovi rijeke.
var sidraValova = map[string][]string{"Dunav": {"batina"}, "Drava": {"belisce", "botovo"}}

var provjeraValova struct {
	sync.Mutex
	put   string
	kad   time.Time
	ishod *ProvjeraValova
}

// provjeraValovaIz čita sažetak iz mape podataka; nil kad ga nema. Rezultat
// se pamti dok se datoteka ne promijeni.
func provjeraValovaIz(dir string) *ProvjeraValova {
	if dir == "" {
		return nil
	}
	put := filepath.Join(dir, "hindcast_valovi_sazetak.csv")
	info, err := os.Stat(put)
	if err != nil {
		return nil
	}
	provjeraValova.Lock()
	defer provjeraValova.Unlock()
	if provjeraValova.put == put && provjeraValova.kad.Equal(info.ModTime()) {
		return provjeraValova.ishod
	}
	fh, err := os.Open(put)
	if err != nil {
		return nil
	}
	defer fh.Close()
	p, err := citajProvjeruValova(fh)
	if err != nil {
		return nil
	}
	p.Datoteka = filepath.Base(put)
	p.Zapisano = info.ModTime().In(models.Zagreb).Format("2.1.2006.")
	provjeraValova.put, provjeraValova.kad, provjeraValova.ishod = put, info.ModTime(), p
	return p
}

type vrhValova struct {
	val, letva, model string
	doseg             int
	izmjereno, vrh    float64
	postojanost       float64
}

func citajProvjeruValova(fh *os.File) (*ProvjeraValova, error) {
	r := csv.NewReader(fh)
	r.Comma = ';'
	r.FieldsPerRecord = -1
	zaglavlje, err := r.Read()
	if err != nil {
		return nil, err
	}
	st := map[string]int{}
	for i, z := range zaglavlje {
		st[strings.TrimPrefix(z, "\ufeff")] = i
	}
	for _, k := range []string{"vrsta", "val", "letva", "doseg_h", "izmjereno", "vrh_prognoze", "postojanost"} {
		if _, ima := st[k]; !ima {
			return nil, fmt.Errorf("sažetak nema stupac %s", k)
		}
	}
	var vrhovi []vrhValova
	for {
		red, err := r.Read()
		if err != nil {
			break
		}
		if red[st["vrsta"]] != "vrh" {
			continue
		}
		broj := func(k string) float64 { v, _ := strconv.ParseFloat(red[st[k]], 64); return v }
		d, _ := strconv.Atoi(red[st["doseg_h"]])
		v := vrhValova{val: red[st["val"]], letva: red[st["letva"]], doseg: d,
			izmjereno: broj("izmjereno"), vrh: broj("vrh_prognoze"), postojanost: broj("postojanost")}
		if i, ima := st["model"]; ima && i < len(red) {
			v.model = red[i]
		}
		vrhovi = append(vrhovi, v)
	}
	p := &ProvjeraValova{DoseziVrh: doseziValova}
	rijeka := func(val string) string { r, _, _ := strings.Cut(val, "-"); return r }

	type kljuc struct{ rijeka, model string }
	poSkupini := map[kljuc][]vrhValova{}
	var redom []kljuc
	sviValovi := map[string]bool{}
	for _, v := range vrhovi {
		k := kljuc{rijeka(v.val), v.model}
		if _, bilo := poSkupini[k]; !bilo {
			redom = append(redom, k)
		}
		poSkupini[k] = append(poSkupini[k], v)
		sviValovi[v.val] = true
	}
	p.Valova = len(sviValovi)
	sort.Slice(redom, func(i, j int) bool {
		if redom[i].rijeka != redom[j].rijeka {
			return redom[i].rijeka > redom[j].rijeka // Dunav prije Drave
		}
		return redom[i].model < redom[j].model
	})
	for _, k := range redom {
		s := SkupinaValova{Rijeka: k.rijeka, Model: k.model}
		valovi := map[string]bool{}
		for _, v := range poSkupini[k] {
			valovi[v.val] = true
		}
		s.Valova = len(valovi)
		for _, d := range doseziValova {
			var n int
			var apsol, zbroj, post float64
			for _, v := range poSkupini[k] {
				if v.doseg != d {
					continue
				}
				e := v.vrh - v.izmjereno
				n++
				apsol += math.Abs(e)
				zbroj += e
				post += math.Abs(v.postojanost - v.izmjereno)
			}
			if n == 0 {
				continue
			}
			s.Dosezi = append(s.Dosezi, DosegValova{Doseg: d, N: n, MAE: apsol / float64(n),
				Pristranost: zbroj / float64(n), MAEPostojanost: post / float64(n)})
		}
		p.Skupine = append(p.Skupine, s)
	}

	// Najveći valovi: po sidrenoj letvi rijeke, po izmjerenom vrhu.
	for _, rij := range []string{"Dunav", "Drava"} {
		for _, letva := range sidraValova[rij] {
			poValu := map[string]map[int]vrhValova{}
			for _, v := range vrhovi {
				if v.letva != letva || rijeka(v.val) != rij {
					continue
				}
				if poValu[v.val] == nil {
					poValu[v.val] = map[int]vrhValova{}
				}
				poValu[v.val][v.doseg] = v
			}
			var kandidati []NajveciVal
			for val, po := range poValu {
				var nv NajveciVal
				nv.Val, nv.Letva = val, letva
				for _, x := range po {
					nv.Vrh = x.izmjereno
					break
				}
				for _, d := range doseziValova {
					if x, ima := po[d]; ima {
						nv.Pogreske = append(nv.Pogreske, fmt.Sprintf("%+.0f", x.vrh-x.izmjereno))
					} else {
						nv.Pogreske = append(nv.Pogreske, "—")
					}
				}
				kandidati = append(kandidati, nv)
			}
			sort.Slice(kandidati, func(i, j int) bool { return kandidati[i].Vrh > kandidati[j].Vrh })
			if len(kandidati) > 6 {
				kandidati = kandidati[:6]
			}
			p.Najveci = append(p.Najveci, kandidati...)
		}
	}
	return p, nil
}
