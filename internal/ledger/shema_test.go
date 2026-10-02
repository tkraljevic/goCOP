package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

// Stariji program zna samo naziv i prag; noviji je dodao vodotok i
// opomenu. Kad stariji izmijeni zapis, polja novijeg ostaju.
type stariZapis struct {
	Naziv string `json:"naziv"`
	Prag  int    `json:"prag,omitempty"`
}

func payloadMapa(t *testing.T, rec *Recorder, entity, id string) map[string]any {
	t.Helper()
	v, err := rec.Latest(context.Background(), entity, id)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(v.Payload, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

var brojac int

// novijaVerzija upisuje verziju kakvu bi napisao noviji program na drugom čvoru
func novijaVerzija(t *testing.T, rec *Recorder, entity, id, payload string, shema int) {
	t.Helper()
	brojac++
	vid := fmt.Sprintf("01990000-0000-7000-8000-%012d", brojac)
	if _, err := rec.Apply(context.Background(), []Version{{VersionID: vid, Entity: entity, EntityID: id, NodeID: "noviji",
		Payload: json.RawMessage(payload), CreatedAt: time.Now(), SchemaVersion: shema}}); err != nil {
		t.Fatal(err)
	}
}

func TestStarijiProgramCuvaNepoznataPolja(t *testing.T) {
	db := openTestDB(t)
	rec := New(db, "stari")
	ctx := context.Background()

	novijaVerzija(t, rec, "polja_a", "st-1", `{"naziv":"Županja","prag":600,"vodotok":"Sava","opomena":{"cm":650}}`, 1)
	if _, err := rec.Record(ctx, db, "polja_a", "st-1", stariZapis{Naziv: "Županja (Sava)", Prag: 610}); err != nil {
		t.Fatal(err)
	}
	m := payloadMapa(t, rec, "polja_a", "st-1")
	if m["naziv"] != "Županja (Sava)" || m["prag"] != float64(610) {
		t.Errorf("poznata polja nisu iz nove verzije: %v", m)
	}
	if m["vodotok"] != "Sava" || m["opomena"] == nil {
		t.Errorf("nepoznata polja novijeg programa izgubljena: %v", m)
	}

	// Ispražnjeno poznato polje (omitempty ga izbaci iz tijela) ne smije
	// oživjeti iz prethodne verzije
	if _, err := rec.Record(ctx, db, "polja_a", "st-1", stariZapis{Naziv: "Županja"}); err != nil {
		t.Fatal(err)
	}
	m = payloadMapa(t, rec, "polja_a", "st-1")
	if _, ima := m["prag"]; ima {
		t.Errorf("ispražnjeni prag je oživio: %v", m)
	}
	if m["vodotok"] != "Sava" {
		t.Errorf("nepoznato polje se izgubilo pri drugoj izmjeni: %v", m)
	}

	// Arhiviranje također čuva nepoznato
	if _, err := rec.Archive(ctx, db, "polja_a", "st-1", stariZapis{Naziv: "Županja"}); err != nil {
		t.Fatal(err)
	}
	if m = payloadMapa(t, rec, "polja_a", "st-1"); m["vodotok"] != "Sava" {
		t.Errorf("spomenik je izgubio nepoznato polje: %v", m)
	}
}

// Ugrađena struktura i polje s json:"-": poznata su kako ih vidi encoding/json
type ugradjeni struct {
	Naziv string `json:"naziv"`
	Tajna string `json:"-"`
}
type omotac struct {
	ugradjeni
	Lozinka string `json:"lozinka,omitempty"`
}

func TestPoznataPoljaUgradjeneStrukture(t *testing.T) {
	db := openTestDB(t)
	rec := New(db, "stari")
	ctx := context.Background()

	novijaVerzija(t, rec, "polja_b", "u-1", `{"naziv":"a","lozinka":"x","Tajna":"stara","novo":1}`, 1)
	if _, err := rec.Record(ctx, db, "polja_b", "u-1", omotac{ugradjeni: ugradjeni{Naziv: "b"}}); err != nil {
		t.Fatal(err)
	}
	m := payloadMapa(t, rec, "polja_b", "u-1")
	if _, ima := m["lozinka"]; ima {
		t.Errorf("polje ugrađene/omotane strukture je oživjelo: %v", m)
	}
	if m["novo"] != float64(1) {
		t.Errorf("nepoznato polje izgubljeno: %v", m)
	}
	// "Tajna" ima json:"-": ovaj je program nikad ne piše, pa je za njega nepoznata
	if m["Tajna"] != "stara" {
		t.Errorf("polje koje program ne zapisuje (json:\"-\") treba prenijeti: %v", m)
	}
}

func TestMapaIIskljuceniEntitetiNePrenoseNista(t *testing.T) {
	db := openTestDB(t)
	rec := New(db, "stari")
	ctx := context.Background()

	novijaVerzija(t, rec, "polja_c", "m-1", `{"a":"1","b":"2"}`, 1)
	if _, err := rec.Record(ctx, db, "polja_c", "m-1", map[string]string{"a": "3"}); err != nil {
		t.Fatal(err)
	}
	if m := payloadMapa(t, rec, "polja_c", "m-1"); len(m) != 1 {
		t.Errorf("mapa zna sva svoja polja, ništa se ne prenosi: %v", m)
	}

	BezPrijenosaPolja("polja_d")
	novijaVerzija(t, rec, "polja_d", "p-1", `{"naziv":"x","potpisano":"staro"}`, 1)
	if _, err := rec.Record(ctx, db, "polja_d", "p-1", stariZapis{Naziv: "y"}); err != nil {
		t.Fatal(err)
	}
	if m := payloadMapa(t, rec, "polja_d", "p-1"); m["potpisano"] != nil {
		t.Errorf("potpisanom zapisu staro polje ne smije prijeći u novu verziju: %v", m)
	}

	UmiroviPolja("polja_e", "ukinuto")
	novijaVerzija(t, rec, "polja_e", "p-2", `{"naziv":"x","ukinuto":1,"novo":2}`, 1)
	if _, err := rec.Record(ctx, db, "polja_e", "p-2", stariZapis{Naziv: "y"}); err != nil {
		t.Fatal(err)
	}
	if m := payloadMapa(t, rec, "polja_e", "p-2"); m["ukinuto"] != nil || m["novo"] != float64(2) {
		t.Errorf("umirovljeno polje mora otpasti, novo ostati: %v", m)
	}
}

func TestZapisNovijeShemeSeNeUredjuje(t *testing.T) {
	db := openTestDB(t)
	rec := New(db, "stari")
	ctx := context.Background()

	var javljeno []Novost
	rec.NaNovost(func(n Novost) { javljeno = append(javljeno, n) })
	novijaVerzija(t, rec, "shema_a", "z-1", `{"naziv":"x"}`, 3)

	_, err := rec.Record(ctx, db, "shema_a", "z-1", stariZapis{Naziv: "y"})
	var ns *NovijaShemaError
	if !errors.As(err, &ns) || !errors.Is(err, ErrNovijaShema) || ns.Shema != 3 || ns.Lokalna != SchemaVersion {
		t.Fatalf("očekivana NovijaShemaError, dobiveno %v", err)
	}
	if _, err := rec.Archive(ctx, db, "shema_a", "z-1", stariZapis{}); !errors.Is(err, ErrNovijaShema) {
		t.Errorf("ni arhivirati se ne smije: %v", err)
	}
	if len(javljeno) != 1 || javljeno[0].Shema != 3 {
		t.Errorf("novost nije javljena: %+v", javljeno)
	}

	// Kad se program ažurira i zna shemu 3, uređuje i piše shemu 3
	PostaviShemu("shema_a", 3)
	if _, err := rec.Record(ctx, db, "shema_a", "z-1", stariZapis{Naziv: "y"}); err != nil {
		t.Fatal(err)
	}
	v, _ := rec.Latest(ctx, "shema_a", "z-1")
	if v.SchemaVersion != 3 {
		t.Errorf("verzija nosi shemu %d, očekivano 3", v.SchemaVersion)
	}
	// ostali entiteti i dalje pišu prvu shemu
	rec.Record(ctx, db, "shema_b", "z-2", stariZapis{Naziv: "y"})
	if v, _ := rec.Latest(ctx, "shema_b", "z-2"); v.SchemaVersion != SchemaVersion {
		t.Errorf("shema drugog entiteta: %d", v.SchemaVersion)
	}
}

func TestNovostiNakonPokretanja(t *testing.T) {
	db := openTestDB(t)
	pisac := New(db, "noviji")
	novijaVerzija(t, pisac, "shema_c", "z-1", `{}`, 2)
	novijaVerzija(t, pisac, "nepoznati_ent", "n-1", `{}`, 1)
	novijaVerzija(t, pisac, "poznati_ent", "p-1", `{}`, 1)

	PoznatiEntiteti("shema_c", "poznati_ent")
	t.Cleanup(func() {
		shemeMu.Lock()
		poznatiEnt = map[string]bool{}
		shemeMu.Unlock()
	})
	rec := New(db, "stari")
	if err := rec.UcitajNovosti(context.Background()); err != nil {
		t.Fatal(err)
	}
	n := rec.Novosti()
	if len(n) != 2 || n[0].Entity != "nepoznati_ent" || !n[0].Nepoznat || n[1].Entity != "shema_c" || n[1].Shema != 2 {
		t.Errorf("novosti: %+v", n)
	}
}

// Sažimanje ne smije spustiti granicu autora: zadnja verzija svakog autora
// ostaje i kad je zapis poslije izmijenio netko drugi.
func TestSazimanjeCuvaGranicuAutora(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	a, b := New(db, "a"), New(db, "b")
	a.Record(ctx, db, "zapisi", "z-1", stariZapis{Naziv: "1"})
	a.Record(ctx, db, "zapisi", "z-1", stariZapis{Naziv: "2"})
	time.Sleep(2 * time.Millisecond)
	b.Record(ctx, db, "zapisi", "z-1", stariZapis{Naziv: "3"})
	prije, _ := a.Frontier(ctx)
	if _, err := a.Compact(ctx, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	poslije, _ := a.Frontier(ctx)
	if poslije["a"] != prije["a"] || poslije["b"] != prije["b"] {
		t.Errorf("granica se pomaknula: prije %v, poslije %v", prije, poslije)
	}
	if h, _ := a.History(ctx, "zapisi", "z-1"); len(h) != 2 {
		t.Errorf("ostaju zadnja verzija zapisa i zadnja verzija autora a, ostalo %d", len(h))
	}
}

// Nepoznat entitet čije su neke verzije i novije sheme broji se jednom
func TestNovostiNepoznatogEntitetaBezDvostrukogBrojanja(t *testing.T) {
	db := openTestDB(t)
	pisac := New(db, "noviji")
	novijaVerzija(t, pisac, "buduci_ent", "b-1", `{}`, 1)
	novijaVerzija(t, pisac, "buduci_ent", "b-2", `{}`, 2)
	novijaVerzija(t, pisac, "buduci_ent", "b-3", `{}`, 2)

	PoznatiEntiteti("nesto_drugo")
	t.Cleanup(func() {
		shemeMu.Lock()
		poznatiEnt = map[string]bool{}
		shemeMu.Unlock()
	})
	rec := New(db, "stari")
	var javljeno []Novost
	rec.NaNovost(func(n Novost) { javljeno = append(javljeno, n) })
	if err := rec.UcitajNovosti(context.Background()); err != nil {
		t.Fatal(err)
	}
	n := rec.Novosti()
	if len(n) != 1 || !n[0].Nepoznat || n[0].Verzija != 3 || n[0].Shema != 2 {
		t.Errorf("novosti: %+v", n)
	}
	if len(javljeno) != 1 || javljeno[0].Verzija != 3 {
		t.Errorf("javljeno: %+v", javljeno)
	}
}
