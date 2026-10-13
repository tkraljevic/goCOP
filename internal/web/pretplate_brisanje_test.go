package web

import (
	"database/sql"
	"html"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/repository"
	"gocop/internal/sadrzaj"
	"gocop/internal/service"
)

// Stranica „Što ovo računalo prati”: brisanje s računala, dodavanje i
// micanje pravila. Osobe i računala su izmišljeni.

type stranicaPretplata struct {
	h    *SubscriptionsHandler
	baza *sql.DB
	rec  *ledger.Recorder
}

// novaStranicaPretplata diže čvor laptop-baranja s pravom stranicom
func novaStranicaPretplata(t *testing.T) stranicaPretplata {
	t.Helper()
	put := filepath.Join(t.TempDir(), "gocop.db")
	baza, err := db.OpenDB(put)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	cvor, err := peers.LoadNode(put, "laptop-baranja", "Laptop Baranja", "test")
	if err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "laptop-baranja")
	svc, err := peers.NewService(baza, rec, cvor, peers.Ports{})
	if err != nil {
		t.Fatal(err)
	}
	org := service.NewOrgService(repository.NewOrgRepository(baza, rec), service.NewSSEBroker())
	return stranicaPretplata{h: NewSubscriptionsHandler(svc, org, predlozakStranice(t, "pretplate.html")), baza: baza, rec: rec}
}

// prijava upisuje prijavu BP 16 iz 2026. s verzijom autora
func (s stranicaPretplata) prijava(t *testing.T, id, autor string) {
	t.Helper()
	sada := time.Now().UTC()
	if _, err := s.baza.Exec(`INSERT INTO prijave (id, user_id, area_id, datum, created_at, updated_at) VALUES (?, 'u-1', 16, '2026-07-01', ?, ?)`, id, sada, sada); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.New(s.baza, autor).RecordIn(t.Context(), s.baza, "prijave/16/2026", "prijave", id, map[string]string{"id": id}); err != nil {
		t.Fatal(err)
	}
}

func (s stranicaPretplata) prikazi(t *testing.T) string {
	t.Helper()
	w := httptest.NewRecorder()
	s.h.ShowSubscriptions(w, zahtjevPostavki(http.MethodGet, "/pretplate", nil, kaoAdmin))
	if w.Code != http.StatusOK {
		t.Fatalf("stranica: %d %s", w.Code, w.Body.String())
	}
	return html.UnescapeString(w.Body.String())
}

// obrazacPretplata šalje obrazac rukovatelju i vraća upit preusmjerenja
func obrazacPretplata(t *testing.T, rukovatelj http.HandlerFunc, putanja string, obrazac url.Values) url.Values {
	t.Helper()
	w := httptest.NewRecorder()
	rukovatelj(w, zahtjevPostavki(http.MethodPost, putanja, obrazac, kaoAdmin))
	kamo, err := url.Parse(w.Header().Get("Location"))
	if w.Code != http.StatusSeeOther || err != nil {
		t.Fatalf("%s: odgovor %d, %q", putanja, w.Code, w.Header().Get("Location"))
	}
	return kamo.Query()
}

