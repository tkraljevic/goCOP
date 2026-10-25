package skok

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// sat je trenutak po zagrebačkom zidnom satu 24. 10. 2026.
func sat(h int) time.Time {
	return time.Date(2026, 10, 24, h, 0, 0, 0, time.FixedZone("CEST", 2*3600))
}

func tockeOd(h0 int, cm ...float64) []Tocka {
	out := make([]Tocka, len(cm))
	for i, v := range cm {
		out[i] = Tocka{Kad: sat(h0 + i), Cm: v}
	}
	return out
}

// Donji Brod 24. 10. 2026. (vodostaji.voda.hr): u 21 h 830 cm, a sat
// prije i poslije −123 i −121 cm. Prag letve iz arhive je 90 cm.
func TestSkokDonjegBrodaSeOdbacuje(t *testing.T) {
	got := Procijeni(tockeOd(19, -124, -123, 830, -121, -121), 90)
	if want := []Odluka{Prihvaceno, Prihvaceno, Odbaceno, Prihvaceno, Prihvaceno}; !slices.Equal(got, want) {
		t.Errorf("odluke %v, želim %v", got, want)
	}
}

// Skok koji iduće očitanje potvrdi prihvaća se, s idućim (ispuštanje
// elektrane, pravi val); i skok koji ide dalje ili stane između. Zadnje
// koje opet odskoči čeka svoje iduće.
func TestSkokKojiIducePotvrdiSePrihvaca(t *testing.T) {
	for _, niz := range [][]float64{{100, 100, 300, 305}, {100, 100, 300, 500}, {100, 100, 300, 210}} {
		got := Procijeni(tockeOd(10, niz...), 100)
		if !slices.Equal(got[:3], []Odluka{Prihvaceno, Prihvaceno, Prihvaceno}) || got[3] == Odbaceno {
			t.Errorf("%v: %v", niz, got)
		}
	}
}

// Zadnja vrijednost koja odskoči čeka iduće očitanje; stalna (već
// upisana, ručna) se nikad ne odbacuje, ali je razina od koje se mjeri.
func TestCekanjePrvaIStalna(t *testing.T) {
	if got := Procijeni(tockeOd(10, 100, 101, 400), 100); got[2] != Ceka {
		t.Errorf("zadnja: %v", got)
	}
	tocke := tockeOd(10, 100, 830, 101, 102)
	tocke[1].Stalna = true
	if got := Procijeni(tocke, 100); slices.Contains(got, Odbaceno) || slices.Contains(got, Ceka) {
		t.Errorf("stalna: %v", got)
	}
	if got := Procijeni(nil, 100); len(got) != 0 {
		t.Errorf("prazno: %v", got)
	}
}

// Prag je po satu razmaka: tri sata rupe dopuštaju trostruku promjenu, a
// razmak kraći od sata cijeli prag.
func TestPragPoSatuRazmaka(t *testing.T) {
	a := Tocka{Kad: sat(10), Cm: 100}
	if Prevelik(a, Tocka{Kad: sat(13), Cm: 340}, 100) {
		t.Error("240 cm u tri sata uz prag 100 nije skok")
	}
	if !Prevelik(a, Tocka{Kad: sat(13), Cm: 410}, 100) {
		t.Error("310 cm u tri sata uz prag 100 jest skok")
	}
	if Prevelik(a, Tocka{Kad: sat(10).Add(10 * time.Minute), Cm: 190}, 100) {
		t.Error("90 cm u deset minuta uz prag 100 nije skok")
	}
	// iduće se vratilo nakon rupe: razmak do stare razine je dva sata
	got := Procijeni([]Tocka{a, {Kad: sat(11), Cm: 400}, {Kad: sat(13), Cm: 150}}, 100)
	if got[1] != Odbaceno {
		t.Errorf("povratak nakon rupe: %v", got)
	}
}

