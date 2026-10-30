package repository

import (
	"bytes"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/peers"
	"gocop/internal/razmjena"
)

// kljucProbe potpisuje opozive u testovima primjene
var kljucProbe = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{3}, ed25519.SeedSize))

// mrezaProbe je mreža probe sa stalnim ključem, za ovlasti koje potpisuje
// ključ mreže
var mrezaProbe = razmjena.LoadNetworkKey("Probna", ed25519.NewKeyFromSeed(bytes.Repeat([]byte{4}, ed25519.SeedSize)))

// opozivProbe je opoziv članstva potpisan ključem probe; primjena upisuje
// samo opoziv koji vrijedi sam za sebe
func opozivProbe(nodeID, kljuc string) peers.Opoziv {
	return peers.Opoziv{Vrsta: peers.OpozivClanstva, NodeID: nodeID, PublicKey: kljuc,
		IssuedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), OpozvanoAt: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)}.Potpisi(kljucProbe)
}

// Opoziv vrijedi i kad iza njegove žive verzije stoji arhivirana: opoziv se
// ne poništava, pa ga ni razmjena ni obnova ne smiju izostaviti iz tablice.
func TestOpozivSArhiviranomVerzijomOstajeUTablici(t *testing.T) {
	op := opozivProbe("cop-proba-opozvani", "kljuc-probe")
	id := op.ID
	ziva := verzijaZa(t, peers.EntityOpozivi, id, vid(1), op)
	ponistenje := verzijaZa(t, peers.EntityOpozivi, id, vid(2), op)
	ponistenje.Archived = true

	t.Run("razmjena", func(t *testing.T) {
		sNedovrsenima(t)
		c := noviCvor(t, "cop-proba")
		nuzno(t, primiPaket(t, c, ziva, ponistenje))
		if !imaRedak(t, c, "opozivi", id) {
			t.Error("opoziv nije u tablici kad ga razmjena donese s arhiviranom verzijom")
		}
	})
	t.Run("obnova", func(t *testing.T) {
		sNedovrsenima(t)
		c := noviCvor(t, "cop-proba")
		samoUKnjigu(t, c, []ledger.Version{ziva, ponistenje})
		o, err := ObnoviPovrsinu(ctxRaz, c.db, c.rec)
		nuzno(t, err)
		if o.Upisano[peers.EntityOpozivi] != 1 || !imaRedak(t, c, "opozivi", id) {
			t.Errorf("obnova nije vratila opoziv: %s", o)
		}
	})
	// bez žive verzije nema opoziva koji bi se upisao
	t.Run("samo arhivirana", func(t *testing.T) {
		sNedovrsenima(t)
		c := noviCvor(t, "cop-proba")
		nuzno(t, primiPaket(t, c, ponistenje))
		if imaRedak(t, c, "opozivi", id) {
			t.Error("arhivirana verzija bez žive upisala je opoziv")
		}
	})
}

// redakOpoziva je ključ i potpis u retku opoziva s oznakom id, ili "" kad
// retka nema
func redakOpoziva(t *testing.T, c *cvor, id string) (kljuc, potpis string) {
	t.Helper()
	err := c.db.QueryRow(`SELECT public_key, potpis FROM opozivi WHERE id = ?`, id).Scan(&kljuc, &potpis)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	return kljuc, potpis
}

