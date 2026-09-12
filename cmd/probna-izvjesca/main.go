// probna-izvjesca upisuje probne dionice i dnevna izvješća rukovoditelja
// dionica u lokalnu bazu, da se sektorsko izvješće ima iz čega složiti.
// Bez -upisi samo ispisuje što bi upisao. Probne dionice nose "(probna
// dionica)" u opisu pa se lako nađu i maknu.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"gocop/internal/config"
	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

type probnaDionica struct {
	Code    string
	Area    int
	Opis    string
	Voda    string // šifra vode iz registra, kad je ima
	Vodotok string
	Postaje []string // šifre vodomjera
}

var dionice = []probnaDionica{
	{"B.15.1", 15, "rijeka Vuka, l.o. i d.o.; Vukovar – Nuštar (probna dionica)", "", "Vuka", nil},
	{"B.16.1", 16, "rijeka Dunav, l.o.; Batina – Zmajevac (probna dionica)", "rijeka-dunav", "Dunav", []string{"batina"}},
	{"B.16.2", 16, "rijeka Drava, l.o.; Osijek – ušće u Dunav (probna dionica)", "", "Drava", nil},
	{"B.17.1", 17, "rijeka Karašica; Donji Miholjac – ušće (probna dionica)", "", "Karašica", nil},
}

type probnoIzvjesce struct {
	Code   string
	Danak  int // 0 = danas, -1 = jučer
	Stadij models.DefensePhase
	Predaj bool
	S      models.IzvjesceSadrzaj
}

func vod(postaja, sat, v, j string) models.VodostajUIzvjescu {
	return models.VodostajUIzvjescu{Postaja: postaja, Sat: sat, Vrijednost: v, Jedinica: j}
}

