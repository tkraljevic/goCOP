package peers

// Zlatni test paketa karte: potpisani bajtovi (kanonskaKarta), potpis
// ed25519 stalnim ključem i ime datoteke paketa. Paket karte koji je
// izdalo jedno izdanje mora se učitati u drugom, pa se ovo ne smije
// promijeniti ni kad karta izađe iz peers (N04). Vrijednosti su zapisane
// s 0.0.71.

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/razmjena"
)

// stalniKljucKarte je ključ izmišljenog čvora cop-osijek, uvijek isti
func stalniKljucKarte() ed25519.PrivateKey {
	sjeme := sha256.Sum256([]byte("izmišljeni čvor cop-osijek, stalni ključ za zlatni test karte"))
	return ed25519.NewKeyFromSeed(sjeme[:])
}

// zlatnaKarta je opis karte s nazivom s dijakriticima i obuhvatom koji se
// zaokružuje na šest decimala; dijelovi se ne potpisuju
func zlatnaKarta() models.VektorskaKarta {
	return models.VektorskaKarta{Naziv: "Sektor B – Baranja", Izvor: "OpenStreetMap (proba)",
		Otisak: "5f3c2a9e8d7b6c5a4f3e2d1c0b9a8f7e6d5c4b3a2f1e0d9c8b7a6f5e4d3c2b1a", Bajtova: 3_221_225,
		Obuhvat: [4]float64{18.3, 45.5, 18.9512345678, 45.95}, NajviseZ: 15,
		Dio: 4 << 20, Dijelovi: []string{"aa", "bb"}}
}

const (
	zlatniBajtoviKarte = "inacica\t1\nvrsta\tkarta\nnaziv\tSektor B – Baranja\nizvor\tOpenStreetMap (proba)\n" +
		"otisak\t5f3c2a9e8d7b6c5a4f3e2d1c0b9a8f7e6d5c4b3a2f1e0d9c8b7a6f5e4d3c2b1a\nbajtova\t3221225\n" +
		"obuhvat\t18.300000,45.500000,18.951235,45.950000\nnajvise_z\t15\nizdao\tcop-osijek\n"
	zlatniKljucKarte  = "2N0jVwAy00vN6x0Rd0e/aZrRKoSvD/Ark4TUHNjIMz8="
	zlatniPotpisKarte = "wR7y1YcKuiEIdM5GB0L/k3t8r64F1sBFCICJnlypDyG2T5RZMF+XWxgLRJTmWA59ed7zi0fpHzw7BLOqOzFCAw=="
)

func TestZlatniPaketKarte(t *testing.T) {
	kljuc := stalniKljucKarte()
	m := KartaManifest{Inacica: KartaInacica, Vrsta: VrstaPaketaKarte, Karta: zlatnaKarta(), Izdao: "cop-osijek"}
	if got := string(kanonskaKarta(m)); got != zlatniBajtoviKarte {
		t.Fatalf("potpisani bajtovi:\n%q\nočekivano:\n%q", got, zlatniBajtoviKarte)
	}
	bez := m
	bez.Karta.Dio, bez.Karta.Dijelovi = 0, nil
	if string(kanonskaKarta(bez)) != zlatniBajtoviKarte {
		t.Error("dijelovi karte ušli su u potpisane bajtove")
	}
	if got := base64.StdEncoding.EncodeToString(kljuc.Public().(ed25519.PublicKey)); got != zlatniKljucKarte {
		t.Fatalf("javni ključ: %s", got)
	}
	if got := base64.StdEncoding.EncodeToString(ed25519.Sign(kljuc, []byte(zlatniBajtoviKarte))); got != zlatniPotpisKarte {
		t.Errorf("potpis: %s", got)
	}
	m.Potpis = &CopPotpis{Kljuc: zlatniKljucKarte, Potpis: zlatniPotpisKarte}
	if javni, err := ProvjeriKartaPotpis(m); err != nil || !bytes.Equal(javni, kljuc.Public().(ed25519.PublicKey)) {
		t.Errorf("zlatni potpis ne prolazi provjeru: %v", err)
	}
	m.Nastalo = m.Nastalo.AddDate(1, 0, 0) // vrijeme izdavanja se ne potpisuje
	if _, err := ProvjeriKartaPotpis(m); err != nil {
		t.Errorf("uz drugo vrijeme izdavanja: %v", err)
	}
	m.Karta.Naziv = "Sektor C"
	if _, err := ProvjeriKartaPotpis(m); err == nil {
		t.Error("potpis prolazi uz drugi naziv karte")
	}

	k := zlatnaKarta()
	for naziv, ime := range map[string]string{
		"Sektor B – Baranja":   "gocop-karta-sektor-b-baranja.cop",
		"Karta Đakovo i Čepin": "gocop-karta-karta-dakovo-i-cepin.cop",
		"  Vukovar  ":          "gocop-karta-vukovar.cop",
		"v-lista":              "gocop-karta-v-lista.cop",
		"":                     "gocop-karta-5f3c2a9e8d7b.cop",
		"!!!":                  "gocop-karta-5f3c2a9e8d7b.cop",
	} {
		k.Naziv = naziv
		if got := ImePaketaKarte(k); got != ime {
			t.Errorf("ime paketa za %q: %s, očekivano %s", naziv, got, ime)
		}
	}
}

// Paket koji izveze čvor sa stalnim ključem nosi točno zlatni ključ i
// zlatni potpis, i isti opis karte
func TestIzvezenaKartaNosiZlatniPotpis(t *testing.T) {
	dir := t.TempDir()
	if err := razmjena.SaveKey(filepath.Join(dir, KeyFileName), stalniKljucKarte()); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "gocop.db")
	baza, err := db.BazaIzPredloska(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	n, err := LoadNode(dbPath, "cop-osijek", "cop-osijek", "test")
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewService(baza, ledger.New(baza, "cop-osijek"), n, Ports{})
	if err != nil {
		t.Fatal(err)
	}
	put := filepath.Join(dir, "karta.pmtiles")
	if err := os.WriteFile(put, []byte("pločice"), 0o600); err != nil {
		t.Fatal(err)
	}
	var paket bytes.Buffer
	if _, err := IzveziKartu(s.Node(), zlatnaKarta(), put, &paket); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(paket.Bytes()), int64(paket.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var m KartaManifest
	for _, f := range z.File {
		if f.Name != "manifest.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close() // samo čitanje
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
	}
	if m.Potpis == nil || m.Potpis.Kljuc != zlatniKljucKarte || m.Potpis.Potpis != zlatniPotpisKarte {
		t.Errorf("potpis izvezenog paketa: %+v", m.Potpis)
	}
	if string(kanonskaKarta(m)) != zlatniBajtoviKarte {
		t.Errorf("opis izvezenog paketa: %q", kanonskaKarta(m))
	}
}
