package web

import (
	"fmt"
	"math"
	"net/http"
	"strings"

	"gocop/internal/models"
	"gocop/internal/prognoza"
)

// Opis metode prognoze. Isti tekst stoji na stranici „O prognozi” i na
// zasebnom listu izvoza u Excel, da se uz svaku izdanu tablicu zna odakle su
// brojevi i kako su izvedeni. Brojke koje su dio računa — kašnjenja, prozori,
// pojasi, poluvrijeme ispravka, broj analogija, dani dnevnog modela — čitaju
// se iz paketa prognoza, a ulazi i raspon svake postaje iz baze prognoza, pa
// opis ne može zaostati za računom.

// OdlomakMetode je jedan odlomak opisa: tekst, formula (zasebno, uvučeno),
// tablica brojki ili popis stavki. Brojke idu u tablicu, ne u rečenicu.
type OdlomakMetode struct {
	Tekst   string
	Formula bool
	TeX     string // matematički zapis za KaTeX; Tekst ostaje za Excel i rezervni prikaz
	Tablica *TablicaMetode
	Popis   []string
}

// TablicaMetode je mala tablica brojki uz opis.
type TablicaMetode struct {
	Naslov string
	Stupci []string
	Redci  [][]string
}

// OdjeljakMetode je naslov s odlomcima. Detalji su odjeljci za one koji
// provjeravaju račun; na stranici stoje sklopljeni ispod glavnog dijela.
type OdjeljakMetode struct {
	Naslov  string
	Odlomci []OdlomakMetode
	Detalji bool
}

func odjeljakM(naslov string, odlomci ...OdlomakMetode) OdjeljakMetode {
	return OdjeljakMetode{Naslov: naslov, Odlomci: odlomci}
}

func detaljiM(naslov string, odlomci ...OdlomakMetode) OdjeljakMetode {
	return OdjeljakMetode{Naslov: naslov, Odlomci: odlomci, Detalji: true}
}

// LetvaMetode je redak tablice postaja: iz čega se postaja računa i koliko
// se prognozi na njoj vjeruje.
type LetvaMetode struct {
	Naziv    string
	Voda     string
	Racuna   string   // vodostaj ili protok
	Satni    []string // ulazi satnog lanca s kašnjenjem i prozorom
	Slaganje string   // koeficijent korelacije lanca s mjerenjima
	Raspon   string   // polovina raspona na 24, 48 i 72 h
	Dnevni   []string // ulazi dnevnog modela
	DnevniOd string   // od kojeg dana vrijednost daje dnevni model
}

// PrognozeMetodaData je stranica „O prognozi”.
type PrognozeMetodaData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner

	Izdaje   string
	Izdano   string
	Odjeljci []OdjeljakMetode
	Letve    []LetvaMetode
	RasponDo []int // dosezi u stupcu raspona
	BezLetvi string

	Suradnja     string          // tko model radi i proučava
	SuradnjaVeza string          // adresa profila profesora
	Izvozi       []IzvozDatoteka // podaci za ponavljanje računa
	Valovi       *ProvjeraValova // provjera na poplavnim valovima, iz sažetka u mapi podataka
	Lanac        *SlikaLanca     // crtež lanca postaja, iz namještenih pojasa
}

// Glavni su odjeljci za dežurnog, na vrhu stranice.
func (m PrognozeMetodaData) Glavni() []OdjeljakMetode { return odjeljciGdje(m.Odjeljci, false) }

// Detaljni su odjeljci s formulama, provjerama i poviješću.
func (m PrognozeMetodaData) Detaljni() []OdjeljakMetode { return odjeljciGdje(m.Odjeljci, true) }

func odjeljciGdje(sve []OdjeljakMetode, detalji bool) []OdjeljakMetode {
	var out []OdjeljakMetode
	for _, o := range sve {
		if o.Detalji == detalji {
			out = append(out, o)
		}
	}
	return out
}

// DoseziRaspona su dosezi za koje tablica postaja navodi raspon.
var DoseziRaspona = []int{24, 48, 72}

func tekstM(s string) OdlomakMetode { return OdlomakMetode{Tekst: s} }
func tablicaM(naslov string, stupci []string, redci ...[]string) OdlomakMetode {
	return OdlomakMetode{Tablica: &TablicaMetode{Naslov: naslov, Stupci: stupci, Redci: redci}}
}
func popisM(stavke ...string) OdlomakMetode { return OdlomakMetode{Popis: stavke} }
func formulaM(tekst, tex string) OdlomakMetode {
	return OdlomakMetode{Tekst: tekst, Formula: true, TeX: tex}
}

// texBroj decimalni zarez štiti vitičastim zagradama da ga TeX ne tretira
// kao interpunkciju i ne doda razmak iza njega.
func texBroj(s string) string { return strings.ReplaceAll(s, ",", "{,}") }

