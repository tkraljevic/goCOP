package web

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"gocop/internal/models"
)

// Obrazac prijave koji se ne da pročitati ne broji se kao pokušaj i ne
// otvara ništa: vraća se prijava s porukom.
func TestPrijavaNeispravanObrazac(t *testing.T) {
	o := novaOkolinaPrijave(t)
	o.racun("ana", "anina-lozinka", false, false)
	r := obicniObrazac("/login", "username=%zz")
	w := httptest.NewRecorder()
	o.srv.ServeHTTP(w, izravno(r))
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "Neispravan zahtjev|") || kolacicSesije(w) != nil {
		t.Errorf("neispravan obrazac: %d %q", w.Code, w.Body.String())
	}
}

// Promjena lozinke bez prijave vodi na prijavu; neispravan obrazac, ključ
// koji se ne da prekljucati i promjena koja ne uspije vraćaju na profil s
// greškom, a lozinka ostaje stara.
func TestPromjenaLozinkeGreske(t *testing.T) {
	o := novaOkolinaPrijave(t)
	ana := o.racun("ana", "anina-lozinka", false, false)
	ova, _, err := o.auth.Login("ana", "anina-lozinka", "192.168.1.50", "laptop")
	if err != nil {
		t.Fatal(err)
	}
	zovi := func(u *models.User, tijelo string) string {
		r := obicniObrazac("/profile/change-password", tijelo)
		ctx := context.WithValue(r.Context(), contextKeySession, ova.ID)
		if u != nil {
			ctx = context.WithValue(ctx, contextKeyUser, u)
		}
		w := httptest.NewRecorder()
		o.authH.HandleChangePassword(w, r.WithContext(ctx))
		if w.Code != http.StatusSeeOther {
			t.Fatalf("očekivano preusmjerenje, dobiveno %d", w.Code)
		}
		loc, _ := url.QueryUnescape(w.Header().Get("Location"))
		return loc
	}
	nova := url.Values{"current_password": {"anina-lozinka"}, "new_password": {"nova-lozinka"}, "confirm_password": {"nova-lozinka"}}.Encode()
	if loc := zovi(nil, nova); loc != "/login" {
		t.Errorf("bez prijave: %q", loc)
	}
	if loc := zovi(ana, "current_password=%zz"); loc != "/?error=Neispravan zahtjev" {
		t.Errorf("neispravan obrazac: %q", loc)
	}
	o.authH.SetPrekljucaj(func(context.Context, string, string, string) error { return errors.New("ključ je oštećen") })
	if loc := zovi(ana, nova); loc != "/?error=potpisni ključ se ne da prekljucati: ključ je oštećen" {
		t.Errorf("ključ se ne da prekljucati: %q", loc)
	}
	// Ključ se prekljuca, a promjena lozinke zatim padne: baza se zatvori
	// između ta dva koraka.
	o.authH.SetPrekljucaj(func(context.Context, string, string, string) error { return o.baza.Close() })
	if loc := zovi(ana, nova); !strings.HasPrefix(loc, "/?error=") || strings.Contains(loc, "success") {
		t.Errorf("neuspjela promjena: %q", loc)
	}
}

// Prijava izvana: upis koji nije ni PIN ni kod javlja oblik i ostavlja
// prijavu na čekanju; prijava na čekanju za osobu čiji se zapis ne da
// pročitati prekida se greškom, a kad se prijava na čekanju ne da ni
// zapisati, javlja se da prijava izvana trenutačno ne radi, bez kolačića.
func TestPINGreskePrijaveNaCekanju(t *testing.T) {
	o := novaOkolinaPIN(t)
	o.osoba("ana", "anina-lozinka", "ana.anic@voda.hr", false)
	tunel := krozTunel("198.51.100.7")

	c := naCekanju(t, o.prijava(tunel, "ana", "anina-lozinka"))
	w := o.upisi(tunel, c, "nije-kod", false)
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "Upišite PIN od 6 znamenki ili kod oblika R3-XXXX-XXXX ili P-XXXX-XXXX.|") ||
		kolacic(w, imeKolacicaPrijave) != nil || kolacicSesije(w) != nil {
		t.Errorf("kod krivog oblika: %d %q", w.Code, w.Body.String())
	}

	// Obrazac PIN-a koji se ne da pročitati
	r := obicniObrazac("/login/pin", "kod=%zz")
	r.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	w = httptest.NewRecorder()
	o.srv.ServeHTTP(w, tunel(r))
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "Neispravan zahtjev|") {
		t.Errorf("neispravan obrazac PIN-a: %d %q", w.Code, w.Body.String())
	}

	// Zapis osobe prijave na čekanju više se ne da pročitati.
	if _, err := o.baza.Exec(`UPDATE users SET is_active = 'nije' WHERE username = 'ana'`); err != nil {
		t.Fatal(err)
	}
	w = o.upisi(tunel, c, "123456", false)
	if w.Code != http.StatusInternalServerError || !strings.HasPrefix(w.Body.String(), "Prijava nije uspjela. Pokušajte ponovno ili se prijavite iz ureda.|") ||
		kolacicSesije(w) != nil {
		t.Errorf("osoba koje više nema: %d %q", w.Code, w.Body.String())
	}

	// Prijava na čekanju ne da se zapisati.
	o.osoba("marko", "markova-lozinka", "marko.markic@voda.hr", false)
	if _, err := o.baza.Exec(`DROP TABLE prijave_na_cekanju`); err != nil {
		t.Fatal(err)
	}
	w = o.prijava(tunel, "marko", "markova-lozinka")
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Prijava izvana trenutačno ne radi") ||
		kolacic(w, imeKolacicaPrijave) != nil || kolacicSesije(w) != nil {
		t.Errorf("prijava na čekanju se ne da zapisati: %d %q", w.Code, w.Body.String())
	}
}

