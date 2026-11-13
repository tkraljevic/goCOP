package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Kartica objekta po crtežu 17d (5b, 5d): povratak na registar, Podaci s danom
// izmjene, kod crpne stanice ploča Pogon, „Dodaj opis” kad opisa nema, a
// „Upiši očitanje” i „Očitanja” ostaju uz objekt koji prima očitanja.
func TestKarticaObjektaPoCrtezu(t *testing.T) {
	var reg models.StructureRegister
	for _, r := range models.StructureRegisters {
		if r.Kind == models.StructureKindPumpingStation {
			reg = r
		}
	}
	cs := &models.Structure{ID: uuid.MustParse("9f1c0b2e-0000-7000-8000-000000000041"), Name: "CS Primjerovo", Code: "cs-primjerovo",
		Kind: models.StructureKindPumpingStation, SectorID: "P", AreaID: 1, CapacityText: "2x2,15", BrojAgregata: 2,
		StartText: "145", StopText: "80", ImaUstavu: true, UpdatedAt: time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)}
	html := iscrtaj(t, "structure_detail.html", StructurePageData{
		CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Structure: cs, CanEdit: true, Register: reg,
	})
	for _, want := range []string{
		`class="povratna-poveznica">← ` + reg.Title, "<h2>Podaci</h2>", "izmijenjen 4. 11. 2026.",
		"<h2>Pogon</h2>", "2x2,15 m³/s", "145 cm", "80 cm", "ugrađena, gravitacijsko upuštanje i ispuštanje",
		"Upiši očitanje", `href="/readings/structure/` + cs.ID.String() + `"`,
		"Dodaj opis", `href="` + cs.Path() + `/edit"`, "Objekt nije vezan ni na jednu dionicu.",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("kartica crpne stanice nema %q", want)
		}
	}
	nasip := &models.Structure{ID: uuid.New(), Name: "Nasip Primjerovo", Kind: models.StructureKindEmbankment, Notes: "Kruna 4 m."}
	html = iscrtaj(t, "structure_detail.html", StructurePageData{
		CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{}, Structure: nasip,
	})
	for _, ne := range []string{"<h2>Pogon</h2>", "Dodaj opis", "Upiši očitanje", "/edit"} {
		if strings.Contains(html, ne) {
			t.Errorf("kartica nasipa bez prava nudi %q", ne)
		}
	}
}

// Obrazac objekta šalje sva polja za svaku vrstu: rukovatelj prepisuje sve što
// stigne, pa polje koje bi otpalo ili bilo onemogućeno obrisalo bi podatak.
// Obriši je zaseban obrazac, a gumb za njega stoji uz Spremi.
func TestObrazacObjektaSaljeSvaPolja(t *testing.T) {
	polja := []string{"name", "kind", "area_id", "station_id", "station_down_id", "zero_datum", "zero_datum_system",
		"watercourse_code", "vodotok_druge_strane", "u_sklopu_id", "capacity_text", "broj_agregata", "start", "stop",
		"smjer_rada", "ima_sifon", "sifon_min_cm", "ima_ustavu", "latitude", "longitude", "notes", "id", "code"}
	for _, vrsta := range []string{models.StructureKindEmbankment, models.StructureKindSluice, models.StructureKindPumpingStation, models.StructureKindSiphon} {
		st := &models.Structure{ID: uuid.New(), Name: "Objekt Primjerovo", Kind: vrsta, Code: "primjerovo", SectionCodes: []string{"P.1.1"}}
		html := iscrtaj(t, "structure_form.html", StructurePageData{
			CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
			Structure: st, IsEdit: true, CanEdit: true, Kinds: models.StructureKinds,
		})
		glavni := izmedju(html, `<form class="form-page form-wide"`, "</form>")
		for _, p := range polja {
			if !strings.Contains(glavni, `name="`+p+`"`) {
				t.Errorf("%s: obrazac ne šalje polje %s", vrsta, p)
			}
		}
		if strings.Contains(glavni, "disabled") {
			t.Errorf("%s: obrazac ima onemogućeno polje", vrsta)
		}
		for _, want := range []string{`href="` + st.Path() + `" class="povratna-poveznica">← Objekt Primjerovo`, "Uredi objekt",
			`<form id="obrisi-objekt" method="POST" action="/structures/delete"`, `form="obrisi-objekt"`, `href="/sections/P.1.1"`,
			`href="/karta#uredi=objekt:` + st.ID.String() + `"`} {
			if !strings.Contains(html, want) {
				t.Errorf("%s: obrazac nema %q", vrsta, want)
			}
		}
	}
}

// Smjer rada čita se samo za crpnu stanicu: nasip spremljen bez izmjena ne
// smije dobiti „odvodnju” iz skrivenog polja obrasca.
func TestSmjerRadaSamoZaCrpnuStanicu(t *testing.T) {
	obrazac := func(vrsta string) models.Structure {
		r := httptest.NewRequest(http.MethodPost, "/structures/update", strings.NewReader(url.Values{
			"name": {"Objekt"}, "kind": {vrsta}, "smjer_rada": {models.SmjerOdvodnjaINavodnjavanje}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return structureFromForm(r)
	}
	if st := obrazac(models.StructureKindEmbankment); st.SmjerRada != "" || st.Reverzibilna {
		t.Errorf("nasip je dobio smjer rada %q", st.SmjerRada)
	}
	if st := obrazac(models.StructureKindPumpingStation); st.SmjerRada != models.SmjerOdvodnjaINavodnjavanje || !st.Reverzibilna {
		t.Errorf("crpna stanica nema smjer iz obrasca: %q", st.SmjerRada)
	}
}
