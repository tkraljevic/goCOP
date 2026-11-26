package web

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/propisi"
	"gocop/internal/service"
)

// Učitavanje propisa kroz rute radi samo na izmišljenim tekstovima i
// izmišljenim stranicama u obliku Narodnih novina; pravi poslužitelj NN se
// nikad ne pita (Odluke (25. 11. 2026.)).

// lazniDohvatRute vraća izmišljene stranice akata
type lazniDohvatRute map[models.IzdanjeNN]string

func (l lazniDohvatRute) Akt(_ context.Context, i models.IzdanjeNN) ([]byte, error) {
	s, ok := l[i]
	if !ok {
		return nil, propisi.ErrNemaAkta
	}
	return []byte(s), nil
}

// stranicaPlanaRute je izmišljena stranica plana s dvije točke
const stranicaPlanaRute = `<!-- izmišljen tekst, nije propis -->
<html><body>
<div id="sticky" class="title"><h2>Plan čuvanja Oglednice</h2></div>
<div class="article-column">
<p class="T-11-9-sred">STADIJI</p>
<p class="Clanak">I.</p>
<p class="T-9-8">Pripremno stanje proglašava se kad voda poraste.</p>
<p class="Clanak">II.</p>
<p class="T-9-8">Redovna obrana slijedi pripremno stanje.</p>
</div>
<table class="detailsTable"><tr><td><span class='key'>Donositelj:</span>Vlada Primjerova</td></tr></table>
</body></html>`

// tekstPravilnikaRute je izmišljen pravilnik za zalijepljeni tekst
const tekstPravilnikaRute = "Članak 1.\nČuvar nasipa nosi čizme.\nČlanak 2.\nOvaj Pravilnik stupa na snagu 1. siječnja 2027."

// posalji šalje obrazac kao s iste stranice; sesija "" je bez prijave
func (o *okolinaPropisa) posalji(put, sesija string, forma url.Values) *httptest.ResponseRecorder {
	o.t.Helper()
	r := izravno(httptest.NewRequest(http.MethodPost, put, strings.NewReader(forma.Encode())))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return o.posaljiZahtjev(r, sesija, "same-origin")
}

// posaljiDatoteku šalje obrazac s datotekom .txt (multipart)
func (o *okolinaPropisa) posaljiDatoteku(put, sesija string, polja map[string]string, datoteka []byte) *httptest.ResponseRecorder {
	o.t.Helper()
	var tijelo bytes.Buffer
	mw := multipart.NewWriter(&tijelo)
	for k, v := range polja {
		if err := mw.WriteField(k, v); err != nil {
			o.t.Fatal(err)
		}
	}
	fw, err := mw.CreateFormFile("datoteka", "propis.txt")
	if err == nil {
		_, err = fw.Write(datoteka)
	}
	if err == nil {
		err = mw.Close()
	}
	if err != nil {
		o.t.Fatal(err)
	}
	r := izravno(httptest.NewRequest(http.MethodPost, put, &tijelo))
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return o.posaljiZahtjev(r, sesija, "same-origin")
}

func (o *okolinaPropisa) posaljiZahtjev(r *http.Request, sesija, odakle string) *httptest.ResponseRecorder {
	o.t.Helper()
	r.Header.Set("Accept", "text/html")
	r.Header.Set("Sec-Fetch-Site", odakle)
	if sesija != "" {
		r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: sesija})
	}
	w := httptest.NewRecorder()
	o.srv.ServeHTTP(w, r)
	return w
}

// drugiAdmin je sesija drugog globalnog administratora
func (o *okolinaPropisa) drugiAdmin() string {
	o.t.Helper()
	o.prijava.osoba("mmaric", "marina-lozinka", "mmaric@example.com", true)
	ses, _, err := o.prijava.auth.Login("mmaric", "marina-lozinka", "192.168.1.50", "test")
	if err != nil {
		o.t.Fatal(err)
	}
	return ses.ID.String()
}

// mjesto je adresa preusmjerenja bez upita
func mjesto(t *testing.T, w *httptest.ResponseRecorder) (string, url.Values) {
	t.Helper()
	if w.Code != http.StatusSeeOther {
		t.Fatalf("očekivano preusmjerenje, a dobiveno %d %s", w.Code, w.Body.String())
	}
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return u.Path, u.Query()
}

