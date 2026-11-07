package prognoza

import (
	"database/sql"
	"slices"
	"testing"
	"time"
)

// Letve u ovim testovima su izmišljene, a nizovi računati. Stvarne šifre (letenye,
// komarom) stoje samo zato što ih dnevni model prepoznaje po imenu.

// zapisMjere je račun potok-dole iz ulaza, kakav daje priprema: potok-dole je dvostruki
// ulaz od prije pet sati uz tri, a odsječak račun drži deset iznad toga. Ispravak
// iz zadnjeg mjerenja većinu toga skine, a ostatak je izmjereni pomak.
func zapisMjere(ulaz string) ZapisLetve {
	z := zapisZaPripremu(ulaz)
	z.Racun.Doseg = 5
	z.Brojevi.Inacice = [][]Pojas{{{Letva: "potok-dole", Velicina: "vodostaj", Od: -1000, Do: 1000, Odsjecak: 13, Sati: 5000,
		Ulazi: []Ulaz{{Letva: ulaz, Velicina: "vodostaj", PomakH: 5, Sirina: 1, Nagib: 2}}}}}
	return z
}

// arhivaMjere ima potok-dole i ulaz.
func arhivaMjere(t *testing.T, ulaz string) *sql.DB {
	t.Helper()
	return arhivaSatnih(t, map[string]map[int64]float64{
		ulaz + ":vodostaj":    satniNiz(0, satiPotoka, valUlaza),
		"potok-dole:vodostaj": satniNiz(0, satiPotoka, func(h int64) float64 { return 2*valUlaza(h-5) + 3 }),
	})
}

func opcijeMjere() OpcijeProvjere {
	return OpcijeMjere(UcenjeRacuna{OdSata: 10, DoSata: satiPotoka - 1})
}

func mjeri(t *testing.T, arhiva, baza *sql.DB, z ZapisLetve, o OpcijeProvjere) MjeraRacuna {
	t.Helper()
	m, err := MjeriLetvu(arhiva, baza, otvoriProbnu(t, "privremena.db"), z, o)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestOcijeniPromasaj(t *testing.T) {
	for _, c := range []struct {
		ime   string
		p     Promasaj
		zelim StanjeMjere
	}{
		{"bez promašaja", Promasaj{}, MjeraNijeProvjereno},
		{"99 slučajeva", Promasaj{Rasap: 1, Postojanost: 9, Slucaja: 99}, MjeraNijeProvjereno},
		{"100 slučajeva", Promasaj{Rasap: 1, Postojanost: 9, Slucaja: 100}, MjeraBolje},
		{"lošije", Promasaj{Rasap: 9, Postojanost: 1, Slucaja: 100}, MjeraLosije},
		{"jednako nije bolje", Promasaj{Rasap: 5, Postojanost: 5, Slucaja: 100}, MjeraLosije},
		{"bez čega usporediti", Promasaj{Rasap: 1, Slucaja: 100}, MjeraNijeProvjereno},
	} {
		if s := OcijeniPromasaj(c.p); s != c.zelim {
			t.Errorf("%s: %q, želim %q", c.ime, s, c.zelim)
		}
	}
}

// Račun koji prati ulaz bolji je nego bez modela, a brojevi u zapisu sežu do njegova
// dosega. Baza prognoza se ne dira.
func TestMjeriLetvu(t *testing.T) {
	arhiva, baza := arhivaMjere(t, "potok-vrh"), otvoriProbnu(t, "p.db")
	o := opcijeMjere()
	m := mjeri(t, arhiva, baza, zapisMjere("potok-vrh"), o)
	if len(m.Dosezi) != 1 || m.Dosezi[0].Doseg != 5 {
		t.Fatalf("dosezi %+v, želim jedino doseg računa", m.Dosezi)
	}
	d := m.Dosezi[0]
	if d.Stanje != MjeraBolje || d.Slucaja < 100 || d.BezModela <= 0 {
		t.Errorf("doseg %+v", d)
	}
	if d.Pomak <= 0 {
		t.Errorf("pomak %v, račun je iznad vode", d.Pomak)
	}
	if p := m.Promasaji[5]; p.Pomak != d.Pomak {
		t.Errorf("pomak u zapisu %v, izmjereno %v: dnevni model ovdje ne sudjeluje", p.Pomak, d.Pomak)
	}
	for doseg := range m.Promasaji {
		if doseg > 5 {
			t.Errorf("promašaj za doseg %d, a račun seže do 5", doseg)
		}
	}
	if len(m.Natpisi) != 0 {
		t.Errorf("natpisi %v", m.Natpisi)
	}
	if want := []string{o.Od.Format(time.DateOnly), o.Do.Format(time.DateOnly)}; !slices.Equal(m.Razdoblje, want) {
		t.Errorf("razdoblje %v, želim %v", m.Razdoblje, want)
	}
	for _, tablica := range []string{"pojasi", "promasaji", "model_letve"} {
		var n int
		if err := baza.QueryRow(`SELECT count(*) FROM ` + tablica).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s u bazi prognoza: %d, %v", tablica, n, err)
		}
	}
	z := zapisMjere("potok-vrh")
	z.Brojevi.Promasaji, z.Brojevi.Razdoblje = m.Promasaji, m.Razdoblje
	if err := SpremiZapisLetve(baza, z, "otisak", OkvirModela{}); err != nil {
		t.Errorf("izmjereni brojevi se ne daju spremiti: %v", err)
	}
}

