package javnivodostaji

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/skok"
)

// letvaSIzvorom je letva Donji Brod s probnim izvorom koji daje zadane
// retke, nad bazom čvora
func letvaSIzvorom(t *testing.T, redci []Redak) *preuzimanjeUBazu {
	t.Helper()
	p := novoPreuzimanjeUBazu(t)
	p.st.Code = "donji-brod"
	p.u.Izvori = []Izvor{probniIzvor{naziv: Podrijetlo, redci: redci}}
	p.u.Prag = func(sifra string) float64 {
		if sifra != "donji-brod" {
			t.Errorf("prag traži %q", sifra)
		}
		return 90 // prag Donjeg Broda iz arhive 2012.–2026.
	}
	return p
}

// Odluke (25. 10. 2026.): 24. 10. 2026. vodostaji.voda.hr je za Donji
// Brod u 21 h (19:00 UTC) javio 830 cm; sat prije i poslije −123 i
// −121 cm. Skok se ne upisuje, nego bilježi za stranicu letve, a ostala
// očitanja upisuju se. Isti prozor idući sat ne broji ga dvaput.
func TestSkokDonjegBrodaNijeUpisan(t *testing.T) {
	zg := models.Zagreb
	p := letvaSIzvorom(t, []Redak{
		{Kad: time.Date(2026, 10, 24, 19, 0, 0, 0, zg), LevelCm: intPtr(-123)},
		{Kad: time.Date(2026, 10, 24, 20, 0, 0, 0, zg), LevelCm: intPtr(-123)},
		{Kad: time.Date(2026, 10, 24, 21, 0, 0, 0, zg), LevelCm: intPtr(830)},
		{Kad: time.Date(2026, 10, 24, 22, 0, 0, 0, zg), LevelCm: intPtr(-121)},
	})
	ctx := context.Background()
	s := p.u.Preuzmi(ctx, p.st)
	if s.Greska != "" || s.Novih != 3 || s.Skokova != 1 || s.Ceka != 0 || s.Odbaceno != 0 {
		t.Fatalf("stanje: %+v", s)
	}
	if n := p.broj(t, `SELECT COUNT(*) FROM readings WHERE station_id = ? AND level_cm = 830`, p.st.ID.String()); n != 0 {
		t.Error("upisan je skok od 830 cm")
	}
	sazetak, err := p.ocitanja.SazetakOdbacenih(ctx, "station:"+p.st.ID.String())
	if err != nil || sazetak == nil || sazetak.Broj != 1 || sazetak.Skokova != 1 || sazetak.Zadnja.Polje != models.PoljeSkok ||
		sazetak.Zadnja.Opis != "skok na 830 cm" || !sazetak.Zadnja.Kad.Equal(time.Date(2026, 10, 24, 19, 0, 0, 0, time.UTC)) {
		t.Fatalf("sažetak: %+v, %v", sazetak, err)
	}
	if !strings.HasPrefix(sazetak.Tekst(), "Odbačeno zbog skoka: 1, zadnje skok na 830 cm 24. 10. 2026. u 21 h") {
		t.Errorf("tekst: %q", sazetak.Tekst())
	}
	s = p.u.Preuzmi(ctx, p.st)
	if s.Novih != 0 || s.Skokova != 1 {
		t.Errorf("drugi krug: %+v", s)
	}
	if sazetak, err := p.ocitanja.SazetakOdbacenih(ctx, "station:"+p.st.ID.String()); err != nil || sazetak == nil || sazetak.Broj != 1 {
		t.Errorf("drugi krug udvostručio je skok: %+v, %v", sazetak, err)
	}
}

