package web

import (
	"bytes"
	"errors"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gocop/internal/arhiva"
	"gocop/internal/models"
)

// sazetakKrivulja je predložak koji ispiše samo ono što rukovatelj preda
// stranici o krivuljama, da se vidi bez cijele stranice
func sazetakKrivulja(t *testing.T) *template.Template {
	t.Helper()
	return template.Must(template.New("uvoz_niza.html").Parse(
		`{{with .Krivulje}}id={{.Id}};letva={{.Letva}};ime={{.Ime}};krivulja={{.Krivulja}};odsjecaka={{.Odsjecaka}};` +
			`potencija={{.Potencija}};razdoblje={{.Od}}..{{.Do}};kota={{.KotaNule}};zateceno={{.ZatecenihKrivulja}}/{{.ZatecenihOdsjecaka}}{{end}}` +
			`|greska={{.ErrorMessage}}|posao={{.PosaoID}}`))
}

// Pregled krivulja čita datoteku i predaje stranici razdoblje, odsječke i kotu
// nule; ništa ne zapisuje, a datoteka čeka potvrdu. Potvrda zapisuje krivulje
// u stablo letve, čuva izvornik uz njih i gradi letvu; druga ista potvrda ne
// prolazi.
func TestKrivuljePregledPaUpis(t *testing.T) {
	o := novaOkolinaVrata(t)
	var izgradjene []string
	o.h.izgradi = func(letva string, _ io.Writer) (arhiva.Izvjestaj, error) {
		izgradjene = append(izgradjene, letva)
		return arhiva.Izvjestaj{}, nil
	}
	o.h.tmpl = sazetakKrivulja(t)
	r := viseDijelni(t, "/administracija/uvoz-krivulja/pregled", map[string]string{"letva": " primjerovo "},
		datotekaObrasca{"datoteka", "krivulje.csv", krivuljePrimjerova})
	w := odgovor(o.h.PregledUvozaKrivulja, r, o.pero, o.peroOvl)
	sazetak := htmlUTekst(w.Body.String())
	if !strings.HasPrefix(sazetak, "id=") || !strings.Contains(sazetak, ";letva=primjerovo;ime=krivulje.csv;krivulja=2;odsjecaka=3;"+
		"potencija=1;razdoblje=2001-01-01..2012-12-31;kota=100,00;zateceno=0/0|greska=|posao=") {
		t.Fatalf("pregled krivulja: %s", sazetak)
	}
	id := strings.TrimPrefix(strings.SplitN(sazetak, ";", 2)[0], "id=")
	if c, ima := uvozi.uzmi(id, o.pero.ID.String()); !ima || c.ime != "krivulje.csv" || string(c.sadrzaj) != krivuljePrimjerova {
		t.Errorf("datoteka ne čeka potvrdu: %v %q", ima, c.ime)
	}
	if d := o.datoteke(t); len(d) != 0 {
		t.Errorf("pregled je zapisao %v", d)
	}

	o.h.tmpl = predlozakVrata(t)
	upis := func() string {
		w := odgovor(o.h.UpisiKrivulje, obicniObrazac("/administracija/uvoz-krivulja/upisi", "letva=primjerovo&id="+id), o.pero, o.peroOvl)
		return htmlUTekst(w.Body.String())
	}
	p := posaoSaStranice(t, o.h.poslovi, upis(), o.pero)
	hq := filepath.Join(o.koren, "primjerica", "primjerovo", "hq")
	put := filepath.Join(hq, "primjerovo_hq_krivulje.csv")
	if p.Greska() != "" || p.Stanje().Sazetak != "Krivulje su zapisane u "+put+"; primjerovo je ponovno izgrađena." {
		t.Errorf("posao upisa krivulja: %q %q", p.Greska(), p.Stanje().Sazetak)
	}
	if len(izgradjene) != 1 || izgradjene[0] != "primjerovo" {
		t.Errorf("izgrađeno %v", izgradjene)
	}
	zapisano := procitaj(t, put)
	for _, redak := range []string{
		"vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena\n",
		"2001-01-01;2009-12-31;100;500;polinom;",
		"2001-01-01;2009-12-31;0;100;potencija;",
		"2010-01-01;;0;300;polinom;",
	} {
		if !strings.Contains(zapisano, redak) {
			t.Errorf("zapisane krivulje nemaju %q:\n%s", redak, zapisano)
		}
	}
	if got := procitaj(t, filepath.Join(hq, "izvornik", "krivulje.csv")); got != krivuljePrimjerova {
		t.Error("izvornik nije spremljen uz krivulje")
	}
	if g := porukaGreske(upis()); g != "Odabir je istekao ili više ne postoji. Pošaljite datoteku ponovno." {
		t.Errorf("druga ista potvrda: %q", g)
	}
}

