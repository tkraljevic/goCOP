package service

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Izmjena produljuje dužnost kad joj daje dulji vijek nego što ga je imala
// (Odluke (30. 10. 2026.), 16)
func TestProduljuje(t *testing.T) {
	sad := time.Now()
	ranije, kasnije := sad.Add(time.Hour), sad.Add(48*time.Hour)
	izvor := uuid.New()
	for _, x := range []struct {
		opis        string
		nova, dosad models.Duty
		da          bool
	}{
		{"stalna ostaje stalna", models.Duty{}, models.Duty{}, false},
		{"privremena postaje stalna", models.Duty{}, models.Duty{IsTemporary: true}, true},
		{"rok se skraćuje", models.Duty{IsTemporary: true, ExpiresAt: &ranije}, models.Duty{IsTemporary: true, ExpiresAt: &kasnije}, false},
		{"rok se produljuje", models.Duty{IsTemporary: true, ExpiresAt: &kasnije}, models.Duty{IsTemporary: true, ExpiresAt: &ranije}, true},
		{"gubi istek s obranom", models.Duty{IsTemporary: true}, models.Duty{IsTemporary: true, IsticeSObranom: true}, true},
		{"gubi dužnost o kojoj ovisi", models.Duty{IsTemporary: true}, models.Duty{IsTemporary: true, OvisiO: &izvor}, true},
		{"zadržava istek s obranom", models.Duty{IsTemporary: true, IsticeSObranom: true}, models.Duty{IsTemporary: true, IsticeSObranom: true}, false},
	} {
		if got := produljuje(x.nova, x.dosad); got != x.da {
			t.Errorf("%s: %v, želi %v", x.opis, got, x.da)
		}
	}
}
