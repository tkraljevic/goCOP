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

// odlukeUvoza su odluke accepta pri uvozu: po ključu najmanja i najveća
// odbijena verzija (bacio) i stanje ograde pročitano prije prve odluke, po
// kojem se pod bravom vidi je li ograda u međuvremenu popustila
// (podOgradom). cekaju su verzije koje čekaju članstvo ili potpisanu
// inačicu (zaCekanje).
type odlukeUvoza struct {
	bacio  map[string]baceno
	ograda stanjeOgrade
	cekaju []ledger.Version
}

// zaUvoz su verzije paketa koje ovaj čvor uzima: kanal koji prati i ono
// što prihvaća (Accept), kao razmjenom. Odbijeno se vraća zasebno, za
// zapis rupa; što otpadne na pretplatama, ne.
func (s *Service) zaUvoz(versions []ledger.Version, filter func(string) bool) ([]ledger.Version, odlukeUvoza) {
	var wanted []ledger.Version
	odluke := odlukeUvoza{ograda: s.ogradaSada()}
	for _, v := range versions {
		switch {
		case filter != nil && !filter(v.Channel):
		case s.accept != nil && !s.accept(v):
			odluke.bacio = uzBaceno(odluke.bacio, ledger.FrontierKey(v.NodeID, v.Channel), v.VersionID)
		default:
			wanted = append(wanted, v)
		}
	}
	return wanted, odluke
}

// uvjetCekanja je ono po čemu verzija koja čeka ide u zapis rupa
// (zaCekanje): granica knjige prije upisa i, u razgovoru s oznakom rupe/1,
// popuna koju je druga strana dala za ono što ovaj čvor traži i za nju
// jamči (valjanaPopuna; nil: nema je)
type uvjetCekanja struct {
	granica map[string]string
	popuna  func(k string) raspon
}

// cekanje su, po ključu, najmanja i najveća verzija koja čeka članstvo ili
// potpisanu inačicu i ide u trazi (zaCekanje), najveća verzija ključa u
// knjizi ispod najmanje koja čeka (uKnjizi) i najveća primljena verzija
// ispod nje (ispod, iznad). Od veće od tih dviju počinje traženje
// (rasponCekanja). ostaju su, po ključu i poredano, verzije koje čekaju, a
// zaCekanje ih ne uzima: druga strana ih je dala, a ovo računalo ih nije
// ni upisalo ni odbilo, pa ih jamstvo druge strane ne pokriva (doOstalih).
type cekanje struct {
	trazi          map[string]baceno
	uKnjizi, ispod map[string]string
	ostaju         map[string][]ostaje
}

// ostaje je verzija koja čeka, a ne ide u trazi (id), uz najveću primljenu
// verziju istog ključa ispod nje (ispod; nema je: prazno)
type ostaje struct{ id, ispod string }

