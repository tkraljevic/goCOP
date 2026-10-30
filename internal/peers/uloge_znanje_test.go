package peers

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"gocop/internal/razmjena"
)

// Uloge se u znanju javljaju poredane, a bez ijedne izostaju.
func TestUlogeOznake(t *testing.T) {
	for _, x := range []struct {
		u    Uloge
		want []string
	}{
		{Uloge{}, nil},
		{Uloge{Izdaje: true}, []string{razmjena.UlogaIzdaje}},
		{Uloge{Preuzima: true}, []string{razmjena.UlogaPreuzima}},
		{Uloge{Izdaje: true, Preuzima: true}, []string{razmjena.UlogaIzdaje, razmjena.UlogaPreuzima}},
	} {
		if got := x.u.Oznake(); !slices.Equal(got, x.want) || (x.want == nil && got != nil) {
			t.Errorf("%+v: %q, a treba %q", x.u, got, x.want)
		}
	}
}

// Odluke (30. 10. 2026.), 13: granica nosi uloge ovog računala, samo za
// prikaz. Dok ih nema, granica je ista kao prije, a čvor 0.0.38 čita i
// granicu s ulogama.
func TestMojeZnanjeNosiUloge(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	if z := s.mojeZnanje(ctxRub); z.Uloge != nil {
		t.Fatalf("prije učitavanja uloga: %+v", z)
	}
	if err := s.PostaviUloge(ctxRub, Uloge{Preuzima: true}); err != nil {
		t.Fatal(err)
	}
	z := s.mojeZnanje(ctxRub)
	if !slices.Equal(z.Uloge, []string{razmjena.UlogaPreuzima}) {
		t.Fatalf("uloge u znanju: %+v", z)
	}
	r := &razgovorSCvorom{s: s, myWants: Wants{All: true}}
	e, err := razmjena.NewEnvelope(kindFrontier, r.mojaGranica(map[string]string{}, z))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(e.Payload), `,"uloge":["preuzima"]}`) {
		t.Errorf("granica na žici: %s", e.Payload)
	}
	var stara granica0038
	if err := json.Unmarshal(e.Payload, &stara); err != nil {
		t.Errorf("0.0.38 ne pročita granicu s ulogama: %v", err)
	}
}

// Uloge drugog čvora pamte se uz stanje razmjene, očišćene, i stoje na
// ploči. Program koji ih ne javlja briše zapamćene, a brisanje zapisa
// računala briše i njih.
func TestZapamceneUlogeDrugogCvora(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	uloge := func() []string {
		t.Helper()
		states, err := s.syncStates(ctxRub)
		if err != nil {
			t.Fatal(err)
		}
		return states["laptop-baranja"].Uloge
	}
	javi := func(z razmjena.Znanje) {
		s.zapamtiZnanje(ctxRub, "laptop-baranja", syncOutcome{program: "0.0.47-alfa", znanje: z})
	}
	javi(razmjena.Znanje{Uloge: []string{razmjena.UlogaPreuzima, "nepoznata", razmjena.UlogaIzdaje}})
	if u := uloge(); !slices.Equal(u, []string{razmjena.UlogaIzdaje, razmjena.UlogaPreuzima}) {
		t.Fatalf("zapamćeno: %q", u)
	}
	ploca, err := json.Marshal(PeerStatus{State: SyncState{Uloge: uloge()}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ploca), `"uloge":["izdaje","preuzima"]`) {
		t.Errorf("ploča: %s", ploca)
	}

	javi(razmjena.Znanje{})
	if u := uloge(); u != nil {
		t.Errorf("program koji ih ne javlja: %q", u)
	}

	javi(razmjena.Znanje{Uloge: []string{razmjena.UlogaIzdaje}})
	tx, err := s.db.BeginTx(ctxRub, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := obrisiZapisCvora(ctxRub, tx, "laptop-baranja"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if u := uloge(); u != nil {
		t.Errorf("nakon brisanja zapisa računala: %q", u)
	}
}
