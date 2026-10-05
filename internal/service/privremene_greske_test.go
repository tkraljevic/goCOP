package service

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// zatvorenaBaza je baza sa shemom koja je zatvorena: svaki upit vraća grešku
func zatvorenaBaza(t *testing.T) (*sql.DB, *ledger.Recorder) {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "zatvorena.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test")
	baza.Close()
	return baza, rec
}

// Kad se baza ne da čitati: istek iz dužnosti uprave ostaje kakav je bio,
// usklađivanje vraća grešku (opoziv je samo bilježi, ovjera je javlja kao
// upozorenje), a kraja obrane nema, pa imenovanje vrijedi dalje
func TestPrivremeneGreskeBaze(t *testing.T) {
	baza, rec := zatvorenaBaza(t)
	us := NewUserService(repository.NewUserRepository(baza, rec), nil, NewSSEBroker())
	dosad := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	if got := us.istekIzvora(uuid.New(), &dosad, time.Now()); got == nil || !got.Equal(dosad) {
		t.Errorf("istek dužnosti uprave kad se baza ne da čitati: %v", got)
	}
	if err := us.UskladiPrivremene(); err == nil {
		t.Errorf("usklađivanje bez baze bez greške")
	}
	us.uskladiPoOpozivu(uuid.New())

	akti := &AktService{repo: repository.NewAktiRepository(baza, rec), sections: repository.NewSectionRepository(baza, rec), users: us}
	if poruke := akti.uskladiPrivremene(); len(poruke) != 1 || !strings.Contains(poruke[0], "privremena imenovanja nisu usklađena") {
		t.Errorf("upozorenje uz ovjeru: %v", poruke)
	}
	if poruke := (&AktService{}).uskladiPrivremene(); poruke != nil {
		t.Errorf("bez servisa korisnika: %v", poruke)
	}
	podrucje := 1
	for _, d := range []models.Duty{{AreaID: &podrucje, IsticeSObranom: true}, {SectionCodes: "P.1.1", IsticeSObranom: true}} {
		if k := akti.PrestanakObraneDuznosti(d); k != nil {
			t.Errorf("kraj obrane bez baze (%+v): %v", d, k)
		}
	}
}
