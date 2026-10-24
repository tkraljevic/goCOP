package razmjena

// Nova UDP utičnica nakon promjene mreže (plan WEB synca, F2b). Kad se
// promijene lokalne adrese ili slanje s utičnice javi da nema rute, program
// zatvori utičnicu i quic.Transport i otvori nove na istom portu, s novim
// čitačem. Sesije preko stare utičnice ionako su mrtve. Na macOS-u je to
// nužno: po Nebuli Darwin veže UDP utičnicu uz sučelje na kojem je nastala,
// pa nakon selidbe šalje u prazno (mjeri se nakon izdanja 1).
//
// Prije provjere mreže nakon promjene program čeka zadanu rutu: bez nje
// provjera javi lažni „UDP blokiran”.

import (
	"context"
	"net"
	"slices"
	"time"
)

// posalji šalje paket s utičnice; slanje bez rute zapamti
func (u *UDP) posalji(b []byte, na net.Addr) {
	if _, err := u.tr.WriteTo(b, na); BezRute(err) {
		u.bezRute.Store(true)
	}
}

// BezRute javlja da je slanje s utičnice javilo da nema rute: utičnicu
// treba zamijeniti novom
func (u *UDP) BezRute() bool { return u.bezRute.Load() }

// ObnoviUDP zatvori utičnicu i otvori novu na istom portu; kad se taj ne
// da otvoriti, na nasumičnom, uz upozorenje (kao OtvoriUDP)
func ObnoviUDP(stara *UDP) (u *UDP, upozorenje, err error) {
	port := stara.Port()
	_ = stara.Close() // stara ide u svakom slučaju; greška zatvaranja ne sprječava novu
	conn, upozorenje, err := OtvoriUDP(port)
	if err != nil {
		return nil, upozorenje, err
	}
	return NoviUDP(conn), upozorenje, nil
}

// Čekanje rute; var, da ih test podmetne
var (
	poveziUDP = func(adresa string) error {
		c, err := net.Dial("udp4", adresa)
		if err == nil {
			c.Close()
		}
		return err
	}
	rokRute = 5 * time.Second
)

// CekajRutu čeka da postoji ruta prema adresi: povezana UDP utičnica
// (bez slanja), ponovno nakon 100 ms, pa dvostruko dulje, do 1 s, najviše
// 5 s. Vraća zadnju grešku kad rute nema.
func CekajRutu(ctx context.Context, adresa string) error {
	ctx, otkazi := context.WithTimeout(ctx, rokRute)
	defer otkazi()
	cekaj := 100 * time.Millisecond
	for {
		err := poveziUDP(adresa)
		if err == nil {
			return nil
		}
		t := time.NewTimer(cekaj)
		select {
		case <-ctx.Done():
			t.Stop()
			return err
		case <-t.C:
		}
		cekaj = min(2*cekaj, time.Second)
	}
}

// LokalneAdrese su adrese uključenih sučelja koja vode u lokalnu mrežu,
// poredane (isti filtar kao pronalaženje i kandidati): po njima se vidi
// promjena mreže
func LokalneAdrese() []string {
	var out []string
	for _, s := range lokalnaSucelja(procitajSucelja()) {
		for _, a := range s.adrese {
			out = append(out, a.String())
		}
	}
	slices.Sort(out)
	return out
}
