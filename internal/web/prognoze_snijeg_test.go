package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/poslovi"
	"gocop/internal/prognoza"
)

// Brojevi i osobe u ovim testovima su izmišljeni.

// probniUrednikSnijega je probni urednik koji zna i postavke snijega.
type probniUrednikSnijega struct {
	*probniUrednik
	postavke []prognoza.PostavkeSnijega
	radnje   []string
}

func (u *probniUrednikSnijega) SpremiSnijeg(p prognoza.PostavkeSnijega, tko string) error {
	u.postavke = append(u.postavke, p)
	u.radnje = append(u.radnje, "spremi "+tko)
	return u.greska
}
func (u *probniUrednikSnijega) ObrisiNacrtSnijega() error {
	u.radnje = append(u.radnje, "odbaci")
	return u.greska
}
func (u *probniUrednikSnijega) ProbajSnijeg(p *poslovi.Posao) error {
	u.pripremljeni <- "proba snijega"
	p.Zavrsi("Proba je gotova.", nil)
	return nil
}
func (u *probniUrednikSnijega) PrimijeniSnijeg(tko, razlog string) error {
	u.radnje = append(u.radnje, "primijeni "+tko+" "+razlog)
	return u.greska
}

// rukovateljSnijega ima bazu prognoza s izdanjem i urednika snijega na
// računalu koje izdaje. nacrt, kad nije nil, upisuje se u bazu.
func rukovateljSnijega(t *testing.T, nacrt *prognoza.NacrtSnijega) (*PrognozeHandler, *probniUrednikSnijega) {
	t.Helper()
	put := bazaPrognoza(t, time.Date(2026, 11, 25, 5, 0, 0, 0, time.UTC).Unix()/3600)
	if nacrt != nil {
		pb, err := prognoza.Otvori(put)
		if err != nil {
			t.Fatal(err)
		}
		sadrzaj, err := json.Marshal(nacrt)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pb.Exec(`INSERT INTO model_nacrt (letva, sadrzaj) VALUES ('postavke:snijeg', ?)`, string(sadrzaj)); err != nil {
			t.Fatal(err)
		}
		if err := pb.Close(); err != nil {
			t.Fatal(err)
		}
	}
	c, err := OtvoriPrognoze(put)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	h := NewPrognozeHandler(nil, nil, nil)
	h.citac = func() *CitacPrognoza { return c }
	h.SetPostavke(predlozakStranice(t, "prognoze_postavke.html"))
	h.SetPripremaModela(nil, nil, poslovi.NoviRegistar(), predlozakStranice(t, "posao.html"))
	u := &probniUrednikSnijega{probniUrednik: &probniUrednik{izdaje: true, pripremljeni: make(chan string, 2)}}
	h.urednik = u
	return h, u
}

func stranicaPostavki(t *testing.T, h *PrognozeHandler, perms *models.UserPermissions) string {
	t.Helper()
	w := httptest.NewRecorder()
	h.ShowPostavke(w, zahtjevOsobe(http.MethodGet, "/prognoze/postavke", "", perms))
	if w.Code != http.StatusOK {
		t.Fatalf("postavke: status %d %s", w.Code, w.Header().Get("Location"))
	}
	return w.Body.String()
}

// mjeraDana je šest dana s istim promašajem i dovoljno slučajeva.
func mjeraDana(promasaj float64) []prognoza.MjeraDana {
	var out []prognoza.MjeraDana
	for d := 1; d <= 6; d++ {
		out = append(out, prognoza.MjeraDana{Dan: d, Promasaj: promasaj * float64(d), Slucaja: 480})
	}
	return out
}

