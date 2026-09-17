package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
	"gocop/internal/xlsxw"
)

// IzvoziTablicu piše popis sredstava sektora na dan kao .xlsx, u obliku
// koji sektori šalju Glavnom centru
func (h *MtsHandler) IzvoziTablicu(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if data.Sektor == "" {
		redirectWith(w, r, "/sredstva", "error", "Popis za Glavni centar radi se po sektoru; odaberite sektor")
		return
	}
	dan := time.Now().In(models.Zagreb)
	if t, err := time.ParseInLocation("2006-01-02", r.URL.Query().Get("dan"), models.Zagreb); err == nil {
		dan = t
	}
	t, err := h.svc().Tablica(r.Context(), data.Sektor, dan)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	terms := models.Terms()
	z := ZaglavljeIzvoza{Organizacija: terms.OrgName, Sektor: data.Sektor, Datum: time.Now().In(models.Zagreb)}
	if terms.HasLogo() && terms.LogoMime == "image/png" {
		z.LogoPNG = terms.Logo
	}
	nazivSektora := terms.Sector + " " + data.Sektor
	for _, sk := range data.Sektori {
		if sk.ID == data.Sektor {
			z.Odjel, z.Centar, nazivSektora = sk.VgoName, sk.CenterCop, sk.Name
			z.Mjesto = strings.TrimSpace(strings.TrimPrefix(sk.CenterCop, terms.CenterShort))
		}
	}
	posaljiXLSX(w, "MTS_"+strings.ToLower(data.Sektor)+"_"+dan.Format("2006-01-02")+".xlsx", KnjigaMts(t, nazivSektora, z))
}

// IzvoziSkladiste piše karticu skladišta kao .xlsx: stanje na dan po
// vrstama s oblicima, potrebe iz inventure, i knjigu prometa na drugom listu
func (h *MtsHandler) IzvoziSkladiste(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	sk, ok := h.ucitajSkladiste(w, r, &data)
	if !ok {
		return
	}
	dan := time.Now().In(models.Zagreb)
	if t, err := time.ParseInLocation("2006-01-02", r.URL.Query().Get("dan"), models.Zagreb); err == nil {
		dan = t
	}
	stanje, err := h.svc().StanjeSkladista(r.Context(), sk.ID, &dan)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var potrebe map[string]float64
	if p, _ := h.svc().Popisi(r.Context(), "", sk.ID, 0); len(p) > 0 {
		// potrebe iz zadnje inventure na taj dan ili prije
		for _, x := range p {
			if !x.Dan.After(dan) {
				potrebe = map[string]float64{}
				for _, st := range x.Stavke {
					potrebe[st.VrstaID] += st.Potrebno
				}
				break
			}
		}
	}
	promet, _ := h.svc().Promet(r.Context(), repository.FiltarPrometa{SkladisteID: sk.ID, Do: &dan})
	terms := models.Terms()
	z := ZaglavljeIzvoza{Organizacija: terms.OrgName, Sektor: sk.Sektor, Datum: time.Now().In(models.Zagreb)}
	if terms.HasLogo() && terms.LogoMime == "image/png" {
		z.LogoPNG = terms.Logo
	}
	for _, x := range data.Sektori {
		if x.ID == sk.Sektor {
			z.Odjel, z.Centar = x.VgoName, x.CenterCop
			z.Mjesto = strings.TrimSpace(strings.TrimPrefix(x.CenterCop, terms.CenterShort))
		}
	}
	posaljiXLSX(w, "MTS_"+models.OznakaSredstva(sk.Naziv)+"_"+dan.Format("2006-01-02")+".xlsx", KnjigaSkladista(sk, dan, stanje, potrebe, promet, z))
}

