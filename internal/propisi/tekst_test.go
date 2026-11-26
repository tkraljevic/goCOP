package propisi

import (
	"reflect"
	"strings"
	"testing"

	"gocop/internal/models"
)

func podijeliTekst(tekst string) Podjela {
	return Podijeli(ProcitajTekst(tekst).Odlomci)
}

// Odluke (25. 11. 2026.): zalijepljen tekst nema klasa NN, pa se članak
// prepoznaje po retku „Članak 5.”, naslov članka po retku iznad njega, a
// stavci po oznaci „(1)”. Što je iznad „Na temelju …”, zaglavlje je
// stranice s koje je tekst kopiran.
func TestTekstClanciIStavci(t *testing.T) {
	p := podijeliTekst("Početna stranica\nNN 12/2001\nNa temelju članka 7. Zakona o čuvanju nasipa ministar donosi\n" +
		"PRAVILNIK\nO ČUVARIMA\nPredmet\nČlanak 1.\n(1) Prvi stavak.\n(2) Drugi stavak:\n1. točka;\n2. točka.\nČlanak 2.\nBez stavaka.")
	zeli := []string{"uvod", "clanak-1", "clanak-2"}
	if got := oznake(p.Dijelovi); !reflect.DeepEqual(got, zeli) || len(p.Upozorenja) != 0 {
		t.Fatalf("oznake %v (upozorenja %+v), želi %v", got, p.Upozorenja, zeli)
	}
	if p := podijeliTekst("Samo tekst bez oznaka.\nI još jedan redak."); !reflect.DeepEqual(oznake(p.Dijelovi), []string{"odlomak-1"}) {
		t.Errorf("tekst bez oznaka: %v", oznake(p.Dijelovi))
	}
	c := p.Dijelovi[1]
	if c.Naslov != "Predmet" || len(c.Stavci) != 2 || c.Stavci[1].Oznaka != "(2)" || len(c.Stavci[1].Tocke) != 2 {
		t.Errorf("članak 1. %+v", c)
	}
}

// Odluke (25. 11. 2026.): u zalijepljenom Planu redak „XXIII.” je točka, a
// redak velikim slovima ispred točke naslov dijela.
func TestTekstTockePlana(t *testing.T) {
	p := podijeliTekst("PLAN OBRANE PRIMJEROVA\nUVODNE ODREDBE\nI.\nOvaj Plan uređuje:\n– stadije,\n– mjere.\nSTADIJI\nII.\nTekst.\nIII.\nTekst.")
	zeli := []string{"dio-1", "i", "dio-2", "ii", "iii"}
	if got := oznake(p.Dijelovi); !reflect.DeepEqual(got, zeli) {
		t.Fatalf("oznake %v, želi %v", got, zeli)
	}
	if d := p.Dijelovi[0]; d.Vrsta != models.DioPlana || d.Naslov != "UVODNE ODREDBE" {
		t.Errorf("dio %+v", d)
	}
}

// Odluke (25. 11. 2026.): redak velikim slovima je naslov akta kad je iza
// njega još jedan takav redak ili nastavak „O …”, a inače naslov glave ili
// dijela. Glava s rimskim brojem i odjeljak ispred naslova članka
// prepoznaju se po broju.
func TestTekstNaslovVelikimSlovima(t *testing.T) {
	p := podijeliTekst("ZAKON\nO ČUVANJU NASIPA\nI. OPĆE ODREDBE\n1. Predmet\nNaslov članka\nČlanak 1.\nTekst.\n" +
		"PRIJELAZNE ODREDBE\nČlanak 2.\nTekst.")
	zeli := []string{"glava-i", "glava-i-1", "clanak-1", "glava-2", "clanak-2"}
	if got := oznake(p.Dijelovi); !reflect.DeepEqual(got, zeli) || len(p.Upozorenja) != 0 {
		t.Fatalf("oznake %v (upozorenja %+v), želi %v", got, p.Upozorenja, zeli)
	}
	if g := p.Dijelovi[3]; g.Naslov != "PRIJELAZNE ODREDBE" || g.Broj != "" {
		t.Errorf("glava bez broja %+v", g)
	}
	if velikimSlovima("AB") || velikimSlovima("Abc DEF") || !velikimSlovima("ČĆŽ 12") {
		t.Error("velika slova")
	}
}

// Naslov akta u jednom retku nije glava: prije su svi članci padali pod
// glavu s naslovom akta. Isti redak velikim slovima iza članka ostaje glava.
func TestTekstNaslovAktaUJednomRetku(t *testing.T) {
	p := podijeliTekst("Na temelju članka 7. Zakona o čuvanju nasipa ministar donosi\nPRAVILNIK O ČUVANJU NASIPA\n" +
		"Predmet\nČlanak 1.\nTekst.\nČlanak 2.\nTekst.\nPLAN RADA\nČlanak 3.\nTekst.")
	zeli := []string{"uvod", "clanak-1", "clanak-2", "glava-1", "clanak-3"}
	if got := oznake(p.Dijelovi); !reflect.DeepEqual(got, zeli) {
		t.Fatalf("oznake %v, želi %v", got, zeli)
	}
	if p.Dijelovi[1].Roditelj != "" || p.Dijelovi[4].Roditelj != "glava-1" {
		t.Errorf("roditelji %q %q", p.Dijelovi[1].Roditelj, p.Dijelovi[4].Roditelj)
	}
}

// Odluke (25. 11. 2026.): datoteka s Windowsa ima BOM i CRLF, a stari Mac
// samo CR; svaki neprazan redak je odlomak.
func TestTekstCRLFIBOM(t *testing.T) {
	p := ProcitajTekst("\ufeffČlanak 1.\r\n(1) Tekst.\r\r\n  \rDrugi\u00a0redak\n")
	if got := tekstovi(p.Odlomci); !reflect.DeepEqual(got, []string{"Članak 1.", "(1) Tekst.", "Drugi redak"}) {
		t.Errorf("odlomci %q", got)
	}
	if p.Odlomci[0].Klasa != "" {
		t.Error("odlomak teksta ima klasu")
	}
}

// Odluke (25. 11. 2026.): isti akt kao stranica NN i kao zalijepljen tekst
// daje iste stalne adrese dijelova, pa se propis može učitati na oba načina.
// Zalijepljen tekst i odluku o proglašenju i potpise prepoznaje po obliku.
func TestTekstIHTMLDajuIsteOznake(t *testing.T) {
	for _, ime := range []string{"izmisljeni_zakon.html", "izmisljeni_plan.html", "izmisljena_izmjena.html", "izmisljeni_pravilnik.html"} {
		p := procitajPrimjer(t, ime)
		html := Podijeli(p.Odlomci)
		tekst := podijeliTekst(strings.Join(tekstovi(p.Odlomci), "\n"))
		if a, b := oznake(html.Dijelovi), oznake(tekst.Dijelovi); !reflect.DeepEqual(a, b) {
			t.Errorf("%s: stranica %v, tekst %v", ime, a, b)
		}
		if html.Proglasenje != tekst.Proglasenje {
			t.Errorf("%s: proglašenje %v i %v", ime, html.Proglasenje, tekst.Proglasenje)
		}
	}
	// Potpis bez „v. r.” u nekoliko redaka iza klase nije potpis.
	p := podijeliTekst("Članak 1.\nTekst.\nKlasa: 1\nRedak\nRedak\nRedak\nRedak\nRedak\nRedak\nRedak, v. r.")
	if z := dio(t, p, "zavrsetak"); len(z.Stavci) != 1 {
		t.Errorf("završetak %+v", z.Stavci)
	}
}
