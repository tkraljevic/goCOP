// Paket peers je goCOP-ova strana sinkronizacije: identitet ovog čvora,
// popis poznatih čvorova i razmjena verzija iz knjige preko syncnet-a.
//
// Svaki čvor je punopravna kopija; nijedan nije izvor istine. Dva čvora
// razmijene "dokle znaju" (frontier po autoru) i svaki pošalje drugome
// verzije koje ovaj još nema — i one tuđe koje je sam primio, pa promjena
// stiže i zaobilazno. Primanje je samo dodavanje, pa je i ponovljeno
// primanje bezopasno.
package peers

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tkraljevic/syncnet"

	"gocop/internal/ledger"
)

// Protocol imenuje goCOP mrežu; čvorovi drugih aplikacija (npr. goEMM na
// istom laptopu) se ne uparuju s goCOP-om i ne odgovaraju na njegove probe.
const Protocol = "gocop"

// Portovi kao obitelj: razmjena, uparivanje, pronalaženje. Drukčiji od
// goEMM-ovih (4610–4612) da oba mogu raditi na istom stroju.
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
	return syncnet.PublicKeyString(n.key.Public().(ed25519.PublicKey))
}

// identity je ono što ovaj čvor kaže o sebi pri uparivanju. Port razmjene
// putuje ovdje jer druga strana vidi samo odakle je veza došla — a
// uparivanje ide na jednom portu, razmjena na drugom.
func (s *Service) identity() syncnet.Identity {
	return syncnet.Identity{
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
	key, err := syncnet.LoadOrCreateKey(filepath.Join(filepath.Dir(dbPath), KeyFileName))
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
	db    *sql.DB
	rec   *ledger.Recorder
	node  *Node
	ports Ports

	mu      sync.Mutex
	pending *syncnet.PairResult // uparivanje koje čeka odluku čovjeka
	pairErr error
	waiting context.CancelFunc // aktivni Listen, ako čeka

	// mreža kojoj čvor pripada; nil dok ga netko ne primi ili dok je ne osnuje
	network  *syncnet.NetworkKey
	joinedAt time.Time

	// onApplied osvježava površinu iz primljenih verzija; zna ga sloj
	// koji zna entitete (repository.ApplyVersions)
	onApplied func(ctx context.Context, versions []ledger.Version) error

	// accept propušta ono što ovaj čvor uopće želi držati. Terenski uređaj
	// ne treba stoljeće očitanja svih letvi u zemlji, pa ono što ovdje
	// otpadne ne ulazi ni u knjigu ni na površinu.
	accept func(ledger.Version) bool

	every    time.Duration // razmak automatske sinkronizacije (0 = isključena)
	autoOn   bool
	wantsAll bool // prati sve kanale (uredski čvor)
}

func (s *Service) autoSync() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.autoOn
}

