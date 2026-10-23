package peers

// Rokovi članstva i ovlasti na pločici razmjene i na stranici Sinkronizacija
// (G9a; Odluke (18. 10. 2026.)): pločica pokazuje samo rokove koji traže
// pažnju (manje od 30 dana, pričuva, isteklo) i ovlasti obnovljene u zadnjih
// 7 dana; puni rokovi su u Postavkama i u stupcu Član.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gocop/internal/models"
)

// danaObnovljeneOvlasti je koliko dana pločica javlja obnovljenu ovlast
const danaObnovljeneOvlasti = 7

// VlastitiRokovi su rok članstva ovog računala i, kad nema ključ mreže, rok
// njegove ovlasti za primanje; nil kad ih nema
func (s *Service) VlastitiRokovi(ctx context.Context) (clanstvo, ovlast *Rok) {
	n := s.NetworkInfo()
	if n == nil {
		return nil, nil
	}
	sada := time.Now()
	m, err := s.izabranoClanstvo(ctx, s.node.ID)
	if err != nil {
		m = s.myMembership(ctx) // isteklo i bez pričuve: rok iz zadnjeg koje je stiglo
	}
	if m != nil {
		r := rokZa(m.ExpiresAt, sada, true)
		clanstvo = &r
	}
	if o := s.ovlastZaObnovu(ctx, s.node.ID, s.node.PublicKey()); o != nil && !n.DrziKljuc {
		r := rokZa(o.ExpiresAt, sada, false)
		ovlast = &r
	}
	return clanstvo, ovlast
}

// upozorenjaRokova su rečenice o rokovima za pločicu: vlastito članstvo i
// ovlast, a na računalu koje prima i članovi kojima rok ističe ili je u
// pričuvi, te obnovljene ovlasti
func (s *Service) upozorenjaRokova(ctx context.Context, clanovi []Member) []string {
	n := s.NetworkInfo()
	if n == nil {
		return nil
	}
	clanstvo, ovlast := s.VlastitiRokovi(ctx)
	var out []string
	for _, t := range []string{tekstVlastitogClanstva(clanstvo), tekstVlastiteOvlasti(ovlast)} {
		if t != "" {
			out = append(out, t)
		}
	}
	if n.CanAdmit {
		out = append(out, s.upozorenjaClanova(ctx, clanovi)...)
	}
	return append(out, s.obnovljeneOvlasti(ctx, n.DrziKljuc)...)
}

// tekstVlastitogClanstva je rečenica o roku članstva ovog računala, ili ""
func tekstVlastitogClanstva(r *Rok) string {
	if r == nil {
		return ""
	}
	switch r.Stanje {
	case RokIstjece:
		d := danaDo(r.Do, time.Now())
		return fmt.Sprintf("Članstvo ovog računala u mreži ističe %s (za %d %s). Obnova se traži sama uz razmjenu; ako ne stigne, kliknite Obnovi članstvo u Postavkama, pod Mreža.",
			datumHR(r.Do), d, models.UzBrojHR(d, "dan", "dana", "dana"))
	case RokPricuva:
		return fmt.Sprintf("Članstvo ovog računala u mreži isteklo je %s Razmjena radi još do %s, a nakon toga računalo treba ponovno primiti u mrežu. Kliknite Obnovi članstvo u Postavkama, pod Mreža. Dok je članstvo isteklo, ovo računalo ne prima druga računala.",
			datumHR(r.Do), datumHR(r.Do.Add(PricuvniRok)))
	case RokIsteklo:
		return fmt.Sprintf("Članstvo ovog računala u mreži isteklo je %s i pričuva je prošla, pa razmjena ne ide. Računalo treba ponovno primiti u mrežu: uparite ga s računalom koje prima ili mu pošaljite zahtjev za primanje na daljinu.", datumHR(r.Do))
	}
	return ""
}

// tekstVlastiteOvlasti je rečenica o roku ovlasti ovog računala, ili ""
func tekstVlastiteOvlasti(r *Rok) string {
	if r == nil {
		return ""
	}
	switch r.Stanje {
	case RokIstjece:
		d := danaDo(r.Do, time.Now())
		return fmt.Sprintf("Ovlast ovog računala za primanje ističe %s (za %d %s). Obnavlja je računalo s ključem mreže kad do njega stigne zahtjev, a zahtjev ide sam uz razmjenu.",
			datumHR(r.Do), d, models.UzBrojHR(d, "dan", "dana", "dana"))
	case RokIsteklo:
		return fmt.Sprintf("Ovlast ovog računala za primanje istekla je %s, pa ovo računalo ne prima i ne obnavlja druga računala. Obnavlja je računalo s ključem mreže kad do njega stigne zahtjev; kliknite Obnovi članstvo u Postavkama, pod Mreža.", datumHR(r.Do))
	}
	return ""
}

