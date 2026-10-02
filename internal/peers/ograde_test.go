package peers

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/sadrzaj"
)

// Delta se reže kad bi poruka prešla ogradu u bajtovima: šalje se početak,
// a ostatak sljedećim razgovorom. Unutar ključa redom nastanka, pa granica
// primatelja ne preskače ništa; prva verzija ide i kad je sama veća.
func TestDeltaSeRezePoBajtovima(t *testing.T) {
	var delta []ledger.Version
	for i := 0; i < 10; i++ {
		delta = append(delta, ledger.Version{VersionID: fmt.Sprintf("%04d", i), NodeID: "unraid", Entity: "stations",
			Payload: json.RawMessage(`"` + strings.Repeat("x", 1000) + `"`)})
	}
	jedna, _ := json.Marshal(delta[0])
	ograda := 3*len(jedna) + 10
	rez := ogradiDeltu(delta, ograda)
	if len(rez) != 3 {
		t.Fatalf("odrezano na %d verzija, očekivano 3", len(rez))
	}
	if b, _ := json.Marshal(deltaMsg{Versions: rez}); len(b) > ograda+100 {
		t.Errorf("poruka %d B prelazi ogradu %d B", len(b), ograda)
	}
	if g := pomakniGranicu(nil, rez); g["unraid"] != "0002" {
		t.Errorf("granica nakon odrezane delte: %v", g)
	}
	if rez := ogradiDeltu(delta, 10); len(rez) != 1 {
		t.Errorf("prva verzija mora proći i kad je sama veća od ograde: %d", len(rez))
	}
	if rez := ogradiDeltu(delta, 1<<20); len(rez) != len(delta) {
		t.Errorf("delta ispod ograde je odrezana: %d", len(rez))
	}
}

// Sadržaj veći od ograde poruke ne šalje se ni sam; manji uz njega ide.
func TestPrevelikSadrzajSeNeSalje(t *testing.T) {
	stara := najveciSadrzajUPoruci
	najveciSadrzajUPoruci = 1 << 10
	defer func() { najveciSadrzajUPoruci = stara }()

	ctx := context.Background()
	sp, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sp.Zatvori()
	golem, err := sp.Upisi(ctx, "application/pdf", []byte(strings.Repeat("g", 2<<10)), "ovdje")
	if err != nil {
		t.Fatal(err)
	}
	malen, err := sp.Upisi(ctx, "application/pdf", []byte("malen"), "ovdje")
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{}
	s.SetSpremiste(sp)
	stavke := s.sadrzajZa(ctx, []string{golem, malen}, nil)
	if len(stavke) != 1 || stavke[0].Otisak != malen {
		t.Fatalf("poslano: %d stavki", len(stavke))
	}
}
