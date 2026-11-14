package web

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/dhmz"
	"gocop/internal/models"
	"gocop/internal/prognoza"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// sadNaslovne je trenutak u testovima kartica: 2. 11. 2026. u 10 h
var sadNaslovne = time.Date(2026, 11, 2, 10, 0, 0, 0, models.Zagreb)

func cmNaslovne(v int) *int { return &v }

// Vodostaji: samo letve iznad praga s aktualnim očitanjem; promjena u 24 h
// računa se samo za retke koji se vide, a staro očitanje je sivo
func TestKarticaVodostaja(t *testing.T) {
	letva := func(ime string, cm int, faza models.DefensePhase, prije time.Duration) models.GaugeSummary {
		return models.GaugeSummary{Name: ime, URL: "/l/" + ime, Phase: faza,
			Latest: &models.Reading{LevelCm: cmNaslovne(cm), MeasuredAt: sadNaslovne.Add(-prije)}}
	}
	letve := []models.GaugeSummary{
		letva("Batina", 312, models.PhasePrep, 3*time.Hour),
		letva("Stara", 250, models.PhasePrep, 40*time.Hour),
		letva("Zaboravljena", 300, models.PhaseRegular, 30*24*time.Hour),
		letva("Mirna", 100, models.PhaseNormal, time.Hour),
		{Name: "Bez očitanja", Phase: models.PhasePrep},
		{Name: "Bez vodostaja", Phase: models.PhasePrep, Latest: &models.Reading{MeasuredAt: sadNaslovne}},
	}
	for i := 0; i < 6; i++ {
		letve = append(letve, letva("Dodatna", 200, models.PhasePrep, time.Hour))
	}
	pitano := 0
	prije := func(g models.GaugeSummary) *int {
		pitano++
		if g.Name == "Batina" {
			return cmNaslovne(301)
		}
		if g.Name == "Stara" {
			return cmNaslovne(260)
		}
		return nil
	}
	k := karticaVodostaja(letve, prije, sadNaslovne)
	if len(k.Redovi) != 8 || pitano != NajviseRedaka {
		t.Fatalf("redaka %d, pitano za promjenu %d", len(k.Redovi), pitano)
	}
	b := k.Redovi[0]
	if b.Glavno != "Batina" || b.Sporedno != "07:00 · +11 cm u 24 h" || b.Vrijednost != "312 cm" || b.Ikona != "trending-up" ||
		b.Oznaka == nil || b.Oznaka.Razred != "prep" || !b.Hitno || b.Staro || b.URL != "/l/Batina" {
		t.Errorf("Batina: %+v", b)
	}
	s := k.Redovi[1]
	if !s.Staro || s.Ikona != "trending-down" || !strings.HasPrefix(s.Sporedno, "31. 10. 18:00 · -10 cm") {
		t.Errorf("staro očitanje: %+v", s)
	}
	if k.Redovi[2].Ikona != "minus" || k.Vise() != "Više: još 3, od toga 3 iznad praga" {
		t.Errorf("bez promjene ili Više: %+v, %q", k.Redovi[2], k.Vise())
	}
	if p := karticaVodostaja(letve[3:6], prije, sadNaslovne); p.Prazno == nil || p.Prazno.Vrsta != PraznoGotovo {
		t.Errorf("nijedna iznad praga: %+v", p)
	}
}

