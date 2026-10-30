package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Izmišljeni spojeni niz letve Primjerovo: tri dnevna vodostaja 2014. (i
// jedan 2013.), jedan satni protok.
var (
	danArhive1 = time.Date(2014, 1, 1, 6, 0, 0, 0, time.UTC)
	danArhive2 = time.Date(2014, 1, 2, 6, 0, 0, 0, time.UTC)
	danArhive3 = time.Date(2014, 12, 31, 6, 0, 0, 0, time.UTC)
)

func spojArhive(letva, velicina, korak string, kad time.Time, v float64, izvor string, tocnost float64) string {
	return fmt.Sprintf(`INSERT INTO spoj (letva, velicina, korak, vrijeme, vrijednost, izvor, vrsta, tocnost)
		VALUES ('%s', '%s', '%s', %d, %g, '%s', 'jutarnji', %g)`, letva, velicina, korak, kad.Unix(), v, izvor, tocnost)
}

// posluziteljArhive je poslužitelj očitanja s arhivom koja nosi niz letve Primjerovo.
func posluziteljArhive(t *testing.T) *posluziteljOcitanja {
	t.Helper()
	p := noviPosluziteljOcitanja(t)
	p.s.SetArhiva(arhivaBrojki(t, nil,
		spojArhive("primjerovo", "vodostaj", "dnevni", danArhive2, -118, "dhmz", 1),
		spojArhive("primjerovo", "vodostaj", "dnevni", danArhive1, -120, "dhmz", 1),
		spojArhive("primjerovo", "vodostaj", "dnevni", danArhive3, 35, "rekonstrukcija", 4),
		spojArhive("primjerovo", "vodostaj", "dnevni", time.Date(2013, 12, 31, 6, 0, 0, 0, time.UTC), -90, "dhmz", 1),
		spojArhive("primjerovo", "protok", "satni", time.Date(2014, 6, 1, 10, 0, 0, 0, time.UTC), 231, "his2000", 12),
	))
	return p
}

// Bez prijave nema ni izvoza ni ispravaka arhive.
func TestArhivaCSVBezPrijaveVodiNaPrijavu(t *testing.T) {
	p := posluziteljArhive(t)
	osnova := "/readings/station/" + p.letva.ID.String()
	for _, w := range []interface{ Result() *http.Response }{
		p.posalji("", http.MethodGet, osnova+"/izvoz.csv?god=2014", nil, ""),
		p.datoteka(t, "", osnova+"/uvoz", "a.csv", "vrijeme;ispravak\n", map[string]string{"god": "2014"}),
		p.obrazac("", osnova+"/uvoz/potvrdi", url.Values{"kad": {danArhive1.Format(time.RFC3339)}}),
	} {
		res := w.Result()
		if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/login" {
			t.Errorf("bez prijave: %d → %q", res.StatusCode, res.Header.Get("Location"))
		}
	}
}

