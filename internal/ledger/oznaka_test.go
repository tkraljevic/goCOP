package ledger

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// oznakaU je oznaka ispravnog oblika s vremenom t i zadanim krajem
func oznakaU(t time.Time, kraj string) string {
	return oznakaZa(t)[:14] + "7000-8000-" + kraj
}

// Oznaka verzije vrijedi samo u obliku koji program upisuje: UUIDv7, 36
// znakova, mala slova, crtice na mjestu, inačica 7 i varijanta RFC 9562.
// Prava nova oznaka uvijek prolazi.
func TestIspravnaOznaka(t *testing.T) {
	nova, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct {
		id    string
		valja bool
	}{
		{nova.String(), true},
		{"0190a000-0000-7000-8000-000000000001", true},
		{"0190a000-0000-7abc-bfff-ffffffffffff", true},
		{"", false},
		{"~", false},
		{"0002", false},
		{"0190A000-0000-7000-8000-000000000001", false},   // velika slova
		{"0190a00000007000800000000000000001", false},     // bez crtica
		{"{0190a000-0000-7000-8000-000000000001}", false}, // u zagradama
		{"urn:uuid:0190a000-0000-7000-8000-000000000001", false},
		{"0190a000-0000-4000-8000-000000000001", false}, // inačica 4
		{"0190a000-0000-7000-c000-000000000001", false}, // druga varijanta
		{"0190a000-0000-7000-8000-00000000000g", false},
		{"0190a000-0000-7000-8000-0000000000001", false},
		{"0190a000-0000-7000-8000-00000000001\n", false},
	} {
		if got := IspravnaOznaka(p.id); got != p.valja {
			t.Errorf("IspravnaOznaka(%q) = %v", p.id, got)
		}
		if got := Ispravna(Version{VersionID: p.id, Entity: "stations", EntityID: "s1"}); got != p.valja {
			t.Errorf("Ispravna s oznakom %q = %v", p.id, got)
		}
	}
	if _, ok := VrijemeOznake("~"); ok {
		t.Error("vrijeme iz neispravne oznake")
	}
	if got, ok := VrijemeOznake("0190a000-0000-7000-8000-000000000001"); !ok || got.UnixMilli() != 0x0190a0000000 {
		t.Errorf("vrijeme oznake: %v %v", got, ok)
	}
}

// Primljena verzija s vremenom do pet minuta ispred sata prolazi, a s
// kasnijim ne. Verzija s imenom ovog računala ne smije biti ni malo ispred
// sata; starija prolazi, pa računalo koje je izgubilo bazu dobije svoje
// zapise natrag.
func TestProvjeriPrimljenu(t *testing.T) {
	sada := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	verzija := func(t time.Time, autor string) Version {
		return Version{VersionID: oznakaU(t, "000000000001"), Entity: "stations", EntityID: "s1", NodeID: autor}
	}
	for _, p := range []struct {
		ime    string
		v      Version
		razlog error
	}{
		{"sada", verzija(sada, "cop-osijek"), nil},
		{"jučer", verzija(sada.Add(-24*time.Hour), "cop-osijek"), nil},
		{"na granici", verzija(sada.Add(NajviseUnaprijed), "cop-osijek"), nil},
		{"iza granice", verzija(sada.Add(NajviseUnaprijed+time.Second), "cop-osijek"), ErrIzBuducnosti},
		{"najkasnije moguće", Version{VersionID: "ffffffff-ffff-7fff-bfff-ffffffffffff", Entity: "stations", EntityID: "s1"}, ErrIzBuducnosti},
		{"vlastita iz prošlosti", verzija(sada.Add(-time.Hour), "laptop-baranja"), nil},
		{"vlastita sada", verzija(sada, "laptop-baranja"), nil},
		{"vlastita ispred sata", verzija(sada.Add(time.Second), "laptop-baranja"), ErrVlastitaUnaprijed},
		{"bez oznake", Version{Entity: "stations", EntityID: "s1"}, ErrBezOznake},
		{"krivi oblik", Version{VersionID: "~", Entity: "stations", EntityID: "s1"}, ErrOblikOznake},
		{"znak | u autoru", verzija(sada.Add(-time.Hour), "cop-osijek|ocitanja/16/2026"), ErrOblikAutora},
		{"znak | u kanalu", Version{VersionID: oznakaU(sada, "000000000001"), Entity: "stations", EntityID: "s1", NodeID: "cop-osijek", Channel: "ocitanja/16|2026"}, ErrOblikAutora},
		{"kanal očitanja", Version{VersionID: oznakaU(sada, "000000000001"), Entity: "stations", EntityID: "s1", NodeID: "cop-osijek", Channel: "ocitanja/16/2026"}, nil},
	} {
		if err := ProvjeriPrimljenu(p.v, "laptop-baranja", sada); !errors.Is(err, p.razlog) {
			t.Errorf("%s: %v, očekivano %v", p.ime, err, p.razlog)
		}
	}
	// bez imena ovog računala vlastitog pravila nema
	if err := ProvjeriPrimljenu(verzija(sada.Add(time.Second), ""), "", sada); err != nil {
		t.Errorf("bez imena računala: %v", err)
	}
}

