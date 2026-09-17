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

// IzvoziPotvrdu piše potvrdu jednog zahvata: otpremnicu za izdavanje i
// prijenos, primku za primljeno, povratnicu za povrat, potvrdu za ostalo
func (h *MtsHandler) IzvoziPotvrdu(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	redci, err := h.svc().Promet(r.Context(), repository.FiltarPrometa{VezaID: r.PathValue("veza")})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(redci) == 0 {
		http.NotFound(w, r)
		return
	}
	// skladište iz kojeg je zahvat: redak sa skladištem, kod izdavanja onaj negativni
	var sk *models.Skladiste
	for _, x := range redci {
		if x.SkladisteID != "" && (sk == nil || x.Kolicina < 0) {
			sk, _ = h.svc().Skladiste(r.Context(), x.SkladisteID)
		}
	}
	terms := models.Terms()
	z := ZaglavljeIzvoza{Organizacija: terms.OrgName, Datum: time.Now().In(models.Zagreb)}
	if terms.HasLogo() && terms.LogoMime == "image/png" {
		z.LogoPNG = terms.Logo
	}
	if sk != nil {
		z.Sektor = sk.Sektor
		for _, x := range data.Sektori {
			if x.ID == sk.Sektor {
				z.Odjel, z.Centar = x.VgoName, x.CenterCop
				z.Mjesto = strings.TrimSpace(strings.TrimPrefix(x.CenterCop, terms.CenterShort))
			}
		}
	}
	var obrana string
	if h.journals != nil && redci[0].JournalID != "" {
		if j, err := h.journals.GetJournal(r.Context(), redci[0].JournalID); err == nil && j != nil {
			obrana = j.DisplayTitle()
		}
	}
	naslov := nazivPotvrde(redci[0].Vrsta)
	posaljiXLSX(w, models.OznakaSredstva(naslov)+"_"+redci[0].Datum.Format("2006-01-02")+"_"+redci[0].VezaID[:8]+".xlsx", KnjigaPotvrde(redci, sk, obrana, z))
}

// nazivPotvrde je naslov dokumenta po vrsti zahvata
func nazivPotvrde(vrsta string) string {
	switch vrsta {
	case models.PrometIzdano, models.PrometPrijenos:
		return "Otpremnica"
	case models.PrometPrimka, models.PrometPocetno:
		return "Primka"
	case models.PrometPovrat:
		return "Povratnica"
	}
	return "Potvrda o zahvatu"
}

