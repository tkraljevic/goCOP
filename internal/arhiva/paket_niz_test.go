package arhiva

import (
	"bytes"
	"database/sql"
	"errors"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// Zaključava izbor mjerila kojim se niz pakira u cijele brojeve: najmanje
// koje sve vrijednosti svodi na cijeli broj bez gubitka. Kad takvoga nema,
// jer vrijednost ima previše decimala, prevelika je ili se ne vraća ista,
// mjerila nema i vrijednosti idu kakve jesu.
func TestMjeriloNizaBezGubitka(t *testing.T) {
	for _, s := range []struct {
		ime     string
		v       []float64
		mjerilo int
	}{
		{"prazan niz", nil, 1},
		{"cijeli centimetri", []float64{776, -102, 0}, 1},
		{"jedna decimala", []float64{12.5, -0.5, 3}, 10},
		{"dvije decimale", []float64{8.66, 9.1}, 100},
		{"tri decimale", []float64{0.125, 1}, 1000},
		{"četiri decimale", []float64{0.1234}, 0},
		{"prevelik broj", []float64{1e17}, 0},
		{"ne vraća se isti", []float64{math.Nextafter(0.3, 1)}, 0},
	} {
		if got := mjeriloZa(s.v); got != s.mjerilo {
			t.Errorf("%s: mjerilo %d, očekivano %d", s.ime, got, s.mjerilo)
		}
	}
}

// Zaključava da zapakiran niz raspakiran daje točno iste brojeve, i s
// mjerilom i bez njega, i da odrezan zapis javlja grešku umjesto da vrati
// manje zapisa.
func TestNizSePakiraIRaspakiravaBezGubitka(t *testing.T) {
	vrijeme := []int64{1371186000, 1371189600, 1371196800, 1371196801}
	for _, s := range []struct {
		ime        string
		vrijednost []float64
	}{
		{"centimetri", []float64{776, 774, -86, -102}},
		{"decimale", []float64{8.66, 9.1, 0.01, -3.25}},
		{"bez mjerila", []float64{math.Nextafter(0.3, 1), 1e300, -1e-9, 0}},
	} {
		m := mjeriloZa(s.vrijednost)
		var b bytes.Buffer
		if err := zapisiNiz(&b, vrijeme, s.vrijednost, m); err != nil {
			t.Fatalf("%s: %v", s.ime, err)
		}
		t2, v2, err := citajNiz(&citac{b: b.Bytes()}, m)
		if err != nil {
			t.Fatalf("%s: čitanje: %v", s.ime, err)
		}
		if !slices.Equal(t2, vrijeme) || !slices.Equal(v2, s.vrijednost) {
			t.Errorf("%s: vraćeno %v %v, upisano %v %v", s.ime, t2, v2, vrijeme, s.vrijednost)
		}
		odrezano := b.Bytes()[:b.Len()-1]
		if _, _, err := citajNiz(&citac{b: odrezano}, m); err == nil {
			t.Errorf("%s: odrezan zapis pročitan bez greške", s.ime)
		}
	}
}

// pokvarenPisac prima zadani broj upisa, a svaki sljedeći odbija.
type pokvarenPisac struct{ prolazi int }

var errPisanja = errors.New("disk je pun")

func (p *pokvarenPisac) Write(b []byte) (int, error) {
	if p.prolazi == 0 {
		return 0, errPisanja
	}
	p.prolazi--
	return len(b), nil
}

// Zaključava da pakiranje niza javlja grešku pisanja na kojem god mjestu
// pukne: na broju zapisa, na vremenu ili na vrijednosti, s mjerilom i bez.
func TestPakiranjeNizaJavljaGreskuPisanja(t *testing.T) {
	vrijeme := []int64{100, 200}
	for _, s := range []struct {
		ime     string
		mjerilo int
		prolazi int
	}{
		{"broj zapisa", 1, 0},
		{"prvo vrijeme", 1, 1},
		{"prva vrijednost s mjerilom", 1, 2},
		{"prva vrijednost bez mjerila", 0, 2},
		{"drugo vrijeme bez mjerila", 0, 3},
	} {
		err := zapisiNiz(&pokvarenPisac{prolazi: s.prolazi}, vrijeme, []float64{1, 2}, s.mjerilo)
		if !errors.Is(err, errPisanja) {
			t.Errorf("%s: greška %v, očekivana greška pisanja", s.ime, err)
		}
	}
}

// Zaključava skraćeni otisak u porukama: dugi otisak skraćuje se na dvanaest
// znakova s trotočjem, a kratki ostaje kakav jest.
func TestKratkiOtisak(t *testing.T) {
	for _, s := range []struct{ otisak, want string }{
		{"6e0ed8df1a2b3c4d5e6f", "6e0ed8df1a2b…"},
		{"6e0ed8df1a2b", "6e0ed8df1a2b"},
		{"abc", "abc"},
		{"", ""},
	} {
		if got := kratki(s.otisak); got != s.want {
			t.Errorf("%q → %q, očekivano %q", s.otisak, got, s.want)
		}
	}
}

// Zaključava postavke izvora koje paket nosi: samo izvori koje nizovi letve
// koriste, svaki jednom i poredani po nazivu; izvor kojeg čvor ne poznaje
// opisuje se zadanim vrijednostima i napomenom, a preračun ne ulazi.
func TestPaketNosiSamoIzvoreSvojihNizova(t *testing.T) {
	db := praznaArhiva(t)
	nizovi := []nizUPaketu{
		{Izvor: "letva-hv"}, {Izvor: "cop"}, {Izvor: "cop"},
		{Izvor: "preracun-batina"}, {Izvor: "izvor-sa-strane"},
	}
	izvori, err := izvoriZaNizove(db, nizovi)
	if err != nil {
		t.Fatal(err)
	}
	var imena []string
	for _, i := range izvori {
		imena = append(imena, i.Naziv)
	}
	if want := []string{"cop", "izvor-sa-strane", "letva-hv"}; !slices.Equal(imena, want) {
		t.Fatalf("izvori u paketu %q, očekivano %q", imena, want)
	}
	if cop := izvori[0]; !cop.Ukljucen || cop.Tocnost != 3 {
		t.Errorf("postavke izvora cop nisu prenesene: %+v", cop)
	}
	strani := izvori[1]
	if strani.Red != 900 || strani.Ukljucen || strani.Tocnost != zadanaTocnost("izvor-sa-strane") ||
		!strings.Contains(strani.Napomena, "nije bio na popisu") {
		t.Errorf("nepoznat izvor: %+v", strani)
	}
	if got := (&Sadrzaj{Izvori: izvori}).PostavkeIzvora(); !slices.Equal(got, izvori) {
		t.Errorf("paket vraća druge postavke: %+v", got)
	}

	if prazni, err := izvoriZaNizove(db, nil); err != nil || len(prazni) != 0 {
		t.Errorf("letva bez nizova: %+v, %v", prazni, err)
	}
}

// Zaključava da paket ne nastaje kad se popis izvora ne da pročitati: bez
// njega primatelj ne bi znao s čime je spojeni niz složen.
func TestPaketBezPopisaIzvoraNeNastaje(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "bez-izvora.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := izvoriZaNizove(db, []nizUPaketu{{Izvor: "cop"}}); err == nil {
		t.Error("postavke izvora složene su bez tablice izvora")
	}
	if _, err := ucitajPromjene(db, "batina"); err == nil {
		t.Error("promjene kote pročitane su bez tablice")
	}
}

