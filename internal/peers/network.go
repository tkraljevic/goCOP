package peers

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tkraljevic/syncnet"
)

// Mreža je skupina čvorova koji si vjeruju, i ima vlastiti ključ. Članstvo
// je potpis mrežnog ključa nad ključem čvora; provjerava se na vratima
// svake razmjene. Dvije organizacije s istim programom imaju dva mrežna
// ključa i time ništa zajedničko: potvrda jedne ne vrijedi kod druge.
//
// Uparivanje (šest znamenki) i dalje dokazuje KOJI je čvor na drugoj
// strani; potvrda dokazuje da je JEDAN OD NAŠIH. Čvor uparen bez važeće
// potvrde je poznat, ali ne i pouzdan — razmjena mu se odbija dok ga
// nositelj mrežnog ključa ne primi.

// NetworkKeyFileName je privatni ključ mreže — postoji samo na čvorovima
// čiji vlasnici smiju primati članove. Nikad u bazi, nikad se ne sinkronizira.
const NetworkKeyFileName = "network-key"

// MembershipValidity je rok potvrde; obnavlja se ponovnim primanjem
const MembershipValidity = 365 * 24 * time.Hour

// EntityMemberships je naziv entiteta u knjizi verzija
const EntityMemberships = "memberships"

// Network je mreža kojoj čvor pripada, ako pripada
type Network struct {
	Name      string    `json:"name"`
	PublicKey string    `json:"public_key"`
	JoinedAt  time.Time `json:"joined_at"`
	CanAdmit  bool      `json:"can_admit"` // ovaj čvor drži privatni ključ mreže
}

// welcomePack je ono što se preda pri uparivanju: tko smo (mreža), moja
// potvrda (da me drugi može provjeriti) i, kad je mogu izdati, potvrda za
// drugoga — pa je član onog trena kad oba čovjeka potvrde kod.
type welcomePack struct {
	NetworkName string              `json:"network_name,omitempty"`
	NetworkKey  string              `json:"network_key,omitempty"`
	Mine        *syncnet.Membership `json:"mine,omitempty"`
	ForYou      *syncnet.Membership `json:"for_you,omitempty"`
}

// loadNetwork čita mrežu iz baze i, ako postoji, privatni ključ uz bazu
func (s *Service) loadNetwork() error {
	var name, pub string
	var joined time.Time
	err := s.db.QueryRow(`SELECT name, public_key, joined_at FROM network WHERE id = 1`).Scan(&name, &pub, &joined)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	pubKey, err := syncnet.ParsePublicKey(pub)
	if err != nil {
		return fmt.Errorf("javni ključ mreže u bazi je neispravan: %w", err)
	}

	key := syncnet.PublicNetwork(name, pubKey)
	if priv, err := syncnet.LoadKey(s.networkKeyPath()); err == nil {
		if !priv.Public().(ed25519.PublicKey).Equal(pubKey) {
			return fmt.Errorf("datoteka %s ne pripada mreži %q iz baze", s.networkKeyPath(), name)
		}
		key = syncnet.LoadNetworkKey(name, priv)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("ključ mreže: %w", err)
	}

	s.mu.Lock()
	s.network = &key
	s.joinedAt = joined
	s.mu.Unlock()
	return nil
}

func (s *Service) networkKeyPath() string {
	return filepath.Join(s.node.Dir, NetworkKeyFileName)
}

// NetworkInfo vraća mrežu čvora, ili nil kad čvor još nije ni u jednoj
func (s *Service) NetworkInfo() *Network {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.network == nil {
		return nil
	}
	return &Network{
		Name:      s.network.Name,
		PublicKey: syncnet.PublicKeyString(s.network.Public),
		JoinedAt:  s.joinedAt,
		CanAdmit:  s.network.CanSign(),
	}
}