// Ručno ispravljeno očitanje ostaje: kad je čovjek u 21 h upisao −122 cm,
// preuzimanje 830 cm s izvora ne upisuje i ne broji kao skok.
func TestRucnoIspravljenoOcitanjeOstaje(t *testing.T) {
	zg := models.Zagreb
	p := letvaSIzvorom(t, []Redak{
		{Kad: time.Date(2026, 10, 24, 20, 0, 0, 0, zg), LevelCm: intPtr(-123)},
		{Kad: time.Date(2026, 10, 24, 21, 0, 0, 0, zg), LevelCm: intPtr(830)},
		{Kad: time.Date(2026, 10, 24, 22, 0, 0, 0, zg), LevelCm: intPtr(-121)},
	})
	cm := -122
	rucno := models.Reading{ID: uuid.New(), StationID: p.st.ID.String(), MeasuredAt: time.Date(2026, 10, 24, 21, 0, 0, 0, zg).UTC(),
		LevelCm: &cm, Source: models.ReadingSourceManual}
	if _, err := p.ocitanja.ImportBatch(context.Background(), []models.Reading{rucno}); err != nil {
		t.Fatal(err)
	}
	s := p.u.Preuzmi(context.Background(), p.st)
	if s.Greska != "" || s.Novih != 2 || s.Skokova != 0 {
		t.Fatalf("stanje: %+v", s)
	}
	if n := p.broj(t, `SELECT COUNT(*) FROM readings WHERE station_id = ? AND level_cm = -122 AND source = ?`,
		p.st.ID.String(), models.ReadingSourceManual); n != 1 {
		t.Error("ručno očitanje nije ostalo")
	}
}

// probnaLetva je letva s probnim izvorom bez baze; spremište razine pamti
// iz onoga što je upisano
func probnaLetva(redci []Redak) (*Uvoznik, *probnoSpremiste, *models.Station) {
	st := &models.Station{ID: uuid.New(), Name: "Probna", Code: "probna", JavniURL: "https://probna", JavniUvoz: true}
	sp := &probnoSpremiste{postojeca: map[int64]bool{}, razine: map[int64]int{}}
	u := NoviUvoznik(sp, nil)
	u.Izvori = []Izvor{probniIzvor{naziv: Podrijetlo, redci: redci}}
	return u, sp, st
}

// upisano kaže koje je vodostaje spremište primilo
func (p *probnoSpremiste) upisaniCm() []int {
	var out []int
	for _, o := range p.upisano {
		out = append(out, *o.LevelCm)
	}
	return out
}

// Zadnja vrijednost koja odskoči čeka iduće očitanje i ne upisuje se. Kad
// iduće potvrdi novu razinu, upišu se obje (pravi val, ispuštanje
// elektrane); kad se vrati, skok se odbacuje i bilježi.
func TestSkokCekaIducePaGaPotvrdiIliOdbaci(t *testing.T) {
	sat := time.Now().UTC().Truncate(time.Hour)
	prije := []Redak{{Kad: sat.Add(-3 * time.Hour), LevelCm: intPtr(100)}, {Kad: sat.Add(-2 * time.Hour), LevelCm: intPtr(101)}}
	skocio := Redak{Kad: sat.Add(-time.Hour), LevelCm: intPtr(400)}

	for _, x := range []struct {
		ime     string
		iduce   int
		upisano []int
		skokova int
	}{{"potvrdi", 405, []int{100, 101, 400, 405}, 0}, {"vrati se", 102, []int{100, 101, 102}, 1}} {
		u, sp, st := probnaLetva(append(append([]Redak{}, prije...), skocio))
		s := u.Preuzmi(context.Background(), st)
		if s.Greska != "" || s.Novih != 2 || s.Ceka != 1 || len(sp.odbaceno) != 0 {
			t.Fatalf("%s: prvi krug: %+v", x.ime, s)
		}
		// izvor ovaj put ne vraća skočenu vrijednost (samo zadnja na popisu):
		// o njoj se odlučuje iz onoga što čeka
		u.Izvori = []Izvor{probniIzvor{naziv: Podrijetlo, redci: []Redak{{Kad: sat, LevelCm: intPtr(x.iduce)}}}}
		sp.postojeca = map[int64]bool{}
		for _, o := range sp.upisano {
			sp.razine[o.MeasuredAt.Unix()] = *o.LevelCm
		}
		s = u.Preuzmi(context.Background(), st)
		if s.Greska != "" || s.Ceka != 0 || s.Skokova != x.skokova || len(sp.odbaceno) != x.skokova {
			t.Errorf("%s: drugi krug: %+v, odbačeno %+v", x.ime, s, sp.odbaceno)
		}
		if got := sp.upisaniCm(); len(got) != len(x.upisano) || got[len(got)-1] != x.upisano[len(x.upisano)-1] {
			t.Errorf("%s: upisano %v, želim %v", x.ime, got, x.upisano)
		}
		if c := u.cekaju[st.ID.String()]; len(c.ocitanja) != 0 {
			t.Errorf("%s: nešto i dalje čeka", x.ime)
		}
	}
}

