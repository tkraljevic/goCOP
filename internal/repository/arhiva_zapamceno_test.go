package repository

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
)

// Zapamćeni pregled niza, dosezi i sažetak spojenih nizova jednaki su
// izračunu iz arhive, i nakon što se arhiva promijeni: tada se ne vraća
// zapamćeno, nego novo stanje.
func TestZapamcenoIstoKaoIzracunIPratiArhivu(t *testing.T) {
	ctx := context.Background()
	r := punaArhiva(t)
	provjeri := func(kad string) {
		t.Helper()
		for range 2 { // drugi put iz zapamćenog
			for _, niz := range []int64{1, 2, 3} {
				dobio, err1 := r.PregledZapamcen(ctx, niz)
				htio, err2 := r.Pregled(ctx, niz)
				if err1 != nil || err2 != nil || !reflect.DeepEqual(dobio, htio) {
					t.Errorf("%s: pregled niza %d: %+v (%v), izračun %+v (%v)", kad, niz, dobio, err1, htio, err2)
				}
			}
			for _, letva := range []string{"batina", "osijek", "nema"} {
				d1, err1 := r.SpojDoseziZapamceni(ctx, letva)
				d2, err2 := r.SpojDosezi(ctx, letva)
				if err1 != nil || err2 != nil || !reflect.DeepEqual(d1, d2) {
					t.Errorf("%s: dosezi %s: %+v, izračun %+v", kad, letva, d1, d2)
				}
				s1, err1 := r.SazetakZapamcen(ctx, letva)
				s2, err2 := r.Sazetak(ctx, letva)
				if err1 != nil || err2 != nil || !reflect.DeepEqual(s1, s2) {
					t.Errorf("%s: sažetak %s: %+v, izračun %+v", kad, letva, s1, s2)
				}
			}
		}
	}
	provjeri("prije izmjene")
	staro, err := r.SpojDoseziZapamceni(ctx, "batina")
	if err != nil {
		t.Fatal(err)
	}

	pis, err := sql.Open("sqlite", r.put)
	if err != nil {
		t.Fatal(err)
	}
	defer pis.Close()
	for _, q := range []string{
		`INSERT INTO spoj VALUES ('batina', 'vodostaj', 'dnevni', ` + itoa64(satArhive(2023, 3, 1, 0)) + `, 777, 'his2000', 'srednjak', 1)`,
		`INSERT INTO ocitanja VALUES (1, ` + itoa64(satArhive(1996, 7, 1, 0)) + `, 640)`,
	} {
		if _, err := pis.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	provjeri("nakon izmjene")
	novo, err := r.SpojDoseziZapamceni(ctx, "batina")
	if err != nil || reflect.DeepEqual(novo, staro) {
		t.Errorf("dosezi se nisu promijenili s arhivom (%v)", err)
	}
	var bez *ArhivaRepository
	if p, err := bez.PregledZapamcen(ctx, 1); p != nil || err != nil {
		t.Errorf("pregled bez arhive: %v %v", p, err)
	}
	if d, err := bez.SpojDoseziZapamceni(ctx, "batina"); d != nil || err != nil {
		t.Errorf("dosezi bez arhive: %v %v", d, err)
	}
	if s, err := bez.SazetakZapamcen(ctx, "batina"); s != nil || err != nil {
		t.Errorf("sažetak bez arhive: %v %v", s, err)
	}
}
