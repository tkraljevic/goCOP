package service

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Odluke (8. 10. 2026.), pitanje 10: opis rada koji je ispravila uprava
// centra (ispravak opisa) osoba čije je dežurstvo poslije ne mijenja i
// zapis ne miče. Inače bi napomena i dalje tvrdila da je opis izabrala
// uprava, a zapis bi se dao maknuti i upisati ponovno s opisom po vlastitom
// izboru i bez traga. Pero Perić ima zapis s drugog čvora bez mjesta rada,
// Ana Anić iz uprave centra bira opis, Ivo Ivić iz uprave ga mijenja još
// jednom.
func TestIspravljenOpisDezurstvaBiraUprava(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	ana := &models.User{ID: uuid.New(), FullName: "Ana Anić"}
	ivo := &models.User{ID: uuid.New(), FullName: "Ivo Ivić"}
	jutro := o.peroDezura(dzSat(9, 8, 0), dzSat(9, 12, 0))
	if err := o.svc.SpremiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik, jutro); err != nil {
		t.Fatal(err)
	}
	o.bezMjesta(t, jutro)
	if _, err := o.svc.IspraviOpisDezurstva(ctx, ana, o.ovlUprav, o.dnevnik, jutro.ID, "Administrativni rad pri provedbi obrane od poplava"); err != nil {
		t.Fatal(err)
	}

	// Pero ne mijenja opis koji je izabrala uprava
	izmjena := o.peroDezura(dzSat(9, 8, 0), dzSat(9, 12, 0))
	izmjena.ID, izmjena.Opis = jutro.ID, "Ostali terenski poslovi pri obrani od poplava"
	if err := o.svc.SpremiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik, izmjena); err == nil || !strings.Contains(err.Error(), "voditelj ili zamjenik centra") {
		t.Errorf("Pero mijenja opis koji je ispravila uprava: %v", err)
	}
	if d, _ := o.svc.repo.GetDezurstvo(ctx, jutro.ID); d.Opis != "Administrativni rad pri provedbi obrane od poplava" || d.Mjesto != models.MjestoUred {
		t.Errorf("opis promijenjen: %+v", d)
	}
	// ni ne miče zapis, pa da ga upiše ponovno
	if err := o.svc.MakniDezurstvo(ctx, o.pperic, o.ovlPero, o.dnevnik, jutro.ID); err == nil || !strings.Contains(err.Error(), "voditelj ili zamjenik centra") {
		t.Errorf("Pero miče zapis kojemu je opis ispravila uprava: %v", err)
	}
	if d, _ := o.svc.repo.GetDezurstvo(ctx, jutro.ID); d == nil {
		t.Fatal("zapis maknut")
	}
	// ostalo u svom zapisu mijenja, uz isti opis; trag ostaje
	vrijeme := o.peroDezura(dzSat(9, 8, 0), dzSat(9, 13, 0))
	vrijeme.ID, vrijeme.Opis = jutro.ID, "Administrativni rad pri provedbi obrane od poplava"
	if err := o.svc.SpremiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik, vrijeme); err != nil {
		t.Errorf("Pero mijenja vrijeme uz isti opis: %v", err)
	}
	d, _ := o.svc.repo.GetDezurstvo(ctx, jutro.ID)
	if !d.Do.Equal(dzSat(9, 13, 0)) || strings.Count(d.Napomena, "[opis ispravio Ana Anić ") != 1 {
		t.Errorf("izmjena vremena: %+v", d)
	}

	// uprava opis mijenja i dalje; trag tada kaže tko je izabrao sadašnji opis
	ivin := o.peroDezura(dzSat(9, 8, 0), dzSat(9, 13, 0))
	ivin.ID, ivin.Opis, ivin.Napomena = jutro.ID, "Ostali terenski poslovi pri obrani od poplava", d.Napomena
	if err := o.svc.SpremiDezurstvo(ctx, ivo, o.ovlUprav, o.opseg, o.dnevnik, ivin); err != nil {
		t.Fatal(err)
	}
	d, _ = o.svc.repo.GetDezurstvo(ctx, jutro.ID)
	if d.Mjesto != models.MjestoTeren || !strings.Contains(d.Napomena, "[opis ispravio Ivo Ivić ") ||
		!strings.Contains(d.Napomena, "prije: „Administrativni rad pri provedbi obrane od poplava”]") || strings.Count(d.Napomena, "[opis ispravio Ana Anić ") != 1 {
		t.Errorf("uprava mijenja ispravljeni opis: %+v", d)
	}
	// uprava ga i miče
	if err := o.svc.MakniDezurstvo(ctx, ana, o.ovlUprav, o.dnevnik, jutro.ID); err != nil {
		t.Errorf("uprava miče zapis s ispravljenim opisom: %v", err)
	}
}

