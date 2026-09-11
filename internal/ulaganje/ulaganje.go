// Paket ulaganje pretvara operativna očitanja u arhivski niz.
//
// Očitanje u knjizi verzija stoji 1.186 bajta: redak, kazala i verzija. Ista
// vrijednost u arhivi stoji tri. Dvadeset i dvije letve sa satnim očitanjima
// znače 230 MB godišnje u knjizi naspram 600 kB u arhivi — bez ulaganja baza s
// vremenom postane neupotrebljiva.
//
// Ulaže se u stablo s datotekama, ne izravno u arhivsku bazu: ondje su podaci
// obnovljivi i arhiva se iz njih uvijek može izgraditi iznova. To je i jedina
// mreža koja brisanje čini sigurnim.
//
// Ulaganje i zaboravljanje su dva koraka, i to namjerno. Uloženo se ne briše
// dok se ne provjeri da je doista u arhivi — mjerenje se ne da ponoviti.
package ulaganje

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"sort"
	"time"

	"gocop/internal/arhiva"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Zadani izvori pod kojima uloženo stoji u arhivi.
const (
	IzvorDojave = "cop"
	IzvorRucnog = "cop-rucno"
)

// Zahtjev je što se ulaže i odakle.
type Zahtjev struct {
	Baza       *sql.DB
	ArhivaPut  string
	Koren      string
	Cvor       string
	Letva      string // šifra postaje
	Od, Do     time.Time
	Izvor      string // prazno → IzvorDojave
	IzvorRucno string // prazno → IzvorRucnog
	Vrsta      string // prazno → zatečena u arhivi, pa pogađanje iz gustoće
	Izdanje    string // oznaka koja se upisuje uz uloženo; prazno → današnji dan
}

func (z Zahtjev) izvor() string {
	if z.Izvor != "" {
		return z.Izvor
	}
	return IzvorDojave
}

func (z Zahtjev) izvorRucnog() string {
	if z.IzvorRucno != "" {
		return z.IzvorRucno
	}
	return IzvorRucnog
}

// Pregled je što bi ušlo u arhivu, prije nego išta uđe.
type Pregled struct {
	Postaja models.Station
	Sliv    string

	Ukupno         int
	Mjereno        int
	Rucno          int
	Preracunato    int
	Sumnjivo       int
	BezVrijednosti int
	SBiljeskom     int

	Izvor       string
	IzvorRucnog string
	Vrsta       string
	VrstaRucnog string

	Od, Do time.Time

	razvrstano
}

// Ima javlja bi li ulaganje uopće nešto zapisalo.
func (p *Pregled) Ima() bool {
	return len(p.mjereno)+len(p.rucno)+len(p.preracunato) > 0
}

type razvrstano struct {
	mjereno     []arhiva.Redak
	rucno       []arhiva.Redak
	preracunato []arhiva.Redak
	biljeske    []sBiljeskom
	ulozeniID   []string
}

type sBiljeskom struct {
	Vrijeme time.Time
	Vrsta   string
	Tekst   string
	Tko     string
}

