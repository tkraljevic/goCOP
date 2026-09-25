package javnivodostaji

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Isječak njihove datoteke s izmišljenim brojevima: vrijeme je MEZ, pa je
// 21:00 MEZ 20:00 UTC; četvrtine se preskaču.
const ispisNOEL = `ungeprüfte Rohdaten!;
Stationsname;Angern an der March
Stationsnummer;207324
Parameter;Wasserstand
Zeitreihenname;Messwerte
von;2026-09-21 20:30:00
bis;2026-09-25 21:30:00
Einheit;cm
;
Datum;Wert
2026-09-25 20:30:00;52
2026-09-25 20:45:00;52
2026-09-25 21:00:00;51.6
2026-09-25 21:15:00;51
2026-09-25 22:00:00;50
2026-09-25 22:15:00;
`

func TestNOELPrepoznajeICita(t *testing.T) {
	adresa := "https://www.noel.gv.at/wasserstand/kidata/stationdata/207324_Wasserstand_3Tage.csv"
	if PostajaNOELIzAdrese(adresa) != "207324" {
		t.Fatalf("adresa nije prepoznata")
	}
	if PostajaNOELIzAdrese("https://www.noel.gv.at/wasserstand/kidata/stationdata/207324_WasserstandPrognose_48Stunden.csv") != "" {
		t.Error("prognoza nije mjerenje i ne smije se prepoznati")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, ispisNOEL)
	}))
	defer srv.Close()
	redci, err := NOEL{Base: srv.URL, Client: &Client{HTTP: srv.Client()}}.Ocitanja(context.Background(), adresa)
	if err != nil {
		t.Fatal(err)
	}
	if len(redci) != 2 {
		t.Fatalf("redaka %d, želim 2 puna sata: %+v", len(redci), redci)
	}
	if r := redci[0]; *r.LevelCm != 52 || !r.Kad.Equal(time.Date(2026, 9, 25, 20, 0, 0, 0, time.UTC)) {
		t.Errorf("prvi sat %+v (51,6 → 52; 21:00 MEZ = 20:00 UTC)", r)
	}
	if r := redci[1]; *r.LevelCm != 50 || !r.Kad.Equal(time.Date(2026, 9, 25, 21, 0, 0, 0, time.UTC)) {
		t.Errorf("drugi sat %+v", r)
	}
	if _, err := CitajNOEL("Datum;Wert\n2026-09-25 20:15:00;52\n"); err == nil {
		t.Error("bez punog sata mora biti pogreška")
	}
}
