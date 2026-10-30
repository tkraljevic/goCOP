package peers

import (
	"bytes"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// oznakaVremena je oznaka verzije ispravnog oblika s vremenom t i rednim
// brojem n na kraju
func oznakaVremena(t time.Time, n int) string {
	var ms [8]byte
	binary.BigEndian.PutUint64(ms[:], uint64(t.UnixMilli()))
	return fmt.Sprintf("%x-%x-7000-8000-%012d", ms[2:6], ms[6:8], n)
}

// verzijaProbe je verzija zapisa u zajedničkom kanalu
func verzijaProbe(id, autor, zapis string) ledger.Version {
	return ledger.Version{VersionID: id, Entity: "probni", EntityID: zapis, NodeID: autor,
		Payload: json.RawMessage(`{"cm":1}`), CreatedAt: time.Now().UTC(), SchemaVersion: 1}
}

// primljeneProbe su verzije koje druga strana (autor) pošalje računalu
// ovaj: dvije koje se upisuju i pet koje se ne upisuju
func primljeneProbe(autor, ovaj string) (dobre, lose []ledger.Version) {
	sada := time.Now()
	dobre = []ledger.Version{
		verzijaProbe(oznakaVremena(sada.Add(-time.Minute), 1), autor, "prava"),
		verzijaProbe(oznakaVremena(sada.Add(-time.Hour), 2), ovaj, "vlastita-starija"),
	}
	lose = []ledger.Version{
		verzijaProbe("~", autor, "tilda"),
		verzijaProbe("ffffffff-ffff-7fff-bfff-ffffffffffff", autor, "najkasnija"),
		verzijaProbe(strings.ToUpper(oznakaVremena(sada, 3)), autor, "velika-slova"),
		verzijaProbe(oznakaVremena(sada.Add(time.Hour), 4), autor, "za-sat"),
		verzijaProbe(oznakaVremena(sada.Add(time.Minute), 5), ovaj, "vlastita-unaprijed"),
		// isti ključ granice kao autor u kanalu očitanja (ledger.FrontierKey)
		verzijaProbe(oznakaVremena(sada.Add(-5*time.Hour), 6), autor+"|ocitanja/16/2026", "autor-s-kanalom"),
	}
	return dobre, lose
}

// snimacDnevnika pamti retke dnevnika; piše se iz gorutina razmjene, pa
// ide pod bravom
type snimacDnevnika struct {
	mu    sync.Mutex
	retci []string
}

func (d *snimacDnevnika) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.retci = append(d.retci, string(p))
	return len(p), nil
}

// sa su retci koji sadrže sve zadane tekstove
func (d *snimacDnevnika) sa(tekstovi ...string) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []string
	for _, r := range d.retci {
		ima := true
		for _, t := range tekstovi {
			ima = ima && strings.Contains(r, t)
		}
		if ima {
			out = append(out, r)
		}
	}
	return out
}

// snimiDnevnik preusmjeri dnevnik u snimač do kraja testa
func snimiDnevnik(t *testing.T) *snimacDnevnika {
	t.Helper()
	d := &snimacDnevnika{}
	stari := log.Writer()
	log.SetOutput(d)
	t.Cleanup(func() { log.SetOutput(stari) })
	return d
}

// uKnjizi su zapisi verzija koje knjiga čvora drži, redom
func uKnjizi(t *testing.T, s *Service, vv []ledger.Version) string {
	t.Helper()
	ids := make([]string, len(vv))
	for i, v := range vv {
		ids[i] = v.VersionID
	}
	nadjene, err := s.rec.Verzije(ctxRub, ids)
	if err != nil {
		t.Fatal(err)
	}
	var zapisi []string
	for _, v := range nadjene {
		zapisi = append(zapisi, v.EntityID)
	}
	return strings.Join(zapisi, ",")
}

