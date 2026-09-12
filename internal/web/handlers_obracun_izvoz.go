package web

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/obracun"
	"gocop/internal/service"
	"gocop/internal/xlsxw"
)

// Izvoz obračuna u Excel kao dokument za ispis na A4: logotip i naziv
// organizacije, odjela i centra u zaglavlju, naslov, razdoblje, tablica s
// obrubima, potpisi i podnožje s brojem stranice.
//
// Skupni obračun je u obliku koji računovodstvo već poznaje: list po
// branjenom području (i "sektor i ostali"), redak po osobi sa šest
// kategorija — stvarni sati, obračunski sati, bruto € — pa sveukupno, bruto
// satnica i ukupan bruto; ispod doprinosi 16,5 % i sveukupan iznos; na
// kraju rekapitulacija po područjima. Satnicu goCOP ne zna i ne pamti: to je
// plaća, računovodstvo je upiše iz prošle plaće, a formule izračunaju iznose.
// Natrag se ne uvozi.

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

// PotpisnikIzvoza je tko potpisuje obračun: naziv funkcije i ime s titulom
type PotpisnikIzvoza struct {
	Funkcija string
	Ime      string
}

// ZaglavljeIzvoza je ono što na listu stoji iznad tablice i ispod nje
type ZaglavljeIzvoza struct {
	Organizacija string // Hrvatske vode
	Odjel        string // Vodnogospodarski odjel za Dunav i donju Dravu, Osijek
	Centar       string // COP Osijek
	Sektor       string // B
	Mjesto       string // Osijek
	Datum        time.Time
	Potpisnici   []PotpisnikIzvoza
	LogoPNG      []byte
}

// ZaglavljeIzvozaDnevnika skuplja ono što na dokumentu stoji o organizaciji: nazive
// iz postavki i registra sektora, logotip, i tko potpisuje — tko god to sad jest
func (h *JournalsHandler) ZaglavljeIzvozaDnevnika(j *models.Journal) ZaglavljeIzvoza {
	t := models.Terms()
	centar := j.CentarNaziv
	if centar == "" {
		centar = t.Sector + " " + j.CentarSektor
	}
	z := ZaglavljeIzvoza{Organizacija: t.OrgName, Centar: centar, Sektor: j.CentarSektor,
		Mjesto: strings.TrimSpace(strings.TrimPrefix(centar, t.CenterShort)), Datum: time.Now().In(models.Zagreb)}
	if t.HasLogo() && t.LogoMime == "image/png" {
		z.LogoPNG = t.Logo
	}
	if sektori, err := h.users.ListSectors(); err == nil {
		for _, sk := range sektori {
			if sk.ID == j.CentarSektor {
				z.Odjel = sk.VgoName
			}
		}
	}
	for _, p := range []struct {
		uloga    models.Role
		funkcija string
	}{
		{models.RoleCopLeader, "voditelj Centra obrane od poplava " + t.Lower("sektor") + "a " + j.CentarSektor},
		{models.RoleSectorLeader, "rukovoditelj obrane od poplava " + t.Lower("sektor") + "a " + j.CentarSektor},
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
	return z
}

// nazivi područja po broju, za "za koga"
func (h *JournalsHandler) naziviPodrucja(sektor string) map[int]string {
	nazivi := map[int]string{}
	if podrucja, err := h.users.ListAreas(sektor); err == nil {
		for _, a := range podrucja {
			nazivi[a.ID] = a.Name
		}
	}
	return nazivi
}

// IzvoziObracun piše skupni obračun razdoblja kao .xlsx
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
	obr, err := h.journals.Obracun(r.Context(), j, od, do, postavke.Kalendar(r.Context()), postavke.Koeficijenti(r.Context()), h.naziviPodrucja(j.CentarSektor))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	z := h.ZaglavljeIzvozaDnevnika(j)
	knjiga := KnjigaObracuna(obr, z, od, do.AddDate(0, 0, -1))
	ime := fmt.Sprintf("Obracun_sati_%s_%s_%s.xlsx", service.OznakaIzNaziva(z.Centar), od.Format("2006-01-02"), do.AddDate(0, 0, -1).Format("2006-01-02"))
	posaljiXLSX(w, ime, knjiga)
}

// IzvoziIORS piše izvješće o radnim satima jedne osobe kao .xlsx, u obliku
// obrasca IORS, kao dokument za ispis
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
	koef := postavke.Koeficijenti(r.Context())
	iors, err := h.journals.ObracunOsobe(r.Context(), j, userID, od, do, postavke.Kalendar(r.Context()), koef, h.naziviPodrucja(j.CentarSektor))
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
	z := h.ZaglavljeIzvozaDnevnika(j)
	knjiga := KnjigaIORS(iors, koef, z, j.DisplayTitle(), od, do.AddDate(0, 0, -1))
	ime := fmt.Sprintf("IORS_%s_%s_%s.xlsx", service.OznakaIzNaziva(iors.UserName), od.Format("2006-01-02"), do.AddDate(0, 0, -1).Format("2006-01-02"))
	posaljiXLSX(w, ime, knjiga)
}

