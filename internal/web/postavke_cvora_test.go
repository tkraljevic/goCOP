package web

import (
	"bytes"
	"context"
	"encoding/json"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"gocop/internal/config"
	"gocop/internal/models"
	webassets "gocop/web"
)

// Izmišljeni čvor i administrator za testove postavki
const (
	testniCvor     = "ivic-laptop"
	adresaLokalno  = "192.168.1.20:51000"
	adresaIzvana   = "203.0.113.7:51000"
	adresaTunela   = "127.0.0.1:51000"
	sadrzajPostavk = "# Moje bilješke o ovom čvoru: ne brisati\n" +
		"addr = ':8080'\n" +
		"nepoznato_iz_buducnosti = 'ostaje'\n\n" +
		"[node]\nid = 'ivic-laptop'\n\n" +
		"[sync]\n# dogovoreno s uredom\nauto_sync = '5m'   # ne dirati bez dogovora\nsve = false\n"
)

var adminIvic = &models.User{FullName: "Ivan Ivić", Username: "iivic"}

// cvorSPostavkama je poslužitelj s gocop.toml u privremenoj mapi
func cvorSPostavkama(t *testing.T, sadrzaj string, zastavice map[string]string) (*Server, string) {
	t.Helper()
	put := filepath.Join(t.TempDir(), config.FileName)
	if sadrzaj != "" {
		if err := os.WriteFile(put, []byte(sadrzaj), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	vrijedi, err := config.ProcitajTekst([]byte(sadrzaj))
	if err != nil {
		vrijedi = config.Default()
	}
	vrijedi.Node.ID = testniCvor
	for k := range zastavice {
		if k == "addr" {
			vrijedi.Addr = ":9999"
		}
	}
	pocetna := config.Default()
	pocetna.Node.ID = testniCvor
	s := &Server{pokrenut: time.Unix(1_800_000_000, 0)}
	s.SetPostavkeCvora(PostavkeCvora{Put: put, Vrijedi: vrijedi, Zastavice: zastavice, Pocetna: pocetna})
	return s, put
}

type uvjetiZahtjeva struct {
	perms    *models.UserPermissions
	adresa   string
	tunel    bool // zaglavlje posrednika (cloudflared)
	tudjim   bool // gleda tuđim očima
	korisnik *models.User
}

var kaoAdmin = uvjetiZahtjeva{perms: &models.UserPermissions{IsGlobalAdmin: true}, adresa: adresaLokalno, korisnik: adminIvic}

func zahtjevPostavki(metoda, putanja string, obrazac url.Values, u uvjetiZahtjeva) *http.Request {
	var r *http.Request
	if obrazac != nil {
		r = httptest.NewRequest(metoda, putanja, strings.NewReader(obrazac.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(metoda, putanja, nil)
	}
	r.RemoteAddr = u.adresa
	if u.tunel {
		r.Header.Set(ZaglavljeCloudflare, "198.51.100.4")
	}
	ctx := context.WithValue(r.Context(), contextKeyPerms, u.perms)
	ctx = context.WithValue(ctx, contextKeyViewing, u.tudjim)
	if u.korisnik != nil {
		ctx = context.WithValue(ctx, contextKeyUser, u.korisnik)
		ctx = context.WithValue(ctx, contextKeyRealUsr, u.korisnik)
	}
	return r.WithContext(ctx)
}

func predlozakStranice(t *testing.T, stranica string) *template.Template {
	t.Helper()
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
	if err != nil {
		t.Fatal(err)
	}
	return tmpl
}

// prikazStranice iscrtava pravu stranicu s blokovima postavki, kao njezin
// rukovatelj, samo bez ostalih podataka stranice
func prikazStranice(t *testing.T, s *Server, stranica string) http.Handler {
	t.Helper()
	datoteka := map[string]string{"/settings": "settings.html", "/pretplate": "pretplate.html",
		"/administracija/baza": "baza.html", "/administracija/karte": "administracija_karte.html",
		"/administracija/potpisi": "administracija_potpisi.html"}[stranica]
	tmpl := predlozakStranice(t, datoteka)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
		if perms == nil {
			perms = &models.UserPermissions{}
		}
		p := s.postavkeStranice(r, stranica)
		var data any
		switch datoteka {
		case "settings.html":
			data = SettingsPageData{CurrentUser: adminIvic, Permissions: perms, Postavke: p}
		case "pretplate.html":
			data = SubscriptionsPageData{CurrentUser: adminIvic, Permissions: perms, Postavke: p}
		case "baza.html":
			data = DBMaintPageData{CurrentUser: adminIvic, Permissions: perms, Postavke: p}
		case "administracija_potpisi.html":
			data = PotpisiPageData{CurrentUser: adminIvic, Permissions: perms, Postavke: p}
		default:
			data = PostavkeKarteData{CurrentUser: adminIvic, Permissions: perms, Postavke: p}
		}
		if err := tmpl.ExecuteTemplate(w, datoteka, data); err != nil {
			t.Errorf("%s: %v", datoteka, err)
		}
	})
}

func iscrtajPostavke(t *testing.T, s *Server, stranica string, u uvjetiZahtjeva) string {
	t.Helper()
	w := httptest.NewRecorder()
	prikazStranice(t, s, stranica).ServeHTTP(w, zahtjevPostavki(http.MethodGet, stranica, nil, u))
	return w.Body.String()
}

// Svaka postavka iz strukture ima redak u rasporedu, a raspored nema
// postavki kojih nema. Kad ovo padne, novoj postavci odredite stranicu
// (cvor, mreza, pretplate, baza, opcije, posta) i smije li se mijenjati.
func TestRasporedPokrivaSvePostavke(t *testing.T) {
	poznati := map[string]bool{"posta": true}
	for _, d := range dijeloviPostavki {
		poznati[d.ime] = true
	}
	ima := map[string]bool{}
	for _, k := range config.Kljucevi() {
		ima[k.Puni()] = true
		i := redRasporeda(k.Puni())
		if i == len(rasporedPostavki) {
			t.Errorf("postavka %s nema mjesta u rasporedu (internal/web/postavke_cvora.go, rasporedPostavki)", k.Oznaka())
			continue
		}
		m := rasporedPostavki[i].mjestoPostavke
		if !poznati[m.dio] {
			t.Errorf("%s: nepoznat blok %q", k.Oznaka(), m.dio)
		}
		if m.nacin != postavkaDrugdje && m.naziv == "" {
			t.Errorf("%s: nema naziva", k.Oznaka())
		}
		if m.nacin == postavkaSamoCitanje && m.razlog == "" {
			t.Errorf("%s: samo za čitanje, a ne piše zašto", k.Oznaka())
		}
		if (k.Odjeljak == "posta") != (m.nacin == postavkaDrugdje) {
			t.Errorf("%s: [posta] vodi stranica E-pošta, i samo nju", k.Oznaka())
		}
	}
	vidjeno := map[string]bool{}
	for i, r := range rasporedPostavki {
		if !ima[r.kljuc] || vidjeno[r.kljuc] {
			t.Errorf("raspored ima %s, a takve postavke nema ili je dvaput", r.kljuc)
		}
		vidjeno[r.kljuc] = true
		// urediva u bloku stoje prije onih samo za čitanje
		if i > 0 && rasporedPostavki[i-1].dio == r.dio && rasporedPostavki[i-1].nacin != postavkaUrediva && r.nacin == postavkaUrediva {
			t.Errorf("%s: urediva postavka iza onih samo za čitanje", r.kljuc)
		}
	}
}

// Postavka koje još nema u rasporedu prikazuje se u bloku svog odjeljka,
// samo za čitanje; nova postavka [posta] ostaje na stranici E-pošta.
func TestNovaPostavkaBezRasporeda(t *testing.T) {
	for _, s := range []struct {
		odjeljak, dio string
		nacin         nacinPostavke
	}{{"sync", "mreza", postavkaSamoCitanje}, {"web", "mreza", postavkaSamoCitanje}, {"posta", "posta", postavkaDrugdje},
		{"nepoznato", "cvor", postavkaSamoCitanje}, {"", "cvor", postavkaSamoCitanje}} {
		m := mjestoKljuca(config.Kljuc{Odjeljak: s.odjeljak, Ime: "nova_postavka"})
		if m.dio != s.dio || m.nacin != s.nacin {
			t.Errorf("[%s] nova_postavka: %+v", s.odjeljak, m)
		}
	}
}

// Svaka stranica prikazuje svoje postavke, s obrascem za one koje se ondje
// mijenjaju, a tuđe i [posta] ne prikazuje.
func TestStranicePrikazujuSvojePostavke(t *testing.T) {
	s, _ := cvorSPostavkama(t, sadrzajPostavk, nil)
	s.SetPonovnoPokretanje(UslugaSustava, func() {})
	for _, d := range dijeloviPostavki {
		html := iscrtajPostavke(t, s, d.stranica, kaoAdmin)
		for _, k := range config.Kljucevi() {
			m := mjestoKljuca(k)
			naStranici := false
			for _, d2 := range dijeloviPostavki {
				naStranici = naStranici || d2.stranica == d.stranica && d2.ime == m.dio
			}
			prikazana := strings.Contains(html, `data-postavka="`+k.Puni()+`"`)
			if prikazana != (naStranici && m.nacin != postavkaDrugdje) {
				t.Errorf("%s: %s prikazana=%v (blok %s)", d.stranica, k.Oznaka(), prikazana, m.dio)
			}
			polje := strings.Contains(html, `name="postavka.`+k.Puni()+`"`)
			if polje != (naStranici && m.nacin == postavkaUrediva) {
				t.Errorf("%s: polje za %s=%v", d.stranica, k.Oznaka(), polje)
			}
		}
		if !strings.Contains(html, `id="`+d.sidro+`"`) {
			t.Errorf("%s: nema bloka %s", d.stranica, d.sidro)
		}
	}
	// gumb i objašnjenje uz [kontakt], sa sidrom na koje vodi traka
	html := iscrtajPostavke(t, s, "/settings", kaoAdmin)
	for _, mora := range []string{`id="ponovno-pokretanje"`, `id="gumb-ponovno"`, "Centar obrane od poplava ovog čvora", "usluga sustava"} {
		if !strings.Contains(html, mora) {
			t.Errorf("/settings: nema %q", mora)
		}
	}
	if baza := iscrtajPostavke(t, s, "/administracija/baza", kaoAdmin); !strings.Contains(baza, "Datoteka postavki") || strings.Contains(baza, `id="gumb-ponovno"`) {
		t.Error("/administracija/baza: nema podataka o datoteci postavki ili ima gumb")
	}
}

// Blokove vidi samo administrator; izvana, kroz tunel i tuđim očima samo
// pregled, bez obrasca i bez gumba.
func TestPostavkeSamoAdministratoruLokalno(t *testing.T) {
	s, _ := cvorSPostavkama(t, sadrzajPostavk, nil)
	s.SetPonovnoPokretanje(PodPostavom, func() {})
	obican := uvjetiZahtjeva{perms: &models.UserPermissions{}, adresa: adresaLokalno, korisnik: adminIvic}
	if p := s.postavkeStranice(zahtjevPostavki(http.MethodGet, "/settings", nil, obican), "/settings"); p != nil {
		t.Error("ne-administrator vidi postavke čvora")
	}
	if html := iscrtajPostavke(t, s, "/settings", obican); strings.Contains(html, "postavke-cvor") {
		t.Error("ne-administrator vidi blok postavki")
	}
	izvana := kaoAdmin
	izvana.adresa = adresaIzvana
	tunel := kaoAdmin
	tunel.adresa, tunel.tunel = adresaTunela, true
	tudjim := kaoAdmin
	tudjim.tudjim = true
	for ime, u := range map[string]uvjetiZahtjeva{"izvana": izvana, "kroz tunel": tunel, "tuđim očima": tudjim} {
		p := s.postavkeStranice(zahtjevPostavki(http.MethodGet, "/settings", nil, u), "/settings")
		b := p.Dio("cvor")
		if b == nil || b.Uredivo || b.Zabrana == "" || p.Ponovno.Nudi || p.Ponovno.Zabrana == "" {
			t.Errorf("%s: blok %+v, ponovno %+v", ime, b, p.Ponovno)
		}
		if html := iscrtajPostavke(t, s, "/settings", u); strings.Contains(html, `name="postavka.`) || strings.Contains(html, `id="gumb-ponovno"`) {
			t.Errorf("%s: obrazac ili gumb na stranici", ime)
		}
	}
	if (*PostavkeStranice)(nil).Dio("cvor") != nil {
		t.Error("bez postavki nema bloka")
	}
	bez := &Server{}
	if bez.postavkeStranice(zahtjevPostavki(http.MethodGet, "/settings", nil, kaoAdmin), "/settings") != nil {
		t.Error("poslužitelj bez gocop.toml prikazuje postavke")
	}
}

func obrazacBloka(dio, otisak string, polja map[string]string) url.Values {
	v := url.Values{"dio": {dio}, "otisak": {otisak}}
	for k, x := range polja {
		v.Set("postavka."+k, x)
	}
	return v
}

func spremi(t *testing.T, s *Server, stranica string, obrazac url.Values, u uvjetiZahtjeva) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	s.spremiPostavkeCvora(stranica, prikazStranice(t, s, stranica)).ServeHTTP(w, zahtjevPostavki(http.MethodPost, stranica+"/postavke-cvora", obrazac, u))
	return w
}

func otisakDatoteke(t *testing.T, put string) string {
	t.Helper()
	b, err := os.ReadFile(put)
	if err != nil {
		t.Fatal(err)
	}
	return otisakTeksta(b)
}

// Ispravno spremanje: datoteka dobije nove vrijednosti, ručni komentari i
// nepoznati ključevi ostaju, stara ide u kopiju, dnevnik kaže tko je što
// promijenio, a stranice pokažu da promjena čeka ponovno pokretanje.
func TestSpremanjePostavkiCvora(t *testing.T) {
	s, put := cvorSPostavkama(t, sadrzajPostavk, nil)
	var dnevnik bytes.Buffer
	log.SetOutput(&dnevnik)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	// adresa za QR kod stoji uz potpise akata (Odluke (30. 10. 2026.)), a ne
	// sprema se uz kontakt centra
	if w := spremi(t, s, "/settings", obrazacBloka("cvor", otisakDatoteke(t, put), map[string]string{
		"javna_adresa": "https://cop.primjer.hr"}), kaoAdmin); w.Code != http.StatusBadRequest {
		t.Errorf("adresa za QR kod spremljena je uz čvor: %d", w.Code)
	}
	w := spremi(t, s, "/settings", obrazacBloka("cvor", otisakDatoteke(t, put), map[string]string{
		"kontakt.centar": "COP Primjer", "kontakt.centar_telefon": "",
	}), kaoAdmin)
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/settings?success=") {
		t.Fatalf("spremanje: %d %s\n%s", w.Code, w.Header().Get("Location"), w.Body.String())
	}
	w = spremi(t, s, "/administracija/potpisi", obrazacBloka("dokumenti", otisakDatoteke(t, put), map[string]string{
		"javna_adresa": "https://cop.primjer.hr"}), kaoAdmin)
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/administracija/potpisi?success=") {
		t.Fatalf("spremanje adrese za QR kod: %d %s\n%s", w.Code, w.Header().Get("Location"), w.Body.String())
	}
	b, _ := os.ReadFile(put)
	for _, mora := range []string{"# Moje bilješke o ovom čvoru: ne brisati", "nepoznato_iz_buducnosti = 'ostaje'",
		"auto_sync = '5m'   # ne dirati bez dogovora", "javna_adresa = 'https://cop.primjer.hr'",
		"[kontakt]", "centar = 'COP Primjer'", "# Adresa na kojoj se program otvara izvana"} {
		if !strings.Contains(string(b), mora) {
			t.Errorf("u datoteci nema %q:\n%s", mora, b)
		}
	}
	if strings.Contains(string(b), "centar_telefon") {
		t.Error("nepromijenjena postavka upisana je u datoteku")
	}
	if k := config.Kopije(put); len(k) != 2 {
		t.Errorf("kopije: %+v", k)
	}
	if fi, _ := os.Stat(put); fi.Mode().Perm() != 0o640 {
		t.Errorf("dopuštenja: %v", fi.Mode().Perm())
	}
	for _, mora := range []string{"Ivan Ivić (iivic)", "[kontakt] centar", "javna_adresa", put} {
		if !strings.Contains(dnevnik.String(), mora) {
			t.Errorf("u dnevniku nema %q: %s", mora, dnevnik.String())
		}
	}
	// traka na svim stranicama s blokom, a uz postavku što vrijedi do tada
	for _, stranica := range []string{"/settings", "/pretplate", "/administracija/baza", "/administracija/karte", "/administracija/potpisi"} {
		if !strings.Contains(iscrtajPostavke(t, s, stranica, kaoAdmin), "Spremljena promjena u gocop.toml vrijedi nakon ponovnog pokretanja čvora.") {
			t.Errorf("%s: nema trake o ponovnom pokretanju", stranica)
		}
	}
	p := s.postavkeStranice(zahtjevPostavki(http.MethodGet, "/administracija/potpisi", nil, kaoAdmin), "/administracija/potpisi")
	for _, st := range p.Dio("dokumenti").Stavke {
		if st.Kljuc == "javna_adresa" && (!st.CekaPonovno || st.UDatoteci != "https://cop.primjer.hr" || st.Polje != "https://cop.primjer.hr") {
			t.Errorf("javna_adresa nakon spremanja: %+v", st)
		}
	}
	// isto još jednom ne mijenja ništa i ne pravi kopiju
	w = spremi(t, s, "/settings", obrazacBloka("cvor", otisakDatoteke(t, put), map[string]string{"kontakt.centar": "COP Primjer"}), kaoAdmin)
	if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), url.QueryEscape("Ništa nije promijenjeno.")) || len(config.Kopije(put)) != 2 {
		t.Errorf("isto spremanje: %d %s", w.Code, w.Header().Get("Location"))
	}
}

