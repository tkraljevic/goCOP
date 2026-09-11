package arhiva

import (
	"bytes"
	"crypto/ed25519"
	"database/sql"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func probniManifest() (Manifest, []DioPaketa) {
	return Manifest{
			Inacica: 2, Letva: "batina", Izdanje: 3, Izdao: "cop-osijek-node",
			Otisak: "6e0ed8df", Nizova: 15, Zapisa: 933732, Od: "1901-01-01", Do: "2026-09-11",
		}, []DioPaketa{
			{Ime: "nizovi.json", Otisak: "aa", Bajtova: 10},
			{Ime: "ocitanja.bin", Otisak: "bb", Bajtova: 20},
		}
}

// Otisak dokazuje cjelovitost, potpis dokazuje tko je izdao. Tko promijeni
// sadržaj, izračuna novi otisak i upiše proizvoljno ime u "izdao" — potpis je
// jedino što to razlikuje.
func TestPotpisPrepoznajePromjenuSadrzaja(t *testing.T) {
	javni, tajni, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	m, d := probniManifest()
	p := potpisi(m, d, tajni)
	if p == nil {
		t.Fatal("potpis nije nastao")
	}

	got, err := ProvjeriPotpis(m, d, p)
	if err != nil {
		t.Fatalf("valjan potpis odbijen: %v", err)
	}
	if !ed25519.PublicKey(got).Equal(javni) {
		t.Error("vraćen krivi ključ")
	}

	// Promijenjen otisak — sadržaj je drugi.
	m2 := m
	m2.Otisak = "drugi"
	if _, err := ProvjeriPotpis(m2, d, p); err == nil {
		t.Error("potpis je prošao uz promijenjen otisak")
	}
	// Promijenjen izdavač — netko se predstavlja kao drugi čvor.
	m3 := m
	m3.Izdao = "tudji-cvor"
	if _, err := ProvjeriPotpis(m3, d, p); err == nil {
		t.Error("potpis je prošao uz promijenjenog izdavača")
	}
	// Promijenjen dio — zajednički otisak veže spojene dijelove, ali ne i
	// granicu među njima; otisak po dijelu veže i to.
	d2 := append([]DioPaketa(nil), d...)
	d2[0].Bajtova = 11
	if _, err := ProvjeriPotpis(m, d2, p); err == nil {
		t.Error("potpis je prošao uz promijenjen dio")
	}
}

// Poredak dijelova u ZIP-u ne smije mijenjati potpis: isti sadržaj prepakiran
// drugim redom je isti paket.
func TestPotpisNeOvisiOPoretkuDijelova(t *testing.T) {
	_, tajni, _ := ed25519.GenerateKey(nil)
	m, d := probniManifest()
	obrnuto := []DioPaketa{d[1], d[0]}

	p := potpisi(m, d, tajni)
	if _, err := ProvjeriPotpis(m, obrnuto, p); err != nil {
		t.Errorf("potpis pukao na drugom poretku: %v", err)
	}
}

// Tuđi ključ ne prolazi.
func TestTudjiKljucNeProlazi(t *testing.T) {
	_, tajni, _ := ed25519.GenerateKey(nil)
	_, drugi, _ := ed25519.GenerateKey(nil)
	m, d := probniManifest()

	p := potpisi(m, d, tajni)
	lazni := potpisi(m, d, drugi)
	// Potpis jednog s ključem drugog.
	krivo := &Potpis{Kljuc: p.Kljuc, Vaznost: lazni.Vaznost}
	if _, err := ProvjeriPotpis(m, d, krivo); err == nil {
		t.Error("potpis s tuđim ključem je prošao")
	}
}

// Nepotpisan paket se prepoznaje kao nepotpisan, ne kao neispravan.
func TestNepotpisanSePrepoznaje(t *testing.T) {
	m, d := probniManifest()
	_, err := ProvjeriPotpis(m, d, nil)
	if err == nil || !strings.Contains(err.Error(), "nije potpisan") {
		t.Errorf("nepotpisan paket: %v", err)
	}
	if potpisi(m, d, nil) != nil {
		t.Error("bez ključa je nastao potpis")
	}
}

// Kanonski zapis mora biti isti za isti sadržaj, svaki put.
func TestKanonskiZapisJeStabilan(t *testing.T) {
	m, d := probniManifest()
	a := string(kanonski(m, d))
	b := string(kanonski(m, []DioPaketa{d[1], d[0]}))
	if a != b {
		t.Error("kanonski zapis ovisi o poretku dijelova")
	}
	if !strings.Contains(a, "izdao\tcop-osijek-node\n") {
		t.Errorf("kanonski zapis: %q", a)
	}
	// Vrijeme nastanka NE ulazi: isti sadržaj izdan dvaput mora dati isti
	// potpis, kao što daje i isti otisak.
	if strings.Contains(a, "nastalo") {
		t.Error("vrijeme nastanka ulazi u potpis, pa isti sadržaj daje dva potpisa")
	}
}

// Potpisivanje ne smije pomaknuti ni otisak ni broj izdanja: otisak se računa
// preko podataka, a potpis živi u manifestu koji je izvan njega. Zato se
// postojeći paket može izdati potpisan i ostati isto izdanje.
func TestPotpisNeMijenjaOtisak(t *testing.T) {
	db, _ := arhivaSNizom(t, "letva-hv")
	defer db.Close()

	PostaviKljucIzdavaca(nil)
	var bez bytes.Buffer
	m1, err := Izvezi(db, "vukovar", 3, "cop-osijek", &bez)
	if err != nil {
		t.Fatal(err)
	}
	if m1.Potpis != nil {
		t.Error("bez ključa je nastao potpis")
	}

	_, tajni, _ := ed25519.GenerateKey(nil)
	PostaviKljucIzdavaca(tajni)
	defer PostaviKljucIzdavaca(nil)
	var sPotpisom bytes.Buffer
	m2, err := Izvezi(db, "vukovar", 3, "cop-osijek", &sPotpisom)
	if err != nil {
		t.Fatal(err)
	}
	if m2.Potpis == nil {
		t.Fatal("potpis nije nastao")
	}
	if m2.Otisak != m1.Otisak {
		t.Errorf("potpis je pomaknuo otisak: %s → %s", kratki(m1.Otisak), kratki(m2.Otisak))
	}
	if m2.Izdanje != m1.Izdanje {
		t.Errorf("potpis je pomaknuo izdanje: %d → %d", m1.Izdanje, m2.Izdanje)
	}

	// Potpisan paket se čita i potpis se provjerava.
	s, err := Procitaj(bytes.NewReader(sPotpisom.Bytes()), int64(sPotpisom.Len()))
	if err != nil {
		t.Fatalf("potpisan paket se ne čita: %v", err)
	}
	if len(s.PotpisaoKljuc) == 0 {
		t.Error("ključ potpisnika nije zapamćen")
	}
	// Nepotpisan se i dalje čita — odbijanje je odluka pri ugradnji, ne ovdje.
	if _, err := Procitaj(bytes.NewReader(bez.Bytes()), int64(bez.Len())); err != nil {
		t.Errorf("nepotpisan paket se ne čita: %v", err)
	}
}

// Diran potpisan paket se prepoznaje. Otisak bi promjenu uhvatio samo ako
// napadač ne preračuna i njega; potpis hvata i preračunatog.
func TestDiranPotpisanPaketSeOdbija(t *testing.T) {
	db, _ := arhivaSNizom(t, "letva-hv")
	defer db.Close()
	_, tajni, _ := ed25519.GenerateKey(nil)
	PostaviKljucIzdavaca(tajni)
	defer PostaviKljucIzdavaca(nil)

	var b bytes.Buffer
	if _, err := Izvezi(db, "vukovar", 1, "cop-osijek", &b); err != nil {
		t.Fatal(err)
	}
	s, err := Procitaj(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}
	// Napadač mijenja izdavača i preračunava otisak — otisak se poklapa jer je
	// nad podacima, koje nije dirao. Potpis ne prolazi.
	krivi := s.Manifest
	krivi.Izdao = "tudji-cvor"
	bezPotpisa := krivi
	bezPotpisa.Potpis = nil
	if _, err := ProvjeriPotpis(bezPotpisa, krivi.Dijelovi, krivi.Potpis); err == nil {
		t.Error("promijenjen izdavač je prošao provjeru potpisa")
	}
}

// Otisak mora ovisiti samo o sadržaju. Vrijeme naše gradnje ne smije u njega:
// dok je "osvjezeno" putovalo paketom, svaka ponovna gradnja mijenjala je
// otisak iako se nijedna vrijednost nije promijenila, pa je izdanje skakalo
// bez razloga. Na Batini se to i dogodilo — ponovna gradnja bez ijedne
// promjene tražila je v4.
func TestPonovnaGradnjaNeMijenjaOtisak(t *testing.T) {
	koren := t.TempDir()
	baza := filepath.Join(t.TempDir(), "arhiva.db")
	kad := time.Date(2026, 9, 11, 7, 0, 0, 0, time.UTC)
	if _, err := Upisi(koren, "dunav", "batina", "his2000", "vodostaj", "satni",
		[]Redak{{Vrijeme: kad, Vrijednost: -160}}); err != nil {
		t.Fatal(err)
	}

	otisak := func() string {
		if _, err := Izgradi(koren, baza, "batina", nil); err != nil {
			t.Fatal(err)
		}
		db, err := sql.Open("sqlite", baza+"?mode=ro")
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		m, err := Izvezi(db, "batina", 1, "cop-osijek", io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		return m.Otisak
	}

	prvi := otisak()
	// Druga gradnja istog sadržaja; "osvjezeno" se pritom mijenja.
	time.Sleep(1100 * time.Millisecond)
	drugi := otisak()
	if prvi != drugi {
		t.Errorf("ponovna gradnja bez promjene podataka dala drugi otisak:\n  %s\n  %s",
			kratki(prvi), kratki(drugi))
	}
}
