package web

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Čip filtra knjige mijenja samo svoj parametar, a ostali izbor ostaje;
// Excel ide po istim filtrima.
func TestFiltriKnjige(t *testing.T) {
	d := MtsPageData{Sektor: "B", Skladiste: &models.Skladiste{ID: "s1"}, VrstaID: "lopata", Od: "2025-12-01",
		Vrste: []models.VrstaSredstva{{ID: "lopata", Naziv: "Lopata"}}}
	if u := d.URLKnjige("vrsta", ""); u != "/sredstva/promet?od=2025-12-01&sektor=B&skladiste=s1" {
		t.Errorf("brisanje sredstva: %s", u)
	}
	if u := d.URLKnjige("skladiste", "s2"); u != "/sredstva/promet?od=2025-12-01&sektor=B&skladiste=s2&vrsta=lopata" {
		t.Errorf("drugo skladište: %s", u)
	}
	if u := d.ExcelKnjige(); u != "/sredstva/promet.xlsx?od=2025-12-01&sektor=B&skladiste=s1&vrsta=lopata" {
		t.Errorf("Excel: %s", u)
	}
	if n := d.NazivVrsteKnjige(); n != "Lopata" {
		t.Errorf("naziv sredstva: %s", n)
	}
	d.VrstaID = ""
	if n := d.NazivVrsteKnjige(); n != "sva" {
		t.Errorf("bez sredstva: %s", n)
	}
	if dd := d.DatumiKnjige(); len(dd) != 2 || dd[0].Vrijednost != "2025-12-01" || dd[1].Ime != "do" {
		t.Errorf("datumi: %+v", dd)
	}
}

// Redci istog upisa su jedan zahvat: mjesta i količine jedno ispod drugog,
// oblici „prazno → napunjeno”, otpis oštećenog ne mijenja vrstu zahvata, a
// zaprimanje istog poteza poslije je zaseban redak.
func TestSpojiZahvate(t *testing.T) {
	upis := time.Date(2026, 10, 24, 7, 15, 0, 0, time.UTC)
	kasnije := upis.Add(time.Hour)
	promet := []models.Promet{
		{ID: "z", VezaID: "v1", CreatedAt: kasnije, Vrsta: models.PrometIzdano, Kolicina: 3, Mjesto: "nasip"},
		{ID: "a", VezaID: "v2", CreatedAt: upis, Vrsta: models.PrometOtpis, Kolicina: -2, SkladisteID: "s1", SkladisteNaziv: "Bilje", Oblik: models.OblikPunjeno, Napomena: "oštećeno"},
		{ID: "b", VezaID: "v2", CreatedAt: upis, Vrsta: models.PrometPraznjenje, Kolicina: -8, SkladisteID: "s1", SkladisteNaziv: "Bilje", Oblik: models.OblikPunjeno},
		{ID: "c", VezaID: "v2", CreatedAt: upis, Vrsta: models.PrometPraznjenje, Kolicina: 8, SkladisteID: "s1", SkladisteNaziv: "Bilje", Oblik: models.OblikPrazno, Napomena: "oštećeno"},
		{ID: "d", VezaID: "v1", CreatedAt: upis, Vrsta: models.PrometIzdano, Kolicina: -3, SkladisteID: "s1", SkladisteNaziv: "Bilje"},
		{ID: "e", CreatedAt: upis, Vrsta: models.PrometPrimka, Kolicina: 5, SkladisteID: "s1", SkladisteNaziv: "Bilje"},
		{ID: "f", CreatedAt: upis, Vrsta: models.PrometPrimka, Kolicina: 6, SkladisteID: "s1", SkladisteNaziv: "Bilje"},
		{ID: "g", VezaID: "v3", CreatedAt: upis, Vrsta: models.PrometOtpis, Kolicina: -1, SkladisteID: "s1", SkladisteNaziv: "Bilje", Napomena: "manjak pri prijevozu"},
		{ID: "h", VezaID: "v3", CreatedAt: upis, Vrsta: models.PrometPrijenos, Kolicina: 1, SkladisteID: "s1", SkladisteNaziv: "Bilje"},
	}
	k := spojiZahvate(promet)
	if len(k) != 6 || k[5].Vrsta != models.PrometOtpis || len(k[5].Redci) != 2 {
		t.Errorf("manjak pri prijevozu ostaje otpis: %+v", k[len(k)-1])
	}
	if len(k) != 6 {
		t.Fatalf("zahvata %d: %+v", len(k), k)
	}
	if z := k[1]; z.Vrsta != models.PrometPraznjenje || len(z.Redci) != 3 || len(z.Mjesta) != 1 ||
		strings.Join(z.Oblici, ",") != "PUNJENO,PRAZNO" || strings.Join(z.Napomene, ",") != "oštećeno" {
		t.Errorf("pražnjenje s otpisom: %+v", z)
	}
	if z := k[2]; z.ID != "d" || len(z.Redci) != 1 {
		t.Errorf("izdavanje prije zaprimanja: %+v", z)
	}
	if k[0].ID != "z" || k[3].ID != "e" || k[4].ID != "f" {
		t.Errorf("zaprimanje i primke zasebno: %+v", k)
	}
	if spojiZahvate(nil) != nil {
		t.Error("prazna knjiga")
	}
}

