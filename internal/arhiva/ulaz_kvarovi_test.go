package arhiva

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Zaključava da upis niza odbija ono od čega ne može složiti ime i mjesto
// datoteke, prije nego išta napiše.
func TestUpisNizaOdbijaNeupotrebljivo(t *testing.T) {
	r := []Redak{{Vrijeme: time.Date(2024, 3, 1, 7, 0, 0, 0, time.UTC), Vrijednost: 120}}
	koren := t.TempDir()
	for _, s := range []struct {
		ime, koren, sliv, vrsta string
		redci                   []Redak
		greska                  string
	}{
		{"bez stabla", "", "dunav", "jutarnji", r, "nije zadano gdje stablo s podacima stoji"},
		{"nepoznata vrsta", koren, "dunav", "tjedni", r, `vrsta "tjedni" nije jedna od`},
		{"bez sliva", koren, " ", "jutarnji", r, `sliv " " nije upotrebljiv kao naziv mape`},
		{"razmak u slivu", koren, "sava donja", "jutarnji", r, `sliv "sava donja" nije upotrebljiv`},
		{"bez redaka", koren, "dunav", "jutarnji", nil, "nema nijednog retka za upis"},
	} {
		_, err := Upisi(s.koren, s.sliv, "vodograd", "cop", "vodostaj", s.vrsta, s.redci)
		greskaSadrzi(t, s.ime, err, s.greska)
	}
	if ima, _ := os.ReadDir(koren); len(ima) != 0 {
		t.Errorf("odbijeni upisi ostavili su trag u stablu: %v", ima)
	}

	// Stablo koje je datoteka ne može primiti mapu letve.
	datoteka := filepath.Join(t.TempDir(), "stablo")
	zapisiDatoteku(t, datoteka, "x")
	if _, err := Upisi(datoteka, "dunav", "vodograd", "cop", "vodostaj", "jutarnji", r); err == nil {
		t.Error("upis u stablo koje je datoteka prošao je")
	}

	// Na mjestu datoteke niza stoji mapa: upis javlja grešku i mapu ne dira.
	mapa := filepath.Join(koren, "dunav", "vodograd",
		KanonskoIme("vodograd", "cop", "vodostaj", "jutarnji", r[0].Vrijeme, r[0].Vrijeme))
	zapisiDatoteku(t, filepath.Join(mapa, "unutra.txt"), "x")
	if _, err := Upisi(koren, "dunav", "vodograd", "cop", "vodostaj", "jutarnji", r); err == nil {
		t.Error("upis preko mape prošao je")
	}
	if _, err := os.Stat(filepath.Join(mapa, "unutra.txt")); err != nil {
		t.Errorf("mapa na mjestu datoteke je dirana: %v", err)
	}
}

// Zaključava provjeru zapisanog: datoteka mora redak po redak odgovarati
// onome što je upisano, prazni retci se ne broje, a sve drugo je greška koja
// kaže što se razlikuje.
func TestProvjeraZapisanogNiza(t *testing.T) {
	redci := []Redak{
		{Vrijeme: time.Date(2024, 3, 1, 7, 0, 0, 0, time.UTC), Vrijednost: 120},
		{Vrijeme: time.Date(2024, 3, 1, 8, 0, 0, 0, time.UTC), Vrijednost: 121},
	}
	const glava = "vrijeme_utc;vodostaj_cm\n"
	const prvi, drugi = "2024-03-01 07:00:00;120\n", "2024-03-01 08:00:00;121\n"
	for _, s := range []struct {
		ime, sadrzaj, greska string
	}{
		{"prazna datoteka", "", "provjera: datoteka je prazna"},
		{"redak viška", glava + prvi + drugi + "2024-03-01 09:00:00;122\n", "provjera: zapisano je više redaka nego što ih je bilo"},
		{"drukčija vrijednost", glava + "2024-03-01 07:00:00;125\n" + drugi,
			`provjera: redak 1 je "2024-03-01 07:00:00;125", a upisano je bilo "2024-03-01 07:00:00;120"`},
		{"redak manjka", glava + prvi, "provjera: zapisano 2 redaka, pročitano 1"},
		{"predug redak", glava + strings.Repeat("9", 1<<20+1) + "\n", bufio.ErrTooLong.Error()},
	} {
		put := filepath.Join(t.TempDir(), "niz.csv")
		zapisiDatoteku(t, put, s.sadrzaj)
		greskaSadrzi(t, s.ime, provjeriZapisano(put, time.UTC, redci), s.greska)
	}
	put := filepath.Join(t.TempDir(), "niz.csv")
	zapisiDatoteku(t, put, glava+prvi+"\n  \n"+drugi+"\n")
	if err := provjeriZapisano(put, time.UTC, redci); err != nil {
		t.Errorf("prazni retci su se brojali: %v", err)
	}
	if err := provjeriZapisano(filepath.Join(t.TempDir(), "nema.csv"), time.UTC, redci); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("datoteka koje nema: %v", err)
	}
}