// Prati sve (da-ne), broj i tekst kroz obrazac; komentar u retku ostaje
func TestSpremanjeVrstaPolja(t *testing.T) {
	s, put := cvorSPostavkama(t, sadrzajPostavk, nil)
	obrazac := obrazacBloka("pretplate", otisakDatoteke(t, put), map[string]string{"vodostaji.povijest_mjeseci": "12"})
	obrazac["postavka.sync.sve"] = []string{"0", "1"}
	if w := spremi(t, s, "/pretplate", obrazac, kaoAdmin); w.Code != http.StatusSeeOther {
		t.Fatalf("pretplate: %d\n%s", w.Code, w.Body.String())
	}
	w := spremi(t, s, "/settings", obrazacBloka("mreza", otisakDatoteke(t, put), map[string]string{"sync.auto_sync": "15m"}), kaoAdmin)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("mreža: %d\n%s", w.Code, w.Body.String())
	}
	b, _ := os.ReadFile(put)
	cfg, err := config.ProcitajTekst(b)
	if err != nil || !cfg.Sync.All || cfg.Readings.HistoryMonths != 12 || cfg.Sync.AutoSync != "15m" {
		t.Errorf("pročitano nakon spremanja: %+v %+v %v", cfg.Sync, cfg.Readings, err)
	}
	if !strings.Contains(string(b), "auto_sync = '15m'   # ne dirati bez dogovora") {
		t.Errorf("komentar u retku nestao:\n%s", b)
	}
}