// Kartica „Model snijega” stoji iza kartice Model prognoze: bez nacrta
// pokazuje postavke iz programa, a mijenja ih samo globalni administrator na
// računalu koje izdaje; ostali vide polja zaključana i razlog.
func TestKarticaSnijegaBezNacrta(t *testing.T) {
	h, u := rukovateljSnijega(t, nil)
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	html := stranicaPostavki(t, h, admin)
	redom(t, html, `id="model"`, `id="snijeg"`, "Model snijega",
		"Vrijede postavke iz programa: topi se 3,0 mm vode po °C na dan iznad 0,0 °C.",
		`name="ukljucen" value="1" checked`, `name="topljenje" value="3"`, `name="prag" value="0"`,
		`value="spremi" class="btn prog-obrub">Spremi kao nacrt`, `value="probaj" class="btn btn-primary">Isprobaj na povijesti`, "Nacrt ne mijenja prognozu.")
	for _, nema := range []string{`id="snijeg-proba"`, `value="odbaci"`, `class="snijeg-polja" disabled`} {
		if strings.Contains(html, nema) {
			t.Errorf("bez nacrta kartica ne smije imati %q", nema)
		}
	}
	u.izdaje = false
	if html := stranicaPostavki(t, h, admin); !strings.Contains(html, `class="snijeg-polja" disabled`) || strings.Contains(html, `value="probaj"`) ||
		!strings.Contains(html, porukaNeIzdaje) {
		t.Error("na računalu koje ne izdaje postavke se samo gledaju")
	}
}

// S probanim nacrtom kartica pokazuje nacrt u obrascu, a ispod nje rezultat
// probe: po letvi i danu sadašnje → nacrt, ocjenu, razlog i primjenu. Proba
// prema drugim primijenjenim postavkama se ne primjenjuje.
func TestKarticaSnijegaSProbom(t *testing.T) {
	nove := prognoza.PostavkeSnijega{Ukljucen: true, Topljenje: 4.5, Prag: -0.5}
	proba := &prognoza.ProbaSnijega{Kad: "25. 11. 2026. 10:00", Primijenjene: prognoza.ZadanePostavkeSnijega,
		Razdoblje: []string{"2024-11-01", "2026-10-31"}, Natpisi: []string{prognoza.NatpisSnjezneZime},
		Letve: []prognoza.ProbaSnijegaLetve{
			{Letva: "botovo", Sada: mjeraDana(8), Nacrt: mjeraDana(7), Ocjena: prognoza.SnijegBolje},
			{Letva: "komarom", Sada: mjeraDana(9), Nacrt: mjeraDana(10), Ocjena: prognoza.SnijegLosije},
			{Letva: "mursko-sredisce", Ocjena: prognoza.MjeraNijeProvjereno, Greska: "letva nema dnevni niz"},
		}}
	h, _ := rukovateljSnijega(t, &prognoza.NacrtSnijega{Postavke: nove, Tko: "Iva Proba", Kad: "25. 11. 2026. 09:55", Proba: proba})
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	html := stranicaPostavki(t, h, admin)
	redom(t, html, `id="snijeg"`, `name="topljenje" value="4.5"`, `name="prag" value="-0.5"`, "Nacrt je spremio Iva Proba 25. 11. 2026. 09:55.",
		`id="snijeg-proba"`, "Rezultat probe · Model snijega", ">Nacrt</span>", "od 1.11.2024. do 31.10.2026., samo snježne zime",
		"botovo", "8,0 → 7,0", "48,0 → 42,0", "Bolje nego sada", "komarom", "Lošije nego sada", "mursko-sredisce", "Nije izmjereno: letva nema dnevni niz",
		prognoza.NatpisSnjezneZime, "Probano 25. 11. 2026. 10:00.", "Na 1 letvi nacrt je lošiji nego sada", `name="razlog"`,
		`value="primijeni" class="btn btn-primary">Primijeni`, `value="odbaci"`, "Promjena vrijedi tek nakon primjene; dotad prognoza računa s primijenjenim postavkama.")

	nikoga := stranicaPostavki(t, h, &models.UserPermissions{})
	if !strings.Contains(nikoga, "Rezultat probe") || strings.Contains(nikoga, `value="primijeni"`) || !strings.Contains(nikoga, porukaNijeAdmin) {
		t.Error("tko nije globalni administrator vidi probu, ali je ne primjenjuje")
	}

	proba.Primijenjene.Topljenje = 5
	h, _ = rukovateljSnijega(t, &prognoza.NacrtSnijega{Postavke: nove, Proba: proba})
	if html := stranicaPostavki(t, h, admin); !strings.Contains(html, "Primijenjene postavke promijenile su se otkad je nacrt proban") ||
		strings.Contains(html, `value="primijeni"`) {
		t.Error("zastarjela proba se ne primjenjuje")
	}
}

