package web

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/obracun"
	"gocop/internal/service"
	"gocop/internal/xlsxw"
)

// Izvoz obračuna u Excel, u obliku koji računovodstvo već poznaje: list po
// branjenom području (i "sektor i ostali"), redak po osobi sa šest
// kategorija — stvarni sati, obračunski sati, bruto € — pa sveukupno, bruto
// satnica i ukupan bruto; ispod doprinosi 16,5 % i sveukupan iznos; na
// kraju rekapitulacija po područjima.
//
// Satnicu goCOP ne zna i ne pamti: to je plaća, računovodstvo je upiše u
// stupac iz prošle plaće, a formule izračunaju iznose. Natrag se ne uvozi.

// kategorija izvoza: naziv i razredi koje zbraja
type kategorijaIzvoza struct {
	Naziv   string
	Razredi []obracun.Razred
}

var kategorijeIzvoza = []kategorijaIzvoza{
	{"radni dan", []obracun.Razred{obracun.RRV, obracun.DRD}},
	{"noćni radni dan", []obracun.Razred{obracun.NRD}},
	{"dnevni subota", []obracun.Razred{obracun.VID}},
	{"noćni subota", []obracun.Razred{obracun.VIN}},
	{"dnevni nedjelja i blagdan", []obracun.Razred{obracun.BLD}},
	{"noćni nedjelja i blagdan", []obracun.Razred{obracun.BLN}},
}

// DoprinosiNaBruto je stopa doprinosa na bruto plaću, kako stoji u obračunu
const DoprinosiNaBruto = 0.165

// osobaUIzvozu su sati jedne osobe zbrojeni preko ureda i terena
type osobaUIzvozu struct {
	Ime        string
	Stvarni    map[obracun.Razred]float64
	Obracunski map[obracun.Razred]float64
}

