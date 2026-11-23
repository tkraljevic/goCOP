package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"gocop/internal/dhmz"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// IzvoriNaslovne su izvori podataka sporih kartica. Postavlja ih poslužitelj,
// jer su to iste funkcije koje pune Prognoze, pločicu razmjene i brojke;
// izvor koji nije postavljen daje karticu „podaci nisu dostupni”.
type IzvoriNaslovne struct {
	Kisa       func(context.Context) kisaSlivovaPodaci
	Razmjena   func(context.Context) (RazmjenaStanje, bool)
	Brojke     func(context.Context) *BrojkePodataka
	Vrijeme    func(*http.Request) PlocaVremena
	Letve      func(context.Context) ([]models.GaugeSummary, error)
	Prije      func(context.Context, models.GaugeSummary) *int // vodostaj 24 h prije zadnjeg očitanja
	Prava      func(context.Context) (ceka int, nacin string, err error)
	Djelatnici func() ([]models.User, error)
	// SaZaduzenjem su računi koji imaju ili su imali zaduženje: račun bez
	// uloge koji je među njima nije nov, nego je ostao bez zaduženja
	SaZaduzenjem func() (map[uuid.UUID]bool, error)
	Nepredani    func(context.Context, *models.UserPermissions, string) ([]models.VodocuvarskiList, error)
	// Teren je terenski pogled osobe: letve koje redovito očitava
	Teren func(context.Context, *models.UserPermissions, *models.User) (*service.FieldOverview, error)
	// StanjaSektora su stanja obrane dionica sektora iz akata i dionice
	// sektora s područjem kojem pripadaju
	StanjaSektora func(context.Context, string) (map[string]models.StanjeObrane, []models.Section, error)
	// Osobe su osobe s valjanom ulogom u sektoru i, kad je zadano, području
	Osobe func(sektor string, area int, uloga models.Role) ([]models.User, error)
	// Podrucja su branjena područja sektora
	Podrucja func(sektor string) ([]models.Area, error)
	// Dezurstva su plan dežurstava dnevnika COP-a, Zapisi njegovi upisi
	Dezurstva func(ctx context.Context, dnevnik string) ([]models.Dezurstvo, error)
	Zapisi    func(ctx context.Context, dnevnik string) ([]models.JournalEntry, error)
	// Listovi su dnevni listovi vodočuvara koje osoba smije čitati, po filtru
	Listovi func(context.Context, *models.UserPermissions, repository.FiltarListova) ([]models.VodocuvarskiList, error)
	// Prijave su prijave s terena koje osoba smije vidjeti, po filtru
	Prijave func(context.Context, *models.UserPermissions, repository.FiltarPrijava) ([]models.PrijavaSTerena, error)
	// ZadaciPodrucja su zadaci vodočuvara područja kojima osoba smije
	// zadavati zadatke, otvoreni i zaključeni od trenutka od, uz imena
	ZadaciPodrucja func(ctx context.Context, perms *models.UserPermissions, area int, od time.Time) ([]models.Zadatak, map[string]string, error)
	// CekajuParafu su listovi područja koje osoba smije parafirati, a još
	// ih nije parafirala
	CekajuParafu func(ctx context.Context, perms *models.UserPermissions, area int) ([]models.VodocuvarskiList, error)
	// AktiNaPotpis su nacrti akata sektora koje osoba smije ovjeriti
	AktiNaPotpis func(ctx context.Context, perms *models.UserPermissions, sektor string) ([]models.Akt, error)
	// Sredstva za obranu: skladišta sektora ili područja, stanje skladišta,
	// sredstva na terenu sektora, promet, godišnji popisi i katalog
	Skladista        func(ctx context.Context, sektor string, area int) ([]models.Skladiste, error)
	StanjeSkladista  func(ctx context.Context, skladiste string) ([]models.StanjeVrste, error)
	SredstvaNaTerenu func(ctx context.Context, sektor string) ([]models.Stanje, error)
	PrometSredstava  func(ctx context.Context, f repository.FiltarPrometa) ([]models.Promet, error)
	PopisiSredstava  func(ctx context.Context, sektor, skladiste string, godina int) ([]models.Popis, error)
	VrsteSredstava   func(ctx context.Context) ([]models.VrstaSredstva, error)
	Posiljke         func(ctx context.Context, f repository.FiltarPosiljki) ([]models.Posiljka, error)
	// DnevniciUsluga su otvoreni dnevnici radova A.02 i A.03 područja, a
	// ListoviDnevnika dnevni listovi dnevnika
	DnevniciUsluga func(ctx context.Context, area int) ([]models.Journal, error)
	// NaloziDavatelja su nalozi koje je osoba dala izvođačima, za karticu
	// „Nalozi koje sam dao”
	NaloziDavatelja func(ctx context.Context, u *models.User) ([]models.NalogIzvodjacu, error)
	ListoviDnevnika func(ctx context.Context, dnevnik string) ([]models.JournalSheet, error)
}

