// upis-djelatnika upisuje djelatnike iz imenika wikija kroz servisni sloj, da
// prođu istu provjeru kao unos rukom i ostave zapis u knjizi verzija.
// Zaduženja se dodjeljuju posebno, iz rasporeda.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

type osoba struct {
	Ime        string
	Titula     string
	Pripadnost string
	Telefon    string
	Mobitel    string
	Skraceni   string
	Email      string
	Jedinica   string // odjeljak imenika iz kojeg je redak
	Korisnicko string
}

func main() {
	dbPath := flag.String("db", "data/gocop.db", "putanja do baze")
	nodeID := flag.String("node", "cop-osijek-node", "oznaka čvora")
	imenik := flag.String("imenik", "", "datoteka imenika iz wikija")
	raspored := flag.String("raspored", "", "datoteka rasporeda iz wikija; dodjeljuje zaduženja upisanim djelatnicima")
	sektor := flag.String("sektor", "B", "oznaka sektora za doseg zaduženja")
	lozinka := flag.String("lozinka", "gocop2026", "početna lozinka; program svejedno traži promjenu pri prvoj prijavi")
	suho := flag.Bool("probno", false, "samo ispiši što bi se upisalo")
	flag.Parse()

	if *raspored != "" {
		dodjelaZaduzenja(*dbPath, *nodeID, *raspored, *sektor, *suho)
		return
	}

	raw, err := os.ReadFile(*imenik)
	if err != nil {
		log.Fatal(err)
	}
	ljudi := citajImenik(string(raw))
	dodijeliKorisnicka(ljudi)

	if *suho {
		for _, o := range ljudi {
			fmt.Printf("%-14s %-24s %-22s %-14s %-8s %s\n", o.Korisnicko, o.Ime, o.Titula, o.Mobitel, o.Skraceni, o.Jedinica)
		}
		fmt.Printf("\nosoba: %d\n", len(ljudi))
		return
	}

	database, err := db.OpenDB(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()
	rec := ledger.New(database, *nodeID)
	uRepo := repository.NewUserRepository(database, rec)
	sRepo := repository.NewSessionRepository(database)
	svc := service.NewUserService(uRepo, service.NewAuthService(uRepo, sRepo), service.NewSSEBroker())

	admin, err := uRepo.GetUserByUsername("admin")
	if err != nil || admin == nil {
		log.Fatalf("račun admin nije nađen: %v", err)
	}
	actor := &models.UserPermissions{User: *admin, IsGlobalAdmin: true}

	upisano, preskoceno := 0, 0
	for _, o := range ljudi {
		if postoji, _ := uRepo.GetUserByUsername(o.Korisnicko); postoji != nil {
			preskoceno++
			continue
		}
		_, err := svc.CreateUser(actor, service.CreateUserRequest{
			Username: o.Korisnicko, Password: *lozinka, FullName: o.Ime, Title: o.Titula,
			OrgType: models.OrgHrvatskeVode, OrgName: o.Pripadnost,
			Phone: o.Telefon, MobilePhone: o.Mobitel, ShortMobile: o.Skraceni, Email: o.Email,
		})
		if err != nil {
			log.Printf("%s (%s): %v", o.Ime, o.Korisnicko, err)
			continue
		}
		upisano++
	}
	fmt.Printf("upisano %d, preskočeno %d (već postoje)\n", upisano, preskoceno)
}

var (
	reOdjeljak     = regexp.MustCompile(`^##\s+(.*)$`)
	reTitulaIspred = regexp.MustCompile(`^(mr\.sc\.|dr\.sc\.|prof\.|dipl\.|mag\.)\s+`)
)

// citajImenik čita tablice imenika. Redci koji nisu osobe — ispostave i firme
// upisane u istom stupcu — preskaču se: račun se otvara čovjeku, ne uredu.
func citajImenik(tekst string) []osoba {
	var out []osoba
	odjeljak := ""
	for _, line := range strings.Split(tekst, "\n") {
		if m := reOdjeljak.FindStringSubmatch(line); m != nil {
			odjeljak = strings.TrimSpace(m[1])
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(line), "|") || odjeljak == "" {
			continue
		}
		c := stupci(line)
		if len(c) < 6 || c[0] == "Osoba" || strings.Trim(c[0], "- ") == "" {
			continue
		}
		if !osobno(c[0]) {
			continue
		}
		tel, mob := telefoni(c[3])
		// izvor ponegdje u titulu prepiše i ustanovu ("dipl.ing.građ. Hrvatske
		// vode"); pripadnost ima svoj stupac, pa u tituli nema što tražiti
		titula := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(c[1]), "Hrvatske vode"))
		out = append(out, osoba{
			Ime: reTitulaIspred.ReplaceAllString(c[0], ""), Titula: titula, Pripadnost: c[2],
			Telefon: tel, Mobitel: mob, Skraceni: c[4], Email: c[5], Jedinica: odjeljak,
		})
	}
	return out
}

func stupci(line string) []string {
	dijelovi := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
	out := make([]string, 0, len(dijelovi))
	for _, d := range dijelovi {
		out = append(out, strings.TrimSpace(d))
	}
	return out
}

// osobno razlikuje čovjeka od ustanove ili firme upisane u stupac osobe
func osobno(ime string) bool {
	l := strings.ToLower(ime)
	for _, znak := range []string{"d.o.o", "d.d.", "j.d.o.o", "vgi ", "vgo ", "hrvatske vode", "centar obrane", "podcentar"} {
		if strings.Contains(l, znak) {
			return false
		}
	}
	return len(strings.Fields(ime)) >= 2
}

// telefoni razvrstava zapisane brojeve: 09x je mobitel, ostalo je fiksni
func telefoni(zapis string) (fiksni, mobitel string) {
	for _, b := range strings.Split(zapis, ",") {
		b = strings.TrimSpace(b)
		if b == "" {
			continue
		}
		if strings.HasPrefix(b, "09") {
			mobitel = b
		} else {
			fiksni = b
		}
	}
	return fiksni, mobitel
}

// dodijeliKorisnicka slaže korisničko ime kao inicijal i prezime. Podloga je
// e-pošta kad je ima, jer je ondje ime već bez kvačica i crtica
// (josipa.zekopivac → jzekopivac); inače se prepisuje iz imena.
func dodijeliKorisnicka(ljudi []osoba) {
	zauzeto := map[string]bool{}
	for i := range ljudi {
		o := &ljudi[i]
		ime, prezime := "", ""
		if lokalni, _, ok := strings.Cut(o.Email, "@"); ok && strings.Contains(lokalni, ".") {
			ime, prezime, _ = strings.Cut(lokalni, ".")
		} else {
			polja := strings.Fields(o.Ime)
			ime, prezime = polja[0], polja[len(polja)-1]
		}
		kandidat := preslovi(string([]rune(ime)[0]) + prezime)
		osnovni := kandidat
		for n := 2; zauzeto[kandidat]; n++ {
			kandidat = fmt.Sprintf("%s%d", osnovni, n)
		}
		zauzeto[kandidat] = true
		o.Korisnicko = kandidat
	}
}

// preslovi svodi naziv na mala slova bez kvačica i crtica, kakvo korisničko
// ime traži prijava
func preslovi(s string) string {
	zamjene := strings.NewReplacer(
		"č", "c", "ć", "c", "đ", "d", "š", "s", "ž", "z",
		"Č", "c", "Ć", "c", "Đ", "d", "Š", "s", "Ž", "z",
		"-", "", " ", "", ".", "",
	)
	return strings.ToLower(zamjene.Replace(s))
}