var izvjesca = []probnoIzvjesce{
	// jučer
	{"B.34.1", -1, models.PhaseRegular, true, models.IzvjesceSadrzaj{Vodotok: "Dunav", Tendencija: models.TendencijaPorast,
		Vodostaji: []models.VodostajUIzvjescu{vod("Dunav – Batina", "07:00", "+548", "cm"), vod("Dunav – Batina", "13:00", "+556", "cm")},
		Pregled:   "Nasip pregledan cijelom dužinom; bez procjeđivanja. Naplavine na zaštitnoj rešetki propusta kod Zelenog otoka uočene u 09:30, uklonjene do 11:00.",
		Radnje:    "Ophodnja nasipa 2×, čišćenje propusta. Pripremljene vreće na deponiji Batina.",
		Vrece:     "800 (pripremljeno)", Materijal: "12 m³ pijeska",
		Pravne: models.SudioniciPravne{Ljudi: 6, Kamioni: 1, Bageri: 1, Traktori: 1},
	}},
	{"B.34.2", -1, models.PhaseRegular, true, models.IzvjesceSadrzaj{Vodotok: "Dunav", Tendencija: models.TendencijaPorast,
		Vodostaji: []models.VodostajUIzvjescu{vod("Dunav – Batina", "07:00", "+548", "cm")},
		Pregled:   "Nasip Zeleni otok – Ludaš bez oštećenja. Kritično mjesto rkm 1412+300 (stari proboj) pod nadzorom, suho.",
		Radnje:    "Ophodnja nasipa; postavljena rampa na pristupnom putu Aljmaš.",
		Pravne:    models.SudioniciPravne{Ljudi: 4, Kamioni: 1},
	}},
	{"B.16.1", -1, models.PhaseRegular, true, models.IzvjesceSadrzaj{Vodotok: "Dunav", Tendencija: models.TendencijaPorast,
		Vodostaji: []models.VodostajUIzvjescu{vod("Dunav – Batina", "07:00", "+548", "cm")},
		Pregled:   "Lijevoobalni nasip Batina – Zmajevac bez oštećenja. Procjeđivanje kroz temeljno tlo kod Zmajevca uočeno u 14:00, bistra voda.",
		Radnje:    "Ophodnja nasipa 3×; na procjeđivanju postavljen kontranasip od vreća 12 m.",
		Vrece:     "350", Materijal: "6 m³ pijeska", Nasipi: "12 m kontranasipa",
		Pravne: models.SudioniciPravne{Ljudi: 8, Kamioni: 2, Bageri: 1, Utovarivaci: 1},
		Ostali: models.SudioniciOstali{Vatrogasci: 6},
	}},
	{"B.16.2", -1, models.PhasePrep, true, models.IzvjesceSadrzaj{Vodotok: "Drava", Tendencija: models.TendencijaStagnacija,
		Vodostaji: []models.VodostajUIzvjescu{vod("Drava – Osijek", "07:00", "+312", "cm")},
		Pregled:   "Nasipi u Osijeku pregledani, bez oštećenja. Protočnost korita uredna.",
		Radnje:    "Ophodnja 1×. Provjera zatvarača na ušću kanala Poloj.",
		Pravne:    models.SudioniciPravne{Ljudi: 2},
	}},
	// danas
	{"B.34.1", 0, models.PhaseEmergency, true, models.IzvjesceSadrzaj{Vodotok: "Dunav", Tendencija: models.TendencijaNagliPorast,
		Vodostaji: []models.VodostajUIzvjescu{vod("Dunav – Batina", "07:00", "+571", "cm"), vod("Dunav – Batina", "12:00", "+583", "cm")},
		Pregled:   "U 05:40 uočeno procjeđivanje kroz nasip na rkm 1425+100, mutna voda. Kritično mjesto: rkm 1425+000 – 1425+300.",
		Radnje:    "Izvedeno nadvišenje nasipa vrećama u dužini 180 m i kontranasip 40 m. Crpljenje procjedne vode 2 mobilne crpke.",
		Vrece:     "4 200", Materijal: "38 m³ pijeska, 20 m³ šljunka", Nasipi: "180 m nadvišenja, 40 m kontranasipa", Crpke: "2 × 60 l/s",
		Objekti: "Ustava Zeleni otok zatvorena u 06:15.",
		Pravne:  models.SudioniciPravne{Ljudi: 24, Kamioni: 4, Bageri: 2, KombStrojevi: 1, Utovarivaci: 2, Traktori: 2, Camci: 1},
		Ostali:  models.SudioniciOstali{Policija: 2, Vatrogasci: 18, CivilnaZastita: 6, Drugi: "DVD Batina, mještani 15"},
	}},
	{"B.34.2", 0, models.PhaseRegular, true, models.IzvjesceSadrzaj{Vodotok: "Dunav", Tendencija: models.TendencijaPorast,
		Vodostaji: []models.VodostajUIzvjescu{vod("Dunav – Batina", "07:00", "+571", "cm")},
		Pregled:   "Nasip bez oštećenja; kritično mjesto rkm 1412+300 suho.",
		Radnje:    "Ophodnja nasipa 3×; pripremljeno 500 vreća na deponiji Aljmaš.",
		Vrece:     "500 (pripremljeno)",
		Pravne:    models.SudioniciPravne{Ljudi: 6, Kamioni: 1, Traktori: 1},
	}},
	{"B.16.1", 0, models.PhaseEmergency, true, models.IzvjesceSadrzaj{Vodotok: "Dunav", Tendencija: models.TendencijaNagliPorast,
		Vodostaji: []models.VodostajUIzvjescu{vod("Dunav – Batina", "07:00", "+571", "cm")},
		Pregled:   "Procjeđivanje kod Zmajevca pojačano, voda i dalje bistra. Uočen klizavi pokos nasipa na rkm 1419+800 u 10:20.",
		Radnje:    "Kontranasip produžen na 45 m; sanacija pokosa kamenim nabačajem 30 m. Crpljenje 1 mobilna crpka.",
		Vrece:     "1 800", Materijal: "14 m³ pijeska, 60 t kamena", Nasipi: "45 m kontranasipa", Crpke: "1 × 40 l/s",
		Pravne:      models.SudioniciPravne{Ljudi: 14, Kamioni: 3, Bageri: 2, Utovarivaci: 1, Camci: 1},
		Ostali:      models.SudioniciOstali{Vatrogasci: 12, CrveniKriz: 2},
		Poplavljeno: models.Poplavljeno{Naselja: "Zmajevac (podrumi uz nasip)", Stambeni: 4, Infrastruktura: "lokalna cesta Zmajevac – Batina, 200 m", PoljoprivredneHa: 35},
	}},
	{"B.16.2", 0, models.PhaseRegular, true, models.IzvjesceSadrzaj{Vodotok: "Drava", Tendencija: models.TendencijaPorast,
		Vodostaji: []models.VodostajUIzvjescu{vod("Drava – Osijek", "07:00", "+341", "cm")},
		Pregled:   "Nasipi bez oštećenja. Uspor Dunava podiže Dravu u Osijeku.",
		Radnje:    "Ophodnja 2×; zatvorena ustava na ušću kanala Poloj u 08:00.",
		Objekti:   "Ustava Poloj zatvorena 08:00; CS Poloj u radu od 08:30 (2 × 1,5 m³/s).",
		Pravne:    models.SudioniciPravne{Ljudi: 4, Kamioni: 1},
	}},
	{"B.15.1", 0, models.PhasePrep, true, models.IzvjesceSadrzaj{Vodotok: "Vuka", Tendencija: models.TendencijaStagnacija,
		Vodostaji: []models.VodostajUIzvjescu{vod("Vuka – Vukovar", "07:00", "+188", "cm")},
		Pregled:   "Korito Vuke protočno, nasipi u Vukovaru bez oštećenja.",
		Radnje:    "Ophodnja 1×.",
		Pravne:    models.SudioniciPravne{Ljudi: 2},
	}},
	{"B.17.1", 0, models.PhaseNormal, false, models.IzvjesceSadrzaj{Vodotok: "Karašica", Tendencija: models.TendencijaOpadanje,
		Vodostaji: []models.VodostajUIzvjescu{vod("Karašica – Donji Miholjac", "07:00", "+142", "cm")},
		Pregled:   "Bez posebnosti.",
	}},
}

