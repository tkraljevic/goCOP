package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Svaka vrsta objekta ima svoj registar: popis i pretraga nasipa ne
// pokazuju crpne stanice, a stranica nasipa vodi natrag na nasipe.
func TestRegistriObjekataPoVrstama(t *testing.T) {
	o := novaOkolinaSastavljaca(t)
	s, err := NewServer("", o.auth, o.users, nil, nil, o.stations, nil, o.structures, o.readings, nil, o.moduli, nil, nil, nil,
		SupportContact{}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	nasip := &models.Structure{Code: "nasip-primjerovo", Name: "Nasip Primjerovo", Kind: models.StructureKindEmbankment, SectorID: "P", AreaID: 1}
	stanica := &models.Structure{Code: "cs-primjerovo", Name: "CS Primjerovo", Kind: models.StructureKindPumpingStation, SectorID: "P", AreaID: 1}
	sifon := &models.Structure{Code: "sifon-primjerovo", Name: "Sifon Primjerovo", Kind: models.StructureKindSiphon, SectorID: "P", AreaID: 1}
	for _, st := range []*models.Structure{nasip, stanica, sifon} {
		if err := o.structures.Create(ctx, admin, st); err != nil {
			t.Fatal(err)
		}
	}
	sesija := &models.Session{UserID: o.osobe["zamjenik"].ID, IPAddress: "192.168.1.50", UserAgent: "test", ExpiresAt: time.Now().Add(time.Hour)}
	if err := o.sesije.CreateSession(sesija); err != nil {
		t.Fatal(err)
	}
	posalji := func(put string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, put, nil)
		r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: sesija.ID.String()})
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, r)
		return w
	}
	otvori := func(put string) string {
		t.Helper()
		w := posalji(put)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d", put, w.Code)
		}
		return w.Body.String()
	}
	if b := otvori("/nasipi?q=Primjerovo"); !strings.Contains(b, "Nasip Primjerovo") || strings.Contains(b, "CS Primjerovo") || !strings.Contains(b, "Novi nasip") {
		t.Error("registar nasipa pokazuje i druge vrste ili nema svoj gumb")
	}
	if b := otvori("/crpne-stanice"); !strings.Contains(b, "CS Primjerovo") || strings.Contains(b, "Nasip Primjerovo") {
		t.Error("registar crpnih stanica pokazuje i nasipe")
	}
	if b := otvori("/ostali-objekti"); !strings.Contains(b, "Sifon Primjerovo") || strings.Contains(b, "Nasip Primjerovo") || !strings.Contains(b, `name="kind"`) {
		t.Error("ostali objekti ne pokazuju sifon, pokazuju nasip ili nemaju izbor vrste")
	}
	for put, cilj := range map[string]string{"/structures?kind=" + models.StructureKindSluice + "&area=1": "/ustave?area=1", "/structures": "/nasipi",
		"/structures?kind=" + models.StructureKindWeir: "/ostali-objekti?kind=" + models.StructureKindWeir} {
		if w := posalji(put); w.Code != http.StatusSeeOther || w.Header().Get("Location") != cilj {
			t.Errorf("%s: %d → %q, htio %q", put, w.Code, w.Header().Get("Location"), cilj)
		}
	}
	if b := otvori("/nasipi/" + nasip.ID.String()); !strings.Contains(b, `<a href="/nasipi">Nasipi</a>`) || strings.Contains(b, "Upiši očitanje") ||
		!strings.Contains(b, `href="/nasipi/`+nasip.ID.String()+`/edit"`) {
		t.Error("stranica nasipa ne vodi natrag na nasipe, nudi očitanje ili uređivanje nije u registru nasipa")
	}
	if b := otvori("/nasipi?q=Primjerovo"); !strings.Contains(b, `href="/nasipi/`+nasip.ID.String()+`"`) {
		t.Error("popis nasipa ne vodi na adresu nasipa u registru")
	}
	// stare poveznice (dionice, održavanje, skladište) vode na adresu u registru
	for stara, nova := range map[string]string{
		"/structures/" + nasip.ID.String():                         "/nasipi/" + nasip.ID.String(),
		"/structures/" + stanica.ID.String() + "/edit":             "/crpne-stanice/" + stanica.ID.String() + "/edit",
		"/structures/" + nasip.ID.String() + "?success=Spremljeno": "/nasipi/" + nasip.ID.String() + "?success=Spremljeno",
	} {
		if w := posalji(stara); w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != nova {
			t.Errorf("%s: %d → %q, htio %q", stara, w.Code, w.Header().Get("Location"), nova)
		}
	}
	if b := otvori("/structures/new?kind=" + models.StructureKindSluice); !strings.Contains(b, "Nova ustava") {
		t.Error("novi zapis iz registra ustava ne nosi vrstu")
	}
}

// Nakon brisanja nasipa program se vraća na registar nasipa
func TestBrisanjeObjektaVodiNaRegistarVrste(t *testing.T) {
	o := novaOkolinaSastavljaca(t)
	ctx := context.Background()
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	nasip := &models.Structure{Code: "nasip-brisanje", Name: "Nasip Brisanje", Kind: models.StructureKindEmbankment, SectorID: "P", AreaID: 1}
	if err := o.structures.Create(ctx, admin, nasip); err != nil {
		t.Fatal(err)
	}
	h := NewStructuresHandler(o.structures, nil, nil, nil, nil, nil, nil)
	brisi := func(id string, perms *models.UserPermissions) string {
		r := httptest.NewRequest(http.MethodPost, "/structures/delete", strings.NewReader("id="+id))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r = r.WithContext(context.WithValue(ctx, contextKeyPerms, perms))
		w := httptest.NewRecorder()
		h.HandleDelete(w, r)
		return w.Header().Get("Location")
	}
	if l := brisi("nije-id", admin); !strings.HasPrefix(l, "/nasipi?") {
		t.Errorf("neispravan id: %q", l)
	}
	if l := brisi(nasip.ID.String(), &models.UserPermissions{}); !strings.HasPrefix(l, "/structures/"+nasip.ID.String()) {
		t.Errorf("brisanje bez prava: %q", l)
	}
	if l := brisi(nasip.ID.String(), admin); !strings.HasPrefix(l, "/nasipi?") {
		t.Errorf("nakon brisanja nasipa: %q", l)
	}
}

func TestOznakeIzbornikaObjekata(t *testing.T) {
	if len(registriObjekataUIzborniku()) != 5 || !jeNavObjekta(navObjekta(models.StructureKindDam)) || jeNavObjekta("stations") {
		t.Error("izbornik registara objekata nije složen kako treba")
	}
}

func TestLetvaObjekta(t *testing.T) {
	o := novaOkolinaSastavljaca(t)
	h := NewStructuresHandler(o.structures, o.stations, nil, nil, nil, nil, nil)
	ctx := context.Background()
	for _, id := range []string{"", "nije-id", "11111111-2222-3333-4444-555555555555"} {
		if l := h.letvaObjekta(ctx, id); l != nil {
			t.Errorf("%q: dobio letvu %s", id, l.Name)
		}
	}
	if l := h.letvaObjekta(ctx, o.letva.ID.String()); l == nil || l.ID != o.letva.ID {
		t.Errorf("postojeća letva nije pročitana: %+v", l)
	}
}