// KnjigaSkladista slaže karticu jednog skladišta: list stanja na dan (redak
// po vrsti, oblici u zasebnim stupcima, potrebe iz inventure) i list prometa
func KnjigaSkladista(sk *models.Skladiste, dan time.Time, stanje []models.StanjeVrste, potrebe map[string]float64, promet []models.Promet, z ZaglavljeIzvoza) *xlsxw.Knjiga {
	k := &xlsxw.Knjiga{LogoPNG: z.LogoPNG}
	B := xlsxw.T
	const stupaca = 8
	l := k.NoviList("Stanje")
	l.Uspravno = true
	l.Sirine = []float64{6, 34, 7, 12, 12, 12, 12, 24}
	podnaslov := sk.Naziv
	if sk.AreaName != "" {
		podnaslov = fmt.Sprintf("BP %d %s · %s", sk.AreaID, sk.AreaName, sk.Naziv)
	}
	if sk.Adresa != "" {
		podnaslov += " · " + sk.Adresa
	}
	zaglavljeLista(l, z, "STANJE SREDSTAVA ZA OBRANU OD POPLAVA NA DAN "+dan.Format("02.01.2006."), podnaslov, stupaca)
	r := l.Redak()
	l.Dodaj(B("R. br.", xlsxw.Zaglavlje), B("Vrsta sredstava", xlsxw.Zaglavlje), B("Jed.", xlsxw.Zaglavlje), B("Stanje na dan", xlsxw.Zaglavlje),
		B("od toga prazno", xlsxw.Zaglavlje), B("od toga napunjeno", xlsxw.Zaglavlje), B("Potrebe za nabavom", xlsxw.Zaglavlje), B("Napomena", xlsxw.Zaglavlje))
	l.Visina(r, 30)
	l.PonoviRetke(r, r)
	broj := func(v float64, stil int) xlsxw.Celija {
		if v == 0 {
			return B("", stil)
		}
		return xlsxw.N(v, stil)
	}
	for _, g := range models.GrupeSredstava {
		red := make([]xlsxw.Celija, stupaca)
		for c := range red {
			red[c] = B("", xlsxw.TablicaPod)
		}
		red[0], red[1] = B(g.Rimski, xlsxw.TablicaPod), B(g.Naziv, xlsxw.TablicaPod)
		l.Dodaj(red...)
		for _, sv := range stanje {
			if sv.Vrsta.Grupa != g.ID {
				continue
			}
			red := []xlsxw.Celija{B(strconv.Itoa(sv.Vrsta.Redoslijed)+".", xlsxw.TablicaSredina), B(sv.Vrsta.Naziv, xlsxw.Tablica), B(sv.Vrsta.Jedinica, xlsxw.TablicaSredina),
				broj(sv.Ukupno, xlsxw.TablicaBrojPod), B("", xlsxw.TablicaBroj), B("", xlsxw.TablicaBroj), broj(potrebe[sv.Vrsta.ID], xlsxw.TablicaBroj), B("", xlsxw.Tablica)}
			if sv.Vrsta.ImaOblike() {
				red[4], red[5] = broj(sv.Oblik(models.OblikPrazno), xlsxw.TablicaBroj), broj(sv.Oblik(models.OblikPunjeno), xlsxw.TablicaBroj)
			}
			l.Dodaj(red...)
		}
	}
	napomenaLista(l, "Stanje je zbroj prometa do toga dana. Potrebe za nabavom su iz zadnje inventure do toga dana. Iz programa goCOP.", stupaca, 24)
	potpisiLista(l, z, stupaca, []PotpisnikIzvoza{{Funkcija: "skladištar"}, {Funkcija: "rukovoditelj branjenog područja"}})

	// promet
	p := k.NoviList("Promet")
	p.Vodoravno = true
	p.Sirine = []float64{11, 18, 30, 11, 11, 8, 30, 20, 20, 18, 18, 30}
	zaglavljeLista(p, z, "KNJIGA PROMETA SREDSTAVA — "+sk.Naziv, "do "+dan.Format("02.01.2006.")+", najnoviji prvi", 12)
	r = p.Redak()
	p.Dodaj(B("Datum", xlsxw.Zaglavlje), B("Zahvat", xlsxw.Zaglavlje), B("Sredstvo", xlsxw.Zaglavlje), B("Oblik", xlsxw.Zaglavlje), B("Količina", xlsxw.Zaglavlje), B("Jed.", xlsxw.Zaglavlje),
		B("Mjesto (teren / drugo skladište)", xlsxw.Zaglavlje), B("Naložio", xlsxw.Zaglavlje), B("Preuzeo / dopremio", xlsxw.Zaglavlje), B("Dokument", xlsxw.Zaglavlje), B("Upisao", xlsxw.Zaglavlje), B("Napomena", xlsxw.Zaglavlje))
	p.Visina(r, 30)
	p.PonoviRetke(r, r)
	for _, x := range promet {
		mjesto := ""
		if x.SkladisteID == "" {
			mjesto = x.MjestoNaziv()
		}
		p.Dodaj(B(x.Datum.Format("02.01.2006."), xlsxw.TablicaSredina), B(models.PrometNaziv(x.Vrsta), xlsxw.Tablica), B(x.VrstaNaziv, xlsxw.Tablica),
			B(models.OblikNaziv(x.Oblik), xlsxw.TablicaSredina), xlsxw.N(x.Kolicina, xlsxw.TablicaBroj), B(x.Jedinica, xlsxw.TablicaSredina),
			B(mjesto, xlsxw.TablicaTekst), B(x.Nalozio, xlsxw.TablicaTekst), B(x.Preuzeo, xlsxw.TablicaTekst), B(x.Dokument, xlsxw.Tablica), B(x.UserName, xlsxw.Tablica), B(x.Napomena, xlsxw.TablicaTekst))
	}
	return k
}

