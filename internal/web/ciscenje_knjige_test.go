package web

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Neispravne zapise iz knjige briše samo globalni administrator, svojim
// očima, s ovog računala ili iz lokalne mreže (Odluke (30. 10. 2026.));
// ostali dobivaju 403, a knjiga ostaje ista
func TestCiscenjeKnjigeSamoNaRacunalu(t *testing.T) {
	_, rec, baza := cvorZaUvoz(t, "cop-osijek")
	h := &DBMaintHandler{db: func() *sql.DB { return baza }, rec: rec}
	ruta := samoNaCvoru(zaKnjigu, http.HandlerFunc(h.HandleOcistiKnjigu))
	ubaciNeispravnu := func() {
		t.Helper()
		if _, err := baza.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel)
			VALUES ('~', 'stations', 's1', 'drugi', '', 0, '{}', '2026-10-30T10:00:00Z', 1, '')`); err != nil {
			t.Fatal(err)
		}
	}
	brojNeispravnih := func() int {
		var n int
		if err := baza.QueryRow(`SELECT COUNT(*) FROM record_versions WHERE version_id = '~'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	ubaciNeispravnu()
	for _, x := range uvjetiRacunala(zaKnjigu) {
		w := httptest.NewRecorder()
		ruta.ServeHTTP(w, zahtjevPostavki(http.MethodPost, "/administracija/baza/ocisti-knjigu", nil, x.u))
		if x.poruka == "" {
			if _, upit := preusmjerenje(t, w); !strings.HasPrefix(upit.Get("success"), "Iz knjige je obrisano 1 neispravna verzija") {
				t.Errorf("%s: %v", x.opis, upit)
			}
			if brojNeispravnih() != 0 {
				t.Errorf("%s: neispravna verzija je ostala", x.opis)
			}
			ubaciNeispravnu()
			continue
		}
		if w.Code != http.StatusForbidden || strings.TrimSpace(w.Body.String()) != x.poruka {
			t.Errorf("%s: %d %q", x.opis, w.Code, w.Body.String())
		}
		if brojNeispravnih() != 1 {
			t.Errorf("%s: knjiga je promijenjena", x.opis)
		}
	}
	// zatvorena baza: greška se javlja, ništa se ne tvrdi da je obrisano
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	ruta.ServeHTTP(w, zahtjevPostavki(http.MethodPost, "/administracija/baza/ocisti-knjigu", nil, kaoAdmin))
	if _, upit := preusmjerenje(t, w); !strings.HasPrefix(upit.Get("error"), "Knjiga nije očišćena") {
		t.Errorf("zatvorena baza: %v", upit)
	}
}
