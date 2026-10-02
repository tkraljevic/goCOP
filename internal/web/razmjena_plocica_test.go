package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPlocicaRazmjene(t *testing.T) {
	sad := time.Now()
	malo := sad.Add(-2 * time.Minute)
	for _, slucaj := range []struct {
		ime     string
		st      RazmjenaStanje
		ocekuje []string
	}{
		{"bez mreže", RazmjenaStanje{}, []string{"nije ni u jednoj mreži"}},
		{"usklađeno", RazmjenaStanje{UMrezi: true, Verzija: "0.0.24-alfa",
			Cvorovi:  []CvorRazmjene{{Naziv: "COP Osijek (Unraid)", Dostupnost: "online", Zadnja: &malo, Verzija: "0.0.24-alfa"}},
			Prognoza: PrognozaRazmjene{Ima: true, Izdavac: "cop-osijek-unraid", Primljeno: true, Izdano: sad, Nastalo: sad}},
			[]string{"Sve je usklađeno s 1 čvorom", "COP Osijek (Unraid) na 0.0.24-alfa: zadnja razmjena prije 2 min", "s čvora cop-osijek-unraid", "ovaj čvor na 0.0.24-alfa"}},
		{"stariji program", RazmjenaStanje{UMrezi: true, Verzija: "0.0.24-alfa",
			Cvorovi: []CvorRazmjene{
				{Naziv: "laptop", Dostupnost: "online", Zadnja: &malo, Verzija: "0.0.23-alfa", Razlicita: true},
				{Naziv: "stari laptop", Dostupnost: "online", Zadnja: &malo, Razlicita: true}}},
			[]string{"<strong>laptop</strong><div class=\"reg-card-sub\">0.0.23-alfa — drukčija od ovog čvora</div>", "starija inačica (ne javlja je)"}},
		{"laptop izvan kuće", RazmjenaStanje{UMrezi: true,
			Cvorovi: []CvorRazmjene{{Naziv: "laptop", Dostupnost: "online", Zadnja: &malo, Zaostaje: 3, SamoDolazi: true, Greska: "dial tcp 192.168.1.96:4710: no route to host"}}},
			[]string{"na mreži", "javlja se sam (kroz tunel)"}},
		{"prva razmjena", RazmjenaStanje{UMrezi: true,
			Cvorovi:     []CvorRazmjene{{Naziv: "laptop", Dostupnost: "online", Zadnja: &malo, Primljeno: 5000, JosPrima: true}},
			Arhiva:      ArhivaRazmjene{UKazalu: 298, Ugradjeno: 168, Ceka: 130, CekaBajtova: 12_400_000, Trenutno: "ugradnja paketa osijek"},
			SadrzajCeka: 12, SadrzajBajtova: 3_000_000,
			Prognoza: PrognozaRazmjene{Ima: true, Nastalo: sad.Add(-4 * time.Hour), Izdano: sad.Add(-4 * time.Hour)}},
			[]string{"prima se još", "ugrađeno 168 od 298 paketa", "12 MB", "upravo: ugradnja paketa osijek", "width:56%", "čeka dohvat 12", "starije od tri sata"}},
	} {
		if slucaj.ime == "stariji program" && slucaj.st.Sredeno() {
			t.Error("čvor na drugom izdanju ne smije dati „sve je usklađeno”")
		}
		var b strings.Builder
		if err := razmjenaTmpl.Execute(&b, slucaj.st); err != nil {
			t.Fatalf("%s: %v", slucaj.ime, err)
		}
		for _, o := range slucaj.ocekuje {
			if !strings.Contains(b.String(), o) {
				t.Errorf("%s: nema %q u\n%s", slucaj.ime, o, b.String())
			}
		}
	}
}

// Ništa osim /static/ ne smije u priručnu memoriju posrednika: Cloudflare je
// izvoz prognoze čuvao 4 sata i davao ga i bez prijave.
func TestBezPriruckeMemorije(t *testing.T) {
	h := bezPriruckeMemorije(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for put, ocekivano := range map[string]string{"/prognoze.xlsx": "private, no-store", "/": "private, no-store", "/static/css/style.css": ""} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", put, nil))
		if got := w.Header().Get("Cache-Control"); got != ocekivano {
			t.Errorf("%s: Cache-Control %q, očekivano %q", put, got, ocekivano)
		}
	}
}
