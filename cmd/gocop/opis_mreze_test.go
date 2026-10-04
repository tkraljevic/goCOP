package main

import (
	"strings"
	"testing"

	"gocop/internal/peers"
)

func TestOpisMreze(t *testing.T) {
	for _, s := range []struct {
		net *peers.Network
		ima string
	}{
		{nil, "nije ni u jednoj mreži"},
		{&peers.Network{Name: "HV", CanAdmit: true, DrziKljuc: true}, "drži ključ mreže"},
		{&peers.Network{Name: "HV", CanAdmit: true}, "ovlašteni primatelj"},
		{&peers.Network{Name: "HV"}, "— član"},
	} {
		if got := opisMreze(s.net); !strings.Contains(got, s.ima) {
			t.Errorf("%+v: %q", s.net, got)
		}
	}
}
