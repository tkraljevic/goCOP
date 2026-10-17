package peers

// Tijek razgovora razmjene, poruku po poruku, za obje strane. Drugu stranu
// vodi skripta s ključem pravog člana mreže, na loopbacku, kao u
// razgovor_rubovi_test.go, i bilježi svaku omotnicu koju čvor pošalje,
// točno kako je stigla žicom (vrsta, deviceId, razlog i payload), i redom
// kojim je stigla. Na kraju se bilježi što je exchange vratio, je li
// razgovor zapamćen kao skraćen i što je u knjizi. Testovi su napisani
// 15. 10. 2026., prije podjele razgovora na korake (G1 u
// docs/plan-web-sync.md): podjela ne smije promijeniti nijedan redak zapisa.
// G3 (isti plan, §3) neke je retke namjerno promijenio, i uz svaki to piše.
// G2 (isti plan, §3) promijenio je granicu čvora: nosi i njegove mogućnosti.
// Granica druge strane ostala je ista, jer skripta glumi čvor koji ih ne
// javlja (0.0.38), pa ostatak razgovora ide bajt po bajt kao prije.
// G4 (isti plan, §3) dodao je čvoru oznaku granica-odbijenih/1, pa se opet
// promijenila samo granica čvora: poruka granice odbijenih ide samo kad
// oznaku ima i druga strana, a skripta je nema. Razgovor sa stranom koja je
// ima je u granica_odbijenih_tijek_test.go.
//
// Zapis je isti od pokretanja do pokretanja: verzije imaju zadani
// version_id i vrijeme, verzije članstava iz postave izbrisane su iz knjige,
// delta čvora ima najviše jedan ključ (ledger.Recorder.Delta ključeve uzima
// redom mape), a greške mreže bilježe se kao vrsta (kraj veze, veza pala),
// jer zatvorena veza nekad javi EOF, a nekad RST. Ostale greške bilježe se
// tekstom, jer tekst ide u bilješku o razmjeni i na ploču.

import (
	"cmp"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
	"gocop/internal/sadrzaj"
)

var stalnoVrijeme = time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)

// Verzije u razgovorima: v su verzije čvora koji se ispituje
// (laptop-baranja), d verzije druge strane (cop-osijek)
const (
	tijekV1 = "0190a000-0000-7000-8000-000000000001" // ocitanja/16/2026
	tijekV2 = "0190a000-0000-7000-8000-000000000002" // isti ključ, kasnija
	tijekV3 = "0190a000-0000-7000-8000-000000000003" // ocitanja/3/2026
	tijekD1 = "0190a000-0000-7000-8000-000000000101" // ocitanja/16/2026
	tijekD2 = "0190a000-0000-7000-8000-000000000102" // ocitanja/16/2026, ograda je odbija
	tijekD3 = "0190a000-0000-7000-8000-000000000103" // ocitanja/3/2026
	tijekD4 = "0190a000-0000-7000-8000-000000000104" // ocitanja/16/2026, bez identifikatora zapisa
)

var imenaVerzija = map[string]string{tijekV1: "v1", tijekV2: "v2", tijekV3: "v3", tijekD1: "d1", tijekD2: "d2", tijekD3: "d3", tijekD4: "d4"}

// verzije čvora kako idu žicom
const (
	jsonV1 = `{"version_id":"0190a000-0000-7000-8000-000000000001","entity":"probni","entity_id":"r-1","node_id":"laptop-baranja","supersedes":"","archived":false,"payload":{"cm":120},"created_at":"2026-10-15T08:00:00Z","schema_version":1,"channel":"ocitanja/16/2026"}`
	jsonV3 = `{"version_id":"0190a000-0000-7000-8000-000000000003","entity":"probni","entity_id":"r-3","node_id":"laptop-baranja","supersedes":"","archived":false,"payload":{"cm":80},"created_at":"2026-10-15T08:00:00Z","schema_version":1,"channel":"ocitanja/3/2026"}`
)

func stalnaVerzija(id, cvor, kanal, entitetID, payload string) ledger.Version {
	return ledger.Version{VersionID: id, Entity: "probni", EntityID: entitetID, NodeID: cvor,
		Payload: json.RawMessage(payload), CreatedAt: stalnoVrijeme, SchemaVersion: 1, Channel: kanal}
}

var (
	vlastitaV1 = stalnaVerzija(tijekV1, "laptop-baranja", "ocitanja/16/2026", "r-1", `{"cm":120}`)
	vlastitaV2 = stalnaVerzija(tijekV2, "laptop-baranja", "ocitanja/16/2026", "r-2", `{"cm":121}`)
	vlastitaV3 = stalnaVerzija(tijekV3, "laptop-baranja", "ocitanja/3/2026", "r-3", `{"cm":80}`)
	dolaznaD1  = stalnaVerzija(tijekD1, "cop-osijek", "ocitanja/16/2026", "r-11", `{"cm":130}`)
	dolaznaD2  = stalnaVerzija(tijekD2, "cop-osijek", "ocitanja/16/2026", "r-12", `{"cm":131}`)
	dolaznaD3  = stalnaVerzija(tijekD3, "cop-osijek", "ocitanja/3/2026", "r-13", `{"cm":81}`)
)

// odCvora je omotnica koju čvor laptop-baranja šalje u razgovoru: vrsta,
// deviceId i payload, tim redom i bez razmaka
func odCvora(vrsta, payload string) string {
	return `{"kind":"` + vrsta + `","deviceId":"laptop-baranja","payload":` + payload + `}`
}

func krajCvora(applied int) string { return odCvora(kindDone, fmt.Sprintf(`{"applied":%d}`, applied)) }

const programCvora = `"program":"0.0.39-test","gradnja":"0.0.39-test (abc1234)"`

// naseOznake su mogućnosti čvora kako idu žicom. namjerno promijenjeno: G4
// (docs/plan-web-sync.md §3); do G4 samo ["razmjena/1"]
const naseOznake = `["granica-odbijenih/1","razmjena/1"]`

// poruke čvora koje se ponavljaju
var (
	// namjerno promijenjeno: G2 (docs/plan-web-sync.md §3); granica čvora nosi
	// i njegove mogućnosti (katalog je u testovima paketa prazan, a ništa ne
	// čeka nadogradnju). namjerno promijenjeno: G4 (docs/plan-web-sync.md §3);
	// uz razmjena/1 i granica-odbijenih/1
	granicaPrazna = odCvora(kindFrontier, `{"frontier":{},"wants":{"all":true},`+programCvora+`,"mogucnosti":`+naseOznake+`}`)
	// namjerno promijenjeno: G2 (docs/plan-web-sync.md §3); namjerno
	// promijenjeno: G4 (docs/plan-web-sync.md §3)
	granicaV1 = odCvora(kindFrontier, `{"frontier":{"laptop-baranja|ocitanja/16/2026":"`+tijekV1+`"},"wants":{"all":true},`+programCvora+`,"mogucnosti":`+naseOznake+`}`)
	// namjerno promijenjeno: G2 (docs/plan-web-sync.md §3); namjerno
	// promijenjeno: G4 (docs/plan-web-sync.md §3)
	granicaV2 = odCvora(kindFrontier, `{"frontier":{"laptop-baranja|ocitanja/16/2026":"`+tijekV2+`"},"wants":{"all":true},`+programCvora+`,"mogucnosti":`+naseOznake+`}`)

	deltaPrazna   = odCvora(kindDelta, `{"versions":null}`)
	deltaV1       = odCvora(kindDelta, `{"versions":[`+jsonV1+`]}`)
	zeljePrazne   = odCvora(kindZelje, `{"otisci":null}`)
	sadrzajPrazan = odCvora(kindSadrzaj, `{"stavke":null}`)
)

// krajVeze je ono što druga strana pročita kad čvor više ništa ne šalje i
// vezu zatvori
const krajVeze = "(kraj veze)"

// granicaDrugog je granica novijeg čvora koji još nema ništa
var granicaDrugog = frontierMsg{Frontier: map[string]string{}, Wants: &Wants{All: true}, Program: "0.0.38-alfa"}

// Retci ishoda koji se ponavljaju: granica druge strane kako je exchange
// vrati, i redak bez Tailscalea i bez skraćene delte
const (
	granicaNula              = `granica {"frontier":null}`
	granicaDrugogNepomaknuta = `granica {"frontier":{},"wants":{"all":true},"program":"0.0.38-alfa"}`
	granicaDrugogSV1         = `granica {"frontier":{"laptop-baranja|ocitanja/16/2026":"` + tijekV1 + `"},"wants":{"all":true},"program":"0.0.38-alfa"}`
	obicno                   = "tailscale=[]string(nil) skraceno=ne"
)

// snimka je zapis razgovora: retke piše skripta druge strane, a ishod test
// kad razgovor završi
type snimka struct {
	mu    sync.Mutex
	retci []string
}

func (z *snimka) dodaj(redak string) {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.retci = append(z.retci, redak)
}

func (z *snimka) sve() []string {
	z.mu.Lock()
	defer z.mu.Unlock()
	return slices.Clone(z.retci)
}

// strana je druga strana razgovora dok skripta teče
type strana struct {
	t       *testing.T
	c       *razmjena.Conn
	z       *snimka
	id      string
	pokvari func() // sljedeće pisanje čvora padne
	prekini func() // otkaže kontekst razgovora čvora
}

// potez je jedan korak druge strane i redak koji taj korak mora zapisati
type potez struct {
	radi    func(d *strana)
	ocekuje string
}

