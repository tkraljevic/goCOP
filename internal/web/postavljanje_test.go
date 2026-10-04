package web

import (
	"bytes"
	"context"
	"encoding/json"
	"html/template"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/peers"
	"gocop/internal/service"
)

// okolinaPostavljanja je svjež čvor: samo početni račun admin
type okolinaPostavljanja struct {
	*okolinaPrijave
	h     *PostavljanjeHandler
	mreza *peers.Service
}

func novaOkolinaPostavljanja(t *testing.T) *okolinaPostavljanja {
	o := novaOkolinaPrijave(t)
	o.racun("admin", db.ZadanaLozinka, true, true)
	dbPath := filepath.Join(t.TempDir(), "cvor.db")
	cvor, err := peers.LoadNode(dbPath, "pperic-thinkpad", "ThinkPad", "test")
	if err != nil {
		t.Fatal(err)
	}
	mreza, err := peers.NewService(o.baza, ledger.New(o.baza, "pperic-thinkpad"), cvor, peers.Ports{})
	if err != nil {
		t.Fatal(err)
	}
	svjez := func() bool {
		svi, err := o.repo.ListUsers("", 0, "", "", "")
		return err == nil && len(svi) <= 1
	}
	tmpl := template.Must(template.New("postavljanje.html").Parse(`{{if .Dopusteno}}dopusteno{{else}}zakljucano{{end}}|{{.Error}}|{{.Cvor}}|kod-primanja={{.KodPrimanja}}|u-mrezi={{.UMrezi}}`))
	h := &PostavljanjeHandler{users: service.NewUserService(o.repo, o.auth, service.NewSSEBroker()), auth: o.auth,
		peers: mreza, tmpl: tmpl, svjez: svjez, kod: "7KQ4-M2XD"}
	o.mux.HandleFunc("GET /postavljanje", h.Prikazi)
	o.mux.HandleFunc("POST /postavljanje", h.Osnuj)
	o.mux.HandleFunc("GET /postavljanje/zahtjev", h.Zahtjev)
	o.mux.HandleFunc("POST /postavljanje/zahtjev", h.NoviZahtjev)
	o.mux.HandleFunc("POST /postavljanje/potvrda", h.Potvrda)
	return &okolinaPostavljanja{okolinaPrijave: o, h: h, mreza: mreza}
}

func lokalno(r *http.Request) *http.Request {
	r.RemoteAddr = "127.0.0.1:40000"
	return r
}

func (o *okolinaPostavljanja) get(porijeklo func(*http.Request) *http.Request, upit string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	o.srv.ServeHTTP(w, porijeklo(httptest.NewRequest(http.MethodGet, "/postavljanje"+upit, nil)))
	return w
}

func (o *okolinaPostavljanja) post(porijeklo func(*http.Request) *http.Request, v url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/postavljanje", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	o.srv.ServeHTTP(w, porijeklo(r))
	return w
}

func obrazac(izmjene ...string) url.Values {
	v := url.Values{"nacin": {"nova"}, "mreza": {"Hrvatske vode — COP Osijek"}, "korisnik": {"pperic"},
		"ime": {"Pero Perić"}, "email": {"pperic@example.org"}, "lozinka": {"duga-lozinka-1"}, "lozinka2": {"duga-lozinka-1"},
		"razumijem": {"da"}}
	for i := 0; i+1 < len(izmjene); i += 2 {
		v.Set(izmjene[i], izmjene[i+1])
	}
	return v
}