// Stranica s pregledom krivulja mora se iscrtati do kraja, s gumbom za
// potvrdu.
func TestKrivuljePregledNaStranici(t *testing.T) {
	o := novaOkolinaVrata(t)
	r := viseDijelni(t, "/administracija/uvoz-krivulja/pregled", map[string]string{"letva": "primjerovo"},
		datotekaObrasca{"datoteka", "krivulje.csv", krivuljePrimjerova})
	html := odgovor(o.h.PregledUvozaKrivulja, r, o.pero, o.peroOvl).Body.String()
	if strings.Contains(html, "nil pointer") || !strings.Contains(html, `action="/administracija/uvoz-krivulja/upisi"`) {
		t.Errorf("pregled krivulja se ne iscrtava:\n%s", html[max(0, len(html)-400):])
	}
}

// Pregled krivulja ne otvara se bez prava i tuđu letvu ne gleda; obrazac koji
// nije višedijelni, obrazac bez datoteke, prevelika datoteka, datoteka koja
// nije izvoz HIS-a i datoteka krivulja bez krivulja ili izvan UTF-8 vraćaju
// poruku, a ništa ne čeka potvrdu.
func TestKrivuljePregledOdbija(t *testing.T) {
	o := novaOkolinaVrata(t)
	o.h.tmpl = sazetakKrivulja(t)
	krivulje := datotekaObrasca{"datoteka", "krivulje.csv", krivuljePrimjerova}
	for _, s := range []struct {
		opis   string
		u      *models.User
		p      *models.UserPermissions
		r      *http.Request
		status int
		poruka string
	}{
		{"bez ijedne letve", o.marko, o.markoOvl, viseDijelni(t, "/", map[string]string{"letva": "primjerovo"}, krivulje), http.StatusForbidden, "unosi administrator"},
		{"nije višedijelni obrazac", o.pero, o.peroOvl, obicniObrazac("/", "letva=primjerovo"), http.StatusOK, "greska=request Content-Type isn't multipart/form-data"},
		{"tuđa letva", o.ana, o.anaOvl, viseDijelni(t, "/", map[string]string{"letva": "drugovo"}, krivulje), http.StatusOK,
			"greska=Letva drugovo nije na vašim dionicama — njezine podatke uvozi administrator njezina područja."},
		{"bez datoteke", o.pero, o.peroOvl, viseDijelni(t, "/", map[string]string{"letva": "primjerovo"}), http.StatusOK, "greska=datoteka nije poslana: "},
		{"prevelika datoteka", o.pero, o.peroOvl, viseDijelni(t, "/", map[string]string{"letva": "primjerovo"},
			datotekaObrasca{"datoteka", "krivulje.csv", strings.Repeat("x", 8<<20+1)}), http.StatusOK, "greska=datoteka je veća od 8 MB"},
		{"nije izvoz HIS-a", o.pero, o.peroOvl, viseDijelni(t, "/", map[string]string{"letva": "primjerovo"},
			datotekaObrasca{"datoteka", "krivulje.csv", "datum;vodostaj\n"}), http.StatusOK, "greska=datoteka se ne čita kao HIS-2000 izvoz"},
		{"zaglavlje krivulja bez stupca p1", o.pero, o.peroOvl, viseDijelni(t, "/", map[string]string{"letva": "primjerovo"},
			datotekaObrasca{"datoteka", "krivulje.csv", "vrijedi_od;vrijedi_do;od_cm;do_cm;p2;p3\n2001-01-01;;0;100;2;3\n"}),
			http.StatusOK, "greska=datoteka se ne čita kao HIS-2000 izvoz"},
		{"datoteka krivulja samo sa zaglavljem", o.pero, o.peroOvl, viseDijelni(t, "/", map[string]string{"letva": "primjerovo"},
			datotekaObrasca{"datoteka", "susjed.csv", zaglavljeKrivuljaTesta}),
			http.StatusOK, "greska=datoteka krivulja nije primljena: ispod zaglavlja nema nijedne krivulje"},
		{"datoteka krivulja nije UTF-8", o.pero, o.peroOvl, viseDijelni(t, "/", map[string]string{"letva": "primjerovo"},
			datotekaObrasca{"datoteka", "susjed.csv", zaglavljeKrivuljaTesta + "2001-01-01;;0;100;polinom;1;2;3;;Ma\xf0arska;\n"}),
			http.StatusOK, "greska=datoteka krivulja nije primljena: nije spremljena kao UTF-8"},
	} {
		t.Run(s.opis, func(t *testing.T) {
			w := odgovor(o.h.PregledUvozaKrivulja, s.r, s.u, s.p)
			tekst := htmlUTekst(w.Body.String())
			if w.Code != s.status || !strings.Contains(tekst, s.poruka) {
				t.Errorf("status %d, očekivan %d s porukom %q: %s", w.Code, s.status, s.poruka, tekst)
			}
			if strings.HasPrefix(tekst, "id=") {
				t.Error("odbijen pregled predao je krivulje stranici")
			}
		})
	}
}

