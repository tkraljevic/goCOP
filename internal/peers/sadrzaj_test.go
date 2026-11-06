package peers_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/repository"
	"gocop/internal/sadrzaj"
)

// startNodeBezRegistra diže čvor bez registara iz data/: dovoljno za
// razmjenu sadržaja, a test ne ovisi o datotekama izvan repozitorija
func startNodeBezRegistra(t *testing.T, ctx context.Context, id string) *node {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "gocop.db")
	database, err := db.BazaIzPredloska(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	rec := ledger.New(database, id)
	n, err := peers.LoadNode(dbPath, id, "test-"+id, "test")
	if err != nil {
		t.Fatal(err)
	}
	svc := noviServis(t, database, rec, n)
	svc.OnApplied(func(ctx context.Context, versions []ledger.Version) error {
		return repository.ApplyVersions(ctx, database, rec, versions)
	})
	posluzi(t, ctx, svc)
	return &node{id: id, db: database, svc: svc, rec: rec}
}

// Sadržaj putuje po otisku prema razini pretplate: čvor koji prati samo
// kazalo primi zapis izvornika bez bajtova; kad pretplatu digne na "sve",
// sljedeća razmjena donese PDF, provjeren otiskom.
func TestSadrzajPutujePremaRaziniPretplate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	a := startNodeBezRegistra(t, ctx, "cop-osijek")
	b := startNodeBezRegistra(t, ctx, "laptop-baranja")
	founder(t, ctx, a, "Hrvatske vode")
	pair(t, ctx, a, b)

	// A drži PDF u svom spremištu; B-ovo spremište je ono koje primjena
	// verzija koristi (jedno po programu), pa se A-ovo drži zasebno
	spA, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer spA.Zatvori()
	spB, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer spB.Zatvori()
	a.svc.SetSpremiste(spA)
	b.svc.SetSpremiste(spB)
	repository.SetSpremiste(spB)
	defer repository.SetSpremiste(nil)
	a.svc.SetWantsAll(true)

	pdf := []byte("%PDF-1.4\nizvornik prijave s Osijeka")
	kanal := "prijave/16/2026"
	otisak, err := spA.Upisi(ctx, "application/pdf", pdf, "ovdje", sadrzaj.Veza{Entitet: repository.EntityPrijave, EntitetID: "p1", Uloga: "izvornik", Kanal: kanal})
	if err != nil {
		t.Fatal(err)
	}
	tx, _ := a.db.BeginTx(ctx, nil)
	if _, err := a.rec.RecordIn(ctx, tx, kanal, repository.EntityPrijaveIzvornici, "p1",
		models.IzvornikLista{ListID: "p1", Otisak: otisak, Bajtova: len(pdf), Vrsta: "application/pdf", UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	_ = tx.Commit()

	// B prati prijave BP 16, ali samo kazalo
	b.svc.SetWantsAll(false)
	pravilo, err := b.svc.AddSubscription(ctx, peers.Subscription{Kind: "prijave", AreaID: 16, Razina: peers.RazinaKazalo})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatalf("razmjena: %v", err)
	}
	var zapisa int
	_ = b.db.QueryRow(`SELECT count(*) FROM prijave_izvornici WHERE prijava_id = 'p1' AND otisak = ?`, otisak).Scan(&zapisa)
	if zapisa != 1 {
		t.Fatalf("B nije primio zapis izvornika: %d", zapisa)
	}
	if spB.Ima(ctx, otisak) {
		t.Fatal("B je primio sadržaj iako prati samo kazalo")
	}
	if z, _ := spB.Zeljeni(ctx, 10); len(z) != 1 || z[0].Kanal != kanal {
		t.Fatalf("B ne zna što mu nedostaje: %+v", z)
	}

	// pretplata na sve: sljedeća razmjena donese bajtove
	if err := b.svc.RemoveSubscription(ctx, pravilo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := b.svc.AddSubscription(ctx, peers.Subscription{Kind: "prijave", AreaID: 16, Razina: peers.RazinaSve, DrziDana: 90}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatalf("druga razmjena: %v", err)
	}
	got, _, err := spB.Citaj(ctx, otisak)
	if err != nil || string(got) != string(pdf) {
		t.Fatalf("B nije dobio PDF: %v", err)
	}
	if z, _ := spB.Zeljeni(ctx, 10); len(z) != 0 {
		t.Errorf("želja nije nestala: %+v", z)
	}
	// rok držanja još nije istekao, pa otpuštanje ništa ne dira
	if n, _, err := b.svc.OtpustiStare(ctx); err != nil || n != 0 {
		t.Errorf("otpušteno prerano: %d %v", n, err)
	}
	// A ne traži ništa natrag (ima sve), i razmjena u drugom smjeru prolazi
	if _, _, err := a.svc.SyncWith(ctx, b.id); err != nil {
		t.Fatalf("razmjena A→B: %v", err)
	}
}

// B je izvornik prijave BP 16 već tražio bez uspjeha, a ispred njega na
// popisu za dohvat stoji 250 izvornika BP 3, koje B po pretplati ne
// dohvaća. Izvornik ipak stigne u prvoj razmjeni: neželjeno ne zauzima
// mjesta od 200 po razmjeni, a ostaje na popisu.
func TestNezeljeniSadrzajNeZadrzavaZeljeni(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	a := startNodeBezRegistra(t, ctx, "cop-osijek")
	b := startNodeBezRegistra(t, ctx, "laptop-baranja")
	founder(t, ctx, a, "Hrvatske vode")
	pair(t, ctx, a, b)
	spA, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = spA.Zatvori() }()
	spB, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = spB.Zatvori() }()
	a.svc.SetSpremiste(spA)
	b.svc.SetSpremiste(spB)
	a.svc.SetWantsAll(true)
	if _, err := b.svc.AddSubscription(ctx, peers.Subscription{Kind: "prijave", AreaID: 16}); err != nil {
		t.Fatal(err)
	}

	pdf := []byte("%PDF-1.4\nizvornik prijave BP 16")
	kanal := "prijave/16/2026"
	otisak, err := spA.Upisi(ctx, "application/pdf", pdf, "ovdje", sadrzaj.Veza{Entitet: repository.EntityPrijave, EntitetID: "p16", Uloga: "izvornik", Kanal: kanal})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 250; i++ {
		o := sadrzaj.Otisak([]byte(fmt.Sprintf("izvornik prijave BP 3 br. %d", i)))
		if err := spB.Zeli(ctx, o, "application/pdf", 1000, "pretplata", "prijave/3/2026"); err != nil {
			t.Fatal(err)
		}
	}
	if err := spB.Zeli(ctx, otisak, "application/pdf", len(pdf), "pretplata", kanal); err != nil {
		t.Fatal(err)
	}
	if err := spB.Trazeno(ctx, otisak); err != nil {
		t.Fatal(err)
	}

	if _, _, err := b.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatalf("razmjena: %v", err)
	}
	if got, _, err := spB.Citaj(ctx, otisak); err != nil || string(got) != string(pdf) {
		t.Fatalf("izvornik nije stigao u prvoj razmjeni: %v", err)
	}
	if z, _ := spB.Zeljeni(ctx, 300); len(z) != 250 {
		t.Errorf("na popisu za dohvat %d, očekivano 250 neželjenih", len(z))
	}
}