// KnjigaPotvrde slaže potvrdu zahvata na A4 uspravno: broj i datum, odakle
// i kamo, stavke, tko je naložio, tko preuzeo i tko upisao, s potpisima
func KnjigaPotvrde(redci []models.Promet, sk *models.Skladiste, obrana string, z ZaglavljeIzvoza) *xlsxw.Knjiga {
	k := &xlsxw.Knjiga{LogoPNG: z.LogoPNG}
	B := xlsxw.T
	const stupaca = 8
	prvi := redci[0]
	l := k.NoviList(nazivPotvrde(prvi.Vrsta))
	l.Uspravno = true
	l.Sirine = []float64{7, 30, 12, 8, 12, 12, 12, 14}
	broj := prvi.Dokument
	if broj == "" {
		broj = strings.ToUpper(prvi.VezaID[:8])
	}
	zaglavljeLista(l, z, strings.ToUpper(nazivPotvrde(prvi.Vrsta))+" br. "+broj, models.PrometNaziv(prvi.Vrsta)+" · "+prvi.Datum.Format("02.01.2006."), stupaca)

	polje := func(oznaka, tekst string) {
		if strings.TrimSpace(tekst) == "" {
			return
		}
		r := l.Redak()
		red := make([]xlsxw.Celija, stupaca)
		for c := range red {
			red[c] = B("", xlsxw.Tablica)
		}
		red[0], red[2] = B(oznaka, xlsxw.TablicaPod), B(tekst, xlsxw.TablicaTekst)
		l.Dodaj(red...)
		l.Spoji(0, r, 1, r)
		l.Spoji(2, r, stupaca-1, r)
		l.Visina(r, visinaTeksta(tekst, 70, 18, 0))
	}
	if sk != nil {
		odakle := sk.Naziv
		if sk.AreaName != "" {
			odakle = fmt.Sprintf("BP %d %s · %s", sk.AreaID, sk.AreaName, sk.Naziv)
		}
		if sk.Adresa != "" {
			odakle += ", " + sk.Adresa
		}
		switch prvi.Vrsta {
		case models.PrometPrimka, models.PrometPocetno, models.PrometPovrat:
			polje("Skladište (prima)", odakle)
		default:
			polje("Skladište (izdaje)", odakle)
		}
	}
	for _, x := range redci {
		if x.SkladisteID == "" {
			polje("Mjesto na terenu", x.MjestoNaziv())
			break
		}
		if sk != nil && x.SkladisteID != sk.ID && x.SkladisteNaziv != "" {
			polje("U skladište", x.SkladisteNaziv)
			break
		}
	}
	polje("Obrana", obrana)
	polje("Naložio", prvi.Nalozio)
	polje(models.StranaOznaka(prvi.Vrsta), prvi.Preuzeo)
	polje("Napomena", prvi.Napomena)

	l.Dodaj()
	r := l.Redak()
	l.Dodaj(B("R. br.", xlsxw.Zaglavlje), B("Vrsta sredstva", xlsxw.Zaglavlje), B("Oblik", xlsxw.Zaglavlje), B("Jed.", xlsxw.Zaglavlje), B("Količina", xlsxw.Zaglavlje),
		B("", xlsxw.Zaglavlje), B("", xlsxw.Zaglavlje), B("", xlsxw.Zaglavlje))
	l.Spoji(4, r, 7, r)
	l.Visina(r, 22)
	n := 0
	vidjeno := map[string]bool{}
	for _, x := range redci {
		// dvoredni potez (iz jednog mjesta u drugo) je jedna stavka
		kljuc := x.VrstaID + "|" + x.Oblik + "|" + kolicinaHR(absKolicina(x.Kolicina))
		if vidjeno[kljuc] {
			continue
		}
		vidjeno[kljuc] = true
		n++
		oblik := ""
		if x.Oblik != "" {
			oblik = models.OblikNaziv(x.Oblik)
		}
		rr := l.Redak()
		l.Dodaj(B(strconv.Itoa(n)+".", xlsxw.TablicaSredina), B(x.VrstaNaziv, xlsxw.Tablica), B(oblik, xlsxw.TablicaSredina), B(x.Jedinica, xlsxw.TablicaSredina),
			xlsxw.N(absKolicina(x.Kolicina), xlsxw.TablicaBrojPod), B("", xlsxw.TablicaBrojPod), B("", xlsxw.TablicaBrojPod), B("", xlsxw.TablicaBrojPod))
		l.Spoji(4, rr, 7, rr)
		l.Visina(rr, 20)
	}
	l.Dodaj()
	upisao := "Upisao u program: " + prvi.UserName + ", " + prvi.CreatedAt.In(models.Zagreb).Format("02.01.2006. 15:04")
	rr := l.Redak()
	red := make([]xlsxw.Celija, stupaca)
	red[0] = B(upisao, xlsxw.Napomena)
	l.Dodaj(red...)
	l.Spoji(0, rr, stupaca-1, rr)
	zp := z
	zp.Datum = prvi.Datum
	potpisnici := []PotpisnikIzvoza{{Funkcija: "izdao / zaprimio (skladištar)", Ime: prvi.UserName}}
	switch prvi.Vrsta {
	case models.PrometIzdano, models.PrometPrijenos, models.PrometPovrat, models.PrometPrimka:
		potpisnici = append(potpisnici, PotpisnikIzvoza{Funkcija: models.StranaOznaka(prvi.Vrsta), Ime: prvi.Preuzeo})
	}
	if prvi.Nalozio != "" {
		potpisnici = append(potpisnici, PotpisnikIzvoza{Funkcija: "naložio", Ime: prvi.Nalozio})
	}
	potpisiLista(l, zp, stupaca, potpisnici)
	return k
}

func absKolicina(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
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

	listPrometa(k, "KNJIGA PROMETA SREDSTAVA — "+sk.Naziv, "do "+dan.Format("02.01.2006.")+", najnoviji prvi", promet, z, false)
	return k
}