// Izvoz godine spojenog niza: od najstarije vrijednosti, u zagrebačkom
// vremenu, s izvorom i točnošću i praznim stupcima za ispravak i razlog.
func TestIzvozGodineSpojenogNiza(t *testing.T) {
	p := posluziteljArhive(t)
	put := "/readings/station/" + p.letva.ID.String() + "/izvoz.csv"
	w := p.posalji("dežurni", http.MethodGet, put+"?god=2014", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("izvoz: %d %s", w.Code, w.Body.String())
	}
	if v := w.Header().Get("Content-Disposition"); v != `attachment; filename="primjerovo_vodostaj_dnevni_2014.csv"` {
		t.Errorf("ime datoteke %q", v)
	}
	if v := w.Header().Get("Content-Type"); v != "text/csv; charset=utf-8" {
		t.Errorf("vrsta %q", v)
	}
	redci := citajCSV(t, w.Body.String())
	zeli := [][]string{
		{"vrijeme", "vodostaj_cm", "izvor", "tocnost", "ispravak", "razlog"},
		{"2014-01-01 07:00:00", "-120", "dhmz", "1", "", ""},
		{"2014-01-02 07:00:00", "-118", "dhmz", "1", "", ""},
		{"2014-12-31 07:00:00", "35", "rekonstrukcija", "4", "", ""},
	}
	if len(redci) != len(zeli) {
		t.Fatalf("redci: %q", redci)
	}
	for i := range zeli {
		if strings.Join(redci[i], "|") != strings.Join(zeli[i], "|") {
			t.Errorf("redak %d: %q, želim %q", i, redci[i], zeli[i])
		}
	}

	// satni protok ima svoj stupac i ime; nepoznat korak je dnevni. Protok
	// se zasad piše bez decimala (decimalaVelicine("protok") je 0, a to je
	// greška), pa se vrijednost čita kao broj, da provjera ne ovisi o broju
	// decimala.
	w = p.posalji("dežurni", http.MethodGet, put+"?god=2014&v=protok&korak=satni", nil, "")
	redci = citajCSV(t, w.Body.String())
	if w.Header().Get("Content-Disposition") != `attachment; filename="primjerovo_protok_satni_2014.csv"` ||
		len(redci) != 2 || redci[0][1] != "protok_m3s" || redci[1][0] != "2014-06-01 12:00:00" {
		t.Errorf("satni protok: %q %q", w.Header().Get("Content-Disposition"), redci)
	} else if q, err := strconv.ParseFloat(strings.Replace(redci[1][1], ",", ".", 1), 64); err != nil || q != 231 {
		t.Errorf("satni protok: %q, želim 231", redci[1][1])
	}
	w = p.posalji("dežurni", http.MethodGet, put+"?god=2014&korak=tjedni", nil, "")
	if w.Header().Get("Content-Disposition") != `attachment; filename="primjerovo_vodostaj_dnevni_2014.csv"` {
		t.Errorf("nepoznat korak: %q", w.Header().Get("Content-Disposition"))
	}

	// godina mora biti zadana i razumna
	for _, upit := range []string{"", "?god=abc", "?god=1799", "?god=2201"} {
		if w := p.posalji("dežurni", http.MethodGet, put+upit, nil, ""); w.Code != http.StatusBadRequest {
			t.Errorf("godina %q: %d", upit, w.Code)
		}
	}
	if w := p.posalji("dežurni", http.MethodGet, "/readings/station/"+uuid.NewString()+"/izvoz.csv?god=2014", nil, ""); w.Code != http.StatusNotFound {
		t.Errorf("nepostojeća letva: %d", w.Code)
	}
	bez := &models.Station{ID: uuid.New(), Name: "Bezimena"}
	if err := repository.NewStationRepository(p.baza, ledger.New(p.baza, "test")).CreateStation(context.Background(), bez); err != nil {
		t.Fatal(err)
	}
	if w := p.posalji("dežurni", http.MethodGet, "/readings/station/"+bez.ID.String()+"/izvoz.csv?god=2014", nil, ""); w.Code != http.StatusNotFound {
		t.Errorf("letva bez šifre: %d", w.Code)
	}
}

// Čvor bez arhive nema što izvesti ni ispraviti.
func TestArhivaCSVBezArhive(t *testing.T) {
	p := noviPosluziteljOcitanja(t)
	osnova := "/readings/station/" + p.letva.ID.String()
	w := p.posalji("uprava", http.MethodGet, osnova+"/izvoz.csv?god=2014", nil, "")
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "Arhiva nije dostupna") {
		t.Errorf("izvoz bez arhive: %d %q", w.Code, w.Body.String())
	}
	if w := p.datoteka(t, "uprava", osnova+"/uvoz", "a.csv", "vrijeme;ispravak\n", map[string]string{"god": "2014"}); w.Code != http.StatusNotFound {
		t.Errorf("uvoz bez arhive: %d", w.Code)
	}
	// bez radne baze nema ni pohrane ispravaka
	p.s.db = nil
	if w := p.obrazac("uprava", osnova+"/uvoz/potvrdi", url.Values{}); w.Code != http.StatusNotFound {
		t.Errorf("potvrda bez baze: %d", w.Code)
	}
}

func TestStupacVelicine(t *testing.T) {
	for v, zeli := range map[string]string{"protok": "protok_m3s", "temperatura": "temperatura_c",
		"koncentracija": "koncentracija_gm3", "pronos": "pronos_t", "vodostaj": "vodostaj_cm", "": "vodostaj_cm", "nesto": "vodostaj_cm"} {
		if got := stupacVelicine(v); got != zeli {
			t.Errorf("stupacVelicine(%q) = %q, želim %q", v, got, zeli)
		}
	}
}

