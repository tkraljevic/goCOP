package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/prognoza"
)

// Letve, vodotoci i osobe u ovim testovima su izmišljeni.

// probniUrednik pamti što mu je stranica predala.
type probniUrednik struct {
	izdaje     bool
	spremljeni []prognoza.NacrtLetve
	obrisani   []string
	greska     error
}

func (u *probniUrednik) Izdaje() bool { return u.izdaje }
func (u *probniUrednik) SpremiNacrt(n prognoza.NacrtLetve) error {
	u.spremljeni = append(u.spremljeni, n)
	return u.greska
}
func (u *probniUrednik) ObrisiNacrt(letva string) error {
	u.obrisani = append(u.obrisani, letva)
	return u.greska
}

// rukovateljRacuna ima potok s tri letve u sektoru B i bazu prognoza u kojoj je
// donja letva u glavnom lancu, a srednja ima nacrt.
func rukovateljRacuna(t *testing.T) (*PrognozeHandler, *probniUrednik) {
	t.Helper()
	h := rukovateljSektora(t)
	ctx := context.Background()
	for _, l := range []struct{ kod, ime, rkm string }{
		{"potok-gornji", "Gornji Lug", "rkm 9+000"}, {"potok-srednji", "Srednji Lug", "rkm 5+000"}, {"potok-donji", "Donji Lug", "rkm 1+000"}} {
		st := &models.Station{ID: uuid.New(), Code: l.kod, Name: l.ime, Watercourse: "Potok Lug", Stationing: l.rkm, PrateSektori: []string{"B"}}
		if err := h.stations.CreateStation(ctx, &models.UserPermissions{IsGlobalAdmin: true}, st, ""); err != nil {
			t.Fatal(err)
		}
	}
	put := filepath.Join(t.TempDir(), "prognoze.db")
	pb, err := prognoza.Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	if err := prognoza.Spremi(pb, []prognoza.Pojas{{Letva: "potok-donji", Velicina: "vodostaj", Od: -1000, Do: 1000, Sati: 5000,
		Ulazi: []prognoza.Ulaz{{Letva: "donji-miholjac", Velicina: "vodostaj", PomakH: 3, Sirina: 1, Nagib: 1}}}}, "proba"); err != nil {
		t.Fatal(err)
	}
	nacrt := prognoza.NacrtLetve{Tko: "Iva Proba", Kad: "21. 11. 2026. 20:00", Zapis: prognoza.ZapisLetve{Oblik: prognoza.OblikModelaLetve,
		Letva: "potok-srednji", Metoda: prognoza.MetodaLanac, Velicina: "vodostaj", Racun: prognoza.RacunLetve{Ulazi: []string{"potok-gornji"},
			Rezerve: [][]string{{"donji-miholjac"}}, Razine: prognoza.RazineLetve{Postoci: []float64{0, 40, 80, 100}}}}}
	if err := prognoza.SpremiNacrt(pb, nacrt, prognoza.OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	if err := pb.Close(); err != nil {
		t.Fatal(err)
	}
	c, err := OtvoriPrognoze(put)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	h.citac = func() *CitacPrognoza { return c }
	u := &probniUrednik{izdaje: true}
	h.SetUrednikModela(u, predlozakStranice(t, "prognoze_racun.html"))
	return h, u
}

func stranicaRacuna(t *testing.T, h *PrognozeHandler, upit string, perms *models.UserPermissions) string {
	t.Helper()
	w := httptest.NewRecorder()
	h.ShowRacun(w, zahtjevOsobe(http.MethodGet, "/prognoze/model/racun"+upit, "", perms))
	if w.Code != http.StatusOK {
		t.Fatalf("%s: status %d", upit, w.Code)
	}
	return w.Body.String()
}

// Stranica po crtežu: letve redom kako voda teče, uz svaku što je složeno, i
// obrazac odabrane letve. Nacrt se vidi s imenom i vremenom, letva glavnog lanca
// nema obrazac, a letva bez računa ima prazan.
func TestStranicaSlaganjaRacuna(t *testing.T) {
	h, _ := rukovateljRacuna(t)
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	vodotok := "?sektor=B&vodotok=" + oznakaImena("Potok Lug")

	html := stranicaRacuna(t, h, vodotok+"&letva=potok-srednji", admin)
	redom(t, html, "← Model prognoze · Sektor B", "Model · Potok Lug", "Letve redom",
		"<span>Gornji Lug</span>", `<span class="rac-cip">Još nema račun</span>`,
		"<span>Srednji Lug</span>", "vodostaj · ulaz: Gornji Lug · rezerva: Donji Brod", `<span class="rac-cip pozor">Nacrt</span>`,
		"<span>Donji Lug</span>", "računa se u glavnom lancu", `<span class="rac-cip">Glavni lanac</span>`,
		`<h2 id="rac-obrazac-naslov">Srednji Lug</h2>`, "nacrt spremljen 21. 11. 2026. 20:00, Iva Proba",
		`name="letva" value="potok-srednji"`, `<optgroup label="Potok Lug">`,
		`<option value="potok-gornji" selected>Gornji Lug</option>`, `<optgroup label="Brodska rijeka">`, "Drugi ulaz", "— bez drugog ulaza —",
		`<option value="donji-miholjac" selected>Donji Brod</option>`,
		`name="razine_nacin" value="postoci" checked`, `name="razine" class="form-control" value="40, 80"`,
		`value="spremi" class="btn btn-primary">Spremi nacrt`, `value="odbaci"`)
	if strings.Contains(html, `<option value="potok-srednji"`) || strings.Contains(html, "disabled") {
		t.Error("letva je sama sebi u izboru ili je obrazac administratoru zaključan")
	}

	html = stranicaRacuna(t, h, vodotok+"&letva=potok-donji", admin)
	if !strings.Contains(html, porukaULancu) || strings.Contains(html, `class="rac-obrazac"`) {
		t.Error("letva glavnog lanca ne smije imati obrazac")
	}

	html = stranicaRacuna(t, h, vodotok, admin)
	redom(t, html, `<h2 id="rac-obrazac-naslov">Gornji Lug</h2>`, `name="velicina" value="vodostaj" checked`,
		`name="razine_nacin" value="zadano" checked`, "Spremi nacrt")
	if strings.Contains(html, `value="odbaci"`) {
		t.Error("bez nacrta nema što odbaciti")
	}
}

// Tko ne smije slagati vidi isto, ali bez gumba i sa zaključanim poljima, uz
// razlog: nije administrator, gleda tuđim očima ili računalo ne izdaje.
func TestSlaganjeRacunaSamoCitanje(t *testing.T) {
	h, u := rukovateljRacuna(t)
	upit := "?sektor=B&vodotok=" + oznakaImena("Potok Lug") + "&letva=potok-srednji"
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	provjeri := func(ime, html, razlog string) {
		t.Helper()
		if !strings.Contains(html, razlog) || !strings.Contains(html, "disabled") || strings.Contains(html, "Spremi nacrt") {
			t.Errorf("%s: stranica mora biti samo za čitanje, uz razlog", ime)
		}
	}
	provjeri("korisnik", stranicaRacuna(t, h, upit, &models.UserPermissions{}), porukaNijeAdmin)
	u.izdaje = false
	provjeri("ne izdaje", stranicaRacuna(t, h, upit, admin), porukaNeIzdaje)
	u.izdaje = true
	r := zahtjevOsobe(http.MethodGet, "/prognoze/model/racun"+upit, "", admin)
	w := httptest.NewRecorder()
	h.ShowRacun(w, r.WithContext(context.WithValue(r.Context(), contextKeyViewing, true)))
	provjeri("tuđim očima", w.Body.String(), porukaTudjeOci)
}

// Bez predloška stranice nema, a nepoznat vodotok vodi na izbor vodotoka.
func TestSlaganjeRacunaBezVodotoka(t *testing.T) {
	w := httptest.NewRecorder()
	(&PrognozeHandler{}).ShowRacun(w, zahtjevOsobe(http.MethodGet, "/prognoze/model/racun", "", &models.UserPermissions{}))
	if w.Code != http.StatusNotFound {
		t.Errorf("bez predloška: %d", w.Code)
	}
	h, _ := rukovateljRacuna(t)
	for _, upit := range []string{"", "?sektor=B&vodotok=nema-ga", "?sektor=Z&vodotok=" + oznakaImena("Potok Lug")} {
		w := httptest.NewRecorder()
		h.ShowRacun(w, zahtjevOsobe(http.MethodGet, "/prognoze/model/racun"+upit, "", &models.UserPermissions{}))
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/prognoze/model" {
			t.Errorf("%q: %d %s", upit, w.Code, w.Header().Get("Location"))
		}
	}
}

