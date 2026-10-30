package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Na površini akta stoji najnovija ovjerena verzija, a dok akt nije ovjeren
// nijednom verzijom, najnovija: nacrt i brisanje nacrta s čvora koji ovjeru
// još nije primio ne vraćaju ovjeren akt u nacrt (Odluke (8. 10. 2026.),
// pitanje 4)
func TestAktNaPovrsiniPrednostImaOvjera(t *testing.T) {
	verzija := func(id, status string, obrisan bool) ledger.Version {
		return ledger.Version{VersionID: id, Entity: EntityAkti, EntityID: "akt-1", Archived: obrisan,
			Payload: json.RawMessage(`{"id":"akt-1","status":"` + status + `"}`)}
	}
	for _, c := range []struct {
		opis     string
		povijest []ledger.Version // od najnovije prema najstarijoj
		ocekuje  string
	}{
		{"samo nacrti", []ledger.Version{verzija("v2", models.AktNacrt, false), verzija("v1", models.AktNacrt, false)}, "v2"},
		{"obrisan nacrt", []ledger.Version{verzija("v2", models.AktNacrt, true), verzija("v1", models.AktNacrt, false)}, "v2"},
		{"nacrt s drugog čvora poslije ovjere", []ledger.Version{verzija("v3", models.AktNacrt, false), verzija("v2", models.AktOvjeren, false), verzija("v1", models.AktNacrt, false)}, "v2"},
		{"brisanje nacrta poslije ovjere", []ledger.Version{verzija("v3", models.AktNacrt, true), verzija("v2", models.AktOvjeren, false), verzija("v1", models.AktNacrt, false)}, "v2"},
		{"izmjena ovjerenog akta", []ledger.Version{verzija("v3", models.AktOvjeren, false), verzija("v2", models.AktOvjeren, false), verzija("v1", models.AktNacrt, false)}, "v3"},
		{"trajno obrisan ovjeren akt", []ledger.Version{verzija("v4", models.AktNacrt, false), verzija("v3", models.AktOvjeren, true), verzija("v2", models.AktOvjeren, false)}, "v3"},
		{"nečitljiva verzija nije ovjera", []ledger.Version{{VersionID: "v2", Payload: json.RawMessage(`{`)}, verzija("v1", models.AktOvjeren, false)}, "v1"},
	} {
		if got := aktNaPovrsini(c.povijest); got.VersionID != c.ocekuje {
			t.Errorf("%s: na površini %s, očekivano %s", c.opis, got.VersionID, c.ocekuje)
		}
	}
}

// Razmjena: ovjera s drugog čvora dođe na površinu i kad je u knjizi
// poslije nje nacrt, a nacrt koji stigne poslije ovjere ne skida je; za
// ostale zapise starija verzija ne vraća površinu unatrag
func TestPrimjenaAktaOvjeraPrijeNacrta(t *testing.T) {
	baza, repo := bazaAkata(t)
	rec := repo.rec
	ctx := context.Background()
	a := &models.Akt{ID: "akt-1", Sektor: "B", AreaID: 34, Godina: 2026, Radnja: models.AktUspostava, Stupanj: models.PhasePrep,
		Status: models.AktNacrt, Vrijedi: time.Date(2026, 10, 6, 3, 0, 0, 0, models.Zagreb)}
	verzija := func(id string, a models.Akt, obrisan bool) ledger.Version {
		p, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		return ledger.Version{VersionID: id, Entity: EntityAkti, EntityID: a.ID, NodeID: "cop-vukovar", Archived: obrisan,
			Payload: p, CreatedAt: time.Now(), SchemaVersion: 1}
	}
	ovjerena := *a
	ovjerena.Status, ovjerena.Broj, ovjerena.Potpis = models.AktOvjeren, 1, "potpis B-1/2026"
	nacrtPoslije := *a
	nacrtPoslije.Napomena = "nacrt s čvora koji ovjeru još nije primio"
	primi := func(v ...ledger.Version) {
		t.Helper()
		if _, err := rec.Apply(ctx, v); err != nil {
			t.Fatal(err)
		}
		if err := ApplyVersions(ctx, baza, rec, v); err != nil {
			t.Fatal(err)
		}
	}
	naPovrsini := func(opis string) {
		t.Helper()
		x, err := repo.GetAkt(ctx, a.ID)
		if err != nil || x == nil || !x.Ovjeren() || x.Broj != 1 || x.Napomena != "" {
			t.Errorf("%s: na površini %+v %v", opis, x, err)
		}
	}

	// u knjizi su nacrt i nacrt poslije ovjere, a ovjera stiže zadnja
	primi(verzija(vid(1), *a, false), verzija(vid(3), nacrtPoslije, false))
	if x, _ := repo.GetAkt(ctx, a.ID); x == nil || x.Napomena == "" {
		t.Fatalf("dok ovjere nema, na površini je najnoviji nacrt: %+v", x)
	}
	primi(verzija(vid(2), ovjerena, false))
	naPovrsini("ovjera stigla poslije novijeg nacrta")
	primi(verzija(vid(4), nacrtPoslije, true))
	naPovrsini("brisanje nacrta poslije ovjere")
	if _, err := ReplaySurface(ctx, baza, rec); err != nil {
		t.Fatal(err)
	}
	naPovrsini("obnova površine iz knjige")

	// ostali zapisi: na površinu ide samo najnovija verzija
	akt := verzija(vid(1), *a, false)
	if v, primijeni, err := verzijaZaPovrsinu(ctx, rec, akt); err != nil || !primijeni || v.VersionID != vid(2) {
		t.Errorf("akt: %s %v %v", v.VersionID, primijeni, err)
	}
	sluzba := func(id string) ledger.Version {
		return ledger.Version{VersionID: id, Entity: EntitySluzbe, EntityID: "sluzba-1", Payload: json.RawMessage(`{}`), CreatedAt: time.Now(), SchemaVersion: 1}
	}
	if _, err := rec.Apply(ctx, []ledger.Version{sluzba(vid(11)), sluzba(vid(12))}); err != nil {
		t.Fatal(err)
	}
	if v, primijeni, err := verzijaZaPovrsinu(ctx, rec, sluzba(vid(12))); err != nil || !primijeni || v.VersionID != vid(12) {
		t.Errorf("najnovija verzija: %s %v %v", v.VersionID, primijeni, err)
	}
	if _, primijeni, err := verzijaZaPovrsinu(ctx, rec, sluzba(vid(11))); err != nil || primijeni {
		t.Errorf("starija verzija ne ide na površinu: %v %v", primijeni, err)
	}

	// zapis bez verzije i knjiga koja ne odgovara
	nema := akt
	nema.EntityID = "nema-ga"
	if _, _, err := verzijaZaPovrsinu(ctx, rec, nema); !errors.Is(err, ledger.ErrNoVersion) {
		t.Errorf("akt bez verzije: %v", err)
	}
	otkazan, prekini := context.WithCancel(ctx)
	prekini()
	for _, v := range []ledger.Version{akt, sluzba(vid(12))} {
		if _, primijeni, err := verzijaZaPovrsinu(otkazan, rec, v); err == nil || primijeni {
			t.Errorf("%s uz prekinut zahtjev: %v %v", v.Entity, primijeni, err)
		}
	}
}
