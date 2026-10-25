package razmjena

// Stroj stanja probijanja (probijanje.go): prijelazi bez mreže i bez sata,
// samo stanje i događaji.

import (
	"math"
	"net/netip"
	"slices"
	"testing"
	"time"
)

var (
	t0         = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	javnaProbe = netip.MustParseAddrPort("198.51.100.7:4713")
	lanProbe   = netip.MustParseAddrPort("192.168.1.40:4713")
	natLak     = &NATPoruke{Vrsta: NATLak}
	natOvisan  = &NATPoruke{Vrsta: NATOvisan}
)

func pozivProbe(nat *NATPoruke, kandidati ...KandidatPoruke) TijeloPoruke {
	return TijeloPoruke{Sesija: sesijaUdaraca, Od: "A", Za: "B", NAT: nat, Kandidati: kandidati}
}

func odzivProbe(nat *NATPoruke, kandidati ...KandidatPoruke) TijeloPoruke {
	return TijeloPoruke{Sesija: sesijaUdaraca, Od: "B", Za: "A", Odluka: OdlukaProbaj, NAT: nat, Kandidati: kandidati, ObradaMs: 20}
}

func sinkroProbe(n int) TijeloPoruke {
	return TijeloPoruke{Sesija: sesijaUdaraca, Od: "A", Za: "B", Pokusaj: n, RTTMs: 40}
}

var (
	lokalniKandidat = KandidatPoruke{Vrsta: KandidatLokalni, Adresa: lanProbe.String()}
	javniKandidat   = KandidatPoruke{Vrsta: KandidatJavni, Adresa: javnaProbe.String()}
)

func sat(kad time.Time) Dogadjaj { return Dogadjaj{Vrsta: DogadjajSat, Sad: kad} }

func vrsteRadnji(r []Radnja) []VrstaRadnje {
	var out []VrstaRadnje
	for _, x := range r {
		out = append(out, x.Vrsta)
	}
	return out
}

// korak je prijelaz koji mora dati baš te radnje
func korak(t *testing.T, p Probijanje, d Dogadjaj, want ...VrstaRadnje) (Probijanje, []Radnja) {
	t.Helper()
	p, r := p.Prijelaz(d)
	if got := vrsteRadnji(r); !slices.Equal(got, want) {
		t.Fatalf("radnje %v, želim %v (faza %d)", got, want, p.Faza)
	}
	return p, r
}

// pozivateljUPokusaju je pozivatelj (A) u 1. pokušaju: odziv je stigao
// nakon 60 ms, uz 20 ms obrade, pa je RTT 40 ms
func pozivateljUPokusaju(t *testing.T, a bool, odziv TijeloPoruke) (Probijanje, []Radnja) {
	t.Helper()
	p := NovoProbijanjePozivatelja(pozivProbe(natLak), a, t0)
	return korak(t, p, Dogadjaj{Vrsta: DogadjajOdziv, Sad: t0.Add(60 * time.Millisecond), Poruka: odziv}, RadnjaSinkro, RadnjaUdaraj)
}

// Pozivatelj šalje sinkro s RTT-om bez obrade druge strane i kreće pola
// RTT-a kasnije; pozvani kreće čim sinkro stigne. Udarci traju 3 s, a
// pokušaj 5 s.
func TestPrviPokusajPoRTTu(t *testing.T) {
	p, r := pozivateljUPokusaju(t, true, odzivProbe(natLak))
	pocetak := t0.Add(80 * time.Millisecond)
	if r[0].Pokusaj != 1 || r[0].RTTMs != 40 || r[1].Od != pocetak || r[1].Do != pocetak.Add(3*time.Second) || r[1].Pokusaj != 1 {
		t.Errorf("radnje %+v", r)
	}
	if p.Faza != FazaUdara || p.Pokusaj != 1 || p.Budjenje() != pocetak.Add(5*time.Second) || p.Zavrseno() {
		t.Errorf("stanje %+v", p)
	}
	q := NovoProbijanjePozvanog(pozivProbe(natLak), false, natLak, t0)
	if q.Faza != FazaCekaSinkro || q.Budjenje() != t0.Add(rokSinkra) {
		t.Fatalf("pozvani %+v", q)
	}
	stigao := t0.Add(100 * time.Millisecond)
	q, r = korak(t, q, Dogadjaj{Vrsta: DogadjajSinkro, Sad: stigao, Poruka: sinkroProbe(1)}, RadnjaUdaraj)
	if r[0].Od != stigao || r[0].Do != stigao.Add(3*time.Second) || q.Pokusaj != 1 || q.Budjenje() != stigao.Add(5*time.Second) {
		t.Errorf("pozvani: %+v, %+v", r, q)
	}
}

