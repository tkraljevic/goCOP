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
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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
type Opoziv = razmjena.Opoziv

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
// Oznaka uključuje sažetak potpisa (razmjena.Opoziv.Oznaka). Redak iste
// oznake se zamijeni, pa je opoziv u tablici točno onaj koji je upravo
// potpisan.
func (s *Service) zapisiOpoziv(ctx context.Context, tx *sql.Tx, priv ed25519.PrivateKey, vrsta, nodeID, kljuc string, izdano time.Time) error {
	op := Opoziv{
		Vrsta: vrsta, NodeID: nodeID, PublicKey: kljuc, IssuedAt: izdano.UTC().Truncate(time.Second),
		OpozvanoAt: time.Now().UTC().Truncate(time.Second), Opozvao: s.node.ID,
	}.Potpisi(priv)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO opozivi (`+stupciOpoziva+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			vrsta = excluded.vrsta, node_id = excluded.node_id, public_key = excluded.public_key,
			issued_at = excluded.issued_at, opozvano_at = excluded.opozvano_at, opozvao = excluded.opozvao,
			potpisnik = excluded.potpisnik, potpis = excluded.potpis
	`, op.ID, op.Vrsta, op.NodeID, op.PublicKey, op.IssuedAt, op.OpozvanoAt, op.Opozvao, op.Potpisnik, op.Potpis); err != nil {
		return err
	}
	_, err := s.rec.Record(ctx, tx, EntityOpozivi, op.ID, op)
	return err
}

// opozvano javlja je li potvrda te vrste i ključa, izdana u trenutku
// izdano, opozvana opozivom koji je potpisao jedan od ovlaštenih ključeva.
// Opozivi se čitaju iz tablice i iz svih verzija u knjizi (opoziviKljuca).
// Kad se opozivi ne mogu pročitati, potvrda ne vrijedi.
func (s *Service) opozvano(ctx context.Context, vrsta, kljuc string, izdano time.Time, ovlasteni ...ed25519.PublicKey) bool {
	opozivi, err := s.opoziviKljuca(ctx, vrsta, kljuc)
	if err != nil {
		return true
	}
	for _, op := range opozivi {
		if op.IssuedAt.Unix() >= izdano.Unix() && op.Potpisao(ovlasteni...) {
			return true
		}
	}
	return false
}

// opoziviKljuca su opozivi potvrda te vrste i ključa: redovi tablice i
// opozivi iz svih verzija u knjizi, i arhiviranih (sažimanje čuva svaku,
// akti_sazimanje.go). Čitaju se samo pod oznakom koja počinje vrstom i
// ključem (rasponOznaka), jer je oznaka opoziva izvedena iz njih
// (razmjena.Opoziv.Oznaka).
func (s *Service) opoziviKljuca(ctx context.Context, vrsta, kljuc string) ([]Opoziv, error) {
	od, do := rasponOznaka(vrsta, kljuc)
	uTablici, err := s.opoziviUTablici(ctx, od, do)
	if err != nil {
		return nil, err
	}
	uKnjizi, err := sadrzajiVerzija[Opoziv](ctx, s.db, `SELECT payload FROM record_versions WHERE entity = ? AND entity_id >= ? AND entity_id < ?`,
		EntityOpozivi, od, do)
	return slices.DeleteFunc(append(uTablici, uKnjizi...), func(op Opoziv) bool { return op.Vrsta != vrsta || op.PublicKey != kljuc }), err
}

// rasponOznaka su granice oznaka opoziva te vrste i ključa: od
// "vrsta:ključ:" do prvog niza iza svih koji tako počinju
func rasponOznaka(vrsta, kljuc string) (od, do string) {
	return vrsta + ":" + kljuc + ":", vrsta + ":" + kljuc + ";"
}

// opoziviUTablici su redovi tablice opoziva s oznakom u rasponu [od, do)
func (s *Service) opoziviUTablici(ctx context.Context, od, do string) ([]Opoziv, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+stupciOpoziva+` FROM opozivi WHERE id >= ? AND id < ?`, od, do)
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

