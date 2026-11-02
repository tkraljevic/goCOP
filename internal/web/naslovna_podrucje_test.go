package web

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// dozvoleRukovoditeljaBP su prava rukovoditelja BP 16 u sektoru B
func dozvoleRukovoditeljaBP(uloga models.Role) *models.UserPermissions {
	p := dozvoleNaslovne(false, uloga)
	p.User.Duties[0].SectorID, p.User.Duties[0].AreaID = nizNaslovne("B"), brojNaslovne(16)
	return p
}

// Zamjenik ima isti zaslon kao rukovoditelj; dužnost bez područja ne
// vrijedi
func TestDuznostPodrucja(t *testing.T) {
	for _, uloga := range []models.Role{models.RoleAreaLeader, models.RoleAreaDeputy} {
		if p := dozvoleRukovoditeljaBP(uloga); duznostPodrucja(&p.User, sadNaslovne) == nil || karticeUloge(p)[0] != kPodrucje {
			t.Errorf("%s nema Naslovnu područja: %v", uloga, karticeUloge(p))
		}
	}
	if p := dozvoleNaslovne(false, models.RoleAreaLeader); duznostPodrucja(&p.User, sadNaslovne) != nil {
		t.Error("dužnost bez područja")
	}
}

// Kartica područja: stanje obrane s napomenom o sektoru, vodočuvari na
// terenu i objekti
func TestKarticaPodrucja(t *testing.T) {
	od := time.Date(2026, 10, 30, 6, 15, 0, 0, models.Zagreb)
	dionice := []models.Section{{Code: "B.16.1", AreaID: 16}, {Code: "B.16.2", AreaID: 16}, {Code: "B.34.1", AreaID: 34}}
	p := podaciPodrucja{Podrucje: models.Area{ID: 16, Name: "Mali sliv Baranja"}, Sektor: "B", Dionice: dionice,
		Stanja: map[string]models.StanjeObrane{"B.34.1": {Aktivni: []models.AktivniStadij{{Stupanj: models.PhasePrep, Od: od}}}}, Crpne: 12, Ustave: 1, NaTerenu: 3}
	k := karticaPodrucja(p, sadNaslovne)
	if k.Naslov != "BP 16 · Mali sliv Baranja" || len(k.Redovi) != 3 {
		t.Fatalf("kartica: %+v", k)
	}
	if r := k.Redovi[0]; r.Glavno != "Preventivna obrana" || r.Sporedno != "Sektor B: pripremno stanje na BP 34 od 30. 10." || r.Hitno {
		t.Errorf("stanje: %+v", r)
	}
	if r := k.Redovi[1]; r.Sporedno != "3 vodočuvara s današnjim dnevnim listom" || r.URL != "/vodocuvar?sektor=B&podrucje=16" {
		t.Errorf("na terenu: %+v", r)
	}
	if r := k.Redovi[2]; r.Sporedno != "12 crpnih stanica · 1 ustava · 2 dionice" {
		t.Errorf("objekti: %+v", r)
	}
	p.Stanja = map[string]models.StanjeObrane{"B.16.2": {Aktivni: []models.AktivniStadij{{Stupanj: models.PhaseRegular, Od: od}}}}
	p.NaTerenu, p.Crpne, p.Ustave = 0, 0, 0
	k = karticaPodrucja(p, sadNaslovne)
	if r := k.Redovi[0]; r.Glavno != "Redovna obrana" || r.Istaknuto != "regular" || !r.Hitno || r.Sporedno != "od 30. 10." {
		t.Errorf("područje u obrani: %+v", r)
	}
	if len(k.Redovi) != 3 || k.Redovi[1].Sporedno != "Danas još nitko nije otvorio dnevni list." || k.Redovi[2].Sporedno != "2 dionice" {
		t.Errorf("nitko na terenu, samo dionice: %+v", k.Redovi)
	}
	p.NaTerenu, p.Dionice = -1, nil
	if k := karticaPodrucja(p, sadNaslovne); len(k.Redovi) != 1 {
		t.Errorf("bez dnevnika redak na terenu ostaje: %+v", k.Redovi)
	}
}

// objekt je crpna stanica ili ustava u pregledu letava
func objekt(ime, vrsta string, area int, zadnje, prethodno *models.Reading) models.GaugeSummary {
	return models.GaugeSummary{Name: ime, URL: "/o/" + ime, StructureID: ime, AreaID: area, Kind: vrsta, Latest: zadnje, Previous: prethodno}
}