// Prva točka niza i prva nakon rupe mjere se prema iduća dva očitanja.
// Donji Brod s −123 cm prije 30 sati, pa izvor vrati 830, −121, −120:
// prag za 30 sati rupe dopušta i 950 cm, ali 830 strši od oba iduća, pa se
// odbacuje. Prava nova razina nakon rupe prolazi jer ju iduće nastavi, a
// i kad se iduća dva ne slažu međusobno.
func TestSkokNakonRupeMjeriSePremaIducima(t *testing.T) {
	rupa := func(cm ...float64) []Tocka {
		return append([]Tocka{{Kad: sat(21).Add(-30 * time.Hour), Cm: -123}}, tockeOd(21, cm...)...)
	}
	for _, x := range []struct {
		ime   string
		tocke []Tocka
		want  []Odluka
	}{
		{"830 nakon rupe", rupa(830, -121, -120), []Odluka{Prihvaceno, Odbaceno, Prihvaceno, Prihvaceno}},
		{"prvo u nizu", tockeOd(10, 900, 100, 101), []Odluka{Odbaceno, Prihvaceno, Prihvaceno}},
		{"prava razina nakon rupe", rupa(300, 305, 310), []Odluka{Prihvaceno, Prihvaceno, Prihvaceno, Prihvaceno}},
		{"iduće strši, ne prvo", rupa(300, 900, 302), []Odluka{Prihvaceno, Prihvaceno, Odbaceno, Prihvaceno}},
		{"iduća dva se ne slažu", rupa(830, -121, 500), []Odluka{Prihvaceno, Prihvaceno, Prihvaceno, Ceka}},
		{"blizu razine prije rupe", rupa(-100, 830, -101), []Odluka{Prihvaceno, Prihvaceno, Odbaceno, Prihvaceno}},
		{"čeka iduće", rupa(830), []Odluka{Prihvaceno, CekaIduca}},
		{"čeka i drugo iduće", rupa(830, -121), []Odluka{Prihvaceno, Ceka, Prihvaceno}},
		{"jedino očitanje", tockeOd(10, 830), []Odluka{Prihvaceno}},
		{"prvo, pa drugo strši", tockeOd(10, 830, -121), []Odluka{Ceka, Prihvaceno}},
	} {
		if got := Procijeni(x.tocke, 90); !slices.Equal(got, x.want) {
			t.Errorf("%s: %v, želim %v", x.ime, got, x.want)
		}
	}
	z := Tocka{Kad: sat(10), Cm: 100}
	if PoRupi(&z, Tocka{Kad: sat(11), Cm: 300}, 90) || PoRupi(&z, Tocka{Kad: sat(14), Cm: 150}, 90) ||
		!PoRupi(&z, Tocka{Kad: sat(14), Cm: 300}, 90) || !PoRupi(nil, z, 90) {
		t.Error("PoRupi")
	}
}

func promjene(n int, f func(i int) float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = f(i)
	}
	return out
}

// godina je jedna godina satnih promjena.
func godina(n int, f func(i int) float64) map[int][]float64 {
	return map[int][]float64{2020: promjene(n, f)}
}

// Prag iz satnih promjena: 99,9. percentil puta tri, na 10 cm naviše,
// najmanje 50; premalo promjena daje zadani prag.
func TestPragIzGodina(t *testing.T) {
	if p := PragIzGodina(godina(NajmanjePromjena-1, func(int) float64 { return 500 }), false); p != ZadaniPrag {
		t.Errorf("premalo promjena: %v", p)
	}
	// mirna letva: promjene do 5 cm
	if p := PragIzGodina(godina(10000, func(i int) float64 { return float64(i%11 - 5) }), false); p != NajmanjiPrag {
		t.Errorf("mirna letva: %v", p)
	}
	// letva ispod elektrane: 0,2 % sati ±150 cm, ostalo ±20; po jedan lažni
	// skok od 1500 cm ne mijenja percentil
	p := PragIzGodina(godina(10000, func(i int) float64 {
		switch {
		case i == 0:
			return 1500
		case i%500 == 1:
			return -150
		}
		return 20
	}), false)
	if p != 450 {
		t.Errorf("ispod elektrane: %v, želim 450", p)
	}
	// 99,9. percentil 31 cm → 93 → 100
	if p := PragIzGodina(godina(10000, func(i int) float64 {
		if i%900 == 0 {
			return 31
		}
		return 3
	}), false); p != 100 {
		t.Errorf("zaokruživanje: %v", p)
	}
}

// Godina u kojoj je mjerilo bilo u kvaru ne diže prag: vrijedi medijan
// godišnjih percentila (Donji Brod 2026.). Ustava zadržava percentil
// cijele arhive, a godina s premalo promjena ne daje svoj percentil; kad
// nijedna nema dovoljno, vrijedi percentil svih.
func TestPragMedijanGodina(t *testing.T) {
	mirna := promjene(8760, func(i int) float64 { return float64(i % 8) })     // percentil 7 → 50
	kvar := promjene(8760, func(i int) float64 { return float64(i%25) * 1.2 }) // percentil 28,8 → 90
	poGod := map[int][]float64{2023: mirna, 2024: mirna, 2025: mirna, 2026: kvar}
	if p := PragIzGodina(poGod, false); p != NajmanjiPrag {
		t.Errorf("medijan godina: %v, želim 50", p)
	}
	if p := PragIzGodina(poGod, true); p != 90 {
		t.Errorf("ustava: %v, želim 90", p)
	}
	// dvije godine: prosjek dvaju percentila (7 i 28,8 → 17,9 → 54 → 60)
	if p := PragIzGodina(map[int][]float64{2025: mirna, 2026: kvar}, false); p != 60 {
		t.Errorf("dvije godine: %v, želim 60", p)
	}
	// po 1500 promjena u dvije godine: nijedna godina nema dovoljno, sve zajedno da
	malo := promjene(1500, func(i int) float64 { return float64(i % 30) })
	if p := PragIzGodina(map[int][]float64{2025: malo, 2026: malo}, false); p != 90 {
		t.Errorf("premalo po godini: %v, želim 90", p)
	}
	if !jeUstava("ustava-kopacevo-nizvodno") || jeUstava("donji-brod") {
		t.Error("jeUstava")
	}
}

