package service

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Vodočuvar uz svoj nacrt miče samo fotografiju koja je na tom nacrtu:
// id fotografije s nacrta drugog vodočuvara vraća grešku, a ta fotografija
// ostaje. Svoju fotografiju i dalje miče. Podaci su izmišljeni.
func TestFotografijaSeMiceSamoSVlastitogNacrta(t *testing.T) {
	k := novaKnjigaProba(t)
	repo := repository.NewPrijavaRepository(k.o.baza, ledger.New(k.o.baza, "test"))
	prijave := NewPrijavaService(repo, k.o.vs.users, k.o.vs, "test")
	ctx := context.Background()
	unos := UnosPrijave{Vrsta: models.PrijavaPrijava, Naslov: "Procjeđivanje", Opis: "mokro uz nožicu", Datum: time.Now()}

	nacrtAne, err := prijave.Spremi(ctx, k.ana, "", unos)
	if err != nil {
		t.Fatal(err)
	}
	if nacrtAne, err = prijave.DodajSliku(ctx, k.ana, nacrtAne.ID, "nozica.png", sivaFotografija(t)); err != nil {
		t.Fatal(err)
	}
	if nacrtAne, err = prijave.DodajSliku(ctx, k.ana, nacrtAne.ID, "kruna.png", sivaFotografija(t)); err != nil {
		t.Fatal(err)
	}
	sid, druga := nacrtAne.Slike[0].ID, nacrtAne.Slike[1].ID
	nacrtPere, err := prijave.Spremi(ctx, k.pero, "", unos)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := prijave.ObrisiSliku(ctx, k.pero, nacrtPere.ID, sid); !errors.Is(err, ErrSlikaNijeNaNacrtu) {
		t.Fatalf("fotografija s drugog nacrta: %v", err)
	}
	if b, err := repo.Slika(ctx, sid); err != nil || len(b) == 0 {
		t.Fatalf("fotografija Ane nakon pokušaja s drugog nacrta: %d bajtova, %v", len(b), err)
	}
	if _, err := prijave.ObrisiSliku(ctx, k.pero, nacrtAne.ID, sid); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("nacrt drugog vodočuvara: %v", err)
	}
	if p, err := repo.Get(ctx, nacrtAne.ID); err != nil || p == nil || len(p.Slike) != 2 {
		t.Fatalf("nacrt Ane izgubio fotografiju: %+v %v", p, err)
	}

	// kontrola: Ana miče svoju fotografiju, druga ostaje
	p, err := prijave.ObrisiSliku(ctx, k.ana, nacrtAne.ID, sid)
	if err != nil || len(p.Slike) != 1 || p.Slike[0].ID != druga {
		t.Fatalf("Ana miče svoju fotografiju: %+v %v", p, err)
	}
	if b, err := repo.Slika(ctx, sid); err != nil || len(b) != 0 {
		t.Errorf("maknuta fotografija i dalje ima %d bajtova: %v", len(b), err)
	}
	if _, err := prijave.ObrisiSliku(ctx, k.ana, nacrtAne.ID, sid); !errors.Is(err, ErrSlikaNijeNaNacrtu) {
		t.Errorf("već maknuta fotografija: %v", err)
	}
}

