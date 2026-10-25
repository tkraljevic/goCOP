package razmjena

// Pravila pripreme WEB synca (web_sync.go): jedna priprema po računalu,
// istodobni pozivi, granica priprema pozvanoga, odbijanje, sastajalište bez
// druge strane, zamjena sesije i kraj rada. Računala su javna, bez NAT-a.

import (
	"context"
	"crypto/ed25519"
	"errors"
	"slices"
	"testing"
	"testing/synctest"
	"time"
)

// greskaPripreme je razlog neuspjele pripreme; prazno kad greška nije
// GreskaPripreme
func greskaPripreme(err error) string {
	var g GreskaPripreme
	if errors.As(err, &g) {
		return g.Razlog
	}
	return ""
}

// Sa samim sobom se ne priprema, druga priprema prema istom računalu čeka
// prvu, a kad WEB sync ne radi, priprema ne kreće.
func TestPripremaSeNePonavlja(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		a, b, posta := javniPar(t, m)
		if _, err := a.w.Pripremi(t.Context(), a.kljuc); !errors.Is(err, ErrSesijaSaSobom) {
			t.Errorf("sa samim sobom: %v", err)
		}
		posta.zadrziAko(func(pp PotpisanaPoruka) bool { return pp.Vrsta == VrstaOdziv })
		prva := make(chan error, 1)
		go func() {
			_, err := a.w.Pripremi(t.Context(), b.kljuc)
			prva <- err
		}()
		time.Sleep(time.Millisecond)
		if _, err := a.w.Pripremi(t.Context(), b.kljuc); !errors.Is(err, ErrPripremaUTijeku) {
			t.Errorf("druga priprema: %v", err)
		}
		if err := <-prva; greskaPripreme(err) != RazlogBezOdziva || err.Error() != "WEB sync nije uspio: bez-odziva" {
			t.Errorf("prva priprema bez odziva: %v", err)
		}
		a.zaustavi()
		if _, err := a.w.Pripremi(t.Context(), b.kljuc); !errors.Is(err, ErrWebSyncNeRadi) {
			t.Errorf("nakon zaustavljanja: %v", err)
		}
	})
}

// Dva računala zovu jedno drugo istodobno: vrijedi poziv računala s
// manjim ključem. Njegova priprema uspije, a druga strana svoju prekine
// (druga-zove) i sesiju dobije kao pozvana.
func TestPripremaIstodobno(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		a, b, _ := javniPar(t, m)
		ishodB := make(chan error, 1)
		go func() {
			_, err := b.w.Pripremi(t.Context(), a.kljuc)
			ishodB <- err
		}()
		s, err := a.w.Pripremi(t.Context(), b.kljuc)
		bezGreske(t, err)
		if err := <-ishodB; greskaPripreme(err) != RazlogDrugaZove {
			t.Errorf("B: %v", err)
		}
		if sB := b.sesijaIz(t); sB.Sesija != s.Sesija {
			t.Errorf("sesije %s i %s", s.Sesija, sB.Sesija)
		}
		if _, err := razgovorNa(t.Context(), s); err != nil {
			t.Error(err)
		}
	})
}

// Pozvani koji poziv ne prima (Prima) odgovori „odbij”, pa pozivatelj
// odmah zna; poziv iz kojeg se ključevi udaraca ne daju izvesti (bez
// tajne) također se odbija.
func TestPripremaOdbijena(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		a, b, posta := javniPar(t, m, func(p *PostavkeWeb) {
			p.Prima = func(ed25519.PublicKey) string { return "iskljucen" }
		})
		pocetak := time.Now()
		if _, err := a.w.Pripremi(t.Context(), b.kljuc); greskaPripreme(err) != RazlogOdbijeno || time.Since(pocetak) > time.Second {
			t.Errorf("odbijena priprema: %v nakon %v", err, time.Since(pocetak))
		}
		b.w.Prima = func(ed25519.PublicKey) string { return "" }
		poziv := a.w.potpisi(VrstaPoziv, NovaSesija(), PublicKeyString(b.kljuc), TijeloPoruke{})
		bezGreske(t, posta.posalji(poziv))
		time.Sleep(time.Second)
		if v := posta.vrste(); v[len(v)-1] != VrstaOdziv {
			t.Errorf("poruke: %v", v)
		}
		zadnji, _ := posta.poslano[len(posta.poslano)-1].Zaglavlje() // posta je zapisala ispravno tijelo
		if zadnji.Odluka != OdlukaOdbij || zadnji.Razlog != RazlogOdbijeno {
			t.Errorf("odziv na poziv bez tajne: %+v", zadnji)
		}
	})
}

