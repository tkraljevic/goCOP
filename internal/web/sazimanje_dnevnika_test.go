package web

import (
	"context"
	"database/sql"
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
	"gocop/internal/repository"
)

// Sažimanje knjige iz Administracije › Održavanje baze ne vraća nacrt preko
// predanog dnevnog lista, Odluke (8. 10. 2026.), pitanje 7. Vodočuvar Ivo
// Ivić preda list na laptopu; rukovoditeljica Ana Anić na poslužitelju upiše
// bilješku na nacrt istog dana, koji je zadnja verzija. Nakon sažimanja i
// obnove površine iz knjige list na poslužitelju je i dalje predan, s
// bilješkom.
func TestSazimanjeKnjigeCuvaPredanList(t *testing.T) {
	cvor := func(ime string) (*sql.DB, *ledger.Recorder) {
		t.Helper()
		baza, err := db.OpenDB(filepath.Join(t.TempDir(), ime+".db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { baza.Close() })
		if err := db.InitSchema(baza); err != nil {
			t.Fatal(err)
		}
		return baza, ledger.New(baza, ime)
	}
	nuzno := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	laptop, recL := cvor("laptop")
	posluzitelj, recP := cvor("posluzitelj")
	ivo := "01900000-0000-7000-8000-0000000017a1"
	dan := time.Date(2026, time.October, 14, 0, 0, 0, 0, models.Zagreb)
	list := func(d time.Time, opis string) *models.VodocuvarskiList {
		return &models.VodocuvarskiList{UserID: ivo, Ime: "Ivo Ivić", Sektor: "B", AreaID: 16, Datum: d, Broj: 41, Od: "07:00", Do: "15:00", Opis: opis}
	}

	predan := list(dan, "obilazak nasipa Batina")
	kad := time.Now()
	predan.PredanoAt = &kad
	repoL := repository.NewVodocuvarRepository(laptop, recL)
	nuzno(repoL.Predaj(ctx, predan, nil))
	// laptop zatim piše i sutrašnji list, pa predan list nije njegova zadnja
	// verzija, koju sažimanje čuva zbog granice razmjene
	nuzno(repoL.Save(ctx, list(dan.AddDate(0, 0, 1), "sutradan")))
	nacrt := list(dan, "")
	nacrt.Upisi = []models.UpisRukovoditelja{{UserID: "01900000-0000-7000-8000-0000000017a2", Ime: "Ana Anić", Kad: kad, Tekst: "pojačati noćnu ophodnju"}}
	nuzno(repository.NewVodocuvarRepository(posluzitelj, recP).Save(ctx, nacrt))

	verzije, err := recL.Since(ctx, "", 0)
	nuzno(err)
	_, err = recP.Apply(ctx, verzije)
	nuzno(err)
	nuzno(repository.ApplyVersions(ctx, posluzitelj, recP, verzije))
	_, err = posluzitelj.Exec(`UPDATE record_versions SET created_at = ?`, time.Now().UTC().AddDate(0, 0, -200))
	nuzno(err)

	h := &DBMaintHandler{db: func() *sql.DB { return posluzitelj }, rec: recP}
	r := httptest.NewRequest(http.MethodPost, "/administracija/baza/sazmi", strings.NewReader(url.Values{"days": {"90"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	w := httptest.NewRecorder()
	h.HandleCompact(w, r.WithContext(context.WithValue(r.Context(), contextKeyPerms, admin)))
	if lok := w.Header().Get("Location"); !strings.Contains(lok, "success=") {
		t.Fatalf("sažimanje nije uspjelo: %s", lok)
	}

	_, err = repository.ReplaySurface(ctx, posluzitelj, recP)
	nuzno(err)
	l, err := repository.NewVodocuvarRepository(posluzitelj, recP).ZaDan(ctx, ivo, dan)
	nuzno(err)
	if l == nil || !l.Predan() || l.Opis != "obilazak nasipa Batina" || len(l.Upisi) != 1 {
		t.Errorf("sažimanje je vratilo nacrt preko predanog lista: %+v", l)
	}
}
