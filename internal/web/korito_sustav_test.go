package web

import (
	"math"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"

	"github.com/google/uuid"
)

func probniProfil() models.ProfilKorita {
	return models.ProfilKorita{Datum: "2020-08-18", KotaNule: 80.450,
		Tocke: []models.TockaProfila{
			{Stacionaza: 0, Visina: 86.45},   // lijeva obala, +600 cm
			{Stacionaza: 100, Visina: 78.45}, // dno, -200 cm
			{Stacionaza: 200, Visina: 85.45}, // desna obala, +500 cm
		}}
}

func batinaSKotama() models.Station {
	stara, nova := 80.450, 80.189
	return models.Station{Name: "Batina", ZeroDatum: &stara, ZeroDatumSystem: "TRST",
		ZeroDatumNew: &nova, ZeroDatumNewSystem: "HVRS71"}
}

// Profil je snimljen u starom sustavu, a kote se prikazuju u novom. Razlika se
// računa iz kota nule same letve — nigdje ne stoji upisana kao konstanta, pa
// ne može zastarjeti kad se kota ispravi.
func TestPresjekPrikazujeKoteUNovomSustavu(t *testing.T) {
	p := probniProfil()
	st := batinaSKotama()
	c := crtajKoritoP(p, 300, sirokoKoritoM.uSustavu(st))
	if c == nil {
		t.Fatal("presjek se nije nacrtao")
	}
	if c.Sustav != "HVRS71" {
		t.Errorf("sustav prikaza: %q", c.Sustav)
	}
	if math.Abs(c.PomakSustava-(-0.261)) > 1e-9 {
		t.Errorf("pomak %.4f, očekivan -0,261", c.PomakSustava)
	}
	// vodna ploha pri +300 cm: 80,450 + 3,00 = 83,450 TRST → 83,189 HVRS71
	if math.Abs(c.KotaVode-83.189) > 1e-9 {
		t.Errorf("kota vode %.4f, očekivana 83,189", c.KotaVode)
	}
	// dno 78,450 TRST → 78,189 HVRS71
	if math.Abs(c.Dno-78.189) > 1e-9 {
		t.Errorf("dno %.4f, očekivano 78,189", c.Dno)
	}
	// dubina je razlika i pomak je ne smije dirati
	if math.Abs(c.DubinaM-5.0) > 1e-9 {
		t.Errorf("dubina %.4f, očekivana 5,000 — pomak sustava je ne mijenja", c.DubinaM)
	}
	// centimetri na letvi su mjera od nule i ostaju isti u oba sustava
	if c.DnoCm != -200 || c.LijevaCm != 600 || c.DesnaCm != 500 {
		t.Errorf("centimetri na letvi su se pomaknuli: dno %d, l. obala %d, d. obala %d",
			c.DnoCm, c.LijevaCm, c.DesnaCm)
	}
	if c.NazivOsi() != "m n.m. HVRS71" {
		t.Errorf("naziv osi: %q", c.NazivOsi())
	}
}

// Podjele moraju biti okrugle u sustavu u kojem se ispisuju. Bez toga bi crta
// s natpisom „80" stajala na 80,26 stvarne visine.
func TestPodjelePresjekaSuOkrugleUPrikazanomSustavu(t *testing.T) {
	c := crtajKoritoP(probniProfil(), 300, sirokoKoritoM.uSustavu(batinaSKotama()))
	if len(c.KoteY) < 3 {
		t.Fatalf("premalo podjela: %d", len(c.KoteY))
	}
	for _, k := range c.KoteY {
		if math.Abs(k.Kota-math.Round(k.Kota)) > 1e-6 {
			t.Errorf("podjela %.4f nije okrugla u prikazanom sustavu", k.Kota)
		}
		// uz svaku kotu stoji i vodostaj koji joj odgovara; provjera je da se
		// iz njega vrati ista visina
		natrag := 80.450 + float64(k.Cm)/100 - 0.261
		if math.Abs(natrag-k.Kota) > 0.006 {
			t.Errorf("kota %.3f i vodostaj %d cm ne govore o istoj visini (%.3f)", k.Kota, k.Cm, natrag)
		}
	}
}

// Letva bez novog sustava nema iz čega izračunati razliku, pa se kote
// prikazuju kako su snimljene. Nagađanje bi pomaknulo cijeli presjek.
func TestBezNovogSustavaPresjekOstajeKakoJeSnimljen(t *testing.T) {
	stara := 80.450
	st := models.Station{ZeroDatum: &stara, ZeroDatumSystem: "TRST"}
	c := crtajKoritoP(probniProfil(), 300, sirokoKoritoM.uSustavu(st))
	if c.PomakSustava != 0 || c.Sustav != "" {
		t.Errorf("pomak %.4f, sustav %q — bez novog sustava se ne smije pomicati", c.PomakSustava, c.Sustav)
	}
	if math.Abs(c.KotaVode-83.450) > 1e-9 {
		t.Errorf("kota vode %.4f, očekivana 83,450", c.KotaVode)
	}
	if c.NazivOsi() != "m n.m." {
		t.Errorf("naziv osi: %q", c.NazivOsi())
	}
}

