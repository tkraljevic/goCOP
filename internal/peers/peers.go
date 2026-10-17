// Paket peers je goCOP-ova strana sinkronizacije: identitet ovog čvora,
// popis poznatih čvorova i razmjena verzija iz knjige preko sloja razmjene.
//
// Svaki čvor je punopravna kopija; nijedan nije izvor istine. Dva čvora
// razmijene "dokle znaju" (frontier po autoru) i svaki pošalje drugome
// verzije koje ovaj još nema — i one tuđe koje je sam primio, pa promjena
// stiže i zaobilazno. Primanje je samo dodavanje, pa je i ponovljeno
// primanje bezopasno.
package peers

import (
	"cmp"
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gocop/internal/razmjena"

	"gocop/internal/ledger"
	"gocop/internal/sadrzaj"
)

// Protocol imenuje goCOP mrežu: čvor druge aplikacije koja se služi istim
// slojem razmjene ne upari se s goCOP-om i ne odgovara na njegove pozive.
const Protocol = "gocop"

// Portovi kao obitelj: razmjena, uparivanje, pronalaženje. Namjerno pomaknuti
// od 4610–4612, da program koji ih zauzima može raditi na istom stroju.
const (
	DefaultExchangePort  = 4710
	DefaultPairPort      = 4711
	DefaultDiscoveryPort = 4712
)

// KeyFileName je datoteka s ključem čvora, UZ bazu a ne u njoj — kopija
// baze ne smije naslijediti identitet stroja s kojeg je kopirana.
const KeyFileName = "node-key"

// EntityPeers je naziv entiteta u knjizi verzija
const EntityPeers = "peers"

