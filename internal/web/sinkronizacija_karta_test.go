package web

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/razmjena"
)

// kljuceviObjekta su ključevi JSON objekta, redom kojim su zapisani
func kljuceviObjekta(t *testing.T, b []byte) []string {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(b))
	if tok, err := d.Token(); err != nil || tok != json.Delim('{') {
		t.Fatalf("nije objekt: %s", b)
	}
	var out []string
	for d.More() {
		tok, err := d.Token()
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, tok.(string))
		var v json.RawMessage
		if err := d.Decode(&v); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// prijaviProbnuKartu spremi probnu kartu u tri dijela i prijavi je kako to
// radi goCOP (main.go), bez obrane; vrati njezin opis. Samo ovaj pomoćnik
// zove kartu (SpremiKartu, VelicinaDijelaKarte, PostaviKarte), a to u N04
// seli u vektorskakarta: tada se mijenja samo on, očekivanja testa ne.
func prijaviProbnuKartu(t *testing.T, s *peers.Service) models.VektorskaKarta {
	t.Helper()
	stara := peers.VelicinaDijelaKarte
	peers.VelicinaDijelaKarte = 64
	mapa := filepath.Join(t.TempDir(), "karte")
	k, err := peers.SpremiKartu(bytes.NewReader(probnaPMTiles()), mapa, "Baranja", "OpenStreetMap (proba)")
	peers.VelicinaDijelaKarte = stara
	if err != nil || len(k.Dijelovi) != 3 {
		t.Fatalf("probna karta: %d dijelova, %v", len(k.Dijelovi), err)
	}
	s.PostaviKarte(mapa, func() *models.VektorskaKarta { return &k }, nil)
	return k
}

// Ključevi stanja razmjene redom. karta je iza svih ključeva stanja, i
// iza onih koji se pojave samo ponekad (program, gradnja, network,
// last_ok, lan), a pokusaji, kad ih ima, iza nje.
var (
	kljuceviStanja      = []string{"node_id", "node_name", "versions", "interval_sec", "auto_sync", "peers", "online", "total", "alerts", "generated_at", "karta"}
	kljuceviPunogStanja = []string{"node_id", "node_name", "program", "gradnja", "network", "versions", "interval_sec", "auto_sync", "peers", "online", "total", "last_ok", "alerts", "generated_at", "lan", "karta"}
)

// Stanje razmjene (/api/sync/status i odgovor gumba „Razmijeni sa svima”)
// nosi kartu bez interneta ovog računala kao ključ karta, iza svih ostalih
// ključeva stanja, a uz svako računalo ono što je o svojoj karti javilo
// (peers[].state.karta). Stranica Sinkronizacija iz toga slaže stupac
// „Karta bez interneta”. Zaključano prije nego što karta izađe iz peers
// (N04, PR 1).
func TestStanjeRazmjeneNosiKartu(t *testing.T) {
	s, baza := cvorIBazaZaWeb(t, "cop-osijek")
	k := prijaviProbnuKartu(t, s)
	kljuc, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePeer(context.Background(), peers.Peer{NodeID: "laptop-baranja", Name: "Laptop Baranja", PublicKey: razmjena.PublicKeyString(kljuc)}); err != nil {
		t.Fatal(err)
	}
	njegova := `{"otisak":"4d2c6f7a8b9e0d1c2b3a4f5e6d7c8b9a0f1e2d3c4b5a69788796a5b4c3d2e1f0","svih":8}`
	if _, err := baza.Exec(`INSERT INTO peer_sync (node_id, mogucnosti, mogucnosti_poznate, karta) VALUES ('laptop-baranja', ?, 1, ?)`,
		`["dijelovi-karte/1","razmjena/1"]`, njegova); err != nil {
		t.Fatal(err)
	}
	moja := `{"otisak":"` + k.Otisak + `","slozena":true,"dijelova":3,"svih":3}`
	h := NewSyncHandler(s, nil)

	provjeri := func(ime string, b []byte, kljucevi []string) {
		t.Helper()
		if got := kljuceviObjekta(t, b); !slices.Equal(got, kljucevi) {
			t.Errorf("%s: ključevi stanja\n%v\nočekivano\n%v", ime, got, kljucevi)
		}
		var st struct {
			Karta json.RawMessage `json:"karta"`
			Peers []struct {
				NodeID string `json:"node_id"`
				State  struct {
					Karta json.RawMessage `json:"karta"`
				} `json:"state"`
			} `json:"peers"`
		}
		if err := json.Unmarshal(b, &st); err != nil {
			t.Fatal(err)
		}
		if string(st.Karta) != moja {
			t.Errorf("%s: karta ovog računala %s, očekivano %s", ime, st.Karta, moja)
		}
		if len(st.Peers) != 1 || st.Peers[0].NodeID != "laptop-baranja" || string(st.Peers[0].State.Karta) != njegova {
			t.Errorf("%s: karta drugog računala %+v", ime, st.Peers)
		}
	}
	zahtjev := func(metoda, put string, admin bool) *http.Request {
		r := httptest.NewRequest(metoda, put, nil)
		return r.WithContext(context.WithValue(r.Context(), contextKeyPerms, &models.UserPermissions{IsGlobalAdmin: admin}))
	}
	zovi := func(metoda, put string, admin bool, rukovatelj http.HandlerFunc) []byte {
		t.Helper()
		w := httptest.NewRecorder()
		rukovatelj(w, zahtjev(metoda, put, admin))
		if w.Code != http.StatusOK {
			t.Fatalf("%s %s: %d %s", metoda, put, w.Code, w.Body)
		}
		return w.Body.Bytes()
	}
	for _, admin := range []bool{false, true} {
		ime := "stanje, administrator"
		if !admin {
			ime = "stanje, drugi korisnik"
		}
		provjeri(ime, zovi(http.MethodGet, "/api/sync/status", admin, h.HandleStatus), kljuceviStanja)
	}

	var odgovor struct {
		Status json.RawMessage `json:"status"`
	}
	b := zovi(http.MethodPost, "/api/sync/all", true, h.HandleSyncAll)
	if err := json.Unmarshal(b, &odgovor); err != nil {
		t.Fatal(err)
	}
	if kljucevi := kljuceviObjekta(t, b); !slices.Equal(kljucevi, []string{"results", "status", "success"}) {
		t.Errorf("odgovor razmjene sa svima: %v", kljucevi)
	}
	provjeri("razmjena sa svima", odgovor.Status, kljuceviStanja)

	// Ključeve koji se pojave samo ponekad test ne može dobiti od samog
	// računala (lan traži drugo računalo u lokalnoj mreži), pa ih dopiše u
	// stanje računala prije prikaza. Drugi korisnik lan ne vidi.
	s.PostaviProgram("0.0.71-alfa", "proba")
	st, err := s.Status(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	sad := time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)
	st.Network = &peers.Network{Name: "Hrvatske vode", JoinedAt: sad}
	st.LastOK = &sad
	for _, admin := range []bool{true, false} {
		st.LanDiscovered = []peers.Discovered{{Found: razmjena.Found{Addr: "192.168.1.20"}}}
		ime, kljucevi := "puno stanje, administrator", kljuceviPunogStanja
		if !admin {
			ime, kljucevi = "puno stanje, drugi korisnik", slices.DeleteFunc(slices.Clone(kljuceviPunogStanja), func(k string) bool { return k == "lan" })
		}
		b, err := json.Marshal(h.zaPrikaz(zahtjev(http.MethodGet, "/api/sync/status", admin), st))
		if err != nil {
			t.Fatal(err)
		}
		provjeri(ime, b, kljucevi)
	}
}