// Tko je uz dužnost vodočuvara i zamjenik rukovoditelja područja ne ovjerava
// svoj predani list i ne označava svoju prijavu riješenom. Tuđi list i tuđu
// prijavu na istom području i dalje ovjerava i rješava, a svoj list i dalje
// čita. Podaci su izmišljeni.
func TestDvojnaDuznostNeOvjeravaSvoje(t *testing.T) {
	k := novaKnjigaProba(t)
	ctx := context.Background()
	vs := k.o.vs
	repo := repository.NewPrijavaRepository(k.o.baza, ledger.New(k.o.baza, "test"))
	prijave := NewPrijavaService(repo, vs.users, vs, "test")

	marko := &models.User{ID: uuid.New(), Username: "mmarkic", FullName: "Marko Markić", IsActive: true, Duties: []models.Duty{
		{Role: models.RoleWaterGuard, ScopeType: models.ScopeArea, SectorID: &k.sektor, AreaID: &k.p1, IsActive: true, IsPrimary: true},
		{Role: models.RoleAreaDeputy, ScopeType: models.ScopeArea, SectorID: &k.sektor, AreaID: &k.p1, IsActive: true},
	}}
	perms := models.NewUserPermissions(*marko)
	predaj := func(u *models.User) *models.VodocuvarskiList {
		t.Helper()
		kad := vs.sada()
		l := &models.VodocuvarskiList{UserID: u.ID.String(), Ime: u.FullName, Sektor: k.sektor, AreaID: k.p1,
			Datum: kad.AddDate(0, 0, -1), Broj: 1, Od: "07:00", Do: "15:00", Opis: "obilazak nasipa", PredanoAt: &kad}
		if err := k.o.repo.Save(ctx, l); err != nil {
			t.Fatal(err)
		}
		return l
	}
	objavi := func(u *models.User) *models.PrijavaSTerena {
		t.Helper()
		p := &models.PrijavaSTerena{UserID: u.ID.String(), Ime: u.FullName, Sektor: k.sektor, AreaID: k.p1, Vrsta: models.PrijavaPrijava,
			Naslov: "Srušeno stablo", Opis: "opis", Datum: time.Now(), Status: models.PrijavaObjavljena}
		if err := repo.Save(ctx, p); err != nil {
			t.Fatal(err)
		}
		return p
	}

	if vs.SmijeOvjeriti(nil, k.listPere) || vs.SmijeOvjeriti(perms, nil) {
		t.Error("ovjera bez osobe ili bez lista")
	}
	svoj := predaj(marko)
	if vs.SmijeOvjeriti(perms, svoj) {
		t.Error("nudi ovjeru vlastitog lista")
	}
	if _, err := vs.Ovjeri(ctx, perms, marko, svoj.ID, false); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("ovjera vlastitog lista: %v", err)
	}
	if l, err := k.o.repo.Get(ctx, svoj.ID); err != nil || l == nil || l.Potvrden() {
		t.Errorf("vlastiti list ovjeren: %+v %v", l, err)
	}
	if !vs.SmijeVidjeti(perms, svoj) {
		t.Error("svoj list više ne čita")
	}
	svojaPrijava := objavi(marko)
	if prijave.SmijeRijesiti(perms, svojaPrijava) {
		t.Error("nudi rješavanje vlastite prijave")
	}
	if _, err := prijave.Rijesi(ctx, perms, marko, svojaPrijava.ID); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("rješavanje vlastite prijave: %v", err)
	}

	// kontrola: isti zamjenik ovjerava list i rješava prijavu Pere
	if l, err := vs.Ovjeri(ctx, perms, marko, predaj(k.pero).ID, false); err != nil || !l.Potvrden() {
		t.Errorf("ovjera tuđeg lista: %+v %v", l, err)
	}
	if p, err := prijave.Rijesi(ctx, perms, marko, objavi(k.pero).ID); err != nil || p.Status != models.PrijavaRijesena {
		t.Errorf("rješavanje tuđe prijave: %+v %v", p, err)
	}

	// ni globalni administrator ne ovjerava svoj list i ne rješava svoju
	// prijavu (Odluke (30. 10. 2026.), 8), a tuđi list ovjerava
	admin := models.NewUserPermissions(*marko)
	admin.IsGlobalAdmin = true
	if vs.SmijeOvjeriti(admin, svoj) {
		t.Error("globalni administrator ovjerava svoj list")
	}
	if _, err := vs.Ovjeri(ctx, admin, marko, svoj.ID, false); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("globalni administrator ovjerio svoj list: %v", err)
	}
	if prijave.SmijeRijesiti(admin, svojaPrijava) {
		t.Error("globalni administrator rješava svoju prijavu")
	}
	if !vs.SmijeVidjeti(admin, svoj) {
		t.Error("globalni administrator više ne čita svoj list")
	}
	if l, err := vs.Ovjeri(ctx, admin, marko, predaj(k.pero).ID, false); err != nil || !l.Potvrden() {
		t.Errorf("globalni administrator ne ovjerava tuđi list: %+v %v", l, err)
	}
}

