package repository

import (
	"testing"
	"time"

	"gocop/internal/ledger"
)

// Razmjena po stanju ograde zna kad se pravilo čuvanja promijenilo (novi
// narastaj sa svakim postavljanjem, i istog pravila) i dokle ograda odbija
// (rub, isti kao u KeepReadingVersion; bez razdoblja nula)
func TestStanjeOgradePovijesti(t *testing.T) {
	prije := readingPolicy.Load()
	t.Cleanup(func() { readingPolicy.Store(prije) })
	readingPolicy.Store(nil)
	if n, rub := StanjeOgradePovijesti(); n != 0 || !rub.IsZero() {
		t.Errorf("bez pravila: %d %v", n, rub)
	}

	SetReadingHistoryPolicy(ReadingHistoryPolicy{})
	n1, rub := StanjeOgradePovijesti()
	if n1 == 0 || !rub.IsZero() {
		t.Errorf("bez razdoblja: %d %v", n1, rub)
	}
	SetReadingHistoryPolicy(ReadingHistoryPolicy{})
	if n2, _ := StanjeOgradePovijesti(); n2 == n1 {
		t.Errorf("isto pravilo postavljeno opet, a narastaj je ostao %d", n2)
	}

	od := time.Now().AddDate(0, -12, 0)
	SetReadingHistoryPolicy(ReadingHistoryPolicy{Months: 12, Followed: map[string]bool{"station:pracena": true}})
	n3, rub := StanjeOgradePovijesti()
	do := time.Now().AddDate(0, -12, 0)
	if n3 <= n1 || rub.Before(od) || rub.After(do) {
		t.Errorf("12 mjeseci: narastaj %d (prije %d), rub %v između %v i %v", n3, n1, rub, od, do)
	}
	ocitanje := func(stanica string, izmjereno time.Time) ledger.Version {
		return ledger.Version{Entity: EntityReadings, Payload: []byte(`{"measured_at":"` + izmjereno.Format(time.RFC3339Nano) + `","station_id":"` + stanica + `"}`)}
	}
	if KeepVersion(ocitanje("druga", rub.Add(-time.Minute))) || !KeepVersion(ocitanje("druga", rub.Add(time.Minute))) {
		t.Error("ograda ne odbija po rubu iz stanja ograde")
	}
	if !KeepVersion(ocitanje("pracena", rub.Add(-time.Minute))) {
		t.Error("praćena letva ne prolazi ispod ruba")
	}
}