// cita: druga strana pročita jednu poruku čvora; očekuje se točno ta
// omotnica ili krajVeze
func cita(omotnica string) potez {
	return potez{ocekuje: "čvor  " + omotnica, radi: func(d *strana) {
		e, err := d.c.Receive()
		if err != nil {
			d.z.dodaj("čvor  " + krajVeze)
			return
		}
		// Encoder i Marshal pišu omotnicu isto, a RawMessage čuva
		// primljeni payload, pa su ovo bajtovi retka sa žice bez \n
		b, err := json.Marshal(e)
		if err != nil {
			d.t.Errorf("omotnica čvora: %v", err)
		}
		d.z.dodaj("čvor  " + string(b))
	}}
}

// salje: druga strana pošalje poruku kroz razmjena.NewEnvelope
func salje(vrsta string, v any) potez {
	return potez{ocekuje: "drugi " + vrsta, radi: func(d *strana) {
		e, err := razmjena.NewEnvelope(vrsta, v)
		if err != nil {
			d.t.Error(err)
			return
		}
		e.DeviceID = d.id
		d.z.dodaj("drugi " + vrsta)
		_ = d.c.Send(e)
	}}
}

// saljeSirovo: druga strana pošalje omotnicu kakvu je složio test
// (pokvaren payload, odbijenica)
func saljeSirovo(oznaka string, e razmjena.Envelope) potez {
	return potez{ocekuje: "drugi " + oznaka, radi: func(d *strana) {
		d.z.dodaj("drugi " + oznaka)
		_ = d.c.Send(e)
	}}
}

// odbija: druga strana odbije razgovor razlogom bez koda, kao čvor 0.0.38
func odbija(razlog string) potez {
	return saljeSirovo("odbijeno: "+razlog, razmjena.Envelope{Kind: razmjena.VrstaOdbijeno, Reason: razlog})
}

// odbijaKodom: druga strana odbije razgovor kodom i razlogom, kao
// razmjena.Odbij
func odbijaKodom(kod razmjena.KodRazloga, razlog string) potez {
	return saljeSirovo(fmt.Sprintf("odbijeno 0x%04x: %s", uint64(kod), razlog), razmjena.Odbijenica(kod, razlog))
}

// kvar: sljedeće pisanje čvora padne
func kvar() potez {
	return potez{ocekuje: "kvar veze čvora", radi: func(d *strana) {
		d.z.dodaj("kvar veze čvora")
		d.pokvari()
	}}
}

// usput: druga strana između poruka napravi nešto drugo (npr. prekine
// kontekst čvora)
func usput(oznaka string, f func(d *strana)) potez {
	return potez{ocekuje: oznaka, radi: func(d *strana) {
		d.z.dodaj(oznaka)
		f(d)
	}}
}

// do je prvih n poteza pravilnog razgovora i zatim zadani potezi
func do(tok []potez, n int, dalje ...potez) []potez {
	return append(slices.Clone(tok[:n]), dalje...)
}

// prerusi su adrese pod kojima veza čvora izgleda kao veza preko Tailscalea
type prerusi struct{ lokalna, udaljena net.Addr }

// razgovor je jedan razgovor čvora koji se ispituje s drugom stranom
type razgovor struct {
	pozivatelj bool     // čvor zove; inače prima
	odmah      bool     // pisanje čvora pada od prve poruke (samo kad zove)
	prerusi    *prerusi // nil: obična veza na loopbacku
	potezi     []potez
	nakon      func() []string // nuspojave koje razgovor još bilježi
}

// provjeriTijek odvede razgovor i usporedi cijeli zapis: retke poteza,
// zatim zadane retke ishoda
func provjeriTijek(t *testing.T, s, drugi *Service, ime string, r razgovor, ishod ...string) {
	t.Helper()
	dobiveno := snimiTijek(t, s, drugi, r)
	var ocekivano []string
	for _, p := range r.potezi {
		ocekivano = append(ocekivano, p.ocekuje)
	}
	ocekivano = append(ocekivano, ishod...)
	if !slices.Equal(dobiveno, ocekivano) {
		t.Errorf("%s: zapis razgovora se razlikuje (- očekivano, + dobiveno)\n%s", ime, razlikaZapisa(ocekivano, dobiveno))
	}
}

// razlikaZapisa ispiše cijeli zapis, redak po redak, s oznakom gdje se
// razlikuje
func razlikaZapisa(ocekivano, dobiveno []string) string {
	var b strings.Builder
	for i := range max(len(ocekivano), len(dobiveno)) {
		if i < len(ocekivano) && i < len(dobiveno) && ocekivano[i] == dobiveno[i] {
			fmt.Fprintf(&b, "  %s\n", ocekivano[i])
			continue
		}
		if i < len(ocekivano) {
			fmt.Fprintf(&b, "- %s\n", ocekivano[i])
		}
		if i < len(dobiveno) {
			fmt.Fprintf(&b, "+ %s\n", dobiveno[i])
		}
	}
	return b.String()
}

// snimiTijek: čvor s razgovara s drugim, čiju stranu vode potezi, i vrati
// zapis razgovora s ishodom
func snimiTijek(t *testing.T, s, drugi *Service, r razgovor) []string {
	t.Helper()
	ctx, otkazi := context.WithTimeout(ctxRub, 30*time.Second)
	defer otkazi()
	ctxCvora, prekini := context.WithCancel(ctx)
	defer prekini()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	pada := new(atomic.Bool)
	z := &snimka{}
	vodi := func(c *razmjena.Conn) {
		d := &strana{t: t, c: c, z: z, id: drugi.node.ID, pokvari: func() { pada.Store(true) }, prekini: prekini}
		for _, p := range r.potezi {
			p.radi(d)
		}
	}
	var o ishodRazgovora
	var skraceno string
	if r.pozivatelj {
		o, skraceno = zoviZaZapis(ctx, ctxCvora, t, s, drugi, r, ln, pada, vodi)
	} else {
		o, skraceno = primiZaZapis(ctx, ctxCvora, t, s, drugi, r, ln, pada, vodi)
	}
	granica, err := json.Marshal(o.theirs)
	if err != nil {
		t.Fatal(err)
	}
	z.dodaj(fmt.Sprintf("ishod applied=%d sent=%d err=%s", o.applied, o.sent, vrstaGreske(o.err)))
	z.dodaj("granica " + string(granica))
	z.dodaj(fmt.Sprintf("tailscale=%#v skraceno=%s", o.theirs.tailscale, skraceno))
	z.dodaj(knjigaCvora(s))
	if r.nakon != nil {
		for _, redak := range r.nakon() {
			z.dodaj(redak)
		}
	}
	return z.sve()
}

// zoviZaZapis: čvor s nazove drugoga (initiator), a drugu stranu vodi skripta
func zoviZaZapis(ctx, ctxCvora context.Context, t *testing.T, s, drugi *Service, r razgovor, ln net.Listener, pada *atomic.Bool, vodi func(*razmjena.Conn)) (ishodRazgovora, string) {
	t.Helper()
	gotovo := make(chan struct{})
	go func() {
		_ = razmjena.ServeExchangeOn(ctx, drugi.node.key, Protocol, drugi.vjerodajnice, ln, drugi.trusted, func(c *razmjena.Conn) {
			defer close(gotovo)
			defer c.Close()
			vodi(c)
		})
	}()
	raw, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	var veza net.Conn = raw
	if r.prerusi != nil {
		veza = prerusenaVeza{Conn: raw, lokalna: r.prerusi.lokalna, udaljena: r.prerusi.udaljena}
	}
	conn, err := razmjena.DialExchangeOn(ctx, kvarnaVeza{Conn: veza, pada: pada}, s.node.key, Protocol, s.vjerodajnice, ln.Addr().String(), drugi.javni())
	if err != nil {
		t.Fatal(err)
	}
	// razgovor koji zapne (obje strane čekaju) prekine se kad rok istekne,
	// a ne tek kad istekne rok čitanja veze (120 s)
	defer context.AfterFunc(ctx, func() { conn.Close() })()
	pada.Store(r.odmah)
	var o ishodRazgovora
	o.applied, o.sent, o.theirs, o.err = s.exchange(ctxCvora, conn, true)
	skraceno := skracenoZa(s, conn)
	conn.Close()
	select {
	case <-gotovo:
	case <-ctx.Done():
		t.Fatal("skripta nije završila")
	}
	return o, skraceno
}

// primiZaZapis: drugi (skripta) nazove čvor s, koji razgovor prima
func primiZaZapis(ctx, ctxCvora context.Context, t *testing.T, s, drugi *Service, r razgovor, ln net.Listener, pada *atomic.Bool, vodi func(*razmjena.Conn)) (ishodRazgovora, string) {
	t.Helper()
	var sl net.Listener = ln
	if r.prerusi != nil {
		sl = prerusenaSlusalica{Listener: ln, lokalna: r.prerusi.lokalna, udaljena: r.prerusi.udaljena}
	}
	type rezultat struct {
		o        ishodRazgovora
		skraceno string
	}
	ishod := make(chan rezultat, 1)
	// namjerno promijenjeno: vrata puštaju drugoga po ključu, bez s.trusted.
	// Vrata članstva čitaju i iz knjige (clanstvo_oznake.go), pa razgovor s
	// pokvarenom knjigom inače ne bi ni počeo; vrata imaju svoje testove.
	vrata := func(pub ed25519.PublicKey, _ []byte) bool { return pub.Equal(drugi.javni()) }
	go func() {
		_ = razmjena.ServeExchangeOn(ctx, s.node.key, Protocol, s.vjerodajnice, kvarnaSlusalica{Listener: sl, pada: pada}, vrata, func(c *razmjena.Conn) {
			defer c.Close()
			var r rezultat
			r.o.applied, r.o.sent, r.o.theirs, r.o.err = s.exchange(ctxCvora, c, false)
			r.skraceno = skracenoZa(s, c)
			ishod <- r
		})
	}()
	conn, err := razmjena.DialExchange(ctx, drugi.node.key, Protocol, drugi.vjerodajnice, ln.Addr().String(), s.javni())
	if err != nil {
		t.Fatal(err)
	}
	defer context.AfterFunc(ctx, func() { conn.Close() })()
	vodi(conn)
	conn.Close()
	select {
	case r := <-ishod:
		return r.o, r.skraceno
	case <-ctx.Done():
		t.Fatal("čvor nije završio razgovor")
	}
	return ishodRazgovora{}, ""
}

