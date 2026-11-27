package peers

// Provjera potpisa autora pri primanju (Odluke (26. 11. 2026.), 3, 4, 6, 7
// i 12). Primljena verzija s potpisom ulazi u knjigu samo kad potpis
// vrijedi pod ključem iz same verzije i kad je taj ključ ikad imao članstvo
// s valjanim potpisom pod imenom autora (potpisanaImena, i isteklo i
// opozvano). Dva dijela provjere ne miješaju se:
//
//   - Kriv potpis (ne vrijedi pod ključem uz verziju) zna se odmah i vrijedi
//     zauvijek: verzija ne ulazi u knjigu, zapiše se u dnevnik i u zapis
//     odbijenog, a pošiljatelju se javlja kroz granicu odbijenih.
//   - Ključ koji ovdje još nije vezan uz ime ovisi o tome je li članstvo
//     već stiglo. Takva verzija čeka: ne ulazi u knjigu i ne javlja se kao
//     odbijena. Kad u istom primanju prođe novija verzija istog autora u
//     istom kanalu, granica knjige prijeđe preko nje, pa sama više ne
//     dolazi. Tada ide u trazi zapisa rupa (rupe.go, zaCekanje), pa je
//     razmjene traže popunom, a za ključ u kojem čeka granica odbijenih
//     ne javlja ništa iznad nje (bezCekanja). U odbijeno ne ide nikad:
//     članstvo može stići i kasnije. Verzija pod imenom ovog računala čeka
//     isto, ali rupu nema.
//
// Nepotpisana verzija prolazi kao i prije, dok traje prijelaz. Iznimka su
// prava i mreža (strogiEntiteti): kad je pošiljatelj javio oznaku
// potpis-verzije/1, nepotpisana verzija tuđeg autora koji već potpisuje
// također čeka. Takav pošiljatelj potpis ne skida, pa je takvu verziju
// autor napisao prije nego što je počeo potpisivati, ili ju je potpisao, a
// potpis je skinulo starije računalo na putu. Primatelj to ne može
// razlikovati (rane potpisane verzije autora možda nema, a oznake idu po
// satu autora), pa i ona, kad je granica prijeđe, ide u trazi. Popuna
// prolazi istu provjeru kao delta (ista poruka), pa je pošiljatelj s
// oznakom koji je drži bez potpisa daje opet nepotpisanu, i ona opet čeka.
// Verzija napisana prije potpisivanja zato se traži zauvijek, a svaka
// razmjena s računalom koje za nju jamči popunom je šalje opet.
// U knjigu ne ide, jer bi je obnova površine pri pokretanju ionako
// stavila na površinu. Računalo bez oznake potpis ne prenosi, pa od njega
// strogo ne vrijedi: kad tu verziju pošalje, deltom ili popunom, prolazi
// bez potpisa kao i ostale dok traje prijelaz. Isto vrijedi za verziju
// potpisanu ključem koji nije vezan, jer je takvo računalo šalje bez
// potpisa.

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"slices"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

var (
	// errKrivPotpis: potpis verzije ne vrijedi pod ključem koji stoji uz nju
	errKrivPotpis = errors.New("ima potpis koji ne vrijedi")
	// errKljucNijeVezan: ključ kojim je verzija potpisana ovdje još nije
	// imao članstvo pod imenom autora
	errKljucNijeVezan = errors.New("potpisana je ključem koji ovdje još nije vezan uz ime autora")
	// errNepotpisanaOdPotpisnika: verzija prava ili mreže bez potpisa, od
	// autora koji već potpisuje, a stigla je od računala koje potpis prenosi
	errNepotpisanaOdPotpisnika = errors.New("nema potpisa, a autor ovakve zapise potpisuje")
)

// ceka: verzija nije kriva, nego čeka članstvo ili potpisanu inačicu
func ceka(zasto error) bool {
	return errors.Is(zasto, errKljucNijeVezan) || errors.Is(zasto, errNepotpisanaOdPotpisnika)
}

