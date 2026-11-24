package oborine

import (
	"reflect"
	"testing"
)

// Snijeg i temperatura čitaju se po kišomjeru i satu za zadane sate; prazno
// polje izostaje samo iz svog niza, a sat izvan raspona ne dolazi.
func TestSnijegITemperatura(t *testing.T) {
	u := probnaBaza(t)
	if _, err := u.DB.Exec(`INSERT INTO satne (kisomjer, sat, oborina, snijeg, temperatura, prognoza, preuzeto) VALUES
		('k-a-1', 10, 1, 0.5, -2, 0, 1),
		('k-a-1', 11, 1, NULL, 3, 0, 1),
		('k-a-1', 12, 1, 0.2, NULL, 0, 1),
		('k-b-1', 11, 2, 0, 7.5, 1, 1),
		('k-b-1', 20, 2, 9, 9, 1, 1)`); err != nil {
		t.Fatal(err)
	}
	sn, te, err := u.SnijegITemperatura(10, 12)
	if err != nil {
		t.Fatal(err)
	}
	wantSn := map[string]map[int64]float64{"k-a-1": {10: 0.5, 12: 0.2}, "k-b-1": {11: 0}}
	wantTe := map[string]map[int64]float64{"k-a-1": {10: -2, 11: 3}, "k-b-1": {11: 7.5}}
	if !reflect.DeepEqual(sn, wantSn) || !reflect.DeepEqual(te, wantTe) {
		t.Errorf("snijeg %v, temperatura %v; želim %v, %v", sn, te, wantSn, wantTe)
	}

	t.Run("pokvaren redak", func(t *testing.T) {
		if _, err := u.DB.Exec(`UPDATE satne SET snijeg = 'x' WHERE sat = 10`); err != nil {
			t.Fatal(err)
		}
		if sn, te, err := u.SnijegITemperatura(10, 12); err == nil {
			t.Errorf("bez greške: %v, %v", sn, te)
		}
	})
	t.Run("zatvorena baza", func(t *testing.T) {
		if err := u.DB.Close(); err != nil {
			t.Fatal(err)
		}
		if sn, te, err := u.SnijegITemperatura(10, 12); err == nil {
			t.Errorf("bez greške: %v, %v", sn, te)
		}
	})
}
