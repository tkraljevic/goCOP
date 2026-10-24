package prognoza

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"gocop/internal/models"
)

// Uzvodni ispravak dnevne prognoze Dunava (Odluke (24. 10. 2026.)). Dnevni
// model Batine, Aljmaša, Vukovara i Iloka gleda Komárom, Budimpeštu i
// Mohács, a val od Wildungsmauera do Batine putuje pet dana. Što su
// Wildungsmauer i Achleiten izmjerili zadnja dva dana, model ne zna, a
// njegova greška 4.–6. dan jasno ide s tim. Ispravak je zato sloj iza
// dnevnog modela: model se uči i računa kao prije, a promjena za dan k se
// pomakne za naučenu grešku
//
//	nova promjena[k] = promjena[k] − (c0[k] + Σ b_i[k]·x_i),
//
// gdje su x promjene vodostaja za 24 i 48 sati na Wildungsmaueru i
// Achleitenu, iz satnih vrijednosti u satu prije izdanja (uzvodnaVrijednost).
// Vrijednost, donja i gornja granica pomiču se zajedno, pa raspon ostaje.
//
// Provjereno izostavljanjem godine na 2012.–8/2026. (svaku godinu računaju
// dnevni model i tablica koji je nisu vidjeli): Dunav 4.–6. dan griješi u
// prosjeku 2,55 cm manje (95 % interval 2,18–2,93 cm, uz MAE 20,7 cm),
// bolje u svih 15 godina, 2024. za 1,7 cm. Passau-Ingling na Innu dodavao je
// 0,03 cm, pa ne ulazi. Prije 2012. satnih vrijednosti nema; zamjenska
// dnevna inačica na valovima 1991.–2010. govori isto.
//
// Zaštita: ispravak ne spušta prognozu dok cilj raste više od UzvodniRast u
// 24 sata i ne diže je više od UzvodnoNajvise. U velikoj vodi val putuje
// 7,5–9,7 dana umjesto pet, pa je puni ispravak, čim je Wildungsmauer počeo
// padati, spuštao Batinu dok je ona još rasla: u valu 9/2024 izdanje 19. 9.
// za Batinu 6. dan −28 → −115 cm. Puni ispravak je preko svih godina bolji
// za još 0,9 cm, ali u svih devet najvećih valova od 1991. gori od modela
// bez ispravka. Ni zaštita u najvećim valovima ne pomaže: blizu vrha
// prognoza ostaje preniska kao i bez nje (Batina 4.–6. dan za 50–55 cm u
// 6/2013 i 9/2024), a u ranijim izdanjima vala ispravak zna dići previsoko
// (UzvodnoNapomena).
//
// Dnevni model uzvodne letve ne čita kao ulaze (nisu u DnevniUlazi): učenje
// bi tada s 1901. palo na 2012., otkad satne vrijednosti postoje, ispale bi
// poplave 2002. i 2006., a analogije bi se birale iz oko pet umjesto
// trinaest tisuća dana.
// Komárom i Budimpešta ispravak nemaju: preko UgadjanjeCvorova mijenjaju
// satni lanac, a sa zaštitom im ostaje manje od pola dobitka. Za njih
// odluka čeka provjeru lanca.

// UzvodneLetve su letve iznad Komároma čija satna mjerenja ispravljaju
// dnevnu prognozu Dunava, redom kojim se ispravak bez njih snalazi: bez
// Achleitena ispravlja se samo prema Wildungsmaueru, bez Wildungsmauera
// nikako (uzvodniSkupovi).
var UzvodneLetve = []string{"wildungsmauer", "achleiten"}

// Skupovi uzvodnog ispravka, kako ih bilježi izbor KljucUzvodno: ispravak
// prema Wildungsmaueru i Achleitenu, samo prema Wildungsmaueru, ili bez
// ispravka jer Wildungsmauer nema mjerenja.
const (
	UzvodnoWA  = 1
	UzvodnoW   = 2
	UzvodnoBez = 3
)

