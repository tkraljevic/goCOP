package peers

// Koja ovlast za primanje vrijedi i s kojim trenutkom se izdaje nova
// (Odluke (30. 10. 2026.)).
//
// Prikaz, obnova i vlastita ovlast čitaju ovlasti kao opoziv
// (ovlastiCvora): iz tablice i iz svih verzija u knjizi, samo one koje je
// potpisao ključ mreže i koje nisu opozvane. Od njih vrijedi zadnja izdana.
//
// Nova ovlast i novo članstvo nose trenutak iza zadnjeg poznatog opoziva
// iste vrste i istog ključa, jer opoziv vrijedi za svaku potvrdu izdanu do
// svog trenutka (opozvano), i kad sat ovog računala kasni.

import (
	"cmp"
	"context"
	"crypto/ed25519"
	"slices"
	"time"

	"gocop/internal/razmjena"
)

// zadnjaOvlast je od zadanih ovlasti zadnja izdana za ključ kljuc, ili nil
func zadnjaOvlast(ovlasti []razmjena.Ovlast, kljuc string) *razmjena.Ovlast {
	var zadnja *razmjena.Ovlast
	for i := range ovlasti {
		o := &ovlasti[i]
		if o.DeviceKey == kljuc && (zadnja == nil || kasnijaOvlast(*o, *zadnja)) {
			zadnja = o
		}
	}
	return zadnja
}

// kasnijaOvlast: a je izdana kasnije od b, ili u istom trenutku s kasnijim
// rokom
func kasnijaOvlast(a, b razmjena.Ovlast) bool {
	if c := a.IssuedAt.Compare(b.IssuedAt); c != 0 {
		return c > 0
	}
	return a.ExpiresAt.After(b.ExpiresAt)
}

// boljaOvlast: a je za prikaz bolja od b u trenutku sada. Prednost ima
// ovlast koja je sada u roku, kao kod vlastite ovlasti (mojaOvlast), a
// među jednakima kasnije izdana (kasnijaOvlast).
func boljaOvlast(a, b razmjena.Ovlast, sada time.Time) bool {
	if ua, ub := uRoku(a, sada), uRoku(b, sada); ua != ub {
		return ua
	}
	return kasnijaOvlast(a, b)
}

// zadnjeOvlasti su za svako računalo, od ovlasti koje vrijede
// (vazeceOvlasti), zadnja izdana koja je sada u roku, a kad takve nema,
// zadnja izdana (boljaOvlast); poredane po imenu računala
func (s *Service) zadnjeOvlasti(ctx context.Context) ([]razmjena.Ovlast, error) {
	sve, err := s.vazeceOvlasti(ctx, func(razmjena.Ovlast) bool { return true })
	if err != nil {
		return nil, err
	}
	sada := time.Now()
	po := map[string]razmjena.Ovlast{}
	for _, o := range sve {
		if z, ima := po[o.DeviceID]; !ima || boljaOvlast(o, z, sada) {
			po[o.DeviceID] = o
		}
	}
	out := make([]razmjena.Ovlast, 0, len(po))
	for _, o := range po {
		out = append(out, o)
	}
	slices.SortFunc(out, func(a, b razmjena.Ovlast) int { return cmp.Compare(a.DeviceID, b.DeviceID) })
	return out, nil
}

// uRoku: ovlast vrijedi u trenutku t (izdana do t, a rok nije prošao)
func uRoku(o razmjena.Ovlast, t time.Time) bool {
	return !t.Before(o.IssuedAt) && (o.ExpiresAt.IsZero() || !t.After(o.ExpiresAt))
}

// problemRoka je razlog zbog kojeg ovlast u trenutku t ne vrijedi po roku,
// ili "" kad vrijedi
func problemRoka(o razmjena.Ovlast, t time.Time) string {
	switch {
	case t.Before(o.IssuedAt):
		return "ovlast vrijedi tek od " + datumHR(o.IssuedAt)
	case !uRoku(o, t):
		return "ovlast je istekla " + datumHR(o.ExpiresAt)
	}
	return ""
}

// novaOvlast potpisuje ključem mreže ovlast za primanje računala id s
// ključem pub, s trenutkom iza zadnjeg poznatog opoziva tog ključa
// (nakonOpoziva)
func (s *Service) novaOvlast(ctx context.Context, network razmjena.NetworkKey, id string, pub ed25519.PublicKey) (razmjena.Ovlast, error) {
	od, err := s.nakonOpoziva(ctx, OpozivOvlasti, razmjena.PublicKeyString(pub), time.Now(), network.Public)
	if err != nil {
		return razmjena.Ovlast{}, err
	}
	return network.OvlastiOd(id, pub, s.node.ID, od, OvlastValidity)
}

// nakonOpoziva je trenutak izdavanja nove potvrde te vrste za ključ kljuc:
// sada, ili sekunda iza najkasnijeg opoziva te vrste i ključa koji je
// potpisao jedan od ovlastenih, kad taj nije prije sada. Opoziv s drugim
// potpisom potvrdu ne opoziva, pa ni ne pomiče trenutak.
func (s *Service) nakonOpoziva(ctx context.Context, vrsta, kljuc string, sada time.Time, ovlasteni ...ed25519.PublicKey) (time.Time, error) {
	opozivi, err := s.opoziviKljuca(ctx, vrsta, kljuc)
	if err != nil {
		return time.Time{}, err
	}
	od := sada.UTC().Truncate(time.Second)
	for _, op := range opozivi {
		if !op.IssuedAt.Before(od) && op.Potpisao(ovlasteni...) {
			od = op.IssuedAt.UTC().Truncate(time.Second).Add(time.Second)
		}
	}
	return od, nil
}