// SetIzvori daje Naslovnoj izvore sporih kartica i zaboravlja zapamćene
// letve
func (h *DashboardHandler) SetIzvori(i IzvoriNaslovne) {
	h.izvori = i
	h.pamcenje.mu.Lock()
	h.pamcenje.letve = nil
	h.pamcenje.mu.Unlock()
}

// PamtiLetve je koliko dugo Naslovna pamti pregled letava. Kartice jedne
// Naslovne (područje, crpne stanice, ustave, vodostaji) traže ga u isto
// vrijeme, a pregled traje i dvije sekunde.
const PamtiLetve = 30 * time.Second

// zapamceneLetve su zadnji pregled letava i kada je složen
type zapamceneLetve struct {
	mu    sync.Mutex
	kad   time.Time
	letve []models.GaugeSummary
}

// errBezLetava je greška dok poslužitelj nema pregled letava
var errBezLetava = errors.New("pregled letava nije postavljen")

// letve su letve iz izvora. Unutar PamtiLetve od zadnjeg pregleda vraća
// isti, a kartice koje ih traže u isto vrijeme čekaju jedan pregled.
func (h *DashboardHandler) letve(ctx context.Context) ([]models.GaugeSummary, error) {
	if h.izvori.Letve == nil {
		return nil, errBezLetava
	}
	p := &h.pamcenje
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.letve != nil && time.Since(p.kad) < PamtiLetve {
		return p.letve, nil
	}
	l, err := h.izvori.Letve(ctx)
	if err == nil {
		p.letve, p.kad = l, time.Now()
	}
	return l, err
}

// nedostupna je kartica čiji izvor podataka nije stigao
func nedostupna(id, tekst string) KarticaNaslovne {
	k := glavaKartice(id)
	k.Prazno = &PraznaKartica{Vrsta: PraznoNedostupno, Tekst: tekst}
	return k
}

// glavaKartice je kartica s naslovom i poveznicom, bez tijela
func glavaKartice(id string) KarticaNaslovne {
	k := glaveKartica[id]
	k.ID = id
	return k
}

// vodostaji: letve iznad praga u cijeloj državi, od najvišeg stupnja; za
// dežurnog i rukovoditelja obrane izvođača samo letve njegova sektora, za
// rukovoditelja područja sve letve područja
func (h *DashboardHandler) vodostaji(ctx context.Context, perms *models.UserPermissions, sad time.Time) KarticaNaslovne {
	letve, err := h.letve(ctx)
	if err != nil {
		return nedostupna(kVodostaji, "Vodostaji trenutačno nisu dostupni.")
	}
	prije := func(g models.GaugeSummary) *int { return nil }
	if h.izvori.Prije != nil {
		prije = func(g models.GaugeSummary) *int { return h.izvori.Prije(ctx, g) }
	}
	switch ulogaKartice(perms, kVodostaji, sad) {
	case uSektor:
		return vodostajiSektora(letve, prije, sektorOsobe(perms, sad), sad)
	case uDezurni:
		letve = letveSektora(letve, *dezurnaDuznost(&perms.User, sad).SectorID)
	case uIzvObrana:
		letve = letveSektora(letve, *duznostObraneIzvodjaca(&perms.User, sad).SectorID)
	case uPodrucje:
		area := *duznostPodrucja(&perms.User, sad).AreaID
		return karticaVodostajaPodrucja(letvePodrucja(letve, area), prije, area, sad)
	case uUgovor:
		area := *duznostUgovora(&perms.User, sad).AreaID
		return karticaVodostajaPodrucja(letvePodrucja(letve, area), prije, area, sad)
	}
	return karticaVodostaja(letve, prije, sad)
}

// letveSektora su letve koje pripadaju sektoru ili ih sektor prati
func letveSektora(letve []models.GaugeSummary, sektor string) []models.GaugeSummary {
	var out []models.GaugeSummary
	for _, g := range letve {
		if g.SectorID == sektor || slices.Contains(g.SectorIDs, sektor) {
			out = append(out, g)
		}
	}
	return out
}

