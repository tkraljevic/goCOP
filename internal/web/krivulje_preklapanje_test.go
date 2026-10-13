package web

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"gocop/internal/arhiva"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Zaključava kako stranice biraju krivulju protoka kad se razdoblja
// valjanosti preklapaju: vrijedi krivulja s najkasnijim početkom koji nije
// poslije trenutka očitanja. Arhiva Donjeg Miholjca 13. 10. 2026. imala je
// krivulju od 1. 1. 2016. bez kraja, a uz nju krivulje od 2017., 2019. i
// 2025. Krivulje se upisuju izmiješanim redom i čitaju iz arhive kao na
// stranici. Pri vodostaju 0 cm protok je oznaka krivulje, pa se vidi koja je
// izabrana.
func TestProtokKadSeRazdobljaKrivuljaPreklapaju(t *testing.T) {
	put := filepath.Join(t.TempDir(), "arhiva.db")
	db, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	if err := arhiva.PripremiPraznu(db); err != nil {
		t.Fatal(err)
	}
	for _, k := range []struct {
		od, do string
		oznaka int
	}{
		{"2025-01-01", "", 25},
		{"2016-01-01", "", 16},
		{"2019-01-01", "2024-12-31", 19},
		{"2015-01-01", "2015-12-31", 15},
		{"2017-01-01", "2018-12-31", 17},
	} {
		res, err := db.Exec(`INSERT INTO hq_krivulje (letva, vrijedi_od, vrijedi_do) VALUES ('donji-miholjac', ?, ?)`, k.od, k.do)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		if _, err := db.Exec(`INSERT INTO hq_odsjecci (krivulja, od_cm, do_cm, oblik, p1, p2, p3) VALUES (?, -150, 600, 'polinom', 0, 100, ?)`,
			id, k.oznaka); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	arh, err := repository.OpenArhiva(put)
	if err != nil || arh == nil {
		t.Fatalf("arhiva: %v", err)
	}
	defer arh.Close()
	krivulje, err := arh.Krivulje(context.Background(), "donji-miholjac")
	if err != nil || len(krivulje) != 5 {
		t.Fatalf("krivulje: %d, %v", len(krivulje), err)
	}
	nula := 0
	for _, s := range []struct {
		kad    time.Time
		oznaka int
	}{
		{time.Date(2015, 6, 1, 7, 0, 0, 0, models.Zagreb), 15},
		{time.Date(2016, 6, 1, 7, 0, 0, 0, models.Zagreb), 16},
		{time.Date(2017, 1, 1, 0, 0, 0, 0, models.Zagreb), 17},
		{time.Date(2018, 12, 31, 23, 0, 0, 0, models.Zagreb), 17},
		{time.Date(2019, 1, 1, 0, 0, 0, 0, models.Zagreb), 19},
		{time.Date(2022, 6, 1, 7, 0, 0, 0, models.Zagreb), 19},
		{time.Date(2025, 1, 1, 0, 0, 0, 0, models.Zagreb), 25},
		{time.Date(2026, 10, 13, 7, 0, 0, 0, models.Zagreb), 25},
	} {
		if q, _ := protokIzKrivulje(krivulje, s.kad, &nula); q != strconv.Itoa(s.oznaka) {
			k := krivuljaZa(krivulje, s.kad)
			t.Errorf("%s: protok %q (krivulja %+v), očekivana krivulja s oznakom %d", s.kad.Format(time.DateTime), q, k, s.oznaka)
		}
	}
	if q, _ := protokIzKrivulje(krivulje, time.Date(2014, 6, 1, 7, 0, 0, 0, models.Zagreb), &nula); q != "" {
		t.Errorf("prije prve krivulje protok je %q", q)
	}
}
