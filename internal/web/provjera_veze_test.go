package web

// Provjera veze (provjera_veze.go): gumb samo administratoru izravno iz
// lokalne mreže, u bloku Dohvatljivost s javnom adresom i kandidatima, i
// upozorenje kad WEB sync u spremniku nema lan_adresa.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"regexp"
	"strings"
	"testing"
	"time"

	"gocop/internal/config"
	"gocop/internal/models"
	"gocop/internal/razmjena"
)

// ishodProvjere je provjera s javnom adresom, kandidatima i izlazom kroz
// Tailscale, kakvu vraća program
func ishodProvjere() ProvjeraVeze {
	javna := netip.MustParseAddrPort("203.0.113.7:4713")
	return ProvjeraVeze{Kad: time.Date(2026, 10, 23, 14, 3, 0, 0, time.UTC), Port: 4713, Vrijedi: razmjena.NATLak,
		Mjerenje: razmjena.MjerenjeMreze{Vrsta: razmjena.NATLak, CuvaPort: true, Javna: javna,
			Odgovori: []razmjena.OdgovorPosluzitelja{{Ime: "Cloudflare", Razrijesen: true, Odgovorio: true, Javna: javna, RTT: 8 * time.Millisecond}}},
		Kandidati: []razmjena.Kandidat{{Vrsta: razmjena.KandidatJavni, Adresa: javna},
			{Vrsta: razmjena.KandidatLokalni, Adresa: netip.MustParseAddrPort("192.168.1.10:4713"), Sucelje: "en0"}},
		Razlozi: []string{"10.20.0.5 na eth2: promet prema toj mreži izlazi s 100.101.0.10"},
		Izlazi:  []razmjena.IzlazSTUN{{Ime: "Google", Kroz: razmjena.IzlazTailscale}, {Ime: "Cloudflare"}}}
}

// lazniProvjeravatelj broji mjerenja i vraća zadani ishod
type lazniProvjeravatelj struct {
	ishod  ProvjeraVeze
	zadnja *ProvjeraVeze
	puta   int
}

func (l *lazniProvjeravatelj) postavi(s *Server) {
	s.SetProvjeraVeze(func(context.Context) ProvjeraVeze {
		l.puta++
		l.zadnja = &l.ishod
		return l.ishod
	}, func() *ProvjeraVeze { return l.zadnja })
}

