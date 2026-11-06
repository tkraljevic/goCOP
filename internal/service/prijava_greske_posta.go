package service

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"

	"gocop/internal/models"
)

// E-pošta o novoj prijavi: šalje je samo računalo na kojem je prijava nastala,
// jednom, u pozadini. Prijava koja stigne razmjenom ne dolazi ovamo, pa poruka
// ne ide sa svakog računala u mreži. Slanje ne smije usporiti ni srušiti slanje
// prijave: neuspjela poruka ne dira prijavu, a administrator je i dalje vidi
// u Administraciji.
const (
	// najvisePorukaOPrijavamaUSatu ograđuje poruke s ovog računala, po prijavi.
	// Računaju se poslane; prijave iznad granice vide se samo u Administraciji.
	najvisePorukaOPrijavamaUSatu = 10
	// mjestaURedu: što ne stane u red samo ostaje bez poruke
	mjestaURedu = 16
	// najviseOpisaUPoruci: cijeli opis je u programu
	najviseOpisaUPoruci = 1000
)

// PosiljateljPorukaOPrijavi šalje poruku globalnim administratorima (DrugiKorak)
type PosiljateljPorukaOPrijavi interface {
	PrimateljiObavijesti(ctx context.Context) ([]PrimateljObavijesti, error)
	PosaljiPorukuOPrijavi(ctx context.Context, p PrimateljObavijesti, predmet, tekst string) error
}

// PokreniPoruke pokreće slanje poruka u pozadini; adresa daje javnu adresu
// programa, za poveznicu. Pozvati prije nego što poslužitelj počne primati
// zahtjeve. Vraća zaustavljanje, koje slanje otkaže i pričeka, da se baza ne
// zatvori prije nego što je poslana poruka zapisana.
func (s *PrijavaGreskeService) PokreniPoruke(ctx context.Context, p PosiljateljPorukaOPrijavi, adresa func() string) func() {
	ctx, otkazi := context.WithCancel(ctx)
	s.red = make(chan *models.PrijavaGreske, mjestaURedu)
	var teku sync.WaitGroup
	teku.Go(func() {
		for {
			select {
			case <-ctx.Done():
				return
			case prijava := <-s.red:
				s.javiPrijavu(ctx, p, adresa, prijava)
			}
		}
	})
	return func() {
		otkazi()
		teku.Wait()
	}
}

// staviURed predaje prijavu slanju i odmah se vraća
func (s *PrijavaGreskeService) staviURed(p *models.PrijavaGreske) {
	if s.red == nil {
		return
	}
	kopija := *p // pozivatelj zadržava svoju
	select {
	case s.red <- &kopija:
	default:
		log.Printf("prijava greške: red za e-poštu je pun, poruka o prijavi %s se ne šalje", p.ID)
	}
}

// javiPrijavu šalje poruku administratorima i pamti da je poslana. Kvar u
// slanju ostaje ovdje, ne ruši program.
func (s *PrijavaGreskeService) javiPrijavu(ctx context.Context, posiljatelj PosiljateljPorukaOPrijavi, adresa func() string, p *models.PrijavaGreske) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("prijava greške: slanje e-pošte je palo: %v", r)
		}
	}()
	if !s.imaMjestaZaPoruku(ctx, p) {
		return
	}
	primatelji, err := posiljatelj.PrimateljiObavijesti(ctx)
	if err != nil {
		log.Printf("prijava greške: primatelji poruke se ne daju pročitati: %v", err)
		return
	}
	predmet, tekst := tekstPorukeOPrijavi(p, adresa())
	if !posaljiSvima(ctx, posiljatelj, primatelji, p.UserID, predmet, tekst) {
		return
	}
	if err := s.repo.ZabiljeziPoruku(context.WithoutCancel(ctx), p.ID, s.sat()); err != nil {
		log.Printf("prijava greške: poslana poruka nije zapisana: %v", err)
	}
}

// imaMjestaZaPoruku gleda koliko je poruka ovo računalo poslalo u zadnjih sat vremena
func (s *PrijavaGreskeService) imaMjestaZaPoruku(ctx context.Context, p *models.PrijavaGreske) bool {
	poslano, err := s.repo.BrojPorukaOd(ctx, s.sat().Add(-prozorPrijava))
	if err != nil {
		log.Printf("prijava greške: broj poslanih poruka se ne da pročitati: %v", err)
		return false
	}
	if poslano >= najvisePorukaOPrijavamaUSatu {
		log.Printf("prijava greške: u zadnjih sat vremena poslano je %d poruka, pa se za prijavu %s poruka ne šalje", poslano, p.ID)
		return false
	}
	return true
}

