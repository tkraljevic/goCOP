package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/javnivodostaji"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/prognoza"
	"gocop/internal/repository"
)

// spremisteKruga je spremište vodostaja bez ijedne letve za preuzimanje, pa
// krug ne ide na mrežu. Kad je poceo zadan, krug na popisu letvi javi da je
// počeo i čeka pusti — tako se krug koji traje može vidjeti bez čekanja na sat.
type spremisteKruga struct {
	poceo, pusti chan struct{}
}

func (s *spremisteKruga) LetveZaPreuzimanje(context.Context) ([]models.Station, error) {
	if s.poceo != nil {
		close(s.poceo)
		<-s.pusti
	}
	return nil, nil
}
func (*spremisteKruga) Postojeca(context.Context, string, time.Time, time.Time) (map[int64]bool, error) {
	return nil, nil
}
func (*spremisteKruga) Upisi(context.Context, []models.Reading) (int, error) { return 0, nil }
func (*spremisteKruga) Razine(context.Context, string, time.Time, time.Time) (map[int64]int, error) {
	return nil, nil
}
func (*spremisteKruga) ZabiljeziOdbacene(context.Context, []models.OdbacenaVrijednost) error {
	return nil
}

// primi čeka vrijednost s kanala do roka. Rok je samo ograda da test padne,
// a ne visi, kad krug zapne; ishod o njemu ne ovisi.
func primi[T any](t *testing.T, rok context.Context, ch <-chan T, sto string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-rok.Done():
		t.Fatalf("%s: nije stiglo do roka", sto)
		var nula T
		return nula
	}
}

// dovrsenKrug čeka da uvoznik pusti krug. Zadnji krug se tu ne gleda: on
// postoji već od prvog kruga u testu, pa ne govori je li ovaj gotov.
func dovrsenKrug(t *testing.T, rok context.Context, u *javnivodostaji.Uvoznik) {
	t.Helper()
	gotov := make(chan struct{})
	go func() {
		defer close(gotov)
		for u.UTijeku() && rok.Err() == nil {
			runtime.Gosched()
		}
	}()
	primi(t, rok, gotov, "krug preuzimanja")
	if u.UTijeku() {
		t.Fatal("krug preuzimanja nije završio do roka")
	}
}

// Pregled prognoza kroz poslužitelj: bez prijave na prijavu, bez baze
// prognoza poruka, s bazom izdanje i letve.
func TestStranicaPrognozaKrozPosluzitelj(t *testing.T) {
	p := noviPosluziteljOcitanja(t)
	if w := p.posalji("", http.MethodGet, "/prognoze", nil, ""); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Errorf("bez prijave: %d %q", w.Code, w.Header().Get("Location"))
	}
	w := p.posalji("dežurni", http.MethodGet, "/prognoze", nil, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Baza prognoza nije otvorena.") {
		t.Errorf("bez baze: %d", w.Code)
	}

	izdano := time.Date(2026, 9, 23, 5, 0, 0, 0, time.UTC).Unix() / 3600
	c, err := OtvoriPrognoze(bazaPrognoza(t, izdano))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	p.s.SetPrognoze(c)
	w = p.posalji("dežurni", http.MethodGet, "/prognoze", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("pregled: %d %s", w.Code, w.Body.String())
	}
	html := w.Body.String()
	for _, zeli := range []string{"23.9.2026. u 07:00", "belisce", "donji-miholjac"} {
		if !strings.Contains(html, zeli) {
			t.Errorf("pregled nema %q", zeli)
		}
	}
	if strings.Contains(html, "Baza prognoza nije otvorena.") || strings.Contains(html, "Nijedna prognoza još nije izdana.") {
		t.Error("pregled s izdanjem kaže da ga nema")
	}
}

// Predložak koji ne zna stranicu prognoza daje grešku poslužitelja, a ne
// prazan odgovor.
func TestStranicaPrognozaBezPredloska(t *testing.T) {
	h := NewPrognozeHandler(template.Must(template.New("prazan").Parse("")), nil, nil)
	w := httptest.NewRecorder()
	h.ShowPrognoze(w, httptest.NewRequest(http.MethodGet, "/prognoze", nil))
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "prognoze.html") {
		t.Errorf("bez predloška: %d %q", w.Code, w.Body.String())
	}
}

