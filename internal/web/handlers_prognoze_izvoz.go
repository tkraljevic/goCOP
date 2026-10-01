package web

import (
	"fmt"
	"gocop/internal/hydro"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"
	"gocop/internal/xlsxw"
)

// Izvoz prognoza u Excel, u obliku uredske pomoćne tablice na koju su svi
// navikli: list po vodi, letve u stupcima od uzvodne prema nizvodnoj, a u
// recima termini — stanje sada, +6 i +12 sati, pa za svaki dan u 07 h naša
// vrijednost s rasponom, protokom i modelom, te mađarska i srpska prognoza
// ispod. Vrijednosti su brojevi, da se s njima može računati.

// IzvoziPrognoze šalje zadnje izdanje prognoze kao .xlsx.
func (h *PrognozeHandler) IzvoziPrognoze(w http.ResponseWriter, r *http.Request) {
	// Sažetak piše o najnižem i najvišem zabilježenom vodostaju, pa krajnosti
	// iz arhive moraju biti izračunate prije nego se podaci slože; stranica ih
	// računa u pozadini, a izvoz smije pričekati koju sekundu.
	var zaSazetak []string
	for _, g := range sazetakSkupine {
		zaSazetak = append(zaSazetak, g.Letve...)
	}
	h.godisnje(r.Context(), zaSazetak)
	data := h.podaci(r, h.nacini(r.Context()).PrognozaExcel, h.nacini(r.Context()).PrognozaGraf)
	if data.Nema {
		http.Error(w, data.Razlog, http.StatusNotFound)
		return
	}
	z := h.zaglavljeIzvoza(data.CurrentUser)
	k := &xlsxw.Knjiga{LogoPNG: z.LogoPNG}
	h.listSazetka(r.Context(), k, z, data)
	for _, t := range data.Tablice {
		listPrognoze(k, z, data, t)
	}
	grafovi := h.nizoviGrafova(r.Context(), data)
	listGrafova(k, z, data, grafovi)
	kodovi := make([]string, len(grafovi))
	for i, g := range grafovi {
		kodovi[i] = g.kod
	}
	listoviGodisnjih(k, z, data, grafovi, h.godisnje(r.Context(), kodovi), godisnjiVodostajiVrsta)
	listoviGodisnjih(k, z, data, grafovi, h.godisnjeVelicine(r.Context(), "protok", kodovi), godisnjiProtociVrsta)
	listMetode(k, z, h.metoda(r))
	// Skriveni list s podacima grafova ide na kraj knjige.
	sort.SliceStable(k.Listovi, func(i, j int) bool { return !k.Listovi[i].Skriven && k.Listovi[j].Skriven })
	posaljiXLSX(w, "prognoza_"+time.Now().In(models.Zagreb).Format("2006-01-02_15h")+".xlsx", k)
}

