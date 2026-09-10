package web

import (
	"strings"
	"testing"
	"time"

	"gocop/internal/models"

	"github.com/google/uuid"
)

// Kartica, očitanja i historijat su tri pogleda na istu letvu, pa nose isti
// izbornik. Prije je svaka imala svoj: s očitanja se nije moglo nikamo, a s
// historijata se nije mogao upisati vodostaj.
func TestSveStraniceLetveNoseIstiIzbornik(t *testing.T) {
	id := uuid.MustParse("c625fa9d-0425-5115-8c49-8819cbb17bbd")
	st := models.Station{ID: id, Name: "Batina", Code: "batina"}
	korisnik := &models.User{FullName: "P"}
	prava := &models.UserPermissions{IsGlobalAdmin: true}

	stranice := map[string]string{
		"kartica": iscrtaj(t, "station_detail.html", StationPageData{
			CurrentUser: korisnik, Permissions: prava, Station: st,
			CanEdit: true, CanRecord: true, LetvaStranica: "kartica"}),
		"historijat": iscrtaj(t, "station_history.html", StationPageData{
			CurrentUser: korisnik, Permissions: prava, Station: st,
			CanEdit: true, CanRecord: true, LetvaStranica: "historijat"}),
		"ocitanja": iscrtaj(t, "reading_history.html", ReadingHistoryData{
			CurrentUser: korisnik, Permissions: prava, Station: &st, GaugeName: "Batina",
			CanEdit: true, CanRecord: true, LetvaStranica: "ocitanja"}),
	}

	for ime, html := range stranice {
		izbornik := dioIzbornika(t, ime, html)
		for _, gumb := range []string{"Upiši očitanje", "Kartica letve", "Očitanja", "Historijat", "Uredi"} {
			if !strings.Contains(izbornik, gumb) {
				t.Errorf("%s: u izborniku nema %q", ime, gumb)
			}
		}
		// stranica na kojoj se stoji označena je i nije poveznica
		if !strings.Contains(izbornik, `aria-current="page"`) {
			t.Errorf("%s: nijedan gumb nije označen kao trenutna stranica", ime)
		}
		if n := strings.Count(izbornik, `aria-current="page"`); n != 1 {
			t.Errorf("%s: označeno je %d stranica, mora biti jedna", ime, n)
		}
	}

	// rijetke radnje ne ulaze u izbornik
	for ime, html := range stranice {
		if strings.Contains(dioIzbornika(t, ime, html), "Generiraj izvješće") {
			t.Errorf("%s: izvješće je u izborniku; rijetke radnje idu na dno stranice", ime)
		}
	}
}

// Bez prava upisa nema ni gumba za upis — inače vodi na odbijenicu.
func TestBezPravaUpisaNemaGumba(t *testing.T) {
	st := models.Station{ID: uuid.New(), Name: "Batina", Code: "batina"}
	html := iscrtaj(t, "station_detail.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{},
		Station:     st, LetvaStranica: "kartica"})
	if strings.Contains(dioIzbornika(t, "kartica", html), "Upiši očitanje") {
		t.Error("gumb za upis stoji i bez prava upisa")
	}
}

// Stranica očitanja služi i vodnim građevinama; ondje izbornik letve nema smisla.
func TestOcitanjaGradevineNemajuIzbornikLetve(t *testing.T) {
	html := iscrtaj(t, "reading_history.html", ReadingHistoryData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Structure:   &models.Structure{ID: uuid.New(), Name: "CS Budžak"},
		GaugeName:   "CS Budžak", CanRecord: true, NewURL: "/readings/new?structure=1",
	})
	if strings.Contains(html, "letva-izbornik") {
		t.Error("građevina je dobila izbornik letve")
	}
	if !strings.Contains(html, "Upiši očitanje") {
		t.Error("građevini je nestao gumb za upis")
	}
}

