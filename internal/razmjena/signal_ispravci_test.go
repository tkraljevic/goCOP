package razmjena

// Popravci sastajališta i klijenta nakon pregleda F2b

import (
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

// Računalo opozvano između rukovanja i prijave ne uđe u registar: članstvo
// se gleda i pri upisu
func TestOpozivIzmeduRukovanjaIPrijave(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		su := novoSastajalisteUMemoriji(t)
		kljuc := newKey(t)
		su.clan("laptop", kljuc)
		v := su.rucnaVeza(t, kljuc, "")
		time.Sleep(time.Second) // sastajalište je dovršilo rukovanje (i kašnjenje mreže) i čeka prijavu
		su.opozovi(kljuc)
		su.r.ProvjeriSve()
		bezGreske(t, v.posalji(SignalPrijava, prijavaSignala{Ver: 1, Proces: "p"}))
		p, err := v.primi(time.Minute)
		var odb odbijenoSignala
		if err != nil || p.Vrsta != SignalOdbijeno || tijeloIz(p, &odb) != nil || odb.Kod != KodSignalaNijeClan {
			t.Errorf("odgovor opozvanom: %+v, %v, %+v", p, err, odb)
		}
		if n := su.r.Stanje().Prijavljenih; n != 0 {
			t.Errorf("opozvani u registru: %d", n)
		}
	})
}

// Računalo koje ode prije potvrde ne ostaje u registru, a prekid se broji
func TestPrijavaBezPotvrdeNeOstaje(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		su := novoSastajalisteUMemoriji(t)
		kljuc := newKey(t)
		su.clan("laptop", kljuc)
		_ = su.rucnaVeza(t, kljuc, "tihi") // ne odgovara na puls
		v := su.rucnaVeza(t, kljuc, "")
		bezGreske(t, v.posalji(SignalPrijava, prijavaSignala{Ver: 1, Proces: "novi"}))
		v.zatvori() // ode prije potvrde, dok sastajalište provjerava staru vezu
		time.Sleep(rokStareVeze + time.Second)
		synctest.Wait()
		if st := su.r.Stanje(); st.Prijavljenih != 0 || st.Prekida24h < 1 {
			t.Errorf("nakon odlaska bez potvrde: %+v", st)
		}
	})
}

// Odziv pozivatelja ne zatvara sesiju; poziv koji nije stigao ne drži
// mjesto; odziv sastajališta zatvara sesiju prema njemu
func TestSesijeNakonPregleda(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		su := novoSastajalisteUMemoriji(t)
		kljucA, kljucB := newKey(t), newKey(t)
		su.clan("laptop", kljucA)
		su.clan("ured", kljucB)
		a, b := su.klijent(kljucA), su.klijent(kljucB)
		nema := make(chan string, 16)
		a.NaNemu = func(_, razlog string) { nema <- razlog }
		kodB := make(chan PotpisanaPoruka, 16)
		b.NaPoruku = func(p PotpisanaPoruka) { kodB <- p }
		pokreni(t, a)
		pokreni(t, b)
		cekajStanje(t, a, "A prijavljen", prijavljen)
		cekajStanje(t, b, "B prijavljen", prijavljen)
		javniR := PublicKeyString(su.kljucR.Public().(ed25519.PublicKey))
		zaA, zaB := PublicKeyString(kljucA.Public().(ed25519.PublicKey)), PublicKeyString(kljucB.Public().(ed25519.PublicKey))
		pozovi := func(za string) string {
			s := NovaSesija()
			bezGreske(t, a.Posalji(Potpisi(kljucA, protokolSignala, VrstaPoziv, TijeloPoruke{Sesija: s, Za: za, Sastajaliste: javniR})))
			return s
		}
		prva := pozovi(zaB)
		pozovi(zaB)
		<-kodB
		<-kodB
		// A sam „odgovara” na svoj poziv: sesija ostaje
		bezGreske(t, a.Posalji(Potpisi(kljucA, protokolSignala, VrstaOdziv, TijeloPoruke{Sesija: prva, Za: zaB, Sastajaliste: javniR})))
		<-kodB
		pozovi(zaB)
		if r := <-nema; r != NemaPreviseSesija {
			t.Errorf("odziv pozivatelja zatvorio je sesiju: %q", r)
		}

		time.Sleep(time.Minute + rokSesijeSignala)
		nepoznat := PublicKeyString(newKey(t).Public().(ed25519.PublicKey))
		for range 3 {
			pozovi(nepoznat)
			if r := <-nema; r != NemaNijePrijavljen {
				t.Errorf("neisporučen poziv: %q", r)
			}
		}
		bezGreske(t, a.Posalji(Potpisi(kljucA, protokolSignala, VrstaOdziv, TijeloPoruke{Sesija: NovaSesija(), Za: nepoznat, Sastajaliste: javniR})))
		if r := <-nema; r != NemaNijePrijavljen {
			t.Errorf("neisporučen odziv: %q", r)
		}

		time.Sleep(time.Minute)
		s1 := pozovi(javniR)
		pozovi(javniR)
		<-su.naR
		<-su.naR
		bezGreske(t, su.r.Posalji(Potpisi(su.kljucR, protokolSignala, VrstaOdziv, TijeloPoruke{Sesija: s1, Za: zaA, Sastajaliste: javniR})))
		pozovi(javniR)
		select {
		case <-su.naR:
		case r := <-nema:
			t.Errorf("odziv sastajališta nije zatvorio sesiju: %q", r)
		}
	})
}