// Pripremi čita očitanja i razvrstava ih, ali ništa ne mijenja.
func Pripremi(ctx context.Context, z Zahtjev) (*Pregled, error) {
	postaja, err := PostajaPoSifri(ctx, z.Baza, z.Letva)
	if err != nil {
		return nil, err
	}
	letve, err := arhiva.Letve(z.Koren)
	if err != nil {
		return nil, err
	}
	sliv := letve[postaja.Code]
	if sliv == "" {
		return nil, fmt.Errorf("letva %q nije u stablu %s — ondje se ulaže, pa mora imati mjesto",
			postaja.Code, z.Koren)
	}

	ocitanja, err := ocitanjaZaUlaganje(ctx, z.Baza, postaja.ID.String(), z.Od, z.Do)
	if err != nil {
		return nil, err
	}
	p := &Pregled{Postaja: postaja, Sliv: sliv, Ukupno: len(ocitanja),
		Izvor: z.izvor(), IzvorRucnog: z.izvorRucnog()}
	p.razvrstano, p.Sumnjivo, p.BezVrijednosti = razvrstaj(ocitanja)
	p.Mjereno, p.Rucno, p.Preracunato = len(p.mjereno), len(p.rucno), len(p.preracunato)
	p.SBiljeskom = len(p.biljeske)

	p.Vrsta = z.Vrsta
	if p.Vrsta == "" {
		p.Vrsta = pogodiVrstu(p.mjereno)
	}
	// Gdje niz već postoji, ulaže se u njega. Pogađanje iz gustoće vrijedi samo
	// kad se ulaže na prazno: jedno jedino očitanje izgleda kao jutarnje, pa bi
	// razdvojilo izvor na dva niza i vrijednost bi pala u dnevni umjesto u
	// satni — što je provjera i uhvatila.
	if v := zatecenaVrsta(z.ArhivaPut, postaja.Code, p.Izvor); v != "" {
		p.Vrsta = v
	}
	p.VrstaRucnog = p.Vrsta
	if v := zatecenaVrsta(z.ArhivaPut, postaja.Code, p.IzvorRucnog); v != "" {
		p.VrstaRucnog = v
	}

	svi := append(append(append([]arhiva.Redak{}, p.mjereno...), p.rucno...), p.preracunato...)
	if len(svi) > 0 {
		sort.Slice(svi, func(a, b int) bool { return svi[a].Vrijeme.Before(svi[b].Vrijeme) })
		p.Od, p.Do = svi[0].Vrijeme, svi[len(svi)-1].Vrijeme
	}
	return p, nil
}

// Ishod je što je ulaganje napravilo.
type Ishod struct {
	Putovi     []string
	Nizova     int
	Ocitanja   int
	Spojenih   int
	Provjereno int
	Biljeski   int
	Oznaceno   int
	Oznaka     string
}

