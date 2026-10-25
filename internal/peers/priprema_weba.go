package peers

// WEB sync u servisu (plan WEB synca, F3b): jezgra iz internal/razmjena
// (WebSync) na UDP utičnici programa, priprema u pozadini i pauza po paru.
//
// Program da servisu WEB sync na svakoj novoj UDP utičnici
// (PokreniWebSync): uz web_sync = true on radi (Radi), a uz web_sync =
// false samo na pozive odgovara „isključeno” (Odbija). Nova utičnica
// zatvori sesije stare (njezin WebSync staje) i obriše pauze. Dok WEB sync
// postoji, računalo javlja oznaku web/1.
//
// Priprema u pozadini (pripremiWeb, iz kruga razmjene u redoslijed.go):
//   - kreće kad LAN ne odgovori, a sesije s tim računalom nema, kad drugo
//     računalo ima web/1 i nije u pauzi;
//   - ne kreće prema računalu s javnim ulazom (Odluke (24. 10. 2026.)):
//     prema računalu s tunelom nikad, a prema računalu s javnom adresom
//     tek kad ona u tom krugu nije odgovorila;
//   - najviše 4 odjednom, jedna po računalu, izvan četiri mjesta za
//     razmjenu; krug je ne čeka;
//   - kad uspije, sesija ostaje živa (razmjena), a razmjena s tim
//     računalom kreće odmah; ishod ide u brojače pripreme u peer_sync.
//
// Pauza po paru: 3 neuspjeha zaredom daju pauzu od 30 min, a svaki idući
// neuspjeh 1 h. Odgovor „isključeno” odmah daje pauzu (Odluke
// (25. 10. 2026.), 1). Pauza se briše kad se promijeni stanje mreže ove
// strane (javni kandidati i vrsta NAT-a) ili druge (iz njezinog poziva ili
// odziva), nakon uspjeha i s novom UDP utičnicom. Drugi ključ na adresi
// druge strane, prekid i istodobni poziv druge strane nisu neuspjeh.
//
// Ključ cilja i expect za QUIC su iz članstva (kljucClana, u
// partnerZaRazmjenu), nikad iz zapisa čvora. Kandidati i stanje mreže druge
// strane drže se samo u memoriji. U dnevnik idu samo imena računala.

import (
	"cmp"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"sync"
	"time"

	"gocop/internal/razmjena"
)

const (
	najviseWebPriprema = 4                // priprema u pozadini odjednom
	neuspjehaDoPauze   = 3                // neuspjeha zaredom prije pauze
	prvaPauza          = 30 * time.Minute // prva pauza; svaka iduća 1 h
	najduljaPauza      = time.Hour
)

// ErrWebSyncSePriprema: do računala nema puta, a WEB sync s njim se
// priprema u pozadini. To nije neuspjeh razmjene (due ga ne broji):
// razmjena kreće čim je WEB sync spreman.
var ErrWebSyncSePriprema = errors.New("WEB sync se priprema")

// razloziBezPauze su razlozi prekida pripreme koji nisu neuspjeh
// probijanja: druga strana istodobno zove (njezin poziv vrijedi), WEB sync
// ovog računala je stao, druga strana je poslala noviji poziv
var razloziBezPauze = []string{razmjena.RazlogDrugaZove, razmjena.RazlogStao, razmjena.RazlogNoviPoziv}

// webSyncServisa je WEB sync na jednoj UDP utičnici: ukljucen uz Radi,
// inače Odbija. Pripreme u pozadini i razmjene nakon njih idu pod ctx i
// prate se u pozadina; kreću samo kroz uPozadini, pod mu, dok ctx traje.
type webSyncServisa struct {
	w           *razmjena.WebSync
	ctx         context.Context
	otkazi      context.CancelFunc
	ukljucen    bool
	prijePoziva func(context.Context)
	mu          sync.Mutex
	pozadina    sync.WaitGroup
}