// Svjež čvor postavlja samo vlasnik: s ovog računala, ili iz lokalne mreže
// s kodom iz dnevnika; kroz tunel nikako
func TestPostavljanjeSamoVlasnik(t *testing.T) {
	o := novaOkolinaPostavljanja(t)

	if w := o.get(lokalno, ""); !strings.HasPrefix(w.Body.String(), "dopusteno|") || !strings.Contains(w.Body.String(), "pperic-thinkpad") {
		t.Errorf("s ovog računala: %d %q", w.Code, w.Body.String())
	}
	if w := o.get(izravno, ""); !strings.HasPrefix(w.Body.String(), "zakljucano|") {
		t.Errorf("iz mreže bez koda: %q", w.Body.String())
	}
	if w := o.get(izravno, "?kod=AAAA-BBBB"); w.Code != http.StatusForbidden {
		t.Errorf("iz mreže s krivim kodom: %d", w.Code)
	}
	if w := o.get(izravno, "?kod=7kq4-m2xd"); !strings.HasPrefix(w.Body.String(), "dopusteno|") {
		t.Errorf("iz mreže s točnim kodom (mala slova): %q", w.Body.String())
	}
	if w := o.get(krozTunel("198.51.100.7"), "?kod=7KQ4-M2XD"); w.Code != http.StatusSeeOther {
		t.Errorf("kroz tunel s kodom: %d", w.Code)
	}
	if w := o.get(javnoIzravno, "?kod=7KQ4-M2XD"); w.Code != http.StatusSeeOther {
		t.Errorf("s javne adrese s kodom: %d", w.Code)
	}
	if w := o.post(javnoIzravno, obrazac("kod", "7KQ4-M2XD")); w.Code != http.StatusSeeOther || kolacicSesije(w) != nil {
		t.Errorf("POST s javne adrese s kodom: %d", w.Code)
	}
	if w := o.post(izravno, obrazac()); w.Code != http.StatusForbidden {
		t.Errorf("POST iz mreže bez koda: %d", w.Code)
	}
	if w := o.post(krozTunel("198.51.100.7"), obrazac("kod", "7KQ4-M2XD")); w.Code != http.StatusSeeOther || kolacicSesije(w) != nil {
		t.Errorf("POST kroz tunel s kodom: %d", w.Code)
	}

	// deset krivih kodova: ni točan više ne vrijedi
	for i := 0; i < najviseKrivihKodova; i++ {
		o.get(izravno, "?kod=ZZZZ-ZZZZ")
	}
	if w := o.get(izravno, "?kod=7KQ4-M2XD"); strings.HasPrefix(w.Body.String(), "dopusteno|") {
		t.Error("nakon deset krivih kodova točan i dalje vrijedi")
	}
	if w := o.get(lokalno, ""); !strings.HasPrefix(w.Body.String(), "dopusteno|") {
		t.Error("ovo računalo izgubilo je pristup zbog krivih kodova iz mreže")
	}
}

// Nova mreža: vlastiti administrator, isključen početni admin, osnovana
// mreža, otvorena sesija; nakon toga stranice više nema
func TestPostavljanjeNoveMreze(t *testing.T) {
	o := novaOkolinaPostavljanja(t)

	for _, s := range []struct {
		ime string
		v   url.Values
	}{
		{"različite lozinke", obrazac("lozinka2", "druga-lozinka-1")},
		{"kratka lozinka", obrazac("lozinka", "kratka", "lozinka2", "kratka")},
		{"javna lozinka", obrazac("lozinka", db.ZadanaLozinka, "lozinka2", db.ZadanaLozinka)},
		{"korisnik admin", obrazac("korisnik", "admin")},
		{"bez naziva mreže", obrazac("mreza", " ")},
		{"bez potvrde da je to nova mreža", obrazac("razumijem", "")},
	} {
		if w := o.post(lokalno, s.v); w.Code != http.StatusUnprocessableEntity || kolacicSesije(w) != nil {
			t.Errorf("%s: %d %q", s.ime, w.Code, w.Body.String())
		}
	}
	if o.mreza.NetworkInfo() != nil {
		t.Fatal("mreža osnovana uz neispravan obrazac")
	}

	w := o.post(lokalno, obrazac())
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" || kolacicSesije(w) == nil {
		t.Fatalf("osnivanje: %d %q %v", w.Code, w.Body.String(), w.Header())
	}
	u, err := o.repo.GetUserByUsername("pperic")
	if err != nil || u == nil || !u.IsGlobalAdmin || !u.IsActive || u.MustChangePassword {
		t.Fatalf("prvi administrator: %+v %v", u, err)
	}
	if a, _ := o.repo.GetUserByUsername("admin"); a == nil || a.IsActive {
		t.Error("početni račun admin nije isključen")
	}
	if n := o.mreza.NetworkInfo(); n == nil || !n.CanAdmit || n.Name != "Hrvatske vode — COP Osijek" {
		t.Errorf("mreža: %+v", n)
	}
	if w := o.prijava(lokalno, "admin", db.ZadanaLozinka); kolacicSesije(w) != nil {
		t.Error("početni admin se i dalje prijavljuje")
	}

	// čvor više nije svjež: stranice nema, drugi pokušaj ništa ne mijenja
	if w := o.get(lokalno, ""); w.Code != http.StatusSeeOther {
		t.Errorf("nakon postavljanja stranica i dalje postoji: %d", w.Code)
	}
	if w := o.post(lokalno, obrazac("korisnik", "uljez")); w.Code != http.StatusSeeOther {
		t.Errorf("drugo postavljanje: %d", w.Code)
	}
	if u, _ := o.repo.GetUserByUsername("uljez"); u != nil {
		t.Error("drugo postavljanje napravilo je račun")
	}
}