// Gumb „Generiraj” kroz poslužitelj: bez kruga preuzimanja poruka, dok krug
// traje drugi se ne pokreće, a pokrenut krug ide u pozadini i ostavlja zadnji
// krug na postavkama. Tko ne smije generirati, ne dolazi do rukovatelja.
func TestGenerirajPrognozuKrozPosluzitelj(t *testing.T) {
	p := noviPosluziteljOcitanja(t)
	put := "/prognoze/generiraj"
	provjeri := func(w *httptest.ResponseRecorder, kljuc, zeli string) {
		t.Helper()
		u, err := url.Parse(w.Header().Get("Location"))
		if w.Code != http.StatusSeeOther || err != nil || u.Path != "/prognoze/postavke" || u.Fragment != "generiraj" || u.Query().Get(kljuc) != zeli {
			t.Errorf("%d %q, želim %s=%q", w.Code, w.Header().Get("Location"), kljuc, zeli)
		}
	}
	provjeri(p.obrazac("dežurni", put, nil), "error", "Preuzimanje vodostaja nije uključeno na ovom čvoru.")
	if w := p.posalji("dežurni", http.MethodGet, "/prognoze/napredak", nil, ""); w.Code != http.StatusNotFound {
		t.Errorf("napredak bez kruga: %d", w.Code)
	}

	// jedan rok za sva čekanja u testu
	rok, otkazi := context.WithTimeout(context.Background(), 2*time.Minute)
	defer otkazi()
	sp := &spremisteKruga{poceo: make(chan struct{}), pusti: make(chan struct{})}
	u := javnivodostaji.NoviUvoznik(sp, nil)
	iznova := make(chan bool, 2)
	u.NakonPreuzimanja = func(ctx context.Context) { iznova <- prognoza.TraziIznova(ctx) }
	p.s.SetJavniUvoz(u)

	// krug koji traje
	gotov := make(chan struct{})
	go func() {
		defer close(gotov)
		u.PreuzmiSve(context.Background())
	}()
	primi(t, rok, sp.poceo, "početak kruga")
	provjeri(p.obrazac("dežurni", put, nil), "success", "Krug preuzimanja već traje.")
	w := p.posalji("dežurni", http.MethodGet, "/prognoze/napredak", nil, "")
	var n javnivodostaji.Napredak
	if err := json.Unmarshal(w.Body.Bytes(), &n); err != nil || w.Code != http.StatusOK || !n.UTijeku || n.Faza != "popis letvi" ||
		w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Errorf("napredak kruga: %d %s %v", w.Code, w.Body.String(), err)
	}
	if html := p.posalji("dežurni", http.MethodGet, "/prognoze/postavke", nil, "").Body.String(); !strings.Contains(html, "Generiranje u tijeku") {
		t.Error("postavke ne kažu da krug traje")
	}
	close(sp.pusti)
	primi(t, rok, gotov, "kraj kruga sata")
	if primi(t, rok, iznova, "krug sata") {
		t.Error("krug sata traži izračun iznova")
	}

	// zadnji krug stoji na postavkama
	html := p.posalji("dežurni", http.MethodGet, "/prognoze/postavke", nil, "").Body.String()
	if !strings.Contains(html, "Zadnji krug ") || !strings.Contains(html, ": 0 letvi, 0 novih očitanja, 0 s.") {
		t.Error("postavke ne pokazuju zadnji krug")
	}

	// pokrenut rukom: ide u pozadini i traži izračun iznova
	sp.poceo = nil
	provjeri(p.obrazac("dežurni", put, nil), "success", "Pokrenuto: preuzimanje vodostaja svih letvi, tuđih prognoza i izračun naše.")
	if !primi(t, rok, iznova, "krug pokrenut rukom") {
		t.Error("krug pokrenut rukom ne traži izračun iznova")
	}
	dovrsenKrug(t, rok, u)

	// vodočuvar ne pokreće prognozu
	if w := p.obrazac("vodočuvar", put, nil); w.Code != http.StatusForbidden {
		t.Errorf("vodočuvar: %d", w.Code)
	}
}

func TestTrajanjeKruga(t *testing.T) {
	for d, zeli := range map[time.Duration]string{
		0: "0 s", 45 * time.Second: "45 s", 59*time.Second + 900*time.Millisecond: "59 s",
		time.Minute: "1 min 0 s", 125 * time.Second: "2 min 5 s", 61*time.Minute + 7*time.Second: "61 min 7 s",
	} {
		if got := trajanjeKruga(d); got != zeli {
			t.Errorf("trajanjeKruga(%v) = %q, želim %q", d, got, zeli)
		}
	}
}

