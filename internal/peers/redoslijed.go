package peers

// Redoslijed putova u jednom krugu razmjene s računalom (plan WEB synca,
// F3b, „Redoslijed u SyncWith”; Odluke (14. 10. 2026.): LAN → WEB → SERVER,
// a program uvijek prvo proba LAN):
//  1. LAN sync: adrese iz pronalaženja i spremljene privatne adrese, TCP,
//     usporedno, svaka iduća 250 ms kasnije, svaka s rokom od 3 s. LAN ide
//     prvi i kad postoji živa WEB sync sesija, pa laptop koji dođe kući
//     iduću razmjenu radi LAN syncom.
//  2. WEB sync, kad s tim računalom postoji živa sesija i odgovori na ping
//     (najmanje 1 s, inače 3 RTT-a). Sesija koja ne odgovori se zatvara, a
//     krug ide dalje, pa mrtva sesija (druga strana je promijenila mrežu,
//     a rok mirovanja još nije istekao) ne drži Tailscale ni tunel.
//  3. Inače zajedno, odmah: Tailscale, javne adrese i domene koje je
//     upisao čovjek (SERVER sync) i tunel (SERVER sync), svaka s rokom od
//     20 s. Usporedo s tim, kad treba, u pozadini kreće priprema WEB synca
//     (priprema_weba.go), koju krug ne čeka.
//
// U svakom koraku vrijedi prva veza koja prođe TLS s očekivanim ključem;
// ostale se zatvaraju prije razgovora, pa razgovor ide samo na jednoj.
// Spajanje ima ukupni rok od 45 s; razgovor na vezi koja je pobijedila ima
// rok onoga tko je zvao SyncWith, jer razmjena s puno zapisa i sadržaja
// smije trajati dulje. Tunel nikad ne čeka WEB sync.
//
// Kad druga strana odbije razmjenu jer joj je Tailscale isključen
// (KodBezTailscalea), krug zaboravi tu vezu i Tailscale adrese i spaja se
// ponovno ostalim putovima.

import (
	"context"
	"crypto/ed25519"
	"errors"
	"log"
	"time"

	"gocop/internal/razmjena"
)

// Rokovi kruga; var, da ih test skrati
var (
	rokSpajanja = 45 * time.Second       // cijelo spajanje u jednom krugu
	rokLAN      = 3 * time.Second        // jedna adresa u lokalnoj mreži
	razmakLAN   = 250 * time.Millisecond // iduća adresa u lokalnoj mreži kreće toliko kasnije
	rokOstalih  = 20 * time.Second       // Tailscale, javna adresa ili tunel
	rokPinga    = time.Second            // najmanje toliko sesija ima za odgovor na ping
)

// najviseAdresaPuta: jedan krug zove najviše toliko adresa istim putem
// (LAN sve zajedno), redom iz adreseZaSpajanje. Zapis čvora piše bilo koji
// član, pa bi inače stotine podmetnutih adresa svako računalo u mreži zvalo
// odjednom, u svakom krugu.
const najviseAdresaPuta = 8

var (
	// errBezKandidata: korak nema nijednu adresu
	errBezKandidata = errors.New("nijedna adresa")
	// errDrugaAdresa je razlog prekida poziva koje je druga adresa
	// pretekla: njihov ishod ne govori ništa o adresi
	errDrugaAdresa = errors.New("javila se druga adresa")
)

// krug je jedan krug razmjene s računalom: adrese po koracima i što se u
// njemu dogodilo
type krug struct {
	s      *Service
	peer   *Peer
	expect ed25519.PublicKey
	// lan su adrese iz lokalne mreže, a ostali Tailscale, javne adrese,
	// domene i tunel; tunel i javnih kažu ima li ih među ostalima
	lan, ostali   []kandidat
	tunel, javnih bool
	imaAdresa     bool
	// neodgovorili su LAN adrese koje nisu odgovorile (razlog puta), bezWeba
	// da je druga strana odbila vezu sesije, a zadnjiWeb da je zadnja veza
	// bila iz sesije
	neodgovorili       []kandidat
	bezWeba, zadnjiWeb bool
	// razlogWeba je razlog puta od WEB synca (priprema, pauza), a uPripremi
	// da se WEB sync s tim računalom priprema u pozadini
	razlogWeba razmjena.RazlogPuta
	uPripremi  bool
	greska     error // zadnja greška koraka s adresama
}

