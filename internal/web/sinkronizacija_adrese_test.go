package web

// Adrese računala i sirove greške razmjene (nose adrese) vidi samo
// administrator: /api/sync/status ih drugom korisniku s modulom
// Administracija ne šalje, a stranica Čvor i mreža ih ne pokazuje (plan
// WEB synca, F1). Put razmjene nema adresa, pa ga vide svi.

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/razmjena"
)

func TestStanjeSinkronizacijeBezAdresa(t *testing.T) {
	s, baza := cvorIBazaZaWeb(t, "cop-osijek")
	kljuc, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePeer(context.Background(), peers.Peer{NodeID: "laptop-baranja", Name: "Laptop Baranja", PublicKey: razmjena.PublicKeyString(kljuc),
		Addresses: []string{"192.168.1.30:4710"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO peer_sync (node_id, last_ok, last_error, fails, tailscale, adrese, put, rtt_ms)
		VALUES ('laptop-baranja', ?, 'dial tcp 192.168.1.30:4710: no route to host', 1, '["100.101.0.2:4710"]',
		'[{"a":"192.168.1.31:4710","k":"2026-10-23T10:00:00Z"}]', 'tunel', 35)`, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	h := NewSyncHandler(s, nil)
	trazi := func(admin bool) string {
		r := httptest.NewRequest(http.MethodGet, "/api/sync/status", nil)
		r = r.WithContext(context.WithValue(r.Context(), contextKeyPerms, &models.UserPermissions{IsGlobalAdmin: admin}))
		w := httptest.NewRecorder()
		h.HandleStatus(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("admin %v: %d %s", admin, w.Code, w.Body)
		}
		return w.Body.String()
	}
	drugi := trazi(false)
	for _, nema := range []string{"192.168.1.30", "192.168.1.31", "100.101.0.2", "no route", `"pokusaji":{`} {
		if strings.Contains(drugi, nema) {
			t.Errorf("korisnik koji nije administrator dobiva %q: %s", nema, drugi)
		}
	}
	var st struct {
		Peers []struct {
			State struct {
				Put peers.PutRazmjene `json:"put"`
			} `json:"state"`
		} `json:"peers"`
	}
	if err := json.Unmarshal([]byte(drugi), &st); err != nil || len(st.Peers) != 1 || st.Peers[0].State.Put.Naziv != "SERVER sync" || st.Peers[0].State.Put.RTTMs != 35 {
		t.Errorf("put razmjene: %+v %v", st, err)
	}
	if admin := trazi(true); !strings.Contains(admin, "192.168.1.30:4710") || !strings.Contains(admin, "no route to host") || !strings.Contains(admin, "100.101.0.2") {
		t.Errorf("administrator ne dobiva adrese: %s", admin)
	}
	if z := h.zaPrikaz(httptest.NewRequest(http.MethodGet, "/", nil), nil); z.Status != nil || z.Pokusaji != nil {
		t.Errorf("bez stanja: %+v", z)
	}
}

// Stranica Čvor i mreža pokazuje adrese poznatih čvorova, domenskih
// čvorova i vlastite javne adrese, i bilješku s greškom razmjene, samo
// administratoru; pretragu lokalne mreže nudi samo njemu.
func TestPoznatiCvoroviBezAdresa(t *testing.T) {
	tmpl := predlozakStranice(t, "settings.html")
	zadnji := time.Now()
	for admin, treba := range map[bool]bool{true: true, false: false} {
		var b strings.Builder
		data := SettingsPageData{CurrentUser: adminIvic, Permissions: &models.UserPermissions{IsGlobalAdmin: admin},
			Self: peers.Peer{NodeID: "cop-osijek", Addresses: []string{"cop-osijek.primjer.hr:4710"}, IsBootstrap: true},
			Peers: []peers.Peer{
				{NodeID: "laptop-baranja", Name: "Laptop Baranja", Addresses: []string{"192.168.1.30:4710"},
					LastSync: &zadnji, LastSyncNote: "greška: dial tcp 192.168.1.30:4710: no route to host"},
				{NodeID: "ured-vukovar", Name: "Ured Vukovar", Addresses: []string{"ured.primjer.hr:4710"}, IsBootstrap: true}}}
		if err := tmpl.ExecuteTemplate(&b, "settings.html", data); err != nil {
			t.Fatal(err)
		}
		html := b.String()
		for _, adresa := range []string{"192.168.1.30", "ured.primjer.hr", "cop-osijek.primjer.hr", "onclick=\"discover()\""} {
			if strings.Contains(html, adresa) != treba {
				t.Errorf("administrator %v: %q na stranici %v", admin, adresa, !treba)
			}
		}
		if !strings.Contains(html, "zadnji sync") || !admin && !strings.Contains(html, "ima javnu adresu") {
			t.Errorf("administrator %v: nema zadnje razmjene ili oznake javne adrese", admin)
		}
	}
}

// Pretraga lokalne mreže, povijest zapisa iz knjige i stanje uparivanja
// nose adrese računala, pa ih dobiva samo administrator.
func TestPretragaIPovijestSamoAdministratoru(t *testing.T) {
	s, baza := cvorIBazaZaWeb(t, "cop-osijek")
	h := NewSettingsHandler(s, ledger.New(baza, "cop-osijek"), nil)
	for ime, rukovatelj := range map[string]http.HandlerFunc{"pretraga": h.HandleDiscover, "povijest": h.HandleHistory, "uparivanje": h.HandlePairStatus} {
		r := httptest.NewRequest(http.MethodGet, "/api/history/peers/laptop-baranja", nil)
		r.SetPathValue("entity", "peers")
		r.SetPathValue("id", "laptop-baranja")
		r = r.WithContext(context.WithValue(r.Context(), contextKeyPerms, &models.UserPermissions{}))
		w := httptest.NewRecorder()
		rukovatelj(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s bez prava: %d", ime, w.Code)
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/api/history/peers/laptop-baranja", nil)
	r.SetPathValue("entity", "peers")
	r.SetPathValue("id", "laptop-baranja")
	r = r.WithContext(context.WithValue(r.Context(), contextKeyPerms, &models.UserPermissions{IsGlobalAdmin: true}))
	w := httptest.NewRecorder()
	h.HandleHistory(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"versions":[]`) {
		t.Errorf("povijest administratoru: %d %s", w.Code, w.Body)
	}
}
