package repository

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/sadrzaj"
)

const (
	vodocuvarMarko = "01900000-0000-7000-8000-00000000a001"
	vodocuvarLuka  = "01900000-0000-7000-8000-00000000a002"
)

// vodocuvarProbe diže bazu za listove i zadatke vodočuvara
func vodocuvarProbe(t *testing.T) (*sql.DB, *VodocuvarRepository) {
	t.Helper()
	baza, rec := bazaSPodrucjem(t)
	return baza, NewVodocuvarRepository(baza, rec)
}

// listMarka je nacrt lista Marka Markića za zadani dan
func listMarka(dan time.Time, broj int) *models.VodocuvarskiList {
	return &models.VodocuvarskiList{UserID: vodocuvarMarko, Ime: "Marko Markić", Sektor: "B", AreaID: 34, Datum: dan, Broj: broj,
		Od: "07:00", Do: "15:00", Opis: "obilazak nasipa"}
}

func idListova(l []models.VodocuvarskiList) []int {
	var out []int
	for _, x := range l {
		out = append(out, x.Broj)
	}
	return out
}

// Popis listova sužava se osobom, sektorom, područjem, godinom i listovima
// koji čekaju potvrdu; najnoviji su prvi
func TestPopisListovaVodocuvara(t *testing.T) {
	_, repo := vodocuvarProbe(t)
	ctx := context.Background()
	predano := time.Date(2026, 3, 9, 16, 0, 0, 0, models.Zagreb)
	potvrdeno := time.Date(2026, 1, 2, 8, 0, 0, 0, models.Zagreb)

	nuzno(t, repo.Save(ctx, listMarka(time.Date(2026, 3, 10, 0, 0, 0, 0, models.Zagreb), 3)))
	predan := listMarka(time.Date(2026, 3, 9, 0, 0, 0, 0, models.Zagreb), 2)
	predan.PredanoAt = &predano
	nuzno(t, repo.Predaj(ctx, predan, nil))
	stari := listMarka(time.Date(2025, 12, 31, 0, 0, 0, 0, models.Zagreb), 1)
	stari.PredanoAt, stari.PotvrdenoAt, stari.PotvrdioID, stari.Potvrdio = &potvrdeno, &potvrdeno, "r-1", "Ana Anić"
	nuzno(t, repo.Predaj(ctx, stari, nil))
	luka := &models.VodocuvarskiList{UserID: vodocuvarLuka, Ime: "Luka Lukić", Sektor: "C", AreaID: 20,
		Datum: time.Date(2026, 3, 8, 0, 0, 0, 0, models.Zagreb), Broj: 7}
	nuzno(t, repo.Save(ctx, luka))

	slucajevi := []struct {
		ime  string
		f    FiltarListova
		htio []int
	}{
		{"svi, najnoviji prvi", FiltarListova{}, []int{3, 2, 7, 1}},
		{"osoba", FiltarListova{UserID: vodocuvarLuka}, []int{7}},
		{"sektor", FiltarListova{Sektor: "B"}, []int{3, 2, 1}},
		{"područje", FiltarListova{AreaID: 20}, []int{7}},
		{"godina", FiltarListova{Godina: 2025}, []int{1}},
		{"čeka potvrdu", FiltarListova{CekaPotvrdu: true}, []int{2}},
		{"ograničeno", FiltarListova{UserID: vodocuvarMarko, Limit: 2}, []int{3, 2}},
	}
	for _, s := range slucajevi {
		l, err := repo.List(ctx, s.f)
		if err != nil {
			t.Fatalf("%s: %v", s.ime, err)
		}
		if got := idListova(l); !reflect.DeepEqual(got, s.htio) {
			t.Errorf("%s: %v, htio %v", s.ime, got, s.htio)
		}
	}
	if _, err := NewVodocuvarRepository(zatvorenaBaza(t), nil).List(ctx, FiltarListova{}); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}

