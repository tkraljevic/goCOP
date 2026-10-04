package peers

// Ovlašteni primatelj i opozivi.
//
// Nositelj ključa mreže može članu dati ovlast da prima druge (npr. uredski
// poslužitelj prima uredska računala, a ključ mreže ostaje na USB-u).
// Članstvo koje primatelj potpiše nosi njegovu ovlast, pa ga svaki član
// provjeri sam: ključ mreže → ovlast → članstvo.
//
// Čvor svoju potvrdu (i ovlast, ako je ima) pokazuje u certifikatu
// razmjene, pa ga drugi član prima i prije nego što mu potvrda stigne
// knjigom — nije ga potrebno upariti sa svakim čvorom.
//
// Opoziv je zapis u knjizi (opozivi): potvrda istog ključa izdana tada ili
// ranije više ne vrijedi, iako potpis i rok štimaju. Opozvana ovlast
// poništava i sva članstva koja je primatelj potpisao.

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gocop/internal/razmjena"
)

// EntityOvlasti i EntityOpozivi su nazivi entiteta u knjizi verzija
const (
	EntityOvlasti = "ovlasti"
	EntityOpozivi = "opozivi"
)

// OvlastValidity je rok ovlasti za primanje; članstvo koje primatelj izda
// ne traje dulje od njegove ovlasti
const OvlastValidity = 2 * MembershipValidity

// Vrste opoziva
const (
	OpozivClanstva = "clanstvo"
	OpozivOvlasti  = "ovlast"
)

// ErrOpozvano: potvrda je opozvana
var ErrOpozvano = errors.New("potvrda je opozvana")

// vjerodajniceCvora su ono što čvor pokazuje u certifikatu razmjene
type vjerodajniceCvora struct {
	Clanstvo *razmjena.Membership `json:"clanstvo,omitempty"`
	Ovlast   *razmjena.Ovlast     `json:"ovlast,omitempty"`
}

// vjerodajnice su potvrda ovog čvora i njegova ovlast za primanje (JSON),
// za certifikat razmjene; nil dok čvor nije član
func (s *Service) vjerodajnice() []byte {
	ctx := context.Background()
	m := s.myMembership(ctx)
	if m == nil {
		return nil
	}
	b, _ := json.Marshal(vjerodajniceCvora{Clanstvo: m, Ovlast: s.mojaOvlast(ctx)}) // zapis ne pada
	return b
}

// Opoziv je zapis da potvrda (članstvo ili ovlast) više ne vrijedi. Zapis
// putuje knjigom kao i svaki drugi, pa vrijedi samo potpisan: opoziv ovlasti
// ključem mreže, opoziv članstva ključem mreže ili primatelja koji je to
// članstvo izdao. Nepotpisan ili tuđe potpisan opoziv ne opoziva ništa.
type Opoziv struct {
	ID         string    `json:"id"`
	Vrsta      string    `json:"vrsta"` // OpozivClanstva | OpozivOvlasti
	NodeID     string    `json:"nodeId"`
	PublicKey  string    `json:"publicKey"`
	IssuedAt   time.Time `json:"issuedAt"` // opozvana je potvrda izdana tada i sve starije istog ključa
	OpozvanoAt time.Time `json:"opozvanoAt"`
	Opozvao    string    `json:"opozvao"`
	Potpisnik  string    `json:"potpisnik"` // javni ključ koji je potpisao opoziv
	Potpis     string    `json:"potpis"`
}

func (op Opoziv) signedBytes() []byte {
	b, _ := json.Marshal(struct {
		V          string `json:"v"`
		Vrsta      string `json:"vrsta"`
		NodeID     string `json:"nodeId"`
		PublicKey  string `json:"publicKey"`
		IssuedAt   int64  `json:"issuedAt"`
		OpozvanoAt int64  `json:"opozvanoAt"`
		Opozvao    string `json:"opozvao"`
		Potpisnik  string `json:"potpisnik"`
	}{"opoziv/1", op.Vrsta, op.NodeID, op.PublicKey, op.IssuedAt.Unix(), op.OpozvanoAt.Unix(), op.Opozvao, op.Potpisnik})
	return b
}

