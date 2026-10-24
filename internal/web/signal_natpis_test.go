package web

import (
	"context"
	"html/template"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/razmjena"
)

// Redak o WEB syncu: prijavljeno računalo kaže kod koga i od kada, a dok
// WEB sync ne nosi podatke, i da idu kao prije; sastajalište broji
// prijavljene, pozive i prekide s hrvatskim oblicima uz broj
func TestRedakSignala(t *testing.T) {
	od := time.Date(2026, 11, 4, 11, 1, 0, 0, time.UTC) // 12:01 u Zagrebu
	for _, c := range []struct {
		ime string
		st  SignalStanje
		je  string
	}{
		{"prazno", SignalStanje{}, ""},
		{"kvar", SignalStanje{Posluzitelj: "cop.primjer.hr", Kod: razmjena.KodSignalaNedostupno, Od: od}, ""},
		{"prijavljen", SignalStanje{Posluzitelj: "cop.primjer.hr", Prijavljen: true, Od: od},
			"WEB sync: prijavljen kod cop.primjer.hr od 12:01. Podaci još idu kao prije."},
		{"prijavljen, WEB sync radi", SignalStanje{Posluzitelj: "cop.primjer.hr", Prijavljen: true, Od: od, WebSync: true},
			"WEB sync: prijavljen kod cop.primjer.hr od 12:01."},
		{"sastajalište", SignalStanje{Sastajaliste: true, Prijavljenih: 3, PozivaSat: 1, Prekida24h: 0},
			"Sastajalište za WEB sync: 3 prijavljena računala, 1 poziv u zadnjem satu, 0 prekida u 24 h."},
		{"sastajalište, 21", SignalStanje{Sastajaliste: true, Prijavljenih: 21, PozivaSat: 12, Prekida24h: 2},
			"Sastajalište za WEB sync: 21 prijavljeno računalo, 12 poziva u zadnjem satu, 2 prekida u 24 h."},
	} {
		if got := c.st.stanjeRecenica(); got != c.je {
			t.Errorf("%s: %q, a treba %q", c.ime, got, c.je)
		}
	}
	if (&Server{}).redakSignala() != "" || len((&Server{}).upozorenjaPovezivosti()) != 0 {
		t.Error("poslužitelj bez stanja sastajališta")
	}
}

// Prolazno stanje dolazi na pločicu tek nakon 10 s, a trajno odmah. Svaki
// kod ima rečenicu bez ostatka oblikovanja; prijavljeno računalo i
// sastajalište nemaju upozorenja o prijavi.
func TestUpozorenjaSignala(t *testing.T) {
	sad := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	kodovi := []razmjena.KodSignala{razmjena.KodSignalaSpajaSe, razmjena.KodSignalaNedostupno, razmjena.KodSignalaNemaRute,
		razmjena.KodSignalaKriviKljuc, razmjena.KodSignalaNijeClan, razmjena.KodSignalaKopija, razmjena.KodSignalaBezPostavke}
	for _, k := range kodovi {
		for _, traje := range []time.Duration{0, 9 * time.Second, odgodaProlaznog, time.Hour} {
			st := SignalStanje{Posluzitelj: "cop.primjer.hr", Kod: k, Od: sad.Add(-traje)}
			u := st.upozorenja(sad)
			if ocekuje := k.Trajan() || traje >= odgodaProlaznog; (len(u) == 1) != ocekuje {
				t.Errorf("%q, traje %v: %q", k, traje, u)
				continue
			}
			for _, r := range u {
				if r == "" || strings.Contains(r, "%") || strings.Contains(r, "  ") {
					t.Errorf("%q: rečenica %q", k, r)
				}
			}
			for _, bez := range []SignalStanje{{Prijavljen: true, Kod: k, Od: st.Od}, {Sastajaliste: true, Kod: k, Od: st.Od}} {
				if u := bez.upozorenja(sad); len(u) != 0 {
					t.Errorf("%q uz prijavu ili na sastajalištu: %q", k, u)
				}
			}
		}
	}
	if u := (SignalStanje{Kod: "nepoznat", Od: sad.Add(-time.Hour)}).upozorenja(sad); len(u) != 0 {
		t.Errorf("nepoznat kod: %q", u)
	}
	if u := (SignalStanje{Posluzitelj: "cop.primjer.hr", Kod: razmjena.KodSignalaNemaRute, Od: sad}).upozorenja(sad); u[0] !=
		"cop.primjer.hr nije sastajalište za WEB sync. Na stalnom čvoru uključite posluzi_sastajaliste ili ispravite sastajaliste u gocop.toml." {
		t.Errorf("nema rute: %q", u)
	}
}