// provjeriClanstvo: potpis i rok (i lanac kroz ovlast primatelja), te da
// ni članstvo ni ovlast njegova primatelja nisu opozvani. Isteklo članstvo
// vrijedi još za pričuvnog roka (obnova.go).
func (s *Service) provjeriClanstvo(ctx context.Context, networkPub ed25519.PublicKey, m razmjena.Membership, pub ed25519.PublicKey) error {
	if err := m.Verify(networkPub, pub, time.Now()); err != nil && !uPricuvi(m, err, time.Now()) {
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
	return ovlastIz(ctx, s.db, nodeID)
}

// ovlastIz je red ovlasti računala nodeID u bazi ili transakciji q, ili nil
func ovlastIz(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, nodeID string) (*razmjena.Ovlast, error) {
	o, err := scanOvlast(q.QueryRowContext(ctx, `SELECT `+stupciOvlasti+` FROM ovlasti WHERE node_id = ?`, nodeID))
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
	return s.uTransakciji(ctx, func(tx *sql.Tx) error { return s.upisiOvlast(ctx, tx, o) })
}

// upisiOvlast je saveOvlast u transakciji koju vodi pozivatelj
func (s *Service) upisiOvlast(ctx context.Context, tx *sql.Tx, o razmjena.Ovlast) error {
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
	o := s.mojaOvlast(ctx)
	if o == nil {
		return razmjena.Membership{}, fmt.Errorf("ovaj čvor ne smije primati članove: nema ključ mreže ni ovlast za primanje")
	}
	// Odluke (23. 10. 2026.): uz ovlast treba i vlastito članstvo koje nije
	// isteklo; u pričuvi računalo ne prima i ne obnavlja druge
	if m, err := s.izabranoClanstvo(ctx, s.node.ID); err != nil || m.DeviceKey != s.node.PublicKey() || rokZa(m.ExpiresAt, time.Now(), true).Stanje == RokPricuva {
		return razmjena.Membership{}, fmt.Errorf("ovaj čvor ne smije primati članove: njegovo članstvo u mreži je isteklo; obnovite ga (Ovo računalo i mreža, pod Mreža: Obnovi članstvo)")
	}
	return razmjena.AdmitAs(s.node.key, *o, id, kljuc, MembershipValidity)
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
	defer s.javiPromjenuClanstva()
	s.mu.Lock()
	network := s.network
	s.mu.Unlock()
	if network == nil || !network.CanSign() {
		return nil, fmt.Errorf("ovlast za primanje opoziva samo čvor koji drži ključ mreže")
	}
	ovlasti, err := s.ovlastiCvora(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if len(ovlasti) == 0 {
		return nil, fmt.Errorf("čvor %s nema ovlast za primanje", nodeID)
	}
	var pogodjeni []string
	err = s.uTransakciji(ctx, func(tx *sql.Tx) error {
		pogodjeni, err = s.opozoviOvlastiTx(ctx, tx, nodeID, kljuceviZaOpoziv(nil, ovlasti), ovlasti, network.Private())
		return err
	})
	return pogodjeni, err
}

// ovlastiCvora su ovlasti za primanje računala nodeID koje vrijede
// (ovlastVrijedi): red tablice i ovlasti iz svih verzija u knjizi, i
// arhiviranih.
func (s *Service) ovlastiCvora(ctx context.Context, nodeID string) ([]razmjena.Ovlast, error) {
	s.mu.Lock()
	network := s.network
	s.mu.Unlock()
	if network == nil {
		return nil, fmt.Errorf("ovaj čvor nije ni u jednoj mreži")
	}
	sve, err := sadrzajiVerzija[razmjena.Ovlast](ctx, s.db, `SELECT payload FROM record_versions WHERE entity = ? ORDER BY version_id`, EntityOvlasti)
	if err != nil {
		return nil, err
	}
	red, err := s.getOvlast(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if red != nil {
		sve = append(sve, *red)
	}
	var out []razmjena.Ovlast
	for _, o := range sve {
		if s.ovlastVrijedi(ctx, network.Public, nodeID, o) {
			out = append(out, o)
		}
	}
	return out, nil
}

// ovlastVrijedi: ovlast računala nodeID koju je potpisao ključ mreže i koja
// nije opozvana; rok se ne gleda, jer opoziv pokriva i isteklu
func (s *Service) ovlastVrijedi(ctx context.Context, mreza ed25519.PublicKey, nodeID string, o razmjena.Ovlast) bool {
	return o.DeviceID == nodeID && o.Verify(mreza, o.IssuedAt) == nil && !s.opozvano(ctx, OpozivOvlasti, o.DeviceKey, o.IssuedAt, mreza)
}

// kljuceviZaOpoziv su ključevi čije se ovlasti za primanje opozivaju:
// ključ svakog članstva i svake ovlasti, svaki jednom
func kljuceviZaOpoziv(clanstva []razmjena.Membership, ovlasti []razmjena.Ovlast) []string {
	var out []string
	for _, m := range clanstva {
		out = append(out, m.DeviceKey)
	}
	for _, o := range ovlasti {
		out = append(out, o.DeviceKey)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// opozoviOvlastiTx ključem mreže (mrezni) opoziva ovlasti za primanje
// zadanih ključeva. Opoziv nosi trenutak „sada”, a kad je neka poznata
// ovlast izdana kasnije (sat računala koje ju je izdalo ide naprijed),
// trenutak te ovlasti (trenutakOpoziva). Tako vrijedi za svaku ovlast tih
// ključeva izdanu do sada, i za onu koju ovaj čvor ne zna. Ovlast računala
// nodeID miče s površine i arhivira. Vraća čvorove koje su ti ključevi
// primili.
func (s *Service) opozoviOvlastiTx(ctx context.Context, tx *sql.Tx, nodeID string, kljucevi []string, ovlasti []razmjena.Ovlast, mrezni ed25519.PrivateKey) ([]string, error) {
	if err := s.makniOvlast(ctx, tx, nodeID, ovlasti); err != nil {
		return nil, err
	}
	izdano := trenutakOpoziva(time.Now(), ovlasti)
	for _, k := range kljucevi {
		if err := s.zapisiOpoziv(ctx, tx, mrezni, OpozivOvlasti, nodeID, k, izdano); err != nil {
			return nil, err
		}
	}
	return primljeniKljucevima(ctx, tx, kljucevi)
}

// trenutakOpoziva je trenutak koji nosi opoziv ovlasti: sada, ili kasniji
// trenutak izdavanja neke od zadanih ovlasti
func trenutakOpoziva(sada time.Time, ovlasti []razmjena.Ovlast) time.Time {
	for _, o := range ovlasti {
		if o.IssuedAt.After(sada) {
			sada = o.IssuedAt
		}
	}
	return sada
}

// makniOvlast briše red ovlasti računala nodeID s površine i arhivira ga u
// knjizi: s valjanom ovlašću kad je ovaj čvor zna, inače s onim što je u
// redu stajalo. Bez reda i bez poznate ovlasti nema što maknuti.
func (s *Service) makniOvlast(ctx context.Context, tx *sql.Tx, nodeID string, ovlasti []razmjena.Ovlast) error {
	zadnja, err := ovlastZaSpomenik(ctx, tx, nodeID, ovlasti)
	if err != nil || zadnja == nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM ovlasti WHERE node_id = ?`, nodeID); err != nil {
		return err
	}
	_, err = s.rec.Archive(ctx, tx, EntityOvlasti, nodeID, *zadnja)
	return err
}

// ovlastZaSpomenik je sadržaj spomenika ovlasti računala nodeID: zadnja
// valjana ovlast, ili red tablice kad valjane nema; nil kad nema ni reda
func ovlastZaSpomenik(ctx context.Context, tx *sql.Tx, nodeID string, ovlasti []razmjena.Ovlast) (*razmjena.Ovlast, error) {
	if len(ovlasti) > 0 {
		return &ovlasti[len(ovlasti)-1], nil
	}
	return ovlastIz(ctx, tx, nodeID)
}

// primljeniKljucevima su čvorovi čija je članstva potpisao primatelj s
// jednim od zadanih ključeva: s opozivom njegovih ovlasti ne vrijede
func primljeniKljucevima(ctx context.Context, tx *sql.Tx, kljucevi []string) ([]string, error) {
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
		if slices.Contains(kljucevi, m.Primatelj.DeviceKey) {
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
// neopozvanu potvrdu NAŠE mreže, i to onu koja za njegovo ime vrijedi
// (oznakaDokazanog). "Postoji u popisu" nije dovoljno — popis putuje. Kad
// potvrde tog ključa još nemamo (primio ga je ovlašteni primatelj, a knjiga
// od njega još nije stigla), vrijedi potvrda koju čvor pokaže u
// certifikatu, ako lanac do ključa mreže štima i ime nije tuđe.
func (s *Service) trusted(pub ed25519.PublicKey, vj []byte) bool {
	id, err := s.oznakaDokazanog(context.Background(), pub, vj)
	return err == nil && id != ""
}