// Samo-čitanje, tajna, tuđi blok i nepoznata postavka ne mijenjaju se ni
// podmetnutim obrascem: odbije se cijeli obrazac i ništa se ne zapisuje.
func TestPodmetnutObrazacNeMijenja(t *testing.T) {
	s, put := cvorSPostavkama(t, sadrzajPostavk, nil)
	telefon := redRasporeda("kontakt.centar_telefon")
	staro := rasporedPostavki[telefon]
	rasporedPostavki[telefon].mjestoPostavke = mjestoPostavke{dio: "cvor", nacin: postavkaTajna, naziv: "Telefon centra"}
	t.Cleanup(func() { rasporedPostavki[telefon] = staro })
	otisak := otisakDatoteke(t, put)
	for ime, polja := range map[string]map[string]string{
		"samo čitanje":     {"addr": ":1"},
		"ime čvora":        {"node.id": "tudji-cvor"},
		"posrednici":       {"web.pouzdani_posrednici": "0.0.0.0/0"},
		"tajna":            {"kontakt.centar_telefon": "099 000 000"},
		"[posta]":          {"posta.posluzitelj": "smtp.primjer.hr"},
		"s druge stranice": {"sync.sve": "1"},
		"nepoznata":        {"nema.takve": "x"},
	} {
		polja["kontakt.centar"] = "COP Primjer"
		w := spremi(t, s, "/settings", obrazacBloka("cvor", otisak, polja), kaoAdmin)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", ime, w.Code)
		}
	}
	if b, _ := os.ReadFile(put); string(b) != sadrzajPostavk || len(config.Kopije(put)) != 0 {
		t.Errorf("datoteka promijenjena:\n%s", b)
	}
	// tajna se ne prikazuje
	cfgSTelefonom := sadrzajPostavk + "\n[kontakt]\ncentar_telefon = '031 999 888'\n"
	s2, _ := cvorSPostavkama(t, cfgSTelefonom, nil)
	if html := iscrtajPostavke(t, s2, "/settings", kaoAdmin); strings.Contains(html, "031 999 888") || !strings.Contains(html, "skriveno") {
		t.Error("tajna postavka prikazana")
	}
	// krivi blok za stranicu i neispravan obrazac
	if w := spremi(t, s, "/pretplate", obrazacBloka("cvor", otisak, nil), kaoAdmin); w.Code != http.StatusBadRequest {
		t.Errorf("blok s druge stranice: %d", w.Code)
	}
}

