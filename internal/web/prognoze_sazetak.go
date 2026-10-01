package web

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"
	"gocop/internal/xlsxw"
)

// Sažetak je prvi list izvoza: prognoza ukratko za one kojima je puni
// Excel previše — nekoliko rečenica, tablica naših postaja (sada, sutra, za
// 3 i 6 dana, kretanje, stanje obrane, napomena), usporedba s mađarskom
// prognozom i kiša po slivovima. Rečenice se slažu iz brojki, bez
// stručnih izraza.

// sazetakSkupine su postaje sažetka, po rijekama, redom niz tok.
var sazetakSkupine = []struct {
	Voda  string
	Letve []string
}{
	{"Dunav", []string{"batina", "aljmas", "vukovar", "ilok"}},
	{"Drava", []string{"botovo", "terezino-polje", "donji-miholjac", "belisce", "osijek"}},
	{"Mura", []string{"mursko-sredisce", "gorican"}},
}

// protokPrema su postaje kojima se u sažetku protok uzima sa susjedne:
// Vukovar nema mjerenih protoka od 2019. (HIS-2000 do kraja 2018.), a između
// Vukovara i Iloka nema većeg pritoka, pa je protok gotovo isti (1. 10.
// 2026. po krivuljama 851 i 873 m³/s). Iločka krivulja seže i niže, pa
// protoka ima i kad Vukovar padne ispod raspona svoje.
var protokPrema = map[string]string{"vukovar": "ilok"}

// postajaSazetka je jedna postaja s brojkama koje sažetak treba
type postajaSazetka struct {
	Kod, Ime    string
	Sada, SadaQ *float64
	Dani        []*float64 // 1.–6. dan u 07 h
	DaniQ       []*float64 // protok za iste termine, gdje letva ima krivulju
	Naslovi     []string   // „pet 2.10."
	HU          []*float64 // mađarska prognoza za iste termine
	Min, Max    *Krajnost
	QPrema      string // ime postaje čiji se protok uzima, kad nije vlastiti
	Postaja     models.Station
	ImaPostaju  bool
}

// kraj vraća zadnji dan s vrijednošću i njegov indeks
func (p postajaSazetka) kraj() (float64, int, bool) {
	for i := len(p.Dani) - 1; i >= 0; i-- {
		if p.Dani[i] != nil {
			return *p.Dani[i], i, true
		}
	}
	return 0, 0, false
}

// najniziNajvisi vraća najnižu i najvišu prognoziranu vrijednost i dan
func (p postajaSazetka) najniziNajvisi() (mn, mx float64, imn, imx int, ok bool) {
	for i, v := range p.Dani {
		if v == nil {
			continue
		}
		if !ok || *v < mn {
			mn, imn = *v, i
		}
		if !ok || *v > mx {
			mx, imx = *v, i
		}
		ok = true
	}
	return
}

func (p postajaSazetka) dan(i int) *float64 {
	if i < len(p.Dani) {
		return p.Dani[i]
	}
	return nil
}

func (p postajaSazetka) danQ(i int) *float64 {
	if i < len(p.DaniQ) {
		return p.DaniQ[i]
	}
	return nil
}

// qS piše protok zaokružen na cijeli m³/s, s točkom tisućica
func qS(v float64) string { return brojHRf(math.Round(v), 0) }

// postajeSazetka skuplja postaje sažetka iz podataka izvoza
func postajeSazetka(data PrognozePageData) map[string][]postajaSazetka {
	po := map[string]LetvaPrognoze{}
	for _, t := range data.Tablice {
		for _, x := range t.Letve {
			po[x.Kod] = x
		}
	}
	for _, x := range data.Letve {
		if _, ima := po[x.Kod]; !ima {
			po[x.Kod] = x
		}
	}
	out := map[string][]postajaSazetka{}
	for _, g := range sazetakSkupine {
		for _, kod := range g.Letve {
			x, ima := po[kod]
			if !ima {
				continue
			}
			ime, _ := imeIDrzava(x.Naziv)
			p := postajaSazetka{Kod: kod, Ime: ime, Sada: x.SadaCmV, SadaQ: x.SadaQV, Min: x.KrajMin, Max: x.KrajMax}
			p.Postaja, p.ImaPostaju = data.postajeM[kod]
			for _, d := range x.Dani {
				p.Dani = append(p.Dani, d.CmV)
				p.DaniQ = append(p.DaniQ, d.QV)
				p.Naslovi = append(p.Naslovi, d.Naslov)
				var hu *float64
				for _, t := range d.Tude {
					if t.Oznaka == "HU" {
						v := t.CmV
						hu = &v
					}
				}
				p.HU = append(p.HU, hu)
			}
			out[g.Voda] = append(out[g.Voda], p)
		}
	}
	// protok sa susjedne postaje
	for v, postaje := range out {
		for i := range postaje {
			susjed, ima := protokPrema[postaje[i].Kod]
			if !ima {
				continue
			}
			x, ima := po[susjed]
			if !ima {
				continue
			}
			ime, _ := imeIDrzava(x.Naziv)
			p := &out[v][i]
			p.QPrema, p.SadaQ, p.DaniQ = ime, x.SadaQV, nil
			for _, d := range x.Dani {
				p.DaniQ = append(p.DaniQ, d.QV)
			}
		}
	}
	return out
}

