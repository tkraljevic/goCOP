package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Odluke (8. 10. 2026.), pitanje 21: letva s oznakom „kota u cm n. m.”
// (razina akumulacije HEP-a) prima vrijednost u rasponu kote, 5000…40 000 cm,
// a ne vodostaja, i to i pri ručnom upisu, izmjeni i lijepljenju. Ostale
// letve ostaju na −500…3000 cm.
func TestRucniUpisNaLetvuSKotom(t *testing.T) {
	o := novaOkolinaCitanja(t)
	ctx := context.Background()
	stations := repository.NewStationRepository(o.baza, ledger.New(o.baza, "test"))
	akumulacija := &models.Station{ID: uuid.New(), Code: "akumulacija-probna", Name: "Razina akumulacije Probna", KotaNM: true}
	if err := stations.CreateStation(ctx, akumulacija); err != nil {
		t.Fatal(err)
	}
	kad := time.Now().Add(-time.Hour)
	ocitanje := func(stanica *models.Station, cm int) *models.Reading {
		return &models.Reading{StationID: stanica.ID.String(), MeasuredAt: kad, LevelCm: cmP(cm)}
	}

	// sama provjera: raspon ovisi o oznaci letve
	for _, s := range []struct {
		cm     int
		kota   bool
		greska string
	}{
		{19089, true, ""}, {5000, true, ""}, {40000, true, ""},
		{4999, true, "kota 4999 cm n. m. je izvan razumnog raspona"},
		{40001, true, "kota 40001 cm n. m."},
		{300, true, "kota 300 cm n. m."},
		{19089, false, "vodostaj 19089 cm je izvan razumnog raspona"},
		{300, false, ""},
	} {
		err := o.rs.validate(ocitanje(akumulacija, s.cm), s.kota)
		if (s.greska == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), s.greska)) {
			t.Errorf("%d cm, kota %v: %v, očekivano %q", s.cm, s.kota, err, s.greska)
		}
	}

	// upis: kota akumulacije prolazi, vodostaj na njoj ne; na običnoj letvi obrnuto
	rd := ocitanje(akumulacija, 19089)
	if err := o.rs.Create(ctx, o.adminOvl, rd); err != nil {
		t.Fatalf("kota akumulacije: %v", err)
	}
	if err := o.rs.Create(ctx, o.adminOvl, ocitanje(akumulacija, 300)); err == nil || !strings.Contains(err.Error(), "kota 300 cm n. m.") {
		t.Errorf("vodostaj na letvi s kotom: %v", err)
	}
	if err := o.rs.Create(ctx, o.adminOvl, ocitanje(o.primjerovo, 19089)); err == nil || !strings.Contains(err.Error(), "vodostaj 19089 cm") {
		t.Errorf("kota na običnoj letvi: %v", err)
	}

	// izmjena drži raspon letve
	izmjena := *rd
	izmjena.LevelCm = cmP(300)
	if err := o.rs.Update(ctx, o.adminOvl, &izmjena); err == nil || !strings.Contains(err.Error(), "kota 300 cm n. m.") {
		t.Errorf("izmjena u vodostaj: %v", err)
	}
	izmjena.LevelCm = cmP(19095)
	if err := o.rs.Update(ctx, o.adminOvl, &izmjena); err != nil {
		t.Errorf("izmjena kote: %v", err)
	}

	// lijepljenje: cijeli niz prolazi ili pada, po rasponu letve
	if n, err := o.rs.UveziZalijepljena(ctx, o.adminOvl, akumulacija, []models.Reading{
		{ID: uuid.New(), StationID: akumulacija.ID.String(), MeasuredAt: kad.Add(-time.Hour), LevelCm: cmP(19080)},
	}); err != nil || n != 1 {
		t.Errorf("zalijepljena kota: %d, %v", n, err)
	}
	if _, err := o.rs.UveziZalijepljena(ctx, o.adminOvl, akumulacija, []models.Reading{
		{ID: uuid.New(), StationID: akumulacija.ID.String(), MeasuredAt: kad.Add(-2 * time.Hour), LevelCm: cmP(150)},
	}); err == nil || !strings.Contains(err.Error(), "kota 150 cm n. m.") {
		t.Errorf("zalijepljen vodostaj na letvi s kotom: %v", err)
	}
}

// Letva koje nema, ili objekt, vode vodostaj
func TestKotaLetveBezPostaje(t *testing.T) {
	o := novaOkolinaCitanja(t)
	ctx := context.Background()
	for _, id := range []string{"", "nije-uuid", uuid.NewString(), o.primjerovo.ID.String()} {
		if o.rs.kotaLetve(ctx, id) {
			t.Errorf("%q: letva bez oznake ne vodi kotu", id)
		}
	}
	if kota, err := o.rs.pravoNaLetvu(ctx, o.adminOvl, &models.Reading{StructureID: o.csProbni.ID.String()}); err != nil || kota {
		t.Errorf("objekt: kota %v, %v", kota, err)
	}
	if kota, err := o.rs.pravoNaLetvu(ctx, o.adminOvl, &models.Reading{StationID: "nije-uuid"}); err == nil || kota {
		t.Errorf("neispravna postaja: kota %v, %v", kota, err)
	}
}

// Stranica letve čita odbačene vrijednosti kroz servis
func TestSazetakOdbacenihKrozServis(t *testing.T) {
	o := novaOkolinaCitanja(t)
	ctx := context.Background()
	letva := "station:" + o.primjerovo.ID.String()
	if s, err := o.rs.SazetakOdbacenih(ctx, letva); err != nil || s != nil {
		t.Fatalf("bez odbačenih: %+v, %v", s, err)
	}
	if err := o.readings.ZabiljeziOdbacene(ctx, []models.OdbacenaVrijednost{
		{Letva: letva, Kad: time.Now().Add(-time.Hour), Polje: models.PoljeVodostaj, Opis: "−1102 cm", Izvor: "hdv.voda.hr"},
	}); err != nil {
		t.Fatal(err)
	}
	if s, err := o.rs.SazetakOdbacenih(ctx, letva); err != nil || s == nil || s.Broj != 1 {
		t.Errorf("sažetak: %+v, %v", s, err)
	}
}
