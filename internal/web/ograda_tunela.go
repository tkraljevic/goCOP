package web

import (
	"net/http"
	"net/netip"
	"sync"
)

// Tunel razmjene je jedina ruta bez prijave koja drži vezu dugo, pa je
// ograđen prije nadogradnje na WebSocket: najviše 2 veze po klijentu (po
// jedna razmjena i ručna sinkronizacija; IPv6 po mreži /48, jer jedan
// pretplatnik dobiva čitavu takvu mrežu) i 32 ukupno. Ukupna granica je
// široka jer veze koje šute nakon nadogradnje i dalje drže mjesto dok ih
// razmjena ne odbaci (5 s, razmjena.NovaOgradaTunela); uska bi se dala
// popuniti s nekoliko adresa. Višak dobije 503 s Retry-After; stariji čvor
// to vidi kao grešku spajanja i pokuša opet sljedećim krugom.
const (
	najviseTunela           = 32
	najviseTunelaPoKlijentu = 2
	ponoviTunelZa           = "30"
)

// kljucKlijentaTunela: IPv4 adresa, a za IPv6 njezina mreža /48
func kljucKlijentaTunela(a netip.Addr) netip.Addr {
	if a.Is6() && !a.Is4In6() {
		if p, err := a.Prefix(48); err == nil {
			return p.Addr()
		}
	}
	return a
}

type ogradaTunela struct {
	mu          sync.Mutex
	ukupno      int
	poKlijentu  map[netip.Addr]int
	najvise     int
	poKlijentuN int
}

func novaOgradaTunela(najvise, poKlijentu int) *ogradaTunela {
	return &ogradaTunela{najvise: najvise, poKlijentuN: poKlijentu, poKlijentu: map[netip.Addr]int{}}
}

func (o *ogradaTunela) uzmi(a netip.Addr) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.ukupno >= o.najvise || o.poKlijentu[a] >= o.poKlijentuN {
		return false
	}
	o.ukupno++
	o.poKlijentu[a]++
	return true
}

func (o *ogradaTunela) pusti(a netip.Addr) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.ukupno--
	if o.poKlijentu[a]--; o.poKlijentu[a] <= 0 {
		delete(o.poKlijentu, a)
	}
}

// ograditiTunel omata rukovatelja tunela. websocket.Server.ServeHTTP vraća
// tek kad razmjena završi, pa se mjesto pušta na izlasku.
func ograditiTunel(next http.Handler) http.Handler {
	return novaOgradaTunela(najviseTunela, najviseTunelaPoKlijentu).omotaj(next)
}

func (o *ogradaTunela) omotaj(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := kljucKlijentaTunela(klijentIz(r).Adresa)
		if !o.uzmi(a) {
			w.Header().Set("Retry-After", ponoviTunelZa)
			http.Error(w, "Čvor već drži najviše veza razmjene kroz tunel; pokušajte kasnije.", http.StatusServiceUnavailable)
			return
		}
		defer o.pusti(a)
		next.ServeHTTP(w, r)
	})
}