func posaljiXLSX(w http.ResponseWriter, ime string, knjiga *xlsxw.Knjiga) {
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+ime+`"`)
	if err := knjiga.Zapisi(w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

var prazno = xlsxw.T("")

// zaglavljeLista piše vrh dokumenta: logotip lijevo, organizacija, odjel i
// centar desno od njega, pa naslov i podnaslov preko cijele širine. Vraća
// sljedeći slobodan redak.
func zaglavljeLista(l *xlsxw.List, z ZaglavljeIzvoza, naslov, podnaslov string, stupaca int) int {
	l.Logo = len(z.LogoPNG) > 0
	l.Podnozje = "&L" + z.Organizacija + " · " + z.Centar + " · " + naslov + "&Rstranica &P od &N"
	pocetak := 2
	if !l.Logo {
		pocetak = 0
	}
	redak := func(tekst string, stil int, visina float64) {
		r := l.Redak()
		red := make([]xlsxw.Celija, stupaca)
		red[pocetak] = xlsxw.T(tekst, stil)
		l.Dodaj(red...)
		l.Spoji(pocetak, r, stupaca-1, r)
		l.Visina(r, visina)
	}
	// Četiri retka po 17 točaka su 2,4 cm: logotip od 2,2 cm stane u njih i
	// ne prelazi ispod naslova. Tekst stoji od stupca C, desno od logotipa.
	redak(z.Organizacija, xlsxw.Podnaslov, 17)
	redak(z.Odjel, xlsxw.Obican, 17)
	redak(z.Centar, xlsxw.Obican, 17)
	l.Dodaj()
	l.Visina(l.Redak()-1, 17)
	r := l.Redak()
	red := make([]xlsxw.Celija, stupaca)
	red[0] = xlsxw.T(naslov, xlsxw.Naslov)
	l.Dodaj(red...)
	l.Spoji(0, r, stupaca-1, r)
	l.Visina(r, 24)
	if podnaslov != "" {
		r = l.Redak()
		red = make([]xlsxw.Celija, stupaca)
		red[0] = xlsxw.T(podnaslov, xlsxw.Obican)
		l.Dodaj(red...)
		l.Spoji(0, r, stupaca-1, r)
	}
	l.Dodaj()
	l.Visina(l.Redak()-1, 8)
	return l.Redak()
}

// napomenaLista piše sitnu napomenu preko cijele širine
func napomenaLista(l *xlsxw.List, tekst string, stupaca int, visina float64) {
	r := l.Redak()
	red := make([]xlsxw.Celija, stupaca)
	red[0] = xlsxw.T(tekst, xlsxw.Napomena)
	l.Dodaj(red...)
	l.Spoji(0, r, stupaca-1, r)
	l.Visina(r, visina)
}

// potpisiLista piše mjesto i datum te potpisne crte s funkcijom i imenom
func potpisiLista(l *xlsxw.List, z ZaglavljeIzvoza, stupaca int, potpisnici []PotpisnikIzvoza) {
	l.Dodaj()
	l.Dodaj(xlsxw.T(z.Mjesto + ", " + z.Datum.Format("02.01.2006.")))
	l.Dodaj()
	l.Dodaj()
	// potpisi u dva stupca, razmaknuti
	sirina := stupaca / len(potpisnici)
	if sirina < 3 {
		sirina = 3
	}
	crte := make([]xlsxw.Celija, stupaca)
	funkcije := make([]xlsxw.Celija, stupaca)
	imena := make([]xlsxw.Celija, stupaca)
	r := l.Redak()
	for i, p := range potpisnici {
		c := i * sirina
		if c+2 >= stupaca {
			break
		}
		crte[c] = xlsxw.T("_______________________________", xlsxw.Obican)
		funkcije[c] = xlsxw.T(p.Funkcija, xlsxw.Napomena)
		imena[c] = xlsxw.T(p.Ime, xlsxw.Podebljan)
		l.Spoji(c, r, c+sirina-2, r)
		l.Spoji(c, r+1, c+sirina-2, r+1)
		l.Spoji(c, r+2, c+sirina-2, r+2)
	}
	l.Dodaj(crte...)
	l.Dodaj(funkcije...)
	l.Dodaj(imena...)
}

// osobaUIzvozu su sati jedne osobe zbrojeni preko ureda i terena
type osobaUIzvozu struct {
	Ime        string
	Stvarni    map[obracun.Razred]float64
	Obracunski map[obracun.Razred]float64
}

// KnjigaObracuna slaže radnu knjigu skupnog obračuna u obliku dosadašnjeg
// (Prekovremeni_<mjesec>_<godina>.xlsx): list BP_<broj> po području i
// <sektor>_i_ostali, stupac A prazan, UKUPNO iznad osoba, imena velikim
// slovima, stupac AB s kontrolom; REKAPITULACIJA na kraju.
func KnjigaObracuna(obr service.Obracun, z ZaglavljeIzvoza, od, do time.Time) *xlsxw.Knjiga {
	k := &xlsxw.Knjiga{LogoPNG: z.LogoPNG}
	razdoblje := fmt.Sprintf("od %s do %s", od.Format("02.01.2006."), do.Format("02.01.2006."))
	type zbrojLista struct {
		naziv, list, bruto, dopr, ukupno string
	}
	var listovi []zbrojLista
	B := xlsxw.T
	const stupaca = 25 // A..Y

	for _, g := range obr.Grupe {
		naziv, list := "Sektor "+z.Sektor+" i ostali", z.Sektor+"_i_ostali"
		if g.Podrucje != nil {
			naziv, list = fmt.Sprintf("Branjeno područje %d", *g.Podrucje), fmt.Sprintf("BP_%d", *g.Podrucje)
		}
		l := k.NoviList(list)
		l.Vodoravno = true
		l.Sirine = []float64{4, 26}
		for range kategorijeIzvoza {
			l.Sirine = append(l.Sirine, 8, 11, 11)
		}
		l.Sirine = append(l.Sirine, 11, 12, 2, 11, 13)
		zaglavljeLista(l, z, "Obračun radnog vremena pri obrani od poplava", "razdoblje "+razdoblje+" · "+naziv, stupaca)

		// zaglavlje tablice: dva reda, kategorije spojene preko tri stupca
		r6 := l.Redak()
		red6 := []xlsxw.Celija{prazno, B(naziv, xlsxw.Zaglavlje)}
		for _, kat := range kategorijeIzvoza {
			red6 = append(red6, B(kat.Naziv, xlsxw.Zaglavlje), B("", xlsxw.Zaglavlje), B("", xlsxw.Zaglavlje))
		}
		red6 = append(red6, B("SVEUKUPNO", xlsxw.Zaglavlje), B("", xlsxw.Zaglavlje), prazno, B("BRUTO", xlsxw.Zaglavlje), B("", xlsxw.Zaglavlje))
		l.Dodaj(red6...)
		red7 := []xlsxw.Celija{prazno, B("Ime i prezime", xlsxw.Zaglavlje)}
		for range kategorijeIzvoza {
			red7 = append(red7, B("sati", xlsxw.Zaglavlje), B("obračunski sati", xlsxw.Zaglavlje), B("bruto (€)", xlsxw.Zaglavlje))
		}
		red7 = append(red7, B("sati", xlsxw.Zaglavlje), B("obračunski sati", xlsxw.Zaglavlje), prazno, B("satnica (€)", xlsxw.Zaglavlje), B("ukupan bruto (€)", xlsxw.Zaglavlje))
		l.Dodaj(red7...)
		for i := range kategorijeIzvoza {
			l.Spoji(2+i*3, r6, 4+i*3, r6)
		}
		l.Spoji(20, r6, 21, r6)
		l.Spoji(23, r6, 24, r6)
		l.Spoji(1, r6, 1, r6+1)
		l.Visina(r6, 30)
		l.Visina(r6+1, 30)
		l.PonoviRetke(r6, r6+1)

		osobe := osobeGrupe(g)
		ukupnoRedak := l.Redak()
		prvi := ukupnoRedak + 1
		zadnji := ukupnoRedak + len(osobe)
		if len(osobe) == 0 {
			zadnji = prvi
		}
		// stupci: 2..19 kategorije, 20 U sati, 21 V obr, 22 W prazno, 23 X satnica, 24 Y bruto.
		// Stupca kontrole (DOBRO/GREŠKA) nema: postojao je za ručni prijepis.
		const cU, cV, cX, cY = 20, 21, 23, 24
		suma := func(c int, v float64) xlsxw.Celija {
			return xlsxw.F(fmt.Sprintf("SUM(%s:%s)", xlsxw.Adresa(c, prvi), xlsxw.Adresa(c, zadnji)), v, xlsxw.TablicaBrojPod)
		}
		zbrojPo := func(c int) float64 {
			var v float64
			for _, o := range osobe {
				v += vrijednostStupca(o, c)
			}
			return v
		}
		ukupno := []xlsxw.Celija{prazno, B("UKUPNO:", xlsxw.TablicaPod)}
		for c := 2; c <= cV; c++ {
			ukupno = append(ukupno, suma(c, zbrojPo(c)))
		}
		ukupno = append(ukupno, prazno, B("", xlsxw.TablicaPod), suma(cY, 0))
		l.Dodaj(ukupno...)

		for _, o := range osobe {
			r := l.Redak()
			red := []xlsxw.Celija{prazno, B(strings.ToUpper(o.Ime), xlsxw.Tablica)}
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
				red = append(red, xlsxw.N(sati, xlsxw.TablicaBroj), xlsxw.N(obrac, xlsxw.TablicaBroj),
					xlsxw.F(fmt.Sprintf("ROUND(%s*%s,2)", xlsxw.Adresa(c+1, r), xlsxw.Adresa(cX, r)), 0, xlsxw.TablicaBroj))
				satiAdrese = append(satiAdrese, xlsxw.Adresa(c, r))
				obrAdrese = append(obrAdrese, xlsxw.Adresa(c+1, r))
				brutoAdrese = append(brutoAdrese, xlsxw.Adresa(c+2, r))
			}
			red = append(red,
				xlsxw.F(strings.Join(satiAdrese, "+"), vrijednostStupca(o, cU), xlsxw.TablicaBrojPod),
				xlsxw.F(strings.Join(obrAdrese, "+"), vrijednostStupca(o, cV), xlsxw.TablicaBrojPod),
				prazno,
				xlsxw.N(0, xlsxw.TablicaBroj), // satnica: upisuje računovodstvo
				xlsxw.F(strings.Join(brutoAdrese, "+"), 0, xlsxw.TablicaBrojPod))
			l.Dodaj(red...)
		}
		brutoAdresa := xlsxw.Adresa(cY, ukupnoRedak)
		l.Dodaj()
		dopr := make([]xlsxw.Celija, cY+1)
		dopr[cY-3] = B("Doprinosi na bruto 16,50 %", xlsxw.Desno)
		dopr[cY] = xlsxw.F(fmt.Sprintf("ROUND(%s*0.165,2)", brutoAdresa), 0, xlsxw.TablicaBroj)
		l.Dodaj(dopr...)
		l.Spoji(cY-3, l.Redak()-1, cY-1, l.Redak()-1)
		doprAdresa := xlsxw.Adresa(cY, l.Redak()-1)
		sve := make([]xlsxw.Celija, cY+1)
		sve[cY-3] = B("SVEUKUPAN IZNOS (€)", xlsxw.Desno)
		sve[cY] = xlsxw.F(brutoAdresa+"+"+doprAdresa, 0, xlsxw.TablicaBrojPod)
		l.Dodaj(sve...)
		l.Spoji(cY-3, l.Redak()-1, cY-1, l.Redak()-1)
		ukAdresa := xlsxw.Adresa(cY, l.Redak()-1)
		l.Dodaj()
		napomenaLista(l, "Bruto satnicu po osobi upisuje računovodstvo iz prošle plaće u stupac „satnica“; iznosi se izračunaju sami. "+
			"Sati radnog dana su prekovremeni (6–8 i 16–22); redovno radno vrijeme 8–16 nije sat, ali na terenu ulazi u obračunske s koeficijentom 0,2. "+
			"Obračunski sati zaokruženi su po razredu na pola sata, sredina djelatniku. Nedjelja se obračunava kao blagdan.", stupaca, 30)
		potpisiLista(l, z, stupaca, z.Potpisnici)
		listovi = append(listovi, zbrojLista{naziv, l.Naziv, brutoAdresa, doprAdresa, ukAdresa})
	}

	rk := k.NoviList("REKAPITULACIJA")
	rk.Sirine = []float64{8, 34, 16, 18, 16}
	zaglavljeLista(rk, z, "Rekapitulacija troškova obrane od poplava", "razdoblje "+razdoblje+" · "+models.Terms().Sector+" "+z.Sektor, 5)
	rk.Dodaj(B("r. br.", xlsxw.Zaglavlje), B("Branjeno područje", xlsxw.Zaglavlje), B("Bruto iznos (€)", xlsxw.Zaglavlje), B("doprinos na bruto (€)", xlsxw.Zaglavlje), B("ukupan iznos (€)", xlsxw.Zaglavlje))
	rk.Visina(rk.Redak()-1, 28)
	prvi := rk.Redak()
	for i, zl := range listovi {
		ref := func(adresa string) string { return "'" + zl.list + "'!" + adresa }
		rk.Dodaj(B(fmt.Sprintf("%d.", i+1), xlsxw.TablicaSredina), B(zl.naziv, xlsxw.Tablica), xlsxw.F(ref(zl.bruto), 0, xlsxw.TablicaBroj), xlsxw.F(ref(zl.dopr), 0, xlsxw.TablicaBroj), xlsxw.F(ref(zl.ukupno), 0, xlsxw.TablicaBroj))
	}
	zadnji := rk.Redak() - 1
	if zadnji >= prvi {
		rk.Dodaj(B("", xlsxw.TablicaPod), B("UKUPNO "+strings.ToUpper(models.Terms().Sector)+" "+z.Sektor, xlsxw.TablicaPod),
			xlsxw.F(fmt.Sprintf("SUM(%s:%s)", xlsxw.Adresa(2, prvi), xlsxw.Adresa(2, zadnji)), 0, xlsxw.TablicaBrojPod),
			xlsxw.F(fmt.Sprintf("SUM(%s:%s)", xlsxw.Adresa(3, prvi), xlsxw.Adresa(3, zadnji)), 0, xlsxw.TablicaBrojPod),
			xlsxw.F(fmt.Sprintf("SUM(%s:%s)", xlsxw.Adresa(4, prvi), xlsxw.Adresa(4, zadnji)), 0, xlsxw.TablicaBrojPod))
	}
	rk.Dodaj()
	napomenaLista(rk, fmt.Sprintf("Stvarni sati u razdoblju: %s h; obračunski sati: %s. Iznosi se izračunaju kad računovodstvo upiše satnice na listovima po područjima.",
		satiTekst(obr.Stvarni), strings.ReplaceAll(fmt.Sprintf("%.1f", obr.Obracunski), ".", ",")), 5, 30)
	potpisiLista(rk, z, 5, z.Potpisnici)

	// po razredima, za provjeru
	rz := k.NoviList("Po razredima")
	rz.Vodoravno = true
	rz.Sirine = []float64{28, 22, 8}
	red := []xlsxw.Celija{B("Ime i prezime", xlsxw.Zaglavlje), B("Za", xlsxw.Zaglavlje), B("Mjesto", xlsxw.Zaglavlje)}
	for _, razred := range obracun.Razredi {
		rz.Sirine = append(rz.Sirine, 9, 9)
		red = append(red, B(razred.Kratko()+" sati", xlsxw.Zaglavlje), B(razred.Kratko()+" obr.", xlsxw.Zaglavlje))
	}
	red = append(red, B("sati", xlsxw.Zaglavlje), B("obračunski", xlsxw.Zaglavlje))
	rz.Dodaj(red...)
	rz.Visina(0, 40)
	rz.PonoviRetke(0, 0)
	for _, g := range obr.Grupe {
		for _, r := range g.Redovi {
			mjesto := "ured"
			if r.Mjesto == models.MjestoTeren {
				mjesto = "teren"
			}
			red := []xlsxw.Celija{B(r.UserName, xlsxw.Tablica), B(g.Za, xlsxw.Tablica), B(mjesto, xlsxw.Tablica)}
			po := map[obracun.Razred]service.ObracunStavka{}
			for _, st := range r.Stavke {
				po[st.Razred] = st
			}
			for _, razred := range obracun.Razredi {
				red = append(red, xlsxw.N(po[razred].Sati.Hours(), xlsxw.TablicaBroj), xlsxw.N(po[razred].Obracunski, xlsxw.TablicaBroj))
			}
			red = append(red, xlsxw.N(r.Stvarni.Hours(), xlsxw.TablicaBrojPod), xlsxw.N(r.Obracunski, xlsxw.TablicaBrojPod))
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

// KnjigaIORS slaže obrazac IORS jedne osobe kao jedan dokument: zaglavlje s
// logotipom, osnovno o obračunu, redak po danu i razmaku sa satima po
// razredu, obračun po razredu za ured i teren, potpisi.
func KnjigaIORS(iors service.IORS, koef obracun.Koeficijenti, z ZaglavljeIzvoza, obrana string, od, do time.Time) *xlsxw.Knjiga {
	k := &xlsxw.Knjiga{LogoPNG: z.LogoPNG}
	B := xlsxw.T
	const stupaca = 14 // A..N
	razdoblje := fmt.Sprintf("%s – %s", od.Format("02.01.2006."), do.Format("02.01.2006."))

	l := k.NoviList("IORS")
	l.Vodoravno = true
	l.Sirine = []float64{12, 12, 7, 7, 40, 9, 8, 9, 10, 9, 9, 9, 9, 9}
	zaglavljeLista(l, z, "Izvješće o radnim satima ostvarenim pri provedbi obrane od poplava/leda", "obrazac IORS · "+razdoblje, stupaca)

	// osnovno o obračunu: oznaka lijevo, vrijednost preko ostatka
	var za []string
	vidjeno := map[string]bool{}
	mjesta := map[string]bool{}
	for _, r := range iors.Redovi {
		if !vidjeno[r.Za] {
			vidjeno[r.Za] = true
			za = append(za, r.Za)
		}
		mjesta[strings.ToLower(r.Mjesto)] = true
	}
	sort.Strings(za)
	var mjestaTekst []string
	for _, m := range []string{"ured", "teren"} {
		if mjesta[m] {
			mjestaTekst = append(mjestaTekst, m)
		}
	}
	osnovno := func(oznaka, vrijednost string, stil int) {
		r := l.Redak()
		red := make([]xlsxw.Celija, stupaca)
		red[0] = B(oznaka, xlsxw.TablicaPod)
		red[2] = B(vrijednost, stil)
		for c := 3; c < stupaca; c++ {
			red[c] = B("", stil)
		}
		red[1] = B("", xlsxw.TablicaPod)
		l.Dodaj(red...)
		l.Spoji(0, r, 1, r)
		l.Spoji(2, r, stupaca-1, r)
	}
	osnovno("Sudionik", strings.ToUpper(iors.UserName), xlsxw.TablicaPod)
	osnovno("Obrana", obrana+" — "+z.Centar, xlsxw.Tablica)
	osnovno("Razdoblje", razdoblje, xlsxw.Tablica)
	osnovno("Radio za", strings.Join(za, "; "), xlsxw.Tablica)
	osnovno("Mjesto rada", strings.Join(mjestaTekst, ", "), xlsxw.Tablica)
	osnovno("Stvarni sati", satiTekst(iors.Stvarni)+" h", xlsxw.Tablica)
	osnovno("Obračunski sati", strings.ReplaceAll(fmt.Sprintf("%.1f", iors.Obracunski), ".", ","), xlsxw.TablicaPod)
	if iors.CekaPotvrdu > 0 {
		osnovno("Čeka potvrdu", satiTekst(iors.CekaPotvrdu)+" h — nije u obračunu", xlsxw.Tablica)
	}
	l.Dodaj()
	l.Visina(l.Redak()-1, 8)

	// radni sati po danima
	r := l.Redak()
	red := make([]xlsxw.Celija, stupaca)
	red[0] = B("Radni sati po danima", xlsxw.Podnaslov)
	l.Dodaj(red...)
	l.Spoji(0, r, stupaca-1, r)
	r1 := l.Redak()
	red1 := []xlsxw.Celija{B("Dan", xlsxw.Zaglavlje), B("Datum", xlsxw.Zaglavlje), B("Od", xlsxw.Zaglavlje), B("Do", xlsxw.Zaglavlje), B("Opis rada · za koga", xlsxw.Zaglavlje), B("Ukupno", xlsxw.Zaglavlje), B("Mjesto", xlsxw.Zaglavlje),
		B("RADNI DAN", xlsxw.Zaglavlje), B("", xlsxw.Zaglavlje), B("", xlsxw.Zaglavlje), B("SUBOTA", xlsxw.Zaglavlje), B("", xlsxw.Zaglavlje), B("NEDJELJA I BLAGDAN", xlsxw.Zaglavlje), B("", xlsxw.Zaglavlje)}
	red2 := []xlsxw.Celija{B("", xlsxw.Zaglavlje), B("", xlsxw.Zaglavlje), B("", xlsxw.Zaglavlje), B("", xlsxw.Zaglavlje), B("", xlsxw.Zaglavlje), B("sati", xlsxw.Zaglavlje), B("", xlsxw.Zaglavlje)}
	for _, razred := range obracun.Razredi {
		red2 = append(red2, B(razred.Pojas(), xlsxw.Zaglavlje))
	}
	l.Dodaj(red1...)
	l.Dodaj(red2...)
	for c := 0; c < 7; c++ {
		if c != 5 {
			l.Spoji(c, r1, c, r1+1)
		}
	}
	l.Spoji(7, r1, 9, r1)
	l.Spoji(10, r1, 11, r1)
	l.Spoji(12, r1, 13, r1)
	l.Visina(r1, 22)
	l.Visina(r1+1, 30)
	l.PonoviRetke(r1, r1+1)
	prvi := l.Redak()
	for _, r := range iors.Redovi {
		mjesto := "ured"
		if r.Mjesto == models.MjestoTeren {
			mjesto = "teren"
		}
		opis := r.Opis + " · " + r.Za
		if !r.Potvrdeno {
			opis += " (čeka potvrdu — nije u obračunu)"
		}
		doTekst := r.Do.Format("15:04")
		if doTekst == "00:00" {
			doTekst = "24:00"
		}
		red := []xlsxw.Celija{B(strings.ToLower(danTjednaHR(r.Dan)), xlsxw.Tablica), B(r.Dan.Format("02.01.2006."), xlsxw.Tablica), B(r.Od.Format("15:04"), xlsxw.TablicaSredina), B(doTekst, xlsxw.TablicaSredina), B(opis, xlsxw.Tablica),
			xlsxw.N(r.Ukupno.Hours(), xlsxw.TablicaBrojPod), B(mjesto, xlsxw.TablicaSredina)}
		for _, razred := range obracun.Razredi {
			if v := r.Sati[razred]; v > 0 && r.Potvrdeno {
				red = append(red, xlsxw.N(v.Hours(), xlsxw.TablicaBroj))
			} else {
				red = append(red, B("", xlsxw.Tablica))
			}
		}
		l.Dodaj(red...)
	}
	zadnji := l.Redak() - 1
	if zadnji >= prvi {
		sve := []xlsxw.Celija{B("SVEUKUPNO", xlsxw.TablicaPod), B("", xlsxw.TablicaPod), B("", xlsxw.TablicaPod), B("", xlsxw.TablicaPod), B("", xlsxw.TablicaPod),
			xlsxw.F(fmt.Sprintf("SUM(%s:%s)", xlsxw.Adresa(5, prvi), xlsxw.Adresa(5, zadnji)), iors.Stvarni.Hours()+iors.CekaPotvrdu.Hours(), xlsxw.TablicaBrojPod), B("", xlsxw.TablicaPod)}
		for i, razred := range obracun.Razredi {
			c := 7 + i
			sve = append(sve, xlsxw.F(fmt.Sprintf("SUM(%s:%s)", xlsxw.Adresa(c, prvi), xlsxw.Adresa(c, zadnji)), (iors.Ured[razred]+iors.Teren[razred]).Hours(), xlsxw.TablicaBrojPod))
		}
		l.Dodaj(sve...)
		l.Spoji(0, l.Redak()-1, 4, l.Redak()-1)
	}
	l.Dodaj()
	l.Visina(l.Redak()-1, 8)

	// obračun po razredu: samo razredi sa satima, ured pa teren
	r = l.Redak()
	red = make([]xlsxw.Celija, stupaca)
	red[0] = B("Obračun sati", xlsxw.Podnaslov)
	l.Dodaj(red...)
	l.Spoji(0, r, stupaca-1, r)
	rz := l.Redak()
	l.Dodaj(B("Mjesto", xlsxw.Zaglavlje), B("", xlsxw.Zaglavlje), B("Sati", xlsxw.Zaglavlje), B("", xlsxw.Zaglavlje), B("", xlsxw.Zaglavlje), B("stvarni", xlsxw.Zaglavlje), B("k", xlsxw.Zaglavlje), B("obračunski", xlsxw.Zaglavlje))
	l.Spoji(0, rz, 1, rz)
	l.Spoji(2, rz, 4, rz)
	l.Visina(rz, 22)
	var ukStvarni, ukObr float64
	for _, mjesto := range []obracun.Mjesto{obracun.Ured, obracun.Teren} {
		naziv := "ured"
		if mjesto == obracun.Teren {
			naziv = "teren"
		}
		for _, razred := range obracun.Razredi {
			sati := iors.Sat(mjesto, razred).Hours()
			if sati == 0 {
				continue
			}
			obr := iors.Obr(mjesto, razred)
			rr := l.Redak()
			l.Dodaj(B(naziv, xlsxw.Tablica), B("", xlsxw.Tablica), B(razred.Dan()+" — "+razred.Pojas(), xlsxw.Tablica), B("", xlsxw.Tablica), B("", xlsxw.Tablica),
				xlsxw.N(sati, xlsxw.TablicaBroj), xlsxw.N(koef[mjesto][razred], xlsxw.TablicaBroj), xlsxw.N(obr, xlsxw.TablicaBroj))
			l.Spoji(0, rr, 1, rr)
			l.Spoji(2, rr, 4, rr)
			ukStvarni += sati
			ukObr += obr
		}
	}
	rr := l.Redak()
	l.Dodaj(B("UKUPNO OBRAČUN", xlsxw.TablicaPod), B("", xlsxw.TablicaPod), B("", xlsxw.TablicaPod), B("", xlsxw.TablicaPod), B("", xlsxw.TablicaPod), xlsxw.N(ukStvarni, xlsxw.TablicaBrojPod), B("", xlsxw.TablicaPod), xlsxw.N(ukObr, xlsxw.TablicaBrojPod))
	l.Spoji(0, rr, 4, rr)
	l.Dodaj()
	napomenaLista(l, "Iz plana dežurstava goCOP-a. Sati po zidnom satu; razmak preko ponoći podijeljen po danima; nedjelja se obračunava kao blagdan. "+
		"Obračunski sati zaokruženi su po razredu na pola sata, sredina djelatniku; zbroj je iz zaokruženih. Koeficijenti iz postavki obračuna.", stupaca, 30)
	potpisi := []PotpisnikIzvoza{{Funkcija: "sudionik provedbe obrane od poplava", Ime: iors.UserName}}
	if len(z.Potpisnici) > 0 {
		potpisi = append(potpisi, z.Potpisnici[0])
	}
	potpisiLista(l, z, stupaca, potpisi)
	return k
}

func satiTekst(d time.Duration) string {
	return fmt.Sprintf("%d:%02d", int(d.Hours()), int(d.Minutes())%60)
}

// IzvoziDnevnik piše dnevnik COP-a kao dokument za ispis: zapisi po danima,
// s vremenom, vrstom, područjem, tko je javio i tko upisao. Filtar po
// području isti je kao na stranici.
func (h *JournalsHandler) IzvoziDnevnik(w http.ResponseWriter, r *http.Request) {
	j, area, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	if j.CentarSektor == "" {
		http.Error(w, "izvoz dnevnika je za dnevnik COP-a", http.StatusNotFound)
		return
	}
	data := h.pageData(r)
	data.Journal, data.Area = j, area
	h.fillRights(&data)
	zapisi, err := h.journals.EntriesForJournal(r.Context(), j.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	nazivi := h.naziviPodrucja(j.CentarSektor)
	podrucje, _ := strconv.Atoi(r.URL.Query().Get("podrucje"))
	if podrucje > 0 {
		var samo []models.JournalEntry
		for _, z := range zapisi {
			if !z.ZaPodrucje() || z.PodrucjeID() == podrucje {
				samo = append(samo, z)
			}
		}
		zapisi = samo
	}
	z := h.ZaglavljeIzvozaDnevnika(j)
	dezurstva, _ := h.journals.Dezurstva(r.Context(), j.ID)
	knjiga := KnjigaDnevnika(j, zapisi, dezurstva, z, nazivi, nazivi[podrucje])
	ime := "Dnevnik_COP_" + service.OznakaIzNaziva(z.Centar) + "_" + strconv.Itoa(j.Year)
	if j.StartedAt != nil {
		ime += "_" + j.StartedAt.Format("2006-01-02")
	}
	if podrucje > 0 {
		ime += "_" + service.OznakaIzNaziva(nazivi[podrucje])
	}
	posaljiXLSX(w, ime+".xlsx", knjiga)
}

// KnjigaDnevnika slaže dnevnik COP-a kao jedan list: zaglavlje, osnovno o
// dnevniku, zapisi po danima — dan kao naslovni redak, ispod redak po zapisu
// (storniran ostaje, označen, s razlogom) — pa tko ga je vodio i potpisi.
//
// Tko potpisuje: oni koji su javljali ne potpisuju nikad — izvor su, ne
// autor, i njihovo ime i sat stoje uz zapis. Oni koji su vodili već su
// potpisali svaki svoj zapis: tko i kad, nepromjenjivo; na kraju stoje
// popisom, s brojem zapisa i dežurstvima. Dokument potpisuje voditelj
// centra, da je zaključen i cjelovit, a rukovoditelj sektora prima na
// znanje. Pravi potpis dolazi pečaćenjem u .cop; dotad ispis nosi datum.
func KnjigaDnevnika(j *models.Journal, zapisi []models.JournalEntry, dezurstva []models.Dezurstvo, z ZaglavljeIzvoza, nazivi map[int]string, samoPodrucje string) *xlsxw.Knjiga {
	k := &xlsxw.Knjiga{LogoPNG: z.LogoPNG}
	B := xlsxw.T
	const stupaca = 8 // A..H
	l := k.NoviList("Dnevnik")
	l.Vodoravno = true
	l.Sirine = []float64{6, 8, 12, 18, 18, 72, 18, 4}
	podnaslov := ""
	if j.StartedAt != nil {
		podnaslov = j.StartedAt.Format("02.01.2006.")
		if j.EndedAt != nil {
			podnaslov += " – " + j.EndedAt.Format("02.01.2006.")
		} else {
			podnaslov += " – (otvoren)"
		}
	}
	if samoPodrucje != "" {
		podnaslov += " · zapisi za " + samoPodrucje + " i cijeli " + models.Terms().Lower("sektor")
	}
	zaglavljeLista(l, z, j.DisplayTitle(), podnaslov, stupaca)

	osnovno := func(oznaka, vrijednost string) {
		r := l.Redak()
		red := make([]xlsxw.Celija, stupaca)
		red[0] = B(oznaka, xlsxw.TablicaPod)
		red[1] = B("", xlsxw.TablicaPod)
		red[2] = B(vrijednost, xlsxw.Tablica)
		for c := 3; c < stupaca; c++ {
			red[c] = B("", xlsxw.Tablica)
		}
		l.Dodaj(red...)
		l.Spoji(0, r, 1, r)
		l.Spoji(2, r, stupaca-1, r)
	}
	osnovno("Centar", z.Centar)
	osnovno("Razdoblje", podnaslov)
	dana := map[string]bool{}
	storniranih := 0
	for _, e := range zapisi {
		dana[e.Date.Format("2006-01-02")] = true
		if e.Voided {
			storniranih++
		}
	}
	osnovno("Zapisa", fmt.Sprintf("%d, u %d dana; storniranih %d", len(zapisi), len(dana), storniranih))
	if j.Reconstruction {
		osnovno("Napomena", "Prijepis iz uveza. "+j.Notes)
	} else if j.Notes != "" {
		osnovno("Napomena", j.Notes)
	}
	l.Dodaj()
	l.Visina(l.Redak()-1, 8)

	r1 := l.Redak()
	l.Dodaj(B("Br.", xlsxw.Zaglavlje), B("Vrijeme", xlsxw.Zaglavlje), B("Vrsta", xlsxw.Zaglavlje), B("Za", xlsxw.Zaglavlje), B("Javio", xlsxw.Zaglavlje), B("Zapis", xlsxw.Zaglavlje), B("Upisao", xlsxw.Zaglavlje), B("", xlsxw.Zaglavlje))
	l.Visina(r1, 22)
	l.PonoviRetke(r1, r1)
	var dan string
	for _, e := range zapisi {
		if d := e.Date.Format("2006-01-02"); d != dan {
			dan = d
			r := l.Redak()
			red := make([]xlsxw.Celija, stupaca)
			red[0] = B(danTjednaHR(e.Date)+" "+e.Date.Format("2.1.2006."), xlsxw.TablicaPod)
			for c := 1; c < stupaca; c++ {
				red[c] = B("", xlsxw.TablicaPod)
			}
			l.Dodaj(red...)
			l.Spoji(0, r, stupaca-1, r)
		}
		vrijeme := ""
		if e.HappenedAt != nil {
			vrijeme = e.HappenedAt.Format("15:04")
		}
		za := "cijeli " + models.Terms().Lower("sektor")
		if e.ZaPodrucje() {
			if za = nazivi[e.PodrucjeID()]; za == "" {
				za = fmt.Sprintf("%s %d", models.Terms().Lower("podrucje"), e.PodrucjeID())
			}
		}
		tekst := e.Text
		oznaka := ""
		if e.Voided {
			oznaka = "STORNO"
			tekst += "\n[STORNIRAN"
			if e.VoidReason != "" {
				tekst += ": " + e.VoidReason
			}
			if e.VoidedBy != "" {
				tekst += " — " + e.VoidedBy
			}
			tekst += "]"
		}
		l.Dodaj(xlsxw.N(float64(e.Number), xlsxw.TablicaSredina), B(vrijeme, xlsxw.TablicaSredina), B(e.KindLabel(), xlsxw.Tablica), B(za, xlsxw.TablicaTekst),
			B(e.ReportedBy, xlsxw.TablicaTekst), B(tekst, xlsxw.TablicaTekst), B(e.UserName, xlsxw.TablicaTekst), B(oznaka, xlsxw.TablicaSredina))
	}
	if len(zapisi) == 0 {
		l.Dodaj(B("U dnevniku nema zapisa.", xlsxw.Napomena))
	}
	l.Dodaj()
	l.Visina(l.Redak()-1, 8)

	// Dnevnik vodili: tko je upisivao, koliko, i kad je dežurao
	type vodio struct {
		ime       string
		zapisa    int
		dezurstva []string
	}
	poImenu := map[string]*vodio{}
	var redom []string
	for _, e := range zapisi {
		if e.UserName == "" {
			continue
		}
		v := poImenu[e.UserName]
		if v == nil {
			v = &vodio{ime: e.UserName}
			poImenu[e.UserName] = v
			redom = append(redom, e.UserName)
		}
		v.zapisa++
	}
	for _, d := range dezurstva {
		if d.Opis != models.OpisiRada[0].Opis {
			continue // samo dežurstvo u centru; teren i ostalo je u planu, ne u vođenju dnevnika
		}
		v := poImenu[d.UserName]
		if v == nil {
			v = &vodio{ime: d.UserName}
			poImenu[d.UserName] = v
			redom = append(redom, d.UserName)
		}
		kraj := d.Do.Format("15:04")
		if d.Do.Format("2006-01-02") != d.Od.Format("2006-01-02") {
			kraj = d.Do.Format("2.1. 15:04")
		}
		v.dezurstva = append(v.dezurstva, d.Od.Format("2.1. 15:04")+"–"+kraj)
	}
	sort.Strings(redom)
	if len(redom) > 0 {
		r := l.Redak()
		red := make([]xlsxw.Celija, stupaca)
		red[0] = B("Dnevnik vodili", xlsxw.Podnaslov)
		l.Dodaj(red...)
		l.Spoji(0, r, stupaca-1, r)
		rz := l.Redak()
		l.Dodaj(B("Ime i prezime", xlsxw.Zaglavlje), B("", xlsxw.Zaglavlje), B("", xlsxw.Zaglavlje), B("Zapisa", xlsxw.Zaglavlje), B("Dežurstva u centru", xlsxw.Zaglavlje), B("", xlsxw.Zaglavlje), B("", xlsxw.Zaglavlje), B("", xlsxw.Zaglavlje))
		l.Spoji(0, rz, 2, rz)
		l.Spoji(4, rz, stupaca-1, rz)
		for _, ime := range redom {
			v := poImenu[ime]
			rr := l.Redak()
			l.Dodaj(B(v.ime, xlsxw.Tablica), B("", xlsxw.Tablica), B("", xlsxw.Tablica), xlsxw.N(float64(v.zapisa), xlsxw.TablicaSredina),
				B(strings.Join(v.dezurstva, "; "), xlsxw.TablicaTekst), B("", xlsxw.TablicaTekst), B("", xlsxw.TablicaTekst), B("", xlsxw.TablicaTekst))
			l.Spoji(0, rr, 2, rr)
			l.Spoji(4, rr, stupaca-1, rr)
		}
		l.Dodaj()
	}
	napomenaLista(l, "Svaki zapis nosi tko ga je upisao i kad, i ne mijenja se: to je potpis onoga tko je vodio. Tko je javio ne potpisuje — izvor je, "+
		"a njegovo ime stoji uz zapis. Vrijeme je kad se dogodilo; kad je upisano vidi se u programu. Zapis se ne briše nego stornira uz razlog. "+
		"Izvezeno iz goCOP-a "+z.Datum.Format("02.01.2006. 15:04")+".", stupaca, 36)
	potpisi := make([]PotpisnikIzvoza, 0, 2)
	for i, p := range z.Potpisnici {
		uloga := "primio na znanje"
		if i == 0 {
			uloga = "zaključio dnevnik"
		}
		potpisi = append(potpisi, PotpisnikIzvoza{Funkcija: p.Funkcija + " — " + uloga, Ime: p.Ime})
	}
	potpisiLista(l, z, stupaca, potpisi)
	return k
}
