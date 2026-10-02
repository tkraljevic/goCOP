package peers_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"gocop/internal/peers"
)

// Delta skraćena zbog veličine poruke nastavlja se odmah, i kad ju je
// skratio čvor koji prima poziv (laptop zove Unraid kroz tunel, pa je
// Unraid uvijek ta strana). Prije je ostatak čekao sljedeći krug.
func TestSkracenaDeltaSNazvaneStraneNastavljaOdmah(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	a := startNodeBezRegistra(t, ctx, "unraid")
	b := startNodeBezRegistra(t, ctx, "laptop")
	founder(t, ctx, a, "Hrvatske vode")
	pair(t, ctx, a, b)
	defer peers.SuziDeltu(350 << 10)()

	velika := strings.Repeat("x", 100<<10)
	for i := 0; i < 20; i++ {
		if _, err := a.rec.Record(ctx, a.db, "probni_zapis", fmt.Sprintf("z-%02d", i), map[string]string{"tekst": velika}); err != nil {
			t.Fatal(err)
		}
	}
	b.svc.SyncAll(ctx)
	var n int
	if err := b.db.QueryRow(`SELECT count(*) FROM record_versions WHERE entity = 'probni_zapis'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 20 {
		t.Errorf("nakon jednog SyncAll B ima %d od 20 verzija", n)
	}
}