// Sastajalište javlja da druge strane nema: priprema staje odmah, s
// razlogom sastajališta, a ne nakon 10 s.
func TestPripremaBezDrugeStrane(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		a, b, posta := javniPar(t, m)
		posta.zadrziAko(func(pp PotpisanaPoruka) bool {
			t, _ := pp.Zaglavlje() // posta je zaglavlje već pročitala
			go a.w.NaNemu(t.Sesija, NemaNijePrijavljen)
			return true
		})
		pocetak := time.Now()
		_, err := a.w.Pripremi(t.Context(), b.kljuc)
		if greskaPripreme(err) != RazlogNema+": "+NemaNijePrijavljen || time.Since(pocetak) > time.Second {
			t.Errorf("bez druge strane: %v nakon %v", err, time.Since(pocetak))
		}
		a.w.NaNemu("nepoznata", NemaNijePrijavljen) // nepoznata sesija ne smeta
	})
}

// Kraj rada prekida pripremu u tijeku (stao), a pozivatelj koji odustane
// dobije grešku svog konteksta.
func TestPripremaPrekinuta(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		a, b, posta := javniPar(t, m)
		posta.zadrziAko(func(pp PotpisanaPoruka) bool { return pp.Vrsta == VrstaOdziv })
		ishod := make(chan error, 1)
		go func() {
			_, err := a.w.Pripremi(t.Context(), b.kljuc)
			ishod <- err
		}()
		time.Sleep(time.Second)
		a.zaustavi()
		if err := <-ishod; greskaPripreme(err) != RazlogStao {
			t.Errorf("nakon kraja rada: %v", err)
		}
		time.Sleep(rokSinkra) // B, kao pozvani, prestane čekati sinkro
		ctx, otkazi := context.WithTimeout(t.Context(), time.Second)
		defer otkazi()
		if _, err := b.w.Pripremi(ctx, a.kljuc); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("odustao pozivatelj: %v", err)
		}
	})
}

// Slanje poziva ne uspije: priprema se odmah vrati s greškom i ne drži
// mjesto za iduću.
func TestPripremaBezSlanja(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		greska := errors.New("signal nije prijavljen")
		a, b, _ := javniPar(t, m, func(p *PostavkeWeb) {
			p.Posalji = func(PotpisanaPoruka) error { return greska }
		})
		for range 2 {
			if _, err := a.w.Pripremi(t.Context(), b.kljuc); !errors.Is(err, greska) {
				t.Errorf("bez slanja: %v", err)
			}
		}
	})
}

// Nova priprema s istim računalom zamijeni živu sesiju: stara se zatvori
// na obje strane, a upisana je nova.
func TestNovaSesijaZamijeniStaru(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		a, b, _ := javniPar(t, m)
		stara, staraB := sesijaPara(t, a, b)
		nova, novaB := sesijaPara(t, a, b)
		if !zatvorena(stara, 2*time.Second) || !zatvorena(staraB, time.Second) {
			t.Fatal("stara sesija je ostala otvorena")
		}
		synctest.Wait()
		if a.w.Sesija(b.kljuc) != nova || b.w.Sesija(a.kljuc) != novaB {
			t.Error("upisana je stara sesija")
		}
		if _, err := razgovorNa(t.Context(), nova); err != nil {
			t.Error(err)
		}
	})
}

