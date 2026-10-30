package razmjena

import (
	"bytes"
	"crypto/ed25519"
	"testing"
	"time"
)

// opozivProbe je opoziv članstva potpisan ključem iz stalnog sjemena
func opozivProbe() (Opoziv, ed25519.PrivateKey) {
	priv := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{5}, ed25519.SeedSize))
	op := Opoziv{Vrsta: "clanstvo", NodeID: "pperic-thinkpad", PublicKey: "kljuc-clana",
		IssuedAt: time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC), OpozvanoAt: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC),
		Opozvao: "cop-osijek"}
	return op.Potpisi(priv), priv
}

// Oblik koji se potpisuje, oznaka i potpis isti su kao u opozivima koje su
// potpisala izdanja od 0.0.33: drukčiji bi poništili sve dotad izdane
// opozive
func TestOpozivStalnogOblika(t *testing.T) {
	op, _ := opozivProbe()
	if got := string(op.signedBytes()); got != `{"v":"opoziv/1","vrsta":"clanstvo","nodeId":"pperic-thinkpad","publicKey":"kljuc-clana","issuedAt":1790841600,"opozvanoAt":1793361600,"opozvao":"cop-osijek","potpisnik":"bnoc3Smwt4/ROvTFWY/v9O8qlxZuPKby5Pv8zYBQW/E="}` {
		t.Errorf("oblik za potpis: %s", got)
	}
	if op.ID != "clanstvo:kljuc-clana:1790841600:345e09d5f1a96cca" {
		t.Errorf("oznaka: %s", op.ID)
	}
	if op.Potpis != "r9BIfQN3ZlikZa8hHmsXLrUzXy02bBqRKF/oMS73NM5rZ5nQ8Zie6sfSgyrccwSC5xEWFwMH3HWAQzKAppHnBA==" {
		t.Errorf("potpis: %s", op.Potpis)
	}
}

// Opoziv vrijedi sam za sebe samo s oznakom izvedenom iz sadržaja i
// potpisom ključa koji je naveden kao potpisnik; tko ga je potpisao,
// provjerava Potpisao
func TestOpozivIspravanSamZaSebe(t *testing.T) {
	op, priv := opozivProbe()
	if !op.Ispravan() || !op.Potpisao(priv.Public().(ed25519.PublicKey)) {
		t.Fatal("potpisan opoziv ne vrijedi")
	}
	drugi := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{6}, ed25519.SeedSize))
	if op.Potpisao(drugi.Public().(ed25519.PublicKey)) || op.Potpisao() {
		t.Error("opoziv vrijedi kao da ga je potpisao drugi ključ")
	}
	pokvaren := op
	pokvaren.Potpis = "%%%"
	if pokvaren.Potpisao(priv.Public().(ed25519.PublicKey)) {
		t.Error("potpis koji nije base64 vrijedi")
	}
	for ime, izmjena := range map[string]func(*Opoziv){
		"drugi ključ":          func(o *Opoziv) { o.PublicKey = "drugi-kljuc" },
		"drugi čvor":           func(o *Opoziv) { o.NodeID = "drugi" },
		"drugo vrijeme":        func(o *Opoziv) { o.OpozvanoAt = o.OpozvanoAt.Add(time.Second) },
		"drugi potpisnik":      func(o *Opoziv) { o.Potpisnik = PublicKeyString(drugi.Public().(ed25519.PublicKey)) },
		"potpisnik nije ključ": func(o *Opoziv) { o.Potpisnik = "nije-kljuc" },
		"druga oznaka":         func(o *Opoziv) { o.ID += "x" },
		"potpis nije base64":   func(o *Opoziv) { o.Potpis = "%%%" },
		"tuđi potpis":          func(o *Opoziv) { *o = o.Potpisi(drugi); o.Potpisnik = op.Potpisnik },
	} {
		x := op
		izmjena(&x)
		if x.Ispravan() {
			t.Errorf("%s: opoziv vrijedi sam za sebe", ime)
		}
	}
}