// Crpne stanice: u retcima one koje rade ili su u kvaru (kvar prvi), ostale
// se prebroje; staro očitanje ne govori o stanju
func TestKarticaCrpnihStanica(t *testing.T) {
	cs := models.StructureKindPumpingStation
	ocitanje := func(stanje string, prije time.Duration) *models.Reading {
		return &models.Reading{StructureState: stanje, MeasuredAt: sadNaslovne.Add(-prije), LevelCm: cmNaslovne(186), Observer: "Ante Proba"}
	}
	stanice := []models.GaugeSummary{
		objekt("CS Draž", cs, 16, ocitanje(models.StructureStateStarting, time.Hour), nil),
		objekt("CS Tikveš", cs, 16, ocitanje(models.StructureStateIdle, time.Hour), nil),
		objekt("CS Bakanka", cs, 16, ocitanje(models.StructureStateFault, 40*time.Hour), nil),
		objekt("CS Topolje", cs, 16, nil, nil),
		objekt("CS Šarkanj", cs, 16, ocitanje(models.StructureStateStarting, 30*24*time.Hour), nil),
	}
	k := karticaCrpnihStanica(stanice, "B", 16, sadNaslovne)
	if k.Poveznica != "Sve 5" || k.URL != "/structures?kind=CRPNA_STANICA&sector=B&area=16" || len(k.Redovi) != 2 {
		t.Fatalf("kartica: %+v", k)
	}
	if r := k.Redovi[0]; r.Glavno != "CS Bakanka" || !r.Hitno || r.Ikona != "triangle-alert" || !r.Staro || !strings.HasPrefix(r.Sporedno, "kvar · ") {
		t.Errorf("kvar: %+v", r)
	}
	if r := k.Redovi[1]; r.Sporedno != "radi · 09:00 · Ante Proba" || r.Vrijednost != "186 cm" || r.Ikona != "crpna-stanica" {
		t.Errorf("radi: %+v", r)
	}
	if k.Napomena != "Još 1 crpna stanica miruje. Za 2 crpne stanice nema očitanja u zadnja tri dana." {
		t.Errorf("napomena: %q", k.Napomena)
	}
	if k := karticaCrpnihStanica(stanice[1:2], "B", 16, sadNaslovne); k.Napomena != "Sve crpne stanice miruju." {
		t.Errorf("sve miruju: %q", k.Napomena)
	}
	if k := karticaCrpnihStanica(nil, "B", 34, sadNaslovne); k.Prazno == nil || k.Prazno.Tekst != "U registru BP 34 nema crpnih stanica." || k.Poveznica != "" {
		t.Errorf("bez stanica: %+v", k)
	}
}

// Napomena ispod redaka: ostali objekti i oni bez svježeg očitanja
func TestNapomenaObjekata(t *testing.T) {
	oblici := [3]string{"ustava bez promjene", "ustave bez promjene", "ustava bez promjene"}
	bez := [3]string{"ustavu", "ustave", "ustava"}
	for _, s := range []struct {
		redaka       bool
		ostali, nema int
		zeli         string
	}{
		{true, 12, 0, "Još 12 ustava bez promjene."},
		{false, 3, 0, "Nijedna se nije mijenjala."},
		{false, 3, 1, "3 ustave bez promjene. Za 1 ustavu nema očitanja u zadnja tri dana."},
		{true, 0, 0, ""},
	} {
		if n := napomenaObjekata(s.redaka, s.ostali, s.nema, oblici, bez, "Nijedna se nije mijenjala."); n != s.zeli {
			t.Errorf("%+v: %q", s, n)
		}
	}
}

