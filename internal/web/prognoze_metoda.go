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

// OdjeljakMetode je naslov s odlomcima.
type OdjeljakMetode struct {
	Naslov  string
	Odlomci []OdlomakMetode
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
// slučajeva koji ostaju u rasponu, izdaje centar koji prognozu izdaje.
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
	dunav, drava := daniDnevnog()

	dani := []string{"", "1. dan", "2. dan", "3. dan", "4. dan", "5. dan", "6. dan"}
	sati := []string{"", "6 h", "12 h", "18 h", "24 h", "48 h", "96 h"}
	return []OdjeljakMetode{
		{"Kako čitati prognozu", []OdlomakMetode{
			tekstM("Uz svaku postaju stoje vrijednosti za +6 i +12 sati te za sedam dana u 07 h: vodostaj u " +
				"centimetrima na nuli letve, a gdje postaja ima krivulju protoka, i protok u m³/s. Vrijednost je " +
				"najvjerojatnija, a ne najgora."),
			tekstM(fmt.Sprintf("Raspon uz vrijednost (±, ili granicama kad je nesimetričan) obuhvaća %d %% slučajeva. "+
				"To znači da voda otprilike jednom u tri termina izađe iz njega, podjednako iznad i ispod. Za "+
				"odluku vrijedi gledati gornju granicu, a ne samo sredinu.", udio)),
			tekstM("Oznake: slovo d uz vrijednost znači da je dao dnevni model, a ne satni lanac. Svjetlije pisana " +
				"vrijednost je ona na kojoj prognoza u provjeri ne pobjeđuje pretpostavku da se ništa neće promijeniti " +
				"— ondje je bolje vjerovati zadnjem mjerenju. Sitno ispod stoje tuđe prognoze za isti termin: HU " +
				"mađarska, RS srpska, AT austrijska. Termin obojen bojom faze obrane doseže prag, obrubljen ga " +
				"doseže tek gornjom granicom raspona."),
			tekstM("Kartica postaje na vrhu lanca kaže odakle joj budućnost: iz modela ispuštanja elektrane, iz " +
				"našeg dnevnog modela ili iz tuđe prognoze s navedenim izvorom. Uzdužni profil crta promjenu prema " +
				"danas uzduž toka; klizač pomiče vrijeme, brane su okomite crte, a razine akumulacija točke uz njih."),
			tekstM("Prognozi vrijedi vjerovati manje:"),
			popisM(
				"na Dravi od trećeg dana pri velikom valu, jer ga model podcjenjuje — gledati gornju granicu raspona;",
				"na Dunavu pri valu kad mađarska prognoza Komároma izostane, jer naš lanac iz Austrije val propušta prebrzo;",
				"na Varaždinu, gdje razinu vodi HE Varaždin sat po sat, pa je prognoza tek za val vrijedna više od postojanosti;",
				"pri vodi izvan svega što je arhiva vidjela, jer model tada produžuje zadnji pravac."),
			tekstM("Rječnik:"),
			popisM(
				"doseg — koliko sati ili dana unaprijed vrijedi vrijednost;",
				"postojanost — pretpostavka da će voda ostati kakva je sad; mjera s kojom se svaka prognoza uspoređuje;",
				"raspon — interval oko vrijednosti u kojem voda ostaje u zadanom udjelu slučajeva;",
				"vrh lanca — postaja iznad koje nemamo ništa u računu; njezinu budućnost daje poseban izvor ili zadnje mjerenje;",
				"karika — postaja koja se računa iz uzvodnih;",
				"režim — obična ili velika voda; model se za svaki uči zasebno;",
				"analogije — povijesni dani najsličniji današnjem, čije se stvarne promjene uzmu kao procjena;",
				"međusliv — dio sliva između dviju letvi, za koji se kiša zbraja."),
		}},
		{"Ukratko o metodi", []OdlomakMetode{
			tekstM("Prognoza " + izdaje + " je statistička, a ne hidraulička: ne rješava jednadžbe tečenja, " +
				"nego iz dugih nizova mjerenja uči kako se val prenosi od postaje do postaje. Rade dva modela. " +
				"Prvih dana satni hidrološki lanac — regresija nizvodne postaje na zakašnjele vrijednosti " +
				"uzvodnih; dalje dnevni statistički model na dnevnim srednjacima od 1901. s kišom po međuslivovima — " +
				"regresija s pragom i metoda analognih situacija. Koji model daje koji dan određeno je provjerom na " +
				"poplavnim valovima, zasebno za svaku postaju (tablica postaja na kraju)."),
			tekstM("Ono što model ne može izmjeriti, uzima iz najboljeg dostupnog izvora: na vrhu Drave iz " +
				"naučenog ponašanja hidroelektrana, na vrhu Mure iz vlastitog dnevnog modela s kišom, na vrhu Dunava " +
				"iz mađarske prognoze Komároma dok je svježa, inače iz austrijske prognoze Wildungsmauera; kišu koja " +
				"tek pada iz prognoza Open-Meteo. Kad izvora nema, vrh lanca drži zadnje mjerenje."),
		}},
		{"Ulazni podaci", []OdlomakMetode{
			tekstM("Satni vodostaji i protoci iz arhive goCOP-a, koja se puni s mjernih sustava Hrvatskih voda " +
				"(uključivo istjecanje i razine akumulacija HE Varaždin, Čakovec i Dubrava sa zatvorene mobilne " +
				"stranice) i sa stranica hidroloških službi susjednih država: Slovenije (ARSO), Mađarske (vizugy.hu), " +
				"Slovačke (SHMÚ), Austrije (eHYD, viadonau, noel.gv.at) i Njemačke (GKD, Pegelonline). Očitanja rjeđa " +
				"od satnih premošćuju se linearno, ali ne preko " + tekstBroja(prognoza.NajveciRazmak) + " sati — " +
				"dulja rupa ostaje rupa."),
			tekstM("Tuđe prognoze preuzimaju se kako ih službe izdaju: mađarska (hydroinfo.hu) i srpska " +
				"(hidmet.gov.rs) jednom dnevno za šest, odnosno četiri dana, austrijska (Donja Austrija, noel.gv.at) " +
				"više puta dnevno za 48 sati. Mađarska vodi Komárom, austrijska Wildungsmauer, srpska stoji samo " +
				"radi usporedbe; sve se pamte, da se s našom prognozom uspoređuju i unatrag."),
			tekstM("Oborina: kvazi-kišomjeri registra slivova po međuslivovima između letvi (Drava A–G, Dunav H–J " +
				"od Komároma do Aljmaša, gornji Dunav K–O od Bavarske do Komároma, Mura B), po visinskim pojasima. " +
				"Povijest je reanaliza ERA5 (Open-Meteo) od 1990.; uživo zadnjih sedam dana daje analiza, a sljedećih " +
				"sedam prognoza prognostičkih modela Open-Meteo, preuzeta svaki sat u zasebnu bazu."),
		}},
		{"Satni lanac — građa", []OdlomakMetode{
			tekstM("Postaje su složene u lanac niz tok. Svaka postaja y ima glavni ulaz x₁ — uzvodnu postaju na " +
				"istoj vodi — i po potrebi sporedne ulaze x₂ … xₘ (pritoke, istjecanje elektrane). Postaja se " +
				"računa u onoj veličini u kojoj je veza najčvršća, vodostaju ili protoku. Prognoza nizvodne " +
				"postaje računa se iz prognoza uzvodnih, rekurzivno, pa se pogreška uzvodno nosi nizvodno — i " +
				"ulazi u izmjereni raspon. Svaki ulaz uzima se zakašnjen i zaglađen kliznim prosjekom:"),
			formulaM("x̄ⱼ(t) = (1 / wⱼ) · Σᵢ xⱼ(t − Lⱼ − i),   i = 0 … wⱼ − 1",
				`\bar{x}_j(t)=\frac{1}{w_j}\sum_{i=0}^{w_j-1}x_j(t-L_j-i)`),
			tekstM("Lⱼ je vrijeme propagacije vala u satima, a wⱼ širina prozora kojim se ulaz zagladi: rijeka " +
				"kratke valove guši, pa se dnevni val hidroelektrane ne smije prenijeti nizvodno neprigušen."),
			tekstM("Veza s glavnim ulazom je neprekinuta, po dijelovima linearna funkcija (linearni spline) s " +
				"čvorovima c₀ < c₁ < … < c_K na percentilima " + strings.Join(pojasi, ", ") + " glavnog ulaza. " +
				"Sporedni ulazi ulaze linearno, jednim nagibom:"),
			formulaM("y(t) = Σₖ βₖ · φₖ(x̄₁(t)) + Σⱼ γⱼ · x̄ⱼ(t) + ε(t),   k = 0 … K,   j = 2 … m",
				`y(t)=\sum_{k=0}^{K}\beta_k\varphi_k\!\left(\bar{x}_1(t)\right)+\sum_{j=2}^{m}\gamma_j\bar{x}_j(t)+\varepsilon(t)`),
			tekstM("φₖ su „šatorske” bazne funkcije: φₖ(cₖ) = 1, u susjednim čvorovima 0, linearno između. " +
				"βₖ je tako vrijednost veze u čvoru cₖ, a pravci susjednih pojasa vodnosti sastaju se na " +
				"granici — val koji raste ne dobiva skok kakvog u rijeci nema. Pojasi su gušći pri velikoj vodi, " +
				"jer se ondje ponašanje mijenja: voda izlazi u inundaciju, a Kopački rit se puni i val uspori."),
			tekstM("Karika Nagybajcs ← Wildungsmauer namještena je na 5–7 sati kašnjenja, što vrijedi pri običnoj " +
				"vodi; pri velikoj vodi val kroz Szigetköz, akumulaciju Gabčíkovo i rukavce putuje tri dana, a " +
				"pridružuje mu se Morava. Satna povijest Angerna na Moravi i Bratislave skuplja se da se ta dionica " +
				"jednom namjesti po režimima."),
		}},
		{"Satni lanac — procjena", []OdlomakMetode{
			tekstM("Koeficijenti β i γ procjenjuju se metodom najmanjih kvadrata, zajednički za sve pojase, " +
				"rješavanjem normalnih jednadžbi uz neznatnu stabilizaciju dijagonale (10⁻⁹ · trag / n)."),
			tekstM(fmt.Sprintf("Kašnjenja i prozori biraju se tako da maksimiziraju koeficijent determinacije R², "+
				"naizmjeničnom pretragom po koordinatama: glavni ulaz L ∈ [0, %d] h, sporedni L ∈ [0, %d] h, "+
				"w ∈ {%s} h. Kandidati se uspoređuju na istim satima, a prozor koji bi izbacio više od četvrtine "+
				"sati s potpunim podacima ne uzima se. Kašnjenje glavnog ulaza zatim se traži zasebno u svakom "+
				"pojasu vodnosti, jer val pri velikoj vodi putuje drukčije — na dionici Batina → Aljmaš od 4 do "+
				"38 sati. Kašnjenje pritoka i širina prozora svojstvo su dionice, pa ostaju ista u svim pojasima.",
				prognoza.NajveciPomak, prognoza.NajveciPomakPritoka, strings.Join(sirine, ", "))),
		}},
		{"Satni lanac — vrhovi lanca", []OdlomakMetode{
			tekstM("Za sate poslije zadnjeg mjerenja postaja na vrhu lanca drži zadnje izmjereno stanje " +
				"(postojanost), osim gdje ima bolji izvor. Svaki takav izvor F ulazi kao promjena od trenutka " +
				"zadnjeg mjerenja, ne kao gotova vrijednost, pa se vrh nikad ne odvoji od onoga što je izmjereno:"),
			formulaM("x(t) = x_mj(t₀) + [F(t) − F(t₀)]",
				`x(t)=x_{\mathrm{mj}}(t_0)+\left[F(t)-F(t_0)\right]`),
			tekstM("Drava: model ispuštanja elektrane. Istjecanje HE Dubrava (vrh lanca Botova), HE Čakovec i HE " +
				"Varaždin predviđa regresija po dosegu 1–96 h i po režimu (obična ili velika voda, granica je 90. " +
				"percentil istjecanja) iz vlastitog istjecanja, dotoka uzvodnih elektrana te sata i dana u tjednu " +
				"sada i u ciljnom satu. HE Varaždin vrši — navečer u 20 h ide 150 % dnevnog srednjaka, noću polovina, " +
				"u svim godišnjim dobima — a pri velikoj vodi sve tri stepenice propuštaju dotok s nekoliko sati " +
				"kašnjenja. Za svaki doseg i režim pamti se je li model u provjeri pobijedio postojanost; gdje nije, " +
				"vrh drži zadnje mjerenje. Razina akumulacije ne ulazi, jer ne mijenja brojke."),
			tablicaM("Istjecanje HE Dubrava, srednja pogreška u m³/s: model / postojanost (2024.–2026., model naučen prije)",
				sati,
				[]string{"velika voda", "39 / 64", "48 / 98", "57 / 114", "77 / 121", "109 / 154", "141 / 194"},
				[]string{"obična voda", "68 / 69", "— / 67", "56 / 77", "54 / 61", "65 / 73", "74 / 79"}),
			tablicaM("Što to daje lancu: rasap u provjeri unatrag 2023.–2025., prije → poslije",
				[]string{"", "24 h", "48 h", "72 h", "96 h"},
				[]string{"Botovo (m³/s)", "85 → 71", "116 → 99", "136 → 119", "150 → 130"},
				[]string{"Terezino Polje (m³/s)", "47 → 42", "81 → 66", "107 → 93", "122 → 106"},
				[]string{"Donji Miholjac (cm)", "9 → 9", "25 → 23", "37 → 32", "47 → 42"},
				[]string{"Belišće (cm)", "6 → 6", "18 → 17", "29 → 25", "36 → 32"},
				[]string{"Varaždin (cm)", "27 → 21", "33 → 25", "35 → 26", "36 → 27"}),
			tekstM("Mura: naš dnevni model. Goričan, naša letva nasuprot Letenyeu (isti rkm, javni izvor, satni niz " +
				"od 1982.), u protoku kroz vlastitu krivulju daje vrh lanca Botova. Budućnost mu daje dnevni model iz " +
				"Murskog Središća i kiše nad Murom; Letenye je rezerva, a mađarska prognoza rezerva rezervi."),
			tablicaM("Mura, srednja pogreška 1.–6. dana u cm (28 mađarskih izdanja 2024.–2026., model naučen prije)",
				dani,
				[]string{"Letenye, mađarska prognoza", "13", "23", "28", "37", "42", "50"},
				[]string{"Letenye, naš dnevni model", "13", "22", "18", "22", "30", "24"},
				[]string{"Goričan, naš dnevni model", "7", "14", "15", "19", "20", "23"}),
			tekstM("Dunav: mađarska prognoza dok je svježa. Komárom se računa iz Nagybajcsa i Wildungsmauera, a " +
				"Wildungsmauer slijedi 48-satnu prognozu Donje Austrije; ipak, dok je mađarska prognoza Komároma " +
				"svježa (ne starija od dva dana), Komárom postaje vrh i slijedi nju, jer je u valu dvostruko bolja od " +
				"našeg lanca. Lanac iz Austrije je rezerva za sate kad mađarske nema. Cilj ostaje potpuna neovisnost, " +
				"ali ne po cijenu lošije prognoze."),
			tablicaM("Komárom, srednja pogreška 1.–6. dana u cm (27 mađarskih izdanja 2024.–2026., uglavnom valovi)",
				dani,
				[]string{"mađarska prognoza", "12", "20", "28", "38", "47", "59"},
				[]string{"naš lanac od Wildungsmauera", "18", "38", "61", "90", "113", "131"},
				[]string{"isti lanac sa savršenom austrijskom prognozom", "27", "51", "58", "51", "43", "60"}),
		}},
		{"Satni lanac — izdavanje prognoze", []OdlomakMetode{
			tekstM("Ispravak prema mjerenju. Razlika između modela i zadnjeg mjerenja postaje (ne starijeg od " +
				tekstBroja(prognoza.ZaostatakVrha) + " sata) nosi se naprijed i eksponencijalno slabi, s " +
				"poluvremenom od " + poluvrijeme + " sati:"),
			formulaM("ŷ*(t₀ + τ) = ŷ(t₀ + τ) + r₀ · 2^(−τ / "+poluvrijeme+"),   r₀ = y_mj(t₀) − ŷ(t₀)",
				`\hat{y}^{*}(t_0+\tau)=\hat{y}(t_0+\tau)+r_0\,2^{-\tau/`+texBroj(poluvrijeme)+`},\qquad r_0=y_{\mathrm{mj}}(t_0)-\hat{y}(t_0)`),
			tekstM("Sustavna pogreška. Prognoza je puštena unatrag kroz arhivu — izdanje svakih 12 sati kroz " +
				"više godina, svako samo s onim što je u tom trenutku bilo izmjereno, s istim vrhovima lanca kao " +
				"uživo — i za svaku postaju i doseg τ izmjerena je srednja pogreška b(τ). Ona se od prognoze oduzima."),
		}},
		{"Satni lanac — raspon", []OdlomakMetode{
			tekstM(fmt.Sprintf("Polovina širine raspona r(τ) je empirijski %d. percentil apsolutnog odstupanja "+
				"pogreške od njezine srednje vrijednosti, iz iste provjere unatrag, izravnan po dosegu (±6 h):", udio)),
			formulaM(fmt.Sprintf("r(τ) = Q_%s( |eᵢ(τ) − b(τ)| ),   prognoza = ŷ*(t₀ + τ) − b(τ) ± r(τ)",
				brojHRf(float64(udio)/100, 2)),
				`r(\tau)=Q_{`+texBroj(brojHRf(float64(udio)/100, 2))+`}\!\left(\left|e_i(\tau)-b(\tau)\right|\right),\qquad \mathrm{prognoza}=\hat{y}^{*}(t_0+\tau)-b(\tau)\pm r(\tau)`),
			tekstM("Raspon se dakle ne izvodi iz pretpostavke o normalnoj raspodjeli pogrešaka, nego brojanjem. " +
				"Kad bi pogreške bile normalne, r bi bio 1,04 standardna odstupanja."),
			tekstM("Ista provjera daje i korijen srednje kvadratne pogreške postojanosti — pretpostavke da se " +
				"ništa neće promijeniti. Termin na kojem prognoza nju ne pobjeđuje pisan je na stranici svjetlije: " +
				"ondje je bolje vjerovati zadnjem mjerenju."),
		}},
		{"Dnevni model — značajke i cilj", []OdlomakMetode{
			tekstM("Uči se na dnevnim srednjacima vodostaja od 1901.; dan kojem u arhivi nema dnevnog srednjaka " +
				"dopunjuje se srednjakom satnih vrijednosti, ako ih ima barem 18. Za ciljnu postaju T i njezine " +
				"ulaze s (tablica postaja) značajke dana t su:"),
			formulaM("z(t) = [ 1,  h_T(t),  { Δ¹h_s(t), Δ²h_s(t) } za s ∈ {T} ∪ ulazi,  kiša ]",
				`\mathbf z(t)=\left[1,\ h_T(t),\ \left\{\Delta^1h_s(t),\Delta^2h_s(t)\right\}_{s\in\{T\}\cup\mathrm{ulazi}},\ \mathrm{ki\check{s}a}\right]`),
			formulaM("Δ¹h(t) = h(t) − h(t−1),   Δ²h(t) = h(t−1) − h(t−3)",
				`\Delta^1h(t)=h(t)-h(t-1),\qquad \Delta^2h(t)=h(t-1)-h(t-3)`),
			tekstM("Uz razinu cilja ulaze samo promjene, jer one ne ovise o nuli vodokaza, a nule su se kroz " +
				fmt.Sprintf("stoljeće mijenjale. Cilj je promjena na dosegu k = 1 … %d dana, svaki doseg sa svojim ", prognoza.DnevniDosezi) +
				"modelom (izravna višekoračna prognoza, bez rekurzije):"),
			formulaM("Δₖ(t) = h_T(t + k) − h_T(t)",
				`\Delta_k(t)=h_T(t+k)-h_T(t)`),
			tekstM("Kiša: za svaki međusliv uzvodno od cilja zbroj kiše zadnjeg dana, zadnja tri dana i zadnjih " +
				"sedam dana te prognozirana kiša sljedeća dva, četiri i šest dana, u mm, kao težinski srednjak " +
				"kvazi-kišomjera međusliva po visinskim pojasima. Model s kišom uči od 1990., na kiši koja je doista " +
				"pala i poslije (savršena prognoza); uživo ulazi analiza i prognoza. Kad oborine nema, uzima se " +
				"inačica bez nje. U udaljenosti analogija oborina nosi polovicu težine promjena vodostaja."),
			tekstM("Ciljevi i ulazi: na Muri Mursko Središće iz vlastite razine i kiše (uzvodno nema satne letve), " +
				"Goričan, Letenye i Kotoriba iz uzvodnih; na Dravi Botovo do Osijeka iz uzvodnih letvi, Murskog " +
				"Središća i Borla; Osijek uz to iz Mohácsa, Budimpešte i Komároma s dunavskom kišom, jer ondje odlučuje " +
				"uspor Dunava koji te letve vide dva-tri dana prije Batine; na Dunavu Batina, Aljmaš, Vukovar i Ilok iz " +
				"Komároma, Budimpešte, Mohácsa i međusobno, s dravskim ulazima za letve ispod ušća. Komárom nije dnevni " +
				"cilj: s kišom gornjeg Dunava griješi preko svih dana 10, 20, 29, 35, 39 i 43 cm, ali na mađarskim " +
				"izdanjima 25–119 prema njihovih 12–59, jer oni nose cijeli austrijsko-njemački prognostički lanac."),
		}},
		{"Dnevni model — što kiša donosi", []OdlomakMetode{
			tekstM("Kiša koja je pala i kiša koja se prognozira ulaze u model od 25. rujna 2026. Prva dva dana ne " +
				"mijenjaju, jer tu vodu postaje već vide; od trećeg dana donose većinu poboljšanja. Provjere su " +
				"modelima naučenima prije razdoblja provjere."),
			tablicaM("Drava, vrh vala 5. i 6. dana, srednja pogreška u cm (valovi 2012.–2024., model naučen 1990.–2011.): bez kiše → s palom kišom → i s budućom kišom iz arhive",
				[]string{"", "5. dan", "6. dan"},
				[]string{"Osijek", "70 → 54 → 67", "108 → 71 → —"},
				[]string{"Belišće", "80 → 60 → —", "119 → 94 → 70"},
				[]string{"Donji Miholjac", "110 → 86 → —", "172 → 141 → 92"}),
			tablicaM("Botovo, pogreška preko svih dana u cm (2024.–2026., model naučen do 2023.): bez prognoze kiše / s pravim prognozama Open-Meteo / sa savršenom prognozom",
				[]string{"", "3. dan", "4. dan", "5. dan", "6. dan"},
				[]string{"Botovo", "33 / 24 / 24", "40 / 28 / 27", "44 / 32 / 29", "46 / 37 / 29"}),
			tekstM("Prognoza kiše treći i četvrti dan donese gotovo sve, peti tri četvrtine, šesti polovicu. " +
				"Umjeravanje prognoze na razinu reanalize po točkama provjeru pogoršava, pa se prognoza uzima kakva jest."),
			tablicaM("Dunav, pogreška preko svih dana u cm (2024.–2026., model naučen do 2023., prave prognoze kiše): bez kiše → s kišom H, I, J",
				[]string{"", "4. dan", "5. dan", "6. dan"},
				[]string{"Batina", "23 → 21", "37 → 31", "50 → 40"},
				[]string{"Aljmaš", "23 → 21", "34 → 28", "45 → 35"},
				[]string{"Vukovar", "17 → 16", "26 → 22", "35 → 28"},
				[]string{"Ilok", "16 → 16", "24 → 21", "33 → 27"},
				[]string{"Osijek, s dunavskim ulazima", "32 → 29", "37 → 34", "42 → 38"}),
			tekstM("Prva tri dana na Dunavu su lošija za pola do jedan centimetar. Dravski međuslivovi dunavskim " +
				"letvama ne pomažu, pa ne ulaze. Je li dnevna prognoza računata s kišom, piše uz vrijeme izdanja na " +
				"stranici Prognoze, uz razlog kad nije; u izvozu svaka dnevna vrijednost nosi oznaku modela, " +
				"dnevni-1-kisa ili dnevni-1."),
		}},
		{"Dnevni model — procjena", []OdlomakMetode{
			tekstM("(a) Linearna regresija s pragom. Dani se dijele na dva režima po 75. percentilu razine h_T; " +
				"za svaki režim i svaki doseg koeficijenti se procjenjuju metodom najmanjih kvadrata, uz " +
				"neznatan greben (10⁻⁶) na dijagonali."),
			tekstM("(b) Metoda analognih situacija (k najbližih susjeda). Značajke se standardiziraju, a u " +
				"euklidskoj udaljenosti razina cilja nosi dvostruku težinu, jer isti porast drukčije završi " +
				"pri velikoj vodi:"),
			formulaM("d²(t, u) = 2 · ((h_T(t) − h_T(u)) / σ_h)² + Σᵢ ((zᵢ(t) − zᵢ(u)) / σᵢ)²",
				`d^2(t,u)=2\left(\frac{h_T(t)-h_T(u)}{\sigma_h}\right)^2+\sum_i\left(\frac{z_i(t)-z_i(u)}{\sigma_i}\right)^2`),
			tekstM(fmt.Sprintf("Uzima se K = %d povijesnih dana najbližih današnjem; njihove stvarne promjene Δₖ "+
				"daju procjenu (srednjak) i rasipanje (standardno odstupanje sₖ).", prognoza.DnevnihAnalogija)),
			tekstM(fmt.Sprintf("Prognoza je spoj dviju procjena, tri četvrtine regresije i četvrtina analogija, a raspon "+
				"rasipanje analogija pomnoženo s %s, da cilja isti udio kao satni lanac, najmanje 1 cm. Analogije "+
				"vrh velikog vala vuku prema srednjem danu, jer rijetkoj velikoj kiši nema dovoljno sličnih dana; sama "+
				"regresija pak s pravim prognozama kiše pojačava njihovu pogrešku 4.–6. dan. Omjer tri četvrtine "+
				"zadržava dobitak prvih dana (Donji Miholjac 7 → 5 cm, Belišće 6 → 4), 4.–6. dan ne gubi ništa, a vrh "+
				"hvata bolje:", brojHRf(prognoza.DnevniRasponMnozitelj, 2))),
			tablicaM("Vrh Botova 2.–6. dana, srednja pogreška u cm (11 dravskih valova 2012.–2023., kiša iz arhive)",
				[]string{"", "2. dan", "3. dan", "4. dan", "5. dan", "6. dan"},
				[]string{"same analogije", "64", "102", "119", "156", "143"},
				[]string{"pola-pola (do 25. 9. 2026.)", "48", "85", "102", "134", "123"},
				[]string{"¾ regresije (sad)", "41", "76", "94", "122", "114"},
				[]string{"sama regresija", "33", "68", "87", "111", "104"}),
			formulaM("Δ̂ₖ = ¾ · Δₖ_reg + ¼ · Δₖ_kNN,   ĥ_T(t + k) = h_T(t) + Δ̂ₖ ± "+brojHRf(prognoza.DnevniRasponMnozitelj, 2)+" · sₖ",
				`\widehat{\Delta}_k=\tfrac{3}{4}\,\Delta_{k,\mathrm{reg}}+\tfrac{1}{4}\,\Delta_{k,\mathrm{kNN}},\qquad \hat{h}_T(t+k)=h_T(t)+\widehat{\Delta}_k\pm `+texBroj(brojHRf(prognoza.DnevniRasponMnozitelj, 2))+`\,s_k`),
			tekstM("Uživo je „dan” srednjak 24 sata koji završavaju u satu izdavanja, pa se dnevna prognoza " +
				"obnavlja svaki sat, a ne tek u ponoć; vrijednost za dan k srednjak je 24 sata koji završavaju " +
				"k dana poslije."),
		}},
		{"Koji model daje koji dan", []OdlomakMetode{
			tekstM("Satni lanac seže do 96 sati. Od kojeg dana vrijednost daje dnevni model određeno je " +
				"provjerom na poplavnim valovima, zasebno za svaku postaju: na Dunavu od " + dunav + ", na Dravi od " +
				drava + ". Na Dravi satni lanac dulje pogađa bolje jer nosi istjecanje HE Dubrava s modelom " +
				"ispuštanja i Goričan iz dnevnog modela. Vrijednost iz dnevnog modela na stranici je označena slovom " +
				"d, a u Excelu retkom „model”."),
		}},
		{"Vodostaj i protok", []OdlomakMetode{
			tekstM("Postaja se računa u jednoj veličini, a drugu daje važeća krivulja protoka (Q–H) postaje. " +
				"Krivulja je monotona, pa se kroz nju preračunaju i granice raspona: interval zadrži istu " +
				"vjerojatnost, ali može postati nesimetričan — tada se piše granicama umjesto ±. Gdje krivulje " +
				"nema, nema ni druge veličine."),
		}},
		{"Raspon i vjerojatnost", []OdlomakMetode{
			tekstM(fmt.Sprintf("Raspon obuhvaća %d %% slučajeva: u %d %% stvarna vrijednost izlazi iz njega, "+
				"podjednako iznad i ispod. Raspon nije granica mogućeg — otprilike jednom u tri termina "+
				"vrijednost je izvan njega; za 95 %% slučajeva, uz normalnu raspodjelu, trebao bi otprilike "+
				"dvostruko širi. Mađarska hidrološka služba uz svoju prognozu navodi isti udio (70 %%), pa "+
				"naš i njihov raspon znače isto i izravno su usporedivi.", udio, 100-udio)),
		}},
		{"Provjera i usporedbe", []OdlomakMetode{
			tekstM("Svaki dio modela provjeren je na podacima koje pri učenju nije vidio. Dnevni model učen je do " +
				"2012. i mjeren na valovima 2012.–2024., a s kišom do 2023. i mjeren na 2024.–2026. s tada izdanim " +
				"prognozama kiše. Satni lanac provjeren je na 34 poplavna vala od 2012. metodom izostavljanja skupine: " +
				"svaki val prognozira model namješten bez njega i bez dvadesetak dana oko njega. Mjeri se pogreška " +
				"vrha vala (srednja apsolutna i pristranost) na 24, 48, 72 i 96 h, korijen srednje kvadratne pogreške " +
				"kroz val i udio mjerenja unutar raspona. Sve se uspoređuje s postojanošću, s mađarskom prognozom, s " +
				"uredskom prognozom (55 izdanja) i s modelom MIKE (48)."),
			tablicaM("Drava prema mađarskoj prognozi: srednja pogreška 1.–6. dana u cm, oni / mi (28 izdanja rujan 2024. – rujan 2026., dnevni model s kišom naučen do rujna 2024.)",
				dani,
				[]string{"Botovo", "24 / 16", "37 / 26", "41 / 27", "51 / 28", "52 / 37", "58 / 38"},
				[]string{"Terezino Polje", "14 / 10", "28 / 24", "40 / 30", "48 / 29", "52 / 32", "60 / 39"},
				[]string{"Donji Miholjac", "7 / 5", "20 / 16", "36 / 29", "47 / 34", "55 / 33", "52 / 38"},
				[]string{"Belišće", "6 / 5", "10 / 13", "20 / 24", "29 / 30", "42 / 29", "44 / 30"},
				[]string{"Osijek", "12 / 14", "23 / 28", "36 / 41", "46 / 53", "58 / 60", "62 / 67"}),
			tablicaM("Dunav prema mađarskoj prognozi: srednja pogreška 1.–6. dana u cm, oni / mi (27 izdanja; naš satni lanac od Wildungsmauera bez mađarske, dnevni model s kišom)",
				dani,
				[]string{"Komárom, satni lanac", "12 / 18", "20 / 38", "28 / 61", "38 / 90", "47 / 113", "59 / 131"},
				[]string{"Mohács, satni lanac", "7 / 12", "14 / 33", "20 / 51", "24 / 57", "28 / 66", "31 / 73"},
				[]string{"Aljmaš, dnevni model", "29 / 9", "31 / 18", "31 / 31", "32 / 49", "37 / 68", "43 / 76"}),
			tekstM("Na Dravi smo bolji ili jednaki na svim dosezima, Osijek od uspora Dunava nešto lošiji. Na " +
				"mađarskom Dunavu njihov je model bolji, pa Komárom vodi njihova prognoza. Njihova prognoza Aljmaša " +
				"sustavno je 25 cm previsoka, pa je usporedba tamo u našu korist do trećeg dana. Sve ove račune " +
				"administrator ponavlja dijagnostičkim alatima izvan aplikacije."),
		}},
		{"Ograničenja", []OdlomakMetode{
			popisM(
				"Veliki dravski val od trećeg dana prognoza podcjenjuje: vrh Botova 3.–5. dan promaši 76, 94 i 122 cm i sa savršenom kišom. Pratiti gornju granicu raspona.",
				"Rad hidroelektrana unaprijed se zna samo koliko ga model ispuštanja nauči; unutar dana odluke elektrane ostaju nepredvidive. Varaždin ispod HE Varaždin o njima ovisi cijeli.",
				"Na Dunavu je prognoza za valove ovisna o mađarskoj službi dok se dionica Wildungsmauer → Nagybajcs ne namjesti po režimima.",
				"Vrijednost izvan svega viđenoga u arhivi model procjenjuje produženjem zadnjeg pravca; pri rekordnoj vodi valja biti oprezan.",
				"Prognoza kiše iz Open-Meteo je jedina; kad izostane (kvota, mreža), dnevni model radi bez kiše i to piše uz izdanje."),
		}},
		{"Što je novo", []OdlomakMetode{
			popisM(
				"26. 9. 2026. — model ispuštanja HE Dubrava, Čakovec i Varaždin na vrhu lanca; razine akumulacija na pregledu i profilu; Komárom po mađarskoj prognozi dok je svježa, inače lanac iz Austrije; Osijek s dunavskim ulazima; dnevni model ¾ regresije; Varaždin kao karika; uzdužni profil s branama, punim zaslonom i ispisom, mađarski profili Dunava i Drave.",
				"25. 9. 2026. — austrijska prognoza Wildungsmauera (noel.gv.at) i karike Nagybajcs, Komárom; kiša po međuslivovima Drave, Dunava i Mure u dnevnom modelu; Goričan kao vrh lanca Botova; Mura sa svojom dnevnom prognozom; Angern na Moravi.",
				"23. 9. 2026. — istjecanje elektrana s mletva.voda.hr uživo; mađarska prognoza kao vrh Letenyea i Komároma; raspon 70 %.",
				"Ranije — satni lanac od 2012., dnevni model od 1901., provjera na 195 valova, uzdužni profil s ušćima."),
		}},
	}
}

// daniDnevnog opisuje od kojeg dana dnevni model daje vrijednost, po vodi.
func daniDnevnog() (dunav, drava string) {
	raspon := func(letve ...string) string {
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
	return raspon("batina", "aljmas", "vukovar", "ilok"),
		raspon("botovo", "terezino-polje", "donji-miholjac", "belisce", "osijek")
}

// letveMetode slaže tablicu postaja: ulazi lanca iz namještenih pojasa,
// raspon iz izmjerenih promašaja, ulazi dnevnog modela iz paketa prognoza.
// Redom je kao na pregledu, od uzvodne prema nizvodnoj po vodama.
func letveMetode(tablice []TablicaPrognoza, postaje map[string]models.Station,
	pojasi map[string][]prognoza.Pojas, promasaji map[string]map[int]prognoza.Promasaj) []LetvaMetode {
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
				for _, d := range DoseziRaspona {
					if p, ima := promasaji[x.Kod][d]; ima {
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
				if d, ima := prognoza.DnevnaOdDana[x.Kod]; ima {
					l.DnevniOd = tekstBroja(d) + ". dana"
				}
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
	data := h.podaci(r)
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
