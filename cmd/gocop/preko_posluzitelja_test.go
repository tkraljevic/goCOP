package main

import (
	"testing"

	"gocop/internal/peers"
)

// Ime stalnog čvora na mreži je domena iz adrese tunela, nikad IP adresa;
// bez stalnog čvora na mreži natpisa nema, a uz sam stalni čvor i uz
// računalo na mreži ne stoji
func TestPrekoPosluzitelja(t *testing.T) {
	stalni := func(dostupnost string, adrese ...string) peers.PeerStatus {
		return peers.PeerStatus{Peer: peers.Peer{Name: "COP Osijek", IsBootstrap: true, Addresses: adrese}, Reachability: dostupnost}
	}
	laptop := peers.PeerStatus{Peer: peers.Peer{Name: "laptop", Addresses: []string{"192.168.1.20:4710"}}, Reachability: "offline"}
	for _, c := range []struct {
		ime     string
		cvorovi []peers.PeerStatus
		je      string
	}{
		{"domena", []peers.PeerStatus{laptop, stalni("online", "192.168.1.5:4710", " HTTPS://Cop.Primjer.hr ")}, "cop.primjer.hr"},
		{"wss", []peers.PeerStatus{stalni("online", "wss://cop.primjer.hr:8443/razmjena")}, "cop.primjer.hr"},
		{"tunel na IP adresi", []peers.PeerStatus{stalni("online", "https://203.0.113.7", "https://[2001:db8::1]")}, bezImenaPosluzitelja},
		{"bez tunela", []peers.PeerStatus{stalni("online", "192.168.1.5:4710")}, bezImenaPosluzitelja},
		{"stalni ne odgovara", []peers.PeerStatus{laptop, stalni("offline", "https://cop.primjer.hr")}, ""},
		{"bez stalnog", []peers.PeerStatus{laptop}, ""},
	} {
		if got := prekoPosluzitelja(c.cvorovi); got != c.je {
			t.Errorf("%s: %q", c.ime, got)
		}
	}
	if domenaTunela("https://%zz") != "" {
		t.Error("neispravna adresa")
	}
	for _, c := range []struct {
		ps peers.PeerStatus
		je string
	}{
		{laptop, "cop.primjer.hr"},
		{peers.PeerStatus{Reachability: "never"}, "cop.primjer.hr"},
		{peers.PeerStatus{Reachability: "online"}, ""},
		{stalni("offline"), ""},
	} {
		if got := prekoPosluziteljaZa(c.ps, "cop.primjer.hr"); got != c.je {
			t.Errorf("%s, %s: %q", c.ps.Name, c.ps.Reachability, got)
		}
	}
	if got := cvorRazmjene(laptop, false, "cop.primjer.hr").Razlog(); got != "SERVER sync, preko cop.primjer.hr: podaci stižu do 10 min kasnije." {
		t.Errorf("pločica: %q", got)
	}
}
