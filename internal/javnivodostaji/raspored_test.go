package javnivodostaji

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// Raspored kruga: svaki sat u istoj minuti, a pri pokretanju odmah samo
// kad je krug zadnjeg termina propušten ili mu prognoza nije izdana.
// Testovi idu na probnom satu (synctest), koji počinje 1. 1. 2000. u ponoć
// UTC-a; minuta kruga je 2.

// u je trenutak hh:mm prvog dana probnog sata
func u(hh, mm int) time.Time { return time.Date(2000, 1, 1, hh, mm, 0, 0, time.UTC) }

// biljegU zapiše biljeg s vremenom zadnjeg preuzimanja i vrati njegovu
// putanju; nula znači da biljega nema
func biljegU(t *testing.T, zadnje time.Time) string {
	t.Helper()
	put := filepath.Join(t.TempDir(), "zadnje-preuzimanje.txt")
	if !zadnje.IsZero() {
		if err := os.WriteFile(put, []byte(zadnje.Format(time.RFC3339)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return put
}

// biljezi omota NakonPreuzimanja tako da pamti kad je koji krug došao do
// izračuna prognoze; vraća popis tih trenutaka (hh:mm)
func biljezi(u *Uvoznik) func() []string {
	var mu sync.Mutex
	var kad []string
	prije := u.NakonPreuzimanja
	u.NakonPreuzimanja = func(ctx context.Context) {
		mu.Lock()
		kad = append(kad, time.Now().UTC().Format("15:04"))
		mu.Unlock()
		if prije != nil {
			prije(ctx)
		}
	}
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(kad)
	}
}

// vrti pokrene raspored u trenutku od i pusti ga da radi do trenutka do.
// Zove se unutar synctest.Test.
func vrti(u *Uvoznik, od, do time.Time) {
	time.Sleep(time.Until(od))
	ctx, stani := context.WithCancel(context.Background())
	gotovo := make(chan struct{})
	go func() {
		u.Pokreni(ctx)
		close(gotovo)
	}()
	time.Sleep(time.Until(do))
	stani()
	<-gotovo
}

// krugovi pokrene raspored od–do i vrati kad je koji krug prošao
func krugovi(u *Uvoznik, od, do time.Time) []string {
	kad := biljezi(u)
	vrti(u, od, do)
	return kad()
}

func TestTerminiKruga(t *testing.T) {
	for _, s := range []struct {
		sada          time.Time
		iduci, zadnji time.Time
	}{
		{u(13, 28), u(14, 2), u(13, 2)},
		{u(13, 1), u(13, 2), u(12, 2)},
		{u(13, 2), u(14, 2), u(13, 2)}, // u samom terminu: taj je zadnji
		{u(23, 59), u(0, 2).Add(24 * time.Hour), u(23, 2)},
	} {
		if g := iduciTermin(s.sada, 2); !g.Equal(s.iduci) {
			t.Errorf("idući termin iza %s: %s, želim %s", s.sada.Format("15:04"), g.Format("15:04"), s.iduci.Format("15:04"))
		}
		if g := zadnjiTermin(s.sada, 2); !g.Equal(s.zadnji) {
			t.Errorf("zadnji termin do %s: %s, želim %s", s.sada.Format("15:04"), g.Format("15:04"), s.zadnji.Format("15:04"))
		}
	}
	// minuta 0 je pun sat
	if g := iduciTermin(u(13, 28), 0); !g.Equal(u(14, 0)) {
		t.Errorf("minuta 0: %s", g.Format("15:04"))
	}
	// lokalno vrijeme ne mijenja minutu: 15:28 u Zagrebu ljeti je 13:28 UTC
	zagreb := time.FixedZone("CEST", 2*3600)
	if g := iduciTermin(u(13, 28).In(zagreb), 2); g.In(zagreb).Format("15:04") != "16:02" {
		t.Errorf("po zagrebačkom vremenu: %s", g.In(zagreb).Format("15:04"))
	}
}

// Pokretanje u 13:28, kao 18. 10. 2026. nakon nadogradnje kućnog servera.
// Kad su letve u ovom satu već preuzete (13:02) i prognoza izdana, krug ne
// ide odmah ni minutu kasnije, nego u 14:02 i dalje svaki sat u :02. Inače
// odmah, pa opet po satu.
func TestRasporedKruga(t *testing.T) {
	for _, s := range []struct {
		ime        string
		pocetak    time.Time
		zadnje     time.Time // zadnje preuzimanje; nula: nema biljega
		nedovrseno bool      // prognoza za sat zadnjeg termina nije izdana
		zeli       []string
	}{
		{"preuzeto u ovom satu: čeka idući termin", u(13, 28), u(13, 2), false, []string{"14:02", "15:02"}},
		{"preuzeto prije zadnjeg termina: odmah", u(13, 28), u(12, 2), false, []string{"13:28", "14:02", "15:02"}},
		{"bez biljega: odmah", u(13, 28), time.Time{}, false, []string{"13:28", "14:02", "15:02"}},
		{"biljeg iz budućnosti ne vrijedi", u(13, 28), u(17, 0), false, []string{"13:28", "14:02", "15:02"}},
		{"prognoza ovog sata nije izdana: odmah", u(13, 28), u(13, 2), true, []string{"13:28", "14:02", "15:02"}},
		// prije termina ovog sata vrijedi krug prošlog sata, a ovaj je za minutu
		{"pokretanje prije termina", u(13, 1), u(12, 2), false, []string{"13:02", "14:02", "15:02"}},
	} {
		t.Run(s.ime, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				uv := NoviUvoznik(&probnoSpremiste{}, nil)
				uv.Minuta, uv.Biljeg = 2, biljegU(t, s.zadnje)
				var pitano []string
				uv.Nedovrseno = func(sat time.Time) bool {
					pitano = append(pitano, sat.UTC().Format("15:04"))
					return s.nedovrseno
				}
				if g := krugovi(uv, s.pocetak, u(15, 30)); !slices.Equal(g, s.zeli) {
					t.Errorf("krugovi u %v, želim %v", g, s.zeli)
				}
				// prognoza se provjerava za sat zadnjeg termina, i samo
				// kad su letve u tom terminu preuzete
				if s.zadnje.Equal(u(13, 2)) && !slices.Equal(pitano, []string{"13:00"}) {
					t.Errorf("izdanje se provjeravalo za %v, želim [13:00]", pitano)
				}
			})
		})
	}
}

