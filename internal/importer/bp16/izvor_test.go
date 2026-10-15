package bp16

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Zaključava čitanje stare evidencije Baranje bez mreže: Directus glumi
// lažni poslužitelj ili prijenos koji test sam složi. Pokriva zbirke po
// stranicama, korisnike, datoteke, čitanje iz mape, prvu točku geometrije
// i sažetke izvješća.

// prijenos je http.RoundTripper iz funkcije, za odgovore koje pravi
// poslužitelj ne bi dao: prekinutu vezu i tijelo koje se ne da pročitati.
type prijenos func(*http.Request) (*http.Response, error)

func (p prijenos) RoundTrip(r *http.Request) (*http.Response, error) { return p(r) }

type pokvarenoTijelo struct{}

func (pokvarenoTijelo) Read([]byte) (int, error) { return 0, errors.New("veza prekinuta") }
func (pokvarenoTijelo) Close() error             { return nil }

func bezVeze() *http.Client {
	return &http.Client{Transport: prijenos(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("nema veze")
	})}
}

func tijeloPuca() *http.Client {
	return &http.Client{Transport: prijenos(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: pokvarenoTijelo{}, Request: r}, nil
	})}
}

// lazniDirectus odgovara na /items, /users i /assets; token mora stići u
// zaglavlju, inače odgovara 401.
func lazniDirectus(t *testing.T, zbirke map[string]int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer probni-token" {
			http.Error(w, `{"errors":[{"message":"Invalid user credentials."}]}`, http.StatusUnauthorized)
			return
		}
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/users":
			if q.Get("fields") != "id,first_name,last_name,email" || q.Get("limit") != "1000" {
				http.Error(w, "polja", http.StatusBadRequest)
				return
			}
			_, _ = fmt.Fprint(w, `{"data":[{"id":"u1","first_name":"Pero","last_name":"Perić","email":"pero@primjer.hr"}]}`)
		case r.URL.Path == "/users-pokvareno":
			_, _ = fmt.Fprint(w, `{"data":`)
		case strings.HasPrefix(r.URL.Path, "/assets/"):
			if q.Get("width") == "" && r.URL.RawQuery != "" {
				http.Error(w, "upit", http.StatusBadRequest)
				return
			}
			if r.URL.Path == "/assets/nema" {
				http.NotFound(w, r)
				return
			}
			_, _ = fmt.Fprintf(w, "slika %s %s", strings.TrimPrefix(r.URL.Path, "/assets/"), r.URL.RawQuery)
		case strings.HasPrefix(r.URL.Path, "/items/"):
			zbirka := strings.TrimPrefix(r.URL.Path, "/items/")
			ukupno, ima := zbirke[zbirka]
			if !ima {
				http.Error(w, `{"errors":[{"message":"Forbidden"}]}`, http.StatusForbidden)
				return
			}
			if zbirka == "pokvarena" {
				_, _ = fmt.Fprint(w, `{"data":[`)
				return
			}
			kljuc := map[string]string{"a02_stavke": "id_stavke", "evidencije_obilaska": "id_obilasci"}[zbirka]
			if kljuc == "" {
				kljuc = "id"
			}
			if q.Get("sort") != kljuc || q.Get("limit") != "1000" {
				http.Error(w, "sort "+q.Get("sort"), http.StatusBadRequest)
				return
			}
			od, _ := strconv.Atoi(q.Get("offset"))
			var zapisi []string
			for i := od; i < ukupno && i < od+1000; i++ {
				zapisi = append(zapisi, fmt.Sprintf(`{"%s":%d}`, kljuc, i+1))
			}
			_, _ = fmt.Fprintf(w, `{"data":[%s]}`, strings.Join(zapisi, ","))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Zbirka se čita stranicu po stranicu od tisuću zapisa, poredana po svom
