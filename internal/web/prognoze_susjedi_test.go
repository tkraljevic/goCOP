package web

import (
	"context"
	"math"
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
	"gocop/internal/prognoza"
	"gocop/internal/repository"
	"gocop/internal/service"
	"gocop/internal/xlsxw"
)

// izmisljeniSusjedi zamijene letve iz susjeda izmišljenom: Srednji Brod bez
// dnevnog modela, između Gornjeg Grada i Donjeg Grada koji ga imaju.
func izmisljeniSusjedi(t *testing.T) {
	t.Helper()
	stare := LetveIzSusjeda
	t.Cleanup(func() { LetveIzSusjeda = stare })
	LetveIzSusjeda = []LetvaIzSusjeda{{Letva: "srednja", Uzvodna: "gornja", Nizvodna: "donja"}}
}

// probaSusjeda je izmišljena tablica: gornja (rkm 200) i donja (rkm 100)
// imaju dnevni model od prvog dana; srednja (rkm 175) ga nema, pa joj je
// udio gornje tri četvrtine. Srednja ima satni lanac prva četiri dana
// (130–133), sada 125, a srednjak zadnja 24 sata 120.
type probaSusjeda struct {
	t         tablicaSusjeda
	srednjaci map[string]float64
}

func novaProbaSusjeda(t *testing.T) probaSusjeda {
	t.Helper()
	izdano := time.Date(2026, 10, 8, 19, 0, 0, 0, time.UTC)
	_, ciljevi := daniPregleda(izdano)
	sat := izdano.Unix() / 3600
	dnevne := map[string][]prognoza.DnevnaIzdana{}
	for k := 0; k <= prognoza.DnevniDosezi; k++ {
		dnevne["gornja"] = append(dnevne["gornja"], prognoza.DnevnaIzdana{Letva: "gornja", Dan: k, Ciljni: sat + int64(24*k), Vrijednost: 50})
		dnevne["donja"] = append(dnevne["donja"], prognoza.DnevnaIzdana{Letva: "donja", Dan: k, Ciljni: sat + int64(24*k), Vrijednost: 300})
	}
	v := func(x float64) *float64 { return &x }
	letve := []LetvaPrognoze{{Kod: "gornja"}, {Kod: "srednja"}, {Kod: "donja"}}
	for k := range ciljevi {
		// promjena gornje od dana 0: 10, 20, …; donje 20, 40, …
		letve[0].Dani = append(letve[0].Dani, CelijaDana{CmV: v(60 + 10*float64(k)), Dnevna: true})
		letve[2].Dani = append(letve[2].Dani, CelijaDana{CmV: v(320 + 20*float64(k)), Dnevna: true})
		c := CelijaDana{}
		if k < 4 {
			c = CelijaDana{CmV: v(130 + float64(k)), Cm: brojHRf(130+float64(k), 0), Raspon: "±5", Q: "40", QV: v(40), Slabija: true}
		}
		letve[1].Dani = append(letve[1].Dani, c)
	}
	cm := func(x int) *int { return &x }
	postaje := map[string]models.Station{
		"gornja":  {Code: "gornja", Stationing: "rkm 200,0"},
		"srednja": {Code: "srednja", Stationing: "rkm 175,0", Prep: models.Threshold{Cm: cm(140)}},
		"donja":   {Code: "donja", Stationing: "rkm 100,0"},
	}
	pregled := []PregledLetve{
		{Letva: "gornja", Sada: map[string]float64{"vodostaj": 60}},
		{Letva: "srednja", Sada: map[string]float64{"vodostaj": 125}},
		{Letva: "donja", Sada: map[string]float64{"vodostaj": 310}},
	}
	return probaSusjeda{
		t:         tablicaSusjeda{letve: letve, pregled: pregled, dnevne: dnevne, postaje: postaje, ciljevi: ciljevi},
		srednjaci: map[string]float64{"srednja": 120},
	}
}

func (p probaSusjeda) srednja() LetvaPrognoze { return p.t.letve[1] }

// ocekivano je dan k (od 0) srednje kad oba susjeda daju dnevni model:
// srednjak 120 + ¾ promjene gornje + ¼ promjene donje.
func ocekivano(k int) float64 {
	return 120 + 0.75*(10+10*float64(k)) + 0.25*(20+20*float64(k))
}

