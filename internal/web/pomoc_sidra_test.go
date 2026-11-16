package web

import (
	"bytes"
	"html/template"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"gocop/internal/models"
	webassets "gocop/web"
)

// sidroStranice je ono što znak „?” stranice dodaje na /pomoc, kako ga
// predložak stranice i base.html zajedno iscrtaju
func sidroStranice(t *testing.T, stranica string, data any) string {
	t.Helper()
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := tp.ExecuteTemplate(&b, "sidroPomoci", data); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// Znak „?” vodi na najuži odjeljak Pomoći koji postoji: registri objekata na
// Objekte (prije na nepostojeće #objekti-…), stranice Dokumenata i
// Administracije na svoj podnaslov umjesto na vrh velikog poglavlja.
func TestZnakPomociVodiNaPostojeceSidro(t *testing.T) {
	h := pomocHTML(t)
	sidra := map[string]bool{}
	for _, m := range regexp.MustCompile(`id="([a-z0-9-]+)"`).FindAllStringSubmatch(h, -1) {
		sidra[m[1]] = true
	}
	for _, reg := range models.StructureRegisters {
		if s := sidroPomoci(navObjekta(reg.Kind)); !sidra[s] {
			t.Errorf("registar %s vodi na #%s, a toga u Pomoći nema", reg.Kind, s)
		}
	}
	if got := sidroStranice(t, "structures.html", struct{ ActiveNav string }{navObjekta("NASIP")}); got != "#structures" {
		t.Errorf("nasipi: %q, želim #structures", got)
	}

	// svaka stranica koja sama zadaje sidro mora pogoditi postojeće
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	stranice, _ := fs.Glob(templatesFS, "*.html") // uzorak je ispravan

	zadano := 0
	for _, st := range stranice {
		b, _ := fs.ReadFile(templatesFS, st) // datoteka je upravo nađena u istom FS-u
		def := regexp.MustCompile(`(?s)\{\{define "sidroPomoci"\}\}(.*?)\{\{end\}\}\n`).FindStringSubmatch(string(b))
		if def == nil {
			continue
		}
		zadano++
		for _, m := range regexp.MustCompile(`#([a-z0-9-]+)`).FindAllStringSubmatch(def[1], -1) {
			if !sidra[m[1]] {
				t.Errorf("%s vodi na #%s, a toga u Pomoći nema", st, m[1])
			}
		}
	}
	if zadano < 30 {
		t.Errorf("sidro zadaje samo %d stranica", zadano)
	}

	for stranica, want := range map[string]string{
		"akti.html":                 "#akti",
		"administracija_tema.html":  "#admin-tema",
		"imenik_exchange.html":      "#posta",
		"dnevnik_dezurstva.html":    "#dnevnik-dezurstva",
		"administracija.html":       "#admin",
		"administracija_posta.html": "#admin-eposta",
	} {
		if got := sidroStranice(t, stranica, struct{ ActiveNav string }{"admin"}); got != want {
			t.Errorf("%s: znak „?” vodi na %q, želim %q", stranica, got, want)
		}
	}
	type popis struct {
		ActiveNav   string
		StranicaCOP bool
		Vrsta       string
	}
	for _, c := range []struct {
		data popis
		want string
	}{
		{popis{"journals", true, ""}, "#dnevnik-cop"},
		{popis{"journals", false, "ODRZAVANJE_A03"}, "#dnevnik-usluge"},
		{popis{"journals", false, "DEZURSTVA"}, "#dnevnik-dezurstva"},
		{popis{"journals", false, ""}, "#journals"},
	} {
		if got := sidroStranice(t, "dnevnici.html", c.data); got != c.want {
			t.Errorf("popis dnevnika %+v: %q, želim %q", c.data, got, c.want)
		}
	}
	if !strings.Contains(h, `<li id="admin-eposta">`) {
		t.Error("stavka E-pošta u Administraciji nema sidro")
	}
}
