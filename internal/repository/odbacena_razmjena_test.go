package repository

import (
	"context"
	"testing"
	"time"

	"gocop/internal/models"

	"github.com/google/uuid"
)

// Dorada (Odluke (8. 10. 2026.), pitanje 21): odbačene vrijednosti broje se
// i pokazuju na stranici letve. Preuzima samo jedan čvor (Unraid), a laptop
// očitanja dobiva razmjenom, pa zapis o odbačenoj vrijednosti mora putovati
// kao i očitanje — inače laptop ne vidi da je mjerilo u kvaru, nego samo
// rupu u nizu.

// letvaUPodrucju upisuje letvu na dionicu područja 16, da njezina očitanja
// (i odbačene vrijednosti) idu u kanal tog područja
func letvaUPodrucju(t *testing.T, c *cvorZaRazmjenu, sifra string) *models.Station {
	t.Helper()
	st := novaLetva(t, c, sifra, false)
	for _, q := range []string{
		`INSERT OR IGNORE INTO sections (code, area_id, sector_id, description, created_at, updated_at)
			VALUES ('B.16.1', 16, 'B', 'Probna dionica', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES ('` + uuid.NewString() + `', 'B.16.1', '` + st.ID.String() + `', CURRENT_TIMESTAMP)`,
	} {
		if _, err := c.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

func TestOdbaceneVrijednostiPutujuRazmjenom(t *testing.T) {
	unraid := noviCvorZaRazmjenu(t, "unraid")
	laptop := noviCvorZaRazmjenu(t, "laptop")
	ctx := context.Background()
	pivnica := letvaUPodrucju(t, unraid, "pivnica-probna")
	unraid.posalji(t, laptop, "")

	sat := time.Now().Truncate(time.Hour).Add(-3 * time.Hour)
	zaglavljeno := uvezeno(pivnica, sat.Add(time.Hour), -1102)
	if _, err := NewReadingRepository(unraid.db, unraid.rec).ImportBatch(ctx,
		[]models.Reading{uvezeno(pivnica, sat, 120), zaglavljeno}); err != nil {
		t.Fatal(err)
	}

	// zapis o odbačenoj vrijednosti je u knjizi, u kanalu očitanja letve
	id := idOdbacene(models.OdbaceneVrijednosti(zaglavljeno, models.VrijednostiIzvanRaspona(zaglavljeno, false))[0])
	v, err := unraid.rec.Latest(ctx, EntityOdbacena, id)
	if err != nil {
		t.Fatalf("odbačena vrijednost nije u knjizi: %v", err)
	}
	if ocekivan := "ocitanja/16/" + sat.Format("2006"); v.Channel != ocekivan {
		t.Errorf("kanal %q, očekivan %q (kao očitanje letve)", v.Channel, ocekivan)
	}

	unraid.posalji(t, laptop, "")
	s, err := NewReadingRepository(laptop.db, laptop.rec).SazetakOdbacenih(ctx, "station:"+pivnica.ID.String())
	if err != nil || s == nil {
		t.Fatalf("laptop ne vidi odbačenu vrijednost: %+v, %v", s, err)
	}
	if s.Broj != 1 || s.Zadnja.Opis != "−1102 cm" || s.Zadnja.Izvor != "hdv.voda.hr" || !s.Zadnja.Kad.Equal(zaglavljeno.MeasuredAt) {
		t.Errorf("sažetak na laptopu: %+v", s)
	}
	// ni vrijednost ni očitanje nisu upisani kao očitanje
	if got, _ := NewReadingRepository(laptop.db, laptop.rec).Get(ctx, zaglavljeno.ID); got != nil {
		t.Errorf("odbačeno očitanje stiglo je razmjenom: %+v", got)
	}

	// drugi čvor koji preuzima istu letvu (cop-osijek-node) i dobije istu
	// vrijednost ne dodaje novu: broji se jednom u cijeloj mreži
	ured := noviCvorZaRazmjenu(t, "ured")
	unraid.do = ""
	unraid.posalji(t, ured, "")
	ponovo := zaglavljeno
	ponovo.ID = uuid.New()
	if _, err := NewReadingRepository(ured.db, ured.rec).ImportBatch(ctx, []models.Reading{ponovo}); err != nil {
		t.Fatal(err)
	}
	if n := brojVerzija(t, ured, EntityOdbacena, id); n != 1 {
		t.Errorf("čvor koji je zapis već primio napisao je svoj: %d verzija", n)
	}
	if s, _ := NewReadingRepository(ured.db, ured.rec).SazetakOdbacenih(ctx, "station:"+pivnica.ID.String()); s == nil || s.Broj != 1 {
		t.Errorf("sažetak na uredskom čvoru: %+v", s)
	}
}

// Isto vrijedi za ćeliju tablice centra koja je odbačena prije nego što je
// postala očitanje
func TestOdbaceneIzTabliceCentraPutujuRazmjenom(t *testing.T) {
	a := noviCvorZaRazmjenu(t, "a")
	b := noviCvorZaRazmjenu(t, "b")
	ctx := context.Background()
	letva := letvaUPodrucju(t, a, "letva-probna")
	kad := time.Date(2026, 9, 1, 5, 0, 0, 0, time.UTC)
	if err := NewReadingRepository(a.db, a.rec).ZabiljeziOdbacene(ctx, []models.OdbacenaVrijednost{
		{Letva: "station:" + letva.ID.String(), Kad: kad, Polje: models.PoljeVodostaj, Opis: "8650 cm", Izvor: "tablica"},
	}); err != nil {
		t.Fatal(err)
	}
	a.posalji(t, b, "")
	s, err := NewReadingRepository(b.db, b.rec).SazetakOdbacenih(ctx, "station:"+letva.ID.String())
	if err != nil || s == nil || s.Broj != 1 || s.Zadnja.Opis != "8650 cm" {
		t.Fatalf("sažetak na drugom čvoru: %+v, %v", s, err)
	}
	v, err := a.rec.Latest(ctx, EntityOdbacena, idOdbacene(s.Zadnja))
	if err != nil || v.Channel != "ocitanja/16/2026" {
		t.Errorf("verzija ćelije tablice: %+v, %v", v, err)
	}
}

// Čvor na 0.0.34 entitet ne poznaje: verziju čuva u knjizi, ali je ne
// stavlja na površinu. Kad se ažurira, obnova površine pri pokretanju je
// stavi, pa se i ono primljeno prije ažuriranja broji.
func TestObnovaPovrsineStavljaOdbacenePrimljeneNaStarijemProgramu(t *testing.T) {
	a := noviCvorZaRazmjenu(t, "a")
	b := noviCvorZaRazmjenu(t, "b")
	ctx := context.Background()
	letva := novaLetva(t, a, "letva-probna", false)
	if err := NewReadingRepository(a.db, a.rec).ZabiljeziOdbacene(ctx, []models.OdbacenaVrijednost{
		{Letva: "station:" + letva.ID.String(), Kad: time.Now().Add(-time.Hour), Polje: models.PoljeVodostaj, Opis: "−1102 cm", Izvor: "hdv.voda.hr"},
	}); err != nil {
		t.Fatal(err)
	}
	vs, err := a.rec.Since(ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.rec.Apply(ctx, vs); err != nil { // samo knjiga, kao stariji program
		t.Fatal(err)
	}
	if n, err := ReplaySurface(ctx, b.db, b.rec); err != nil || n == 0 {
		t.Fatalf("obnova površine: %d, %v", n, err)
	}
	if s, _ := NewReadingRepository(b.db, b.rec).SazetakOdbacenih(ctx, "station:"+letva.ID.String()); s == nil || s.Broj != 1 {
		t.Errorf("nakon obnove površine: %+v", s)
	}
}

func TestOcitanjeLetve(t *testing.T) {
	kad := time.Date(2026, 9, 1, 5, 0, 0, 0, time.UTC)
	if rd := ocitanjeLetve("structure:abc", kad); rd.StructureID != "abc" || rd.StationID != "" || !rd.MeasuredAt.Equal(kad) {
		t.Errorf("objekt: %+v", rd)
	}
	if rd := ocitanjeLetve("station:def", kad); rd.StationID != "def" || rd.StructureID != "" {
		t.Errorf("postaja: %+v", rd)
	}
}

// Kad se verzija zapisa ne može upisati u knjigu, ne ostaje ni redak na
// površini: odbačeno bez verzije drugi čvor nikad ne bi vidio
func TestOdbacenoBezVerzijeSeNeUpisuje(t *testing.T) {
	c := noviCvorZaRazmjenu(t, "a")
	ctx := context.Background()
	repo := NewReadingRepository(c.db, c.rec)
	if _, err := c.db.Exec(`CREATE TRIGGER odbij_verziju BEFORE INSERT ON record_versions
		WHEN NEW.entity = 'odbacena_ocitanja' BEGIN SELECT RAISE(ABORT, 'knjiga odbija'); END`); err != nil {
		t.Fatal(err)
	}
	err := repo.ZabiljeziOdbacene(ctx, []models.OdbacenaVrijednost{
		{Letva: "station:x", Kad: time.Now().Add(-time.Hour), Polje: models.PoljeVodostaj, Opis: "−1102 cm", Izvor: "hdv.voda.hr"},
	})
	if err == nil {
		t.Fatal("greška knjige mora se javiti")
	}
	if n := brojRedaka(t, c.db, `SELECT COUNT(*) FROM odbacena_ocitanja`); n != 0 {
		t.Errorf("na površini je ostalo %d zapisa bez verzije", n)
	}
	c.db.Close()
	if err := repo.ZabiljeziOdbacene(ctx, nil); err == nil {
		t.Error("zatvorena baza mora javiti grešku")
	}
}

// Primljena verzija koja se ne da pročitati ne upisuje ništa
func TestUpsertOdbacenaNeispravnaVerzija(t *testing.T) {
	c := noviCvorZaRazmjenu(t, "a")
	ctx := context.Background()
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := upsertOdbacena(ctx, tx, []byte(`{"kad": 5`)); err == nil {
		t.Error("neispravan zapis mora javiti grešku")
	}
}

// Očitanje izvan raspona koje je preuzeo čvor na 0.0.34 (on raspon ne
// provjerava) stiže razmjenom i upisuje se: primljene verzije se ne
// filtriraju. Filtar pri primanju razdvojio bi površine čvorova (isti zapis
// u knjizi, na površini samo negdje), a novi čvor ne bi upisao ni očitanja
// izvan raspona upisana prije provjere, koja se po odluci ne diraju (što s
// njima, još nije odlučeno). Pravilo zato provodi čvor koji preuzima ili
// uvozi, od 0.0.35; dok preuzima čvor na 0.0.34, takva očitanja stižu.
func TestPrimljenoOcitanjeIzvanRasponaSeNeFiltrira(t *testing.T) {
	stari := noviCvorZaRazmjenu(t, "stari")
	novi := noviCvorZaRazmjenu(t, "novi")
	ctx := context.Background()
	pivnica := novaLetva(t, stari, "pivnica-probna", false)
	stari.posalji(t, novi, "")

	cm := -1102
	rd := models.Reading{ID: uuid.New(), StationID: pivnica.ID.String(), MeasuredAt: time.Now().Add(-time.Hour).UTC().Truncate(time.Second),
		LevelCm: &cm, Source: models.ReadingSourceImport, Origin: "hdv.voda.hr", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if _, err := stari.rec.Record(ctx, stari.db, EntityReadings, rd.ID.String(), rd); err != nil { // kao 0.0.34: bez provjere
		t.Fatal(err)
	}
	stari.posalji(t, novi, EntityReadings)

	got, err := NewReadingRepository(novi.db, novi.rec).Get(ctx, rd.ID)
	if err != nil || got == nil || got.LevelCm == nil || *got.LevelCm != cm {
		t.Fatalf("primljeno očitanje: %+v, %v", got, err)
	}
	if n := brojRedaka(t, novi.db, `SELECT COUNT(*) FROM odbacena_ocitanja`); n != 0 {
		t.Errorf("primljeno očitanje zabilježeno je kao odbačeno: %d", n)
	}
}
