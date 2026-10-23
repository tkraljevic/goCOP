package main

// WEB sync u pokretanju (povezivost.go): utičnica se otvara tek kad web
// sluša i uz upisanu oznaku padova, provjera mreže ide na lažne STUN
// poslužitelje na 127.0.0.1, uredno gašenje obriše oznaku, a dvaput
// zatečena oznaka drži WEB sync isključenim dok ga administrator ne uključi.

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gocop/internal/config"
	"gocop/internal/peers"
	"gocop/internal/razmjena"
	"gocop/internal/web"
)

// lazniWeb je poslužitelj koji sluša kad test zatvori slusa
type lazniWeb struct {
	slusa    chan struct{}
	stanje   func() web.StanjePovezivosti
	ukljuci  func() error
	provjeri func(context.Context) web.ProvjeraVeze
	zadnja   func() *web.ProvjeraVeze
}

func noviLazniWeb() *lazniWeb { return &lazniWeb{slusa: make(chan struct{})} }

func (l *lazniWeb) Slusa() <-chan struct{} { return l.slusa }

func (l *lazniWeb) SetPovezivost(stanje func() web.StanjePovezivosti, ukljuci func() error) {
	l.stanje, l.ukljuci = stanje, ukljuci
}

func (l *lazniWeb) SetProvjeraVeze(provjeri func(context.Context) web.ProvjeraVeze, zadnja func() *web.ProvjeraVeze) {
	l.provjeri, l.zadnja = provjeri, zadnja
}

// dnevnikPovezivosti hvata retke dnevnika; bez međuspremnika gorutina
// WEB synca čeka dok test ne pročita redak, pa je redoslijed određen
type dnevnikPovezivosti chan string

func (d dnevnikPovezivosti) javi(format string, args ...any) { d <- fmt.Sprintf(format, args...) }

func (d dnevnikPovezivosti) cekaj(t *testing.T, sadrzi string) string {
	t.Helper()
	select {
	case r := <-d:
		if !strings.Contains(r, sadrzi) {
			t.Fatalf("redak %q ne sadrži %q", r, sadrzi)
		}
		return r
	case <-time.After(10 * time.Second):
		t.Fatalf("nema retka s %q", sadrzi)
	}
	return ""
}

// lazniSTUN odgovara na svaki STUN upit adresom pošiljatelja
// (XOR-MAPPED-ADDRESS), kao poslužitelj iza kojeg nema NAT-a
func lazniSTUN(t *testing.T) string {
	t.Helper()
	odmah := make(chan struct{})
	close(odmah)
	return lazniSTUNSa(t, odmah, nil)
}

// lazniSTUNSa odgovara tek kad se pusti zatvori, a svaki primljeni upit
// javi na upiti, kad ga ima
func lazniSTUNSa(t *testing.T, pusti <-chan struct{}, upiti chan<- struct{}) string {
	t.Helper()
	c, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	go func() {
		b := make([]byte, 1500)
		for {
			n, od, err := c.ReadFrom(b)
			if err != nil {
				return
			}
			if upiti != nil {
				upiti <- struct{}{}
			}
			if a, ok := od.(*net.UDPAddr); ok && n >= 20 {
				odg := odgovorSTUN(b[:20], a)
				go func() {
					<-pusti
					_, _ = c.WriteTo(odg, od) // izgubljen odgovor nadoknadi idući upit
				}()
			}
		}
	}()
	return c.LocalAddr().String()
}

func odgovorSTUN(upit []byte, a *net.UDPAddr) []byte {
	o := make([]byte, 32)
	binary.BigEndian.PutUint16(o[0:], 0x0101)
	binary.BigEndian.PutUint16(o[2:], 12)
	copy(o[4:20], upit[4:20]) // kolačić i ID transakcije
	binary.BigEndian.PutUint16(o[20:], 0x0020)
	binary.BigEndian.PutUint16(o[22:], 8)
	o[25] = 0x01
	binary.BigEndian.PutUint16(o[26:], uint16(a.Port)^0x2112)
	for i, x := range a.IP.To4() {
		o[28+i] = x ^ o[4+i]
	}
	return o
}

