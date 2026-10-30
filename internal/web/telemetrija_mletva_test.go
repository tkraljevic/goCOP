package web

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/hidroview"
	"gocop/internal/mletva"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Račun e-pošte je adresa, a mletva.voda.hr traži samo korisničko ime domene.
func TestKorisnikDomene(t *testing.T) {
	for ulaz, treba := range map[string]string{
		"pero.peric@voda.hr": "pero.peric",
		" pero ":             "pero",
		"@voda.hr":           "@voda.hr",
	} {
		if got := KorisnikDomene(ulaz); got != treba {
			t.Errorf("%q → %q, a treba %q", ulaz, got, treba)
		}
	}
}

// telemetrijaZaTest sastavlja stranicu Telemetrija nad praznom bazom, s
// upisanim računom HydroViewa i mletve.
func telemetrijaZaTest(t *testing.T) (*TelemetrijaHandler, *repository.HidroViewRepository, *repository.RacuniSustavaRepository, *sql.DB) {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "telemetrija.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	hv := repository.NewHidroViewRepository(baza)
	sr := repository.NewRacuniSustavaRepository(baza)
	ctx := context.Background()
	if err := hv.Spremi(ctx, &repository.RacunHidroView{Korisnik: "geolux.cop", Lozinka: []byte("z")}); err != nil {
		t.Fatal(err)
	}
	if err := sr.Spremi(ctx, &repository.RacunSustava{Sustav: mletva.Podrijetlo, Korisnik: "pero.peric", Lozinka: []byte("z")}); err != nil {
		t.Fatal(err)
	}
	h := &TelemetrijaHandler{
		racuni:  func() *repository.HidroViewRepository { return hv },
		sustavi: func() *repository.RacuniSustavaRepository { return sr },
		kljuc:   func() []byte { return hvKljuc },
	}
	return h, hv, sr, baza
}

// racunHV čita upisani račun HydroViewa; greška čitanja ruši test.
func racunHV(t *testing.T, hv *repository.HidroViewRepository) *repository.RacunHidroView {
	t.Helper()
	x, err := hv.Racun(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	return x
}

// racunMLetve čita upisani račun mletve; greška čitanja ruši test.
func racunMLetve(t *testing.T, sr *repository.RacuniSustavaRepository) *repository.RacunSustava {
	t.Helper()
	x, err := sr.Racun(context.Background(), mletva.Podrijetlo)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

// posaljiTelemetriji šalje obrazac i vraća poruku iz preusmjerenja.
func posaljiTelemetriji(t *testing.T, rukovatelj http.HandlerFunc, polja url.Values) (vrsta, poruka string) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/administracija/telemetrija", strings.NewReader(polja.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	rukovatelj(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("odgovor %d, a treba preusmjerenje", w.Code)
	}
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if p := u.Query().Get("error"); p != "" {
		return "error", p
	}
	return "success", u.Query().Get("success")
}