// Ulozi zapisuje razvrstano u stablo, gradi letvu, provjerava da je sve doista
// u arhivi i tek onda označava očitanja kao uložena. Ništa se ne briše.
func (p *Pregled) Ulozi(ctx context.Context, z Zahtjev, zapisi io.Writer) (*Ishod, error) {
	if zapisi == nil {
		zapisi = io.Discard
	}
	if !p.Ima() {
		return nil, fmt.Errorf("nema nijednog očitanja za ulaganje u tom razdoblju")
	}
	iz := &Ishod{}

	upisi := func(izvor, vrsta string, redci []arhiva.Redak) error {
		if len(redci) == 0 {
			return nil
		}
		put, err := arhiva.Dopuni(z.Koren, p.Sliv, p.Postaja.Code, izvor, "vodostaj", vrsta, redci)
		if err != nil {
			return fmt.Errorf("ulaganje u %s: %w", izvor, err)
		}
		fmt.Fprintf(zapisi, "zapisano: %s (%d)\n", put, len(redci))
		iz.Putovi = append(iz.Putovi, put)
		return nil
	}

	javi(zapisi, "zapisujem u stablo", 0, 0)
	if err := upisi(p.Izvor, p.Vrsta, p.mjereno); err != nil {
		return nil, err
	}
	// Ručno očitanje ide kao satni niz, iako nije satno: arhiva u satnom nizu
	// drži trenutke, a ne pune sate — VITUKI ondje stoji u 03:30. Kao "jutarnji"
	// bi palo u dnevni niz i izgubilo svoj sat, a 8. rujna su na Batini tri
	// očitanja u danu: 05, 13 i 21 h.
	if err := upisi(p.IzvorRucnog, p.VrstaRucnog, p.rucno); err != nil {
		return nil, err
	}
	if len(p.rucno) > 0 {
		if err := upisiIzvorRucnog(z.ArhivaPut, p.IzvorRucnog); err != nil {
			return nil, fmt.Errorf("izvor %s: %w", p.IzvorRucnog, err)
		}
	}
	if err := upisi("preracun-"+p.Izvor, p.Vrsta, p.preracunato); err != nil {
		return nil, err
	}

	javi(zapisi, "gradim letvu iz stabla", 0, 0)
	izvjestaj, err := arhiva.Izgradi(z.Koren, z.ArhivaPut, p.Postaja.Code, zapisi)
	if err != nil {
		return nil, fmt.Errorf("gradnja: %w", err)
	}
	iz.Nizova, iz.Ocitanja, iz.Spojenih = izvjestaj.Nizova, izvjestaj.Ocitanja, izvjestaj.Spojenih

	// Provjera prije ikakvog označavanja: je li svako uloženo očitanje doista u
	// arhivi, u svom nizu i sa svojom vrijednošću. Mjerenje se ne može ponoviti,
	// pa se ne vjeruje na riječ.
	//
	// Rekonstruirano mora biti u provjeri kao i ostalo. Prva izvedba ga je
	// izostavila iz provjere, a ostavila u skupu koji se označava kao uloženo —
	// pa se moglo obrisati iz operative a da nitko nije potvrdio da je stiglo.
	javi(zapisi, "provjeravam je li sve stiglo u arhivu", 0, 0)
	provjera := p.nizoviZaProvjeru()
	ukupno := 0
	for _, n := range provjera {
		ukupno += len(n.Redci)
	}
	nedostaje, err := ProvjeriUArhivi(z.ArhivaPut, p.Postaja.Code, provjera)
	if err != nil {
		return nil, err
	}
	if nedostaje > 0 {
		return nil, fmt.Errorf("provjera pala: %d od %d vrijednosti nije u arhivi — ništa se ne označava",
			nedostaje, ukupno)
	}
	iz.Provjereno = ukupno
	fmt.Fprintf(zapisi, "provjera: svih %d vrijednosti je u arhivi\n", ukupno)

	rec := ledger.New(z.Baza, z.Cvor)
	if len(p.biljeske) > 0 {
		bilRepo := repository.NewBiljeskaRepository(z.Baza, rec)
		n, err := bilRepo.Spremi(ctx, biljeskeZa(p.Postaja.Code, p.VrstaRucnog, p.biljeske))
		if err != nil {
			return nil, fmt.Errorf("bilješke: %w", err)
		}
		iz.Biljeski = n
		fmt.Fprintf(zapisi, "bilješki uz vrijednosti: %d\n", n)
	}

	iz.Oznaka = z.Izdanje
	if iz.Oznaka == "" {
		iz.Oznaka = time.Now().UTC().Format("2006-01-02")
	}
	iz.Oznaceno, err = oznaciUlozeno(ctx, z.Baza, p.ulozeniID, iz.Oznaka)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(zapisi, "označeno kao uloženo (%s): %d očitanja\n", iz.Oznaka, iz.Oznaceno)
	return iz, nil
}

// nizoviZaProvjeru kaže gdje svaka skupina mora završiti. Isti raspored po
// kojem se i upisuje, pa se ne može razići s njim.
func (p *Pregled) nizoviZaProvjeru() []UNizu {
	return []UNizu{
		{Izvor: p.Izvor, Velicina: "vodostaj", Vrsta: p.Vrsta, Redci: p.mjereno},
		{Izvor: p.IzvorRucnog, Velicina: "vodostaj", Vrsta: p.VrstaRucnog, Redci: p.rucno},
		{Izvor: "preracun-" + p.Izvor, Velicina: "vodostaj", Vrsta: p.Vrsta, Redci: p.preracunato},
	}
}

// javi šalje korak odredištu koje ga zna primiti; naredbeni redak ne mora.
func javi(zapisi io.Writer, sto string, gotovo, ukupno int) {
	if j, ok := zapisi.(arhiva.Javljac); ok {
		j.Korak(sto, gotovo, ukupno)
	}
}
