package javnivodostaji

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Letve i vodostaji u ovim testovima su izmišljeni.

const probnoPodrijetlo = "vodostaji.voda.hr"

var pocetakIspravka = time.Date(2026, 11, 17, 6, 0, 0, 0, time.UTC) // 7 h po našem

// uSat je trenutak dan i sat nakon početka.
func uSat(dan, sat int) time.Time {
	return pocetakIspravka.Add(time.Duration(dan*24+sat) * time.Hour)
}

func rucno(dan, cm int) models.UpisanoOcitanje {
	return models.UpisanoOcitanje{ID: uuid.New(), Kad: uSat(dan, 0), Cm: cm, Rucno: true}
}

// preuzeto je upisano očitanje s izvora: cm je upisano, sirovo vrijednost
// mjernog uređaja (isto kad nije ispravljano).
func preuzeto(dan, sat, cm, sirovo int) models.UpisanoOcitanje {
	o := models.UpisanoOcitanje{ID: uuid.New(), Kad: uSat(dan, sat), Cm: cm, Podrijetlo: probnoPodrijetlo}
	if sirovo != cm {
		o.SirovoCm = &sirovo
	}
	return o
}

// satniRedci su retci izvora od zadanog dana, svaki sat ista vrijednost.
func satniRedci(odDana, sati, cm int) []Redak {
	out := make([]Redak, 0, sati)
	for h := range sati {
		out = append(out, Redak{Kad: uSat(odDana, h), LevelCm: intPtr(cm)})
	}
	return out
}

// Ispravak počinje tek kad razliku potvrde dva uzastopna ručna očitanja,
// razlika unutar točnosti ne ispravlja se, a jedna tipkarska greška ne mijenja
// ispravak koji već vrijedi.
func TestNoviIspravak(t *testing.T) {
	p := func(v int) *int { return &v }
	for _, s := range []struct {
		ime              string
		razlika, vrijedi int
		prethodna        *int
		htio             int
	}{
		{"unutar točnosti", -5, 0, nil, 0},
		{"prvo očitanje čeka potvrdu", -43, 0, nil, 0},
		{"drugo potvrđuje prvo", -44, 0, p(-43), -44},
		{"drugo se ne slaže s prvim", -43, 0, p(-103), 0},
		{"prethodno je bilo unutar točnosti", -12, 0, p(-4), 0},
		{"slaže se s ispravkom koji vrijedi", -40, -43, p(-103), -40},
		{"tipkarska greška ne mijenja ispravak", -103, -43, p(-43), -43},
		{"uređaj je popravljen", 2, -43, p(-43), 0},
		{"uređaj je skočio, prvo očitanje", -20, -43, p(-43), -43},
		{"uređaj je skočio, drugo potvrđuje", -21, -43, p(-20), -21},
	} {
		if got := noviIspravak(s.razlika, s.vrijedi, s.prethodna); got != s.htio {
			t.Errorf("%s: %d, želim %d", s.ime, got, s.htio)
		}
	}
}

// Plan iz ručnih očitanja: mjerni uređaj pokazuje 355, motritelj tri dana
// zaredom čita 312, 313 i 311. Ispravak vrijedi od drugog očitanja.
func TestPlanIspravkaOdDrugogOcitanja(t *testing.T) {
	upisana := []models.UpisanoOcitanje{rucno(0, 312), rucno(1, 313), rucno(2, 311)}
	plan := sloziPlanIspravka(upisana, satniRedci(0, 60, 355), probnoPodrijetlo, bezUmjerenja)
	for _, s := range []struct {
		kad  time.Time
		htio int
	}{
		{uSat(0, 5), 0}, {uSat(1, 0), 0}, {uSat(1, 1), -42}, {uSat(2, 0), -42}, {uSat(2, 3), -44},
	} {
		if got := plan.pomakU(s.kad); got != s.htio {
			t.Errorf("%v: ispravak %d, želim %d", s.kad, got, s.htio)
		}
	}
	// samo jedno ručno očitanje ne ispravlja ništa
	if plan := sloziPlanIspravka(upisana[:1], satniRedci(0, 30, 355), probnoPodrijetlo, bezUmjerenja); len(plan) != 1 || plan[0].pomak != 0 {
		t.Errorf("jedno očitanje: plan %+v", plan)
	}
}

// Ispravak nadživi raspon ručnih očitanja: bez novog ručnog vrijedi onaj
// kojim je ispravljeno zadnje preuzeto očitanje. Ručno očitanje bez
// vrijednosti mjernog uređaja u blizini se preskače, a tuđi izvor ne broji.
func TestPlanIspravkaBezNovogRucnog(t *testing.T) {
	upisana := []models.UpisanoOcitanje{preuzeto(0, 1, 312, 355), preuzeto(0, 2, 313, 356),
		{ID: uuid.New(), Kad: uSat(0, 3), Cm: 500, Podrijetlo: "zalijepljeno"}}
	plan := sloziPlanIspravka(upisana, satniRedci(1, 5, 357), probnoPodrijetlo, bezUmjerenja)
	if got := plan.pomakU(uSat(1, 2)); got != -43 {
		t.Errorf("ispravak %d, želim -43", got)
	}
	// ručno očitanje daleko od svakog mjerenja uređaja: ne zna se razlika
	upisana = append(upisana, models.UpisanoOcitanje{ID: uuid.New(), Kad: uSat(5, 0), Cm: 300, Rucno: true})
	plan = sloziPlanIspravka(upisana, satniRedci(1, 5, 357), probnoPodrijetlo, bezUmjerenja)
	if len(plan) != 1 || plan[0].pomak != -43 {
		t.Errorf("ručno bez mjerenja uređaja promijenilo je plan: %+v", plan)
	}
}