// Od drugog dana srednja slijedi susjede: promjena svakog od njegova dana 0,
// utegnuta po kilometru, dodana srednjaku letve. Dan nosi oznaku iz susjeda,
// bez raspona i protoka, i fazu obrane po svojim pragovima. Prvi dan ostaje
// satni lanac.
func TestDaniIzSusjedaPoKilometru(t *testing.T) {
	izmisljeniSusjedi(t)
	p := novaProbaSusjeda(t)
	dopuniIzSusjeda(p.t, p.srednjaci, false)
	dani := p.srednja().Dani
	if d := dani[0]; d.IzSusjeda || d.Cm != "130" || d.Raspon != "±5" || d.Q != "40" {
		t.Errorf("1. dan %+v: mora ostati satni lanac", d)
	}
	for k := 1; k < len(dani); k++ {
		d := dani[k]
		if d.CmV == nil || math.Abs(*d.CmV-ocekivano(k)) > 1e-9 {
			t.Errorf("%d. dan: %v, želim %g", k+1, d.CmV, ocekivano(k))
			continue
		}
		if !d.IzSusjeda || d.Dnevna || d.IzSatne || d.BezDnevne || d.Slabija || d.Raspon != "" || d.Q != "" || d.QV != nil ||
			d.Cm != brojHRf(ocekivano(k), 0) || d.Razina != "prep" || d.Moguce != "" {
			t.Errorf("%d. dan %+v: oznaka iz susjeda, bez raspona i protoka, pripremna obrana", k+1, d)
		}
	}
	if dani[1].Cm != "145" {
		t.Errorf("2. dan %q, želim 145", dani[1].Cm)
	}
	if !p.srednja().ImaTermina {
		t.Error("letva s danima iz susjeda mora imati termine")
	}
}

// Dan koji susjed pokazuje iz satnog lanca (prije svog dana prijelaza)
// mjeri promjenu od sadašnje vrijednosti susjeda i dodaje je sadašnjoj
// vrijednosti letve, a ne srednjaku.
func TestDaniIzSusjedaSatniDanSusjeda(t *testing.T) {
	izmisljeniSusjedi(t)
	p := novaProbaSusjeda(t)
	v := 79.0 // gornja sada 60: promjena 19
	p.t.letve[0].Dani[1] = CelijaDana{CmV: &v}
	dopuniIzSusjeda(p.t, p.srednjaci, false)
	// ¾ · (125 + 19) + ¼ · (120 + 40)
	if d := p.srednja().Dani[1]; d.CmV == nil || math.Abs(*d.CmV-148) > 1e-9 || !d.IzSusjeda {
		t.Errorf("2. dan %+v, želim 148", d)
	}
	// bez sadašnje vrijednosti gornje taj dan ostaje kakav je bio
	p = novaProbaSusjeda(t)
	p.t.letve[0].Dani[1] = CelijaDana{CmV: &v}
	p.t.pregled[0].Sada = map[string]float64{}
	dopuniIzSusjeda(p.t, p.srednjaci, false)
	if d := p.srednja().Dani[1]; d.IzSusjeda || d.Cm != "131" {
		t.Errorf("2. dan %+v: susjed bez sadašnje vrijednosti", d)
	}
}

// Kad susjed u izdanju nema dnevne prognoze za termin, dan ostaje kakav je
// bio: satni lanac dokle seže, dalje prazan.
func TestDaniIzSusjedaBezDnevneSusjeda(t *testing.T) {
	izmisljeniSusjedi(t)
	p := novaProbaSusjeda(t)
	p.t.dnevne["donja"] = p.t.dnevne["donja"][:4] // dani 0–3: pokriva samo termine do trećeg dana
	dopuniIzSusjeda(p.t, p.srednjaci, false)
	dani := p.srednja().Dani
	for k := 1; k <= 2; k++ {
		if !dani[k].IzSusjeda {
			t.Errorf("%d. dan %+v: donja ga još pokriva", k+1, dani[k])
		}
	}
	if d := dani[3]; d.IzSusjeda || d.Cm != "133" {
		t.Errorf("4. dan %+v: mora ostati satni", d)
	}
	for k := 4; k < len(dani); k++ {
		if d := dani[k]; d.IzSusjeda || d.CmV != nil {
			t.Errorf("%d. dan %+v: mora ostati prazan", k+1, d)
		}
	}
	// susjed bez ijedne dnevne
	p = novaProbaSusjeda(t)
	delete(p.t.dnevne, "gornja")
	dopuniIzSusjeda(p.t, p.srednjaci, false)
	for k, d := range p.srednja().Dani {
		if d.IzSusjeda {
			t.Errorf("%d. dan %+v: gornja nema dnevne", k+1, d)
		}
	}
}

