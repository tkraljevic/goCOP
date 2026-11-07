package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

func kalDan(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.ParseInLocation("2006-01-02", s, models.Zagreb)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// daniKalendara vraća dane kao „26.” da se tablica lako čita
func daniKalendara(dani []DanKalendara) string {
	var s []string
	for _, d := range dani {
		s = append(s, d.Datum.Format("2."))
	}
	return strings.Join(s, " ")
}

func TestPopisDanaPoMjesecima(t *testing.T) {
	otvoren := []models.Zadatak{{Tekst: "Obiđi nasip Draž", Status: models.ZadatakOtvoren}}
	obavljen := []models.Zadatak{{Tekst: "Očitaj letvu", Status: models.ZadatakObavljen}}
	for _, s := range []struct {
		ime, mjesec, danas string
		zadaci             map[string][]models.Zadatak
		dani, ostali, opis string
		prosli             string
		prosliZadataka     int
		prosliOtvoreni     bool
	}{
		{ime: "tekući mjesec kao na crtežu", mjesec: "2026-10-01", danas: "2026-10-26",
			zadaci: map[string][]models.Zadatak{"2026-10-02": obavljen, "2026-10-08": obavljen, "2026-10-26": otvoren, "2026-10-28": otvoren},
			dani:   "26. 27. 28. 29.", ostali: "30. 31.", opis: "30. i 31. 10.", prosli: "2. 8.", prosliZadataka: 2},
		{ime: "dan sa zadatkom nije među sklopljenima", mjesec: "2026-10-01", danas: "2026-10-05",
			zadaci: map[string][]models.Zadatak{"2026-10-20": otvoren},
			dani:   "5. 6. 7. 8. 20.", ostali: "9. 10. 11. 12. 13. 14. 15. 16. 17. 18. 19. 21. 22. 23. 24. 25. 26. 27. 28. 29. 30. 31.", opis: "9. do 19., 21. do 31. 10."},
		{ime: "prošli dan s otvorenim zadatkom se otvara", mjesec: "2026-10-01", danas: "2026-10-30",
			zadaci: map[string][]models.Zadatak{"2026-10-02": otvoren},
			dani:   "30. 31.", prosli: "2.", prosliZadataka: 1, prosliOtvoreni: true},
		{ime: "prvi dan mjeseca nema prošlih", mjesec: "2026-10-01", danas: "2026-10-01",
			dani: "1. 2. 3. 4.", ostali: "5. 6. 7. 8. 9. 10. 11. 12. 13. 14. 15. 16. 17. 18. 19. 20. 21. 22. 23. 24. 25. 26. 27. 28. 29. 30. 31.", opis: "5. do 31. 10."},
		{ime: "prošli mjesec je otvoren", mjesec: "2026-09-01", danas: "2026-10-26",
			zadaci: map[string][]models.Zadatak{"2026-09-14": obavljen},
			prosli: "14.", prosliZadataka: 1, prosliOtvoreni: true},
		{ime: "prošli mjesec bez zadataka", mjesec: "2026-09-01", danas: "2026-10-26"},
		{ime: "budući mjesec", mjesec: "2026-11-01", danas: "2026-10-26",
			zadaci: map[string][]models.Zadatak{"2026-11-10": otvoren},
			dani:   "1. 2. 3. 4. 10.", ostali: "5. 6. 7. 8. 9. 11. 12. 13. 14. 15. 16. 17. 18. 19. 20. 21. 22. 23. 24. 25. 26. 27. 28. 29. 30.", opis: "5. do 9., 11. do 30. 11."},
	} {
		t.Run(s.ime, func(t *testing.T) {
			mjesec, danas := kalDan(t, s.mjesec), kalDan(t, s.danas)
			najdalje := danas.AddDate(0, 0, models.NajdaljePlaniranje).Format("2006-01-02")
			p := slozPopisDana(mrezaKalendara(mjesec, danas, najdalje, s.zadaci), mjesec)
			if g := daniKalendara(p.Dani); g != s.dani {
				t.Errorf("dani = %q, treba %q", g, s.dani)
			}
			if g := daniKalendara(p.Ostali); g != s.ostali {
				t.Errorf("ostali = %q, treba %q", g, s.ostali)
			}
			if p.OstaliOpis != s.opis {
				t.Errorf("opis ostalih = %q, treba %q", p.OstaliOpis, s.opis)
			}
			if g := daniKalendara(p.Prosli); g != s.prosli || p.ProsliZadataka != s.prosliZadataka || p.ProsliOtvoreni != s.prosliOtvoreni {
				t.Errorf("prošli = %q, zadataka %d, otvoreni %v", g, p.ProsliZadataka, p.ProsliOtvoreni)
			}
		})
	}
}

// Popis nosi isto pravilo za + kao mreža: nikad na prošli dan ni dalje od dopuštenog.
func TestPlusUPopisuIMrezi(t *testing.T) {
	danas, mjesec := kalDan(t, "2026-10-26"), kalDan(t, "2026-11-01")
	najdalje := danas.AddDate(0, 0, models.NajdaljePlaniranje).Format("2006-01-02")
	tjedni := mrezaKalendara(mjesec, danas, najdalje, nil)
	ponudiZadavanje(tjedni, "vodocuvar-1", mjesec)
	p := slozPopisDana(tjedni, mjesec)
	for _, d := range append(append([]DanKalendara{}, p.Dani...), p.Ostali...) {
		if treba := d.Kljuc <= najdalje; d.Zadaj != treba {
			t.Errorf("%s: + = %v, treba %v", d.Kljuc, d.Zadaj, treba)
		}
	}
	for _, s := range []struct{ ime, danas, mjesec string }{
		{"prošli mjesec", "2026-10-26", "2026-09-01"},
		// Najdalji dan je 31. 1., a mreža veljače počinje ponedjeljkom 1. 2.
		{"sljedeći mjesec bez ijednog dopuštenog dana", "2027-01-01", "2027-02-01"},
	} {
		danas, mjesec := kalDan(t, s.danas), kalDan(t, s.mjesec)
		tjedni := mrezaKalendara(mjesec, danas, danas.AddDate(0, 0, models.NajdaljePlaniranje).Format("2006-01-02"), nil)
		ponudiZadavanje(tjedni, "vodocuvar-1", mjesec)
		for _, tj := range tjedni {
			for _, d := range tj {
				if d.Zadaj {
					t.Errorf("%s: %s nudi +", s.ime, d.Kljuc)
				}
			}
		}
	}
}

func TestNaslovDanaUPopisu(t *testing.T) {
	if g := (DanKalendara{Datum: kalDan(t, "2026-10-26"), Danas: true}).Naslov(); g != "Danas, pon 26. 10." {
		t.Errorf("danas: %q", g)
	}
	if g := (DanKalendara{Datum: kalDan(t, "2026-11-01")}).Naslov(); g != "ned 1. 11." {
		t.Errorf("drugi dan: %q", g)
	}
	danas := DanKalendara{Datum: kalDan(t, "2026-10-30")}
	if g := opisDana([]DanKalendara{danas}); g != "30. 10." {
		t.Errorf("jedan dan: %q", g)
	}
	if g := opisDana(nil); g != "" {
		t.Errorf("bez dana: %q", g)
	}
}

// Vodočuvar vidi svoj kalendar bez imena u naslovu, bez izbora osobe i bez plusa, a
// rukovoditelj s imenom, izborom i plusom. Mreža i popis stoje na stranici zajedno.
func TestKalendarVodocuvaraIRukovoditelja(t *testing.T) {
	p := noviPreneseniList(t)
	mjesec := p.dan.Format("2006-01")
	vlastiti := p.k.zovi(t, p.pero, http.MethodGet, "/vodocuvar/kalendar?mjesec="+mjesec, nil)
	if vlastiti.Code != http.StatusOK {
		t.Fatalf("vlastiti kalendar: %d", vlastiti.Code)
	}
	b := vlastiti.Body.String()
	for _, treba := range []string{"← Moja knjiga", ">Kalendar zadataka</h1>", `class="kal-dan`, `class="kal-popis"`, "bez upisanog odgovora", "kal-zadatak BEZ_ODGOVORA"} {
		if !strings.Contains(b, treba) {
			t.Errorf("vlastiti kalendar nema %q", treba)
		}
	}
	for _, ne := range []string{"Kalendar zadataka: Pero", `name="vodocuvar"`, `action="/vodocuvar/zadatak"`, "Zadatak se zadaje", models.ObjasnjenjeBezOdgovora} {
		if strings.Contains(b, ne) {
			t.Errorf("vlastiti kalendar ne smije imati %q", ne)
		}
	}

	// Tekući mjesec, jer je danas uvijek u njemu pa ima dan s +. Mjesec zadataka je tri
	// dana unatrag: prvih dana mjeseca to je prošli mjesec, u kojem + ne mora biti.
	tudji := p.k.zovi(t, p.rukovoditelj, http.MethodGet, "/vodocuvar/kalendar?vodocuvar="+p.pero.ID.String(), nil)
	b = tudji.Body.String()
	if tudji.Code != http.StatusOK {
		t.Fatalf("kalendar rukovoditelja: %d", tudji.Code)
	}
	for _, treba := range []string{"← Vodočuvarski dnevnik", "Kalendar zadataka: Pero Perić", `name="vodocuvar"`, `action="/vodocuvar/zadatak"`,
		"Zadatak se zadaje klikom na + u danu", "Zadatak se zadaje dodirom na + u danu", `class="kal-dan`} {
		if !strings.Contains(b, treba) {
			t.Errorf("kalendar rukovoditelja nema %q", treba)
		}
	}
}

// Prošli i budući mjesec otvaraju se na obje širine, a + ima samo dopušten dan.
func TestKalendarProsliIBuduciMjesec(t *testing.T) {
	p := noviPreneseniList(t)
	n := time.Now().In(models.Zagreb)
	prvi := time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, models.Zagreb)
	put := "/vodocuvar/kalendar?vodocuvar=" + p.pero.ID.String() + "&mjesec="

	prosli := p.k.zovi(t, p.rukovoditelj, http.MethodGet, put+prvi.AddDate(0, -3, 0).Format("2006-01"), nil)
	if prosli.Code != http.StatusOK {
		t.Fatalf("prošli mjesec: %d", prosli.Code)
	}
	if b := prosli.Body.String(); strings.Contains(b, `action="/vodocuvar/zadatak"`) || !strings.Contains(b, "U tom mjesecu nema zadataka.") {
		t.Error("prošli mjesec bez zadataka nudi + ili ne kaže da je prazan")
	}
	buduci := p.k.zovi(t, p.rukovoditelj, http.MethodGet, put+prvi.AddDate(0, 4, 0).Format("2006-01"), nil)
	if buduci.Code != http.StatusOK {
		t.Fatalf("budući mjesec: %d", buduci.Code)
	}
	if b := buduci.Body.String(); strings.Contains(b, `action="/vodocuvar/zadatak"`) || !strings.Contains(b, "Ostali dani do kraja mjeseca") {
		t.Error("mjesec dalje od 30 dana nudi + ili nema sklopljene dane")
	}
	// Sljedeći mjesec ima + samo ako njegova mreža počinje najkasnije na najdalji dopušteni
	// dan. Ne mora: 1. 1. 2027. najdalji je 31. 1., a mreža veljače počinje 1. 2.
	sljedeciPrvi := prvi.AddDate(0, 1, 0)
	danas := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, models.Zagreb)
	najdalje := danas.AddDate(0, 0, models.NajdaljePlaniranje).Format("2006-01-02")
	treba := mrezaKalendara(sljedeciPrvi, danas, najdalje, nil)[0][0].Kljuc <= najdalje
	sljedeci := p.k.zovi(t, p.rukovoditelj, http.MethodGet, put+sljedeciPrvi.Format("2006-01"), nil)
	if sljedeci.Code != http.StatusOK {
		t.Fatalf("sljedeći mjesec: %d", sljedeci.Code)
	}
	if g := strings.Contains(sljedeci.Body.String(), `action="/vodocuvar/zadatak"`); g != treba {
		t.Errorf("sljedeći mjesec: + = %v, treba %v (najdalji dan %s)", g, treba, najdalje)
	}
}

func TestStanjeZadatkaRijecju(t *testing.T) {
	for status, treba := range map[string]string{"": "otvoren", models.ZadatakOtvoren: "otvoren", models.ZadatakObavljen: "obavljen", models.ZadatakOdbacen: "nije obavljen", models.ZadatakBezOdgovora: "bez upisanog odgovora"} {
		if g := (models.Zadatak{ID: uuid.NewString(), Status: status}).StanjeRijecju(); g != treba {
			t.Errorf("%q: %q, treba %q", status, g, treba)
		}
	}
}