// karticaVodostaja slaže letve iznad praga (Odluke (31. 10. 2026.), državna
// razina); uz vodostaj piše vrijeme očitanja i promjena u 24 h, a promjenu
// računa samo za retke koji se vide
func karticaVodostaja(letve []models.GaugeSummary, prije func(models.GaugeSummary) *int, sad time.Time) KarticaNaslovne {
	k := glavaKartice(kVodostaji)
	k.HitnoNaziv = "iznad praga"
	for _, g := range letve {
		if g.Latest == nil || g.Latest.LevelCm == nil || !g.Phase.InForce() || sad.Sub(g.Latest.MeasuredAt) > NeaktualnoOcitanje {
			continue
		}
		var p *int
		if len(k.Redovi) < NajviseRedaka {
			p = prije(g)
		}
		k.Redovi = append(k.Redovi, redakLetve(g, p, sad))
	}
	if len(k.Redovi) == 0 {
		k.Prazno = &PraznaKartica{Vrsta: PraznoGotovo, Tekst: "Nijedna letva nije iznad praga."}
	}
	return k
}

// redakLetve: ime, vrijeme očitanja i promjena u 24 h, vodostaj desno i
// stupanj obrane ispod
func redakLetve(g models.GaugeSummary, prije *int, sad time.Time) RedakKartice {
	cm := *g.Latest.LevelCm
	r := RedakKartice{Ikona: "minus", Glavno: g.Name, Sporedno: satOcitanja(g.Latest.MeasuredAt, sad), Vrijednost: fmt.Sprintf("%d cm", cm),
		URL: g.URL, Hitno: true, Oznaka: &OznakaRetka{Tekst: g.Phase.Label(), Razred: g.Phase.PillClass()},
		Staro: sad.Sub(g.Latest.MeasuredAt) > StaroOcitanje}
	if prije != nil {
		d := cm - *prije
		r.Sporedno += fmt.Sprintf(" · %+d cm u 24 h", d)
		r.Ikona = map[bool]string{true: "trending-up", false: "trending-down"}[d >= 0]
	}
	return r
}

// StaroOcitanje je koliko staro očitanje kartica pokazuje sivo: letva
// koja se očitava jednom na dan ima ga dan ranije
const StaroOcitanje = 36 * time.Hour

// NeaktualnoOcitanje je očitanje koje više ne govori o stanju: letva koja
// se tri dana nije očitala ne stoji na kartici kao da je danas iznad praga
// (npr. crpna stanica s očitanjem od prije mjesec dana)
const NeaktualnoOcitanje = 72 * time.Hour

// satOcitanja: „07:00” za današnje očitanje, inače i datum „30. 10. 07:00”
func satOcitanja(t, sad time.Time) string {
	s := kadNaslovne(t, sad)
	return strings.TrimPrefix(s, "danas ")
}

// jaceOborine: međuslivovi u kojima pada neuobičajeno jaka kiša
func (h *DashboardHandler) jaceOborine(ctx context.Context) KarticaNaslovne {
	if h.izvori.Kisa == nil {
		return nedostupna(kJaceOborine, nedostupnaKisa)
	}
	return karticaJacihOborina(h.izvori.Kisa(ctx), time.Now())
}

// karticaJacihOborina: lanac međuslivova i svaki međusliv iznad svog praga
// kiše s trakom prema pragu (posebno tijelo „kisa”, crtež 11a od 14. 11.);
// obični retci ostaju za broj hitnih i ispis bez tijela. Bez kiše iznad
// praga kartica je prazna i lanca nema (Odluke 14. 11. 2026.).
func karticaJacihOborina(p kisaSlivovaPodaci, sad time.Time) KarticaNaslovne {
	k := glavaKartice(kJaceOborine)
	k.HitnoNaziv = "s jakom kišom"
	// jača kiša ide na vrh, da ne ostane skrivena pod „Više”; ista razina
	// ostaje redom međuslivova
	slivovi := slices.Clone(p.Upozorenja)
	sort.SliceStable(slivovi, func(i, j int) bool { return slivovi[i].Razina > slivovi[j].Razina })
	for _, s := range slivovi {
		naziv := s.Naziv
		if naziv == "" {
			naziv = s.Sliv
		}
		k.Redovi = append(k.Redovi, RedakKartice{Glavno: naziv, Sporedno: opisKise(s), Istaknuto: "kisa", Hitno: s.Razina >= 2,
			URL: "/slivovi#sliv-" + s.Sliv})
	}
	switch {
	case len(k.Redovi) > 0:
		k.Tijelo, k.Podaci = "kisa", kisaNaslovne(p, slivovi, sad)
	case p.Greska != "":
		k.Prazno = &PraznaKartica{Vrsta: PraznoNedostupno, Tekst: p.Greska}
	default:
		k.Prazno = &PraznaKartica{Vrsta: PraznoGotovo, Tekst: "Nema jače kiše."}
	}
	return k
}

