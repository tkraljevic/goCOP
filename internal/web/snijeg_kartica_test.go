package web

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/snijeg"
)

// Brojevi snijega u testovima su izmišljeni.

// stanjaSnijega su četiri alpska međusliva: A na razini 5 zima, B ispod
// praga, K na 10 zima, L na 3 zime (crtež „snijeg”).
func stanjaSnijega(zadnji int64) []snijeg.StanjeSliva {
	st := func(oznaka string, mm float64, p snijeg.Pragovi, razina int, trend string) snijeg.StanjeSliva {
		s, _ := snijeg.AlpskiSliv(oznaka)
		return snijeg.StanjeSliva{Sliv: s, Ima: true, Dan: zadnji, Mm: mm, Pragovi: p, ImaPragove: true, Razina: razina,
			Ukljucen: razina > 0, Trend: trend,
			Sezona:   []snijeg.TockaKrivulje{{Dan: zadnji - 2, Mm: mm - 20}, {Dan: zadnji, Mm: mm}},
			Procjena: []snijeg.TockaKrivulje{{Dan: zadnji, Mm: mm}, {Dan: zadnji + 3, Mm: mm + 5}}}
	}
	return []snijeg.StanjeSliva{
		st("A", 170, snijeg.Pragovi{130, 160, 220}, 2, "raste"),
		st("B", 50, snijeg.Pragovi{65, 70, 110}, 0, "raste"),
		st("K", 315, snijeg.Pragovi{205, 275, 310}, 3, "miruje"),
		st("L", 95, snijeg.Pragovi{90, 110, 130}, 1, "topi se"),
	}
}

// Kartica: retci po rijekama, razina, traka s crticama, trend i podnožje;
// alarm se zasad ne prikazuje.
func TestKarticaSnijega(t *testing.T) {
	sad := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	zadnji := sad.Unix()/86400 - 1
	k := karticaSnijega(stanjaSnijega(zadnji), sad, "/prognoze/vrijeme")
	p := k.Podaci.(SnijegNaslovne)
	if k.ID != kSnijeg || k.Naslov != "Snijeg u Alpama" || len(p.Skupine) != 2 || p.Skupine[0].Rijeka != "Drava" || len(p.Skupine[1].Retci) != 2 {
		t.Fatalf("kartica %+v", k)
	}
	a, b := p.Skupine[0].Retci[0], p.Skupine[0].Retci[1]
	if a.Klasa != "kisa-narancasto" || a.Rijetko != "jednom u 5 zima" || a.Mm != "170 mm vode" || a.Trend != "raste" ||
		a.Popuna != 61.8 || len(a.Crtice) != 3 || a.Crtice[2].Lijevo != 80 || a.URL != "/slivovi#sliv-A" {
		t.Errorf("Gornja Drava %+v", a)
	}
	if b.Klasa != "" || b.Rijetko != "" {
		t.Errorf("Mura ispod praga %+v", b)
	}
	if l := p.Skupine[1].Retci[1]; l.Rijetko != "jednom u 3 zime" || l.Klasa != "kisa-zuto" {
		t.Errorf("Traun i Enns %+v", l)
	}
	if k := p.Skupine[1].Retci[0]; k.Rijetko != "jednom u 10 zima" || k.Popuna != 81.3 {
		t.Errorf("Inn i Salzach %+v", k)
	}
	if p.Kasni != "" || p.Alarm != nil {
		t.Errorf("kasni %q, alarm %v", p.Kasni, p.Alarm)
	}

	o := novaOkolinaNaslovne(t)
	var b2 bytes.Buffer
	if err := o.h.tmpl.ExecuteTemplate(&b2, "karticaNaslovne", k); err != nil {
		t.Fatal(err)
	}
	html := b2.String()
	for _, zeli := range []string{`id="kartica-snijeg-u-alpama"`, "Snijeg u Alpama", "Na tlu je puno snijega.", ">Drava<", ">Dunav<",
		"Južni i Istočni Tirol, Koruška, slovenska Drava do Borla", `class="kisa-redak snijeg-redak snijeg-ispod"`,
		`<span class="badge kisa-crveno">jednom u 10 zima</span>`, `style="width: 61.8%"`, "topi se",
		"Crtice: toliko snijega bude jednom u 3, 5 i 10 zima.", "Kartica nestane kad snijeg u svim slivovima padne ispod pola praga za 3 zime.",
		"Snijeg na tlu: SNOWGRID, GeoSphere Austria (austrijski dio sliva, kasni 1–2 dana).",
		"Prognoza vodostaja računa s topljenjem snijega.", `href="/prognoze/vrijeme"`, "Više na Prognoze › Vrijeme"} {
		if !strings.Contains(html, zeli) {
			t.Errorf("kartica nema %q", zeli)
		}
	}
	if strings.Contains(html, "snijeg-alarm") {
		t.Error("alarm se prikazuje")
	}

	// kuka za alarm: kad se jednom napuni, okvir i redak su na kartici
	p.Alarm = &AlarmSnijega{Naslov: "Kiša i topljenje na puno snijega!", Tekst: "U idućih 5 dana s gornje Drave dolazi oko 95 mm vode."}
	sa := stanjaSnijega(zadnji)
	sa[0].Alarm = &snijeg.AlarmSliva{Mm: 95, Rijetko: "jednom u 5 godina"}
	p.Skupine[0].Retci[0] = redakSnijega(sa[0])
	k.Podaci = p
	b2.Reset()
	if err := o.h.tmpl.ExecuteTemplate(&b2, "karticaNaslovne", k); err != nil {
		t.Fatal(err)
	}
	for _, zeli := range []string{`class="snijeg-alarm" role="alert"`, "Kiša i topljenje na puno snijega!", "idućih 5 dana: 95 mm vode · jednom u 5 godina"} {
		if !strings.Contains(b2.String(), zeli) {
			t.Errorf("alarm: nema %q", zeli)
		}
	}
}