func posaljiRacun(h *PrognozeHandler, f url.Values, perms *models.UserPermissions, tudjimOcima bool) string {
	r := zahtjevOsobe(http.MethodPost, "/prognoze/model/racun", f.Encode(), perms)
	if tudjimOcima {
		r = r.WithContext(context.WithValue(r.Context(), contextKeyViewing, true))
	}
	w := httptest.NewRecorder()
	h.SpremiRacun(w, r)
	lokacija, err := url.QueryUnescape(w.Header().Get("Location"))
	if err != nil {
		return "adresa se ne da pročitati: " + err.Error()
	}
	return lokacija
}

// Spremanje slaže zapis iz obrasca i predaje ga uredniku s imenom osobe. Ručne
// vrijednosti staroga nacrta ostaju samo dok se razine ne promijene.
func TestSpremiNacrtRacuna(t *testing.T) {
	h, u := rukovateljRacuna(t)
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	f := url.Values{"sektor": {"B"}, "vodotok": {oznakaImena("Potok Lug")}, "letva": {"potok-gornji"}, "radnja": {"spremi"},
		"velicina": {"protok"}, "ulaz": {" donji-miholjac ", "", "potok-donji"}, "rezerva": {"", "belisce"}, "razine_nacin": {"granice"}, "razine": {"40; 90 150"}}
	lok := posaljiRacun(h, f, admin, false)
	if !strings.Contains(lok, "letva=potok-gornji") || !strings.Contains(lok, "Nacrt za letvu Gornji Lug je spremljen") {
		t.Errorf("odgovor %q", lok)
	}
	if len(u.spremljeni) != 1 {
		t.Fatalf("spremljeno %d nacrta", len(u.spremljeni))
	}
	n := u.spremljeni[0]
	z := n.Zapis
	if n.Tko != "Ana Anić" || n.Kad == "" || z.Letva != "potok-gornji" || z.Oblik != prognoza.OblikModelaLetve || z.Metoda != prognoza.MetodaLanac ||
		z.Velicina != "protok" || !slices.Equal(z.Racun.Ulazi, []string{"donji-miholjac", "potok-donji"}) || len(z.Racun.Rezerve) != 1 ||
		z.Racun.Rezerve[0][0] != "belisce" || !slices.Equal(z.Racun.Razine.Granice, []float64{40, 90, 150}) || z.Racun.Razine.Postoci != nil {
		t.Errorf("zapis iz obrasca %+v", n)
	}

	nagib := 1.5
	stari := prognoza.ZapisLetve{Racun: prognoza.RacunLetve{Razine: prognoza.RazineLetve{Postoci: []float64{0, 40, 80, 100}, Iskljucene: []int{2}},
		Rucno: prognoza.RucnoLetve{Nagib: &nagib}}}
	isti, err := zapisIzObrasca(url.Values{"razine_nacin": {"postoci"}, "razine": {"40, 80"}}, "potok-srednji", stari)
	if err != nil || !slices.Equal(isti.Racun.Razine.Iskljucene, []int{2}) || isti.Racun.Rucno.Nagib == nil {
		t.Errorf("iste razine moraju zadržati ručno i isključeno: %+v, %v", isti.Racun, err)
	}
	drugi, err := zapisIzObrasca(url.Values{"razine_nacin": {"postoci"}, "razine": {"50"}}, "potok-srednji", stari)
	if err != nil || drugi.Racun.Razine.Iskljucene != nil || drugi.Racun.Rucno.Nagib != nil ||
		!slices.Equal(drugi.Racun.Razine.Postoci, []float64{0, 50, 100}) {
		t.Errorf("nove razine brišu ručno i isključeno: %+v, %v", drugi.Racun, err)
	}
}