// CreateNetwork osniva mrežu: ovaj čvor dobiva privatni ključ mreže i
// postaje njezin prvi član. Radi se jednom, na jednom čvoru.
func (s *Service) CreateNetwork(ctx context.Context, name string) error {
	if s.NetworkInfo() != nil {
		return fmt.Errorf("ovaj čvor već pripada mreži %q", s.NetworkInfo().Name)
	}
	if name == "" {
		return fmt.Errorf("naziv mreže je obavezan")
	}

	key, err := syncnet.NewNetwork(name)
	if err != nil {
		return err
	}
	if err := syncnet.SaveKey(s.networkKeyPath(), key.Private()); err != nil {
		return fmt.Errorf("ključ mreže se ne može zapisati: %w", err)
	}

	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO network (id, name, public_key, joined_at) VALUES (1, ?, ?, ?)`,
		name, syncnet.PublicKeyString(key.Public), now); err != nil {
		return err
	}
	s.mu.Lock()
	s.network = &key
	s.joinedAt = now
	s.mu.Unlock()

	// osnivač je prvi član — vlastitim potpisom
	self, err := key.Admit(s.node.ID, s.node.key.Public().(ed25519.PublicKey), s.node.ID, 10*MembershipValidity)
	if err != nil {
		return err
	}
	return s.saveMembership(ctx, self)
}

// joinNetwork prihvaća mrežu iz paketa dobrodošlice — samo kad čvor još
// nije ni u jednoj mreži i kad paket nosi potvrdu za baš ovaj čvor
func (s *Service) joinNetwork(ctx context.Context, pack welcomePack) error {
	if pack.NetworkKey == "" || pack.ForYou == nil {
		return fmt.Errorf("druga strana nije nositelj mrežnog ključa — čvor je uparen, ali nije primljen u mrežu; primiti ga mora netko tko drži ključ mreže")
	}
	pubKey, err := syncnet.ParsePublicKey(pack.NetworkKey)
	if err != nil {
		return fmt.Errorf("neispravan ključ mreže u paketu: %w", err)
	}
	myKey := s.node.key.Public().(ed25519.PublicKey)
	if err := pack.ForYou.Verify(pubKey, myKey, time.Now()); err != nil {
		return fmt.Errorf("potvrda članstva za ovaj čvor ne vrijedi: %w", err)
	}

	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO network (id, name, public_key, joined_at) VALUES (1, ?, ?, ?)`,
		pack.NetworkName, pack.NetworkKey, now); err != nil {
		return err
	}
	key := syncnet.PublicNetwork(pack.NetworkName, pubKey)
	s.mu.Lock()
	s.network = &key
	s.joinedAt = now
	s.mu.Unlock()

	return s.saveMembership(ctx, *pack.ForYou)
}

// myMembership vraća potvrdu ovog čvora, ako je ima
func (s *Service) myMembership(ctx context.Context) *syncnet.Membership {
	m, _ := s.getMembership(ctx, s.node.ID)
	return m
}

func (s *Service) getMembership(ctx context.Context, nodeID string) (*syncnet.Membership, error) {
	var m syncnet.Membership
	err := s.db.QueryRowContext(ctx, `
		SELECT node_id, public_key, network, issued_by, issued_at, expires_at, signature
		FROM memberships WHERE node_id = ?`, nodeID).
		Scan(&m.DeviceID, &m.DeviceKey, &m.Network, &m.IssuedBy, &m.IssuedAt, &m.ExpiresAt, &m.Signature)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// saveMembership upisuje potvrdu i ostavlja verziju — članstva se
// sinkroniziraju, pa i čvor koji nije bio prisutan pri primanju sazna za
// novog člana
func (s *Service) saveMembership(ctx context.Context, m syncnet.Membership) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO memberships (node_id, public_key, network, issued_by, issued_at, expires_at, signature, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			public_key = excluded.public_key, network = excluded.network, issued_by = excluded.issued_by,
			issued_at = excluded.issued_at, expires_at = excluded.expires_at, signature = excluded.signature
	`, m.DeviceID, m.DeviceKey, m.Network, m.IssuedBy, m.IssuedAt, m.ExpiresAt, m.Signature, time.Now().UTC()); err != nil {
		return err
	}
	if _, err := s.rec.Record(ctx, tx, EntityMemberships, m.DeviceID, m); err != nil {
		return err
	}
	return tx.Commit()
}

// RevokeMembership opoziva člana: potvrda se arhivira i to putuje na sve
// čvorove; od tada mu nitko ne odgovara na razmjenu
func (s *Service) RevokeMembership(ctx context.Context, nodeID string) error {
	if nodeID == s.node.ID {
		return fmt.Errorf("čvor ne može opozvati sam sebe")
	}
	m, err := s.getMembership(ctx, nodeID)
	if err != nil {
		return err
	}
	if m == nil {
		return fmt.Errorf("čvor %s nije član", nodeID)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM memberships WHERE node_id = ?`, nodeID); err != nil {
		return err
	}
	if _, err := s.rec.Archive(ctx, tx, EntityMemberships, nodeID, m); err != nil {
		return err
	}
	return tx.Commit()
}

// Member je član za prikaz
type Member struct {
	syncnet.Membership
	Valid   bool   `json:"valid"`
	Problem string `json:"problem,omitempty"`
	IsSelf  bool   `json:"is_self"`
}

