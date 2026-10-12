package repository

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Razmjena upiše primljene verzije u knjigu, a površinu osvježi u drugoj
// transakciji. Kad ta druga ne prođe (SQLITE_BUSY dok dva čvora šalju, čvor
// ugašen prije ponovnog pokušaja), verzije ostanu samo u knjizi. Ovi testovi
// to slažu ručno: verzije idu u knjigu primatelja bez ApplyVersions.

// samoUKnjigu daje verzije čvoru kao razmjena kojoj primjena na površinu nije
// prošla
func samoUKnjigu(t *testing.T, c *cvor, verzije []ledger.Version) {
	t.Helper()
	if _, err := c.rec.Apply(ctxRaz, verzije); err != nil {
		t.Fatalf("knjiga: %v", err)
	}
}

// sPolitikomPovijesti postavlja razdoblje očitanja koje čvor drži i vraća
// prijašnje poslije testa
func sPolitikomPovijesti(t *testing.T, p ReadingHistoryPolicy) {
	t.Helper()
	prije := readingPolicy.Load()
	readingPolicy.Store(&p)
	t.Cleanup(func() { readingPolicy.Store(prije) })
}

func zadnjaVerzijaCvora(t *testing.T, c *cvor) string {
	t.Helper()
	var id string
	nuzno(t, c.db.QueryRow(`SELECT COALESCE(MAX(version_id), '') FROM record_versions`).Scan(&id))
	return id
}

// ocitanjeNa upisuje očitanje letve iz oslonca kroz repozitorij, s verzijom
func ocitanjeNa(t *testing.T, c *cvor, kad time.Time, cm int) uuid.UUID {
	t.Helper()
	rd := &models.Reading{StationID: osnovaLetva.String(), MeasuredAt: kad, LevelCm: &cm, Source: models.ReadingSourceManual}
	nuzno(t, NewReadingRepository(c.db, c.rec).Create(ctxRaz, rd))
	return rd.ID
}

// povrsinaOcitanja je popis očitanja na površini: id, vodostaj i kanal
func povrsinaOcitanja(t *testing.T, c *cvor) []string {
	t.Helper()
	rows, err := c.db.Query(`SELECT id || '/' || COALESCE(level_cm, '') || '/' || channel FROM readings ORDER BY id`)
	nuzno(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		nuzno(t, rows.Scan(&s))
		out = append(out, s)
	}
	nuzno(t, rows.Err())
	return out
}

func imaRedak(t *testing.T, c *cvor, tablica, id string) bool {
	t.Helper()
	var n int
	nuzno(t, c.db.QueryRow(`SELECT count(*) FROM `+tablica+` WHERE id = ?`, id).Scan(&n))
	return n > 0
}

func praznaObnova(o Obnova) bool {
	return len(o.Upisano) == 0 && len(o.Maknuto) == 0 && o.String() == ""
}

// Očitanja koja su stigla, a nisu upisana, obnova upiše; brisanje koje je
// stiglo samo u knjigu makne redak. Površina primatelja tada je jednaka
// površini izvora, a drugi poziv nema što popraviti.
func TestObnovaVracaOcitanjaKojaSuOstalaSamoUKnjizi(t *testing.T) {
	sNedovrsenima(t)
	sPolitikomPovijesti(t, ReadingHistoryPolicy{})
	paket := paketObnove
	paketObnove = 2 // pet zapisa ide u tri paketa
	t.Cleanup(func() { paketObnove = paket })

	a, b := noviCvor(t, "cop-proba-a"), noviCvor(t, "cop-proba-b")
	kad := time.Now().UTC().Truncate(time.Hour).Add(-48 * time.Hour)
	ocitanjeNa(t, a, kad, 120)
	obrisano := ocitanjeNa(t, a, kad.Add(time.Hour), 121)
	posalji(t, a, b)
	granica := zadnjaVerzijaCvora(t, a)
	for i := range 4 {
		ocitanjeNa(t, a, kad.Add(time.Duration(2+i)*time.Hour), 130+i)
	}
	nuzno(t, NewReadingRepository(a.db, a.rec).Delete(ctxRaz, obrisano))
	nove, err := a.rec.Since(ctxRaz, granica, 0)
	nuzno(t, err)
	samoUKnjigu(t, b, nove)
	if n := len(povrsinaOcitanja(t, b)); n != 2 {
		t.Fatalf("prije obnove primatelj ima %d očitanja, a treba 2", n)
	}

	o, err := ObnoviPovrsinu(ctxRaz, b.db, b.rec)
	nuzno(t, err)
	if o.Upisano[EntityReadings] != 4 || o.Maknuto[EntityReadings] != 1 {
		t.Errorf("upisano %d i maknuto %d očitanja, a treba 4 i 1", o.Upisano[EntityReadings], o.Maknuto[EntityReadings])
	}
	if got, want := o.String(), "upisano što je bilo samo u knjizi: očitanja 4; maknuto što je u knjizi obrisano: očitanja 1"; got != want {
		t.Errorf("opis obnove je %q, a treba %q", got, want)
	}
	if got, want := strings.Join(povrsinaOcitanja(t, b), "\n"), strings.Join(povrsinaOcitanja(t, a), "\n"); got != want {
		t.Errorf("površina primatelja nije jednaka izvoru:\n%s\n---\n%s", got, want)
	}

	o, err = ObnoviPovrsinu(ctxRaz, b.db, b.rec)
	nuzno(t, err)
	if !praznaObnova(o) {
		t.Errorf("drugi poziv je opet nešto popravio: %+v", o)
	}
}