// opisKise: „52 mm u 3 dana (0 mm u zadnja 24 h) · još 15 mm u 2 dana ·
// ovoliko kiše padne prosječno jednom godišnje (u 3 dana) · porast u
// prognozi: …”. Pokazuje sva tri razdoblja, jer prag može prijeći kiša bilo
// kojeg od njih, a uz učestalost kaže kojeg.
func opisKise(s SlivUKisi) string {
	palo := brojHRf(s.Palo24, 0) + " mm u zadnja 24 h"
	if s.Palo72 > 0 {
		palo = brojHRf(s.Palo72, 0) + " mm u 3 dana (" + brojHRf(s.Palo24, 0) + " mm u zadnja 24 h)"
	}
	dijelovi := []string{palo}
	if s.Dolazi48 > 0 {
		dijelovi = append(dijelovi, "još "+brojHRf(s.Dolazi48, 0)+" mm u 2 dana")
	}
	if s.Ucestalost != "" {
		u := s.Ucestalost
		if prozor := prozoriOpisa[s.Zasto]; prozor != "" {
			u += " (" + prozor + ")"
		}
		dijelovi = append(dijelovi, u)
	}
	if p := porastRetka(s); p != "" {
		dijelovi = append(dijelovi, p)
	}
	return strings.Join(dijelovi, " · ")
}

// prozoriOpisa kažu u retku kartice iz kojeg je razdoblja učestalost
var prozoriOpisa = map[string]string{"palo u zadnja 72 sata": "u 3 dana", "palo i očekuje se u 72 sata": "u 3 dana s prognozom"}

// vrijeme: upozorenja DHMZ-a, trenutno vrijeme i prognoza regije za mjesto
// osobe
func (h *DashboardHandler) vrijeme(r *http.Request) KarticaNaslovne {
	if h.izvori.Vrijeme == nil {
		return nedostupna(kVrijeme, "Podaci DHMZ-a trenutačno nisu dostupni.")
	}
	return karticaVremena(h.izvori.Vrijeme(r))
}

// karticaVremena: najprije upozorenja za županiju mjesta, pa ostala po
// regijama, onda „Sada” i prognoza regije. Upozorenje DHMZ-a nikad nema
// plohu u boji faze, samo crtu i natpis (Odluke (31. 10. 2026.)).
func karticaVremena(p PlocaVremena) KarticaNaslovne {
	k := glavaKartice(kVrijeme)
	if p.Mjesto != "" {
		k.Naslov = "Vrijeme · " + p.Mjesto
	}
	for _, u := range p.Upozorenja {
		k.Redovi = append(k.Redovi, redakUpozorenja(u))
	}
	if r, ima := redakOstalihUpozorenja(p.Ostala, time.Now()); ima {
		k.Redovi = append(k.Redovi, r)
	}
	if r, ima := redakSada(p); ima {
		k.Redovi = append(k.Redovi, r)
	}
	if p.Prognoza != "" {
		k.Redovi = append(k.Redovi, RedakKartice{Ikona: "calendar", Glavno: "Prognoza · " + p.Regija, Sporedno: skrati(p.Prognoza, 160)})
	}
	if len(k.Redovi) == 0 {
		k.Prazno = &PraznaKartica{Vrsta: PraznoNedostupno, Tekst: "Podaci DHMZ-a trenutačno nisu dostupni."}
	}
	return k
}

// razineUpozorenja su riječi i razredi crte za razine upozorenja DHMZ-a
var razineUpozorenja = map[int]string{2: "žuto", 3: "narančasto", 4: "crveno"}

// redakUpozorenja: „DHMZ · Žuto upozorenje za kišu”, područje i vrijeme
func redakUpozorenja(u dhmz.Upozorenje) RedakKartice {
	sporedno := []string{u.Podrucje}
	if !u.Od.IsZero() {
		sporedno = append(sporedno, u.Od.In(models.Zagreb).Format("2. 1. 15:04")+"–"+u.Do.In(models.Zagreb).Format("15:04"))
	}
	return RedakKartice{Ikona: "triangle-alert", Glavno: "DHMZ · " + nazivUpozorenja(u),
		Sporedno: strings.Join(sporedno, " · "), Istaknuto: fmt.Sprintf("dhmz-%d", u.Razina), Hitno: u.Razina >= 3}
}

// nazivUpozorenja: DHMZ ga piše cijelog („Narančasto upozorenje za vjetar”);
// samo događaj („vjetar”) dobije razinu riječima
func nazivUpozorenja(u dhmz.Upozorenje) string {
	if strings.Contains(strings.ToLower(u.Dogadjaj), "upozorenje") {
		return u.Dogadjaj
	}
	return razineUpozorenja[u.Razina] + " upozorenje: " + u.Dogadjaj
}