// Ista se visina čita s dvije strane: lijevo kota, desno vodostaj koji bi joj
// na letvi odgovarao. Bez druge osi se s presjeka ne da očitati koji vodostaj
// koju visinu korita doseže.
func TestPresjekImaObjeOsi(t *testing.T) {
	c := crtajKoritoP(probniProfil(), 300, sirokoKoritoM.uSustavu(batinaSKotama()))
	if !c.DvijeOsi {
		t.Fatal("presjek na kartici mora imati obje osi")
	}
	if c.NazivDesneOsi() != "cm na letvi" {
		t.Errorf("desna os: %q", c.NazivDesneOsi())
	}
	if c.DesnaOsCrtaX() >= float64(c.Sirina) {
		t.Error("vodoravne crte moraju stati prije brojki desne osi")
	}
	if c.DesnaOsX() <= c.DesnaOsCrtaX() {
		t.Error("brojke desne osi stoje desno od kraja crta")
	}

	// uz graf očitanja je obrnuto: lijevo centimetri, desno kote
	u := crtajKoritoP(probniProfil(), 300, sirokoKorito.uSustavu(batinaSKotama()))
	if !u.OsUCm || u.NazivOsi() != "cm na letvi" || u.NazivDesneOsi() != "m n.m. HVRS71" {
		t.Errorf("uz graf: lijevo %q, desno %q", u.NazivOsi(), u.NazivDesneOsi())
	}
	if len(u.KoteY) == 0 {
		t.Fatal("nema podjela")
	}
	if !strings.Contains(u.KoteY[0].DesniIspis(), ",") {
		t.Errorf("desni ispis uz graf mora biti kota u metrima: %q", u.KoteY[0].DesniIspis())
	}
}

// Presjek na očitanjima mora biti isti kao na kartici letve: spojen iz svih
// snimaka. Prije se ondje birala jedna, pa je korito ispadalo niže nego što
// jest — Batinina snimka iz 2020. lijevu obalu presijeca na +166 cm, a
// spojena seže do +966. Vraćanje na jednu snimku ne bi ništa srušilo, samo
// bi tiho odrezalo pola korita.
func TestOcitanjaCrtajuSpojeniPresjek(t *testing.T) {
	stara := models.ProfilKorita{Datum: "2010-03-22", KotaNule: 80.45,
		Tocke: []models.TockaProfila{
			{Stacionaza: 0, Visina: 90.11}, // lijeva obala do vrha, samo u staroj
			{Stacionaza: 105, Visina: 82.11},
			{Stacionaza: 200, Visina: 73.20},
			{Stacionaza: 500, Visina: 89.25},
		}}
	nova := models.ProfilKorita{Datum: "2020-08-18", KotaNule: 80.45, PomakM: 104.5,
		Tocke: []models.TockaProfila{
			{Stacionaza: 0, Visina: 82.11}, // presječena na +166 cm
			{Stacionaza: 95, Visina: 72.81},
			{Stacionaza: 294, Visina: 89.24},
		}}
	spoj := models.SpojiProfile([]models.ProfilKorita{stara, nova})
	if !spoj.Spojen() {
		t.Fatal("priprema: profil nije spojen")
	}
	st := batinaSKotama()

	spojeni := crtajKoritoP(spoj, 300, sirokoKorito.uSustavu(st))
	sama := crtajKoritoP(nova, 300, sirokoKorito.uSustavu(st))
	if spojeni == nil || sama == nil {
		t.Fatal("presjek se nije nacrtao")
	}
	if spojeni.LijevaCm <= sama.LijevaCm {
		t.Errorf("spojeni presjek mora sezati više uz lijevu obalu: %d naspram %d",
			spojeni.LijevaCm, sama.LijevaCm)
	}
	// dno ostaje iz novije snimke — novija ima prednost gdje seže
	if spojeni.DnoCm > sama.DnoCm {
		t.Errorf("dno spojenog (%d cm) nije iz novije snimke (%d cm)", spojeni.DnoCm, sama.DnoCm)
	}
}