// Sumnjiva vrijednost koja čeka dulje od NajduljeCekanje od kad je prvi
// put stigla odbacuje se; dok toliko nije prošlo, čeka. Vrijednost koja je
// već stara kad prvi put stigne (rezervni izvor, oporavak nakon ispada)
// također čeka svojih šest sati.
func TestRazvrstajSkokove(t *testing.T) {
	zg := models.Zagreb
	kad := func(h int) time.Time { return time.Date(2026, 10, 24, h, 0, 0, 0, zg).UTC() }
	rd := func(h, cm int) models.Reading {
		return models.Reading{StationID: "s", MeasuredAt: kad(h), LevelCm: intPtr(cm), Origin: Podrijetlo}
	}
	razine := map[int64]int{kad(19).Unix(): -123, kad(20).Unix(): -123}
	stiglo := map[int64]time.Time{kad(21).Unix(): kad(21)}
	upis, ceka, odb := razvrstajSkokove([]models.Reading{rd(21, 830)}, razine, 90, kad(21).Add(5*time.Hour), stiglo)
	if len(upis) != 0 || len(ceka) != 1 || len(odb) != 0 {
		t.Errorf("unutar čekanja: %d %d %d", len(upis), len(ceka), len(odb))
	}
	upis, ceka, odb = razvrstajSkokove([]models.Reading{rd(21, 830)}, razine, 90, kad(21).Add(7*time.Hour), stiglo)
	if len(upis) != 0 || len(ceka) != 0 || len(odb) != 1 || odb[0].Polje != models.PoljeSkok {
		t.Errorf("nakon čekanja: %d %d %+v", len(upis), len(ceka), odb)
	}
	upis, ceka, odb = razvrstajSkokove([]models.Reading{rd(21, 830)}, razine, 90, kad(21).Add(7*time.Hour), nil)
	if len(upis) != 0 || len(ceka) != 1 || len(odb) != 0 {
		t.Errorf("stiglo kasno: %d %d %d", len(upis), len(ceka), len(odb))
	}
	// izvor s dnevnim vrijednostima: 77 cm dan poslije −123 nakon rupe
	// čeka iduća dva, a kad ne stignu za šest sati, upiše se (ništa ne kaže
	// da je skok)
	dnevno := map[int64]int{kad(21).Add(-24 * time.Hour).Unix(): -123}
	upis, ceka, odb = razvrstajSkokove([]models.Reading{rd(21, 77)}, dnevno, 50, kad(21).Add(5*time.Hour), stiglo)
	if len(upis) != 0 || len(ceka) != 1 || len(odb) != 0 {
		t.Errorf("dnevno, unutar čekanja: %d %d %d", len(upis), len(ceka), len(odb))
	}
	upis, ceka, odb = razvrstajSkokove([]models.Reading{rd(21, 77)}, dnevno, 50, kad(21).Add(7*time.Hour), stiglo)
	if len(upis) != 1 || len(ceka) != 0 || len(odb) != 0 {
		t.Errorf("dnevno, nakon čekanja: %d %d %d", len(upis), len(ceka), len(odb))
	}
}

