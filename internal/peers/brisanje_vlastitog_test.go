package peers

// Brisanje kanala zapisa nastalih ovdje koje je ured poslije mijenjao ili
// obrisao, na jednom čvoru bez mreže: tuđe izmjene odlaze, ured je obrisano
// ostaje obrisano kao vlastiti spomenik, a površina se odmah obnovi iz
// onoga što ostaje u knjizi.

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/ledger"
)

// vrijemeVerzije je vrijeme nastanka verzije u milisekundama, iz UUIDv7
func vrijemeVerzije(t *testing.T, id string) int64 {
	t.Helper()
	u, err := uuid.Parse(id)
	if err != nil || u.Version() != 7 {
		t.Fatalf("oznaka verzije %s: %v", id, err)
	}
	var ms int64
	for _, b := range u[:6] {
		ms = ms<<8 | int64(b)
	}
	return ms
}

// tudjiSpomenik upisuje brisanje zapisa kakvo je ovaj čvor primio s ureda
func tudjiSpomenik(t *testing.T, s *Service, kanal, entitet, id string) {
	t.Helper()
	if _, err := ledger.New(s.db, "cop-osijek-ured").ArchiveIn(ctxRub, s.db, kanal, entitet, id, map[string]string{"id": id, "obrisao": "ured"}); err != nil {
		t.Fatal(err)
	}
}

// Prijavu p-nasa napisao je laptop, a ured ju je obrisao; prijavu
// p-ispravljena laptop je napisao, a ured ispravio; p-ured je samo s
// ureda. Brisanje kanala obrisanu prijavu ostavi obrisanom vlastitim
// spomenikom, a obje prijave nastale ovdje preda primjeni sa zadnjom
// vlastitom verzijom. Drugo brisanje nema više ništa za obnoviti.
func TestBrisanjeKanalaObnavljaVlastitoIzKnjige(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	kanal := "prijave/16/2026"
	vlastitaVerzija(t, s, kanal, "prijave", "p-nasa")
	tudjiSpomenik(t, s, kanal, "prijave", "p-nasa")
	vlastitaVerzija(t, s, kanal, "prijave", "p-ispravljena")
	tudjaVerzija(t, s, kanal, "prijave", "p-ispravljena")
	tudjaVerzija(t, s, kanal, "prijave", "p-ured")
	var primijenjeno []ledger.Version
	s.OnApplied(func(_ context.Context, v []ledger.Version) error {
		primijenjeno = append(primijenjeno, v...)
		return nil
	})

	uredovo, err := s.rec.Latest(ctxRub, "prijave", "p-nasa")
	if err != nil {
		t.Fatal(err)
	}

	if b := obrisiKanal(t, s, kanal); b != (Brisanje{Kanal: kanal, Verzija: 3, OstajeVerzija: 3}) {
		t.Errorf("brisanje: %+v", b)
	}
	zadnja, err := s.rec.Latest(ctxRub, "prijave", "p-nasa")
	if err != nil || !zadnja.Archived || zadnja.NodeID != "laptop-baranja" || zadnja.Channel != kanal || !strings.Contains(string(zadnja.Payload), `"obrisao":"ured"`) {
		t.Fatalf("zadnja verzija obrisane prijave: %+v %v", zadnja, err)
	}
	// vlastiti spomenik stoji odmah iza uredova, ne s vremenom brisanja
	if zadnja.VersionID <= uredovo.VersionID || vrijemeVerzije(t, zadnja.VersionID)-vrijemeVerzije(t, uredovo.VersionID) != 1 {
		t.Errorf("vlastiti spomenik %s nije milisekundu iza uredova %s", zadnja.VersionID, uredovo.VersionID)
	}
	sort.Slice(primijenjeno, func(i, j int) bool { return primijenjeno[i].EntityID < primijenjeno[j].EntityID })
	if len(primijenjeno) != 2 || primijenjeno[0].EntityID != "p-ispravljena" || primijenjeno[0].Archived || primijenjeno[0].NodeID != "laptop-baranja" ||
		primijenjeno[1].EntityID != "p-nasa" || primijenjeno[1].VersionID != zadnja.VersionID {
		t.Errorf("primijenjeno nakon brisanja: %+v", primijenjeno)
	}

	primijenjeno = nil
	if b := obrisiKanal(t, s, kanal); b != (Brisanje{Kanal: kanal, OstajeVerzija: 3}) || len(primijenjeno) != 0 {
		t.Errorf("drugo brisanje: %+v, primijenjeno %+v", b, primijenjeno)
	}
}

