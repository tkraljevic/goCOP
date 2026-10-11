package peers_test

import (
	"context"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/peers"
)

// Laptop koji prati samo BP 1 dobiva zajedničke zapise i BP 1, ne i
// BP 2; kad pretplatu makne i obriše, verzije nestaju s računala, a
// vraćaju se čim se opet pretplati. Oba su područja u izmišljenom sektoru P.
func TestPretplataOgradjujeRazmjenu(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ured := startNode(t, ctx, "ured")
	laptop := startNode(t, ctx, "laptop")
	founder(t, ctx, ured, "Hrvatske vode")
	pair(t, ctx, ured, laptop)
	ured.svc.SetWantsAll(true)

	ch1 := ledger.ChannelFor(ledger.ChannelReadings, 1, 2026)
	ch2 := ledger.ChannelFor(ledger.ChannelReadings, 2, 2026)
	db := ured.rec
	if _, err := db.RecordIn(ctx, ured.db, ch1, "probni", "r-1", map[string]int{"cm": 300}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RecordIn(ctx, ured.db, ch2, "probni", "r-2", map[string]int{"cm": 200}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Record(ctx, ured.db, "probni", "zajednicki", map[string]int{"cm": 1}); err != nil {
		t.Fatal(err)
	}

	if _, err := laptop.svc.AddSubscription(ctx, peers.Subscription{Kind: ledger.ChannelReadings, AreaID: 1}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := laptop.svc.SyncWith(ctx, ured.id); err != nil {
		t.Fatalf("razmjena: %v", err)
	}
	counts, _ := laptop.rec.CountByChannel(ctx)
	if counts[ch1] != 1 || counts[ch2] != 0 {
		t.Fatalf("laptop drži %v, očekivano samo BP 1", counts)
	}
	if _, err := laptop.rec.Latest(ctx, "probni", "zajednicki"); err != nil {
		t.Error("zajednički zapis mora stići svakome")
	}

	// druga razmjena ne vraća ništa: BP 2 nije rupa u granici
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
	if err != nil || removed[ch1] != 1 {
		t.Fatalf("brisanje s računala: %v %v", removed, err)
	}
	if _, err := laptop.svc.AddSubscription(ctx, peers.Subscription{SectorID: "P"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := laptop.svc.SyncWith(ctx, ured.id); err != nil {
		t.Fatal(err)
	}
	counts, _ = laptop.rec.CountByChannel(ctx)
	if counts[ch1] != 1 || counts[ch2] != 1 {
		t.Errorf("pretplata na sektor P mora vratiti oba područja: %v", counts)
	}

	// kanal pod pretplatom se ne briše
	if _, err := laptop.svc.PurgeChannel(ctx, ch1); err == nil {
		t.Error("kanal pod pretplatom ne smije se obrisati")
	}
}

// Odluke (8. 10. 2026.), pitanje 21: zapis o vrijednosti odbačenoj izvan
// raspona putuje u kanalu očitanja letve. Kad se kanal obriše s računala,
// s očitanjima nestaje i taj zapis, inače bi stranica letve brojila odbačeno
// iz kanala koji računalo više ne drži (verzije su obrisane, redak ostao).
func TestBrisanjeKanalaBriseOdbaceneVrijednosti(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	n := startNodeBezRegistra(t, ctx, "laptop-baranja")
	ch16 := ledger.ChannelFor(ledger.ChannelReadings, 16, 2026)
	ch15 := ledger.ChannelFor(ledger.ChannelReadings, 15, 2026)
	sada := time.Now().UTC()
	for _, z := range []struct{ id, kanal string }{{"odbacena-16", ch16}, {"odbacena-15", ch15}} {
		if _, err := n.db.Exec(`INSERT INTO odbacena_ocitanja (id, letva, kad, polje, izvor, opis, zabiljezeno)
			VALUES (?, ?, ?, 'vodostaj', 'hdv.voda.hr', '−1102 cm', ?)`, z.id, "station:"+z.id, sada, sada); err != nil {
			t.Fatal(err)
		}
		if _, err := n.rec.RecordIn(ctx, n.db, z.kanal, "odbacena_ocitanja", z.id, map[string]string{"id": z.id}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := n.svc.PurgeChannel(ctx, ch16); err != nil {
		t.Fatal(err)
	}
	ostalo := map[string]bool{}
	rows, err := n.db.Query(`SELECT id FROM odbacena_ocitanja`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ostalo[id] = true
	}
	rows.Close()
	if ostalo["odbacena-16"] || !ostalo["odbacena-15"] {
		t.Errorf("nakon brisanja kanala BP 16 ostali su zapisi %v, očekivan samo BP 15", ostalo)
	}

	// kad se površina ne može obrisati, knjiga kanala ostaje
	if _, err := n.db.Exec(`DROP TABLE odbacena_ocitanja`); err != nil {
		t.Fatal(err)
	}
	if _, err := n.svc.PurgeChannel(ctx, ch15); err == nil {
		t.Error("brisanje kanala mora javiti grešku površine")
	}
	if counts, _ := n.rec.CountByChannel(ctx); counts[ch15] != 1 {
		t.Errorf("knjiga kanala BP 15 nakon neuspjelog brisanja: %v", counts)
	}
}
