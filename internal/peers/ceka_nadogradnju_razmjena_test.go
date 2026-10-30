package peers

// Zapis koji stariji program još ne razumije, od kraja do kraja (G5 u
// planu WEB synca, §3; Odluke (11. 10. 2026.), 1): server s novijim
// programom upiše blagdan, Windows sa starijim ga primi, sačuva i
// proslijedi laptopu. Koliko kod njega čeka, Windows javlja u granici, pa
// laptop to zna. Pločica na naslovnoj (upozorenjaNadogradnje u cmd/gocop)
// čita upravo ta dva broja: Recorder.CekaNadogradnju za ovo računalo i
// SyncState.CekaNadogradnju za druga; njezine rečenice provjeravaju testovi
// u cmd/gocop. Čvorovi ne slušaju na svojim portovima: razgovori idu
// izravno na loopbacku (razgovaraj), bez čekanja.
//
// Dvije vrste zapisa:
//   - novija shema poznatog entiteta (ledger.PostaviShemu): stariji program
//     zapis odmah prikazuje, ali ga ne da mijenjati; nakon nadogradnje ga
//     smije mijenjati;
//   - entitet koji stariji program nema: zapis je samo u knjizi, a nakon
//     nadogradnje ga program primijeni iz knjige (ReplaySurface).

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
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