// Redak bez podataka, bez pragova i bez snijega; SNOWGRID koji kasni.
func TestRedakSnijegaRubovi(t *testing.T) {
	s, _ := snijeg.AlpskiSliv("B")
	if r := redakSnijega(snijeg.StanjeSliva{Sliv: s}); r.Mm != "—" || r.Popuna != 0 || r.Opis != "nema podataka o snijegu" {
		t.Errorf("bez podataka %+v", r)
	}
	if r := redakSnijega(snijeg.StanjeSliva{Sliv: s, Ima: true, Mm: 40}); r.Popuna != 80 || len(r.Crtice) != 0 || r.Opis != "40 mm vode" {
		t.Errorf("bez pragova %+v", r)
	}
	if r := redakSnijega(snijeg.StanjeSliva{Sliv: s, Ima: true}); r.Popuna != 0 || r.Crtice != nil {
		t.Errorf("bez snijega %+v", r)
	}
	sad := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	if k := kasniSnijeg(stanjaSnijega(sad.Unix()/86400-5), sad); k != "Podaci o snijegu kasne: zadnji je od 19. 11., pa se prikazuje on." {
		t.Errorf("kasni: %q", k)
	}
}

// Na Naslovnoj: kartica je tu dok je ijedan sliv uključen, a nema je kad
// snijeg nije rijedak, kad izvora nema i kad se ne da pročitati.
func TestNaslovnaSnijeg(t *testing.T) {
	o := novaOkolinaNaslovne(t)
	admin := &models.UserPermissions{IsGlobalAdmin: true, User: models.User{IsGlobalAdmin: true}}
	zadnji := time.Now().Unix()/86400 - 1
	stanja, greska := stanjaSnijega(zadnji), error(nil)
	o.h.SetIzvori(IzvoriNaslovne{Snijeg: func() ([]snijeg.StanjeSliva, error) { return stanja, greska }})
	if s := o.zovi("/", admin).Body.String(); !strings.Contains(s, `id="kartica-snijeg-u-alpama"`) || !strings.Contains(s, "jednom u 5 zima") {
		t.Error("kartice snijega nema, a snijeg je rijedak")
	}
	for i := range stanja {
		stanja[i].Ukljucen = false
	}
	if s := o.zovi("/", admin).Body.String(); strings.Contains(s, "kartica-snijeg-u-alpama") {
		t.Error("kartica snijega je tu, a nijedan sliv nije uključen")
	}
	greska = errors.New("baza")
	if s := o.zovi("/", admin).Body.String(); strings.Contains(s, "kartica-snijeg-u-alpama") {
		t.Error("kartica snijega je tu uz grešku")
	}
	o.h.SetIzvori(IzvoriNaslovne{})
	if s := o.zovi("/", admin).Body.String(); strings.Contains(s, "kartica-snijeg-u-alpama") {
		t.Error("kartica snijega je tu bez izvora")
	}
	// račun koji ne vidi Prognoze nema poveznicu „Više”
	stanja, greska = stanjaSnijega(zadnji), nil
	o.h.SetIzvori(IzvoriNaslovne{Snijeg: func() ([]snijeg.StanjeSliva, error) { return stanja, greska }})
	if k, ima := o.h.karticaSnijega(models.Visibility{models.ModuleJournals: true}); !ima || k.Podaci.(SnijegNaslovne).Vise != "" {
		t.Errorf("poveznica bez Prognoza: %+v", k.Podaci)
	}
	if s, err := (&Server{}).snijegSlivova(); s != nil || err != nil {
		t.Errorf("poslužitelj bez snijega: %v %v", s, err)
	}
}

