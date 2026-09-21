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

// Membership is a device's admission to a network, signed by the network key.
type Membership struct {
	Network   string    `json:"network"`   // base64 public key of the network
	DeviceID  string    `json:"deviceId"`  // the member
	DeviceKey string    `json:"deviceKey"` // base64 public key of the member
	IssuedBy  string    `json:"issuedBy"`  // device id of the admitter
	IssuedAt  time.Time `json:"issuedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Signature string    `json:"signature"` // base64, over signedBytes()
}

// signedBytes is the canonical form the signature covers. Field order and
// encoding are fixed here; changing them invalidates every certificate.
func (m Membership) signedBytes() []byte {
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
	sig, err := base64.StdEncoding.DecodeString(m.Signature)
	if err != nil || !ed25519.Verify(networkPub, m.signedBytes(), sig) {
		return ErrBadSignature
	}
	if !m.ExpiresAt.IsZero() && now.After(m.ExpiresAt) {
		return fmt.Errorf("%w: %s", ErrExpired, m.ExpiresAt.Format(time.RFC3339))
	}
	return nil
}
