package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// copProba je proba dnevnika usluga s dnevnikom COP-a sektora P i rutama
// zapisa, dežurstva i obračuna
type copProba struct {
	*uslugeProba
	cop *models.Journal
}

func novaCOPProba(t *testing.T) *copProba {
	t.Helper()
	p := &copProba{uslugeProba: novaUslugeProba(t)}
	h := p.h
	p.mux.HandleFunc("POST /dnevnici/{id}/zapisi", h.HandleAddCOPEntry)
	p.mux.HandleFunc("POST /dnevnici/{id}/dezurstvo/preuzmi", h.HandlePreuzmiDezurstvo)
	p.mux.HandleFunc("GET /dnevnici/{id}/dezurstva", h.ShowDezurstva)
	p.mux.HandleFunc("POST /dnevnici/{id}/dezurstva", h.HandleSaveDezurstvo)
	p.mux.HandleFunc("GET /dnevnici/{id}/obracun", h.ShowObracun)
	p.mux.HandleFunc("GET /dnevnici/{id}/obracun.xlsx", h.IzvoziObracun)
	p.mux.HandleFunc("GET /dnevnici/{id}/obracun/{user}", h.ShowIORS)
	p.mux.HandleFunc("GET /dnevnici/{id}/obracun/{user}/iors.xlsx", h.IzvoziIORS)
	pocetak := time.Date(2026, 10, 1, 0, 0, 0, 0, models.Zagreb)
	p.cop = &models.Journal{Kind: models.JournalKindDefense, CentarSektor: "P", Title: "Dnevnik COP-a, 2026.", Year: 2026, StartedAt: &pocetak}
	if err := p.journalRepo.SaveJournal(context.Background(), p.cop); err != nil {
		t.Fatal(err)
	}
	return p
}