// zaCekanje razvrsta verzije koje čekaju (preskociNeispravne) za zapis
// rupa. U trazi ide verzija preko koje granica knjige prelazi, a da je
// poslije nitko ne bi poslao:
//   - tuđeg autora: vlastiti ključ nema rupu;
//   - iznad granice knjige samo kad isto primanje upisuje noviju verziju
//     istog ključa (prolazi). Inače granica ostaje ispod nje, pa je druga
//     strana šalje opet. Tako ni verzija koju je netko izmislio iznad svih
//     granica, u ključu u koji autor više ne piše, ne ostavlja rupu koju
//     nitko ne bi mogao zatvoriti;
//   - ispod granice samo kad stigne u popuni za koju druga strana jamči
//     (popuna). Ispod granice niz je prijeđen i prije, a što se traži, a ne
//     stigne u takvoj popuni, ostaje u trazi kakvo jest;
//   - koje nema u knjizi: popuna šalje i ono što ovaj čvor već ima.
//
// Kanal se ne gleda: ono što prolazi i popuna već su samo iz kanala koje
// ovaj čvor prati, a arhiva knjige upisuje sve kanale.
//
// Verzija koja čeka nikad ne ide u odbijeno, ni potpisana ni nepotpisana.
// Članstvo može stići kasnije (pod drugim ključem, u odrezanoj popuni,
// nakon greške pri čitanju članstava), a ovo računalo nema pouzdan dokaz
// da potpisanu inačicu nema nitko. Cijena je da je računalo koje jamči,
// dok verzija čeka, svakom razmjenom popunom šalje opet, a kad traženje
// ključa ne stane u jednu popunu, ostatak ključa iza nje ne stiže dok ona
// čeka, a za verziju koja čeka zauvijek nikad (invarijanta u rupe.go).
// Pločica dotle javlja da od autora možda nedostaju zapisi
// (NedostajeDulje), a za verziju koja čeka zauvijek trajno.
//
// Poznato ograničenje dok sva računala nemaju ovaj popravak: računalo
// 0.0.70 bez njega, kojem je granica prešla preko verzije koja čeka,
// verziju je izgubilo, a za niz bez nje jamči. Ovaj čvor koji je verziju
// ranije primio samu (bez novije) nije je zapisao, pa kad od takvog
// računala dobije noviju verziju istog ključa, granica prijeđe preko nje
// bez rupe. Kad je verzija već u trazi, popuna takvog računala to traženje
// makne.
//
// Kad se knjiga ne da pročitati, verzija se uzima kao da je u knjizi nema,
// pa ide u trazi, a traženje počinje niže (ispodUKnjizi). Oboje je na
// sigurnu stranu.
func (s *Service) zaCekanje(ctx context.Context, cekaju, prolazi []ledger.Version, u uvjetCekanja) cekanje {
	upis := pomakniGranicu(nil, prolazi)
	var uzete, neuzete []ledger.Version
	for _, v := range cekaju {
		if s.uzmiZaCekanje(v, upis, u) {
			uzete = append(uzete, v)
		} else {
			neuzete = append(neuzete, v)
		}
	}
	ima, err := s.uKnjizi(ctx, uzete)
	if err != nil {
		ima = nil
	}
	var out cekanje
	for _, v := range uzete {
		if !ima[v.VersionID] {
			out.trazi = uzBaceno(out.trazi, ledger.FrontierKey(v.NodeID, v.Channel), v.VersionID)
		}
	}
	out.uKnjizi = s.ispodUKnjizi(ctx, out.trazi)
	out.ostaju = ostajuIspod(neuzete, slices.Concat(cekaju, prolazi))
	return out
}

// ostajuIspod su verzije koje čekaju, a ne idu u trazi (neuzete), po
// ključu i poredano, svaka s najvećom primljenom verzijom (primljeno)
// istog ključa ispod nje
func ostajuIspod(neuzete, primljeno []ledger.Version) map[string][]ostaje {
	if len(neuzete) == 0 {
		return nil
	}
	ids := map[string][]string{}
	for _, v := range primljeno {
		k := ledger.FrontierKey(v.NodeID, v.Channel)
		ids[k] = append(ids[k], v.VersionID)
	}
	for _, l := range ids {
		slices.Sort(l)
	}
	out := map[string][]ostaje{}
	for _, v := range neuzete {
		k := ledger.FrontierKey(v.NodeID, v.Channel)
		o := ostaje{id: v.VersionID}
		if i, _ := slices.BinarySearch(ids[k], v.VersionID); i > 0 {
			o.ispod = ids[k][i-1]
		}
		out[k] = append(out[k], o)
	}
	for _, l := range out {
		slices.SortFunc(l, func(a, b ostaje) int { return strings.Compare(a.id, b.id) })
	}
	return out
}

// uzmiZaCekanje: verzija v koja čeka ide u trazi po pravilima iz
// zaCekanje, osim knjige, uz najveću verziju po ključu koju primanje
// upisuje (upis)
func (s *Service) uzmiZaCekanje(v ledger.Version, upis map[string]string, u uvjetCekanja) bool {
	k := ledger.FrontierKey(v.NodeID, v.Channel)
	switch {
	case v.NodeID == s.node.ID:
		return false
	case v.VersionID > u.granica[k]:
		return v.VersionID < upis[k]
	}
	return u.popuna != nil && u.popuna(k).drziVerziju(v.VersionID)
}

// ispodCekanjaSQL je najveća verzija ključa (autor, kanal) u knjizi ispod
// zadane
const ispodCekanjaSQL = `SELECT coalesce(max(version_id), '') FROM record_versions WHERE node_id = ? AND channel = ? AND version_id < ?`

