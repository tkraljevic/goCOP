package sadrzaj

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

// Želje koje čvor ne uzima preskaču se: 250 neželjenih ispred jedne
// željene ne zadržava je, a ograda broji samo uzete. Nečitljiv popis javlja
// grešku.
func TestZeljeniKojePreskaceNeuzete(t *testing.T) {
	s, err := Otvori(filepath.Join(t.TempDir(), "sadrzaj.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Zatvori()
	ctx := context.Background()
	for i := 0; i < 250; i++ {
		o := Otisak([]byte(fmt.Sprintf("izvornik prijave br. %d", i)))
		if err := s.Zeli(ctx, o, "application/pdf", 10, "pretplata", "prijave/3/2026"); err != nil {
			t.Fatal(err)
		}
	}
	// željena je već jednom tražena, pa stoji iza svih koje još nisu
	foto := Otisak([]byte("fotografija uz dnevni list"))
	if err := s.Zeli(ctx, foto, "image/jpeg", 10, "pretplata", "vodocuvarski/16/2026"); err != nil {
		t.Fatal(err)
	}
	if err := s.Trazeno(ctx, foto); err != nil {
		t.Fatal(err)
	}
	if z, _ := s.Zeljeni(ctx, 200); len(z) != 200 || z[199].Otisak == foto {
		t.Fatalf("Zeljeni bez probira: %d želja", len(z))
	}
	z, err := s.ZeljeniKoje(ctx, 200, func(z Zelja) bool { return z.Kanal == "vodocuvarski/16/2026" })
	if err != nil || len(z) != 1 || z[0].Otisak != foto || z[0].Trazeno == nil {
		t.Fatalf("željena iza neželjenih: %+v %v", z, err)
	}
	if z, _ := s.ZeljeniKoje(ctx, 10, func(Zelja) bool { return true }); len(z) != 10 {
		t.Errorf("ograda: %d želja", len(z))
	}
	if z, _ := s.Zeljeni(ctx, 300); len(z) != 251 || z[250].Otisak != foto {
		t.Errorf("svih želja %d", len(z))
	}

	if _, err := s.db.Exec(`UPDATE sadrzaj_zeljen SET bajtova = 'nije broj' WHERE otisak = ?`, foto); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ZeljeniKoje(ctx, 300, func(Zelja) bool { return true }); err == nil {
		t.Error("nečitljiva želja bez greške")
	}
	s.Zatvori()
	if _, err := s.Zeljeni(ctx, 10); err == nil {
		t.Error("zatvoreno spremište vraća želje bez greške")
	}
}
