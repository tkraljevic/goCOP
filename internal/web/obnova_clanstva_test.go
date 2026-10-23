package web

// Gumbi za obnovu članstva u Postavkama (G9a): samo administrator; računalo
// s ključem mreže obnovi sebe odmah, član upiše zahtjev, a stranica
// pokazuje rok i gumbe.

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/razmjena"
)

func TestGumbiObnoveClanstva(t *testing.T) {
	ured := primateljNaDaljinu(t)
	clan := svjeziCvorZaWeb(t, "pperic-thinkpad")
	zahtjev, kod, err := clan.NapraviZahtjev()
	if err != nil {
		t.Fatal(err)
	}
	potvrda, err := ured.PrimiZahtjev(context.Background(), zahtjev, kod, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clan.UveziPotvrdu(context.Background(), potvrda); err != nil {
		t.Fatal(err)
	}
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	zovi := func(s *peers.Service, perms *models.UserPermissions, rukovatelj func(*SettingsHandler) http.HandlerFunc, cvor string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/api/network/obnovi", strings.NewReader("{}"))
		if cvor != "" {
			r.SetPathValue("node", cvor)
		}
		r = r.WithContext(context.WithValue(r.Context(), contextKeyPerms, perms))
		w := httptest.NewRecorder()
		rukovatelj(&SettingsHandler{peers: s})(w, r)
		return w
	}
	sebe := func(h *SettingsHandler) http.HandlerFunc { return h.HandleObnoviClanstvo }
	clana := func(h *SettingsHandler) http.HandlerFunc { return h.HandleObnoviClana }

	for ime, w := range map[string]*httptest.ResponseRecorder{
		"obnovi sebe":  zovi(ured, &models.UserPermissions{}, sebe, ""),
		"obnovi člana": zovi(ured, nil, clana, "pperic-thinkpad"),
	} {
		if w.Code != http.StatusForbidden {
			t.Errorf("%s bez administratora: %d", ime, w.Code)
		}
	}
	for _, c := range []struct {
		ime, cvor, ima string
		s              *peers.Service
		rukovatelj     func(*SettingsHandler) http.HandlerFunc
		kod            int
	}{
		{"osnivač obnovi sebe", "", "Članstvo ovog računala obnovljeno je i vrijedi do", ured, sebe, http.StatusOK},
		{"član traži obnovu", "", "Zahtjev za obnovu je upisan", clan, sebe, http.StatusOK},
		{"izvan mreže", "", "Obnova nije zatražena", svjeziCvorZaWeb(t, "samotnjak"), sebe, http.StatusBadRequest},
		{"Obnovi uz sebe", "cop-osijek", "Članstvo računala cop-osijek obnovljeno je", ured, clana, http.StatusOK},
		{"svježe članstvo", "pperic-thinkpad", "Članstvo nije obnovljeno", ured, clana, http.StatusBadRequest},
		{"nepoznato računalo", "nepoznat", "ne vrijedi ni u pričuvi", ured, clana, http.StatusBadRequest},
	} {
		if w := zovi(c.s, admin, c.rukovatelj, c.cvor); w.Code != c.kod || !strings.Contains(w.Body.String(), c.ima) {
			t.Errorf("%s: %d %s", c.ime, w.Code, w.Body.String())
		}
	}
}

// Postavke pokazuju rok ovog računala s gumbom, a uz člana kojem rok
// ističe ili je u pričuvi njegov rok i gumb Obnovi.
func TestPostavkePokazujuRokClanstva(t *testing.T) {
	ured := primateljNaDaljinu(t)
	clanstvo, ovlast := ured.VlastitiRokovi(context.Background())
	sada := time.Now()
	data := SettingsPageData{
		CurrentUser: adminIvic, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Network: ured.NetworkInfo(), RokClanstva: clanstvo, RokOvlasti: &peers.Rok{Do: sada.Add(5 * 24 * time.Hour), Stanje: peers.RokIstjece},
		Members: []peers.Member{
			{Membership: razmjena.Membership{DeviceID: "laptop-baranja", IssuedBy: "cop-osijek", ExpiresAt: sada.Add(-5 * 24 * time.Hour)}, Valid: true,
				Rok: peers.Rok{Do: sada.Add(-5 * 24 * time.Hour), Stanje: peers.RokPricuva}},
			{Membership: razmjena.Membership{DeviceID: "laptop-vinkovci", IssuedBy: "cop-osijek", ExpiresAt: sada.Add(300 * 24 * time.Hour)}, Valid: true,
				Rok: peers.Rok{Do: sada.Add(300 * 24 * time.Hour), Stanje: peers.RokVrijedi}},
		},
	}
	if ovlast != nil {
		t.Errorf("osnivač s ovlasti: %+v", ovlast)
	}
	var b bytes.Buffer
	if err := predlozakStranice(t, "settings.html").ExecuteTemplate(&b, "settings.html", data); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{`onclick="obnoviClanstvo()"`, "vrijedi do ", "Ovlast za primanje", "badge-warning", "badge-danger", "razmjena radi još do",
		`obnoviClana('laptop-baranja')`, `obnoviClana('laptop-vinkovci')`} {
		if !strings.Contains(b.String(), d) {
			t.Errorf("Postavke nemaju %q", d)
		}
	}
}
