package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// „Sažmi knjigu” u Održavanju baze čuva ovjeru akta i kad je u knjizi
// poslije nje nacrt s drugog čvora: na površini akta stoji ovjera, pa je
// obnova površine pri pokretanju ne smije izgubiti (Odluke (8. 10. 2026.),
// pitanje 4). Prvi nacrt i dalje se briše, a pregled prije radnje broji isto.
func TestSazimanjeKnjigeURukovateljuCuvaOvjeruAkta(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "baza.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "cop-osijek")
	ctx := context.Background()
	akt := models.Akt{ID: "akt-1", Sektor: "B", Godina: 2026, Status: models.AktNacrt}
	if _, err := rec.Record(ctx, baza, repository.EntityAkti, akt.ID, akt); err != nil {
		t.Fatal(err)
	}
	ovjeren := akt
	ovjeren.Status, ovjeren.Broj, ovjeren.OvjeraKod = models.AktOvjeren, 1, "806A"
	ovjera, err := rec.Record(ctx, baza, repository.EntityAkti, akt.ID, ovjeren)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Record(ctx, baza, repository.EntityAkti, "akt-2", models.Akt{ID: "akt-2", Status: models.AktNacrt}); err != nil {
		t.Fatal(err)
	}
	nacrt, err := json.Marshal(akt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Apply(ctx, []ledger.Version{{VersionID: uuid.Must(uuid.NewV7()).String(), Entity: repository.EntityAkti,
		EntityID: akt.ID, NodeID: "posluzitelj", Payload: nacrt, CreatedAt: time.Now().UTC(), SchemaVersion: 1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`UPDATE record_versions SET created_at = ?`, time.Now().AddDate(0, 0, -200)); err != nil {
		t.Fatal(err)
	}

	h := NewDBMaintHandler(func() *sql.DB { return baza }, rec, nil, nil, nil)
	r := httptest.NewRequest(http.MethodPost, "/administracija/baza/sazmi", strings.NewReader(url.Values{"days": {"90"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = r.WithContext(context.WithValue(r.Context(), contextKeyPerms, &models.UserPermissions{IsGlobalAdmin: true}))
	if n, err := repository.ZaSazimanje(ctx, rec, time.Now().AddDate(0, 0, -90)); err != nil || n != 1 {
		t.Fatalf("pregled prije sažimanja: %d verzija, %v", n, err)
	}
	w := httptest.NewRecorder()
	h.HandleCompact(w, r)
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "success") || !strings.Contains(loc, "Obrisano%3A+1+starija+verzija") {
		t.Fatalf("sažimanje briše samo prvi nacrt: %d %s", w.Code, loc)
	}
	povijest, err := rec.History(ctx, repository.EntityAkti, akt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(povijest) != 2 || povijest[1].VersionID != ovjera {
		t.Errorf("ovjera akta ostaje u knjizi: %+v", povijest)
	}
}