// Promašaj na dosegu računa ne vidi sate iza njega. Potok-dole do pet sati unaprijed ima
// izmjeren ulaz, a poslije promašuje više. Da glačanje gleda te sate, rasap bi bio veći.
func TestMjeraNeVidiSateIzaDosega(t *testing.T) {
	arhiva, baza := arhivaMjere(t, "potok-vrh"), otvoriProbnu(t, "p.db")
	duzi := zapisMjere("potok-vrh")
	duzi.Racun.Doseg = 11
	na5 := mjeri(t, arhiva, baza, zapisMjere("potok-vrh"), opcijeMjere()).Promasaji[5]
	saDuljim := mjeri(t, arhiva, baza, duzi, opcijeMjere()).Promasaji[5]
	if na5.Rasap >= saDuljim.Rasap {
		t.Errorf("rasap na 5 h: %v, a uz doseg 11 h %v", na5.Rasap, saDuljim.Rasap)
	}
}

// Razdoblje dolazi izvana. Premalo izdanja znači da račun nije provjeren, a ne da je
// dobar. Isto vrijedi za razdoblje bez ijednog izdanja.
func TestMjeraBezDostaSlucajeva(t *testing.T) {
	arhiva, baza := arhivaMjere(t, "potok-vrh"), otvoriProbnu(t, "p.db")
	sat := func(h int64) time.Time { return time.Unix(h*3600, 0).UTC() }
	for ime, o := range map[string]OpcijeProvjere{
		"kratko":     {Od: sat(100), Do: sat(100 + 12*50), Korak: 12, Najdalje: 96, Glacenje: 6},
		"izvan niza": {Od: sat(satiPotoka + 500), Do: sat(satiPotoka + 5000), Korak: 12, Najdalje: 96, Glacenje: 6},
	} {
		m := mjeri(t, arhiva, baza, zapisMjere("potok-vrh"), o)
		if len(m.Dosezi) != 1 || m.Dosezi[0].Stanje != MjeraNijeProvjereno || m.Dosezi[0].Slucaja != 0 {
			t.Errorf("%s: %+v", ime, m.Dosezi)
		}
		if len(m.Promasaji) != 0 {
			t.Errorf("%s: promašaji %v", ime, m.Promasaji)
		}
	}
}

