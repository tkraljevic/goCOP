package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// sadPopisa je trenutak prema kojem se gleda popis: 10. 3. u 12 h po
// zagrebačkom vremenu.
var sadPopisa = time.Date(2026, 3, 10, 11, 0, 0, 0, time.UTC)

// mjerenja je niz mjerenja jedne veličine: uobičajeno satna, zadnje prije
// zadanog vremena.
func mjerenja(prije time.Duration) repository.MjerenjaVelicine {
	zadnje := sadPopisa.Add(-prije)
	return repository.MjerenjaVelicine{Broj: 100, Prvo: zadnje.Add(-99 * time.Hour), Zadnje: zadnje}
}

func TestRetciNedostaju(t *testing.T) {
	bez := repository.MjerenjaVelicine{}
	for _, s := range []struct {
		ime   string
		letve []repository.MjerenjaLetve
		zeli  []RedakNedostaju
	}{
		{ime: "sve stiže", letve: []repository.MjerenjaLetve{{Naziv: "Primjerovo", Vodostaj: mjerenja(time.Hour), Protok: mjerenja(time.Hour)}}},
		{ime: "točno na pragu još nije na popisu", letve: []repository.MjerenjaLetve{{Naziv: "Primjerovo", Vodostaj: mjerenja(NedostajeNakon)}}},
		{
			ime:   "minutu preko praga",
			letve: []repository.MjerenjaLetve{{Naziv: "Primjerovo", Vodostaj: mjerenja(NedostajeNakon + time.Minute)}},
			zeli:  []RedakNedostaju{{Velicina: "Vodostaj", Tekst: "Primjerovo (od 10. 3. u 5 h)"}},
		},
		{
			ime:   "letva bez protoka navodi se samo za vodostaj",
			letve: []repository.MjerenjaLetve{{Naziv: "Primjerovo", Vodostaj: mjerenja(20 * time.Hour), Protok: bez}},
			zeli:  []RedakNedostaju{{Velicina: "Vodostaj", Tekst: "Primjerovo (od 9. 3. u 16 h)"}},
		},
		{ime: "letva bez ijedne veličine u razdoblju", letve: []repository.MjerenjaLetve{{Naziv: "Primjerovo"}}},
		{
			ime: "vodostaj stiže, protok ne",
			letve: []repository.MjerenjaLetve{
				{Naziv: "Ana", Vodostaj: mjerenja(time.Hour), Protok: mjerenja(20 * time.Hour)},
				{Naziv: "Bela", Vodostaj: mjerenja(time.Hour), Protok: mjerenja(20*time.Hour - 10*time.Minute)},
				{Naziv: "Cera", Vodostaj: mjerenja(8 * time.Hour)},
			},
			zeli: []RedakNedostaju{
				{Velicina: "Vodostaj", Tekst: "Cera (od 10. 3. u 4 h)"},
				{Velicina: "Protok", Tekst: "Ana, Bela (od 9. 3. u 16 h)"},
			},
		},
		{
			ime: "najsvježiji prekid ide prvi",
			letve: []repository.MjerenjaLetve{
				{Naziv: "Stara", Vodostaj: mjerenja(50 * time.Hour)},
				{Naziv: "Nova", Vodostaj: mjerenja(7 * time.Hour)},
			},
			zeli: []RedakNedostaju{{Velicina: "Vodostaj", Tekst: "Nova (od 10. 3. u 5 h); Stara (od 8. 3. u 10 h)"}},
		},
		{
			// dvaput na dan: šesti do dvanaesti sat je redovno stanje, a
			// tek kad izostane još jedno javljanje, letva je na popisu
			ime: "izvor koji se javlja rijetko",
			letve: []repository.MjerenjaLetve{
				{Naziv: "Rijetka", Vodostaj: repository.MjerenjaVelicine{Broj: 3, Prvo: sadPopisa.Add(-34 * time.Hour), Zadnje: sadPopisa.Add(-10 * time.Hour)}},
				{Naziv: "Stala", Vodostaj: repository.MjerenjaVelicine{Broj: 3, Prvo: sadPopisa.Add(-50 * time.Hour), Zadnje: sadPopisa.Add(-26 * time.Hour)}},
			},
			zeli: []RedakNedostaju{{Velicina: "Vodostaj", Tekst: "Stala (od 9. 3. u 10 h)"}},
		},
		{
			// jedno mjerenje nema razmaka, a niz koji je stao prije puno sati
			// ne smije se sakriti iza vlastitog kratkog razmaka
			ime: "jedno mjerenje i niz koji je davno stao",
			letve: []repository.MjerenjaLetve{
				{Naziv: "Jedna", Vodostaj: repository.MjerenjaVelicine{Broj: 1, Prvo: sadPopisa.Add(-7 * time.Hour), Zadnje: sadPopisa.Add(-7 * time.Hour)}},
				{Naziv: "Stala", Vodostaj: repository.MjerenjaVelicine{Broj: 13, Prvo: sadPopisa.Add(-200 * time.Hour), Zadnje: sadPopisa.Add(-188 * time.Hour)}},
			},
			zeli: []RedakNedostaju{{Velicina: "Vodostaj", Tekst: "Jedna (od 10. 3. u 5 h); Stala (od 2. 3. u 16 h)"}},
		},
		{
			// razmak je 5 dana, ali se ne čeka 10: letva bi prije ispala iz razdoblja
			ime: "vrlo rijedak izvor ne čeka se dulje od pola razdoblja",
			letve: []repository.MjerenjaLetve{
				{Naziv: "Dvije", Vodostaj: repository.MjerenjaVelicine{Broj: 2, Prvo: sadPopisa.Add(-13 * 24 * time.Hour), Zadnje: sadPopisa.Add(-8 * 24 * time.Hour)}},
				{Naziv: "Tjedna", Vodostaj: repository.MjerenjaVelicine{Broj: 2, Prvo: sadPopisa.Add(-11 * 24 * time.Hour), Zadnje: sadPopisa.Add(-6 * 24 * time.Hour)}},
			},
			zeli: []RedakNedostaju{{Velicina: "Vodostaj", Tekst: "Dvije (od 2. 3. u 12 h)"}},
		},
		{
			ime: "država iz imena se miče, a druga zagrada ostaje",
			letve: []repository.MjerenjaLetve{
				{Naziv: "Primjerovo (Mađarska)", Vodostaj: mjerenja(7 * time.Hour)},
				{Naziv: "Ustava Tisovo (nizvodno)", Vodostaj: mjerenja(7 * time.Hour)},
			},
			zeli: []RedakNedostaju{{Velicina: "Vodostaj", Tekst: "Primjerovo, Ustava Tisovo (nizvodno) (od 10. 3. u 5 h)"}},
		},
	} {
		t.Run(s.ime, func(t *testing.T) {
			got := retciNedostaju(s.letve, sadPopisa)
			if obicniRazmaci(fmt.Sprint(got)) != fmt.Sprint(s.zeli) {
				t.Errorf("dobio %+v\nželim %+v", got, s.zeli)
			}
		})
	}
}

