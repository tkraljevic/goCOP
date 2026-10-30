package repository

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/models"
)

// kvarBaze je jedan način na koji upis u bazu ne uspije; prazan upit znači
// zatvorenu bazu
type kvarBaze struct{ naziv, q string }

// pokvari zatvori bazu čvora ili postavi okidač kvara
func pokvari(t *testing.T, n *cvor, k kvarBaze) {
	t.Helper()
	if k.q == "" {
		n.db.Close()
		return
	}
	if _, err := n.db.Exec(k.q); err != nil {
		t.Fatalf("%s: %v", k.naziv, err)
	}
}

// verzijaEntiteta broji verzije entiteta u knjizi čvora (sve zapise)
func verzijaEntiteta(t *testing.T, n *cvor, entitet string) int {
	t.Helper()
	return brojRedaka(t, n.db, `SELECT COUNT(*) FROM record_versions WHERE entity = ?`, entitet)
}

// zabranaKnjigeZa odbija upis verzije samo jednog entiteta
func zabranaKnjigeZa(entitet string) string {
	return `CREATE TRIGGER zabrana_knjige_` + entitet + ` BEFORE INSERT ON record_versions WHEN NEW.entity = '` + entitet + `'
		BEGIN SELECT RAISE(ABORT, 'knjiga ne prima'); END`
}

// Novi račun koji ne uspije upisati ni u jednom koraku ne ostavlja ništa:
// ni račun ni početnu dužnost na površini, ni verziju u knjizi.
func TestNoviRacunNeuspjeliUpisNistaNeOstavlja(t *testing.T) {
	for _, kvar := range []kvarBaze{
		{"zatvorena baza", ""},
		{"knjiga se ne da pročitati", `ALTER TABLE record_versions RENAME TO record_versions_stara`},
		{"upis računa zabranjen", `CREATE TRIGGER zabrana BEFORE INSERT ON users BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`},
		{"upis dužnosti zabranjen", `CREATE TRIGGER zabrana BEFORE INSERT ON duties BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`},
		{"knjiga ne prima račun", zabranaKnjige},
		{"knjiga ne prima dužnost", zabranaKnjigeZa(EntityDuties)},
	} {
		t.Run(kvar.naziv, func(t *testing.T) {
			n := noviCvor(t, "ured")
			repo := NewUserRepository(n.db, n.rec)
			pokvari(t, n, kvar)
			u := &models.User{Username: "aanic", PasswordHash: "sazetak", FullName: "Ana Anić", OrgType: models.OrgHrvatskeVode, IsActive: true}
			if err := repo.CreateUser(u, vodocuvarDuznost(uuid.Nil, osnovaDionica)); err == nil {
				t.Fatal("upis računa je prošao")
			}
			if kvar.q == "" {
				return
			}
			if n := brojRedaka(t, n.db, `SELECT COUNT(*) FROM users WHERE username = 'aanic'`); n != 0 {
				t.Errorf("račun je na površini (%d)", n)
			}
			if n := brojRedaka(t, n.db, `SELECT COUNT(*) FROM duties`); n != 0 {
				t.Errorf("dužnost je na površini (%d)", n)
			}
			if strings.Contains(kvar.q, "RENAME") {
				return
			}
			if v := verzijaEntiteta(t, n, EntityUsers) + verzijaEntiteta(t, n, EntityDuties); v != 0 {
				t.Errorf("u knjizi je %d verzija", v)
			}
		})
	}
}

// Novi račun dobiva stalni identitet iz korisničkog imena, osim kad je taj
// identitet već bio nečiji: obrisani račun istog imena ostaje svoja
// povijest, a novi račun dobiva novi identitet.
func TestNoviRacunNakonObrisanogIstogImena(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	prvi := novaOsoba(t, repo, "aanic", "Ana Anić", nil)
	stalni := db.StableID("user", "aanic")
	if prvi.ID != stalni {
		t.Fatalf("prvi račun: %s, očekivan stalni %s", prvi.ID, stalni)
	}
	nuzno(t, repo.DeleteUser(prvi.ID))

	drugi := novaOsoba(t, repo, "aanic", "Ana Anić", nil)
	if drugi.ID == stalni || drugi.ID == uuid.Nil {
		t.Fatalf("drugi račun istog imena dobio je identitet %s", drugi.ID)
	}
	if v := verzijeRacuna(t, n, stalni); len(v) != 2 || !v[0].archived {
		t.Errorf("povijest obrisanog računa: %d verzija", len(v))
	}
	if v := verzijeRacuna(t, n, drugi.ID); len(v) != 1 || v[0].archived {
		t.Errorf("povijest novog računa: %d verzija", len(v))
	}
}

