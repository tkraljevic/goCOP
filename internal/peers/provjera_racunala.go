package peers

// Provjera veze s računalom (plan WEB synca, F2a, gumb „Provjeri vezu s
// računalom” na stranici Sinkronizacija). Za odabrano računalo ovo nazove
// svaku adresu koju za njega zna, istim putem kao razmjena, i za svaku kaže
// kako je prošlo. Na svakoj adresi rukovanje stane čim druga strana dokaže
// ključ (razmjena.ProvjeriKljucOn), pa razgovora nema: ništa se ne prima ni
// ne šalje, a drugo računalo to ne broji kao neuspjelu razmjenu. Adrese se
// zovu usporedno. Ishod ne ide u pokušaje razmjene (pokusaji.go), jer
// razmjena na toj adresi nije ni počela, a tvrdnje o adresama se ne troše.
// Poziv, odziv, udarce i QUIC dodaju F2b i F3.

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net"
	"sync"
	"time"

	"gocop/internal/razmjena"
)

// rokProvjereAdrese je koliko jedna adresa ima za odgovor
const rokProvjereAdrese = 10 * time.Second

// IzvorAdrese kaže odakle ovo računalo zna adresu drugog
type IzvorAdrese string

const (
	IzvorLokalnaMreza IzvorAdrese = "lokalna-mreza" // javio se u lokalnoj mreži, sad ili nedavno
	IzvorZapis        IzvorAdrese = "zapis"         // adresa iz zapisa računala u mreži
	IzvorNaucena      IzvorAdrese = "naucena"       // ovo računalo je tamo uspješno razmijenilo
	IzvorDolazna      IzvorAdrese = "dolazna"       // s nje je stigla razmjena
	IzvorTailscale    IzvorAdrese = "tailscale"     // Tailscale adresa koju je računalo samo javilo
	IzvorTunel        IzvorAdrese = "tunel"         // tunel iz zapisa računala
)

// kandidatIzvora je adresa za spajanje i odakle je ovo računalo zna
type kandidatIzvora struct {
	kandidat
	izvor IzvorAdrese
}

// adreseSIzvorom su adrese čvora redom kojim ih razmjena zove
// (adreseZaSpajanje), svaka s izvorom s kojim je prvi put došla
func (s *Service) adreseSIzvorom(ctx context.Context, peer *Peer) []kandidatIzvora {
	izravne, tuneli := izravneITuneli(peer.Addresses, s.exchangePortOf(*peer))
	skupine := []struct {
		izvor     IzvorAdrese
		kandidati []kandidat
	}{
		{IzvorLokalnaMreza, s.uLokalnojMrezi(ctx, peer.NodeID)},
		{IzvorLokalnaMreza, s.tvrdnjePutem(peer.NodeID, razmjena.PutLAN)},
		{IzvorZapis, uz(razmjena.PutAdresa, izravne)},
		{IzvorNaucena, uz(razmjena.PutAdresa, s.naucene(ctx, peer.NodeID))},
		{IzvorDolazna, s.tvrdnjePutem(peer.NodeID, razmjena.PutAdresa)},
		{IzvorTailscale, uz(razmjena.PutTailscale, s.tailscaleZaSpajanje(ctx, peer.NodeID))},
		{IzvorTunel, uz(razmjena.PutKrozTunel, tuneli)},
	}
	var sve []kandidatIzvora
	for _, g := range skupine {
		for _, k := range g.kandidati {
			sve = append(sve, kandidatIzvora{kandidat: k, izvor: g.izvor})
		}
	}
	return bezDvojnika(sve)
}

// RazlogAdrese je zašto adresa nije prošla, kad se zna
type RazlogAdrese string

const (
	RazlogAdreseDrugiKljuc  RazlogAdrese = "drugi-kljuc"  // javlja se drugo računalo
	RazlogAdreseNemaOdgovor RazlogAdrese = "nema-odgovor" // nitko ne odgovori u roku
	RazlogAdreseOdbijeno    RazlogAdrese = "odbijeno"     // računalo je tu, ali na portu nitko ne sluša
	RazlogAdreseNemaPuta    RazlogAdrese = "nema-puta"    // do adrese nema rute
)

