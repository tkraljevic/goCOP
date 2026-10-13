package peers

// Razgovor razmjene kad veza ili baza padne usred njega. Drugu stranu glumi
// skripta s ključem pravog člana mreže, na loopbacku: pošalje i pročita
// točno ono što treba, prekine vezu na zadanom mjestu ili zatraži da veza
// čvora padne pri sljedećem pisanju. Gleda se što exchange vrati i što
// ostane u knjizi.

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

var errVezaPala = errors.New("namjerno: veza je pala")

// kvarnaVeza je TCP veza kojoj pisanje padne kad test to zatraži
type kvarnaVeza struct {
	net.Conn
	pada *atomic.Bool
}

func (v kvarnaVeza) Write(b []byte) (int, error) {
	if v.pada.Load() {
		return 0, errVezaPala
	}
	return v.Conn.Write(b)
}

// kvarnaSlusalica predaje veze kojima pisanje padne kad test to zatraži
type kvarnaSlusalica struct {
	net.Listener
	pada *atomic.Bool
}

func (l kvarnaSlusalica) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return kvarnaVeza{Conn: c, pada: l.pada}, nil
}

type ishodRazgovora struct {
	applied, sent int
	theirs        frontierMsg
	err           error
}

// skripta vodi drugu stranu razgovora; pokvari() kaže da sljedeće pisanje
// čvora koji se ispituje padne
type skripta func(t *testing.T, c *razmjena.Conn, pokvari func())

func posaljiPoruku(t *testing.T, c *razmjena.Conn, vrsta string, v any) {
	t.Helper()
	e, err := razmjena.NewEnvelope(vrsta, v)
	if err != nil {
		t.Error(err)
		return
	}
	_ = c.Send(e)
}

func primiPoruku(t *testing.T, c *razmjena.Conn, vrsta string) razmjena.Envelope {
	t.Helper()
	e, err := c.Receive()
	if err != nil {
		t.Errorf("skripta je čekala %s: %v", vrsta, err)
	} else if e.Kind != vrsta {
		t.Errorf("skripta je čekala %s, stiglo %s", vrsta, e.Kind)
	}
	return e
}

// nazoviSkriptu: čvor s nazove drugoga (initiator), a razgovor s druge
// strane vodi skripta. Uz odmah=true veza čvora pada od prvog pisanja.
func nazoviSkriptu(t *testing.T, s, drugi *Service, odmah bool, sk skripta) ishodRazgovora {
	t.Helper()
	ctx, otkazi := context.WithTimeout(ctxRub, 30*time.Second)
	defer otkazi()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	pada := new(atomic.Bool)
	gotovo := make(chan struct{})
	go func() {
		_ = razmjena.ServeExchangeOn(ctx, drugi.node.key, Protocol, drugi.vjerodajnice, ln, drugi.trusted, func(c *razmjena.Conn) {
			defer close(gotovo)
			defer c.Close()
			sk(t, c, func() { pada.Store(true) })
		})
	}()
	raw, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	conn, err := razmjena.DialExchangeOn(ctx, kvarnaVeza{Conn: raw, pada: pada}, s.node.key, Protocol, s.vjerodajnice, ln.Addr().String(), drugi.javni())
	if err != nil {
		t.Fatal(err)
	}
	pada.Store(odmah)
	var o ishodRazgovora
	o.applied, o.sent, o.theirs, o.err = s.exchange(ctx, conn, true)
	conn.Close()
	select {
	case <-gotovo:
	case <-ctx.Done():
		t.Fatal("skripta nije završila")
	}
	return o
}

// primiSkriptu: drugi (skripta) nazove čvor s, koji razgovor prima
func primiSkriptu(t *testing.T, s, drugi *Service, sk skripta) ishodRazgovora {
	t.Helper()
	ctx, otkazi := context.WithTimeout(ctxRub, 30*time.Second)
	defer otkazi()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	pada := new(atomic.Bool)
	ishod := make(chan ishodRazgovora, 1)
	go func() {
		_ = razmjena.ServeExchangeOn(ctx, s.node.key, Protocol, s.vjerodajnice, kvarnaSlusalica{Listener: ln, pada: pada}, s.trusted, func(c *razmjena.Conn) {
			defer c.Close()
			var o ishodRazgovora
			o.applied, o.sent, o.theirs, o.err = s.exchange(ctx, c, false)
			ishod <- o
		})
	}()
	conn, err := razmjena.DialExchange(ctx, drugi.node.key, Protocol, drugi.vjerodajnice, ln.Addr().String(), s.javni())
	if err != nil {
		t.Fatal(err)
	}
	sk(t, conn, func() { pada.Store(true) })
	conn.Close()
	select {
	case o := <-ishod:
		return o
	case <-ctx.Done():
		t.Fatal("čvor nije završio razgovor")
	}
	return ishodRazgovora{}
}

var praznaGranica = frontierMsg{Frontier: map[string]string{}}