// kratkiDatum je dan i mjesec, npr. 3. 11.
func kratkiDatum(t time.Time) string { return t.In(models.Zagreb).Format("2. 1.") }

// upozorenjaClanova: drugi članovi kojima članstvo ističe za manje od 30
// dana ili je u pričuvi, po jedna rečenica za svako stanje
func (s *Service) upozorenjaClanova(ctx context.Context, clanovi []Member) []string {
	istjece, pricuva := s.clanoviPoRoku(ctx, clanovi)
	var out []string
	if len(istjece) > 0 {
		out = append(out, "Članstvo ističe za manje od 30 dana: "+strings.Join(istjece, ", ")+". Obnovit će se kad ta računala razmijene podatke, ili kliknite Obnovi uz računalo u Postavkama, pod Mreža.")
	}
	if len(pricuva) > 0 {
		out = append(out, "Članstvo je isteklo, a razmjena radi još do kraja pričuve: "+strings.Join(pricuva, ", ")+". Kliknite Obnovi uz računalo u Postavkama, pod Mreža.")
	}
	return out
}

// clanoviPoRoku su drugi valjani članovi s rokom koji ističe i oni u
// pričuvi, imenom i danom
func (s *Service) clanoviPoRoku(ctx context.Context, clanovi []Member) (istjece, pricuva []string) {
	for _, m := range clanovi {
		if m.IsSelf || !m.Valid {
			continue
		}
		switch m.Rok.Stanje {
		case RokIstjece:
			istjece = append(istjece, fmt.Sprintf("%s (%s)", s.imeRacunala(ctx, m.DeviceID), kratkiDatum(m.ExpiresAt)))
		case RokPricuva:
			pricuva = append(pricuva, fmt.Sprintf("%s (do %s)", s.imeRacunala(ctx, m.DeviceID), kratkiDatum(m.ExpiresAt.Add(PricuvniRok))))
		}
	}
	return istjece, pricuva
}

// obnovljeneOvlasti: ovlasti za primanje obnovljene u zadnjih 7 dana. Na
// računalu s ključem mreže one koje je ono obnovilo, na drugom njegova
// vlastita. Obnovljena je ovlast koja u knjizi ima raniju verziju.
func (s *Service) obnovljeneOvlasti(ctx context.Context, kljucMreze bool) []string {
	ovlasti, err := s.ListOvlasti(ctx)
	if err != nil {
		return nil
	}
	od := time.Now().AddDate(0, 0, -danaObnovljeneOvlasti)
	var out []string
	for _, o := range ovlasti {
		switch {
		case !s.javljaOvlast(o, od, kljucMreze) || !s.imaRanijuOvlast(ctx, o.DeviceID):
		case o.DeviceID == s.node.ID:
			out = append(out, fmt.Sprintf("Ovlast ovog računala za primanje obnovljena je %s i vrijedi do %s", datumHR(o.IssuedAt), datumHR(o.ExpiresAt)))
		default:
			out = append(out, fmt.Sprintf("Obnovljena je ovlast za primanje: %s (vrijedi do %s).", s.imeRacunala(ctx, o.DeviceID), datumHR(o.ExpiresAt)))
		}
	}
	return out
}

// javljaOvlast: važeća ovlast izdana od od, koju je izdalo ovo računalo
// (s ključem mreže) ili koja je njegova (bez ključa)
func (s *Service) javljaOvlast(o Primatelj, od time.Time, kljucMreze bool) bool {
	if !o.Valid || o.IssuedAt.Before(od) {
		return false
	}
	if kljucMreze {
		return o.IssuedBy == s.node.ID
	}
	return o.DeviceID == s.node.ID
}

// imaRanijuOvlast javlja ima li ovlast računala id u knjizi više od jedne
// verzije, tj. je li obnovljena
func (s *Service) imaRanijuOvlast(ctx context.Context, id string) bool {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM record_versions WHERE entity = ? AND entity_id = ? AND archived = 0`, EntityOvlasti, id).Scan(&n)
	return err == nil && n > 1
}