// Ustave: u retcima one čija se zapornica promijenila, najnovija prva;
// ostale se prebroje
func TestKarticaUstava(t *testing.T) {
	u := models.StructureKindSluice
	zapornica := func(polozaj string, prije time.Duration) *models.Reading {
		return &models.Reading{Gate: polozaj, MeasuredAt: sadNaslovne.Add(-prije), Observer: "Ivo Proba"}
	}
	ustave := []models.GaugeSummary{
		objekt("Ustava Draž", u, 16, zapornica(models.GateOpen, 3*time.Hour), zapornica(models.GateClosed, 27*time.Hour)),
		objekt("Ustava Kopačevo", u, 16, zapornica(models.GateClosed, time.Hour), zapornica(models.GateOpen, 25*time.Hour)),
		objekt("Ustava Bilje", u, 16, zapornica(models.GateClosed, time.Hour), zapornica(models.GateClosed, 25*time.Hour)),
		objekt("Ustava Vadar", u, 16, nil, nil),
		objekt("Ustava bez zapornice", u, 16, zapornica("", time.Hour), nil),
	}
	k := karticaUstava(ustave, "B", 16, sadNaslovne)
	if len(k.Redovi) != 2 || k.Redovi[0].Glavno != "Ustava Kopačevo" || k.Redovi[0].Ikona != "ustava" || k.Redovi[0].Vrijednost != "" ||
		!strings.HasPrefix(k.Redovi[0].Sporedno, "zatvorena · 09:00") {
		t.Fatalf("promjene: %+v", k.Redovi)
	}
	if k.Napomena != "Još 1 ustava bez promjene. Za 2 ustave nema očitanja u zadnja tri dana." {
		t.Errorf("napomena: %q", k.Napomena)
	}
	if k := karticaUstava(nil, "B", 34, sadNaslovne); k.Prazno == nil || k.Poveznica != "" {
		t.Errorf("bez ustava: %+v", k)
	}
}

// Vodostaji područja: sve letve područja sa svježim očitanjem, iznad praga
// prve i s oznakom; objekti i tuđe letve ne ulaze
func TestKarticaVodostajaPodrucja(t *testing.T) {
	letva := func(ime string, faza models.DefensePhase, podrucja []int, prije time.Duration) models.GaugeSummary {
		return models.GaugeSummary{Name: ime, StationID: ime, AreaIDs: podrucja, Phase: faza,
			Latest: &models.Reading{LevelCm: cmNaslovne(100), MeasuredAt: sadNaslovne.Add(-prije)}}
	}
	letve := []models.GaugeSummary{
		letva("Branjin Vrh", models.PhaseNormal, []int{16}, time.Hour),
		letva("Batina", models.PhasePrep, []int{16, 34}, time.Hour),
		letva("Sisak", models.PhasePrep, []int{50}, time.Hour),
		letva("Zaboravljena", models.PhaseNormal, []int{16}, 10*24*time.Hour),
		objekt("CS Draž", models.StructureKindPumpingStation, 16, &models.Reading{MeasuredAt: sadNaslovne}, nil),
	}
	podrucja := letvePodrucja(letve, 16)
	if len(podrucja) != 3 {
		t.Fatalf("letve područja: %+v", podrucja)
	}
	pozvano := 0
	k := karticaVodostajaPodrucja(podrucja, func(models.GaugeSummary) *int { pozvano++; return cmNaslovne(90) }, 16, sadNaslovne)
	if len(k.Redovi) != 2 || k.Redovi[0].Glavno != "Batina" || k.Redovi[0].Oznaka == nil || !k.Redovi[0].Hitno || pozvano != 2 {
		t.Fatalf("redovi: %+v", k.Redovi)
	}
	if r := k.Redovi[1]; r.Oznaka != nil || r.Hitno || !strings.Contains(r.Sporedno, "+10 cm u 24 h") {
		t.Errorf("letva ispod praga: %+v", r)
	}
	if k := karticaVodostajaPodrucja(nil, nil, 16, sadNaslovne); k.Prazno == nil || k.Prazno.Tekst != "U zadnja tri dana nema očitanja letava BP 16." {
		t.Errorf("bez letava: %+v", k)
	}
}

