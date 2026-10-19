package peers

// Koje članstvo vrijedi za oznaku računala.
//
// Red u tablici članstava kaže samo što je zadnje stiglo pod tom oznakom:
// prepisuje ga svaka primljena verzija, i krivotvorena, a briše ga svaka
// arhivirana verzija, koju može poslati bilo koji član. Potpis se pri
// primjeni ne provjerava (apply_mreza.go). Zato se ključ računala ne uzima
// iz tog reda, nego iz svih članstava koja ovaj čvor zna (red u tablici i
// sve verzije u knjizi, i arhivirane), i to samo iz onih koja vrijede:
// potpis do ključa mreže, rok i opoziv (provjeriClanstvo). Članstvo tako
// ukida samo potpisan opoziv.
//
// Kad za istu oznaku vrijede dva članstva s različitim ključem, vrijedi
// ključ iz prvog izdanog, a sukob ide u dnevnik. Takvo članstvo može
// potpisati samo ključ mreže ili ovlašteni primatelj, npr. primatelj koji
// za računalo tog imena još ne zna. Novi ključ istog računala zato vrijedi
// tek kad staro članstvo više ne vrijedi: uz opoziv (zaborav, opoziv i
// ponovno primanje, kao i dosad) ili istek. Primatelj s izmijenjenim
// programom može datum izdavanja pomaknuti unatrag do početka svoje
// ovlasti; protiv toga pomaže samo opoziv ovlasti.

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"log"
	"slices"

	"gocop/internal/razmjena"
)

// errNijeClan: računalo nema valjano članstvo u našoj mreži (ili ovaj čvor
// nije ni u jednoj mreži)
var errNijeClan = errors.New("nije član naše mreže")

// poznataClanstva su sva članstva koja ovaj čvor zna, najranije izdana
// prva, i ključ mreže kojim se provjeravaju
type poznataClanstva struct {
	s     *Service
	mreza ed25519.PublicKey
	sva   []razmjena.Membership
}

// ucitajClanstva čita sva članstva koja ovaj čvor zna: redove tablice i
// članstva iz svih verzija u knjizi. Kad ovaj čvor nije ni u jednoj mreži,
// errNijeClan.
func (s *Service) ucitajClanstva(ctx context.Context) (*poznataClanstva, error) {
	s.mu.Lock()
	network := s.network
	s.mu.Unlock()
	if network == nil {
		return nil, errNijeClan
	}
	sva, err := s.clanstvaUKnjizi(ctx)
	if err != nil {
		return nil, err
	}
	uTablici, err := s.svaClanstva(ctx)
	if err != nil {
		return nil, err
	}
	// Izdana u istoj sekundi idu redom kojim su upisana u knjigu (oznaka
	// verzije je ista na svim čvorovima), a red tablice iza njih: on je ono
	// što je zadnje stiglo.
	sva = append(sva, uTablici...)
	slices.SortStableFunc(sva, func(a, b razmjena.Membership) int { return a.IssuedAt.Compare(b.IssuedAt) })
	return &poznataClanstva{s: s, mreza: network.Public, sva: sva}, nil
}

