package service

import (
	"regexp"
	"strings"

	"gocop/internal/models"
)

// U prijepisu se ne zna tko je koji zapis upisao — uvez nema potpisa uz
// redak. Zna se tko je dežurao: zapisi "Dežurstvo preuzeo X" i "Dežurstvo
// završio X" nose ime. Onaj tko dežura upisuje, pa se svaki zapis između
// preuzimanja i predaje pripisuje dežurnom. To je zaključak, ne podatak iz
// uveza, i tako se i označava.

var (
	rePreuzeo = regexp.MustCompile(`(?i)preuz|dežurstvo\s+\d{1,2}[:.]\d{2}\s*[–-]|dežurstvo od`)
	reZavrsio = regexp.MustCompile(`(?i)završ|preda[ol]|kraj dežurstva`)
)

// PripisiUpisivace popunjava UpisaoPoDezurstvu zapisima bez upisivača:
// naprijed od svakog preuzimanja do predaje, pa natrag od svake predaje bez
// preuzimanja do prethodne predaje — jer se u uvezu često bilježio samo kraj.
func PripisiUpisivace(zapisi []models.JournalEntry) {
	dezurni := ""
	for i := range zapisi {
		z := &zapisi[i]
		if z.Kind == models.EntryKindDuty {
			ime := strings.TrimSpace(z.ReportedBy)
			switch {
			case rePreuzeo.MatchString(z.Text) && ime != "":
				dezurni = ime
				z.UpisaoPoDezurstvu = ime
			case reZavrsio.MatchString(z.Text):
				if ime != "" {
					z.UpisaoPoDezurstvu = ime
				} else if dezurni != "" {
					z.UpisaoPoDezurstvu = dezurni
				}
				dezurni = ""
			default:
				if ime != "" {
					z.UpisaoPoDezurstvu = ime
				}
			}
			continue
		}
		if z.UserName == "" && dezurni != "" {
			z.UpisaoPoDezurstvu = dezurni
		}
	}
	// natrag: predaja bez preuzimanja pokriva zapise prije sebe, do prethodne predaje
	dezurni = ""
	for i := len(zapisi) - 1; i >= 0; i-- {
		z := &zapisi[i]
		if z.Kind == models.EntryKindDuty {
			ime := strings.TrimSpace(z.ReportedBy)
			if reZavrsio.MatchString(z.Text) && ime != "" && !rePreuzeo.MatchString(z.Text) {
				dezurni = ime
			} else {
				dezurni = ""
			}
			continue
		}
		if z.UserName == "" && z.UpisaoPoDezurstvu == "" && dezurni != "" {
			z.UpisaoPoDezurstvu = dezurni
		}
	}
}
