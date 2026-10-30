package service_test

import (
	"context"
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/service"
)

// Obavijest (npr. da na disku računala ostaje malo mjesta) ide svakoj osobi
// koja je na ovom čvoru spremila lozinku sandučića, s njezina računa na
// njezinu adresu. Isključeni računi i računi bez adrese preskaču se; kome
// poslužitelj odbije lozinku, ostali poruku ipak dobiju.
func TestObavijestiSanducima(t *testing.T) {
	ctx := context.Background()
	o := novaOkolinaOvlasti(t)
	if n, err := o.akti.ObavijestiSanducima(ctx, "Predmet", "Tekst"); n != 0 || err != nil {
		t.Fatalf("bez poslužitelja e-pošte: %d, %v", n, err)
	}

	srv, err := posta.PokreniProbniEWS(`voda.int\vod`, "Domenska-Lozinka-1")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Zatvori()
	pp := srv.Postavke()
	pp.Domena = "voda.int"
	// lozinka spremljena dok poslužitelj nije bio dostupan, pa je nije
	// provjerio; poslužitelj je sad odbija
	kriva := o.osoba(t, "kriva", service.AddDutyRequest{})
	o.spremiBezProvjere(t, kriva, `voda.int\kriva`, "Stara-Lozinka", pp)
	if err := o.akti.SpremiPostu(ctx, o.admin, pp); err != nil {
		t.Fatal(err)
	}
	if n, err := o.akti.ObavijestiSanducima(ctx, "Predmet", "Tekst"); n != 0 || err == nil || !strings.Contains(err.Error(), "kriva:") {
		t.Fatalf("odbijena lozinka: %d, %v", n, err)
	}

	for _, u := range []*models.User{
		o.osoba(t, "ana", service.AddDutyRequest{}),
		o.racun(t, "ugasena", "lozinka-ugasena", "ugasena@voda.hr", false, false),
		o.racun(t, "bezadrese", "lozinka-bezadrese", "", false, true),
	} {
		if _, err := o.akti.SpremiRacunPoste(ctx, u, `voda.int\vod`, "Domenska-Lozinka-1"); err != nil {
			t.Fatal(err)
		}
	}
	n, err := o.akti.ObavijestiSanducima(ctx, "goCOP: na disku računala ostaje malo mjesta", "Na disku čvora Laptop Ana ostalo je 8,2 GB.")
	if n != 1 || err == nil || !strings.Contains(err.Error(), "kriva:") {
		t.Fatalf("poslano %d, %v", n, err)
	}
	poruke := srv.Poruke()
	if len(poruke) != 1 || poruke[0].Od != "ana@voda.hr" || poruke[0].Za != "ana@voda.hr" {
		t.Fatalf("poruke: %+v", poruke)
	}
}
