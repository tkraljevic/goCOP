package web

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// tekstXLSX spaja tekst svih dijelova Excel datoteke, za traženje upisanog
func tekstXLSX(t *testing.T, sadrzaj []byte) string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(sadrzaj), int64(len(sadrzaj)))
	if err != nil {
		t.Fatalf("izvoz nije ZIP: %v", err)
	}
	var b strings.Builder
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatalf("dio %s: %v", f.Name, err)
		}
		s, err := io.ReadAll(r)
		if cerr := r.Close(); err != nil || cerr != nil {
			t.Fatalf("dio %s: %v %v", f.Name, err, cerr)
		}
		b.Write(s)
	}
	return b.String()
}

// brojNaKarticiPlana je broj na kartici „Plan dežurstava” razdjelnice
// /dnevnici: zadnja oznaka broja prije naslova kartice
func brojNaKarticiPlana(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	tijelo := w.Body.String()
	naslov := strings.Index(tijelo, `<h2 class="dash-card-title">Plan dežurstava</h2>`)
	if w.Code != http.StatusOK || naslov < 0 {
		t.Fatalf("razdjelnica bez kartice plana: %d", w.Code)
	}
	prije := tijelo[:naslov]
	oznaka := strings.LastIndex(prije, "dash-status-pill")
	if oznaka < 0 {
		t.Fatal("kartica plana bez broja")
	}
	_, broj, _ := strings.Cut(prije[oznaka:], ">")
	broj, _, _ = strings.Cut(broj, "<")
	return strings.TrimSpace(broj)
}