// KljucUzvodno je ključ izbora pod kojim izdanje bilježi skup uzvodnog
// ispravka.
const KljucUzvodno = "uzvodno"

// UzvodnoKasnjenje je koliko se sati, od sata prije izdanja unatrag, traži
// mjerenje uzvodne letve; starije se ne uzima i letva je tada prazna. Tako
// je ispravak i naučen. Austrijske letve uživo u četiri od pet izdanja
// kasne sat, a nisu kasnile više od tri.
const UzvodnoKasnjenje = 3

// UzvodniSkok je koliko vodostaj uzvodne letve smije skočiti u satu; veći
// skok je vjerojatno kvar (bezSkoka). Wildungsmauer u arhivi 2011.–2026.
// nije skočio više od 43 cm u satu, a Achleiten je lažno skakao i do 184 cm,
// sat ili dva pa natrag. Lažna vrijednost ne ulazi ni u svojem satu, ni u
// satu poslije nje, kad se letva vratila. Pravi skok zato kasni sat ili dva,
// što ne smeta, jer val do Batine putuje pet dana.
const UzvodniSkok = 50.0

// UzvodniRast je koliko cilj smije porasti u 24 sata (dnevna razina danas
// prema jučer) a da ga ispravak još smije spustiti.
const UzvodniRast = 5.0

// UzvodnoNajvise je koliko ispravak najviše diže prognozu. Najveću precjenu
// 2024.–8/2026. skida sa 223 na 158 cm, a prosječnu pogrešku ne mijenja.
const UzvodnoNajvise = 100.0

// UzvodnoNapomena je podizanje (cm) iznad kojega izdanje dobiva napomenu,
// da dežurni vidi odakle visok broj: u valovima 6/2013, 1/2024 i 6/2010
// ispravak je dizao do granice kad je model već bio blizu ili previsoko.
const UzvodnoNapomena = 50.0

// uzvodniSkup je jedna tablica ispravka: letve čije promjene ulaze, redom
// 24 h i 48 h po letvi, i po cilju i danu c0, b za svaku promjenu te donja i
// gornja granica naučene greške.
type uzvodniSkup struct {
	letve []string
	koef  map[string][DnevniDosezi + 1][]float64
}

// uzvodniSkupovi su tablice redom kojim se uzimaju: prva čije letve imaju
// mjerenja. Indeks + 1 je skup (UzvodnoWA, UzvodnoW).
var uzvodniSkupovi = []uzvodniSkup{
	{letve: UzvodneLetve, koef: uzvodniKoefWA},
	{letve: UzvodneLetve[:1], koef: uzvodniKoefW},
}

