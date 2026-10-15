package web

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"gocop/internal/config"
	"gocop/internal/models"
	webassets "gocop/web"
)

// isjecakPostavke je dio stranice s jednom postavkom: od njezina retka do
// sljedeće postavke
func isjecakPostavke(html, kljuc string) string {
	i := strings.Index(html, `data-postavka="`+kljuc+`"`)
	if i < 0 {
		return ""
	}
	ostatak := html[i:]
	if j := strings.Index(ostatak[1:], `data-postavka="`); j >= 0 {
		ostatak = ostatak[:j+1]
	}
	return ostatak
}

// oznakaBloka je početna oznaka bloka postavki sa sidrom, npr.
// <details id="postavke-cvor" …>
func oznakaBloka(html, sidro string) string {
	i := strings.Index(html, `id="`+sidro+`"`)
	if i < 0 {
		return ""
	}
	pocetak := strings.LastIndex(html[:i], "<")
	kraj := strings.Index(html[i:], ">")
	return html[pocetak : i+kraj+1]
}

func stavkaBloka(b *BlokPostavki, kljuc string) StavkaPostavke {
	for _, st := range b.Stavke {
		if st.Kljuc == kljuc {
			return st
		}
	}
	return StavkaPostavke{}
}

// Na stranici Čvor i mreža postavke iz datoteke stoje sklopljene, pa
// kartice ispod ne siđu za cijeli ekran. Sklopljene ostaju i kad spremljena
// promjena čeka (to javlja traka), a otvore se same kad spremanje ne uspije;
// gumb za ponovno pokretanje je izvan sklopa.
func TestPostavkeNaCvoruSklopljene(t *testing.T) {
	s, put := cvorSPostavkama(t, sadrzajPostavk, nil)
	s.SetPonovnoPokretanje(UslugaSustava, func() {})
	html := iscrtajPostavke(t, s, "/settings", kaoAdmin)
	for _, sidro := range []string{"postavke-cvor", "postavke-mreza"} {
		oznaka := oznakaBloka(html, sidro)
		if !strings.HasPrefix(oznaka, "<details") || strings.Contains(oznaka, " open") {
			t.Errorf("%s nije sklopljen: %q", sidro, oznaka)
		}
	}
	cvor := strings.Index(html, `id="postavke-cvor"`)
	kraj := cvor + strings.Index(html[cvor:], "</details>")
	if gumb := strings.Index(html, `id="ponovno-pokretanje"`); gumb < kraj {
		t.Error("ponovno pokretanje je u sklopu postavki")
	}
	// ostale stranice nemaju sklop
	if o := oznakaBloka(iscrtajPostavke(t, s, "/administracija/opcije", kaoAdmin), "postavke-cvora"); strings.HasPrefix(o, "<details") {
		t.Errorf("Opcije: %q", o)
	}
	if w := spremi(t, s, "/settings", obrazacBloka("cvor", otisakDatoteke(t, put), map[string]string{"kontakt.centar": "COP Primjer"}), kaoAdmin); w.Code != http.StatusSeeOther {
		t.Fatalf("spremanje: %d", w.Code)
	}
	html = iscrtajPostavke(t, s, "/settings", kaoAdmin)
	if o := oznakaBloka(html, "postavke-cvor"); strings.Contains(o, " open") || !strings.Contains(html, "vrijedi nakon ponovnog pokretanja čvora") {
		t.Errorf("promjena koja čeka: blok %q, traka %v", o, strings.Contains(html, "vrijedi nakon ponovnog pokretanja čvora"))
	}
	// neispravan upis: odgovor pokaže blok otvoren, s porukom
	w := spremi(t, s, "/settings", obrazacBloka("mreza", otisakDatoteke(t, put), map[string]string{"sync.auto_sync": "pet minuta"}), kaoAdmin)
	if o := oznakaBloka(w.Body.String(), "postavke-mreza"); w.Code != http.StatusUnprocessableEntity || !strings.Contains(o, " open") {
		t.Errorf("neispravan upis: %d %q", w.Code, o)
	}
}