// Prazno korisničko ime prije je brisalo račun, pa su letve tiho prestajale
// preuzimati. Sad se spremanje odbija, a račun briše samo gumb „Obriši račun”.
func TestTelemetrijaPraznoImeNeBriseRacun(t *testing.T) {
	h, hv, sr, _ := telemetrijaZaTest(t)

	vrsta, poruka := posaljiTelemetriji(t, h.Spremi, url.Values{"hidroview_korisnik": {"  "}})
	if vrsta != "error" || !strings.Contains(poruka, "korisničko ime je prazno") {
		t.Errorf("prazno ime HydroViewa: %s %q", vrsta, poruka)
	}
	if x := racunHV(t, hv); x == nil {
		t.Fatal("prazno ime obrisalo je račun HydroViewa")
	}
	vrsta, poruka = posaljiTelemetriji(t, h.SpremiMLetvu, url.Values{"mletva_korisnik": {""}})
	if vrsta != "error" || !strings.Contains(poruka, "korisničko ime je prazno") {
		t.Errorf("prazno ime mletve: %s %q", vrsta, poruka)
	}
	if x := racunMLetve(t, sr); x == nil {
		t.Fatal("prazno ime obrisalo je račun mletve")
	}

	vrsta, poruka = posaljiTelemetriji(t, h.Spremi, url.Values{"radnja": {"obrisi"}, "hidroview_korisnik": {"geolux.cop"}})
	if vrsta != "success" || !strings.Contains(poruka, "Račun za HydroView je obrisan") {
		t.Errorf("brisanje HydroViewa: %s %q", vrsta, poruka)
	}
	if x := racunHV(t, hv); x != nil {
		t.Error("gumb nije obrisao račun HydroViewa")
	}
	vrsta, poruka = posaljiTelemetriji(t, h.SpremiMLetvu, url.Values{"radnja": {"obrisi"}})
	if vrsta != "success" || !strings.Contains(poruka, "Račun za mobilnu stranicu Hrvatskih voda je obrisan") {
		t.Errorf("brisanje mletve: %s %q", vrsta, poruka)
	}
	if x := racunMLetve(t, sr); x != nil {
		t.Error("gumb nije obrisao račun mletve")
	}
}

