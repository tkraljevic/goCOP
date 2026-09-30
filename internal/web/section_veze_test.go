package web

import (
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
)

func TestPredloziVezu(t *testing.T) {
	budzak := models.Structure{ID: uuid.New(), Name: "CS Budžak", Kind: models.StructureKindPumpingStation, AreaID: 16}
	draz := models.Structure{ID: uuid.New(), Name: "Ustava Draž", Kind: models.StructureKindSluice, AreaID: 16}
	csDraz := models.Structure{ID: uuid.New(), Name: "CS Draž", Kind: models.StructureKindPumpingStation, AreaID: 16}
	nasip := models.Structure{ID: uuid.New(), Name: "Nasip Draž", Kind: models.StructureKindEmbankment, AreaID: 34}
	objekti := []models.Structure{budzak, draz, csDraz, nasip}
	vode := []models.Watercourse{
		{Code: "rijeka-dunav", Name: "Dunav", OfficialName: "rijeka Dunav", Kind: "rijeka"},
		{Code: "rijeka-drava", Name: "Drava", OfficialName: "rijeka Drava", Kind: "rijeka"},
		{Code: "rijeka-karasica-miholjacka", Name: "Karašica", OfficialName: "rijeka Karašica (miholjačka)", Kind: "rijeka"},
		{Code: "potok-karasica-baranja", Name: "Karašica", OfficialName: "potok Karašica (Baranja)", Kind: "potok"},
		{Code: "sarkanjski-dunavac", Name: "Šarkanjski Dunavac", OfficialName: "Šarkanjski Dunavac"},
		{Code: "potok-brana", Name: "Brana", OfficialName: "potok Brana", Kind: "potok"},
		{Code: "potok-krajna", Name: "Krajna", OfficialName: "potok Krajna", Kind: "potok"},
		{Code: "rijeka-vuka", Name: "Vuka", OfficialName: "rijeka Vuka", Kind: "rijeka"},
	}
	for _, c := range []struct {
		naziv, voda string
		objekt      *models.Structure
		vodaKod     string
	}{
		// objekt drugog područja, naziv s kapacitetom
		{"CS Budžak,Q=0,40 m3/s", "rijeka-dunav", &budzak, ""},
		{"ustava Draž,Q=1,50m3/s", "rijeka-dunav", &draz, ""},
		{"crpna stanica Draž", "", &csDraz, ""},
		// nasip se ne predlaže kao objekt
		{"Nasip Draž", "", nil, ""},
		// sklonjena voda s vrstom ispred razrješava istoimene
		{"ušće p. Karašice", "rijeka-dunav", nil, "potok-karasica-baranja"},
		{"ušće u r. Karašicu", "rijeka-drava", nil, "rijeka-karasica-miholjacka"},
		{"ušće Šarkanjskog Dun.", "rijeka-dunav", nil, "sarkanjski-dunavac"},
		{"ušće r. Drave", "rijeka-dunav", nil, "rijeka-drava"},
		{"ušće u Vuku l.o.", "rijeka-drava", nil, "rijeka-vuka"},
		// voda poddionice nije veza
		{"r. Drava postaje granična rijeka s Mađarskom", "rijeka-drava", nil, ""},
		// imenice i mjesta koja liče na vode
		{"brana retencije Lisičine", "", nil, ""},
		{"kraj gradske obaloutvrde", "", nil, ""},
		{"ušće u Dunavac", "rijeka-drava", nil, ""},
		{"ušće p. Toplica u Staru Dravu", "potok-toplica", nil, ""},
		{"c.m. Vukovar-Bačka Palanka", "", nil, ""},
		{"c.m. Batina-Bezdan granični prijelaz", "rijeka-dunav", nil, ""},
	} {
		p := predloziVezu(c.naziv, 34, "Međudržavne rijeke Drava i Dunav", c.voda, objekti, vode)
		switch {
		case c.objekt != nil:
			if p == nil || p.StructureID != c.objekt.ID.String() {
				t.Errorf("%q: htio objekt %s, dobio %+v", c.naziv, c.objekt.Name, p)
			}
		case c.vodaKod != "":
			if p == nil || p.WatercourseCode != c.vodaKod {
				t.Errorf("%q: htio vodu %s, dobio %+v", c.naziv, c.vodaKod, p)
			}
		default:
			if p != nil {
				t.Errorf("%q: nije trebalo prijedloga, dobio %+v", c.naziv, p)
			}
		}
	}
}
