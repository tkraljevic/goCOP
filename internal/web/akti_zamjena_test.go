package web

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/service"
)

// novoUmjesto: poništen akt ima karticu Poništenje s razlogom i gumbom „Novi
// akt”. Gumb otvara obrazac s istom letvom, radnjom, stupnjem i aktom za
// prekid, a u napomeni stoji koji akt novi zamjenjuje (Odluke (9. 11. 2026.)).
func novoUmjesto(t *testing.T, akti *service.AktService, id string,
	zovi func(string, string, url.Values) *httptest.ResponseRecorder, mora func(*httptest.ResponseRecorder, string, ...string)) {
	t.Helper()
	a, err := akti.Get(context.Background(), id)
	if err != nil || a == nil || !a.Storniran() {
		t.Fatalf("poništen akt: %+v, %v", a, err)
	}
	w := zovi(http.MethodGet, "/akti/"+id, nil)
	mora(w, "kartica Poništenje", "Poništenje</h2>", "<dt>Razlog</dt><dd>prekid je ovjeren prerano</dd>", "Ispravak ide novim aktom")
	m := regexp.MustCompile(`<a class="btn btn-secondary" href="([^"]+)">Novi akt</a>`).FindStringSubmatch(w.Body.String())
	if m == nil {
		t.Fatal("poništen akt nema gumba Novi akt")
	}
	recenica := models.RecenicaZamjene(*a)
	if !strings.HasPrefix(recenica, "Ovim aktom zamjenjuje se akt "+a.Oznaka()+" od ") || !strings.HasSuffix(recenica, ", koji je poništen i ne vrijedi.") {
		t.Errorf("rečenica zamjene: %q", recenica)
	}
	mora(zovi(http.MethodGet, html.UnescapeString(m[1]), nil), "obrazac umjesto poništenog",
		`value="PREKID" checked`, `<option value="`+a.PrekidaAktID+`" data-podrucje="34" selected>`, ">"+recenica+"</textarea>")
	if (AktiPageData{Akt: &models.Akt{}}).NoviUmjestoPonistenog() != "" {
		t.Error("akt koji nije poništen nema adrese novog akta umjesto njega")
	}
}

// Kartica Ovjera na nacrtu kaže tko ovjerava akt njegova stupnja i sektora;
// bez akta nema rečenice
func TestOvjeravaAkt(t *testing.T) {
	if got := (AktiPageData{}).OvjeravaAkt(); got != "" {
		t.Errorf("bez akta: %q", got)
	}
	d := AktiPageData{Akt: &models.Akt{Sektor: "B", Stupanj: models.PhaseEmergency}}
	if got := d.OvjeravaAkt(); !strings.HasPrefix(got, "Rukovoditelj obrane od poplava Sektora B ili viši.") {
		t.Errorf("izvanredna obrana: %q", got)
	}
}
