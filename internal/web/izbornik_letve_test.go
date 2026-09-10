package web

import (
	"strings"
	"testing"

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