// Letva bez računa nije uvijek vrh lanca: ulaz dnevnog modela i letva samo
// za pregled stoje kao takve; letva koja se računa ne.
func TestOpisLetveBezRacuna(t *testing.T) {
	ulaz := prognoza.DnevniUlazi()[0]
	if slices.Contains(prognoza.PregledneLetve, ulaz) {
		t.Fatalf("%s je i ulaz i letva za pregled", ulaz)
	}
	pregledna := prognoza.PregledneLetve[0]
	if !jeDnevniUlaz(ulaz) || jeDnevniUlaz("nepostojeca") || !jePregledna(pregledna) || jePregledna("nepostojeca") {
		t.Fatal("prepoznavanje ulaza i letve za pregled")
	}
	h := &PrognozeHandler{}
	letve := h.opisiLetve(map[string]models.Station{}, []PregledLetve{
		{Letva: ulaz},
		{Letva: pregledna},
		{Letva: "vrh-primjer"},
		{Letva: pregledna, Racuna: "vodostaj"},
		{Letva: ulaz, UlazLanca: true},
	})
	type zastavice struct{ vrh, ulaz, pregledna bool }
	zeli := []zastavice{{false, true, false}, {false, false, true}, {true, false, false}, {false, false, false}, {true, false, false}}
	for i, z := range zeli {
		if got := (zastavice{letve[i].Vrh, letve[i].Ulaz, letve[i].Pregledna}); got != z {
			t.Errorf("letva %d (%s): %+v, želim %+v", i, letve[i].Kod, got, z)
		}
	}
}

// Ušća na profilu: tok dobiva oznaku ušća pritoke s vodostajem njezine
// zadnje letve, pritoka na kraju vodostaj najbliže letve toka. Pritoka bez
// letvi ima ušće bez vodostaja, a pritoka bez kilometra ušća ga nema.
func TestUscaNaProfilu(t *testing.T) {
	p := noviPosluziteljOcitanja(t)
	ctx := context.Background()
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	for _, w := range []models.Watercourse{
		{Code: "primjerica-t", OfficialName: "rijeka Primjerica", Name: "Primjerica", Kind: "rijeka"},
		{Code: "malica-t", OfficialName: "rijeka Malica", Name: "Malica", Kind: "rijeka", FlowsInto: "Primjerica", Mouth: "u Primjericu kod rkm 25+500"},
		{Code: "potok-t", OfficialName: "Potok", Name: "Potok", Kind: "potok", FlowsInto: "Primjerica", Mouth: "rkm 12,3"},
		{Code: "jarak-t", OfficialName: "Jarak", Name: "Jarak", Kind: "kanal", FlowsInto: "Primjerica", Mouth: "kod mosta"},
		{Code: "kruzna-t", OfficialName: "Kružna", Name: "Kružna", Kind: "kanal", FlowsInto: "Kružna", Mouth: "rkm 1"},
	} {
		w := w
		if err := p.vode.CreateWatercourse(ctx, admin, &w); err != nil {
			t.Fatalf("%s: %v", w.Name, err)
		}
	}
	postaje := map[string]models.Station{}
	for _, st := range []models.Station{
		{Code: "gornje", Name: "Gornje (Mađarska)", Watercourse: "Primjerica", Stationing: "rkm 40+000"},
		{Code: "donje", Name: "Donje", Watercourse: "Primjerica", Stationing: "rkm 10+000"},
		{Code: "malo-gornje", Name: "Malo Gornje", Watercourse: "Malica", Stationing: "rkm 5+000"},
		{Code: "malo-donje", Name: "Malo Donje", Watercourse: "Malica", Stationing: "rkm 1+500"},
		{Code: "kanal-letva", Name: "Kanalska", Watercourse: "Primjerica", Stationing: "nkm 3"},
		{Code: "bez-vode", Name: "Bez vode", Watercourse: "Primjerica", Stationing: "rkm 20"},
	} {
		st.ID = uuid.New()
		postaje[st.Code] = st
	}
	sada := func(cm float64) map[string]float64 { return map[string]float64{"vodostaj": cm} }
	letve := []PregledLetve{
		{Letva: "gornje", Sada: sada(120)}, {Letva: "donje", Sada: sada(-40)},
		{Letva: "malo-gornje", Sada: sada(88)}, {Letva: "malo-donje", Sada: sada(73)},
		{Letva: "kanal-letva", Sada: sada(5)},                      // nije na rijeci
		{Letva: "bez-vode", Sada: map[string]float64{"protok": 3}}, // bez vodostaja
		{Letva: "nepoznata", Sada: sada(1)},                        // nije u registru
	}
	h := &PrognozeHandler{watercourses: p.vode}
	out := h.usca(ctx, postaje, letve)

	glavni := map[string]UsceUlaz{}
	for _, u := range out["Primjerica"] {
		glavni[u.Naziv] = u
	}
	if len(glavni) != 2 {
		t.Fatalf("ušća na Primjerici: %+v", out["Primjerica"])
	}
	if u := glavni["ušće Malice"]; u.Rkm != 25.5 || !u.Vezano || u.Tekst != "Malo Donje 73 cm" || u.Letva != "" {
		t.Errorf("ušće Malice: %+v", u)
	}
	if u := glavni["ušće Potok"]; u.Rkm != 12.3 || u.Vezano || u.Tekst != "" {
		t.Errorf("ušće potoka bez letvi: %+v", u)
	}
	if m := out["Malica"]; len(m) != 1 || m[0] != (UsceUlaz{Naziv: "ušće u Primjericu", Rkm: 0, Tekst: "Gornje 120 cm", Vezano: true, Letva: "gornje"}) {
		t.Errorf("kraj Malice: %+v", m)
	}
	if len(out) != 2 {
		t.Errorf("ušća: %+v", out)
	}

	if (&PrognozeHandler{}).usca(ctx, postaje, letve) != nil {
		t.Error("bez registra vodotoka ima ušća")
	}
	// prekinut zahtjev: registar se ne da pročitati, ušća nema
	otkazan, otkazi := context.WithCancel(ctx)
	otkazi()
	if got := h.usca(otkazan, postaje, letve); got != nil {
		t.Errorf("registar se ne čita, a ušća su %+v", got)
	}
}

