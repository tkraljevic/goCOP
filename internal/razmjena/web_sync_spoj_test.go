package razmjena

// Ono što WEB sync dobiva za spoj u program (F3b): odziv „isključeno” uz
// web_sync = false, put kroz Tailscale uz [sync] tailscale = false, stanje
// mreže za pauzu po paru, brojila i drugi ključ u grešci pripreme i rok
// mirovanja QUIC veze.

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// odbijaUmjestoRadi zaustavi Radi računala c i pokrene Odbija, kao program
// uz web_sync = false; Odbija staje na kraju testa
func odbijaUmjestoRadi(t *testing.T, c *cvorWeb) {
	t.Helper()
	c.zaustavi()
	ctx, stani := context.WithCancel(context.Background())
	kraj := make(chan error, 1)
	go func() { kraj <- c.w.Odbija(ctx) }()
	for !c.w.odbija.Load() {
		time.Sleep(time.Millisecond)
	}
	t.Cleanup(func() {
		stani()
		if err := <-kraj; err != nil {
			t.Errorf("Odbija: %v", err)
		}
	})
}

// Pozvani s isključenim WEB syncom odmah odgovori „isključeno” (Odluke
// (25. 10. 2026.), 1), pa pozivatelj ne čeka 10 s bez odziva. Odziv i
// sinkro te poziv nečlana i vlastiti poziv ne dobiju ništa, a dok radi
// Odbija, ne kreće ni Radi ni drugi Odbija.
func TestIskljucenWebSyncOdgovaraOdmah(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		a, b, posta := javniPar(t, m)
		odbijaUmjestoRadi(t, b)
		if err := b.w.Radi(t.Context()); !errors.Is(err, ErrWebSyncVecRadi) {
			t.Errorf("Radi uz Odbija: %v", err)
		}
		if err := b.w.Odbija(t.Context()); !errors.Is(err, ErrWebSyncVecRadi) {
			t.Errorf("drugi Odbija: %v", err)
		}
		pocetak := time.Now()
		_, err := a.w.Pripremi(t.Context(), b.kljuc)
		var g GreskaPripreme
		if !errors.As(err, &g) || g.Razlog != RazlogIskljuceno || g.Pokusaj != 0 || time.Since(pocetak) > time.Second {
			t.Fatalf("priprema prema isključenom: %v nakon %v", err, time.Since(pocetak))
		}
		posta.mu.Lock()
		odziv, _ := posta.poslano[len(posta.poslano)-1].Zaglavlje() // posta je zapisala ispravno tijelo
		posta.mu.Unlock()
		if odziv.Odluka != OdlukaIskljuceno || odziv.Razlog != "" || len(odziv.Kandidati) != 0 || odziv.Tajna != nil {
			t.Errorf("odziv: %+v", odziv)
		}
		prije := len(posta.vrste())
		stranac := newKey(t)
		b.clan(b.kljuc, true) // vlastiti poziv prođe provjeru, a ipak ne dobije odziv
		for _, pp := range []PotpisanaPoruka{
			a.w.potpisi(VrstaOdziv, NovaSesija(), PublicKeyString(b.kljuc), TijeloPoruke{Odluka: OdlukaProbaj}),
			a.w.potpisi(VrstaSinkro, NovaSesija(), PublicKeyString(b.kljuc), TijeloPoruke{Pokusaj: 1}),
			Potpisi(stranac, "testproto", VrstaPoziv, TijeloPoruke{Sesija: NovaSesija(), Za: PublicKeyString(b.kljuc), Sastajaliste: PublicKeyString(posta.javni())}),
			b.w.potpisi(VrstaPoziv, NovaSesija(), PublicKeyString(b.kljuc), TijeloPoruke{}),
		} {
			b.w.NaPoruku(pp)
		}
		time.Sleep(time.Second)
		if v := posta.vrste(); len(v) != prije {
			t.Errorf("odbijanje je poslalo još %v", v[prije:])
		}
		b.w.pricuva.mu.Lock()
		n := len(b.w.pricuva.vidjeno)
		b.w.pricuva.mu.Unlock()
		if n != 2 {
			t.Errorf("pričuva poruka: %d, želim samo poziv od A i vlastiti", n)
		}
	})
}