// PostaviStrogePotpise zadaje entitete prava i mreže čija nepotpisana
// verzija tuđeg autora koji već potpisuje ne ulazi u knjigu, kad ju je
// poslalo računalo koje potpis prenosi (Odluke (26. 11. 2026.), 7)
func (s *Service) PostaviStrogePotpise(entiteti ...string) {
	m := make(map[string]bool, len(entiteti))
	for _, e := range entiteti {
		m[e] = true
	}
	s.strogiEntiteti = m
}

// provjeraPotpisa provjerava potpise jedne poruke ili jednog uvoza.
// Članstva čita jednom, i to tek kad zatrebaju, a uz njih uzme i članstva
// iz iste poruke, da verzija ne čeka članstvo koje je stiglo s njom.
type provjeraPotpisa struct {
	s       *Service
	ctx     context.Context
	verzije []ledger.Version
	// strogo: pošiljatelj je javio potpis-verzije/1 (errNepotpisanaOdPotpisnika)
	strogo    bool
	clanstva  *poznataClanstva
	vezano    map[string]bool
	potpisuje map[string]bool
}

func (s *Service) novaProvjeraPotpisa(ctx context.Context, verzije []ledger.Version, strogo bool) *provjeraPotpisa {
	return &provjeraPotpisa{s: s, ctx: ctx, verzije: verzije, strogo: strogo, vezano: map[string]bool{}, potpisuje: map[string]bool{}}
}

// provjeri je razlog zbog kojeg verzija ne ulazi u knjigu, ili nil
func (p *provjeraPotpisa) provjeri(v ledger.Version) error {
	if v.Potpis == "" && v.KljucAutora == "" {
		return p.nepotpisana(v)
	}
	if err := razmjena.ProvjeriPotpisVerzije(p.s.protokol(), PodaciVerzije(v), v.Potpis, v.KljucAutora); err != nil {
		return fmt.Errorf("%w (%v)", errKrivPotpis, err)
	}
	if !p.vezan(v.NodeID, v.KljucAutora) {
		return errKljucNijeVezan
	}
	return nil
}

// nepotpisana: prolazi, osim verzije prava ili mreže tuđeg autora koji
// već potpisuje, kad ju je poslalo računalo koje potpis prenosi
func (p *provjeraPotpisa) nepotpisana(v ledger.Version) error {
	if !p.strogo || !p.s.strogiEntiteti[v.Entity] || v.NodeID == p.s.node.ID || !p.autorPotpisuje(v.NodeID) {
		return nil
	}
	return errNepotpisanaOdPotpisnika
}

// vezan: ključ je ikad imao članstvo s valjanim potpisom pod imenom autora,
// ili je to ključ ovog računala pod njegovim imenom
func (p *provjeraPotpisa) vezan(autor, kljuc string) bool {
	if autor == p.s.node.ID && kljuc == p.s.node.PublicKey() {
		return true
	}
	k := autor + "\x00" + kljuc
	if v, ima := p.vezano[k]; ima {
		return v
	}
	v := false
	for _, m := range p.poznata().potpisanaImena(autor) {
		if m.DeviceKey == kljuc {
			v = true
			break
		}
	}
	p.vezano[k] = v
	return v
}

// poznata su članstva iz knjige i tablice i ona iz iste poruke. Bez mreže
// ili kad se ne daju pročitati nema nijednog, pa potpisana verzija tuđeg
// autora čeka.
func (p *provjeraPotpisa) poznata() *poznataClanstva {
	if p.clanstva != nil {
		return p.clanstva
	}
	c, err := p.s.ucitajClanstva(p.ctx)
	switch {
	case errors.Is(err, errNijeClan):
		c = &poznataClanstva{s: p.s}
	case err != nil:
		log.Printf("razmjena: članstva za provjeru potpisa ne daju se pročitati, potpisane verzije čekaju: %v", err)
		c = &poznataClanstva{s: p.s}
	}
	c.sva = append(c.sva, clanstvaIzPoruke(p.verzije)...)
	p.clanstva = c
	return c
}

// clanstvaIzPoruke su članstva iz živih verzija poruke. Potpis im se ovdje
// ne gleda: potpisanaImena uzima samo ona koja vrijede do ključa mreže.
func clanstvaIzPoruke(verzije []ledger.Version) []razmjena.Membership {
	var out []razmjena.Membership
	for _, v := range verzije {
		var m razmjena.Membership
		if v.Entity == EntityMemberships && !v.Archived && json.Unmarshal(v.Payload, &m) == nil {
			out = append(out, m)
		}
	}
	return out
}

