package db

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
)

// Zid (Naslovna, Događanja) traži najnovije verzije nekoliko entiteta.
// Kazalo po entitetu i verziji daje isti ishod kao poredak cijele knjige, a
// upit ga koristi, pa ne slaže sve verzije očitanja da bi uzeo vrh.
func TestZidCitaKazalomEntitetaIVerzije(t *testing.T) {
	database := praznaBaza(t)
	rec := ledger.New(database, "cvor-a")
	pocetak := time.Date(2026, 4, 1, 6, 0, 0, 0, time.UTC)
	entiteti := []string{"readings", "stations", "journal_entries", "users"}
	var vv []ledger.Version
	for i := range 60 {
		// očitanja su gusta, ostali entiteti rijetki i razbacani
		e := entiteti[0]
		if i%7 == 3 {
			e = entiteti[1+i%3]
		}
		vv = append(vv, ledger.Version{
			VersionID: fmt.Sprintf("01960000-0000-7000-8000-%012d", i+1), Entity: e,
			EntityID: fmt.Sprintf("z-%d", i), NodeID: "cvor-a", SchemaVersion: 1,
			Payload: json.RawMessage(`{"n":1}`), CreatedAt: pocetak.Add(time.Duration(i) * time.Hour),
		})
	}
	if n, err := rec.Apply(context.Background(), vv); err != nil || n != len(vv) {
		t.Fatalf("primljeno %d: %v", n, err)
	}

	ocekivano := func(trazeni []string, prije string, od time.Time, limit int) string {
		var ids []string
		for _, v := range slices.Backward(vv) {
			if slices.Contains(trazeni, v.Entity) && (prije == "" || v.VersionID < prije) && (od.IsZero() || !v.CreatedAt.Before(od)) {
				ids = append(ids, v.VersionID)
			}
		}
		return strings.Join(ids[:min(limit, len(ids))], ",")
	}
	slucajevi := []struct {
		trazeni []string
		prije   string
		od      time.Time
		limit   int
	}{
		{entiteti, "", time.Time{}, 10},
		{entiteti[1:], "", time.Time{}, 5},
		{entiteti, vv[40].VersionID, time.Time{}, 7},
		{[]string{"stations", "users"}, "", pocetak.Add(20 * time.Hour), 50},
	}
	for _, s := range slucajevi {
		dobiveno, err := rec.Recent(context.Background(), s.trazeni, s.prije, s.od, time.Time{}, s.limit)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, v := range dobiveno {
			ids = append(ids, v.VersionID)
		}
		if got, want := strings.Join(ids, ","), ocekivano(s.trazeni, s.prije, s.od, s.limit); got != want {
			t.Errorf("%v prije %q: %s, želim %s", s.trazeni, s.prije, got, want)
		}
	}

	r, err := database.Query(`EXPLAIN QUERY PLAN SELECT version_id, payload FROM record_versions
		WHERE entity IN ('readings', 'stations') AND version_id < 'z' ORDER BY version_id DESC LIMIT 200`)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var plan []string
	for r.Next() {
		var id, roditelj, nista int
		var korak string
		if err := r.Scan(&id, &roditelj, &nista, &korak); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, korak)
	}
	if !strings.Contains(strings.Join(plan, "\n"), "USING INDEX idx_versions_entitet_verzija (entity=? AND version_id<?)") {
		t.Errorf("zid ne čita kazalom entiteta i verzije:\n%s", strings.Join(plan, "\n"))
	}
}