// Stranica PIN-a koja se ne da iscrtati ne ruši odgovor: ne sprema se i
// ostaje prazna.
func TestStranicaPINBezPredloska(t *testing.T) {
	h := &AuthHandler{tmplPIN: template.Must(template.New("prazan").Parse(""))}
	w := httptest.NewRecorder()
	h.prikaziPIN(w, http.StatusOK, PINPageData{Error: "Krivi PIN."})
	if w.Code != http.StatusOK || w.Body.Len() != 0 || w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("stranica bez predloška: %d %q %v", w.Code, w.Body.String(), w.Header())
	}
}

// Skriveni modul ne otvara se ni izravnom adresom, a kad se vidljivost
// modula ne da pročitati, zahtjev ne prolazi do rukovatelja.
func TestProvjeraPrijaveModuli(t *testing.T) {
	o := novaOkolinaPrijave(t)
	o.osoba("pperic", "perina-lozinka", "pperic@primjer.test", false)
	sesija, _, err := o.auth.Login("pperic", "perina-lozinka", "192.168.1.50", "test")
	if err != nil {
		t.Fatal(err)
	}
	var z mwZapis
	h := mwPosluzitelj(o, &z)
	w := mwZahtjev(h, http.MethodGet, "/administracija/uvoz-niza", sesija.ID.String())
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "nije uključen za vaš račun") || z.pozvan {
		t.Errorf("skriveni modul: %d %q, rukovatelj pozvan %v", w.Code, w.Body.String(), z.pozvan)
	}
	if w := mwZahtjev(h, http.MethodGet, "/", sesija.ID.String()); w.Code != http.StatusNoContent || !z.pozvan {
		t.Errorf("početna stranica: %d", w.Code)
	}
	z = mwZapis{}
	if _, err := o.baza.Exec(`DROP TABLE role_modules`); err != nil {
		t.Fatal(err)
	}
	w = mwZahtjev(h, http.MethodGet, "/", sesija.ID.String())
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Greška pri čitanju vidljivosti modula") || z.pozvan {
		t.Errorf("vidljivost se ne čita: %d %q", w.Code, w.Body.String())
	}
}

// Deset krivih unosa osobe u satu zaključava kodove i kad svaki dolazi s
// druge adrese, pa ga brojač adrese ne bi zaustavio: odgovor je 429 i sesije
// nema.
func TestPINKodoviZakljucaniNakonDesetKrivih(t *testing.T) {
	o := novaOkolinaPIN(t)
	o.osoba("ana", "anina-lozinka", "ana.anic@voda.hr", false)
	adresa := 0
	sljedeca := func() func(*http.Request) *http.Request {
		adresa++
		return krozTunel("198.51.100." + strconv.Itoa(adresa))
	}
	var w *httptest.ResponseRecorder
	for _, krivih := range []int{4, 4, 2} {
		c := naCekanju(t, o.prijava(sljedeca(), "ana", "anina-lozinka"))
		pin := o.posta.pin(t)
		for i := 0; i < krivih; i++ {
			w = o.upisi(sljedeca(), c, krivPIN(pin), false)
		}
	}
	if w.Code != http.StatusTooManyRequests || kolacicSesije(w) != nil ||
		!strings.HasPrefix(w.Body.String(), "Previše krivih kodova u zadnjih sat vremena; pokušajte ponovno za sat.|") {
		t.Errorf("deseti krivi unos: %d %q", w.Code, w.Body.String())
	}
}

// Kad se računalo ne da zapamtiti, prijava ipak prolazi: točan PIN otvara
// sesiju, a kolačića zapamćenog računala nema.
func TestPINRacunaloSeNeDaZapamtiti(t *testing.T) {
	o := novaOkolinaPIN(t)
	o.osoba("ana", "anina-lozinka", "ana.anic@voda.hr", false)
	tunel := krozTunel("198.51.100.7")
	c := naCekanju(t, o.prijava(tunel, "ana", "anina-lozinka"))
	if _, err := o.baza.Exec(`DROP TABLE zapamcena_racunala`); err != nil {
		t.Fatal(err)
	}
	w := o.upisi(tunel, c, o.posta.pin(t), true)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" || kolacicSesije(w) == nil || kolacic(w, imeKolacicaRacunala) != nil {
		t.Errorf("prijava bez zapamćenog računala: %d %q, kolačić računala %v", w.Code, w.Header().Get("Location"), kolacic(w, imeKolacicaRacunala))
	}
}
