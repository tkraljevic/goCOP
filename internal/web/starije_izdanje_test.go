package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"gocop/internal/izdanje"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// cvorUzPostavu je čvor pod Postavom, izdanja 0.0.47-alfa, s fiksnim satom
func cvorUzPostavu(t *testing.T, nacin NacinRada) (*Server, *time.Time) {
	t.Helper()
	stara := verzijaPrograma
	SetVerzijaPrograma("0.0.47-alfa (86f791c)")
	t.Cleanup(func() { SetVerzijaPrograma(stara) })
	s, _ := cvorSPostavkama(t, sadrzajPostavk, nil)
	s.SetPonovnoPokretanje(nacin, func() {})
	sad := time.Date(2026, 10, 30, 14, 5, 0, 0, time.Local)
	s.dopustenje.sat = func() time.Time { return sad }
	return s, &sad
}

func dopustiStarije(s *Server, izd string, u uvjetiZahtjeva) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.handleStarijeIzdanje(w, zahtjevPostavki(http.MethodPost, "/settings/starije-izdanje", url.Values{"izdanje": {izd}}, u))
	return w
}

// zdravljeZa pita /zdravlje čvora s ovog računala, kao Postava
func zdravljeZa(t *testing.T, s *Server) (Zdravlje, string) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/zdravlje", nil)
	r.RemoteAddr, r.Host = "127.0.0.1:50000", "127.0.0.1:1160"
	w := httptest.NewRecorder()
	s.serveZdravlje(w, r)
	var z Zdravlje
	if err := json.Unmarshal(w.Body.Bytes(), &z); err != nil || w.Code != http.StatusOK {
		t.Fatalf("/zdravlje: %d %v", w.Code, err)
	}
	return z, w.Body.String()
}

// Dopuštenje vrijedi 30 minuta od upisa; novo zamjenjuje staro
func TestDopustenjeIzdanjaIstjece(t *testing.T) {
	sad := time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC)
	d := &dopustenjeIzdanja{sat: func() time.Time { return sad }}
	if izd, _ := d.vrijedi(); izd != "" {
		t.Fatalf("prije dopuštenja: %q", izd)
	}
	if do := d.dopusti("0.0.45-alfa"); !do.Equal(sad.Add(30 * time.Minute)) {
		t.Errorf("vrijedi do %v", do)
	}
	sad = sad.Add(29*time.Minute + 59*time.Second)
	if izd, _ := d.vrijedi(); izd != "0.0.45-alfa" {
		t.Errorf("unutar 30 minuta: %q", izd)
	}
	sad = sad.Add(time.Second)
	if izd, do := d.vrijedi(); izd != "" || !do.IsZero() {
		t.Errorf("nakon 30 minuta: %q %v", izd, do)
	}
	d.dopusti("0.0.44-alfa")
	if izd, _ := d.vrijedi(); izd != "0.0.44-alfa" {
		t.Errorf("novo dopuštenje: %q", izd)
	}
	if (&dopustenjeIzdanja{}).sad().IsZero() {
		t.Error("bez ubrizganog sata nema vremena")
	}
}

