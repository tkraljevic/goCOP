// namjesti-prognozu mjeri kako se letve slažu sa svojim uzvodnim ulazima i
// sprema izmjereno u bazu prognoza. Sam račun prognoze ne radi — ovo je korak
// koji mu daje brojke.
//
//	namjesti-prognozu -probno          samo ispiši što bi se namjestilo
//	namjesti-prognozu                  namjesti i spremi
//	namjesti-prognozu -proba "aljmas = batina + belisce + siga"
//	                                   isprobaj jednu postavku, ništa ne spremaj
//
// Veze se mjere po pojasima vodnosti jer se kašnjenje mijenja s razinom: na
// Batini → Aljmaš ide od 11 sati pri maloj vodi do 39 pri velikoj, jer se
// Kopački rit puni i val uspori.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"gocop/internal/prognoza"

	_ "modernc.org/sqlite"
)

// Velicine kaže u čemu se koja letva vodi — i kao cilj, i kad ulazi drugamo.
// Ne bira se sama: na gornjoj Dravi korito se ispod lanca hidroelektrana
// produbljuje, pa vodostaj kroz desetljeća mijenja značenje i Novo Virje iz
// vodostaja drži r 0,49–0,61 umjesto 0,87–0,97 iz protoka. Na Dunavu je
// obrnuto, ondje je vodostaj bolji. Vrbovka, Moslavina, Donji Miholjac,
// Belišće, Ilok i Osijek protok u arhivi nemaju, pa im izbora ni nema.
var Velicine = map[string]string{
	"donja-dubrava":    "protok",
	"he-dubrava":       "protok",
	"letenye":          "vodostaj",
	"gorican":          "protok",
	"tikves":           "vodostaj",
	"mursko-sredisce":  "protok",
	"komarno":          "vodostaj",
	"bogojevo":         "vodostaj",
	"bezdan":           "vodostaj",
	"kotoriba":         "vodostaj",
	"zeleznica":        "protok",
	"tuhovec":          "protok",
	"ludbreg":          "protok",
	"botovo":           "protok",
	"novo-virje":       "protok",
	"terezino-polje":   "protok",
	"vrbovka":          "vodostaj",
	"moslavina":        "vodostaj",
	"donji-miholjac":   "vodostaj",
	"belisce":          "vodostaj",
	"batina":           "vodostaj",
	"aljmas":           "vodostaj",
	"dalj":             "vodostaj",
	"vukovar":          "vodostaj",
	"sotin":            "vodostaj",
	"mohovo":           "vodostaj",
	"ilok":             "vodostaj",
	"osijek":           "vodostaj",
	"kapelna":          "vodostaj",
	"miholjacki-porec": "vodostaj",
	"benicanci-prkos":  "vodostaj",
	"marjancaci":       "vodostaj",
	"jelengrad":        "vodostaj",
	"siga":             "vodostaj",
	"petres":           "vodostaj",
	// Mađarske letve uzvodno od Batine i uz lijevu obalu Drave. Nisu naše, ali
	// su na istoj vodi i imaju satnu povijest — a val ne mari za granicu.
	"nagybajcs":     "vodostaj",
	"achleiten":     "vodostaj",
	"kienstock":     "vodostaj",
	"wildungsmauer": "vodostaj",
	"komarom":       "vodostaj",
	"esztergom":     "vodostaj",
	"budapest":      "vodostaj",
	"dunafoldvar":   "vodostaj",
	"paks":          "vodostaj",
	"baja":          "vodostaj",
	"dunaszekcso":   "vodostaj",
	"mohacs":        "vodostaj",
	"szentborbas":   "vodostaj",
	"dravaszabolcs": "vodostaj",
	"ortilos":       "vodostaj",
	"vizvar":        "vodostaj",
	"barcs":         "vodostaj",
}

// Racun je jedna letva i ono iz čega se računa. Prvi ulaz je glavni tok: po
// njemu se dijele pojasi vodnosti.
type Racun struct {
	Letva string
	Ulazi []string
}

