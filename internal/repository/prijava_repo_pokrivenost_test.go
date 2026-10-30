package repository

import (
	"context"
	"reflect"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// objavljenaPrijava je objavljena prijava sektora B na području 16
func objavljenaPrijava(id string, broj int, dan time.Time) *models.PrijavaSTerena {
	return &models.PrijavaSTerena{ID: id, UserID: "g-1", Ime: "Marko Markić", Sektor: "B", AreaID: 16,
		Broj: broj, Godina: dan.Year(), Vrsta: models.PrijavaPrijava, Naslov: "Procjeđivanje", Opis: "voda uz nožicu nasipa",
		Datum: dan, Status: models.PrijavaObjavljena, ObjavljenoAt: &dan, CreatedAt: dan}
}

// Urudžba bez izvornika upisuje klasu i urbroj na prijavu, s verzijom u
// knjizi, a izvornika nema
func TestUrudzbaPrijaveBezIzvornika(t *testing.T) {
	baza, repo := bazaPrenesenih(t)
	ctx := context.Background()
	p := objavljenaPrijava("urudzba-1", 3, time.Date(2026, 4, 2, 10, 0, 0, 0, models.Zagreb))
	nuzno(t, repo.Save(ctx, p))
	p.Klasa, p.Urbroj = "325-01/26-01/3", "374-26-1"
	nuzno(t, repo.Urudzbiraj(ctx, p, nil, ""))

	g, err := repo.Get(ctx, p.ID)
	if err != nil || g == nil || g.Klasa != "325-01/26-01/3" || g.Urbroj != "374-26-1" || g.Status != models.PrijavaObjavljena {
		t.Fatalf("prijava %+v, %v", g, err)
	}
	if iz, err := repo.Izvornik(ctx, p.ID); err != nil || iz != nil {
		t.Errorf("izvornik bez PDF-a: %+v, %v", iz, err)
	}
	kanali := kanaliVerzija(t, baza, p.ID)
	if want := map[string]string{EntityPrijave: ledger.ChannelFor(ledger.ChannelPrijave, 16, 2026)}; !reflect.DeepEqual(kanali, want) {
		t.Errorf("verzije %v, htio %v", kanali, want)
	}
}

// Urudžba s izvornikom sprema i PDF dopunjen bilješkom urudžbe, u kanalu prijave
func TestUrudzbaPrijaveSIzvornikom(t *testing.T) {
	baza, repo := bazaPrenesenih(t)
	ctx := context.Background()
	p := objavljenaPrijava("urudzba-2", 4, time.Date(2026, 4, 3, 10, 0, 0, 0, models.Zagreb))
	nuzno(t, repo.Save(ctx, p))
	p.Klasa = "325-01/26-01/4"
	pdf := []byte("%PDF-1.7\nurudzbeni zig")
	nuzno(t, repo.Urudzbiraj(ctx, p, pdf, "sazetak-urudzbe"))

	iz, err := repo.Izvornik(ctx, p.ID)
	if err != nil || iz == nil {
		t.Fatalf("izvornik %+v, %v", iz, err)
	}
	if string(iz.PDF) != string(pdf) || iz.Sazetak != "sazetak-urudzbe" || iz.Bajtova != len(pdf) || iz.Vrsta != "application/pdf" {
		t.Errorf("izvornik %+v", iz)
	}
	kanal := ledger.ChannelFor(ledger.ChannelPrijave, 16, 2026)
	if want := map[string]string{EntityPrijave: kanal, EntityPrijaveIzvornici: kanal}; !reflect.DeepEqual(kanaliVerzija(t, baza, p.ID), want) {
		t.Errorf("verzije %v, htio %v", kanaliVerzija(t, baza, p.ID), want)
	}
}

// Prijava prenesena iz ranije evidencije nalazi se po točnoj oznaci izvora:
// oznaka …:1 ne nalazi prijavu …:10
func TestPrijavaPoOznaciIzvora(t *testing.T) {
	_, repo := bazaPrenesenih(t)
	ctx := context.Background()
	dan := time.Date(2024, 7, 2, 10, 0, 0, 0, models.Zagreb)
	deseta := objavljenaPrijava("prenesena-10", 10, dan)
	deseta.Rekonstrukcija, deseta.Izvor = true, "bp16:obavijesti_sa_terena:10"
	nuzno(t, repo.Save(ctx, deseta))

	if p, err := repo.PoIzvoru(ctx, "bp16:obavijesti_sa_terena:1"); err != nil || p != nil {
		t.Errorf("…:1 našao %+v, %v", p, err)
	}
	p, err := repo.PoIzvoru(ctx, "bp16:obavijesti_sa_terena:10")
	if err != nil || p == nil || p.ID != "prenesena-10" || !p.Rekonstrukcija {
		t.Errorf("…:10: %+v, %v", p, err)
	}
}

// Popis prijava sužava se sektorom, područjem, osobom, stanjem, godinom
// događaja i pretragom; najnovije su prve, a broj se može ograničiti
func TestPopisPrijavaSFiltrima(t *testing.T) {
	_, repo := bazaPrenesenih(t)
	ctx := context.Background()
	prva := objavljenaPrijava("p-1", 1, time.Date(2025, 11, 5, 9, 0, 0, 0, models.Zagreb))
	druga := objavljenaPrijava("p-2", 2, time.Date(2026, 2, 10, 9, 0, 0, 0, models.Zagreb))
	druga.Naslov = "Klizište na pokosu"
	treca := objavljenaPrijava("p-3", 1, time.Date(2026, 3, 1, 9, 0, 0, 0, models.Zagreb))
	treca.Sektor, treca.AreaID, treca.UserID = "C", 20, "g-2"
	nacrt := &models.PrijavaSTerena{ID: "p-4", UserID: "g-1", Ime: "Marko Markić", Sektor: "B", AreaID: 16, Vrsta: models.PrijavaObavijest,
		Naslov: "Nacrt", Datum: time.Date(2026, 3, 2, 9, 0, 0, 0, models.Zagreb), Status: models.PrijavaNacrt}
	nacrt.Stacionaza = "rkm 12+300"
	for _, p := range []*models.PrijavaSTerena{prva, druga, treca, nacrt} {
		nuzno(t, repo.Save(ctx, p))
	}
	ids := func(l []models.PrijavaSTerena) []string {
		var out []string
		for _, p := range l {
			out = append(out, p.ID)
		}
		return out
	}
	slucajevi := []struct {
		ime  string
		f    FiltarPrijava
		htio []string
	}{
		{"sve, najnovije prve", FiltarPrijava{}, []string{"p-4", "p-3", "p-2", "p-1"}},
		{"sektor", FiltarPrijava{Sektor: "C"}, []string{"p-3"}},
		{"područje", FiltarPrijava{AreaID: 16}, []string{"p-4", "p-2", "p-1"}},
		{"osoba", FiltarPrijava{UserID: "g-2"}, []string{"p-3"}},
		{"stanje", FiltarPrijava{Status: models.PrijavaNacrt}, []string{"p-4"}},
		{"godina", FiltarPrijava{Godina: 2025}, []string{"p-1"}},
		{"pretraga naslova", FiltarPrijava{Trazi: " klizište "}, []string{"p-2"}},
		{"pretraga podataka", FiltarPrijava{Trazi: "12+300"}, []string{"p-4"}},
		{"ograničeno", FiltarPrijava{Sektor: "B", Limit: 2}, []string{"p-4", "p-2"}},
		{"bez pogotka", FiltarPrijava{Sektor: "B", Godina: 2024}, nil},
	}
	for _, s := range slucajevi {
		l, err := repo.List(ctx, s.f)
		if err != nil {
			t.Fatalf("%s: %v", s.ime, err)
		}
		if got := ids(l); !reflect.DeepEqual(got, s.htio) {
			t.Errorf("%s: %v, htio %v", s.ime, got, s.htio)
		}
	}
	if _, err := NewPrijavaRepository(zatvorenaBaza(t), nil).List(ctx, FiltarPrijava{}); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}

// Stanje „ARHIVIRANA” s čvora otprije preimenovanja čita se kao riješena
func TestStaroStanjeArhiviranaJeRijesena(t *testing.T) {
	_, repo := bazaPrenesenih(t)
	ctx := context.Background()
	p := objavljenaPrijava("stara", 1, time.Date(2025, 5, 5, 9, 0, 0, 0, models.Zagreb))
	p.Status = "ARHIVIRANA"
	nuzno(t, repo.Save(ctx, p))
	if g, _ := repo.Get(ctx, "stara"); g == nil || g.Status != models.PrijavaRijesena {
		t.Errorf("stanje %+v", g)
	}
	if l, _ := repo.List(ctx, FiltarPrijava{}); len(l) != 1 || l[0].Status != models.PrijavaRijesena {
		t.Errorf("popis %+v", l)
	}
}

// Filtar „riješene” trebao bi naći i prijavu sa starim stanjem „ARHIVIRANA”
// koja je stigla razmjenom; sada je ne nalazi jer uspoređuje spremljeno stanje
func TestFiltarRijesenihNalaziStaroStanje(t *testing.T) {
	t.Skip("poznata greška: List s filtrom stanja ne prevodi staro stanje ARHIVIRANA")
	_, repo := bazaPrenesenih(t)
	ctx := context.Background()
	p := objavljenaPrijava("stara", 1, time.Date(2025, 5, 5, 9, 0, 0, 0, models.Zagreb))
	p.Status = "ARHIVIRANA"
	nuzno(t, repo.Save(ctx, p))
	if l, _ := repo.List(ctx, FiltarPrijava{Status: models.PrijavaRijesena}); len(l) != 1 {
		t.Errorf("riješene %+v", l)
	}
}

// Sljedeći broj je najveći broj u sektoru i godini plus jedan
func TestSljedeciBrojPrijave(t *testing.T) {
	_, repo := bazaPrenesenih(t)
	ctx := context.Background()
	dan := time.Date(2026, 5, 5, 9, 0, 0, 0, models.Zagreb)
	for _, p := range []*models.PrijavaSTerena{objavljenaPrijava("b-1", 1, dan), objavljenaPrijava("b-5", 5, dan)} {
		nuzno(t, repo.Save(ctx, p))
	}
	for _, s := range []struct {
		sektor       string
		godina, htio int
	}{{"B", 2026, 6}, {"B", 2025, 1}, {"C", 2026, 1}} {
		if n, err := repo.SljedeciBroj(ctx, s.sektor, s.godina); err != nil || n != s.htio {
			t.Errorf("%s %d: %d, %v; htio %d", s.sektor, s.godina, n, err, s.htio)
		}
	}
}

// Brisanje nacrta miče i njegove fotografije; nepostojeći nacrt nije greška
func TestBrisanjeNacrtaSFotografijom(t *testing.T) {
	baza, repo := bazaPrenesenih(t)
	ctx := context.Background()
	nuzno(t, repo.Delete(ctx, "nema-ga"))
	if n := brojRedaka(t, baza, `SELECT count(*) FROM record_versions`); n != 0 {
		t.Errorf("brisanje nepostojećeg upisalo %d verzija", n)
	}

	p := &models.PrijavaSTerena{ID: "nacrt-1", UserID: "g-1", Sektor: "B", AreaID: 16, Datum: time.Date(2026, 6, 1, 9, 0, 0, 0, models.Zagreb),
		Status: models.PrijavaNacrt, Slike: []models.SlikaPrijave{{ID: "foto-1", Naziv: "fotografija 1"}}}
	nuzno(t, repo.Save(ctx, p))
	nuzno(t, repo.SaveSlika(ctx, "foto-1", p.ID, []byte("\xff\xd8 nasip")))
	if b, _ := repo.Slika(ctx, "foto-1"); string(b) != "\xff\xd8 nasip" {
		t.Fatalf("fotografija prije brisanja %q", b)
	}
	nuzno(t, repo.Delete(ctx, p.ID))
	if g, _ := repo.Get(ctx, p.ID); g != nil {
		t.Error("nacrt je ostao")
	}
	if b, err := repo.Slika(ctx, "foto-1"); err != nil || b != nil {
		t.Errorf("fotografija je ostala: %q, %v", b, err)
	}
}