// Sljedeći broj lista ide po osobi i godini; najstarija godina je godina
// najstarijeg lista, a bez listova nula
func TestBrojListaINajstarijaGodina(t *testing.T) {
	_, repo := vodocuvarProbe(t)
	ctx := context.Background()
	if g := repo.NajstarijaGodina(ctx); g != 0 {
		t.Errorf("bez listova godina %d", g)
	}
	nuzno(t, repo.Save(ctx, listMarka(time.Date(2026, 3, 10, 0, 0, 0, 0, models.Zagreb), 4)))
	nuzno(t, repo.Save(ctx, listMarka(time.Date(2024, 6, 1, 0, 0, 0, 0, models.Zagreb), 9)))
	for _, s := range []struct {
		osoba        string
		godina, htio int
	}{{vodocuvarMarko, 2026, 5}, {vodocuvarMarko, 2024, 10}, {vodocuvarMarko, 2025, 1}, {vodocuvarLuka, 2026, 1}} {
		if n, err := repo.SljedeciBroj(ctx, s.osoba, s.godina); err != nil || n != s.htio {
			t.Errorf("%s %d: %d, %v; htio %d", s.osoba, s.godina, n, err, s.htio)
		}
	}
	if g := repo.NajstarijaGodina(ctx); g != 2024 {
		t.Errorf("najstarija godina %d, htio 2024", g)
	}
	if g := NewVodocuvarRepository(zatvorenaBaza(t), nil).NajstarijaGodina(ctx); g != 0 {
		t.Errorf("zatvorena baza: %d", g)
	}
}