// Kad letvi ima puno, vidi se prvih nekoliko, a ostale su pod „i još N”.
func TestRetciNedostajuPuno(t *testing.T) {
	var letve []repository.MjerenjaLetve
	for i := range nedostajeOdmah + 3 {
		letve = append(letve, repository.MjerenjaLetve{Naziv: fmt.Sprintf("L%02d", i), Protok: mjerenja(10 * time.Hour)})
	}
	got := retciNedostaju(letve, sadPopisa)
	if len(got) != 1 || got[0].Velicina != "Protok" || got[0].Jos != 3 ||
		obicniRazmaci(got[0].Tekst) != "L00, L01, L02, L03, L04, L05, L06, L07 (od 10. 3. u 2 h)" || obicniRazmaci(got[0].Ostatak) != "L08, L09, L10 (od 10. 3. u 2 h)" {
		t.Errorf("%+v", got)
	}
}

// obicniRazmaci vraća tekst s običnim razmacima umjesto neprelomivih, da se
// očekivanja u testovima mogu čitati.
func obicniRazmaci(s string) string {
	return strings.ReplaceAll(s, "\u00a0", " ")
}

// novaOkolinaNapomene je poslužitelj s pravom bazom i servisom akata.
func novaOkolinaNapomene(t *testing.T) *posluziteljOcitanja {
	t.Helper()
	p := noviPosluziteljOcitanja(t)
	rec := ledger.New(p.baza, "test")
	stationRepo := repository.NewStationRepository(p.baza, rec)
	p.s.akti = service.NewAktService(repository.NewAktiRepository(p.baza, rec), stationRepo, repository.NewSectionRepository(p.baza, rec),
		repository.NewTerritoryRepository(p.baza, rec), p.ocitanja, p.users, service.NewEpisodeService(repository.NewEpisodeRepository(p.baza, rec), p.ocitanja, stationRepo), "test")
	return p
}

