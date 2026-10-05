package prognoza

import (
	"fmt"
	"maps"
	"os"
	"reflect"
	"slices"
	"testing"
)

// postavi mijenja globalnu postavku paketa za jedan test i vraća je kad test
// završi. Postavka ostavljena izmijenjenom prelazi u sljedeći test, pa ishod
// ovisi o redoslijedu kojim se testovi pokreću.
func postavi[T any](t *testing.T, p *T, v T) {
	t.Helper()
	staro := *p
	*p = v
	t.Cleanup(func() { *p = staro })
}

// postavke su globalne postavke koje testovi paketa mijenjaju, snimljene u
// jednom trenutku. Karte i odsječci su preslike, da se vidi i izmjena na
// mjestu.
type postavke struct {
	PoluvijekIspravka  float64
	StalniIspravakSati int
	TudaIspredRacuna   map[string]string
	VrhoviIzDnevnog    map[string]bool
	DnevniCiljevi      []DnevniCilj
}

func snimiPostavke() postavke {
	return postavke{
		PoluvijekIspravka:  PoluvijekIspravka,
		StalniIspravakSati: StalniIspravakSati,
		TudaIspredRacuna:   maps.Clone(TudaIspredRacuna),
		VrhoviIzDnevnog:    maps.Clone(VrhoviIzDnevnog),
		DnevniCiljevi:      slices.Clone(DnevniCiljevi),
	}
}

// razlike su imena postavki koje se razlikuju između dviju snimki.
func (p postavke) razlike(q postavke) []string {
	var out []string
	a, b := reflect.ValueOf(p), reflect.ValueOf(q)
	for i := range a.NumField() {
		if !reflect.DeepEqual(a.Field(i).Interface(), b.Field(i).Interface()) {
			out = append(out, a.Type().Field(i).Name)
		}
	}
	return out
}

// TestMain pazi da testovi vrate globalne postavke kakve su zatekli: koji ih
// ne vrati, ruši cijeli paket, pa i kad se pokrene sam (-run).
func TestMain(m *testing.M) {
	prije := snimiPostavke()
	kod := m.Run()
	if r := prije.razlike(snimiPostavke()); kod == 0 && len(r) > 0 {
		fmt.Fprintf(os.Stderr, "testovi nisu vratili globalne postavke: %v\n", r)
		kod = 1
	}
	os.Exit(kod)
}