// skracenoZa kaže je li razgovor zapamćen kao skraćen. Ključ mora biti baš
// veza koju je exchange dobio, jer je SyncWith tako čita.
func skracenoZa(s *Service, c *razmjena.Conn) string {
	n := 0
	s.skracene.Range(func(_, _ any) bool { n++; return true })
	v, ok := s.skracene.LoadAndDelete(c)
	da, _ := v.(bool)
	switch {
	case n == 0:
		return "ne"
	case n == 1 && ok && da:
		return "da"
	}
	return fmt.Sprintf("krivo (%d zapisa, za ovu vezu %v)", n, v)
}

// vrstaGreske bilježi grešku razgovora. Greške mreže idu kao vrsta (kraj
// veze, veza pala), jer zatvorena veza javi EOF ili RST; ostale idu
// tekstom. Vrsta se prepoznaje samo na neomotanoj grešci, pa bi omotanu
// zapis uhvatio po tekstu. Odbijenica ide s kodom razloga i razlogom
// (namjerno promijenjeno: G3, docs/plan-web-sync.md §3; prije samo razlog).
func vrstaGreske(err error) string {
	odbijena, jeOdbijena := err.(odbijenaRazmjena)
	_, mrezna := err.(*net.OpError)
	switch {
	case err == nil:
		return "nil"
	case err == errVezaPala:
		return "veza pala"
	case err == io.EOF, err == io.ErrUnexpectedEOF, mrezna:
		return "kraj veze"
	case jeOdbijena:
		return fmt.Sprintf("odbijeno 0x%04x: %s", uint64(odbijena.kod), odbijena.razlog)
	case err == errUpisZauzet:
		return "errUpisZauzet"
	}
	return err.Error()
}

// knjigaCvora su verzije u knjizi čvora, imenima iz imenaVerzija
func knjigaCvora(s *Service) string {
	rows, err := s.db.Query(`SELECT version_id FROM record_versions ORDER BY version_id`)
	if err != nil {
		return "knjiga se ne čita"
	}
	defer rows.Close()
	var imena []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "knjiga: " + err.Error()
		}
		imena = append(imena, cmp.Or(imenaVerzija[id], id))
	}
	if err := rows.Err(); err != nil {
		return "knjiga: " + err.Error()
	}
	if len(imena) == 0 {
		return "knjiga prazna"
	}
	return "knjiga " + strings.Join(imena, " ")
}

// cistiPar: a (cop-osijek) i b (laptop-baranja) iz izdavacIPrimatelj, bez
// verzija u knjizi (članstva ostaju u tablici memberships, pa se čvorovi i
// dalje prepoznaju) i sa zadanim izdanjem programa čvora b
func cistiPar(t *testing.T) (a, b *Service) {
	t.Helper()
	a, b = izdavacIPrimatelj(t)
	for _, s := range []*Service{a, b} {
		if _, err := s.db.Exec(`DELETE FROM record_versions`); err != nil {
			t.Fatal(err)
		}
	}
	b.PostaviProgram("0.0.39-test", "0.0.39-test (abc1234)")
	return a, b
}

