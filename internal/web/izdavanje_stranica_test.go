package web

import (
	"strings"
	"testing"
	"time"

	"gocop/internal/arhiva"
	"gocop/internal/models"
)

func vrataZaTest() UvozPageData {
	return UvozPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		PodaciDir:   "vodostaji",
	}
}

// Dok posao traje, stranica mora nositi njegov broj — bez toga preglednik ne
// zna koga pitati i traka stoji prazna.
func TestVrataCrtajuTrakuDokPosaoTraje(t *testing.T) {
	d := vrataZaTest()
	d.PosaoID, d.PosaoNaziv = "abc-1", "Izgradnja letve vukovar"
	html := iscrtaj(t, "uvoz_niza.html", d)
	for _, want := range []string{`data-posao="abc-1"`, "posao-traka-crta", "posao-dnevnik",
		"Izgradnja letve vukovar"} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica nema %q", want)
		}
	}
	// Dok posao traje ne nudi se novo izdavanje: arhiva se upravo mijenja.
	if strings.Contains(html, "/administracija/izdavanje") {
		t.Error("gumb za izdavanje se nudi dok gradnja još traje")
	}
}

// Prijedlog izdanja mora reći i staro i novo izdanje i otisak — inače čovjek
// ne vidi zašto bi broj uopće skočio.
func TestPrijedlogIzdanjaPokazujeSkokIOtisak(t *testing.T) {
	d := vrataZaTest()
	d.IzdavanjeRadi, d.PaketiDir = true, "pakete"
	d.IzdanjeLetva = "vukovar"
	d.Izdanja = &arhiva.IzvjestajIzdanja{
		Probno: true, Mapa: "pakete", Promijenjenih: 1,
		Redci: []arhiva.RedIzdanja{{Letva: "vukovar", Prije: 2, Izdanje: 3, Novo: true,
			Otisak: "b2ed34cc", Zapisa: 808495, Od: "1900-01-01", Do: "2026-09-11"}},
	}
	html := iscrtaj(t, "uvoz_niza.html", d)
	for _, want := range []string{"v2 → v3", "b2ed34cc", "808.495", "Izdaj vukovar",
		`action="/administracija/izdavanje"`} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica nema %q", want)
		}
	}
}

// Kad se ništa nije promijenilo, gumb za izdavanje te letve se ne nudi: izdanje
// bez promjene sadržaja je isti paket pod novim brojem.
func TestBezPromjeneNemaGumbaZaIzdavanje(t *testing.T) {
	d := vrataZaTest()
	d.IzdavanjeRadi, d.PaketiDir = true, "pakete"
	d.IzdanjeLetva = "vukovar"
	d.Izdanja = &arhiva.IzvjestajIzdanja{
		Probno: true, Mapa: "pakete", Istih: 1,
		Redci: []arhiva.RedIzdanja{{Letva: "vukovar", Prije: 2, Izdanje: 2, Otisak: "a7036846"}},
	}
	html := iscrtaj(t, "uvoz_niza.html", d)
	if !strings.Contains(html, "v2 nepromijenjeno") {
		t.Error("stranica ne kaže da je nepromijenjeno")
	}
	if strings.Contains(html, `action="/administracija/izdavanje"`) {
		t.Error("nudi se izdavanje iako se ništa nije promijenilo")
	}
}

// Čvor bez mape za izdavanje ne smije nuditi izdavanje; on arhivu prima, ne daje.
func TestCvorKojiNeIzdajeNemaOdjeljak(t *testing.T) {
	d := vrataZaTest()
	html := iscrtaj(t, "uvoz_niza.html", d)
	if strings.Contains(html, "Izdanja arhive") {
		t.Error("čvor koji ne izdaje ipak nudi izdavanje")
	}
}

// Bez prijedloga se pokazuje što katalog već ima, da se vidi stanje prije klika.
func TestKatalogSeVidiIBezProvjere(t *testing.T) {
	d := vrataZaTest()
	d.IzdavanjeRadi, d.PaketiDir = true, "pakete"
	d.Katalog = arhiva.Katalog{Inacica: 2, Izdao: "cop-osijek-node",
		Nastalo: time.Date(2026, 9, 11, 9, 30, 0, 0, time.UTC),
		Paketi:  []arhiva.UPaketu{{Letva: "vukovar", Izdanje: 2}, {Letva: "batina", Izdanje: 2}}}
	d.KatalogNastalo = "11.09.2026. 11:30"
	html := iscrtaj(t, "uvoz_niza.html", d)
	for _, want := range []string{"Izdanja arhive", "cop-osijek-node", "11.09.2026. 11:30", "pakete"} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica nema %q", want)
		}
	}
}