// OpisMetode je opis računa, od podataka do raspona. udio je postotak
// slučajeva koji ostaju u rasponu, izdaje centar koji prognozu izdaje. Na
// vrhu je ono što dežurni treba za čitanje prognoze; formule, provjere i
// povijest su u odjeljcima s oznakom Detalji, koji se na stranici otvaraju
// klikom.
func OpisMetode(udio int, izdaje string) []OdjeljakMetode {
	if izdaje == "" {
		izdaje = "centra obrane od poplava"
	}
	pojasi := make([]string, 0, len(prognoza.Pojasi)+1)
	for _, p := range prognoza.Pojasi {
		pojasi = append(pojasi, brojHRf(p[0]*100, 0))
	}
	pojasi = append(pojasi, brojHRf(prognoza.Pojasi[len(prognoza.Pojasi)-1][1]*100, 0))
	sirine := make([]string, len(prognoza.Sirine))
	for i, s := range prognoza.Sirine {
		sirine[i] = tekstBroja(s)
	}
	poluvrijeme := brojHRf(prognoza.PoluvijekIspravka, 0)
	drava := daniDnevnogDrave()

	dani := []string{"", "1. dan", "2. dan", "3. dan", "4. dan", "5. dan", "6. dan"}
	return []OdjeljakMetode{
		odjeljakM("Kako čitati prognozu",
			tekstM("Uz svaku postaju stoje vrijednosti za +6 i +12 sati i za šest dana u 7 h. Vodostaj je u "+
				"centimetrima na nuli letve, a gdje postaja ima krivulju protoka, ispod je i protok u m³/s. "+
				"Vrijednost je najvjerojatnija, a ne najgora."),
			tekstM(fmt.Sprintf("Raspon uz vrijednost obuhvaća %d %% slučajeva: otprilike jednom u tri termina voda "+
				"izađe iz njega, podjednako iznad i ispod. Za odluku gledajte gornju granicu, a ne samo sredinu.", udio)),
			tekstM("Slovo uz vrijednost kaže iz čega je:"),
			popisM(
				"s — satni lanac;",
				"s u drugoj boji — satni lanac, jer dnevnog modela u tom izdanju nema;",
				"d — dnevni model;",
				"ds — satni lanac i dnevni model zajedno (Batina, Aljmaš, Vukovar i Ilok do 96. sata);",
				"p — iz susjednih postaja s dnevnim modelom, s rasponom procijenjenim iz susjeda; protok samo gdje postaja ima krivulju (Novo Virje, Szentborbás, Vrbovka, Moslavina i Drávaszabolcs na Dravi, a Siga, Petreš, Bogojevo, Dalj, Sotin i Mohovo na Dunavu)."),
			tekstM("Svjetlije pisana vrijednost nije u provjeri bila bolja od pretpostavke da se ništa neće promijeniti; "+
				"ondje je bolje vjerovati zadnjem mjerenju. Termin obojen bojom faze obrane doseže prag, a obrubljen "+
				"ga doseže tek gornjom granicom raspona."),
			tekstM("Sitno ispod stoje tuđe prognoze za isti termin: HU mađarska, RS srpska, AT austrijska. U naš račun "+
				"ne ulaze, nego stoje samo radi usporedbe."),
			tekstM("Kad pri izdavanju nešto ne radi, iznad kartica stoji okvir „Upozorenja uz ovo izdanje”. Kaže što ne "+
				"radi i koje su vrijednosti zbog toga upitne. Sitnije stvari stoje sklopljene pod „Napomene”. Izdanje "+
				"starije od tri sata stranica javlja sama."),
			tekstM("Prognozi vrijedi vjerovati manje:"),
			popisM(
				"na Dravi od trećeg dana pri velikom valu, jer ga model podcjenjuje: dotok iz Slovenije ne znamo unaprijed; gledajte gornju granicu raspona;",
				"na mađarskom Dunavu (Komárom, Budimpešta) pri valu, jer naš lanac od Wildungsmauera val propušta prebrzo;",
				"na Varaždinu, gdje razinu sat po sat vodi HE Varaždin;",
				"pri vodi izvan svega što je arhiva vidjela, jer model tada produžuje zadnji pravac."),
		),
		odjeljakM("Ukratko o metodi",
			tekstM("Prognoza "+izdaje+" je statistička, a ne hidraulička: ne rješava jednadžbe tečenja, nego iz dugih "+
				"nizova mjerenja uči kako se val prenosi od postaje do postaje. Rade dva modela. Satni lanac iz "+
				"uzvodnih postaja računa nizvodne, sat po sat do 96 sati. Dnevni model uči na dnevnim vodostajima "+
				"od 1901., uz kišu po međuslivovima, i daje dane do šestog."),
			tekstM("Na Batini, Aljmašu, Vukovaru i Iloku do 96. sata rade oba modela zajedno, otprilike pola-pola, a "+
				"dalje dnevni model. Na Dravi dnevni model preuzima od dana koji je provjera odredila za svaku postaju "+
				"i za vodostaj u satu izdanja: pri visokoj vodi od "+drava+", pri niskoj i srednjoj od 1. ili 2. dana. "+
				"Novo Virje, Szentborbás, Vrbovka, Moslavina i Drávaszabolcs nemaju dnevni model, pa od 2. dana "+
				"slijede susjedne postaje koje ga imaju. Siga, Petreš, Bogojevo, Dalj, Sotin i Mohovo na Dunavu "+
				"isto nemaju dnevni model: do 96. sata uz svoj satni lanac uzimaju dnevni dio susjeda, istom "+
				"težinom kao susjedi, a dalje samo njega. Srpske letve s našom prognozom (Bezdan, Apatin, Bačka "+
				"Palanka) pokazuju isto što i letve s kojih je prenesena."),
			tekstM("Vrhovi lanca su naši. Dravu vodi naučeno ispuštanje hidroelektrana, Muru slovenska Gornja Radgona, "+
				"a Wildungsmauer na Dunavu drži zadnje mjerenje. Kišu koja tek pada daje prognoza Open-Meteo. Tuđe "+
				"prognoze u račun ne ulaze. Model ispuštanja ima režim obične i velike vode za sve tri elektrane "+
				"i uči ga iz podataka."),
		),
		odjeljakM("Raspon i vjerojatnost",
			tekstM(fmt.Sprintf("Raspon obuhvaća %d %% slučajeva: u %d %% stvarna vrijednost izlazi iz njega, "+
				"podjednako iznad i ispod. Raspon nije granica mogućeg. Za 95 %% slučajeva, uz normalnu raspodjelu, "+
				"trebao bi otprilike dvostruko širi. Mađarska hidrološka služba uz svoju prognozu navodi isti udio "+
				"(70 %%), pa su naš i njihov raspon izravno usporedivi.", udio, 100-udio)),
		),
		odjeljakM("Ograničenja",
			popisM(
				"Veliki dravski val od trećeg dana prognoza podcjenjuje: vrh Botova 3.–5. dan promaši 52, 76 i 90 cm (21 val 2012.–2024.), i kad je kiša nakon izdanja poznata točno. Uzrok je dotok Drave iz Slovenije, za koji prognoze nemamo, a ne rad elektrana. Pratite gornju granicu raspona.",
				"Rad hidroelektrana unaprijed se zna samo koliko ga model ispuštanja nauči. Pri velikoj vodi HEP akumulacije spušta i puni bez zapisanog pravila. Varaždin ispod HE Varaždin o tome ovisi cijeli.",
				"Na mađarskom Dunavu naš lanac u valu griješi više od mađarske prognoze; naše letve to do 96 sati gotovo ne osjete.",
				"Vrijednost izvan svega viđenoga u arhivi model procjenjuje produženjem zadnjeg pravca; pri rekordnoj vodi budite oprezni.",
				"Prognoza kiše iz Open-Meteo je jedina. Kad izostane, dnevni model radi bez kiše i to piše uz izdanje.",
				"Kad gore u lancu nešto ne radi, upozorenje kaže od kojeg je dosega satna prognoza upitna. To je najraniji mogući doseg; nevolja stigne i kasnije."),
		),
		odjeljakM("Kad prognoza ne radi — pričuvni izračun",
			tekstM("Gumb „Pričuvni izračun” na vrhu ove stranice daje Excel koji radi bez programa i bez mreže. Na list "+
				"„Unos” upisujete mađarske i srpske prognoze letvi nasuprot i uz naše; program ih pri preuzimanju "+
				"popuni zadnjim poznatim. List „Naše postaje” iz njih računa Batinu, Aljmaš, Dalj, Vukovar, Ilok, "+
				"Botovo, Terezino Polje, Donji Miholjac, Belišće i Osijek pravcima iz dnevnih vrijednosti zadnjih "+
				"deset godina. Točnost je koliko i tuđa prognoza; letve nasuprot prenose se gotovo bez gubitka."),
		),

		detaljiM("Izdavanje, ranija prognoza i rječnik",
			tekstM("Prognozu računa i izdaje jedan čvor, onaj kojemu je u Administraciji, na stranici Čvor, mreža i "+
				"sinkronizacija (Uloge ovog čvora), uključeno „Izdaje prognozu”. Svako izdanje, s kišom po "+
				"međuslivovima i tuđim prognozama koje je izdavač tada imao, ide razmjenom ostalim čvorovima. Uz "+
				"vrijeme izdanja tada piše na kojem je čvoru izdano. Upozorenja uz izdanje zapisuje čvor koji izdaje, "+
				"pa svi čvorovi pokazuju isto."),
			tekstM("Baza prognoza čuva svako izdanje. Na stranici Postavke prognoze, u odjeljku „Ranija prognoza”, "+
				"birate dan i sat, a izvoz u Excel tada daje to izdanje kakvo je bilo, s upozorenjima, dnevnom "+
				"prognozom, kišom i mjerenjima do sata izdanja. Koji dan daje satni, a koji dnevni model ide po "+
				"današnjem izboru u Postavkama prognoze. Kad u tom satu prognoza nije izdana, uzima se najbliže "+
				"ranije izdanje. Što uz najstarija izdanja nije zapisano, izvoz nabraja."),
			tekstM("Kartica postaje na vrhu lanca kaže odakle joj budućnost: iz modela ispuštanja elektrane ili iz "+
				"našeg dnevnog modela; inače vrh drži zadnje mjerenje."),
			popisM(
				"doseg — koliko sati ili dana unaprijed vrijedi vrijednost;",
				"postojanost — pretpostavka da će voda ostati kakva je sad; mjera s kojom se svaka prognoza uspoređuje;",
				"vrh lanca — postaja iznad koje nemamo ništa u računu; njezinu budućnost daje poseban izvor ili zadnje mjerenje;",
				"karika — postaja koja se računa iz uzvodnih;",
				"rezerva — drugi ulaz iste karike, kad glavni ne javi (npr. Belišće iz Drávaszabolcsa umjesto iz Donjeg Miholjca);",
				"režim — obična ili velika voda; model se za svaki uči zasebno;",
				"analogije — povijesni dani najsličniji današnjem, čije se stvarne promjene uzmu kao procjena;",
				"međusliv — dio sliva između dviju letvi, za koji se kiša zbraja."),
		),
		detaljiM("Ulazni podaci",
			tekstM("Satni vodostaji i protoci iz arhive goCOP-a, koja se puni s mjernih sustava Hrvatskih voda "+
				"(uključivo istjecanje i razine akumulacija HE Varaždin, Čakovec i Dubrava) i sa stranica "+
				"hidroloških službi susjednih država: Slovenije (ARSO), Mađarske (vizugy.hu, a kad kasni, "+
				"DanubeHIS), Slovačke (SHMÚ), Srbije (hidmet.gov.rs), Austrije (eHYD, viadonau) i Njemačke (GKD, "+
				"Pegelonline). Povijest stranih postaja dopunjena je iz ICPDR-ova sustava DanubeHIS (licenca CC "+
				"BY-NC-SA 4.0): ovjereni dnevni podaci od 1961. i satni od 2020., očišćeni od grešaka uređaja. "+
				"Očitanja rjeđa od satnih premošćuju se linearno, ali ne preko "+tekstBroja(prognoza.NajveciRazmak)+
				" sati; dulja rupa ostaje rupa."),
			tekstM("Letve jedna nasuprot drugoj, na dvije obale, čuvaju jedna drugu: Komárom i Komárno, Batina i "+
				"Bezdan, Goričan i Letenye, Terezino Polje i Barcs, Donji Miholjac i Drávaszabolcs, Ilok i Bačka "+
				"Palanka. Sat koji jednoj nedostaje uzima druga, pomaknuta za medijan razlike zadnja dva tjedna. "+
				"Takav sat ulazi samo u račun, a kartica letve kaže kad je kraj niza došao s druge obale."),
			tekstM("Tuđe prognoze preuzimaju se kako ih službe izdaju i sve se pamte, da se s našom uspoređuju i "+
				"unatrag. Srpske letve uz naše na Dunavu (Bezdan, Apatin, Bogojevo, Bačka Palanka) dobivaju našu "+
				"prognozu prenesenu pravcem iz dnevnih vrijednosti; srpska stoji ispod nje."),
			tekstM("Oborina: kvazi-kišomjeri registra slivova po međuslivovima između letvi (Drava A–G, Dunav H–J "+
				"od Komároma do Aljmaša, gornji Dunav K–O od Bavarske do Komároma, Mura B), po visinskim pojasima. "+
				"Povijest je reanaliza ERA5 (Open-Meteo) od 1990.; uživo zadnjih sedam dana daje analiza, a "+
				"sljedećih sedam prognoza Open-Meteo, preuzeta svaki sat. Pravi kišomjeri služe za usporedbu i u "+
				"prognozu ne ulaze."),
		),
		detaljiM("Satni lanac — građa i procjena",
			tekstM("Postaje su složene u lanac niz tok. Svaka postaja y ima glavni ulaz x₁, uzvodnu postaju na istoj "+
				"vodi, i po potrebi sporedne ulaze x₂ … xₘ (pritoke, istjecanje elektrane). Postaja se računa u "+
				"onoj veličini u kojoj je veza najčvršća, vodostaju ili protoku. Prognoza nizvodne postaje računa "+
				"se iz prognoza uzvodnih, pa se pogreška uzvodno nosi nizvodno i ulazi u izmjereni raspon. Svaki "+
				"ulaz uzima se zakašnjen i zaglađen kliznim prosjekom:"),
			formulaM("x̄ⱼ(t) = (1 / wⱼ) · Σᵢ xⱼ(t − Lⱼ − i),   i = 0 … wⱼ − 1",
				`\bar{x}_j(t)=\frac{1}{w_j}\sum_{i=0}^{w_j-1}x_j(t-L_j-i)`),
			tekstM("Lⱼ je vrijeme propagacije vala u satima, a wⱼ širina prozora kojim se ulaz zagladi: rijeka "+
				"kratke valove guši, pa se dnevni val hidroelektrane ne smije prenijeti nizvodno neprigušen. Veza "+
				"s glavnim ulazom je neprekinuta, po dijelovima linearna funkcija (linearni spline) s čvorovima "+
				"na percentilima "+strings.Join(pojasi, ", ")+" glavnog ulaza; sporedni ulazi ulaze linearno:"),
			formulaM("y(t) = Σₖ βₖ · φₖ(x̄₁(t)) + Σⱼ γⱼ · x̄ⱼ(t) + ε(t),   k = 0 … K,   j = 2 … m",
				`y(t)=\sum_{k=0}^{K}\beta_k\varphi_k\!\left(\bar{x}_1(t)\right)+\sum_{j=2}^{m}\gamma_j\bar{x}_j(t)+\varepsilon(t)`),
			tekstM("φₖ su „šatorske” bazne funkcije, pa se pravci susjednih pojasa vodnosti sastaju na granici i "+
				"val koji raste ne dobiva skok. Pojasi su gušći pri velikoj vodi, jer voda ondje izlazi u "+
				"inundaciju. Koeficijenti se procjenjuju metodom najmanjih kvadrata, zajednički za sve pojase."),
			tekstM(fmt.Sprintf("Kašnjenja i prozori biraju se tako da maksimiziraju R²: glavni ulaz L ∈ [0, %d] h, "+
				"sporedni L ∈ [0, %d] h, w ∈ {%s} h. Kašnjenje glavnog ulaza traži se zasebno u svakom pojasu "+
				"vodnosti, jer val pri velikoj vodi putuje drukčije (Batina → Aljmaš od 4 do 38 sati).",
				prognoza.NajveciPomak, prognoza.NajveciPomakPritoka, strings.Join(sirine, ", "))),
			tekstM("Donji Miholjac i Belišće računaju se u protoku (satni protok iz HIS-2000), Belišće izravno iz "+
				"Donjeg Miholjca, a kad on ne javi, iz Drávaszabolcsa. Vodostaj pri istom protoku danas je niži "+
				"nego u godinama na kojima je lanac naučen; protok to zaobilazi."),
		),
		detaljiM("Satni lanac — vrhovi lanca",
			tekstM("Za sate poslije zadnjeg mjerenja postaja na vrhu lanca drži zadnje izmjereno stanje "+
				"(postojanost), osim gdje ima bolji izvor. Svaki takav izvor F ulazi kao promjena od zadnjeg "+
				"mjerenja, pa se vrh nikad ne odvoji od onoga što je izmjereno:"),
			formulaM("x(t) = x_mj(t₀) + [F(t) − F(t₀)]",
				`x(t)=x_{\mathrm{mj}}(t_0)+\left[F(t)-F(t_0)\right]`),
			tekstM("Drava: model ispuštanja elektrane. Istjecanje HE Dubrava (vrh lanca Botova), HE Čakovec i HE "+
				"Varaždin predviđa regresija po dosegu 1–96 h i po režimu (obična ili velika voda, granica je 90. "+
				"percentil istjecanja) iz vlastitog istjecanja, dotoka uzvodnih elektrana, razine akumulacije te "+
				"sata i dana u tjednu. Gdje model u provjeri ne pobijedi postojanost, vrh drži zadnje mjerenje. Pri "+
				"običnoj vodi model drži dnevni srednjak ispusta na dotoku. Režim obične i velike vode ima za sve "+
				"tri elektrane i uči ga iz podataka; kad u zadnje dvije godine ima premalo sati velike vode, "+
				"ocjena se uzima na zadnjih sto sati velike vode."),
			tablicaM("Što model ispuštanja daje lancu: rasap u provjeri unatrag 2023.–2025., prije → poslije",
				[]string{"", "24 h", "48 h", "72 h", "96 h"},
				[]string{"Botovo (m³/s)", "85 → 71", "116 → 99", "136 → 119", "150 → 130"},
				[]string{"Terezino Polje (m³/s)", "47 → 42", "81 → 66", "107 → 93", "122 → 106"},
				[]string{"Donji Miholjac (cm)", "9 → 9", "25 → 23", "37 → 32", "47 → 42"},
				[]string{"Belišće (cm)", "6 → 6", "18 → 17", "29 → 25", "36 → 32"},
				[]string{"Varaždin (cm)", "27 → 21", "33 → 25", "35 → 26", "36 → 27"}),
			tekstM("Mura: vrh je slovenska Gornja Radgona (ARSO, protok svakih 10 minuta), iz koje se računaju "+
				"Mursko Središće i Goričan, a Goričan nosi Muru u Botovo. Kad Murskog Središća nema, Goričan se "+
				"računa izravno iz Gornje Radgone, a kad nema ni nje, drži zadnje mjerenje."),
			tekstM("Dunav: Komárom se računa iz Nagybajcsa i Wildungsmauera, a Wildungsmauer drži zadnje mjerenje. "+
				"Do 22. 10. 2026. Wildungsmauer je slijedio austrijsku, a Komárom mađarsku prognozu. Obje su "+
				"izbačene iz lanca: pogrešku satne prognoze naših letvi na 96 sati mijenjale su za 0–3 cm."),
		),
		detaljiM("Satni lanac — ispravak i raspon",
			tekstM("Razlika između modela i zadnjeg mjerenja postaje dijeli se na stalni i prolazni dio. Mjerenje "+
				"smije biti staro do "+tekstBroja(prognoza.ZaostatakIspravka)+" sati. Stalni dio s je medijan "+
				"pogreške modela u zadnja "+tekstBroja(prognoza.StalniIspravakSati)+" sata, ali samo kad je "+
				"pogreška u tom prozoru skladna i kad letva ne raste; inače je nula. Stalni dio ostaje cijelim "+
				"dosegom, a prolazni slabi s poluvremenom od "+poluvrijeme+" sati:"),
			formulaM("ŷ*(t₀ + τ) = ŷ(t₀ + τ) + s + (r₀ − s) · 2^(−τ / "+poluvrijeme+"),   r₀ = y_mj(t₀) − ŷ(t₀)",
				`\hat{y}^{*}(t_0+\tau)=\hat{y}(t_0+\tau)+s+(r_0-s)\,2^{-\tau/`+texBroj(poluvrijeme)+`},\qquad r_0=y_{\mathrm{mj}}(t_0)-\hat{y}(t_0)`),
			tekstM("Sustavna pogreška: prognoza je puštena unatrag kroz arhivu, izdanje svakih 12 sati, svako samo s "+
				"onim što je tada bilo izmjereno. Za svaku postaju i doseg τ izmjerena je srednja pogreška b(τ), "+
				"koja se od prognoze oduzima."),
			tekstM(fmt.Sprintf("Polovina širine raspona r(τ) je empirijski %d. percentil apsolutnog odstupanja "+
				"pogreške od njezine srednje vrijednosti, iz iste provjere unatrag:", udio)),
			formulaM(fmt.Sprintf("r(τ) = Q_%s( |eᵢ(τ) − b(τ)| ),   prognoza = ŷ*(t₀ + τ) − b(τ) ± r(τ)",
				brojHRf(float64(udio)/100, 2)),
				`r(\tau)=Q_{`+texBroj(brojHRf(float64(udio)/100, 2))+`}\!\left(\left|e_i(\tau)-b(\tau)\right|\right),\qquad \mathrm{prognoza}=\hat{y}^{*}(t_0+\tau)-b(\tau)\pm r(\tau)`),
			tekstM("Raspon se dakle ne izvodi iz pretpostavke o normalnoj raspodjeli, nego brojanjem. Ista provjera "+
				"daje i pogrešku postojanosti; termin na kojem prognoza nju ne pobjeđuje pisan je svjetlije."),
		),
		detaljiM("Dnevni model",
			tekstM("Uči se na dnevnim srednjacima vodostaja od 1901. Za ciljnu postaju T i njezine ulaze s (tablica "+
				"postaja) značajke dana t su razina cilja, promjene vodostaja i kiša:"),
			formulaM("z(t) = [ 1,  h_T(t),  { Δ¹h_s(t), Δ²h_s(t) } za s ∈ {T} ∪ ulazi,  kiša ]",
				`\mathbf z(t)=\left[1,\ h_T(t),\ \left\{\Delta^1h_s(t),\Delta^2h_s(t)\right\}_{s\in\{T\}\cup\mathrm{ulazi}},\ \mathrm{ki\check{s}a}\right]`),
			formulaM("Δ¹h(t) = h(t) − h(t−1),   Δ²h(t) = h(t−1) − h(t−3)",
				`\Delta^1h(t)=h(t)-h(t-1),\qquad \Delta^2h(t)=h(t-1)-h(t-3)`),
			tekstM(fmt.Sprintf("Uz razinu cilja ulaze samo promjene, jer ne ovise o nuli vodokaza. Cilj je promjena na "+
				"dosegu k = 1 … %d dana, svaki doseg sa svojim modelom:", prognoza.DnevniDosezi)),
			formulaM("Δₖ(t) = h_T(t + k) − h_T(t)",
				`\Delta_k(t)=h_T(t+k)-h_T(t)`),
			tekstM("Kiša: za svaki međusliv uzvodno od cilja zbroj kiše zadnjeg dana, zadnja tri i zadnjih sedam dana "+
				"te prognozirana kiša sljedeća dva, četiri i šest dana. Kad oborine nema, uzima se inačica bez nje."),
			tekstM("Procjena je spoj dviju metoda. (a) Linearna regresija s pragom: dani se dijele na dva režima po "+
				"75. percentilu razine, a koeficijenti se procjenjuju metodom najmanjih kvadrata. (b) Metoda "+
				"analognih situacija (k najbližih susjeda): standardizirane značajke, razina cilja s dvostrukom "+
				"težinom:"),
			formulaM("d²(t, u) = 2 · ((h_T(t) − h_T(u)) / σ_h)² + Σᵢ ((zᵢ(t) − zᵢ(u)) / σᵢ)²",
				`d^2(t,u)=2\left(\frac{h_T(t)-h_T(u)}{\sigma_h}\right)^2+\sum_i\left(\frac{z_i(t)-z_i(u)}{\sigma_i}\right)^2`),
			tekstM(fmt.Sprintf("Uzima se K = %d povijesnih dana najbližih današnjem. Prognoza je tri četvrtine "+
				"regresije i četvrtina analogija, a raspon rasipanje analogija pomnoženo s %s:",
				prognoza.DnevnihAnalogija, brojHRf(prognoza.DnevniRasponMnozitelj, 2))),
			formulaM("Δ̂ₖ = ¾ · Δₖ_reg + ¼ · Δₖ_kNN,   ĥ_T(t + k) = h_T(t) + Δ̂ₖ ± "+brojHRf(prognoza.DnevniRasponMnozitelj, 2)+" · sₖ",
				`\widehat{\Delta}_k=\tfrac{3}{4}\,\Delta_{k,\mathrm{reg}}+\tfrac{1}{4}\,\Delta_{k,\mathrm{kNN}},\qquad \hat{h}_T(t+k)=h_T(t)+\widehat{\Delta}_k\pm `+texBroj(brojHRf(prognoza.DnevniRasponMnozitelj, 2))+`\,s_k`),
			tekstM("Uživo je „dan” srednjak 24 sata koji završavaju u satu izdavanja, pa se dnevna prognoza obnavlja "+
				"svaki sat. Model se uči iz arhive jednom na dan, iza ponoći, i iznova kad se arhiva promijeni ili "+
				"se pokrene Pripremi model."),
			tablicaM("Što kiša donosi: pogreška preko svih dana u cm (2024.–2026., model naučen do 2023.): bez kiše / s kišom i pravim prognozama Open-Meteo / gornja granica",
				[]string{"", "3. dan", "4. dan", "5. dan", "6. dan"},
				[]string{"Mursko Središće", "21,9 / 17,0 / 18,3", "23,7 / 18,8 / 17,8", "25,2 / 19,6 / 19,3", "26,5 / 21,9 / 19,0"},
				[]string{"Botovo", "35,8 / 29,6 / 29,5", "39,6 / 31,2 / 32,1", "42,5 / 33,7 / 32,2", "44,2 / 37,0 / 33,8"},
				[]string{"Donji Miholjac", "30,8 / 28,1 / 26,8", "41,0 / 34,9 / 33,4", "47,9 / 37,2 / 37,5", "52,9 / 40,4 / 39,3"},
				[]string{"Osijek", "23,0 / 22,6 / 22,1", "34,3 / 31,6 / 30,2", "42,2 / 35,9 / 35,0", "48,3 / 39,3 / 38,8"}),
		),
		detaljiM("Koji model daje koji dan",
			tekstM("Na Batini, Aljmašu, Vukovaru i Iloku vrijednost do 96. sata nije ni satni lanac ni dnevni model, "+
				"nego oboje zajedno (ds): dnevni model je osnova, a satni lanac ugađanje. Težina satnog lanca je 1 u "+
				"satu izdanja, 0,56 od 6. do 72. sata, a do 96. sata pada na 0; dalje je samo dnevni model (d). Na "+
				"živim izdanjima od 26. 9. 2026. prosječna pogreška manja je za 0,3–0,5 cm, a na 76–87 valova po "+
				"postaji od 2012. pri visokoj vodi za 0,4–2,0 cm. U vrhu vala 3. dan Batina pokaže nekoliko "+
				"centimetara više nego uz prijelaz."),
			tekstM("Na Dravi je ugađanje bilo lošije, jer satni lanac od 2. dana ide previsoko zbog elektrana. Ondje "+
				"dnevni model preuzima od dana koji je provjera odredila (tablica niže). Između dana koje daju "+
				"različiti modeli crta postupno prelazi s jednoga na drugi, pa tablica, uzdužni profil, graf letve i "+
				"graf u Excelu pokazuju isto."),
			tablicaM("Na Dravi: od kojeg dana vrijednost daje dnevni model", []string{"", "niska i srednja voda", "visoka voda"},
				[]string{"Osijek", "2. dana", "5. dana"},
				[]string{"Belišće", "2. dana", "4. dana"},
				[]string{"Donji Miholjac", "1. dana", "3. dana"},
				[]string{"Terezino Polje", "1. dana", "3. dana"},
				[]string{"Botovo", "1. dana", "2. dana"}),
			tekstM("Kad dnevni model za izdanje nije računao, dani koje bi on dao, sam ili zajedno sa satnim lancem, "+
				"pokazuju satnu prognozu u 7 h s oznakom s u drugoj boji, a dani dalje od 96 sati ostaju prazni. "+
				"Dnevna starijeg izdanja ne podmeće se uz noviju satnu."),
			tekstM("Novo Virje, Szentborbás, Vrbovka, Moslavina i Drávaszabolcs nemaju dnevni model. Od 2. dana daju "+
				"ih uzvodna i nizvodna postaja s dnevnim modelom: promjena koju tablica pokazuje na njima dodaje se "+
				"vrijednosti postaje, razmjerno udaljenosti po rijeci. Na izdanjima 2024.–2025. to je bliže "+
				"izmjerenom nego da voda ostane kakva je. Rizik: kad dnevni model susjedima najavi porast od kiše "+
				"koji ne dođe, i ovdje je najava pogrešna. U Postavkama prognoze uprava to može isključiti."),
			tekstM("Siga, Petreš, Bogojevo, Dalj, Sotin i Mohovo leže između dunavskih postaja s ugađanjem i nemaju "+
				"dnevni model. Bez njega je uzdužni profil imao grbe: susjedi su rasli s kišom, a one nisu. Dnevni "+
				"dio zato posuđuju od susjeda (Batina i Aljmaš, Aljmaš i Vukovar, Vukovar i Ilok), razmjerno "+
				"udaljenosti po rijeci, i do 96. sata miješaju ga sa svojim satnim lancem istom težinom kao "+
				"susjedi; dalje stoji samo dio iz susjeda. Siga i Petreš su iznad ušća Drave, a Aljmaš ispod, pa "+
				"njihov dio od Aljmaša nosi i malo Drave. Nije posebno izmjereno."),
			tekstM("Uz oznaku p raspon je raspon susjeda, utegnut po kilometru kao i vrijednost: procijenjen je, a "+
				"ne izmjeren. Protok daje krivulja postaje (Novo Virje, Dalj); gdje je nema, nema ni protoka."),
			tekstM("Koji model daje dane bira se u Postavkama prognoze, jednom za tablicu, grafove i Excel: "+
				"kombinacija (zadano), samo satni lanac ili samo dnevni model. Uz samo jedan model ugađanja nema."),
		),
		detaljiM("Vodostaj i protok",
			tekstM("Postaja se računa u jednoj veličini, a drugu daje važeća krivulja protoka (Q–H) postaje. Kroz "+
				"krivulju se preračunaju i granice raspona, pa raspon može postati nesimetričan; tada se piše "+
				"granicama umjesto ±. Gdje krivulje nema, nema ni druge veličine."),
		),
		detaljiM("Uzdužni profil",
			tekstM("Profil ne crta kote nego koliko je voda iznad ili ispod vode zadnjih 30 dana na svakoj letvi: "+
				"medijana satnih vodostaja mjeseca prije današnjeg jutra. Mirna voda je tako ravna crta, a val se "+
				"vidi kao brijeg koji putuje nizvodno. Jutarnja crta (7 h) stoji cijeli dan, a crte prognoze "+
				"pokazuju kamo val ide; klizač pomiče vrijeme i uz točku piše promjenu od jutra."),
			tekstM("Nula se računa iz istih dana na svim letvama jednog toka. S profila ispada letva čije mjerilo "+
				"danima stoji dok se susjedna pomakne, i letva koja nakon prekida mjerenja nastavi bitno niže ili "+
				"više; ispod crteža piše koje su izostavljene i zašto. Uobičajena voda zadnjih deset godina "+
				"(medijan, crtkano) i srednji vodostaj (SV, točkasto) crtaju se kad su blizu vode. Pad vodnog lica "+
				"računa se u Trstu; mađarske kote preračunate su dodavanjem 0,675 m."),
			tekstM("Profil Dunava kreće od Batine. Mađarske letve imaju samo naš satni lanac do 96 sati, bez "+
				"dnevnog modela, pa bi na profilu Batina rasla sama; u tablici prognoza i dalje stoje."),
		),
		detaljiM("Provjera i usporedbe",
			tekstM("Svaki dio modela provjeren je na podacima koje pri učenju nije vidio. Dnevni model s kišom učen je "+
				"do 2023. i mjeren na 2024.–2026. s tada izdanim prognozama kiše. Satni lanac provjeren je na "+
				"poplavnim valovima od 2012.: svaki val prognozira model namješten bez njega. Sve se uspoređuje s "+
				"postojanošću, s mađarskom prognozom, s uredskom prognozom i s modelom MIKE. Tuđa prognoza je pritom "+
				"samo mjerilo koliko vlastiti lanac još treba popraviti."),
			tablicaM("Drava prema mađarskoj prognozi: srednja pogreška 1.–6. dana u cm, oni / mi (28 izdanja rujan 2024. – rujan 2026., dnevni model s kišom naučen do rujna 2024.)",
				dani,
				[]string{"Botovo", "24 / 16", "37 / 26", "41 / 27", "51 / 28", "52 / 37", "58 / 38"},
				[]string{"Terezino Polje", "14 / 10", "28 / 24", "40 / 30", "48 / 29", "52 / 32", "60 / 39"},
				[]string{"Donji Miholjac", "7 / 5", "20 / 16", "36 / 29", "47 / 34", "55 / 33", "52 / 38"},
				[]string{"Belišće", "6 / 5", "10 / 13", "20 / 24", "29 / 30", "42 / 29", "44 / 30"},
				[]string{"Osijek", "12 / 14", "23 / 28", "36 / 41", "46 / 53", "58 / 60", "62 / 67"}),
			tekstM("Na Dravi smo bolji ili jednaki na svim dosezima, a Osijek je zbog uspora Dunava nešto lošiji. Na "+
				"mađarskom Dunavu njihov je model u valu bolji, ali naše letve to do 96 sati gotovo ne osjete."),
		),
		detaljiM("Što je novo",
			popisM(
				"22. 10. 2026. — Siga, Petreš, Bogojevo, Dalj, Sotin i Mohovo dnevni dio posuđuju od susjeda (oznaka p), pa uzdužni profil više nema grbe; Bezdan, Apatin i Bačka Palanka pokazuju isto što i letve s kojih se prenose; uz p raspon procijenjen iz susjeda i protok iz krivulje; profil Dunava kreće od Batine.",
				"22. 10. 2026. — na Batini, Aljmašu, Vukovaru i Iloku do 96. sata satni lanac i dnevni model zajedno (oznaka ds); uz svaku vrijednost slovo kaže iz čega je; graf i tablica pokazuju isto, a šesti dan ima vrijednost i kad je izdanje bilo danju; u Postavkama prognoze jedan je izbor modela umjesto tri.",
				"22. 10. 2026. — prognoza ne ovisi ni o jednoj tuđoj prognozi; Novo Virje, Szentborbás, Vrbovka, Moslavina i Drávaszabolcs od 2. dana slijede susjede (oznaka p); pri visokoj vodi dnevni model na Dravi preuzima ranije (Botovo od 2., Donji Miholjac od 3., Belišće od 4. dana).",
				"10. 10. 2026. — ranija prognoza: izvoz u Excel za odabrani dan i sat.",
				"9. 10. 2026. — upozorenja uz izdanje na stranici, u izvozu, sažetku i grafu postaje.",
				"2. 10. 2026. — prognozu izdaje jedan čvor, a izdanje ide razmjenom ostalim čvorovima.",
				"Ranije — satni lanac od 2012., dnevni model od 1901. s kišom po međuslivovima, model ispuštanja elektrana, uzdužni profil, pričuvni izračun."),
		),
	}
}