// adminRutePropisa su sve rute za upis propisa, s primjerom adrese
var adminRutePropisa = []struct{ metoda, put string }{
	{http.MethodGet, "/propisi/ucitaj"},
	{http.MethodPost, "/propisi/ucitaj"},
	{http.MethodGet, "/propisi/ucitaj/provjera?p=x"},
	{http.MethodPost, "/propisi/ucitaj/spremi"},
	{http.MethodGet, "/propisi/zakon-o-cuvanju-nasipa/dodaj-izmjenu"},
	{http.MethodPost, "/propisi/zakon-o-cuvanju-nasipa/dodaj-izmjenu"},
	{http.MethodGet, "/propisi/zakon-o-cuvanju-nasipa/dodaj-izmjenu/provjera?p=x"},
	{http.MethodPost, "/propisi/zakon-o-cuvanju-nasipa/dodaj-izmjenu/spremi"},
	{http.MethodPost, "/propisi/zakon-o-cuvanju-nasipa/ukloni"},
	{http.MethodPost, "/propisi/zakon-o-cuvanju-nasipa/izmjene/nn-2026-47-808/ukloni"},
}

// Učitava, dopunjuje i uklanja propise samo globalni administrator: drugi
// dobiju odbijenicu i na ruti, a izmjena s tuđe stranice se odbija
// (Odluke (25. 11. 2026.))
func TestUcitavanjeSamoAdministrator(t *testing.T) {
	o := novaOkolinaPropisa(t)
	o.napuni()
	forma := url.Values{"razlog": {"krivo"}, "broj_nn": {"12/2026"}, "broj_akta": {"201"}}
	for _, ruta := range adminRutePropisa {
		var w *httptest.ResponseRecorder
		if ruta.metoda == http.MethodGet {
			w = o.get(ruta.put, o.vodocuvar)
		} else {
			w = o.posalji(ruta.put, o.vodocuvar, forma)
		}
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), porukaSamoAdmin) {
			t.Errorf("vodočuvar %s %s: %d", ruta.metoda, ruta.put, w.Code)
		}
	}
	r := izravno(httptest.NewRequest(http.MethodPost, "/propisi/zakon-o-cuvanju-nasipa/ukloni", strings.NewReader("razlog=krivo")))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if w := o.posaljiZahtjev(r, o.admin, "cross-site"); w.Code != http.StatusForbidden {
		t.Errorf("uklanjanje s tuđe stranice: %d", w.Code)
	}
	if p, err := o.repo.Get(context.Background(), nnZakonaRute.ID()); err != nil || p == nil {
		t.Fatalf("propis uklonjen bez prava: %v", err)
	}
	if w := o.get("/propisi/ucitaj", o.admin); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Dohvati i podijeli") {
		t.Errorf("administrator: %d", w.Code)
	}
}

