package ledger

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Brisanje pojedine verzije (zapis s oglasne ploče) i spomenika: zadnja
// verzija zapisa ostaje, i kad je spomenik; starija verzija odlazi, a
// spomenike briše samo PurgeArchived.
func TestBrisanjeVerzijeISpomenika(t *testing.T) {
	db := openTestDB(t)
	rec := New(db, "ured")
	ctx := context.Background()
	prva, _ := rec.Record(ctx, db, "stations", "st-1", probni{"Županja", 600})
	druga, _ := rec.Record(ctx, db, "stations", "st-1", probni{"Županja", 610})
	if _, err := rec.Record(ctx, db, "stations", "st-2", probni{"Dalj", 500}); err != nil {
		t.Fatal(err)
	}
	spomenik, err := rec.Archive(ctx, db, "stations", "st-2", probni{"Dalj", 500})
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.DeleteVersion(ctx, druga); !errors.Is(err, ErrVazecaVerzija) {
		t.Fatalf("zadnja verzija živog zapisa se ne briše: %v", err)
	}
	// bez spomenika bi zadnja bila živa verzija i obrisani zapis bi se vratio
	if err := rec.DeleteVersion(ctx, spomenik); !errors.Is(err, ErrZapisOBrisanju) {
		t.Fatalf("spomenik koji je zadnja verzija zapisa se ne briše: %v", err)
	}
	if l, _ := rec.Latest(ctx, "stations", "st-2"); l == nil || l.VersionID != spomenik || !l.Archived {
		t.Fatal("zadnja verzija obrisanog zapisa mora ostati spomenik")
	}
	if err := rec.DeleteVersion(ctx, prva); err != nil {
		t.Fatal(err)
	}
	if err := rec.DeleteVersion(ctx, prva); err == nil {
		t.Error("obrisana verzija više ne postoji")
	}
	if l, _ := rec.Latest(ctx, "stations", "st-1"); l == nil || l.VersionID != druga {
		t.Fatal("zadnja verzija mora ostati")
	}
	n, err := rec.PurgeArchived(ctx)
	if err != nil || n != 1 {
		t.Fatalf("spomenici: %d %v", n, err)
	}
	var ostalo int
	_ = db.QueryRow(`SELECT COUNT(*) FROM record_versions`).Scan(&ostalo)
	if ostalo != 2 {
		t.Errorf("ostaju zadnja verzija st-1 i prva verzija st-2: %d", ostalo)
	}
}

// Zadnji upis jednog čvora u kanalu ne briše se s oglasne ploče, iako zapis
// ima noviju verziju drugog čvora: bez njega granica tog čvora u kanalu
// padne i razmjena ga vrati (Odluke (13. 10. 2026.), 1; pravilo sažimanja).
// Starija verzija istog čvora briše se i ne vraća se.
func TestBrisanjeNeDiraZadnjiUpisCvoraUKanalu(t *testing.T) {
	ctx := context.Background()
	dbT, dbU := openTestDB(t), openTestDB(t)
	teren, ured := New(dbT, "teren"), New(dbU, "ured")
	kanal := ChannelFor(ChannelPrijave, 34, 2026)
	nacrt, _ := teren.RecordIn(ctx, dbT, kanal, "prijave", "p-1", probni{"nacrt", 1})
	objava, err := teren.RecordIn(ctx, dbT, kanal, "prijave", "p-1", probni{"objava", 2})
	if err != nil {
		t.Fatal(err)
	}
	razmijeni := func() int {
		t.Helper()
		granica, err := ured.Frontier(ctx)
		if err != nil {
			t.Fatal(err)
		}
		paket, err := teren.Delta(ctx, granica, nil, 100)
		if err != nil {
			t.Fatal(err)
		}
		n, err := ured.Apply(ctx, paket)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := razmijeni(); n != 2 {
		t.Fatalf("ured je primio %d verzija, očekivane 2", n)
	}
	if _, err := ured.RecordIn(ctx, dbU, kanal, "prijave", "p-1", probni{"urudžba", 3}); err != nil {
		t.Fatal(err)
	}

	if err := ured.DeleteVersion(ctx, objava); !errors.Is(err, ErrZadnjiUpisCvora) {
		t.Fatalf("zadnji upis terena u kanalu se ne briše: %v", err)
	}
	if err := ured.DeleteVersion(ctx, nacrt); err != nil {
		t.Fatalf("starija verzija terena se briše: %v", err)
	}
	if n := razmijeni(); n != 0 {
		t.Errorf("razmjena je vratila %d verzija", n)
	}
	if h, _ := ured.History(ctx, "prijave", "p-1"); len(h) != 2 || h[0].VersionID == nacrt || h[1].VersionID == nacrt {
		t.Errorf("u knjizi ureda ostaju objava i urudžba: %+v", h)
	}
}

// Kad se knjiga ne da pročitati, brisanje to kaže, a ne javlja razlog
// zašto se verzija ne briše
func TestBrisanjeVerzijeKadSeKnjigaNeDaProcitati(t *testing.T) {
	db := openTestDB(t)
	rec := New(db, "ured")
	ctx := context.Background()
	v, err := rec.Record(ctx, db, "stations", "st-1", probni{"Županja", 600})
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	err = rec.DeleteVersion(ctx, v)
	if err == nil || !strings.HasPrefix(err.Error(), "čitanje verzije za brisanje: ") {
		t.Errorf("greška čitanja: %v", err)
	}
}
