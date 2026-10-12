package peers

// Mreža se pri pokretanju čita iz baze, a privatni ključ mreže iz datoteke
// uz bazu. Ključ koji ne pripada mreži iz baze ili se ne da pročitati
// zaustavlja pokretanje: čvor ne smije potpisivati tuđim ključem ni tiho
// izgubiti pravo primanja.

import (
	"os"
	"path/filepath"
	"testing"

	"gocop/internal/razmjena"
)

// Nositelj ključa mreže nakon ponovnog pokretanja i dalje drži ključ; bez
// datoteke ključa isti čvor je samo član iste mreže.
func TestUcitavanjeMrezeNositeljIClan(t *testing.T) {
	osnivac := rubnaMreza(t, "cop-osijek")
	prije := osnivac.NetworkInfo()

	s, err := NewService(osnivac.db, osnivac.rec, osnivac.node, Ports{})
	if err != nil {
		t.Fatal(err)
	}
	n := s.NetworkInfo()
	if n == nil || !n.DrziKljuc || n.Name != "Hrvatske vode" || n.PublicKey != prije.PublicKey || !n.JoinedAt.Equal(prije.JoinedAt) {
		t.Fatalf("nositelj nakon pokretanja: %+v, prije %+v", n, prije)
	}

	if err := os.Rename(osnivac.networkKeyPath(), filepath.Join(t.TempDir(), NetworkKeyFileName)); err != nil {
		t.Fatal(err)
	}
	s, err = NewService(osnivac.db, osnivac.rec, osnivac.node, Ports{})
	if err != nil {
		t.Fatal(err)
	}
	if n := s.NetworkInfo(); n == nil || n.DrziKljuc || n.PublicKey != prije.PublicKey {
		t.Errorf("bez datoteke ključa: %+v", n)
	}
}

// Svaki kvar mreže ili njezina ključa zaustavlja pokretanje.
func TestUcitavanjeMrezeOdbijaKvarove(t *testing.T) {
	for ime, kvar := range map[string]func(t *testing.T, s *Service){
		"ključ druge mreže": func(t *testing.T, s *Service) {
			tudja, err := razmjena.NewNetwork("Tuđa mreža")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(s.networkKeyPath()); err != nil {
				t.Fatal(err)
			}
			if err := razmjena.SaveKey(s.networkKeyPath(), tudja.Private()); err != nil {
				t.Fatal(err)
			}
		},
		"datoteka ključa nije ključ": func(t *testing.T, s *Service) {
			if err := os.WriteFile(s.networkKeyPath(), []byte("nije ključ"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"javni ključ mreže u bazi nije ključ": func(t *testing.T, s *Service) {
			if _, err := s.db.Exec(`UPDATE network SET public_key = 'nije-kljuc' WHERE id = 1`); err != nil {
				t.Fatal(err)
			}
		},
		"zapis mreže se ne čita": func(t *testing.T, s *Service) { pokvariTablicu(t, s, "network") },
	} {
		t.Run(ime, func(t *testing.T) {
			osnivac := rubnaMreza(t, "cop-osijek")
			kvar(t, osnivac)
			if s, err := NewService(osnivac.db, osnivac.rec, osnivac.node, Ports{}); err == nil {
				t.Errorf("pokrenut je čvor s mrežom %+v", s.NetworkInfo())
			}
		})
	}
}
