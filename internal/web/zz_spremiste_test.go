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
//
// Paket dobije i svoju privremenu mapu. Testovi šalju i datoteke veće od
// onoga što obrazac drži u memoriji, pa ih Go sprema na disk (multipart-*).
// Poslužitelj ih obriše nakon zahtjeva, a u testu poslužitelja nema, pa su
// ostajale u privremenoj mapi sustava: svako pokretanje testova ostavilo je
// po nekoliko desetaka megabajta. Mapa paketa se nakon testova obriše cijela.
func TestMain(m *testing.M) {
	privremeno, err := os.MkdirTemp("", "gocop-web-test-")
	if err != nil {
		log.Fatal(err)
	}
	for _, ime := range []string{"TMPDIR", "TMP", "TEMP"} {
		if err := os.Setenv(ime, privremeno); err != nil {
			log.Fatal(err)
		}
	}
	sp, err := sadrzaj.Otvori("")
	if err != nil {
		log.Fatal(err)
	}
	repository.SetSpremiste(sp)
	kod := m.Run()
	sp.Zatvori()
	if err := os.RemoveAll(privremeno); err != nil {
		log.Printf("privremena mapa testova nije obrisana: %v", err)
	}
	os.Exit(kod)
}