// Traka kroz poslužitelj: popis iz stvarnih očitanja, napomena iznad njega,
// tekst napomene se ne izvršava, a bez ičega nema ni trake.
func TestTrakaPodatakaNaStranici(t *testing.T) {
	p := novaOkolinaNapomene(t)
	ctx := context.Background()
	rec := ledger.New(p.baza, "test")
	stationRepo := repository.NewStationRepository(p.baza, rec)
	stranica := func() string {
		t.Helper()
		w := p.posalji("dežurni", http.MethodGet, "/prognoze", nil, "")
		if w.Code != http.StatusOK {
			t.Fatalf("stranica: %d", w.Code)
		}
		return w.Body.String()
	}
	if html := stranica(); strings.Contains(html, "prog-nedostaju") {
		t.Error("traka bez popisa i napomene")
	}

	st := &models.Station{ID: uuid.New(), Code: "tisovo", Name: "Tisovo", JavniURL: "https://izvor.example/1", JavniUvoz: true}
	if err := stationRepo.CreateStation(ctx, st); err != nil {
		t.Fatal(err)
	}
	cm := 100
	kad := time.Now().Add(-9 * time.Hour).UTC().Truncate(time.Hour)
	if err := p.ocitanja.Create(ctx, &models.Reading{ID: uuid.New(), StationID: st.ID.String(), MeasuredAt: kad, LevelCm: &cm, Source: models.ReadingSourceImport}); err != nil {
		t.Fatal(err)
	}
	// prethodni prikaz je zapamćen: popis se ne računa pri svakom otvaranju
	if html := stranica(); strings.Contains(html, "Tisovo (od") {
		t.Error("popis nije zapamćen")
	}
	if err := p.s.akti.SpremiNapomenuPodataka(ctx, &models.UserPermissions{IsGlobalAdmin: true}, "Izvor <b>mijenja</b> adrese."); err != nil {
		t.Fatal(err)
	}
	html := stranica()
	if !strings.Contains(html, "Izvor &lt;b&gt;mijenja&lt;/b&gt; adrese.") || strings.Contains(html, "<b>mijenja</b>") {
		t.Error("napomena se ne vidi ili nije obični tekst")
	}
	if strings.Contains(html, "nema ovih mjerenja") {
		t.Error("napomena bez popisa, a piše da mjerenja nema")
	}
}

// „i još N” nastavlja redak, pa točka dolazi tek na kraju sklopljenog dijela.
func TestTrakaTockaUzSklopljeno(t *testing.T) {
	html := iscrtaj(t, "prognoze.html", PrognozePageData{CurrentUser: &models.User{FullName: "Ana Anić"}, ActiveNav: "prognoze",
		Nedostaju: []RedakNedostaju{{Velicina: "Protok", Tekst: "Prva", Jos: 2, Ostatak: "Druga, Treća"}, {Velicina: "Vodostaj", Tekst: "Četvrta"}}})
	for _, zeli := range []string{"<p>Protok: Prva</p>", "<summary>i još 2</summary><p>Druga, Treća.</p>", "<p>Vodostaj: Četvrta.</p>"} {
		if !strings.Contains(html, zeli) {
			t.Errorf("stranica nema %q", zeli)
		}
	}
}

// Svjež popis na stranici: letva s uključenim preuzimanjem koja kasni je na
// njemu, s vremenom po zagrebačkom vremenu, a isključena letva nije.
func TestTrakaPopisKrozPosluzitelj(t *testing.T) {
	p := novaOkolinaNapomene(t)
	ctx := context.Background()
	rec := ledger.New(p.baza, "test")
	stationRepo := repository.NewStationRepository(p.baza, rec)
	kad := time.Now().Add(-9 * time.Hour).UTC().Truncate(time.Hour)
	cm, q := 100, 50.0
	for _, st := range []*models.Station{
		{ID: uuid.New(), Code: "tisovo", Name: "Tisovo", JavniURL: "https://izvor.example/1", JavniUvoz: true},
		{ID: uuid.New(), Code: "jasenovo", Name: "Jasenovo", JavniURL: "https://izvor.example/2"},
	} {
		if err := stationRepo.CreateStation(ctx, st); err != nil {
			t.Fatal(err)
		}
		if err := p.ocitanja.Create(ctx, &models.Reading{ID: uuid.New(), StationID: st.ID.String(), MeasuredAt: kad, LevelCm: &cm, FlowM3s: &q, Source: models.ReadingSourceImport}); err != nil {
			t.Fatal(err)
		}
	}
	sirovo := p.posalji("dežurni", http.MethodGet, "/prognoze", nil, "").Body.String()
	if !strings.Contains(sirovo, "Tisovo (od\u00a0") {
		t.Error("zagrada s vremenom se prelama")
	}
	html := obicniRazmaci(sirovo)
	for _, zeli := range []string{"Na ovom računalu nema ovih mjerenja:", "Vodostaj: Tisovo (od " + satUpozorenja(kad) + ").", "Protok: Tisovo (od " + satUpozorenja(kad) + ")."} {
		if !strings.Contains(html, zeli) {
			t.Errorf("stranica nema %q", zeli)
		}
	}
	if strings.Contains(html, "Jasenovo") {
		t.Error("na popisu je letva s isključenim preuzimanjem")
	}
}

