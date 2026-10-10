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
	// dorada (Odluke (8. 10. 2026.), pitanje 21): ne upisuje se odbačena
	// vrijednost, a ostale vrijednosti istog očitanja upisuju se; dobar
	// vodostaj uz pokvaren termometar ulazi bez temperature
	if n != 3 {
		t.Fatalf("upisano %d, očekivana 3 (ispravno, djelomično bez temperature, kota)", n)
	}
	for _, rd := range []models.Reading{zaglavljeno, vodostajNaAkumulaciji, buduce} {
		if got, _ := repo.Get(ctx, rd.ID); got != nil {
			t.Errorf("upisano očitanje izvan raspona: %+v", got)
		}
		if v, _ := c.rec.Latest(ctx, EntityReadings, rd.ID.String()); v != nil {
			t.Errorf("knjiga nosi očitanje izvan raspona: %s", v.Payload)
		}
	}
	got, err := repo.Get(ctx, djelomicno.ID)
	if err != nil || got == nil || got.LevelCm == nil || *got.LevelCm != 215 || got.TempC != nil {
		t.Fatalf("djelomično očitanje: %+v, %v", got, err)
	}
	// ni verzija u knjizi ne nosi odbačenu temperaturu: drugi čvor je ne prima
	v, err := c.rec.Latest(ctx, EntityReadings, djelomicno.ID.String())
	if err != nil || v == nil {
		t.Fatalf("verzija djelomičnog očitanja: %v, %v", v, err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(v.Payload, &m); err != nil {
		t.Fatal(err)
	}
	if _, ima := m["temp_c"]; ima || string(m["level_cm"]) != "215" {
		t.Errorf("verzija djelomičnog očitanja: %s", v.Payload)
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
// napomena i zapornica kad vrijednost otpadne (dorada, Odluke (8. 10.
// 2026.), pitanje 21: ne upisuje se samo odbačena vrijednost)
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

// Dorada (Odluke (8. 10. 2026.), pitanje 21): tri letve akumulacija HEP-a
// zadržavaju raspon kote na svim putovima i kad im oznaku skine istodobna
// izmjena letve na čvoru koji je ne poznaje. Čvor a označi Varaždin
// popravkom; prije nego što oznaka stigne do čvora b, b (0.0.34) izmijeni
// letvu i pošalje verziju bez oznake. Njegova je novija i pobjeđuje na
// svakom čvoru, a popravak je zabilježen kao izveden i više se ne pokreće.
// Oznaka se zato izvodi i iz šifre letve: kota Varaždina (19 089 cm n. m.)
// se i dalje upisuje, a izmjena letve na čvoru s ovim programom oznaku
// ponovo upisuje u knjigu.
func TestLetvaAkumulacijeZadrzavaKotuKadJojIzmjenaSDrugogCvoraSkineOznaku(t *testing.T) {
	a, b := noviCvorZaRazmjenu(t, "a"), noviCvorZaRazmjenu(t, "b")
	ctx := context.Background()
	varazdin := novaLetva(t, a, "gvb-he-varazdin", false)
	a.posalji(t, b, "")
	if err := RunFixups(ctx, a.db, a.rec); err != nil {
		t.Fatal(err)
	}

	// čvor b na 0.0.34: polje ne poznaje, pa ga njegova verzija ne nosi
	stB := *varazdin
	stB.Notes, stB.KotaNM = "Izmijenio Ivo Ivić", false
	if _, err := b.rec.Record(ctx, b.db, EntityStations, varazdin.ID.String(), stB); err != nil {
		t.Fatal(err)
	}
	b.posalji(t, a, "")
	if n := brojRedaka(t, a.db, `SELECT kota_nm FROM stations WHERE id = ?`, varazdin.ID.String()); n != 0 {
		t.Fatalf("verzija s čvora b nije skinula oznaku (kota_nm = %d): test ne ponavlja utrku", n)
	}
	if err := RunFixups(ctx, a.db, a.rec); err != nil {
		t.Fatal(err)
	}

	repoA := NewStationRepository(a.db, a.rec)
	got, err := repoA.GetStationByID(ctx, varazdin.ID)
	if err != nil || got == nil || !got.KotaNM || got.Notes != "Izmijenio Ivo Ivić" {
		t.Fatalf("letva akumulacije na čvoru a: %+v, %v", got, err)
	}
	kota := uvezeno(varazdin, time.Now().Truncate(time.Hour).Add(-time.Hour), 19089)
	kota.Origin = "mletva.voda.hr"
	if n, err := NewReadingRepository(a.db, a.rec).ImportBatch(ctx, []models.Reading{kota}); err != nil || n != 1 {
		t.Fatalf("kota akumulacije nije upisana: %d, %v", n, err)
	}

	// izmjena letve na čvoru a oznaku ponovo upisuje u knjigu
	got.Notes = "Izmijenila Ana Anić"
	if err := repoA.UpdateStation(ctx, got); err != nil {
		t.Fatal(err)
	}
	v, err := a.rec.Latest(ctx, EntityStations, varazdin.ID.String())
	if err != nil || v == nil {
		t.Fatalf("zadnja verzija letve: %v, %v", v, err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(v.Payload, &m); err != nil || string(m["kota_nm"]) != "true" {
		t.Errorf("izmjena letve nije upisala oznaku: %s, %v", v.Payload, err)
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

// odbaciUBuducnosti provjerava očitanje kao da sat računala kasni tri sata,
// pa je očitanje „više od sat unaprijed”, i bilježi to odbacivanje
func odbaciUBuducnosti(t *testing.T, c *cvorZaRazmjenu, rd models.Reading) {
	t.Helper()
	ctx := context.Background()
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	p := novaProvjeraRaspona(tx, c.rec)
	p.sada = rd.MeasuredAt.Add(-3 * time.Hour)
	if upisati, err := p.propusti(ctx, &rd); err != nil || upisati {
		t.Fatalf("očitanje tri sata unaprijed: upisati %v, %v", upisati, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// Dorada (Odluke (8. 10. 2026.), pitanje 21): očitanje odbačeno samo zato
// što je stiglo više od sat unaprijed (sat izvora ili računala ide naprijed)
// sljedeći krug primi, kad mu vrijeme dođe. Tada se više ne broji kao
// odbačeno, jer ga letva ima; odbačena vrijednost (vodostaj izvan raspona)
// broji se i kad letva za isti sat poslije dobije ispravno očitanje.
func TestOdbacenoVrijemeNeBrojiSeKadLetvaPoslijeImaOcitanje(t *testing.T) {
	a, b := noviCvorZaRazmjenu(t, "a"), noviCvorZaRazmjenu(t, "b")
	ctx := context.Background()
	repo := NewReadingRepository(a.db, a.rec)
	pivnica := novaLetva(t, a, "pivnica-probna", false)
	kad := time.Now().Truncate(time.Hour).Add(-time.Hour)

	rano := uvezeno(pivnica, kad, 120)
	odbaciUBuducnosti(t, a, rano)
	zaglavljeno := uvezeno(pivnica, kad.Add(-2*time.Hour), -1102)
	if _, err := repo.ImportBatch(ctx, []models.Reading{zaglavljeno}); err != nil {
		t.Fatal(err)
	}
	if s, err := repo.SazetakOdbacenih(ctx, "station:"+pivnica.ID.String()); err != nil || s == nil || s.Broj != 2 {
		t.Fatalf("prije upisa: %+v, %v", s, err)
	}

	// sljedeći krug isto očitanje prima; ispravno očitanje u satu
	// zaglavljenog mjerila ne briše odbačeni vodostaj
	ispravljeno := uvezeno(pivnica, zaglavljeno.MeasuredAt, 118)
	if n, err := repo.ImportBatch(ctx, []models.Reading{rano, ispravljeno}); err != nil || n != 2 {
		t.Fatalf("drugi krug: %d, %v", n, err)
	}
	s, err := repo.SazetakOdbacenih(ctx, "station:"+pivnica.ID.String())
	if err != nil || s == nil || s.Broj != 1 || s.Zadnja.Polje != models.PoljeVodostaj || s.Zadnja.Opis != "−1102 cm" {
		t.Errorf("poslije upisa: %+v, %v", s, err)
	}

	// drugi čvor dobije zapis i očitanje razmjenom: ni on ga ne broji
	a.posalji(t, b, "")
	s, err = NewReadingRepository(b.db, b.rec).SazetakOdbacenih(ctx, "station:"+pivnica.ID.String())
	if err != nil || s == nil || s.Broj != 1 || s.Zadnja.Polje != models.PoljeVodostaj {
		t.Errorf("na drugom čvoru: %+v, %v", s, err)
	}

	// objekt: zapis odbačen zbog vremena, pa primljen, ne ostaje ni kao sažetak
	cm := 215
	cs := models.Reading{ID: uuid.New(), StructureID: uuid.NewString(), MeasuredAt: kad, LevelCm: &cm,
		Origin: models.ReadingOriginBP16, Source: models.ReadingSourceManual}
	odbaciUBuducnosti(t, a, cs)
	if s, _ := repo.SazetakOdbacenih(ctx, cs.GaugeKey()); s == nil || s.Broj != 1 {
		t.Fatalf("objekt prije upisa: %+v", s)
	}
	if n, err := repo.ImportBatch(ctx, []models.Reading{cs}); err != nil || n != 1 {
		t.Fatalf("objekt: %d, %v", n, err)
	}
	if s, err := repo.SazetakOdbacenih(ctx, cs.GaugeKey()); err != nil || s != nil {
		t.Errorf("objekt poslije upisa: %+v, %v", s, err)
	}
}

// Dorada (Odluke (8. 10. 2026.), pitanje 21): preuzimanje i uvoz ne upisuju
// vrijednost izvan raspona, a ostale vrijednosti istog očitanja upisuju.
// Zaglavljeno mjerilo Popovca (hdv.voda.hr, 8. 10. 2026.) javlja −1104 cm
// uz protok 0,0 m³/s: upisuje se protok, vodostaj se broji kao odbačen.
//
// Sat je time zauzet. Kao i za svako očitanje koje letva već ima, ispravak
// koji isti izvor poslije pošalje za taj sat ne upisuje se sam (isti
// identitet, uvoz ga preskače). Tako je bilo i prije provjere raspona, kad
// se upisivao i sam −1104 cm; dopuna upisanog očitanja nije dio odluke.
func TestUvozZaglavljenogMjerilaUpisujeOstatakOcitanja(t *testing.T) {
	c := noviCvorZaRazmjenu(t, "a")
	ctx := context.Background()
	repo := NewReadingRepository(c.db, c.rec)
	popovac := novaLetva(t, c, "popovac-probni", false)
	kad := time.Now().Truncate(time.Hour).Add(-2 * time.Hour)
	zaglavljeno := uvezeno(popovac, kad, -1104)
	nula := 0.0
	zaglavljeno.FlowM3s = &nula

	if n, err := repo.ImportBatch(ctx, []models.Reading{zaglavljeno}); err != nil || n != 1 {
		t.Fatalf("zaglavljeno mjerilo: upisano %d, %v", n, err)
	}
	got, err := repo.Get(ctx, zaglavljeno.ID)
	if err != nil || got == nil || got.LevelCm != nil || got.FlowM3s == nil || *got.FlowM3s != 0 {
		t.Fatalf("ostatak očitanja: %+v, %v", got, err)
	}
	s, err := repo.SazetakOdbacenih(ctx, "station:"+popovac.ID.String())
	if err != nil || s == nil || s.Broj != 1 || s.Zadnja.Opis != "−1104 cm" {
		t.Errorf("sažetak: %+v, %v", s, err)
	}

	// izvor poslije ispravi vodostaj istog sata: očitanje već postoji
	ispravljeno := uvezeno(popovac, kad, 14)
	ispravljeno.ID, ispravljeno.FlowM3s = zaglavljeno.ID, &nula
	if n, err := repo.ImportBatch(ctx, []models.Reading{ispravljeno}); err != nil || n != 0 {
		t.Fatalf("ispravljeni vodostaj: upisano %d, %v", n, err)
	}
	if got, _ := repo.Get(ctx, zaglavljeno.ID); got == nil || got.LevelCm != nil {
		t.Errorf("upisano očitanje dopunjeno je: %+v", got)
	}
}
