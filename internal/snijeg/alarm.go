package snijeg

// Upozorenje „Kiša i topljenje na puno snijega!” (Odluke 25. 11. 2026., iz
// provjere na zimama 1961.–2025.). Pali se za rijeku (Drava: A i B, Dunav:
// K i L) kad u nekom njezinu slivu ima neobično puno snijega za ovo doba
// godine, a kiša i otopljeni snijeg u idućih pet dana dosegnu vodu koja
// bude jednom u 2 godine.

import (
	"math"
	"time"

	"gocop/internal/prognoza"
)

// Snijeg „za ovo doba”: vrijednost dana uspoređuje se sa svim vrijednostima
// ranijih godina u danima ±7 oko istog dana u godini.
const (
	danaOkoDoba    = 7
	najmanjeZaDoba = 50.0 // mm vode; manje nije „puno” ni kad je rijetko
	udioZaDoba     = 0.9  // veći od 9 od 10 vrijednosti istog doba
	uzorakZaDoba   = 300  // najmanje vrijednosti istog doba (20 godina po 15 dana)
)

// UdioIspodZaDoba je udio vrijednosti povijesti istog doba (dani ±7 oko
// istog dana u godini, sve sezone prije sezone dana) manjih od v, i koliko
// je tih vrijednosti.
func UdioIspodZaDoba(povijest map[int64]float64, dan int64, v float64) (float64, int) {
	t := Dan(dan)
	sezona := prognoza.PocetakSezone(dan)
	ispod, n := 0, 0
	for g := Dan(prviDan).Year(); g <= t.Year(); g++ {
		sredina := time.Date(g, t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).Unix() / 86400
		for d := sredina - danaOkoDoba; d <= sredina+danaOkoDoba; d++ {
			x, ima := povijest[d]
			if !ima || d >= sezona {
				continue
			}
			n++
			if x < v {
				ispod++
			}
		}
	}
	if n == 0 {
		return 0, 0
	}
	return float64(ispod) / float64(n), n
}

// PunoZaDoba kaže je li snijeg v (mm vode) dana neobično velik za ovo doba:
// barem 50 mm i veći od 9 od 10 vrijednosti istog doba ranijih godina.
func PunoZaDoba(povijest map[int64]float64, dan int64, v float64) bool {
	if v < najmanjeZaDoba {
		return false
	}
	udio, n := UdioIspodZaDoba(povijest, dan, v)
	return n >= uzorakZaDoba && udio >= udioZaDoba
}

// Godine su povratna razdoblja vode iz Alpa: toliko kiše i otopljenog snijega
// u pet dana bude jednom u 2, 5 i 10 godina.
var Godine = [3]int{2, 5, 10}

// RazineVode su voda rijeke u pet dana (mm) koja bude jednom u 2, 5 i 10
// godina.
type RazineVode [3]float64

// najmanjeGodina je koliko punih godina treba za razine vode.
const najmanjeGodina = 20

// danaZaGodinu je koliko zbrojeva pet dana godina treba da bi bila puna.
const danaZaGodinu = 300

// VodaRijeke je dnevni niz rijeke iz nizova međuslivova (ključevi
// prognoza.OborinaKljuc): srednjak njezinih alpskih međuslivova po
// površini. Dan ulazi samo kad ga imaju svi.
func VodaRijeke(nizovi map[string]prognoza.DnevniNiz, rijeka string) map[int64]float64 {
	var slivovi []Sliv
	for _, s := range Alpski {
		if s.Rijeka == rijeka {
			slivovi = append(slivovi, s)
		}
	}
	out := map[int64]float64{}
	if len(slivovi) == 0 {
		return out
	}
	prvi := nizovi[prognoza.OborinaKljuc(slivovi[0].Oznaka)]
	for d := range prvi {
		var zbroj, povrsina float64
		svi := true
		for _, s := range slivovi {
			v, ima := nizovi[prognoza.OborinaKljuc(s.Oznaka)][d]
			if !ima {
				svi = false
				break
			}
			zbroj += s.Km2 * v
			povrsina += s.Km2
		}
		if svi {
			out[d] = zbroj / povrsina
		}
	}
	return out
}

// zbrojDana je zbroj niza u danima od–do (uključivo); bez ijednog dana ga
// nema.
func zbrojDana(niz map[int64]float64, od, do int64) (float64, bool) {
	var s float64
	for d := od; d <= do; d++ {
		v, ima := niz[d]
		if !ima {
			return 0, false
		}
		s += v
	}
	return s, true
}