// Jače oborine: međusliv iznad praga s palom i očekivanom kišom, učestalošću
// i najvećim porastom letve; bez podataka kartica to kaže
func TestKarticaJacihOborina(t *testing.T) {
	p := kisaSlivovaPodaci{Upozorenja: []SlivUKisi{
		{StanjeSliva: prognoza.StanjeSliva{Sliv: "vuka", Palo24: 12, Razina: 1}},
		{StanjeSliva: prognoza.StanjeSliva{Sliv: "drava-dm", Palo24: 0, Palo72: 52, Dolazi48: 15, Razina: 2,
			Zasto: "palo u zadnja 72 sata", Ucestalost: "toliko padne u prosjeku jednom godišnje"},
			Naziv: "Drava: Donji Miholjac – Osijek", Letve: []PorastLetve{{Naziv: "Bez", Ima: false}, {Naziv: "Osijek", Cm: 45, Ima: true}}},
	}}
	k := karticaJacihOborina(p, sadNaslovne)
	// jača kiša na vrhu, iako je u podacima druga; pokazana su sva tri
	// razdoblja i razdoblje u kojem je prag prijeđen
	if len(k.Redovi) != 2 || k.Redovi[0].Glavno != "Drava: Donji Miholjac – Osijek" ||
		k.Redovi[0].Sporedno != "palo 52 mm u 72 h, 0 mm u 24 h · očekivano 15 mm u 48 h · "+
			"toliko padne u prosjeku jednom godišnje (palo u 72 h) · porast u prognozi: Osijek +45 cm" ||
		!k.Redovi[0].Hitno || k.Redovi[0].Istaknuto != "kisa" || k.Redovi[1].Glavno != "vuka" || k.Redovi[1].Hitno ||
		k.Redovi[1].Sporedno != "palo 12 mm u 24 h" {
		t.Errorf("redovi: %+v", k.Redovi)
	}
	if p.Upozorenja[0].Sliv != "vuka" {
		t.Error("kartica ne smije presložiti podatke koje dijeli s ostalima")
	}
	// redak vodi na svoj međusliv, „Više” na tablicu kiše; kartica kaže odakle je broj
	if k.Redovi[0].URL != "/slivovi#sliv-drava-dm" || k.URL != "/slivovi#kisa" || k.Napomena != napomenaTrakeKise+" "+napomenaKiseSlivova {
		t.Errorf("poveznice i napomena: %q %q %q", k.Redovi[0].URL, k.URL, k.Napomena)
	}
	if g := karticaJacihOborina(kisaSlivovaPodaci{Greska: "nema"}, sadNaslovne); g.Prazno == nil || g.Prazno.Vrsta != PraznoNedostupno || g.Prazno.Tekst != "nema" {
		t.Errorf("greška: %+v", g)
	}
	if m := karticaJacihOborina(kisaSlivovaPodaci{}, sadNaslovne); m.Prazno == nil || m.Prazno.Vrsta != PraznoGotovo {
		t.Errorf("mirno: %+v", m)
	}
}

// Vrijeme: upozorenja županije pojedinačno, ostale županije u jednom retku,
// pa trenutno vrijeme i prognoza regije
func TestKarticaVremena(t *testing.T) {
	temp, vjetar, kisa := 12.0, 5.6, 6.2 // DHMZ daje vjetar u m/s
	od := time.Date(2026, 11, 2, 14, 0, 0, 0, models.Zagreb)
	p := PlocaVremena{Mjesto: "Osijek", Regija: "istočna Hrvatska",
		Upozorenja: []dhmz.Upozorenje{{Dogadjaj: "kiša", Razina: 2, Podrucje: "Osječko-baranjska", Od: od, Do: od.Add(6 * time.Hour)}},
		Ostala: []dhmz.Upozorenje{
			{Dogadjaj: "Narančasto upozorenje za vjetar", Razina: 3, Podrucje: "Zadarska"}, {Dogadjaj: "Narančasto upozorenje za vjetar", Razina: 3, Podrucje: "Zadarska"},
			{Dogadjaj: "Žuto upozorenje za kišu", Razina: 2, Podrucje: "Zadarska"}, {Dogadjaj: "Narančasto upozorenje za vjetar", Razina: 3, Podrucje: "Velebitski kanal"}},
		Postaja:   &dhmz.Postaja{Opis: "kiša", Temp: &temp, VjetarSmjer: "S", VjetarBrzina: &vjetar},
		Izmjereno: &IzmjerenaKisa{Naziv: "Osijek", Danas: &kisa},
		Prognoza:  strings.Repeat("Oblačno i kišovito ", 20)}
	k := karticaVremena(p)
	if k.Naslov != "Vrijeme · Osijek" || len(k.Redovi) != 4 {
		t.Fatalf("kartica: %q, %+v", k.Naslov, k.Redovi)
	}
	if u := k.Redovi[0]; u.Glavno != "DHMZ · žuto upozorenje: kiša" || u.Sporedno != "Osječko-baranjska · 2. 11. 14:00–20:00" || u.Istaknuto != "dhmz-2" || u.Hitno {
		t.Errorf("upozorenje županije: %+v", u)
	}
	// isto upozorenje dvaput za isto područje broji se jednom; broj je zbroj po područjima
	if o := k.Redovi[1]; o.Glavno != "DHMZ · 3 upozorenja drugdje" || o.Istaknuto != "dhmz-3" ||
		o.Sporedno != "u 2 područja · najviše narančasto" || o.URL != "/prognoze/vrijeme?drugdje=1" {
		t.Errorf("ostale županije: %+v", o)
	}
	if s := k.Redovi[2]; s.Glavno != "12 °C · kiša" || s.Sporedno != "južni vjetar 20 km/h · kišomjer Osijek: 6,2 mm danas" {
		t.Errorf("sada: %+v", s)
	}
	if pr := k.Redovi[3]; pr.Glavno != "Prognoza · istočna Hrvatska" || !strings.HasSuffix(pr.Sporedno, "…") || len([]rune(pr.Sporedno)) > 161 {
		t.Errorf("prognoza: %+v", pr)
	}
	if bez := karticaVremena(PlocaVremena{Postaja: &dhmz.Postaja{Opis: "vedro"}}); bez.Naslov != "Vrijeme" || bez.Redovi[0].Glavno != "vedro" || bez.Redovi[0].Sporedno != "" {
		t.Errorf("bez mjesta i temperature: %+v", bez)
	}
	if n := karticaVremena(PlocaVremena{}); n.Prazno == nil || n.Prazno.Vrsta != PraznoNedostupno {
		t.Errorf("bez podataka: %+v", n)
	}
	if skrati("kratko", 10) != "kratko" || skrati("dugackarijecbezrazmaka", 5) != "dugac…" {
		t.Error("skraćivanje teksta")
	}
}

