package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Zaključava da dvije objave u isto vrijeme ne dobiju isti broj prijave.
// Broj se čitao prije izrade PDF-a i upisivao bez provjere, pa je objava
// koja je za to vrijeme sastavljala PDF dobila broj koji je druga već
// upisala. Ovdje se druga prijava objavi do kraja dok se PDF prve sastavlja;
// prva tada kreće iznova i PDF se sastavi s novim brojem. Vodočuvar Pero
// Perić je izmišljen.
func TestIstodobneObjaveDobivajuRazliciteBrojeve(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	pero, users := o.vdVodocuvarUBazi(t)
	o.vs.sad = nil
	repo := repository.NewPrijavaRepository(o.baza, ledger.New(o.baza, "test"))
	prijave := NewPrijavaService(repo, users, o.vs, "cvor-probni")
	nacrt := func(naslov string) *models.PrijavaSTerena {
		t.Helper()
		p, err := prijave.Spremi(ctx, pero, "", UnosPrijave{Vrsta: models.PrijavaPrijava, Naslov: naslov,
			Opis: "Zatečeno pri obilasku.", Datum: time.Now().In(models.Zagreb)})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	prva, druga := nacrt("Procjeđivanje kod crpne stanice"), nacrt("Odron pokosa nasipa")
	pdf := func(*models.PrijavaSTerena) ([]byte, error) {
		return []byte("%PDF-1.4\n% probna prijava\n%%EOF\n"), nil
	}

	var brojeviUPDF []int
	if _, _, err := prijave.Objavi(ctx, pero, prva.ID, func(p *models.PrijavaSTerena) ([]byte, error) {
		brojeviUPDF = append(brojeviUPDF, p.Broj)
		if len(brojeviUPDF) == 1 {
			if _, _, err := prijave.Objavi(ctx, pero, druga.ID, pdf); err != nil {
				return nil, err
			}
		}
		return pdf(p)
	}); err != nil {
		t.Fatal(err)
	}

	p1, err := repo.Get(ctx, prva.ID)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := repo.Get(ctx, druga.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p1.Broj == p2.Broj || p1.Broj == 0 || p2.Broj == 0 {
		t.Fatalf("brojevi prijava: prva %d, druga %d; trebaju biti različiti", p1.Broj, p2.Broj)
	}
	if len(brojeviUPDF) != 2 || brojeviUPDF[1] != p1.Broj {
		t.Errorf("PDF prve prijave sastavljen s brojevima %v, a prijava nosi broj %d", brojeviUPDF, p1.Broj)
	}

	// već objavljena prijava ne objavljuje se ponovno, a greška nije zauzet broj
	if err := repo.Objavi(ctx, p1, []byte("%PDF-1.4\n%%EOF\n"), "x"); err == nil || errors.Is(err, repository.ErrBrojPrijaveZauzet) {
		t.Errorf("ponovna objava: %v", err)
	}
	// PDF koji se ne da sastaviti: prijava ostaje nacrt
	treca := nacrt("Oštećena rampa")
	if _, _, err := prijave.Objavi(ctx, pero, treca.ID, func(*models.PrijavaSTerena) ([]byte, error) {
		return nil, errors.New("pisač PDF-a nije dostupan")
	}); err == nil {
		t.Error("objava bez PDF-a javila je uspjeh")
	}
	if p3, err := repo.Get(ctx, treca.ID); err != nil || p3.Objavljena() {
		t.Errorf("prijava bez PDF-a: %+v, %v", p3, err)
	}
}
