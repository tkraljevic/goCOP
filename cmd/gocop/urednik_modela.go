package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/poslovi"
	"gocop/internal/prognoza"
	"gocop/internal/web"
)

// urednikModela sprema nacrte modela prognoze za stranicu „Slaganje računa”.
// Model se slaže samo na računalu koje izdaje prognozu: nacrt se ondje priprema
// i primjenjuje, a ostalima model stiže razmjenom.
type urednikModela struct {
	pb, arhiva, ocitanja *sql.DB
	mreza                ulogeCvora
}

// ulogeCvora je dio mreže koji kaže što ovo računalo trenutno radi; pita se pri
// svakom pozivu, jer se uloga mijenja dok program radi.
type ulogeCvora interface{ TrenutneUloge() peers.Uloge }

var errNeIzdaje = errors.New("model se slaže na računalu koje izdaje prognozu")

func (u *urednikModela) Izdaje() bool { return u.mreza.TrenutneUloge().Izdaje }

// okvir je ono što provjera nacrta čita iz registra i arhive.
func (u *urednikModela) okvir() prognoza.OkvirModela {
	ctx := context.Background()
	return prognoza.OkvirModela{
		ImaSektor:      func(letva string) bool { return prognoza.LetvaImaSektor(ctx, u.ocitanja, letva) },
		ImaSatniProtok: func(letva string) bool { return prognoza.ImaSatniProtok(ctx, u.arhiva, letva) },
	}
}

func (u *urednikModela) SpremiNacrt(n prognoza.NacrtLetve) error {
	if !u.Izdaje() {
		return errNeIzdaje
	}
	return prognoza.SpremiNacrt(u.pb, n, u.okvir())
}

func (u *urednikModela) ObrisiNacrt(letva string) error {
	if !u.Izdaje() {
		return errNeIzdaje
	}
	return prognoza.ObrisiNacrt(u.pb, letva)
}

// pripremiNacrt namješta račun iz nacrta; testovi ga zamjenjuju, jer pravi traži
// arhivu sa satnim nizovima.
var pripremiNacrt = prognoza.PripremiNacrt

// PripremiNacrt namjesti račun iz spremljenog nacrta letve i upiše brojeve u
// nacrt. Model i prognoza se ne mijenjaju.
func (u *urednikModela) PripremiNacrt(letva string, p *poslovi.Posao) error {
	if !u.Izdaje() {
		return errNeIzdaje
	}
	p.Korak("namještanje računa iz arhive", 0, 0)
	n, err := pripremiNacrt(u.arhiva, u.pb, letva, time.Now().In(models.Zagreb).Format("2. 1. 2006. 15:04"), u.okvir())
	if err != nil {
		return err
	}
	p.Zavrsi(opisPripremeNacrta(n), nil)
	return nil
}

// opisPripremeNacrta je rečenica na kraju posla: koliko razina računa i dokle.
func opisPripremeNacrta(n prognoza.NacrtLetve) string {
	razina := 0
	if len(n.Zapis.Brojevi.Inacice) > 0 {
		razina = len(n.Zapis.Brojevi.Inacice[0])
	}
	return fmt.Sprintf("Račun je pripremljen: razina s računom %d, računa do %d h unaprijed. Prognoza se ne mijenja dok se model ne primijeni.",
		razina, n.Zapis.Racun.Doseg)
}

// posluziteljModela je dio poslužitelja kojem se uključuje rad s modelom.
type posluziteljModela interface {
	SetPripremaModela(web.PripremaModela)
	SetUrednikModela(web.UrednikModela)
}

// ukljuciModel daje poslužitelju pripremu modela i urednika nacrta.
func ukljuciModel(s posluziteljModela, priprema web.PripremaModela, u *urednikModela) {
	s.SetPripremaModela(priprema)
	s.SetUrednikModela(u)
}
