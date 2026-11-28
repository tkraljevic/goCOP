package peers

import (
	"context"
	"crypto/ed25519"
	"net"
	"net/netip"
	"slices"
	"testing"
	"time"

	"gocop/internal/mrezatest"
	"gocop/internal/razmjena"
)

// Testovi ne smiju ovisiti o tome ima li računalo na kojem rade Tailscale:
// čvor u testu nema ga dok mu test ne zada adrese (ZadajTailscale)
func init() {
	zadaneTailscaleAdrese = func() []netip.Addr { return nil }
}

// ZadajTailscale daje čvoru u testu izmišljene Tailscale adrese; bez
// adresa čvor nema Tailscale
func (s *Service) ZadajTailscale(adrese ...netip.Addr) {
	s.tailscaleAdrese = func() []netip.Addr { return adrese }
}

// SuziDeltu privremeno smanjuje ogradu veličine delte, za test skraćenih
// razgovora; vraća funkciju koja je vraća
func SuziDeltu(n int) func() {
	prije := najviseBajtovaDelte
	najviseBajtovaDelte = n
	return func() { najviseBajtovaDelte = prije }
}

// ZadajMogucnosti daje čvoru u testu oznake koje javlja u granici, umjesto
// onih koje javlja program; tako test glumi drugi program
func (s *Service) ZadajMogucnosti(oznake ...string) {
	s.mogucnosti = func() []string { return slices.Clone(oznake) }
}

// Pomoćnici za testove karte bez interneta u peers_test (N04). Karta
// razmjenu koristi samo kroz njezino javno sučelje, pa njezini testovi koji
// trebaju dva čvora idu u peers_test, a čvorove, mrežu i razgovor dobiju
// odavde.

// Čvorovi i mreža (petlja_test.go, rubovi_test.go, cop_rubovi_test.go)
var (
	ClanoviMreze      = clanoviMreze
	Clanovi           = clanovi
	IzdavacIPrimatelj = izdavacIPrimatelj
	RubniCvor         = rubniCvor
	RubnaMreza        = rubnaMreza
	PrimiIzravno      = primiIzravno
	SpremisteNaDisku  = spremisteNaDisku
	Izjednaci         = izjednaci
	BrojVerzija       = brojVerzija
	VezaIzMreze       = vezaIzMreze
)

// NajviseSadrzajaPoRazmjeni je ograda broja stavki sadržaja po razgovoru
const NajviseSadrzajaPoRazmjeni = najviseSadrzajaPoRazmjeni

// SmanjiOgrade smanji ograde dijelova karte po putu koje dobije svaki novi
// prijenos karte, za trajanje testa
func SmanjiOgrade(t *testing.T, lan, internet, tunel int64) {
	t.Helper()
	prije := zadaneOgrade
	zadaneOgrade = ogradeKarte{lan: lan, internet: internet, tunel: tunel}
	t.Cleanup(func() { zadaneOgrade = prije })
}

// ZadajNajviseBajtovaPoRazmjeni smanji ogradu bajtova sadržaja po
// razgovoru za trajanje testa
func ZadajNajviseBajtovaPoRazmjeni(t *testing.T, n int) {
	t.Helper()
	staro := najviseBajtovaPoRazmjeni
	najviseBajtovaPoRazmjeni = n
	t.Cleanup(func() { najviseBajtovaPoRazmjeni = staro })
}

// ZadajRokCekanjaUpisa skrati čekanje brave upisa za trajanje testa
func ZadajRokCekanjaUpisa(t *testing.T, rok time.Duration) {
	t.Helper()
	staro := rokCekanjaUpisa
	rokCekanjaUpisa = rok
	t.Cleanup(func() { rokCekanjaUpisa = staro })
}

// ZadajMrezeSucelja podmetne mreže sučelja ovog računala za trajanje testa
func ZadajMrezeSucelja(t *testing.T, mreze func() [][]*net.IPNet) {
	t.Helper()
	staro := mrezeSucelja
	mrezeSucelja = mreze
	t.Cleanup(func() { mrezeSucelja = staro })
}

