package godisnjak

import (
	"path/filepath"
	"testing"
)

// Letva koja u stablu već stoji ide u svoj sliv, nova u zadani. Sliv ili
// letva izvan stabla odbijaju se.
func TestMapaLetveGodisnjaka(t *testing.T) {
	koren := t.TempDir()
	z := Zadatak{Koren: koren, Sliv: "dunav", SlivZa: map[string]string{"batina": "drava"}}
	for letva, hoce := range map[string]string{"batina": filepath.Join(koren, "drava", "batina"), "bezdan": filepath.Join(koren, "dunav", "bezdan")} {
		if m, err := z.mapaLetve(letva); err != nil || m != hoce {
			t.Errorf("%s: %s, %v", letva, m, err)
		}
	}
	if m, err := z.mapaLetve(".."); err == nil {
		t.Errorf("letva ..: %s", m)
	}
	z.Sliv = ".."
	if m, err := z.mapaLetve("bezdan"); err == nil {
		t.Errorf("sliv ..: %s", m)
	}
}
