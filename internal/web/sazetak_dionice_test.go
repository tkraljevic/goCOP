package web

import (
	"strings"
	"testing"

	"gocop/internal/models"
)

// Sažetak na vrhu ima smisla samo kad sažima nešto što se ne vidi odjednom.
// Kod dionice s jednom poddionicom opis se slaže IZ nje, pa ispadne doslovan
// prijepis onoga što piše dva retka niže, a sve brojke stoje kao značke uz
// same stavke.
func TestSazetakStojiSamoKadSazima(t *testing.T) {
	jedna := models.Section{Code: "B.34.2"}
	if s := sazetakDionice(jedna, []PartView{{}}); s.Ima() {
		t.Errorf("dionica s jednom poddionicom dobila sažetak: %+v", s)
	}

	// Vlastiti opis je čovjekov tekst i pokazuje se uvijek.
	svoj := models.Section{Code: "B.34.2", DescriptionCustom: true}
	if s := sazetakDionice(svoj, []PartView{{}}); !s.Opis {
		t.Error("vlastiti opis se ne pokazuje")
	}

	// S više poddionica sažetak doista sažima.
	vise := models.Section{Code: "B.15.5"}
	s := sazetakDionice(vise, []PartView{{}, {}, {}})
	if !s.Poddionica {
		t.Error("broj poddionica se ne pokazuje kad ih je više")
	}
	if !s.Ima() {
		t.Error("dionica s tri poddionice ostala bez sažetka")
	}
}

// Prazan sažetak ne smije ostaviti praznu karticu na vrhu stranice.
func TestPrazanSazetakNeIscrtavaKarticu(t *testing.T) {
	part := models.SectionPart{Seq: 1, Bank: "D"}
	d := SectionPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Section:     models.Section{Code: "B.34.2", AreaID: 34, SectorID: "B", Parts: []models.SectionPart{part}},
		Parts:       []PartView{{SectionPart: part}},
	}
	d.Sazetak = sazetakDionice(d.Section, d.Parts)
	html := iscrtaj(t, "section_detail.html", d)
	if strings.Contains(html, "Ukupna duljina") || strings.Contains(html, "Poddionica</dt>") {
		t.Error("sažetak se iscrtao iako nema što sažeti")
	}
}