// potpisao javlja je li opoziv potpisao jedan od zadanih ključeva
func (op Opoziv) potpisao(kljucevi ...ed25519.PublicKey) bool {
	potpis, err := base64.StdEncoding.DecodeString(op.Potpis)
	if err != nil {
		return false
	}
	for _, k := range kljucevi {
		if op.Potpisnik == razmjena.PublicKeyString(k) && ed25519.Verify(k, op.signedBytes(), potpis) {
			return true
		}
	}
	return false
}

const stupciOpoziva = `id, vrsta, node_id, public_key, issued_at, opozvano_at, opozvao, potpisnik, potpis`

func scanOpoziv(sc interface{ Scan(...any) error }) (Opoziv, error) {
	var op Opoziv
	err := sc.Scan(&op.ID, &op.Vrsta, &op.NodeID, &op.PublicKey, &op.IssuedAt, &op.OpozvanoAt, &op.Opozvao, &op.Potpisnik, &op.Potpis)
	return op, err
}

// ListOpozivi vraća sve opozive, najnovije prve
func (s *Service) ListOpozivi(ctx context.Context) ([]Opoziv, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+stupciOpoziva+` FROM opozivi ORDER BY opozvano_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Opoziv
	for rows.Next() {
		op, err := scanOpoziv(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, op)
	}
	return out, rows.Err()
}

// zapisiOpoziv potpiše opoziv zadanim ključem, upiše ga i ostavi verziju.
// Oznaka uključuje sažetak potpisa: zapis s istom vrstom, ključem i
// trenutkom, a lažnim potpisom ne može zauzeti mjesto pravog opoziva.
func (s *Service) zapisiOpoziv(ctx context.Context, tx *sql.Tx, priv ed25519.PrivateKey, vrsta, nodeID, kljuc string, izdano time.Time) error {
	op := Opoziv{
		Vrsta: vrsta, NodeID: nodeID, PublicKey: kljuc, IssuedAt: izdano.UTC().Truncate(time.Second),
		OpozvanoAt: time.Now().UTC().Truncate(time.Second), Opozvao: s.node.ID,
		Potpisnik: razmjena.PublicKeyString(priv.Public().(ed25519.PublicKey)),
	}
	potpis := ed25519.Sign(priv, op.signedBytes())
	op.Potpis = base64.StdEncoding.EncodeToString(potpis)
	sazetak := sha256.Sum256(potpis)
	op.ID = fmt.Sprintf("%s:%s:%d:%s", op.Vrsta, op.PublicKey, op.IssuedAt.Unix(), hex.EncodeToString(sazetak[:8]))
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO opozivi (`+stupciOpoziva+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING
	`, op.ID, op.Vrsta, op.NodeID, op.PublicKey, op.IssuedAt, op.OpozvanoAt, op.Opozvao, op.Potpisnik, op.Potpis); err != nil {
		return err
	}
	_, err := s.rec.Record(ctx, tx, EntityOpozivi, op.ID, op)
	return err
}

// opozvano javlja je li potvrda te vrste i ključa, izdana u trenutku
// izdano, opozvana opozivom koji je potpisao jedan od ovlaštenih ključeva.
// Kad se opozivi ne mogu pročitati, potvrda ne vrijedi.
func (s *Service) opozvano(ctx context.Context, vrsta, kljuc string, izdano time.Time, ovlasteni ...ed25519.PublicKey) bool {
	rows, err := s.db.QueryContext(ctx, `SELECT `+stupciOpoziva+` FROM opozivi WHERE vrsta = ? AND public_key = ?`, vrsta, kljuc)
	if err != nil {
		return true
	}
	defer rows.Close()
	for rows.Next() {
		op, err := scanOpoziv(rows)
		if err != nil {
			return true
		}
		if op.IssuedAt.Unix() >= izdano.Unix() && op.potpisao(ovlasteni...) {
			return true
		}
	}
	return rows.Err() != nil
}

// provjeriClanstvo: potpis i rok (i lanac kroz ovlast primatelja), te da
// ni članstvo ni ovlast njegova primatelja nisu opozvani
func (s *Service) provjeriClanstvo(ctx context.Context, networkPub ed25519.PublicKey, m razmjena.Membership, pub ed25519.PublicKey) error {
	if err := m.Verify(networkPub, pub, time.Now()); err != nil {
		return err
	}
	if s.opozvano(ctx, OpozivClanstva, m.DeviceKey, m.IssuedAt, opozivateljiClanstva(networkPub, m)...) {
		return fmt.Errorf("%w: članstvo čvora %s", ErrOpozvano, m.DeviceID)
	}
	if o := m.Primatelj; o != nil && s.opozvano(ctx, OpozivOvlasti, o.DeviceKey, o.IssuedAt, networkPub) {
		return fmt.Errorf("%w: ovlast primatelja %s, koji je primio %s", ErrOpozvano, o.DeviceID, m.DeviceID)
	}
	return nil
}

// opozivateljiClanstva su ključevi čiji opoziv članstva vrijedi: ključ
// mreže i primatelj koji je to članstvo izdao (Verify je već provjerio
// njegov ključ)
func opozivateljiClanstva(networkPub ed25519.PublicKey, m razmjena.Membership) []ed25519.PublicKey {
	if m.Primatelj == nil {
		return []ed25519.PublicKey{networkPub}
	}
	primatelj, _ := razmjena.ParsePublicKey(m.Primatelj.DeviceKey)
	return []ed25519.PublicKey{networkPub, primatelj}
}

// provjeriOvlast: potpis ključa mreže, rok i opoziv
func (s *Service) provjeriOvlast(ctx context.Context, networkPub ed25519.PublicKey, o razmjena.Ovlast) error {
	if err := o.Verify(networkPub, time.Now()); err != nil {
		return err
	}
	if s.opozvano(ctx, OpozivOvlasti, o.DeviceKey, o.IssuedAt, networkPub) {
		return fmt.Errorf("%w: ovlast čvora %s", ErrOpozvano, o.DeviceID)
	}
	return nil
}

const stupciOvlasti = `node_id, public_key, network, issued_by, issued_at, expires_at, signature`

func scanOvlast(sc interface{ Scan(...any) error }) (razmjena.Ovlast, error) {
	var o razmjena.Ovlast
	err := sc.Scan(&o.DeviceID, &o.DeviceKey, &o.Network, &o.IssuedBy, &o.IssuedAt, &o.ExpiresAt, &o.Signature)
	return o, err
}

func (s *Service) getOvlast(ctx context.Context, nodeID string) (*razmjena.Ovlast, error) {
	o, err := scanOvlast(s.db.QueryRowContext(ctx, `SELECT `+stupciOvlasti+` FROM ovlasti WHERE node_id = ?`, nodeID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// saveOvlast upisuje ovlast i ostavlja verziju (ovlasti putuju kao članstva)
func (s *Service) saveOvlast(ctx context.Context, o razmjena.Ovlast) error {
	return s.uTransakciji(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
		INSERT INTO ovlasti (node_id, public_key, network, issued_by, issued_at, expires_at, signature, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			public_key = excluded.public_key, network = excluded.network, issued_by = excluded.issued_by,
			issued_at = excluded.issued_at, expires_at = excluded.expires_at, signature = excluded.signature
	`, o.DeviceID, o.DeviceKey, o.Network, o.IssuedBy, o.IssuedAt.UTC(), o.ExpiresAt.UTC(), o.Signature, time.Now().UTC()); err != nil {
			return err
		}
		_, err := s.rec.Record(ctx, tx, EntityOvlasti, o.DeviceID, o)
		return err
	})
}