// Zalijepljen tekst i stranica iz (lažnih) Narodnih novina idu istim putem:
// obrazac, provjera podjele s ključem pregleda, spremanje. Nakon spremanja
// pregled više ne postoji.
func TestUcitajProvjeriSpremiKrozRute(t *testing.T) {
	o := novaOkolinaPropisa(t)
	o.server.SetPropisi(service.NewPropisiService(o.repo, lazniDohvatRute{nnPlanaRute: stranicaPlanaRute}))

	w := o.posalji("/propisi/ucitaj", o.admin, url.Values{"nacin": {"tekst"}, "broj_nn": {"3/2026"}, "broj_akta": {"30"},
		"naziv": {"Pravilnik o čizmama"}, "tekst": {tekstPravilnikaRute}, "skupina": {"nova"}, "nova_skupina": {"Oprema"}})
	put, upit := mjesto(t, w)
	if put != "/propisi/ucitaj/provjera" || upit.Get("p") == "" {
		t.Fatalf("nakon čitanja: %s %v", put, upit)
	}
	kljuc := upit.Get("p")
	provjera := o.get("/propisi/ucitaj/provjera?p="+kljuc, o.admin)
	for _, ima := range []string{"Tekst je pročitan", "Prepoznato: 2 članka (1.–2.)", `value="pravilnik-o-cizmama"`, "Nova skupina…</option>",
		`value="Oprema"`, "Narodne novine za ovaj akt nemaju podatke o izmjenama.", "Čuvar nasipa nosi čizme."} {
		if !strings.Contains(provjera.Body.String(), ima) {
			t.Errorf("provjera nema %q", ima)
		}
	}
	put, upit = mjesto(t, o.posalji("/propisi/ucitaj/spremi", o.admin, url.Values{"p": {kljuc}, "adresa": {"Kriva Adresa"}, "skupina": {"Ostali"}}))
	if put != "/propisi/ucitaj/provjera" || upit.Get("p") != kljuc || !strings.Contains(upit.Get("error"), "Stalna adresa smije imati samo") {
		t.Fatalf("kriva adresa: %s %v", put, upit)
	}
	put, upit = mjesto(t, o.posalji("/propisi/ucitaj/spremi", o.admin, url.Values{"p": {kljuc}, "adresa": {"pravilnik-o-cizmama"}, "skupina": {"nova"}, "nova_skupina": {"Oprema"}}))
	if put != "/propisi/pravilnik-o-cizmama" || upit.Get("success") != "Propis „Pravilnik o čizmama” je spremljen i stiže na sva računala u mreži." {
		t.Fatalf("spremanje: %s %v", put, upit)
	}
	p, err := o.repo.PoAdresi(context.Background(), "pravilnik-o-cizmama")
	if err != nil || p == nil || p.Skupina != "Oprema" || p.Izvor != models.IzvorPropisaTekst || p.Ucitao != "pperic" {
		t.Fatalf("spremljen: %+v %v", p, err)
	}
	if w := o.get("/propisi/pravilnik-o-cizmama?success=Spremljeno", o.admin); !strings.Contains(w.Body.String(), "Spremljeno") {
		t.Error("stranica propisa ne pokazuje poruku o uspjehu")
	}
	put, upit = mjesto(t, o.get("/propisi/ucitaj/provjera?p="+kljuc, o.admin))
	if put != "/propisi/ucitaj" || upit.Get("error") != porukaPregledIstekao {
		t.Errorf("pregled nakon spremanja: %s %v", put, upit)
	}

	// isti put s lažnim dohvatom
	put, upit = mjesto(t, o.posalji("/propisi/ucitaj", o.admin, url.Values{"nacin": {"nn"}, "poveznica": {"https://narodne-novine.nn.hr/eli/sluzbeni/2026/5/101"}, "skupina": {"Vodočuvari"}}))
	if put != "/propisi/ucitaj/provjera" {
		t.Fatalf("dohvat: %s", put)
	}
	provjera = o.get("/propisi/ucitaj/provjera?p="+upit.Get("p"), o.admin)
	for _, ima := range []string{"Akt je dohvaćen", "Plan čuvanja Oglednice", "Vlada Primjerova", "(I.–II.)", `<option value="Vodočuvari" selected>`} {
		if !strings.Contains(provjera.Body.String(), ima) {
			t.Errorf("provjera dohvata nema %q", ima)
		}
	}
	put, _ = mjesto(t, o.posalji("/propisi/ucitaj/spremi", o.admin, url.Values{"p": {upit.Get("p")}, "skupina": {"Vodočuvari"}}))
	if put != "/propisi/plan-cuvanja-oglednice" {
		t.Errorf("plan spremljen na %s", put)
	}
	// ponovno učitavanje istog akta kaže da je već učitan
	_, upit = mjesto(t, o.posalji("/propisi/ucitaj", o.admin, url.Values{"nacin": {"nn"}, "broj_nn": {"5/2026"}, "broj_akta": {"101"}}))
	if w := o.get("/propisi/ucitaj/provjera?p="+upit.Get("p"), o.admin); !strings.Contains(w.Body.String(), "Ovaj propis je već učitan") ||
		!strings.Contains(w.Body.String(), "/propisi/plan-cuvanja-oglednice</a>") {
		t.Error("provjera ne kaže da je propis već učitan")
	}
}

// Kad čitanje ne uspije, obrazac se vraća popunjen, s rečenicom zašto, i
// ništa se ne sprema; datoteka .txt mora biti UTF-8
func TestUcitavanjeGreskaVracaObrazac(t *testing.T) {
	o := novaOkolinaPropisa(t)
	o.server.SetPropisi(service.NewPropisiService(o.repo, lazniDohvatRute{}))
	w := o.posalji("/propisi/ucitaj", o.admin, url.Values{"nacin": {"nn"}, "broj_nn": {"84-2010"}, "broj_akta": {"2389"}})
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "Broj NN upišite kao 84/2010") || !strings.Contains(w.Body.String(), `value="84-2010"`) {
		t.Errorf("krivi broj: %d %s", w.Code, w.Body.String())
	}
	w = o.posalji("/propisi/ucitaj", o.admin, url.Values{"nacin": {"nn"}, "broj_nn": {"12/2026"}, "broj_akta": {"201"}})
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "Narodne novine nemaju akt 201 u NN 12/2026.") {
		t.Errorf("akta nema: %d", w.Code)
	}
	polja := map[string]string{"nacin": "nn", "broj_nn": "3/2026", "broj_akta": "30", "naziv": "Pravilnik o čizmama"}
	if w := o.posaljiDatoteku("/propisi/ucitaj", o.admin, polja, []byte("\x9aum\xe8a")); w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "UTF-8") {
		t.Errorf("datoteka u drugoj kodnoj stranici: %d", w.Code)
	}
	put, _ := mjesto(t, o.posaljiDatoteku("/propisi/ucitaj", o.admin, polja, []byte(tekstPravilnikaRute)))
	if put != "/propisi/ucitaj/provjera" {
		t.Errorf("datoteka .txt: %s", put)
	}
	skupine, err := o.svc.Popis(context.Background())
	if err != nil || len(skupine) != 0 {
		t.Errorf("nešto je spremljeno: %v %v", skupine, err)
	}
}

