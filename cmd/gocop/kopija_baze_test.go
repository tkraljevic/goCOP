package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// bazaZaKopiju je izmišljena baza s WAL-om u novoj mapi
func bazaZaKopiju(t *testing.T) string {
	t.Helper()
	baza := filepath.Join(t.TempDir(), "gocop.db")
	for put, sadrzaj := range map[string]string{baza: "baza", baza + "-wal": "wal"} {
		if err := os.WriteFile(put, []byte(sadrzaj), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return baza
}

func kopijeBaze(t *testing.T, baza string) []string {
	t.Helper()
	k, err := filepath.Glob(filepath.Join(filepath.Dir(baza), mapaKopija, "gocop-*.db"))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// Prvo pokretanje novog izdanja kopira bazu i WAL, s imenom prethodnog
// izdanja; isto izdanje poslije ne kopira ništa.
func TestKopijaPrijeNadogradnje(t *testing.T) {
	baza := bazaZaKopiju(t)
	if err := os.WriteFile(filepath.Join(filepath.Dir(baza), izdanjeBazeDatoteka), []byte("0.0.68-alfa\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sada := time.Date(2026, 11, 26, 7, 0, 0, 0, time.UTC)
	kopija, err := kopijaPrijeNadogradnje(baza, "0.0.69-alfa", sada)
	if err != nil || filepath.Base(kopija) != "gocop-20261126-070000-0.0.68-alfa.db" {
		t.Fatalf("kopija %q, %v", kopija, err)
	}
	for nastavak, ocekivano := range map[string]string{"": "baza", "-wal": "wal"} {
		if b, err := os.ReadFile(kopija + nastavak); err != nil || string(b) != ocekivano {
			t.Errorf("kopija%s: %q %v", nastavak, b, err)
		}
	}
	if b, err := os.ReadFile(filepath.Join(filepath.Dir(baza), izdanjeBazeDatoteka)); err != nil || strings.TrimSpace(string(b)) != "0.0.69-alfa" {
		t.Errorf("zapamćeno izdanje %q, %v", b, err)
	}
	if kopija, err := kopijaPrijeNadogradnje(baza, "0.0.69-alfa", sada.Add(time.Hour)); err != nil || kopija != "" {
		t.Errorf("isto izdanje kopira opet: %q %v", kopija, err)
	}
	if n := len(kopijeBaze(t, baza)); n != 1 {
		t.Errorf("kopija %d", n)
	}
}

// Baza koju je program ovog oblika još nije otvorio (izdanje se ne zna)
// kopira se kao „nepoznato”; nova mapa bez baze samo zapamti izdanje.
func TestKopijaBezZapamcenogIzdanja(t *testing.T) {
	baza := bazaZaKopiju(t)
	kopija, err := kopijaPrijeNadogradnje(baza, "0.0.69-alfa", time.Now())
	if err != nil || !strings.HasSuffix(kopija, "-nepoznato.db") {
		t.Fatalf("kopija %q, %v", kopija, err)
	}

	nova := filepath.Join(t.TempDir(), "gocop.db")
	if kopija, err := kopijaPrijeNadogradnje(nova, "0.0.69-alfa", time.Now()); err != nil || kopija != "" {
		t.Fatalf("nova baza: %q %v", kopija, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(nova), mapaKopija)); err == nil {
		t.Error("za novu bazu nastala je mapa kopija")
	}
}

// Kad je Postava ili instalacija bazu kopirala prije manje od deset
// minuta, druga kopija se ne radi; ostaju tri najnovije kopije.
func TestKopijaSvjezaITriNajnovije(t *testing.T) {
	baza := bazaZaKopiju(t)
	mapa := filepath.Join(filepath.Dir(baza), mapaKopija)
	if err := os.MkdirAll(mapa, 0o700); err != nil {
		t.Fatal(err)
	}
	sada := time.Now()
	for i, ime := range []string{"gocop-20261101-000000-0.0.60-alfa.db", "gocop-20261102-000000-0.0.61-alfa.db", "gocop-20261103-000000-0.0.62-alfa.db"} {
		put := filepath.Join(mapa, ime)
		if err := os.WriteFile(put, []byte("stara"), 0o600); err != nil {
			t.Fatal(err)
		}
		kad := sada.Add(-time.Duration(30-i) * time.Hour)
		if err := os.Chtimes(put, kad, kad); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(mapa, "gocop-20261103-000000-0.0.62-alfa.db-wal"), []byte("w"), 0o600); err != nil {
		t.Fatal(err)
	}
	kopija, err := kopirajBazu(baza, "0.0.68-alfa", sada)
	if err != nil || kopija == "" {
		t.Fatalf("kopija %q, %v", kopija, err)
	}
	k := kopijeBaze(t, baza)
	if len(k) != cuvajKopija || strings.Contains(strings.Join(k, " "), "0.0.60-alfa") {
		t.Errorf("kopije nakon čišćenja: %v", k)
	}
	if kopija, err := kopirajBazu(baza, "0.0.68-alfa", sada.Add(time.Minute)); err != nil || kopija != "" {
		t.Errorf("svježa kopija postoji, a radi se nova: %q %v", kopija, err)
	}
}

// Kopija koja ne uspije zaustavlja pokretanje i ne mijenja zapamćeno
// izdanje; bez nje novo izdanje ne smije promijeniti bazu.
func TestKopijaKojaNeUspije(t *testing.T) {
	baza := bazaZaKopiju(t)
	if err := os.WriteFile(filepath.Join(filepath.Dir(baza), mapaKopija), []byte("nije mapa"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := kopijaPrijeNadogradnje(baza, "0.0.69-alfa", time.Now()); err == nil || !strings.Contains(err.Error(), "nije spremljena") {
		t.Fatalf("greška kopije: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(baza), izdanjeBazeDatoteka)); err == nil {
		t.Error("izdanje je zapamćeno iako kopije nema")
	}
	oznaka := filepath.Join(t.TempDir(), izdanjeBazeDatoteka)
	if err := os.Mkdir(oznaka, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := procitajIzdanjeBaze(oznaka); err == nil {
		t.Error("oznaka izdanja koja se ne da pročitati prošla je")
	}
}

func TestImeIzdanja(t *testing.T) {
	for ulaz, ocekivano := range map[string]string{"0.0.69-alfa": "0.0.69-alfa", "": "nepoznato", "../x y": "..xy", "šđ": "nepoznato"} {
		if got := imeIzdanja(ulaz); got != ocekivano {
			t.Errorf("%q → %q", ulaz, got)
		}
	}
}

// Greške pri kopiranju: nema izvora, nema odredišne mape, put baze ispod
// datoteke; baza bez WAL-a kopira se bez njega.
func TestKopijaRubovi(t *testing.T) {
	mapa := t.TempDir()
	if err := kopirajDatoteku(filepath.Join(mapa, "nema"), filepath.Join(mapa, "u")); err == nil {
		t.Error("kopija nepostojeće datoteke prošla je")
	}
	izvor := filepath.Join(mapa, "izvor")
	if err := os.WriteFile(izvor, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := kopirajDatoteku(izvor, filepath.Join(mapa, "nema-mape", "u")); err == nil {
		t.Error("kopija u nepostojeću mapu prošla je")
	}
	if err := kopirajDatoteku(mapa, filepath.Join(mapa, "u")); err == nil {
		t.Error("kopija mape kao datoteke prošla je")
	}
	if _, err := os.Stat(filepath.Join(mapa, "u.dio")); err == nil {
		t.Error("napola kopirana datoteka ostala je")
	}
	if _, err := kopijaPrijeNadogradnje(filepath.Join(izvor, "gocop.db"), "0.0.69-alfa", time.Now()); err == nil {
		t.Error("baza ispod datoteke prošla je")
	}

	baza := filepath.Join(t.TempDir(), "gocop.db")
	if err := os.WriteFile(baza, []byte("baza"), 0o600); err != nil {
		t.Fatal(err)
	}
	kopija, err := kopirajBazu(baza, "", time.Now())
	if err != nil || !strings.HasSuffix(kopija, "-nepoznato.db") {
		t.Fatalf("kopija bez WAL-a: %q %v", kopija, err)
	}
	if _, err := os.Stat(kopija + "-wal"); err == nil {
		t.Error("nastao je WAL koji baza nema")
	}

	pokvarena := filepath.Join(t.TempDir(), "gocop.db")
	if err := os.WriteFile(pokvarena, []byte("baza"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(pokvarena+"-wal", 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := kopirajBazu(pokvarena, "0.0.68-alfa", time.Now()); err == nil {
		t.Error("kopija s WAL-om koji se ne da pročitati prošla je")
	}
}

func TestIzdanjePrograma(t *testing.T) {
	staro := version
	t.Cleanup(func() { version = staro })
	version = ""
	if got := izdanjePrograma(); got != verzijaPrograma {
		t.Errorf("bez oznake: %q", got)
	}
	version = "0.0.70-alfa"
	if got := izdanjePrograma(); got != "0.0.70-alfa" {
		t.Errorf("s oznakom: %q", got)
	}
	// slika za Docker zadaje izdanje s oznakom commita
	version = "0.0.70-alfa (abc1234)"
	if got := izdanjePrograma(); got != "0.0.70-alfa" {
		t.Errorf("izdanje s oznakom commita = %q, želim 0.0.70-alfa", got)
	}
	if got := imeIzdanja(izdanjePrograma()); got != "0.0.70-alfa" {
		t.Errorf("ime kopije = %q, želim 0.0.70-alfa", got)
	}
}

func TestOstaviNajnovijeBezMape(t *testing.T) {
	if err := ostaviNajnovije(filepath.Join(t.TempDir(), "nema")); err == nil {
		t.Error("čišćenje nepostojeće mape prošlo je")
	}
}