// ListMembers vraća sve poznate potvrde, s ocjenom vrijede li za našu mrežu
func (s *Service) ListMembers(ctx context.Context) ([]Member, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT node_id, public_key, network, issued_by, issued_at, expires_at, signature
		FROM memberships ORDER BY issued_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	s.mu.Lock()
	network := s.network
	s.mu.Unlock()

	var out []Member
	for rows.Next() {
		var m syncnet.Membership
		if err := rows.Scan(&m.DeviceID, &m.DeviceKey, &m.Network, &m.IssuedBy, &m.IssuedAt, &m.ExpiresAt, &m.Signature); err != nil {
			return nil, err
		}
		member := Member{Membership: m, IsSelf: m.DeviceID == s.node.ID}
		if network == nil {
			member.Problem = "čvor nije ni u jednoj mreži"
		} else if pub, err := syncnet.ParsePublicKey(m.DeviceKey); err != nil {
			member.Problem = "neispravan ključ"
		} else if err := m.Verify(network.Public, pub, time.Now()); err != nil {
			member.Problem = err.Error()
		} else {
			member.Valid = true
		}
		out = append(out, member)
	}
	return out, rows.Err()
}

// trusted je provjera na vratima razmjene: ključ mora imati važeću
// potvrdu NAŠE mreže. "Postoji u popisu" nije dovoljno — popis putuje.
func (s *Service) trusted(pub ed25519.PublicKey) bool {
	s.mu.Lock()
	network := s.network
	s.mu.Unlock()
	if network == nil {
		return false
	}

	var m syncnet.Membership
	err := s.db.QueryRow(`
		SELECT node_id, public_key, network, issued_by, issued_at, expires_at, signature
		FROM memberships WHERE public_key = ?`, syncnet.PublicKeyString(pub)).
		Scan(&m.DeviceID, &m.DeviceKey, &m.Network, &m.IssuedBy, &m.IssuedAt, &m.ExpiresAt, &m.Signature)
	if err != nil {
		return false
	}
	return m.Verify(network.Public, pub, time.Now()) == nil
}

// welcomeFor sastavlja paket dobrodošlice za uparenog čvora
func (s *Service) welcomeFor(ctx context.Context, peerID string, peerKey ed25519.PublicKey) *welcomePack {
	s.mu.Lock()
	network := s.network
	s.mu.Unlock()
	if network == nil {
		return nil
	}
	pack := &welcomePack{
		NetworkName: network.Name,
		NetworkKey:  syncnet.PublicKeyString(network.Public),
		Mine:        s.myMembership(ctx),
	}
	if network.CanSign() {
		if m, err := network.Admit(peerID, peerKey, s.node.ID, MembershipValidity); err == nil {
			pack.ForYou = &m
		}
	}
	return pack
}

// acceptWelcome obrađuje paket druge strane nakon uspješnog uparivanja.
// Vraća je li drugi čvor sada član naše mreže, i poruku za ekran.
func (s *Service) acceptWelcome(ctx context.Context, raw json.RawMessage, peerID string, peerKey ed25519.PublicKey, given *welcomePack) (bool, string, error) {
	var theirs welcomePack
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &theirs); err != nil {
			return false, "", fmt.Errorf("paket dobrodošlice se ne može pročitati: %w", err)
		}
	}

	s.mu.Lock()
	network := s.network
	s.mu.Unlock()

	// nismo ni u jednoj mreži: jedini način unutra je da nas druga strana primi
	if network == nil {
		if err := s.joinNetwork(ctx, theirs); err != nil {
			return false, "", err
		}
		if theirs.Mine != nil {
			_ = s.saveMembership(ctx, *theirs.Mine)
		}
		return true, fmt.Sprintf("Ovaj čvor je primljen u mrežu %q.", theirs.NetworkName), nil
	}

	// u mreži smo: druga strana je ili iste mreže, ili ju mi primamo, ili ništa
	ourKey := syncnet.PublicKeyString(network.Public)
	if theirs.NetworkKey != "" && theirs.NetworkKey != ourKey {
		return false, "", fmt.Errorf("čvor %s pripada drugoj mreži (%q) — ne može biti član naše", peerID, theirs.NetworkName)
	}
	if theirs.Mine != nil && theirs.Mine.Verify(network.Public, peerKey, time.Now()) == nil {
		_ = s.saveMembership(ctx, *theirs.Mine)
		return true, "Čvor je već član naše mreže.", nil
	}
	if given != nil && given.ForYou != nil {
		if err := s.saveMembership(ctx, *given.ForYou); err != nil {
			return false, "", err
		}
		return true, fmt.Sprintf("Čvor %s je primljen u mrežu %q.", peerID, network.Name), nil
	}
	return false, fmt.Sprintf("Čvor %s je uparen, ali NIJE član mreže — primiti ga može samo nositelj mrežnog ključa.", peerID), nil
}