// Pregled je vezan uz osobu i propis: drugi administrator s istim ključem
// i nepoznat ili istekao ključ dobiju rečenicu da je pregled istekao
func TestTudjiIIstekaoPregled(t *testing.T) {
	o := novaOkolinaPropisa(t)
	o.napuni()
	mara := o.drugiAdmin()
	_, upit := mjesto(t, o.posalji("/propisi/ucitaj", o.admin, url.Values{"nacin": {"tekst"}, "broj_nn": {"3/2026"}, "broj_akta": {"30"},
		"naziv": {"Pravilnik"}, "tekst": {tekstPravilnikaRute}}))
	kljuc := upit.Get("p")
	for _, w := range []*httptest.ResponseRecorder{
		o.get("/propisi/ucitaj/provjera?p="+kljuc, mara),
		o.posalji("/propisi/ucitaj/spremi", mara, url.Values{"p": {kljuc}}),
		o.get("/propisi/ucitaj/provjera?p=nepoznat", o.admin),
	} {
		if put, upit := mjesto(t, w); put != "/propisi/ucitaj" || upit.Get("error") != porukaPregledIstekao {
			t.Errorf("%s %v", put, upit)
		}
	}
	_, upit = mjesto(t, o.posalji("/propisi/zakon-o-cuvanju-nasipa/dodaj-izmjenu", o.admin, url.Values{"nacin": {"tekst"}, "broj_nn": {"9/2027"}, "broj_akta": {"77"},
		"naziv": {"Izmjena"}, "tekst": {"Članak 1.\nU članku 2. riječ »nasip« briše se."}}))
	kljuc = upit.Get("p")
	for _, w := range []*httptest.ResponseRecorder{
		o.get("/propisi/zakon-o-cuvanju-nasipa/dodaj-izmjenu/provjera?p="+kljuc, mara),
		o.get("/propisi/plan-obrane-primjerova/dodaj-izmjenu/provjera?p="+kljuc, o.admin),
		o.posalji("/propisi/plan-obrane-primjerova/dodaj-izmjenu/spremi", o.admin, url.Values{"p": {kljuc}}),
	} {
		if _, upit := mjesto(t, w); upit.Get("error") != porukaPregledIzmjene {
			t.Errorf("tuđi pregled izmjene: %v", upit)
		}
	}
	if w := o.get("/propisi/"+nnZakonaRute.ID()+"/dodaj-izmjenu/provjera?p="+kljuc, o.admin); w.Code != http.StatusOK {
		t.Errorf("pregled po oznaci propisa: %d", w.Code)
	}
	// propis uklonjen dok je pregled izmjene čekao
	if err := o.repo.Ukloni(context.Background(), nnZakonaRute.ID(), models.Uklanjanje{Razlog: "krivo"}); err != nil {
		t.Fatal(err)
	}
	if w := o.posalji("/propisi/zakon-o-cuvanju-nasipa/dodaj-izmjenu/spremi", o.admin, url.Values{"p": {kljuc}}); w.Code != http.StatusNotFound {
		t.Errorf("izmjena uklonjenog propisa: %d", w.Code)
	}
}

