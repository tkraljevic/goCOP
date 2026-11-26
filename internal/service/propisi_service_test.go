package service

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Testovi propisa rade samo na izmišljenim, kratkim primjerima („Zakon o
// čuvanju nasipa”, „Plan obrane Primjerova”); tekst pravih propisa ne ulazi
// u repozitorij (Odluke (25. 11. 2026.)).

func servisPropisa(t *testing.T) (*PropisiService, *repository.PropisiRepository, *sql.DB) {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "propisi.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	repo := repository.NewPropisiRepository(baza, ledger.New(baza, "ured"))
	return NewPropisiService(repo, nil), repo, baza
}

func stavak(tekst string) []models.StavakPropisa {
	return []models.StavakPropisa{{Tekst: tekst}}
}

var (
	nnPlana    = models.IzdanjeNN{Godina: 2026, Broj: 5, Akt: 101}
	nnZakona   = models.IzdanjeNN{Godina: 2026, Broj: 12, Akt: 201}
	nnIzmjene  = models.IzdanjeNN{Godina: 2026, Broj: 47, Akt: 808}
	nnDruge    = models.IzdanjeNN{Godina: 2027, Broj: 3, Akt: 50}
	nnNeucitan = models.IzdanjeNN{Godina: 2027, Broj: 9, Akt: 77}
)

// planObrane je izmišljen plan s dva dijela i rimskim točkama
func planObrane() *models.Propis {
	return &models.Propis{
		ID: nnPlana.ID(), Adresa: "plan-obrane-primjerova", Naziv: "Plan obrane Primjerova", Skupina: "O obrani od poplava",
		Donositelj: "Vlada Primjerova", NN: nnPlana, DatumObjave: "2026-02-01", Izvor: models.IzvorPropisaNN,
		Dijelovi: []models.DioPropisa{
			{Oznaka: "uvod", Vrsta: models.DioUvod, Stavci: stavak("Na temelju izmišljenog ovlaštenja donosi se")},
			{Oznaka: "dio-1", Vrsta: models.DioPlana, Naslov: "UVODNE ODREDBE"},
			{Oznaka: "i", Vrsta: models.DioTocka, Broj: "I.", Roditelj: "dio-1", Stavci: stavak("Planom se uređuje obrana Primjerova.")},
			{Oznaka: "ii", Vrsta: models.DioTocka, Broj: "II.", Roditelj: "dio-1", Stavci: []models.StavakPropisa{
				{Tekst: "Pripremno stanje proglašava se kad voda poraste."},
				{Tekst: "U pripremnom stanju dežura se u centru."},
				{Tekst: "Pripremno stanje traje do opoziva."},
			}},
			{Oznaka: "dio-2", Vrsta: models.DioPlana, Naslov: "STADIJI OBRANE"},
			{Oznaka: "xxiii", Vrsta: models.DioTocka, Broj: "XXIII.", Roditelj: "dio-2", Stavci: stavak("Redovna obrana slijedi pripremno stanje.")},
			{Oznaka: "zavrsetak", Vrsta: models.DioZavrsetak, Stavci: stavak("Klasa: izmišljena")},
		},
	}
}

// zakonNasipa je izmišljen zakon s dvije glave, odjeljkom i člankom 119.
func zakonNasipa() *models.Propis {
	return &models.Propis{
		ID: nnZakona.ID(), Adresa: "zakon-o-cuvanju-nasipa", Naziv: "Zakon o čuvanju nasipa", Skupina: "O obrani od poplava",
		Donositelj: "Hrvatski sabor", NN: nnZakona, DatumObjave: "2026-03-02", Izvor: models.IzvorPropisaNN,
		Veze: []models.VezaELI{{Vrsta: "amended_by", ELI: nnIzmjene.PutELI()}, {Vrsta: "changed_by", ELI: nnNeucitan.PutELI()},
			{Vrsta: "amended_by", ELI: nnNeucitan.PutELI()}, {Vrsta: "based_on", ELI: "/eli/sluzbeni/2001/1/1"}},
		Dijelovi: []models.DioPropisa{
			{Oznaka: "glava-i", Vrsta: models.DioGlava, Broj: "I.", Naslov: "OPĆE ODREDBE"},
			{Oznaka: "clanak-1", Vrsta: models.DioClanak, Broj: "1.", Naslov: "Predmet", Roditelj: "glava-i", Stavci: stavak("Zakonom se uređuje čuvanje nasipa.")},
			{Oznaka: "clanak-2", Vrsta: models.DioClanak, Broj: "2.", Roditelj: "glava-i", Stavci: stavak("Nasip čuva vodočuvar.")},
			{Oznaka: "glava-ii", Vrsta: models.DioGlava, Broj: "II.", Naslov: "ČUVANJE"},
			{Oznaka: "glava-ii-1", Vrsta: models.DioOdjeljak, Broj: "1.", Naslov: "Nadzor", Roditelj: "glava-ii"},
			{Oznaka: "clanak-3", Vrsta: models.DioClanak, Broj: "3.", Roditelj: "glava-ii-1", Stavci: stavak("Nadzor nad nasipom vodi rukovoditelj.")},
			{Oznaka: "clanak-4", Vrsta: models.DioClanak, Broj: "4.", Roditelj: "glava-ii-1", Stavci: stavak("Ophodnja u vrijeme obrane ide jednom dnevno.")},
			{Oznaka: "clanak-119", Vrsta: models.DioClanak, Broj: "119.", Roditelj: "glava-ii", Stavci: stavak("Kazna za oštećenje nasipa.")},
		},
	}
}

// izmjenaNasipa je izmišljena izmjena sa svim vrstama oznaka
func izmjenaNasipa(p *models.Propis) *models.IzmjenaPropisa {
	return &models.IzmjenaPropisa{
		ID: p.ID + "-izmjena-" + nnIzmjene.ID(), PropisID: p.ID, Naziv: "Zakon o izmjeni Zakona o čuvanju nasipa", NN: nnIzmjene,
		DatumObjave: "2026-06-01", StupaNaSnagu: "2026-06-09", StupanjeIzraz: "osmoga dana od dana objave", Izvor: models.IzvorPropisaNN,
		Dijelovi: []models.DioPropisa{
			{Oznaka: "clanak-1", Vrsta: models.DioClanak, Broj: "1.", Stavci: stavak("U članku 2. riječ »nasip« zamjenjuje se riječima »obrambeni nasip«.")},
			{Oznaka: "clanak-2", Vrsta: models.DioClanak, Broj: "2.", Stavci: stavak("Članak 3. briše se.")},
			{Oznaka: "clanak-3", Vrsta: models.DioClanak, Broj: "3.", Stavci: []models.StavakPropisa{
				{Tekst: "Iza članka 4. dodaju se članci 4.a do 4.c koji glase:"}, {Tekst: "»Članak 4.a", Navod: true}}},
		},
		Upute: []models.UputaIzmjene{
			{Redni: 1, IzClanka: "clanak-1", Vrsta: models.UputaZamjenaRijeci, Cilj: models.CiljUpute{Clanci: []string{"clanak-2"}},
				Stari: "nasip", Novi: "obrambeni nasip", Tekst: "U članku 2. riječ »nasip« zamjenjuje se riječima »obrambeni nasip«."},
			{Redni: 2, IzClanka: "clanak-2", Vrsta: models.UputaZamjenaBroja, Cilj: models.CiljUpute{Clanci: []string{"clanak-2"}},
				Tekst: "U članku 2. broj »5« zamjenjuje se brojem »6«.", Blago: true},
			{Redni: 3, IzClanka: "clanak-2", Vrsta: models.UputaBrisanjeDijela, Cilj: models.CiljUpute{Clanci: []string{"clanak-3"}}, Tekst: "Članak 3. briše se."},
			{Redni: 4, IzClanka: "clanak-2", Vrsta: models.UputaBrisanjeDijela, Cilj: models.CiljUpute{Clanci: []string{"clanak-1"}, Stavci: []string{"2"}}, Tekst: "U članku 1. stavak 2. briše se."},
			{Redni: 5, IzClanka: "clanak-3", Vrsta: models.UputaDodavanjeDijela, Cilj: models.CiljUpute{Clanci: []string{"clanak-4"}}, Iza: "iza članka 4.",
				NoveOznake: []string{"4.a", "4.b", "4.c"}, Blok: []string{"»Članak 4.a"}, Tekst: "Iza članka 4. dodaju se članci 4.a do 4.c koji glase:"},
			{Redni: 6, IzClanka: "clanak-3", Vrsta: models.UputaCijeliTekst, Tekst: "U cijelom tekstu riječ »vodočuvar« zamjenjuje se riječju »čuvar«."},
			{Redni: 7, IzClanka: "clanak-3", Vrsta: models.UputaNeprepoznata, Tekst: "Nešto nejasno."},
			{Redni: 8, IzClanka: "clanak-3", Vrsta: models.UputaNeprepoznata, Rucno: true, Tekst: "Ne mijenja članke, rekao je administrator."},
			{Redni: 9, IzClanka: "clanak-3", Vrsta: models.UputaVlastita, Tekst: "Postupci započeti nastavljaju se."},
			{Redni: 10, IzClanka: "clanak-3", Vrsta: models.UputaStupanje, Tekst: "Ovaj Zakon stupa na snagu osmoga dana."},
			{Redni: 11, IzClanka: "clanak-9", Vrsta: models.UputaNoviTekst, Cilj: models.CiljUpute{Clanci: []string{"clanak-300"}}, Tekst: "Članak 300. mijenja se i glasi:", Upozorenje: "Članka 300. nema u propisu."},
		},
	}
}

// drugaIzmjena je kasnija izmjena istog članka
func drugaIzmjena(p *models.Propis) *models.IzmjenaPropisa {
	return &models.IzmjenaPropisa{
		ID: p.ID + "-izmjena-" + nnDruge.ID(), PropisID: p.ID, Naziv: "Zakon o drugoj izmjeni", NN: nnDruge, DatumObjave: "2027-01-10",
		Dijelovi: []models.DioPropisa{{Oznaka: "clanak-1", Vrsta: models.DioClanak, Broj: "1.", Stavci: stavak("U članku 2. riječ »čuva« zamjenjuje se riječju »štiti«.")}},
		Upute:    []models.UputaIzmjene{{Redni: 1, IzClanka: "clanak-1", Vrsta: models.UputaZamjenaRijeci, Cilj: models.CiljUpute{Clanci: []string{"clanak-2"}}, Tekst: "U članku 2. riječ »čuva« zamjenjuje se riječju »štiti«."}},
	}
}

func spremiPropise(t *testing.T, repo *repository.PropisiRepository, pp ...*models.Propis) {
	t.Helper()
	for _, p := range pp {
		if err := repo.Spremi(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}
}

func spremiIzmjene(t *testing.T, repo *repository.PropisiRepository, izz ...*models.IzmjenaPropisa) {
	t.Helper()
	for _, iz := range izz {
		if err := repo.SpremiIzmjenu(context.Background(), iz); err != nil {
			t.Fatal(err)
		}
	}
}

// Popis ide po skupinama redom iz plana: tri stalne skupine, upisane po
// abecedi i Ostali na kraju, a unutar skupine po hrvatskoj abecedi (Č iza
// C, a ne iza Z). Prazna skupina se ne vidi (Odluke (25. 11. 2026.)).
func TestPopisPoSkupinamaRedom(t *testing.T) {
	s, repo, _ := servisPropisa(t)
	ctx := context.Background()
	if sk, err := s.Popis(ctx); err != nil || len(sk) != 0 {
		t.Fatalf("prazan popis: %v %v", sk, err)
	}
	broj := 0
	propis := func(naziv, skupina string) *models.Propis {
		broj++
		nn := models.IzdanjeNN{Godina: 2026, Broj: broj, Akt: broj}
		return &models.Propis{ID: nn.ID(), Adresa: "propis-" + nn.ID(), Naziv: naziv, Skupina: skupina, NN: nn,
			Dijelovi: []models.DioPropisa{{Oznaka: "clanak-1", Vrsta: models.DioClanak, Broj: "1.", Stavci: stavak("Izmišljen tekst.")}}}
	}
	spremiPropise(t, repo,
		propis("Pravilnik o čamcima", ""),
		propis("Zakon o čuvanju nasipa", "O obrani od poplava"),
		propis("Čuvarski red Primjerova", "O obrani od poplava"),
		propis("Pravilnik o kanalima", "Kanali"),
		propis("Pravilnik o veslima", "Čamci"),
		propis("Pravilnik o kacigama", "Zaštita na radu"),
		propis("Pravilnik o sidrima", "  Ostali "),
	)
	sk, err := s.Popis(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var redom []string
	for _, g := range sk {
		redom = append(redom, g.Naziv)
	}
	if want := []string{"O obrani od poplava", "Zaštita na radu", "Čamci", "Kanali", "Ostali"}; !reflect.DeepEqual(redom, want) {
		t.Fatalf("skupine %q, a treba %q", redom, want)
	}
	if prvi := sk[0].Propisi; len(prvi) != 2 || prvi[0].Naziv != "Čuvarski red Primjerova" || prvi[1].Naziv != "Zakon o čuvanju nasipa" {
		t.Errorf("redom unutar skupine: %+v", prvi)
	}
	if sk[0].Opis != "Propisi po kojima se vodi obrana od poplava." || sk[2].Opis != "" || sk[4].Opis == "" {
		t.Errorf("opisi: %q %q %q", sk[0].Opis, sk[2].Opis, sk[4].Opis)
	}
	if len(sk[4].Propisi) != 2 {
		t.Errorf("Ostali: bez skupine i „Ostali” s razmacima su ista skupina: %+v", sk[4].Propisi)
	}
}

// Broj točke ili članka vodi ravno na dio, a riječ se traži i u drugom
// padežu, u svim propisima i u tekstu izmjena; izbor propisa iznad
// rezultata broji pogotke svakog propisa (Odluke (25. 11. 2026.), O8).
func TestPretragaIzravnoNaTockuIClanak(t *testing.T) {
	s, repo, _ := servisPropisa(t)
	ctx := context.Background()
	plan, zakon := planObrane(), zakonNasipa()
	spremiPropise(t, repo, plan, zakon)
	spremiIzmjene(t, repo, izmjenaNasipa(zakon))

	for upit, put := range map[string]string{
		"XXIII":       "/propisi/plan-obrane-primjerova/xxiii",
		"točka 23":    "/propisi/plan-obrane-primjerova/xxiii",
		"članak 119.": "/propisi/zakon-o-cuvanju-nasipa/clanak-119",
		"119":         "/propisi/zakon-o-cuvanju-nasipa/clanak-119",
	} {
		rez, err := s.Trazi(ctx, upit, "")
		if err != nil || rez.Izravno == nil || rez.Izravno.Adresa != put {
			t.Errorf("%q: %+v %v", upit, rez, err)
		}
	}
	rez, err := s.Trazi(ctx, "XXIII", "zakon-o-cuvanju-nasipa")
	if err != nil || rez.Izravno != nil || len(rez.PoPropisu) != 0 || rez.Sazetak != "" {
		t.Errorf("točka iz plana unutar zakona: %+v %v", rez, err)
	}

	rez, err = s.Trazi(ctx, "  pripremnog stanja ", "")
	if err != nil || rez.Izravno != nil || rez.Upit != "pripremnog stanja" || len(rez.PoPropisu) != 1 || rez.Ukupno != 2 {
		t.Fatalf("padeži: %+v %v", rez, err)
	}
	g := rez.PoPropisu[0]
	if g.Adresa != "plan-obrane-primjerova" || g.Naziv != "Plan obrane Primjerova" || g.Pogoci[0].Put != "Točka II. · UVODNE ODREDBE" ||
		g.Pogoci[0].Adresa != "/propisi/plan-obrane-primjerova/ii" || g.Pogoci[1].Put != "Točka XXIII. · STADIJI OBRANE" {
		t.Errorf("pogoci: %+v", g)
	}
	if g.Pogoci[0].JosStavaka != 2 || g.Pogoci[0].Jos != "Još 2 stavka u ovoj točki." || g.Pogoci[1].Jos != "" {
		t.Errorf("ostali stavci: %+v", g.Pogoci)
	}
	if !istaknuto(g.Pogoci[0], "Pripremno") {
		t.Errorf("isječak ne ističe pogodak: %+v", g.Pogoci[0].Isjecak)
	}
	if rez.Sazetak != "Pronađeno: 2 točke u propisu „Plan obrane Primjerova”." || rez.Cipovi != nil {
		t.Errorf("sažetak: %q, izbor %+v", rez.Sazetak, rez.Cipovi)
	}

	// „nasip” je u zakonu i u izmjeni zakona; uvod ni naslov nisu pogodak
	rez, err = s.Trazi(ctx, "nasipa", "")
	if err != nil || len(rez.PoPropisu) != 1 {
		t.Fatalf("nasip: %+v %v", rez, err)
	}
	pogoci := rez.PoPropisu[0].Pogoci
	zadnji := pogoci[len(pogoci)-1]
	if zadnji.Put != "U izmjeni NN 47/26 · članak 1." || zadnji.IzIzmjene != "NN 47/26" || zadnji.Adresa != "/propisi/zakon-o-cuvanju-nasipa/izmjene/nn-2026-47-808" {
		t.Errorf("pogodak u izmjeni: %+v", zadnji)
	}
	if pogoci[0].Put != "Članak 1. · OPĆE ODREDBE" || !strings.HasPrefix(rez.Sazetak, "Pronađeno: ") || !strings.Contains(rez.Sazetak, "5 članaka") {
		t.Errorf("put %q, sažetak %q", pogoci[0].Put, rez.Sazetak)
	}

	// Riječ iz oba propisa: izbor po propisu, a s adresom samo njegovi pogoci
	rez, err = s.Trazi(ctx, "obrana", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rez.Cipovi) != 3 || rez.Cipovi[0].Naziv != "Svi propisi" || rez.Cipovi[0].Broj != rez.Ukupno || !rez.Cipovi[0].Odabran ||
		!strings.Contains(rez.Cipovi[1].Href, "q=obrana&propis=") || !strings.Contains(rez.Sazetak, " i ") {
		t.Errorf("izbor propisa: %+v, %q", rez.Cipovi, rez.Sazetak)
	}
	samoZakon, err := s.Trazi(ctx, "obrana", "zakon-o-cuvanju-nasipa")
	if err != nil || len(samoZakon.PoPropisu) != 1 || samoZakon.PoPropisu[0].Adresa != "zakon-o-cuvanju-nasipa" ||
		samoZakon.Cipovi[0].Odabran || !samoZakon.Cipovi[2].Odabran && !samoZakon.Cipovi[1].Odabran {
		t.Errorf("samo zakon: %+v %v", samoZakon, err)
	}

	if rez, err := s.Trazi(ctx, "ab", ""); err != nil || !rez.Prekratko {
		t.Errorf("prekratko: %+v %v", rez, err)
	}
	if rez, err := s.Trazi(ctx, "kormilo", ""); err != nil || rez.PoPropisu != nil || rez.Sazetak != "" {
		t.Errorf("bez pogotka: %+v %v", rez, err)
	}
	if _, err := s.Trazi(ctx, "nasip", "nema-ga"); !errors.Is(err, ErrNemaPropisa) {
		t.Errorf("nepoznat propis: %v", err)
	}
}

func istaknuto(pg Pogodak, rijec string) bool {
	for _, k := range pg.Isjecak {
		if k.Istaknuto && strings.HasPrefix(k.Tekst, rijec) {
			return true
		}
	}
	return false
}

// Stalna adresa članka pokazuje put od glave, susjede istog reda i cjelinu
// u kojoj je; glava pokazuje sve pod sobom, a točka Plana kao prethodni dio
// ima uvod (Odluke (25. 11. 2026.)).
func TestDioSPutemPrethodnimISljedecim(t *testing.T) {
	s, repo, _ := servisPropisa(t)
	ctx := context.Background()
	spremiPropise(t, repo, planObrane(), zakonNasipa())

	d, err := s.Dio(ctx, "zakon-o-cuvanju-nasipa", "clanak-3")
	if err != nil {
		t.Fatal(err)
	}
	if d.Cijeli || len(d.Put) != 2 || d.Put[0].Oznaka != "glava-ii" || d.Put[1].Oznaka != "glava-ii-1" ||
		d.Prethodni.Oznaka != "clanak-2" || d.Sljedeci.Oznaka != "clanak-4" || d.Nadredeni().Oznaka != "glava-ii-1" || d.Vrh() != "glava-ii" {
		t.Errorf("članak 3.: %+v", d)
	}
	if d.Dio.Poveznica != "/propisi/zakon-o-cuvanju-nasipa/clanak-3" || d.Dio.OpisPoveznice != "Stalna adresa članka 3." {
		t.Errorf("stalna adresa: %q %q", d.Dio.Poveznica, d.Dio.OpisPoveznice)
	}

	g, err := s.Dio(ctx, "nn-2026-12-201", "glava-ii")
	if err != nil {
		t.Fatal(err)
	}
	var djeca []string
	for _, x := range g.Djeca {
		djeca = append(djeca, x.Oznaka)
	}
	if !g.Cijeli || !reflect.DeepEqual(djeca, []string{"glava-ii-1", "clanak-3", "clanak-4", "clanak-119"}) ||
		g.Prethodni.Oznaka != "glava-i" || g.Sljedeci != nil || g.Nadredeni() != nil || g.Vrh() != "glava-ii" ||
		g.Dio.Raspon != "članci 3.–119." || g.Dio.OpisPoveznice != "Stalna adresa glave II." {
		t.Errorf("glava II.: %+v, djeca %q", g, djeca)
	}

	tocka, err := s.Dio(ctx, "plan-obrane-primjerova", "i")
	if err != nil || tocka.Prethodni.Oznaka != "uvod" || tocka.Sljedeci.Oznaka != "ii" {
		t.Errorf("točka I.: %+v %v", tocka, err)
	}
	if _, err := s.Dio(ctx, "plan-obrane-primjerova", "clanak-9"); !errors.Is(err, ErrNemaDijela) {
		t.Errorf("nepostojeći dio: %v", err)
	}
	if _, err := s.Dio(ctx, "nema-ga", "i"); !errors.Is(err, ErrNemaPropisa) {
		t.Errorf("nepostojeći propis: %v", err)
	}

	st, err := s.Propis(ctx, "plan-obrane-primjerova")
	if err != nil {
		t.Fatal(err)
	}
	var sadrzaj []string
	for _, x := range st.Sadrzaj {
		sadrzaj = append(sadrzaj, x.Oznaka+" "+x.Raspon)
	}
	if st.SviClanci || !reflect.DeepEqual(sadrzaj, []string{"uvod ", "dio-1 točke I.–II.", "dio-2 točka XXIII.", "zavrsetak "}) ||
		st.Opseg.Opis() != "2 dijela, 3 točke" || st.PoveznicaNN != "https://narodne-novine.nn.hr/eli/sluzbeni/2026/5/101" {
		t.Errorf("naslovnica plana: %q %+v", sadrzaj, st)
	}
}

// Akt bez glava, dijelova i priloga na naslovnici pokazuje sve članke, a
// kružni roditelji iz zapisa s drugog računala ne zapetljaju stranicu
func TestNaslovnicaBezCjelinaIKruzniRoditelji(t *testing.T) {
	s, repo, _ := servisPropisa(t)
	nn := models.IzdanjeNN{Godina: 2026, Broj: 30, Akt: 3}
	spremiPropise(t, repo, &models.Propis{ID: nn.ID(), Adresa: "pravilnik-o-ophodnji", Naziv: "Pravilnik o ophodnji", NN: nn,
		Dijelovi: []models.DioPropisa{
			{Oznaka: "clanak-1", Vrsta: models.DioClanak, Broj: "1.", Stavci: stavak("Ophodnja ide nasipom.")},
			{Oznaka: "odjeljak-1", Vrsta: models.DioOdjeljak, Broj: "1.", Roditelj: "odjeljak-2"},
			{Oznaka: "odjeljak-2", Vrsta: models.DioOdjeljak, Broj: "2.", Roditelj: "odjeljak-1"},
			{Oznaka: "clanak-2", Vrsta: models.DioClanak, Broj: "2.", Roditelj: "odjeljak-1", Stavci: stavak("Ophodnja traje sat.")},
		}})
	st, err := s.Propis(context.Background(), "pravilnik-o-ophodnji")
	if err != nil || !st.SviClanci || len(st.Dijelovi) != 4 || len(st.Sadrzaj) != 1 {
		t.Fatalf("%+v %v", st, err)
	}
	d, err := s.Dio(context.Background(), "pravilnik-o-ophodnji", "clanak-2")
	if err != nil || len(d.Put) != najdubljeStablo {
		t.Errorf("put kroz kružne roditelje: %d %v", len(d.Put), err)
	}
}

// Oznake uz članke računaju se iz uputa, redom po izdanju: brisan, iza
// njega dodani, mijenjan, a dvije odredbe iste izmjene uz isti članak su
// jedna oznaka. Zamjena u cijelom tekstu je traka na vrhu, a neprepoznata
// odredba se broji (Odluke (25. 11. 2026.), O10).
func TestOznakeMijenjanBrisanDodanoICijeliTekst(t *testing.T) {
	s, repo, _ := servisPropisa(t)
	ctx := context.Background()
	zakon := zakonNasipa()
	spremiPropise(t, repo, zakon)
	spremiIzmjene(t, repo, drugaIzmjena(zakon), izmjenaNasipa(zakon))

	st, err := s.Propis(ctx, "zakon-o-cuvanju-nasipa")
	if err != nil {
		t.Fatal(err)
	}
	natpisi := func(oznaka string) []string {
		var out []string
		for _, o := range st.Oznake[oznaka] {
			out = append(out, o.Natpis)
		}
		return out
	}
	if got := natpisi("clanak-2"); !reflect.DeepEqual(got, []string{"mijenjan NN 47/26", "mijenjan NN 3/27"}) {
		t.Errorf("članak 2.: %q", got)
	}
	if got := natpisi("clanak-3"); !reflect.DeepEqual(got, []string{"brisan NN 47/26"}) {
		t.Errorf("članak 3.: %q", got)
	}
	if got := natpisi("clanak-1"); !reflect.DeepEqual(got, []string{"mijenjan NN 47/26"}) {
		t.Errorf("brisan stavak mijenja članak: %q", got)
	}
	if got := natpisi("clanak-4"); !reflect.DeepEqual(got, []string{"iza njega dodani članci 4.a–4.c · NN 47/26"}) {
		t.Errorf("članak 4.: %q", got)
	}
	prva := st.Oznake["clanak-2"][0]
	if prva.IzClanka != "članak 1., članak 2." || len(prva.Odredbe) != 2 || prva.UzDio != "članak 2." ||
		prva.Poveznica != "/propisi/zakon-o-cuvanju-nasipa/izmjene/nn-2026-47-808" || prva.PoveznicaNN != "https://narodne-novine.nn.hr/eli/sluzbeni/2026/47/808" {
		t.Errorf("spojene odredbe: %+v", prva)
	}
	if dodano := st.Oznake["clanak-4"][0]; len(dodano.Odredbe[0].Blok) != 1 || dodano.Vrsta != OznakaDodano {
		t.Errorf("blok uz dodani članak: %+v", dodano)
	}
	if nema := st.Oznake["clanak-300"]; len(nema) != 1 || nema[0].UzDio != "clanak-300" {
		t.Errorf("članak kojeg nema: %+v", nema)
	}
	if len(st.CijeliTekst) != 1 || st.CijeliTekst[0].IzClanka != "članak 3." || st.CijeliTekst[0].Vrsta != OznakaCijeliTekst || st.Nepovezane != 1 {
		t.Errorf("cijeli tekst %+v, nepovezane %d", st.CijeliTekst, st.Nepovezane)
	}
	if len(st.Neucitane) != 1 || st.Neucitane[0] != nnNeucitan {
		t.Errorf("neučitane izmjene: %+v", st.Neucitane)
	}
	if len(st.PopisIzmjena) != 2 || st.PopisIzmjena[0].Stupila != "9. 6. 2026." || st.PopisIzmjena[1].Stupila != "" {
		t.Errorf("popis izmjena: %+v", st.PopisIzmjena)
	}

	// Dodana točka Plana i dvije dodane točke istom izmjenom jedna su oznaka
	plan := planObrane()
	spremiPropise(t, repo, plan)
	spremiIzmjene(t, repo, &models.IzmjenaPropisa{ID: plan.ID + "-izmjena-" + nnIzmjene.ID(), PropisID: plan.ID, NN: nnIzmjene,
		Upute: []models.UputaIzmjene{
			{Redni: 1, Vrsta: models.UputaDodavanjeDijela, Cilj: models.CiljUpute{Clanci: []string{"xxiii"}}, NoveOznake: []string{"XXIII.a"}, Tekst: "Prva."},
			{Redni: 2, Vrsta: models.UputaDodavanjeDijela, Cilj: models.CiljUpute{Clanci: []string{"xxiii"}}, NoveOznake: []string{"XXIII.b"}, Tekst: "Druga."},
		}})
	sp, err := s.Propis(ctx, "plan-obrane-primjerova")
	if err != nil {
		t.Fatal(err)
	}
	if o := sp.Oznake["xxiii"]; len(o) != 1 || o[0].Natpis != "iza njega dodane točke XXIII.a i XXIII.b · NN 47/26" || o[0].IzClanka != "" {
		t.Errorf("dodane točke: %+v", o)
	}
}

// Stranica izmjene pokazuje svaku odredbu s člankom propisa i stanjem, a
// izmjena kojoj nema propisa ili izmjene nema daje grešku za 404
func TestStranicaIzmjeneSOdredbama(t *testing.T) {
	s, repo, _ := servisPropisa(t)
	ctx := context.Background()
	zakon := zakonNasipa()
	spremiPropise(t, repo, zakon)
	spremiIzmjene(t, repo, izmjenaNasipa(zakon))

	st, err := s.Izmjena(ctx, "zakon-o-cuvanju-nasipa", "nn-2026-47-808")
	if err != nil {
		t.Fatal(err)
	}
	var stanja []string
	for _, u := range st.Upute {
		stanja = append(stanja, u.Stanje)
	}
	if want := []string{"prepoznato", "uz upozorenje", "prepoznato", "prepoznato", "prepoznato", "u cijelom tekstu",
		"nije prepoznato", "ne mijenja članke", "ne mijenja članke", "ne mijenja članke", "uz upozorenje"}; !reflect.DeepEqual(stanja, want) {
		t.Errorf("stanja %q", stanja)
	}
	prva := st.Upute[0]
	if prva.IzClanka != "Članak 1." || len(prva.Ciljevi) != 1 || prva.Ciljevi[0] != (CiljNaStranici{Naziv: "Članak 2.", Poveznica: "/propisi/zakon-o-cuvanju-nasipa/clanak-2"}) {
		t.Errorf("prva odredba: %+v", prva)
	}
	if st.Upute[1].Upozorenje != "Navodnik nije zatvoren u Narodnim novinama. Provjerite odredbu." ||
		st.Upute[10].Upozorenje != "Članka 300. nema u propisu." || st.Upute[10].Ciljevi[0].Naziv != "clanak-300" || st.Upute[10].IzClanka != "" {
		t.Errorf("upozorenja: %+v / %+v", st.Upute[1], st.Upute[10])
	}
	if st.Stupila != "9. 6. 2026." || len(st.Dijelovi) != 3 || st.Dijelovi[0].Poveznica != "" || st.PoveznicaNN == "" {
		t.Errorf("izmjena: %+v", st)
	}
	if _, err := s.Izmjena(ctx, "zakon-o-cuvanju-nasipa", "nn-2030-1-1"); !errors.Is(err, ErrNemaIzmjene) {
		t.Errorf("nepostojeća izmjena: %v", err)
	}
	if _, err := s.Izmjena(ctx, "nema-ga", "nn-2026-47-808"); !errors.Is(err, ErrNemaPropisa) {
		t.Errorf("nepostojeći propis: %v", err)
	}
}

// Kvar baze ne smije izgledati kao „propisa nema”: svaka metoda čitanja
// vraća grešku baze
func TestPropisiKvarBazeUServisu(t *testing.T) {
	s, repo, baza := servisPropisa(t)
	ctx := context.Background()
	zakon := zakonNasipa()
	spremiPropise(t, repo, zakon)
	spremiIzmjene(t, repo, izmjenaNasipa(zakon))

	// izmjena s pokvarenim sadržajem: propis se čita, a izmjene ne
	if _, err := baza.Exec(`UPDATE propisi_izmjene SET sadrzaj = 'pokvareno'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Propis(ctx, "zakon-o-cuvanju-nasipa"); err == nil {
		t.Error("pokvarena izmjena, a naslovnica bez greške")
	}
	if _, err := s.Izmjena(ctx, "zakon-o-cuvanju-nasipa", "nn-2026-47-808"); err == nil {
		t.Error("pokvarena izmjena, a stranica izmjene bez greške")
	}
	if _, err := s.Dio(ctx, "zakon-o-cuvanju-nasipa", "clanak-1"); err == nil {
		t.Error("pokvarena izmjena, a dio bez greške")
	}
	// kazalo bez propisa: pretraga ne može složiti popis ni put
	if _, err := baza.Exec(`DROP TABLE propisi_izmjene`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Trazi(ctx, "nasipa", ""); err == nil {
		t.Error("popis propisa ne radi, a pretraga bez greške")
	}

	baza.Close()
	if _, err := s.Popis(ctx); err == nil || errors.Is(err, ErrNemaPropisa) {
		t.Errorf("popis: %v", err)
	}
	for ime, f := range map[string]func() error{
		"propis":  func() error { _, err := s.Propis(ctx, "zakon-o-cuvanju-nasipa"); return err },
		"dio":     func() error { _, err := s.Dio(ctx, "zakon-o-cuvanju-nasipa", "clanak-1"); return err },
		"izmjena": func() error { _, err := s.Izmjena(ctx, "zakon-o-cuvanju-nasipa", "nn-2026-47-808"); return err },
		"trazi":   func() error { _, err := s.Trazi(ctx, "nasip", ""); return err },
		"propis uz pretragu": func() error {
			_, err := s.Trazi(ctx, "nasip", "zakon-o-cuvanju-nasipa")
			return err
		},
	} {
		if err := f(); err == nil || errors.Is(err, ErrNemaPropisa) {
			t.Errorf("%s uz zatvorenu bazu: %v", ime, err)
		}
	}
}

// Rečenice uz pogodak i stalnu adresu ne smiju ostati bez riječi ni za dio
// koji se rijetko traži, a izmjena s neobičnom oznakom zapisa nosi tu oznaku
func TestRecenicePretrageZaRijetkeDijelove(t *testing.T) {
	if got := recenicaJos(1, models.DioGlava); got != "Još 1 stavak u ovom dijelu." {
		t.Errorf("glava: %q", got)
	}
	if got := recenicaJos(5, models.DioClanak); got != "Još 5 stavaka u ovom članku." {
		t.Errorf("članak: %q", got)
	}
	if got := kratkoIzmjene("x-izmjena"); got != "x-izmjena" {
		t.Errorf("neobična oznaka: %q", got)
	}
	if got := nazivUGenitivu(models.DioPropisa{Vrsta: "nepoznato", Broj: "3"}); got != "odlomka 3" {
		t.Errorf("nepoznata vrsta: %q", got)
	}
	if got := nazivUGenitivu(models.DioPropisa{Vrsta: models.DioPlana, Naslov: "UVODNE ODREDBE"}); got != "dijela" {
		t.Errorf("dio Plana bez broja: %q", got)
	}
}