// Sat provjere ne ide iza zadnje oznake koju je knjiga dala: verzija koja
// nije kasnija od upravo napisane nije „iz budućnosti” ni kad je sat
// računala u međuvremenu skočio unatrag.
func TestSadaNeIdeIzaZadnjeOznake(t *testing.T) {
	prije := zadnjaMs.Load()
	t.Cleanup(func() { zadnjaMs.Store(prije) })
	if Sada().Before(time.Now().Add(-time.Second)) {
		t.Fatalf("sat provjere zaostaje: %v", Sada())
	}
	kasnije := time.Now().Add(time.Hour).Truncate(time.Millisecond)
	zapamtiOznaku(uuid.MustParse(oznakaU(kasnije, "000000000001")), nil)
	if !Sada().Equal(kasnije) {
		t.Errorf("sat provjere %v, očekivano %v", Sada(), kasnije)
	}
	zapamtiOznaku(uuid.MustParse(oznakaU(kasnije.Add(-time.Minute), "000000000001")), nil)
	zapamtiOznaku(uuid.UUID{}, errors.New("nema slučajnih brojeva"))
	if !Sada().Equal(kasnije) {
		t.Errorf("sat provjere se vratio: %v", Sada())
	}
}

// Iza verzije s najkasnijim mogućim vremenom ne može se upisati nova:
// zbrajanje bi vrijeme vratilo na 1970. i nova bi stala ispred svih.
// Iza obične verzije nova je odmah iza nje.
func TestOznakaIzaNajkasnije(t *testing.T) {
	if id, err := oznakaVerzije("ffffffff-ffff-7fff-bfff-ffffffffffff"); err == nil {
		t.Fatalf("iza najkasnije: %s", id)
	}
	iza := "0190a000-0000-7000-8000-000000000001"
	id, err := oznakaVerzije(iza)
	if err != nil || !IspravnaOznaka(id) || id <= iza || !strings.HasPrefix(id, "0190a000-0001-7") {
		t.Errorf("iza %s: %s %v", iza, id, err)
	}
	if id, err := oznakaVerzije("ffffffff-fffe-7fff-bfff-ffffffffffff"); err != nil || !strings.HasPrefix(id, "ffffffff-ffff-7") {
		t.Errorf("iza predzadnje: %s %v", id, err)
	}
}

