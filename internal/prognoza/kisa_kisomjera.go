package prognoza

import (
	"database/sql"
	"slices"
	"strings"
	"time"

	"gocop/internal/models"
)

// Dnevni model letve iz zapisa smije uz kišu međusliva uzeti i kišu jednog
// stvarnog kišomjera: jezeru ili rječici kišomjer na samom mjestu kaže više od
// srednjaka sliva. U popisu slivova zapisa takav ulaz piše „kisomjer:<šifra>”.
//
// Kišomjer mjeri samo ono što je palo. Kišu idućih dana, i sate za koje
// mjerenja još nema (ručni kišomjer upisuje se jednom dnevno), daje međusliv u
// kojem kišomjer stoji. Tako se i uči: pala kiša iz arhive kišomjera, buduća iz
// arhive međusliva, u mjerilu u kojem uživo stiže prognoza.

const predmetakKisomjera = "kisomjer:"

// KisaKisomjera je oznaka kiše jednog stvarnog kišomjera u popisu slivova
// dnevnog modela.
func KisaKisomjera(sifra string) string { return predmetakKisomjera + sifra }

// KisomjerIzSliva vraća šifru kišomjera kad oznaka u popisu slivova znači kišu
// jednog kišomjera, a ne međusliva.
func KisomjerIzSliva(sliv string) (string, bool) {
	sifra, je := strings.CutPrefix(sliv, predmetakKisomjera)
	return sifra, je && sifra != ""
}

