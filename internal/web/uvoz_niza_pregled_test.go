package web

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/arhiva"
	"gocop/internal/models"
)

// Pregled niza ne otvara se bez prava; obrazac koji nije višedijelni,
// obrazac bez datoteke i datoteka iz koje se ne čita niz vraćaju poruku koja
// kaže što je krivo, a u stablu ne nastaje ništa.
func TestPregledNizaOdbija(t *testing.T) {
	o := novaOkolinaVrata(t)
	datoteka := func(ime, sadrzaj string) *http.Request {
		return viseDijelni(t, "/administracija/uvoz-niza/pregled", nil, datotekaObrasca{"datoteka", ime, sadrzaj})
	}
	for _, s := range []struct {
		opis   string
		u      *models.User
		p      *models.UserPermissions
		r      *http.Request
		status int
		poruka string
	}{
		{"bez ijedne letve", o.marko, o.markoOvl, datoteka("satni.csv", dnevniPrimjerova), http.StatusForbidden, "unosi administrator"},
		{"nije višedijelni obrazac", o.pero, o.peroOvl, obicniObrazac("/", "x=1"), http.StatusOK,
			"datoteka se nije dala pročitati: request Content-Type isn't multipart/form-data"},
		{"bez datoteke", o.pero, o.peroOvl, viseDijelni(t, "/", map[string]string{"x": "1"}), http.StatusOK, "Nije odabrana datoteka."},
		{"Excel koji to nije", o.pero, o.peroOvl, datoteka("krivo.xlsx", "nije zip"), http.StatusOK, "zip"},
		{"samo zaglavlje", o.pero, o.peroOvl, datoteka("satni.csv", "datum;vodostaj\n"), http.StatusOK, "datoteka nema ni zaglavlje ni jedan redak"},
		{"bez vremena i broja", o.pero, o.peroOvl, datoteka("satni.csv", "a;b\nc;d\ne;f\n"), http.StatusOK,
			"ni u jednom retku se ne čitaju vrijeme i vrijednost jedno uz drugo"},
		{"tuđa letva iz naziva", o.ana, o.anaOvl, datoteka("drugovo_probni_vodostaj_dnevni.csv", dnevniPrimjerova), http.StatusOK,
			"letva drugovo nije na vašim dionicama — njezine podatke uvozi administrator njezina područja"},
	} {
		t.Run(s.opis, func(t *testing.T) {
			w := odgovor(o.h.PregledUvoza, s.r, s.u, s.p)
			html := htmlUTekst(w.Body.String())
			if w.Code != s.status || !strings.Contains(html, s.poruka) {
				t.Errorf("status %d, očekivan %d s porukom %q: %s", w.Code, s.status, s.poruka, porukeStranice(html))
			}
			if reIdObrasca.MatchString(html) {
				t.Error("odbijen pregled dao je broj za potvrdu")
			}
		})
	}
	if d := o.datoteke(t); len(d) != 0 {
		t.Errorf("odbijen pregled zapisao je %v", d)
	}
}

// Pregled pokazuje raspon i kad vrijednosti padaju, redak s dvije prazne
// ćelije nije ni podatak ni greška, a osvježeni pregled s krivom zonom ili
// stupcem bez ijednog broja kaže što ne valja.
func TestPregledNizaRasponIPrazniRedci(t *testing.T) {
	o := novaOkolinaVrata(t)
	sadrzaj := "datum;vodostaj\n2026-03-01;131\n;\n2026-03-02;125\n2026-03-03;120\n"
	id := o.posalji(t, o.pero, o.peroOvl, "primjerovo_probni_vodostaj_dnevni.csv", sadrzaj)
	ponovi := func(polja url.Values) string {
		polja.Set("id", id)
		w := odgovor(o.h.PonoviPregled, obicniObrazac("/administracija/uvoz-niza/pregled-ponovo", polja.Encode()), o.pero, o.peroOvl)
		return htmlUTekst(w.Body.String())
	}
	html := ponovi(url.Values{})
	if !strings.Contains(html, "120,0 – 131,0") || !strings.Contains(html, "<dt>Redaka</dt><dd>3</dd>") || porukaGreske(html) != "" {
		t.Errorf("raspon padajućeg niza: %s", porukeStranice(html))
	}
	if g := porukaGreske(ponovi(url.Values{"zona": {"Nema/Zona"}})); !strings.HasPrefix(g, `zona "Nema/Zona": `) {
		t.Errorf("kriva zona: %q", g)
	}
	if g := porukaGreske(ponovi(url.Values{"stupac_vrijednost": {"0"}})); g != "nijedan redak se nije dao pročitati kao vrijeme i vrijednost" {
		t.Errorf("stupac bez brojeva: %q", g)
	}
	if d := o.datoteke(t); len(d) != 0 {
		t.Errorf("pregled je zapisao %v", d)
	}
}

