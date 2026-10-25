package razmjena

// Sigurnost WEB synca (plan, F3, „Sigurnost”): vrata za QUIC, ključ druge
// strane, pozdrav, jednosmjerni tokovi, opoziv usred sesije, podmetnute
// poruke, poplava pričuve i gorutine nakon zatvaranja. Računala su javna,
// bez NAT-a, u mjehuru.

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net"
	"net/netip"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/testutils/simnet"

	"gocop/internal/natsim"
)

var (
	vrataA = netip.MustParseAddrPort("198.51.100.1:4713")
	vrataB = netip.MustParseAddrPort("198.51.100.2:4713")
	vrataC = netip.MustParseAddrPort("198.51.100.3:4713") // član koji nije u ovoj sesiji
	vrataX = netip.MustParseAddrPort("198.51.100.9:4713") // drži adresu kandidata, a ne udara
)

// otvorenaVrata: B radi i ima pripremu pozvanoga za A, s kandidatima A i X,
// a A-ovi udarci (iste sesije i tajni) su mu otvorili vrata za QUIC s
// adrese A. Stroj B-ove pripreme ne vozi nitko: što stigne do njega,
// test čita s ulaza.
type otvorenaVrata struct {
	m     *natsim.Mreza
	b     *cvorWeb
	z     *zapisVrata
	privA ed25519.PrivateKey
	uA    *UDP
	p     *priprema
	vozi  bool         // stroj pripreme vozi test, pa ona sama počisti
	retry atomic.Int32 // Retry paketa koje je B poslao
}

func otvoriVrata(t *testing.T) *otvorenaVrata {
	t.Helper()
	ov := &otvorenaVrata{m: internetUdaraca(t), z: &zapisVrata{}, privA: newKey(t)}
	ov.m.Promatraj(func(p simnet.Packet) {
		if jeRetry(p.Data) && p.From.String() == vrataB.String() {
			ov.retry.Add(1)
		}
	})
	o := NovaOgrada()
	o.KadOdbije(ov.z.biljezi)
	ov.b = noviCvorWeb(t, stranaJavna(t, ov.m, vrataB.String(), newKey(t)), novaPosta(t), func(p *PostavkeWeb) { p.Ograda = o })
	javniA := ov.privA.Public().(ed25519.PublicKey)
	ov.b.clan(javniA, true)
	ctx, otkazi := context.WithCancelCause(t.Context())
	ov.p = ov.b.w.novaPriprema(otkazi, javniA, NovaSesija(), false)
	ov.p.ctx = ctx
	bezGreske(t, ov.p.pokreniUdarce(tajnaUdaraca, ov.p.tajna, javniKandidati(vrataA, vrataX)))
	ov.b.w.mu.Lock()
	ov.b.w.pripreme[ov.p.drugaS] = ov.p
	ov.b.w.mu.Unlock()
	t.Cleanup(func() {
		if !ov.vozi {
			ov.p.pocisti()
		}
	})
	ov.uA = NoviUDPCvora(ov.m.Racunalo(vrataA, natsim.PostavkeRacunala{Kasnjenje: 5 * time.Millisecond}), ov.privA)
	t.Cleanup(func() { _ = ov.uA.Close() }) // greška zatvaranja na kraju testa nema što reći
	kA, err := NoviKljuceviUdarca("testproto", ov.p.sesija, tajnaUdaraca, ov.p.tajna, javniA, ov.b.kljuc)
	bezGreske(t, err)
	xA := noviUdarci(t, ov.uA, kA, javniKandidati(vrataB), vrataA)
	xA.Pokusaj(time.Now(), time.Now().Add(trajanjeUdaraca))
	time.Sleep(200 * time.Millisecond)
	if !ov.p.x.Load().Pusta(net.UDPAddrFromAddrPort(vrataA)) {
		t.Fatal("A-ovi udarci nisu otvorili vrata")
	}
	return ov
}

// jeRetry: paket je QUIC Retry (dugo zaglavlje, vrsta 3)
func jeRetry(b []byte) bool { return len(b) > 0 && b[0]&0xc0 == 0xc0 && (b[0]>>4)&3 == 3 }

// nazovi nazove B QUIC-om s utičnice u ključem priv, s rokom od 5 s
func (ov *otvorenaVrata) nazovi(t *testing.T, u *UDP, priv ed25519.PrivateKey) (*quic.Conn, error) {
	t.Helper()
	cfg, err := tlsKlijentaWeb(priv, "testproto", nil, vrataB.String(), ov.b.kljuc)
	bezGreske(t, err)
	ctx, otkazi := context.WithTimeout(t.Context(), 5*time.Second)
	defer otkazi()
	return u.tr.Dial(ctx, net.UDPAddrFromAddrPort(vrataB), cfg, postavkeQUIC())
}