// UzmiBravu i PustiBravu: brava upisa baze, kao kad upisuje drugi razgovor
func (s *Service) UzmiBravu(ctx context.Context) error { return s.uzmiBravu(ctx) }
func (s *Service) PustiBravu()                         { s.pustiBravu() }

// RazgovoriZaRedom je krug razgovora s istim čvorom (SyncWith bez bilješke)
func (s *Service) RazgovoriZaRedom(ctx context.Context, id string) (applied, sent, krugova int, err error) {
	return s.razgovoriZaRedom(ctx, id)
}

// SyncStates je ono što čvor zna o drugim čvorovima (peer_sync)
func (s *Service) SyncStates(ctx context.Context) (map[string]SyncState, error) {
	return s.syncStates(ctx)
}

// RazgovorPutem: pozivatelj s IP adrese od zove pozvanoga na adresi kamo, na
// mreži u memoriji, a obje strane vezi daju put. Vraća greške obiju strana
// i je li pozivatelj zapamtio da dodatni korak još teče; greška razgovora
// ruši test.
func RazgovorPutem(t *testing.T, put razmjena.Put, pozivatelj, pozvani *Service, od, kamo string) (tece bool) {
	t.Helper()
	ctx, otkazi := context.WithTimeout(ctxRub, 60*time.Second)
	defer otkazi()
	m := mrezatest.NovaMreza()
	ln, err := m.Slusaj(kamo)
	if err != nil {
		t.Fatal(err)
	}
	vrata := func(pub ed25519.PublicKey, _ []byte) bool { return pub.Equal(pozivatelj.javni()) }
	ishod := make(chan ishodRazgovora, 1)
	primi := func(c *razmjena.Conn) {
		defer c.Close()
		c.Put = put
		var o ishodRazgovora
		o.applied, o.sent, o.theirs, o.err = pozvani.exchange(ctx, c, false)
		pozvani.korakTece.Delete(c)
		ishod <- o
	}
	go func() {
		_ = razmjena.ServeExchangeOn(ctx, pozvani.node.key, Protocol, pozvani.vjerodajnice, ln, vrata, primi) // staje s ctx
	}()
	raw, err := m.DialContextOd(od)(ctx, "tcp", kamo)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := razmjena.DialExchangeOn(ctx, raw, pozivatelj.node.key, Protocol, pozivatelj.vjerodajnice, kamo, pozvani.javni())
	if err != nil {
		t.Fatal(err)
	}
	conn.Put = put
	var p, z ishodRazgovora
	p.applied, p.sent, p.theirs, p.err = pozivatelj.exchange(ctx, conn, true)
	_, tece = pozivatelj.korakTece.LoadAndDelete(conn)
	conn.Close()
	select {
	case z = <-ishod:
	case <-ctx.Done():
		t.Fatal("pozvani nije završio razgovor")
	}
	if p.err != nil || z.err != nil {
		t.Fatalf("razgovor %s → %s: %v / %v", pozivatelj.node.ID, pozvani.node.ID, p.err, z.err)
	}
	return tece
}