// Rezerve su drugi ulazi za istu kariku, redom kojim se uzimaju kad glavni
// ulaz zakaže. Svaka se namješta zasebno (inačica 1, 2, …) i čeka u bazi;
// pri izdavanju se uzme prva čiji su ulazi svježi. Pravilo je „preskoči
// jednu”: iduća uzvodna letva, pa srpske i mađarske letve preko puta. Kad
// arhiva za rezervu nema dovoljno zajedničkih sati, namještanje je preskoči
// i javi; tablicu je potvrdio korisnik 24. 9. 2026.
var Rezerve = map[string][][]string{
	"komarom":        {{"wildungsmauer"}},
	"esztergom":      {{"komarno"}},
	"budapest":       {{"komarom"}},
	"dunafoldvar":    {{"esztergom"}},
	"paks":           {{"budapest"}},
	"baja":           {{"dunafoldvar"}},
	"dunaszekcso":    {{"paks"}},
	"mohacs":         {{"baja"}},
	"batina":         {{"dunaszekcso"}},
	"aljmas":         {{"mohacs", "belisce"}, {"bezdan", "belisce"}},
	"dalj":           {{"batina"}, {"bogojevo"}},
	"vukovar":        {{"aljmas"}, {"bogojevo"}},
	"sotin":          {{"dalj"}},
	"mohovo":         {{"vukovar"}},
	"ilok":           {{"sotin"}},
	"botovo":         {{"he-dubrava", "letenye"}, {"donja-dubrava", "gorican"}, {"he-dubrava", "kotoriba"}, {"he-dubrava", "mursko-sredisce"}},
	"novo-virje":     {{"he-dubrava", "gorican"}, {"he-dubrava", "letenye"}},
	"terezino-polje": {{"botovo"}, {"vizvar"}},
	"szentborbas":    {{"barcs"}},
	"vrbovka":        {{"terezino-polje"}},
	"moslavina":      {{"szentborbas"}},
	"donji-miholjac": {{"vrbovka"}},
	"dravaszabolcs":  {{"moslavina"}},
	"belisce":        {{"donji-miholjac"}},
	"osijek":         {{"dravaszabolcs", "aljmas"}, {"belisce", "batina"}, {"belisce", "dalj"}},
}

// SamoPovezane su letve koje slijede ulaz samo pri dovoljnoj vodi: pojasi u
// kojima veza ne drži (r ispod NajmanjeSlaganje) označe se nepovezanima i u
// njima se prognoza ne izdaje — kartica tada kaže da letva nije povezana sa
// živom vodom.
var SamoPovezane = map[string]bool{"tikves": true}

// NajmanjeSlaganje je r ispod kojeg pojas letve iz SamoPovezane vrijedi kao
// nepovezan. Na Tikvešu su odvojeni pojasi na 0,12–0,20, povezani na 0,93+.
const NajmanjeSlaganje = 0.5

