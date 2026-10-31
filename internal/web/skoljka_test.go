package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func oznakaIzOdgovora(t *testing.T, s *Server) (string, *httptest.ResponseRecorder) {
	t.Helper()
	w := httptest.NewRecorder()
	s.ShowRazmjenaZnak(w, httptest.NewRequest("GET", "/razmjena/znak", nil))
	var d map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatalf("odgovor nije JSON: %v u %q", err, w.Body.String())
	}
	return d["stanje"], w
}

// Oznaka razmjene u gornjoj traci je jedna riječ iz istog računa kao
// pločica. Čvor koji razmjenu ne prati, nije u mreži ili je u njoj sam nema
// točke; upozorenje WEB synca traži pozornost i izvan mreže; malo mjesta na
// disku na usklađenost ne utječe, kao ni na pločici.
func TestOznakaRazmjeneUGornjojTraci(t *testing.T) {
	malo := time.Now().Add(-2 * time.Minute)
	online := []CvorRazmjene{{Naziv: "laptop", Dostupnost: "online", Zadnja: &malo}}
	for ime, slucaj := range map[string]struct {
		st   *RazmjenaStanje
		zeli string
	}{
		"ne prati":                {nil, "nema"},
		"izvan mreže":             {&RazmjenaStanje{}, "nema"},
		"izvan mreže, upozorenje": {&RazmjenaStanje{Upozorenja: []string{"WEB sync se ne javlja."}}, "ceka"},
		"sam u mreži":             {&RazmjenaStanje{UMrezi: true}, "nema"},
		"usklađeno":               {&RazmjenaStanje{UMrezi: true, Cvorovi: online}, "uskladjeno"},
		"malo mjesta ne smeta":    {&RazmjenaStanje{UMrezi: true, Cvorovi: online, Disk: []DiskRazmjene{{Poruka: "malo mjesta"}}}, "uskladjeno"},
		"čvor ne odgovara":        {&RazmjenaStanje{UMrezi: true, Cvorovi: []CvorRazmjene{{Naziv: "laptop", Dostupnost: "offline", Zadnja: &malo}}}, "ceka"},
		"arhiva čeka":             {&RazmjenaStanje{UMrezi: true, Cvorovi: online, Arhiva: ArhivaRazmjene{Ceka: 3}}, "ceka"},
		"upozorenje u mreži":      {&RazmjenaStanje{UMrezi: true, Cvorovi: online, Upozorenja: []string{"Sastajalište bez prijave."}}, "ceka"},
	} {
		s := &Server{}
		if slucaj.st != nil {
			st := *slucaj.st
			s.SetRazmjena(func(context.Context) RazmjenaStanje { return st })
		}
		stanje, w := oznakaIzOdgovora(t, s)
		if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
			t.Errorf("%s: odgovor %d, zaglavlja %v", ime, w.Code, w.Header())
		}
		if stanje != slucaj.zeli {
			t.Errorf("%s: oznaka %q, želi %q", ime, stanje, slucaj.zeli)
		}
	}
}

// Oznaku traži svaka otvorena stranica svake minute, pa se stanje pamti pola
// minute i čvorovi se ne pitaju za svaku; nakon isteka računa se iznova
func TestOznakaRazmjenePamtiStanje(t *testing.T) {
	poziva := 0
	s := &Server{}
	s.SetRazmjena(func(context.Context) RazmjenaStanje {
		poziva++
		return RazmjenaStanje{UMrezi: true, Cvorovi: []CvorRazmjene{{Naziv: "laptop", Dostupnost: "online"}}}
	})
	for i := 0; i < 3; i++ {
		if stanje, _ := oznakaIzOdgovora(t, s); stanje != "uskladjeno" {
			t.Errorf("%d. put: %q", i+1, stanje)
		}
	}
	if poziva != 1 {
		t.Errorf("stanje razmjene računato %d puta za tri upita, želi 1", poziva)
	}
	s.znak.do = time.Now().Add(-time.Second)
	oznakaIzOdgovora(t, s)
	if poziva != 2 {
		t.Errorf("nakon isteka stanje računato %d puta, želi 2", poziva)
	}
}

// pisacKojiPada je odgovor čiji upis ne uspijeva (veza prekinuta)
type pisacKojiPada struct {
	zaglavlja http.Header
	kod       int
}

func (p *pisacKojiPada) Header() http.Header        { return p.zaglavlja }
func (p *pisacKojiPada) Write([]byte) (int, error)  { return 0, errors.New("veza prekinuta") }
func (p *pisacKojiPada) WriteHeader(statusCode int) { p.kod = statusCode }

// Kad se pločica ne može poslati (veza je u međuvremenu prekinuta),
// poslužitelj javi grešku, a ne šuti
func TestPlocicaRazmjeneJaviGreskuUpisa(t *testing.T) {
	s := &Server{}
	s.SetRazmjena(func(context.Context) RazmjenaStanje { return RazmjenaStanje{UMrezi: true} })
	p := &pisacKojiPada{zaglavlja: http.Header{}}
	s.ShowRazmjena(p, httptest.NewRequest("GET", "/razmjena/plocica", nil))
	if p.kod != http.StatusInternalServerError {
		t.Errorf("odgovor %d, želi 500", p.kod)
	}
}

// Oznaku razmjene vidi samo prijavljena osoba, kao i pločicu
func TestOznakaRazmjeneTraziPrijavu(t *testing.T) {
	p := noviPosluziteljOcitanja(t)
	w := p.posalji("", "GET", "/razmjena/znak", nil, "")
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/login") {
		t.Errorf("bez prijave: %d %s", w.Code, w.Header().Get("Location"))
	}
	w = p.posalji("uprava", "GET", "/razmjena/znak", nil, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"stanje":"nema"`) {
		t.Errorf("prijavljen, bez razmjene: %d %s", w.Code, w.Body.String())
	}
}
