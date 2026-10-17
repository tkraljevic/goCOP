package peers

// Zapis koji stariji program još ne razumije, od kraja do kraja (G5 u
// docs/plan-web-sync.md §3; Odluke (11. 10. 2026.), 1): Unraid s novijim
// programom upiše blagdan novije sheme, Windows sa starijim ga primi,
// sačuva i proslijedi laptopu, a nakon nadogradnje ga primijeni. Koliko
// kod njega čeka, Windows javlja u granici, pa laptop to zna i pokazuje na
// pločici. Čvorovi ne slušaju na svojim portovima: razgovori idu izravno
// na loopbacku (razgovaraj), bez čekanja.
//
// Noviji program glumi se novijom shemom stvarnog entiteta
// (ledger.PostaviShemu), a ne nepoznatim entitetom: popis poznatih
// (ledger.PoznatiEntiteti) se ne da vratiti, pa bi zatrovao ostale testove
// paketa.

import (
	"context"
	"errors"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/obracun"
	"gocop/internal/repository"
)

// sPovrsinom: primljeno ide i na površinu, kao u programu
func sPovrsinom(s *Service) {
	s.OnApplied(func(ctx context.Context, vv []ledger.Version) error {
		return repository.ApplyVersions(ctx, s.db, s.rec, vv)
	})
}

// cekaKodDrugog: pozivatelj nazove pozvanoga, zabilježi razgovor kao
// SyncWith i vrati koliko zapisa po njegovoj ploči čeka kod pozvanoga
func cekaKodDrugog(t *testing.T, pozivatelj, pozvani *Service) SyncState {
	t.Helper()
	p, _ := razgovaraj(t, pozivatelj, pozvani)
	peer, err := pozivatelj.GetPeer(ctxRub, pozvani.node.ID)
	if err != nil || peer == nil {
		t.Fatalf("%s ne zna za %s: %v", pozivatelj.node.ID, pozvani.node.ID, err)
	}
	pozivatelj.noteSync(ctxRub, peer, nil, ishodRazmjene(p.applied, p.sent, p.theirs, 0, nil))
	st, err := pozivatelj.Status(ctxRub, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, ps := range st.Peers {
		if ps.NodeID == pozvani.node.ID {
			return ps.State
		}
	}
	t.Fatalf("%s nema %s na ploči", pozivatelj.node.ID, pozvani.node.ID)
	return SyncState{}
}

func TestZapisCekaNadogradnjuOdKrajaDoKraja(t *testing.T) {
	cv := clanoviMreze(t, false, "cop-osijek", "windows-ured", "laptop-baranja")
	unraid, windows, laptop := cv[0], cv[1], cv[2]
	for _, s := range cv {
		s.SetWantsAll(true)
		sPovrsinom(s)
	}
	izjednaci(t, unraid, windows)
	izjednaci(t, windows, laptop)

	// noviji program na Unraidu piše blagdane shemom 2, koju stariji ne smije
	// uređivati; Windows i laptop imaju stariji program
	t.Cleanup(func() { ledger.PostaviShemu(repository.EntityBlagdani, ledger.SchemaVersion) })
	ledger.PostaviShemu(repository.EntityBlagdani, 2)
	dan := obracun.Pravilo{ID: "dan-obrane", Naziv: "Dan obrane", Vrsta: obracun.Stalni, Mjesec: 8, Dan: 5}
	if err := repository.NewObracunRepository(unraid.db, unraid.rec).SaveBlagdan(ctxRub, dan); err != nil {
		t.Fatal(err)
	}
	ledger.PostaviShemu(repository.EntityBlagdani, ledger.SchemaVersion)

	// Windows ga primi i sačuva, ali ga ne smije uređivati
	razgovaraj(t, windows, unraid)
	if n, err := windows.rec.CekaNadogradnju(ctxRub); err != nil || n != 1 {
		t.Fatalf("na Windowsu čeka %d zapisa (%v), očekivan 1", n, err)
	}
	izmjena := dan
	izmjena.Naziv = "Dan obrane (izmjena)"
	if err := repository.NewObracunRepository(windows.db, windows.rec).SaveBlagdan(ctxRub, izmjena); !errors.Is(err, ledger.ErrNovijaShema) {
		t.Fatalf("stariji program uređuje zapis novije sheme: %v", err)
	}

	// Windows ga proslijedi laptopu i javi koliko kod njega čeka
	if st := cekaKodDrugog(t, laptop, windows); st.CekaNadogradnju != 1 || !st.MogucnostiPoznate {
		t.Fatalf("laptop o Windowsu: čeka %d, mogućnosti poznate %v", st.CekaNadogradnju, st.MogucnostiPoznate)
	}
	if v, err := laptop.rec.Latest(ctxRub, repository.EntityBlagdani, dan.ID); err != nil || v == nil || v.NodeID != unraid.node.ID || v.SchemaVersion != 2 {
		t.Fatalf("laptop nema Unraidov zapis novije sheme: %+v %v", v, err)
	}
	if n, err := laptop.rec.CekaNadogradnju(ctxRub); err != nil || n != 1 {
		t.Fatalf("na laptopu čeka %d zapisa (%v), očekivan 1", n, err)
	}

	// nadogradnja Windowsa: novi program nad istom bazom, kao ponovno pokretanje
	ledger.PostaviShemu(repository.EntityBlagdani, 2)
	rec := ledger.New(windows.db, windows.node.ID)
	nadograden, err := NewService(windows.db, rec, windows.node, windows.ports)
	if err != nil {
		t.Fatal(err)
	}
	nadograden.SetWantsAll(true)
	sPovrsinom(nadograden)
	if err := rec.UcitajNovosti(ctxRub); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ReplaySurface(ctxRub, windows.db, rec); err != nil {
		t.Fatal(err)
	}
	if n, err := rec.CekaNadogradnju(ctxRub); err != nil || n != 0 {
		t.Fatalf("nakon nadogradnje na Windowsu čeka %d zapisa (%v)", n, err)
	}
	if err := repository.NewObracunRepository(windows.db, rec).SaveBlagdan(ctxRub, izmjena); err != nil {
		t.Fatalf("nadograđeni program ne uređuje zapis: %v", err)
	}

	// laptop više nema što pokazati uz Windows, a izmjena mu stigne
	if st := cekaKodDrugog(t, laptop, nadograden); st.CekaNadogradnju != 0 {
		t.Errorf("laptop o nadograđenom Windowsu: čeka %d", st.CekaNadogradnju)
	}
	ps, err := repository.NewObracunRepository(laptop.db, laptop.rec).Blagdani(ctxRub)
	if err != nil || len(ps) != 1 || ps[0].Naziv != izmjena.Naziv {
		t.Errorf("blagdani na laptopu: %+v %v", ps, err)
	}
}
