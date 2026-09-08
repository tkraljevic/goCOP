package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gocop/internal/models"
)

// Stranica koja proširuje base.html mora ga i pozvati; bez toga se iscrta
// prazno, a greške nema. Ovo drži da svaka nova stranica ima okvir.
func TestSveStraniceImajuOkvir(t *testing.T) {
	redci, satni, _ := citajZalijepljeno("07.09.2026. 00 h    -118\n07.09.2026. 01 h    -118")
	postaja := &models.Station{ID: uuid.New(), Name: "Batina", Code: "batina"}

	html := iscrtaj(t, "uvoz_ocitanja.html", PregledUvoza{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     postaja, GaugeName: "Batina", Satni: satni, Redci: redci, Novih: 2,
		Od: time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC),
		Do: time.Date(2026, 9, 7, 1, 0, 0, 0, time.UTC),
	})
	for _, want := range []string{"<!DOCTYPE html>", "Što bi se upisalo", "-118", "novo"} {
		if !strings.Contains(html, want) {
			t.Errorf("uvoz_ocitanja.html nema %q", want)
		}
	}

	html = iscrtaj(t, "arhiva_ispravci.html", PregledIspravaka{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     postaja, GaugeName: "Batina",
		Velicina: "vodostaj", Korak: "dnevni", Godina: 2013, Jedinica: "cm",
		Datoteka: "batina_vodostaj_dnevni_2013.csv", Promjena: 1,
		Redci: []RedakIspravka{{Kad: time.Date(2013, 6, 14, 6, 0, 0, 0, time.UTC),
			Staro: 771, Novo: 775, Izvor: "his2000", Razlog: "ovjereni maksimum", Redak: 166}},
	})
	for _, want := range []string{"<!DOCTYPE html>", "Što bi se promijenilo", "775", "ovjereni maksimum"} {
		if !strings.Contains(html, want) {
			t.Errorf("arhiva_ispravci.html nema %q", want)
		}
	}
}

// Vrijeme vrijednosti u nizu ispisuje se kako je zapisano, bez pomicanja u
// zagrebačko i bez dvostrukog sata. Prije se dobivalo „07.09.2026 02:00 00:00“:
// formatDate već ispisuje sat, localTime ga pomakne, pa je dopisani sirovi sat
// stajao uz njega.
func TestVrijemeNizaBezPomakaIDvostrukogSata(t *testing.T) {
	redci, satni, _ := citajZalijepljeno("07.09.2026. 00 h    -118\n07.09.2026. 23 h    -122")
	html := iscrtaj(t, "uvoz_ocitanja.html", PregledUvoza{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     &models.Station{ID: uuid.New(), Name: "Batina", Code: "batina"},
		GaugeName:   "Batina", Satni: satni, Redci: redci, Novih: 2,
		Od: redci[0].Kad, Do: redci[1].Kad,
	})
	// vrijeme s letve je lokalno: 00 h ostaje 00:00 i pri ispisu, jer se
	// pretvorbom u UTC i natrag vraća na isti sat
	if !strings.Contains(html, "07.09.2026 00:00") {
		t.Error("prvi sat se ne ispisuje kao 07.09.2026 00:00")
	}
	if !strings.Contains(html, "07.09.2026 23:00") {
		t.Error("zadnji sat se ne ispisuje kao 07.09.2026 23:00")
	}
	if strings.Contains(html, "02:00 00:00") || strings.Contains(html, "01:00 23:00") {
		t.Error("vrijeme se ispisuje dvaput, pomaknuto pa sirovo")
	}
	if strings.Contains(html, "08.09.2026") {
		t.Error("razdoblje se pomaknulo u sljedeći dan")
	}
	// 00 h po lokalnom je 22:00 UTC prethodnog dana — tako mora i biti spremljeno
	if got := redci[0].Kad.UTC(); got.Hour() != 22 || got.Day() != 6 {
		t.Errorf("00 h lokalno spremljeno kao %v, očekivano 6.9. 22:00 UTC", got)
	}
}

