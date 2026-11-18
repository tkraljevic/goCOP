package peers

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"gocop/internal/ledger"
)

// CopPregled je ono što bi ugradnja paketa donijela, izračunato bez
// upisa: iste provjere kao UveziCop (potpis, izdavač, izdanje, pretplata i
// ograda), a knjiga, sadržaj i popis primljenih izdanja ostaju netaknuti
// (Odluke (18. 11. 2026.), Administracija, novo ponašanje 9).
type CopPregled struct {
	Manifest    CopManifest
	Verzija     int // zapisa u paketu
	Neispravnih int // ne bi se upisali (ledger.ProvjeriPrimljenu)
	Prima       int // prolaze pretplatu i ogradu ovog računala
	Novih       int // od njih još nisu u knjizi
	// Odbija kaže zašto bi se ugradnja odbila; prazno kad ne bi
	Odbija string
	Kanali []KanalPregleda
}

// KanalPregleda je jedan kanal paketa u pregledu
type KanalPregleda struct {
	Kanal                 string
	Verzija, Prima, Novih int
}

// PregledCop čita paket i kaže što bi ugradnja donijela. Paket koji se ne
// da pročitati vraća grešku; paket koji bi se odbio vraća pregled s
// razlogom u Odbija.
func (s *Service) PregledCop(ctx context.Context, put string) (CopPregled, error) {
	var pr CopPregled
	f, err := os.Open(put)
	if err != nil {
		return pr, err
	}
	defer f.Close() //nolint:errcheck // samo čitanje; paket čita zapise iz datoteke, pa ostaje otvorena do kraja
	p := &copPaket{}
	st, err := f.Stat()
	if err == nil {
		p, err = otvoriCop(f, st.Size())
	}
	pr.Manifest = p.m
	if err != nil {
		return pr, err
	}
	if pr.Odbija = s.razlogOdbijanja(ctx, p.m); pr.Odbija != "" {
		return pr, nil
	}
	versions, err := p.provjereniZapisi()
	if err != nil {
		return pr, err
	}
	return pr, s.prebrojiPregled(ctx, &pr, versions)
}

// provjereniZapisi su zapisi paketa, uz istu provjeru popisa sadržaja kao
// pri ugradnji
func (p *copPaket) provjereniZapisi() ([]ledger.Version, error) {
	versions, sadrzaji, err := p.zapisi()
	if err == nil {
		err = p.provjeriSadrzaje(sadrzaji)
	}
	return versions, err
}

// razlogOdbijanja kaže bi li ugradnja paket odbila: potpis i izdavač, pa
// izdanje prema već primljenom istog izdavača
func (s *Service) razlogOdbijanja(ctx context.Context, m CopManifest) string {
	var rep CopIzvjestaj
	if err := s.izdavacCopa(ctx, m, &rep); err != nil {
		return err.Error()
	}
	var primljeno int
	var otisak string
	// bez primljenog izdanja (ili tablice) ništa se ne odbija; ugradnja to
	// ionako provjerava ponovno
	if s.db.QueryRowContext(ctx, `SELECT izdanje, otisak FROM cop_primljena WHERE kljuc = ? AND izdao = ?`, m.Obuhvat.Kljuc(), m.Izdao).Scan(&primljeno, &otisak) != nil {
		return ""
	}
	return porukaIzdanja(m, primljeno, otisak)
}

// porukaIzdanja kaže zašto se izdanje ne ugrađuje, kao UveziCop: starije od
// primljenog ili isto s drugim otiskom; prazno kad se ugrađuje
func porukaIzdanja(m CopManifest, primljeno int, otisak string) string {
	switch {
	case m.Izdanje < primljeno:
		return fmt.Sprintf("paket je izdanje %d, a od čvora %s već je primljeno izdanje %d; starije se ne ugrađuje", m.Izdanje, m.Izdao, primljeno)
	case m.Izdanje == primljeno && m.Otisak != otisak:
		return fmt.Sprintf("izdanje %d od čvora %s već je primljeno s drugim otiskom; paket se ne ugrađuje", m.Izdanje, m.Izdao)
	}
	return ""
}