// Neispravno ne zapisuje ništa: stranica se pokaže iznova s porukom uz
// polje i s onim što je upisano.
func TestNeispravnoSeNeZapisuje(t *testing.T) {
	s, put := cvorSPostavkama(t, sadrzajPostavk, nil)
	otisak := otisakDatoteke(t, put)
	slucajevi := []struct {
		stranica, dio string
		polja         map[string]string
		poruka        string
	}{
		{"/settings", "mreza", map[string]string{"sync.auto_sync": "pet minuta"}, "Upišite razmak kao 5m ili 1h"},
		{"/administracija/potpisi", "dokumenti", map[string]string{"javna_adresa": "cop.primjer.hr"}, "Upišite punu adresu"},
		{"/settings", "cvor", map[string]string{"kontakt.centar": "dva\nretka"}, "u jednom retku"},
		{"/pretplate", "pretplate", map[string]string{"vodostaji.povijest_mjeseci": "dvanaest"}, "Upišite cijeli broj"},
		{"/administracija/karte", "karte", map[string]string{"karta.najvise_z": "25"}, "od 0 do 19"},
		{"/administracija/karte", "karte", map[string]string{"karta.plocice": "https://plocice.primjer.hr/a.png"}, "{z}, {x} i {y}"},
	}
	for _, sl := range slucajevi {
		w := spremi(t, s, sl.stranica, obrazacBloka(sl.dio, otisak, sl.polja), kaoAdmin)
		tijelo := w.Body.String()
		if w.Code != http.StatusUnprocessableEntity || !strings.Contains(tijelo, template.HTMLEscapeString(sl.poruka)) || !strings.Contains(tijelo, "Ispravite označena polja") {
			t.Errorf("%v: %d, poruke %q nema", sl.polja, w.Code, sl.poruka)
		}
		for _, v := range sl.polja {
			if !strings.Contains(tijelo, template.HTMLEscapeString(v)) {
				t.Errorf("%v: upisano se ne vidi u obrascu", sl.polja)
			}
		}
	}
	if b, _ := os.ReadFile(put); string(b) != sadrzajPostavk || len(config.Kopije(put)) != 0 {
		t.Errorf("neispravno je zapisano:\n%s", b)
	}
	// datoteka promijenjena dok je obrazac bio otvoren
	w := spremi(t, s, "/settings", obrazacBloka("cvor", "stari-otisak", map[string]string{"kontakt.centar": "COP Primjer"}), kaoAdmin)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "u međuvremenu promijenjen") {
		t.Errorf("stari otisak: %d", w.Code)
	}
}

