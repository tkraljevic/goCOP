package prognoza

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// dnevniZapis je zapis letve s dnevnim modelom iz zadanih ulaza i slivova.
func dnevniZapis(t *testing.T, letva string, ulazi, slivovi []string) ZapisLetve {
	t.Helper()
	blok, err := json.Marshal(DnevniLetve{Ulazi: ulazi, Slivovi: slivovi})
	if err != nil {
		t.Fatal(err)
	}
	return ZapisLetve{Oblik: OblikModelaLetve, Letva: letva, Metoda: MetodaDnevni, Velicina: "vodostaj", Dnevni: blok}
}

// osvjezivacDnevnihZapisa ima satna očitanja letvi gornja i donja, dnevnu arhivu u
// kojoj donja ponavlja gornju dva dana kasnije, i praznu bazu prognoza.
func osvjezivacDnevnihZapisa(t *testing.T) (*Osvjezivac, time.Time) {
	t.Helper()
	ZaboraviDnevneZapisa()
	t.Cleanup(ZaboraviDnevneZapisa)
	arhiva, _ := arhivaUDatoteci(t)
	upisiDnevne(t, arhiva, "gornja", "vodostaj", 3600, valIzmisljeni)
	upisiDnevne(t, arhiva, "donja", "vodostaj", 3600, func(d int64) float64 { return valIzmisljeni(d-2) + 20 })
	upisiDnevne(t, arhiva, "kisomjer-x", "oborina", 3600, kisaIzmisljena)
	baza, err := Otvori(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	zadnji := time.Now().UTC().Truncate(time.Hour)
	return &Osvjezivac{Baza: baza, Ocitanja: probneOcitanja(t, 120, zadnji), Arhiva: arhiva}, zadnji
}

func praznoIzdanje(zadnji time.Time) *Ishod {
	return &Ishod{Sada: zadnji.Unix() / 3600, Izbor: map[string]Izbor{}, BezPrognoze: map[string]error{}}
}

func dnevnihZa(i *Ishod, letva string) int {
	n := 0
	for _, d := range i.Dnevne {
		if d.Letva == letva {
			n++
		}
	}
	return n
}

// Letva čiji zapis bira dnevni model dobiva u izdanju dnevne vrijednosti i svoje
// mjerenje u satu izdavanja. Satni krug zapisa je ne računa.
func TestDnevniKrugZapisa(t *testing.T) {
	o, zadnji := osvjezivacDnevnihZapisa(t)
	upisiZapis(t, o.Baza, dnevniZapis(t, "donja", []string{"gornja"}, nil))
	od := zadnji.Add(-10 * 24 * time.Hour)
	i := praznoIzdanje(zadnji)
	o.dnevniKrugZapisa(context.Background(), i, nil, od)
	if dnevnihZa(i, "donja") != DnevniDosezi+1 || len(i.BezDnevne) != 0 || len(i.Upozorenja) != 0 {
		t.Fatalf("dnevnih %d, bez %v, upozorenja %v", dnevnihZa(i, "donja"), i.BezDnevne, i.Upozorenja)
	}
	if len(i.Izdane) != 1 || i.Izdane[0].Letva != "donja" || i.Izdane[0].Ciljni != i.Sada || i.Izdane[0].Model != ModelDnevni {
		t.Errorf("mjerenje u satu izdavanja: %+v", i.Izdane)
	}
	zapisi, pokvareni, err := zapisiZaRacun(o.Baza, nil)
	if err != nil || len(zapisi) != 0 || len(pokvareni) != 0 {
		t.Errorf("satni krug ne smije vidjeti zapis s dnevnim modelom: %v, %v, %v", zapisi, pokvareni, err)
	}
	// drugi put isti sat: model je u zalihi, a letva koja već ima dnevne se preskače
	o.dnevniKrugZapisa(context.Background(), i, nil, od)
	if dnevnihZa(i, "donja") != DnevniDosezi+1 || len(i.Izdane) != 1 {
		t.Errorf("letva s dnevnim vrijednostima računata je opet: %d dnevnih, %d izdanih", dnevnihZa(i, "donja"), len(i.Izdane))
	}
	// letva koja već stoji u izdanju ne dobiva drugo mjerenje
	j := praznoIzdanje(zadnji)
	j.Izdane = []Izdana{{Letva: "donja", Velicina: "vodostaj", Izdano: j.Sada, Ciljni: j.Sada, Vrijednost: 1}}
	o.dnevniKrugZapisa(context.Background(), j, nil, od)
	if dnevnihZa(j, "donja") != DnevniDosezi+1 || len(j.Izdane) != 1 || j.Izdane[0].Vrijednost != 1 {
		t.Errorf("zatečeno mjerenje je zamijenjeno: %+v", j.Izdane)
	}
}

// Bez arhive, bez baze ili bez zapisa s dnevnim modelom krug ne radi ništa.
func TestDnevniKrugZapisaBezPosla(t *testing.T) {
	o, zadnji := osvjezivacDnevnihZapisa(t)
	i := praznoIzdanje(zadnji)
	o.dnevniKrugZapisa(context.Background(), i, nil, zadnji)
	(&Osvjezivac{Baza: o.Baza}).dnevniKrugZapisa(context.Background(), i, nil, zadnji)
	(&Osvjezivac{Arhiva: o.Arhiva}).dnevniKrugZapisa(context.Background(), i, nil, zadnji)
	if len(i.Dnevne)+len(i.Izdane)+len(i.BezDnevne)+len(i.Upozorenja) != 0 {
		t.Errorf("izdanje je dirano: %+v", i)
	}
	if err := o.Baza.Close(); err != nil {
		t.Fatal(err)
	}
	o.dnevniKrugZapisa(context.Background(), i, nil, zadnji)
	if len(i.Dnevne)+len(i.BezDnevne) != 0 {
		t.Errorf("zatvorena baza: %+v", i)
	}
}

// Što ne radi, stoji uz letvu s razlogom: zapis koji ne prolazi provjeru, letva
// bez arhive, kiša bez kišomjera. Ostale letve računaju.
func TestDnevniKrugZapisaRazlozi(t *testing.T) {
	o, zadnji := osvjezivacDnevnihZapisa(t)
	upisiZapis(t, o.Baza, dnevniZapis(t, "donja", []string{"gornja"}, nil))
	upisiZapis(t, o.Baza, dnevniZapis(t, "prazna", nil, nil))
	upisiZapis(t, o.Baza, dnevniZapis(t, "bez-arhive", []string{"gornja"}, nil))
	upisiZapis(t, o.Baza, dnevniZapis(t, "s-kisom", []string{"gornja"}, []string{"X"}))
	i := praznoIzdanje(zadnji)
	o.dnevniKrugZapisa(context.Background(), i, nil, zadnji.Add(-10*24*time.Hour))
	if dnevnihZa(i, "donja") != DnevniDosezi+1 {
		t.Errorf("ispravna letva nije računala: %d", dnevnihZa(i, "donja"))
	}
	for letva, razlog := range map[string]string{"prazna": "ni ulaz ni sliv", "bez-arhive": "nema dnevnog niza", "s-kisom": "oborine nisu uključene"} {
		if err := i.BezDnevne[letva]; !porukaIma(err, razlog) {
			t.Errorf("%s: %v, želim %q", letva, err, razlog)
		}
	}
	// neuspjelo učenje ne ponavlja se isti sat, a novi dan ga ponavlja
	zalihaZapisa.Lock()
	prije := zalihaZapisa.po["bez-arhive"]
	zalihaZapisa.Unlock()
	if prije.greska == nil || prije.model != nil {
		t.Fatalf("zaliha nakon neuspjelog učenja: %+v", prije)
	}
	upisiDnevne(t, o.Arhiva, "bez-arhive", "vodostaj", 3600, valIzmisljeni)
	// isti sat i ista arhiva: neuspjelo se ne ponavlja
	if _, err := o.dnevniModelZapisa(DnevniCilj{Letva: "prazna-arhiva", Ulazi: []string{"gornja"}}, sadaUcenja()); err == nil {
		t.Error("letva bez arhive mora javiti grešku")
	}
	zalihaZapisa.Lock()
	prvi := zalihaZapisa.po["prazna-arhiva"]
	zalihaZapisa.Unlock()
	if _, err := o.dnevniModelZapisa(DnevniCilj{Letva: "prazna-arhiva", Ulazi: []string{"gornja"}}, sadaUcenja()); err == nil || prvi.greska == nil {
		t.Error("neuspjelo učenje u istom satu vraća istu grešku")
	}
	uSat(t, sadaUcenja().Add(48*time.Hour))
	if m, err := o.dnevniModelZapisa(DnevniCilj{Letva: "bez-arhive", Ulazi: []string{"gornja"}}, sadaUcenja()); err != nil || m == nil {
		t.Errorf("novi dan mora ponoviti učenje: %v", err)
	}
	// druga definicija iste letve uči se iznova
	if m, err := o.dnevniModelZapisa(DnevniCilj{Letva: "bez-arhive", Ulazi: []string{"donja"}}, sadaUcenja()); err != nil || m == nil || m.Cilj.Ulazi[0] != "donja" {
		t.Errorf("promijenjena definicija: %v", err)
	}
	// s kišomjerom u registru model s kišom se uči
	o.Oborine = &OborinskiIzvor{Tocke: []OborinskaTocka{{Code: "kisomjer-x", Sliv: "X", Tezina: 1}}}
	if m, err := o.nauciDnevniZapisa(DnevniCilj{Letva: "donja", Ulazi: []string{"gornja"}, Slivovi: []string{"X"}}); err != nil || m == nil {
		t.Errorf("model s kišom: %v", err)
	}
	if err := o.Arhiva.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := o.nauciDnevniZapisa(DnevniCilj{Letva: "donja", Ulazi: []string{"gornja"}}); err == nil {
		t.Error("zatvorena arhiva mora javiti grešku")
	}
}

// Račun koji ne završi na vrijeme ili padne ostavlja izdanje kakvo je bilo i javlja
// upozorenje; neispravni zapisi i tada stoje s razlogom.
func TestDnevniKrugZapisaRokIPanika(t *testing.T) {
	o, zadnji := osvjezivacDnevnihZapisa(t)
	upisiZapis(t, o.Baza, dnevniZapis(t, "donja", []string{"gornja"}, nil))
	upisiZapis(t, o.Baza, dnevniZapis(t, "prazna", nil, nil))
	od := zadnji.Add(-10 * 24 * time.Hour)

	postavi(t, &RokOdvojenih, time.Nanosecond)
	i := praznoIzdanje(zadnji)
	o.dnevniKrugZapisa(context.Background(), i, nil, od)
	if len(i.Dnevne) != 0 || len(i.Izdane) != 0 || i.BezDnevne["prazna"] == nil ||
		!slices.ContainsFunc(i.Upozorenja, func(u Upozorenje) bool { return u.Vrsta == VrstaOdvojeni && strings.Contains(u.Opis, "traje dulje") }) {
		t.Errorf("prekoračen rok: dnevnih %d, bez %v, upozorenja %v", len(i.Dnevne), i.BezDnevne, i.Upozorenja)
	}
	// otkazan račun izlazi prije prve letve
	otkazan, otkazi := context.WithCancel(context.Background())
	otkazi()
	if izvod := o.racunDnevnihZapisa(otkazan, []DnevniCilj{{Letva: "donja", Ulazi: []string{"gornja"}}}, i.Sada, od); len(izvod.dnevne) != 0 || izvod.greska != nil {
		t.Errorf("otkazan račun: %+v", izvod)
	}

	postavi(t, &RokOdvojenih, 20*time.Second)
	o.Kvarovi = prepoznavacKojiPuca{letva: "gornja"}
	j := praznoIzdanje(zadnji)
	o.dnevniKrugZapisa(context.Background(), j, nil, od)
	if len(j.Dnevne) != 0 ||
		!slices.ContainsFunc(j.Upozorenja, func(u Upozorenje) bool {
			return u.Vrsta == VrstaOdvojeni && strings.Contains(u.Opis, "greška u računu")
		}) {
		t.Errorf("panika: dnevnih %d, upozorenja %v", len(j.Dnevne), j.Upozorenja)
	}
}

// Letva kojoj fale satna očitanja nema dnevnu prognozu, uz razlog, a bez očitanja
// u satu izdavanja nema ni mjerenje na pregledu.
func TestDnevniKrugZapisaBezOcitanja(t *testing.T) {
	o, zadnji := osvjezivacDnevnihZapisa(t)
	upisiDnevne(t, o.Arhiva, "treca", "vodostaj", 3600, func(d int64) float64 { return valIzmisljeni(d-1) + 5 })
	upisiZapis(t, o.Baza, dnevniZapis(t, "treca", []string{"gornja"}, nil))
	i := praznoIzdanje(zadnji)
	o.dnevniKrugZapisa(context.Background(), i, nil, zadnji.Add(-10*24*time.Hour))
	if dnevnihZa(i, "treca") != 0 || i.BezDnevne["treca"] == nil || len(i.Izdane) != 0 {
		t.Errorf("letva bez satnih očitanja: dnevnih %d, bez %v, izdane %v", dnevnihZa(i, "treca"), i.BezDnevne, i.Izdane)
	}
}

// Zapis dnevnog modela: što mora imati i što ga odbija.
func TestProvjeraDnevnogZapisa(t *testing.T) {
	dobar := dnevniZapis(t, "donja", []string{"gornja"}, []string{"X"})
	if err := dobar.Provjeri(OkvirModela{}); err != nil {
		t.Fatalf("ispravan zapis: %v", err)
	}
	c, err := dobar.DnevniCiljZapisa()
	if err != nil || c.Letva != "donja" || !slices.Equal(c.Ulazi, []string{"gornja"}) || !slices.Equal(c.Slivovi, []string{"X"}) {
		t.Errorf("cilj iz zapisa %+v, %v", c, err)
	}
	mnogo := make([]string, najviseUlaza+1)
	for i := range mnogo {
		mnogo[i] = "letva-" + string(rune('a'+i))
	}
	for ime, s := range map[string]struct {
		z      ZapisLetve
		o      OkvirModela
		razlog string
	}{
		"noviji oblik":    {func() ZapisLetve { z := dobar; z.Oblik = OblikModelaLetve + 1; return z }(), OkvirModela{}, "novije inačice"},
		"bez oblika":      {func() ZapisLetve { z := dobar; z.Oblik = 0; return z }(), OkvirModela{}, "nema oblik"},
		"bez letve":       {func() ZapisLetve { z := dobar; z.Letva = ""; return z }(), OkvirModela{}, "nema letvu"},
		"bez bloka":       {func() ZapisLetve { z := dobar; z.Dnevni = nil; return z }(), OkvirModela{}, "nema dnevni model"},
		"blok null":       {func() ZapisLetve { z := dobar; z.Dnevni = json.RawMessage("null"); return z }(), OkvirModela{}, "nema dnevni model"},
		"blok pokvaren":   {func() ZapisLetve { z := dobar; z.Dnevni = json.RawMessage(`"x"`); return z }(), OkvirModela{}, "ne da pročitati"},
		"ni ulaz ni sliv": {dnevniZapis(t, "donja", nil, nil), OkvirModela{}, "ni ulaz ni sliv"},
		"previše ulaza":   {dnevniZapis(t, "donja", mnogo, nil), OkvirModela{}, "najviše"},
		"prazan ulaz":     {dnevniZapis(t, "donja", []string{""}, nil), OkvirModela{}, "prazan ulaz ili sliv"},
		"sama sebi":       {dnevniZapis(t, "donja", []string{"donja"}, nil), OkvirModela{}, "sama sebi"},
		"ulaz dvaput":     {dnevniZapis(t, "donja", []string{"gornja", "gornja"}, nil), OkvirModela{}, "naveden je dvaput"},
		"sliv dvaput":     {dnevniZapis(t, "donja", nil, []string{"X", "X"}), OkvirModela{}, "naveden je dvaput"},
		"brana":           {dnevniZapis(t, "brana-lug", []string{"gornja"}, nil), OkvirModela{}, "elektrana ili brana"},
		"bez sektora":     {dobar, OkvirModela{ImaSektor: func(string) bool { return false }}, "nema sektor"},
	} {
		if err := s.z.Provjeri(s.o); !porukaIma(err, s.razlog) {
			t.Errorf("%s: %v, želim %q", ime, err, s.razlog)
		}
	}
	// zapis se sprema istim putem kao i satni
	baza := otvoriProbnu(t, "p.db")
	if err := SpremiZapisLetve(baza, dobar, "", OkvirModela{}); err != nil {
		t.Errorf("spremanje zapisa s dnevnim modelom: %v", err)
	}
	if err := SpremiZapisLetve(baza, dnevniZapis(t, "donja", nil, nil), "", OkvirModela{}); err == nil {
		t.Error("neispravan zapis se ne smije spremiti")
	}
}

// Stranica prognoza čita satne brojeve letvi iz zapisa. Zapis s dnevnim modelom
// ih nema, pa ga taj prikaz mora preskočiti, a ne pasti na njemu.
func TestPrikazOdvojenihPreskaceDnevni(t *testing.T) {
	baza := otvoriProbnu(t, "p.db")
	upisiZapis(t, baza, dnevniZapis(t, "donja", []string{"gornja"}, nil))
	if p := OdvojeniZaPrikaz(baza); len(p.Pojasi)+len(p.Promasaji)+len(p.Vrhovi) != 0 {
		t.Errorf("prikaz samo s dnevnim zapisom: %+v", p)
	}
	upisiZapis(t, baza, zapisPotoka("potok-dole", "potok-vrh", 2))
	p := OdvojeniZaPrikaz(baza)
	if len(p.Pojasi) != 1 || p.Pojasi["potok-dole"] == nil || !p.Vrhovi["potok-vrh"] {
		t.Errorf("prikaz uz satni i dnevni zapis: %+v", p)
	}
}

// Nacrt dnevnog modela se sprema, proba na zadnje dvije godine arhive i primjenjuje
// bez pripreme: model se uči u samoj probi. Što nije probano, ne primjenjuje se.
func TestNacrtDnevnogModela(t *testing.T) {
	arhiva, _ := arhivaUDatoteci(t)
	upisiDnevne(t, arhiva, "gornja", "vodostaj", 4600, valIzmisljeni)
	upisiDnevne(t, arhiva, "donja", "vodostaj", 4600, func(d int64) float64 { return valIzmisljeni(d-2) + 20 })
	upisiDnevne(t, arhiva, "kratka", "vodostaj", 900, valIzmisljeni)
	baza := otvoriProbnu(t, "p.db")
	nacrt := NacrtLetve{Zapis: dnevniZapis(t, "donja", []string{"gornja"}, nil), Tko: "Iva Proba", Kad: "09:00"}

	if _, err := ProbajDnevniNacrt(arhiva, baza, nil, "donja", "sada"); !porukaIma(err, "nema nacrt dnevnog modela") {
		t.Errorf("proba bez nacrta: %v", err)
	}
	if MetodaNacrta(baza, "donja") != "" {
		t.Error("letva bez nacrta nema metodu")
	}
	if err := SpremiNacrt(baza, NacrtLetve{Zapis: dnevniZapis(t, "donja", nil, nil)}, OkvirModela{}); !porukaIma(err, "ni ulaz ni sliv") {
		t.Errorf("neispravan nacrt: %v", err)
	}
	if err := SpremiNacrt(baza, nacrt, OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	if MetodaNacrta(baza, "donja") != MetodaDnevni {
		t.Error("metoda nacrta")
	}
	promjena := PromjenaLetve{Kad: "22. 11. 2026. 12:00", Tko: "Iva Proba", Razlog: "dnevni model potoka"}
	if err := PrimijeniNacrt(baza, "donja", promjena, OkvirModela{}); !porukaIma(err, "nije proban") {
		t.Errorf("primjena bez probe: %v", err)
	}
	n, err := ProbajDnevniNacrt(arhiva, baza, nil, "donja", "22. 11. 2026. 11:00")
	if err != nil {
		t.Fatal(err)
	}
	if n.Proba == nil || len(n.Proba.Dani) != DnevniDosezi || n.Proba.Dani[0].Stanje != MjeraBolje || n.Proba.Dani[0].Slucaja < 600 ||
		len(n.Proba.Razdoblje) != 2 || n.Proba.Razdoblje[1] != "1982-08-05" || n.Proba.Kad != "22. 11. 2026. 11:00" {
		t.Fatalf("proba dnevnog modela: %+v", n.Proba)
	}
	// ista definicija čuva probu, promijenjena je briše
	if err := SpremiNacrt(baza, nacrt, OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	svi, err := Nacrti(baza)
	if err != nil || svi["donja"].Proba == nil {
		t.Fatalf("ista definicija mora zadržati probu: %v", err)
	}
	drugi := NacrtLetve{Zapis: dnevniZapis(t, "donja", []string{"gornja", "kratka"}, nil)}
	if err := SpremiNacrt(baza, drugi, OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	if svi, err := Nacrti(baza); err != nil || svi["donja"].Proba != nil {
		t.Errorf("promijenjena definicija mora izgubiti probu: %v", err)
	}
	// uz dvije godine odvojene za mjerenje srednja nema dosta dana, pa se mjeri na jednoj
	upisiDnevne(t, arhiva, "srednja", "vodostaj", 3400, func(d int64) float64 { return valIzmisljeni(d-1) + 7 })
	if err := SpremiNacrt(baza, NacrtLetve{Zapis: dnevniZapis(t, "srednja", []string{"gornja"}, nil)}, OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	godinu, err := ProbajDnevniNacrt(arhiva, baza, nil, "srednja", "sada")
	if err != nil || godinu.Proba.Dani[0].Slucaja > 365 || godinu.Proba.Dani[0].Slucaja < 300 {
		t.Errorf("proba na zadnjoj godini: %+v, %v", godinu.Proba, err)
	}
	// ulaz s malo dana: proba javlja da nema dosta dana za učenje
	if _, err := ProbajDnevniNacrt(arhiva, baza, nil, "donja", "sada"); !porukaIma(err, "dana sa svim ulazima") {
		t.Errorf("premalo dana: %v", err)
	}
	if err := SpremiNacrt(baza, nacrt, OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbajDnevniNacrt(arhiva, baza, nil, "donja", "11:30"); err != nil {
		t.Fatal(err)
	}
	if err := PrimijeniNacrt(baza, "donja", promjena, OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	zapisi, _, err := ZapisiLetvi(baza)
	if err != nil || zapisi["donja"].Metoda != MetodaDnevni || len(zapisi["donja"].Promjene) != 1 {
		t.Fatalf("primijenjen dnevni model: %+v, %v", zapisi["donja"], err)
	}
	if c, err := zapisi["donja"].DnevniCiljZapisa(); err != nil || !slices.Equal(c.Ulazi, []string{"gornja"}) {
		t.Errorf("cilj primijenjenog modela: %+v, %v", c, err)
	}
	// satni nacrt se ne proba kao dnevni; pokvaren blok i nečitljiva arhiva javljaju grešku
	if err := SpremiNacrt(baza, nacrtPotoka(), OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbajDnevniNacrt(arhiva, baza, nil, "potok-dole", "sada"); !porukaIma(err, "nema nacrt dnevnog modela") {
		t.Errorf("satni nacrt: %v", err)
	}
	los, err := json.Marshal(NacrtLetve{Zapis: ZapisLetve{Letva: "losa", Metoda: MetodaDnevni, Dnevni: json.RawMessage(`"x"`)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO model_nacrt (letva, sadrzaj) VALUES ('losa', ?)`, string(los)); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbajDnevniNacrt(arhiva, baza, nil, "losa", "sada"); !porukaIma(err, "ne da pročitati") {
		t.Errorf("pokvaren blok: %v", err)
	}
	if err := SpremiNacrt(baza, nacrt, OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	if err := arhiva.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbajDnevniNacrt(arhiva, baza, nil, "donja", "sada"); err == nil {
		t.Error("zatvorena arhiva mora javiti grešku")
	}
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbajDnevniNacrt(arhiva, baza, nil, "donja", "sada"); err == nil || MetodaNacrta(baza, "donja") != "" {
		t.Error("zatvorena baza mora javiti grešku i nema metode")
	}
	if zadnjiDan(nil) != 0 {
		t.Error("prazan niz nema zadnjeg dana")
	}
}