// ispodUKnjizi je po ključu najveća verzija u knjizi ispod najmanje koja
// čeka (c). Kad se knjiga ne da pročitati, prazno, pa traženje počinje
// niže, što je na sigurnu stranu.
func (s *Service) ispodUKnjizi(ctx context.Context, c map[string]baceno) map[string]string {
	out := make(map[string]string, len(c))
	for k, b := range c {
		autor, kanal := ledger.SplitFrontierKey(k)
		var id string
		if s.db.QueryRowContext(ctx, ispodCekanjaSQL, autor, kanal, b.najmanja).Scan(&id) != nil {
			id = ""
		}
		out[k] = id
	}
	return out
}

// promjenaUvoza je promjena rupa uz uvoz: po tuđem ključu odbijeno (kao u
// razgovoru), verzije koje čekaju (zaCekanje, u trazi) i rupe izvoznika
// iznad granice knjige ovog čvora (nesigurno). Pokrivenoga nema, pa uvoz
// trazi ne smanjuje. Kad se granica ne da pročitati, uzima se prazna, što
// je na sigurnu stranu.
//
// Što isto primanje upisuje, ovdje je ono što je prošlo prvu provjeru
// potpisa (wanted), a ne ono što se stvarno upiše nakon provjere u
// serijama. Verzija koja prođe samo uz članstvo iz istog paketa, a to
// članstvo samo čeka, u seriji počne čekati, a ovdje se broji kao
// upisana. Tada verzija koja čeka ode u trazi, a granica preko nje ne
// prijeđe. To je na sigurnu stranu: novija verzija stigne kad stigne
// članstvo, a razmjena koja je donese makne i to traženje.
func (s *Service) promjenaUvoza(ctx context.Context, wanted []ledger.Version, odluke odlukeUvoza, rupe map[string]raspon) promjenaRupa {
	moja := s.granicaZaRupe(ctx)
	ceka := s.zaCekanje(ctx, odluke.cekaju, wanted, uvjetCekanja{granica: moja}).iznad(wanted)
	ispod := ispodOdbijenih(wanted, odluke.bacio)
	p := promjenaRupa{kljucevi: map[string]promjenaKljuca{}, ograda: odluke.ograda}
	for _, k := range skupKljuceva(maps.Keys(pomakniGranicu(nil, wanted)), maps.Keys(odluke.bacio), maps.Keys(ceka.trazi)) {
		if autor, _ := ledger.SplitFrontierKey(k); autor != s.node.ID {
			pk := promjenaKljuca{fs: moja[k], nesigurno: rupe[k], odbijeno: rasponOdbijenog(odluke.bacio[k], moja[k], ispod[k])}
			p.kljucevi[k] = ceka.uKljuc(k, pk)
		}
	}
	return p
}

// promjenaCekanja je promjena rupa za verzije koje čekaju tek u provjeri
// serije uvoza (primijeniPodBravom): samo njihovi ključevi, s granicom
// knjige prije uvoza (granica), jer ju je ranija serija možda već pomaknula
func promjenaCekanja(serija []ledger.Version, ceka cekanje, granica map[string]string, ograda stanjeOgrade) promjenaRupa {
	p := promjenaRupa{kljucevi: map[string]promjenaKljuca{}, ograda: ograda}
	ceka = ceka.iznad(serija)
	for k := range ceka.trazi {
		p.kljucevi[k] = ceka.uKljuc(k, promjenaKljuca{fs: granica[k]})
	}
	return p
}

// iznad je isto čekanje uz najveću primljenu verziju ispod najmanje koja
// čeka, po ključu (ispodOdbijenih)
func (c cekanje) iznad(primljeno []ledger.Version) cekanje {
	c.ispod = ispodOdbijenih(primljeno, c.trazi)
	return c
}

// uKljuc doda promjeni ključa k verzije koje u njemu čekaju, u uTrazi, kao
// rasponCekanja: od najveće verzije ispod njih koja je u knjizi ili se
// upisuje, a kad su iznad granice knjige, barem od nje. Pokriveno skrati
// ispod verzija koje čekaju, a ne idu u trazi (doOstalih).
func (c cekanje) uKljuc(k string, pk promjenaKljuca) promjenaKljuca {
	if b, ok := c.trazi[k]; ok {
		pk.uTrazi = pk.uTrazi.uz(rasponCekanja(b, pk.fs, c.ispod[k], c.uKnjizi[k]))
	}
	pk.pokriveno = c.doOstalih(k, pk.pokriveno)
	return pk
}