// listPrometa dodaje knjizi list s knjigom prometa: redak po retku, s
// mjestom, nalogom, preuzimateljem, dokumentom i onim tko je upisao
func listPrometa(k *xlsxw.Knjiga, naslov, podnaslov string, promet []models.Promet, z ZaglavljeIzvoza, saSkladistem bool) {
	B := xlsxw.T
	p := k.NoviList("Promet")
	p.Vodoravno = true
	stupaca := 12
	if saSkladistem {
		stupaca = 13
	}
	p.Sirine = []float64{11, 18, 30, 11, 11, 8, 30, 20, 20, 18, 18, 30}
	if saSkladistem {
		p.Sirine = append([]float64{11, 18, 24}, p.Sirine[2:]...)
	}
	zaglavljeLista(p, z, naslov, podnaslov, stupaca)
	r := p.Redak()
	glava := []xlsxw.Celija{B("Datum", xlsxw.Zaglavlje), B("Zahvat", xlsxw.Zaglavlje)}
	if saSkladistem {
		glava = append(glava, B("Skladište", xlsxw.Zaglavlje))
	}
	glava = append(glava, B("Sredstvo", xlsxw.Zaglavlje), B("Oblik", xlsxw.Zaglavlje), B("Količina", xlsxw.Zaglavlje), B("Jed.", xlsxw.Zaglavlje),
		B("Mjesto (teren / drugo skladište)", xlsxw.Zaglavlje), B("Naložio", xlsxw.Zaglavlje), B("Preuzeo / dopremio", xlsxw.Zaglavlje), B("Dokument", xlsxw.Zaglavlje), B("Upisao", xlsxw.Zaglavlje), B("Napomena", xlsxw.Zaglavlje))
	p.Dodaj(glava...)
	p.Visina(r, 30)
	p.PonoviRetke(r, r)
	for _, x := range promet {
		mjesto := ""
		if x.SkladisteID == "" {
			mjesto = x.MjestoNaziv()
		}
		red := []xlsxw.Celija{B(x.Datum.Format("02.01.2006."), xlsxw.TablicaSredina), B(models.PrometNaziv(x.Vrsta), xlsxw.Tablica)}
		if saSkladistem {
			red = append(red, B(x.SkladisteNaziv, xlsxw.Tablica))
		}
		red = append(red, B(x.VrstaNaziv, xlsxw.Tablica), B(models.OblikNaziv(x.Oblik), xlsxw.TablicaSredina), xlsxw.N(x.Kolicina, xlsxw.TablicaBroj), B(x.Jedinica, xlsxw.TablicaSredina),
			B(mjesto, xlsxw.TablicaTekst), B(x.Nalozio, xlsxw.TablicaTekst), B(x.Preuzeo, xlsxw.TablicaTekst), B(x.Dokument, xlsxw.Tablica), B(x.UserName, xlsxw.Tablica), B(x.Napomena, xlsxw.TablicaTekst))
		p.Dodaj(red...)
	}
	napomenaLista(p, "Količina sa znakom: u mjesto pozitivno, iz mjesta negativno. Redci istog poteza (izdavanje, prijenos, punjenje) idu u paru. Iz programa goCOP.", stupaca, 24)
}

// IzvoziPromet piše knjigu prometa po istim filtrima kao stranica
func (h *MtsHandler) IzvoziPromet(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	q := r.URL.Query()
	fl := repository.FiltarPrometa{Sektor: data.Sektor, SkladisteID: q.Get("skladiste"), VrstaID: q.Get("vrsta")}
	var opis []string
	if t, err := time.ParseInLocation("2006-01-02", q.Get("od"), models.Zagreb); err == nil {
		fl.Od = &t
		opis = append(opis, "od "+t.Format("02.01.2006."))
	}
	if t, err := time.ParseInLocation("2006-01-02", q.Get("do"), models.Zagreb); err == nil {
		fl.Do = &t
		opis = append(opis, "do "+t.Format("02.01.2006."))
	}
	naslov := "KNJIGA PROMETA SREDSTAVA"
	if fl.SkladisteID != "" {
		if sk, _ := h.svc().Skladiste(r.Context(), fl.SkladisteID); sk != nil {
			naslov += " — " + sk.Naziv
		}
	} else if data.Sektor != "" {
		naslov += " — " + models.Terms().Sector + " " + data.Sektor
	}
	if fl.VrstaID != "" {
		if v, _ := h.svc().Vrsta(r.Context(), fl.VrstaID); v != nil {
			opis = append(opis, v.Naziv)
		}
	}
	promet, err := h.svc().Promet(r.Context(), fl)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	terms := models.Terms()
	z := ZaglavljeIzvoza{Organizacija: terms.OrgName, Sektor: data.Sektor, Datum: time.Now().In(models.Zagreb)}
	if terms.HasLogo() && terms.LogoMime == "image/png" {
		z.LogoPNG = terms.Logo
	}
	for _, x := range data.Sektori {
		if x.ID == data.Sektor {
			z.Odjel, z.Centar = x.VgoName, x.CenterCop
		}
	}
	k := &xlsxw.Knjiga{LogoPNG: z.LogoPNG}
	podnaslov := strings.Join(opis, " · ")
	if podnaslov == "" {
		podnaslov = "sav promet, najnoviji prvi"
	}
	listPrometa(k, naslov, podnaslov, promet, z, fl.SkladisteID == "")
	posaljiXLSX(w, "MTS_promet_"+time.Now().In(models.Zagreb).Format("2006-01-02")+".xlsx", k)
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