func oznakaPadova(t *testing.T, dir string) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, peers.DatotekaPadaPovezivosti))
	if os.IsNotExist(err) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b), true
}

func uOznaci(t *testing.T, dir, sadrzaj string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, peers.DatotekaPadaPovezivosti), []byte(sadrzaj), 0o600); err != nil {
		t.Fatal(err)
	}
}

func prijavaWebSynca(stun ...string) config.Povezivost {
	return config.Povezivost{WebSyncPrijava: true, Stun: stun}
}

// Uz web_sync_prijava = false nema ni oznake ni stanja na poslužitelju.
func TestPovezivostBezPrijave(t *testing.T) {
	dir, w := t.TempDir(), noviLazniWeb()
	close(w.slusa)
	stani := pokreniPovezivost(context.Background(), config.Default().Povezivost, dir, w, make(dnevnikPovezivosti).javi)
	stani()
	if _, ima := oznakaPadova(t, dir); ima || w.stanje != nil {
		t.Errorf("bez prijave: oznaka %v, stanje %v", ima, w.stanje != nil)
	}
}

// Utičnica se otvara tek kad web sluša, a oznaka se upiše prije nje. Nakon
// stabilnog rada brojač je nula, a uredno gašenje obriše oznaku i pusti port.
func TestPovezivostRadi(t *testing.T) {
	stari := stabilanRadPovezivosti
	stabilanRadPovezivosti = 50 * time.Millisecond
	t.Cleanup(func() { stabilanRadPovezivosti = stari })
	dir, w, d := t.TempDir(), noviLazniWeb(), make(dnevnikPovezivosti)
	uOznaci(t, dir, "0\n") // prethodno pokretanje palo jednom
	stani := pokreniPovezivost(context.Background(), prijavaWebSynca(lazniSTUN(t), lazniSTUN(t)), dir, w, d.javi)
	if s, _ := oznakaPadova(t, dir); s != "0\n" || w.stanje().NakonPadova {
		t.Fatalf("prije nego web sluša: oznaka %q, stanje %+v", s, w.stanje())
	}
	close(w.slusa)
	redak := d.cekaj(t, "povezivost: UDP radi na :")
	if s, _ := oznakaPadova(t, dir); s != "1\n" {
		t.Errorf("oznaka dok radi: %q", s)
	}
	port, err := strconv.Atoi(strings.TrimPrefix(redak, "povezivost: UDP radi na :"))
	if err != nil {
		t.Fatal(err)
	}
	if r := d.cekaj(t, "povezivost: NAT je lak i čuva port. STUN: 127.0.0.1 "); strings.Count(r, "127.0.0.1") != 2 {
		t.Errorf("provjera mreže: %q", r)
	}
	for rok := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if s, _ := oznakaPadova(t, dir); s == "0\n" {
			break
		}
		if time.Now().After(rok) {
			t.Fatal("brojač padova nije vraćen na nulu nakon stabilnog rada")
		}
	}
	stani()
	if _, ima := oznakaPadova(t, dir); ima {
		t.Error("uredno gašenje nije obrisalo oznaku")
	}
	c, err := net.ListenPacket("udp4", ":"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("port :%d nije pušten: %v", port, err)
	}
	c.Close()
}

