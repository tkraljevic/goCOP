// uvoz-mts upisuje skladišta i početno stanje sredstava iz JSON datoteke
// složene iz sektorske tablice za Glavni centar (stanje na dan, potrebe za
// nabavom). Za svako skladište upiše početno stanje na taj dan i zaključen
// popis s potrebama, pa tablica za GCOP na taj dan daje ono što je i
// poslano. Bez -upisi samo ispisuje što bi upisao.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
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

type ulaz struct {
	Dan       string `json:"dan"`
	Sektor    string `json:"sektor"`
	Skladista []struct {
		Area      int    `json:"area"`
		Naziv     string `json:"naziv"`
		Adresa    string `json:"adresa"`
		Centralno bool   `json:"centralno"`
		Stavke    []struct {
			Vrsta   string  `json:"vrsta"`
			Stanje  float64 `json:"stanje"`
			Potrebe float64 `json:"potrebe"`
		} `json:"stavke"`
	} `json:"skladista"`
}

func main() {
	iz := flag.String("iz", "", "JSON datoteka sa skladištima i stavkama")
	dbFlag := flag.String("db", "", "Putanja do SQLite baze (zadano iz gocop.toml)")
	korisnik := flag.String("korisnik", "", "Korisničko ime autora upisa (zadano: prvi globalni administrator)")
	zamijeni := flag.Bool("zamijeni", false, "Skladištu koje već ima promet arhiviraj postojeći promet prije uvoza")
	upisi := flag.Bool("upisi", false, "Bez ove zastavice samo ispisuje što bi upisao")
	flag.Parse()
	if *iz == "" {
		log.Fatal("treba -iz datoteka.json")
	}
	var u ulaz
	b, err := os.ReadFile(*iz)
	if err != nil {
		log.Fatal(err)
	}
	if err := json.Unmarshal(b, &u); err != nil {
		log.Fatal(err)
	}
	dan, err := time.ParseInLocation("2006-01-02", u.Dan, models.Zagreb)
	if err != nil {
		log.Fatalf("dan: %v", err)
	}

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
	database, err := db.OpenDB(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()
	if err := db.InitSchema(database); err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	rec := ledger.New(database, cfg.Node.ID)
	repo := repository.NewMtsRepository(database, rec)
	if err := repo.OsigurajKatalog(ctx); err != nil {
		log.Fatal(err)
	}
	svc := service.NewMtsService(repo, repository.NewSectionRepository(database, rec), nil)

	var autor models.User
	q, args := `SELECT id, full_name FROM users WHERE is_global_admin = 1 AND is_active = 1 ORDER BY username = 'admin', username LIMIT 1`, []any{}
	if *korisnik != "" {
		q, args = `SELECT id, full_name FROM users WHERE username = ?`, []any{*korisnik}
	}
	var id string
	if err := database.QueryRowContext(ctx, q, args...).Scan(&id, &autor.FullName); err != nil {
		log.Fatalf("autor: %v", err)
	}
	autor.ID = uuid.MustParse(id)
	perms := &models.UserPermissions{IsGlobalAdmin: true}
	fmt.Printf("Autor upisa: %s · dan %s · sektor %s\n", autor.FullName, dan.Format("02.01.2006."), u.Sektor)

	vrste, err := svc.SveVrste(ctx)
	if err != nil {
		log.Fatal(err)
	}
	poNazivu := map[string]models.VrstaSredstva{}
	for _, v := range vrste {
		poNazivu[strings.ToLower(strings.TrimSpace(v.Naziv))] = v
	}
	postojeca, err := svc.Skladista(ctx, u.Sektor, 0, true)
	if err != nil {
		log.Fatal(err)
	}

	for _, s := range u.Skladista {
		var sk *models.Skladiste
		for i := range postojeca {
			if strings.EqualFold(strings.TrimSpace(postojeca[i].Naziv), strings.TrimSpace(s.Naziv)) {
				sk = &postojeca[i]
			}
		}
		if sk == nil {
			sk = &models.Skladiste{Sektor: u.Sektor, AreaID: s.Area, Naziv: s.Naziv, Adresa: s.Adresa, Centralno: s.Centralno, Aktivno: true}
			fmt.Printf("skladište BP %d %q: novo\n", s.Area, s.Naziv)
			if *upisi {
				if err := svc.SpremiSkladiste(ctx, perms, sk); err != nil {
					log.Fatalf("skladište %s: %v", s.Naziv, err)
				}
			}
		} else {
			fmt.Printf("skladište BP %d %q: postoji\n", sk.AreaID, sk.Naziv)
			if sk.ID != "" {
				stari, _ := svc.Promet(ctx, repository.FiltarPrometa{SkladisteID: sk.ID})
				if len(stari) > 0 {
					if !*zamijeni {
						fmt.Printf("  već ima %d redaka prometa — preskačem (ili -zamijeni)\n", len(stari))
						continue
					}
					fmt.Printf("  arhiviram %d postojećih redaka prometa\n", len(stari))
					if *upisi {
						if err := repo.ArhivirajPromet(ctx, stari); err != nil {
							log.Fatal(err)
						}
					}
				}
			}
		}
		if !*upisi {
			fmt.Printf("  %d stavki bi se upisalo\n", len(s.Stavke))
			continue
		}
		// početno stanje na dan, pa popis s potrebama
		upisano := 0
		for _, st := range s.Stavke {
			v, ok := poNazivu[strings.ToLower(strings.TrimSpace(st.Vrsta))]
			if !ok {
				log.Fatalf("vrsta %q nije u katalogu", st.Vrsta)
			}
			if st.Stanje <= 0 {
				continue
			}
			oblik := models.OblikOsnovni
			if v.ImaOblike() {
				oblik = models.OblikPrazno
			}
			if _, err := svc.Provedi(ctx, &autor, perms, service.Zahvat{Vrsta: models.PrometPocetno, Datum: dan, SkladisteID: sk.ID,
				VrstaID: v.ID, Oblik: oblik, Kolicina: st.Stanje, Napomena: "iz tablice za Glavni centar, stanje na dan " + dan.Format("02.01.2006.")}); err != nil {
				log.Fatalf("%s / %s: %v", s.Naziv, st.Vrsta, err)
			}
			upisano++
		}
		p, err := svc.PredlozakPopisa(ctx, sk.ID, dan)
		if err != nil {
			log.Fatal(err)
		}
		if p.ID != "" {
			if !p.Zakljucen() && p.Prazan() {
				fmt.Printf("  prazan nacrt popisa na taj dan se arhivira\n")
				if err := repo.ArhivirajPopis(ctx, p); err != nil {
					log.Fatal(err)
				}
			} else {
				fmt.Printf("  %d stavki početnog stanja; popis na taj dan već postoji\n", upisano)
				continue
			}
		}
		if upisano == 0 {
			prazno := true
			for _, st := range s.Stavke {
				if st.Potrebe != 0 {
					prazno = false
				}
			}
			if prazno {
				fmt.Printf("  skladište je prazno u tablici; popis se ne otvara\n")
				continue
			}
		}
		potrebe := map[string]float64{}
		for _, st := range s.Stavke {
			v := poNazivu[strings.ToLower(strings.TrimSpace(st.Vrsta))]
			potrebe[v.ID] += st.Potrebe
		}
		for i := range p.Stavke {
			if p.Stavke[i].Oblik == models.OblikOsnovni || p.Stavke[i].Oblik == models.OblikPrazno {
				p.Stavke[i].Potrebno = potrebe[p.Stavke[i].VrstaID]
			}
		}
		p.Napomena = "prijepis tablice za Glavni centar"
		if err := svc.SpremiPopis(ctx, &autor, perms, p); err != nil {
			log.Fatal(err)
		}
		if err := svc.ZakljuciPopis(ctx, &autor, perms, p.ID); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("  %d stavki početnog stanja, popis zaključen\n", upisano)
	}
	if !*upisi {
		fmt.Println("Ništa nije upisano; ponovite s -upisi.")
	}
}