// Pozvani ima najviše 4 pripreme odjednom: peti poziv dobije „zauzet”.
// Noviji poziv istog računala prekine stariji (novi-poziv), a poziv
// vlastitim ključem i poziv nečlana se ne primaju.
func TestPozivaNajviseCetiri(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		m := internetUdaraca(t)
		b := noviCvorWeb(t, stranaJavna(t, m, "198.51.100.2:4713", newKey(t)), novaPosta(t))
		var kljucevi []ed25519.PrivateKey
		var pripreme []*priprema
		for i := range najvisePripremaPozvanog + 1 {
			kljuc := newKey(t)
			kljucevi = append(kljucevi, kljuc)
			b.clan(kljuc.Public().(ed25519.PublicKey), true)
			odziv := pozoviRucno(t, b, kljuc, NovaSesija())
			want := OdlukaProbaj
			if i == najvisePripremaPozvanog {
				want = OdlukaOdbij
			}
			if odziv.Odluka != want || i == najvisePripremaPozvanog && odziv.Razlog != RazlogPripremaZauzeta {
				t.Errorf("%d. poziv: %+v", i+1, odziv)
			}
			if p := b.w.pripremaSesije(odziv.Sesija); p != nil {
				pripreme = append(pripreme, p)
			}
		}
		if len(pripreme) != najvisePripremaPozvanog {
			t.Fatalf("pripreme: %d", len(pripreme))
		}
		if odziv := pozoviRucno(t, b, kljucevi[0], NovaSesija()); odziv.Odluka != OdlukaProbaj || b.w.pripremaSesije(odziv.Sesija) == nil {
			t.Errorf("noviji poziv: %+v", odziv)
		}
		synctest.Wait()
		if c := context.Cause(pripreme[0].ctx); greskaPripreme(c) != RazlogNoviPoziv {
			t.Errorf("stari poziv: %v", c)
		}
		for ime, od := range map[string]ed25519.PrivateKey{"vlastiti ključ": b.priv, "nečlan": newKey(t)} {
			b.posta.mu.Lock()
			prije := len(b.posta.poslano)
			b.posta.mu.Unlock()
			b.w.NaPoruku(Potpisi(od, "testproto", VrstaPoziv, TijeloPoruke{Sesija: NovaSesija(), Za: PublicKeyString(b.kljuc),
				Sastajaliste: PublicKeyString(b.posta.javni()), Tajna: tajnaUdaraca}))
			time.Sleep(time.Second)
			if b.posta.mu.Lock(); len(b.posta.poslano) != prije {
				t.Errorf("%s: poziv je dobio odziv", ime)
			}
			b.posta.mu.Unlock()
		}
	})
}

// pozoviRucno pošalje računalu b poziv potpisan ključem od, sa sesijom, i
// vrati odziv koji je b poslao
func pozoviRucno(t *testing.T, b *cvorWeb, od ed25519.PrivateKey, sesija string) TijeloPoruke {
	t.Helper()
	b.posta.mu.Lock()
	prije := len(b.posta.poslano)
	b.posta.mu.Unlock()
	poziv := Potpisi(od, "testproto", VrstaPoziv, TijeloPoruke{Sesija: sesija, Za: PublicKeyString(b.kljuc), Sastajaliste: PublicKeyString(b.posta.javni()),
		Tajna: tajnaUdaraca, Kandidati: javniKandidati(udaracZ)})
	b.w.NaPoruku(poziv)
	for range 100 {
		time.Sleep(10 * time.Millisecond)
		b.posta.mu.Lock()
		poslano := slices.Clone(b.posta.poslano[prije:])
		b.posta.mu.Unlock()
		for _, pp := range poslano {
			o, _ := pp.Zaglavlje() // posta je zapisala ispravno tijelo
			if pp.Vrsta == VrstaOdziv && o.Sesija == sesija {
				return o
			}
		}
	}
	t.Fatal("odziv nije poslan")
	return TijeloPoruke{}
}