// noviKrug razvrsta adrese računala po koracima, redom iz adreseZaSpajanje
func (s *Service) noviKrug(ctx context.Context, peer *Peer, expect ed25519.PublicKey) *krug {
	k := &krug{s: s, peer: peer, expect: expect}
	for _, kk := range s.adreseZaSpajanje(ctx, peer) {
		k.dodaj(kk)
	}
	k.lan = ograniciPoPutu(k.lan, func(kandidat) razmjena.Put { return razmjena.PutLAN })
	k.ostali = ograniciPoPutu(k.ostali, func(kk kandidat) razmjena.Put { return kk.put })
	k.imaAdresa = len(k.lan)+len(k.ostali) > 0
	return k
}

// ograniciPoPutu ostavi prvih najviseAdresaPuta kandidata svakog puta
// (put kaže kojem putu kandidat pripada); isto vrijedi za krug i za
// Provjeri vezu s računalom
func ograniciPoPutu[T any](kandidati []T, put func(T) razmjena.Put) []T {
	broj := map[razmjena.Put]int{}
	var out []T
	for _, kk := range kandidati {
		if p := put(kk); broj[p] < najviseAdresaPuta {
			broj[p]++
			out = append(out, kk)
		}
	}
	return out
}

// dodaj stavi adresu u korak: iz pronalaženja i privatne su LAN, ostalo
// ide zajedno iza WEB synca
func (k *krug) dodaj(kk kandidat) {
	if kk.put == razmjena.PutLAN || kk.put == razmjena.PutAdresa && privatnaAdresa(kk.adresa) {
		k.lan = append(k.lan, kk)
		return
	}
	k.ostali = append(k.ostali, kk)
	k.tunel = k.tunel || kk.put == razmjena.PutKrozTunel
	k.javnih = k.javnih || kk.put == razmjena.PutAdresa
}

// spoji je veza ovog kruga, korak po korak. LAN adrese koje nisu
// odgovorile ne zovu se ponovno ni kad se krug spaja još jednom. Greška je
// zadnja greška koraka koji je imao adrese.
func (k *krug) spoji(ctx context.Context) (*razmjena.Conn, kandidat, error) {
	c, kk, err := k.s.utrka(ctx, k.peer, k.expect, k.lan, razmakLAN, rokLAN)
	if err == nil {
		return c, kk, nil
	}
	k.zapamtiGresku(err)
	k.neodgovorili, k.lan = append(k.neodgovorili, k.lan...), nil
	if c, kk, ok := k.sesijaWeba(ctx); ok {
		return c, kk, nil
	}
	k.pripremaPrije()
	c, kk, err = k.s.utrka(ctx, k.peer, k.expect, k.ostali, 0, rokOstalih)
	k.pripremaPoslije(kk, err)
	k.zapamtiGresku(err)
	return c, kk, k.greska
}

// zapamtiGresku pamti grešku koraka (nil uz uspjeh); korak bez ijedne
// adrese ne briše grešku ranijeg koraka
func (k *krug) zapamtiGresku(err error) {
	if !errors.Is(err, errBezKandidata) || k.greska == nil {
		k.greska = err
	}
}

// sesijaWeba otvori razgovor na živoj WEB sync sesiji s računalom; sesija
// koja ne odgovori na ping (zivaSesija) ne vrijedi
func (k *krug) sesijaWeba(ctx context.Context) (*razmjena.Conn, kandidat, bool) {
	ws := k.s.web.Load()
	if ws == nil || k.bezWeba {
		return nil, kandidat{}, false
	}
	ses := ws.w.Sesija(k.expect)
	if ses == nil || !k.zivaSesija(ctx, ws, ses) {
		return nil, kandidat{}, false
	}
	c, err := ses.Razgovor(ctx)
	if err != nil {
		return nil, kandidat{}, false
	}
	k.zadnjiWeb = true
	return c, kandidat{adresa: c.RemoteAddr().String(), put: c.Put}, true
}

