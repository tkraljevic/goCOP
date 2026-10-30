package models

import (
	"strings"
	"testing"
	"time"
)

// Tendencija u rečenici akta; nepoznata se ne ispisuje.
func TestTendencijaLabelURecenici(t *testing.T) {
	for _, s := range []struct{ ulaz, want string }{
		{TendencijaPorast, "s tendencijom daljnjeg porasta"},
		{TendencijaOpadanje, "s tendencijom daljnjeg opadanja"},
		{TendencijaStagnacija, "sa stagnacijom"},
		{TendencijaNagliPorast, "s tendencijom daljnjeg naglog porasta"},
		{"", ""},
		{"NESTO", ""},
	} {
		if got := TendencijaLabel(s.ulaz); got != s.want {
			t.Errorf("TendencijaLabel(%q) = %q, očekivano %q", s.ulaz, got, s.want)
		}
	}
}

// Pripremno stanje ide obaviješću, ostali stupnjevi rješenjem, a članak
// Državnog plana slijedi stupanj.
func TestVrstaIClanakAktaPoStupnju(t *testing.T) {
	for _, s := range []struct {
		stupanj          DefensePhase
		rjesenje         bool
		naziv, vrsta, cl string
	}{
		{PhasePrep, false, "Obavijest", "OBAVIJEST", "XXII"},
		{PhaseRegular, true, "Rješenje", "RJEŠENJE", "XXIII"},
		{PhaseEmergency, true, "Rješenje", "RJEŠENJE", "XXIV"},
		{PhaseState, true, "Rješenje", "RJEŠENJE", "XXV"},
	} {
		a := Akt{Stupanj: s.stupanj}
		if a.JeRjesenje() != s.rjesenje || a.VrstaNaziv() != s.naziv || a.Vrsta() != s.vrsta || a.Clanak() != s.cl {
			t.Errorf("%s: rješenje %v, %q, %q, %q", s.stupanj, a.JeRjesenje(), a.VrstaNaziv(), a.Vrsta(), a.Clanak())
		}
	}
	if got := ZadaniClanak(PhaseNormal); got != "XXII" {
		t.Errorf("nepoznat stupanj: članak %q, očekivano XXII", got)
	}
}

// Predmet u naslovu ovisi i o radnji: prekid redovne i izvanredne obrane
// zove se prekidom mjera.
func TestPredmetINaslovAkta(t *testing.T) {
	for _, s := range []struct {
		stupanj DefensePhase
		radnja  string
		naslov  string
	}{
		{PhasePrep, AktUspostava, "OBAVIJEST o uspostavi pripremnog stanja obrane od poplava"},
		{PhasePrep, AktPrekid, "OBAVIJEST o prekidu pripremnog stanja obrane od poplava"},
		{PhaseRegular, AktUspostava, "RJEŠENJE o uspostavi redovne obrane od poplava"},
		{PhaseRegular, AktPrekid, "RJEŠENJE o prekidu redovnih mjera obrane od poplava"},
		{PhaseEmergency, AktUspostava, "RJEŠENJE o uspostavi izvanredne obrane od poplava"},
		{PhaseEmergency, AktPrekid, "RJEŠENJE o prekidu izvanrednih mjera obrane od poplava"},
		{PhaseState, AktUspostava, "RJEŠENJE o uspostavi izvanrednog stanja obrane od poplava na zaštitnim vodnim građevinama"},
	} {
		a := Akt{Stupanj: s.stupanj, Radnja: s.radnja}
		if got := a.Naslov(); got != s.naslov {
			t.Errorf("%s %s: %q, očekivano %q", s.stupanj, s.radnja, got, s.naslov)
		}
	}
	if got := (Akt{Stupanj: PhaseNormal}).Predmet(); got != PhaseNormal.Label() {
		t.Errorf("stupanj izvan akata: predmet %q", got)
	}
	if got := (Akt{}).RadnjaLabel(); got != "uspostavi" {
		t.Errorf("prazna radnja: %q, očekivano uspostavi", got)
	}
}