// uPozadini pokrene f u pozadini WEB synca, osim kad on staje ili je stao;
// javlja je li f krenula. Pod mu, pa zaustavljanje (stani) ne može početi
// čekati pozadinu dok se ona još povećava.
func (ws *webSyncServisa) uPozadini(f func()) bool {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.ctx.Err() != nil {
		return false
	}
	ws.pozadina.Go(f)
	return true
}

// stani zaustavi pozadinu: nakon otkazivanja pod mu nijedna nova gorutina
// ne kreće, pa čekanje vidi sve
func (ws *webSyncServisa) stani() {
	ws.mu.Lock()
	ws.otkazi()
	ws.mu.Unlock()
	ws.pozadina.Wait()
}

// PostavkeWebSynca su postavke WEB synca koje zna servis: ključ, ime
// protokola, vjerodajnice, članstvo, ograda (ista mjesta za razmjene kao
// port i tunel, a odbijanja na vratima idu u zapis odbijenog), obrada
// dolaznog razgovora pod ctx, sesije, pozdrav i Tailscale. Program dodaje
// slanje preko sastajališta, ključ sastajališta i vlastitu mrežu.
func (s *Service) PostavkeWebSynca(ctx context.Context) razmjena.PostavkeWeb {
	return razmjena.PostavkeWeb{Priv: s.node.key, Protokol: s.protokol(), Vj: s.vjerodajnice, Trusted: s.trusted, Ograda: s.ogradaWeb,
		Handle:   func(c *razmjena.Conn) { s.primiRazmjenu(ctx, c) },
		NaSesiju: s.naSesijuWeba, Znanje: s.znanjeZaPozdrav, NaPoziv: s.naPozivWeba,
		BezTailscalea: s.bezTailscalea.Load, TailscaleAdrese: s.vlastiteTailscale, Izlaz: s.izlazUDP}
}

// PokreniWebSync pokrene WEB sync w ove UDP utičnice dok ctx traje: uz
// ukljucen Radi, inače Odbija. prijePoziva (smije biti nil) se zove prije
// svake pripreme u pozadini, da program osvježi mjerenje mreže. Pauze se
// brišu, jer je utičnica nova. Vraća zaustavljanje, koje pričeka i
// pripreme u pozadini; drugi poziv ne radi ništa.
func (s *Service) PokreniWebSync(ctx context.Context, w *razmjena.WebSync, ukljucen bool, prijePoziva func(context.Context)) func() {
	ctx, otkazi := context.WithCancel(ctx)
	ws := &webSyncServisa{w: w, ctx: ctx, otkazi: otkazi, ukljucen: ukljucen, prijePoziva: prijePoziva}
	if ws.prijePoziva == nil {
		ws.prijePoziva = func(context.Context) {}
	}
	s.pripreme.ocisti()
	s.web.Store(ws)
	gotovo := make(chan struct{})
	go func() {
		defer close(gotovo)
		s.radiWeb(ws)
	}()
	return sync.OnceFunc(func() {
		s.web.CompareAndSwap(ws, nil)
		otkazi()
		<-gotovo
		ws.stani()
	})
}

// radiWeb drži WEB sync dok mu ctx traje; greška ide u dnevnik
func (s *Service) radiWeb(ws *webSyncServisa) {
	radi := ws.w.Odbija
	if ws.ukljucen {
		radi = ws.w.Radi
	}
	if err := radi(ws.ctx); err != nil {
		log.Printf("povezivost: WEB sync ne radi: %v", err)
	}
}

// webSync je WEB sync ovog računala; nil dok ga nema
func (s *Service) webSync() *razmjena.WebSync {
	if ws := s.web.Load(); ws != nil {
		return ws.w
	}
	return nil
}

// znanjeZaPozdrav je znanje ovog računala za pozdrav sesije
func (s *Service) znanjeZaPozdrav() *razmjena.Znanje {
	z := s.mojeZnanje(context.Background())
	return &z
}

