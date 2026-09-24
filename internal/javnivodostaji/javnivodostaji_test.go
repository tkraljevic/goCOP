package javnivodostaji

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

const probnaTablica = `<table><tr><th scope="col">DATUM</th><th scope="col">VRIJEME</th><th scope="col"><span>VODOSTAJ</span></th><th scope="col">TREND</th></tr>
<tr> <td>18.09.2026.</td> <td>10:00 h</td> <td>-119 cm</td> <td><div class="red">+2</div></td> </tr>
<tr> <td>18.09.2026.</td> <td>09:00 h</td> <td>-121 cm</td> <td><div class="blue">0</div></td> </tr>
<tr> <td>17.09.2026.</td> <td>23:00 h</td> <td>-121 cm</td> <td><div class="blue">0</div></td> </tr>
</table>`

// Tablica sa stranice čita se u satna očitanja, lokalno vrijeme u UTC,
// najstarije prvo
func TestCitanjeTabliceSaStranice(t *testing.T) {
	redci, err := CitajTablicu(probnaTablica)
	if err != nil {
		t.Fatal(err)
	}
	if len(redci) != 3 || redci[0].LevelCm == nil || *redci[0].LevelCm != -121 || redci[2].LevelCm == nil || *redci[2].LevelCm != -119 {
		t.Fatalf("redci: %+v", redci)
	}
	// 18.9.2026. 10:00 ljetno vrijeme = 08:00 UTC
	if !redci[2].Kad.Equal(time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("vrijeme nije pretvoreno iz lokalnog: %v", redci[2].Kad)
	}
	if _, err := CitajTablicu("<html>Sorry, an error occurred while processing your request.</html>"); err == nil {
		t.Error("greška stranice mora biti greška, ne prazno")
	}
	if _, err := CitajTablicu("<html><body>nešto drugo</body></html>"); err == nil {
		t.Error("stranica bez tablice mora biti greška")
	}
}

type probnoSpremiste struct {
	letve     []models.Station
	postojeca map[int64]bool
	upisano   []models.Reading
}

func (p *probnoSpremiste) LetveZaPreuzimanje(context.Context) ([]models.Station, error) {
	return p.letve, nil
}
func (p *probnoSpremiste) Postojeca(context.Context, string, time.Time, time.Time) (map[int64]bool, error) {
	return p.postojeca, nil
}
func (p *probnoSpremiste) Upisi(_ context.Context, o []models.Reading) (int, error) {
	p.upisano = append(p.upisano, o...)
	return len(o), nil
}

type probniPrijenos struct{ html string }

func (p probniPrijenos) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(p.html)), Header: http.Header{}}, nil
}

// Uvoznik upisuje samo ono čega letva još nema, i pamti stanje po letvi
func TestUvoznikUpisujeSamoNovo(t *testing.T) {
	st := models.Station{ID: uuid.New(), Name: "Batina", Code: "batina", JavniURL: "https://mvodostaji.voda.hr/Home/PregledVodostajaPostaje?sektorID=2&bpID=34&postajaID=424", JavniUvoz: true}
	vecIma := time.Date(2026, 9, 18, 7, 0, 0, 0, time.UTC) // 09:00 lokalno, zalijepljeno ranije
	sp := &probnoSpremiste{letve: []models.Station{st}, postojeca: map[int64]bool{vecIma.Unix(): true}}
	u := NoviUvoznik(sp, nil)
	u.Client.HTTP = &http.Client{Transport: probniPrijenos{probnaTablica}}

	if n := u.PreuzmiSve(context.Background()); n != 2 {
		t.Fatalf("novih %d, očekivano 2 (jedno već ima)", n)
	}
	for _, o := range sp.upisano {
		if o.Origin != Podrijetlo || o.Source != models.ReadingSourceImport || o.LevelCm == nil {
			t.Errorf("očitanje bez podrijetla: %+v", o)
		}
		if o.MeasuredAt.Equal(vecIma) {
			t.Error("upisano je i ono što letva već ima")
		}
	}
	s, ok := u.Stanje(st.ID.String())
	if !ok || s.Novih != 2 || s.Preuzeto != 3 || s.Greska != "" {
		t.Errorf("stanje letve: %+v", s)
	}
	// isti identitet za isti trenutak: ponovno preuzimanje daje iste ID-eve
	a := Ocitanje(&st, Redak{Kad: vecIma, LevelCm: intPtr(-121)}, Podrijetlo)
	b := Ocitanje(&st, Redak{Kad: vecIma, LevelCm: intPtr(-121)}, Podrijetlo)
	if a.ID != b.ID {
		t.Error("identitet očitanja nije stabilan")
	}
	// nepoznata adresa je greška na letvi, ne tiho ništa
	tudja := models.Station{ID: uuid.New(), Name: "Mohács", JavniURL: "https://www.hydroinfo.hu/Html/vizallas/mohacs.html"}
	if s := u.Preuzmi(context.Background(), &tudja); s.Greska == "" {
		t.Error("adresa bez čitača mora javiti grešku")
	}
}

