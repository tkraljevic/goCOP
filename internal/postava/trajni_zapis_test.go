package postava

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/trajno"
)

// Zaključava da preuzeti program i kopija baze idu na disk prije
// preimenovanja, a mapa nakon njega. Bez toga je nestanak struje odmah nakon
// nadogradnje mogao ostaviti prazan program ili praznu kopiju baze.
func TestPreuzetoIKopijaSpustajuNaDisk(t *testing.T) {
	popis, prestani := trajno.Prati()
	defer prestani()

	g := noviGitHub(t)
	program := []byte("program")
	g.mu.Lock()
	g.datoteke["/program"] = program
	g.mu.Unlock()
	zbroj := sha256.Sum256(program)
	u := Ugradnja{M: MjestaU(t.TempDir()), Izdanja: g.izdanjaZa(), GOOS: "linux", GOARCH: "amd64"}
	dio := filepath.Join(t.TempDir(), "gocop.dio")
	if err := u.preuzmiProvjereno(context.Background(), g.srv.URL+"/program", dio, hex.EncodeToString(zbroj[:])); err != nil {
		t.Fatal(err)
	}
	if !popis.Ima(dio) {
		t.Error("preuzeti program nije spušten na disk")
	}

	m := MjestaU(t.TempDir())
	if err := os.MkdirAll(m.Podaci, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.BazaCvora(), []byte("baza"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := KopirajBazu(m, "0.0.46-alfa", time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if !popis.ImaUMapi(m.Kopije()) || !popis.Ima(m.Kopije()) {
		t.Error("kopija baze ili mapa kopija nije spuštena na disk")
	}
}
