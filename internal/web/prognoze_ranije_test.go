package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gocop/internal/prognoza"
)

// Sati dvaju izdanja u bazi za izvoz ranije prognoze: 5. 10. 2026. u 10 h
// i 9. 10. 2026. u 19 h po mjesnom vremenu.
var (
	satStarog = time.Date(2026, 10, 5, 8, 0, 0, 0, time.UTC).Unix() / 3600
	satNovog  = time.Date(2026, 10, 9, 17, 0, 0, 0, time.UTC).Unix() / 3600
)

// bazaDvaIzdanja: postojeća fikstura s izdanjem 5. 10. (vrijednosti 30 +
// sat) i novije izdanje 9. 10. s drugim brojkama (500 + sat). Svako ima svoj
// zapis, dnevnu prognozu (77 i 777) i mađarsku prognozu koju je izdavač tada
// imao (222 i 333); staro izdanje nosi upozorenje, novo nijedno.
func bazaDvaIzdanja(t *testing.T) string {
	t.Helper()
	put := bazaPrognoza(t, satStarog)
	db, err := prognoza.Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var izdane []prognoza.Izdana
	for _, letva := range []string{"belisce", "donji-miholjac"} {
		for sat := int64(0); sat <= 72; sat++ {
			v := 500 + float64(sat)
			izdane = append(izdane, prognoza.Izdana{Letva: letva, Velicina: "vodostaj", Izdano: satNovog, Ciljni: satNovog + sat,
				Vrijednost: v, Dolje: v - 2, Gore: v + 2, Model: "proba"})
		}
	}
	if err := prognoza.SpremiIzdane(db, izdane); err != nil {
		t.Fatal(err)
	}
	for _, iz := range []struct {
		sat    int64
		cm, hu float64
	}{{satStarog, 77, 222}, {satNovog, 777, 333}} {
		var dnevne []prognoza.DnevnaIzdana
		for dan := 0; dan <= 6; dan++ {
			dnevne = append(dnevne, prognoza.DnevnaIzdana{Letva: "belisce", Izdano: iz.sat, Dan: dan,
				Ciljni: iz.sat + int64(24*dan), Vrijednost: iz.cm, Dolje: iz.cm - 5, Gore: iz.cm + 5, Model: prognoza.ModelDnevni})
		}
		if err := prognoza.SpremiDnevne(db, dnevne); err != nil {
			t.Fatal(err)
		}
		_, ciljevi := daniPregleda(satOdEpohe(iz.sat))
		if _, err := db.Exec(`INSERT INTO tude (izvor, letva, izdano, ciljni, vrijednost, raspon) VALUES (?, 'belisce', ?, ?, ?, 0)`,
			prognoza.Podrijetlo, iz.sat-2, ciljevi[0], iz.hu); err != nil {
			t.Fatal(err)
		}
	}
	upozorenja := map[int64][]prognoza.Upozorenje{
		satStarog: {{Vrsta: prognoza.VrstaUlaz, Tezina: prognoza.TezinaUpozorenje, Letva: "moslavina", Zadnje: satStarog - 11,
			Upitno: []prognoza.Upitno{{Letva: "belisce", OdSata: 7}}}},
		satNovog: {},
	}
	for _, sat := range []int64{satStarog, satNovog} {
		if _, err := prognoza.SpremiZapisIzdanja(db, prognoza.ZapisIzdanja{Izdano: sat, Cvor: "cvor-izdavac",
			Upozorenja: upozorenja[sat]}); err != nil {
			t.Fatal(err)
		}
	}
	return put
}

