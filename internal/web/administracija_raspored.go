package web

// Raspored ulazne stranice Administracije (Odluke (30. 10. 2026.), 1 i 2):
// kartice stoje u šest skupina, iznad njih su Prvi koraci dok ustroj nije
// gotov, a uz karticu piše vrijedi li ono što se na njoj mijenja samo na ovom
// računalu, u cijeloj mreži ili oboje. Kartica se ne prikazuje onome tko
// njezinu stranicu ne smije otvoriti, a skupina bez ijedne vidljive kartice
// se ne prikazuje.

import (
	"context"
	"database/sql"

	"gocop/internal/models"
)

// DosegKartice kaže gdje vrijedi ono što se na kartici mijenja. Oznaka stoji
// samo gdje to sigurno slijedi iz spremanja: gocop.toml i lokalne tablice su
// ovo računalo, zapis s verzijom u knjizi je cijela mreža.
type DosegKartice string

const (
	DosegRacunalo DosegKartice = "ovo računalo"
	DosegMreza    DosegKartice = "cijela mreža"
	DosegOboje    DosegKartice = "oboje"
)

// Opis je objašnjenje oznake, za natpis uz nju
func (d DosegKartice) Opis() string {
	switch d {
	case DosegRacunalo:
		return "Vrijedi samo na ovom računalu i ne ide razmjenom."
	case DosegMreza:
		return "Vrijedi na svim računalima u mreži; promjena ide razmjenom."
	case DosegOboje:
		return "Dio vrijedi samo na ovom računalu, a dio na svim računalima u mreži."
	}
	return ""
}

// pravaKartice su prava i vidljivi moduli onoga tko gleda stranicu
type pravaKartice struct {
	perms  *models.UserPermissions
	moduli models.Visibility
}

func (p pravaKartice) admin() bool { return p.perms != nil && p.perms.IsGlobalAdmin }

func samoAdministrator(p pravaKartice) bool { return p.admin() }

// svakome: stranica je pod modulom Administracija, a njega ima svatko tko
// je otvorio ovu stranicu; što smije samo administrator, stranica skriva sama
func svakome(pravaKartice) bool { return true }

func uzModul(modul string) func(pravaKartice) bool {
	return func(p pravaKartice) bool { return p.admin() || p.moduli.Sees(modul) }
}

// upraviSektora: žig mijenja uprava sektora na stranici sektora, a kartica
// je vodi na njezin sektor (Odluke (30. 10. 2026.), 4)
func upraviSektora(p pravaKartice) bool {
	return p.admin() || p.perms != nil && len(p.perms.AdminSectors) > 0
}

// dezurnomIliAdministratoru: Postavke prognoze otvara onaj tko prognozu
// smije generirati (Odluke (30. 10. 2026.), 7)
func dezurnomIliAdministratoru(p pravaKartice) bool { return p.perms.SmijeGeneriratiPrognozu() }

// karticaAdministracije je jedna kartica: ključ (isti u predlošku), skupina,
// gdje vrijedi i tko je vidi
type karticaAdministracije struct {
	kljuc, skupina string
	doseg          DosegKartice
	vidi           func(pravaKartice) bool
}

// Skupine, redom kako stoje na stranici (Odluke (30. 10. 2026.), 1)
const (
	skupinaUstroj    = "ustroj"
	skupinaMreza     = "mreza"
	skupinaVodostaji = "vodostaji"
	skupinaAkti      = "akti"
	skupinaIzgled    = "izgled"
	skupinaTestni    = "testni"
)

// karticeAdministracije: raspored iz analize administracije (odjeljak 2),
// sa žigom na stranici sektora i Uvozima u Prvim koracima
var karticeAdministracije = []karticaAdministracije{
	{"organizacija", skupinaUstroj, DosegMreza, uzModul(models.ModuleRegisters)},
	{"djelatnici", skupinaUstroj, DosegMreza, uzModul(models.ModuleUsers)},
	{"sudionici", skupinaUstroj, DosegMreza, samoAdministrator},
	{"moduli", skupinaUstroj, DosegMreza, samoAdministrator},
	{"nazivi", skupinaUstroj, DosegMreza, samoAdministrator},

	{"racunalo", skupinaMreza, DosegOboje, svakome},
	{"razmjena", skupinaMreza, "", svakome},
	{"pretplate", skupinaMreza, DosegRacunalo, svakome},
	{"baza", skupinaMreza, DosegOboje, svakome},

	{"telemetrija", skupinaVodostaji, DosegRacunalo, samoAdministrator},
	{"izvori", skupinaVodostaji, DosegRacunalo, samoAdministrator},
	{"unos", skupinaVodostaji, "", samoAdministrator},
	{"prognoza", skupinaVodostaji, "", dezurnomIliAdministratoru},

	{"posta", skupinaAkti, DosegOboje, samoAdministrator},
	{"zig", skupinaAkti, DosegMreza, upraviSektora},
	{"potpisi", skupinaAkti, DosegOboje, samoAdministrator},
	{"obracun", skupinaAkti, DosegMreza, samoAdministrator},

	{"tema", skupinaIzgled, DosegMreza, samoAdministrator},
	{"karte", skupinaIzgled, DosegOboje, samoAdministrator},

	{"opcije", skupinaTestni, DosegMreza, samoAdministrator},
}