// Pokretanje broji verzije u knjizi koje razmjena ne bi primila (oblik i
// budućnost) i vraća prve, a ispravne i vlastite iz prošlosti ne broji.
// Knjigu ne mijenja.
func TestNeispravneUKnjizi(t *testing.T) {
	db := openTestDB(t)
	rec := New(db, "laptop-baranja")
	ctx := context.Background()
	sada := time.Now()
	ubaci := func(id string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel)
			VALUES (?, 'stations', 's1', 'cop-osijek', '', 0, '{}', ?, 1, '')`, id, sada.UTC()); err != nil {
			t.Fatal(err)
		}
	}
	if n, primjeri, err := rec.NeispravneUKnjizi(ctx, sada, 5); n != 0 || len(primjeri) != 0 || err != nil {
		t.Fatalf("prazna knjiga: %d %v %v", n, primjeri, err)
	}
	ubaci(oznakaU(sada.Add(-time.Hour), "000000000001"))
	ubaci(oznakaU(sada.Add(NajviseUnaprijed-time.Second), "000000000002"))
	ubaci("~")
	ubaci("0190A000-0000-7000-8000-000000000001")
	ubaci("0190a000-0000-7000-c000-000000000001") // druga varijanta
	ubaci("ffffffff-ffff-7fff-bfff-ffffffffffff")
	n, primjeri, err := rec.NeispravneUKnjizi(ctx, sada, 2)
	if err != nil || n != 4 || len(primjeri) != 2 {
		t.Fatalf("neispravnih %d, primjera %d, %v", n, len(primjeri), err)
	}
	if primjeri[0].VersionID != "0190A000-0000-7000-8000-000000000001" || primjeri[1].VersionID != "0190a000-0000-7000-c000-000000000001" {
		t.Errorf("primjeri: %s, %s", primjeri[0].VersionID, primjeri[1].VersionID)
	}
	if n := brojVerzija(t, db); n != 6 {
		t.Errorf("u knjizi %d verzija, očekivano 6", n)
	}
	db.Close()
	if _, _, err := rec.NeispravneUKnjizi(ctx, sada, 1); err == nil {
		t.Error("zatvorena baza: nema greške")
	}
}

// ubaciMimoProvjere upiše verziju ravno u tablicu, kao što ju je upisao
// program koji oznaku nije provjeravao
func ubaciMimoProvjere(t *testing.T, db *sql.DB, v Version) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel)
		VALUES (?, ?, ?, ?, '', 0, '{}', ?, 1, ?)`, v.VersionID, v.Entity, v.EntityID, v.NodeID, v.CreatedAt.UTC(), v.Channel); err != nil {
		t.Fatal(err)
	}
}

