package web

import (
	"crypto/ed25519"
	"net/http"
	"net/netip"
	"sync"

	"gocop/internal/razmjena"
)

// Tunel razmjene je ruta bez prijave koja drži vezu dugo, pa je ograđen prije
// nadogradnje na WebSocket. Dok druga strana ne dokaže ključ člana (TLS
// unutar WebSocketa), veza je stranac: najviše 2 po klijentu (IPv4 adresa,
// IPv6 mreža /64), 4 po mreži IPv4 /24 i 2 po mreži IPv6 /48 (jedan
// pretplatnik dobiva čitavu takvu mrežu) i 24 ukupno, a klijenti s kojih je
// član u zadnja 24 sata dokazao ključ imaju još 8 svojih mjesta. Stranac
// mjesto drži samo dok traje rukovanje: razmjena vezu koja ne pošalje prvu
// poruku rukovanja zatvori nakon 3 s, a rukovanje nakon 5 s
// (razmjena.NovaOgradaTunela, s 32 mjesta, koliko ih ovdje ima stranaca i
// poznatih). Kad ključ prođe, veza prelazi na mjesto
// člana: najviše 2 po ključu (razmjena i ručna sinkronizacija) i 32 ukupno,
// pa stranci ne zauzimaju mjesta razmjena koje traju. Višak dobije 503 s
// Retry-After; stariji čvor to vidi kao grešku spajanja i pokuša opet
// sljedećim krugom.
var granicaTunela = granicaOgrade{stranaca: 24, poKlijentu: 2, poMrezi: 4, poMrezi6: 2, poznatih: 8, clanova: 32, poKljucu: 2}

const ponoviTunelZa = "30"

// granicaOgrade su granice web ograde: stranci po klijentu, mreži (IPv4 i
// IPv6 posebno) i ukupno, mjesta za poznate klijente, te članovi po ključu i
// ukupno
type granicaOgrade struct {
	stranaca, poKlijentu, poMrezi, poMrezi6, poznatih int
	clanova, poKljucu                                 int
}

// naMrezi je koliko stranaca smije biti s mreže
func (g granicaOgrade) naMrezi(mreza netip.Addr) int {
	if mreza.Is6() {
		return g.poMrezi6
	}
	return g.poMrezi
}

// kljucKlijentaTunela: IPv4 adresa, a za IPv6 njezina mreža /48; po njemu
// sastajalište ograđuje svoja rukovanja
func kljucKlijentaTunela(a netip.Addr) netip.Addr {
	if a.Is6() && !a.Is4In6() {
		if p, err := a.Prefix(48); err == nil {
			return p.Addr()
		}
	}
	return a
}

type ogradaTunela struct {
	mu                 sync.Mutex
	g                  granicaOgrade
	stranaca, poznatih int
	clanova            int
	poKlijentu         map[netip.Addr]int
	poMrezi            map[netip.Addr]int
	poKljucu           map[string]int
	poznate            *razmjena.PoznateAdrese
}

func novaOgradaTunela(g granicaOgrade) *ogradaTunela {
	return &ogradaTunela{g: g, poKlijentu: map[netip.Addr]int{}, poMrezi: map[netip.Addr]int{},
		poKljucu: map[string]int{}, poznate: razmjena.NovePoznateAdrese()}
}

// mjestoTunela je mjesto jedne veze: stranca dok ključ nije dokazan, zatim
// člana (kljuc). Mijenja se pod bravom ograde.
type mjestoTunela struct {
	o              *ogradaTunela
	klijent, mreza netip.Addr
	pricuva        bool   // stranac je na mjestu za poznate klijente
	kljuc          string // ključ člana; prazno dok je stranac
	pusteno        bool
}

// uzmiStranca daje mjesto stranca klijentu s adrese a; false kad ga nema
func (o *ogradaTunela) uzmiStranca(a netip.Addr) (*mjestoTunela, bool) {
	m := &mjestoTunela{o: o, klijent: razmjena.KljucAdrese(a), mreza: razmjena.MrezaAdrese(a)}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.poKlijentu[m.klijent] >= o.g.poKlijentu {
		return nil, false
	}
	switch {
	case o.stranaca < o.g.stranaca && o.poMrezi[m.mreza] < o.g.naMrezi(m.mreza):
		o.stranaca++
	case o.poznatih < o.g.poznatih && o.poznate.Zna(m.klijent):
		o.poznatih++
		m.pricuva = true
	default:
		return nil, false
	}
	o.poKlijentu[m.klijent]++
	o.poMrezi[m.mreza]++
	return m, true
}

// pustiStranca vraća mjesto stranca; pod bravom ograde
func (m *mjestoTunela) pustiStranca() {
	o := m.o
	if m.pricuva {
		o.poznatih--
	} else {
		o.stranaca--
	}
	smanji(o.poKlijentu, m.klijent)
	smanji(o.poMrezi, m.mreza)
}

// Clan: druga strana je dokazala ključ člana, pa veza prelazi na mjesto
// člana, a klijent postaje poznat. False kad za taj ključ mjesta nema; veza
// tada ostaje stranac dok je razmjena ili sastajalište ne zatvori, što ne
// traje dulje od 2 s.
func (m *mjestoTunela) Clan(kljuc ed25519.PublicKey) bool {
	k := razmjena.PublicKeyString(kljuc)
	o := m.o
	o.mu.Lock()
	defer o.mu.Unlock()
	if m.pusteno || m.kljuc != "" {
		return !m.pusteno && m.kljuc == k
	}
	if o.clanova >= o.g.clanova || o.poKljucu[k] >= o.g.poKljucu {
		return false
	}
	m.pustiStranca()
	o.clanova++
	o.poKljucu[k]++
	m.kljuc = k
	o.poznate.Zapamti(m.klijent, kljuc)
	return true
}

// pusti vraća mjesto kad veza završi
func (m *mjestoTunela) pusti() {
	o := m.o
	o.mu.Lock()
	defer o.mu.Unlock()
	if m.pusteno {
		return
	}
	m.pusteno = true
	if m.kljuc == "" {
		m.pustiStranca()
		return
	}
	o.clanova--
	smanji(o.poKljucu, m.kljuc)
}

func smanji[K comparable](brojac map[K]int, k K) {
	if brojac[k]--; brojac[k] <= 0 {
		delete(brojac, k)
	}
}

// ograditiTunel omata rukovatelja tunela. websocket.Server.ServeHTTP vraća
// tek kad razmjena završi, pa se mjesto pušta na izlasku.
func ograditiTunel(next http.Handler) http.Handler {
	return novaOgradaTunela(granicaTunela).omotaj(next, "Čvor već drži najviše veza razmjene kroz tunel; pokušajte kasnije.")
}

// omotaj pušta zahtjev dok ima mjesta, a višku odgovori porukom i 503.
// Mjesto ide uz zahtjev, da ga razmjena ili sastajalište prebace na mjesto
// člana kad ključ prođe.
func (o *ogradaTunela) omotaj(next http.Handler, poruka string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m, ok := o.uzmiStranca(klijentIz(r).Adresa)
		if !ok {
			w.Header().Set("Retry-After", ponoviTunelZa)
			http.Error(w, poruka, http.StatusServiceUnavailable)
			return
		}
		defer m.pusti()
		next.ServeHTTP(w, r.WithContext(razmjena.SMjestom(r.Context(), m)))
	})
}
