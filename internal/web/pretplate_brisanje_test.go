package web

import (
	"database/sql"
	"fmt"
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
	return s.prikaziKao(t, kaoAdmin)
}

// prikaziKao prikaže stranicu korisniku iz uvjeta zahtjeva
func (s stranicaPretplata) prikaziKao(t *testing.T, u uvjetiZahtjeva) string {
	t.Helper()
	w := httptest.NewRecorder()
	s.h.ShowSubscriptions(w, zahtjevPostavki(http.MethodGet, "/pretplate", nil, u))
	if w.Code != http.StatusOK {
		t.Fatalf("stranica: %d %s", w.Code, w.Body.String())
	}
	return html.UnescapeString(w.Body.String())
}

// obrazacPretplata šalje obrazac rukovatelju i vraća upit preusmjerenja
func obrazacPretplata(t *testing.T, rukovatelj http.HandlerFunc, putanja string, obrazac url.Values) url.Values {
	t.Helper()
	return obrazacPretplataKao(t, rukovatelj, putanja, obrazac, kaoAdmin)
}

// obrazacPretplataKao šalje obrazac kao korisnik iz uvjeta zahtjeva
func obrazacPretplataKao(t *testing.T, rukovatelj http.HandlerFunc, putanja string, obrazac url.Values, u uvjetiZahtjeva) url.Values {
	t.Helper()
	w := httptest.NewRecorder()
	rukovatelj(w, zahtjevPostavki(http.MethodPost, putanja, obrazac, u))
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
		"Sve vrste: očitanja, vodočuvarski dnevnik, prijave s terena, dnevnici radova i arhiva vodostaja",
		"Samo dnevnici radova, po području (branjeno područje); dnevnik COP-a stiže svakom računalu",
		"Samo pregledi: slike, bez PDF-ova",
	} {
		if !strings.Contains(stranica, tekst) {
			t.Errorf("stranica nema %q", tekst)
		}
	}
	potvrda := `confirm("Brisanje s ovog računala: prijave s terena, BP 16, 2026.\n\nObrisat će se 1 zapis. Ostaje ono što je nastalo na ovom računalu: 1 zapis.\n\n` +
		`Ono što se briše stiglo je s drugih računala i vratit će se razmjenom kad to opet počnete pratiti. Obrisati?")`
	if !strings.Contains(stranica, potvrda) {
		t.Errorf("potvrda nema iste brojeve:\n%s", stranica[strings.Index(stranica, "confirm("):][:400])
	}
	for _, nema := range []string{"Makni i obriši", "PDF na zahtjev", "ostaje u uredu", "Tuđa očitanja starija od"} {
		if strings.Contains(stranica, nema) {
			t.Errorf("stranica još piše %q", nema)
		}
	}
	s.h.peers.SetWantsAll(true)
	if !strings.Contains(s.prikazi(t), "očitanja, vodočuvarski dnevnik, prijave s terena, dnevnike radova i arhivu vodostaja svih područja i godina") {
		t.Error("obavijest o čvoru koji prati sve ne nabraja sve vrste")
	}
	s.h.peers.SetWantsAll(false)

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
		{"year_from", "20255", porukaOdGodine},
		{"year_from", "0999", porukaOdGodine},
		{"year_from", "+202", porukaOdGodine},
		{"year_to", "dvije tisuće", porukaDoGodine},
		{"drzi_dana", "90 dana", porukaDana},
		{"drzi_dana", "+90", porukaDana},
		{"drzi_dana", "0", porukaDana},
		{"drzi_dana", "99999999999999999999", porukaDana},
	} {
		q := obrazacPretplata(t, s.h.HandleAdd, "/pretplate/dodaj", url.Values{"kind": {"prijave"}, "area_id": {"16"}, p.polje: {p.vrijednost}})
		if want := fmt.Sprintf(p.poruka, p.vrijednost); q.Get("error") != want {
			t.Errorf("%s = %q: %v", p.polje, p.vrijednost, q)
		}
	}
	if got := obrazacPretplata(t, s.h.HandleAdd, "/pretplate/dodaj", url.Values{"drzi_dana": {strings.Repeat("9", 30)}}).Get("error"); !strings.Contains(got, "piše „"+strings.Repeat("9", 24)+"…”, a upisuje se cijeli broj dana veći od nule.") {
		t.Errorf("dug upis u poruci: %q", got)
	}
	if want := "Pretplata nije dodana: u polju „Od godine” piše „2025.”, a godina se upisuje s četiri znamenke i bez točke. Upišite npr. 2025 ili ostavite polje prazno za sve godine od početka."; fmt.Sprintf(porukaOdGodine, "2025.") != want {
		t.Errorf("poruka o godini: %q", fmt.Sprintf(porukaOdGodine, "2025."))
	}
	if l, _ := s.h.peers.ListSubscriptions(t.Context()); len(l) != 0 {
		t.Fatalf("krivo upisano pravilo je dodano: %+v", l)
	}

	dobro := url.Values{"kind": {"prijave"}, "sector_id": {"B"}, "area_id": {"16"}, "year_from": {"2025"}, "year_to": {"2026"}, "drzi_dana": {"90"}, "sync": {"1"}}
	if q := obrazacPretplata(t, s.h.HandleAdd, "/pretplate/dodaj", dobro); q.Get("success") != "Pretplata je dodana: prijave s terena, BP 16, 2025.–2026., sadržaj 90 dana." {
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
	want := "Dodane su 3 pretplate: vodočuvarski dnevnik, BP 16, od 2025., samo pregledi, sadržaj 90 dana; očitanja, BP 16, od 2025.; dnevnici, BP 16, od 2025., samo pregledi, sadržaj 90 dana."
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
	if q := obrazacPretplata(t, s.h.HandleAdd, "/pretplate/dodaj", prijedlog); q.Get("error") != "Ništa nije dodano: ovo računalo već prati sve što prijedlog donosi." {
		t.Errorf("prijedlog drugi put: %v", q)
	}
	if got := porukaDodano(l[:2]); !strings.HasPrefix(got, "Dodane su 2 pretplate: ") {
		t.Errorf("dva pravila: %q", got)
	}
	if got := porukaDodano(nil); got != "Ništa nije dodano." {
		t.Errorf("bez pravila: %q", got)
	}
	pet := []peers.Subscription{{}, {}, {}, {}, {}}
	if got := porukaDodano(pet); !strings.HasPrefix(got, "Dodano je 5 pretplata: sve vrste") {
		t.Errorf("pet pravila: %q", got)
	}
	if _, err := s.baza.Exec(`DROP TABLE subscriptions`); err != nil {
		t.Fatal(err)
	}
	if q := obrazacPretplata(t, s.h.HandleAdd, "/pretplate/dodaj", prijedlog); !strings.HasPrefix(q.Get("error"), "Pretplate nisu dodane: postojeće pretplate nisu se mogle pročitati") {
		t.Errorf("prijedlog bez tablice pravila: %v", q)
	}
}

