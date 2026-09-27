package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/oborine"
	"gocop/internal/prognoza"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Stvarni kišomjer stoji u istom registru kao izvedene točke, ali nema
// težinu, mora imati izvor i ne ulazi ni u preuzimanje s Open-Meteo ni u
// oborinu sliva.
func TestStvarniKisomjer(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "stvarni.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	svc := service.NewKisomjerService(repository.NewKisomjerRepository(baza, ledger.New(baza, "test-node")))
	h := NewSlivoviHandler(func() *service.KisomjerService { return svc }, nil, nil, nil, nil)
	ctx := context.Background()
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	posalji := func(tijelo string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/slivovi/kisomjer/create", strings.NewReader(tijelo))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req = req.WithContext(context.WithValue(ctx, contextKeyPerms, admin))
		w := httptest.NewRecorder()
		h.HandleCreate(w, req)
		return w
	}

	// izvedena točka bez vrste u obrascu ostaje izvedena
	if w := posalji("naziv=C+ravnica&code=k-c-ravnica&sliv=C&lat=46,4&lon=16,1&tezina=0,4&aktivan=1"); w.Code != http.StatusSeeOther ||
		!strings.Contains(w.Header().Get("Location"), "success=") {
		t.Fatalf("izvedena: %d %q", w.Code, w.Header().Get("Location"))
	}
	// stvarni bez izvora se ne upisuje
	if w := posalji("naziv=Varaždin&code=dhmz-varazdin&vrsta=stvarni&sliv=C&lat=46,283&lon=16,364&aktivan=1"); !strings.Contains(w.Header().Get("Location"), "error=") {
		t.Fatalf("stvarni bez izvora prošao: %d %q", w.Code, w.Header().Get("Location"))
	}
	// stvarni s izvorom; težina i površina se odbacuju
	if w := posalji("naziv=Varaždin&code=dhmz-varazdin&vrsta=stvarni&izvor=DHMZ&izvor_sifra=239&korak=satni&sliv=C&lat=46,283&lon=16,364&tezina=0,5&km2=100&aktivan=1"); !strings.Contains(w.Header().Get("Location"), "success=") {
		t.Fatalf("stvarni: %d %q", w.Code, w.Header().Get("Location"))
	}
	if w := posalji("naziv=Loš&code=los&vrsta=stvarni&izvor=dhmz&korak=tjedni&lat=46&lon=16"); !strings.Contains(w.Header().Get("Location"), "error=") {
		t.Errorf("nepoznat korak prošao: %q", w.Header().Get("Location"))
	}

	izv, _ := svc.GetKisomjer(ctx, "k-c-ravnica")
	stv, _ := svc.GetKisomjer(ctx, "dhmz-varazdin")
	if izv == nil || izv.Vrsta != models.KisomjerIzvedeni || izv.JeStvarni() {
		t.Errorf("izvedena: %+v", izv)
	}
	if stv == nil || !stv.JeStvarni() || stv.Izvor != "dhmz" || stv.IzvorSifra != "239" || stv.Korak != "satni" ||
		stv.Tezina != nil || stv.Km2 != nil {
		t.Errorf("stvarni: %+v", stv)
	}

	// model i Open-Meteo vide samo izvedenu točku
	ot, err := prognoza.OborinskeTocke(baza)
	if err != nil || len(ot) != 1 || ot[0].Code != "k-c-ravnica" {
		t.Errorf("OborinskeTocke = %+v, %v", ot, err)
	}
	tocke, err := oborine.TockeIzRegistra(baza)()
	if err != nil || len(tocke) != 1 || tocke[0].Code != "k-c-ravnica" {
		t.Errorf("TockeIzRegistra = %+v, %v", tocke, err)
	}
}