// RazgovorKrozTunel: pozivatelj zove pozvanoga kroz tunel na mreži u
// memoriji s jednosmjernim kašnjenjem (uz međuspremnik od 64 KiB po smjeru
// to je i ograda brzine, oko 320 KiB/s uz 100 ms, 2,6 Mbit/s); vraća
// greške razgovora obiju strana
func RazgovorKrozTunel(t *testing.T, kasnjenje time.Duration, pozivatelj, pozvani *Service) (p, z error) {
	t.Helper()
	mreza := func(t *testing.T, ctx context.Context, pozivatelj, pozvani *Service, primi func(*razmjena.Conn)) (*razmjena.Conn, <-chan struct{}, error) {
		m := mrezatest.NovaMreza()
		m.Kasnjenje = kasnjenje
		const adresa = "198.51.100.2:443"
		ln, err := m.Slusaj(adresa)
		if err != nil {
			t.Fatal(err)
		}
		slusalica := make(chan struct{})
		krozTunel := func(c *razmjena.Conn) {
			c.Put = razmjena.PutKrozTunel
			primi(c)
		}
		go func() {
			defer close(slusalica)
			_ = razmjena.ServeExchangeOn(ctx, pozvani.node.key, Protocol, pozvani.vjerodajnice, ln, vrataZa(pozivatelj), krozTunel) // staje s ctx
		}()
		raw, err := m.DialContextOd("203.0.113.1")(ctx, "tcp", adresa)
		if err != nil {
			return nil, slusalica, err
		}
		c, err := razmjena.DialExchangeOn(ctx, raw, pozivatelj.node.key, Protocol, pozivatelj.vjerodajnice, adresa, pozvani.javni())
		if c != nil {
			c.Put = razmjena.PutKrozTunel
		}
		return c, slusalica, err
	}
	ip, iz := razgovarajNaMrezi(t, mreza, pozivatelj, pozvani)
	return ip.err, iz.err
}

// RazgovorKoraka je razgovor čvora s drugom stranom bez mreže, za test
// dodatnog koraka (razgovorZa u karta_razgovor_test.go)
type RazgovorKoraka struct{ r *razgovorSCvorom }

// RazgovorZa je razgovor čvora s drugom stranom koja javlja oznake
// njihove, na vezi zadanog puta, bez mreže
func RazgovorZa(s *Service, put razmjena.Put, njihove ...string) *RazgovorKoraka {
	return &RazgovorKoraka{razgovorZa(s, put, njihove...)}
}

// ZapocniKorak počne dodatni korak u razgovoru; false kad u njemu ne radi
func (r *RazgovorKoraka) ZapocniKorak(ctx context.Context) bool {
	r.r.korak = r.r.zapocniKorak(ctx)
	return r.r.korak != nil
}

func (r *RazgovorKoraka) MojeZelje(ctx context.Context) []string { return r.r.mojeZelje(ctx) }
func (r *RazgovorKoraka) ZavrsiKorak()                           { r.r.zavrsiKorak() }
func (r *RazgovorKoraka) PredajKoraku(stavke []StavkaSadrzaja)   { r.r.predajKoraku(stavke) }

func (r *RazgovorKoraka) SadrzajZaDrugu(ctx context.Context, otisci []string) []StavkaSadrzaja {
	return r.r.sadrzajZaDrugu(ctx, otisci)
}

// SadrzajIKraj vodi ostatak razgovora vezom c, kao pozivatelj
func (r *RazgovorKoraka) SadrzajIKraj(ctx context.Context, c *razmjena.Conn) error {
	r.r.c, r.r.initiator = c, true
	return r.r.sadrzajIKraj(ctx)
}

// RazdvojiPrimljeno odvoji stavke vrste koraka od ostalog sadržaja
var RazdvojiPrimljeno = razdvojiPrimljeno

// Okvir tijeka razgovora (razgovor_tijek_test.go): druga strana je skripta
// poteza, a zapis razgovora se uspoređuje redak po redak
type (
	Potez          = potez
	FrontierMsg    = frontierMsg
	DeltaMsg       = deltaMsg
	DoneMsg        = doneMsg
	ZeljeMsg       = zeljeMsg
	SadrzajMsg     = sadrzajMsg
	StavkaSadrzaja = stavkaSadrzaja
)

const (
	KindFrontier = kindFrontier
	KindDelta    = kindDelta
	KindDone     = kindDone
	KindZelje    = kindZelje
	KindSadrzaj  = kindSadrzaj
	TijekV1      = tijekV1
	KrajVeze     = krajVeze
	ProgramCvora = programCvora
	NaseOznake   = naseOznake
	Obicno       = obicno
	OtisakTudji  = otisakTudji
	PdfTudji     = pdfTudji
)

