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
	// CuvanjeSlikaDana je koliko se dana nakon objave čuvaju izvorne
	// fotografije uz prijave s terena; potpisani PDF ih nosi trajno.
	// 0 znači zadanih 180.
	CuvanjeSlikaDana int `json:"cuvanje_slika_dana,omitempty"`
	// SlikeOdmah briše izvorne fotografije odmah po objavi prijave (PDF ih
	// nosi), umjesto nakon roka; štedi prostor, a stranica prijave slike
	// pokazuje iz PDF-a
	SlikeOdmah bool `json:"slike_odmah,omitempty"`
	// PrognozaGraf, PrognozaTablica i PrognozaExcel biraju koji model daje
	// dane prognoze na uzdužnom profilu, u tablici i u izvozu: prazno je
	// kombinacija (dnevni model od dana koji je odredila provjera na
	// valovima), "satni" satni lanac dokle seže, "dnevni" dnevni model od
	// prvog dana gdje ga letva ima.
	PrognozaGraf    string `json:"prognoza_graf,omitempty"`
	PrognozaTablica string `json:"prognoza_tablica,omitempty"`
	PrognozaExcel   string `json:"prognoza_excel,omitempty"`
}

// Načini prikaza prognoze.
const (
	PrognozaKombinacija = ""
	PrognozaSatni       = "satni"
	PrognozaDnevni      = "dnevni"
)

// OpisNacinaPrognoze je kratak opis načina za stranicu.
func OpisNacinaPrognoze(n string) string {
	switch n {
	case PrognozaSatni:
		return "satni lanac dokle seže (96 h), dalje dnevni model"
	case PrognozaDnevni:
		return "dnevni model od prvog dana, satni samo gdje dnevnog nema"
	}
	return "kombinacija: satni lanac, a dnevni model (d) od dana koji je provjera odredila za svaku letvu i vodostaj"
}

// CuvanjeSlika vraća rok čuvanja fotografija u danima
func (o Opcije) CuvanjeSlika() int {
	if o.CuvanjeSlikaDana <= 0 {
		return ZadanoCuvanjeSlikaDana
	}
	return o.CuvanjeSlikaDana
}
