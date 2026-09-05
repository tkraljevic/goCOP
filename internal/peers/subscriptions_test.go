package peers_test

import (
	"context"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/peers"
)

// Laptop koji prati samo BP 16 dobiva zajedničke zapise i BP 16, ne i
// BP 15; kad pretplatu makne i obriše, verzije nestaju s računala, a
// vraćaju se čim se opet pretplati.
func TestPretplataOgradjujeRazmjenu(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ured := startNode(t, ctx, "ured")
	laptop := startNode(t, ctx, "laptop")
	founder(t, ctx, ured, "Hrvatske vode")
	pair(t, ctx, ured, laptop)
	ured.svc.SetWantsAll(true)

	ch16 := ledger.ChannelFor(ledger.ChannelReadings, 16, 2026)
	ch15 := ledger.ChannelFor(ledger.ChannelReadings, 15, 2026)
	db := ured.rec
	if _, err := db.RecordIn(ctx, ured.db, ch16, "probni", "r-16", map[string]int{"cm": 300}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RecordIn(ctx, ured.db, ch15, "probni", "r-15", map[string]int{"cm": 200}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Record(ctx, ured.db, "probni", "zajednicki", map[string]int{"cm": 1}); err != nil {
		t.Fatal(err)
	}

	if _, err := laptop.svc.AddSubscription(ctx, peers.Subscription{Kind: ledger.ChannelReadings, AreaID: 16}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := laptop.svc.SyncWith(ctx, ured.id); err != nil {
		t.Fatalf("razmjena: %v", err)
	}
	counts, _ := laptop.rec.CountByChannel(ctx)
	if counts[ch16] != 1 || counts[ch15] != 0 {
		t.Fatalf("laptop drži %v, očekivano samo BP 16", counts)
	}
	if _, err := laptop.rec.Latest(ctx, "probni", "zajednicki"); err != nil {
		t.Error("zajednički zapis mora stići svakome")
	}

	// druga razmjena ne vraća ništa: BP 15 nije rupa u granici
	applied, _, err := laptop.svc.SyncWith(ctx, ured.id)
	if err != nil || applied != 0 {
		t.Errorf("ponovljena razmjena: primljeno %d, %v", applied, err)
	}

	// pretplata se miče, kanal briše, pa opet vraća
	rules, _ := laptop.svc.ListSubscriptions(ctx)
	if err := laptop.svc.RemoveSubscription(ctx, rules[0].ID); err != nil {
		t.Fatal(err)
	}
	removed, err := laptop.svc.PurgeUnwanted(ctx)
	if err != nil || removed[ch16] != 1 {
		t.Fatalf("brisanje s računala: %v %v", removed, err)
	}
	if _, err := laptop.svc.AddSubscription(ctx, peers.Subscription{SectorID: "B"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := laptop.svc.SyncWith(ctx, ured.id); err != nil {
		t.Fatal(err)
	}
	counts, _ = laptop.rec.CountByChannel(ctx)
	if counts[ch16] != 1 || counts[ch15] != 1 {
		t.Errorf("pretplata na sektor B mora vratiti oba područja: %v", counts)
	}

	// kanal pod pretplatom se ne briše
	if _, err := laptop.svc.PurgeChannel(ctx, ch16); err == nil {
		t.Error("kanal pod pretplatom ne smije se obrisati")
	}
}