// Pregledne letve i ulazi dnevnog modela nisu u istoj letvi slučajno: popis
// stanica iz baze ne mijenja njihovu ulogu (stanica postoji pod drugim imenom).
func TestOpisLetveSPostajom(t *testing.T) {
	p := noviPosluziteljOcitanja(t)
	st := &models.Station{ID: uuid.New(), Code: prognoza.PregledneLetve[0], Name: "Pregledna (Slovačka)", Watercourse: "Dunav", Stationing: "rkm 1868"}
	if err := repository.NewStationRepository(p.baza, ledger.New(p.baza, "test")).CreateStation(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	h := NewPrognozeHandler(nil, nil, p.stations)
	letve := h.opisiLetve(h.postaje(context.Background()), []PregledLetve{{Letva: st.Code, Sada: map[string]float64{"vodostaj": 210}}})
	if len(letve) != 1 || !letve[0].Pregledna || letve[0].Naziv != "Pregledna (Slovačka)" || letve[0].Voda != "Dunav" ||
		letve[0].URL != "/readings/station/"+st.ID.String() || letve[0].SadaCm != "210" {
		t.Errorf("pregledna letva s postajom: %+v", letve)
	}
}

// zahtjevOsobe je zahtjev s osobom i ovlastima u kontekstu
func zahtjevOsobe(metoda, put string, tijelo string, perms *models.UserPermissions) *http.Request {
	r := httptest.NewRequest(metoda, put, strings.NewReader(tijelo))
	if tijelo != "" {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	c := context.WithValue(r.Context(), contextKeyUser, &models.User{ID: uuid.New(), FullName: "Ana Anić"})
	return r.WithContext(context.WithValue(c, contextKeyPerms, perms))
}

// Postavke prognoze: bez predloška ih nema, bez ičega za raditi vode na
// pregled, a s krugom preuzimanja i izdanjem pokazuju zadnje izdanje i
// spremanje načina prikaza upravi.
func TestStranicaPostavkiPrognoze(t *testing.T) {
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	h := NewPrognozeHandler(nil, nil, nil)
	w := httptest.NewRecorder()
	h.ShowPostavke(w, zahtjevOsobe(http.MethodGet, "/prognoze/postavke", "", admin))
	if w.Code != http.StatusNotFound {
		t.Errorf("bez predloška: %d", w.Code)
	}

	h.SetPostavke(predlozakStranice(t, "prognoze_postavke.html"))
	w = httptest.NewRecorder()
	h.ShowPostavke(w, zahtjevOsobe(http.MethodGet, "/prognoze/postavke", "", admin))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/prognoze" {
		t.Errorf("bez kruga i izdanja: %d %q", w.Code, w.Header().Get("Location"))
	}

	izdano := time.Date(2026, 9, 23, 5, 0, 0, 0, time.UTC).Unix() / 3600
	c, err := OtvoriPrognoze(bazaPrognoza(t, izdano))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	h.citac = func() *CitacPrognoza { return c }
	u := javnivodostaji.NoviUvoznik(&spremisteKruga{}, nil)
	h.SetJavniUvoz(func() *javnivodostaji.Uvoznik { return u })
	h.SetOpcije(func(context.Context) models.Opcije { return models.Opcije{PrognozaPrikaz: models.PrognozaDnevni} },
		func(context.Context, *models.UserPermissions, func(*models.Opcije)) error { return nil })
	for _, slucaj := range []struct {
		perms  *models.UserPermissions
		sprema bool
	}{{admin, true}, {&models.UserPermissions{}, false}} {
		w = httptest.NewRecorder()
		h.ShowPostavke(w, zahtjevOsobe(http.MethodGet, "/prognoze/postavke", "", slucaj.perms))
		html := w.Body.String()
		if w.Code != http.StatusOK || !strings.Contains(html, "Zadnje izdanje 23.9.2026. u 07:00.") || !strings.Contains(html, `id="generiraj"`) {
			t.Errorf("postavke: %d", w.Code)
		}
		// generira i sprema uprava; tko nema dužnost, samo gleda
		if strings.Contains(html, "Spremi</button>") != slucaj.sprema || strings.Contains(html, `action="/prognoze/generiraj"`) != slucaj.sprema {
			t.Errorf("spremanje i generiranje: želim %v", slucaj.sprema)
		}
	}

	h.SetPostavke(template.Must(template.New("prazan").Parse("")))
	w = httptest.NewRecorder()
	h.ShowPostavke(w, zahtjevOsobe(http.MethodGet, "/prognoze/postavke", "", admin))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("predložak bez stranice: %d", w.Code)
	}
}

