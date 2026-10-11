package web

import (
	"context"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Odluke (8. 10. 2026.), pitanje 7, kroz rute: dva predana lista istog
// vodočuvara za isti dan (nastala prije jedinstvene oznake lista) ostaju
// oba, svaki s oznakom „dvostruki list – vidi i list br. …”, a
// rukovoditeljica koja ih ovjerava dobiva popis dvostrukih listova.
// Vodočuvar popis ne dobiva, ali oznaku na svojim listovima vidi.
func TestDvostrukiListoviKrozRute(t *testing.T) {
	o := novaOkolinaDnevnika(t)
	n := time.Now().In(models.Zagreb)
	dan := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, models.Zagreb)
	predan := func(broj int, opis string) *models.VodocuvarskiList {
		t.Helper()
		kad := time.Now()
		l := &models.VodocuvarskiList{ID: uuid.Must(uuid.NewV7()).String(), UserID: o.vodocuvar.ID.String(), Ime: "Pero Perić", Sektor: "P", AreaID: 1,
			Datum: dan, Broj: broj, Od: "07:00", Do: "15:00", Opis: opis, PredanoAt: &kad}
		if err := o.repo.Save(context.Background(), l); err != nil {
			t.Fatal(err)
		}
		return l
	}
	prvi := predan(41, "obilazak nasipa, predano na laptopu")
	drugi := predan(42, "obilazak nasipa, predano na poslužitelju")

	popis := o.zovi(t, o.rukovodi, http.MethodGet, "/vodocuvar", nil).Body.String()
	for _, x := range []string{"Dvostruki dnevni listovi", `href="/vodocuvar/` + prvi.ID + `">list 41</a>`, `href="/vodocuvar/` + drugi.ID + `">list 42</a>`,
		"dvostruki list – vidi i list br. 41", "dvostruki list – vidi i list br. 42"} {
		if !strings.Contains(popis, x) {
			t.Errorf("popis rukovoditeljice bez %q", x)
		}
	}
	moj := o.zovi(t, o.vodocuvar, http.MethodGet, "/vodocuvar", nil).Body.String()
	if strings.Contains(moj, "Dvostruki dnevni listovi") || !strings.Contains(moj, "dvostruki list – vidi i list br. 42") {
		t.Error("vodočuvar ne dobiva popis dvojnika, ali oznaku na svom listu vidi")
	}
	list := o.zovi(t, o.rukovodi, http.MethodGet, "/vodocuvar/"+prvi.ID, nil).Body.String()
	if !strings.Contains(list, "Dvostruki list – vidi i") || !strings.Contains(list, `href="/vodocuvar/`+drugi.ID+`">list br. 42</a>`) {
		t.Error("predan list bez oznake dvostrukog lista")
	}
}

// Pregled „čeka ovjeru” pokazuje samo predane listove bez ovjere. List čiji
// je dvojnik već ovjeren i ondje nosi oznaku „dvostruki list – vidi i list
// br. …”, jer se upravo ondje ovjerava; oznaka ne ovisi o tome je li i
// drugi list u pregledu.
func TestOznakaDvostrukogListaUPregleduCekaOvjeru(t *testing.T) {
	o := novaOkolinaDnevnika(t)
	n := time.Now().In(models.Zagreb)
	dan := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, models.Zagreb)
	predan := func(broj int, ovjeren bool) *models.VodocuvarskiList {
		t.Helper()
		kad := time.Now()
		l := &models.VodocuvarskiList{ID: uuid.Must(uuid.NewV7()).String(), UserID: o.vodocuvar.ID.String(), Ime: "Pero Perić", Sektor: "P", AreaID: 1,
			Datum: dan, Broj: broj, Od: "07:00", Do: "15:00", Opis: "obilazak nasipa", PredanoAt: &kad}
		if ovjeren {
			l.PotvrdioID, l.Potvrdio, l.PotvrdenoAt = o.rukovodi.ID.String(), "Ana Anić", &kad
		}
		if err := o.repo.Save(context.Background(), l); err != nil {
			t.Fatal(err)
		}
		return l
	}
	predan(41, true)
	predan(42, false)
	ceka := o.zovi(t, o.rukovodi, http.MethodGet, "/vodocuvar?ceka=1", nil).Body.String()
	if !strings.Contains(ceka, "List 42") || strings.Contains(ceka, "List 41") {
		t.Fatal("pregled „čeka ovjeru” mora pokazati samo list 42")
	}
	if !strings.Contains(ceka, "dvostruki list – vidi i list br. 41") {
		t.Error("list 42 u pregledu „čeka ovjeru” bez oznake dvostrukog lista")
	}
}