// Kota vodne plohe stoji na sredini vode. Kad je voda došla do praga —
// upravo tada se presjek i gleda — natpis praga je na istoj visini, pa se
// dva natpisa preklope i nijedan se ne može pročitati. Tada se kota vode
// mora pomaknuti.
func TestKotaVodeSeMicePredNatpisomPraga(t *testing.T) {
	st := batinaSKotama()
	pragovi := []PragKorita{
		{Cm: 300, Label: "pripremno", Class: "prep"},
		{Cm: 500, Label: "redovna", Class: "regular"},
		{Cm: 650, Label: "izvanredna", Class: "emerg"},
		{Cm: 800, Label: "izvanredno stanje", Class: "crit"},
	}
	// uski crtež: veći font, pa se natpisi sudare i pri srednjoj vodi
	c := crtajKoritoP(probniProfil(), 500, uskoKorito.sKoritom(pragovi, 100, 500).uSustavu(st))
	if c == nil || !c.ImaVode {
		t.Fatal("presjek bez vode")
	}
	if c.VodaNatpis == "" {
		t.Fatal("kota vodne plohe se ne ispisuje")
	}
	if !strings.Contains(c.VodaNatpis, "500 cm") || !strings.Contains(c.VodaNatpis, "85,19 m") {
		t.Errorf("natpis vode: %q — mora nositi očitanje i kotu", c.VodaNatpis)
	}

	// natpis praga na istoj visini mora natjerati kotu vode da se makne
	font := 16.0
	sirina := sirinaNatpisa(c.VodaNatpis, font)
	var od, do float64
	switch c.VodaNatpisSidro {
	case "start":
		od, do = c.VodaNatpisX, c.VodaNatpisX+sirina
	case "end":
		od, do = c.VodaNatpisX-sirina, c.VodaNatpisX
	default:
		od, do = c.VodaNatpisX-sirina/2, c.VodaNatpisX+sirina/2
	}
	for _, pr := range c.Pragovi {
		if math.Abs(pr.Y-c.YVode) > font+3 {
			continue
		}
		kraj := c.NatpisX() + sirinaNatpisa(pr.Label+" "+brojHR(pr.Cm), font)
		if kraj > od && c.NatpisX() < do {
			t.Errorf("kota vode (%.0f–%.0f) preklapa natpis %q (%.0f–%.0f)",
				od, do, pr.Label, c.NatpisX(), kraj)
		}
	}
	// i ne smije izaći iz crteža
	if od < 0 || do > float64(c.Sirina) {
		t.Errorf("natpis vode izlazi iz crteža: %.0f–%.0f od %d", od, do, c.Sirina)
	}
}

// Natpisi pragova ne smiju stajati uz samu os: ondje na strmom profilu leži
// obala, pa natpis pada na crtu korita. Zato su pomaknuti u korito, ali ne
// do sredine — ondje stoji kota vode.
func TestNatpisiPragovaNisuUzSamuOs(t *testing.T) {
	c := crtajKoritoP(probniProfil(), 300, sirokoKoritoM.uSustavu(batinaSKotama()))
	if c.NatpisX() <= c.Lijevo+20 {
		t.Errorf("natpis praga na %.0f, os na %.0f — prelijeva se preko obale", c.NatpisX(), c.Lijevo)
	}
	sredina := c.Lijevo + c.SirinaPlohe/2
	if c.NatpisX() >= sredina {
		t.Errorf("natpis praga na %.0f je u sredini (%.0f), gdje stoji kota vode", c.NatpisX(), sredina)
	}
}

// Predložak koji pukne pri izvršavanju ne javlja grešku na stranici nego je
// prekine na mjestu pucanja: sve ispod tiho nestane. Tako je krivi pomoćnik u
// natpisu ispod presjeka — brojHRd traži pokazivač, a razlika sustava je
// obična brojka — odnio pola kartice letve, a nijedan test to nije primijetio
// jer su svi presjek crtali izravno, bez iscrtavanja stranice.
//
// Zato se ovdje iscrtava bogato popunjena kartica i traži da stigne do
// zadnjeg odjeljka i do zatvaranja dokumenta.
func TestKarticaLetveStigneDoKraja(t *testing.T) {
	st := batinaSKotama()
	st.ID = uuid.New()
	st.Code = "batina"
	st.Watercourse = "Dunav"
	p := probniProfil()

	html := iscrtaj(t, "station_detail.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, PragoviKote: pragoviUKotama(st),
		Profili:   []models.ProfilKorita{p},
		Profil:    &p,
		Zadnji:    &models.HidroTocka{Kad: time.Now(), Vrijednost: 300},
		Crtez:     crtajKoritoP(p, 300, sirokoKoritoM.uSustavu(st)),
		CrtezUzak: crtajKoritoP(p, 300, uskoKoritoM.uSustavu(st)),
	})

	// Presjek korita više nije na kartici — stoji uz očitanja, gdje se i čita.
	if strings.Contains(html, "Korito i voda u njemu") {
		t.Error("presjek se vratio na karticu; njegovo je mjesto uz očitanja")
	}
	// ono što na kartici jest, mora doći do kraja
	if !strings.Contains(html, "Mjerodavna za dionice") {
		t.Error("stranica je prekinuta prije zadnjeg odjeljka — predložak je pukao usred izvršavanja")
	}
	if !strings.HasSuffix(strings.TrimSpace(html), "</html>") {
		t.Error("stranica ne završava zatvaranjem dokumenta")
	}
}

