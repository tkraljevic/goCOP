package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Roba na putu kroz stranice (Odluke 16. 11. 2026., Na putu): prijenos iz
// Bilja u Dardu je na putu dok ga Darda ne zaprimi. Skladište vidi što
// stiže i što je poslalo; zaprima se djelomično, a ostatak se otpisuje kao
// manjak uz razlog. Tko ne smije zaprimiti, obrazac ne dobije.
func TestRobaNaPutuKrozStranice(t *testing.T) {
	o := novaOkolinaSredstava(t)
	ctx := context.Background()
	darda := &models.Skladiste{Sektor: "B", AreaID: 34, Naziv: "Skladište Darda", Aktivno: true}
	if err := o.svc.SpremiSkladiste(ctx, upravaSredstava(), darda); err != nil {
		t.Fatal(err)
	}
	preusmjerenjeSredstava(t, o.zovi(t, upravaSredstava(), http.MethodPost, "/sredstva/skladista/"+o.sk.ID+"/promet",
		url.Values{"vrsta": {models.PrometPrijenos}, "sredstvo": {"lopata"}, "kolicina": {"5"}, "na_skladiste": {darda.ID}, "dokument": {"OTP 3/26"}}), "success")
	stize, err := o.svc.NaPutu(ctx, repository.FiltarPosiljki{PremaSkladisteID: darda.ID})
	if err != nil || len(stize) != 1 {
		t.Fatalf("na putu u Dardu: %+v %v", stize, err)
	}
	put := "/sredstva/na-putu/" + stize[0].Slanje.VezaID

	mora(t, o.ispis(t, promatracSredstava(), "/sredstva/skladista/"+darda.ID), "Darda", "Stize=1;", "Poslano=0;")
	mora(t, o.ispis(t, promatracSredstava(), "/sredstva/skladista/"+o.sk.ID), "Bilje", "Stize=0;", "Poslano=1;")

	if w := o.zovi(t, promatracSredstava(), http.MethodGet, put, nil); w.Code != http.StatusForbidden {
		t.Errorf("promatrač otvara zaprimanje: %d", w.Code)
	}
	mora(t, o.ispis(t, upravaSredstava(), put), "obrazac", "Posiljka="+stize[0].Slanje.VezaID+";", "Sektor=B;")
	if p, poruka := preusmjerenjeSredstava(t, o.zovi(t, upravaSredstava(), http.MethodGet, "/sredstva/na-putu/nema", nil), "error"); p != "/sredstva" || !strings.Contains(poruka, "više nema na putu") {
		t.Errorf("nepoznata pošiljka: %s %q", p, poruka)
	}

	for _, k := range []struct{ kolicina, poruka string }{{"tri", "nije broj"}, {"10", "Na putu je 5"}} {
		w := o.zovi(t, upravaSredstava(), http.MethodPost, put, url.Values{"kolicina": {k.kolicina}, "datum": {"2026-01-01"}})
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "VratiUpisano=true;") || !strings.Contains(w.Body.String(), k.poruka) {
			t.Errorf("zaprimanje %q: %d %s", k.kolicina, w.Code, w.Body.String())
		}
	}
	p, poruka := preusmjerenjeSredstava(t, o.zovi(t, upravaSredstava(), http.MethodPost, put, url.Values{"kolicina": {"3"}, "preuzeo": {"Ivo Ivić"}}), "success")
	if p != "/sredstva/skladista/"+darda.ID || !strings.Contains(poruka, "Na putu je ostalo 2.") {
		t.Errorf("djelomično zaprimanje: %s %q", p, poruka)
	}

	if w := o.zovi(t, upravaSredstava(), http.MethodPost, put+"/manjak", url.Values{"razlog": {" "}}); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "uz razlog") {
		t.Errorf("manjak bez razloga: %d %s", w.Code, w.Body.String())
	}
	if _, poruka := preusmjerenjeSredstava(t, o.zovi(t, upravaSredstava(), http.MethodPost, put+"/manjak", url.Values{"razlog": {"dvije lopate pale s kamiona"}}), "success"); !strings.Contains(poruka, "2 kom Lopata") {
		t.Errorf("otpis manjka: %q", poruka)
	}
	mora(t, o.ispis(t, promatracSredstava(), "/sredstva/skladista/"+darda.ID), "Darda poslije", "Stize=0;")
}