// naSesijuWeba: sesija s računalom briše njegovu pauzu; sesiju koju je
// pripremilo drugo računalo bilježi dnevnik
func (s *Service) naSesijuWeba(ses *razmjena.SesijaWeb) {
	kljuc := razmjena.PublicKeyString(ses.Druga)
	if s.pripreme.uspjeh(kljuc) {
		return
	}
	log.Printf("povezivost: WEB sync s %s uspio u %d. pokušaju, pozvao=drugi", s.imeIliKljuc(ses.Druga), ses.Pokusaj)
}

// naPozivWeba: poziv drugog računala s drugim stanjem mreže briše pauzu
// para
func (s *Service) naPozivWeba(druga ed25519.PublicKey, otisak string) {
	s.pripreme.stanjeDruge(razmjena.PublicKeyString(druga), otisak)
}

// provjeriWebSync ponovno provjeri članstvo druge strane svake sesije
// (zove ga javiPromjenuClanstva)
func (s *Service) provjeriWebSync() {
	if w := s.webSync(); w != nil {
		w.ProvjeriSve()
	}
}

// imaWeb javlja zna li drugo računalo WEB sync (oznaka web/1 iz zadnje
// razmjene, i kod ovog računala)
func (s *Service) imaWeb(ctx context.Context, nodeID string) bool {
	var b string
	_ = s.db.QueryRowContext(ctx, `SELECT mogucnosti FROM peer_sync WHERE node_id = ?`, nodeID).Scan(&b) // bez retka nema ni oznaka
	var njihove []string
	_ = json.Unmarshal([]byte(b), &njihove) // prazno ili pokvareno: druga strana nije javila ništa
	return razmjena.Dogovor(s.mojeMogucnosti(), njihove)[razmjena.ImeWeb] == 1
}

// pripremaPrije: bez javne adrese u zapisu priprema kreće usporedo s
// ostalim putovima
func (k *krug) pripremaPrije() {
	if !k.javnih {
		k.pripremiWeb()
	}
}

// pripremaPoslije: uz javnu adresu u zapisu priprema kreće tek kad ona u
// ovom krugu nije odgovorila, jer javna adresa koja odgovara je javni
// ulaz (Odluke (24. 10. 2026.))
func (k *krug) pripremaPoslije(pobjednik kandidat, err error) {
	if k.javnih && (err != nil || !javnaAdresa(pobjednik)) {
		k.pripremiWeb()
	}
}

// javnaAdresa: kandidat je javna adresa ili domena koju je upisao čovjek
func javnaAdresa(k kandidat) bool {
	return k.put == razmjena.PutAdresa && !privatnaAdresa(k.adresa)
}

// pripremiWeb pokrene pripremu WEB synca s računalom kruga u pozadini, kad
// treba i smije; krug zapamti razlog puta (priprema, pauza) i je li
// priprema u tijeku
func (k *krug) pripremiWeb() {
	ws := k.s.web.Load()
	if ws == nil || !ws.ukljucen || k.tunel || !k.s.imaWeb(ws.ctx, k.peer.NodeID) {
		return
	}
	peer, expect := *k.peer, k.expect
	k.razlogWeba, k.uPripremi = k.s.pripreme.pokreni(ws.ctx, peer.NodeID, razmjena.PublicKeyString(expect), ws.w.MojOtisak(), time.Now(),
		func(ctx context.Context) bool { return ws.uPozadini(func() { k.s.pripremi(ctx, ws, peer, expect) }) })
}

// pripremi pripremi WEB sync s računalom i zabilježi ishod
func (s *Service) pripremi(ctx context.Context, ws *webSyncServisa, peer Peer, expect ed25519.PublicKey) {
	kljuc := razmjena.PublicKeyString(expect)
	defer s.pripreme.gotova(kljuc)
	ws.prijePoziva(ctx)
	moj := ws.w.MojOtisak()
	pocetak := time.Now()
	ses, err := ws.w.Pripremi(ctx, expect)
	s.nakonPripreme(ctx, ws, peer, ishodPripreme{kljuc: kljuc, moj: moj, trajanje: time.Since(pocetak), ses: ses, err: err})
}

