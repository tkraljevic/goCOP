package prognoza

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Isječak njihove datoteke s izmišljenim brojevima: vrijeme je MEZ, redci po
// četvrt sata, a puni sat 19:00 MEZ je 18:00 UTC.
const ispisNOEL = `Datenqualität;!ungeprüfte Rohdaten!;;
Stationsname;Wildungsmauer;;
Stationsnummer;207373;;
Parameter;WasserstandPrognose;;
Zeitreihenname;Wahrscheinlichste Prognose;Vertrauensbreich;Vertrauensbreich
von;2026-09-25 18:00:00;;
bis;2026-09-26 01:00:00;;
Einheit;cm;;
;;;
Datum;Mittel;Min;Max
2026-09-25 18:00:00;103;103;103
2026-09-25 18:15:00;103;103;103
2026-09-25 18:30:00;104;103;105
2026-09-25 18:45:00;104;103;105
2026-09-25 19:00:00;104.6;98.0;111.0
2026-09-25 19:15:00;105;98;112
2026-09-25 20:00:00;106;99;113
`

func TestCitaAustrijskuPrognozu(t *testing.T) {
	l, err := CitajNOEL(ispisNOEL)
	if err != nil {
		t.Fatal(err)
	}
	if SifraNOEL(l.Naziv) != "wildungsmauer" {
		t.Fatalf("naziv %q", l.Naziv)
	}
	if l.ImaDanas {
		t.Error("austrijska datoteka nema jutarnje mjerenje")
	}
	if len(l.Dani) != 3 {
		t.Fatalf("dana %d, želim 3 puna sata: %+v", len(l.Dani), l.Dani)
	}
	if got := l.Dani[1]; got.Cm != 105 || got.PlusMin != 7 || !got.Kad.Equal(time.Date(2026, 9, 25, 18, 0, 0, 0, time.UTC)) {
		t.Errorf("sat 19 MEZ: %+v", got)
	}
	if !l.Izdano.UTC().Equal(time.Date(2026, 9, 25, 17, 0, 0, 0, time.UTC)) {
		t.Errorf("izdano %v, želim 18:00 MEZ = 17:00 UTC", l.Izdano)
	}
}

func TestDohvatiNOELVracaStoDobije(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/207373_") {
			fmt.Fprint(w, ispisNOEL)
			return
		}
		http.Error(w, "nema", http.StatusNotFound)
	}))
	defer srv.Close()
	letve, err := dohvatiNOEL(context.Background(), nil, srv.URL+"/")
	if err != nil || len(letve) != 1 || letve[0].Naziv != "Wildungsmauer" {
		t.Fatalf("letve %+v, err %v", letve, err)
	}
	prazan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nema", http.StatusNotFound)
	}))
	defer prazan.Close()
	if _, err := dohvatiNOEL(context.Background(), nil, prazan.URL+"/"); err == nil {
		t.Error("bez ijedne letve mora biti pogreška")
	}
}