// Pošiljka na teren: ime mjesta i povratak na stranicu terena sektora
func TestPosiljkaNaTeren(t *testing.T) {
	p := PosiljkaNaPutu{Posiljka: models.Posiljka{
		Slanje: models.Promet{Kolicina: -400, SkladisteID: "osijek", SkladisteNaziv: "Centralno skladište Osijek", PremaAreaID: 34, PremaSectionCode: "B.34.1",
			PremaAreaName: "Drava i Dunav"},
		PremaSektor: "B",
	}}
	if p.Kamo() != "teren · BP 34 Drava i Dunav · B.34.1" || p.Povratak() != "/sredstva/na-terenu?sektor=B" || p.Odakle() != "Centralno skladište Osijek" || p.Poslano() != 400 {
		t.Errorf("pošiljka na teren: %q %q %q %v", p.Kamo(), p.Povratak(), p.Odakle(), p.Poslano())
	}
	p.Slanje.PremaSkladisteID, p.Slanje.PremaSkladisteNaziv = "sava", "Skladište Sava"
	if p.Kamo() != "Skladište Sava" || p.Povratak() != "/sredstva/skladista/sava" {
		t.Errorf("pošiljka u skladište: %q %q", p.Kamo(), p.Povratak())
	}
}

// Kad se roba na putu ne može pročitati, stranica skladišta to javlja
// greškom, a ne pokazuje skladište bez pošiljaka
func TestSkladisteBezKnjigePuta(t *testing.T) {
	o := novaOkolinaSredstava(t)
	if _, err := o.baza.Exec(`ALTER TABLE mts_promet RENAME COLUMN prema_mjesto TO prema_mjesto_staro`); err != nil {
		t.Fatal(err)
	}
	if w := o.zovi(t, promatracSredstava(), http.MethodGet, "/sredstva/skladista/"+o.sk.ID, nil); w.Code != http.StatusInternalServerError {
		t.Errorf("skladište bez knjige puta: %d", w.Code)
	}
	for _, put := range []string{"/sredstva?sektor=B", "/sredstva/gdje/lopata"} {
		if w := o.zovi(t, promatracSredstava(), http.MethodGet, put, nil); w.Code != http.StatusInternalServerError {
			t.Errorf("%s bez knjige puta: %d", put, w.Code)
		}
	}
}

// Na terenu: izdano a nezaprimljeno stoji u kartici „Na putu”, suženo na
// odabranu obranu; tek zaprimljeno se broji na terenu
func TestNaPutuNaTerenu(t *testing.T) {
	o := novaOkolinaSredstava(t)
	preusmjerenjeSredstava(t, o.zovi(t, upravaSredstava(), http.MethodPost, "/sredstva/skladista/"+o.sk.ID+"/promet",
		url.Values{"vrsta": {models.PrometIzdano}, "sredstvo": {"lopata"}, "kolicina": {"4"}, "podrucje": {"34"}, "dionica": {"B.34.1"}, "obrana": {o.otvoren.ID}}), "success")
	mora(t, o.ispis(t, promatracSredstava(), "/sredstva/na-terenu?sektor=B"), "sve obrane", "Stize=1;")
	mora(t, o.ispis(t, promatracSredstava(), "/sredstva/na-terenu?sektor=B&obrana="+o.otvoren.ID), "ta obrana", "Stize=1;")
	mora(t, o.ispis(t, promatracSredstava(), "/sredstva/na-terenu?sektor=B&obrana=druga"), "druga obrana", "Stize=0;")

	// teren zaprimi, pa vrati dvije lopate: povrat je na putu u skladište i ima potvrdu
	zaprimiSveRutom(t, o)
	preusmjerenjeSredstava(t, o.zovi(t, upravaSredstava(), http.MethodPost, "/sredstva/skladista/"+o.sk.ID+"/promet",
		url.Values{"vrsta": {models.PrometPovrat}, "sredstvo": {"lopata"}, "kolicina": {"2"}, "podrucje": {"34"}, "dionica": {"B.34.1"}, "obrana": {o.otvoren.ID}}), "success")
	povrat, err := o.svc.NaPutu(context.Background(), repository.FiltarPosiljki{PremaSkladisteID: o.sk.ID})
	if err != nil || len(povrat) != 1 {
		t.Fatalf("povrat na putu: %+v %v", povrat, err)
	}
	if w := o.zovi(t, upravaSredstava(), http.MethodGet, "/sredstva/promet/"+povrat[0].Slanje.VezaID+"/potvrda.xlsx", nil); w.Code != http.StatusOK {
		t.Errorf("potvrda povrata na putu: %d", w.Code)
	}
	if w := o.zovi(t, upravaSredstava(), http.MethodGet, "/sredstva/promet/"+povrat[0].Slanje.VezaID+"/potvrda.pdf", nil); w.Code != http.StatusOK {
		t.Errorf("PDF potvrde povrata na putu: %d", w.Code)
	}
	// obrana koje više nema: potvrda je ispisuje bez obrane
	if o := o.h.obranaZahvata(httptest.NewRequest(http.MethodGet, "/", nil), models.Promet{JournalID: "nema-je"}); o != "" {
		t.Errorf("nepostojeća obrana: %q", o)
	}

	if _, err := o.baza.Exec(`ALTER TABLE mts_promet RENAME COLUMN prema_mjesto TO prema_mjesto_staro`); err != nil {
		t.Fatal(err)
	}
	if w := o.zovi(t, promatracSredstava(), http.MethodGet, "/sredstva/na-terenu?sektor=B", nil); w.Code != http.StatusInternalServerError {
		t.Errorf("na terenu bez knjige puta: %d", w.Code)
	}
	if w := o.zovi(t, upravaSredstava(), http.MethodGet, "/sredstva/promet/"+povrat[0].Slanje.VezaID+"/potvrda.pdf", nil); w.Code != http.StatusInternalServerError {
		t.Errorf("PDF potvrde bez knjige puta: %d", w.Code)
	}
}