// Krug iz rasporeda ne propada kad u terminu traje krug pokrenut rukom:
// pričeka ga i ide odmah iza njega. Ručni je krenuo u 14:01, prije nego što
// su izvori objavili 14 h, pa bez ovoga 14 h ne bi imao svoj krug.
func TestKrugIzRasporedaCekaRucni(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		uv := NoviUvoznik(&probnoSpremiste{}, nil)
		uv.Minuta, uv.Biljeg = 2, biljegU(t, u(13, 2))
		// ručni krug računa prognozu tri minute
		uv.NakonPreuzimanja = func(context.Context) {
			if time.Now().Before(u(14, 2)) {
				time.Sleep(3 * time.Minute)
			}
		}
		kad := biljezi(uv)
		rucni := make(chan struct{})
		go func() {
			time.Sleep(time.Until(u(14, 1)))
			uv.PreuzmiSve(context.Background())
			close(rucni)
		}()
		vrti(uv, u(13, 28), u(15, 30))
		<-rucni
		if g, zeli := kad(), []string{"14:01", "14:04", "15:02"}; !slices.Equal(g, zeli) {
			t.Errorf("krugovi u %v, želim %v", g, zeli)
		}
	})
}

// Čvor se gasi dok krug iz rasporeda čeka ručni: čekanje prestaje odmah, a
// krug iz rasporeda ne ide.
func TestGasenjeDokKrugCekaRucni(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		uv := NoviUvoznik(&probnoSpremiste{}, nil)
		pusti := make(chan struct{})
		uv.NakonPreuzimanja = func(context.Context) { <-pusti }
		kad := biljezi(uv)
		rucni := make(chan struct{})
		go func() {
			uv.PreuzmiSve(context.Background())
			close(rucni)
		}()
		synctest.Wait()
		ctx, ugasi := context.WithCancel(context.Background())
		gotovo := make(chan struct{})
		go func() {
			uv.krugPoRasporedu(ctx)
			close(gotovo)
		}()
		time.Sleep(time.Minute)
		ugasi()
		<-gotovo
		close(pusti)
		<-rucni
		if g := kad(); len(g) != 1 {
			t.Errorf("krugovi u %v, a nakon gašenja smije proći samo ručni", g)
		}
	})
}