// Plan dežurstava centra sektora P, gumb do njega na dnevniku COP-a, kartica
// na popisu planova i stupac dežurstava u dnevnik.xlsx vide djelatnici
// sektora P i globalni administrator. Rukovoditelj područja drugog sektora
// dnevnik COP-a čita, ali plana ni dežurstava u izvozu nema (Odluke
// (30. 10. 2026.)). Podaci su izmišljeni.
func TestPlanDezurstavaVidiSektorCentra(t *testing.T) {
	p := novaCOPProba(t)
	p.mux.HandleFunc("GET /dnevnici/{id}/dnevnik.xlsx", p.h.IzvoziDnevnik)
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('D', 'Sektor D', 'VGO Drugovo', 'COP Drugovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (7, 'D', 'Drugovo', 'VGI Drugovo', 'Drugovo')`,
	} {
		if _, err := p.baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	operater := p.osoba(t, "ddezurni", "Dora Dežurna", "", models.Duty{Title: "Dežurni operater", Role: models.RoleOperator, ScopeType: models.ScopeSector})
	d, sedmo := "D", 7
	drugi := &models.User{ID: uuid.New(), Username: "rdrugi", FullName: "Rudi Drugi", IsActive: true}
	if err := p.userRepo.CreateUser(drugi, &models.Duty{Title: "Rukovoditelj BP", Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, SectorID: &d, AreaID: &sedmo, IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	put := "/dnevnici/" + p.cop.ID
	plan := url.Values{"user_id": {operater.ID.String()}, "od_date": {"2026-10-02"}, "od_time": {"08:00"}, "do_time": {"16:00"}, "opis": {models.OpisiRada[0].Opis}}
	if got := ishod(p.salji(t, operater, http.MethodPost, put+"/dezurstva", plan)); got != "success" {
		t.Fatalf("operater se ne upisuje u plan: %s", got)
	}
	razmak := "08:00–16:00"

	// drugi sektor: dnevnik čita, plana nema
	if w := p.salji(t, drugi, http.MethodGet, put+"/dezurstva", nil); w.Code != http.StatusForbidden {
		t.Errorf("rukovoditelj drugog sektora na planu: %d", w.Code)
	}
	if w := p.salji(t, drugi, http.MethodGet, put, nil); w.Code != http.StatusOK || strings.Contains(w.Body.String(), put+"/dezurstva") {
		t.Errorf("dnevnik COP-a rukovoditelju drugog sektora: %d (ili nudi plan)", w.Code)
	}
	if w := p.salji(t, drugi, http.MethodGet, "/dnevnici/popis?vrsta="+models.PopisDezurstava, nil); w.Code != http.StatusOK || strings.Contains(w.Body.String(), put+"/dezurstva") {
		t.Errorf("popis planova rukovoditelju drugog sektora: %d (ili nudi plan centra P)", w.Code)
	}
	w := p.salji(t, drugi, http.MethodGet, put+"/dnevnik.xlsx", nil)
	if w.Code != http.StatusOK || strings.Contains(tekstXLSX(t, w.Body.Bytes()), razmak) {
		t.Errorf("izvoz dnevnika rukovoditelju drugog sektora: %d (ili nosi dežurstva)", w.Code)
	}
	if got := brojNaKarticiPlana(t, p.salji(t, drugi, http.MethodGet, "/dnevnici", nil)); got != "0" {
		t.Errorf("kartica plana rukovoditelju drugog sektora broji %s dežurstava", got)
	}

	// kontrola: operater sektora P vidi plan, gumb, karticu i dežurstva u izvozu
	if w := p.salji(t, operater, http.MethodGet, put+"/dezurstva", nil); w.Code != http.StatusOK {
		t.Errorf("operater na planu: %d", w.Code)
	}
	if w := p.salji(t, operater, http.MethodGet, put, nil); !strings.Contains(w.Body.String(), put+"/dezurstva") {
		t.Error("dnevnik COP-a operateru ne nudi plan")
	}
	if w := p.salji(t, operater, http.MethodGet, "/dnevnici/popis?vrsta="+models.PopisDezurstava, nil); !strings.Contains(w.Body.String(), put+"/dezurstva") {
		t.Error("popis planova operateru ne nudi plan centra P")
	}
	w = p.salji(t, operater, http.MethodGet, put+"/dnevnik.xlsx", nil)
	if w.Code != http.StatusOK || !strings.Contains(tekstXLSX(t, w.Body.Bytes()), razmak) {
		t.Errorf("izvoz dnevnika operateru: %d (ili bez dežurstava)", w.Code)
	}
	if got := brojNaKarticiPlana(t, p.salji(t, operater, http.MethodGet, "/dnevnici", nil)); got != "1" {
		t.Errorf("kartica plana operateru broji %s dežurstava", got)
	}
	admin := &models.User{ID: uuid.New(), Username: "gadmin", FullName: "Goran Admin", IsActive: true, IsGlobalAdmin: true}
	if err := p.userRepo.CreateUser(admin, &models.Duty{Title: "Dežurni operater", Role: models.RoleOperator, ScopeType: models.ScopeSector, SectorID: &d, IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	if got := brojNaKarticiPlana(t, p.salji(t, admin, http.MethodGet, "/dnevnici", nil)); got != "1" {
		t.Errorf("kartica plana globalnom administratoru broji %s dežurstava", got)
	}
}

// Poslovođa izvođača koji je ostao dežurni ne predaje smjenu kroz rutu i ne
// upisuje zapis; smjenu predaje voditelj centra, a ona ne ulazi u plan
// (Odluke (30. 10. 2026.)). Rutu poslovođi zatvara već učitavanje dnevnika
// COP-a; test čuva da tako i ostane, a pravilo u servisu provjerava
// TestSmjenuIzvodjacaPredajeUpravaCentra. Podaci su izmišljeni.
func TestPredajaSmjeneIzvodjacaKrozRutu(t *testing.T) {
	p := novaCOPProba(t)
	p.mux.HandleFunc("POST /dnevnici/{id}/dezurstvo/predaj", p.h.HandlePredajDezurstvo)
	poslovodja := p.poslovodja(t, "mmarkic", "Marko Markić", "Primjer d.o.o.")
	voditelj := p.osoba(t, "vvoditelj", "Vid Voditelj", "", models.Duty{Title: "Voditelj COP-a", Role: models.RoleCopLeader, ScopeType: models.ScopeSector})
	od := time.Now().In(models.Zagreb).Add(-3 * time.Hour)
	p.cop.DezurniID, p.cop.DezurniIme, p.cop.DezurniOd = poslovodja.ID.String(), poslovodja.FullName, &od
	if err := p.journalRepo.SaveJournal(context.Background(), p.cop); err != nil {
		t.Fatal(err)
	}
	put := "/dnevnici/" + p.cop.ID + "/dezurstvo/predaj"
	if w := p.salji(t, poslovodja, http.MethodPost, put, url.Values{}); w.Code != http.StatusForbidden {
		t.Errorf("poslovođa predaje smjenu: %d", w.Code)
	}
	if zapisi, err := p.journalRepo.EntriesForJournal(context.Background(), p.cop.ID); err != nil || len(zapisi) != 0 {
		t.Errorf("predaja poslovođe upisala je %d zapisa: %v", len(zapisi), err)
	}
	// kontrola: voditelj centra predaje njegovu smjenu
	if got := ishod(p.salji(t, voditelj, http.MethodPost, put, url.Values{})); got != "success" {
		t.Errorf("voditelj centra ne predaje smjenu izvođača: %s", got)
	}
	zapisi, err := p.journalRepo.EntriesForJournal(context.Background(), p.cop.ID)
	if err != nil || len(zapisi) != 1 || zapisi[0].UserID != voditelj.ID.String() {
		t.Errorf("zapis predaje: %+v, %v", zapisi, err)
	}
	if plan, err := p.journalRepo.ListDezurstva(context.Background(), p.cop.ID); err != nil || len(plan) != 0 {
		t.Errorf("smjena poslovođe ušla je u plan: %+v, %v", plan, err)
	}
}
