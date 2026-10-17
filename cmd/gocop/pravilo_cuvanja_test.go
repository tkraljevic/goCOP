package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/repository"
)

// Pravilo čuvanja: razdoblje za sve letve i cijela povijest praćenih; kad se
// praćene ne daju pročitati, samo razdoblje. Svako postavljanje je novi
// narastaj, pa razmjena zna da je pravilo promijenjeno (granica odbijenih).
func TestPostaviPraviloCuvanja(t *testing.T) {
	t.Cleanup(func() { repository.SetReadingHistoryPolicy(repository.ReadingHistoryPolicy{}) })
	ctx := context.Background()
	staro := func(stanica string) ledger.Version {
		return ledger.Version{Entity: repository.EntityReadings, Payload: []byte(`{"measured_at":"2000-01-01T00:00:00Z","station_id":"` + stanica + `"}`)}
	}
	praceno := func(context.Context) (map[string]bool, error) { return map[string]bool{"station:pracena": true}, nil }
	dnevnik := zapisDnevnika(t)

	n0, _ := repository.StanjeOgradePovijesti()
	postaviPraviloCuvanja(ctx, praceno, 12)
	n1, rub := repository.StanjeOgradePovijesti()
	if n1 == n0 || rub.IsZero() || repository.MjeseciPovijestiOcitanja() != 12 {
		t.Errorf("12 mjeseci: narastaj %d → %d, rub %v", n0, n1, rub)
	}
	if !repository.KeepVersion(staro("pracena")) || repository.KeepVersion(staro("druga")) {
		t.Error("praćena letva mora proći cijelu povijest, a druga samo razdoblje")
	}
	if !strings.Contains(dnevnik.String(), "zadnjih 12 mjeseci, a za 1 praćenih letvi cijela povijest") {
		t.Errorf("dnevnik: %q", dnevnik.String())
	}

	pada := func(context.Context) (map[string]bool, error) { return nil, errors.New("baza je zaključana") }
	postaviPraviloCuvanja(ctx, pada, 12)
	if n2, _ := repository.StanjeOgradePovijesti(); n2 == n1 || repository.KeepVersion(staro("pracena")) {
		t.Errorf("kad se praćene ne čitaju: narastaj %d → %d, praćena prolazi %v", n1, n2, repository.KeepVersion(staro("pracena")))
	}
	if !strings.Contains(dnevnik.String(), "praćene letve nisu pročitane: baza je zaključana") {
		t.Errorf("dnevnik: %q", dnevnik.String())
	}

	dnevnik.Reset()
	postaviPraviloCuvanja(ctx, praceno, 0)
	if _, rub := repository.StanjeOgradePovijesti(); !rub.IsZero() || !repository.KeepVersion(staro("druga")) || dnevnik.Len() != 0 {
		t.Errorf("bez razdoblja: rub %v, dnevnik %q", rub, dnevnik.String())
	}
}
