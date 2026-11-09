package web

import (
	"strings"
	"testing"

	"gocop/internal/models"
)

// Kartica letve na obrascu novog akta: vodostaj je zadnje očitanje s
// vodostajem (očitanje bez vodostaja, npr. samo temperatura, ne briše ga),
// područja se pišu jednom, a „Tko ovjerava” je za sektor dionica letve.
func TestKarticaLetveNaObrascuAkta(t *testing.T) {
	cm := 652
	sVodostajem := models.Reading{LevelCm: &cm}
	bezVodostaja := &models.Reading{}
	if (AktiPageData{}).ZadnjiVodostaj() != nil {
		t.Error("letva bez očitanja ima vodostaj")
	}
	if d := (AktiPageData{Zadnje: &sVodostajem}); d.ZadnjiVodostaj() != d.Zadnje {
		t.Error("zadnje očitanje s vodostajem nije na kartici")
	}
	if v := (AktiPageData{Zadnje: bezVodostaja, Ocitanja: []models.Reading{sVodostajem}}).ZadnjiVodostaj(); v == nil || v.LevelCm == nil || *v.LevelCm != 652 {
		t.Errorf("uz zadnje očitanje bez vodostaja kartica treba prvo očitanje s vodostajem: %+v", v)
	}
	if (AktiPageData{Zadnje: bezVodostaja}).ZadnjiVodostaj() != nil {
		t.Error("očitanje bez vodostaja nije vodostaj")
	}

	d := AktiPageData{Stupnjevi: models.StupnjeviAkta, Dionice: []models.Section{
		{Code: "B.34.1", AreaID: 34, SectorID: "B", AreaName: "Međudržavne rijeke Drava i Dunav"},
		{Code: "B.16.2", AreaID: 16, SectorID: "B", AreaName: "Mali sliv Baranja"},
		{Code: "B.34.2", AreaID: 34, SectorID: "B", AreaName: "Međudržavne rijeke Drava i Dunav"},
	}}
	if p := d.PodrucjaLetve(); len(p) != 2 || p[0].ID != 34 || p[1].ID != 16 || p[1].Name != "Mali sliv Baranja" {
		t.Errorf("područja letve: %+v", p)
	}
	tko := d.TkoOvjerava()
	if len(tko) != len(models.StupnjeviAkta) {
		t.Fatalf("rečenica po stupnju: %+v", tko)
	}
	for _, x := range tko {
		if x.Stupanj == models.PhaseEmergency && !strings.Contains(x.Tekst, "Sektora B") {
			t.Errorf("izvanredna obrana bez sektora letve: %q", x.Tekst)
		}
	}
	if s := (AktiPageData{Stupnjevi: models.StupnjeviAkta}).TkoOvjerava(); !strings.Contains(s[2].Tekst, "sektora ili viši") {
		t.Errorf("bez dionica: %q", s[2].Tekst)
	}
}