// listPrognoze piše jednu vodu.
func listPrognoze(k *xlsxw.Knjiga, z ZaglavljeIzvoza, data PrognozePageData, t TablicaPrognoza) {
	T, N := xlsxw.T, xlsxw.N
	// U izvoz ulaze letve s našom prognozom i srpske letve, sa svojim
	// mjerenjem i prognozom srpske službe, jer su na istom Dunavu nasuprot
	// našima. Ostale letve samo s mjerenjem (Bratislava, Komárno) ili samo s
	// tuđom prognozom (Komárom, Letenye) ostaju na stranici.
	var letve []LetvaPrognoze
	for _, x := range t.Letve {
		if _, drzava := imeIDrzava(x.Naziv); x.Racuna != "" || drzava == "RS" {
			letve = append(letve, x)
		}
	}
	if len(letve) == 0 {
		return
	}
	stupaca := 2 + len(letve)
	l := k.NoviList(t.Naslov)
	l.Vodoravno = true
	l.Sirine = []float64{14, 8}
	for _, x := range letve {
		l.Sirine = append(l.Sirine, sirinaLetve(x, t.Naslov))
	}
	zaglavljeLista(l, z, "PROGNOZA VODOSTAJA — "+t.Naslov,
		"izdano "+data.Izdano+" · dani za 07 h · vodostaj u cm, protok u m³/s · dani: "+data.NacinTablice, stupaca)

	// Zaglavlje u tri retka: naziv letve (dulji se prelomi u dva retka),
	// država kraticom — ili voda, gdje nije ista kao naslov lista — i
	// kilometar. Država u svom retku drži stupce uskima.
	r := l.Redak()
	red := []xlsxw.Celija{T("Termin", xlsxw.Zaglavlje), T("", xlsxw.Zaglavlje)}
	for _, x := range letve {
		ime, _ := imeIDrzava(x.Naziv)
		red = append(red, T(ime, xlsxw.Zaglavlje))
	}
	l.Dodaj(red...)
	l.Visina(r, 30)
	red = []xlsxw.Celija{T("", xlsxw.Tablica), T("država", xlsxw.TablicaSredina)}
	for _, x := range letve {
		red = append(red, T(drzavaLetve(x), xlsxw.TablicaSredina))
	}
	l.Dodaj(red...)
	// Voda u svom retku samo gdje nije ista kao naslov lista: dunavci
	// inundacije i pritoke; na Dunavu i Dravi s Murom redak ne treba.
	if imaVode(letve, t.Naslov) {
		red = []xlsxw.Celija{T("", xlsxw.Tablica), T("voda", xlsxw.TablicaSredina)}
		for _, x := range letve {
			red = append(red, T(vodaLetve(x, t.Naslov), xlsxw.TablicaSredina))
		}
		l.Dodaj(red...)
	}
	red = []xlsxw.Celija{T("", xlsxw.Tablica), T("km", xlsxw.TablicaSredina)}
	for _, x := range letve {
		red = append(red, T(kmIzvoza(x), xlsxw.TablicaSredina))
	}
	l.Dodaj(red...)
	l.PonoviRetke(r, l.Redak()-1)

	broj := func(v *float64, stil int) xlsxw.Celija {
		if v == nil {
			return T("", stil)
		}
		return N(math.Round(*v), stil) // cijeli centimetri i m³/s, kao u uredskoj tablici
	}
	// Razdoblja se odvajaju debelom crtom na početku i podlogom svakog
	// drugog; redak naše vrijednosti ima svoju ispunu u oba.
	razdoblje := -1
	redak := func(termin, sto string, stil int, celija func(LetvaPrognoze) xlsxw.Celija) {
		if termin != "" {
			razdoblje++
		}
		pojas := func(st int) int {
			if razdoblje%2 == 1 {
				return xlsxw.UPojasu(st)
			}
			return st
		}
		// Termin počinje debelom crtom: prvi redak svakog razdoblja nosi
		// njegov naziv, pa se po njemu razdoblja i odvajaju.
		prvi := termin != ""
		stilTermina := xlsxw.TablicaPod
		if prvi {
			stil, stilTermina = xlsxw.TablicaPodRub, xlsxw.TablicaPodRub
		}
		red := []xlsxw.Celija{T(termin, pojas(stilTermina)), T(sto, pojas(stil))}
		for _, x := range letve {
			c := celija(x)
			if prvi {
				c.Stil = xlsxw.TablicaPodRub
			}
			c.Stil = pojas(c.Stil)
			red = append(red, c)
		}
		l.Dodaj(red...)
	}
	ima := func(f func(LetvaPrognoze) bool) bool {
		for _, x := range letve {
			if f(x) {
				return true
			}
		}
		return false
	}

	redak("Sada", "cm", xlsxw.TablicaPod, func(x LetvaPrognoze) xlsxw.Celija { return broj(x.SadaCmV, xlsxw.TablicaPod) })
	if ima(func(x LetvaPrognoze) bool { return x.SadaQV != nil }) {
		redak("", "m³/s", xlsxw.TablicaKurziv, func(x LetvaPrognoze) xlsxw.Celija { return broj(x.SadaQV, xlsxw.TablicaKurziv) })
	}
	for i, d := range data.Bliski {
		termin := "+" + tekstBroja(d) + " h"
		vr := func(x LetvaPrognoze) VrijednostPrognoze {
			if i < len(x.Vrijednosti) {
				return x.Vrijednosti[i]
			}
			return VrijednostPrognoze{}
		}
		redak(termin, "cm", xlsxw.TablicaPod, func(x LetvaPrognoze) xlsxw.Celija { return broj(vr(x).CmV, xlsxw.TablicaPod) })
		redak("", "raspon", xlsxw.TablicaSivo, func(x LetvaPrognoze) xlsxw.Celija { return T(vr(x).CmRaspon, xlsxw.TablicaSivo) })
		if ima(func(x LetvaPrognoze) bool { return vr(x).QV != nil }) {
			redak("", "m³/s", xlsxw.TablicaKurziv, func(x LetvaPrognoze) xlsxw.Celija { return broj(vr(x).QV, xlsxw.TablicaKurziv) })
		}
	}
	for i, naslov := range data.Dani {
		dan := func(x LetvaPrognoze) CelijaDana {
			if i < len(x.Dani) {
				return x.Dani[i]
			}
			return CelijaDana{}
		}
		redak(naslov+" 07 h", "cm", xlsxw.TablicaPod, func(x LetvaPrognoze) xlsxw.Celija { return broj(dan(x).CmV, xlsxw.TablicaPod) })
		redak("", "raspon", xlsxw.TablicaSivo, func(x LetvaPrognoze) xlsxw.Celija { return T(dan(x).Raspon, xlsxw.TablicaSivo) })
		if ima(func(x LetvaPrognoze) bool { return dan(x).QV != nil }) {
			redak("", "m³/s", xlsxw.TablicaKurziv, func(x LetvaPrognoze) xlsxw.Celija { return broj(dan(x).QV, xlsxw.TablicaKurziv) })
		}
		redak("", "model", xlsxw.Tablica, func(x LetvaPrognoze) xlsxw.Celija {
			d := dan(x)
			switch {
			case d.CmV == nil:
				return T("", xlsxw.TablicaSredina)
			case d.Dnevna:
				return T("dnevni", xlsxw.TablicaSredina)
			}
			return T("satni", xlsxw.TablicaSredina)
		})
		l.SakrijRedak(l.Redak() - 1) // tko želi vidjeti model, otkrije ga
		for _, iz := range tudiIzvori {
			tuda := func(x LetvaPrognoze) *TudaCelija {
				for _, tc := range dan(x).Tude {
					if tc.Oznaka == iz.oznaka {
						return &tc
					}
				}
				return nil
			}
			if !ima(func(x LetvaPrognoze) bool { return tuda(x) != nil }) {
				continue
			}
			redak("", iz.oznaka, xlsxw.Tablica, func(x LetvaPrognoze) xlsxw.Celija {
				if tc := tuda(x); tc != nil {
					v := tc.CmV
					return N(v, xlsxw.Tablica)
				}
				return T("", xlsxw.Tablica)
			})
		}
	}
	l.Dodaj()
	izdaje := "centra obrane od poplava"
	if z.Centar != "" {
		izdaje = z.Centar
	}
	napomena := "Prognoza " + izdaje + ". " +
		"Metoda: prvih dana (skriveni redak „model”: satni) hidrološki lanac vodomjernih postaja — vodostaj, odnosno protok " +
		"nizvodne postaje izvodi se iz uzvodnih, uz izmjereno vrijeme propagacije vala i po dijelovima linearnu vezu " +
		"ovisnu o vodnosti, ispravljeno prema zadnjem mjerenju; dalje (dnevni) statistički model na dnevnim vodostajima " +
		"od 1901. — višestruka regresija i metoda analognih situacija. Vodostaj i protok međusobno su preračunati " +
		"krivuljom protoka postaje. Na vrhu lanca Mura (Letenye) i Dunav (Komárom) slijede prognozu mađarske službe.\n" +
		fmt.Sprintf("Raspon obuhvaća %d %% pogrešaka prognoze, izmjerenih puštanjem prognoze unatrag kroz arhivu (satni "+
			"lanac), odnosno %s standardna odstupanja analognih situacija (dnevni model): stvarna vrijednost ostaje "+
			"u rasponu u %d %% slučajeva, a u %d %% izlazi iz njega, podjednako iznad i ispod. Zapisan je kao ± kad je "+
			"simetričan, a granicama kad ga preračun krivuljom protoka učini nesimetričnim.\n",
			data.Udio, brojHRf(prognoza.DnevniRasponMnozitelj, 2), data.Udio, 100-data.Udio) +
		"HU — prognoza mađarske hidrološke službe (hydroinfo.hu), uz našu radi usporedbe; njihov raspon " +
		"obuhvaća isti udio (70 %), pa dva raspona znače isto. Potpun opis modela i računa, " +
		"s ulazima svake postaje, na listu „O prognozi”."
	var sirina float64
	for _, w := range l.Sirine {
		sirina += w
	}
	// Napomena je sitnijim slovima, pa u redak stane više znakova nego što
	// kaže širina stupaca.
	napomenaLista(l, napomena, stupaca, visinaTeksta(napomena, int(sirina*1.3), 30, 0))
	potpisiLista(l, z, max(stupaca, 8), z.Potpisnici) // dva potpisa trebaju mjesta i kad je letvi malo
}

