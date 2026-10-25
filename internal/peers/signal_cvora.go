package peers

// WEB sync prema sastajalištu (plan WEB synca, F2b). Sastajalište se uzima
// samo iz lokalnih postavki: adresa (sastajaliste) i ime čvora u mreži
// (sastajaliste_cvor), a njegov ključ iz članstva (kljucClana), nikad iz
// zapisa čvora, koji može promijeniti bilo koji član. Član koji sam sebi
// upiše is_bootstrap zato ne postaje ničije sastajalište.
//
// Na stalnom čvoru s posluzi_sastajaliste sastajalište vjeruje istim
// pravilom kao razmjena (trusted, s članstvom iz certifikata, pa ulazi i
// tek upareno računalo), a nakon primjene članstava, ovlasti ili opoziva
// ponovno provjeri sve prijave, kad su već na površini (opozivi), pa
// opozvano računalo odmah ispada.

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"slices"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// KlijentSignala je prijava kod sastajališta na adresi, s ključem čvora
// cvor iz članstva
func (s *Service) KlijentSignala(adresa, cvor string, javi func(string, ...any)) *razmjena.KlijentSignala {
	program, _ := s.Program()
	return &razmjena.KlijentSignala{Priv: s.node.key, Protokol: s.protokol(), Vj: s.vjerodajnice, Adresa: adresa,
		Kljuc:   func(ctx context.Context) (ed25519.PublicKey, error) { return s.kljucSastajalista(ctx, cvor) },
		Tunel:   &s.tunelKlijent,
		Program: program, Javi: javi}
}

// kljucSastajalista je ključ čvora sastajališta iz članstva. Kad čvor nije
// poznat član, to je ErrNepoznatCvorSastajalista (tek upareno računalo ga
// dobije prvom razmjenom); greška baze ostaje prolazna.
func (s *Service) kljucSastajalista(ctx context.Context, cvor string) (ed25519.PublicKey, error) {
	k, err := s.kljucClana(ctx, cvor)
	if err != nil && ctx.Err() != nil {
		return nil, ctx.Err() // prekinut pokušaj nije nepoznat čvor
	}
	if errors.Is(err, errNijeClan) {
		return nil, fmt.Errorf("%w: %s", razmjena.ErrNepoznatCvorSastajalista, cvor)
	}
	return k, err
}

// Sastajaliste je sastajalište ovog čvora; od tada promjene članstva
// ponovno provjeravaju prijave
func (s *Service) Sastajaliste(javi func(string, ...any)) (*razmjena.Sastajaliste, error) {
	r, err := razmjena.NovoSastajaliste(razmjena.PostavkeSastajalista{Priv: s.node.key, Protokol: s.protokol(),
		Vjeruje: s.trusted, Ime: s.imeIliKljuc, Javi: javi, Sat: time.Now})
	if err != nil {
		return nil, err
	}
	provjeri := r.ProvjeriSve
	s.promjenaClanstva.Store(&provjeri)
	return r, nil
}

// imeIliKljuc je oznaka računala za dnevnik sastajališta, a za nepoznat
// ključ početak ključa
func (s *Service) imeIliKljuc(kljuc ed25519.PublicKey) string {
	if ime := s.imeKljuca(kljuc); ime != "" {
		return ime
	}
	return razmjena.PublicKeyString(kljuc)[:8] + "…"
}

// clanstvoIOvlasti su entiteti čija primjena mijenja tko je član
var clanstvoIOvlasti = []string{EntityMemberships, EntityOvlasti, EntityOpozivi}

// nakonClanstva javi promjenu kad su među primljenim verzijama članstva,
// ovlasti ili opozivi
func (s *Service) nakonClanstva(verzije []ledger.Version) {
	if slices.ContainsFunc(verzije, func(v ledger.Version) bool { return slices.Contains(clanstvoIOvlasti, v.Entity) }) {
		s.javiPromjenuClanstva()
	}
}

// javiPromjenuClanstva zove one koji prate članstvo: sastajalište i
// sesije WEB synca (priprema_weba.go)
func (s *Service) javiPromjenuClanstva() {
	if f := s.promjenaClanstva.Load(); f != nil {
		(*f)()
	}
	s.provjeriWebSync()
}