// Odluke (13. 10. 2026.): uz gumb „Obriši s računala” stoji koliko se
// briše i koliko ostaje jer je nastalo ovdje, potvrda kaže iste brojeve, a
// poruka nakon brisanja kaže što je obrisano i što je ostalo. Prijavu Ive
// Ivića upisao je laptop, prijavu Pere Perića ured.
func TestBrisanjeSRacunalaKazeBrojeve(t *testing.T) {
	s := novaStranicaPretplata(t)
	s.prijava(t, "p-ivo", "laptop-baranja")
	s.prijava(t, "p-pero", "cop-osijek-ured")

	stranica := s.prikazi(t)
	for _, tekst := range []string{
		"Ovo računalo: Laptop Baranja (laptop-baranja)",
		"Prijave s terena",
		"Obrisat će se 1 zapis. Ostaje ono što je nastalo na ovom računalu: 1 zapis.",
		`name="year_from" class="form-control" value="` + strconv.Itoa(time.Now().In(models.Zagreb).Year()-1) + `"`,
		"Godina je ona na koju se podatak odnosi: dan mjerenja, dan lista ili dan događaja prijave.",
	} {
		if !strings.Contains(stranica, tekst) {
			t.Errorf("stranica nema %q", tekst)
		}
	}
	potvrda := `confirm("Brisanje s ovog računala: Prijave s terena, BP 16, 2026.\n\nObrisat će se 1 zapis. Ostaje ono što je nastalo na ovom računalu: 1 zapis.\n\n`
	if !strings.Contains(stranica, potvrda) {
		t.Errorf("potvrda nema iste brojeve:\n%s", stranica[strings.Index(stranica, "confirm("):][:300])
	}
	for _, nema := range []string{"Makni i obriši", "PDF na zahtjev", "ostaje u uredu"} {
		if strings.Contains(stranica, nema) {
			t.Errorf("stranica još piše %q", nema)
		}
	}

	q := obrazacPretplata(t, s.h.HandlePurge, "/pretplate/obrisi", url.Values{"channel": {"prijave/16/2026"}})
	if want := "S ovog računala obrisan je 1 zapis iz prijava s terena, BP 16, 2026. Ostalo je ono što je nastalo na ovom računalu: 1 zapis."; q.Get("success") != want {
		t.Errorf("poruka nakon brisanja: %q", q.Get("success"))
	}
	var ostale []string
	rows, err := s.baza.Query(`SELECT id FROM prijave`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ostale = append(ostale, id)
	}
	rows.Close()
	if strings.Join(ostale, ",") != "p-ivo" {
		t.Errorf("nakon brisanja ostale su prijave %v", ostale)
	}

	// kad je ostalo samo ono što je nastalo ovdje, gumba nema
	stranica = s.prikazi(t)
	if !strings.Contains(stranica, "Ništa se neće obrisati. Ostaje ono što je nastalo na ovom računalu: 1 zapis.") || strings.Contains(stranica, "Obriši s računala") {
		t.Error("kanal bez ičega za brisanje i dalje nudi brisanje")
	}
	q = obrazacPretplata(t, s.h.HandlePurge, "/pretplate/obrisi", url.Values{"channel": {"prijave/16/2026"}})
	if want := "S ovog računala nije obrisano ništa iz prijava s terena, BP 16, 2026. Ostalo je ono što je nastalo na ovom računalu: 1 zapis."; q.Get("success") != want {
		t.Errorf("drugo brisanje: %q", q.Get("success"))
	}
}