// ishodPripreme je ishod jedne pripreme u pozadini: s kim (ključ), stanje
// mreže ove strane na početku, koliko je trajala i sesija ili greška
type ishodPripreme struct {
	kljuc, moj string
	trajanje   time.Duration
	ses        *razmjena.SesijaWeb
	err        error
}

// nakonPripreme: uspjeh briše pauzu i odmah pokreće razmjenu, a neuspjeh
// se broji za pauzu (greskaZaPauzu)
func (s *Service) nakonPripreme(ctx context.Context, ws *webSyncServisa, peer Peer, i ishodPripreme) {
	if i.err == nil {
		s.pripreme.ocistiPauzu(i.kljuc)
		s.brojiPripremu(ws.ctx, peer.NodeID, true)
		log.Printf("povezivost: WEB sync s %s uspio u %d. pokušaju (%d ms), pozvao=ovaj", label(peer), i.ses.Pokusaj, i.trajanje.Milliseconds())
		ws.uPozadini(func() { s.razmijeniOdmah(ws.ctx, peer) }) // WEB sync koji staje ne razmjenjuje
		return
	}
	g, broji := greskaZaPauzu(ctx, i.err)
	if !broji {
		return
	}
	s.pripreme.neuspjeh(i.kljuc, peer.NodeID, i.moj, g, time.Now())
	s.brojiPripremu(ws.ctx, peer.NodeID, false)
	log.Printf("povezivost: WEB sync s %s nije uspio (%s; pokušaja: %d), pozvao=ovaj", label(peer), g.Razlog, g.Pokusaj)
}

// greskaZaPauzu je greška pripreme kao neuspjeh za pauzu i broji li se.
// Ne broje se prekid (kraj rada, nova utičnica), priprema koja već teče,
// WEB sync koji ne radi, ovo računalo koje nije prijavljeno kod
// sastajališta (kvar je ovdje, ne kod para), istodobni poziv druge strane
// i drugi ključ na adresi druge strane. Poziv koji ni ne ode preko
// sastajališta (druga strana nije prijavljena) broji se kao „nema”.
func greskaZaPauzu(ctx context.Context, err error) (razmjena.GreskaPripreme, bool) {
	var g razmjena.GreskaPripreme
	switch {
	case errors.As(err, &g):
		return g, neuspjehProbijanja(g)
	case ctx.Err() != nil || slices.ContainsFunc(greskeBezPauze, func(e error) bool { return errors.Is(err, e) }):
		return g, false
	}
	return razmjena.GreskaPripreme{Razlog: razmjena.RazlogNema}, true
}

// greskeBezPauze su greške pripreme koje nisu neuspjeh para (greskaZaPauzu)
var greskeBezPauze = []error{razmjena.ErrPripremaUTijeku, razmjena.ErrWebSyncNeRadi, razmjena.ErrNijePrijavljen}

// neuspjehProbijanja javlja broji li se greška pripreme za pauzu
func neuspjehProbijanja(g razmjena.GreskaPripreme) bool {
	return !g.DrugiKljuc && !slices.Contains(razloziBezPauze, g.Razlog)
}

// razmijeniOdmah: nakon uspjele pripreme razmjena s tim računalom kreće
// odmah, ne za 5 min
func (s *Service) razmijeniOdmah(ctx context.Context, peer Peer) {
	for cvor, biljeska := range s.syncPeers(ctx, []Peer{peer}) {
		log.Printf("sinkronizacija s %s: %s", cvor, biljeska)
	}
}

