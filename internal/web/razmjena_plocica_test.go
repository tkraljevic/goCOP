package web

import (
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/razmjena"
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
			Cvorovi:  []CvorRazmjene{{Naziv: "COP Osijek (kućni server)", Dostupnost: "online", Zadnja: &malo, Verzija: "0.0.24-alfa", Put: "LAN sync", RTTMs: 3}},
			Prognoza: PrognozaRazmjene{Ima: true, Izdavac: "cop-osijek-server", Primljeno: true, Izdano: sad, Nastalo: sad}},
			[]string{"Sve je usklađeno s 1 čvorom", "COP Osijek (kućni server) na 0.0.24-alfa: zadnja razmjena prije 2 min, LAN sync, 3 ms", "s čvora cop-osijek-server", "ovaj čvor na 0.0.24-alfa"}},
		{"stariji program", RazmjenaStanje{UMrezi: true, Verzija: "0.0.24-alfa",
			Cvorovi: []CvorRazmjene{
				{Naziv: "laptop", Dostupnost: "online", Zadnja: &malo, Verzija: "0.0.23-alfa", Razlicita: true},
				{Naziv: "stari laptop", Dostupnost: "online", Zadnja: &malo, Razlicita: true}}},
			[]string{"<strong>laptop</strong><div class=\"reg-card-sub\">0.0.23-alfa — drukčija od ovog čvora</div>", "starija inačica (ne javlja je)"}},
		{"laptop izvan kuće", RazmjenaStanje{UMrezi: true,
			Cvorovi: []CvorRazmjene{{Naziv: "laptop", Dostupnost: "online", Zadnja: &malo, Zaostaje: 3, SamoDolazi: true,
				Put: "SERVER sync", RTTMs: 35, RazlogPuta: razmjena.RazlogPutaNijeSeJavio}}},
			[]string{"na mreži", "SERVER sync, 35 ms", "javlja se sam (kroz tunel)"}},
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

// Upozorenje da na disku ostaje malo mjesta stoji na vrhu pločice jednom,
// i kad čvor nije ni u jednoj mreži, i kad je sve usklađeno, i kad nije.
// Poruku i savjet vide svi, pa i osoba za tim računalom: ona javi
// administratoru kad ne zna što učiniti (Odluke (15. 10. 2026.)).
func TestPlocicaPokazujeMaloMjesta(t *testing.T) {
	poruka := "Na disku čvora COP Osijek s bazom ostalo je 8,2 GB (6 % diska)."
	savjet := "Oslobodite mjesto ili premjestite arhivu na drugi disk. Ako ne znate kako, javite administratoru."
	upozorenje := []DiskRazmjene{{Poruka: poruka, Savjet: savjet}}
	malo := time.Now().Add(-2 * time.Minute)
	for ime, slucaj := range map[string]struct {
		st      RazmjenaStanje
		ocekuje string
	}{
		"bez mreže": {RazmjenaStanje{Disk: upozorenje}, "nije ni u jednoj mreži"},
		"usklađeno": {RazmjenaStanje{UMrezi: true, Disk: upozorenje,
			Cvorovi: []CvorRazmjene{{Naziv: "laptop", Dostupnost: "online", Zadnja: &malo}}}, "Sve je usklađeno s 1 čvorom"},
		"ne odgovara": {RazmjenaStanje{UMrezi: true, Disk: upozorenje,
			Cvorovi: []CvorRazmjene{{Naziv: "laptop", Dostupnost: "offline", Zadnja: &malo}}}, "ne odgovara"},
	} {
		s := &Server{}
		s.SetRazmjena(func(context.Context) RazmjenaStanje { return slucaj.st })
		for tko, prava := range map[string]*models.UserPermissions{
			"administrator": {IsGlobalAdmin: true}, "vodočuvar": {}, "bez prava": nil} {
			r := httptest.NewRequest("GET", "/razmjena/plocica", nil)
			if prava != nil {
				r = r.WithContext(context.WithValue(r.Context(), contextKeyPerms, prava))
			}
			w := httptest.NewRecorder()
			s.ShowRazmjena(w, r)
			tijelo := w.Body.String()
			if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
				t.Errorf("%s, %s: odgovor %d, zaglavlja %v", ime, tko, w.Code, w.Header())
			}
			cijelo := poruka + " " + savjet
			if strings.Count(tijelo, cijelo) != 1 || !strings.Contains(tijelo, slucaj.ocekuje) {
				t.Errorf("%s, %s: %q %d puta, %q: %v u\n%s", ime, tko, cijelo, strings.Count(tijelo, cijelo), slucaj.ocekuje, strings.Contains(tijelo, slucaj.ocekuje), tijelo)
			}
		}
	}
	// čvor bez pločice razmjene ne vraća ništa, pa je naslovna ne prikaže
	w := httptest.NewRecorder()
	(&Server{}).ShowRazmjena(w, httptest.NewRequest("GET", "/razmjena/plocica", nil))
	if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Errorf("bez pločice: %d %q", w.Code, w.Body.String())
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

