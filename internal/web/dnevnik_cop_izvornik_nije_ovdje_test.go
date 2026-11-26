package web

import (
	"context"
	"html/template"
	"io/fs"
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
	"gocop/internal/sadrzaj"
	"gocop/internal/service"
	webassets "gocop/web"
)

// spremisteZaDnevnik postavlja novo, prazno spremište sadržaja, kao na
// drugom računalu, i vraća prijašnje na kraju testa
func spremisteZaDnevnik(t *testing.T) *sadrzaj.Spremiste {
	t.Helper()
	sp, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "sadrzaj.db"))
	if err != nil {
		t.Fatal(err)
	}
	prijasnje := repository.Spremiste()
	repository.SetSpremiste(sp)
	t.Cleanup(func() {
		repository.SetSpremiste(prijasnje)
		if err := sp.Zatvori(); err != nil {
			t.Error(err)
		}
	})
	return sp
}

// Dnevnik COP-a ovjeren na drugom računalu: zapis izvornika je stigao, a PDF
// još nije. Stranica to kaže bez crvene trake i bez poziva administratoru,
// PDF se ne nudi ni ne crta iznova, a izravan zahtjev za PDF vraća na
// stranicu dnevnika s porukom. Kad PDF stigne pod istim otiskom, daje se baš
// on. Neispravan i nestao izvornik javljaju se kao i prije.
func TestDnevnikCOPKojemuIzvornikNijeOvdje(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "izvornik.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := baza.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "cop-zagreb")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	journalRepo := repository.NewJournalRepository(baza, rec)
	journals := service.NewJournalService(journalRepo, nil, nil)
	journals.SetPovijestZaduzenja(users.PovijestZaduzenja)
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tmpl := func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	h := NewJournalsHandler(journals, users, nil, nil, nil,
		tmpl("dnevnici_izbor.html"), tmpl("dnevnici.html"), tmpl("dnevnik_form.html"), tmpl("dnevnik.html"),
		tmpl("dnevnik_cop.html"), tmpl("dnevnik_cop_form.html"), tmpl("dnevnik_list.html"), nil, tmpl("dnevnik_obracun.html"), tmpl("dnevnik_dezurstva.html"), tmpl("dnevnik_iors.html"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /dnevnici/{id}", h.ShowJournal)
	mux.HandleFunc("GET /dnevnici/{id}/dezurstva", h.ShowDezurstva)
	mux.HandleFunc("GET /dnevnici/{id}/obracun", h.ShowObracun)
	mux.HandleFunc("GET /dnevnici/{id}/dnevnik.pdf", h.IzvoziDnevnikPDF)

	voditelj := &models.User{ID: uuid.New(), FullName: "Iva Ivić"}
	uprava := &models.UserPermissions{AdminSectors: map[string]bool{"B": true}, AllowedSectors: map[string]bool{"B": true}, User: *voditelj}
	zovi := func(putanja string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, putanja, nil)
		ctx := context.WithValue(context.WithValue(r.Context(), contextKeyUser, voditelj), contextKeyPerms, uprava)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(ctx))
		return w
	}
	greska := func(w *httptest.ResponseRecorder) string {
		t.Helper()
		u, err := url.Parse(w.Header().Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		return u.Query().Get("error")
	}

	ctx := context.Background()
	dnevnik := func(godina int) *models.Journal {
		t.Helper()
		pocetak := time.Date(godina, 9, 10, 0, 0, 0, 0, models.Zagreb)
		kraj := pocetak.AddDate(0, 0, 3)
		j := &models.Journal{Kind: models.JournalKindDefense, CentarSektor: "B", Title: "Dnevnik COP-a", Year: godina, StartedAt: &pocetak, EndedAt: &kraj}
		if err := journalRepo.SaveJournal(ctx, j); err != nil {
			t.Fatal(err)
		}
		return j
	}

	// ovjera na drugom računalu: tamo je i PDF
	spremisteZaDnevnik(t)
	j := dnevnik(2026)
	kad := time.Date(2026, 9, 14, 8, 30, 0, 0, models.Zagreb)
	j.ZakljucioID, j.Zakljucio, j.ZakljucenoAt = voditelj.ID.String(), voditelj.FullName, &kad
	pdf := []byte("%PDF-1.4\npotpisani izvornik dnevnika")
	if err := journalRepo.SaveOvjeraCOP(ctx, j, pdf); err != nil {
		t.Fatal(err)
	}
	prije, err := journalRepo.IzvornikDnevnika(ctx, j.ID)
	if err != nil || prije == nil {
		t.Fatalf("izvornik nakon ovjere: %+v, %v", prije, err)
	}

	// ovo računalo: zapis izvornika je tu, PDF-a nema
	ovdje := spremisteZaDnevnik(t)
	poveznica := `href="/dnevnici/` + j.ID + `/dnevnik.pdf"`
	stranica := zovi("/dnevnici/" + j.ID).Body.String()
	if !strings.Contains(stranica, "Izvornik dnevnika, PDF kakav je ovjeren, nije na ovom računalu") {
		t.Error("stranica ne kaže da izvornik nije na ovom računalu")
	}
	for _, x := range []string{"nije ispravan", "Javite administratoru", "knjige verzija", poveznica} {
		if strings.Contains(stranica, x) {
			t.Errorf("stranica dnevnika kojemu izvornik nije ovdje ima %q", x)
		}
	}
	for _, kartica := range []string{"/dezurstva", "/obracun"} {
		if w := zovi("/dnevnici/" + j.ID + kartica); w.Code != http.StatusOK || strings.Contains(w.Body.String(), poveznica) {
			t.Errorf("kartica %s: %d, ili nudi PDF kojeg nema", kartica, w.Code)
		}
	}

	w := zovi("/dnevnici/" + j.ID + "/dnevnik.pdf")
	if w.Code != http.StatusSeeOther || strings.HasPrefix(w.Body.String(), "%PDF") || greska(w) != porukaDnevnikPDFNijeOvdje {
		t.Errorf("PDF dnevnika kojemu izvornik nije ovdje: %d, greška %q", w.Code, greska(w))
	}
	if cd := w.Header().Get("Content-Disposition"); cd != "" {
		t.Errorf("odbijen PDF ima ime datoteke %q", cd)
	}
	if s := zovi(w.Header().Get("Location")).Body.String(); !strings.Contains(s, porukaDnevnikPDFNijeOvdje) {
		t.Error("stranica dnevnika ne pokazuje zašto PDF nije otvoren")
	}
	if iz, err := journalRepo.IzvornikDnevnika(ctx, j.ID); err != nil || iz == nil || iz.Otisak != prije.Otisak || iz.Sazetak != prije.Sazetak || len(iz.PDF) != 0 {
		t.Errorf("izvornik je promijenjen: %+v, %v", iz, err)
	}

	// PDF stigne razmjenom pod istim otiskom: daje se baš on
	if err := ovdje.UpisiProvjereno(ctx, sadrzaj.Otisak(pdf), "application/pdf", pdf, "razmjena"); err != nil {
		t.Fatal(err)
	}
	if s := zovi("/dnevnici/" + j.ID).Body.String(); !strings.Contains(s, "Izvornik je ispravan") || !strings.Contains(s, poveznica) || strings.Contains(s, "nije na ovom računalu") {
		t.Error("dnevnik s izvornikom ovdje: nema PDF-a ili piše da izvornik nije ovdje")
	}
	if w := zovi("/dnevnici/" + j.ID + "/dnevnik.pdf"); w.Code != http.StatusOK || w.Body.String() != string(pdf) {
		t.Errorf("PDF dnevnika s izvornikom ovdje: %d, nije izvornik", w.Code)
	}
	for _, kartica := range []string{"/dezurstva", "/obracun"} {
		if w := zovi("/dnevnici/" + j.ID + kartica); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), poveznica) {
			t.Errorf("kartica %s: %d, ili ne nudi PDF koji je ovdje", kartica, w.Code)
		}
	}

	// spremište se ne da pročitati: stranica ne tvrdi ni da PDF nije ovdje
	// ni da izvornik nije ispravan, a PDF se ne crta iznova
	pokvareno, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "pokvareno.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := pokvareno.Zatvori(); err != nil {
		t.Fatal(err)
	}
	repository.SetSpremiste(pokvareno)
	stranica = zovi("/dnevnici/" + j.ID).Body.String()
	if !strings.Contains(stranica, "ali se izvornik dnevnika nije dao pročitati</strong>, pa se ovdje ne može otvoriti ni provjeriti. Pokušajte ponovno") {
		t.Error("nečitljiv izvornik: stranica to ne kaže")
	}
	for _, x := range []string{"nije na ovom računalu", "traži razmjenom", "nije ispravan", "knjige verzija", "database is closed"} {
		if strings.Contains(stranica, x) {
			t.Errorf("stranica s nečitljivim izvornikom ima %q", x)
		}
	}
	if w := zovi("/dnevnici/" + j.ID + "/dnevnik.pdf"); w.Code != http.StatusInternalServerError || strings.TrimSpace(w.Body.String()) != porukaDnevnikNijeProcitan {
		t.Errorf("PDF kad se spremište ne da pročitati: %d, %q", w.Code, w.Body.String())
	}
	repository.SetSpremiste(ovdje)

	// kontrola: neispravan izvornik javlja se kao i prije
	if _, err := baza.Exec(`UPDATE journal_izvornici SET sazetak = 'krivi' WHERE journal_id = ?`, j.ID); err != nil {
		t.Fatal(err)
	}
	if s := zovi("/dnevnici/" + j.ID).Body.String(); !strings.Contains(s, "ali izvornik nije ispravan:</strong> izvornik je mijenjan nakon ovjere") {
		t.Error("neispravan izvornik: stranica to ne kaže")
	}
	if w := zovi("/dnevnici/" + j.ID + "/dnevnik.pdf"); w.Code != http.StatusConflict || strings.HasPrefix(w.Body.String(), "%PDF") {
		t.Errorf("PDF neispravnog izvornika: %d", w.Code)
	}
	// kontrola: bez zapisa izvornika kao i prije
	if _, err := baza.Exec(`DELETE FROM journal_izvornici WHERE journal_id = ?`, j.ID); err != nil {
		t.Fatal(err)
	}
	if s := zovi("/dnevnici/" + j.ID).Body.String(); !strings.Contains(s, "izvornik ovjerenog dnevnika nedostaje. Javite administratoru") {
		t.Error("izvornik bez zapisa: stranica to ne kaže")
	}
	if w := zovi("/dnevnici/" + j.ID + "/dnevnik.pdf"); w.Code != http.StatusConflict || strings.HasPrefix(w.Body.String(), "%PDF") {
		t.Errorf("PDF bez zapisa izvornika: %d", w.Code)
	}

	// kontrola: neovjeren dnevnik bez izvornika crta se, kao i prije
	neovjeren := dnevnik(2025)
	if w := zovi("/dnevnici/" + neovjeren.ID + "/dnevnik.pdf"); w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "%PDF") {
		t.Errorf("neovjeren dnevnik: %d, nije PDF", w.Code)
	}

	// izvornik se ne da pročitati: nema ni crteža ni gole greške baze
	if _, err := baza.Exec(`DROP TABLE journal_izvornici`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{j.ID, neovjeren.ID} {
		w := zovi("/dnevnici/" + id + "/dnevnik.pdf")
		if w.Code != http.StatusInternalServerError || strings.TrimSpace(w.Body.String()) != porukaDnevnikNijeProcitan {
			t.Errorf("PDF kad se izvornik ne da pročitati: %d, %q", w.Code, w.Body.String())
		}
	}
}