// brojiPripremu doda ishod pripreme brojačima u peer_sync
func (s *Service) brojiPripremu(ctx context.Context, nodeID string, uspjeh bool) {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO peer_sync (node_id, priprema_pokusaji, priprema_uspjesi, priprema_neuspjesi) VALUES (?1, 1, ?2, 1 - ?2)
		ON CONFLICT(node_id) DO UPDATE SET priprema_pokusaji = priprema_pokusaji + 1, priprema_uspjesi = priprema_uspjesi + ?2,
			priprema_neuspjesi = priprema_neuspjesi + 1 - ?2`, nodeID, boolInt(uspjeh)); err != nil {
		log.Printf("povezivost: brojač pripreme WEB synca za %s nije spremljen: %v", nodeID, err)
	}
}

// zapisiRazlogPuta upiše razlog puta kad razmjena nije ni počela, a to nije
// neuspjeh (WEB sync se priprema)
func (s *Service) zapisiRazlogPuta(ctx context.Context, nodeID string, r razmjena.RazlogPuta) {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO peer_sync (node_id, put_razlog) VALUES (?, ?)
		ON CONFLICT(node_id) DO UPDATE SET put_razlog = excluded.put_razlog`, nodeID, string(r)); err != nil {
		log.Printf("razmjena: razlog puta za %s nije spremljen: %v", nodeID, err)
	}
}

// pripremeWeba su pripreme u tijeku i pauze, po ključu druge strane
type pripremeWeba struct {
	mu    sync.Mutex
	teku  map[string]bool
	pauze map[string]*pauzaPara
}

// pauzaPara je stanje neuspjeha s jednim računalom
type pauzaPara struct {
	nodeID      string
	neuspjeha   int
	do          time.Time
	moje, druge string // stanje mreže obiju strana (razmjena.OtisakMreze)
	razlog      razmjena.RazlogPuta
}

// pokreni pokrene pripremu kad par nije u pauzi, priprema s njim već ne
// teče i nema ih najviše; vraća razlog puta i teče li priprema. pocni
// javlja je li priprema krenula (WEB sync koji staje je ne pokreće).
func (pw *pripremeWeba) pokreni(ctx context.Context, nodeID, kljuc, moj string, sad time.Time, pocni func(context.Context) bool) (razmjena.RazlogPuta, bool) {
	pw.mu.Lock()
	defer pw.mu.Unlock()
	if r, ok := pw.uPauzi(kljuc, moj, sad); ok {
		return r, false
	}
	switch {
	case pw.teku[kljuc]:
		return razmjena.RazlogPutaPriprema, true
	case len(pw.teku) >= najviseWebPriprema:
		return "", false
	}
	if pw.teku == nil {
		pw.teku = map[string]bool{}
	}
	pw.teku[kljuc] = true
	if !pocni(ctx) {
		delete(pw.teku, kljuc)
		return "", false
	}
	return razmjena.RazlogPutaPriprema, true
}

// uPauzi je razlog pauze para kad je u pauzi; pauza ode kad se promijenilo
// stanje mreže ove strane. Pod pw.mu.
func (pw *pripremeWeba) uPauzi(kljuc, moj string, sad time.Time) (razmjena.RazlogPuta, bool) {
	p := pw.pauze[kljuc]
	switch {
	case p == nil:
		return "", false
	case p.moje != moj:
		delete(pw.pauze, kljuc)
		return "", false
	case p.neuspjeha < neuspjehaDoPauze || !sad.Before(p.do):
		return "", false
	}
	return p.razlog, true
}

// gotova: priprema s računalom je završila
func (pw *pripremeWeba) gotova(kljuc string) {
	pw.mu.Lock()
	defer pw.mu.Unlock()
	delete(pw.teku, kljuc)
}

// uspjeh: sesija s računalom briše njegovu pauzu; javlja je li ovo
// računalo tu sesiju pripremalo
func (pw *pripremeWeba) uspjeh(kljuc string) bool {
	pw.mu.Lock()
	defer pw.mu.Unlock()
	delete(pw.pauze, kljuc)
	return pw.teku[kljuc]
}

// ocistiPauzu obriše pauzu para
func (pw *pripremeWeba) ocistiPauzu(kljuc string) {
	pw.mu.Lock()
	defer pw.mu.Unlock()
	delete(pw.pauze, kljuc)
}

// ocisti obriše sve pauze (nova UDP utičnica)
func (pw *pripremeWeba) ocisti() {
	pw.mu.Lock()
	defer pw.mu.Unlock()
	pw.pauze = nil
}