// pozdravi pošalje pozdrav sa sesijom na novom kontrolnom toku i vrati
// grešku čitanja odgovora
func pozdraviNa(t *testing.T, veza *quic.Conn, sesija string) error {
	t.Helper()
	v := &vezaSesije{veza: veza}
	ctx, otkazi := context.WithTimeout(t.Context(), 5*time.Second)
	defer otkazi()
	return v.razmijeniPozdrav(ctx, sesija, nil)
}

// odbijenaVeza: greška veze je odbijanje QUIC-a (CONNECTION_REFUSED)
func odbijenaVeza(err error) bool {
	var te *quic.TransportError
	return errors.As(err, &te) && te.ErrorCode == quic.ConnectionRefused
}

// Stranac dobiva Retry, a zatim odbijanje; isto i računalo koje drži
// adresu potpisanog kandidata (X), ali ne udara, i član C koji nije u ovoj
// sesiji. Nijednom vrata ne puste vezu do TLS-a, a svako odbijanje ograda
// broji kao rukovanje, bez ključa.
func TestStranacDobivaRetryPaOdbijanje(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		ov := otvoriVrata(t)
		c := newKey(t)
		ov.b.clan(c.Public().(ed25519.PublicKey), true)
		for ime, s := range map[string]struct {
			a    netip.AddrPort
			priv ed25519.PrivateKey
		}{
			"stranac":           {netip.MustParseAddrPort("203.0.113.66:5000"), newKey(t)},
			"adresa kandidata":  {vrataX, newKey(t)},
			"član izvan sesije": {vrataC, c},
		} {
			prije := ov.retry.Load()
			u := uticnicaUdaraca(t, ov.m.Racunalo(s.a, natsim.PostavkeRacunala{Kasnjenje: 5 * time.Millisecond}))
			if _, err := ov.nazovi(t, u, s.priv); !odbijenaVeza(err) || ov.retry.Load() == prije {
				t.Errorf("%s: %v, Retry %d", ime, err, ov.retry.Load()-prije)
			}
		}
		if len(ov.p.ulaz) != 0 || ov.b.obrada.Load() != 0 {
			t.Error("odbijena veza došla je do pripreme")
		}
		ov.z.cekaj(t, 3)
		ov.z.mu.Lock()
		defer ov.z.mu.Unlock()
		for i, r := range ov.z.razlozi {
			if r != VrataRukovanje || ov.z.kljucevi[i] != nil {
				t.Errorf("vrata su zapisala %s %x", r, ov.z.kljucevi[i])
			}
		}
	})
}

// S adrese koju su vrata pustila javi se drugi ključ: nečlan s ispravnim
// TLS-om (certifikat čvora), član C s vlastitim ključem i ključ samog B-a
// (sesija sa samim sobom). Rukovanje kod
// pozivatelja prođe (Dial), ali B ga odbije usred rukovanja, prije
// pozdrava, i vrata to zapišu; do pripreme ne dođe ništa.
func TestDrugiKljucNaPustenojAdresi(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		ov := otvoriVrata(t)
		c := newKey(t)
		ov.b.clan(c.Public().(ed25519.PublicKey), true)
		for i, priv := range []ed25519.PrivateKey{newKey(t), c, ov.b.priv} {
			veza, err := ov.nazovi(t, ov.uA, priv)
			bezGreske(t, err)
			if err := pozdraviNa(t, veza, ov.p.sesija); err == nil {
				t.Errorf("%d: drugi ključ je dobio pozdrav", i)
			}
			if r, k := ov.z.cekaj(t, i+1); r != VrataNijeClan || !k.Equal(priv.Public()) {
				t.Errorf("%d: vrata su zapisala %s %x", i, r, k)
			}
		}
		if len(ov.p.ulaz) != 0 || ov.b.obrada.Load() != 0 {
			t.Error("drugi ključ došao je do pripreme")
		}
	})
}