// stanjeObrane vraća naziv najvišeg praga koji vodostaj doseže
func stanjeObrane(st models.Station, v float64) (string, bool) {
	for _, p := range []struct {
		naziv string
		prag  models.Threshold
	}{
		{"izvanredno stanje", st.State},
		{"izvanredna obrana", st.Emergency},
		{"redovna obrana", st.Regular},
		{"pripremno stanje", st.Prep},
	} {
		if p.prag.IsUsable() && v >= float64(*p.prag.Cm) {
			return p.naziv, true
		}
	}
	return "ispod pragova obrane", false
}

// cmS piše vodostaj kao cijeli broj s predznakom minus po hrvatski
func cmS(v float64) string {
	return strings.Replace(fmt.Sprintf("%d", int(math.Round(v))), "-", "−", 1)
}

// danS skraćuje „pet 2.10." na „2. 10."
func danS(naslov string) string {
	f := strings.Fields(naslov)
	if len(f) == 0 {
		return naslov
	}
	d := strings.Split(strings.TrimSuffix(f[len(f)-1], "."), ".")
	if len(d) == 2 {
		return d[0] + ". " + d[1] + "."
	}
	return f[len(f)-1]
}

// recenicaRijeke opisuje rijeku u jednoj do dvije rečenice
func recenicaRijeke(voda string, postaje []postajaSazetka) string {
	var zbroj float64
	n := 0
	var najveca *postajaSazetka
	var najvecaPromjena float64
	for i := range postaje {
		p := &postaje[i]
		k, _, ok := p.kraj()
		if p.Sada == nil || !ok {
			continue
		}
		pr := k - *p.Sada
		zbroj += pr
		n++
		if najveca == nil || math.Abs(pr) > math.Abs(najvecaPromjena) {
			najveca, najvecaPromjena = p, pr
		}
	}
	if n == 0 {
		return ""
	}
	srednja := zbroj / float64(n)
	var b strings.Builder
	switch {
	case srednja <= -5:
		b.WriteString(voda + " pada")
	case srednja >= 5:
		b.WriteString(voda + " raste")
	default:
		if voda == "Dunav" {
			b.WriteString(voda + " je stabilan")
		} else {
			b.WriteString(voda + " je stabilna")
		}
	}
	if najveca != nil && math.Abs(najvecaPromjena) >= 5 {
		k, ik, _ := najveca.kraj()
		fmt.Fprintf(&b, ": %s s %s na %s cm do %s", najveca.Ime, cmS(*najveca.Sada), cmS(k), danS(najveca.Naslovi[ik]))
		if q := najveca.danQ(ik); q != nil && najveca.SadaQ != nil {
			if najveca.QPrema != "" {
				fmt.Fprintf(&b, " (protok prema %s s %s na %s m³/s)", genitivPostaje(najveca.QPrema), qS(*najveca.SadaQ), qS(*q))
			} else {
				fmt.Fprintf(&b, " (protok s %s na %s m³/s)", qS(*najveca.SadaQ), qS(*q))
			}
		}
	}
	if !strings.HasSuffix(b.String(), ".") {
		b.WriteString(".")
	}
	// rekordi i pragovi
	var novo, prag []string
	for _, p := range postaje {
		mn, mx, imn, imx, ok := p.najniziNajvisi()
		if !ok {
			continue
		}
		if p.Min != nil && math.Round(mn) < math.Round(p.Min.Cm) {
			novo = append(novo, fmt.Sprintf("%s (%s cm %s, dosad najniže %s cm%s)", p.Ime, cmS(mn), danS(p.Naslovi[imn]), cmS(p.Min.Cm), godinaS(p.Min)))
		}
		if p.Max != nil && math.Round(mx) > math.Round(p.Max.Cm) {
			novo = append(novo, fmt.Sprintf("%s (%s cm %s, dosad najviše %s cm%s)", p.Ime, cmS(mx), danS(p.Naslovi[imx]), cmS(p.Max.Cm), godinaS(p.Max)))
		}
		if p.ImaPostaju {
			if s, ima := stanjeObrane(p.Postaja, mx); ima {
				prag = append(prag, fmt.Sprintf("%s %s (%s cm)", p.Ime, s, danS(p.Naslovi[imx])))
			}
		}
	}
	if len(novo) > 0 {
		b.WriteString(" Mogući novi zabilježeni vodostaj: " + strings.Join(novo, "; ") + ".")
	}
	if len(prag) > 0 {
		b.WriteString(" Pragovi obrane: " + strings.Join(prag, "; ") + ".")
	}
	return b.String()
}

