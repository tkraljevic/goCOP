package web

import (
	"bytes"
	"database/sql"
	"html/template"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/arhiva"
	"gocop/internal/models"
)

// okolinaVrata je izmišljeno stablo arhive: sliv primjerica s letvama
// primjerovo i drugovo, i arhiva s izvorom probni (uključen) i cekajuci
// (isključen). Pero Perić je globalni administrator, Ana Anić administratorica
// područja koja smije samo primjerovo, a Marko Markić nema nijednu letvu.
type okolinaVrata struct {
	h                *UvozHandler
	koren, arhivaPut string
	pero, ana, marko *models.User
	peroOvl, anaOvl  *models.UserPermissions
	markoOvl         *models.UserPermissions
}

func novaOkolinaVrata(t *testing.T) *okolinaVrata {
	t.Helper()
	o := &okolinaVrata{koren: t.TempDir(), arhivaPut: filepath.Join(t.TempDir(), "arhiva.db")}
	for _, l := range []string{"primjerovo", "drugovo"} {
		if err := os.MkdirAll(filepath.Join(o.koren, "primjerica", l), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	db, err := sql.Open("sqlite", o.arhivaPut)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := arhiva.PripremiPraznu(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO izvori (naziv, tocnost, red, ukljucen) VALUES ('probni', 5, 20, 1), ('cekajuci', 5, 30, 0)`); err != nil {
		t.Fatal(err)
	}
	o.h = NewUvozHandler(func() string { return o.arhivaPut }, func() string { return o.koren },
		func(string, io.Writer) (arhiva.Izvjestaj, error) { return arhiva.Izvjestaj{}, nil }, predlozakVrata(t))
	o.h.SetOvlastiLetve(func(p *models.UserPermissions, letva string) bool {
		return p != nil && p.AllowedSections["P"] && letva == "primjerovo"
	})
	o.pero = &models.User{ID: uuid.New(), FullName: "Pero Perić", IsGlobalAdmin: true}
	o.peroOvl = &models.UserPermissions{User: *o.pero, IsGlobalAdmin: true}
	o.ana = &models.User{ID: uuid.New(), FullName: "Ana Anić"}
	o.anaOvl = &models.UserPermissions{User: *o.ana, AllowedSections: map[string]bool{"P": true}}
	o.marko = &models.User{ID: uuid.New(), FullName: "Marko Markić"}
	o.markoOvl = &models.UserPermissions{User: *o.marko, AllowedSections: map[string]bool{}}
	return o
}

// zovi šalje obrazac rukovatelju u ime osobe
func (o *okolinaVrata) zovi(h http.HandlerFunc, u *models.User, p *models.UserPermissions, putanja string, forma url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, putanja, strings.NewReader(forma.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h(w, zahtjevSKorisnikom(r, u, p))
	return w
}

// posalji šalje datoteku na pregled i vraća broj pod kojim čeka potvrdu
func (o *okolinaVrata) posalji(t *testing.T, u *models.User, p *models.UserPermissions, ime, sadrzaj string) string {
	t.Helper()
	var tijelo bytes.Buffer
	mw := multipart.NewWriter(&tijelo)
	fw, err := mw.CreateFormFile("datoteka", ime)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte(sadrzaj)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/administracija/uvoz-niza/pregled", &tijelo)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	o.h.PregledUvoza(w, zahtjevSKorisnikom(r, u, p))
	id := regexp.MustCompile(`name="id" value="([^"]+)"`).FindStringSubmatch(w.Body.String())
	if id == nil {
		t.Fatalf("pregled %s nije dao broj za potvrdu: %d", ime, w.Code)
	}
	return id[1]
}

// datoteke vraća sve datoteke u stablu, da se vidi je li išta zapisano
func (o *okolinaVrata) datoteke(t *testing.T) []string {
	t.Helper()
	var out []string
	if err := filepath.WalkDir(o.koren, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			out = append(out, p)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

const dnevniPrimjerova = "datum;vodostaj_cm\n2026-03-01;120\n2026-03-02;125\n2026-03-03;131\n"

// zatecenPrimjerova upisuje u stablo niz koji letva već ima: dan prije nove
// datoteke i prvi dan koji se s njom poklapa
func (o *okolinaVrata) zatecenPrimjerova(t *testing.T) {
	t.Helper()
	put := filepath.Join(o.koren, "primjerica", "primjerovo", "primjerovo_probni_vodostaj_dnevni_2026.csv")
	if err := os.WriteFile(put, []byte("datum;vodostaj_cm\n2026-02-28;118\n2026-03-01;119\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Okvir „Niz već postoji” stiže čim čovjek upiše letvu, izvor i vrstu, bez
// osvježavanja cijele stranice: iz naziva „satni.csv” poslužitelj ne zna o
// kojem je nizu riječ, pa ga gleda tek po obrascu. Zamjena se pokazuje s
// onim što bi odnijela.
func TestZatecenNizPoObrascu(t *testing.T) {
	o := novaOkolinaVrata(t)
	o.zatecenPrimjerova(t)
	id := o.posalji(t, o.pero, o.peroOvl, "satni.csv", dnevniPrimjerova)
	prije := o.datoteke(t)

	niz := url.Values{"id": {id}, "sliv": {"primjerica"}, "letva": {"primjerovo"}, "izvor": {"probni"},
		"velicina": {"vodostaj"}, "vrsta": {"dnevni"}, "nacin": {"zamijeni"}}
	w := o.zovi(o.h.Zatecen, o.pero, o.peroOvl, "/administracija/uvoz-niza/zatecen", niz)
	html := w.Body.String()
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" ||
		!strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("okvir zatečenog niza: %d, zaglavlja %v", w.Code, w.Header())
	}
	for _, treba := range []string{"Niz već postoji", "U stablu stoji 2 vrijednosti, 28.02.2026. – 01.03.2026.",
		"Zamjena bi odnijela <strong>1</strong> vrijednosti", "(28.02.2026. – 28.02.2026.)",
		`value="zamijeni" checked`} {
		if !strings.Contains(html, treba) {
			t.Errorf("okvir nema %q:\n%s", treba, html)
		}
	}
	if strings.Contains(html, "<html") {
		t.Error("okvir je cijela stranica, a treba samo njegov dio")
	}

	// Niz kojeg u stablu nema daje prazan okvir.
	niz.Set("izvor", "cekajuci")
	if w := o.zovi(o.h.Zatecen, o.pero, o.peroOvl, "/administracija/uvoz-niza/zatecen", niz); w.Code != http.StatusOK ||
		strings.Contains(w.Body.String(), "Niz već postoji") {
		t.Errorf("niz bez zatečenih vrijednosti: %d %q", w.Code, w.Body.String())
	}
	if poslije := o.datoteke(t); len(poslije) != len(prije) {
		t.Errorf("gledanje zatečenog je pisalo u stablo: %v", poslije)
	}
}

// Okvir zatečenog niza ne otvara ništa što stranica ne bi otvorila: bez
// prava se odbija, istekao ili tuđi odabir i tuđa letva daju prazan odgovor,
// a neispravan obrazac je loš zahtjev.
func TestZatecenNizOdbija(t *testing.T) {
	o := novaOkolinaVrata(t)
	o.zatecenPrimjerova(t)
	id := o.posalji(t, o.ana, o.anaOvl, "satni.csv", dnevniPrimjerova)
	obrazac := func(letva string) url.Values {
		return url.Values{"id": {id}, "sliv": {"primjerica"}, "letva": {letva}, "izvor": {"probni"},
			"velicina": {"vodostaj"}, "vrsta": {"dnevni"}}
	}
	for _, s := range []struct {
		opis   string
		u      *models.User
		p      *models.UserPermissions
		forma  url.Values
		status int
	}{
		{"bez ijedne letve", o.marko, o.markoOvl, obrazac("primjerovo"), http.StatusForbidden},
		{"neprijavljen", nil, nil, obrazac("primjerovo"), http.StatusForbidden},
		{"tuđi odabir", o.pero, o.peroOvl, obrazac("primjerovo"), http.StatusOK},
		{"istekao odabir", o.ana, o.anaOvl, url.Values{"id": {"nema-ga"}, "letva": {"primjerovo"}}, http.StatusOK},
		{"tuđa letva", o.ana, o.anaOvl, obrazac("drugovo"), http.StatusOK},
	} {
		t.Run(s.opis, func(t *testing.T) {
			w := o.zovi(o.h.Zatecen, s.u, s.p, "/administracija/uvoz-niza/zatecen", s.forma)
			if w.Code != s.status {
				t.Fatalf("status %d, očekivan %d", w.Code, s.status)
			}
			if s.status == http.StatusOK && w.Body.Len() != 0 {
				t.Errorf("okvir mora ostati prazan: %q", w.Body.String())
			}
		})
	}

	r := httptest.NewRequest(http.MethodPost, "/administracija/uvoz-niza/zatecen", strings.NewReader("id=%zz"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	o.h.Zatecen(w, zahtjevSKorisnikom(r, o.ana, o.anaOvl))
	if w.Code != http.StatusBadRequest {
		t.Errorf("neispravan obrazac: %d", w.Code)
	}

	// Svoja letva i svoj odabir daju okvir i administratorici područja.
	if w := o.zovi(o.h.Zatecen, o.ana, o.anaOvl, "/administracija/uvoz-niza/zatecen", obrazac("primjerovo")); !strings.Contains(w.Body.String(), "Niz već postoji") {
		t.Errorf("svoja letva: %d %q", w.Code, w.Body.String())
	}
}

// Kad se dio stranice ne da iscrtati, odgovor je greška poslužitelja, a ne
// prazan okvir koji bi izgledao kao da niza nema.
func TestZatecenNizBezPredloskaJeGreska(t *testing.T) {
	o := novaOkolinaVrata(t)
	o.zatecenPrimjerova(t)
	id := o.posalji(t, o.pero, o.peroOvl, "primjerovo_probni_vodostaj_dnevni.csv", dnevniPrimjerova)
	o.h.tmpl = template.Must(template.New("prazan").Parse(""))
	w := o.zovi(o.h.Zatecen, o.pero, o.peroOvl, "/administracija/uvoz-niza/zatecen", url.Values{"id": {id}})
	if w.Code != http.StatusInternalServerError {
		t.Errorf("predložak bez okvira: %d %q", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	o.h.pisi(w, UvozPageData{})
	if w.Code != http.StatusInternalServerError {
		t.Errorf("stranica bez predloška: %d", w.Code)
	}
}

// Osvježeni pregled preračunava isti odabir s onim što je čovjek ispravio,
// bez ponovnog slanja datoteke i bez ikakvog upisa u stablo.
func TestPonovljeniPregledPrimaIspravke(t *testing.T) {
	o := novaOkolinaVrata(t)
	sadrzaj := "datum;protok;vodostaj\n2026-03-01;40;120\n2026-03-02;45;125\n2026-03-03;50;131\n"
	id := o.posalji(t, o.pero, o.peroOvl, "primjerovo_probni_vodostaj_dnevni.csv", sadrzaj)

	w := o.zovi(o.h.PonoviPregled, o.pero, o.peroOvl, "/administracija/uvoz-niza/pregled-ponovo", url.Values{"id": {id}})
	if html := w.Body.String(); w.Code != http.StatusOK || !strings.Contains(html, "40,0 – 50,0") {
		t.Fatalf("pregled bez ispravka uzima prvi stupac s brojevima: %d", w.Code)
	}
	w = o.zovi(o.h.PonoviPregled, o.pero, o.peroOvl, "/administracija/uvoz-niza/pregled-ponovo",
		url.Values{"id": {id}, "stupac_vrijednost": {"2"}, "nacin": {"zamijeni"}})
	html := w.Body.String()
	for _, treba := range []string{"Što je pročitano", "primjerovo_probni_vodostaj_dnevni.csv", "120,0 – 131,0",
		"01.03.2026. – 03.03.2026.", `name="id" value="` + id + `"`} {
		if !strings.Contains(html, treba) {
			t.Errorf("osvježeni pregled nema %q", treba)
		}
	}
	if d := o.datoteke(t); len(d) != 0 {
		t.Errorf("pregled je zapisao u stablo: %v", d)
	}
}

// Osvježeni pregled pazi isto što i prvi: bez prava se ne otvara, istekao
// odabir se javlja, tuđa letva se ne pregledava, a neispravan obrazac ne
// prolazi tiho.
func TestPonovljeniPregledOdbija(t *testing.T) {
	o := novaOkolinaVrata(t)
	id := o.posalji(t, o.ana, o.anaOvl, "primjerovo_probni_vodostaj_dnevni.csv", dnevniPrimjerova)
	for _, s := range []struct {
		opis   string
		u      *models.User
		p      *models.UserPermissions
		forma  url.Values
		status int
		poruka string
	}{
		{"bez ijedne letve", o.marko, o.markoOvl, url.Values{"id": {id}}, http.StatusForbidden, "unosi administrator"},
		{"tuđi odabir", o.pero, o.peroOvl, url.Values{"id": {id}}, http.StatusOK, "Odabir je istekao"},
		{"istekao odabir", o.ana, o.anaOvl, url.Values{"id": {"nema-ga"}}, http.StatusOK, "Odabir je istekao"},
		{"tuđa letva", o.ana, o.anaOvl, url.Values{"id": {id}, "letva": {"drugovo"}}, http.StatusOK, "letva drugovo nije na vašim dionicama"},
	} {
		t.Run(s.opis, func(t *testing.T) {
			w := o.zovi(o.h.PonoviPregled, s.u, s.p, "/administracija/uvoz-niza/pregled-ponovo", s.forma)
			if w.Code != s.status || !strings.Contains(w.Body.String(), s.poruka) {
				t.Errorf("status %d, očekivan %d s porukom %q", w.Code, s.status, s.poruka)
			}
		})
	}
	r := httptest.NewRequest(http.MethodPost, "/administracija/uvoz-niza/pregled-ponovo", strings.NewReader("id=%zz"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	o.h.PonoviPregled(w, zahtjevSKorisnikom(r, o.ana, o.anaOvl))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "invalid URL escape") {
		t.Errorf("neispravan obrazac mora se javiti na stranici: %d", w.Code)
	}
	if d := o.datoteke(t); len(d) != 0 {
		t.Errorf("odbijen pregled je zapisao u stablo: %v", d)
	}
}

// Prazna vrata: tko nema nijednu letvu ne otvara ih, administratorica
// područja na popisu vidi samo svoje letve, a čvor bez stabla kaže da se u
// njegovu arhivu ne unosi.
func TestPraznaVrataUvoza(t *testing.T) {
	o := novaOkolinaVrata(t)
	poziv := func(u *models.User, p *models.UserPermissions) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		o.h.ShowUvoz(w, zahtjevSKorisnikom(httptest.NewRequest(http.MethodGet, "/administracija/uvoz-niza", nil), u, p))
		return w
	}
	if w := poziv(o.marko, o.markoOvl); w.Code != http.StatusForbidden {
		t.Errorf("bez ijedne letve: %d", w.Code)
	}
	w := poziv(o.ana, o.anaOvl)
	popis := regexp.MustCompile(`(?s)<datalist id="popis-letvi-krivulje">(.*?)</datalist>`).FindStringSubmatch(w.Body.String())
	if w.Code != http.StatusOK || popis == nil || !strings.Contains(popis[1], `value="primjerovo"`) || strings.Contains(popis[1], "drugovo") {
		t.Errorf("popis letava administratorice područja: %d %v", w.Code, popis)
	}
	if strings.Contains(w.Body.String(), "nema stablo") {
		t.Error("čvor sa stablom javlja da ga nema")
	}

	bezStabla := NewUvozHandler(func() string { return "" }, func() string { return "" }, nil, predlozakVrata(t))
	w = httptest.NewRecorder()
	bezStabla.ShowUvoz(w, zahtjevSKorisnikom(httptest.NewRequest(http.MethodGet, "/administracija/uvoz-niza", nil), o.pero, o.peroOvl))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Ovaj čvor nema stablo s izvornim datotekama") {
		t.Errorf("čvor bez stabla: %d", w.Code)
	}
}

// Izdavanje i provjera izdanja su zahvat nad cijelom arhivom, pa ih
// administratorica područja ne pokreće ni sa svojom letvom.
func TestIzdavanjeSamoGlobalnom(t *testing.T) {
	o := novaOkolinaVrata(t)
	for _, h := range []http.HandlerFunc{o.h.ProvjeriIzdanja, o.h.Izdaj} {
		w := o.zovi(h, o.ana, o.anaOvl, "/administracija/uvoz-niza/izdaj", url.Values{"letva": {"primjerovo"}})
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "globalni administrator") {
			t.Errorf("izdavanje administratorici područja: %d %q", w.Code, w.Body.String())
		}
	}
}

// Popis izvora stiže iz arhive; bez arhive ili kad se ne da pročitati popisa
// nema, pa se ime izvora upisuje rukom.
func TestImenaIzvoraIzArhive(t *testing.T) {
	o := novaOkolinaVrata(t)
	imena := strings.Join(o.h.imenaIzvora(), ",")
	if !strings.Contains(imena, "probni") || !strings.Contains(imena, "cekajuci") {
		t.Errorf("izvori iz arhive: %s", imena)
	}
	for opis, put := range map[string]string{"bez arhive": "", "arhive nema": filepath.Join(t.TempDir(), "nema.db")} {
		h := NewUvozHandler(func() string { return put }, func() string { return o.koren }, nil, nil)
		if got := h.imenaIzvora(); got != nil {
			t.Errorf("%s: %v", opis, got)
		}
	}
}

// Novi izvor ulazi isključen, pa se nakon upisa pita ulazi li u spoj. Kad se
// arhiva ne da pročitati, program ne plaši čovjeka; kad arhive nema, ništa se
// ni ne vidi.
func TestIzvorUlaziUSpoj(t *testing.T) {
	o := novaOkolinaVrata(t)
	for izvor, ulazi := range map[string]bool{"probni": true, "cekajuci": false, "nepoznat": false} {
		if got := o.h.izvorUlaziUSpoj(izvor); got != ulazi {
			t.Errorf("%s: ulazi %v, očekivano %v", izvor, got, ulazi)
		}
	}
	bezArhive := NewUvozHandler(func() string { return "" }, func() string { return o.koren }, nil, nil)
	if bezArhive.izvorUlaziUSpoj("probni") {
		t.Error("bez arhive ništa ne ulazi u spoj")
	}
	necitljiva := NewUvozHandler(func() string { return filepath.Join(t.TempDir(), "nema.db") }, func() string { return o.koren }, nil, nil)
	if !necitljiva.izvorUlaziUSpoj("probni") {
		t.Error("kad se arhiva ne čita, ne smije se javiti da izvor ne ulazi")
	}
	if s, err := sirotaniUArhivi(o.arhivaPut, o.koren); err != nil || len(s) != 0 {
		t.Errorf("prazna arhiva nema sirotana: %v, %v", s, err)
	}
}

// Naziv iz naše arhive kazuje sve; naziv s nedopuštenim dijelom daje samo
// letvu i izvor, a kraći naziv ništa. Sliv se uzima iz stabla.
func TestPrijedlogIzNaziva(t *testing.T) {
	slivZa := map[string]string{"primjerovo": "primjerica"}
	for _, s := range []struct {
		ime  string
		zeli UvozNiza
	}{
		{"primjerovo_probni_vodostaj_dnevni.csv", UvozNiza{Sliv: "primjerica", Letva: "primjerovo", Izvor: "probni", Velicina: "vodostaj", Vrsta: "dnevni"}},
		{"primjerovo_probni_protok_satni_2001-2026.XLSX", UvozNiza{Sliv: "primjerica", Letva: "primjerovo", Izvor: "probni", Velicina: "protok", Vrsta: "satni"}},
		{"drugovo_probni_razina_satni.CSV", UvozNiza{Letva: "drugovo", Izvor: "probni"}},
		{"satni.csv", UvozNiza{}},
	} {
		if got := prijedlogIzNaziva(s.ime, slivZa); got != s.zeli {
			t.Errorf("%s: %+v, očekivano %+v", s.ime, got, s.zeli)
		}
	}
}

// Obrazac prepisuje prijedlog samo onim što je čovjek stvarno upisao: prazno
// polje i negativan ili nečitljiv stupac ostavljaju prijedlog.
func TestObrazacPrepisujePrijedlog(t *testing.T) {
	polazni := UvozNiza{Sliv: "s", Letva: "l", Izvor: "i", Velicina: "vodostaj", Vrsta: "satni", Zona: "UTC",
		Nacin: "dopuni", StupacVrijeme: 1, StupacVrijednost: 2}
	u := polazni
	primiIzObrasca(&u, nil)
	if u != polazni {
		t.Errorf("bez zahtjeva prijedlog se mijenja: %+v", u)
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(url.Values{
		"sliv": {" primjerica "}, "letva": {"primjerovo"}, "izvor": {"probni"}, "velicina": {"protok"},
		"vrsta": {"dnevni"}, "zona": {"Europe/Zagreb"}, "nacin": {"zamijeni"},
		"stupac_vrijeme": {"0"}, "stupac_vrijednost": {"3"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	primiIzObrasca(&u, r)
	zeli := UvozNiza{Sliv: "primjerica", Letva: "primjerovo", Izvor: "probni", Velicina: "protok", Vrsta: "dnevni",
		Zona: "Europe/Zagreb", Nacin: "zamijeni", StupacVrijeme: 0, StupacVrijednost: 3}
	if u != zeli {
		t.Errorf("ispravak iz obrasca: %+v", u)
	}
	u = polazni
	r = httptest.NewRequest(http.MethodPost, "/", strings.NewReader("letva=+&stupac_vrijeme=-1&stupac_vrijednost=x"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	primiIzObrasca(&u, r)
	if u != polazni {
		t.Errorf("prazno i nečitljivo ne smiju prepisati prijedlog: %+v", u)
	}
}

// Datoteka čeka potvrdu pola sata i samo onoga tko ju je poslao; istekle se
// pri sljedećem spremanju pospremaju.
func TestOdabirCekaSamoPosiljatelja(t *testing.T) {
	u := uvoziUTijeku{m: map[string]cekaUvoz{}}
	u.m["staro"] = cekaUvoz{ime: "staro.csv", korisnik: "pero", istice: time.Now().Add(-time.Minute)}
	u.spremi("novo", "novo.csv", []byte("x"), "pero")
	if _, ima := u.m["staro"]; ima {
		t.Error("istekli odabir je ostao")
	}
	if c, ima := u.uzmi("novo", "pero"); !ima || c.ime != "novo.csv" || string(c.sadrzaj) != "x" {
		t.Errorf("svoj odabir: %+v %v", c, ima)
	}
	if _, ima := u.uzmi("novo", "ana"); ima {
		t.Error("tuđi odabir se ne smije uzeti")
	}
	if _, ima := u.uzmi("nema", "pero"); ima {
		t.Error("nepostojeći odabir")
	}
	u.m["isteklo"] = cekaUvoz{korisnik: "pero", istice: time.Now().Add(-time.Second)}
	if _, ima := u.uzmi("isteklo", "pero"); ima {
		t.Error("istekli odabir se ne smije uzeti")
	}
	u.makni("novo")
	if _, ima := u.uzmi("novo", "pero"); ima {
		t.Error("maknuti odabir je ostao")
	}
}

// Sažetak izdavanja je jedna rečenica, s letvom u pravom padežu.
func TestSazetakIzdanja(t *testing.T) {
	for _, s := range []struct {
		iz   arhiva.IzvjestajIzdanja
		zeli string
	}{
		{arhiva.IzvjestajIzdanja{Probno: true}, "Ništa se nije promijenilo — nema što izdati."},
		{arhiva.IzvjestajIzdanja{Probno: true, Promijenjenih: 3, Istih: 7}, "3 letve bi dobilo novo izdanje; 7 je nepromijenjeno."},
		{arhiva.IzvjestajIzdanja{}, "Katalog je osvježen; nijedna letva nije trebala novo izdanje."},
		{arhiva.IzvjestajIzdanja{Promijenjenih: 21, Istih: 2, Mapa: "/paketi"}, "Izdano 21 letva u /paketi; 2 nepromijenjeno."},
	} {
		if got := sazetakIzdanja(s.iz); got != s.zeli {
			t.Errorf("%+v: %q, očekivano %q", s.iz, got, s.zeli)
		}
	}
}

// Letva se slaže s brojem po hrvatskom pravilu, i u drugoj desetici.
func TestLetvaUzBroj(t *testing.T) {
	for n, zeli := range map[int]string{
		0: "letvi", 1: "letva", 2: "letve", 4: "letve", 5: "letvi", 11: "letvi", 12: "letvi", 14: "letvi",
		21: "letva", 22: "letve", 101: "letva", 111: "letvi", 112: "letvi", 124: "letve",
	} {
		if got := uzBrojLetva(n); got != zeli {
			t.Errorf("%d %s, očekivano %d %s", n, got, n, zeli)
		}
	}
}

// Excel se čita kao tablica bez razdjelnika, a nastavak se prepoznaje bez
// obzira na velika slova.
func TestVrataCitajuExcel(t *testing.T) {
	redci, razdjelnik, err := citajTablicu("Primjerovo.XLSX", napraviXLSX(t, [][]string{
		{"datum", "vodostaj"}, {"2026-03-01", "120"}, {"2026-03-02", "125"}}))
	if err != nil {
		t.Fatal(err)
	}
	if razdjelnik != "" || len(redci) != 3 || redci[2][1] != "125" {
		t.Errorf("Excel: razdjelnik %q, redci %v", razdjelnik, redci)
	}
	if _, _, err := citajTablicu("krivo.xlsx", []byte("nije zip")); err == nil {
		t.Error("datoteka koja nije Excel prošla je kao Excel")
	}
}

// Stupac je stupac vremena ili brojeva samo kad se svaka popunjena ćelija
// tako čita; prazne se preskaču, a stupac bez ijedne popunjene nije ni jedno.
func TestStupacSvihVremenaIBrojeva(t *testing.T) {
	redci := [][]string{
		{"2026-03-01", "120", "x", ""},
		{"", "", "", ""},
		{"02.03.2026.", "125,5", "131", ""},
		{"3.3.2026", " 1 300 ", "y"},
	}
	for _, s := range []struct {
		stupac        int
		vrijeme, broj bool
	}{
		{0, true, false},
		{1, false, true},
		{2, false, false},
		{3, false, false},
		{7, false, false},
	} {
		if got := sviSuVrijeme(redci, s.stupac); got != s.vrijeme {
			t.Errorf("stupac %d: sve vrijeme %v, očekivano %v", s.stupac, got, s.vrijeme)
		}
		if got := sviSuBroj(redci, s.stupac); got != s.broj {
			t.Errorf("stupac %d: sve brojevi %v, očekivano %v", s.stupac, got, s.broj)
		}
	}
	if celija([]string{"a"}, -1) != "" || celija([]string{"a"}, 1) != "" || celija([]string{" a "}, 0) != "a" {
		t.Error("ćelija izvan retka mora biti prazna, a unutar njega bez razmaka")
	}
	if _, _, ok := procitajVrijeme("   "); ok {
		t.Error("prazno vrijeme je pročitano")
	}
	if _, ok := procitajBroj("  "); ok {
		t.Error("prazan broj je pročitan")
	}
}