// Pločica za svaki naziv puta i svaki razlog: rečenica stoji uz računalo, a
// u HTML-u nema ni IPv4 ni IPv6 adrese ni imena sustava na kojem radi
// poslužitelj, jer pločicu vidi svaki prijavljeni korisnik. Trajan razlog
// stoji kao upozorenje, a prolazni kao sitni tekst.
func TestPlocicaPutIRazlozi(t *testing.T) {
	malo := time.Now().Add(-2 * time.Minute)
	adresa := regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}\b|[0-9a-f]{1,4}(:[0-9a-f]{0,4}){2,7}`)
	razlozi := []razmjena.RazlogPuta{razmjena.RazlogPutaNijeSeJavio, razmjena.RazlogPutaBezOdgovora, razmjena.RazlogPutaUDPNeProlazi, razmjena.RazlogPutaNATOvisan,
		razmjena.RazlogPutaUDPDoDrugih, razmjena.RazlogPutaVatrozid, razmjena.RazlogPutaNijeClan, razmjena.RazlogPutaStarijiProgram,
		razmjena.RazlogPutaPriprema, razmjena.RazlogPutaPauza, razmjena.RazlogPutaIstiKljuc}
	for _, naziv := range []string{razmjena.NazivLAN, razmjena.NazivServer, razmjena.NazivTailscale, razmjena.NazivWeb, ""} {
		for _, r := range razlozi {
			// Zaostaje 1: pločica s tablicom; 0: jedan redak, uz prolazni razlog
			for _, zaostaje := range []int{0, 1} {
				c := CvorRazmjene{Naziv: "laptop", Dostupnost: "online", Zadnja: &malo, Zaostaje: zaostaje, Put: naziv, RTTMs: 12, RazlogPuta: r}
				var b strings.Builder
				if err := razmjenaTmpl.Execute(&b, RazmjenaStanje{UMrezi: true, Cvorovi: []CvorRazmjene{c}}); err != nil {
					t.Fatal(err)
				}
				html, recenica := b.String(), c.Razlog()
				if recenica == "" || strings.Contains(recenica, "%") || !strings.Contains(html, template.HTMLEscapeString(recenica)) {
					t.Errorf("%q, %q: rečenica %q nije na pločici", naziv, r, recenica)
				}
				if naziv != "" && !strings.Contains(html, naziv+", 12 ms") {
					t.Errorf("%q, %q: nema naziva puta", naziv, r)
				}
				if m := adresa.FindString(html); m != "" || nadjiRijec(html, sustavKucnogServera, nil) >= 0 {
					t.Errorf("%q, %q: na pločici je adresa ili ime sustava: %q", naziv, r, m)
				}
				trajan := strings.Contains(html, `<div class="razmjena-upozorenje">`+template.HTMLEscapeString(recenica))
				if zaostaje == 1 && trajan != r.Trajan() {
					t.Errorf("%q: trajan na pločici %v", r, trajan)
				}
				if jedanRedak := strings.Contains(html, "Sve je usklađeno"); jedanRedak != (zaostaje == 0 && !r.Trajan()) {
					t.Errorf("%q, zaostaje %d: jedan redak %v", r, zaostaje, jedanRedak)
				}
			}
		}
	}
	// trajan razlog izbaci pločicu iz „sve je usklađeno”, prolazni ne
	for r, sredeno := range map[razmjena.RazlogPuta]bool{razmjena.RazlogPutaNijeSeJavio: true, razmjena.RazlogPutaNijeClan: false} {
		st := RazmjenaStanje{UMrezi: true, Cvorovi: []CvorRazmjene{{Naziv: "laptop", Dostupnost: "online", Zadnja: &malo, RazlogPuta: r}}}
		if st.Sredeno() != sredeno {
			t.Errorf("%q: usklađeno %v", r, st.Sredeno())
		}
	}
	if (CvorRazmjene{}).Razlog() != "" || (CvorRazmjene{RazlogPuta: "nepoznat"}).Razlog() != "" {
		t.Error("razlog bez koda")
	}
	if r := (CvorRazmjene{RazlogPuta: razmjena.RazlogPutaBezOdgovora, Put: razmjena.NazivLAN}).Razlog(); r != "Računalo se ne javlja ni na jednoj poznatoj adresi." {
		t.Errorf("bez ijedne adrese: %q", r)
	}
	if r := (CvorRazmjene{RazlogPuta: razmjena.RazlogPutaNijeSeJavio, Put: razmjena.NazivServer}).Razlog(); r != "Računalo se ne javlja u lokalnoj mreži; podaci idu preko poslužitelja (SERVER sync)." {
		t.Errorf("primjer iz plana: %q", r)
	}
}
