package web

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/arhiva"
	"gocop/internal/models"
	"gocop/internal/prognoza"
	"gocop/internal/repository"
)

// Letve i vodotoci u ovim testovima su izmišljeni.

// Što letvi fali: bez sati, malo sati, stara arhiva ili spremno. Malo sati je
// važnije od stare arhive.
func TestStaDaFali(t *testing.T) {
	sada := time.Date(2026, 11, 21, 3, 0, 0, 0, time.UTC)
	for _, s := range []struct {
		sati int
		kraj time.Time
		want StanjeModela
	}{
		{0, time.Time{}, oznakaNemaSati},
		{999, sada.AddDate(0, 0, -20), oznakaMaloSati},
		{800, sada.AddDate(-2, 0, 0), oznakaMaloSati},
		{1000, sada.AddDate(0, 0, -20), oznakaSpremno}, // toliko traži i priprema računa
		{9800, sada.AddDate(0, 0, -366), oznakaStara},
		{9800, sada.AddDate(0, -6, 0), oznakaSpremno}, // izvoz iz HIS-2000 kasni pola godine
		{18400, sada.AddDate(0, 0, -20), oznakaSpremno},
	} {
		if got := staDaFali(s.sati, s.kraj, sada); got != s.want {
			t.Errorf("%d sati do %v: %v, želim %v", s.sati, s.kraj, got, s.want)
		}
	}
}

// Stanje vodotoka po letvama: neprovjeren zapis u izdanju, pa izdanje, pa
// račun bez izdanja, inače nema prognoze.
func TestStanjeVodotokaModela(t *testing.T) {
	s := stanjaModela{
		izdane:       map[string]bool{"a": true, "b": true},
		racun:        map[string]bool{"a": true, "b": true, "c": true},
		neprovjerene: map[string]bool{"b": true, "d": true},
	}
	for letve, want := range map[string]StanjeModela{
		"a": oznakaRadi, "a,b": oznakaNeprovjeren, "c": oznakaStoji, "c,d": oznakaStoji, "e": oznakaNema, "": oznakaNema,
	} {
		if got := s.stanje(strings.Split(letve, ",")); got != want {
			t.Errorf("%q: %v, želim %v", letve, got, want)
		}
	}
}

// Letve na vodi: od većeg riječnog kilometra, letve bez kilometra na kraj po
// imenu, a voda prije svega.
func TestUsporediNaVodi(t *testing.T) {
	st := func(voda, rkm, ime string) models.Station {
		return models.Station{Watercourse: voda, Stationing: rkm, Name: ime}
	}
	gornja, donja := st("Lug", "rkm 50+000", "Gornja"), st("Lug", "rkm 10+000", "Donja")
	bez, bez2 := st("Lug", "", "Bez"), st("Lug", "", "Cesta")
	for _, s := range []struct {
		a, b models.Station
		want int
	}{
		{gornja, donja, -1}, {donja, gornja, 1}, {donja, bez, -1}, {bez, gornja, 1},
		{bez, bez2, -1}, {gornja, gornja, 0}, {st("Alfa", "", ""), gornja, -1},
	} {
		if got := usporediNaVodi(s.a, s.b); got != s.want {
			t.Errorf("%s/%s prema %s/%s: %d, želim %d", s.a.Watercourse, s.a.Name, s.b.Watercourse, s.b.Name, got, s.want)
		}
	}
}

