package hidroview

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// probniHV glumi hdv.voda.hr: daje javni ključ, provjerava šifriranu
// lozinku i token, i vraća zadane odgovore po putu.
type probniHV struct {
	t       *testing.T
	kljuc   *rsa.PrivateKey
	lozinka string
	prijava map[string]any // odgovor na get_token kad je lozinka točna
	odgovor map[string]string
	status  map[string]int

	mu      sync.Mutex
	upiti   []string
	korisni string
}

func noviHV(t *testing.T) *probniHV {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	return &probniHV{t: t, kljuc: k, lozinka: "Sifra.2026",
		prijava: map[string]any{"status": "ok", "token": "tok-1"},
		odgovor: map[string]string{}, status: map[string]int{}}
}

func (p *probniHV) javni() string {
	der, _ := x509.MarshalPKIXPublicKey(&p.kljuc.PublicKey)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func (p *probniHV) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	put := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	p.mu.Lock()
	p.upiti = append(p.upiti, r.URL.RequestURI())
	p.mu.Unlock()
	if r.Header.Get("User-Agent") == "" {
		p.t.Error("upit bez imena programa")
	}
	if s, ima := p.status[put]; ima {
		w.WriteHeader(s)
		return
	}
	switch put {
	case "login/get_rsa_key":
		if o, ima := p.odgovor[put]; ima {
			w.Write([]byte(o))
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"rsa_key": p.javni()})
		return
	case "login/get_token":
		p.mu.Lock()
		p.korisni = r.URL.Query().Get("username")
		p.mu.Unlock()
		tajna, _ := hex.DecodeString(r.URL.Query().Get("password"))
		otvoreno, err := rsa.DecryptPKCS1v15(nil, p.kljuc, tajna)
		zbroj := md5.Sum([]byte(p.lozinka))
		if err != nil || !bytes.Equal(otvoreno, []byte(strings.ToUpper(hex.EncodeToString(zbroj[:])))) {
			json.NewEncoder(w).Encode(map[string]string{"status": "wrong_password"})
			return
		}
		json.NewEncoder(w).Encode(p.prijava)
		return
	}
	if r.Header.Get("Authorization") != "Bearer tok-1" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	o, ima := p.odgovor[put]
	if !ima {
		http.NotFound(w, r)
		return
	}
	w.Write([]byte(o))
}

func (p *probniHV) zadnjiUpit() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.upiti[len(p.upiti)-1]
}

func prijavljen(t *testing.T, p *probniHV) *Klijent {
	t.Helper()
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	k := &Klijent{Adresa: " " + srv.URL + "/ ", HTTP: srv.Client()}
	if err := k.Prijava(t.Context(), "marko.babic", "Sifra.2026"); err != nil {
		t.Fatal(err)
	}
	return k
}

