package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Administrativna organizacija po crtežu (17a, 2a), kroz pravi poslužitelj: globalni
// administrator vidi Novi sektor, Novo područje, Uredi i uvoz pod CSV, a rukovoditelj
// samo podatke i izvoz.
func TestOrganizacijaPoCrtezuKrozRutu(t *testing.T) {
	o := novaOkolinaSastavljaca(t)
	admin := &models.User{ID: uuid.New(), Username: "aadmin", FullName: "Ante Antić", IsGlobalAdmin: true, IsActive: true}
	if err := o.userRepo.CreateUser(admin, nil); err != nil {
		t.Fatal(err)
	}
	o.osobe["administrator"] = admin
	if _, err := o.baza.Exec(`UPDATE areas SET vgi_phone = '031/000-001', subcenter = 'Podcentar Primjerovo', latitude = 45.5548793, longitude = 18.6953685 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := o.baza.Exec(`UPDATE sectors SET name = 'Sektor P — Primjerica i okolica', phone = '031/000-002' WHERE id = 'P'`); err != nil {
		t.Fatal(err)
	}
	org := service.NewOrgService(repository.NewOrgRepository(o.baza, ledger.New(o.baza, "test")), service.NewSSEBroker())
	s, err := NewServer("", o.auth, o.users, nil, nil, o.stations, nil, o.structures, o.readings, nil, o.moduli, nil, nil, org,
		SupportContact{}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	stranica := func(tko string) string {
		t.Helper()
		sesija := &models.Session{UserID: o.osobe[tko].ID, IPAddress: "192.168.1.50", UserAgent: "test", ExpiresAt: time.Now().Add(time.Hour)}
		if err := o.sesije.CreateSession(sesija); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodGet, "/organizacija", nil)
		r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: sesija.ID.String()})
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: /organizacija %d", tko, w.Code)
		}
		return w.Body.String()
	}

	zajednicko := []string{
		`<a href="/registri" class="povratna-poveznica">← Registri</a>`,
		`Razina 1 ·`, `Razina 2 ·`, `Razina 3 ·`,
		// pločica: slovo, naziv bez oznake, centar s telefonom, broj područja
		`<span class="org-slovo">P</span><span class="org-sektor-ime">Primjerica i okolica</span>`,
		`COP Primjerovo · 031/000-002`, `1 BP</span>`,
		// redak područja: naziv vodi na dionice, ispostava s telefonom, točka sa zarezom
		`href="/sections?sector=P&area=1"`, `VGI Primjerica`, `tel:031/000-001`, `Podcentar Primjerovo`, `45,555 · 18,695`,
		`href="/organizacija/sektori.csv"`, `href="/organizacija/podrucja.csv"`,
	}
	samoAdmin := []string{
		`href="/organizacija/sektori/new"`, `href="/organizacija/podrucja/new"`, `href="/organizacija/sektori/new?razina=1"`,
		`href="/organizacija/podrucja/1/edit"`, `name="kind" value="sektori"`, `name="kind" value="podrucja"`,
		`action="/organizacija/podrucja/koordinate"`, `href="/administracija/nazivi"`,
	}
	for tko, admin := range map[string]bool{"administrator": true, "rukovoditelj": false} {
		html := stranica(tko)
		for _, mora := range zajednicko {
			if !strings.Contains(html, mora) {
				t.Errorf("%s: nema %q", tko, mora)
			}
		}
		for _, samo := range samoAdmin {
			if strings.Contains(html, samo) != admin {
				t.Errorf("%s: %q vidljivo %v, želi %v", tko, samo, !admin, admin)
			}
		}
		if strings.Contains(html, "/organizacija/sektori/delete") || strings.Contains(html, "/organizacija/podrucja/delete") {
			t.Errorf("%s: brisanje je i dalje na popisu, a pripada obrascu", tko)
		}
	}
}

func TestKratkiNazivSektora(t *testing.T) {
	for naziv, zeli := range map[string]string{
		"Sektor B — Dunav i donja Drava": "Dunav i donja Drava",
		"Sektor B":                       "Sektor B",
		"Dunav — donja Drava":            "Dunav — donja Drava", // crta, ali ne iza oznake
		"Sektor B — ":                    "Sektor B — ",
	} {
		if dobio := (SectorView{Sector: models.Sector{ID: "B", Name: naziv}}).KratkiNaziv(); dobio != zeli {
			t.Errorf("%q: %q, želi %q", naziv, dobio, zeli)
		}
	}
}
