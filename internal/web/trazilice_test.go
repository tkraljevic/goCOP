package web

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/razmjena"
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"
)

// posluziteljZaTrazilice je pravi poslužitelj sa svim rutama i zaštitnim
// slojevima, s praznom bazom korisnika: dovoljno za stranice koje se vide
// bez prijave
func posluziteljZaTrazilice(t *testing.T) *Server {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "trazilice.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	userRepo := repository.NewUserRepository(baza, ledger.New(baza, "test"))
	auth := service.NewAuthService(userRepo, repository.NewSessionRepository(baza))
	users := service.NewUserService(userRepo, auth, service.NewSSEBroker())
	s, err := NewServer("", auth, users, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, SupportContact{}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// robots.txt vide i tražilice, koje se nikad ne prijavljuju: odgovor je
// izravan, bez preusmjeravanja na prijavu, i zabranjuje sve osim ulaza i
// prijave. Isto vrijedi izvana, kroz tunel, gdje nosi i sigurnosna
// zaglavlja kao svaki drugi odgovor.
func TestRobotsTxtBezPrijave(t *testing.T) {
	h := posluziteljZaTrazilice(t).Handler()
	for ime, zag := range map[string]map[string]string{
		"lokalna mreža": nil,
		"kroz tunel":    {"CF-Connecting-IP": "203.0.113.9", "X-Forwarded-Proto": "https"},
	} {
		for _, metoda := range []string{http.MethodGet, http.MethodHead} {
			r := httptest.NewRequest(metoda, "/robots.txt", nil)
			r.RemoteAddr = "127.0.0.1:5000"
			r.Host = adresaCvoraTesta
			for k, v := range zag {
				r.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusOK || w.Header().Get("Location") != "" {
				t.Errorf("%s, %s: %d %q", ime, metoda, w.Code, w.Header().Get("Location"))
			}
			if v := w.Header().Get("Content-Type"); v != "text/plain; charset=utf-8" {
				t.Errorf("%s, %s: vrsta %q", ime, metoda, v)
			}
			if metoda == http.MethodGet && w.Body.String() != "User-agent: *\nAllow: /$\nAllow: /login$\nDisallow: /\n" {
				t.Errorf("%s: robots.txt = %q", ime, w.Body.String())
			}
			if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("X-Frame-Options") != "DENY" {
				t.Errorf("%s, %s: bez sigurnosnih zaglavlja", ime, metoda)
			}
			if hsts := w.Header().Get("Strict-Transport-Security"); (hsts != "") != (zag != nil) {
				t.Errorf("%s, %s: HSTS %q", ime, metoda, hsts)
			}
		}
	}
}

// dopustenoTrazilici čita robots.txt kao Google i Bing: od pravila koja se
// poklapaju s početkom puta vrijedi najdulje, a $ na kraju traži cijeli put
func dopustenoTrazilici(robots, put string) bool {
	dopusteno, najdulje := true, 0
	for _, redak := range strings.Split(robots, "\n") {
		pravilo, uzorak, _ := strings.Cut(redak, ":")
		uzorak = strings.TrimSpace(uzorak)
		poklapa := uzorak != "" && strings.HasPrefix(put, uzorak)
		if bez, cijeli := strings.CutSuffix(uzorak, "$"); cijeli {
			poklapa = put == bez
		}
		if poklapa && (pravilo == "Allow" || pravilo == "Disallow") && len(uzorak) > najdulje {
			dopusteno, najdulje = pravilo == "Allow", len(uzorak)
		}
	}
	return dopusteno
}

// Tražilica koja poštuje robots.txt stranicu koju on zabrani ne otvara, pa
// ne vidi ni noindex, a adresu s tuđe poveznice ipak smije upisati, bez
// sadržaja. Zato ulaz i prijavu, kamo vodi svaka adresa bez prijave, smije
// otvoriti i ondje vidi noindex; ostalo joj je zabranjeno.
func TestTrazilicaNaUlazuVidiNoindex(t *testing.T) {
	h := posluziteljZaTrazilice(t).Handler()
	zovi := func(put string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, put, nil)
		r.RemoteAddr = "127.0.0.1:5000"
		r.Host = adresaCvoraTesta
		r.Header.Set("CF-Connecting-IP", "203.0.113.9")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	robots := zovi("/robots.txt").Body.String()
	if w := zovi("/"); w.Header().Get("Location") != "/login" {
		t.Fatalf("ulaz bez prijave: %d %q", w.Code, w.Header().Get("Location"))
	}
	for _, put := range []string{"/", "/login"} {
		if !dopustenoTrazilici(robots, put) {
			t.Errorf("tražilica ne smije otvoriti %q, pa ne vidi noindex", put)
		}
		if v := zovi(put).Header().Get("X-Robots-Tag"); !strings.Contains(v, "noindex") {
			t.Errorf("%q: X-Robots-Tag = %q", put, v)
		}
	}
	for _, put := range []string{"/pomoc", "/login/pin", "/login?x=1", "/static/css/style.css", "/razmjena/tunel", "/profile"} {
		if dopustenoTrazilici(robots, put) {
			t.Errorf("tražilica smije otvoriti %q", put)
		}
	}
}

// Svaki odgovor web sučelja nosi X-Robots-Tag: stranica, statična
// datoteka, preusmjeravanje na prijavu, nepostojeća stranica, preuzimanje
// i odbijanje s tuđe stranice. Rukovatelj koji postavlja svoja zaglavlja
// (privitak) oznaku ne gubi.
func TestSkrivenoOdTrazilica(t *testing.T) {
	s := posluziteljZaTrazilice(t)
	s.mux.HandleFunc("GET /proba/izvoz.pdf", func(w http.ResponseWriter, r *http.Request) {
		posluziTudjuDatoteku(w, "application/pdf", "izvoz.pdf", false)
		_, _ = w.Write([]byte("%PDF-1.7"))
	})
	h := s.Handler()
	for _, c := range []struct {
		ime, metoda, put string
		kod              int
	}{
		{"stranica za prijavu", "GET", "/login", http.StatusOK},
		{"statična datoteka", "GET", "/static/css/style.css", http.StatusOK},
		{"preusmjeravanje na prijavu", "GET", "/", http.StatusSeeOther},
		{"nepostojeća stranica", "GET", "/nema-ove-stranice", http.StatusNotFound},
		{"preuzimanje", "GET", "/proba/izvoz.pdf", http.StatusOK},
		{"zdravlje", "GET", "/zdravlje", http.StatusOK},
		{"tuđa stranica", "POST", "/logout", http.StatusForbidden},
	} {
		r := httptest.NewRequest(c.metoda, c.put, nil)
		r.RemoteAddr = "127.0.0.1:5000"
		r.Host = "localhost:1160"
		if c.metoda == "POST" {
			r.Header.Set("Sec-Fetch-Site", "cross-site")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.kod {
			t.Errorf("%s: %d, očekivano %d", c.ime, w.Code, c.kod)
		}
		if v := w.Header().Get("X-Robots-Tag"); v != "noindex, nofollow, noarchive, nosnippet, noimageindex" {
			t.Errorf("%s: X-Robots-Tag = %q", c.ime, v)
		}
	}
}

// Čvor koji nije smio krenuti i dalje odgovara na svojem portu: i on je
// skriven od tražilica
func TestOdbijenoPokretanjeSkrivenoOdTrazilica(t *testing.T) {
	h := OdbijenoPokretanje("Ključ čvora nedostaje.")
	for put, kod := range map[string]int{"/": http.StatusServiceUnavailable, "/robots.txt": http.StatusOK} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", put, nil))
		if w.Code != kod || w.Header().Get("X-Robots-Tag") == "" {
			t.Errorf("%s: %d, X-Robots-Tag %q", put, w.Code, w.Header().Get("X-Robots-Tag"))
		}
	}
}

// Svaki predložak koji je cijela HTML stranica (base i stranice koje ga
// ne nasljeđuju: prijava, PIN, uparivanje, postavljanje, ispis dnevnika)
// u zaglavlju nosi oznaku za tražilice
func TestPredlosciNoseOznakuZaTrazilice(t *testing.T) {
	predlosci, err := fs.Glob(webassets.Files, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	glava := regexp.MustCompile(`(?s)<head>.*<meta name="robots" content="noindex, nofollow">.*</head>`)
	stranice := map[string]bool{}
	for _, p := range predlosci {
		raw, err := fs.ReadFile(webassets.Files, p)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(bytes.ToLower(raw), []byte("<!doctype html>")) {
			continue
		}
		stranice[filepath.Base(p)] = true
		if !glava.Match(raw) {
			t.Errorf("%s nema <meta name=\"robots\"> u zaglavlju", p)
		}
	}
	for _, ime := range []string{"base.html", "login.html", "login_pin.html"} {
		if !stranice[ime] {
			t.Errorf("%s nije prepoznat kao cijela stranica", ime)
		}
	}
}

// Razmjena kroz tunel ide kroz isti web poslužitelj, sa svim slojevima:
// oznaka za tražilice ne smije smetati nadogradnji na WebSocket ni
// promijeniti ijedan bajt razmjene
func TestRazmjenaKrozTunelIzaSkrivanja(t *testing.T) {
	_, kljucA, _ := ed25519.GenerateKey(rand.Reader)
	_, kljucB, _ := ed25519.GenerateKey(rand.Reader)
	javniA := kljucA.Public().(ed25519.PublicKey)
	javniB := kljucB.Public().(ed25519.PublicKey)

	tunel := razmjena.NoviTunel()
	defer tunel.Close()
	s := &Server{mux: http.NewServeMux()}
	s.mux.Handle("GET "+razmjena.PutTunela, ograditiTunel(tunel.Handler()))
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	ctx, otkazi := context.WithTimeout(context.Background(), 20*time.Second)
	defer otkazi()
	vrati := func(c *razmjena.Conn) {
		defer c.Close()
		e, err := c.Receive()
		if err != nil {
			return
		}
		_ = c.Send(razmjena.Envelope{Kind: "odgovor", Payload: e.Payload})
	}
	go func() {
		_ = razmjena.ServeExchangeOn(ctx, kljucB, "gocop-test", nil, tunel, func(k ed25519.PublicKey, _ []byte) bool { return k.Equal(javniA) }, vrati)
	}()

	ws, err := websocket.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+razmjena.PutTunela, "", srv.URL)
	if err != nil {
		t.Fatalf("nadogradnja na WebSocket: %v", err)
	}
	ws.PayloadType = websocket.BinaryFrame
	c, err := razmjena.DialExchangeOn(ctx, ws, kljucA, "gocop-test", nil, srv.URL, javniB)
	if err != nil {
		t.Fatalf("razmjena kroz tunel: %v", err)
	}
	defer c.Close()
	sviBajtovi := make([]byte, 256)
	for i := range sviBajtovi {
		sviBajtovi[i] = byte(i)
	}
	pitanje, _ := razmjena.NewEnvelope("frontier", sviBajtovi)
	if err := c.Send(pitanje); err != nil {
		t.Fatal(err)
	}
	var vraceno []byte
	odg, err := c.Receive()
	if err != nil || odg.Kind != "odgovor" || odg.Decode(&vraceno) != nil || !bytes.Equal(vraceno, sviBajtovi) {
		t.Errorf("odgovor kroz tunel: %q %v", odg.Kind, err)
	}
}