// Lozinka ide kao RSA šifra MD5 zbroja velikim slovima, a korisničko ime
// kakvo jest; nakon prijave klijent ima token.
func TestPrijavaSifriraLozinku(t *testing.T) {
	p := noviHV(t)
	k := prijavljen(t, p)
	if !k.Prijavljen() {
		t.Fatal("bez tokena nakon prijave")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.korisni != "marko.babic" {
		t.Errorf("korisnik %q", p.korisni)
	}
	for _, u := range p.upiti {
		if strings.Contains(u, "Sifra.2026") {
			t.Errorf("lozinka otvoreno u upitu %s", u)
		}
	}
}

// Kriva lozinka, blokiran račun i odgovor bez tokena ne daju prijavu, a
// poruka ne nosi ni ime ni lozinku.
func TestPrijavaOdbijena(t *testing.T) {
	slucajevi := []struct {
		ime     string
		lozinka string
		odgovor map[string]any
		zelim   string
	}{
		{"kriva lozinka", "kriva", nil, "prijava odbijena (wrong_password)"},
		{"blokiran", "Sifra.2026", map[string]any{"status": "ok", "token": "x", "account_blocked": true, "account_blocked_reason": "previše pokušaja"}, "račun je blokiran: previše pokušaja"},
		{"bez tokena", "Sifra.2026", map[string]any{"status": "ok"}, "prijava odbijena (ok)"},
	}
	for _, s := range slucajevi {
		t.Run(s.ime, func(t *testing.T) {
			p := noviHV(t)
			if s.odgovor != nil {
				p.prijava = s.odgovor
			}
			srv := httptest.NewServer(p)
			defer srv.Close()
			k := &Klijent{Adresa: srv.URL}
			err := k.Prijava(t.Context(), "marko.babic", s.lozinka)
			if err == nil || err.Error() != s.zelim {
				t.Fatalf("greška %v, želim %q", err, s.zelim)
			}
			if strings.Contains(err.Error(), "marko") || k.Prijavljen() {
				t.Errorf("poruka %q, prijavljen %v", err, k.Prijavljen())
			}
		})
	}
}

// Javni ključ koji nije PEM, nije čitljiv ili nije RSA odbija se prije
// slanja lozinke.
func TestPrijavaLosKljuc(t *testing.T) {
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKIXPublicKey(&ec.PublicKey)
	ecPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	losPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: []byte("smeće")}))
	for ime, s := range map[string]struct{ odgovor, zelim string }{
		"nije PEM":  {`{"rsa_key":"abc"}`, "nije dao javni ključ u PEM obliku"},
		"nečitljiv": {mustJSON(map[string]string{"rsa_key": losPEM}), "javni ključ:"},
		"nije RSA":  {mustJSON(map[string]string{"rsa_key": ecPEM}), "javni ključ nije RSA nego *ecdsa.PublicKey"},
		"nije JSON": {`<html>`, "javni ključ: login/get_rsa_key: odgovor nije očekivani JSON"},
	} {
		t.Run(ime, func(t *testing.T) {
			p := noviHV(t)
			p.odgovor["login/get_rsa_key"] = s.odgovor
			srv := httptest.NewServer(p)
			defer srv.Close()
			err := (&Klijent{Adresa: srv.URL}).Prijava(t.Context(), "marko.babic", "Sifra.2026")
			if err == nil || !strings.Contains(err.Error(), s.zelim) {
				t.Fatalf("greška %v, želim %q", err, s.zelim)
			}
			p.mu.Lock()
			defer p.mu.Unlock()
			for _, u := range p.upiti {
				if strings.Contains(u, "get_token") {
					t.Error("lozinka poslana uz loš ključ")
				}
			}
		})
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// Poslužitelj koji ne daje ključ javlja grešku s putom.
func TestPrijavaBezKljuca(t *testing.T) {
	p := noviHV(t)
	p.status["login/get_rsa_key"] = http.StatusServiceUnavailable
	srv := httptest.NewServer(p)
	defer srv.Close()
	err := (&Klijent{Adresa: srv.URL}).Prijava(t.Context(), "marko.babic", "Sifra.2026")
	if err == nil || !strings.Contains(err.Error(), "javni ključ: login/get_rsa_key: 503") {
		t.Fatalf("greška %v", err)
	}
}

// Postaje iz svih grupa dolaze u jednom popisu, svaka s imenom svoje grupe.
func TestPostajeIzGrupa(t *testing.T) {
	p := noviHV(t)
	p.odgovor["sites/get_verbose"] = `{"status":"ok","sites_get":{"groups_sites":[
		{"group":{"name":"Drava"},"sites":[{"site_id":"9000","logger_id":"900000","last_timestamp":1790000000,
			"site":{"name":"Tikveš","latitude":45.67,"longitude":18.83,"scan_interval_min":15,"station_id":"T1"}}]},
		{"group":{"name":"Dunav"},"sites":[{"site_id":"9001","site":{"name":"Probna"}},{"site_id":"9002","site":{"name":"Druga"}}]}]}}`
	k := prijavljen(t, p)
	ps, err := k.Postaje(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 3 {
		t.Fatalf("postaja %d", len(ps))
	}
	if ps[0].Grupa != "Drava" || ps[0].Site.Naziv != "Tikveš" || ps[0].LoggerID != "900000" ||
		ps[0].Site.Sken != 15 || ps[0].Zadnje != 1790000000 || ps[0].Site.Sirina != 45.67 {
		t.Errorf("prva %+v", ps[0])
	}
	if ps[1].Grupa != "Dunav" || ps[2].Grupa != "Dunav" || ps[2].SiteID != "9002" {
		t.Errorf("grupe %q %q", ps[1].Grupa, ps[2].Grupa)
	}
}

func TestPostajeStatusNijeOK(t *testing.T) {
	p := noviHV(t)
	p.odgovor["sites/get_verbose"] = `{"status":"no_access"}`
	k := prijavljen(t, p)
	if _, err := k.Postaje(t.Context()); err == nil || err.Error() != "popis postaja: no_access" {
		t.Fatalf("greška %v", err)
	}
}

// Oprema skuplja mjerenja zapisivača, instrumenata i obrade; uz obradu
// stoje postavke koje zadnje vrijede.
func TestOprema(t *testing.T) {
	p := noviHV(t)
	p.odgovor["installed_equipment/get"] = `{"status":"ok","installed_equipment":{
		"communicator":{"measurements":[{"msr_id":"m-napon","qty_id":"#logger-$1"}]},
		"instruments":[{"name":"bubbler","measurements":[{"msr_id":"m-vod","qty_id":"#hydro-$6"},{"msr_id":"m-temp","qty_id":"#hydro-$18"}]}],
		"postprocessing":[
			{"msr_id":"m-sred","qty_id":"#hydro-$7","description":"srednji","param_sets":[
				{"valid_from":1,"settings":{"offset":"0.10"}},{"valid_from":2,"settings":{"offset":"0.12"}}]},
			{"msr_id":"m-bez","qty_id":"#hydro-$1","description":"brzina"}],
		"alarms":[{"msr_id":"m-sred","description":"redovna obrana","relational_operator":">","threshold_value":4.5}]}}`
	k := prijavljen(t, p)
	m, a, err := k.Oprema(t.Context(), "9000 A")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.zadnjiUpit(), "site_id=9000+A") {
		t.Errorf("upit %s", p.zadnjiUpit())
	}
	zelim := []Mjerenje{
		{ID: "m-napon", Velicina: "#logger-$1", Odakle: "zapisivač"},
		{ID: "m-vod", Velicina: VelicinaVodostaj, Odakle: "bubbler"},
		{ID: "m-temp", Velicina: VelicinaTempVode, Odakle: "bubbler"},
		{ID: "m-sred", Velicina: VelicinaSrednjiVodostaj, Odakle: "obrada: srednji", Postavke: map[string]string{"offset": "0.12"}},
		{ID: "m-bez", Velicina: VelicinaBrzina, Odakle: "obrada: brzina"},
	}
	if len(m) != len(zelim) {
		t.Fatalf("mjerenja %+v", m)
	}
	for i := range zelim {
		if m[i].ID != zelim[i].ID || m[i].Velicina != zelim[i].Velicina || m[i].Odakle != zelim[i].Odakle ||
			m[i].Postavke["offset"] != zelim[i].Postavke["offset"] || len(m[i].Postavke) != len(zelim[i].Postavke) {
			t.Errorf("mjerenje %d: %+v, želim %+v", i, m[i], zelim[i])
		}
	}
	if len(a) != 1 || a[0].Prag != 4.5 || a[0].Odnos != ">" || a[0].MjerenjeID != "m-sred" {
		t.Errorf("alarmi %+v", a)
	}
}

func TestOpremaGreske(t *testing.T) {
	p := noviHV(t)
	p.odgovor["installed_equipment/get"] = `{"status":"unknown_site"}`
	k := prijavljen(t, p)
	if _, _, err := k.Oprema(t.Context(), "1"); err == nil || err.Error() != "oprema postaje: unknown_site" {
		t.Fatalf("greška %v", err)
	}
	p.status["installed_equipment/get"] = http.StatusInternalServerError
	if _, _, err := k.Oprema(t.Context(), "1"); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("greška %v", err)
	}
}