// Peer je poznati čvor. Identitet je javni ključ; adrese se mijenjaju.
type Peer struct {
	NodeID       string     `json:"node_id"`
	Name         string     `json:"name"`
	PublicKey    string     `json:"public_key"`
	Addresses    []string   `json:"addresses"`
	IsBootstrap  bool       `json:"is_bootstrap"` // stalno izložen na mreži (domena)
	LastSeen     *time.Time `json:"last_seen,omitempty"`
	LastSync     *time.Time `json:"last_sync,omitempty"`
	LastSyncNote string     `json:"last_sync_note,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// Node je ovaj čvor
type Node struct {
	ID      string
	Name    string
	Version string
	Dir     string // mapa uz bazu: ključ čvora, ključ mreže
	key     ed25519.PrivateKey
}

// PublicKey vraća javni ključ čvora u obliku za prikaz i pohranu
func (n *Node) PublicKey() string {
	return razmjena.PublicKeyString(n.key.Public().(ed25519.PublicKey))
}

// identity je ono što ovaj čvor kaže o sebi pri uparivanju. Port razmjene
// putuje ovdje jer druga strana vidi samo odakle je veza došla — a
// uparivanje ide na jednom portu, razmjena na drugom.
func (s *Service) identity() razmjena.Identity {
	return razmjena.Identity{
		Protocol: Protocol,
		DeviceID: s.node.ID,
		Name:     s.node.Name,
		Version:  s.node.Version,
		Meta: map[string]string{
			"schema_version": fmt.Sprint(ledger.SchemaVersion),
			"exchange_port":  fmt.Sprint(s.ports.Exchange),
		},
	}
}

// LoadNode učitava ili stvara ključ čvora uz bazu
func LoadNode(dbPath, nodeID, name, version string) (*Node, error) {
	key, err := razmjena.LoadOrCreateKey(filepath.Join(filepath.Dir(dbPath), KeyFileName))
	if err != nil {
		return nil, fmt.Errorf("ključ čvora: %w", err)
	}
	return &Node{ID: nodeID, Name: name, Version: version, Dir: filepath.Dir(dbPath), key: key}, nil
}

// Ports su portovi na kojima ovaj čvor radi
type Ports struct {
	Exchange  int
	Pair      int
	Discovery int
}

// Service veže identitet, popis čvorova i knjigu verzija u sinkronizaciju
type Service struct {
	// skracene: razgovori u kojima je delta skraćena zbog veličine, pa
	// ima još; razmjena tada nastavlja odmah (syncPeers), ne za pet minuta
	skracene sync.Map // *razmjena.Conn → bool
	josIma   sync.Map // nodeID → bool, iz zadnjeg SyncWith
	db       *sql.DB
	rec      *ledger.Recorder
	node     *Node
	ports    Ports

	mu      sync.Mutex
	pending *razmjena.PairResult // uparivanje koje čeka odluku čovjeka
	// pozvao: uparivanje koje čeka odluku ovaj je čvor započeo pozivom
	// (DialPair), a nije čekao poziv (StartListening)
	pozvao  bool
	pairErr error
	waiting context.CancelFunc // aktivni Listen, ako čeka
	// cekanje je redni broj zadnjeg čekanja (StartListening): po njemu
	// prekinuto čekanje zna da je u međuvremenu počelo novo
	cekanje uint64

	// mreža kojoj čvor pripada; nil dok ga netko ne primi ili dok je ne osnuje
	network  *razmjena.NetworkKey
	joinedAt time.Time

	// onApplied osvježava površinu iz primljenih verzija; zna ga sloj
	// koji zna entitete (repository.ApplyVersions)
	onApplied func(ctx context.Context, versions []ledger.Version) error

	// accept propušta ono što ovaj čvor uopće želi držati. Terenski uređaj
	// ne treba stoljeće očitanja svih letvi u zemlji, pa ono što ovdje
	// otpadne ne ulazi ni u knjigu ni na površinu.
	accept func(ledger.Version) bool

	// spremiste sadržaja: PDF-ovi i slike koje razmjena prenosi po otisku,
	// nakon verzija, prema razini pretplate
	spremiste *sadrzaj.Spremiste

	// tunel prima razmjenu kroz web sučelje (WebSocket), za čvorove koji
	// izvana smiju samo na web
	tunel *razmjena.Tunel

	// uloge ovog računala za mrežu (preuzima vodostaje, izdaje prognozu)
	uloge           atomic.Pointer[Uloge]
	naPromjenuUloga func(Uloge)

	// izdanje programa i puna oznaka gradnje, za razmjenu (PostaviProgram)
	program, gradnja string
	// čvorovi čija je drukčija inačica programa već zapisana u dnevnik
	javljenaInacica sync.Map
	// zapisi čvorova čiji je ključ drukčiji od članstva već zapisani u
	// dnevnik (javiDrugiKljuc)
	javljenKljuc sync.Map
	// dva računala s valjanim članstvom pod istim imenom, već zapisana u
	// dnevnik (javiSukob)
	javljenSukob sync.Map
	// neispravne su version_id verzija iz vlastite knjige koje razmjena ne
	// šalje (zaSlanje); broj se vidi na ploči. Primljene neispravne su u
	// preskocene.
	neispravne sync.Map
	// preskocene su primljene neispravne verzije (ledger.Ispravna) koje su
	// već zapisane u dnevnik (preskociNeispravne); na ploči se ne broje
	preskocene sync.Map

	every    time.Duration // razmak automatske sinkronizacije (0 = isključena)
	autoOn   bool
	wantsAll bool // prati sve kanale (uredski čvor)
	// autoCtx je kontekst automatske sinkronizacije (RunAutoSync); nil dok
	// nije uključena. U njemu ide i razmjena odmah nakon uparivanja.
	autoCtx context.Context
	// nakonUparenja su razmjene koje je uparivanje pokrenulo u pozadini
	nakonUparenja sync.WaitGroup

	// tailscaleAdrese čita Tailscale adrese ovog računala; nil znači
	// zadaneTailscaleAdrese. Test njime daje čvoru izmišljene adrese.
	tailscaleAdrese func() []netip.Addr
	// dial otvara TCP vezu do drugog čvora (razmjena i uparivanje); nil je
	// obična veza. Test njime vezu preruši, npr. u vezu preko Tailscalea.
	dial func(ctx context.Context, adresa string) (net.Conn, error)
	// slusaj čeka poziv za uparivanje (StartListening); nil je
	// razmjena.Listen na portu uparivanja. Test njime čekanje drži bez mreže.
	slusaj func(ctx context.Context) (*razmjena.PairResult, error)
	// katalog su vrste zapisa koje čvor javlja u granici (mojKatalog); nil
	// je ledger.Katalog. Popis poznatih entiteta u ledgeru zajednički je
	// cijelom procesu i ne vraća se, pa test katalog daje ovuda.
	katalog func() map[string]int
	// brava upisa primljenog (upis.go); nastaje pri prvoj upotrebi
	upis       chan struct{}
	upisJednom sync.Once

	// bezTailscalea: [sync] tailscale = false (vidi PostaviTailscale)
	bezTailscalea atomic.Bool
	// čvorovi za koje je odbijena razmjena na Tailscale adresi već zapisana u dnevnik
	javljenTailscale sync.Map

	// odbijene je ono što je ovaj čvor odbio od drugih, po pošiljatelju
	// (granica_odbijenih.go); stanjePrihvata kaže o čemu ovisi što accept
	// odbija (PostaviStanjePrihvata), a satOdbijenih je njezin sat: nil je
	// time.Now, test njime pomiče vrijeme
	odbijene       pamcenjeOdbijenih
	stanjePrihvata func() (narastaj uint64, rub time.Time)
	satOdbijenih   func() time.Time
	// uvozi su narastaj uvoza i što su druga računala nakon uvoza dobila
	// (granica_nakon_uvoza.go)
	uvozi pamcenjeUvoza
	// mogucnosti su oznake koje čvor javlja u granici (mojeMogucnosti); nil
	// je razmjena.NaseMogucnosti. Test njime glumi čvor bez neke oznake.
	mogucnosti func() []string
	// rupeMijenja raste kad nekom ključu u zapisu rupa naraste raspon za
	// koji ovo računalo ne jamči (rupe.go); praviloPrihvata je pravilo
	// čuvanja (PostaviPraviloPrihvata)
	rupeMijenja     atomic.Uint64
	praviloPrihvata func() (mjeseci int, pracene []string)
}

func (s *Service) autoSync() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.autoOn
}

func NewService(db *sql.DB, rec *ledger.Recorder, node *Node, ports Ports) (*Service, error) {
	s := &Service{db: db, rec: rec, node: node, ports: ports, tunel: razmjena.NoviTunel()}
	if err := s.loadNetwork(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) Node() *Node  { return s.node }
func (s *Service) Ports() Ports { return s.ports }

// PostaviProgram pamti izdanje programa (npr. 0.0.24-alfa) i punu oznaku
// gradnje; oboje putuje drugim čvorovima u razmjeni, da ploča pokaže tko
// radi na starijem programu
func (s *Service) PostaviProgram(program, gradnja string) {
	s.mu.Lock()
	s.program, s.gradnja = program, gradnja
	s.mu.Unlock()
}

// Program vraća izdanje i punu oznaku gradnje ovog čvora
func (s *Service) Program() (program, gradnja string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.program, s.gradnja
}

// ---------- poznati čvorovi ----------

// ListPeers vraća druge čvorove. Vlastiti zapis (kad ga razmjena vrati)
// nije partner za razmjenu, pa se ne navodi; do njega vodi SelfPeer.
func (s *Service) ListPeers(ctx context.Context) ([]Peer, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT node_id, name, public_key, addresses, is_bootstrap, last_seen, last_sync, last_sync_note, created_at
		FROM peers WHERE node_id <> ? ORDER BY is_bootstrap DESC, name COLLATE NOCASE`, s.node.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Peer
	for rows.Next() {
		p, err := scanPeer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SelfPeer je zapis ovog čvora kakav putuje drugima: identitet iz ključa,
// javne adrese i izloženost iz baze kad su upisane. Njime ured objavljuje
// domenu na kojoj ga svi mogu naći.
func (s *Service) SelfPeer(ctx context.Context) (Peer, error) {
	self := Peer{NodeID: s.node.ID, Name: s.node.Name, PublicKey: s.node.PublicKey(), Addresses: []string{}}
	stored, err := s.GetPeer(ctx, s.node.ID)
	if err != nil {
		return self, err
	}
	if stored != nil {
		self.Addresses, self.IsBootstrap, self.CreatedAt = stored.Addresses, stored.IsBootstrap, stored.CreatedAt
	}
	return self, nil
}

// PublicAddress dodaje ili miče javnu adresu čvora (domenu ili IP s portom
// razmjene) i označava ga stalno izloženim dok ima bar jednu. Vrijedi i za
// ovaj čvor: tako ured objavi svoju domenu, a zapis stigne svima razmjenom.
func (s *Service) PublicAddress(ctx context.Context, nodeID, add, remove string) (*Peer, error) {
	var p Peer
	if nodeID == s.node.ID {
		self, err := s.SelfPeer(ctx)
		if err != nil {
			return nil, err
		}
		p = self
	} else {
		known, err := s.GetPeer(ctx, nodeID)
		if err != nil {
			return nil, err
		}
		if known == nil {
			return nil, fmt.Errorf("čvor %s nije poznat — prvo ga uparite", nodeID)
		}
		p = *known
	}
	add, remove = strings.TrimSpace(add), strings.TrimSpace(remove)
	if add != "" {
		if err := s.dodajAdresu(&p, add); err != nil {
			return nil, err
		}
	}
	if remove != "" {
		var keep []string
		for _, a := range p.Addresses {
			host, _, splitErr := net.SplitHostPort(a)
			if a == remove || (splitErr == nil && host == remove) {
				continue
			}
			keep = append(keep, a)
		}
		p.Addresses = dedupe(keep)
	}
	p.IsBootstrap = len(p.Addresses) > 0 && (p.IsBootstrap || add != "")
	if err := s.SavePeer(ctx, p); err != nil {
		return nil, err
	}
	return &p, nil
}

// dodajAdresu dodaje čvoru adresu koju je upisao čovjek. Tailscale adresa
// se ne prima: popis adresa dobivaju svi čvorovi mreže, a Tailscale je
// osobna mreža. Ovaj čvor je zapamti kad se preko nje upari (tailscale.go).
func (s *Service) dodajAdresu(p *Peer, add string) error {
	if jeTailscaleAdresa(add) {
		return fmt.Errorf("Tailscale adresa se ne upisuje među adrese čvora: popis adresa dobivaju svi čvorovi mreže. Da dva vaša čvora s Tailscaleom razmjenjuju preko njega, uparite ih jednom preko Tailscale adrese; adresa tada ostaje samo na ovom računalu")
	}
	if razmjena.JeAdresaTunela(add) {
		// razmjena kroz web tunel (Cloudflare): https://domena
		t, err := razmjena.NormalizirajTunel(add)
		if err != nil {
			return err
		}
		p.Addresses = dedupe(append(p.Addresses, t))
		return nil
	}
	if strings.ContainsAny(add, " /\\") {
		return fmt.Errorf("adresa je domena ili IP, po želji s portom (npr. cop-osijek.com ili 10.0.0.5:4710), ili https://domena za razmjenu kroz web tunel")
	}
	p.Addresses = dedupe(append(p.Addresses, withPort(add, s.exchangePortOf(*p))))
	return nil
}

// exchangePortOf je port razmjene čvora za adresu bez porta: vlastiti port
// za ovaj čvor, port iz već poznate adrese za tuđi, inače zadani
func (s *Service) exchangePortOf(p Peer) string {
	if p.NodeID == s.node.ID {
		return fmt.Sprint(s.ports.Exchange)
	}
	for _, a := range p.Addresses {
		if _, port, err := net.SplitHostPort(a); err == nil && port != "" {
			return port
		}
	}
	return fmt.Sprint(DefaultExchangePort)
}

func (s *Service) GetPeer(ctx context.Context, nodeID string) (*Peer, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT node_id, name, public_key, addresses, is_bootstrap, last_seen, last_sync, last_sync_note, created_at
		FROM peers WHERE node_id = ?`, nodeID)
	p, err := scanPeer(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func scanPeer(scanner interface{ Scan(...any) error }) (Peer, error) {
	var p Peer
	var addrs string
	var bootstrap int
	var lastSeen, lastSync sql.NullTime
	err := scanner.Scan(&p.NodeID, &p.Name, &p.PublicKey, &addrs, &bootstrap, &lastSeen, &lastSync, &p.LastSyncNote, &p.CreatedAt)
	if err != nil {
		return p, err
	}
	_ = json.Unmarshal([]byte(addrs), &p.Addresses)
	// Tailscale adresu u zapis mogao je donijeti stariji program; ovaj je ne
	// pokazuje, ne zove i ne prenosi dalje
	p.Addresses = bezTailscalea(p.Addresses)
	p.IsBootstrap = bootstrap != 0
	if lastSeen.Valid {
		t := lastSeen.Time
		p.LastSeen = &t
	}
	if lastSync.Valid {
		t := lastSync.Time
		p.LastSync = &t
	}
	return p, nil
}

// SavePeer upisuje ili osvježava poznati čvor i ostavlja verziju u knjizi
func (s *Service) SavePeer(ctx context.Context, p Peer) error {
	if p.NodeID == "" || p.PublicKey == "" {
		return fmt.Errorf("čvor bez identifikatora ili ključa")
	}
	if _, err := razmjena.ParsePublicKey(p.PublicKey); err != nil {
		return fmt.Errorf("neispravan javni ključ čvora: %w", err)
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}
	// Zapis čvora putuje knjigom svima, a Tailscale je osobna mreža: njegove
	// adrese ne ulaze ni u zapis ni u knjigu (vidi tailscale.go)
	p.Addresses = bezTailscalea(p.Addresses)
	addrs, _ := json.Marshal(dedupe(p.Addresses))

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Verziju zaslužuje samo ono što drugi čvorovi trebaju znati: naziv,
	// ključ, adrese, izloženost. Bilješke "zadnji put viđen / sinkroniziran"
	// opisuju odnos DVA čvora i ne putuju — inače bi svaka razmjena rađala
	// verziju koja se u sljedećoj razmjeni vraća, u beskraj.
	var prevName, prevKey, prevAddrs string
	var prevBootstrap int
	err = tx.QueryRowContext(ctx, `SELECT name, public_key, addresses, is_bootstrap FROM peers WHERE node_id = ?`, p.NodeID).
		Scan(&prevName, &prevKey, &prevAddrs, &prevBootstrap)
	isNew := errors.Is(err, sql.ErrNoRows)
	if err != nil && !isNew {
		return err
	}
	if !isNew && prevKey != p.PublicKey {
		// isto ime, drugo računalo (ili isto računalo s novim ključem):
		// ključ se ne prepisuje; stari čvor se najprije zaboravi
		return fmt.Errorf("%w: %s", ErrIstoImeDrugiKljuc, p.NodeID)
	}
	identityChanged := isNew || prevName != p.Name || prevKey != p.PublicKey ||
		prevAddrs != string(addrs) || prevBootstrap != boolInt(p.IsBootstrap)

	_, err = tx.ExecContext(ctx, `
		INSERT INTO peers (node_id, name, public_key, addresses, is_bootstrap, last_seen, last_sync, last_sync_note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			name = excluded.name, public_key = excluded.public_key, addresses = excluded.addresses,
			is_bootstrap = excluded.is_bootstrap, last_seen = excluded.last_seen,
			last_sync = excluded.last_sync, last_sync_note = excluded.last_sync_note
	`, p.NodeID, p.Name, p.PublicKey, string(addrs), boolInt(p.IsBootstrap), utcIliNull(p.LastSeen), utcIliNull(p.LastSync), p.LastSyncNote, p.CreatedAt.UTC())
	if err != nil {
		return fmt.Errorf("greška pri spremanju čvora %s: %w", p.NodeID, err)
	}
	if identityChanged {
		shared := p
		shared.LastSeen, shared.LastSync, shared.LastSyncNote = nil, nil, ""
		if _, err := s.rec.Record(ctx, tx, EntityPeers, p.NodeID, shared); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ErrIstoImeDrugiKljuc: poznati čvor tog imena ima drugi ključ
var ErrIstoImeDrugiKljuc = errors.New("ime već ima drugo računalo u mreži (drugi ključ)")

// provjeriImeDrugog odbija uparivanje s čvorom koji nosi ime ovog čvora ili
// ime poznatog čvora, odnosno člana mreže, s drugim ključem. Ključ člana je
// iz članstva koje za to ime vrijedi (kljucClana): krivotvoreno, isteklo ili
// opozvano članstvo ime ne zauzima.
func (s *Service) provjeriImeDrugog(ctx context.Context, id string, kljuc ed25519.PublicKey) error {
	if id == s.node.ID {
		return fmt.Errorf("drugo računalo nosi isto ime kao ovo (%s); svaki čvor treba svoje ime: drugo računalo instalirajte ponovno s drugim imenom", id)
	}
	k := razmjena.PublicKeyString(kljuc)
	poruka := fmt.Errorf("ime %s već ima drugo računalo u mreži; izaberite drugo ime, a ako je to isto računalo s novim ključem, najprije ga zaboravite i opozovite mu članstvo", id)
	// greška čitanja nije "nema sukoba": tada se ime ne prihvaća
	p, err := s.GetPeer(ctx, id)
	if err != nil {
		return fmt.Errorf("ime %s se ne može provjeriti: %w", id, err)
	}
	if p != nil && p.PublicKey != k {
		return poruka
	}
	clanski, err := s.kljucClana(ctx, id)
	if err != nil && !errors.Is(err, errNijeClan) {
		return fmt.Errorf("ime %s se ne može provjeriti: %w", id, err)
	}
	if err == nil && !clanski.Equal(kljuc) {
		return poruka
	}
	return nil
}

// ForgetPeer uklanja čvor s popisa; u knjizi ostaje arhiviran
func (s *Service) ForgetPeer(ctx context.Context, nodeID string) error {
	// granica odbijenih zaboravi se cijela, i kad brisanje ne uspije:
	// zaboraviti previše košta samo jedno ponovno slanje
	defer s.zaboraviOdbijene()
	p, err := s.GetPeer(ctx, nodeID)
	if err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("čvor %s nije poznat", nodeID)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM peers WHERE node_id = ?`, nodeID); err != nil {
		return err
	}
	// Spomenik putuje svima, kao i verzija iz SavePeer: bilješke o razmjeni
	// opisuju odnos dvaju čvorova i ne putuju. U bilješci o grešci zna stajati
	// i adresa koju svi ne smiju vidjeti (npr. Tailscale).
	p.LastSeen, p.LastSync, p.LastSyncNote = nil, nil, ""
	if _, err := s.rec.Archive(ctx, tx, EntityPeers, nodeID, p); err != nil {
		return err
	}
	return tx.Commit()
}

// peerByKey je poznati čvor koji je u razmjeni dokazao ključ pub, po istoj
// odluci po kojoj su ga vrata razmjene pustila (oznakaDokazanog): po
// članstvu, ili po potvrdi iz certifikata (vj) dok članstvo ovdje još
// nije. Ključ u zapisu čvora to ne kaže, jer zapis piše i šalje bilo koji
// član (dva zapisa mogu nositi isti ključ). nil kad računalo nije član ili
// ovaj čvor nema njegov zapis.
func (s *Service) peerByKey(ctx context.Context, pub ed25519.PublicKey, vj []byte) (*Peer, error) {
	// uz grešku je oznaka prazna
	id, err := s.oznakaDokazanog(ctx, pub, vj)
	if id == "" {
		return nil, err
	}
	return s.GetPeer(ctx, id)
}

// ---------- pronalaženje na lokalnoj mreži ----------

// Announce odgovara na probe drugih goCOP čvorova dok ctx traje
func (s *Service) Announce(ctx context.Context) error {
	return razmjena.Announce(ctx, Protocol, s.ports.Discovery, func() razmjena.Beacon {
		b := razmjena.Beacon{DeviceID: s.node.ID, Name: s.node.Name, ExchangePort: s.ports.Exchange}
		s.mu.Lock()
		if s.waiting != nil {
			b.PairPort = s.ports.Pair
		}
		s.mu.Unlock()
		return b
	})
}

// Discovered je čvor viđen na lokalnoj mreži, uz naznaku je li već poznat
type Discovered struct {
	razmjena.Found
	Known bool `json:"known"`
}

// Discover proba lokalnu mrežu i vraća goCOP čvorove koji su se javili
func (s *Service) Discover(ctx context.Context, timeout time.Duration) ([]Discovered, error) {
	found, err := razmjena.Discover(Protocol, timeout, s.ports.Discovery)
	if err != nil {
		return nil, err
	}
	var out []Discovered
	for _, f := range found {
		if f.DeviceID == s.node.ID {
			continue
		}
		known, _ := s.GetPeer(ctx, f.DeviceID)
		if known != nil {
			// osvježi adresu — laptop se preselio
			now := time.Now().UTC()
			known.LastSeen = &now
			known.Addresses = dedupe(append([]string{withPort(f.Addr, fmt.Sprint(f.ExchangePort))}, known.Addresses...))
			_ = s.SavePeer(ctx, *known)
		}
		out = append(out, Discovered{Found: f, Known: known != nil})
	}
	return out, nil
}

// ---------- uparivanje ----------

// PairStatus je stanje uparivanja za ekran
type PairStatus struct {
	Waiting  bool           `json:"waiting"` // ovaj čvor čeka da ga netko nazove
	Pending  bool           `json:"pending"` // netko se javio, čeka odluku
	SAS      string         `json:"sas,omitempty"`
	Peer     razmjena.Hello `json:"peer,omitempty"`
	PeerHost string         `json:"peer_host,omitempty"`
	Error    string         `json:"error,omitempty"`
}

func (s *Service) PairStatus() PairStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := PairStatus{Waiting: s.waiting != nil, Pending: s.pending != nil}
	if s.pending != nil {
		st.SAS, st.Peer, st.PeerHost = s.pending.SAS, s.pending.Peer, s.pending.PeerHost()
	}
	if s.pairErr != nil {
		st.Error = s.pairErr.Error()
	}
	return st
}

// StartListening čeka da drugi čvor nazove ovaj; kad se javi, uparivanje
// čeka odluku čovjeka (PairStatus.Pending + SAS)
func (s *Service) StartListening() error {
	s.mu.Lock()
	if s.waiting != nil || s.pending != nil {
		s.mu.Unlock()
		return fmt.Errorf("uparivanje je već u tijeku")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	s.waiting = cancel
	s.pairErr = nil
	s.cekanje++
	moje := s.cekanje
	s.mu.Unlock()

	go func() {
		res, err := s.slusajUparivanje(ctx)
		s.zavrsiCekanje(moje, res, err)
	}()
	return nil
}

// zavrsiCekanje bilježi kako je završilo čekanje s rednim brojem moje.
// Prekinuto čekanje koje je u međuvremenu zamijenilo novo ne dira ništa:
// inače bi obrisalo zapis novog čekanja, pa ga StopListening ne bi mogao
// prekinuti, a čvor ne bi javljao da čeka. Poziv koji je takvo čekanje
// dočekalo se odbija.
func (s *Service) zavrsiCekanje(moje uint64, res *razmjena.PairResult, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if moje != s.cekanje {
		odbijPoziv(res)
		return
	}
	s.waiting = nil
	if err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			s.pairErr = err
		}
		return
	}
	s.pending, s.pozvao = res, false
}

