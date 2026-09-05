package ledger

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// Sažimanje ostavlja zadnju verziju svakog zapisa i spomenike obrisanih;
// starije verzije istog zapisa nestaju, granica razmjene ostaje ista.
func TestSazimanjeOstavljaZadnjuVerzijuISpomenike(t *testing.T) {
	db := openTestDB(t)
	rec := New(db, "ured")
	ctx := context.Background()

	for _, prag := range []int{600, 610, 620} {
		if _, err := rec.Record(ctx, db, "stations", "st-1", probni{"Županja", prag}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := rec.Record(ctx, db, "stations", "st-2", probni{"Dalj", 500}); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Archive(ctx, db, "stations", "st-2", probni{"Dalj", 500}); err != nil {
		t.Fatal(err)
	}
	// sve je "staro": pomakni nastanak u prošlost
	if _, err := db.Exec(`UPDATE record_versions SET created_at = ?`, time.Now().AddDate(0, 0, -200)); err != nil {
		t.Fatal(err)
	}

	before, _ := rec.Frontier(ctx)
	if n, err := rec.Compactable(ctx, time.Now().AddDate(0, 0, -90)); err != nil || n != 3 {
		t.Fatalf("za brisanje bi trebale biti 3 verzije, javljeno %d (%v)", n, err)
	}
	n, err := rec.Compact(ctx, time.Now().AddDate(0, 0, -90))
	if err != nil || n != 3 {
		t.Fatalf("sažimanje: obrisano %d, %v", n, err)
	}

	latest, _ := rec.Latest(ctx, "stations", "st-1")
	if latest == nil || latest.Payload == nil {
		t.Fatal("zadnja verzija st-1 mora ostati")
	}
	var p probni
	if err := json.Unmarshal(latest.Payload, &p); err != nil || p.Prag != 620 {
		t.Errorf("zadnja verzija st-1 = %+v", p)
	}
	tomb, _ := rec.Latest(ctx, "stations", "st-2")
	if tomb == nil || !tomb.Archived {
		t.Error("spomenik obrisanog zapisa mora ostati")
	}
	hist, _ := rec.History(ctx, "stations", "st-1")
	if len(hist) != 1 {
		t.Errorf("povijest st-1 nakon sažimanja: %d verzija, očekivana 1", len(hist))
	}
	after, _ := rec.Frontier(ctx)
	if before["ured"] != after["ured"] {
		t.Errorf("granica se sažimanjem ne smije pomaknuti: %s → %s", before["ured"], after["ured"])
	}
	st, _ := rec.Stats(ctx)
	if st.Versions != 2 || st.Records != 2 || st.Tombstones != 1 {
		t.Errorf("brojke nakon sažimanja: %+v", st)
	}
}
