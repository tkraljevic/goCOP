package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"gocop/internal/xlsxw"
)

// knjigaZaSlanje je mala knjiga s jednim listom
func knjigaZaSlanje() *xlsxw.Knjiga {
	l := &xlsxw.List{Naziv: "Proba"}
	l.Dodaj(xlsxw.T("Primjerovo"))
	return &xlsxw.Knjiga{Listovi: []*xlsxw.List{l}}
}

// Excel ide cijeli, s duljinom u zaglavlju.
func TestPosaljiXLSXCijelaDatoteka(t *testing.T) {
	w := httptest.NewRecorder()
	posaljiXLSX(w, "proba.xlsx", knjigaZaSlanje())
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	tijelo := w.Body.Bytes()
	if len(tijelo) < 4 || string(tijelo[:2]) != "PK" {
		t.Fatalf("nije zip: % x", tijelo[:min(len(tijelo), 8)])
	}
	if got := w.Header().Get("Content-Length"); got != strconv.Itoa(len(tijelo)) {
		t.Errorf("Content-Length %q, tijelo %d", got, len(tijelo))
	}
	if got := w.Header().Get("Content-Disposition"); got != `attachment; filename="proba.xlsx"` {
		t.Errorf("Content-Disposition %q", got)
	}
}

// prekinutaVeza je odgovor kojem preglednik usred slanja zatvori vezu
type prekinutaVeza struct {
	zaglavlje http.Header
	statusa   int
}

func (p *prekinutaVeza) Header() http.Header { return p.zaglavlje }

func (p *prekinutaVeza) WriteHeader(int) { p.statusa++ }

func (p *prekinutaVeza) Write([]byte) (int, error) {
	if p.statusa == 0 {
		p.statusa++
	}
	return 0, errors.New("broken pipe")
}

// Prekid veze usred slanja ne šalje grešku preko već poslanog zaglavlja
// (u dnevniku poslužitelja to je bilo „superfluous WriteHeader”).
func TestPosaljiXLSXPrekidVeze(t *testing.T) {
	p := &prekinutaVeza{zaglavlje: http.Header{}}
	posaljiXLSX(p, "proba.xlsx", knjigaZaSlanje())
	if p.statusa != 1 {
		t.Errorf("zaglavlje odgovora poslano %d puta", p.statusa)
	}
}