// zivaSesija javlja odgovara li sesija na ping u roku (rokPinga, a najmanje
// 3 RTT-a). Sesija koja ne odgovori zatvara se u pozadini, a krug je više
// ne zove; kad je prekinut cijeli krug, ping ne govori ništa o sesiji, pa
// ona ostaje.
func (k *krug) zivaSesija(ctx context.Context, ws *webSyncServisa, ses *razmjena.SesijaWeb) bool {
	pingCtx, otkazi := context.WithTimeout(ctx, max(rokPinga, 3*ses.RTT))
	defer otkazi()
	_, err := ses.Ping(pingCtx)
	switch {
	case err == nil:
		return true
	case ctx.Err() == nil:
		k.bezWeba = true
		log.Printf("povezivost: WEB sync sesija s %s ne odgovara (%v), pa se zatvara", label(*k.peer), err)
		ws.uPozadini(func() { ses.Zatvori(0) })
	}
	return false
}

// bez makne vezu koju je druga strana odbila jer joj je Tailscale
// isključen, i sve Tailscale adrese, pa se krug spaja ostalim putovima
func (k *krug) bez(kk kandidat) {
	k.bezWeba = k.bezWeba || k.zadnjiWeb
	k.zadnjiWeb = false
	k.lan = bezKandidata(k.lan, kk)
	k.ostali = bezKandidata(k.ostali, kk)
}

// bezKandidata su kandidati bez kk i bez Tailscale adresa
func bezKandidata(kandidati []kandidat, kk kandidat) []kandidat {
	var out []kandidat
	for _, c := range kandidati {
		if c != kk && c.put != razmjena.PutTailscale {
			out = append(out, c)
		}
	}
	return out
}

// razlog je razlog puta za bilješku: od WEB synca (priprema, pauza), a
// inače da se računalo ne javlja u lokalnoj mreži, kad je tamo imalo
// adresu
func (k *krug) razlog() razmjena.RazlogPuta {
	if k.razlogWeba != "" {
		return k.razlogWeba
	}
	return razlogNakon(k.neodgovorili)
}

// ishodPoziva je ishod spajanja na jednog kandidata
type ishodPoziva struct {
	conn *razmjena.Conn
	k    kandidat
	err  error
}

// utrka zove kandidate usporedno, svakog razmak kasnije od prethodnog, svakog
// s rokom rok. Prva veza pobjeđuje; ostali pozivi se prekidaju, a veze koje
// se ipak uspostave zatvaraju se prije razgovora. Bez pobjednika greška je
// zadnjeg ishoda.
func (s *Service) utrka(ctx context.Context, peer *Peer, expect ed25519.PublicKey, kandidati []kandidat, razmak, rok time.Duration) (*razmjena.Conn, kandidat, error) {
	if len(kandidati) == 0 {
		return nil, kandidat{}, errBezKandidata
	}
	ctx, stani := context.WithCancelCause(ctx)
	ishodi := make(chan ishodPoziva, len(kandidati))
	for i, k := range kandidati {
		go func() { ishodi <- s.nazoviNakon(ctx, time.Duration(i)*razmak, peer, k, expect, rok) }()
	}
	pobjednik, ostalo := prviUspjeh(ishodi, len(kandidati))
	stani(errDrugaAdresa)
	go zatvoriOstale(ishodi, ostalo)
	return pobjednik.conn, pobjednik.k, pobjednik.err
}

// nazoviNakon nazove kandidata nakon cekaj; prekinuta utrka ga ne zove
func (s *Service) nazoviNakon(ctx context.Context, cekaj time.Duration, peer *Peer, k kandidat, expect ed25519.PublicKey, rok time.Duration) ishodPoziva {
	t := time.NewTimer(cekaj)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ishodPoziva{k: k, err: context.Cause(ctx)}
	case <-t.C:
	}
	c, err := s.nazovi(ctx, peer, k, expect, rok)
	return ishodPoziva{conn: c, k: k, err: err}
}

// prviUspjeh čita ishode do prvog uspjeha i vraća ga, s brojem ishoda koji
// još nisu pročitani; bez uspjeha vraća zadnji ishod
func prviUspjeh(ishodi <-chan ishodPoziva, n int) (ishodPoziva, int) {
	var zadnji ishodPoziva
	for i := range n {
		if zadnji = <-ishodi; zadnji.err == nil {
			return zadnji, n - i - 1
		}
	}
	return zadnji, 0
}

// zatvoriOstale zatvori veze iz ishoda koji su stigli nakon pobjednika
func zatvoriOstale(ishodi <-chan ishodPoziva, n int) {
	for range n {
		if r := <-ishodi; r.conn != nil {
			r.conn.Close()
		}
	}
}