// Vrijednost koja je stigla sedam sati kasno čeka svoje iduće i kad ga
// izvor ne javi, a nakon šest sati od stizanja odbacuje se; kad je izvor
// poslije opet vrati, ne čeka iznova. Čekanje očitanja koja izvor više ne
// vraća zaboravlja se.
func TestCekanjeSeMjeriOdStizanja(t *testing.T) {
	u, sp, st := probnaLetva(nil)
	sat := time.Date(2026, 10, 25, 12, 0, 0, 0, models.Zagreb).UTC()
	sp.razine = map[int64]int{sat.Add(-9 * time.Hour).Unix(): 100, sat.Add(-8 * time.Hour).Unix(): 101}
	kasno := []models.Reading{{StationID: st.ID.String(), MeasuredAt: sat.Add(-7 * time.Hour), LevelCm: intPtr(400), Origin: Podrijetlo}}
	var s StanjeLetve
	if out, err := u.bezSkokova(context.Background(), st, kasno, sat, &s); err != nil || len(out) != 0 || s.Ceka != 1 || len(sp.odbaceno) != 0 {
		t.Fatalf("stiglo kasno: %d, %v, %+v", len(out), err, s)
	}
	if out, err := u.bezSkokova(context.Background(), st, nil, sat.Add(5*time.Hour), &s); err != nil || len(out) != 0 || s.Ceka != 1 {
		t.Fatalf("pet sati poslije: %d, %v, %+v", len(out), err, s)
	}
	if _, err := u.bezSkokova(context.Background(), st, nil, sat.Add(7*time.Hour), &s); err != nil || s.Skokova != 1 || len(sp.odbaceno) != 1 {
		t.Fatalf("sedam sati poslije: %v, %+v, %+v", err, s, sp.odbaceno)
	}
	if _, err := u.bezSkokova(context.Background(), st, kasno, sat.Add(8*time.Hour), &s); err != nil || s.Ceka != 0 || s.Skokova != 1 {
		t.Fatalf("izvor ga opet vrati: %v, %+v", err, s)
	}
	if _, err := u.bezSkokova(context.Background(), st, nil, sat.Add(3*24*time.Hour), &s); err != nil {
		t.Fatal(err)
	}
	if _, ima := u.cekaju[st.ID.String()]; ima {
		t.Errorf("čekanje nije zaboravljeno: %+v", u.cekaju[st.ID.String()])
	}
}

// Zaglavljeno −1102 cm upisano prije provjere raspona nije razina od koje
// se mjeri: inače bi ispravna vrijednost 3 cm između dva −1102 ispala kao
// skok (Odluke (8. 10. 2026.)).
func TestRazinaIzvanRasponaSeNeMjeri(t *testing.T) {
	u, sp, st := probnaLetva(nil)
	kad := func(h int) time.Time { return time.Date(2026, 10, 24, h, 0, 0, 0, models.Zagreb).UTC() }
	sp.razine = map[int64]int{kad(18).Unix(): -1102, kad(20).Unix(): -1102}
	nova := []models.Reading{
		{StationID: st.ID.String(), MeasuredAt: kad(19), LevelCm: intPtr(3), Origin: Podrijetlo},
		{StationID: st.ID.String(), MeasuredAt: kad(21), LevelCm: intPtr(4), Origin: Podrijetlo},
	}
	var s StanjeLetve
	out, err := u.bezSkokova(context.Background(), st, nova, kad(22), &s)
	if err != nil || len(out) != 2 || s.Skokova != 0 || s.Ceka != 0 {
		t.Errorf("upisano %d, %v, %+v", len(out), err, s)
	}
}

