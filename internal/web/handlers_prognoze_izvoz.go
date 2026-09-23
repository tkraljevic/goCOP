package web

import (
	"math"
	"net/http"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/xlsxw"
)

// Izvoz prognoza u Excel, u obliku uredske pomoćne tablice na koju su svi
// navikli: list po vodi, letve u stupcima od uzvodne prema nizvodnoj, a u
// recima termini — stanje sada, +6 i +12 sati, pa za svaki dan u 07 h naša
// vrijednost s rasponom, protokom i modelom, te mađarska i srpska prognoza
// ispod. Vrijednosti su brojevi, da se s njima može računati.

// IzvoziPrognoze šalje zadnje izdanje prognoze kao .xlsx.
func (h *PrognozeHandler) IzvoziPrognoze(w http.ResponseWriter, r *http.Request) {
	data := h.podaci(r)
	if data.Nema {
		http.Error(w, data.Razlog, http.StatusNotFound)
		return
	}
	z := h.zaglavljeIzvoza(data.CurrentUser)
	k := &xlsxw.Knjiga{LogoPNG: z.LogoPNG}
	for _, t := range data.Tablice {
		listPrognoze(k, z, data, t)
	}
	posaljiXLSX(w, "prognoza_"+time.Now().In(models.Zagreb).Format("2006-01-02_15h")+".xlsx", k)
}

// listPrognoze piše jednu vodu.
func listPrognoze(k *xlsxw.Knjiga, z ZaglavljeIzvoza, data PrognozePageData, t TablicaPrognoza) {
	T, N := xlsxw.T, xlsxw.N
	// U izvoz ulaze samo letve s našom prognozom. Letve samo s mjerenjem
	// (Bratislava, Komárno), samo s tuđom prognozom (Komárom, Letenye,
	// srpske nasuprot našima) ostaju na stranici, ali u tablicu koja se
	// izdaje ne idu.
	var letve []LetvaPrognoze
	for _, x := range t.Letve {
		if x.Racuna != "" {
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
		ime, _ := imeIDrzava(x.Naziv)
		l.Sirine = append(l.Sirine, max(10, float64(len([]rune(ime)))+2,
			float64(len([]rune(drugiRedak(x, t.Naslov))))+2))
	}
	zaglavljeLista(l, z, "PROGNOZA VODOSTAJA — "+t.Naslov,
		"izdano "+data.Izdano+" · dani za 07 h · vodostaj u cm, protok u m³/s", stupaca)

	// Zaglavlje: naziv letve, pa voda i kilometar.
	r := l.Redak()
	red := []xlsxw.Celija{T("Termin", xlsxw.Zaglavlje), T("", xlsxw.Zaglavlje)}
	for _, x := range letve {
		ime, _ := imeIDrzava(x.Naziv)
		red = append(red, T(ime, xlsxw.Zaglavlje))
	}
	l.Dodaj(red...)
	l.Visina(r, 24)
	// Drugi redak: država kraticom (naše letve bez nje), voda gdje nije ista
	// kao naslov lista, i riječni kilometar.
	red = []xlsxw.Celija{T("", xlsxw.Tablica), T("rkm", xlsxw.TablicaSredina)}
	for _, x := range letve {
		red = append(red, T(drugiRedak(x, t.Naslov), xlsxw.TablicaSredina))
	}
	l.Dodaj(red...)
	l.Visina(l.Redak()-1, 18)
	l.PonoviRetke(r, r+1)

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
		"Metoda: prvih dana (redak „model”: satni) hidrološki lanac vodomjernih postaja — vodostaj, odnosno protok " +
		"nizvodne postaje izvodi se iz uzvodnih, uz izmjereno vrijeme propagacije vala i po dijelovima linearnu vezu " +
		"ovisnu o vodnosti, ispravljeno prema zadnjem mjerenju; dalje (dnevni) statistički model na dnevnim vodostajima " +
		"od 1901. — višestruka regresija i metoda analognih situacija. Vodostaj i protok međusobno su preračunati " +
		"krivuljom protoka postaje. Na vrhu lanca Mura (Letenye) i Dunav (Komárom) slijede prognozu mađarske službe.\n" +
		"Raspon je interval od ±1 standardnog odstupanja pogreške prognoze, određenog usporedbom s mjerenjima " +
		"(satni lanac) odnosno rasipanjem analognih situacija (dnevni model): stvarna vrijednost ostaje u rasponu " +
		"u 68 % slučajeva, a u 32 % izlazi iz njega, podjednako iznad i ispod. Zapisan je kao ± kad je " +
		"simetričan, a granicama kad ga preračun krivuljom protoka učini nesimetričnim.\n" +
		"HU — prognoza mađarske hidrološke službe (hydroinfo.hu), uz našu radi usporedbe; njihov raspon " +
		"obuhvaća 70 % slučajeva, pa su dva raspona gotovo izravno usporediva."
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
	sektor := "B"
	if u != nil {
		if d := u.PrimaryDuty(); d != nil && d.SectorID != nil && *d.SectorID != "" {
			sektor = *d.SectorID
		}
	}
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
}

// imeIDrzava rastavlja „Komárom (Mađarska)" na ime i kraticu države; naša
// letva nema zagradu i vraća samo ime.
func imeIDrzava(naziv string) (string, string) {
	i := strings.LastIndex(naziv, " (")
	if i < 0 || !strings.HasSuffix(naziv, ")") {
		return naziv, ""
	}
	drzava := naziv[i+2 : len(naziv)-1]
	if k, ima := drzave[drzava]; ima {
		drzava = k
	}
	return naziv[:i], drzava
}

// drugiRedak je ono što pod imenom letve stoji u zaglavlju: država kraticom
// (naše letve bez nje), voda ondje gdje nije ista kao naslov lista, i riječni
// kilometar bez „rkm", koji stoji jednom, u oznaci retka.
func drugiRedak(x LetvaPrognoze, naslovLista string) string {
	_, drzava := imeIDrzava(x.Naziv)
	var d []string
	if drzava != "" {
		d = append(d, drzava)
	}
	if x.Voda != "" && !strings.Contains(naslovLista, x.Voda) {
		d = append(d, x.Voda)
	}
	if km := strings.TrimSpace(strings.TrimPrefix(x.Stacionaza, "rkm")); km != "" {
		d = append(d, km)
	}
	return strings.Join(d, " · ")
}
