package web

import (
	"bytes"
	"html/template"
	"io/fs"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	webassets "gocop/web"
)

// Papir akta stoji u zasebnom predlošku: stranica ga crta samo preko njega, a
// sam predložak radi s aktom, sektorom i područjem, bez ostatka stranice.
func TestPapirAktaUZasebnomPredlosku(t *testing.T) {
	if !slices.Contains(DijeloviPredloska("akt.html"), "akt_papir.html") {
		t.Fatal("akt.html nema akt_papir.html među dijelovima predloška")
	}

	// kroz rutu, s pravim podacima iz baze
	o := novaOkolinaOznake(t)
	a := o.akt(t, "Primjerovo", "laptop", "KOD-1", models.PhaseRegular)
	imaNaStranici(t, o.zovi(t, o.voditelj, o.vOvl, http.MethodGet, "/akti/"+a.ID, nil), "stranica akta",
		`<div class="akt-papir">`, "Centar obrane od poplava Sektora B", "na branjenom području 15: Mali sliv Primjerica", "O tome obavijest:")

	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("akt.html")...)
	if err != nil {
		t.Fatal(err)
	}
	if tp.Lookup("akt_papir") == nil {
		t.Fatal("predložak akt_papir nije učitan")
	}
	zamijenjen, err := tp.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zamijenjen.Parse(`{{define "akt_papir"}}<div id="zamjena"></div>{{end}}`); err != nil {
		t.Fatal(err)
	}

	akt := &models.Akt{Sektor: "B", AreaID: 15, Radnja: models.AktUspostava, Stupanj: models.PhaseRegular, StationName: "Primjerovo",
		Vrijedi: time.Date(2026, 10, 6, 3, 0, 0, 0, models.Zagreb), Uvod: "Uvod iz probe.", Izradio: "Ana Anić"}
	data := AktiPageData{CurrentUser: &models.User{ID: uuid.New(), Username: "aanic", FullName: "Ana Anić"},
		Permissions: &models.UserPermissions{}, Akt: akt,
		Sektor: &models.Sector{ID: "B", VgoName: "VGO Primjerovo"}, Podrucje: &models.Area{ID: 15, Name: "Mali sliv Primjerica"}}
	crtaj := func(p *template.Template, ime string, podaci any) string {
		t.Helper()
		var b bytes.Buffer
		if err := p.ExecuteTemplate(&b, ime, podaci); err != nil {
			t.Fatalf("%s: %v", ime, err)
		}
		return b.String()
	}

	// sam papir, samo s onim što mu treba, i na stranici stoji baš takav
	papir := crtaj(tp, "akt_papir", data.Papir())
	for _, d := range []string{`<div class="akt-papir">`, "NACRT — nije ovjeren", "Uvod iz probe.", "VGO Primjerovo", "na branjenom području 15: Mali sliv Primjerica"} {
		if !strings.Contains(papir, d) {
			t.Errorf("papir nema %q:\n%s", d, papir)
		}
	}
	if stranica := crtaj(tp, "akt.html", data); !strings.Contains(stranica, papir) {
		t.Errorf("na stranici nema papira iz predloška:\n%s", stranica)
	}

	// kad se predložak zamijeni, od papira na stranici ne ostane ništa
	stranica := crtaj(zamijenjen, "akt.html", data)
	if !strings.Contains(stranica, `<div id="zamjena"></div>`) {
		t.Error("stranica ne crta papir preko predloška akt_papir")
	}
	for _, d := range []string{`class="akt-papir"`, "O tome obavijest:", "Uvod iz probe."} {
		if strings.Contains(stranica, d) {
			t.Errorf("stranica i dalje sama crta dio papira: %q", d)
		}
	}
}