func TestOznakaOvjeraIImePotpisa(t *testing.T) {
	if got := (Akt{Sektor: "B", Godina: 2026}).Oznaka(); got != "nacrt" {
		t.Errorf("nacrt bez broja: %q", got)
	}
	a := Akt{Sektor: "B", Broj: 4, Godina: 2026, Status: AktOvjeren, Ovjerio: "Iva Ivić"}
	if got := a.Oznaka(); got != "B-4/2026" {
		t.Errorf("oznaka %q, očekivano B-4/2026", got)
	}
	if !a.Ovjeren() || (Akt{Status: AktNacrt}).Ovjeren() {
		t.Error("Ovjeren ne prati stanje akta")
	}
	if got := a.ImePotpisa(); got != "Iva Ivić" {
		t.Errorf("nositelj: %q", got)
	}
	a.UZamjeni = true
	if got := a.ImePotpisa(); got != "u.z. Iva Ivić" {
		t.Errorf("u zamjeni: %q", got)
	}
	if a.ImaIzvornik() {
		t.Error("akt bez skena ima izvornik")
	}
	a.Rucno = &RucniPotpis{Sazetak: "abc"}
	if !a.ImaIzvornik() {
		t.Error("akt sa skenom nema izvornik")
	}
}

// Za izvanrednu obranu i stanje nositelj je rukovoditelj sektora, za
// pripremno i redovnu rukovoditelj područja; neaktivna dužnost ne vrijedi.
func TestNositeljFunkcijePotpisnika(t *testing.T) {
	sektor := func(s string, aktivna bool) Duty {
		return Duty{Role: RoleSectorLeader, SectorID: &s, IsActive: aktivna}
	}
	podrucje := func(id int, aktivna bool) Duty { return Duty{Role: RoleAreaLeader, AreaID: &id, IsActive: aktivna} }
	for _, s := range []struct {
		ime     string
		stupanj DefensePhase
		duties  []Duty
		want    bool
	}{
		{"sektor za izvanrednu", PhaseEmergency, []Duty{sektor("B", true)}, true},
		{"sektor za stanje", PhaseState, []Duty{sektor("B", true)}, true},
		{"drugi sektor", PhaseEmergency, []Duty{sektor("A", true)}, false},
		{"neaktivan sektor", PhaseEmergency, []Duty{sektor("B", false)}, false},
		{"područje za izvanrednu", PhaseEmergency, []Duty{podrucje(15, true)}, false},
		{"sektor bez šifre", PhaseEmergency, []Duty{{Role: RoleSectorLeader, IsActive: true}}, false},
		{"područje za redovnu", PhaseRegular, []Duty{podrucje(15, true)}, true},
		{"područje za pripremno", PhasePrep, []Duty{podrucje(15, true)}, true},
		{"drugo područje", PhaseRegular, []Duty{podrucje(16, true)}, false},
		{"sektor za redovnu", PhaseRegular, []Duty{sektor("B", true)}, false},
		{"područje bez broja", PhaseRegular, []Duty{{Role: RoleAreaLeader, IsActive: true}}, false},
		{"bez dužnosti", PhaseRegular, nil, false},
		{"druga dužnost pomaže", PhaseRegular, []Duty{podrucje(16, true), podrucje(15, true)}, true},
	} {
		a := Akt{Sektor: "B", AreaID: 15, Stupanj: s.stupanj}
		if got := a.NositeljFunkcije(s.duties); got != s.want {
			t.Errorf("%s: %v, očekivano %v", s.ime, got, s.want)
		}
	}
}

// Osnova akta: po prognozi, po stanju bez broja, ili po izmjerenom
// vodostaju sa satom u zagrebačkom vremenu i tendencijom.
func TestOsnovaAkta(t *testing.T) {
	v := 512
	for _, s := range []struct {
		ime  string
		a    Akt
		want string
	}{
		{"prognoza", Akt{StationName: "Batina", Prognoza: "  porast do 560 cm  ", VodostajCm: &v},
			"a vezano na prognozu vodostaja vodotoka na mjerodavnom vodomjeru Batina: porast do 560 cm"},
		{"bez vodostaja", Akt{StationName: "Batina", Watercourse: "r. Dunav"},
			"a vezano na stanje vodostaja r. Dunav na mjerodavnom vodomjeru Batina"},
		{"samo broj", Akt{StationName: "Batina", Watercourse: "r. Dunav", VodostajCm: &v},
			"a vezano na visinu vodostaja r. Dunav na mjerodavnom vodomjeru Batina, na kojem je zabilježen vodostaj od 512 cm"},
		{"sa satom i tendencijom", Akt{StationName: "Batina", Watercourse: "r. Dunav", VodostajCm: &v,
			VodostajKad: time.Date(2026, 11, 3, 6, 0, 0, 0, time.UTC), Tendencija: TendencijaPorast},
			"a vezano na visinu vodostaja r. Dunav na mjerodavnom vodomjeru Batina, na kojem je zabilježen vodostaj od 512 cm u 07:00 sati, s tendencijom daljnjeg porasta"},
	} {
		if got := s.a.Osnova(); got != s.want {
			t.Errorf("%s:\n %q\nočekivano\n %q", s.ime, got, s.want)
		}
	}
}

