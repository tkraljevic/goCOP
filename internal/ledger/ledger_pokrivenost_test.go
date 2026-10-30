package ledger

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Kanal koji nije vrsta/područje/godina vraća se cijeli kao vrsta, bez
// područja i godine; nečitljiv broj je nula.
func TestSplitChannelNeobicni(t *testing.T) {
	slucajevi := []struct {
		kanal     string
		vrsta     string
		podr, god int
	}{
		{"", "", 0, 0},
		{"ocitanja/16/2026", "ocitanja", 16, 2026},
		{"ocitanja/16", "ocitanja/16", 0, 0},
		{"a/b/c/d", "a/b/c/d", 0, 0},
		{"dnevnici/x/2025", "dnevnici", 0, 2025},
	}
	for _, s := range slucajevi {
		v, p, g := SplitChannel(s.kanal)
		if v != s.vrsta || p != s.podr || g != s.god {
			t.Errorf("SplitChannel(%q) = %q %d %d", s.kanal, v, p, g)
		}
	}
	if k := ChannelFor(ChannelPrijave, 7, 2026); k != "prijave/7/2026" {
		t.Errorf("ChannelFor %q", k)
	}
	if v, p, g := SplitChannel(ChannelFor(ChannelVodocuvar, 3, 2024)); v != ChannelVodocuvar || p != 3 || g != 2024 {
		t.Errorf("povratno %q %d %d", v, p, g)
	}
}

func TestCvor(t *testing.T) {
	var r *Recorder
	if r.Cvor() != "" {
		t.Error("prazan zapisivač ima čvor")
	}
	if c := New(nil, "cop-vukovar").Cvor(); c != "cop-vukovar" {
		t.Errorf("čvor %q", c)
	}
}

// fiksna je primljena verzija sa zadanim autorom, kanalom i vremenom
func fiksna(id, entitet, zapis, cvor, kanal string, kad time.Time) Version {
	return Version{VersionID: id, Entity: entitet, EntityID: zapis, NodeID: cvor, Channel: kanal,
		Payload: json.RawMessage(`{"n":"` + id[len(id)-2:] + `"}`), CreatedAt: kad, SchemaVersion: 1}
}

var pocetak = time.Date(2026, 3, 10, 8, 0, 0, 0, time.UTC)

// probnaKnjiga drži verzije triju entiteta, dva autora i dva kanala, sat po sat
func probnaKnjiga(t *testing.T) (*sql.DB, *Recorder) {
	t.Helper()
	db := openTestDB(t)
	rec := New(db, "cop-osijek")
	vv := []Version{
		fiksna("01950000-0000-7000-8000-000000000001", "stations", "st-1", "cop-osijek", "", pocetak),
		fiksna("01950000-0000-7000-8000-000000000002", "stations", "st-1", "laptop-a", "", pocetak.Add(time.Hour)),
		fiksna("01950000-0000-7000-8000-000000000003", "sections", "dn-1", "cop-osijek", "", pocetak.Add(2*time.Hour)),
		fiksna("01950000-0000-7000-8000-000000000004", "readings", "r-1", "laptop-a", "ocitanja/16/2026", pocetak.Add(3*time.Hour)),
		fiksna("01950000-0000-7000-8000-000000000005", "readings", "r-2", "laptop-a", "ocitanja/16/2026", pocetak.Add(4*time.Hour)),
		fiksna("01950000-0000-7000-8000-000000000006", "readings", "r-3", "cop-osijek", "ocitanja/17/2026", pocetak.Add(5*time.Hour)),
		fiksna("01950000-0000-7000-8000-000000000007", "stations", "st-2", "cop-osijek", "", pocetak.Add(6*time.Hour)),
	}
	vv[6].Archived = true
	if n, err := rec.Apply(context.Background(), vv); err != nil || n != len(vv) {
		t.Fatalf("primljeno %d: %v", n, err)
	}
	return db, rec
}

func oznake(vv []Version) string {
	var out []string
	for _, v := range vv {
		out = append(out, v.VersionID[len(v.VersionID)-1:])
	}
	return strings.Join(out, ",")
}