// redakOstalihUpozorenja sažme upozorenja drugih područja u jedan redak
// („DHMZ · 18 upozorenja drugdje”, „u 7 područja · najviše narančasto”) s
// crtom najviše razine. Broj je isti kao na Prognozama › Vrijeme, a klik
// ondje otvara popis po područjima.
func redakOstalihUpozorenja(ostala []dhmz.Upozorenje, sada time.Time) (RedakKartice, bool) {
	d := upozorenjaDrugdje(ostala, sada)
	if d == nil {
		return RedakKartice{}, false
	}
	return RedakKartice{Ikona: "triangle-alert", Glavno: "DHMZ · " + brojUpozorenjaRijecima(d.Ukupno) + " drugdje",
		Sporedno:  "u " + uPodrucja(len(d.Podrucja)) + " · najviše " + razineUpozorenja[d.Razina],
		Istaknuto: fmt.Sprintf("dhmz-%d", d.Razina), URL: "/prognoze/vrijeme?drugdje=1"}, true
}

// redakSada: temperatura i opis s najbliže postaje, vjetar i kiša s
// najbližeg kišomjera
func redakSada(p PlocaVremena) (RedakKartice, bool) {
	if p.Postaja == nil {
		return RedakKartice{}, false
	}
	glavno := p.Postaja.Opis
	if p.Postaja.Temp != nil {
		glavno = strings.TrimSuffix(brojHRf(*p.Postaja.Temp, 0)+" °C · "+p.Postaja.Opis, " · ")
	}
	var sporedno []string
	if v := vjetarRijecima(p.Postaja.VjetarSmjer, p.Postaja.VjetarBrzina); v != "" { // isto kao na Prognozama › Vrijeme
		sporedno = append(sporedno, v)
	}
	if i := p.Izmjereno; i != nil && i.Danas != nil {
		sporedno = append(sporedno, "kišomjer "+i.Naziv+": "+brojHRf(*i.Danas, 1)+" mm danas")
	}
	return RedakKartice{Ikona: "cloud-rain", Glavno: glavno, Sporedno: strings.Join(sporedno, " · ")}, true
}

// skrati reže tekst na granici riječi i dodaje tri točke
func skrati(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	i := strings.LastIndex(string(r[:n]), " ")
	if i < 0 {
		i = len(string(r[:n]))
	}
	return strings.TrimRight(string(r[:n])[:i], " ,.;") + "…"
}

// sustav: razmjena s ostalim računalima, zadnja prognoza i koliko podataka
// sustav nosi
func (h *DashboardHandler) sustav(ctx context.Context, sad time.Time) KarticaNaslovne {
	if h.izvori.Razmjena == nil {
		return nedostupna(kSustav, "Stanje sustava trenutačno nije dostupno.")
	}
	r, ima := h.izvori.Razmjena(ctx)
	if !ima {
		return nedostupna(kSustav, "Stanje sustava trenutačno nije dostupno.")
	}
	var b *BrojkePodataka
	if h.izvori.Brojke != nil {
		b = h.izvori.Brojke(ctx)
	}
	return karticaSustava(r, b, sad)
}

// karticaSustava: jedan redak za razmjenu, prognozu i podatke; pune brojke
// su u Administraciji (Odluke (31. 10. 2026.))
func karticaSustava(r RazmjenaStanje, b *BrojkePodataka, sad time.Time) KarticaNaslovne {
	k := glavaKartice(kSustav)
	k.Redovi = append(k.Redovi, redakRazmjene(r, sad))
	if r.Prognoza.Ima {
		p := RedakKartice{Ikona: "gauge", Glavno: "Prognoza izdana " + kadNaslovne(r.Prognoza.Izdano, sad), URL: "/prognoze"}
		if r.Prognoza.Izdavac != "" {
			p.Sporedno = "izračunalo računalo " + r.Prognoza.Izdavac
		}
		if r.PrognozaKasni() {
			p.Sporedno, p.Hitno = "izdanje je starije od tri sata", true
		}
		k.Redovi = append(k.Redovi, p)
	}
	if b != nil && b.Zapisa > 0 {
		k.Redovi = append(k.Redovi, RedakKartice{Ikona: "layers", Glavno: "Podaci u sustavu", Sporedno: opisPodataka(b)})
	}
	return k
}