// Vodočuvar s dužnošću u BP 16 vidi prijedlog za sve četiri vrste. Kad je
// do 0.0.38 prihvatio stari prijedlog (dnevnici radova i prijave), stranica
// mu nudi ono što mu nedostaje, a prihvat doda samo to. Kad prati sve,
// prijedloga nema.
func TestPrijedlogNaStraniciNudiStoNedostaje(t *testing.T) {
	s := novaStranicaPretplata(t)
	bp16, sektor := 16, "B"
	vodocuvar := kaoAdmin
	vodocuvar.korisnik = &models.User{FullName: "Ivo Ivić", Duties: []models.Duty{{IsActive: true, IsPrimary: true, AreaID: &bp16, SectorID: &sektor}}}
	prosla := time.Now().In(models.Zagreb).Year() - 1
	if stranica := s.prikaziKao(t, vodocuvar); !strings.Contains(stranica, fmt.Sprintf("Prijedlog iz vašeg zaduženja, BP 16: pratiti <b>vodočuvarski dnevnik, očitanja, prijave s terena i dnevnike radova</b> od godine %d. Uz zapise stižu slike, bez PDF-ova, a primljene slike ostaju na ovom računalu 90 dana.", prosla)) {
		t.Error("stranica nema prijedlog za sve četiri vrste")
	}
	for _, vrsta := range []string{"dnevnici", "prijave"} {
		if _, err := s.h.peers.AddSubscription(t.Context(), peers.Subscription{Kind: vrsta, AreaID: 16, YearFrom: prosla, Razina: peers.RazinaPregled, DrziDana: 90}); err != nil {
			t.Fatal(err)
		}
	}
	if stranica := s.prikaziKao(t, vodocuvar); !strings.Contains(stranica, fmt.Sprintf("Prijedlog iz vašeg zaduženja, BP 16: pratiti <b>vodočuvarski dnevnik i očitanja</b> od godine %d.", prosla)) {
		t.Error("stranica ne nudi ono što starom prijedlogu nedostaje")
	}
	prijedlog := url.Values{"predlog": {"1"}, "area_id": {"16"}, "year_from": {strconv.Itoa(prosla)}, "razina": {"pregled"}, "drzi_dana": {"90"}}
	want := fmt.Sprintf("Dodane su 2 pretplate: vodočuvarski dnevnik, BP 16, od %d., samo pregledi, sadržaj 90 dana; očitanja, BP 16, od %d.", prosla, prosla)
	if q := obrazacPretplataKao(t, s.h.HandleAdd, "/pretplate/dodaj", prijedlog, vodocuvar); q.Get("success") != want {
		t.Errorf("prihvat prijedloga: %v", q)
	}
	if stranica := s.prikaziKao(t, vodocuvar); strings.Contains(stranica, "Prijedlog iz vašeg zaduženja") {
		t.Error("stranica nudi prijedlog i kad se prati sve iz njega")
	}
}

