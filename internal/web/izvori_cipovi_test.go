package web

import (
	"strings"
	"testing"

	"gocop/internal/arhiva"
	"gocop/internal/models"
)

// Čipovi filtra Izvora arhive: svi, po skupini povjerenja, isključeni i
// oni čiji se podaci ne vide, svaki s brojem; prazne skupine nemaju čipa
// (Odluke (18. 11. 2026.), Administracija, novo ponašanje 11)
func TestCipoviIzvoraArhive(t *testing.T) {
	izvor := func(naziv string, red int, ukljucen bool, nizova int) IzvorURedu {
		return IzvorURedu{Izvor: arhiva.Izvor{Naziv: naziv, Red: red, Ukljucen: ukljucen}, Nizova: nizova}
	}
	d := IzvoriPageData{CurrentUser: adminIvic, Permissions: &models.UserPermissions{IsGlobalAdmin: true}, Izvori: []IzvorURedu{
		izvor("his2000", 5, true, 3), izvor("cop", 15, true, 2), izvor("vodostaji-voda-hr", 30, true, 9),
		izvor("hydroview", 31, false, 4), izvor("stari", 32, false, 0),
	}}
	var got []string
	for _, c := range d.Cipovi() {
		got = append(got, c.Naziv+" "+itoa(c.Broj))
	}
	if strings.Join(got, ", ") != "Svi 5, Ovjereno 1, S letve 1, Operativno 3, Isključeni 2, Ne vidi se 1" {
		t.Errorf("čipovi: %q", got)
	}

	var b strings.Builder
	if err := predlozakStranice(t, "izvori.html").ExecuteTemplate(&b, "base.html", d); err != nil {
		t.Fatal(err)
	}
	html := b.String()
	for _, mora := range []string{`value="sve" checked><span>Svi <b>5</b>`, `value="nevidi"><span>Ne vidi se <b>1</b>`,
		`data-skupina="operativno" data-iskljucen data-nevidi>`, `data-skupina="ovjereno">`, "k.hasAttribute('data-nevidi')"} {
		if !strings.Contains(html, mora) {
			t.Errorf("stranica nema %q", mora)
		}
	}
}