// Filtar zahvata prima samo poznatu vrstu prometa; zaslon prikazuje korak po
// korak do granice, a „Prikaži još” vodi na prvi novi redak.
func TestListanjeKnjige(t *testing.T) {
	if zahvatKnjige(models.PrometPrimka) != models.PrometPrimka || zahvatKnjige("NESTO") != "" {
		t.Error("zahvat iz adrese")
	}
	for v, n := range map[string]int{"": korakKnjige, "-3": korakKnjige, "250": 250, "9000": najviseUKnjizi} {
		if g := zahvataNaZaslonu(v); g != n {
			t.Errorf("n=%q: %d, očekivano %d", v, g, n)
		}
	}
	d := MtsPageData{Sektor: "B", ZahvatKnjige: models.PrometPrimka, Knjiga: make([]RedakKnjige, 100), UkupnoZahvata: 147, ZahvataNaZaslonu: 100}
	if n := d.NazivZahvataKnjige(); n != "Primka" {
		t.Errorf("naziv zahvata: %s", n)
	}
	if !d.ImaJosUKnjizi() || d.JosKnjige() != "/sredstva/promet?n=200&sektor=B&zahvat=PRIMKA#zahvat-101" {
		t.Errorf("još: %v %s", d.ImaJosUKnjizi(), d.JosKnjige())
	}
	if d.ExcelKnjige() != "/sredstva/promet.xlsx?sektor=B&zahvat=PRIMKA" {
		t.Errorf("Excel bez broja na zaslonu: %s", d.ExcelKnjige())
	}
	d.ZahvatKnjige, d.ZahvataNaZaslonu = "", najviseUKnjizi
	if d.NazivZahvataKnjige() != "svi" || d.ImaJosUKnjizi() || d.NajviseUKnjizi() != 500 {
		t.Error("na granici nema još")
	}
}

// Excel knjige čita iste filtre kao stranica i opisuje ih u zaglavlju.
func TestFiltarIzvozaKnjige(t *testing.T) {
	fl, opis := filtarIzvozaKnjige(url.Values{"zahvat": {models.PrometPrimka}, "od": {"2026-01-02"}, "do": {"2026-03-04"}, "skladiste": {"s1"}}, "B")
	if fl.Sektor != "B" || fl.SkladisteID != "s1" || fl.Zahvat != models.PrometPrimka || fl.Od == nil || fl.Do == nil ||
		strings.Join(opis, "|") != "Primka|od 02.01.2026.|do 04.03.2026." {
		t.Errorf("filtar %+v, opis %v", fl, opis)
	}
	if fl, opis := filtarIzvozaKnjige(url.Values{"od": {"nije"}, "zahvat": {"X"}}, ""); fl.Od != nil || fl.Zahvat != "" || len(opis) != 0 {
		t.Errorf("krivi filtri: %+v %v", fl, opis)
	}
}
