package prognoza

import (
	"path/filepath"
	"testing"
	"time"
)

// Promašaji u ovim provjerama su izmišljeni.

func mjereLetvi(po map[string]float64, velicina string) MjereProvjere {
	m := MjereProvjere{}
	for letva, cm := range po {
		d := map[int]MjeraProvjere{}
		for _, doseg := range DoseziTjedne {
			d[doseg] = MjeraProvjere{Promasaj: cm, Slucaja: 150}
		}
		m[Izvor{Letva: letva, Velicina: velicina}] = d
	}
	return m
}

func TestUsporediLance(t *testing.T) {
	stara := mjereLetvi(map[string]float64{"botovo": 10, "terezino-polje": 8, "osijek": 12, "dalj": 9, "samo-stari": 5}, "vodostaj")
	nova := mjereLetvi(map[string]float64{"botovo": 13, "terezino-polje": 10, "osijek": 5, "dalj": 11}, "vodostaj")
	u := UsporediLance(stara, nova)
	if !u.NijeLosiji || u.Prije != 9.75 || u.Poslije != 9.75 {
		t.Errorf("ukupno: %+v", u)
	}
	if len(u.Losije) != 3 || u.Losije[0].Letva != "botovo" || u.Losije[0].Razlika != 3 || u.Losije[1].Letva != "dalj" || u.Losije[2].Letva != "terezino-polje" {
		t.Errorf("lošije: %+v", u.Losije)
	}
	// ukupno lošije za više od 2 %
	if u := UsporediLance(stara, mjereLetvi(map[string]float64{"botovo": 11, "terezino-polje": 8, "osijek": 12}, "vodostaj")); u.NijeLosiji {
		t.Errorf("lošiji za 3,3 %% prošao: %+v", u)
	}
	// protok se gleda zasebno, u svojoj jedinici
	sp := mjereLetvi(map[string]float64{"dubrava": 50}, "protok")
	np := mjereLetvi(map[string]float64{"dubrava": 60}, "protok")
	for iz, m := range stara {
		sp[iz], np[iz] = m, m
	}
	if u := UsporediLance(sp, np); u.NijeLosiji || u.Prije != 8.8 {
		t.Errorf("lošiji protok prošao: %+v", u)
	}
	if u := UsporediLance(MjereProvjere{}, MjereProvjere{}); u.NijeLosiji {
		t.Error("bez mjera ne smije proći")
	}
}

func TestLanacSlabi(t *testing.T) {
	if !LanacSlabi(14.6, 11.9) || LanacSlabi(14.2, 11.9) || LanacSlabi(20, 0) {
		t.Error("pravilo slabljenja")
	}
}

func TestTerminTjedne(t *testing.T) {
	z := time.FixedZone("CET", 3600)
	for sada, zelim := range map[time.Time]time.Time{
		time.Date(2026, 11, 28, 2, 0, 0, 0, z):  time.Date(2026, 11, 28, 2, 0, 0, 0, z),
		time.Date(2026, 11, 28, 1, 59, 0, 0, z): time.Date(2026, 11, 21, 2, 0, 0, 0, z),
		time.Date(2026, 12, 3, 12, 0, 0, 0, z):  time.Date(2026, 11, 28, 2, 0, 0, 0, z),
	} {
		if got := TerminTjedne(sada, z); !got.Equal(zelim) {
			t.Errorf("%s: %s", sada, got)
		}
	}
	if s := SljedecaTjedna(time.Date(2026, 11, 25, 12, 0, 0, 0, z), z); !s.Equal(time.Date(2026, 11, 28, 2, 0, 0, 0, z)) {
		t.Errorf("sljedeća %s", s)
	}
}

func TestUpozorenjeTjedne(t *testing.T) {
	p := func(ishodi ...string) []TjednaPriprema {
		var out []TjednaPriprema
		for _, i := range ishodi {
			out = append(out, TjednaPriprema{Ishod: i})
		}
		return out
	}
	n, d, s := TjednaNijePrimijenjena, TjednaPrimijenjena, TjednaPreskocena
	if u := UpozorenjeTjedne(p(n, s, n, n, d)); u != UpozorenjeZaredom {
		t.Errorf("tri zaredom uz preskočenu: %q", u)
	}
	if u := UpozorenjeTjedne(p(n, n, d, n)); u != "" {
		t.Errorf("dva zaredom: %q", u)
	}
	sl := p(d, n, n, n)
	sl[0].Slabi = true
	if u := UpozorenjeTjedne(sl); u != UpozorenjeSlabi {
		t.Errorf("slabi: %q", u)
	}
}

