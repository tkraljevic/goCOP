package repository

import (
	"encoding/json"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Na površini dnevnika stoji najnovija ovjerena verzija, a dok dnevnik nije
// ovjeren nijednom verzijom, najnovija. Brisanje neovjerenog dnevnika ne
// skida ovjeru, a brisanje koje nosi ovjeren dnevnik vrijedi.
func TestDnevnikNaPovrsiniPrednostImaOvjera(t *testing.T) {
	verzija := func(id string, ovjeren, obrisan bool) ledger.Version {
		p := `{"id":"dn-1"}`
		if ovjeren {
			p = `{"id":"dn-1","zakljuceno_at":"2026-10-30T14:00:00Z"}`
		}
		return ledger.Version{VersionID: id, Entity: EntityJournals, EntityID: "dn-1", Archived: obrisan, Payload: json.RawMessage(p)}
	}
	for _, c := range []struct {
		opis     string
		povijest []ledger.Version // od najnovije prema najstarijoj
		ocekuje  string
	}{
		{"neovjeren dnevnik", []ledger.Version{verzija("v2", false, false), verzija("v1", false, false)}, "v2"},
		{"zaglavlje s drugog čvora poslije ovjere", []ledger.Version{verzija("v3", false, false), verzija("v2", true, false), verzija("v1", false, false)}, "v2"},
		{"novija ovjerena verzija", []ledger.Version{verzija("v3", true, false), verzija("v2", true, false)}, "v3"},
		{"brisanje neovjerenog poslije ovjere", []ledger.Version{verzija("v3", false, true), verzija("v2", true, false)}, "v2"},
		{"brisanje bez ijedne ovjere", []ledger.Version{verzija("v3", false, true), verzija("v2", false, false)}, "v3"},
		{"brisanje ovjerenog dnevnika", []ledger.Version{verzija("v3", true, true), verzija("v2", true, false)}, "v3"},
		{"obrisan ovjeren dnevnik ne vraća se", []ledger.Version{verzija("v3", false, false), verzija("v2", true, true)}, "v2"},
		{"nečitljiva verzija nije ovjera", []ledger.Version{{VersionID: "v2", Payload: json.RawMessage(`{`)}, verzija("v1", true, false)}, "v1"},
	} {
		if got := dnevnikNaPovrsini(c.povijest); got.VersionID != c.ocekuje {
			t.Errorf("%s: na površini %s, očekivano %s", c.opis, got.VersionID, c.ocekuje)
		}
	}
}

// probniDnevnikCOP je dnevnik COP-a sektora B iz oslonca noviCvor
func probniDnevnikCOP() models.Journal {
	od := time.Date(2026, 10, 20, 7, 0, 0, 0, time.UTC)
	do := od.Add(7 * 24 * time.Hour)
	return models.Journal{ID: "01a00000-0000-7000-8000-0000000d0001", CentarSektor: "B", Kind: models.JournalKindDefense,
		Title: "Dnevnik COP-a, 2026.", Year: 2026, StartedAt: &od, EndedAt: &do, CreatedAt: od, UpdatedAt: od}
}

// verzijeOvjere su tri verzije zaglavlja: otvoren dnevnik, ovjera i, poslije
// nje, izmjena s čvora koji ovjeru još nije primio
func verzijeOvjere(t *testing.T) (otvoren, ovjera, poslije ledger.Version, kad time.Time) {
	t.Helper()
	j := probniDnevnikCOP()
	otvoren = verzijaZa(t, EntityJournals, j.ID, vid(1), j)
	kad = time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC)
	o := j
	o.ZakljucioID, o.Zakljucio, o.ZakljucenoAt, o.Notes = "r-1", "Ana Anić", &kad, "ovjereno"
	ovjera = verzijaZa(t, EntityJournals, j.ID, vid(2), o)
	p := j
	p.Notes = "izmjena s čvora bez ovjere"
	poslije = verzijaZa(t, EntityJournals, j.ID, vid(3), p)
	return otvoren, ovjera, poslije, kad
}

