package web

// Gumbi uz članove na stranici Ovo računalo i mreža (Odluke (30. 10.
// 2026.)): „Oduzmi ovlast za primanje” prikazuje se uz svaku poznatu
// ovlast koja nije opozvana, i kad članstvo ne vrijedi ili nije zapisano;
// „Daj ovlast” samo uz važeće članstvo bez važeće ovlasti za njegov ključ. Računalo bez
// ključa mreže kaže da ovlast oduzima samo računalo s ključem mreže.

import (
	"bytes"
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/razmjena"
)

// clanZaGumbe je član za prikaz s ovlašću ili bez nje
func clanZaGumbe(id string, vrijedi bool, ovlast *peers.Primatelj, bezClanstva bool) peers.Member {
	return peers.Member{Membership: razmjena.Membership{DeviceID: id, DeviceKey: "k-" + id}, Valid: vrijedi, OvlastZaPrimanje: ovlast, BezClanstva: bezClanstva}
}

// gumbiStranice su gumbi uz članove na prikazanoj stranici
func gumbiStranice(t *testing.T, mreza *peers.Network, kljuc string, clanovi []peers.Member) string {
	t.Helper()
	var b bytes.Buffer
	data := SettingsPageData{CurrentUser: adminIvic, Permissions: &models.UserPermissions{IsGlobalAdmin: true}, Network: mreza, NodePublicKey: kljuc, Members: clanovi}
	if err := predlozakStranice(t, "settings.html").ExecuteTemplate(&b, "settings.html", data); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestGumbiOvlastiUzClanove(t *testing.T) {
	vazeca := &peers.Primatelj{Valid: true}
	istekla := &peers.Primatelj{Problem: "ovlast je istekla 1. 11. 2026."}
	clanovi := []peers.Member{
		clanZaGumbe("s-ovlascu", true, vazeca, false),
		clanZaGumbe("isteklo-clanstvo", false, vazeca, false),
		clanZaGumbe("bez-reda", false, istekla, true),
		clanZaGumbe("bez-ovlasti", true, nil, false),
		clanZaGumbe("istekla-ovlast", true, istekla, false),
		clanZaGumbe("drugi-kljuc", true, vazeca, false),
	}
	clanovi[len(clanovi)-1].OvlastDrugomKljucu = true
	html := gumbiStranice(t, &peers.Network{Name: "Probna", DrziKljuc: true, CanAdmit: true}, "k-osijek", clanovi)
	for _, c := range []struct {
		id           string
		oduzmi, daje bool
	}{
		{"s-ovlascu", true, false},
		{"isteklo-clanstvo", true, false},
		{"bez-reda", true, false},
		{"bez-ovlasti", false, true},
		{"istekla-ovlast", true, true},
		{"drugi-kljuc", true, true},
	} {
		if got := strings.Contains(html, `opozoviOvlast('`+c.id+`')`); got != c.oduzmi {
			t.Errorf("%s: Oduzmi ovlast %v, a treba %v", c.id, got, c.oduzmi)
		}
		if got := strings.Contains(html, `izdajOvlast('`+c.id+`')`); got != c.daje {
			t.Errorf("%s: Daj ovlast %v, a treba %v", c.id, got, c.daje)
		}
	}
	if !strings.Contains(html, "na ovom računalu nema njegovog valjanog članstva") {
		t.Error("član bez valjanog članstva nema objašnjenja")
	}
	if strings.Count(html, "izdana je za drugi ključ") != 1 {
		t.Error("ovlast za drugi ključ nema objašnjenja, ili ga ima član kojem ne pripada")
	}
	if !strings.Contains(html, `revokeMember('s-ovlascu', true)`) || strings.Contains(html, "Ovlast za primanje oduzima samo računalo s ključem mreže.</p>") {
		t.Error("računalo s ključem mreže: opoziv člana")
	}

	// primatelj: bez gumba za ovlast, uz napomenu da ovlast oduzima samo
	// računalo s ključem mreže
	primljeni := clanZaGumbe("primljeni", true, nil, false)
	primljeni.Primatelj = &razmjena.Ovlast{DeviceKey: "k-ured"}
	html = gumbiStranice(t, &peers.Network{Name: "Probna", CanAdmit: true}, "k-ured", []peers.Member{clanZaGumbe("s-ovlascu", true, vazeca, false), primljeni})
	if strings.Contains(html, "opozoviOvlast('") || strings.Contains(html, "izdajOvlast('") {
		t.Error("računalo bez ključa mreže nudi gumbe za ovlast")
	}
	if !strings.Contains(html, "Ovlast za primanje oduzima samo računalo s ključem mreže.") || !strings.Contains(html, `revokeMember('primljeni', false)`) {
		t.Error("računalo bez ključa mreže ne kaže tko oduzima ovlast")
	}
	if strings.Contains(html, `revokeMember('s-ovlascu'`) {
		t.Error("primatelj nudi opoziv člana kojeg nije primio")
	}
}
