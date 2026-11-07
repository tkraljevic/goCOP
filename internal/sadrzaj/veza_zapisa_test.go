package sadrzaj

import (
	"context"
	"testing"
)

// Dva zapisa drže isti sadržaj pod istom ulogom: odvezivanje jednoga ostavlja
// vezu drugoga, pa sadržaj ostaje dok ga drugi drži
func TestOdveziUloguSamoJednogZapisa(t *testing.T) {
	s, err := Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Zatvori() //nolint:errcheck // spremište u memoriji testa
	ctx := context.Background()
	o, err := s.Upisi(ctx, "image/jpeg", []byte("fotografija"), "", Veza{"prijave", "p1", "slika:s1", ""})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Vezi(ctx, o, Veza{"prijave", "p2", "slika:s1", ""}); err != nil {
		t.Fatal(err)
	}
	if got := s.OtisakZapisaPoUlozi(ctx, "prijave", "p2", "slika:s1"); got != o {
		t.Errorf("otisak drugog zapisa: %q", got)
	}
	if got := s.OtisakZapisaPoUlozi(ctx, "prijave", "p3", "slika:s1"); got != "" {
		t.Errorf("zapis bez veze ima otisak %q", got)
	}
	if err := s.OdveziUlogu(ctx, "prijave", "p2", "slika:s1"); err != nil {
		t.Fatal(err)
	}
	if s.ImaVezu(ctx, o, Veza{"prijave", "p2", "slika:s1", ""}) || !s.ImaVezu(ctx, o, Veza{"prijave", "p1", "slika:s1", ""}) {
		t.Error("odvezan je krivi zapis")
	}
	if err := s.Ukloni(ctx, o); err == nil || !s.Ima(ctx, o) {
		t.Error("uklonjen sadržaj koji drugi zapis još drži")
	}
	if err := s.OdveziUlogu(ctx, "prijave", "p1", "slika:s1"); err != nil {
		t.Fatal(err)
	}
	if got := s.OtisakPoUlozi(ctx, "prijave", "slika:s1"); got != "" {
		t.Errorf("veza ostala nakon odvezivanja oba zapisa: %q", got)
	}
}

// Bajtovi i želja nestaju tek kad sadržaj ne drži nijedan zapis, a veza se miče
// samo za traženi zapis i traženi sadržaj
func TestOdveziIUkloni(t *testing.T) {
	ctx := context.Background()
	s, err := Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Zatvori() //nolint:errcheck // spremište u memoriji testa; test ga i sam zatvara
	prva := Veza{Entitet: "e", EntitetID: "1", Uloga: "slika"}
	druga := Veza{Entitet: "e", EntitetID: "2", Uloga: "slika"}
	const vrsta = "image/png"
	b := []byte("sadržaj")
	otisak, err := s.Upisi(ctx, vrsta, b, "ovdje", prva, druga)
	if err != nil {
		t.Fatal(err)
	}
	if bila, err := s.OdveziIUkloni(ctx, "drugi-otisak", vrsta, prva); err != nil || bila {
		t.Errorf("drugi sadržaj: %v %v", bila, err)
	}
	if bila, err := s.OdveziIUkloni(ctx, otisak, vrsta, prva); err != nil || !bila {
		t.Errorf("prva: %v %v", bila, err)
	}
	if !s.Ima(ctx, otisak) || !s.ImaVezu(ctx, otisak, druga) || s.ImaVezu(ctx, otisak, prva) {
		t.Error("nestalo je što drži druga veza")
	}
	if bila, err := s.OdveziIUkloni(ctx, otisak, vrsta, prva); err != nil || bila {
		t.Errorf("ponovljeno: %v %v", bila, err)
	}
	if _, err := s.OdveziIUkloni(ctx, otisak, vrsta, druga); err != nil {
		t.Fatal(err)
	}
	if s.Ima(ctx, otisak) {
		t.Error("bajtovi su ostali bez veze")
	}

	// sadržaj koji se još samo čeka: želja ostaje dok je netko drži
	if err := s.Vezi(ctx, otisak, prva); err != nil {
		t.Fatal(err)
	}
	if err := s.Vezi(ctx, otisak, druga); err != nil {
		t.Fatal(err)
	}
	if err := s.Zeli(ctx, otisak, vrsta, len(b), "pretplata", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OdveziIUkloni(ctx, otisak, vrsta, prva); err != nil || !s.Zeljen(ctx, otisak) {
		t.Errorf("želja druge veze: %v", err)
	}
	if _, err := s.OdveziIUkloni(ctx, otisak, vrsta, druga); err != nil || s.Zeljen(ctx, otisak) {
		t.Errorf("želja je ostala: %v", err)
	}

	if err := s.Zatvori(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OdveziIUkloni(ctx, otisak, vrsta, prva); err == nil {
		t.Error("zatvoreno spremište")
	}
}

// Primljeni paket nema veze, pa ga opis slike s njegovim otiskom ne smije obrisati.
// Slika koja je stigla bez veze briše se.
func TestOdveziIUkloniBezVeze(t *testing.T) {
	ctx := context.Background()
	s, err := Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Zatvori() //nolint:errcheck // spremište u memoriji testa
	veza := Veza{Entitet: "e", EntitetID: "1", Uloga: "slika"}
	const paket, slika = "application/vnd.gocop.arhiva", "image/png"

	otisak, err := s.Upisi(ctx, paket, []byte("paket"), "ovdje")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.OdveziIUkloni(ctx, otisak, slika, veza); err != nil || !s.Ima(ctx, otisak) {
		t.Errorf("paket je nestao: %v", err)
	}
	if err := s.Zeli(ctx, "paket-koji-se-ceka", paket, 5, "arhiva", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OdveziIUkloni(ctx, "paket-koji-se-ceka", slika, veza); err != nil || !s.Zeljen(ctx, "paket-koji-se-ceka") {
		t.Errorf("želja paketa je nestala: %v", err)
	}

	zaostala, err := s.Upisi(ctx, slika, []byte("slika"), "cvor:drugo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.OdveziIUkloni(ctx, zaostala, slika, veza); err != nil || s.Ima(ctx, zaostala) {
		t.Errorf("zaostala slika je ostala: %v", err)
	}
}

// Što se ne da obrisati, javlja se pozivatelju
func TestOdveziIUkloniKvar(t *testing.T) {
	ctx := context.Background()
	for _, tablica := range []string{"sadrzaj_veze", "sadrzaj_zeljen"} {
		s, err := Otvori("")
		if err != nil {
			t.Fatal(err)
		}
		defer s.Zatvori() //nolint:errcheck // spremište u memoriji testa
		if _, err := s.db.Exec(`DROP TABLE ` + tablica); err != nil {
			t.Fatal(err)
		}
		if _, err := s.OdveziIUkloni(ctx, "otisak", "image/png", Veza{Entitet: "e", EntitetID: "1", Uloga: "slika"}); err == nil {
			t.Errorf("bez tablice %s nema greške", tablica)
		}
	}
}
