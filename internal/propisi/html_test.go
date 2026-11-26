package propisi

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"gocop/internal/models"
)

// procitajPrimjer čita izmišljenu stranicu akta iz testdata.
func procitajPrimjer(t *testing.T, ime string) *Procitano {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", ime))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	p, err := ProcitajHTML(f)
	if err != nil {
		t.Fatalf("%s: %v", ime, err)
	}
	return p
}

func tekstovi(o []Odlomak) []string {
	var out []string
	for _, x := range o {
		out = append(out, x.Tekst)
	}
	return out
}

// Odluke (25. 11. 2026.): tekst akta je samo ono u stupcu akta
// (div.article-column). Zaglavlje stranice, <style>, podnožje i skripte se
// ne čitaju, a ni prazni odlomci i završni BEZREDA.
func TestTijeloSamoIzStupcaAkta(t *testing.T) {
	p := procitajPrimjer(t, "izmisljeni_zakon.html")
	sve := strings.Join(tekstovi(p.Odlomci), "\n")
	for _, ne := range []string{"Zaglavlje stranice", "color", "Uvjeti korištenja", "skripta", "NN 12/2001 (14.3.2001.)"} {
		if strings.Contains(sve, ne) {
			t.Errorf("u tekstu akta je %q", ne)
		}
	}
	if p.Odlomci[0] != (Odlomak{Klasa: "tb-na18", Tekst: "Hrvatski sabor"}) {
		t.Errorf("prvi odlomak %+v", p.Odlomci[0])
	}
	for _, o := range p.Odlomci {
		if o.Tekst == "" || o.Klasa == "bezreda" {
			t.Errorf("prazan ili završni odlomak %+v", o)
		}
	}
	potpis := p.Odlomci[9]
	if potpis.Klasa != "t-9-8-potpis" || potpis.Tekst != "Predsjednik\nRepublike Hrvatske\nIvo Ivić, v. r." {
		t.Errorf("potpis %+v", potpis)
	}
	if !strings.Contains(sve, "u m³ po letvi") || !strings.Contains(sve, "mjesto udionici") || !strings.Contains(sve, "i češće") {
		t.Error("eksponent, meka crtica ili tvrdi razmak nisu očišćeni")
	}
}

// Odluke (25. 11. 2026.): Državni plan ima dodatne omotače (div.story),
// pa se odlomci skupljaju redom kroz cijeli stupac, a ne samo iz prve
// razine.
func TestOdlomciRedomUOmotacima(t *testing.T) {
	p := procitajPrimjer(t, "izmisljeni_plan.html")
	got := tekstovi(p.Odlomci)
	zeli := []string{"UVODNE ODREDBE", "I.", "II.", "STADIJI OBRANE OD POPLAVA", "III.", "Ovaj Plan stupa na snagu danom objave u »Narodnim novinama«."}
	j := 0
	for _, s := range got {
		if j < len(zeli) && s == zeli[j] {
			j++
		}
	}
	if j != len(zeli) {
		t.Errorf("odlomci nisu redom: %q", got)
	}
}

// Odluke (25. 11. 2026.): naziv je iz naslova stranice, a vrsta, izdanje,
// broj akta, donositelj, datum i ELI iz tablice uz akt. Datum tiskanog
// izdanja „7.7.2010.” je „2010-07-07”; datum iz ELI meta oznaka ima
// prednost.
func TestMetapodaciIzDetailsTable(t *testing.T) {
	p := procitajPrimjer(t, "izmisljeni_zakon.html")
	if p.Naziv != "Zakon o čuvanju nasipa" || p.Vrsta != "Zakon" || p.Donositelj != "Hrvatski sabor" ||
		p.NN != (models.IzdanjeNN{Godina: 2001, Broj: 12, Akt: 345}) || p.DatumObjave != "2001-03-14" ||
		p.ELI != "/eli/sluzbeni/2001/12/345" {
		t.Errorf("metapodaci %+v", p)
	}
	plan := procitajPrimjer(t, "izmisljeni_plan.html")
	if plan.DatumObjave != "2010-07-07" || plan.Donositelj != "Vlada Republike Hrvatske" || plan.Vrsta != "Ostalo" {
		t.Errorf("plan: %q %q %q", plan.DatumObjave, plan.Donositelj, plan.Vrsta)
	}
	if datumIzTiska("7. 7. 2010") != "2010-07-07" || datumIzTiska("srpanj 2010.") != "" {
		t.Error("datum tiskanog izdanja")
	}
}