// zaglavljeIzvoza skuplja vrh i dno dokumenta: logotip i naziv organizacije,
// odjel i centar koji prognozu izdaje, te voditelja centra i zamjenika koji
// je potpisuju. Sektor je onaj prijavljenog korisnika, a bez njega B — COP
// Osijek, koji prognozu i izdaje.
func (h *PrognozeHandler) zaglavljeIzvoza(u *models.User) ZaglavljeIzvoza {
	t := models.Terms()
	sektor := sektorIzdavaca(u)
	z := ZaglavljeIzvoza{Organizacija: t.OrgName, Sektor: sektor, Datum: time.Now().In(models.Zagreb)}
	if t.HasLogo() && t.LogoMime == "image/png" {
		z.LogoPNG = t.Logo
	}
	if h.users == nil {
		return z
	}
	if sektori, err := h.users.ListSectors(); err == nil {
		for _, sk := range sektori {
			if sk.ID == sektor {
				z.Odjel, z.Centar = sk.VgoName, sk.CenterCop
			}
		}
	}
	z.Mjesto = strings.TrimSpace(strings.TrimPrefix(z.Centar, t.CenterShort))
	z.Potpisnici = []PotpisnikIzvoza{h.izdavac(u, sektor)}
	return z
}

// sektorIzdavaca je sektor primarne dužnosti korisnika, a bez nje B.
func sektorIzdavaca(u *models.User) string {
	if u != nil {
		if d := u.PrimaryDuty(); d != nil && d.SectorID != nil && *d.SectorID != "" {
			return *d.SectorID
		}
	}
	return "B"
}