// mojaOvlast je važeća ovlast ovog čvora za primanje, ili nil
func (s *Service) mojaOvlast(ctx context.Context) *razmjena.Ovlast {
	s.mu.Lock()
	network := s.network
	s.mu.Unlock()
	if network == nil {
		return nil
	}
	o, err := s.getOvlast(ctx, s.node.ID)
	if err != nil || o == nil || o.DeviceKey != s.node.PublicKey() {
		return nil
	}
	if s.provjeriOvlast(ctx, network.Public, *o) != nil {
		return nil
	}
	return o
}

// izdajClanstvo prima uređaj u mrežu: ključem mreže kad ga ovaj čvor drži,
// inače ključem ovog čvora uz njegovu ovlast
func (s *Service) izdajClanstvo(ctx context.Context, id string, kljuc ed25519.PublicKey) (razmjena.Membership, error) {
	s.mu.Lock()
	network := s.network
	s.mu.Unlock()
	switch {
	case network == nil:
		return razmjena.Membership{}, fmt.Errorf("ovaj čvor nije ni u jednoj mreži")
	case network.CanSign():
		return network.Admit(id, kljuc, s.node.ID, MembershipValidity)
	}
	if o := s.mojaOvlast(ctx); o != nil {
		return razmjena.AdmitAs(s.node.key, *o, id, kljuc, MembershipValidity)
	}
	return razmjena.Membership{}, fmt.Errorf("ovaj čvor ne smije primati članove: nema ključ mreže ni ovlast za primanje")
}

