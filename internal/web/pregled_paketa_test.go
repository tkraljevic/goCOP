package web

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/peers"
)

// pregledajPaket šalje paket na Održavanje baze i vraća odgovor
func pregledajPaket(t *testing.T, h *DBMaintHandler, paket []byte) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	fw, err := mw.CreateFormFile("archive", "ocitanja-bp16.cop")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(paket); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/administracija/baza/uvoz", &b)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.RemoteAddr, r.Host = adresaLokalno, adresaCvoraTesta
	r = r.WithContext(context.WithValue(context.WithValue(r.Context(), contextKeyPerms, &models.UserPermissions{IsGlobalAdmin: true}), contextKeyUser, adminIvic))
	w := httptest.NewRecorder()
	h.HandleImport(w, r)
	return w
}

// odluciOPaketu šalje „Ugradi” ili „Odustani” za paket koji čeka
func odluciOPaketu(t *testing.T, h *DBMaintHandler, paket, radnja string) string {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/administracija/baza/uvoz/ugradi", strings.NewReader(url.Values{"paket": {paket}, "radnja": {radnja}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr, r.Host = adresaLokalno, adresaCvoraTesta
	r = r.WithContext(context.WithValue(r.Context(), contextKeyPerms, &models.UserPermissions{IsGlobalAdmin: true}))
	w := httptest.NewRecorder()
	h.HandleUgradiPaket(w, r)
	return mustUnescape(w.Header().Get("Location"))
}

var poljePaketa = regexp.MustCompile(`name="paket" value="([^"]+)"`)

// Paket se prvo pregleda: stranica kaže što nosi i što bi donio, a knjiga se
// ne mijenja dok se ne pritisne Ugradi; Odustani i Ugradi brišu paket koji
// je čekao (Odluke (18. 11. 2026.), Administracija, novo ponašanje 9)
func TestPregledPaketaPrijeUgradnje(t *testing.T) {
	ctx := context.Background()
	mapaIzdavaca := t.TempDir()
	izdavac, upisi, _ := cvorZaUvozCopa(t, "laptop-izvan", mapaIzdavaca)
	upisi("r-1")
	var paket bytes.Buffer
	if _, err := izdavac.IzveziCop(ctx, peers.CopObuhvat{Vrsta: "ocitanja", AreaID: 16}, []string{"ocitanja/16/2026"}, false, &paket); err != nil {
		t.Fatal(err)
	}
	kljuc, err := os.ReadFile(filepath.Join(mapaIzdavaca, peers.KeyFileName))
	if err != nil {
		t.Fatal(err)
	}
	mapa := t.TempDir()
	if err := os.WriteFile(filepath.Join(mapa, peers.KeyFileName), kljuc, 0o600); err != nil {
		t.Fatal(err)
	}
	ps, _, h := cvorZaUvozCopa(t, "laptop-izvan", mapa)
	ps.SetWantsAll(true)
	h.SetPregledPaketa(predlozakStranice(t, "baza_paket.html"))

	w := pregledajPaket(t, h, paket.Bytes())
	tijelo := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(tijelo, "Pregled paketa") || !strings.Contains(tijelo, "<strong>1</strong> još nema ovdje") || !strings.Contains(tijelo, "ocitanja/16/2026") {
		t.Fatalf("pregled: %d\n%.600s", w.Code, tijelo)
	}
	ime := poljePaketa.FindStringSubmatch(tijelo)
	if ime == nil {
		t.Fatal("pregled nema paketa za ugradnju")
	}
	if loc := odluciOPaketu(t, h, ime[1], "ugradi"); !strings.Contains(loc, "success=Ugrađeno izdanje 1") {
		t.Fatalf("ugradnja nakon pregleda: %s", loc)
	}
	if _, err := os.Stat(putPaketa(ime[1])); !os.IsNotExist(err) {
		t.Error("ugrađen paket ostao u privremenoj mapi")
	}

	// isti paket opet: ništa novo, pa Odustani
	tijelo = pregledajPaket(t, h, paket.Bytes()).Body.String()
	if !strings.Contains(tijelo, "nijedan nije nov") {
		t.Errorf("drugi pregled: %.600s", tijelo)
	}
	ime = poljePaketa.FindStringSubmatch(tijelo)
	if loc := odluciOPaketu(t, h, ime[1], "odustani"); !strings.Contains(loc, "success=Paket nije ugrađen") {
		t.Errorf("odustani: %s", loc)
	}
	if loc := odluciOPaketu(t, h, ime[1], "ugradi"); !strings.Contains(loc, "više ne čeka ugradnju") {
		t.Errorf("odbačen paket se ugrađuje: %s", loc)
	}
	if loc := odluciOPaketu(t, h, "../gocop.db", "ugradi"); !strings.Contains(loc, "više ne čeka ugradnju") {
		t.Errorf("ime izvan privremene mape: %s", loc)
	}

	// paket drugog računala: pregled kaže zašto, bez gumba Ugradi
	_, _, tudji := cvorZaUvozCopa(t, "novi-laptop", t.TempDir())
	tudji.SetPregledPaketa(predlozakStranice(t, "baza_paket.html"))
	tijelo = pregledajPaket(t, tudji, paket.Bytes()).Body.String()
	if !strings.Contains(tijelo, `id="paket-odbija"`) || strings.Contains(tijelo, `name="paket"`) {
		t.Errorf("paket drugog računala: %.600s", tijelo)
	}

	// pokvaren paket ne dolazi do pregleda
	pokvaren := append([]byte("PK"), bytes.Repeat([]byte{0}, 64)...)
	if loc := mustUnescape(pregledajPaket(t, h, pokvaren).Header().Get("Location")); !strings.Contains(loc, "error=Paket se ne da pročitati") {
		t.Errorf("pokvaren paket: %s", loc)
	}
}