func godinaS(k *Krajnost) string {
	if k == nil || k.Godina == 0 {
		return ""
	}
	return fmt.Sprintf(", %d.", k.Godina)
}

// recenicaMadjara navodi postaje na kojima se mađarska prognoza od naše
// razlikuje 10 cm ili više
func recenicaMadjara(sve map[string][]postajaSazetka) string {
	var r []string
	for _, g := range sazetakSkupine {
		for _, p := range sve[g.Voda] {
			najv, gdje := 0.0, -1
			for i := range p.Dani {
				if p.Dani[i] == nil || i >= len(p.HU) || p.HU[i] == nil {
					continue
				}
				if d := math.Abs(*p.Dani[i] - *p.HU[i]); d > najv {
					najv, gdje = d, i
				}
			}
			if gdje >= 0 && najv >= 10 {
				r = append(r, fmt.Sprintf("%s (%s: naša %s, mađarska %s cm)", p.Ime, danS(p.Naslovi[gdje]),
					cmS(*p.Dani[gdje]), cmS(*p.HU[gdje])))
			}
		}
	}
	if len(r) == 0 {
		return "Mađarska prognoza slaže se s našom (razlike manje od 10 cm)."
	}
	return "Mađarska prognoza razlikuje se od naše 10 cm ili više: " + strings.Join(r, "; ") + "."
}

// recenicaKise opisuje kišu po slivovima jednom rečenicom
func recenicaKise(stanja []prognoza.StanjeSliva, err error) string {
	if err != nil || stanja == nil {
		return ""
	}
	var u []string
	for _, st := range stanja {
		if st.Razina == 0 {
			continue
		}
		u = append(u, fmt.Sprintf("međusliv %s — %s (palo 72 h %s mm, očekuje se 48 h %s mm)",
			st.Sliv, map[int]string{1: "pojačana kiša", 2: "jaka kiša", 3: "izvanredna kiša"}[st.Razina],
			brojHRf(st.Palo72, 0), brojHRf(st.Dolazi48, 0)))
	}
	if len(u) == 0 {
		return "Na slivovima nema neuobičajene kiše, ni pale ni prognozirane za sljedećih 48 sati."
	}
	return "Neuobičajena kiša na slivovima: " + strings.Join(u, "; ") + ". Porast je uključen u prognozu."
}

// redakaTeksta procjenjuje u koliko se redaka tekst prelomi u širini od
// sirina Excelovih jedinica (Arial 10: oko 1,1 znak po jedinici, uz rezervu
// za prelamanje na riječi). Excel kod spojenih ćelija visinu ne prilagodi
// sam, pa se računa ovdje.
func redakaTeksta(tekst string, sirina float64) int {
	if tekst == "" {
		return 1
	}
	po := math.Max(1, sirina*1.05)
	n := 0
	for _, dio := range strings.Split(tekst, "\n") {
		n += int(math.Max(1, math.Ceil(float64(len([]rune(dio)))*1.08/po)))
	}
	return n
}

// visinaRedaka je visina retka u točkama za toliko redaka teksta
func visinaRedaka(n int) float64 { return float64(n)*13 + 3 }

