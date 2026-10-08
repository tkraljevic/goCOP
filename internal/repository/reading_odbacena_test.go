package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"

	"github.com/google/uuid"
)

// Odluke (8. 10. 2026.), pitanje 21: svako uvezeno očitanje (satno
// preuzimanje, rezervni izvori, BP16, tablica centra, lijepljenje) prolazi
// kroz ImportBatch, a ondje istu provjeru raspona kao ručni upis. Vrijednost
// izvan raspona se ne upisuje, nego se bilježi za stranicu letve.

func novaLetva(t *testing.T, c *cvorZaRazmjenu, sifra string, kota bool) *models.Station {
	t.Helper()
	st := &models.Station{ID: uuid.New(), Code: sifra, Name: "Letva " + sifra, KotaNM: kota}
	if err := NewStationRepository(c.db, c.rec).CreateStation(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	return st
}

func uvezeno(st *models.Station, kad time.Time, cm int) models.Reading {
	v := cm
	return models.Reading{ID: uuid.New(), StationID: st.ID.String(), MeasuredAt: kad, LevelCm: &v,
		Source: models.ReadingSourceImport, Origin: "hdv.voda.hr"}
}

func TestUvozNeUpisujeVrijednostiIzvanRaspona(t *testing.T) {
	c := noviCvorZaRazmjenu(t, "a")
	ctx := context.Background()
	repo := NewReadingRepository(c.db, c.rec)
	pivnica := novaLetva(t, c, "pivnica-probna", false)
	akumulacija := novaLetva(t, c, "akumulacija-probna", true)
	sat := time.Now().Truncate(time.Hour).Add(-6 * time.Hour)

	dobro := uvezeno(pivnica, sat, 120)
	zaglavljeno := uvezeno(pivnica, sat.Add(time.Hour), -1102)
	djelomicno := uvezeno(pivnica, sat.Add(2*time.Hour), 215)
	temp := 85.0
	djelomicno.TempC = &temp
	kota := uvezeno(akumulacija, sat, 19089)
	vodostajNaAkumulaciji := uvezeno(akumulacija, sat.Add(time.Hour), 300)
	buduce := uvezeno(pivnica, time.Now().Add(3*time.Hour), 120)
	serija := []models.Reading{dobro, zaglavljeno, djelomicno, kota, vodostajNaAkumulaciji, buduce}

	n, err := repo.ImportBatch(ctx, serija)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("upisano %d, očekivana 3 (ispravno, djelomično bez temperature, kota)", n)
	}
	for _, rd := range []models.Reading{zaglavljeno, vodostajNaAkumulaciji, buduce} {
		if got, _ := repo.Get(ctx, rd.ID); got != nil {
			t.Errorf("upisano očitanje izvan raspona: %+v", got)
		}
	}
	got, err := repo.Get(ctx, djelomicno.ID)
	if err != nil || got == nil || got.LevelCm == nil || *got.LevelCm != 215 || got.TempC != nil {
		t.Fatalf("djelomično očitanje: %+v, %v", got, err)
	}
	// ni verzija u knjizi ne nosi odbačenu temperaturu: drugi čvor je ne prima
	v, err := c.rec.Latest(ctx, EntityReadings, djelomicno.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(v.Payload, &m); err != nil {
		t.Fatal(err)
	}
	if _, ima := m["temp_c"]; ima {
		t.Errorf("verzija nosi odbačenu temperaturu: %s", v.Payload)
	}
	if got, _ := repo.Get(ctx, kota.ID); got == nil {
		t.Error("kota akumulacije nije upisana")
	}

	// odbačeno je zabilježeno po letvi
	if n := brojRedaka(t, c.db, `SELECT COUNT(*) FROM odbacena_ocitanja`); n != 4 {
		t.Errorf("zabilježeno %d odbačenih, očekivano 4", n)
	}
	s, err := repo.SazetakOdbacenih(ctx, "station:"+pivnica.ID.String())
	if err != nil || s == nil {
		t.Fatalf("sažetak: %+v, %v", s, err)
	}
	// zadnja je ona s najkasnijim trenutkom (budućnost), sve zabilježene odjednom
	if s.Broj != 3 || s.Zadnja.Polje != models.PoljeVrijeme || s.Zadnja.Izvor != "hdv.voda.hr" {
		t.Errorf("sažetak Pivnice: %+v", s)
	}
	s, err = repo.SazetakOdbacenih(ctx, "station:"+akumulacija.ID.String())
	if err != nil || s == nil || s.Broj != 1 || s.Zadnja.Opis != "300 cm n. m." || !s.Zadnja.Kad.Equal(sat.Add(time.Hour)) {
		t.Errorf("sažetak akumulacije: %+v, %v", s, err)
	}

	// isti prozor sati sljedeći sat: ništa se ne upisuje ni ne broji dvaput
	ponovo := []models.Reading{dobro, zaglavljeno, djelomicno, kota, vodostajNaAkumulaciji}
	ponovo[1].ID = uuid.New() // isti trenutak, novo preuzimanje
	if n, err := repo.ImportBatch(ctx, ponovo); err != nil || n != 0 {
		t.Errorf("ponovni uvoz: %d, %v", n, err)
	}
	if n := brojRedaka(t, c.db, `SELECT COUNT(*) FROM odbacena_ocitanja`); n != 4 {
		t.Errorf("ponovni uvoz udvostručio je odbačene: %d", n)
	}

	// letva bez odbačenih nema sažetka
	if s, err := repo.SazetakOdbacenih(ctx, "station:"+uuid.NewString()); err != nil || s != nil {
		t.Errorf("letva bez odbačenih: %+v, %v", s, err)
	}
}

// Očitanje objekta (ustava, crpna stanica) vodi vodostaj; uz njega ostaju
// napomena i zapornica kad vrijednost otpadne
func TestUvozObjektaZadrzavaOstatakOcitanja(t *testing.T) {
	c := noviCvorZaRazmjenu(t, "a")
	ctx := context.Background()
	repo := NewReadingRepository(c.db, c.rec)
	nizvodno := -900
	rd := models.Reading{ID: uuid.New(), StructureID: uuid.NewString(), MeasuredAt: time.Now().Add(-time.Hour),
		Level2Cm: &nizvodno, Gate: models.GateClosed, Origin: models.ReadingOriginBP16, Source: models.ReadingSourceManual}
	if n, err := repo.ImportBatch(ctx, []models.Reading{rd}); err != nil || n != 1 {
		t.Fatalf("ustava: %d, %v", n, err)
	}
	got, _ := repo.Get(ctx, rd.ID)
	if got == nil || got.Level2Cm != nil || got.Gate != models.GateClosed {
		t.Errorf("ustava: %+v", got)
	}
	s, err := repo.SazetakOdbacenih(ctx, rd.GaugeKey())
	if err != nil || s == nil || s.Broj != 1 || s.Zadnja.Opis != "−900 cm nizvodno" || s.IzvorLabel() != "evidencija VGI Baranja" {
		t.Errorf("sažetak ustave: %+v, %v", s, err)
	}
}

// Kad se odbačeno ne može zabilježiti, serija pada cijela, kao i pri svakoj
// drugoj grešci upisa
func TestUvozPadaKadSeOdbacenoNeMozeZabiljeziti(t *testing.T) {
	c := noviCvorZaRazmjenu(t, "a")
	ctx := context.Background()
	repo := NewReadingRepository(c.db, c.rec)
	pivnica := novaLetva(t, c, "pivnica-probna", false)
	if _, err := c.db.Exec(`CREATE TRIGGER odbij_biljezenje BEFORE INSERT ON odbacena_ocitanja
		BEGIN SELECT RAISE(ABORT, 'bilježenje odbijeno'); END`); err != nil {
		t.Fatal(err)
	}
	sat := time.Now().Add(-3 * time.Hour)
	n, err := repo.ImportBatch(ctx, []models.Reading{uvezeno(pivnica, sat, 120), uvezeno(pivnica, sat.Add(time.Hour), -1102)})
	if err == nil || n != 0 {
		t.Fatalf("serija s neuspjelim bilježenjem: %d, %v", n, err)
	}
	if n := brojRedaka(t, c.db, `SELECT COUNT(*) FROM readings`); n != 0 {
		t.Errorf("poništena serija ostavila je %d očitanja", n)
	}
	if err := repo.ZabiljeziOdbacene(ctx, []models.OdbacenaVrijednost{{Letva: "station:x", Kad: sat, Polje: models.PoljeVodostaj}}); err == nil {
		t.Error("ZabiljeziOdbacene mora javiti grešku baze")
	}
}

// Kad se oznaka kote letve ne može pročitati, serija pada: bez oznake bi se
// kota akumulacije odbacila kao vodostaj
func TestUvozPadaKadSeOznakaKoteNeMozeProcitati(t *testing.T) {
	c := noviCvorZaRazmjenu(t, "a")
	ctx := context.Background()
	repo := NewReadingRepository(c.db, c.rec)
	akumulacija := novaLetva(t, c, "akumulacija-probna", true)
	if _, err := c.db.Exec(`ALTER TABLE stations RENAME COLUMN kota_nm TO kota_stara`); err != nil {
		t.Fatal(err)
	}
	if n, err := repo.ImportBatch(ctx, []models.Reading{uvezeno(akumulacija, time.Now().Add(-time.Hour), 19089)}); err == nil || n != 0 {
		t.Fatalf("serija bez oznake kote: %d, %v", n, err)
	}
}

func TestZabiljeziOdbaceneISazetak(t *testing.T) {
	c := noviCvorZaRazmjenu(t, "a")
	ctx := context.Background()
	repo := NewReadingRepository(c.db, c.rec)
	kad := time.Date(2026, 9, 1, 5, 0, 0, 0, time.UTC)
	odb := []models.OdbacenaVrijednost{
		{Letva: "station:a", Kad: kad, Polje: models.PoljeVodostaj, Opis: "8650 cm", Izvor: "tablica"},
		{Letva: "station:a", Kad: kad.Add(24 * time.Hour), Polje: models.PoljeVodostaj, Opis: "8655 cm", Izvor: "tablica"},
	}
	if err := repo.ZabiljeziOdbacene(ctx, odb); err != nil {
		t.Fatal(err)
	}
	if err := repo.ZabiljeziOdbacene(ctx, odb); err != nil {
		t.Fatal(err)
	}
	s, err := repo.SazetakOdbacenih(ctx, "station:a")
	if err != nil || s == nil || s.Broj != 2 || s.Zadnja.Opis != "8655 cm" || s.Zadnja.Letva != "station:a" {
		t.Errorf("sažetak: %+v, %v", s, err)
	}
	c.db.Close()
	if _, err := repo.SazetakOdbacenih(ctx, "station:a"); err == nil {
		t.Error("zatvorena baza mora javiti grešku")
	}
}

// Oznaka „kota u cm n. m.” je polje letve: sprema se, mijenja i putuje
// razmjenom kao i ostala polja
func TestOznakaKoteLetvePutujeRazmjenom(t *testing.T) {
	a := noviCvorZaRazmjenu(t, "a")
	b := noviCvorZaRazmjenu(t, "b")
	ctx := context.Background()
	stA := NewStationRepository(a.db, a.rec)
	st := novaLetva(t, a, "akumulacija-probna", true)
	got, err := stA.GetStationByID(ctx, st.ID)
	if err != nil || got == nil || !got.KotaNM {
		t.Fatalf("spremljena oznaka: %+v, %v", got, err)
	}
	a.posalji(t, b, "")
	naB, err := NewStationRepository(b.db, b.rec).GetStationByID(ctx, st.ID)
	if err != nil || naB == nil || !naB.KotaNM {
		t.Fatalf("primljena oznaka: %+v, %v", naB, err)
	}
	got.KotaNM = false
	if err := stA.UpdateStation(ctx, got); err != nil {
		t.Fatal(err)
	}
	if got, _ := stA.GetStationByID(ctx, st.ID); got.KotaNM {
		t.Error("oznaka nije skinuta")
	}
	a.posalji(t, b, "")
	if naB, _ := NewStationRepository(b.db, b.rec).GetStationByID(ctx, st.ID); naB.KotaNM {
		t.Error("skinuta oznaka nije stigla razmjenom")
	}
}

// Popravak podataka označi tri letve akumulacija HEP-a jednom verzijom
// svaku; ostale letve ne dira, a čvor koji je oznaku već primio ne piše svoju
func TestPopravakOznacujeLetveAkumulacija(t *testing.T) {
	a := noviCvorZaRazmjenu(t, "a")
	b := noviCvorZaRazmjenu(t, "b")
	ctx := context.Background()
	varazdin := novaLetva(t, a, "gvb-he-varazdin", false)
	dubrava := novaLetva(t, a, "gvb-he-dubrava", false)
	botovo := novaLetva(t, a, "botovo", false)
	a.posalji(t, b, "")

	if err := RunFixups(ctx, a.db, a.rec); err != nil {
		t.Fatal(err)
	}
	repo := NewStationRepository(a.db, a.rec)
	for _, st := range []*models.Station{varazdin, dubrava} {
		got, _ := repo.GetStationByID(ctx, st.ID)
		if got == nil || !got.KotaNM {
			t.Errorf("%s nije označena: %+v", st.Code, got)
		}
		if n := brojVerzija(t, a, EntityStations, st.ID.String()); n != 2 {
			t.Errorf("%s: %d verzija, očekivane 2", st.Code, n)
		}
	}
	if got, _ := repo.GetStationByID(ctx, botovo.ID); got.KotaNM || brojVerzija(t, a, EntityStations, botovo.ID.String()) != 1 {
		t.Errorf("obična letva je dirana: %+v", got)
	}

	// drugi čvor primi označene letve prije svog popravka i ne piše ništa
	a.posalji(t, b, "")
	if err := RunFixups(ctx, b.db, b.rec); err != nil {
		t.Fatal(err)
	}
	if n := brojVerzija(t, b, EntityStations, varazdin.ID.String()); n != 2 {
		t.Errorf("na drugom čvoru %d verzija Varaždina, očekivane 2", n)
	}
	if got, _ := NewStationRepository(b.db, b.rec).GetStationByID(ctx, varazdin.ID); got == nil || !got.KotaNM {
		t.Errorf("drugi čvor: %+v", got)
	}
}

// Letvu koju je zadnji izmijenio noviji program popravak preskače i ne
// bilježi se kao izveden
func TestPopravakKotePreskaceLetvuNovijeSheme(t *testing.T) {
	c := noviCvorZaRazmjenu(t, "a")
	ctx := context.Background()
	cakovec := novaLetva(t, c, "gvb-he-cakovec", false)
	top, err := c.rec.Latest(ctx, EntityStations, cakovec.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	vid, _ := uuid.NewV7()
	if _, err := c.rec.Apply(ctx, []ledger.Version{{VersionID: vid.String(), Entity: EntityStations,
		EntityID: cakovec.ID.String(), NodeID: "noviji", Supersedes: top.VersionID, Payload: top.Payload,
		CreatedAt: time.Now(), SchemaVersion: ledger.ShemaEntiteta(EntityStations) + 1, Channel: top.Channel}}); err != nil {
		t.Fatal(err)
	}
	if err := RunFixups(ctx, c.db, c.rec); err != nil {
		t.Fatalf("pokretanje palo: %v", err)
	}
	if n := brojRedaka(t, c.db, `SELECT COUNT(*) FROM data_fixups WHERE name = 'letve-akumulacija-kota'`); n != 0 {
		t.Error("popravak s preskočenom letvom zabilježen je kao izveden")
	}
	if n := brojVerzija(t, c, EntityStations, cakovec.ID.String()); n != 2 {
		t.Errorf("letva novije sheme je prepisana: %d verzija", n)
	}
}

func TestOznaciKotu(t *testing.T) {
	m := map[string]json.RawMessage{}
	if ok, err := oznaciKotu(m); !ok || err != nil || string(m["kota_nm"]) != "true" {
		t.Errorf("prvo označavanje: %v, %v, %s", ok, err, m["kota_nm"])
	}
	if ok, err := oznaciKotu(m); ok || err != nil {
		t.Errorf("već označena: %v, %v", ok, err)
	}
}