// GodisnjiMaksimumi su najveći zbroj pet uzastopnih dana svake pune godine
// prije godine doGodine; zbroj pripada godini svog prvog dana, a godina je
// puna s barem 300 zbrojeva.
func GodisnjiMaksimumi(niz map[int64]float64, doGodine int) []float64 {
	maks, broj := map[int]float64{}, map[int]int{}
	for d := range niz {
		g := Dan(d).Year()
		if g >= doGodine {
			continue
		}
		s, ima := zbrojDana(niz, d, d+ProcjenaDana-1)
		if !ima {
			continue
		}
		maks[g] = math.Max(maks[g], s)
		broj[g]++
	}
	var out []float64
	for g, n := range broj {
		if n >= danaZaGodinu {
			out = append(out, maks[g])
		}
	}
	return out
}

// RazineVodeIz su razine vode iz godišnjih maksimuma; bez dovoljno punih
// godina ih nema.
func RazineVodeIz(maksimumi []float64) (RazineVode, bool) {
	if len(maksimumi) < najmanjeGodina {
		return RazineVode{}, false
	}
	var r RazineVode
	for i, t := range Godine {
		r[i] = math.Round(PovratnaRazina(maksimumi, float64(t)))
	}
	return r, true
}

// AlarmRijeke je upozorenje za rijeku: koliko vode (kiša i otopljeni
// snijeg) u idućih pet dana s Alpa stiže u nju, koliko je od toga otopljeni
// snijeg i koliko je to rijetko.
type AlarmRijeke struct {
	Rijeka    string
	Mm        float64 // voda idućih pet dana, mm
	Topljenje float64 // od toga otopljeni snijeg, mm
	Godina    int     // 2, 5 ili 10: najrjeđa razina koju voda doseže
}

// AlarmIz slaže upozorenje rijeke iz vode i same kiše po međuslivu za dane
// +1…+5 (VodaIzOborina) i razina vode. Kad koji dan fali ili voda ne
// doseže razinu za 2 godine, upozorenja nema.
func AlarmIz(rijeka string, voda, kisa map[string]prognoza.DnevniNiz, r RazineVode) *AlarmRijeke {
	w, ima := zbrojDana(VodaRijeke(voda, rijeka), 1, ProcjenaDana)
	if !ima || r[0] <= 0 || w < r[0] {
		return nil
	}
	a := &AlarmRijeke{Rijeka: rijeka, Mm: w}
	if k, ima := zbrojDana(VodaRijeke(kisa, rijeka), 1, ProcjenaDana); ima {
		a.Topljenje = math.Max(w-k, 0)
	}
	for i, prag := range r {
		if prag > 0 && w >= prag {
			a.Godina = Godine[i]
		}
	}
	return a
}

// VodaIzOborina je ulaz modela prognoze po međuslivu za dane +1…+5 od sata
// sada: voda je kiša i otopljeni snijeg (prognoza.OborineOkoSada sa
// snijegom), a kisa ista bez otopljenog, pa je razlika otopljeni snijeg.
func VodaIzOborina(iz *prognoza.OborinskiIzvor, sada int64) (voda, kisa map[string]prognoza.DnevniNiz, err error) {
	if iz == nil {
		return nil, nil, nil
	}
	sa := *iz
	sa.Kisomjeri, sa.Izmjerene = nil, nil
	if voda, err = prognoza.OborineOkoSada(&sa, sada, 0, ProcjenaDana); err != nil {
		return nil, nil, err
	}
	bez := sa.BezSnijega()
	if iz.Snijeg != nil {
		bez.Satne = samoKisa(iz.Satne, iz.Snijeg)
	}
	if kisa, err = prognoza.OborineOkoSada(bez, sada, 0, ProcjenaDana); err != nil {
		return nil, nil, err
	}
	return voda, kisa, nil
}

// samoKisa je satna kiša točaka: u danu s punim snijegom i temperaturom
// (prognoza.DaniIzSatnih) oborina bez snijega, kao u modelu snijega, a
// inače oborina kakva je pala.
func samoKisa(satne func(od, do int64) (map[string]map[int64]float64, error), snijeg prognoza.SatniSnijeg) func(od, do int64) (map[string]map[int64]float64, error) {
	return func(od, do int64) (map[string]map[int64]float64, error) {
		prvi, zadnji := od/24, do/24
		ob, err := satne(prvi*24, zadnji*24+23)
		if err != nil {
			return nil, err
		}
		sn, te, err := snijeg(prvi*24, zadnji*24+23)
		if err != nil {
			return nil, err
		}
		out := make(map[string]map[int64]float64, len(ob))
		for code, n := range ob {
			dani := prognoza.DaniIzSatnih(n, sn[code], te[code], prvi, zadnji)
			m := make(map[int64]float64, len(n))
			for h, v := range n {
				if dani[h/24].ImaSnijega {
					v = math.Max(v-sn[code][h], 0)
				}
				m[h] = v
			}
			out[code] = m
		}
		return out, nil
	}
}