// rasporedKartica puni što ovaj korisnik vidi, koje skupine imaju što
// pokazati i oznake dosega. Uvozi podataka su u Prvim koracima; kad njih
// nema, administrator ih vidi kao poveznicu u skupini Ustroj i ljudi.
func rasporedKartica(p pravaKartice, prviKoraci bool) (vidi, skupine map[string]bool, doseg map[string]DosegKartice) {
	vidi, skupine, doseg = map[string]bool{}, map[string]bool{}, map[string]DosegKartice{}
	for _, k := range karticeAdministracije {
		if k.doseg != "" {
			doseg[k.kljuc] = k.doseg
		}
		if k.vidi(p) {
			vidi[k.kljuc], skupine[k.skupina] = true, true
		}
	}
	if p.admin() && !prviKoraci {
		vidi["uvozi"], skupine[skupinaUstroj] = true, true
	}
	return vidi, skupine, doseg
}

// KorakUstroja je stavka Prvih koraka. Zna kaže da se gotovost da utvrditi
// iz podataka; bez toga stavka stoji bez kvačice.
type KorakUstroja struct {
	Naslov, Href, Opis string
	Zna, Gotov         bool
}

// stanjeUstroja je ono što je za Prve korake pročitano iz postojećih podataka
type stanjeUstroja struct {
	nazivi, sektori, djelatnici, uparen bool
	telemetrija, posta                  *bool // nil kad se ne da utvrditi
}

// poznato vraća (zna, gotov) za stavku čija se gotovost možda ne da utvrditi
func poznato(b *bool) (bool, bool) {
	if b == nil {
		return false, false
	}
	return true, *b
}

// prviKoraci su koraci redom kojim se postavlja novi čvor (analiza
// administracije, skupina 0; Uvozi podataka po Odlukama (30. 10. 2026.), 2)
func prviKoraci(s stanjeUstroja) []KorakUstroja {
	telZna, tel := poznato(s.telemetrija)
	postaZna, posta := poznato(s.posta)
	return []KorakUstroja{
		{Naslov: "Nazivi", Href: "/administracija/nazivi", Opis: "Naziv organizacije, znak i riječi za razine ustroja. Kvačica stoji kad su barem jednom spremljeni.", Zna: true, Gotov: s.nazivi},
		{Naslov: "Administrativna organizacija", Href: "/organizacija", Opis: "Sektori s centrima obrane i branjena područja.", Zna: true, Gotov: s.sektori},
		{Naslov: "Djelatnici ili uvoz imenika", Href: "/users", Opis: "Računi za ljude koji rade u programu, upisani rukom ili uvezeni iz imenika.", Zna: true, Gotov: s.djelatnici},
		{Naslov: "Uvozi podataka", Href: "/administracija/uvozi", Opis: "Registri, imenik, ugovori i tablice vodostaja iz ranijih evidencija. Treba samo prvom računalu u mreži."},
		{Naslov: "Uloge ovog računala", Href: "/settings#uloge", Opis: "Preuzima li ovo računalo vodostaje s izvora i izdaje li prognozu."},
		{Naslov: "Telemetrija", Href: "/administracija/telemetrija", Opis: "Računi za HydroView i mobilnu stranicu Hrvatskih voda.", Zna: telZna, Gotov: tel},
		{Naslov: "E-pošta", Href: "/administracija/posta", Opis: "Poslužitelj preko kojeg se šalju akti i PIN za prijavu izvana.", Zna: postaZna, Gotov: posta},
		{Naslov: "Uparivanje", Href: "/uparivanje", Opis: "Drugo računalo u istoj mreži, da podaci imaju gdje ići razmjenom.", Zna: true, Gotov: s.uparen},
	}
}

// ustrojGotov: svaka stavka čija se gotovost da utvrditi je gotova
func ustrojGotov(koraci []KorakUstroja) bool {
	for _, k := range koraci {
		if k.Zna && !k.Gotov {
			return false
		}
	}
	return true
}

// ukljucenihTestnih broji uključene prekidače sa stranice Opcije
func ukljucenihTestnih(o models.Opcije) int {
	n := 0
	for _, b := range []bool{o.BrisanjeOvjerenihAkata, o.BrisanjePovijestiVerzija, o.BrisanjeSOglasnePloce, o.UpisTudjimOcima, o.SimulacijaKljuca} {
		if b {
			n++
		}
	}
	return n
}

// imaRacunTelemetrije javlja je li na ovom računalu upisan barem jedan račun
// telemetrije (HydroView ili račun sustava)
func imaRacunTelemetrije(ctx context.Context, db *sql.DB) bool {
	if db == nil {
		return false
	}
	var ima bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM racuni_sustava) OR EXISTS(SELECT 1 FROM hidroview_racuni)`).Scan(&ima)
	return err == nil && ima
}

// izvoriStanja su podaci izvan servisa ulazne stranice; svaki smije biti nil
type izvoriStanja struct {
	opcije      func(context.Context) models.Opcije
	telemetrija func(context.Context) bool
	posta       func(context.Context) bool
}

// SetIzvoriStanja spaja ulaznu stranicu s opcijama, telemetrijom i e-poštom
func (h *AdminHandler) SetIzvoriStanja(opcije func(context.Context) models.Opcije, telemetrija, posta func(context.Context) bool) {
	h.izvori = izvoriStanja{opcije: opcije, telemetrija: telemetrija, posta: posta}
}

// izvorStanja vraća vrijednost izvora; nil kad izvora nema
func izvorStanja(ctx context.Context, f func(context.Context) bool) *bool {
	if f == nil {
		return nil
	}
	v := f(ctx)
	return &v
}