// Primjena upisuje samo opoziv koji vrijedi sam za sebe: zapis s istom
// oznakom, a drugim sadržajem, ne zauzima mjesto pravog opoziva, ni kad
// stigne prvi, pod drugom oznakom zapisa ili kao novija verzija, a redak
// koji je takav zapis ostavio od prije pravi opoziv zamijeni.
func TestPrimjenaOpozivaSamoIspravnog(t *testing.T) {
	pravi := opozivProbe("cop-proba-opozvani", "kljuc-probe")
	drugi := pravi
	drugi.PublicKey = "drugi-kljuc"
	pravaV := verzijaZa(t, peers.EntityOpozivi, pravi.ID, vid(5), pravi)
	provjeri := func(t *testing.T, c *cvor) {
		t.Helper()
		if k, p := redakOpoziva(t, c, pravi.ID); k != pravi.PublicKey || p != pravi.Potpis {
			t.Errorf("u retku opoziva je ključ %q, a treba %q", k, pravi.PublicKey)
		}
	}
	t.Run("drugi zapis s istom oznakom stigne prvi", func(t *testing.T) {
		sNedovrsenima(t)
		c := noviCvor(t, "cop-proba")
		nuzno(t, primiPaket(t, c, verzijaZa(t, peers.EntityOpozivi, "drugi-zapis", vid(1), drugi)))
		if k, _ := redakOpoziva(t, c, pravi.ID); k != "" {
			t.Fatalf("zapis pod drugom oznakom upisao je redak opoziva (%s)", k)
		}
		nuzno(t, primiPaket(t, c, pravaV))
		provjeri(t, c)
	})
	t.Run("novija verzija pod istom oznakom", func(t *testing.T) {
		sNedovrsenima(t)
		c := noviCvor(t, "cop-proba")
		nuzno(t, primiPaket(t, c, verzijaZa(t, peers.EntityOpozivi, pravi.ID, vid(9), drugi), pravaV))
		provjeri(t, c)
		nuzno(t, primiPaket(t, c, verzijaZa(t, peers.EntityOpozivi, pravi.ID, vid(10), drugi)))
		provjeri(t, c)
	})
	t.Run("redak od prije", func(t *testing.T) {
		sNedovrsenima(t)
		c := noviCvor(t, "cop-proba")
		_, err := c.db.Exec(`INSERT INTO opozivi (id, vrsta, node_id, public_key, issued_at, opozvano_at, opozvao, potpisnik, potpis)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, pravi.ID, pravi.Vrsta, pravi.NodeID, drugi.PublicKey, pravi.IssuedAt, pravi.OpozvanoAt, pravi.Opozvao, pravi.Potpisnik, pravi.Potpis)
		nuzno(t, err)
		nuzno(t, primiPaket(t, c, pravaV))
		provjeri(t, c)
	})
	// ono što ne vrijedi samo za sebe ostaje u knjizi, bez retka
	for ime, v := range map[string]ledger.Version{
		"bez potpisa":        verzijaZa(t, peers.EntityOpozivi, "bez-potpisa", vid(20), peers.Opoziv{ID: "bez-potpisa", Vrsta: peers.OpozivClanstva, PublicKey: "k"}),
		"potpis tuđeg retka": verzijaZa(t, peers.EntityOpozivi, drugi.ID, vid(21), drugi),
		"nije opoziv":        {VersionID: vid(22), Entity: peers.EntityOpozivi, EntityID: "x", NodeID: "ured", Payload: []byte(`[1]`), SchemaVersion: ledger.SchemaVersion},
	} {
		t.Run(ime, func(t *testing.T) {
			sNedovrsenima(t)
			c := noviCvor(t, "cop-proba")
			nuzno(t, primiPaket(t, c, v))
			var n int
			nuzno(t, c.db.QueryRow(`SELECT count(*) FROM opozivi`).Scan(&n))
			h, err := c.rec.History(ctxRaz, v.Entity, v.EntityID)
			nuzno(t, err)
			if n != 0 || len(h) != 1 {
				t.Errorf("redaka %d, verzija u knjizi %d", n, len(h))
			}
		})
	}
}

// Sažimanje knjige čuva svaki opoziv i svaku ovlast za primanje: opoziv
// člana i provjera opoziva čitaju ih iz knjige, pa ostaje i starija verzija
// iste oznake.
func TestSazimanjeCuvaOpoziveIOvlasti(t *testing.T) {
	sNedovrsenima(t)
	c := noviCvor(t, "cop-proba")
	pravi := opozivProbe("cop-proba-opozvani", "kljuc-probe")
	drugi := pravi
	drugi.PublicKey = "drugi-kljuc"
	samoUKnjigu(t, c, []ledger.Version{
		verzijaZa(t, peers.EntityOpozivi, pravi.ID, vid(1), pravi),
		verzijaZa(t, peers.EntityOpozivi, pravi.ID, vid(2), drugi),
		verzijaZa(t, peers.EntityOvlasti, "ured-vukovar", vid(3), map[string]string{"deviceId": "ured-vukovar", "signature": "prva"}),
		verzijaZa(t, peers.EntityOvlasti, "ured-vukovar", vid(4), map[string]string{"deviceId": "ured-vukovar", "signature": "druga"}),
		verzijaZa(t, EntityBlagdani, "b-1", vid(5), map[string]string{"id": "b-1", "naziv": "prvi"}),
		verzijaZa(t, EntityBlagdani, "b-1", vid(6), map[string]string{"id": "b-1", "naziv": "drugi"}),
	})
	n, err := SazmiKnjigu(ctxRaz, c.db, c.rec, time.Now().Add(time.Hour))
	nuzno(t, err)
	if n != 1 {
		t.Errorf("sažimanje je obrisalo %d verzija, a treba samo stariju verziju blagdana", n)
	}
	for _, id := range []string{pravi.ID, "ured-vukovar"} {
		var k int
		nuzno(t, c.db.QueryRow(`SELECT count(*) FROM record_versions WHERE entity_id = ?`, id).Scan(&k))
		if k != 2 {
			t.Errorf("od zapisa %s ostalo je %d verzija", id, k)
		}
	}
}

// Sažimanje čuva i živu i arhiviranu verziju opoziva istog sadržaja, pa
// živa ostaje za obnovu površine; sadržaj koji SQLite ne može pročitati kao
// JSON ne zaustavlja sažimanje
func TestSazimanjeZivihIArhiviranihIDubokogSadrzaja(t *testing.T) {
	sNedovrsenima(t)
	c := noviCvor(t, "cop-proba")
	pravi := opozivProbe("cop-proba-opozvani", "kljuc-probe")
	arhivirana := verzijaZa(t, peers.EntityOpozivi, pravi.ID, vid(2), pravi)
	arhivirana.Archived = true
	dubok := json.RawMessage(strings.Repeat("[", 1100) + strings.Repeat("]", 1100))
	verzije := []ledger.Version{verzijaZa(t, peers.EntityOpozivi, pravi.ID, vid(1), pravi), arhivirana}
	for i, entitet := range []string{peers.EntityOpozivi, peers.EntityOvlasti} {
		for j := range 2 {
			verzije = append(verzije, verzijaZa(t, entitet, "dubok", vid(10+2*i+j), dubok))
		}
	}
	verzije = append(verzije, verzijaZa(t, EntityBlagdani, "b-1", vid(20), map[string]string{"id": "b-1"}))
	samoUKnjigu(t, c, verzije)
	n, err := SazmiKnjigu(ctxRaz, c.db, c.rec, time.Now().Add(time.Hour))
	nuzno(t, err)
	if n != 2 {
		t.Errorf("sažimanje je obrisalo %d verzija, a treba po jednu dubokog sadržaja", n)
	}
	h, err := c.rec.History(ctxRaz, peers.EntityOpozivi, pravi.ID)
	nuzno(t, err)
	if len(h) != 2 {
		t.Errorf("od opoziva su ostale %d verzije", len(h))
	}
	if _, err := ObnoviPovrsinu(ctxRaz, c.db, c.rec); err != nil {
		t.Fatal(err)
	}
	if k, _ := redakOpoziva(t, c, pravi.ID); k != pravi.PublicKey {
		t.Errorf("nakon sažimanja i obnove u retku opoziva je %q", k)
	}
}

// Upis opoziva na površinu (primijeniOpoziv) sam odbija sadržaj koji nije
// opoziv i preskače opoziv koji ne vrijedi sam za sebe, i kad ga pozove
// put mimo izbora verzije za površinu
func TestUpisOpozivaBezIspravnogSadrzaja(t *testing.T) {
	sNedovrsenima(t)
	c := noviCvor(t, "cop-proba")
	pravi := opozivProbe("cop-proba-opozvani", "kljuc-probe")
	tx, err := c.db.BeginTx(ctxRaz, nil)
	nuzno(t, err)
	t.Cleanup(func() {
		_ = tx.Rollback() // test samo čita što je upisano u transakciji, pa se ništa ne sprema
	})
	if err := primijeniOpoziv(ctxRaz, tx, ledger.Version{Entity: peers.EntityOpozivi, EntityID: "x", Payload: []byte(`[1]`)}); err == nil {
		t.Error("sadržaj koji nije opoziv upisan je bez greške")
	}
	nuzno(t, primijeniOpoziv(ctxRaz, tx, verzijaZa(t, peers.EntityOpozivi, "drugi-zapis", vid(1), pravi)))
	nuzno(t, primijeniOpoziv(ctxRaz, tx, verzijaZa(t, peers.EntityOpozivi, pravi.ID, vid(2), pravi)))
	var n int
	nuzno(t, tx.QueryRow(`SELECT count(*) FROM opozivi`).Scan(&n))
	if n != 1 {
		t.Errorf("redaka opoziva %d, a treba samo ispravan", n)
	}
}
