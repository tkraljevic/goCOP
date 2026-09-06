package main

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// dodjelaZaduzenja upisuje zaduženja iz rasporeda djelatnicima koji već postoje.
// Osoba koju imenik ne vodi — a raspored je spominje, poput ljudi iz pravnih
// osoba — preskače se i navodi na kraju, da se ne izgubi iz vida.
func dodjelaZaduzenja(dbPath, nodeID, rasporedPath, sektor string, suho bool) {
	raw, err := os.ReadFile(rasporedPath)
	if err != nil {
		log.Fatal(err)
	}
	zaduzenja := citajRaspored(string(raw), sektor)

	database, err := db.OpenDB(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()
	rec := ledger.New(database, nodeID)
	uRepo := repository.NewUserRepository(database, rec)
	sRepo := repository.NewSessionRepository(database)
	svc := service.NewUserService(uRepo, service.NewAuthService(uRepo, sRepo), service.NewSSEBroker())

	admin, err := uRepo.GetUserByUsername("admin")
	if err != nil || admin == nil {
		log.Fatalf("račun admin nije nađen: %v", err)
	}
	actor := &models.UserPermissions{User: *admin, IsGlobalAdmin: true}

	svi, err := uRepo.ListUsers("", 0, "", "", "")
	if err != nil {
		log.Fatal(err)
	}
	poImenu := map[string]*models.User{}
	for i := range svi {
		poImenu[strings.ToLower(svi[i].FullName)] = &svi[i]
	}

	prvo := map[string]bool{} // prvo zaduženje osobe je glavno
	upisano := 0
	nepoznati := map[string]bool{}
	for _, z := range zaduzenja {
		u, ok := poImenu[strings.ToLower(z.Osoba)]
		if !ok {
			nepoznati[z.Osoba] = true
			continue
		}
		if suho {
			fmt.Printf("%-24s %-22s %-14s %s\n", z.Osoba, z.Uloga, dosegOpis(z), z.Dionice)
			upisano++
			continue
		}
		req := service.AddDutyRequest{
			UserID: u.ID, Title: z.Naslov, Role: z.Uloga,
			SectionCodes: z.Dionice, IsPrimary: !prvo[u.Username],
		}
		if z.SektorID != "" {
			s := z.SektorID
			req.SectorID = &s
		}
		if z.Podrucje > 0 {
			p := z.Podrucje
			req.AreaID = &p
		}
		if err := svc.AddDuty(actor, req); err != nil {
			log.Printf("%s — %s: %v", z.Osoba, z.Uloga, err)
			continue
		}
		prvo[u.Username] = true
		upisano++
	}

	fmt.Printf("\nzaduženja: %d\n", upisano)
	if len(nepoznati) > 0 {
		var popis []string
		for n := range nepoznati {
			popis = append(popis, n)
		}
		sort.Strings(popis)
		fmt.Printf("nije nađeno u imeniku (%d): %s\n", len(popis), strings.Join(popis, ", "))
	}
}

func dosegOpis(z zaduzenje) string {
	if z.Podrucje > 0 {
		return fmt.Sprintf("BP %d", z.Podrucje)
	}
	return "sektor " + z.SektorID
}
