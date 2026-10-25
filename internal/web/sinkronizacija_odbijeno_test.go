package web

// Zapis odbijenog na stranici Sinkronizacija (G7): samo administrator ga
// vidi, riječima i s imenom poznatog računala; zapis koji se ne da
// pročitati stranica javi, a ostatak stranice radi.

import (
	"context"
	"crypto/ed25519"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/razmjena"
	webassets "gocop/web"
)

func TestStranicaSinkronizacijeOdbijeno(t *testing.T) {
	s, baza := cvorIBazaZaWeb(t, "cop-osijek")
	kljuc, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePeer(context.Background(), peers.Peer{NodeID: "laptop-baranja", Name: "Laptop Baranja", PublicKey: razmjena.PublicKeyString(kljuc)}); err != nil {
		t.Fatal(err)
	}
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("sinkronizacija.html")...)
	if err != nil {
		t.Fatal(err)
	}
	h := NewSyncHandler(s, tmpl)
	korisnik := &models.User{ID: uuid.New(), FullName: "Dežurni Centra", OrgName: "Hrvatske vode"}
	otvori := func(perms *models.UserPermissions) string {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/sinkronizacija", nil)
		ctx := context.WithValue(r.Context(), contextKeyUser, korisnik)
		ctx = context.WithValue(ctx, contextKeyPerms, perms)
		w := httptest.NewRecorder()
		h.ShowDashboard(w, r.WithContext(ctx))
		if w.Code != http.StatusOK {
			t.Fatalf("stranica %d\n%s", w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	admin := &models.UserPermissions{IsGlobalAdmin: true, User: *korisnik}
	obican := &models.UserPermissions{User: *korisnik}

	if b := otvori(admin); !strings.Contains(b, "Ništa nije odbijeno.") {
		t.Error("prazan zapis bez poruke da ništa nije odbijeno")
	}
	sada := time.Now().UTC()
	for _, r := range [][]string{
		{peers.VrstaVrata, peers.PutVrataTunel, "nije-clan", "laptop-baranja"},
		{peers.VrstaDjelomicno, "", "oznaka stranice/2", ""},
		{peers.VrstaOdbiliSu, "", "kod 0x0999", "ured-vukovar"},
	} {
		if _, err := baza.Exec(`INSERT INTO razmjena_odbijeno (dan, vrsta, put, razlog, racunalo, broj, zadnje) VALUES (?, ?, ?, ?, ?, 3, ?)`,
			sada.Format(time.DateOnly), r[0], r[1], r[2], r[3], sada); err != nil {
			t.Fatal(err)
		}
	}
	b := otvori(admin)
	for _, d := range []string{`id="odbijeno"`, sada.Format("2. 1."), "vrata nisu pustila vezu", "tunel", "računalo nije član mreže ili je opozvano", "Laptop Baranja",
		"stiglo je nešto što ovaj program ne razumije", "oznaka mogućnosti stranice/2", "—", "kod 0x0999", "ured-vukovar"} {
		if !strings.Contains(b, d) {
			t.Errorf("administrator ne vidi %q", d)
		}
	}
	if b := otvori(obican); strings.Contains(b, "Odbijeno i djelomično") {
		t.Error("zapis odbijenog vidi i korisnik koji nije administrator")
	}
	if _, err := baza.Exec(`DROP TABLE razmjena_odbijeno`); err != nil {
		t.Fatal(err)
	}
	if b := otvori(admin); !strings.Contains(b, "Zapis odbijenih razmjena se ne da pročitati") || strings.Contains(b, "Ništa nije odbijeno.") {
		t.Error("stranica ne javlja da se zapis ne da pročitati")
	}
}

func TestOpisOdbijenog(t *testing.T) {
	for razlog, want := range map[string]string{
		"nije-clan":             "računalo nije član mreže ili je opozvano",
		"oznaka web/1":          "oznaka mogućnosti web/1",
		"nepoznat-zapis ustave": "vrsta zapisa koju program ne poznaje: ustave",
		"kod 0x0999":            "kod 0x0999",
	} {
		if got := opisRazloga(razlog); got != want {
			t.Errorf("opisRazloga(%q) = %q, želim %q", razlog, got, want)
		}
	}
	if got := danOdbijenog("2026-01-02"); got != "2. 1." {
		t.Errorf("dan %q", got)
	}
	if got := danOdbijenog("drugi dan"); got != "drugi dan" {
		t.Errorf("neispravan dan %q", got)
	}
	var data SyncPageData
	(&SyncHandler{}).dodajOdbijeno(context.Background(), nil, &data)
	if data.Odbijeno != nil || data.OdbijenoGreska != "" {
		t.Errorf("bez ovlasti %+v", data)
	}
}

// Svaka vrata imaju natpis, i WEB sync, pa stupac puta nije prazan.
func TestNatpisVrataOdbijenog(t *testing.T) {
	for _, p := range []string{peers.PutVrataPort, peers.PutVrataTunel, peers.PutVrataWeb} {
		if opisPutaOdbijenog[p] == "" {
			t.Errorf("vrata %q nemaju natpis", p)
		}
	}
}