// centar je naziv centra obrane koji prognozu izdaje, iz sektora korisnika.
func (h *PrognozeHandler) centar(u *models.User) string {
	if h.users == nil {
		return ""
	}
	sektor := sektorIzdavaca(u)
	if sektori, err := h.users.ListSectors(); err == nil {
		for _, sk := range sektori {
			if sk.ID == sektor {
				return sk.CenterCop
			}
		}
	}
	return ""
}

// funkcijeIzdavaca su dužnosti koje prognozu izdaju, redom prednosti.
var funkcijeIzdavaca = []struct {
	uloga    models.Role
	funkcija string
}{
	{models.RoleCopLeader, "voditelj Centra obrane od poplava"},
	{models.RoleCopDeputy, "zamjenik voditelja Centra obrane od poplava"},
}

// izdavac je onaj tko tablicu potpisuje: prognozu izdaje ili voditelj centra
// obrane ili njegov zamjenik, ne obojica. Kad izvoz radi jedan od njih,
// potpisuje on; inače potpis stoji na voditelju centra.
func (h *PrognozeHandler) izdavac(u *models.User, sektor string) PotpisnikIzvoza {
	imeOsobe := func(o models.User) string {
		if o.Title != "" {
			return o.FullName + ", " + o.Title
		}
		return o.FullName
	}
	if u != nil {
		for _, f := range funkcijeIzdavaca {
			for _, d := range u.Duties {
				if d.Role == f.uloga && (d.SectorID == nil || *d.SectorID == sektor) {
					return PotpisnikIzvoza{Funkcija: f.funkcija, Ime: imeOsobe(*u)}
				}
			}
		}
	}
	p := PotpisnikIzvoza{Funkcija: funkcijeIzdavaca[0].funkcija}
	if h.users == nil {
		return p
	}
	if osobe, err := h.users.ListUsers(sektor, 0, string(models.RoleCopLeader), "", ""); err == nil && len(osobe) > 0 {
		p.Ime = imeOsobe(osobe[0])
	}
	return p
}