// saljeDeltu glumi drugu stranu razgovora koja pošalje zadanu deltu i
// završi bez sadržaja; zove je ili je pozvana
func saljeDeltu(zove bool, delta []ledger.Version) skripta {
	return func(t *testing.T, c *razmjena.Conn, _ func()) {
		if zove {
			posaljiPoruku(t, c, kindFrontier, praznaGranica)
			primiPoruku(t, c, kindFrontier)
			posaljiPoruku(t, c, kindDelta, deltaMsg{Versions: delta})
			primiPoruku(t, c, kindDelta)
			posaljiPoruku(t, c, kindDone, doneMsg{})
			primiPoruku(t, c, kindDone)
			return
		}
		primiPoruku(t, c, kindFrontier)
		posaljiPoruku(t, c, kindFrontier, praznaGranica)
		primiPoruku(t, c, kindDelta)
		posaljiPoruku(t, c, kindDelta, deltaMsg{Versions: delta})
		primiPoruku(t, c, kindZelje)
		posaljiPoruku(t, c, kindDone, doneMsg{})
		primiPoruku(t, c, kindDone)
	}
}

// Razmjena ne upisuje verziju čija oznaka nije ispravnog oblika, čije je
// vrijeme predaleko u budućnosti, ni verziju s imenom ovog računala i
// vremenom ispred njegova sata. Razgovor zbog njih ne pada, ispravne iz iste
// delte se upišu, a granica za autora ostane gdje jest, pa njegova nova
// verzija sljedećim razgovorom stigne. Za obje uloge.
func TestRazmjenaNeUpisujeNeispravnuOznaku(t *testing.T) {
	for _, laptopZove := range []bool{true, false} {
		a, b := izdavacIPrimatelj(t)
		dnevnik := snimiDnevnik(t)
		dobre, lose := primljeneProbe(a.node.ID, b.node.ID)
		delta := append(append([]ledger.Version(nil), lose...), dobre...)
		var o ishodRazgovora
		if laptopZove {
			o = nazoviSkriptu(t, b, a, false, saljeDeltu(false, delta))
		} else {
			o = primiSkriptu(t, b, a, saljeDeltu(true, delta))
		}
		if o.err != nil || o.applied != len(dobre) {
			t.Fatalf("laptop zove %v: upisano %d, %v", laptopZove, o.applied, o.err)
		}
		if got := uKnjizi(t, b, delta); got != "vlastita-starija,prava" {
			t.Errorf("laptop zove %v: u knjizi %s", laptopZove, got)
		}
		granica, err := b.rec.Frontier(ctxRub)
		if err != nil || !ledger.IspravnaOznaka(granica[a.node.ID]) || granica[a.node.ID] > oznakaVremena(time.Now().Add(time.Second), 0) {
			t.Errorf("laptop zove %v: granica za autora %q, %v", laptopZove, granica[a.node.ID], err)
		}
		// tri neispravne svaka svojim retkom, a one koje kasne jednim po autoru
		if n := len(dnevnik.sa("i ne upisuje se")); n != 5 {
			t.Errorf("laptop zove %v: dnevnik %d redaka: %q", laptopZove, n, dnevnik.sa("i ne upisuje se"))
		}
		if len(dnevnik.sa(ledger.ErrVlastitaUnaprijed.Error(), "takvih u poruci 1")) != 1 || len(dnevnik.sa(ledger.ErrIzBuducnosti.Error(), "takvih u poruci 2")) != 1 ||
			len(dnevnik.sa(ledger.ErrOblikAutora.Error())) != 1 {
			t.Errorf("laptop zove %v: dnevnik %q", laptopZove, dnevnik.sa("i ne upisuje se"))
		}

		// autor zatim napiše novo, i to stigne pravom razmjenom
		upisiOcitanja(t, a, "", "novo")
		razgovaraj(t, b, a)
		if _, err := b.rec.Latest(ctxRub, "probni", "novo"); err != nil {
			t.Errorf("laptop zove %v: nova verzija autora: %v", laptopZove, err)
		}
	}
}

