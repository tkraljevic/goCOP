package web

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Poslužitelj e-pošte koji nije spremljen ispituje se samo s ovog računala
// ili iz lokalne mreže, kao provjera veze; izvana čvor se na njega ne
// spaja. Spremljeni poslužitelj ispituje se i izvana.
func TestIspitajNoviPosluziteljSamoIzLokalneMreze(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "posta.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	akti := service.NewAktService(repository.NewAktiRepository(baza, ledger.New(baza, "test")), nil, nil, nil, nil, nil, nil, "test")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var spajanja atomic.Int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			spajanja.Add(1)
			c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	spremljeni := posta.Postavke{Nacin: posta.NacinSMTP, Posluzitelj: "127.0.0.1", Port: port, Sigurnost: posta.TLS}
	novi := spremljeni
	novi.Posluzitelj = "localhost"
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	if err := akti.SpremiPostu(context.Background(), admin, spremljeni); err != nil {
		t.Fatal(err)
	}
	ispitaj := func(u uvjetiZahtjeva, p posta.Postavke) string {
		w := httptest.NewRecorder()
		ispitajPostu(w, zahtjevPostavki(http.MethodPost, "/administracija/posta", nil, u), akti, p)
		return mustUnescape(w.Header().Get("Location"))
	}
	izvana := uvjetiZahtjeva{perms: admin, adresa: adresaLokalno, tunel: true, korisnik: adminIvic}

	if loc := ispitaj(izvana, novi); !strings.Contains(loc, "Izvana se ispituje samo spremljeni poslužitelj") || spajanja.Load() != 0 {
		t.Errorf("novi poslužitelj izvana: %s (spajanja %d)", loc, spajanja.Load())
	}
	for _, x := range []struct {
		opis string
		u    uvjetiZahtjeva
		p    posta.Postavke
	}{
		{"spremljeni poslužitelj izvana", izvana, spremljeni},
		{"novi poslužitelj iz lokalne mreže", kaoAdmin, novi},
	} {
		prije := spajanja.Load()
		if loc := ispitaj(x.u, x.p); strings.Contains(loc, "Izvana se ispituje") || !strings.Contains(loc, "nije prošao provjeru") {
			t.Errorf("%s: %s", x.opis, loc)
		}
		if spajanja.Load() == prije {
			t.Errorf("%s: čvor se nije spojio na poslužitelj", x.opis)
		}
	}
}