// Letva ostaje kakva jest kad joj se ne zna kilometar, kad ne leži između
// susjeda, kad nije povezana sa živom vodom, kad susjeda nema u izdanju,
// kad susjed nema dana 0 ili kad letvi nema srednjaka 24 sata.
func TestDaniIzSusjedaPreskace(t *testing.T) {
	izmisljeniSusjedi(t)
	for _, tc := range []struct {
		ime   string
		izmij func(p *probaSusjeda)
	}{
		{"bez kilometra", func(p *probaSusjeda) { p.t.postaje["srednja"] = models.Station{Code: "srednja"} }},
		{"izvan susjeda", func(p *probaSusjeda) { p.t.postaje["srednja"] = models.Station{Stationing: "rkm 250,0"} }},
		{"nizvodno od donje", func(p *probaSusjeda) { p.t.postaje["srednja"] = models.Station{Stationing: "rkm 90,0"} }},
		{"nepovezana", func(p *probaSusjeda) { p.t.pregled[1].Nepovezana = true }},
		{"bez donje", func(p *probaSusjeda) { p.t.letve[2].Kod = "neka-druga" }},
		{"bez srednje", func(p *probaSusjeda) { p.t.letve[1].Kod = "neka-druga" }},
		{"bez dana 0", func(p *probaSusjeda) { p.t.dnevne["gornja"] = p.t.dnevne["gornja"][1:] }},
		{"bez srednjaka", func(p *probaSusjeda) { p.srednjaci = nil }},
	} {
		t.Run(tc.ime, func(t *testing.T) {
			p := novaProbaSusjeda(t)
			tc.izmij(&p)
			dopuniIzSusjeda(p.t, p.srednjaci, false)
			for k, d := range p.t.letve[1].Dani {
				if d.IzSusjeda {
					t.Errorf("%d. dan %+v: letva se mora preskočiti", k+1, d)
				}
			}
		})
	}
}

// U satnom načinu prikaza susjedi daju samo dane do kojih satni lanac ne
// seže, kao što ondje i dnevni model daje samo njih.
func TestDaniIzSusjedaSamoPrazni(t *testing.T) {
	izmisljeniSusjedi(t)
	p := novaProbaSusjeda(t)
	dopuniIzSusjeda(p.t, p.srednjaci, true)
	for k, d := range p.srednja().Dani {
		if d.IzSusjeda != (k >= 4) {
			t.Errorf("%d. dan %+v: iz susjeda samo 5. i 6.", k+1, d)
		}
	}
}

// Uprava dane iz susjeda može isključiti u Postavkama prognoze; tada letva
// ostaje na satnom lancu. Bez očitanja srednjaka nema, pa dani iz dnevnog
// modela susjeda izostaju.
func TestDopuniSusjedePoOpciji(t *testing.T) {
	izmisljeniSusjedi(t)
	izdano := time.Date(2026, 10, 8, 19, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		ime          string
		bez          bool
		nacin        string
		srednjak     bool
		iz2, iz5     bool
		ocitanjaBaza bool
	}{
		{ime: "isključeno", bez: true, srednjak: true, ocitanjaBaza: true},
		{ime: "uključeno", srednjak: true, iz2: true, iz5: true, ocitanjaBaza: true},
		{ime: "satni način", nacin: models.PrognozaSatni, srednjak: true, iz5: true, ocitanjaBaza: true},
		{ime: "bez očitanja", srednjak: false},
	} {
		t.Run(tc.ime, func(t *testing.T) {
			p := novaProbaSusjeda(t)
			h := &PrognozeHandler{}
			h.SetOpcije(func(context.Context) models.Opcije { return models.Opcije{PrognozaBezSusjeda: tc.bez} }, nil)
			if tc.ocitanjaBaza {
				h.readings, p.t.postaje = ocitanjaSrednje(t, p.t.postaje, izdano)
			}
			h.dopuniSusjede(context.Background(), p.t.letve, p.t.pregled, p.t.dnevne, p.t.postaje, p.t.ciljevi, izdano, tc.nacin)
			dani := p.srednja().Dani
			if dani[1].IzSusjeda != tc.iz2 || dani[4].IzSusjeda != tc.iz5 {
				t.Errorf("2. dan %v, 5. dan %v; želim %v i %v", dani[1].IzSusjeda, dani[4].IzSusjeda, tc.iz2, tc.iz5)
			}
			if tc.iz2 && math.Abs(*dani[1].CmV-ocekivano(1)) > 1e-9 {
				t.Errorf("2. dan %v: srednjak iz očitanja mora biti 120", *dani[1].CmV)
			}
		})
	}
}