// Račun koji uzvodno dira ono čemu uživo pomaže dnevni model nosi natpis, a izmjereni
// pomak ostaje samo u retku probe. Rasap u zapisu ostaje.
func TestMjeraBezDnevnogModela(t *testing.T) {
	for _, ulaz := range []string{"letenye", "komarom"} {
		arhiva, baza := arhivaMjere(t, ulaz), otvoriProbnu(t, "p.db")
		m := mjeri(t, arhiva, baza, zapisMjere(ulaz), opcijeMjere())
		if !slices.Contains(m.Natpisi, NatpisBezDnevnog) {
			t.Errorf("%s: natpisi %v", ulaz, m.Natpisi)
		}
		if m.Dosezi[0].Pomak <= 0 {
			t.Errorf("%s: pomak u retku probe %v", ulaz, m.Dosezi[0].Pomak)
		}
		p, ima := m.Promasaji[5]
		if !ima || p.Pomak != 0 || p.Slucaja < 100 || p.Rasap != m.Dosezi[0].Rasap {
			t.Errorf("%s: promašaj u zapisu %+v, %v", ulaz, p, ima)
		}
	}
}

// Dnevni model se traži kroz sve račune uzvodno: glavni lanac, drugi zapis i rezervu.
func TestMjeraGledaCijeliPutUzvodno(t *testing.T) {
	sredina := func(ulaz string) []Pojas {
		return []Pojas{{Letva: "potok-sredina", Velicina: "vodostaj", Od: -1000, Do: 1000, Sati: 5000,
			Ulazi: []Ulaz{{Letva: ulaz, Velicina: "vodostaj", PomakH: 1, Sirina: 1, Nagib: 1}}}}
	}
	nizovi := map[string]map[int64]float64{
		"letenye:vodostaj":       satniNiz(0, satiPotoka, valUlaza),
		"potok-sredina:vodostaj": satniNiz(0, satiPotoka, valUlaza),
		"potok-dole:vodostaj":    satniNiz(0, satiPotoka, valUlaza),
	}
	natpisi := func(t *testing.T, baza *sql.DB, z ZapisLetve) []string {
		t.Helper()
		return mjeri(t, arhivaSatnih(t, nizovi), baza, z, opcijeMjere()).Natpisi
	}
	t.Run("glavni lanac", func(t *testing.T) {
		baza := otvoriProbnu(t, "p.db")
		if err := Spremi(baza, sredina("letenye"), "proba"); err != nil {
			t.Fatal(err)
		}
		if n := natpisi(t, baza, zapisMjere("potok-sredina")); !slices.Contains(n, NatpisBezDnevnog) {
			t.Errorf("natpisi %v", n)
		}
	})
	t.Run("drugi zapis", func(t *testing.T) {
		baza := otvoriProbnu(t, "p.db")
		d := zapisZaPripremu("letenye")
		d.Letva, d.Racun.Doseg, d.Brojevi.Inacice = "potok-sredina", 1, [][]Pojas{sredina("letenye")}
		if err := SpremiZapisLetve(baza, d, "otisak", OkvirModela{}); err != nil {
			t.Fatal(err)
		}
		if n := natpisi(t, baza, zapisMjere("potok-sredina")); !slices.Contains(n, NatpisBezDnevnog) {
			t.Errorf("natpisi %v", n)
		}
	})
	t.Run("rezerva", func(t *testing.T) {
		z := zapisMjere("potok-sredina")
		z.Racun.Rezerve = [][]string{{"letenye"}}
		rez := []Pojas{{Letva: "potok-dole", Velicina: "vodostaj", Od: -1000, Do: 1000, Sati: 5000, Inacica: 1,
			Ulazi: []Ulaz{{Letva: "letenye", Velicina: "vodostaj", PomakH: 1, Sirina: 1, Nagib: 1}}}}
		z.Brojevi.Inacice = append(z.Brojevi.Inacice, rez)
		if n := natpisi(t, otvoriProbnu(t, "p.db"), z); !slices.Contains(n, NatpisBezDnevnog) {
			t.Errorf("natpisi %v", n)
		}
	})
	t.Run("bez dnevnog", func(t *testing.T) {
		baza := otvoriProbnu(t, "p.db")
		if n := natpisi(t, baza, zapisMjere("potok-sredina")); slices.Contains(n, NatpisBezDnevnog) {
			t.Errorf("natpisi %v", n)
		}
	})
}