// Bez lozinke se mijenja samo ime upisanog računa; bez računa se traži lozinka.
func TestTelemetrijaSpremiBezLozinke(t *testing.T) {
	h, hv, _, baza := telemetrijaZaTest(t)
	ctx := context.Background()
	vrsta, _ := posaljiTelemetriji(t, h.Spremi, url.Values{"hidroview_korisnik": {"novo.ime"}, "hidroview_adresa": {"http://127.0.0.1:1"}})
	if x := racunHV(t, hv); vrsta != "success" || x == nil || x.Korisnik != "novo.ime" || string(x.Lozinka) != "z" || x.Adresa != hidroview.ZadanaAdresa {
		t.Errorf("promjena imena (adresa iz obrasca se ne sprema): %s %+v", vrsta, x)
	}
	// baza koja ne da upisati: poruka kaže zašto
	if _, err := baza.ExecContext(ctx, `CREATE TRIGGER zakljucano BEFORE UPDATE ON hidroview_racuni BEGIN SELECT RAISE(ABORT, 'baza je zaključana'); END`); err != nil {
		t.Fatal(err)
	}
	if vrsta, poruka := posaljiTelemetriji(t, h.Spremi, url.Values{"hidroview_korisnik": {"trece.ime"}}); vrsta != "error" || !strings.Contains(poruka, "zaključana") {
		t.Errorf("neuspjelo spremanje: %s %q", vrsta, poruka)
	}
	if err := hv.Obrisi(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if vrsta, poruka := posaljiTelemetriji(t, h.Spremi, url.Values{"hidroview_korisnik": {"novo.ime"}}); vrsta != "error" || poruka != "Upišite i lozinku." {
		t.Errorf("bez računa i lozinke: %s %q", vrsta, poruka)
	}
	if vrsta, poruka := posaljiTelemetriji(t, h.SpremiMLetvu, url.Values{"mletva_korisnik": {"pero"}}); vrsta != "error" || poruka != "Upišite i lozinku." {
		t.Errorf("mletva bez lozinke: %s %q", vrsta, poruka)
	}
}

// „Uzmi moj račun e-pošte” bez računa e-pošte javlja zašto ne ide.
func TestTelemetrijaRacunIzPoste(t *testing.T) {
	h, _, _, _ := telemetrijaZaTest(t)
	zahtjev := func(korisnik *models.User) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("iz_poste=1"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if korisnik != nil {
			r = r.WithContext(context.WithValue(r.Context(), contextKeyUser, korisnik))
		}
		return r
	}
	if _, _, greska := h.racunZaMLetvu(zahtjev(nil)); greska != "Račun e-pošte nije dostupan." {
		t.Errorf("bez prijavljenog: %q", greska)
	}
	h.racunPoste = func(context.Context, *models.User) (string, string, error) { return "", "", errors.New("nema lozinke") }
	if _, _, greska := h.racunZaMLetvu(zahtjev(&models.User{})); !strings.Contains(greska, "nema lozinke") {
		t.Errorf("greška računa e-pošte: %q", greska)
	}
	h.racunPoste = func(context.Context, *models.User) (string, string, error) { return "pero.peric@voda.hr", "tajna", nil }
	if k, l, greska := h.racunZaMLetvu(zahtjev(&models.User{})); k != "pero.peric" || l != "tajna" || greska != "" {
		t.Errorf("račun e-pošte: %q %q %q", k, l, greska)
	}
}

// Lozinka se ne sprema bez ključa čvora; s ključem se sprema zaključana.
func TestTelemetrijaZakljucaj(t *testing.T) {
	h, _, _, _ := telemetrijaZaTest(t)
	if z, greska := h.zakljucaj("tajna"); greska != "" || len(z) == 0 || strings.Contains(string(z), "tajna") {
		t.Errorf("zaključavanje: %q %q", z, greska)
	}
	h.kljuc = func() []byte { return nil }
	if _, greska := h.zakljucaj("tajna"); !strings.Contains(greska, "Ključ čvora") {
		t.Errorf("bez ključa: %q", greska)
	}
	h.kljuc = func() []byte { return []byte("prekratak") }
	if _, greska := h.zakljucaj("tajna"); greska == "" {
		t.Error("prekratak ključ je prošao")
	}
}

// Nova lozinka sprema se tek kad prijava na HydroView prođe. Prijava ide
// samo na sustav HydroViewa, a ne na adresu iz obrasca (namjerna izmjena
// testa: lažni sustav se zadaje rukovatelju, ne obrascem).
func TestTelemetrijaSpremiProvjeriPrijavu(t *testing.T) {
	h, hv, _, _ := telemetrijaZaTest(t)
	srv := lazniHydroViewVrata(t)
	h.hvAdresa = srv.URL
	polja := func(korisnik string) url.Values {
		return url.Values{"hidroview_korisnik": {korisnik}, "hidroview_lozinka": {"tajna"}, "hidroview_adresa": {"http://127.0.0.1:1"}}
	}
	if vrsta, poruka := posaljiTelemetriji(t, h.Spremi, polja("odbijen")); vrsta != "error" || !strings.Contains(poruka, "nije prošla") {
		t.Errorf("odbijena prijava: %s %q", vrsta, poruka)
	}
	if x := racunHV(t, hv); x == nil || x.Korisnik != "geolux.cop" {
		t.Errorf("odbijena prijava promijenila je račun: %+v", x)
	}
	if vrsta, poruka := posaljiTelemetriji(t, h.Spremi, polja("novi.racun")); vrsta != "success" {
		t.Errorf("prijava: %s %q", vrsta, poruka)
	}
	if x := racunHV(t, hv); x == nil || x.Korisnik != "novi.racun" || x.Adresa != srv.URL {
		t.Errorf("spremljeni račun: %+v", x)
	}
	h.kljuc = func() []byte { return nil }
	if vrsta, poruka := posaljiTelemetriji(t, h.Spremi, polja("treci.racun")); vrsta != "error" || !strings.Contains(poruka, "Ključ čvora") {
		t.Errorf("bez ključa: %s %q", vrsta, poruka)
	}
}

// Kad brisanje ne uspije, poruka kaže koji račun nije obrisan.
func TestObrisiRacunGreska(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	w := httptest.NewRecorder()
	obrisiRacun(w, r, "Račun za proba", func() error { return errors.New("baza zaključana") })
	if lok := w.Header().Get("Location"); !strings.Contains(lok, "error=") || !strings.Contains(lok, "nije+obrisan") {
		t.Errorf("preusmjerenje: %q", lok)
	}
}