// Stroj probijanja odluku „isključeno” čita kao neuspjeh s vlastitim
// razlogom, a nepoznatu odluku kao „odbij”.
func TestOdlukaIskljuceno(t *testing.T) {
	for odluka, razlog := range map[string]string{OdlukaIskljuceno: RazlogIskljuceno, OdlukaOdbij: RazlogOdbijeno, "nepoznata": RazlogOdbijeno} {
		sad := time.Now()
		p := NovoProbijanjePozivatelja(TijeloPoruke{Sesija: "s", Za: "b"}, true, sad)
		p, r := p.Prijelaz(Dogadjaj{Vrsta: DogadjajOdziv, Sad: sad, Poruka: TijeloPoruke{Sesija: "s", Od: "b", Odluka: odluka}})
		if p.Faza != FazaNeuspjeh || p.Razlog != razlog || len(r) != 2 || r[1].Razlog != razlog {
			t.Errorf("%s: %+v %+v", odluka, p, r)
		}
	}
}

// tailscaleAdresaA je izmišljena Tailscale adresa ovog računala u testovima
// puta kroz Tailscale
var tailscaleAdresaA = netip.MustParseAddr("100.101.102.103")

// krozTailscaleSve: promet prema svakoj adresi izlazi s Tailscale adrese
// (ruta podmreže ili izlazni čvor kroz Tailscale)
func krozTailscaleSve(p *PostavkeWeb) {
	p.TailscaleAdrese = func() []netip.Addr { return []netip.Addr{tailscaleAdresaA} }
	p.Izlaz = func(netip.AddrPort) (netip.Addr, error) { return tailscaleAdresaA, nil }
}

// Uz [sync] tailscale = false B odbacuje udarce s adrese do koje promet
// ide kroz Tailscale: ne odgovara na njih i vrata ih ne puštaju, pa
// priprema ne uspije, a A vidi da udarci druge strane nisu stigli. Uz
// uključen Tailscale isti par uspije, a sesija se vodi kao tailscale.
func TestUdarciKrozTailscale(t *testing.T) {
	for _, bez := range []bool{true, false} {
		uMjehuru(t, func(t *testing.T) {
			m := internetUdaraca(t)
			ka, kb := poredaniKljucevi(t)
			posta := novaPosta(t)
			a := noviCvorWeb(t, stranaJavna(t, m, vrataA.String(), ka), posta)
			b := noviCvorWeb(t, stranaJavna(t, m, vrataB.String(), kb), posta, krozTailscaleSve, func(p *PostavkeWeb) {
				p.BezTailscalea = func() bool { return bez }
			})
			a.clan(b.kljuc, true)
			b.clan(a.kljuc, true)
			s, err := a.w.Pripremi(t.Context(), b.kljuc)
			if !bez {
				bezGreske(t, err)
				if sB := b.sesijaIz(t); sB.Put != PutTailscale || s.Put != PutProbijeno {
					t.Errorf("put sesije: B %s, A %s", sB.Put, s.Put)
				}
				return
			}
			var g GreskaPripreme
			if !errors.As(err, &g) || g.Razlog != RazlogBezPuta || g.Udaraca == 0 || g.Valjanih != 0 {
				t.Fatalf("priprema kroz isključen Tailscale: %v (%+v)", err, g)
			}
		})
	}
}

// QUIC put kroz Tailscale uz isključen Tailscale A odbija i kad je došao
// do kraja probijanja (ovdje se Tailscale isključi dok A zove): priprema
// vrati bez-tailscalea, a sesija se ne upiše.
func TestQUICKrozTailscaleSeOdbija(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		ka, kb := poredaniKljucevi(t)
		posta := novaPosta(t)
		var iskljucen atomic.Bool
		a := noviCvorWeb(t, stranaJavna(t, m, vrataA.String(), ka), posta, krozTailscaleSve, func(p *PostavkeWeb) {
			p.BezTailscalea = iskljucen.Load
			p.Znanje = func() *Znanje { // A ga traži za pozdrav, tik prije QUIC-a
				iskljucen.Store(true)
				return nil
			}
		})
		b := noviCvorWeb(t, stranaJavna(t, m, vrataB.String(), kb), posta)
		a.clan(b.kljuc, true)
		b.clan(a.kljuc, true)
		_, err := a.w.Pripremi(t.Context(), b.kljuc)
		if greskaPripreme(err) != RazlogBezTailscalea || a.w.Sesija(b.kljuc) != nil {
			t.Fatalf("QUIC kroz isključen Tailscale: %v", err)
		}
	})
}

