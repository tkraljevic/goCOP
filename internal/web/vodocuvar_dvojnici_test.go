package web

import (
	"context"
	"net/http"
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
