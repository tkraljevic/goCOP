package models

// Opcije su općeniti prekidači programa koje uprava organizacije uključuje
// u Administraciji, uz objašnjenje što koji radi. Vrijede na svim čvorovima.
type Opcije struct {
	// BrisanjeOvjerenihAkata dopušta upravi trajno brisanje ovjerenih
	// rješenja i obavijesti, s izvornikom i dnevnikom slanja. Za testno
	// okruženje; u operativnom radu ostaje isključeno, jer ovjeren akt
	// ima pravni učinak i ostaje u evidenciji.
	BrisanjeOvjerenihAkata bool `json:"brisanje_ovjerenih_akata"`
	// BrisanjePovijestiVerzija dopušta u Održavanju baze brisanje cijele
	// povijesti izmjena odmah (bez roka) i spomenika obrisanih zapisa.
	// Za čišćenje testnog čvora; u radu ostaje isključeno.
	BrisanjePovijestiVerzija bool `json:"brisanje_povijesti_verzija"`
	// BrisanjeSOglasnePloce dopušta upravi organizacije brisanje pojedinog
	// zapisa s oglasne ploče (događanja), tj. te verzije iz knjige.
	BrisanjeSOglasnePloce bool `json:"brisanje_s_oglasne_ploce"`
	// UpisTudjimOcima dopušta administratoru da, dok gleda program očima
	// drugog djelatnika, i upisuje u njegovo ime (npr. dnevni list
	// vodočuvara). Za testiranje i pomoć; zapis nastaje pod tuđim imenom.
	UpisTudjimOcima bool `json:"upis_tudjim_ocima"`
	// SimulacijaKljuca dopušta da se, dok se gleda tuđim očima, dokumenti
	// potpisuju simuliranim ključem te osobe, bez lozinke: certifikat i
	// potpis nose oznaku SIMULACIJA, a PDF pečat BEZVRIJEDNO. Samo za
	// testiranje toka potpisivanja.
	SimulacijaKljuca bool `json:"simulacija_kljuca"`
}
