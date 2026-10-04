package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Zaključava ponašanje authMiddleware kroz pravu provjeru sesije: što se
// događa bez kolačića, s isteklom sesijom, s isključenim računom, s
// obveznom promjenom lozinke i dok administrator gleda tuđim očima, te što
// rukovatelj dobiva u kontekstu.

type mwZapis struct {
	pozvan            bool
	korisnik, stvarni *models.User
	ovlasti           *models.UserPermissions
	gleda             bool
	sesija            uuid.UUID
}

func mwPosluzitelj(o *okolinaPrijave, z *mwZapis) http.Handler {
	moduli := service.NewModuleService(repository.NewModuleRepository(o.baza, ledger.New(o.baza, "test")))
	s := &Server{authService: o.auth, moduleService: moduli}
	return s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		z.pozvan = true
		z.korisnik, _ = r.Context().Value(contextKeyUser).(*models.User)
		z.stvarni, _ = r.Context().Value(contextKeyRealUsr).(*models.User)
		z.ovlasti, _ = r.Context().Value(contextKeyPerms).(*models.UserPermissions)
		z.gleda, _ = r.Context().Value(contextKeyViewing).(bool)
		z.sesija, _ = r.Context().Value(contextKeySession).(uuid.UUID)
		w.WriteHeader(http.StatusNoContent)
	}))
}

func mwZahtjev(h http.Handler, metoda, putanja, kolacic string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(metoda, putanja, nil)
	if kolacic != "" {
		r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: kolacic})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestProvjeraPrijavePrijeRukovatelja(t *testing.T) {
	o := novaOkolinaPrijave(t)
	pperic := o.osoba("pperic", "perina-lozinka", "pperic@example.com", false)
	sesija, _, err := o.auth.Login("pperic", "perina-lozinka", "192.168.1.50", "test")
	if err != nil {
		t.Fatal(err)
	}

	slucajevi := []struct {
		ime, kolacic string
		pripremi     func()
	}{
		{"bez kolačića", "", nil},
		{"kolačić nije UUID", "nije-uuid", nil},
		{"nepoznata sesija", uuid.NewString(), nil},
		{"istekla sesija", sesija.ID.String(), func() {
			if _, err := o.baza.Exec(`UPDATE sessions SET expires_at = '2000-01-01 00:00:00' WHERE id = ?`, sesija.ID.String()); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, s := range slucajevi {
		t.Run(s.ime, func(t *testing.T) {
			if s.pripremi != nil {
				s.pripremi()
			}
			z := &mwZapis{}
			w := mwZahtjev(mwPosluzitelj(o, z), http.MethodGet, "/dashboard", s.kolacic)
			if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" || z.pozvan {
				t.Errorf("%d → %q, rukovatelj pozvan: %v", w.Code, w.Header().Get("Location"), z.pozvan)
			}
		})
	}

	t.Run("isključen račun", func(t *testing.T) {
		nova, _, err := o.auth.Login("pperic", "perina-lozinka", "192.168.1.50", "test")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := o.baza.Exec(`UPDATE users SET is_active = 0 WHERE id = ?`, pperic.ID.String()); err != nil {
			t.Fatal(err)
		}
		z := &mwZapis{}
		w := mwZahtjev(mwPosluzitelj(o, z), http.MethodGet, "/dashboard", nova.ID.String())
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" || z.pozvan {
			t.Errorf("isključen račun: %d → %q", w.Code, w.Header().Get("Location"))
		}
		// sesija isključenog računa briše se, kao pri odjavi, i kolačić s njom
		if s, err := o.sessions.GetSession(nova.ID); err != nil || s != nil {
			t.Errorf("sesija isključenog računa ostala je: %v %v", s, err)
		}
		if k := w.Result().Cookies(); len(k) != 1 || k[0].Name != imeKolacicaSesije || k[0].MaxAge >= 0 {
			t.Errorf("kolačić sesije nije obrisan: %v", k)
		}
		// ponovno uključenje računa ne vraća staru prijavu
		if _, err := o.baza.Exec(`UPDATE users SET is_active = 1 WHERE id = ?`, pperic.ID.String()); err != nil {
			t.Fatal(err)
		}
		if w := mwZahtjev(mwPosluzitelj(o, &mwZapis{}), http.MethodGet, "/dashboard", nova.ID.String()); w.Code != http.StatusSeeOther {
			t.Errorf("stara sesija vrijedi nakon ponovnog uključenja: %d", w.Code)
		}
	})
}

func TestObveznaPromjenaLozinke(t *testing.T) {
	o := novaOkolinaPrijave(t)
	o.racun("pperic", "perina-lozinka", false, true)
	sesija, _, err := o.auth.Login("pperic", "perina-lozinka", "192.168.1.50", "test")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []struct {
		metoda, putanja string
		prolazi         bool
	}{
		{http.MethodGet, "/dashboard", false},
		{http.MethodPost, "/readings", false},
		// izlaz iz pregleda tuđim očima nije na popisu iznimki
		{http.MethodPost, "/view-as/stop", false},
		{http.MethodGet, "/profile", true},
		{http.MethodPost, "/profile/change-password", true},
		{http.MethodPost, "/logout", true},
		{http.MethodGet, "/static/css/style.css", true},
		{http.MethodGet, "/tema.css", true},
	} {
		z := &mwZapis{}
		w := mwZahtjev(mwPosluzitelj(o, z), s.metoda, s.putanja, sesija.ID.String())
		if s.prolazi {
			if !z.pozvan {
				t.Errorf("%s %s mora proći uz obveznu promjenu lozinke: %d → %q", s.metoda, s.putanja, w.Code, w.Header().Get("Location"))
			}
			continue
		}
		if z.pozvan || w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/profile?force=1#lozinka" {
			t.Errorf("%s %s: %d → %q (rukovatelj pozvan: %v)", s.metoda, s.putanja, w.Code, w.Header().Get("Location"), z.pozvan)
		}
	}
}

