package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Primljeni nalog kroz rute (Odluke (22. 11. 2026.)): izvođač u obrani na
// listu svog dnevnika A.02 upisuje primljeni nalog s dionicom, vremenom i
// davateljem s popisa; davatelj na listu nalog potvrđuje ili javlja da
// nije tako rečeno. Okolina je sektor P s područjima 1 i 2; podaci su
// izmišljeni.
func TestPrimljeniNalogKrozRute(t *testing.T) {
	p, j, list := listUObrani(t)
	p.mux.HandleFunc("POST /dnevnici/{id}/upisi/{entry}/odgovor", p.h.HandleOdgovorNaNalog)
	drugo := 2
	dionicar := p.osoba(t, "iivic", "Ivo Ivić", "", models.Duty{Title: "Rukovoditelj dionice P.2.1", Role: models.RoleSectionLeader, ScopeType: models.ScopeSection, AreaID: &drugo})
	poslovodja := p.poslovodja(t, "mmarkic", "Marko Markić", "Primjer d.o.o.")
	js := p.h.journals
	js.SetObrana(obranaNaDionici("P.2.1"), dioniceSektoraP)
	js.SetOsobeSektora(func(sektor string) ([]models.User, error) { return p.users.ListUsers(sektor, 0, "", "", "") })
	listPut := "/dnevnici/" + j.ID + "/listovi/" + list.ID

	stranica := p.salji(t, poslovodja, http.MethodGet, listPut, nil).Body.String()
	for _, treba := range []string{`<option value="NALOG">Primljeni nalog</option>`, `id="primljeni-nalog"`, `value="` + dionicar.ID.String() + `">Ivo Ivić · Rukovoditelj dionice`,
		`<option value="P.2.1">`} {
		if !strings.Contains(stranica, treba) {
			t.Errorf("list izvođaču u obrani nema %q", treba)
		}
	}
	if strings.Contains(stranica, `<option value="P.1.1">`) {
		t.Error("list nudi dionicu područja bez obrane")
	}
	obrazac := url.Values{"kind": {models.EntryKindTask}, "section_code": {"P.2.1"}, "text": {"postaviti vreće na nasip"},
		"davatelj_id": {dionicar.ID.String()}, "kada": {"2026-11-22T14:30"}}
	if got := ishod(p.salji(t, poslovodja, http.MethodPost, listPut+"/upisi", obrazac)); got != "success" {
		t.Fatalf("izvođač ne upisuje primljeni nalog: %s", got)
	}
	upisi, err := p.journalRepo.EntriesForSheet(context.Background(), list.ID)
	if err != nil || len(upisi) != 1 || !upisi[0].Primljen() || upisi[0].Davatelj != "Ivo Ivić" || upisi[0].HappenedAt == nil ||
		!upisi[0].HappenedAt.Equal(time.Date(2026, 11, 22, 14, 30, 0, 0, models.Zagreb)) {
		t.Fatalf("primljeni nalog: %+v, %v", upisi, err)
	}
	nalog := upisi[0]
	if stranica = p.salji(t, poslovodja, http.MethodGet, listPut, nil).Body.String(); !strings.Contains(stranica, "čeka potvrdu davatelja") ||
		strings.Contains(stranica, "/odgovor") {
		t.Error("izvođaču list ne kaže da nalog čeka potvrdu ili mu nudi odgovor")
	}

	// davatelj vidi list i odgovara
	stranica = p.salji(t, dionicar, http.MethodGet, listPut, nil).Body.String()
	odgovor := "/dnevnici/" + j.ID + "/upisi/" + nalog.ID + "/odgovor"
	if !strings.Contains(stranica, odgovor) || !strings.Contains(stranica, "Ivo Ivić, rukovoditelj dionice") {
		t.Error("davatelju list ne nudi odgovor ili ne kaže tko je dao nalog")
	}
	if got := ishod(p.salji(t, poslovodja, http.MethodPost, odgovor, url.Values{"odgovor": {models.OdgovorPotvrden}, "sheet": {list.ID}})); got != "error" {
		t.Errorf("izvođač potvrđuje nalog umjesto davatelja: %s", got)
	}
	if got := ishod(p.salji(t, dionicar, http.MethodPost, odgovor, url.Values{"odgovor": {models.OdgovorNijeTako}, "napomena": {"rekao sam P.2.2"}, "sheet": {list.ID}})); got != "success" {
		t.Fatalf("davatelj ne odgovara: %s", got)
	}
	if stranica = p.salji(t, poslovodja, http.MethodGet, listPut, nil).Body.String(); !strings.Contains(stranica, "rekao sam P.2.2") {
		t.Error("list ne pokazuje odgovor davatelja")
	}
}

// Odgovor na nalog bez lista vraća na dnevnik, nepoznat dnevnik nije
// pronađen, a neispravan obrazac se ne upisuje
func TestOdgovorNaNalogRubovi(t *testing.T) {
	p, j, _ := listUObrani(t)
	p.mux.HandleFunc("POST /dnevnici/{id}/upisi/{entry}/odgovor", p.h.HandleOdgovorNaNalog)
	osoba := p.osoba(t, "iivic", "Ivo Ivić", "", models.Duty{Title: "Rukovoditelj sektora", Role: models.RoleSectorLeader, ScopeType: models.ScopeSector})
	w := p.salji(t, osoba, http.MethodPost, "/dnevnici/"+j.ID+"/upisi/nema/odgovor", url.Values{"odgovor": {models.OdgovorPotvrden}})
	if ishod(w) != "error" || !strings.HasPrefix(w.Header().Get("Location"), "/dnevnici/"+j.ID+"?") {
		t.Errorf("odgovor bez lista: %s %s", ishod(w), w.Header().Get("Location"))
	}
	if w := p.salji(t, osoba, http.MethodPost, "/dnevnici/nema/upisi/nema/odgovor", url.Values{}); w.Code != http.StatusNotFound {
		t.Errorf("nepoznat dnevnik: %d", w.Code)
	}
	cijeli, err := p.users.GetUserByID(osoba.ID)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/dnevnici/"+j.ID+"/upisi/nema/odgovor", strings.NewReader("odgovor=%zz"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c := context.WithValue(context.WithValue(r.Context(), contextKeyUser, cijeli), contextKeyPerms, models.NewUserPermissions(*cijeli))
	rw := httptest.NewRecorder()
	p.mux.ServeHTTP(rw, r.WithContext(c))
	if ishod(rw) != "error" {
		t.Errorf("neispravan obrazac: %s", ishod(rw))
	}
}
