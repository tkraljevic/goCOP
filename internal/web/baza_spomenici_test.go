package web

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// „Obriši spomenike” u Održavanju baze ne vraća obrisani zapis: briše se
// samo spomenik zapisa kojemu je sažimanje već obrisalo starije verzije.
// Sažimanje i brisanje spomenika ostaju u dnevniku programa, s osobom i
// brojem obrisanih verzija. Na kućnom serveru 3. 11. 2026. brisanje svih
// spomenika ostavilo je starije žive verzije, pa su se obrisani zapisi
// vratili, a u dnevniku programa nije bilo ničega.
func TestOdrzavanjeBazeBrisanjeSpomenika(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "baza.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	rec := ledger.New(baza, "cop-osijek")
	ctx := context.Background()
	for _, id := range []string{"stari", "novi"} {
		if _, err := rec.Record(ctx, baza, "stations", id, map[string]string{"id": id}); err != nil {
			t.Fatal(err)
		}
		if _, err := rec.Archive(ctx, baza, "stations", id, map[string]string{"id": id}); err != nil {
			t.Fatal(err)
		}
	}
	// zadnja verzija čvora, da granica ne čuva nijedan spomenik
	if _, err := rec.Record(ctx, baza, "stations", "zivi", map[string]string{"id": "zivi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`UPDATE record_versions SET created_at = ? WHERE entity_id = 'stari'`, time.Now().AddDate(0, 0, -200)); err != nil {
		t.Fatal(err)
	}
	h := NewDBMaintHandler(func() *sql.DB { return baza }, rec, nil, nil, nil)
	h.SetOpcije(func(context.Context) models.Opcije { return models.Opcije{BrisanjePovijestiVerzija: true} })
	var dnevnik bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&dnevnik)
	defer log.SetOutput(stari)
	posalji := func(put string, polja url.Values, radnja http.HandlerFunc) string {
		r := httptest.NewRequest(http.MethodPost, put, strings.NewReader(polja.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r = r.WithContext(context.WithValue(r.Context(), contextKeyPerms, &models.UserPermissions{IsGlobalAdmin: true}))
		w := httptest.NewRecorder()
		radnja(w, r)
		return w.Header().Get("Location")
	}

	// prije sažimanja oba obrisana zapisa imaju živu verziju
	if n, err := rec.SpomeniciZaBrisanje(ctx); err != nil || n != 0 {
		t.Fatalf("prije sažimanja: %d %v", n, err)
	}
	if loc := posalji("/administracija/baza/sazmi", url.Values{"days": {"90"}}, h.HandleCompact); !strings.Contains(loc, "success") {
		t.Fatalf("sažimanje: %s", loc)
	}
	if loc := posalji("/administracija/baza/spomenici", nil, h.HandlePurgeTombstones); !strings.Contains(loc, "Obrisano%3A+1+spomenik") {
		t.Fatalf("briše se samo spomenik starog zapisa: %s", loc)
	}
	if l, err := rec.Latest(ctx, "stations", "stari"); !errors.Is(err, ledger.ErrNoVersion) {
		t.Errorf("od starog zapisa nije smjelo ostati ništa: %+v %v", l, err)
	}
	if l, err := rec.Latest(ctx, "stations", "novi"); err != nil || !l.Archived {
		t.Errorf("novi zapis ima živu verziju mlađu od roka, pa mu spomenik ostaje: %v", err)
	}
	for _, zeli := range []string{"sažeo knjigu uz rok od 90 dana i obrisao 1 stariju verziju", "obrisao 1 spomenik obrisanog zapisa"} {
		if !strings.Contains(dnevnik.String(), zeli) {
			t.Errorf("dnevnik programa nema %q:\n%s", zeli, dnevnik.String())
		}
	}
}