// doOstalih je pokriveno ključa k (dijelovi za koje druga strana jamči da
// je dala sve) bez verzija koje čekaju, a ne idu u trazi (ostaju): dio koji
// drži takvu verziju završava ispod nje, na najvećoj primljenoj verziji
// ispod nje. Jamstvo druge strane računa na to da je ovo računalo sve
// primljeno upisalo ili odbilo, a takvu verziju nije ni jedno. Kad se
// razgovori preklapaju, drugi razgovor ili uvoz je možda već traži, a
// granicu knjige pomaknuo preko nje (granica s početka ovog razgovora to
// ne vidi), pa suzi to traženje ne smije maknuti.
func (c cekanje) doOstalih(k string, pokriveno []raspon) []raspon {
	ostaju := c.ostaju[k]
	if len(ostaju) == 0 {
		return pokriveno
	}
	out := slices.Clone(pokriveno)
	for i := range out {
		for _, o := range ostaju {
			if out[i].drziVerziju(o.id) {
				out[i].Do = o.ispod
			}
		}
	}
	return out
}

// granicaZaRupe je granica knjige za zapis rupa uz uvoz; kad se ne da
// pročitati, prazna, što je na sigurnu stranu
func (s *Service) granicaZaRupe(ctx context.Context) map[string]string {
	moja, err := s.rec.Frontier(ctx)
	if err != nil {
		return nil
	}
	return moja
}

// uveziSRupama upiše uvezene verzije kao razmjena: najprije pod bravom
// rupe (prosiri), pa verzije u serijama od 2000, članstva prva
// (clanstvaNaPocetak). Kad se rupe ne daju zapisati prije prve serije, ne
// upisuje ništa. Kad u kasnijoj seriji padne zapis rupa za verzije koje
// tek tu čekaju (primijeniPodBravom), uvoz staje s greškom, a ranije
// serije ostaju upisane i broje se u vraćenom broju.
// Verzije koje čekaju tek u provjeri serije bilježe se po istom uvjetu kao
// prije uvoza (zaCekanje), uz granicu knjige prije prve serije, a kao
// upisano se i tu broji sve što je prošlo prvu provjeru (wanted; vidi
// promjenaUvoza).
func (s *Service) uveziSRupama(ctx context.Context, wanted []ledger.Version, p promjenaRupa) (int, error) {
	if err := s.rupePodBravom(ctx, p, promjenaKljuca.prosiri); err != nil {
		return 0, err
	}
	u := uvjetCekanja{granica: s.granicaZaRupe(ctx)}
	slozi := func(serija, cekaju []ledger.Version) promjenaRupa {
		return promjenaCekanja(serija, s.zaCekanje(ctx, cekaju, wanted, u), u.granica, s.ogradaSada())
	}
	wanted = clanstvaNaPocetak(wanted)
	ukupno := 0
	for start := 0; start < len(wanted); start += 2000 {
		applied, err := s.primijeniPodBravom(ctx, wanted[start:min(start+2000, len(wanted))], slozi)
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
	return s.promijeniRupe(ctx, &p, korak)
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

// clanstvaNaPocetak su iste verzije, članstva prva, a ostale istim redom.
// Svaka serija uvoza ponovno provjerava potpise (primijeniPodBravom), a
// ključ autora vezan je uz ime tek kad je njegovo članstvo u knjizi ili u
// istoj seriji. Računalo potpisuje i prije ulaska u mrežu, pa njegovo
// članstvo može biti novije od njegovih verzija.
func clanstvaNaPocetak(verzije []ledger.Version) []ledger.Version {
	out := make([]ledger.Version, 0, len(verzije))
	for _, v := range verzije {
		if v.Entity == EntityMemberships {
			out = append(out, v)
		}
	}
	for _, v := range verzije {
		if v.Entity != EntityMemberships {
			out = append(out, v)
		}
	}
	return out
}