// dnevniOd je stupac „Dnevni od” tablice postaja: dan prijelaza pri visokoj
// vodi, a na letvama s ugađanjem „ds do 96. sata”, jer ondje prijelaza nema.
func dnevniOd(kod string) string {
	if prognoza.Ugadja(kod) {
		return "ds do 96. sata"
	}
	if d, ima := prognoza.DnevnaOdDana[kod]; ima {
		return tekstBroja(d) + ". dana"
	}
	return ""
}

// daniDnevnogDrave opisuje od kojeg dana dnevni model na Dravi daje
// vrijednost pri visokoj vodi; na Dunavu je ugađanje, bez dana prijelaza.
func daniDnevnogDrave() string {
	return daniDnevnogZa("botovo", "terezino-polje", "donji-miholjac", "belisce", "osijek")
}

// daniDnevnogZa opisuje od kojeg dana dnevni model daje vrijednost letvama.
func daniDnevnogZa(letve ...string) string {
	naj, vrh := 99, 0
	for _, l := range letve {
		if d, ima := prognoza.DnevnaOdDana[l]; ima {
			naj, vrh = min(naj, d), max(vrh, d)
		}
	}
	switch {
	case vrh == 0:
		return "—"
	case naj == vrh:
		return tekstBroja(naj) + ". dana"
	}
	return tekstBroja(naj) + ". do " + tekstBroja(vrh) + ". dana, već prema postaji"
}