// Protok stoji u istom retku kao i prag na koji se odnosi. Prije je bio u
// zasebnom popisu ispod tablice, pa se naziv stupnja i vodostaj ponavljao, a
// čitatelj ih je morao spajati očima.
//
// Veže se po centimetrima, ne po nazivu: naziv je tekst za prikaz i mijenja
// se, a prag je brojka. Vezivanje po nazivu puklo bi tiho — protok bi
// jednostavno nestao iz tablice.
func TestProtokStojiUzSvojPrag(t *testing.T) {
	cm := func(v int) *int { return &v }
	st := batinaSKotama()
	st.Prep = models.Threshold{Cm: cm(300)}
	st.Regular = models.Threshold{Cm: cm(500)}
	st.Emergency = models.Threshold{Cm: cm(650)}
	st.State = models.Threshold{Cm: cm(800)}

	kote := pragoviUKotama(st)
	q := []PragProtok{
		{Naziv: "Pripremno stanje", Cm: 300, Q: 2749},
		{Naziv: "Redovna obrana", Cm: 500, Q: 4124},
		// izvanredna namjerno izostavljena: prag izvan raspona krivulje
		{Naziv: "Izvanredno stanje", Cm: 800, Q: 8300},
	}
	spojeno := sProtokom(kote, q)
	if !imaProtok(spojeno) {
		t.Fatal("tablica mora dobiti stupac protoka")
	}
	po := map[int]*float64{}
	for _, p := range spojeno {
		po[p.Cm] = p.Q
	}
	for cmv, want := range map[int]float64{300: 2749, 500: 4124, 800: 8300} {
		if po[cmv] == nil || *po[cmv] != want {
			t.Errorf("prag %d cm: protok %v, očekivan %.0f", cmv, po[cmv], want)
		}
	}
	if po[650] != nil {
		t.Errorf("prag izvan raspona krivulje ne smije dobiti protok: %v", *po[650])
	}

	// letva bez krivulje: tablica ostaje bez stupca, ne s praznim
	bez := sProtokom(pragoviUKotama(st), nil)
	if imaProtok(bez) {
		t.Error("bez krivulje tablica ne smije tražiti stupac protoka")
	}
}

// Kartica mora protok pokazati u tablici, a ne više u zasebnom popisu ispod —
// inače se isti podatak čita dvaput.
func TestProtokStojiUzVodostajIstogStupnja(t *testing.T) {
	cm := func(v int) *int { return &v }
	st := batinaSKotama()
	st.ID = uuid.New()
	st.Code = "batina"
	st.Prep = models.Threshold{Cm: cm(300)}
	st.Regular = models.Threshold{Cm: cm(500)}
	q := []PragProtok{{Naziv: "Pripremno stanje", Cm: 300, Q: 2749},
		{Naziv: "Redovna obrana", Cm: 500, Q: 4124}}
	kote := sProtokom(pragoviUKotama(st), q)

	html := iscrtaj(t, "station_detail.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, PragoviKote: kote, PragoviQ: q, ImaProtok: imaProtok(kote),
	})
	for _, want := range []string{
		`<div class="prag-kartica prep">`, `<span class="threshold-pill prep">300 cm</span>`,
		`<span class="threshold-pill prep protok">2.749 m³/s</span>`,
		`<div class="prag-kartica regular">`, "4.124 m³/s",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("na kartici stupnja nema %q", want)
		}
	}
	if strings.Contains(html, "Isti stupnjevi u protoku, po krivulji koja danas vrijedi") {
		t.Error("zaseban popis protoka ostao je uz pragove — podatak se čita dvaput")
	}
}

// Kota nule je temelj svake apsolutne brojke na kartici: iz nje se računaju i
// pragovi u koti i kota vodne plohe. Stajala je u zasebnom odjeljku ispod
// povratnih vodostaja, gdje je operativa nije nalazila — sad je uz pragove,
// odmah iznad tablice koja se iz nje izvodi.
func TestKotaNuleImaSvojOdjeljakIzaPragova(t *testing.T) {
	cm := func(v int) *int { return &v }
	st := batinaSKotama()
	st.ID = uuid.New()
	st.Code = "batina"
	st.Prep = models.Threshold{Cm: cm(300)}
	st.Regular = models.Threshold{Cm: cm(500)}

	html := iscrtaj(t, "station_detail.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, PragoviKote: pragoviUKotama(st),
	})
	pragovi := strings.Index(html, "Pragovi obrane od poplava")
	stupanj := strings.Index(html, `<div class="prag-kartica prep">`)
	kota := strings.Index(html, "Kota nule vodomjera")
	if kota < 0 {
		t.Fatal("kota nule se ne prikazuje")
	}
	// vlastiti odjeljak, odmah iza pragova iz kojih se svaka kota računa
	if !(pragovi < stupanj && stupanj < kota) {
		t.Errorf("kota nule ne stoji odmah iza pragova (pragovi %d, stupanj %d, kota %d)",
			pragovi, stupanj, kota)
	}
	for _, want := range []string{"80,189 m", "80,450 m"} {
		if !strings.Contains(html, want) {
			t.Errorf("nema kote %q", want)
		}
	}
}

// Letva bez upisane kote to mora reći, a ne prešutjeti: bez kote se pragovi ne
// mogu izraziti u apsolutnoj visini i nivelman na terenu nema od čega krenuti.
func TestLetvaBezKoteNuleToKaze(t *testing.T) {
	cm := func(v int) *int { return &v }
	st := models.Station{ID: uuid.New(), Name: "Dalj", Code: "dalj",
		Prep: models.Threshold{Cm: cm(300)}}
	html := iscrtaj(t, "station_detail.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, PragoviKote: pragoviUKotama(st),
	})
	if !strings.Contains(html, "Kota nule vodomjera") {
		t.Error("kota nule mora stajati i kad nije upisana")
	}
	if strings.Count(html, "nije upisana") < 2 {
		t.Error("obje kote koje nedostaju moraju to reći")
	}
}

