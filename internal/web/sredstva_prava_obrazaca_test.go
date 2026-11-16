package web

import (
	"net/http"
	"strings"
	"testing"

	"gocop/internal/models"
)

// Rukovoditelj branjenog područja smije upisati skladište u svoje područje,
// pa vidi gumb „Novo skladište” i obrazac samo sa svojim područjem. Tko ne
// smije spremiti, obrazac i karticu „Nova inventura” ne dobiva.
func TestSredstvaObrasciPoPravima(t *testing.T) {
	o := novaOkolinaSredstava(t)
	podrucja := &models.UserPermissions{AdminAreas: map[int]bool{34: true}, AllowedSectors: map[string]bool{"B": true}}
	if _, err := o.baza.Exec(`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (35, 'B', 'Karašica', 'COP', 'Osijek')`); err != nil {
		t.Fatal(err)
	}
	ima := func(ime string, perms *models.UserPermissions, putanja, ocekivano string) {
		t.Helper()
		if s := o.ispis(t, perms, putanja); !strings.Contains(s, ocekivano) {
			t.Errorf("%s: %s nema %q:\n%s", ime, putanja, ocekivano, s)
		}
	}

	ima("rukovoditelj područja vidi Novo skladište", podrucja, "/sredstva?sektor=B", "SmijeUrediti=true")
	ima("promatrač ne vidi Novo skladište", promatracSredstava(), "/sredstva?sektor=B", "SmijeUrediti=false")
	ima("rukovoditelj područja bira samo svoje", podrucja, "/sredstva/skladista/novo?sektor=B", "Podrucja=1;")
	ima("uprava sektora bira sva područja", upravaSredstava(), "/sredstva/skladista/novo?sektor=B", "Podrucja=2;")
	if w := o.zovi(t, promatracSredstava(), http.MethodGet, "/sredstva/skladista/novo?sektor=B", nil); w.Code != http.StatusForbidden {
		t.Errorf("promatrač otvara obrazac novog skladišta: %d", w.Code)
	}

	ima("uprava otvara inventuru", upravaSredstava(), "/sredstva/popisi?sektor=B", "ZaInventuru=1;")
	ima("promatrač ne otvara inventuru", promatracSredstava(), "/sredstva/popisi?sektor=B", "ZaInventuru=0;")
}
