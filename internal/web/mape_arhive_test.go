package web

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/arhiva"
)

func sadrziSve(t *testing.T, sto, tekst string, treba ...string) {
	t.Helper()
	for _, s := range treba {
		if !strings.Contains(tekst, s) {
			t.Errorf("%s nema %q:\n%s", sto, s, tekst)
		}
	}
}

// izMapeBaze pokrene test iz mape data unutar mape projekta, kao Postava;
// mape navedene u iznad nastanu u mapi projekta, gdje su stajale dok se čvor
// pokretao ručno. Vraća mapu projekta.
func izMapeBaze(t *testing.T, iznad ...string) string {
	t.Helper()
	projekt := t.TempDir()
	for _, m := range append([]string{"data"}, iznad...) {
		if err := os.MkdirAll(filepath.Join(projekt, m), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(filepath.Join(projekt, "data"))
	return projekt
}

// Uputa je ista kao u poruci odbijenog upisa (config.Mapa.Uputa), a uz nju
// stranica i dnevnik kažu kako se mapa zove, što se ne radi dok je drugdje i
// kad nastaje.
func TestRijeciZaMapuKojeNema(t *testing.T) {
	postavke := filepath.Join(t.TempDir(), "gocop.toml")
	stablo := NovaMapaArhive("podaci", "vodostaji", PostavkeCvora{Put: postavke})
	paketi := NovaMapaArhive("pakete", "pakete", PostavkeCvora{Zastavice: map[string]string{"pakete": "-pakete"}})
	if stablo.Postavke != postavke || stablo.Zastavica != "" || paketi.Zastavica != "-pakete" {
		t.Errorf("izvor: %+v %+v", stablo, paketi)
	}
	sadrziSve(t, "uputa", stablo.Uputa(), "Postavka podaci = 'vodostaji' ne počinje od korijena diska",
		"u "+postavke+" i ponovno pokrenite čvor")
	if stablo.Naziv() != "Stablo arhive" || paketi.Naziv() != "Mapa paketa" ||
		stablo.Dok() != "Dok se to ne riješi, uvoz u stablo ne zapisuje ništa." ||
		paketi.Dok() != "Dok se to ne riješi, paketi se ne izdaju." ||
		stablo.Nastaje() != "nastaje pri prvom upisu" || paketi.Nastaje() != "nastaje pri prvom izdavanju" {
		t.Errorf("nazivi: %q %q %q %q %q %q", stablo.Naziv(), paketi.Naziv(), stablo.Dok(), paketi.Dok(),
			stablo.Nastaje(), paketi.Nastaje())
	}
}

// Redak u dnevniku pri pokretanju uvijek nosi punu putanju; kad mape nema,
// upozorenje, a uz relativnu i uputu.
func TestRedakODnevnikuZaMapu(t *testing.T) {
	projekt := izMapeBaze(t, "vodostaji")
	nemaApsolutna := filepath.Join(projekt, "drugi-disk", "pakete")
	for _, s := range []struct {
		m     MapaArhive
		treba string
	}{
		{NovaMapaArhive("podaci", "", PostavkeCvora{}), "Stablo arhive: isključeno, čvor arhivu samo prima"},
		{NovaMapaArhive("pakete", "", PostavkeCvora{}), "Mapa paketa: isključena, čvor ne izdaje pakete"},
		{NovaMapaArhive("podaci", projekt, PostavkeCvora{}), "Stablo arhive: " + projekt},
		{NovaMapaArhive("pakete", nemaApsolutna, PostavkeCvora{}),
			"Mapa paketa: " + nemaApsolutna + " (mape još nema, nastaje pri prvom izdavanju)"},
	} {
		if g := s.m.Redak(); g != s.treba {
			t.Errorf("redak %q, treba %q", g, s.treba)
		}
	}
	drugdje := NovaMapaArhive("podaci", "vodostaji", PostavkeCvora{})
	if g, treba := drugdje.Redak(), "Stablo arhive: mape "+filepath.Join(projekt, "data", "vodostaji")+
		" nema. Dok se to ne riješi, uvoz u stablo ne zapisuje ništa. "+drugdje.Uputa(); g != treba {
		t.Errorf("redak %q, treba %q", g, treba)
	}
	nova := NovaMapaArhive("pakete", "pakete", PostavkeCvora{})
	if g, treba := nova.Redak(), "Mapa paketa: "+filepath.Join(projekt, "data", "pakete")+
		" (mape još nema, nastaje pri prvom izdavanju). "+nova.Uputa(); g != treba {
		t.Errorf("redak %q, treba %q", g, treba)
	}
}

// Poslužitelj uz uputu daje datoteku postavki i zastavicu; bez postavki uputa
// ne zna datoteku, ali i dalje vidi mapu.
func TestPosluziteljevaMapaArhive(t *testing.T) {
	projekt := izMapeBaze(t, "vodostaji")
	s := &Server{}
	m := s.mapaArhive("podaci", "vodostaji")
	if !m.Drugdje() || m.Prijedlog != filepath.Join(projekt, "vodostaji") || m.Postavke != "" || m.Zastavica != "" {
		t.Errorf("bez postavki: %+v", m)
	}
	s.SetPostavkeCvora(PostavkeCvora{Put: filepath.Join(projekt, "data", "gocop.toml"),
		Zastavice: map[string]string{"pakete": "-pakete"}})
	m = s.mapaArhive("pakete", "pakete")
	if m.Postavke != filepath.Join(projekt, "data", "gocop.toml") || m.Zastavica != "-pakete" || m.Drugdje() {
		t.Errorf("s postavkama: %+v", m)
	}
}

// vrataUMapiBaze su vrata Unosa u arhivu s relativnim stablom i mapom paketa,
// pokrenuta iz mape baze
func vrataUMapiBaze(t *testing.T, iznad ...string) (*okolinaVrata, string) {
	t.Helper()
	o := novaOkolinaVrata(t)
	projekt := izMapeBaze(t, iznad...)
	o.koren = "vodostaji"
	postavke := filepath.Join(projekt, "data", "gocop.toml")
	o.h.SetMape(func(kljuc, zadano string) MapaArhive {
		return NovaMapaArhive(kljuc, zadano, PostavkeCvora{Put: postavke})
	})
	o.h.SetIzdavanje(func() string { return "pakete" },
		func(string, bool, io.Writer) (arhiva.IzvjestajIzdanja, error) { return arhiva.IzvjestajIzdanja{}, nil },
		func() (arhiva.Katalog, error) { return arhiva.Katalog{}, nil }, nil)
	return o, projekt
}

// Laptop pod Postavom: stablo i paketi traže se u mapi baze, a stoje u mapi
// iznad. Stranica Unos u arhivu to kaže umjesto sitne greške o nizovima bez
// datoteke, s punom putanjom, prijedlogom i datotekom postavki, i ne nudi ni
// uvoz ni izdavanje.
func TestVrataKadSuStabloIPaketiDrugdje(t *testing.T) {
	o, projekt := vrataUMapiBaze(t, "vodostaji", "pakete")
	stranica := htmlUTekst(odgovor(o.h.ShowUvoz, httptestGet("/administracija/uvoz-niza"), o.pero, o.peroOvl).Body.String())
	postavke := filepath.Join(projekt, "data", "gocop.toml")
	sadrziSve(t, "stranica", stranica,
		"Stablo arhive nije pronađeno.", filepath.Join(projekt, "data", "vodostaji"),
		"Dok se to ne riješi, uvoz u stablo ne zapisuje ništa.",
		"Ako je to stablo ovog čvora, upišite podaci = '"+filepath.Join(projekt, "vodostaji")+"' u "+postavke+
			" i ponovno pokrenite čvor.",
		"Mapa paketa nije pronađena.", filepath.Join(projekt, "data", "pakete"),
		"Dok se to ne riješi, paketi se ne izdaju.")
	for _, ne := range []string{"Nizovi bez datoteke se ne daju provjeriti", "Pogledaj što je u njoj",
		"Na ovom čvoru još nije izdano nijedno izdanje", "Provjeri sve letve", "/administracija/izdavanje",
		"/administracija/uvoz-izvora/pregled"} {
		if strings.Contains(stranica, ne) {
			t.Errorf("stranica ipak ima %q", ne)
		}
	}
}

// Novi čvor (usluga na Linuxu, prvi čvor pod Postavom): stabla i paketa još
// nema, a ni iznad. Stranica kaže gdje će nastati i nudi uvoz i izdavanje
// kao i dosad.
func TestVrataNaNovomCvoru(t *testing.T) {
	o, projekt := vrataUMapiBaze(t)
	stranica := htmlUTekst(odgovor(o.h.ShowUvoz, httptestGet("/administracija/uvoz-niza"), o.pero, o.peroOvl).Body.String())
	sadrziSve(t, "stranica", stranica,
		"Stablo arhive još ne postoji.", "Čvor ga traži u <span class=\"mono\">"+filepath.Join(projekt, "data", "vodostaji"),
		"mapa nastaje pri prvom upisu.", "Ako stablo stoji drugdje, upišite podaci = '…' u",
		"Na ovom čvoru još nije izdano nijedno izdanje.", "(mape još nema, nastaje pri prvom izdavanju)",
		"Pogledaj što je u njoj", "Provjeri sve letve")
	for _, ne := range []string{"nije pronađeno", "nije pronađena", "Nizovi bez datoteke se ne daju provjeriti"} {
		if strings.Contains(stranica, ne) {
			t.Errorf("stranica ipak ima %q", ne)
		}
	}
}

// Postojeće stablo: stranica pokazuje punu putanju i nudi uvoz kao i prije.
func TestVrataSPostojecimStablom(t *testing.T) {
	o := novaOkolinaVrata(t)
	stranica := htmlUTekst(odgovor(o.h.ShowUvoz, httptestGet("/administracija/uvoz-niza"), o.pero, o.peroOvl).Body.String())
	sadrziSve(t, "stranica", stranica, "Stablo arhive: <span class=\"mono\">"+o.koren+"</span>", "Pogledaj što je u njoj")
	for _, ne := range []string{"Stablo arhive nije pronađeno.", "Stablo arhive još ne postoji."} {
		if strings.Contains(stranica, ne) {
			t.Errorf("postojeće stablo: stranica ima %q", ne)
		}
	}
}