// Kartica letve odgovara na pitanje što letva jest, historijat na pitanje što
// se dogodilo. Dok je oboje stajalo na istoj stranici, dežurni je do pragova
// dolazio kroz sedam odjeljaka povijesti. Podjela se lako izgubi — dovoljno je
// da netko vrati jedan odjeljak natrag — pa se traži izričito.
func TestKarticaIHistorijatSuRazdvojeni(t *testing.T) {
	cm := func(v int) *int { return &v }
	st := batinaSKotama()
	st.ID, st.Code, st.Name = uuid.New(), "batina", "Batina"
	st.Prep = models.Threshold{Cm: cm(300)}
	st.Regular = models.Threshold{Cm: cm(500)}
	st.Extremes = []models.StationExtreme{
		{Kind: models.ExtremeMax, LevelCm: cm(775), OnDate: "2013-06-14", Quality: models.QualityMeasured}}
	st.ReturnLevels = []models.StationReturnLevel{{Years: 100, LevelCm: cm(792)}}
	st.ZeroDatumHistory = []models.ZeroDatumChange{{ValidFrom: "2001-03-09", System: "TRST"}}

	podaci := StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, PragoviKote: pragoviUKotama(st),
		Episodes: []models.DefenseEpisode{{SectionCode: "B.34.1", Phase: models.PhasePrep}},
	}
	kartica := iscrtaj(t, "station_detail.html", podaci)
	povijest := iscrtaj(t, "station_history.html", podaci)

	// Na kartici: što letva jest.
	for _, want := range []string{"Pragovi obrane od poplava", "Kota nule vodomjera", "80,189 m", "Historijat"} {
		if !strings.Contains(kartica, want) {
			t.Errorf("na kartici nema %q", want)
		}
	}
	// Na kartici NE: što se dogodilo.
	for _, ne := range []string{
		"Povratni vodostaji", "Promjene kote nule",
		"Vodostaj i protok", "Valovi obrane u nizu", "Obrane vođene po ovoj letvi",
	} {
		if strings.Contains(kartica, ne) {
			t.Errorf("odjeljak %q vratio se na karticu; njegovo je mjesto u historijatu", ne)
		}
	}
	// U historijatu: sve povijesno, i put natrag na karticu.
	for _, want := range []string{
		"Povratni vodostaji", "Promjene kote nule",
		"Valovi obrane u nizu", "Obrane vođene po ovoj letvi", "Kartica letve",
	} {
		if !strings.Contains(povijest, want) {
			t.Errorf("u historijatu nema %q", want)
		}
	}
	// U historijatu NE: pragovi i kota nule, to je posao kartice.
	for _, ne := range []string{"Pragovi obrane od poplava", "Kota nule vodomjera", "Zabilježeni ekstremi"} {
		if strings.Contains(povijest, ne) {
			t.Errorf("%q se preselilo u historijat; to je posao kartice", ne)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(povijest), "</html>") {
		t.Error("historijat ne završava zatvaranjem dokumenta")
	}
}

// Letva bez ijednog povijesnog podatka ne smije dati stranicu praznih okvira.
func TestHistorijatPrazneLetveToKaze(t *testing.T) {
	st := models.Station{ID: uuid.New(), Name: "Dalj", Code: "dalj"}
	podaci := StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, CanEdit: true,
	}
	if !historijatPrazan(podaci) {
		t.Fatal("letva bez ijednog podatka nije prepoznata kao prazna")
	}
	podaci.HistorijatPrazan = true
	html := iscrtaj(t, "station_history.html", podaci)
	if !strings.Contains(html, "još nema zabilježene povijesti") {
		t.Error("prazan historijat mora reći da podataka nema")
	}
	if strings.Contains(html, "<table") {
		t.Error("prazan historijat ne smije crtati prazne tablice")
	}
	// jedna poruka, ne dvije: valovi ne ponavljaju istu vijest ispod nje
	if strings.Contains(html, "valovi se ne mogu izračunati") {
		t.Error("uz praznu poruku stoji i poruka valova")
	}
	// i vodi na obrazac koji te podatke doista prima
	if !strings.Contains(html, "/historijat/uredi") {
		t.Error("prazan historijat ne nudi obrazac historijata")
	}
	if strings.Contains(html, `href="/stations/`+st.ID.String()+`/edit">Uredi</a>`) {
		t.Error("prazan historijat vodi na obrazac kartice, koji te podatke ne prima")
	}
}

// Ekstremi su na kartici; letva koja ih ima, a nema ništa od historijata, i
// dalje ima prazan historijat.
func TestEkstremiNeCineHistorijatPunim(t *testing.T) {
	cm := func(v int) *int { return &v }
	st := models.Station{ID: uuid.New(), Name: "Dalj", Code: "dalj",
		Extremes: []models.StationExtreme{{Kind: models.ExtremeMax, LevelCm: cm(514)}}}
	if !historijatPrazan(StationPageData{Station: st}) {
		t.Error("ekstremi s kartice čine historijat punim")
	}
}

