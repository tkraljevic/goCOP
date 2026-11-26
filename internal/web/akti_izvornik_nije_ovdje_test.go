package web

import (
	"context"
	"database/sql"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/posta"
	"gocop/internal/repository"
	"gocop/internal/sadrzaj"
	"gocop/internal/service"
)

// probnoOnemoguceno je gumb "Probno samo meni" kad se akt ne može poslati
const probnoOnemoguceno = `name="proba" value="1" formnovalidate disabled>`

// pokvariPDF stavlja mapu na mjesto datoteke PDF-a u spremištu: zapis i
// redak postoje, a PDF se ne da pročitati
func pokvariPDF(t *testing.T, put, otisak string) {
	t.Helper()
	nadjeni, err := filepath.Glob(filepath.Join(sadrzaj.MapaUz(put), "pdf", "*", otisak+".pdf"))
	if err != nil || len(nadjeni) != 1 {
		t.Fatalf("datoteka PDF-a %s: %v, %v", otisak, nadjeni, err)
	}
	if err := os.Remove(nadjeni[0]); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(nadjeni[0], 0o755); err != nil {
		t.Fatal(err)
	}
}

// slanjeAktaBezIzvornika: ovjeren akt (ovdje ručno potpisan, sa skenom kao
// izvornikom) čiji je zapis izvornika stigao razmjenom, a PDF još nije.
// Stranica to kaže i ne nudi ni PDF ni slanje, PDF akta se ne crta iznova
// pod imenom izvornika, a slanje se odbije prije slanja i prije upisa, i
// probno. Kad PDF stigne pod istim otiskom, sve radi s njim; akt bez
// zapisa izvornika crta se kao i prije.
func slanjeAktaBezIzvornika(t *testing.T, ctx context.Context, baza *sql.DB, repo *repository.AktiRepository, id string,
	srv *posta.ProbniPosluzitelj, zovi func(*http.Request) *httptest.ResponseRecorder, stranica func(string) string) {
	t.Helper()
	iz, err := repo.GetIzvornik(ctx, id)
	if err != nil || iz == nil || len(iz.PDF) == 0 {
		t.Fatalf("akt nema izvornik ovdje: %+v, %v", iz, err)
	}
	sken, otisak := iz.PDF, iz.Otisak
	if sadrzaj.Otisak(sken) != otisak {
		t.Fatalf("otisak izvornika %s nije otisak skena", otisak)
	}
	// ovo računalo dobije novo, prazno spremište: zapis izvornika ostaje, a
	// PDF-a nema, kao kad je zapis stigao razmjenom prije PDF-a
	put := filepath.Join(t.TempDir(), "sadrzaj.db")
	sp, err := sadrzaj.Otvori(put)
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
	posalji := func(v url.Values) string {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/akti/"+id+"/posalji", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return greskaPreusmjerenja(t, zovi(r))
	}
	pdfAkta := func() *httptest.ResponseRecorder {
		return zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id+"/akt.pdf", nil))
	}
	poslano := len(srv.Poruke())
	slanja, err := repo.ListSlanja(ctx, id)
	if err != nil {
		t.Fatal(err)
	}

	s := stranica(id)
	for _, x := range []string{"Izvornik akta još nije stigao na ovo računalo.", "sken s potpisom i žigom", "Ne može se poslati dok izvornik akta ne stigne",
		"(izvornik, još nije stigao na ovo računalo)", `id="akt-posalji" disabled>`, probnoOnemoguceno} {
		if !strings.Contains(s, x) {
			t.Errorf("stranica akta kojemu izvornik nije ovdje nema %q", x)
		}
	}
	for _, x := range []string{"/akt.pdf", "Otvori izvornik", "Otvori PDF"} {
		if strings.Contains(s, x) {
			t.Errorf("stranica akta kojemu izvornik nije ovdje ima %q", x)
		}
	}
	w := pdfAkta()
	if w.Code != http.StatusSeeOther || strings.HasPrefix(w.Body.String(), "%PDF") || greskaPreusmjerenja(t, w) != porukaPDFAktaNijeOvdje {
		t.Errorf("PDF akta kojemu izvornik nije ovdje: %d, greška %q", w.Code, greskaPreusmjerenja(t, w))
	}
	if cd := w.Header().Get("Content-Disposition"); cd != "" {
		t.Errorf("odbijen PDF ima ime datoteke %q", cd)
	}
	for _, v := range []url.Values{{"adresa": {"osjecko-baranjska@policija.hr"}, "kopija": {"1"}}, {"proba": {"1"}}} {
		if g := posalji(v); g != service.ErrSlanjeBezIzvornika.Error() {
			t.Errorf("slanje %v bez izvornika ovdje: %q", v, g)
		}
	}
	if n := len(srv.Poruke()); n != poslano {
		t.Errorf("bez izvornika ovdje poslano je još %d poruka", n-poslano)
	}
	if sl, err := repo.ListSlanja(ctx, id); err != nil || len(sl) != len(slanja) {
		t.Errorf("odbijeno slanje je upisano: %d zapisa, prije %d (%v)", len(sl), len(slanja), err)
	}
	if nakon, err := repo.GetIzvornik(ctx, id); err != nil || nakon == nil || nakon.Otisak != otisak || nakon.Sazetak != iz.Sazetak {
		t.Errorf("izvornik je promijenjen: %+v, %v", nakon, err)
	}

	// izvornik se ne da pročitati: PDF se ne crta, akt se ne šalje
	if _, err := baza.Exec(`ALTER TABLE akti_izvornici RENAME TO akti_izvornici_skriveni`); err != nil {
		t.Fatal(err)
	}
	if w := pdfAkta(); w.Code != http.StatusInternalServerError || strings.HasPrefix(w.Body.String(), "%PDF") {
		t.Errorf("PDF akta kad se izvornik ne da pročitati: %d", w.Code)
	}
	if g := posalji(url.Values{"proba": {"1"}}); !strings.HasPrefix(g, "akt nije poslan: izvornik akta nije se dao pročitati") {
		t.Errorf("slanje kad se izvornik ne da pročitati: %q", g)
	}
	if _, err := baza.Exec(`ALTER TABLE akti_izvornici_skriveni RENAME TO akti_izvornici`); err != nil {
		t.Fatal(err)
	}
	if n := len(srv.Poruke()); n != poslano {
		t.Errorf("izvornik se nije dao pročitati, a poslano je još %d poruka", n-poslano)
	}

	// PDF stigne razmjenom pod istim otiskom: otvara se i šalje baš on
	if err := sp.UpisiProvjereno(ctx, otisak, "application/pdf", sken, "razmjena"); err != nil {
		t.Fatal(err)
	}
	if s := stranica(id); strings.Contains(s, "još nije stigao") || !strings.Contains(s, "Otvori izvornik") || !strings.Contains(s, "/akti/"+id+"/akt.pdf") ||
		strings.Contains(s, `id="akt-posalji" disabled>`) || strings.Contains(s, probnoOnemoguceno) {
		t.Error("akt s izvornikom ovdje: stranica kaže da izvornik nije ovdje, ne nudi PDF ili ne da poslati")
	}
	if w := pdfAkta(); w.Code != http.StatusOK || w.Body.String() != string(sken) {
		t.Errorf("PDF akta s izvornikom ovdje: %d, nije sken", w.Code)
	}
	if g := posalji(url.Values{"proba": {"1"}}); g != "" {
		t.Errorf("probno slanje s izvornikom ovdje: %q", g)
	}
	prim := srv.Poruke()
	if len(prim) != poslano+1 || !strings.Contains(prim[len(prim)-1].Podaci, base64.StdEncoding.EncodeToString(sken)) {
		t.Error("probno slanje s izvornikom ovdje nije poslalo sken")
	}

	// PDF je ovdje, a ne da se pročitati: stranica ne kaže da nije stigao,
	// PDF se ne crta iznova, a akt se ne šalje
	pokvariPDF(t, put, otisak)
	if s := stranica(id); strings.Contains(s, "još nije stigao") {
		t.Error("izvornik koji se ne da pročitati: stranica kaže da još nije stigao")
	}
	if w := pdfAkta(); w.Code != http.StatusInternalServerError || strings.HasPrefix(w.Body.String(), "%PDF") {
		t.Errorf("PDF akta kad se PDF izvornika ne da pročitati: %d", w.Code)
	}
	if g := posalji(url.Values{"proba": {"1"}}); !strings.HasPrefix(g, "akt nije poslan: izvornik akta nije se dao pročitati") {
		t.Errorf("slanje kad se PDF izvornika ne da pročitati: %q", g)
	}
	if n := len(srv.Poruke()); n != poslano+1 {
		t.Errorf("PDF izvornika se nije dao pročitati, a poslano je još %d poruka", n-poslano-1)
	}

	// kontrola: akt bez ikakvog zapisa izvornika crta se iznova, kao i prije
	if _, err := baza.Exec(`ALTER TABLE akti_izvornici RENAME TO akti_izvornici_skriveni`); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`CREATE TABLE akti_izvornici AS SELECT * FROM akti_izvornici_skriveni WHERE 0`); err != nil {
		t.Fatal(err)
	}
	if w := pdfAkta(); w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "%PDF") || w.Body.String() == string(sken) {
		t.Errorf("akt bez zapisa izvornika: %d, nije nacrtan PDF", w.Code)
	}
	if s := stranica(id); strings.Contains(s, "još nije stigao") || !strings.Contains(s, "/akti/"+id+"/akt.pdf") {
		t.Error("akt bez zapisa izvornika: stranica kaže da izvornik nije ovdje ili ne nudi PDF")
	}
	for _, q := range []string{`DROP TABLE akti_izvornici`, `ALTER TABLE akti_izvornici_skriveni RENAME TO akti_izvornici`} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}