// U privremenu bazu idu računi samo za put uzvodno, a modeli ispuštanja svih elektrana,
// jer se budućnost elektrane računa i iz budućnosti ostalih.
func TestMjeraPrepisujeRacuneUzvodnoIOperatereSve(t *testing.T) {
	arhiva, baza := arhivaMjere(t, "he-gornja"), otvoriProbnu(t, "p.db")
	for _, l := range []string{"he-gornja", "he-druga"} {
		if err := SpremiOperatera(baza, &OperaterModel{Letva: l}); err != nil {
			t.Fatal(err)
		}
	}
	tudi := []Pojas{{Letva: "rijeka-druga", Velicina: "vodostaj", Od: -1000, Do: 1000, Sati: 5000,
		Ulazi: []Ulaz{{Letva: "rijeka-vrh", Velicina: "vodostaj", PomakH: 1, Sirina: 1, Nagib: 1}}}}
	if err := Spremi(baza, tudi, "proba"); err != nil {
		t.Fatal(err)
	}
	privremena := otvoriProbnu(t, "privremena.db")
	if _, err := MjeriLetvu(arhiva, baza, privremena, zapisMjere("he-gornja"), opcijeMjere()); err != nil {
		t.Fatal(err)
	}
	operateri, err := UcitajOperatere(privremena)
	if err != nil {
		t.Fatal(err)
	}
	if len(operateri) != 2 || operateri["he-gornja"] == nil || operateri["he-druga"] == nil {
		t.Errorf("operateri %v", operateri)
	}
	inacice, err := SveInacice(privremena)
	if err != nil {
		t.Fatal(err)
	}
	if len(inacice) != 1 || inacice["potok-dole"] == nil {
		t.Errorf("računi %v", inacice)
	}
}

func TestPremaloZaNajvisuVodu(t *testing.T) {
	pojasi := func(najvisa int, iskljucena bool) []Pojas {
		return []Pojas{{Sati: 5000}, {Sati: najvisa, Nepovezan: iskljucena}}
	}
	for _, c := range []struct {
		ime     string
		pojasi  []Pojas
		premalo bool
	}{
		{"1.199 sati", pojasi(1199, false), true},
		{"1.200 sati", pojasi(1200, false), false},
		{"isključena se ne gleda", pojasi(10, true), false},
		{"sve isključeno", []Pojas{{Sati: 5000, Nepovezan: true}}, true},
	} {
		if p := premaloNajvise(c.pojasi, 12); p != c.premalo {
			t.Errorf("%s: %v", c.ime, p)
		}
	}
	arhiva, baza := arhivaMjere(t, "potok-vrh"), otvoriProbnu(t, "p.db")
	z := zapisMjere("potok-vrh")
	z.Brojevi.Inacice[0][0].Sati = 1000
	if m := mjeri(t, arhiva, baza, z, opcijeMjere()); !slices.Contains(m.Natpisi, NatpisPremaloNajvisa) {
		t.Errorf("natpisi %v", m.Natpisi)
	}
}

func TestDoseziMjere(t *testing.T) {
	for doseg, zelim := range map[int][]int{1: {1}, 6: {6}, 30: {6, 12, 24, 30}, 96: {6, 12, 24, 48, 72, 96}} {
		if d := doseziMjere(doseg); !slices.Equal(d, zelim) {
			t.Errorf("doseg %d: %v, želim %v", doseg, d, zelim)
		}
	}
}

// Razdoblje je vrijeme učenja, a dug niz mjeri se samo na zadnje tri godine.
func TestOpcijeMjere(t *testing.T) {
	sat := func(g int) int64 { return time.Date(g, 6, 1, 0, 0, 0, 0, time.UTC).Unix() / 3600 }
	o := OpcijeMjere(UcenjeRacuna{OdSata: sat(2025), DoSata: sat(2026)})
	if o.Od.Year() != 2025 || o.Do.Year() != 2026 || o.Korak != ZadaneOpcijeProvjere().Korak {
		t.Errorf("kratak niz: %+v", o)
	}
	o = OpcijeMjere(UcenjeRacuna{OdSata: sat(1990), DoSata: sat(2026)})
	if o.Od.Year() != 2023 || o.Do.Year() != 2026 {
		t.Errorf("dug niz: %v do %v", o.Od, o.Do)
	}
}

