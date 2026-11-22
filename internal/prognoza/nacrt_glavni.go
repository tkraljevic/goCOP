package prognoza

// Nacrt računa letve glavnog lanca: priprema i proba. Priprema namjesti samo taj
// račun, a proba mjeri cijeli lanac dvaput na istoj arhivi, sadašnjim modelom i
// modelom s izmjenom, pa se vidi što izmjena radi letvi i svima nizvodno. Sve se
// računa u privremenim bazama; model i prognoza se ne mijenjaju.

import (
	"database/sql"
	"errors"
	"maps"
	"math"
	"slices"
	"sort"
)

// PripremiGlavnuLetvu namjesti račun letve glavnog lanca iz zapisa i vrati ga,
// bez spremanja. Baza prognoza se samo čita.
func PripremiGlavnuLetvu(arhiva, baza *sql.DB, z ZapisLetve, o OkvirModela) (PripremaLetve, error) {
	if !namjestanjeTraje.TryLock() {
		return PripremaLetve{}, errors.New("namještanje modela već traje; pokušaj kad završi")
	}
	defer namjestanjeTraje.Unlock()
	glavne, err := SveInacice(baza)
	if err != nil {
		return PripremaLetve{}, err
	}
	zapisi, _, err := ZapisiLetvi(baza)
	if err != nil {
		return PripremaLetve{}, err
	}
	if err := z.ProvjeriGlavni(o, zapisi); err != nil {
		return PripremaLetve{}, err
	}
	return namjestiGlavnu(arhiva, z, glavne)
}

// ProbaLanca je ono što proba letve glavnog lanca nosi uz mjeru same letve.
type ProbaLanca struct {
	// Prije je mjera iste letve sadašnjim modelom, na istoj arhivi i razdoblju.
	Prije []MjeraDosega `json:"prije"`
	// Nizvodno su letve kojima se promašaj promijenio, po dosegu.
	Nizvodno []PromjenaNizvodno `json:"nizvodno"`
	// Promasaji su promašaji letve i svih kojima se mjera promijenila, kako ih
	// je izmjerio model s izmjenom; ključ je „letva|veličina”. Upisuje ih Primijeni.
	Promasaji map[string]map[int]Promasaj `json:"promasaji"`
	// Model je otisak modela na kojem je proba mjerila; kad se model poslije
	// promijeni, proba više ne vrijedi.
	Model string `json:"model"`
}

// PromjenaNizvodno je promašaj jedne letve na jednom dosegu, prije i poslije.
type PromjenaNizvodno struct {
	Letva    string  `json:"letva"`
	Velicina string  `json:"velicina"`
	Doseg    int     `json:"doseg"`
	Prije    float64 `json:"prije"`
	Poslije  float64 `json:"poslije"`
}

// opcijeProbeLanca su postavke provjere unatrag za probu letve glavnog lanca:
// iste s kojima se promašaji zapisuju za živu prognozu. Testovi ih zamjenjuju,
// jer im arhiva nije iz tih godina.
var opcijeProbeLanca = ZadaneOpcijeProvjere

// doseziNizvodno su dosezi na kojima se uspoređuju letve nizvodno.
var doseziNizvodno = []int{24, 48, 72, 96}

// kljucPromasaja je ključ letve i veličine u ProbaLanca.Promasaji.
func kljucPromasaja(iz Izvor) string { return iz.Letva + "|" + iz.Velicina }

// ProbajGlavniNacrt mjeri pripremljeni račun letve glavnog lanca: cijeli lanac
// sadašnjim modelom i modelom s izmjenom, na istoj arhivi. prije i poslije su
// nove, prazne baze prognoza; baza se samo čita, osim nacrta. korak javlja
// što se upravo radi.
func ProbajGlavniNacrt(arhiva, baza, prije, poslije *sql.DB, letva, kad string, korak func(string)) (NacrtLetve, error) {
	n, err := pripremljeniGlavniNacrt(baza, letva)
	if err != nil {
		return NacrtLetve{}, err
	}
	otisak, svi, err := modeliZaProbu(baza, prije, poslije, n.Zapis)
	if err != nil {
		return NacrtLetve{}, err
	}
	o := opcijeProbeLanca()
	mjere, err := izmjeriModele(arhiva, o, korak, prije, poslije)
	if err != nil {
		return NacrtLetve{}, err
	}
	stara, nova := mjere[0], mjere[1]
	iz := Izvor{Letva: letva, Velicina: n.Zapis.Velicina}
	uzvodno := uzvodneLetve(letva, svi)
	m := slozenaMjera(n.Zapis, nova.promasaji[iz], nova.mjere[iz], uzvodno, o)
	n.Proba = &ProbaNacrta{Kad: kad, Dosezi: m.Dosezi, Natpisi: m.Natpisi, Razdoblje: m.Razdoblje,
		Lanac: &ProbaLanca{Prije: slozenaMjera(n.Zapis, stara.promasaji[iz], stara.mjere[iz], uzvodno, o).Dosezi,
			Nizvodno: promjeneNizvodno(stara.mjere, nova.mjere, iz), Promasaji: promijenjeniPromasaji(stara, nova, iz), Model: otisak}}
	return n, upisiNacrt(baza, n)
}

