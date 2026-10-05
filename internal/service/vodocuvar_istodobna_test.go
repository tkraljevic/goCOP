package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// verzijeListova broji verzije listova u knjizi
func (o *okolinaVodocuvara) verzijeListova(t *testing.T) int {
	t.Helper()
	var n int
	if err := o.baza.QueryRow(`SELECT COUNT(*) FROM record_versions WHERE entity = ?`, repository.EntityVodocuvarski).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Isti list predaje se istodobno iz dvije kartice: obje su pročitale list
// nepredan, a druga ga preda nakon što je predaja prve već pročitala list,
// a prije njezina upisa. Predaja prve se odbija kao predaja predanog lista i
// ne upisuje ništa: ostaju vrijeme predaje, opis i zadaci druge kartice, a u
// knjizi nema nove verzije. Isto vrijedi kad lista još nema ni kao nacrta,
// pa bi svaka kartica upisala svoj list za isti dan.
func TestIstodobnaPredajaIstogLista(t *testing.T) {
	for _, slucaj := range []struct {
		naziv string
		nacrt bool
	}{
		{"nacrt već spremljen", true},
		{"bez nacrta", false},
	} {
		t.Run(slucaj.naziv, func(t *testing.T) {
			o := novaOkolinaVodocuvara(t)
			ctx := context.Background()
			u := vdVodocuvar()
			dan := vdDan(time.March, 10)
			z := o.vdZadatak(t, u, "pregledati ustavu", dan)
			if slucaj.nacrt {
				if _, err := o.vs.Spremi(ctx, u, dan, vdUnos("nacrt"), false); err != nil {
					t.Fatal(err)
				}
			}

			drugaKartica := vdUnos("obilazak iz druge kartice")
			drugaKartica.Zadaci = map[string]UnosZadatka{z.ID: {Status: models.ZadatakOdbacen, Obavljeno: "ustava srušena"}}
			var druga *models.VodocuvarskiList
			pozvano, verzije := 0, 0
			o.vs.prijeUpisaPredaje = func(ctx context.Context) {
				pozvano++
				if pozvano > 1 {
					return
				}
				var err error
				if druga, err = o.vs.Spremi(ctx, u, dan, drugaKartica, true); err != nil {
					t.Fatal(err)
				}
				verzije = o.verzijeListova(t)
			}
			prvaKartica := vdUnos("obilazak iz prve kartice")
			prvaKartica.Zadaci = map[string]UnosZadatka{z.ID: {Status: models.ZadatakObavljen, Obavljeno: "pregledana"}}
			l, err := o.vs.Spremi(ctx, u, dan, prvaKartica, true)
			vdGreska(t, err, "list od "+dan.Format("02.01.2006.")+" je već predan i više se ne mijenja")
			if !errors.Is(err, repository.ErrListPredan) {
				t.Errorf("greška nije ErrListPredan: %v", err)
			}
			if l != nil {
				t.Errorf("odbijena predaja vratila je list: %+v", l)
			}
			if druga == nil || !druga.Predan() {
				t.Fatal("druga kartica nije predala list")
			}

			uBazi := o.vdListIzBaze(t, u, dan)
			if uBazi.ID != druga.ID || uBazi.Opis != "obilazak iz druge kartice" || !uBazi.PredanoAt.Equal(*druga.PredanoAt) {
				t.Errorf("predan list prepisan: %s %q %v (druga kartica %s %v)", uBazi.ID, uBazi.Opis, uBazi.PredanoAt, druga.ID, druga.PredanoAt)
			}
			if nl := vdNaListu(uBazi, z.ID); nl == nil || nl.Status != models.ZadatakOdbacen || nl.Obavljeno != "ustava srušena" {
				t.Errorf("zadatak na listu: %+v", uBazi.Zadaci)
			}
			if ev := o.vdZadatakIzBaze(t, z.ID); ev.Status != models.ZadatakOdbacen || ev.ListID != druga.ID {
				t.Errorf("evidencija: %+v", ev)
			}
			var listova int
			if err := o.baza.QueryRow(`SELECT COUNT(*) FROM vodocuvarski_listovi WHERE user_id = ?`, u.ID.String()).Scan(&listova); err != nil {
				t.Fatal(err)
			}
			if listova != 1 {
				t.Errorf("listova za dan: %d, očekivan jedan", listova)
			}
			// odbijena predaja ne upisuje verziju u knjigu
			if n := o.verzijeListova(t); n != verzije {
				t.Errorf("odbijena predaja upisala je verziju u knjigu: %d → %d", verzije, n)
			}
		})
	}
}

// Nacrt se sprema iz jedne kartice, a druga kartica preda list nakon što je
// prva pročitala list nepredan, a prije njezina upisa. Nacrt (i nacrt koji
// ostaje kad predaja ne prođe provjeru) ne upisuje se preko predanog lista:
// spremanje se odbija, a predan list ostaje kako ga je predala druga
// kartica, bez nove verzije u knjizi. Isto vrijedi kad lista još nema ni
// kao nacrta, pa bi prva kartica upisala drugi list za isti dan.
func TestNacrtNePrepisujeListPredanUMedjuvremenu(t *testing.T) {
	for _, slucaj := range []struct {
		naziv  string
		nacrt  bool
		predaj bool
	}{
		{"spremanje nacrta", true, false},
		{"spremanje nacrta, bez ranijeg nacrta", false, false},
		{"nacrt neuspjele predaje", true, true},
		{"nacrt neuspjele predaje, bez ranijeg nacrta", false, true},
	} {
		t.Run(slucaj.naziv, func(t *testing.T) {
			o := novaOkolinaVodocuvara(t)
			ctx := context.Background()
			u := vdVodocuvar()
			dan := vdDan(time.March, 10)
			if slucaj.nacrt {
				if _, err := o.vs.Spremi(ctx, u, dan, vdUnos("nacrt"), false); err != nil {
					t.Fatal(err)
				}
			}
			druga := o.predajaIzDrugeKartice(t, u, dan)

			// predaja bez opisa ne prolazi provjeru, pa se upisano sprema kao nacrt
			prvaKartica := vdUnos("obilazak iz prve kartice")
			if slucaj.predaj {
				prvaKartica.Opis = ""
			}
			l, err := o.vs.Spremi(ctx, u, dan, prvaKartica, slucaj.predaj)
			vdGreska(t, err, "list od "+dan.Format("02.01.2006.")+" je u međuvremenu predan; upisano nije spremljeno")
			if !errors.Is(err, repository.ErrListPredanUMedjuvremenu) {
				t.Errorf("greška nije ErrListPredanUMedjuvremenu: %v", err)
			}
			if l != nil {
				t.Errorf("odbijeno spremanje vratilo je list: %+v", l)
			}
			o.provjeriPredanuDruguKarticu(t, u, dan, druga)
		})
	}
}

// Rukovoditelj upisuje bilješku u dnevnik vodočuvara, a vodočuvar u drugoj
// kartici preda list nakon što je upis pročitao list nepredan, a prije
// njegova upisa. Upis se odbija i ništa se ne upisuje: list ostaje predan,
// bez bilješke i bez nove verzije u knjizi (bilješka se može upisati
// ponovno, na predan list).
func TestUpisRukovoditeljaNePrepisujeListPredanUMedjuvremenu(t *testing.T) {
	for _, slucaj := range []struct {
		naziv string
		nacrt bool
	}{
		{"nacrt već spremljen", true},
		{"bez nacrta", false},
	} {
		t.Run(slucaj.naziv, func(t *testing.T) {
			o := novaOkolinaVodocuvara(t)
			ctx := context.Background()
			users := repository.NewUserRepository(o.baza, ledger.New(o.baza, "test"))
			o.vs.users = NewUserService(users, nil, nil)
			novi := &models.User{Username: "pperic", FullName: "Pero Perić", IsActive: true}
			d := vdDuznost(models.RoleWaterGuard, 1, true)
			if err := users.CreateUser(novi, &d); err != nil {
				t.Fatal(err)
			}
			u, err := users.GetUserByID(novi.ID)
			if err != nil || u == nil {
				t.Fatalf("vodočuvar: %v %v", u, err)
			}
			dan := vdDan(time.March, 10)
			if slucaj.nacrt {
				if _, err := o.vs.Spremi(ctx, u, dan, vdUnos("nacrt"), false); err != nil {
					t.Fatal(err)
				}
			}
			druga := o.predajaIzDrugeKartice(t, u, dan)

			rukovoditelj := vdPperic(vdDuznost(models.RoleAreaLeader, 1, true))
			rukovoditelj.Username, rukovoditelj.FullName = "mmarkic", "Marko Markić"
			l, err := o.vs.Upisi(ctx, models.NewUserPermissions(*rukovoditelj), rukovoditelj, u.ID.String(), dan, "obilazak nasipa s rukovoditeljem")
			vdGreska(t, err, "list od "+dan.Format("02.01.2006.")+" je u međuvremenu predan; upisano nije spremljeno")
			if !errors.Is(err, repository.ErrListPredanUMedjuvremenu) {
				t.Errorf("greška nije ErrListPredanUMedjuvremenu: %v", err)
			}
			if l != nil {
				t.Errorf("odbijen upis vratio je list: %+v", l)
			}
			if uBazi := o.provjeriPredanuDruguKarticu(t, u, dan, druga); len(uBazi.Upisi) != 0 {
				t.Errorf("bilješka upisana na predan list: %+v", uBazi.Upisi)
			}
		})
	}
}

// predanoUDrugojKartici je list kako ga je predala druga kartica, s brojem
// verzija listova u knjizi nakon predaje
type predanoUDrugojKartici struct {
	list    *models.VodocuvarskiList
	verzije int
}

// predajaIzDrugeKartice postavlja drugu karticu koja list vodočuvara preda
// između čitanja lista i upisa koji nije predaja (nacrt, upis rukovoditelja)
func (o *okolinaVodocuvara) predajaIzDrugeKartice(t *testing.T, u *models.User, dan time.Time) *predanoUDrugojKartici {
	t.Helper()
	druga := &predanoUDrugojKartici{}
	pozvano := 0
	o.vs.prijeSpremanja = func(ctx context.Context) {
		pozvano++
		if pozvano > 1 {
			return
		}
		var err error
		if druga.list, err = o.vs.Spremi(ctx, u, dan, vdUnos("obilazak iz druge kartice"), true); err != nil {
			t.Fatal(err)
		}
		druga.verzije = o.verzijeListova(t)
	}
	return druga
}

// provjeriPredanuDruguKarticu provjerava da je u bazi samo list kako ga je
// predala druga kartica, bez nove verzije u knjizi
func (o *okolinaVodocuvara) provjeriPredanuDruguKarticu(t *testing.T, u *models.User, dan time.Time, druga *predanoUDrugojKartici) *models.VodocuvarskiList {
	t.Helper()
	if druga.list == nil || !druga.list.Predan() {
		t.Fatal("druga kartica nije predala list")
	}
	uBazi := o.vdListIzBaze(t, u, dan)
	if uBazi == nil || uBazi.ID != druga.list.ID || uBazi.Opis != "obilazak iz druge kartice" || !uBazi.Predan() || !uBazi.PredanoAt.Equal(*druga.list.PredanoAt) {
		t.Fatalf("predan list prepisan: %+v (druga kartica %s %v)", uBazi, druga.list.ID, druga.list.PredanoAt)
	}
	var listova int
	if err := o.baza.QueryRow(`SELECT COUNT(*) FROM vodocuvarski_listovi WHERE user_id = ?`, u.ID.String()).Scan(&listova); err != nil {
		t.Fatal(err)
	}
	if listova != 1 {
		t.Errorf("listova za dan: %d, očekivan jedan", listova)
	}
	// odbijen upis ne upisuje verziju u knjigu
	if n := o.verzijeListova(t); n != druga.verzije {
		t.Errorf("odbijen upis upisao je verziju u knjigu: %d → %d", druga.verzije, n)
	}
	return uBazi
}

// verzijeBrisanja broji verzije lista i njegova izvornika u knjizi; uz
// arhivirane još i kanal zadnje arhivirane verzije izvornika
func (o *okolinaVodocuvara) verzijeBrisanja(t *testing.T, listID string) (lista, izvornika, arhivirano int, kanalIzvornika string) {
	t.Helper()
	if err := o.baza.QueryRow(`SELECT
		COUNT(*) FILTER (WHERE entity = ?),
		COUNT(*) FILTER (WHERE entity = ?),
		COUNT(*) FILTER (WHERE archived = 1),
		COALESCE(MAX(CASE WHEN entity = ? AND archived = 1 THEN channel END), '')
		FROM record_versions WHERE entity_id = ?`,
		repository.EntityVodocuvarski, repository.EntityVodocuvarskiIzvornici, repository.EntityVodocuvarskiIzvornici, listID).
		Scan(&lista, &izvornika, &arhivirano, &kanalIzvornika); err != nil {
		t.Fatal(err)
	}
	return
}

// Vodočuvar briše nacrt u jednoj kartici, a u drugoj ga preda (i potpiše
// izvornik) nakon što je brisanje pročitalo list nepredan, a prije brisanja.
// Brisanje se odbija i ništa se ne briše: predan list i njegov potpisani
// izvornik ostaju, a u knjizi nema arhivirane verzije koja bi brisanje
// odnijela drugim čvorovima.
func TestBrisanjeNeBriseListPredanUMedjuvremenu(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	u := vdVodocuvar()
	dan := vdDan(time.March, 10)
	nacrt, err := o.vs.Spremi(ctx, u, dan, vdUnos("nacrt"), false)
	if err != nil {
		t.Fatal(err)
	}
	var druga *models.VodocuvarskiList
	var lista, izvornika int
	o.vs.prijeBrisanja = func(ctx context.Context) {
		if druga, err = o.vs.Spremi(ctx, u, dan, vdUnos("obilazak iz druge kartice"), true); err != nil {
			t.Fatal(err)
		}
		if err := o.vs.SpremiIzvornik(ctx, druga.ID, []byte("%PDF-1.4 potpisan list")); err != nil {
			t.Fatal(err)
		}
		lista, izvornika, _, _ = o.verzijeBrisanja(t, druga.ID)
	}

	err = o.vs.Obrisi(ctx, u, nacrt.ID)
	vdGreska(t, err, "list od "+dan.Format("02.01.2006.")+" je u međuvremenu predan i nije obrisan")
	if !errors.Is(err, repository.ErrListPredanNijeObrisan) {
		t.Errorf("greška nije ErrListPredanNijeObrisan: %v", err)
	}
	if druga == nil || druga.ID != nacrt.ID {
		t.Fatalf("druga kartica nije predala isti list: %+v", druga)
	}
	if uBazi := o.vdListIzBaze(t, u, dan); uBazi == nil || uBazi.ID != nacrt.ID || !uBazi.Predan() || uBazi.Opis != "obilazak iz druge kartice" {
		t.Fatalf("predan list obrisan ili prepisan: %+v", uBazi)
	}
	if iz, err := o.repo.GetIzvornik(ctx, nacrt.ID); err != nil || iz == nil {
		t.Errorf("izvornik predanog lista obrisan: %v %v", iz, err)
	}
	if l, iz, arh, _ := o.verzijeBrisanja(t, nacrt.ID); l != lista || iz != izvornika || arh != 0 {
		t.Errorf("odbijeno brisanje upisalo je verzije u knjigu: lista %d → %d, izvornika %d → %d, arhiviranih %d", lista, l, izvornika, iz, arh)
	}
}

// Nacrt se briše zajedno sa svojim izvornikom, u jednoj transakciji: obje
// verzije u knjizi arhivirane su, izvornik u kanalu lista (područje i
// godina), iako lista više nema da se kanal pročita iz tablice.
func TestObrisiNacrtSIzvornikom(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	u := vdVodocuvar()
	dan := vdDan(time.March, 10)
	nacrt, err := o.vs.Spremi(ctx, u, dan, vdUnos("nacrt"), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.vs.SpremiIzvornik(ctx, nacrt.ID, []byte("%PDF-1.4 zaostali izvornik")); err != nil {
		t.Fatal(err)
	}
	if err := o.vs.Obrisi(ctx, u, nacrt.ID); err != nil {
		t.Fatal(err)
	}
	if l := o.vdListIzBaze(t, u, dan); l != nil {
		t.Errorf("nacrt nije obrisan: %+v", l)
	}
	if iz, err := o.repo.GetIzvornik(ctx, nacrt.ID); err != nil || iz != nil {
		t.Errorf("izvornik nije obrisan: %v %v", iz, err)
	}
	_, _, arh, kanal := o.verzijeBrisanja(t, nacrt.ID)
	if arh != 2 {
		t.Errorf("arhiviranih verzija: %d, očekivane dvije (list i izvornik)", arh)
	}
	if want := ledger.ChannelFor(ledger.ChannelVodocuvar, nacrt.AreaID, 2026); kanal != want {
		t.Errorf("kanal arhiviranog izvornika: %q, očekivan %q", kanal, want)
	}
}

// Isti nacrt briše se istodobno iz dvije kartice: druga ga obriše nakon
// što je prva pročitala list, a prije njezina brisanja. Prvo brisanje nema
// što obrisati i ne javlja grešku, a u knjizi je brisanje upisano samo
// jednom.
func TestIstodobnoBrisanjeIstogNacrta(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	u := vdVodocuvar()
	dan := vdDan(time.March, 10)
	nacrt, err := o.vs.Spremi(ctx, u, dan, vdUnos("nacrt"), false)
	if err != nil {
		t.Fatal(err)
	}
	pozvano := 0
	o.vs.prijeBrisanja = func(ctx context.Context) {
		pozvano++
		if pozvano > 1 {
			return
		}
		if err := o.vs.Obrisi(ctx, u, nacrt.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := o.vs.Obrisi(ctx, u, nacrt.ID); err != nil {
		t.Fatalf("brisanje već obrisanog nacrta: %v", err)
	}
	if l := o.vdListIzBaze(t, u, dan); l != nil {
		t.Errorf("nacrt nije obrisan: %+v", l)
	}
	if _, _, arh, _ := o.verzijeBrisanja(t, nacrt.ID); arh != 1 {
		t.Errorf("arhiviranih verzija: %d, očekivana jedna", arh)
	}
}
