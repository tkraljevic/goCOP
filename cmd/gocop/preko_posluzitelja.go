package main

// Računalo do kojeg ovo nema puta ipak dobiva podatke: oba se razmjenjuju sa
// stalnim čvorom, pa podaci idu preko njega u dva kruga, do 10 min kasnije
// (plan WEB synca, putovi na pločici). Pločica tada uz to računalo kaže
// „SERVER sync, preko cop.primjer.hr”. Ime je domena iz adrese tunela
// stalnog čvora, jer pločicu vidi svaki prijavljeni korisnik, a IP adresa
// ne ide na nju.

import (
	"net/netip"
	"net/url"
	"strings"

	"gocop/internal/peers"
	"gocop/internal/razmjena"
)

// bezImenaPosluzitelja stoji kad stalni čvor nema adresu tunela s domenom
const bezImenaPosluzitelja = "poslužitelja"

// prekoPosluzitelja je ime stalnog čvora koji je na mreži, za natpis uz
// računala do kojih ovo nema puta; prazno kad takvog čvora nema
func prekoPosluzitelja(cvorovi []peers.PeerStatus) string {
	for _, ps := range cvorovi {
		if !ps.IsBootstrap || ps.Reachability != "online" {
			continue
		}
		for _, a := range ps.Addresses {
			if ime := domenaTunela(a); ime != "" {
				return ime
			}
		}
		return bezImenaPosluzitelja
	}
	return ""
}

// domenaTunela je domena iz adrese tunela (https://cop.primjer.hr); prazno
// za druge adrese i za tunel na IP adresi
func domenaTunela(a string) string {
	a = strings.TrimSpace(a)
	if !razmjena.JeAdresaTunela(a) {
		return ""
	}
	u, err := url.Parse(a)
	if err != nil {
		return ""
	}
	if _, err := netip.ParseAddr(u.Hostname()); err == nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// prekoPosluziteljaZa: natpis ide uz člana mreže koji nije na mreži i nije
// sam stalni čvor. S opozvanim računalom i s onim bez valjanog članstva ne
// razmjenjuje nitko, pa ni stalni čvor.
func prekoPosluziteljaZa(ps peers.PeerStatus, preko string) string {
	if ps.IsBootstrap || ps.Reachability == "online" || !ps.Member {
		return ""
	}
	return preko
}
