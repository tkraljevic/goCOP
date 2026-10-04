package web

// Postavke mreže: ovlast za primanje i primanje na daljinu smije samo
// globalni administrator; kriv kod ili neispravan zahtjev ne prolaze.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/peers"
)

// svjeziCvorZaWeb je čvor bez mreže, u svojoj bazi
func svjeziCvorZaWeb(t *testing.T, id string) *peers.Service {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), id+".db")
	baza, err := db.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	cvor, err := peers.LoadNode(dbPath, id, id, "test")
	if err != nil {
		t.Fatal(err)
	}
	s, err := peers.NewService(baza, ledger.New(baza, id), cvor, peers.Ports{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPostavkeMrezeOvlastIPrimanjeNaDaljinu(t *testing.T) {
	ured := primateljNaDaljinu(t)
	h := &SettingsHandler{peers: ured}
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	obican := &models.UserPermissions{}

	zovi := func(perms *models.UserPermissions, rukovatelj http.HandlerFunc, cvor string, tijelo any) *httptest.ResponseRecorder {
		t.Helper()
		var b []byte
		switch v := tijelo.(type) {
		case string:
			b = []byte(v)
		default:
			b, _ = json.Marshal(v)
		}
		r := httptest.NewRequest(http.MethodPost, "/api/network/x", strings.NewReader(string(b)))
		r.Header.Set("Content-Type", "application/json")
		if cvor != "" {
			r.SetPathValue("node", cvor)
		}
		r = r.WithContext(context.WithValue(r.Context(), contextKeyPerms, perms))
		w := httptest.NewRecorder()
		rukovatelj(w, r)
		return w
	}

	f := svjeziCvorZaWeb(t, "pperic-thinkpad")
	zahtjev, kod, err := f.NapraviZahtjev()
	if err != nil {
		t.Fatal(err)
	}

	// bez administratora ništa
	for ime, w := range map[string]*httptest.ResponseRecorder{
		"pročitaj":       zovi(obican, h.HandleProcitajZahtjev, "", map[string]string{"zahtjev": string(zahtjev)}),
		"primi":          zovi(obican, h.HandlePrimiZahtjev, "", map[string]any{"zahtjev": string(zahtjev), "kod": kod}),
		"daj ovlast":     zovi(obican, h.HandleIzdajOvlast, "pperic-thinkpad", nil),
		"oduzmi ovlast":  zovi(obican, h.HandleOpozoviOvlast, "pperic-thinkpad", nil),
		"bez dopuštenja": zovi(nil, h.HandlePrimiZahtjev, "", map[string]any{"zahtjev": string(zahtjev), "kod": kod}),
	} {
		if w.Code != http.StatusForbidden {
			t.Errorf("%s bez administratora: %d", ime, w.Code)
		}
	}

	// čitanje zahtjeva
	if w := zovi(admin, h.HandleProcitajZahtjev, "", "nije json"); w.Code != http.StatusBadRequest {
		t.Errorf("pročitaj nevaljano tijelo: %d", w.Code)
	}
	if w := zovi(admin, h.HandleProcitajZahtjev, "", map[string]string{"zahtjev": "{}"}); w.Code != http.StatusBadRequest {
		t.Errorf("pročitaj nevaljan zahtjev: %d", w.Code)
	}
	w := zovi(admin, h.HandleProcitajZahtjev, "", map[string]string{"zahtjev": string(zahtjev)})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"cvor":"pperic-thinkpad"`) || strings.Contains(w.Body.String(), kod) {
		t.Fatalf("pročitaj: %d %s", w.Code, w.Body.String())
	}

	// primanje
	if w := zovi(admin, h.HandlePrimiZahtjev, "", "nije json"); w.Code != http.StatusBadRequest {
		t.Errorf("primi nevaljano tijelo: %d", w.Code)
	}
	if w := zovi(admin, h.HandlePrimiZahtjev, "", map[string]any{"zahtjev": "{}", "kod": kod}); w.Code != http.StatusBadRequest {
		t.Errorf("primi nevaljan zahtjev: %d", w.Code)
	}
	if w := zovi(admin, h.HandlePrimiZahtjev, "", map[string]any{"zahtjev": string(zahtjev), "kod": "AAAA-BBBB"}); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "kod za primanje") {
		t.Errorf("primi s krivim kodom: %d %s", w.Code, w.Body.String())
	}
	w = zovi(admin, h.HandlePrimiZahtjev, "", map[string]any{"zahtjev": string(zahtjev), "kod": kod})
	var odg struct {
		Potvrda  string `json:"potvrda"`
		Datoteka string `json:"datoteka"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &odg); w.Code != http.StatusOK || err != nil || odg.Datoteka != "gocop-potvrda-pperic-thinkpad.json" {
		t.Fatalf("primi: %d %s", w.Code, w.Body.String())
	}
	if _, err := f.UveziPotvrdu(context.Background(), []byte(odg.Potvrda)); err != nil {
		t.Fatalf("potvrda iz Postavki: %v", err)
	}

	// ovlast za primanje
	if w := zovi(admin, h.HandleIzdajOvlast, "nepoznat", nil); w.Code != http.StatusBadRequest {
		t.Errorf("ovlast nepoznatom čvoru: %d", w.Code)
	}
	if w := zovi(admin, h.HandleIzdajOvlast, "pperic-thinkpad", nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"ovlast"`) {
		t.Fatalf("daj ovlast: %d %s", w.Code, w.Body.String())
	}
	w = zovi(admin, h.HandleOpozoviOvlast, "pperic-thinkpad", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"pogodjeni"`) {
		t.Fatalf("oduzmi ovlast: %d %s", w.Code, w.Body.String())
	}
	if w := zovi(admin, h.HandleOpozoviOvlast, "pperic-thinkpad", nil); w.Code != http.StatusBadRequest {
		t.Errorf("oduzmi ovlast koje više nema: %d", w.Code)
	}
}