// Slika zaslona uz prijavu greške ide stvarnom razmjenom: drugo računalo dobije
// prijavu i sliku po otisku, i kad prati samo jedan sektor, jer je prijava u
// zajedničkom kanalu. Bajtovi su izvan knjige.
func TestSlikaPrijaveGreskePutujeRazmjenom(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	a := startNodeBezRegistra(t, ctx, "cop-osijek")
	b := startNodeBezRegistra(t, ctx, "laptop-baranja")
	founder(t, ctx, a, "Hrvatske vode")
	pair(t, ctx, a, b)
	spA, spB := spremisteCvora(t, "a"), spremisteCvora(t, "b")
	a.svc.SetSpremiste(spA)
	b.svc.SetSpremiste(spB)
	a.svc.SetWantsAll(true)
	b.svc.SetWantsAll(false)
	if _, err := b.svc.AddSubscription(ctx, peers.Subscription{Kind: "prijave", AreaID: 16, Razina: peers.RazinaKazalo}); err != nil {
		t.Fatal(err)
	}

	slika := []byte("\x89PNG bajtovi slike zaslona")
	repository.SetSpremiste(spA)
	defer repository.SetSpremiste(nil)
	p := &models.PrijavaGreske{UserID: "osoba-1", Vrsta: models.VrstaPrijaveGreska, Opis: "Graf se ne crta.", Stanje: models.StanjePrijaveNova,
		SlikaOtisak: sadrzaj.Otisak(slika), SlikaBajtova: len(slika), SlikaVrsta: models.SlikaPrijavePNG}
	if err := repository.NewPrijavaGreskeRepository(a.db, a.rec).SaveSaSlikom(ctx, p, slika); err != nil {
		t.Fatal(err)
	}

	// primjena verzija na B-u koristi spremište B-a
	repository.SetSpremiste(spB)
	if _, _, err := b.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatalf("razmjena: %v", err)
	}
	stigla, err := repository.NewPrijavaGreskeRepository(b.db, b.rec).Get(ctx, p.ID)
	if err != nil || stigla == nil || stigla.SlikaOtisak != p.SlikaOtisak {
		t.Fatalf("prijava na B-u: %+v %v", stigla, err)
	}
	if !spB.Ima(ctx, p.SlikaOtisak) {
		if _, _, err := b.svc.SyncWith(ctx, a.id); err != nil {
			t.Fatalf("druga razmjena: %v", err)
		}
	}
	got, vrsta, err := spB.Citaj(ctx, p.SlikaOtisak)
	if err != nil || string(got) != string(slika) || vrsta != models.SlikaPrijavePNG {
		t.Fatalf("slika na B-u: %q %q %v", got, vrsta, err)
	}
	if z, err := spB.Zeljeni(ctx, 10); err != nil || len(z) != 0 {
		t.Errorf("želja nije nestala: %+v %v", z, err)
	}
}