// Pod Postavom javna početna lozinka vrijedi samo s ovog računala
func TestPocetnaLozinkaPodPostavomSamoLokalno(t *testing.T) {
	o := novaOkolinaPrijave(t)
	o.racun("admin", db.ZadanaLozinka, true, true)
	podPostavom := true
	o.authH.samoOvoRacunalo = func() bool { return podPostavom }

	if w := o.prijava(izravno, "admin", db.ZadanaLozinka); w.Code != http.StatusForbidden || kolacicSesije(w) != nil {
		t.Errorf("iz lokalne mreže pod Postavom: %d", w.Code)
	}
	if w := o.prijava(lokalno, "admin", db.ZadanaLozinka); kolacicSesije(w) == nil {
		t.Errorf("s ovog računala pod Postavom: %d %q", w.Code, w.Body.String())
	}
	// bez Postave (Unraid, ručno pokretanje) iz lokalne mreže kao dosad
	podPostavom = false
	ponovnaLozinka = newLoginLimiter()
	if w := o.prijava(izravno, "admin", db.ZadanaLozinka); kolacicSesije(w) == nil {
		t.Errorf("bez Postave iz lokalne mreže: %d %q", w.Code, w.Body.String())
	}
}

// primateljNaDaljinu je čvor ureda s ključem mreže, u svojoj bazi
func primateljNaDaljinu(t *testing.T) *peers.Service {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "ured.db")
	baza, err := db.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	cvor, err := peers.LoadNode(dbPath, "cop-osijek", "Ured", "test")
	if err != nil {
		t.Fatal(err)
	}
	ured, err := peers.NewService(baza, ledger.New(baza, "cop-osijek"), cvor, peers.Ports{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ured.CreateNetwork(context.Background(), "Hrvatske vode"); err != nil {
		t.Fatal(err)
	}
	return ured
}

func (o *okolinaPostavljanja) postPotvrdu(porijeklo func(*http.Request) *http.Request, datoteka []byte, polja url.Values) *httptest.ResponseRecorder {
	var r *http.Request
	if datoteka != nil {
		var tijelo bytes.Buffer
		mw := multipart.NewWriter(&tijelo)
		for k, v := range polja {
			_ = mw.WriteField(k, v[0])
		}
		fw, _ := mw.CreateFormFile("datoteka", "gocop-potvrda.json")
		_, _ = fw.Write(datoteka)
		_ = mw.Close()
		r = httptest.NewRequest(http.MethodPost, "/postavljanje/potvrda", &tijelo)
		r.Header.Set("Content-Type", mw.FormDataContentType())
	} else {
		r = httptest.NewRequest(http.MethodPost, "/postavljanje/potvrda", strings.NewReader(polja.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	w := httptest.NewRecorder()
	o.srv.ServeHTTP(w, porijeklo(r))
	return w
}

// Postojeća mreža na daljinu: zahtjev i potvrdu smije samo vlasnik, a
// potvrda se prihvaća samo ako odgovara kodu za primanje ovog računala
func TestPostavljanjeNaDaljinu(t *testing.T) {
	o := novaOkolinaPostavljanja(t)
	ured := primateljNaDaljinu(t)

	noviZahtjev := func(porijeklo func(*http.Request) *http.Request, v url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/postavljanje/zahtjev", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		o.srv.ServeHTTP(w, porijeklo(r))
		return w
	}
	datoteka := func(porijeklo func(*http.Request) *http.Request, upit string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		o.srv.ServeHTTP(w, porijeklo(httptest.NewRequest(http.MethodGet, "/postavljanje/zahtjev"+upit, nil)))
		return w
	}
	if w := noviZahtjev(izravno, url.Values{}); w.Code != http.StatusForbidden {
		t.Errorf("zahtjev iz mreže bez koda: %d", w.Code)
	}
	if w := noviZahtjev(javnoIzravno, url.Values{"kod": {"7KQ4-M2XD"}}); w.Code != http.StatusSeeOther {
		t.Errorf("zahtjev s javne adrese: %d", w.Code)
	}
	if w := noviZahtjev(krozTunel("198.51.100.7"), url.Values{"kod": {"7KQ4-M2XD"}}); w.Code != http.StatusSeeOther {
		t.Errorf("zahtjev kroz tunel: %d", w.Code)
	}
	if _, _, ok := o.mreza.ZahtjevNaCekanju(); ok {
		t.Fatal("odbijen zahtjev je napravljen")
	}
	// datoteke još nema: natrag na stranicu (uz kod iz dnevnika, ako je dan)
	if w := datoteka(lokalno, ""); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/postavljanje?put=postojeca" {
		t.Errorf("datoteka prije zahtjeva: %d %q", w.Code, w.Header().Get("Location"))
	}
	if w := datoteka(izravno, "?kod=7KQ4-M2XD"); w.Code != http.StatusSeeOther || !strings.HasSuffix(w.Header().Get("Location"), "&kod=7KQ4-M2XD") {
		t.Errorf("datoteka prije zahtjeva, s kodom: %d %q", w.Code, w.Header().Get("Location"))
	}
	if w := datoteka(krozTunel("198.51.100.7"), "?kod=7KQ4-M2XD"); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Errorf("datoteka kroz tunel: %d %q", w.Code, w.Header().Get("Location"))
	}
	if w := o.postPotvrdu(lokalno, nil, url.Values{}); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("potvrda bez datoteke: %d", w.Code)
	}
	if w := o.postPotvrdu(krozTunel("198.51.100.7"), nil, url.Values{"kod": {"7KQ4-M2XD"}}); w.Code != http.StatusSeeOther {
		t.Errorf("potvrda kroz tunel: %d", w.Code)
	}
	w := noviZahtjev(lokalno, url.Values{})
	_, kod, ok := o.mreza.ZahtjevNaCekanju()
	if w.Code != http.StatusSeeOther || !ok {
		t.Fatalf("zahtjev s ovog računala: %d %q", w.Code, w.Body.String())
	}
	if w := o.get(lokalno, "?put=postojeca"); !strings.Contains(w.Body.String(), "kod-primanja="+kod+"|") {
		t.Fatalf("stranica ne pokazuje kod zahtjeva: %q", w.Body.String())
	}
	if w := datoteka(izravno, ""); w.Code != http.StatusForbidden {
		t.Errorf("datoteka zahtjeva iz mreže bez koda: %d", w.Code)
	}
	w = datoteka(lokalno, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Disposition"), "gocop-zahtjev-pperic-thinkpad.json") {
		t.Fatalf("datoteka zahtjeva: %d %v", w.Code, w.Header())
	}
	zahtjev := w.Body.Bytes()

	potvrda, err := ured.PrimiZahtjev(context.Background(), zahtjev, kod, false)
	if err != nil {
		t.Fatal(err)
	}

	// tuđi ne smije učitati potvrdu
	if w := o.postPotvrdu(izravno, potvrda, url.Values{}); w.Code != http.StatusForbidden || o.mreza.NetworkInfo() != nil {
		t.Errorf("potvrda iz mreže bez koda: %d", w.Code)
	}
	// podmetnuta potvrda ne odgovara kodu
	var p map[string]any
	_ = json.Unmarshal(potvrda, &p)
	p["mreza"] = "Lažna mreža"
	losa, _ := json.Marshal(p)
	if w := o.postPotvrdu(lokalno, losa, url.Values{}); w.Code != http.StatusUnprocessableEntity || o.mreza.NetworkInfo() != nil {
		t.Errorf("podmetnuta potvrda: %d %q", w.Code, w.Body.String())
	}

	w = o.postPotvrdu(lokalno, potvrda, url.Values{})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "u-mrezi=Hrvatske vode") {
		t.Fatalf("potvrda: %d %q", w.Code, w.Body.String())
	}
	if n := o.mreza.NetworkInfo(); n == nil || n.Name != "Hrvatske vode" {
		t.Fatalf("mreža nakon potvrde: %+v", n)
	}
	if w := o.get(lokalno, ""); !strings.Contains(w.Body.String(), "u-mrezi=Hrvatske vode") || strings.Contains(w.Body.String(), "kod-primanja="+kod) {
		t.Errorf("stranica nakon primanja: %q", w.Body.String())
	}
	if w := noviZahtjev(lokalno, url.Values{}); w.Code != http.StatusConflict {
		t.Errorf("zahtjev čvora koji je već u mreži: %d", w.Code)
	}
}