// A-ov ključ s pozdravom starije (ili tuđe) sesije: B zatvori vezu kodom
// 0x0103. S pozdravom ove sesije veza ide stroju pripreme.
func TestPozdravStarijeSesijeSeZatvara(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		ov := otvoriVrata(t)
		veza, err := ov.nazovi(t, ov.uA, ov.privA)
		bezGreske(t, err)
		var ae *quic.ApplicationError
		if err := pozdraviNa(t, veza, NovaSesija()); !errors.As(err, &ae) || ae.ErrorCode != quic.ApplicationErrorCode(KodOdbijeno) {
			t.Errorf("starija sesija: %v", err)
		}
		if r, _ := ov.z.cekaj(t, 1); r != VrataRukovanje {
			t.Errorf("vrata: %s", r)
		}
		veza, err = ov.nazovi(t, ov.uA, ov.privA)
		bezGreske(t, err)
		go func() {
			_ = pozdraviNa(t, veza, ov.p.sesija) // odgovor B-a ne stiže, jer stroj ne vozi nitko
		}()
		st := <-ov.p.ulaz
		if d, ima := st.f(); !ima || d.Vrsta != DogadjajPrihvacen || ov.p.veza == nil {
			t.Errorf("veza ove sesije: %+v", d)
		}
		ov.p.veza.odbaci()
	})
}

// Veza koja nakon rukovanja ne otvori kontrolni tok, na prvom toku ne
// pošalje kanal 0x00 ili pošalje pokvaren pozdrav, zatvara se kodom
// 0x0103.
func TestVezaBezPozdrava(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		ov := otvoriVrata(t)
		tiha, err := ov.nazovi(t, ov.uA, ov.privA)
		bezGreske(t, err)
		krivi, err := ov.nazovi(t, ov.uA, ov.privA)
		bezGreske(t, err)
		tok, err := krivi.OpenStreamSync(t.Context())
		bezGreske(t, err)
		_, _ = tok.Write([]byte{kanalRazmjena}) // ishod je zatvaranje veze
		pokvaren, err := ov.nazovi(t, ov.uA, ov.privA)
		bezGreske(t, err)
		tok, err = pokvaren.OpenStreamSync(t.Context())
		bezGreske(t, err)
		_, _ = tok.Write([]byte{kanalKontrola, 0, 0, 0, 1, '{'}) // isto
		for ime, v := range map[string]*quic.Conn{"bez toka": tiha, "drugi kanal": krivi, "pokvaren pozdrav": pokvaren} {
			select {
			case <-v.Context().Done():
			case <-time.After(2 * rokPozdrava):
			}
			var ae *quic.ApplicationError
			if !errors.As(context.Cause(v.Context()), &ae) || ae.ErrorCode != quic.ApplicationErrorCode(KodOdbijeno) {
				t.Errorf("%s: %v", ime, context.Cause(v.Context()))
			}
		}
	})
}

// Jednosmjerni tok se ne da otvoriti: sesija ih ne prima nijedan.
func TestJednosmjerniTokSeOdbija(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		a, b, _ := javniPar(t, internetUdaraca(t))
		sa, sb := sesijaPara(t, a, b)
		for _, s := range []*SesijaWeb{sa, sb} {
			if _, err := s.veza.OpenUniStream(); err == nil {
				t.Error("jednosmjerni tok je otvoren")
			}
		}
		if _, err := razgovorNa(t.Context(), sa); err != nil {
			t.Error(err)
		}
	})
}

// Član opozvan usred sesije: čim se opoziv primijeni (ProvjeriSve), obje
// strane imaju sesiju zatvorenu kodom 0x0104. Isto kad je opozvana ovlast
// računala koje ga je primilo. Bez ProvjeriSve opoziv uhvati provjera na
// početku idućeg razgovora, u oba smjera.
func TestOpozivUsredSesije(t *testing.T) {
	for _, c := range []struct {
		ime     string
		opozovi func(b *cvorWeb, a *cvorWeb, primatelji *sync.Map)
		odmah   bool // ProvjeriSve nakon opoziva
		zove    func(a, b *SesijaWeb) *SesijaWeb
	}{
		{"opoziv člana", func(b, a *cvorWeb, _ *sync.Map) { b.clan(a.kljuc, false) }, true, nil},
		{"opoziv ovlasti primatelja", func(_, _ *cvorWeb, p *sync.Map) { p.Store("primatelj", false) }, true, nil},
		{"razgovor opozvanoga", func(b, a *cvorWeb, _ *sync.Map) { b.clan(a.kljuc, false) }, false, func(a, _ *SesijaWeb) *SesijaWeb { return a }},
		{"razgovor s opozvanim", func(b, a *cvorWeb, _ *sync.Map) { b.clan(a.kljuc, false) }, false, func(_, b *SesijaWeb) *SesijaWeb { return b }},
	} {
		t.Run(c.ime, func(t *testing.T) {
			uMjehuru(t, func(t *testing.T) {
				var primatelji sync.Map
				primatelji.Store("primatelj", true)
				a, b, _ := javniPar(t, internetUdaraca(t), func(p *PostavkeWeb) {
					clan := p.Trusted
					p.Trusted = func(k ed25519.PublicKey, vj []byte) bool {
						valjana, _ := primatelji.Load("primatelj") // oba je primio isti primatelj
						return clan(k, vj) && valjana.(bool)
					}
				})
				sa, sb := sesijaPara(t, a, b)
				c.opozovi(b, a, &primatelji)
				if c.odmah {
					b.w.ProvjeriSve()
				} else if _, err := razgovorNa(t.Context(), c.zove(sa, sb)); err == nil {
					t.Error("razgovor s opozvanim je prošao")
				}
				if !zatvorena(sa, 2*time.Second) || !zatvorena(sb, time.Second) || sa.Kod() != KodNijeClan || sb.Kod() != KodNijeClan {
					t.Errorf("kod zatvaranja: A %#x, B %#x", sa.Kod(), sb.Kod())
				}
				if b.obrada.Load() != 0 {
					t.Error("opozvani je došao do obrade")
				}
			})
		})
	}
}