// Batina, Donja Dubrava i Letenye nemaju svoj račun: ništa uzvodno od njih
// nemamo u arhivi. Oni su ulaz, i oni određuju dokle prognoza seže.
var Tokovi = []struct {
	Ime    string
	Racuni []Racun
}{
	{"Drava", []Racun{
		// Botovo je nizvodno od ušća Mure kod Legrada. Bez Mure mu veza s
		// Donjom Dubravom drži svega R² 0,24–0,45 po pojasu, a Dubravi ispadne
		// nagib 2,07 — protok koji se na dvadeset kilometara udvostruči. To
		// nije bio val nego Mura koja se u njemu skrivala.
		// Bednja se ulijeva u staro korito Drave, koje se s odvodnim kanalom
		// HE Dubrava spaja kod Donje Dubrave. Lanac joj ide Železnica (rkm
		// 70,4) → Tuhovec (31,4) → Ludbreg (12,7). Tuhovec Ludbregu drži r
		// 0,92 uz kašnjenje 5 h, Železnica Tuhovcu slabije (r 0,79, rasap 12,9
		// m³/s), ali kašnjenje joj raste s vodnošću 0→4 h, pa produljuje lanac:
		// Ludbreg s njom ide 37/40/26/7/2 % bolje od postojanosti umjesto
		// 32/21/7/1/lošije.
		//
		// Plitvica u Bednju ne utječe. Teče usporedno s njom, sjevernije, i
		// ulijeva se u staro korito Drave uz Bednju, kod Donje Dubrave. Zato joj
		// ni na jednoj bednjanskoj letvi nije mjesto među ulazima, iako u
		// namještanju izgleda kao pritoka: Ludbregu ulazi s kašnjenjem 0 h,
		// prozorom od sata i nagibom 0,63 (Krkanec) odnosno 0,70 (Vidovićev
		// Mlin) i diže mu r s 0,924 na 0,965 odnosno 0,952. To nije voda koja
		// stiže nego ista kiša na dva susjedna sliva, i izvan namještanja od
		// toga ne ostane ništa: Ludbreg ostaje na 1,7/2,6/5,2/8,9/10,6 m³/s.
		//
		// Botovu, kamo Plitvica stvarno pripada, ulazi kao natopljenost:
		// Vidovićev Mlin s kašnjenjem 32 h i prozorom od 49 sati (gornja
		// granica pretrage) uz nagib 7,14, Krkanec s 33 h i istim prozorom —
		// isti potpis koji je Bednja imala na Botovu, a taj izvan namještanja
		// nije donio ništa. Ni vlastiti lanac joj ne pomaže: od Krkanca do
		// Vidovićeva Mlina je 16,8 km, a kašnjenje je opet 0 h uz r 0,21–0,33
		// na srednjim vodama, pa Vidovićev Mlin iz Krkanca ide svega
		// 15/13/5/3/4 % bolje od postojanosti. Plitvica je duga pedesetak
		// kilometara sa sitnim podsljevovima: kiša padne na cijeli sliv
		// odjednom i val nastaje posvuda istodobno umjesto da putuje. Bednja
		// kašnjenje ima jer je dulja i strmija. Obje letve ostaju upisane i
		// uvoze se, samo nisu ulaz.
		//
		// Lepoglava ostaje vani. Kao sporedni ulaz Ludbregu ne vrijedi ništa
		// (8,0 → 7,7 m³/s rasapa), a ni Železnici iznad koje leži: ondje joj je
		// kašnjenje 0 h na svim pojasima uz prozor od 21 sat, dakle model je
		// čita kao oborinu. Kašnjenje nula znači i da prognoza Železnice iz nje
		// traži prognozu same Lepoglave, pa lanac ne produljuje ni za sat.
		{"tuhovec", []string{"zeleznica"}},
		{"ludbreg", []string{"tuhovec"}},
		// Bednja Botovu nije ulaz. U namještanju izgleda kao da jest — na
		// najvišem pojasu skida rasap s 93,5 na 83,9 m³/s — ali model je čita
		// kao natopljenost sliva, ne kao val: kašnjenje 26 h uz prozor od 49
		// sati, a 49 je gornja granica pretrage. Izvan namještanja od toga ne
		// ostane ništa: pogreška Botova s Bednjom i bez nje je 51,5/51,7,
		// 74,7/74,7, 95,4/95,0, 146,5/146,6 i 181,3/181,4 m³/s na 6, 12, 24,
		// 48 i 72 sata, mjereno 2023.–2025., dakle i preko svibnja 2023. kad
		// je Bednja imala pravi val. Nije ni dvostruko brojenje: ulaz je
		// ispuštanje elektrane, a Bednja i Plitvica ulaze u staro korito ispod
		// njega. Botovu nedostaje raspored ispuštanja HE Dubrava unaprijed, a
		// to nijedna pritoka ne nadomješta. (Ti brojevi su mjereni dok je niz
		// elektrane u arhivi stajao sat-dva prerano; na ispravljenom nizu
		// pogreška je 61,6/79,3/100,0/149,5/183,0 m³/s.)
		//
		// Ulaz je he-dubrava, istjecanje elektrane s mletva.voda.hr, a ne
		// donja-dubrava. U arhivi su to isti brojevi, ali uživo Donja Dubrava
		// javlja protok preračunat iz vodostaja krivuljom, a model je namješten
		// na istjecanje elektrane.
		//
		// Muru nosi Goričan, naša letva nasuprot Letenyeu (isti rkm), s
		// javnim izvorom i satnim nizom od 1982.: u probi je Botovu u
		// najvišem pojasu rasap 77,8 m³/s prema 97,0 s Letenyeom, na nižima
		// 29,5–47,1 prema 26,7–44,7 (Letenye ima kraće preklapanje s
		// elektranom). Letenye je prva rezerva; njegova mađarska prognoza
		// ostaje samo za usporedbu.
		{"botovo", []string{"he-dubrava", "gorican"}},
		{"novo-virje", []string{"botovo"}},
		{"terezino-polje", []string{"novo-virje"}},
		// Szentborbás i Drávaszabolcs leže na samoj Dravi, između naših letvi,
		// pa ulaze kao karike a ne kao sporedni ulazi. Kao sporedni ulaz letva
		// nema vlastiti račun, pa je se na dugom dosegu drži nepomičnom — i to
		// košta više nego što u namještanju donese: Belišće je tako na 48 sati
		// palo s 18,9 na 26,0 cm.
		{"szentborbas", []string{"terezino-polje"}},
		{"vrbovka", []string{"szentborbas"}},
		{"moslavina", []string{"vrbovka"}},
		{"donji-miholjac", []string{"moslavina"}},
		{"dravaszabolcs", []string{"donji-miholjac"}},
		{"belisce", []string{"dravaszabolcs"}},
		// Pritočni lanci Karašice i Vučice: vlastite prognoze za Valpovštinu,
		// ne ulazi Dravi. Ulaze tek s telemetrijom vrhova (Kapelna, Beničanci).
		{"miholjacki-porec", []string{"kapelna"}},
		{"marjancaci", []string{"benicanci-prkos"}},
		// Karašica Osijeku nije ulaz, iako se u Dravu ulijeva između Belišća i
		// Osijeka (kod Petrijevaca; letva Belišće 5152 je 24,5 km uzvodno od
		// ušća, s dnevnim očitanjem u 7:30). Poreč i Kapelna traženi su i do
		// 96 sati: u vodostaju nagib −0,00, u protoku kašnjenje na granici
		// pretrage uz prozor 31–49 h i nagib 0,15–0,38 cm po m³/s — natoplje-
		// nost, ne val. Dvadesetak kubika Karašice u tisuću dravskih, uz
		// dunavski uspor s Aljmaša, u ostatku Osijeka nema se što naći. Ni
		// Belišću ne pomaže (kašnjenje 48 h, prozor 49 h), jer je ušće ispod
		// njega. Isto vrijedi za Vučicu, koja se s Karašicom sastaje pred
		// ušćem: Beničanci-Prkos na devetnaest godina satnog preklopa s
		// Osijekom (2003.–2021.) i pretragom do 96 sati ulaze u vodostaju s
		// nagibom 0,02 uz prozor od 49 sati, a u protoku s kašnjenjem 96 h —
		// rasap Osijeka ne pomakne se ni za desetinku. Nije analogija nego
		// mjerenje. Karašica ima svoj lanac, Kapelna → Poreč (vodostaj r 0,89,
		// kašnjenje 6–17 h; protok r 0,88, 8 h), ali ulazi tek kad Kapelna i
		// Poreč budu imali živa očitanja u bazi — vrh lanca bez očitanja ruši
		// cijelu prognozu, ne samo svoj krak. Isto čeka Vučica: Beničanci →
		// Marjančaci na poplavnom pojasu drži r 0,82 uz kašnjenje 6 h i nagib
		// 0,96 (2008.–2021.; na tri godine radara bilo je 0,47, jer poplava
		// nije bilo), a u hindcastu 2015.–2021. ide 17/29/28/13/7 % bolje od
		// postojanosti. Orahovica joj nije vrh: prema Beničancima na 2003.–2021.
		// drži r 0,60 uz kašnjenje 0 h, prozor 31–37 h i nagib 6–14 —
		// natopljenost, jer Vučica vodu dobiva od Krndije ispod Orahovice.
		// Probni lanci namještaju se u zasebnu bazu (-baza), da živoj prognozi
		// ne podmetnu vrh bez očitanja.
	}},
	{"Dunav", []Racun{
		// Uzvodno od Batine ide niz mađarskih letvi. Batina je dugo bila vrh
		// lanca, ali samo zato što se nije pogledalo iznad nje: Mohács je 22 km
		// uzvodno, a Esztergom 294. Zbroj kašnjenja od Esztergoma do Batine je
		// 45–66 sati, i to nasljeđuje cijeli krak nizvodno.
		// Vrh je Wildungsmauer, s 48-satnom prognozom Donje Austrije
		// (noel.gv.at), da lanac ne ovisi o mađarskoj prognozi Komároma.
		// Austrijski Dunav iznad njega niz je akumulacija kroz koje val
		// prolazi brzo (Achleiten → Kienstock → Wildungsmauer 2–5, 2–4 i
		// 6–7 h), pa uzvodnije karike ne dodaju ništa što prognoza vrha
		// već ne nosi. Između Wildungsmauera i Nagybajcsa ulaze Morava i
		// Gabčíkovo, koje karika nosi u rasapu (14–20 cm; namješteno do
		// rujna 2024.: r 0,76–0,89, kašnjenje 6–7 h); Angern na Moravi
		// ući će kao pritok kad se skupi satna povijest. Komárom iz
		// Nagybajcsa (3–4 h) i Wildungsmauera drži r 0,85–0,97 uz rasap
		// 8–15 cm; sam Nagybajcs 10–18 cm. Rajka nije karika: leži na
		// starom koritu ispod Čunova, pa pravu vodu vidi tek kad se kanal
		// Gabčíkovo prelijeva. Ni Bavarska ne pomaže: Achleiten iz
		// Hofkirchena i Inna u Passau-Inglingu (GKD, satno od 2012.) kasni
		// 0–2 h — leži dva kilometra ispod ušća Inna.
		{"nagybajcs", []string{"wildungsmauer"}},
		{"komarom", []string{"nagybajcs", "wildungsmauer"}},
		{"esztergom", []string{"komarom"}},
		{"budapest", []string{"esztergom"}},
		{"dunafoldvar", []string{"budapest"}},
		{"paks", []string{"dunafoldvar"}},
		{"baja", []string{"paks"}},
		{"dunaszekcso", []string{"baja"}},
		{"mohacs", []string{"dunaszekcso"}},
		{"batina", []string{"mohacs"}},
		// Drava se ulijeva u Dunav kod Aljmaša, pa Aljmaš nije samo dunavska
		// letva. Ovdje se dva kraka sastaju.
		{"aljmas", []string{"batina", "belisce"}},
		{"dalj", []string{"aljmas"}},
		{"vukovar", []string{"dalj"}},
		{"sotin", []string{"vukovar"}},
		{"mohovo", []string{"sotin"}},
		{"ilok", []string{"mohovo"}},
		// Tikveš u Kopačkom ritu: pri maloj i srednjoj vodi rit je odvojen od
		// Dunava (r 0,15), a kad Batina prijeđe oko 120 cm uspor uđe u rit i
		// Tikveš ga slijedi (r 0,93–0,99, s Osijekom ±8 cm u najvišem pojasu).
		// Zato je SamoPovezan: donji pojasi se označe nepovezanima i ondje se
		// ne prognozira. Crpna stanica se zanemaruje.
		{"tikves", []string{"batina", "osijek"}},
	}},
	{"ušće", []Racun{
		// Osijek nema krivulje protoka i nikad je neće imati: blizu ušća veza
		// vodostaja i protoka nije jednoznačna. Zato ide na vodostaj, i zato mu
		// treba i Dunav — razinu mu jednako drži uspor odozdo koliko dotok
		// odozgo.
		{"osijek", []string{"belisce", "aljmas"}},
		// Jelengrad kod Petrijevaca, pet kilometara od ušća Karašice i Vučice,
		// nije ulaz nego izlaz Drave: na visokoj vodi prati Osijek s nagibom
		// 0,90 bez kašnjenja (uspor), a Poreč uzvodno ne prati uopće (r 0,06
		// do 0,19). Iz Osijeka i Belišća drži r 0,92 na najvišem pojasu, pa
		// Petrijevci dobivaju prognozu iz Drave koju već imamo.
		{"jelengrad", []string{"osijek", "belisce"}},
	}},
}

