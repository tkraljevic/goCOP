package web

// Slusa se zatvori tek kad web stvarno sluša: zauzet port ga ne zatvara,
// a prvo uspjelo slušanje (i nakon prijelaza na drugi port) zatvara.

import (
	"net"
	"testing"
	"time"
)

func TestSlusaTekKadWebSlusa(t *testing.T) {
	zauzet, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer zauzet.Close()
	s, err := NewServer(zauzet.Addr().String(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, SupportContact{}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err == nil {
		t.Fatal("zauzet port bez greške")
	}
	select {
	case <-s.Slusa():
		t.Fatal("javlja slušanje na zauzetom portu")
	default:
	}
	s.SetAddr("127.0.0.1:0")
	go func() { _ = s.Start() }() // poslužitelj radi do kraja testova
	select {
	case <-s.Slusa():
	case <-time.After(5 * time.Second):
		t.Fatal("nije javio slušanje")
	}
}
