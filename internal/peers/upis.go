package peers

// Jedan upis primljenog u isto vrijeme. Čvor razmjenjuje s do četiri čvora
// koja sam zove (syncPeers) i s do četiri koja zovu njega (port i tunel), a
// svaka razmjena primljeno upisuje u knjigu, na površinu i u spremište
// sadržaja. Istodobni upisi u istu bazu čekali su jedan drugoga dulje nego
// što SQLite čeka (5 s), pa je upis padao s „database is locked” (uredski
// čvor 12. 10. 2026., dok je istodobno primao s Unraida). Sad primljeno
// upisuje jedna razmjena u isto vrijeme, pod bravom čvora; isto i uvoz
// arhive i COP paketa.
//
// Mreža je izvan brave. Razgovor bravu čeka najviše rokCekanjaUpisa, a
// kad je ne dobije, upis ostavi za kraj razgovora, kad druga strana više
// ne čeka odgovor. Brava se ne drži dok se čeka mreža ni druga brava
// (unutra su samo upisi u bazu), pa ne može nastati zastoj.

import (
	"context"
	"fmt"
	"log"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// rokCekanjaUpisa je koliko razgovor čeka bravu upisa prije nego što upis
// ostavi za kraj. Druga strana na sljedeću poruku čeka 120 s (Receive), a
// tunel kroz Cloudflare prekida vezu bez prometa nakon 100 s; rok ostavlja
// mjesta i za sam upis.
var rokCekanjaUpisa = 30 * time.Second

// primitak je ono što je jedan razgovor primio, a još nije upisao
type primitak struct {
	verzije []ledger.Version
	sadrzaj []stavkaSadrzaja
	od      string // ključ pošiljatelja: izvor primljenog sadržaja
}

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

// uzmiBravuUnutar čeka bravu upisa najviše rok; javlja je li je dobio
func (s *Service) uzmiBravuUnutar(ctx context.Context, rok time.Duration) bool {
	t := time.NewTimer(rok)
	defer t.Stop()
	select {
	case s.bravaUpisa() <- struct{}{}:
		return true
	case <-t.C:
		return false
	case <-ctx.Done():
		return false
	}
}

func (s *Service) pustiBravu() { <-s.bravaUpisa() }

// primijeni upisuje primljene verzije u knjigu i, kad je nešto novo, na
// površinu. Zove se pod bravom. Greška površine vraća se zasebno: verzije
// su tada u knjizi, a površinu obnovi sljedeća primjena.
func (s *Service) primijeni(ctx context.Context, verzije []ledger.Version) (applied int, povrsina, err error) {
	applied, err = s.rec.Apply(ctx, verzije)
	if err != nil || applied == 0 || s.onApplied == nil {
		return applied, nil, err
	}
	return applied, s.onApplied(ctx, verzije), nil
}

// primijeniPodBravom upisuje verzije iz arhive ili COP paketa. Mreže tu
// nema, pa bravu čeka koliko treba.
func (s *Service) primijeniPodBravom(ctx context.Context, verzije []ledger.Version) (int, error) {
	if err := s.uzmiBravu(ctx); err != nil {
		return 0, err
	}
	defer s.pustiBravu()
	applied, povrsina, err := s.primijeni(ctx, verzije)
	if povrsina != nil {
		return applied, fmt.Errorf("površina nije osvježena: %w", povrsina)
	}
	return applied, err
}

// primijeniAkoStigne upisuje verzije primljene u razgovoru odmah, ako brava
// stigne unutar roka; inače ih ostavlja za kraj razgovora (dovrsiPrimitak)
func (s *Service) primijeniAkoStigne(ctx context.Context, pr *primitak) (int, error) {
	if !s.uzmiBravuUnutar(ctx, rokCekanjaUpisa) {
		return 0, nil
	}
	defer s.pustiBravu()
	verzije := pr.verzije
	pr.verzije = nil
	applied, povrsina, err := s.primijeni(ctx, verzije)
	javiPovrsinu(povrsina)
	return applied, err
}

// primiSadrzajAkoStigne upisuje primljeni sadržaj odmah, ako brava stigne
// unutar roka; inače ga ostavlja za kraj razgovora
func (s *Service) primiSadrzajAkoStigne(ctx context.Context, pr *primitak, stavke []stavkaSadrzaja) {
	if !s.uzmiBravuUnutar(ctx, rokCekanjaUpisa) {
		pr.sadrzaj = stavke
		return
	}
	defer s.pustiBravu()
	s.primiSadrzaj(ctx, pr.od, stavke)
}

// dovrsiPrimitak upisuje na kraju razgovora ono što nije stiglo na red;
// vraća koliko je novih verzija upisano. Mreža je tada gotova, pa smije
// čekati bravu dok ctx traje.
func (s *Service) dovrsiPrimitak(ctx context.Context, pr *primitak) int {
	if len(pr.verzije) == 0 && len(pr.sadrzaj) == 0 {
		return 0
	}
	if err := s.uzmiBravu(ctx); err != nil {
		log.Printf("sinkronizacija: primljeno nije upisano: %v", err)
		return 0
	}
	defer s.pustiBravu()
	applied, povrsina, err := s.primijeni(ctx, pr.verzije)
	if err != nil {
		log.Printf("sinkronizacija: primljene verzije nisu upisane, stići će sljedećom razmjenom: %v", err)
	}
	javiPovrsinu(povrsina)
	s.primiSadrzaj(ctx, pr.od, pr.sadrzaj)
	return applied
}

// zabiljezi upisuje bilješku o razmjeni (stanje razmjene, zapis čvora,
// ponekad i njegovu verziju) pod bravom upisa: i ona piše u bazu, a SQLite
// odbija upis koji je počeo čitanjem dok je drugi upisao („database is
// locked” bez čekanja). Razgovor je tada gotov, pa smije čekati.
func (s *Service) zabiljezi(ctx context.Context, peer *Peer, c *razmjena.Conn, o syncOutcome) {
	if err := s.uzmiBravu(ctx); err != nil {
		return
	}
	defer s.pustiBravu()
	s.noteSync(ctx, peer, c, o)
}

// javiPovrsinu zapisuje u dnevnik da površina nije osvježena
func javiPovrsinu(err error) {
	if err != nil {
		log.Printf("sinkronizacija: verzije primljene, ali površina nije osvježena: %v", err)
	}
}
