package peers_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/peers"
	"gocop/internal/razmjena"
)

func podaciVerzije(v ledger.Version) razmjena.PodaciVerzije {
	return razmjena.PodaciVerzije{
		VersionID: v.VersionID, Entity: v.Entity, EntityID: v.EntityID, NodeID: v.NodeID,
		Supersedes: v.Supersedes, Archived: v.Archived, Channel: v.Channel,
		SchemaVersion: v.SchemaVersion, CreatedAt: v.CreatedAt, Payload: v.Payload,
	}
}

// verzijaKanala je jedina verzija kanala na čvoru
func verzijaKanala(t *testing.T, ctx context.Context, n *node, kanal string) ledger.Version {
	t.Helper()
	vs, err := n.rec.InChannels(ctx, []string{kanal})
	if err != nil || len(vs) != 1 {
		t.Fatalf("%s: verzije kanala %d, %v", n.id, len(vs), err)
	}
	return vs[0]
}

// Potpis autora preživljava svaki put kojim verzija ide: spremanje u bazu,
// žicu, razmjenu, .db arhivu i .cop paket. Potpisuju se vrijednosti kakve
// su prije spremanja (vrijeme s nanosekundama, sadržaj s razmacima, s <, >
// i & i s U+2028), a provjeravaju se vrijednosti pročitane na kraju puta.
func TestPotpisVerzijeKruzniPut(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	const protokol = "gocop-proba"
	autor := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x01}, ed25519.SeedSize))

	a := startNodeBezRegistra(t, ctx, "cop-osijek")
	b := startNodeBezRegistra(t, ctx, "laptop-baranja")
	founder(t, ctx, a, "Hrvatske vode")
	pair(t, ctx, a, b)
	b.svc.SetWantsAll(true)

	kanal := "prijave/16/2026"
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	izvorna := ledger.Version{
		VersionID: id.String(), Entity: "probni", EntityID: "p1", NodeID: a.id, Channel: kanal,
		SchemaVersion: ledger.SchemaVersion, CreatedAt: time.Now().UTC(),
		Payload: json.RawMessage("{ \"napomena\": \"a<b>&c d\",\n  \"cm\": 312 }"),
	}
	if izvorna.CreatedAt.Nanosecond() == 0 {
		izvorna.CreatedAt = izvorna.CreatedAt.Add(123 * time.Nanosecond)
	}
	potpis, kljuc, err := razmjena.PotpisiVerziju(autor, protokol, podaciVerzije(izvorna))
	if err != nil {
		t.Fatal(err)
	}
	vrijedi := func(put string, v ledger.Version) {
		t.Helper()
		if err := razmjena.ProvjeriPotpisVerzije(protokol, podaciVerzije(v), potpis, kljuc); err != nil {
			t.Errorf("%s: %v\nverzija %+v\nsadržaj %s", put, err, v, v.Payload)
		}
	}

	if n, err := a.rec.Apply(ctx, []ledger.Version{izvorna}); err != nil || n != 1 {
		t.Fatalf("upis: %d, %v", n, err)
	}
	uBazi := verzijaKanala(t, ctx, a, kanal)
	vrijedi("baza", uBazi)

	zica, err := json.Marshal(uBazi)
	if err != nil {
		t.Fatal(err)
	}
	var sazice ledger.Version
	if err := json.Unmarshal(zica, &sazice); err != nil {
		t.Fatal(err)
	}
	vrijedi("žica", sazice)

	if _, _, err := b.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatalf("razmjena: %v", err)
	}
	vrijedi("razmjena", verzijaKanala(t, ctx, b, kanal))

	arhiva := filepath.Join(t.TempDir(), "arhiva.db")
	if n, err := a.svc.ExportFile(ctx, arhiva, []string{kanal}); err != nil || n != 1 {
		t.Fatalf("izvoz: %d, %v", n, err)
	}
	c := startNodeBezRegistra(t, ctx, "treci")
	if _, err := c.svc.ImportFile(ctx, arhiva); err != nil {
		t.Fatalf("uvoz arhive: %v", err)
	}
	vrijedi(".db", verzijaKanala(t, ctx, c, kanal))

	var paket bytes.Buffer
	if _, err := a.svc.IzveziCop(ctx, peers.CopObuhvat{Vrsta: "prijave", AreaID: 16, Od: 2026, Do: 2026}, []string{kanal}, false, &paket); err != nil {
		t.Fatalf("izvoz paketa: %v", err)
	}
	put := filepath.Join(t.TempDir(), "p.cop")
	if err := os.WriteFile(put, paket.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	d := startNodeBezRegistra(t, ctx, "cetvrti")
	pair(t, ctx, a, d) // paket se ugrađuje samo od računala iz iste mreže
	d.svc.SetWantsAll(true)
	if _, err := d.svc.UveziCop(ctx, put); err != nil {
		t.Fatalf("uvoz paketa: %v", err)
	}
	vrijedi(".cop", verzijaKanala(t, ctx, d, kanal))
}
