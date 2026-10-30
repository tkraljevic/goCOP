package web

import (
	"context"
	"net/http"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Odluke (30. 10. 2026.), 11: zasebnog modula Akti nema, pa akti,
// vodočuvarski dnevnik i događanja traže modul Dnevnici, i izravnom
// adresom i u izvozu
func TestAktiVodocuvarIDogadjanjaPodDnevnicima(t *testing.T) {
	for put, modul := range map[string]string{
		"/akti":                       models.ModuleJournals,
		"/akti/novi":                  models.ModuleJournals,
		"/akti/0190a000/akt.pdf":      models.ModuleJournals,
		"/vodocuvar":                  models.ModuleJournals,
		"/vodocuvar/dan":              models.ModuleJournals,
		"/vodocuvar/knjiga.pdf":       models.ModuleJournals,
		"/dogadjanja":                 models.ModuleJournals,
		"/dogadjanja.xlsx":            models.ModuleJournals,
		"/aktivnosti":                 "",
		"/vodocuvari":                 "",
		"/prijave":                    "",
		"/teren":                      models.ModuleField,
		"/administracija/akti-uvjeti": models.ModuleAdmin,
	} {
		if got := moduleForPath(put); got != modul {
			t.Errorf("%s: modul %q, a treba %q", put, got, modul)
		}
	}
}

// Kroz pravu provjeru prijave: bez modula Dnevnici akti, događanja i tuđi
// vodočuvarski dnevnik daju 403. Vodočuvar svoj dnevnik vodi i bez modula
// (Odluke (30. 10. 2026.), 8), ali akte i događanja ne otvara. S modulom i
// administratoru sve prolazi. Osobe su izmišljene.
func TestBezDnevnikaAktiIDogadjanjaSeOdbijaju(t *testing.T) {
	o := novaOkolinaPrijave(t)
	if _, err := o.baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo');
		INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', '')`); err != nil {
		t.Fatal(err)
	}
	sektor, podrucje := "P", 1
	duznost := func(ime, lozinka string, uloga models.Role, istece *time.Time) *models.User {
		t.Helper()
		hash, err := o.auth.HashPassword(lozinka)
		if err != nil {
			t.Fatal(err)
		}
		u := &models.User{Username: ime, FullName: ime, PasswordHash: hash, Email: ime + "@example.com", IsActive: true, OrgType: models.OrgHrvatskeVode}
		d := &models.Duty{Role: uloga, ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &podrucje, IsActive: true, IsPrimary: true, ExpiresAt: istece}
		if err := o.repo.CreateUser(u, d); err != nil {
			t.Fatal(err)
		}
		return u
	}
	jucer := time.Now().AddDate(0, 0, -1)
	duznost("ivodocuvar", "ivina-lozinka", models.RoleWaterGuard, nil)        // zadano: Teren, Očitanja
	duznost("bivsi", "bivseg-lozinka", models.RoleWaterGuard, &jucer)         // dužnost je istekla
	duznost("rpodrucja", "rukovoditelja-lozinka", models.RoleAreaLeader, nil) // zadano s Dnevnicima
	// vodočuvaru kojem je administrator Dnevnike izričito sakrio, svoj se
	// dnevnik ne otvara
	skriven := duznost("svodocuvar", "skrivena-lozinka", models.RoleWaterGuard, nil)
	if err := repository.NewModuleRepository(o.baza, ledger.New(o.baza, "test")).SetUserOverride(context.Background(), skriven.ID.String(), nil, []string{models.ModuleJournals}); err != nil {
		t.Fatal(err)
	}
	o.osoba("aivic", "anina-lozinka", "aivic@example.com", false) // zadano: Teren, Očitanja, Registri
	o.osoba("sef", "sefova-lozinka", "sef@example.com", true)
	sesija := func(ime, lozinka string) string {
		t.Helper()
		s, _, err := o.auth.Login(ime, lozinka, "192.168.1.50", "test")
		if err != nil {
			t.Fatal(err)
		}
		return s.ID.String()
	}
	vodocuvar, bivsi, rukovoditelj := sesija("ivodocuvar", "ivina-lozinka"), sesija("bivsi", "bivseg-lozinka"), sesija("rpodrucja", "rukovoditelja-lozinka")
	skrivenom := sesija("svodocuvar", "skrivena-lozinka")
	aivic, sef := sesija("aivic", "anina-lozinka"), sesija("sef", "sefova-lozinka")

	prolazi := func(kolacic, metoda, put string) bool {
		t.Helper()
		z := &mwZapis{}
		w := mwZahtjev(mwPosluzitelj(o, z), metoda, put, kolacic)
		if w.Code != http.StatusNoContent && w.Code != http.StatusForbidden {
			t.Fatalf("%s: neočekivan odgovor %d", put, w.Code)
		}
		return w.Code == http.StatusNoContent && z.pozvan
	}
	for _, x := range []struct {
		metoda, put string
		vodocuvar   bool // vodočuvar sa zadanim modulima
	}{
		{http.MethodGet, "/akti", false},
		{http.MethodGet, "/akti/0190a000/akt.pdf", false},
		{http.MethodGet, "/dogadjanja", false},
		{http.MethodGet, "/dogadjanja.xlsx", false},
		{http.MethodGet, "/vodocuvar", true},
		{http.MethodGet, "/vodocuvar/dan", true},
		{http.MethodPost, "/vodocuvar/spremi", true},
	} {
		if prolazi(aivic, x.metoda, x.put) {
			t.Errorf("%s bez modula Dnevnici prolazi", x.put)
		}
		if prolazi(bivsi, x.metoda, x.put) {
			t.Errorf("%s s isteklom dužnošću vodočuvara prolazi", x.put)
		}
		if prolazi(skrivenom, x.metoda, x.put) {
			t.Errorf("%s vodočuvaru sa sakrivenim Dnevnicima prolazi", x.put)
		}
		if got := prolazi(vodocuvar, x.metoda, x.put); got != x.vodocuvar {
			t.Errorf("%s vodočuvaru: prolaz %v, a treba %v", x.put, got, x.vodocuvar)
		}
		if !prolazi(rukovoditelj, x.metoda, x.put) {
			t.Errorf("%s rukovoditelju područja ne prolazi", x.put)
		}
		if !prolazi(sef, x.metoda, x.put) {
			t.Errorf("%s administratoru ne prolazi", x.put)
		}
	}
	if vodiSvojuKnjigu("/vodocuvar", nil) {
		t.Error("bez osobe")
	}
}
