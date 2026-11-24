package web

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Naslovna rukovoditelja obrane izvođača i zamjenika (Odluke (22. 11.
// 2026.), pitanje 3): kartice voditelja usluga, uz stanje obrane i
// vodostaje svog sektora. Stanje ne vodi u dnevnik COP-a i ne kaže tko
// dežura.
func TestNaslovnaObraneIzvodjaca(t *testing.T) {
	o := novaOkolinaNaslovne(t)
	o.h.SetIzvori(IzvoriNaslovne{
		Letve: func(context.Context) ([]models.GaugeSummary, error) {
			return []models.GaugeSummary{
				{Name: "Primjerovo", SectorID: "B", Phase: models.PhasePrep, Latest: &models.Reading{LevelCm: cmNaslovne(300), MeasuredAt: time.Now()}},
				{Name: "Drugdje", SectorID: "C", Phase: models.PhasePrep, Latest: &models.Reading{LevelCm: cmNaslovne(600), MeasuredAt: time.Now()}},
			}, nil
		},
	})
	zeli := []string{kStanjeObrane, kDnevnikRadova, kNaloziOvl, kLjudiNaTerenu, kVodostaji, kVrijeme, kJaceOborine, kSnijeg}
	for _, uloga := range []models.Role{models.RoleContractorAreaLeader, models.RoleContractorAreaDeputy} {
		t.Run(string(uloga), func(t *testing.T) {
			p := dozvoleUsluge(uloga)
			if u := ulogeNaslovne(p, sadNaslovne); !slices.Equal(u, []string{uIzvObrana, uUsluga}) {
				t.Errorf("uloge Naslovne: %v", u)
			}
			if k := karticeUloge(p); !slices.Equal(k, zeli) {
				t.Errorf("kartice: %v", k)
			}
			if d := duznostUsluge(p, kDnevnikRadova, sadNaslovne); d == nil || d.Role != uloga {
				t.Errorf("dnevnik radova po dužnosti %+v", d)
			}
			w := o.zovi("/", p)
			if s := w.Body.String(); w.Code != http.StatusOK || strings.Contains(s, "template:") || !strings.Contains(s, "Stanje obrane") {
				t.Errorf("Naslovna: %d", w.Code)
			}
			v := o.zovi("/naslovna/kartica/vodostaji", p).Body.String()
			if !strings.Contains(v, "Primjerovo") || strings.Contains(v, "Drugdje") {
				t.Errorf("vodostaji izvan sektora: %s", v)
			}
			if o.zovi("/naslovna/kartica/za-rijesiti", p).Code != http.StatusNotFound {
				t.Error("kartica koju uloga nema")
			}
		})
	}
	// voditelj usluga ostaje kakav je bio
	if k := karticeUloge(dozvoleUsluge(models.RoleServiceLeaderForeman)); slices.Contains(k, kStanjeObrane) || duznostObraneIzvodjaca(&dozvoleUsluge(models.RoleServiceLeaderForeman).User, sadNaslovne) != nil {
		t.Errorf("kartice voditelja usluga: %v", k)
	}
}

// Stanje obrane izvođaču ne vodi u dnevnik COP-a i ne kaže tko dežura;
// ostalima kao i dosad
func TestStanjeObraneZaIzvodjaca(t *testing.T) {
	o := novaOkolinaNaslovne(t)
	data := &DashboardViewData{Modules: models.Visibility{models.ModuleJournals: true},
		Stanja: []service.StanjeObrane{{Sektor: "B", Stadij: models.PhasePrep, Dezurni: "Ana Proba", Dnevnik: &models.Journal{ID: "dn-1"}}}}
	izv := o.h.karticaStanja(dozvoleUsluge(models.RoleContractorAreaLeader), data)
	if r := izv.Redovi[0]; r.URL != "" || strings.Contains(r.Sporedno, "Ana Proba") || r.Vrijednost != models.PhasePrep.Label() {
		t.Errorf("izvođač: %+v", r)
	}
	sektor := dozvoleNaslovne(false, models.RoleSectorLeader)
	sektor.User.Duties[0].SectorID = nizNaslovne("B")
	if r := o.h.karticaStanja(sektor, data).Redovi[0]; r.URL != "/dnevnici/dn-1" || !strings.Contains(r.Sporedno, "Ana Proba") {
		t.Errorf("rukovoditelj sektora: %+v", r)
	}
}

// Administracija › Sudionici obrane: nove uloge u skupini Vanjski suradnici,
// s opisom i poljem za vlastiti naziv; spremanje ih uzima kao i ostale
func TestSudioniciObraneIzvodjaca(t *testing.T) {
	stari := models.Terms()
	t.Cleanup(func() { models.SetTerms(stari) })
	models.SetTerms(models.OrgTerms{RoleLabels: map[string]string{string(models.RoleContractorAreaDeputy): "Zamjenik izvođača"}})
	var vanjski roleGroup
	for _, g := range roleGroups() {
		if g.Group == models.RoleGroupExternal {
			vanjski = g
		}
	}
	var uloge []string
	for _, r := range vanjski.Rows {
		uloge = append(uloge, r.Role)
	}
	if !slices.Equal(uloge, []string{"SERVICE_LEADER_FOREMAN", "CONTRACTOR_AREA_LEADER", "CONTRACTOR_AREA_DEPUTY"}) {
		t.Fatalf("Vanjski suradnici: %v", uloge)
	}
	html := iscrtaj(t, "sudionici.html", OrgPageData{RoleGroups: roleGroups()})
	_, skupina, _ := strings.Cut(html, ">Vanjski suradnici</h2>")
	skupina, _, _ = strings.Cut(skupina, "</section>")
	for _, zeli := range []string{
		"Rukovoditelj obrane izvođača za branjeno područje", "Zamjenik rukovoditelja obrane izvođača",
		`name="role_CONTRACTOR_AREA_LEADER"`, `name="role_CONTRACTOR_AREA_DEPUTY" class="form-control" value="Zamjenik izvođača"`,
		"čita rješenja i obavijesti područja", "zamjenjuje rukovoditelja obrane izvođača; ista prava", ">područje<",
	} {
		if !strings.Contains(skupina, zeli) {
			t.Errorf("skupina Vanjski suradnici nema %q", zeli)
		}
	}
}
