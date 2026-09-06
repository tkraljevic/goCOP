package models

import "testing"

func TestWatercourseWikiURLUsesSlug(t *testing.T) {
	w := Watercourse{OfficialName: "rijeka Dunav", WikiSlug: "Dunav"}
	if got, want := w.WikiURL(), "https://hr.wikipedia.org/wiki/Dunav"; got != want {
		t.Fatalf("WikiURL() = %q, želim %q", got, want)
	}
}
