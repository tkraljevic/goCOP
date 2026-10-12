package repository

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Razmjena šalje verzije po autoru i kanalu, u paketima od po 5000, pa novi
// čvor ne prima verzije redom nastanka: novija verzija zapisa zna stići prije
// starije, a zapis prije onoga o koji se oslanja. Ovi testovi slažu verzije
// ručno, s poretkom koji odredi test.

// vid je identitet verzije s poretkom n
func vid(n int) string { return fmt.Sprintf("01a00000-0000-7000-8000-%012d", n) }

// verzijaZa slaže verziju kakvu donosi razmjena
func verzijaZa(t *testing.T, entitet, id, versionID string, sadrzaj any) ledger.Version {
	t.Helper()
	b, err := json.Marshal(sadrzaj)
	nuzno(t, err)
	return ledger.Version{VersionID: versionID, Entity: entitet, EntityID: id, NodeID: "ured",
		Payload: b, SchemaVersion: ledger.SchemaVersion}
}

// primiPaket radi što i razmjena s jednim paketom: verzije u knjigu, pa na
// površinu; vraća grešku primjene na površinu
func primiPaket(t *testing.T, c *cvor, verzije ...ledger.Version) error {
	t.Helper()
	if _, err := c.rec.Apply(ctxRaz, verzije); err != nil {
		t.Fatalf("knjiga: %v", err)
	}
	return ApplyVersions(ctxRaz, c.db, c.rec, verzije)
}

// sNedovrsenima izdvaja popis nedovršenih za trajanje testa i vraća ga poslije
func sNedovrsenima(t *testing.T) {
	t.Helper()
	nedovrseni.Lock()
	prije := nedovrseni.kljucevi
	nedovrseni.kljucevi = map[string]bool{}
	nedovrseni.Unlock()
	t.Cleanup(func() {
		nedovrseni.Lock()
		nedovrseni.kljucevi = prije
		nedovrseni.Unlock()
	})
}

func nedovrsen(kljuc string) bool {
	nedovrseni.Lock()
	defer nedovrseni.Unlock()
	return nedovrseni.kljucevi[kljuc]
}

func opisDionice(t *testing.T, c *cvor, kod string) string {
	t.Helper()
	var opis string
	if err := c.db.QueryRow(`SELECT description FROM sections WHERE code = ?`, kod).Scan(&opis); err != nil {
		return ""
	}
	return opis
}

// Novija verzija dionice stigne prva i ne prođe, jer područje još nije
// stiglo. Sljedeći paket donese stariju verziju iste dionice i područje. Na
// površinu mora doći novija verzija; starija je samo povijest. Prije je
// starija bila preskočena kao zastarjela, dionica maknuta s popisa
// nedovršenih i nije se pojavila nikad, bez ikakve greške.
func TestNedovrsenaDionicaDobivaNajnovijuVerziju(t *testing.T) {
	sNedovrsenima(t)
	c := noviCvor(t, "novi-ured")
	starija := verzijaZa(t, EntitySections, "B.34.2", vid(1), models.Section{Code: "B.34.2", AreaID: 34, SectorID: "B", Description: "stari opis"})
	novija := verzijaZa(t, EntitySections, "B.34.2", vid(2), models.Section{Code: "B.34.2", AreaID: 34, SectorID: "B", Description: "novi opis"})
	podrucje := verzijaZa(t, EntityAreas, "34", vid(3), models.Area{ID: 34, SectorID: "B", Name: "Područje 34", VgiName: "VGI"})

	if err := primiPaket(t, c, novija); err == nil {
		t.Fatal("dionica bez područja prošla je strani ključ")
	}
	if !nedovrsen("sections|B.34.2") {
		t.Fatal("dionica koja nije prošla nije među nedovršenima")
	}
	if err := primiPaket(t, c, starija, podrucje); err != nil {
		t.Fatalf("drugi paket: %v", err)
	}
	if got := opisDionice(t, c, "B.34.2"); got != "novi opis" {
		t.Errorf("na površini je %q, a treba biti najnovija verzija (novi opis)", got)
	}
	if nedovrsen("sections|B.34.2") {
		t.Error("dionica je i dalje među nedovršenima")
	}
}

// Starija verzija koja stigne nakon što je novija primijenjena ne vraća
// površinu unatrag i ne javlja grešku.
func TestStarijaVerzijaNeVracaPovrsinu(t *testing.T) {
	sNedovrsenima(t)
	c := noviCvor(t, "novi-ured")
	starija := verzijaZa(t, EntitySections, "B.16.9", vid(1), models.Section{Code: "B.16.9", AreaID: 16, SectorID: "B", Description: "stari opis"})
	novija := verzijaZa(t, EntitySections, "B.16.9", vid(2), models.Section{Code: "B.16.9", AreaID: 16, SectorID: "B", Description: "novi opis"})
	nuzno(t, primiPaket(t, c, novija))
	nuzno(t, primiPaket(t, c, starija))
	if got := opisDionice(t, c, "B.16.9"); got != "novi opis" {
		t.Errorf("starija verzija vratila je površinu na %q", got)
	}
}

// Epizoda obrane koja stigne prije svoje dionice ne prođe. Popis nedovršenih
// ne preživi ponovno pokretanje, pa je poslije vraća obnova površine iz knjige
// pri pokretanju; zato su epizode među entitetima koje ona prolazi.
func TestEpizodaBezDioniceSeObnavljaPriPokretanju(t *testing.T) {
	sNedovrsenima(t)
	c := noviCvor(t, "novi-ured")
	pocetak := time.Date(2026, 9, 13, 6, 0, 0, 0, time.UTC)
	epizodaID := uuid.MustParse("01a00000-0000-7000-8000-0000000e0001")
	epizoda := verzijaZa(t, EntityEpisodes, epizodaID.String(), vid(5), models.DefenseEpisode{
		ID: epizodaID, SectionCode: "B.34.7", StartedAt: pocetak, Phase: models.PhaseRegular,
		CreatedAt: pocetak, UpdatedAt: pocetak})
	dionica := verzijaZa(t, EntitySections, "B.34.7", vid(4), models.Section{Code: "B.34.7", AreaID: 34, SectorID: "B", Description: "probna"})
	podrucje := verzijaZa(t, EntityAreas, "34", vid(6), models.Area{ID: 34, SectorID: "B", Name: "Područje 34", VgiName: "VGI"})

	if err := primiPaket(t, c, epizoda); err == nil {
		t.Fatal("epizoda bez dionice prošla je strani ključ")
	}
	// ponovno pokretanje: popis nedovršenih je u memoriji
	nedovrseni.Lock()
	nedovrseni.kljucevi = map[string]bool{}
	nedovrseni.Unlock()
	nuzno(t, primiPaket(t, c, dionica, podrucje))

	broj := func() int {
		var n int
		nuzno(t, c.db.QueryRow(`SELECT count(*) FROM defense_episodes WHERE id = ?`, epizodaID.String()).Scan(&n))
		return n
	}
	if broj() != 0 {
		t.Fatal("epizoda je na površini i bez obnove; test ne ispituje ono što treba")
	}
	if _, err := ReplaySurface(ctxRaz, c.db, c.rec); err != nil {
		t.Fatal(err)
	}
	if broj() != 1 {
		t.Error("obnova površine pri pokretanju nije vratila epizodu obrane")
	}
}