// Vrijednost mjernog uređaja uz ručno očitanje: najbliži redak izvora unutar
// pola sata, inače najbliže upisano preuzeto očitanje unutar sat i pol, i to
// njegova sirova vrijednost.
func TestSirovoU(t *testing.T) {
	redci := []Redak{{Kad: uSat(0, 0).Add(-20 * time.Minute), LevelCm: intPtr(353)}, {Kad: uSat(0, 0).Add(10 * time.Minute), LevelCm: intPtr(355)},
		{Kad: uSat(0, 0), TempC: floatPtr(4)}}
	if v, ima := sirovoU(uSat(0, 0), redci, nil, bezUmjerenja); !ima || v != 355 {
		t.Errorf("iz redaka: %d %v", v, ima)
	}
	preuzeta := []models.UpisanoOcitanje{preuzeto(0, 1, 312, 356)}
	if v, ima := sirovoU(uSat(0, 0), redci[:1][:0], preuzeta, bezUmjerenja); !ima || v != 356 {
		t.Errorf("iz upisanog: %d %v", v, ima)
	}
	if _, ima := sirovoU(uSat(0, 4), nil, preuzeta, bezUmjerenja); ima {
		t.Error("očitanje tri sata dalje uzeto je kao istodobno")
	}
}

// Preuzimanje ispravlja nove retke i usklađuje one koji su upisani prije nego
// što je ručno očitanje stiglo. Sirova vrijednost ostaje uz očitanje.
func TestPreuzimanjeIspravljaPoRucnom(t *testing.T) {
	st := models.Station{ID: uuid.New(), Name: "Jezero", Code: "jezero", JavniURL: "https://mvodostaji.voda.hr/Home/PregledVodostajaPostaje?sektorID=2&bpID=0&postajaID=1", JavniUvoz: true}
	kasno := preuzeto(1, 1, 355, 355) // upisano prije nego što je motritelj upisao drugo očitanje
	sp := &probnoSpremiste{letve: []models.Station{st}, postojeca: map[int64]bool{},
		vodostaji: []models.UpisanoOcitanje{rucno(0, 312), rucno(1, 313), kasno}}
	u := NoviUvoznik(sp, nil)
	var dnevnik []string
	u.Zapisnik = func(f string, a ...any) { dnevnik = append(dnevnik, f) }
	redci := satniRedci(0, 30, 355)
	if err := u.ispraviPoRucnom(context.Background(), &st, probnoPodrijetlo, redci); err != nil {
		t.Fatal(err)
	}
	if r := redci[10]; *r.LevelCm != 355 || r.SirovoCm != nil {
		t.Errorf("prije potvrde: %d, sirovo %v", *r.LevelCm, r.SirovoCm)
	}
	if r := redci[26]; *r.LevelCm != 313 || r.SirovoCm == nil || *r.SirovoCm != 355 {
		t.Errorf("poslije potvrde: %d, sirovo %v", *r.LevelCm, r.SirovoCm)
	}
	if len(sp.ispravljeno) != 1 || sp.ispravljeno[0].ID != kasno.ID || sp.ispravljeno[0].LevelCm != 313 || *sp.ispravljeno[0].SirovoCm != 355 {
		t.Errorf("usklađeno: %+v", sp.ispravljeno)
	}
	if len(dnevnik) != 1 || !strings.Contains(dnevnik[0], "ispravak po ručnom očitanju") {
		t.Errorf("dnevnik: %v", dnevnik)
	}
	if rd := Ocitanje(&st, redci[26], probnoPodrijetlo); rd.IspravakCm() != -42 {
		t.Errorf("očitanje nosi ispravak %d, želim -42", rd.IspravakCm())
	}
}

// Kad se ručno očitanje opet složi s mjernim uređajem, ispravak prestaje i već
// upisana ispravljena očitanja poslije njega vraćaju se na sirovu vrijednost.
func TestIspravakPrestaje(t *testing.T) {
	st := models.Station{ID: uuid.New(), Name: "Jezero", Code: "jezero"}
	ispravljeno := preuzeto(1, 2, 312, 355)
	sp := &probnoSpremiste{vodostaji: []models.UpisanoOcitanje{preuzeto(0, 1, 312, 355), rucno(1, 353), ispravljeno}}
	u := NoviUvoznik(sp, nil)
	u.Zapisnik = func(string, ...any) {}
	redci := satniRedci(1, 6, 355)
	if err := u.ispraviPoRucnom(context.Background(), &st, probnoPodrijetlo, redci); err != nil {
		t.Fatal(err)
	}
	if r := redci[4]; *r.LevelCm != 355 || r.SirovoCm != nil {
		t.Errorf("nakon popravka uređaja: %d, sirovo %v", *r.LevelCm, r.SirovoCm)
	}
	if len(sp.ispravljeno) != 1 || sp.ispravljeno[0].LevelCm != 355 || sp.ispravljeno[0].SirovoCm != nil {
		t.Errorf("usklađeno: %+v", sp.ispravljeno)
	}
}