// Sustav: razmjena, prognoza i podaci po jedan redak
func TestKarticaSustava(t *testing.T) {
	prije := sadNaslovne.Add(-2 * time.Minute)
	dobar := CvorRazmjene{Dostupnost: "online", Zadnja: &prije}
	r := RazmjenaStanje{UMrezi: true, Cvorovi: []CvorRazmjene{dobar, dobar},
		Prognoza: PrognozaRazmjene{Ima: true, Izdavac: "ured", Izdano: sadNaslovne.Add(-time.Hour), Nastalo: time.Now()}}
	b := &BrojkePodataka{Zapisa: 48_300_000, Letvi: 212, NajstarijiDan: "1. 1. 1900."}
	k := karticaSustava(r, b, sadNaslovne)
	if len(k.Redovi) != 3 || k.Redovi[0].Glavno != "Razmjena: 2 računala usklađena" || k.Redovi[0].Sporedno != "zadnja danas 09:58" ||
		k.Redovi[1].Glavno != "Prognoza izdana danas 09:00" || k.Redovi[1].Sporedno != "izračunalo računalo ured" || k.Redovi[1].Hitno ||
		k.Redovi[2].Sporedno != "48,3 mil. zapisa · 212 letvi · vodostaji od 1. 1. 1900." {
		t.Errorf("sve u redu: %+v", k.Redovi)
	}
	r.Cvorovi = append(r.Cvorovi, CvorRazmjene{Dostupnost: "offline"})
	r.Upozorenja = []string{"Ured se ne javlja."}
	r.Prognoza.Nastalo = time.Now().Add(-4 * time.Hour)
	k = karticaSustava(r, &BrojkePodataka{Zapisa: 900, Letvi: 1}, sadNaslovne)
	if k.Redovi[0].Glavno != "Razmjena: 2 od 3 računala usklađeno" || k.Redovi[0].Sporedno != "Ured se ne javlja." || !k.Redovi[0].Hitno ||
		!k.Redovi[1].Hitno || k.Redovi[2].Sporedno != "900 zapisa · 1 letva" {
		t.Errorf("s upozorenjima: %+v", k.Redovi)
	}
	if v := karticaSustava(RazmjenaStanje{}, nil, sadNaslovne); len(v.Redovi) != 1 || v.Redovi[0].Glavno != "Razmjena: računalo nije u mreži" {
		t.Errorf("izvan mreže: %+v", v.Redovi)
	}
}