// prebrojiPregled broji po kanalu zapise paketa, one koje bi ovo računalo
// primilo i one kojih u knjizi još nema
func (s *Service) prebrojiPregled(ctx context.Context, pr *CopPregled, versions []ledger.Version) error {
	ispravne, neispravnih := s.ispravneZaPregled(versions)
	wanted, ima, err := s.zaPregled(ctx, ispravne)
	if err != nil {
		return err
	}
	pr.Verzija, pr.Neispravnih = len(versions), neispravnih
	pr.Kanali, pr.Prima, pr.Novih = kanaliPregleda(versions, wanted, ima)
	return nil
}

// ispravneZaPregled su zapisi koje bi ugradnja upisala; neispravne samo
// broji, bez zapisa u dnevnik (to radi ugradnja, preskociNeispravne)
func (s *Service) ispravneZaPregled(versions []ledger.Version) ([]ledger.Version, int) {
	sada := ledger.Sada()
	ispravne := make([]ledger.Version, 0, len(versions))
	for _, v := range versions {
		if ledger.ProvjeriPrimljenu(v, s.node.ID, sada) == nil {
			ispravne = append(ispravne, v)
		}
	}
	return ispravne, len(versions) - len(ispravne)
}

// zaPregled su zapisi koje bi ovo računalo primilo po svojoj pretplati i
// ogradi, kao zaUvoz, i oznake onih koje knjiga već ima
func (s *Service) zaPregled(ctx context.Context, ispravne []ledger.Version) ([]ledger.Version, map[string]bool, error) {
	myWants, err := s.CurrentWants(ctx)
	if err != nil {
		return nil, nil, err
	}
	wanted, _ := s.zaUvoz(ispravne, s.wantsFunc(ctx, &myWants))
	ima, err := s.uKnjizi(ctx, wanted)
	return wanted, ima, err
}

// kanaliPregleda broji po kanalu, redom po imenu kanala
func kanaliPregleda(versions, wanted []ledger.Version, ima map[string]bool) (out []KanalPregleda, prima, novih int) {
	kanali := map[string]*KanalPregleda{}
	kanal := func(c string) *KanalPregleda {
		if kanali[c] == nil {
			kanali[c] = &KanalPregleda{Kanal: c}
		}
		return kanali[c]
	}
	for _, v := range versions {
		kanal(v.Channel).Verzija++
	}
	for _, v := range wanted {
		k := kanal(v.Channel)
		k.Prima++
		if !ima[v.VersionID] {
			k.Novih++
			novih++
		}
	}
	for _, k := range kanali {
		out = append(out, *k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kanal < out[j].Kanal })
	return out, len(wanted), novih
}

// obrokKnjige je koliko se oznaka verzija pita knjigu u jednom upitu
const obrokKnjige = 500

// uKnjizi su oznake verzija koje knjiga već ima, čitane u obrocima
func (s *Service) uKnjizi(ctx context.Context, versions []ledger.Version) (map[string]bool, error) {
	ima := map[string]bool{}
	for od := 0; od < len(versions); od += obrokKnjige {
		if err := s.dodajUKnjizi(ctx, ima, versions[od:min(od+obrokKnjige, len(versions))]); err != nil {
			return nil, err
		}
	}
	return ima, nil
}

// dodajUKnjizi upiše u ima oznake verzija iz dio koje knjiga već ima
func (s *Service) dodajUKnjizi(ctx context.Context, ima map[string]bool, dio []ledger.Version) error {
	args := make([]any, len(dio))
	for i, v := range dio {
		args[i] = v.VersionID
	}
	rows, err := s.db.QueryContext(ctx, `SELECT version_id FROM record_versions WHERE version_id IN (?`+strings.Repeat(",?", len(dio)-1)+`)`, args...)
	if err != nil {
		return err
	}
	defer rows.Close() //nolint:errcheck // samo čitanje
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ima[id] = true
		}
	}
	return rows.Err()
}