// Vrijednosti traže razdoblje u sekundama i vraćaju niz od najstarijeg.
func TestVrijednosti(t *testing.T) {
	p := noviHV(t)
	p.odgovor["data/get"] = "timestamp,m-sred\r\n1790000900,0.5\r\n1790000000,0.4\r\nkrivo,1\r\n1790001800,nije broj\r\nsamo-jedno\r\n"
	k := prijavljen(t, p)
	od := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	do := od.Add(time.Hour)
	v, err := k.Vrijednosti(t.Context(), "m sred", od, do)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.zadnjiUpit(), "id0=m+sred&start=1789948800&end=1789952400") {
		t.Errorf("upit %s", p.zadnjiUpit())
	}
	if len(v) != 2 || v[0].Vrijednost != 0.4 || v[1].Vrijednost != 0.5 || !v[0].Kad.Equal(time.Unix(1790000000, 0)) {
		t.Errorf("vrijednosti %+v", v)
	}
}

// CSV sa samim zaglavljem je prazan niz bez greške.
func TestCitajCSVBezRedaka(t *testing.T) {
	v, err := CitajCSV([]byte("timestamp,x\n\n"))
	if err != nil || v != nil {
		t.Fatalf("%v %v", v, err)
	}
}

// Greška dohvata podataka ne prolazi kao prazan niz.
func TestVrijednostiGreska(t *testing.T) {
	p := noviHV(t)
	p.status["data/get"] = http.StatusBadGateway
	k := prijavljen(t, p)
	if _, err := k.Vrijednosti(t.Context(), "m", time.Unix(0, 0), time.Unix(1, 0)); err == nil {
		t.Fatal("greška poslužitelja prošla")
	}
}