// Do postavki „Prati sve” i povijesti očitanja dolazi se iz Administracije,
// a na stranici stoje odmah ispod obavijesti da čvor prati sve, koja na njih
// i upućuje.
func TestPutDoPostavkiPretplata(t *testing.T) {
	adm, err := fs.ReadFile(webassets.Files, "templates/administracija.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(adm), `href="/pretplate"`) {
		t.Error("iz Administracije nema puta do stranice Što ovo računalo prati")
	}
	s, _ := cvorSPostavkama(t, sadrzajPostavk, nil)
	r := zahtjevPostavki(http.MethodGet, "/pretplate", nil, kaoAdmin)
	data := SubscriptionsPageData{CurrentUser: adminIvic, Permissions: kaoAdmin.perms, All: true, Postavke: s.postavkeStranice(r, "/pretplate")}
	w := httptest.NewRecorder()
	if err := predlozakStranice(t, "pretplate.html").ExecuteTemplate(w, "pretplate.html", data); err != nil {
		t.Fatal(err)
	}
	html := w.Body.String()
	obavijest := strings.Index(html, "Ovaj čvor prati <b>sve</b>")
	blok := strings.Index(html, `id="postavke-cvora"`)
	pretplate := strings.Index(html, "Pretplate <span")
	if obavijest < 0 || blok < obavijest || blok > pretplate {
		t.Errorf("redoslijed: obavijest %d, postavke %d, pretplate %d", obavijest, blok, pretplate)
	}
	if !strings.Contains(html[obavijest:blok], `href="#postavke-cvora"`) {
		t.Error("obavijest da čvor prati sve ne upućuje na postavku")
	}
}

