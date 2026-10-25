package main

import (
	"context"
	"path/filepath"
	"testing"
)

// Kvar mjerila na čvoru bez baze i arhive: veze se otvore, priprema samo
// zabilježi grešku, a stanje se ne da izračunati, ali program ne pada.
func TestKvaroviMjerilaBezBaze(t *testing.T) {
	dir := t.TempDir()
	p := kvaroviMjerila(filepath.Join(dir, "nema.db"), filepath.Join(dir, "nema-arhive.db"), nil)
	pripremiKvarove(context.Background(), p)
	if p.Izuzeta(context.Background(), "donji-brod") {
		t.Error("bez registra letva nije izuzeta")
	}
}