// Zamjena ide samo kad je na mjestu ključa još provjerena veza, prazno ili
// isti proces; računalo koje u međuvremenu nije član ne uđe
func TestZamjenaPrijave(t *testing.T) {
	kljuc := newKey(t)
	vjeruje := true
	s := &Sastajaliste{prijavljeni: map[string]*prijavljeni{}, vjeruje: func(ed25519.PublicKey, []byte) bool { return vjeruje }}
	veza := func(proces string) *prijavljeni {
		a, b := net.Pipe()
		t.Cleanup(func() { a.Close(); b.Close() })
		return &prijavljeni{v: &vezaSignala{c: a, kljuc: kljuc.Public().(ed25519.PublicKey)}, kljuc: "k", proces: proces, ziv: make(chan struct{}, 1)}
	}
	provjerena, treca, nova := veza("x"), veza("c"), veza("y")
	s.prijavljeni["k"] = treca
	if err := s.zamijeni(nova, provjerena); !errors.Is(err, errPromjena) || s.prijavljeni["k"] != treca {
		t.Errorf("zamjena nepoznate veze: %v", err)
	}
	isti := veza("c")
	if err := s.zamijeni(isti, provjerena); err != nil || s.prijavljeni["k"] != isti {
		t.Errorf("isti proces: %v", err)
	}
	vjeruje = false
	if err := s.zamijeni(nova, isti); !errors.Is(err, errNijeClan) || s.prijavljeni["k"] != isti {
		t.Errorf("nečlan: %v", err)
	}
	delete(s.prijavljeni, "k")
	vjeruje = true
	if err := s.zamijeni(nova, provjerena); err != nil || s.prijavljeni["k"] != nova {
		t.Errorf("prazno mjesto: %v", err)
	}
	if odbijenicaZa(errNijeClan).Kod != KodSignalaNijeClan || odbijenicaZa(errPromjena).Kod != KodSignalaNedostupno {
		t.Error("odbijenice")
	}
}

// Kopija čeka 10 min i kad je PonovnoSpoji probudi; upozorenje na
// računalu koje je ostalo ode kad se kopija dulje ne javlja
func TestRokKopije(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		su := novoSastajalisteUMemoriji(t)
		kljuc := newKey(t)
		su.clan("laptop", kljuc)
		prvi := su.klijent(kljuc)
		pokreni(t, prvi)
		cekajStanje(t, prvi, "prvi prijavljen", prijavljen)
		kopija := su.klijent(kljuc)
		ctx, ugasi := context.WithCancel(t.Context())
		gotovo := make(chan struct{})
		go func() { kopija.Radi(ctx); close(gotovo) }()
		cekajStanje(t, kopija, "kopija odbijena", func(st StanjeSignala) bool { return st.Odbijen })
		odbijanja := func() int {
			su.mu.Lock()
			defer su.mu.Unlock()
			return strings.Count(strings.Join(su.dnevnik, "\n"), "odbilo drugu kopiju")
		}
		time.Sleep(time.Minute)
		kopija.PonovnoSpoji()
		time.Sleep(5 * time.Minute)
		if n := odbijanja(); n != 1 {
			t.Errorf("PonovnoSpoji skratio rok kopije: %d odbijanja", n)
		}
		time.Sleep(5 * time.Minute)
		if n := odbijanja(); n != 2 {
			t.Errorf("nakon 10 min: %d odbijanja", n)
		}
		ugasi()
		<-gotovo
		cekajStanje(t, prvi, "upozorenje stiglo", func(st StanjeSignala) bool { return st.Kod == KodSignalaKopija })
		time.Sleep(trajanjeUpozorenjaKopije + time.Minute)
		if st := prvi.Stanje(); !st.Prijavljen || st.Kod != "" {
			t.Errorf("upozorenje o kopiji nije otišlo: %+v", st)
		}
	})
}

// Gašenje prijavljenog klijenta ne piše redak o prekidu veze
func TestGasenjeBezLaznogRetka(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		su := novoSastajalisteUMemoriji(t)
		kljuc := newKey(t)
		su.clan("laptop", kljuc)
		k := su.klijent(kljuc)
		var redci []string
		k.Javi = func(f string, a ...any) { redci = append(redci, f) }
		ctx, ugasi := context.WithCancel(t.Context())
		gotovo := make(chan struct{})
		go func() { k.Radi(ctx); close(gotovo) }()
		cekajStanje(t, k, "prijavljen", prijavljen)
		ugasi()
		<-gotovo
		for _, r := range redci {
			if strings.Contains(r, "nije prijavljen") {
				t.Errorf("redak pri gašenju: %q", r)
			}
		}
	})
}

// PonovnoSpoji prekine i pokušaj koji se tek otvara, pa klijent odmah
// pokuša ponovno
func TestPonovnoSpojiPrekidaPokusaj(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		a, b := net.Pipe()
		t.Cleanup(func() { a.Close(); b.Close() })
		go func() { _, _ = io.Copy(io.Discard, b) }() // cijev se zatvara s testom
		pokusaja := 0
		k := &KlijentSignala{Kljuc: func(ctx context.Context) (ed25519.PublicKey, error) {
			pokusaja++
			<-ctx.Done() // pokušaj visi dok ga ne prekine PonovnoSpoji
			return nil, ctx.Err()
		}}
		ctx, ugasi := context.WithCancel(t.Context())
		gotovo := make(chan struct{})
		go func() { k.Radi(ctx); close(gotovo) }()
		synctest.Wait()
		k.PonovnoSpoji()
		synctest.Wait()
		if pokusaja != 2 {
			t.Errorf("pokušaja %d, a treba 2", pokusaja)
		}
		ugasi()
		<-gotovo
	})
}