// redakRazmjene: koliko je računala usklađeno i kad je bila zadnja razmjena
func redakRazmjene(r RazmjenaStanje, sad time.Time) RedakKartice {
	red := RedakKartice{Ikona: "refresh-cw", URL: "/sinkronizacija"}
	if !r.UMrezi {
		red.Glavno, red.Sporedno = "Razmjena: računalo nije u mreži", "podaci se ne razmjenjuju s drugim računalima"
		return red
	}
	uskladjenih := 0
	var zadnja time.Time
	for _, c := range r.Cvorovi {
		if c.uskladjen() {
			uskladjenih++
		}
		if c.Zadnja != nil && c.Zadnja.After(zadnja) {
			zadnja = *c.Zadnja
		}
	}
	n := len(r.Cvorovi)
	red.Glavno = fmt.Sprintf("Razmjena: %d od %d %s usklađeno", uskladjenih, n, models.UzBrojHR(n, "računala", "računala", "računala"))
	if r.Sredeno() {
		red.Glavno = fmt.Sprintf("Razmjena: %d %s usklađen%s", n, models.UzBrojHR(n, "računalo", "računala", "računala"), models.UzBrojHR(n, "o", "a", "o"))
	}
	red.Sporedno = "zadnja " + kadNaslovne(zadnja, sad)
	if len(r.Upozorenja) > 0 {
		red.Sporedno, red.Hitno = r.Upozorenja[0], true
	}
	return red
}

// opisPodataka: „48,3 mil. zapisa · 212 letvi · vodostaji od 1. 1. 1900.”
func opisPodataka(b *BrojkePodataka) string {
	zapisa := brojHRf(float64(b.Zapisa), 0) + " zapisa"
	if b.Zapisa >= 1_000_000 {
		zapisa = brojHRf(float64(b.Zapisa)/1e6, 1) + " mil. zapisa"
	}
	dijelovi := []string{zapisa, fmt.Sprintf("%d %s", b.Letvi, models.UzBrojHR(b.Letvi, "letva", "letve", "letvi"))}
	if b.NajstarijiDan != "" {
		dijelovi = append(dijelovi, "vodostaji od "+b.NajstarijiDan)
	}
	return strings.Join(dijelovi, " · ")
}

// stavkeZaRijesiti su izvori kartice Za riješiti globalnog administratora
type stavkeZaRijesiti struct {
	Stanja     []service.StanjeObrane
	Nepredani  map[string][]models.VodocuvarskiList // sektor → nepredani listovi
	PravaCeka  int
	PravaNacin string
	BezUloge   []models.User
	// BezZaduzenja su računi koji su imali zaduženje, a više nemaju nijedno
	BezZaduzenja []models.User
	// uprava sektora: akti na potpis i prijave s terena sektora
	Akti    []models.Akt
	Prijave []models.PrijavaSTerena
	Sektor  string
	NaPutu  []RedakKartice // COP: pošiljke dugo na putu
}

// zaRijesiti spaja posao svih uloga osobe koje imaju tu karticu
func (h *DashboardHandler) zaRijesiti(r *http.Request, perms *models.UserPermissions, sad time.Time) KarticaNaslovne {
	var kartice []KarticaNaslovne
	for _, u := range ulogeNaslovne(perms, sad) {
		if slices.Contains(karticeZaUlogu[u], kZaRijesiti) {
			kartice = append(kartice, h.zaRijesitiUloge(r, perms, u, sad))
		}
	}
	return spojiZaRijesiti(kartice)
}

// spojiZaRijesiti spaja kartice Za riješiti više uloga: retci bez
// ponavljanja, a prazna je tek kad nijedna uloga nema posla. Nedostupan
// izvor jedne uloge piše se samo kad drugih redaka nema.
func spojiZaRijesiti(kartice []KarticaNaslovne) KarticaNaslovne {
	if len(kartice) == 0 {
		k := glavaKartice(kZaRijesiti)
		k.Prazno = &PraznaKartica{Vrsta: PraznoGotovo, Tekst: "Nema ništa za riješiti."}
		return k
	}
	k := kartice[0]
	k.Redovi = nil
	vidjeno := map[string]bool{}
	for _, d := range kartice {
		for _, red := range d.Redovi {
			if kljuc := red.URL + "|" + red.Glavno + "|" + red.Sporedno; !vidjeno[kljuc] {
				vidjeno[kljuc] = true
				k.Redovi = append(k.Redovi, red)
			}
		}
		if d.Prazno != nil && d.Prazno.Vrsta == PraznoNedostupno {
			k.Prazno = d.Prazno
		}
	}
	k.Broj = len(k.Redovi)
	if k.Broj > 0 {
		k.Prazno = nil
	}
	return k
}

