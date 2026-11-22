package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Odlazak iz službe s obrasca: neispravan zahtjev i neispravan dan vraćaju s
// porukom, odbijenica servisa vraća na stranicu osobe, a uspjeh isključi
// prijavu i upiše razlog. Izvana račun s pravom globalnog administratora
// traži lozinku vlastitog računa.
func TestObrazacOdlaska(t *testing.T) {
	o := novaOkolinaRacuna(t)
	pero := o.osoba("pperic", false)
	putanja := "/users/" + pero.ID.String() + "/odlazak"

	if w := o.salji(izravno, "/users/nije-oznaka/odlazak", url.Values{}); w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "Neispravan") {
		t.Errorf("neispravna oznaka: %d %s", w.Code, w.Header().Get("Location"))
	}
	if g := greska(t, o.salji(izravno, putanja, url.Values{"razlog": {"mirovina"}, "dan": {"21.11.2026."}})); !strings.Contains(g, "datum") {
		t.Errorf("neispravan dan: %q", g)
	}
	if g := greska(t, o.salji(izravno, putanja, url.Values{"razlog": {"otkaz"}})); !strings.Contains(g, "razlog") {
		t.Errorf("nepoznat razlog: %q", g)
	}
	jucer := time.Now().In(models.Zagreb).AddDate(0, 0, -1).Format("2006-01-02")
	w := o.salji(izravno, putanja, url.Values{"razlog": {"mirovina"}, "dan": {jucer}})
	if g := greska(t, w); g != "" {
		t.Fatalf("odlazak odbijen: %q", g)
	}
	sad, err := o.repo.GetUserByID(pero.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sad.IsActive || sad.Odlazak != models.OdlazakMirovina || sad.OdlazakDan == nil || sad.OdlazakDan.In(models.Zagreb).Format("2006-01-02") != jucer {
		t.Errorf("nakon odlaska: aktivan %v, %q %v", sad.IsActive, sad.Odlazak, sad.OdlazakDan)
	}

	sef := o.osoba("sef", true)
	if w := o.salji(krozTunel("198.51.100.7"), "/users/"+sef.ID.String()+"/odlazak", url.Values{"razlog": {"odlazak"}}); !bezLozinke(t, w) {
		t.Errorf("izvana odlazak uprave bez lozinke: %s", w.Header().Get("Location"))
	}
}