// Slika riješene prijave briše se stvarnom razmjenom kroz razgovor dva čvora:
// kad je rok prošao, računalo koje je ima briše je, a ono koje ju je obrisalo ne
// traži je ni kad je druga strana još ima. Nitko ne piše nove verzije.
func TestIsteklaSlikaPrijaveNeKruziRazmjenom(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	a := startNodeBezRegistra(t, ctx, "cop-osijek")
	b := startNodeBezRegistra(t, ctx, "laptop-baranja")
	founder(t, ctx, a, "Hrvatske vode")
	pair(t, ctx, a, b)
	spA, spB := spremisteCvora(t, "a"), spremisteCvora(t, "b")
	a.svc.SetSpremiste(spA)
	b.svc.SetSpremiste(spB)
	a.svc.SetWantsAll(true)
	b.svc.SetWantsAll(true)
	defer repository.SetSpremiste(nil)
	razmjena := func(tko string) {
		t.Helper()
		repository.SetSpremiste(spB) // primjena verzija na B-u koristi spremište B-a
		if _, _, err := b.svc.SyncWith(ctx, a.id); err != nil {
			t.Fatalf("razmjena %s: %v", tko, err)
		}
	}

	slika := []byte("\x89PNG bajtovi slike zaslona")
	otisak := sadrzaj.Otisak(slika)
	ra, rb := repository.NewPrijavaGreskeRepository(a.db, a.rec), repository.NewPrijavaGreskeRepository(b.db, b.rec)
	repository.SetSpremiste(spA)
	p := &models.PrijavaGreske{UserID: "osoba-1", Vrsta: models.VrstaPrijaveGreska, Opis: "Graf se ne crta.", Stanje: models.StanjePrijaveNova,
		SlikaOtisak: otisak, SlikaBajtova: len(slika), SlikaVrsta: models.SlikaPrijavePNG}
	if err := ra.SaveSaSlikom(ctx, p, slika); err != nil {
		t.Fatal(err)
	}
	razmjena("prva")
	if !spB.Ima(ctx, otisak) {
		razmjena("za sliku")
	}
	if !spB.Ima(ctx, otisak) {
		t.Fatal("slika nije stigla na B")
	}

	// prijava je riješena prije 31 dan, a A još nije čistio
	q := *p
	q.Stanje, q.UpdatedAt = models.StanjePrijaveRijeseno, time.Now().Add(-31*24*time.Hour).UTC()
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE prijave_gresaka SET stanje = ?, updated_at = ? WHERE id = ?`, q.Stanje, q.UpdatedAt, q.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.rec.Record(ctx, tx, repository.EntityPrijaveGresaka, q.ID, &q); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// B obriše već kad primi riješeno stanje, a sliku ne traži ni dok je A ima
	razmjena("s riješenom prijavom")
	if spB.Ima(ctx, otisak) || spB.Zeljen(ctx, otisak) {
		t.Errorf("B nakon roka: ima %v, traži %v", spB.Ima(ctx, otisak), spB.Zeljen(ctx, otisak))
	}
	razmjena("ponovna")
	if !spA.Ima(ctx, otisak) {
		t.Fatal("A još nije čistio")
	}
	if spB.Ima(ctx, otisak) || spB.Zeljen(ctx, otisak) {
		t.Error("B je sliku opet dohvatio od A")
	}

	repository.SetSpremiste(spA)
	if n, err := ra.UkloniIstekleSlike(ctx, time.Now()); err != nil || n != 1 || spA.Ima(ctx, otisak) {
		t.Errorf("A: uklonjeno %d, %v", n, err)
	}
	prije := brojVerzija(t, ctx, a)
	razmjena("nakon brisanja na oba")
	razmjena("još jedna")
	if brojVerzija(t, ctx, a) != prije || brojVerzija(t, ctx, b) != prije {
		t.Errorf("brisanje je stvorilo verzije: A %d, B %d, bilo %d", brojVerzija(t, ctx, a), brojVerzija(t, ctx, b), prije)
	}
	for _, sp := range []*sadrzaj.Spremiste{spA, spB} {
		if sp.Ima(ctx, otisak) || sp.Zeljen(ctx, otisak) {
			t.Error("slika se vratila")
		}
	}
	for _, r := range []*repository.PrijavaGreskeRepository{ra, rb} {
		x, err := r.Get(ctx, p.ID)
		if err != nil || x == nil || x.Opis != p.Opis || !x.SlikaUklonjena() {
			t.Errorf("prijava nakon brisanja: %+v %v", x, err)
		}
	}
}

// brojVerzija broji sve verzije u knjizi čvora
func brojVerzija(t *testing.T, ctx context.Context, n *node) int {
	t.Helper()
	var broj int
	if err := n.db.QueryRowContext(ctx, `SELECT count(*) FROM record_versions`).Scan(&broj); err != nil {
		t.Fatal(err)
	}
	return broj
}

// spremisteCvora otvara spremište sadržaja za jedan čvor u testu
func spremisteCvora(t *testing.T, ime string) *sadrzaj.Spremiste {
	t.Helper()
	sp, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), ime+".db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sp.Zatvori(); err != nil {
			t.Error(err)
		}
	})
	return sp
}
