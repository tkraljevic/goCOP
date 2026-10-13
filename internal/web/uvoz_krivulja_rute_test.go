package web

import (
	"errors"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
// potvrdu. Danas puca jer predložak u odjeljku krivulja čita .Profili, a uz
// krivulje snimki nema.
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
// nije višedijelni, obrazac bez datoteke, prevelika datoteka i datoteka koja
// nije izvoz HIS-a vraćaju poruku, a ništa ne čeka potvrdu.
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
// ne daju zapisati, poruka to kaže i gradnja ne kreće.
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
		{"datoteka se više ne čita", o.pero, o.peroOvl, "letva=primjerovo&id=" + pokvarena, http.StatusOK, "datoteka se više ne čita kao krivulje"},
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
	// Na mjestu mape hq stoji datoteka: mapa izvornika se ne da napraviti.
	if err := os.WriteFile(hq, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if g := posalji(); !strings.Contains(g, hq) {
		t.Errorf("mapa izvornika se ne da napraviti: %q", g)
	}
	// Na mjestu datoteke krivulja stoji mapa.
	if err := os.Remove(hq); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(put, 0o755); err != nil {
		t.Fatal(err)
	}
	if g := posalji(); !strings.HasPrefix(g, "upis nije uspio: ") || !strings.Contains(g, put) {
		t.Errorf("krivulje se ne daju zapisati: %q", g)
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
