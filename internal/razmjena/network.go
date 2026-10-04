package razmjena

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// A network is a group of devices that trust each other, and it has a key
// of its own. Membership is a signature by the network key over a device's
// public key. This is what keeps two organisations running the same open
// source program from having anything in common: same code, different
// network keys, and a certificate from one means nothing to the other.
//
// Pairing (the six digits) still proves WHICH device is on the other end;
// the certificate proves it is ONE OF US. A device that pairs without a
// valid certificate is known but not trusted — an exchange is refused until
// a holder of the network key admits it.

// NetworkKey is the network's signing key. The private half lives only on
// devices whose owners may admit members; the public half on every member.
type NetworkKey struct {
	Name    string
	Public  ed25519.PublicKey
	private ed25519.PrivateKey
}

// NewNetwork mints a network key. The device that runs this founds the
// network and holds its private key.
func NewNetwork(name string) (NetworkKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return NetworkKey{}, err
	}
	return NetworkKey{Name: name, Public: pub, private: priv}, nil
}

// LoadNetworkKey wraps a stored private key (see LoadOrCreateKey for the
// file format); the caller decides where it lives.
func LoadNetworkKey(name string, priv ed25519.PrivateKey) NetworkKey {
	return NetworkKey{Name: name, Public: priv.Public().(ed25519.PublicKey), private: priv}
}

// PublicNetwork is the public half only — what every member holds.
func PublicNetwork(name string, pub ed25519.PublicKey) NetworkKey {
	return NetworkKey{Name: name, Public: pub}
}

// CanSign reports whether this device holds the private half.
func (n NetworkKey) CanSign() bool { return n.private != nil }

// Private exposes the private half for storage; nil when only the public
// half is held.
func (n NetworkKey) Private() ed25519.PrivateKey { return n.private }

// Membership is a device's admission to a network. It is signed either by
// the network key itself, or by an authorised admitter (ovlašteni
// primatelj): a member the network key has authorised to admit others,
// whose Ovlast travels inside the membership so any member can check the
// chain network key → ovlast → membership without asking anyone.
type Membership struct {
	Network   string    `json:"network"`   // base64 public key of the network
	DeviceID  string    `json:"deviceId"`  // the member
	DeviceKey string    `json:"deviceKey"` // base64 public key of the member
	IssuedBy  string    `json:"issuedBy"`  // device id of the admitter
	IssuedAt  time.Time `json:"issuedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Signature string    `json:"signature"` // base64, over signedBytes()
	// Primatelj je ovlast čvora koji je potpisao ovo članstvo svojim ključem;
	// prazno kad ga je potpisao ključ mreže (sva članstva do 0.0.33)
	Primatelj *Ovlast `json:"primatelj,omitempty"`
}

// signedBytes is the canonical form the signature covers. Field order and
// encoding are fixed here; changing them invalidates every certificate.
// v1 is a membership signed by the network key; v2 one signed by an
// admitter, bound to that admitter's ovlast (its signature), so the ovlast
// cannot be swapped for another one.
func (m Membership) signedBytes() []byte {
	if m.Primatelj != nil {
		b, _ := json.Marshal(struct {
			V         int    `json:"v"`
			Network   string `json:"network"`
			DeviceID  string `json:"deviceId"`
			DeviceKey string `json:"deviceKey"`
			IssuedBy  string `json:"issuedBy"`
			IssuedAt  int64  `json:"issuedAt"`
			ExpiresAt int64  `json:"expiresAt"`
			Ovlast    string `json:"ovlast"`
		}{2, m.Network, m.DeviceID, m.DeviceKey, m.IssuedBy, m.IssuedAt.Unix(), m.ExpiresAt.Unix(), m.Primatelj.Signature})
		return b
	}
	b, _ := json.Marshal(struct {
		V         int    `json:"v"`
		Network   string `json:"network"`
		DeviceID  string `json:"deviceId"`
		DeviceKey string `json:"deviceKey"`
		IssuedBy  string `json:"issuedBy"`
		IssuedAt  int64  `json:"issuedAt"`
		ExpiresAt int64  `json:"expiresAt"`
	}{1, m.Network, m.DeviceID, m.DeviceKey, m.IssuedBy, m.IssuedAt.Unix(), m.ExpiresAt.Unix()})
	return b
}

// Ovlast je dopuštenje ključa mreže jednom članu da prima druge u mrežu
// (ovlašteni primatelj): npr. uredski poslužitelj prima uredska računala, a
// ključ mreže ostaje kod nositelja. Vrijedi do isteka ili opoziva; opoziv
// poništava i sva članstva koja je primatelj potpisao.
type Ovlast struct {
	Network   string    `json:"network"`
	DeviceID  string    `json:"deviceId"`
	DeviceKey string    `json:"deviceKey"`
	IssuedBy  string    `json:"issuedBy"`
	IssuedAt  time.Time `json:"issuedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Signature string    `json:"signature"` // ključ mreže, nad signedBytes()
}

