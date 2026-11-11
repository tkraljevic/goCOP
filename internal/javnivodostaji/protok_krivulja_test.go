package javnivodostaji

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// probnaKrivulja: Q = 100·H + 50 (H u metrima) od 0 do 300 cm, vrijedi od
// 1. 1. 2026.; starija krivulja do kraja 2025. daje uvijek dvostruko
func probneKrivulje() []models.HQKrivulja {
	return []models.HQKrivulja{
		{Letva: "botovo", VrijediOd: "2026-01-01", Odsjecci: []models.HQOdsjecak{
			{OdCm: 0, DoCm: 300, Oblik: models.OblikPolinom, P2: 100, P3: 50}}},
		{Letva: "botovo", VrijediOd: "2000-01-01", VrijediDo: "2025-12-31", Odsjecci: []models.HQOdsjecak{
			{OdCm: 0, DoCm: 300, Oblik: models.OblikPolinom, P2: 200, P3: 100}}},
	}
}

func TestDopuniKrivuljom(t *testing.T) {
	kad := time.Date(2026, 11, 10, 6, 0, 0, 0, time.UTC)
	redci := []Redak{
		{Kad: kad, LevelCm: intPtr(100)},                                          // dopuni: 150
		{Kad: kad.Add(time.Hour), LevelCm: intPtr(120), FlowM3s: floatPtr(7)},     // izvor je dao, ostaje
		{Kad: kad.Add(2 * time.Hour), LevelCm: intPtr(350)},                       // izvan raspona, ostaje prazno
		{Kad: kad.Add(3 * time.Hour)},                                             // bez vodostaja
		{Kad: time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC), LevelCm: intPtr(100)}, // starija krivulja: 300
		{Kad: time.Date(1999, 6, 1, 12, 0, 0, 0, time.UTC), LevelCm: intPtr(100)}, // nijedna ne vrijedi
	}
	if n := dopuniKrivuljom(redci, probneKrivulje()); n != 2 {
		t.Fatalf("dopunjeno %d, očekivano 2", n)
	}
	if q := redci[0].FlowM3s; q == nil || *q != 150 || redci[0].FlowMetoda != models.FlowMethodKrivulja || redci[0].FlowBiljeska != BiljeskaNaseKrivulje {
		t.Errorf("u rasponu: %+v", redci[0])
	}
	if q := redci[1].FlowM3s; q == nil || *q != 7 || redci[1].FlowMetoda != "" {
		t.Errorf("protok izvora se ne smije dirati: %+v", redci[1])
	}
	for _, i := range []int{2, 3, 5} {
		if redci[i].FlowM3s != nil {
			t.Errorf("redak %d ne smije dobiti protok: %v", i, *redci[i].FlowM3s)
		}
	}
	if q := redci[4].FlowM3s; q == nil || *q != 300 {
		t.Errorf("starija krivulja: %+v", redci[4])
	}
}

// HidroView ispod svoje krivulje upiše protok 0 (Belišće, 9.–11. 11. 2026.).
// Gdje naša krivulja kaže da rijeka teče, nula se odbaci i protok se izračuna
// našom krivuljom; ispod njezina raspona ostaje prazan. Na potoku čija
// krivulja i sama dolazi do nule, nula ostaje.
func TestOdbaciNuluIzvora(t *testing.T) {
	kad := time.Date(2026, 11, 10, 6, 0, 0, 0, time.UTC)
	potok := []models.HQKrivulja{{Letva: "potok", VrijediOd: "2026-01-01", Odsjecci: []models.HQOdsjecak{
		{OdCm: 0, DoCm: 300, Oblik: models.OblikPolinom, P2: 100}}}}
	redci := []Redak{
		{Kad: kad, LevelCm: intPtr(100), FlowM3s: floatPtr(0)},                    // u rasponu: naša 150
		{Kad: kad.Add(time.Hour), LevelCm: intPtr(-3), FlowM3s: floatPtr(0)},      // ispod raspona: prazno
		{Kad: kad.Add(2 * time.Hour), LevelCm: intPtr(120), FlowM3s: floatPtr(7)}, // pravi protok ostaje
		{Kad: kad.Add(3 * time.Hour), FlowM3s: floatPtr(0)},                       // bez vodostaja ostaje
	}
	u := NoviUvoznik(&probnoSpremiste{}, nil)
	u.Krivulje = func(context.Context, string) ([]models.HQKrivulja, error) { return probneKrivulje(), nil }
	u.protokIzKrivulje(context.Background(), &models.Station{Code: "belisce"}, redci)
	if q := redci[0].FlowM3s; q == nil || *q != 150 || redci[0].FlowMetoda != models.FlowMethodKrivulja {
		t.Errorf("nula u rasponu krivulje: %+v", redci[0])
	}
	if redci[1].FlowM3s != nil {
		t.Errorf("nula ispod krivulje mora ostati prazna, a ne %v", *redci[1].FlowM3s)
	}
	if q := redci[2].FlowM3s; q == nil || *q != 7 {
		t.Errorf("pravi protok izvora: %+v", redci[2])
	}
	if q := redci[3].FlowM3s; q == nil || *q != 0 {
		t.Errorf("nula bez vodostaja: %+v", redci[3])
	}

	suho := []Redak{
		{Kad: kad, LevelCm: intPtr(0), FlowM3s: floatPtr(0)},
		{Kad: kad, LevelCm: intPtr(50), FlowM3s: floatPtr(0)},
	}
	if n := odbaciNulu(suho, potok); n != 1 || suho[0].FlowM3s == nil || suho[1].FlowM3s != nil {
		t.Errorf("potok: odbačeno %d, %+v", n, suho)
	}
}