// Špranca: članak iz vlastite tablice, inače zadani; {clanak} se zamjenjuje,
// a zarez na kraju osnove ne udvostručuje se.
func TestSprancaClanakIUvod(t *testing.T) {
	sp := Spranca{Osnova: " Na temelju članka {clanak}, ", Clanci: map[DefensePhase]string{PhaseRegular: " XXX "}}
	if got := sp.ClanakZa(PhaseRegular); got != "XXX" {
		t.Errorf("vlastiti članak: %q", got)
	}
	if got := sp.ClanakZa(PhaseEmergency); got != "XXIV" {
		t.Errorf("članak koji nedostaje: %q", got)
	}
	if got := (Spranca{}).ClanakZa(PhaseState); got != "XXV" {
		t.Errorf("prazna špranca: %q", got)
	}
	a := Akt{Stupanj: PhaseEmergency, StationName: "Batina", Watercourse: "r. Dunav"}
	want := "Na temelju članka XXIV, a vezano na stanje vodostaja r. Dunav na mjerodavnom vodomjeru Batina, donosim"
	if got := sp.Uvod(a); got != want {
		t.Errorf("uvod:\n %q\nočekivano\n %q", got, want)
	}
}

func TestZadanaSprancaITekstAkta(t *testing.T) {
	sp := ZadanaSpranca("B")
	if sp.Sektor != "B" || !strings.Contains(sp.Osnova, "{clanak}") || len(sp.Clanci) != 4 {
		t.Fatalf("zadana špranca: %+v", sp)
	}
	if n := len(strings.Split(sp.Poveznice, "\n")); n != 2 {
		t.Errorf("zadane poveznice: %d redaka, očekivano 2", n)
	}

	a := Akt{Sektor: "B", Stupanj: PhaseRegular, StationName: "Batina", Watercourse: "r. Dunav", Uvod: "  ", Zavrsno: "\t"}
	uvod := a.TekstUvoda()
	if !strings.HasPrefix(uvod, "Na temelju Zakona o vodama") || !strings.Contains(uvod, "odredbi članka XXIII ") ||
		!strings.HasSuffix(uvod, "mjerodavnom vodomjeru Batina, donosim") || strings.Contains(uvod, "{clanak}") {
		t.Errorf("uvod iz zadane špranče: %q", uvod)
	}
	if got := a.TekstZavrsni(); got != sp.Zavrsno {
		t.Errorf("završna iz zadane špranče: %q", got)
	}

	a.Uvod, a.Zavrsno = "Ispravljen uvod", "Ispravljen kraj"
	if a.TekstUvoda() != "Ispravljen uvod" || a.TekstZavrsni() != "Ispravljen kraj" {
		t.Error("spremljeni tekst akta nije zadržan")
	}
}

func TestRecenicaIzvanSnage(t *testing.T) {
	u := Akt{Sektor: "B", Broj: 4, Godina: 2026, Stupanj: PhaseRegular, Radnja: AktUspostava,
		Vrijedi: time.Date(2026, 11, 2, 13, 30, 0, 0, time.UTC)}
	want := "Stavlja se izvan snage Rješenje o uspostavi redovne obrane od poplava oznake B-4/2026 od 02.11.2026. u 14:30 sati."
	if got := RecenicaIzvanSnage(u); got != want {
		t.Errorf("\n %q\nočekivano\n %q", got, want)
	}
}

func TestSifreDionicaPoredane(t *testing.T) {
	a := Akt{Dionice: []AktDionica{{Code: "B.2"}, {Code: "B.10"}, {Code: "B.1"}}}
	if got := strings.Join(a.Sifre(), ","); got != "B.1,B.10,B.2" {
		t.Errorf("šifre %q", got)
	}
	if got := (Akt{}).Sifre(); got == nil || len(got) != 0 {
		t.Errorf("akt bez dionica: %#v", got)
	}
	if a.Dionice[0].Code != "B.2" {
		t.Error("Sifre je promijenio redoslijed dionica na aktu")
	}
}

func probniAkt() Akt {
	v := 512
	return Akt{
		ID: "akt-1", Sektor: "B", AreaID: 15, Broj: 4, Godina: 2026, Radnja: AktUspostava, Stupanj: PhaseRegular,
		StationID: "st-1", StationName: "Batina", Watercourse: "r. Dunav",
		VodostajCm: &v, VodostajKad: time.Date(2026, 11, 3, 6, 0, 0, 0, time.UTC), Tendencija: TendencijaPorast,
		Vrijedi: time.Date(2026, 11, 3, 7, 0, 0, 0, time.UTC), Potpisnik: "Rukovoditelj",
		Dionice:    []AktDionica{{Code: "B.34.1", Opis: "d.o. r. Dunav"}},
		Primatelji: []AktPrimatelj{{Naziv: "Hrvatske vode", Email: "pismohrana@primjer.hr"}},
	}
}