func TestKontekstNakonProvjere(t *testing.T) {
	o := novaOkolinaPrijave(t)
	pperic := o.osoba("pperic", "perina-lozinka", "pperic@example.com", false)
	sesija, _, err := o.auth.Login("pperic", "perina-lozinka", "192.168.1.50", "test")
	if err != nil {
		t.Fatal(err)
	}
	z := &mwZapis{}
	w := mwZahtjev(mwPosluzitelj(o, z), http.MethodGet, "/dashboard", sesija.ID.String())
	if !z.pozvan || w.Code != http.StatusNoContent {
		t.Fatalf("prijavljen korisnik ne prolazi: %d → %q", w.Code, w.Header().Get("Location"))
	}
	if z.korisnik == nil || z.korisnik.ID != pperic.ID || z.stvarni == nil || z.stvarni.ID != pperic.ID {
		t.Errorf("korisnik u kontekstu: %+v, stvarni: %+v", z.korisnik, z.stvarni)
	}
	if z.ovlasti == nil || z.ovlasti.IsGlobalAdmin || z.gleda || z.sesija != sesija.ID {
		t.Errorf("ovlasti %+v, gleda %v, sesija %v", z.ovlasti, z.gleda, z.sesija)
	}
}

func TestTudjimOcimaKrozProvjeru(t *testing.T) {
	o := novaOkolinaPrijave(t)
	uprava := o.osoba("uprava", "upravina-lozinka", "uprava@example.com", true)
	pperic := o.osoba("pperic", "perina-lozinka", "pperic@example.com", false)
	sesija, _, err := o.auth.Login("uprava", "upravina-lozinka", "192.168.1.50", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := o.auth.StartViewingAs(sesija.ID, o.ovlasti(uprava), pperic.ID); err != nil {
		t.Fatal(err)
	}

	z := &mwZapis{}
	h := mwPosluzitelj(o, z)
	mwZahtjev(h, http.MethodGet, "/dashboard", sesija.ID.String())
	if !z.pozvan || !z.gleda {
		t.Fatalf("pregled tuđim očima nije prošao: %+v", z)
	}
	// rukovatelj dobiva gledanu osobu i njezine ovlasti, a stvarni je korisnik
	// administrator
	if z.korisnik.ID != pperic.ID || z.stvarni.ID != uprava.ID || z.ovlasti.IsGlobalAdmin {
		t.Errorf("korisnik %s, stvarni %s, ovlasti globalne: %v", z.korisnik.Username, z.stvarni.Username, z.ovlasti.IsGlobalAdmin)
	}

	// upis tuđim očima je zabranjen, izlaz iz pregleda nije
	z = &mwZapis{}
	h = mwPosluzitelj(o, z)
	if w := mwZahtjev(h, http.MethodPost, "/readings", sesija.ID.String()); w.Code != http.StatusForbidden || z.pozvan {
		t.Errorf("upis tuđim očima: %d (rukovatelj pozvan: %v)", w.Code, z.pozvan)
	}
	if mwZahtjev(h, http.MethodPost, "/view-as/stop", sesija.ID.String()); !z.pozvan {
		t.Error("izlaz iz pregleda tuđim očima mora proći i kao POST")
	}

	// Gledani račun smije biti isključen: provjera gleda samo stvarnog korisnika.
	if _, err := o.baza.Exec(`UPDATE users SET is_active = 0 WHERE id = ?`, pperic.ID.String()); err != nil {
		t.Fatal(err)
	}
	z = &mwZapis{}
	mwZahtjev(mwPosluzitelj(o, z), http.MethodGet, "/dashboard", sesija.ID.String())
	if !z.pozvan || !z.gleda || z.korisnik.ID != pperic.ID {
		t.Errorf("pogled na isključen račun danas prolazi; dobiveno %+v", z)
	}

	// kad administratoru prestane uprava, pogled se gasi i vidi sebe
	if _, err := o.baza.Exec(`UPDATE users SET is_global_admin = 0 WHERE id = ?`, uprava.ID.String()); err != nil {
		t.Fatal(err)
	}
	z = &mwZapis{}
	mwZahtjev(mwPosluzitelj(o, z), http.MethodGet, "/dashboard", sesija.ID.String())
	if !z.pozvan || z.gleda || z.korisnik.ID != uprava.ID {
		t.Errorf("bez uprave pogled tuđim očima mora prestati: gleda %v, korisnik %v", z.gleda, z.korisnik)
	}
}