// ocitanjaSrednje upiše srednjoj letvi očitanja zadnja 24 sata do izdanja,
// naizmjence 110 i 130 cm (srednjak 120), i vrati čitanje očitanja.
func ocitanjaSrednje(t *testing.T, postaje map[string]models.Station, izdano time.Time) (*service.ReadingService, map[string]models.Station) {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "susjedi.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = baza.Close() // privremena baza testa, briše je t.TempDir
	})
	rec := ledger.New(baza, "test")
	stanice := repository.NewStationRepository(baza, rec)
	ocitanja := repository.NewReadingRepository(baza, rec)
	ctx := context.Background()
	st := postaje["srednja"]
	st.ID, st.Name, st.Watercourse = uuid.New(), "Srednji Brod", "Primjerica"
	if err := stanice.CreateStation(ctx, &st); err != nil {
		t.Fatal(err)
	}
	for h := 0; h < 24; h++ {
		cm := 110 + 20*(h%2)
		if err := ocitanja.Create(ctx, &models.Reading{StationID: st.ID.String(), MeasuredAt: izdano.Add(-time.Duration(h) * time.Hour),
			LevelCm: &cm, Source: "RUČNO"}); err != nil {
			t.Fatal(err)
		}
	}
	nove := map[string]models.Station{}
	for k, v := range postaje {
		nove[k] = v
	}
	nove["srednja"] = st
	return service.NewReadingService(ocitanja, stanice, nil, nil, nil), nove
}

// Srednjak se čita iz očitanja; letva izvan registra ga nema, a greška
// čitanja ostavlja letvu bez srednjaka.
func TestSrednjaciDana(t *testing.T) {
	izmisljeniSusjedi(t)
	izdano := time.Date(2026, 10, 8, 19, 0, 0, 0, time.UTC)
	p := novaProbaSusjeda(t)
	r, postaje := ocitanjaSrednje(t, p.t.postaje, izdano)
	h := &PrognozeHandler{readings: r}
	if s := h.srednjaciDana(context.Background(), postaje, izdano); len(s) != 1 || s["srednja"] != 120 {
		t.Errorf("srednjaci %v, želim srednja 120", s)
	}
	if s := h.srednjaciDana(context.Background(), map[string]models.Station{}, izdano); len(s) != 0 {
		t.Errorf("letva izvan registra: %v", s)
	}
	zatvorena, err := db.OpenDB(filepath.Join(t.TempDir(), "zatvorena.db"))
	if err != nil {
		t.Fatal(err)
	}
	_ = zatvorena.Close() // namjerno zatvorena: čitanje mora javiti grešku
	h.readings = service.NewReadingService(repository.NewReadingRepository(zatvorena, ledger.New(zatvorena, "test")), nil, nil, nil, nil)
	if s := h.srednjaciDana(context.Background(), postaje, izdano); len(s) != 0 {
		t.Errorf("greška čitanja: %v", s)
	}
	h.readings = nil
	if s := h.srednjaciDana(context.Background(), postaje, izdano); len(s) != 0 {
		t.Errorf("bez očitanja: %v", s)
	}
}

// Srednjak 24 sata: svaki sat uzima očitanje najbliže punom satu, sati
// izvan zadnja 24 i očitanja bez vodostaja ne broje se, a bez 18 sati
// srednjaka nema.
func TestSrednjakDana(t *testing.T) {
	izdano := time.Date(2026, 10, 8, 19, 0, 0, 0, time.UTC)
	cm := func(x int) *int { return &x }
	o := func(prije time.Duration, v *int) models.Reading {
		return models.Reading{MeasuredAt: izdano.Add(-prije), LevelCm: v}
	}
	var rs []models.Reading
	for h := 0; h < 20; h++ {
		rs = append(rs, o(time.Duration(h)*time.Hour+20*time.Minute, cm(500))) // dalje od punog sata
		rs = append(rs, o(time.Duration(h)*time.Hour, cm(100+h)))
		rs = append(rs, o(time.Duration(h)*time.Hour-25*time.Minute, cm(700))) // isto dalje
	}
	rs = append(rs, o(time.Hour, nil), o(-2*time.Hour, cm(900)), o(30*time.Hour, cm(900)))
	if v, ok := srednjakDana(rs, izdano); !ok || math.Abs(v-109.5) > 1e-9 {
		t.Errorf("srednjak %v (%v), želim 109,5", v, ok)
	}
	if _, ok := srednjakDana(rs[:3*17], izdano); ok {
		t.Error("17 sati nije dovoljno za srednjak")
	}
}

// Svaka letva iz susjeda nema dnevni model, a oba susjeda ga imaju.
func TestLetveIzSusjedaImajuDnevneSusjede(t *testing.T) {
	for _, s := range LetveIzSusjeda {
		if jeDnevniCilj(s.Letva) || !jeDnevniCilj(s.Uzvodna) || !jeDnevniCilj(s.Nizvodna) {
			t.Errorf("%+v: letva bez dnevnog modela između dviju s njim", s)
		}
	}
}

