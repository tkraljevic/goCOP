package web

import (
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/prognoza"
)

// Čitač daje vodostaj u satu izdanja; bez čitača, izdanja ili baze ništa,
// pa kartica broji porast od dana 0.
func TestPolazistaZa(t *testing.T) {
	put := filepath.Join(t.TempDir(), "p.db")
	baza, err := prognoza.Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO izdane (letva, velicina, izdano, ciljni, vrijednost, dolje, gore, model)
		VALUES ('batina', 'vodostaj', 100, 100, -53, 0, 0, 'proba')`); err != nil {
		t.Fatal(err)
	}
	baza.Close()
	c, err := OtvoriPrognoze(put)
	if err != nil {
		t.Fatal(err)
	}
	izdano := time.Unix(100*3600, 0)
	if p := c.PolazistaZa(izdano); p["batina"] != -53 {
		t.Errorf("polazišta %v", p)
	}
	if p := c.PolazistaZa(time.Time{}); p != nil {
		t.Errorf("bez izdanja %v", p)
	}
	var nema *CitacPrognoza
	if p := nema.PolazistaZa(izdano); p != nil {
		t.Errorf("bez čitača %v", p)
	}
	c.Close()
	if p := c.PolazistaZa(izdano); p != nil {
		t.Errorf("zatvorena baza %v", p)
	}
}