// Dodana izmjena s odredbom koju je administrator sam povezao daje oznaku
// uz taj članak; kriv članak se ne sprema (Odluke (25. 11. 2026.), O10)
func TestDodajIzmjenuOznakaUzClanak(t *testing.T) {
	o := novaOkolinaPropisa(t)
	o.napuni()
	obrazac := o.get("/propisi/zakon-o-cuvanju-nasipa/dodaj-izmjenu", o.admin)
	for _, ima := range []string{"Dodaj izmjenu", "Zakon o čuvanju nasipa · NN 12/2026", "NN 9/2027 (akt 77)", `name="broj_nn" value="9/2027"`} {
		if !strings.Contains(obrazac.Body.String(), ima) {
			t.Errorf("obrazac izmjene nema %q", ima)
		}
	}
	tekst := "Članak 1.\nU Zakonu o čuvanju nasipa (»Narodne novine«, broj 12/26) u članku 2. riječ »vodočuvar« zamjenjuje se riječju »čuvar«.\n" +
		"Članak 2.\nČlanak 300. mijenja se i glasi:\n»Članak 300.\nNovi tekst.«"
	_, upit := mjesto(t, o.posalji("/propisi/zakon-o-cuvanju-nasipa/dodaj-izmjenu", o.admin, url.Values{"nacin": {"tekst"}, "broj_nn": {"9/2027"}, "broj_akta": {"77"},
		"naziv": {"Zakon o drugoj izmjeni"}, "tekst": {tekst}}))
	kljuc := upit.Get("p")
	provjera := o.get("/propisi/zakon-o-cuvanju-nasipa/dodaj-izmjenu/provjera?p="+kljuc, o.admin).Body.String()
	for _, ima := range []string{"Tekst izmjene je pročitan", "nije prepoznato", `name="cilj-2"`, "Izaberite članak", `<option value="clanak-1">Članak 1.</option>`,
		"Prepoznato 1 od 2 odredbi. 1 odredbu povežite sami.", "Članka 300. nema u propisu."} {
		if !strings.Contains(provjera, ima) {
			t.Errorf("provjera izmjene nema %q", ima)
		}
	}
	put, upit := mjesto(t, o.posalji("/propisi/zakon-o-cuvanju-nasipa/dodaj-izmjenu/spremi", o.admin, url.Values{"p": {kljuc}, "cilj-2": {"clanak-999"}}))
	if put != "/propisi/zakon-o-cuvanju-nasipa/dodaj-izmjenu/provjera" || !strings.Contains(upit.Get("error"), "odabrani članak nije u propisu") {
		t.Errorf("kriv članak: %s %v", put, upit)
	}
	put, upit = mjesto(t, o.posalji("/propisi/zakon-o-cuvanju-nasipa/dodaj-izmjenu/spremi", o.admin, url.Values{"p": {kljuc}, "cilj-2": {"clanak-1"}, "cilj-x": {"clanak-2"}}))
	if put != "/propisi/zakon-o-cuvanju-nasipa" || upit.Get("success") != "Izmjena NN 9/2027 je spremljena. Oznaka stoji uz 2 članka." {
		t.Fatalf("spremanje: %s %v", put, upit)
	}
	clanak := o.get("/propisi/zakon-o-cuvanju-nasipa/clanak-1", o.vodocuvar).Body.String()
	if !strings.Contains(clanak, "mijenjan NN 9/27") {
		t.Error("uz članak 1. nema oznake ručno povezane izmjene")
	}
	if _, upit := mjesto(t, o.posalji("/propisi/zakon-o-cuvanju-nasipa/dodaj-izmjenu/spremi", o.admin, url.Values{"p": {kljuc}})); upit.Get("error") != porukaPregledIzmjene {
		t.Errorf("pregled nakon spremanja: %v", upit)
	}
	// izmjena propisa kojeg nema
	if w := o.posalji("/propisi/nema-ga/dodaj-izmjenu", o.admin, url.Values{"nacin": {"tekst"}, "broj_nn": {"9/2027"}, "broj_akta": {"77"}, "naziv": {"I"}, "tekst": {"Članak 1."}}); w.Code != http.StatusNotFound {
		t.Errorf("izmjena propisa kojeg nema: %d", w.Code)
	}
	if w := o.get("/propisi/nema-ga/dodaj-izmjenu", o.admin); w.Code != http.StatusNotFound {
		t.Errorf("obrazac izmjene propisa kojeg nema: %d", w.Code)
	}
	if w := o.posalji("/propisi/zakon-o-cuvanju-nasipa/dodaj-izmjenu", o.admin, url.Values{"nacin": {"tekst"}, "broj_nn": {"9/2027"}, "broj_akta": {"77"}}); w.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(w.Body.String(), "Zalijepite tekst akta") || !strings.Contains(w.Body.String(), "Dodaj izmjenu") {
		t.Errorf("izmjena bez teksta: %d", w.Code)
	}
}