// Potvrda krivulja traži pravo i živ odabir; datoteka koja se više ne čita i
// letva koje nema u stablu ne zapisuju ništa, a kad se krivulje ili izvornik
// ne daju zapisati, poruka to kaže i gradnja ne kreće. Kad se zatečena
// datoteka ne da pročitati, put i razlog idu u dnevnik čvora, a ne na
// stranicu.
func TestKrivuljeUpisOdbija(t *testing.T) {
	o := novaOkolinaVrata(t)
	korisnik := o.pero.ID.String()
	// stalni brojevi: dva poziva novIdUvoza u istoj mikrosekundi daju isti
	ceka := func(ime, sadrzaj string) string {
		id := "krivulje-" + ime + "-" + t.Name()
		uvozi.spremi(id, "krivulje.csv", []byte(sadrzaj), korisnik)
		t.Cleanup(func() { uvozi.makni(id) })
		return id
	}
	pokvarena, dobra := ceka("pokvarena", "datum;vodostaj\n"), ceka("dobra", krivuljePrimjerova)
	for _, s := range []struct {
		opis   string
		u      *models.User
		p      *models.UserPermissions
		tijelo string
		status int
		poruka string
	}{
		{"bez ijedne letve", o.marko, o.markoOvl, "letva=primjerovo&id=" + dobra, http.StatusForbidden, "unosi administrator"},
		{"neispravan obrazac", o.pero, o.peroOvl, "id=%zz", http.StatusOK, "invalid URL escape"},
		{"tuđa letva", o.ana, o.anaOvl, "letva=drugovo&id=" + dobra, http.StatusOK, "Letva drugovo nije na vašim dionicama."},
		{"istekao odabir", o.pero, o.peroOvl, "letva=primjerovo&id=nema-ga", http.StatusOK, "Odabir je istekao ili više ne postoji. Pošaljite datoteku ponovno."},
		{"datoteka se više ne čita", o.pero, o.peroOvl, "letva=primjerovo&id=" + pokvarena, http.StatusOK, "datoteka se ne čita kao HIS-2000 izvoz"},
		{"letve nema u stablu", o.pero, o.peroOvl, "letva=nepoznata&id=" + dobra, http.StatusOK, "letva nepoznata nije u stablu"},
	} {
		t.Run(s.opis, func(t *testing.T) {
			w := odgovor(o.h.UpisiKrivulje, obicniObrazac("/administracija/uvoz-krivulja/upisi", s.tijelo), s.u, s.p)
			if w.Code != s.status || !strings.Contains(htmlUTekst(w.Body.String()), s.poruka) {
				t.Errorf("status %d, očekivan %d s porukom %q: %s", w.Code, s.status, s.poruka, porukeStranice(w.Body.String()))
			}
			if rePosaoNaStranici.MatchString(w.Body.String()) {
				t.Error("odbijena potvrda pokrenula je gradnju")
			}
		})
	}
	if _, ima := uvozi.uzmi(dobra, korisnik); !ima {
		t.Fatal("odbijena potvrda potrošila je odabir")
	}
	if d := o.datoteke(t); len(d) != 0 {
		t.Errorf("odbijena potvrda zapisala je %v", d)
	}

	hq := filepath.Join(o.koren, "primjerica", "primjerovo", "hq")
	put := filepath.Join(hq, "primjerovo_hq_krivulje.csv")
	posalji := func() string {
		w := odgovor(o.h.UpisiKrivulje, obicniObrazac("/", "letva=primjerovo&id="+dobra), o.pero, o.peroOvl)
		if rePosaoNaStranici.MatchString(w.Body.String()) {
			t.Error("neuspio upis pokrenuo je gradnju")
		}
		return porukaGreske(htmlUTekst(w.Body.String()))
	}
	// Na mjestu mape hq stoji datoteka: zatečene krivulje ne daju se
	// pročitati, pa se nema s čim spojiti.
	if err := os.WriteFile(hq, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var dnevnik bytes.Buffer
	log.SetOutput(&dnevnik)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	necitljiva := "Datoteka krivulja letve primjerovo ne da se pročitati, pa se izvoz ne može spojiti s njom. " +
		"Razlog je zapisan u dnevnik čvora."
	if g := posalji(); g != necitljiva || !strings.Contains(dnevnik.String(), put) {
		t.Errorf("mapa hq je datoteka: %q, dnevnik %q", g, dnevnik.String())
	}
	// Na mjestu datoteke krivulja stoji mapa.
	if err := os.Remove(hq); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(put, 0o755); err != nil {
		t.Fatal(err)
	}
	if g := posalji(); g != necitljiva {
		t.Errorf("datoteka krivulja je mapa: %q", g)
	}
	// Na mjestu izvornika stoji mapa.
	if err := os.Remove(put); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(hq, "izvornik", "krivulje.csv"), 0o755); err != nil {
		t.Fatal(err)
	}
	if g := posalji(); !strings.HasPrefix(g, "izvornik se nije spremio: ") {
		t.Errorf("izvornik se ne da spremiti: %q", g)
	}
	if _, ima := uvozi.uzmi(dobra, korisnik); !ima {
		t.Error("neuspio upis potrošio je odabir")
	}
}