// Način prikaza sprema samo uprava; nepoznat način je kombinacija, a
// neoznačen kvadratić susjeda znači bez susjeda.
func TestSpremanjeNacinaPrikazaPrognoze(t *testing.T) {
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	h := NewPrognozeHandler(nil, nil, nil)
	posalji := func(perms *models.UserPermissions, tijelo string) (string, string) {
		t.Helper()
		w := httptest.NewRecorder()
		h.SpremiPostavke(w, zahtjevOsobe(http.MethodPost, "/prognoze/postavke", tijelo, perms))
		u, err := url.Parse(w.Header().Get("Location"))
		if w.Code != http.StatusSeeOther || err != nil || u.Path != "/prognoze/postavke" || u.Fragment != "prikaz" {
			t.Fatalf("spremanje: %d %q", w.Code, w.Header().Get("Location"))
		}
		if p := u.Query().Get("error"); p != "" {
			return "error", p
		}
		return "success", u.Query().Get("success")
	}
	if k, p := posalji(admin, "prognoza_prikaz=dnevni"); k != "error" || p != "Način prikaza mijenja uprava organizacije." {
		t.Errorf("bez izmjene opcija: %s %q", k, p)
	}

	opcije := models.Opcije{PrognozaPrikaz: models.PrognozaSatni, BrisanjeSOglasnePloce: true}
	var greska error
	h.SetOpcije(func(context.Context) models.Opcije { return opcije },
		func(_ context.Context, _ *models.UserPermissions, izmjena func(*models.Opcije)) error {
			if greska != nil {
				return greska
			}
			izmjena(&opcije)
			return nil
		})
	if k, p := posalji(&models.UserPermissions{}, "prognoza_prikaz=dnevni"); k != "error" || p != "Način prikaza mijenja uprava organizacije." ||
		opcije.PrognozaPrikaz != models.PrognozaSatni {
		t.Errorf("bez uprave: %s %q %+v", k, p, opcije)
	}
	if k, p := posalji(admin, "prognoza_prikaz=dnevni&prognoza_susjedi=1"); k != "success" || p != "Način prikaza prognoze je spremljen." ||
		opcije.PrognozaPrikaz != models.PrognozaDnevni || opcije.PrognozaBezSusjeda || !opcije.BrisanjeSOglasnePloce {
		t.Errorf("dnevni sa susjedima: %s %q %+v", k, p, opcije)
	}
	if posalji(admin, "prognoza_prikaz=tjedni"); opcije.PrognozaPrikaz != models.PrognozaKombinacija || !opcije.PrognozaBezSusjeda {
		t.Errorf("nepoznat način bez susjeda: %+v", opcije)
	}
	greska = fmt.Errorf("baza je zaključana")
	if k, p := posalji(admin, "prognoza_prikaz=satni"); k != "error" || p != "baza je zaključana" || opcije.PrognozaPrikaz != models.PrognozaKombinacija {
		t.Errorf("greška pri spremanju: %s %q %+v", k, p, opcije)
	}
}