// RTT ne pada ispod nule kad druga strana javi dulju obradu od obilaska, a
// kriva obrada (negativna, golema) ni kasan odziv ne odgode udarce dulje od
// roka odziva. RTT iz sinkra pozvani stisne isto.
func TestRTTBezObrade(t *testing.T) {
	for _, c := range []struct {
		ime      string
		obradaMs int
		stigao   time.Duration // nakon poziva
		rtt      time.Duration
	}{
		{"dulja obrada", 500, 60 * time.Millisecond, 0},
		{"negativna obrada", -3_600_000, 40 * time.Millisecond, 40 * time.Millisecond},
		{"najmanja obrada", math.MinInt, 40 * time.Millisecond, 40 * time.Millisecond},
		{"golema obrada", math.MaxInt, 40 * time.Millisecond, 0},
		{"odziv nakon roka", 0, time.Hour, rokOdziva},
		{"odziv prije poziva", 0, -time.Second, 0},
	} {
		o := odzivProbe(natLak)
		o.ObradaMs = c.obradaMs
		p := NovoProbijanjePozivatelja(pozivProbe(natLak), true, t0)
		q, r := korak(t, p, Dogadjaj{Vrsta: DogadjajOdziv, Sad: t0.Add(c.stigao), Poruka: o}, RadnjaSinkro, RadnjaUdaraj)
		if q.rtt != c.rtt || r[0].RTTMs != int(c.rtt.Milliseconds()) || r[1].Od != t0.Add(c.stigao+c.rtt/2) {
			t.Errorf("%s: RTT %s, radnje %+v", c.ime, q.rtt, r)
		}
	}
	for ms, want := range map[int]time.Duration{-5: 0, 40: 40 * time.Millisecond, math.MaxInt: rokOdziva} {
		s := sinkroProbe(1)
		s.RTTMs = ms
		q := NovoProbijanjePozvanog(pozivProbe(natLak), false, natLak, t0)
		if q, _ = korak(t, q, Dogadjaj{Vrsta: DogadjajSinkro, Sad: t0, Poruka: s}, RadnjaUdaraj); q.rtt != want {
			t.Errorf("sinkro s %d ms: RTT %s", ms, q.rtt)
		}
	}
}

// Tri pokušaja, i kad se NAT ne zna; jedan kad je kod obiju strana
// potvrđen NAT ovisan o odredištu; nijedan kad UDP ne prolazi.
func TestBrojPokusaja(t *testing.T) {
	for _, c := range []struct {
		moj, drugi *NATPoruke
		want       int
	}{
		{natLak, natLak, 3},
		{natOvisan, natLak, 3},
		{natOvisan, nil, 3},
		{nil, nil, 3},
		{&NATPoruke{Vrsta: NATNepoznat}, natOvisan, 3},
		{natOvisan, natOvisan, 1},
		{&NATPoruke{UDPBlokiran: true}, natLak, 0},
		{natLak, &NATPoruke{Vrsta: UDPBlokiran}, 0},
	} {
		if got := brojPokusaja(c.moj, c.drugi); got != c.want {
			t.Errorf("%+v i %+v: %d, želim %d", c.moj, c.drugi, got, c.want)
		}
	}
}

