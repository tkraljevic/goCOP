package web

import (
	"bytes"
	"context"
	"encoding/csv"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// posluziteljOcitanja je pravi poslužitelj nad okolinom sastavljača (letva
// Primjerovo, osobe sektora P) s upravom organizacije u bazi, registrom
// vodotoka i kišomjera i sesijom za svaku osobu.
type posluziteljOcitanja struct {
	*okolinaSastavljaca
	s         *Server
	ocitanja  *repository.ReadingRepository
	vode      *service.WatercourseService
	kisomjeri *service.KisomjerService
	sesije    map[string]string
}

func noviPosluziteljOcitanja(t *testing.T) *posluziteljOcitanja {
	t.Helper()
	o := novaOkolinaSastavljaca(t)
	rec := ledger.New(o.baza, "test")
	p := &posluziteljOcitanja{okolinaSastavljaca: o,
		ocitanja:  repository.NewReadingRepository(o.baza, rec),
		vode:      service.NewWatercourseService(repository.NewWatercourseRepository(o.baza, rec)),
		kisomjeri: service.NewKisomjerService(repository.NewKisomjerRepository(o.baza, rec)),
		sesije:    map[string]string{}}
	uprava := &models.User{ID: uuid.New(), Username: "aantic", FullName: "Ante Antić", IsGlobalAdmin: true, IsActive: true}
	if err := o.userRepo.CreateUser(uprava, nil); err != nil {
		t.Fatal(err)
	}
	o.osobe["uprava"] = uprava
	s, err := NewServer("", o.auth, o.users, nil, nil, o.stations, p.vode, o.structures, o.readings, nil, o.moduli, nil, nil, nil,
		SupportContact{}, nil, nil, nil, rec, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.db = o.baza
	p.s = s
	for tko, u := range o.osobe {
		sesija := &models.Session{UserID: u.ID, IPAddress: "192.168.1.50", UserAgent: "test", ExpiresAt: time.Now().Add(time.Hour)}
		if err := o.sesije.CreateSession(sesija); err != nil {
			t.Fatal(err)
		}
		p.sesije[tko] = sesija.ID.String()
	}
	return p
}

// posalji šalje zahtjev poslužitelju u ime osobe; bez osobe nema ni kolačića
func (p *posluziteljOcitanja) posalji(tko, metoda, put string, tijelo io.Reader, vrsta string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(metoda, put, tijelo)
	if vrsta != "" {
		r.Header.Set("Content-Type", vrsta)
	}
	if tko != "" {
		r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: p.sesije[tko]})
	}
	w := httptest.NewRecorder()
	p.s.mux.ServeHTTP(w, r)
	return w
}

// obrazac šalje obični obrazac
func (p *posluziteljOcitanja) obrazac(tko, put string, forma url.Values) *httptest.ResponseRecorder {
	return p.posalji(tko, http.MethodPost, put, strings.NewReader(forma.Encode()), "application/x-www-form-urlencoded")
}

