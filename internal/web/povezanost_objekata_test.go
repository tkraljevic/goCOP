package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Stranica povezanosti pokazuje svaku ustavu i crpnu stanicu s vodotokom i
// letvom obje strane, ističe što nedostaje i nudi „Uredi” onome tko smije
func TestStranicaPovezanostiObjekata(t *testing.T) {
	cs := models.Structure{Name: "CS Proba", Kind: models.StructureKindPumpingStation, AreaID: 16, Reverzibilna: true, ImaSifon: true}
	ustava := models.Structure{Name: "Ustava Proba", Kind: models.StructureKindSluice, AreaID: 16}
	potpun := RedakPovezanosti{Objekt: cs, Vodotok: "Kanal Proba", VodotokDruge: "Dunavac Proba", Letva: "Proba kanal", LetvaDruge: "Proba dunavac", MozeUredjivati: true}
	nepotpun := RedakPovezanosti{Objekt: ustava, Vodotok: "Kanal Proba", Letva: "Proba kanal", USklopu: "CS Proba"}
	if potpun.Nepotpun() || !nepotpun.Nepotpun() {
		t.Fatal("ocjena potpunosti")
	}
	html := iscrtaj(t, "povezanost_objekata.html", PovezanostPageData{CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Redovi: []RedakPovezanosti{potpun, nepotpun}, Nepotpunih: 1})
	for _, zeli := range []string{"CS Proba", "reverzibilna", "sa sifonom", "Dunavac Proba", "Ustava Proba", "u sklopu: CS Proba", "vodotok nedostaje", "letva nedostaje", "1 nepotpunih", "/edit"} {
		if !strings.Contains(html, zeli) {
			t.Errorf("stranica povezanosti nema %q", zeli)
		}
	}
	if strings.Count(html, "/edit") != 1 {
		t.Error("„Uredi” se nudi i bez prava uređivanja")
	}
}

// Obrazac objekta nudi vodotoke iz registra za obje strane, a šifra koje
// nema u registru ostaje izabrana uz napomenu
func TestObrazacObjektaNudiVodotokeObjeStrane(t *testing.T) {
	st := &models.Structure{Name: "CS Proba", Kind: models.StructureKindPumpingStation, WatercourseCode: "stari-kod", VodotokDrugeStrane: "dunavac"}
	html := iscrtaj(t, "structure_form.html", StructurePageData{CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Structure: st, IsEdit: true, Vodotoci: []models.Watercourse{{Code: "dunavac", Name: "Dunavac Proba", Kind: "dunavac"}, {Code: "kanal", Name: "Kanal Proba", Kind: "kanal"}}})
	for _, zeli := range []string{`name="vodotok_druge_strane"`, `value="dunavac" selected`, "stari-kod (nije u registru)", `name="reverzibilna"`, `name="ima_sifon"`, `name="sifon_min_cm"`} {
		if !strings.Contains(html, zeli) {
			t.Errorf("obrazac objekta nema %q", zeli)
		}
	}
}

// Stranica povezanosti kroz rukovatelja: crpne stanice i ustave područja s
// nazivima letava umjesto šifri; područje se bira u adresi. Vodotoci se
// čitaju iz registra, a bez registra ponuda je prazna.
func TestPovezanostKrozRukovatelja(t *testing.T) {
	o := novaOkolinaSastavljaca(t)
	ctx := context.Background()
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	cs := &models.Structure{Name: "CS Proba", Kind: models.StructureKindPumpingStation, AreaID: 1, StationID: o.letva.ID.String()}
	ustava := &models.Structure{Name: "Ustava Proba", Kind: models.StructureKindSluice, AreaID: 1}
	for _, s := range []*models.Structure{cs, ustava} {
		if err := o.structures.Create(ctx, admin, s); err != nil {
			t.Fatal(err)
		}
	}
	h := NewStructuresHandler(o.structures, o.stations, nil, o.users, nil, nil, nil)
	if h.vodotoci(ctx) != nil {
		t.Error("bez registra vodotoka ponuda nije prazna")
	}
	h.SetPovezanost(nil, o.tmpl("povezanost_objekata.html"))
	for _, podrucje := range []string{"1", "nije-broj"} {
		r := httptest.NewRequest(http.MethodGet, "/registri/povezanost-objekata?area="+url.QueryEscape(podrucje), nil)
		r = r.WithContext(context.WithValue(ctx, contextKeyPerms, admin))
		w := httptest.NewRecorder()
		h.ShowPovezanost(w, r)
		s := w.Body.String()
		if w.Code != http.StatusOK || !strings.Contains(s, "CS Proba") || !strings.Contains(s, "Ustava Proba") || !strings.Contains(s, o.letva.Name) {
			t.Errorf("područje %q: %d\n%s", podrucje, w.Code, s)
		}
	}
}

// Najniži vodostaj za sifon čita se samo uz kvačicu „Ima sifon”
func TestNajniziZaSifon(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/structures", strings.NewReader(url.Values{"sifon_min_cm": {"250"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if v := najniziZaSifon(r, true); v == nil || *v != 250 {
		t.Errorf("sa sifonom: %v", v)
	}
	if v := najniziZaSifon(r, false); v != nil {
		t.Errorf("bez sifona: %v", *v)
	}
}

// Ponuda vodotoka na obrascu objekta dolazi iz registra; kad registar ne
// odgovara, ponuda je prazna, a obrazac i dalje radi
func TestVodotociZaObrazacObjekta(t *testing.T) {
	o := novaOkolinaSastavljaca(t)
	ctx := context.Background()
	vodotoci := service.NewWatercourseService(repository.NewWatercourseRepository(o.baza, ledger.New(o.baza, "test")))
	if err := vodotoci.CreateWatercourse(ctx, &models.UserPermissions{IsGlobalAdmin: true}, &models.Watercourse{Code: "kanal-proba", Name: "Kanal Proba", OfficialName: "kanal Proba", Kind: "kanal"}); err != nil {
		t.Fatal(err)
	}
	h := NewStructuresHandler(o.structures, o.stations, nil, o.users, nil, nil, nil)
	h.SetPovezanost(vodotoci, nil)
	v := h.vodotoci(ctx)
	if !imaVodotok(v, "kanal-proba") || imaVodotok(v, "nema-ga") {
		t.Errorf("ponuda vodotoka: %+v", v)
	}
	if err := o.baza.Close(); err != nil {
		t.Fatal(err)
	}
	if v := h.vodotoci(ctx); v != nil {
		t.Errorf("nedostupan registar: %+v", v)
	}
}
