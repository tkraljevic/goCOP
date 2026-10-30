package web

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Kroz rute dnevnika COP-a: storno u adresi jednog dnevnika ne dira zapis
// drugog dnevnika, ovjeren dnevnik ne prima zapis ni storno i ne nudi ih na
// stranici, a list se u dnevniku COP-a ne otvara. Dežurni operater u
// neovjerenom dnevniku i dalje upisuje i stornira. Podaci su izmišljeni.
func TestUpisiUDnevnikCOPaKrozRute(t *testing.T) {
	p := novaCOPProba(t)
	p.mux.HandleFunc("POST /dnevnici/{id}/upisi/{entry}/storno", p.h.HandleVoidEntry)
	ctx := context.Background()
	operater := p.osoba(t, "ddezurni", "Dora Dežurna", "", models.Duty{Title: "Dežurni operater", Role: models.RoleOperator, ScopeType: models.ScopeSector})
	put := "/dnevnici/" + p.cop.ID
	zapis := func(tekst string) url.Values {
		return url.Values{"date": {"2026-10-02"}, "kind": {models.EntryKindReport}, "text": {tekst}}
	}

	// drugi dnevnik COP-a istog sektora, sa svojim zapisom
	pocetak := time.Date(2026, 10, 1, 0, 0, 0, 0, models.Zagreb)
	drugi := &models.Journal{Kind: models.JournalKindDefense, CentarSektor: "P", Title: "Drugi dnevnik", Year: 2026, StartedAt: &pocetak}
	if err := p.journalRepo.SaveJournal(ctx, drugi); err != nil {
		t.Fatal(err)
	}
	if got := ishod(p.salji(t, operater, http.MethodPost, "/dnevnici/"+drugi.ID+"/zapisi", zapis("zapis drugog dnevnika"))); got != "success" {
		t.Fatalf("zapis u drugi dnevnik: %s", got)
	}
	zapisi, err := p.journalRepo.EntriesForJournal(ctx, drugi.ID)
	if err != nil || len(zapisi) != 1 {
		t.Fatalf("zapisi drugog dnevnika: %d, %v", len(zapisi), err)
	}
	tudji := zapisi[0].ID
	if got := ishod(p.salji(t, operater, http.MethodPost, put+"/upisi/"+tudji+"/storno", url.Values{"reason": {"razlog"}})); got != "error" {
		t.Errorf("storno zapisa drugog dnevnika: %s", got)
	}
	if e, err := p.journalRepo.GetEntry(ctx, tudji); err != nil || e == nil || e.Voided {
		t.Error("zapis drugog dnevnika je storniran")
	}

	// list u dnevniku COP-a
	if got := ishod(p.salji(t, operater, http.MethodPost, put+"/listovi", url.Values{"date": {"2020-01-01"}})); got != "error" {
		t.Errorf("list u dnevniku COP-a: %s", got)
	}
	if listovi, err := p.journalRepo.ListSheets(ctx, p.cop.ID); err != nil || len(listovi) != 0 {
		t.Errorf("dnevnik COP-a ima %d listova", len(listovi))
	}

	// kontrola: zapis i storno u svom, neovjerenom dnevniku
	if got := ishod(p.salji(t, operater, http.MethodPost, put+"/zapisi", zapis("zapis prije ovjere"))); got != "success" {
		t.Fatalf("zapis u dnevnik: %s", got)
	}
	stranica := p.salji(t, operater, http.MethodGet, put, nil).Body.String()
	m := regexp.MustCompile(`/upisi/([^/]+)/storno`).FindStringSubmatch(stranica)
	if m == nil || !strings.Contains(stranica, `id="novi-zapis"`) {
		t.Fatal("neovjeren dnevnik ne nudi zapis ni storno")
	}

	// ovjera: dnevnik više ne prima zapis ni storno
	if _, err := p.baza.Exec(`UPDATE journals SET ended_at = ?, zakljucio_id = ?, zakljucio = 'Vid Voditelj', zakljuceno_at = ? WHERE id = ?`,
		pocetak.AddDate(0, 0, 5).UTC(), operater.ID.String(), time.Now().UTC(), p.cop.ID); err != nil {
		t.Fatal(err)
	}
	if got := ishod(p.salji(t, operater, http.MethodPost, put+"/zapisi", zapis("naknadni zapis"))); got != "error" {
		t.Errorf("ovjeren dnevnik primio zapis: %s", got)
	}
	if got := ishod(p.salji(t, operater, http.MethodPost, put+"/upisi/"+m[1]+"/storno", url.Values{"reason": {"razlog"}})); got != "error" {
		t.Errorf("storno u ovjerenom dnevniku: %s", got)
	}
	stranica = p.salji(t, operater, http.MethodGet, put, nil).Body.String()
	if strings.Contains(stranica, `id="novi-zapis"`) || strings.Contains(stranica, "/storno") || strings.Contains(stranica, "naknadni zapis") {
		t.Error("ovjeren dnevnik nudi zapis ili storno")
	}
}