// koraciRazgovora su poruke razgovora redom; i pozivatelj i pozvani šalju
// svaku, pozivatelj prvi
var koraciRazgovora = []struct {
	ime, vrsta string
	v          any
}{{"granica", kindFrontier, praznaGranica}, {"delta", kindDelta, deltaMsg{}}, {"želje", kindZelje, zeljeMsg{}}, {"sadržaj", kindSadrzaj, sadrzajMsg{}}}

// pozvaniDo glumi pozvanu stranu pravilnog razgovora do koraka do. Bez kvara
// nakon primitka te poruke zatvori vezu; s kvarom odgovori na nju, a vezu
// čvora pokvari prije odgovora, pa čvoru padne sljedeće pisanje.
func pozvaniDo(do string, kvar bool) skripta {
	return func(t *testing.T, c *razmjena.Conn, pokvari func()) {
		for _, k := range koraciRazgovora {
			primiPoruku(t, c, k.vrsta)
			if k.ime != do {
				posaljiPoruku(t, c, k.vrsta, k.v)
				continue
			}
			if kvar {
				pokvari()
				posaljiPoruku(t, c, k.vrsta, k.v)
				_, _ = c.Receive()
			}
			return
		}
	}
}

// Čvor koji zove: na svakom mjestu na kojem veza padne ili druga strana
// nestane razgovor završi greškom i ne čeka dalje.
func TestRazgovorPozivateljaKadVezaPadne(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	for _, p := range []struct {
		ime    string
		odmah  bool
		sk     skripta
		greska error
	}{
		{"pisanje granice", true, func(t *testing.T, c *razmjena.Conn, _ func()) { _, _ = c.Receive() }, errVezaPala},
		{"nestane nakon granice", false, pozvaniDo("granica", false), nil},
		{"pisanje delte", false, pozvaniDo("granica", true), errVezaPala},
		{"nestane nakon delte", false, pozvaniDo("delta", false), nil},
		{"pisanje želja", false, pozvaniDo("delta", true), errVezaPala},
		{"nestane nakon želja", false, pozvaniDo("želje", false), nil},
		{"pisanje sadržaja", false, pozvaniDo("želje", true), errVezaPala},
		{"nestane nakon sadržaja", false, pozvaniDo("sadržaj", false), nil},
		{"pisanje kraja", false, pozvaniDo("sadržaj", true), errVezaPala},
	} {
		o := nazoviSkriptu(t, b, a, p.odmah, p.sk)
		if o.err == nil || (p.greska != nil && !errors.Is(o.err, p.greska)) {
			t.Errorf("%s: %v, očekivano %v", p.ime, o.err, p.greska)
		}
	}
}

// Pozvani čvor: isto, na svakom mjestu na kojem veza padne ili pozivatelj
// nestane, i kad pozivatelj umjesto želja pošalje nešto drugo.
func TestRazgovorPozvanogKadVezaPadne(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	// pozivatelj šalje redom granicu, deltu, želje i sadržaj, a pozvani
	// odgovara na svaku; skripta prestane ili pokvari vezu pozvanog prije
	// zadanog koraka
	pozivatelj := func(do string, pokvari bool) skripta {
		return func(t *testing.T, c *razmjena.Conn, kvar func()) {
			for _, k := range koraciRazgovora {
				if k.ime == do {
					if !pokvari {
						return
					}
					kvar()
				}
				posaljiPoruku(t, c, k.vrsta, k.v)
				if k.ime == do {
					_, _ = c.Receive()
					return
				}
				if k.ime != "sadržaj" {
					primiPoruku(t, c, k.vrsta)
				}
			}
		}
	}
	for _, p := range []struct {
		ime    string
		sk     skripta
		greska error
	}{
		{"nestane prije granice", pozivatelj("granica", false), nil},
		{"pisanje granice", pozivatelj("granica", true), errVezaPala},
		{"nestane prije delte", pozivatelj("delta", false), nil},
		{"pisanje delte", pozivatelj("delta", true), errVezaPala},
		{"nestane prije želja", pozivatelj("želje", false), nil},
		{"pisanje želja", pozivatelj("želje", true), errVezaPala},
		{"nestane prije sadržaja", pozivatelj("sadržaj", false), nil},
		{"pisanje sadržaja", pozivatelj("sadržaj", true), errVezaPala},
	} {
		o := primiSkriptu(t, b, a, p.sk)
		if o.err == nil || (p.greska != nil && !errors.Is(o.err, p.greska)) {
			t.Errorf("%s: %v, očekivano %v", p.ime, o.err, p.greska)
		}
	}

	// umjesto želja stigne sadržaj
	o := primiSkriptu(t, b, a, func(t *testing.T, c *razmjena.Conn, _ func()) {
		posaljiPoruku(t, c, kindFrontier, praznaGranica)
		primiPoruku(t, c, kindFrontier)
		posaljiPoruku(t, c, kindDelta, deltaMsg{})
		primiPoruku(t, c, kindDelta)
		posaljiPoruku(t, c, kindSadrzaj, sadrzajMsg{})
	})
	if o.err == nil || !strings.Contains(o.err.Error(), `očekivana poruka "zelje", stigla "sadrzaj"`) {
		t.Errorf("krivi redoslijed: %v", o.err)
	}
}