func TestPutPaketa(t *testing.T) {
	for ime, dobar := range map[string]bool{
		"gocop-1763466000-paket.cop": true, "gocop-12-a b.cop": true,
		"": false, "gocop-x-paket.cop": false, "../gocop-1-x": false, "gocop-1-a/b": false, "gocop-1-..": false, "drugo-1-x": false,
	} {
		if (putPaketa(ime) != "") != dobar {
			t.Errorf("%q: %q", ime, putPaketa(ime))
		}
	}
}

// Ugradnju nakon pregleda smije samo administrator, a računalo izvan mreže
// paket ugrađuje samo iz lokalne mreže
func TestUgradnjaPaketaPrava(t *testing.T) {
	_, _, h := cvorZaUvozCopa(t, "ured", t.TempDir())
	r := httptest.NewRequest(http.MethodPost, "/administracija/baza/uvoz/ugradi", nil)
	r = r.WithContext(context.WithValue(r.Context(), contextKeyPerms, &models.UserPermissions{}))
	w := httptest.NewRecorder()
	h.HandleUgradiPaket(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("bez administratora: %d", w.Code)
	}
	r = httptest.NewRequest(http.MethodPost, "/administracija/baza/uvoz/ugradi", nil)
	r.RemoteAddr = "198.51.100.7:40000"
	r = r.WithContext(context.WithValue(r.Context(), contextKeyPerms, &models.UserPermissions{IsGlobalAdmin: true}))
	w = httptest.NewRecorder()
	h.HandleUgradiPaket(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("izvana, bez mreže: %d", w.Code)
	}
}

// Paket koji je čekao dulje od dana briše se pri idućem odabiru paketa
func TestOcistiStarePakete(t *testing.T) {
	star := filepath.Join(os.TempDir(), "gocop-1-star-test.cop")
	svjez := filepath.Join(os.TempDir(), "gocop-2-svjez-test.cop")
	for _, p := range []string{star, svjez} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(p) }) // samo testne datoteke
	}
	davno := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(star, davno, davno); err != nil {
		t.Fatal(err)
	}
	ocistiStarePakete()
	if _, err := os.Stat(star); !os.IsNotExist(err) {
		t.Error("star paket nije obrisan")
	}
	if _, err := os.Stat(svjez); err != nil {
		t.Error("svjež paket je obrisan")
	}
}