// Graf i popis moraju govoriti o istom razdoblju: graf koji pokazuje drugo od
// tablice ispod njega laže o tome što se gleda. Zadano je zadnjih 30 dana.
func TestPogledVeziGrafITablicu(t *testing.T) {
	cm := func(v int) *int { return &v }
	sad := time.Now()
	var sve []models.Reading
	for i := 0; i < 200; i++ { // 200 dana unatrag
		sve = append(sve, models.Reading{MeasuredAt: sad.AddDate(0, 0, -i), LevelCm: cm(100 + i)})
	}
	_ = sve

	html := iscrtaj(t, "reading_history.html", ReadingHistoryData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     &models.Station{ID: uuid.New(), Name: "Batina", Code: "batina"},
		GaugeName:   "Batina",
		Pogled:      "30", PogledOpis: "zadnjih 30 dana", Dana: 30,
		Years: []int{2026, 2025}, Count: 30,
		Readings: sve[:30],
		Chart:    crtajNiz(nizZaGraf(sad), "vodostaj", nil, nil),
	})
	if !strings.Contains(html, "zadnjih 30 dana") {
		t.Error("pogled nije ispisan uz graf")
	}
	if !strings.Contains(html, `name="pogled"`) {
		t.Error("nema izbornika pogleda")
	}
	// uvoz stoji ispod popisa očitanja
	iOcitanja := strings.Index(html, "Očitanja")
	iUvoz := strings.Index(html, "Unesi više očitanja odjednom")
	if iUvoz > 0 && iOcitanja > 0 && iUvoz < iOcitanja {
		t.Error("uvoz stoji iznad popisa očitanja")
	}
}

// U arhivi sažetak po veličinama stoji iznad grafa, a graf iznad tablice:
// prvo se vidi što uopće ima, pa kretanje, pa pojedine vrijednosti.
func TestRedoslijedUArhivi(t *testing.T) {
	kad := time.Date(2013, 6, 14, 6, 0, 0, 0, time.UTC)
	html := iscrtaj(t, "reading_history.html", ReadingHistoryData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     &models.Station{ID: uuid.New(), Name: "Batina", Code: "batina"},
		GaugeName:   "Batina", Pogled: "30", PogledOpis: "zadnjih 30 dana",
		ArhVelicine: []string{"vodostaj"}, ArhVelicina: "vodostaj",
		ArhKorak: "dnevni", ArhGodina: 2013, ArhGodine: []int{2013},
		ArhNiz:   []models.SpojenaVrijednost{{Kad: kad, Vrijednost: 771, Izvor: "his2000"}},
		ArhChart: crtajNiz(nizZaGraf(kad), "vodostaj", nil, nil),
		ArhSazetak: []models.SazetakVelicine{
			{Velicina: "vodostaj", Od: "1901-01-01", Do: "2026-09-06", Srednjak: 205, Max: 797, Min: -308},
		},
		ArhPager: pagerZa(&http.Request{URL: &url.URL{Path: "/x"}}, "ap", 365, 100),
	})
	iSazetak := strings.Index(html, "Karakteristične vrijednosti")
	iGraf := strings.Index(html, `aria-label="Graf:`)
	iTablica := strings.Index(html, "Novije prvo. Svaka vrijednost")
	if iSazetak < 0 || iGraf < 0 || iTablica < 0 {
		t.Fatalf("nedostaje odjeljak: sažetak %d, graf %d, tablica %d", iSazetak, iGraf, iTablica)
	}
	if !(iSazetak < iGraf && iGraf < iTablica) {
		t.Errorf("redoslijed nije sažetak → graf → tablica: %d, %d, %d", iSazetak, iGraf, iTablica)
	}
}

// Apsolutna kota stoji uz svako očitanje u popisu, ne samo uz zadnje: na teren
// se ide s popisom, a ne s jednom brojkom.
func TestPopisOcitanjaImaApsolutnuKotu(t *testing.T) {
	kotaP := func(v float64) *float64 { return &v }
	cm := func(v int) *int { return &v }
	st := &models.Station{
		ID: uuid.New(), Name: "Batina", Code: "batina",
		ZeroDatum: kotaP(80.450), ZeroDatumSystem: "TRST",
		ZeroDatumNew: kotaP(80.189), ZeroDatumNewSystem: "HVRS71",
	}
	sad := time.Now()
	html := iscrtaj(t, "reading_history.html", ReadingHistoryData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, GaugeName: "Batina",
		Pogled: "30", PogledOpis: "zadnjih 30 dana", Count: 2,
		Latest:   &models.Reading{MeasuredAt: sad, LevelCm: cm(-129)},
		Readings: []models.Reading{{MeasuredAt: sad, LevelCm: cm(-129)}},
	})
	// -129 cm: 80,189 - 1,29 = 78,899 i 80,450 - 1,29 = 79,160
	for _, want := range []string{"78,899 m HVRS71", "79,160 m TRST"} {
		if !strings.Contains(html, want) {
			t.Errorf("uz očitanje nema %q", want)
		}
	}

	// letva bez kote nule ne smije ispisivati apsolutnu visinu
	bez := iscrtaj(t, "reading_history.html", ReadingHistoryData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     &models.Station{ID: uuid.New(), Name: "Nešto", Code: "nesto"},
		GaugeName:   "Nešto", Pogled: "30", PogledOpis: "zadnjih 30 dana",
		Readings: []models.Reading{{MeasuredAt: sad, LevelCm: cm(-129)}},
	})
	if strings.Contains(bez, " m HVRS71") || strings.Contains(bez, " m TRST") {
		t.Error("letva bez kote nule ispisuje apsolutnu visinu")
	}
}

