package peers

// Jedan upis primljenog u isto vrijeme. Čvor razmjenjuje s do četiri čvora
// koja sam zove (syncPeers) i s do četiri koja zovu njega (port i tunel), a
// svaka razmjena primljeno upisuje u knjigu, na površinu i u spremište
// sadržaja. Istodobni upisi u istu bazu čekali su jedan drugoga dulje nego
// što SQLite čeka (5 s), pa je upis padao s „database is locked” (uredski
// čvor 12. 10. 2026., dok je istodobno primao s kućnog servera). Sad primljeno
// upisuje jedna razmjena u isto vrijeme, pod bravom čvora; isto i uvoz
// arhive i COP paketa.
//
// Mreža je izvan brave. Razgovor bravu čeka samo kad ima što upisati, i to
// najviše rokCekanjaUpisa. Kad je ne dobije, primljene verzije ne upisuje:
// pozvani čvor odbije ostatak razgovora (razlogUpisZauzet), a pozivatelj
// javi kraj. Razgovor tako završi greškom, pa krug razmjena s tim čvorom
// stane (syncPeers ne šalje isto odmah ponovno), a primljeno stigne
// sljedećom razmjenom. Ništa ne čeka upis nakon razgovora, pa mjesto
// razmjene ostaje zauzeto samo dok razgovor traje; bilješku o dolaznoj
// razmjeni čvor upisuje nakon što ga oslobodi. Brava se ne drži dok se
// čeka mreža ni druga brava (unutra su samo upisi u bazu), pa ne može
// nastati zastoj.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// rokCekanjaUpisa je koliko razgovor čeka bravu upisa prije nego što
// odustane od upisa. Druga strana na sljedeću poruku čeka 120 s (Receive),
// a tunel kroz Cloudflare prekida vezu bez prometa nakon 100 s; rok ostavlja
// mjesta i za sam upis.
var rokCekanjaUpisa = 30 * time.Second

// errUpisZauzet: razgovor nije dočekao bravu upisa, pa primljeno nije upisano
var errUpisZauzet = errors.New("primljeno nije upisano jer ovaj čvor dulje od pola minute upisuje ono što je primio u drugim razgovorima ili uvozom; stiže sljedećom razmjenom")

// razlogUpisZauzet je odbijenica ostatka razgovora kad pozvani čvor nije
// dočekao bravu upisa
const razlogUpisZauzet = "čvor upravo upisuje ono što je primio od drugih; poslano stiže sljedećom razmjenom"

// bravaUpisa je brava čvora za upis primljenog
func (s *Service) bravaUpisa() chan struct{} {
	s.upisJednom.Do(func() { s.upis = make(chan struct{}, 1) })
	return s.upis
}

