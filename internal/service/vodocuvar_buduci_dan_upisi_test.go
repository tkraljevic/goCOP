package service

import (
	"context"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// List koji nastaje upisom iz akta o obrani ili objavom prijave s terena za
// dan koji još nije počeo ostaje nacrt bez rednog broja, kao i list koji
// vodočuvar sam spremi unaprijed (Odluke (8. 10. 2026.), pitanja 5 i 6);
// za dan koji je počeo broj se daje kao i dosad. Osobe su izmišljene:
// vodočuvar Pero Perić, rukovoditeljica Ana Anić.

// vdVodocuvarUBazi upisuje Peru Perića, vodočuvara na području 1, i daje
// servisu korisnike iz baze
func (o *okolinaVodocuvara) vdVodocuvarUBazi(t *testing.T) (*models.User, *UserService) {
	t.Helper()
	users := repository.NewUserRepository(o.baza, ledger.New(o.baza, "test"))
	o.vs.users = NewUserService(users, nil, nil)
	pero := &models.User{Username: "pperic", FullName: "Pero Perić", IsActive: true}
	d := vdDuznost(models.RoleWaterGuard, 1, true)
	if err := users.CreateUser(pero, &d); err != nil {
		t.Fatal(err)
	}
	return pero, o.vs.users
}

func TestUpisIzAktaNaBuduciDanBezBroja(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	pero, _ := o.vdVodocuvarUBazi(t)
	danas := vdDan(time.March, 10)
	o.vs.sad = func() time.Time { return danas.Add(9 * time.Hour) }
	sutra := danas.AddDate(0, 0, 1)
	akt := &models.Akt{Sektor: "P", AreaID: 1, Broj: 3, Godina: danas.Year(), OvjerioID: "01900000-0000-7000-8000-0000000017a2", Ovjerio: "Ana Anić"}

	if upozorenja := o.vs.UpisiIzAkta(ctx, akt, sutra, "pripremno stanje"); len(upozorenja) != 0 {
		t.Fatalf("upis iz akta na sutra: %v", upozorenja)
	}
	l := o.vdListIzBaze(t, pero, sutra)
	if l == nil || l.Broj != 0 || l.Predan() || len(l.Upisi) != 1 || l.Upisi[0].Ime != "Ana Anić" {
		t.Fatalf("list iz akta za dan koji nije počeo: %+v", l)
	}
	if upozorenja := o.vs.UpisiIzAkta(ctx, akt, danas, "pripremno stanje"); len(upozorenja) != 0 {
		t.Fatalf("upis iz akta na danas: %v", upozorenja)
	}
	if l := o.vdListIzBaze(t, pero, danas); l == nil || l.Broj != 1 {
		t.Errorf("današnji list iz akta dobiva broj: %+v", l)
	}
}

// Akt o obrani koji vrijedi od dana dalje od 30 dana unaprijed ne otvara
// dnevni list vodočuvara, jer list za budući dan smije postojati najviše 30
// dana unaprijed (Odluke (8. 10. 2026.), pitanje 5): umjesto lista vraća se
// upozorenje. Za 30. dan unaprijed list nastaje kao nacrt bez broja.
func TestUpisIzAktaNajvise30DanaUnaprijed(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	pero, _ := o.vdVodocuvarUBazi(t)
	danas := vdDan(time.March, 10)
	o.vs.sad = func() time.Time { return danas.Add(9 * time.Hour) }
	akt := &models.Akt{Sektor: "P", AreaID: 1, Broj: 4, Godina: danas.Year(), OvjerioID: "01900000-0000-7000-8000-0000000017a2", Ovjerio: "Ana Anić"}

	predaleko := danas.AddDate(0, 0, models.NajdaljePlaniranje+1)
	upozorenja := o.vs.UpisiIzAkta(ctx, akt, predaleko, "pripremno stanje")
	ocekivano := "dnevnici vodočuvara: dnevni list za budući dan priprema se najviše 30 dana unaprijed, do " +
		danas.AddDate(0, 0, models.NajdaljePlaniranje).Format("02.01.2006.")
	if len(upozorenja) != 1 || upozorenja[0] != ocekivano {
		t.Errorf("upis iz akta dalje od 30 dana: %q", upozorenja)
	}
	if l := o.vdListIzBaze(t, pero, predaleko); l != nil {
		t.Errorf("list dalje od 30 dana unaprijed ne smije nastati: %+v", l)
	}

	zadnji := danas.AddDate(0, 0, models.NajdaljePlaniranje)
	if upozorenja := o.vs.UpisiIzAkta(ctx, akt, zadnji, "pripremno stanje"); len(upozorenja) != 0 {
		t.Fatalf("upis iz akta 30 dana unaprijed: %v", upozorenja)
	}
	if l := o.vdListIzBaze(t, pero, zadnji); l == nil || l.Broj != 0 || len(l.Upisi) != 1 {
		t.Errorf("list 30 dana unaprijed iz akta: %+v", l)
	}
}

func TestObjavaPrijaveZaSutraNaNacrtBezBroja(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	pero, users := o.vdVodocuvarUBazi(t)
	// prijava pazi na stvarni sat (dan događaja najviše sutra), pa i dnevnik
	o.vs.sad = nil
	prijave := NewPrijavaService(repository.NewPrijavaRepository(o.baza, ledger.New(o.baza, "test")), users, o.vs, "cvor-probni")
	pdf := func(*models.PrijavaSTerena) ([]byte, error) {
		return []byte("%PDF-1.4\n% probna prijava\n%%EOF\n"), nil
	}
	n := time.Now().In(models.Zagreb)
	danas := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, models.Zagreb)
	objavi := func(dan time.Time) *models.PrijavaSTerena {
		t.Helper()
		p, err := prijave.Spremi(ctx, pero, "", UnosPrijave{Vrsta: models.PrijavaObavijest, Naslov: "Oštećena rampa", Opis: "odrezana brava na rampi nasipa", Datum: dan})
		if err != nil {
			t.Fatal(err)
		}
		p, upozorenje, err := prijave.Objavi(ctx, pero, p.ID, pdf)
		if err != nil || upozorenje != "" {
			t.Fatalf("objava prijave za %s: %v %s", dan.Format("02.01.2006."), err, upozorenje)
		}
		return p
	}

	sutra := danas.AddDate(0, 0, 1)
	p := objavi(sutra)
	l := o.vdListIzBaze(t, pero, sutra)
	if p.ListBroj != 0 || l == nil || p.ListID != l.ID || l.Broj != 0 || len(l.Prijave) != 1 {
		t.Fatalf("prijava za sutra: broj lista %d, list %+v", p.ListBroj, l)
	}
	if p := objavi(danas); p.ListBroj != 1 {
		t.Errorf("prijava za danas: broj lista %d, očekivan 1", p.ListBroj)
	}
}