// Broj postaje se čita iz adrese Hrvatskih voda, u oba oblika
func TestPostajaIzAdrese(t *testing.T) {
	for adresa, zeli := range map[string]int{
		"https://mvodostaji.voda.hr/Home/PregledVodostajaPostaje?sektorID=2&bpID=34&postajaID=424": 424,
		"https://vodostaji.voda.hr/Home/PregledVodostajaPostaje?postajaID=426":                     426,
		"https://www.hydroinfo.hu/?postajaID=5":                                                    0,
		"424":                                                                                      0,
		"":                                                                                         0,
	} {
		if id := PostajaIzAdrese(adresa); id != zeli {
			t.Errorf("%q: %d, očekivano %d", adresa, id, zeli)
		}
	}
}

// Adresa postaje bez sektora išla je na vodostaji.voda.hr, gdje ta putanja
// vraća 404 za svaku postaju — pa i za one koje inače rade. Letva upisana bez
// sektora dobivala je mrtvu adresu.
func TestAdresaPostajeNikadNeIdeNaMrtvuPutanju(t *testing.T) {
	for _, p := range []Postaja{{ID: 737, Sektor: 0}, {ID: 424, Sektor: 2}} {
		a := AdresaPostaje(p)
		if !strings.HasPrefix(a, AdresaStranice) {
			t.Errorf("postaja %d: adresa %q nije na %s", p.ID, a, AdresaStranice)
		}
		if !strings.Contains(a, "postajaID="+strconv.Itoa(p.ID)) {
			t.Errorf("postaja %d: adresa %q nema broj postaje", p.ID, a)
		}
	}
}

// Vrijeme s popisa piše se drukčije nego u tablici postaje.
func TestVrijemeSPopisa(t *testing.T) {
	kad, ok := vrijemeSPopisa("23.09.2026. 12:00 h")
	if !ok {
		t.Fatal("vrijeme se ne čita")
	}
	// 12:00 po zagrebačkom ljetnom vremenu je 10:00 UTC.
	if kad.UTC().Format("2006-01-02 15:04") != "2026-09-23 10:00" {
		t.Errorf("pročitano %s", kad.UTC().Format("2006-01-02 15:04"))
	}
	if _, ok := vrijemeSPopisa("nema vremena"); ok {
		t.Error("besmislica je pročitana kao vrijeme")
	}
}

// Dva kruga ne idu odjednom: dok jedan traje, drugi se ne pokreće; poslije
// kruga zna se kad je završio i koliko je letvi prošao.
func TestKrugPreuzimanjaNeIdeDvaputOdjednom(t *testing.T) {
	u := NoviUvoznik(&probnoSpremiste{}, nil)
	if _, ima := u.ZadnjiKrug(); ima || u.UTijeku() {
		t.Fatal("prije prvog kruga ne smije biti ni ishoda ni kruga u tijeku")
	}
	usao, pusti := make(chan struct{}), make(chan struct{})
	u.NakonPreuzimanja = func(context.Context) { close(usao); <-pusti }
	gotov := make(chan int)
	go func() { gotov <- u.PreuzmiSve(context.Background()) }()
	<-usao
	if !u.UTijeku() {
		t.Error("krug traje, a UTijeku kaže da ne")
	}
	if n := u.PreuzmiSve(context.Background()); n != 0 {
		t.Errorf("drugi krug usred prvoga vratio %d umjesto 0", n)
	}
	// Usred kruga: letve su prošle (90 %), faza je ona koju je javio korak.
	u.Korak("izračun prognoze", 96)
	if n := u.Napredak(); !n.UTijeku || n.Postotak != 96 || n.Faza != "izračun prognoze" {
		t.Errorf("napredak usred kruga: %+v", n)
	}
	close(pusti)
	<-gotov
	k, ima := u.ZadnjiKrug()
	if !ima || k.Kad.IsZero() || k.Letvi != 0 || u.UTijeku() {
		t.Errorf("poslije kruga: %+v, ima=%v, uTijeku=%v", k, ima, u.UTijeku())
	}
	if n := u.Napredak(); n.UTijeku || n.Postotak != 100 || n.Faza != "gotovo" || len(n.Redci) == 0 {
		t.Errorf("napredak poslije kruga: %+v", n)
	}
}