// AdresaProvjere je ishod provjere jedne adrese. Nosi adresu i sirovu
// grešku, pa je samo za administratora.
type AdresaProvjere struct {
	Adresa   string        `json:"adresa"`
	Put      string        `json:"put"` // naziv puta kao na pločici: LAN sync, SERVER sync, Tailscale
	Izvor    IzvorAdrese   `json:"izvor"`
	Kada     time.Time     `json:"kada"`
	Trajanje time.Duration `json:"trajanje"`
	RTT      time.Duration `json:"rtt,omitempty"` // do dokaza ključa; 0 kad nije prošla
	Greska   string        `json:"greska,omitempty"`
	Razlog   RazlogAdrese  `json:"razlog,omitempty"` // prazno uz grešku: razlog se ne zna
}

// Prosla javlja da je druga strana na toj adresi dokazala pravi ključ
func (a AdresaProvjere) Prosla() bool { return a.Greska == "" }

// ProvjeraRacunala je ishod provjere veze s jednim računalom
type ProvjeraRacunala struct {
	NodeID string           `json:"node_id"`
	Naziv  string           `json:"naziv"`
	Kad    time.Time        `json:"kad"`
	Adrese []AdresaProvjere `json:"adrese"`
}

// ProvjeriVezuS provjeri svaku poznatu adresu računala; greška je samo kad
// se računalo ne smije zvati (nepoznato, nije član)
func (s *Service) ProvjeriVezuS(ctx context.Context, nodeID string) (ProvjeraRacunala, error) {
	peer, expect, err := s.partnerZaRazmjenu(ctx, nodeID)
	if err != nil {
		return ProvjeraRacunala{}, err
	}
	kandidati := s.adreseSIzvorom(ctx, peer)
	out := ProvjeraRacunala{NodeID: peer.NodeID, Naziv: label(*peer), Kad: time.Now(), Adrese: make([]AdresaProvjere, len(kandidati))}
	var wg sync.WaitGroup
	for i, k := range kandidati {
		wg.Go(func() { out.Adrese[i] = s.provjeriAdresu(ctx, peer, k, expect) })
	}
	wg.Wait()
	return out, nil
}

// provjeriAdresu provjeri jednu adresu u roku rokProvjereAdrese
func (s *Service) provjeriAdresu(ctx context.Context, peer *Peer, k kandidatIzvora, expect ed25519.PublicKey) AdresaProvjere {
	a := AdresaProvjere{Adresa: k.adresa, Put: razmjena.NazivPuta(k.put, privatnaAdresa(k.adresa)), Izvor: k.izvor, Kada: time.Now()}
	ctx, otkazi := context.WithTimeout(ctx, rokProvjereAdrese)
	defer otkazi()
	rtt, err := s.provjeriKljuc(ctx, peer, k.kandidat, expect)
	a.Trajanje = time.Since(a.Kada)
	if err != nil {
		a.Greska, a.Razlog = err.Error(), razlogAdrese(err)
		return a
	}
	a.RTT = rtt
	return a
}

// provjeriKljuc je spoji bez razgovora: kroz tunel ili na portu razmjene,
// do dokaza ključa druge strane
func (s *Service) provjeriKljuc(ctx context.Context, peer *Peer, k kandidat, expect ed25519.PublicKey) (time.Duration, error) {
	if k.put == razmjena.PutKrozTunel {
		return s.tunelKlijent.ProvjeriTunel(ctx, s.node.key, s.protokol(), k.adresa, expect)
	}
	adresa := withPort(k.adresa, s.exchangePortOf(*peer))
	raw, err := s.otvori(ctx, adresa)
	if err != nil {
		return 0, err
	}
	return razmjena.ProvjeriKljucOn(ctx, raw, s.node.key, s.protokol(), adresa, expect)
}

// razloziAdrese prepoznaju grešku, redom; sirova greška ostaje uz razlog
var razloziAdrese = []struct {
	razlog RazlogAdrese
	je     func(error) bool
}{
	{RazlogAdreseDrugiKljuc, func(err error) bool { return errors.Is(err, razmjena.ErrDrugiKljuc) }},
	{RazlogAdreseNemaOdgovor, isteklo},
	{RazlogAdreseOdbijeno, razmjena.VezaOdbijena},
	{RazlogAdreseNemaPuta, razmjena.BezRute},
}

// razlogAdrese je razlog za grešku provjere; prazno kad se ne prepozna
func razlogAdrese(err error) RazlogAdrese {
	for _, r := range razloziAdrese {
		if r.je(err) {
			return r.razlog
		}
	}
	return ""
}

// isteklo: nitko nije odgovorio u roku
func isteklo(err error) bool {
	var ne net.Error
	return errors.Is(err, context.DeadlineExceeded) || errors.As(err, &ne) && ne.Timeout()
}
