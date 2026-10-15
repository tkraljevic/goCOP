package peers

import "net/netip"

// Testovi ne smiju ovisiti o tome ima li računalo na kojem rade Tailscale:
// čvor u testu nema ga dok mu test ne zada adrese (ZadajTailscale)
func init() {
	zadaneTailscaleAdrese = func() []netip.Addr { return nil }
}

// ZadajTailscale daje čvoru u testu izmišljene Tailscale adrese; bez
// adresa čvor nema Tailscale
func (s *Service) ZadajTailscale(adrese ...netip.Addr) {
	s.tailscaleAdrese = func() []netip.Addr { return adrese }
}

// SuziDeltu privremeno smanjuje ogradu veličine delte, za test skraćenih
// razgovora; vraća funkciju koja je vraća
func SuziDeltu(n int) func() {
	prije := najviseBajtovaDelte
	najviseBajtovaDelte = n
	return func() { najviseBajtovaDelte = prije }
}
