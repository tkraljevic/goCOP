package web

// Promjene prava koje čekaju potvrdu na stranici Stanje razmjene
// (sinkronizacija_prava.go)

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/razmjena"
	"gocop/internal/repository"
)

// primiPromjenuPrava upisuje račun na ovom računalu, pa primi verziju s
// drugog računala koja mu daje zastavicu globalnog administratora
func primiPromjenuPrava(t *testing.T, baza *sql.DB, rec *ledger.Recorder, ime string) ledger.Version {
	t.Helper()
	u := models.User{ID: uuid.New(), Username: ime, FullName: "Račun " + ime, PasswordHash: "s", IsActive: true,
		OrgType: models.OrgHrvatskeVode, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := repository.NewUserRepository(baza, rec).CreateUser(&u, nil); err != nil {
		t.Fatal(err)
	}
	u.IsGlobalAdmin = true
	p, err := json.Marshal(struct {
		models.User
		PasswordHash string `json:"password_hash"`
	}{u, u.PasswordHash})
	if err != nil {
		t.Fatal(err)
	}
	v := ledger.Version{VersionID: uuid.Must(uuid.NewV7()).String(), Entity: repository.EntityUsers, EntityID: u.ID.String(),
		NodeID: "laptop-baranja", Payload: p, CreatedAt: time.Now(), SchemaVersion: ledger.SchemaVersion}
	if _, err := rec.Apply(context.Background(), []ledger.Version{v}); err != nil {
		t.Fatal(err)
	}
	if err := repository.ApplyVersions(context.Background(), baza, rec, []ledger.Version{v}); err != nil {
		t.Fatal(err)
	}
	return v
}

func zastavicaRacuna(t *testing.T, baza *sql.DB, ime string) bool {
	t.Helper()
	var z bool
	if err := baza.QueryRow(`SELECT is_global_admin FROM users WHERE username = ?`, ime).Scan(&z); err != nil {
		t.Fatal(err)
	}
	return z
}

// Globalni administrator na stranici vidi čiji račun, što se mijenja i s
// kojeg računala, i svojim očima potvrđuje ili odbija; tuđim očima vidi
// popis bez gumba i odluka se ne prima, a drugi korisnik bloka nema.
// Potvrda stavlja zastavicu na površinu, odbijanje bilježi tko je odbio.
func TestPromjenePravaNaStanjuRazmjene(t *testing.T) {
	s, baza := cvorIBazaZaWeb(t, "cop-osijek")
	kljuc, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePeer(context.Background(), peers.Peer{NodeID: "laptop-baranja", Name: "Laptop Baranja", PublicKey: razmjena.PublicKeyString(kljuc)}); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "cop-osijek")
	prva := primiPromjenuPrava(t, baza, rec, "prvi")
	druga := primiPromjenuPrava(t, baza, rec, "drugi")
	if zastavicaRacuna(t, baza, "prvi") {
		t.Fatal("zastavica je na površini bez potvrde")
	}

	h := NewSyncHandler(s, predlozakStranice(t, "sinkronizacija.html"))
	h.prava = &potvrdePrava{baza: func() *sql.DB { return baza }, rec: rec}
	stranica := func(u uvjetiZahtjeva) string {
		t.Helper()
		w := httptest.NewRecorder()
		h.ShowDashboard(w, zahtjevPostavki(http.MethodGet, "/sinkronizacija", nil, u))
		if w.Code != http.StatusOK {
			t.Fatalf("stranica %d\n%s", w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	odluci := func(u uvjetiZahtjeva, odluka, verzija string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		h.handleOdlukaPrava(w, zahtjevPostavki(http.MethodPost, "/sinkronizacija/prava", url.Values{"odluka": {odluka}, "verzija": {verzija}}, u))
		return w
	}

	str := stranica(kaoAdmin)
	for _, ima := range []string{"Promjene prava koje čekaju potvrdu", "Račun prvi (prvi)", "dobiva zastavicu globalnog administratora",
		"Laptop Baranja", `value="` + prva.VersionID + `"`, ">Potvrdi</button>", ">Odbij</button>"} {
		if !strings.Contains(str, ima) {
			t.Errorf("stranica administratora nema %q", ima)
		}
	}
	tudjim := kaoAdmin
	tudjim.tudjim = true
	str = stranica(tudjim)
	if !strings.Contains(str, "Račun prvi (prvi)") || strings.Contains(str, ">Potvrdi</button>") || !strings.Contains(str, "svojim očima") {
		t.Error("tuđim očima: popis bez gumba i s razlogom")
	}
	obican := uvjetiZahtjeva{perms: &models.UserPermissions{}, adresa: adresaLokalno, korisnik: adminIvic}
	if strings.Contains(stranica(obican), "Promjene prava koje čekaju potvrdu") {
		t.Error("korisnik koji nije globalni administrator vidi promjene prava")
	}

	for _, u := range []uvjetiZahtjeva{tudjim, obican} {
		if w := odluci(u, "potvrdi", prva.VersionID); w.Code != http.StatusForbidden {
			t.Errorf("odluka bez prava: %d", w.Code)
		}
	}
	if w := odluci(kaoAdmin, "obrisi", prva.VersionID); w.Code != http.StatusBadRequest {
		t.Errorf("nepoznata odluka: %d", w.Code)
	}
	if zastavicaRacuna(t, baza, "prvi") {
		t.Fatal("zastavica je stigla bez odluke administratora svojim očima")
	}

	w := odluci(kaoAdmin, "potvrdi", prva.VersionID)
	if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "success=") || !zastavicaRacuna(t, baza, "prvi") {
		t.Errorf("potvrda: %d %s", w.Code, w.Header().Get("Location"))
	}
	if w := odluci(kaoAdmin, "potvrdi", prva.VersionID); !strings.Contains(w.Header().Get("Location"), "error=") {
		t.Errorf("druga potvrda iste promjene: %s", w.Header().Get("Location"))
	}
	if w := odluci(kaoAdmin, "odbij", druga.VersionID); !strings.Contains(w.Header().Get("Location"), "success=") || zastavicaRacuna(t, baza, "drugi") {
		t.Errorf("odbijanje: %s", w.Header().Get("Location"))
	}
	str = stranica(kaoAdmin)
	for _, ima := range []string{"Nijedna promjena prava ne čeka potvrdu", "Zadnje odluke", "potvrđeno", "odbijeno", "Ivan Ivić (iivic)"} {
		if !strings.Contains(str, ima) {
			t.Errorf("stranica nakon odluka nema %q", ima)
		}
	}

	// bez tablice odluka stranica javi grešku i ne pada
	if _, err := baza.Exec(`DROP TABLE prava_na_potvrdi`); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stranica(kaoAdmin), "Promjene prava nisu pročitane") {
		t.Error("greška čitanja nije na stranici")
	}
}

// U mreži se o promjeni prava odlučuje na računalu s ključem mreže: ondje
// gumbi postoje, a izvana odluka traži lozinku računa; odluka se potpiše
// ključem mreže. Računalo mreže bez ključa pokazuje popis bez gumba, kaže
// gdje se odlučuje i odluku ne prima.
func TestPromjenePravaUMrezi(t *testing.T) {
	s, baza := cvorIBazaZaWeb(t, "cop-osijek")
	if err := s.CreateNetwork(context.Background(), "Hrvatske vode"); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "cop-osijek")
	v := primiPromjenuPrava(t, baza, rec, "prvi")
	h := NewSyncHandler(s, predlozakStranice(t, "sinkronizacija.html"))
	h.prava = &potvrdePrava{baza: func() *sql.DB { return baza }, rec: rec, potpis: s,
		lozinka: func(_ uuid.UUID, l string) error {
			if l != "tocna-lozinka" {
				return errors.New("lozinka nije točna")
			}
			return nil
		}}
	izvana := kaoAdmin
	izvana.adresa = adresaIzvana
	stranica := func(u uvjetiZahtjeva) string {
		t.Helper()
		w := httptest.NewRecorder()
		h.ShowDashboard(w, zahtjevPostavki(http.MethodGet, "/sinkronizacija", nil, u))
		return w.Body.String()
	}
	odluci := func(u uvjetiZahtjeva, obrazac url.Values) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		h.handleOdlukaPrava(w, zahtjevPostavki(http.MethodPost, "/sinkronizacija/prava", obrazac, u))
		return w
	}
	str := stranica(izvana)
	for _, ima := range []string{"Ovo računalo drži ključ mreže", `name="lozinka_racuna"`, ">Potvrdi</button>"} {
		if !strings.Contains(str, ima) {
			t.Errorf("računalo s ključem izvana nema %q", ima)
		}
	}
	if w := odluci(izvana, url.Values{"odluka": {"potvrdi"}, "verzija": {v.VersionID}}); !strings.Contains(w.Header().Get("Location"), "error=") {
		t.Errorf("izvana bez lozinke: %s", w.Header().Get("Location"))
	}
	if w := odluci(izvana, url.Values{"odluka": {"potvrdi"}, "verzija": {v.VersionID}, "lozinka_racuna": {"kriva"}}); !strings.Contains(w.Header().Get("Location"), "error=") {
		t.Errorf("izvana s krivom lozinkom: %s", w.Header().Get("Location"))
	}
	if zastavicaRacuna(t, baza, "prvi") {
		t.Fatal("zastavica bez točne lozinke")
	}
	w := odluci(izvana, url.Values{"odluka": {"potvrdi"}, "verzija": {v.VersionID}, "lozinka_racuna": {"tocna-lozinka"}})
	if !strings.Contains(w.Header().Get("Location"), "success=") || !zastavicaRacuna(t, baza, "prvi") {
		t.Fatalf("potvrda ključem: %s", w.Header().Get("Location"))
	}
	var potvrda int
	if err := baza.QueryRow(`SELECT COUNT(*) FROM record_versions WHERE entity = ?`, repository.EntityPotvrdePrava).Scan(&potvrda); err != nil || potvrda != 1 {
		t.Errorf("potpisanih potvrda %d (%v)", potvrda, err)
	}

	// računalo iste mreže bez ključa
	druga := primiPromjenuPrava(t, baza, rec, "drugi")
	h.prava.potpis = nil
	str = stranica(kaoAdmin)
	if !strings.Contains(str, "Promjenu potvrđuje ili odbija globalni administrator na računalu koje drži ključ mreže") ||
		strings.Contains(str, ">Potvrdi</button>") || !strings.Contains(str, "Račun drugi (drugi)") {
		t.Error("računalo bez ključa: popis bez gumba i s porukom gdje se odlučuje")
	}
	if w := odluci(kaoAdmin, url.Values{"odluka": {"potvrdi"}, "verzija": {druga.VersionID}}); w.Code != http.StatusForbidden {
		t.Errorf("odluka na računalu bez ključa: %d", w.Code)
	}
	if zastavicaRacuna(t, baza, "drugi") {
		t.Error("zastavica na računalu bez ključa")
	}
}
