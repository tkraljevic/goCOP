package main

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/peers"
	"gocop/internal/repository"
)

// blagdanNovijeSheme je verzija blagdana kakvu piše noviji program (shema 2)
func blagdanNovijeSheme(versionID, zapis string) ledger.Version {
	return ledger.Version{VersionID: versionID, Entity: repository.EntityBlagdani, EntityID: zapis, NodeID: "cop-osijek",
		Payload: []byte(`{"id":"` + zapis + `","naziv":"Dan obrane","vrsta":"STALNI","mjesec":8,"dan":5}`), CreatedAt: time.Now().UTC(), SchemaVersion: 2}
}

// primiNovije upisuje u knjigu verzije koje je poslao noviji program
func primiNovije(t *testing.T, rec *ledger.Recorder, vv ...ledger.Version) {
	t.Helper()
	if _, err := rec.Apply(context.Background(), vv); err != nil {
		t.Fatal(err)
	}
}

// Pločica kaže koliko je zapisa stiglo, a ovaj program ih još ne razumije:
// broje se zapisi, ne verzije, i bez imena tablice
func TestUpozorenjeNadogradnjeOvogRacunala(t *testing.T) {
	ctx := context.Background()
	_, rec, _, _ := cvorPlocice(t)
	if u := upozorenjaNadogradnje(ctx, rec, nil); u != nil {
		t.Errorf("bez ičega na čekanju: %q", u)
	}
	primiNovije(t, rec, blagdanNovijeSheme("0190a000-0000-7000-8000-000000000301", "dan-obrane"))
	if u := upozorenjaNadogradnje(ctx, rec, nil); !slices.Equal(u, []string{"Stigao je 1 zapis koji ovaj program još ne razumije. Primijenit će ga nakon nadogradnje goCOP-a."}) {
		t.Errorf("jedan zapis: %q", u)
	}
	// druga verzija istog zapisa ne broji se, dva nova zapisa da
	primiNovije(t, rec, blagdanNovijeSheme("0190a000-0000-7000-8000-000000000302", "dan-obrane"),
		blagdanNovijeSheme("0190a000-0000-7000-8000-000000000303", "dan-sjecanja"),
		blagdanNovijeSheme("0190a000-0000-7000-8000-000000000304", "dan-zahvalnosti"))
	u := upozorenjaNadogradnje(ctx, rec, nil)
	if !slices.Equal(u, []string{"Stigla su 3 zapisa koje ovaj program još ne razumije. Primijenit će ih nakon nadogradnje goCOP-a."}) {
		t.Errorf("tri zapisa u četiri verzije: %q", u)
	}
	if strings.Contains(strings.Join(u, "\n"), repository.EntityBlagdani) {
		t.Errorf("upozorenje nosi ime tablice: %q", u)
	}
}

// Riječi uz broj idu po hrvatskoj sklonidbi: 1 i 21 zapis, 2 do 4 zapisa,
// ostalo (i 11 do 14) zapisa
func TestTekstStigloUzBroj(t *testing.T) {
	for n, ocekivano := range map[int]string{
		21:  "Stigao je 21 zapis koji ovaj program još ne razumije. Primijenit će ga nakon nadogradnje goCOP-a.",
		4:   "Stigla su 4 zapisa koje ovaj program još ne razumije. Primijenit će ih nakon nadogradnje goCOP-a.",
		11:  "Stiglo je 11 zapisa koje ovaj program još ne razumije. Primijenit će ih nakon nadogradnje goCOP-a.",
		120: "Stiglo je 120 zapisa koje ovaj program još ne razumije. Primijenit će ih nakon nadogradnje goCOP-a.",
	} {
		if s := tekstStiglo(n); s != ocekivano {
			t.Errorf("%d: %q", n, s)
		}
	}
}

