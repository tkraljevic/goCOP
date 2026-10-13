package repository

import (
	"context"
	"reflect"
	"sort"
	"testing"
	"time"

	"gocop/internal/models"
)

// vodostajiPopisa su vodostaji očitanja redom kojim ih je popis vratio
func vodostajiPopisa(t *testing.T, popis []models.Reading) []int {
	t.Helper()
	out := []int{}
	for _, rd := range popis {
		if rd.LevelCm == nil {
			t.Fatalf("očitanje bez vodostaja u popisu: %+v", rd)
		}
		out = append(out, *rd.LevelCm)
	}
	return out
}

// Popis očitanja ide od najnovijeg; uz isto vrijeme mjerenja prvo je
// kasnije upisano. Letva, objekt, razdoblje (s oba ruba) i najveći broj
// sužavaju popis, a prazan filtar vraća sve.
func TestPopisOcitanjaPoFiltru(t *testing.T) {
	o := novaOkolinaOcitanja(t)
	ctx := context.Background()
	o.upisi(t, letvaPrimjerovo, "", u2026(time.March, 1, 7, 0), cmVodostaja(101), models.ReadingOriginGoCOP)
	o.upisi(t, letvaPrimjerovo, "", u2026(time.March, 3, 7, 0), cmVodostaja(103), models.ReadingOriginGoCOP)
	ranije := o.upisi(t, letvaPrimjerovo, "", u2026(time.March, 2, 7, 0), cmVodostaja(102), models.ReadingOriginGoCOP)
	kasnije := o.upisi(t, letvaBezimena, "", u2026(time.March, 2, 7, 0), cmVodostaja(42), models.ReadingOriginGoCOP)
	o.upisi(t, "", ustavaProbna, u2026(time.March, 4, 7, 0), cmVodostaja(75), models.ReadingOriginGoCOP)
	// isto vrijeme mjerenja: redoslijed odlučuje vrijeme upisa
	for id, kad := range map[string]string{ranije.ID.String(): "2026-03-02 08:00:00", kasnije.ID.String(): "2026-03-02 09:00:00"} {
		if _, err := o.baza.Exec(`UPDATE readings SET created_at = ? WHERE id = ?`, kad, id); err != nil {
			t.Fatal(err)
		}
	}

	for _, slucaj := range []struct {
		naziv  string
		filtar ReadingFilter
		ocek   []int
	}{
		{"sve", ReadingFilter{}, []int{75, 103, 42, 102, 101}},
		{"letva", ReadingFilter{StationID: letvaPrimjerovo}, []int{103, 102, 101}},
		{"objekt", ReadingFilter{StructureID: ustavaProbna}, []int{75}},
		{"od", ReadingFilter{StationID: letvaPrimjerovo, From: u2026(time.March, 2, 7, 0)}, []int{103, 102}},
		{"do", ReadingFilter{StationID: letvaPrimjerovo, To: u2026(time.March, 2, 7, 0)}, []int{102, 101}},
		{"od i do", ReadingFilter{From: u2026(time.March, 2, 7, 0), To: u2026(time.March, 3, 7, 0)}, []int{103, 42, 102}},
		{"najviše dva", ReadingFilter{Limit: 2}, []int{75, 103}},
		{"letva i objekt", ReadingFilter{StationID: letvaPrimjerovo, StructureID: ustavaProbna}, []int{}},
	} {
		popis, err := o.repo.List(ctx, slucaj.filtar)
		if err != nil {
			t.Fatalf("%s: %v", slucaj.naziv, err)
		}
		if dobiveno := vodostajiPopisa(t, popis); !reflect.DeepEqual(dobiveno, slucaj.ocek) {
			t.Errorf("%s: %v, očekivano %v", slucaj.naziv, dobiveno, slucaj.ocek)
		}
	}
	popis, err := o.repo.List(ctx, ReadingFilter{StructureID: ustavaProbna})
	if err != nil || len(popis) != 1 || popis[0].StructureID != ustavaProbna || popis[0].Observer != "Pero Perić" ||
		!popis[0].MeasuredAt.Equal(u2026(time.March, 4, 7, 0)) {
		t.Errorf("očitanje ustave iz popisa: %+v %v", popis, err)
	}
}