// Krug dulji od sata (nema interneta, svaka letva čeka svoj rok) ne povlači
// za sobom sve propuštene termine: iza njega ide jedan krug odmah, pa dalje
// po satu.
func TestPredugKrugNePovlaciSvePropustene(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		uv := NoviUvoznik(&probnoSpremiste{}, nil)
		uv.Minuta, uv.Biljeg = 2, biljegU(t, u(12, 2))
		prvi := true
		uv.NakonPreuzimanja = func(context.Context) {
			if prvi {
				prvi = false
				time.Sleep(150 * time.Minute) // 13:28 → 15:58
			}
		}
		if g, zeli := krugovi(uv, u(13, 28), u(17, 30)), []string{"13:28", "15:58", "16:02", "17:02"}; !slices.Equal(g, zeli) {
			t.Errorf("krugovi u %v, želim %v", g, zeli)
		}
	})
}

// Biljeg se zapiše tek kad su sve letve došle na red, s vremenom početka
// kruga; prekinut krug ga ne pomiče, pa ponovno pokretanje preuzme odmah.
func TestBiljegTekKadSuLetvePreuzete(t *testing.T) {
	uv, _ := uvoznikJedneLetve()
	uv.Biljeg = biljegU(t, time.Time{})
	rok, otkazi := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer otkazi()
	uv.PreuzmiSve(rok)
	if z, ima := uv.zadnjePreuzimanje(time.Now()); ima {
		t.Errorf("prekinut krug zapamćen kao preuzimanje u %s", z)
	}
	pocetak := time.Now().Truncate(time.Second)
	uv.PreuzmiSve(context.Background())
	z, ima := uv.zadnjePreuzimanje(time.Now())
	if !ima || z.Before(pocetak) || z.After(time.Now()) {
		t.Errorf("zapamćeno %s (ima=%v), krug je počeo %s", z, ima, pocetak)
	}

	// oštećen biljeg (npr. nestanak struje usred zapisa) ne vrijedi, pa krug
	// pri pokretanju ide odmah
	if err := os.WriteFile(uv.Biljeg, []byte("2026-10-18T13:0"), 0o600); err != nil {
		t.Fatal(err)
	}
	if z, ima := uv.zadnjePreuzimanje(time.Now()); ima {
		t.Errorf("oštećen biljeg pročitan kao %s", z)
	}

	// biljeg koji se ne da zapisati javi se u dnevnik, a krug ide dalje
	var dnevnik []string
	uv.Zapisnik = func(f string, a ...any) { dnevnik = append(dnevnik, f) }
	uv.Biljeg = filepath.Join(t.TempDir(), "nema", "zadnje-preuzimanje.txt")
	uv.PreuzmiSve(context.Background())
	if !slices.ContainsFunc(dnevnik, func(r string) bool { return strings.Contains(r, "vrijeme preuzimanja nije zapamćeno") }) {
		t.Errorf("dnevnik: %q", dnevnik)
	}
}
