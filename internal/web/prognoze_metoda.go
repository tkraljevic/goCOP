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

// OdlomakMetode je jedan odlomak opisa; formula se piše zasebno, uvučeno.
type OdlomakMetode struct {
	Tekst   string
	Formula bool
	TeX     string // matematički zapis za KaTeX; Tekst ostaje za Excel i rezervni prikaz
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
}

// DoseziRaspona su dosezi za koje tablica postaja navodi raspon.
var DoseziRaspona = []int{24, 48, 72}

func tekstM(s string) OdlomakMetode { return OdlomakMetode{Tekst: s} }
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

	return []OdjeljakMetode{
		{"Ukratko", []OdlomakMetode{
			tekstM("Prognoza " + izdaje + " je statistička, a ne hidraulička: ne rješava jednadžbe tečenja, " +
				"nego iz dugih nizova mjerenja uči kako se val prenosi od postaje do postaje. Rade dva modela. " +
				"Prvih dana satni hidrološki lanac — regresija nizvodne postaje na zakašnjele vrijednosti " +
				"uzvodnih; dalje dnevni statistički model na dnevnim srednjacima od 1901. — regresija s pragom " +
				"i metoda analognih situacija. Koji model daje koji dan određeno je provjerom na poplavnim " +
				"valovima, zasebno za svaku postaju (tablica postaja na kraju)."),
			tekstM("Model ne zna za oborinu koja tek pada ni za budući rad hidroelektrana: sve što zna, zna iz " +
				"vode koja je već izmjerena uzvodno, a na vrhu Mure i Dunava iz prognoze mađarske hidrološke službe."),
		}},
		{"Ulazni podaci", []OdlomakMetode{
			tekstM("Satni vodostaji i protoci iz arhive goCOP-a, koja se puni s mjernih sustava Hrvatskih voda " +
				"(uključivo istjecanje HE Dubrava sa zatvorene mobilne stranice) i sa stranica hidroloških " +
				"službi susjednih država: Slovenije (ARSO), Mađarske (vizugy.hu, hydroinfo.hu), Slovačke (SHMÚ), " +
				"Austrije (eHYD, viadonau) i Njemačke (GKD, Pegelonline). Očitanja rjeđa od satnih premošćuju " +
				"se linearno, ali ne preko " + tekstBroja(prognoza.NajveciRazmak) + " sati — dulja rupa ostaje rupa."),
			tekstM("Mađarska (hydroinfo.hu) i srpska (hidmet.gov.rs) prognoza preuzimaju se kako ih službe " +
				"izdaju, jednom dnevno. Mađarska ulazi u račun na vrhu lanca Dunava, Komáromu (vidi dolje); " +
				"srpska stoji samo radi usporedbe. Vrh lanca Mure je od 25. 9. 2026. Goričan, naša letva nasuprot " +
				"Letenyeu (isti rkm, javni izvor, satni niz od 1982.), u protoku kroz vlastitu krivulju; budućnost mu " +
				"daje naš dnevni model (Mursko Središće i kiša nad Murom), a Letenye je rezerva. Na 28 mađarskih " +
				"izdanja 2024.–2026. naš dnevni model Letenyea griješi 13, 22, 18, 22, 30 i 24 cm za 1.–6. dan, " +
				"njihova prognoza 13, 23, 28, 37, 42 i 50; Goričan iz istih ulaza 7, 14, 15, 19, 20 i 23. Tako i Mura " +
				"ima svoju dnevnu prognozu: Mursko Središće iz vlastite razine i kiše nad Murom (uzvodno nema satne " +
				"letve), Letenye i Kotoriba iz uzvodnih letvi i iste kiše; na 2024.–2026. Mursko Središće griješi " +
				"9, 13, 15, 17, 18 i 21 cm za 1.–6. dan uz postojanost 14–28, Kotoriba 11, 17, 20, 24, 26 i 29 uz " +
				"postojanost 18–41."),
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
		{"Satni lanac — izdavanje prognoze", []OdlomakMetode{
			tekstM("Vrh lanca. Za sate poslije zadnjeg mjerenja postaja na vrhu lanca drži zadnje izmjereno " +
				"stanje (postojanost). Na Muri (Letenye) i Dunavu (Komárom) umjesto toga slijedi promjenu " +
				"mađarske prognoze od trenutka izdavanja, ne stariju od dva dana:"),
			formulaM("x(t) = x_mj(t₀) + [F_HU(t) − F_HU(t₀)]",
				`x(t)=x_{\mathrm{mj}}(t_0)+\left[F_{\mathrm{HU}}(t)-F_{\mathrm{HU}}(t_0)\right]`),
			tekstM("Ispravak prema mjerenju. Razlika između modela i zadnjeg mjerenja postaje (ne starijeg od " +
				tekstBroja(prognoza.ZaostatakVrha) + " sata) nosi se naprijed i eksponencijalno slabi, s " +
				"poluvremenom od " + poluvrijeme + " sati:"),
			formulaM("ŷ*(t₀ + τ) = ŷ(t₀ + τ) + r₀ · 2^(−τ / "+poluvrijeme+"),   r₀ = y_mj(t₀) − ŷ(t₀)",
				`\hat{y}^{*}(t_0+\tau)=\hat{y}(t_0+\tau)+r_0\,2^{-\tau/`+texBroj(poluvrijeme)+`},\qquad r_0=y_{\mathrm{mj}}(t_0)-\hat{y}(t_0)`),
			tekstM("Sustavna pogreška. Prognoza je puštena unatrag kroz arhivu — izdanje svakih 12 sati kroz " +
				"više godina, svako samo s onim što je u tom trenutku bilo izmjereno — i za svaku postaju i " +
				"doseg τ izmjerena je srednja pogreška b(τ). Ona se od prognoze oduzima."),
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
			formulaM("z(t) = [ 1,  h_T(t),  { Δ¹h_s(t), Δ²h_s(t) } za s ∈ {T} ∪ ulazi ]",
				`\mathbf z(t)=\left[1,\ h_T(t),\ \left\{\Delta^1h_s(t),\Delta^2h_s(t)\right\}_{s\in\{T\}\cup\mathrm{ulazi}}\right]`),
			formulaM("Δ¹h(t) = h(t) − h(t−1),   Δ²h(t) = h(t−1) − h(t−3)",
				`\Delta^1h(t)=h(t)-h(t-1),\qquad \Delta^2h(t)=h(t-1)-h(t-3)`),
			tekstM("Uz razinu cilja ulaze samo promjene, jer one ne ovise o nuli vodokaza, a nule su se kroz " +
				fmt.Sprintf("stoljeće mijenjale. Cilj je promjena na dosegu k = 1 … %d dana, svaki doseg sa svojim ", prognoza.DnevniDosezi) +
				"modelom (izravna višekoračna prognoza, bez rekurzije):"),
			formulaM("Δₖ(t) = h_T(t + k) − h_T(t)",
				`\Delta_k(t)=h_T(t+k)-h_T(t)`),
			tekstM("Na Dravi i Dunavu u značajke ulazi i oborina: za svaki međusliv uzvodno od cilja (registar " +
				"slivova: na Dravi međuslivovi A–G između letvi, na Dunavu H Komárom → Budimpešta s Váhom, Hronom i " +
				"Ipeľom, I Budimpešta → Mohács i J Mohács → Aljmaš bez Drave) zbroj kiše zadnjeg dana, zadnja tri dana i zadnjih sedam dana te " +
				"prognozirana kiša sljedeća dva, četiri i šest dana, u mm, kao težinski srednjak kvazi-kišomjera " +
				"međusliva po visinskim pojasima. Povijest je reanaliza ERA5 (Open-Meteo) od 1990., pa model s " +
				"oborinom uči od tada, i to na kiši koja je doista pala i poslije (savršena prognoza); uživo zadnjih " +
				"sedam dana daje analiza, a sljedećih šest prognoza prognostičkih modela (Open-Meteo). Kad oborine " +
				"nema, uzima se inačica bez nje. U udaljenosti analogija oborina nosi polovicu težine promjena vodostaja."),
			tekstM("Provjereno na dravskim valovima 2012.–2024. modelom naučenim 1990.–2011. Sama pala kiša: srednja " +
				"pogreška vrha 5. i 6. dana pada u Osijeku sa 70 i 108 cm na 54 i 71, u Belišću s 80 i 119 na 60 i 94, " +
				"u Donjem Miholjcu sa 110 i 172 na 86 i 141. S budućom kišom iz arhive umjesto prognoze, što je " +
				"gornja granica: Botovo 3.–6. dan sa 90, 134, 156 i 166 na 62, 74, 100 i 96, Belišće 6. dan na 70, " +
				"Donji Miholjac na 92, Osijek na 67. Prva dva dana se ne mijenjaju."),
			tekstM("Koliko od toga prava prognoza kiše donese, provjereno je na arhiviranim prognozama Open-Meteo " +
				"2024.–2026. modelom naučenim do kraja 2023.: pogreška preko svih dana za Botovo 3.–6. dan bez " +
				"prognoze kiše 33, 40, 44 i 46 cm, s pravim prognozama 24, 28, 32 i 37, sa savršenom prognozom 24, " +
				"27, 29 i 29. Prognoza kiše dakle treći i četvrti dan donese gotovo sve, peti tri četvrtine, šesti " +
				"polovicu. Umjeravanje prognoze na razinu reanalize po točkama provjeru pogoršava, pa se prognoza " +
				"uzima kakva jest."),
			tekstM("Na Dunavu su dnevni ulazi tek od 2000-ih, pa je provjera samo na 2024.–2026. modelom naučenim " +
				"do kraja 2023., s arhiviranim prognozama kiše: pogreška preko svih dana 4.–6. dan pada na Batini " +
				"s 23, 37 i 50 cm na 21, 31 i 40, na Aljmašu s 23, 34 i 45 na 21, 28 i 35, na Vukovaru sa 17, 26 i 35 " +
				"na 16, 22 i 28, na Iloku sa 16, 24 i 33 na 16, 21 i 27; prva tri dana lošija su za pola do jedan " +
				"centimetar. Dravski međuslivovi uz dunavske nizvodnim letvama ne pomažu, pa ne ulaze."),
			tekstM("Prema mađarskoj prognozi (hydroinfo.hu, 28 izdanja od rujna 2024. do rujna 2026., model naučen do " +
				"rujna 2024., s arhiviranim prognozama kiše), srednja pogreška 1.–6. dana u cm, oni prema nama: Botovo " +
				"24, 37, 41, 51, 52, 58 prema 16, 26, 27, 25, 34, 36; Terezino Polje 14, 28, 40, 48, 52, 60 prema 11, " +
				"23, 31, 29, 30, 37; Donji Miholjac 7, 20, 36, 47, 55, 52 prema 7, 18, 29, 34, 32, 37; Belišće 6, 10, " +
				"20, 29, 42, 44 prema 6, 13, 23, 29, 29, 28. Bez kiše Botovo je bilo 20, 35, 38, 46, 52, 58. Osijek " +
				"im ostaje bolji (12–62 prema 13–81), jer ondje odlučuje uspor Dunava; Aljmaš je naš bolji do 3. dana, " +
				"njihov od 5. (alat usporedi-dnevnu)."),
			tekstM("Je li dnevna prognoza računata s kišom, piše uz vrijeme izdanja na stranici Prognoze, uz razlog " +
				"kad nije (oborine nisu preuzete, kvota ili mreža). U izvozu izdanja i u Excelu svaka dnevna vrijednost " +
				"nosi oznaku modela: dnevni-1-kisa kad je računata s kišom, dnevni-1 bez nje."),
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
			tekstM(fmt.Sprintf("Prognoza je srednjak dviju procjena, a raspon rasipanje analogija pomnoženo s %s, "+
				"da cilja isti udio kao satni lanac, najmanje 1 cm. Dvije procjene u provjeri griješe u suprotnom "+
				"smjeru, pa srednjak ima manju pristranost od svake zasebno:", brojHRf(prognoza.DnevniRasponMnozitelj, 2))),
			formulaM("Δ̂ₖ = ½ · (Δₖ_reg + Δₖ_kNN),   ĥ_T(t + k) = h_T(t) + Δ̂ₖ ± "+brojHRf(prognoza.DnevniRasponMnozitelj, 2)+" · sₖ",
				`\widehat{\Delta}_k=\frac{1}{2}\left(\Delta_{k,\mathrm{reg}}+\Delta_{k,\mathrm{kNN}}\right),\qquad \hat{h}_T(t+k)=h_T(t)+\widehat{\Delta}_k\pm `+texBroj(brojHRf(prognoza.DnevniRasponMnozitelj, 2))+`\,s_k`),
			tekstM("Uživo je „dan” srednjak 24 sata koji završavaju u satu izdavanja, pa se dnevna prognoza " +
				"obnavlja svaki sat, a ne tek u ponoć; vrijednost za dan k srednjak je 24 sata koji završavaju " +
				"k dana poslije."),
		}},
		{"Koji model daje koji dan", []OdlomakMetode{
			tekstM("Satni lanac seže do 96 sati. Od kojeg dana vrijednost daje dnevni model određeno je " +
				"provjerom na poplavnim valovima, zasebno za svaku postaju: na Dunavu od " + dunav + ", na Dravi od " +
				drava + ". Na Dravi satni lanac dulje pogađa bolje jer nosi istjecanje HE Dubrava i mađarsku " +
				"prognozu Letenyea. Vrijednost iz dnevnog modela na stranici je označena slovom d, a u Excelu " +
				"retkom „model”."),
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
		{"Provjera", []OdlomakMetode{
			tekstM("Svaki dio modela provjeren je na podacima koje pri učenju nije vidio. Dnevni model učen je do " +
				"2012. i mjeren na valovima 2012.–2024. Satni lanac provjeren je na 34 poplavna vala od 2012. " +
				"metodom izostavljanja skupine (četiri skupine valova): svaki val prognozira model namješten bez " +
				"njega i bez dvadesetak dana oko njega. Mjeri se pogreška vrha vala (srednja apsolutna i " +
				"pristranost) na 24, 48, 72 i 96 h, korijen srednje kvadratne pogreške kroz val i udio mjerenja " +
				"unutar raspona. Sve se uspoređuje s postojanošću, s mađarskom prognozom (29 izdanja), s uredskom " +
				"prognozom (55) i s modelom MIKE (48)."),
			tekstM("Primjer: na Botovu lanac koji na vrhu slijedi mađarsku prognozu Letenyea promaši vrh vala " +
				"1.–4. dan prosječno za 22, 29, 41 i 49 cm — s postojanošću na vrhu bilo bi 30, 36, 48 i 60 cm, " +
				"a mađarska prognoza samog Botova 24, 37, 42 i 51 cm."),
		}},
		{"Ograničenja", []OdlomakMetode{
			tekstM("Veliki dravski val od trećeg dana prognoza podcjenjuje, jer nastaje iz kiše koju još nijedna " +
				"postaja ne vidi — ondje vrijedi pratiti gornju granicu raspona. Rad hidroelektrana unaprijed se " +
				"ne zna. Vrijednost izvan svega viđenoga u arhivi model procjenjuje produženjem zadnjeg pravca, " +
				"pa pri rekordnoj vodi valja biti oprezan. Oborine kao ulaz tek se pripremaju."),
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
	m.Letve = letveMetode(data.Tablice, h.postaje(r.Context()), pojasi, promasaji)
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
