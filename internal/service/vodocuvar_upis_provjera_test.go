package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"gocop/internal/models"
)

// Odbijen upis ne smije promijeniti ni sadržaj lista ni knjigu verzija.
func provjeriListBezPromjene(t *testing.T, k *knjigaProba, prije *models.VodocuvarskiList, verzije int) {
	t.Helper()
	poslije, err := k.o.repo.Get(context.Background(), prije.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(prije, poslije) {
		t.Errorf("odbijen upis promijenio je list %s", prije.ID)
	}
	if n := k.o.verzijeListova(t); n != verzije {
		t.Errorf("odbijen upis promijenio je broj verzija: %d, prije %d", n, verzije)
	}
}

func procitajListZaProvjeru(t *testing.T, k *knjigaProba, id string) *models.VodocuvarskiList {
	t.Helper()
	l, err := k.o.repo.Get(context.Background(), id)
	if err != nil || l == nil {
		t.Fatalf("čitanje lista %s: %v", id, err)
	}
	return l
}

// Dva predana lista istog dana mogu nositi različita područja. Ovlast se
// provjerava prema odabranom listu, ne prema zadanoj stavci ni današnjem
// zaduženju vodočuvara. Uspješan upis ne smije promijeniti drugi list.
func TestUpisiNaListPravoNaOdabranomPredanomListu(t *testing.T) {
	for _, s := range []struct {
		naziv        string
		podrucje     int
		odaberiDrugi bool
		dopusteno    bool
	}{
		{"prvi rukovoditelj na prvom listu", 1, false, true},
		{"prvi rukovoditelj na drugom listu", 1, true, false},
		{"drugi rukovoditelj na prvom listu", 2, false, false},
		{"drugi rukovoditelj na drugom listu", 2, true, true},
	} {
		t.Run(s.naziv, func(t *testing.T) {
			k := novaKnjigaProba(t)
			ctx := context.Background()
			dan := vdDan(time.March, 10)
			k.o.naDan(dan)
			prvi := k.o.dvojnik(t, k.pero, dan, 1, "prvi list", true)
			drugi := k.o.dvojnik(t, k.pero, dan, 2, "drugi list", true)
			drugi.AreaID = 2
			if err := k.o.repo.Save(ctx, drugi); err != nil {
				t.Fatal(err)
			}
			odabrani, netaknuti := prvi, drugi
			if s.odaberiDrugi {
				odabrani, netaknuti = drugi, prvi
			}
			prije := procitajListZaProvjeru(t, k, odabrani.ID)
			drugiPrije := procitajListZaProvjeru(t, k, netaknuti.ID)
			verzije := k.o.verzijeListova(t)
			p := k.osoba(models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &s.podrucje})
			upisan, err := k.o.vs.UpisiNaList(ctx, p, &p.User, k.pero.ID.String(), dan.UTC(), odabrani.ID, "obilazak potvrđen")
			if !s.dopusteno {
				if !errors.Is(err, ErrUnauthorized) || upisan != nil {
					t.Fatalf("očekivano odbijanje, list=%v greška=%v", upisan, err)
				}
				provjeriListBezPromjene(t, k, prije, verzije)
			} else {
				if err != nil || upisan == nil || upisan.ID != odabrani.ID {
					t.Fatalf("upis na odabrani list: %v %v", upisan, err)
				}
				if l := procitajListZaProvjeru(t, k, odabrani.ID); len(l.Upisi) != 1 || l.Upisi[0].Biljeska {
					t.Fatalf("naredba nije spremljena na odabrani list: %+v", l.Upisi)
				}
				verzije++
			}
			provjeriListBezPromjene(t, k, drugiPrije, verzije)
		})
	}
}

func TestUpisiNaListOpozvanoIIstekloZaduzenje(t *testing.T) {
	for _, s := range []struct {
		naziv   string
		aktivno bool
		isteklo bool
	}{
		{"opozvano", false, false},
		{"isteklo", true, true},
	} {
		t.Run(s.naziv, func(t *testing.T) {
			k := novaKnjigaProba(t)
			dan := vdDan(time.March, 10)
			k.o.naDan(dan)
			l := k.o.dvojnik(t, k.pero, dan, 1, "obilazak nasipa", true)
			prije := procitajListZaProvjeru(t, k, l.ID)
			verzije := k.o.verzijeListova(t)
			d := vdDuznost(models.RoleAreaLeader, 1, true)
			d.IsActive = s.aktivno
			if s.isteklo {
				istek := time.Now().Add(-time.Hour)
				d.ExpiresAt = &istek
			}
			u := vdPperic(d)
			p := models.NewUserPermissions(*u)
			upisan, err := k.o.vs.UpisiNaList(context.Background(), p, u, k.pero.ID.String(), dan, l.ID, "zabranjen upis")
			if !errors.Is(err, ErrUnauthorized) || upisan != nil {
				t.Fatalf("očekivano odbijanje, list=%v greška=%v", upisan, err)
			}
			provjeriListBezPromjene(t, k, prije, verzije)
		})
	}
}

