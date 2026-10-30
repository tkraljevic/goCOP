package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"gocop/internal/service"
)

// Tok događaja otvoren je samo prijavljenima i ne dopušta čitanje s tuđih
// stranica: čvor je javno dostupan kroz tunel.
func TestDogadajiSamoPrijavljenima(t *testing.T) {
	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`"GET /api/events",\s*s\.authMiddleware\(`).Match(src) {
		t.Error("ruta /api/events mora biti iza authMiddleware")
	}

	h := NewSSEHandler(service.NewSSEBroker())
	ctx, prekini := context.WithCancel(context.Background())
	prekini()
	w := httptest.NewRecorder()
	h.ServeSSE(w, httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx))
	if v := w.Header().Get("Access-Control-Allow-Origin"); v != "" {
		t.Errorf("tok dopušta tuđe stranice: Access-Control-Allow-Origin %q", v)
	}

	// neprijavljen zahtjev ne dobiva tok, nego preusmjerenje na prijavu
	w = httptest.NewRecorder()
	(&Server{}).authMiddleware(http.HandlerFunc(h.ServeSSE)).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/events", nil))
	if w.Code != http.StatusSeeOther || w.Header().Get("Content-Type") == "text/event-stream" {
		t.Errorf("neprijavljen dobiva %d %q", w.Code, w.Header().Get("Content-Type"))
	}
}

// Otvoren tok događaja prestaje kad prijava prestane vrijediti (npr. nova
// lozinka, odjava): prijava se gleda uz svaki ping
func TestTokDogadajaPrestajeSPrijavom(t *testing.T) {
	stari := razmakPinga
	razmakPinga = 5 * time.Millisecond
	t.Cleanup(func() { razmakPinga = stari })

	var pitano atomic.Int32
	h := NewSSEHandler(service.NewSSEBroker())
	h.vrijedi = func(*http.Request) bool { return pitano.Add(1) < 3 }
	gotovo := make(chan struct{})
	go func() {
		defer close(gotovo)
		h.ServeSSE(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/events", nil))
	}()
	select {
	case <-gotovo:
	case <-time.After(5 * time.Second):
		t.Fatal("tok događaja ostao otvoren nakon kraja prijave")
	}
	if n := pitano.Load(); n != 3 {
		t.Errorf("prijava provjerena %d puta, želim 3", n)
	}
}

// prijavaJosVrijedi: vrijedi sesija iz kolačića; bez kolačića, s krivim
// kolačićem ili nakon odjave ne vrijedi, a greška baze nije odjava
func TestPrijavaJosVrijedi(t *testing.T) {
	o := novaOkolinaPrijave(t)
	ana := o.racun("ana", "anina-lozinka", false, false)
	s := &Server{authService: o.auth}
	ses, err := o.auth.OtvoriSesiju(ana, "192.168.1.50", "test")
	if err != nil {
		t.Fatal(err)
	}
	zahtjev := func(kolacic string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/api/events", nil)
		if kolacic != "" {
			r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: kolacic})
		}
		return r
	}
	if !s.prijavaJosVrijedi(zahtjev(ses.ID.String())) {
		t.Fatal("otvorena sesija mora vrijediti")
	}
	if s.prijavaJosVrijedi(zahtjev("")) || s.prijavaJosVrijedi(zahtjev("nije-uuid")) {
		t.Error("bez valjanog kolačića prijava ne vrijedi")
	}
	if err := o.auth.Logout(ses.ID); err != nil {
		t.Fatal(err)
	}
	if s.prijavaJosVrijedi(zahtjev(ses.ID.String())) {
		t.Error("nakon odjave prijava ne vrijedi")
	}
	_ = o.baza.Close() // baza namjerno zatvorena: kvar nije odjava
	if !s.prijavaJosVrijedi(zahtjev(ses.ID.String())) {
		t.Error("greška baze ne smije zatvoriti tok")
	}
}

// pokvarenaVeza je odgovor čija veza više ne prima podatke
type pokvarenaVeza struct{ *httptest.ResponseRecorder }

func (pokvarenaVeza) Write([]byte) (int, error) { return 0, http.ErrHandlerTimeout }

// Ping koji ne stigne do preglednika zatvara tok, kao i kraj prijave
func TestPingZatvaraTok(t *testing.T) {
	h := NewSSEHandler(service.NewSSEBroker())
	r := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	w := pokvarenaVeza{httptest.NewRecorder()}
	if h.ping(w, r, http.NewResponseController(w)) {
		t.Error("ping na prekinutu vezu mora zatvoriti tok")
	}
	ok := httptest.NewRecorder()
	if !h.ping(ok, r, http.NewResponseController(ok)) || ok.Body.String() != ": ping\n\n" {
		t.Errorf("ping: %q", ok.Body.String())
	}
}