// Zaključava redoslijed promjena kote nule u paketu: po datumu, bez obzira na
// redoslijed upisa, i samo za traženu letvu.
func TestPromjeneKoteUPaketuIduPoDatumu(t *testing.T) {
	db := praznaArhiva(t)
	for _, p := range []struct {
		letva, datum string
		pomak        int
	}{
		{"batina", "1943-01-01", 200},
		{"batina", "1909-05-01", -15},
		{"vukovar", "1950-01-01", 30},
	} {
		if _, err := db.Exec(`INSERT INTO promjene_kote (letva, datum, pomak_cm) VALUES (?,?,?)`,
			p.letva, p.datum, p.pomak); err != nil {
			t.Fatal(err)
		}
	}
	pr, err := ucitajPromjene(db, "batina")
	if err != nil {
		t.Fatal(err)
	}
	if len(pr) != 2 || pr[0].Datum != "1909-05-01" || pr[1].Datum != "1943-01-01" || pr[1].PomakCm != 200 {
		t.Errorf("promjene batine: %+v", pr)
	}
}

// Zaključava da prazna arhiva ne nastaje nad zatvorenom bazom: greška se
// javlja, ne guta.
func TestPraznaArhivaNadZatvorenomBazom(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := PripremiPraznu(db); err == nil {
		t.Error("shema je \"pripremljena\" nad zatvorenom bazom")
	}
}
