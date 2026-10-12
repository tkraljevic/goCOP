package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"gocop/internal/models"
)

// Obrazac lista otvoren prije ponoći, a prvi put spremljen poslije nje,
// sprema se na svoj dan i upisano se ne gubi (Odluke (11. 10. 2026.)).
// Bez potvrđenog trenutka otvaranja, ili s otvaranjem na drugi dan, list
// za dan koji je prošao i dalje se ne otvara (pitanje 6). Podaci su
// izmišljeni.
func TestListOtvorenPrijePonociSpremaSeNaSvojDan(t *testing.T) {
	ctx := context.Background()
	jucer := vdDan(time.March, 10)
	poslijePonoci := jucer.AddDate(0, 0, 1).Add(10 * time.Minute)
	for _, c := range []struct {
		ime      string
		otvoreno time.Time
		sprema   bool
	}{
		{"otvoren u 23:50 istog dana", jucer.Add(23*time.Hour + 50*time.Minute), true},
		{"bez potvrđenog otvaranja", time.Time{}, false},
		{"otvoren dan prije", jucer.Add(-time.Hour), false},
		{"otvoren poslije ponoći", poslijePonoci.Add(-time.Minute), false},
	} {
		o := novaOkolinaVodocuvara(t)
		o.vs.sad = func() time.Time { return poslijePonoci }
		u := vdVodocuvar()
		unos := vdUnos("obilazak nasipa do mraka")
		unos.Otvoreno = c.otvoreno
		l, err := o.vs.Spremi(ctx, u, jucer, unos, false)
		if !c.sprema {
			if !errors.Is(err, ErrProsliDan) {
				t.Errorf("%s: %v, želim ErrProsliDan", c.ime, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", c.ime, err)
		}
		if !istiDan(l.Datum, jucer) || l.Opis != "obilazak nasipa do mraka" || l.Broj != 1 || l.Predan() {
			t.Errorf("%s: list %s, opis %q, broj %d, predan %v", c.ime, l.Datum.In(models.Zagreb).Format("02.01."), l.Opis, l.Broj, l.Predan())
		}
		// list sada postoji, pa se dalje sprema i predaje kao i svaki drugi
		unos.Otvoreno = time.Time{}
		if _, err := o.vs.Spremi(ctx, u, jucer, unos, false); err != nil {
			t.Errorf("%s: drugo spremanje: %v", c.ime, err)
		}
	}
}