// Vraćena datoteka pokazuje što bi se promijenilo, a ništa ne upisuje:
// promjena s razlogom ide u obrazac potvrde, ista vrijednost je netaknuta,
// a ispravak bez razloga ili trenutak kojeg nema stoji kao greška.
func TestUvozIspravakaArhivePokazujePregled(t *testing.T) {
	p := posluziteljArhive(t)
	put := "/readings/station/" + p.letva.ID.String() + "/uvoz"
	datoteka := "\ufeffvrijeme;vodostaj_cm;izvor;tocnost;ispravak;razlog\n" +
		"2014-01-01 07:00:00;-120;dhmz;1;-121;predznak s lista\n" + // promjena
		"2014-01-02 07:00:00;-118;dhmz;1;-118;isto\n" + // netaknuto
		"2014-12-31 07:00:00;35;rekonstrukcija;4;36;\n" + // bez razloga
		"2014-03-03 07:00:00;;;;10;nema ga\n" + // nema u arhivi
		"2014-02-30 07:00:00;;;;10;krivi datum\n" +
		"2014-01-01 07:00:00;-120;dhmz;1;deset;slovima\n" +
		"2014-01-02 07:00:00;-118;dhmz;1;;\n" // nije diran

	w := p.datoteka(t, "uprava", put, "primjerovo_vodostaj_dnevni_2014.csv", datoteka, map[string]string{"god": "2014"})
	if w.Code != http.StatusOK {
		t.Fatalf("pregled: %d %q", w.Code, w.Header().Get("Location"))
	}
	html := w.Body.String()
	for _, zeli := range []string{"primjerovo_vodostaj_dnevni_2014.csv", "2014.",
		"mijenja se <strong>1</strong>", "bez promjene <strong>1</strong>", "4 s greškom",
		"ispravak bez razloga — upiši zašto se mijenja", "tog trenutka nema u arhivi",
		"vrijeme nije čitljivo: 2014-02-30 07:00:00", "vrijednost nije broj: deset",
		`action="/readings/station/` + p.letva.ID.String() + `/uvoz/potvrdi"`,
		`name="kad" value="2014-01-01T06:00:00Z"`, `name="novo" value="-121"`, `name="staro" value="-120"`,
		`name="razlog" value="predznak s lista"`, "Upiši 1 ispravak"} {
		if !strings.Contains(html, zeli) {
			t.Errorf("pregled nema %q", zeli)
		}
	}
	if strings.Count(html, `name="kad"`) != 1 {
		t.Errorf("u obrascu potvrde %d redaka, želim 1", strings.Count(html, `name="kad"`))
	}
	if n, err := repository.NewIspravakRepository(p.baza, nil).Broj(context.Background(), "primjerovo", "vodostaj", "dnevni"); err != nil || n != 0 {
		t.Errorf("pregled je upisao %d ispravaka", n)
	}

	// stari naziv stupca vremena još se čita
	w = p.datoteka(t, "uprava", put, "stara.csv", "vrijeme_utc;ispravak;razlog\n2014-01-01 07:00:00;-119;ponovno\n", map[string]string{"god": "2014"})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "mijenja se <strong>1</strong>") {
		t.Errorf("stari naziv stupca: %d", w.Code)
	}
}

// Ispravke arhive upisuje uprava organizacije; bez datoteke, godine ili s
// datotekom koja nije odavde vraća se na arhivu letve s razlogom.
func TestUvozIspravakaArhiveOdbijeniSlucajevi(t *testing.T) {
	p := posluziteljArhive(t)
	put := "/readings/station/" + p.letva.ID.String() + "/uvoz"
	natrag := "/readings/station/" + p.letva.ID.String()
	dobra := "vrijeme;ispravak;razlog\n2014-01-01 07:00:00;-119;predznak\n"
	for ime, slucaj := range map[string]struct {
		posalji func() interface{ Result() *http.Response }
		poruka  string
	}{
		"dežurni nije uprava": {func() interface{ Result() *http.Response } {
			return p.datoteka(t, "dežurni", put, "a.csv", dobra, map[string]string{"god": "2014"})
		}, "Ispravke arhive upisuje administrator"},
		"nije obrazac s datotekom": {func() interface{ Result() *http.Response } {
			return p.obrazac("uprava", put, url.Values{"god": {"2014"}})
		}, "Datoteka nije primljena: "},
		"bez datoteke": {func() interface{ Result() *http.Response } {
			return p.datoteka(t, "uprava", put, "", "", map[string]string{"god": "2014"})
		}, "Odaberi datoteku"},
		"bez godine": {func() interface{ Result() *http.Response } {
			return p.datoteka(t, "uprava", put, "a.csv", dobra, nil)
		}, "Godina nije zadana"},
		"bez stupca ispravka": {func() interface{ Result() *http.Response } {
			return p.datoteka(t, "uprava", put, "a.csv", "vrijeme;vodostaj_cm\n2014-01-01 07:00:00;-120\n", map[string]string{"god": "2014"})
		}, "datoteci nedostaje stupac „vrijeme“ ili „ispravak“ — je li izvezena odavde?"},
		"samo zaglavlje": {func() interface{ Result() *http.Response } {
			return p.datoteka(t, "uprava", put, "a.csv", "vrijeme;ispravak\n", map[string]string{"god": "2014"})
		}, "datoteka nema nijedan redak s podacima"},
		"nečitljiva datoteka": {func() interface{ Result() *http.Response } {
			return p.datoteka(t, "uprava", put, "a.csv", "vrijeme;ispravak\n\"2014;1\n", map[string]string{"god": "2014"})
		}, "datoteka nije čitljiva: "},
	} {
		res := slucaj.posalji().Result()
		u, err := url.Parse(res.Header.Get("Location"))
		if res.StatusCode != http.StatusSeeOther || err != nil {
			t.Errorf("%s: %d", ime, res.StatusCode)
			continue
		}
		if u.Path != natrag || u.Fragment != "arhiva" || !strings.HasPrefix(u.Query().Get("error"), slucaj.poruka) {
			t.Errorf("%s: %s#%s %q", ime, u.Path, u.Fragment, u.Query().Get("error"))
		}
	}
	if w := p.datoteka(t, "uprava", "/readings/station/"+uuid.NewString()+"/uvoz", "a.csv", dobra, map[string]string{"god": "2014"}); w.Code != http.StatusNotFound {
		t.Errorf("nepostojeća letva: %d", w.Code)
	}
}