// clanstvaUKnjizi su članstva iz svih verzija u knjizi, redom nastanka.
// Verzija koja se ne da pročitati kao članstvo se preskače: poslati je
// može bilo koji član.
func (s *Service) clanstvaUKnjizi(ctx context.Context) ([]razmjena.Membership, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM record_versions WHERE entity = ? ORDER BY version_id`, EntityMemberships)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []razmjena.Membership
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var m razmjena.Membership
		if json.Unmarshal([]byte(payload), &m) == nil {
			out = append(out, m)
		}
	}
	return out, rows.Err()
}

// vrijedi: potpis do ključa mreže, rok i opoziv
func (p *poznataClanstva) vrijedi(ctx context.Context, m razmjena.Membership) bool {
	pub, err := razmjena.ParsePublicKey(m.DeviceKey)
	return err == nil && p.s.provjeriClanstvo(ctx, p.mreza, m, pub) == nil
}

// izabrano je članstvo koje vrijedi za oznaku id: od valjanih članstava s
// ključem iz prvog izdanog ono zadnje izdano (njegov opoziv ključem mreže
// ukida i starija istog ključa), ili nil kad nijedno ne vrijedi
func (p *poznataClanstva) izabrano(ctx context.Context, id string) *razmjena.Membership {
	var izabrano *razmjena.Membership
	for i := range p.sva {
		m := &p.sva[i]
		switch {
		case m.DeviceID != id || !p.vrijedi(ctx, *m):
		case izabrano == nil || m.DeviceKey == izabrano.DeviceKey:
			izabrano = m
		default:
			p.s.javiSukob(*izabrano, *m)
		}
	}
	return izabrano
}

// oznakaKljuca je oznaka računala kojem je pub ključ iz izabranog
// članstva, ili "" kad takvog nema. Isti ključ može stajati u članstvima
// više oznaka (preimenovano računalo zadrži ključ, a članstvo putuje bez
// provjere), pa se gleda od najnovije izdanog.
func (p *poznataClanstva) oznakaKljuca(ctx context.Context, pub ed25519.PublicKey) string {
	k := razmjena.PublicKeyString(pub)
	provjereno := map[string]bool{}
	for _, m := range slices.Backward(p.sva) {
		if m.DeviceKey != k || provjereno[m.DeviceID] {
			continue
		}
		provjereno[m.DeviceID] = true
		if iz := p.izabrano(ctx, m.DeviceID); iz != nil && iz.DeviceKey == k {
			return m.DeviceID
		}
	}
	return ""
}

// zaOpoziv su sva valjana članstva oznake id, svako jednom: opoziv imena
// ukida i drugo računalo koje ima valjano članstvo pod istim imenom, jer bi
// ono inače poslije zauzelo ime
func (p *poznataClanstva) zaOpoziv(ctx context.Context, id string) []razmjena.Membership {
	vidjeno := map[string]bool{}
	var out []razmjena.Membership
	for _, m := range p.sva {
		if m.DeviceID != id || vidjeno[m.Signature] || !p.vrijedi(ctx, m) {
			continue
		}
		vidjeno[m.Signature] = true
		out = append(out, m)
	}
	return out
}

// izabranoClanstvo je članstvo koje vrijedi za računalo nodeID (izabrano).
// errNijeClan kad takvog nema ili ovaj čvor nije ni u jednoj mreži; greška
// čitanja baze nije ta presuda.
func (s *Service) izabranoClanstvo(ctx context.Context, nodeID string) (*razmjena.Membership, error) {
	p, err := s.ucitajClanstva(ctx)
	if err != nil {
		return nil, err
	}
	if m := p.izabrano(ctx, nodeID); m != nil {
		return m, nil
	}
	return nil, errNijeClan
}

// kljucClana je ključ koji računalo nodeID mora dokazati u razmjeni: ključ
// iz članstva koje za njega vrijedi (izabranoClanstvo). Zapis čvora
// (peers) za to ne vrijedi, jer ga piše i šalje bilo koji član. Istu
// funkciju trebaju i ključevi za WEB sync (plan WEB synca, F2b i
// F3b).
func (s *Service) kljucClana(ctx context.Context, nodeID string) (ed25519.PublicKey, error) {
	m, err := s.izabranoClanstvo(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	pub, _ := razmjena.ParsePublicKey(m.DeviceKey) // izabrano vrijedi, pa se ključ da pročitati
	return pub, nil
}

// oznakaDokazanog je oznaka računala koje je u razmjeni dokazalo ključ pub:
// onog kojem je pub ključ iz izabranog članstva, a kad takvog članstva
// ovdje nema, oznaka iz potvrde koju je pokazalo u certifikatu
// (clanPremaVjerodajnicama). "" kad nije član ili ovaj čvor nije ni u
// jednoj mreži. Po njoj vrata razmjene puštaju (trusted), a razmjena se
// bilježi tom računalu (peerByKey).
func (s *Service) oznakaDokazanog(ctx context.Context, pub ed25519.PublicKey, vj []byte) (string, error) {
	p, err := s.ucitajClanstva(ctx)
	if errors.Is(err, errNijeClan) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if id := p.oznakaKljuca(ctx, pub); id != "" {
		return id, nil
	}
	return s.clanPremaVjerodajnicama(ctx, p.mreza, pub, vj), nil
}

// clanPremaVjerodajnicama je oznaka iz potvrde koju je računalo s ključem
// pub pokazalo u certifikatu razmjene, kad potvrda vrijedi za našu mrežu i
// ime nije tuđe; inače "". Tako se prima i član čija potvrda još nije
// stigla knjigom (primio ga je ovlašteni primatelj).
func (s *Service) clanPremaVjerodajnicama(ctx context.Context, mreza, pub ed25519.PublicKey, vj []byte) string {
	var v vjerodajniceCvora
	if json.Unmarshal(vj, &v) != nil || v.Clanstvo == nil {
		return ""
	}
	if s.provjeriClanstvo(ctx, mreza, *v.Clanstvo, pub) != nil || s.provjeriImeDrugog(ctx, v.Clanstvo.DeviceID, pub) != nil {
		return ""
	}
	return v.Clanstvo.DeviceID
}

// javiSukob zapisuje u dnevnik, jednom po imenu i ključu, kad pod istim
// imenom vrijede članstva dva računala; razmjena ide s ključem iz prvog
// izdanog (izabrano). Opoziv imena ukida oba (zaOpoziv), pa savjet kaže da
// se ono koje zadržava ime primi ponovno.
func (s *Service) javiSukob(prvo, drugo razmjena.Membership) {
	if _, vec := s.javljenSukob.LoadOrStore(drugo.DeviceID+"\x00"+drugo.DeviceKey, true); !vec {
		log.Printf("razmjena: pod imenom %s valjano članstvo imaju dva računala: ključ %.8s… (primio %s) i ključ %.8s… (primio %s); razmjena ide s onim čije je članstvo prvo izdano. Opoziv tog imena ukida članstvo obaju računala, pa nakon opoziva ponovno primite računalo koje zadržava ime, a drugo pod drugim imenom",
			prvo.DeviceID, prvo.DeviceKey, prvo.IssuedBy, drugo.DeviceKey, drugo.IssuedBy)
	}
}
