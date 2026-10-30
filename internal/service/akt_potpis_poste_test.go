package service_test

import (
	"context"
	"testing"

	"gocop/internal/repository"
)

// Potpis e-pošte čisti se pri spremanju i pri čitanju: potpis spremljen u
// ranijem izdanju, sa slabijim čišćenjem, ne vraća se s HTML-om izvan
// popisa dopuštenih oznaka.
func TestPotpisPosteCistiSePriSpremanjuICitanju(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	id := o.rukovod.ID.String()
	ulaz := `<b>Pero</b><img src="x"/onerror="y()">`
	if err := o.akti.SpremiPotpis(ctx, o.rukovod, ulaz); err != nil {
		t.Fatal(err)
	}
	if h, err := repoAkata(o).GetPotpis(ctx, id); err != nil || h != "<b>Pero</b>" {
		t.Errorf("spremljeni potpis: %q %v", h, err)
	}
	if err := repoAkata(o).SavePotpis(ctx, &repository.PotpisPoste{UserID: id, HTML: ulaz}); err != nil {
		t.Fatal(err)
	}
	if h := o.akti.Potpis(ctx, id); h != "<b>Pero</b>" {
		t.Errorf("pročitani potpis: %q", h)
	}
	if err := o.akti.SpremiPotpis(ctx, nil, ulaz); err == nil {
		t.Error("potpis bez osobe je spremljen")
	}
}
