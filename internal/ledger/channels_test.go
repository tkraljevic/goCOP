package ledger

import (
	"context"
	"fmt"
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

	// brisanje kanala s računala koje ga je primilo od ureda
	laptop := New(db, "laptop")
	n, vlastitih, err := laptop.PurgeChannel(ctx, db, "ocitanja/15/2026", true)
	if err != nil || n != 1 || vlastitih != 0 {
		t.Fatalf("brisanje kanala: %d, %d, %v", n, vlastitih, err)
	}
	if _, _, err := laptop.PurgeChannel(ctx, db, "", true); err == nil {
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

// Brisanje kanala s računala odnosi samo verzije koje su napisali drugi
// čvorovi. Vlastite ostaju, s najvećom vlastitom verzijom kanala (pravilo
// 7), a drugom autoru kanal nestaje cijeli, pa ga granica više ne spominje.
// Prebrojavanje istim uvjetom ne briše ništa i daje iste brojeve.
func TestBrisanjeKanalaOstavljaVlastiteVerzije(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	laptop, ured := New(db, "laptop"), New(db, "ured")
	kanal := ChannelFor(ChannelVodocuvar, 16, 2026)
	for i, r := range []*Recorder{ured, laptop, ured, laptop} {
		if _, err := r.RecordIn(ctx, db, kanal, "vodocuvarski_listovi", fmt.Sprintf("l-%d", i), probni{"list", i}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ured.RecordIn(ctx, db, ChannelFor(ChannelVodocuvar, 17, 2026), "vodocuvarski_listovi", "l-17", probni{"list", 17}); err != nil {
		t.Fatal(err)
	}
	prije, err := laptop.Frontier(ctx)
	if err != nil {
		t.Fatal(err)
	}

	tudjih, vlastitih, err := laptop.PurgeChannel(ctx, db, kanal, false)
	if err != nil || tudjih != 2 || vlastitih != 2 {
		t.Fatalf("prebrojavanje: %d tuđih, %d vlastitih, %v", tudjih, vlastitih, err)
	}
	if c, _ := laptop.CountByChannel(ctx); c[kanal] != 4 {
		t.Fatalf("prebrojavanje je brisalo: %v", c)
	}
	obrisano, ostalo, err := laptop.PurgeChannel(ctx, db, kanal, true)
	if err != nil || obrisano != tudjih || ostalo != vlastitih {
		t.Fatalf("brisanje: %d obrisano, %d ostalo, %v", obrisano, ostalo, err)
	}

	poslije, err := laptop.Frontier(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if poslije[FrontierKey("laptop", kanal)] != prije[FrontierKey("laptop", kanal)] {
		t.Errorf("najveća vlastita verzija kanala nije ostala: %v → %v", prije, poslije)
	}
	if _, ima := poslije[FrontierKey("ured", kanal)]; ima {
		t.Errorf("kanal ureda ostao je u granici: %v", poslije)
	}
	if poslije[FrontierKey("ured", ChannelFor(ChannelVodocuvar, 17, 2026))] == "" {
		t.Errorf("obrisan je i drugi kanal: %v", poslije)
	}
	if ponovno, _, err := laptop.PurgeChannel(ctx, db, kanal, true); err != nil || ponovno != 0 {
		t.Errorf("drugo brisanje: %d, %v", ponovno, err)
	}
}

// Kad se knjiga ne može pročitati ni obrisati, brisanje kanala javi grešku
// i ne kaže da je išta obrisano.
func TestBrisanjeKanalaBezKnjige(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if _, err := db.Exec(`DROP TABLE record_versions`); err != nil {
		t.Fatal(err)
	}
	for _, obrisi := range []bool{false, true} {
		if n, v, err := New(db, "laptop").PurgeChannel(ctx, db, "prijave/16/2026", obrisi); err == nil || n != 0 || v != 0 {
			t.Errorf("obrisi=%v: %d %d %v", obrisi, n, v, err)
		}
	}
}