// Prijedlog se slaže iz zaduženja: područje primarne dužnosti, inače prve
// aktivne s područjem; nema ga bez područja ni kad pravilo za područje ili
// sektor već postoji.
func TestPrijedlogIzZaduzenja(t *testing.T) {
	bp := func(n int) *int { return &n }
	sektor := "B"
	aktivna := models.Duty{IsActive: true, AreaID: bp(16), SectorID: &sektor}
	sve := "vodočuvarski dnevnik, očitanja, prijave s terena i dnevnike radova"
	for _, p := range []struct {
		ime       string
		u         *models.User
		postojeca []peers.Subscription
		area      int
		sto       string
	}{
		{"bez korisnika", nil, nil, 0, ""},
		{"bez dužnosti", &models.User{}, nil, 0, ""},
		{"primarna", &models.User{Duties: []models.Duty{{IsActive: true, AreaID: bp(17)}, {IsActive: true, IsPrimary: true, AreaID: bp(18)}}}, nil, 18, sve},
		{"prva aktivna s područjem", &models.User{Duties: []models.Duty{{IsActive: true, IsPrimary: true}, {AreaID: bp(15)}, aktivna}}, nil, 16, sve},
		{"područje već prati", &models.User{Duties: []models.Duty{aktivna}}, []peers.Subscription{{AreaID: 16}}, 0, ""},
		{"sektor već prati", &models.User{Duties: []models.Duty{aktivna}}, []peers.Subscription{{SectorID: "B"}}, 0, ""},
		{"prati sve", &models.User{Duties: []models.Duty{aktivna}}, []peers.Subscription{{}}, 0, ""},
		{"drugo područje", &models.User{Duties: []models.Duty{aktivna}}, []peers.Subscription{{AreaID: 3}, {SectorID: "A"}}, 16, sve},
		{"stari prijedlog", &models.User{Duties: []models.Duty{aktivna}},
			[]peers.Subscription{{Kind: "dnevnici", AreaID: 16, YearFrom: 2025}, {Kind: "prijave", SectorID: "B", YearFrom: 2024}}, 16, "vodočuvarski dnevnik i očitanja"},
		{"tek od ove godine", &models.User{Duties: []models.Duty{aktivna}}, []peers.Subscription{{AreaID: 16, YearFrom: 2026}}, 16, sve},
		{"do prošle godine", &models.User{Duties: []models.Duty{aktivna}}, []peers.Subscription{{AreaID: 16, YearTo: 2025}}, 16, sve},
		{"samo očitanja nedostaju", &models.User{Duties: []models.Duty{aktivna}},
			[]peers.Subscription{{Kind: "vodocuvarski", AreaID: 16}, {Kind: "dnevnici", AreaID: 16}, {Kind: "prijave", AreaID: 16}, {Kind: "arhiva", AreaID: 16}}, 16, "očitanja"},
	} {
		got := predlogPretplate(p.u, p.postojeca, 2026)
		switch {
		case p.area == 0 && got != nil:
			t.Errorf("%s: prijedlog %+v", p.ime, got)
		case p.area > 0 && (got == nil || got.Subscription != peers.Subscription{AreaID: p.area, YearFrom: 2025, Razina: peers.RazinaPregled, DrziDana: 90} || got.Sto() != p.sto):
			t.Errorf("%s: prijedlog %+v", p.ime, got)
		case got != nil && got.SaSlikama() != (p.sto != "očitanja"):
			t.Errorf("%s: slike uz prijedlog %v", p.ime, got.SaSlikama())
		}
	}
	if (PrijedlogPretplate{}).Sto() != "" || vrstaZaPracenje("dnevnici") != "dnevnike radova" {
		t.Error("prazan prijedlog ili naziv vrste")
	}
	if p := pravilaPrijedloga(peers.Subscription{AreaID: 16}, []peers.Subscription{{Kind: "ocitanja", AreaID: 16, YearFrom: 1990}}, "B", 2026); len(p) != 4 {
		t.Errorf("prijedlog za sve godine uz očitanja tek od 1990.: %+v", p)
	}
}