// Oznaka zatečena dvaput: WEB sync ne kreće, gašenje oznaku ne briše, a
// tek je gumb obriše.
func TestPovezivostNakonPadova(t *testing.T) {
	dir, w, d := t.TempDir(), noviLazniWeb(), make(dnevnikPovezivosti, 4)
	uOznaci(t, dir, "1\n")
	stani := pokreniPovezivost(context.Background(), prijavaWebSynca(), dir, w, d.javi)
	d.cekaj(t, "povezivost: WEB sync je isključen")
	close(w.slusa)
	stani()
	if s, _ := oznakaPadova(t, dir); s != "1\n" || !w.stanje().NakonPadova || w.stanje().UkljucujeSe {
		t.Fatalf("nakon padova: oznaka %q, stanje %+v", s, w.stanje())
	}
	if err := w.ukljuci(); err != nil {
		t.Fatal(err)
	}
	if _, ima := oznakaPadova(t, dir); ima || !w.stanje().UkljucujeSe {
		t.Errorf("nakon gumba: oznaka %v, stanje %+v", ima, w.stanje())
	}
	// iduće pokretanje opet otvara utičnicu
	if p := peers.ProcitajPadPovezivosti(dir); p.Iskljucen() {
		t.Errorf("iduće pokretanje: %+v", p)
	}
}

// Oznaka koja se ne da obrisati (na njezinu mjestu je puna mapa): gumb
// javi grešku, a stanje se ne mijenja.
func TestPovezivostGumbBezBrisanja(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, peers.DatotekaPadaPovezivosti, "a"), 0o700); err != nil {
		t.Fatal(err)
	}
	p := &povezivost{pad: peers.ProcitajPadPovezivosti(dir), nakonPadova: true}
	if err := p.ukljuci(); err == nil || p.stanje().UkljucujeSe {
		t.Errorf("brisanje pune mape: %v, %+v", err, p.stanje())
	}
}

// Bez oznake (mapa baze se ne da pisati) WEB sync ne kreće; otkazan prije
// nego web sluša ne upiše ništa.
func TestPovezivostNeKrece(t *testing.T) {
	nema := filepath.Join(t.TempDir(), "nema")
	w, d := noviLazniWeb(), make(dnevnikPovezivosti)
	stani := pokreniPovezivost(context.Background(), prijavaWebSynca(), nema, w, d.javi)
	close(w.slusa)
	d.cekaj(t, "ne da se upisati")
	stani()

	dir := t.TempDir()
	stani = pokreniPovezivost(context.Background(), prijavaWebSynca(), dir, noviLazniWeb(), make(dnevnikPovezivosti).javi)
	stani()
	if _, ima := oznakaPadova(t, dir); ima {
		t.Error("oznaka upisana prije nego je web slušao")
	}
}

// Zauzet port: nasumičan uz upozorenje; prazan popis STUN-a ne pita nikoga.
func TestPovezivostZauzetPortBezSTUNa(t *testing.T) {
	zauzet, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer zauzet.Close()
	port := zauzet.LocalAddr().(*net.UDPAddr).Port
	cfg := prijavaWebSynca()
	cfg.UDPPort = port
	dir, w, d := t.TempDir(), noviLazniWeb(), make(dnevnikPovezivosti)
	stani := pokreniPovezivost(context.Background(), cfg, dir, w, d.javi)
	close(w.slusa)
	d.cekaj(t, fmt.Sprintf("UDP port %d se ne da otvoriti", port))
	if r := d.cekaj(t, "povezivost: UDP radi na :"); strings.HasSuffix(r, ":"+strconv.Itoa(port)) {
		t.Errorf("isti port: %q", r)
	}
	d.cekaj(t, "provjera mreže je isključena (stun = [])")
	stani()
}