// Tablice ispravka: po cilju i danu c0, b za svaku promjenu te donja i
// gornja granica greške viđene u učenju (cm). Ispravak ê je naučena greška
// modela (prognoza − izmjereno), pa ê > 0 prognozu spušta.
//
// Naučene 24. 10. 2026. najmanjim kvadratima na svim izdanjima
// 2012.–8/2026. (izdanje u 6 h UTC): za 2012.–2023. greške dnevnog modela
// naučenog do 2011. s kišom koja je doista pala, za 2024.–8/2026. modela
// naučenog do 2023. s tada izdanim prognozama kiše. Greške daje alat
// provjera-sat (zasicenost-unatrag), a tablicu skripta koeficijenti.py u
// analizi uzvodnih mjerenja (ugradnja), istim računom promjena kao
// uzvodnaVrijednost. Koeficijenti su na četiri decimale; pomak se zbog toga
// razlikuje najviše 0,03 cm. Program i skripta na svih 3.741 izdanja i letvi
// 2024.–8/2026. daju isti skup i isti pomak (alat uzvodni-provjera), a isto i
// na 538 izdanja, svaki sat, oko 16 skokova Achleitena većih od 50 cm u satu
// (2013.–2025.), gdje radi filtar lažnog skoka (bezSkoka). Učenje uzima
// samo izdanja u 6 h UTC; u njima filtar skida tri očitanja Achleitena.
//
// Tablica vrijedi za dnevni model na čijim je greškama naučena
// (uzvodniOtisak, TestUzvodniOtisak, TestUzvodniOtisakIzlaza). Kad se
// promijene ciljevi, ulazi, kiša, međuslivovi H, I i J u registru slivova
// (njih test ne vidi) ili kotačići i račun dnevnog modela, treba je
// izračunati iznova: provjera-sat s -uci-do 2012-01-01 i savršenom kišom te
// s -uci-do 2024-01-01 i arhiviranim prognozama kiše, pa koeficijenti.py,
// pa provjera izostavljanjem godine (po_godinama.py), i otiske prepisati.
// Greške za učenje moraju biti greške modela bez ispravka: provjera-sat
// satne nizove učitava samo za cilj i njegove ulaze, bez UzvodneLetve, pa
// daje upravo njih. Alat koji mjeri prognozu kakva je uživo (dan prijelaza,
// težina lanca, valovi) mora zato uz njih učitati i UzvodneLetve, kao
// zasicenost-proba -uzvodno; inače mjeri model bez ispravka.
var uzvodniKoefWA = map[string][DnevniDosezi + 1][]float64{
	"batina": {nil,
		{0.0042, -0.0021, 0.0035, 0.0125, -0.0033, -0.9765, 2.7185},
		{-0.0117, 0.0049, -0.0255, 0.0322, -0.0095, -12.2077, 4.2049},
		{-0.1079, -0.1471, -0.0348, 0.0627, -0.0658, -65.7971, 26.4944},
		{-0.2606, -0.3323, -0.0085, -0.1148, -0.1425, -146.1926, 58.8632},
		{-0.3649, -0.4219, 0.0355, -0.3186, -0.2389, -235.6047, 81.2077},
		{-0.4040, -0.4412, 0.0720, -0.4199, -0.3205, -280.1413, 95.3898},
	},
	"aljmas": {nil,
		{-0.0028, 0.0129, -0.0074, 0.0050, -0.0121, -3.7826, 1.9362},
		{-0.0235, 0.0453, -0.0512, 0.0068, -0.0122, -16.0639, 7.2850},
		{-0.0848, -0.0357, -0.0738, 0.0360, -0.0409, -42.8295, 20.7698},
		{-0.1908, -0.1993, -0.0552, -0.0634, -0.0826, -99.4865, 44.3806},
		{-0.2774, -0.3130, -0.0112, -0.2316, -0.1495, -174.9363, 64.1679},
		{-0.3206, -0.3628, 0.0319, -0.3337, -0.2294, -224.4585, 75.3449},
	},
	"vukovar": {nil,
		{-0.0010, 0.0007, 0.0010, 0.0006, -0.0068, -2.0933, 0.8379},
		{-0.0080, 0.0147, -0.0155, -0.0010, -0.0060, -4.9449, 2.4305},
		{-0.0402, -0.0252, -0.0337, 0.0218, -0.0172, -21.7005, 9.9879},
		{-0.1156, -0.1444, -0.0308, -0.0177, -0.0443, -64.3263, 27.1842},
		{-0.1977, -0.2482, -0.0090, -0.1452, -0.0870, -120.0167, 45.3992},
		{-0.2552, -0.3019, 0.0223, -0.2448, -0.1530, -168.8015, 57.5905},
	},
	"ilok": {nil,
		{0.0018, 0.0045, 0.0008, 0.0027, -0.0049, -0.9115, 1.5502},
		{-0.0035, 0.0242, -0.0161, 0.0036, -0.0084, -6.1118, 2.3137},
		{-0.0315, 0.0211, -0.0509, 0.0086, -0.0065, -19.1457, 8.1314},
		{-0.0974, -0.0675, -0.0655, -0.0116, -0.0240, -49.2974, 23.1199},
		{-0.1786, -0.1747, -0.0519, -0.1253, -0.0544, -99.6052, 41.1871},
		{-0.2343, -0.2466, -0.0192, -0.2328, -0.1101, -151.3825, 54.3188},
	},
}