// Otpremnica robe na putu piše drugo mjesto s retka slanja, i prije
// zaprimanja; potez upisan odjednom kao i prije
func TestDrugoMjestoPotvrde(t *testing.T) {
	osijek := &models.Skladiste{ID: "osijek"}
	for _, k := range []struct {
		opis          string
		redci         []models.Promet
		oznaka, naziv string
	}{
		{"izdavanje na putu", []models.Promet{{Kolicina: -4, SkladisteID: "osijek", PremaAreaID: 34, PremaSectionCode: "B.34.1"}}, "Mjesto na terenu", "teren · BP 34 · B.34.1"},
		{"prijenos na putu", []models.Promet{{Kolicina: -4, SkladisteID: "osijek", PremaSkladisteID: "sava", PremaSkladisteNaziv: "Sava"}}, "U skladište", "Sava"},
		{"povrat na putu", []models.Promet{{Kolicina: -4, AreaID: 34, Mjesto: "kod ustave", PremaSkladisteID: "osijek"}}, "Mjesto na terenu", "teren · BP 34 · kod ustave"},
		{"izdavanje odjednom", []models.Promet{{Kolicina: -4, SkladisteID: "osijek"}, {Kolicina: 4, AreaID: 34}}, "Mjesto na terenu", "teren · BP 34"},
		{"prijenos odjednom", []models.Promet{{Kolicina: -4, SkladisteID: "osijek"}, {Kolicina: 4, SkladisteID: "sava", SkladisteNaziv: "Sava"}}, "U skladište", "Sava"},
		{"primka", []models.Promet{{Kolicina: 4, SkladisteID: "osijek"}}, "", ""},
	} {
		if oznaka, naziv := drugoMjestoPotvrde(k.redci, osijek); oznaka != k.oznaka || naziv != k.naziv {
			t.Errorf("%s: %q %q", k.opis, oznaka, naziv)
		}
	}
}

// zaprimiSveRutom zaprima sve što je na putu kroz obrazac, kao uprava sektora
func zaprimiSveRutom(t *testing.T, o *okolinaSredstava) {
	t.Helper()
	sve, err := o.svc.NaPutu(context.Background(), repository.FiltarPosiljki{})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range sve {
		preusmjerenjeSredstava(t, o.zovi(t, upravaSredstava(), http.MethodPost, "/sredstva/na-putu/"+p.Slanje.VezaID,
			url.Values{"kolicina": {kolicinaHR(p.NaPutu())}}), "success")
	}
}
