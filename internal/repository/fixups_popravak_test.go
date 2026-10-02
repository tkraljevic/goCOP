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

// cvorSEpizodom je čvor s dionicom B.16.1 i jednom epizodom utvrđenom iz
// očitanja, bez osnove — kakve su bile prije nego što je epizoda znala za prag
func cvorSEpizodom(t *testing.T, ime string) (*cvorZaRazmjenu, *models.DefenseEpisode) {
	t.Helper()
	c := noviCvorZaRazmjenu(t, ime)
	ctx := context.Background()
	if err := NewSectionRepository(c.db, c.rec).SaveSection(ctx,
		&models.Section{Code: "B.16.1", AreaID: 16, SectorID: "B", Description: "probna"}); err != nil {
		t.Fatal(err)
	}
	e := novaEpizoda(t, c)
	return c, e
}

func novaEpizoda(t *testing.T, c *cvorZaRazmjenu) *models.DefenseEpisode {
	t.Helper()
	e := &models.DefenseEpisode{SectionCode: "B.16.1", StartedAt: time.Date(2013, 3, 1, 6, 0, 0, 0, time.UTC),
		Phase: models.PhaseRegular, Origin: models.EpisodeFromReadings}
	if err := NewEpisodeRepository(c.db, c.rec).SaveEpisode(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	return e
}

func brojVerzija(t *testing.T, c *cvorZaRazmjenu, entity, id string) int {
	t.Helper()
	h, err := c.rec.History(context.Background(), entity, id)
	if err != nil {
		t.Fatal(err)
	}
	return len(h)
}

// popravi u transakciji, kao što to radi RunFixups
func popraviUTx(t *testing.T, c *cvorZaRazmjenu, entity, id string, f func(map[string]json.RawMessage) (bool, error)) (bool, error) {
	t.Helper()
	ctx := context.Background()
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	ok, err := popraviZapis(ctx, tx, c.rec, entity, id, f)
	if err != nil {
		return ok, err
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return ok, nil
}

// Popravak koji ne promijeni sadržaj ne piše verziju, i kad vrati da je
// nešto mijenjao, i kad JSON napiše drukčijim redom
func TestPopraviZapisBezPromjeneNePise(t *testing.T) {
	c, e := cvorSEpizodom(t, "a")
	id := e.ID.String()
	ok, err := popraviUTx(t, c, EntityEpisodes, id, func(m map[string]json.RawMessage) (bool, error) {
		m["phase"] = json.RawMessage(` "` + string(models.PhaseRegular) + `" `)
		return true, nil
	})
	if err != nil || ok {
		t.Fatalf("popravak bez promjene: %v, %v", ok, err)
	}
	if n := brojVerzija(t, c, EntityEpisodes, id); n != 1 {
		t.Errorf("verzija: %d, očekivana 1", n)
	}
	// i popravak koji kaže da nema što
	ok, err = popraviUTx(t, c, EntityEpisodes, id, func(map[string]json.RawMessage) (bool, error) { return false, nil })
	if err != nil || ok || brojVerzija(t, c, EntityEpisodes, id) != 1 {
		t.Errorf("popravak bez posla upisao je verziju: %v, %v", ok, err)
	}
}

// Popravak koji promijeni sadržaj piše točno jednu verziju, a površina je
// nakon njega jednaka knjizi
func TestPopraviZapisPiseJednuVerzijuIOsvjezavaPovrsinu(t *testing.T) {
	c, e := cvorSEpizodom(t, "a")
	id := e.ID.String()
	napomena := func(m map[string]json.RawMessage) (bool, error) {
		m["note"] = json.RawMessage(`"rekonstruirano"`)
		return true, nil
	}
	ok, err := popraviUTx(t, c, EntityEpisodes, id, napomena)
	if err != nil || !ok {
		t.Fatalf("popravak: %v, %v", ok, err)
	}
	if n := brojVerzija(t, c, EntityEpisodes, id); n != 2 {
		t.Errorf("verzija: %d, očekivane 2", n)
	}
	var note string
	if err := c.db.QueryRow(`SELECT note FROM defense_episodes WHERE id = ?`, id).Scan(&note); err != nil {
		t.Fatal(err)
	}
	if note != "rekonstruirano" {
		t.Errorf("površina nije osvježena: %q", note)
	}
	// drugi prolaz istog popravka nema što mijenjati
	if ok, err := popraviUTx(t, c, EntityEpisodes, id, napomena); err != nil || ok {
		t.Errorf("ponovljeni popravak: %v, %v", ok, err)
	}
	if n := brojVerzija(t, c, EntityEpisodes, id); n != 2 {
		t.Errorf("ponovljeni popravak upisao je verziju: %d", n)
	}
}

// Polja koja je u zapis dodao noviji program ostaju i nakon popravka
func TestPopraviZapisCuvaNepoznataPolja(t *testing.T) {
	ctx := context.Background()
	c, e := cvorSEpizodom(t, "a")
	id := e.ID.String()
	top, err := c.rec.Latest(ctx, EntityEpisodes, id)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(top.Payload, &m); err != nil {
		t.Fatal(err)
	}
	m["novo_polje"] = json.RawMessage(`{"razina":3}`)
	tijelo, _ := json.Marshal(m)
	vid, _ := uuid.NewV7()
	if _, err := c.rec.Apply(ctx, []ledger.Version{{VersionID: vid.String(), Entity: EntityEpisodes, EntityID: id,
		NodeID: "noviji", Supersedes: top.VersionID, Payload: tijelo, CreatedAt: time.Now(), SchemaVersion: 1}}); err != nil {
		t.Fatal(err)
	}

	ok, err := popraviUTx(t, c, EntityEpisodes, id, func(m map[string]json.RawMessage) (bool, error) {
		m["note"] = json.RawMessage(`"rekonstruirano"`)
		return true, nil
	})
	if err != nil || !ok {
		t.Fatalf("popravak: %v, %v", ok, err)
	}
	novi, err := c.rec.Latest(ctx, EntityEpisodes, id)
	if err != nil {
		t.Fatal(err)
	}
	var poslije map[string]json.RawMessage
	if err := json.Unmarshal(novi.Payload, &poslije); err != nil {
		t.Fatal(err)
	}
	if string(poslije["novo_polje"]) != `{"razina":3}` {
		t.Errorf("nepoznato polje izgubljeno: %s", novi.Payload)
	}
	if string(poslije["note"]) != `"rekonstruirano"` {
		t.Errorf("popravak nije upisan: %s", novi.Payload)
	}
}

// Popravak osnove praga: epizoda dobiva osnovu PRAG i prag u trenutku
// početka, bez satnog updated_at u tijelu — pa čvor koji je verziju već
// primio od drugoga ne piše svoju
func TestPopravakOsnovePragaJednaVerzijaUMrezi(t *testing.T) {
	ctx := context.Background()
	a, e := cvorSEpizodom(t, "a")
	id := e.ID.String()
	prije, err := a.rec.Latest(ctx, EntityEpisodes, id)
	if err != nil {
		t.Fatal(err)
	}
	b := noviCvorZaRazmjenu(t, "b")
	a.posalji(t, b, "")

	if err := RunFixups(ctx, a.db, a.rec); err != nil {
		t.Fatal(err)
	}
	poslije, err := a.rec.Latest(ctx, EntityEpisodes, id)
	if err != nil {
		t.Fatal(err)
	}
	var p models.DefenseEpisode
	if err := json.Unmarshal(poslije.Payload, &p); err != nil {
		t.Fatal(err)
	}
	var stari models.DefenseEpisode
	json.Unmarshal(prije.Payload, &stari)
	if p.Basis != models.BasisThreshold || p.ThresholdAt == nil || !p.ThresholdAt.Equal(p.StartedAt) {
		t.Errorf("osnova %q, prag %v", p.Basis, p.ThresholdAt)
	}
	if !p.UpdatedAt.Equal(stari.UpdatedAt) {
		t.Errorf("popravak je promijenio updated_at: %v → %v", stari.UpdatedAt, p.UpdatedAt)
	}
	var basis string
	var prag *time.Time
	if err := a.db.QueryRow(`SELECT basis, threshold_at FROM defense_episodes WHERE id = ?`, id).Scan(&basis, &prag); err != nil {
		t.Fatal(err)
	}
	if basis != models.BasisThreshold || prag == nil || !prag.Equal(e.StartedAt) {
		t.Errorf("površina: osnova %q, prag %v", basis, prag)
	}

	// drugi čvor primi popravljenu verziju prije svog popravka
	a.posalji(t, b, "")
	if err := RunFixups(ctx, b.db, b.rec); err != nil {
		t.Fatal(err)
	}
	if n := brojVerzija(t, b, EntityEpisodes, id); n != 2 {
		t.Errorf("na drugom čvoru %d verzija, očekivane 2 (izvorna i jedan popravak)", n)
	}
}

// Zapis koji je zadnji izmijenio noviji program popravak preskače: ostali
// se poprave, pokretanje ne pada, a popravak se ne bilježi kao izveden
func TestRunFixupsPreskaceZapisNovijeSheme(t *testing.T) {
	ctx := context.Background()
	c, e := cvorSEpizodom(t, "a")
	novija := novaEpizoda(t, c)

	top, err := c.rec.Latest(ctx, EntityEpisodes, novija.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	vid, _ := uuid.NewV7()
	if _, err := c.rec.Apply(ctx, []ledger.Version{{VersionID: vid.String(), Entity: EntityEpisodes,
		EntityID: novija.ID.String(), NodeID: "noviji", Supersedes: top.VersionID, Payload: top.Payload,
		CreatedAt: time.Now(), SchemaVersion: ledger.ShemaEntiteta(EntityEpisodes) + 1}}); err != nil {
		t.Fatal(err)
	}

	if err := RunFixups(ctx, c.db, c.rec); err != nil {
		t.Fatalf("pokretanje palo na zapisu novije sheme: %v", err)
	}
	var basis string
	c.db.QueryRow(`SELECT basis FROM defense_episodes WHERE id = ?`, e.ID.String()).Scan(&basis)
	if basis != models.BasisThreshold {
		t.Errorf("obična epizoda nije popravljena: %q", basis)
	}
	if n := brojVerzija(t, c, EntityEpisodes, novija.ID.String()); n != 2 {
		t.Errorf("zapis novije sheme je prepisan: %d verzija", n)
	}
	var izveden int
	c.db.QueryRow(`SELECT COUNT(*) FROM data_fixups WHERE name = 'epizode-osnova-prag'`).Scan(&izveden)
	if izveden != 0 {
		t.Error("popravak s preskočenim zapisom zabilježen je kao izveden")
	}
	// sljedeće pokretanje opet ne pada i ništa ne udvostručuje
	if err := RunFixups(ctx, c.db, c.rec); err != nil {
		t.Fatal(err)
	}
	if n := brojVerzija(t, c, EntityEpisodes, e.ID.String()); n != 2 {
		t.Errorf("ponovljeni popravak: %d verzija", n)
	}
}

// Drugom čvoru sat žuri, pa je njegova verzija na vrhu i iznad one koju
// popravak upravo upiše. Popravak ostaje u povijesti, površina pokazuje
// noviju verziju, a pokretanje ne staje.
func TestPopraviZapisIspodVerzijeSaSatomKojiZuri(t *testing.T) {
	c, e := cvorSEpizodom(t, "spori")
	ctx := context.Background()
	top, err := c.rec.Latest(ctx, EntityEpisodes, e.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.rec.Apply(ctx, []ledger.Version{{VersionID: "ffffffff-ffff-7fff-bfff-ffffffffffff",
		Entity: EntityEpisodes, EntityID: e.ID.String(), NodeID: "brzi", Payload: top.Payload,
		CreatedAt: time.Now(), SchemaVersion: ledger.SchemaVersion, Channel: top.Channel}}); err != nil {
		t.Fatal(err)
	}
	ok, err := popraviUTx(t, c, EntityEpisodes, e.ID.String(), func(m map[string]json.RawMessage) (bool, error) {
		m["note"] = json.RawMessage(`"popravljeno"`)
		return true, nil
	})
	if err != nil || !ok {
		t.Fatalf("popravak ispod tuđe novije verzije: ok=%v err=%v", ok, err)
	}
	if n := brojVerzija(t, c, EntityEpisodes, e.ID.String()); n != 3 {
		t.Errorf("očekivane 3 verzije (izvorna, tuđa, popravak), ima %d", n)
	}
	if v, _ := c.rec.Latest(ctx, EntityEpisodes, e.ID.String()); v.NodeID != "brzi" {
		t.Errorf("na vrhu mora ostati tuđa verzija, a je %s", v.NodeID)
	}
}
