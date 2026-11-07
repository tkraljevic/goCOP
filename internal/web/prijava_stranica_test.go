package web

import (
	"html/template"
	"io/fs"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/potpis"
	webassets "gocop/web"
)

// stranicaPrijave slaže stranicu prijave iz podataka, bez baze: stanja koja se
// kroz rute teško postignu (nevaljan potpis, prijava bez izvornika) tako se
// provjeravaju jednako kao i obična.
func stranicaPrijave(t *testing.T, d PrijavePageData) string {
	t.Helper()
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("prijava.html")...)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := tp.ExecuteTemplate(&b, "prijava.html", d); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func prijavaZaStranicu(vrsta, status string) *models.PrijavaSTerena {
	kad := time.Date(2026, 10, 26, 9, 4, 0, 0, models.Zagreb)
	p := &models.PrijavaSTerena{ID: "p1", UserID: "u1", Ime: "Ivan Primjer", Sektor: "B", AreaID: 16, Godina: 2026, Vrsta: vrsta,
		Naslov: "Oštećena rampa", Opis: "Rampa je ulegnuta.", Datum: kad, Status: status, Vodotok: "kanal Travnik", Objekt: "nasip Draž", DionicaCode: "B.16.2", Stacionaza: "4+300"}
	if status != models.PrijavaNacrt {
		p.Broj, p.ObjavljenoAt, p.ListID, p.ListBroj = 132, &kad, "l1", 41
	}
	if status == models.PrijavaRijesena {
		p.Rijesio = "Stjepan Primjer"
	}
	return p
}

// Koje radnje tko vidi, po stanju prijave. Forme i uvjeti su isti kao prije
// novog izgleda: mijenja se samo mjesto ploče, a na mobitelu je urudžba prva samo
// onome tko je rješava.
func TestRadnjePrijavePoStanju(t *testing.T) {
	const (
		objavi    = `name="radnja" value="objavi"`
		obrisi    = `name="radnja" value="obrisi"`
		ukloni    = `name="radnja" value="obrisi-sliku"`
		urudzba   = `name="radnja" value="urudzbiraj"`
		rijesi    = `value="rijesi"`
		lozinka   = `name="lozinka"`
		prva      = `prijava-radnja-prva`
		uredi     = `/prijave/p1/uredi`
		pregled   = `PDF (pregled nacrta)`
		objasnjen = `potvrđuje da je pregledana i riješena`
		kuca      = `ostaje u kući, bez urudžbe`
	)
	sve := []string{objavi, obrisi, ukloni, urudzba, rijesi, lozinka, prva, uredi, pregled, objasnjen, kuca}
	for _, c := range []struct {
		ime string
		d   PrijavePageData
		ima []string
	}{
		{"nacrt, autor s ključem", PrijavePageData{Prijava: prijavaZaStranicu("PRIJAVA", models.PrijavaNacrt), Moja: true, ImaKljuc: true},
			[]string{objavi, obrisi, ukloni, lozinka, uredi, pregled}},
		{"nacrt, autor bez ključa", PrijavePageData{Prijava: prijavaZaStranicu("PRIJAVA", models.PrijavaNacrt), Moja: true},
			[]string{objavi, obrisi, ukloni, uredi, pregled}},
		{"nacrt, simulirani ključ", PrijavePageData{Prijava: prijavaZaStranicu("PRIJAVA", models.PrijavaNacrt), Moja: true, ImaKljuc: true, Simulacija: true},
			[]string{objavi, obrisi, ukloni, uredi, pregled}},
		{"objavljena, rukovoditelj", PrijavePageData{Prijava: prijavaZaStranicu("PRIJAVA", models.PrijavaObjavljena), SmijeRijesiti: true},
			[]string{urudzba, rijesi, prva, objasnjen}},
		{"objavljena, autor", PrijavePageData{Prijava: prijavaZaStranicu("PRIJAVA", models.PrijavaObjavljena), Moja: true},
			[]string{urudzba}},
		{"objavljena, tko samo čita", PrijavePageData{Prijava: prijavaZaStranicu("PRIJAVA", models.PrijavaObjavljena)},
			nil},
		{"obavijest, rukovoditelj", PrijavePageData{Prijava: prijavaZaStranicu("OBAVIJEST", models.PrijavaObjavljena), SmijeRijesiti: true},
			[]string{rijesi, prva, kuca}},
		{"obavijest, autor", PrijavePageData{Prijava: prijavaZaStranicu("OBAVIJEST", models.PrijavaObjavljena), Moja: true},
			nil},
		{"riješena, rukovoditelj", PrijavePageData{Prijava: prijavaZaStranicu("PRIJAVA", models.PrijavaRijesena)},
			nil},
		{"riješena, autor", PrijavePageData{Prijava: prijavaZaStranicu("PRIJAVA", models.PrijavaRijesena), Moja: true},
			[]string{urudzba}},
	} {
		t.Run(c.ime, func(t *testing.T) {
			c.d.Prijava.Slike = []models.SlikaPrijave{{ID: "s1", Naziv: "IMG_2041.jpg", Sirina: 1000, Visina: 750}}
			s := stranicaPrijave(t, c.d)
			for _, x := range sve {
				treba := false
				for _, y := range c.ima {
					treba = treba || y == x
				}
				if strings.Contains(s, x) != treba {
					t.Errorf("%q na stranici: %v, a treba %v", x, !treba, treba)
				}
			}
			if strings.Contains(s, "app.bp16.xyz") || strings.Contains(s, "0001") {
				t.Error("adresa ranije evidencije ili nulta godina na stranici")
			}
		})
	}
}