// LatestOf daje zadnju verziju svakog zapisa, redom nastanka; prazan popis
// znači sve entitete.
func TestLatestOf(t *testing.T) {
	_, rec := probnaKnjiga(t)
	ctx := context.Background()
	sve, err := rec.LatestOf(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := oznake(sve); got != "2,3,4,5,6,7" {
		t.Errorf("sve %s", got)
	}
	postaje, err := rec.LatestOf(ctx, []string{"stations"})
	if err != nil {
		t.Fatal(err)
	}
	if got := oznake(postaje); got != "2,7" || !postaje[1].Archived || postaje[0].NodeID != "laptop-a" {
		t.Errorf("postaje %s %+v", got, postaje)
	}
	dva, _ := rec.LatestOf(ctx, []string{"sections", "readings", "nema"}) // probni podaci su ispravni; kvar bi pokazala provjera niže
	if got := oznake(dva); got != "3,4,5,6" {
		t.Errorf("dionice i očitanja %s", got)
	}
}

// SinceByNode daje samo jednog autora u jednom kanalu; bez granice broja
// vraća sve (zadano tisuću).
func TestSinceByNode(t *testing.T) {
	_, rec := probnaKnjiga(t)
	ctx := context.Background()
	vv, err := rec.SinceByNode(ctx, "laptop-a", "ocitanja/16/2026", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := oznake(vv); got != "4,5" {
		t.Errorf("laptop-a u kanalu %s", got)
	}
	vv, _ = rec.SinceByNode(ctx, "laptop-a", "ocitanja/16/2026", "01950000-0000-7000-8000-000000000004", 0) // probni podaci su ispravni; kvar bi pokazala provjera niže
	if got := oznake(vv); got != "5" {
		t.Errorf("nakon 4: %s", got)
	}
	vv, _ = rec.SinceByNode(ctx, "cop-osijek", "", "", 1) // probni podaci su ispravni; kvar bi pokazala provjera niže
	if got := oznake(vv); got != "1" {
		t.Errorf("jedna zajednička: %s", got)
	}
	if vv[0].CreatedAt.UTC() != pocetak || string(vv[0].Payload) != `{"n":"01"}` {
		t.Errorf("verzija nije pročitana kako je primljena: %+v", vv[0])
	}
}

// InChannels daje verzije kanala redom kojim su kanali zadani.
func TestInChannels(t *testing.T) {
	_, rec := probnaKnjiga(t)
	vv, err := rec.InChannels(context.Background(), []string{"ocitanja/17/2026", "ocitanja/16/2026", "nema/1/1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := oznake(vv); got != "6,4,5" {
		t.Errorf("kanali %s", got)
	}
	vv, _ = rec.InChannels(context.Background(), []string{""}) // probni podaci su ispravni; kvar bi pokazala provjera niže
	if got := oznake(vv); got != "1,2,3,7" {
		t.Errorf("zajednički %s", got)
	}
	if vv, err := rec.InChannels(context.Background(), nil); err != nil || vv != nil {
		t.Errorf("bez kanala %v", vv)
	}
}

// Recent: najnovija prva, listanje unatrag od zadane verzije, razdoblje po
// zidnom satu [od, do) i granica broja.
func TestRecent(t *testing.T) {
	_, rec := probnaKnjiga(t)
	ctx := context.Background()
	if vv, err := rec.Recent(ctx, nil, "", time.Time{}, time.Time{}, 0); vv != nil || err != nil {
		t.Fatalf("bez entiteta %v %v", vv, err)
	}
	slucajevi := []struct {
		ime      string
		entiteti []string
		prije    string
		od, do   time.Time
		limit    int
		zelim    string
	}{
		{"sve postaje", []string{"stations"}, "", time.Time{}, time.Time{}, 0, "7,2,1"},
		{"dva entiteta s granicom", []string{"stations", "readings"}, "", time.Time{}, time.Time{}, 3, "7,6,5"},
		{"unatrag", []string{"stations", "readings"}, "01950000-0000-7000-8000-000000000005", time.Time{}, time.Time{}, 0, "4,2,1"},
		{"od", []string{"readings"}, "", pocetak.Add(4 * time.Hour), time.Time{}, 0, "6,5"},
		{"do, bez kraja", []string{"readings"}, "", time.Time{}, pocetak.Add(4 * time.Hour), 0, "4"},
		{"razdoblje u drugoj zoni", []string{"stations", "sections", "readings"}, "",
			pocetak.Add(time.Hour).In(time.FixedZone("CET", 3600)), pocetak.Add(3 * time.Hour).In(time.FixedZone("CET", 3600)), 0, "3,2"},
	}
	for _, s := range slucajevi {
		vv, err := rec.Recent(ctx, s.entiteti, s.prije, s.od, s.do, s.limit)
		if err != nil {
			t.Fatalf("%s: %v", s.ime, err)
		}
		if got := oznake(vv); got != s.zelim {
			t.Errorf("%s: %s, želim %s", s.ime, got, s.zelim)
		}
	}
}

// Stats: spomenik broji samo zapis kojem je zadnja verzija brisanje, a
// najstarija verzija je po zidnom satu.
func TestStatsSaSpomenikom(t *testing.T) {
	_, rec := probnaKnjiga(t)
	// st-3 je obrisan pa vraćen: ranija verzija je brisanje, zadnja je živa,
	// pa se ne broji kao spomenik
	obrisan := fiksna("01950000-0000-7000-8000-000000000008", "stations", "st-3", "cop-osijek", "", pocetak.Add(7*time.Hour))
	obrisan.Archived = true
	vracen := fiksna("01950000-0000-7000-8000-000000000009", "stations", "st-3", "cop-osijek", "", pocetak.Add(8*time.Hour))
	if n, err := rec.Apply(context.Background(), []Version{obrisan, vracen}); err != nil || n != 2 {
		t.Fatalf("primljeno %d: %v", n, err)
	}
	st, err := rec.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Versions != 9 || st.Records != 7 || st.Tombstones != 1 {
		t.Errorf("brojke %+v", st)
	}
	if st.Oldest == nil || !st.Oldest.Equal(pocetak) {
		t.Errorf("najstarija %v", st.Oldest)
	}
	if st.ByEntity["readings"] != 3 || st.ByEntity["stations"] != 5 || st.ByEntity["sections"] != 1 {
		t.Errorf("po entitetu %v", st.ByEntity)
	}
}

// Prazna knjiga nema najstarije verzije.
func TestStatsPraznaKnjiga(t *testing.T) {
	st, err := New(openTestDB(t), "cop").Stats(context.Background())
	if err != nil || st.Oldest != nil || st.Versions != 0 || len(st.ByEntity) != 0 {
		t.Fatalf("%+v %v", st, err)
	}
}

// Zatvorena baza: svaka radnja javlja grešku, nijedna ne vraća prazno kao
// da je knjiga prazna.
func TestZatvorenaKnjiga(t *testing.T) {
	db, rec := probnaKnjiga(t)
	db.Close()
	ctx := context.Background()
	provjere := map[string]error{}
	_, provjere["Count"] = rec.Count(ctx)
	_, provjere["Frontier"] = rec.Frontier(ctx)
	_, provjere["CountByChannel"] = rec.CountByChannel(ctx)
	_, provjere["Stats"] = rec.Stats(ctx)
	_, provjere["PurgeArchived"] = rec.PurgeArchived(ctx)
	_, provjere["Prorijedi"] = rec.Prorijedi(ctx, "stations", pocetak)
	_, provjere["ProrijediZamijenjene"] = rec.ProrijediZamijenjene(ctx, "stations")
	_, provjere["Latest"] = rec.Latest(ctx, "stations", "st-1")
	_, provjere["LatestOf"] = rec.LatestOf(ctx, nil)
	_, provjere["Delta"] = rec.Delta(ctx, nil, nil, 0)
	_, provjere["InChannels"] = rec.InChannels(ctx, []string{""})
	_, provjere["Recent"] = rec.Recent(ctx, []string{"stations"}, "", time.Time{}, time.Time{}, 0)
	_, provjere["Compact"] = rec.Compact(ctx, pocetak)
	_, provjere["Apply"] = rec.Apply(ctx, []Version{primljena("01950000-0000-7000-8000-0000000000ff", "stations", "st-9")})
	provjere["UcitajNovosti"] = rec.UcitajNovosti(ctx)
	provjere["DeleteVersion"] = rec.DeleteVersion(ctx, "01950000-0000-7000-8000-000000000001")
	_, provjere["Record"] = rec.Record(ctx, db, "stations", "st-1", probni{"Osijek", 1})
	for ime, err := range provjere {
		if err == nil {
			t.Errorf("%s nad zatvorenom bazom bez greške", ime)
		}
	}
}

// zabraniUpis postavi okidač koji odbije svaki novi redak knjige
func zabraniUpis(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`CREATE TRIGGER zabrana BEFORE INSERT ON record_versions
		BEGIN SELECT RAISE(ABORT, 'upis zabranjen'); END`); err != nil {
		t.Fatal(err)
	}
}

// Kad baza odbije upis, lokalna verzija javlja zapis, a primljeni paket ne
// upiše ništa i ne bilježi novosti.
func TestOdbijenUpis(t *testing.T) {
	db, rec := probnaKnjiga(t)
	zabraniUpis(t, db)
	ctx := context.Background()
	if _, err := rec.Record(ctx, db, "stations", "st-1", probni{"Osijek", 1}); err == nil ||
		!strings.Contains(err.Error(), "upisu verzije stations/st-1") {
		t.Errorf("lokalni upis: %v", err)
	}
	v := primljena("01950000-0000-7000-8000-0000000000aa", "nepoznat_entitet_x", "z-1")
	n, err := rec.Apply(ctx, []Version{v})
	if err == nil || n != 0 || !strings.Contains(err.Error(), "primanju verzije "+v.VersionID) {
		t.Errorf("primanje: %d %v", n, err)
	}
	if len(rec.Novosti()) != 0 {
		t.Errorf("novosti nakon odbijenog paketa: %v", rec.Novosti())
	}
	if brojVerzija(t, db) != 7 {
		t.Error("knjiga se promijenila")
	}
}

// Sadržaj koji se ne da zapisati u JSON ne ostavlja verziju.
func TestSadrzajKojiNijeJSON(t *testing.T) {
	db := openTestDB(t)
	rec := New(db, "cop")
	_, err := rec.Record(context.Background(), db, "stations", "st-1", map[string]any{"kanal": make(chan int)})
	if err == nil || !strings.Contains(err.Error(), "ne može zapisati") {
		t.Fatalf("greška %v", err)
	}
	if brojVerzija(t, db) != 0 {
		t.Error("ostala je verzija")
	}
}

func TestNovostString(t *testing.T) {
	if s := (Novost{Entity: "akti", Nepoznat: true, Verzija: 4}).String(); s != "akti: entitet koji ovaj program ne poznaje (4 verzija)" {
		t.Errorf("nepoznat: %q", s)
	}
	if s := (Novost{Entity: "stations", Shema: 3, Lokalna: 2, Verzija: 1}).String(); s != "stations: zapisi sheme 3, ovaj program zna 2 (1 verzija)" {
		t.Errorf("novija shema: %q", s)
	}
}

type ugradjeniDio struct {
	Ugradjeno string `json:"ugradjeno"`
}

type ImenovaniDio struct {
	X int `json:"x"`
}

// zapisSDijelovima ima ugrađenu strukturu preko pokazivača, ugrađenu
// strukturu s imenom, polje koje se ne zapisuje i neizvezeno polje
type zapisSDijelovima struct {
	*ugradjeniDio
	ImenovaniDio `json:"meta"`
	Naziv        string
	Skriveno     string `json:"-"`
	tajno        string
}

// samoSebe ugrađuje sebe, pa skupljanje polja ne smije ući u petlju
type samoSebe struct {
	*samoSebe
	Ime string `json:"ime"`
}

// Polja prethodne verzije koja program ne zapisuje (oznaka "-", neizvezena)
// ostaju; poznata polja, i ona iz ugrađene strukture, ne oživljavaju.
func TestSpojiNepoznataUgradjeneStrukture(t *testing.T) {
	db := openTestDB(t)
	rec := New(db, "stari")
	ctx := context.Background()
	novijaVerzija(t, rec, "polja_ugr", "z-1",
		`{"Naziv":"A","ugradjeno":"u","meta":{"x":1},"x":5,"Skriveno":"s","tajno":"t","novo":"n"}`, 1)
	if _, err := rec.Record(ctx, db, "polja_ugr", "z-1", zapisSDijelovima{Naziv: "B", tajno: "lokalno"}); err != nil {
		t.Fatal(err)
	}
	m := payloadMapa(t, rec, "polja_ugr", "z-1")
	if m["Naziv"] != "B" || m["Skriveno"] != "s" || m["tajno"] != "t" || m["novo"] != "n" || m["x"] != float64(5) {
		t.Errorf("nepoznata polja: %v", m)
	}
	if _, ima := m["ugradjeno"]; ima {
		t.Errorf("polje ugrađene strukture je oživjelo: %v", m)
	}
	if meta, _ := m["meta"].(map[string]any); meta["x"] != float64(0) {
		t.Errorf("imenovani dio nije iz nove verzije: %v", m)
	}

	novijaVerzija(t, rec, "polja_sebe", "z-1", `{"ime":"a","drugo":2}`, 1)
	if _, err := rec.Record(ctx, db, "polja_sebe", "z-1", &samoSebe{Ime: "b"}); err != nil {
		t.Fatal(err)
	}
	if m := payloadMapa(t, rec, "polja_sebe", "z-1"); m["ime"] != "b" || m["drugo"] != float64(2) {
		t.Errorf("ugrađen sam u sebe: %v", m)
	}
}

// Prethodna verzija koja nije objekt ne dopunjuje ništa, a ni nova koja
// nije objekt ni struktura.
func TestSpojiNepoznataBezObjekta(t *testing.T) {
	db := openTestDB(t)
	rec := New(db, "stari")
	ctx := context.Background()
	novijaVerzija(t, rec, "polja_niz", "z-1", `[1,2]`, 1)
	if _, err := rec.Record(ctx, db, "polja_niz", "z-1", probni{Naziv: "A"}); err != nil {
		t.Fatal(err)
	}
	if m := payloadMapa(t, rec, "polja_niz", "z-1"); len(m) != 2 || m["naziv"] != "A" {
		t.Errorf("iz niza: %v", m)
	}
	if _, err := rec.Record(ctx, db, "polja_niz", "z-1", nil); err != nil {
		t.Fatal(err)
	}
	if v, err := rec.Latest(ctx, "polja_niz", "z-1"); err != nil || string(v.Payload) != "null" {
		t.Errorf("prazan sadržaj %s", v.Payload)
	}
	// struktura koja se zapisuje kao null (nil pokazivač) ne dobiva polja
	if _, err := rec.Record(ctx, db, "polja_niz", "z-1", probni{Naziv: "C"}); err != nil {
		t.Fatal(err)
	}
	var prazan *probni
	if _, err := rec.Record(ctx, db, "polja_niz", "z-1", prazan); err != nil {
		t.Fatal(err)
	}
	if v, err := rec.Latest(ctx, "polja_niz", "z-1"); err != nil || string(v.Payload) != "null" {
		t.Errorf("nil pokazivač %s", v.Payload)
	}
}

// Brisanje bez zadane prethodnice ili iza verzije bez vremena ne upisuje se.
func TestArchiveNakonGreske(t *testing.T) {
	db := openTestDB(t)
	rec := New(db, "cop")
	ctx := context.Background()
	if _, err := rec.ArchiveNakon(ctx, db, "", "stations", "st-1", nil, ""); err == nil {
		t.Error("bez prethodnice")
	}
	if _, err := rec.ArchiveNakon(ctx, db, "", "stations", "st-1", nil, "6ba7b810-9dad-11d1-80b4-00c04fd430c8"); err == nil ||
		!strings.Contains(err.Error(), "ne nosi vrijeme nastanka") {
		t.Errorf("UUIDv1: %v", err)
	}
	if _, err := rec.ArchiveNakon(ctx, db, "", "stations", "st-1", nil, "nije-uuid"); err == nil {
		t.Error("neispravna oznaka")
	}
	if _, err := rec.Record(ctx, db, " ", "st-1", nil); err == nil {
		t.Error("bez entiteta")
	}
	if brojVerzija(t, db) != 0 {
		t.Error("upisano uz grešku")
	}
}