// KnjigaMts slaže popis sredstava po skladištima kao list na A4 vodoravno:
// redak po vrsti iz propisanog popisa, blok od dva stupca po skladištu
// (stanje na dan, dodatne potrebe za nabavom) i zbroj sektora ispred njih —
// isti raspored kao tablica koja se dosad slala rukom.
func KnjigaMts(t *service.TablicaSredstava, nazivSektora string, z ZaglavljeIzvoza) *xlsxw.Knjiga {
	k := &xlsxw.Knjiga{LogoPNG: z.LogoPNG}
	B := xlsxw.T
	stupaca := 5 + 2*len(t.Skladista)
	if stupaca < 8 {
		stupaca = 8
	}
	l := k.NoviList("MTS " + t.Sektor)
	l.Vodoravno = true
	l.Sirine = make([]float64, stupaca)
	l.Sirine[0], l.Sirine[1], l.Sirine[2] = 5, 28, 6
	for c := 3; c < stupaca; c++ {
		l.Sirine[c] = 10
	}
	godina := t.Dan.Year()
	zaglavljeLista(l, z, "POPIS SREDSTAVA ZA OBRANU OD POPLAVA PO SKLADIŠTIMA", strings.ToUpper(nazivSektora)+" · stanje na dan "+t.Dan.Format("02.01.2006."), stupaca)

	// zaglavlje tablice: dva reda; skladišta spojena preko dva stupca
	r1 := l.Redak()
	red1 := make([]xlsxw.Celija, stupaca)
	red2 := make([]xlsxw.Celija, stupaca)
	for c := range red1 {
		red1[c], red2[c] = B("", xlsxw.Zaglavlje), B("", xlsxw.Zaglavlje)
	}
	red1[0], red1[1], red1[2] = B("Red. br.", xlsxw.Zaglavlje), B("Vrsta sredstava", xlsxw.Zaglavlje), B("Jed. mj.", xlsxw.Zaglavlje)
	red1[3] = B(strings.ToUpper(nazivSektora)+"\nSVEUKUPNO", xlsxw.Zaglavlje)
	red2[3] = B("Stanje na dan "+t.Dan.Format("02.01.2006."), xlsxw.Zaglavlje)
	red2[4] = B(fmt.Sprintf("Dodatne potrebe za nabavom u %d.", godina), xlsxw.Zaglavlje)
	for i, sk := range t.Skladista {
		c := 5 + 2*i
		naslov := sk.Naziv
		if sk.AreaName != "" {
			naslov = fmt.Sprintf("BP %d - %s\nSkladište: %s", sk.AreaID, strings.ToUpper(sk.AreaName), sk.Naziv)
		}
		if sk.Adresa != "" {
			naslov += "\n" + sk.Adresa
		}
		red1[c] = B(naslov, xlsxw.Zaglavlje)
		red2[c] = B("Stanje na dan "+t.Dan.Format("02.01.2006."), xlsxw.Zaglavlje)
		red2[c+1] = B(fmt.Sprintf("Dodatne potrebe za nabavom u %d.", godina), xlsxw.Zaglavlje)
	}
	l.Dodaj(red1...)
	l.Dodaj(red2...)
	l.Visina(r1, 54)
	l.Visina(r1+1, 40)
	l.Spoji(0, r1, 0, r1+1)
	l.Spoji(1, r1, 1, r1+1)
	l.Spoji(2, r1, 2, r1+1)
	l.Spoji(3, r1, 4, r1)
	for i := range t.Skladista {
		l.Spoji(5+2*i, r1, 6+2*i, r1)
	}
	l.PonoviRetke(r1, r1+1)

	broj := func(v float64, stil int) xlsxw.Celija {
		if v == 0 {
			return B("", stil)
		}
		return xlsxw.N(v, stil)
	}
	for _, g := range models.GrupeSredstava {
		red := make([]xlsxw.Celija, stupaca)
		for c := range red {
			red[c] = B("", xlsxw.TablicaPod)
		}
		red[0], red[1] = B(g.Rimski, xlsxw.TablicaPod), B(g.Naziv, xlsxw.TablicaPod)
		l.Dodaj(red...)
		for _, v := range t.Vrste {
			if v.Grupa != g.ID {
				continue
			}
			r := l.Redak()
			red := make([]xlsxw.Celija, stupaca)
			red[0] = B(strconv.Itoa(v.Redoslijed)+".", xlsxw.TablicaSredina)
			red[1] = B(v.Naziv, xlsxw.Tablica)
			red[2] = B(v.Jedinica, xlsxw.TablicaSredina)
			// zbroj sektora kao formula preko stupaca skladišta, s izračunatom vrijednošću
			var fs, fp []string
			for i := range t.Skladista {
				fs = append(fs, xlsxw.Adresa(5+2*i, r))
				fp = append(fp, xlsxw.Adresa(6+2*i, r))
			}
			if len(t.Skladista) > 0 {
				red[3] = xlsxw.F(strings.Join(fs, "+"), t.UkupnoStanje(v.ID), xlsxw.TablicaBrojPod)
				red[4] = xlsxw.F(strings.Join(fp, "+"), t.UkupnoPotrebe(v.ID), xlsxw.TablicaBrojPod)
			} else {
				red[3], red[4] = B("", xlsxw.TablicaBrojPod), B("", xlsxw.TablicaBrojPod)
			}
			for i, sk := range t.Skladista {
				red[5+2*i] = broj(t.StanjeU(sk.ID, v.ID), xlsxw.TablicaBroj)
				red[6+2*i] = broj(t.PotrebeU(sk.ID, v.ID), xlsxw.TablicaBroj)
			}
			l.Dodaj(red...)
		}
	}

	// odakle su stupci: popis ili knjiga
	l.Dodaj()
	var izvori []string
	for _, sk := range t.Skladista {
		izvori = append(izvori, sk.Naziv+": "+t.Izvor(sk.ID))
	}
	napomenaLista(l, "Stupac skladišta je iz inventure na taj dan kad je ima (prebrojano i potrebe za nabavom), inače iz knjige prometa. "+
		strings.Join(izvori, "; ")+". Vreće su zbrojene prazne i napunjene. Iz programa goCOP.", stupaca, 40)
	potpisiLista(l, z, stupaca, []PotpisnikIzvoza{{Funkcija: "sastavio"}, {Funkcija: "rukovoditelj obrane od poplava " + models.Terms().Lower("sektor") + "a " + t.Sektor}})
	return k
}