// Cijela knjiga ispisuje oba predana lista istog dana i svaki nosi oznaku
// „dvostruki list – vidi i list br. …”, kao na popisu; list bez dvojnika je
// nema
func TestKnjigaOznacavaDvostrukeListove(t *testing.T) {
	dan := time.Date(2026, time.March, 10, 0, 0, 0, 0, models.Zagreb)
	kad := dan.Add(15 * time.Hour)
	list := func(id string, d time.Time, broj int) models.VodocuvarskiList {
		return models.VodocuvarskiList{ID: id, UserID: "pero", Ime: "Pero Perić", Datum: d, Broj: broj, Od: "07:00", Do: "15:00", Opis: "obilazak nasipa", PredanoAt: &kad}
	}
	pdf := PDFVodocuvarskaKnjiga([]models.VodocuvarskiList{list("a", dan, 41), list("b", dan, 42), list("c", dan.AddDate(0, 0, 1), 43)},
		"Pero Perić", 2026, models.OrgTerms{}, nil, models.OtisciLista{}, nil)
	// PDF se crta uvijek (i na stroju CI-ja bez pdftotexta, da se kod oznake
	// izvede i izmjeri), a tekst se provjerava gdje ga se da pročitati
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext nije dostupan; PDF je nacrtan, a tekst se ne može provjeriti")
	}
	tekst := pdfTekst(t, pdf)
	for _, x := range []string{"dvostruki list – vidi i list br. 42", "dvostruki list – vidi i list br. 41"} {
		if strings.Count(tekst, x) != 1 {
			t.Errorf("knjiga: %q %d puta, očekivano jednom", x, strings.Count(tekst, x))
		}
	}
	if strings.Count(tekst, "dvostruki list") != 2 {
		t.Errorf("oznaka dvostrukog lista u knjizi %d puta, očekivano dvaput", strings.Count(tekst, "dvostruki list"))
	}
}

// „Upiši” na stranici jednog od dva predana lista istog dana upisuje
// bilješku na taj list, a ne na drugi: obrazac nosi oznaku lista. Upis bez
// oznake lista (pregled dnevnika) ide na list koji uzima i spremanje.
func TestUpisNaDvostrukiListIdeNaTajList(t *testing.T) {
	o := novaOkolinaDnevnika(t)
	n := time.Now().In(models.Zagreb)
	dan := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, models.Zagreb)
	predan := func(broj int) *models.VodocuvarskiList {
		t.Helper()
		kad := time.Now()
		l := &models.VodocuvarskiList{ID: uuid.Must(uuid.NewV7()).String(), UserID: o.vodocuvar.ID.String(), Ime: "Pero Perić", Sektor: "P", AreaID: 1,
			Datum: dan, Broj: broj, Od: "07:00", Do: "15:00", Opis: "obilazak nasipa", PredanoAt: &kad}
		if err := o.repo.Save(context.Background(), l); err != nil {
			t.Fatal(err)
		}
		return l
	}
	prvi, drugi := predan(41), predan(42)
	stranica := o.zovi(t, o.rukovodi, http.MethodGet, "/vodocuvar/"+drugi.ID, nil).Body.String()
	if !strings.Contains(stranica, `name="list" value="`+drugi.ID+`"`) {
		t.Fatal("obrazac upisa na stranici lista ne nosi oznaku lista")
	}
	upisi := func(listID, tekst string) string {
		t.Helper()
		forma := url.Values{"vodocuvar": {o.vodocuvar.ID.String()}, "datum": {dan.Format("2006-01-02")}, "list": {listID}, "tekst": {tekst}, "natrag": {"/vodocuvar/" + listID}}
		return mustUnescape(o.zovi(t, o.rukovodi, http.MethodPost, "/vodocuvar/upis", forma).Header().Get("Location"))
	}
	if l := upisi(drugi.ID, "provjeriti propust"); !strings.HasPrefix(l, "/vodocuvar/"+drugi.ID) || !strings.Contains(l, "na listu 42") {
		t.Errorf("upis sa stranice drugog lista: %s", l)
	}
	upisa := func(id string) int {
		t.Helper()
		l, err := o.repo.Get(context.Background(), id)
		if err != nil || l == nil {
			t.Fatalf("list %s: %v", id, err)
		}
		return len(l.Upisi)
	}
	if upisa(drugi.ID) != 1 || upisa(prvi.ID) != 0 {
		t.Errorf("upis je na listu 41 (%d) umjesto na listu 42 (%d)", upisa(prvi.ID), upisa(drugi.ID))
	}
	if l := upisi("", "upis iz pregleda"); !strings.Contains(l, "na listu 41") || upisa(prvi.ID) != 1 {
		t.Errorf("upis bez oznake lista: %s", l)
	}
}
