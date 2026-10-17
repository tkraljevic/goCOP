package peers

// Rupe uz izvoz i uvoz (rupe.go). Arhiva knjige i COP paket su posrednik
// bez mreže: izvoznik uz verzije zapiše za koji dio niza ne jamči (archive_meta
// 'rupe', CopManifest.Rupe), a uvoznik to, iznad svoje granice, zapiše kao
// trazi, pa ostatak zatraži razmjenom. Uvoz nikad ne smanjuje trazi: stariji
// paket ne razlikuje „nema rupa” od „ne zna”.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"gocop/internal/ledger"
)

// zaUvoz su verzije paketa koje ovaj čvor uzima: kanal koji prati i ono
// što prihvaća (Accept), kao razmjenom. Odbijeno se vraća zasebno, za
// zapis rupa; što otpadne na pretplatama, ne.
func (s *Service) zaUvoz(versions []ledger.Version, filter func(string) bool) ([]ledger.Version, map[string]baceno) {
	var wanted []ledger.Version
	var bacio map[string]baceno
	for _, v := range versions {
		switch {
		case filter != nil && !filter(v.Channel):
		case s.accept != nil && !s.accept(v):
			bacio = uzBaceno(bacio, ledger.FrontierKey(v.NodeID, v.Channel), v.VersionID)
		default:
			wanted = append(wanted, v)
		}
	}
	return wanted, bacio
}

// promjenaUvoza je promjena rupa uz uvoz: po tuđem ključu odbijeno (kao u
// razgovoru) i rupe izvoznika iznad granice knjige ovog čvora (nesigurno).
// Pokrivenoga nema, pa uvoz trazi ne smanjuje. Kad se granica ne da
// pročitati, uzima se prazna, što je na sigurnu stranu.
func (s *Service) promjenaUvoza(ctx context.Context, wanted []ledger.Version, bacio map[string]baceno, rupe map[string]raspon) promjenaRupa {
	moja, err := s.rec.Frontier(ctx)
	if err != nil {
		moja = nil
	}
	ispod := ispodOdbijenih(wanted, bacio)
	p := promjenaRupa{kljucevi: map[string]promjenaKljuca{}}
	for _, k := range skupKljuceva(maps.Keys(pomakniGranicu(nil, wanted)), maps.Keys(bacio)) {
		if autor, _ := ledger.SplitFrontierKey(k); autor != s.node.ID {
			p.kljucevi[k] = promjenaKljuca{fs: moja[k], nesigurno: rupe[k], odbijeno: rasponOdbijenog(bacio[k], moja[k], ispod[k])}
		}
	}
	return p
}

// uveziSRupama upiše uvezene verzije kao razmjena: najprije pod bravom
// rupe (prosiri), pa verzije u serijama od 2000. Kad se rupe ne daju
// zapisati, ne upisuje ništa.
func (s *Service) uveziSRupama(ctx context.Context, wanted []ledger.Version, p promjenaRupa) (int, error) {
	if err := s.rupePodBravom(ctx, p, promjenaKljuca.prosiri); err != nil {
		return 0, err
	}
	ukupno := 0
	for start := 0; start < len(wanted); start += 2000 {
		applied, err := s.primijeniPodBravom(ctx, wanted[start:min(start+2000, len(wanted))])
		ukupno += applied
		if err != nil {
			return ukupno, err
		}
	}
	return ukupno, nil
}

// rupePodBravom promijeni zapis rupa pod bravom upisa, koju čeka koliko treba
func (s *Service) rupePodBravom(ctx context.Context, p promjenaRupa, korak korakRupa) error {
	if err := s.uzmiBravu(ctx); err != nil {
		return err
	}
	defer s.pustiBravu()
	return s.promijeniRupe(ctx, p, korak)
}

// rupeIzArhive su rupe izvoznika iz arhive knjige (archive_meta 'rupe').
// Arhiva bez njih (stariji program) ne zna za rupe, pa joj se vjeruje kao
// prije; pokvaren zapis znači da izvoznik ne jamči ni za što.
func rupeIzArhive(ctx context.Context, in *sql.DB, versions []ledger.Version) map[string]raspon {
	var sirovo string
	if in.QueryRowContext(ctx, `SELECT value FROM archive_meta WHERE key = 'rupe'`).Scan(&sirovo) != nil {
		return nil
	}
	var rupe map[string]raspon
	if json.Unmarshal([]byte(sirovo), &rupe) != nil {
		return sveNesigurno(versions)
	}
	return ocistiRupe(rupe, versions)
}

// rupeIzPaketa su rupe izdavača COP paketa; paket inačice 3 ih nema.
// Potpis ih pokriva (kanonskiCop), pa ih nitko usput ne može maknuti.
func rupeIzPaketa(m CopManifest, versions []ledger.Version) map[string]raspon {
	return ocistiRupe(m.Rupe, versions)
}

// ocistiRupe su rupe izvoznika za ključeve koji su u uvozu, najviše do
// najveće uvezene verzije ključa; neispravan raspon znači da za ključ ne
// jamči ni za što
func ocistiRupe(rupe map[string]raspon, verzije []ledger.Version) map[string]raspon {
	out := map[string]raspon{}
	for k, vrh := range pomakniGranicu(nil, verzije) {
		r, ok := rupe[k]
		switch {
		case !ok:
		case r.ispravan():
			out[k] = r.presjek(raspon{Do: vrh})
		default:
			out[k] = raspon{Do: vrh}
		}
	}
	return out
}

// sveNesigurno su rupe kad se zapis izvoznika ne da pročitati: ni za jedan
// ključ ne jamči ništa do najveće uvezene verzije
func sveNesigurno(verzije []ledger.Version) map[string]raspon {
	out := map[string]raspon{}
	for k, vrh := range pomakniGranicu(nil, verzije) {
		out[k] = raspon{Do: vrh}
	}
	return out
}

// redRupa je redak rupa u kanonskom zapisu COP paketa (kanonskiCop):
// „rupe”, tabulator i poredani ključ=od,do, svaki pod navodnicima. Bez rupa
// redka nema, pa je potpis paketa inačice 3 isti kao prije.
func redRupa(rupe map[string]raspon) string {
	if len(rupe) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("rupe\t")
	for _, k := range slices.Sorted(maps.Keys(rupe)) {
		fmt.Fprintf(&b, "%q=%q,%q;", k, rupe[k].Od, rupe[k].Do)
	}
	b.WriteByte('\n')
	return b.String()
}

// inacicaCopa je inačica paketa: 4 samo kad nosi rupe, jer ga program koji
// ih ne razumije ne smije uvesti (odbija ga porukom da treba nadogradnja),
// a bez rupa 3, pa ga uveze i 0.0.38
func inacicaCopa(rupe map[string]raspon) int {
	if len(rupe) == 0 {
		return 3
	}
	return CopInacica
}
