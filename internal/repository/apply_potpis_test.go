package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"gocop/internal/ledger"
)

// Potpis e-pošte s drugog čvora prolazi isti čistač kao potpis spremljen
// ovdje: oznake i atributi izvan popisa otpadaju, a običan potpis ostaje.
func TestPotpisSDrugogCvoraSeCisti(t *testing.T) {
	baza, repo := bazaAkata(t)
	ctx := context.Background()
	for i, s := range []struct{ stigao, ostaje string }{
		{`<b>Pero</b><img src="x"/onerror="y()"><script>z()</script>`, `<b>Pero</b>`},
		{`<p>Pero Perić</p><a href="mailto:pero@primjer.hr">pero@primjer.hr</a>`, `<p>Pero Perić</p><a href="mailto:pero@primjer.hr">pero@primjer.hr</a>`},
	} {
		p, err := json.Marshal(PotpisPoste{UserID: "korisnik-1", HTML: s.stigao})
		if err != nil {
			t.Fatal(err)
		}
		v := []ledger.Version{{VersionID: fmt.Sprintf("%02d-potpis", i+1), Entity: EntityPotpisi, EntityID: "korisnik-1",
			NodeID: "cop-vukovar", Payload: p, CreatedAt: time.Now(), SchemaVersion: 1}}
		if _, err := repo.rec.Apply(ctx, v); err != nil {
			t.Fatal(err)
		}
		if err := ApplyVersions(ctx, baza, repo.rec, v); err != nil {
			t.Fatal(err)
		}
		if got, err := repo.GetPotpis(ctx, "korisnik-1"); err != nil || got != s.ostaje {
			t.Errorf("potpis na površini: %q %v, očekivano %q", got, err, s.ostaje)
		}
	}
}
