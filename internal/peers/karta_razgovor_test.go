package peers

// Karta bez interneta u razgovoru, bez druge razmjene: ograda dijelova po
// putu, ista lokalna mreža, mreže sučelja i ponavljanje razgovora zbog
// karte. Testovi karte s dva čvora su u karta_razgovor_mreza_test.go.

import (
	"context"
	"net"
	"slices"
	"testing"

	"gocop/internal/models"
	"gocop/internal/razmjena"
)

// razgovorZa je razgovor čvora s drugom stranom koja javlja oznake njihove,
// na vezi zadanog puta, bez mreže
func razgovorZa(s *Service, put razmjena.Put, njihove ...string) *razgovorSCvorom {
	return &razgovorSCvorom{s: s, c: &razmjena.Conn{Put: put}, myWants: Wants{All: true},
		moja:   frontierMsg{Znanje: razmjena.Znanje{Mogucnosti: s.mojeMogucnosti()}},
		theirs: frontierMsg{Wants: &Wants{All: true}, Znanje: razmjena.Znanje{Mogucnosti: njihove}}}
}

// Ograda dijelova po putu: lokalna mreža i privatna adresa 48 MiB, javna
// adresa i Tailscale 16 MiB, tunel, probijanje i nepoznat put 8 MiB.
// Dolazna veza s adrese ovog računala (posrednik na istom računalu) nije
// lokalna mreža, a odlazna na nju jest. Ograda ne gleda sučelja ovog
// računala: privatna adresa izvan njihovih mreža i dalje dobiva 48 MiB.
func TestOgradaDijelovaPoPutu(t *testing.T) {
	suceljaRacunala(t, []string{"192.168.1.5/24"})
	s := rubniCvor(t, "laptop-baranja")
	kljuc := rubniCvor(t, "cop-osijek").node.key
	privatna := vezaIzMreze(t, s, kljuc, "10.0.0.1", "10.0.0.2:4710")
	javna := vezaIzMreze(t, s, kljuc, "10.0.0.1", "203.0.113.5:4710")
	ovdje := vezaIzMreze(t, s, kljuc, "10.0.0.1", "127.0.0.1:4710")
	ovdje6 := vezaIzMreze(t, s, kljuc, "10.0.0.1", "[::ffff:127.0.0.1]:4710")
	for _, c := range []struct {
		veza      *razmjena.Conn
		put       razmjena.Put
		zvao      bool
		ograda    int64
		uLokalnoj bool
		opisVeze  string
	}{
		{privatna, razmjena.PutLAN, false, 48 << 20, true, "lan"},
		{privatna, razmjena.PutAdresa, true, 48 << 20, true, "privatna adresa"},
		{privatna, razmjena.PutAdresa, false, 48 << 20, true, "dolazna s privatne adrese"},
		{ovdje, razmjena.PutAdresa, true, 48 << 20, true, "na ovo računalo"},
		{ovdje, razmjena.PutAdresa, false, 16 << 20, false, "dolazna s ovog računala"},
		{ovdje6, razmjena.PutAdresa, false, 16 << 20, false, "dolazna s ovog računala (IPv6)"},
		{javna, razmjena.PutAdresa, true, 16 << 20, false, "javna adresa"},
		{privatna, razmjena.PutTailscale, true, 16 << 20, false, "tailscale"},
		{privatna, razmjena.PutKrozTunel, true, 8 << 20, false, "tunel"},
		{privatna, razmjena.PutProbijeno, true, 8 << 20, false, "probijeno"},
		{privatna, "", true, 8 << 20, false, "nepoznat put"},
	} {
		c.veza.Put = c.put
		if got := zadaneOgrade.poPutu(vezaRazgovora{c: c.veza, zvao: c.zvao}); got != c.ograda || putULokalnojMrezi(c.veza, c.zvao) != c.uLokalnoj {
			t.Errorf("%s: ograda %d, u lokalnoj mreži %v", c.opisVeze, got, putULokalnojMrezi(c.veza, c.zvao))
		}
	}
	if adresaOvogRacunala("localhost:4710") || !adresaOvogRacunala("::1") || adresaOvogRacunala("10.0.0.2") {
		t.Error("adresa ovog računala")
	}
}

// suceljaRacunala podmetne sučelja ovog računala za trajanje testa: svako
// sučelje je popis adresa s maskom (192.168.1.5/24)
func suceljaRacunala(t *testing.T, sucelja ...[]string) {
	t.Helper()
	var mreze [][]*net.IPNet
	for _, adrese := range sucelja {
		var m []*net.IPNet
		for _, a := range adrese {
			ip, n, err := net.ParseCIDR(a)
			if err != nil {
				t.Fatal(err)
			}
			n.IP = ip
			m = append(m, n)
		}
		mreze = append(mreze, m)
	}
	staro := mrezeSucelja
	mrezeSucelja = func() [][]*net.IPNet { return mreze }
	t.Cleanup(func() { mrezeSucelja = staro })
}