// IzdajOvlast daje članu ovlast da prima druge. Samo nositelj ključa mreže.
func (s *Service) IzdajOvlast(ctx context.Context, nodeID string) (razmjena.Ovlast, error) {
	s.mu.Lock()
	network := s.network
	s.mu.Unlock()
	if network == nil {
		return razmjena.Ovlast{}, fmt.Errorf("ovaj čvor nije ni u jednoj mreži")
	}
	if nodeID == s.node.ID {
		return razmjena.Ovlast{}, fmt.Errorf("ovaj čvor drži ključ mreže; ovlast mu ne treba")
	}
	m, err := s.getMembership(ctx, nodeID)
	if err != nil {
		return razmjena.Ovlast{}, err
	}
	if m == nil {
		return razmjena.Ovlast{}, fmt.Errorf("čvor %s nije član mreže", nodeID)
	}
	kljuc, err := razmjena.ParsePublicKey(m.DeviceKey)
	if err != nil {
		return razmjena.Ovlast{}, err
	}
	if err := s.provjeriClanstvo(ctx, network.Public, *m, kljuc); err != nil {
		return razmjena.Ovlast{}, fmt.Errorf("članstvo čvora %s ne vrijedi: %w", nodeID, err)
	}
	o, err := network.Ovlasti(nodeID, kljuc, s.node.ID, OvlastValidity)
	if err != nil {
		return razmjena.Ovlast{}, err
	}
	return o, s.saveOvlast(ctx, o)
}

// OpozoviOvlast oduzima članu ovlast za primanje. Sva članstva koja je
// potpisao time prestaju vrijediti; vraća te čvorove (treba ih primiti
// ponovno, ključem mreže ili drugim primateljem). Samo nositelj ključa mreže.
func (s *Service) OpozoviOvlast(ctx context.Context, nodeID string) ([]string, error) {
	s.mu.Lock()
	network := s.network
	s.mu.Unlock()
	if network == nil || !network.CanSign() {
		return nil, fmt.Errorf("ovlast za primanje opoziva samo čvor koji drži ključ mreže")
	}
	o, err := s.getOvlast(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if o == nil {
		return nil, fmt.Errorf("čvor %s nema ovlast za primanje", nodeID)
	}
	var pogodjeni []string
	err = s.uTransakciji(ctx, func(tx *sql.Tx) error {
		pogodjeni, err = s.opozoviOvlastTx(ctx, tx, *o, network.Private())
		return err
	})
	return pogodjeni, err
}

// opozoviOvlastTx arhivira ovlast, zapisuje opoziv potpisan ključem mreže
// (mrezni) i vraća čvorove koje je taj primatelj primio
func (s *Service) opozoviOvlastTx(ctx context.Context, tx *sql.Tx, o razmjena.Ovlast, mrezni ed25519.PrivateKey) ([]string, error) {
	if _, err := tx.ExecContext(ctx, `DELETE FROM ovlasti WHERE node_id = ?`, o.DeviceID); err != nil {
		return nil, err
	}
	if _, err := s.rec.Archive(ctx, tx, EntityOvlasti, o.DeviceID, o); err != nil {
		return nil, err
	}
	if err := s.zapisiOpoziv(ctx, tx, mrezni, OpozivOvlasti, o.DeviceID, o.DeviceKey, o.IssuedAt); err != nil {
		return nil, err
	}
	return primljeniOvlascu(ctx, tx, o)
}

// primljeniOvlascu su čvorovi čija članstva je potpisao primatelj s tom
// ovlašću (ili starijom istog ključa): s njezinim opozivom ne vrijede
func primljeniOvlascu(ctx context.Context, tx *sql.Tx, o razmjena.Ovlast) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT `+stupciClanstva+` FROM memberships WHERE primatelj != '' ORDER BY node_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var pogodjeni []string
	for rows.Next() {
		m, err := scanMembership(rows)
		if err != nil {
			return nil, err
		}
		if m.Primatelj.DeviceKey == o.DeviceKey && m.Primatelj.IssuedAt.Unix() <= o.IssuedAt.Unix() {
			pogodjeni = append(pogodjeni, m.DeviceID)
		}
	}
	return pogodjeni, rows.Err()
}