// Izmjena računa koja ne uspije ni u jednom koraku ne mijenja ništa: račun
// ostaje kakav je bio, a u knjizi nema nove verzije.
func TestIzmjenaRacunaNeuspjelaNistaNeMijenja(t *testing.T) {
	for _, kvar := range []kvarBaze{
		{"zatvorena baza", ""},
		{"upis zabranjen", `CREATE TRIGGER zabrana BEFORE UPDATE ON users BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`},
		{"izmijenjeni račun se ne da pročitati", `CREATE TRIGGER pokvari AFTER UPDATE OF full_name ON users
			BEGIN UPDATE users SET created_at = 'nije vrijeme' WHERE id = NEW.id; END`},
		{"knjiga ne prima verziju", zabranaKnjige},
	} {
		t.Run(kvar.naziv, func(t *testing.T) {
			n := noviCvor(t, "ured")
			repo := NewUserRepository(n.db, n.rec)
			ana := novaOsoba(t, repo, "aanic", "Ana Anić", nil)
			pokvari(t, n, kvar)
			izmjena := *ana
			izmjena.FullName, izmjena.PasswordHash = "Ana Anić Horvat", ""
			if err := repo.UpdateUser(&izmjena); err == nil {
				t.Fatal("izmjena računa je prošla")
			}
			if kvar.q == "" {
				return
			}
			u, err := repo.GetUserByID(ana.ID)
			nuzno(t, err)
			if u.FullName != "Ana Anić" {
				t.Errorf("ime nakon neuspjele izmjene: %q", u.FullName)
			}
			if v := verzijeRacuna(t, n, ana.ID); len(v) != 1 {
				t.Errorf("verzija nakon neuspjele izmjene: %d, očekivana 1", len(v))
			}
		})
	}
}

// Brisanje računa koje ne uspije ne briše ništa: račun ostaje na površini,
// a u knjizi nema spomenika. Račun kojeg nema nije pronađen.
func TestBrisanjeRacunaNeuspjeloNistaNeBrise(t *testing.T) {
	for _, kvar := range []kvarBaze{
		{"zatvorena baza", ""},
		{"račun se ne da pročitati", `UPDATE users SET created_at = 'nije vrijeme' WHERE username = 'aanic'`},
		{"brisanje zabranjeno", `CREATE TRIGGER zabrana BEFORE DELETE ON users BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`},
		{"knjiga ne prima spomenik", zabranaKnjige},
	} {
		t.Run(kvar.naziv, func(t *testing.T) {
			n := noviCvor(t, "ured")
			repo := NewUserRepository(n.db, n.rec)
			ana := novaOsoba(t, repo, "aanic", "Ana Anić", nil)
			pokvari(t, n, kvar)
			if err := repo.DeleteUser(ana.ID); err == nil {
				t.Fatal("brisanje računa je prošlo")
			}
			if kvar.q == "" {
				return
			}
			if n := brojRedaka(t, n.db, `SELECT COUNT(*) FROM users WHERE id = ?`, ana.ID.String()); n != 1 {
				t.Errorf("račun nakon neuspjelog brisanja: %d redaka", n)
			}
			if v := brojRedaka(t, n.db, `SELECT COUNT(*) FROM record_versions WHERE entity = ? AND archived = 1`, EntityUsers); v != 0 {
				t.Errorf("spomenik nakon neuspjelog brisanja: %d", v)
			}
		})
	}

	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	if err := repo.DeleteUser(uuid.New()); err == nil || !strings.Contains(err.Error(), "korisnik nije pronađen") {
		t.Errorf("brisanje računa kojeg nema: %v", err)
	}
	if v := verzijaEntiteta(t, n, EntityUsers); v != 0 {
		t.Errorf("brisanje računa kojeg nema ostavilo je %d verzija", v)
	}
}

