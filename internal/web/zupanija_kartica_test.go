package web

import (
	"strings"
	"testing"

	"gocop/internal/models"
)

// Kartica županije po crtežu (7b): službe kao retci koji vode na obrazac službe,
// gradovi i općine kao čipovi, Podaci desno. Uredi županiju vidi samo globalni
// administrator, a službe i uprava sektora (SmijeSluzbe).
func TestKarticaZupanijePoCrtezu(t *testing.T) {
	sluzbe := []models.Sluzba{
		{ID: "s1", CountyID: 14, Vrsta: models.SluzbaCentar112, Naziv: "Centar 112 Primjerovo", Phone: "112", Email: "c112@primjer.hr"},
		{ID: "s2", CountyID: 14, MunicipalityID: 302, MunicipalityName: "Primjerovo", Vrsta: models.SluzbaPolicijskaPost, Naziv: "PP Primjerovo"},
	}
	data := func(admin, sluzbe2 bool, uredi *models.Sluzba) TerritoryPageData {
		return TerritoryPageData{
			Permissions: &models.UserPermissions{IsGlobalAdmin: admin}, SmijeSluzbe: sluzbe2, Sluzba: uredi,
			County:         models.County{ID: 14, Code: "PR", Name: "Primjerska županija", Seat: "Primjerovo", Prefect: "Ivo Izmišljenić", AreaSqKm: 4155, Population: 305032},
			Sluzbe:         sluzbe,
			Municipalities: []models.Municipality{{ID: 301, Name: "Grad Primjer", Type: "GRAD"}, {ID: 302, Name: "Primjerovo", Type: "OPCINA"}},
			VrsteSluzbi:    models.VrsteSluzbi,
		}
	}
	html := iscrtajStranicu(t, "county_detail.html", data(true, true, nil))
	for _, mora := range []string{
		`Županija · sjedište Primjerovo · 4.155 km² · 305.032 stanovnika`,
		`<a class="redak" href="/territories/counties/14?uredi=s1#sluzba">`, `Centar 112 · 112 · c112@primjer.hr`,
		`Policijska postaja · vrijedi za Primjerovo`,
		`<a href="/territories/municipalities/301">Grad Primjer<span> · grad</span></a>`,
		`<a href="/territories/municipalities/302">Primjerovo</a>`,
		`href="/territories/municipalities/new?county_id=14"`, `href="/territories/counties/14/edit"`,
		`<dt>Župan ili županica</dt><dd>Ivo Izmišljenić</dd>`, `id="sluzba"`, `Stožer civilne zaštite ide na akt od izvanrednog stanja`,
	} {
		if !strings.Contains(html, mora) {
			t.Errorf("administrator: nema %q", mora)
		}
	}
	if strings.Contains(html, `obrisi-sluzbu`) {
		t.Error("Obriši službu je vidljiv bez službe u uređivanju")
	}

	// uprava sektora: službe da, županija ne
	html = iscrtajStranicu(t, "county_detail.html", data(false, true, &sluzbe[0]))
	for _, mora := range []string{`action="/territories/counties/14/sluzbe/s1/obrisi"`, `form="obrisi-sluzbu"`, `name="sluzba_id" value="s1"`} {
		if !strings.Contains(html, mora) {
			t.Errorf("uprava sektora: nema %q", mora)
		}
	}
	for _, ne := range []string{`/territories/counties/14/edit`, `/territories/municipalities/new`} {
		if strings.Contains(html, ne) {
			t.Errorf("uprava sektora vidi %q", ne)
		}
	}

	// bez prava: retci bez poveznice i bez obrasca
	html = iscrtajStranicu(t, "county_detail.html", data(false, false, nil))
	if strings.Contains(html, `?uredi=`) || strings.Contains(html, `id="sluzba"`) || !strings.Contains(html, "Centar 112 Primjerovo") {
		t.Error("osoba bez prava vidi uređivanje službi ili ne vidi službe")
	}
	if strings.Contains(html, `data-naziv-stranice>Ivo`) || strings.Contains(html, "Izmišljenić</h1>") {
		t.Error("naslov nosi ime župana")
	}
}