// Obrazac kartice ne poznaje ekstreme ni povratne vodostaje, a obrazac
// historijata ne poznaje pragove ni kotu nule. Dok je svaki gradio cijelu
// postaju, spremanje kartice tiho bi obrisalo ekstreme — stigli bi prazni i
// zapisali se kao prazni, bez ijedne poruke. Zato svaki prenosi samo svoja
// polja na postojeći zapis.
func TestSpremanjeJednogObrascaNeBriseTudaPolja(t *testing.T) {
	cm := func(v int) *int { return &v }
	kota := func(v float64) *float64 { return &v }
	postojeca := models.Station{
		Name: "Batina", Code: "batina",
		ZeroDatum: kota(80.450), ZeroDatumSystem: "TRST",
		ZeroDatumNew: kota(80.189), ZeroDatumNewSystem: "HVRS71",
		Prep: models.Threshold{Cm: cm(300)}, Regular: models.Threshold{Cm: cm(500)},
		Record: models.Threshold{Cm: cm(775)},
		Extremes: []models.StationExtreme{
			{Kind: models.ExtremeMax, LevelCm: cm(795), OnDate: "1965-06-24",
				Quality: models.QualityReconstructed, Source: "postaja Bezdan"}},
		ReturnLevels: []models.StationReturnLevel{{Years: 100, LevelCm: cm(792)}},
		ZeroDatumHistory: []models.ZeroDatumChange{
			{ValidFrom: "2001-03-09", Datum: kota(80.450), System: "TRST"}},
	}

	// Obrazac kartice: mijenja naziv i prag, o povijesnim poljima ne zna ništa.
	kartica := stationForm{Obrazac: obrazacKartica, Name: "Batina (novo)", Prep: "+320",
		Record: "+775", ZeroDatum: "80,450", ZeroDatumNew: "80,189",
		Extremes: `[{"kind":"MAX","level_cm":"795","on_date":"1965-06-24","quality":"REKONSTRUIRANO","source":"postaja Bezdan"}]`}
	st := postojeca
	kartica.primijeni(&st)
	if st.Name != "Batina (novo)" || st.Prep.Cm == nil || *st.Prep.Cm != 320 {
		t.Errorf("kartica nije prenijela svoja polja: %q %v", st.Name, st.Prep.Cm)
	}
	if len(st.ReturnLevels) != 1 || len(st.ZeroDatumHistory) != 1 {
		t.Errorf("spremanje kartice pojelo je polja historijata: povratnih %d, promjena kote %d",
			len(st.ReturnLevels), len(st.ZeroDatumHistory))
	}
	if st.Record.Cm == nil || *st.Record.Cm != 775 {
		t.Error("spremanje kartice pojelo je najviši zabilježeni")
	}

	// Obrazac historijata: mijenja ekstreme, o pragovima i koti ne zna ništa.
	povijest := stationForm{Obrazac: obrazacHistorijat,
		ReturnLevels: `[{"years":"50","level_cm":"776"}]`}
	st2 := postojeca
	povijest.primijeni(&st2)
	if len(st2.ReturnLevels) != 1 || st2.ReturnLevels[0].Years != 50 {
		t.Errorf("historijat nije prenio povratne vodostaje: %+v", st2.ReturnLevels)
	}
	if len(st2.Extremes) != 1 || *st2.Extremes[0].LevelCm != 795 {
		t.Error("spremanje historijata pojelo je ekstreme; oni su na kartici")
	}
	if st2.Name != "Batina" || st2.Prep.Cm == nil || *st2.Prep.Cm != 300 {
		t.Errorf("spremanje historijata pojelo je naziv ili prag: %q %v", st2.Name, st2.Prep.Cm)
	}
	if st2.ZeroDatumNew == nil || *st2.ZeroDatumNew != 80.189 {
		t.Errorf("spremanje historijata pojelo je kotu nule: %v", st2.ZeroDatumNew)
	}
	if st2.Record.Cm == nil || *st2.Record.Cm != 775 {
		t.Error("spremanje historijata pojelo je najviši zabilježeni; on je na kartici")
	}
}

// Svaki obrazac mora ponuditi sve što njegova stranica prikazuje, i ništa
// tuđe. Polje koje se prikazuje a ne da se urediti nigdje ne javlja da
// nedostaje — jednostavno se ne može promijeniti.
func TestObrasciPokrivajuSvojeStranice(t *testing.T) {
	cm := func(v int) *int { return &v }
	kota := func(v float64) *float64 { return &v }
	st := models.Station{ID: uuid.New(), Name: "Batina", Code: "batina",
		ZeroDatum: kota(80.450), ZeroDatumNew: kota(80.189),
		Prep: models.Threshold{Cm: cm(300)}}
	podaci := StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, IsEdit: true,
	}
	kartica := iscrtaj(t, "station_form.html", podaci)
	povijest := iscrtaj(t, "station_history_form.html", podaci)

	// Kartica prikazuje: naziv, vodotok, stacionažu, vodno područje, položaj,
	// pragove, kotu nule, napomenu, oznaku provjere — sve mora biti u obrascu.
	for _, polje := range []string{"name", "watercourse", "stationing", "water_area",
		"latitude", "longitude", "prep", "regular", "emergency", "state", "record",
		"extremes", "zero_datum", "zero_datum_new", "notes", "needs_review"} {
		if !strings.Contains(kartica, `name="`+polje+`"`) {
			t.Errorf("obrazac kartice nema polje %q, a stranica ga prikazuje", polje)
		}
	}
	// a povijesna polja ne dira
	for _, polje := range []string{"return_levels", "zero_datum_history"} {
		if strings.Contains(kartica, `name="`+polje+`"`) {
			t.Errorf("obrazac kartice ureduje %q, a to je posao historijata", polje)
		}
		if !strings.Contains(povijest, `name="`+polje+`"`) {
			t.Errorf("obrazac historijata nema polje %q", polje)
		}
	}
	for _, polje := range []string{"prep", "latitude", "zero_datum_new", "record", "extremes"} {
		if strings.Contains(povijest, `name="`+polje+`"`) {
			t.Errorf("obrazac historijata ureduje %q, a to je posao kartice", polje)
		}
	}
	// oba moraju reći kojem obrascu pripadaju, inače spremanje briše tuđe
	if !strings.Contains(kartica, `name="obrazac" value="kartica"`) {
		t.Error("obrazac kartice se ne predstavlja")
	}
	if !strings.Contains(povijest, `name="obrazac" value="historijat"`) {
		t.Error("obrazac historijata se ne predstavlja")
	}
}