func posaljiSnijeg(h *PrognozeHandler, f url.Values, perms *models.UserPermissions) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.SpremiSnijeg(w, zahtjevOsobe(http.MethodPost, "/prognoze/postavke/snijeg", f.Encode(), perms))
	return w
}

func porukaSnijega(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	l, err := url.QueryUnescape(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// Radnje obrasca idu uredniku: spremanje s brojevima iz obrasca (i sa
// zarezom), proba kao posao nakon spremanja, primjena s razlogom i
// odbacivanje. Tko ne smije, i brojevi izvan granica, vraćaju se s porukom.
func TestRadnjeKarticeSnijega(t *testing.T) {
	h, u := rukovateljSnijega(t, nil)
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	polja := func(radnja string) url.Values {
		return url.Values{"radnja": {radnja}, "ukljucen": {"1"}, "topljenje": {"4,5"}, "prag": {"-1"}, "razlog": {" topi se brže "}}
	}
	if l := porukaSnijega(t, posaljiSnijeg(h, polja("spremi"), admin)); !strings.Contains(l, "#snijeg") || !strings.Contains(l, "Nacrt postavki snijega je spremljen") {
		t.Errorf("spremi: %q", l)
	}
	if len(u.postavke) != 1 || u.postavke[0] != (prognoza.PostavkeSnijega{Ukljucen: true, Topljenje: 4.5, Prag: -1}) {
		t.Fatalf("postavke iz obrasca: %+v", u.postavke)
	}
	bez := polja("spremi")
	bez.Del("ukljucen")
	posaljiSnijeg(h, bez, admin)
	if u.postavke[1].Ukljucen {
		t.Error("neoznačen prekidač isključuje model")
	}

	w := posaljiSnijeg(h, polja("probaj"), admin)
	select {
	case sto := <-u.pripremljeni:
		if sto != "proba snijega" || w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Proba postavki snijega") || len(u.postavke) != 3 {
			t.Errorf("proba: %q, status %d, spremljeno %d", sto, w.Code, len(u.postavke))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("posao probe nije pokrenut")
	}

	if l := porukaSnijega(t, posaljiSnijeg(h, polja("primijeni"), admin)); !strings.Contains(l, "Postavke snijega su primijenjene") {
		t.Errorf("primijeni: %q", l)
	}
	if l := porukaSnijega(t, posaljiSnijeg(h, polja("odbaci"), admin)); !strings.Contains(l, "Nacrt postavki snijega je odbačen") {
		t.Errorf("odbaci: %q", l)
	}
	if want := []string{"spremi Ana Anić", "spremi Ana Anić", "spremi Ana Anić", "primijeni Ana Anić topi se brže", "odbaci"}; strings.Join(u.radnje, "|") != strings.Join(want, "|") {
		t.Errorf("radnje %v, želim %v", u.radnje, want)
	}

	for _, s := range []struct {
		f     url.Values
		perms *models.UserPermissions
		zeli  string
	}{
		{url.Values{"radnja": {"spremi"}, "topljenje": {"12"}, "prag": {"0"}}, admin, "Brzina topljenja mora biti od 1 do 8"},
		{url.Values{"radnja": {"spremi"}, "topljenje": {"tri"}, "prag": {"0"}}, admin, "Brzina topljenja mora biti broj"},
		{url.Values{"radnja": {"spremi"}, "topljenje": {"3"}, "prag": {""}}, admin, "Temperatura topljenja mora biti broj"},
		{polja("spremi"), &models.UserPermissions{}, porukaNijeAdmin},
	} {
		if l := porukaSnijega(t, posaljiSnijeg(h, s.f, s.perms)); !strings.Contains(l, "error="+s.zeli) {
			t.Errorf("%v: %q, želim %q", s.f, l, s.zeli)
		}
	}
	h.urednik = u.probniUrednik // urednik bez postavki snijega
	if l := porukaSnijega(t, posaljiSnijeg(h, polja("spremi"), admin)); !strings.Contains(l, "Baza prognoza nije otvorena") {
		t.Errorf("bez urednika snijega: %q", l)
	}
}

// Rečenice kartice: primijenjene postavke s imenom i razlogom, isključen
// model, postavke koje se ne daju pročitati i dan bez slučajeva.
func TestReceniceKarticeSnijega(t *testing.T) {
	z := prognoza.ZapisSnijega{Postavke: prognoza.PostavkeSnijega{Ukljucen: true, Topljenje: 4, Prag: -0.5},
		Promjene: []prognoza.PromjenaLetve{{Kad: "25. 11. 2026. 10:00", Tko: "Iva Proba", Razlog: "brže se topi"}}}
	for _, s := range []struct {
		z    prognoza.ZapisSnijega
		ima  bool
		err  error
		zeli string
	}{
		{z, true, nil, "Primijenjeno 25. 11. 2026. 10:00 · Iva Proba · topi se 4,0 mm vode po °C na dan iznad −0,5 °C · „brže se topi”"},
		{prognoza.ZapisSnijega{Postavke: prognoza.PostavkeSnijega{Topljenje: 3}, Promjene: []prognoza.PromjenaLetve{{Kad: "1. 12. 2026.", Razlog: "ljeto"}}}, true, nil,
			"Primijenjeno 1. 12. 2026. · model snijega isključen, prognoza dobiva oborinu kakva je pala · „ljeto”"},
		{prognoza.ZapisSnijega{Postavke: prognoza.ZadanePostavkeSnijega}, false, errors.New("pokvareno"),
			"Primijenjene postavke se ne daju pročitati (pokvareno), pa vrijede postavke iz programa: topi se 3,0"},
	} {
		if got := stanjeSnijega(s.z, s.ima, s.err); !strings.HasPrefix(got, s.zeli) {
			t.Errorf("%q, želim %q", got, s.zeli)
		}
	}
	if got := celijaProbeSnijega(prognoza.MjeraDana{Slucaja: 0}, prognoza.MjeraDana{Slucaja: 5}); got != "—" {
		t.Errorf("dan bez slučajeva %q", got)
	}
	if got := opisNacrtaSnijega(prognoza.NacrtSnijega{}); !strings.HasPrefix(got, "Nacrt je spremljen. Dok se ne primijeni") {
		t.Errorf("nacrt bez imena %q", got)
	}
}

// Kartica bez baze prognoza ne postoji, a baza koja se ne da čitati kaže to
// umjesto postavki. Urednika snijega stranica nalazi i kroz poslužitelj.
func TestKarticaSnijegaBezBaze(t *testing.T) {
	h, u := rukovateljSnijega(t, nil)
	r := zahtjevOsobe(http.MethodGet, "/prognoze/postavke", "", &models.UserPermissions{IsGlobalAdmin: true})
	c := h.citac()
	if err := c.db.Close(); err != nil {
		t.Fatal(err)
	}
	if k := h.karticaSnijega(r); k == nil || !strings.Contains(k.Stanje, "ne daju pročitati") || !strings.Contains(k.Nacrt, "Nacrt se ne da pročitati") {
		t.Errorf("baza koja se ne da čitati: %+v", k)
	}
	h.citac = func() *CitacPrognoza { return nil }
	if h.karticaSnijega(r) != nil {
		t.Error("bez baze prognoza nema kartice")
	}
	h.citac = nil
	if h.karticaSnijega(r) != nil {
		t.Error("bez čitača nema kartice")
	}
	h.urednik = urednikIliNista(&Server{urednikModela: u})
	if got, err := h.urednikSnijega(); err != nil || got != UrednikSnijega(u) {
		t.Errorf("urednik kroz poslužitelj: %v, %v", got, err)
	}
}