// Odbijanja: tko ne smije, obrazac koji se ne da pročitati, granice koje nisu
// brojevi i greška provjere iz urednika. Odbaci predaje letvu uredniku.
func TestSpremiNacrtRacunaOdbijanja(t *testing.T) {
	h, u := rukovateljRacuna(t)
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	f := url.Values{"sektor": {"B"}, "vodotok": {oznakaImena("Potok Lug")}, "letva": {"potok-gornji"}, "radnja": {"spremi"},
		"velicina": {"vodostaj"}, "ulaz": {"donji-miholjac"}}
	if lok := posaljiRacun(h, f, &models.UserPermissions{}, false); !strings.Contains(lok, porukaNijeAdmin) {
		t.Errorf("korisnik: %q", lok)
	}
	if lok := posaljiRacun(h, f, admin, true); !strings.Contains(lok, porukaTudjeOci) {
		t.Errorf("tuđim očima: %q", lok)
	}
	u.izdaje = false
	if lok := posaljiRacun(h, f, admin, false); !strings.Contains(lok, porukaNeIzdaje) {
		t.Errorf("ne izdaje: %q", lok)
	}
	u.izdaje = true
	if len(u.spremljeni) != 0 {
		t.Fatalf("odbijeni zahtjevi su spremili nacrt: %v", u.spremljeni)
	}
	for nacin, ocekujem := range map[string]string{"granice": "nije broj", "postoci": "nije broj"} {
		f.Set("razine_nacin", nacin)
		f.Set("razine", "40, visoko")
		if lok := posaljiRacun(h, f, admin, false); !strings.Contains(lok, ocekujem) {
			t.Errorf("%s: %q", nacin, lok)
		}
	}
	f.Set("razine", "")
	f.Set("razine_nacin", "granice")
	if lok := posaljiRacun(h, f, admin, false); !strings.Contains(lok, "Upiši barem jednu granicu") {
		t.Errorf("granice bez brojeva: %q", lok)
	}
	f.Set("razine_nacin", "zadano")
	u.greska = errors.New("letva nema sektor; upiši ga u obrascu letve")
	if lok := posaljiRacun(h, f, admin, false); !strings.Contains(lok, "error=Letva nema sektor; upiši ga u obrascu letve.") {
		t.Errorf("greška provjere: %q", lok)
	}
	u.greska = nil
	f.Set("radnja", "odbaci")
	if lok := posaljiRacun(h, f, admin, false); !strings.Contains(lok, "Nacrt za letvu Gornji Lug je odbačen.") || !slices.Equal(u.obrisani, []string{"potok-gornji"}) {
		t.Errorf("odbaci: %q, %v", lok, u.obrisani)
	}
	r := httptest.NewRequest(http.MethodPost, "/prognoze/model/racun", strings.NewReader("%zz"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.SpremiRacun(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("pokvaren obrazac: %d", w.Code)
	}
}

// Razine u obrascu i u zapisu: postoci u zapisu nose krajeve 0 i 100, a u
// obrascu se vide samo granice između razina.
func TestRazineObrasca(t *testing.T) {
	for _, c := range []struct {
		razine         prognoza.RazineLetve
		nacin, granice string
	}{
		{prognoza.RazineLetve{}, razineZadane, ""},
		{prognoza.RazineLetve{Granice: []float64{40, 90.5}}, razineGranice, "40, 90.5"},
		{prognoza.RazineLetve{Postoci: []float64{0, 50, 100}}, razinePostoci, "50"},
		{prognoza.RazineLetve{Postoci: []float64{0, 100}}, razinePostoci, ""},
	} {
		nacin, granice := razineUObrascu(c.razine)
		if nacin != c.nacin || granice != c.granice {
			t.Errorf("%+v: %q %q", c.razine, nacin, granice)
		}
		natrag, err := razineIzObrasca(nacin, granice)
		if err != nil || !slices.Equal(natrag.Postoci, c.razine.Postoci) || !slices.Equal(natrag.Granice, c.razine.Granice) {
			t.Errorf("%+v natrag: %+v, %v", c.razine, natrag, err)
		}
	}
	if velikoPrvo("") != "" || velikoPrvo("čvor") != "Čvor" {
		t.Error("prvo slovo")
	}
}

// Sitnice stranice: odabrana letva, ime letve koje nema u registru, opis i
// obrazac primijenjenog modela, izbor bez letvi i urednik bez baze prognoza.
func TestSitniceSlaganjaRacuna(t *testing.T) {
	if odabranaLetva(nil, "x") != "" || odabranaLetva([]string{"a", "b"}, "b") != "b" || odabranaLetva([]string{"a", "b"}, "x") != "a" {
		t.Error("odabrana letva")
	}
	if imeLetve(nil, "potok-x") != "potok-x" {
		t.Error("letva izvan registra nosi šifru")
	}
	rm := racuniModela{zapisi: map[string]prognoza.ZapisLetve{"a": {Velicina: "protok", Racun: prognoza.RacunLetve{Ulazi: []string{"b"},
		Rezerve: [][]string{{"c"}, {"d"}, {"e"}}, Razine: prognoza.RazineLetve{Granice: []float64{10}}}}}, glavni: map[string]bool{}}
	if s := rm.stanje("a"); s != oznakaModel {
		t.Errorf("stanje %v", s)
	}
	if opis := rm.opis("a", nil); opis != "protok · ulaz: b · rezerva: c · rezerva: d · rezerva: e" {
		t.Errorf("opis %q", opis)
	}
	if o := rm.obrazac("a", "A"); o.Velicina != "protok" || !slices.Equal(o.Ulazi, []string{"b", ""}) || !slices.Equal(o.Rezerve, []string{"c", "d"}) ||
		o.NacinRazina != razineGranice || o.Razine != "10" || o.Kad != "" {
		t.Errorf("obrazac %+v", o)
	}
	if rm.opis("nema", nil) != "" {
		t.Error("letva bez računa nema opis")
	}
	if rm := (&PrognozeHandler{}).racuniModela(); len(rm.glavni)+len(rm.nacrti)+len(rm.zapisi) != 0 {
		t.Errorf("bez baze prognoza: %+v", rm)
	}
	if (*CitacPrognoza)(nil).NacrtiModela() != nil {
		t.Error("bez čitača nema nacrta")
	}
	if len(izborUlaza(nil, "a", "Potok")) != 0 {
		t.Error("izbor bez registra")
	}
	u := urednikIliNista(&Server{})
	if u.Izdaje() || u.SpremiNacrt(prognoza.NacrtLetve{}) != errBezUrednika || u.ObrisiNacrt("a") != errBezUrednika {
		t.Error("poslužitelj bez urednika ne izdaje i ne sprema")
	}
	p := &probniUrednik{izdaje: true}
	s := &Server{}
	s.SetUrednikModela(p)
	u = urednikIliNista(s)
	if !u.Izdaje() || u.SpremiNacrt(prognoza.NacrtLetve{}) != nil || u.ObrisiNacrt("a") != nil || len(p.spremljeni) != 1 || len(p.obrisani) != 1 {
		t.Error("poslužitelj s urednikom predaje dalje")
	}
}

// Zatvorena baza prognoza: nacrta nema, a stranica radi kao da ih nema.
func TestNacrtiIzZatvoreneBaze(t *testing.T) {
	put := bazaPrognoza(t, 1)
	c, err := OtvoriPrognoze(put)
	if err != nil {
		t.Fatal(err)
	}
	if c.NacrtiModela() == nil {
		t.Error("otvorena baza bez nacrta daje praznu mapu")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if c.NacrtiModela() != nil {
		t.Error("zatvorena baza prognoza")
	}
}