// Uz drugo računalo piše koliko kod njega čeka, po nazivu ili imenu. Računalo
// koje broj ne javlja (0.0.38), ono s nulom i redak iz vremena prije nego što
// se to pamtilo nemaju retka: nula se nikad ne izmišlja.
func TestUpozorenjeNadogradnjeDrugihRacunala(t *testing.T) {
	_, rec, _, _ := cvorPlocice(t)
	znaju := []string{"razmjena/1"}
	cvorovi := []peers.PeerStatus{
		{Peer: peers.Peer{NodeID: "windows-ured", Name: "Windows"}, State: peers.SyncState{CekaNadogradnju: 120, Mogucnosti: znaju, MogucnostiPoznate: true}},
		{Peer: peers.Peer{NodeID: "laptop-0038"}, State: peers.SyncState{Program: "0.0.38-alfa", MogucnostiPoznate: true}},
		{Peer: peers.Peer{NodeID: "laptop-vinkovci"}, State: peers.SyncState{Mogucnosti: znaju, MogucnostiPoznate: true}},
		{Peer: peers.Peer{NodeID: "stari-redak"}},
		{Peer: peers.Peer{NodeID: "cop-ured", Name: "COP Ured"}, State: peers.SyncState{CekaNadogradnju: 1, Mogucnosti: znaju, MogucnostiPoznate: true}},
		{Peer: peers.Peer{NodeID: "laptop-ilok"}, State: peers.SyncState{CekaNadogradnju: 3, Mogucnosti: znaju, MogucnostiPoznate: true}},
	}
	ocekivano := []string{
		"Windows čuva 120 zapisa koje će primijeniti nakon nadogradnje.",
		"COP Ured čuva 1 zapis koji će primijeniti nakon nadogradnje.",
		"laptop-ilok čuva 3 zapisa koje će primijeniti nakon nadogradnje.",
	}
	if u := upozorenjaNadogradnje(context.Background(), rec, cvorovi); !slices.Equal(u, ocekivano) {
		t.Errorf("druga računala: %q", u)
	}
}

// Kad se ne da izbrojiti što čeka, pločica to kaže, a druga računala ostaju
func TestUpozorenjeNadogradnjeKadBrojanjeNeUspije(t *testing.T) {
	baza, rec, _, _ := cvorPlocice(t)
	primiNovije(t, rec, blagdanNovijeSheme("0190a000-0000-7000-8000-000000000301", "dan-obrane"))
	if _, err := baza.Exec(`DROP TABLE record_versions`); err != nil {
		t.Fatal(err)
	}
	u := upozorenjaNadogradnje(context.Background(), rec, []peers.PeerStatus{{Peer: peers.Peer{NodeID: "cop-ured", Name: "COP Ured"}, State: peers.SyncState{CekaNadogradnju: 2}}})
	if len(u) != 2 || !strings.HasPrefix(u[0], "Ne da se izbrojiti koliko je stiglo zapisa koje ovaj program još ne razumije: ") ||
		u[1] != "COP Ured čuva 2 zapisa koje će primijeniti nakon nadogradnje." {
		t.Errorf("uz grešku brojanja: %q", u)
	}
}

// Nakon nadogradnje program zapis razumije, pa broja više nema. Nadogradnja
// je novi proces: shema se postavi pri pokretanju, a knjiga se pregleda
// iznova (UcitajNovosti).
func TestUpozorenjeNadogradnjeNestaneNakonNadogradnje(t *testing.T) {
	ctx := context.Background()
	baza, rec, _, _ := cvorPlocice(t)
	primiNovije(t, rec, blagdanNovijeSheme("0190a000-0000-7000-8000-000000000301", "dan-obrane"))
	if u := upozorenjaNadogradnje(ctx, rec, nil); len(u) != 1 {
		t.Fatalf("prije nadogradnje: %q", u)
	}
	t.Cleanup(func() { ledger.PostaviShemu(repository.EntityBlagdani, ledger.SchemaVersion) })
	ledger.PostaviShemu(repository.EntityBlagdani, 2)
	nadograden := ledger.New(baza, "pperic-thinkpad")
	if err := nadograden.UcitajNovosti(ctx); err != nil {
		t.Fatal(err)
	}
	if u := upozorenjaNadogradnje(ctx, nadograden, nil); u != nil {
		t.Errorf("nakon nadogradnje: %q", u)
	}
}