// KisomjeriKise čita aktivne stvarne kišomjere iz registra (gocop.db) kao
// točke kiše: svaki je sam svoj „sliv” s oznakom KisaKisomjera, a SlivMjesta
// je međusliv u kojem stoji.
func KisomjeriKise(registar *sql.DB) ([]OborinskaTocka, error) {
	r, err := registar.Query(`SELECT code, sliv FROM kisomjeri
		WHERE aktivan = 1 AND vrsta = 'stvarni' ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var out []OborinskaTocka
	for r.Next() {
		t := OborinskaTocka{Tezina: 1}
		if err := r.Scan(&t.Code, &t.SlivMjesta); err != nil {
			return nil, err
		}
		t.Sliv = KisaKisomjera(t.Code)
		out = append(out, t)
	}
	return out, r.Err()
}

// TockeSKisomjerima su izvedene točke registra i stvarni kišomjeri zajedno:
// sve iz čega dnevni model letve iz zapisa smije učiti kišu.
func TockeSKisomjerima(registar *sql.DB) ([]OborinskaTocka, error) {
	tocke, err := OborinskeTocke(registar)
	if err != nil {
		return nil, err
	}
	kisomjeri, err := KisomjeriKise(registar)
	return slices.Concat(tocke, kisomjeri), err
}

// kisomjeriCilja su kišomjeri čiju kišu cilj traži.
func kisomjeriCilja(tocke []OborinskaTocka, c DnevniCilj) []OborinskaTocka {
	var out []OborinskaTocka
	for _, t := range tocke {
		if _, kisomjer := KisomjerIzSliva(t.Sliv); kisomjer && slices.Contains(c.Slivovi, t.Sliv) {
			out = append(out, t)
		}
	}
	return out
}

// tockeCilja ostavlja izvedene točke i one kišomjere koje cilj traži, da se iz
// arhive ne čita kiša stotinu kišomjera koje ovaj model ne gleda.
func tockeCilja(tocke []OborinskaTocka, c DnevniCilj) []OborinskaTocka {
	var out []OborinskaTocka
	for _, t := range tocke {
		if _, kisomjer := KisomjerIzSliva(t.Sliv); !kisomjer {
			out = append(out, t)
		}
	}
	return append(out, kisomjeriCilja(tocke, c)...)
}

// dodajKisuKisomjera upisuje među nizove cilja kišu kišomjera koje cilj traži:
// palu iz arhive kišomjera (dnevni niz, a dan kojeg ondje nema iz satnog), a
// buduću iz međusliva u kojem kišomjer stoji. oborine su dnevni nizovi svih
// točaka (DnevneOborine).
func dodajKisuKisomjera(arhiva *sql.DB, nizovi, oborine map[string]DnevniNiz, tocke []OborinskaTocka, c DnevniCilj) error {
	for _, t := range kisomjeriCilja(tocke, c) {
		n := oborine[OborinaKljuc(t.Sliv)]
		if n == nil {
			n = DnevniNiz{}
		}
		if err := dopuniIzSatnih(arhiva, t.Code, n); err != nil {
			return err
		}
		nizovi[OborinaKljuc(t.Sliv)] = n
		if unaprijed, ima := oborine[OborinaKljuc(t.SlivMjesta)]; ima && t.SlivMjesta != "" {
			nizovi[OborinaUnaprijedKljuc(t.Sliv)] = unaprijed
		}
	}
	return nil
}

// najmanjeSatiDana je koliko satnih mjerenja dan mora imati da mu se zbroj
// uzme kao dnevna kiša; dan promjene sata ima ih 23.
const najmanjeSatiDana = 23

// zbrojSati je kiša jednog dana zbrojena iz satnih mjerenja.
type zbrojSati struct {
	mm   float64
	sati int
}

// dopuniIzSatnih dodaje dnevnom nizu kišomjera dane kojih u dnevnom nizu arhive
// nema, a satni ih pokriva: zbroj sati zagrebačkog dana.
func dopuniIzSatnih(arhiva *sql.DB, sifra string, n DnevniNiz) error {
	r, err := arhiva.Query(`SELECT vrijeme, vrijednost FROM spoj
		WHERE letva = ? AND velicina = 'oborina' AND korak = 'satni'`, sifra)
	if err != nil {
		return err
	}
	defer r.Close()
	dani, err := zbrojiSatePoDanu(r)
	if err != nil {
		return err
	}
	for d, z := range dani {
		if _, ima := n[d]; !ima && z.sati >= najmanjeSatiDana {
			n[d] = z.mm
		}
	}
	return nil
}

// zbrojiSatePoDanu zbraja satnu kišu po danu kojem sat pripada.
func zbrojiSatePoDanu(r *sql.Rows) (map[int64]*zbrojSati, error) {
	dani := map[int64]*zbrojSati{}
	for r.Next() {
		var kad int64
		var v float64
		if err := r.Scan(&kad, &v); err != nil {
			return nil, err
		}
		d := danKise(kad)
		if dani[d] == nil {
			dani[d] = &zbrojSati{}
		}
		dani[d].mm += v
		dani[d].sati++
	}
	return dani, r.Err()
}

// danKise je dan (od 1970.) kojem pripada sat kiše koji završava u kad: sat u
// arhivi stoji na kraju svog sata, pa ponoćni pripada danu koji je završio.
func danKise(kad int64) int64 {
	l := time.Unix(kad-60, 0).In(models.Zagreb)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, time.UTC).Unix() / 86400
}

// dodajKisomjere dodaje živoj kiši međuslivova kišu stvarnih kišomjera, po
// danima kao OborineOkoSada: sat ima izmjerenu kišu kišomjera, a kad je nema
// (iduće dane, i dok ručno mjerenje nije upisano) kišu međusliva u kojem
// kišomjer stoji. Kišomjer bez međusliva nema idućih dana, pa mu dani izostanu
// i model koji ga traži javi da mu kiša fali.
func (iz *OborinskiIzvor) dodajKisomjere(out map[string]DnevniNiz, satne map[string]map[int64]float64, sada int64, unatrag, unaprijed int) {
	if len(iz.Kisomjeri) == 0 {
		return
	}
	od, do := sada-int64(24*unatrag)+1, sada+int64(24*unaprijed)
	var izmjerene map[string]map[int64]float64
	if iz.Izmjerene != nil {
		izmjerene, _ = iz.Izmjerene(od, do) // bez mjerenja ostaje kiša međusliva
	}
	poSlivu := map[string]map[int64]float64{}
	for _, t := range iz.Kisomjeri {
		if t.SlivMjesta == "" {
			continue
		}
		sliv, ima := poSlivu[t.SlivMjesta]
		if !ima {
			sliv = satiMedjusliva(iz.Tocke, satne, t.SlivMjesta, od, do)
			poSlivu[t.SlivMjesta] = sliv
		}
		out[OborinaKljuc(t.Sliv)] = daniIzSati(izmjerene[t.Code], sliv, sada, unatrag, unaprijed)
	}
}

// satiMedjusliva je kiša međusliva po satu: težinski srednjak njegovih točaka,
// za sate koje ima barem polovica težine.
func satiMedjusliva(tocke []OborinskaTocka, satne map[string]map[int64]float64, sliv string, od, do int64) map[int64]float64 {
	svoje, ukupno := tockeMedjusliva(tocke, sliv)
	out := map[int64]float64{}
	for h := od; h <= do; h++ {
		var zbroj, tezina float64
		for _, t := range svoje {
			if v, ima := satne[t.Code][h]; ima {
				zbroj += t.Tezina * v
				tezina += t.Tezina
			}
		}
		if tezina > 0 && tezina >= ukupno/2 {
			out[h] = zbroj / tezina
		}
	}
	return out
}

// tockeMedjusliva su točke međusliva koje nose težinu i zbroj njihovih težina.
func tockeMedjusliva(tocke []OborinskaTocka, sliv string) (svoje []OborinskaTocka, ukupno float64) {
	for _, t := range tocke {
		if t.Sliv == sliv && t.Tezina > 0 {
			svoje = append(svoje, t)
			ukupno += t.Tezina
		}
	}
	return svoje, ukupno
}

// daniIzSati zbraja sate u dane oko sata izdavanja: dan 0 su 24 sata koja
// završavaju u sada. Sat se uzima iz prvog niza koji ga ima; dan kojem fali
// ijedan sat izostaje.
func daniIzSati(izmjereno, sliv map[int64]float64, sada int64, unatrag, unaprijed int) DnevniNiz {
	n := DnevniNiz{}
	for d := -unaprijed; d < unatrag; d++ {
		kraj := sada - int64(24*d)
		var zbroj float64
		sati := 0
		for h := kraj - 23; h <= kraj; h++ {
			v, ima := izmjereno[h]
			if !ima {
				v, ima = sliv[h]
			}
			if ima {
				zbroj += v
				sati++
			}
		}
		if sati == 24 {
			n[int64(-d)] = zbroj
		}
	}
	return n
}