// Kad je port 80 zauzet, čvor sam prijeđe na 8080. To nije promjena koja
// čeka ponovno pokretanje: nakon njega čvor opet prijeđe na 8080.
func TestPrijelazNa8080NeCekaPonovno(t *testing.T) {
	s, put := cvorSPostavkama(t, "addr = ':80'\n", nil)
	s.SetAddr(":8080")
	p := s.postavkeStranice(zahtjevPostavki(http.MethodGet, "/settings", nil, kaoAdmin), "/settings")
	st := stavkaBloka(p.Dio("cvor"), "addr")
	if st.CekaPonovno || st.Prikaz != ":8080" || !strings.Contains(st.Zamjena, "zauzet") {
		t.Errorf("addr nakon prijelaza: %+v", st)
	}
	redak := isjecakPostavke(iscrtajPostavke(t, s, "/settings", kaoAdmin), "addr")
	if strings.Contains(redak, "nakon ponovnog pokretanja") || !strings.Contains(redak, "sam prešao na") {
		t.Errorf("redak addr nakon prijelaza:\n%s", redak)
	}
	// datoteka ručno promijenjena na drugi port: to stvarno čeka
	if err := os.WriteFile(put, []byte("addr = ':8090'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p = s.postavkeStranice(zahtjevPostavki(http.MethodGet, "/settings", nil, kaoAdmin), "/settings")
	if st := stavkaBloka(p.Dio("cvor"), "addr"); !st.CekaPonovno || st.Zamjena != "" {
		t.Errorf("addr nakon ručne izmjene: %+v", st)
	}
}

// Tekstovi uz ime i naziv čvora ne proturječe jedan drugome i ne ponavljaju
// objašnjenje; „ovdje” iz datoteke na stranici ne zbunjuje.
func TestTekstoviImenaCvora(t *testing.T) {
	s, _ := cvorSPostavkama(t, sadrzajPostavk, nil)
	html := iscrtajPostavke(t, s, "/settings", kaoAdmin)
	id, naziv := isjecakPostavke(html, "node.id"), isjecakPostavke(html, "node.name")
	if strings.Count(id, "upisuje zapise") != 1 || strings.Contains(id, "upiše ovdje") || !strings.Contains(id, "ni u datoteci") {
		t.Errorf("ime čvora:\n%s", id)
	}
	if strings.Contains(naziv, "kao i ime čvora") || !strings.Contains(naziv, "u datoteci gocop.toml") {
		t.Errorf("naziv čvora:\n%s", naziv)
	}
	if strings.Contains(html, "Ove se mijenjaju samo u datoteci") {
		t.Error("natpis kaže da se sve ispod mijenja u datoteci, a ime čvora se ne mijenja")
	}
	baza := isjecakPostavke(iscrtajPostavke(t, s, "/administracija/baza", kaoAdmin), "db")
	if strings.Contains(baza, "ova datoteka") {
		t.Errorf("baza: „ova datoteka” na stranici nije jasno:\n%s", baza)
	}
}

// Zatvoren Wikimedijin izvor pločica: polje nudi zamjenu s kojom čvor već
// radi, a spremanje je upiše u datoteku.
func TestZatvorenIzvorKarteSeZamjenjuje(t *testing.T) {
	stari := "[karta]\nplocice = 'https://maps.wikimedia.org/osm-intl/{z}/{x}/{y}.png'\nzasluge = '© Wikimedia'\nnajvise_z = 17\n"
	s, put := cvorSPostavkama(t, stari, nil)
	s.postavke.p.Vrijedi.ZamijeniZatvoreneIzvore() // kao main pri pokretanju
	p := s.postavkeStranice(zahtjevPostavki(http.MethodGet, "/administracija/opcije", nil, kaoAdmin), "/administracija/opcije")
	st := stavkaBloka(p.Dio("opcije"), "karta.plocice")
	if st.Polje != config.ZadaniIzvorPlocica || st.CekaPonovno || st.Zamjena == "" || !strings.Contains(st.UDatoteci, "wikimedia") || p.CekaPonovno {
		t.Errorf("zatvoren izvor: %+v", st)
	}
	w := spremi(t, s, "/administracija/opcije", obrazacBloka("opcije", otisakDatoteke(t, put), map[string]string{
		"karta.plocice": st.Polje, "karta.zasluge": stavkaBloka(p.Dio("opcije"), "karta.zasluge").Polje, "karta.najvise_z": "17"}), kaoAdmin)
	if w.Code != http.StatusSeeOther || strings.Contains(w.Header().Get("Location"), url.QueryEscape("Ništa nije promijenjeno.")) {
		t.Fatalf("spremanje zamjene: %d %s", w.Code, w.Header().Get("Location"))
	}
	b, _ := os.ReadFile(put)
	if !strings.Contains(string(b), config.ZadaniIzvorPlocica) || strings.Contains(string(b), "Wikimedia") {
		t.Errorf("u datoteci:\n%s", b)
	}
}

// Postavka koju administrator nije dirao ostaje u datoteci kakva jest, i
// kad ima razmak na početku ili kraju.
func TestNedirnutoOstajeKakvoJest(t *testing.T) {
	sadrzaj := "[kontakt]\ncentar = ' COP Osijek '\n"
	s, put := cvorSPostavkama(t, sadrzaj, nil)
	w := spremi(t, s, "/settings", obrazacBloka("cvor", otisakDatoteke(t, put), map[string]string{
		"kontakt.centar": " COP Osijek ", "kontakt.centar_telefon": "031 100 200"}), kaoAdmin)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("spremanje: %d", w.Code)
	}
	b, _ := os.ReadFile(put)
	if !strings.Contains(string(b), "centar = ' COP Osijek '") || !strings.Contains(string(b), "centar_telefon = '031 100 200'") {
		t.Errorf("u datoteci:\n%s", b)
	}
}

// Primjeri uz polje su vrijednosti koje se upisuju, bez navodnika i znaka #
func TestPrimjeriUzPolje(t *testing.T) {
	for redak, ocekivano := range map[string]PrimjerPostavke{
		"centar = 'COP Osijek'":                                  {Vrijednost: "COP Osijek"},
		"plocice = ''                         # bez karte":       {Napomena: "bez karte"},
		"povijest_mjeseci = 12   # terenski laptop ili mobitel":  {Vrijednost: "12", Napomena: "terenski laptop ili mobitel"},
		"plocice = '/karta/{z}/{x}/{y}.png'   # preuzete, # bez": {Vrijednost: "/karta/{z}/{x}/{y}.png", Napomena: "preuzete, # bez"},
		"posve drukčiji redak":                                   {Vrijednost: "posve drukčiji redak"},
	} {
		ime, _, _ := strings.Cut(redak, " ")
		if g := primjerIzRetka(ime, redak); g != ocekivano {
			t.Errorf("%q: %+v", redak, g)
		}
	}
	s, _ := cvorSPostavkama(t, sadrzajPostavk, nil)
	html := iscrtajPostavke(t, s, "/settings", kaoAdmin)
	redak := isjecakPostavke(html, "kontakt.centar")
	if strings.Contains(redak, "centar = ") || !strings.Contains(redak, `Npr. <span class="mono">COP Osijek</span>`) {
		t.Errorf("primjer uz centar:\n%s", redak)
	}
}

// „Prati sve” je kvačica s nazivom postavke, a objašnjenje i poruka o
// promjeni govore riječima, a ne true, false, da i ne.
func TestKvacicaPratiSve(t *testing.T) {
	s, put := cvorSPostavkama(t, sadrzajPostavk, nil)
	obrazac := obrazacBloka("pretplate", otisakDatoteke(t, put), nil)
	obrazac["postavka.sync.sve"] = []string{"0", "1"}
	if w := spremi(t, s, "/pretplate", obrazac, kaoAdmin); w.Code != http.StatusSeeOther {
		t.Fatalf("spremanje: %d", w.Code)
	}
	redak := isjecakPostavke(iscrtajPostavke(t, s, "/pretplate", kaoAdmin), "sync.sve")
	for _, nema := range []string{"true =", "false =", "<span>da</span>", "U datoteci je"} {
		if strings.Contains(redak, nema) {
			t.Errorf("uz Prati sve stoji %q:\n%s", nema, redak)
		}
	}
	if !strings.Contains(redak, "Do ponovnog pokretanja vrijedi") || !strings.Contains(redak, "uključeno") {
		t.Errorf("poruka o promjeni:\n%s", redak)
	}
}

// Izvana i kroz tunel ponovno pokretanje kaže zašto ga nema, svojim
// riječima, a traka ne nudi gumb kojeg nema.
func TestPonovnoPokretanjeIzvanaKazeZasto(t *testing.T) {
	s, put := cvorSPostavkama(t, sadrzajPostavk, nil)
	s.SetPonovnoPokretanje(UslugaSustava, func() {})
	if w := spremi(t, s, "/settings", obrazacBloka("cvor", otisakDatoteke(t, put), map[string]string{"kontakt.centar": "COP Primjer"}), kaoAdmin); w.Code != http.StatusSeeOther {
		t.Fatalf("spremanje: %d", w.Code)
	}
	tunel := kaoAdmin
	tunel.adresa, tunel.tunel = adresaTunela, true
	html := iscrtajPostavke(t, s, "/settings", tunel)
	ponovno := html[strings.Index(html, `id="ponovno-pokretanje"`):]
	ponovno = ponovno[:strings.Index(ponovno, `<div class="card">`)]
	if !strings.Contains(ponovno, "Čvor se ponovno pokreće samo") || strings.Contains(ponovno, "Postavke ovog čvora mijenjaju se") {
		t.Errorf("ponovno pokretanje kroz tunel:\n%s", ponovno)
	}
	if !strings.Contains(html, "Kako ponovno pokrenuti čvor</a>") || strings.Contains(html, ">Ponovno pokreni čvor</a>") {
		t.Error("traka kroz tunel nudi gumb")
	}
	if !strings.Contains(iscrtajPostavke(t, s, "/pretplate", kaoAdmin), ">Ponovno pokreni čvor</a>") {
		t.Error("traka lokalno ne nudi ponovno pokretanje")
	}
	w := httptest.NewRecorder()
	s.handlePonovnoPokretanje(w, zahtjevPostavki(http.MethodPost, "/settings/ponovno-pokretanje", nil, tunel))
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "ponovno pokreće samo") {
		t.Errorf("zahtjev kroz tunel: %d %s", w.Code, w.Body.String())
	}
}

