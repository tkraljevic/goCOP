package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// brojOcitanjaIVerzija broji očitanja na površini i njihove verzije u knjizi
func (o *okolinaOcitanja) brojOcitanjaIVerzija(t *testing.T) (int, int) {
	t.Helper()
	var n, v int
	nuzno(t, o.baza.QueryRow(`SELECT COUNT(*) FROM readings`).Scan(&n))
	nuzno(t, o.baza.QueryRow(`SELECT COUNT(*) FROM record_versions WHERE entity = ?`, EntityReadings).Scan(&v))
	return n, v
}

// Novo očitanje koje ne uspije upisati ni u jednom koraku ne ostavlja ništa:
// ni očitanje na površini ni verziju u knjizi.
func TestNovoOcitanjeNeuspjeloNistaNeOstavlja(t *testing.T) {
	for _, slucaj := range []struct {
		naziv, q string
		otkazan  bool
		poruka   string
	}{
		{naziv: "otkazan zahtjev", otkazan: true},
		{naziv: "upis zabranjen", q: `CREATE TRIGGER zabrana BEFORE INSERT ON readings
			BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`, poruka: "greška pri upisu očitanja"},
		{naziv: "kanal se ne da upisati", q: `CREATE TRIGGER zabrana BEFORE UPDATE OF channel ON readings
			BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`},
		{naziv: "upisano se ne da pročitati", q: `CREATE TRIGGER pokvari AFTER INSERT ON readings
			BEGIN UPDATE readings SET measured_at = 'nije vrijeme' WHERE id = NEW.id; END`},
		{naziv: "knjiga ne prima verziju", q: zabranaKnjige},
	} {
		t.Run(slucaj.naziv, func(t *testing.T) {
			o := novaOkolinaOcitanja(t)
			if slucaj.q != "" {
				if _, err := o.baza.Exec(slucaj.q); err != nil {
					t.Fatal(err)
				}
			}
			ctx := context.Background()
			if slucaj.otkazan {
				ctx = otkazan()
			}
			rd := models.Reading{StationID: letvaPrimjerovo, MeasuredAt: u2026(time.March, 10, 7, 0), LevelCm: cmVodostaja(152),
				Source: models.ReadingSourceManual, Origin: models.ReadingOriginGoCOP}
			err := o.repo.Create(ctx, &rd)
			if err == nil || !strings.Contains(err.Error(), slucaj.poruka) {
				t.Fatalf("upis očitanja: %v", err)
			}
			if rd.ID == uuid.Nil {
				t.Error("očitanje bez identifikatora nije ga dobilo prije upisa")
			}
			if n, v := o.brojOcitanjaIVerzija(t); n != 0 || v != 0 {
				t.Errorf("nakon neuspjelog upisa: %d očitanja, %d verzija", n, v)
			}
		})
	}
}

// Uvoz koji ne uspije poništava cijelu seriju i javlja koje očitanje nije
// prošlo; uvoz bez tablice očitanja ne počinje.
func TestUvozNeuspioPonistavaSeriju(t *testing.T) {
	serija := func() []models.Reading {
		return []models.Reading{
			{ID: uuid.New(), StationID: letvaPrimjerovo, MeasuredAt: u2026(time.March, 10, 7, 0), LevelCm: cmVodostaja(152),
				Source: models.ReadingSourceImport, Origin: models.ReadingOriginArchive, SourceRef: "red-1"},
			{ID: uuid.New(), StationID: letvaBezimena, MeasuredAt: u2026(time.March, 10, 7, 0), LevelCm: cmVodostaja(40),
				Source: models.ReadingSourceImport, Origin: models.ReadingOriginArchive, SourceRef: "red-2"},
		}
	}
	for _, slucaj := range []struct {
		naziv, q string
		otkazan  bool
		poruka   string
	}{
		{naziv: "otkazan zahtjev", otkazan: true},
		{naziv: "nema tablice očitanja", q: `ALTER TABLE readings RENAME TO readings_negdje`},
		{naziv: "drugo očitanje se ne da upisati", q: `CREATE TRIGGER zabrana BEFORE INSERT ON readings WHEN NEW.source_ref = 'red-2'
			BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`, poruka: "greška pri uvozu očitanja red-2"},
	} {
		t.Run(slucaj.naziv, func(t *testing.T) {
			o := novaOkolinaOcitanja(t)
			if slucaj.q != "" {
				if _, err := o.baza.Exec(slucaj.q); err != nil {
					t.Fatal(err)
				}
			}
			ctx := context.Background()
			if slucaj.otkazan {
				ctx = otkazan()
			}
			n, err := o.repo.ImportBatch(ctx, serija())
			if err == nil || n != 0 || !strings.Contains(err.Error(), slucaj.poruka) {
				t.Fatalf("uvoz: %d %v", n, err)
			}
			if strings.Contains(slucaj.q, "RENAME") {
				return
			}
			if n, v := o.brojOcitanjaIVerzija(t); n != 0 || v != 0 {
				t.Errorf("nakon neuspjelog uvoza: %d očitanja, %d verzija", n, v)
			}
		})
	}
}

// Navike očitavanja i postojeći identifikatori ne prolaze tiho kad se
// zapis ne da pročitati ili je baza zatvorena
func TestNavikeIIdentifikatoriGreskeBaze(t *testing.T) {
	o := novaOkolinaOcitanja(t)
	ctx := context.Background()
	rd := models.Reading{StationID: letvaPrimjerovo, MeasuredAt: u2026(time.March, 10, 7, 0), LevelCm: cmVodostaja(152),
		Source: models.ReadingSourceManual, Origin: models.ReadingOriginArchive, UserID: "korisnik-1"}
	nuzno(t, o.repo.Create(ctx, &rd))
	od := u2026(time.January, 1, 0, 0)
	if navike, err := o.repo.HabitsFor(ctx, "korisnik-1", "", od); err != nil || navike["station:"+letvaPrimjerovo].Count != 1 {
		t.Fatalf("navike: %+v %v", navike, err)
	}

	if _, err := o.baza.Exec(`UPDATE readings SET id = NULL`); err != nil {
		t.Fatal(err)
	}
	if ids, err := o.repo.ExistingIDs(ctx, models.ReadingOriginArchive); err == nil {
		t.Errorf("identifikatori s očitanjem bez identifikatora bez greške: %v", ids)
	}
	if _, err := o.baza.Exec(`UPDATE readings SET measured_at = 'nije vrijeme'`); err != nil {
		t.Fatal(err)
	}
	if navike, err := o.repo.HabitsFor(ctx, "korisnik-1", "", time.Time{}); err == nil {
		t.Errorf("navike s nečitljivim očitanjem bez greške: %+v", navike)
	}
	o.baza.Close()
	if _, err := o.repo.HabitsFor(ctx, "korisnik-1", "", od); err == nil {
		t.Error("navike iz zatvorene baze bez greške")
	}
}