// Simetrični NAT kod obiju strana: nakon jednog pokušaja probijanje nije
// uspjelo, kod obiju strana.
func TestSimetricniSObjeStraneJedanPokusaj(t *testing.T) {
	p := NovoProbijanjePozivatelja(pozivProbe(natOvisan), true, t0)
	p, _ = korak(t, p, Dogadjaj{Vrsta: DogadjajOdziv, Sad: t0.Add(60 * time.Millisecond), Poruka: odzivProbe(natOvisan)}, RadnjaSinkro, RadnjaUdaraj)
	p, _ = korak(t, p, sat(p.Budjenje()), RadnjaStani, RadnjaKraj)
	if p.Faza != FazaNeuspjeh || p.Razlog != RazlogBezPuta || p.Pokusaj != 1 || !p.Zavrseno() || !p.Budjenje().IsZero() {
		t.Errorf("pozivatelj %+v", p)
	}
	q := NovoProbijanjePozvanog(pozivProbe(natOvisan), false, natOvisan, t0)
	q, _ = korak(t, q, Dogadjaj{Vrsta: DogadjajSinkro, Sad: t0, Poruka: sinkroProbe(1)}, RadnjaUdaraj)
	q, _ = korak(t, q, sat(t0.Add(5*time.Second)), RadnjaStani, RadnjaKraj)
	if q.Faza != FazaNeuspjeh || q.Razlog != RazlogBezPuta {
		t.Errorf("pozvani %+v", q)
	}
}

// Nakon 1. pokušaja pozivatelj šalje sinkro 2, a nakon 2. sinkro 3; u 2.
// pokušaju B kreće 1 s kasnije, a u 3. A. Pozvani nakon pokušaja čeka
// sinkro i stane s udarcima.
func TestOdgodaUDrugomITrecemPokusaju(t *testing.T) {
	for _, a := range []bool{true, false} {
		p, _ := pozivateljUPokusaju(t, a, odzivProbe(natLak))
		var r []Radnja
		for n := 2; n <= 3; n++ {
			kraj := p.Budjenje()
			p, r = korak(t, p, sat(kraj), RadnjaSinkro, RadnjaUdaraj)
			odgoda := r[1].Od.Sub(kraj.Add(20 * time.Millisecond))
			want := map[bool]time.Duration{true: time.Second}[n == 2 && !a || n == 3 && a]
			if r[0].Pokusaj != n || odgoda != want || p.Budjenje() != kraj.Add(20*time.Millisecond+5*time.Second) {
				t.Errorf("A %v, %d. pokušaj: odgoda %s, radnje %+v", a, n, odgoda, r)
			}
		}
		p, _ = korak(t, p, sat(p.Budjenje()), RadnjaStani, RadnjaKraj)
		if p.Pokusaj != 3 || p.Razlog != RazlogBezPuta {
			t.Errorf("nakon 3. pokušaja %+v", p)
		}
		q := NovoProbijanjePozvanog(pozivProbe(natLak), !a, natLak, t0)
		q, _ = korak(t, q, Dogadjaj{Vrsta: DogadjajSinkro, Sad: t0, Poruka: sinkroProbe(1)}, RadnjaUdaraj)
		q, _ = korak(t, q, sat(t0.Add(5*time.Second)), RadnjaStani)
		if q.Faza != FazaCekaSinkro || q.Budjenje() != t0.Add(10*time.Second) {
			t.Fatalf("pozvani nakon 1. pokušaja %+v", q)
		}
		q, r = korak(t, q, Dogadjaj{Vrsta: DogadjajSinkro, Sad: t0.Add(6 * time.Second), Poruka: sinkroProbe(2)}, RadnjaUdaraj)
		if want := map[bool]time.Duration{true: time.Second}[a]; r[0].Od != t0.Add(6*time.Second+want) {
			t.Errorf("pozvani (A %v) u 2. pokušaju kreće u %s", !a, r[0].Od.Sub(t0))
		}
	}
}