// Za riješiti rukovoditelja područja: ovjera, nepredani listovi, nove
// prijave svaka u svom retku, starije u jednom
func TestKarticaZaRijesitiPodrucja(t *testing.T) {
	dan := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, models.Zagreb) }
	objavljeno := sadNaslovne.Add(-2 * time.Hour)
	s := stavkePodrucja{
		Ovjera:    []models.VodocuvarskiList{{Ime: "Ana Proba", Datum: dan(31)}, {Ime: "Ivo Proba", Datum: dan(30)}, {Ime: "Ana Proba", Datum: dan(29)}, {Rekonstrukcija: true, Datum: dan(1)}},
		Nepredani: []models.VodocuvarskiList{{Datum: dan(28)}},
		Prijave: []models.PrijavaSTerena{
			{ID: "p1", Vrsta: models.PrijavaPrijava, Naslov: "procjeđivanje", Ime: "Ivo Proba", ObjavljenoAt: &objavljeno},
			{ID: "p2", Vrsta: models.PrijavaPrijava, Naslov: "stara", Datum: time.Date(2024, 1, 19, 0, 0, 0, 0, models.Zagreb)},
			{ID: "p3", Vrsta: models.PrijavaObavijest, Naslov: "starija", Datum: time.Date(2023, 4, 13, 0, 0, 0, 0, models.Zagreb)},
		},
	}
	k := karticaZaRijesitiPodrucja(16, "B", s, sadNaslovne)
	if k.Broj != 4 || len(k.Redovi) != 4 {
		t.Fatalf("kartica: %+v", k)
	}
	if r := k.Redovi[0]; r.Glavno != "3 dnevna lista čekaju ovjeru" || r.Sporedno != "Ana Proba, Ivo Proba · najstariji od 29. 10." || r.URL != "/vodocuvar?sektor=B&podrucje=16&ceka=1" {
		t.Errorf("ovjera: %+v", r)
	}
	if r := k.Redovi[1]; r.Glavno != "1 dnevni list nije predan" || r.Sporedno != "najstariji od 28. 10." {
		t.Errorf("nepredani: %+v", r)
	}
	if r := k.Redovi[2]; !strings.HasSuffix(r.Glavno, ": procjeđivanje") || r.Sporedno != "Ivo Proba · danas 08:00" || r.URL != "/prijave/p1" {
		t.Errorf("nova prijava: %+v", r)
	}
	if r := k.Redovi[3]; r.Glavno != "2 starije prijave s terena nisu riješene" || r.Sporedno != "najstarija od 13. 4. 2023." || r.URL != "/prijave?sektor=B&status=OBJAVLJENA" {
		t.Errorf("starije prijave: %+v", r)
	}
	if k := karticaZaRijesitiPodrucja(16, "B", stavkePodrucja{Ovjera: s.Ovjera[3:]}, sadNaslovne); k.Prazno == nil || k.Broj != 0 {
		t.Errorf("ništa za riješiti: %+v", k)
	}
}

// Zadaci vodočuvarima: zaključeni u zadnja 24 sata najnoviji prvi, pa
// otvoreni od najstarijeg; tuđi zadatak kaže tko ga je zadao
func TestKarticaZadatakaPodrucja(t *testing.T) {
	ja := uuid.NewString()
	sat := func(h int) time.Time { return time.Date(2026, 11, 2, h, 0, 0, 0, models.Zagreb) }
	prije, kasnije, sutra := sat(7), sat(8), sat(0).AddDate(0, 0, 1)
	zadaci := []models.Zadatak{
		{Tekst: "otvoren", UserID: "v1", ZadaoID: ja, ZadanoAt: sat(6), Status: models.ZadatakOtvoren},
		{Tekst: "obavljen", UserID: "v1", ZadanoAt: sat(5), Status: models.ZadatakObavljen, ObavljenoAt: &prije},
		{Tekst: "odbačen", UserID: "v2", ZadanoAt: sat(5), Status: models.ZadatakOdbacen, ObavljenoAt: &kasnije},
		{Tekst: "planiran", UserID: "v2", ZadaoID: "drugi", Zadao: "Ivo Proba", ZadanoAt: sat(7), Za: sutra, Status: models.ZadatakOtvoren},
	}
	imena := map[string]string{"v1": "Ana Proba", "v2": "Iva Proba"}
	k := karticaZadatakaPodrucja(zadaci, imena, ja, "B", 16, sadNaslovne)
	glavna := []string{}
	for _, r := range k.Redovi {
		glavna = append(glavna, r.Glavno)
	}
	if strings.Join(glavna, ",") != "odbačen,obavljen,otvoren,planiran" || k.Gumbi[0].URL != "/vodocuvar#zadaj-zadatak" {
		t.Fatalf("redoslijed: %v %+v", glavna, k.Gumbi)
	}
	if r := k.Redovi[0]; r.Ikona != "x" || r.Sporedno != "Iva Proba · nije obavljen danas 08:00" {
		t.Errorf("odbačen: %+v", r)
	}
	if r := k.Redovi[1]; r.Ikona != "check" || r.Sporedno != "Ana Proba · obavljen danas 07:00" {
		t.Errorf("obavljen: %+v", r)
	}
	if r := k.Redovi[2]; r.Sporedno != "Ana Proba · zadan danas 06:00" {
		t.Errorf("otvoren: %+v", r)
	}
	if r := k.Redovi[3]; r.Sporedno != "Iva Proba · za 3. 11. · zadao Ivo Proba" {
		t.Errorf("planiran: %+v", r)
	}
	if k := karticaZadatakaPodrucja(nil, nil, ja, "B", 16, sadNaslovne); k.Prazno == nil || len(k.Gumbi) != 1 {
		t.Errorf("bez zadataka: %+v", k)
	}
}

