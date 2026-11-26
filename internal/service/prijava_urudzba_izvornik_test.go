package service

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/sadrzaj"
)

// Urudžba se dopisuje na izvornik prijave. Kad zapis izvornika postoji, a
// PDF nije na ovom računalu, urudžba i rješavanje s urudžbom odbijaju se prije
// upisa; kad PDF stigne pod istim otiskom, urudžba se dopiše na njegove
// bajtove. Prijava bez izvornika upisuje urudžbu kao i prije. Podaci su
// izmišljeni.
func TestUrudzbaCekaIzvornikPrijave(t *testing.T) {
	k := novaKnjigaProba(t)
	ctx := context.Background()
	sp, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "sadrzaj.db"))
	if err != nil {
		t.Fatal(err)
	}
	prijasnje := repository.Spremiste()
	repository.SetSpremiste(sp)
	t.Cleanup(func() {
		repository.SetSpremiste(prijasnje)
		if err := sp.Zatvori(); err != nil {
			t.Error(err)
		}
	})
	repo := repository.NewPrijavaRepository(k.o.baza, ledger.New(k.o.baza, "test"))
	prijave := NewPrijavaService(repo, k.o.vs.users, k.o.vs, "test")
	ivo := &models.User{ID: uuid.New(), Username: "iivic", FullName: "Ivo Ivić", IsActive: true, Duties: []models.Duty{
		{Role: models.RoleSectorLeader, ScopeType: models.ScopeSector, SectorID: &k.sektor, IsActive: true, IsPrimary: true},
	}}
	perms := models.NewUserPermissions(*ivo)
	objaviKao := func(prenesena bool) *models.PrijavaSTerena {
		t.Helper()
		kad := time.Now()
		p := &models.PrijavaSTerena{UserID: k.pero.ID.String(), Ime: k.pero.FullName, Sektor: k.sektor, AreaID: k.p1, Vrsta: models.PrijavaPrijava,
			Naslov: "Procjeđivanje", Opis: "opis", Datum: kad, Status: models.PrijavaObjavljena, ObjavljenoAt: &kad, Rekonstrukcija: prenesena}
		if err := repo.Save(ctx, p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	objavi := func() *models.PrijavaSTerena { return objaviKao(false) }
	zapisIzvornika := func(p *models.PrijavaSTerena, otisak string, bajtova int) {
		t.Helper()
		if _, err := k.o.baza.Exec(`INSERT INTO prijave_izvornici (prijava_id, otisak, bajtova, vrsta, sazetak, updated_at) VALUES (?, ?, ?, 'application/pdf', 'sazetak-objave', ?)`,
			p.ID, otisak, bajtova, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	izvornik := []byte("%PDF-1.7 izvornik prijave Procjeđivanje")
	otisak := sadrzaj.Otisak(izvornik)
	var dobio []byte
	izradi := func(_ *models.PrijavaSTerena, iz []byte) ([]byte, error) {
		dobio = iz
		return append(append([]byte{}, iz...), []byte(" bilješka urudžbe")...), nil
	}
	ur := Urudzba{Klasa: "325-02/26-01/0000001", Urbroj: "374-26-1"}

	p := objavi()
	zapisIzvornika(p, otisak, len(izvornik))
	// pravilo stoji u servisu: odbija i pozivatelja koji bilješku ne izrađuje
	for _, iz := range []func(*models.PrijavaSTerena, []byte) ([]byte, error){izradi, nil} {
		if _, err := prijave.Urudzbiraj(ctx, perms, ivo, p.ID, ur, iz); !errors.Is(err, ErrUrudzbaCekaIzvornik) || err.Error() != ErrUrudzbaCekaIzvornik.Error() {
			t.Errorf("urudžba kad izvornik nije ovdje (izradi %v): %v", iz != nil, err)
		}
		if _, err := prijave.RijesiSUrudzbom(ctx, perms, ivo, p.ID, ur, iz); !errors.Is(err, ErrRjesenjeCekaIzvornik) || err.Error() != ErrRjesenjeCekaIzvornik.Error() {
			t.Errorf("rješavanje s urudžbom kad izvornik nije ovdje (izradi %v): %v", iz != nil, err)
		}
	}
	if dobio != nil {
		t.Error("bilješka se izrađivala bez izvornika")
	}
	if nakon, err := repo.Get(ctx, p.ID); err != nil || nakon == nil || nakon.Klasa != "" || nakon.Rijesena() || nakon.PrimljenoAt != nil {
		t.Errorf("prijava je promijenjena iako izvornik nije ovdje: %+v %v", nakon, err)
	}
	if iz, err := repo.Izvornik(ctx, p.ID); err != nil || iz == nil || iz.Otisak != otisak || iz.Sazetak != "sazetak-objave" {
		t.Errorf("izvornik je promijenjen: %+v %v", iz, err)
	}
	// bez klase i urbroja prijava se ovdje rješava i dalje
	if r, err := prijave.RijesiSUrudzbom(ctx, perms, ivo, p.ID, Urudzba{}, izradi); err != nil || !r.Rijesena() || r.Klasa != "" {
		t.Errorf("rješavanje bez urudžbe kad izvornik nije ovdje: %+v %v", r, err)
	}

	// prenesena prijava nije objavljena ni na jednom računalu: poruka kaže
	// gdje je njezin PDF, a greška je ista
	prenesena := objaviKao(true)
	pdfPrenesene := []byte("%PDF prenesene prijave")
	zapisIzvornika(prenesena, sadrzaj.Otisak(pdfPrenesene), len(pdfPrenesene))
	for _, c := range []struct {
		ime    string
		radnja func() error
		ista   error
	}{
		{"urudžba", func() error {
			_, err := prijave.Urudzbiraj(ctx, perms, ivo, prenesena.ID, ur, izradi)
			return err
		}, ErrUrudzbaCekaIzvornik},
		{"rješavanje s urudžbom", func() error {
			_, err := prijave.RijesiSUrudzbom(ctx, perms, ivo, prenesena.ID, ur, nil)
			return err
		}, ErrRjesenjeCekaIzvornik},
	} {
		err := c.radnja()
		if !errors.Is(err, c.ista) || !strings.Contains(err.Error(), "na kojem je prijava prenesena") || strings.Contains(err.Error(), "objavljen") {
			t.Errorf("%s prenesene prijave kojoj izvornik nije ovdje: %v", c.ime, err)
		}
	}
	if nakon, err := repo.Get(ctx, prenesena.ID); err != nil || nakon == nil || nakon.Klasa != "" || nakon.Rijesena() {
		t.Errorf("prenesena prijava je promijenjena iako izvornik nije ovdje: %+v %v", nakon, err)
	}
	// kad PDF prenesene prijave stigne, urudžba se upisuje i bez bilješke
	if err := sp.UpisiProvjereno(ctx, sadrzaj.Otisak(pdfPrenesene), "application/pdf", pdfPrenesene, "razmjena"); err != nil {
		t.Fatal(err)
	}
	if r, err := prijave.Urudzbiraj(ctx, perms, ivo, prenesena.ID, ur, nil); err != nil || r.Klasa != ur.Klasa {
		t.Errorf("urudžba prenesene prijave s PDF-om ovdje: %+v %v", r, err)
	}
	if iz, err := repo.Izvornik(ctx, prenesena.ID); err != nil || iz == nil || !bytes.Equal(iz.PDF, pdfPrenesene) {
		t.Errorf("urudžba bez izrade bilješke promijenila je PDF prenesene prijave: %v", err)
	}

	// PDF objave stigne razmjenom pod istim otiskom: urudžba se dopisuje na njega
	if err := sp.UpisiProvjereno(ctx, otisak, "application/pdf", izvornik, "razmjena"); err != nil {
		t.Fatal(err)
	}
	if r, err := prijave.Urudzbiraj(ctx, perms, ivo, p.ID, ur, izradi); err != nil || r.Klasa != ur.Klasa {
		t.Fatalf("urudžba s izvornikom ovdje: %+v %v", r, err)
	}
	if !bytes.Equal(dobio, izvornik) {
		t.Error("bilješka nije izrađena na bajtovima izvornika objave")
	}
	if iz, err := repo.Izvornik(ctx, p.ID); err != nil || iz == nil || !bytes.HasPrefix(iz.PDF, izvornik) || iz.Otisak == otisak {
		t.Error("izvornik nije zamijenjen izvornikom s bilješkom")
	}

	// kontrola: prijava bez zapisa izvornika upisuje urudžbu bez bilješke
	dobio = nil
	bez := objavi()
	if r, err := prijave.RijesiSUrudzbom(ctx, perms, ivo, bez.ID, ur, izradi); err != nil || !r.Rijesena() || r.Klasa != ur.Klasa || dobio != nil {
		t.Errorf("prijava bez izvornika: %+v %v", r, err)
	}
	if r, err := prijave.Urudzbiraj(ctx, perms, ivo, objavi().ID, ur, nil); err != nil || r.Klasa != ur.Klasa {
		t.Errorf("prijava bez izvornika, bez izrade bilješke: %+v %v", r, err)
	}

	// izvornik se ne da pročitati: greška se ne prešućuje, urudžbe nema
	druga := objavi()
	if _, err := k.o.baza.Exec(`DROP TABLE prijave_izvornici`); err != nil {
		t.Fatal(err)
	}
	for _, iz := range []func(*models.PrijavaSTerena, []byte) ([]byte, error){izradi, nil} {
		if _, err := prijave.Urudzbiraj(ctx, perms, ivo, druga.ID, ur, iz); err == nil || !strings.HasPrefix(err.Error(), "urudžba nije upisana: izvornik prijave nije se dao pročitati") {
			t.Errorf("urudžba kad se izvornik ne da pročitati (izradi %v): %v", iz != nil, err)
		}
	}
	if nakon, err := repo.Get(ctx, druga.ID); err != nil || nakon == nil || nakon.Klasa != "" {
		t.Error("urudžba je upisana iako se izvornik nije dao pročitati")
	}
}