// Kartica mora biti samodostatna: sve što na njoj piše vidi se i ureduje bez
// odlaska u historijat. Najviši zabilježeni je to pravilo i prekršio — stajao
// je u obrascu historijata, koji ga uopće ne prikazuje, a prikazivale su ga tri
// druge stranice koje ga nisu mogle urediti.
func TestKarticaJeSamodostatna(t *testing.T) {
	cm := func(v int) *int { return &v }
	kota := func(v float64) *float64 { return &v }
	st := models.Station{ID: uuid.New(), Name: "Batina", Code: "batina", Watercourse: "Dunav",
		Stationing: "rkm 1.425+000", WaterArea: "Sliv Drave i Dunava",
		ZeroDatum: kota(80.450), ZeroDatumNew: kota(80.189),
		ZeroDatumSystem: "TRST", ZeroDatumNewSystem: "HVRS71",
		Latitude: kota(45.845833), Longitude: kota(18.854722),
		Prep: models.Threshold{Cm: cm(300)}, Regular: models.Threshold{Cm: cm(500)},
		Emergency: models.Threshold{Cm: cm(650)}, State: models.Threshold{Cm: cm(800)},
		Record: models.Threshold{Cm: cm(775)}, Notes: "letva obnovljena 2001."}
	podaci := StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, PragoviKote: pragoviUKotama(st), CanEdit: true, CanRecord: true, IsEdit: true,
	}
	kartica := iscrtaj(t, "station_detail.html", podaci)
	obrazac := iscrtaj(t, "station_form.html", podaci)

	// Svaka vrijednost s kartice mora se naći i u njezinu obrascu.
	for _, s := range []struct{ naVidjelu, uObrascu string }{
		{"Batina", `name="name"`},
		{"Dunav", `name="watercourse"`},
		{"rkm 1.425&#43;000", `name="stationing"`}, // predložak plus ispisuje kao &#43;
		{"Sliv Drave i Dunava", `name="water_area"`},
		{"300 cm", `name="prep"`},
		{"800 cm", `name="state"`},
		{"80,189 m", `name="zero_datum_new"`},
		{"80,450 m", `name="zero_datum"`},
		{"letva obnovljena 2001.", `name="notes"`},
		{"45,845833", `name="latitude"`},
	} {
		if !strings.Contains(kartica, s.naVidjelu) {
			t.Errorf("kartica ne prikazuje %q", s.naVidjelu)
		}
		if !strings.Contains(obrazac, s.uObrascu) {
			t.Errorf("kartica prikazuje %q, a obrazac nema %s", s.naVidjelu, s.uObrascu)
		}
	}

	// Najviši zabilježeni ne stoji među pragovima: cijela je krajnost u tablici
	// ekstrema, s datumom i podrijetlom. Ureduje se i dalje s kartice, jer se
	// prikazuje u popisu letvi i na kartici dionice.
	if strings.Contains(kartica, "prag-kartica rekord") {
		t.Error("najviši zabilježeni se vratio među pragove; on je u tablici ekstrema")
	}
	if !strings.Contains(obrazac, `name="record"`) {
		t.Error("najviši zabilježeni se ne da urediti ni s jedne stranice")
	}

	// Pragovi idu ispred karte: letva se ne pomiče, a pragovi su ono po što se dolazi.
	pragovi := strings.Index(kartica, "Pragovi obrane od poplava")
	polozaj := strings.Index(kartica, "Položaj letve")
	if pragovi < 0 || polozaj < 0 || pragovi > polozaj {
		t.Errorf("karta stoji ispred pragova (pragovi %d, položaj %d)", pragovi, polozaj)
	}

	// Rijetke radnje ne stoje u zaglavlju uz svakodnevne.
	zaglavlje := kartica[:strings.Index(kartica, "Pragovi obrane od poplava")]
	for _, rijetka := range []string{"Generiraj izvješće", "Obriši"} {
		if strings.Contains(zaglavlje, rijetka) {
			t.Errorf("%q je u zaglavlju; rijetke radnje idu na dno", rijetka)
		}
	}
	for _, cesta := range []string{"Upiši očitanje", "Očitanja", "Historijat", "Uredi"} {
		if !strings.Contains(zaglavlje, cesta) {
			t.Errorf("%q nije u zaglavlju", cesta)
		}
	}
	if !strings.Contains(kartica, "kartica-podnozje") {
		t.Error("nema podnožja s rijetkim radnjama")
	}
}