// upisiStalno upisuje verzije u knjigu mimo Recorder-a, sa zadanim
// version_id i vremenom
func upisiStalno(t *testing.T, s *Service, vv ...ledger.Version) {
	t.Helper()
	for _, v := range vv {
		if _, err := s.db.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel)
			VALUES (?, ?, ?, ?, '', 0, ?, ?, 1, ?)`, v.VersionID, v.Entity, v.EntityID, v.NodeID, string(v.Payload), v.CreatedAt, v.Channel); err != nil {
			t.Fatal(err)
		}
	}
}

// samoV1 vrati knjigu čvora na jednu verziju, v1, između razgovora istog
// para
func samoV1(t *testing.T, s *Service) {
	t.Helper()
	if _, err := s.db.Exec(`DELETE FROM record_versions`); err != nil {
		t.Fatal(err)
	}
	upisiStalno(t, s, vlastitaV1)
}

func uloga(pozivatelj bool) string {
	if pozivatelj {
		return "pozivatelj"
	}
	return "pozvani"
}

// Retci ishoda pravilnog razgovora (čvor ima v1, druga strana d1) prema
// tome dokle je razgovor stigao
func prijeGranice(greska string) []string {
	return []string{"ishod applied=0 sent=0 err=" + greska, granicaNula, obicno, "knjiga v1"}
}

func prijeDelte(greska string) []string {
	return []string{"ishod applied=0 sent=0 err=" + greska, granicaDrugogNepomaknuta, obicno, "knjiga v1"}
}

func nakonDelte(greska string) []string {
	return []string{"ishod applied=1 sent=1 err=" + greska, granicaDrugogSV1, obicno, "knjiga v1 d1"}
}

// tokPozivatelja je pravilan razgovor kad čvor zove: čvor ima v1, druga
// strana šalje d1, a sadržaja nema nijedna
func tokPozivatelja() []potez {
	return []potez{
		cita(granicaV1), salje(kindFrontier, granicaDrugog),
		cita(deltaV1), salje(kindDelta, deltaMsg{Versions: []ledger.Version{dolaznaD1}}),
		cita(zeljePrazne), salje(kindZelje, zeljeMsg{}),
		cita(sadrzajPrazan), salje(kindSadrzaj, sadrzajMsg{}),
		cita(krajCvora(1)), salje(kindDone, doneMsg{Applied: 1}),
	}
}

// tokPozvanog je isti razgovor kad druga strana zove čvor
func tokPozvanog() []potez {
	return []potez{
		salje(kindFrontier, granicaDrugog), cita(granicaV1),
		salje(kindDelta, deltaMsg{Versions: []ledger.Version{dolaznaD1}}), cita(deltaV1),
		salje(kindZelje, zeljeMsg{}), cita(zeljePrazne),
		salje(kindSadrzaj, sadrzajMsg{}), cita(sadrzajPrazan),
		salje(kindDone, doneMsg{Applied: 1}), cita(krajCvora(1)),
	}
}

// zamijeni je pravilan razgovor u kojem je i-ti potez zamijenjen
func zamijeni(tok []potez, i int, p potez) []potez {
	out := slices.Clone(tok)
	out[i] = p
	return out
}

// slucaj je jedan razgovor iz niza razgovora istog para
type slucaj struct {
	ime      string
	odmah    bool
	prerusi  *prerusi
	pripremi func() // stanje čvora uz v1, prije razgovora
	potezi   []potez
	nakon    func() []string
	ishod    []string
}

// provjeriSlucajeve vodi razgovore jedan za drugim s istim parom; prije
// svakog čvor u knjizi ima samo v1 i nema spremišta sadržaja
func provjeriSlucajeve(t *testing.T, a, b *Service, pozivatelj bool, slucajevi []slucaj) {
	t.Helper()
	for _, sl := range slucajevi {
		samoV1(t, b)
		b.SetSpremiste(nil)
		if sl.pripremi != nil {
			sl.pripremi()
		}
		r := razgovor{pozivatelj: pozivatelj, odmah: sl.odmah, prerusi: sl.prerusi, potezi: sl.potezi, nakon: sl.nakon}
		provjeriTijek(t, b, a, uloga(pozivatelj)+", "+sl.ime, r, sl.ishod...)
	}
}

// Pravilan razgovor i pad veze na svakom koraku, za obje strane: što je
// čvor poslao do pada, što je vratio (applied, sent, granica) i je li
// primljeno upisao. Kraj razgovora (done) greške ne javlja, osim kad
// pozivatelju padne pisanje kraja.
func TestTijekRazgovoraPadVeze(t *testing.T) {
	a, b := cistiPar(t)
	tok := tokPozivatelja()
	provjeriSlucajeve(t, a, b, true, []slucaj{
		{ime: "bez kvara", potezi: do(tok, 10, cita(krajVeze)), ishod: nakonDelte("nil")},
		{ime: "nestane nakon granice", potezi: do(tok, 1), ishod: prijeGranice("kraj veze")},
		{ime: "nestane nakon delte", potezi: do(tok, 3), ishod: prijeDelte("kraj veze")},
		{ime: "nestane nakon želja", potezi: do(tok, 5), ishod: nakonDelte("kraj veze")},
		{ime: "nestane nakon sadržaja", potezi: do(tok, 7), ishod: nakonDelte("kraj veze")},
		{ime: "nestane nakon kraja", potezi: do(tok, 9), ishod: nakonDelte("nil")},
		{ime: "pisanje granice", odmah: true, potezi: []potez{cita(krajVeze)}, ishod: prijeGranice("veza pala")},
		{ime: "pisanje delte", potezi: do(tok, 1, kvar(), tok[1], cita(krajVeze)), ishod: prijeDelte("veza pala")},
		{ime: "pisanje želja", potezi: do(tok, 3, kvar(), tok[3], cita(krajVeze)), ishod: nakonDelte("veza pala")},
		{ime: "pisanje sadržaja", potezi: do(tok, 5, kvar(), tok[5], cita(krajVeze)), ishod: nakonDelte("veza pala")},
		{ime: "pisanje kraja", potezi: do(tok, 7, kvar(), tok[7], cita(krajVeze)), ishod: nakonDelte("veza pala")},
	})
	tok = tokPozvanog()
	provjeriSlucajeve(t, a, b, false, []slucaj{
		{ime: "bez kvara", potezi: do(tok, 10, cita(krajVeze)), ishod: nakonDelte("nil")},
		{ime: "nestane prije granice", potezi: do(tok, 0), ishod: prijeGranice("kraj veze")},
		{ime: "nestane prije delte", potezi: do(tok, 2), ishod: prijeDelte("kraj veze")},
		{ime: "nestane prije želja", potezi: do(tok, 4), ishod: nakonDelte("kraj veze")},
		{ime: "nestane prije sadržaja", potezi: do(tok, 6), ishod: nakonDelte("kraj veze")},
		{ime: "nestane prije kraja", potezi: do(tok, 8), ishod: nakonDelte("nil")},
		// granica druge strane je stigla, pa se vraća i kad odgovor padne
		{ime: "pisanje granice", potezi: do(tok, 0, kvar(), tok[0], cita(krajVeze)), ishod: prijeDelte("veza pala")},
		// primljena delta se ne upisuje kad odgovor na nju ne ode
		{ime: "pisanje delte", potezi: do(tok, 2, kvar(), tok[2], cita(krajVeze)), ishod: prijeDelte("veza pala")},
		{ime: "pisanje želja", potezi: do(tok, 4, kvar(), tok[4], cita(krajVeze)), ishod: nakonDelte("veza pala")},
		{ime: "pisanje sadržaja", potezi: do(tok, 6, kvar(), tok[6], cita(krajVeze)), ishod: nakonDelte("veza pala")},
		{ime: "pisanje kraja", potezi: do(tok, 8, kvar(), tok[8], cita(krajVeze)), ishod: nakonDelte("nil")},
	})
}

// zamjenaNaSvakomKoraku: druga strana na svakom koraku pravilnog razgovora,
// za obje strane, umjesto očekivane poruke pošalje zamjenu; greska je
// greška razgovora kad je čvor čekao zadanu vrstu
func zamjenaNaSvakomKoraku(t *testing.T, a, b *Service, zamjena potez, greska func(vrsta string) string) {
	t.Helper()
	tok := tokPozivatelja()
	provjeriSlucajeve(t, a, b, true, []slucaj{
		{ime: "umjesto granice", potezi: do(tok, 1, zamjena, cita(krajVeze)), ishod: prijeGranice(greska(kindFrontier))},
		{ime: "umjesto delte", potezi: do(tok, 3, zamjena, cita(krajVeze)), ishod: prijeDelte(greska(kindDelta))},
		{ime: "umjesto želja", potezi: do(tok, 5, zamjena, cita(krajVeze)), ishod: nakonDelte(greska(kindZelje))},
		{ime: "umjesto sadržaja", potezi: do(tok, 7, zamjena, cita(krajVeze)), ishod: nakonDelte(greska(kindSadrzaj))},
		{ime: "umjesto kraja", potezi: do(tok, 9, zamjena, cita(krajVeze)), ishod: nakonDelte("nil")},
	})
	tok = tokPozvanog()
	provjeriSlucajeve(t, a, b, false, []slucaj{
		{ime: "umjesto granice", potezi: do(tok, 0, zamjena, cita(krajVeze)), ishod: prijeGranice(greska(kindFrontier))},
		{ime: "umjesto delte", potezi: do(tok, 2, zamjena, cita(krajVeze)), ishod: prijeDelte(greska(kindDelta))},
		{ime: "umjesto želja", potezi: do(tok, 4, zamjena, cita(krajVeze)), ishod: nakonDelte(greska(kindZelje))},
		{ime: "umjesto sadržaja", potezi: do(tok, 6, zamjena, cita(krajVeze)), ishod: nakonDelte(greska(kindSadrzaj))},
		// pozvani kraj ne provjerava: na bilo što odgovori svojim krajem
		{ime: "umjesto kraja", potezi: do(tok, 8, zamjena, cita(krajCvora(1)), cita(krajVeze)), ishod: nakonDelte("nil")},
	})
}

// Poruka krive vrste, bez razloga, na svakom koraku i za obje strane:
// razgovor stane greškom koja kaže što je čekao i što je stiglo, i čvor
// više ništa ne pošalje. Na kraju razgovora kriva vrsta nije greška.
func TestTijekRazgovoraNeocekivanaPoruka(t *testing.T) {
	a, b := cistiPar(t)
	zamjenaNaSvakomKoraku(t, a, b, salje("nepoznato", struct{}{}), func(vrsta string) string {
		return `očekivana poruka "` + vrsta + `", stigla "nepoznato"`
	})
}

// Odbijenica s razlogom na svakom koraku i za obje strane: razgovor stane
// neomotanom greškom odbijenaRazmjena s razlogom i kodom druge strane (po
// kodu SyncWith prepoznaje i isključen Tailscale), a na kraju razgovora
// odbijenica nije greška. Odbijenica čvora 0.0.38 nema kod, pa se kod uzme
// iz poznatog razloga (zamrznut tekst 0.0.38), a nepoznat razlog bez koda
// daje 0.
//
// Namjerno promijenjeno: G3 (docs/plan-web-sync.md §3). Greška se bilježi s
// kodom, a uz odbijenice bez koda dodane su odbijenice s kodom.
func TestTijekRazgovoraOdbijenica(t *testing.T) {
	a, b := cistiPar(t)
	zauzet := func(string) string { return "odbijeno 0x0102: čvor je zauzet, pokušajte kasnije" }
	zamjenaNaSvakomKoraku(t, a, b, odbija(zauzet0038), zauzet)
	zamjenaNaSvakomKoraku(t, a, b, odbijaKodom(razmjena.KodZauzet, razmjena.RazlogZauzet), zauzet)
	bezTailscalea := prijeGranice("odbijeno 0x0107: na ovom računalu razmjena preko Tailscalea je isključena ([sync] tailscale = false)")
	nepoznat := "razmjena na ovim vratima nije dopuštena"
	tok := tokPozivatelja()
	provjeriSlucajeve(t, a, b, true, []slucaj{
		{ime: "bez Tailscalea", potezi: do(tok, 1, odbija(bezTailscalea0038), cita(krajVeze)), ishod: bezTailscalea},
		{ime: "bez Tailscalea, s kodom", potezi: do(tok, 1, odbijaKodom(razmjena.KodBezTailscalea, razlogBezTailscalea), cita(krajVeze)), ishod: bezTailscalea},
		{ime: "nepoznat razlog", potezi: do(tok, 1, odbija(nepoznat), cita(krajVeze)), ishod: prijeGranice("odbijeno 0x0000: " + nepoznat)},
		{ime: "nepoznat razlog, s kodom", potezi: do(tok, 1, odbijaKodom(razmjena.KodOdbijeno, nepoznat), cita(krajVeze)), ishod: prijeGranice("odbijeno 0x0103: " + nepoznat)},
	})
}

// Sadržaj u punom razgovoru: čvor nudi svoj PDF (otisakCvora) i traži tuđi
// (otisakTudji), a druga strana traži oba i šalje tuđi
const (
	pdfCvora    = "%PDF-1.4 akt"
	pdfTudji    = "%PDF-1.4 tudji"
	otisakCvora = "39ab16b8deb4b42c50182143a8caa72aaa5edb130421b3c736165e2655fef107"
	otisakTudji = "71964644455c62e9bbc1ed818c973c4ce1312a38483fba45ab6bc71b84e559e6"
	stavkaCvora = `{"otisak":"` + otisakCvora + `","vrsta":"application/pdf","kanal":"prijave/16/2026","bajtova":12,"podaci":"JVBERi0xLjQgYWt0"}`
	stavkaTudja = `{"otisak":"` + otisakTudji + `","vrsta":"application/pdf","kanal":"prijave/16/2026","bajtova":14,"podaci":"JVBERi0xLjQgdHVkamk="}`
)

var (
	zeljeCvora = odCvora(kindZelje, `{"otisci":["`+otisakTudji+`"]}`)
	tudjiPDF   = sadrzajMsg{Stavke: []stavkaSadrzaja{{Otisak: otisakTudji, Vrsta: "application/pdf", Kanal: "prijave/16/2026", Bajtova: len(pdfTudji), Podaci: []byte(pdfTudji)}}}
)

// sSadrzajem daje čvoru novo spremište s vlastitim PDF-om i željom za tuđim
func sSadrzajem(t *testing.T, s *Service) *sadrzaj.Spremiste {
	t.Helper()
	sp := otvoriSpremiste(t)
	s.SetSpremiste(sp)
	if _, err := sp.Upisi(ctxRub, "application/pdf", []byte(pdfCvora), "ovdje", sadrzaj.Veza{Kanal: "prijave/16/2026"}); err != nil {
		t.Fatal(err)
	}
	if err := sp.Zeli(ctxRub, otisakTudji, "application/pdf", len(pdfTudji), "test", "prijave/16/2026"); err != nil {
		t.Fatal(err)
	}
	return sp
}

// stanjeSadrzaja: drži li čvor tuđi PDF, traži li ga još i je li ga
// razgovor označio zatraženim (sadrzaj.Spremiste.Trazeno)
func stanjeSadrzaja(sp **sadrzaj.Spremiste) func() []string {
	return func() []string {
		zatrazen := false
		if zz, err := (*sp).Zeljeni(ctxRub, 10); err == nil {
			for _, z := range zz {
				zatrazen = zatrazen || (z.Otisak == otisakTudji && z.Trazeno != nil)
			}
		}
		return []string{fmt.Sprintf("spremište ima tuđi=%v, traži tuđi=%v, zatražen=%v", (*sp).Ima(ctxRub, otisakTudji), (*sp).Zeljen(ctxRub, otisakTudji), zatrazen)}
	}
}

// Puni razgovor novijih čvorova, za obje strane: granica, delta, želje,
// sadržaj i kraj, svaka poruka točno kako ide žicom. Pozivatelj svoj
// sadržaj šalje prije nego što primi tuđi; pozvani tek nakon što je
// primljeni upisao, pa vrati i upravo primljeno.
func TestTijekRazgovoraPuni(t *testing.T) {
	a, b := cistiPar(t)
	var sp *sadrzaj.Spremiste
	granica := frontierMsg{Frontier: map[string]string{"cop-osijek|ocitanja/16/2026": tijekD1}, Wants: &Wants{All: true}, Program: "0.0.38-alfa"}
	delta := deltaMsg{Versions: []ledger.Version{dolaznaD1}}
	zelje := zeljeMsg{Otisci: []string{otisakCvora, otisakTudji}}
	ishod := []string{"ishod applied=1 sent=1 err=nil",
		`granica {"frontier":{"cop-osijek|ocitanja/16/2026":"` + tijekD1 + `","laptop-baranja|ocitanja/16/2026":"` + tijekV1 + `"},"wants":{"all":true},"program":"0.0.38-alfa"}`,
		obicno, "knjiga v1 d1", "spremište ima tuđi=true, traži tuđi=false, zatražen=false"}
	pripremi := func() { sp = sSadrzajem(t, b) }
	provjeriSlucajeve(t, a, b, true, []slucaj{{ime: "sa sadržajem", pripremi: pripremi, nakon: stanjeSadrzaja(&sp), ishod: ishod, potezi: []potez{
		cita(granicaV1), salje(kindFrontier, granica),
		cita(deltaV1), salje(kindDelta, delta),
		cita(zeljeCvora), salje(kindZelje, zelje),
		cita(odCvora(kindSadrzaj, `{"stavke":[`+stavkaCvora+`]}`)), salje(kindSadrzaj, tudjiPDF),
		cita(krajCvora(1)), salje(kindDone, doneMsg{Applied: 1}),
		cita(krajVeze),
	}}})
	provjeriSlucajeve(t, a, b, false, []slucaj{{ime: "sa sadržajem", pripremi: pripremi, nakon: stanjeSadrzaja(&sp), ishod: ishod, potezi: []potez{
		salje(kindFrontier, granica), cita(granicaV1),
		salje(kindDelta, delta), cita(deltaV1),
		salje(kindZelje, zelje), cita(zeljeCvora),
		salje(kindSadrzaj, tudjiPDF), cita(odCvora(kindSadrzaj, `{"stavke":[`+stavkaCvora+`,`+stavkaTudja+`]}`)),
		salje(kindDone, doneMsg{Applied: 1}), cita(krajCvora(1)),
		cita(krajVeze),
	}}})
}

// Stariji čvor bez sadržaja šalje granicu bez želja i izdanja, a nakon
// delte done. Kad ga novi čvor zove, pošalje želje, primi done i odgovori
// svojim; kad stariji zove, novi umjesto želja primi done i odgovori. Oba
// puta je to uspjeh, a želje novi čvor složi (i označi zatraženima) i kad
// ih ne pošalje.
func TestTijekRazgovoraStarijiCvor(t *testing.T) {
	a, b := cistiPar(t)
	var sp *sadrzaj.Spremiste
	stariji := frontierMsg{Frontier: map[string]string{}}
	delta := deltaMsg{Versions: []ledger.Version{dolaznaD1}}
	ishod := []string{"ishod applied=1 sent=1 err=nil", `granica {"frontier":{"laptop-baranja|ocitanja/16/2026":"` + tijekV1 + `"}}`,
		obicno, "knjiga v1 d1", "spremište ima tuđi=false, traži tuđi=true, zatražen=true"}
	pripremi := func() { sp = sSadrzajem(t, b) }
	provjeriSlucajeve(t, a, b, true, []slucaj{{ime: "stariji pozvani", pripremi: pripremi, nakon: stanjeSadrzaja(&sp), ishod: ishod, potezi: []potez{
		cita(granicaV1), salje(kindFrontier, stariji),
		cita(deltaV1), salje(kindDelta, delta),
		cita(zeljeCvora), salje(kindDone, doneMsg{}),
		cita(krajCvora(1)), cita(krajVeze),
	}}})
	provjeriSlucajeve(t, a, b, false, []slucaj{{ime: "stariji pozivatelj", pripremi: pripremi, nakon: stanjeSadrzaja(&sp), ishod: ishod, potezi: []potez{
		salje(kindFrontier, stariji), cita(granicaV1),
		salje(kindDelta, delta), cita(deltaV1),
		salje(kindDone, doneMsg{}), cita(krajCvora(1)),
		cita(krajVeze),
	}}})
}

// Skraćena delta: čvor pošalje samo prvu verziju ključa uz "skraceno":true,
// granica druge strane pomakne se samo do poslanog, a razgovor je zapamćen
// kao skraćen pod svojom vezom, i kad je skratila druga strana, i kad
// razgovor poslije delte padne. Kad stane prije nego što obje delte prođu,
// nije zapamćen.
func TestTijekRazgovoraSkracenaDelta(t *testing.T) {
	t.Cleanup(SuziDeltu(1))
	a, b := cistiPar(t)
	saV2 := func() { upisiStalno(t, b, vlastitaV2) }
	skracena := odCvora(kindDelta, `{"versions":[`+jsonV1+`],"skraceno":true}`)
	ostaje := "tailscale=[]string(nil) skraceno=da"
	provjeriSlucajeve(t, a, b, true, []slucaj{
		{ime: "čvor skrati", pripremi: saV2, potezi: []potez{
			cita(granicaV2), salje(kindFrontier, granicaDrugog),
			cita(skracena), salje(kindDelta, deltaMsg{}),
			cita(zeljePrazne), salje(kindDone, doneMsg{}),
			cita(krajCvora(0)), cita(krajVeze),
		}, ishod: []string{"ishod applied=0 sent=1 err=nil", granicaDrugogSV1, ostaje, "knjiga v1 v2"}},
		{ime: "nestane nakon delte", pripremi: saV2, potezi: []potez{
			cita(granicaV2), salje(kindFrontier, granicaDrugog), cita(skracena),
		}, ishod: []string{"ishod applied=0 sent=0 err=kraj veze", granicaDrugogNepomaknuta, obicno, "knjiga v1 v2"}},
		{ime: "nestane nakon želja", pripremi: saV2, potezi: []potez{
			cita(granicaV2), salje(kindFrontier, granicaDrugog),
			cita(skracena), salje(kindDelta, deltaMsg{}),
			cita(zeljePrazne),
		}, ishod: []string{"ishod applied=0 sent=1 err=kraj veze", granicaDrugogSV1, ostaje, "knjiga v1 v2"}},
		{ime: "druga strana skrati", potezi: []potez{
			cita(granicaV1), salje(kindFrontier, granicaDrugog),
			cita(deltaV1), salje(kindDelta, deltaMsg{Skraceno: true}),
			cita(zeljePrazne), salje(kindDone, doneMsg{}),
			cita(krajCvora(0)), cita(krajVeze),
		}, ishod: []string{"ishod applied=0 sent=1 err=nil", granicaDrugogSV1, ostaje, "knjiga v1"}},
	})
	provjeriSlucajeve(t, a, b, false, []slucaj{
		{ime: "čvor skrati", pripremi: saV2, potezi: []potez{
			salje(kindFrontier, granicaDrugog), cita(granicaV2),
			salje(kindDelta, deltaMsg{}), cita(skracena),
			salje(kindDone, doneMsg{}), cita(krajCvora(0)),
			cita(krajVeze),
		}, ishod: []string{"ishod applied=0 sent=1 err=nil", granicaDrugogSV1, ostaje, "knjiga v1 v2"}},
		{ime: "druga strana skrati", potezi: []potez{
			salje(kindFrontier, granicaDrugog), cita(granicaV1),
			salje(kindDelta, deltaMsg{Skraceno: true}), cita(deltaV1),
			salje(kindDone, doneMsg{}), cita(krajCvora(0)),
			cita(krajVeze),
		}, ishod: []string{"ishod applied=0 sent=1 err=nil", granicaDrugogSV1, ostaje, "knjiga v1"}},
	})
}

// Verzija s neispravnim JSON-om u knjizi: granica čvora je oglašava, delta
// je ne nosi, granica druge strane pomakne se preko nje, a u dnevnik ide
// jednom. Delta se složi (i neispravna zapiše) prije razmjene delti, pa i
// kad delta ne ode ili tuđa ne stigne.
func TestTijekRazgovoraNeispravnaVerzija(t *testing.T) {
	a, b := cistiPar(t)
	var dnevnik *brojacDnevnika
	pripremi := func() {
		upisiRedakKnjige(t, b, tijekV2, "ocitanja/16/2026", "nije json")
		b.neispravne.Clear()
		dnevnik = brojiDnevnik(t, "nije ispravan JSON")
	}
	nakon := func() []string {
		n := 0
		b.neispravne.Range(func(_, _ any) bool { n++; return true })
		u, _ := dnevnik.broj()
		return []string{fmt.Sprintf("neispravnih %d, u dnevniku %d", n, u)}
	}
	prekoV2 := `granica {"frontier":{"laptop-baranja|ocitanja/16/2026":"` + tijekV2 + `"},"wants":{"all":true},"program":"0.0.38-alfa"}`
	zapisana := "neispravnih 1, u dnevniku 1"
	provjeriSlucajeve(t, a, b, true, []slucaj{
		{ime: "preskoči neispravnu", pripremi: pripremi, nakon: nakon, potezi: []potez{
			cita(granicaV2), salje(kindFrontier, granicaDrugog),
			cita(deltaV1), salje(kindDelta, deltaMsg{}),
			cita(zeljePrazne), salje(kindDone, doneMsg{}),
			cita(krajCvora(0)), cita(krajVeze),
		}, ishod: []string{"ishod applied=0 sent=1 err=nil", prekoV2, obicno, "knjiga v1 v2", zapisana}},
		{ime: "pisanje delte", pripremi: pripremi, nakon: nakon, potezi: []potez{
			cita(granicaV2), kvar(), salje(kindFrontier, granicaDrugog), cita(krajVeze),
		}, ishod: []string{"ishod applied=0 sent=0 err=veza pala", granicaDrugogNepomaknuta, obicno, "knjiga v1 v2", zapisana}},
	})
	provjeriSlucajeve(t, a, b, false, []slucaj{
		{ime: "preskoči neispravnu", pripremi: pripremi, nakon: nakon, potezi: []potez{
			salje(kindFrontier, granicaDrugog), cita(granicaV2),
			salje(kindDelta, deltaMsg{}), cita(deltaV1),
			salje(kindDone, doneMsg{}), cita(krajCvora(0)),
			cita(krajVeze),
		}, ishod: []string{"ishod applied=0 sent=1 err=nil", prekoV2, obicno, "knjiga v1 v2", zapisana}},
		{ime: "nestane prije delte", pripremi: pripremi, nakon: nakon, potezi: []potez{
			salje(kindFrontier, granicaDrugog), cita(granicaV2),
		}, ishod: []string{"ishod applied=0 sent=0 err=kraj veze", granicaDrugogNepomaknuta, obicno, "knjiga v1 v2", zapisana}},
	})
}

// Pretplata i ograda čvora (Accept): od primljene delte upiše se samo ono
// što čvor prati i propušta, i to se javi u done; delta čvora nosi samo
// kanale koje druga strana prati. Isto za obje strane.
func TestTijekRazgovoraFiltriraPrimljeno(t *testing.T) {
	a, b := cistiPar(t)
	b.SetWantsAll(false)
	if _, err := b.AddSubscription(ctxRub, Subscription{Kind: "ocitanja", AreaID: 16}); err != nil {
		t.Fatal(err)
	}
	b.Accept(func(v ledger.Version) bool { return v.EntityID != dolaznaD2.EntityID })
	saV3 := func() { upisiStalno(t, b, vlastitaV3) }
	// namjerno promijenjeno: G2 (docs/plan-web-sync.md §3); granica nosi mogućnosti čvora.
	// namjerno promijenjeno: G4 (docs/plan-web-sync.md §3); i granica-odbijenih/1
	granica := odCvora(kindFrontier, `{"frontier":{"laptop-baranja|ocitanja/16/2026":"`+tijekV1+`","laptop-baranja|ocitanja/3/2026":"`+tijekV3+`"},`+
		`"wants":{"all":false,"rules":[{"id":1,"kind":"ocitanja","sector_id":"","area_id":16,"year_from":0,"year_to":0,"razina":"sve"}]},`+programCvora+`,"mogucnosti":`+naseOznake+`}`)
	delta := odCvora(kindDelta, `{"versions":[`+jsonV3+`]}`)
	njihova := frontierMsg{Frontier: map[string]string{}, Wants: &Wants{Rules: []Subscription{{Kind: "ocitanja", AreaID: 3}}}}
	dolazna := deltaMsg{Versions: []ledger.Version{dolaznaD1, dolaznaD2, dolaznaD3}}
	ishod := []string{"ishod applied=1 sent=1 err=nil",
		`granica {"frontier":{"laptop-baranja|ocitanja/3/2026":"` + tijekV3 + `"},"wants":{"all":false,"rules":[{"id":0,"kind":"ocitanja","sector_id":"","area_id":3,"year_from":0,"year_to":0}]}}`,
		obicno, "knjiga v1 v3 d1"}
	provjeriSlucajeve(t, a, b, true, []slucaj{{ime: "filtrira", pripremi: saV3, ishod: ishod, potezi: []potez{
		cita(granica), salje(kindFrontier, njihova),
		cita(delta), salje(kindDelta, dolazna),
		cita(zeljePrazne), salje(kindDone, doneMsg{}),
		cita(krajCvora(1)), cita(krajVeze),
	}}})
	provjeriSlucajeve(t, a, b, false, []slucaj{{ime: "filtrira", pripremi: saV3, ishod: ishod, potezi: []potez{
		salje(kindFrontier, njihova), cita(granica),
		salje(kindDelta, dolazna), cita(delta),
		salje(kindDone, doneMsg{}), cita(krajCvora(1)),
		cita(krajVeze),
	}}})
}

// Tailscale u delti: u razgovoru preko Tailscalea čvor javi svoje Tailscale
// adrese s portom razmjene, a od primljenih vrati samo Tailscale adrese,
// bez ponavljanja. U lokalnoj mreži, ili kad je Tailscale isključen, ne
// javi ništa i ne vrati ništa. Primljene adrese vrijede tek kad obje delte
// prođu, i ostaju kad razgovor poslije padne.
func TestTijekRazgovoraTailscale(t *testing.T) {
	a, b := cistiPar(t)
	b.ZadajTailscale(adresa("100.101.0.1"), adresa("fd7a:115c:a1e0::1"))
	b.ports.Exchange = 4710
	zove := &prerusi{lokalna: tcpAdresa(t, "100.101.0.1:50000"), udaljena: tcpAdresa(t, "100.101.0.2:4710")}
	prima := &prerusi{lokalna: tcpAdresa(t, "100.101.0.1:4710"), udaljena: tcpAdresa(t, "100.101.0.2:50000")}
	njihova := deltaMsg{Tailscale: []string{"100.101.0.2:4710", "192.168.1.5:4710", "100.101.0.2:4710"}}
	sAdresama := odCvora(kindDelta, `{"versions":[`+jsonV1+`],"tailscale":["100.101.0.1:4710","[fd7a:115c:a1e0::1]:4710"]}`)
	primljene := `tailscale=[]string{"100.101.0.2:4710"} skraceno=ne`
	ukljuci := func(da bool) func() { return func() { b.PostaviTailscale(da) } }
	pozivatelj := func(delta string) []potez {
		return []potez{
			cita(granicaV1), salje(kindFrontier, granicaDrugog),
			cita(delta), salje(kindDelta, njihova),
			cita(zeljePrazne), salje(kindDone, doneMsg{}),
			cita(krajCvora(0)), cita(krajVeze),
		}
	}
	pozvani := func(delta string) []potez {
		return []potez{
			salje(kindFrontier, granicaDrugog), cita(granicaV1),
			salje(kindDelta, njihova), cita(delta),
			salje(kindDone, doneMsg{}), cita(krajCvora(0)),
			cita(krajVeze),
		}
	}
	ishod := func(tailscale string) []string {
		return []string{"ishod applied=0 sent=1 err=nil", granicaDrugogSV1, tailscale, "knjiga v1"}
	}
	provjeriSlucajeve(t, a, b, true, []slucaj{
		{ime: "preko Tailscalea", prerusi: zove, pripremi: ukljuci(true), potezi: pozivatelj(sAdresama), ishod: ishod(primljene)},
		{ime: "lokalna mreža", pripremi: ukljuci(true), potezi: pozivatelj(deltaV1), ishod: ishod(obicno)},
		{ime: "Tailscale isključen", prerusi: zove, pripremi: ukljuci(false), potezi: pozivatelj(deltaV1), ishod: ishod(obicno)},
		{ime: "nestane nakon delte", prerusi: zove, pripremi: ukljuci(true), potezi: pozivatelj(sAdresama)[:3],
			ishod: []string{"ishod applied=0 sent=0 err=kraj veze", granicaDrugogNepomaknuta, obicno, "knjiga v1"}},
		{ime: "nestane nakon želja", prerusi: zove, pripremi: ukljuci(true), potezi: pozivatelj(sAdresama)[:5],
			ishod: []string{"ishod applied=0 sent=1 err=kraj veze", granicaDrugogSV1, primljene, "knjiga v1"}},
	})
	provjeriSlucajeve(t, a, b, false, []slucaj{
		{ime: "preko Tailscalea", prerusi: prima, pripremi: ukljuci(true), potezi: pozvani(sAdresama), ishod: ishod(primljene)},
		{ime: "lokalna mreža", pripremi: ukljuci(true), potezi: pozvani(deltaV1), ishod: ishod(obicno)},
		{ime: "Tailscale isključen", prerusi: prima, pripremi: ukljuci(false), potezi: pozvani(deltaV1), ishod: ishod(obicno)},
	})
}

// Zauzet upis (zavrsiZauzet): pozivatelj pošalje done{0} s deviceId,
// pročita jednu poruku i vrati errUpisZauzet; pozvani pročita jednu poruku
// (želje ili kraj) i pošalje odbijenicu s kodom, bez deviceId i payloada. Obje
// strane vrate 0 i 0, iako je delta otišla, s pomaknutom granicom. Bez
// primljenih verzija razgovor bravu ne čeka, a primljeni sadržaj tada se
// ne upiše i razgovor uspije.
func TestTijekRazgovoraZauzetUpis(t *testing.T) {
	a, b := cistiPar(t)
	if err := b.uzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.pustiBravu)
	postaviRok(t, 20*time.Millisecond)
	// namjerno promijenjeno: G3 (docs/plan-web-sync.md §3); odbijenica nosi
	// kod razloga 0x0106 (262)
	odbijenica := `{"kind":"odbijeno","reason":"čvor upravo upisuje ono što je primio od drugih; poslano stiže sljedećom razmjenom","kod":262}`
	zauzet := []string{"ishod applied=0 sent=0 err=errUpisZauzet", granicaDrugogSV1, obicno, "knjiga v1"}
	bezVerzija := []string{"ishod applied=0 sent=1 err=nil", granicaDrugogSV1, obicno, "knjiga v1"}
	var sp *sadrzaj.Spremiste
	var dnevnik *brojacDnevnika
	saSadrzajem := func() {
		sp = sSadrzajem(t, b)
		dnevnik = brojiDnevnik(t, "primljeni sadržaj nije upisan")
	}
	nakon := func() []string {
		n, _ := dnevnik.broj()
		return append(stanjeSadrzaja(&sp)(), fmt.Sprintf("u dnevniku %d", n))
	}
	sSadrzajemIshod := append(slices.Clone(bezVerzija), "spremište ima tuđi=false, traži tuđi=true, zatražen=true", "u dnevniku 1")
	tok := tokPozivatelja()
	provjeriSlucajeve(t, a, b, true, []slucaj{
		{ime: "zauzet", potezi: do(tok, 4, cita(krajCvora(0)), salje(kindDone, doneMsg{}), cita(krajVeze)), ishod: zauzet},
		{ime: "bez primljenih verzija", potezi: append(zamijeni(zamijeni(tok, 3, salje(kindDelta, deltaMsg{})), 8, cita(krajCvora(0))), cita(krajVeze)), ishod: bezVerzija},
		{ime: "samo sadržaj", pripremi: saSadrzajem, nakon: nakon, potezi: []potez{
			cita(granicaV1), salje(kindFrontier, granicaDrugog),
			cita(deltaV1), salje(kindDelta, deltaMsg{}),
			cita(zeljeCvora), salje(kindZelje, zeljeMsg{}),
			cita(sadrzajPrazan), salje(kindSadrzaj, tudjiPDF),
			cita(krajCvora(0)), salje(kindDone, doneMsg{}),
			cita(krajVeze),
		}, ishod: sSadrzajemIshod},
	})
	tok = tokPozvanog()
	provjeriSlucajeve(t, a, b, false, []slucaj{
		{ime: "zauzet, pa želje", potezi: do(tok, 4, salje(kindZelje, zeljeMsg{}), cita(odbijenica), cita(krajVeze)), ishod: zauzet},
		{ime: "zauzet, pa kraj", potezi: do(tok, 4, salje(kindDone, doneMsg{}), cita(odbijenica), cita(krajVeze)), ishod: zauzet},
		{ime: "bez primljenih verzija", potezi: append(zamijeni(zamijeni(tok, 2, salje(kindDelta, deltaMsg{})), 9, cita(krajCvora(0))), cita(krajVeze)), ishod: bezVerzija},
		{ime: "samo sadržaj", pripremi: saSadrzajem, nakon: nakon, potezi: []potez{
			salje(kindFrontier, granicaDrugog), cita(granicaV1),
			salje(kindDelta, deltaMsg{}), cita(deltaV1),
			salje(kindZelje, zeljeMsg{}), cita(zeljeCvora),
			salje(kindSadrzaj, tudjiPDF), cita(sadrzajPrazan),
			salje(kindDone, doneMsg{}), cita(krajCvora(0)),
			cita(krajVeze),
		}, ishod: sSadrzajemIshod},
	})
}

// Greška upisa koja nije zauzeta brava (baza ne primi verziju, kontekst
// čvora prestane dok čeka bravu): razgovor stane tom greškom, neomotanom,
// bez done i bez odbijenice, s pomaknutom granicom i 0, 0.
//
// Namjerno promijenjeno: G3 (docs/plan-web-sync.md §3). Do tada je ovdje
// bio slučaj „knjiga odbije”: primljena verzija bez identifikatora zapisa
// rušila je upis. Sad se preskoči (TestTijekRazgovoraNeispravnaPrimljena),
// pa greška upisa nastaje samo u bazi ili kontekstu.
func TestTijekRazgovoraGreskaUpisa(t *testing.T) {
	a, b := cistiPar(t)
	nePrima := []string{"ishod applied=0 sent=0 err=greška pri primanju verzije " + tijekD1 + ": constraint failed: namjerni kvar (1811)", granicaDrugogSV1, obicno, "knjiga v1"}
	pozivatelj, pozvani := tokPozivatelja(), tokPozvanog()
	kvarUpisa(t, b, "INSERT", "record_versions", "NEW.node_id = 'cop-osijek'")
	provjeriSlucajeve(t, a, b, true, []slucaj{{ime: "baza ne primi", potezi: do(pozivatelj, 4, cita(krajVeze)), ishod: nePrima}})
	provjeriSlucajeve(t, a, b, false, []slucaj{{ime: "baza ne primi", potezi: do(pozvani, 5, cita(krajVeze)), ishod: nePrima}})
	if _, err := b.db.Exec(fmt.Sprintf(`DROP TRIGGER kvar_%d`, brojKvarova)); err != nil {
		t.Fatal(err)
	}

	if err := b.uzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.pustiBravu)
	postaviRok(t, time.Minute)
	prekid := usput("prekid konteksta čvora", func(d *strana) { d.prekini() })
	otkazan := []string{"ishod applied=0 sent=0 err=context canceled", granicaDrugogSV1, obicno, "knjiga v1"}
	provjeriSlucajeve(t, a, b, true, []slucaj{{ime: "kontekst prestane", potezi: do(pozivatelj, 4, prekid, cita(krajVeze)), ishod: otkazan}})
	provjeriSlucajeve(t, a, b, false, []slucaj{{ime: "kontekst prestane", potezi: do(pozvani, 4, prekid, cita(krajVeze)), ishod: otkazan}})
}

// Primljena verzija bez identifikatora zapisa (d4) se preskoči, a ostale
// iz delte se upišu; razgovor ide do kraja i done javlja samo upisane. U
// dnevnik ide jednom po verziji. Paket samo neispravnih ne čeka bravu, pa
// razgovor uspije i kad je upis zauzet. G3 (docs/plan-web-sync.md §3): do
// tada je jedna takva verzija odbila cijeli paket (TestTijekRazgovoraGreskaUpisa).
func TestTijekRazgovoraNeispravnaPrimljena(t *testing.T) {
	a, b := cistiPar(t)
	neispravnaD4 := stalnaVerzija(tijekD4, "cop-osijek", "ocitanja/16/2026", "", `{"cm":132}`)
	samoNeispravna := salje(kindDelta, deltaMsg{Versions: []ledger.Version{neispravnaD4}})
	uzIspravnu := salje(kindDelta, deltaMsg{Versions: []ledger.Version{dolaznaD1, neispravnaD4}})
	var dnevnik *brojacDnevnika
	brojiIznova := func() {
		b.preskocene.Clear()
		dnevnik = brojiDnevnik(t, "ne upisuje se")
	}
	brojiDalje := func() { dnevnik = brojiDnevnik(t, "ne upisuje se") }
	nakon := func() []string {
		n, _ := dnevnik.broj()
		return []string{fmt.Sprintf("u dnevniku %d", n)}
	}
	bezUpisa := []string{"ishod applied=0 sent=1 err=nil", granicaDrugogSV1, obicno, "knjiga v1", "u dnevniku 1"}
	sUpisom := append(nakonDelte("nil"), "u dnevniku 1")
	ponovljena := append(nakonDelte("nil"), "u dnevniku 0")
	// pravilan razgovor u kojem druga strana pošalje zadanu deltu, a čvor u
	// done javi koliko je upisao
	sDeltom := func(pozivatelj bool, delta potez, upisano int) []potez {
		tok, d, k := tokPozvanog(), 2, 9
		if pozivatelj {
			tok, d, k = tokPozivatelja(), 3, 8
		}
		return append(zamijeni(zamijeni(tok, d, delta), k, cita(krajCvora(upisano))), cita(krajVeze))
	}
	for _, pozivatelj := range []bool{true, false} {
		provjeriSlucajeve(t, a, b, pozivatelj, []slucaj{
			{ime: "samo neispravna", pripremi: brojiIznova, nakon: nakon, potezi: sDeltom(pozivatelj, samoNeispravna, 0), ishod: bezUpisa},
			{ime: "uz ispravnu", pripremi: brojiIznova, nakon: nakon, potezi: sDeltom(pozivatelj, uzIspravnu, 1), ishod: sUpisom},
			{ime: "ista neispravna opet", pripremi: brojiDalje, nakon: nakon, potezi: sDeltom(pozivatelj, uzIspravnu, 1), ishod: ponovljena},
		})
	}

	if err := b.uzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.pustiBravu)
	postaviRok(t, 20*time.Millisecond)
	for _, pozivatelj := range []bool{true, false} {
		provjeriSlucajeve(t, a, b, pozivatelj, []slucaj{{ime: "samo neispravna, upis zauzet", pripremi: brojiIznova, nakon: nakon,
			potezi: sDeltom(pozivatelj, samoNeispravna, 0), ishod: bezUpisa}})
	}
}

// Poruka ispravne vrste s payloadom koji se ne da pročitati: granica, delta i
// sadržaj zaustave razgovor neomotanom greškom dekodiranja, a granica druge
// strane ostaje djelomično pročitana. Pokvarene želje i pokvaren kraj se
// zanemare. Granica bez payloada je granica bez ičega i bez želja: čvor
// šalje sve, a granica bez poslanog ostaje nil.
func TestTijekRazgovoraPokvarenPayload(t *testing.T) {
	a, b := cistiPar(t)
	sirovo := func(vrsta, payload string) potez {
		return saljeSirovo(vrsta+" "+payload, razmjena.Envelope{Kind: vrsta, DeviceID: "cop-osijek", Payload: json.RawMessage(payload)})
	}
	losaGranica := sirovo(kindFrontier, `{"frontier":[1],"program":"0.0.38-alfa"}`)
	losaDelta := sirovo(kindDelta, `{"versions":5}`)
	loseZelje := sirovo(kindZelje, `{"otisci":"nije popis"}`)
	losSadrzaj := sirovo(kindSadrzaj, `{"stavke":"x"}`)
	losKraj := sirovo(kindDone, `{"applied":"x"}`)
	bezPayloada := saljeSirovo("frontier bez payloada", razmjena.Envelope{Kind: kindFrontier, DeviceID: "cop-osijek"})
	djelomicna := []string{"ishod applied=0 sent=0 err=json: cannot unmarshal array into Go struct field frontierMsg.frontier of type map[string]string",
		`granica {"frontier":null,"program":"0.0.38-alfa"}`, obicno, "knjiga v1"}
	greskaDelte := "json: cannot unmarshal number into Go struct field deltaMsg.versions of type []ledger.Version"
	greskaSadrzaja := "json: cannot unmarshal string into Go struct field sadrzajMsg.stavke of type []peers.stavkaSadrzaja"
	sveBezGranice := []string{"ishod applied=1 sent=1 err=nil", `granica {"frontier":{"laptop-baranja|ocitanja/16/2026":"` + tijekV1 + `"}}`, obicno, "knjiga v1 d1"}
	prazna := func() {
		if _, err := b.db.Exec(`DELETE FROM record_versions`); err != nil {
			t.Fatal(err)
		}
	}
	tok := tokPozivatelja()
	provjeriSlucajeve(t, a, b, true, []slucaj{
		{ime: "granica", potezi: do(tok, 1, losaGranica, cita(krajVeze)), ishod: djelomicna},
		{ime: "delta", potezi: do(tok, 3, losaDelta, cita(krajVeze)), ishod: prijeDelte(greskaDelte)},
		{ime: "želje", potezi: append(zamijeni(tok, 5, loseZelje), cita(krajVeze)), ishod: nakonDelte("nil")},
		{ime: "sadržaj", potezi: do(tok, 7, losSadrzaj, cita(krajVeze)), ishod: nakonDelte(greskaSadrzaja)},
		{ime: "kraj", potezi: append(zamijeni(tok, 9, losKraj), cita(krajVeze)), ishod: nakonDelte("nil")},
		{ime: "granica bez payloada", potezi: append(zamijeni(tok, 1, bezPayloada), cita(krajVeze)), ishod: sveBezGranice},
		{ime: "granica bez payloada, čvor nema ništa", pripremi: prazna, potezi: []potez{
			cita(granicaPrazna), bezPayloada,
			cita(deltaPrazna), salje(kindDelta, deltaMsg{Versions: []ledger.Version{dolaznaD1}}),
			cita(zeljePrazne), salje(kindDone, doneMsg{}),
			cita(krajCvora(1)), cita(krajVeze),
		}, ishod: []string{"ishod applied=1 sent=0 err=nil", granicaNula, obicno, "knjiga d1"}},
	})
	tok = tokPozvanog()
	provjeriSlucajeve(t, a, b, false, []slucaj{
		{ime: "granica", potezi: do(tok, 0, losaGranica, cita(krajVeze)), ishod: djelomicna},
		{ime: "delta", potezi: do(tok, 2, losaDelta, cita(krajVeze)), ishod: prijeDelte(greskaDelte)},
		{ime: "želje", potezi: append(zamijeni(tok, 4, loseZelje), cita(krajVeze)), ishod: nakonDelte("nil")},
		{ime: "sadržaj", potezi: do(tok, 6, losSadrzaj, cita(krajVeze)), ishod: nakonDelte(greskaSadrzaja)},
		{ime: "kraj", potezi: append(zamijeni(tok, 8, losKraj), cita(krajVeze)), ishod: nakonDelte("nil")},
		{ime: "granica bez payloada", potezi: append(zamijeni(tok, 0, bezPayloada), cita(krajVeze)), ishod: sveBezGranice},
	})
}

// Knjiga ili pretplate čvora ne čitaju se: prije razgovora čvor ne pošalje
// ništa, a kad delta ne može nastati, pošalje samo granicu. Greška baze
// vraća se neomotana, s granicom druge strane kad je stigla.
func TestTijekRazgovoraGreskaBaze(t *testing.T) {
	a, b := cistiPar(t)
	izvrsi := func(upiti ...string) func() {
		return func() {
			for _, u := range upiti {
				if _, err := b.db.Exec(u); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	bezKnjige := izvrsi(`ALTER TABLE record_versions RENAME TO knjiga_skrivena`)
	vratiKnjigu := izvrsi(`ALTER TABLE knjiga_skrivena RENAME TO record_versions`)
	bezPretplata := func() {
		b.SetWantsAll(false)
		izvrsi(`ALTER TABLE subscriptions RENAME TO pretplate_skrivene`)()
	}
	vratiPretplate := func() {
		izvrsi(`ALTER TABLE pretplate_skrivene RENAME TO subscriptions`)()
		b.SetWantsAll(true)
	}
	// granica se čita, a delta ne: knjiga ima samo stupce granice
	samoGranica := izvrsi(`ALTER TABLE record_versions RENAME TO knjiga_skrivena`,
		`CREATE TABLE record_versions (version_id TEXT, node_id TEXT, channel TEXT)`,
		`INSERT INTO record_versions VALUES ('`+tijekV1+`', 'laptop-baranja', 'ocitanja/16/2026')`)
	vratiCijelu := izvrsi(`DROP TABLE record_versions`, `ALTER TABLE knjiga_skrivena RENAME TO record_versions`)
	greskaKnjige := "ishod applied=0 sent=0 err=SQL logic error: no such table: record_versions (1)"
	greskaDelte := "ishod applied=0 sent=0 err=greška pri čitanju verzija: SQL logic error: no such column: entity (1)"
	for _, p := range []struct {
		ime            string
		pozivatelj     bool
		pokvari, vrati func()
		potezi         []potez
		ishod          []string
	}{
		{"bez knjige", true, bezKnjige, vratiKnjigu, []potez{cita(krajVeze)},
			[]string{greskaKnjige, granicaNula, obicno, "knjiga se ne čita"}},
		{"bez knjige", false, bezKnjige, vratiKnjigu, []potez{salje(kindFrontier, granicaDrugog), cita(krajVeze)},
			[]string{greskaKnjige, granicaNula, obicno, "knjiga se ne čita"}},
		{"bez pretplata", true, bezPretplata, vratiPretplate, []potez{cita(krajVeze)},
			[]string{"ishod applied=0 sent=0 err=SQL logic error: no such table: subscriptions (1)", granicaNula, obicno, "knjiga v1"}},
		{"bez pretplata", false, bezPretplata, vratiPretplate, []potez{salje(kindFrontier, granicaDrugog), cita(krajVeze)},
			[]string{"ishod applied=0 sent=0 err=SQL logic error: no such table: subscriptions (1)", granicaNula, obicno, "knjiga v1"}},
		{"delta ne nastane", true, samoGranica, vratiCijelu, []potez{cita(granicaV1), salje(kindFrontier, granicaDrugog), cita(krajVeze)},
			[]string{greskaDelte, granicaDrugogNepomaknuta, obicno, "knjiga v1"}},
		// tuđa delta ostaje nepročitana
		{"delta ne nastane", false, samoGranica, vratiCijelu, []potez{salje(kindFrontier, granicaDrugog), cita(granicaV1),
			salje(kindDelta, deltaMsg{Versions: []ledger.Version{dolaznaD1}}), cita(krajVeze)},
			[]string{greskaDelte, granicaDrugogNepomaknuta, obicno, "knjiga v1"}},
	} {
		samoV1(t, b)
		p.pokvari()
		provjeriTijek(t, b, a, uloga(p.pozivatelj)+", "+p.ime, razgovor{pozivatelj: p.pozivatelj, potezi: p.potezi}, p.ishod...)
		p.vrati()
	}
}
