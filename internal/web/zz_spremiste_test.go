package web

import (
	"log"
	"os"
	"testing"

	"gocop/internal/repository"
	"gocop/internal/sadrzaj"
)

// Ovjere i objave pišu PDF-ove u spremište sadržaja, pa ga i testovi paketa
// trebaju. Jedno memorijsko spremište za cijeli paket: sadržaj se vodi po
// otisku, pa se testovi međusobno ne gaze.
func TestMain(m *testing.M) {
	sp, err := sadrzaj.Otvori("")
	if err != nil {
		log.Fatal(err)
	}
	repository.SetSpremiste(sp)
	kod := m.Run()
	sp.Zatvori()
	os.Exit(kod)
}