// Odluke (25. 11. 2026.): veze s drugim aktima čitaju se iz ELI meta
// oznaka djela, bez ponavljanja i bez veza koje program ne koristi
// (is_about, basis_for). JSON-LD se ne čita, jer su u njemu adrese
// pokvarene.
func TestVezeIzELIMeta(t *testing.T) {
	p := procitajPrimjer(t, "izmisljeni_zakon.html")
	zeli := []models.VezaELI{
		{Vrsta: "amended_by", ELI: "/eli/sluzbeni/2002/5/67"},
		{Vrsta: "changed_by", ELI: "/eli/sluzbeni/2002/5/67"},
		{Vrsta: "repeals", ELI: "/eli/sluzbeni/1999/1/2"},
	}
	if !reflect.DeepEqual(p.Veze, zeli) {
		t.Errorf("veze %+v", p.Veze)
	}
	iz := procitajPrimjer(t, "izmisljena_izmjena.html")
	if len(iz.Veze) != 2 || iz.Veze[1] != (models.VezaELI{Vrsta: "changes", ELI: "/eli/sluzbeni/2001/12/345"}) {
		t.Errorf("veze izmjene %+v", iz.Veze)
	}
}

// Odluke (25. 11. 2026.): stariji akt (Državni plan) nema ELI meta oznake.
// Tada ELI ostaje prazan, jer Narodne novine o njemu nemaju podatke o
// izmjenama, iako tablica uz akt navodi ELI adresu; izdanje je iz tablice.
func TestAktBezELIMeta(t *testing.T) {
	p := procitajPrimjer(t, "izmisljeni_plan.html")
	if p.ELI != "" || p.Veze != nil || p.NN != (models.IzdanjeNN{Godina: 2010, Broj: 30, Akt: 777}) {
		t.Errorf("ELI %q, veze %v, izdanje %+v", p.ELI, p.Veze, p.NN)
	}
	// Bez retka „Izdanje” izdanje se uzima iz ELI adrese u tablici.
	p, err := ProcitajHTML(strings.NewReader(`<div class="article-column"><p>x</p></div>` +
		`<table class="detailsTable"><tr><td><span class="key">ELI:</span> https://narodne-novine.nn.hr/eli/sluzbeni/2005/3/44</td></tr>` +
		`<tr><td><span class="key">Datum tiskanog izdanja:</span> nepoznat</td></tr></table>`))
	if err != nil || p.NN != (models.IzdanjeNN{Godina: 2005, Broj: 3, Akt: 44}) || p.DatumObjave != "" {
		t.Errorf("izdanje iz ELI-ja %+v, %v", p, err)
	}
}

// Odluke (25. 11. 2026.): tablica se čuva kao ćelije s tekstom, rasponom
// i podebljanjem, a ne kao HTML; „&lt;” je znak „<”.
func TestTablicaSRasponom(t *testing.T) {
	p := procitajPrimjer(t, "izmisljeni_pravilnik.html")
	var tablica *models.TablicaPropisa
	for _, o := range p.Odlomci {
		if o.Tablica != nil {
			tablica = o.Tablica
			break
		}
	}
	if tablica == nil || len(tablica.Retci) != 3 {
		t.Fatalf("tablica %+v", tablica)
	}
	if got := tablica.Retci[0][0]; got != (models.CelijaTablice{Tekst: "Razina", Redaka: 2, Podebljano: true}) {
		t.Errorf("prva ćelija %+v", got)
	}
	if got := tablica.Retci[0][1]; got.Stupaca != 2 || got.Redaka != 0 {
		t.Errorf("druga ćelija %+v", got)
	}
	if tablica.Retci[2][0].Tekst != "D < 2,5 m" || tablica.Retci[2][2].Tekst != "čizme i rukavice" || tablica.Retci[2][1].Podebljano {
		t.Errorf("treći redak %+v", tablica.Retci[2])
	}
}