var (
	Cita           = cita
	Salje          = salje
	OdCvora        = odCvora
	KrajCvora      = krajCvora
	NakonDelte     = nakonDelte
	CistiPar       = cistiPar
	SSadrzajem     = sSadrzajem
	StanjeSadrzaja = stanjeSadrzaja
	DeltaV1        = deltaV1
	ZeljePrazne    = zeljePrazne
	SadrzajPrazan  = sadrzajPrazan
	ZeljeCvora     = zeljeCvora
	GranicaDrugog  = granicaDrugog
	DolaznaD1      = dolaznaD1
)

// SlucajTijeka je jedan razgovor iz niza razgovora istog para (slucaj)
type SlucajTijeka struct {
	Ime      string
	Pripremi func()
	Potezi   []Potez
	Nakon    func() []string
	Ishod    []string
}

// ProvjeriSlucajeve vodi razgovore jedan za drugim s istim parom
// (provjeriSlucajeve)
func ProvjeriSlucajeve(t *testing.T, a, b *Service, pozivatelj bool, slucajevi []SlucajTijeka) {
	t.Helper()
	var out []slucaj
	for _, sl := range slucajevi {
		out = append(out, slucaj{ime: sl.Ime, pripremi: sl.Pripremi, potezi: sl.Potezi, nakon: sl.Nakon, ishod: sl.Ishod})
	}
	provjeriSlucajeve(t, a, b, pozivatelj, out)
}

// ProcitajCop čita paket kanala iz bajtova; vraća samo grešku
func ProcitajCop(b []byte) error {
	_, _, _, err := procitajCop(b)
	return err
}

// Karta bez interneta iznutra. Ovo nije javno sučelje karte, a testovi u
// peers_test ga trebaju: kad karta izađe u svoj paket, ovo treba drugo
// rješenje.

// NajviseRazgovoraZaKartu je koliko dodatnih razgovora zbog karte smije
// doći u jednom krugu
const NajviseRazgovoraZaKartu = najviseRazgovoraZaKartu

// PrijenosKarte je prijenos karte koji je čvoru dao PostaviKarte
func (s *Service) PrijenosKarte() *PrijenosKarte { return s.karte.Load() }

// ZnanjeKarte je ono što pločica ovog računala pokazuje o njegovoj karti;
// nil bez PostaviKarte
func (s *Service) ZnanjeKarte() *razmjena.ZnanjeKarte { return s.znanjeKarte() }

// SmiriKartu pričeka da prijenos pročita stanje s diska i završi slaganje
// (smiri)
func SmiriKartu(p *PrijenosKarte) { smiri(p.d.Load()) }

// CekajKartu pričeka posao prijenosa u pozadini (čitanje stanja, slaganje)
func CekajKartu(p *PrijenosKarte) { p.d.Load().pozadina.Wait() }

// PustiDijelove oslobodi dijelove koje su razgovori zatražili
func PustiDijelove(p *PrijenosKarte, otisci []string) { p.d.Load().pusti(otisci) }

// ZauzetiDijelovi su dijelovi koje je neki razgovor zatražio, a nije ih
// oslobodio
func ZauzetiDijelovi(p *PrijenosKarte) int {
	d := p.d.Load()
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.zauzeto)
}

// TijekKarte je tijek karte koji je razgovor počeo; nil kad karta u
// razgovoru ne radi
func (r *RazgovorKoraka) TijekKarte() *TijekKarte {
	tijek, _ := r.r.korak.(*tijekKarte)
	return tijek
}

// TijekKarte su dijelovi karte u jednom razgovoru (tijekKarte)
type TijekKarte = tijekKarte

func (t *tijekKarte) MojiDijelovi() []string { return t.mojiDijelovi }
func (t *tijekKarte) Poslano() int           { return t.poslano }
func (t *tijekKarte) Primljeno() int         { return t.primljeno }

// TraziDijelove: razgovor je zatražio dijelove koji ovdje nedostaju, do
// budžeta bajtova i najviše stavki
func (t *tijekKarte) TraziDijelove(budzet int64, najvise int) {
	t.mojiDijelovi = t.d.zeljeni(budzet, najvise)
}
