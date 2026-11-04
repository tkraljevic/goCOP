package models

import "testing"

func TestWatercourseWikiURLUsesSlug(t *testing.T) {
	w := Watercourse{OfficialName: "rijeka Dunav", WikiSlug: "Dunav"}
	if got, want := w.WikiURL(), "https://hr.wikipedia.org/wiki/Dunav"; got != want {
		t.Fatalf("WikiURL() = %q, želim %q", got, want)
	}
}

// Voda izvan popisa voda I. reda pokazuje svoju skupinu, npr. kanali II.
// reda u Baranji; bez skupine ostaje „nije voda I. reda”.
func TestCategoryLabelSkupinaIzvanOdluke(t *testing.T) {
	for _, c := range []struct {
		w    Watercourse
		want string
	}{
		{Watercourse{Subcategory: "kanal II. reda"}, "kanal II. reda"},
		{Watercourse{Subcategory: "  "}, "nije voda I. reda"},
		{Watercourse{}, "nije voda I. reda"},
		{Watercourse{Category: "MEĐUDRŽAVNE VODE", Subcategory: "vodotoci"}, "Međudržavna voda"},
	} {
		if got := c.w.CategoryLabel(); got != c.want {
			t.Errorf("%+v: %q, htio %q", c.w, got, c.want)
		}
	}
}

// Red vode za slojeve Karte: iz kategorije s popisa voda I. reda, inače iz
// upisane skupine; bez skupine reda nema.
func TestRedVode(t *testing.T) {
	for _, s := range []struct {
		w    Watercourse
		htio string
	}{
		{Watercourse{Category: "MEĐUDRŽAVNE VODE"}, RedMedudrzavni},
		{Watercourse{Category: "DRUGE VEĆE VODE I KANALI", Subcategory: "vodotoci"}, RedPrvi},
		{Watercourse{Subcategory: "kanal I. reda"}, RedPrvi},
		{Watercourse{Subcategory: "kanal II. reda"}, RedDrugi},
		{Watercourse{Subcategory: "Kanal III. reda"}, RedTreciCetvrti},
		{Watercourse{Subcategory: "potok IV. reda"}, RedTreciCetvrti},
		{Watercourse{Kind: "potok"}, ""},
	} {
		if got := s.w.RedVode(); got != s.htio {
			t.Errorf("%+v: %q, htio %q", s.w, got, s.htio)
		}
	}
}
