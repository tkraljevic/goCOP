package main

import (
	"context"
	"path/filepath"
	"testing"

	"gocop/internal/peers"
	"gocop/internal/poslovi"
	"gocop/internal/prognoza"
	"gocop/internal/web"
)

// Letve i osobe su izmišljene.

// Nacrt se sprema i odbacuje samo na računalu koje izdaje prognozu.
func TestUrednikModela(t *testing.T) {
	pb, err := prognoza.Otvori(filepath.Join(t.TempDir(), "prognoze.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer pb.Close()
	uloge := &probneUloge{}
	u := &urednikModela{pb: pb, mreza: uloge}
	n := prognoza.NacrtLetve{Tko: "Iva Proba", Zapis: prognoza.ZapisLetve{Oblik: prognoza.OblikModelaLetve, Letva: "potok-dole",
		Metoda: prognoza.MetodaLanac, Velicina: "protok", Racun: prognoza.RacunLetve{Ulazi: []string{"potok-vrh"}}}}
	if u.Izdaje() || u.SpremiNacrt(n) != errNeIzdaje || u.ObrisiNacrt("potok-dole") != errNeIzdaje {
		t.Error("računalo koje ne izdaje ne smije spremati ni odbacivati nacrt")
	}
	uloge.izdaje = true
	// bez arhive i registra provjera sektora i satnog protoka se ne radi
	if err := u.SpremiNacrt(n); err != nil || !u.Izdaje() {
		t.Fatalf("spremanje: %v", err)
	}
	if svi, err := prognoza.Nacrti(pb); err != nil || svi["potok-dole"].Tko != "Iva Proba" {
		t.Errorf("nacrt nije u bazi: %v, %v", svi, err)
	}
	n.Zapis.Racun.Ulazi = nil
	if err := u.SpremiNacrt(n); err == nil {
		t.Error("nacrt bez ulaza mora biti odbijen")
	}
	if err := u.ObrisiNacrt("potok-dole"); err != nil {
		t.Fatal(err)
	}
	if svi, err := prognoza.Nacrti(pb); err != nil || len(svi) != 0 {
		t.Errorf("nacrt nije odbačen: %v, %v", svi, err)
	}
}

// probneUloge su uloge računala koje test mijenja.
type probneUloge struct{ izdaje bool }

func (p *probneUloge) TrenutneUloge() peers.Uloge { return peers.Uloge{Izdaje: p.izdaje} }

// probniPosluzitelj pamti što mu je uključeno.
type probniPosluzitelj struct {
	priprema web.PripremaModela
	urednik  web.UrednikModela
}

func (p *probniPosluzitelj) SetPripremaModela(f web.PripremaModela) { p.priprema = f }
func (p *probniPosluzitelj) SetUrednikModela(u web.UrednikModela)   { p.urednik = u }

// Poslužitelj dobiva pripremu modela i urednika koji izdavanje pita pri svakom
// pozivu, jer se uloga računala mijenja dok program radi.
func TestUkljuciModel(t *testing.T) {
	uloge := &probneUloge{}
	p := &probniPosluzitelj{}
	ukljuciModel(p, func(context.Context, *poslovi.Posao) error { return nil }, &urednikModela{mreza: uloge})
	if p.priprema == nil || p.urednik == nil || p.urednik.Izdaje() {
		t.Fatal("poslužitelj nije dobio pripremu i urednika")
	}
	uloge.izdaje = true
	if !p.urednik.Izdaje() {
		t.Error("urednik mora vidjeti promjenu uloge")
	}
}