// Naslovna rukovoditelja područja kroz rukovatelja: glava kartice područja
// s brojem područja, spore kartice s izvorima i bez njih
func TestNaslovnaRukovoditeljaBP(t *testing.T) {
	o := novaOkolinaNaslovne(t)
	ruk := dozvoleRukovoditeljaBP(models.RoleAreaLeader)
	s := o.zovi("/", ruk).Body.String()
	for _, zeli := range []string{`id="kartica-podrucje"`, ">BP 16<", `data-lijeno="/naslovna/kartica/crpne-stanice"`, `id="kartica-zadaci-podrucja"`} {
		if !strings.Contains(s, zeli) {
			t.Errorf("Naslovna rukovoditelja nema %q", zeli)
		}
	}
	for id, zeli := range map[string]string{
		"podrucje":        "Stanje područja trenutačno nije dostupno.",
		"zadaci-podrucja": "Dnevnik vodočuvara trenutačno nije dostupan.",
		"crpne-stanice":   "Očitanja objekata trenutačno nisu dostupna.",
		"za-rijesiti":     "Nema ništa za riješiti.",
	} {
		if w := o.zovi("/naslovna/kartica/"+id, ruk).Body.String(); !strings.Contains(w, zeli) {
			t.Errorf("%s bez izvora: %s", id, w)
		}
	}
	greska := errors.New("baza zauzeta")
	o.h.SetIzvori(IzvoriNaslovne{
		StanjaSektora: func(context.Context, string) (map[string]models.StanjeObrane, []models.Section, error) {
			return nil, []models.Section{{Code: "B.16.1", AreaID: 16}}, nil
		},
		Podrucja: func(string) ([]models.Area, error) { return []models.Area{{ID: 16, Name: "Mali sliv Baranja"}}, nil },
		Letve: func(context.Context) ([]models.GaugeSummary, error) {
			return []models.GaugeSummary{
				objekt("CS Draž", models.StructureKindPumpingStation, 16, &models.Reading{StructureState: models.StructureStateStarting, MeasuredAt: time.Now()}, nil),
				objekt("Ustava Bilje", models.StructureKindSluice, 16, &models.Reading{Gate: models.GateClosed, MeasuredAt: time.Now()}, nil),
				{Name: "Batina", StationID: "b", AreaIDs: []int{16}, Phase: models.PhaseNormal, Latest: &models.Reading{LevelCm: cmNaslovne(300), MeasuredAt: time.Now()}},
			}, nil
		},
		Listovi: func(_ context.Context, _ *models.UserPermissions, f repository.FiltarListova) ([]models.VodocuvarskiList, error) {
			if f.CekaPotvrdu {
				return []models.VodocuvarskiList{{Ime: "Ana Proba", Datum: time.Now()}}, nil
			}
			return []models.VodocuvarskiList{{UserID: "a", Datum: time.Now()}, {UserID: "a", Datum: time.Now()}, {UserID: "b", Datum: time.Now().AddDate(0, 0, -1)}}, nil
		},
		Nepredani: func(context.Context, *models.UserPermissions, string) ([]models.VodocuvarskiList, error) {
			return []models.VodocuvarskiList{{AreaID: 16, Datum: time.Now()}, {AreaID: 34, Datum: time.Now()}}, nil
		},
		Prijave: func(context.Context, *models.UserPermissions, repository.FiltarPrijava) ([]models.PrijavaSTerena, error) {
			return nil, greska
		},
		ZadaciPodrucja: func(context.Context, *models.UserPermissions, int, time.Time) ([]models.Zadatak, map[string]string, error) {
			return []models.Zadatak{{Tekst: "obići nasip", UserID: "a", ZadanoAt: time.Now(), Status: models.ZadatakOtvoren}}, map[string]string{"a": "Ana Proba"}, nil
		},
	})
	for id, zeli := range map[string][]string{
		"podrucje":        {"BP 16 · Mali sliv Baranja", "1 crpna stanica · 1 ustava · 1 dionica", "1 vodočuvar s današnjim dnevnim listom"},
		"za-rijesiti":     {"1 dnevni list čeka ovjeru", "1 dnevni list nije predan"},
		"zadaci-podrucja": {"obići nasip", "Ana Proba"},
		"crpne-stanice":   {"CS Draž", "radi"},
		"ustave":          {"Nijedna zapornica nije mijenjana"},
		"vodostaji":       {"Batina", "300 cm"},
	} {
		w := o.zovi("/naslovna/kartica/"+id, ruk).Body.String()
		for _, z := range zeli {
			if !strings.Contains(w, z) {
				t.Errorf("%s nema %q: %s", id, z, w)
			}
		}
	}
	o.h.SetIzvori(IzvoriNaslovne{
		StanjaSektora: func(context.Context, string) (map[string]models.StanjeObrane, []models.Section, error) {
			return nil, nil, greska
		},
		Podrucja: func(string) ([]models.Area, error) { return nil, nil },
		Letve:    func(context.Context) ([]models.GaugeSummary, error) { return nil, greska },
		Listovi: func(context.Context, *models.UserPermissions, repository.FiltarListova) ([]models.VodocuvarskiList, error) {
			return nil, greska
		},
		ZadaciPodrucja: func(context.Context, *models.UserPermissions, int, time.Time) ([]models.Zadatak, map[string]string, error) {
			return nil, nil, greska
		},
	})
	for id, zeli := range map[string]string{
		"podrucje":        "Stanje područja trenutačno nije dostupno.",
		"zadaci-podrucja": "Dnevnik vodočuvara trenutačno nije dostupan.",
		"ustave":          "Očitanja objekata trenutačno nisu dostupna.",
	} {
		if w := o.zovi("/naslovna/kartica/"+id, ruk).Body.String(); !strings.Contains(w, zeli) {
			t.Errorf("%s s greškom: %s", id, w)
		}
	}
	if n := o.h.naTerenu(context.Background(), ruk, 16, time.Now()); n != -1 {
		t.Errorf("na terenu s greškom dnevnika: %d", n)
	}
	if c, u := (&DashboardHandler{}).brojObjekata(context.Background(), 16); c != 0 || u != 0 {
		t.Errorf("objekti bez letava: %d %d", c, u)
	}
	bez := dozvoleNaslovne(false, models.RoleAreaLeader)
	if k := o.h.karticaRukovoditeljaBP(zahtjevSDozvolama(bez), bez, kUstave, time.Now()); k.Prazno == nil {
		t.Errorf("bez dužnosti: %+v", k)
	}
	if k := lijenaKarticaPodrucja(bez); k.Naslov != "Branjeno područje" {
		t.Errorf("glava bez dužnosti: %+v", k)
	}
}

// Kartice jedne Naslovne dijele pregled letava: drugi poziv unutar
// PamtiLetve ne pita izvor, greška se ne pamti, a novi izvori brišu
// zapamćeno
func TestLetveSePamte(t *testing.T) {
	h := &DashboardHandler{}
	if _, err := h.letve(context.Background()); err == nil {
		t.Error("bez izvora letava bez greške")
	}
	pitano, greska := 0, errors.New("baza zauzeta")
	izvori := IzvoriNaslovne{Letve: func(context.Context) ([]models.GaugeSummary, error) {
		pitano++
		if pitano == 1 {
			return nil, greska
		}
		return []models.GaugeSummary{{Name: "Batina"}}, nil
	}}
	h.SetIzvori(izvori)
	for range 3 {
		_, _ = h.letve(context.Background()) // prvi poziv vrati grešku, drugi pregled
	}
	if pitano != 2 {
		t.Errorf("izvor pitan %d puta, želi 2", pitano)
	}
	h.SetIzvori(izvori)
	if l, err := h.letve(context.Background()); err != nil || len(l) != 1 || pitano != 3 {
		t.Errorf("nakon novih izvora: %v %v %d", l, err, pitano)
	}
}