func main() {
	arhivaPut := flag.String("arhiva", "data/vodostaji.db", "arhiva vodostaja")
	bazaPut := flag.String("baza", "data/prognoze.db", "baza prognoza")
	probno := flag.Bool("probno", false, "samo ispiši što bi se namjestilo")
	bezS := flag.String("bez", "", "razdoblja koja namještanje ne vidi, npr. 2013-05-25..2013-07-05,2024-09-01..2024-10-15")
	doS := flag.String("do", "", "namješta samo na podacima prije tog datuma (YYYY-MM-DD), za poštenu usporedbu")
	proba := flag.String("proba", "", `isprobaj jednu postavku, npr. "aljmas = batina + belisce"`)
	// Sporednom ulazu pretraga stane na 48 sati, jer dulje kašnjenje na našim
	// pritokama i nema — osim Karašice, kojoj je od Poreča do ušća pedesetak
	// kilometara spore nizinske rijeke pa Dravom do Osijeka. Za takvu probu
	// granica se smije pomaknuti; namještanje ostaje na zadanoj.
	pomakPritoke := flag.Int("pomak-pritoke", prognoza.NajveciPomakPritoka,
		"dokle se traži kašnjenje sporednog ulaza, u satima")
	flag.Parse()
	prognoza.NajveciPomakPritoka = *pomakPritoke
	for _, r := range strings.Split(*bezS, ",") {
		if r = strings.TrimSpace(r); r == "" {
			continue
		}
		a, b, ok := strings.Cut(r, "..")
		od, err1 := time.Parse("2006-01-02", a)
		do, err2 := time.Parse("2006-01-02", b)
		if !ok || err1 != nil || err2 != nil {
			log.Fatalf("-bez: %q nije oblika 2013-05-25..2013-07-05", r)
		}
		prognoza.NamjestiBez = append(prognoza.NamjestiBez, [2]int64{od.Unix() / 3600, do.Unix() / 3600})
	}
	if *doS != "" {
		do, err := time.Parse("2006-01-02", *doS)
		if err != nil {
			log.Fatal(err)
		}
		prognoza.NamjestiDo = do.Unix() / 3600
	}

	arhiva, err := sql.Open("sqlite", *arhivaPut+"?mode=ro")
	if err != nil {
		log.Fatal(err)
	}
	defer arhiva.Close()

	if *proba != "" {
		r, err := razaberi(*proba)
		if err != nil {
			log.Fatal(err)
		}
		namjesti(arhiva, r)
		return
	}

	var sve []prognoza.Pojas
	for _, tok := range Tokovi {
		fmt.Printf("\n— %s —\n", tok.Ime)
		for _, r := range tok.Racuni {
			sve = append(sve, namjesti(arhiva, r)...)
		}
	}

	if *probno {
		fmt.Printf("\nproba — ništa nije spremljeno; %d pojasa bi ušlo\n", len(sve))
		return
	}
	baza, err := prognoza.Otvori(*bazaPut)
	if err != nil {
		log.Fatal(err)
	}
	defer baza.Close()
	if err := prognoza.Spremi(baza, sve, time.Now().Format(time.RFC3339)); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nspremljeno %d pojasa u %s\n", len(sve), *bazaPut)
}