// Primatelj je član s ovlašću za primanje, za prikaz
type Primatelj struct {
	razmjena.Ovlast
	Valid   bool   `json:"valid"`
	Problem string `json:"problem,omitempty"`
}

// ListOvlasti vraća poznate ovlasti za primanje, s ocjenom vrijede li
func (s *Service) ListOvlasti(ctx context.Context) ([]Primatelj, error) {
	ovlasti, err := s.sveOvlasti(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	network := s.network
	s.mu.Unlock()
	out := make([]Primatelj, 0, len(ovlasti))
	for _, o := range ovlasti {
		p := Primatelj{Ovlast: o}
		if network == nil {
			p.Problem = "čvor nije ni u jednoj mreži"
		} else if err := s.provjeriOvlast(ctx, network.Public, o); err != nil {
			p.Problem = err.Error()
		} else {
			p.Valid = true
		}
		out = append(out, p)
	}
	return out, nil
}

// sveOvlasti su sve poznate ovlasti za primanje; čitaju se do kraja prije
// ocjene, jer ocjena čita opozive iz iste baze
func (s *Service) sveOvlasti(ctx context.Context) ([]razmjena.Ovlast, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+stupciOvlasti+` FROM ovlasti ORDER BY node_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ovlasti []razmjena.Ovlast
	for rows.Next() {
		o, err := scanOvlast(rows)
		if err != nil {
			return nil, err
		}
		ovlasti = append(ovlasti, o)
	}
	return ovlasti, rows.Err()
}

// trusted je provjera na vratima razmjene: ključ mora imati važeću,
// neopozvanu potvrdu NAŠE mreže. "Postoji u popisu" nije dovoljno — popis
// putuje. Kad potvrde tog ključa još nemamo (primio ga je ovlašteni
// primatelj, a knjiga od njega još nije stigla), vrijedi potvrda koju čvor
// pokaže u certifikatu, ako lanac do ključa mreže štima i ime nije tuđe.
func (s *Service) trusted(pub ed25519.PublicKey, vj []byte) bool {
	s.mu.Lock()
	network := s.network
	s.mu.Unlock()
	if network == nil {
		return false
	}
	ctx := context.Background()
	row := s.db.QueryRowContext(ctx, `SELECT `+stupciClanstva+` FROM memberships WHERE public_key = ?`, razmjena.PublicKeyString(pub))
	if m, err := scanMembership(row); err == nil && s.provjeriClanstvo(ctx, network.Public, m, pub) == nil {
		return true
	}
	if len(vj) == 0 {
		return false
	}
	var v vjerodajniceCvora
	if json.Unmarshal(vj, &v) != nil || v.Clanstvo == nil {
		return false
	}
	if s.provjeriClanstvo(ctx, network.Public, *v.Clanstvo, pub) != nil {
		return false
	}
	return s.provjeriImeDrugog(ctx, v.Clanstvo.DeviceID, pub) == nil
}
