package main

import (
	"strings"
	"testing"
	"time"

	"gocop/internal/peers"
	"gocop/internal/razmjena"
)

// Ime stalnog čvora na mreži je domena iz adrese tunela, nikad IP adresa;
// bez stalnog čvora na mreži natpisa nema, a uz sam stalni čvor, uz
// računalo na mreži i uz opozvano računalo ne stoji
func TestPrekoPosluzitelja(t *testing.T) {
	stalni := func(dostupnost string, adrese ...string) peers.PeerStatus {
		return peers.PeerStatus{Peer: peers.Peer{Name: "COP Osijek", IsBootstrap: true, Addresses: adrese}, Reachability: dostupnost}
	}
	laptop := peers.PeerStatus{Peer: peers.Peer{Name: "laptop", Addresses: []string{"192.168.1.20:4710"}}, Reachability: "offline", Member: true}
	opozvan := laptop
	opozvan.Member, opozvan.MemberProblem = false, "članstvo je opozvano"
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
		{peers.PeerStatus{Reachability: "never", Member: true}, "cop.primjer.hr"},
		{peers.PeerStatus{Reachability: "online", Member: true}, ""},
		{stalni("offline"), ""},
		{opozvan, ""},
	} {
		if got := prekoPosluziteljaZa(c.ps, "cop.primjer.hr"); got != c.je {
			t.Errorf("%s, %s: %q", c.ps.Name, c.ps.Reachability, got)
		}
	}
	if got := cvorRazmjene(laptop, false, "cop.primjer.hr").Razlog(); got != "SERVER sync, preko cop.primjer.hr: podaci stižu do 10 min kasnije." {
		t.Errorf("pločica: %q", got)
	}
}

// Dok je par u pauzi WEB synca, pločica kaže kad WEB sync pokušava ponovno
// (PonovnoU iz peers).
func TestPlocicaPauzeWebSynca(t *testing.T) {
	ponovno := time.Date(2026, 10, 25, 13, 30, 0, 0, time.UTC)
	ps := peers.PeerStatus{Peer: peers.Peer{Name: "ThinkCentre"}, Reachability: "online", Member: true,
		State: peers.SyncState{Put: peers.PutRazmjene{Naziv: razmjena.NazivServer, Razlog: razmjena.RazlogPutaPauza, PonovnoU: ponovno}}}
	c := cvorRazmjene(ps, false, "")
	if !c.PonovnoU.Equal(ponovno) || !strings.Contains(c.Razlog(), "pokušava ponovno u 14:30") {
		t.Errorf("pločica u pauzi: %+v %q", c, c.Razlog())
	}
}