// Datoteka koja se ne da pročitati: blok kaže zašto, obrazca nema i ništa
// se ne zapisuje.
func TestPokvarenaDatotekaPostavki(t *testing.T) {
	s, put := cvorSPostavkama(t, "addr = :80 nije toml\n", nil)
	p := s.postavkeStranice(zahtjevPostavki(http.MethodGet, "/settings", nil, kaoAdmin), "/settings")
	if b := p.Dio("cvor"); b.Uredivo || !strings.Contains(b.Greska, "ne da pročitati") || p.CekaPonovno {
		t.Errorf("pokvarena datoteka: %+v", b)
	}
	w := spremi(t, s, "/settings", obrazacBloka("cvor", otisakDatoteke(t, put), map[string]string{"kontakt.centar": "x"}), kaoAdmin)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "ne da pročitati") {
		t.Errorf("spremanje uz pokvarenu datoteku: %d", w.Code)
	}
	if baza := s.postavkeStranice(zahtjevPostavki(http.MethodGet, "/", nil, kaoAdmin), "/administracija/baza").Dio("baza"); baza.Datoteka.Greska == "" {
		t.Error("blok baze ne javlja grešku datoteke")
	}
}

// Čvor bez gocop.toml: spremanje napravi novu datoteku s objašnjenjima i
// imenom čvora, bez kopije.
func TestSpremanjeBezDatoteke(t *testing.T) {
	s, put := cvorSPostavkama(t, "", nil)
	if b := s.postavkeStranice(zahtjevPostavki(http.MethodGet, "/", nil, kaoAdmin), "/administracija/baza").Dio("baza"); b.Datoteka.Postoji {
		t.Error("nepostojeća datoteka prikazana kao da postoji")
	}
	w := spremi(t, s, "/administracija/karte", obrazacBloka("karte", "nema", map[string]string{"karta.najvise_z": "15"}), kaoAdmin)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("nova datoteka: %d\n%s", w.Code, w.Body.String())
	}
	b, _ := os.ReadFile(put)
	cfg, err := config.ProcitajTekst(b)
	if err != nil || cfg.Karta.NajviseZ != 15 || cfg.Node.ID != testniCvor || !strings.HasPrefix(string(b), "# goCOP — postavke čvora") || len(config.Kopije(put)) != 0 {
		t.Errorf("nova datoteka: %v\n%s", err, b)
	}
}