// zaRijesitiUloge je kartica Za riješiti jedne uloge: područje, sektor,
// skladište, ugovor i dionica imaju svoj posao, a ostali posao uprave
func (h *DashboardHandler) zaRijesitiUloge(r *http.Request, perms *models.UserPermissions, uloga string, sad time.Time) KarticaNaslovne {
	switch uloga {
	case uPodrucje:
		return h.zaRijesitiPodrucja(r.Context(), perms, *duznostPodrucja(&perms.User, sad), sad)
	case uSektor:
		return h.zaRijesitiSektora(r, perms, sektorOsobe(perms, sad), sad)
	case uSkladiste:
		return h.karticaSkladistara(r, perms, kZaRijesiti, sad)
	case uUgovor:
		return h.karticaUsluga(r, perms, kZaRijesiti, sad)
	case uDionica:
		return h.zaRijesitiDionica(r.Context(), perms, duznostiDionica(&perms.User, sad), sad)
	}
	return h.zaRijesitiUprave(r, perms, uloga, sad)
}

// zaRijesitiUprave skuplja posao globalnog administratora, uprave i
// dežurnog: sektorsko izvješće u obrani, nepredane dnevne listove, a
// administratoru i promjene prava i nove djelatnike
func (h *DashboardHandler) zaRijesitiUprave(r *http.Request, perms *models.UserPermissions, uloga string, sad time.Time) KarticaNaslovne {
	ctx := r.Context()
	s := stavkeZaRijesiti{Nepredani: map[string][]models.VodocuvarskiList{}}
	data := DashboardViewData{ViewAsBanner: viewBanner(r)}
	h.stanjeIZid(r, perms, &data)
	s.Stanja = data.Stanja
	for _, st := range s.Stanja {
		if h.izvori.Nepredani == nil {
			break
		}
		if l, err := h.izvori.Nepredani(ctx, perms, st.Sektor); err == nil && len(l) > 0 {
			s.Nepredani[st.Sektor] = l
		}
	}
	if uloga == uDezurni {
		s.NaPutu = h.dugoNaPutu(ctx, *dezurnaDuznost(&perms.User, sad).SectorID, sad)
	}
	if !perms.IsGlobalAdmin {
		return karticaZaRijesiti(s, sad) // prava i nove djelatnike rješava samo globalni administrator
	}
	if h.izvori.Prava != nil {
		s.PravaCeka, s.PravaNacin, _ = h.izvori.Prava(ctx) // bez knjige prava redak izostaje
	}
	s.BezUloge, s.BezZaduzenja = h.racuniBezUloge()
	return karticaZaRijesiti(s, sad)
}

// racuniBezUloge su računi koji su se prijavili, a nemaju zaduženje: novi i
// oni koji su ga imali; bez imenika nema ni jednih
func (h *DashboardHandler) racuniBezUloge() (novi, bivsi []models.User) {
	if h.izvori.Djelatnici == nil {
		return nil, nil
	}
	svi, _ := h.izvori.Djelatnici() // bez imenika redak izostaje
	var imali map[uuid.UUID]bool
	if h.izvori.SaZaduzenjem != nil {
		imali, _ = h.izvori.SaZaduzenjem() // bez popisa svi bez uloge su novi, kao prije
	}
	return razdvojiBezUloge(noviBezUloge(svi), imali)
}

// noviBezUloge su računi koji su se prijavili, a još nemaju ulogu. Djelatnik
// iz imenika koji se nikad nije prijavio ne čeka ništa (Odluke
// (30. 10. 2026.), 15: novi račun nema zadanu ulogu).
func noviBezUloge(svi []models.User) []models.User {
	var out []models.User
	for i := range svi {
		if svi[i].IsActive && svi[i].LastLoginAt != nil && cekaUlogu(&svi[i]) {
			out = append(out, svi[i])
		}
	}
	return out
}

// razdvojiBezUloge dijeli račune bez uloge na nove i one koji su imali
// zaduženje (imali), pa su ga izgubili opozivom ili istekom
func razdvojiBezUloge(bez []models.User, imali map[uuid.UUID]bool) (novi, bivsi []models.User) {
	for _, u := range bez {
		if imali[u.ID] {
			bivsi = append(bivsi, u)
		} else {
			novi = append(novi, u)
		}
	}
	return novi, bivsi
}

