package service_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// djelatniciZaZid je baza sa sektorima B i C, područjem 34 i globalnim
// administratorom „Ana Uprava”, s uslugom računa i zidom nad istom knjigom
type djelatniciZaZid struct {
	repo   *repository.UserRepository
	usluga *service.UserService
	zid    *service.ZidService
	admin  *models.UserPermissions
}

func noviDjelatniciZaZid(t *testing.T) djelatniciZaZid {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "zid.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`,
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('C', 'Sektor C', 'VGO Zagreb', 'COP Zagreb')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (34, 'B', 'Drava i Dunav', 'COP', 'Osijek')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "cvor-a")
	repo := repository.NewUserRepository(baza, rec)
	glavna := &models.User{Username: "auprava", PasswordHash: "x", FullName: "Ana Uprava", IsGlobalAdmin: true, IsActive: true}
	if err := repo.CreateUser(glavna, nil); err != nil {
		t.Fatal(err)
	}
	usluga := service.NewUserService(repo, service.NewAuthService(repo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	zid := service.NewZidService(rec, nil, nil, nil, repo, nil, nil)
	return djelatniciZaZid{repo: repo, usluga: usluga, zid: zid, admin: &models.UserPermissions{IsGlobalAdmin: true, User: *glavna}}
}

// redci su naslovi redaka modula Djelatnici, najnoviji prvi, s tekstom i
// imenom onoga tko je promjenu napravio
func (d djelatniciZaZid) redci(t *testing.T, perms *models.UserPermissions) []service.Dogadjaj {
	t.Helper()
	dog, _, err := d.zid.Zadnji(context.Background(), perms, service.FiltarZida{Modul: service.ModulDjelatnici, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	return dog
}

func (d djelatniciZaZid) noviVodocuvar(t *testing.T) *models.User {
	t.Helper()
	b, p := "B", 34
	u, err := d.usluga.CreateUser(d.admin, service.CreateUserRequest{
		Username: "mmarkovic", Password: "privremena-lozinka", FullName: "Marko Marković", OrgType: models.OrgHrvatskeVode,
		Email: "marko.markovic@voda.hr", Role: models.RoleWaterGuard, ScopeType: models.ScopeArea, SectorID: &b, AreaID: &p,
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func naslovi(dog []service.Dogadjaj) []string {
	var out []string
	for _, d := range dog {
		out = append(out, d.Naslov)
	}
	return out
}

// Promjene ovlasti idu u Događanja s imenom onoga tko ih je napravio:
// otvoren račun i dodijeljeno zaduženje, izmjena telefona nije redak, a
// odlazak iz službe isključi prijavu, opozove zaduženja i vidi se kao
// istaknut redak; ponovno uključen račun više nije izvan službe.
func TestZidDjelatniciOvlastiIOdlazak(t *testing.T) {
	d := noviDjelatniciZaZid(t)
	u := d.noviVodocuvar(t)

	dog := d.redci(t, d.admin)
	if got := strings.Join(naslovi(dog), " | "); got != "Dodijeljeno zaduženje | Otvoren račun | Otvoren račun" {
		t.Fatalf("redci nakon otvaranja: %s", got)
	}
	if dog[0].Tko != "Ana Uprava" || dog[0].Sektor != "B" || dog[0].AreaID != 34 {
		t.Errorf("redak zaduženja: %+v", dog[0])
	}
	if !strings.Contains(dog[0].Tekst, "Marko Marković") || !strings.Contains(dog[0].Tekst, "BP 34") {
		t.Errorf("tekst zaduženja: %q", dog[0].Tekst)
	}

	// telefon nije promjena ovlasti
	puni, err := d.repo.GetUserByID(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.usluga.UpdateUser(d.admin, service.UpdateUserRequest{ID: u.ID, Username: puni.Username, FullName: puni.FullName,
		OrgType: puni.OrgType, Email: puni.Email, IsActive: true, MobilePhone: "099-000-0000"}); err != nil {
		t.Fatal(err)
	}
	if n := len(d.redci(t, d.admin)); n != 3 {
		t.Fatalf("izmjena telefona dala je redak: %d redaka", n)
	}

	// odlazak u mirovinu
	dan := time.Now().In(models.Zagreb).AddDate(0, 0, -2)
	if err := d.usluga.OdlazakIzSluzbe(d.admin, service.OdlazakRequest{UserID: u.ID, Razlog: models.OdlazakMirovina, Dan: dan}); err != nil {
		t.Fatal(err)
	}
	otisao, err := d.repo.GetUserByID(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if otisao.IsActive || !otisao.IzvanSluzbe() || len(otisao.Duties) != 0 {
		t.Fatalf("nakon odlaska: aktivan %v, izvan službe %v, zaduženja %d", otisao.IsActive, otisao.IzvanSluzbe(), len(otisao.Duties))
	}
	dog = d.redci(t, d.admin)
	if got := strings.Join(naslovi(dog[:2]), " | "); got != "Odlazak iz službe | Opozvano zaduženje" {
		t.Fatalf("redci nakon odlaska: %s", got)
	}
	if !dog[0].Vazno || dog[0].Tko != "Ana Uprava" || !strings.Contains(dog[0].Tekst, "odlazak u mirovinu "+dan.Format("2. 1. 2006.")) {
		t.Errorf("redak odlaska: %+v", dog[0])
	}
	if dog[1].Tko != "Ana Uprava" {
		t.Errorf("opoziv bez imena: %+v", dog[1])
	}

	// povratak: uključena prijava briše odlazak
	if _, err := d.usluga.UpdateUser(d.admin, service.UpdateUserRequest{ID: u.ID, Username: otisao.Username, FullName: otisao.FullName,
		OrgType: otisao.OrgType, Email: otisao.Email, IsActive: true}); err != nil {
		t.Fatal(err)
	}
	vracen, err := d.repo.GetUserByID(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !vracen.IsActive || vracen.IzvanSluzbe() || vracen.OdlazakDan != nil {
		t.Errorf("vraćen račun: aktivan %v, odlazak %q %v", vracen.IsActive, vracen.Odlazak, vracen.OdlazakDan)
	}
	if got := d.redci(t, d.admin)[0].Naslov; got != "Ponovno uključena prijava" {
		t.Errorf("redak povratka: %s", got)
	}

	// uprava sektora C ne vidi zaduženja sektora B, a račune vidi
	upravaC := &models.UserPermissions{AdminSectors: map[string]bool{"C": true}, AllowedSectors: map[string]bool{"C": true}}
	for _, r := range d.redci(t, upravaC) {
		if strings.Contains(r.Naslov, "zaduženje") {
			t.Errorf("uprava sektora C vidi zaduženje sektora B: %+v", r)
		}
	}
}

// Odlazak se ne bilježi sam sebi, ni unaprijed, ni bez razloga, i ne
// dvaput
func TestOdlazakIzSluzbeOdbija(t *testing.T) {
	d := noviDjelatniciZaZid(t)
	u := d.noviVodocuvar(t)
	sutra := time.Now().In(models.Zagreb).AddDate(0, 0, 1)
	for ime, req := range map[string]service.OdlazakRequest{
		"bez razloga":    {UserID: u.ID},
		"kriv razlog":    {UserID: u.ID, Razlog: "otkaz"},
		"u budućnosti":   {UserID: u.ID, Razlog: models.OdlazakOdlazak, Dan: sutra},
		"sam sebi":       {UserID: d.admin.User.ID, Razlog: models.OdlazakMirovina},
		"nepoznat račun": {UserID: uuid.New(), Razlog: models.OdlazakMirovina},
		"tuđim očima":    {UserID: u.ID, Razlog: models.OdlazakMirovina, TudjimOcima: true},
	} {
		if err := d.usluga.OdlazakIzSluzbe(d.admin, req); err == nil {
			t.Errorf("%s: odlazak je zabilježen", ime)
		}
	}
	if err := d.usluga.OdlazakIzSluzbe(d.admin, service.OdlazakRequest{UserID: u.ID, Razlog: models.OdlazakPremjestaj}); err != nil {
		t.Fatal(err)
	}
	if err := d.usluga.OdlazakIzSluzbe(d.admin, service.OdlazakRequest{UserID: u.ID, Razlog: models.OdlazakPremjestaj}); err == nil {
		t.Error("odlazak je zabilježen dvaput")
	}
	// vodočuvar nema upravu, pa ničiji odlazak ne bilježi
	vodocuvar := models.NewUserPermissions(*u)
	if err := d.usluga.OdlazakIzSluzbe(vodocuvar, service.OdlazakRequest{UserID: d.admin.User.ID, Razlog: models.OdlazakMirovina}); err == nil {
		t.Error("vodočuvar je zabilježio odlazak uprave")
	}
}

// Uprava drugog sektora ne bilježi odlazak, a tuđim očima ne bilježi se
// odlazak osobe s upravnom dužnošću, ni kad joj je prijava već isključena
func TestOdlazakIzSluzbeDosegIUprava(t *testing.T) {
	d := noviDjelatniciZaZid(t)
	u := d.noviVodocuvar(t)
	c, b := "C", "B"
	upravaC, err := d.usluga.CreateUser(d.admin, service.CreateUserRequest{Username: "rkovac", Password: "privremena-lozinka", FullName: "Rade Kovač",
		OrgType: models.OrgHrvatskeVode, Email: "rade.kovac@voda.hr", Role: models.RoleSectorLeader, ScopeType: models.ScopeSector, SectorID: &c})
	if err != nil {
		t.Fatal(err)
	}
	puni, err := d.repo.GetUserByID(upravaC.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.usluga.OdlazakIzSluzbe(models.NewUserPermissions(*puni), service.OdlazakRequest{UserID: u.ID, Razlog: models.OdlazakMirovina}); err == nil {
		t.Error("uprava sektora C zabilježila je odlazak vodočuvara iz sektora B")
	}

	sef, err := d.usluga.CreateUser(d.admin, service.CreateUserRequest{Username: "sbaric", Password: "privremena-lozinka", FullName: "Stipe Barić",
		OrgType: models.OrgHrvatskeVode, Email: "stipe.baric@voda.hr", Role: models.RoleSectorLeader, ScopeType: models.ScopeSector, SectorID: &b})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.usluga.UpdateUser(d.admin, service.UpdateUserRequest{ID: sef.ID, Username: sef.Username, FullName: sef.FullName,
		OrgType: sef.OrgType, Email: sef.Email, IsActive: false}); err != nil {
		t.Fatal(err)
	}
	err = d.usluga.OdlazakIzSluzbe(d.admin, service.OdlazakRequest{UserID: sef.ID, Razlog: models.OdlazakMirovina, TudjimOcima: true})
	if err == nil || !strings.Contains(err.Error(), "ne možete opozvati") {
		t.Errorf("tuđim očima odlazak uprave: %v", err)
	}
}