func tekstBroja(n int) string { return brojHRf(float64(n), 0) }

// drzave su kratice za zagradu u nazivu strane letve.
var drzave = map[string]string{
	"Mađarska": "HU", "Srbija": "RS", "Slovačka": "SK", "Slovenija": "SI", "Austrija": "AT",
	"Bosna i Hercegovina": "BA",
}

// imeIDrzava rastavlja „Komárom (Mađarska)" na ime i kraticu države; naša
// letva nema zagradu i vraća samo ime.
func imeIDrzava(naziv string) (string, string) {
	i := strings.LastIndex(naziv, " (")
	if i < 0 || !strings.HasSuffix(naziv, ")") {
		return naziv, ""
	}
	// Zagrada koja nije država ostaje dio imena: „Ustava Zmajevac (nizvodno)".
	k, ima := drzave[naziv[i+2:len(naziv)-1]]
	if !ima {
		return naziv, ""
	}
	return naziv[:i], k
}

// drzavaLetve je država kraticom; naša letva je RH.
func drzavaLetve(x LetvaPrognoze) string {
	if _, drzava := imeIDrzava(x.Naziv); drzava != "" {
		return drzava
	}
	return "RH"
}

// vodaLetve je voda letve kad nije ista kao naslov lista (Bednja na listu
// pritoka, Zmajevački Dunavac u inundaciji).
func vodaLetve(x LetvaPrognoze, naslovLista string) string {
	if x.Voda != "" && !strings.Contains(naslovLista, x.Voda) {
		return x.Voda
	}
	return ""
}

// imaVode kaže treba li listu redak vode.
func imaVode(letve []LetvaPrognoze, naslovLista string) bool {
	for _, x := range letve {
		if vodaLetve(x, naslovLista) != "" {
			return true
		}
	}
	return false
}

// kmIzvoza je kilometar letve u jednom obliku za sve: „1424+850" i
// „rkm 1.380,30" oba postaju „1.424,9" i „1.380,3". Stacionaža koja nije
// riječni kilometar (nkm na dunavcu) ostaje kako je upisana.
func kmIzvoza(x LetvaPrognoze) string {
	s := strings.TrimSpace(x.Stacionaza)
	if strings.HasPrefix(s, "rkm") {
		if km, ok := hydro.ParseStationingKm(s); ok {
			return brojHRf(km, 1)
		}
	}
	return strings.TrimSpace(strings.TrimPrefix(s, "rkm"))
}