// Zaglavlje: naslov nacrta bez riječi „nacrt”, a poruke ispisuje samo osnova stranice.
func TestZaglavljePrijave(t *testing.T) {
	nacrt := prijavaZaStranicu("PRIJAVA", models.PrijavaNacrt)
	s := stranicaPrijave(t, PrijavePageData{Prijava: nacrt, Moja: true, SuccessMessage: "Nacrt je spremljen.", ErrorMessage: "Fotografija nije dodana."})
	for _, x := range []string{`← Moje prijave`, `data-naziv-stranice="Prijava s terena">Prijava s terena <span class="oznaka-stanja nacrt">nacrt</span>`, "kanal Travnik · nasip Draž · B.16.2 · st. 4&#43;300"} {
		if !strings.Contains(s, x) {
			t.Errorf("nacrt: nema %q", x)
		}
	}
	if n := strings.Count(s, "Nacrt je spremljen."); n != 1 {
		t.Errorf("poruka o uspjehu ispisana %d puta", n)
	}
	if n := strings.Count(s, "Fotografija nije dodana."); n != 1 {
		t.Errorf("poruka o grešci ispisana %d puta", n)
	}
	s = stranicaPrijave(t, PrijavePageData{Prijava: prijavaZaStranicu("OBAVIJEST", models.PrijavaRijesena)})
	for _, x := range []string{`← Prijave s terena`, `Obavijest s terena B-T-132/2026`, `riješio Stjepan Primjer`, `<h2>Obavijest</h2>`} {
		if !strings.Contains(s, x) {
			t.Errorf("riješena obavijest: nema %q", x)
		}
	}
}

// Tri stanja objave moraju reći istinu o tome što će biti s PDF-om.
func TestObjavaKazeStoBivaSPotpisom(t *testing.T) {
	for _, c := range []struct {
		ime string
		d   PrijavePageData
		ima []string
	}{
		{"s ključem", PrijavePageData{ImaKljuc: true}, []string{"potpisan vašim ključem", `autocomplete="current-password"`, `required`}},
		{"simuliran", PrijavePageData{ImaKljuc: true, Simulacija: true}, []string{"potpisan simuliranim ključem", "simulirani ključ, bez lozinke"}},
		{"bez ključa", PrijavePageData{}, []string{"postaje izvornik bez elektroničkog potpisa"}},
	} {
		c.d.Prijava, c.d.Moja = prijavaZaStranicu("PRIJAVA", models.PrijavaNacrt), true
		s := stranicaPrijave(t, c.d)
		for _, x := range c.ima {
			if !strings.Contains(s, x) {
				t.Errorf("%s: nema %q", c.ime, x)
			}
		}
		if !strings.Contains(s, "Objavi i potpiši") || !strings.Contains(s, "za 26. 10. 2026.") {
			t.Errorf("%s: objava bez gumba ili dana", c.ime)
		}
		if strings.Contains(s, "Lozinka za potpis") != (c.d.ImaKljuc && !c.d.Simulacija) {
			t.Errorf("%s: polje lozinke", c.ime)
		}
	}
}