// uzmiBravu čeka bravu upisa dok ctx traje
func (s *Service) uzmiBravu(ctx context.Context) error {
	select {
	case s.bravaUpisa() <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// uzmiBravuUnutar čeka bravu upisa najviše rok: nil kad ju je dobio,
// errUpisZauzet kad rok istekne
func (s *Service) uzmiBravuUnutar(ctx context.Context, rok time.Duration) error {
	t := time.NewTimer(rok)
	defer t.Stop()
	select {
	case s.bravaUpisa() <- struct{}{}:
		return nil
	case <-t.C:
		return errUpisZauzet
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) pustiBravu() { <-s.bravaUpisa() }

// primijeni upisuje primljene verzije u knjigu i, kad je nešto novo, na
// površinu. Zove se pod bravom. Greška površine vraća se zasebno: verzije
// su tada u knjizi, a površinu obnovi sljedeća primjena.
func (s *Service) primijeni(ctx context.Context, verzije []ledger.Version) (applied int, povrsina, err error) {
	// sastajalište provjerava prijave tek kad su opozivi i članstva na
	// površini, iz koje ih čita trusted
	defer s.nakonClanstva(verzije)
	applied, err = s.rec.Apply(ctx, verzije)
	if err != nil || applied == 0 || s.onApplied == nil {
		return applied, nil, err
	}
	return applied, s.onApplied(ctx, verzije), nil
}

// primijeniPodBravom upisuje verzije iz arhive ili COP paketa. Mreže tu
// nema, pa bravu čeka koliko treba. Neispravne verzije preskoči kao i
// razmjena (od G3; do tada je uvoz s njima padao), a vide se samo u
// dnevniku. Uvezeno može biti ispod granice odbijenih drugih računala, pa
// se uvoz broji (zabiljeziUvoz, granica_nakon_uvoza.go).
func (s *Service) primijeniPodBravom(ctx context.Context, verzije []ledger.Version) (int, error) {
	if err := s.uzmiBravu(ctx); err != nil {
		return 0, err
	}
	defer s.pustiBravu()
	applied, povrsina, err := s.primijeni(ctx, s.preskociNeispravne("uvoz", verzije))
	s.zabiljeziUvoz(applied, true)
	if povrsina != nil {
		return applied, fmt.Errorf("površina nije osvježena: %w", povrsina)
	}
	return applied, err
}

// primijeniAkoStigne upisuje verzije primljene u razgovoru, ako brava
// stigne unutar roka; inače vraća errUpisZauzet. Kad ne mijenja ni knjigu
// ni zapis rupa (promjenaRupa.nista), bravu ne čeka, pa ni kad su sve
// primljene neispravne. Upis koji popuni rupu u nizu broji se kao uvoz
// (popunjavaRupu, zabiljeziUvoz).
func (s *Service) primijeniAkoStigne(ctx context.Context, verzije []ledger.Version, p promjenaRupa) (int, error) {
	verzije = s.preskociNeispravne("razmjena", verzije)
	if p.nista(verzije) {
		return 0, nil
	}
	if err := s.uzmiBravuUnutar(ctx, rokCekanjaUpisa); err != nil {
		return 0, err
	}
	defer s.pustiBravu()
	rupa := s.popunjavaRupu(ctx, verzije)
	applied, povrsina, err := s.primijeniSRupama(ctx, verzije, p)
	s.zabiljeziUvoz(applied, rupa)
	javiPovrsinu(povrsina)
	return applied, err
}

// primijeniSRupama upiše primljeno pod bravom (rupe.go): najprije trajno
// ono što je odbijeno i stiglo s rupom, zatim verzije, a tek nakon
// uspješnog upisa makne ono što je pokriveno. Kad se rupe ne daju zapisati,
// ne upisuje ni verzije, pa sve stigne sljedećim razgovorom. Kad se ne da
// zapisati smanjenje, rupa ostane veća, a to košta jedno ponovno slanje.
func (s *Service) primijeniSRupama(ctx context.Context, verzije []ledger.Version, p promjenaRupa) (applied int, povrsina, err error) {
	if err := s.promijeniRupe(ctx, &p, promjenaKljuca.prosiri); err != nil {
		return 0, nil, err
	}
	applied, povrsina, err = s.primijeni(ctx, verzije)
	p.pravilo = false
	s.suziNakonUpisa(ctx, p, err)
	return applied, povrsina, err
}

// suziNakonUpisa makne iz zapisa rupa ono što je upis pokrio, kad je upis
// prošao; greška ide samo u dnevnik
func (s *Service) suziNakonUpisa(ctx context.Context, p promjenaRupa, upis error) {
	if upis != nil {
		return
	}
	if err := s.promijeniRupe(ctx, &p, promjenaKljuca.suzi); err != nil {
		log.Printf("razmjena: zapis rupa nije smanjen, pokriveno se traži još jednom: %v", err)
	}
}

// preskociNeispravne izostavi primljene verzije koje knjiga ovog računala
// ne prima (ledger.ProvjeriPrimljenu): bez oznake, entiteta ili
// identifikatora zapisa, s oznakom koja nije ispravnog oblika, sa znakom |
// u imenu autora ili kanala, s vremenom predaleko u budućnosti, ili s
// imenom ovog računala i vremenom ispred njegova sata. Ostale prođu, a
// razgovor zbog njih ne staje. Neispravnu zapiše u dnevnik jednom po
// verziji, s putem kojim je stigla (razmjena ili uvoz) i razlogom; verzije
// preskočene samo zbog vremena zapiše jednim retkom po autoru
// (zabiljeziKasne), jer ih uz krivi sat stiže mnogo i stići će kad im
// vrijeme dođe. Izostavlja ih već ovdje, a ne samo knjiga, jer površina
// smije dobiti samo ispravne: zbog jedne neispravne pala bi za cijeli
// paket. Sadržaj ne ide u dnevnik, a autor je pod navodnicima jer ga piše
// druga strana. To nisu neispravne verzije iz vlastite knjige
// (zabiljeziNeispravne), pa se ne broje na ploči.
func (s *Service) preskociNeispravne(put string, verzije []ledger.Version) []ledger.Version {
	sada := ledger.Sada()
	ispravne := make([]ledger.Version, 0, len(verzije))
	var kasne kasneVerzije
	for _, v := range verzije {
		zasto := ledger.ProvjeriPrimljenu(v, s.node.ID, sada)
		switch {
		case zasto == nil:
			ispravne = append(ispravne, v)
		case kasni(zasto):
			kasne.dodaj(v, zasto)
		default:
			s.zabiljeziPreskocenu(put, v, zasto)
		}
	}
	s.zabiljeziKasne(put, kasne)
	return ispravne
}

// zabiljeziPreskocenu zapiše u dnevnik primljenu neispravnu verziju, jednom
// po verziji
func (s *Service) zabiljeziPreskocenu(put string, v ledger.Version, zasto error) {
	kljuc := v.NodeID + "|" + v.VersionID + "|" + v.Entity + "|" + v.EntityID
	if _, bilo := s.preskocene.LoadOrStore(kljuc, struct{}{}); !bilo {
		log.Printf("%s: primljena verzija %.80q (entitet %.80q, zapis %.80q, autor %.80q) %v i ne upisuje se", put, v.VersionID, v.Entity, v.EntityID, v.NodeID, zasto)
	}
}

// primiSadrzajAkoStigne upisuje primljeni sadržaj, ako brava stigne unutar
// roka. Inače ga ne upisuje: ostaje među željama, pa ga ovaj čvor traži
// sljedećom razmjenom. Bez sadržaja bravu ne čeka.
func (s *Service) primiSadrzajAkoStigne(ctx context.Context, od string, stavke []stavkaSadrzaja) {
	if len(stavke) == 0 {
		return
	}
	if err := s.uzmiBravuUnutar(ctx, rokCekanjaUpisa); err != nil {
		log.Printf("sinkronizacija: primljeni sadržaj nije upisan (%d stavki), traži se sljedećom razmjenom: %v", len(stavke), err)
		return
	}
	defer s.pustiBravu()
	s.primiSadrzaj(ctx, od, stavke)
}

// zavrsiZauzet završava razgovor u kojem primljene verzije nisu upisane jer
// je upis zauzet: pozivatelj javi kraj, a pozvani odbije ostatak razgovora
// kodom i razlogom. Druga strana tako dobije odgovor, a ne prekinutu vezu,
// i krug razmjena s ovim čvorom stane. Pozvani najprije pročita sljedeću
// poruku pozivatelja (želje ili kraj), koja stiže tek nakon njegova upisa,
// da zatvaranje s nepročitanim podacima ne pošalje RST koji bi odbijenicu
// progutao.
func (s *Service) zavrsiZauzet(c *razmjena.Conn, initiator bool, err error) {
	switch {
	case !errors.Is(err, errUpisZauzet):
	case initiator:
		kraj, _ := razmjena.NewEnvelope(kindDone, doneMsg{})
		kraj.DeviceID = s.node.ID
		_ = c.Send(kraj)
		_, _ = c.PrimiDo(ogradaPoruke(kindDone)) // kraj druge strane samo se pročita prije zatvaranja
	default:
		s.odbiliSmo(RazlogUpisZauzet, c.PeerKey)
		_, _ = c.PrimiDo(ogradaPoruke(kindZelje)) // želje ili kraj pozivatelja
		_ = c.Send(razmjena.Odbijenica(razmjena.KodUpisZauzet, razlogUpisZauzet))
	}
}

// zabiljezi upisuje bilješku o razmjeni (stanje razmjene, naučenu adresu ili
// tvrdnju i bilješku uz zapis čvora, bez nove verzije) pod bravom upisa: i
// ona piše u bazu, a SQLite odbija upis koji je počeo čitanjem dok je drugi
// upisao („database is locked” bez čekanja). Razgovor je tada gotov, pa
// smije čekati. Put veze pamti se samo u memoriji, pa ne čeka bravu.
func (s *Service) zabiljezi(ctx context.Context, peer *Peer, c *razmjena.Conn, o syncOutcome) {
	s.upisiBiljesku(ctx, peer, s.izVeze(peer, c, o))
}

// izVeze uzme od veze sve što bilješka treba: put zadnje veze (samo u
// memoriji), put, brojače i adresu za učenje. Bilješka tako ne drži vezu
// dok čeka bravu.
func (s *Service) izVeze(peer *Peer, c *razmjena.Conn, o syncOutcome) syncOutcome {
	s.zapamtiPut(peer, c)
	o = o.uzVezu(c)
	if peer != nil {
		o.ucenje, _ = s.adresaZaUcenje(peer, c, o.dolazna) // veza koja ne smije učiti daje praznu adresu
	}
	return o
}

// upisiBiljesku upisuje bilješku kad dobije bravu upisa
func (s *Service) upisiBiljesku(ctx context.Context, peer *Peer, o syncOutcome) {
	if err := s.uzmiBravu(ctx); err != nil {
		return
	}
	defer s.pustiBravu()
	s.noteSync(ctx, peer, o)
}

// biljeskeNaCekanju su bilješke dolaznih razmjena koje čekaju bravu upisa,
// najviše jedna po računalu
type biljeskeNaCekanju struct {
	mu   sync.Mutex
	ceka map[string]biljeskaNaCekanju
}

type biljeskaNaCekanju struct {
	peer *Peer
	o    syncOutcome
}

// zabiljeziKasnije upisuje bilješku dolazne razmjene u svojoj gorutini,
// jer mjesto razmjene treba sljedećem. Dok bilješka istog računala još
// čeka bravu (npr. za dugog uvoza), novija je zamijeni, pa ne čeka više
// gorutina ni ishoda nego što ima računala.
func (s *Service) zabiljeziKasnije(ctx context.Context, peer *Peer, c *razmjena.Conn, o syncOutcome) {
	if peer == nil {
		return
	}
	o = s.izVeze(peer, c, o)
	s.biljeske.mu.Lock()
	if s.biljeske.ceka == nil {
		s.biljeske.ceka = map[string]biljeskaNaCekanju{}
	}
	_, ceka := s.biljeske.ceka[peer.NodeID]
	s.biljeske.ceka[peer.NodeID] = biljeskaNaCekanju{peer: peer, o: o}
	s.biljeske.mu.Unlock()
	if !ceka {
		go s.upisiNaCekanju(ctx, peer.NodeID)
	}
}

// upisiNaCekanju čeka bravu i upiše najnoviju bilješku računala koja čeka
func (s *Service) upisiNaCekanju(ctx context.Context, nodeID string) {
	err := s.uzmiBravu(ctx)
	s.biljeske.mu.Lock()
	b := s.biljeske.ceka[nodeID]
	delete(s.biljeske.ceka, nodeID)
	s.biljeske.mu.Unlock()
	if err != nil {
		return
	}
	defer s.pustiBravu()
	s.noteSync(ctx, b.peer, b.o)
}

// javiPovrsinu zapisuje u dnevnik da površina nije osvježena
func javiPovrsinu(err error) {
	if err != nil {
		log.Printf("sinkronizacija: verzije primljene, ali površina nije osvježena: %v", err)
	}
}

// odbijenaRazmjena je odbijenica druge strane, s njezinim razlogom i kodom
// (kodOdbijenice). Program odlučuje po kodu, a korisnik vidi samo razlog.
type odbijenaRazmjena struct {
	razlog string
	kod    razmjena.KodRazloga
}

func (e odbijenaRazmjena) Error() string { return "čvor odbio razmjenu: " + e.razlog }

// neocekivanaPoruka je greška za poruku druge strane koja nije ona koju
// razgovor čeka; odbijenica nosi razlog i kod druge strane. Pamti se i
// prikazuje najviše najduljiRazlog bajtova razloga i 40 znakova vrste.
func neocekivanaPoruka(kind string, e razmjena.Envelope) error {
	if e.Reason != "" {
		return odbijenaRazmjena{razlog: skrati(e.Reason, najduljiRazlog), kod: kodOdbijenice(e)}
	}
	return fmt.Errorf("očekivana poruka %q, stigla %.40q", kind, e.Kind)
}

// kodoviStarijih su kodovi odbijenica čvora 0.0.38, koji šalje samo razlog;
// to su sva tri razloga koja on zna poslati. Tekstovi se zato ne mijenjaju
// dok u mreži ima čvorova 0.0.38, a kad ih više nema, popis se briše.
var kodoviStarijih = map[string]razmjena.KodRazloga{
	razmjena.RazlogZauzet: razmjena.KodZauzet,
	razlogUpisZauzet:      razmjena.KodUpisZauzet,
	razlogBezTailscalea:   razmjena.KodBezTailscalea,
}

// kodOdbijenice je kod iz odbijenice; kad ga nema (čvor 0.0.38), kod
// poznatog razloga, a za nepoznat razlog 0
func kodOdbijenice(e razmjena.Envelope) razmjena.KodRazloga {
	if e.Kod != 0 {
		return e.Kod
	}
	return kodoviStarijih[e.Reason]
}