// sirinaLetve je širina stupca letve u znakovima: dovoljna za najdulju riječ
// imena (dulje ime prelomi se u dva retka), za državu, kilometar i sve što
// u stupcu piše — raspon „-379 do -339" zna biti najdulji.
func sirinaLetve(x LetvaPrognoze, naslovLista string) float64 {
	duljina := func(s string) float64 { return float64(len([]rune(s))) }
	ime, _ := imeIDrzava(x.Naziv)
	w := 5.0 // „1.234"
	for _, rijec := range strings.Fields(ime) {
		w = max(w, duljina(rijec)*1.15) // zaglavlje je podebljano
	}
	w = max(w, duljina(vodaLetve(x, naslovLista)), duljina(kmIzvoza(x)))
	for _, v := range x.Vrijednosti {
		w = max(w, duljina(v.CmRaspon))
	}
	for _, d := range x.Dani {
		w = max(w, duljina(d.Raspon))
	}
	return max(7, w+2)
}

// listMetode piše list „O prognozi”: isti opis metode kao na stranici, i
// tablicu postaja s ulazima i rasponom, da se uz izdanu tablicu uvijek zna
// odakle su brojevi i kako su izvedeni.
func listMetode(k *xlsxw.Knjiga, z ZaglavljeIzvoza, m PrognozeMetodaData) {
	T, N := xlsxw.T, xlsxw.N
	l := k.NoviList("O prognozi")
	l.Vodoravno = true
	l.Sirine = []float64{20, 46, 11, 18, 38, 11}
	stupaca := len(l.Sirine)
	var sirina float64
	for _, w := range l.Sirine {
		sirina += w
	}
	podnaslov := "metoda i račun prognoze"
	if m.Izdano != "" {
		podnaslov += " · izdanje " + m.Izdano
	}
	zaglavljeLista(l, z, "O PROGNOZI", podnaslov, stupaca)

	preko := func(c xlsxw.Celija, visina float64) {
		r := l.Redak()
		red := make([]xlsxw.Celija, stupaca)
		red[0] = c
		l.Dodaj(red...)
		l.Spoji(0, r, stupaca-1, r)
		l.Visina(r, visina)
	}
	tekst := func(t string) { preko(T(t, xlsxw.Tekst), visinaTeksta(t, int(sirina*1.1), 15, 0)) }
	// Uz izdanu tablicu ide sažetak: kako je čitati, metoda ukratko, raspon,
	// točnost, ograničenja i izvori. Potpun opis (građa lanca, dnevni model,
	// provjere, promjene) ostaje na stranici „O prognozi" u aplikaciji.
	preko(T("Kako čitati tablicu", xlsxw.Podnaslov), 22)
	tekst("Svaki list je jedna voda, letve od uzvodne prema nizvodnoj. U zaglavlju stoje država (RH, HU, RS), " +
		"voda gdje nije ista kao naslov lista i riječni kilometar.")
	tekst(fmt.Sprintf("Redak „Sada” je zadnje mjerenje; +6 h i +12 h računaju se od izdanja, a dani vrijede za 07 h. "+
		"Vodostaj je u cm na nuli letve, protok (kurzivom) u m³/s iz krivulje protoka. Raspon uz vrijednost obuhvaća "+
		"%d %% slučajeva. Skriveni redak „model” (otkriva se gumbom „+” uz rub) kaže je li dan dao satni lanac ili dnevni model. HU i RS su prognoze mađarske "+
		"i srpske službe za isti termin, radi usporedbe; srpske letve nose samo svoje mjerenje i srpsku prognozu.", int(math.Round(prognoza.UdioURasponu*100))))
	tekst("List „Grafovi”: zadnja tri dana izmjereno i šest dana prognoze s rasponom. P, R i I su pragovi pripremne, " +
		"redovne i izvanredne obrane, IS izvanredno stanje, MAX najviši izmjereni vodostaj; u naslovu grafa stoje " +
		"uvijek, a crtaju se kad su blizu vode. List „Godišnji vodostaji”: srednji, najniži i najviši vodostaj po " +
		"godinama iz arhive, s označenim rekordima.")
	odjeljci := map[string]bool{"Ukratko o metodi": true, "Raspon i vjerojatnost": true, "Ograničenja": true}
	for _, o := range m.Odjeljci {
		if !odjeljci[o.Naslov] {
			continue
		}
		preko(T(o.Naslov, xlsxw.Podnaslov), 22)
		for _, od := range o.Odlomci {
			switch {
			case od.Formula:
				preko(T(od.Tekst, xlsxw.Formula), 20)
			case od.Tablica != nil:
				if od.Tablica.Naslov != "" {
					preko(T(od.Tablica.Naslov, xlsxw.Tekst), visinaTeksta(od.Tablica.Naslov, int(sirina*1.1), 16, 0))
				}
				redak := func(polja []string, stil int) {
					red := make([]xlsxw.Celija, stupaca)
					for i := range red {
						red[i] = T("", stil)
					}
					for i, c := range polja {
						if i < stupaca-1 {
							red[i] = T(c, stil)
						} else {
							red[stupaca-1] = T(strings.TrimSpace(red[stupaca-1].Tekst+" "+c), stil)
						}
					}
					// visina prema najduljem tekstu u retku, ćelija po ćelija
					visina := 16.0
					for i, c := range red {
						visina = max(visina, visinaTeksta(c.Tekst, max(1, int(l.Sirine[i]*1.1)), 16, 0))
					}
					r := l.Redak()
					l.Dodaj(red...)
					l.Visina(r, visina)
				}
				redak(od.Tablica.Stupci, xlsxw.Zaglavlje)
				for _, r := range od.Tablica.Redci {
					redak(r, xlsxw.TablicaTekst)
				}
			case len(od.Popis) > 0:
				for _, st := range od.Popis {
					preko(T("• "+st, xlsxw.Tekst), visinaTeksta("• "+st, int(sirina*1.1), 15, 0))
				}
			default:
				preko(T(od.Tekst, xlsxw.Tekst), visinaTeksta(od.Tekst, int(sirina*1.1), 15, 0))
			}
		}
	}

	if v := m.Valovi; v != nil {
		preko(T("Provjera na poplavnim valovima", xlsxw.Podnaslov), 22)
		uvod := fmt.Sprintf("%d valova iz arhive, svaki provjeren modelom koji ga nije vidio. Pogreška najavljenog vrha "+
			"(srednja apsolutna, cm) i pristranost (negativno = prognoza preniska); postojanost = pretpostavka da se "+
			"ništa ne mijenja. U provjeri vrh lanca ne slijedi mađarsku prognozu i nema ispravka pomaka. Dunavske valove "+
			"do 2013. model precjenjuje desetak centimetara jer uči na koritu kakvo je od 2014.: Baja je 2002.–2004. "+
			"prema Paksu stajala 20–30 cm niže nego danas.", v.Valova)
		preko(T(uvod, xlsxw.Tekst), visinaTeksta(uvod, int(sirina*1.1), 15, 0))
		// Svaka skupina dobiva svoj naslov preko cijele širine (rijeka, model,
		// broj valova), a ispod nje samo doseg i pogreške: u uskom prvom
		// stupcu dugi naziv modela nije stajao.
		r := l.Redak()
		l.Dodaj(T("Doseg", xlsxw.Zaglavlje), T("Pogreška vrha (cm)", xlsxw.Zaglavlje), T("Pristranost (cm)", xlsxw.Zaglavlje),
			T("Postojanost (cm)", xlsxw.Zaglavlje), T("", xlsxw.Zaglavlje), T("", xlsxw.Zaglavlje))
		l.Spoji(3, r, stupaca-1, r)
		l.Visina(r, 30)
		for _, sk := range v.Skupine {
			rr := l.Redak()
			red := make([]xlsxw.Celija, stupaca)
			red[0] = T(fmt.Sprintf("%s · %s · %d %s", sk.Rijeka, sk.Model, sk.Valova, uzBrojHR(sk.Valova, "val", "vala", "valova")), xlsxw.SazetakNaslov)
			for i := 1; i < stupaca; i++ {
				red[i] = T("", xlsxw.SazetakNaslov)
			}
			l.Dodaj(red...)
			l.Spoji(0, rr, stupaca-1, rr)
			for _, d := range sk.Dosezi {
				rd := l.Redak()
				l.Dodaj(T(tekstBroja(d.Doseg)+" h", xlsxw.TablicaSredina), N(math.Round(d.MAE), xlsxw.TablicaSredina),
					N(math.Round(d.Pristranost), xlsxw.TablicaSredina), N(math.Round(d.MAEPostojanost), xlsxw.TablicaSredina),
					T("", xlsxw.TablicaSredina), T("", xlsxw.TablicaSredina))
				l.Spoji(3, rd, stupaca-1, rd)
			}
		}
		l.Dodaj()
	}

	preko(T("Izvori podataka", xlsxw.Podnaslov), 22)
	tekst("Mjerenja s mjernih sustava Hrvatskih voda i sa stranica hidroloških službi susjednih država: " +
		"ARSO (Slovenija), vizugy.hu i hydroinfo.hu (Mađarska), hidmet.gov.rs (Srbija), SHMÚ (Slovačka), eHYD, " +
		"viadonau i noel.gv.at (Austrija), GKD i Pegelonline (Njemačka). Povijest stranih postaja dopunjena je iz " +
		"ICPDR-ova sustava DanubeHIS (licenca CC BY-NC-SA 4.0: izvedeni podaci samo za nekomercijalnu uporabu, uz " +
		"navođenje izvora). Kiša: Open-Meteo (CC BY 4.0), povijest iz reanalize ERA5.")
	tekst("Potpun opis metode, provjere i promjena modela: goCOP, Prognoze → O prognozi.")
	if m.Suradnja != "" {
		tekst(m.Suradnja)
	}

	preko(T("Postaje", xlsxw.Podnaslov), 22)
	if len(m.Letve) == 0 {
		preko(T(m.BezLetvi, xlsxw.Tekst), 18)
		return
	}
	dosezi := make([]string, len(m.RasponDo))
	for i, d := range m.RasponDo {
		dosezi[i] = tekstBroja(d)
	}
	uvod := "Kod satnog lanca uz ulaz stoji veličina, kašnjenje (raspon po pojasima vodnosti kod glavnog " +
		"ulaza) i prozor glačanja; R je koeficijent korelacije računa s mjerenjima, a raspon polovina širine " +
		"raspona na " + strings.Join(dosezi, " / ") + " h. Postaje bez satnog lanca imaju samo dnevni model."
	preko(T(uvod, xlsxw.Tekst), visinaTeksta(uvod, int(sirina*1.1), 15, 0))
	r := l.Redak()
	l.Dodaj(T("Postaja", xlsxw.Zaglavlje), T("Satni lanac — ulazi", xlsxw.Zaglavlje), T("R", xlsxw.Zaglavlje),
		T("Raspon", xlsxw.Zaglavlje), T("Dnevni model — ulazi", xlsxw.Zaglavlje), T("Dnevni od", xlsxw.Zaglavlje))
	l.Visina(r, 20)
	crtica := func(s string) string {
		if s == "" {
			return "—"
		}
		return s
	}
	for _, x := range m.Letve {
		postaja := x.Naziv
		if x.Voda != "" {
			postaja += "\n" + x.Voda
		}
		if x.Racuna != "" {
			postaja += ", u " + map[bool]string{true: "protoku", false: "vodostaju"}[x.Racuna == "protok"]
		}
		satni := strings.Join(x.Satni, "\n")
		dnevni := strings.Join(x.Dnevni, ", ")
		celije := []string{postaja, crtica(satni), crtica(x.Slaganje), crtica(x.Raspon), crtica(dnevni), crtica(x.DnevniOd)}
		visina := 2*13.5 + 5
		for i, c := range celije {
			visina = max(visina, visinaTeksta(c, max(1, int(l.Sirine[i]*1.1)), 0, 0))
		}
		r := l.Redak()
		red := make([]xlsxw.Celija, len(celije))
		for i, c := range celije {
			red[i] = T(c, xlsxw.TablicaTekst)
		}
		l.Dodaj(red...)
		l.Visina(r, visina)
	}
}
