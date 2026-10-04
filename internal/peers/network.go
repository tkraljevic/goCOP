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

	"gocop/internal/razmjena"
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
	Name      string           `json:"name"`
	PublicKey string           `json:"public_key"`
	JoinedAt  time.Time        `json:"joined_at"`
	CanAdmit  bool             `json:"can_admit"`        // ovaj čvor smije primati članove
	DrziKljuc bool             `json:"drzi_kljuc"`       // ovaj čvor drži privatni ključ mreže
	Ovlast    *razmjena.Ovlast `json:"ovlast,omitempty"` // ovlast ovog čvora za primanje, kad je ima
}

// welcomePack je ono što se preda pri uparivanju: tko smo (mreža), moja
// potvrda (da me drugi može provjeriti) i, kad je mogu izdati, potvrda za
// drugoga — pa je član onog trena kad oba čovjeka potvrde kod.
type welcomePack struct {
	NetworkName string               `json:"network_name,omitempty"`
	NetworkKey  string               `json:"network_key,omitempty"`
	Mine        *razmjena.Membership `json:"mine,omitempty"`
	ForYou      *razmjena.Membership `json:"for_you,omitempty"`
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
	pubKey, err := razmjena.ParsePublicKey(pub)
	if err != nil {
		return fmt.Errorf("javni ključ mreže u bazi je neispravan: %w", err)
	}

	key := razmjena.PublicNetwork(name, pubKey)
	if priv, err := razmjena.LoadKey(s.networkKeyPath()); err == nil {
		if !priv.Public().(ed25519.PublicKey).Equal(pubKey) {
			return fmt.Errorf("datoteka %s ne pripada mreži %q iz baze", s.networkKeyPath(), name)
		}
		key = razmjena.LoadNetworkKey(name, priv)
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
	if s.network == nil {
		s.mu.Unlock()
		return nil
	}
	n := &Network{
		Name:      s.network.Name,
		PublicKey: razmjena.PublicKeyString(s.network.Public),
		JoinedAt:  s.joinedAt,
		CanAdmit:  s.network.CanSign(),
		DrziKljuc: s.network.CanSign(),
	}
	s.mu.Unlock()
	if !n.DrziKljuc {
		if o := s.mojaOvlast(context.Background()); o != nil {
			n.CanAdmit, n.Ovlast = true, o
		}
	}
	return n
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

	// Datoteka ključa bez zapisa o mreži ostaje kad baza nastane iznova
	// (ili se mreža osnivala dok baza još nije bila ta). Takav ključ nije
	// nikoga primio u ovu bazu, pa ga osnivanje preuzme umjesto da odbije
	// ili ga pregazi: ključ se nikad ne briše sam.
	var key razmjena.NetworkKey
	if priv, err := razmjena.LoadKey(s.networkKeyPath()); err == nil {
		key = razmjena.LoadNetworkKey(name, priv)
	} else if errors.Is(err, os.ErrNotExist) {
		if key, err = razmjena.NewNetwork(name); err != nil {
			return err
		}
		if err := razmjena.SaveKey(s.networkKeyPath(), key.Private()); err != nil {
			return fmt.Errorf("ključ mreže se ne može zapisati: %w", err)
		}
	} else {
		return fmt.Errorf("zatečena datoteka ključa mreže se ne čita: %w", err)
	}

	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO network (id, name, public_key, joined_at) VALUES (1, ?, ?, ?)`,
		name, razmjena.PublicKeyString(key.Public), now); err != nil {
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
		return fmt.Errorf("druga strana nije nositelj mrežnog ključa ni ovlašteni primatelj — čvor je uparen, ali nije primljen u mrežu; primiti ga mora netko tko drži ključ mreže ili ovlast za primanje")
	}
	pubKey, err := razmjena.ParsePublicKey(pack.NetworkKey)
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
	key := razmjena.PublicNetwork(pack.NetworkName, pubKey)
	s.mu.Lock()
	s.network = &key
	s.joinedAt = now
	s.mu.Unlock()

	return s.saveMembership(ctx, *pack.ForYou)
}

// myMembership vraća potvrdu ovog čvora, ako je ima
func (s *Service) myMembership(ctx context.Context) *razmjena.Membership {
	m, _ := s.getMembership(ctx, s.node.ID)
	return m
}

// stupciClanstva su stupci tablice memberships redom koji čita scanMembership
const stupciClanstva = `node_id, public_key, network, issued_by, issued_at, expires_at, signature, primatelj`

// scanMembership čita redak članstva; primatelj je ovlast potpisnika (JSON)
// kad članstvo nije potpisao ključ mreže
func scanMembership(sc interface{ Scan(...any) error }) (razmjena.Membership, error) {
	var m razmjena.Membership
	var primatelj string
	if err := sc.Scan(&m.DeviceID, &m.DeviceKey, &m.Network, &m.IssuedBy, &m.IssuedAt, &m.ExpiresAt, &m.Signature, &primatelj); err != nil {
		return m, err
	}
	if primatelj != "" {
		var o razmjena.Ovlast
		if err := json.Unmarshal([]byte(primatelj), &o); err != nil {
			return m, fmt.Errorf("ovlast primatelja u članstvu %s: %w", m.DeviceID, err)
		}
		m.Primatelj = &o
	}
	return m, nil
}

// primateljJSON je ovlast potpisnika za stupac primatelj; prazno kad je
// članstvo potpisao ključ mreže
func primateljJSON(m razmjena.Membership) string {
	if m.Primatelj == nil {
		return ""
	}
	b, _ := json.Marshal(m.Primatelj.UTC())
	return string(b)
}

func (s *Service) getMembership(ctx context.Context, nodeID string) (*razmjena.Membership, error) {
	m, err := scanMembership(s.db.QueryRowContext(ctx, `SELECT `+stupciClanstva+` FROM memberships WHERE node_id = ?`, nodeID))
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
func (s *Service) saveMembership(ctx context.Context, m razmjena.Membership) error {
	return s.uTransakciji(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
		INSERT INTO memberships (node_id, public_key, network, issued_by, issued_at, expires_at, signature, created_at, primatelj)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			public_key = excluded.public_key, network = excluded.network, issued_by = excluded.issued_by,
			issued_at = excluded.issued_at, expires_at = excluded.expires_at, signature = excluded.signature,
			primatelj = excluded.primatelj
	`, m.DeviceID, m.DeviceKey, m.Network, m.IssuedBy, m.IssuedAt.UTC(), m.ExpiresAt.UTC(), m.Signature, time.Now().UTC(), primateljJSON(m)); err != nil {
			return err
		}
		_, err := s.rec.Record(ctx, tx, EntityMemberships, m.DeviceID, m)
		return err
	})
}

