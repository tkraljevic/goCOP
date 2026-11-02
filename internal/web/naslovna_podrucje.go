package web

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Naslovna rukovoditelja branjenog područja i zamjenika (Odluke
// (31. 10. 2026.)): područje, Za riješiti, zadaci vodočuvarima, crpne
// stanice, ustave, vodostaji područja, vrijeme i jače oborine. Naloga
// strojarima i rukovateljima u programu još nema, pa ni njihove kartice.

// Kartice rukovoditelja branjenog područja
const (
	kPodrucje       = "podrucje"
	kZadaciPodrucja = "zadaci-podrucja"
	kCrpneStanice   = "crpne-stanice"
	kUstave         = "ustave"
)

// duznostPodrucja je valjana dužnost rukovoditelja ili zamjenika
// rukovoditelja branjenog područja; nil kad je nema. Zamjenik ima isti
// zaslon kao rukovoditelj.
func duznostPodrucja(u *models.User, sad time.Time) *models.Duty {
	for _, d := range models.VazecaZaduzenja(u.Duties, sad) {
		if (d.Role == models.RoleAreaLeader || d.Role == models.RoleAreaDeputy) && d.AreaID != nil && d.SectorID != nil {
			return &d
		}
	}
	return nil
}

// lijenaKarticaPodrucja je glava kartice područja s brojem područja, dok
// se tijelo ne učita
func lijenaKarticaPodrucja(perms *models.UserPermissions) KarticaNaslovne {
	k := lijenaKartica(kPodrucje)
	if d := duznostPodrucja(&perms.User, time.Now()); d != nil {
		k.Naslov = fmt.Sprintf("BP %d", *d.AreaID)
	}
	return k
}

// karticaRukovoditeljaBP slaže spore kartice rukovoditelja područja
func (h *DashboardHandler) karticaRukovoditeljaBP(r *http.Request, perms *models.UserPermissions, id string, sad time.Time) KarticaNaslovne {
	d := duznostPodrucja(&perms.User, sad)
	if d == nil {
		return nedostupna(id, "Dužnost na branjenom području je u međuvremenu istekla.")
	}
	ctx := r.Context()
	switch id {
	case kZadaciPodrucja:
		return h.zadaciPodrucja(ctx, perms, *d, sad)
	case kCrpneStanice, kUstave:
		return h.objektiPodrucja(ctx, id, *d, sad)
	}
	return h.podrucje(ctx, perms, *d, sad)
}

// podaciPodrucja su podaci kartice područja
type podaciPodrucja struct {
	Podrucje models.Area
	Sektor   string
	Stanja   map[string]models.StanjeObrane
	Dionice  []models.Section
	Crpne    int
	Ustave   int
	NaTerenu int // vodočuvari s današnjim dnevnim listom; -1 kad se ne zna
}

// podrucje: stanje obrane područja, tko je danas na terenu i broj objekata
func (h *DashboardHandler) podrucje(ctx context.Context, perms *models.UserPermissions, d models.Duty, sad time.Time) KarticaNaslovne {
	area, sektor := *d.AreaID, *d.SectorID
	k := nedostupna(kPodrucje, "Stanje područja trenutačno nije dostupno.")
	k.Naslov = fmt.Sprintf("BP %d", area)
	if h.izvori.StanjaSektora == nil || h.izvori.Podrucja == nil {
		return k
	}
	stanja, dionice, err := h.izvori.StanjaSektora(ctx, sektor)
	podrucja, err2 := h.izvori.Podrucja(sektor)
	if err != nil || err2 != nil {
		return k
	}
	p := podaciPodrucja{Podrucje: models.Area{ID: area}, Sektor: sektor, Stanja: stanja, Dionice: dionice}
	for _, a := range podrucja {
		if a.ID == area {
			p.Podrucje = a
		}
	}
	p.Crpne, p.Ustave = h.brojObjekata(ctx, area)
	p.NaTerenu = h.naTerenu(ctx, perms, area, sad)
	return karticaPodrucja(p, sad)
}

// brojObjekata su broj crpnih stanica i ustava područja koje se očitavaju
func (h *DashboardHandler) brojObjekata(ctx context.Context, area int) (crpne, ustave int) {
	letve, _ := h.letve(ctx) // bez letava redak objekata broji samo dionice
	return len(objektiVrste(letve, area, models.StructureKindPumpingStation)), len(objektiVrste(letve, area, models.StructureKindSluice))
}

// naTerenu je broj vodočuvara područja koji su danas otvorili dnevni list
func (h *DashboardHandler) naTerenu(ctx context.Context, perms *models.UserPermissions, area int, sad time.Time) int {
	if h.izvori.Listovi == nil {
		return -1
	}
	danas := sad.In(models.Zagreb).Format("2006-01-02")
	l, err := h.izvori.Listovi(ctx, perms, repository.FiltarListova{AreaID: area, Godina: sad.In(models.Zagreb).Year(), Limit: 200})
	if err != nil {
		return -1
	}
	osobe := map[string]bool{}
	for _, x := range l {
		if x.Datum.In(models.Zagreb).Format("2006-01-02") == danas {
			osobe[x.UserID] = true
		}
	}
	return len(osobe)
}

// karticaPodrucja: stanje obrane područja s napomenom o ostatku sektora,
// vodočuvari na terenu i broj objekata
func karticaPodrucja(p podaciPodrucja, sad time.Time) KarticaNaslovne {
	area := p.Podrucje.ID
	k := KarticaNaslovne{ID: kPodrucje, Naslov: podrucjeNaslovne(p.Podrucje), Poveznica: "Akti", URL: "/akti", HitnoNaziv: "u obrani"}
	k.Redovi = append(k.Redovi, redakStanjaPodrucja(area, p.Sektor, p.Stanja, p.Dionice))
	vodociste := fmt.Sprintf("/vodocuvar?sektor=%s&podrucje=%d", p.Sektor, area)
	switch {
	case p.NaTerenu > 0:
		k.Redovi = append(k.Redovi, RedakKartice{Ikona: "users", Glavno: "Na terenu danas",
			Sporedno: fmt.Sprintf("%d %s", p.NaTerenu, models.UzBrojHR(p.NaTerenu, "vodočuvar s današnjim dnevnim listom", "vodočuvara s današnjim dnevnim listom", "vodočuvara s današnjim dnevnim listom")), URL: vodociste})
	case p.NaTerenu == 0:
		k.Redovi = append(k.Redovi, RedakKartice{Ikona: "users", Glavno: "Na terenu danas", Sporedno: "Danas još nitko nije otvorio dnevni list.", URL: vodociste})
	}
	if s := opisObjekata(p.Crpne, p.Ustave, brojDionica(p.Dionice, area)); s != "" {
		k.Redovi = append(k.Redovi, RedakKartice{Ikona: "layers", Glavno: "Objekti", Sporedno: s,
			URL: fmt.Sprintf("/structures?sector=%s&area=%d", p.Sektor, area)})
	}
	return k
}

// redakStanjaPodrucja: stupanj obrane područja i od kada, a kad je drugdje
// u sektoru viši, i to (kao traka vodočuvara)
func redakStanjaPodrucja(area int, sektor string, stanja map[string]models.StanjeObrane, dionice []models.Section) RedakKartice {
	vlastito := fazaPodrucja(area, stanja, dionice)
	drugo, drugoBP := najvisaFazaDrugdje(area, stanja, dionice)
	r := RedakKartice{Ikona: "shield", Glavno: "Preventivna obrana"}
	var dio []string
	if vlastito.Stupanj.InForce() {
		r.Glavno, r.Istaknuto, r.Hitno = vlastito.Stupanj.Label(), vlastito.Stupanj.PillClass(), true
		dio = append(dio, "od "+odKadFaze(vlastito.Od))
	}
	if drugo.Stupanj.Severity() > vlastito.Stupanj.Severity() {
		dio = append(dio, fmt.Sprintf("Sektor %s: %s na BP %d od %s", sektor, strings.ToLower(drugo.Stupanj.Label()), drugoBP, odKadFaze(drugo.Od)))
	}
	r.Sporedno = strings.Join(dio, " · ")
	return r
}

// brojDionica je broj dionica područja
func brojDionica(dionice []models.Section, area int) int {
	n := 0
	for _, d := range dionice {
		if d.AreaID == area {
			n++
		}
	}
	return n
}

// opisObjekata: „12 crpnih stanica · 14 ustava · 10 dionica”, bez onoga
// čega nema
func opisObjekata(crpne, ustave, dionice int) string {
	var dio []string
	for _, b := range []struct {
		n      int
		oblici [3]string
	}{
		{crpne, [3]string{"crpna stanica", "crpne stanice", "crpnih stanica"}},
		{ustave, [3]string{"ustava", "ustave", "ustava"}},
		{dionice, [3]string{"dionica", "dionice", "dionica"}},
	} {
		if b.n > 0 {
			dio = append(dio, fmt.Sprintf("%d %s", b.n, models.UzBrojHR(b.n, b.oblici[0], b.oblici[1], b.oblici[2])))
		}
	}
	return strings.Join(dio, " · ")
}

// objektiVrste su objekti vrste na području, iz pregleda letava
func objektiVrste(letve []models.GaugeSummary, area int, vrsta string) []models.GaugeSummary {
	var out []models.GaugeSummary
	for _, g := range letve {
		if g.StructureID != "" && g.AreaID == area && g.Kind == vrsta {
			out = append(out, g)
		}
	}
	return out
}

// objektiPodrucja: crpne stanice ili ustave područja po zadnjem očitanju
func (h *DashboardHandler) objektiPodrucja(ctx context.Context, id string, d models.Duty, sad time.Time) KarticaNaslovne {
	letve, err := h.letve(ctx)
	if err != nil {
		return nedostupna(id, "Očitanja objekata trenutačno nisu dostupna.")
	}
	area, sektor := *d.AreaID, *d.SectorID
	if id == kUstave {
		return karticaUstava(objektiVrste(letve, area, models.StructureKindSluice), sektor, area, sad)
	}
	return karticaCrpnihStanica(objektiVrste(letve, area, models.StructureKindPumpingStation), sektor, area, sad)
}

// svjeze je očitanje koje još govori o stanju objekta
func svjeze(g models.GaugeSummary, sad time.Time) bool {
	return g.Latest != nil && sad.Sub(g.Latest.MeasuredAt) <= NeaktualnoOcitanje
}

// stanjeCrpneStanice je stanje crpne stanice riječima kad nije u
// mirovanju; prazno za mirovanje i zaustavljenu stanicu
var stanjeCrpneStanice = map[string]string{
	models.StructureStateStarting:  "radi",
	models.StructureStateSiphoning: "sifonira",
	models.StructureStateFault:     "kvar",
}

// karticaCrpnihStanica: stanice koje rade ili su u kvaru, s dovodnim
// vodostajem; ostale se samo prebroje (Odluke (31. 10. 2026.))
func karticaCrpnihStanica(stanice []models.GaugeSummary, sektor string, area int, sad time.Time) KarticaNaslovne {
	k := KarticaNaslovne{ID: kCrpneStanice, Naslov: "Crpne stanice", Poveznica: fmt.Sprintf("Sve %d", len(stanice)),
		URL: fmt.Sprintf("/structures?kind=%s&sector=%s&area=%d", models.StructureKindPumpingStation, sektor, area), HitnoNaziv: "u kvaru"}
	if len(stanice) == 0 {
		k.Poveznica = ""
		k.Prazno = &PraznaKartica{Vrsta: PraznoCeka, Tekst: fmt.Sprintf("U registru BP %d nema crpnih stanica.", area)}
		return k
	}
	miruje, bez := 0, 0
	for _, g := range stanice {
		switch {
		case !svjeze(g, sad):
			bez++
		case stanjeCrpneStanice[g.Latest.StructureState] != "":
			k.Redovi = append(k.Redovi, redakObjekta(g, stanjeCrpneStanice[g.Latest.StructureState], sad))
		default:
			miruje++
		}
	}
	slices.SortStableFunc(k.Redovi, func(a, b RedakKartice) int { return cmp.Compare(boolBroj(b.Hitno), boolBroj(a.Hitno)) })
	k.Napomena = napomenaObjekata(len(k.Redovi) > 0, miruje, bez,
		[3]string{"crpna stanica miruje", "crpne stanice miruju", "crpnih stanica miruje"},
		[3]string{"crpnu stanicu", "crpne stanice", "crpnih stanica"}, "Sve crpne stanice miruju.")
	return k
}

// boolBroj je 1 za istinu, za redoslijed
func boolBroj(b bool) int {
	if b {
		return 1
	}
	return 0
}

// redakObjekta: ime, stanje, vrijeme i tko je očitao, vodostaj desno
func redakObjekta(g models.GaugeSummary, stanje string, sad time.Time) RedakKartice {
	dio := []string{stanje, satOcitanja(g.Latest.MeasuredAt, sad)}
	if o := strings.TrimSpace(g.Latest.Observer); o != "" {
		dio = append(dio, o)
	}
	r := RedakKartice{Ikona: "crpna-stanica", Glavno: g.Name, Sporedno: strings.Join(dio, " · "), URL: g.URL,
		Staro: sad.Sub(g.Latest.MeasuredAt) > StaroOcitanje}
	if g.Kind == models.StructureKindSluice {
		r.Ikona = "ustava"
	}
	if g.Latest.LevelCm != nil {
		r.Vrijednost = fmt.Sprintf("%d cm", *g.Latest.LevelCm)
	}
	if g.Latest.StructureState == models.StructureStateFault {
		r.Ikona, r.Hitno = "triangle-alert", true
	}
	return r
}

// napomenaObjekata: „Još 9 crpnih stanica miruje. Za 2 crpne stanice nema
// očitanja u zadnja tri dana.” Ostali su objekti koji nisu u retcima, a bez
// oni bez svježeg očitanja.
func napomenaObjekata(imaRedaka bool, ostali, bez int, ostaliOblici, bezOblici [3]string, sviOstali string) string {
	var dio []string
	switch {
	case ostali > 0 && imaRedaka:
		dio = append(dio, fmt.Sprintf("Još %d %s.", ostali, models.UzBrojHR(ostali, ostaliOblici[0], ostaliOblici[1], ostaliOblici[2])))
	case ostali > 0 && bez == 0:
		dio = append(dio, sviOstali)
	case ostali > 0:
		dio = append(dio, fmt.Sprintf("%d %s.", ostali, models.UzBrojHR(ostali, ostaliOblici[0], ostaliOblici[1], ostaliOblici[2])))
	}
	if bez > 0 {
		dio = append(dio, fmt.Sprintf("Za %d %s nema očitanja u zadnja tri dana.", bez, models.UzBrojHR(bez, bezOblici[0], bezOblici[1], bezOblici[2])))
	}
	return strings.Join(dio, " ")
}

// karticaUstava: ustave čija se zapornica promijenila od prethodnog
// očitanja, najnovija prva; ostale se samo prebroje (Odluke (31. 10. 2026.))
func karticaUstava(ustave []models.GaugeSummary, sektor string, area int, sad time.Time) KarticaNaslovne {
	k := KarticaNaslovne{ID: kUstave, Naslov: "Ustave", Poveznica: fmt.Sprintf("Sve %d", len(ustave)),
		URL: fmt.Sprintf("/structures?kind=%s&sector=%s&area=%d", models.StructureKindSluice, sektor, area)}
	if len(ustave) == 0 {
		k.Poveznica = ""
		k.Prazno = &PraznaKartica{Vrsta: PraznoCeka, Tekst: fmt.Sprintf("U registru BP %d nema ustava.", area)}
		return k
	}
	var promjene []models.GaugeSummary
	ista, bez := 0, 0
	for _, g := range ustave {
		switch {
		case !svjeze(g, sad) || g.Latest.Gate == "":
			bez++
		case g.Previous != nil && g.Previous.Gate != "" && g.Previous.Gate != g.Latest.Gate:
			promjene = append(promjene, g)
		default:
			ista++
		}
	}
	slices.SortStableFunc(promjene, func(a, b models.GaugeSummary) int { return b.Latest.MeasuredAt.Compare(a.Latest.MeasuredAt) })
	for _, g := range promjene {
		r := redakObjekta(g, g.Latest.GateLabel(), sad)
		r.Vrijednost = ""
		k.Redovi = append(k.Redovi, r)
	}
	k.Napomena = napomenaObjekata(len(k.Redovi) > 0, ista, bez,
		[3]string{"ustava bez promjene", "ustave bez promjene", "ustava bez promjene"},
		[3]string{"ustavu", "ustave", "ustava"}, "Nijedna zapornica nije mijenjana od prethodnog očitanja.")
	return k
}

// letvePodrucja su letve (ne objekti) mjerodavne za područje
func letvePodrucja(letve []models.GaugeSummary, area int) []models.GaugeSummary {
	var out []models.GaugeSummary
	for _, g := range letve {
		if g.StationID != "" && g.StructureID == "" && slices.Contains(g.AreaIDs, area) {
			out = append(out, g)
		}
	}
	return out
}

// karticaVodostajaPodrucja: sve letve područja sa svježim očitanjem, prvo
// one iznad praga; stupanj obrane piše samo uz letvu iznad praga
func karticaVodostajaPodrucja(letve []models.GaugeSummary, prije func(models.GaugeSummary) *int, area int, sad time.Time) KarticaNaslovne {
	k := glavaKartice(kVodostaji)
	k.HitnoNaziv = "iznad praga"
	for _, g := range letve {
		if !svjeze(g, sad) || g.Latest.LevelCm == nil {
			continue
		}
		var p *int
		if len(k.Redovi) < NajviseRedaka {
			p = prije(g)
		}
		r := redakLetve(g, p, sad)
		if !g.Phase.InForce() {
			r.Oznaka, r.Hitno = nil, false
		}
		k.Redovi = append(k.Redovi, r)
	}
	slices.SortStableFunc(k.Redovi, func(a, b RedakKartice) int { return cmp.Compare(boolBroj(b.Hitno), boolBroj(a.Hitno)) })
	if len(k.Redovi) == 0 {
		k.Prazno = &PraznaKartica{Vrsta: PraznoCeka, Tekst: fmt.Sprintf("U zadnja tri dana nema očitanja letava BP %d.", area)}
	}
	return k
}

// NovePrijave je koliko dugo objavljena prijava s terena stoji na kartici
// Za riješiti u svom retku; starije neriješene sažme jedan redak (u
// ranijoj evidenciji ih je mnogo koje nitko nije zatvorio)
const NovePrijave = 7 * 24 * time.Hour

// stavkePodrucja su ono što rukovoditelj područja treba riješiti
type stavkePodrucja struct {
	Ovjera    []models.VodocuvarskiList
	Nepredani []models.VodocuvarskiList
	Prijave   []models.PrijavaSTerena
}

// zaRijesitiPodrucja: listovi koji čekaju ovjeru, nepredani listovi i
// neriješene prijave s terena područja
func (h *DashboardHandler) zaRijesitiPodrucja(ctx context.Context, perms *models.UserPermissions, d models.Duty, sad time.Time) KarticaNaslovne {
	area, sektor := *d.AreaID, *d.SectorID
	var s stavkePodrucja
	if h.izvori.Listovi != nil {
		s.Ovjera, _ = h.izvori.Listovi(ctx, perms, repository.FiltarListova{AreaID: area, CekaPotvrdu: true, Limit: 500}) // bez dnevnika redak izostaje
	}
	if h.izvori.Nepredani != nil {
		l, _ := h.izvori.Nepredani(ctx, perms, sektor) // bez dnevnika redak izostaje
		for _, x := range l {
			if x.AreaID == area {
				s.Nepredani = append(s.Nepredani, x)
			}
		}
	}
	if h.izvori.Prijave != nil {
		s.Prijave, _ = h.izvori.Prijave(ctx, perms, repository.FiltarPrijava{Sektor: sektor, AreaID: area, Status: models.PrijavaObjavljena, Limit: 2000}) // bez prijava redak izostaje
	}
	return karticaZaRijesitiPodrucja(area, sektor, s, sad)
}

// karticaZaRijesitiPodrucja: ovjera, nepredani listovi, nove prijave svaka
// u svom retku i starije neriješene prijave u jednom
func karticaZaRijesitiPodrucja(area int, sektor string, s stavkePodrucja, sad time.Time) KarticaNaslovne {
	k := glavaKartice(kZaRijesiti)
	knjiga := fmt.Sprintf("/vodocuvar?sektor=%s&podrucje=%d", sektor, area)
	var ovjera []models.VodocuvarskiList
	for _, l := range s.Ovjera {
		if !l.Rekonstrukcija {
			ovjera = append(ovjera, l)
		}
	}
	if n := len(ovjera); n > 0 {
		k.Redovi = append(k.Redovi, redakListova(ovjera, fmt.Sprintf("%d %s", n, models.UzBrojHR(n, "dnevni list čeka ovjeru", "dnevna lista čekaju ovjeru", "dnevnih listova čeka ovjeru")), knjiga+"&ceka=1"))
	}
	if n := len(s.Nepredani); n > 0 {
		k.Redovi = append(k.Redovi, redakListova(s.Nepredani, fmt.Sprintf("%d %s", n, models.UzBrojHR(n, "dnevni list nije predan", "dnevna lista nisu predana", "dnevnih listova nije predano")), knjiga))
	}
	var starije []models.PrijavaSTerena
	for _, p := range s.Prijave {
		if sad.Sub(objavljenaPrijava(p)) <= NovePrijave {
			k.Redovi = append(k.Redovi, redakPrijave(p, sad))
		} else {
			starije = append(starije, p)
		}
	}
	if len(starije) > 0 {
		k.Redovi = append(k.Redovi, redakStarijihPrijava(starije, sektor))
	}
	k.Broj = len(k.Redovi)
	if k.Broj == 0 {
		k.Prazno = &PraznaKartica{Vrsta: PraznoGotovo, Tekst: "Nema ništa za riješiti."}
	}
	return k
}

// redakListova: koliko listova, čiji su i od kada najstariji
func redakListova(listovi []models.VodocuvarskiList, glavno, url string) RedakKartice {
	najstariji := listovi[0].Datum
	var imena []string
	for _, l := range listovi {
		if l.Datum.Before(najstariji) {
			najstariji = l.Datum
		}
		if l.Ime != "" && !slices.Contains(imena, l.Ime) && len(imena) < 3 {
			imena = append(imena, l.Ime)
		}
	}
	dan := najstariji.In(models.Zagreb)
	return RedakKartice{Ikona: "notebook-pen", Glavno: glavno, URL: url,
		Sporedno: strings.TrimPrefix(strings.Join(imena, ", ")+fmt.Sprintf(" · najstariji od %d. %d.", dan.Day(), int(dan.Month())), " · ")}
}

// objavljenaPrijava je kad je prijava objavljena; za prenesenu bez toga
// podatka dan događaja
func objavljenaPrijava(p models.PrijavaSTerena) time.Time {
	if p.ObjavljenoAt != nil {
		return *p.ObjavljenoAt
	}
	return p.Datum
}

// redakPrijave: „Prijava: procjeđivanje” · mjesto, tko i kada
func redakPrijave(p models.PrijavaSTerena, sad time.Time) RedakKartice {
	var dio []string
	for _, s := range []string{p.Mjesto(), p.Ime, kadNaslovne(objavljenaPrijava(p), sad)} {
		if s = strings.TrimSpace(s); s != "" {
			dio = append(dio, s)
		}
	}
	return RedakKartice{Ikona: "map-pin", Glavno: p.VrstaLabel() + ": " + skrati(p.Naslov, 80), Sporedno: strings.Join(dio, " · "), URL: "/prijave/" + p.ID}
}

// redakStarijihPrijava: koliko je starijih prijava neriješeno i od kada
func redakStarijihPrijava(starije []models.PrijavaSTerena, sektor string) RedakKartice {
	najstarija := starije[0].Datum
	for _, p := range starije {
		if p.Datum.Before(najstarija) {
			najstarija = p.Datum
		}
	}
	n, dan := len(starije), najstarija.In(models.Zagreb)
	return RedakKartice{Ikona: "map-pin",
		Glavno:   fmt.Sprintf("%d %s", n, models.UzBrojHR(n, "starija prijava s terena nije riješena", "starije prijave s terena nisu riješene", "starijih prijava s terena nije riješeno")),
		Sporedno: fmt.Sprintf("najstarija od %d. %d. %d.", dan.Day(), int(dan.Month()), dan.Year()),
		URL:      fmt.Sprintf("/prijave?sektor=%s&status=%s", sektor, models.PrijavaObjavljena)}
}

// ZakljuceniNaKartici je koliko dugo obavljen ili odbačen zadatak stoji na
// kartici, da onaj tko ga je zadao vidi da je gotov (Odluke (1. 11. 2026.))
const ZakljuceniNaKartici = 24 * time.Hour

// zadaciPodrucja: zadaci vodočuvarima područja
func (h *DashboardHandler) zadaciPodrucja(ctx context.Context, perms *models.UserPermissions, d models.Duty, sad time.Time) KarticaNaslovne {
	if h.izvori.ZadaciPodrucja == nil {
		return nedostupna(kZadaciPodrucja, "Dnevnik vodočuvara trenutačno nije dostupan.")
	}
	z, imena, err := h.izvori.ZadaciPodrucja(ctx, perms, *d.AreaID, sad.Add(-ZakljuceniNaKartici))
	if err != nil {
		return nedostupna(kZadaciPodrucja, "Dnevnik vodočuvara trenutačno nije dostupan.")
	}
	return karticaZadatakaPodrucja(z, imena, perms.User.ID.String(), *d.SectorID, *d.AreaID, sad)
}

// karticaZadatakaPodrucja: najprije zadaci zaključeni u zadnja 24 sata,
// najnoviji prvi, pa otvoreni od najstarijeg; gumb „Novi zadatak”
func karticaZadatakaPodrucja(zadaci []models.Zadatak, imena map[string]string, ja, sektor string, area int, sad time.Time) KarticaNaslovne {
	k := glavaKartice(kZadaciPodrucja)
	k.Poveznica, k.URL = "Dnevnik", fmt.Sprintf("/vodocuvar?sektor=%s&podrucje=%d", sektor, area)
	k.Gumbi = []GumbNaslovne{{Tekst: "Novi zadatak", URL: "/vodocuvar#zadaj-zadatak", Glavni: true}}
	var zakljuceni []models.Zadatak
	var otvoreni []RedakKartice
	for _, z := range zadaci {
		if z.Otvoren() {
			otvoreni = append(otvoreni, redakOtvorenogZadatka(z, imena[z.UserID], ja, sad))
		} else if z.ObavljenoAt != nil {
			zakljuceni = append(zakljuceni, z)
		}
	}
	slices.SortStableFunc(zakljuceni, func(a, b models.Zadatak) int { return b.ObavljenoAt.Compare(*a.ObavljenoAt) })
	for _, z := range zakljuceni {
		k.Redovi = append(k.Redovi, redakZakljucenogZadatka(z, imena[z.UserID], sad))
	}
	k.Redovi = append(k.Redovi, otvoreni...)
	if len(k.Redovi) == 0 {
		k.Prazno = &PraznaKartica{Vrsta: PraznoGotovo, Tekst: "Nema otvorenih zadataka."}
	}
	return k
}

// redakOtvorenogZadatka: zadatak, kome je zadan i od kada; kad ga je zadao
// netko drugi, i tko
func redakOtvorenogZadatka(z models.Zadatak, vodocuvar, ja string, sad time.Time) RedakKartice {
	dio := []string{vodocuvar, "zadan " + kadNaslovne(z.ZadanoAt, sad)}
	if dan := z.Za.In(models.Zagreb); !z.Za.IsZero() && dan.After(sad) {
		dio[1] = fmt.Sprintf("za %d. %d.", dan.Day(), int(dan.Month()))
	}
	if z.ZadaoID != ja && z.Zadao != "" {
		dio = append(dio, "zadao "+z.Zadao)
	}
	return RedakKartice{Ikona: "clipboard-list", Glavno: skrati(z.Tekst, 100), Sporedno: strings.TrimPrefix(strings.Join(dio, " · "), " · "), URL: "/vodocuvar/dan"}
}

// redakZakljucenogZadatka: zadatak, tko ga je zaključio i kada
func redakZakljucenogZadatka(z models.Zadatak, vodocuvar string, sad time.Time) RedakKartice {
	r := RedakKartice{Ikona: "check", Glavno: skrati(z.Tekst, 100), URL: "/vodocuvar/dan"}
	kako := "obavljen "
	if z.Status == models.ZadatakOdbacen {
		r.Ikona, kako = "x", "nije obavljen "
	}
	r.Sporedno = strings.TrimPrefix(vodocuvar+" · "+kako+kadNaslovne(*z.ObavljenoAt, sad), " · ")
	return r
}
