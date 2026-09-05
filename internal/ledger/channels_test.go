package ledger

import (
	"context"
	"testing"
)

// Granica se vodi po autoru i kanalu: što čvor ne prati, nema u granici,
// a delta mu to i ne šalje kad kaže da ne želi.
func TestGranicaPoKanaluIDeltaPoPretplati(t *testing.T) {
	db := openTestDB(t)
	rec := New(db, "ured")
	ctx := context.Background()

	if _, err := rec.Record(ctx, db, "sections", "B.16.1", probni{"dionica", 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.RecordIn(ctx, db, ChannelFor(ChannelReadings, 16, 2026), "readings", "r-16", probni{"BP16", 300}); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.RecordIn(ctx, db, ChannelFor(ChannelReadings, 15, 2026), "readings", "r-15", probni{"BP15", 200}); err != nil {
		t.Fatal(err)
	}

	front, err := rec.Frontier(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"ured", "ured|ocitanja/16/2026", "ured|ocitanja/15/2026"} {
		if front[key] == "" {
			t.Errorf("granica nema ključ %q: %v", key, front)
		}
	}

	// prazan čvor koji prati samo BP 16 dobiva zajedničko i BP 16, ne i BP 15
	only16 := func(ch string) bool { return ch == "" || ch == "ocitanja/16/2026" }
	delta, err := rec.Delta(ctx, map[string]string{}, only16, 100)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, v := range delta {
		got[v.EntityID] = true
	}
	if !got["B.16.1"] || !got["r-16"] || got["r-15"] {
		t.Errorf("delta po pretplati: %v", got)
	}

	// bez ograde ide sve
	all, err := rec.Delta(ctx, map[string]string{}, nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Errorf("bez ograde očekivane 3 verzije, dobiveno %d", len(all))
	}

	// brisanje kanala s računala
	n, err := rec.PurgeChannel(ctx, db, "ocitanja/15/2026")
	if err != nil || n != 1 {
		t.Fatalf("brisanje kanala: %d, %v", n, err)
	}
	if _, err := rec.PurgeChannel(ctx, db, ""); err == nil {
		t.Error("zajednički kanal se ne smije brisati")
	}
	counts, _ := rec.CountByChannel(ctx)
	if counts["ocitanja/15/2026"] != 0 || counts["ocitanja/16/2026"] != 1 || counts[""] != 1 {
		t.Errorf("brojanje po kanalu: %v", counts)
	}
}

func TestKljucGraniceIKanal(t *testing.T) {
	if FrontierKey("ured", "") != "ured" || FrontierKey("ured", "dnevnici/16/2025") != "ured|dnevnici/16/2025" {
		t.Error("ključ granice")
	}
	n, ch := SplitFrontierKey("laptop|ocitanja/3/2024")
	if n != "laptop" || ch != "ocitanja/3/2024" {
		t.Errorf("rastavljanje ključa: %q %q", n, ch)
	}
	kind, area, year := SplitChannel("ocitanja/3/2024")
	if kind != ChannelReadings || area != 3 || year != 2024 {
		t.Errorf("rastavljanje kanala: %q %d %d", kind, area, year)
	}
	if ChannelFor(ChannelReadings, 0, 2024) != "" {
		t.Error("bez područja nema kanala")
	}
}
