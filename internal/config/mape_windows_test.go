//go:build windows

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Na Windowsu putanju bez slova diska sustav otvara na disku radne mape
// (C:\goCOP\vodostaji). Ona je izričita, a puna putanja nosi taj disk, ne
// radnu mapu ispred sebe.
func TestPutanjaBezSlovaDiskaNaWindowsu(t *testing.T) {
	t.Chdir(t.TempDir())
	radna, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	disk := filepath.VolumeName(radna)
	for _, put := range []string{`\goCOP-nema\vodostaji`, `/goCOP-nema/vodostaji`} {
		m := ProvjeriMapu("podaci", put)
		if m.Relativna() || m.Nedostaje() || m.Drugdje() || m.Puna != disk+filepath.Clean(put) {
			t.Errorf("%s: %+v", put, m)
		}
	}
}