// karticaZaRijesiti: najprije ono što obrana traži danas, pa listovi, pa
// prava i djelatnici; broj uz naslov je broj stavki
func karticaZaRijesiti(s stavkeZaRijesiti, sad time.Time) KarticaNaslovne {
	k := glavaKartice(kZaRijesiti)
	for _, a := range s.Akti {
		k.Redovi = append(k.Redovi, redakAkta(a))
	}
	for _, st := range s.Stanja {
		if st.Stadij.InForce() && !st.Sektorsko {
			k.Redovi = append(k.Redovi, RedakKartice{Ikona: "file-text", Glavno: "Sektorsko izvješće " + st.Sektor + " još nije sastavljeno",
				Sporedno: st.Stadij.Label() + " · rok 10:00", URL: "/sektorsko-izvjesce/novo?sektor=" + st.Sektor, Hitno: true})
		}
	}
	k.Redovi = append(k.Redovi, redoviNepredanih(s.Nepredani)...)
	k.Redovi = append(k.Redovi, redoviPrijava(s.Prijave, s.Sektor, sad)...)
	k.Redovi = append(k.Redovi, s.NaPutu...)
	if s.PravaCeka > 0 {
		k.Redovi = append(k.Redovi, redakPrava(s.PravaCeka, s.PravaNacin))
	}
	if r, ima := redakBezUloge(s.BezUloge, sad); ima {
		k.Redovi = append(k.Redovi, r)
	}
	if r, ima := redakBezZaduzenja(s.BezZaduzenja); ima {
		k.Redovi = append(k.Redovi, r)
	}
	k.Broj = len(k.Redovi)
	if k.Broj == 0 {
		k.Prazno = &PraznaKartica{Vrsta: PraznoGotovo, Tekst: "Nema ništa za riješiti."}
	}
	return k
}

// redoviNepredanih: po sektoru koliko listova nije predano i od kada
func redoviNepredanih(poSektoru map[string][]models.VodocuvarskiList) []RedakKartice {
	sektori := make([]string, 0, len(poSektoru))
	for s := range poSektoru {
		sektori = append(sektori, s)
	}
	sort.Strings(sektori)
	var out []RedakKartice
	for _, s := range sektori {
		l := poSektoru[s]
		najstariji := l[0].Datum
		for _, x := range l {
			if x.Datum.Before(najstariji) {
				najstariji = x.Datum
			}
		}
		n := len(l)
		out = append(out, RedakKartice{Ikona: "notebook-pen",
			Glavno:   fmt.Sprintf("%d %s", n, models.UzBrojHR(n, "dnevni list nije predan", "dnevna lista nisu predana", "dnevnih listova nije predano")),
			Sporedno: fmt.Sprintf("Sektor %s · najstariji od %d. %d.", s, najstariji.Day(), int(najstariji.Month())),
			URL:      "/vodocuvar?sektor=" + s})
	}
	return out
}

// redakPrava: promjene prava koje čekaju potvrdu i gdje se o njima odlučuje
func redakPrava(n int, nacin string) RedakKartice {
	sporedno := "Odluči u Sinkronizaciji"
	if nacin != repository.OdlukaOvdje {
		sporedno = "Odluči na računalu s ključem mreže"
	}
	return RedakKartice{Ikona: "key-round", Glavno: fmt.Sprintf("%d %s potvrdu", n, models.UzBrojHR(n, "promjena prava čeka", "promjene prava čekaju", "promjena prava čeka")),
		Sporedno: sporedno, URL: "/sinkronizacija"}
}

// redakBezZaduzenja: djelatnik koji je imao zaduženje, a sad nema nijedno
// (opozvano ili isteklo); uprava mu daje novo ili bilježi odlazak
func redakBezZaduzenja(bez []models.User) (RedakKartice, bool) {
	switch len(bez) {
	case 0:
		return RedakKartice{}, false
	case 1:
		return RedakKartice{Ikona: "user", Glavno: "Djelatnik bez zaduženja", Sporedno: bez[0].FullName + " · dodijelite zaduženje ili zabilježite odlazak",
			URL: "/users/" + bez[0].ID.String()}, true
	}
	var imena []string
	for _, u := range bez[:min(3, len(bez))] {
		imena = append(imena, u.FullName)
	}
	n := len(bez)
	return RedakKartice{Ikona: "users", Glavno: fmt.Sprintf("%d %s bez zaduženja", n, models.UzBrojHR(n, "djelatnik", "djelatnika", "djelatnika")),
		Sporedno: strings.Join(imena, ", "), URL: "/users"}, true
}

// redakBezUloge: novi djelatnik koji čeka ulogu, ili koliko ih čeka
func redakBezUloge(bez []models.User, sad time.Time) (RedakKartice, bool) {
	switch len(bez) {
	case 0:
		return RedakKartice{}, false
	case 1:
		u := bez[0]
		return RedakKartice{Ikona: "user", Glavno: "Novi djelatnik čeka ulogu", Sporedno: u.FullName + ", prijavio se " + danNaslovne(*u.LastLoginAt, sad),
			URL: "/users/" + u.ID.String()}, true
	}
	var imena []string
	for _, u := range bez[:min(3, len(bez))] {
		imena = append(imena, u.FullName)
	}
	n := len(bez)
	return RedakKartice{Ikona: "users", Glavno: fmt.Sprintf("%d %s ulogu", n, models.UzBrojHR(n, "novi djelatnik čeka", "nova djelatnika čekaju", "novih djelatnika čeka")),
		Sporedno: strings.Join(imena, ", "), URL: "/users"}, true
}
