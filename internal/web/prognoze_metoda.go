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
	uzvRast, uzvNajvise := brojHRf(prognoza.UzvodniRast, 0), brojHRf(prognoza.UzvodnoNajvise, 0)
	uzvNapomena := brojHRf(prognoza.UzvodnoNapomena, 0)

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
				"ds — satni lanac i dnevni model zajedno (Komárom, Budimpešta, Batina, Aljmaš, Vukovar i Ilok do 96. sata);",
				"p — iz susjednih postaja s dnevnim modelom, s rasponom procijenjenim iz susjeda; protok samo gdje postaja ima krivulju (Novo Virje, Szentborbás, Vrbovka, Moslavina i Drávaszabolcs na Dravi, a na Dunavu mađarske letve od Esztergoma do Mohácsa te Siga, Petreš, Bogojevo, Dalj, Sotin i Mohovo)."),
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
				"na mađarskom Dunavu (Komárom, Budimpešta) pri valu: Wildungsmauer drži zadnje mjerenje, a kiša iz prognoze val samo djelomično nadoknadi;",
				"na Varaždinu, gdje razinu sat po sat vodi HE Varaždin;",
				"pri vodi izvan svega što je arhiva vidjela, jer model tada produžuje zadnji pravac."),
		),
		odjeljakM("Ukratko o metodi",
			tekstM("Prognoza "+izdaje+" je statistička, a ne hidraulička: ne rješava jednadžbe tečenja, nego iz dugih "+
				"nizova mjerenja uči kako se val prenosi od postaje do postaje. Rade dva modela. Satni lanac iz "+
				"uzvodnih postaja računa nizvodne, sat po sat do 96 sati. Dnevni model uči na dnevnim vodostajima "+
				"od 1901., uz kišu po međuslivovima, i daje dane do šestog."),
			tekstM("Na Batini, Aljmašu, Vukovaru i Iloku do 96. sata rade oba modela zajedno, otprilike pola-pola, a "+
				"dalje dnevni model. Na Komáromu i Budimpešti dnevni model s kišom ulazi u sam satni lanac, pa kišu "+
				"nose i sve letve nizvodno. Na Dravi dnevni model preuzima od dana koji je provjera odredila za svaku postaju "+
				"i za vodostaj u satu izdanja: pri visokoj vodi od "+drava+", pri niskoj i srednjoj od 1. ili 2. dana. "+
				"Novo Virje, Szentborbás, Vrbovka, Moslavina i Drávaszabolcs nemaju dnevni model, pa od 2. dana "+
				"slijede susjedne postaje koje ga imaju. Mađarske letve od Esztergoma do Mohácsa te Siga, Petreš, "+
				"Bogojevo, Dalj, Sotin i Mohovo na Dunavu isto nemaju dnevni model: do 96. sata uz svoj satni "+
				"lanac uzimaju dnevni dio susjeda, istom "+
				"težinom kao susjedi, a dalje samo njega. Srpske letve s našom prognozom (Bezdan, Apatin, Bačka "+
				"Palanka) pokazuju isto što i letve s kojih je prenesena."),
			tekstM("Dnevna prognoza Batine, Aljmaša, Vukovara i Iloka ispravlja se i prema satnim mjerenjima "+
				"Wildungsmauera i Achleitena u Austriji. Val koji su oni izmjerili stiže do Batine za oko pet dana, a "+
				"dnevni model ga ne vidi, pa ispravak najviše mijenja 4.–6. dan. Ne spušta prognozu dok voda na "+
				"postaji raste više od "+uzvRast+" cm na dan i ne diže je više od "+uzvNajvise+" cm. Kad Achleiten "+
				"ne javlja, ispravak se računa samo prema Wildungsmaueru, a kad ne javlja ni Wildungsmauer, ispravka nema. "+
				"Oboje piše u napomenama uz izdanje, kao i kad ispravak digne prognozu za više od "+uzvNapomena+" cm. "+
				"Ispravak dobivaju i letve koje dnevni dio uzimaju od tih postaja: Siga, Petreš, Bogojevo, Dalj, Sotin, "+
				"Mohovo, Bezdan, Apatin i Bačka Palanka, a mađarske letve od Dunaföldvára do Mohácsa dio, to manji što su "+
				"bliže Budimpešti."),
			tekstM("Vrhovi lanca su naši. Dravu vodi naučeno ispuštanje hidroelektrana, Muru slovenska Gornja Radgona, "+
				"a Wildungsmauer na Dunavu drži zadnje mjerenje; kišu gornjeg Dunava, Morave i Slovačke lanac "+
				"dobiva na Komáromu i Budimpešti. Kišu koja tek pada daje prognoza Open-Meteo. Tuđe "+
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
				"U najvećim valovima ispravak prema Wildungsmaueru i Achleitenu ne pomaže: blizu vrha dnevna prognoza Batine 4.–6. dan ostaje preniska kao i bez njega, 50–55 cm u lipnju 2013. i rujnu 2024. Ranije u valu zna dići previsoko; kad digne više od "+uzvNapomena+" cm, to piše u napomeni uz izdanje.",
				"U maloj vodi ispravak dnevnu prognozu Dunava 4.–6. dan u prosjeku malo podigne, a ondje je ona i bez njega malo previsoka: srednja pogreška raste s oko +2 na +4 cm, 2022. sa +7 na +9 cm. U provjeri po godinama ukupna pogreška bila je i tada manja nego bez ispravka, ali prvi tjedni u maloj vodi mogu izgledati lošije.",
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
			tekstM("Uzvodni ispravak (Batina, Aljmaš, Vukovar, Ilok): promjena za doseg k pomiče se za naučenu "+
				"pogrešku modela, iz promjena vodostaja Wildungsmauera i Achleitena x za 24 i 48 sati do sata prije "+
				"izdanja (mjerenje smije kasniti do "+tekstBroja(prognoza.UzvodnoKasnjenje)+" sata; lažan skok veći od "+
				brojHRf(prognoza.UzvodniSkok, 0)+" cm u satu, na sat ili dva pa natrag, ne ulazi, a pravi kasni sat ili dva):"),
			formulaM("Δ̂ₖ* = Δ̂ₖ − (c₀ₖ + Σᵢ bᵢₖ · xᵢ)",
				`\widehat{\Delta}_k^{*}=\widehat{\Delta}_k-\left(c_{0k}+\sum_i b_{ik}\,x_i\right)`),
			tekstM("Koeficijenti su naučeni na pogreškama dnevnog modela 2012.–8/2026., i to samo na danima koje model "+
				"pri učenju nije vidio; ispravak se uči izvan programa. Ispravak ne prelazi granice viđene u učenju, "+
				"ne spušta prognozu dok cilj raste više od "+uzvRast+" cm u 24 sata i ne diže je više od "+uzvNajvise+
				" cm: bez toga je u valu iz rujna 2024. Batinu 6. dan spustio i do 90 cm dok je ona još rasla, jer val "+
				"u velikoj vodi putuje 7,5–9,7 dana umjesto pet. Provjereno izostavljanjem godine: pogreška Dunava "+
				"4.–6. dan manja je za 2,55 cm (s 20,7 cm), u svakoj od 15 godina. Kad Achleiten ne javlja, ispravak "+
				"ide samo iz Wildungsmauera, a bez njega prognoza je ista kao bez ispravka. Izdanje bilježi je li "+
				"dnevna prognoza računata s ispravkom, i kad ga je zaštita zadržala na nuli, pa se izdanja s njim i "+
				"bez njega mogu uspoređivati."),
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
			tekstM("Komárom i Budimpešta imaju ugađanje u samom lancu: njihova vrijednost unaprijed je dnevni model s "+
				"kišom i satni lanac zajedno, pa letve nizvodno računaju iz nje i kišu dobivaju kroz lanac. Komárom "+
				"dnevni model računa iz kiše nad gornjim Dunavom i Moravom, Budimpešta iz Komároma i kiše nad Váhom, "+
				"Hronom i Ipeľom. Težina satnog lanca na Komáromu je kao na Batini, a na Budimpešti 0,8 do 72. sata; "+
				"na obje do 96. sata pada na 0. Kiša kao ulaz u pojedinu vezu lanca nije pomogla, jer su susjedne "+
				"letve nekoliko sati jedna od druge, pa je voda već na uzvodnoj."),
			tablicaM("Kiša u lancu: srednja pogreška u cm, lanac bez nje / s njom (1.881 izdanje 2024.–2026., model naučen do 2023., prave prognoze kiše)",
				[]string{"", "72 h", "96 h", "96 h kad voda raste 30 cm i više"},
				[]string{"Komárom", "29,2 / 25,6", "36,3 / 29,5", "67 / 45"},
				[]string{"Mohács", "14,0 / 11,5", "19,5 / 17,0", "30,6 / 25,2"},
				[]string{"Batina", "12,6 / 11,5", "18,9 / 16,2", "28,5 / 23,3"}),
			tekstM("Kad kiša nakon suhih dana digne prognozu za 20 cm i više, Batina za 96 sati u prosjeku pokaže "+
				"14 cm previše, a lanac bez kiše je u istim izdanjima pokazivao 15 cm premalo. Isto rade i dnevni "+
				"modeli Batine i Aljmaša; gornju granicu raspona tada treba čitati s tim na umu."),
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
			tekstM("Mađarske letve od Esztergoma do Mohácsa te Siga, Petreš, Bogojevo, Dalj, Sotin i Mohovo leže između "+
				"dunavskih postaja s ugađanjem i nemaju dnevni model. Bez njega je uzdužni profil imao grbe: susjedi "+
				"su rasli s kišom, a one nisu. Dnevni dio zato posuđuju od susjeda (Komárom i Budimpešta, Budimpešta "+
				"i Batina, Batina i Aljmaš, Aljmaš i Vukovar, Vukovar i Ilok), razmjerno "+
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
			tekstM("Letve stoje redom niz tok, ali razmak nije u mjerilu, da gusto posađene letve dobiju mjesta za "+
				"natpis. Iznad letve piše jutarnji vodostaj, a boja točke je faza obrane. Prije 7 h stoji zadnje "+
				"očitanje od 4 h. Peti i šesti dan daje dnevni model; letva bez njega uzima te dane od susjeda, a "+
				"gdje nema ni toga, crta prestaje. Pragovi obrane ucrtaju se kad im se voda približi, a klik na "+
				"doseg u legendi pokaže njegov raspon."),
			tekstM("Profil Dunava kreće od Komároma. Nagybajcs i Wildungsmauer imaju samo satni lanac, koji vrh drži "+
				"na zadnjem mjerenju, pa bi na profilu Komárom rastao sam; u tablici prognoza i dalje stoje."),
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
		detaljiM("Znanstveni radovi i literatura",
			tekstM("Radove smo pregledali 25. 10. 2026. uz vlastita mjerenja na Dunavu, Dravi i Muri te na sličnim "+
				"rijekama u svijetu (Rajna, Tisa, Visla, Mississippi, Po). Poredani su po važnosti za našu prognozu. Uz "+
				"svaki piše što potvrđuje u našim brojkama i što iz njega možemo primijeniti. Sustavi drugih službi "+
				"(EFAS, mađarska i austrijska prognoza) za nas su samo mjerilo, nikad ulaz u račun."),
			tekstM("Što potvrđuju i što možemo primijeniti, po važnosti:"),
			popisM(
				"1. Wong i Laurenson (1983.), Water Resources Research, doi.org/10.1029/WR019i003p00701; Eder i dr. (2024.), Natural Hazards, doi.org/10.1007/s11069-024-06788-7; Komma i dr. (2026.), HESS, doi.org/10.5194/hess-30-4075-2026. Potvrđuju: val je najsporiji kad voda izlazi u inundaciju, a dunavske inundacije usporavaju ga do 41,5 sati; zato val od Wildungsmauera do Batine pri velikoj vodi putuje 7,5–9,7 dana umjesto oko 5. U rujnu 2024. Wildungsmauer su digle donjoaustrijske pritoke, a Achleiten je bio tek dvogodišnja voda. Primijeniti: kašnjenje ispravka prema vodostaju i podrijetlo vala (raste li i Achleiten ili samo Wildungsmauer).",
				"2. Klemeš (1986.), Hydrological Sciences Journal, doi.org/10.1080/02626668609491024; Shen, Tolson i Mai (2022.), Water Resources Research, doi.org/10.1029/2021WR031523; Roberts i dr. (2017.), Ecography, doi.org/10.1111/ecog.02881. Potvrđuju način na koji provjeravamo: učenje na jednom razdoblju, provjera na drugom. Primijeniti: provjeru po godinama s razmakom oko izostavljene godine, posebnu provjeru na godinama velikih valova i na sušnim godinama, a konačne postavke učiti na svim godinama.",
				"3. Gupta i dr. (2009.), Journal of Hydrology, doi.org/10.1016/j.jhydrol.2009.08.003. Potvrđuje: model učen na kvadratnu grešku sustavno sužava kolebanje, pa 6. dan predvidi tek oko pola porasta, kako i mjerimo. Primijeniti: veći udio analogija kad voda brzo raste.",
				"4. Weerts, Winsemius i Verkade (2011.), HESS, doi.org/10.5194/hess-15-255-2011; López López i dr. (2014.), HESS, doi.org/10.5194/hess-18-3411-2014; Hemri i Klein (2017.), Water Resources Research, doi.org/10.1002/2017WR020684 (Rajna); Biondi i Todini (2018.), Water Resources Research, doi.org/10.1029/2017WR022432 (Po). Potvrđuju da raspon iz samih analogija ispada preuzak. Primijeniti: raspon iz povijesnih grešaka za svaku letvu i dan, ovisno o vodostaju i porastu, posebno za nisku i visoku vodu.",
				"5. Gneiting (2011.), International Journal of Forecasting, doi.org/10.1016/j.ijforecast.2009.12.015. Daje pošten oblik dogovora „bolje u plus nego u minus”: kad je podcjena skuplja, objavljuje se gornji dio raspona (npr. 60–67 %), a ne ručni pomak. Pomak je tada velik samo gdje je nesigurnost velika, u valovima, a pri mirnoj vodi ostaje mali.",
				"6. Brutsaert i Nieber (1977.), Water Resources Research, doi.org/10.1029/WR013i003p00637; Kirchner (2009.), Water Resources Research, doi.org/10.1029/2008WR006912; Stoelzle i dr. (2013.), HESS, doi.org/10.5194/hess-17-817-2013. Potvrđuju: brzina opadanja ovisi o protoku nelinearno, a linearni model zato pri niskoj vodi spušta presporo; to je naš plus od lipnja do studenog. Primijeniti: poseban račun opadanja za dane bez kiše, provjeren po godinama.",
				"7. Berthet i dr. (2009.), HESS, doi.org/10.5194/hess-13-819-2009; Nester i dr. (2012.), Water Resources Research, doi.org/10.1029/2011WR011649; Welles i Sorooshian (2009.), Journal of Hydrometeorology, doi.org/10.1175/2008JHM1022.1. Potvrđuju: izmjereni vodostaj već nosi stanje sliva, pa vlažnost tla na velikoj rijeci malo dodaje, a prognoza kiše vrijedi tek 4.–6. dan i pri visokoj vodi. To je i naše mjerenje.",
				"8. Tadić, Bonacci i Dadić (2016.), Environmental Earth Sciences, doi.org/10.1007/s12665-016-6052-0. Potvrđuje: kod Osijeka velike vode određuje uspor Dunava, pa Osijek mora gledati Dunav, a ne samo uzvodnu Dravu.",
				"9. Bonacci, Tadić i Trninić (1992.), Regulated Rivers, doi.org/10.1002/rrr.3450070405. Niska voda Drave nakon izgradnje akumulacija ima drukčiji režim. Primijeniti: postavke za nisku vodu Drave učiti na razdoblju nakon 1989.",
				"10. Greimel i dr. (2016.), Hydrological Processes, doi.org/10.1002/hyp.10773. Primijeniti: na Botovu i Terezinom Polju prepoznati kratka kolebanja zbog rada elektrana i odvojiti ih od vala prije učenja satnog lanca.",
				"11. Vizi i dr. (2023.), Environmental Sciences Europe, doi.org/10.1186/s12302-023-00796-3 (Tisa). Najbliži objavljeni slučaj našem: nizinska rijeka, prognoza do 7 dana; i ondje model podcjenjuje vodostaj. Primijeniti: izvještaj točnosti po razredima vodostaja.",
				"12. Adams i Dymond (2019.), Journal of Hydrometeorology, doi.org/10.1175/JHM-D-18-0202.1 (Ohio). Pri poplavi prognoza kiše dalje od pola dana brzo gubi vrijednost. Primijeniti: pri velikoj vodi manja težina prognozirane kiše za 3.–6. dan."),
			tekstM("Što ne primjenjujemo, jer se kod nas pokazalo drukčije ili ne odgovara našem modelu:"),
			popisM(
				"Vlažnost tla na Dunavu — Komma, Blöschl i Reszler (2008.), Journal of Hydrology, doi.org/10.1016/j.jhydrol.2008.05.020; Brocca i dr. (2010.), HESS, doi.org/10.5194/hess-14-1881-2010. Ondje pomaže na malim i brzim slivovima. Kod nas je na Dunavu promijenila grešku za najviše ±0,5 cm, a na Komáromu i Budimpešti prognozu čak malo digla. Ostaje kandidat samo za Muru i male pritoke.",
				"Ažuriranje po zadnjoj grešci — Madsen i Skotner (2005.), Journal of Hydrology, doi.org/10.1016/j.jhydrol.2004.10.030; Goswami i dr. (2005.), HESS, doi.org/10.5194/hess-9-394-2005. Kod nas daje ±0,2 cm, jer dnevni model već računa od zadnjeg izmjerenog stanja; to potvrđuje i Refsgaard (1997.), doi.org/10.2166/nh.1997.0005.",
				"Neuronska mreža (LSTM) za jednu letvu — Kratzert i dr. (2024.), HESS, doi.org/10.5194/hess-28-4187-2024. Autori sami upozoravaju da je loša kad se uči na jednom slivu; dolazi u obzir samo za sve letve zajedno, uz provjeru po godinama.",
				"Radovi s nepoštenom provjerom — npr. prognoza Dunava kod Novog Sada s greškom od 0,055 % (Milić i dr., 2024., doi.org/10.22190/FUACR240111002M). Tako mala greška znači da su podaci za provjeru izmiješani s podacima za učenje; to je primjer onoga što izbjegavamo.",
				"Tuđe prognoze kao ulaz — EFAS (Thielen i dr., 2009., doi.org/10.5194/hess-13-125-2009), austrijski sustav gornjeg Dunava (Nester, Komma i Blöschl, 2016., doi.org/10.1515/johh-2016-0033), mađarski model (Szöllősi-Nagy, 1982., doi.org/10.1016/0022-1694(82)90036-1). Korisni su kao opis metoda i mjerilo, ali po Odlukama (22. 10. 2026.) naša prognoza ne ovisi ni o jednoj tuđoj."),
			tekstM("Nismo našli objavljen opis modela slovačke i srpske službe, ni rad o prognozi neposredno ispod "+
				"kaskade Varaždin–Čakovec–Dubrava; za tu dionicu najbolji izvor ostaje studija MuraDrava-FFS."),
		),
		detaljiM("Što je novo",
			popisM(
				"25. 10. 2026. — u detaljima je novo poglavlje Znanstveni radovi i literatura: što radovi potvrđuju u našim brojkama, što iz njih možemo primijeniti, a što ne.",
				"24. 10. 2026. — dnevna prognoza Batine, Aljmaša, Vukovara i Iloka ispravlja se prema satnim mjerenjima Wildungsmauera i Achleitena; 4.–6. dan griješi oko 2,5 cm manje; uz izdanje piše kad ispravak radi samo prema Wildungsmaueru ili ga nema, i kad digne prognozu za više od "+uzvNapomena+" cm.",
				"22. 10. 2026. — kiša ulazi u satni lanac Dunava: Komárom i Budimpešta imaju dnevni model s kišom i ugađanje u samom lancu (oznaka ds), pa kišu nose i letve nizvodno; Batina za 96 sati griješi 2,7 cm manje, a kad voda raste 5 cm manje; mađarske letve od Esztergoma do Mohácsa dnevni dio posuđuju od susjeda (oznaka p); profil Dunava kreće od Komároma.",
				"22. 10. 2026. — Siga, Petreš, Bogojevo, Dalj, Sotin i Mohovo dnevni dio posuđuju od susjeda (oznaka p), pa uzdužni profil više nema grbe; Bezdan, Apatin i Bačka Palanka pokazuju isto što i letve s kojih se prenose; uz p raspon procijenjen iz susjeda i protok iz krivulje.",
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
				l.Dnevni = ulaziDnevnog(x.Kod, ulazi, ime)
				l.DnevniOd = dnevniOd(x.Kod)
			}
			out = append(out, l)
		}
	}
	return out
}

// ulaziDnevnog su ulazi dnevnog modela postaje imenima, a za postaju s
// uzvodnim ispravkom na kraju i letve ispravka, npr. „ispravak: Wildungsmauer
// i Achleiten”.
func ulaziDnevnog(kod string, ulazi []string, ime func(string) string) []string {
	var out []string
	for _, u := range ulazi {
		out = append(out, ime(u))
	}
	if !prognoza.ImaUzvodniIspravak(kod) {
		return out
	}
	uzvodne := make([]string, len(prognoza.UzvodneLetve))
	for i, l := range prognoza.UzvodneLetve {
		uzvodne[i] = ime(l)
	}
	return append(out, "ispravak: "+nabroji(uzvodne))
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
