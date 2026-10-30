package web

// Gumbi za obnovu članstva u Postavkama (G9a): samo administrator; računalo
// s ključem mreže obnovi sebe odmah, član upiše zahtjev, a stranica
// pokazuje rok i gumbe.

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/razmjena"
	"gocop/internal/service"
)

func TestGumbiObnoveClanstva(t *testing.T) {
	ured := primateljNaDaljinu(t)
	clan := svjeziCvorZaWeb(t, "pperic-thinkpad")
	zahtjev, kod, err := clan.NapraviZahtjev()
	if err != nil {
		t.Fatal(err)
	}
	potvrda, err := ured.PrimiZahtjev(context.Background(), zahtjev, kod, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clan.UveziPotvrdu(context.Background(), potvrda); err != nil {
		t.Fatal(err)
	}
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	zovi := func(s *peers.Service, perms *models.UserPermissions, rukovatelj func(*SettingsHandler) http.HandlerFunc, cvor string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/api/network/obnovi", strings.NewReader("{}"))
		if cvor != "" {
			r.SetPathValue("node", cvor)
		}
		r = r.WithContext(context.WithValue(r.Context(), contextKeyPerms, perms))
		w := httptest.NewRecorder()
		rukovatelj(&SettingsHandler{peers: s})(w, r)
		return w
	}
	sebe := func(h *SettingsHandler) http.HandlerFunc { return h.HandleObnoviClanstvo }
	clana := func(h *SettingsHandler) http.HandlerFunc { return h.HandleObnoviClana }

	for ime, w := range map[string]*httptest.ResponseRecorder{
		"obnovi sebe":  zovi(ured, &models.UserPermissions{}, sebe, ""),
		"obnovi člana": zovi(ured, nil, clana, "pperic-thinkpad"),
	} {
		if w.Code != http.StatusForbidden {
			t.Errorf("%s bez administratora: %d", ime, w.Code)
		}
	}
	for _, c := range []struct {
		ime, cvor, ima string
		s              *peers.Service
		rukovatelj     func(*SettingsHandler) http.HandlerFunc
		kod            int
	}{
		{"osnivač obnovi sebe", "", "Članstvo ovog računala obnovljeno je i vrijedi do", ured, sebe, http.StatusOK},
		{"član traži obnovu", "", "Zahtjev za obnovu je upisan", clan, sebe, http.StatusOK},
		{"izvan mreže", "", "Obnova nije zatražena", svjeziCvorZaWeb(t, "samotnjak"), sebe, http.StatusBadRequest},
		{"Obnovi uz sebe", "cop-osijek", "Članstvo računala cop-osijek obnovljeno je", ured, clana, http.StatusOK},
		{"svježe članstvo", "pperic-thinkpad", "Članstvo nije obnovljeno", ured, clana, http.StatusBadRequest},
		{"nepoznato računalo", "nepoznat", "ne vrijedi ni u pričuvi", ured, clana, http.StatusBadRequest},
	} {
		if w := zovi(c.s, admin, c.rukovatelj, c.cvor); w.Code != c.kod || !strings.Contains(w.Body.String(), c.ima) {
			t.Errorf("%s: %d %s", c.ime, w.Code, w.Body.String())
		}
	}
}

// Postavke pokazuju rok ovog računala s gumbom, a uz člana kojem rok
// ističe ili je u pričuvi njegov rok i gumb Obnovi.
func TestPostavkePokazujuRokClanstva(t *testing.T) {
	ured := primateljNaDaljinu(t)
	clanstvo, ovlast := ured.VlastitiRokovi(context.Background())
	sada := time.Now()
	data := SettingsPageData{
		CurrentUser: adminIvic, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Network: ured.NetworkInfo(), RokClanstva: clanstvo, RokOvlasti: &peers.Rok{Do: sada.Add(5 * 24 * time.Hour), Stanje: peers.RokIstjece},
		Members: []peers.Member{
			{Membership: razmjena.Membership{DeviceID: "laptop-baranja", IssuedBy: "cop-osijek", ExpiresAt: sada.Add(-5 * 24 * time.Hour)}, Valid: true,
				Rok: peers.Rok{Do: sada.Add(-5 * 24 * time.Hour), Stanje: peers.RokPricuva}},
			{Membership: razmjena.Membership{DeviceID: "laptop-vinkovci", IssuedBy: "cop-osijek", ExpiresAt: sada.Add(300 * 24 * time.Hour)}, Valid: true,
				Rok: peers.Rok{Do: sada.Add(300 * 24 * time.Hour), Stanje: peers.RokVrijedi}},
		},
	}
	if ovlast != nil {
		t.Errorf("osnivač s ovlasti: %+v", ovlast)
	}
	var b bytes.Buffer
	if err := predlozakStranice(t, "settings.html").ExecuteTemplate(&b, "settings.html", data); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{`onclick="obnoviClanstvo()"`, "vrijedi do ", "Ovlast za primanje", "badge-warning", "badge-danger", "razmjena radi još do",
		`obnoviClana('laptop-baranja')`, `obnoviClana('laptop-vinkovci')`} {
		if !strings.Contains(b.String(), d) {
			t.Errorf("Postavke nemaju %q", d)
		}
	}
}

// Kopiju ključa mreže sprema računalo s ključem, a učita drugo računalo
// iste mreže: samo administrator svojim očima, s ovog računala ili iz
// lokalne mreže, a za spremanje i s lozinkom svog računa. Administratori o
// tome dobiju e-poštu.
func TestKopijaKljucaUPostavkama(t *testing.T) {
	ured := primateljNaDaljinu(t)
	clan := svjeziCvorZaWeb(t, "pperic-thinkpad")
	zahtjev, kod, err := clan.NapraviZahtjev()
	if err != nil {
		t.Fatal(err)
	}
	potvrda, err := ured.PrimiZahtjev(context.Background(), zahtjev, kod, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clan.UveziPotvrdu(context.Background(), potvrda); err != nil {
		t.Fatal(err)
	}
	ivic := &models.User{ID: uuid.New(), Username: "aivic", FullName: "Ana Ivić"}
	rukovatelj := func(s *peers.Service) *SettingsHandler {
		h := &SettingsHandler{peers: s}
		h.SetZastitaKljuca(func(id uuid.UUID, lozinka string) error {
			if lozinka != "moja-lozinka" {
				return service.ErrKrivaLozinka
			}
			return nil
		})
		return h
	}
	hUred, hClan := rukovatelj(ured), rukovatelj(clan)
	type uvjet struct {
		perms   *models.UserPermissions
		izvana  bool
		tudjim  bool
		kopija  bool
		rukovat *SettingsHandler
	}
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	zovi := func(u uvjet, tijelo string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/api/network/kljuc", strings.NewReader(tijelo))
		r.Header.Set("Content-Type", "application/json")
		if !u.izvana {
			r.RemoteAddr = "127.0.0.1:5000"
			r.Host = adresaCvoraTesta
		}
		ctx := context.WithValue(r.Context(), contextKeyPerms, u.perms)
		ctx = context.WithValue(ctx, contextKeyUser, ivic)
		ctx = context.WithValue(ctx, contextKeyViewing, u.tudjim)
		w := httptest.NewRecorder()
		if u.kopija {
			u.rukovat.HandleKopijaKljuca(w, r.WithContext(ctx))
		} else {
			u.rukovat.HandleUcitajKopijuKljuca(w, r.WithContext(ctx))
		}
		return w
	}
	spremi := `{"lozinka":"zelena-letva-na-dravi","lozinka_racuna":"moja-lozinka"}`
	for _, c := range []struct {
		ime    string
		u      uvjet
		tijelo string
		kod    int
		ima    string
	}{
		{"bez administratora", uvjet{perms: &models.UserPermissions{}, kopija: true, rukovat: hUred}, spremi, http.StatusForbidden, "samo administrator"},
		{"izvana", uvjet{perms: admin, izvana: true, kopija: true, rukovat: hUred}, spremi, http.StatusForbidden, "ne izvana ni kroz tunel"},
		{"tuđim očima", uvjet{perms: admin, tudjim: true, kopija: true, rukovat: hUred}, spremi, http.StatusForbidden, "tuđim očima"},
		{"učitavanje izvana", uvjet{perms: admin, izvana: true, rukovat: hClan}, `{}`, http.StatusForbidden, "ne izvana ni kroz tunel"},
		{"krivo tijelo", uvjet{perms: admin, kopija: true, rukovat: hUred}, "nije json", http.StatusBadRequest, ""},
		{"kriva lozinka računa", uvjet{perms: admin, kopija: true, rukovat: hUred}, `{"lozinka":"zelena-letva-na-dravi","lozinka_racuna":"kriva"}`, http.StatusBadRequest, "lozinka nije točna"},
		{"kratka lozinka kopije", uvjet{perms: admin, kopija: true, rukovat: hUred}, `{"lozinka":"kratka","lozinka_racuna":"moja-lozinka"}`, http.StatusBadRequest, "Kopija nije spremljena"},
		{"učitavanje, krivo tijelo", uvjet{perms: admin, rukovat: hClan}, "nije json", http.StatusBadRequest, ""},
		{"nije kopija", uvjet{perms: admin, rukovat: hClan}, `{"kopija":"{}","lozinka":"zelena-letva-na-dravi"}`, http.StatusBadRequest, "Ključ mreže nije učitan"},
	} {
		if w := zovi(c.u, c.tijelo); w.Code != c.kod || !strings.Contains(w.Body.String(), c.ima) {
			t.Errorf("%s: %d %s", c.ime, w.Code, w.Body.String())
		}
	}
	w := zovi(uvjet{perms: admin, kopija: true, rukovat: hUred}, spremi)
	var odg struct {
		Kopija   string `json:"kopija"`
		Datoteka string `json:"datoteka"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &odg); w.Code != http.StatusOK || err != nil || odg.Datoteka != "gocop-kljuc-mreze-hrvatske-vode.json" {
		t.Fatalf("kopija: %d %s", w.Code, w.Body.String())
	}
	tijelo, err := json.Marshal(map[string]string{"kopija": odg.Kopija, "lozinka": "zelena-letva-na-dravi"})
	if err != nil {
		t.Fatal(err)
	}
	if w := zovi(uvjet{perms: admin, rukovat: hClan}, string(tijelo)); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Ključ mreže je učitan") || !clan.NetworkInfo().DrziKljuc {
		t.Errorf("učitavanje: %d %s", w.Code, w.Body.String())
	}
	// spremanje i učitavanje su u knjizi, za e-poštu administratorima
	for _, c := range []struct {
		s   *peers.Service
		sto string
	}{{ured, "Ana Ivić (aivic) spremio je kopiju ključa mreže"}, {clan, "Ana Ivić (aivic) učitao je kopiju ključa mreže"}} {
		if d := c.s.ObavijestiKljuca(context.Background()); len(d) != 1 || !strings.Contains(d[0].Tekst, c.sto) {
			t.Errorf("%s: događaji %+v", c.s.Node().ID, d)
		}
	}

	// Postavke: računalo s ključem nudi spremanje, drugo učitavanje
	bezKljuca := svjeziCvorZaWeb(t, "laptop-baranja")
	z2, k2, err := bezKljuca.NapraviZahtjev()
	if err != nil {
		t.Fatal(err)
	}
	p2, err := ured.PrimiZahtjev(context.Background(), z2, k2, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bezKljuca.UveziPotvrdu(context.Background(), p2); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		ime  string
		data SettingsPageData
		ima  []string
	}{
		{"bez kopije", SettingsPageData{Network: ured.NetworkInfo()}, []string{"spremiKopijuKljuca()", "Kopija još nije spremljena.", `id="kopija-racun"`}},
		{"s kopijom", SettingsPageData{Network: ured.NetworkInfo(), KopijaKljuca: time.Date(2026, 10, 23, 15, 4, 0, 0, time.Local)}, []string{"Kopija postoji: zadnji put spremljena ili učitana 23.10.2026 15:04."}},
		{"bez ključa", SettingsPageData{Network: bezKljuca.NetworkInfo()}, []string{"ucitajKopijuKljuca()", `id="kljuc-datoteka"`}},
	} {
		c.data.CurrentUser, c.data.Permissions = adminIvic, admin
		var b bytes.Buffer
		if err := predlozakStranice(t, "settings.html").ExecuteTemplate(&b, "settings.html", c.data); err != nil {
			t.Fatal(err)
		}
		for _, d := range c.ima {
			if !strings.Contains(b.String(), d) {
				t.Errorf("%s: nema %q", c.ime, d)
			}
		}
	}
}

// Lozinka računa koja se ne da provjeriti i tri krive zaredom odbiju
// spremanje; događaj koji se ne da upisati u knjigu ide u dnevnik.
func TestZastitaKopijeKljucaRubovi(t *testing.T) {
	var dnevnik bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&dnevnik)
	t.Cleanup(func() { log.SetOutput(stari) })
	h := &SettingsHandler{peers: primateljNaDaljinu(t)}
	osoba := &models.User{ID: uuid.New(), Username: "bbrkic", FullName: "Branko Brkić"}
	if err := h.provjeriLozinkuRacuna(context.Background(), osoba, "bilo-sto"); err == nil {
		t.Error("lozinka provjerena bez provjere")
	}
	h.SetZastitaKljuca(func(uuid.UUID, string) error { return service.ErrKrivaLozinka })
	for range 3 {
		_ = h.provjeriLozinkuRacuna(context.Background(), osoba, "kriva") // brojač krivih upisa
	}
	if err := h.provjeriLozinkuRacuna(context.Background(), osoba, "kriva"); err == nil || !strings.Contains(err.Error(), "previše krivih lozinki") {
		t.Errorf("četvrta kriva: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/network/kljuc", nil)
	h.javiKljuc(r, nil, "spremio je kopiju ključa mreže")
	if !strings.Contains(dnevnik.String(), "nepoznata osoba spremio je kopiju ključa mreže") {
		t.Errorf("dnevnik:\n%s", dnevnik.String())
	}
	s, baza := cvorIBazaZaWeb(t, "laptop-ilok")
	if _, err := baza.Exec(`CREATE TRIGGER kvar_dogadjaja BEFORE INSERT ON record_versions WHEN NEW.entity = 'dogadjaji_kljuca' BEGIN SELECT RAISE(ABORT, 'namjerni kvar'); END`); err != nil {
		t.Fatal(err)
	}
	(&SettingsHandler{peers: s}).javiKljuc(r, osoba, "učitao je kopiju ključa mreže")
	if !strings.Contains(dnevnik.String(), "Branko Brkić (bbrkic) učitao je") || !strings.Contains(dnevnik.String(), "događaj s ključem mreže nije upisan u knjigu") {
		t.Errorf("dnevnik bez greške knjige:\n%s", dnevnik.String())
	}
}

func TestMjesnoVrijeme(t *testing.T) {
	if !mjesnoVrijeme(nil).IsZero() {
		t.Error("bez vremena")
	}
	kad := time.Date(2026, 10, 23, 13, 4, 0, 0, time.UTC)
	if got := mjesnoVrijeme(&kad); !got.Equal(kad) || got.Location() != time.Local {
		t.Errorf("mjesno %v", got)
	}
}