// IzvoziObracun piše obračun razdoblja kao .xlsx
func (h *JournalsHandler) IzvoziObracun(w http.ResponseWriter, r *http.Request) {
	j, area, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	data := h.pageData(r)
	data.Journal, data.Area = j, area
	h.fillRights(&data)
	if !data.CanWrite {
		http.Error(w, "Obračun sati vidi tko piše u dnevnik", http.StatusForbidden)
		return
	}
	od, do := h.razdobljeObracuna(r, j)
	postavke := h.postavkeObracuna()
	nazivi := map[int]string{}
	if podrucja, err := h.users.ListAreas(j.CentarSektor); err == nil {
		for _, a := range podrucja {
			nazivi[a.ID] = a.Name
		}
	}
	obr, err := h.journals.Obracun(r.Context(), j, od, do, postavke.Kalendar(r.Context()), postavke.Koeficijenti(r.Context()), nazivi)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	centar := j.CentarNaziv
	if centar == "" {
		centar = models.Terms().Sector + " " + j.CentarSektor
	}
	knjiga := knjigaObracuna(obr, centar, od, do.AddDate(0, 0, -1))
	ime := fmt.Sprintf("Obracun_sati_%s_%s_%s.xlsx", service.OznakaIzNaziva(centar), od.Format("2006-01-02"), do.AddDate(0, 0, -1).Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+ime+`"`)
	if err := knjiga.Zapisi(w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// knjigaObracuna slaže radnu knjigu iz obračuna
func knjigaObracuna(obr service.Obracun, centar string, od, do time.Time) *xlsxw.Knjiga {
	k := &xlsxw.Knjiga{}
	razdoblje := fmt.Sprintf("od %s do %s", od.Format("02.01.2006."), do.Format("02.01.2006."))
	type zbrojLista struct {
		naziv  string
		list   string
		bruto  string // adresa ukupnog bruta
		dopr   string
		ukupno string
	}
	var listovi []zbrojLista

	for _, g := range obr.Grupe {
		l := k.NoviList(g.Za)
		l.Sirine = []float64{28}
		for range kategorijeIzvoza {
			l.Sirine = append(l.Sirine, 9, 11, 11)
		}
		l.Sirine = append(l.Sirine, 11, 12, 12, 14)
		l.Dodaj(xlsxw.T(models.Terms().OrgName, xlsxw.Naslov))
		l.Dodaj(xlsxw.T(centar, xlsxw.Podebljan))
		l.Dodaj(xlsxw.T("Obračun radnog vremena pri obrani od poplava u razdoblju "+razdoblje, xlsxw.Podebljan))
		l.Dodaj()
		l.Dodaj(xlsxw.T(g.Za, xlsxw.Podebljan))
		// zaglavlje: kategorija pa tri stupca ispod
		red1 := []xlsxw.Celija{xlsxw.T("")}
		red2 := []xlsxw.Celija{xlsxw.T("Ime i prezime", xlsxw.Podebljan)}
		for _, kat := range kategorijeIzvoza {
			red1 = append(red1, xlsxw.T(kat.Naziv, xlsxw.Podebljan), xlsxw.T(""), xlsxw.T(""))
			red2 = append(red2, xlsxw.T("sati", xlsxw.Podebljan), xlsxw.T("obračunski sati", xlsxw.Podebljan), xlsxw.T("bruto (€)", xlsxw.Podebljan))
		}
		red1 = append(red1, xlsxw.T("sveukupno", xlsxw.Podebljan), xlsxw.T(""), xlsxw.T(""), xlsxw.T(""))
		red2 = append(red2, xlsxw.T("sati", xlsxw.Podebljan), xlsxw.T("obračunski sati", xlsxw.Podebljan), xlsxw.T("bruto satnica (€)", xlsxw.Podebljan), xlsxw.T("ukupan bruto (€)", xlsxw.Podebljan))
		l.Dodaj(red1...)
		l.Dodaj(red2...)
		prviRedak := len(l.Redci) // redak prve osobe (0-based)

		for _, o := range osobeGrupe(g) {
			r := len(l.Redci)
			red := []xlsxw.Celija{xlsxw.T(o.Ime)}
			var satiAdrese, obrAdrese, brutoAdrese []string
			for i, kat := range kategorijeIzvoza {
				var sati, obrac float64
				for _, razred := range kat.Razredi {
					sati += o.Stvarni[razred]
					obrac += o.Obracunski[razred]
				}
				c := 1 + i*3
				satnica := xlsxw.Adresa(1+len(kategorijeIzvoza)*3+2, r)
				red = append(red, xlsxw.N(sati, xlsxw.Broj2), xlsxw.N(obrac, xlsxw.Broj2),
					xlsxw.F(fmt.Sprintf("ROUND(%s*%s,2)", xlsxw.Adresa(c+1, r), satnica), 0, xlsxw.Broj2))
				satiAdrese = append(satiAdrese, xlsxw.Adresa(c, r))
				obrAdrese = append(obrAdrese, xlsxw.Adresa(c+1, r))
				brutoAdrese = append(brutoAdrese, xlsxw.Adresa(c+2, r))
			}
			var ukSati, ukObr float64
			for _, razred := range obracun.Razredi {
				ukSati += o.Stvarni[razred]
				ukObr += o.Obracunski[razred]
			}
			red = append(red,
				xlsxw.F(strings.Join(satiAdrese, "+"), ukSati, xlsxw.Broj2Pod),
				xlsxw.F(strings.Join(obrAdrese, "+"), ukObr, xlsxw.Broj2Pod),
				xlsxw.N(0, xlsxw.Broj2), // satnica: upisuje računovodstvo
				xlsxw.F(strings.Join(brutoAdrese, "+"), 0, xlsxw.Broj2Pod))
			l.Dodaj(red...)
		}
		zadnji := len(l.Redci) - 1
		// zbroj po stupcima
		zbroj := []xlsxw.Celija{xlsxw.T("UKUPNO", xlsxw.Podebljan)}
		stupaca := 1 + len(kategorijeIzvoza)*3 + 4
		for c := 1; c < stupaca; c++ {
			if c == stupaca-2 { // satnica se ne zbraja
				zbroj = append(zbroj, xlsxw.T(""))
				continue
			}
			var v float64
			for r := prviRedak; r <= zadnji; r++ {
				if c < len(l.Redci[r]) {
					v += l.Redci[r][c].Broj
				}
			}
			zbroj = append(zbroj, xlsxw.F(fmt.Sprintf("SUM(%s:%s)", xlsxw.Adresa(c, prviRedak), xlsxw.Adresa(c, zadnji)), v, xlsxw.Broj2Pod))
		}
		l.Dodaj(zbroj...)
		zbrojRedak := len(l.Redci) - 1
		brutoAdresa := xlsxw.Adresa(stupaca-1, zbrojRedak)
		l.Dodaj()
		l.Dodaj(xlsxw.T("Doprinosi na bruto: 16,50 %"), xlsxw.F(fmt.Sprintf("ROUND(%s*%s,2)", brutoAdresa, strings.ReplaceAll(fmt.Sprintf("%.3f", DoprinosiNaBruto), ",", ".")), 0, xlsxw.Broj2))
		doprAdresa := xlsxw.Adresa(1, len(l.Redci)-1)
		l.Dodaj(xlsxw.T("SVEUKUPAN IZNOS:", xlsxw.Podebljan), xlsxw.F(brutoAdresa+"+"+doprAdresa, 0, xlsxw.Broj2Pod))
		ukAdresa := xlsxw.Adresa(1, len(l.Redci)-1)
		l.Dodaj()
		l.Dodaj(xlsxw.T("Bruto satnicu po osobi upisuje računovodstvo iz prošle plaće; iznosi se izračunaju sami. Obračunski sati zaokruženi su po razredu na pola sata, sredina djelatniku."))
		listovi = append(listovi, zbrojLista{g.Za, l.Naziv, brutoAdresa, doprAdresa, ukAdresa})
	}

	// rekapitulacija
	rk := k.NoviList("REKAPITULACIJA")
	rk.Sirine = []float64{6, 34, 14, 16, 14}
	rk.Dodaj(xlsxw.T(models.Terms().OrgName, xlsxw.Naslov))
	rk.Dodaj(xlsxw.T(centar, xlsxw.Podebljan))
	rk.Dodaj(xlsxw.T("Rekapitulacija troškova, razdoblje "+razdoblje, xlsxw.Podebljan))
	rk.Dodaj()
	rk.Dodaj(xlsxw.T("r.br.", xlsxw.Podebljan), xlsxw.T("Branjeno područje", xlsxw.Podebljan), xlsxw.T("Bruto iznos", xlsxw.Podebljan), xlsxw.T("doprinos na bruto", xlsxw.Podebljan), xlsxw.T("ukupan iznos", xlsxw.Podebljan))
	prvi := len(rk.Redci)
	for i, z := range listovi {
		ref := func(adresa string) string { return "'" + z.list + "'!" + adresa }
		rk.Dodaj(xlsxw.T(fmt.Sprintf("%d.", i+1)), xlsxw.T(z.naziv), xlsxw.F(ref(z.bruto), 0, xlsxw.Broj2), xlsxw.F(ref(z.dopr), 0, xlsxw.Broj2), xlsxw.F(ref(z.ukupno), 0, xlsxw.Broj2))
	}
	zadnji := len(rk.Redci) - 1
	if zadnji >= prvi {
		rk.Dodaj(xlsxw.T(""), xlsxw.T("UKUPNO "+strings.ToUpper(centar)+":", xlsxw.Podebljan),
			xlsxw.F(fmt.Sprintf("SUM(%s:%s)", xlsxw.Adresa(2, prvi), xlsxw.Adresa(2, zadnji)), 0, xlsxw.Broj2Pod),
			xlsxw.F(fmt.Sprintf("SUM(%s:%s)", xlsxw.Adresa(3, prvi), xlsxw.Adresa(3, zadnji)), 0, xlsxw.Broj2Pod),
			xlsxw.F(fmt.Sprintf("SUM(%s:%s)", xlsxw.Adresa(4, prvi), xlsxw.Adresa(4, zadnji)), 0, xlsxw.Broj2Pod))
	}
	rk.Dodaj()
	rk.Dodaj(xlsxw.T("Stvarni sati u razdoblju: " + satiTekst(obr.Stvarni) + " h; obračunski sati: " + strings.ReplaceAll(fmt.Sprintf("%.1f", obr.Obracunski), ".", ",")))

	// po razredima, za provjeru
	rz := k.NoviList("Po razredima")
	rz.Sirine = []float64{28, 22, 8}
	red := []xlsxw.Celija{xlsxw.T("Ime i prezime", xlsxw.Podebljan), xlsxw.T("Za", xlsxw.Podebljan), xlsxw.T("Mjesto", xlsxw.Podebljan)}
	for _, razred := range obracun.Razredi {
		rz.Sirine = append(rz.Sirine, 9, 9)
		red = append(red, xlsxw.T(razred.Kratko()+" sati", xlsxw.Podebljan), xlsxw.T(razred.Kratko()+" obr.", xlsxw.Podebljan))
	}
	red = append(red, xlsxw.T("sati", xlsxw.Podebljan), xlsxw.T("obračunski", xlsxw.Podebljan))
	rz.Dodaj(red...)
	for _, g := range obr.Grupe {
		for _, r := range g.Redovi {
			mjesto := "ured"
			if r.Mjesto == models.MjestoTeren {
				mjesto = "teren"
			}
			red := []xlsxw.Celija{xlsxw.T(r.UserName), xlsxw.T(g.Za), xlsxw.T(mjesto)}
			po := map[obracun.Razred]service.ObracunStavka{}
			for _, st := range r.Stavke {
				po[st.Razred] = st
			}
			for _, razred := range obracun.Razredi {
				red = append(red, xlsxw.N(po[razred].Sati.Hours(), xlsxw.Broj2), xlsxw.N(po[razred].Obracunski, xlsxw.Broj2))
			}
			red = append(red, xlsxw.N(r.Stvarni.Hours(), xlsxw.Broj2Pod), xlsxw.N(r.Obracunski, xlsxw.Broj2Pod))
			rz.Dodaj(red...)
		}
	}
	return k
}

// osobeGrupe zbraja retke grupe po osobi preko ureda i terena — u obrascu
// je jedan redak po osobi, a koeficijenti su već u obračunskim satima
func osobeGrupe(g service.ObracunGrupa) []osobaUIzvozu {
	var out []osobaUIzvozu
	idx := map[string]int{}
	for _, r := range g.Redovi {
		i, ok := idx[r.UserID]
		if !ok {
			i = len(out)
			idx[r.UserID] = i
			out = append(out, osobaUIzvozu{Ime: r.UserName, Stvarni: map[obracun.Razred]float64{}, Obracunski: map[obracun.Razred]float64{}})
		}
		for _, st := range r.Stavke {
			out[i].Stvarni[st.Razred] += st.Sati.Hours()
			out[i].Obracunski[st.Razred] += st.Obracunski
		}
	}
	return out
}

func satiTekst(d time.Duration) string {
	return fmt.Sprintf("%d:%02d", int(d.Hours()), int(d.Minutes())%60)
}
