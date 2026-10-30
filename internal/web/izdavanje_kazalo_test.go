package web

import (
	"database/sql"
	"path/filepath"
	"testing"

	"gocop/internal/arhiva"
)

// Zaključava da izdavanje s poslužitelja broj uzima i iz kazala u knjizi:
// kazalo pod v5 drži drugi sadržaj, a katalog je prazan, pa letva dobiva v6,
// a ne v1 ili v5 koje drugi čvorovi već imaju s drugim sadržajem.
func TestIzdavanjeUzimaBrojIzKazala(t *testing.T) {
	put := filepath.Join(t.TempDir(), "vodostaji.db")
	db, err := sql.Open("sqlite", arhiva.AdresaZaPisanje(put))
	if err != nil {
		t.Fatal(err)
	}
	if err := arhiva.PripremiPraznu(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta, zapisa) VALUES ('drava','vodograd','his2000','vodostaj','satni',1);
		INSERT INTO ocitanja (niz, vrijeme, vrijednost) SELECT id, 1000, 120 FROM nizovi`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s := &Server{arhivaPut: put, paketiDir: t.TempDir()}
	s.SetKazaloArhive(func(string) (int, string) { return 5, "drugi-otisak" })
	iz, err := s.IzdajArhivu("", false, nil)
	if err != nil || len(iz.Redci) != 1 || iz.Redci[0].Izdanje != 6 {
		t.Errorf("izdavanje uz kazalo s v5: %+v, %v", iz.Redci, err)
	}
}