// Kad je zastavica pokretanja jača od datoteke, uz postavku to piše, a
// promjena u datoteci ne čeka ponovno pokretanje jer i dalje vrijedi zastavica.
func TestZastavicaJacaOdDatoteke(t *testing.T) {
	s, _ := cvorSPostavkama(t, sadrzajPostavk, map[string]string{"addr": "-addr"})
	p := s.postavkeStranice(zahtjevPostavki(http.MethodGet, "/administracija/potpisi", nil, kaoAdmin), "/administracija/potpisi")
	for _, st := range p.Dio("dokumenti").Stavke {
		if st.Kljuc == "addr" && (st.Zastavica != "-addr" || st.Prikaz != ":9999" || st.UDatoteci != ":8080" || st.CekaPonovno) {
			t.Errorf("addr uz zastavicu: %+v", st)
		}
	}
	if p.CekaPonovno {
		t.Error("traka bez spremljene promjene")
	}
	if html := iscrtajPostavke(t, s, "/settings", kaoAdmin); !strings.Contains(html, `Vrijedi zastavica <span class="mono">-addr</span>`) {
		t.Error("na stranici ne piše da vrijedi zastavica")
	}
	s.SetAddr(":8081")
	if s.postavke.snimka().Vrijedi.Addr != ":8081" {
		t.Error("prijelaz na drugi port nije zabilježen")
	}
}

// Spremanje i ponovno pokretanje smiju samo administrator, svojim očima,
// izravno iz lokalne mreže; kroz tunel ni s istog računala.
func TestZastitePostavkiCvora(t *testing.T) {
	s, put := cvorSPostavkama(t, sadrzajPostavk, nil)
	zatrazeno := make(chan struct{}, 4)
	s.SetPonovnoPokretanje(UslugaSustava, func() { zatrazeno <- struct{}{} })
	s.ponovno.odgoda = 0
	obican := uvjetiZahtjeva{perms: &models.UserPermissions{}, adresa: adresaLokalno, korisnik: adminIvic}
	izvana := kaoAdmin
	izvana.adresa = adresaIzvana
	tunel := kaoAdmin
	tunel.adresa, tunel.tunel = adresaTunela, true
	tudjim := kaoAdmin
	tudjim.tudjim = true
	bezPrava := uvjetiZahtjeva{adresa: adresaLokalno}
	for ime, u := range map[string]uvjetiZahtjeva{"ne-administrator": obican, "izvana": izvana, "kroz tunel": tunel, "tuđim očima": tudjim, "bez prijave": bezPrava} {
		w := spremi(t, s, "/settings", obrazacBloka("cvor", otisakDatoteke(t, put), map[string]string{"kontakt.centar": "COP Primjer"}), u)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: spremanje %d", ime, w.Code)
		}
		w = httptest.NewRecorder()
		s.handlePonovnoPokretanje(w, zahtjevPostavki(http.MethodPost, "/settings/ponovno-pokretanje", nil, u))
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: ponovno pokretanje %d", ime, w.Code)
		}
	}
	if b, _ := os.ReadFile(put); string(b) != sadrzajPostavk {
		t.Error("datoteka promijenjena bez prava")
	}
	select {
	case <-zatrazeno:
		t.Error("ponovno pokretanje bez prava")
	case <-time.After(50 * time.Millisecond):
	}
	// i lokalno s ovog računala smije
	lokalno := kaoAdmin
	lokalno.adresa = "127.0.0.1:5000"
	if w := spremi(t, s, "/settings", obrazacBloka("cvor", otisakDatoteke(t, put), map[string]string{"kontakt.centar": "COP Primjer"}), lokalno); w.Code != http.StatusSeeOther {
		t.Errorf("s ovog računala: %d", w.Code)
	}
	bez := &Server{}
	w := httptest.NewRecorder()
	bez.spremiPostavkeCvora("/settings", http.NotFoundHandler()).ServeHTTP(w, zahtjevPostavki(http.MethodPost, "/settings/postavke-cvora", url.Values{}, kaoAdmin))
	if w.Code != http.StatusNotFound {
		t.Errorf("bez gocop.toml: %d", w.Code)
	}
}

