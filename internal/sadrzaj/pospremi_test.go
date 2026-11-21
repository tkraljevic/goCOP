package sadrzaj

import (
	"context"
	"path/filepath"
	"testing"
)

// Pospremanje vrste briše pakete koji ne ostaju, i one nastale ovdje i
// primljene bez veze, te želje za njima. Ostaje paket koji treba ostati,
// paket koji drži i drugi zapis i sadržaj druge vrste.
func TestPospremiVrstu(t *testing.T) {
	s, err := Otvori(filepath.Join(t.TempDir(), "sadrzaj.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = s.Zatvori() // test ga na kraju već zatvara; drugo zatvaranje ne mijenja ishod
	}()
	ctx := context.Background()
	const vrsta, drugaVrsta = "application/vnd.gocop.arhiva", "application/pdf"
	paket := func(id string) Veza { return Veza{Entitet: "arhiva_paket", EntitetID: id, Uloga: "paket"} }
	upisi := func(b, vrsta, izvor string, veze ...Veza) string {
		t.Helper()
		o, err := s.Upisi(ctx, vrsta, []byte(b), izvor, veze...)
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	v1 := upisi("kisa v1", vrsta, "ovdje", paket("kisa"))
	v2 := upisi("kisa v2", vrsta, "ovdje", paket("kisa"))
	v3 := upisi("kisa v3", vrsta, "ovdje", paket("kisa"))
	primljen := upisi("drava v1", vrsta, "server")
	drugi := upisi("drava v2", vrsta, "ovdje", paket("drava"), Veza{Entitet: "akt", EntitetID: "a1", Uloga: "prilog"})
	pdf := upisi("%PDF sken", drugaVrsta, "ovdje", Veza{Entitet: "akt", EntitetID: "a2", Uloga: "sken"})
	stara := Otisak([]byte("kisa v0"))
	nova := Otisak([]byte("kisa v4"))
	for _, o := range []string{stara, nova} {
		if err := s.Zeli(ctx, o, vrsta, 7, "arhiva", ""); err != nil {
			t.Fatal(err)
		}
	}

	n, err := s.PospremiVrstu(ctx, vrsta, "arhiva_paket", map[string]bool{v2: true, v3: true, nova: true})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("obrisano %d sadržaja, očekivana 2 (kisa v1 i primljeni drava v1)", n)
	}
	for o, htio := range map[string]bool{v1: false, v2: true, v3: true, primljen: false, drugi: true, pdf: true} {
		if s.Ima(ctx, o) != htio {
			t.Errorf("%s: na računalu %v, očekivano %v", o[:8], !htio, htio)
		}
	}
	if s.ImaVezu(ctx, drugi, paket("drava")) {
		t.Error("paketu koji drži i drugi zapis nije maknuta veza kazala")
	}
	if !s.ImaVezu(ctx, drugi, Veza{Entitet: "akt", EntitetID: "a1", Uloga: "prilog"}) {
		t.Error("maknuta je veza drugog zapisa")
	}
	if s.Zeljen(ctx, stara) || !s.Zeljen(ctx, nova) {
		t.Errorf("želje: stara %v (očekivano ne), nova %v (očekivano da)", s.Zeljen(ctx, stara), s.Zeljen(ctx, nova))
	}
	// drugi prolaz nema što brisati
	if n, err := s.PospremiVrstu(ctx, vrsta, "arhiva_paket", map[string]bool{v2: true, v3: true, nova: true}); err != nil || n != 0 {
		t.Errorf("drugi prolaz: %d, %v", n, err)
	}
	// zatvoreno spremište javlja grešku
	if err := s.Zatvori(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PospremiVrstu(ctx, vrsta, "arhiva_paket", nil); err == nil {
		t.Error("zatvoreno spremište: bez greške")
	}
}