// UTC je ista ovlast s vremenima u UTC-u, za pohranu u istom obliku na
// svim čvorovima (potpis pokriva sekunde, ne zonu)
func (o Ovlast) UTC() Ovlast {
	o.IssuedAt, o.ExpiresAt = o.IssuedAt.UTC(), o.ExpiresAt.UTC()
	return o
}

func (o Ovlast) signedBytes() []byte {
	b, _ := json.Marshal(struct {
		V         string `json:"v"`
		Network   string `json:"network"`
		DeviceID  string `json:"deviceId"`
		DeviceKey string `json:"deviceKey"`
		IssuedBy  string `json:"issuedBy"`
		IssuedAt  int64  `json:"issuedAt"`
		ExpiresAt int64  `json:"expiresAt"`
	}{"ovlast/1", o.Network, o.DeviceID, o.DeviceKey, o.IssuedBy, o.IssuedAt.Unix(), o.ExpiresAt.Unix()})
	return b
}

// Ovlasti potpisuje članu dopuštenje da prima druge, na zadano vrijeme
func (n NetworkKey) Ovlasti(deviceID string, deviceKey ed25519.PublicKey, issuedBy string, validFor time.Duration) (Ovlast, error) {
	if !n.CanSign() {
		return Ovlast{}, errors.New("samo nositelj ključa mreže daje ovlast za primanje")
	}
	now := time.Now().UTC().Truncate(time.Second)
	o := Ovlast{
		Network: PublicKeyString(n.Public), DeviceID: deviceID, DeviceKey: PublicKeyString(deviceKey),
		IssuedBy: issuedBy, IssuedAt: now, ExpiresAt: now.Add(validFor),
	}
	o.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(n.private, o.signedBytes()))
	return o, nil
}

// ErrOvlast: ovlast primatelja ne vrijedi
var ErrOvlast = errors.New("ovlast primatelja ne vrijedi")

// Verify provjerava da je ovlast potpisao ključ mreže i da vrijedi u trenutku at
func (o Ovlast) Verify(networkPub ed25519.PublicKey, at time.Time) error {
	if o.Network != PublicKeyString(networkPub) {
		return fmt.Errorf("%w: druga mreža", ErrOvlast)
	}
	sig, err := base64.StdEncoding.DecodeString(o.Signature)
	if err != nil || !ed25519.Verify(networkPub, o.signedBytes(), sig) {
		return fmt.Errorf("%w: potpis", ErrOvlast)
	}
	if at.Before(o.IssuedAt) || (!o.ExpiresAt.IsZero() && at.After(o.ExpiresAt)) {
		return fmt.Errorf("%w: ne vrijedi %s", ErrOvlast, at.Format(time.RFC3339))
	}
	return nil
}

// AdmitAs prima uređaj ključem ovlaštenog primatelja (priv) uz njegovu
// ovlast. Članstvo ne smije vrijediti dulje od ovlasti.
func AdmitAs(priv ed25519.PrivateKey, o Ovlast, deviceID string, deviceKey ed25519.PublicKey, validFor time.Duration) (Membership, error) {
	if PublicKeyString(priv.Public().(ed25519.PublicKey)) != o.DeviceKey {
		return Membership{}, errors.New("ovlast nije izdana ovom čvoru")
	}
	now := time.Now().UTC().Truncate(time.Second)
	if now.Before(o.IssuedAt) || (!o.ExpiresAt.IsZero() && now.After(o.ExpiresAt)) {
		return Membership{}, errors.New("ovlast za primanje je istekla")
	}
	exp := now.Add(validFor)
	if !o.ExpiresAt.IsZero() && exp.After(o.ExpiresAt) {
		exp = o.ExpiresAt
	}
	oo := o
	m := Membership{
		Network: o.Network, DeviceID: deviceID, DeviceKey: PublicKeyString(deviceKey),
		IssuedBy: o.DeviceID, IssuedAt: now, ExpiresAt: exp, Primatelj: &oo,
	}
	m.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, m.signedBytes()))
	return m, nil
}

