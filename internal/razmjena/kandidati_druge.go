package razmjena

// Kandidati druge strane (plan WEB synca, F3a, „Kandidati druge strane”):
// prije probijanja ovo računalo za svaki potpisani kandidat druge strane
// pita jezgru kojim bi putem UDP do njega izašao, povezanom utičnicom koja
// ništa ne šalje (izlaznaAdresa, kao za vlastite kandidate). Kandidat se
// izbacuje kad promet do njega ide preko vlastite Tailscale adrese: ruta
// podmreže kroz Tailscale (druga strana u 192.168.1.0/24, a ta mreža ide
// kroz tailscale0) i izlazni čvor (javna adresa druge strane ide kroz
// Tailscale). Tada bi WEB sync potiho išao Tailscaleom, a pločica bi pisala
// WEB sync. Izbacuje se i kandidat do kojeg nema rute. Vlastita Tailscale
// adresa je adresa na Tailscaleovoj kartici (tailscale.VlastiteAdrese), pa
// pravilo vrijedi i za karticu i za adresu.
//
// Put QUIC veze određuje se istim pitanjem: slušalica je na 0.0.0.0, pa
// lokalna adresa veze ne kaže kuda promet ide. QUIC kojem je izlazna adresa
// vlastita Tailscale adresa vodi se kao tailscale, inače kao probijeno.
// kandidati.go iz F1 se ne dira.

import (
	"fmt"
	"net/netip"
	"slices"
)

// kandidatiDruge su potpisani kandidati druge strane do kojih promet ne
// ide kroz Tailscale i do kojih ima rute; izbačeni su razlozi za stranicu
// Sinkronizacija. Oblik adrese i dvojnike i dalje provjerava
// ciljeviUdaraca.
func kandidatiDruge(kandidati []KandidatPoruke, ts []netip.Addr, izlaz izlazFunc) (ostali []KandidatPoruke, izbaceni []string) {
	for _, k := range kandidati {
		if razlog := izbaciKandidat(k, ts, izlaz); razlog != "" {
			izbaceni = append(izbaceni, razlog)
			continue
		}
		ostali = append(ostali, k)
	}
	return ostali, izbaceni
}

// izbaciKandidat je razlog zašto se kandidat ne udara; prazno kad se udara
func izbaciKandidat(k KandidatPoruke, ts []netip.Addr, izlaz izlazFunc) string {
	a, ok := adresaKandidata(k.Adresa)
	if !ok {
		return fmt.Sprintf("%q nije adresa na koju se udara", k.Adresa)
	}
	lok, err := izlaz(a)
	switch {
	case err != nil:
		return fmt.Sprintf("%s: nema rute (%v)", a, err)
	case slices.Contains(ts, lok):
		return fmt.Sprintf("%s: promet ide kroz Tailscale (%s)", a, lok)
	}
	return ""
}

// putQUIC je put QUIC veze prema adresi druge strane: tailscale kad promet
// izlazi s vlastite Tailscale adrese, inače probijeno
func putQUIC(druga netip.AddrPort, ts []netip.Addr, izlaz izlazFunc) Put {
	if lok, err := izlaz(druga); err == nil && slices.Contains(ts, lok) {
		return PutTailscale
	}
	return PutProbijeno
}