// Postavke čvora mijenjaju se preko Tailscalea samo kad je zahtjev stigao na
// Tailscale adresu ovog računala, i dok je [web] tailscale_lokalno uključen
// (Odluke (12. 10. 2026.)). Izvor 100.x koji stigne na drugu adresu ostaje
// izvana.
func TestPostavkePrekoTailscalea(t *testing.T) {
	admin := uvjetiZahtjeva{perms: &models.UserPermissions{IsGlobalAdmin: true}, adresa: "100.90.1.2:51000", korisnik: adminIvic}
	if smijeMijenjatiCvor(zahtjevPostavki(http.MethodGet, "/settings", nil, admin)) == nil {
		t.Error("izvor 100.x na drugu adresu smije mijenjati postavke čvora")
	}
	saKlijentom := func(k Klijent) *http.Request {
		r := zahtjevPostavki(http.MethodGet, "/settings", nil, admin)
		return r.WithContext(context.WithValue(r.Context(), kljucKlijenta{}, k))
	}
	ts := ZadaniPosrednici().Klijent(zahtjevPostavki(http.MethodGet, "/settings", nil, admin))
	ts.NaTailscale, ts.TailscaleLokalno = true, true
	if z := smijeMijenjatiCvor(saKlijentom(ts)); z != nil {
		t.Errorf("zahtjev na Tailscale adresu ovog računala odbijen: %v", z)
	}
	ts.TailscaleLokalno = false
	if smijeMijenjatiCvor(saKlijentom(ts)) == nil {
		t.Error("uz tailscale_lokalno = false Tailscale smije mijenjati postavke čvora")
	}
	ts.TailscaleLokalno, ts.KrozPosrednika = true, true
	if smijeMijenjatiCvor(saKlijentom(ts)) == nil {
		t.Error("kroz posrednika Tailscale smije mijenjati postavke čvora")
	}
}