// Nova dužnost koja ne uspije ne mijenja ništa: nema je na površini ni u
// knjizi, a dosadašnja glavna dužnost ostaje glavna.
func TestNovaDuznostNeuspjelaNistaNeMijenja(t *testing.T) {
	for _, kvar := range []kvarBaze{
		{"zatvorena baza", ""},
		{"upis zabranjen", `CREATE TRIGGER zabrana BEFORE INSERT ON duties BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`},
		{"dosadašnja glavna se ne da pročitati", `UPDATE duties SET created_at = 'nije vrijeme'`},
		{"knjiga ne prima verziju", zabranaKnjige},
	} {
		t.Run(kvar.naziv, func(t *testing.T) {
			n := noviCvor(t, "ured")
			repo := NewUserRepository(n.db, n.rec)
			glavna := vodocuvarDuznost(osnovaKorisnik, osnovaDionica)
			glavna.IsPrimary = true
			nuzno(t, repo.AddDuty(glavna))
			pokvari(t, n, kvar)
			nova := privremena(nil, false, nil)
			nova.IsPrimary = strings.Contains(kvar.naziv, "glavna")
			if err := repo.AddDuty(nova); err == nil {
				t.Fatal("upis dužnosti je prošao")
			}
			if kvar.q == "" {
				return
			}
			if n := brojRedaka(t, n.db, `SELECT COUNT(*) FROM duties WHERE id = ?`, nova.ID.String()); n != 0 {
				t.Errorf("nova dužnost je na površini")
			}
			if n := brojRedaka(t, n.db, `SELECT COUNT(*) FROM duties WHERE id = ? AND is_primary = 1`, glavna.ID.String()); n != 1 {
				t.Errorf("dosadašnja glavna dužnost više nije glavna")
			}
			if v := verzijaEntiteta(t, n, EntityDuties); v != 1 {
				t.Errorf("verzija dužnosti: %d, očekivana 1", v)
			}
		})
	}
}