// Ručni upis u satu vrijednosti koja čeka: idući krug je ne upisuje, nego
// u tom satu ostaje samo ručno očitanje (bezUpisanih).
func TestRucniUpisDokSkokCeka(t *testing.T) {
	sat := time.Now().UTC().Truncate(time.Hour)
	p := letvaSIzvorom(t, []Redak{
		{Kad: sat.Add(-3 * time.Hour), LevelCm: intPtr(100)},
		{Kad: sat.Add(-2 * time.Hour), LevelCm: intPtr(101)},
		{Kad: sat.Add(-time.Hour), LevelCm: intPtr(400)},
	})
	ctx := context.Background()
	if s := p.u.Preuzmi(ctx, p.st); s.Greska != "" || s.Novih != 2 || s.Ceka != 1 {
		t.Fatalf("prvi krug: %+v", s)
	}
	cm := 102
	rucno := models.Reading{ID: uuid.New(), StationID: p.st.ID.String(), MeasuredAt: sat.Add(-time.Hour), LevelCm: &cm, Source: models.ReadingSourceManual}
	if _, err := p.ocitanja.ImportBatch(ctx, []models.Reading{rucno}); err != nil {
		t.Fatal(err)
	}
	p.u.Izvori = []Izvor{probniIzvor{naziv: Podrijetlo, redci: []Redak{{Kad: sat, LevelCm: intPtr(103)}}}}
	if s := p.u.Preuzmi(ctx, p.st); s.Greska != "" || s.Ceka != 0 || s.Skokova != 0 {
		t.Fatalf("drugi krug: %+v", s)
	}
	if n := p.broj(t, `SELECT COUNT(*) FROM readings WHERE station_id = ? AND measured_at = ?`, p.st.ID.String(), sat.Add(-time.Hour)); n != 1 {
		t.Errorf("u satu ručnog upisa %d očitanja", n)
	}
	if n := p.broj(t, `SELECT COUNT(*) FROM readings WHERE station_id = ? AND level_cm = 400`, p.st.ID.String()); n != 0 {
		t.Error("upisana je vrijednost koja je čekala")
	}
}

// Prvo očitanje nakon rupe: letva je zadnje imala −123 cm prije 30 sati,
// a izvor vrati 830, −121, −120. Prag za 30 sati rupe dopušta i 950 cm, ali
// 830 strši od oba iduća, pa se ne upisuje nego bilježi kao skok.
func TestSkokNakonRupeNijeUpisan(t *testing.T) {
	sat := time.Now().UTC().Truncate(time.Hour)
	p := letvaSIzvorom(t, []Redak{
		{Kad: sat.Add(-2 * time.Hour), LevelCm: intPtr(830)},
		{Kad: sat.Add(-time.Hour), LevelCm: intPtr(-121)},
		{Kad: sat, LevelCm: intPtr(-120)},
	})
	cm := -123
	staro := models.Reading{ID: uuid.New(), StationID: p.st.ID.String(), MeasuredAt: sat.Add(-32 * time.Hour), LevelCm: &cm,
		Source: models.ReadingSourceImport, Origin: Podrijetlo}
	ctx := context.Background()
	if _, err := p.ocitanja.ImportBatch(ctx, []models.Reading{staro}); err != nil {
		t.Fatal(err)
	}
	// izvor prvo vrati samo 830: zadnja razina prije 30 sati kaže da je
	// ispred rupa, pa 830 čeka iduća očitanja, a ne upisuje se
	izvor := p.u.Izvori
	p.u.Izvori = []Izvor{probniIzvor{naziv: Podrijetlo, redci: []Redak{{Kad: sat.Add(-2 * time.Hour), LevelCm: intPtr(830)}}}}
	if s := p.u.Preuzmi(ctx, p.st); s.Greska != "" || s.Novih != 0 || s.Ceka != 1 {
		t.Fatalf("samo 830: %+v", s)
	}
	p.u.Izvori = izvor
	if s := p.u.Preuzmi(ctx, p.st); s.Greska != "" || s.Novih != 2 || s.Skokova != 1 || s.Ceka != 0 {
		t.Fatalf("stanje: %+v", s)
	}
	if n := p.broj(t, `SELECT COUNT(*) FROM readings WHERE station_id = ? AND level_cm = 830`, p.st.ID.String()); n != 0 {
		t.Error("upisan je skok nakon rupe")
	}
}

// Bez praga vrijedi zadani; očitanje bez vodostaja ili izvan raspona ide
// dalje netaknuto, a vodostaj iz istog trenutka uzima se jednom.
func TestBezSkokovaRubovi(t *testing.T) {
	u, sp, st := probnaLetva(nil)
	if p := u.pragZa("bilo"); p != skok.ZadaniPrag {
		t.Errorf("zadani prag: %v", p)
	}
	kad := time.Date(2026, 10, 24, 21, 0, 0, 0, models.Zagreb).UTC()
	temp := 12.0
	nova := []models.Reading{
		{MeasuredAt: kad, TempC: &temp},
		{MeasuredAt: kad, LevelCm: intPtr(-1102)},
		{MeasuredAt: kad.Add(time.Hour), LevelCm: intPtr(50)},
		{MeasuredAt: kad.Add(time.Hour), LevelCm: intPtr(51)},
	}
	var s StanjeLetve
	out, err := u.bezSkokova(context.Background(), st, nova, kad, &s)
	if err != nil || len(out) != 3 {
		t.Errorf("izlaz %d, %v", len(out), err)
	}
	// samo očitanja bez vodostaja: ne pita bazu
	sp.greskaRazina = errors.New("baza ne radi")
	if out, err := u.bezSkokova(context.Background(), st, nova[:1], kad, &s); err != nil || len(out) != 1 {
		t.Errorf("bez vodostaja: %d, %v", len(out), err)
	}
}