// letveMetode slaže tablicu postaja: ulazi lanca iz namještenih pojasa,
// raspon iz izmjerenih promašaja, ulazi dnevnog modela iz paketa prognoza.
// Redom je kao na pregledu, od uzvodne prema nizvodnoj po vodama.
func letveMetode(tablice []TablicaPrognoza, postaje map[string]models.Station,
	pojasi map[string][]prognoza.Pojas, promasaji map[prognoza.Izvor]map[int]prognoza.Promasaj) []LetvaMetode {
	ime := func(kod string) string {
		if st, ima := postaje[kod]; ima && st.Name != "" {
			return st.Name
		}
		return kod
	}
	dnevni := map[string][]string{}
	for _, c := range prognoza.DnevniCiljevi {
		dnevni[c.Letva] = c.Ulazi
	}
	var out []LetvaMetode
	for _, t := range tablice {
		for _, x := range t.Letve {
			ps := pojasi[x.Kod]
			ulazi, imaDnevni := dnevni[x.Kod]
			if len(ps) == 0 && !imaDnevni {
				continue
			}
			l := LetvaMetode{Naziv: x.Naziv, Voda: x.Voda}
			if len(ps) > 0 {
				l.Racuna = ps[0].Velicina
				l.Satni = ulaziLanca(ps, ime)
				najR, vrhR := math.Inf(1), math.Inf(-1)
				for _, p := range ps {
					najR, vrhR = math.Min(najR, p.R), math.Max(vrhR, p.R)
				}
				l.Slaganje = brojHRf(najR, 3)
				if vrhR-najR >= 0.0005 {
					l.Slaganje += "–" + brojHRf(vrhR, 3)
				}
				jed := "cm"
				if l.Racuna == "protok" {
					jed = "m³/s"
				}
				var r []string
				izmjeren := false
				izmjereni := promasaji[prognoza.Izvor{Letva: x.Kod, Velicina: l.Racuna}]
				for _, d := range DoseziRaspona {
					if p, ima := izmjereni[d]; ima {
						r = append(r, "±"+brojHRf(math.Round(p.Rasap), 0))
						izmjeren = true
					} else {
						r = append(r, "—")
					}
				}
				if izmjeren {
					l.Raspon = strings.Join(r, " / ") + " " + jed
				}
			}
			if imaDnevni {
				for _, u := range ulazi {
					l.Dnevni = append(l.Dnevni, ime(u))
				}
				l.DnevniOd = dnevniOd(x.Kod)
			}
			out = append(out, l)
		}
	}
	return out
}