// Ograda prihvata koja odbije verziju iz budućnosti ne pamti je u granici
// odbijenih: ta granica ide drugoj strani, koja tada ne bi slala ništa ispod
// nje. Ispravna odbijena verzija se pamti kao i prije.
func TestOdbijenaIzBuducnostiNijeUGraniciOdbijenih(t *testing.T) {
	rz := razgovorZaRupe(t, nil, nil)
	rz.myWants = Wants{All: true}
	rz.s.Accept(odbijaStaro)
	kanal := "ocitanja/16/2024"
	staro := ledger.Version{VersionID: oznakaVremena(time.Now().Add(-time.Hour), 1), Entity: "probni", EntityID: "staro-1", NodeID: "ured", Channel: kanal}
	buduce := staro
	buduce.VersionID, buduce.EntityID = "ffffffff-ffff-7fff-bfff-ffffffffffff", "staro-2"
	if err := rz.upisiPrimljeno(ctxRub, []ledger.Version{staro, buduce}); err != nil {
		t.Fatal(err)
	}
	if got := rz.odbijeno[ledger.FrontierKey("ured", kanal)]; got != staro.VersionID {
		t.Errorf("zapamćeno odbijeno %q, očekivano %q", got, staro.VersionID)
	}
}

// Uvoz arhive knjige (.db) preskoči verzije koje razmjena ne prima, a
// ostale iz iste arhive upiše
func TestUvozArhiveNeUpisujeNeispravnuOznaku(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	const kanal = "ocitanja/16/2026"
	dobre, lose := primljeneProbe(a.node.ID, b.node.ID)
	for _, v := range append(append([]ledger.Version(nil), dobre...), lose...) {
		v.Channel = kanal
		upisiStalno(t, a, v)
	}
	put := filepath.Join(t.TempDir(), "arhiva.db")
	// izvoz nosi i vlastitu-unaprijed: s izvoznika gledano vrijeme joj je
	// unutar dopuštenog, a ime je tuđe
	if n, err := a.ExportFile(ctxRub, put, []string{kanal}); err != nil || n != len(dobre)+1 {
		t.Fatalf("izvoz: %d %v", n, err)
	}
	// arhiva koju je izvezao program koji oznake nije provjeravao
	arhiva, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range lose {
		if _, err := arhiva.Exec(`INSERT OR IGNORE INTO record_versions (version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel)
			VALUES (?, ?, ?, ?, '', 0, ?, ?, 1, ?)`, v.VersionID, v.Entity, v.EntityID, v.NodeID, string(v.Payload), v.CreatedAt, kanal); err != nil {
			t.Fatal(err)
		}
	}
	arhiva.Close()
	rep, err := b.ImportFile(ctxRub, put)
	if err != nil || rep.Versions != len(dobre)+len(lose) || rep.Applied != len(dobre) {
		t.Fatalf("uvoz: %+v %v", rep, err)
	}
	if got := uKnjizi(t, b, append(dobre, lose...)); got != "vlastita-starija,prava" {
		t.Errorf("u knjizi %s", got)
	}
}

// copSaZapisima je paket čvora s koji nosi baš zadane verzije, potpisan
// njegovim ključem: kao da ga je izdao program koji oznake ne provjerava
func copSaZapisima(t *testing.T, s *Service, kanal string, vv []ledger.Version) string {
	t.Helper()
	return copSaZapisimaIRupama(t, s, kanal, vv, nil)
}

// copSaZapisimaIRupama je copSaZapisima s rupama izdavača u manifestu
func copSaZapisimaIRupama(t *testing.T, s *Service, kanal string, vv []ledger.Version, rupe map[string]raspon) string {
	t.Helper()
	upisiOcitanja(t, s, kanal, "podloga")
	vrsta, podrucje, godina := ledger.SplitChannel(kanal)
	m, _ := izvezi(t, s, CopObuhvat{Vrsta: vrsta, AreaID: podrucje, Od: godina, Do: godina}, []string{kanal}, false)
	var zapisi bytes.Buffer
	for _, v := range vv {
		redak, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		zapisi.Write(append(redak, '\n'))
	}
	m.Zapisa, m.Sadrzaja, m.Rupe, m.Inacica = len(vv), 0, rupe, inacicaCopa(rupe)
	m.Otisak = otisakIzdanja(zapisi.Bytes(), nil)
	m.Potpis = &CopPotpis{Kljuc: base64.StdEncoding.EncodeToString(s.javni()),
		Potpis: base64.StdEncoding.EncodeToString(ed25519.Sign(s.node.key, kanonskiCop(m)))}
	manifest, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	put := filepath.Join(t.TempDir(), "rucno.cop")
	if err := os.WriteFile(put, zipCopa(t, datotekaCopa{ime: "manifest.json", b: manifest},
		datotekaCopa{ime: "zapisi.jsonl", b: zapisi.Bytes()}, datotekaCopa{ime: "sadrzaji.json", b: []byte("[]")}), 0o600); err != nil {
		t.Fatal(err)
	}
	return put
}