// Obrazac mora slijediti redoslijed stranice koju uređuje: uređivač koji je
// upravo gledao karticu traži polja onim redom kojim ih je vidio. Redoslijed
// se lako razidе — dovoljno je premjestiti jedan odjeljak na stranici a
// zaboraviti obrazac, i ništa ne pukne, samo zbunjuje.
func TestObrazacPratiRedoslijedKartice(t *testing.T) {
	cm := func(v int) *int { return &v }
	kota := func(v float64) *float64 { return &v }
	st := models.Station{ID: uuid.New(), Name: "Batina", Code: "batina",
		ZeroDatum: kota(80.450), ZeroDatumNew: kota(80.189),
		Latitude: kota(45.845833), Longitude: kota(18.854722),
		Prep: models.Threshold{Cm: cm(300)}, Record: models.Threshold{Cm: cm(775)},
		Extremes: []models.StationExtreme{
			{Kind: models.ExtremeMax, LevelCm: cm(775), OnDate: "2013-06-14", Quality: models.QualityMeasured}},
		Notes: "napomena"}
	podaci := StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, PragoviKote: pragoviUKotama(st), CanEdit: true, IsEdit: true,
	}
	kartica := iscrtaj(t, "station_detail.html", podaci)
	obrazac := iscrtaj(t, "station_form.html", podaci)

	// Odjeljci koji postoje na obje strane, redom kojim ih kartica prikazuje.
	redom := []string{
		"Pragovi obrane od poplava",
		"Kota nule vodomjera",
		"Zabilježeni ekstremi",
		"Položaj letve",
		"Napomena",
	}
	// Traže se NASLOVI, ne puke riječi: „Napomena" je i naslov stupca u
	// uređivaču ekstrema, pa bi obična pretraga našla krivo mjesto.
	polozaji := func(html, oznaka string) []int {
		out := make([]int, len(redom))
		for i, naslov := range redom {
			out[i] = strings.Index(html, naslov+"</"+oznaka+">")
			if out[i] < 0 {
				t.Fatalf("odjeljak %q ne postoji kao naslov <%s>", naslov, oznaka)
			}
		}
		return out
	}
	naKartici, uObrascu := polozaji(kartica, "h2"), polozaji(obrazac, "h3")
	for i := 1; i < len(redom); i++ {
		if naKartici[i] < naKartici[i-1] {
			t.Errorf("kartica: %q stoji ispred %q", redom[i], redom[i-1])
		}
		if uObrascu[i] < uObrascu[i-1] {
			t.Errorf("obrazac: %q stoji ispred %q, a kartica ih prikazuje obrnuto",
				redom[i], redom[i-1])
		}
	}
}

// Brisanje letve traži upozorenje koje govori istinu. Očitanja upisana na letvi
// NEMAJU vezu s kaskadnim brisanjem — ostaju u bazi bez letve. Upozorenje to
// mora reći, a skripta treba brojke: bez njih bi pisalo općenito, i nitko ne bi
// znao koliko toga visi o toj letvi.
func TestBrisanjeLetveNosiPodatkeZaUpozorenje(t *testing.T) {
	cm := func(v int) *int { return &v }
	st := models.Station{ID: uuid.New(), Name: "Batina", Code: "batina",
		Prep: models.Threshold{Cm: cm(300)}, SectionCodes: []string{"B.34.1", "B.34.2"}}
	html := iscrtaj(t, "station_detail.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, PragoviKote: pragoviUKotama(st), CanEdit: true,
		BrojOcitanja: 40,
	})
	for _, want := range []string{
		"data-brisanje-letve",
		`data-naziv="Batina"`,
		`data-ocitanja="40"`,
		`data-dionice="2"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("obrazac brisanja nema %q — upozorenje bi ostalo bez te brojke", want)
		}
	}
	// Brisanje ne smije biti obična poveznica ni gumb bez potvrde.
	if strings.Contains(html, `href="/api/stations/delete"`) {
		t.Error("brisanje je poveznica; mora ići kroz obrazac s potvrdom")
	}
	// Oba gumba podnožja iste veličine: nijedan nema btn-sm.
	podnozje := html[strings.Index(html, "kartica-podnozje"):]
	podnozje = podnozje[:strings.Index(podnozje, "</div>")+6]
	if strings.Contains(podnozje, "btn-sm") {
		t.Error("gumbi podnožja nisu iste veličine")
	}
	if !strings.Contains(podnozje, "btn-danger") {
		t.Error("brisanje nije označeno kao opasna radnja")
	}

	// Bez prava se ne nudi.
	bez := iscrtaj(t, "station_detail.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{},
		Station:     st, PragoviKote: pragoviUKotama(st),
	})
	if strings.Contains(bez, "data-brisanje-letve") {
		t.Error("brisanje se nudi bez ovlasti")
	}
}