// Kad se vlastiti spomenik ne može upisati, ne briše se ništa. Kad se
// površina poslije brisanja ne da obnoviti, brisanje to javi s uputom.
func TestBrisanjeKanalaKadObnovaPadne(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	kanal := "prijave/16/2026"
	vlastitaVerzija(t, s, kanal, "prijave", "p-nasa")
	tudjiSpomenik(t, s, kanal, "prijave", "p-nasa")
	kvarUpisa(t, s, "INSERT", "record_versions", "NEW.node_id = 'laptop-baranja'")
	if b, err := s.PurgeChannel(ctxRub, kanal); err == nil || b != (Brisanje{Kanal: kanal}) {
		t.Errorf("brisanje bez upisa spomenika: %+v %v", b, err)
	}
	if c, _ := s.rec.CountByChannel(ctxRub); c[kanal] != 2 {
		t.Errorf("knjiga kanala nakon neuspjela brisanja: %v", c)
	}

	drugi := rubniCvor(t, "laptop-drugi")
	vlastitaVerzija(t, drugi, kanal, "prijave", "p-ispravljena")
	tudjaVerzija(t, drugi, kanal, "prijave", "p-ispravljena")
	drugi.OnApplied(func(context.Context, []ledger.Version) error { return errors.New("namjerni kvar") })
	b, err := drugi.PurgeChannel(ctxRub, kanal)
	if err == nil || !strings.HasPrefix(err.Error(), "Obrisani su zapisi koji su stigli s drugih računala, ali zapisi nastali") || b.Verzija != 1 {
		t.Errorf("obnova koja padne: %+v %v", b, err)
	}

	treci := rubniCvor(t, "laptop-treci")
	vlastitaVerzija(t, treci, kanal, "prijave", "p-ispravljena")
	tudjaVerzija(t, treci, kanal, "prijave", "p-ispravljena")
	if _, err := treci.db.Exec(`UPDATE record_versions SET created_at = 'nije vrijeme' WHERE node_id = 'laptop-treci'`); err != nil {
		t.Fatal(err)
	}
	treci.OnApplied(func(context.Context, []ledger.Version) error { return nil })
	if _, err := treci.PurgeChannel(ctxRub, kanal); err == nil || !strings.Contains(err.Error(), "Ponovno pokrenite program") {
		t.Errorf("nečitljiva zadnja verzija: %v", err)
	}
}

// citajZapise javi grešku upita i retka koji se ne da pročitati
func TestCitajZapiseKadUpitPadne(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	tx, err := s.db.BeginTx(ctxRub, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := citajZapise(ctxRub, tx, `SELECT nema FROM nigdje`, nil, false); err == nil {
		t.Error("upit nad nepostojećom tablicom")
	}
	if _, err := citajZapise(ctxRub, tx, `SELECT NULL, 'p-1', 'x', 'v-1'`, nil, true); err == nil {
		t.Error("redak bez entiteta")
	}
	if z, err := citajZapise(ctxRub, tx, `SELECT 'prijave', 'p-1', '{}', 'v-1'`, nil, true); err != nil || len(z) != 1 || z[0].sadrzaj != "{}" || z[0].verzija != "v-1" {
		t.Errorf("redak sa sadržajem: %+v %v", z, err)
	}
}
