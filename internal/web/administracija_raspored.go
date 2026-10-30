package web

// Raspored ulazne stranice Administracije (Odluke (30. 10. 2026.), 1 i 2):
// kartice stoje u šest skupina, iznad njih su Prvi koraci dok ustroj nije
// gotov, a uz karticu piše vrijedi li ono što se na njoj mijenja samo na ovom
// računalu, u cijeloj mreži ili dio ovdje, a dio u mreži. Kartica se ne prikazuje onome tko
// njezinu stranicu ne smije otvoriti, a skupina bez ijedne vidljive kartice
// se ne prikazuje.

import (
	"context"
	"database/sql"
	"errors"
	"log"

	"gocop/internal/mletva"
	"gocop/internal/models"
)

// DosegKartice kaže gdje vrijedi ono što se na kartici mijenja. Oznaka stoji
// samo gdje to sigurno slijedi iz spremanja: gocop.toml i lokalne tablice su
// ovo računalo, zapis s verzijom u knjizi je cijela mreža.
type DosegKartice string

const (
	DosegRacunalo DosegKartice = "ovo računalo"
	DosegMreza    DosegKartice = "cijela mreža"
	DosegOboje    DosegKartice = "dio ovdje, dio u mreži"
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
// je vodi na njezin sektor (Odluke (30. 10. 2026.), 4). Stranica sektora je
// pod modulom Registri, pa bez njega kartica vodi na 403.
func upraviSektora(p pravaKartice) bool {
	return p.admin() || p.perms != nil && len(p.perms.AdminSectors) > 0 && p.moduli.Sees(models.ModuleRegisters)
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
	// način prikaza je u Opcijama, istom zapisu u knjizi kao kartica Opcije
	{"prognoza", skupinaVodostaji, DosegMreza, dezurnomIliAdministratoru},

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

// stanjeUstroja je ono što je za Prve korake pročitano iz postojećih
// podataka; nil kad se ne zna: izvora nema, čitanje nije uspjelo ili korak
// ovom računalu ne treba
type stanjeUstroja struct {
	nazivi, sektori, djelatnici, uparen, telemetrija, posta *bool
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
	korak := func(naslov, href, opis string, b *bool) KorakUstroja {
		zna, gotov := poznato(b)
		return KorakUstroja{Naslov: naslov, Href: href, Opis: opis, Zna: zna, Gotov: gotov}
	}
	return []KorakUstroja{
		korak("Nazivi", "/administracija/nazivi", "Naziv organizacije, znak i riječi za razine ustroja. Kvačica stoji kad su nazivi ili nazivi sudionika obrane barem jednom spremljeni.", s.nazivi),
		korak("Administrativna organizacija", "/organizacija", "Sektori s centrima obrane i branjena područja.", s.sektori),
		korak("Djelatnici ili uvoz imenika", "/users", "Računi za ljude koji rade u programu, upisani rukom ili uvezeni iz imenika.", s.djelatnici),
		korak("Uvozi podataka", "/administracija/uvozi", "Registri, imenik, ugovori i tablice vodostaja iz ranijih evidencija. Treba samo prvom računalu u mreži.", nil),
		korak("Uloge ovog čvora", "/settings#uloge", "Preuzima li ovo računalo vodostaje s izvora i izdaje li prognozu.", nil),
		korak("Telemetrija", "/administracija/telemetrija", "Računi za HydroView i mobilnu stranicu Hrvatskih voda. Trebaju samo računalu koje preuzima vodostaje s izvora.", s.telemetrija),
		korak("E-pošta", "/administracija/posta", "Poslužitelj preko kojeg se šalju akti i PIN za prijavu izvana.", s.posta),
		korak("Uparivanje", "/settings#uparivanje", "Drugo računalo uparite s ovim, da podaci imaju kamo ići razmjenom. Kvačica stoji kad ovo računalo zna barem jedno drugo.", s.uparen),
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

var errBezBaze = errors.New("baza nije otvorena")

// imaRacunTelemetrije javlja je li na ovom računalu upisan barem jedan račun
// telemetrije: HydroView ili račun za mobilnu stranicu Hrvatskih voda. U
// istoj tablici je i račun koji šalje PIN, a on se ne broji.
func imaRacunTelemetrije(ctx context.Context, db *sql.DB) (bool, error) {
	if db == nil {
		return false, errBezBaze
	}
	var ima bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM racuni_sustava WHERE sustav = ?) OR EXISTS(SELECT 1 FROM hidroview_racuni)`,
		mletva.Podrijetlo).Scan(&ima)
	return ima, err
}

// izvoriStanja su podaci izvan servisa ulazne stranice; svaki smije biti nil
type izvoriStanja struct {
	opcije             func(context.Context) models.Opcije
	telemetrija, posta func(context.Context) (bool, error)
}

// SetIzvoriStanja spaja ulaznu stranicu s opcijama, telemetrijom i e-poštom
func (h *AdminHandler) SetIzvoriStanja(opcije func(context.Context) models.Opcije, telemetrija, posta func(context.Context) (bool, error)) {
	h.izvori = izvoriStanja{opcije: opcije, telemetrija: telemetrija, posta: posta}
}

// izvorStanja vraća vrijednost izvora; nil kad izvora nema ili se nije dao
// pročitati
func izvorStanja(ctx context.Context, naziv string, f func(context.Context) (bool, error)) *bool {
	if f == nil {
		return nil
	}
	v, err := f(ctx)
	return procitano(naziv, v, err)
}

// procitano je vrijednost za Prve korake; kad čitanje nije uspjelo, korak
// ostaje bez kvačice i ne drži popis otvorenim, a greška ide u dnevnik
func procitano(naziv string, v bool, err error) *bool {
	if err != nil {
		log.Printf("administracija: za Prve korake se ne zna stanje (%s): %v", naziv, err)
		return nil
	}
	return &v
}

// greskeUstroja su čitanja ulazne stranice koja nisu uspjela, za Prve korake
type greskeUstroja struct{ sektori, racuni, razmjena error }

// stanjeUstroja čita ono po čemu se vidi koji su Prvi koraci gotovi.
// Telemetrija se gleda samo na računalu koje preuzima vodostaje s izvora;
// ostalima računi ne trebaju.
func (h *AdminHandler) stanjeUstroja(ctx context.Context, data *AdminPageData, g greskeUstroja) stanjeUstroja {
	t, err := h.org.Terms(ctx)
	s := stanjeUstroja{
		nazivi:     procitano("nazivi", err == nil && !t.UpdatedAt.IsZero(), err),
		sektori:    procitano("sektori", data.Sectors > 0, g.sektori),
		djelatnici: procitano("računi", data.Djelatnika > 0, g.racuni),
		uparen:     procitano("razmjena", data.SyncTotal > 0, g.razmjena),
		posta:      izvorStanja(ctx, "e-pošta", h.izvori.posta),
	}
	if h.preuzimaVodostaje() {
		s.telemetrija = izvorStanja(ctx, "telemetrija", h.izvori.telemetrija)
	}
	return s
}

// opcijeAdministracije su Opcije za broj uključenih testnih prekidača
func (s *Server) opcijeAdministracije(ctx context.Context) models.Opcije {
	if s.akti == nil {
		return models.Opcije{}
	}
	return s.akti.Opcije(ctx)
}

// racunTelemetrije je izvor koraka Telemetrija u Prvim koracima
func (s *Server) racunTelemetrije(ctx context.Context) (bool, error) {
	return imaRacunTelemetrije(ctx, s.db)
}

// postaPodesena je izvor koraka E-pošta u Prvim koracima
func (s *Server) postaPodesena(ctx context.Context) (bool, error) {
	return s.akti != nil && s.akti.PostaPodesena(ctx), nil
}

// preuzimaVodostaje: ovo računalo ima ulogu „Preuzima vodostaje s izvora”
func (h *AdminHandler) preuzimaVodostaje() bool {
	return h.peers == nil || h.peers.TrenutneUloge().Preuzima
}
