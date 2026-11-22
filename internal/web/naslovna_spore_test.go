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
			Naziv: "Drava: Donji Miholjac – Osijek", Letve: []PorastLetve{{Naziv: "Bez", Ima: false}, {Naziv: "Osijek", Cm: 45, Dan: 2, Ima: true, Od: -170,
				Kad: time.Date(2026, 11, 16, 12, 0, 0, 0, models.Zagreb)}}},
	}}
	k := karticaJacihOborina(p, sadNaslovne)
	// jača kiša na vrhu, iako je u podacima druga; pokazana su sva tri
	// razdoblja i razdoblje u kojem je prag prijeđen
	if len(k.Redovi) != 2 || k.Redovi[0].Glavno != "Drava: Donji Miholjac – Osijek" ||
		k.Redovi[0].Sporedno != "palo 52 mm u 72 h, 0 mm u 24 h · očekivano 15 mm u 48 h · "+
			"toliko padne u prosjeku jednom godišnje (palo u 72 h) · u prognozi Osijek raste s −170 na −125 cm do pon. 16. 11." ||
		!k.Redovi[0].Hitno || k.Redovi[0].Istaknuto != "kisa" || k.Redovi[1].Glavno != "vuka" || k.Redovi[1].Hitno ||
		k.Redovi[1].Sporedno != "palo 12 mm u 24 h" {
		t.Errorf("redovi: %+v", k.Redovi)
	}
	if p.Upozorenja[0].Sliv != "vuka" {
		t.Error("kartica ne smije presložiti podatke koje dijeli s ostalima")
	}
	// redak vodi na svoj međusliv, „Više” na tablicu kiše; boje objašnjava
	// legenda u tijelu kartice, pa napomene ispod nema
	if k.Redovi[0].URL != "/slivovi#sliv-drava-dm" || k.URL != "/slivovi#kisa" || k.Napomena != "" {
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
	// tko je imao zaduženje, a izgubio ga, nije nov djelatnik
	bivsi := novi
	bivsi.ID, bivsi.FullName = uuid.New(), "Pero Bivši"
	nov, bez := razdvojiBezUloge([]models.User{novi, bivsi}, map[uuid.UUID]bool{bivsi.ID: true})
	if len(nov) != 1 || nov[0].ID != novi.ID || len(bez) != 1 || bez[0].ID != bivsi.ID {
		t.Errorf("razdvajanje: novi %v, bez zaduženja %v", nov, bez)
	}
	if nov, bez := razdvojiBezUloge([]models.User{novi, bivsi}, nil); len(nov) != 2 || len(bez) != 0 {
		t.Error("bez popisa zaduženja svi su novi")
	}
	k = karticaZaRijesiti(stavkeZaRijesiti{BezZaduzenja: []models.User{bivsi}}, sadNaslovne)
	if len(k.Redovi) != 1 || k.Redovi[0].Glavno != "Djelatnik bez zaduženja" || !strings.Contains(k.Redovi[0].Sporedno, "Pero Bivši") ||
		k.Redovi[0].URL != "/users/"+bivsi.ID.String() {
		t.Errorf("redak bez zaduženja: %+v", k.Redovi)
	}
	if r, _ := redakBezZaduzenja([]models.User{bivsi, bivsi, bivsi, bivsi, bivsi}); r.Glavno != "5 djelatnika bez zaduženja" || r.URL != "/users" {
		t.Errorf("više bez zaduženja: %+v", r)
	}
	if _, ima := redakBezZaduzenja(nil); ima {
		t.Error("prazan popis dao je redak")
	}
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
	d.Zasto = "palo u zadnja 72 sata"
	d.Letve = []PorastLetve{{Naziv: "Terezino Polje", Cm: 35, Dan: 3, Ima: true, Od: -340}}
	p := kisaSlivovaPodaci{Izdano: izdano, Slivovi: []SlivUKisi{sliv("A", 0, [5]float64{10, 13, 0, 0, 4}), sliv("B", 0, [5]float64{}), d}}
	for _, o := range []string{"E", "F", "I", "J", "H"} {
		p.Upozorenja = append(p.Upozorenja, sliv(o, 1, [5]float64{20, 20, 1, 0, 5}))
	}
	// Aljmaš je žut tek s očekivanom kišom: polje pokazuje i nju
	p.Upozorenja[3].Dani, p.Upozorenja[3].Palo72, p.Upozorenja[3].Palo24 = [5]float64{0, 0, 3, 16, 20}, 3, 3
	p.Upozorenja[3].Dolazi48, p.Upozorenja[3].Zasto = 36, zastoOcekivano
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
	if a := tijelo.Lanac[2].Polja[3]; a.Mm != "3" || a.Ocekivano != "+36" || a.Klasa != "kisa-zuto" ||
		a.Naslov != "međusliv J: 3 mm u 72 h, očekivano 36 mm u 48 h" || !tijelo.ImaOcekivano() {
		t.Errorf("Aljmaš s očekivanom kišom %+v", a)
	}
	if drava[2].Ocekivano != "" {
		t.Errorf("polje obojeno palom kišom ne pokazuje očekivanu: %+v", drava[2])
	}
	r := tijelo.Retci[0]
	if r.Naziv != "međusliv D" || r.Rijetko != "jednom godišnje u 72 h" ||
		r.Palo != "palo 52 mm u 72 h: čet 18 mm · pet 33 mm · zadnja 24 h 1 mm" ||
		r.Ocekivano != "očekivano 15 mm u 48 h: do ned 14 h 0 mm · do pon 14 h 15 mm" ||
		r.Porast != "u prognozi Terezino Polje raste s −340 na −305 cm do 3. dana" {
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

// Porast se broji od vodostaja u satu izdanja, a ne od prosjeka zadnja 24
// sata: Batina je 16. 11. 2026. bila −53 cm, prosjek dana −67, a prognoza za
// 22. 11. +11, pa je porast 64 cm, ne 78. Bez vodostaja ostaje dan 0.
func TestPorastOdVodostajaUSatuIzdanja(t *testing.T) {
	d := []prognoza.DnevnaIzdana{{Dan: 0, Vrijednost: -67}, {Dan: 1, Vrijednost: -45}, {Dan: 6, Vrijednost: 11}}
	if cm, dan, ok := najveciPorast(d, -53, true); cm != 64 || dan != 6 || !ok {
		t.Errorf("od vodostaja u satu izdanja: %v cm, %d. dan, %v", cm, dan, ok)
	}
	if od := polazistePorasta(d, -53, true); od != -53 {
		t.Errorf("polazište u satu izdanja: %v", od)
	}
	if od := polazistePorasta(d, 0, false); od != -67 {
		t.Errorf("polazište bez vodostaja je dan 0: %v", od)
	}
	if od := polazistePorasta(d[1:], 0, false); od != 0 {
		t.Errorf("bez dana 0 nema polazišta: %v", od)
	}
	if cm, _, _ := najveciPorast(d, 0, false); cm != 78 {
		t.Errorf("bez vodostaja od dana 0: %v cm", cm)
	}
	if cm, dan, _ := najveciPorast(d, 20, true); cm != 0 || dan != 0 {
		t.Errorf("voda iznad cijele prognoze nema porasta: %v cm, %d. dan", cm, dan)
	}
}

// Kad razinu da kiša zadnja 24 h, oznaka to kaže, a traka pokazuje samo tu
// kišu i pragove za 24 h: 20. 11. 2026. „jednom godišnje” stajalo je uz traku
// za 72 h koja nije dosegla nijednu crticu. Kiša iza 48 h ne smije nestati
// iza „0 mm u 48 h”.
func TestKarticaKisaU24hIIza48h(t *testing.T) {
	izdano := time.Date(2026, 11, 20, 13, 0, 0, 0, models.Zagreb) // petak
	s := SlivUKisi{StanjeSliva: prognoza.StanjeSliva{Sliv: "I", Ima: true, Razina: 2, Zasto: zasto24,
		Ucestalost: ucestalostIme[2], Palo24: 24, Palo72: 26, Dani: [5]float64{2, 0, 24, 0, 0},
		ImaDan: [5]bool{true, true, true, true, true}, Poslije48: 15, ImaPoslije48: true,
		Pragovi: prognoza.PragoviKise{Dan1: [3]float64{14, 20, 30}, Dan3: [3]float64{42, 51, 66}}},
		Naziv: "Budimpešta → Mohács", Klasa: razineKise[2][0],
		Letve: []PorastLetve{{Letva: "batina", Naziv: "Batina", Cm: 91, Dan: 6, Ima: true, Od: -55,
			Kad: time.Date(2026, 11, 26, 13, 0, 0, 0, models.Zagreb)}}}
	r := redakKise(s, izdano)
	if r.Rijetko != "jednom godišnje u 24 h" {
		t.Errorf("oznaka %q", r.Rijetko)
	}
	// samo zadnja 24 h, mjerilo 30 · 1,12 = 33,6 mm: 24 mm je iza crtice od 20 mm
	if len(r.Dijelovi) != 1 || r.Dijelovi[0].Naslov != "zadnja 24 h 24 mm" || len(r.Crtice) != 3 ||
		r.Crtice[1].Naslov != "prag za 24 h: jednom godišnje, 20 mm" ||
		r.Dijelovi[0].Lijevo+r.Dijelovi[0].Sirina <= r.Crtice[1].Lijevo {
		t.Errorf("traka %+v %+v", r.Dijelovi, r.Crtice)
	}
	if r.Ocekivano != "očekivano 0 mm u 48 h: do sub 13 h 0 mm · do ned 13 h 0 mm, zatim do pon 13 h 15 mm" {
		t.Errorf("očekivano %q", r.Ocekivano)
	}
	if r.Porast != "u prognozi Batina raste s −55 na 36 cm do čet. 26. 11." {
		t.Errorf("porast %q", r.Porast)
	}
	if !strings.HasSuffix(r.Opis, "prag za 24 h 14 mm") {
		t.Errorf("opis %q", r.Opis)
	}
	// bez kiše iza 48 h nema „zatim”, a ni „−0”
	s.Poslije48 = 0.4
	if r := redakKise(s, izdano); strings.Contains(r.Ocekivano, "zatim") {
		t.Errorf("kiša ispod 1 mm iza 48 h: %q", r.Ocekivano)
	}
	if cmSPredznakom(-0.4) != "0" || cmSPredznakom(-55.2) != "−55" {
		t.Errorf("predznak: %q %q", cmSPredznakom(-0.4), cmSPredznakom(-55.2))
	}
}