// uzvodniKoefW je rezerva samo s Wildungsmauerom, kad Achleiten nema
// mjerenja; naučena isto kao uzvodniKoefWA.
var uzvodniKoefW = map[string][DnevniDosezi + 1][]float64{
	"batina": {nil,
		{0.0026, 0.0006, 0.0029, -0.6238, 1.3420},
		{-0.0155, 0.0114, -0.0275, -9.8394, 4.2749},
		{-0.1226, -0.1486, -0.0533, -64.7837, 26.8631},
		{-0.2644, -0.4148, -0.0503, -145.3077, 61.5773},
		{-0.3534, -0.5994, -0.0366, -196.9286, 91.0617},
		{-0.3866, -0.6757, -0.0263, -216.6097, 103.5195},
	},
	"aljmas": {nil,
		{-0.0032, 0.0106, -0.0104, -3.2035, 1.3937},
		{-0.0226, 0.0432, -0.0537, -16.0549, 7.3581},
		{-0.0908, -0.0372, -0.0847, -42.8085, 20.5167},
		{-0.1947, -0.2448, -0.0791, -103.5962, 42.5772},
		{-0.2729, -0.4318, -0.0568, -152.8712, 63.8556},
		{-0.3136, -0.5375, -0.0397, -179.0610, 81.2733},
	},
	"vukovar": {nil,
		{-0.0013, -0.0013, -0.0008, -0.6731, 0.2931},
		{-0.0074, 0.0125, -0.0168, -5.0683, 2.3121},
		{-0.0444, -0.0235, -0.0381, -20.9710, 9.9870},
		{-0.1220, -0.1631, -0.0436, -65.7442, 26.6131},
		{-0.1989, -0.3200, -0.0356, -110.9187, 47.6852},
		{-0.2492, -0.4249, -0.0253, -139.3617, 64.5862},
	},
	"ilok": {nil,
		{0.0015, 0.0038, -0.0005, -0.6139, 0.9748},
		{-0.0032, 0.0226, -0.0181, -6.1088, 2.3547},
		{-0.0313, 0.0216, -0.0521, -18.6916, 8.1171},
		{-0.0987, -0.0784, -0.0721, -50.0523, 22.6711},
		{-0.1735, -0.2306, -0.0685, -95.3849, 38.9366},
		{-0.2243, -0.3529, -0.0540, -127.6597, 51.7553},
	},
}

// uzvodniOtisak su postavke dnevnog modela s kojima su izračunate greške na
// kojima su tablice naučene, i račun promjena uzvodnih letvi.
// TestUzvodniOtisak pada kad se koja promijeni: tablice tada treba
// izračunati iznova (vidi gore), pa otisak prepisati.
const uzvodniOtisak = `batina: ulazi [komarom budapest mohacs], kiša [H I J], rezerve []
aljmas: ulazi [komarom budapest mohacs batina osijek donji-miholjac], kiša [H I J], rezerve []
vukovar: ulazi [komarom budapest mohacs batina osijek donji-miholjac], kiša [H I J], rezerve []
ilok: ulazi [komarom budapest mohacs batina osijek donji-miholjac], kiša [H I J], rezerve []
dosezi 6, udio regresije 0.75, analogija 25, težinske false, režim kiše 0, kvadrati kiše false
kiša: dani [1 3 7], unaprijed [2 4 6] (true), korijen false, težina u analogijama 0.5
dan uživo iz najmanje 18 sati
uzvodno: [wildungsmauer achleiten], kašnjenje 3 h, skok 50 cm`

