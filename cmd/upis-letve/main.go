// upis-letve popunjava karticu vodomjerne postaje iz plana obrane od poplava i
// geodetskog elaborata.
//
// Ide kroz servis, dakle kroz knjigu verzija — izravan upis u tablicu ne bi
// stvorio verziju, pa promjena ne bi otišla na druge čvorove i vratila bi se
// pri prvoj sinkronizaciji.
//
// Upisuje se samo ono što je zadano; ostalo se ne dira. Prazna zastavica i
// upisana prazna vrijednost nisu isto: prvo znači „ne diraj", pa se za brisanje
// polja koristi "-".
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"gocop/internal/db"
	"gocop/internal/javnivodostaji"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

func main() {
	dbPath := flag.String("db", "data/gocop.db", "putanja do baze")
	nodeID := flag.String("node", "cop-osijek-node", "oznaka čvora")
	sifra := flag.String("letva", "", "šifra postaje, npr. vukovar")
	suho := flag.Bool("probno", false, "samo ispiši što bi se promijenilo")

	naziv := flag.String("naziv", "", "naziv postaje")
	vodotok := flag.String("vodotok", "", "naziv vodotoka")
	vodotokSifra := flag.String("vodotok-sifra", "", "šifra vodotoka u registru")
	podrucje := flag.String("podrucje", "", "vodno područje")
	stacionaza := flag.String("stacionaza", "", "stacionaža vodomjera, npr. „rkm 1.333,45”")
	izvorniNaziv := flag.String("izvorni-naziv", "", "zapis naziva iz dokumentacije dionice")

	kota := flag.String("kota", "", "kota nule u starom sustavu (TRST)")
	kotaNova := flag.String("kota-nova", "", "kota nule u novom sustavu (HVRS71)")
	kotaIzvor := flag.String("kota-izvor", "", "odakle je kota nule")
	kotaNacin := flag.String("kota-nacin", "", "kako je kota nule dobivena")
	kotaDatumMjerenja := flag.String("kota-datum-mjerenja", "", "datum terenskog mjerenja kote, YYYY-MM-DD")
	kotaDatumDokumenta := flag.String("kota-datum-dokumenta", "", "datum elaborata, YYYY-MM-DD ili YYYY-MM")

	pripremno := flag.String("pripremno", "", "prag pripremnog stanja, u cm")
	redovna := flag.String("redovna", "", "prag redovne obrane, u cm")
	izvanredna := flag.String("izvanredna", "", "prag izvanredne obrane, u cm")
	stanje := flag.String("stanje", "", "prag izvanrednog stanja, u cm")
	rekord := flag.String("rekord", "", "najviši zabilježeni vodostaj, u cm")

	ograda := flag.String("ograda", "", "ograda uz niz: izvor|veličina|od|do|ispod|iznad|tekst")
	rekordZapis := flag.String("rekord-zapis", "", "kako rekord piše na kartici, npr. „+958 (25. 6. 1965.), preračunato”")
	ekstrem := flag.String("ekstrem", "", "zabilježeni ekstrem: MAX|MIN|cm|datum|kvaliteta|izvor|metoda|napomena")
	ekstremiOcisti := flag.Bool("ekstremi-ocisti", false, "ukloni ponovljene ekstreme (ista vrsta, vrijednost i datum); ostaje zadnji upisani")
	sirina := flag.String("sirina", "", "zemljopisna širina, decimalni stupnjevi")
	duzina := flag.String("duzina", "", "zemljopisna dužina, decimalni stupnjevi")
	napomena := flag.String("napomena", "", "opća napomena uz postaju")
	pregled := flag.String("pregled", "", "označi za pregled: da ili ne; prazno ne mijenja")
	napomenaPregleda := flag.String("napomena-pregleda", "", "upozorenje i razlog pregleda")
	nova := flag.Bool("nova", false, "otvori postaju ako je nema (traži -naziv)")
	javnaPostaja := flag.String("javna-postaja", "", "broj postaje na vodostaji.voda.hr; uključuje automatsko preuzimanje")
	javnaAdresa := flag.String("javna-adresa", "", "adresa javne stranice letve, za izvore izvan Hrvatskih voda; uključuje automatsko preuzimanje")
	flag.Parse()

	if strings.TrimSpace(*sifra) == "" {
		log.Fatal("zadajte -letva")
	}
	database, err := db.OpenDB(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()

	rec := ledger.New(database, *nodeID)
	repo := repository.NewStationRepository(database, rec)
	secRepo := repository.NewSectionRepository(database, rec)
	broker := service.NewSSEBroker()
	svc := service.NewStationService(repo, service.NewSectionService(secRepo, broker), broker)
	perms := &models.UserPermissions{IsGlobalAdmin: true}
	ctx := context.Background()

	postaje, err := svc.ListStations(ctx, *sifra, "", "", false)
	if err != nil {
		log.Fatal(err)
	}
	var letva *models.Station
	for i := range postaje {
		if strings.EqualFold(postaje[i].Code, *sifra) {
			letva = &postaje[i]
			break
		}
	}
	if letva == nil && !*nova {
		log.Fatalf("postaja %q nije pronađena; otvorite ju kroz Registri › Vodomjerne postaje ili dodajte -nova", *sifra)
	}
	if letva == nil {
		if strings.TrimSpace(*naziv) == "" {
			log.Fatal("nova postaja treba -naziv")
		}
		letva = &models.Station{Code: strings.TrimSpace(*sifra), Name: strings.TrimSpace(*naziv)}
		if *suho {
			fmt.Printf("otvorila bi se postaja %q (%s)\n", letva.Name, letva.Code)
		} else if err := svc.CreateStation(ctx, perms, letva, ""); err != nil {
			log.Fatalf("otvaranje postaje: %v", err)
		} else {
			fmt.Printf("otvorena postaja %q (%s)\n", letva.Name, letva.Code)
		}
	}

	var promjene []string
	tekst := func(ime string, cilj *string, nova string) {
		if nova == "" {
			return
		}
		if nova == "-" {
			nova = ""
		}
		if *cilj == nova {
			return
		}
		promjene = append(promjene, fmt.Sprintf("%-18s %q → %q", ime, *cilj, nova))
		*cilj = nova
	}
	broj := func(ime string, cilj **float64, nova string) {
		if nova == "" {
			return
		}
		if nova == "-" {
			promjene = append(promjene, fmt.Sprintf("%-18s briše se", ime))
			*cilj = nil
			return
		}
		v, err := strconv.ParseFloat(strings.ReplaceAll(nova, ",", "."), 64)
		if err != nil {
			log.Fatalf("%s: %v", ime, err)
		}
		staro := "—"
		if *cilj != nil {
			staro = fmt.Sprintf("%g", **cilj)
		}
		promjene = append(promjene, fmt.Sprintf("%-18s %s → %g", ime, staro, v))
		*cilj = &v
	}
	prag := func(ime string, cilj *models.Threshold, nova string) {
		if nova == "" {
			return
		}
		if nova == "-" {
			promjene = append(promjene, fmt.Sprintf("%-18s briše se", ime))
			*cilj = models.Threshold{}
			return
		}
		v, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(nova), "+"))
		if err != nil {
			log.Fatalf("%s: %v", ime, err)
		}
		staro := "—"
		if cilj.Cm != nil {
			staro = strconv.Itoa(*cilj.Cm)
		}
		promjene = append(promjene, fmt.Sprintf("%-18s %s → %+d", ime, staro, v))
		*cilj = models.Threshold{Cm: &v, Raw: fmt.Sprintf("%+d", v)}
	}

	tekst("naziv", &letva.Name, *naziv)
	tekst("vodotok", &letva.Watercourse, *vodotok)
	tekst("šifra vodotoka", &letva.WatercourseCode, *vodotokSifra)
	tekst("vodno područje", &letva.WaterArea, *podrucje)
	tekst("stacionaža", &letva.Stationing, *stacionaza)
	tekst("izvorni naziv", &letva.SourceName, *izvorniNaziv)
	tekst("izvor kote", &letva.ZeroDatumSource, *kotaIzvor)
	tekst("način kote", &letva.ZeroDatumMethod, *kotaNacin)
	tekst("datum mjerenja", &letva.ZeroDatumSurveyDate, *kotaDatumMjerenja)
	tekst("datum dokumenta", &letva.ZeroDatumDocumentDate, *kotaDatumDokumenta)
	tekst("napomena", &letva.Notes, *napomena)
	tekst("napomena pregleda", &letva.ReviewNote, *napomenaPregleda)
	if *javnaPostaja != "" {
		id, err := strconv.Atoi(strings.TrimSpace(*javnaPostaja))
		if err != nil || id <= 0 {
			log.Fatalf("javna postaja: %q nije broj", *javnaPostaja)
		}
		postaja := javnivodostaji.Postaja{ID: id}
		klijent := &javnivodostaji.Client{}
		if sektor, err := klijent.NadjiSektor(ctx, id); err != nil {
			fmt.Printf("upozorenje: sektor postaje %d nije pronađen (%v); adresa ostaje bez njega\n", id, err)
		} else {
			postaja.Sektor = sektor
		}
		adresa := javnivodostaji.AdresaPostaje(postaja)
		tekst("javna postaja", &letva.JavniURL, adresa)
		if !letva.JavniUvoz {
			promjene = append(promjene, "automatsko preuzimanje  ne → da")
			letva.JavniUvoz = true
		}
	}
	if *javnaAdresa != "" {
		adresa := strings.TrimSpace(*javnaAdresa)
		tekst("javna adresa", &letva.JavniURL, adresa)
		if !letva.JavniUvoz {
			promjene = append(promjene, "automatsko preuzimanje  ne → da")
			letva.JavniUvoz = true
		}
	}
	broj("kota nule", &letva.ZeroDatum, *kota)
	broj("kota nule (nova)", &letva.ZeroDatumNew, *kotaNova)
	broj("širina", &letva.Latitude, *sirina)
	broj("dužina", &letva.Longitude, *duzina)
	prag("pripremno", &letva.Prep, *pripremno)
	prag("redovna", &letva.Regular, *redovna)
	prag("izvanredna", &letva.Emergency, *izvanredna)
	prag("izvanredno stanje", &letva.State, *stanje)
	prag("rekord", &letva.Record, *rekord)
	if *pregled != "" {
		nova, err := daNe(*pregled)
		if err != nil {
			log.Fatal(err)
		}
		if letva.NeedsReview != nova {
			promjene = append(promjene, fmt.Sprintf("%-18s %t → %t", "za pregled", letva.NeedsReview, nova))
			letva.NeedsReview = nova
		}
	}

	if *rekordZapis != "" {
		zapis := strings.TrimSpace(*rekordZapis)
		if letva.Record.Raw != zapis {
			promjene = append(promjene, fmt.Sprintf("%-18s %q → %q", "zapis rekorda", letva.Record.Raw, zapis))
			letva.Record.Raw = zapis
		}
	}

	if *ekstrem != "" {
		e, err := ekstremIz(*ekstrem)
		if err != nil {
			log.Fatal(err)
		}
		letva.Extremes = append(letva.Extremes, e)
		promjene = append(promjene, fmt.Sprintf("%-18s + %s %d cm, %s, %s", "ekstrem", e.Kind, *e.LevelCm, e.OnDate, e.Quality))
	}

	if *ekstremiOcisti {
		ostaje, maknuto := bezPonovljenih(letva.Extremes)
		for _, e := range maknuto {
			promjene = append(promjene, fmt.Sprintf("%-18s − %s %d cm, %s (%s)", "ekstrem", e.Kind, *e.LevelCm, e.OnDate, e.Source))
		}
		letva.Extremes = ostaje
	}

	if *ograda != "" {
		o, err := ogradaIz(*ograda)
		if err != nil {
			log.Fatal(err)
		}
		letva.OgradeNiza = append(letva.OgradeNiza, o)
		promjene = append(promjene, fmt.Sprintf("%-18s + %s %s %s", "ograda", o.Izvor, o.Raspon(), o.Tekst))
	}

	if len(promjene) == 0 {
		fmt.Println("ništa se ne mijenja")
		return
	}
	fmt.Printf("%s (%s):\n", letva.Name, letva.Code)
	for _, p := range promjene {
		fmt.Println("  " + p)
	}
	if *suho {
		fmt.Println("\nproba — ništa nije upisano; ponovite bez -probno")
		return
	}
	if err := svc.UpdateStation(ctx, perms, letva); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("\nupisano, %d %s\n", len(promjene), uzBroj(len(promjene)))
}