// QUIC kreće na prvi valjan paket. S javne adrese čeka se do 500 ms
// lokalni, kad ga druga strana nudi; lokalni koji stigne pobjeđuje, inače
// ide javni. Bez lokalnih kandidata QUIC kreće odmah.
func TestLokalniPobjedujeJavni(t *testing.T) {
	odziv := odzivProbe(natLak, javniKandidat, lokalniKandidat)
	p, r := pozivateljUPokusaju(t, true, odziv)
	pocetak := r[1].Od
	javni := Valjan{Adresa: javnaProbe, Kad: pocetak.Add(100 * time.Millisecond)}
	p, _ = korak(t, p, javni.Dogadjaj())
	if p.Faza != FazaCekaLokalni || p.Budjenje() != javni.Kad.Add(500*time.Millisecond) {
		t.Fatalf("čeka lokalni: %+v", p)
	}
	q, _ := korak(t, p, Dogadjaj{Vrsta: DogadjajUdarac, Sad: javni.Kad.Add(time.Millisecond), Adresa: netip.MustParseAddrPort("203.0.113.9:4713")}) // još jedan javni ne mijenja ništa
	q, _ = korak(t, q, sat(javni.Kad.Add(499*time.Millisecond)))
	q, r = korak(t, q, Valjan{Adresa: lanProbe, Lokalni: true, Kad: javni.Kad.Add(300 * time.Millisecond)}.Dogadjaj(), RadnjaQUIC)
	if r[0].Adresa != lanProbe || q.Faza != FazaQUIC || q.Budjenje() != pocetak.Add(5*time.Second) {
		t.Errorf("lokalni: %+v, %+v", r, q)
	}
	q, r = korak(t, p, sat(javni.Kad.Add(500*time.Millisecond)), RadnjaQUIC)
	if r[0].Adresa != javnaProbe || q.Faza != FazaQUIC {
		t.Errorf("javni nakon 500 ms: %+v", r)
	}
	// čekanje lokalnog ne prelazi kraj pokušaja
	kasno := Valjan{Adresa: javnaProbe, Kad: pocetak.Add(4800 * time.Millisecond)}
	if q, _ = korak(t, p0(t, odziv), kasno.Dogadjaj()); q.Budjenje() != pocetak.Add(5*time.Second) {
		t.Errorf("čeka do %s", q.Budjenje().Sub(pocetak))
	}
	q, r = korak(t, p0(t, odzivProbe(natLak, javniKandidat)), javni.Dogadjaj(), RadnjaQUIC)
	if r[0].Adresa != javnaProbe {
		t.Errorf("bez lokalnih: %+v", r)
	}
	q, r = korak(t, p0(t, odziv), Valjan{Adresa: lanProbe, Lokalni: true, Kad: pocetak}.Dogadjaj(), RadnjaQUIC)
	if r[0].Adresa != lanProbe {
		t.Errorf("lokalni prvi: %+v", r)
	}
}

func p0(t *testing.T, odziv TijeloPoruke) Probijanje {
	t.Helper()
	p, _ := pozivateljUPokusaju(t, true, odziv)
	return p
}

// Uspio QUIC završava probijanje; neuspio vraća na udarce, pa QUIC kreće
// ponovno na valjan paket s adrese koja se u ovom pokušaju još nije javila
// (Udarci istu adresu u pokušaju javljaju jednom), a u idućem pokušaju i
// na istu. Pokušaj koji istekne usred QUIC-a prekida ga.
func TestQUICUPokusaju(t *testing.T) {
	p := p0(t, odzivProbe(natLak, javniKandidat))
	v := Valjan{Adresa: javnaProbe, Kad: t0.Add(time.Second)}
	q, _ := korak(t, p, v.Dogadjaj(), RadnjaQUIC)
	gotov, r := korak(t, q, Dogadjaj{Vrsta: DogadjajQUIC, Sad: t0.Add(2 * time.Second), Uspjeh: true}, RadnjaStani, RadnjaKraj)
	if gotov.Faza != FazaUspjeh || !r[1].Uspjeh || !gotov.Zavrseno() {
		t.Errorf("uspjeh %+v", gotov)
	}
	if g, r := gotov.Prijelaz(sat(t0.Add(time.Hour))); g != gotov || r != nil {
		t.Error("završeno probijanje se mijenja")
	}
	q, _ = korak(t, q, Dogadjaj{Vrsta: DogadjajQUIC, Sad: t0.Add(2 * time.Second)})
	if q.Faza != FazaUdara {
		t.Fatalf("nakon neuspjelog QUIC-a %+v", q)
	}
	druga := Valjan{Adresa: netip.MustParseAddrPort("203.0.113.9:4713"), Kad: t0.Add(2500 * time.Millisecond)}
	if q, r = korak(t, q, druga.Dogadjaj(), RadnjaQUIC); r[0].Adresa != druga.Adresa {
		t.Errorf("druga adresa: %+v", r)
	}
	q, r = korak(t, q, sat(q.Budjenje()), RadnjaPrekiniQUIC, RadnjaSinkro, RadnjaUdaraj)
	if q.Pokusaj != 2 || r[1].Pokusaj != 2 {
		t.Errorf("idući pokušaj %+v", r)
	}
	if q, _ = korak(t, q, sat(q.Budjenje().Add(-time.Millisecond))); q.Pokusaj != 2 {
		t.Error("sat prije roka mijenja pokušaj")
	}
	v.Kad = r[2].Od
	if _, r = korak(t, q, v.Dogadjaj(), RadnjaQUIC); r[0].Adresa != javnaProbe {
		t.Errorf("ista adresa u 2. pokušaju: %+v", r)
	}
}