// Hrvatske vode na novom broju postaje daju vodostaj bez protoka: uvoz ga
// izračuna našom krivuljom i upiše uz očitanje, s načinom i bilješkom.
func TestPreuzmiProtokIzNaseKrivulje(t *testing.T) {
	sat := time.Now().UTC().Truncate(time.Hour)
	sp := &probnoSpremiste{postojeca: map[int64]bool{}}
	u := NoviUvoznik(sp, nil)
	u.Izvori = []Izvor{probniIzvor{naziv: Podrijetlo, redci: []Redak{
		{Kad: sat.Add(-2 * time.Hour), LevelCm: intPtr(100)},
		{Kad: sat.Add(-time.Hour), LevelCm: intPtr(101)},
	}}}
	var trazeno []string
	u.Krivulje = func(_ context.Context, sifra string) ([]models.HQKrivulja, error) {
		trazeno = append(trazeno, sifra)
		return probneKrivulje(), nil
	}
	st := &models.Station{ID: uuid.New(), Name: "Botovo", Code: "botovo",
		JavniURL: "https://mvodostaji.voda.hr/Home/PregledVodostajaPostaje?sektorID=2&bpID=0&postajaID=1740", JavniUvoz: true}

	s := u.Preuzmi(context.Background(), st)
	if s.Greska != "" || s.Novih != 2 {
		t.Fatalf("preuzimanje: %+v", s)
	}
	if len(trazeno) != 1 || trazeno[0] != "botovo" {
		t.Errorf("krivulje tražene za %v", trazeno)
	}
	for _, o := range sp.upisano {
		if o.FlowM3s == nil || o.FlowMethod != models.FlowMethodKrivulja || o.FlowNote != BiljeskaNaseKrivulje {
			t.Errorf("%s: protok %v, način %q, bilješka %q", o.MeasuredAt, o.FlowM3s, o.FlowMethod, o.FlowNote)
		}
	}
}

// Kad izvor da protok za sve, krivulje se i ne traže; kad ih se ne može
// pročitati, vodostaj se upiše bez protoka, a greška se javi jednom.
func TestProtokIzKrivuljeBezPotrebeIGreska(t *testing.T) {
	st := &models.Station{Code: "botovo"}
	u := NoviUvoznik(&probnoSpremiste{}, nil)
	trazeno := 0
	u.Krivulje = func(context.Context, string) ([]models.HQKrivulja, error) {
		trazeno++
		return nil, errors.New("no such table: hq_krivulje")
	}
	puni := []Redak{{Kad: time.Now(), LevelCm: intPtr(10), FlowM3s: floatPtr(1)}}
	u.protokIzKrivulje(context.Background(), st, puni, nil)
	if trazeno != 0 {
		t.Errorf("krivulje tražene iako izvor ima protok")
	}

	greskaKrivuljaJavljena.Store(false)
	var dnevnik []string
	u.Zapisnik = func(f string, a ...any) { dnevnik = append(dnevnik, f) }
	for range 3 {
		prazni := []Redak{{Kad: time.Now(), LevelCm: intPtr(10)}}
		u.protokIzKrivulje(context.Background(), st, prazni)
		if prazni[0].FlowM3s != nil {
			t.Fatal("bez krivulja protok mora ostati prazan")
		}
	}
	if trazeno != 3 || len(dnevnik) != 1 || !strings.Contains(dnevnik[0], "krivulje se ne mogu pročitati") {
		t.Errorf("traženo %d, dnevnik %v", trazeno, dnevnik)
	}

	u.Krivulje = nil
	prazni := []Redak{{Kad: time.Now(), LevelCm: intPtr(10)}}
	u.protokIzKrivulje(context.Background(), st, prazni)
	if prazni[0].FlowM3s != nil {
		t.Error("bez izvora krivulja protok mora ostati prazan")
	}
}
