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

// Excel historijata nosi što i stranica: za svaki od najviših valova kada bi
// po vodostaju počeo i završio svaki dosegnuti stupanj, ne samo početak i
// kraj vala i najviši stupanj (Odluke (8. 10. 2026.), pitanje 14).
func TestExcelHistorijataNosiVremenaSvakogStupnjaVala(t *testing.T) {
	iz := probnoIzvjesce(t)
	iz.Dio = izvjesceHistorijat
	iz.Sastavio = "Pero Perić"
	if len(iz.Valovi) != 1 || iz.Valovi[0].NajviseDosegnuto() != models.PhaseEmergency {
		t.Fatalf("priprema testa: %d valova", len(iz.Valovi))
	}
	s := tekstKnjigeLetve(t, iz)
	v := iz.Valovi[0]
	for _, z := range iz.ValoviZbroj {
		if !strings.Contains(s, z.Stupanj.Label()) {
			t.Errorf("u tablici valova nema stupca %s", z.Stupanj.Label())
		}
		st := v.StupanjZa(z.Stupanj)
		if st == nil {
			continue
		}
		vrijeme := st.PoceloHR() + " → " + st.ZavrsiloHR()
		if !strings.Contains(s, vrijeme) {
			t.Errorf("Excel nema kada bi %s počelo i završilo (%s)", z.Stupanj.Label(), vrijeme)
		}
	}
}

// Tablica valova u Excelu stane u list i za stupanj prekinut u valu kaže u
// koliko navrata, a za nedosegnuti stavlja crticu, kao i stranica.
func TestTablicaValovaStaneUListIBrojiNavrate(t *testing.T) {
	cm := func(v int) *int { return &v }
	st := models.Station{Prep: models.Threshold{Cm: cm(300)}, Regular: models.Threshold{Cm: cm(500)},
		Emergency: models.Threshold{Cm: cm(650)}, State: models.Threshold{Cm: cm(800)}}
	pragovi := st.PragoviObrane()
	poc := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	var niz []models.HidroTocka
	for _, d := range []struct{ sati, cm int }{{1, 100}, {24, 400}, {36, 600}, {12, 400}, {36, 600}, {24, 400}, {1, 100}} {
		for i := 0; i < d.sati; i++ {
			niz = append(niz, models.HidroTocka{Kad: poc.Add(time.Duration(len(niz)) * time.Hour), Vrijednost: float64(d.cm)})
		}
	}
	valovi := models.Valovi(niz, pragovi)
	zbroj := models.ZbrojValova(valovi, pragovi)
	glave, redci := tablicaValova(valovi, zbroj)
	if len(glave) != stupacaLetve || len(redci) != 1 || len(redci[0]) != len(glave) {
		t.Fatalf("stupaca %d (list ima %d), redaka %d", len(glave), stupacaLetve, len(redci))
	}
	redovna := valovi[0].StupanjZa(models.PhaseRegular)
	if redovna == nil || redovna.Navrata != 2 {
		t.Fatal("priprema testa: redovna obrana mora biti u dva navrata")
	}
	if c := redci[0][6]; glave[6] != models.PhaseRegular.Label() ||
		!strings.Contains(c, redovna.PoceloHR()+" → "+redovna.ZavrsiloHR()) || !strings.Contains(c, "u 2 navrata") {
		t.Errorf("stupac %s: %q", glave[6], c)
	}
	if glave[7] != models.PhaseEmergency.Label() || redci[0][7] != "—" {
		t.Errorf("nedosegnuta izvanredna obrana: %q", redci[0][7])
	}
}