// Ponovno pokretanje u svakom načinu rada: gdje čvor netko podiže, gumb
// traži izlazak (main tada vraća KodPonovnogPokretanja); ručno pokrenut
// čvor gumba nema, a zahtjev se odbija.
func TestPonovnoPokretanjeUSvakomNacinu(t *testing.T) {
	for _, nacin := range []NacinRada{RucnoPokrenut, PodPostavom, UslugaSustava, USpremniku} {
		s, _ := cvorSPostavkama(t, sadrzajPostavk, nil)
		zatrazeno := make(chan struct{}, 1)
		s.SetPonovnoPokretanje(nacin, func() { zatrazeno <- struct{}{} })
		s.ponovno.odgoda = 0
		prikaz := s.postavkeStranice(zahtjevPostavki(http.MethodGet, "/settings", nil, kaoAdmin), "/settings").Ponovno
		if prikaz.Nudi != nacin.Podize() || prikaz.Opis == "" || nacin.Podize() && (prikaz.Potvrda == "" || prikaz.Nevraca == "") {
			t.Errorf("način %d: %+v", nacin, prikaz)
		}
		w := httptest.NewRecorder()
		s.handlePonovnoPokretanje(w, zahtjevPostavki(http.MethodPost, "/settings/ponovno-pokretanje", nil, kaoAdmin))
		var odg struct {
			OK       bool   `json:"ok"`
			Pokrenut string `json:"pokrenut"`
			Error    string `json:"error"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &odg)
		if !nacin.Podize() {
			if w.Code != http.StatusConflict || !strings.Contains(odg.Error, "Ctrl+C") {
				t.Errorf("ručno: %d %+v", w.Code, odg)
			}
			continue
		}
		if w.Code != http.StatusOK || !odg.OK || odg.Pokrenut != prikaz.Pokrenut {
			t.Errorf("način %d: %d %+v", nacin, w.Code, odg)
		}
		select {
		case <-zatrazeno:
		case <-time.After(2 * time.Second):
			t.Errorf("način %d: izlazak nije zatražen", nacin)
		}
		w = httptest.NewRecorder()
		s.handleStanjePokretanja(w, zahtjevPostavki(http.MethodGet, "/settings/ponovno-pokretanje", nil, kaoAdmin))
		if !strings.Contains(w.Body.String(), prikaz.Pokrenut) {
			t.Errorf("stanje pokretanja: %s", w.Body.String())
		}
	}
	if KodPonovnogPokretanja == 0 {
		t.Error("kod ponovnog pokretanja mora biti različit od 0 (systemd Restart=on-failure)")
	}
	// bez funkcije izlaska čvor se smatra ručno pokrenutim
	s, _ := cvorSPostavkama(t, sadrzajPostavk, nil)
	if p := s.prikazPonovnog(nil); p.Nudi || !strings.Contains(p.Opis, "ručno") {
		t.Errorf("bez izlaska: %+v", p)
	}
}

// Pretvorba upisanog u vrijednost postavke
func TestProcitajPolje(t *testing.T) {
	kljuc := func(puni string) config.Kljuc {
		k, ok := config.NadjiKljuc(puni)
		if !ok {
			t.Fatalf("nema %s", puni)
		}
		return k
	}
	if v, p := procitajPolje(kljuc("sync.sve"), "1"); v != true || p != "" {
		t.Errorf("da-ne: %v %q", v, p)
	}
	if v, p := procitajPolje(kljuc("karta.najvise_z"), " 18 "); v != 18 || p != "" {
		t.Errorf("broj: %v %q", v, p)
	}
	if _, p := procitajPolje(config.Kljuc{Tip: reflect.TypeFor[int8]()}, "300"); p == "" {
		t.Error("prevelik broj prošao")
	}
	if _, p := procitajPolje(config.Kljuc{Tip: reflect.TypeFor[[]int]()}, "1"); p == "" {
		t.Error("popis brojeva u obrascu prošao")
	}
	if _, p := procitajPolje(config.Kljuc{Tip: reflect.TypeFor[float64]()}, "1.5"); p == "" {
		t.Error("decimalni broj u obrascu prošao")
	}
	if v, _ := procitajPolje(kljuc("sync.bootstrap"), ""); !reflect.DeepEqual(v, []string{}) {
		t.Errorf("prazan popis: %#v", v)
	}
	if v, _ := procitajPolje(kljuc("web.pouzdani_posrednici"), "172.17.0.1\n 127.0.0.1 ,"); !reflect.DeepEqual(v, []string{"172.17.0.1", "127.0.0.1"}) {
		t.Errorf("popis po retku i zarezu: %#v", v)
	}
	if opis, primjeri := razdijeliOpis("Prvi redak\ndrugi redak.\n  x = 1   # primjer"); opis != "Prvi redak drugi redak." || len(primjeri) != 1 || primjeri[0] != "x = 1   # primjer" {
		t.Errorf("opis: %q %q", opis, primjeri)
	}
	if prikaziVrijednost(false) != "isključeno" || prikaziVrijednost([]string{"a", "b"}) != "a, b" || prikaziVrijednost(4710) != "4710" {
		t.Error("prikaz vrijednosti")
	}
	if korisnikZahtjeva(httptest.NewRequest(http.MethodGet, "/", nil)) != "nepoznat korisnik" {
		t.Error("korisnik bez prijave")
	}
}

// Pomoć kaže gdje se postavke čvora mijenjaju, što se ne može i zašto
func TestPomocOpisujePostavkeCvora(t *testing.T) {
	h := pomocHTML(t)
	for _, mora := range []string{`id="postavke-cvora"`, "Postavke ovog čvora (gocop.toml)", "Ponovno pokreni čvor",
		"iz lokalne mreže", "Mijenjaju se u datoteci", "<code>--restart</code>", "Ctrl+C"} {
		if !strings.Contains(h, mora) {
			t.Errorf("pomoć nema %q", mora)
		}
	}
}

// Rute postavki i ponovnog pokretanja stoje iza zaštite od tuđih stranica
// (CSRF) i prijave: tuđa stranica dobije 403, neprijavljen ide na prijavu.
func TestRutePostavkiIzaZastita(t *testing.T) {
	s, err := NewServer("", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, SupportContact{}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, putanja := range []string{"/settings/postavke-cvora", "/pretplate/postavke-cvora", "/administracija/baza/postavke-cvora",
		"/administracija/karte/postavke-cvora", "/settings/ponovno-pokretanje", "/settings/web-sync/ukljuci", "/settings/provjeri-vezu"} {
		r := httptest.NewRequest(http.MethodPost, putanja, strings.NewReader("dio=cvor"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.Header.Set("Origin", "https://zlo.primjer.hr")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "druga stranica") {
			t.Errorf("%s s tuđe stranice: %d", putanja, w.Code)
		}
		r = httptest.NewRequest(http.MethodPost, putanja, strings.NewReader("dio=cvor"))
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		w = httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
			t.Errorf("%s bez prijave: %d %q", putanja, w.Code, w.Header().Get("Location"))
		}
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/settings/ponovno-pokretanje", nil))
	if w.Code != http.StatusSeeOther {
		t.Errorf("stanje pokretanja bez prijave: %d", w.Code)
	}
}

// Mapa u koju se ne da pisati: ništa se ne mijenja, a poruka kaže zašto
func TestSpremanjeUMapuSamoZaCitanje(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("mapa samo za čitanje ne brani pisanje")
	}
	s, put := cvorSPostavkama(t, sadrzajPostavk, nil)
	if err := os.Chmod(filepath.Dir(put), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(put), 0o755) })
	w := spremi(t, s, "/settings", obrazacBloka("cvor", otisakDatoteke(t, put), map[string]string{"kontakt.centar": "COP Primjer"}), kaoAdmin)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "se ne da zapisati") {
		t.Errorf("mapa samo za čitanje: %d", w.Code)
	}
	if b, _ := os.ReadFile(put); string(b) != sadrzajPostavk {
		t.Error("datoteka promijenjena")
	}
}

// Rubni slučajevi spremanja: datoteka koju program ne zna sigurno izmijeniti,
// posrednici s kojima se čvor ne bi pokrenuo, nepoznat blok, pokvaren
// obrazac i put koji je mapa
func TestSpremanjeRubovi(t *testing.T) {
	slucajevi := map[string]struct{ sadrzaj, poruka string }{
		"tablica u retku": {"kontakt = { centar_telefon = '031 000 000' }\n", "Uredite datoteku u uređivaču teksta"},
		"posrednici":      {"[web]\npouzdani_posrednici = ['nije adresa']\n", "čvor ne bi pokrenuo"},
	}
	for ime, sl := range slucajevi {
		s, put := cvorSPostavkama(t, sl.sadrzaj, nil)
		w := spremi(t, s, "/settings", obrazacBloka("cvor", otisakDatoteke(t, put), map[string]string{"kontakt.centar": "COP Primjer"}), kaoAdmin)
		if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), template.HTMLEscapeString(sl.poruka)) {
			t.Errorf("%s: %d", ime, w.Code)
		}
		if b, _ := os.ReadFile(put); string(b) != sl.sadrzaj {
			t.Errorf("%s: datoteka promijenjena", ime)
		}
	}
	s, put := cvorSPostavkama(t, sadrzajPostavk, nil)
	if w := spremi(t, s, "/settings", obrazacBloka("nepostoji", otisakDatoteke(t, put), nil), kaoAdmin); w.Code != http.StatusBadRequest {
		t.Errorf("nepoznat blok: %d", w.Code)
	}
	r := zahtjevPostavki(http.MethodPost, "/settings/postavke-cvora", nil, kaoAdmin)
	r.Body = io.NopCloser(strings.NewReader("dio=%zz"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.spremiPostavkeCvora("/settings", http.NotFoundHandler()).ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("pokvaren obrazac: %d", w.Code)
	}
	// put postavki je mapa: čita se kao greška, a ne kao da datoteke nema
	mapa := &Server{}
	mapa.SetPostavkeCvora(PostavkeCvora{Put: t.TempDir(), Vrijedi: config.Default(), Pocetna: config.Default()})
	if b := mapa.postavkeStranice(zahtjevPostavki(http.MethodGet, "/", nil, kaoAdmin), "/administracija/baza").Dio("baza"); b.Greska == "" || b.Uredivo {
		t.Errorf("put je mapa: %+v", b)
	}
	if izvorPostavki(nil).za(r, "/settings") != nil || izvorPostavki(s.postavkeStranice).za(zahtjevPostavki(http.MethodGet, "/", nil, kaoAdmin), "/settings") == nil {
		t.Error("izvor postavki rukovatelja")
	}
	if prikaziVrijednost(true) != "uključeno" {
		t.Error("da-ne")
	}
}