var reAdresa = regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}\b`)

func TestSazetakProvjere(t *testing.T) {
	r := ishodProvjere()
	if got := r.Sazetak(); got != "UDP radi na :4713. NAT je lak i čuva port. STUN: Cloudflare 8 ms." {
		t.Errorf("sažetak: %q", got)
	}
	r.Mjerenje = razmjena.MjerenjeMreze{Vrsta: razmjena.UDPBlokiran, Odgovori: []razmjena.OdgovorPosluzitelja{{Ime: "Google", Razrijesen: true}}}
	if got := r.Sazetak(); got != "UDP utičnica je na :4713, ali odgovora nema. UDP do STUN poslužitelja ne prolazi. STUN: Google bez odgovora." {
		t.Errorf("blokiran: %q", got)
	}
	r.Mjerenje.Vrsta = razmjena.NATNepoznat
	if got := r.Sazetak(); !strings.HasPrefix(got, "UDP radi na :4713. Vrsta NAT-a se ne zna.") {
		t.Errorf("nepoznato: %q", got)
	}
	if got := (ProvjeraVeze{Greska: "Provjera mreže je isključena."}).Sazetak(); got != "Provjera mreže je isključena." {
		t.Errorf("greška: %q", got)
	}
}

// Gumb Provjeri vezu je u bloku Dohvatljivost na Stanju razmjene (Odluke
// (30. 10. 2026.), 3): samo administratoru lokalno, a izvana razlog. Bez
// provjere u programu gumba nema.
func TestProvjeraVezeNaStanjuRazmjene(t *testing.T) {
	s, _ := cvorSPostavkama(t, sadrzajPostavk, nil)
	tmpl := predlozakStranice(t, "sinkronizacija.html")
	iscrtaj := func(u uvjetiZahtjeva) string {
		t.Helper()
		h := &SyncHandler{dohvatljivost: s.dohvatljivost, gumbProvjere: s.gumbProvjereVeze}
		r := zahtjevPostavki(http.MethodGet, "/sinkronizacija", nil, u)
		data := SyncPageData{CurrentUser: adminIvic, Permissions: u.perms}
		h.dodajDohvatljivost(r, &data)
		var b strings.Builder
		if err := tmpl.ExecuteTemplate(&b, "sinkronizacija.html", data); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	if html := iscrtaj(kaoAdmin); strings.Contains(html, `id="provjera-veze"`) {
		t.Error("gumb bez provjere u programu")
	}
	l := &lazniProvjeravatelj{ishod: ishodProvjere()}
	l.postavi(s)
	html := iscrtaj(kaoAdmin)
	if !strings.Contains(html, `id="provjera-veze"`) || !strings.Contains(html, `action="/settings/provjeri-vezu"`) {
		t.Error("administrator: nema gumba Provjeri vezu")
	}
	if i, j := strings.Index(html, `id="dohvatljivost"`), strings.Index(html, `id="provjera-veze"`); i < 0 || j < i {
		t.Error("gumb nije u bloku Dohvatljivost")
	}
	izvana := kaoAdmin
	izvana.adresa = adresaIzvana
	if html := iscrtaj(izvana); strings.Contains(html, `action="/settings/provjeri-vezu"`) || !strings.Contains(html, zabranaIzvana.provjera) {
		t.Error("izvana: gumb ili bez razloga")
	}
}

// Obrađivač: bez prava 403 bez mjerenja; inače mjeri jednom i vraća na
// karticu s porukom, uspjeh bez adresa.
func TestHandleProvjeriVezu(t *testing.T) {
	s, _ := cvorSPostavkama(t, sadrzajPostavk, nil)
	salji := func(u uvjetiZahtjeva) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.handleProvjeriVezu(w, zahtjevPostavki(http.MethodPost, "/settings/provjeri-vezu", nil, u))
		return w
	}
	if w := salji(kaoAdmin); w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "error=") {
		t.Errorf("bez provjere u programu: %d %q", w.Code, w.Header().Get("Location"))
	}
	l := &lazniProvjeravatelj{ishod: ishodProvjere()}
	l.postavi(s)
	obican := uvjetiZahtjeva{perms: &models.UserPermissions{}, adresa: adresaLokalno, korisnik: adminIvic}
	if w := salji(obican); w.Code != http.StatusForbidden || l.puta != 0 {
		t.Errorf("bez prava: %d, mjerenja %d", w.Code, l.puta)
	}
	w := salji(kaoAdmin)
	lok := w.Header().Get("Location")
	if w.Code != http.StatusSeeOther || !strings.Contains(lok, "success=") || !strings.HasPrefix(lok, "/sinkronizacija?") || !strings.HasSuffix(lok, "#dohvatljivost") || l.puta != 1 || reAdresa.MatchString(lok) {
		t.Errorf("uspjeh: %d %q, mjerenja %d", w.Code, lok, l.puta)
	}
	l.ishod = ProvjeraVeze{Greska: "Provjera mreže je isključena (stun = [] u gocop.toml)."}
	if w := salji(kaoAdmin); !strings.Contains(w.Header().Get("Location"), "error=") {
		t.Errorf("greška: %q", w.Header().Get("Location"))
	}
}

// Blok Dohvatljivost na Sinkronizaciji: javna adresa, kandidati, izlaz kroz
// Tailscale i razlozi, samo administratoru; prije provjere rečenica kako je
// pokrenuti.
func TestDohvatljivostNaSinkronizaciji(t *testing.T) {
	s, _ := cvorSPostavkama(t, sadrzajPostavk, nil)
	tmpl := predlozakStranice(t, "sinkronizacija.html")
	iscrtaj := func(admin bool) string {
		h := &SyncHandler{dohvatljivost: s.dohvatljivost}
		data := SyncPageData{CurrentUser: adminIvic, Permissions: &models.UserPermissions{IsGlobalAdmin: admin}}
		h.dodajDohvatljivost(httptest.NewRequest(http.MethodGet, "/sinkronizacija", nil), &data)
		var b strings.Builder
		if err := tmpl.ExecuteTemplate(&b, "sinkronizacija.html", data); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	l := &lazniProvjeravatelj{ishod: ishodProvjere()}
	l.postavi(s)
	if html := iscrtaj(true); !strings.Contains(html, "Provjere još nije bilo") {
		t.Error("prije provjere: nema uputa")
	}
	l.zadnja = &l.ishod
	html := iscrtaj(true)
	for _, mora := range []string{`id="dohvatljivost"`, "203.0.113.7:4713", "192.168.1.10:4713 (en0)", "203.0.113.7:4713 (javna)",
		"Promet prema Google ide kroz Tailscale", "Vrijedi: NAT je lak.", "10.20.0.5 na eth2"} {
		if !strings.Contains(html, mora) {
			t.Errorf("blok: nema %q", mora)
		}
	}
	if html := iscrtaj(false); strings.Contains(html, `id="dohvatljivost"`) || strings.Contains(html, "203.0.113.7") {
		t.Error("blok vidi korisnik koji nije administrator")
	}
	// poslužitelj bez bloka (stari testovi, SyncHandler bez dohvatljivosti)
	data := SyncPageData{Permissions: &models.UserPermissions{IsGlobalAdmin: true}}
	(&SyncHandler{}).dodajDohvatljivost(httptest.NewRequest(http.MethodGet, "/sinkronizacija", nil), &data)
	if data.Dohvatljivost != nil {
		t.Error("blok bez izvora")
	}
}

// U spremniku u mostu bez lan_adresa: pločica upozori uz prijavu za WEB
// sync, a blok Dohvatljivost i nakon provjere bez prijave. S lan_adresa,
// izvan spremnika ili kad je provjera našla adresu izvan Dockerove mreže
// (host, macvlan) upozorenja nema.
func TestUpozorenjeLanAdrese(t *testing.T) {
	u172 := []razmjena.Kandidat{{Vrsta: razmjena.KandidatLokalni, Adresa: netip.MustParseAddrPort("172.17.0.2:4713"), Sucelje: "eth0"}}
	u192 := []razmjena.Kandidat{{Vrsta: razmjena.KandidatLokalni, Adresa: netip.MustParseAddrPort("192.168.1.10:4713"), Sucelje: "eth0"}}
	for _, c := range []struct {
		ime           string
		nacin         NacinRada
		prijava       bool
		lan           string
		provjera      []razmjena.Kandidat // nil: provjere nije bilo
		plocica, blok bool
	}{
		{"most, prijava", USpremniku, true, "", nil, true, true},
		{"most, prijava, provjera", USpremniku, true, "", u172, true, true},
		{"host, prijava, provjera", USpremniku, true, "", u192, false, false},
		{"s lan_adresa", USpremniku, true, "192.168.1.2", u172, false, false},
		{"bez prijave, bez provjere", USpremniku, false, "", nil, false, false},
		{"bez prijave, provjera", USpremniku, false, "", u172, false, true},
		{"izvan spremnika", UslugaSustava, true, "", u172, false, false},
	} {
		s, _ := cvorSPostavkama(t, sadrzajPostavk, nil)
		s.SetPonovnoPokretanje(c.nacin, func() {})
		p := s.postavke.snimka()
		p.Vrijedi.Povezivost = config.Povezivost{WebSyncPrijava: c.prijava, LanAdresa: c.lan}
		s.SetPostavkeCvora(p)
		if c.provjera != nil {
			l := &lazniProvjeravatelj{ishod: ProvjeraVeze{Kandidati: c.provjera}}
			l.zadnja = &l.ishod
			l.postavi(s)
		}
		if got := len(s.upozorenjaPovezivosti()) > 0; got != c.plocica {
			t.Errorf("%s: pločica %v", c.ime, got)
		}
		if got := len(s.dohvatljivost().Upozorenja) > 0; got != c.blok {
			t.Errorf("%s: blok %v", c.ime, got)
		}
	}
	if (&Server{}).upozorenjeLanAdrese(true) != "" {
		t.Error("poslužitelj bez postavki")
	}
}