// rukovateljRanijih je rukovatelj prognoza nad bazom s dva izdanja; kiša po
// slivovima javlja iz kojeg je izdanja tražena.
func rukovateljRanijih(t *testing.T) *PrognozeHandler {
	t.Helper()
	c, err := OtvoriPrognoze(bazaDvaIzdanja(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	h := NewPrognozeHandler(nil, func() *CitacPrognoza { return c }, nil)
	h.kisa = func() KisaSlivovaFunc {
		return func(ctx context.Context) ([]prognoza.StanjeSliva, map[string][]prognoza.DnevnaIzdana, error) {
			palo := 99.0
			if sat, ranije := IzdanjeZahtjeva(ctx); ranije && sat == satStarog {
				palo = 11
			}
			return []prognoza.StanjeSliva{{Sliv: "Z", Razina: 1, Palo72: palo, Dolazi48: 1}}, nil, nil
		}
	}
	return h
}

// izvezi poziva izvoz s upitom i vraća odgovor.
func izvezi(h *PrognozeHandler, upit string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.IzvoziPrognoze(w, httptest.NewRequest(http.MethodGet, "/prognoze.xlsx"+upit, nil))
	return w
}

// Izvoz ranijeg izdanja nosi brojke, dnevnu prognozu, upozorenja, kišu i
// mađarsku prognozu toga izdanja, a ne zadnjega; bez dana i sata izvoz je
// kao dosad.
func TestIzvozRanijegIzdanja(t *testing.T) {
	h := rukovateljRanijih(t)
	w := izvezi(h, "?dan=2026-10-05&sat=10")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if d := w.Header().Get("Content-Disposition"); !strings.Contains(d, "prognoza_izdanje_2026-10-05_10h.xlsx") {
		t.Errorf("ime datoteke: %s", d)
	}
	staro := tekstZipa(t, w.Body.Bytes())
	for _, want := range []string{"izdano 5.10.2026. u 10:00 (ranije izdanje)", "Ranije izdanje, izvezeno",
		"Prognoza izdana 5.10.2026. u 10:00.", ">36<", ">42<", ">77<", ">222<", "nema mjerenja", "palo 72 h 11 mm"} {
		if !strings.Contains(staro, want) {
			t.Errorf("u izvozu ranijeg izdanja nema %q", want)
		}
	}
	for _, nema := range []string{">506<", ">777<", ">333<", "nije izdana;", "Zadnja prognoza izdana je"} {
		if strings.Contains(staro, nema) {
			t.Errorf("u izvozu ranijeg izdanja je %q", nema)
		}
	}

	w = izvezi(h, "")
	if w.Code != http.StatusOK {
		t.Fatalf("zadnje izdanje: status %d: %s", w.Code, w.Body.String())
	}
	if d := w.Header().Get("Content-Disposition"); strings.Contains(d, "izdanje") {
		t.Errorf("ime datoteke zadnjeg izdanja: %s", d)
	}
	zadnje := tekstZipa(t, w.Body.Bytes())
	for _, want := range []string{">506<", ">777<", ">333<", "palo 72 h 99 mm"} {
		if !strings.Contains(zadnje, want) {
			t.Errorf("u izvozu zadnjeg izdanja nema %q", want)
		}
	}
	for _, nema := range []string{"ranije izdanje", ">222<", "nema mjerenja"} {
		if strings.Contains(zadnje, nema) {
			t.Errorf("u izvozu zadnjeg izdanja je %q", nema)
		}
	}
}

// Sat bez izdanja daje najbliže ranije, s napomenom; zastarjelost se računa
// prema traženom satu. Prije prvog izdanja i uz krivi upit izvoza nema.
func TestIzvozRanijegBezIzdanjaUSatu(t *testing.T) {
	h := rukovateljRanijih(t)
	w := izvezi(h, "?dan=2026-10-05&sat=15")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if d := w.Header().Get("Content-Disposition"); !strings.Contains(d, "prognoza_izdanje_2026-10-05_10h.xlsx") {
		t.Errorf("ime datoteke: %s", d)
	}
	txt := tekstZipa(t, w.Body.Bytes())
	for _, want := range []string{"Za 5.10.2026. u 15:00 prognoza nije izdana; najbliže ranije izdanje je 5.10.2026. u 10:00.",
		">36<", "Zadnja prognoza izdana je za 5. 10. u 10 h — prije 5 h"} {
		if !strings.Contains(txt, want) {
			t.Errorf("u izvozu nema %q", want)
		}
	}
	if strings.Contains(txt, ">506<") {
		t.Error("izvezeno novije izdanje")
	}

	w = izvezi(h, "?dan=2026-10-01&sat=9")
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "najstarije izdanje u bazi je 5.10.2026. u 10:00") {
		t.Errorf("prije prvog izdanja: %d %s", w.Code, w.Body.String())
	}
	for _, upit := range []string{"?dan=5.10.2026.&sat=9", "?dan=2026-10-05&sat=24", "?dan=2026-10-05"} {
		if w := izvezi(h, upit); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d", upit, w.Code)
		}
	}
	prazan := NewPrognozeHandler(nil, nil, nil)
	if w := izvezi(prazan, "?dan=2026-10-05&sat=10"); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "Nijedna prognoza još nije izdana.") {
		t.Errorf("bez baze: %d %s", w.Code, w.Body.String())
	}
}

// Ponovno izdan sat: izvozi se zadnja verzija i to se kaže.
func TestPorukaRanijegIzdanja(t *testing.T) {
	iz := RanijeIzdanje{Trazeno: satOdEpohe(satStarog), Izdano: satOdEpohe(satStarog), Verzija: 2,
		Nastalo: time.Date(2026, 10, 5, 8, 41, 0, 0, time.UTC)}
	if got, want := iz.Poruka(), "Prognoza izdana 5.10.2026. u 10:00. Taj je sat izdan 2 puta; izvozi se zadnje izdanje, izračunato 10:41."; got != want {
		t.Errorf("%q\nželim\n%q", got, want)
	}
	if !strings.Contains(iz.Napomena(satOdEpohe(satNovog)), "Ranije izdanje, izvezeno 9.10.2026. u 19:00. Prognoza izdana") {
		t.Errorf("napomena: %q", iz.Napomena(satOdEpohe(satNovog)))
	}
	var nema *CitacPrognoza
	if nema.Na(satStarog) != nil {
		t.Error("prikovan čitač bez baze")
	}
	if _, ima := nema.OpisIzdanja(satOdEpohe(satStarog)); ima {
		t.Error("opis izdanja bez baze")
	}
	if _, ranije := IzdanjeZahtjeva(context.Background()); ranije {
		t.Error("zahtjev bez ranijeg izdanja ga ima")
	}
	if sat, ranije := IzdanjeZahtjeva(UzRanijeIzdanje(context.Background(), iz)); !ranije || sat != satStarog {
		t.Errorf("sat ranijeg izdanja %d %v", sat, ranije)
	}
}