// Uklanjanje traži razlog; uklonjena izmjena i propis nestaju, a propis
// kojeg nema daje stranicu „nema”
func TestUkloniPropisIIzmjenu(t *testing.T) {
	o := novaOkolinaPropisa(t)
	o.napuni()
	izmjena := "/propisi/zakon-o-cuvanju-nasipa/izmjene/nn-2026-47-808"
	put, upit := mjesto(t, o.posalji(izmjena+"/ukloni", o.admin, url.Values{"razlog": {" "}}))
	if put != izmjena || upit.Get("error") != string(service.ErrBezRazloga) {
		t.Errorf("bez razloga: %s %v", put, upit)
	}
	put, upit = mjesto(t, o.posalji(izmjena+"/ukloni", o.admin, url.Values{"razlog": {"dvaput učitana"}}))
	if put != "/propisi/zakon-o-cuvanju-nasipa" || upit.Get("success") != "Izmjena NN 47/2026 je uklonjena." {
		t.Errorf("izmjena: %s %v", put, upit)
	}
	if w := o.get(izmjena, o.vodocuvar); w.Code != http.StatusNotFound {
		t.Errorf("uklonjena izmjena: %d", w.Code)
	}
	if w := o.posalji(izmjena+"/ukloni", o.admin, url.Values{"razlog": {"opet"}}); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "nema izmjene „nn-2026-47-808”") {
		t.Errorf("izmjena koje nema: %d", w.Code)
	}
	if _, upit = mjesto(t, o.posalji("/propisi/zakon-o-cuvanju-nasipa/ukloni", o.admin, nil)); upit.Get("error") != string(service.ErrBezRazloga) {
		t.Errorf("propis bez razloga: %v", upit)
	}
	put, upit = mjesto(t, o.posalji("/propisi/zakon-o-cuvanju-nasipa/ukloni", o.admin, url.Values{"razlog": {"krivo učitan"}}))
	if put != "/propisi" || upit.Get("success") != "Propis „Zakon o čuvanju nasipa” je uklonjen. U povijesti zapisa ostaje." {
		t.Errorf("propis: %s %v", put, upit)
	}
	if w := o.get("/propisi/zakon-o-cuvanju-nasipa", o.vodocuvar); w.Code != http.StatusNotFound {
		t.Errorf("uklonjen propis: %d", w.Code)
	}
	if w := o.posalji("/propisi/zakon-o-cuvanju-nasipa/ukloni", o.admin, url.Values{"razlog": {"opet"}}); w.Code != http.StatusNotFound {
		t.Errorf("propis kojeg nema: %d", w.Code)
	}
}

