package web

// Zahtjev preko Tailscalea (Odluke 12. 10. 2026.). Tailscale imaju samo neki
// čvorovi. Na njima zahtjev koji stigne izravno na Tailscale adresu ovog
// računala vrijedi kao iz lokalne mreže: do te adrese dolaze samo uređaji iz
// iste Tailscale mreže. Odlučuje adresa na koju je veza stigla, ne adresa
// pošiljatelja, jer 100.x s druge kartice može biti bilo tko kod mobilnog
// operatera (CGNAT). Zahtjev kroz posrednika ostaje izvana (Klijent.Izvana).
// Uz [web] tailscale_lokalno = false zahtjev na Tailscale adresu ovog
// računala vrijedi kao izvana.
//
// Na računalu bez Tailscalea popis vlastitih Tailscale adresa je prazan, pa
// sve radi kao prije. Kartice se čitaju tek kad veza stigne na adresu iz
// Tailscaleova raspona, što se bez Tailscalea događa samo uz drugi VPN s
// takvom adresom.

import (
	"net"
	"net/http"
	"net/netip"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"gocop/internal/tailscale"
)

// tailscaleOsvjezi je koliko dugo vrijedi pročitan popis vlastitih adresa
const tailscaleOsvjezi = 30 * time.Second

// tailscaleVeze prepoznaje vezu koja je stigla na Tailscale adresu ovog
// računala i pamti popis tih adresa kratko, jer se Tailscale može upaliti ili
// ugasiti dok čvor radi
type tailscaleVeze struct {
	iskljuceno atomic.Bool // [web] tailscale_lokalno = false
	citaj      func() []netip.Addr
	sad        func() time.Time

	mu        sync.Mutex
	adrese    []netip.Addr
	procitano time.Time
}

func noviTailscale(citaj func() []netip.Addr) *tailscaleVeze {
	return &tailscaleVeze{citaj: citaj, sad: time.Now}
}

// vlastite vraća Tailscale adrese ovog računala, pročitane najviše prije 30 s
func (t *tailscaleVeze) vlastite() []netip.Addr {
	t.mu.Lock()
	defer t.mu.Unlock()
	if sad := t.sad(); sad.Sub(t.procitano) >= tailscaleOsvjezi {
		t.adrese, t.procitano = t.citaj(), sad
	}
	return t.adrese
}

// stigloNa javlja je li veza zahtjeva stigla na Tailscale adresu ovog
// računala; je li takav zahtjev lokalan, odlučuje sklopka (Klijent.Izvana)
func (t *tailscaleVeze) stigloNa(r *http.Request) bool {
	a := lokalnaAdresa(r)
	return tailscale.JeTailscale(a) && slices.Contains(t.vlastite(), a)
}

// lokalnaAdresa je adresa ovog računala na koju je stigla TCP veza zahtjeva
func lokalnaAdresa(r *http.Request) netip.Addr {
	if a, ok := r.Context().Value(http.LocalAddrContextKey).(*net.TCPAddr); ok {
		return a.AddrPort().Addr().Unmap()
	}
	return netip.Addr{}
}

// SetTailscaleLokalno odlučuje je li zahtjev na Tailscale adresu ovog
// računala lokalan (uključeno) ili izvana (gocop.toml [web]
// tailscale_lokalno). Vrijedi odmah, pa se smije zvati i dok poslužitelj radi.
func (s *Server) SetTailscaleLokalno(ukljuceno bool) {
	s.tailscaleIliZadani().iskljuceno.Store(!ukljuceno)
}

// tailscaleIliZadani vraća prepoznavanje Tailscalea; bez postavke je uključeno
func (s *Server) tailscaleIliZadani() *tailscaleVeze {
	if s.tailscale == nil {
		s.tailscale = noviTailscale(tailscale.VlastiteAdrese)
	}
	return s.tailscale
}