// autorPotpisuje: u knjizi je barem jedna potpisana verzija tog autora.
// Svaki potpisan redak u knjizi je provjeren, pa je ključ vezan uz ime.
func (p *provjeraPotpisa) autorPotpisuje(autor string) bool {
	if v, ima := p.potpisuje[autor]; ima {
		return v
	}
	var jedan int
	err := p.s.db.QueryRowContext(p.ctx, `SELECT 1 FROM record_versions WHERE node_id = ? AND potpis != '' LIMIT 1`, autor).Scan(&jedan)
	v := err == nil
	p.potpisuje[autor] = v
	return v
}

// dopuniPotpise upiše potpis u redak koji je u knjizi bez potpisa, kad je
// ista verzija sad stigla potpisana i potpis vrijedi nad spremljenim
// retkom (Odluke (26. 11. 2026.), 6). Zove se pod bravom upisa, nakon
// Apply, s verzijama koje su prošle provjeru. Greška ide samo u dnevnik:
// potpisana inačica stići će ponovno.
func (s *Service) dopuniPotpise(ctx context.Context, verzije []ledger.Version) {
	if err := s.dopuni(ctx, potpisaneIz(verzije)); err != nil {
		log.Printf("razmjena: dopuna potpisa nije uspjela: %v", err)
	}
}

// dopuni upiše potpise primljenih potpisanih verzija u retke bez potpisa
// nad kojima vrijede
func (s *Service) dopuni(ctx context.Context, potpisane map[string]ledger.Version) error {
	if len(potpisane) == 0 {
		return nil
	}
	spremljene, err := s.rec.NepotpisaneMedju(ctx, slices.Collect(maps.Keys(potpisane)))
	if err != nil {
		return err
	}
	dopuna := s.zaDopunu(spremljene, potpisane)
	if len(dopuna) == 0 {
		return nil
	}
	_, err = s.rec.DopuniPotpise(ctx, dopuna)
	return err
}

// potpisaneIz su potpisane verzije po oznaci
func potpisaneIz(verzije []ledger.Version) map[string]ledger.Version {
	out := map[string]ledger.Version{}
	for _, v := range verzije {
		if v.Potpis != "" {
			out[v.VersionID] = v
		}
	}
	return out
}

// zaDopunu su spremljeni retci bez potpisa s potpisom primljene inačice,
// samo oni nad kojima taj potpis vrijedi
func (s *Service) zaDopunu(spremljene []ledger.Version, potpisane map[string]ledger.Version) []ledger.Version {
	var out []ledger.Version
	for _, sp := range spremljene {
		v := potpisane[sp.VersionID]
		if razmjena.ProvjeriPotpisVerzije(s.protokol(), PodaciVerzije(sp), v.Potpis, v.KljucAutora) == nil {
			sp.Potpis, sp.KljucAutora = v.Potpis, v.KljucAutora
			out = append(out, sp)
		}
	}
	return out
}

// zabiljeziCekaju zapiše u dnevnik verzije koje čekaju članstvo ili
// potpisanu inačicu, jednim retkom po autoru, kao verzije koje kasne.
// Koje od njih idu u trazi zapisa rupa, pa se traže od drugih računala,
// kaže zaCekanje.
func (s *Service) zabiljeziCekaju(put string, cekaju kasneVerzije) {
	for autor, z := range cekaju {
		log.Printf("%s: primljena verzija %.80q autora %.80q %v i ne upisuje se; takvih u poruci %d, a upisat će se kad stigne članstvo autora ili potpisana inačica",
			put, z.prva.VersionID, autor, z.zasto, z.broj)
	}
}

// kljucDruge je ključ druge strane razgovora, za zapis odbijenog; nil kad
// razgovor nema vezu
func (r *razgovorSCvorom) kljucDruge() ed25519.PublicKey {
	if r.c == nil {
		return nil
	}
	return r.c.PeerKey
}
