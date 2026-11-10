package web

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Jedan popis izvješća: noviji dan prvi, u danu izvješće sektora prije
// dionica; karta sektora za dan i čipovi vrste s brojem.
func TestPopisIzvjescaSlaganje(t *testing.T) {
	d1 := time.Date(2026, 10, 25, 0, 0, 0, 0, models.Zagreb)
	d2 := d1.AddDate(0, 0, 1)
	dionice := []models.DnevnoIzvjesce{{ID: "a", Dan: d2}, {ID: "b", Dan: d1}}
	sektorska := []models.SektorskoIzvjesce{{ID: "s1", Sektor: "B", Dan: d1}}
	r := slozenRedoslijed(dionice, sektorska)
	if len(r) != 3 || r[0].Dionice.ID != "a" || r[1].Sektorsko.ID != "s1" || r[2].Dionice.ID != "b" {
		t.Fatalf("redoslijed %+v", r)
	}
	k := karteSektora("2026-10-25", "", []string{"C", "B"}, sektorska)
	if len(k) != 2 || k[0].Sektor != "B" || k[0].Izvjesce == nil || !k[0].Sastavlja || k[1].Sektor != "C" || k[1].Izvjesce != nil {
		t.Errorf("karte %+v", k)
	}
	if k := karteSektora("2026-10-26", "C", []string{"B"}, sektorska); len(k) != 0 {
		t.Errorf("karte za drugi sektor %+v", k)
	}
	if k := karteSektora("2026-10-25", "", nil, sektorska); len(k) != 1 || k[0].Sastavlja {
		t.Errorf("karta bez sastavljanja %+v", k)
	}
	c := cipoviVrsteIzvjesca(url.Values{"sektor": {"B"}}, vrstaIzvjescaSektor, 2, 1)
	if len(c) != 3 || c[0].Naziv != "Sva (3)" || c[1].Naziv != "Dionice (2)" || !c[2].Odabran || !strings.Contains(c[1].URL, "vrsta=dionice") || !strings.Contains(c[1].URL, "sektor=B") {
		t.Errorf("čipovi %+v", c)
	}
}

// Strelice u prikazu Dan: dan prije uvijek, dan poslije samo do danas.
func TestStrelicePrikazaDan(t *testing.T) {
	d := &IzvjescaPageData{Danas: "2026-10-27", DanKarte: time.Date(2026, 10, 25, 0, 0, 0, 0, models.Zagreb)}
	strelicePrikazaDan(d, url.Values{"prikaz": {"dan"}})
	if !strings.Contains(d.DanPrije, "dan=2026-10-24") || !strings.Contains(d.DanPoslije, "dan=2026-10-26") {
		t.Errorf("strelice %q %q", d.DanPrije, d.DanPoslije)
	}
	d = &IzvjescaPageData{Danas: "2026-10-27", DanKarte: time.Date(2026, 10, 27, 0, 0, 0, 0, models.Zagreb)}
	strelicePrikazaDan(d, url.Values{})
	if d.DanPoslije != "" {
		t.Errorf("poslije danas: %q", d.DanPoslije)
	}
}

// Sektori za čipove: bez imenika nema ih, s imenikom su iz baze.
func TestSektoriZaPopis(t *testing.T) {
	if s, err := (&Server{}).sektoriZaPopis(); s != nil || err != nil {
		t.Errorf("bez imenika: %v %v", s, err)
	}
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "sektori.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if _, err := baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`); err != nil {
		t.Fatal(err)
	}
	userRepo := repository.NewUserRepository(baza, ledger.New(baza, "test"))
	srv := &Server{userService: service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())}
	s, err := srv.sektoriZaPopis()
	if err != nil || len(s) == 0 {
		t.Errorf("s imenikom: %v %v", s, err)
	}
}