// ključu, dok stranica nije kraća od pune.
func TestDirectusZbirkePoStranicama(t *testing.T) {
	srv := lazniDirectus(t, map[string]int{"a02_stavke": 1001, "evidencije_obilaska": 3, "cs_baranja": 0, "pokvarena": 1})
	src := HTTPSource{URL: srv.URL + "/", Token: "probni-token"}
	ctx := context.Background()
	for zbirka, ocekivano := range map[string]int{"a02_stavke": 1001, "evidencije_obilaska": 3, "cs_baranja": 0} {
		zapisi, err := src.Items(ctx, zbirka)
		if err != nil {
			t.Fatalf("%s: %v", zbirka, err)
		}
		if len(zapisi) != ocekivano {
			t.Errorf("%s: %d zapisa, očekivano %d", zbirka, len(zapisi), ocekivano)
		}
	}
	if _, err := src.Items(ctx, "zabranjena"); err == nil || !strings.Contains(err.Error(), "Directus odgovorio 403") {
		t.Errorf("zabranjena zbirka: %v", err)
	}
	if _, err := src.Items(ctx, "pokvarena"); err == nil || !strings.HasPrefix(err.Error(), "pokvarena:") {
		t.Errorf("nečitljiv odgovor: %v", err)
	}
	for ime, s := range map[string]HTTPSource{
		"neispravna adresa": {URL: "://bez-sheme"},
		"nema veze":         {URL: "http://directus.primjer", Client: bezVeze()},
		"tijelo puca":       {URL: "http://directus.primjer", Client: tijeloPuca()},
	} {
		if _, err := s.Items(ctx, "cs_baranja"); err == nil {
			t.Errorf("%s: prošlo bez greške", ime)
		}
	}
}

