package razmjena

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"
)

// A membership signed by the network key verifies for that device on that
// network — and for nothing else.
func TestMembershipVerifiesOnlyForItsNetworkAndDevice(t *testing.T) {
	hv, _ := NewNetwork("hrvatske-vode")
	other, _ := NewNetwork("some-other-authority")
	device := newKey(t).Public().(ed25519.PublicKey)
	stranger := newKey(t).Public().(ed25519.PublicKey)

	m, err := hv.Admit("laptop-1", device, "cop-osijek", 365*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()

	if err := m.Verify(hv.Public, device, now); err != nil {
		t.Errorf("a freshly issued membership must verify: %v", err)
	}
	if err := m.Verify(other.Public, device, now); !errors.Is(err, ErrOtherNetwork) {
		t.Errorf("another network must not accept it: %v", err)
	}
	if err := m.Verify(hv.Public, stranger, now); !errors.Is(err, ErrKeyMismatch) {
		t.Errorf("another device must not ride on it: %v", err)
	}
	if err := m.Verify(hv.Public, device, now.Add(366*24*time.Hour)); !errors.Is(err, ErrExpired) {
		t.Errorf("an expired membership must be refused: %v", err)
	}

	forged := m
	forged.DeviceID = "laptop-2"
	if err := forged.Verify(hv.Public, device, now); !errors.Is(err, ErrBadSignature) {
		t.Errorf("a tampered membership must fail the signature: %v", err)
	}
}

// Only a holder of the private network key can admit; the public half alone
// can verify but never sign.
func TestOnlyTheNetworkKeyHolderCanAdmit(t *testing.T) {
	hv, _ := NewNetwork("hrvatske-vode")
	member := PublicNetwork(hv.Name, hv.Public)
	if member.CanSign() {
		t.Fatal("a public-only network key must not be able to sign")
	}
	device := newKey(t).Public().(ed25519.PublicKey)
	if _, err := member.Admit("x", device, "y", time.Hour); err == nil {
		t.Error("admitting without the private key must fail")
	}

	// the private key round-trips through the on-disk format
	path := t.TempDir() + "/network-key"
	if _, err := LoadOrCreateKey(path); err != nil {
		t.Fatal(err)
	}
	loaded := LoadNetworkKey("hrvatske-vode", hv.Private())
	m, _ := loaded.Admit("x", device, "y", time.Hour)
	if err := m.Verify(hv.Public, device, time.Now()); err != nil {
		t.Errorf("a key loaded back must sign valid memberships: %v", err)
	}
}