// Kanal pod pretplatom ne briše se, a poruka kaže što učiniti; krivo
// poslan obrazac javi grešku.
func TestBrisanjeSRacunalaOdbijeno(t *testing.T) {
	s := novaStranicaPretplata(t)
	s.prijava(t, "p-pero", "cop-osijek-ured")
	if _, err := s.h.peers.AddSubscription(t.Context(), peers.Subscription{Kind: "prijave", AreaID: 16}); err != nil {
		t.Fatal(err)
	}
	q := obrazacPretplata(t, s.h.HandlePurge, "/pretplate/obrisi", url.Values{"channel": {"prijave/16/2026"}})
	if got := q.Get("error"); got != "Ništa nije obrisano: ovo računalo to još prati. Najprije maknite pretplatu, a zatim obrišite." {
		t.Errorf("kanal pod pretplatom: %q", got)
	}
	for _, rukovatelj := range []http.HandlerFunc{s.h.HandlePurge, s.h.HandleRemove, s.h.HandleAdd} {
		r := httptest.NewRequest(http.MethodPost, "/pretplate", strings.NewReader("a=%zz"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		rukovatelj(w, r)
		if !strings.Contains(w.Header().Get("Location"), "error=") {
			t.Errorf("krivo poslan obrazac: %q", w.Header().Get("Location"))
		}
	}
}

// „Makni” samo makne pravilo: ništa se ne briše, a poruka kaže gdje se
// briše.
func TestMakniPraviloNistaNeBrise(t *testing.T) {
	s := novaStranicaPretplata(t)
	s.prijava(t, "p-pero", "cop-osijek-ured")
	pravilo, err := s.h.peers.AddSubscription(t.Context(), peers.Subscription{Kind: "prijave", AreaID: 16})
	if err != nil {
		t.Fatal(err)
	}
	if stranica := s.prikazi(t); !strings.Contains(stranica, "Pravilo prestaje vrijediti, a ništa se ne briše") || strings.Contains(stranica, `name="purge"`) {
		t.Error("pravilo nema samo gumb Makni")
	}
	q := obrazacPretplata(t, s.h.HandleRemove, "/pretplate/ukloni", url.Values{"id": {strconv.Itoa(pravilo.ID)}, "purge": {"1"}})
	if got := q.Get("success"); got != "Pretplata je maknuta. Što je već na ovom računalu, ostaje dok to ne obrišete u tablici „Što računalo drži”." {
		t.Errorf("poruka: %q", got)
	}
	var n int
	if err := s.baza.QueryRow(`SELECT count(*) FROM prijave`).Scan(&n); err != nil || n != 1 {
		t.Errorf("micanje pravila obrisalo je prijavu: %d %v", n, err)
	}
	if _, err := s.baza.Exec(`DROP TABLE subscriptions`); err != nil {
		t.Fatal(err)
	}
	if q := obrazacPretplata(t, s.h.HandleRemove, "/pretplate/ukloni", url.Values{"id": {"1"}}); q.Get("error") == "" {
		t.Error("micanje bez tablice pravila nije javilo grešku")
	}
}

// Godina se upisuje s četiri znamenke i bez točke, rok kao cijeli broj
// dana. Do 0.0.38 „2025.” je tiho postajalo „sve godine”, „25” „od 25.”, a
// „90 dana” „trajno”. Isto pravilo ne dodaje se dvaput.
func TestPretplataSeUpisujeStrogo(t *testing.T) {
	s := novaStranicaPretplata(t)
	for _, p := range []struct {
		polje, vrijednost, poruka string
	}{
		{"year_from", "2025.", porukaOdGodine},
		{"year_from", "25", porukaOdGodine},
		{"year_from", "0999", porukaOdGodine},
		{"year_to", "dvije tisuće", porukaDoGodine},
		{"drzi_dana", "90 dana", porukaDana},
		{"drzi_dana", "0", porukaDana},
		{"drzi_dana", "99999999999999999999", porukaDana},
	} {
		q := obrazacPretplata(t, s.h.HandleAdd, "/pretplate/dodaj", url.Values{"kind": {"prijave"}, "area_id": {"16"}, p.polje: {p.vrijednost}})
		if q.Get("error") != p.poruka {
			t.Errorf("%s = %q: %v", p.polje, p.vrijednost, q)
		}
	}
	if l, _ := s.h.peers.ListSubscriptions(t.Context()); len(l) != 0 {
		t.Fatalf("krivo upisano pravilo je dodano: %+v", l)
	}

	dobro := url.Values{"kind": {"prijave"}, "sector_id": {"B"}, "area_id": {"16"}, "year_from": {"2025"}, "year_to": {"2026"}, "drzi_dana": {"90"}, "sync": {"1"}}
	if q := obrazacPretplata(t, s.h.HandleAdd, "/pretplate/dodaj", dobro); q.Get("success") != "Pretplata je dodana: prijave s terena, BP 16, 2025.–2026., sadržaj 90 dana" {
		t.Errorf("dobro upisano: %v", q)
	}
	if q := obrazacPretplata(t, s.h.HandleAdd, "/pretplate/dodaj", dobro); q.Get("error") != "Pretplata nije dodana: isto pravilo već postoji." {
		t.Errorf("isto pravilo drugi put: %v", q)
	}
	if q := obrazacPretplata(t, s.h.HandleAdd, "/pretplate/dodaj", url.Values{"kind": {"prijave"}, "year_from": {"2026"}, "year_to": {"2024"}}); !strings.HasPrefix(q.Get("error"), "Pretplata nije dodana: godina u polju „Do godine”") {
		t.Errorf("obrnute godine: %v", q)
	}
}

// Prijedlog iz zaduženja vodočuvaru dodaje i njegov dnevnik i očitanja
// BP-a, ne samo dnevnike radova i prijave, a poruka nabroji sva dodana
// pravila. Što već postoji, preskače se.
func TestPrijedlogIzZaduzenjaDodajeSvePotrebno(t *testing.T) {
	s := novaStranicaPretplata(t)
	if _, err := s.h.peers.AddSubscription(t.Context(), peers.Subscription{Kind: "prijave", AreaID: 16, YearFrom: 2025, Razina: peers.RazinaPregled, DrziDana: 90}); err != nil {
		t.Fatal(err)
	}
	prijedlog := url.Values{"predlog": {"1"}, "area_id": {"16"}, "year_from": {"2025"}, "razina": {"pregled"}, "drzi_dana": {"90"}}
	q := obrazacPretplata(t, s.h.HandleAdd, "/pretplate/dodaj", prijedlog)
	want := "Dodane su 3 pretplate: vodočuvarski dnevnik, BP 16, od 2025., samo pregledi, sadržaj 90 dana; očitanja, BP 16, od 2025.; dnevnici, BP 16, od 2025., samo pregledi, sadržaj 90 dana"
	if q.Get("success") != want {
		t.Errorf("poruka: %q", q.Get("success"))
	}
	l, err := s.h.peers.ListSubscriptions(t.Context())
	if err != nil || len(l) != 4 {
		t.Fatalf("pravila: %+v %v", l, err)
	}
	vrste := map[string]peers.Subscription{}
	for _, p := range l {
		vrste[p.Kind] = p
	}
	if o := vrste["ocitanja"]; o.AreaID != 16 || o.YearFrom != 2025 || o.Razina != peers.RazinaSve || o.DrziDana != 0 {
		t.Errorf("očitanja: %+v", o)
	}
	if v := vrste["vodocuvarski"]; v.Razina != peers.RazinaPregled || v.DrziDana != 90 {
		t.Errorf("vodočuvarski dnevnik: %+v", v)
	}
	if q := obrazacPretplata(t, s.h.HandleAdd, "/pretplate/dodaj", prijedlog); q.Get("error") != "Pretplata nije dodana: isto pravilo već postoji." {
		t.Errorf("prijedlog drugi put: %v", q)
	}
	if got := porukaDodano(l[:2]); !strings.HasPrefix(got, "Dodane su 2 pretplate: ") {
		t.Errorf("dva pravila: %q", got)
	}
	pet := []peers.Subscription{{}, {}, {}, {}, {}}
	if got := porukaDodano(pet); !strings.HasPrefix(got, "Dodano je 5 pretplata: sve vrste") {
		t.Errorf("pet pravila: %q", got)
	}
	if _, err := s.baza.Exec(`DROP TABLE subscriptions`); err != nil {
		t.Fatal(err)
	}
	if q := obrazacPretplata(t, s.h.HandleAdd, "/pretplate/dodaj", prijedlog); !strings.Contains(q.Get("error"), "upis pretplate") {
		t.Errorf("prijedlog bez tablice pravila: %v", q)
	}
}

// Prijedlog se slaže iz zaduženja: područje primarne dužnosti, inače prve
// aktivne s područjem; nema ga bez područja ni kad pravilo za područje ili
// sektor već postoji.
func TestPrijedlogIzZaduzenja(t *testing.T) {
	bp := func(n int) *int { return &n }
	sektor := "B"
	aktivna := models.Duty{IsActive: true, AreaID: bp(16), SectorID: &sektor}
	for _, p := range []struct {
		ime       string
		u         *models.User
		postojeca []peers.Subscription
		area      int
	}{
		{"bez korisnika", nil, nil, 0},
		{"bez dužnosti", &models.User{}, nil, 0},
		{"primarna", &models.User{Duties: []models.Duty{{IsActive: true, AreaID: bp(17)}, {IsActive: true, IsPrimary: true, AreaID: bp(18)}}}, nil, 18},
		{"prva aktivna s područjem", &models.User{Duties: []models.Duty{{IsActive: true, IsPrimary: true}, {AreaID: bp(15)}, aktivna}}, nil, 16},
		{"područje već prati", &models.User{Duties: []models.Duty{aktivna}}, []peers.Subscription{{AreaID: 16}}, 0},
		{"sektor već prati", &models.User{Duties: []models.Duty{aktivna}}, []peers.Subscription{{SectorID: "B"}}, 0},
		{"drugo područje", &models.User{Duties: []models.Duty{aktivna}}, []peers.Subscription{{AreaID: 3}, {SectorID: "A"}}, 16},
	} {
		got := predlogPretplate(p.u, p.postojeca, 2026)
		switch {
		case p.area == 0 && got != nil:
			t.Errorf("%s: prijedlog %+v", p.ime, got)
		case p.area > 0 && (got == nil || *got != peers.Subscription{AreaID: p.area, YearFrom: 2025, Razina: peers.RazinaPregled, DrziDana: 90}):
			t.Errorf("%s: prijedlog %+v", p.ime, got)
		}
	}
}

// Rečenice o brisanju za svaki slučaj: jedan, dva i pet zapisa, samo
// dokumenti, samo povijest izmjena i ništa.
func TestRecenicaOBrisanju(t *testing.T) {
	kanal := "vodocuvarski/16/2026"
	for _, p := range []struct {
		b                        peers.Brisanje
		briseSe, ostaje, poruka string
	}{
		{peers.Brisanje{Kanal: kanal, Zapisa: 1, Dokumenata: 1, OstajeZapisa: 1, OstajeDokumenata: 1},
			"Obrisat će se 1 zapis i 1 dokument.", "Ostaje ono što je nastalo na ovom računalu: 1 zapis i 1 dokument.",
			"S ovog računala obrisan je 1 zapis i 1 dokument iz vodočuvarskog dnevnika, BP 16, 2026. Ostalo je ono što je nastalo na ovom računalu: 1 zapis i 1 dokument."},
		{peers.Brisanje{Kanal: kanal, Zapisa: 2, OstajeDokumenata: 5},
			"Obrisat će se 2 zapisa.", "Ostaje ono što je nastalo na ovom računalu: 5 dokumenata.",
			"S ovog računala obrisana su 2 zapisa iz vodočuvarskog dnevnika, BP 16, 2026. Ostalo je ono što je nastalo na ovom računalu: 5 dokumenata."},
		{peers.Brisanje{Kanal: kanal, Zapisa: 5, Dokumenata: 3, OstajeZapisa: 12},
			"Obrisat će se 5 zapisa i 3 dokumenta.", "Ostaje ono što je nastalo na ovom računalu: 12 zapisa.",
			"S ovog računala obrisano je 5 zapisa i 3 dokumenta iz vodočuvarskog dnevnika, BP 16, 2026. Ostalo je ono što je nastalo na ovom računalu: 12 zapisa."},
		{peers.Brisanje{Kanal: "ocitanja/16/2026", Dokumenata: 21},
			"Obrisat će se 21 dokument.", "",
			"S ovog računala obrisan je 21 dokument iz očitanja, BP 16, 2026."},
		{peers.Brisanje{Kanal: "dnevnici/16/2026", Verzija: 4},
			"Obrisat će se samo povijest izmjena s drugih računala.", "",
			"S ovog računala obrisana je samo povijest izmjena iz dnevnika radova, BP 16, 2026."},
		{peers.Brisanje{Kanal: "vodostaji/16/2026"},
			"Ništa se neće obrisati.", "",
			"S ovog računala nije obrisano ništa iz vodostaji, BP 16, 2026."},
	} {
		k := KanalStranice{Brisanje: &p.b}
		if k.BriseSe() != p.briseSe || k.Ostaje() != p.ostaje || porukaObrisano(p.b) != p.poruka {
			t.Errorf("%+v:\n%q\n%q\n%q", p.b, k.BriseSe(), k.Ostaje(), porukaObrisano(p.b))
		}
		if k.ImaZaBrisanje() != (p.b.Zapisa+p.b.Dokumenata+p.b.Verzija > 0) {
			t.Errorf("%+v: ima za brisanje %v", p.b, k.ImaZaBrisanje())
		}
		if !strings.Contains(recenicaOstalo(p.b), strings.TrimPrefix(p.ostaje, "Ostaje ono što je nastalo na ovom računalu: ")) {
			t.Errorf("ostalo: %q", recenicaOstalo(p.b))
		}
	}
	prazno := KanalStranice{}
	if prazno.BriseSe() != "" || prazno.Ostaje() != "" || prazno.ImaZaBrisanje() {
		t.Error("kanal pod pretplatom nema rečenica o brisanju")
	}
	for vrsta, naziv := range map[string]string{"ocitanja": "Očitanja", "dnevnici": "Dnevnici radova", "prijave": "Prijave s terena", "vodocuvarski": "Vodočuvarski dnevnik", "vodostaji": "vodostaji"} {
		if nazivVrste(vrsta) != naziv {
			t.Errorf("naziv vrste %s: %q", vrsta, nazivVrste(vrsta))
		}
	}
	if vrstaIz("prijave") != "prijava s terena" {
		t.Error("vrsta u genitivu")
	}
	for _, p := range []struct {
		n    *peers.Node
		opis string
	}{{nil, ""}, {&peers.Node{ID: "laptop-baranja"}, "laptop-baranja"}, {&peers.Node{ID: "ured", Name: "ured"}, "ured"}} {
		if opisCvora(p.n) != p.opis {
			t.Errorf("opis čvora %+v: %q", p.n, opisCvora(p.n))
		}
	}
}

// Kad se pregled brisanja ili stranica ne mogu složiti, stranica to kaže
// umjesto da pokaže krive brojeve.
func TestStranicaPretplataKadNestoNeRadi(t *testing.T) {
	s := novaStranicaPretplata(t)
	s.prijava(t, "p-pero", "cop-osijek-ured")
	sp, err := sadrzaj.Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	s.h.peers.SetSpremiste(sp)
	if err := sp.Zatvori(); err != nil {
		t.Fatal(err)
	}
	if stranica := s.prikazi(t); strings.Contains(stranica, "Obriši s računala") || !strings.Contains(stranica, "database is closed") {
		t.Error("stranica s nečitljivim spremištem nudi brisanje")
	}
	s.h.peers.SetSpremiste(nil)
	if _, err := s.baza.Exec(`DROP TABLE subscriptions`); err != nil {
		t.Fatal(err)
	}
	if stranica := s.prikazi(t); strings.Contains(stranica, "Obriši s računala") {
		t.Error("stranica bez čitljivih pretplata nudi brisanje")
	}
	s.h.tmpl = template.New("prazan")
	w := httptest.NewRecorder()
	s.h.ShowSubscriptions(w, zahtjevPostavki(http.MethodGet, "/pretplate", nil, kaoAdmin))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("stranica bez predloška: %d", w.Code)
	}
}
