package service_test

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/sadrzaj"
	"gocop/internal/service"
)

// Ovjeren akt čiji je zapis izvornika stigao razmjenom, a PDF još nije: akt
// se ne šalje ni probno, ništa se ne upisuje u slanja, a crtež programa ne
// zamjenjuje izvornik. Kad PDF stigne pod istim otiskom, šalje se baš on;
// akt bez zapisa izvornika šalje se s crtežom, kao i prije.
func TestSlanjeAktaKojemuIzvornikNijeOvdje(t *testing.T) {
	ctx := context.Background()
	o := novaOkolinaSanducica(t)
	repo := repository.NewAktiRepository(o.db, o.rec)
	a := o.aktNaZnanje(t, models.AktOvjeren)
	izvornik := []byte("%PDF-1.7 izvornik akta\n")
	if err := repo.SaveIzvornik(ctx, &repository.Izvornik{AktID: a.ID, PDF: izvornik, Sazetak: "sazetak-ovjere"}); err != nil {
		t.Fatal(err)
	}
	otisak := sadrzaj.Otisak(izvornik)
	// novo, prazno spremište: zapis izvornika ostaje, a PDF-a nema
	sp, err := sadrzaj.Otvori("")
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
	nacrtan := service.PorukaAkta{Predmet: "Akt 7/2026", Tekst: "U privitku je akt.", ImeDatoteke: "akt-7-2026.pdf", PDF: []byte("%PDF-1.7 nacrtao program\n")}

	if _, err := o.akti.Izvornik(ctx, a.ID); !errors.Is(err, service.ErrIzvornikAktaNijeOvdje) {
		t.Errorf("izvornik koji nije ovdje: %v", err)
	}
	for _, x := range []struct {
		opis   string
		adrese []string
		kopija bool
	}{{"primateljima", []string{"opcina@probna.test"}, true}, {"probno samo meni", nil, true}} {
		if ishod, err := o.akti.PosaljiNaZnanje(ctx, o.anaOvl, o.ana, a.ID, x.adrese, nil, x.kopija, nacrtan); ishod != nil || !errors.Is(err, service.ErrSlanjeBezIzvornika) {
			t.Errorf("%s bez izvornika ovdje: %+v, %v", x.opis, ishod, err)
		}
	}
	if n := len(o.srv.Poruke()); n != 0 {
		t.Errorf("bez izvornika ovdje poslano je %d poruka", n)
	}
	if _, _, slanja := o.akti.AdresatiAkta(ctx, a); len(slanja) != 0 {
		t.Errorf("odbijeno slanje je upisano: %+v", slanja)
	}
	if iz, err := repo.GetIzvornik(ctx, a.ID); err != nil || iz == nil || iz.Otisak != otisak || iz.Sazetak != "sazetak-ovjere" {
		t.Errorf("izvornik je promijenjen: %+v, %v", iz, err)
	}

	// PDF stigne razmjenom pod istim otiskom: primatelj dobiva izvornik
	if err := sp.UpisiProvjereno(ctx, otisak, "application/pdf", izvornik, "razmjena"); err != nil {
		t.Fatal(err)
	}
	ishod, err := o.akti.PosaljiNaZnanje(ctx, o.anaOvl, o.ana, a.ID, []string{"opcina@probna.test"}, nil, false, nacrtan)
	if err != nil || ishod.Poslano != 1 {
		t.Fatalf("slanje s izvornikom ovdje: %+v, %v", ishod, err)
	}
	prim := o.srv.Poruke()
	if len(prim) != 1 || !strings.Contains(prim[0].Podaci, base64.StdEncoding.EncodeToString(izvornik)) ||
		strings.Contains(prim[0].Podaci, base64.StdEncoding.EncodeToString(nacrtan.PDF)) {
		t.Error("primatelj nije dobio izvornik akta")
	}

	// izvornik se ne da pročitati: akt se ne šalje
	pokvari := func(q string) {
		t.Helper()
		if _, err := o.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	pokvari(`ALTER TABLE akti_izvornici RENAME TO akti_izvornici_skriveni`)
	if _, err := o.akti.PosaljiNaZnanje(ctx, o.anaOvl, o.ana, a.ID, nil, nil, true, nacrtan); err == nil ||
		!strings.HasPrefix(err.Error(), "akt nije poslan: izvornik akta nije se dao pročitati") {
		t.Errorf("slanje kad se izvornik ne da pročitati: %v", err)
	}
	pokvari(`ALTER TABLE akti_izvornici_skriveni RENAME TO akti_izvornici`)
	if n := len(o.srv.Poruke()); n != 1 {
		t.Errorf("izvornik se nije dao pročitati, a poslano je još %d poruka", n-1)
	}

	// kontrola: akt bez zapisa izvornika šalje se s PDF-om koji izradi program
	bez := o.aktNaZnanje(t, models.AktOvjeren)
	if _, err := o.akti.PosaljiNaZnanje(ctx, o.anaOvl, o.ana, bez.ID, []string{"opcina@probna.test"}, nil, false, nacrtan); err != nil {
		t.Fatalf("akt bez zapisa izvornika: %v", err)
	}
	if prim := o.srv.Poruke(); len(prim) != 2 || !strings.Contains(prim[1].Podaci, base64.StdEncoding.EncodeToString(nacrtan.PDF)) {
		t.Error("akt bez zapisa izvornika nije poslan s PDF-om koji izradi program")
	}
}

// PDF izvornika akta je na ovom računalu, a ne da se pročitati: Izvornik
// javlja grešku, a ne da izvornik još nije stigao. Spremanje izvornika pri
// ovjeri tada ne dira zatečeni izvornik, a zamjena skena i dalje zna
// veličinu prethodnog skena, jer joj PDF ne treba.
func TestIzvornikAktaKojiSeNeDaProcitati(t *testing.T) {
	ctx := context.Background()
	put := filepath.Join(t.TempDir(), "sadrzaj.db")
	sp, err := sadrzaj.Otvori(put)
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
	pokvari := func(otisak string) {
		t.Helper()
		nadjeni, err := filepath.Glob(filepath.Join(sadrzaj.MapaUz(put), "pdf", "*", otisak+".pdf"))
		if err != nil || len(nadjeni) != 1 {
			t.Fatalf("datoteka PDF-a %s: %v, %v", otisak, nadjeni, err)
		}
		if err := os.Remove(nadjeni[0]); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(nadjeni[0], 0o755); err != nil {
			t.Fatal(err)
		}
	}
	o := novaOkolinaAkta(t)
	repo := repoAkata(o)
	prvi, drugi := []byte("%PDF-1.7 prvi sken"), []byte("%PDF-1.7 drugi sken")

	// zamjena skena kad se prethodni sken ne da pročitati
	nacrt := o.nacrtDezurnog(t, models.AktUspostava, time.Now().Add(-2*time.Hour).Truncate(time.Minute))
	a, _, err := o.akti.UcitajSkenirani(ctx, o.ovlasti, o.rukovod, nacrt.ID, prvi, o.rukovod.ID.String(), potvrdaPrimatelja)
	if err != nil {
		t.Fatal(err)
	}
	stari, err := repo.ZapisIzvornika(ctx, a.ID)
	if err != nil || stari == nil {
		t.Fatalf("izvornik skena: %+v, %v", stari, err)
	}
	pokvari(stari.Otisak)
	if a, err = o.akti.ZamijeniSken(ctx, o.ovlasti, o.rukovod, a.ID, drugi, "nečitko"); err != nil {
		t.Fatal(err)
	}
	if len(a.ZamjeneSkena) != 1 || a.ZamjeneSkena[0].StariBajtova != stari.Bajtova {
		t.Errorf("zamjena skena ne zna veličinu prethodnog: %+v, želi %d", a.ZamjeneSkena, stari.Bajtova)
	}

	// izvornik spremljen pri ovjeri, a ne da se pročitati
	pri, nacrtan := []byte("%PDF-1.7 izvornik pri ovjeri"), []byte("%PDF-1.7 nacrtao program")
	ovjeren := ovjerenAkt(t, o, o.rukovod.ID.String())
	if err := o.akti.SpremiIzvornikPDF(ctx, ovjeren, pri); err != nil {
		t.Fatal(err)
	}
	pokvari(sadrzaj.Otisak(pri))
	if _, err := o.akti.Izvornik(ctx, ovjeren.ID); err == nil || errors.Is(err, service.ErrIzvornikAktaNijeOvdje) {
		t.Errorf("izvornik koji se ne da pročitati: %v", err)
	}
	if err := o.akti.SpremiIzvornikPDF(ctx, ovjeren, nacrtan); err != nil {
		t.Errorf("spremanje uz izvornik koji se ne da pročitati: %v", err)
	}
	if iz, err := repo.ZapisIzvornika(ctx, ovjeren.ID); err != nil || iz == nil || iz.Otisak != sadrzaj.Otisak(pri) {
		t.Errorf("izvornik je promijenjen: %+v, %v", iz, err)
	}
}