func daNe(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "da", "d", "1", "true":
		return true, nil
	case "ne", "n", "0", "false":
		return false, nil
	default:
		return false, fmt.Errorf("pregled treba biti da ili ne, dobiveno %q", s)
	}
}

// bezPonovljenih miče ekstreme koji govore o istom događaju: ista vrsta,
// ista vrijednost i isti datum. Ostaje zadnji upisani, jer je ručni upis
// došao nakon onoga izvučenog iz podataka i nosi bolju podlogu.
func bezPonovljenih(svi []models.StationExtreme) (ostaje, maknuto []models.StationExtreme) {
	kljuc := func(e models.StationExtreme) string {
		cm := "—"
		if e.LevelCm != nil {
			cm = strconv.Itoa(*e.LevelCm)
		}
		return e.Kind + "|" + cm + "|" + e.OnDate
	}
	zadnji := map[string]int{}
	for i, e := range svi {
		zadnji[kljuc(e)] = i
	}
	for i, e := range svi {
		if zadnji[kljuc(e)] == i {
			ostaje = append(ostaje, e)
			continue
		}
		maknuto = append(maknuto, e)
	}
	return ostaje, maknuto
}

// ekstremIz čita zabilježeni ekstrem iz jednog retka:
// MAX|MIN|cm|datum|kvaliteta|izvor|metoda|napomena. Kvaliteta kaže je li
// vrijednost izmjerena na ovoj letvi ili dobivena računom, pa se bez nje ne
// upisuje: na kartici bi inače preračun izgledao kao mjerenje.
func ekstremIz(s string) (models.StationExtreme, error) {
	dj := strings.Split(s, "|")
	if len(dj) != 7 {
		return models.StationExtreme{}, fmt.Errorf("ekstrem treba sedam polja odvojenih |, dobiveno %d", len(dj))
	}
	vrsta := strings.ToUpper(strings.TrimSpace(dj[0]))
	if vrsta != models.ExtremeMax && vrsta != models.ExtremeMin {
		return models.StationExtreme{}, fmt.Errorf("ekstrem je MAX ili MIN, ne %q", dj[0])
	}
	cm, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(dj[1]), "+"))
	if err != nil {
		return models.StationExtreme{}, fmt.Errorf("ekstrem: %v", err)
	}
	kvaliteta := strings.ToUpper(strings.TrimSpace(dj[3]))
	switch kvaliteta {
	case models.QualityMeasured, models.QualityReconstructed, models.QualityUncertain:
	default:
		return models.StationExtreme{}, fmt.Errorf("kvaliteta je %s, %s ili %s, ne %q",
			models.QualityMeasured, models.QualityReconstructed, models.QualityUncertain, dj[3])
	}
	return models.StationExtreme{
		Kind:    vrsta,
		LevelCm: &cm,
		OnDate:  strings.TrimSpace(dj[2]),
		Quality: kvaliteta,
		Source:  strings.TrimSpace(dj[4]),
		Method:  strings.TrimSpace(dj[5]),
		Note:    strings.TrimSpace(dj[6]),
	}, nil
}