// Starije izdanje dopušta samo globalni administrator, svojim očima, s
// ovog računala ili iz lokalne mreže; svi ostali dobiju 403 i dopuštenja
// nema. Dnevnik programa pamti tko je dopustio i s koje adrese.
func TestStarijeIzdanjeSamoGlobalniAdministratorLokalno(t *testing.T) {
	s, _ := cvorUzPostavu(t, PodPostavom)
	dnevnik := dnevnikPrograma(t)
	obican := uvjetiZahtjeva{perms: &models.UserPermissions{}, adresa: adresaLokalno, korisnik: adminIvic}
	izvana := kaoAdmin
	izvana.adresa = adresaIzvana
	tunel := kaoAdmin
	tunel.adresa, tunel.tunel = adresaTunela, true
	tudjim := kaoAdmin
	tudjim.tudjim = true
	for _, c := range []struct {
		ime    string
		u      uvjetiZahtjeva
		poruka string
	}{
		{"bez prava", uvjetiZahtjeva{adresa: adresaLokalno}, zabranaNijeAdmin.starije},
		{"obični korisnik", obican, zabranaNijeAdmin.starije},
		{"izvana", izvana, zabranaIzvana.starije},
		{"kroz tunel", tunel, zabranaIzvana.starije},
		{"tuđim očima", tudjim, zabranaTudjimOcima.starije},
	} {
		w := dopustiStarije(s, "0.0.45-alfa", c.u)
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), c.poruka) {
			t.Errorf("%s: %d %q", c.ime, w.Code, w.Body.String())
		}
		if z, _ := zdravljeZa(t, s); z.DopustenoIzdanje != "" {
			t.Fatalf("%s: dopušteno %q", c.ime, z.DopustenoIzdanje)
		}
	}
	if dnevnik.Len() != 0 {
		t.Errorf("odbijeno je zapisano kao dopuštenje:\n%s", dnevnik)
	}

	for _, adresa := range []string{adresaLokalno, "127.0.0.1:5000"} {
		u := kaoAdmin
		u.adresa = adresa
		w := dopustiStarije(s, " v0.0.45-alfa ", u)
		if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "success=") ||
			!strings.HasSuffix(w.Header().Get("Location"), "#starije-izdanje") {
			t.Errorf("%s: %d %s", adresa, w.Code, w.Header().Get("Location"))
		}
		if z, _ := zdravljeZa(t, s); z.DopustenoIzdanje != "0.0.45-alfa" || z.Izdanje != "0.0.47-alfa" || !z.Radi {
			t.Errorf("%s: /zdravlje %+v", adresa, z)
		}
		if host, _, _ := strings.Cut(adresa, ":"); !strings.Contains(dnevnik.String(), `"Ivan Ivić (iivic)" s adrese `+host) ||
			!strings.Contains(dnevnik.String(), "postavi 0.0.45-alfa, do 14:35") {
			t.Errorf("%s: dnevnik programa:\n%s", adresa, dnevnik)
		}
	}
}

