// upis-dionice upisuje dionice iz prijepisa wikija kroz servisni sloj, da
// prođu istu provjeru kao unos rukom i ostave zapis u knjizi verzija.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

func main() {
	dbPath := flag.String("db", "data/gocop.db", "putanja do baze")
	nodeID := flag.String("node", "cop-osijek-node", "oznaka čvora")
	src := flag.String("prijepis", "", "JSON prijepis dionica")
	only := flag.String("sifre", "", "šifre dionica odvojene zarezom; prazno = sve iz prijepisa")
	suho := flag.Bool("probno", false, "samo ispiši što bi se upisalo")
	flag.Parse()

	raw, err := os.ReadFile(*src)
	if err != nil {
		log.Fatal(err)
	}
	var sve []models.Section
	if err := json.Unmarshal(raw, &sve); err != nil {
		log.Fatal(err)
	}
	trazene := map[string]bool{}
	for _, c := range strings.Split(*only, ",") {
		if c = strings.ToUpper(strings.TrimSpace(c)); c != "" {
			trazene[c] = true
		}
	}

	database, err := db.OpenDB(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()

	repo := repository.NewSectionRepository(database, ledger.New(database, *nodeID))
	svc := service.NewSectionService(repo, service.NewSSEBroker())
	perms := &models.UserPermissions{IsGlobalAdmin: true}
	ctx := context.Background()

	upisano := 0
	for i := range sve {
		sec := &sve[i]
		if len(trazene) > 0 && !trazene[strings.ToUpper(sec.Code)] {
			continue
		}
		fmt.Printf("%-10s %s\n", sec.Code, sazetak(sec))
		if *suho {
			upisano++
			continue
		}
		if err := svc.SaveSection(ctx, perms, sec, true); err != nil {
			log.Printf("%s: %v", sec.Code, err)
			continue
		}
		upisano++
	}
	fmt.Printf("dionica: %d\n", upisano)
}

func sazetak(s *models.Section) string {
	var objekata, nasipa, vodomjera int
	for _, p := range s.Parts {
		objekata += len(p.Objects)
		nasipa += len(p.Embankments)
		vodomjera += len(p.Gauges)
	}
	return fmt.Sprintf("%s · poddionica %d, objekata %d, nasipa %d, vodomjera %d",
		models.FormatKm(s.Length())+" km", len(s.Parts), objekata, nasipa, vodomjera)
}
