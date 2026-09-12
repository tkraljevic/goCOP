package web

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

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

// kategorija izvoza: naziv, razredi čiji se sati ispisuju, i razredi čiji
// obračunski sati ulaze. Redovno radno vrijeme (8–16 radnim danom) nije
// prekovremeni sat i u obrascu se ne broji kao sat — ali na terenu nosi
// koeficijent 0,2, pa u obračunske sate ulazi. Tako je u isplaćenom obračunu:
// 33 sata radnog dana daju 72 obračunska.
type kategorijaIzvoza struct {
	Naziv   string
	Sati    []obracun.Razred
	Razredi []obracun.Razred
}

var kategorijeIzvoza = []kategorijaIzvoza{
	{"radni dan", []obracun.Razred{obracun.DRD}, []obracun.Razred{obracun.RRV, obracun.DRD}},
	{"noćni radni dan", []obracun.Razred{obracun.NRD}, []obracun.Razred{obracun.NRD}},
	{"dnevni subota", []obracun.Razred{obracun.VID}, []obracun.Razred{obracun.VID}},
	{"noćni subota", []obracun.Razred{obracun.VIN}, []obracun.Razred{obracun.VIN}},
	{"dnevni nedjelja i blagdan", []obracun.Razred{obracun.BLD}, []obracun.Razred{obracun.BLD}},
	{"noćni nedjelja i blagdan", []obracun.Razred{obracun.BLN}, []obracun.Razred{obracun.BLN}},
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
	z := ZaglavljeIzvoza{Organizacija: strings.ToUpper(models.Terms().OrgName), Odjel: strings.ToUpper(models.Terms().SectorOffice),
		Sektor: j.CentarSektor, Mjesto: strings.TrimSpace(strings.TrimPrefix(centar, models.Terms().CenterShort)), Datum: time.Now().In(models.Zagreb)}
	if sektori, err := h.users.ListSectors(); err == nil {
		for _, sk := range sektori {
			if sk.ID == j.CentarSektor {
				z.OdjelNaziv = strings.ToUpper(sk.VgoName)
			}
		}
	}
	// Potpisuju voditelj centra i rukovoditelj sektora — tko god to sad jest
	for _, p := range []struct {
		uloga    models.Role
		funkcija string
	}{
		{models.RoleCopLeader, "voditelj Centra obrane od poplava Sektora " + j.CentarSektor},
		{models.RoleSectorLeader, "Rukovoditelj obrane od poplava Sektora " + j.CentarSektor},
	} {
		ime := ""
		if osobe, err := h.users.ListUsers(j.CentarSektor, 0, string(p.uloga), "", ""); err == nil && len(osobe) > 0 {
			ime = osobe[0].FullName
			if osobe[0].Title != "" {
				ime += ", " + osobe[0].Title
			}
		}
		z.Potpisnici = append(z.Potpisnici, PotpisnikIzvoza{Funkcija: p.funkcija, Ime: ime})
	}
	knjiga := knjigaObracuna(obr, z, od, do.AddDate(0, 0, -1))
	ime := fmt.Sprintf("Obracun_sati_%s_%s_%s.xlsx", service.OznakaIzNaziva(centar), od.Format("2006-01-02"), do.AddDate(0, 0, -1).Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+ime+`"`)
	if err := knjiga.Zapisi(w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// PotpisnikIzvoza je tko potpisuje obračun: naziv funkcije i ime s titulom
type PotpisnikIzvoza struct {
	Funkcija string
	Ime      string
}

// ZaglavljeIzvoza je ono što na listu stoji iznad tablice i ispod nje
type ZaglavljeIzvoza struct {
	Organizacija string // HRVATSKE VODE
	Odjel        string // VODNOGOSPODARSKI ODJEL
	OdjelNaziv   string // ZA DUNAV I DONJU DRAVU, OSIJEK
	Sektor       string // B
	Mjesto       string // Osijek
	Datum        time.Time
	Potpisnici   []PotpisnikIzvoza
}

// knjigaObracuna slaže radnu knjigu točno u obliku dosadašnjeg obračuna
// (Prekovremeni_<mjesec>_<godina>.xlsx): list BP_<broj> po području i
// <sektor>_i_ostali, stupac A prazan, UKUPNO iznad osoba, imena velikim
// slovima, stupac AB s kontrolom, potpisi dolje; REKAPITULACIJA na kraju.
func knjigaObracuna(obr service.Obracun, z ZaglavljeIzvoza, od, do time.Time) *xlsxw.Knjiga {
	k := &xlsxw.Knjiga{}
	razdoblje := fmt.Sprintf("od %s do %s", od.Format("02.01.2006."), do.Format("02.01.2006."))
	type zbrojLista struct {
		naziv, list, bruto, dopr, ukupno string
	}
	var listovi []zbrojLista
	B := xlsxw.T
	prazno := xlsxw.T("")

	for _, g := range obr.Grupe {
		naziv, list := "Sektor "+z.Sektor+" i ostali", z.Sektor+"_i_ostali"
		if g.Podrucje != nil {
			naziv, list = fmt.Sprintf("Branjeno područje %d", *g.Podrucje), fmt.Sprintf("BP_%d", *g.Podrucje)
		}
		l := k.NoviList(list)
		l.Sirine = []float64{3, 26}
		for range kategorijeIzvoza {
			l.Sirine = append(l.Sirine, 8, 11, 11)
		}
		l.Sirine = append(l.Sirine, 11, 12, 3, 11, 13, 3, 3, 9)
		l.Dodaj(prazno, B(z.Organizacija, xlsxw.Naslov))
		l.Dodaj(prazno, B(z.Odjel, xlsxw.Podebljan))
		l.Dodaj(prazno, B(z.OdjelNaziv, xlsxw.Podebljan))
		l.Dodaj(prazno, B("Obračun radnog vremena pri obrani od poplava u razdoblju "+razdoblje, xlsxw.Podebljan))
		l.Dodaj()
		red6 := []xlsxw.Celija{prazno, B(naziv, xlsxw.Podebljan), B("SUMARNO", xlsxw.Podebljan)}
		for len(red6) < 20 {
			red6 = append(red6, prazno)
		}
		red6 = append(red6, B("SATI SVEUKUPNO", xlsxw.Podebljan), prazno, prazno, B("BRUTO", xlsxw.Podebljan))
		l.Dodaj(red6...)
		red7 := []xlsxw.Celija{prazno, prazno}
		for _, kat := range kategorijeIzvoza {
			red7 = append(red7, B("Sati "+kat.Naziv, xlsxw.Podebljan), B("OBRAČUNSKI SATI "+kat.Naziv, xlsxw.Podebljan), B(kat.Naziv+" BRUTO (€)", xlsxw.Podebljan))
		}
		red7 = append(red7, B("SATI SVEUKUPNO", xlsxw.Podebljan), B("OBRAČUNSKI SATI SVEUKUPNO", xlsxw.Podebljan), prazno, B("BRUTO satnica (€)", xlsxw.Podebljan), B("Ukupan BRUTO iznos (€)", xlsxw.Podebljan))
		l.Dodaj(red7...)

		osobe := osobeGrupe(g)
		ukupnoRedak := len(l.Redci)        // 0-based redak UKUPNO
		prvi := ukupnoRedak + 1            // prva osoba
		zadnji := ukupnoRedak + len(osobe) // zadnja osoba
		if len(osobe) == 0 {
			zadnji = prvi
		}
		// stupci: 2..19 kategorije, 20 U sati, 21 V obr, 22 W prazno, 23 X satnica, 24 Y bruto, 27 AB kontrola
		const cU, cV, cX, cY, cAB = 20, 21, 23, 24, 27
		suma := func(c int, v float64) xlsxw.Celija {
			return xlsxw.F(fmt.Sprintf("SUM(%s:%s)", xlsxw.Adresa(c, prvi), xlsxw.Adresa(c, zadnji)), v, xlsxw.Broj2Pod)
		}
		zbrojPo := func(c int) float64 {
			var v float64
			for _, o := range osobe {
				v += vrijednostStupca(o, c)
			}
			return v
		}
		ukupno := []xlsxw.Celija{prazno, B("UKUPNO:", xlsxw.Podebljan)}
		for c := 2; c <= cV; c++ {
			ukupno = append(ukupno, suma(c, zbrojPo(c)))
		}
		ukupno = append(ukupno, prazno, prazno, suma(cY, 0), prazno, prazno,
			xlsxw.FT(fmt.Sprintf(`IF(ROUND(%s+%s+%s+%s+%s+%s-%s,0)=0,"DOBRO","GREŠKA")`,
				xlsxw.Adresa(4, ukupnoRedak), xlsxw.Adresa(7, ukupnoRedak), xlsxw.Adresa(10, ukupnoRedak), xlsxw.Adresa(13, ukupnoRedak), xlsxw.Adresa(16, ukupnoRedak), xlsxw.Adresa(19, ukupnoRedak), xlsxw.Adresa(cY, ukupnoRedak)), "DOBRO"))
		l.Dodaj(ukupno...)

		for _, o := range osobe {
			r := len(l.Redci)
			red := []xlsxw.Celija{prazno, B(strings.ToUpper(o.Ime))}
			var satiAdrese, obrAdrese, brutoAdrese []string
			for i, kat := range kategorijeIzvoza {
				c := 2 + i*3
				var sati, obrac float64
				for _, razred := range kat.Sati {
					sati += o.Stvarni[razred]
				}
				for _, razred := range kat.Razredi {
					obrac += o.Obracunski[razred]
				}
				red = append(red, xlsxw.N(sati, xlsxw.Broj2), xlsxw.N(obrac, xlsxw.Broj2),
					xlsxw.F(fmt.Sprintf("ROUND(%s*%s,2)", xlsxw.Adresa(c+1, r), xlsxw.Adresa(cX, r)), 0, xlsxw.Broj2))
				satiAdrese = append(satiAdrese, xlsxw.Adresa(c, r))
				obrAdrese = append(obrAdrese, xlsxw.Adresa(c+1, r))
				brutoAdrese = append(brutoAdrese, xlsxw.Adresa(c+2, r))
			}
			red = append(red,
				xlsxw.F(strings.Join(satiAdrese, "+"), vrijednostStupca(o, cU), xlsxw.Broj2Pod),
				xlsxw.F(strings.Join(obrAdrese, "+"), vrijednostStupca(o, cV), xlsxw.Broj2Pod),
				prazno,
				xlsxw.N(0, xlsxw.Broj2), // satnica: upisuje računovodstvo
				xlsxw.F(strings.Join(brutoAdrese, "+"), 0, xlsxw.Broj2Pod),
				prazno, prazno,
				xlsxw.FT(fmt.Sprintf(`IF(ROUND(%s*%s-%s,0)=0,"DOBRO","GREŠKA")`, xlsxw.Adresa(cV, r), xlsxw.Adresa(cX, r), xlsxw.Adresa(cY, r)), "DOBRO"))
			l.Dodaj(red...)
		}
		brutoAdresa := xlsxw.Adresa(cY, ukupnoRedak)
		l.Dodaj()
		dopr := make([]xlsxw.Celija, cY+1)
		dopr[1] = B("Doprinosi na bruto : 16,50%")
		dopr[cY] = xlsxw.F(fmt.Sprintf("ROUND(%s*0.165,2)", brutoAdresa), 0, xlsxw.Broj2)
		l.Dodaj(dopr...)
		doprAdresa := xlsxw.Adresa(cY, len(l.Redci)-1)
		sve := make([]xlsxw.Celija, cY+1)
		sve[1] = B("SVEUKUPAN IZNOS:", xlsxw.Podebljan)
		sve[cY] = xlsxw.F(brutoAdresa+"+"+doprAdresa, 0, xlsxw.Broj2Pod)
		l.Dodaj(sve...)
		ukAdresa := xlsxw.Adresa(cY, len(l.Redci)-1)
		l.Dodaj()
		l.Dodaj(prazno, B("Bruto satnicu po osobi upisuje računovodstvo iz prošle plaće u stupac X; iznosi se izračunaju sami. Sati radnog dana su prekovremeni (6–8 i 16–22); redovno radno vrijeme 8–16 nije sat, ali na terenu ulazi u obračunske s koeficijentom 0,2. Obračunski sati zaokruženi su po razredu na pola sata, sredina djelatniku."))
		// potpisi, kao na obrascu: mjesto i datum lijevo, funkcije pa imena
		for len(l.Redci) < ukupnoRedak+22 {
			l.Dodaj()
		}
		potpis := make([]xlsxw.Celija, cU+1)
		potpis[1] = B(z.Mjesto + ", " + z.Datum.Format("02.01.2006."))
		imena := make([]xlsxw.Celija, cU+1)
		for i, p := range z.Potpisnici {
			c := 13 + i*7 // N, U
			if c > cU {
				break
			}
			potpis[c] = B(p.Funkcija)
			imena[c] = B(p.Ime)
		}
		l.Dodaj(potpis...)
		l.Dodaj(imena...)
		listovi = append(listovi, zbrojLista{naziv, l.Naziv, brutoAdresa, doprAdresa, ukAdresa})
	}

	rk := k.NoviList("REKAPITULACIJA")
	rk.Sirine = []float64{8, 30, 14, 18, 14}
	for len(rk.Redci) < 8 {
		rk.Dodaj()
	}
	rk.Dodaj(B("Rekapitulacija troškova Sektora "+z.Sektor, xlsxw.Naslov))
	rk.Dodaj()
	rk.Dodaj(B("r.br.", xlsxw.Podebljan), B("Branjeno područje", xlsxw.Podebljan), B("Bruto iznos", xlsxw.Podebljan), B("doprinos na bruto", xlsxw.Podebljan), B("ukupan iznos", xlsxw.Podebljan))
	prvi := len(rk.Redci)
	for i, zl := range listovi {
		ref := func(adresa string) string { return "'" + zl.list + "'!" + adresa }
		rk.Dodaj(B(fmt.Sprintf("%d.", i+1)), B(zl.naziv), xlsxw.F(ref(zl.bruto), 0, xlsxw.Broj2), xlsxw.F(ref(zl.dopr), 0, xlsxw.Broj2), xlsxw.F(ref(zl.ukupno), 0, xlsxw.Broj2))
	}
	zadnji := len(rk.Redci) - 1
	if zadnji >= prvi {
		rk.Dodaj(B("UKUPNO SEKTOR "+z.Sektor+":", xlsxw.Podebljan), prazno,
			xlsxw.F(fmt.Sprintf("SUM(%s:%s)", xlsxw.Adresa(2, prvi), xlsxw.Adresa(2, zadnji)), 0, xlsxw.Broj2Pod),
			xlsxw.F(fmt.Sprintf("SUM(%s:%s)", xlsxw.Adresa(3, prvi), xlsxw.Adresa(3, zadnji)), 0, xlsxw.Broj2Pod),
			xlsxw.F(fmt.Sprintf("SUM(%s:%s)", xlsxw.Adresa(4, prvi), xlsxw.Adresa(4, zadnji)), 0, xlsxw.Broj2Pod))
	}
	rk.Dodaj()
	rk.Dodaj(B("Stvarni sati u razdoblju: " + satiTekst(obr.Stvarni) + " h; obračunski sati: " + strings.ReplaceAll(fmt.Sprintf("%.1f", obr.Obracunski), ".", ",")))
	for len(rk.Redci) < 28 {
		rk.Dodaj()
	}
	rk.Dodaj(prazno, B(z.Mjesto+", "+z.Datum.Format("02.01.2006.")))

	// po razredima, za provjeru
	rz := k.NoviList("Po razredima")
	rz.Sirine = []float64{28, 22, 8}
	red := []xlsxw.Celija{B("Ime i prezime", xlsxw.Podebljan), B("Za", xlsxw.Podebljan), B("Mjesto", xlsxw.Podebljan)}
	for _, razred := range obracun.Razredi {
		rz.Sirine = append(rz.Sirine, 9, 9)
		red = append(red, B(razred.Kratko()+" sati", xlsxw.Podebljan), B(razred.Kratko()+" obr.", xlsxw.Podebljan))
	}
	red = append(red, B("sati", xlsxw.Podebljan), B("obračunski", xlsxw.Podebljan))
	rz.Dodaj(red...)
	for _, g := range obr.Grupe {
		for _, r := range g.Redovi {
			mjesto := "ured"
			if r.Mjesto == models.MjestoTeren {
				mjesto = "teren"
			}
			red := []xlsxw.Celija{B(r.UserName), B(g.Za), B(mjesto)}
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

// vrijednostStupca daje brojku osobe u stupcu obrasca (2..21): sati ili
// obračunski po kategoriji, ili sveukupno — za izračunate vrijednosti zbroja
func vrijednostStupca(o osobaUIzvozu, c int) float64 {
	var v float64
	switch {
	case c == 20:
		for _, kat := range kategorijeIzvoza {
			for _, razred := range kat.Sati {
				v += o.Stvarni[razred]
			}
		}
	case c == 21:
		for _, razred := range obracun.Razredi {
			v += o.Obracunski[razred]
		}
	case c >= 2 && c <= 19:
		i, koji := (c-2)/3, (c-2)%3
		if koji == 2 {
			return 0 // bruto: satnica nepoznata
		}
		if koji == 0 {
			for _, razred := range kategorijeIzvoza[i].Sati {
				v += o.Stvarni[razred]
			}
		} else {
			for _, razred := range kategorijeIzvoza[i].Razredi {
				v += o.Obracunski[razred]
			}
		}
	}
	return v
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

// IzvoziIORS piše izvješće o radnim satima jedne osobe kao .xlsx, u obliku
// obrasca IORS: list s redcima po danu i list s obračunom po razredu.
func (h *JournalsHandler) IzvoziIORS(w http.ResponseWriter, r *http.Request) {
	j, area, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	data := h.pageData(r)
	data.Journal, data.Area = j, area
	h.fillRights(&data)
	userID := r.PathValue("user")
	if !data.CanWrite && (data.CurrentUser == nil || data.CurrentUser.ID.String() != userID) {
		http.Error(w, "Izvješće o satima vidi tko piše u dnevnik, i osoba sama", http.StatusForbidden)
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
	koef := postavke.Koeficijenti(r.Context())
	iors, err := h.journals.ObracunOsobe(r.Context(), j, userID, od, do, postavke.Kalendar(r.Context()), koef, nazivi)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if iors.UserName == "" {
		if id, err := uuid.Parse(userID); err == nil {
			if osoba, _ := h.users.GetUserByID(id); osoba != nil {
				iors.UserName = osoba.FullName
			}
		}
	}
	centar := j.CentarNaziv
	if centar == "" {
		centar = models.Terms().Sector + " " + j.CentarSektor
	}
	knjiga := knjigaIORS(iors, koef, centar, od, do.AddDate(0, 0, -1))
	ime := fmt.Sprintf("IORS_%s_%s_%s.xlsx", service.OznakaIzNaziva(iors.UserName), od.Format("2006-01-02"), do.AddDate(0, 0, -1).Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+ime+`"`)
	if err := knjiga.Zapisi(w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// knjigaIORS slaže obrazac IORS jedne osobe: list Izvjesce_o_radnim_satima
// (redak po danu i razmaku, sati po razredu) i list Obrazac_Obracuna
// (stvarni sati × koeficijent = obračunski, ured i teren odvojeno)
func knjigaIORS(iors service.IORS, koef obracun.Koeficijenti, centar string, od, do time.Time) *xlsxw.Knjiga {
	k := &xlsxw.Knjiga{}
	B := xlsxw.T
	prazno := xlsxw.T("")
	razdoblje := fmt.Sprintf("od %s do %s", od.Format("02.01.2006."), do.Format("02.01.2006."))

	l := k.NoviList("Izvjesce_o_radnim_satima")
	l.Sirine = []float64{13, 12, 7, 7, 46, 9, 8, 9, 9, 9, 9, 9, 9, 9}
	l.Dodaj(B(models.Terms().OrgName+" — "+centar, xlsxw.Podebljan))
	l.Dodaj(B("Izvješće o radnim satima ostvarenim pri provedbi obrane od poplava/leda:", xlsxw.Naslov))
	l.Dodaj(B("za razdoblje " + razdoblje))
	l.Dodaj()
	l.Dodaj(B("Ime i prezime sudionika u provedbi obrane od poplava/leda:"), prazno, prazno, prazno, prazno, prazno, B(strings.ToUpper(iors.UserName), xlsxw.Podebljan))
	l.Dodaj()
	red1 := []xlsxw.Celija{B("Dan u tjednu", xlsxw.Podebljan), B("Datum", xlsxw.Podebljan), B("Od", xlsxw.Podebljan), B("Do", xlsxw.Podebljan), B("Opis rada", xlsxw.Podebljan), B("Ukupno sati", xlsxw.Podebljan), B("Mjesto", xlsxw.Podebljan),
		B("RADNI DAN", xlsxw.Podebljan), prazno, prazno, B("SUBOTA", xlsxw.Podebljan), prazno, B("NEDJELJA I BLAGDAN", xlsxw.Podebljan), prazno}
	red2 := []xlsxw.Celija{prazno, prazno, prazno, prazno, prazno, prazno, prazno}
	for _, razred := range obracun.Razredi {
		red2 = append(red2, B(razred.Pojas(), xlsxw.Podebljan))
	}
	l.Dodaj(red1...)
	l.Dodaj(red2...)
	prvi := len(l.Redci)
	for _, r := range iors.Redovi {
		mjesto := "Ured"
		if r.Mjesto == models.MjestoTeren {
			mjesto = "Teren"
		}
		opis := r.Opis
		if !r.Potvrdeno {
			opis += " (čeka potvrdu — nije u obračunu)"
		}
		doTekst := r.Do.Format("15:04")
		if doTekst == "00:00" {
			doTekst = "24:00"
		}
		red := []xlsxw.Celija{B(strings.ToLower(danTjednaHR(r.Dan))), B(r.Dan.Format("02.01.2006.")), B(r.Od.Format("15:04")), B(doTekst), B(opis + " — " + r.Za),
			xlsxw.N(r.Ukupno.Hours(), xlsxw.Broj2Pod), B(mjesto)}
		for _, razred := range obracun.Razredi {
			if v := r.Sati[razred]; v > 0 && r.Potvrdeno {
				red = append(red, xlsxw.N(v.Hours(), xlsxw.Broj2))
			} else {
				red = append(red, prazno)
			}
		}
		l.Dodaj(red...)
	}
	zadnji := len(l.Redci) - 1
	sve := []xlsxw.Celija{B("SVEUKUPNO", xlsxw.Podebljan), prazno, prazno, prazno, prazno,
		xlsxw.F(fmt.Sprintf("SUM(%s:%s)", xlsxw.Adresa(5, prvi), xlsxw.Adresa(5, zadnji)), iors.Stvarni.Hours()+iors.CekaPotvrdu.Hours(), xlsxw.Broj2Pod), prazno}
	for i, razred := range obracun.Razredi {
		c := 7 + i
		sve = append(sve, xlsxw.F(fmt.Sprintf("SUM(%s:%s)", xlsxw.Adresa(c, prvi), xlsxw.Adresa(c, zadnji)), (iors.Ured[razred]+iors.Teren[razred]).Hours(), xlsxw.Broj2Pod))
	}
	if zadnji >= prvi {
		l.Dodaj(sve...)
	}
	l.Dodaj()
	l.Dodaj(B("Evidenciju vodio sudionik provedbe obrane:"), prazno, prazno, prazno, prazno, prazno, B(iors.UserName))
	l.Dodaj()
	l.Dodaj(B("Iz plana dežurstava goCOP-a; sati po zidnom satu, razmak preko ponoći podijeljen po danima. Nedjelja se obračunava kao blagdan."))

	o := k.NoviList("Obrazac_Obracuna")
	o.Sirine = []float64{8, 34, 12, 10, 14}
	o.Dodaj(B("OBRAČUN SATI RADNOG VREMENA PRI OBRANI OD POPLAVA", xlsxw.Naslov))
	o.Dodaj(B("za razdoblje " + razdoblje))
	o.Dodaj()
	o.Dodaj(B("IME I PREZIME"), B(strings.ToUpper(iors.UserName), xlsxw.Podebljan))
	o.Dodaj()
	o.Dodaj(B("Mjesto", xlsxw.Podebljan), B("Sati", xlsxw.Podebljan), B("stvarni sati", xlsxw.Podebljan), B("k", xlsxw.Podebljan), B("obračunski sati", xlsxw.Podebljan))
	var ukStvarni, ukObr float64
	for _, mjesto := range []obracun.Mjesto{obracun.Ured, obracun.Teren} {
		naziv := "URED"
		if mjesto == obracun.Teren {
			naziv = "TEREN"
		}
		for _, razred := range obracun.Razredi {
			sati := iors.Sat(mjesto, razred).Hours()
			obr := iors.Obr(mjesto, razred)
			o.Dodaj(B(naziv), B(razred.Dan()+" — "+razred.Pojas()), xlsxw.N(sati, xlsxw.Broj2), xlsxw.N(koef[mjesto][razred], xlsxw.Broj2), xlsxw.N(obr, xlsxw.Broj2))
			ukStvarni += sati
			ukObr += obr
		}
	}
	o.Dodaj(B("SVEUKUPNO", xlsxw.Podebljan), prazno, xlsxw.N(ukStvarni, xlsxw.Broj2Pod), prazno, xlsxw.N(ukObr, xlsxw.Broj2Pod))
	o.Dodaj()
	o.Dodaj(B("Obračunski sati zaokruženi su po razredu na pola sata, sredina djelatniku; zbroj je iz zaokruženih."))
	return k
}
