package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"gocop/internal/postava"
)

// cvorSDopustenjem je čvor koji radi i u /zdravlje javlja dopušteno
// starije izdanje
type cvorSDopustenjem struct {
	cvorUStanju
	dopusteno string
}

func (c *cvorSDopustenjem) PitajZdravlje(context.Context) postava.OdgovorZdravlja {
	return postava.OdgovorZdravlja{Izdanje: "0.0.47-alfa", Port: 8080, Dopusteno: c.dopusteno}
}

// Stavka za vraćanje: isključena bez dopuštenja, s izdanjem dok vrijedi
func TestStavkaVracanja(t *testing.T) {
	tr, pr := novaTraka(t, nil)
	m := tr.izbornik()
	if !slices.Contains(natpisi(m), bezStarijeg) {
		t.Fatalf("nema stavke za vraćanje: %v", natpisi(m))
	}
	tr.osvjeziVracanje(postava.Stanje{Starije: "0.0.45-alfa"})
	if !slices.Contains(natpisi(m), "Vrati na 0.0.45-alfa") || tr.vrati.IsDisabled() || tr.javljenoStar != "0.0.45-alfa" {
		t.Errorf("dopušteno: %v, isključena %v, javljeno %q", natpisi(m), tr.vrati.IsDisabled(), tr.javljenoStar)
	}
	tr.osvjeziVracanje(postava.Stanje{Starije: "0.0.45-alfa", Zauzeto: "Nadogradnja"})
	if !tr.vrati.IsDisabled() {
		t.Error("uz drugi posao stavka je uključena")
	}
	if len(pr.obavijesti) != 1 || !strings.Contains(pr.obavijesti[0], "dopustio je vraćanje na izdanje 0.0.45-alfa") {
		t.Errorf("obavijest o dopuštenju (jednom): %v", pr.obavijesti)
	}
	tr.osvjeziVracanje(postava.Stanje{})
	if !slices.Contains(natpisi(m), bezStarijeg) || !tr.vrati.IsDisabled() || tr.javljenoStar != "" {
		t.Errorf("bez dopuštenja: %v, isključena %v", natpisi(m), tr.vrati.IsDisabled())
	}
}

// Klik pita prije vraćanja; bez dopuštenja ne pita ništa, a neuspjeh
// javlja porukom
func TestVratiKlik(t *testing.T) {
	tr, pr := novaTraka(t, nil)
	c := &cvorSDopustenjem{cvorUStanju: cvorUStanju{proc: postava.StanjeProcesa{Radi: true}}}
	tr.p.Cvor = c
	tr.vratiKlik()
	if len(pr.pitanja)+len(pr.poruke) != 0 {
		t.Fatalf("bez dopuštenja: pitanja %v, poruke %v", pr.pitanja, pr.poruke)
	}
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	tr.p.Ugradnja.Izdanja = postava.Izdanja{API: srv.URL, Repo: "x/goCOP", Klijent: srv.Client(), Agent: "test"}
	c.dopusteno = "0.0.45-alfa"
	tr.vratiKlik()
	if len(pr.pitanja) != 1 || !strings.Contains(pr.pitanja[0], "s 0.0.47-alfa na starije izdanje 0.0.45-alfa") || len(pr.poruke) != 0 {
		t.Fatalf("odustajanje: pitanja %v, poruke %v", pr.pitanja, pr.poruke)
	}
	pr.odgovor = true
	tr.vratiKlik()
	if len(pr.poruke) != 1 || !strings.Contains(pr.poruke[0], "Vraćanje nije uspjelo") || !strings.Contains(pr.poruke[0], "404") {
		t.Errorf("neuspjelo vraćanje: %v", pr.poruke)
	}
	if c.zaustavljen != 0 {
		t.Error("čvor zaustavljen iako izdanje nije preuzeto")
	}
}

func TestPitanjeVracanja(t *testing.T) {
	usluga := pitanjeVracanja(postava.Stanje{Izdanje: "0.0.47-alfa", Starije: "0.0.45-alfa", UslugaSustava: true})
	if !strings.HasPrefix(usluga, "Vratiti goCOP s 0.0.47-alfa na starije izdanje 0.0.45-alfa?") ||
		!strings.Contains(usluga, "lozinka administratora") || !strings.Contains(usluga, "globalni administrator") ||
		!strings.Contains(usluga, "Postava (ova ikona) ostaje ista.") {
		t.Errorf("uz uslugu: %q", usluga)
	}
	vlastiti := pitanjeVracanja(postava.Stanje{Starije: "0.0.45-alfa"})
	if !strings.HasPrefix(vlastiti, "Vratiti goCOP na starije izdanje 0.0.45-alfa?") || !strings.Contains(vlastiti, "vraća se sadašnje") {
		t.Errorf("vlastiti čvor: %q", vlastiti)
	}
}

// Klik na stavku pokreće posao u pozadini, a izbornik ne čeka
func TestUPozadini(t *testing.T) {
	gotovo := make(chan struct{})
	pusti := make(chan struct{})
	uPozadini(func() { <-pusti; close(gotovo) })() // vrati se odmah, iako posao čeka
	close(pusti)
	<-gotovo
}
