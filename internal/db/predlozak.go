package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Predložak baze služi testovima drugih paketa. Gotovo svaki test radi na
// svojoj novoj bazi, a InitSchema na novoj bazi uz race detector traje oko
// 1,4 s, jer migrateSchema doda tridesetak stupaca i svaki put iznova čita
// cijelu shemu. Zato se shema složi jednom po testnom procesu, istim
// InitSchema, a svaki test dobije kopiju te datoteke. Program predložak ne
// koristi: čvor pri pokretanju uvijek zove InitSchema.

// predlozak je sadržaj baze sa složenom shemom, jednom po procesu
var predlozak = sync.OnceValues(func() ([]byte, error) {
	mapa, err := os.MkdirTemp("", "gocop-predlozak-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(mapa) }() //nolint:errcheck // privremena mapa; sadržaj predloška je već pročitan
	return slozPredlozak(filepath.Join(mapa, "predlozak.db"))
})

// slozPredlozak složi shemu (InitSchema) u bazi na putu put i vrati sadržaj
// cijele baze u jednoj datoteci
func slozPredlozak(put string) ([]byte, error) {
	baza, err := OpenDB(put)
	if err != nil {
		return nil, err
	}
	defer func() { _ = baza.Close() }() //nolint:errcheck // baza je samo izvor kopije, a kopija je već zapisana
	if err := InitSchema(baza); err != nil {
		return nil, err
	}
	// VACUUM INTO zapiše cijelu bazu u novu datoteku, bez dnevnika (-wal)
	kopija := put + ".kopija"
	if _, err := baza.Exec(`VACUUM INTO ?`, kopija); err != nil {
		return nil, err
	}
	return os.ReadFile(kopija)
}

// UpisiPredlozak upiše na put novu bazu sa shemom kakvu složi InitSchema.
// Postojeću datoteku ne dira i javlja grešku, da test ne prepiše bazu koju
// ponovno otvara.
func UpisiPredlozak(put string) error {
	return upisiNovu(put, predlozak)
}

// BazaIzPredloska je UpisiPredlozak pa OpenDB: nova baza sa shemom, kao
// OpenDB i InitSchema na novom putu
func BazaIzPredloska(put string) (*sql.DB, error) {
	if err := UpisiPredlozak(put); err != nil {
		return nil, err
	}
	return OpenDB(put)
}

// upisiNovu upiše sadržaj iz izvora u novu datoteku na putu put. Baza u
// memoriji (":memory:") i adresa s postavkama ("file:…") nisu put datoteke:
// predložak bi tada nastao kao datoteka tog imena u radnoj mapi.
func upisiNovu(put string, izvor func() ([]byte, error)) error {
	if put == ":memory:" || strings.HasPrefix(put, "file:") {
		return fmt.Errorf("predložak se upisuje samo u datoteku, a ne u %s", put)
	}
	sadrzaj, err := izvor()
	if err != nil {
		return err
	}
	if _, err := os.Lstat(put); err == nil {
		return fmt.Errorf("baza %s već postoji; predložak se upisuje samo u novu datoteku", put)
	}
	if err := os.MkdirAll(filepath.Dir(put), 0o755); err != nil {
		return err
	}
	return os.WriteFile(put, sadrzaj, 0o644)
}