// razaberi čita "letva = ulaz + ulaz".
func razaberi(zapis string) (Racun, error) {
	strane := strings.SplitN(zapis, "=", 2)
	if len(strane) != 2 {
		return Racun{}, fmt.Errorf(`proba mora izgledati kao "letva = ulaz + ulaz"`)
	}
	r := Racun{Letva: strings.TrimSpace(strane[0])}
	for _, u := range strings.Split(strane[1], "+") {
		if u = strings.TrimSpace(u); u != "" {
			r.Ulazi = append(r.Ulazi, u)
		}
	}
	if len(r.Ulazi) == 0 {
		return Racun{}, fmt.Errorf("%s nema nijedan ulaz", r.Letva)
	}
	return r, nil
}

// rastavi vadi letvu i veličinu iz zapisa "letva" ili "letva:velicina". Druga
// se navodi kad se postavka isprobava bez diranja koda:
// "vrbovka:vodostaj = terezino-polje:protok".
func rastavi(zapis string) (string, string, error) {
	letva, velicina, ima := strings.Cut(zapis, ":")
	if ima {
		return letva, velicina, nil
	}
	v, zna := Velicine[letva]
	if !zna {
		return "", "", fmt.Errorf("za %s se ne zna u čemu se vodi; navedi %s:vodostaj ili %s:protok",
			letva, letva, letva)
	}
	return letva, v, nil
}