// Odluke (8. 10. 2026.), pitanje 10: mjesto rada izvedeno iz opisa s popisa
// zapisuje se kad uprava centra dežurstvo potvrdi. Potvrđeni sati tako ne
// ovise o tome hoće li se popis opisa poslije promijeniti, a čvor sa
// starijim programom, koji svako mjesto osim terena broji kao ured, ne
// plaća isti teren kao ured. Pero Perićev obilazak stigao je bez mjesta,
// Ana Anić ga potvrđuje.
func TestPotvrdaZapisujeIzvedenoMjesto(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	ana := &models.User{ID: uuid.New(), FullName: "Ana Anić"}
	obilazak := o.peroDezura(dzSat(9, 8, 0), dzSat(9, 12, 0))
	obilazak.Opis = "Obilazak i pregled obrambenih objekata"
	if err := o.svc.SpremiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik, obilazak); err != nil {
		t.Fatal(err)
	}
	if _, err := o.baza.Exec(`UPDATE dezurstva SET mjesto = '' WHERE id = ?`, obilazak.ID); err != nil {
		t.Fatal(err)
	}
	if d, _ := o.svc.repo.GetDezurstvo(ctx, obilazak.ID); d.Mjesto != "" || d.MjestoRada() != models.MjestoTeren {
		t.Fatalf("priprema: %+v", d)
	}
	if err := o.svc.PotvrdiDezurstvo(ctx, ana, o.ovlUprav, o.dnevnik, obilazak.ID); err != nil {
		t.Fatal(err)
	}
	if d, _ := o.svc.repo.GetDezurstvo(ctx, obilazak.ID); !d.Potvrdeno() || d.Mjesto != models.MjestoTeren {
		t.Errorf("potvrđeno bez zapisanog mjesta: %+v", d)
	}
}

// Odluke (8. 10. 2026.), pitanje 10: ispravak opisa rada istim opisom ne
// mijenja ništa ni kad mjesto rada nije zapisano nego se izvodi iz opisa
// (stariji zapis ili zapis s drugog čvora). Mjesto je i tada poznato, pa
// potvrda ostaje, napomena ne dobiva trag ispravka, nova inačica se ne
// piše, a opis i dalje ne bira uprava — osoba čije je dežurstvo s njim radi
// kao i prije. Pero Perićev obilazak stigao je bez zapisanog mjesta i
// potvrđen je; Ana Anić iz uprave centra izabere opis koji već ima.
func TestIspravakIstimOpisomBezZapisanogMjesta(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	ana := &models.User{ID: uuid.New(), FullName: "Ana Anić"}
	obilazak := o.peroDezura(dzSat(9, 8, 0), dzSat(9, 12, 0))
	obilazak.Opis = "Obilazak i pregled obrambenih objekata"
	if err := o.svc.SpremiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik, obilazak); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.PotvrdiDezurstvo(ctx, ana, o.ovlUprav, o.dnevnik, obilazak.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := o.baza.Exec(`UPDATE dezurstva SET mjesto = '' WHERE id = ?`, obilazak.ID); err != nil {
		t.Fatal(err)
	}
	prije, _ := o.svc.repo.GetDezurstvo(ctx, obilazak.ID)
	if prije.Mjesto != "" || prije.MjestoRada() != models.MjestoTeren || !prije.Potvrdeno() || prije.OpisBiraUprava() {
		t.Fatalf("priprema: %+v", prije)
	}
	verzija := func() int {
		t.Helper()
		var n int
		if err := o.baza.QueryRow(`SELECT count(*) FROM record_versions WHERE entity = ? AND entity_id = ?`, repository.EntityDezurstva, obilazak.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	n := verzija()

	promijenjeno, err := o.svc.IspraviOpisDezurstva(ctx, ana, o.ovlUprav, o.dnevnik, obilazak.ID, "Obilazak i pregled obrambenih objekata")
	if err != nil || promijenjeno {
		t.Errorf("ispravak istim opisom: promijenjeno %v, %v", promijenjeno, err)
	}
	d, _ := o.svc.repo.GetDezurstvo(ctx, obilazak.ID)
	if d.Napomena != prije.Napomena || !d.Potvrdeno() || d.OpisBiraUprava() {
		t.Errorf("ispravak istim opisom promijenio je dežurstvo: napomena %q, potvrdio %q, opis bira uprava %v", d.Napomena, d.Potvrdio, d.OpisBiraUprava())
	}
	if m := verzija(); m != n {
		t.Errorf("ispravak istim opisom upisao je inačicu: %d → %d", n, m)
	}
}