// Očitanja zadanih letvi: postaje i objekti zajedno (bilo koji od njih), u
// razdoblju s oba ruba; bez ijedne letve nema ni upita ni očitanja.
func TestOcitanjaZadanihLetvi(t *testing.T) {
	o := novaOkolinaOcitanja(t)
	ctx := context.Background()
	o.upisi(t, letvaPrimjerovo, "", u2026(time.March, 1, 7, 0), cmVodostaja(101), models.ReadingOriginGoCOP)
	o.upisi(t, letvaPrimjerovo, "", u2026(time.March, 2, 7, 0), cmVodostaja(102), models.ReadingOriginGoCOP)
	o.upisi(t, letvaBezimena, "", u2026(time.March, 2, 7, 0), cmVodostaja(42), models.ReadingOriginGoCOP)
	o.upisi(t, "", ustavaProbna, u2026(time.March, 3, 7, 0), cmVodostaja(75), models.ReadingOriginGoCOP)

	od, do := u2026(time.March, 2, 7, 0), u2026(time.March, 3, 7, 0)
	for _, slucaj := range []struct {
		naziv            string
		postaje, objekti []string
		od, do           time.Time
		ocek             []int
	}{
		{"jedna postaja", []string{letvaPrimjerovo}, nil, time.Time{}, time.Time{}, []int{101, 102}},
		{"dvije postaje", []string{letvaPrimjerovo, letvaBezimena}, nil, time.Time{}, time.Time{}, []int{42, 101, 102}},
		{"samo objekt", nil, []string{ustavaProbna}, time.Time{}, time.Time{}, []int{75}},
		{"postaja ili objekt", []string{letvaBezimena}, []string{ustavaProbna}, time.Time{}, time.Time{}, []int{42, 75}},
		{"od", []string{letvaPrimjerovo}, []string{ustavaProbna}, od, time.Time{}, []int{75, 102}},
		{"do", []string{letvaPrimjerovo}, []string{ustavaProbna}, time.Time{}, od, []int{101, 102}},
		{"od i do", []string{letvaPrimjerovo, letvaBezimena}, []string{ustavaProbna}, od, do, []int{42, 75, 102}},
		{"letva bez očitanja", []string{"nema-je"}, nil, time.Time{}, time.Time{}, []int{}},
	} {
		popis, err := o.repo.ListForGauges(ctx, slucaj.postaje, slucaj.objekti, slucaj.od, slucaj.do)
		if err != nil {
			t.Fatalf("%s: %v", slucaj.naziv, err)
		}
		dobiveno := vodostajiPopisa(t, popis)
		sort.Ints(dobiveno)
		if !reflect.DeepEqual(dobiveno, slucaj.ocek) {
			t.Errorf("%s: %v, očekivano %v", slucaj.naziv, dobiveno, slucaj.ocek)
		}
	}

	o.baza.Close()
	if popis, err := o.repo.ListForGauges(ctx, nil, []string{}, od, do); popis != nil || err != nil {
		t.Errorf("bez letvi: %v %v", popis, err)
	}
}

// Greška baze ne prolazi tiho: popis s nečitljivim zapisom i popis iz
// zatvorene baze vraćaju grešku, a ne prazan popis.
func TestPopisOcitanjaGreskeBaze(t *testing.T) {
	o := novaOkolinaOcitanja(t)
	ctx := context.Background()
	o.upisi(t, letvaPrimjerovo, "", u2026(time.March, 10, 7, 0), cmVodostaja(152), models.ReadingOriginGoCOP)
	if _, err := o.baza.Exec(`UPDATE readings SET measured_at = 'nije vrijeme'`); err != nil {
		t.Fatal(err)
	}
	if popis, err := o.repo.List(ctx, ReadingFilter{}); err == nil {
		t.Errorf("popis s nečitljivim zapisom bez greške: %+v", popis)
	}
	if popis, err := o.repo.ListForGauges(ctx, []string{letvaPrimjerovo}, nil, time.Time{}, time.Time{}); err == nil {
		t.Errorf("očitanja letve s nečitljivim zapisom bez greške: %+v", popis)
	}

	o.baza.Close()
	if _, err := o.repo.List(ctx, ReadingFilter{StationID: letvaPrimjerovo}); err == nil {
		t.Error("popis iz zatvorene baze bez greške")
	}
	if _, err := o.repo.ListForGauges(ctx, []string{letvaPrimjerovo}, nil, time.Time{}, time.Time{}); err == nil {
		t.Error("očitanja letve iz zatvorene baze bez greške")
	}
}