// Kad se razine ne daju pročitati ili skok zabilježiti, ništa se ne upiše,
// a stanje letve javlja grešku (sljedeći krug pokuša opet).
func TestBezSkokovaGreske(t *testing.T) {
	sat := time.Now().UTC().Truncate(time.Hour)
	redci := []Redak{{Kad: sat.Add(-3 * time.Hour), LevelCm: intPtr(100)}, {Kad: sat.Add(-2 * time.Hour), LevelCm: intPtr(900)},
		{Kad: sat.Add(-time.Hour), LevelCm: intPtr(101)}}
	for _, x := range []struct {
		ime     string
		postavi func(*probnoSpremiste)
	}{
		{"razine", func(p *probnoSpremiste) { p.greskaRazina = errors.New("razine nisu pročitane") }},
		{"bilježenje", func(p *probnoSpremiste) { p.greskaOdbacenih = errors.New("skok nije zabilježen") }},
	} {
		u, sp, st := probnaLetva(redci)
		x.postavi(sp)
		var zapis []string
		u.Zapisnik = func(f string, a ...any) { zapis = append(zapis, f) }
		s := u.Preuzmi(context.Background(), st)
		if s.Greska == "" || s.Novih != 0 || len(sp.upisano) != 0 || len(zapis) != 1 {
			t.Errorf("%s: %+v, upisano %d, dnevnik %q", x.ime, s, len(sp.upisano), zapis)
		}
	}
}

// greskaIzvor je izvor koji ne odgovara
type greskaIzvor struct{}

func (greskaIzvor) Naziv() string          { return Podrijetlo }
func (greskaIzvor) Prepoznaje(string) bool { return true }
func (greskaIzvor) Ocitanja(context.Context, string) ([]Redak, error) {
	return nil, errors.New("izvor ne odgovara")
}

// Preuzimanje bez adrese, s izvorom koji ne odgovara, bez ijednog očitanja
// ili samo s očitanjima iz budućnosti ne upisuje ništa i ne čeka ništa, a
// letva prebačena na telemetriju čita se s telemetrije.
func TestPreuzimanjeBezIcegaZaUpis(t *testing.T) {
	u, sp, st := probnaLetva(nil)
	bez := *st
	bez.JavniURL = ""
	if s := u.Preuzmi(context.Background(), &bez); s.Greska != "letva nema adresu javne stranice" {
		t.Errorf("bez adrese: %+v", s)
	}
	tele := *st
	tele.TelemetrijaUvoz, tele.TelemetrijaSite = true, "probno-mjesto"
	if s := u.Preuzmi(context.Background(), &tele); !strings.Contains(s.Greska, "očitanje") {
		t.Errorf("telemetrija bez očitanja: %+v", s)
	}
	u.Izvori = []Izvor{greskaIzvor{}}
	if s := u.Preuzmi(context.Background(), st); s.Greska != "izvor ne odgovara" {
		t.Errorf("izvor ne odgovara: %+v", s)
	}
	u.Izvori = []Izvor{probniIzvor{naziv: Podrijetlo, redci: []Redak{{Kad: time.Now().Add(3 * time.Hour), LevelCm: intPtr(100)}}}}
	if s := u.Preuzmi(context.Background(), st); s.Greska != "" || s.Preuzeto != 1 || s.Novih != 0 || len(sp.upisano) != 0 {
		t.Errorf("samo iz budućnosti: %+v", s)
	}
}