// Uvoz COP paketa s ispravnim potpisom preskoči verzije koje razmjena ne
// prima, a ostale iz istog paketa upiše
func TestUvozCopaNeUpisujeNeispravnuOznaku(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	const kanal = "ocitanja/16/2026"
	dobre, lose := primljeneProbe(a.node.ID, b.node.ID)
	var vv []ledger.Version
	for _, v := range append(append([]ledger.Version(nil), lose...), dobre...) {
		v.Channel = kanal
		vv = append(vv, v)
	}
	rep, err := b.UveziCop(ctxRub, copSaZapisima(t, a, kanal, vv))
	if err != nil || !rep.PotpisValjan || rep.Verzija != len(vv) || rep.Novih != len(dobre) {
		t.Fatalf("uvoz: %+v %v", rep, err)
	}
	if got := uKnjizi(t, b, vv); got != "vlastita-starija,prava" {
		t.Errorf("u knjizi %s", got)
	}
}

// Verzije koje razmjena ne prima ne idu ni iz vlastite knjige: oznaka
// krivog oblika i vrijeme predaleko u budućnosti. Ostale iz istog ključa
// idu, a svaka takva ide u dnevnik jednom.
func TestRazmjenaNeSaljeNeispravnuOznaku(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	dnevnik := brojiDnevnik(t, "ne šalje se drugim čvorovima")
	const kanal = "ocitanja/16/2026"
	dobra := oznakaVremena(time.Now().Add(-time.Minute), 1)
	upisiRedakKnjige(t, b, dobra, kanal, `{"cm":1}`)
	upisiRedakKnjige(t, b, "~", kanal, `{"cm":2}`)
	upisiRedakKnjige(t, b, oznakaVremena(time.Now().Add(time.Hour), 3), kanal, `{"cm":3}`)
	kljuc := ledger.FrontierKey(b.node.ID, kanal)
	mine, err := b.rec.Frontier(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	granica := map[string]string{}
	for k := range mine {
		if k != kljuc {
			granica[k] = "~"
		}
	}
	var stiglo deltaMsg
	if o := nazoviSkriptu(t, b, a, false, primiDeltu(granica, &stiglo)); o.err != nil || o.sent != 1 {
		t.Fatalf("razmjena: poslano %d, %v", o.sent, o.err)
	}
	if got := idoviVerzija(stiglo.Versions); got != dobra {
		t.Errorf("poslano %s", got)
	}
	// vlastita verzija iz budućnosti nije pokvarena: ne ide u dnevnik ni na
	// ploču, nego ide kad joj vrijeme dođe
	if n, zadnji := dnevnik.broj(); n != 1 || !strings.Contains(zadnji, "u knjizi ") || !strings.Contains(zadnji, ledger.ErrOblikOznake.Error()) {
		t.Errorf("dnevnik %d redaka, zadnji %q", n, zadnji)
	}
	if u := b.upozorenjeNeispravnih(); len(u) != 1 || !strings.Contains(u[0], " 1 zapis ") {
		t.Errorf("ploča: %q", u)
	}
}

// Paket se iz knjige ne izdaje s verzijom koju razmjena ne prima
func TestIzvozCopaBezNeispravneOznake(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	const kanal = "ocitanja/16/2026"
	upisiOcitanja(t, a, kanal, "r-1")
	upisiRedakKnjige(t, a, "~", kanal, `{"cm":2}`)
	vrsta, podrucje, godina := ledger.SplitChannel(kanal)
	m, put := izvezi(t, a, CopObuhvat{Vrsta: vrsta, AreaID: podrucje, Od: godina, Do: godina}, []string{kanal}, false)
	paket, err := os.ReadFile(put)
	if err != nil {
		t.Fatal(err)
	}
	_, vv, _, _, err := citajCop(paket)
	if err != nil || m.Zapisa != 1 || len(vv) != 1 || vv[0].EntityID != "r-1" {
		t.Errorf("paket: %d zapisa, %v, %v", m.Zapisa, vv, err)
	}
}