// koraciProbeLanca su natpisi dvaju mjerenja probe, redom.
var koraciProbeLanca = []string{"mjerenje sadašnjeg modela unatrag", "mjerenje modela s izmjenom unatrag"}

// izmjeriModele mjeri redom model u svakoj bazi i javlja što se upravo radi.
func izmjeriModele(arhiva *sql.DB, o OpcijeProvjere, korak func(string), baze ...*sql.DB) ([]mjeraModela, error) {
	var out []mjeraModela
	for i, baza := range baze {
		korak(koraciProbeLanca[i])
		m, err := izmjeriModel(arhiva, baza, o)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// pripremljeniGlavniNacrt je pripremljen nacrt letve glavnog lanca.
func pripremljeniGlavniNacrt(baza *sql.DB, letva string) (NacrtLetve, error) {
	nacrti, err := Nacrti(baza)
	if err != nil {
		return NacrtLetve{}, err
	}
	n, ima := nacrti[letva]
	switch {
	case !GlavnaLetva(letva):
		return NacrtLetve{}, errors.New("letva nije u glavnom lancu")
	case !ima || n.Priprema == nil || len(n.Zapis.Brojevi.Inacice) == 0:
		return NacrtLetve{}, errors.New("račun nije pripremljen; pripremi ga pa probaj")
	}
	return n, nil
}

// modeliZaProbu prepisuje model iz baze u obje privremene baze, a u drugoj
// zamijeni račun letve onim iz zapisa. Vraća otisak modela iz baze i sve račune
// modela s izmjenom.
func modeliZaProbu(baza, prije, poslije *sql.DB, z ZapisLetve) (string, map[string][][]Pojas, error) {
	paket, otisak, err := SastaviModel(baza)
	if err != nil {
		return "", nil, err
	}
	for _, db := range []*sql.DB{prije, poslije} {
		if err := novaSModelom(db, paket); err != nil {
			return "", nil, err
		}
	}
	svi, err := zamijeniRacun(poslije, z)
	return otisak, svi, err
}

// novaSModelom upisuje model u novu, praznu bazu prognoza.
func novaSModelom(db *sql.DB, paket PaketModela) error {
	if err := provjeriNovuBazu(db); err != nil {
		return err
	}
	return PrimiModel(db, paket)
}

// zamijeniRacun upisuje u bazu račun letve iz zapisa umjesto zatečenog; ostali
// računi ostaju isti. Vraća sve račune kako sada stoje u bazi.
func zamijeniRacun(baza *sql.DB, z ZapisLetve) (map[string][][]Pojas, error) {
	svi, err := sveNamjesteno(baza)
	if err != nil {
		return nil, err
	}
	svi[z.Letva] = z.Brojevi.Inacice
	var pojasi []Pojas
	for _, letva := range slices.Sorted(maps.Keys(svi)) {
		pojasi = append(pojasi, slices.Concat(svi[letva]...)...)
	}
	return svi, Spremi(baza, pojasi, "proba")
}

// mjeraModela je što je provjera unatrag izmjerila jednom modelu.
type mjeraModela struct {
	mjere     MjereProvjere
	promasaji map[Izvor]map[int]Promasaj
}

// izmjeriModel pušta provjeru unatrag na modelu iz baze i čita što je izmjerila.
func izmjeriModel(arhiva, baza *sql.DB, o OpcijeProvjere) (mjeraModela, error) {
	o.Zapisi, o.Mjera = true, MjereProvjere{}
	if _, err := ProvjeriUnatrag(arhiva, baza, o); err != nil {
		return mjeraModela{}, err
	}
	promasaji, err := Promasaji(baza)
	return mjeraModela{mjere: o.Mjera, promasaji: promasaji}, err
}

// istaMjera javlja jesu li dvije mjere jedne letve iste na svim dosezima.
func istaMjera(a, b map[int]MjeraProvjere) bool {
	return maps.Equal(a, b)
}

// promjeneNizvodno su letve, osim same, kojima se promašaj na nekom dosegu
// promijenio barem za desetinku, redom letve i dosega.
func promjeneNizvodno(stare, nove MjereProvjere, sama Izvor) []PromjenaNizvodno {
	var out []PromjenaNizvodno
	for iz, nova := range nove {
		if iz == sama {
			continue
		}
		for _, d := range doseziNizvodno {
			prije, poslije := stare[iz][d].Promasaj, nova[d].Promasaj
			if math.Round(prije*10) != math.Round(poslije*10) {
				out = append(out, PromjenaNizvodno{Letva: iz.Letva, Velicina: iz.Velicina, Doseg: d, Prije: prije, Poslije: poslije})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Letva != out[j].Letva {
			return out[i].Letva < out[j].Letva
		}
		return out[i].Doseg < out[j].Doseg
	})
	return out
}

// promijenjeniPromasaji su promašaji modela s izmjenom za letvu samu i za svaku
// kojoj se mjera promijenila. Ostalima promašaji ostaju kakvi su u modelu.
func promijenjeniPromasaji(stara, nova mjeraModela, sama Izvor) map[string]map[int]Promasaj {
	out := map[string]map[int]Promasaj{}
	for iz, p := range nova.promasaji {
		if iz == sama || !istaMjera(stara.mjere[iz], nova.mjere[iz]) {
			out[kljucPromasaja(iz)] = p
		}
	}
	return out
}