// Uzorak koji se pokazuje ima najviše osam redaka, a broj redaka je cijelo
// tijelo datoteke.
func TestPogodakUzorakOsamRedaka(t *testing.T) {
	var b strings.Builder
	b.WriteString("datum;vodostaj\n")
	for d := 1; d <= 12; d++ {
		fmt.Fprintf(&b, "2026-03-%02d;%d\n", d, 100+d)
	}
	p, tijelo, err := pogodi("primjerovo.csv", []byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if p.Redaka != 12 || len(tijelo) != 12 || len(p.Uzorak) != uzorakRedaka || p.Uzorak[7][1] != "108" ||
		strings.Join(p.Zaglavlje, ",") != "datum,vodostaj" {
		t.Errorf("pogodak: redaka %d, uzorak %d, zaglavlje %v", p.Redaka, len(p.Uzorak), p.Zaglavlje)
	}
}

// Razdjelnik se pogađa samo iz početka datoteke: iz prvih 64 kB i prvih
// pedeset redaka s podacima. Ono što dolazi poslije ne mijenja izbor.
func TestRazdjelnikIzPocetkaDatoteke(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 60; i++ {
		if i < 50 {
			b.WriteString("2026-03-01;120\n")
		} else {
			b.WriteString("2026-03-01\t120\t1\t2\t3\n")
		}
	}
	if r := pogodiRazdjelnik([]byte(b.String())); r != ";" {
		t.Errorf("iza pedesetog retka: %q", r)
	}
	dugo := "#" + strings.Repeat("x", 70000) + "\n" + strings.Repeat("2026-03-01\t120\t1\n", 10)
	if r := pogodiRazdjelnik([]byte(dugo)); r != ";" {
		t.Errorf("iza 64 kB: %q", r)
	}
	if r := pogodiRazdjelnik([]byte(strings.Repeat("2026-03-01\t120\t1\n", 10))); r != "\t" {
		t.Errorf("tabulator na početku: %q", r)
	}
}

// Zatečeni niz broji se i kad ni stara ni nova datoteka nisu po redu; bez
// stabla ili bez novih redaka nema se što uspoređivati.
func TestZatecenoBezRedaIBezStabla(t *testing.T) {
	koren := t.TempDir()
	mapa := filepath.Join(koren, "primjerica", "primjerovo")
	if err := os.MkdirAll(mapa, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mapa, "primjerovo_probni_vodostaj_dnevni_2026.csv"),
		[]byte("datum;vodostaj_cm\n2026-03-03;131\n2026-03-01;119\n2026-02-27;110\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	u := UvozNiza{Sliv: "primjerica", Letva: "primjerovo", Izvor: "probni", Velicina: "vodostaj", Vrsta: "dnevni"}
	dan := func(d int) time.Time { return time.Date(2026, 3, d, 0, 0, 0, 0, time.UTC) }
	novi := []arhiva.Redak{{Vrijeme: dan(2), Vrijednost: 125, PoDanu: true}, {Vrijeme: dan(1), Vrijednost: 120, PoDanu: true}}
	z := zateceno(koren, u, novi)
	if z == nil || *z != (ZatecenNiz{Redaka: 3, Od: "27.02.2026.", Do: "03.03.2026.", IzvanNov: 2, OdIzvan: "27.02.2026.", DoIzvan: "03.03.2026."}) {
		t.Errorf("zatečeno: %+v", z)
	}
	if zateceno("", u, novi) != nil || zateceno(koren, u, nil) != nil {
		t.Error("bez stabla ili bez novih redaka nema zatečenog")
	}
}

// Zalijepljeni redak s datumom izvan kalendara ili s vrijednošću koja nije
// broj ostaje na popisu s razlogom, da se ne izgubi tiho.
func TestZalijepljeniRedakIzvanRasponaINijeBroj(t *testing.T) {
	redci, satni, err := citajZalijepljeno("07.13.2026. 00 h  -118\n07.09.2026. 24 h  -118\n07.09.2026. 01 h  ..\n07.09.2026. 02 h  -117")
	if err != nil {
		t.Fatal(err)
	}
	if !satni || len(redci) != 4 {
		t.Fatalf("redci: %+v", redci)
	}
	for i, zeli := range []string{"datum ili sat izvan raspona", "datum ili sat izvan raspona", "vrijednost nije broj: ..", ""} {
		if redci[i].Greska != zeli || redci[i].Redak != i+1 {
			t.Errorf("redak %d: %+v, očekivana greška %q", i+1, redci[i], zeli)
		}
	}
	if redci[3].Vrijedi != -117 {
		t.Errorf("ispravan redak: %+v", redci[3])
	}
}