func dioIzbornika(t *testing.T, ime, html string) string {
	t.Helper()
	i := strings.Index(html, "letva-izbornik")
	if i < 0 {
		t.Fatalf("%s: nema zajedničkog izbornika", ime)
	}
	kraj := strings.Index(html[i:], "</div>\n")
	if kraj < 0 {
		kraj = len(html) - i
	}
	return html[i : i+kraj]
}

// Prazan historijat mora reći gdje se što upisuje, a obrazac ne smije obećavati
// ono što ne prima. Krajnosti su otišle na karticu kad su se stranice
// razdvojile, a tekstovi su ostali govoriti o njima.
func TestObrazacHistorijataNeObecavaTudje(t *testing.T) {
	st := models.Station{ID: uuid.New(), Name: "Siga", Code: "siga"}
	html := iscrtaj(t, "station_history_form.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, CanEdit: true, IsEdit: true,
	})
	// prima ovo dvoje
	for _, want := range []string{"Povratni vodostaji", "Promjene kote nule"} {
		if !strings.Contains(html, want) {
			t.Errorf("obrazac historijata nema %q", want)
		}
	}
	// i upućuje drugamo za ono što ne prima
	if !strings.Contains(html, "kartica letve") {
		t.Error("obrazac ne kaže gdje se uređuje ostalo")
	}
	// ali ne tvrdi da krajnosti uređuje on
	uvod := html[:strings.Index(html, "Povratni vodostaji")]
	if strings.Contains(uvod, "zabilježeno: krajnosti") {
		t.Error("obrazac i dalje obećava krajnosti, kojih na njemu nema")
	}
}

// Uredi mijenja ono što je na svojoj stranici. Prije je s očitanja vodio na
// obrazac kartice — uređivao bi pragove i kotu nule, a ničega od toga ondje
// nema.
func TestUrediMijenjaOnoStoJeNaStranici(t *testing.T) {
	id := uuid.MustParse("c625fa9d-0425-5115-8c49-8819cbb17bbd")
	st := models.Station{ID: id, Name: "Batina", Code: "batina"}
	korisnik := &models.User{FullName: "P"}
	prava := &models.UserPermissions{IsGlobalAdmin: true}

	for _, s := range []struct {
		stranica, predlozak, cilj string
	}{
		{"kartica", "station_detail.html", "/stations/" + id.String() + "/edit"},
		{"historijat", "station_history.html", "/stations/" + id.String() + "/historijat/uredi"},
	} {
		var html string
		if s.stranica == "historijat" {
			html = iscrtaj(t, s.predlozak, StationPageData{CurrentUser: korisnik, Permissions: prava,
				Station: st, CanEdit: true, CanRecord: true, LetvaStranica: s.stranica})
		} else {
			html = iscrtaj(t, s.predlozak, StationPageData{CurrentUser: korisnik, Permissions: prava,
				Station: st, CanEdit: true, CanRecord: true, LetvaStranica: s.stranica})
		}
		if !strings.Contains(dioIzbornika(t, s.stranica, html), s.cilj) {
			t.Errorf("%s: Uredi ne vodi na %s", s.stranica, s.cilj)
		}
	}

	// očitanja: uređuju se sama očitanja, na istoj stranici
	ocitanja := iscrtaj(t, "reading_history.html", ReadingHistoryData{
		CurrentUser: korisnik, Permissions: prava, Station: &st, GaugeName: "Batina",
		CanEdit: true, CanRecord: true, LetvaStranica: "ocitanja"})
	izbornik := dioIzbornika(t, "ocitanja", ocitanja)
	if !strings.Contains(izbornik, `href="#ispravci"`) {
		t.Error("očitanja: Uredi ne vodi na ispravak očitanja")
	}
	if strings.Contains(izbornik, "/stations/"+id.String()+"/edit") {
		t.Error("očitanja: Uredi i dalje vodi na obrazac kartice")
	}
	if !strings.Contains(ocitanja, `id="ispravci"`) {
		t.Error("očitanja: nema odjeljka na koji Uredi vodi")
	}
}