// ulaziLanca opisuje ulaze satnog lanca: kašnjenje glavnog po pojasima, od
// najkraćeg do najduljeg, i prozor glačanja.
func ulaziLanca(ps []prognoza.Pojas, ime func(string) string) []string {
	var out []string
	for j, u := range ps[0].Ulazi {
		najL, vrhL := u.PomakH, u.PomakH
		for _, p := range ps {
			if j < len(p.Ulazi) {
				najL, vrhL = min(najL, p.Ulazi[j].PomakH), max(vrhL, p.Ulazi[j].PomakH)
			}
		}
		kas := tekstBroja(najL)
		if vrhL != najL {
			kas += "–" + tekstBroja(vrhL)
		}
		s := ime(u.Letva) + " · " + u.Velicina + " · " + kas + " h"
		if u.Sirina > 1 {
			s += " · prozor " + tekstBroja(u.Sirina) + " h"
		}
		out = append(out, s)
	}
	return out
}

// metoda skuplja sve što stranica i list „O prognozi” pokazuju.
func (h *PrognozeHandler) metoda(r *http.Request) PrognozeMetodaData {
	o := h.nacini(r.Context())
	data := h.podaci(r, o.PrognozaPrikaz)
	izdaje := data.Izdaje
	if izdaje == "" {
		izdaje = h.centar(data.CurrentUser)
	}
	m := PrognozeMetodaData{
		CurrentUser: data.CurrentUser, Permissions: data.Permissions,
		ActiveNav: "prognoze", ViewAsBanner: data.ViewAsBanner,
		Izdaje: izdaje, Izdano: data.Izdano,
		Odjeljci: OpisMetode(int(math.Round(prognoza.UdioURasponu*100)), izdaje),
		RasponDo: DoseziRaspona,
		Suradnja: suradnja, SuradnjaVeza: suradnjaVeza, Izvozi: h.izvozi(),
	}
	if h.podaciDir != nil {
		m.Valovi = provjeraValovaIz(h.podaciDir())
	}
	var c *CitacPrognoza
	if h.citac != nil {
		c = h.citac()
	}
	pojasi, promasaji := c.Namjesteno()
	if data.Nema || len(pojasi) == 0 {
		m.BezLetvi = "Tablica postaja čita se iz baze prognoza, a ona još nema namještenog lanca."
		return m
	}
	postaje := h.postaje(r.Context())
	m.Letve = letveMetode(data.Tablice, postaje, pojasi, promasaji)
	m.Lanac = slikaLanca(pojasi, postaje)
	return m
}

// ShowMetoda prikazuje stranicu „O prognozi”.
func (h *PrognozeHandler) ShowMetoda(w http.ResponseWriter, r *http.Request) {
	if h.metodaTmpl == nil {
		http.NotFound(w, r)
		return
	}
	if err := h.metodaTmpl.ExecuteTemplate(w, "prognoze_metoda.html", h.metoda(r)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
