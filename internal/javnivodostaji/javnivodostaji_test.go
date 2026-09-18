package javnivodostaji

import (
	"context"
	"io"
	"net/http"
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
	if len(redci) != 3 || redci[0].Cm != -121 || redci[2].Cm != -119 {
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
	a := Ocitanje(&st, Redak{Kad: vecIma, Cm: -121}, Podrijetlo)
	b := Ocitanje(&st, Redak{Kad: vecIma, Cm: -121}, Podrijetlo)
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
