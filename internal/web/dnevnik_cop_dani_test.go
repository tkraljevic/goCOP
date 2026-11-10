package web

import (
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

func daniOd(od time.Time, n int) []DanZapisa {
	var out []DanZapisa
	for i := 0; i < n; i++ {
		out = append(out, DanZapisa{Dan: od.AddDate(0, 0, i), Zapisi: make([]models.JournalEntry, i+1)})
	}
	return out
}

// Najnoviji dan otvoren, dan prije sklopljen, stariji u jednom rasponu;
// prijepis dan po dan, otvoren prvi.
func TestSkupineDana(t *testing.T) {
	od := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	if s := skupineDana(nil, false); s != nil {
		t.Fatalf("bez dana: %v", s)
	}
	cases := []struct {
		ime      string
		n        int
		prijepis bool
		duljine  []int
		otvoren  int
	}{
		{"jedan dan", 1, false, []int{1}, 0},
		{"dva dana", 2, false, []int{1, 1}, 1},
		{"tri dana", 3, false, []int{1, 1, 1}, 2},
		{"petnaest dana", 15, false, []int{13, 1, 1}, 2},
		{"prijepis", 3, true, []int{1, 1, 1}, 0},
	}
	for _, c := range cases {
		s := skupineDana(daniOd(od, c.n), c.prijepis)
		if len(s) != len(c.duljine) {
			t.Fatalf("%s: %d skupina, želim %d", c.ime, len(s), len(c.duljine))
		}
		for i, sk := range s {
			if len(sk.Dani) != c.duljine[i] || sk.Otvoren != (i == c.otvoren) || sk.Raspon() != (c.duljine[i] > 1) {
				t.Errorf("%s: skupina %d = %d dana, otvorena %v", c.ime, i, len(sk.Dani), sk.Otvoren)
			}
		}
	}
	raspon := skupineDana(daniOd(od, 15), false)[0]
	if g := raspon.Naslov(); g != "pon 12. 10. – sub 24. 10. 2026." {
		t.Errorf("naslov raspona %q", g)
	}
	if g := raspon.BrojZapisa(); g != 91 {
		t.Errorf("zapisa u rasponu %d", g)
	}
	if g := (SkupinaDana{Dani: daniOd(od, 1)}).Naslov(); g != "pon 12. 10. 2026." {
		t.Errorf("naslov dana %q", g)
	}
	preko := SkupinaDana{Dani: daniOd(time.Date(2025, 12, 30, 0, 0, 0, 0, time.UTC), 4)}
	if g := preko.Naslov(); g != "uto 30. 12. 2025. – pet 2. 1. 2026." {
		t.Errorf("naslov preko godine %q", g)
	}
	if g := (SkupinaDana{}).Naslov(); g != "" {
		t.Errorf("prazna skupina %q", g)
	}
}

// Stranica zapisa: stariji dani u sklopljenom rasponu, najnoviji otvoren;
// trake ovjere stoje iznad kartica; zaključen dnevnik ovjerava samo voditelj
// COP-a, a prijepis drugima ne pokazuje traku ovjere.
func TestStranicaZapisaCOP(t *testing.T) {
	od := time.Date(2026, 10, 12, 0, 0, 0, 0, models.Zagreb)
	kraj := od.AddDate(0, 0, 14)
	ovjera := kraj.Add(10 * time.Hour)
	novi := func(j models.Journal) JournalPageData {
		j.ID, j.Kind, j.CentarSektor, j.Title = "dn-5", models.JournalKindDefense, "B", "Dnevnik COP-a, 2026."
		dani := daniOd(od, 15)
		dani[14].Zapisi = []models.JournalEntry{{ID: "z9", Number: 9, Kind: models.EntryKindReport, Text: "Vodostaj Batina +312", UserName: "Marko Marić"}}
		return JournalPageData{CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
			Journal: &j, Dani: dani, VidiPlan: true, KarticaCOP: "zapisi", Today: "2026-10-26"}
	}
	html := iscrtaj(t, "dnevnik_cop.html", novi(models.Journal{}))
	for _, want := range []string{
		`<details class="cop-dan cop-raspon">`, "pon 12. 10. – sub 24. 10. 2026.", "13 dana · 91 zapis",
		`<details class="cop-dan" open>`, `id="dan-2026-10-26"`, "upis: Marko Marić", `class="reg-chip zapis-vrsta">Dojava</span>`,
		`js/dnevnik-dani.js`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica nema %q", want)
		}
	}
	ovjeren := novi(models.Journal{EndedAt: &kraj, Zakljucio: "Ana Anić", ZakljucenoAt: &ovjera})
	ovjeren.IzvornikCOP.Ispravan, ovjeren.IzvornikCOP.Sazetak = true, "abcdef0123456789abcdef"
	html = iscrtaj(t, "dnevnik_cop.html", ovjeren)
	if i, k := strings.Index(html, "Dnevnik je ovjeren."), strings.Index(html, "cop-kartice"); i < 0 || k < 0 || i > k {
		t.Error("traka ovjere nije iznad kartica")
	}
	if !strings.Contains(html, `title="Izvornik, SHA-256 abcdef0123456789…">Potpisani PDF</a>`) {
		t.Error("ovjeren dnevnik nema poveznicu na potpisani PDF sa sažetkom")
	}
	zakljucen := novi(models.Journal{EndedAt: &kraj})
	html = iscrtaj(t, "dnevnik_cop.html", zakljucen)
	if !strings.Contains(html, "Kraj obrane upisan je 26. 10. 2026.") || !strings.Contains(html, "Dnevnik još treba ovjeriti voditelj COP-a.") || strings.Contains(html, "Zaključi i ovjeri") {
		t.Error("zaključen dnevnik drugima ne kaže da ovjera čeka voditelja")
	}
	zakljucen.VoditeljCOP = true
	if html = iscrtaj(t, "dnevnik_cop.html", zakljucen); !strings.Contains(html, "Zaključi i ovjeri") {
		t.Error("voditelj COP-a nema obrazac ovjere")
	}
	prijepis := novi(models.Journal{EndedAt: &kraj, Reconstruction: true})
	if html = iscrtaj(t, "dnevnik_cop.html", prijepis); strings.Contains(html, "Kraj obrane upisan je") || strings.Contains(html, "cop-zapisi-zivi") {
		t.Error("prijepis drugima pokazuje traku ovjere ili obrnut redoslijed")
	}
}