func namjesti(arhiva *sql.DB, r Racun) []prognoza.Pojas {
	letva, vel, err := rastavi(r.Letva)
	if err != nil {
		fmt.Println(err)
		return nil
	}
	izvori := make([]prognoza.Izvor, 0, len(r.Ulazi))
	imena := make([]string, 0, len(r.Ulazi))
	for _, u := range r.Ulazi {
		l, v, err := rastavi(u)
		if err != nil {
			fmt.Println(err)
			return nil
		}
		izvori = append(izvori, prognoza.Izvor{Letva: l, Velicina: v})
		imena = append(imena, l+" ("+jedinica(v)+")")
	}
	zaglavlje := letva + " ← " + strings.Join(imena, " + ")

	pojasi, err := prognoza.NamjestiLetvu(arhiva, letva, vel, izvori)
	if err != nil {
		fmt.Printf("%-56s %v\n", zaglavlje, err)
		return nil
	}
	fmt.Printf("%-56s %s\n", zaglavlje, vel)
	ispisi(pojasi, letva)
	// Rezerve: isti cilj, drugi ulazi, zasebna inačica. Neuspjeh rezerve ne
	// ruši glavni račun — samo se javi.
	for i, rez := range Rezerve[letva] {
		rezervni, err := namjestiInacicu(arhiva, letva, vel, rez, i+1)
		if err != nil {
			fmt.Printf("   rezerva %d (%s): %v\n", i+1, strings.Join(rez, " + "), err)
			continue
		}
		fmt.Printf("   rezerva %d:\n", i+1)
		ispisi(rezervni, letva)
		pojasi = append(pojasi, rezervni...)
	}
	return pojasi
}

