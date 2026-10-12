package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Ono što obrazac akta pita servis prije sastavljanja: je li obrana u
// sektoru aktivna, za koje je dionice letva mjerodavna i koje je zadnje
// očitanje.

// Aktivnu obranu sektora javlja pravilo koje servis dobije; bez servisa ili
// bez pravila obrana nije aktivna i ništa se ne brani.
func TestAktivnaObranaPoPravilu(t *testing.T) {
	ctx := context.Background()
	var nema *service.AktService
	if j := nema.AktivnaObrana(ctx, "P"); j != nil {
		t.Errorf("bez servisa: %+v", j)
	}
	o := novaOkolinaAkta(t)
	if j := o.akti.AktivnaObrana(ctx, "P"); j != nil {
		t.Errorf("bez pravila: %+v", j)
	}
	otvoren := time.Date(2026, 10, 1, 7, 0, 0, 0, models.Zagreb)
	dnevnik := &models.Journal{ID: uuid.NewString(), CentarSektor: "P", StartedAt: &otvoren}
	o.akti.SetObrana(func(_ context.Context, sektor string) *models.Journal {
		if sektor == "P" {
			return dnevnik
		}
		return nil
	}, nil)
	if j := o.akti.AktivnaObrana(ctx, "P"); j != dnevnik {
		t.Errorf("sektor P: %+v", j)
	}
	if j := o.akti.AktivnaObrana(ctx, "Q"); j != nil {
		t.Errorf("sektor Q bez obrane: %+v", j)
	}
}

// Dionice letve su one zadane na letvi, a bez zadanih one za koje je letva
// upisana kao mjerodavna; šifre kojih nema u registru dionica se preskaču.
func TestDioniceLetveZaObrazac(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	sifre := func(dionice []models.Section) string {
		var out []string
		for _, d := range dionice {
			out = append(out, d.Code+" "+d.Description)
		}
		return strings.Join(out, "; ")
	}
	if got := sifre(o.akti.DioniceLetve(ctx, o.letva)); got != "P.1.1 rijeka Primjerica; P.1.2 kanal Probni" {
		t.Errorf("mjerodavna letva: %s", got)
	}
	zadane := *o.letva
	zadane.SectionCodes = []string{"P.1.2", "P.9.9"}
	if got := sifre(o.akti.DioniceLetve(ctx, &zadane)); got != "P.1.2 kanal Probni" {
		t.Errorf("zadane dionice: %s", got)
	}
	nevezana := &models.Station{ID: uuid.New(), Name: "Probni most"}
	if got := o.akti.DioniceLetve(ctx, nevezana); len(got) != 0 {
		t.Errorf("letva bez dionica: %s", sifre(got))
	}
}

// Zadnje očitanje letve je najnovije upisano; letva bez očitanja nema ga i
// to nije greška, a greška čitanja se vraća.
func TestZadnjeOcitanjeZaObrazac(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	r, err := o.akti.ZadnjeOcitanje(ctx, o.letva.ID.String())
	if err != nil || r == nil || r.LevelCm == nil || *r.LevelCm != 320 {
		t.Fatalf("zadnje očitanje: %+v %v", r, err)
	}
	if r, err := o.akti.ZadnjeOcitanje(ctx, uuid.NewString()); err != nil || r != nil {
		t.Errorf("letva bez očitanja: %+v %v", r, err)
	}
	pokvariTablicu(t, o, "readings")
	if _, err := o.akti.ZadnjeOcitanje(ctx, o.letva.ID.String()); err == nil {
		t.Error("greška čitanja nije vraćena")
	}
}