// Sažetak pokriva sadržaj akta, a ne stanje ni ovjeru.
func TestSazetakAktaPokrivaSadrzaj(t *testing.T) {
	a := probniAkt()
	s := a.Sazetak()
	for _, dio := range []string{"B|15|USPOSTAVA|REDOVNA|st-1|", "512@2026-11-03T06:00:00Z|", "B.34.1=d.o. r. Dunav;", "Hrvatske vode<pismohrana@primjer.hr>;"} {
		if !strings.Contains(s, dio) {
			t.Errorf("sažetak ne sadrži %q: %s", dio, s)
		}
	}
	if strings.Contains(s, "sken:") {
		t.Error("sažetak bez skena spominje sken")
	}

	b := a
	b.Status, b.Ovjerio, b.OvjerioID = AktOvjeren, "Iva Ivić", "u-1"
	if b.Sazetak() != s {
		t.Error("ovjera je promijenila sažetak sadržaja")
	}
	for ime, izmjena := range map[string]func(*Akt){
		"napomena": func(x *Akt) { x.Napomena = "dopuna" },
		"dionica":  func(x *Akt) { x.Dionice = []AktDionica{{Code: "B.34.1", Opis: "l.o. r. Dunav"}} },
		"vodostaj": func(x *Akt) { w := 513; x.VodostajCm = &w },
		"sken":     func(x *Akt) { x.Rucno = &RucniPotpis{Sazetak: "abc"} },
	} {
		c := probniAkt()
		izmjena(&c)
		if c.Sazetak() == s {
			t.Errorf("izmjena %s ne mijenja sažetak", ime)
		}
	}
	c := probniAkt()
	c.Rucno = &RucniPotpis{Sazetak: "abc"}
	if !strings.Contains(c.Sazetak(), "|sken:abc|") {
		t.Errorf("sažetak sa skenom: %s", c.Sazetak())
	}
}

func TestPorukaPotpisaAkta(t *testing.T) {
	a := probniAkt()
	a.OvjerioID, a.Ovjerio = "u-1", "Iva Ivić"
	p := string(a.PorukaPotpisa())
	if !strings.HasPrefix(p, "goCOP-akt-v1|akt-1|B-4/2026|"+a.Sazetak()+"|") || !strings.HasSuffix(p, "|u-1|Iva Ivić|") {
		t.Errorf("poruka bez vremena ovjere: %q", p)
	}
	kad := time.Date(2026, 11, 3, 9, 15, 0, 0, Zagreb)
	a.OvjerenoAt = &kad
	if p := string(a.PorukaPotpisa()); !strings.HasSuffix(p, "|u-1|Iva Ivić|2026-11-03T08:15:00Z") {
		t.Errorf("poruka s vremenom ovjere: %q", p)
	}
}

func TestOtisakKljucaCvora(t *testing.T) {
	if got := (Akt{}).OtisakKljuca(); got != "" {
		t.Errorf("bez ključa: %q", got)
	}
	want := "8A02 A260 EFDF"
	if got := (Akt{KljucCvora: "javni-kljuc"}).OtisakKljuca(); got != want {
		t.Errorf("otisak %q, očekivano %q", got, want)
	}
	if (Akt{KljucCvora: "drugi"}).OtisakKljuca() == want {
		t.Error("dva ključa imaju isti otisak")
	}
}

// Kod ovjere ovisi o sadržaju, ovjeritelju i trenutku, ne o vremenskoj zoni.
func TestKodOvjere(t *testing.T) {
	a := probniAkt()
	kad := time.Date(2026, 11, 3, 8, 15, 0, 0, time.UTC)
	k := a.KodOvjere("u-1", kad)
	if len(k) != 10 || strings.Trim(k, "0123456789ABCDEF") != "" {
		t.Fatalf("kod %q nije 10 velikih heksadecimalnih znakova", k)
	}
	if a.KodOvjere("u-1", kad.In(Zagreb)) != k {
		t.Error("isti trenutak u drugoj zoni daje drugi kod")
	}
	if a.KodOvjere("u-2", kad) == k || a.KodOvjere("u-1", kad.Add(time.Second)) == k {
		t.Error("kod ne ovisi o ovjeritelju ili trenutku")
	}
	a.Napomena = "dopuna"
	if a.KodOvjere("u-1", kad) == k {
		t.Error("kod ne ovisi o sadržaju")
	}
}

