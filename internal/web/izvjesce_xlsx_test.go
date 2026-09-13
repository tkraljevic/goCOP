package web

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

func tekstKnjigeLetve(t *testing.T, iz IzvjesceLetve) string {
	t.Helper()
	var b bytes.Buffer
	if err := KnjigaLetve(iz, ZaglavljeIzvoza{
		Organizacija: "Hrvatske vode", Odjel: "VGO Osijek", Centar: "COP Osijek",
	}).Zapisi(&b); err != nil {
		t.Fatalf("sastavljanje xlsx: %v", err)
	}
	redci, err := procitajXLSX(b.Bytes())
	if err != nil {
		t.Fatalf("čitanje sastavljenog xlsx: %v", err)
	}
	var sve []string
	for _, red := range redci {
		sve = append(sve, red...)
	}
	return strings.Join(sve, "\n")
}

func TestExcelLetvePreslikavaTriStranice(t *testing.T) {
	tests := []struct {
		dio      string
		pripremi func(*IzvjesceLetve)
		mora     []string
		neSmije  []string
	}{
		{
			dio: izvjesceKartica,
			mora: []string{"VODOMJERNA POSTAJA BATINA", "Pragovi obrane od poplava",
				"Kota nule vodomjera", "Zabilježeni ekstremi"},
			neSmije: []string{"Povratni vodostaji", "Valovi obrane u nizu"},
		},
		{
			dio: izvjesceHistorijat,
			mora: []string{"HISTORIJAT VODOMJERNE POSTAJE BATINA", "Povratni vodostaji",
				"25 godina", "Valovi obrane u nizu"},
			neSmije: []string{"Pragovi obrane od poplava", "Kota nule vodomjera"},
		},
		{
			dio: izvjesceOcitanja,
			pripremi: func(iz *IzvjesceLetve) {
				iz.OcitanjaOpis = "zadnjih 30 dana"
				iz.Ocitanja = probnaOcitanja(time.Date(2026, 9, 8, 7, 0, 0, 0, models.Zagreb))
			},
			mora: []string{"OČITANJA VODOMJERNE POSTAJE BATINA", "zadnjih 30 dana",
				"660", "Ivan Horvat", "očitano s letve"},
			neSmije: []string{"Povratni vodostaji", "Pragovi obrane od poplava"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.dio, func(t *testing.T) {
			iz := probnoIzvjesce(t)
			iz.Dio = tc.dio
			if tc.pripremi != nil {
				tc.pripremi(&iz)
			}
			s := tekstKnjigeLetve(t, iz)
			for _, want := range tc.mora {
				if !strings.Contains(s, want) {
					t.Errorf("Excel %s nema %q", tc.dio, want)
				}
			}
			for _, ne := range tc.neSmije {
				if strings.Contains(s, ne) {
					t.Errorf("Excel %s nosi %q s druge stranice", tc.dio, ne)
				}
			}
		})
	}
}

func TestImeExcelIzvjescaRazlikujePoglede(t *testing.T) {
	st := models.Station{Code: "batina"}
	kad := time.Date(2026, 9, 13, 12, 0, 0, 0, models.Zagreb)
	vidjeno := map[string]bool{}
	for _, dio := range []string{izvjesceKartica, izvjesceHistorijat, izvjesceOcitanja} {
		ime := imeIzvjesca(st, kad, dio)
		if !strings.HasSuffix(ime, ".xlsx") {
			t.Errorf("ime nije Excel datoteka: %q", ime)
		}
		if vidjeno[ime] {
			t.Errorf("pogledi dijele ime %q", ime)
		}
		vidjeno[ime] = true
	}
}