// Bez očitanja ili kad se ne daju pročitati popisa nema, a neuspjeh se ne pamti.
func TestTrakaGreskaOcitanja(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "traka.db"))
	if err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test")
	h := &PrognozeHandler{}
	if got := h.nedostajuPodaci(context.Background()); got != nil {
		t.Errorf("bez očitanja: %+v", got)
	}
	h.SetReadings(service.NewReadingService(repository.NewReadingRepository(baza, rec), nil, nil, nil, nil))
	baza.Close()
	if got := h.nedostajuPodaci(context.Background()); got != nil {
		t.Errorf("zatvorena baza: %+v", got)
	}
	if !h.nedostaju.kad.IsZero() {
		t.Error("neuspjelo čitanje je zapamćeno")
	}
}

// Dok servis akata nije spojen, napomene nema, a spremanje to i kaže.
func TestNapomenaBezServisaAkata(t *testing.T) {
	p := noviPosluziteljOcitanja(t)
	if html := p.posalji("dežurni", http.MethodGet, "/prognoze", nil, "").Body.String(); strings.Contains(html, "prog-nedostaju") {
		t.Error("traka bez napomene i popisa")
	}
	w := p.obrazac("uprava", "/prognoze/napomena-podataka", url.Values{"napomena": {"Tekst."}})
	if !strings.Contains(mustUnescape(w.Header().Get("Location")), "error=Napomena nije spremljena: na ovom računalu to sada nije moguće.") {
		t.Errorf("spremanje: %q", w.Header().Get("Location"))
	}
}

// Napomenu kroz poslužitelj upisuje i briše globalni administrator; ostali
// ne dolaze do rukovatelja i ne vide obrazac.
func TestNapomenaPravaKrozPosluzitelj(t *testing.T) {
	p := novaOkolinaNapomene(t)
	ctx := context.Background()
	put := "/prognoze/napomena-podataka"

	if w := p.obrazac("", put, url.Values{"napomena": {"Bez prijave."}}); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Errorf("bez prijave: %d %q", w.Code, w.Header().Get("Location"))
	}
	if w := p.obrazac("dežurni", put, url.Values{"napomena": {"Dežurni."}}); w.Code != http.StatusForbidden {
		t.Errorf("dežurni: %d", w.Code)
	}
	if v := p.s.akti.NapomenaPodataka(ctx); v != "" {
		t.Fatalf("spremljeno bez prava: %q", v)
	}

	poruka := func(w *httptest.ResponseRecorder, kljuc string) string {
		t.Helper()
		u, err := url.Parse(w.Header().Get("Location"))
		if err != nil || u.Path != "/prognoze/postavke" || u.Fragment != "napomena" {
			t.Fatalf("preusmjerenje: %q", w.Header().Get("Location"))
		}
		return u.Query().Get(kljuc)
	}
	if m := poruka(p.obrazac("uprava", put, url.Values{"napomena": {"Izvor mijenja adrese."}}), "success"); m != "Napomena je spremljena i vidi se na stranici Prognoze." {
		t.Errorf("spremanje: %q", m)
	}
	if v := p.s.akti.NapomenaPodataka(ctx); v != "Izvor mijenja adrese." {
		t.Errorf("spremljeno: %q", v)
	}
	if m := poruka(p.obrazac("uprava", put, url.Values{"napomena": {strings.Repeat("x", 501)}}), "error"); !strings.Contains(m, "501") {
		t.Errorf("preduga: %q", m)
	}
	if v := p.s.akti.NapomenaPodataka(ctx); v != "Izvor mijenja adrese." {
		t.Errorf("preduga je promijenila napomenu: %q", v)
	}
	if m := poruka(p.obrazac("uprava", put, url.Values{"napomena": {"Ostaje u polju."}, "obrisi": {"1"}}), "success"); m != "Napomena je obrisana." {
		t.Errorf("brisanje: %q", m)
	}
	if v := p.s.akti.NapomenaPodataka(ctx); v != "" {
		t.Errorf("obrisano: %q", v)
	}
}

