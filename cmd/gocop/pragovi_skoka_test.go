package main

import (
	"path/filepath"
	"testing"

	"gocop/internal/skok"
)

// Bez arhive (npr. prije prvog paketa) vrijedi zadani prag skoka.
func TestPragoviSkokaBezArhive(t *testing.T) {
	p := pragoviSkoka(filepath.Join(t.TempDir(), "nema.db"))
	if v := p.Za("donji-brod"); v != skok.ZadaniPrag {
		t.Errorf("prag bez arhive: %v", v)
	}
}