// arhivaSaSpojem je arhiva s tablicom spoj i satnim nizom letve.
func arhivaSaSpojem(t *testing.T, letva string, cm func(h int) float64, sati int) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "arhiva.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE spoj (letva TEXT, velicina TEXT, korak TEXT, vrijeme INTEGER, vrijednost REAL)`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	pocetak := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	for h := range sati {
		if _, err := tx.Exec(`INSERT INTO spoj VALUES (?, 'vodostaj', 'satni', ?, ?)`, letva, pocetak+int64(h)*3600, cm(h)); err != nil {
			t.Fatal(err)
		}
	}
	// stariji od OdGodine i između punih sati ne ulazi
	if _, err := tx.Exec(`INSERT INTO spoj VALUES (?, 'vodostaj', 'satni', ?, 5000), (?, 'vodostaj', 'satni', ?, 9000)`,
		letva, time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC).Unix(), letva, pocetak+1800); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return db
}

// Pragovi se računaju iz arhive pri prvom traženju i pamte; letva bez
// arhive i nil Pragovi daju zadani prag.
func TestPragoviIzArhive(t *testing.T) {
	arhiva := arhivaSaSpojem(t, "botovo", func(h int) float64 {
		if h%100 == 50 {
			return 230 // svakih sto sati sat 29 cm iznad
		}
		return 200 + float64(h%2)
	}, 5000)
	p := NoviPragovi(arhiva)
	if v := p.Za("botovo"); v != 90 {
		t.Errorf("botovo: %v, želim 90", v)
	}
	if v := p.Za("nepoznata"); v != ZadaniPrag {
		t.Errorf("letva bez arhive: %v", v)
	}
	// zapamćeno: i kad arhive više nema
	if _, err := arhiva.Exec(`DROP TABLE spoj`); err != nil {
		t.Fatal(err)
	}
	if v := p.Za("botovo"); v != 90 {
		t.Errorf("zapamćen prag: %v", v)
	}
	// arhiva koja se ne da pročitati: zadani prag, bez pamćenja
	if v := p.Za("osijek"); v != ZadaniPrag {
		t.Errorf("bez tablice: %v", v)
	}
	if _, ima := p.po["osijek"]; ima {
		t.Error("prag iz greške je zapamćen")
	}
	// ustava: percentil cijele arhive, ne medijan godina
	for _, x := range []struct {
		letva string
		prag  float64
	}{{"ustava-probna", 90}, {"letva-probna", NajmanjiPrag}} {
		arhiva := arhivaSaSpojem(t, x.letva, func(h int) float64 {
			if h >= 8784 && h < 2*8784 && h%100 == 50 {
				return 230 // samo 2021.: svakih sto sati sat 29 cm iznad
			}
			return 200 + float64(h%2)
		}, 3*8784)
		if v := NoviPragovi(arhiva).Za(x.letva); v != x.prag {
			t.Errorf("%s: %v, želim %v", x.letva, v, x.prag)
		}
	}
	var nema *Pragovi
	if nema.Za("botovo") != ZadaniPrag || NoviPragovi(nil).Za("botovo") != ZadaniPrag {
		t.Error("bez arhive mora vrijediti zadani prag")
	}
}

// Satne promjene čitaju samo pune sate od OdGodine, i javljaju grešku
// čitanja retka.
func TestSatnePromjene(t *testing.T) {
	arhiva := arhivaSaSpojem(t, "osijek", func(h int) float64 { return float64(h) }, 4)
	p, err := SatnePromjene(context.Background(), arhiva, "osijek")
	if err != nil || len(p) != 1 || !slices.Equal(p[2020], []float64{1, 1, 1}) {
		t.Errorf("promjene %v, %v", p, err)
	}
	if _, err := arhiva.Exec(`INSERT INTO spoj VALUES ('pokvarena', 'vodostaj', 'satni', ?, 'nije broj')`,
		time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := SatnePromjene(context.Background(), arhiva, "pokvarena"); err == nil {
		t.Error("vrijednost koja nije broj mora biti greška")
	}
}