// Podmetnut odziv: član C potpiše odziv za A-ovu sesiju, sa svojim
// kandidatima, i stigne prije B-ova. Stroj ga odbaci prije provjere potpisa
// (nije od druge strane), pa ne ulazi ni u pričuvu poruka, a sesija je s B.
// Ključ druge strane (expect) dolazi samo od pozivatelja.
func TestPodmetnutOdzivNeMijenjaDrugu(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		a, b, posta := javniPar(t, m)
		c := newKey(t)
		a.clan(c.Public().(ed25519.PublicKey), true)
		var podmetnut PotpisanaPoruka
		posta.zadrziAko(func(pp PotpisanaPoruka) bool {
			t, _ := pp.Zaglavlje() // posta je zaglavlje već pročitala
			if pp.Vrsta == VrstaOdziv && podmetnut.Tijelo == nil {
				podmetnut = Potpisi(c, "testproto", VrstaOdziv, TijeloPoruke{Sesija: t.Sesija, Za: t.Za, Sastajaliste: t.Sastajaliste,
					Odluka: OdlukaProbaj, Tajna: tajnaOdziva, Kandidati: javniKandidati(vrataC)})
				a.w.NaPoruku(podmetnut)
			}
			return false
		})
		s, err := a.w.Pripremi(t.Context(), b.kljuc)
		bezGreske(t, err)
		if !s.Druga.Equal(b.kljuc) || podmetnut.Tijelo == nil {
			t.Fatalf("sesija s %x", s.Druga)
		}
		t2, _ := podmetnut.Zaglavlje() // upravo zapisano
		a.w.pricuva.mu.Lock()
		_, zapamcen := a.w.pricuva.vidjeno[VrstaOdziv+"|"+t2.Od+"|"+t2.Sesija+"|0"]
		a.w.pricuva.mu.Unlock()
		if zapamcen {
			t.Error("podmetnut odziv ušao je u pričuvu poruka")
		}
	})
}

// Poplava sinkra i odziva za pripremu koja čeka sinkro: tuđi, prekasni,
// izvan broja pokušaja, za nepoznatu sesiju i odziv pozvanome ne ulaze u
// pričuvu poruka.
func TestPoplavaNePuniPricuvu(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		ov := otvoriVrata(t)
		ov.p.p = NovoProbijanjePozvanog(TijeloPoruke{Sesija: ov.p.sesija, Od: ov.p.drugaS}, false, ov.b.nat, time.Now())
		ov.vozi = true
		gotov := make(chan struct{})
		go func() {
			defer close(gotov)
			_, _ = ov.p.vozi(ov.p.ctx) // stroj pozvanoga čeka sinkro; ishod se ne gleda
		}()
		defer func() {
			ov.p.otkazi(nil)
			<-gotov
		}()
		c := newKey(t)
		ov.b.clan(c.Public().(ed25519.PublicKey), true)
		tijelo := func(sesija string, pokusaj int) TijeloPoruke {
			return TijeloPoruke{Sesija: sesija, Za: PublicKeyString(ov.b.kljuc), Sastajaliste: PublicKeyString(ov.b.posta.javni()), Pokusaj: pokusaj}
		}
		for i := range 2000 {
			for _, pp := range []PotpisanaPoruka{
				Potpisi(c, "testproto", VrstaSinkro, tijelo(ov.p.sesija, 1+i%3)),
				Potpisi(ov.privA, "testproto", VrstaSinkro, tijelo(ov.p.sesija, 4)),
				Potpisi(ov.privA, "testproto", VrstaSinkro, tijelo(NovaSesija(), 1)),
				Potpisi(ov.privA, "testproto", VrstaOdziv, tijelo(ov.p.sesija, 0)),
			} {
				ov.b.w.NaPoruku(pp)
			}
			if i%50 == 0 {
				time.Sleep(time.Millisecond)
			}
		}
		time.Sleep(time.Second)
		ov.b.w.pricuva.mu.Lock()
		n := len(ov.b.w.pricuva.vidjeno)
		ov.b.w.pricuva.mu.Unlock()
		if n != 0 {
			t.Errorf("u pričuvi %d poruka", n)
		}
	})
}