// Očitanje starije od razdoblja koje čvor drži ne ide na površinu ni obnovom,
// kao što ga ni razmjena ne bi primila; ne broji se ni kao popravak.
func TestObnovaPostujeRazdobljeOcitanja(t *testing.T) {
	sNedovrsenima(t)
	a, b := noviCvor(t, "cop-proba-a"), noviCvor(t, "cop-proba-b")
	staro := ocitanjeNa(t, a, time.Now().UTC().AddDate(-2, 0, 0), 300)
	novo := ocitanjeNa(t, a, time.Now().UTC().Add(-time.Hour), 310)
	sve, err := a.rec.Since(ctxRaz, "", 0)
	nuzno(t, err)
	samoUKnjigu(t, b, sve)
	sPolitikomPovijesti(t, ReadingHistoryPolicy{Months: 12})

	for krug := range 2 {
		o, err := ObnoviPovrsinu(ctxRaz, b.db, b.rec)
		nuzno(t, err)
		if want := 1 - krug; o.Upisano[EntityReadings] != want {
			t.Errorf("krug %d: upisano %d očitanja, a treba %d", krug+1, o.Upisano[EntityReadings], want)
		}
	}
	if imaRedak(t, b, "readings", staro.String()) {
		t.Error("obnova je upisala očitanje izvan razdoblja koje čvor drži")
	}
	if !imaRedak(t, b, "readings", novo.String()) {
		t.Error("obnova nije upisala novo očitanje")
	}
}

// Upis obrisanog dnevnika legitimno ostaje bez retka, a upis dnevnika koji
// još nije stigao ne prođe strani ključ. Ni jedan se ne broji kao popravak,
// ni u drugom pozivu, i obnova zbog njih ne staje.
func TestObnovaNeBrojiUpisKojiOstajeBezRetka(t *testing.T) {
	sNedovrsenima(t)
	b := noviCvor(t, "cop-proba-b")
	dan := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	upis := func(id, dnevnik string) models.JournalEntry {
		return models.JournalEntry{ID: id, JournalID: dnevnik, Date: dan, Kind: models.EntryKindNote,
			Text: "probni upis", CreatedAt: dan, UpdatedAt: dan}
	}
	const obrisani, nestigli = "01a00000-0000-7000-8000-0000000d0001", "01a00000-0000-7000-8000-0000000d0002"
	const uObrisanom, uNestiglom, uPostojecem = "01a00000-0000-7000-8000-0000000e0001", "01a00000-0000-7000-8000-0000000e0002", "01a00000-0000-7000-8000-0000000e0003"
	dnevnik := verzijaZa(t, EntityJournals, obrisani, vid(1), models.Journal{ID: obrisani, AreaID: 16, Kind: "COP"})
	brisanje := dnevnik
	brisanje.VersionID, brisanje.Archived = vid(2), true
	samoUKnjigu(t, b, []ledger.Version{dnevnik, brisanje,
		verzijaZa(t, EntityJournalEntries, uObrisanom, vid(3), upis(uObrisanom, obrisani)),
		verzijaZa(t, EntityJournalEntries, uNestiglom, vid(4), upis(uNestiglom, nestigli)),
		verzijaZa(t, EntityJournalEntries, uPostojecem, vid(5), upis(uPostojecem, osnovaDnevnik)),
	})

	for krug := range 2 {
		o, err := ObnoviPovrsinu(ctxRaz, b.db, b.rec)
		if err != nil {
			t.Fatalf("krug %d: obnova je stala zbog upisa koji ne prolazi: %v", krug+1, err)
		}
		if want := 1 - krug; o.Upisano[EntityJournalEntries] != want || len(o.Upisano) > want {
			t.Errorf("krug %d: upisano %v, a treba %d upis dnevnika", krug+1, o.Upisano, want)
		}
	}
	if imaRedak(t, b, "journal_entries", uObrisanom) || imaRedak(t, b, "journal_entries", uNestiglom) {
		t.Error("na površini je upis obrisanog ili nepostojećeg dnevnika")
	}
	if !imaRedak(t, b, "journal_entries", uPostojecem) {
		t.Error("upis postojećeg dnevnika nije upisan")
	}
	if !nedovrsen(EntityJournalEntries + "|" + uNestiglom) {
		t.Error("upis dnevnika koji nije stigao ne čeka među nedovršenima")
	}
}