// Upisano mora biti oznaka izdanja starijeg od ovoga; inače poruka, a
// dopuštenja nema. Bez Postave (spremnik, ručno pokrenut čvor) ga nema.
func TestStarijeIzdanjeOdbijaKrivo(t *testing.T) {
	s, _ := cvorUzPostavu(t, UslugaSustava)
	for upisano, poruka := range map[string]string{
		"":             "upišite oznaku izdanja",
		"najnovije":    "upišite oznaku izdanja",
		"0.0.47-alfa":  "nije starije od 0.0.47-alfa",
		"0.0.48-alfa":  "nije starije",
		"0.0.47":       "upišite punu oznaku izdanja, npr. 0.0.47-alfa", // bez faze bilo bi stabilno, novije od alfe
		"0.0.45":       "upišite punu oznaku izdanja, npr. 0.0.45-alfa",
		"0.0.36-alfa":  "starija od " + izdanje.NajstarijeVracanje,
		"0.0.5-alfa":   "vraća ručno",
		"postava-v1.0": "upišite oznaku izdanja",
	} {
		w := dopustiStarije(s, upisano, kaoAdmin)
		loc, err := url.Parse(w.Header().Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusSeeOther || !strings.Contains(loc.Query().Get("error"), poruka) {
			t.Errorf("%q: %d %q", upisano, w.Code, loc.Query().Get("error"))
		}
	}
	if z, raw := zdravljeZa(t, s); z.DopustenoIzdanje != "" || strings.Contains(raw, "dopusteno_izdanje") {
		t.Errorf("dopušteno uz krivo upisano: %s", raw)
	}
	if _, p := starijeOdOvog("0.0.45-alfa", "razvojna"); !strings.Contains(p, "ne zna se") {
		t.Errorf("nepoznato izdanje ovog programa: %q", p)
	}
	for _, nacin := range []NacinRada{USpremniku, RucnoPokrenut} {
		s, _ := cvorUzPostavu(t, nacin)
		if w := dopustiStarije(s, "0.0.45-alfa", kaoAdmin); w.Code != http.StatusConflict {
			t.Errorf("način %d: %d", nacin, w.Code)
		}
		if p := s.postavkeStranice(zahtjevPostavki(http.MethodGet, "/settings", nil, kaoAdmin), "/settings"); p.Starije != nil {
			t.Errorf("način %d: blok bez Postave %+v", nacin, p.Starije)
		}
	}
}

// /zdravlje javlja dopušteno izdanje samo dok vrijedi
func TestZdravljeJavljaDopustenjeDokVrijedi(t *testing.T) {
	s, sad := cvorUzPostavu(t, PodPostavom)
	if w := dopustiStarije(s, "0.0.45-alfa", kaoAdmin); w.Code != http.StatusSeeOther {
		t.Fatalf("dopuštenje: %d", w.Code)
	}
	*sad = sad.Add(29 * time.Minute)
	if z, _ := zdravljeZa(t, s); z.DopustenoIzdanje != "0.0.45-alfa" {
		t.Errorf("nakon 29 minuta: %+v", z)
	}
	*sad = sad.Add(time.Minute)
	if z, raw := zdravljeZa(t, s); z.DopustenoIzdanje != "" || strings.Contains(raw, "dopusteno_izdanje") || z.Izdanje != "0.0.47-alfa" {
		t.Errorf("nakon 30 minuta: %s", raw)
	}
	// čvor koji nije smio krenuti nikad ne javlja dopuštenje
	r := httptest.NewRequest(http.MethodGet, "/zdravlje", nil)
	r.RemoteAddr, r.Host = "127.0.0.1:50000", "127.0.0.1:1160"
	w := httptest.NewRecorder()
	OdbijenoPokretanje("Ključ čvora nedostaje.").ServeHTTP(w, r)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "dopusteno") {
		t.Errorf("odbijeno pokretanje: %d %s", w.Code, w.Body.String())
	}
}

// Uz ključ od Postave dopuštenje nosi rok i potpis tim ključem; bez ključa
// (usluga sustava) samo rok
func TestZdravljePotpisujeDopustenje(t *testing.T) {
	s, sad := cvorUzPostavu(t, PodPostavom)
	dopustiStarije(s, "0.0.45-alfa", kaoAdmin)
	do := sad.Add(30 * time.Minute).Unix()
	if z, raw := zdravljeZa(t, s); z.DopustenoDo != do || z.PotpisDopustenja != "" {
		t.Errorf("bez ključa: %s", raw)
	}
	s.SetKljucPostave("kljuc-postave")
	z, raw := zdravljeZa(t, s)
	if z.DopustenoDo != do || z.PotpisDopustenja != izdanje.PotpisDopustenja("kljuc-postave", "0.0.47-alfa", "0.0.45-alfa", do) {
		t.Errorf("s ključem: %s", raw)
	}
	*sad = sad.Add(30 * time.Minute)
	if _, raw := zdravljeZa(t, s); strings.Contains(raw, "dopusteno") || strings.Contains(raw, "potpis") {
		t.Errorf("nakon isteka: %s", raw)
	}
}