// Utičnica koja se nikako ne da otvoriti: WEB sync ne radi, a gašenje
// obriše oznaku jer je ovo pokretanje upisalo. Oznaka koja se ne da
// obrisati javi se u dnevniku.
func TestPovezivostBezUticnice(t *testing.T) {
	stari := otvoriUticnicu
	otvoriUticnicu = func(int) (net.PacketConn, error, error) { return nil, nil, errors.New("nema mreže") }
	t.Cleanup(func() { otvoriUticnicu = stari })
	dir, w, d := t.TempDir(), noviLazniWeb(), make(dnevnikPovezivosti)
	stani := pokreniPovezivost(context.Background(), prijavaWebSynca(), dir, w, d.javi)
	close(w.slusa)
	d.cekaj(t, "UDP utičnica se ne da otvoriti, pa WEB sync ne radi: nema mreže")
	for rok := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, ima := oznakaPadova(t, dir); !ima {
			break // prekid ovog pokretanja više se ne broji kao pad WEB synca
		}
		if time.Now().After(rok) {
			t.Fatal("oznaka ostala dok čvor radi bez utičnice")
		}
	}
	stani()

	if err := os.MkdirAll(filepath.Join(dir, peers.DatotekaPadaPovezivosti, "a"), 0o700); err != nil {
		t.Fatal(err)
	}
	d = make(dnevnikPovezivosti, 1)
	p := &povezivost{pad: peers.ProcitajPadPovezivosti(dir), javi: d.javi, upisano: true}
	p.zatvori()
	d.cekaj(t, "ne da se obrisati, pa iduće pokretanje broji pad")
}

// U pravom pokretanju: uz web_sync_prijava = false nitko ne drži UDP port,
// a uz true ga WEB sync drži dok čvor radi; uredno gašenje pusti port i
// obriše oznaku padova.
func TestPovezivostURunu(t *testing.T) {
	if testing.Short() {
		t.Skip("podiže cijeli čvor")
	}
	c, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		t.Fatal(err)
	}
	port := c.LocalAddr().(*net.UDPAddr).Port
	c.Close()
	slobodan := func() bool {
		c, err := net.ListenPacket("udp4", ":"+strconv.Itoa(port))
		if err == nil {
			c.Close()
		}
		return err == nil
	}
	dir := t.TempDir()
	for _, prijava := range []bool{false, true} {
		postavke := fmt.Sprintf("[povezivost]\nweb_sync_prijava = %v\nudp_port = %d\nstun = []\n", prijava, port)
		if err := os.WriteFile(filepath.Join(dir, config.FileName), []byte(postavke), 0o600); err != nil {
			t.Fatal(err)
		}
		_, _, ugasi := cvorURunu(t, "-db", filepath.Join(dir, "gocop.db"), "-node", "pperic-thinkpad")
		drzi := false
		for rok := time.Now().Add(10 * time.Second); prijava && !drzi && time.Now().Before(rok); time.Sleep(20 * time.Millisecond) {
			drzi = !slobodan()
		}
		if _, ima := oznakaPadova(t, dir); drzi != prijava || ima != prijava || prijava != !slobodan() {
			t.Errorf("prijava %v: port zauzet %v, oznaka %v", prijava, drzi, ima)
		}
		if kod := ugasi(); kod != 0 {
			t.Errorf("prijava %v: gašenje s kodom %d", prijava, kod)
		}
		if _, ima := oznakaPadova(t, dir); ima || !slobodan() {
			t.Errorf("prijava %v nakon gašenja: oznaka %v, port slobodan %v", prijava, ima, slobodan())
		}
	}
}

func TestRazrijesiSTUN(t *testing.T) {
	ctx := context.Background()
	if a, err := razrijesiSTUN(ctx, "localhost:3478"); err != nil || a.String() != "127.0.0.1:3478" {
		t.Errorf("localhost: %v %v", a, err)
	}
	for _, krivo := range []string{"bez-porta", "localhost:70000", "nepostoji.invalid:3478"} {
		if a, err := razrijesiSTUN(ctx, krivo); err == nil {
			t.Errorf("%s: %v bez greške", krivo, a)
		}
	}
	p := posluziteljiSTUN(ctx, []string{"stun.l.google.com:19302x", "localhost:3478"})
	if len(p) != 2 || p[0].Ime != "Google" || p[0].Adresa != nil || p[1].Ime != "localhost" || p[1].Adresa == nil {
		t.Errorf("poslužitelji: %+v", p)
	}
}

