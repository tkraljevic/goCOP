package web

import (
	"context"
	"testing"

	"gocop/internal/models"
	"gocop/internal/potpis"
)

// Sken potpisa ide u izvornik samo za osobu koja upravo potpisuje, svojim
// očima i pravim ključem ili bez ključa; tuđim očima i uz simulirani ključ
// izvornik ga ne dobiva.
func TestOtisakPotpisnika(t *testing.T) {
	sken := &models.PotpisSlika{UserID: "u1", Mime: "image/png", Slika: []byte("png")}
	h := &VodocuvarHandler{}
	ctx := context.Background()
	if o := h.otisakPotpisnika(ctx, "u1", nil); len(o) != 0 {
		t.Errorf("bez izvora skenova: %v", o)
	}
	h.SetPotpisSlika(func(context.Context, string) *models.PotpisSlika { return sken })
	tudjim := context.WithValue(ctx, contextKeyViewing, true)
	for _, x := range []struct {
		opis string
		ctx  context.Context
		id   string
		p    *potpis.Potpisnik
		ima  bool
	}{
		{"svojim očima bez ključa", ctx, "u1", nil, true},
		{"svojim očima s ključem", ctx, "u1", &potpis.Potpisnik{}, true},
		{"bez osobe", ctx, "", nil, false},
		{"simulirani ključ", ctx, "u1", &potpis.Potpisnik{Simulacija: true}, false},
		{"tuđim očima", tudjim, "u1", nil, false},
	} {
		o := h.otisakPotpisnika(x.ctx, x.id, x.p)
		if got := o[x.id] != nil; got != x.ima || len(o) > 1 {
			t.Errorf("%s: %v", x.opis, o)
		}
	}
}
