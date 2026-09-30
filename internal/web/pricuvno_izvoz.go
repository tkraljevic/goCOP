package web

// Pričuvni izračun u Excelu: kad prognoza ne radi, dežurni upiše mađarske i
// srpske prognoze letvi nasuprot i uz naše, a Excel iz njih računa naše
// letve pravcima koje je program upisao pri preuzimanju. Radi bez programa i
// bez mreže; program samo unaprijed popuni što zna.

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"gocop/internal/models"
	"gocop/internal/prognoza"
	"gocop/internal/xlsxw"
)

// SetPricuvno daje izvozu račun veza i predupis tuđih prognoza.
func (h *PrognozeHandler) SetPricuvno(f func(ctx context.Context) (prognoza.PricuvniPodaci, bool)) {
	h.pricuvno = f
}

// Stupci su isti na listovima „Naše postaje” i „Unos”: dan d je u stupcu
// pricuvniStupac+d, pa formule na oba lista gledaju isti stupac.
const pricuvniStupac = 3

var daniTjedna = []string{"ned", "pon", "uto", "sri", "čet", "pet", "sub"}

// IzvoziPricuvno šalje pričuvni Excel.
func (h *PrognozeHandler) IzvoziPricuvno(w http.ResponseWriter, r *http.Request) {
	var p prognoza.PricuvniPodaci
	ok := false
	if h.pricuvno != nil {
		p, ok = h.pricuvno(r.Context())
	}
	if !ok {
		http.Error(w, "Pričuvni izračun nije uključen na ovom čvoru (nema arhive vodostaja).", http.StatusNotFound)
		return
	}
	postaje := h.postaje(r.Context())
	ime := func(kod string) string {
		if st, ima := postaje[kod]; ima && st.Name != "" {
			return st.Name
		}
		return kod
	}
	k := knjigaPricuvno(p, ime)
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="pricuvni_izracun_%s.xlsx"`,
		p.Sada.In(models.Zagreb).Format("2006-01-02_15h")))
	if err := k.Zapisi(w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// imeUlaza je ime tuđe letve ili „X — mađarska prognoza” za našu letvu.
func imeUlaza(kod string, ime func(string) string) string {
	if strings.HasPrefix(kod, prognoza.Madjarska) {
		return ime(strings.TrimPrefix(kod, prognoza.Madjarska)) + " — mađarska prognoza"
	}
	return ime(kod)
}

// PricuvniExcel slaže pričuvni Excel iz podataka; ime pretvara šifru letve u
// naziv. Izvan poslužitelja služi alatima i provjeri.
func PricuvniExcel(p prognoza.PricuvniPodaci, ime func(string) string) *xlsxw.Knjiga {
	return knjigaPricuvno(p, ime)
}

func knjigaPricuvno(p prognoza.PricuvniPodaci, ime func(string) string) *xlsxw.Knjiga {
	k := &xlsxw.Knjiga{}
	nase := k.NoviList("Naše postaje")
	unos := k.NoviList("Unos")
	veze := k.NoviList("Veze")
	kako := k.NoviList("Kako")

	naslovDana := func() []xlsxw.Celija {
		var c []xlsxw.Celija
		for d, dan := range p.Dani {
			if d == 0 {
				c = append(c, xlsxw.T("danas "+dan.Format("2.1."), xlsxw.Zaglavlje))
				continue
			}
			c = append(c, xlsxw.T(daniTjedna[dan.Weekday()]+" "+dan.Format("2.1."), xlsxw.Zaglavlje))
		}
		return c
	}
	izdano := p.Sada.In(models.Zagreb).Format("2.1.2006. u 15:04")

	// --- Unos: tuđe letve po danima, u 7 h
	unos.Dodaj(xlsxw.T("Pričuvni izračun — unos tuđih prognoza", xlsxw.Naslov))
	unos.Dodaj(xlsxw.T("Vodostaj u cm na nuli letve, u 7 h. Žuta polja su za unos; program ih je popunio onim što je znao "+
		izdano+". Nepoznato ostavi prazno. Mađarska prognoza: hydroinfo.hu; srpska: hidmet.gov.rs.", xlsxw.Napomena))
	unos.Spoji(0, 1, pricuvniStupac+prognoza.PricuvniDana, 1)
	unos.Visina(1, 30)
	unos.Dodaj()
	unos.Dodaj(append([]xlsxw.Celija{xlsxw.T("Letva", xlsxw.Zaglavlje), xlsxw.T("Država", xlsxw.Zaglavlje),
		xlsxw.T("Upisano iz", xlsxw.Zaglavlje)}, naslovDana()...)...)
	adresaUnosa := map[string]int{} // letva → redak
	for _, u := range prognoza.PricuvniUlazi {
		red := unos.Redak()
		adresaUnosa[u.Letva] = red
		drzava := "RS"
		if u.Izvor == prognoza.Podrijetlo {
			drzava = "HU"
		}
		if strings.HasPrefix(u.Letva, prognoza.Madjarska) {
			drzava = "HU → RH"
		}
		izvor := p.Izvori[u.Letva]
		if izvor == "" {
			izvor = "—"
		}
		c := []xlsxw.Celija{xlsxw.T(imeUlaza(u.Letva, ime), xlsxw.TablicaPod), xlsxw.T(drzava, xlsxw.TablicaSredina),
			xlsxw.T(izvor, xlsxw.TablicaSivo)}
		for d := 0; d <= prognoza.PricuvniDana; d++ {
			if v := p.Ulazi[u.Letva][d]; v != nil {
				c = append(c, xlsxw.N(*v, xlsxw.Unos))
			} else {
				c = append(c, xlsxw.T("", xlsxw.Unos))
			}
		}
		unos.Dodaj(c...)
	}
	unos.Sirine = []float64{34, 9, 14}
	for d := 0; d <= prognoza.PricuvniDana; d++ {
		unos.Sirine = append(unos.Sirine, 11)
	}

	// --- Veze: pravci, rasap i današnji pomak (pomak se smije mijenjati)
	veze.Dodaj(xlsxw.T("Veze: iz čega se računa koja letva", xlsxw.Naslov))
	veze.Dodaj(xlsxw.T("Naša letva = odsječak + pomak + nagib 1 · ulaz 1 + nagib 2 · ulaz 2. Pravci su iz dnevnih vrijednosti "+
		"zadnjih deset godina; ± je standardno odstupanje ostatka. Pomak je današnja razlika mjerenja i pravca — "+
		"smije se promijeniti (žuto) kad se zna da je letva danas drukčija.", xlsxw.Napomena))
	veze.Spoji(0, 1, 10, 1)
	veze.Visina(1, 42)
	veze.Dodaj()
	veze.Dodaj(xlsxw.T("Naša letva", xlsxw.Zaglavlje), xlsxw.T("Put", xlsxw.Zaglavlje), xlsxw.T("Iz", xlsxw.Zaglavlje),
		xlsxw.T("Odsječak", xlsxw.Zaglavlje), xlsxw.T("Nagib 1", xlsxw.Zaglavlje), xlsxw.T("Nagib 2", xlsxw.Zaglavlje),
		xlsxw.T("± cm", xlsxw.Zaglavlje), xlsxw.T("Pomak danas", xlsxw.Zaglavlje), xlsxw.T("Dana", xlsxw.Zaglavlje),
		xlsxw.T("Od", xlsxw.Zaglavlje), xlsxw.T("Napomena", xlsxw.Zaglavlje))
	type vezaRed struct {
		red  int
		veza prognoza.PricuvnaVeza
	}
	vezeLetve := map[string][]vezaRed{}
	for _, pp := range prognoza.PricuvniPutovi {
		for i, v := range p.Veze[pp.Letva] {
			var iz []string
			for _, l := range v.Izvori {
				iz = append(iz, imeUlaza(l, ime))
			}
			red := veze.Redak()
			c := []xlsxw.Celija{xlsxw.T(ime(pp.Letva), xlsxw.TablicaPod), xlsxw.N(float64(i+1), xlsxw.TablicaSredina),
				xlsxw.T(strings.Join(iz, " + "), xlsxw.Tablica)}
			if v.Koef == nil {
				c = append(c, xlsxw.T("", xlsxw.Tablica), xlsxw.T("", xlsxw.Tablica), xlsxw.T("", xlsxw.Tablica),
					xlsxw.T("", xlsxw.Tablica), xlsxw.T("", xlsxw.Tablica), xlsxw.T("", xlsxw.Tablica), xlsxw.T("", xlsxw.Tablica),
					xlsxw.T("nema pravca: "+v.Greska, xlsxw.TablicaSivo))
				veze.Dodaj(c...)
				continue
			}
			nagib2 := xlsxw.T("", xlsxw.Tablica)
			if len(v.Koef) > 2 {
				nagib2 = xlsxw.N(zaokruzi(v.Koef[2], 4), xlsxw.TablicaBroj)
			}
			rasap := xlsxw.T("", xlsxw.TablicaSredina)
			napomena := ""
			if v.Rasap > 0 {
				rasap = xlsxw.N(zaokruzi(v.Rasap, 1), xlsxw.TablicaSredina)
			}
			if strings.HasPrefix(v.Izvori[0], prognoza.Madjarska) {
				napomena = "mađarska prognoza iste letve, kakva je objavljena"
			}
			dana, od := xlsxw.T("", xlsxw.TablicaSredina), xlsxw.T("", xlsxw.TablicaSredina)
			if v.Dana > 0 {
				dana, od = xlsxw.N(float64(v.Dana), xlsxw.TablicaSredina), xlsxw.T(v.Od, xlsxw.TablicaSredina)
			}
			c = append(c, xlsxw.N(zaokruzi(v.Koef[0], 2), xlsxw.TablicaBroj), xlsxw.N(zaokruzi(v.Koef[1], 4), xlsxw.TablicaBroj),
				nagib2, rasap, xlsxw.N(v.Pomak, xlsxw.Unos), dana, od, xlsxw.T(napomena, xlsxw.TablicaSivo))
			veze.Dodaj(c...)
			vezeLetve[pp.Letva] = append(vezeLetve[pp.Letva], vezaRed{red, v})
		}
	}
	veze.Sirine = []float64{16, 6, 38, 10, 9, 9, 7, 11, 7, 11, 44}

	// --- Naše postaje: vrijednost i odakle je, po danu
	vrijednosti, putovi := prognoza.PricuvnoRacunaj(p)
	nase.Dodaj(xlsxw.T("Pričuvni izračun za naše letve", xlsxw.Naslov))
	nase.Dodaj(xlsxw.T("Kad prognoza COP Osijek ne radi: vodostaj u cm, u 7 h, izračunat iz tuđih prognoza na listu „Unos” "+
		"pravcima s lista „Veze” (upisani "+izdano+"). Uz svaku vrijednost piše iz čega je i koliko ta veza obično griješi. "+
		"Točnost je koliko i tuđa prognoza; prijenos na našu letvu dodaje nekoliko centimetara.", xlsxw.Napomena))
	nase.Spoji(0, 1, pricuvniStupac+prognoza.PricuvniDana, 1)
	nase.Visina(1, 42)
	nase.Dodaj()
	nase.Dodaj(append([]xlsxw.Celija{xlsxw.T("Letva", xlsxw.Zaglavlje), xlsxw.T("Izmjereno danas", xlsxw.Zaglavlje),
		xlsxw.T("", xlsxw.Zaglavlje)}, naslovDana()...)...)
	// Redak vrijednosti svake naše letve, za formule koje je uzimaju kao ulaz.
	redNase := map[string]int{}
	r := nase.Redak()
	for _, pp := range prognoza.PricuvniPutovi {
		redNase[pp.Letva] = r
		r += 2
	}
	ref := func(letva string, stupac int) string {
		if rn, ima := redNase[letva]; ima {
			return xlsxw.Adresa(stupac, rn)
		}
		return "Unos!" + xlsxw.Adresa(stupac, adresaUnosa[letva])
	}
	for _, pp := range prognoza.PricuvniPutovi {
		izm := xlsxw.T("", xlsxw.TablicaSredina)
		if v, ima := p.Nase[pp.Letva]; ima {
			izm = xlsxw.N(v, xlsxw.TablicaSredina)
		}
		vr := []xlsxw.Celija{xlsxw.T(ime(pp.Letva), xlsxw.TablicaPodRub), izm, xlsxw.T("cm", xlsxw.TablicaSivo)}
		iz := []xlsxw.Celija{xlsxw.T("iz", xlsxw.TablicaKurziv), xlsxw.T("", xlsxw.Tablica), xlsxw.T("", xlsxw.Tablica)}
		for d := 0; d <= prognoza.PricuvniDana; d++ {
			stupac := pricuvniStupac + d
			formula, tekst := "\"\"", "\"nema ulaza\""
			// put u stupcu s: uvjet (svi ulazi upisani) i izraz pravca
			put := func(vz vezaRed, s int) (string, string) {
				var uvjeti []string
				izraz := fmt.Sprintf("Veze!$D$%d+Veze!$H$%d", vz.red+1, vz.red+1)
				for j, l := range vz.veza.Izvori {
					a := ref(l, s)
					uvjeti = append(uvjeti, "ISNUMBER("+a+")")
					izraz += fmt.Sprintf("+Veze!$%s$%d*%s", xlsxw.Stupac(4+j), vz.red+1, a)
				}
				if len(uvjeti) > 1 {
					return "AND(" + strings.Join(uvjeti, ",") + ")", izraz
				}
				return uvjeti[0], izraz
			}
			for i := len(vezeLetve[pp.Letva]) - 1; i >= 0; i-- {
				vz := vezeLetve[pp.Letva][i]
				uvjet, izraz := put(vz, stupac)
				vrijednost := "ROUND(" + izraz + ",0)"
				if i > 0 && d > 0 {
					// Sljedeći put nastavlja na jučerašnju vrijednost svojom
					// promjenom, da prijelaz između dviju prognoza ne skoči.
					jucer := xlsxw.Adresa(stupac-1, redNase[pp.Letva])
					uvjet0, izraz0 := put(vz, stupac-1)
					vrijednost = fmt.Sprintf("IF(AND(ISNUMBER(%s),%s),ROUND(%s+(%s)-(%s),0),ROUND(%s,0))",
						jucer, uvjet0, jucer, izraz, izraz0, izraz)
				}
				formula = fmt.Sprintf("IF(%s,%s,%s)", uvjet, vrijednost, formula)
				tekst = fmt.Sprintf("IF(%s,\"%s\",%s)", uvjet, opisPuta(vz.veza, ime), tekst)
			}
			if v := vrijednosti[pp.Letva][d]; v != nil {
				vr = append(vr, xlsxw.F(formula, *v, xlsxw.TablicaPod))
			} else {
				vr = append(vr, xlsxw.FT(formula, "", xlsxw.TablicaPod))
			}
			opis := "nema ulaza"
			if i := putovi[pp.Letva][d]; i >= 0 {
				opis = opisPuta(p.Veze[pp.Letva][i], ime)
			}
			iz = append(iz, xlsxw.FT(tekst, opis, xlsxw.TablicaSivo))
		}
		nase.Dodaj(vr...)
		nase.Dodaj(iz...)
	}
	nase.Sirine = []float64{20, 11, 5}
	for d := 0; d <= prognoza.PricuvniDana; d++ {
		nase.Sirine = append(nase.Sirine, 16)
	}
	nase.PonoviRetke(3, 3)
	nase.Vodoravno = true

	// --- Kako
	kako.Dodaj(xlsxw.T("Kako se služiti pričuvnim izračunom", xlsxw.Naslov))
	for _, s := range []string{
		"1. Na listu „Unos” upiši vodostaje tuđih letvi u 7 h po danima: srpske iz biltena hidmet.gov.rs (Bezdan, Apatin, " +
			"Bogojevo, Bačka Palanka), mađarske s hydroinfo.hu (Mohács, Drávaszabolcs, Barcs te mađarska prognoza Botova, " +
			"Terezina Polja, Donjeg Miholjca, Belišća i Osijeka). Što ne znaš, ostavi prazno.",
		"2. List „Naše postaje” odmah računa naše letve. Za svaku uzima prvi put s lista „Veze” za koji su svi ulazi " +
			"upisani, i ispod vrijednosti piše iz čega je i ± koliko ta veza obično griješi.",
		"3. Pomak (list „Veze”, žuto) je današnja razlika mjerenja i pravca. Kad znaš današnje stanje letve, a prvi dan " +
			"izračuna mu ne odgovara, promijeni pomak da se slože.",
		"4. Letve nasuprot (Bezdan–Batina, Bačka Palanka–Ilok, Drávaszabolcs–Donji Miholjac) prenose se gotovo bez gubitka; " +
			"točnost je koliko i tuđa prognoza. Mađarska prognoza Aljmaša u pravilu je 25 cm previsoka, pa se Aljmaš " +
			"računa iz Apatina i Bogojeva, a ne iz nje.",
		"5. Pravci su upisani " + izdano + " iz dnevnih vrijednosti zadnjih deset godina; tablicu vrijedi preuzeti " +
			"iznova s vremena na vrijeme (Prognoze → O prognozi → Pričuvni izračun), dok program radi.",
	} {
		kako.Dodaj(xlsxw.T(s, xlsxw.Tekst))
		kako.Spoji(0, kako.Redak()-1, 8, kako.Redak()-1)
		kako.Visina(kako.Redak()-1, 44)
	}
	kako.Sirine = []float64{12, 12, 12, 12, 12, 12, 12, 12, 12}
	return k
}

// opisPuta je kratak opis puta za redak „iz”: „Bezdan ±4”.
func opisPuta(v prognoza.PricuvnaVeza, ime func(string) string) string {
	var iz []string
	for _, l := range v.Izvori {
		iz = append(iz, imeUlaza(l, ime))
	}
	s := strings.Join(iz, " i ")
	if v.Rasap > 0 {
		s += fmt.Sprintf(" ±%.0f", v.Rasap)
	}
	return strings.ReplaceAll(s, "\"", "")
}

func zaokruzi(v float64, dec int) float64 {
	m := 1.0
	for i := 0; i < dec; i++ {
		m *= 10
	}
	if v < 0 {
		return -float64(int64(-v*m+0.5)) / m
	}
	return float64(int64(v*m+0.5)) / m
}