func main() {
	dbFlag := flag.String("db", "", "Putanja do SQLite baze (zadano iz gocop.toml ili data/gocop.db)")
	nodeFlag := flag.String("node", "", "Identifikator čvora (zadano iz gocop.toml)")
	korisnik := flag.String("korisnik", "", "Korisničko ime autora izvješća (zadano: prvi globalni administrator)")
	upisi := flag.Bool("upisi", false, "Bez ove zastavice samo ispisuje što bi upisao")
	flag.Parse()

	dbPath := *dbFlag
	if dbPath == "" {
		dbPath = config.Default().DB
	}
	cfg, _, err := config.Load(config.Candidates("", dbPath))
	if err != nil {
		log.Fatal(err)
	}
	if *dbFlag == "" {
		dbPath = cfg.DB
	}
	nodeID := cfg.Node.ID
	if *nodeFlag != "" {
		nodeID = *nodeFlag
	}
	database, err := db.OpenDB(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()
	if err := db.InitSchema(database); err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	rec := ledger.New(database, nodeID)
	sections := repository.NewSectionRepository(database, rec)
	stations := repository.NewStationRepository(database, rec)
	journals := repository.NewJournalRepository(database, rec)
	svc := service.NewIzvjescaService(repository.NewIzvjescaRepository(database, rec), sections, stations,
		repository.NewReadingRepository(database, rec), repository.NewEpisodeRepository(database, rec), journals)

	// autor
	var autor models.User
	q := `SELECT id, full_name FROM users WHERE is_global_admin = 1 AND is_active = 1 ORDER BY username = 'admin', username LIMIT 1`
	args := []any{}
	if *korisnik != "" {
		q, args = `SELECT id, full_name FROM users WHERE username = ?`, []any{*korisnik}
	}
	var id string
	if err := database.QueryRowContext(ctx, q, args...).Scan(&id, &autor.FullName); err != nil {
		log.Fatalf("autor: %v", err)
	}
	autor.ID = uuid.MustParse(id)
	perms := &models.UserPermissions{IsGlobalAdmin: true}
	fmt.Printf("Autor izvješća: %s\n", autor.FullName)

	// vodomjeri po šifri
	postaje := map[string]string{}
	rows, err := database.QueryContext(ctx, `SELECT code, id FROM stations`)
	if err != nil {
		log.Fatal(err)
	}
	for rows.Next() {
		var c, i string
		_ = rows.Scan(&c, &i)
		postaje[c] = i
	}
	rows.Close()

	// dionice
	for _, d := range dionice {
		if sec, _ := sections.GetSectionByCode(d.Code); sec != nil {
			fmt.Printf("dionica %s već postoji\n", d.Code)
			continue
		}
		var ids []string
		for _, c := range d.Postaje {
			if id, ok := postaje[c]; ok {
				ids = append(ids, id)
			}
		}
		sec := &models.Section{Code: d.Code, AreaID: d.Area, SectorID: "B", Description: d.Opis, DescriptionCustom: true,
			Notes: "Probna dionica za isprobavanje izvješća; nije iz Državnog plana.",
			Parts: []models.SectionPart{{Seq: 1, WatercourseCode: d.Voda, Description: d.Opis, StationIDs: ids}}}
		fmt.Printf("dionica %s: %s\n", d.Code, d.Opis)
		if *upisi {
			if err := sections.SaveSection(ctx, sec); err != nil {
				log.Fatalf("dionica %s: %v", d.Code, err)
			}
		}
	}

	// izvješća
	danas := time.Now().In(models.Zagreb)
	danas = time.Date(danas.Year(), danas.Month(), danas.Day(), 0, 0, 0, 0, models.Zagreb)
	for _, p := range izvjesca {
		sec, err := sections.GetSectionByCode(p.Code)
		if err != nil || sec == nil {
			if !*upisi {
				fmt.Printf("izvješće %s: dionica još ne postoji (upisala bi se)\n", p.Code)
				continue
			}
			log.Fatalf("dionica %s: %v", p.Code, err)
		}
		dan := danas.AddDate(0, 0, p.Danak)
		iz, err := svc.Predlozak(ctx, sec, dan)
		if err != nil {
			log.Fatal(err)
		}
		if iz.ID != "" {
			fmt.Printf("izvješće %s za %s već postoji\n", p.Code, dan.Format("02.01."))
			continue
		}
		iz.Stadij = p.Stadij
		iz.Sadrzaj = p.S
		stanje := "nacrt"
		if p.Predaj {
			stanje = "predano"
		}
		fmt.Printf("izvješće %s za %s (%s, %s): %s\n", p.Code, dan.Format("02.01."), models.StadijKratica(p.Stadij), stanje, kratko(p.S.Pregled))
		if !*upisi {
			continue
		}
		if err := svc.Spremi(ctx, &autor, perms, sec, iz); err != nil {
			log.Fatalf("izvješće %s: %v", p.Code, err)
		}
		if p.Predaj {
			if err := svc.Predaj(ctx, &autor, perms, sec, iz.ID); err != nil {
				log.Fatalf("predaja %s: %v", p.Code, err)
			}
		}
	}
	// zapisi u otvoreni dnevnik COP-a za danas, da sektorsko izvješće ima kronologiju
	if dnevnici, err := journals.ListCOPJournals(ctx, "B"); err == nil {
		for _, j := range dnevnici {
			if j.EndedAt != nil || j.Reconstruction {
				continue
			}
			postojeci, _ := journals.EntriesForJournal(ctx, j.ID)
			imaDanas := false
			for _, e := range postojeci {
				if e.Date.In(models.Zagreb).Format("2006-01-02") == danas.Format("2006-01-02") && e.Kind != models.EntryKindDuty {
					imaDanas = true
				}
			}
			if imaDanas {
				fmt.Printf("dnevnik %q već ima zapise za danas\n", j.DisplayTitle())
				break
			}
			js := service.NewJournalService(journals, nil, nil)
			bp16, bp34 := 16, 34
			for _, z := range []struct {
				sat  string
				kind string
				tko  string
				pod  *int
				text string
			}{
				{"05:50", models.EntryKindReport, "vodočuvar Batina", &bp34, "Procjeđivanje kroz nasip na rkm 1425+100, mutna voda. Traži se vreće i ljudi."},
				{"06:15", models.EntryKindNotice, "rukovoditelj dionice B.34.1", &bp34, "Zatvorena ustava Zeleni otok. Proglašena izvanredna obrana na B.34.1."},
				{"07:10", models.EntryKindReport, "DHMZ", nil, "Prognoza: Dunav kod Batine sutra +600 cm, kiša 20–40 mm na slivu."},
				{"08:05", models.EntryKindNotice, "VGI Baranja", &bp16, "CS Poloj u radu od 08:30, ustava Poloj zatvorena u 08:00."},
				{"10:30", models.EntryKindReport, "rukovoditelj dionice B.16.1", &bp16, "Klizanje pokosa nasipa rkm 1419+800; sanacija kamenim nabačajem u tijeku, DVD Zmajevac 12 ljudi."},
				{"14:00", models.EntryKindNote, "", nil, "Sastanak stožera u 15:00 u COP-u Osijek."},
			} {
				h, _ := time.ParseInLocation("15:04", z.sat, models.Zagreb)
				kad := time.Date(danas.Year(), danas.Month(), danas.Day(), h.Hour(), h.Minute(), 0, 0, models.Zagreb)
				e := &models.JournalEntry{JournalID: j.ID, Date: kad, HappenedAt: &kad, Kind: z.kind, Text: z.text, ReportedBy: z.tko, Podrucje: z.pod}
				fmt.Printf("zapis %s %s: %s\n", z.sat, z.kind, kratko(z.text))
				if *upisi {
					if err := js.DodajZapisCOP(ctx, &autor, perms, models.Opseg{Sektor: "B", Podrucja: []int{15, 16, 17, 18, 34}}, &j, e); err != nil {
						log.Fatalf("zapis: %v", err)
					}
				}
			}
			break
		}
	}
	if !*upisi {
		fmt.Println("Ništa nije upisano; ponovite s -upisi.")
	}
}

func kratko(s string) string {
	if len([]rune(s)) > 60 {
		return string([]rune(s)[:60]) + "…"
	}
	return strings.TrimSpace(s)
}