// Izvornik i vrijeme objave: svako stanje potpisa ostaje na stranici. Riječ
// „potpisani” i izostanak retka „Objavljeno” vrijede samo uz valjan pravi potpis.
func TestIzvornikPrijavePoStanjuPotpisa(t *testing.T) {
	kad := time.Date(2026, 10, 26, 9, 4, 0, 0, models.Zagreb)
	osoba := func(simulacija bool) potpis.Osoba { return potpis.Osoba{Ime: "Ivan Primjer", Simulacija: simulacija} }
	const objavljeno = "<dt>Objavljeno</dt><dd>26. 10. 2026. 09:04</dd>"
	for _, c := range []struct {
		ime      string
		iz       potpisiIzvornika
		ima      []string
		potpisan bool
	}{
		{"valjan, cijeli dokument", potpisiIzvornika{Ima: true, Potpisi: []potpis.Potpis{{Osoba: osoba(false), Kad: kad, Valjan: true, Cijeli: true}}},
			[]string{">potpisani PDF</a>", "elektronički potpisao Ivan Primjer, 26. 10. 2026. 09:04 (potpis valjan, pokriva cijeli dokument)"}, true},
		{"valjan, nakon urudžbe ne pokriva cijeli", potpisiIzvornika{Ima: true, Potpisi: []potpis.Potpis{{Osoba: osoba(false), Kad: kad, Valjan: true}}},
			[]string{">potpisani PDF</a>", "(potpis valjan)"}, true},
		{"valjan, bez vremena", potpisiIzvornika{Ima: true, Potpisi: []potpis.Potpis{{Osoba: osoba(false), Valjan: true}}},
			[]string{"elektronički potpisao Ivan Primjer (potpis valjan)"}, true},
		{"simuliran", potpisiIzvornika{Ima: true, Potpisi: []potpis.Potpis{{Osoba: osoba(true), Kad: kad, Valjan: true}}},
			[]string{">PDF</a>", "<strong>SIMULIRANI potpis, bezvrijedan</strong> (Ivan Primjer)", objavljeno}, false},
		{"nevaljan", potpisiIzvornika{Ima: true, Potpisi: []potpis.Potpis{{Osoba: osoba(false), Kad: kad, Greska: "dokument je mijenjan"}}},
			[]string{">PDF</a>", "potpis Ivan Primjer: <strong>dokument je mijenjan</strong>", objavljeno}, false},
		{"izvornik bez potpisa", potpisiIzvornika{Ima: true},
			[]string{">PDF</a>", "bez elektroničkog potpisa", objavljeno}, false},
		{"izvornika nema na ovom računalu", potpisiIzvornika{}, []string{objavljeno}, false},
	} {
		t.Run(c.ime, func(t *testing.T) {
			s := stranicaPrijave(t, PrijavePageData{Prijava: prijavaZaStranicu("PRIJAVA", models.PrijavaObjavljena), Izvornik: c.iz})
			for _, x := range c.ima {
				if !strings.Contains(s, x) {
					t.Errorf("nema %q", x)
				}
			}
			if strings.Contains(s, "<dt>Izvornik</dt>") != c.iz.Ima {
				t.Error("redak Izvornik")
			}
			if strings.Contains(s, "<dt>Objavljeno</dt>") == c.potpisan {
				t.Error("redak Objavljeno")
			}
			if strings.Contains(s, "potpisani PDF") != c.potpisan {
				t.Error("riječ „potpisani” bez valjanog potpisa, ili valjan potpis bez nje")
			}
			if strings.Contains(s, "0001") {
				t.Error("nulta godina na stranici")
			}
		})
	}
}

// Dokaz ispod fotografije: zapis fotoaparata ostaje cijeli, a skraćen otisak
// je samo na zaslonu, cijeli stoji u title.
func TestFotografijaPrijaveDrziDokaz(t *testing.T) {
	cijeli := "7f3a" + strings.Repeat("0", 56) + "c91e"
	snimljeno := time.Date(2026, 10, 26, 8, 52, 17, 0, models.Zagreb)
	p := prijavaZaStranicu("PRIJAVA", models.PrijavaObjavljena)
	p.Slike = []models.SlikaPrijave{
		{ID: "s1", Naziv: "IMG_2041.jpg", Sirina: 1000, Visina: 750, Snimljeno: &snimljeno, Lat: 45.7312, Lon: 18.8741, Uredjaj: "mobitel Primjer X1", Otisak: cijeli, IzvornoBajtova: 3 << 20},
		{ID: "s2", Naziv: "IMG_2042.jpg", Sirina: 1000, Visina: 750, Otisak: cijeli},
		{ID: "s3", Naziv: "IMG_2043.jpg"},
	}
	s := stranicaPrijave(t, PrijavePageData{Prijava: p})
	for _, x := range []string{
		"IMG_2041.jpg · 1000×750",
		"26. 10. 2026. 08:52:17 · 45,73120; 18,87410 · mobitel Primjer X1 · <span class=\"prijava-otisak\" title=\"Izvorna datoteka IMG_2041.jpg, 3,0 MB, SHA-256 " + cijeli + "\">SHA-256 7f3a…c91e</span>",
		"Fotoaparat uz sliku nije zapisao vrijeme, položaj ni uređaj. <span class=\"prijava-otisak\" title=\"Izvorna datoteka IMG_2042.jpg, SHA-256 " + cijeli + "\">SHA-256 7f3a…c91e</span>",
		"Fotoaparat uz sliku nije zapisao vrijeme, položaj ni uređaj.</span></figcaption>",
		`loading="lazy"`,
		`classList.add(`,
		`class="form-help nema-slike"`,
		`href="/prijave/p1/prijava.pdf" target="_blank">PDF-u</a>`,
	} {
		if !strings.Contains(s, x) {
			t.Errorf("nema %q", x)
		}
	}
	if !strings.Contains(s, "<h2>Fotografije (3)</h2>") {
		t.Error("broj fotografija")
	}
	p.Slike = nil
	if s := stranicaPrijave(t, PrijavePageData{Prijava: p}); !strings.Contains(s, "Bez fotografija.") || !strings.Contains(s, "<h2>Fotografije (0)</h2>") {
		t.Error("prijava bez fotografija")
	}
}