// Rečenice o brisanju za svaki slučaj: jedan, dva i pet zapisa, samo
// dokumenti, samo povijest izmjena i ništa.
func TestRecenicaOBrisanju(t *testing.T) {
	kanal := "vodocuvarski/16/2026"
	for _, p := range []struct {
		b                       peers.Brisanje
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
		{peers.Brisanje{Kanal: "ocitanja/16/2026", Zapisa: 1426, Dokumenata: 1001, OstajeZapisa: 3530, OstajeDokumenata: 2000},
			"Obrisat će se 1.426 zapisa i 1.001 dokument.", "Ostaje ono što je nastalo na ovom računalu: 3.530 zapisa i 2.000 dokumenata.",
			"S ovog računala obrisano je 1.426 zapisa i 1.001 dokument iz očitanja, BP 16, 2026. Ostalo je ono što je nastalo na ovom računalu: 3.530 zapisa i 2.000 dokumenata."},
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

	// naziv razine uz broj područja je iz naziva ustroja
	stari := models.Terms()
	drugi := stari
	drugi.AreaShort = "VGI"
	models.SetTerms(drugi)
	defer models.SetTerms(stari)
	k := KanalStranice{ChannelStat: peers.ChannelStat{Channel: "prijave/16/2026", Kind: "prijave", AreaID: 16, Year: 2026}, Naziv: "Prijave s terena", Brisanje: &peers.Brisanje{Kanal: "prijave/16/2026", Zapisa: 2}}
	if got := k.Potvrda(); !strings.HasPrefix(got, "Brisanje s ovog računala: prijave s terena, VGI 16, 2026.\n\nObrisat će se 2 zapisa. \n\n") {
		t.Errorf("potvrda s drugim nazivom razine: %q", got)
	}
	if got := porukaObrisano(*k.Brisanje); got != "S ovog računala obrisana su 2 zapisa iz prijava s terena, VGI 16, 2026." {
		t.Errorf("poruka s drugim nazivom razine: %q", got)
	}
}

// Na računalu koje prima samo zadnje mjesece tuđih očitanja potvrda za
// očitanja kaže da se starija ne vraćaju, osim za letve koje se prate; za
// druge vrste i bez ograde vrijedi obična rečenica.
func TestPotvrdaKazeDaSeStarijaOcitanjaNeVracaju(t *testing.T) {
	obicna := "Ono što se briše stiglo je s drugih računala i vratit će se razmjenom kad to opet počnete pratiti. Obrisati?"
	for _, p := range []struct {
		vrsta   string
		mjeseci int
		kraj    string
	}{
		{"ocitanja", 12, "razmjena vrati očitanja iz zadnjih 12 mjeseci, a starija samo za letve koje pratite. Obrisati?"},
		{"ocitanja", 3, "razmjena vrati očitanja iz zadnja 3 mjeseca, a starija samo za letve koje pratite. Obrisati?"},
		{"ocitanja", 1, "razmjena vrati očitanja iz zadnjeg 1 mjeseca, a starija samo za letve koje pratite. Obrisati?"},
		{"ocitanja", 0, obicna},
		{"prijave", 12, obicna},
	} {
		k := KanalStranice{ChannelStat: peers.ChannelStat{Kind: p.vrsta, AreaID: 16, Year: 2025}, Naziv: nazivVrste(p.vrsta), PovijestMjeseci: p.mjeseci,
			Brisanje: &peers.Brisanje{Kanal: p.vrsta + "/16/2025", Zapisa: 5}}
		if got := k.Potvrda(); !strings.HasSuffix(got, p.kraj) {
			t.Errorf("%s uz %d mjeseci: %q", p.vrsta, p.mjeseci, got)
		}
		if (k.OgradaPovratka() != "") != (p.kraj != obicna) {
			t.Errorf("%s uz %d mjeseci: ograda %q", p.vrsta, p.mjeseci, k.OgradaPovratka())
		}
	}
}

// Stranica kaže da se starija tuđa očitanja ne vraćaju, i u opisu i uz
// kanal očitanja koji se ne prati.
func TestStranicaPretplataKazePovijestOcitanja(t *testing.T) {
	repository.SetReadingHistoryPolicy(repository.ReadingHistoryPolicy{Months: 12})
	t.Cleanup(func() { repository.SetReadingHistoryPolicy(repository.ReadingHistoryPolicy{}) })
	s := novaStranicaPretplata(t)
	if _, err := ledger.New(s.baza, "cop-osijek-ured").RecordIn(t.Context(), s.baza, "ocitanja/16/2025", "readings", "r-1", map[string]string{"id": "r-1"}); err != nil {
		t.Fatal(err)
	}
	stranica := s.prikazi(t)
	for _, tekst := range []string{
		"Tuđa očitanja starija od 12 mjeseci ne vraćaju se, osim za letve koje pratite, jer je tako postavljena povijest tuđih očitanja.",
		"Obrisat će se samo povijest izmjena s drugih računala.  Ono što se briše stiglo je s drugih računala. Kad to opet počnete pratiti, razmjena vrati očitanja iz zadnjih 12 mjeseci, a starija samo za letve koje pratite.",
	} {
		if !strings.Contains(stranica, tekst) {
			t.Errorf("stranica nema %q", tekst)
		}
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
	if stranica := s.prikazi(t); strings.Contains(stranica, "Obriši s računala") || !strings.Contains(stranica, porukaTabliceKanala) || strings.Contains(stranica, "database is closed") {
		t.Error("stranica s nečitljivim spremištem nudi brisanje ili pokazuje poruku baze")
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