// Poslije spremanja stranica se otvara na vrhu, gdje su poruka i traka
func TestSpremanjeVodiNaPoruku(t *testing.T) {
	s, put := cvorSPostavkama(t, sadrzajPostavk, nil)
	w := spremi(t, s, "/pretplate", obrazacBloka("pretplate", otisakDatoteke(t, put), map[string]string{"vodostaji.povijest_mjeseci": "6"}), kaoAdmin)
	if l := w.Header().Get("Location"); w.Code != http.StatusSeeOther || strings.Contains(l, "#") || !strings.HasPrefix(l, "/pretplate?success=") {
		t.Errorf("preusmjerenje: %d %s", w.Code, l)
	}
}

// Održavanje baze pokazuje gdje čvor stvarno drži datoteke, i kad je u
// gocop.toml prazno
func TestPutanjeNaDisku(t *testing.T) {
	s, _ := cvorSPostavkama(t, sadrzajPostavk, nil)
	s.postavke.p.Putanje = map[string]string{"arhiva": "/srv/gocop/vodostaji.db", "db": "data/gocop.db"}
	s.postavke.p.Vrijedi.DB = "data/gocop.db"
	html := iscrtajPostavke(t, s, "/administracija/baza", kaoAdmin)
	if r := isjecakPostavke(html, "arhiva"); !strings.Contains(r, "/srv/gocop/vodostaji.db") || !strings.Contains(r, `class="c-muted">prazno`) {
		t.Errorf("arhiva:\n%s", r)
	}
	if r := isjecakPostavke(html, "db"); strings.Contains(r, "Čvor koristi") {
		t.Errorf("baza s istom putanjom:\n%s", r)
	}
}