func TestPrimateljiPodstavkeIRedniBrojevi(t *testing.T) {
	ps := []AktPrimatelj{
		{Naziv: "Hrvatske vode, Direkcija Zagreb"},
		{Naziv: "– Ured generalnog direktora"},
		{Naziv: "-  Glavni centar"},
		{Naziv: "VGI Vuka"},
		{Naziv: "Pismohrana"},
	}
	if got := RedniBrojevi(ps); len(got) != 5 || got[0] != 1 || got[1] != 0 || got[2] != 0 || got[3] != 2 || got[4] != 3 {
		t.Errorf("redni brojevi %v, očekivano [1 0 0 2 3]", got)
	}
	if ps[0].Podstavka() || !ps[1].Podstavka() || !ps[2].Podstavka() {
		t.Error("Podstavka krivo prepoznaje crticu")
	}
	if got := ps[1].NazivBezCrtice(); got != "Ured generalnog direktora" {
		t.Errorf("bez crtice: %q", got)
	}
	if got := ps[2].NazivBezCrtice(); got != "Glavni centar" {
		t.Errorf("bez crtice i razmaka: %q", got)
	}
	if got := RedniBrojevi(nil); len(got) != 0 {
		t.Errorf("bez primatelja: %v", got)
	}
}

func TestRetciPoveznica(t *testing.T) {
	a := Akt{Poveznice: "Glavni plan: https://primjer.hr/plan\n\n  Bez adrese  \n: samo dvotočka\nDržavni plan:   https://primjer.hr/dp  "}
	got := a.Retci()
	want := [][2]string{
		{"Glavni plan:", "https://primjer.hr/plan"},
		{"Bez adrese", ""},
		{": samo dvotočka", ""},
		{"Državni plan:", "https://primjer.hr/dp"},
	}
	if len(got) != len(want) {
		t.Fatalf("retci %q, očekivano %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("redak %d: %q, očekivano %q", i, got[i], want[i])
		}
	}
	if got := (Akt{}).Retci(); got != nil {
		t.Errorf("bez poveznica: %q", got)
	}
}

func TestSkupinaLabel(t *testing.T) {
	for _, s := range []struct{ ulaz, want string }{
		{SkupinaUprava, "uprava organizacije"},
		{SkupinaIspostava, "ispostava i izvođač"},
		{SkupinaSluzbe, "službe"},
		{SkupinaSamouprava, "županije, gradovi i općine"},
		{SkupinaOsobe, "rukovoditelji dionica"},
		{SkupinaPismohrana, "pismohrana"},
		{"NOVA", "NOVA"},
	} {
		if got := SkupinaLabel(s.ulaz); got != s.want {
			t.Errorf("SkupinaLabel(%q) = %q, očekivano %q", s.ulaz, got, s.want)
		}
	}
	if len(SkupinePrimatelja) != 6 || SkupinePrimatelja[0] != SkupinaUprava || SkupinePrimatelja[5] != SkupinaPismohrana {
		t.Errorf("redoslijed skupina %v", SkupinePrimatelja)
	}
}

// Primatelj ide na akt samo ako je aktivan, za to područje (ili cijeli
// sektor) i od svojeg stupnja naviše.
func TestPrimateljVrijedi(t *testing.T) {
	for _, s := range []struct {
		ime     string
		p       Primatelj
		area    int
		stupanj DefensePhase
		want    bool
	}{
		{"cijeli sektor", Primatelj{Aktivan: true}, 15, PhasePrep, true},
		{"neaktivan", Primatelj{}, 15, PhasePrep, false},
		{"arhiviran", Primatelj{Aktivan: true, Archived: true}, 15, PhasePrep, false},
		{"svoje područje", Primatelj{Aktivan: true, AreaID: 15}, 15, PhasePrep, true},
		{"drugo područje", Primatelj{Aktivan: true, AreaID: 16}, 15, PhaseState, false},
		{"ispod stupnja", Primatelj{Aktivan: true, OdStupnja: PhaseRegular}, 15, PhasePrep, false},
		{"na stupnju", Primatelj{Aktivan: true, OdStupnja: PhaseRegular}, 15, PhaseRegular, true},
		{"iznad stupnja", Primatelj{Aktivan: true, OdStupnja: PhaseRegular}, 15, PhaseState, true},
	} {
		if got := s.p.Vrijedi(s.area, s.stupanj); got != s.want {
			t.Errorf("%s: %v, očekivano %v", s.ime, got, s.want)
		}
	}
}
