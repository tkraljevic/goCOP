package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"

	"github.com/google/uuid"
)

// noveNavike otvara bazu s očitanjima i vraća spremište i upis očitanja
func noveNavike(t *testing.T) (*ReadingRepository, func(stanica, korisnik, ocitao string, kad time.Time)) {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "navike.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	repo := NewReadingRepository(baza, ledger.New(baza, "cvor"))
	cm := 100
	upisi := func(stanica, korisnik, ocitao string, kad time.Time) {
		t.Helper()
		rd := models.Reading{ID: uuid.New(), StationID: stanica, MeasuredAt: kad, LevelCm: &cm,
			Source: models.ReadingSourceManual, Origin: models.ReadingOriginGoCOP, UserID: korisnik, Observer: ocitao}
		if _, err := repo.ImportBatch(context.Background(), []models.Reading{rd}); err != nil {
			t.Fatal(err)
		}
	}
	return repo, upisi
}

// Navike se vežu uz korisnika; ime očitavača vrijedi samo za stara
// očitanja bez korisnika, pa imenjak s drugim računom ne ulazi u navike.
func TestNavikeVezaneUzKorisnika(t *testing.T) {
	repo, upisi := noveNavike(t)
	pero, imenjak := uuid.NewString(), uuid.NewString()
	d := time.Now().In(models.Zagreb).AddDate(0, 0, -2)
	u := func(sat int) time.Time { return time.Date(d.Year(), d.Month(), d.Day(), sat, 0, 0, 0, models.Zagreb) }
	upisi("primjerovo", pero, "Pero Perić", u(7))
	upisi("probno", imenjak, "Pero Perić", u(8))
	upisi("uzvodna", "", "Pero Perić", u(9))
	upisi("granica", "", "Ivo Ivić", u(10))
	upisi("staro", pero, "Pero Perić", time.Now().AddDate(0, 0, -91))

	navike, err := repo.HabitsFor(context.Background(), pero, "Pero Perić", time.Now().AddDate(0, 0, -90))
	if err != nil {
		t.Fatal(err)
	}
	if len(navike) != 2 || navike["station:primjerovo"].Count != 1 || navike["station:uzvodna"].UsualMin != 9*60 {
		t.Errorf("navike: %+v", navike)
	}
	if _, ima := navike["station:probno"]; ima {
		t.Error("očitanje imenjaka s drugim računom ušlo je u navike")
	}
}

// Uobičajeno vrijeme je kružna sredina: letva očitavana oko ponoći dobiva
// ponoć, a ne podne kao obična sredina minuta
func TestNavikeVrijemeOkoPonoci(t *testing.T) {
	repo, upisi := noveNavike(t)
	pero := uuid.NewString()
	d := time.Now().In(models.Zagreb).AddDate(0, 0, -3)
	u := func(dan, sat, minuta int) time.Time {
		return time.Date(d.Year(), d.Month(), d.Day()+dan, sat, minuta, 0, 0, models.Zagreb)
	}
	upisi("ponoc", pero, "", u(0, 23, 50))
	upisi("ponoc", pero, "", u(1, 0, 10))
	upisi("kasno", pero, "", u(0, 23, 40))
	upisi("kasno", pero, "", u(1, 0, 0))
	upisi("jutro", pero, "", u(0, 6, 0))
	upisi("jutro", pero, "", u(1, 8, 0))
	navike, err := repo.HabitsFor(context.Background(), pero, "", time.Now().AddDate(0, 0, -90))
	if err != nil {
		t.Fatal(err)
	}
	for letva, minuta := range map[string]int{"ponoc": 0, "kasno": 23*60 + 50, "jutro": 7 * 60} {
		if h := navike["station:"+letva]; h.Count != 2 || h.UsualMin != minuta {
			t.Errorf("%s: %+v, očekivana minuta %d", letva, h, minuta)
		}
	}
	if kruznaSredina([]int{1439}) != 1439 || kruznaSredina([]int{0}) != 0 {
		t.Error("jedno vrijeme je samo sebi sredina")
	}
}