// Korisnici se čitaju jednim upitom s poljima za ime; odbijen token i
// nečitljiv odgovor vraćaju grešku.
func TestDirectusKorisnici(t *testing.T) {
	srv := lazniDirectus(t, nil)
	ctx := context.Background()
	korisnici, err := HTTPSource{URL: srv.URL, Token: "probni-token"}.Users(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(korisnici) != 1 || !strings.Contains(string(korisnici[0]), "Perić") {
		t.Errorf("korisnici: %s", korisnici)
	}
	if _, err := (HTTPSource{URL: srv.URL, Token: "kriv"}).Users(ctx); err == nil || !strings.Contains(err.Error(), "users: Directus odgovorio 401") {
		t.Errorf("odbijen token: %v", err)
	}
	if _, err := (HTTPSource{URL: srv.URL + "/users-pokvareno?", Token: "probni-token"}).Users(ctx); err == nil {
		t.Error("nečitljiv odgovor je prošao")
	}
	for ime, s := range map[string]HTTPSource{
		"neispravna adresa": {URL: "://bez-sheme"},
		"nema veze":         {URL: "http://directus.primjer", Client: bezVeze()},
		"tijelo puca":       {URL: "http://directus.primjer", Client: tijeloPuca()},
	} {
		if _, err := s.Users(ctx); err == nil {
			t.Errorf("%s: prošlo bez greške", ime)
		}
	}
}

// Datoteka se dohvaća s /assets/{id}, s Directusovom pretvorbom kad je
// zadana; nepostojeća datoteka je greška.
func TestDirectusDatoteke(t *testing.T) {
	srv := lazniDirectus(t, nil)
	ctx := context.Background()
	src := HTTPSource{URL: srv.URL, Token: "probni-token"}
	b, err := src.Asset(ctx, "a1b2", "width=1600&fit=inside&format=jpg")
	if err != nil || string(b) != "slika a1b2 width=1600&fit=inside&format=jpg" {
		t.Errorf("pretvorena slika %q, %v", b, err)
	}
	b, err = src.Asset(ctx, "a1b2", "")
	if err != nil || string(b) != "slika a1b2 " {
		t.Errorf("izvorna datoteka %q, %v", b, err)
	}
	if _, err := src.Asset(ctx, "nema", ""); err == nil || !strings.Contains(err.Error(), "assets/nema: Directus odgovorio 404") {
		t.Errorf("nepostojeća datoteka: %v", err)
	}
	for ime, s := range map[string]HTTPSource{
		"neispravna adresa": {URL: "://bez-sheme"},
		"nema veze":         {URL: "http://directus.primjer", Client: bezVeze()},
		"tijelo puca":       {URL: "http://directus.primjer", Client: tijeloPuca()},
	} {
		if _, err := s.Asset(ctx, "a1b2", ""); err == nil {
			t.Errorf("%s: prošlo bez greške", ime)
		}
	}
}

// Mapa s ranije skinutim zbirkama zamjenjuje Directus; korisnici su u
// directus_users.json.
func TestCitanjeIzMape(t *testing.T) {
	mapa := t.TempDir()
	writeJSON(t, mapa, "directus_users", []map[string]string{{"id": "u1", "first_name": "Ana"}})
	if err := os.WriteFile(filepath.Join(mapa, "pokvarena.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := DirSource{Dir: mapa}
	ctx := context.Background()
	korisnici, err := src.Users(ctx)
	if err != nil || len(korisnici) != 1 {
		t.Errorf("korisnici %s, %v", korisnici, err)
	}
	if _, err := src.Items(ctx, "nema"); err == nil {
		t.Error("nepostojeća zbirka je prošla")
	}
	if _, err := src.Items(ctx, "pokvarena"); err == nil || !strings.HasPrefix(err.Error(), "pokvarena:") {
		t.Errorf("nečitljiva zbirka: %v", err)
	}

	var redci []struct {
		ID string `json:"id"`
	}
	if err := fetchInto(ctx, src, "directus_users", &redci); err != nil || len(redci) != 1 || redci[0].ID != "u1" {
		t.Errorf("fetchInto: %+v, %v", redci, err)
	}
	if err := fetchInto(ctx, src, "nema", &redci); err == nil {
		t.Error("fetchInto bez zbirke je prošao")
	}
	var krivTip []int
	if err := fetchInto(ctx, src, "directus_users", &krivTip); err == nil {
		t.Error("fetchInto u krivi tip je prošao")
	}
}

// Iz geometrije se uzima prva točka: točka kako jest, a iz linije ili
// poligona prvi par koordinata.
func TestPrvaTockaGeometrije(t *testing.T) {
	slucajevi := []struct {
		geometrija string
		lon, lat   float64
		ok         bool
	}{
		{`[18.7, 45.8]`, 18.7, 45.8, true},
		{`[[18.7, 45.8], [18.8, 45.9]]`, 18.7, 45.8, true},
		{`[[[18.6, 45.7], [18.8, 45.9], [18.6, 45.7]]]`, 18.6, 45.7, true},
		{`[18.7]`, 0, 0, false},
		{`[]`, 0, 0, false},
		{`"točka"`, 0, 0, false},
		{`[["x"]]`, 0, 0, false},
	}
	for _, s := range slucajevi {
		lon, lat, ok := prvaTocka(json.RawMessage(s.geometrija))
		if lon != s.lon || lat != s.lat || ok != s.ok {
			t.Errorf("%s → (%v, %v, %v)", s.geometrija, lon, lat, ok)
		}
	}
}

// Zbirka se lista po svom ključu: tablice stavki i obilasci imaju vlastiti.
func TestKljucZbirke(t *testing.T) {
	for zbirka, kljuc := range map[string]string{
		"a02_stavke": "id_stavke", "a03_stavke": "id_stavke", "evidencije_obilaska": "id_obilasci", "cs_baranja": "id",
	} {
		if got := primaryKey(zbirka); got != kljuc {
			t.Errorf("%s → %q, očekivano %q", zbirka, got, kljuc)
		}
	}
}

// Opis objekta kaže na koje letve je vezan, ili da je bez letve.
func TestOpisObjekta(t *testing.T) {
	gornja, donja := uuid.New(), uuid.New()
	letve := []models.Station{{ID: gornja, Code: "kopacevo-uzvodno"}, {ID: donja, Code: "kopacevo-nizvodno"}}
	slucajevi := []struct {
		o    models.Structure
		opis string
	}{
		{models.Structure{Code: "bp16-cs-draz"}, "bp16-cs-draz (bez letve)"},
		{models.Structure{Code: "bp16-cs-draz", StationID: gornja.String()}, "bp16-cs-draz → kopacevo-uzvodno"},
		{models.Structure{Code: "bp16-ustava-kopacevo", StationID: gornja.String(), StationDownID: donja.String()},
			"bp16-ustava-kopacevo → kopacevo-uzvodno / nizvodno kopacevo-nizvodno"},
		{models.Structure{Code: "bp16-ustava-x", StationDownID: donja.String()}, "bp16-ustava-x / nizvodno kopacevo-nizvodno"},
		// letva koje nema u popisu ostaje bez šifre
		{models.Structure{Code: "bp16-cs-y", StationID: uuid.NewString()}, "bp16-cs-y → "},
	}
	for _, s := range slucajevi {
		o := s.o
		if got := opisObjekta(&o, letve); got != s.opis {
			t.Errorf("%q, očekivano %q", got, s.opis)
		}
	}
}

// Sažeci izvješća u jednom retku.
func TestSazeciIzvjesca(t *testing.T) {
	r := Report{Fetched: 10, Inserted: 6, Skipped: 2, IzvanRaspona: 3, Odbaceno: 1,
		NewStations: []string{"batina", "kopacevo"}, NoviObjekti: []string{"bp16-cs-draz"},
		DopunjeneVeze: []string{"a", "b"}, Unmapped: map[string]int{"Kanal Probni": 4}}
	ocekivano := "pročitano 10, upisano 6, već postojalo 2, nije upisano zbog raspona 1 (vrijednosti izvan raspona 3)" +
		", nove postaje: batina, kopacevo, novih objekata 1, objekata s dopunjenim letvama 2, preskočeno bez veze: Kanal Probni (4)"
	if got := r.Summary(); got != ocekivano {
		t.Errorf("sažetak uvoza:\n%s", got)
	}

	o := ObilasciReport{Ukupno: 5, Upisano: 3, Postoje: 1, Preskoceno: 1, BezKorisnika: 2, Listova: 4,
		SOdgovorom: 2, BezOdgovora: 1, Slika: 7, SObuhvatom: 1, VrijemeIzEvidencije: 2, VrijemeIzArhive: 1}
	if got := o.Summary(); got != "5 obilazaka: 3 upisano, 1 već postoji, 1 preskočeno, 2 bez poznatog vodočuvara; "+
		"4 listova, 2 s odgovorom, 1 bez odgovora, 7 fotografija, 1 s ucrtanim obuhvatom; vrijeme: 2 iz evidencije, 1 iz arhive" {
		t.Errorf("sažetak obilazaka:\n%s", got)
	}

	p := PrijaveReport{Ukupno: 4, Upisano: 2, Postoje: 1, BezKorisnika: 1, BezPodrucja: 0, Slika: 3, Skenova: 1, Nacrta: 2}
	if got := p.Summary(); got != "4 obavijesti: 2 upisano, 1 već postoji, 1 bez poznatog vodočuvara, 0 bez područja; 3 slika, 1 skenova, 2 nacrta" {
		t.Errorf("sažetak prijava:\n%s", got)
	}

	j := JournalReport{Records: 9, Journals: 2, Sheets: 3, Entries: 8, Supervisor: 1, Notes: 3, NewLocations: []string{"x"}, Unmatched: 2}
	if got := j.Summary(); got != "9 zapisa → 2 dnevnika, 3 listova, 8 upisa (1 nadzora) + 3 napomena o rekonstrukciji; 1 novih lokacija, 2 upisa bez lokacije" {
		t.Errorf("sažetak dnevnika:\n%s", got)
	}
}