// Sinkro se prima samo za vlastitu sesiju, od druge strane, kod pozvanoga,
// s pokušajem većim od zadnjeg i u granici: ponovljeni sinkro 1. pokušaja
// nakon 2. se odbacuje. Sinkro usred QUIC-a prekida ga.
func TestSinkroSamoZaSvojuSesiju(t *testing.T) {
	q := NovoProbijanjePozvanog(pozivProbe(natLak, javniKandidat), false, natLak, t0)
	q, _ = korak(t, q, Dogadjaj{Vrsta: DogadjajSinkro, Sad: t0, Poruka: sinkroProbe(1)}, RadnjaUdaraj)
	q, _ = korak(t, q, Dogadjaj{Vrsta: DogadjajSinkro, Sad: t0.Add(5 * time.Second), Poruka: sinkroProbe(2)}, RadnjaUdaraj)
	tudjaSesija, tudjiPosiljatelj, ponovljen, previse := sinkroProbe(3), sinkroProbe(3), sinkroProbe(1), sinkroProbe(4)
	tudjaSesija.Sesija = "ffeeddccbbaa99887766554433221100"
	tudjiPosiljatelj.Od = "C"
	for ime, s := range map[string]TijeloPoruke{"tuđa sesija": tudjaSesija, "tuđi pošiljatelj": tudjiPosiljatelj, "ponovljen 1. nakon 2.": ponovljen, "isti pokušaj": sinkroProbe(2), "4. pokušaj": previse} {
		if q.PrimaSinkro(s) {
			t.Errorf("%s: prima", ime)
		}
		korak(t, q, Dogadjaj{Vrsta: DogadjajSinkro, Sad: t0.Add(6 * time.Second), Poruka: s})
	}
	if !q.PrimaSinkro(sinkroProbe(3)) {
		t.Error("3. pokušaj se ne prima")
	}
	q, _ = korak(t, q, Valjan{Adresa: javnaProbe, Kad: t0.Add(6 * time.Second)}.Dogadjaj(), RadnjaQUIC)
	q, r := korak(t, q, Dogadjaj{Vrsta: DogadjajSinkro, Sad: t0.Add(7 * time.Second), Poruka: sinkroProbe(3)}, RadnjaPrekiniQUIC, RadnjaUdaraj)
	if q.Pokusaj != 3 || r[1].Od != t0.Add(7*time.Second) {
		t.Errorf("3. pokušaj pozvanoga B, koji ne kasni: %+v", r)
	}
	p := p0(t, odzivProbe(natLak))
	if p.PrimaSinkro(TijeloPoruke{Sesija: sesijaUdaraca, Od: "B", Pokusaj: 2}) {
		t.Error("pozivatelj prima sinkro")
	}
	gotov, _ := q.neuspjeh(RazlogBezPuta)
	if gotov.PrimaSinkro(sinkroProbe(3)) {
		t.Error("završeno probijanje prima sinkro")
	}
}