// posaljiSvima šalje poruku svakom administratoru osim onoga tko je prijavu
// poslao; javlja je li je itko dobio
func posaljiSvima(ctx context.Context, posiljatelj PosiljateljPorukaOPrijavi, primatelji []PrimateljObavijesti, posiljao, predmet, tekst string) bool {
	var uspjelo bool
	for _, pr := range primatelji {
		if pr.ID == posiljao || ctx.Err() != nil {
			continue
		}
		err := posiljatelj.PosaljiPorukuOPrijavi(ctx, pr, predmet, tekst)
		if slanjeNeRadi(err) {
			break
		}
		if err != nil {
			log.Printf("prijava greške: poruka nije poslana na %s: %v", MaskirajAdresu(pr.Adresa), err)
			continue
		}
		uspjelo = true
	}
	return uspjelo
}

// slanjeNeRadi: pošta nije podešena na ovom računalu ili trenutno ne radi, pa
// ostale ne treba ni pokušati. Razlog je, gdje treba, već u dnevniku.
func slanjeNeRadi(err error) bool {
	return errors.Is(err, ErrNemaPosiljatelja) || errors.Is(err, ErrNemaKljucaPrijave) ||
		errors.Is(err, ErrPosiljateljNeispravan) || errors.Is(err, ErrSlanjeZastalo)
}

// tekstPorukeOPrijavi sastavlja poruku. Slika ne ide u poruku, ona ostaje u
// programu, iza prijave. Bez javne adrese poruka kaže gdje je prijava u programu.
// Poruka ide računom programa, a tekst piše osoba, pa program najprije kaže svoje,
// a njezin tekst označi kao tuđi: lažna poveznica u tekstu ne smije izgledati kao
// da ju je dao program. Zato ni naziv stranice, koji šalje preglednik, ne ide u poruku.
func tekstPorukeOPrijavi(p *models.PrijavaGreske, adresa string) (predmet, tekst string) {
	var b strings.Builder
	if adresa = strings.TrimRight(strings.TrimSpace(adresa), "/"); adresa != "" {
		b.WriteString("Otvori prijavu: " + adresa + "/administracija/prijave-gresaka/" + p.ID + "\n\n")
	} else {
		b.WriteString("Prijava je u programu, pod Administracija, Prijave grešaka i prijedlozi.\n\n")
	}
	redak := func(naziv, vrijednost string) {
		if vrijednost = strings.Join(strings.Fields(vrijednost), " "); vrijednost != "" {
			b.WriteString(naziv + ": " + vrijednost + "\n")
		}
	}
	redak("Šalje", p.Ime)
	redak("Uloga", p.Uloga)
	redak("Vrijeme", p.Kad())
	redak("Stranica", putZaPoruku(p.Stranica))
	redak("Izdanje programa", p.Izdanje)
	redak("Računalo", p.Racunalo())
	if p.SlikaOtisak != "" {
		b.WriteString("Uz prijavu je slika zaslona. Vidi se kad otvoriš prijavu.\n")
	}
	b.WriteString("\nTekst koji je napisala osoba:\n" + citiraj(skraceniOpis(p.Opis)))
	return predmetPoruke(p), b.String()
}

// predmetPoruke sastavlja samo program, od vrste prijave i imena s računa
func predmetPoruke(p *models.PrijavaGreske) string {
	predmet := "goCOP: prijava greške"
	if p.Vrsta == models.VrstaPrijavePrijedlog {
		predmet = "goCOP: prijedlog"
	}
	if ime := strings.Join(strings.Fields(p.Ime), " "); ime != "" {
		predmet += ", šalje " + ime
	}
	return predmet
}

// putZaPoruku je put stranice bez izbora iz upita. Put koji izgleda kao adresa
// drugog mjesta ne ide u poruku, jer bi ga pošta pretvorila u poveznicu.
func putZaPoruku(stranica string) string {
	put, _, _ := strings.Cut(stranica, "?")
	if strings.Contains(put, "://") {
		return ""
	}
	return put
}

// citiraj svaki redak teksta označi s „> ” i sažme uzastopne prazne retke, da
// prazni reci ne odguraju oznaku s ekrana
func citiraj(opis string) string {
	opis = strings.NewReplacer("\u2028", "\n", "\u2029", "\n").Replace(opis)
	var b strings.Builder
	prazan := false
	for _, r := range strings.Split(opis, "\n") {
		r = strings.TrimSpace(r)
		if r == "" && prazan {
			continue
		}
		prazan = r == ""
		b.WriteString(strings.TrimSpace("> "+r) + "\n")
	}
	return b.String()
}

func skraceniOpis(opis string) string {
	r := []rune(opis)
	if len(r) <= najviseOpisaUPoruci {
		return opis
	}
	return string(r[:najviseOpisaUPoruci]) + " …"
}