// Račun s greškom se ne mjeri, a privremena baza ostaje prazna.
func TestMjeraOdbija(t *testing.T) {
	arhiva, baza := arhivaMjere(t, "potok-vrh"), otvoriProbnu(t, "p.db")
	sat := time.Unix(5000*3600, 0).UTC()
	glavna := zapisMjere("potok-vrh")
	glavna.Letva = "belisce"
	bezBrojeva := zapisMjere("potok-vrh")
	bezBrojeva.Brojevi = BrojeviLetve{}
	for ime, c := range map[string]struct {
		z ZapisLetve
		o OpcijeProvjere
	}{
		"bez brojeva":    {bezBrojeva, opcijeMjere()},
		"letva lanca":    {glavna, opcijeMjere()},
		"prazno vrijeme": {zapisMjere("potok-vrh"), OpcijeProvjere{Korak: 12}},
		"obrnuto":        {zapisMjere("potok-vrh"), OpcijeProvjere{Od: sat, Do: sat.Add(-time.Hour), Korak: 12}},
	} {
		privremena := otvoriProbnu(t, "privremena.db")
		if _, err := MjeriLetvu(arhiva, baza, privremena, c.z, c.o); err == nil {
			t.Errorf("%s: nije odbijeno", ime)
		}
		var n int
		if err := privremena.QueryRow(`SELECT count(*) FROM pojasi`).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s: u privremenoj bazi %d računa, %v", ime, n, err)
		}
	}
}

// Greška baze vraća se pozivatelju, a ne daje mjeru bez brojeva.
func TestMjeraGreskeBaze(t *testing.T) {
	arhiva := arhivaMjere(t, "he-gornja")
	izvrsi := func(t *testing.T, db *sql.DB, upit string) {
		t.Helper()
		if _, err := db.Exec(upit); err != nil {
			t.Fatal(err)
		}
	}
	for ime, pokvari := range map[string]func(t *testing.T, baza, privremena *sql.DB, o *OpcijeProvjere){
		"baza zatvorena": func(_ *testing.T, baza, _ *sql.DB, _ *OpcijeProvjere) { baza.Close() },
		"zapisi se ne daju čitati": func(t *testing.T, baza, _ *sql.DB, _ *OpcijeProvjere) {
			izvrsi(t, baza, `DROP TABLE model_letve`)
			izvrsi(t, baza, `CREATE TABLE model_letve (nesto TEXT)`)
		},
		"privremena zatvorena": func(_ *testing.T, _, privremena *sql.DB, _ *OpcijeProvjere) { privremena.Close() },
		"operateri se ne daju čitati": func(t *testing.T, baza, _ *sql.DB, _ *OpcijeProvjere) {
			izvrsi(t, baza, `DROP TABLE operateri`)
		},
		"operater se ne da upisati": func(t *testing.T, baza, privremena *sql.DB, _ *OpcijeProvjere) {
			if err := SpremiOperatera(baza, &OperaterModel{Letva: "he-gornja"}); err != nil {
				t.Fatal(err)
			}
			izvrsi(t, privremena, `DROP TABLE operater`)
		},
		"provjera odbija korak": func(_ *testing.T, _, _ *sql.DB, o *OpcijeProvjere) { o.Korak = 0 },
		"promašaji se ne daju čitati": func(t *testing.T, _, privremena *sql.DB, _ *OpcijeProvjere) {
			izvrsi(t, privremena, `CREATE TRIGGER pokvari AFTER INSERT ON promasaji BEGIN
				UPDATE promasaji SET doseg_h = 'nije broj ' || NEW.doseg_h
				WHERE letva = NEW.letva AND velicina = NEW.velicina AND doseg_h = NEW.doseg_h; END`)
		},
	} {
		t.Run(ime, func(t *testing.T) {
			baza, privremena := otvoriProbnu(t, "p.db"), otvoriProbnu(t, "privremena.db")
			o := opcijeMjere()
			pokvari(t, baza, privremena, &o)
			if m, err := MjeriLetvu(arhiva, baza, privremena, zapisMjere("he-gornja"), o); err == nil {
				t.Errorf("nema greške: %+v", m)
			}
		})
	}
}