// Isto vrijedi za vodočuvara koji je ujedno uprava sektora. Rješavanje s
// klasom na vlastitoj prijavi ne upisuje ni klasu; na tuđoj prijavi upiše
// klasu i riješi je. Podaci su izmišljeni.
func TestUpravaSektoraNeRjesavaSvoje(t *testing.T) {
	k := novaKnjigaProba(t)
	ctx := context.Background()
	vs := k.o.vs
	repo := repository.NewPrijavaRepository(k.o.baza, ledger.New(k.o.baza, "test"))
	prijave := NewPrijavaService(repo, vs.users, vs, "test")

	ivo := &models.User{ID: uuid.New(), Username: "iivic", FullName: "Ivo Ivić", IsActive: true, Duties: []models.Duty{
		{Role: models.RoleWaterGuard, ScopeType: models.ScopeArea, SectorID: &k.sektor, AreaID: &k.p1, IsActive: true, IsPrimary: true},
		{Role: models.RoleSectorLeader, ScopeType: models.ScopeSector, SectorID: &k.sektor, IsActive: true},
	}}
	perms := models.NewUserPermissions(*ivo)
	objavi := func(u *models.User) *models.PrijavaSTerena {
		t.Helper()
		kad := time.Now()
		p := &models.PrijavaSTerena{UserID: u.ID.String(), Ime: u.FullName, Sektor: k.sektor, AreaID: k.p1, Vrsta: models.PrijavaPrijava,
			Naslov: "Procjeđivanje", Opis: "opis", Datum: kad, Status: models.PrijavaObjavljena, ObjavljenoAt: &kad}
		if err := repo.Save(ctx, p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	svojList := &models.VodocuvarskiList{UserID: ivo.ID.String(), Sektor: k.sektor, AreaID: k.p1}
	if vs.SmijeOvjeriti(perms, svojList) {
		t.Error("uprava sektora ovjerava svoj list")
	}
	if !vs.SmijeOvjeriti(perms, k.listPere) {
		t.Error("uprava sektora ne ovjerava list Pere")
	}

	ur := Urudzba{Klasa: "325-02/26-01/0000001", Urbroj: "374-26-1"}
	svoja := objavi(ivo)
	if _, err := prijave.RijesiSUrudzbom(ctx, perms, ivo, svoja.ID, ur, nil); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("rješavanje vlastite prijave s klasom: %v", err)
	}
	if p, err := repo.Get(ctx, svoja.ID); err != nil || p == nil || p.Klasa != "" || p.Rijesena() {
		t.Errorf("vlastita prijava nakon odbijenog rješavanja: %+v %v", p, err)
	}
	if _, err := prijave.RijesiSUrudzbom(ctx, perms, ivo, "nema-je", ur, nil); err == nil {
		t.Error("rješavanje nepostojeće prijave")
	}
	// vodočuvar i dalje sam upisuje urudžbu svoje prijave
	if p, err := prijave.Urudzbiraj(ctx, perms, ivo, svoja.ID, ur, nil); err != nil || p.Klasa != ur.Klasa {
		t.Errorf("urudžba vlastite prijave: %+v %v", p, err)
	}

	// kontrola: prijavu Pere riješi i upiše joj klasu, a bez klase samo riješi
	tudja := objavi(k.pero)
	if p, err := prijave.RijesiSUrudzbom(ctx, perms, ivo, tudja.ID, ur, nil); err != nil || !p.Rijesena() || p.Klasa != ur.Klasa {
		t.Errorf("rješavanje prijave Pere s klasom: %+v %v", p, err)
	}
	if p, err := prijave.RijesiSUrudzbom(ctx, perms, ivo, objavi(k.pero).ID, Urudzba{}, nil); err != nil || !p.Rijesena() || p.Klasa != "" {
		t.Errorf("rješavanje prijave Pere bez klase: %+v %v", p, err)
	}
	if _, err := prijave.RijesiSUrudzbom(ctx, perms, ivo, objavi(k.pero).ID, Urudzba{Klasa: " "}, nil); err == nil {
		t.Error("prazna klasa prošla kao urudžba")
	}
}

// Svaka dužnost koja ovjerava ovjerava list samo u svom dosegu: rukovoditelj
// i zamjenik područja kao područje, uprava sektora i glavni rukovoditelj
// umjesto njih; uprava COP-a i centra, vodočuvar i dežurni ne ovjeravaju
// (Odluke (30. 10. 2026.), 8). Uprava knjige ostaje kakva je bila.
func TestOvjeravaDuznoscuPoDosegu(t *testing.T) {
	sektor, drugi, p1, p2 := "P", "Q", 1, 2
	l := &models.VodocuvarskiList{Sektor: sektor, AreaID: p1}
	slucajevi := []struct {
		naziv  string
		d      models.Duty
		ovjera int
		uprava bool
	}{
		{"glavni rukovoditelj", models.Duty{Role: models.RoleNationalLeader}, ovjeravaUmjesto, true},
		{"zamjenik glavnog rukovoditelja", models.Duty{Role: models.RoleNationalDeputy}, ovjeravaUmjesto, true},
		{"voditelj glavnog centra", models.Duty{Role: models.RoleMainCenterLeader}, neOvjerava, true},
		{"uprava sektora", models.Duty{Role: models.RoleSectorLeader, SectorID: &sektor}, ovjeravaUmjesto, true},
		{"zamjenik sektora", models.Duty{Role: models.RoleSectorDeputy, SectorID: &sektor}, ovjeravaUmjesto, true},
		{"zamjenik sektora za područje", models.Duty{Role: models.RoleSectorAreaDeputy, AreaID: &p1}, ovjeravaUmjesto, true},
		{"zamjenik sektora za drugo područje", models.Duty{Role: models.RoleSectorAreaDeputy, AreaID: &p2}, neOvjerava, false},
		{"uprava drugog sektora", models.Duty{Role: models.RoleSectorLeader, SectorID: &drugi}, neOvjerava, false},
		{"uprava sektora bez sektora", models.Duty{Role: models.RoleSectorLeader}, neOvjerava, false},
		{"voditelj COP-a", models.Duty{Role: models.RoleCopLeader, SectorID: &sektor}, neOvjerava, true},
		{"voditelj COP-a bez sektora", models.Duty{Role: models.RoleCopLeader}, neOvjerava, false},
		{"rukovoditelj područja", models.Duty{Role: models.RoleAreaLeader, AreaID: &p1}, ovjeravaPodrucje, true},
		{"rukovoditelj drugog područja", models.Duty{Role: models.RoleAreaLeader, AreaID: &p2}, neOvjerava, false},
		{"zamjenik područja", models.Duty{Role: models.RoleAreaDeputy, AreaID: &p1}, ovjeravaPodrucje, true},
		{"zamjenik drugog područja", models.Duty{Role: models.RoleAreaDeputy, AreaID: &p2}, neOvjerava, false},
		{"vodočuvar", models.Duty{Role: models.RoleWaterGuard, SectorID: &sektor, AreaID: &p1}, neOvjerava, false},
		{"dežurni", models.Duty{Role: models.RoleOperator, SectorID: &sektor, AreaID: &p1}, neOvjerava, false},
	}
	for _, s := range slucajevi {
		if got := ovjeraDuznosti(s.d, l); got != s.ovjera {
			t.Errorf("%s: ovjera %v, očekivano %v", s.naziv, got, s.ovjera)
		}
		if got := upravljaKnjigom(s.d, l); got != s.uprava {
			t.Errorf("%s: uprava knjige %v, očekivano %v", s.naziv, got, s.uprava)
		}
	}
}

// sivaFotografija je siva PNG slika 400 × 300
func sivaFotografija(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, 400, 300))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