// datoteka šalje obrazac s jednom datotekom i ostalim poljima
func (p *posluziteljOcitanja) datoteka(t *testing.T, tko, put, ime, sadrzaj string, polja map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var tijelo bytes.Buffer
	mw := multipart.NewWriter(&tijelo)
	for k, v := range polja {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if ime != "" {
		fw, err := mw.CreateFormFile("datoteka", ime)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(sadrzaj)); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return p.posalji(tko, http.MethodPost, put, &tijelo, mw.FormDataContentType())
}

// dodajStranca dodaje Stipu Stipića, dežurnog izmišljenog sektora Q, koji na
// letve sektora P ne upisuje
func (p *posluziteljOcitanja) dodajStranca(t *testing.T) {
	t.Helper()
	if _, err := p.baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('Q', 'Sektor Q', 'VGO Q', 'COP Q')`); err != nil {
		t.Fatal(err)
	}
	drugi := "Q"
	stranac := &models.User{ID: uuid.New(), Username: "sstipic", FullName: "Stipe Stipić", IsActive: true}
	if err := p.userRepo.CreateUser(stranac, &models.Duty{Title: "Dežurni u COP-u Q", Role: models.RoleOperator,
		ScopeType: models.ScopeSector, SectorID: &drugi}); err != nil {
		t.Fatal(err)
	}
	sesija := &models.Session{UserID: stranac.ID, IPAddress: "192.168.1.51", UserAgent: "test", ExpiresAt: time.Now().Add(time.Hour)}
	if err := p.okolinaSastavljaca.sesije.CreateSession(sesija); err != nil {
		t.Fatal(err)
	}
	p.sesije["stranac"] = sesija.ID.String()
}

// upisi upisuje očitanje letve Primjerovo u ime osobe, u zadani trenutak
func (p *posluziteljOcitanja) upisi(t *testing.T, tko string, kad time.Time, cm int, uredi func(*models.Reading)) models.Reading {
	t.Helper()
	v := cm
	rd := models.Reading{ID: uuid.New(), StationID: p.letva.ID.String(), MeasuredAt: kad.UTC(), LevelCm: &v,
		Source: models.ReadingSourceManual, UserID: p.osobe[tko].ID.String()}
	if uredi != nil {
		uredi(&rd)
	}
	if err := p.ocitanja.Create(context.Background(), &rd); err != nil {
		t.Fatal(err)
	}
	return rd
}

// citajCSV čita izvezenu datoteku bez oznake poretka bajtova
func citajCSV(t *testing.T, tijelo string) [][]string {
	t.Helper()
	if !strings.HasPrefix(tijelo, "\ufeff") {
		t.Error("datoteka nema BOM za Excel")
	}
	cr := csv.NewReader(strings.NewReader(strings.TrimPrefix(tijelo, "\ufeff")))
	cr.Comma = ';'
	redci, err := cr.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return redci
}

// porukaPreusmjerenja vraća putanju i poruku preusmjerenja
func porukaPreusmjerenja(t *testing.T, w *httptest.ResponseRecorder, kljuc string) (string, string) {
	t.Helper()
	put, upit := preusmjerenje(t, w)
	return put, upit.Get(kljuc)
}

// Bez prijave nema ni izvoza ni ispravka očitanja: poslužitelj vodi na prijavu.
func TestOcitanjaCSVBezPrijaveVodiNaPrijavu(t *testing.T) {
	p := noviPosluziteljOcitanja(t)
	osnova := "/readings/station/" + p.letva.ID.String()
	for _, w := range []*httptest.ResponseRecorder{
		p.posalji("", http.MethodGet, osnova+"/ocitanja.csv", nil, ""),
		p.datoteka(t, "", osnova+"/ocitanja/uvoz", "a.csv", "id;vrijeme;vodostaj_cm\n", nil),
		p.obrazac("", osnova+"/ocitanja/potvrdi", url.Values{"id": {uuid.NewString()}}),
	} {
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
			t.Errorf("bez prijave: %d → %q", w.Code, w.Header().Get("Location"))
		}
	}
}

// Izvoz daje očitanja letve od najstarijeg prema novijem, s decimalnim
// zarezom, izvorom i imenom onoga tko je upisao; pogled sužava razdoblje.
func TestIzvozOcitanjaLetveKrozPosluzitelj(t *testing.T) {
	p := noviPosluziteljOcitanja(t)
	temp, protok := 12.5, 231.25
	starije := p.upisi(t, "dežurni", time.Date(2013, 5, 4, 7, 0, 0, 0, models.Zagreb), -118, func(rd *models.Reading) {
		rd.TempC, rd.FlowM3s, rd.Observer, rd.Note = &temp, &protok, "Marko Markić", "led uz obalu"
	})
	novije := p.upisi(t, "vodočuvar", time.Date(2014, 3, 2, 19, 30, 0, 0, models.Zagreb), 45, func(rd *models.Reading) {
		v := 40
		rd.Level2Cm = &v
	})

	w := p.posalji("dežurni", http.MethodGet, "/readings/station/"+p.letva.ID.String()+"/ocitanja.csv", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("izvoz: %d %s", w.Code, w.Body.String())
	}
	if v := w.Header().Get("Content-Type"); v != "text/csv; charset=utf-8" {
		t.Errorf("vrsta %q", v)
	}
	if v := w.Header().Get("Content-Disposition"); !strings.HasPrefix(v, `attachment; filename="primjerovo_ocitanja_`) || !strings.HasSuffix(v, `.csv"`) {
		t.Errorf("ime datoteke %q", v)
	}
	redci := citajCSV(t, w.Body.String())
	if len(redci) != 3 || strings.Join(redci[0], ";") != strings.Join(stupciOcitanja, ";") {
		t.Fatalf("redci: %q", redci)
	}
	// Stupac „upisao” se ne provjerava: ime upisivača čitanje očitanja ne
	// puni, pa stupac zasad ostaje prazan.
	zeli := [][]string{
		{starije.ID.String(), "2013-05-04 07:00", "-118", "", "12,5", "231,25", "Marko Markić", "led uz obalu", "", "ručno"},
		{novije.ID.String(), "2014-03-02 19:30", "45", "40", "", "", "", "", "", "ručno"},
	}
	for i, z := range zeli {
		if len(redci[i+1]) != len(stupciOcitanja) || strings.Join(redci[i+1][:10], "|") != strings.Join(z, "|") {
			t.Errorf("redak %d:\n dobiveno %q\n želim    %q", i+1, redci[i+1], z)
		}
	}

	// pogled: godina, sve, besmislica, nula i zadnjih 30 dana daju svoje;
	// broj veći od 1900 je godina
	for pogled, broj := range map[string]int{"2014": 1, "2013": 1, "2012": 0, "sve": 2, "tjedan": 2, "0": 2, "-5": 2, "5000": 0, "30": 0} {
		w := p.posalji("dežurni", http.MethodGet, "/readings/station/"+p.letva.ID.String()+"/ocitanja.csv?pogled="+pogled, nil, "")
		if n := len(citajCSV(t, w.Body.String())) - 1; n != broj {
			t.Errorf("pogled %q: %d očitanja, želim %d", pogled, n, broj)
		}
	}

	if w := p.posalji("dežurni", http.MethodGet, "/readings/station/"+uuid.NewString()+"/ocitanja.csv", nil, ""); w.Code != http.StatusNotFound {
		t.Errorf("nepostojeća letva: %d", w.Code)
	}
}

// Objekt ima svoja očitanja i svoje ime datoteke; letva bez šifre dobije
// „letva” u imenu.
func TestIzvozOcitanjaObjekta(t *testing.T) {
	p := noviPosluziteljOcitanja(t)
	ctx := context.Background()
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	cs := &models.Structure{Code: "cs-primjerovo", Name: "CS Primjerovo", Kind: models.StructureKindPumpingStation, SectorID: "P", AreaID: 1}
	if err := p.structures.Create(ctx, admin, cs); err != nil {
		t.Fatal(err)
	}
	v := 210
	rd := models.Reading{ID: uuid.New(), StructureID: cs.ID.String(), MeasuredAt: time.Date(2014, 1, 2, 8, 0, 0, 0, time.UTC),
		LevelCm: &v, Source: models.ReadingSourceAutomatic}
	if err := p.ocitanja.Create(ctx, &rd); err != nil {
		t.Fatal(err)
	}
	p.upisi(t, "dežurni", time.Date(2014, 1, 2, 8, 0, 0, 0, time.UTC), 11, nil) // letve, ne ide u izvoz objekta

	w := p.posalji("dežurni", http.MethodGet, "/readings/structure/"+cs.ID.String()+"/ocitanja.csv", nil, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Disposition"), `filename="cs-primjerovo_ocitanja_`) {
		t.Fatalf("izvoz objekta: %d %q", w.Code, w.Header().Get("Content-Disposition"))
	}
	redci := citajCSV(t, w.Body.String())
	if len(redci) != 2 || redci[1][0] != rd.ID.String() || redci[1][2] != "210" || redci[1][9] != "automatski" {
		t.Errorf("očitanja objekta: %q", redci)
	}

	bez := &models.Station{ID: uuid.New(), Name: "Bezimena"}
	if err := repository.NewStationRepository(p.baza, ledger.New(p.baza, "test")).CreateStation(ctx, bez); err != nil {
		t.Fatal(err)
	}
	w = p.posalji("dežurni", http.MethodGet, "/readings/station/"+bez.ID.String()+"/ocitanja.csv", nil, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Disposition"), `filename="letva_ocitanja_`) {
		t.Errorf("letva bez šifre: %d %q", w.Code, w.Header().Get("Content-Disposition"))
	}
}

// Vraćena datoteka: tko ne upisuje na letvu ne dolazi do pregleda, a
// datoteka koja nije stigla ili nije odavde vraća na letvu s razlogom.
func TestUvozOcitanjaOdbijeniSlucajevi(t *testing.T) {
	p := noviPosluziteljOcitanja(t)
	put := "/readings/station/" + p.letva.ID.String() + "/ocitanja/uvoz"
	natrag := "/readings/station/" + p.letva.ID.String()

	p.dodajStranca(t) // osoba iz drugog sektora ne upisuje na letvu P

	for ime, slucaj := range map[string]struct {
		w      func() *httptest.ResponseRecorder
		poruka string
	}{
		"bez prava": {func() *httptest.ResponseRecorder {
			return p.datoteka(t, "stranac", put, "a.csv", "id;vrijeme;vodostaj_cm\n", nil)
		}, "Nemate pravo mijenjati očitanja ove letve"},
		"bez datoteke": {func() *httptest.ResponseRecorder {
			return p.datoteka(t, "dežurni", put, "", "", map[string]string{"x": "1"})
		}, "Odaberi datoteku"},
		"nije obrazac s datotekom": {func() *httptest.ResponseRecorder {
			return p.obrazac("dežurni", put, url.Values{"x": {"1"}})
		}, "Datoteka nije primljena: "},
		"tuđa datoteka": {func() *httptest.ResponseRecorder {
			return p.datoteka(t, "dežurni", put, "a.csv", "datum;cm\n1.1.2014.;5\n", nil)
		}, "datoteci nedostaje stupac „id“, „vrijeme“ ili „vodostaj_cm“ — je li izvezena odavde?"},
		"samo zaglavlje": {func() *httptest.ResponseRecorder {
			return p.datoteka(t, "dežurni", put, "a.csv", strings.Join(stupciOcitanja, ";")+"\n", nil)
		}, "datoteka nema nijedan redak s podacima"},
	} {
		putanja, poruka := porukaPreusmjerenja(t, slucaj.w(), "error")
		if putanja != natrag || !strings.HasPrefix(poruka, slucaj.poruka) {
			t.Errorf("%s: %s %q", ime, putanja, poruka)
		}
	}
	if w := p.datoteka(t, "dežurni", "/readings/station/"+uuid.NewString()+"/ocitanja/uvoz", "a.csv", "x", nil); w.Code != http.StatusNotFound {
		t.Errorf("nepostojeća letva: %d", w.Code)
	}
}

// Pregled na objektu: vraćena datoteka objekta broji izmjenu i nepromijenjen
// redak, a natrag vodi na stranicu objekta.
func TestUvozOcitanjaObjektaPokazujePregled(t *testing.T) {
	p := noviPosluziteljOcitanja(t)
	ctx := context.Background()
	cs := &models.Structure{Code: "cs-primjerovo", Name: "CS Primjerovo", Kind: models.StructureKindPumpingStation, SectorID: "P", AreaID: 1}
	if err := p.structures.Create(ctx, &models.UserPermissions{IsGlobalAdmin: true}, cs); err != nil {
		t.Fatal(err)
	}
	upis := func(kad time.Time, cm int) models.Reading {
		v := cm
		rd := models.Reading{ID: uuid.New(), StructureID: cs.ID.String(), MeasuredAt: kad, LevelCm: &v,
			Source: models.ReadingSourceManual, UserID: p.osobe["dežurni"].ID.String()}
		if err := p.ocitanja.Create(ctx, &rd); err != nil {
			t.Fatal(err)
		}
		return rd
	}
	a := upis(time.Date(2014, 1, 2, 7, 0, 0, 0, models.Zagreb), 210)
	b := upis(time.Date(2014, 1, 3, 7, 0, 0, 0, models.Zagreb), 215)
	datoteka := "id;vrijeme;vodostaj_cm;nizvodni_cm\n" +
		a.ID.String() + ";2014-01-02 07:00;212;\n" +
		b.ID.String() + ";2014-01-03 07:00;215;\n"
	w := p.datoteka(t, "dežurni", "/readings/structure/"+cs.ID.String()+"/ocitanja/uvoz", "cs.csv", datoteka, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("pregled objekta: %d %q", w.Code, w.Header().Get("Location"))
	}
	html := w.Body.String()
	for _, zeli := range []string{"CS Primjerovo", "cs.csv", "1 za ispravak", "/readings/structure/" + cs.ID.String(), `name="id" value="` + a.ID.String() + `"`} {
		if !strings.Contains(html, zeli) {
			t.Errorf("pregled objekta nema %q", zeli)
		}
	}
	if strings.Contains(html, `name="id" value="`+b.ID.String()+`"`) {
		t.Error("nepromijenjen redak ponuđen za upis")
	}
}

// Potvrda upisuje ono što je bilo na zaslonu: ispravak vrijednosti i
// vremena, brisanje; nepromijenjen redak se preskače, a nepoznat ili
// neispravan identifikator ne dira ništa.
func TestPotvrdaIspravkaOcitanja(t *testing.T) {
	p := noviPosluziteljOcitanja(t)
	ctx := context.Background()
	kad := time.Date(2014, 3, 2, 7, 0, 0, 0, models.Zagreb)
	krivo := p.upisi(t, "dežurni", kad, -18, nil)
	visak := p.upisi(t, "dežurni", kad.Add(time.Hour), -120, nil)
	isto := p.upisi(t, "dežurni", kad.Add(2*time.Hour), -119, nil)
	put := "/readings/station/" + p.letva.ID.String() + "/ocitanja/potvrdi"
	rfc := func(t time.Time) string { return t.UTC().Format(time.RFC3339) }

	forma := url.Values{
		"id":          {krivo.ID.String(), visak.ID.String(), isto.ID.String(), "nije-id"},
		"vrijeme":     {rfc(kad.Add(30 * time.Minute)), rfc(visak.MeasuredAt), rfc(isto.MeasuredAt), rfc(kad)},
		"vodostaj":    {"-118", "-120", "-119", "1"},
		"nizvodni":    {"", "", "", ""},
		"temperatura": {"7,5", "", "", ""},
		"protok":      {"", "", "", ""},
		"ocitao":      {" Marko Markić ", "", "", ""},
		"napomena":    {"predznak", "", "", ""},
		"obrisi":      {"", "da", "", ""},
	}
	putanja, poruka := porukaPreusmjerenja(t, p.obrazac("vodočuvar", put, forma), "success")
	if putanja != "/readings/station/"+p.letva.ID.String() ||
		poruka != "Ispravljeno 1 očitanje, obrisano 1 očitanje. Svaka izmjena stoji u knjizi verzija s vašim imenom." {
		t.Errorf("potvrda: %s %q", putanja, poruka)
	}
	nakon, err := p.ocitanja.Get(ctx, krivo.ID)
	if err != nil || nakon == nil {
		t.Fatalf("ispravljeno očitanje: %v", err)
	}
	if *nakon.LevelCm != -118 || !nakon.MeasuredAt.Equal(kad.Add(30*time.Minute)) || nakon.TempC == nil || *nakon.TempC != 7.5 ||
		nakon.Observer != "Marko Markić" || nakon.Note != "predznak" {
		t.Errorf("ispravak nije upisan: %+v", nakon)
	}
	if rd, err := p.ocitanja.Get(ctx, visak.ID); err != nil || rd != nil {
		t.Error("očitanje za brisanje je ostalo")
	}
	if rd, err := p.ocitanja.Get(ctx, isto.ID); err != nil || rd == nil || *rd.LevelCm != -119 {
		t.Error("nepromijenjeno očitanje je dirano")
	}

	// obrisano očitanje više ne postoji: ništa se ne upisuje, razlog stoji u poruci
	putanja, poruka = porukaPreusmjerenja(t, p.obrazac("vodočuvar", put, url.Values{"id": {visak.ID.String()}, "obrisi": {"da"}}), "error")
	if poruka != "Nijedan redak nije promijenjen: očitanje više ne postoji" {
		t.Errorf("nepostojeće očitanje: %s %q", putanja, poruka)
	}
	// isti podaci drugi put ne mijenjaju ništa
	_, poruka = porukaPreusmjerenja(t, p.obrazac("vodočuvar", put, url.Values{"id": {isto.ID.String()},
		"vrijeme": {rfc(isto.MeasuredAt)}, "vodostaj": {"-119"}}), "error")
	if poruka != "Nijedan redak nije promijenjen" {
		t.Errorf("bez promjene: %q", poruka)
	}
	// nečitljivo vrijeme ne prolazi
	_, poruka = porukaPreusmjerenja(t, p.obrazac("vodočuvar", put, url.Values{"id": {isto.ID.String()},
		"vrijeme": {"2.3.2014. 07:00"}, "vodostaj": {"-100"}}), "error")
	if poruka != "Nijedan redak nije promijenjen: vrijeme nije čitljivo" {
		t.Errorf("nečitljivo vrijeme: %q", poruka)
	}
}

// Dežurna upisuje na letvu, ali ne odgovara za nju: svoje ispravlja, tuđe ne
// briše. Poruka broji prošle i kaže zašto ostali nisu.
func TestPotvrdaIspravkaPoPravuRetka(t *testing.T) {
	p := noviPosluziteljOcitanja(t)
	kad := time.Date(2014, 3, 2, 7, 0, 0, 0, models.Zagreb)
	svoje := p.upisi(t, "dežurni", kad, -18, nil)
	tudje := p.upisi(t, "vodočuvar", kad.Add(time.Hour), -120, nil)
	tudje2 := p.upisi(t, "vodočuvar", kad.Add(2*time.Hour), -121, nil)
	rfc := func(t time.Time) string { return t.UTC().Format(time.RFC3339) }
	forma := url.Values{
		"id":       {svoje.ID.String(), tudje.ID.String(), tudje2.ID.String()},
		"vrijeme":  {rfc(svoje.MeasuredAt), rfc(tudje.MeasuredAt), rfc(tudje2.MeasuredAt)},
		"vodostaj": {"-118", "-120", "-100"},
		"obrisi":   {"", "da", ""},
	}
	_, poruka := porukaPreusmjerenja(t, p.obrazac("dežurni", "/readings/station/"+p.letva.ID.String()+"/ocitanja/potvrdi", forma), "success")
	if poruka != "Ispravljeno 1 očitanje. Svaka izmjena stoji u knjizi verzija s vašim imenom. 2 retka nisu prošla: nemate pravo brisati ovo očitanje" {
		t.Errorf("poruka: %q", poruka)
	}
	if rd, err := p.ocitanja.Get(context.Background(), tudje.ID); err != nil || rd == nil {
		t.Error("tuđe očitanje obrisano bez prava")
	}
	if rd, err := p.ocitanja.Get(context.Background(), tudje2.ID); err != nil || rd == nil || *rd.LevelCm != -121 {
		t.Error("tuđe očitanje ispravljeno bez prava")
	}

	// tko ne upisuje na letvu ne dolazi do upisa
	p.dodajStranca(t)
	_, poruka = porukaPreusmjerenja(t, p.obrazac("stranac", "/readings/station/"+p.letva.ID.String()+"/ocitanja/potvrdi", forma), "error")
	if poruka != "Nemate pravo mijenjati očitanja ove letve" {
		t.Errorf("bez prava: %q", poruka)
	}
	if w := p.obrazac("dežurni", "/readings/station/"+uuid.NewString()+"/ocitanja/potvrdi", forma); w.Code != http.StatusNotFound {
		t.Errorf("nepostojeća letva: %d", w.Code)
	}
}

// Sitni pretvarači: cijeli broj za datoteku i iz obrasca, decimala s zarezom.
func TestPretvaraciOcitanjaCSV(t *testing.T) {
	v, n := 7, -118
	if cijeliBroj(nil) != "" || cijeliBroj(&v) != "7" || cijeliBroj(&n) != "-118" {
		t.Error("cijeli broj za datoteku")
	}
	for ulaz, zeli := range map[string]*int{"": nil, "  ": nil, "abc": nil, "12,5": nil, " -118 ": &n, "7": &v} {
		got := cmIzObrasca(ulaz)
		if (got == nil) != (zeli == nil) || (got != nil && *got != *zeli) {
			t.Errorf("cmIzObrasca(%q) = %v", ulaz, got)
		}
	}
	d := 1234.5
	if decimalaUDatoteku(nil) != "" || decimalaUDatoteku(&d) != "1234,5" {
		t.Errorf("decimala za datoteku: %q", decimalaUDatoteku(&d))
	}
	if vezaLetve(nil, nil) != "/readings" {
		t.Error("bez letve natrag na popis")
	}
}

// Redak koji se ne da pročitati stoji kao greška s razlogom; ostali stupci
// koji se ne mijenjaju ne diraju se, a prazan redak na kraju se preskače.
func TestCitanjeOcitanjaGreskePoStupcu(t *testing.T) {
	kad := time.Date(2014, 3, 2, 7, 0, 0, 0, models.Zagreb)
	rd := ocitanjeZaTest(kad, -118)
	rd.Note = "led"
	postojeca := map[uuid.UUID]models.Reading{rd.ID: rd}
	glava := "id;vrijeme;vodostaj_cm;nizvodni_cm;temperatura_c;protok_m3s\n"
	for redak, zeli := range map[string]string{
		";2014-03-02 07:00;-118;;;":                                    "", // prazan identifikator: preskače se
		"%s;;-118;;;":                                                  "vrijeme je prazno",
		"%s;2999-01-01 07:00;-118;;;":                                  "vrijeme je u budućnosti: 2999-01-01 07:00",
		"%s;2014-03-02 07:00;sto;;;":                                   "vodostaj: nije cijeli broj: sto",
		"%s;2014-03-02 07:00;-118;12,5;;":                              "nizvodni vodostaj: vodostaj se vodi u punim centimetrima: 12,5",
		"%s;2014-03-02 07:00;-118;;topla;":                             "temperatura: ",
		"%s;2014-03-02 07:00;-118;;;mnogo":                             "protok: ",
		"%s;2014-03-02 07:00;;;;":                                      "očitanje bez vodostaja, temperature i protoka — za brisanje upiši „da“ u stupac obrisi",
		"%s;2014-03-02 07:00;-118;3;7,25;231,5":                        "-",
		"%s;2014-03-02 07:00;-118;;;":                                  "=",
		"%s;2014-03-02 07:00;-118":                                     "=", // kratak redak: ostali stupci su prazni
		"%s;2014-03-02 07:00;-118,0 cm;;;":                             "=",
		"%s;2014-03-02T07:00;-118;;;":                                  "=",
		"%s;2.3.2014 07:00;-118;;;":                                    "=",
		"%s;2014-03-02 07:00;-118;;;\n%s;;;;;":                         "dvaput",
		"%s;2014-03-02;-118;;;":                                        "-", // samo datum: ponoć
		"%s;nikad;-118;;;":                                             "vrijeme nije čitljivo: nikad",
		"%s;2014-03-02 07:00;-118;;0,0;":                               "-",
		"%s;2014-03-02 07:00;-118;;;\n;;;;;\n\n":                       "=",
		"%s;2014-03-02 07:00;-118;;;  \n":                              "=",
		"%s;2014-03-02 07:00; -118 ;;;":                                "=",
		"%s;2014-03-02 07:00;-1 18;;;":                                 "=", // razmak kao razdjelnik tisućica se briše
		"%s;2014-03-02 07:00;--118;;;":                                 "vodostaj: nije cijeli broj: --118",
		"%s;2014-03-02 07:00;-118;;;\n%s;nikad;;;;":                    "dvaput",
		"%s;2014-03-02 07:00;-118;;;\nnije-id;x;;;;":                   "prvi stupac nije identifikator očitanja: nije-id",
		"%s;2014-03-02 07:00;-118;;;\n" + uuid.Nil.String() + ";x;;;;": "tog očitanja nema na ovoj letvi",
	} {
		sadrzaj := glava + strings.ReplaceAll(redak, "%s", rd.ID.String()) + "\n"
		redci, err := citajOcitanja([]byte(sadrzaj), postojeca)
		if err != nil {
			t.Errorf("%q: %v", redak, err)
			continue
		}
		switch zeli {
		case "":
			if len(redci) != 0 {
				t.Errorf("%q: %d redaka, želim 0", redak, len(redci))
			}
		case "=", "-": // bez promjene / s promjenom
			if len(redci) == 0 || redci[0].Greska != "" || redci[0].Mijenja() != (zeli == "-") {
				t.Errorf("%q: %+v", redak, redci)
			} else if redci[0].Novo.Note != "led" || redci[0].Novo.Observer != rd.Observer {
				t.Errorf("%q: stupci kojih nema su dirani: %+v", redak, redci[0].Novo)
			}
		default:
			zadnji := redci[len(redci)-1]
			if !strings.Contains(zadnji.Greska, zeli) {
				t.Errorf("%q: greška %q, želim %q", redak, zadnji.Greska, zeli)
			}
		}
	}
	if _, err := citajOcitanja([]byte("id;vrijeme\n\"nezatvoreno;x\n"), postojeca); err == nil || !strings.HasPrefix(err.Error(), "datoteka nije čitljiva: ") {
		t.Errorf("nečitljiva datoteka: %v", err)
	}
}

// Pregled pokazuje promjenu nizvodnog vodostaja, temperature i protoka
// onako kako se čitaju: u centimetrima i s decimalnim zarezom.
func TestRazlikeOcitanjaPoPolju(t *testing.T) {
	kad := time.Date(2014, 3, 2, 7, 0, 0, 0, models.Zagreb)
	staro := ocitanjeZaTest(kad, -118)
	novo := staro
	v2, temp, q := 12, 7.5, 231.4
	novo.Level2Cm, novo.TempC, novo.FlowM3s = &v2, &temp, &q
	got := razlike(staro, novo)
	zeli := []Izmjena{{"nizvodni", "—", "12 cm"}, {"temperatura", "", "7,5"}, {"protok", "", "231,4"}}
	if len(got) != len(zeli) {
		t.Fatalf("razlike: %+v", got)
	}
	for i := range zeli {
		if got[i] != zeli[i] {
			t.Errorf("razlika %d: %+v, želim %+v", i, got[i], zeli[i])
		}
	}
}

// Pogled 1900 su dani, a 1901 je već godina. Očitanja su 1901. i 2099.,
// pa ishod ne ovisi o danu kad test ide: 2099. je uvijek unutar zadnjih
// 1900 dana, a nijedno nije u 1900. godini.
func TestGranicaGodineIDanaUPogledu(t *testing.T) {
	stara := models.Reading{ID: uuid.New(), MeasuredAt: time.Date(1901, 6, 1, 8, 0, 0, 0, time.UTC)}
	buduca := models.Reading{ID: uuid.New(), MeasuredAt: time.Date(2099, 6, 1, 8, 0, 0, 0, time.UTC)}
	sve := []models.Reading{stara, buduca}
	if got := uRazdoblju(sve, "1900"); len(got) != 1 || got[0].ID != buduca.ID {
		t.Errorf("pogled 1900 (dani): %d očitanja", len(got))
	}
	if got := uRazdoblju(sve, "1901"); len(got) != 1 || got[0].ID != stara.ID {
		t.Errorf("pogled 1901 (godina): %d očitanja", len(got))
	}
	if got := uRazdoblju(sve, "2099"); len(got) != 1 || got[0].ID != buduca.ID {
		t.Errorf("pogled 2099 (godina): %d očitanja", len(got))
	}
}