// U obrani karta ide samo s računalom u istoj lokalnoj mreži (Odluke
// (30. 10. 2026.)), u oba smjera po sučelju koje nosi lokalnu adresu veze.
// Uz sučelja 192.168.1.5/24 i 10.20.0.5/16: s 192.168.1.5 na 10.1.2.3 nije,
// a na 192.168.1.20 jest; u 10.20.0.0/16 jest samo s 10.20.0.5, a ne s
// 192.168.1.5 (ruta ide drugim sučeljem); pronalaženje jest. Adresa ovog
// računala, Tailscale, link-local, tunel, adresa koja nije IP i sučelja koja
// se ne daju pročitati nisu.
func TestIstaLokalnaMreza(t *testing.T) {
	suceljaRacunala(t, []string{"192.168.1.5/24", "fe80::1/64"}, []string{"10.20.0.5/16"}, []string{"127.0.0.1/8"}, []string{"100.101.102.103/32"})
	s := rubniCvor(t, "laptop-baranja")
	kljuc := rubniCvor(t, "cop-osijek").node.key
	for _, c := range []struct {
		lokalna, udaljena string
		put               razmjena.Put
		ista              bool
		opis              string
	}{
		{"192.168.1.5", "10.1.2.3:4710", razmjena.PutAdresa, false, "s 192.168.1.5 na 10.1.2.3"},
		{"192.168.1.5", "192.168.1.20:4710", razmjena.PutAdresa, true, "s 192.168.1.5 na 192.168.1.20"},
		{"192.168.1.5", "[::ffff:192.168.1.20]:4710", razmjena.PutAdresa, true, "s 192.168.1.5 na 192.168.1.20 (IPv6)"},
		{"::ffff:192.168.1.5", "192.168.1.20:4710", razmjena.PutAdresa, true, "s 192.168.1.5 (IPv6) na 192.168.1.20"},
		{"10.20.0.5", "10.20.3.4:4710", razmjena.PutAdresa, true, "u mreži drugog sučelja s njegove adrese"},
		{"192.168.1.5", "10.20.3.4:4710", razmjena.PutAdresa, false, "u mrežu drugog sučelja s adrese prvog"},
		{"192.168.7.1", "192.168.7.2:4710", razmjena.PutAdresa, false, "s adrese koja nije ni na jednom sučelju"},
		{"fe80::1", "[fe80::2]:4710", razmjena.PutAdresa, false, "link-local"},
		{"127.0.0.1", "127.0.0.1:4710", razmjena.PutAdresa, false, "s ovog računala na ovo računalo"},
		{"100.101.102.103", "192.168.1.20:4710", razmjena.PutAdresa, false, "s Tailscale adrese"},
		{"192.168.1.5", "100.101.102.104:4710", razmjena.PutAdresa, false, "na Tailscale adresu"},
		{"192.168.1.5", "p2p:x", razmjena.PutAdresa, false, "adresa koja nije IP"},
		{"10.0.0.1", "10.0.0.2:4710", razmjena.PutLAN, true, "pronalaženje"},
		{"192.168.1.5", "192.168.1.20:4710", razmjena.PutTailscale, false, "tailscale"},
		{"192.168.1.5", "192.168.1.20:4710", razmjena.PutKrozTunel, false, "tunel"},
	} {
		v := vezaIzMreze(t, s, kljuc, c.lokalna, c.udaljena)
		v.Put = c.put
		if got := istaLokalnaMreza(v); got != c.ista {
			t.Errorf("%s: ista lokalna mreža %v", c.opis, got)
		}
	}
	v := vezaIzMreze(t, s, kljuc, "192.168.1.5", "192.168.1.20:4710")
	v.Put = razmjena.PutAdresa
	mrezeSucelja = func() [][]*net.IPNet { return nil }
	if istaLokalnaMreza(v) {
		t.Error("bez sučelja veza je u istoj lokalnoj mreži")
	}
}

// Sučelja se čitaju iz sustava filtrom pronalaženja: petlja, koju svako
// računalo ima, nije lokalna mreža
func TestMrezeSucelja(t *testing.T) {
	petlja := net.ParseIP("127.0.0.1")
	if slices.ContainsFunc(mrezeSucelja(), func(m []*net.IPNet) bool {
		return slices.ContainsFunc(m, func(n *net.IPNet) bool { return n.Contains(petlja) })
	}) {
		t.Error("petlja je među mrežama lokalnih sučelja")
	}
}

// Ponavljanje zbog karte: najviše osam dodatnih razgovora u jednom krugu,
// a pun razgovor ponavlja se i dalje; greška krug prekida
func TestPonavljanjeZbogKarteImaGranicu(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	s.PostaviKarte(t.TempDir(), func() *models.VektorskaKarta { return nil }, nil) // prijavi kartu
	zbogKarte, ponovljeno := 0, 0
	for range 20 {
		s.josKoraka.Store("cop-osijek", true)
		if !s.dalje("cop-osijek", 0, 0, nil, &zbogKarte, new(string)) {
			break
		}
		ponovljeno++
	}
	if ponovljeno != najviseRazgovoraZaKartu {
		t.Errorf("zbog karte ponovljeno %d puta", ponovljeno)
	}
	s.josKoraka.Store("cop-osijek", true)
	if !s.dalje("cop-osijek", NajviseVerzijaPoRazmjeni, 0, nil, &zbogKarte, new(string)) {
		t.Error("pun razgovor se ponavlja i nakon ograde karte")
	}
	if _, ima := s.josKoraka.Load("cop-osijek"); ima {
		t.Error("zapis o karti nije potrošen")
	}
	if s.dalje("cop-osijek", NajviseVerzijaPoRazmjeni, 0, context.Canceled, &zbogKarte, new(string)) || s.dalje("cop-osijek", 0, 0, nil, new(int), new(string)) {
		t.Error("greška ili prazan razgovor ne ponavljaju se")
	}
	if _, _, krugova, err := s.razgovoriZaRedom(ctxRub, "nepoznat"); krugova != 1 || err == nil {
		t.Errorf("nepoznat čvor: %d razgovora, %v", krugova, err)
	}
}