// Odluke (25. 11. 2026.): pravilnik ima tablicu upisanu unutar odlomka
// (neispravan HTML). Tablica postaje zaseban odlomak između teksta prije i
// poslije nje; prazna tablica se preskače.
func TestTablicaUnutarOdlomka(t *testing.T) {
	p := procitajPrimjer(t, "izmisljeni_pravilnik.html")
	var oko []string
	tablica := 0
	for i, o := range p.Odlomci {
		if o.Tablica == nil {
			continue
		}
		tablica++
		if tablica == 1 {
			oko = []string{p.Odlomci[i-1].Tekst, o.Klasa, p.Odlomci[i+1].Tekst}
		}
	}
	if tablica != 2 || !reflect.DeepEqual(oko, []string{"Oprema po razini vode:", "c-all", "Opremu pregledava rukovoditelj."}) {
		t.Errorf("%d tablica, oko prve %q", tablica, oko)
	}
	// I kad parser tablicu ostavi u odlomku, tekst oko nje ostaje na mjestu.
	var s sakupljac
	s.odlomak(odlomakSTablicom(t))
	if len(s.odlomci) != 3 || s.odlomci[0].Tekst != "prije" || s.odlomci[2].Tekst != "poslije" || !s.odlomci[1].Tablica.Retci[0][0].Podebljano {
		t.Errorf("odlomci %+v", s.odlomci)
	}
}

// Odluke (25. 11. 2026.): stranica bez stupca akta (obavijest „nema
// stranice”) nije akt; greška čitanja se vraća kakva jest.
func TestBezTijelaJeGreska(t *testing.T) {
	if _, err := ProcitajHTML(strings.NewReader(`<html><body><p class="T-9-8">Stranica ne postoji.</p></body></html>`)); !errors.Is(err, ErrNemaTijelaAkta) {
		t.Errorf("greška %v, želi ErrNemaTijelaAkta", err)
	}
	pokvaren := errors.New("veza je pukla")
	if _, err := ProcitajHTML(citacSGreskom{pokvaren}); !errors.Is(err, pokvaren) {
		t.Errorf("greška %v, želi grešku čitanja", err)
	}
}

// Odluke (25. 11. 2026.): tekst iz NN je tekst, nikad HTML. Ono što na
// stranici piše kao „&lt;script&gt;” ostaje doslovan tekst „<script>”, a
// prikaz ga ispisuje kao tekst.
func TestTekstAktaOstajeTekst(t *testing.T) {
	p := procitajPrimjer(t, "izmisljeni_zakon.html")
	sve := strings.Join(tekstovi(p.Odlomci), "\n")
	if !strings.Contains(sve, "<script>alert(1)</script>") || !strings.Contains(sve, "<img src=x onerror=alert(1)>") {
		t.Errorf("tekst s oznakama nije doslovan")
	}
}

// odlomakSTablicom slaže odlomak s tablicom unutra, kakav parser daje
// stranici bez <!DOCTYPE html>.
func odlomakSTablicom(t *testing.T) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(`<table><tr><th>A</th></tr></table>`))
	if err != nil {
		t.Fatal(err)
	}
	tablica := nadjiCvor(doc, func(n *html.Node) bool { return n.DataAtom == atom.Table })
	tablica.Parent.RemoveChild(tablica)
	p := &html.Node{Type: html.ElementNode, Data: "p", DataAtom: atom.P, Attr: []html.Attribute{{Key: "class", Val: "T-9-8"}}}
	p.AppendChild(&html.Node{Type: html.TextNode, Data: "prije"})
	p.AppendChild(tablica)
	p.AppendChild(&html.Node{Type: html.TextNode, Data: "poslije"})
	return p
}

type citacSGreskom struct{ err error }

func (c citacSGreskom) Read([]byte) (int, error) { return 0, c.err }
