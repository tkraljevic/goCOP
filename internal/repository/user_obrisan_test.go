package repository

import (
	"testing"

	"github.com/google/uuid"
)

// Račun je obrisan samo kad ga nema među računima, a zadnja verzija u
// knjizi ga briše. Račun koji još nije stigao, ili je u knjizi, a nije
// među računima, nije obrisan. Greška čitanja se javlja.
func TestRacunObrisan(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	ana := novaOsoba(t, repo, "aanic", "Ana Anić", nil)
	ivo := novaOsoba(t, repo, "iivic", "Ivo Ivić", nil)
	nuzno(t, repo.DeleteUser(ana.ID))
	if _, err := n.db.Exec(`DELETE FROM users WHERE id = ?`, ivo.ID.String()); err != nil {
		t.Fatal(err)
	}
	marko := novaOsoba(t, repo, "mmarkic", "Marko Markić", nil)
	for _, k := range []struct {
		naziv   string
		id      uuid.UUID
		obrisan bool
	}{
		{"obrisan račun", ana.ID, true},
		{"račun u knjizi, a nije među računima", ivo.ID, false},
		{"račun koji još nije stigao", uuid.New(), false},
		{"račun koji postoji", marko.ID, false},
	} {
		obrisan, err := repo.RacunObrisan(k.id)
		if err != nil || obrisan != k.obrisan {
			t.Errorf("%s: obrisan %v (%v), očekivano %v", k.naziv, obrisan, err, k.obrisan)
		}
	}

	nuzno(t, n.db.Close())
	if obrisan, err := repo.RacunObrisan(ana.ID); err == nil || obrisan {
		t.Errorf("zatvorena baza: obrisan %v, greška %v", obrisan, err)
	}
}
