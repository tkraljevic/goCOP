package peers

// Potpis autora verzije (Odluke (26. 11. 2026.)): ovo računalo svaku
// verziju koju upiše u knjigu potpiše ključem čvora (razmjena.PotpisiVerziju).
// Potpis putuje uz verziju u .cop paketu i .db arhivi, a razmjenom samo
// računalu koje je javilo oznaku potpis-verzije/1. Primljeni potpis knjiga
// još ne sprema; to dolazi s provjerom pri primanju.

import (
	"crypto/ed25519"
	"errors"
	"io/fs"
	"path/filepath"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// PodaciVerzije su podaci verzije iz knjige koje potpis autora pokriva
func PodaciVerzije(v ledger.Version) razmjena.PodaciVerzije {
	return razmjena.PodaciVerzije{
		VersionID: v.VersionID, Entity: v.Entity, EntityID: v.EntityID, NodeID: v.NodeID,
		Supersedes: v.Supersedes, Archived: v.Archived, Channel: v.Channel,
		SchemaVersion: v.SchemaVersion, CreatedAt: v.CreatedAt, Payload: v.Payload,
	}
}

// PotpisnikVerzija potpisuje verzije knjige ključem autor, u mreži s
// imenom protokola protokol
func PotpisnikVerzija(autor ed25519.PrivateKey, protokol string) ledger.Potpisnik {
	return func(v ledger.Version) (string, string, error) {
		return razmjena.PotpisiVerziju(autor, protokol, PodaciVerzije(v))
	}
}

// PotpisnikVerzija potpisuje verzije knjige ključem ovog čvora
func (n *Node) PotpisnikVerzija(protokol string) ledger.Potpisnik {
	return PotpisnikVerzija(n.key, protokol)
}

// PotpisnikIzDatoteke potpisuje ključem čvora iz datoteke uz bazu, za
// naredbe koje rade bez pokrenutog čvora. Ključ ne stvara: bez datoteke
// vraća nil, pa verzije idu bez potpisa. Datoteka koja postoji, a ne da se
// pročitati, je greška.
func PotpisnikIzDatoteke(dbPath, protokol string) (ledger.Potpisnik, error) {
	key, err := razmjena.LoadKey(filepath.Join(filepath.Dir(dbPath), KeyFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return PotpisnikVerzija(key, protokol), nil
}

// potpisDogovoren javlja da druga strana prima potpis autora uz verziju
func (r *razgovorSCvorom) potpisDogovoren() bool {
	return razmjena.Dogovor(r.moja.Mogucnosti, r.theirs.Mogucnosti)[razmjena.ImePotpisVerzije] == 1
}

// bezPotpisa su iste verzije bez potpisa i ključa autora: druga strana bez
// oznake potpis-verzije/1 dobije deltu bajt za bajt kakvu je dobivala i
// prije potpisa
func bezPotpisa(versions []ledger.Version) []ledger.Version {
	out := make([]ledger.Version, len(versions))
	for i, v := range versions {
		v.Potpis, v.KljucAutora = "", ""
		out[i] = v
	}
	return out
}