// Kopija: odbijeno računalo zna za koliko minuta pokušava ponovno (barem
// 1), a ono koje je ostalo prijavljeno samo da treba ugasiti kopiju. Sat
// koji odstupa više od 2 min javlja se uz prijavu i bez nje.
func TestKopijaISat(t *testing.T) {
	sad := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		ime string
		st  SignalStanje
		je  []string
	}{
		{"odbijeno", SignalStanje{Kod: razmjena.KodSignalaKopija, Odbijen: true, Od: sad, PonovnoU: sad.Add(10 * time.Minute)},
			[]string{"Na još jednom računalu radi kopija ovog goCOP-a. Ugasite kopiju; WEB sync ovdje pokušava ponovno za 10 min."}},
		{"odbijeno, rok prošao", SignalStanje{Kod: razmjena.KodSignalaKopija, Odbijen: true, Od: sad, PonovnoU: sad.Add(-time.Minute)},
			[]string{"Na još jednom računalu radi kopija ovog goCOP-a. Ugasite kopiju; WEB sync ovdje pokušava ponovno za 1 min."}},
		{"drugo računalo", SignalStanje{Kod: razmjena.KodSignalaKopija, Od: sad},
			[]string{"Na još jednom računalu radi kopija ovog goCOP-a. Ugasite kopiju."}},
		{"sat naprijed", SignalStanje{Prijavljen: true, OdstupanjeSata: 3 * time.Minute},
			[]string{"Sat ovog računala odstupa 3 min od poslužitelja. Uključite automatsko podešavanje sata."}},
		{"sat natrag", SignalStanje{OdstupanjeSata: -4*time.Minute - 20*time.Second},
			[]string{"Sat ovog računala odstupa 4 min od poslužitelja. Uključite automatsko podešavanje sata."}},
		{"sat unutar 2 min", SignalStanje{Prijavljen: true, OdstupanjeSata: 2 * time.Minute}, nil},
	} {
		if got := c.st.upozorenja(sad); strings.Join(got, "|") != strings.Join(c.je, "|") {
			t.Errorf("%s: %q", c.ime, got)
		}
	}
}

// Pločica i blok Dohvatljivost: redak o prijavi i upozorenje o kvaru stoje
// uz ostalo, a pločica nije „sve je usklađeno” dok kvar traje
func TestSignalNaPlociciIDohvatljivosti(t *testing.T) {
	malo := time.Now().Add(-2 * time.Minute)
	s, _ := cvorSPostavkama(t, sadrzajPostavk, nil)
	s.SetRazmjena(func(context.Context) RazmjenaStanje {
		return RazmjenaStanje{UMrezi: true, Cvorovi: []CvorRazmjene{{Naziv: "laptop", Dostupnost: "online", Zadnja: &malo}}}
	})
	stanje := SignalStanje{Posluzitelj: "cop.primjer.hr", Prijavljen: true, Od: malo}
	s.SetSignalStanje(func() SignalStanje { return stanje })
	plocica := func() string {
		w := httptest.NewRecorder()
		s.ShowRazmjena(w, httptest.NewRequest("GET", "/razmjena/plocica", nil))
		return w.Body.String()
	}
	prijava := "WEB sync: prijavljen kod cop.primjer.hr od " + malo.In(models.Zagreb).Format("15:04") + "."
	if html := plocica(); !strings.Contains(html, prijava) || !strings.Contains(html, "Sve je usklađeno") {
		t.Errorf("prijavljen: %s", html)
	}
	if d := s.dohvatljivost(); !strings.HasPrefix(d.Signal, prijava) || len(d.Upozorenja) != 0 {
		t.Errorf("blok uz prijavu: %+v", d)
	}
	stanje = SignalStanje{Posluzitelj: "cop.primjer.hr", Kod: razmjena.KodSignalaKriviKljuc, Od: time.Now()}
	kvar := template.HTMLEscapeString("Na adresi cop.primjer.hr odgovara drugo računalo")
	if html := plocica(); !strings.Contains(html, kvar) || strings.Contains(html, "Sve je usklađeno") || strings.Contains(html, "WEB sync: prijavljen") {
		t.Errorf("krivi ključ: %s", html)
	}
	if d := s.dohvatljivost(); d.Signal != "" || len(d.Upozorenja) != 1 {
		t.Errorf("blok uz krivi ključ: %+v", d)
	}
	var b strings.Builder
	if err := razmjenaTmpl.Execute(&b, RazmjenaStanje{Signal: prijava}); err != nil || strings.Contains(b.String(), prijava) {
		t.Errorf("redak bez mreže: %v", err)
	}
}

// Uz računalo do kojeg ovo nema puta stoji kuda podaci idu; trajan razlog
// ostaje. Pauza kaže kad WEB sync pokušava ponovno.
func TestRazlogPrekoPosluziteljaIPauza(t *testing.T) {
	for _, c := range []struct {
		ime string
		c   CvorRazmjene
		je  string
	}{
		{"bez puta", CvorRazmjene{Dostupnost: "offline", RazlogPuta: razmjena.RazlogPutaBezOdgovora, PrekoPosluzitelja: "cop.primjer.hr"},
			"SERVER sync, preko cop.primjer.hr: podaci stižu do 10 min kasnije."},
		{"bez imena", CvorRazmjene{Dostupnost: "never", PrekoPosluzitelja: "poslužitelja"},
			"SERVER sync, preko poslužitelja: podaci stižu do 10 min kasnije."},
		{"trajan razlog", CvorRazmjene{Dostupnost: "offline", RazlogPuta: razmjena.RazlogPutaNijeClan, PrekoPosluzitelja: "cop.primjer.hr"},
			razlogRijecima(razmjena.RazlogPutaNijeClan, "")},
		{"pauza", CvorRazmjene{Dostupnost: "online", Put: razmjena.NazivServer, RazlogPuta: razmjena.RazlogPutaPauza,
			PonovnoU: time.Date(2026, 11, 4, 13, 30, 0, 0, time.UTC)},
			"WEB sync s ovim računalom ne uspijeva; podaci idu preko poslužitelja (SERVER sync), a WEB sync pokušava ponovno u 14:30."},
		{"pauza bez vremena", CvorRazmjene{Dostupnost: "online", Put: razmjena.NazivServer, RazlogPuta: razmjena.RazlogPutaPauza},
			"WEB sync s ovim računalom ne uspijeva; podaci idu preko poslužitelja (SERVER sync)."},
	} {
		if got := c.c.Razlog(); got != c.je {
			t.Errorf("%s: %q", c.ime, got)
		}
	}
}
