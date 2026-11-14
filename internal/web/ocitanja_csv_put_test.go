package web

import (
	"context"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

var (
	akcijaObrasca = regexp.MustCompile(`<form method="POST" action="([^"]+)">`)
	skrivenoPolje = regexp.MustCompile(`<input type="hidden" name="(\w+)" value="([^"]*)">`)
)

// poslaniObrazac vraća adresu i polja obrasca pregleda onako kako bi ih preglednik poslao.
func poslaniObrazac(t *testing.T, stranica string) (string, url.Values) {
	t.Helper()
	m := akcijaObrasca.FindStringSubmatch(stranica)
	if m == nil {
		t.Fatal("pregled nema obrazac potvrde")
	}
	polja := url.Values{}
	for _, p := range skrivenoPolje.FindAllStringSubmatch(stranica, -1) {
		polja.Add(p[1], html.UnescapeString(p[2]))
	}
	return html.UnescapeString(m[1]), polja
}

// Ispravak iz datoteke prolazi cijeli put kao u pregledniku: datoteka, pregled,
// gumb potvrde i upis. Do 14. 11. 2026. potvrda letve slala je na adresu koja
// ne postoji, a testovi su potvrdu zvali izravno.
func TestIspravakIzDatotekeOdDatotekeDoUpisa(t *testing.T) {
	p := noviPosluziteljOcitanja(t)
	ctx := context.Background()
	kad := time.Date(2014, 3, 2, 7, 0, 0, 0, models.Zagreb)

	cs := &models.Structure{Code: "cs-primjerovo", Name: "CS Primjerovo", Kind: models.StructureKindPumpingStation, SectorID: "P", AreaID: 1}
	if err := p.structures.Create(ctx, &models.UserPermissions{IsGlobalAdmin: true}, cs); err != nil {
		t.Fatal(err)
	}
	v := 210
	uObjektu := models.Reading{ID: uuid.New(), StructureID: cs.ID.String(), MeasuredAt: kad, LevelCm: &v,
		Source: models.ReadingSourceManual, UserID: p.osobe["dežurni"].ID.String()}
	if err := p.ocitanja.Create(ctx, &uObjektu); err != nil {
		t.Fatal(err)
	}
	naLetvi := p.upisi(t, "dežurni", kad, -18, nil)

	for _, s := range []struct {
		ime, put, povratak string
		ocitanje           models.Reading
	}{
		{"letva", "/readings/station/" + p.letva.ID.String(), "/readings/station/" + p.letva.ID.String() + "/uvoz-izvoz", naLetvi},
		{"objekt", "/readings/structure/" + cs.ID.String(), "/readings/structure/" + cs.ID.String(), uObjektu},
	} {
		datoteka := "id;vrijeme;vodostaj_cm;nizvodni_cm\n" + s.ocitanje.ID.String() + ";2014-03-02 07:00;-118;\n"
		w := p.datoteka(t, "dežurni", s.put+"/ocitanja/uvoz", "ispravak.csv", datoteka, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: pregled %d", s.ime, w.Code)
		}
		akcija, polja := poslaniObrazac(t, w.Body.String())
		putanja, poruka := porukaPreusmjerenja(t, p.obrazac("dežurni", akcija, polja), "success")
		if putanja != s.povratak || poruka == "" {
			t.Errorf("%s: potvrda na %s vratila %s %q", s.ime, akcija, putanja, poruka)
		}
		nakon, err := p.ocitanja.Get(ctx, s.ocitanje.ID)
		if err != nil || nakon == nil || *nakon.LevelCm != -118 {
			t.Errorf("%s: ispravak nije upisan: %+v %v", s.ime, nakon, err)
		}
	}
}
