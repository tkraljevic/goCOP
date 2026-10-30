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
