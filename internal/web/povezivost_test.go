package web

// WEB sync isključen nakon padova (povezivost.go): pločica to kaže svima,
// Administracija nudi gumb samo administratoru izravno iz lokalne mreže,
// a gumb mijenja stanje tek kad se oznaka stvarno obriše.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// lazniWebSync je stanje WEB synca kakvo daje main; ukljuci broji pozive
type lazniWebSync struct {
	st     StanjePovezivosti
	greska error
	pozvan int
}

func (l *lazniWebSync) postavi(s *Server) {
	s.SetPovezivost(func() StanjePovezivosti { return l.st }, func() error {
		l.pozvan++
		if l.greska != nil {
			return l.greska
		}
		l.st.UkljucujeSe = true
		return nil
	})
}

func plocicaRazmjene(t *testing.T, s *Server) string {
	t.Helper()
	malo := time.Now().Add(-2 * time.Minute)
	s.SetRazmjena(func(context.Context) RazmjenaStanje {
		return RazmjenaStanje{UMrezi: true, Cvorovi: []CvorRazmjene{{Naziv: "laptop", Dostupnost: "online", Zadnja: &malo}}}
	})
	w := httptest.NewRecorder()
	s.ShowRazmjena(w, httptest.NewRequest(http.MethodGet, "/razmjena/plocica", nil))
	return w.Body.String()
}

func TestWebSyncNakonPadova(t *testing.T) {
	s, _ := cvorSPostavkama(t, sadrzajPostavk, nil)
	if html := iscrtajPostavke(t, s, "/settings", kaoAdmin); strings.Contains(html, `id="web-sync-padovi"`) {
		t.Error("blok WEB synca bez stanja")
	}
	if p := plocicaRazmjene(t, s); strings.Contains(p, "WEB sync") || !strings.Contains(p, "Sve je usklađeno") {
		t.Errorf("pločica bez stanja:\n%s", p)
	}
	l := &lazniWebSync{st: StanjePovezivosti{NakonPadova: true}}
	l.postavi(s)
	if p := plocicaRazmjene(t, s); !strings.Contains(p, "WEB sync je isključen, jer se program dvaput zaredom prekinuo") || strings.Contains(p, "Sve je usklađeno") {
		t.Errorf("pločica nakon padova:\n%s", p)
	}
	html := iscrtajPostavke(t, s, "/settings", kaoAdmin)
	for _, mora := range []string{`id="web-sync-padovi"`, `action="/settings/web-sync/ukljuci"`, "Ponovno uključi WEB sync"} {
		if !strings.Contains(html, mora) {
			t.Errorf("/settings nakon padova: nema %q", mora)
		}
	}
	izvana := kaoAdmin
	izvana.adresa = adresaIzvana
	if html := iscrtajPostavke(t, s, "/settings", izvana); strings.Contains(html, `action="/settings/web-sync/ukljuci"`) ||
		!strings.Contains(html, zabranaIzvana.postavke) {
		t.Error("izvana: gumb ili bez razloga")
	}
	l.st.UkljucujeSe = true
	if html := iscrtajPostavke(t, s, "/settings", kaoAdmin); !strings.Contains(html, "WEB sync kreće idućim pokretanjem") ||
		strings.Contains(html, `action="/settings/web-sync/ukljuci"`) {
		t.Error("nakon gumba: nema poruke ili ima gumb")
	}
	if p := plocicaRazmjene(t, s); !strings.Contains(p, "uključit će se idućim pokretanjem") {
		t.Errorf("pločica nakon gumba:\n%s", p)
	}
}

func TestUkljuciWebSync(t *testing.T) {
	s, _ := cvorSPostavkama(t, sadrzajPostavk, nil)
	salji := func(u uvjetiZahtjeva) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.handleUkljuciWebSync(w, zahtjevPostavki(http.MethodPost, "/settings/web-sync/ukljuci", nil, u))
		return w
	}
	l := &lazniWebSync{st: StanjePovezivosti{NakonPadova: true}, greska: errors.New("zauzeto")}
	l.postavi(s)
	obican := uvjetiZahtjeva{perms: &models.UserPermissions{}, adresa: adresaLokalno, korisnik: adminIvic}
	tunel := kaoAdmin
	tunel.adresa, tunel.tunel = adresaTunela, true
	for ime, u := range map[string]uvjetiZahtjeva{"ne-administrator": obican, "kroz tunel": tunel} {
		if w := salji(u); w.Code != http.StatusForbidden {
			t.Errorf("%s: %d", ime, w.Code)
		}
	}
	if l.pozvan != 0 {
		t.Fatal("oznaka brisana bez prava")
	}
	preusmjereno := func(sadrzi string) {
		t.Helper()
		w := salji(kaoAdmin)
		if lok := w.Header().Get("Location"); w.Code != http.StatusSeeOther || !strings.Contains(lok, sadrzi) || !strings.HasSuffix(lok, "#web-sync-padovi") {
			t.Errorf("%d %q, očekivano %q", w.Code, lok, sadrzi)
		}
	}
	preusmjereno("error=WEB+sync+nije+ponovno+uklju%C4%8Den") // oznaka se ne da obrisati
	if l.st.UkljucujeSe {
		t.Error("stanje promijenjeno bez brisanja")
	}
	l.greska = nil
	preusmjereno("success=WEB+sync+se+ponovno+uklju%C4%8Duje")
	l.st = StanjePovezivosti{} // WEB sync nije isključen nakon padova
	preusmjereno("error=WEB+sync+nije+isklju%C4%8Den")
	if l.pozvan != 2 {
		t.Errorf("brisanja: %d", l.pozvan)
	}
}