// Kad se knjiga ili pretplate čvora ne mogu pročitati, razgovor ne počne; kad
// knjiga nestane usred razgovora (nakon granice), delta se ne šalje.
func TestRazgovorKadBazaPadne(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	sakrij := func(tablica, kamo string) {
		t.Helper()
		if _, err := b.db.Exec(`ALTER TABLE ` + tablica + ` RENAME TO ` + kamo); err != nil {
			t.Error(err)
		}
	}
	sakrij("record_versions", "knjiga_skrivena")
	if o := nazoviSkriptu(t, b, a, false, func(*testing.T, *razmjena.Conn, func()) {}); o.err == nil || !strings.Contains(o.err.Error(), "record_versions") {
		t.Errorf("bez knjige: %v", o.err)
	}
	sakrij("knjiga_skrivena", "record_versions")

	b.SetWantsAll(false)
	sakrij("subscriptions", "pretplate_skrivene")
	if o := nazoviSkriptu(t, b, a, false, func(*testing.T, *razmjena.Conn, func()) {}); o.err == nil || !strings.Contains(o.err.Error(), "subscriptions") {
		t.Errorf("bez pretplata: %v", o.err)
	}
	sakrij("pretplate_skrivene", "subscriptions")
	b.SetWantsAll(true)

	o := nazoviSkriptu(t, b, a, false, func(t *testing.T, c *razmjena.Conn, _ func()) {
		primiPoruku(t, c, kindFrontier)
		sakrij("record_versions", "knjiga_skrivena")
		posaljiPoruku(t, c, kindFrontier, praznaGranica)
		_, _ = c.Receive()
	})
	if o.err == nil || !strings.Contains(o.err.Error(), "record_versions") {
		t.Errorf("knjiga nestala nakon granice: %v", o.err)
	}
	sakrij("knjiga_skrivena", "record_versions")

	// verzija koja se ne da zapisati kao JSON: delta ne prolazi, i to se kaže
	if _, err := b.db.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel)
		VALUES ('0190a000-0000-7000-8000-00000000000f', 'probni', 'r-15', 'laptop-baranja', '', 0, 'nije json', ?, 1, '')`, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if o := nazoviSkriptu(t, b, a, false, func(t *testing.T, c *razmjena.Conn, _ func()) {
		primiPoruku(t, c, kindFrontier)
		posaljiPoruku(t, c, kindFrontier, praznaGranica)
		_, _ = c.Receive()
	}); o.err == nil || !strings.Contains(o.err.Error(), "invalid character") {
		t.Errorf("pokvarena verzija u delti: %v", o.err)
	}
	if o := primiSkriptu(t, b, a, func(t *testing.T, c *razmjena.Conn, _ func()) {
		posaljiPoruku(t, c, kindFrontier, praznaGranica)
		primiPoruku(t, c, kindFrontier)
		posaljiPoruku(t, c, kindDelta, deltaMsg{})
		_, _ = c.Receive()
	}); o.err == nil || !strings.Contains(o.err.Error(), "invalid character") {
		t.Errorf("pokvarena verzija u delti pozvanog: %v", o.err)
	}
}

// Od primljene delte čvor koji zove upiše samo kanale koje prati i ono što
// propušta njegova ograda; razgovor zatim normalno završi.
func TestRazgovorPozivateljaPrimaSamoZeljeno(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	upisiOcitanja(t, a, "ocitanja/16/2026", "r-1", "r-2")
	upisiOcitanja(t, a, "ocitanja/3/2026", "r-3")
	verzije, err := a.rec.InChannels(ctxRub, []string{"ocitanja/16/2026", "ocitanja/3/2026"})
	if err != nil || len(verzije) != 3 {
		t.Fatalf("verzije: %d %v", len(verzije), err)
	}
	b.SetWantsAll(false)
	if _, err := b.AddSubscription(ctxRub, Subscription{Kind: "ocitanja", AreaID: 16}); err != nil {
		t.Fatal(err)
	}
	b.Accept(func(v ledger.Version) bool { return v.EntityID != "r-2" })
	o := nazoviSkriptu(t, b, a, false, func(t *testing.T, c *razmjena.Conn, _ func()) {
		primiPoruku(t, c, kindFrontier)
		posaljiPoruku(t, c, kindFrontier, praznaGranica)
		primiPoruku(t, c, kindDelta)
		posaljiPoruku(t, c, kindDelta, deltaMsg{Versions: verzije})
		primiPoruku(t, c, kindZelje)
		posaljiPoruku(t, c, kindDone, doneMsg{})
		primiPoruku(t, c, kindDone)
	})
	if o.err != nil || o.applied != 1 {
		t.Fatalf("razgovor: primljeno %d, %v", o.applied, o.err)
	}
	if v, err := b.rec.Latest(ctxRub, "probni", "r-1"); err != nil || v.Channel != "ocitanja/16/2026" {
		t.Errorf("željena verzija nije upisana: %v", err)
	}
	for _, id := range []string{"r-2", "r-3"} {
		if _, err := b.rec.Latest(ctxRub, "probni", id); !errors.Is(err, ledger.ErrNoVersion) {
			t.Errorf("%s je upisan: %v", id, err)
		}
	}
}
