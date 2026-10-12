package web

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Oznaka otvaranja vrijedi samo za osobu i dan za koje je potpisana, i
// samo s tajnom poslužitelja koji ju je izdao (Odluke (11. 10. 2026.)).
func TestOznakaOtvaranja(t *testing.T) {
	o := noveOznakeOtvaranja()
	dan := time.Date(2026, 3, 10, 0, 0, 0, 0, models.Zagreb)
	kad := dan.Add(23*time.Hour + 50*time.Minute)
	ozn := o.oznaka("pero", dan, kad)
	if got := o.otvoreno(ozn, "pero", dan); !got.Equal(kad.Truncate(time.Second)) {
		t.Fatalf("ispravna oznaka: %v, želim %v", got, kad)
	}
	dio, potpis, _ := strings.Cut(ozn, ".")
	krivotvoren := base64.RawURLEncoding.EncodeToString([]byte("pero|2026-03-09|" + strings.Split(mustDecode(t, dio), "|")[2]))
	for ime, oznaka := range map[string]string{
		"tuđa tajna":         noveOznakeOtvaranja().oznaka("pero", dan, kad),
		"prepravljen dan":    krivotvoren + "." + potpis,
		"bez potpisa":        dio,
		"potpis nije base64": "!!!." + potpis,
		"prazna":             "",
		"krivi broj polja":   potpisano(o, "pero|2026-03-10"),
		"vrijeme nije broj":  potpisano(o, "pero|2026-03-10|sinoć"),
	} {
		if got := o.otvoreno(oznaka, "pero", dan); !got.IsZero() {
			t.Errorf("%s: %v", ime, got)
		}
	}
	if !o.otvoreno(ozn, "ana", dan).IsZero() || !o.otvoreno(ozn, "pero", dan.AddDate(0, 0, 1)).IsZero() {
		t.Error("oznaka vrijedi za drugu osobu ili drugi dan")
	}
	var bez *oznakeOtvaranja
	if bez.oznaka("pero", dan, kad) != "" || !bez.otvoreno(ozn, "pero", dan).IsZero() {
		t.Error("bez tajne oznaka ne smije postojati ni vrijediti")
	}
}

func mustDecode(t *testing.T, s string) string {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// potpisano je oznaka proizvoljnog sadržaja potpisana tajnom o
func potpisano(o *oznakeOtvaranja, sadrzaj string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(sadrzaj)) + "." + o.potpis(sadrzaj)
}

// Kroz rute: obrazac lista koji još ne postoji nosi oznaku otvaranja, a
// postojeći list ne. Jučerašnji list spremljen danas sprema se samo uz
// oznaku otvaranja od jučer; bez nje vrijedi pravilo da se list za dan koji
// je prošao više ne otvara.
func TestListNakonPonociKrozRute(t *testing.T) {
	o := novaOkolinaDnevnika(t)
	if w := o.zovi(t, o.vodocuvar, http.MethodGet, "/vodocuvar/dan?datum="+danZaTest(0), nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `name="otvoreno"`) {
		t.Fatalf("obrazac novog lista bez oznake otvaranja: %d", w.Code)
	}
	jucer := danZaTest(-1)
	dan, _ := time.ParseInLocation("2006-01-02", jucer, models.Zagreb)
	forma := func(otvoreno string) url.Values {
		return url.Values{"datum": {jucer}, "od": {"07:30"}, "do": {"15:30"}, "opis": {"obilazak nasipa do mraka"}, "otvoreno": {otvoreno}}
	}
	if w := o.zovi(t, o.vodocuvar, http.MethodPost, "/vodocuvar/spremi", forma("")); !strings.Contains(mustUnescape(w.Header().Get("Location")), "više se ne otvara") {
		t.Errorf("bez oznake jučerašnji list se otvorio: %s", mustUnescape(w.Header().Get("Location")))
	}
	ozn := o.vodH.otvaranje.oznaka(o.vodocuvar.ID.String(), dan, dan.Add(23*time.Hour+50*time.Minute))
	w := o.zovi(t, o.vodocuvar, http.MethodPost, "/vodocuvar/spremi", forma(ozn))
	if strings.Contains(mustUnescape(w.Header().Get("Location")), "error") {
		t.Fatalf("jučerašnji list s oznakom: %s", mustUnescape(w.Header().Get("Location")))
	}
	l, err := o.repo.ZaDan(t.Context(), o.vodocuvar.ID.String(), dan)
	if err != nil || l == nil || l.Opis != "obilazak nasipa do mraka" {
		t.Fatalf("jučerašnji list nije spremljen: %+v %v", l, err)
	}
	if w := o.zovi(t, o.vodocuvar, http.MethodGet, "/vodocuvar/"+l.ID, nil); strings.Contains(w.Body.String(), `name="otvoreno"`) {
		t.Error("spremljeni list nosi oznaku otvaranja")
	}
}