// Krivulja: os, mjeseci, danas, puna i crtkana crta i pragovi s natpisima.
func TestCrtajSnijegNaTlu(t *testing.T) {
	danas := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC).Unix() / 86400
	od := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC).Unix() / 86400
	p := snijeg.Pragovi{130, 160, 220}
	g := string(crtajSnijegNaTlu(grafSnijega{Sezona: []snijeg.TockaKrivulje{{Dan: od, Mm: 0}, {Dan: danas - 1, Mm: 170}},
		Procjena: []snijeg.TockaKrivulje{{Dan: danas - 1, Mm: 170}, {Dan: danas + 5, Mm: 160}}, Pragovi: &p, Od: od, Danas: danas,
		Sirina: 960, Visina: 260, Opis: "Snijeg <A>"}))
	for _, zeli := range []string{`<svg class="graf-snijega" viewBox="0 0 960 260" role="img" aria-label="Snijeg &lt;A&gt;">`,
		">1. 9.<", ">1. 11.<", ">danas<", `class="graf-snijega-snowgrid"`, `class="graf-snijega-procjena"`,
		`class="graf-snijega-prag kisa-crveno"`, ">3 zime 130<", ">5 zima 160<", ">10 zima 220<", `class="graf-snijega-zadnji"`, ">250<"} {
		if !strings.Contains(g, zeli) {
			t.Errorf("krivulja nema %q", zeli)
		}
	}
	if !strings.Contains(g, ">1. 8.<") {
		t.Error("nema 1. 8.")
	}
	if crtajSnijegNaTlu(grafSnijega{}) != "" {
		t.Error("prazna krivulja")
	}
	// bez pragova os je po snijegu, a velik snijeg dobije veći korak
	g = string(crtajSnijegNaTlu(grafSnijega{Sezona: []snijeg.TockaKrivulje{{Dan: od, Mm: 900}}, Od: od, Danas: danas, Sirina: 640, Visina: 190}))
	if !strings.Contains(g, ">1.000<") || strings.Contains(g, "graf-snijega-prag") {
		t.Errorf("os bez pragova: %s", g)
	}
}
