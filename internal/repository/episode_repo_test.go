package repository

import (
	"context"
	"reflect"
	"testing"
	"time"

	"gocop/internal/models"
)

// Epizode letve dolaze najnovije prve, s opisom dionice uz šifru. Epizoda
// dionice koje u registru nema ne ispada s popisa, nego dolazi bez opisa.
func TestEpizodeLetveSOpisomDionice(t *testing.T) {
	o := novaOkolinaPostaja(t)
	epizode := NewEpisodeRepository(o.baza, o.rec)
	ctx := context.Background()
	dan := func(d int) time.Time { return time.Date(2026, 3, d, 6, 0, 0, 0, time.UTC) }
	for _, e := range []models.DefenseEpisode{
		{SectionCode: "P.1.1", StationID: o.primjerovo.ID.String(), StartedAt: dan(1), Phase: models.PhasePrep},
		{SectionCode: "P.1.2", StationID: o.primjerovo.ID.String(), StartedAt: dan(5), Phase: models.PhaseRegular},
		{SectionCode: "P.1.1", StationID: o.probno.ID.String(), StartedAt: dan(3), Phase: models.PhasePrep},
	} {
		nuzno(t, epizode.SaveEpisode(ctx, &e))
	}
	// dionica X.9.9 ne postoji, pa se epizoda upisuje mimo provjere veza
	conn, err := o.baza.Conn(ctx)
	nuzno(t, err)
	upis := `INSERT INTO defense_episodes (id, section_code, station_id, started_at, phase, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`
	for _, q := range []struct {
		upit string
		args []any
	}{
		{upit: `PRAGMA foreign_keys = OFF`},
		{upit: upis, args: []any{"0192f000-0000-7000-8000-000000000001", "X.9.9", o.primjerovo.ID.String(), dan(7), "IZVANREDNA", dan(7), dan(7)}},
		{upit: upis, args: []any{"nije-uuid", "P.1.1", "pokvarena", dan(2), "PRIPREMNO", dan(2), dan(2)}},
		{upit: `PRAGMA foreign_keys = ON`},
	} {
		_, err := conn.ExecContext(ctx, q.upit, q.args...)
		nuzno(t, err)
	}
	nuzno(t, conn.Close())

	type red struct{ sifra, opis string }
	procitaj := func(ctx context.Context, letva string, limit int) ([]red, error) {
		e, err := epizode.EpisodesByStation(ctx, letva, limit)
		var out []red
		for _, x := range e {
			out = append(out, red{x.SectionCode, x.SectionName})
		}
		return out, err
	}
	for _, slucaj := range []struct {
		naziv, letva string
		limit        int
		ocekivano    []red
	}{
		{naziv: "sve epizode letve", letva: o.primjerovo.ID.String(),
			ocekivano: []red{{"X.9.9", ""}, {"P.1.2", "druga"}, {"P.1.1", "prva"}}},
		{naziv: "samo najnovije", letva: o.primjerovo.ID.String(), limit: 2,
			ocekivano: []red{{"X.9.9", ""}, {"P.1.2", "druga"}}},
		{naziv: "druga letva iste dionice", letva: o.probno.ID.String(), limit: 10,
			ocekivano: []red{{"P.1.1", "prva"}}},
		{naziv: "letva bez obrana", letva: o.kanalska.ID.String()},
	} {
		dobiveno, err := procitaj(ctx, slucaj.letva, slucaj.limit)
		if err != nil || !reflect.DeepEqual(dobiveno, slucaj.ocekivano) {
			t.Errorf("%s: %v %v, očekivano %v", slucaj.naziv, dobiveno, err, slucaj.ocekivano)
		}
	}
	if e, err := procitaj(ctx, "pokvarena", 0); err == nil {
		t.Errorf("epizoda s neispravnim identifikatorom pročitana: %v", e)
	}
	if e, err := procitaj(otkazan(), o.primjerovo.ID.String(), 0); err == nil {
		t.Errorf("epizode pročitane uz prekinut zahtjev: %v", e)
	}
}