// pokvariCommit dodaje okidač koji ruši potvrdu transakcije koja upiše
// očitanje: odgođeni strani ključ provjerava se tek pri COMMIT-u
func pokvariCommit(t *testing.T, c *cvor) {
	t.Helper()
	for _, q := range []string{
		`CREATE TABLE kvar_roditelj (id INTEGER PRIMARY KEY)`,
		`CREATE TABLE kvar_dijete (roditelj INTEGER REFERENCES kvar_roditelj(id) DEFERRABLE INITIALLY DEFERRED)`,
		`CREATE TRIGGER kvar_commita AFTER INSERT ON readings BEGIN INSERT INTO kvar_dijete VALUES (1); END`,
	} {
		if _, err := c.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

// Paket koji padne pri potvrdi ostaje za ponovni pokušaj: verzije su već u
// knjizi, pa ih razmjena više ne bi poslala. Iduća primjena ih upiše.
func TestPaketKojiPadnePriCommituOstajeZaPonovniPokusaj(t *testing.T) {
	sNedovrsenima(t)
	sPolitikomPovijesti(t, ReadingHistoryPolicy{})
	a, b := noviCvor(t, "cop-proba-a"), noviCvor(t, "cop-proba-b")
	kad := time.Now().UTC().Add(-3 * time.Hour)
	prvo := ocitanjeNa(t, a, kad, 200)
	drugo := ocitanjeNa(t, a, kad.Add(time.Hour), 201)
	paket, err := a.rec.Since(ctxRaz, "", 0)
	nuzno(t, err)
	pokvariCommit(t, b)

	if err := primiPaket(t, b, paket...); err == nil {
		t.Fatal("potvrda je prošla iako je okidač treba srušiti")
	}
	for _, id := range []uuid.UUID{prvo, drugo} {
		if !nedovrsen(EntityReadings + "|" + id.String()) {
			t.Errorf("očitanje %s iz paketa koji nije potvrđen ne čeka ponovni pokušaj", id)
		}
		if imaRedak(t, b, "readings", id.String()) {
			t.Errorf("očitanje %s je na površini iako potvrda nije prošla", id)
		}
	}

	_, err = b.db.Exec(`DROP TRIGGER kvar_commita`)
	nuzno(t, err)
	treca := ocitanjeNa(t, a, kad.Add(2*time.Hour), 202)
	iduci, err := a.rec.Since(ctxRaz, paket[len(paket)-1].VersionID, 0)
	nuzno(t, err)
	nuzno(t, primiPaket(t, b, iduci...))
	for _, id := range []uuid.UUID{prvo, drugo, treca} {
		if !imaRedak(t, b, "readings", id.String()) {
			t.Errorf("očitanje %s nije upisano idućom razmjenom", id)
		}
		if nedovrsen(EntityReadings + "|" + id.String()) {
			t.Errorf("očitanje %s je i dalje među nedovršenima", id)
		}
	}
}

// Obnova koja ne može potvrditi paket javlja grešku i ne broji ga; kad se
// baza oporavi, iduća obnova upiše što je ostalo.
func TestObnovaJavljaPaketKojiNijePotvrden(t *testing.T) {
	sNedovrsenima(t)
	sPolitikomPovijesti(t, ReadingHistoryPolicy{})
	a, b := noviCvor(t, "cop-proba-a"), noviCvor(t, "cop-proba-b")
	id := ocitanjeNa(t, a, time.Now().UTC().Add(-time.Hour), 150)
	sve, err := a.rec.Since(ctxRaz, "", 0)
	nuzno(t, err)
	samoUKnjigu(t, b, sve)
	pokvariCommit(t, b)

	o, err := ObnoviPovrsinu(ctxRaz, b.db, b.rec)
	if err == nil || !strings.Contains(err.Error(), "očitanja") {
		t.Fatalf("obnova s paketom koji nije potvrđen javlja %v", err)
	}
	if !praznaObnova(o) {
		t.Errorf("nepotvrđen paket je izbrojen kao popravak: %+v", o)
	}
	_, err = b.db.Exec(`DROP TRIGGER kvar_commita`)
	nuzno(t, err)
	o, err = ObnoviPovrsinu(ctxRaz, b.db, b.rec)
	nuzno(t, err)
	if o.Upisano[EntityReadings] != 1 || !imaRedak(t, b, "readings", id.String()) {
		t.Errorf("poslije oporavka obnova nije upisala očitanje: %+v", o)
	}
}

// Dok jedna obnova traje, druga ne kreće; greška čitanja (ovdje tablice koje
// nema) javlja se s nazivom onoga što se obnavljalo.
func TestObnovaJednaPoJednaIGreskaCitanja(t *testing.T) {
	sNedovrsenima(t)
	sPolitikomPovijesti(t, ReadingHistoryPolicy{})
	a, b := noviCvor(t, "cop-proba-a"), noviCvor(t, "cop-proba-b")
	ocitanjeNa(t, a, time.Now().UTC().Add(-time.Hour), 150)
	sve, err := a.rec.Since(ctxRaz, "", 0)
	nuzno(t, err)
	samoUKnjigu(t, b, sve)

	obnovaTece.Lock()
	o, err := ObnoviPovrsinu(ctxRaz, b.db, b.rec)
	obnovaTece.Unlock()
	if err != nil || !praznaObnova(o) {
		t.Errorf("obnova je krenula dok je druga trajala: %+v, %v", o, err)
	}

	_, err = b.db.Exec(`DROP TABLE opozivi`)
	nuzno(t, err)
	o, err = ObnoviPovrsinu(ctxRaz, b.db, b.rec)
	if err == nil || !strings.Contains(err.Error(), "opozivi") {
		t.Errorf("greška čitanja ne kaže što se obnavljalo: %v", err)
	}
	if o.Upisano[EntityReadings] != 1 {
		t.Errorf("očitanja obnovljena prije greške nisu izbrojena: %+v", o)
	}
}

// bezObnove su entiteti koje ne obnavlja ni ReplaySurface ni ObnoviPovrsinu,
// s razlogom. Novi entitet mora ući u jedan od ta dva popisa ili ovamo.
var bezObnove = map[string]string{
	EntitySectionStations:    "veza dionice i letve nema svoj redak, dolazi s dionicom",
	EntitySectionTerritories: "veza dionice i područja nema svoj redak, dolazi s dionicom",
	EntitySectionStructures:  "veza dionice i objekta nema svoj redak, dolazi s dionicom",
	"peers":                  "drži ih sloj razmjene, ključ je node_id",
	"memberships":            "drži ih sloj razmjene, ključ je node_id",
	"ovlasti":                "drži ih sloj razmjene, ključ je node_id",
}

func TestSvakiEntitetImaObnovuIliRazlog(t *testing.T) {
	pri := map[string]bool{}
	for _, e := range SurfaceEntities {
		pri[e] = true
	}
	c := noviCvor(t, "cop-proba")
	obnova := map[string]bool{}
	for _, e := range obnovaPovrsine {
		obnova[e.entitet] = true
		if pri[e.entitet] {
			t.Errorf("%s se obnavlja i pri pokretanju i obnovom površine", e.entitet)
		}
		if _, err := c.db.Exec(`SELECT id FROM ` + e.entitet + ` LIMIT 0`); err != nil {
			t.Errorf("površina entiteta %s nije tablica s ključem id: %v", e.entitet, err)
		}
	}
	var bez []string
	for _, e := range PoznatiEntiteti() {
		if !pri[e] && !obnova[e] && bezObnove[e] == "" {
			bez = append(bez, e)
		}
	}
	sort.Strings(bez)
	if len(bez) > 0 {
		t.Errorf("entiteti bez obnove površine i bez razloga u bezObnove: %v", bez)
	}
}

// Kad dio paketa ne prođe, greška kaže koliko verzija čeka i koja je prva
func TestGreskaDjelomicnePrimjeneKazeStoCeka(t *testing.T) {
	sNedovrsenima(t)
	b := noviCvor(t, "cop-proba-b")
	const upis, dnevnik = "01a00000-0000-7000-8000-0000000e0009", "01a00000-0000-7000-8000-0000000d0009"
	dan := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	err := primiPaket(t, b, verzijaZa(t, EntityJournalEntries, upis, vid(1), models.JournalEntry{
		ID: upis, JournalID: dnevnik, Date: dan, Kind: models.EntryKindNote, CreatedAt: dan, UpdatedAt: dan}))
	if err == nil {
		t.Fatal("upis dnevnika koji nije stigao prošao je strani ključ")
	}
	if want := "1 verzija nije primijenjeno na površinu"; !strings.HasPrefix(err.Error(), want) ||
		!strings.Contains(err.Error(), EntityJournalEntries+"/"+upis) {
		t.Errorf("greška %q ne kaže koliko verzija i koja čeka", err)
	}
}