func NewService(db *sql.DB, rec *ledger.Recorder, node *Node, ports Ports) (*Service, error) {
	s := &Service{db: db, rec: rec, node: node, ports: ports}
	if err := s.loadNetwork(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) Node() *Node  { return s.node }
func (s *Service) Ports() Ports { return s.ports }

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
		if strings.ContainsAny(add, " /\\") {
			return nil, fmt.Errorf("adresa je domena ili IP, po želji s portom: npr. cop-osijek.com ili 10.0.0.5:4710")
		}
		p.Addresses = dedupe(append(p.Addresses, withPort(add, s.exchangePortOf(p))))
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
	if _, err := syncnet.ParsePublicKey(p.PublicKey); err != nil {
		return fmt.Errorf("neispravan javni ključ čvora: %w", err)
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}
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
	identityChanged := isNew || prevName != p.Name || prevKey != p.PublicKey ||
		prevAddrs != string(addrs) || prevBootstrap != boolInt(p.IsBootstrap)

	_, err = tx.ExecContext(ctx, `
		INSERT INTO peers (node_id, name, public_key, addresses, is_bootstrap, last_seen, last_sync, last_sync_note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			name = excluded.name, public_key = excluded.public_key, addresses = excluded.addresses,
			is_bootstrap = excluded.is_bootstrap, last_seen = excluded.last_seen,
			last_sync = excluded.last_sync, last_sync_note = excluded.last_sync_note
	`, p.NodeID, p.Name, p.PublicKey, string(addrs), boolInt(p.IsBootstrap), p.LastSeen, p.LastSync, p.LastSyncNote, p.CreatedAt)
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

// ForgetPeer uklanja čvor s popisa; u knjizi ostaje arhiviran
func (s *Service) ForgetPeer(ctx context.Context, nodeID string) error {
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
	if _, err := s.rec.Archive(ctx, tx, EntityPeers, nodeID, p); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) peerByKey(ctx context.Context, pub ed25519.PublicKey) (*Peer, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT node_id, name, public_key, addresses, is_bootstrap, last_seen, last_sync, last_sync_note, created_at
		FROM peers WHERE public_key = ?`, syncnet.PublicKeyString(pub))
	p, err := scanPeer(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}

// ---------- pronalaženje na lokalnoj mreži ----------

// Announce odgovara na probe drugih goCOP čvorova dok ctx traje
func (s *Service) Announce(ctx context.Context) error {
	return syncnet.Announce(ctx, Protocol, s.ports.Discovery, func() syncnet.Beacon {
		b := syncnet.Beacon{DeviceID: s.node.ID, Name: s.node.Name, ExchangePort: s.ports.Exchange}
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
	syncnet.Found
	Known bool `json:"known"`
}

// Discover proba lokalnu mrežu i vraća goCOP čvorove koji su se javili
func (s *Service) Discover(ctx context.Context, timeout time.Duration) ([]Discovered, error) {
	found, err := syncnet.Discover(Protocol, timeout, s.ports.Discovery)
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
	Waiting  bool          `json:"waiting"` // ovaj čvor čeka da ga netko nazove
	Pending  bool          `json:"pending"` // netko se javio, čeka odluku
	SAS      string        `json:"sas,omitempty"`
	Peer     syncnet.Hello `json:"peer,omitempty"`
	PeerHost string        `json:"peer_host,omitempty"`
	Error    string        `json:"error,omitempty"`
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
	s.mu.Unlock()

	go func() {
		res, err := syncnet.Listen(ctx, s.node.key, s.identity(), s.ports.Pair)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.waiting = nil
		if err != nil {
			if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				s.pairErr = err
			}
			return
		}
		s.pending = res
	}()
	return nil
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
	res, err := syncnet.Dial(ctx, s.node.key, s.identity(), addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.pending = res
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
func (s *Service) ConfirmPair(ctx context.Context, approved bool) (PairOutcome, error) {
	s.mu.Lock()
	res := s.pending
	s.pending = nil
	s.mu.Unlock()
	if res == nil {
		return PairOutcome{}, fmt.Errorf("nema uparivanja koje čeka odluku")
	}

	var given *welcomePack
	if approved {
		given = s.welcomeFor(ctx, res.Peer.DeviceID, res.PeerKey)
	}
	ok, theirs, err := res.Finish(approved, given)
	if err != nil {
		return PairOutcome{}, err
	}
	if !ok {
		return PairOutcome{Message: "Uparivanje odbijeno ili nepotvrđeno s druge strane."}, nil
	}

	now := time.Now().UTC()
	peer := Peer{
		NodeID:    res.Peer.DeviceID,
		Name:      res.Peer.Name,
		PublicKey: syncnet.PublicKeyString(res.PeerKey),
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
		return PairOutcome{}, err
	}

	member, msg, err := s.acceptWelcome(ctx, theirs, res.Peer.DeviceID, res.PeerKey, given)
	if err != nil {
		return PairOutcome{Paired: true, Message: err.Error()}, nil
	}
	return PairOutcome{Paired: true, Member: member, Message: msg}, nil
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
}

type deltaMsg struct {
	Versions []ledger.Version `json:"versions"`
}

type doneMsg struct {
	Applied int `json:"applied"`
}

// Serve prima razmjene od poznatih čvorova dok ctx traje
func (s *Service) Serve(ctx context.Context) error {
	return syncnet.ServeExchange(ctx, s.node.key, Protocol, s.ports.Exchange, s.trusted, func(c *syncnet.Conn) {
		defer c.Close()
		peer, _ := s.peerByKey(ctx, c.PeerKey)
		started := time.Now()
		applied, sent, theirs, err := s.exchange(ctx, c, false)
		s.noteSync(ctx, peer, c, syncOutcome{applied: applied, sent: sent, frontier: theirs, took: time.Since(started), err: err})
	})
}

// SyncWith obavi razmjenu s poznatim čvorom, na prvoj adresi koja odgovori
func (s *Service) SyncWith(ctx context.Context, nodeID string) (applied, sent int, err error) {
	peer, err := s.GetPeer(ctx, nodeID)
	if err != nil {
		return 0, 0, err
	}
	if peer == nil {
		return 0, 0, fmt.Errorf("čvor %s nije poznat — prvo ga uparite", nodeID)
	}
	expect, err := syncnet.ParsePublicKey(peer.PublicKey)
	if err != nil {
		return 0, 0, err
	}
	if !s.trusted(expect) {
		return 0, 0, fmt.Errorf("čvor %s nije član naše mreže — razmjena nije dopuštena dok ga nositelj mrežnog ključa ne primi", nodeID)
	}

	addresses := peer.Addresses
	if s.ports.Discovery > 0 {
		if f, ok := syncnet.FindDevice(Protocol, nodeID, 700*time.Millisecond, s.ports.Discovery); ok {
			addresses = dedupe(append([]string{withPort(f.Addr, fmt.Sprint(f.ExchangePort))}, addresses...))
		}
	}
	if len(addresses) == 0 {
		return 0, 0, fmt.Errorf("za čvor %s nije poznata nijedna adresa", nodeID)
	}

	var lastErr error
	for _, host := range addresses {
		// adresa bez porta je stari zapis ili ručni unos — vrijedi zadani port
		addr := withPort(host, fmt.Sprint(DefaultExchangePort))
		dialCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		conn, err := syncnet.DialExchange(dialCtx, s.node.key, Protocol, addr, expect)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		started := time.Now()
		var theirs map[string]string
		applied, sent, theirs, err = s.exchange(ctx, conn, true)
		conn.Close()
		s.noteSync(ctx, peer, conn, syncOutcome{applied: applied, sent: sent, frontier: theirs, took: time.Since(started), err: err})
		return applied, sent, err
	}
	s.noteSync(ctx, peer, nil, syncOutcome{err: lastErr})
	return 0, 0, fmt.Errorf("čvor %s nije dostupan ni na jednoj adresi: %v", nodeID, lastErr)
}

// exchange je jedan razgovor: frontier ↔ frontier, delta ↔ delta, done ↔ done.
// Onaj tko je nazvao (initiator) prvi šalje; obje strane rade isto.
func (s *Service) exchange(ctx context.Context, c *syncnet.Conn, initiator bool) (applied, sent int, theirFrontier map[string]string, err error) {
	mine, err := s.rec.Frontier(ctx)
	if err != nil {
		return 0, 0, nil, err
	}
	myWants, err := s.CurrentWants(ctx)
	if err != nil {
		return 0, 0, nil, err
	}

	send := func(kind string, v any) error {
		e, err := syncnet.NewEnvelope(kind, v)
		if err != nil {
			return err
		}
		e.DeviceID = s.node.ID
		return c.Send(e)
	}
	expect := func(kind string, v any) error {
		e, err := c.Receive()
		if err != nil {
			return err
		}
		if e.Kind != kind {
			if e.Reason != "" {
				return fmt.Errorf("čvor odbio razmjenu: %s", e.Reason)
			}
			return fmt.Errorf("očekivana poruka %q, stigla %q", kind, e.Kind)
		}
		return e.Decode(v)
	}

	var theirs frontierMsg
	if initiator {
		if err := send(kindFrontier, frontierMsg{mine, &myWants}); err != nil {
			return 0, 0, theirs.Frontier, err
		}
		if err := expect(kindFrontier, &theirs); err != nil {
			return 0, 0, theirs.Frontier, err
		}
	} else {
		if err := expect(kindFrontier, &theirs); err != nil {
			return 0, 0, theirs.Frontier, err
		}
		if err := send(kindFrontier, frontierMsg{mine, &myWants}); err != nil {
			return 0, 0, theirs.Frontier, err
		}
	}

	// šalje se samo što drugi prati; što ovaj čvor ne prati, ne prima ni
	// omaškom, jer bi ostalo bez granice i vraćalo se svakom razmjenom
	delta, err := s.rec.Delta(ctx, theirs.Frontier, s.wantsFunc(ctx, theirs.Wants), 5000)
	if err != nil {
		return 0, 0, theirs.Frontier, err
	}
	var incoming deltaMsg
	if initiator {
		if err := send(kindDelta, deltaMsg{delta}); err != nil {
			return 0, 0, theirs.Frontier, err
		}
		if err := expect(kindDelta, &incoming); err != nil {
			return 0, 0, theirs.Frontier, err
		}
	} else {
		if err := expect(kindDelta, &incoming); err != nil {
			return 0, 0, theirs.Frontier, err
		}
		if err := send(kindDelta, deltaMsg{delta}); err != nil {
			return 0, 0, theirs.Frontier, err
		}
	}
	sent = len(delta)

	myFilter := s.wantsFunc(ctx, &myWants)
	wanted := make([]ledger.Version, 0, len(incoming.Versions))
	for _, v := range incoming.Versions {
		if myFilter != nil && !myFilter(v.Channel) {
			continue
		}
		if s.accept != nil && !s.accept(v) {
			continue
		}
		wanted = append(wanted, v)
	}
	applied, err = s.rec.Apply(ctx, wanted)
	if err != nil {
		return 0, 0, theirs.Frontier, err
	}
	if applied > 0 && s.onApplied != nil {
		if err := s.onApplied(ctx, wanted); err != nil {
			log.Printf("sinkronizacija: verzije primljene, ali površina nije osvježena: %v", err)
		}
	}

	var theirDone doneMsg
	if initiator {
		if err := send(kindDone, doneMsg{applied}); err != nil {
			return applied, sent, theirs.Frontier, err
		}
		_ = expect(kindDone, &theirDone)
	} else {
		_ = expect(kindDone, &theirDone)
		_ = send(kindDone, doneMsg{applied})
	}
	return applied, sent, theirs.Frontier, nil
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

func (s *Service) noteSync(ctx context.Context, peer *Peer, c *syncnet.Conn, o syncOutcome) {
	if peer == nil {
		return
	}
	applied, sent, err := o.applied, o.sent, o.err
	s.recordSyncState(ctx, peer.NodeID, o)
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
	s.every, s.autoOn = every, true
	s.mu.Unlock()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for node, note := range s.SyncDue(ctx) {
				log.Printf("sinkronizacija s %s: %s", node, note)
			}
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