func TestUpisiNaListBiljeskaINaredbaCuvajuPredaniList(t *testing.T) {
	k := novaKnjigaProba(t)
	ctx := context.Background()
	dan := vdDan(time.March, 10)
	k.o.naDan(dan)
	l := k.o.dvojnik(t, k.pero, dan, 1, "obilazak nasipa", true)
	prije := procitajListZaProvjeru(t, k, l.ID)
	verzije := k.o.verzijeListova(t)
	for _, s := range []struct {
		uloga    models.Role
		biljeska bool
	}{
		{models.RoleOperator, true},
		{models.RoleAreaLeader, false},
	} {
		p := k.osoba(models.Duty{Role: s.uloga, ScopeType: models.ScopeArea, AreaID: &k.p1})
		if _, err := k.o.vs.UpisiNaList(ctx, p, &p.User, k.pero.ID.String(), dan, l.ID, "  pregled obavljen  "); err != nil {
			t.Fatal(err)
		}
		poslije := procitajListZaProvjeru(t, k, l.ID)
		if len(poslije.Upisi) != len(prije.Upisi)+1 {
			t.Fatalf("broj upisa: %d", len(poslije.Upisi))
		}
		upis := poslije.Upisi[len(poslije.Upisi)-1]
		if upis.Biljeska != s.biljeska || upis.UserID != p.User.ID.String() || upis.Tekst != "pregled obavljen" {
			t.Errorf("vrsta, autor ili tekst upisa: %+v", upis)
		}
		prije.Upisi = append(prije.Upisi, upis)
		prije.UpdatedAt = poslije.UpdatedAt
		if !reflect.DeepEqual(prije, poslije) {
			t.Error("upis je promijenio i druga polja predanog lista")
		}
		verzije++
		if n := k.o.verzijeListova(t); n != verzije {
			t.Errorf("broj verzija: %d, očekivano %d", n, verzije)
		}
	}
}

// Granica se provjerava kroz javni servis, s datumom i satom u UTC-u:
// neposredno prije lokalne ponoći otvara list, od ponoći više ne.
func TestUpisiNaListGranicaPonoci(t *testing.T) {
	for _, pomak := range []time.Duration{-time.Second, 0} {
		t.Run(pomak.String(), func(t *testing.T) {
			k := novaKnjigaProba(t)
			dan := vdDan(time.March, 10)
			k.o.vs.sad = func() time.Time { return dan.AddDate(0, 0, 1).Add(pomak).UTC() }
			p := k.osoba(models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &k.p1})
			l, err := k.o.vs.UpisiNaList(context.Background(), p, &p.User, k.pero.ID.String(), dan.UTC(), "", "upis prije kraja dana")
			if pomak < 0 {
				if err != nil || l == nil || l.Broj != 1 {
					t.Fatalf("dan još traje: %v %v", l, err)
				}
				return
			}
			if !errors.Is(err, ErrProsliDan) || l != nil {
				t.Fatalf("dan je prošao: %v %v", l, err)
			}
			if n := k.o.verzijeListova(t); n != 0 {
				t.Errorf("odbijen upis ostavio je %d verzija", n)
			}
			if l := k.o.vdListIzBaze(t, k.pero, dan); l != nil {
				t.Error("odbijen upis otvorio je list")
			}
		})
	}
}

func TestUpisiNaListNeuspjelaVerzijaVracaList(t *testing.T) {
	k := novaKnjigaProba(t)
	ctx := context.Background()
	dan := vdDan(time.March, 10)
	k.o.naDan(dan)
	l := k.o.dvojnik(t, k.pero, dan, 1, "obilazak nasipa", true)
	prije := procitajListZaProvjeru(t, k, l.ID)
	verzije := k.o.verzijeListova(t)
	// Kvar tek nakon upisa lista unutar transakcije, pri zapisu verzije.
	if _, err := k.o.baza.Exec(`CREATE TRIGGER probni_kvar_verzije BEFORE INSERT ON record_versions BEGIN SELECT RAISE(ABORT, 'probni kvar knjige verzija'); END`); err != nil {
		t.Fatal(err)
	}
	p := k.osoba(models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &k.p1})
	upisan, err := k.o.vs.UpisiNaList(ctx, p, &p.User, k.pero.ID.String(), dan, l.ID, "upis koji se mora povući")
	vdGreska(t, err, "probni kvar knjige verzija")
	if upisan != nil {
		t.Error("neuspjeh je vratio spremljeni list")
	}
	provjeriListBezPromjene(t, k, prije, verzije)
}

// Osobe i područja su izmišljeni. Drugi upis završava između čitanja i
// spremanja prvog, kao u dvije kartice; ne ovisimo o rasporedu gorutina.
func TestUpisiNaListIstodobneBiljeskePredanogLista(t *testing.T) {
	k := novaKnjigaProba(t)
	ctx := context.Background()
	dan := vdDan(time.March, 10)
	k.o.naDan(dan)
	l := k.o.dvojnik(t, k.pero, dan, 1, "obilazak nasipa", true)
	prvi := k.osoba(models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &k.p1})
	drugi := k.osoba(models.Duty{Role: models.RoleOperator, ScopeType: models.ScopeSector})
	verzije := k.o.verzijeListova(t)
	k.o.vs.prijeSpremanja = func(ctx context.Context) {
		k.o.vs.prijeSpremanja = nil
		if _, err := k.o.vs.UpisiNaList(ctx, drugi, &drugi.User, k.pero.ID.String(), dan, l.ID, "bilješka operatera"); err != nil {
			t.Fatalf("drugi upis: %v", err)
		}
	}
	if _, err := k.o.vs.UpisiNaList(ctx, prvi, &prvi.User, k.pero.ID.String(), dan, l.ID, "naredba rukovoditelja"); err != nil {
		t.Fatalf("prvi upis: %v", err)
	}
	spremljen, err := k.o.repo.Get(ctx, l.ID)
	if err != nil || spremljen == nil {
		t.Fatalf("čitanje nakon oba upisa: %v", err)
	}
	if len(spremljen.Upisi) != 2 {
		t.Errorf("oba poziva uspjela, ali nisu sačuvana oba upisa: %+v", spremljen.Upisi)
	}
	if n := k.o.verzijeListova(t); n != verzije+2 {
		t.Errorf("verzije nakon oba uspješna upisa: %d, očekivano %d", n, verzije+2)
	}
}
