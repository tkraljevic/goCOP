package kisomjeri

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
	"time"

	"gocop/internal/arhiva"
	"gocop/internal/db"
)

// uchg je zastavica macOS-a (UF_IMMUTABLE) uz koju se datoteka čita, a ne da
// se maknuti, kao CSV otvoren u Excelu na Windowsu
const uchg = 0x2

// Zaključava da stari niz koji se nije dao maknuti ne zaustavlja ulaganje.
// Novi niz je zapisan, pa se ide na sljedeću postaju, obje letve se grade, a
// upozorenje se vrati uz njih, da ga satni krug zapiše u dnevnik. Prije je
// ulaganje stalo na prvoj takvoj postaji, a ni njezina letva se nije gradila.
func TestUloziStariNizNeZaustavlja(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	s := &Spremiste{DB: baza}
	if err := s.Pripremi(); err != nil {
		t.Fatal(err)
	}
	sada := time.Date(2026, 9, 27, 9, 0, 0, 0, Zagreb)
	sat := time.Date(2026, 9, 26, 18, 0, 0, 0, Zagreb)
	if _, err := s.Upisi(context.Background(), []Mjerenje{
		{Kisomjer: "a", Kraj: sat, Sati: 1, Oborina: 1.2, Izvor: "kisomjer-pljusak"},
		{Kisomjer: "b", Kraj: sat, Sati: 1, Oborina: 0.4, Izvor: "kisomjer-pljusak"},
	}); err != nil {
		t.Fatal(err)
	}
	koren := t.TempDir()
	stari, err := arhiva.Upisi(koren, SlivStabla, "a", "kisomjer-pljusak", "oborina", "satni",
		[]arhiva.Redak{{Vrijeme: time.Date(2025, 5, 1, 7, 0, 0, 0, Zagreb), Vrijednost: 3}})
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Chflags(stari, uchg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := syscall.Chflags(stari, 0); err != nil {
			t.Error(err)
		}
	})

	prom, err := Ulozi(koren, s, []Postaja{{Code: "a"}, {Code: "b"}}, sada.Add(-72*time.Hour), sada)
	var o *arhiva.StariNizOstao
	if !errors.As(err, &o) || o.Stari != filepath.Base(stari) {
		t.Errorf("upozorenje o starom nizu: %v", err)
	}
	if !slices.Equal(prom, []string{"a", "b"}) {
		t.Fatalf("promijenjene letve %v, želim [a b]", prom)
	}
	for _, letva := range prom {
		if r, err := arhiva.PostojeciRedci(koren, SlivStabla, letva, "kisomjer-pljusak", "oborina", "satni"); err != nil || len(r) == 0 {
			t.Errorf("%s: satni niz %v, %v", letva, r, err)
		}
	}
}