// Kroz sve rute i zaštitne slojeve: globalni administrator iz lokalne mreže
// dopusti starije izdanje, a Postava ga vidi u GET /zdravlje s ovog računala.
// Obični korisnik dobije 403, a /zdravlje ostaje bez dopuštenja.
func TestStarijeIzdanjeKrozSveRute(t *testing.T) {
	stara := verzijaPrograma
	SetVerzijaPrograma("0.0.47-alfa (86f791c)")
	t.Cleanup(func() { SetVerzijaPrograma(stara) })
	o := novaOkolinaPrijave(t)
	users := service.NewUserService(o.repo, o.auth, service.NewSSEBroker())
	moduli := service.NewModuleService(repository.NewModuleRepository(o.baza, ledger.New(o.baza, "test")))
	s, err := NewServer("", o.auth, users, nil, nil, nil, nil, nil, nil, nil, moduli, nil, nil, nil, SupportContact{}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.SetPonovnoPokretanje(PodPostavom, func() {})
	s.SetKljucPostave("kljuc-postave")
	h := s.Handler()
	posalji := func(ime, lozinka string, admin bool) *httptest.ResponseRecorder {
		t.Helper()
		o.osoba(ime, lozinka, ime+"@example.com", admin)
		sesija, _, err := o.auth.Login(ime, lozinka, "192.168.1.50", "test")
		if err != nil {
			t.Fatal(err)
		}
		r := izravno(httptest.NewRequest(http.MethodPost, "/settings/starije-izdanje", strings.NewReader("izdanje=0.0.45-alfa")))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: sesija.ID.String()})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	zdravlje := func() string {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/zdravlje", nil)
		r.RemoteAddr, r.Host = "127.0.0.1:50000", "127.0.0.1:1160"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("/zdravlje: %d", w.Code)
		}
		return w.Body.String()
	}
	if w := posalji("pperic", "perina-lozinka-1", false); w.Code != http.StatusForbidden {
		t.Errorf("obični korisnik: %d", w.Code)
	}
	if raw := zdravlje(); strings.Contains(raw, "dopusteno") {
		t.Errorf("dopušteno bez prava: %s", raw)
	}
	if w := posalji("iivic", "ivina-lozinka-1", true); w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "success=") {
		t.Fatalf("globalni administrator: %d %q", w.Code, w.Header().Get("Location"))
	}
	if raw := zdravlje(); !strings.Contains(raw, `"dopusteno_izdanje":"0.0.45-alfa"`) || !strings.Contains(raw, `"potpis_dopustenja":"`) {
		t.Errorf("/zdravlje nakon dopuštenja: %s", raw)
	}
}

// Blok na stranici Ovo računalo i mreža: obrazac samo globalnom
// administratoru lokalno, ostalima rečenica zašto; dopušteno i do kad
func TestStarijeIzdanjeNaStranici(t *testing.T) {
	s, _ := cvorUzPostavu(t, PodPostavom)
	p := s.postavkeStranice(zahtjevPostavki(http.MethodGet, "/settings", nil, kaoAdmin), "/settings").Starije
	if p == nil || !p.Nudi || p.Zabrana != "" || p.Trenutno != "0.0.47-alfa" || p.Dopusteno != "" {
		t.Fatalf("administrator lokalno: %+v", p)
	}
	html := iscrtajPostavke(t, s, "/settings", kaoAdmin)
	if !strings.Contains(html, `action="/settings/starije-izdanje"`) || !strings.Contains(html, "Dopusti starije izdanje") {
		t.Error("na stranici nema obrasca za starije izdanje")
	}
	dopustiStarije(s, "0.0.45-alfa", kaoAdmin)
	izvana := kaoAdmin
	izvana.adresa = adresaIzvana
	p = s.postavkeStranice(zahtjevPostavki(http.MethodGet, "/settings", nil, izvana), "/settings").Starije
	if p.Nudi || p.Zabrana != zabranaIzvana.starije || p.Dopusteno != "0.0.45-alfa" || p.Do != "14:35" {
		t.Errorf("izvana: %+v", p)
	}
	html = iscrtajPostavke(t, s, "/settings", izvana)
	if strings.Contains(html, `action="/settings/starije-izdanje"`) || !strings.Contains(html, "Dopušteno je izdanje 0.0.45-alfa, do 14:35.") {
		t.Error("izvana: obrazac ili bez dopuštenog izdanja")
	}
}