// Izmjena zaglavlja s čvora koji ovjeru još nije primio ne skida ovjeru, bez
// obzira kojim redom verzije stignu i ni nakon obnove površine; izmjena
// ostaje u knjizi
func TestRazmjenaZaglavljaNeSkidaOvjeru(t *testing.T) {
	sNedovrsenima(t)
	otvoren, ovjera, poslije, kad := verzijeOvjere(t)
	for _, s := range []struct {
		opis   string
		paketi [][]ledger.Version
	}{
		{"ovjera pa izmjena", [][]ledger.Version{{otvoren, ovjera}, {poslije}}},
		{"izmjena pa ovjera", [][]ledger.Version{{poslije}, {otvoren}, {ovjera}}},
		{"sve odjednom", [][]ledger.Version{{poslije, ovjera, otvoren}}},
	} {
		c := noviCvor(t, "cop-proba")
		for _, paket := range s.paketi {
			nuzno(t, primiPaket(t, c, paket...))
		}
		ovjeren := func(kada string) {
			t.Helper()
			j, err := NewJournalRepository(c.db, c.rec).GetJournal(ctxRaz, otvoren.EntityID)
			if err != nil || j == nil || j.ZakljucenoAt == nil || !j.ZakljucenoAt.Equal(kad) || j.Zakljucio != "Ana Anić" || j.Notes != "ovjereno" {
				t.Errorf("%s, %s: na površini %+v, %v", s.opis, kada, j, err)
			}
		}
		ovjeren("nakon primanja")
		_, err := ReplaySurface(ctxRaz, c.db, c.rec)
		nuzno(t, err)
		ovjeren("nakon obnove površine")
		if top, err := c.rec.Latest(ctxRaz, EntityJournals, otvoren.EntityID); err != nil || top.VersionID != poslije.VersionID {
			t.Errorf("%s: izmjena nije ostala u knjizi: %+v, %v", s.opis, top, err)
		}
	}
}

// Dok dnevnik nije ovjeren, na površinu ide najnovija izmjena zaglavlja, a
// brisanje ga briše
func TestRazmjenaZaglavljaBezOvjere(t *testing.T) {
	sNedovrsenima(t)
	otvoren, _, poslije, _ := verzijeOvjere(t)

	c := noviCvor(t, "cop-proba")
	nuzno(t, primiPaket(t, c, otvoren))
	nuzno(t, primiPaket(t, c, poslije))
	j, err := NewJournalRepository(c.db, c.rec).GetJournal(ctxRaz, otvoren.EntityID)
	if err != nil || j == nil || j.Ovjeren() || j.Notes != "izmjena s čvora bez ovjere" {
		t.Errorf("neovjeren dnevnik nema najnovije zaglavlje: %+v, %v", j, err)
	}

	brisanje := poslije
	brisanje.VersionID, brisanje.Archived = vid(4), true
	nuzno(t, primiPaket(t, c, brisanje))
	if j, err := NewJournalRepository(c.db, c.rec).GetJournal(ctxRaz, otvoren.EntityID); err != nil || j != nil {
		t.Errorf("brisanje nije maknulo neovjeren dnevnik: %+v, %v", j, err)
	}
}

// Zapis koji stigne razmjenom, a upisan je nakon ovjere, ostaje u dnevniku i
// nosi oznaku. Ovjera ostaje i kad između zapisa stigne zaglavlje s istog
// čvora. Zapis iz vremena prije ovjere oznaku ne nosi.
func TestZapisNakonOvjereOstajeUDnevniku(t *testing.T) {
	sNedovrsenima(t)
	otvoren, ovjera, poslije, kad := verzijeOvjere(t)
	c := noviCvor(t, "cop-proba")
	nuzno(t, primiPaket(t, c, otvoren, ovjera))

	zapis := func(id, verzija string, upisan time.Time) ledger.Version {
		return verzijaZa(t, EntityJournalEntries, id, verzija, models.JournalEntry{ID: id, JournalID: otvoren.EntityID,
			Number: 1, Date: upisan, Kind: models.EntryKindNote, Text: "dojava", UserID: "u-2", UserName: "Iva Ivić",
			CreatedAt: upisan, UpdatedAt: upisan})
	}
	nuzno(t, primiPaket(t, c, zapis("01a00000-0000-7000-8000-0000000e0001", vid(4), kad.Add(-time.Hour))))
	nuzno(t, primiPaket(t, c, poslije))
	nuzno(t, primiPaket(t, c, zapis("01a00000-0000-7000-8000-0000000e0002", vid(5), kad.Add(time.Hour))))

	r := NewJournalRepository(c.db, c.rec)
	j, err := r.GetJournal(ctxRaz, otvoren.EntityID)
	if err != nil || j == nil || !j.Ovjeren() {
		t.Fatalf("zapis je skinuo ovjeru: %+v, %v", j, err)
	}
	zapisi, err := r.EntriesForJournal(ctxRaz, j.ID)
	nuzno(t, err)
	if len(zapisi) != 2 {
		t.Fatalf("u dnevniku je %d zapisa, a stigla su 2", len(zapisi))
	}
	oznake := map[string]bool{}
	for _, e := range zapisi {
		oznake[e.ID] = j.ZapisNakonOvjere(e)
	}
	if oznake["01a00000-0000-7000-8000-0000000e0001"] || !oznake["01a00000-0000-7000-8000-0000000e0002"] {
		t.Errorf("oznake nakon ovjere: %v", oznake)
	}
}
