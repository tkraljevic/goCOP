package web

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// nePrimaNista: akt čiji elektronički potpis ne vrijedi i poništen akt ne
// šalju se, ni probno, a provjeru radi poslužitelj, ne samo onemogućen gumb
// (Odluke (6. 11. 2026.)). Na poništenom aktu umjesto obrasca stoji popis
// adresa na koje je akt otišao prije poništenja. Akt je već poslan, pa se
// provjerava da poslužitelj e-pošte nakon toga ne primi ništa.
func nePrimaNista(t *testing.T, ctx context.Context, repo *repository.AktiRepository, akti *service.AktService, id string,
	srv *posta.ProbniPosluzitelj, post func(string, url.Values) string, stranica func(string) string) {
	t.Helper()
	poslano := len(srv.Poruke())
	posalji := func(sto, mora string) {
		t.Helper()
		for _, v := range []url.Values{{"adresa": {"osjecko-baranjska@policija.hr"}}, {"proba": {"1"}}} {
			if loc := post("/akti/"+id+"/posalji", v); !strings.Contains(loc, "error=") || !strings.Contains(loc, mora) {
				t.Errorf("%s, %v: %s", sto, v, loc)
			}
		}
	}
	spremi := func(a *models.Akt) {
		t.Helper()
		if err := repo.SaveAkt(ctx, a); err != nil {
			t.Fatal(err)
		}
	}

	a, err := akti.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	izvorni := *a
	drugi := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	a.Potpis = base64.StdEncoding.EncodeToString(ed25519.Sign(drugi, a.PorukaPotpisa()))
	spremi(a)
	if p := service.ProvjeriPotpis(a); p != service.PotpisNeVrijedi {
		t.Fatalf("potpis drugim ključem: %s", p)
	}
	posalji("potpis ne vrijedi", "elektronički potpis ne vrijedi")
	if s := stranica(id); !strings.Contains(s, "Ne može se poslati jer elektronički potpis ne vrijedi. Prvo treba provjeriti akt.") || !strings.Contains(s, `id="akt-posalji" disabled>`) {
		t.Error("stranica akta s nevažećim potpisom ne kaže zašto se ne šalje ili gumb nije onemogućen")
	}

	a = &izvorni
	a.Storno = &models.StornoAkta{Ponistio: "Voditelj COP-a", PonistenoAt: time.Now(), Razlog: "Akt nije trebao biti izdan."}
	spremi(a)
	posalji("poništen akt", "poništen i više se ne šalje")
	s := stranica(id)
	if !strings.Contains(s, "Poslano prije poništenja") || !strings.Contains(s, "osjecko-baranjska@policija.hr") || !strings.Contains(s, "dodatno upisano") ||
		strings.Contains(s, `id="akt-slanje-obrazac"`) || strings.Contains(s, "nije prošlo") {
		t.Error("poništen akt: nema popisa poslanog prije poništenja, ili je ostao obrazac za slanje")
	}
	if n := len(srv.Poruke()); n != poslano {
		t.Errorf("poslužitelj e-pošte primio je još %d poruka", n-poslano)
	}
}