// Za riješiti: sektorsko izvješće u obrani, nepredani listovi po sektoru,
// promjene prava i novi djelatnici koji su se prijavili, a nemaju ulogu
func TestKarticaZaRijesiti(t *testing.T) {
	dan := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, models.Zagreb) }
	prijava := sadNaslovne.AddDate(0, 0, -1)
	novi := models.User{ID: uuid.New(), FullName: "Ivo Proba", IsActive: true, LastLoginAt: &prijava}
	s := stavkeZaRijesiti{
		Stanja: []service.StanjeObrane{{Sektor: "B", Stadij: models.PhasePrep}, {Sektor: "C", Stadij: models.PhasePrep, Sektorsko: true}, {Sektor: "D", Stadij: models.PhaseNormal}},
		Nepredani: map[string][]models.VodocuvarskiList{
			"C": {{Datum: dan(30)}},
			"B": {{Datum: dan(30)}, {Datum: dan(28)}, {Datum: dan(29)}},
		},
		PravaCeka: 2, PravaNacin: repository.OdlukaKljucem,
		BezUloge: []models.User{novi},
	}
	k := karticaZaRijesiti(s, sadNaslovne)
	if k.Broj != 5 || len(k.Redovi) != 5 {
		t.Fatalf("stavki %d: %+v", k.Broj, k.Redovi)
	}
	for i, zeli := range []string{"Sektorsko izvješće B još nije sastavljeno", "3 dnevna lista nisu predana", "1 dnevni list nije predan",
		"2 promjene prava čekaju potvrdu", "Novi djelatnik čeka ulogu"} {
		if k.Redovi[i].Glavno != zeli {
			t.Errorf("redak %d: %q, želi %q", i, k.Redovi[i].Glavno, zeli)
		}
	}
	if k.Redovi[1].Sporedno != "Sektor B · najstariji od 28. 10." || k.Redovi[1].URL != "/vodocuvar?sektor=B" ||
		k.Redovi[3].Sporedno != "Odluči na računalu s ključem mreže" || k.Redovi[4].Sporedno != "Ivo Proba, prijavio se jučer" {
		t.Errorf("pojedinosti: %+v", k.Redovi)
	}
	if r := redakPrava(1, repository.OdlukaOvdje); r.Glavno != "1 promjena prava čeka potvrdu" || r.Sporedno != "Odluči u Sinkronizaciji" {
		t.Errorf("prava ovdje: %+v", r)
	}
	vise, _ := redakBezUloge([]models.User{novi, novi, novi, novi}, sadNaslovne)
	if vise.Glavno != "4 nova djelatnika čekaju ulogu" || vise.Sporedno != "Ivo Proba, Ivo Proba, Ivo Proba" {
		t.Errorf("više novih: %+v", vise)
	}
	if p := karticaZaRijesiti(stavkeZaRijesiti{}, sadNaslovne); p.Broj != 0 || p.Prazno == nil || p.Prazno.Vrsta != PraznoGotovo {
		t.Errorf("ništa za riješiti: %+v", p)
	}
	sDuznoscu := novi
	sDuznoscu.Duties = []models.Duty{{Role: models.RoleWaterGuard, IsActive: true}}
	neaktivan, nikad := novi, novi
	neaktivan.IsActive, nikad.LastLoginAt = false, nil
	if n := noviBezUloge([]models.User{novi, sDuznoscu, neaktivan, nikad}); len(n) != 1 {
		t.Errorf("novi bez uloge: %d", len(n))
	}
	stari := time.Date(2026, 10, 20, 9, 0, 0, 0, models.Zagreb)
	if danNaslovne(sadNaslovne, sadNaslovne) != "danas" || danNaslovne(stari, sadNaslovne) != "20. 10." {
		t.Error("dan kratko")
	}
}