func TestImeSTUN(t *testing.T) {
	for adresa, ime := range map[string]string{
		"stun.l.google.com:19302":  "Google",
		"STUN.Cloudflare.com:3478": "Cloudflare",
		"stun.primjer.hr:3478":     "stun.primjer.hr",
		"stun.primjer.hr":          "stun.primjer.hr",
	} {
		if got := imeSTUN(adresa); got != ime {
			t.Errorf("%s: %q", adresa, got)
		}
	}
}

// Upozorenje quic-go-a o međuspremniku gasi se samo kad ga administrator
// nije sam postavio.
func TestUtisajQuic(t *testing.T) {
	const k = "QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING"
	t.Setenv(k, "0")
	utisajQuic()
	if v := os.Getenv(k); v != "0" {
		t.Errorf("postavljeno: %q", v)
	}
	if err := os.Unsetenv(k); err != nil {
		t.Fatal(err)
	}
	utisajQuic()
	if v := os.Getenv(k); v != "1" {
		t.Errorf("nepostavljeno: %q", v)
	}
}

// slobodanUDP je UDP port koji je upravo bio slobodan
func slobodanUDP(t *testing.T) int {
	t.Helper()
	c, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).Port
}

// Uz isključenu prijavu za WEB sync gumb mjeri na privremenoj utičnici na
// udp_port, zatvori je i ne dira oznaku padova; stanje WEB synca poslužitelj
// ne dobiva. Bez STUN poslužitelja provjera kaže da je isključena.
func TestProvjeraVezeBezPrijave(t *testing.T) {
	dir, w, d := t.TempDir(), noviLazniWeb(), make(dnevnikPovezivosti, 4)
	port := slobodanUDP(t)
	cfg := config.Povezivost{UDPPort: port, Stun: []string{lazniSTUN(t), lazniSTUN(t)}}
	stani := pokreniPovezivost(context.Background(), cfg, dir, w, d.javi)
	defer stani()
	if w.stanje != nil || w.provjeri == nil || w.zadnja() != nil {
		t.Fatalf("poslužitelj: stanje %v, provjera %v", w.stanje != nil, w.provjeri != nil)
	}
	r := w.provjeri(context.Background())
	if r.Greska != "" || !r.Privremena || r.Port != port || r.Mjerenje.Vrsta != "lak" || r.Vrijedi != "" || r.Upozorenje != "" {
		t.Errorf("privremena provjera: %+v", r)
	}
	if z := w.zadnja(); z == nil || !z.Kad.Equal(r.Kad) {
		t.Errorf("zadnja: %+v", z)
	}
	if _, ima := oznakaPadova(t, dir); ima {
		t.Error("privremena provjera upisala oznaku padova")
	}
	c, err := net.ListenPacket("udp4", ":"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("privremena utičnica nije zatvorena: %v", err)
	}
	// zauzet port: mjeri se s nasumičnog, uz upozorenje
	r = w.provjeri(context.Background())
	c.Close()
	if r.Port == port || !strings.Contains(r.Upozorenje, strconv.Itoa(port)) || r.Greska != "" {
		t.Errorf("uz zauzet port: %+v", r)
	}

	w2 := noviLazniWeb()
	stani2 := pokreniPovezivost(context.Background(), config.Povezivost{}, dir, w2, d.javi)
	defer stani2()
	if r := w2.provjeri(context.Background()); !strings.Contains(r.Greska, "stun = []") {
		t.Errorf("bez STUN-a: %+v", r)
	}
}