// ImaUzvodniIspravak javlja ima li letva uzvodni ispravak dnevne prognoze.
func ImaUzvodniIspravak(letva string) bool {
	_, ima := uzvodniSkupovi[0].koef[letva]
	return ima
}

// UzvodniIspravak je pomak dnevne prognoze cilja za 1.–6. dan (cm, plus
// diže) i skup kojim je računat; skup je 0 kad cilj ispravak nema, a
// UzvodnoBez kad ni Wildungsmauer nema mjerenja, i tada je pomak nula. rast
// je promjena dnevne razine cilja od jučer (DnevneZnacajke, x[2]).
func UzvodniIspravak(letva string, satni map[string]Niz, sada int64, rast float64) (pomak [DnevniDosezi + 1]float64, skup int) {
	if !ImaUzvodniIspravak(letva) {
		return pomak, 0
	}
	skup, x, _ := uzvodnoStanje(satni, sada)
	if x == nil {
		return pomak, skup
	}
	koef := uzvodniSkupovi[skup-1].koef[letva]
	for k := 1; k <= DnevniDosezi; k++ {
		pomak[k] = uzvodniPomakDana(koef[k], x, rast)
	}
	return pomak, skup
}

// uzvodnaRupa je uzvodna letva bez mjerenja koje ispravak treba; prije je
// koliko je sati to mjerenje prije sata prije izdanja: 0, 24 ili 48.
type uzvodnaRupa struct {
	letva string
	prije int64
}

// uzvodnoStanje bira skup ispravka prema mjerenjima: prvi čije letve imaju
// sve promjene, s tim promjenama. bez je letva zbog koje je otpao zadnji
// skup prije njega, s mjerenjem koje joj fali; prazno kad je uzet prvi.
func uzvodnoStanje(satni map[string]Niz, sada int64) (skup int, x []float64, bez uzvodnaRupa) {
	for i, s := range uzvodniSkupovi {
		var fali uzvodnaRupa
		if x, fali = uzvodnePromjene(s.letve, satni, sada); fali.letva == "" {
			return i + 1, x, bez
		}
		bez = fali
	}
	return len(uzvodniSkupovi) + 1, nil, bez
}

// uzvodnePromjene su promjene vodostaja letvi za 24 i 48 sati do sata prije
// izdanja, redom po letvi; fali je prva letva kojoj neko od tri mjerenja
// nedostaje, i koje.
func uzvodnePromjene(letve []string, satni map[string]Niz, sada int64) (x []float64, fali uzvodnaRupa) {
	h := sada - 1
	for _, l := range letve {
		var v [3]float64
		for i, prije := range [3]int64{0, 24, 48} {
			var ima bool
			if v[i], ima = uzvodnaVrijednost(satni[l], h-prije); !ima {
				return nil, uzvodnaRupa{letva: l, prije: prije}
			}
		}
		x = append(x, v[0]-v[1], v[0]-v[2])
	}
	return x, uzvodnaRupa{}
}

// uzvodnaVrijednost je izmjereni vodostaj u satu sat ili, kad ga nema, do
// UzvodnoKasnjenje − 1 sati ranije, bez lažnog skoka (bezSkoka); ne
// premošćuje se, jer je ispravak učen na izmjerenim satima.
func uzvodnaVrijednost(n Niz, sat int64) (float64, bool) {
	for h := sat; h > sat-UzvodnoKasnjenje; h-- {
		if v, ima := izmjerenoU(n, h); ima {
			return bezSkoka(n, h, v), true
		}
	}
	return 0, false
}

// bezSkoka je vodostaj v iz sata h bez lažnog skoka. Kad je v od sata prije
// skočio više od UzvodniSkok, uzima se sat prije, osim kad se v vratio na
// razinu dva ili tri sata prije, a sat prije je od nje daleko: lažan je
// tada bio sat prije (300, 370, 290 daje 290). Kad se v slaže sa satom
// prije, a oba su skočila od stalne razine dva sata prije, to je drugi sat
// skoka i uzima se ta razina (300, 300, 300, 120, 125 daje 300).
func bezSkoka(n Niz, h int64, v float64) float64 {
	p, imaP := izmjerenoU(n, h-1)
	switch {
	case !imaP:
		return v
	case skokVodostaja(v, p):
		return vratioSe(n, h, v, p)
	}
	return drugiSatSkoka(n, h, v, p)
}