// Kartica s jakom kišom ima posebno tijelo: lanac sa svim međuslivovima
// (i Murom kao pritokom), a retci samo za one iznad praga, s trakom, kišom po
// danima i imenima skrivenih u retku „Više”
func TestKarticaJacihOborinaLanacITraka(t *testing.T) {
	izdano := time.Date(2026, 11, 14, 14, 0, 0, 0, models.Zagreb) // subota
	prag := prognoza.PragoviKise{Dan3: [3]float64{42, 51, 66}}
	sliv := func(o string, razina int, dani [5]float64) SlivUKisi {
		st := prognoza.StanjeSliva{Sliv: o, Ima: true, Razina: razina, Pragovi: prag, Dani: dani,
			ImaDan: [5]bool{true, true, true, true, true}, Palo24: dani[2], Palo72: dani[0] + dani[1] + dani[2], Dolazi48: dani[3] + dani[4]}
		if razina > 0 {
			st.Ucestalost = ucestalostIme[razina]
		}
		return SlivUKisi{StanjeSliva: st, Naziv: "međusliv " + o, Klasa: razineKise[razina][0]}
	}
	d := sliv("D", 2, [5]float64{18, 33, 1, 0, 15})
	d.Letve = []PorastLetve{{Naziv: "Terezino Polje", Cm: 35, Ima: true}}
	p := kisaSlivovaPodaci{Izdano: izdano, Slivovi: []SlivUKisi{sliv("A", 0, [5]float64{10, 13, 0, 0, 4}), sliv("B", 0, [5]float64{}), d}}
	for _, o := range []string{"E", "F", "I", "J", "H"} {
		p.Upozorenja = append(p.Upozorenja, sliv(o, 1, [5]float64{20, 20, 1, 0, 5}))
	}
	p.Upozorenja = append(p.Upozorenja, d)
	p.Slivovi = append(p.Slivovi, p.Upozorenja...)
	k := karticaJacihOborina(p, izdano)
	tijelo, ok := k.Podaci.(KisaNaslovne)
	if k.Tijelo != "kisa" || !ok {
		t.Fatalf("tijelo %q, podaci %T", k.Tijelo, k.Podaci)
	}
	if len(tijelo.Lanac) != 3 || tijelo.Lanac[1].Rijeka != "Mura" || tijelo.Lanac[1].Pritok == "" {
		t.Fatalf("lanac %+v", tijelo.Lanac)
	}
	drava := tijelo.Lanac[0].Polja
	if drava[0].Mm != "23" || drava[0].Klasa != "" || drava[2].Mm != "52" || drava[2].Klasa != "kisa-narancasto" ||
		drava[2].URL != "/slivovi#sliv-D" || drava[5].Mm != "—" {
		t.Errorf("Drava %+v", drava)
	}
	if dunav := tijelo.Lanac[2].Polja; dunav[0].Ime != "uzvodno" || dunav[0].Mm != "—" || dunav[0].URL != "/slivovi#kisa" {
		t.Errorf("uzvodni Dunav %+v", dunav[0])
	}
	r := tijelo.Retci[0]
	if r.Naziv != "međusliv D" || r.Rijetko != "jednom godišnje" ||
		r.Palo != "palo 52 mm u 72 h: čet 18 · pet 33 · danas 1" || r.Ocekivano != "očekivano 15 mm u 48 h: ned 0 · pon 15" ||
		r.Porast != "porast u prognozi: Terezino Polje +35 cm" {
		t.Errorf("redak %+v", r)
	}
	// mjerilo je najviši prag uz 12 % zraka: 66 · 1,12 = 73,92 mm
	if len(r.Dijelovi) != 4 || r.Dijelovi[0].Lijevo != 0 || r.Dijelovi[3].Ocekivano != true || len(r.Crtice) != 3 ||
		r.Crtice[1].Lijevo != 69 || r.Crtice[1].Klasa != "kisa-narancasto" {
		t.Errorf("traka %+v %+v", r.Dijelovi, r.Crtice)
	}
	if tijelo.ViseMobitel != "Više: još 3 · međusliv I, međusliv J, međusliv H" || tijelo.Vise != "Više: još 1 · međusliv H" {
		t.Errorf("više %q / %q", tijelo.Vise, tijelo.ViseMobitel)
	}
}

var ucestalostIme = map[int]string{1: "toliko padne u prosjeku tri puta godišnje", 2: "toliko padne u prosjeku jednom godišnje"}

// Zbirna krivulja na Slivovima: počinje od nule, palo do „sad”, očekivano
// dalje; pragovi su vodoravne crte, a jača kiša je prvi graf
func TestGrafoviKise(t *testing.T) {
	izdano := time.Date(2026, 11, 14, 14, 0, 0, 0, models.Zagreb)
	st := func(o string, r int, d [5]float64) SlivUKisi {
		return SlivUKisi{StanjeSliva: prognoza.StanjeSliva{Sliv: o, Razina: r, Dani: d, ImaDan: [5]bool{true, true, true, true, true},
			Pragovi: prognoza.PragoviKise{Dan3: [3]float64{42, 51, 66}}, Ucestalost: ucestalostIme[r]}, Klasa: razineKise[r][0]}
	}
	g := grafoviKise(&kisaSlivovaPodaci{Izdano: izdano, Upozorenja: []SlivUKisi{st("E", 1, [5]float64{19, 21, 1, 0, 8}), st("D", 2, [5]float64{18, 33, 1, 0, 15})}}, izdano)
	if len(g) != 2 || g[0].Naziv != "D" || g[0].Rijetko != "jednom godišnje" || g[0].URL != "#sliv-D" {
		t.Fatalf("grafovi %+v", g)
	}
	d := g[0]
	if !strings.HasPrefix(d.Palo, "8,146 ") || len(strings.Fields(d.Palo)) != 4 || len(strings.Fields(d.Ocekivano)) != 3 ||
		len(d.Pragovi) != 3 || d.Pragovi[1].Natpis != "1× god. 51" || d.Dani[2].Tekst != "danas" || !d.Dani[2].Danas ||
		d.Zbroj != "palo 52 mm, s očekivanim 67 mm" {
		t.Errorf("graf D %+v", d)
	}
	// krivulja koja prijeđe prag je iznad njegove crte (manji y)
	if d.Tocke[2].Y >= d.Pragovi[1].Y || d.Tocke[0].Y <= d.Pragovi[0].Y {
		t.Errorf("točke %+v prema pragovima %+v", d.Tocke, d.Pragovi)
	}
	if grafoviKise(nil, izdano) != nil {
		t.Error("bez podataka nema grafova")
	}
}