// Odbijanje bez tokena nije odbijen token: nema se što zaboraviti.
func TestOdbijanjeBezTokena(t *testing.T) {
	p := noviHV(t)
	p.odgovor["sites/get_verbose"] = `{"status":"ok"}`
	srv := httptest.NewServer(p)
	defer srv.Close()
	_, err := (&Klijent{Adresa: srv.URL}).Postaje(t.Context())
	if err == nil || errors.Is(err, ErrOdbijenToken) || !strings.Contains(err.Error(), "401") {
		t.Fatalf("greška %v", err)
	}
}

func TestOsnovaIKlijent(t *testing.T) {
	if got := (&Klijent{}).osnova(); got != ZadanaAdresa+"/api/v1/" {
		t.Errorf("zadano %q", got)
	}
	if c := (&Klijent{}).klijent(); c.Timeout != time.Minute {
		t.Errorf("zadani klijent %v", c.Timeout)
	}
	vlastiti := &http.Client{}
	if (&Klijent{HTTP: vlastiti}).klijent() != vlastiti {
		t.Error("podmetnuti klijent se ne koristi")
	}
	if _, err := (&Klijent{Adresa: "http://[::1"}).Postaje(t.Context()); err == nil {
		t.Error("neispravna adresa prošla")
	}
	if _, err := (&Klijent{Adresa: "http://hidroview.test", HTTP: &http.Client{Transport: prekinutaVeza{}}}).Postaje(t.Context()); err == nil {
		t.Error("bez poslužitelja nema greške")
	}
}

// Ključ za lozinku ovisi o tajni čvora i odvojen je natpisom od drugih
// ključeva izvedenih iz iste tajne.
func TestKljuc(t *testing.T) {
	a, b := Kljuc([]byte("tajna-a")), Kljuc([]byte("tajna-b"))
	if len(a) != 32 || bytes.Equal(a, b) || !bytes.Equal(a, Kljuc([]byte("tajna-a"))) {
		t.Fatalf("ključevi %x %x", a, b)
	}
	if bytes.Equal(Kljuc(nil), make([]byte, 32)) {
		t.Error("prazna tajna daje nulti ključ")
	}
}

// prekinutaVeza je prijenos koji ne dođe do poslužitelja
type prekinutaVeza struct{}

func (prekinutaVeza) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("veza prekinuta")
}