// Dva pritiska dok mjerenje traje znače jedno mjerenje; zahtjev koji ode ne
// prekida mjerenje, a nakon zaustavljanja provjere nema.
func TestProvjeraVezeJednaPoJedna(t *testing.T) {
	upiti, pusti := make(chan struct{}, 16), make(chan struct{})
	cfg := config.Povezivost{UDPPort: 0, Stun: []string{lazniSTUNSa(t, pusti, upiti)}}
	w := noviLazniWeb()
	stani := pokreniPovezivost(context.Background(), cfg, t.TempDir(), w, make(dnevnikPovezivosti, 4).javi)
	prvi, drugi := make(chan web.ProvjeraVeze, 1), make(chan web.ProvjeraVeze, 1)
	go func() { prvi <- w.provjeri(context.Background()) }()
	<-upiti // prvo mjerenje je poslalo upit i čeka odgovor, koji test drži
	otisao, odustani := context.WithCancel(context.Background())
	odustani()
	if r := w.provjeri(otisao); !strings.Contains(r.Greska, "nije dočekana") {
		t.Errorf("otišao zahtjev: %+v", r)
	}
	go func() { drugi <- w.provjeri(context.Background()) }()
	time.Sleep(50 * time.Millisecond) // drugi pritisak stigne dok prvo mjerenje čeka (rok mu je 2 s)
	close(pusti)
	a, b := <-prvi, <-drugi
	if a.Greska != "" || !b.Kad.Equal(a.Kad) {
		t.Errorf("dva pritiska, dva mjerenja: %+v / %+v", a, b)
	}
	stani()
	if r := w.provjeri(context.Background()); !strings.Contains(r.Greska, "gasi") {
		t.Errorf("nakon zaustavljanja: %+v", r)
	}
}

// Uz WEB sync gumb mjeri na stalnoj utičnici, a ishod ide u histerezu i
// nosi kandidate; utičnica koja više ne radi i prekinuto mjerenje kažu
// grešku, a ne blokiran UDP.
func TestProvjeraVezeNaStalnoj(t *testing.T) {
	dir, w, d := t.TempDir(), noviLazniWeb(), make(dnevnikPovezivosti)
	stani := pokreniPovezivost(context.Background(), prijavaWebSynca(lazniSTUN(t), lazniSTUN(t)), dir, w, d.javi)
	defer stani()
	close(w.slusa)
	d.cekaj(t, "povezivost: UDP radi na :")
	d.cekaj(t, "povezivost: NAT je lak")
	var pri *web.ProvjeraVeze
	for rok := time.Now().Add(5 * time.Second); pri == nil; time.Sleep(10 * time.Millisecond) {
		if pri = w.zadnja(); pri == nil && time.Now().After(rok) {
			t.Fatal("ishod provjere pri pokretanju nije zapamćen")
		}
	}
	r := w.provjeri(context.Background())
	if r.Privremena || r.Port != pri.Port || r.Vrijedi != "lak" || r.Greska != "" || !r.Kad.After(pri.Kad) {
		t.Errorf("na stalnoj: %+v (pri pokretanju %+v)", r, pri)
	}

	p := &povezivost{cfg: prijavaWebSynca(lazniSTUN(t)), javi: d.javi}
	conn, _, err := razmjena.OtvoriUDP(0)
	if err != nil {
		t.Fatal(err)
	}
	u := razmjena.NoviUDP(conn)
	posl := posluziteljiSTUN(context.Background(), p.cfg.Stun)
	prekinut, otkazi := context.WithCancel(context.Background())
	otkazi()
	if r := p.naStalnoj(prekinut, u, posl, nil); r.Greska != "Provjera je prekinuta." {
		t.Errorf("prekinuto na stalnoj: %+v", r)
	}
	if r := p.naPrivremenoj(prekinut, posl, nil); r.Greska != "Provjera je prekinuta." {
		t.Errorf("prekinuto na privremenoj: %+v", r)
	}
	if err := u.Close(); err != nil {
		t.Fatal(err)
	}
	if r := p.naStalnoj(context.Background(), u, posl, nil); !strings.Contains(r.Greska, "više ne radi") {
		t.Errorf("utičnica koja ne radi: %+v", r)
	}
	stari := otvoriUticnicu
	otvoriUticnicu = func(int) (net.PacketConn, error, error) { return nil, nil, errors.New("nema mreže") }
	t.Cleanup(func() { otvoriUticnicu = stari })
	if r := p.naPrivremenoj(context.Background(), posl, nil); !strings.Contains(r.Greska, "nema mreže") {
		t.Errorf("privremena se ne da otvoriti: %+v", r)
	}
}