// Potvrda upisuje samo retke s čitljivim trenutkom, brojem i razlogom, uz
// staru vrijednost kad je poznata i ime onoga tko je ispravio; arhiva ostaje
// netaknuta.
func TestPotvrdaIspravakaArhive(t *testing.T) {
	p := posluziteljArhive(t)
	put := "/readings/station/" + p.letva.ID.String() + "/uvoz/potvrdi"
	rfc := func(t time.Time) string { return t.Format(time.RFC3339) }
	forma := url.Values{
		"v": {"vodostaj"}, "korak": {"dnevni"},
		"kad":    {rfc(danArhive1), rfc(danArhive2), "1.1.2014.", rfc(danArhive3), rfc(danArhive3)},
		"novo":   {"-121", "-117,5", "1", "36", "deset"},
		"staro":  {"-120", "", "2", "35", "35"},
		"razlog": {"predznak s lista", " očitano s lista ", "nečitljivo vrijeme", "  ", "slovima"},
	}
	w := p.obrazac("uprava", put, forma)
	putanja, poruka := porukaPreusmjerenja(t, w, "success")
	if putanja != "/readings/station/"+p.letva.ID.String() ||
		poruka != "Upisano 2 ispravka. Arhiva je netaknuta — ispravci stoje uz nju i vide se u nizu." {
		t.Errorf("potvrda: %s %q", putanja, poruka)
	}
	ispravci, err := repository.NewIspravakRepository(p.baza, nil).ZaNiz(context.Background(), "primjerovo", "vodostaj", "dnevni",
		danArhive1.Add(-time.Hour), danArhive3.Add(time.Hour))
	if err != nil || len(ispravci) != 2 {
		t.Fatalf("ispravci: %d %v", len(ispravci), err)
	}
	prvi, drugi := ispravci[danArhive1.Unix()], ispravci[danArhive2.Unix()]
	if prvi.Vrijednost != -121 || prvi.Staro == nil || *prvi.Staro != -120 || prvi.Razlog != "predznak s lista" ||
		prvi.Ispravio != p.osobe["uprava"].ID.String() {
		t.Errorf("prvi ispravak: %+v", prvi)
	}
	if drugi.Vrijednost != -117.5 || drugi.Staro != nil || drugi.Razlog != "očitano s lista" {
		t.Errorf("drugi ispravak: %+v", drugi)
	}

	// ništa upotrebljivo: ništa se ne upisuje
	_, poruka = porukaPreusmjerenja(t, p.obrazac("uprava", put, url.Values{"kad": {"jučer"}, "novo": {"1"}, "razlog": {"x"}}), "error")
	if poruka != "Nijedan ispravak nije potvrđen" {
		t.Errorf("bez ispravaka: %q", poruka)
	}
	// dežurni ne upisuje ispravke arhive
	_, poruka = porukaPreusmjerenja(t, p.obrazac("dežurni", put, forma), "error")
	if poruka != "Ispravke arhive upisuje administrator" {
		t.Errorf("dežurni: %q", poruka)
	}
	if w := p.obrazac("uprava", "/readings/station/"+uuid.NewString()+"/uvoz/potvrdi", forma); w.Code != http.StatusNotFound {
		t.Errorf("nepostojeća letva: %d", w.Code)
	}
	// pohrana koja ne radi javlja grešku, ne uspjeh
	if _, err := p.baza.Exec(`DROP TABLE arhiva_ispravci`); err != nil {
		t.Fatal(err)
	}
	_, poruka = porukaPreusmjerenja(t, p.obrazac("uprava", put, forma), "error")
	if !strings.Contains(poruka, "arhiva_ispravci") {
		t.Errorf("pohrana ne radi: %q", poruka)
	}
}