// stanjeDruge: druga strana javlja drugo stanje mreže, pa njezina pauza
// ide
func (pw *pripremeWeba) stanjeDruge(kljuc, otisak string) {
	pw.mu.Lock()
	defer pw.mu.Unlock()
	if p := pw.pauze[kljuc]; p != nil && promijenjeno(p.druge, otisak) {
		delete(pw.pauze, kljuc)
	}
}

// neuspjeh zabilježi neuspjelu pripremu: od trećeg neuspjeha zaredom par
// je u pauzi, a „isključeno” je odmah u pauzi
func (pw *pripremeWeba) neuspjeh(kljuc, nodeID, moj string, g razmjena.GreskaPripreme, sad time.Time) {
	pw.mu.Lock()
	defer pw.mu.Unlock()
	p := pw.pauze[kljuc].nastavak(nodeID, moj, g.Druga)
	p.neuspjeha = max(p.neuspjeha+1, pragNeuspjeha(g))
	p.razlog = razlogPauze(g, moj)
	p.do = sad.Add(trajanjePauze(p.neuspjeha)) // vrijedi tek od trećeg neuspjeha
	if pw.pauze == nil {
		pw.pauze = map[string]*pauzaPara{}
	}
	pw.pauze[kljuc] = p
}

// nastavak je stanje para za još jedan neuspjeh: isto, a kad se stanje
// mreže bilo koje strane promijenilo, brojanje kreće ispočetka
func (p *pauzaPara) nastavak(nodeID, moj, druge string) *pauzaPara {
	if p == nil || p.moje != moj || promijenjeno(p.druge, druge) {
		return &pauzaPara{nodeID: nodeID, moje: moj, druge: druge}
	}
	p.druge = cmp.Or(druge, p.druge)
	return p
}

// promijenjeno: oba stanja mreže su poznata i nisu ista
func promijenjeno(staro, novo string) bool {
	return staro != "" && novo != "" && staro != novo
}

// pragNeuspjeha: „isključeno” odmah daje pauzu
func pragNeuspjeha(g razmjena.GreskaPripreme) int {
	if g.Razlog == razmjena.RazlogIskljuceno {
		return neuspjehaDoPauze
	}
	return 0
}

// trajanjePauze je 30 min nakon trećeg neuspjeha, a zatim 1 h
func trajanjePauze(neuspjeha int) time.Duration {
	if neuspjeha > neuspjehaDoPauze {
		return najduljaPauza
	}
	return prvaPauza
}

// razlogPauze je razlog na pločici dok je par u pauzi: UDP ne prolazi kod
// ove ili kod druge strane, udarci druge strane nisu stigli nijednom iako
// ih je ova slala (vatrozid ovog računala), inače pauza nakon neuspjeha
func razlogPauze(g razmjena.GreskaPripreme, moj string) razmjena.RazlogPuta {
	switch {
	case g.Razlog == razmjena.RazlogUDP && strings.HasSuffix(moj, "|true"):
		return razmjena.RazlogPutaUDPNeProlazi
	case g.Razlog == razmjena.RazlogUDP:
		return razmjena.RazlogPutaUDPDoDrugih
	case g.Udaraca > 0 && g.Valjanih == 0:
		return razmjena.RazlogPutaVatrozid
	}
	return razmjena.RazlogPutaPauza
}

// pauzeDo su računala u pauzi i do kada traje
func (pw *pripremeWeba) pauzeDo(sad time.Time) map[string]time.Time {
	pw.mu.Lock()
	defer pw.mu.Unlock()
	out := map[string]time.Time{}
	for _, p := range pw.pauze {
		if p.neuspjeha >= neuspjehaDoPauze && sad.Before(p.do) {
			out[p.nodeID] = p.do
		}
	}
	return out
}

// opisPripreme je greška kruga kad do računala nema puta, a WEB sync se
// priprema
func opisPripreme(nodeID string) error {
	return fmt.Errorf("čvor %s: %w; razmjena kreće čim bude spreman", nodeID, ErrWebSyncSePriprema)
}