// vratioSe je v kad se nakon skoka vratio na razinu dva ili tri sata prije
// sata h od koje je p, vodostaj sata prije, daleko: lažan je tada bio p.
// Inače je p, pa pravi skok kasni.
func vratioSe(n Niz, h int64, v, p float64) float64 {
	for _, k := range [2]int64{2, 3} {
		if blizuU(n, h-k, v) && !blizuU(n, h-k, p) {
			return v
		}
	}
	return p
}

// drugiSatSkoka je razina dva sata prije sata h kad su od nje skočili i v i
// p (sat prije), a ona je stalna (stalnaRazina); inače v.
func drugiSatSkoka(n Niz, h int64, v, p float64) float64 {
	if r, ima := stalnaRazina(n, h-2); ima && skokVodostaja(v, r) && skokVodostaja(p, r) {
		return r
	}
	return v
}

// stalnaRazina je očitanje u satu h kad se slaže sa satom prije, a taj sa
// satom prije sebe. Razina od koje se skok mjeri tako nije ni sama kraj
// lažnog skoka: iza dva lažna sata (300, 120, 125, 302, 304) sat 304 ne
// mjeri se od 125.
func stalnaRazina(n Niz, h int64) (float64, bool) {
	r, ima := izmjerenoU(n, h)
	p, imaP := izmjerenoU(n, h-1)
	if !ima || !imaP || skokVodostaja(r, p) || !blizuU(n, h-2, p) {
		return 0, false
	}
	return r, true
}

// blizuU javlja ima li očitanje u satu h i je li od v najviše UzvodniSkok.
func blizuU(n Niz, h int64, v float64) bool {
	u, ima := izmjerenoU(n, h)
	return ima && !skokVodostaja(v, u)
}

// skokVodostaja javlja jesu li dva vodostaja razmaknuta više od UzvodniSkok.
func skokVodostaja(a, b float64) bool { return math.Abs(a-b) > UzvodniSkok }

// izmjerenoU je očitanje točno u zadanom satu, bez premošćivanja (Niz.U).
func izmjerenoU(n Niz, sat int64) (float64, bool) {
	i := sort.Search(len(n.Sati), func(i int) bool { return n.Sati[i] >= sat })
	if i < len(n.Sati) && n.Sati[i] == sat {
		return n.Iznosi[i], true
	}
	return 0, false
}

// uzvodniPomakDana je pomak prognoze jednog dana iz retka tablice (c0, b,
// donja i gornja granica) i promjena x: −ê, uz granice iz učenja i zaštitu.
func uzvodniPomakDana(red, x []float64, rast float64) float64 {
	e := red[0]
	for i, b := range red[1 : 1+len(x)] {
		e += b * x[i]
	}
	e = min(max(e, red[1+len(x)]), red[2+len(x)])
	switch {
	case e > 0 && rast > UzvodniRast:
		return 0 // ne spušta dok cilj raste
	case e < -UzvodnoNajvise:
		return UzvodnoNajvise
	}
	return -e
}

// oznakaDnevnog je oznaka modela dnevne prognoze: s kišom ili bez nje, i s
// „-uzv” kad je ispravak računao s uzvodnim mjerenjima (skup UzvodnoWA ili
// UzvodnoW). „-uzv” stoji i kad je zaštita ispravak poništila na svim
// danima: oznaka kaže da je ispravak radio, a ne da je promijenio broj, pa
// se izdanja s njom i bez nje mogu uspoređivati zajedno sa zaštitom.
func oznakaDnevnog(kisa bool, skup int) string {
	uzvodno := skup >= UzvodnoWA && skup <= UzvodnoW
	switch {
	case kisa && uzvodno:
		return ModelDnevniKisaUzv
	case kisa:
		return ModelDnevniKisa
	case uzvodno:
		return ModelDnevniUzv
	}
	return ModelDnevni
}