// Dva akta istog naziva učitana na dva računala prije razmjene imaju istu
// adresu. Stranica mlađeg vodi obrasce po oznaci zapisa, pa „Ukloni propis”
// i „Dodaj izmjenu” pogađaju njega, a ne stariji akt.
func TestIstaAdresaDvaAktaUklanjaPraviPropis(t *testing.T) {
	o := novaOkolinaPropisa(t)
	ctx := context.Background()
	stariji, mladji := zakonZaRute(), zakonZaRute()
	mladji.ID, mladji.NN = "nn-2027-9-77", models.IzdanjeNN{Godina: 2027, Broj: 9, Akt: 77}
	stariji.CreatedAt = time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	mladji.CreatedAt = time.Date(2026, 5, 2, 8, 0, 0, 0, time.UTC)
	for _, p := range []*models.Propis{mladji, stariji} {
		if err := o.repo.Spremi(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	popis := o.get("/propisi", o.vodocuvar).Body.String()
	if !strings.Contains(popis, `href="/propisi/zakon-o-cuvanju-nasipa"`) || !strings.Contains(popis, `href="/propisi/nn-2027-9-77"`) {
		t.Errorf("popis ne vodi na oba propisa")
	}
	stranica := o.get("/propisi/nn-2027-9-77", o.admin).Body.String()
	for _, z := range []string{`action="/propisi/nn-2027-9-77/ukloni"`, `href="/propisi/nn-2027-9-77/dodaj-izmjenu"`} {
		if !strings.Contains(stranica, z) {
			t.Errorf("stranica mlađeg nema %s", z)
		}
	}
	put, _ := mjesto(t, o.posalji("/propisi/nn-2027-9-77/ukloni", o.admin, url.Values{"razlog": {"krivo učitan"}}))
	if put != "/propisi" {
		t.Errorf("uklanjanje: %s", put)
	}
	if x, err := o.repo.Get(ctx, mladji.ID); err != nil || x != nil {
		t.Errorf("mlađi nije uklonjen: %+v %v", x, err)
	}
	if x, err := o.repo.Get(ctx, stariji.ID); err != nil || x == nil {
		t.Errorf("uklonjen je stariji propis: %v", err)
	}
}

// Gumbe za učitavanje, dodavanje i uklanjanje vidi samo globalni
// administrator; vodočuvar čita isti propis bez njih
func TestGumbiSamoAdministratoru(t *testing.T) {
	o := novaOkolinaPropisa(t)
	o.napuni()
	stranice := map[string][]string{
		"/propisi":                        {"Učitaj iz Narodnih novina"},
		"/propisi/zakon-o-cuvanju-nasipa": {"Dodaj izmjenu", "Ukloni propis", `action="/propisi/zakon-o-cuvanju-nasipa/ukloni"`},
		"/propisi/zakon-o-cuvanju-nasipa/izmjene/nn-2026-47-808": {"Ukloni izmjenu", "Dohvati ponovno i poveži odredbe"},
	}
	for put, gumbi := range stranice {
		admin, vodocuvar := o.get(put, o.admin).Body.String(), o.get(put, o.vodocuvar).Body.String()
		for _, g := range gumbi {
			if !strings.Contains(admin, g) {
				t.Errorf("administrator na %s ne vidi %q", put, g)
			}
			if strings.Contains(vodocuvar, g) {
				t.Errorf("vodočuvar na %s vidi %q", put, g)
			}
		}
	}
	if w := o.get("/propisi/zakon-o-cuvanju-nasipa/clanak-1", o.admin); strings.Contains(w.Body.String(), "Ukloni propis") {
		t.Error("gumb za uklanjanje stoji i na stranici članka")
	}
}

// zahtjevAdminaPropisa je zahtjev s prijavljenim globalnim administratorom u
// kontekstu, za rukovatelje bez poslužitelja
func zahtjevAdminaPropisa(metoda, put string, forma url.Values, putanja map[string]string) *http.Request {
	r := httptest.NewRequest(metoda, put, strings.NewReader(forma.Encode()))
	if forma != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range putanja {
		r.SetPathValue(k, v)
	}
	ctx := context.WithValue(r.Context(), contextKeyUser, &models.User{Username: "pperic", FullName: "Pero Perić"})
	ctx = context.WithValue(ctx, contextKeyPerms, &models.UserPermissions{IsGlobalAdmin: true})
	return r.WithContext(ctx)
}

// Bez servisa svaki rukovatelj učitavanja kaže da propisi još nisu spremni,
// a kvar baze daje rečenicu, ne pola stranice
func TestUcitavanjeBezServisaIKvar(t *testing.T) {
	o := novaOkolinaPropisa(t)
	o.napuni()
	bez := NewPropisiUcitajHandler(NewPropisiHandler(func() *service.PropisiService { return nil }, nil, nil, nil), nil, nil, nil)
	for ime, f := range map[string]http.HandlerFunc{"obrazac": bez.ObrazacUcitaj, "učitaj": bez.Ucitaj, "provjera": bez.Provjera, "spremi": bez.SpremiPropis,
		"obrazac izmjene": bez.ObrazacIzmjene, "učitaj izmjenu": bez.UcitajIzmjenu, "provjera izmjene": bez.ProvjeraIzmjene, "spremi izmjenu": bez.SpremiIzmjenu,
		"ukloni": bez.UkloniPropis, "ukloni izmjenu": bez.UkloniIzmjenu} {
		w := httptest.NewRecorder()
		f(w, zahtjevAdminaPropisa(http.MethodPost, "/propisi/x", url.Values{}, nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s bez servisa: %d", ime, w.Code)
		}
	}

	citanje := NewPropisiHandler(func() *service.PropisiService { return o.svc }, o.server.templates["propisi.html"], o.server.templates["propis.html"], o.server.templates["propis_izmjena.html"])
	h := NewPropisiUcitajHandler(citanje, o.server.templates["propisi_ucitaj.html"], o.server.templates["propisi_provjera.html"], o.server.templates["propisi_provjera_izmjene.html"])
	zakon := zakonZaRute()
	kljucIzmjene := h.izmjene.stavi(uuid.Nil.String(), &service.PregledIzmjene{Propis: zakon, Izmjena: *izmjenaZaRute(zakon)})
	kljucPropisa := h.propisi.stavi(uuid.Nil.String(), &service.PregledPropisa{Propis: *planZaRute()})
	o.zatvori()
	putanja := map[string]string{"propis": "zakon-o-cuvanju-nasipa", "izmjena": "nn-2026-47-808"}
	for ime, x := range map[string]struct {
		f     http.HandlerFunc
		forma url.Values
	}{
		"spremi":         {h.SpremiPropis, url.Values{"p": {kljucPropisa}}},
		"spremi izmjenu": {h.SpremiIzmjenu, url.Values{"p": {kljucIzmjene}}},
		"ukloni":         {h.UkloniPropis, url.Values{"razlog": {"krivo"}}},
		"ukloni izmjenu": {h.UkloniIzmjenu, url.Values{"razlog": {"krivo"}}},
	} {
		w := httptest.NewRecorder()
		x.f(w, zahtjevAdminaPropisa(http.MethodPost, "/propisi/x", x.forma, putanja))
		if _, upit := mjesto(t, w); upit.Get("error") != porukaPropisNeSprema {
			t.Errorf("%s uz zatvorenu bazu: %v", ime, upit)
		}
	}
	for ime, f := range map[string]http.HandlerFunc{"obrazac izmjene": h.ObrazacIzmjene, "učitaj izmjenu": h.UcitajIzmjenu} {
		w := httptest.NewRecorder()
		f(w, zahtjevAdminaPropisa(http.MethodPost, "/propisi/x", url.Values{"nacin": {"tekst"}, "broj_nn": {"9/2027"}, "broj_akta": {"77"}, "naziv": {"I"}, "tekst": {"Članak 1."}}, putanja))
		if w.Code != http.StatusInternalServerError {
			t.Errorf("%s uz zatvorenu bazu: %d", ime, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.Ucitaj(w, zahtjevAdminaPropisa(http.MethodPost, "/propisi/ucitaj", url.Values{"nacin": {"tekst"}, "broj_nn": {"9/2027"}, "broj_akta": {"77"}, "naziv": {"I"}, "tekst": {"Članak 1."}}, nil))
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), porukaPropisNeSprema) {
		t.Errorf("učitavanje uz zatvorenu bazu: %d", w.Code)
	}
}

// Obrazac s pokvarenim tijelom ili prevelikom datotekom daje rečenicu, a
// poruka o spremljenoj izmjeni broji članke ili dijelove
func TestObrazacPropisaIPorukaIzmjene(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/propisi/ucitaj", strings.NewReader("--x\r\nloše"))
	r.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	if _, err := procitajObrazacPropisa(r); err == nil || err.Error() != porukaDatotekaNeCitaSe {
		t.Errorf("pokvaren obrazac: %v", err)
	}
	var tijelo bytes.Buffer
	mw := multipart.NewWriter(&tijelo)
	fw, err := mw.CreateFormFile("datoteka", "velik.txt")
	if err == nil {
		_, err = fw.Write(bytes.Repeat([]byte("a"), najveciTekstPropisa+1))
	}
	if err == nil {
		err = mw.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest(http.MethodPost, "/propisi/ucitaj", &tijelo)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	if _, err := procitajObrazacPropisa(r); err == nil || err.Error() != porukaDatotekaVelika {
		t.Errorf("prevelika datoteka: %v", err)
	}

	nn := models.IzdanjeNN{Godina: 2026, Broj: 5, Akt: 101}
	for opis, x := range map[string]struct {
		upute []models.UputaIzmjene
		sto   string
	}{
		"bez cilja": {[]models.UputaIzmjene{{Vrsta: models.UputaStupanje}}, "Izmjena NN 5/2026 je spremljena."},
		"točke":     {[]models.UputaIzmjene{{Vrsta: models.UputaNoviTekst, Cilj: models.CiljUpute{Clanci: []string{"xxiii"}}}}, "Izmjena NN 5/2026 je spremljena. Oznaka stoji uz 1 dio."},
		"članci": {[]models.UputaIzmjene{{Vrsta: models.UputaNoviTekst, Cilj: models.CiljUpute{Clanci: []string{"clanak-1", "clanak-2"}}},
			{Vrsta: models.UputaZamjenaRijeci, Cilj: models.CiljUpute{Clanci: []string{"clanak-1"}}}}, "Izmjena NN 5/2026 je spremljena. Oznaka stoji uz 2 članka."},
	} {
		if got := porukaSpremljeneIzmjene(&models.IzmjenaPropisa{NN: nn, Upute: x.upute}); got != x.sto {
			t.Errorf("%s: %q", opis, got)
		}
	}
	if got := ciljeviIzObrasca(url.Values{"cilj-3": {"clanak-1"}, "cilj-": {"x"}, "cilj-4": {}, "drugo": {"y"}}); len(got) != 1 || got[3] != "clanak-1" {
		t.Errorf("ciljevi: %v", got)
	}
	if korisnikPregleda(PropisiPageData{}) != "" {
		t.Error("pregled bez osobe")
	}
}

// Svaka ruta propisa koja nešto mijenja ili učitava registrirana je kroz
// samoAdmin, a ne kroz opći authMiddleware; ovo hvata rutu koja se doda
// poslije i zaboravi
func TestUpisneRutePropisaIduKrozSamoAdmin(t *testing.T) {
	b, err := os.ReadFile("propisi_rute.go")
	if err != nil {
		t.Fatal(err)
	}
	nadjeno := 0
	for _, redak := range strings.Split(string(b), "\n") {
		if !strings.Contains(redak, "s.mux.Handle(") {
			continue
		}
		upis := strings.Contains(redak, `"POST `) || strings.Contains(redak, "/ucitaj") || strings.Contains(redak, "/dodaj-izmjenu")
		if upis {
			nadjeno++
		}
		if upis != strings.Contains(redak, "s.samoAdmin(") {
			t.Errorf("ograda ne odgovara ruti: %s", strings.TrimSpace(redak))
		}
	}
	if nadjeno != len(adminRutePropisa) {
		t.Errorf("upisnih ruta %d, a u testu %d", nadjeno, len(adminRutePropisa))
	}
}