// ogradaIz čita ogradu iz jednog retka: izvor|veličina|od|do|ispod|iznad|tekst.
// Prazna polja se preskaču — ograda bez granica vrijedi za cijeli niz.
func ogradaIz(s string) (models.OgradaNiza, error) {
	dj := strings.Split(s, "|")
	if len(dj) != 7 {
		return models.OgradaNiza{}, fmt.Errorf("ograda treba sedam polja odvojenih |, dobiveno %d", len(dj))
	}
	o := models.OgradaNiza{
		Izvor: strings.TrimSpace(dj[0]), Velicina: strings.TrimSpace(dj[1]),
		Od: strings.TrimSpace(dj[2]), Do: strings.TrimSpace(dj[3]),
		Tekst: strings.TrimSpace(dj[6]),
	}
	if o.Izvor == "" || o.Tekst == "" {
		return o, fmt.Errorf("ograda mora imati izvor i tekst")
	}
	granica := func(v string) (*float64, error) {
		v = strings.TrimSpace(v)
		if v == "" {
			return nil, nil
		}
		f, err := strconv.ParseFloat(strings.ReplaceAll(v, ",", "."), 64)
		if err != nil {
			return nil, err
		}
		return &f, nil
	}
	var err error
	if o.Ispod, err = granica(dj[4]); err != nil {
		return o, fmt.Errorf("granica ispod: %w", err)
	}
	if o.Iznad, err = granica(dj[5]); err != nil {
		return o, fmt.Errorf("granica iznad: %w", err)
	}
	return o, nil
}

func uzBroj(n int) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return "promjena"
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		return "promjene"
	default:
		return "promjena"
	}
}
