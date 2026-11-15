package web

import (
	"context"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
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
		{"objekt", "/readings/structure/" + cs.ID.String(), "/readings/structure/" + cs.ID.String() + "/uvoz-izvoz", uObjektu},
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

// Ispravak s vodostajem ili temperaturom izvan raspona vidi se u pregledu s
// razlogom i ne nudi se na potvrdu; ostali ispravci iz iste datoteke ostaju
// (Odluke (15. 11. 2026.), 4).
func TestPregledIspravakaPokazujeRaspon(t *testing.T) {
	p := noviPosluziteljOcitanja(t)
	kad := time.Date(2014, 3, 2, 7, 0, 0, 0, models.Zagreb)
	dobar := p.upisi(t, "dežurni", kad, -18, nil)
	los := p.upisi(t, "dežurni", kad.Add(time.Hour), -19, nil)
	topao := p.upisi(t, "dežurni", kad.Add(2*time.Hour), -20, nil)
	datoteka := "id;vrijeme;vodostaj_cm;nizvodni_cm;temperatura_c\n" +
		dobar.ID.String() + ";2014-03-02 07:00;-118;;\n" +
		los.ID.String() + ";2014-03-02 08:00;9999;;\n" +
		topao.ID.String() + ";2014-03-02 09:00;-20;;99\n"
	w := p.datoteka(t, "dežurni", "/readings/station/"+p.letva.ID.String()+"/ocitanja/uvoz", "ispravak.csv", datoteka, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("pregled %d", w.Code)
	}
	s := html.UnescapeString(w.Body.String())
	for _, treba := range []string{"vodostaj 9999 cm je izvan razumnog raspona", "temperatura"} {
		if !strings.Contains(s, treba) {
			t.Errorf("pregled nema %q", treba)
		}
	}
	_, polja := poslaniObrazac(t, w.Body.String())
	if ids := polja["id"]; len(ids) != 1 || ids[0] != dobar.ID.String() {
		t.Errorf("na potvrdu idu %v, a treba samo dobar ispravak", ids)
	}
}

// Objekt ima karticu Uvoz i izvoz s izvozom u CSV i ispravcima iz datoteke,
// bez skupnog unosa i Excel izvješća letve (Odluke (15. 11. 2026.), 6).
func TestObjektImaUvozIIzvoz(t *testing.T) {
	p := noviPosluziteljOcitanja(t)
	cs := &models.Structure{Code: "cs-primjerovo", Name: "CS Primjerovo", Kind: models.StructureKindPumpingStation, SectorID: "P", AreaID: 1}
	if err := p.structures.Create(context.Background(), &models.UserPermissions{IsGlobalAdmin: true}, cs); err != nil {
		t.Fatal(err)
	}
	put := "/readings/structure/" + cs.ID.String()
	if w := p.posalji("dežurni", http.MethodGet, put, nil, ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `href="`+put+`/uvoz-izvoz"`) {
		t.Fatalf("očitanja objekta bez poveznice na Uvoz i izvoz: %d", w.Code)
	}
	w := p.posalji("dežurni", http.MethodGet, put+"/uvoz-izvoz", nil, "")
	s := html.UnescapeString(w.Body.String())
	if w.Code != http.StatusOK || !strings.Contains(s, "Vrati s ispravcima") || !strings.Contains(s, put+"/ocitanja.csv") {
		t.Fatalf("kartica objekta: %d", w.Code)
	}
	for _, nema := range []string{"Unesi više očitanja odjednom", "Excel izvješće", `href="`+put+`/uvoz-izvoz"`} {
		if strings.Contains(s, nema) {
			t.Errorf("kartica objekta ima %q", nema)
		}
	}
}

// Vodostaj upisan na crpnoj stanici vidi se na njezinoj letvi s napomenom
// odakle je, bez olovke za ispravak (Odluke (15. 11. 2026.))
func TestOdrazNaStraniciLetve(t *testing.T) {
	p := noviPosluziteljOcitanja(t)
	ctx := context.Background()
	cs := &models.Structure{Code: "cs-primjerovo", Name: "CS Primjerovo", Kind: models.StructureKindPumpingStation, SectorID: "P", AreaID: 1,
		StationID: p.letva.ID.String()}
	if err := p.structures.Create(ctx, &models.UserPermissions{IsGlobalAdmin: true}, cs); err != nil {
		t.Fatal(err)
	}
	v := 210
	rd := models.Reading{StructureID: cs.ID.String(), MeasuredAt: time.Now().Add(-time.Hour), LevelCm: &v,
		Source: models.ReadingSourceManual, UserID: p.osobe["dežurni"].ID.String(), Observer: "Ivo Ivić"}
	if err := p.ocitanja.Create(ctx, &rd); err != nil {
		t.Fatal(err)
	}
	w := p.posalji("dežurni", http.MethodGet, "/readings/station/"+p.letva.ID.String(), nil, "")
	s := html.UnescapeString(w.Body.String())
	if w.Code != http.StatusOK || !strings.Contains(s, "210 cm") || !strings.Contains(s, "upisano na objektu, ondje se i ispravlja") {
		t.Fatalf("letva ne pokazuje vodostaj s objekta: %d", w.Code)
	}
	if strings.Contains(s, "/readings/edit/") {
		t.Error("vodostaj s objekta ima olovku na letvi")
	}
}
