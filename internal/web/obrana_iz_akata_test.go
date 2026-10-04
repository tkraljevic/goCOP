package web

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Kartica dionice pokazuje stanje obrane iz akata: stadij koji vrijedi, od
// kada i kojim aktom, niže u pozadini i akte koji još nisu stupili na snagu
func TestKarticaDioniceStanjeIzAkata(t *testing.T) {
	od := time.Date(2026, 11, 2, 14, 0, 0, 0, models.Zagreb)
	najavljen := models.Akt{ID: "akt-3", Radnja: models.AktPrekid, Stupanj: models.PhaseRegular, Sektor: "P", Broj: 3, Godina: 2026,
		StationName: "Primjerovo", Vrijedi: time.Date(2026, 11, 4, 9, 0, 0, 0, models.Zagreb)}
	data := SectionPageData{
		CurrentUser: &models.User{FullName: "Pero Perić"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Section:     models.Section{Code: "P.1.1", AreaID: 1, SectorID: "P"},
		StanjeObrane: models.StanjeObrane{Aktivni: []models.AktivniStadij{
			{Stupanj: models.PhasePrep, Od: od.Add(-30 * time.Hour), AktID: "akt-1"},
			{Stupanj: models.PhaseRegular, Od: od, AktID: "akt-2"},
		}},
		NajavljeniAkti: []models.Akt{najavljen},
	}
	html := iscrtaj(t, "section_detail.html", data)
	for _, want := range []string{"Redovna obrana", "na snazi od <strong>02.11.2026 14:00</strong>", `href="/akti/akt-2"`,
		"U pozadini vrijedi Pripremno stanje od 01.11.2026 08:00", `href="/akti/akt-3"`, "stupa na snagu <strong>04.11.2026 09:00</strong>"} {
		if !strings.Contains(html, want) {
			t.Errorf("kartica nema %q", want)
		}
	}
	// bez akata i bez epizode nema okvira obrane
	data.StanjeObrane, data.NajavljeniAkti = models.StanjeObrane{}, nil
	if html := iscrtaj(t, "section_detail.html", data); strings.Contains(html, "na snazi od") || strings.Contains(html, "stupa na snagu") {
		t.Error("kartica bez obrane pokazuje obranu")
	}
}

// Stanje za karticu: bez servisa ili s greškom prazno, inače iz akata
func TestObranaIzAkata(t *testing.T) {
	sec := &models.Section{Code: "P.1.1", SectorID: "P"}
	h := &SectionsHandler{}
	if s, n := h.obranaIzAkata(context.Background(), sec); s.Traje() || n != nil {
		t.Errorf("bez servisa: %+v %v", s, n)
	}
	h.stanjeObrane = func(context.Context, string, string) (models.StanjeObrane, []models.Akt, error) {
		return models.StanjeObrane{Aktivni: []models.AktivniStadij{{Stupanj: models.PhasePrep}}}, nil, errors.New("baza ne odgovara")
	}
	if s, _ := h.obranaIzAkata(context.Background(), sec); s.Traje() {
		t.Error("greška čitanja akata dala je stanje")
	}
	var traziSektor, traziDionicu string
	h.stanjeObrane = func(_ context.Context, sektor, dionica string) (models.StanjeObrane, []models.Akt, error) {
		traziSektor, traziDionicu = sektor, dionica
		return models.StanjeObrane{Aktivni: []models.AktivniStadij{{Stupanj: models.PhasePrep}}}, []models.Akt{{ID: "a"}}, nil
	}
	if s, n := h.obranaIzAkata(context.Background(), sec); s.Najvisi() != models.PhasePrep || len(n) != 1 || traziSektor != "P" || traziDionicu != "P.1.1" {
		t.Errorf("iz akata: %+v %v (%s %s)", s, n, traziSektor, traziDionicu)
	}
}

// Poslužitelj prije postavljanja servisa akata nema stanja; poslije ga čita
// iz ovjerenih akata sektora
func TestStanjeObraneDionicePosluzitelja(t *testing.T) {
	s := &Server{}
	if st, n, err := s.stanjeObraneDionice(context.Background(), "P", "P.1.1"); st.Traje() || n != nil || err != nil {
		t.Errorf("bez servisa akata: %+v %v %v", st, n, err)
	}
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "akti.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewAktiRepository(baza, ledger.New(baza, "test"))
	a := &models.Akt{ID: "akt-1", Sektor: "P", Radnja: models.AktUspostava, Stupanj: models.PhasePrep, Status: models.AktOvjeren,
		Vrijedi: time.Now().Add(-time.Hour), Dionice: []models.AktDionica{{Code: "P.1.1"}}}
	if err := repo.SaveAkt(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	s.SetAkti(service.NewAktService(repo, nil, nil, nil, nil, nil, nil, "test"))
	if st, _, err := s.stanjeObraneDionice(context.Background(), "P", "P.1.1"); err != nil || st.Najvisi() != models.PhasePrep {
		t.Errorf("iz akata: %+v %v", st, err)
	}
}

// Knjiga dionice: obrana iz akata s nižim u pozadini; prekinuta aktom ne
// pokazuje zatečenu epizodu; bez akata vrijedi epizoda
func TestObranaUKnjiziDionice(t *testing.T) {
	od := time.Date(2026, 11, 2, 14, 0, 0, 0, models.Zagreb)
	epizoda := &models.DefenseEpisode{Phase: models.PhaseRegular, StartedAt: od, DeclaredByName: "Pero Perić", Basis: models.BasisOrder}
	d := SectionPageData{StanjeObrane: models.StanjeObrane{IzAkata: true, Aktivni: []models.AktivniStadij{
		{Stupanj: models.PhasePrep, Od: od.Add(-30 * time.Hour)}, {Stupanj: models.PhaseRegular, Od: od}}}}
	if got := obranaUKnjizi(d); !strings.Contains(got, "Redovna obrana, na snazi od 02.11.2026. 14:00") || !strings.Contains(got, "u pozadini pripremno stanje od 01.11.2026. 08:00") {
		t.Errorf("iz akata: %q", got)
	}
	d = SectionPageData{OpenEpisode: epizoda, StanjeObrane: models.StanjeObrane{IzAkata: true}}
	if got := obranaUKnjizi(d); got != "nije proglašena" {
		t.Errorf("prekinuta aktom: %q", got)
	}
	d.StanjeObrane.IzAkata = false
	if got := obranaUKnjizi(d); !strings.Contains(got, "Redovna obrana") || !strings.Contains(got, "proglasio Pero Perić") || !strings.Contains(got, "osnova: ") {
		t.Errorf("bez akata, epizoda: %q", got)
	}
	if got := obranaUKnjizi(SectionPageData{}); got != "nije proglašena" {
		t.Errorf("bez ičega: %q", got)
	}
	// kartica: dionica s aktima ne pokazuje zatečenu epizodu
	html := iscrtaj(t, "section_detail.html", SectionPageData{
		CurrentUser: &models.User{FullName: "Pero Perić"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Section: models.Section{Code: "P.1.1", AreaID: 1, SectorID: "P"}, OpenEpisode: epizoda, StanjeObrane: models.StanjeObrane{IzAkata: true}})
	if strings.Contains(html, "na snazi od") {
		t.Error("kartica pokazuje epizodu koju su akti prekinuli")
	}
}