// Zaključava čitanje zatečenih redaka pri dopuni: sat se čita u zoni izvora,
// decimalni zarez vrijedi, prazan redak se ne broji, a niza bez datoteke nema.
// Što se događa s retkom koji se ne da pročitati, ovdje se namjerno ne
// zaključava (vidi TestDopunaNeBriseRetkeKojeGradnjaCita).
func TestPostojeciRedciCitajuZatecenNiz(t *testing.T) {
	koren := t.TempDir()
	if r, err := PostojeciRedci(koren, "dunav", "vodograd", "cop", "vodostaj", "satni"); err != nil || r != nil {
		t.Errorf("niz bez datoteke: %v, %v", r, err)
	}
	zapisiDatoteku(t, filepath.Join(koren, "dunav", "vodograd", "vodograd_cop_vodostaj_satni_2024.csv"),
		"vrijeme_utc;vodostaj_cm\n"+
			"2024-03-01 07:00:00;212\n"+
			"\n"+
			"2024-03-03 07:00:00;2,5\n")
	r, err := PostojeciRedci(koren, "dunav", "vodograd", "cop", "vodostaj", "satni")
	if err != nil {
		t.Fatal(err)
	}
	want := []Redak{
		{Vrijeme: time.Date(2024, 3, 1, 6, 0, 0, 0, time.UTC), Vrijednost: 212},
		{Vrijeme: time.Date(2024, 3, 3, 6, 0, 0, 0, time.UTC), Vrijednost: 2.5},
	}
	if len(r) != len(want) {
		t.Fatalf("pročitano %+v", r)
	}
	for i := range want {
		if !r[i].Vrijeme.Equal(want[i].Vrijeme) || r[i].Vrijednost != want[i].Vrijednost || r[i].PoDanu {
			t.Errorf("redak %d: %+v, očekivano %+v", i, r[i], want[i])
		}
	}

	// Ime niza pokazuje na datoteku koje nema: to je greška, ne prazan niz.
	veza := filepath.Join(koren, "dunav", "vodograd", "vodograd_cop_vodostaj_jutarnji_2024.csv")
	if err := os.Symlink(filepath.Join(koren, "nema.csv"), veza); err != nil {
		t.Fatal(err)
	}
	if _, err := PostojeciRedci(koren, "dunav", "vodograd", "cop", "vodostaj", "jutarnji"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("veza na datoteku koje nema: %v", err)
	}
}

// GREŠKA: dopuna niza tiho briše zatečene retke koje gradnja čita bez
// problema: redak s trećim stupcem, polja u navodnicima i vrijeme bez
// sekundi. PostojeciRedci dijeli redak na prvoj točki-zarezu, a gradnja ga
// čita kao CSV, pa Dopuni prepiše datoteku bez onoga što nije razumjela.
// Popravak smije i odbiti dopunu, ali tada datoteka mora ostati kakva je bila.
func TestDopunaNeBriseRetkeKojeGradnjaCita(t *testing.T) {
	t.Skip("GREŠKA: internal/arhiva/ulaz.go:215 — Dopuni briše retke koje gradnja čita (treći stupac, navodnici, vrijeme bez sekundi)")
	koren := t.TempDir()
	put := filepath.Join(koren, "dunav", "vodograd", "vodograd_cop_vodostaj_satni_2024.csv")
	zateceno := "vrijeme_utc;vodostaj_cm;napomena\n" +
		"2024-03-01 07:00:00;120;\n" +
		"2024-03-01 08:00:00;121;led\n" +
		"\"2024-03-01 09:00:00\";\"122\"\n" +
		"2024-03-01 10:00:00;123\n" +
		"2024-03-02 07:00;125\n"
	zapisiDatoteku(t, put, zateceno)
	prije, _, err := citaj(put, zonaIzvora("cop"), "vodostaj")
	if err != nil || len(prije) != 5 {
		t.Fatalf("gradnja čita %v (%v), očekivano pet vrijednosti", prije, err)
	}

	novi := Redak{Vrijeme: time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC), Vrijednost: 124}
	novo, err := Dopuni(koren, "dunav", "vodograd", "cop", "vodostaj", "satni", []Redak{novi})
	if err != nil {
		if b, _ := os.ReadFile(put); string(b) != zateceno {
			t.Errorf("odbijena dopuna promijenila je datoteku:\n%s", b)
		}
		return
	}
	poslije, _, err := citaj(novo, zonaIzvora("cop"), "vodostaj")
	if err != nil {
		t.Fatal(err)
	}
	ima := map[zapis]bool{}
	for _, z := range poslije {
		ima[z] = true
	}
	for _, z := range append(prije, zapis{t: novi.Vrijeme.Unix(), v: novi.Vrijednost}) {
		if !ima[z] {
			t.Errorf("nakon dopune nema vrijednosti %v u %s; gradnja sad čita %v", z, filepath.Base(novo), poslije)
		}
	}
}