// UzvodnoIzdanja je što je uzvodni ispravak imao u izdanju, za izbor i
// napomene: skup kojim je računao (0 kad nijedan cilj s ispravkom nije
// izdan), uzvodna letva zbog koje je otpao bolji skup i ciljevi kojima je
// ispravak digao prognozu više od UzvodnoNapomena.
type UzvodnoIzdanja struct {
	Skup int
	// Bez je uzvodna letva bez mjerenja koje ispravak treba. Fali je sat tog
	// mjerenja: sat prije izdanja, ili Prije (24 ili 48) sati prije njega.
	// Kad fali sat prije izdanja, Zadnje je zadnje očitanje letve prije
	// sati u kojima se mjerenje traži (UzvodnoKasnjenje); 0 kad ga nema.
	Bez       RupaUlaza
	Fali      int64
	Prije     int64
	Podignuto []UzvodnoPodizanje
}

// UzvodnoPodizanje je najveće podizanje dnevne prognoze cilja: dan i cm.
type UzvodnoPodizanje struct {
	Letva string
	Dan   int
	Cm    float64
}

// uzvodnoIzdanja slaže stanje uzvodnog ispravka iz satnih nizova kojima je
// dnevna prognoza računata i iz izdanih dana.
func uzvodnoIzdanja(satni map[string]Niz, sada int64, dnevne []DnevnaIzdana) UzvodnoIzdanja {
	podignuto, ima := podizanjaUzvodnog(dnevne)
	if !ima {
		return UzvodnoIzdanja{}
	}
	u := UzvodnoIzdanja{Podignuto: podignuto}
	var rupa uzvodnaRupa
	if u.Skup, _, rupa = uzvodnoStanje(satni, sada); rupa.letva == "" {
		return u
	}
	u.Bez.Letva, u.Prije, u.Fali = rupa.letva, rupa.prije, sada-1-rupa.prije
	if z, imaZ := satni[rupa.letva].ZadnjiSatDo(u.Fali - UzvodnoKasnjenje); imaZ && rupa.prije == 0 {
		u.Bez.Zadnje = z
	}
	return u
}

// podizanjaUzvodnog su ciljevi s ispravkom kojima je ispravak digao neki dan
// više od UzvodnoNapomena, s danom najvećeg podizanja; ima kaže je li u
// izdanju ijedan cilj s ispravkom. Uspoređuje se zaokruženo na centimetar,
// kako napomena i piše.
func podizanjaUzvodnog(dnevne []DnevnaIzdana) (out []UzvodnoPodizanje, ima bool) {
	po := map[string]int{}
	for _, d := range dnevne {
		if !ImaUzvodniIspravak(d.Letva) {
			continue
		}
		ima = true
		if math.Round(d.uzvodno) <= UzvodnoNapomena {
			continue
		}
		p := UzvodnoPodizanje{Letva: d.Letva, Dan: d.Dan, Cm: d.uzvodno}
		if i, bilo := po[d.Letva]; !bilo {
			po[d.Letva] = len(out)
			out = append(out, p)
		} else if p.Cm > out[i].Cm {
			out[i] = p
		}
	}
	return out, ima
}

// upisi bilježi skup uzvodnog ispravka u izbor izdanja.
func (u UzvodnoIzdanja) upisi(izbor map[string]Izbor) {
	if u.Skup == 0 {
		return
	}
	izbor[KljucUzvodno] = Izbor{Inacica: u.Skup, Opis: u.opis()}
}

