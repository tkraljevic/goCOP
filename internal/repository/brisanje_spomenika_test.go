package repository

import (
	"errors"
	"testing"
	"time"

	"gocop/internal/ledger"
)

// Spomenik koji je zadnja verzija zapisa ne briše se s oglasne ploče: bez
// njega bi zadnja verzija bila živa i obnova bi obrisano dežurstvo ili
// očitanje vratila na površinu, u plan i u obračun sati.
func TestBrisanjeSpomenikaNeVracaObrisaniZapis(t *testing.T) {
	sNedovrsenima(t)
	sPolitikomPovijesti(t, ReadingHistoryPolicy{})
	c := noviCvor(t, "cop-proba")
	d := dezurstvoNa(t, c)
	nuzno(t, NewJournalRepository(c.db, c.rec).ArhivirajDezurstvo(ctxRaz, d))
	id := ocitanjeNa(t, c, time.Now().UTC().Add(-time.Hour), 160)
	nuzno(t, NewReadingRepository(c.db, c.rec).Delete(ctxRaz, id))

	for _, z := range []struct{ entitet, id string }{{EntityDezurstva, d.ID}, {EntityReadings, id.String()}} {
		spomenik, err := c.rec.Latest(ctxRaz, z.entitet, z.id)
		nuzno(t, err)
		if err := c.rec.DeleteVersion(ctxRaz, spomenik.VersionID); !errors.Is(err, ledger.ErrZapisOBrisanju) {
			t.Errorf("%s: spomenik je obrisan iz knjige (%v)", z.entitet, err)
		}
	}
	o, err := ObnoviPovrsinu(ctxRaz, c.db, c.rec)
	nuzno(t, err)
	if !praznaObnova(o) {
		t.Errorf("obnova je vratila obrisano: %s", o)
	}
	if imaRedak(t, c, "dezurstva", d.ID) || imaRedak(t, c, "readings", id.String()) {
		t.Error("obrisano dežurstvo ili očitanje opet je na površini")
	}
}