// listSazetka piše prvi list izvoza
func (h *PrognozeHandler) listSazetka(ctx context.Context, k *xlsxw.Knjiga, z ZaglavljeIzvoza, data PrognozePageData) {
	T, N := xlsxw.T, xlsxw.N
	sve := postajeSazetka(data)
	if len(sve) == 0 {
		return
	}
	// postaja, četiri termina po vodostaj i protok, kretanje, stanje, napomena
	const stupaca = 12
	l := k.NoviList("Sažetak")
	l.Vodoravno = true
	l.Sirine = []float64{18, 7, 8, 7, 8, 7, 8, 7, 8, 13, 18, 40}
	zaglavljeLista(l, z, "PROGNOZA VODOSTAJA — KRATKI PREGLED",
		"izdano "+data.Izdano+" · vodostaj u cm i protok u m³/s, za 07 h", stupaca)

	odlomak := func(tekst string, stil int) {
		if tekst == "" {
			return
		}
		r := l.Redak()
		red := make([]xlsxw.Celija, stupaca)
		red[0] = T(tekst, stil)
		l.Dodaj(red...)
		l.Spoji(0, r, stupaca-1, r)
		var sirina float64
		for _, s := range l.Sirine {
			sirina += s
		}
		l.Visina(r, visinaRedaka(redakaTeksta(tekst, sirina)))
	}
	naslov := func(tekst string) {
		l.Dodaj()
		r := l.Redak()
		red := make([]xlsxw.Celija, stupaca)
		red[0] = T(tekst, xlsxw.Podnaslov)
		l.Dodaj(red...)
		l.Spoji(0, r, stupaca-1, r)
	}

	// Ukratko
	var kisa []prognoza.StanjeSliva
	var kisaErr error
	if h.kisa != nil {
		if f := h.kisa(); f != nil {
			c, otkazi := context.WithTimeout(ctx, 10*time.Second)
			kisa, _, kisaErr = f(c)
			otkazi()
		}
	}
	naslov("Ukratko")
	for _, g := range sazetakSkupine {
		odlomak(recenicaRijeke(g.Voda, sve[g.Voda]), xlsxw.Tekst)
	}
	odlomak(recenicaKise(kisa, kisaErr), xlsxw.Tekst)
	odlomak(recenicaMadjara(sve), xlsxw.Tekst)

	// Tablica naših postaja
	naslov("Naše postaje")
	// Zadnji stupac je zadnji dan koji tablica ima: dnevni model daje
	// srednjak 24 sata, pa šesti dan u 07 h po zamisli ostaje prazan.
	var sutra, tri, zadnjiDan string
	zadnji := 0
	for _, g := range sazetakSkupine {
		for _, p := range sve[g.Voda] {
			for i, v := range p.Dani {
				if v != nil && i > zadnji {
					zadnji = i
				}
			}
			if sutra == "" && len(p.Naslovi) > 2 {
				sutra, tri = danS(p.Naslovi[0]), danS(p.Naslovi[2])
			}
		}
	}
	for _, g := range sazetakSkupine {
		for _, p := range sve[g.Voda] {
			if zadnjiDan == "" && zadnji < len(p.Naslovi) {
				zadnjiDan = danS(p.Naslovi[zadnji])
			}
		}
	}
	naslovZadnjeg := fmt.Sprintf("Za %d dana %s", zadnji+1, zadnjiDan)
	// zaglavlje u dva retka: termin preko dvaju stupaca, ispod cm i m³/s
	r0 := l.Redak()
	l.Dodaj(T("Postaja", xlsxw.Zaglavlje), T("Sada", xlsxw.Zaglavlje), T("", xlsxw.Zaglavlje),
		T("Sutra "+sutra, xlsxw.Zaglavlje), T("", xlsxw.Zaglavlje), T("Za 3 dana "+tri, xlsxw.Zaglavlje), T("", xlsxw.Zaglavlje),
		T(naslovZadnjeg, xlsxw.Zaglavlje), T("", xlsxw.Zaglavlje), T("Kretanje vodostaja", xlsxw.Zaglavlje),
		T("Stanje obrane", xlsxw.Zaglavlje), T("Napomena", xlsxw.Zaglavlje))
	l.Dodaj(T("", xlsxw.Zaglavlje), T("cm", xlsxw.Zaglavlje), T("m³/s", xlsxw.Zaglavlje), T("cm", xlsxw.Zaglavlje), T("m³/s", xlsxw.Zaglavlje),
		T("cm", xlsxw.Zaglavlje), T("m³/s", xlsxw.Zaglavlje), T("cm", xlsxw.Zaglavlje), T("m³/s", xlsxw.Zaglavlje),
		T("", xlsxw.Zaglavlje), T("", xlsxw.Zaglavlje), T("", xlsxw.Zaglavlje))
	for _, c := range []int{1, 3, 5, 7} {
		l.Spoji(c, r0, c+1, r0)
	}
	for _, c := range []int{0, 9, 10, 11} {
		l.Spoji(c, r0, c, r0+1)
	}
	l.Visina(r0, 30)
	vrijednost := func(v *float64, stil int) xlsxw.Celija {
		if v == nil {
			return T("—", xlsxw.TablicaSredina)
		}
		return N(math.Round(*v), stil)
	}
	protok := func(v *float64, prema string) xlsxw.Celija {
		stil := xlsxw.TablicaSivo
		if prema != "" {
			stil = xlsxw.TablicaKurziv // procjena sa susjedne postaje
		}
		if v == nil {
			return T("", stil)
		}
		return N(math.Round(*v), stil)
	}
	var premaNapomena []string
	for _, g := range sazetakSkupine {
		postaje := sve[g.Voda]
		if len(postaje) == 0 {
			continue
		}
		r := l.Redak()
		red := make([]xlsxw.Celija, stupaca)
		red[0] = T(strings.ToUpper(g.Voda), xlsxw.SazetakNaslov)
		for i := 1; i < stupaca; i++ {
			red[i] = T("", xlsxw.SazetakNaslov)
		}
		l.Dodaj(red...)
		l.Spoji(0, r, stupaca-1, r)
		for _, p := range postaje {
			kretanje := "—"
			if k, _, ok := p.kraj(); ok && p.Sada != nil {
				d := k - *p.Sada
				switch {
				case d >= 3:
					kretanje = "↑ raste " + cmS(d) + " cm"
				case d <= -3:
					kretanje = "↓ pada " + cmS(-d) + " cm"
				default:
					kretanje = "→ stalno"
				}
			}
			stanje, stilStanja := "—", xlsxw.TablicaTekst
			mn, mx, imn, _, ok := p.najniziNajvisi()
			if ok && p.ImaPostaju {
				var dosize bool
				stanje, dosize = stanjeObrane(p.Postaja, mx)
				if dosize {
					stilStanja = xlsxw.RekordVisok
				}
			}
			var napomena []string
			stilNapomene := xlsxw.TablicaTekst
			if ok && p.Min != nil && math.Round(mn) < math.Round(p.Min.Cm) {
				napomena = append(napomena, fmt.Sprintf("najniži zabilježeni vodostaj: %s cm %s (dosad %s cm%s)",
					cmS(mn), danS(p.Naslovi[imn]), cmS(p.Min.Cm), godinaS(p.Min)))
				stilNapomene = xlsxw.TekstNizak
			} else if ok && p.Min != nil && mn-p.Min.Cm < 10 {
				napomena = append(napomena, fmt.Sprintf("blizu najnižeg zabilježenog (%s cm%s)", cmS(p.Min.Cm), godinaS(p.Min)))
			}
			if ok && p.Max != nil && math.Round(mx) > math.Round(p.Max.Cm) {
				napomena = append(napomena, fmt.Sprintf("iznad najvišeg zabilježenog (%s cm%s)", cmS(p.Max.Cm), godinaS(p.Max)))
				stilNapomene = xlsxw.TekstVisok
			}
			if p.QPrema != "" {
				premaNapomena = append(premaNapomena, fmt.Sprintf("%s: protok (kurziv) je protok %s — mjerenih protoka %s nema od 2019., a između njih nema većeg pritoka",
					p.Ime, genitivPostaje(p.QPrema), genitivPostaje(p.Ime)))
			}
			tekstNapomene := strings.Join(napomena, "; ")
			redaka := 1
			for _, c := range []struct {
				tekst  string
				sirina float64
			}{{p.Ime, l.Sirine[0]}, {kretanje, l.Sirine[9]}, {stanje, l.Sirine[10]}, {tekstNapomene, l.Sirine[11]}} {
				redaka = max(redaka, redakaTeksta(c.tekst, c.sirina))
			}
			if redaka > 1 {
				l.Visina(l.Redak(), visinaRedaka(redaka))
			}
			l.Dodaj(T(p.Ime, xlsxw.TablicaPod),
				vrijednost(p.Sada, xlsxw.TablicaSredina), protok(p.SadaQ, p.QPrema),
				vrijednost(p.dan(0), xlsxw.TablicaSredina), protok(p.danQ(0), p.QPrema),
				vrijednost(p.dan(2), xlsxw.TablicaSredina), protok(p.danQ(2), p.QPrema),
				vrijednost(p.dan(zadnji), xlsxw.TablicaSredina), protok(p.danQ(zadnji), p.QPrema),
				T(kretanje, xlsxw.TablicaSredina), T(stanje, stilStanja), T(tekstNapomene, stilNapomene))
		}
	}

	for _, t := range premaNapomena {
		odlomak(t+".", xlsxw.Napomena)
	}

	// Usporedba s mađarskom prognozom
	var usporedba [][]xlsxw.Celija
	for _, g := range sazetakSkupine {
		for _, p := range sve[g.Voda] {
			if len(p.HU) <= zadnji || (p.HU[2] == nil && p.HU[zadnji] == nil) {
				continue
			}
			red := []xlsxw.Celija{T(p.Ime, xlsxw.TablicaPod)}
			for _, i := range []int{2, zadnji} {
				nasa, hu := p.dan(i), p.HU[i]
				red = append(red, vrijednost(nasa, xlsxw.TablicaSredina), vrijednost(hu, xlsxw.TablicaSredina))
				if nasa != nil && hu != nil {
					// pravi broj s predznakom iz formata ćelije, ne tekst:
					// Excel tekst koji izgleda kao broj označi upozorenjem
					d := math.Round(*nasa) - math.Round(*hu)
					stil := xlsxw.Razlika
					if math.Abs(d) >= 10 {
						stil = xlsxw.RazlikaIstakni
					}
					red = append(red, N(d, stil))
				} else {
					red = append(red, T("—", xlsxw.TablicaSredina))
				}
			}
			usporedba = append(usporedba, red)
		}
	}
	if len(usporedba) > 0 {
		naslov("Usporedba s mađarskom prognozom (hydroinfo.hu)")
		// zaglavlje u dva retka: termin preko triju stupaca, ispod naša · HU · razlika
		r0 := l.Redak()
		l.Dodaj(T("Postaja", xlsxw.Zaglavlje), T("Za 3 dana ("+tri+")", xlsxw.Zaglavlje), T("", xlsxw.Zaglavlje), T("", xlsxw.Zaglavlje),
			T(fmt.Sprintf("Za %d dana (%s)", zadnji+1, zadnjiDan), xlsxw.Zaglavlje), T("", xlsxw.Zaglavlje), T("", xlsxw.Zaglavlje))
		l.Dodaj(T("", xlsxw.Zaglavlje), T("naša", xlsxw.Zaglavlje), T("HU", xlsxw.Zaglavlje), T("razlika", xlsxw.Zaglavlje),
			T("naša", xlsxw.Zaglavlje), T("HU", xlsxw.Zaglavlje), T("razlika", xlsxw.Zaglavlje))
		l.Spoji(0, r0, 0, r0+1)
		l.Spoji(1, r0, 3, r0)
		l.Spoji(4, r0, 6, r0)
		l.Visina(r0, 18)
		l.Visina(r0+1, 18)
		for _, red := range usporedba {
			l.Dodaj(red...)
		}
		odlomak("HU je mađarska prognoza (hydroinfo.hu) za isti termin. Razlika je naša manje mađarska, u cm; 10 cm ili više istaknuto je bojom. Mađarska prognoza izlazi jednom dnevno, pa zna biti starija od naše.", xlsxw.Napomena)
	}

	l.Dodaj()
	odlomak("Vrijednosti su prognoza i nose nesigurnost; raspon, satne vrijednosti, grafovi, godišnji vodostaji i opis metode su na sljedećim listovima. "+
		"Najniži i najviši zabilježeni vodostaj su iz arhive i evidencije očitanja (dnevni srednjaci). "+
		"Protok je procjena iz krivulje protoka postaje (sivo); gdje krivulje nema, stupac je prazan.", xlsxw.Napomena)
}

// genitivPostaje daje genitiv imena postaje za rečenice sažetka
func genitivPostaje(ime string) string {
	switch ime {
	case "Ilok":
		return "Iloka"
	case "Vukovar":
		return "Vukovara"
	}
	return ime
}