// Kad gradnja letve nakon upisa krivulja padne, posao javlja njezinu grešku.
func TestKrivuljeGradnjaPada(t *testing.T) {
	o := novaOkolinaVrata(t)
	o.h.izgradi = func(string, io.Writer) (arhiva.Izvjestaj, error) {
		return arhiva.Izvjestaj{}, errors.New("arhiva je zaključana")
	}
	id := novIdUvoza()
	uvozi.spremi(id, "krivulje.csv", []byte(krivuljePrimjerova), o.pero.ID.String())
	t.Cleanup(func() { uvozi.makni(id) })
	w := odgovor(o.h.UpisiKrivulje, obicniObrazac("/", "letva=primjerovo&id="+id), o.pero, o.peroOvl)
	p := posaoSaStranice(t, o.h.poslovi, w.Body.String(), o.pero)
	if p.Greska() != "arhiva je zaključana" || p.Stanje().Sazetak != "" {
		t.Errorf("pad gradnje: %q %q", p.Greska(), p.Stanje().Sazetak)
	}
}

// Krivulje iz izvoza spajaju se sa zatečenima po razdoblju: zatečena
// krivulja kojeg izvoz nema ostaje, drukčiju istog početka zamijeni izvoz, a
// pregled na stranici to kaže prije upisa. Izvornik drugog izvoza pod istim
// imenom ostaje, a novi dobiva šifru postaje. Ista potvrda istog izvoza
// drugi put ne mijenja ništa.
func TestKrivuljeUpisSpajaSaZatecenima(t *testing.T) {
	o := novaOkolinaVrata(t)
	hq := filepath.Join(o.koren, "primjerica", "primjerovo", "hq")
	put := filepath.Join(hq, "primjerovo_hq_krivulje.csv")
	if err := os.MkdirAll(filepath.Join(hq, "izvornik"), 0o755); err != nil {
		t.Fatal(err)
	}
	zateceno := "vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena\n" +
		"1990-01-01;2000-12-31;0;400;polinom;30;200;500;;DHMZ, HIS-2000;\n" +
		"2010-01-01;;0;300;polinom;41;300;600;;DHMZ, HIS-2000;\n"
	for ime, sadrzaj := range map[string]string{put: zateceno, filepath.Join(hq, "izvornik", "krivulje.csv"): "stari izvoz"} {
		if err := os.WriteFile(ime, []byte(sadrzaj), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tekst := func(html string) string {
		return strings.Join(strings.Fields(htmlUTekst(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(html, " "))), " ")
	}
	pregled := func() string {
		r := viseDijelni(t, "/administracija/uvoz-krivulja/pregled", map[string]string{"letva": "primjerovo"},
			datotekaObrasca{"datoteka", "krivulje.csv", krivuljePrimjerova})
		html := odgovor(o.h.PregledUvozaKrivulja, r, o.pero, o.peroOvl).Body.String()
		m := regexp.MustCompile(`name="id" value="([^"]+)"`).FindStringSubmatch(html)
		if m == nil {
			t.Fatalf("pregled nema odabir za potvrdu: %s", porukeStranice(html))
		}
		for _, zeli := range []string{
			"Izvoz se spaja sa zatečenim krivuljama po razdoblju. Novo razdoblje se dodaje, isto razdoblje uzima se iz izvoza, " +
				"a zatečena razdoblja kojih u izvozu nema ostaju. 1 zatečena krivulja nije u izvozu i ostaje. " +
				"Nakon upisa letva ima 3 krivulje.",
			"1 zatečena krivulja razlikuje se od izvoza. Upis za to razdoblje uzima krivulju iz izvoza. 2010-01-01 .. 2012-12-31",
			"2010-01-01 .. 2012-12-31 1 odsječak razlikuje se od zatečene",
		} {
			if !strings.Contains(tekst(html), zeli) {
				t.Errorf("pregled ne kaže %q:\n%s", zeli, tekst(html))
			}
		}
		if strings.Contains(tekst(html), "2001-01-01 .. 2009-12-31 2 odsječka razlikuje se") {
			t.Error("nova krivulja označena je kao drukčija")
		}
		return m[1]
	}
	upisi := func(id string) {
		w := odgovor(o.h.UpisiKrivulje, obicniObrazac("/", "letva=primjerovo&id="+id), o.pero, o.peroOvl)
		if p := posaoSaStranice(t, o.h.poslovi, w.Body.String(), o.pero); p.Greska() != "" {
			t.Fatalf("upis: %q", p.Greska())
		}
	}

	upisi(pregled())
	spojeno := "vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena\n" +
		"1990-01-01;2000-12-31;0;400;polinom;30;200;500;;DHMZ, HIS-2000;\n" +
		"2001-01-01;2009-12-31;100;500;polinom;40;300;600;;DHMZ, HIS-2000;\n" +
		"2001-01-01;2009-12-31;0;100;potencija;50;2;0,5;10;DHMZ, HIS-2000;\n" +
		"2010-01-01;;0;300;polinom;40;300;600;;DHMZ, HIS-2000;\n"
	if got := procitaj(t, put); got != spojeno {
		t.Errorf("spojene krivulje:\n%s", got)
	}
	if procitaj(t, filepath.Join(hq, "izvornik", "krivulje.csv")) != "stari izvoz" ||
		procitaj(t, filepath.Join(hq, "izvornik", "krivulje-9001.csv")) != krivuljePrimjerova {
		t.Error("izvornik novog izvoza pregazio je stari ili nije spremljen pod šifrom postaje")
	}

	// Isti izvoz još jednom: pregled više nema razlika, a ništa se ne mijenja.
	r := viseDijelni(t, "/administracija/uvoz-krivulja/pregled", map[string]string{"letva": "primjerovo"},
		datotekaObrasca{"datoteka", "krivulje.csv", krivuljePrimjerova})
	html := odgovor(o.h.PregledUvozaKrivulja, r, o.pero, o.peroOvl).Body.String()
	if strings.Contains(tekst(html), "razlikuje se") || !strings.Contains(tekst(html), "Nakon upisa letva ima 3 krivulje.") {
		t.Errorf("drugi pregled istog izvoza:\n%s", tekst(html))
	}
	upisi(regexp.MustCompile(`name="id" value="([^"]+)"`).FindStringSubmatch(html)[1])
	if got := procitaj(t, put); got != spojeno {
		t.Errorf("isti izvoz promijenio je datoteku:\n%s", got)
	}
	if stavke, _ := os.ReadDir(filepath.Join(hq, "izvornik")); len(stavke) != 2 {
		t.Errorf("isti izvoz spremljen je dvaput: %d izvornika", len(stavke))
	}
}

// Zatečena krivulja koja počinje usred krivulje iz izvoza ostaje, a pregled
// prije upisa kaže da od svog početka vrijedi ona.
func TestPregledKrivuljaUsredIzvoza(t *testing.T) {
	o := novaOkolinaVrata(t)
	hq := filepath.Join(o.koren, "primjerica", "primjerovo", "hq")
	if err := os.MkdirAll(hq, 0o755); err != nil {
		t.Fatal(err)
	}
	tekst := func(html string) string {
		return strings.Join(strings.Fields(htmlUTekst(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(html, " "))), " ")
	}
	for _, sl := range []struct{ zateceno, zeli string }{
		{"2005-01-01;2009-12-31;0;400;polinom;30;200;500;;DHMZ, HIS-2000;\n",
			"1 zatečena krivulja počinje usred krivulje iz izvoza. " +
				"Ostaje u datoteci, pa od svog početka vrijedi umjesto krivulje iz izvoza. od 2005-01-01"},
		{"2003-01-01;2004-12-31;0;400;polinom;30;200;500;;DHMZ, HIS-2000;\n" +
			"2011-01-01;;0;400;polinom;31;200;500;;DHMZ, HIS-2000;\n",
			"2 zatečene krivulje počinju usred krivulja iz izvoza. " +
				"Ostaju u datoteci, pa svaka od svog početka vrijedi umjesto krivulje iz izvoza. od 2003-01-01 od 2011-01-01"},
	} {
		zateceno := "vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena\n" + sl.zateceno
		if err := os.WriteFile(filepath.Join(hq, "primjerovo_hq_krivulje.csv"), []byte(zateceno), 0o644); err != nil {
			t.Fatal(err)
		}
		r := viseDijelni(t, "/administracija/uvoz-krivulja/pregled", map[string]string{"letva": "primjerovo"},
			datotekaObrasca{"datoteka", "krivulje.csv", krivuljePrimjerova})
		if got := tekst(odgovor(o.h.PregledUvozaKrivulja, r, o.pero, o.peroOvl).Body.String()); !strings.Contains(got, sl.zeli) {
			t.Errorf("pregled ne kaže %q:\n%s", sl.zeli, got)
		}
	}
}

// Odabir čeka uz oznaku osobe; bez prijavljene osobe oznaka je prazna.
func TestKorisnikUvoza(t *testing.T) {
	o := novaOkolinaVrata(t)
	if got := korisnikUvoza(UvozPageData{}); got != "" {
		t.Errorf("bez osobe: %q", got)
	}
	d := UvozPageData{}
	d.CurrentUser = o.pero
	if got := korisnikUvoza(d); got != o.pero.ID.String() {
		t.Errorf("s osobom: %q", got)
	}
}
