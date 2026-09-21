// Paket razmjena je mrežna polovica izravne razmjene među čvorovima:
// identitet uređaja, ključ mreže, uparivanje, pronalaženje i prijenos.
// that belong to one owner or one organisation: a device key, a pairing
// handshake that creates trust between two devices with a human watching,
// mutually authenticated TLS between paired devices afterwards, and a
// local-network beacon so nobody has to type IP addresses.
//
// The trust model is deliberately small: every device holds one Ed25519
// key, a peer IS a public key the owner confirmed by comparing a short
// code on both screens, and every later connection proves possession of
// that key inside TLS. No certificate authorities, no accounts, no third
// party — a design with nowhere else to put trust.
//
// The package carries bytes, not meaning. What travels inside an exchange
// is the application's business: it goes in and out as JSON payloads the
// package never inspects. Two applications built on this package do not
// talk to each other, because every protocol names itself and the name is
// baked into the pairing code and the discovery probe.
//
// The shape of this package was earned in the field: the comments record
// what went wrong before, because each rule here replaced an incident.
package razmjena

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// LoadOrCreateKey returns the device's Ed25519 private key, minting one on
// first use. The file is created 0600, like any other credential.
//
// Keep the key beside the database, never in it: a database is exported,
// copied and restored, and a private key that travels with a snapshot lets
// a restored backup impersonate the machine it came from. It happened: a
// copy of the live database run with the inherited key WAS the live device
// to every paired machine, and its automatic sync pushed test entries into
// every live database on the network before anyone looked.)
func LoadOrCreateKey(path string) (ed25519.PrivateKey, error) {
	if b, err := os.ReadFile(path); err == nil {
		block, _ := pem.Decode(b)
		if block == nil || block.Type != "PRIVATE KEY" {
			return nil, fmt.Errorf("%s exists but is not a PEM private key — refusing to overwrite it", path)
		}
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		ek, ok := key.(ed25519.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("%s holds a %T, not an Ed25519 key", path, key)
		}
		return ek, nil
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		return nil, err
	}
	return priv, nil
}

// SaveKey writes a private key in the same PEM form LoadOrCreateKey reads,
// refusing to overwrite an existing file — a key file is never replaced by
// accident.
func SaveKey(path string, priv ed25519.PrivateKey) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists — refusing to overwrite a key file", path)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600)
}

// LoadKey reads a private key written by SaveKey or LoadOrCreateKey, and
// reports os.ErrNotExist when there is none — it never mints one.
func LoadKey(path string) (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(b)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf("%s is not a PEM private key", path)
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	ek, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%s holds a %T, not an Ed25519 key", path, key)
	}
	return ek, nil
}

// PublicKeyString is the wire and storage form of an identity: base64 of
// the raw Ed25519 public key.
func PublicKeyString(pub ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(pub)
}

// ParsePublicKey undoes PublicKeyString, refusing anything that is not
// exactly an Ed25519 public key.
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	if len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key has %d bytes, want %d", len(b), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(b), nil
}

// selfSignedCert wraps the device key in the minimal X.509 shape TLS
// demands. The certificate carries no trust of its own — trust is the
// PUBLIC KEY, pinned at pairing and checked on every connection — so its
// lifetime and subject are ceremony.
func selfSignedCert(priv ed25519.PrivateKey, protocol string) (tls.Certificate, error) {
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: protocol + "-sync"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, priv.Public(), priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}, nil
}

// peerPublicKey digs the Ed25519 public key out of the peer's certificate
// chain — the only thing about the certificate anyone here reads.
func peerPublicKey(rawCerts [][]byte) (ed25519.PublicKey, error) {
	if len(rawCerts) == 0 {
		return nil, fmt.Errorf("the peer presented no certificate")
	}
	cert, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return nil, err
	}
	pub, ok := cert.PublicKey.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("the peer's certificate carries a %T, not an Ed25519 key", cert.PublicKey)
	}
	return pub, nil
}

// SASCode derives the six digits both people compare during pairing —
// the same on both screens if and only if both ends of the TLS connection
// hold the keys they claim. Order-independent, so neither side has to
// know which of the two it is. The protocol name salts the code, so a
// device of one application never shows a matching code to a device of
// another.
func SASCode(protocol string, a, b ed25519.PublicKey) string {
	x, y := a, b
	if string(x) > string(y) {
		x, y = y, x
	}
	sum := sha256.Sum256(append(append([]byte(protocol+"-pair-v1|"), x...), y...))
	n := binary.BigEndian.Uint32(sum[:4]) % 1_000_000
	return fmt.Sprintf("%06d", n)
}

// tlsConfig is the shared shape: our self-signed cert, no CA validation —
// verification IS the pairing code (during pairing) or the pinned key
// (afterwards, in the exchange that builds on this).
func tlsConfig(priv ed25519.PrivateKey, protocol string) (*tls.Config, error) {
	cert, err := selfSignedCert(priv, protocol)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true, // the pin/SAS is the verification, not a CA
		ClientAuth:         tls.RequireAnyClientCert,
		MinVersion:         tls.VersionTLS13,
	}, nil
}

func rawPeerCerts(conn *tls.Conn) [][]byte {
	var raw [][]byte
	for _, c := range conn.ConnectionState().PeerCertificates {
		raw = append(raw, c.Raw)
	}
	return raw
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unnamed device"
	}
	return h
}
