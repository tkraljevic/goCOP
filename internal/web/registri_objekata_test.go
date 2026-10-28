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
	for _, st := range []*models.Structure{nasip, stanica} {
		if err := o.structures.Create(ctx, admin, st); err != nil {
			t.Fatal(err)
		}
	}
	sesija := &models.Session{UserID: o.osobe["zamjenik"].ID, IPAddress: "192.168.1.50", UserAgent: "test", ExpiresAt: time.Now().Add(time.Hour)}
	if err := o.sesije.CreateSession(sesija); err != nil {
		t.Fatal(err)
	}
	otvori := func(put string) string {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, put, nil)
		r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: sesija.ID.String()})
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, r)
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
	if b := otvori("/structures"); !strings.Contains(b, "Nasip Primjerovo") || !strings.Contains(b, "CS Primjerovo") || !strings.Contains(b, `href="/nasipi"`) {
		t.Error("popis svih objekata nema obje vrste ili prečac na nasipe")
	}
	if b := otvori("/structures/" + nasip.ID.String()); !strings.Contains(b, `<a href="/nasipi">Nasipi</a>`) || strings.Contains(b, "Upiši očitanje") {
		t.Error("stranica nasipa ne vodi natrag na nasipe ili nudi očitanje")
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
	if l := brisi("nije-id", admin); !strings.HasPrefix(l, "/structures?") {
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
	if len(registriObjekataUIzborniku()) != 4 || !jeNavObjekta(navObjekta(models.StructureKindDam)) || !jeNavObjekta("structures") || jeNavObjekta("stations") {
		t.Error("izbornik registara objekata nije složen kako treba")
	}
}