// Listanje tablice pomiče istaknuti dio grafa, ali graf ostaje cijelo
// razdoblje: os koja se prerazapinje pri svakom kliku teško se čita.
func TestIsticanjeSlijediListanje(t *testing.T) {
	poc := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	var niz []models.SpojenaVrijednost
	for i := 0; i < 100; i++ {
		niz = append(niz, models.SpojenaVrijednost{Kad: poc.AddDate(0, 0, i), Vrijednost: float64(100 + i)})
	}
	g := crtajNiz(niz, "vodostaj", nil, nil)
	if g == nil {
		t.Fatal("graf se nije izgradio")
	}
	if g.Istaknuto {
		t.Error("bez listanja se ništa ne ističe")
	}

	// prva stranica: početak razdoblja
	istakni(g, niz[0].Kad, niz[9].Kad)
	prvi := g.IstakniOd
	if !g.Istaknuto {
		t.Fatal("isticanje nije postavljeno")
	}

	// zadnja stranica: isti graf, istaknuto pomaknuto udesno
	g2 := crtajNiz(niz, "vodostaj", nil, nil)
	istakni(g2, niz[90].Kad, niz[99].Kad)
	if g2.IstakniOd <= prvi {
		t.Errorf("isticanje se nije pomaknulo: %v prema %v", g2.IstakniOd, prvi)
	}
	// os se nije promijenila — graf i dalje pokazuje isto razdoblje
	if !g2.From.Equal(g.From) || !g2.To.Equal(g.To) || g2.Min != g.Min || g2.Max != g.Max {
		t.Error("graf se prerazapeo pri listanju")
	}

	// razdoblje izvan grafa se ne crta izvan okvira
	g3 := crtajNiz(niz, "vodostaj", nil, nil)
	istakni(g3, poc.AddDate(-1, 0, 0), poc.AddDate(1, 0, 0))
	if g3.IstakniOd < 90 || g3.IstakniOd+g3.IstakniSir > float64(g3.Width) {
		t.Errorf("isticanje izlazi iz okvira: od %v širina %v", g3.IstakniOd, g3.IstakniSir)
	}
}

// Razdoblje bez očitanja ne smije se povući ravnom crtom: to bi tvrdilo da
// podatak postoji ondje gdje ga nema. Prag se uzima iz samog niza, pa isto
// pravilo radi i na satnom i na godišnjem.
func TestGrafPrekidaCrtuNaPraznini(t *testing.T) {
	poc := time.Date(2013, 1, 1, 0, 0, 0, 0, time.UTC)
	var niz []models.SpojenaVrijednost
	for d := 0; d < 120; d++ {
		if d >= 40 && d < 70 { // mjesec bez ijednog očitanja
			continue
		}
		niz = append(niz, models.SpojenaVrijednost{Kad: poc.AddDate(0, 0, d), Vrijednost: float64(100 + d)})
	}
	g := crtajNiz(niz, "vodostaj", nil, nil)
	if g == nil {
		t.Fatal("graf se nije izgradio")
	}
	if g.Praznina != 1 {
		t.Errorf("prekida %d, očekivano 1", g.Praznina)
	}
	if strings.Count(g.Path, "M") != 2 {
		t.Errorf("crta ima %d dionica, očekivano 2", strings.Count(g.Path, "M"))
	}
	if strings.Count(g.Area, "Z") != 2 {
		t.Errorf("površina ima %d dionica, očekivano 2", strings.Count(g.Area, "Z"))
	}

	// neprekinut niz ostaje jedna crta
	var pun []models.SpojenaVrijednost
	for d := 0; d < 120; d++ {
		pun = append(pun, models.SpojenaVrijednost{Kad: poc.AddDate(0, 0, d), Vrijednost: float64(100 + d)})
	}
	c := crtajNiz(pun, "vodostaj", nil, nil)
	if c.Praznina != 0 || strings.Count(c.Path, "M") != 1 {
		t.Errorf("neprekinut niz razlomljen: prekida %d, dionica %d", c.Praznina, strings.Count(c.Path, "M"))
	}

	// satni niz se prekida nakon nekoliko sati, ne nakon tjedan dana
	var satni []models.SpojenaVrijednost
	for h := 0; h < 100; h++ {
		if h >= 40 && h < 50 {
			continue
		}
		satni = append(satni, models.SpojenaVrijednost{Kad: poc.Add(time.Duration(h) * time.Hour), Vrijednost: 200})
	}
	sh := crtajNiz(satni, "vodostaj", nil, nil)
	if sh.Praznina != 1 {
		t.Errorf("satni niz: prekida %d, očekivano 1", sh.Praznina)
	}
}