// Zaduženje čita i tko ga je dodijelio; zaduženje kojeg nema nije greška,
// a zaduženje koje se ne da pročitati jest.
func TestCitanjeZaduzenja(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	dodijelio := novaOsoba(t, repo, "aanic", "Ana Anić", nil).ID
	d := vodocuvarDuznost(osnovaKorisnik, osnovaDionica)
	d.AssignedBy = &dodijelio
	nuzno(t, repo.AddDuty(d))

	duznosti, err := repo.GetDutiesForUser(osnovaKorisnik)
	nuzno(t, err)
	if len(duznosti) != 1 || duznosti[0].AssignedBy == nil || *duznosti[0].AssignedBy != dodijelio {
		t.Errorf("dužnosti osobe: %+v", duznosti)
	}
	if nema, err := repo.GetDuty(uuid.New()); nema != nil || err != nil {
		t.Errorf("zaduženje kojeg nema: %+v %v", nema, err)
	}

	if _, err := n.db.Exec(`UPDATE duties SET created_at = 'nije vrijeme'`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetDutiesForUser(osnovaKorisnik); err == nil {
		t.Error("nečitljive dužnosti osobe bez greške")
	}
	n.db.Close()
	if _, err := repo.GetDuty(d.ID); err == nil {
		t.Error("zaduženje iz zatvorene baze bez greške")
	}
	if _, err := repo.ListAreas("B"); err == nil {
		t.Error("popis područja iz zatvorene baze bez greške")
	}
}

// Sektor s razinom koja nije broj ruši popis sektora umjesto da ga skrati
func TestPopisSektoraSNecitljivomRazinom(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	if _, err := n.db.Exec(`UPDATE sectors SET level = 'nije broj' WHERE id = 'B'`); err != nil {
		t.Fatal(err)
	}
	if sektori, err := repo.ListSectors(); err == nil {
		t.Errorf("popis sektora s nečitljivom razinom: %+v", sektori)
	}
}

// Prijava koja se ne da zabilježiti ne mijenja račun: zadnja prijava ostaje
// prazna, a u knjizi nema verzije.
func TestPrijavaNeuspjeloBiljezenje(t *testing.T) {
	for _, kvar := range []kvarBaze{
		{"zatvorena baza", ""},
		{"upis zabranjen", `CREATE TRIGGER zabrana BEFORE UPDATE OF last_login_at ON users
			BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`},
		{"račun se ne da pročitati", `CREATE TRIGGER pokvari AFTER UPDATE OF last_login_at ON users
			BEGIN UPDATE users SET created_at = 'nije vrijeme' WHERE id = NEW.id; END`},
		{"knjiga ne prima verziju", zabranaKnjige},
	} {
		t.Run(kvar.naziv, func(t *testing.T) {
			n := noviCvor(t, "ured")
			repo := NewUserRepository(n.db, n.rec)
			pokvari(t, n, kvar)
			if err := repo.MarkLogin(osnovaKorisnik, time.Date(2026, 3, 10, 7, 0, 0, 0, time.UTC)); err == nil {
				t.Fatal("prijava je zabilježena")
			}
			if kvar.q == "" {
				return
			}
			if n := brojRedaka(t, n.db, `SELECT COUNT(*) FROM users WHERE id = ? AND last_login_at IS NOT NULL`, osnovaKorisnik.String()); n != 0 {
				t.Error("zadnja prijava je upisana")
			}
			if v := verzijaEntiteta(t, n, EntityUsers); v != 0 {
				t.Errorf("verzija nakon neuspjele prijave: %d", v)
			}
		})
	}
}

// Nova glavna dužnost ne prolazi kad se dosadašnje glavne ne daju pročitati:
// dužnost bez identifikatora ili bez tablice dužnosti
func TestGlavnaDuznostKadSeDosadasnjeNeDajuProcitati(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	glavna := vodocuvarDuznost(osnovaKorisnik, osnovaDionica)
	glavna.IsPrimary = true
	nuzno(t, repo.AddDuty(glavna))
	if _, err := n.db.Exec(`UPDATE duties SET id = NULL`); err != nil {
		t.Fatal(err)
	}
	nova := privremena(nil, false, nil)
	nova.IsPrimary = true
	if err := repo.AddDuty(nova); err == nil {
		t.Fatal("nova glavna dužnost uz nečitljivu dosadašnju je upisana")
	}
	if n := brojRedaka(t, n.db, `SELECT COUNT(*) FROM duties WHERE is_primary = 1`); n != 1 {
		t.Errorf("glavnih dužnosti: %d, očekivana 1", n)
	}

	if _, err := n.db.Exec(`ALTER TABLE duties RENAME TO duties_negdje`); err != nil {
		t.Fatal(err)
	}
	izmjena := *glavna
	izmjena.Title = "Glavni vodočuvar"
	if err := repo.UpdateDuty(&izmjena); err == nil {
		t.Error("izmjena glavne dužnosti bez tablice dužnosti je prošla")
	}
	if v := verzijaEntiteta(t, n, EntityDuties); v != 1 {
		t.Errorf("verzija dužnosti: %d, očekivana 1", v)
	}
}

// Račun čije se dužnosti ne daju pročitati ne vraća se kao račun bez
// dužnosti: prava se odlučuju i po njima
func TestRacunBezProcitanihDuznostiJeGreska(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	u := &models.User{Username: "aanic", PasswordHash: "sazetak", FullName: "Ana Anić", OrgType: models.OrgHrvatskeVode, IsActive: true}
	if err := repo.CreateUser(u, vodocuvarDuznost(uuid.Nil, osnovaDionica)); err != nil {
		t.Fatal(err)
	}
	if procitan, err := repo.GetUserByID(u.ID); err != nil || procitan == nil || len(procitan.Duties) != 1 {
		t.Fatalf("račun s dužnošću: %+v %v", procitan, err)
	}
	pokvari(t, n, kvarBaze{"dužnosti se ne daju pročitati", `ALTER TABLE duties RENAME TO duties_stara`})
	if procitan, err := repo.GetUserByID(u.ID); err == nil {
		t.Fatalf("račun vraćen bez dužnosti: %+v", procitan)
	}
}