// Admit signs a device into the network for the given validity.
func (n NetworkKey) Admit(deviceID string, deviceKey ed25519.PublicKey, issuedBy string, validFor time.Duration) (Membership, error) {
	if !n.CanSign() {
		return Membership{}, errors.New("this device does not hold the network key and cannot admit members")
	}
	now := time.Now().UTC().Truncate(time.Second)
	m := Membership{
		Network:   PublicKeyString(n.Public),
		DeviceID:  deviceID,
		DeviceKey: PublicKeyString(deviceKey),
		IssuedBy:  issuedBy,
		IssuedAt:  now,
		ExpiresAt: now.Add(validFor),
	}
	m.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(n.private, m.signedBytes()))
	return m, nil
}

var (
	ErrNotAMember   = errors.New("no valid membership for this device")
	ErrOtherNetwork = errors.New("the certificate belongs to another network")
	ErrExpired      = errors.New("the membership has expired")
	ErrBadSignature = errors.New("the membership signature does not verify")
	ErrKeyMismatch  = errors.New("the membership names a different device key")
)

// Verify checks that m admits deviceKey into the network whose public key
// is networkPub, as of now.
func (m Membership) Verify(networkPub ed25519.PublicKey, deviceKey ed25519.PublicKey, now time.Time) error {
	if m.Network != PublicKeyString(networkPub) {
		return ErrOtherNetwork
	}
	if m.DeviceKey != PublicKeyString(deviceKey) {
		return ErrKeyMismatch
	}
	signer, err := m.potpisnik(networkPub, now)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(m.Signature)
	if err != nil || !ed25519.Verify(signer, m.signedBytes(), sig) {
		return ErrBadSignature
	}
	if !m.ExpiresAt.IsZero() && now.After(m.ExpiresAt) {
		return fmt.Errorf("%w: %s", ErrExpired, m.ExpiresAt.Format(time.RFC3339))
	}
	return nil
}

// najvecaRazlikaSatova je koliko članstvo smije biti „izdano u budućnosti”
// zbog razlike satova dvaju čvorova
const najvecaRazlikaSatova = time.Hour

// potpisnik je ključ koji je smio potpisati ovo članstvo: ključ mreže, ili
// ovlašteni primatelj kad članstvo nosi njegovu ovlast. Lanac: ključ mreže
// → ovlast primatelja → članstvo; ovlast mora vrijediti kad je članstvo
// izdano, a potpisnik mora biti baš taj primatelj.
//
// Trenutak izdavanja i rok upisuje sam primatelj, pa se ne smiju
// pretpostaviti: članstvo ne smije trajati dulje od ovlasti ni biti izdano u
// budućnosti. Inače bi ključ primatelja i nakon isteka ovlasti izdavao
// članstva s datumom unatrag i proizvoljnim rokom.
func (m Membership) potpisnik(networkPub ed25519.PublicKey, now time.Time) (ed25519.PublicKey, error) {
	o := m.Primatelj
	if o == nil {
		return networkPub, nil
	}
	if err := o.Verify(networkPub, m.IssuedAt); err != nil {
		return nil, err
	}
	if !o.ExpiresAt.IsZero() && (m.ExpiresAt.IsZero() || m.ExpiresAt.After(o.ExpiresAt)) {
		return nil, fmt.Errorf("%w: članstvo traje dulje od ovlasti primatelja", ErrOvlast)
	}
	if m.IssuedAt.After(now.Add(najvecaRazlikaSatova)) {
		return nil, fmt.Errorf("%w: članstvo je izdano u budućnosti", ErrOvlast)
	}
	if o.DeviceID != m.IssuedBy {
		return nil, fmt.Errorf("%w: izdao ga je %s, a ovlast glasi na %s", ErrBadSignature, m.IssuedBy, o.DeviceID)
	}
	kljuc, err := ParsePublicKey(o.DeviceKey)
	if err != nil {
		return nil, ErrBadSignature
	}
	return kljuc, nil
}