// Odziv se prima samo kod pozivatelja koji ga čeka, za vlastitu sesiju i
// od druge strane: ni tuđ, ni ponovljen nakon prvoga, ni kod pozvanoga.
func TestOdzivSamoZaSvojuSesiju(t *testing.T) {
	p := NovoProbijanjePozivatelja(pozivProbe(natLak), true, t0)
	o := odzivProbe(natLak)
	if !p.PrimaOdziv(o) {
		t.Error("pozivatelj ne prima svoj odziv")
	}
	tudjaSesija, tudjiPosiljatelj := o, o
	tudjaSesija.Sesija = "ffeeddccbbaa99887766554433221100"
	tudjiPosiljatelj.Od = "C"
	nakon, _ := korak(t, p, Dogadjaj{Vrsta: DogadjajOdziv, Sad: t0.Add(60 * time.Millisecond), Poruka: o}, RadnjaSinkro, RadnjaUdaraj)
	pozvani := NovoProbijanjePozvanog(pozivProbe(natLak), false, natLak, t0)
	for ime, c := range map[string]struct {
		p Probijanje
		o TijeloPoruke
	}{
		"tuđa sesija":      {p, tudjaSesija},
		"tuđi pošiljatelj": {p, tudjiPosiljatelj},
		"nakon odziva":     {nakon, o},
		"pozvani":          {pozvani, TijeloPoruke{Sesija: sesijaUdaraca, Od: "A"}},
	} {
		if c.p.PrimaOdziv(c.o) {
			t.Errorf("%s: prima", ime)
		}
	}
}

// Odziv tuđe sesije ili tuđeg pošiljatelja ne mijenja ništa; odziv „odbij”
// i UDP blokiran kod jedne strane završavaju probijanje; bez odziva 10 s,
// a bez sinkra 5 s, probijanje nije uspjelo.
func TestOdzivIRokovi(t *testing.T) {
	p := NovoProbijanjePozivatelja(pozivProbe(natLak), true, t0)
	tudji := odzivProbe(natLak)
	tudji.Od = "C"
	korak(t, p, Dogadjaj{Vrsta: DogadjajOdziv, Sad: t0, Poruka: tudji})
	odbij := odzivProbe(natLak)
	odbij.Odluka, odbij.Razlog = OdlukaOdbij, "zauzet"
	for ime, c := range map[string]struct {
		d      Dogadjaj
		razlog string
	}{
		"odbij":        {Dogadjaj{Vrsta: DogadjajOdziv, Sad: t0, Poruka: odbij}, RazlogOdbijeno},
		"udp blokiran": {Dogadjaj{Vrsta: DogadjajOdziv, Sad: t0, Poruka: odzivProbe(&NATPoruke{UDPBlokiran: true})}, RazlogUDP},
		"bez odziva":   {sat(t0.Add(rokOdziva)), RazlogBezOdziva},
	} {
		q, _ := korak(t, p, c.d, RadnjaStani, RadnjaKraj)
		if q.Faza != FazaNeuspjeh || q.Razlog != c.razlog {
			t.Errorf("%s: %+v", ime, q)
		}
	}
	korak(t, p, sat(t0.Add(rokOdziva-time.Millisecond)))
	korak(t, p, Dogadjaj{Vrsta: DogadjajUdarac, Sad: t0}) // udarac prije odziva ne radi ništa
	q := NovoProbijanjePozvanog(pozivProbe(natLak), false, natLak, t0)
	if q, _ = korak(t, q, sat(t0.Add(rokSinkra)), RadnjaStani, RadnjaKraj); q.Razlog != RazlogBezSinkra {
		t.Errorf("bez sinkra %+v", q)
	}
	if q := NovoProbijanjePozvanog(pozivProbe(&NATPoruke{UDPBlokiran: true}), false, natLak, t0); q.Faza != FazaNeuspjeh || q.Razlog != RazlogUDP {
		t.Errorf("pozvani uz blokiran UDP %+v", q)
	}
}