// uTransakciji izvede fn u jednoj transakciji: sve ili ništa
func (s *Service) uTransakciji(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// RevokeMembership opoziva člana: potvrda se arhivira, uz zapis opoziva, i
// to putuje na sve čvorove; od tada mu nitko ne odgovara na razmjenu, ni
// kad potvrdu pokaže sam. Ako je bio ovlašteni primatelj, opoziva mu se i
// ovlast, a time i članstva koja je potpisao; vraća te čvorove.
func (s *Service) RevokeMembership(ctx context.Context, nodeID string) ([]string, error) {
	if nodeID == s.node.ID {
		return nil, fmt.Errorf("čvor ne može opozvati sam sebe")
	}
	m, err := s.getMembership(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, fmt.Errorf("čvor %s nije član", nodeID)
	}
	o, err := s.getOvlast(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	priv, err := s.potpisnikOpozivaClanstva(*m, o)
	if err != nil {
		return nil, err
	}
	var pogodjeni []string
	err = s.uTransakciji(ctx, func(tx *sql.Tx) error {
		if err := s.opozoviClanstvoTx(ctx, tx, *m, priv); err != nil || o == nil {
			return err
		}
		pogodjeni, err = s.opozoviOvlastTx(ctx, tx, *o, priv)
		return err
	})
	return pogodjeni, err
}

// potpisnikOpozivaClanstva je ključ kojim ovaj čvor smije opozvati
// članstvo: ključ mreže, ili ključ ovog čvora kad je on primatelj koji je
// to članstvo izdao. Član s ovlašću za primanje opoziva samo ključ mreže,
// jer mu se opoziva i ovlast.
func (s *Service) potpisnikOpozivaClanstva(m razmjena.Membership, o *razmjena.Ovlast) (ed25519.PrivateKey, error) {
	s.mu.Lock()
	network := s.network
	s.mu.Unlock()
	switch {
	case network != nil && network.CanSign():
		return network.Private(), nil
	case o != nil:
		return nil, fmt.Errorf("čvor %s ima ovlast za primanje: opozvati ga može samo čvor koji drži ključ mreže", m.DeviceID)
	case m.Primatelj != nil && m.Primatelj.DeviceKey == s.node.PublicKey():
		return s.node.key, nil
	}
	return nil, fmt.Errorf("članstvo čvora %s opoziva nositelj ključa mreže ili primatelj koji ga je izdao", m.DeviceID)
}

// opozoviClanstvoTx briše članstvo s površine, arhivira ga u knjizi i
// zapisuje opoziv potpisan ključem priv (vrijedi i za potvrdu koju čvor
// pokaže sam)
func (s *Service) opozoviClanstvoTx(ctx context.Context, tx *sql.Tx, m razmjena.Membership, priv ed25519.PrivateKey) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM memberships WHERE node_id = ?`, m.DeviceID); err != nil {
		return err
	}
	if _, err := s.rec.Archive(ctx, tx, EntityMemberships, m.DeviceID, m); err != nil {
		return err
	}
	return s.zapisiOpoziv(ctx, tx, priv, OpozivClanstva, m.DeviceID, m.DeviceKey, m.IssuedAt)
}

// Member je član za prikaz
type Member struct {
	razmjena.Membership
	Valid   bool   `json:"valid"`
	Problem string `json:"problem,omitempty"`
	IsSelf  bool   `json:"is_self"`
	// OvlastZaPrimanje je ovlast ovog člana da prima druge, kad je ima
	OvlastZaPrimanje *Primatelj `json:"ovlast_za_primanje,omitempty"`
}

// ListMembers vraća sve poznate potvrde, s ocjenom vrijede li za našu mrežu
func (s *Service) ListMembers(ctx context.Context) ([]Member, error) {
	clanstva, err := s.svaClanstva(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	network := s.network
	s.mu.Unlock()

	ovlasti := map[string]*Primatelj{}
	if svi, err := s.ListOvlasti(ctx); err == nil {
		for i := range svi {
			ovlasti[svi[i].DeviceID] = &svi[i]
		}
	}

	out := make([]Member, 0, len(clanstva))
	for _, m := range clanstva {
		member := Member{Membership: m, IsSelf: m.DeviceID == s.node.ID}
		if o := ovlasti[m.DeviceID]; o != nil && o.DeviceKey == m.DeviceKey {
			member.OvlastZaPrimanje = o
		}
		member.Problem = s.problemClanstva(ctx, network, m)
		member.Valid = member.Problem == ""
		out = append(out, member)
	}
	return out, nil
}

// svaClanstva su sva poznata članstva, najstarija prva; čitaju se do kraja
// prije ocjene, jer ocjena čita opozive iz iste baze
func (s *Service) svaClanstva(ctx context.Context) ([]razmjena.Membership, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+stupciClanstva+` FROM memberships ORDER BY issued_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var clanstva []razmjena.Membership
	for rows.Next() {
		m, err := scanMembership(rows)
		if err != nil {
			return nil, err
		}
		clanstva = append(clanstva, m)
	}
	return clanstva, rows.Err()
}

// problemClanstva je razlog zbog kojeg članstvo ne vrijedi za našu mrežu,
// ili "" kad vrijedi
func (s *Service) problemClanstva(ctx context.Context, network *razmjena.NetworkKey, m razmjena.Membership) string {
	if network == nil {
		return "čvor nije ni u jednoj mreži"
	}
	pub, err := razmjena.ParsePublicKey(m.DeviceKey)
	if err != nil {
		return "neispravan ključ"
	}
	if err := s.provjeriClanstvo(ctx, network.Public, m, pub); err != nil {
		return err.Error()
	}
	return ""
}

// welcomeFor sastavlja paket dobrodošlice za uparenog čvora; potvrdu
// članstva izdaje samo kad je primi (ovlašten čovjek potvrđuje) i kad ovaj
// čvor smije primati (ključ mreže ili ovlast za primanje).
// Paket bez potvrde je isti kao onaj čvora bez ključa mreže, pa ga i stariji
// programi razumiju.
func (s *Service) welcomeFor(ctx context.Context, peerID string, peerKey ed25519.PublicKey, primi bool) *welcomePack {
	s.mu.Lock()
	network := s.network
	s.mu.Unlock()
	if network == nil {
		return nil
	}
	pack := &welcomePack{
		NetworkName: network.Name,
		NetworkKey:  razmjena.PublicKeyString(network.Public),
		Mine:        s.myMembership(ctx),
	}
	if primi {
		if m, err := s.izdajClanstvo(ctx, peerID, peerKey); err == nil {
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
	ourKey := razmjena.PublicKeyString(network.Public)
	if theirs.NetworkKey != "" && theirs.NetworkKey != ourKey {
		return false, "", fmt.Errorf("čvor %s pripada drugoj mreži (%q) — ne može biti član naše", peerID, theirs.NetworkName)
	}
	if theirs.Mine != nil && s.provjeriClanstvo(ctx, network.Public, *theirs.Mine, peerKey) == nil {
		_ = s.saveMembership(ctx, *theirs.Mine)
		return true, "Čvor je već član naše mreže.", nil
	}
	if given != nil && given.ForYou != nil {
		if err := s.saveMembership(ctx, *given.ForYou); err != nil {
			return false, "", err
		}
		return true, fmt.Sprintf("Čvor %s je primljen u mrežu %q.", peerID, network.Name), nil
	}
	return false, fmt.Sprintf("Čvor %s je uparen, ali NIJE član mreže — primiti ga može samo nositelj mrežnog ključa ili ovlašteni primatelj.", peerID), nil
}
