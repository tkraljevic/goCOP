package web

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Novo očitanje dobije identifikator od programa: poslani identifikator se
// zanemaruje, i kad je slobodan i kad pripada postojećem očitanju. Očitanje
// se upiše, a postojeće ostaje kakvo je bilo. Tko ne smije upisivati na
// letvu, ne upiše ništa.
func TestNovoOcitanjeZanemarujePoslaniIdentifikator(t *testing.T) {
	o := novaOkolinaPrava(t)
	ctx := context.Background()
	kad := time.Now().Add(-10 * time.Minute).In(models.Zagreb).Format("2006-01-02T15:04")
	upis := func(u *models.User, perms *models.UserPermissions, id string, cm string) (string, url.Values) {
		w := posaljiObrazac(o.h.HandleCreate, u, perms, "/readings/create", url.Values{
			"id": {id}, "station_id": {o.st.ID.String()}, "measured_at": {kad}, "level_cm": {cm}})
		return preusmjerenje(t, w)
	}
	brojSVodostajem := func(cm int) int {
		t.Helper()
		var n int
		if err := o.baza.QueryRow(`SELECT COUNT(*) FROM readings WHERE level_cm = ?`, cm).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	sektor := "Q"
	drugi := models.User{ID: uuid.New(), FullName: "Iva Ivić", IsActive: true,
		Duties: []models.Duty{{ID: uuid.New(), Role: models.RoleOperator, ScopeType: models.ScopeSector, SectorID: &sektor, IsActive: true}}}
	if _, upit := upis(&drugi, models.NewUserPermissions(drugi), uuid.NewString(), "140"); upit.Get("error") == "" {
		t.Errorf("drugi sektor je upisao očitanje: %v", upit)
	}
	if n := brojSVodostajem(140); n != 0 {
		t.Fatalf("odbijen upis ostavio je %d očitanja", n)
	}

	slobodan := uuid.New()
	if _, upit := upis(o.ana, o.anaOvl, slobodan.String(), "130"); upit.Get("success") != "Očitanje je upisano." {
		t.Fatalf("upis s poslanim identifikatorom: %v", upit)
	}
	if rd, err := o.ocitanja.Get(ctx, slobodan); err != nil || rd != nil {
		t.Errorf("očitanje je dobilo poslani identifikator: %v %v", rd, err)
	}
	if n := brojSVodostajem(130); n != 1 {
		t.Errorf("očitanja s vodostajem 130: %d", n)
	}

	// Identifikator postojećeg očitanja: ovdje se provjerava da upis uspije
	// pod novim identifikatorom, a ne samo da postojeće ostane kakvo je bilo
	if _, upit := upis(o.ana, o.anaOvl, o.perino.ID.String(), "135"); upit.Get("success") != "Očitanje je upisano." {
		t.Fatalf("upis s identifikatorom postojećeg očitanja: %v", upit)
	}
	if cm := o.vodostajOcitanja(t, o.perino.ID); cm == nil || *cm != 102 {
		t.Errorf("postojeće očitanje je promijenjeno: %v", cm)
	}
	if n := brojSVodostajem(135); n != 1 {
		t.Errorf("očitanja s vodostajem 135: %d", n)
	}

	// nečitljiv identifikator ne smeta novom upisu, jer se ni ne čita
	if _, upit := upis(o.ana, o.anaOvl, "nije-uuid", "137"); !strings.Contains(upit.Get("success"), "upisano") {
		t.Errorf("upis uz nečitljiv identifikator: %v", upit)
	}
}

// Servis i sam daje novi identifikator kad ga upis donese.
func TestServisNovomOcitanjuDajeIdentifikator(t *testing.T) {
	o := novaOkolinaPrava(t)
	cm := 133
	zadani := uuid.New()
	rd := &models.Reading{ID: zadani, StationID: o.st.ID.String(), MeasuredAt: time.Now().Add(-5 * time.Minute).UTC(), LevelCm: &cm}
	if err := o.h.readingService.Create(context.Background(), o.anaOvl, rd); err != nil {
		t.Fatal(err)
	}
	if rd.ID == zadani || rd.ID == uuid.Nil {
		t.Errorf("identifikator novog očitanja: %s", rd.ID)
	}
	if v := o.vodostajOcitanja(t, rd.ID); v == nil || *v != cm {
		t.Errorf("očitanje nije upisano pod novim identifikatorom: %v", v)
	}
}