// arhivaSatnihModela ima satne vodostaje letvi, svaki do sada.
func arhivaSatnihModela(t *testing.T, sati map[string]int, sada time.Time) *repository.ArhivaRepository {
	t.Helper()
	put := filepath.Join(t.TempDir(), "arhiva.db")
	a, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	if err := arhiva.PripremiPraznu(a); err != nil {
		t.Fatal(err)
	}
	kraj := sada.Truncate(time.Hour).Unix()
	for letva, n := range sati {
		for i := range n {
			if _, err := a.Exec(`INSERT INTO spoj (letva, velicina, korak, vrijeme, vrijednost, izvor, vrsta, tocnost)
				VALUES (?, 'vodostaj', 'satni', ?, 100, 'cop', 'trenutna', 2)`, letva, kraj-int64(i)*3600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	arh, err := repository.OpenArhiva(put)
	if err != nil || arh == nil {
		t.Fatalf("arhiva: %v", err)
	}
	t.Cleanup(func() { _ = arh.Close() }) // samo čitanje
	return arh
}

func stranicaModela(t *testing.T, h *PrognozeHandler, upit string, perms *models.UserPermissions) string {
	t.Helper()
	w := httptest.NewRecorder()
	h.ShowModel(w, zahtjevOsobe(http.MethodGet, "/prognoze/model"+upit, "", perms))
	if w.Code != http.StatusOK {
		t.Fatalf("%s: status %d", upit, w.Code)
	}
	return w.Body.String()
}

// Stranica po crtežu: sektor, vodotoci sa stanjem (oni s prognozom prvi) i
// za odabrani vodotok što letvama fali, s poveznicom na karticu i, za
// administratora, na uvoz. Brana nije letva modela.
func TestStranicaModelaPrognoze(t *testing.T) {
	h := rukovateljSektora(t)
	h.SetModel(predlozakStranice(t, "prognoze_model.html"))
	ctx := context.Background()
	for _, st := range []*models.Station{
		{ID: uuid.New(), Code: "potok-gornji", Name: "Gornji Lug", Watercourse: "Potok Lug", Stationing: "rkm 5+000", PrateSektori: []string{"B"}},
		{ID: uuid.New(), Code: "brana-lug", Name: "Brana Lug", Watercourse: "Potok Lug", PrateSektori: []string{"B"}},
	} {
		if err := h.stations.CreateStation(ctx, &models.UserPermissions{IsGlobalAdmin: true}, st, ""); err != nil {
			t.Fatal(err)
		}
	}
	a := arhivaSatnihModela(t, map[string]int{"donji-miholjac": 2500, "potok-gornji": 100}, time.Now())
	h.SetArhiva(func() *repository.ArhivaRepository { return a })
	admin := &models.UserPermissions{IsGlobalAdmin: true}

	html := stranicaModela(t, h, "?sektor=B&vodotok="+oznakaImena("Brodska rijeka"), admin)
	redom(t, html, `href="/prognoze/postavke" class="povratna-poveznica"`, "Model prognoze · Sektor B — Dunav i donja Drava",
		`aria-current="true">B · Dunav i donja Drava</a>`, "Vodotoci sektora B",
		`<span>Brodska rijeka</span><span class="rac-cip dobro">Radi</span>`,
		`<span>Potok Lug</span><span class="rac-cip">Još nema prognoze</span>`,
		`<h2 id="mod-odabran-naslov">Brodska rijeka</h2>`, "Sektor B · 1 letva", "Što letvama fali",
		"Donji Brod", "2.500", `<span class="rac-cip dobro">Spremno</span>`, `href="/administracija/uvoz-niza?letva=donji-miholjac">Uvoz</a>`,
		`href="/prognoze/model/racun?sektor=B&amp;vodotok=brodska-rijeka" class="btn btn-primary">Računi letvi</a>`)
	if strings.Contains(html, "Brana Lug") || !strings.Contains(html, `class="mod-mreza ima-odabran"`) {
		t.Error("brana je na popisu ili odabrani vodotok nije označen")
	}

	html = stranicaModela(t, h, "?sektor=B&vodotok="+oznakaImena("Potok Lug"), &models.UserPermissions{})
	redom(t, html, "Gornji Lug", "100", `<span class="rac-cip pozor">Malo sati</span>`)
	if strings.Contains(html, ">Uvoz</a>") || strings.Contains(html, "Brana Lug") {
		t.Error("uvoz vidi samo globalni administrator, a brana nije letva modela")
	}

	html = stranicaModela(t, h, "", admin)
	if !strings.Contains(html, `<h2 id="mod-odabran-naslov">Odaberi vodotok</h2>`) {
		t.Error("bez odabranog vodotoka stranica traži da se odabere")
	}

	h.SetArhiva(nil)
	html = stranicaModela(t, h, "?sektor=B&vodotok="+oznakaImena("Potok Lug"), admin)
	if !strings.Contains(html, "Arhiva nije na ovom računalu") {
		t.Error("bez arhive")
	}
}

// Bez predloška stranice nema, a bez registra sektora stranica kaže zašto.
func TestStranicaModelaBezRegistra(t *testing.T) {
	w := httptest.NewRecorder()
	(&PrognozeHandler{}).ShowModel(w, zahtjevOsobe(http.MethodGet, "/prognoze/model", "", &models.UserPermissions{}))
	if w.Code != http.StatusNotFound {
		t.Errorf("bez predloška: %d", w.Code)
	}
	h := &PrognozeHandler{}
	h.SetModel(predlozakStranice(t, "prognoze_model.html"))
	if html := stranicaModela(t, h, "", &models.UserPermissions{}); !strings.Contains(html, "Registar nema sektora") {
		t.Error("bez registra")
	}
}

// Dugi popis: vodotoci s prognozom i prvih dvanaest uvijek se vide, ostali su
// pod „Prikaži svih”, otvoreno kad je odabran jedan od njih.
func TestDugiPopisVodotoka(t *testing.T) {
	s := SektorPrognoze{ID: "B", Ime: "Dunav i donja Drava"}
	for i := range 15 {
		s.Vodotoci = append(s.Vodotoci, VodotokPrognoze{Oznaka: fmt.Sprintf("v%02d", i), Ime: fmt.Sprintf("Vodotok %02d", i),
			Letve: []string{fmt.Sprintf("l%02d", i)}})
	}
	st := stanjaModela{izdane: map[string]bool{"l14": true}}
	for upit, otvoreno := range map[string]bool{"?sektor=B&vodotok=v13": true, "?sektor=B&vodotok=v02": false} {
		data := ModelPrognozePageData{}
		data.Permissions = &models.UserPermissions{}
		r := httptest.NewRequest(http.MethodGet, "/prognoze/model"+upit, nil)
		(&PrognozeHandler{}).odaberiModel(r, &data, []SektorPrognoze{s}, st)
		if len(data.Vodotoci) != najviseVidljivih || len(data.Skriveni) != 3 || data.OtvoriSve != otvoreno ||
			data.Vodotoci[0].Ime != "Vodotok 14" || data.Odabran == nil {
			t.Errorf("%s: %d vidljivih, %d skrivenih, otvoreno %v, prvi %q", upit, len(data.Vodotoci), len(data.Skriveni),
				data.OtvoriSve, data.Vodotoci[0].Ime)
		}
	}
}

// Stanje modela iz baze prognoza: zapis koji proba još nije mjerila je
// neprovjeren, a bez baze ili sa zatvorenom bazom zapisa nema.
func TestStanjaModelaIzBaze(t *testing.T) {
	if (*CitacPrognoza)(nil).ZapisiModela() != nil {
		t.Error("bez čitača")
	}
	if s := (&PrognozeHandler{}).stanjaModela(); len(s.izdane)+len(s.racun)+len(s.neprovjerene) != 0 {
		t.Errorf("bez baze prognoza: %+v", s)
	}
	put := filepath.Join(t.TempDir(), "prognoze.db")
	pb, err := prognoza.Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	z := prognoza.ZapisLetve{Oblik: prognoza.OblikModelaLetve, Letva: "potok-dole", Metoda: prognoza.MetodaLanac, Velicina: "vodostaj",
		Racun: prognoza.RacunLetve{Ulazi: []string{"potok-vrh"}, Doseg: 12},
		Brojevi: prognoza.BrojeviLetve{Inacice: [][]prognoza.Pojas{{{Letva: "potok-dole", Velicina: "vodostaj", Od: -1000, Do: 1000, Rasap: 3,
			Ulazi: []prognoza.Ulaz{{Letva: "potok-vrh", Velicina: "vodostaj", PomakH: 2, Sirina: 1, Nagib: 1}}}}}},
		Promjene: []prognoza.PromjenaLetve{{Kad: "2026-11-21 03:00", Tko: "Iva Proba", Razlog: "proba"}}}
	if err := prognoza.SpremiZapisLetve(pb, z, "", prognoza.OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	if err := pb.Close(); err != nil {
		t.Fatal(err)
	}
	c, err := OtvoriPrognoze(put)
	if err != nil {
		t.Fatal(err)
	}
	h := &PrognozeHandler{citac: func() *CitacPrognoza { return c }}
	if s := h.stanjaModela(); !s.racun["potok-dole"] || !s.neprovjerene["potok-dole"] {
		t.Errorf("zapis bez probe: %+v", s)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if c.ZapisiModela() != nil {
		t.Error("zatvorena baza prognoza")
	}
}

// Letva s dnevnim modelom iz zapisa nije „neprovjerena”: brojeva u zapisu nema, a
// bez probe se model ne da primijeniti.
func TestDnevniZapisNijeNeprovjeren(t *testing.T) {
	put := filepath.Join(t.TempDir(), "prognoze.db")
	pb, err := prognoza.Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	z := prognoza.ZapisLetve{Oblik: prognoza.OblikModelaLetve, Letva: "potok-dole", Metoda: prognoza.MetodaDnevni, Velicina: "vodostaj",
		Dnevni: []byte(`{"ulazi":["potok-vrh"],"slivovi":[]}`)}
	if err := prognoza.SpremiZapisLetve(pb, z, "", prognoza.OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	// redak letve glavnog lanca kaže samo kako se račun namješta
	if _, err := pb.Exec(`INSERT INTO model_letve (letva, proba, sadrzaj) VALUES ('osijek', '',
		'{"oblik":1,"letva":"osijek","metoda":"lanac","velicina":"vodostaj","racun":{"ulazi":["belisce"],"doseg":96}}')`); err != nil {
		t.Fatal(err)
	}
	if err := pb.Close(); err != nil {
		t.Fatal(err)
	}
	c, err := OtvoriPrognoze(put)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	h := &PrognozeHandler{citac: func() *CitacPrognoza { return c }}
	if s := h.stanjaModela(); !s.racun["potok-dole"] || s.neprovjerene["potok-dole"] {
		t.Errorf("zapis s dnevnim modelom: %+v", s)
	}
	if s := h.stanjaModela(); !s.racun["osijek"] || s.neprovjerene["osijek"] {
		t.Errorf("zapis letve glavnog lanca: %+v", s)
	}
}