// Pozvani javi poziv člana sa stanjem njegove mreže, a neuspjela priprema
// nosi stanje druge strane iz odziva i brojila udaraca; MojOtisak je
// stanje ove strane.
func TestStanjeMrezeUPripremi(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		m.DvaSTUN()
		sa, sb, _, _ := parIza(t, m, eimPoPortu, simetricni)
		var mu sync.Mutex
		javljeno := map[string]string{}
		a, b, _ := parWeb(t, sa, sb, true, func(p *PostavkeWeb) {
			p.NaPoziv = func(druga ed25519.PublicKey, otisak string) {
				mu.Lock()
				defer mu.Unlock()
				javljeno[PublicKeyString(druga)] = otisak
			}
		})
		_, err := a.w.Pripremi(t.Context(), b.kljuc)
		var g GreskaPripreme
		if !errors.As(err, &g) || g.Razlog != RazlogBezPuta || g.Pokusaj != 3 || g.Udaraca == 0 || g.DrugiKljuc {
			t.Fatalf("priprema: %v (%+v)", err, g)
		}
		if g.Druga != OtisakMreze(sb.kandidati, sb.nat) || g.Druga != b.w.MojOtisak() {
			t.Errorf("stanje druge strane %q, B javlja %q", g.Druga, b.w.MojOtisak())
		}
		mu.Lock()
		defer mu.Unlock()
		if javljeno[PublicKeyString(a.kljuc)] != a.w.MojOtisak() || len(javljeno) != 1 {
			t.Errorf("B je javio %v, A je %q", javljeno, a.w.MojOtisak())
		}
	})
}

// Stanje mreže su javni kandidati, poredani, i vrsta NAT-a; lokalni
// kandidati ga ne mijenjaju.
func TestOtisakMreze(t *testing.T) {
	javni := []KandidatPoruke{{Vrsta: KandidatJavni, Adresa: "203.0.113.2:4713"}, {Vrsta: KandidatJavni, Adresa: "203.0.113.1:4713"}}
	s := OtisakMreze(append(javni, KandidatPoruke{Vrsta: KandidatLokalni, Adresa: "192.168.1.10:4713"}), &NATPoruke{Vrsta: NATLak})
	if s != "203.0.113.1:4713,203.0.113.2:4713|"+NATLak+"|false" || s != OtisakMreze([]KandidatPoruke{javni[1], javni[0]}, &NATPoruke{Vrsta: NATLak}) {
		t.Errorf("otisak: %q", s)
	}
	for _, drugi := range []string{OtisakMreze(javni[:1], &NATPoruke{Vrsta: NATLak}), OtisakMreze(javni, nil), OtisakMreze(javni, &NATPoruke{UDPBlokiran: true})} {
		if drugi == s {
			t.Errorf("drugo stanje daje isti otisak %q", s)
		}
	}
}

// Ishod QUIC-a s drugim ključem na adresi druge strane pamti se za grešku
// pripreme (to nije neuspjeh probijanja), a ishod starije radnje ne.
func TestDrugiKljucUGresciPripreme(t *testing.T) {
	p := &priprema{radnja: 2, p: Probijanje{Faza: FazaQUIC}}
	p.naIshod(1, nil, drugiKljuc{poruka: "drugi"})
	if g := p.greska(RazlogBezPuta); g.DrugiKljuc || g.Udaraca != 0 {
		t.Errorf("starija radnja: %+v", g)
	}
	p.naIshod(2, nil, errors.New("rukovanje"))
	p.naIshod(2, nil, drugiKljuc{poruka: "drugi"})
	p.naIshod(2, nil, errors.New("rukovanje"))
	if g := p.greska(RazlogBezPuta); !g.DrugiKljuc || g.Razlog != RazlogBezPuta {
		t.Errorf("drugi ključ: %+v", g)
	}
}

// Rok mirovanja QUIC veze je 30 s, dvaput razmak održavanja od 15 s
// (Odluke (25. 10. 2026.), 2).
func TestMirovanjeQUIC(t *testing.T) {
	if c := postavkeQUIC(); c.MaxIdleTimeout != 30*time.Second || c.KeepAlivePeriod != 15*time.Second || c.MaxIdleTimeout != 2*c.KeepAlivePeriod {
		t.Errorf("mirovanje %v, održavanje %v", c.MaxIdleTimeout, c.KeepAlivePeriod)
	}
}
