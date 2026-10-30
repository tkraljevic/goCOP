package web

import (
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/peers"
)

// Rečenica o karti koja stiže razmjenom: koliko je stiglo, čeka li računalo
// koje je ima, miruje li preko interneta zbog obrane i je li stigla kriva
func TestPrijenosKarteRijecima(t *testing.T) {
	sad := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	stize := peers.StanjeKarte{Prenosi: true, Dijelova: 12, Svih: 29, Bajtova: 48 << 20, Ukupno: 115 << 20, Od: sad.Add(-time.Hour), Zadnji: sad.Add(-time.Minute)}
	ceka := stize
	ceka.Zadnji = sad.Add(-31 * time.Minute)
	nista := stize
	nista.Dijelova, nista.Bajtova, nista.Zadnji, nista.Od = 0, 0, time.Time{}, sad.Add(-40*time.Minute)
	obrana := stize
	obrana.UObrani = true
	for opis, c := range map[string]struct {
		st   peers.StanjeKarte
		zeli string
	}{
		"bez prijenosa": {peers.StanjeKarte{}, "Učitajte njezin .cop paket ispod."},
		"kriva":         {peers.StanjeKarte{Los: true}, "Karta koja je stigla ne odgovara opisu. Učitajte je ponovno na računalu koje je ima."},
		"stiže":         {stize, "Karta stiže razmjenom: 48,0 MB od 115,0 MB (12 od 29 dijelova)."},
		"čeka":          {ceka, "Karta čeka računalo koje je ima. Stiglo je 48,0 MB od 115,0 MB."},
		"ništa":         {nista, "Karta čeka računalo koje je ima. Stiglo je 0 B od 115,0 MB."},
		"u obrani":      {obrana, "Karta stiže razmjenom: 48,0 MB od 115,0 MB (12 od 29 dijelova). Dok traje obrana, stiže i šalje se samo s računalima u istoj lokalnoj mreži (ista kuća ili ured)."},
	} {
		if got := prijenosKarteRijecima(c.st, sad); got != c.zeli {
			t.Errorf("%s: %q", opis, got)
		}
	}
	r := httptest.NewRequest("GET", "/administracija/karte", nil)
	if got := (&AktiHandler{}).prijenosKarte(r); got != "Učitajte njezin .cop paket ispod." {
		t.Errorf("bez razmjene: %q", got)
	}
	// razmjena bez prijenosa karte (PostaviKarte) daje prazno stanje
	dbPath := filepath.Join(t.TempDir(), "gocop.db")
	baza, err := db.BazaIzPredloska(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	n, err := peers.LoadNode(dbPath, "cop-proba", "cop-proba", "test")
	if err != nil {
		t.Fatal(err)
	}
	ps, err := peers.NewService(baza, ledger.New(baza, "test"), n, peers.Ports{})
	if err != nil {
		t.Fatal(err)
	}
	h := &AktiHandler{}
	h.SetPeers(ps)
	if got := h.prijenosKarte(r); got != "Učitajte njezin .cop paket ispod." {
		t.Errorf("razmjena bez prijenosa karte: %q", got)
	}
}
