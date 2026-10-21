package config

import "testing"

// [sync] protokol je zadano „gocop”, i u datoteci koja ga ne spominje
// (postavke s prijašnjih inačica). Čita se točno kako je upisan, bez
// skraćivanja i pretvaranja u mala slova: neispravno ime odbija pokretanje.
func TestProtokolMrezeIzDatoteke(t *testing.T) {
	for _, s := range []struct{ tekst, protokol string }{
		{"", "gocop"},
		{"[sync]\nauto_sync = '1h'\n", "gocop"},
		{"[sync]\nprotokol = 'gocop-sim'\n", "gocop-sim"},
		{"[sync]\nprotokol = ' Gocop '\n", " Gocop "},
	} {
		cfg, err := ProcitajTekst([]byte(s.tekst))
		if err != nil || cfg.Sync.Protokol != s.protokol {
			t.Errorf("%q: %q (%v)", s.tekst, cfg.Sync.Protokol, err)
		}
	}
}
