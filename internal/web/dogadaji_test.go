package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"

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