// stariProgram: primljeno ide na površinu kao u programu koji nema sve
// entitete. Verzija entiteta izvan popisa poznatih (ledger.Katalog) ostaje
// samo u knjizi, kao u applyOne za entitet bez primjene.
func stariProgram(s *Service) {
	s.OnApplied(func(ctx context.Context, vv []ledger.Version) error {
		poznati := ledger.Katalog()
		zna := slices.DeleteFunc(slices.Clone(vv), func(v ledger.Version) bool {
			_, ima := poznati[v.Entity]
			return poznati != nil && !ima
		})
		return repository.ApplyVersions(ctx, s.db, s.rec, zna)
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
	pozivatelj.noteSync(ctxRub, peer, ishodRazmjene(p.applied, p.sent, p.theirs, 0, nil))
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

// nadogradi: novi program nad istom bazom, kao ponovno pokretanje u
// main.go: pregled knjige (UcitajNovosti) i obnova površine iz knjige
// (ReplaySurface). Što novi program zna, pozivatelj zada prije.
func nadogradi(t *testing.T, s *Service) (*Service, *ledger.Recorder) {
	t.Helper()
	rec := ledger.New(s.db, s.node.ID)
	nadograden, err := NewService(s.db, rec, s.node, s.ports)
	if err != nil {
		t.Fatal(err)
	}
	nadograden.SetWantsAll(true)
	sPovrsinom(nadograden)
	if err := rec.UcitajNovosti(ctxRub); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ReplaySurface(ctxRub, s.db, rec); err != nil {
		t.Fatal(err)
	}
	return nadograden, rec
}

// cekaIPovrsina: na računalu čeka koliko je zadano, a na površini su
// blagdani s tim nazivima
func cekaIPovrsina(t *testing.T, s *Service, rec *ledger.Recorder, ceka int, nazivi ...string) {
	t.Helper()
	if n, err := rec.CekaNadogradnju(ctxRub); err != nil || n != ceka {
		t.Errorf("%s: čeka %d zapisa (%v), očekivano %d", s.node.ID, n, err, ceka)
	}
	ps, err := repository.NewObracunRepository(s.db, rec).Blagdani(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	var na []string
	for _, p := range ps {
		na = append(na, p.Naziv)
	}
	if !slices.Equal(na, nazivi) {
		t.Errorf("%s: blagdani na površini %q, očekivano %q", s.node.ID, na, nazivi)
	}
}

// danObrane je blagdan koji upiše server s novijim programom
var danObrane = obracun.Pravilo{ID: "dan-obrane", Naziv: "Dan obrane", Vrsta: obracun.Stalni, Mjesec: 8, Dan: 5}

// serverWindowsLaptop su server, Windows i laptop iste mreže koji prate
// sve i već su razmijenili ono što postava upiše. Server ima noviji
// program, a stari zada pozivatelj.
func serverWindowsLaptop(t *testing.T, stari func(*Service)) (server, windows, laptop *Service) {
	t.Helper()
	cv := clanoviMreze(t, false, "cop-osijek", "windows-ured", "laptop-baranja")
	server, windows, laptop = cv[0], cv[1], cv[2]
	sPovrsinom(server)
	stari(windows)
	stari(laptop)
	for _, s := range cv {
		s.SetWantsAll(true)
	}
	izjednaci(t, server, windows)
	izjednaci(t, windows, laptop)
	return server, windows, laptop
}

func TestZapisCekaNadogradnjuOdKrajaDoKraja(t *testing.T) {
	server, windows, laptop := serverWindowsLaptop(t, sPovrsinom)

	// noviji program na serveru piše blagdane shemom 2, koju stariji ne smije
	// uređivati; Windows i laptop imaju stariji program
	t.Cleanup(func() { ledger.PostaviShemu(repository.EntityBlagdani, ledger.SchemaVersion) })
	ledger.PostaviShemu(repository.EntityBlagdani, 2)
	if err := repository.NewObracunRepository(server.db, server.rec).SaveBlagdan(ctxRub, danObrane); err != nil {
		t.Fatal(err)
	}
	ledger.PostaviShemu(repository.EntityBlagdani, ledger.SchemaVersion)

	// Windows ga primi, sačuva i prikaže, ali ga ne smije uređivati
	razgovaraj(t, windows, server)
	cekaIPovrsina(t, windows, windows.rec, 1, danObrane.Naziv)
	izmjena := danObrane
	izmjena.Naziv = "Dan obrane (izmjena)"
	if err := repository.NewObracunRepository(windows.db, windows.rec).SaveBlagdan(ctxRub, izmjena); !errors.Is(err, ledger.ErrNovijaShema) {
		t.Fatalf("stariji program uređuje zapis novije sheme: %v", err)
	}

	// Windows ga proslijedi laptopu i javi koliko kod njega čeka
	if st := cekaKodDrugog(t, laptop, windows); st.CekaNadogradnju != 1 || !st.MogucnostiPoznate {
		t.Fatalf("laptop o Windowsu: čeka %d, mogućnosti poznate %v", st.CekaNadogradnju, st.MogucnostiPoznate)
	}
	if v, err := laptop.rec.Latest(ctxRub, repository.EntityBlagdani, danObrane.ID); err != nil || v == nil || v.NodeID != server.node.ID || v.SchemaVersion != 2 {
		t.Fatalf("laptop nema serverov zapis novije sheme: %+v %v", v, err)
	}
	cekaIPovrsina(t, laptop, laptop.rec, 1, danObrane.Naziv)

	// nakon nadogradnje Windows zapis smije mijenjati i broja više nema
	ledger.PostaviShemu(repository.EntityBlagdani, 2)
	nadograden, rec := nadogradi(t, windows)
	cekaIPovrsina(t, windows, rec, 0, danObrane.Naziv)
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

// procesStarogPrograma: test koji ga nađe u okolini radi u zasebnom
// procesu, u kojem smije zadati popis poznatih entiteta
const procesStarogPrograma = "GOCOP_TEST_STARI_PROGRAM"

// uZasebnomProcesu pokrene isti test u zasebnom procesu i padne s
// njegovim ispisom kad on padne ili se uopće ne izvede
func uZasebnomProcesu(t *testing.T) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+t.Name()+"$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), procesStarogPrograma+"=1")
	izlaz, err := cmd.CombinedOutput()
	if err != nil || !bytes.Contains(izlaz, []byte("--- PASS: "+t.Name())) {
		t.Fatalf("zaseban proces: %v\n%s", err, izlaz)
	}
}

// Zapis entiteta koji stariji program uopće nema: Windows ga primi i
// proslijedi laptopu, ali ga ne stavi na površinu. Nakon nadogradnje ga
// primijeni iz knjige, pa broja više nema ni kod njega ni na laptopu uz
// njega. Popis poznatih (ledger.PoznatiEntiteti) se ne da vratiti, pa test
// radi u zasebnom procesu i ostale testove paketa ne dira.
func TestNepoznatZapisPrimijeniSeNakonNadogradnje(t *testing.T) {
	if os.Getenv(procesStarogPrograma) == "" {
		uZasebnomProcesu(t)
		return
	}
	// stari program blagdane ne poznaje
	ledger.PoznatiEntiteti(slices.DeleteFunc(repository.PoznatiEntiteti(), func(e string) bool { return e == repository.EntityBlagdani })...)
	server, windows, laptop := serverWindowsLaptop(t, stariProgram)
	if err := repository.NewObracunRepository(server.db, server.rec).SaveBlagdan(ctxRub, danObrane); err != nil {
		t.Fatal(err)
	}

	// Windows ga sačuva, a ne primijeni
	razgovaraj(t, windows, server)
	cekaIPovrsina(t, windows, windows.rec, 1)

	// Windows ga proslijedi laptopu i javi koliko kod njega čeka
	if st := cekaKodDrugog(t, laptop, windows); st.CekaNadogradnju != 1 {
		t.Fatalf("laptop o Windowsu: čeka %d", st.CekaNadogradnju)
	}
	if v, err := laptop.rec.Latest(ctxRub, repository.EntityBlagdani, danObrane.ID); err != nil || v == nil || v.NodeID != server.node.ID {
		t.Fatalf("laptop nema serverov zapis: %+v %v", v, err)
	}
	cekaIPovrsina(t, laptop, laptop.rec, 1)

	// nadogradnja: novi program zna blagdane i primijeni ono što čeka u knjizi
	ledger.PoznatiEntiteti(repository.EntityBlagdani)
	nadograden, rec := nadogradi(t, windows)
	cekaIPovrsina(t, windows, rec, 0, danObrane.Naziv)
	if st := cekaKodDrugog(t, laptop, nadograden); st.CekaNadogradnju != 0 {
		t.Errorf("laptop o nadograđenom Windowsu: čeka %d", st.CekaNadogradnju)
	}
}