// odbijPoziv javlja računalu koje je nazvalo da je uparivanje odbijeno i
// zatvara vezu; bez toga bi ono čekalo dvije minute da veza istekne. Finish
// čeka i njegov odgovor, pa ide u svojoj gorutini.
func odbijPoziv(res *razmjena.PairResult) {
	if res != nil {
		go func() { _, _, _ = res.Finish(false, nil) }()
	}
}

// slusajUparivanje čeka poziv drugog čvora na portu uparivanja, ili ga
// čeka test (s.slusaj)
func (s *Service) slusajUparivanje(ctx context.Context) (*razmjena.PairResult, error) {
	if s.slusaj != nil {
		return s.slusaj(ctx)
	}
	return razmjena.Listen(ctx, s.node.key, s.identity(), s.ports.Pair)
}

// StopListening prekida čekanje
func (s *Service) StopListening() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.waiting != nil {
		s.waiting()
		s.waiting = nil
	}
}

// DialPair naziva čvor koji čeka; rezultat čeka odluku čovjeka
func (s *Service) DialPair(ctx context.Context, addr string) error {
	s.mu.Lock()
	if s.pending != nil {
		s.mu.Unlock()
		return fmt.Errorf("jedno uparivanje već čeka odluku")
	}
	s.mu.Unlock()

	if !strings.Contains(addr, ":") {
		addr = fmt.Sprintf("%s:%d", addr, s.ports.Pair)
	}
	res, err := s.nazoviUparivanje(ctx, addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.pending, s.pozvao = res, true
	s.pairErr = nil
	s.mu.Unlock()
	return nil
}

// PairOutcome je ishod uparivanja za ekran
type PairOutcome struct {
	Paired  bool   `json:"paired"`  // oba čovjeka potvrdila kod
	Member  bool   `json:"member"`  // drugi čvor je (sada) član naše mreže
	Message string `json:"message"` // što se dogodilo, ljudskim jezikom
}

// ConfirmPair je odluka čovjeka o kodu na ekranu. Oba čvora moraju
// potvrditi. Uz potvrdu putuje paket dobrodošlice: tko smo, moja potvrda
// članstva i — kad ovaj čvor drži ključ mreže — potvrda za drugoga.
// Uparen čvor bez potvrde je poznat, ali ne i član: razmjena mu se odbija.
// primi kaže smije li čovjek koji potvrđuje primiti drugi čvor u mrežu
// (globalni administrator, ili izravan klijent svježeg čvora); bez toga se
// potvrda članstva ne izdaje ni kad ovaj čvor drži ključ mreže. Uspješno
// uparivanje poništava odgodu razmjene s tim čvorom (nakon_uparivanja.go).
func (s *Service) ConfirmPair(ctx context.Context, approved, primi bool) (PairOutcome, error) {
	res, pozvao := s.uzmiUparivanje()
	if res == nil {
		return PairOutcome{}, fmt.Errorf("nema uparivanja koje čeka odluku")
	}
	given, err := s.dobrodoslica(ctx, res, approved, primi)
	if err != nil {
		_, _, _ = res.Finish(false, nil)
		return PairOutcome{Message: err.Error()}, nil
	}
	ok, theirs, err := res.Finish(approved, given)
	if err != nil {
		return PairOutcome{}, err
	}
	if !ok {
		return PairOutcome{Message: "Uparivanje odbijeno ili nepotvrđeno s druge strane."}, nil
	}
	if err := s.spremiUparenog(ctx, res); err != nil {
		return PairOutcome{}, err
	}
	out := s.primiDobrodoslicu(ctx, res, theirs, given)
	s.nakonUparivanja(ctx, res.Peer.DeviceID, out.Member, pozvao)
	return out, nil
}

// uzmiUparivanje preuzima uparivanje koje čeka odluku (nil kad ga nema) i
// kaže je li ga ovaj čvor započeo pozivom
func (s *Service) uzmiUparivanje() (*razmjena.PairResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, pozvao := s.pending, s.pozvao
	s.pending, s.pozvao = nil, false
	return res, pozvao
}

// dobrodoslica je paket koji uz potvrdu ide drugoj strani; bez potvrde
// nema paketa. Dvojnik imena se odbija prije nego što išta stigne drugoj
// strani: dva čvora istog imena miješala bi zapise u knjizi, a drugi bi
// tiho prepisao ključ prvoga na svim čvorovima.
func (s *Service) dobrodoslica(ctx context.Context, res *razmjena.PairResult, approved, primi bool) (*welcomePack, error) {
	if !approved {
		return nil, nil
	}
	if err := s.provjeriImeDrugog(ctx, res.Peer.DeviceID, res.PeerKey); err != nil {
		return nil, err
	}
	return s.welcomeFor(ctx, res.Peer.DeviceID, res.PeerKey, primi), nil
}

// spremiUparenog upisuje upareni čvor s adresom s koje se javio uz adrese
// koje je ovaj već znao
func (s *Service) spremiUparenog(ctx context.Context, res *razmjena.PairResult) error {
	now := time.Now().UTC()
	peer := Peer{
		NodeID:    res.Peer.DeviceID,
		Name:      res.Peer.Name,
		PublicKey: razmjena.PublicKeyString(res.PeerKey),
		LastSeen:  &now,
	}
	if host := res.PeerHost(); host != "" {
		peer.Addresses = []string{withPort(host, res.Peer.Meta["exchange_port"])}
	}
	if existing, _ := s.GetPeer(ctx, peer.NodeID); existing != nil {
		peer.Addresses = dedupe(append(peer.Addresses, existing.Addresses...))
		peer.IsBootstrap = existing.IsBootstrap
		peer.CreatedAt = existing.CreatedAt
	}
	if err := s.SavePeer(ctx, peer); err != nil {
		return err
	}
	s.zapamtiUparivanje(ctx, peer.NodeID, res.PeerHost(), res.Peer.Meta["exchange_port"])
	return nil
}

// primiDobrodoslicu obrađuje paket druge strane i kaže ishod uparivanja
func (s *Service) primiDobrodoslicu(ctx context.Context, res *razmjena.PairResult, theirs json.RawMessage, given *welcomePack) PairOutcome {
	member, msg, err := s.acceptWelcome(ctx, theirs, res.Peer.DeviceID, res.PeerKey, given)
	if err != nil {
		return PairOutcome{Paired: true, Message: err.Error()}
	}
	return PairOutcome{Paired: true, Member: member, Message: msg}
}

// ---------- razmjena verzija ----------

// Poruke razmjene: obje strane kažu dokle znaju, obje pošalju što druga
// nema, obje potvrde koliko su primile.
const (
	kindFrontier = "frontier"
	kindDelta    = "delta"
	kindDone     = "done"
)

type frontierMsg struct {
	Frontier map[string]string `json:"frontier"`
	Wants    *Wants            `json:"wants,omitempty"` // koje kanale pošiljatelj traži; bez toga sve
	// Program je izdanje pošiljatelja (npr. 0.0.24-alfa), Gradnja puna oznaka
	// s commitom. Stariji čvor ne šalje ni jedno: prazno znači program stariji
	// od onoga koji je to uveo.
	Program string `json:"program,omitempty"`
	Gradnja string `json:"gradnja,omitempty"`
	// Znanje je što pošiljatelj zna: oznake mogućnosti, vrste zapisa sa
	// shemama i koliko zapisa kod njega čeka nadogradnju
	// (razmjena/mogucnosti.go). Ugrađeno je iza Gradnja, pa ključevi idu
	// iza "gradnja"; prazno izostaje, a čvor 0.0.38 ga ne šalje i zanemari.
	razmjena.Znanje
	// tailscale su Tailscale adrese koje je drugi čvor javio u delti
	// razgovora preko Tailscalea; ne putuje, samo nosi primljeno do bilješke
	// o razmjeni (noteSync)
	tailscale []string
}

type deltaMsg struct {
	Versions []ledger.Version `json:"versions"`
	// Skraceno: pošiljatelj je deltu skratio zbog veličine poruke, pa ima
	// još; pozivatelj tada nastavlja odmah. Stariji čvor polje ne šalje ni čita.
	Skraceno bool `json:"skraceno,omitempty"`
	// Tailscale su Tailscale adrese pošiljatelja s portom razmjene. Šalju se
	// samo u razgovoru preko Tailscalea (tailscale.go); nikad ne idu u knjigu.
	Tailscale []string `json:"tailscale,omitempty"`
}

type doneMsg struct {
	Applied int `json:"applied"`
}

// Serve prima razmjene od poznatih čvorova dok ctx traje: na portu
// razmjene i, kad je web sučelje uključilo tunel, kroz njega
func (s *Service) Serve(ctx context.Context) error {
	primi := func(c *razmjena.Conn) { s.primiRazmjenu(ctx, c) }
	// tunel ima svoju ogradu: kroz njega dolazi bilo tko s interneta, pa
	// njegova rukovanja ne smiju zauzeti mjesta porta u lokalnoj mreži
	port := razmjena.NovaOgrada()
	go func() {
		if err := razmjena.ServeExchangeOn(ctx, s.node.key, Protocol, s.vjerodajnice, s.tunel, s.trusted, primi, razmjena.NovaOgradaTunela(port)); err != nil && ctx.Err() == nil {
			log.Printf("razmjena kroz tunel: %v", err)
		}
	}()
	return razmjena.ServeExchange(ctx, s.node.key, Protocol, s.vjerodajnice, s.ports.Exchange, s.trusted, primi, port)
}

// primiRazmjenu odradi razmjenu koju je započeo drugi čvor, na portu
// razmjene ili kroz tunel, i zabilježi ishod
func (s *Service) primiRazmjenu(ctx context.Context, c *razmjena.Conn) {
	defer c.Close()
	peer, _ := s.peerByKey(ctx, c.PeerKey, c.Vjerodajnice)
	if s.odbijTailscale(c, peer) {
		return
	}
	started := time.Now()
	applied, sent, theirs, err := s.exchange(ctx, c, false)
	s.skracene.Delete(c)
	// bilješka čeka bravu upisa, a mjesto razmjene treba sljedećem
	go s.zabiljezi(ctx, peer, c, ishodRazmjene(applied, sent, theirs, time.Since(started), err))
}

// TunelHandler je razmjena kroz tunel za web sučelje (razmjena.PutTunela):
// čvor koji izvana smije samo na web razmjenjuje kroz WebSocket.
func (s *Service) TunelHandler() http.Handler { return s.tunel.Handler() }

// NajviseVerzijaPoRazmjeni ograđuje jedan razgovor; ostatak ide sljedećim
const NajviseVerzijaPoRazmjeni = 5000

// najviseKrugovaZaRedom je koliko razgovora s istim čvorom ide jedan za
// drugim dok god je razgovor bio pun: novi čvor bez toga 136 000 verzija
// prima dva i pol sata, po 5000 svakih pet minuta.
const najviseKrugovaZaRedom = 200

// SyncWith obavi razmjenu s poznatim čvorom, na prvoj adresi koja odgovori
func (s *Service) SyncWith(ctx context.Context, nodeID string) (applied, sent int, err error) {
	peer, expect, err := s.partnerZaRazmjenu(ctx, nodeID)
	if err != nil {
		return 0, 0, err
	}
	addresses := s.adreseZaSpajanje(ctx, peer)
	if len(addresses) == 0 {
		return 0, 0, fmt.Errorf("za čvor %s nije poznata nijedna adresa", nodeID)
	}

	var lastErr error
	for _, host := range addresses {
		conn, err := s.nazovi(ctx, host, expect)
		if err != nil {
			lastErr = err
			continue
		}
		started := time.Now()
		var theirs frontierMsg
		applied, sent, theirs, err = s.exchange(ctx, conn, true)
		_, skraceno := s.skracene.LoadAndDelete(conn)
		s.josIma.Store(nodeID, skraceno && err == nil)
		conn.Close()
		if s.odbioTailscale(ctx, nodeID, err) {
			lastErr = err
			continue
		}
		s.zabiljezi(ctx, peer, conn, ishodRazmjene(applied, sent, theirs, time.Since(started), err))
		return applied, sent, err
	}
	s.zabiljezi(ctx, peer, nil, syncOutcome{err: lastErr})
	return 0, 0, fmt.Errorf("čvor %s nije dostupan ni na jednoj adresi: %v", nodeID, lastErr)
}

// partnerZaRazmjenu je poznati čvor s kojim je razmjena dopuštena (član
// mreže) i ključ koji mora dokazati. Ključ dolazi iz članstva, ne iz zapisa
// čvora: zapis piše i šalje bilo koji član, pa bi inače mogao podmetnuti
// ključ drugog člana, a razmjena bi išla s njim (na svim adresama, i kroz
// tunel).
func (s *Service) partnerZaRazmjenu(ctx context.Context, nodeID string) (*Peer, ed25519.PublicKey, error) {
	peer, err := s.GetPeer(ctx, nodeID)
	if err != nil {
		return nil, nil, err
	}
	if peer == nil {
		return nil, nil, fmt.Errorf("čvor %s nije poznat — prvo ga uparite", nodeID)
	}
	expect, err := s.kljucClana(ctx, nodeID)
	if errors.Is(err, errNijeClan) {
		return nil, nil, fmt.Errorf("čvor %s nije član naše mreže — razmjena nije dopuštena dok ga nositelj mrežnog ključa ili ovlašteni primatelj ne primi", nodeID)
	}
	if err != nil {
		return nil, nil, err
	}
	s.javiDrugiKljuc(peer, expect)
	return peer, expect, nil
}

// javiDrugiKljuc zapisuje u dnevnik, jednom po računalu i ključu, kad
// zapis računala nosi drugi ključ od onoga u njegovu članstvu. Takav zapis
// je zastario ili ga je netko podmetnuo; razmjena ionako traži ključ iz
// članstva, a zapis se ovdje ne ispravlja (nova verzija bi putovala svima).
func (s *Service) javiDrugiKljuc(p *Peer, clanski ed25519.PublicKey) {
	k := razmjena.PublicKeyString(clanski)
	if p.PublicKey == k {
		return
	}
	if _, vec := s.javljenKljuc.LoadOrStore(p.NodeID+"\x00"+p.PublicKey, true); !vec {
		log.Printf("razmjena: računalo %s u popisu računala ima drugi ključ (%.8s…) od onoga u potvrdi članstva (%.8s…); razmjena traži ključ iz potvrde", label(*p), p.PublicKey, k)
	}
}

// adreseZaSpajanje su adrese čvora redom kojim ih razmjena zove: adresa
// na kojoj se upravo javio u lokalnoj mreži, izravne adrese iz zapisa,
// Tailscale adrese koje je čvor sam javio, a tunel zadnji. Tunel je
// najsporiji, a u istoj mreži port razmjene je brži.
func (s *Service) adreseZaSpajanje(ctx context.Context, peer *Peer) []string {
	var izravne, tuneli []string
	for _, a := range bezTailscalea(peer.Addresses) {
		if razmjena.JeAdresaTunela(a) {
			tuneli = append(tuneli, a)
		} else {
			izravne = append(izravne, a)
		}
	}
	izravne = append(s.uLokalnojMrezi(peer.NodeID), izravne...)
	izravne = append(izravne, s.tailscaleZaSpajanje(ctx, peer.NodeID)...)
	return dedupe(append(izravne, tuneli...))
}

// uLokalnojMrezi je adresa na kojoj se čvor javlja u lokalnoj mreži, kad
// je pronalaženje uključeno i čvor se javi
func (s *Service) uLokalnojMrezi(nodeID string) []string {
	if s.ports.Discovery <= 0 {
		return nil
	}
	f, ok := razmjena.FindDevice(Protocol, nodeID, 700*time.Millisecond, s.ports.Discovery)
	if !ok {
		return nil
	}
	return []string{withPort(f.Addr, fmt.Sprint(f.ExchangePort))}
}

// nazovi otvara razmjenu na jednoj adresi: kroz tunel (https://domena) ili
// na portu razmjene
func (s *Service) nazovi(ctx context.Context, host string, expect ed25519.PublicKey) (*razmjena.Conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if razmjena.JeAdresaTunela(host) {
		return razmjena.DialTunel(dialCtx, s.node.key, Protocol, s.vjerodajnice, host, expect)
	}
	// adresa bez porta je stari zapis ili ručni unos — vrijedi zadani port
	adresa := withPort(host, fmt.Sprint(DefaultExchangePort))
	raw, err := s.otvori(dialCtx, adresa)
	if err != nil {
		return nil, err
	}
	return razmjena.DialExchangeOn(dialCtx, raw, s.node.key, Protocol, s.vjerodajnice, adresa, expect)
}

// nazoviUparivanje otvara uparivanje s čvorom koji ga čeka na adresi
func (s *Service) nazoviUparivanje(ctx context.Context, adresa string) (*razmjena.PairResult, error) {
	raw, err := s.otvori(ctx, adresa)
	if err != nil {
		return nil, err
	}
	return razmjena.DialOn(raw, s.node.key, s.identity())
}

// otvori otvara TCP vezu do drugog čvora, ili je otvori test (s.dial)
func (s *Service) otvori(ctx context.Context, adresa string) (net.Conn, error) {
	if s.dial != nil {
		return s.dial(ctx, adresa)
	}
	return (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, "tcp", adresa)
}

// najviseBajtovaDelte ograđuje jednu poruku delte (verzije kao JSON).
// Primatelj od 0.0.25 odbija poruku veću od razmjena.NajvecaPoruka, a
// stariji čvor nema ograde, pa ovo čuva i njegovu memoriju.
var najviseBajtovaDelte = 32 << 20

// ogradiDeltu reže deltu kad bi poruka prešla najvise bajtova; ostatak ide
// sljedećim razgovorom. Delta je složena po autoru i kanalu, svaki redom
// nastanka, pa je odrezani dio uvijek početak niza nekog ključa: granica
// primatelja (najveći version_id po ključu) time ne preskače ništa. Prva
// verzija ide uvijek, inače jedna golema ne bi prošla nikad.
//
// Neispravna verzija (zaSlanje, pokvaren redak u knjizi) ne ide, nego se
// vraća zasebno: do 0.0.38 je cijela poruka padala, pa je jedna takva
// verzija rušila svaku razmjenu s čvorom koji je nema. Ostale verzije istog
// ključa idu redom, pa granica primatelja prijeđe preko nje.
func ogradiDeltu(delta []ledger.Version, najvise int) (poslati, neispravne []ledger.Version, skraceno bool) {
	ukupno := 0
	for _, v := range delta {
		b, err := zaSlanje(v)
		if err != nil {
			neispravne = append(neispravne, v)
			continue
		}
		ukupno += len(b) + 1 // zarez među elementima
		if ukupno > najvise && len(poslati) > 0 {
			return poslati, neispravne, true
		}
		poslati = append(poslati, v)
	}
	return poslati, neispravne, false
}

// najviseKrugovaPrekoNeispravnih ograđuje čitanje preko neispravnih verzija
// jednog ključa: najviše toliko punih delti zaredom (uz 5000 po delti to je
// 100.000 verzija)
const najviseKrugovaPrekoNeispravnih = 20

// deltaZa je ono što drugi čvor prema svojoj granici još nema, samo u
// kanalima koje prati, s ključevima pročitanima preko neispravnih verzija
func (s *Service) deltaZa(ctx context.Context, theirs frontierMsg) ([]ledger.Version, error) {
	delta, err := s.rec.Delta(ctx, theirs.Frontier, s.wantsFunc(ctx, theirs.Wants), NajviseVerzijaPoRazmjeni)
	if err != nil {
		return nil, err
	}
	return prekoNeispravnih(ctx, delta, NajviseVerzijaPoRazmjeni, s.rec.SinceByNode)
}

// citacKljuca čita verzije jednog autora u jednom kanalu iza zadane, kao
// ledger.Recorder.SinceByNode
type citacKljuca func(ctx context.Context, nodeID, channel, afterVersionID string, limit int) ([]ledger.Version, error)

// prekoNeispravnih čita dalje ključ kojemu je delta puna, a nijedna verzija
// u njoj se ne da poslati. Ogradu po ključu (najvise) inače bi popunile same
// neispravne verzije: primatelj ne bi dobio ništa, granica mu se u tom
// ključu ne bi pomaknula, a ispravne verzije iza njih ne bi stigle nikad.
// Ključ se čita do prve ispravne verzije ili do kraja, a dodano ide na kraj
// delte, pa je unutar ključa i dalje redom nastanka.
func prekoNeispravnih(ctx context.Context, delta []ledger.Version, najvise int, citaj citacKljuca) ([]ledger.Version, error) {
	for _, zadnja := range zapeliKljucevi(delta, najvise) {
		for krug := 0; krug < najviseKrugovaPrekoNeispravnih; krug++ {
			dalje, err := citaj(ctx, zadnja.NodeID, zadnja.Channel, zadnja.VersionID, najvise)
			if err != nil {
				return nil, err
			}
			delta = append(delta, dalje...)
			if len(dalje) < najvise || imaIspravnu(dalje) {
				break
			}
			zadnja = dalje[len(dalje)-1]
		}
	}
	return delta, nil
}

// zapeliKljucevi vraća zadnju verziju svakog ključa koji u delti ima punu
// ogradu verzija, a nijednu ispravnu. Delta drži verzije istog ključa
// zajedno (ledger.Recorder.Delta), pa se ključ prepoznaje po susjedima.
func zapeliKljucevi(delta []ledger.Version, najvise int) []ledger.Version {
	var out []ledger.Version
	for i := 0; i < len(delta); {
		j := krajKljuca(delta, i)
		if j-i >= najvise && !imaIspravnu(delta[i:j]) {
			out = append(out, delta[j-1])
		}
		i = j
	}
	return out
}

// krajKljuca je prvi indeks iza niza verzija istog ključa kao delta[i]
func krajKljuca(delta []ledger.Version, i int) int {
	j := i + 1
	for j < len(delta) && delta[j].NodeID == delta[i].NodeID && delta[j].Channel == delta[i].Channel {
		j++
	}
	return j
}

// Zašto verzija iz knjige ne ide drugim čvorovima (zaSlanje)
var (
	errBezOznake = errors.New("nema oznaku, entitet ili identifikator zapisa")
	errNijeJSON  = errors.New("nije ispravan JSON")
)

// zaSlanje je verzija zapisana kao JSON, ili greška kad je neispravna: kad
// je knjiga primatelja ne prima (ledger.Ispravna) ili se ne da zapisati kao
// JSON. Bez provjere oznake takvu bi verziju čvor slao svakom razmjenom:
// primatelj je preskoči, pa mu se granica ne pomakne.
func zaSlanje(v ledger.Version) ([]byte, error) {
	if !ledger.Ispravna(v) {
		return nil, errBezOznake
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, errNijeJSON
	}
	return b, nil
}

// imaIspravnu javlja da se barem jedna verzija smije poslati (zaSlanje)
func imaIspravnu(verzije []ledger.Version) bool {
	for _, v := range verzije {
		if _, err := zaSlanje(v); err == nil {
			return true
		}
	}
	return false
}

// zabiljeziNeispravne zapiše u dnevnik, jednom po verziji, verzije iz
// knjige koje razmjena ne šalje (zaSlanje), s razlogom; sadržaj ne ide u
// dnevnik
func (s *Service) zabiljeziNeispravne(neispravne []ledger.Version) {
	for _, v := range neispravne {
		if _, bilo := s.neispravne.LoadOrStore(v.VersionID, struct{}{}); !bilo {
			_, zasto := zaSlanje(v)
			log.Printf("razmjena: verzija %s (%s) u knjizi %v i ne šalje se drugim čvorovima", v.VersionID, v.Entity, zasto)
		}
	}
}

// pomakniGranicu vraća granicu uvećanu za poslane verzije
func pomakniGranicu(granica map[string]string, poslano []ledger.Version) map[string]string {
	if len(poslano) == 0 {
		return granica
	}
	out := make(map[string]string, len(granica))
	for k, v := range granica {
		out[k] = v
	}
	for _, v := range poslano {
		k := ledger.FrontierKey(v.NodeID, v.Channel)
		if v.VersionID > out[k] {
			out[k] = v.VersionID
		}
	}
	return out
}

// exchange je jedan razgovor s drugim čvorom (razgovor.go): granice, delte,
// upis primljenog, želje, sadržaj i kraj. Onaj tko je nazvao (initiator) u
// svakom koraku prvi šalje. Dok upis ne prođe (i kad ne uspije), greška
// vraća 0 upisanih i 0 poslanih, iako je delta možda već otišla. theirs je
// granica druge strane kakvu razgovor dotad ima; nakon delti je pomaknuta
// za poslano i neispravno i nosi Tailscale adrese iz njezine delte.
// Bilješka o razmjeni zato i kod greške pamti izdanje druge strane.
func (s *Service) exchange(ctx context.Context, c *razmjena.Conn, initiator bool) (applied, sent int, theirs frontierMsg, err error) {
	r, err := s.zapocniRazgovor(ctx, c, initiator)
	if err != nil {
		return 0, 0, frontierMsg{}, err
	}
	if err = r.granice(); err != nil {
		return 0, 0, r.theirs, err
	}
	stiglo, err := r.delte(ctx)
	if err != nil {
		return 0, 0, r.theirs, err
	}
	if err = r.upisiPrimljeno(ctx, stiglo); err != nil {
		return 0, 0, r.theirs, err
	}
	err = r.sadrzajIKraj(ctx)
	return r.applied, r.sent, r.theirs, err
}

// OnApplied postavlja što se radi s primljenim verzijama nakon upisa u
// knjigu — osvježavanje površine zna sloj koji zna entitete
func (s *Service) OnApplied(fn func(ctx context.Context, versions []ledger.Version) error) {
	s.onApplied = fn
}

// Accept postavlja što ovaj čvor prima razmjenom. Bez toga prima sve.
func (s *Service) Accept(fn func(ledger.Version) bool) {
	s.accept = fn
}

func (s *Service) noteSync(ctx context.Context, peer *Peer, c *razmjena.Conn, o syncOutcome) {
	if peer == nil {
		return
	}
	applied, sent, err := o.applied, o.sent, o.err
	s.recordSyncState(ctx, peer.NodeID, o)
	s.zapamtiTailscale(ctx, peer.NodeID, o)
	s.zapamtiZnanje(ctx, peer.NodeID, o)
	if mojProgram, mojaGradnja := s.Program(); o.program != "" && mojProgram != "" && o.program != mojProgram {
		if _, vec := s.javljenaInacica.LoadOrStore(peer.NodeID+"\x00"+o.program, true); !vec {
			log.Printf("razmjena: čvor %s radi na %s, ovaj na %s", label(*peer), cmp.Or(o.gradnja, o.program), cmp.Or(mojaGradnja, mojProgram))
		}
	}
	// Razmjena je mogla donijeti noviji zapis tog čvora (adrese, izloženost);
	// bilješka ide na svjež red, inače bi stara kopija iz memorije pregazila
	// primljeno i kao nova verzija otputovala natrag.
	if fresh, getErr := s.GetPeer(ctx, peer.NodeID); getErr == nil && fresh != nil {
		peer = fresh
	}
	now := time.Now().UTC()
	peer.LastSeen = &now
	if err == nil {
		peer.LastSync = &now
		peer.LastSyncNote = fmt.Sprintf("primljeno %d, poslano %d", applied, sent)
	} else {
		peer.LastSyncNote = "greška: " + err.Error()
	}
	if c != nil {
		if host, _, splitErr := net.SplitHostPort(c.RemoteAddr().String()); splitErr == nil && host != "" {
			if !hasAddressForHost(peer.Addresses, host) {
				peer.Addresses = dedupe(append(peer.Addresses, host))
			}
		}
	}
	if saveErr := s.SavePeer(ctx, *peer); saveErr != nil {
		log.Printf("sinkronizacija: bilješka o čvoru %s nije spremljena: %v", peer.NodeID, saveErr)
	}
}

// SyncAll obavi razmjenu sa svakim poznatim čvorom; vraća sažetak po čvoru
func (s *Service) SyncAll(ctx context.Context) map[string]string {
	peersList, err := s.ListPeers(ctx)
	if err != nil {
		return map[string]string{"*": err.Error()}
	}
	return s.syncPeers(ctx, peersList)
}

// RunAutoSync povremeno sinkronizira sa svim poznatim čvorovima
func (s *Service) RunAutoSync(ctx context.Context, every time.Duration) {
	if every <= 0 {
		return
	}
	s.mu.Lock()
	s.every, s.autoOn, s.autoCtx = every, true, ctx
	s.mu.Unlock()
	// Prva razmjena ide pola minute nakon pokretanja, ne nakon punog
	// razmaka: ažuriranje ili ponovno pokretanje inače ostavi čvorove
	// neusklađene po pet minuta (2.10.2026. tri pokretanja zaredom — 15 minuta).
	prva := time.NewTimer(30 * time.Second)
	defer prva.Stop()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-prva.C:
		case <-t.C:
		}
		for node, note := range s.SyncDue(ctx) {
			log.Printf("sinkronizacija s %s: %s", node, note)
		}
	}
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	if out == nil {
		out = []string{}
	}
	return out
}

// utcIliNull piše vrijeme koje smije izostati u UTC-u, kao i ostala vremena
// čvorova: verzija iz JSON-a može nositi zonu bez imena, a takvu modernc
// zapiše u obliku koji više ne zna pročitati
func utcIliNull(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// withPort spaja host i port; host koji već nosi port ostaje kakav jest,
// a prazan ili neispravan port ne dodaje ništa
func withPort(host, port string) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	if strings.TrimSpace(port) == "" || port == "0" {
		return host
	}
	return net.JoinHostPort(host, port)
}

func hasAddressForHost(addresses []string, host string) bool {
	for _, a := range addresses {
		h, _, err := net.SplitHostPort(a)
		if err != nil {
			h = a
		}
		if h == host {
			return true
		}
	}
	return false
}

// PrivateKey daje privatni ključ čvora onome tko njime potpisuje ono što čvor
// izdaje. Ključ je isti kojim se čvor predstavlja na mreži: paket i razmjena
// govore o istom čvoru, pa nema razloga za drugi ključ.
func (n *Node) PrivateKey() ed25519.PrivateKey { return n.key }