// Obrazac na Postavkama prognoze vidi samo globalni administrator, a gumb za
// brisanje samo kad napomena postoji. Bez usluge ni obrasca ni spremanja.
// Računalo bez izdanja i bez kruga preuzimanja samo administratoru otvara
// stranicu, jer mu je to jedino mjesto za napomenu.
func TestNapomenaNaPostavkama(t *testing.T) {
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	izdano := time.Date(2026, 9, 23, 5, 0, 0, 0, time.UTC).Unix() / 3600
	c, err := OtvoriPrognoze(bazaPrognoza(t, izdano))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	h := NewPrognozeHandler(nil, func() *CitacPrognoza { return c }, nil)
	h.SetPostavke(predlozakStranice(t, "prognoze_postavke.html"))
	stranica := func(perms *models.UserPermissions) string {
		t.Helper()
		w := httptest.NewRecorder()
		h.ShowPostavke(w, zahtjevOsobe(http.MethodGet, "/prognoze/postavke", "", perms))
		if w.Code != http.StatusOK {
			t.Fatalf("postavke: %d", w.Code)
		}
		return w.Body.String()
	}
	if html := stranica(admin); strings.Contains(html, `id="napomena"`) {
		t.Error("obrazac bez usluge")
	}
	w := httptest.NewRecorder()
	h.SpremiNapomenu(w, zahtjevOsobe(http.MethodPost, "/prognoze/napomena-podataka", "napomena=x", admin))
	if !strings.Contains(mustUnescape(w.Header().Get("Location")), "error=Napomenu upisuje globalni administrator.") {
		t.Errorf("spremanje bez usluge: %q", w.Header().Get("Location"))
	}

	tekst := ""
	h.SetNapomena(func(context.Context) string { return tekst }, func(_ context.Context, _ *models.UserPermissions, v string) error {
		tekst = v
		return nil
	})
	if html := stranica(&models.UserPermissions{}); strings.Contains(html, `id="napomena"`) {
		t.Error("obrazac vidi tko nije administrator")
	}
	w = httptest.NewRecorder()
	h.SpremiNapomenu(w, zahtjevOsobe(http.MethodPost, "/prognoze/napomena-podataka", "napomena=x", &models.UserPermissions{}))
	if tekst != "" || !strings.Contains(mustUnescape(w.Header().Get("Location")), "error=") {
		t.Errorf("spremanje bez prava: %q %q", tekst, w.Header().Get("Location"))
	}
	html := stranica(admin)
	if !strings.Contains(html, `action="/prognoze/napomena-podataka"`) || strings.Contains(html, "Obriši napomenu") {
		t.Error("administrator bez napomene: nema obrasca ili ima gumb za brisanje")
	}
	tekst = "Izvor mijenja adrese."
	html = stranica(admin)
	if !strings.Contains(html, ">Izvor mijenja adrese.</textarea>") || !strings.Contains(html, "Obriši napomenu") {
		t.Error("administrator s napomenom: polje ili gumb")
	}

	prazan := NewPrognozeHandler(nil, nil, nil)
	prazan.SetPostavke(predlozakStranice(t, "prognoze_postavke.html"))
	prazan.SetNapomena(func(context.Context) string { return "" }, nil)
	w = httptest.NewRecorder()
	prazan.ShowPostavke(w, zahtjevOsobe(http.MethodGet, "/prognoze/postavke", "", admin))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `id="napomena"`) {
		t.Errorf("administrator bez izdanja: %d", w.Code)
	}
	w = httptest.NewRecorder()
	prazan.ShowPostavke(w, zahtjevOsobe(http.MethodGet, "/prognoze/postavke", "", &models.UserPermissions{}))
	if w.Code != http.StatusSeeOther {
		t.Errorf("korisnik bez izdanja: %d", w.Code)
	}
}
