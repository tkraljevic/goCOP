package prognoza

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gocop/internal/oborine"
)

// laziOpenMeteo odgovara kao Open-Meteo kad je kod njega dan danas (UTC):
// sati od ponoći past_days unatrag do 23 h zadnjeg od forecast_days dana,
// kojima je danas prvi; svaki sat 1 mm.
func laziOpenMeteo(t *testing.T, danas time.Time) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		unatrag, err1 := strconv.Atoi(r.URL.Query().Get("past_days"))
		unaprijed, err2 := strconv.Atoi(r.URL.Query().Get("forecast_days"))
		if err1 != nil || err2 != nil {
			http.Error(w, "bez past_days ili forecast_days", http.StatusBadRequest)
			return
		}
		var sati, kisa []string
		kraj := danas.AddDate(0, 0, unaprijed)
		for s := danas.AddDate(0, 0, -unatrag); s.Before(kraj); s = s.Add(time.Hour) {
			sati = append(sati, `"`+s.Format("2006-01-02T15:04")+`"`)
			kisa = append(kisa, "1")
		}
		fmt.Fprintf(w, `[{"latitude":45.5,"longitude":18.6,"hourly":{"time":[%s],"precipitation":[%s]}}]`,
			strings.Join(sati, ","), strings.Join(kisa, ","))
	}))
}

// Krug u :45 zadnjeg sata UTC dana preuzme kišu još za taj dan, a izda za
// ponoć novog dana. Dnevni model tada mora imati kišu: prije je zadnjem danu
// prognoze falio zadnji sat, pa je cijeli model jednom dnevno išao bez kiše.
func TestKisaPreuzetaJucerDostajeZaIzdanjeUPonoc(t *testing.T) {
	jucer := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	srv := laziOpenMeteo(t, jucer)
	defer srv.Close()
	d, err := oborine.Otvori(filepath.Join(t.TempDir(), "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	u := &oborine.Uvoznik{DB: d, Adresa: srv.URL, Tocke: func() ([]oborine.Tocka, error) {
		return []oborine.Tocka{{Code: "k-g-ravnica-1", Lat: 45.5, Lon: 18.6}}, nil
	}}
	if _, err := u.Preuzmi(context.Background()); err != nil {
		t.Fatal(err)
	}

	sada := jucer.AddDate(0, 0, 1).Unix() / 3600
	iz := &OborinskiIzvor{Tocke: []OborinskaTocka{{Code: "k-g-ravnica-1", Sliv: "G", Tezina: 1}}, Satne: u.Satne}
	unatrag, unaprijed := oborinskiDosezi()
	kisa, err := OborineOkoSada(iz, sada, unatrag, unaprijed)
	if err != nil {
		t.Fatal(err)
	}
	nizovi := map[string]DnevniNiz{"cilj": {0: 100, -1: 99, -3: 97}}
	maps.Copy(nizovi, kisa)
	c := DnevniCilj{Letva: "cilj", Slivovi: []string{"G"}}
	if _, ok := DnevneZnacajke(c, nizovi, 0); !ok {
		t.Errorf("dnevni model bez kiše; dani kiše oko izdanja: %v", kisa[OborinaKljuc("G")])
	}
}
