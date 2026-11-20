package web

import (
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/razmjena"
)

// Stanje razmjene piše razlog puta istim rečenicama kao pločica razmjene:
// skripta dobiva rečenice i fraze iz put_natpis.go, pa se ne mogu razići
// (Odluke (18. 11. 2026.), Administracija, novo ponašanje 8)
func TestRecenicePutaZaStanjeRazmjene(t *testing.T) {
	r := recenicePuta()
	if len(r) != len(recenicaRazloga) || r[string(razmjena.RazlogPutaNijeSeJavio)] != recenicaRazloga[razmjena.RazlogPutaNijeSeJavio] {
		t.Fatalf("rečenice: %v", r)
	}
	f := frazePuta()
	if got := strings.Replace(r[string(razmjena.RazlogPutaNijeSeJavio)], "%s", f[razmjena.NazivWeb], 1); got != razlogRijecima(razmjena.RazlogPutaNijeSeJavio, razmjena.NazivWeb) {
		t.Errorf("skripta i pločica govore drukčije: %q", got)
	}

	var b strings.Builder
	if err := predlozakStranice(t, "sinkronizacija.html").ExecuteTemplate(&b, "base.html", SyncPageData{CurrentUser: adminIvic, Permissions: &models.UserPermissions{IsGlobalAdmin: true}}); err != nil {
		t.Fatal(err)
	}
	html := b.String()
	for _, mora := range []string{"<th>Uloge</th>", `const RECENICE_PUTA = {`, `"nije-se-javio":"Računalo se ne javlja u lokalnoj mreži; podaci idu %s."`, `href="/uparivanje?adresa=`, "<th>Karta bez interneta</th>", "karta(s.karta, st.karta)", ">druga karta</span>"} {
		if !strings.Contains(html, mora) {
			t.Errorf("stranica nema %q", mora)
		}
	}
	if strings.Contains(html, "/settings?upari=") {
		t.Error("uparivanje i dalje vodi u postavke")
	}
}