// opis kaže prema čemu je dnevna prognoza Dunava ispravljena, šiframa letvi.
func (u UzvodnoIzdanja) opis() string {
	if u.Skup > len(uzvodniSkupovi) {
		return "dnevna prognoza Dunava od 4. dana računa se bez ispravka prema uzvodnim letvama"
	}
	samo := ""
	if u.Bez.Letva != "" {
		samo = "samo "
	}
	return "dnevna prognoza Dunava od 4. dana ispravlja se " + samo + "prema " + premaLetvama(uzvodniSkupovi[u.Skup-1].letve)
}

// premaLetvama nabraja letve iza „prema”: „letvi x” ili „letvama x i y”.
func premaLetvama(letve []string) string {
	if len(letve) == 1 {
		return "letvi " + letve[0]
	}
	return "letvama " + strings.Join(letve[:len(letve)-1], ", ") + " i " + letve[len(letve)-1]
}

// upozorenjaUzvodnog su napomene uzvodnog ispravka: uzvodna letva bez
// mjerenja (ispravak samo prema Wildungsmaueru ili bez njega) i ciljevi
// koje je ispravak digao više od UzvodnoNapomena. Opis je cijela napomena,
// jer stariji čvorovi ovu vrstu ne znaju i pokazuju samo njega.
func upozorenjaUzvodnog(u UzvodnoIzdanja) []Upozorenje {
	var out []Upozorenje
	if u.Bez.Letva != "" {
		out = append(out, Upozorenje{Vrsta: VrstaUzvodno, Tezina: TezinaNapomena, Letva: u.Bez.Letva,
			Zadnje: u.Bez.Zadnje, Opis: u.opisRupe()})
	}
	if len(u.Podignuto) > 0 {
		out = append(out, Upozorenje{Vrsta: VrstaUzvodno, Tezina: TezinaNapomena, Opis: opisPodizanja(u.Podignuto)})
	}
	return out
}

// opisRupe je napomena o uzvodnoj letvi bez mjerenja, šiframa letvi: koje
// mjerenje fali i kako se zato računa dnevna prognoza Dunava, npr. „fali
// mjerenje 22. 10. u 17 h (prije 24 sata); dnevna prognoza Dunava od 4. dana
// ispravlja se samo prema letvi wildungsmauer”. Letva koja javlja, a fali joj
// mjerenje od prije 24 ili 48 sati, tako ne piše da nema mjerenja.
func (u UzvodnoIzdanja) opisRupe() string {
	sto := "nema mjerenja"
	switch {
	case u.Prije > 0:
		sto = fmt.Sprintf("fali mjerenje %s (prije %d %s)", satOpisa(u.Fali), u.Prije,
			models.UzBrojHR(int(u.Prije), "sat", "sata", "sati"))
	case u.Bez.Zadnje > 0:
		sto += " od " + satOpisa(u.Bez.Zadnje)
	}
	return sto + "; " + u.opis()
}

// satOpisa je sat od epohe po mjesnom vremenu, npr. „22. 10. u 17 h”, kako ga
// piše i stranica.
func satOpisa(sat int64) string {
	return time.Unix(sat*3600, 0).In(models.Zagreb).Format("2. 1. u 15 h")
}

// opisPodizanja je napomena o podizanju, šiframa letvi, npr. „ispravak
// prema uzvodnim letvama digao je dnevnu prognozu za više od 50 cm: batina
// 6. dan za 62 cm. U velikoj vodi ispravak zna dići i previsoko”.
func opisPodizanja(p []UzvodnoPodizanje) string {
	dijelovi := make([]string, len(p))
	for i, x := range p {
		dijelovi[i] = fmt.Sprintf("%s %d. dan za %d cm", x.Letva, x.Dan, int(math.Round(x.Cm)))
	}
	return fmt.Sprintf("ispravak prema uzvodnim letvama digao je dnevnu prognozu za više od %.0f cm: %s. "+
		"U velikoj vodi ispravak zna dići i previsoko", UzvodnoNapomena, strings.Join(dijelovi, ", "))
}