// Redak „Dnevni list”: riječ „preneseni list” stoji samo uz list koji postoji.
// Prenesena prijava bez veze ima crticu, a list koji čvor nema ostaje poveznica.
func TestDnevniListNaPrijavi(t *testing.T) {
	dan := time.Date(2026, 8, 3, 0, 0, 0, 0, models.Zagreb)
	prenesena := func(listID string) *models.PrijavaSTerena {
		p := prijavaZaStranicu("PRIJAVA", models.PrijavaObjavljena)
		p.Rekonstrukcija, p.ListID, p.ListBroj = true, listID, 0
		return p
	}
	if s := stranicaPrijave(t, PrijavePageData{Prijava: prenesena("")}); !strings.Contains(s, "<dt>Dnevni list</dt><dd>—</dd>") || strings.Contains(s, "preneseni list") {
		t.Error("prenesena bez veze na list mora imati crticu")
	}
	s := stranicaPrijave(t, PrijavePageData{Prijava: prenesena("l9"), DnevniList: &models.VodocuvarskiList{ID: "l9", Datum: dan, Rekonstrukcija: true}})
	if !strings.Contains(s, `<a href="/vodocuvar/l9">preneseni list od 3. 8. 2026.</a>`) {
		t.Error("prenesena s vezom na list")
	}
	if s := stranicaPrijave(t, PrijavePageData{Prijava: prenesena("l9")}); !strings.Contains(s, `<a href="/vodocuvar/l9">nacrt bez broja</a>`) {
		t.Error("list koji čvor nema, bez broja")
	}
	if s := stranicaPrijave(t, PrijavePageData{Prijava: prijavaZaStranicu("PRIJAVA", models.PrijavaObjavljena)}); !strings.Contains(s, `<a href="/vodocuvar/l1">list 041/2026</a>`) {
		t.Error("list koji čvor nema, s brojem")
	}
}

// Prenesena prijava: traka bez adrese ranije evidencije, gumb Sken samo kad sken
// postoji, a urudžba bez dana ne ispisuje nultu godinu.
func TestPrenesenaPrijavaNaStranici(t *testing.T) {
	p := prijavaZaStranicu("PRIJAVA", models.PrijavaObjavljena)
	p.Rekonstrukcija, p.Klasa, p.Urbroj, p.PrimljenoAt = true, "325-02/26-01/0000096", "374-26-1", &time.Time{}
	s := stranicaPrijave(t, PrijavePageData{Prijava: p, SmijeRijesiti: true})
	for _, x := range []string{"alert-warning", `oznaka-stanja pozor`, "nema elektroničkog potpisa", "KLASA 325-02/26-01/0000096 · URBROJ 374-26-1<", `id="primljeno" name="primljeno" value=""`} {
		if !strings.Contains(s, x) {
			t.Errorf("nema %q", x)
		}
	}
	if strings.Contains(s, "sken.pdf") || strings.Contains(s, "app.bp16.xyz") || strings.Contains(s, "0001") {
		t.Error("sken bez skena, adresa ranije evidencije ili nulta godina")
	}
	p.Sken = "sken.pdf"
	dan := time.Date(2026, 8, 4, 0, 0, 0, 0, models.Zagreb)
	p.PrimljenoAt = &dan
	s = stranicaPrijave(t, PrijavePageData{Prijava: p, SmijeRijesiti: true})
	for _, x := range []string{`href="/prijave/p1/sken.pdf" target="_blank" class="btn btn-link">Sken potpisanog ispisa</a>`, "primljeno 4. 8. 2026.", `value="2026-08-04"`} {
		if !strings.Contains(s, x) {
			t.Errorf("nema %q", x)
		}
	}
}