// Bez redaka s vodostajem nema što ispravljati; greška baze prekida
// preuzimanje letve, da se ne upiše neispravljena vrijednost.
func TestIspravakRubovi(t *testing.T) {
	st := models.Station{ID: uuid.New(), Name: "Jezero", Code: "jezero", JavniURL: "https://mvodostaji.voda.hr/Home/PregledVodostajaPostaje?sektorID=2&bpID=34&postajaID=424", JavniUvoz: true}
	sp := &probnoSpremiste{letve: []models.Station{st}, greskaVodostaja: errors.New("baza ne odgovara")}
	u := NoviUvoznik(sp, nil)
	var dnevnik []string
	u.Zapisnik = func(f string, a ...any) { dnevnik = append(dnevnik, f) }
	if err := u.ispraviPoRucnom(context.Background(), &st, probnoPodrijetlo, []Redak{{Kad: uSat(0, 0), TempC: floatPtr(3)}}); err != nil {
		t.Errorf("retci bez vodostaja: %v", err)
	}
	u.Client.HTTP = &http.Client{Transport: probniPrijenos{probnaTablica}}
	s := u.Preuzmi(context.Background(), &st)
	if s.Greska != "baza ne odgovara" || len(sp.upisano) != 0 || !slices.ContainsFunc(dnevnik, func(d string) bool { return strings.Contains(d, "ispravak po ručnom očitanju: %v") }) {
		t.Errorf("greška baze: stanje %+v, upisano %d, dnevnik %v", s, len(sp.upisano), dnevnik)
	}
	// greška pri usklađivanju upisanih isto prekida
	sp.greskaVodostaja, sp.greskaIspravka = nil, errors.New("upis ne ide")
	sp.vodostaji = []models.UpisanoOcitanje{rucno(0, 312), rucno(1, 313), preuzeto(1, 1, 355, 355)}
	if err := u.ispraviPoRucnom(context.Background(), &st, probnoPodrijetlo, satniRedci(0, 30, 355)); err == nil {
		t.Error("greška usklađivanja nije javljena")
	}
}

// bezUmjerenja je preračun letve bez umjerenja mjernog uređaja.
func bezUmjerenja(cm int) int { return cm }

// Letva s umjerenjem (uređaj = 300 + 0,6 · letva): uređaj pokazuje 600, a to
// je 500 cm na letvi. Preuzeto se preračuna i prije ikakvog ručnog očitanja,
// a očitanje upisano prije umjerenja ne nosi ispravak koji bi se prenio dalje.
// Ručno koje se slaže s preračunatim ne pokreće ispravak.
func TestUmjerenjeUredjaja(t *testing.T) {
	ods, nag := 300.0, 0.6
	st := models.Station{ID: uuid.New(), Name: "Jezero", Code: "jezero", UmjerenjeOdsjecak: &ods, UmjerenjeNagib: &nag}
	sp := &probnoSpremiste{vodostaji: []models.UpisanoOcitanje{preuzeto(0, 1, 600, 600), rucno(1, 498), rucno(2, 502)}}
	u := NoviUvoznik(sp, nil)
	u.Zapisnik = func(string, ...any) {}
	redci := satniRedci(0, 60, 600)
	if err := u.ispraviPoRucnom(context.Background(), &st, probnoPodrijetlo, redci); err != nil {
		t.Fatal(err)
	}
	for _, i := range []int{2, 30, 59} {
		if r := redci[i]; *r.LevelCm != 500 || r.SirovoCm == nil || *r.SirovoCm != 600 {
			t.Errorf("redak %d: %d, sirovo %v; želim 500 uz uređaj 600", i, *r.LevelCm, r.SirovoCm)
		}
	}
	if len(sp.ispravljeno) != 0 {
		t.Errorf("usklađeno: %+v, želim ništa", sp.ispravljeno)
	}

	// Kad ručno odstupa i od preračunatog, ispravak se računa od preračunatog.
	sp = &probnoSpremiste{vodostaji: []models.UpisanoOcitanje{rucno(1, 480), rucno(2, 481)}}
	u = NoviUvoznik(sp, nil)
	u.Zapisnik = func(string, ...any) {}
	redci = satniRedci(0, 60, 600)
	if err := u.ispraviPoRucnom(context.Background(), &st, probnoPodrijetlo, redci); err != nil {
		t.Fatal(err)
	}
	if r := redci[50]; *r.LevelCm != 481 || *r.SirovoCm != 600 {
		t.Errorf("uz ispravak po ručnom: %d, sirovo %d; želim 481 uz uređaj 600", *r.LevelCm, *r.SirovoCm)
	}
}

