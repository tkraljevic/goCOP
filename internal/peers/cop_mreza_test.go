package peers

// Paket izdanja (.cop) ugrađuje se samo iz vlastite mreže. Čvor koji još
// nije u mreži ugrađuje samo paket potpisan vlastitim ključem. Imena čvorova
// su izmišljena.

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/obracun"
	"gocop/internal/repository"
)

// paketSBlagdanom: čvor upiše blagdan i izda ga u paketu
func paketSBlagdanom(t *testing.T, s *Service, p obracun.Pravilo) string {
	t.Helper()
	if err := repository.NewObracunRepository(s.db, s.rec).SaveBlagdan(ctxRub, p); err != nil {
		t.Fatal(err)
	}
	_, put := izvezi(t, s, CopObuhvat{Vrsta: "ocitanja", AreaID: 16}, []string{""}, false)
	return put
}

// imaBlagdan javlja je li blagdan u knjizi i na površini čvora
func imaBlagdan(t *testing.T, s *Service, p obracun.Pravilo) (uKnjizi, naPovrsini bool) {
	t.Helper()
	_, err := s.rec.Latest(ctxRub, repository.EntityBlagdani, p.ID)
	if err != nil && !errors.Is(err, ledger.ErrNoVersion) {
		t.Fatal(err)
	}
	ps, errP := repository.NewObracunRepository(s.db, s.rec).Blagdani(ctxRub)
	if errP != nil {
		t.Fatal(errP)
	}
	return err == nil, slices.ContainsFunc(ps, func(b obracun.Pravilo) bool { return b.ID == p.ID })
}

var (
	blagdanIzvana = obracun.Pravilo{ID: "probni-blagdan", Naziv: "Probni blagdan", Vrsta: obracun.Stalni, Mjesec: 3, Dan: 9}
	blagdanSvoj   = obracun.Pravilo{ID: "svoj-blagdan", Naziv: "Svoj blagdan", Vrsta: obracun.Stalni, Mjesec: 4, Dan: 11}
)

// Čvor koji još nije u mreži paket drugog izdavača ne ugrađuje, pa ga ni
// nakon ulaska u mrežu nema što poslati dalje. Vlastiti paket (isti ključ
// čvora, nova baza) ugrađuje.
func TestUvozCopaBezMrezeSamoVlastiti(t *testing.T) {
	izdavac := rubniCvor(t, "laptop-izvan")
	put := paketSBlagdanom(t, izdavac, blagdanIzvana)

	laptop := rubniCvor(t, "novi-laptop")
	sPovrsinom(laptop)
	laptop.SetWantsAll(true)
	rep, err := laptop.UveziCop(ctxRub, put)
	if err == nil || !strings.Contains(err.Error(), "paket je izdalo računalo laptop-izvan. Ovo računalo još nije u mreži, pa ne može provjeriti je li laptop-izvan u istoj mreži") ||
		!rep.PotpisValjan || rep.IzdavacClan || rep.Novih != 0 {
		t.Fatalf("paket drugog izdavača na čvoru bez mreže: %+v %v", rep, err)
	}
	if k, p := imaBlagdan(t, laptop, blagdanIzvana); k || p {
		t.Fatalf("zapis iz paketa drugog izdavača je ugrađen: u knjizi %v, na površini %v", k, p)
	}

	// laptop uđe u mrežu i razmijeni sve: poslužitelj ne dobije zapis
	server := rubnaMreza(t, "cop-osijek")
	sPovrsinom(server)
	server.SetWantsAll(true)
	primiIzravno(t, server, laptop)
	razgovaraj(t, laptop, server)
	if k, _ := imaBlagdan(t, server, blagdanIzvana); k {
		t.Fatal("zapis iz paketa drugog izdavača stigao je razmjenom")
	}

	// isto računalo s novom bazom i istim ključem ugradi svoj paket
	vlastiti := paketSBlagdanom(t, izdavac, blagdanSvoj)
	obnovljen := rubniCvor(t, "laptop-izvan")
	obnovljen.node.key = izdavac.node.key
	sPovrsinom(obnovljen)
	obnovljen.SetWantsAll(true)
	if rep, err := obnovljen.UveziCop(ctxRub, vlastiti); err != nil || !rep.PotpisValjan || rep.IzdavacClan || rep.Novih < 1 {
		t.Fatalf("vlastiti paket na čvoru bez mreže: %+v %v", rep, err)
	}
	if k, p := imaBlagdan(t, obnovljen, blagdanSvoj); !k || !p {
		t.Fatalf("vlastiti paket: u knjizi %v, na površini %v", k, p)
	}
}

// Čvor u mreži ugrađuje paket člana svoje mreže, a paket člana druge mreže
// odbija.
func TestUvozCopaSamoIzSvojeMreze(t *testing.T) {
	server := rubnaMreza(t, "cop-osijek")
	clan := rubniCvor(t, "ured-vukovar")
	laptop := rubniCvor(t, "laptop-baranja")
	primiIzravno(t, server, laptop)
	if err := laptop.saveMembership(ctxRub, primiIzravno(t, server, clan)); err != nil {
		t.Fatal(err)
	}
	laptop.SetWantsAll(true)

	drugaMreza := rubnaMreza(t, "cop-zagreb")
	obuhvat, kanal := CopObuhvat{Vrsta: "ocitanja", AreaID: 16}, "ocitanja/16/2026"
	upisiOcitanja(t, drugaMreza, kanal, "r-zagreb")
	_, tudji := izvezi(t, drugaMreza, obuhvat, []string{kanal}, false)
	rep, err := laptop.UveziCop(ctxRub, tudji)
	if err == nil || !strings.Contains(err.Error(), "cop-zagreb koji nije član") || rep.IzdavacClan || rep.Novih != 0 {
		t.Fatalf("paket druge mreže: %+v %v", rep, err)
	}

	upisiOcitanja(t, clan, kanal, "r-vukovar")
	_, svoj := izvezi(t, clan, obuhvat, []string{kanal}, false)
	if rep, err := laptop.UveziCop(ctxRub, svoj); err != nil || !rep.IzdavacClan || rep.Novih != 1 {
		t.Fatalf("paket člana iste mreže: %+v %v", rep, err)
	}
}

func TestVlastitiKljuc(t *testing.T) {
	a, b := rubniCvor(t, "cvor-a"), rubniCvor(t, "cvor-b")
	if !a.vlastitiKljuc(a.javni()) || a.vlastitiKljuc(b.javni()) || a.vlastitiKljuc(nil) {
		t.Fatal("vlastiti ključ se ne prepoznaje")
	}
	a.node.key = nil
	if a.vlastitiKljuc(b.javni()) || a.vlastitiKljuc(nil) {
		t.Fatal("čvor bez ključa prepoznaje tuđi ključ kao svoj")
	}
}
