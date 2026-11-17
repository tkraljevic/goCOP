package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/xlsxw"
)

// Excel potreba za nabavom sektora (crtež 19b, 9a): zaseban izvoz uz
// tablicu za Glavni centar, s vrstom po retku i skladištem po stupcu.

// zaglavljeSektora je zaglavlje izvoza za sektor: organizacija, grb,
// odjel i centar obrane
func zaglavljeSektora(sektori []models.Sector, sektor string) ZaglavljeIzvoza {
	terms := models.Terms()
	z := ZaglavljeIzvoza{Organizacija: terms.OrgName, Sektor: sektor, Datum: time.Now().In(models.Zagreb)}
	if terms.HasLogo() && terms.LogoMime == "image/png" {
		z.LogoPNG = terms.Logo
	}
	for _, s := range sektori {
		if s.ID == sektor {
			z.Odjel, z.Centar = s.VgoName, s.CenterCop
			z.Mjesto = strings.TrimSpace(strings.TrimPrefix(s.CenterCop, terms.CenterShort))
		}
	}
	return z
}

// skladistaSPotrebom su skladišta koja za godinu imaju barem jednu potrebu,
// redom kojim ih daje baza
func skladistaSPotrebom(skladista []models.Skladiste, potrebe []models.Potreba) []models.Skladiste {
	ima := map[string]bool{}
	for _, p := range potrebe {
		if p.Kolicina != 0 {
			ima[p.SkladisteID] = true
		}
	}
	var out []models.Skladiste
	for _, sk := range skladista {
		if ima[sk.ID] {
			out = append(out, sk)
		}
	}
	return out
}

// KnjigaPotreba slaže Excel potreba sektora za godinu: skupine kataloga,
// samo vrste s potrebom, zbroj i stupac po skladištu
func KnjigaPotreba(sektor string, godina int, vrste []models.VrstaSredstva, skladista []models.Skladiste, potrebe []models.Potreba, z ZaglavljeIzvoza) *xlsxw.Knjiga {
	k := &xlsxw.Knjiga{LogoPNG: z.LogoPNG}
	B := xlsxw.T
	skl := skladistaSPotrebom(skladista, potrebe)
	stupaca := 4 + len(skl)
	l := k.NoviList("Potrebe")
	l.Vodoravno = true
	l.Sirine = []float64{6, 34, 7, 12}
	glava := []xlsxw.Celija{B("R. br.", xlsxw.Zaglavlje), B("Vrsta sredstava", xlsxw.Zaglavlje), B("Jed.", xlsxw.Zaglavlje), B("Ukupno", xlsxw.Zaglavlje)}
	stupac := map[string]int{}
	for i, sk := range skl {
		l.Sirine = append(l.Sirine, 14)
		glava = append(glava, B(sk.Naziv, xlsxw.Zaglavlje))
		stupac[sk.ID] = 4 + i
	}
	zaglavljeLista(l, z, fmt.Sprintf("POTREBE ZA NABAVOM SREDSTAVA U %d.", godina), models.Terms().Sector+" "+sektor, stupaca)
	r := l.Redak()
	l.Dodaj(glava...)
	l.Visina(r, 30)
	l.PonoviRetke(r, r)
	for _, g := range models.GrupeSredstava {
		redci := redciPotreba(g.ID, vrste, potrebe, stupac, stupaca)
		if len(redci) == 0 {
			continue
		}
		skupina := make([]xlsxw.Celija, stupaca)
		for c := range skupina {
			skupina[c] = B("", xlsxw.TablicaPod)
		}
		skupina[0], skupina[1] = B(g.Rimski, xlsxw.TablicaPod), B(g.Naziv, xlsxw.TablicaPod)
		l.Dodaj(skupina...)
		for _, red := range redci {
			l.Dodaj(red...)
		}
	}
	napomenaLista(l, "Potrebe se upisuju na stranici skladišta. Iste brojke idu u tablicu za Glavni centar kao dodatne potrebe za nabavom u godini. Iz programa goCOP.", stupaca, 24)
	return k
}

// redciPotreba su redci jedne skupine: vrsta, jedinica, zbroj i količina po
// skladištu; vrsta bez potrebe se preskače
func redciPotreba(grupa string, vrste []models.VrstaSredstva, potrebe []models.Potreba, stupac map[string]int, stupaca int) [][]xlsxw.Celija {
	B := xlsxw.T
	var out [][]xlsxw.Celija
	for _, v := range vrste {
		if v.Grupa != grupa {
			continue
		}
		red := make([]xlsxw.Celija, stupaca)
		for c := 4; c < stupaca; c++ {
			red[c] = B("", xlsxw.TablicaBroj)
		}
		var ukupno float64
		for _, p := range potrebe {
			if c, ima := stupac[p.SkladisteID]; ima && p.VrstaID == v.ID && p.Kolicina != 0 {
				red[c] = xlsxw.N(p.Kolicina, xlsxw.TablicaBroj)
				ukupno += p.Kolicina
			}
		}
		if ukupno == 0 {
			continue
		}
		red[0], red[1], red[2] = B(strconv.Itoa(v.Redoslijed)+".", xlsxw.TablicaSredina), B(v.Naziv, xlsxw.Tablica), B(v.Jedinica, xlsxw.TablicaSredina)
		red[3] = xlsxw.N(ukupno, xlsxw.TablicaBrojPod)
		out = append(out, red)
	}
	return out
}

// IzvoziPotrebe piše Excel potreba sektora za godinu
func (h *MtsHandler) IzvoziPotrebe(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if data.Sektor == "" {
		redirectWith(w, r, "/sredstva/potrebe", "error", "Potrebe se izvoze po sektoru; odaberite sektor.")
		return
	}
	godina, _ := strconv.Atoi(r.URL.Query().Get("godina")) // bez godine ona koju nudi stranica
	if godina == 0 {
		godina = models.GodinaPotreba(time.Now().In(models.Zagreb))
	}
	potrebe, err := h.svc().PotrebeSektora(r.Context(), data.Sektor, godina)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	skladista, _ := h.svc().Skladista(r.Context(), data.Sektor, 0, false) // bez skladišta tablica ima samo zbroj
	vrste, _ := h.svc().Vrste(r.Context())                                // bez kataloga tablica je prazna
	ime := fmt.Sprintf("MTS_potrebe_%s_%d.xlsx", strings.ToLower(data.Sektor), godina)
	posaljiXLSX(w, ime, KnjigaPotreba(data.Sektor, godina, vrste, skladista, potrebe, zaglavljeSektora(data.Sektori, data.Sektor)))
}