// Zapis tjedne pripreme ostaje u bazi prognoza, najnoviji prvi.
func TestTjedneZapis(t *testing.T) {
	db, err := Otvori(filepath.Join(t.TempDir(), "prognoze.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	t1 := time.Date(2026, 11, 21, 1, 0, 0, 0, time.UTC)
	for i, p := range []TjednaPriprema{
		{Kad: t1.AddDate(0, 0, -7), Ishod: TjednaPreskocena, Razlog: "traje obrana od poplava"},
		{Kad: t1, Ishod: TjednaPrimijenjena, Prije: 12.4, Poslije: 11.9, LanacOd: t1},
		{Kad: t1.AddDate(0, 0, 7), Ishod: TjednaNijePrimijenjena, Prije: 12.4, Poslije: 12.9,
			Losije: []LosijaLetva{{Letva: "botovo", Velicina: "vodostaj", Razlika: 3}}, LanacOd: t1, Slabi: true, Osnovica: 10},
	} {
		if err := UpisiTjednu(db, p); err != nil {
			t.Fatal(i, err)
		}
	}
	p, err := TjednePripreme(db, 10)
	if err != nil || len(p) != 3 {
		t.Fatalf("%d %v", len(p), err)
	}
	if !p[2].LanacOd.IsZero() {
		t.Errorf("preskočena ima lanac od %s", p[2].LanacOd)
	}
	if z := p[0]; z.Ishod != TjednaNijePrimijenjena || !z.Slabi || z.Osnovica != 10 || len(z.Losije) != 1 || !z.LanacOd.Equal(t1) {
		t.Errorf("zadnja %+v", z)
	}
	if _, ima, err := ProcitajOsnovicu(db); ima || err != nil {
		t.Errorf("osnovica prije upisa: %v %v", ima, err)
	}
	if err := UpisiOsnovicu(db, Osnovica{Otisak: "x", Promasaj: 11.9}); err != nil {
		t.Fatal(err)
	}
	if o, ima, err := ProcitajOsnovicu(db); !ima || err != nil || o.Promasaj != 11.9 {
		t.Errorf("osnovica %+v %v %v", o, ima, err)
	}
	if !NamjestenOd(db).IsZero() {
		t.Error("prazna baza ima lanac")
	}
}

// Pokvaren zapis ili zatvorena baza javljaju se greškom; nečitljiv dan
// namještanja znači da se ne zna.
func TestTjedneKvarovi(t *testing.T) {
	db, err := Otvori(filepath.Join(t.TempDir(), "prognoze.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tjedna_priprema (kad, ishod, prije) VALUES (1, 'primijenjena', 'nije-broj')`); err != nil {
		t.Fatal(err)
	}
	if _, err := TjednePripreme(db, 5); err == nil {
		t.Error("pokvaren zapis bez greške")
	}
	if err := Spremi(db, []Pojas{{Letva: "a", Velicina: "vodostaj", Od: -1, Do: 1}}, "proba"); err != nil {
		t.Fatal(err)
	}
	if !NamjestenOd(db).IsZero() {
		t.Error("nečitljiv dan namještanja")
	}
	kad := time.Date(2026, 11, 15, 1, 0, 0, 0, time.UTC)
	if err := Spremi(db, []Pojas{{Letva: "a", Velicina: "vodostaj", Od: -1, Do: 1}}, kad.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if !NamjestenOd(db).Equal(kad) {
		t.Errorf("namješteno %s", NamjestenOd(db))
	}
	_ = db.Close() // baza se namjerno zatvara, da se vidi greška
	if _, err := TjednePripreme(db, 5); err == nil {
		t.Error("zatvorena baza bez greške")
	}
	if _, _, err := ProcitajOsnovicu(db); err == nil {
		t.Error("osnovica iz zatvorene baze bez greške")
	}
	if !NamjestenOd(db).IsZero() {
		t.Error("zatvorena baza ima lanac")
	}
}
