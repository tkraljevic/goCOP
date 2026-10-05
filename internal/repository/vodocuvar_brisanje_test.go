package repository

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/sadrzaj"
)

// okolinaBrisanja je baza s nacrtom Pere Perića (sektor P, područje 1) i
// njegovim izvornikom, za brisanje lista
type okolinaBrisanja struct {
	baza  *sql.DB
	repo  *VodocuvarRepository
	nacrt *models.VodocuvarskiList
}

func novaOkolinaBrisanja(t *testing.T, predan bool) *okolinaBrisanja {
	t.Helper()
	dir := t.TempDir()
	baza, err := db.OpenDB(filepath.Join(dir, "brisanje.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', '')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	spremiste, err := sadrzaj.Otvori(filepath.Join(dir, "sadrzaj.db"))
	if err != nil {
		t.Fatal(err)
	}
	prijasnje := Spremiste()
	SetSpremiste(spremiste)
	t.Cleanup(func() {
		SetSpremiste(prijasnje)
		_ = spremiste.Zatvori()
	})
	ctx := context.Background()
	o := &okolinaBrisanja{baza: baza, repo: NewVodocuvarRepository(baza, ledger.New(baza, "cvor-probni"))}
	o.nacrt = &models.VodocuvarskiList{UserID: uuid.NewString(), Ime: "Pero Perić", Sektor: "P", AreaID: 1,
		Datum: time.Date(2026, time.March, 10, 0, 0, 0, 0, models.Zagreb), Broj: 1, Od: "07:00", Do: "15:00", Opis: "obilazak nasipa"}
	if predan {
		kad := time.Now()
		o.nacrt.PredanoAt = &kad
	}
	if err := o.repo.Save(ctx, o.nacrt); err != nil {
		t.Fatal(err)
	}
	if err := o.repo.SaveIzvornik(ctx, &models.IzvornikLista{ListID: o.nacrt.ID, PDF: []byte("%PDF-1.4 list"), Sazetak: "probni"}); err != nil {
		t.Fatal(err)
	}
	return o
}

// stanje kaže ima li u bazi lista i izvornika te koliko je arhiviranih
// verzija u knjizi, s kanalom arhiviranog izvornika
func (o *okolinaBrisanja) stanje(t *testing.T) (list, izvornik bool, arhivirano int, kanal string) {
	t.Helper()
	ctx := context.Background()
	l, err := o.repo.Get(ctx, o.nacrt.ID)
	if err != nil {
		t.Fatal(err)
	}
	iz, err := o.repo.GetIzvornik(ctx, o.nacrt.ID)
	if err != nil && !strings.Contains(err.Error(), "no such table") {
		t.Fatal(err)
	}
	if err := o.baza.QueryRow(`SELECT COUNT(*), COALESCE(MAX(CASE WHEN entity = ? THEN channel END), '')
		FROM record_versions WHERE entity_id = ? AND archived = 1`, EntityVodocuvarskiIzvornici, o.nacrt.ID).Scan(&arhivirano, &kanal); err != nil {
		t.Fatal(err)
	}
	return l != nil, iz != nil, arhivirano, kanal
}

// Nacrt se briše s izvornikom u jednoj transakciji; obje verzije u knjizi
// arhivirane su, izvornik u kanalu lista. Nepostojeći list nema što
// obrisati.
func TestBrisanjeNacrtaSIzvornikom(t *testing.T) {
	o := novaOkolinaBrisanja(t, false)
	if err := o.repo.Delete(context.Background(), o.nacrt.ID); err != nil {
		t.Fatal(err)
	}
	list, izvornik, arh, kanal := o.stanje(t)
	if list || izvornik || arh != 2 {
		t.Errorf("nakon brisanja: list %v, izvornik %v, arhiviranih %d", list, izvornik, arh)
	}
	if want := listChannel(o.nacrt); kanal != want {
		t.Errorf("kanal arhiviranog izvornika: %q, očekivan %q", kanal, want)
	}
	if err := o.repo.Delete(context.Background(), uuid.NewString()); err != nil {
		t.Errorf("brisanje nepostojećeg lista: %v", err)
	}
}

// Predan list (druga kartica ga je predala nakon što je nacrt pročitan)
// repozitorij ne briše: ni list ni izvornik, bez verzije u knjizi.
func TestBrisanjeNeBrisePredanList(t *testing.T) {
	o := novaOkolinaBrisanja(t, true)
	err := o.repo.Delete(context.Background(), o.nacrt.ID)
	if !errors.Is(err, ErrListPredanNijeObrisan) || !strings.Contains(err.Error(), "list od 10.03.2026. je u međuvremenu predan i nije obrisan") {
		t.Fatalf("brisanje predanog lista: %v", err)
	}
	if list, izvornik, arh, _ := o.stanje(t); !list || !izvornik || arh != 0 {
		t.Errorf("nakon odbijenog brisanja: list %v, izvornik %v, arhiviranih %d", list, izvornik, arh)
	}
}

// Kad brisanje lista ili izvornika ne uspije, ne briše se ništa i u knjizi
// nema arhivirane verzije
func TestBrisanjeNacrtaNeuspjeloNistaNeBrise(t *testing.T) {
	for _, slucaj := range []struct {
		naziv, q string
		izvornik bool
	}{
		{"brisanje lista", `CREATE TRIGGER zabrana BEFORE DELETE ON vodocuvarski_listovi BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`, true},
		{"brisanje izvornika", `CREATE TRIGGER zabrana BEFORE DELETE ON vodocuvarski_izvornici BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`, true},
		{"čitanje izvornika", `DROP TABLE vodocuvarski_izvornici`, false},
	} {
		t.Run(slucaj.naziv, func(t *testing.T) {
			o := novaOkolinaBrisanja(t, false)
			if _, err := o.baza.Exec(slucaj.q); err != nil {
				t.Fatal(err)
			}
			if err := o.repo.Delete(context.Background(), o.nacrt.ID); err == nil {
				t.Fatal("brisanje je prošlo")
			}
			if list, izvornik, arh, _ := o.stanje(t); !list || izvornik != slucaj.izvornik || arh != 0 {
				t.Errorf("nakon neuspjelog brisanja: list %v, izvornik %v, arhiviranih %d", list, izvornik, arh)
			}
		})
	}
}

// Nacrt koji je druga kartica u međuvremenu obrisala nema što obrisati;
// greška čitanja prenosi se kakva jest
func TestNacrtNijeObrisan(t *testing.T) {
	o := novaOkolinaBrisanja(t, false)
	ctx := context.Background()
	tx, err := o.baza.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	obrisan := *o.nacrt
	obrisan.ID = uuid.NewString()
	if err := nacrtNijeObrisan(ctx, tx, &obrisan, nil); err != nil {
		t.Errorf("nacrt obrisan u međuvremenu: %v", err)
	}
	greska := errors.New("broj obrisanih nije poznat")
	if err := nacrtNijeObrisan(ctx, tx, o.nacrt, greska); !errors.Is(err, greska) {
		t.Errorf("greška brisanja: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := nacrtNijeObrisan(ctx, tx, o.nacrt, nil); !errors.Is(err, sql.ErrTxDone) {
		t.Errorf("čitanje u završenoj transakciji: %v", err)
	}
	if err := o.repo.obrisiIzvornik(ctx, tx, o.nacrt, nil); err != nil {
		t.Errorf("list bez izvornika: %v", err)
	}
	if err := o.repo.obrisiIzvornik(ctx, tx, o.nacrt, &models.IzvornikLista{ListID: o.nacrt.ID}); !errors.Is(err, sql.ErrTxDone) {
		t.Errorf("brisanje izvornika u završenoj transakciji: %v", err)
	}
}