// salji šalje obrazac u ime osobe; bez obrasca je obični zahtjev
func (p *copProba) salji(t *testing.T, u *models.User, metoda, putanja string, obrazac url.Values) *httptest.ResponseRecorder {
	t.Helper()
	cijeli, err := p.users.GetUserByID(u.ID)
	if err != nil || cijeli == nil {
		t.Fatalf("%s: %v", u.FullName, err)
	}
	r := httptest.NewRequest(metoda, putanja, nil)
	if obrazac != nil {
		r = httptest.NewRequest(metoda, putanja, strings.NewReader(obrazac.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	c := context.WithValue(context.WithValue(r.Context(), contextKeyUser, cijeli), contextKeyPerms, models.NewUserPermissions(*cijeli))
	w := httptest.NewRecorder()
	p.mux.ServeHTTP(w, r.WithContext(c))
	return w
}

// ishod je „error” ili „success” iz preusmjeravanja nakon obrasca
func ishod(w *httptest.ResponseRecorder) string {
	l := w.Header().Get("Location")
	switch {
	case w.Code != http.StatusSeeOther:
		return "kod " + http.StatusText(w.Code)
	case strings.Contains(l, "error="):
		return "error"
	case strings.Contains(l, "success="):
		return "success"
	}
	return l
}

// Poslovođa izvođača s dužnošću na području sektora P ne otvara dnevnik
// COP-a tog sektora ni plan dežurstava, ne upisuje zapis, ne preuzima
// dežurstvo, ne upisuje se u plan i ne vidi obračun ni tuđi IORS; svoj IORS
// vidi. Na popisu dokumentacije nema kartica dnevnika COP-a i plana.
// Dežurni operater i vodočuvar s dužnošću na dionici u sektoru pišu i
// upisuju se kao i dosad i vide svoj IORS, a obračun i tuđe sate vidi
// voditelj centra. Podaci su izmišljeni.
func TestIzvodjacKrozRuteDnevnikaCOPa(t *testing.T) {
	p := novaCOPProba(t)
	poslovodja := p.poslovodja(t, "mmarkic", "Marko Markić", "Primjer d.o.o.")
	operater := p.osoba(t, "ddezurni", "Dora Dežurna", "", models.Duty{Title: "Dežurni operater", Role: models.RoleOperator, ScopeType: models.ScopeSector})
	voditelj := p.osoba(t, "vvoditelj", "Vid Voditelj", "", models.Duty{Title: "Voditelj COP-a", Role: models.RoleCopLeader, ScopeType: models.ScopeSector})
	prvo := 1
	vodocuvar := p.osoba(t, "vvodocuvar", "Vlado Vodočuvar", "", models.Duty{Title: "Vodočuvar", Role: models.RoleWaterGuard, ScopeType: models.ScopeSection, AreaID: &prvo, SectionCodes: "P.1.1"})
	put := "/dnevnici/" + p.cop.ID
	zapis := url.Values{"date": {"2026-10-02"}, "time": {"10:00"}, "kind": {models.EntryKindReport}, "text": {"dojava s nasipa"}}
	plan := func(u *models.User) url.Values {
		return url.Values{"user_id": {u.ID.String()}, "od_date": {"2026-10-02"}, "od_time": {"08:00"}, "do_time": {"16:00"}, "opis": {models.OpisiRada[0].Opis}}
	}
	obracun := []string{put + "/obracun", put + "/obracun.xlsx", put + "/obracun/" + operater.ID.String(), put + "/obracun/" + operater.ID.String() + "/iors.xlsx"}

	// poslovođa: dnevnik COP-a i plan su mu zatvoreni
	for _, x := range []struct {
		metoda, putanja string
		obrazac         url.Values
	}{
		{http.MethodGet, put, nil},
		{http.MethodGet, put + "/dezurstva", nil},
		{http.MethodGet, "/dnevnici/popis?vrsta=" + models.JournalKindDefense, nil},
		{http.MethodGet, "/dnevnici/popis?vrsta=" + models.PopisDezurstava, nil},
		{http.MethodPost, put + "/zapisi", zapis},
		{http.MethodPost, put + "/dezurstvo/preuzmi", url.Values{}},
		{http.MethodPost, put + "/dezurstva", plan(poslovodja)},
	} {
		if w := p.salji(t, poslovodja, x.metoda, x.putanja, x.obrazac); w.Code != http.StatusForbidden {
			t.Errorf("poslovođa %s %s: %d", x.metoda, x.putanja, w.Code)
		}
	}
	for _, putanja := range obracun {
		if w := p.salji(t, poslovodja, http.MethodGet, putanja, nil); w.Code != http.StatusForbidden {
			t.Errorf("poslovođa na %s: %d", putanja, w.Code)
		}
	}
	if j, err := p.journalRepo.GetJournal(context.Background(), p.cop.ID); err != nil || j == nil || j.DezurniID != "" {
		t.Errorf("poslovođa je postao dežurni: %+v", j)
	}
	if zapisi, err := p.journalRepo.EntriesForJournal(context.Background(), p.cop.ID); err != nil || len(zapisi) != 0 {
		t.Errorf("dnevnik COP-a ima %d zapisa poslovođe", len(zapisi))
	}
	for _, putanja := range []string{put + "/obracun/" + poslovodja.ID.String(), put + "/obracun/" + poslovodja.ID.String() + "/iors.xlsx"} {
		// izvođača plaća njegova firma, pa svoj IORS nema
		if w := p.salji(t, poslovodja, http.MethodGet, putanja, nil); w.Code != http.StatusForbidden {
			t.Errorf("poslovođa na svom IORS-u %s: %d", putanja, w.Code)
		}
	}
	if w := p.salji(t, poslovodja, http.MethodGet, "/dnevnici", nil); w.Code != http.StatusOK || strings.Contains(w.Body.String(), "vrsta="+models.JournalKindDefense) {
		t.Errorf("popis dokumentacije nudi poslovođi dnevnik COP-a: %d", w.Code)
	}

	// dežurni operater: piše, preuzima, upisuje sebe, vidi svoj IORS, a
	// obračun i tuđe sate ne vidi
	if got := ishod(p.salji(t, operater, http.MethodPost, put+"/zapisi", zapis)); got != "success" {
		t.Errorf("operater ne upisuje zapis: %s", got)
	}
	if got := ishod(p.salji(t, operater, http.MethodPost, put+"/dezurstva", plan(operater))); got != "success" {
		t.Errorf("operater se ne upisuje u plan: %s", got)
	}
	if got := ishod(p.salji(t, operater, http.MethodPost, put+"/dezurstvo/preuzmi", url.Values{})); got != "success" {
		t.Errorf("operater ne preuzima dežurstvo: %s", got)
	}
	if w := p.salji(t, operater, http.MethodGet, put+"/obracun/"+operater.ID.String(), nil); w.Code != http.StatusOK || strings.Contains(w.Body.String(), `/obracun?od=`) {
		t.Errorf("operater na svom IORS-u: %d (ili mu nudi obračun)", w.Code)
	}
	if w := p.salji(t, operater, http.MethodGet, put+"/obracun/"+operater.ID.String()+"/iors.xlsx", nil); w.Code != http.StatusOK {
		t.Errorf("operater na svom IORS-u u Excelu: %d", w.Code)
	}
	for _, putanja := range []string{put + "/obracun", put + "/obracun.xlsx", put + "/obracun/" + voditelj.ID.String()} {
		if w := p.salji(t, operater, http.MethodGet, putanja, nil); w.Code != http.StatusForbidden {
			t.Errorf("operater na %s: %d", putanja, w.Code)
		}
	}
	if w := p.salji(t, operater, http.MethodGet, put+"/dezurstva", nil); w.Code != http.StatusOK || strings.Contains(w.Body.String(), put+`/obracun"`) {
		t.Errorf("plan dežurstava nudi operateru obračun: %d", w.Code)
	}
	if w := p.salji(t, operater, http.MethodGet, "/dnevnici", nil); !strings.Contains(w.Body.String(), "vrsta="+models.JournalKindDefense) {
		t.Error("popis dokumentacije ne nudi operateru dnevnik COP-a")
	}

	// vodočuvar s dužnošću na dionici u sektoru: piše i upisuje sebe
	if got := ishod(p.salji(t, vodocuvar, http.MethodPost, put+"/zapisi", zapis)); got != "success" {
		t.Errorf("vodočuvar ne upisuje zapis: %s", got)
	}
	if got := ishod(p.salji(t, vodocuvar, http.MethodPost, put+"/dezurstva", plan(vodocuvar))); got != "success" {
		t.Errorf("vodočuvar se ne upisuje u plan: %s", got)
	}

	// voditelj centra vidi sve
	for _, putanja := range append(obracun, put+"/obracun/"+uuid.New().String()) {
		if w := p.salji(t, voditelj, http.MethodGet, putanja, nil); w.Code != http.StatusOK {
			t.Errorf("voditelj centra na %s: %d", putanja, w.Code)
		}
	}
	if w := p.salji(t, voditelj, http.MethodGet, put+"/dezurstva", nil); !strings.Contains(w.Body.String(), put+`/obracun"`) {
		t.Error("plan dežurstava ne nudi voditelju obračun")
	}
}