// Obrazac historijata prima ono što historijat pokazuje a ne računa se samo.
// Ograde su treće, uz povratne vodostaje i promjene kote nule.
func TestObrazacHistorijataPrimaOgrade(t *testing.T) {
	st := models.Station{ID: uuid.New(), Name: "Batina", Code: "batina",
		OgradeNiza: []models.OgradaNiza{{Izvor: "letva-hv", Velicina: "vodostaj",
			Tekst: "mjerač zaleđen"}}}
	html := iscrtaj(t, "station_history_form.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, CanEdit: true, IsEdit: true,
		OgradeNizaJSON: jsonZaObrazac(st.OgradeNiza),
	})
	for _, want := range []string{"Ograde uz nizove", `name="ograde_niza"`, "mjerač zaleđen"} {
		if !strings.Contains(html, want) {
			t.Errorf("obrazac nema %q", want)
		}
	}
	// i kaže da ograda koja stiže s paketom nije njegov posao
	if !strings.Contains(html, "iz izdanja") {
		t.Error("obrazac ne razlikuje vlastitu ogradu od one koja stiže s paketom")
	}
}

// Ograda bez teksta ili bez izvora nije podatak nego pogreška u unosu.
func TestPraznaOgradaSeNeSprema(t *testing.T) {
	ulaz := `[{"izvor":"his2000","tekst":"zaleđen"},{"izvor":"","tekst":"bez izvora"},` +
		`{"izvor":"cop","tekst":"   "},{"izvor":" letva-hv ","velicina":" vodostaj ","tekst":" led "}]`
	out := parseOgradeNiza(ulaz)
	if len(out) != 2 {
		t.Fatalf("spremljeno %d ograda, očekivane 2: %+v", len(out), out)
	}
	if out[1].Izvor != "letva-hv" || out[1].Tekst != "led" {
		t.Errorf("razmaci se ne uklanjaju: %+v", out[1])
	}
}

// Razorna radnja stoji na kraju stranice, ne usred nje. Podnožje se
// iscrtavalo prije odjeljka o dionicama, pa je Obriši stajao na sredini a
// zadnji odjeljak visio ispod njega.
func TestPodnozjeKarticeStojiNaKraju(t *testing.T) {
	html := iscrtaj(t, "station_detail.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     models.Station{ID: uuid.New(), Name: "Batina", Code: "batina"},
		CanEdit:     true, CanRecord: true, LetvaStranica: "kartica",
	})
	podnozje := strings.Index(html, "kartica-podnozje")
	dionice := strings.Index(html, "Mjerodavna za dionice")
	if podnozje < 0 || dionice < 0 {
		t.Fatal("nedostaje podnožje ili odjeljak o dionicama")
	}
	if podnozje < dionice {
		t.Error("podnožje s Obriši stoji prije zadnjeg odjeljka")
	}
}

// Kad telemetrija stane, stranica izgleda jednako kao da sve radi — vrijednost
// stoji, samo je stara. Za velike vode to je razlika između odluke i provjere.
func TestOcitanjeKojeKasniSeOznacava(t *testing.T) {
	cm := func(v int) *int { return &v }
	st := models.Station{ID: uuid.New(), Name: "Batina", Code: "batina"}
	podaci := func(staro time.Duration) string {
		r := models.Reading{MeasuredAt: time.Now().Add(-staro), LevelCm: cm(-118), Origin: "letva"}
		return iscrtaj(t, "reading_history.html", ReadingHistoryData{
			CurrentUser: &models.User{FullName: "P"},
			Permissions: &models.UserPermissions{IsGlobalAdmin: true},
			Station:     &st, GaugeName: "Batina", CanRecord: true, LetvaStranica: "ocitanja",
			Readings: []models.Reading{r}, Latest: &r, Count: 1,
		})
	}
	if strings.Contains(podaci(2*time.Hour), "kasni") {
		t.Error("svježe očitanje označeno kao zakašnjelo")
	}
	if !strings.Contains(podaci(30*time.Hour), "kasni") {
		t.Error("očitanje starije od dana nije označeno")
	}
}