// Nakon zatvaranja sesija i kraja rada WEB synca ne ostaje nijedna
// gorutina.
func TestGorutineNakonZatvaranja(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		ka, kb := poredaniKljucevi(t)
		sa, sb := stranaJavna(t, m, vrataA.String(), ka), stranaJavna(t, m, vrataB.String(), kb)
		synctest.Wait()
		prije := runtime.NumGoroutine()
		a, b, _ := parWeb(t, sa, sb, true)
		s, _ := sesijaPara(t, a, b)
		for range 3 {
			if _, err := razgovorNa(t.Context(), s); err != nil {
				t.Fatal(err)
			}
		}
		a.zaustavi()
		b.zaustavi()
		time.Sleep(time.Minute) // zatvaranje putuje mrežom, a quic-go čisti za sobom
		synctest.Wait()
		if poslije := runtime.NumGoroutine(); poslije != prije {
			t.Errorf("gorutina prije %d, poslije %d", prije, poslije)
		}
	})
}

// Na adresi probijenog puta sluša slušalica s drugim ključem (ovdje B, a
// A očekuje C): A prekine rukovanje prije nego što pošalje svoj
// certifikat, pa slušalica ne vidi ni A-ov ključ ni pozdrav.
func TestKljucDrugeStraneKodPozivatelja(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		ov := otvoriVrata(t)
		a := &WebSync{PostavkeWeb: PostavkeWeb{Priv: ov.privA, Protokol: "testproto", Vj: func() []byte { return nil }, Znanje: func() *Znanje { return nil }}, u: ov.uA}
		c := newKey(t).Public().(ed25519.PublicKey)
		v, err := a.zovi(t.Context(), &priprema{druga: c, sesija: ov.p.sesija}, vrataB)
		var te *quic.TransportError
		if v != nil || !errors.As(err, &te) || !te.ErrorCode.IsCryptoError() {
			t.Fatalf("poziv drugog ključa: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
		ov.z.mu.Lock()
		zapisano := len(ov.z.razlozi)
		ov.z.mu.Unlock()
		if zapisano != 0 || len(ov.p.ulaz) != 0 {
			t.Errorf("slušalica je vidjela A: zapisa %d, na ulazu %d", zapisano, len(ov.p.ulaz))
		}
	})
}

// Veze koje stignu stroju dok priprema završava, nakon kraja ili ostanu na
// ulazu zatvaraju se odmah, a ne tek kad ih zatvori druga strana ili istek
// mirovanja.
func TestVezaNakonKrajaPripreme(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		ov := otvoriVrata(t)
		ov.vozi = true // test počisti sam
		var veze []*quic.Conn
		for range cap(ov.p.ulaz) + 4 {
			veza, err := ov.nazovi(t, ov.uA, ov.privA)
			bezGreske(t, err)
			veze = append(veze, veza)
		}
		nista := func() (Dogadjaj, bool) { return Dogadjaj{}, false }
		ov.p.predaj(&vezaSesije{veza: veze[0]}, nista) // ostane na ulazu
		var predaje sync.WaitGroup
		for _, veza := range veze[1 : len(veze)-1] {
			predaje.Go(func() { ov.p.predaj(&vezaSesije{veza: veza}, nista) })
		}
		ov.p.pocisti()
		predaje.Wait()
		ov.p.predaj(&vezaSesije{veza: veze[len(veze)-1]}, nista) // nakon kraja
		for i, veza := range veze {
			select {
			case <-veza.Context().Done():
			case <-time.After(100 * time.Millisecond):
				t.Errorf("%d. veza nije zatvorena", i)
			}
		}
		if len(ov.p.ulaz) != 0 {
			t.Errorf("na ulazu %d", len(ov.p.ulaz))
		}
	})
}