// Očitanja dana su vodostaji koje je osoba upisala od ponoći do ponoći po
// zagrebačkom vremenu, redom; očitanja bez vodostaja i tuđa ne ulaze
func TestOcitanjaDanaVodocuvara(t *testing.T) {
	baza, repo := vodocuvarProbe(t)
	ctx := context.Background()
	const letva = "0f1e2d3c-0000-4000-8000-0000000000a1"
	kad := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := baza.Exec(`INSERT INTO stations (id, code, name, created_at, updated_at) VALUES (?, 'DM', 'Donji Miholjac', ?, ?)`, letva, kad, kad); err != nil {
		t.Fatal(err)
	}
	cm := func(n int) *int { return &n }
	for _, o := range []struct {
		id, postaja, osoba string
		u                  time.Time
		vodostaj           *int
	}{
		{"r-1", letva, vodocuvarMarko, time.Date(2026, 3, 10, 0, 30, 0, 0, models.Zagreb), cm(120)}, // 9. 3. u 23:30 UTC, ali 10. 3. u Zagrebu
		{"r-2", letva, vodocuvarMarko, time.Date(2026, 3, 10, 7, 0, 0, 0, models.Zagreb), cm(118)},
		{"r-3", "", vodocuvarMarko, time.Date(2026, 3, 10, 12, 0, 0, 0, models.Zagreb), cm(-5)},    // bez postaje
		{"r-4", letva, vodocuvarMarko, time.Date(2026, 3, 10, 14, 0, 0, 0, models.Zagreb), nil},    // bez vodostaja
		{"r-5", letva, vodocuvarMarko, time.Date(2026, 3, 11, 0, 0, 0, 0, models.Zagreb), cm(117)}, // sljedeći dan
		{"r-6", letva, vodocuvarLuka, time.Date(2026, 3, 10, 8, 0, 0, 0, models.Zagreb), cm(119)},  // tuđe
	} {
		var v any
		if o.vodostaj != nil {
			v = *o.vodostaj
		}
		if _, err := baza.Exec(`INSERT INTO readings (id, station_id, measured_at, level_cm, user_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			o.id, o.postaja, o.u.UTC(), v, o.osoba, kad, kad); err != nil {
			t.Fatal(err)
		}
	}
	l, err := repo.OcitanjaDana(ctx, vodocuvarMarko, time.Date(2026, 3, 10, 18, 0, 0, 0, models.Zagreb))
	if err != nil {
		t.Fatal(err)
	}
	if len(l) != 3 {
		t.Fatalf("tri očitanja, dobio %+v", l)
	}
	for i, htio := range []struct {
		postaja string
		cm      int
		sat     int
	}{{"Donji Miholjac", 120, 0}, {"Donji Miholjac", 118, 7}, {"", -5, 12}} {
		if l[i].Postaja != htio.postaja || l[i].Vodostaj == nil || *l[i].Vodostaj != htio.cm || l[i].Kad.In(models.Zagreb).Hour() != htio.sat {
			t.Errorf("očitanje %d: %+v, htio %+v", i, l[i], htio)
		}
	}
	if _, err := NewVodocuvarRepository(zatvorenaBaza(t), nil).OcitanjaDana(ctx, vodocuvarMarko, time.Date(2026, 3, 10, 6, 0, 0, 0, models.Zagreb)); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}

// zadaciProbe upisuje zadatke Marka i Luke oko 10. 3. 2026.
func zadaciProbe(t *testing.T, repo *VodocuvarRepository) {
	t.Helper()
	ctx := context.Background()
	dan := func(d, h, m int) time.Time { return time.Date(2026, 3, d, h, m, 0, 0, models.Zagreb) }
	obavljeno := dan(9, 15, 0)
	for _, z := range []*models.Zadatak{
		{ID: "z-plan-10", UserID: vodocuvarMarko, AreaID: 34, Tekst: "pregledati ustavu", ZadanoAt: dan(5, 9, 0), Za: dan(10, 0, 0)},
		{ID: "z-plan-11", UserID: vodocuvarMarko, AreaID: 34, Tekst: "očistiti propust", ZadanoAt: dan(5, 10, 0), Za: dan(11, 0, 0)},
		{ID: "z-vecer-10", UserID: vodocuvarMarko, AreaID: 34, Tekst: "javiti vodostaj", ZadanoAt: dan(10, 23, 30)},
		{ID: "z-ponoc-11", UserID: vodocuvarMarko, AreaID: 34, Tekst: "obići nasip", ZadanoAt: dan(11, 0, 30)},
		{ID: "z-obavljen", UserID: vodocuvarMarko, AreaID: 34, Tekst: "obilazak", ZadanoAt: dan(8, 8, 0), Za: dan(9, 0, 0),
			Status: models.ZadatakObavljen, Obavljeno: "obiđeno", ObavljenoAt: &obavljeno, ListID: "list-9",
			Od: "07:00", Do: "11:30", Udaljenost: 14.5, Obuhvat: `{"type":"LineString"}`, Izvor: "bp16:zadaci:7"},
		{ID: "z-luka", UserID: vodocuvarLuka, AreaID: 20, Tekst: "tuđi zadatak", ZadanoAt: dan(9, 8, 0), ListID: "list-9"},
	} {
		nuzno(t, repo.SaveZadatak(ctx, z))
	}
}

func idZadataka(l []models.Zadatak) []string {
	var out []string
	for _, z := range l {
		out = append(out, z.ID)
	}
	return out
}

// Otvoreni zadaci do dana: planirani za taj dan ili ranije i oni bez plana
// zadani do ponoći po zagrebačkom vremenu
func TestOtvoreniZadaciDoDana(t *testing.T) {
	_, repo := vodocuvarProbe(t)
	zadaciProbe(t, repo)
	ctx := context.Background()
	l, err := repo.OtvoreniZadaci(ctx, vodocuvarMarko, time.Date(2026, 3, 10, 6, 0, 0, 0, models.Zagreb))
	if err != nil {
		t.Fatal(err)
	}
	if got := idZadataka(l); !reflect.DeepEqual(got, []string{"z-vecer-10", "z-plan-10"}) {
		t.Errorf("otvoreni %v", got)
	}
	if _, err := NewVodocuvarRepository(zatvorenaBaza(t), nil).OtvoreniZadaci(ctx, vodocuvarMarko, time.Date(2026, 3, 10, 6, 0, 0, 0, models.Zagreb)); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}

// Zadatak se čita sa svim poljima obilaska; po izvoru se nalazi prenesen,
// a nepostojeći je nil
func TestZadatakPoIzvoruIIdentitetu(t *testing.T) {
	_, repo := vodocuvarProbe(t)
	zadaciProbe(t, repo)
	ctx := context.Background()
	z, err := repo.ZadatakPoIzvoru(ctx, "bp16:zadaci:7")
	if err != nil || z == nil || z.ID != "z-obavljen" {
		t.Fatalf("po izvoru %+v, %v", z, err)
	}
	if z.Od != "07:00" || z.Do != "11:30" || z.Udaljenost != 14.5 || z.Obuhvat != `{"type":"LineString"}` ||
		z.ObavljenoAt == nil || z.Obavljeno != "obiđeno" || z.Status != models.ZadatakObavljen || !z.Za.Equal(time.Date(2026, 3, 9, 0, 0, 0, 0, models.Zagreb)) {
		t.Errorf("zadatak %+v", z)
	}
	if z, err := repo.ZadatakPoIzvoru(ctx, "bp16:zadaci:8"); err != nil || z != nil {
		t.Errorf("nepostojeći izvor %+v, %v", z, err)
	}
	if z, err := repo.GetZadatak(ctx, "z-plan-10"); err != nil || z == nil || z.Status != models.ZadatakOtvoren || !z.Za.Equal(time.Date(2026, 3, 10, 0, 0, 0, 0, models.Zagreb)) {
		t.Errorf("planirani zadatak %+v, %v", z, err)
	}
	if z, err := repo.GetZadatak(ctx, "nema-ga"); err != nil || z != nil {
		t.Errorf("nepostojeći %+v, %v", z, err)
	}
	if _, err := NewVodocuvarRepository(zatvorenaBaza(t), nil).GetZadatak(ctx, "z-plan-10"); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}

// Zadaci lista, vodočuvara (najnoviji prvi, uz ograničenje) i razdoblja
// (po danu zadatka, kraj razdoblja ne ulazi)
func TestPopisiZadataka(t *testing.T) {
	_, repo := vodocuvarProbe(t)
	zadaciProbe(t, repo)
	ctx := context.Background()

	l, err := repo.ZadaciZaList(ctx, "list-9")
	if err != nil || !reflect.DeepEqual(idZadataka(l), []string{"z-obavljen", "z-luka"}) {
		t.Errorf("zadaci lista %v, %v", idZadataka(l), err)
	}
	l, err = repo.ZadaciVodocuvara(ctx, vodocuvarMarko, 2)
	if err != nil || !reflect.DeepEqual(idZadataka(l), []string{"z-ponoc-11", "z-vecer-10"}) {
		t.Errorf("dva najnovija %v, %v", idZadataka(l), err)
	}
	if l, _ = repo.ZadaciVodocuvara(ctx, vodocuvarMarko, 0); len(l) != 5 {
		t.Errorf("bez ograničenja %d zadataka, htio 5", len(l))
	}
	od, do := time.Date(2026, 3, 10, 0, 0, 0, 0, models.Zagreb), time.Date(2026, 3, 11, 0, 0, 0, 0, models.Zagreb)
	l, err = repo.ZadaciURazdoblju(ctx, vodocuvarMarko, od, do)
	if err != nil || !reflect.DeepEqual(idZadataka(l), []string{"z-vecer-10", "z-plan-10"}) {
		t.Errorf("razdoblje %v, %v", idZadataka(l), err)
	}
	zatvoreno := NewVodocuvarRepository(zatvorenaBaza(t), nil)
	if _, err := zatvoreno.ZadaciZaList(ctx, "list-9"); err == nil {
		t.Error("zadaci lista: zatvorena baza bez greške")
	}
	if _, err := zatvoreno.ZadaciVodocuvara(ctx, vodocuvarMarko, 0); err == nil {
		t.Error("zadaci vodočuvara: zatvorena baza bez greške")
	}
	if _, err := zatvoreno.ZadaciURazdoblju(ctx, vodocuvarMarko, od, do); err == nil {
		t.Error("razdoblje: zatvorena baza bez greške")
	}
}

// Prilog lista čita se iz spremišta; bez spremišta i bez priloga nema ničega
func TestPrilogListaIzSpremista(t *testing.T) {
	_, repo := vodocuvarProbe(t)
	ctx := context.Background()
	prijasnje := Spremiste()
	t.Cleanup(func() { SetSpremiste(prijasnje) })

	SetSpremiste(nil)
	if b, err := repo.Prilog(ctx, "foto-1"); err != nil || b != nil {
		t.Errorf("bez spremišta %q, %v", b, err)
	}
	sp, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "sadrzaj.db"))
	nuzno(t, err)
	t.Cleanup(func() { _ = sp.Zatvori() })
	SetSpremiste(sp)

	l := listMarka(time.Date(2026, 3, 10, 0, 0, 0, 0, models.Zagreb), 1)
	nuzno(t, repo.Save(ctx, l))
	nuzno(t, repo.SavePrilog(ctx, l.ID, "foto-1", []byte("\xff\xd8 ustava")))
	if b, err := repo.Prilog(ctx, "foto-1"); err != nil || string(b) != "\xff\xd8 ustava" {
		t.Errorf("prilog %q, %v", b, err)
	}
	if b, err := repo.Prilog(ctx, "foto-2"); err != nil || b != nil {
		t.Errorf("nepostojeći prilog %q, %v", b, err)
	}
}
