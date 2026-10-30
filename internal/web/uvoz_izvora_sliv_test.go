package web

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/models"
)

const dnevniPrimjerovaHIS = "Dnevni podaci postaje PRIMJEROVO - DRAVA,  VODOSTAJ  (cm)\r\n01.01.2001;176;\r\n02.01.2001;186;\r\n"

// Pregled posebnog izvora: letva koja u stablu već stoji ostaje u svom slivu
// i kad obrazac kaže drugi, a nova letva sa slivom koji nije mapa u stablu
// odbija se. Nova letva s običnim slivom prolazi.
func TestPosebniIzvorSlivIzStabla(t *testing.T) {
	o := novaOkolinaVrata(t)
	his := datotekaObrasca{"datoteke", "vodostaj.csv", dnevniPrimjerovaHIS}
	for _, s := range []struct {
		opis  string
		u     *models.User
		p     *models.UserPermissions
		letva string
		sliv  string
		hoce  string // sliv u pregledu; prazno znači da se pregled odbija
	}{
		{"postojeća letva, sliv izvan stabla", o.ana, o.anaOvl, "primjerovo", "../..", "primjerica"},
		{"postojeća letva, drugi sliv", o.ana, o.anaOvl, "primjerovo", "drugica", "primjerica"},
		{"nova letva, sliv izvan stabla", o.pero, o.peroOvl, "novovo", "..", ""},
		{"nova letva, običan sliv", o.pero, o.peroOvl, "novovo", "novica", "novica"},
	} {
		t.Run(s.opis, func(t *testing.T) {
			r := viseDijelni(t, "/administracija/uvoz-izvora/pregled",
				map[string]string{"format": "his2000", "letva": s.letva, "sliv": s.sliv}, his)
			w := odgovor(o.h.PregledIzvora, r, s.u, s.p)
			html := htmlUTekst(w.Body.String())
			if w.Code != http.StatusOK {
				t.Fatalf("status %d", w.Code)
			}
			ima := strings.Contains(html, "što bi se upisalo</h2>")
			if s.hoce == "" {
				if ima || !strings.Contains(html, "nije upotrebljiv kao naziv mape") {
					t.Errorf("pregled nije odbijen: %s", porukeStranice(html))
				}
				return
			}
			if !ima || !strings.Contains(html, "<dt>Sliv</dt><dd>"+s.hoce+"</dd>") {
				t.Errorf("pregled bez sliva %s: %s", s.hoce, porukeStranice(html))
			}
		})
	}
	if d := o.datoteke(t); len(d) != 0 {
		t.Errorf("pregled je zapisao %v", d)
	}
}

// Povijest s HydroViewa za postojeću letvu ide u njezin sliv, što god
// obrazac kaže o slivu.
func TestPovijestSHydroViewaUSlivLetve(t *testing.T) {
	o, _, _ := okolinaUpisa(t)
	srv := lazniHydroViewVrata(t)
	o.saHydroViewom(t, srv.URL)
	forma := url.Values{"letva": {"primjerovo"}, "sliv": {".."}, "od": {"2001-03-01"}, "do": {"2001-03-01"}}
	w := odgovor(o.h.PreuzmiHidroView, obicniObrazac("/administracija/uvoz-izvora/hidroview", forma.Encode()), o.pero, o.peroOvl)
	p := posaoSaStranice(t, o.h.poslovi, htmlUTekst(w.Body.String()), o.pero)
	if p.Greska() != "" {
		t.Fatalf("preuzimanje: %q", p.Greska())
	}
	d := o.datoteke(t)
	if len(d) != 1 || d[0] != filepath.Join(o.koren, "primjerica", "primjerovo", "primjerovo_geolux-radar_vodostaj_satni_2001.csv") {
		t.Errorf("zapisano %v", d)
	}
}
