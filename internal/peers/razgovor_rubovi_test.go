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
	"sort"
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
}

// upisiRedakKnjige upisuje verziju u knjigu mimo Recorder-a, sa zadanim
// version_id i sadržajem, kao redak koji je u bazi već takav
func upisiRedakKnjige(t *testing.T, s *Service, versionID, kanal, payload string) {
	t.Helper()
	if _, err := s.db.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel)
		VALUES (?, 'probni', ?, ?, '', 0, ?, ?, 1, ?)`, versionID, "r-"+versionID, s.node.ID, payload, time.Now().UTC(), kanal); err != nil {
		t.Fatal(err)
	}
}

// primiDeltu glumi drugu stranu koja zove ili je pozvana s danom granicom i
// pročita deltu čvora; razgovor završi bez sadržaja
func primiDeltu(granica map[string]string, stiglo *deltaMsg) skripta {
	return func(t *testing.T, c *razmjena.Conn, _ func()) {
		primiPoruku(t, c, kindFrontier)
		posaljiPoruku(t, c, kindFrontier, frontierMsg{Frontier: granica})
		e := primiPoruku(t, c, kindDelta)
		if err := e.Decode(stiglo); err != nil {
			t.Errorf("delta se ne da pročitati: %v", err)
		}
		posaljiPoruku(t, c, kindDelta, deltaMsg{})
		primiPoruku(t, c, kindZelje)
		posaljiPoruku(t, c, kindDone, doneMsg{})
		primiPoruku(t, c, kindDone)
	}
}

func idoviVerzija(vv []ledger.Version) string {
	var out []string
	for _, v := range vv {
		out = append(out, v.VersionID)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// Verzija s neispravnim JSON-om u knjizi ne ruši razmjenu: preskoči se,
// ostale verzije idu, u dnevnik ide jednom (entitet i version_id, bez
// sadržaja), a ploča je pokazuje kao upozorenje i ne broji je kao da čeka.
// Granica primatelja prijeđe preko nje čim stigne kasnija verzija istog
// ključa, pa je sljedeća razmjena više ne nudi. Do 0.0.38 jedna takva
// verzija rušila je svaku razmjenu s čvorom koji je nema.
func TestRazgovorPreskaceNeispravnuVerziju(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	dnevnik := brojiDnevnik(t, "nije ispravan JSON")
	const (
		k16v1, k16los, k16v3 = "0190a000-0000-7000-8000-000000000001", "0190a000-0000-7000-8000-000000000002", "0190a000-0000-7000-8000-000000000003"
		k3v1, k3los          = "0190a000-0000-7000-8000-000000000011", "0190a000-0000-7000-8000-000000000012"
	)
	upisiRedakKnjige(t, b, k16v1, "ocitanja/16/2026", `{"cm":120}`)
	upisiRedakKnjige(t, b, k16los, "ocitanja/16/2026", "nije json, Pero Perić 120 cm")
	upisiRedakKnjige(t, b, k16v3, "ocitanja/16/2026", `{"cm":122}`)
	upisiRedakKnjige(t, b, k3v1, "ocitanja/3/2026", `{"cm":80}`)
	upisiRedakKnjige(t, b, k3los, "ocitanja/3/2026", "nije json")
	kljuc16 := ledger.FrontierKey(b.node.ID, "ocitanja/16/2026")
	kljuc3 := ledger.FrontierKey(b.node.ID, "ocitanja/3/2026")
	// druga strana ima sve osim očitanja; "~" je iza svakog version_id
	mine, err := b.rec.Frontier(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	samoOcitanja := map[string]string{}
	for k := range mine {
		if k != kljuc16 && k != kljuc3 {
			samoOcitanja[k] = "~"
		}
	}

	var stiglo deltaMsg
	o := nazoviSkriptu(t, b, a, false, primiDeltu(samoOcitanja, &stiglo))
	if o.err != nil {
		t.Fatalf("razmjena s neispravnom verzijom: %v", o.err)
	}
	if got := idoviVerzija(stiglo.Versions); got != strings.Join([]string{k16v1, k16v3, k3v1}, ",") || stiglo.Skraceno {
		t.Errorf("poslano %s (skraćeno %v)", got, stiglo.Skraceno)
	}
	if o.sent != 3 || o.theirs.Frontier[kljuc16] != k16v3 || o.theirs.Frontier[kljuc3] != k3los {
		t.Errorf("poslano %d, zapamćena granica %v", o.sent, o.theirs.Frontier)
	}
	if n, zadnji := dnevnik.broj(); n != 2 || !strings.Contains(zadnji, "(probni)") || strings.Contains(zadnji, "Perić") || strings.Contains(zadnji, "nije json") {
		t.Errorf("dnevnik: %d redaka, zadnji %q", n, zadnji)
	}

	// zapamćena granica: ploča ne broji neispravnu verziju kao da čeka
	if err := b.SavePeer(ctxRub, Peer{NodeID: a.node.ID, Name: a.node.ID, PublicKey: a.node.PublicKey(), Addresses: []string{"192.168.1.20:4710"}}); err != nil {
		t.Fatal(err)
	}
	b.recordSyncState(ctxRub, a.node.ID, ishodRazmjene(o.applied, o.sent, o.theirs, time.Millisecond, nil))
	st, err := b.Status(ctxRub, false)
	if err != nil || len(st.Peers) != 1 {
		t.Fatalf("stanje: %+v %v", st, err)
	}
	if st.Peers[0].Backlog != 0 {
		t.Errorf("ploča broji %d verzija koje čekaju", st.Peers[0].Backlog)
	}
	upozorenje := "Razmjena drugim čvorovima ne šalje 2 zapisa koji u bazi ovog računala nisu ispravni (probni, verzija " + k16los + "; probni, verzija " + k3los + ")."
	if !strings.Contains(strings.Join(st.Alerts, "\n"), upozorenje) {
		t.Errorf("upozorenje na ploči: %q", st.Alerts)
	}

	// primatelj sad ima k16v3 i k3v1: neispravne se više ne nude, nova
	// verzija drugog ključa ide, a dnevnik se ne ponavlja
	novi := "0190a000-0000-7000-8000-000000000013"
	upisiRedakKnjige(t, b, novi, "ocitanja/3/2026", `{"cm":81}`)
	stiglo = deltaMsg{}
	sGranicom := map[string]string{kljuc16: k16v3, kljuc3: k3v1}
	for k, v := range samoOcitanja {
		sGranicom[k] = v
	}
	if o := primiSkriptu(t, b, a, pozivateljSGranicom(sGranicom, &stiglo)); o.err != nil || o.sent != 1 {
		t.Fatalf("druga razmjena: poslano %d, %v", o.sent, o.err)
	}
	if got := idoviVerzija(stiglo.Versions); got != novi {
		t.Errorf("u drugoj razmjeni poslano %s", got)
	}
	if n, _ := dnevnik.broj(); n != 2 {
		t.Errorf("dnevnik se ponavlja: %d redaka", n)
	}
}

// pozivateljSGranicom glumi stranu koja zove: pošalje granicu, pročita
// deltu čvora i završi razgovor bez sadržaja
func pozivateljSGranicom(granica map[string]string, stiglo *deltaMsg) skripta {
	return func(t *testing.T, c *razmjena.Conn, _ func()) {
		posaljiPoruku(t, c, kindFrontier, frontierMsg{Frontier: granica})
		primiPoruku(t, c, kindFrontier)
		posaljiPoruku(t, c, kindDelta, deltaMsg{})
		e := primiPoruku(t, c, kindDelta)
		if err := e.Decode(stiglo); err != nil {
			t.Errorf("delta se ne da pročitati: %v", err)
		}
		posaljiPoruku(t, c, kindDone, doneMsg{})
		primiPoruku(t, c, kindDone)
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
