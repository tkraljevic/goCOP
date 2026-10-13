package main

import "gocop/internal/config"

// sklopkaTailscalea je dio web poslužitelja koji odlučuje vrijedi li zahtjev
// na Tailscale adresu ovog računala kao iz lokalne mreže (web.Server)
type sklopkaTailscalea interface{ SetTailscaleLokalno(bool) }

// postaviTailscale predaje poslužitelju [web] tailscale_lokalno iz gocop.toml
func postaviTailscale(s sklopkaTailscalea, cfg config.Config) {
	s.SetTailscaleLokalno(cfg.Web.TailscaleLokalno)
}
