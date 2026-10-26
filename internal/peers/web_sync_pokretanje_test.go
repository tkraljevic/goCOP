package peers

// Pokretanje WEB synca u servisu (PokreniWebSync): uključen radi čim
// pokretanje vrati, a onaj koji se ne da pokrenuti servis ne uzima.

import (
	"errors"
	"log"
	"slices"
	"testing"
	"testing/synctest"

	"gocop/internal/razmjena"
)

// Uključen WEB sync radi čim PokreniWebSync vrati: priprema koja krene
// odmah zatim ne dobije ErrWebSyncNeRadi, nego pošalje poziv i uspije.
// Prije je WEB sync krenuo tek u gorutini, pa je krug koji je u tih
// nekoliko mikrosekundi stigao do pripreme ostao bez poziva, a pločica na
// „priprema” do sljedećeg kruga.
func TestWebSyncRadiCimSePokrene(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := mrezaWeba(t)
		c := clanoviMreze(t, false, "cop-osijek", "thinkcentre-ured")
		kucni, tc := c[0], c[1]
		posta := novaPostaWeba(t)
		upoznaj(t, kucni, tc)
		upoznaj(t, tc, kucni)
		sWebom(t, tc, posta, m.NAT(natEIM, adresa("203.0.113.2")), "192.168.2.20:4713", true)
		u, kandidati, nat := uticnicaIza(t, m.NAT(natEIM, adresa("203.0.113.1")), "192.168.1.10:4713")
		w := webNa(t, kucni, posta, u, kandidati, nat)
		t.Cleanup(kucni.PokreniWebSync(t.Context(), w, true, nil))
		ses, err := w.Pripremi(t.Context(), tc.javni())
		if err != nil {
			t.Fatalf("priprema odmah nakon pokretanja: %v", err)
		}
		if !ses.Druga.Equal(tc.javni()) || posta.poslano(razmjena.VrstaPoziv) != 1 {
			t.Errorf("sesija s %s, poziva %d", razmjena.PublicKeyString(ses.Druga), posta.poslano(razmjena.VrstaPoziv))
		}
	})
}

// Kad se slušalica QUIC-a ne da otvoriti (utičnica je u međuvremenu
// zatvorena), PokreniWebSync to upiše u dnevnik, a servis WEB sync ne
// uzima: ne javlja web/1, a krug do računala bez puta ne kreće pripremu i
// ne javlja „WEB sync se priprema”. Prije je WEB sync ostao upisan kao
// uključen, pa je svaki krug kretao pripremu koja odmah stane, a pločica
// je trajno kazala „priprema”. Zaustavljanje ne radi ništa.
func TestWebSyncBezSlusalice(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := mrezaWeba(t)
		c := clanoviMreze(t, false, "cop-osijek", "thinkcentre-ured")
		kucni, tc := c[0], c[1]
		upoznaj(t, kucni, tc)
		znaWeb(t, kucni, tc.node.ID)
		kucni.dial = bezOdgovora
		u, kandidati, nat := uticnicaIza(t, m.NAT(natEIM, adresa("203.0.113.1")), "192.168.1.10:4713")
		if err := u.Close(); err != nil {
			t.Fatal(err)
		}
		w := webNa(t, kucni, novaPostaWeba(t), u, kandidati, nat)
		d := uDnevniku(t, "povezivost: WEB sync ne radi")
		stani := kucni.PokreniWebSync(t.Context(), w, true, nil)
		if !d.zapisano() {
			t.Error("greška slušalice nije u dnevniku")
		}
		if kucni.webSync() != nil || slices.Contains(kucni.mojeMogucnosti(), razmjena.OznakaWeb) {
			t.Fatal("servis je uzeo WEB sync koji ne radi")
		}
		if _, _, err := kucni.SyncWith(t.Context(), tc.node.ID); err == nil || errors.Is(err, ErrWebSyncSePriprema) {
			t.Fatalf("SyncWith: %v", err)
		}
		if r := stanjeS(t, kucni, tc.node.ID).Put.Razlog; r == razmjena.RazlogPutaPriprema {
			t.Errorf("razlog puta: %s", r)
		}
		stani()
		stani()
	})
}

// Isti WEB sync ne radi dvaput: drugo pokretanje uz web_sync = false
// (Odbija) samo upiše grešku u dnevnik.
func TestIskljucenWebSyncNeKreceDvaput(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := mrezaWeba(t)
		s := clanoviMreze(t, false, "cop-osijek")[0]
		w := sWebom(t, s, novaPostaWeba(t), m.NAT(natEIM, adresa("203.0.113.1")), "192.168.1.10:4713", false)
		synctest.Wait() // Odbija prvog pokretanja čeka poruke
		d := uDnevniku(t, "povezivost: WEB sync ne radi")
		s.PokreniWebSync(t.Context(), w, false, nil)() // zaustavljanje čeka drugi Odbija
		if !d.zapisano() {
			t.Error("drugo pokretanje nije u dnevniku")
		}
	})
}

// uDnevniku preusmjeri dnevnik do kraja testa i javi kad se u njemu
// pojavi trazi
func uDnevniku(t *testing.T, trazi string) *dojavaDnevnika {
	t.Helper()
	d := &dojavaDnevnika{trazi: trazi, stiglo: make(chan struct{})}
	stari := log.Writer()
	log.SetOutput(d)
	t.Cleanup(func() { log.SetOutput(stari) })
	return d
}

// zapisano javlja je li traženo već u dnevniku
func (d *dojavaDnevnika) zapisano() bool {
	select {
	case <-d.stiglo:
		return true
	default:
		return false
	}
}