// Granica se slaže samo od verzija koje bi razmjena danas primila. Verzija
// koja je u knjigu ušla prije provjere (oznaka krivog oblika, vrijeme iz
// budućnosti, autor sa znakom |, koji bi inače dao isti ključ kao pravi
// autor u kanalu) ostaje u knjizi, ali granicu ne pomiče, pa autorove
// novije verzije i dalje stižu.
func TestGranicaSamoIzIspravnih(t *testing.T) {
	ctx := context.Background()
	sada := time.Now()
	const kanal = "ocitanja/16/2026"
	ver := func(id, autor, kanal string) Version {
		return Version{VersionID: id, Entity: "readings", EntityID: id, NodeID: autor, Channel: kanal, CreatedAt: sada}
	}
	r1, r2 := oznakaU(sada.Add(-4*time.Hour), "000000000001"), oznakaU(sada.Add(-3*time.Hour), "000000000002")
	r3 := oznakaU(sada.Add(-2*time.Hour), "000000000003")
	db := openTestDB(t)
	p := New(db, "laptop-baranja")
	if _, err := p.Apply(ctx, []Version{ver(r1, "cop-osijek", kanal), ver(r2, "cop-osijek", kanal), ver(r1[:35]+"9", "ured", "")}); err != nil {
		t.Fatal(err)
	}
	for _, v := range []Version{
		ver(oznakaU(sada.Add(-5*time.Hour), "00000000000f"), "cop-osijek|"+kanal, ""),
		ver("~", "cop-osijek", kanal),
		ver("ffffffff-ffff-7fff-bfff-ffffffffffff", "ured", ""),
		ver(oznakaU(sada.Add(NajviseUnaprijed+time.Minute), "000000000004"), "cop-osijek", kanal),
	} {
		ubaciMimoProvjere(t, db, v)
	}
	granica, err := p.Frontier(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := granica[FrontierKey("cop-osijek", kanal)]; got != r2 {
		t.Errorf("granica autora u kanalu %q, očekivano %q", got, r2)
	}
	if got := granica["ured"]; got != r1[:35]+"9" {
		t.Errorf("granica ureda %q", got)
	}
	if len(granica) != 2 {
		t.Errorf("granica %v", granica)
	}

	// autor ima r3, i to drugoj strani pošalje
	a := New(openTestDB(t), "cop-osijek")
	if _, err := a.Apply(ctx, []Version{ver(r1, "cop-osijek", kanal), ver(r2, "cop-osijek", kanal), ver(r3, "cop-osijek", kanal)}); err != nil {
		t.Fatal(err)
	}
	delta, err := a.Delta(ctx, granica, nil, 5000)
	if err != nil || len(delta) != 1 || delta[0].VersionID != r3 {
		t.Errorf("delta autora %v, %v", delta, err)
	}
}

// Sažimanje i prorjeđivanje čuvaju verziju od koje se slaže granica i kad
// iza nje u istom ključu stoji verzija koju granica ne broji
func TestSazimanjeCuvaVerzijuGranice(t *testing.T) {
	ctx := context.Background()
	staro := time.Now().Add(-60 * 24 * time.Hour)
	const kanal = "ocitanja/16/2026"
	for _, brisi := range []func(r *Recorder) error{
		func(r *Recorder) error { _, err := r.Compact(ctx, time.Now()); return err },
		func(r *Recorder) error { _, err := r.Prorijedi(ctx, "readings", time.Now()); return err },
		func(r *Recorder) error { _, err := r.ProrijediZamijenjene(ctx, "readings"); return err },
	} {
		db := openTestDB(t)
		r := New(db, "laptop-baranja")
		prva, druga := oznakaU(staro, "000000000001"), oznakaU(staro.Add(time.Hour), "000000000002")
		// druga je zadnja verzija autora koju granica broji, ali ne i zadnja
		// verzija svog zapisa
		for _, v := range []Version{
			{VersionID: prva, Entity: "readings", EntityID: "z1", NodeID: "cop-osijek", Channel: kanal, CreatedAt: staro},
			{VersionID: druga, Entity: "readings", EntityID: "z2", NodeID: "cop-osijek", Channel: kanal, CreatedAt: staro},
			{VersionID: oznakaU(staro.Add(2*time.Hour), "000000000003"), Entity: "readings", EntityID: "z2", NodeID: "ured", Channel: kanal, CreatedAt: staro},
			{VersionID: "~", Entity: "readings", EntityID: "z3", NodeID: "cop-osijek", Channel: kanal, CreatedAt: staro},
		} {
			ubaciMimoProvjere(t, db, v)
		}
		if err := brisi(r); err != nil {
			t.Fatal(err)
		}
		granica, err := r.Frontier(ctx)
		if err != nil || granica[FrontierKey("cop-osijek", kanal)] != druga {
			t.Errorf("granica nakon brisanja %v, %v", granica, err)
		}
	}
}

// Redak knjige koji se ne da pročitati (vrijeme koje nije vrijeme) vraća
// grešku čitanja, a ne prazan popis
func TestCitanjeNecitljivogRetka(t *testing.T) {
	db := openTestDB(t)
	r := New(db, "laptop-baranja")
	if _, err := db.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel)
		VALUES ('0190a000-0000-7000-8000-000000000001', 'stations', 's1', 'cop-osijek', '', 0, '{}', 'nije vrijeme', 1, '')`); err != nil {
		t.Fatal(err)
	}
	if vv, err := r.History(context.Background(), "stations", "s1"); err == nil {
		t.Errorf("nečitljiv redak: %v", vv)
	}
}