// namjestiInacicu namješta jednu rezervnu inačicu računa.
func namjestiInacicu(arhiva *sql.DB, letva, vel string, ulazi []string, inacica int) ([]prognoza.Pojas, error) {
	izvori := make([]prognoza.Izvor, 0, len(ulazi))
	for _, u := range ulazi {
		l, v, err := rastavi(u)
		if err != nil {
			return nil, err
		}
		izvori = append(izvori, prognoza.Izvor{Letva: l, Velicina: v})
	}
	pojasi, err := prognoza.NamjestiLetvu(arhiva, letva, vel, izvori)
	if err != nil {
		return nil, err
	}
	for i := range pojasi {
		pojasi[i].Inacica = inacica
	}
	return pojasi, nil
}

// ispisi označi nepovezane pojase letvi koje slijede ulaz samo pri dovoljnoj
// vodi i ispiše pojase.
func ispisi(pojasi []prognoza.Pojas, letva string) {
	if SamoPovezane[letva] {
		for i := range pojasi {
			pojasi[i].Nepovezan = pojasi[i].R < NajmanjeSlaganje
		}
	}
	for _, p := range pojasi {
		// Granice pojasa mjere se u glavnom ulazu, pa nose njegovu jedinicu,
		// a rasap nosi jedinicu cilja.
		oznaka := ""
		if p.Nepovezan {
			oznaka = "  nepovezan — ne prognozira se"
		}
		fmt.Printf("   %8.0f – %8.0f %-4s  r %.3f   rasap %7.1f %-4s  (%d sati)%s\n",
			p.Od, p.Do, jedinica(p.Ulazi[0].Velicina), p.R, p.Rasap, jedinica(p.Velicina), p.Sati, oznaka)
		for _, u := range p.Ulazi {
			fmt.Printf("        %-16s -%2d h   prozor %2d h   nagib %6.2f %s\n",
				u.Letva, u.PomakH, u.Sirina, u.Nagib, poJedinici(p.Velicina, u.Velicina))
		}
	}
}

func jedinica(velicina string) string {
	if velicina == "protok" {
		return "m³/s"
	}
	return "cm"
}

func poJedinici(cilj, ulaz string) string {
	if cilj == ulaz {
		return ""
	}
	return jedinica(cilj) + "/" + jedinica(ulaz)
}