// Kartica pokazuje oznaku p s objašnjenjem, a skriveni redak izvoza „iz
// susjeda” i napomenu ispod tablice; list bez takvih dana napomenu nema.
func TestDaniIzSusjedaNaStraniciIUExcelu(t *testing.T) {
	v := 145.0
	html := iscrtaj(t, "prognoze.html", PrognozePageData{
		CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		ActiveNav: "prognoze", Izdano: "8.10.2026. u 21:00", Udio: 70,
		Tablice: []TablicaPrognoza{{Naslov: "Rijeka", Letve: []LetvaPrognoze{{Kod: "srednja", Naziv: "Srednji Brod", Racuna: "vodostaj",
			ImaTermina: true, Dani: []CelijaDana{{Naslov: "pet 9.10.", Cm: "145", CmV: &v, IzSusjeda: true}}}}}},
	})
	for _, want := range []string{">p</sup>", "iz susjednih postaja s dnevnim modelom, po riječnom kilometru; raspon procijenjen iz susjeda",
		"Kad dnevni model susjedima najavi porast od kiše koji ne dođe, i ovdje je najava pogrešna."} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica nema %q", want)
		}
	}
	data := PrognozePageData{Udio: 70, Izdano: "8.10.2026. u 21:00", Dani: []string{"pet 9.10."}}
	tab := TablicaPrognoza{Naslov: "Rijeka", Letve: []LetvaPrognoze{{Kod: "srednja", Naziv: "Srednji Brod", Racuna: "vodostaj",
		SadaCmV: &v, Dani: []CelijaDana{{CmV: &v, IzSusjeda: true}}}}}
	k := &xlsxw.Knjiga{}
	listPrognoze(k, ZaglavljeIzvoza{Potpisnici: []PotpisnikIzvoza{{Funkcija: "voditelj Centra obrane od poplava", Ime: "Ana Anić"}}}, data, tab)
	txt := tekstKnjige(t, k)
	for _, want := range []string{">iz susjeda<", "(skriveni redak „model”: iz susjeda) slijede susjedne postaje"} {
		if !strings.Contains(txt, want) {
			t.Errorf("u izvozu nema %q", want)
		}
	}
	if napomenaSusjeda([]LetvaPrognoze{{Dani: []CelijaDana{{CmV: &v}}}}) != "" {
		t.Error("napomena o susjedima stoji, a nijedan dan nije iz susjeda")
	}
}

// Kvadratić na Postavkama prognoze: označen dok su dani iz susjeda
// uključeni; spremanje bez njega ih isključuje, s njim uključuje.
func TestPostavkeDanaIzSusjeda(t *testing.T) {
	osnova := PrognozePageData{
		CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		ActiveNav: "prognoze", MozeGenerirati: true, SmijeMijenjati: true,
	}
	if html := iscrtaj(t, "prognoze_postavke.html", osnova); !strings.Contains(html, `name="prognoza_susjedi" id="prognoza_susjedi" value="1" checked`) {
		t.Error("kvadratić dana iz susjeda mora biti označen dok su uključeni")
	}
	osnova.Opcije.PrognozaBezSusjeda = true
	if html := iscrtaj(t, "prognoze_postavke.html", osnova); strings.Contains(html, `id="prognoza_susjedi" value="1" checked`) {
		t.Error("kvadratić dana iz susjeda označen, a isključeni su")
	}
	var spremljeno models.Opcije
	h := &PrognozeHandler{}
	h.SetOpcije(func(context.Context) models.Opcije { return models.Opcije{} },
		func(_ context.Context, _ *models.UserPermissions, o models.Opcije) error { spremljeno = o; return nil })
	for _, tc := range []struct {
		forma url.Values
		bez   bool
	}{
		{url.Values{"prognoza_prikaz": {"satni"}}, true},
		{url.Values{"prognoza_prikaz": {"satni"}, "prognoza_susjedi": {"1"}}, false},
	} {
		r := httptest.NewRequest(http.MethodPost, "/prognoze/postavke", strings.NewReader(tc.forma.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r = r.WithContext(context.WithValue(r.Context(), contextKeyPerms, &models.UserPermissions{IsGlobalAdmin: true}))
		h.SpremiPostavke(httptest.NewRecorder(), r)
		if spremljeno.PrognozaBezSusjeda != tc.bez || spremljeno.PrognozaPrikaz != models.PrognozaSatni {
			t.Errorf("forma %v: spremljeno %+v", tc.forma, spremljeno)
		}
	}
}
