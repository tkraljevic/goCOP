package web

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Naslovna rukovoditelja dionice i zamjenika (Odluke (31. 10. 2026.)).
// Rukovoditelj najčešće ima više dionica, ponekad na dva branjena područja,
// pa je na vrhu kartica „Moje dionice”, a ostale kartice zbrajaju sve
// dionice i uz stavku pišu šifru dionice. Naloga u programu još nema.

// Kartice rukovoditelja dionice
const (
	kMojeDionice    = "moje-dionice"
	kObjektiDionica = "objekti-dionica"
)

// duznostiDionica su valjane dužnosti rukovoditelja ili zamjenika
// rukovoditelja dionice osobe na branjenom području
func duznostiDionica(u *models.User, sad time.Time) []models.Duty {
	var out []models.Duty
	for _, d := range models.VazecaZaduzenja(u.Duties, sad) {
		if (d.Role == models.RoleSectionLeader || d.Role == models.RoleSectionDeputy) && d.AreaID != nil && d.SectorID != nil {
			out = append(out, d)
		}
	}
	return out
}

// pokrivaDionicu: dužnost bez upisanih dionica pokriva cijelo područje
func pokrivaDionicu(d models.Duty, s models.Section) bool {
	if d.AreaID == nil || *d.AreaID != s.AreaID {
		return false
	}
	sifre := d.Dionice()
	return len(sifre) == 0 || slices.ContainsFunc(sifre, func(c string) bool { return strings.EqualFold(c, s.Code) })
}

// redDionica slaže šifre po brojevima: B.16.2 prije B.16.10
func redDionica(a, b string) int {
	x, y := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(x) && i < len(y); i++ {
		c := cmp.Compare(x[i], y[i])
		if n, errN := strconv.Atoi(x[i]); errN == nil {
			if m, errM := strconv.Atoi(y[i]); errM == nil {
				c = cmp.Compare(n, m)
			}
		}
		if c != 0 {
			return c
		}
	}
	return cmp.Compare(len(x), len(y))
}

// dioniceOsobe su dionice osobe po dužnostima i stanja obrane njihovih
// sektora, iz akata; greška kad stanje nekog sektora nije dostupno
func (h *DashboardHandler) dioniceOsobe(ctx context.Context, duznosti []models.Duty) ([]models.Section, map[string]models.StanjeObrane, error) {
	stanja := map[string]models.StanjeObrane{}
	var out []models.Section
	vidjeni := map[string]bool{}
	for _, d := range duznosti {
		st, dionice, err := h.izvori.StanjaSektora(ctx, *d.SectorID)
		if err != nil {
			return nil, nil, err
		}
		for k, v := range st {
			stanja[k] = v
		}
		for _, s := range dionice {
			if pokrivaDionicu(d, s) && !vidjeni[s.Code] {
				vidjeni[s.Code] = true
				out = append(out, s)
			}
		}
	}
	slices.SortFunc(out, func(a, b models.Section) int { return redDionica(a.Code, b.Code) })
	return out, stanja, nil
}

// karticaRukovoditeljaDionice slaže spore kartice rukovoditelja dionice
func (h *DashboardHandler) karticaRukovoditeljaDionice(r *http.Request, perms *models.UserPermissions, id string, sad time.Time) KarticaNaslovne {
	duznosti := duznostiDionica(&perms.User, sad)
	ctx := r.Context()
	if id == kZaRijesiti {
		return h.zaRijesitiDionica(ctx, perms, duznosti, sad)
	}
	if len(duznosti) == 0 || h.izvori.StanjaSektora == nil {
		return nedostupna(id, "Dionice trenutačno nisu dostupne.")
	}
	dionice, stanja, err := h.dioniceOsobe(ctx, duznosti)
	if err != nil {
		return nedostupna(id, "Dionice trenutačno nisu dostupne.")
	}
	letve, _ := h.letve(ctx) // bez letava kartice nemaju objekata ni vodostaja
	switch id {
	case kObjektiDionica:
		return karticaObjekataDionica(dionice, letve, sad)
	case kVodostaji:
		return h.vodostajiDionica(ctx, dionice, letve, sad)
	}
	p := podaciDionica{Dionice: dionice, Stanja: stanja, Objekti: objektiPoDionici(dionice, letve)}
	p.Cuvari, p.Napomena = h.ljudiDionica(perms, duznosti, dionice, sad)
	return karticaMojihDionica(p)
}

// podaciDionica su podaci kartice „Moje dionice”
type podaciDionica struct {
	Dionice  []models.Section
	Stanja   map[string]models.StanjeObrane
	Cuvari   map[string][]string // vodočuvari po dionici, samo oni kojima je dionica upisana
	Objekti  map[string]string   // „1 crpna stanica · 2 ustave” po dionici
	Napomena string
}

// karticaMojihDionica: svaka dionica u svom retku, dionice u obrani prve
func karticaMojihDionica(p podaciDionica) KarticaNaslovne {
	k := KarticaNaslovne{ID: kMojeDionice, Naslov: "Moje dionice", HitnoNaziv: "u obrani", Napomena: p.Napomena}
	for _, s := range p.Dionice {
		r := RedakKartice{Glavno: s.Code, Vrijednost: "preventivna obrana", Tiho: true, URL: "/sections/" + s.Code}
		if opis := kratkiOpisDionice(s); opis != "" {
			r.Glavno += " · " + opis
		}
		var dio []string
		switch c := p.Cuvari[s.Code]; len(c) {
		case 0:
		case 1:
			dio = append(dio, "vodočuvar "+c[0])
		default:
			dio = append(dio, "vodočuvari "+strings.Join(c, ", "))
		}
		if o := p.Objekti[s.Code]; o != "" {
			dio = append(dio, o)
		}
		if v := p.Stanja[s.Code].Vrh(); v.Stupanj.InForce() {
			r.Istaknuto, r.Vrijednost, r.Tiho, r.Hitno = v.Stupanj.PillClass(), v.Stupanj.Label(), false, true
			dio = append(dio, "od "+odKadFaze(v.Od))
		}
		r.Sporedno = strings.Join(dio, " · ")
		k.Redovi = append(k.Redovi, r)
	}
	slices.SortStableFunc(k.Redovi, func(a, b RedakKartice) int { return cmp.Compare(boolBroj(b.Hitno), boolBroj(a.Hitno)) })
	k.Broj = len(k.Redovi)
	if k.Broj == 0 {
		k.Prazno = &PraznaKartica{Vrsta: PraznoCeka, Tekst: "Dužnost nema dionica u registru."}
	}
	return k
}

// kratkiOpisDionice je voda dionice, bez obale i obuhvata: „potok
// Karašica (Baranja)”, a za dionicu na više voda „kanal Travnik i još 3”
func kratkiOpisDionice(s models.Section) string {
	dijelovi := strings.Split(models.SkratiOpisDionice(s.EffectiveDescription()), " · ")
	voda, _, _ := strings.Cut(dijelovi[0], ",")
	voda = skrati(strings.TrimSpace(voda), 40)
	if n := len(dijelovi) - 1; n > 0 && voda != "" {
		voda += fmt.Sprintf(" i još %d", n)
	}
	return voda
}

// objektiPoDionici: crpne stanice i ustave svake dionice, riječima
func objektiPoDionici(dionice []models.Section, letve []models.GaugeSummary) map[string]string {
	vrsta := map[string]string{}
	for _, g := range letve {
		if g.StructureID != "" {
			vrsta[g.StructureID] = g.Kind
		}
	}
	out := map[string]string{}
	for _, s := range dionice {
		crpne, ustave := 0, 0
		for _, id := range s.AllStructureIDs() {
			switch vrsta[id] {
			case models.StructureKindPumpingStation:
				crpne++
			case models.StructureKindSluice:
				ustave++
			}
		}
		out[s.Code] = opisObjekata(crpne, ustave, 0)
	}
	return out
}

// ljudiDionica su vodočuvari kojima je dionica posebno upisana i napomena
// kartice: područje, drugi rukovoditelj istih dionica i vodočuvari
// područja („BP 16 · zamjenik Ana Proba · vodočuvari Ivo Proba, Pero
// Proba”). Vodočuvar je u pravilu dostupan na svim dionicama svog
// područja, pa vodočuvari područja stoje jednom, a ne uz svaku dionicu.
func (h *DashboardHandler) ljudiDionica(perms *models.UserPermissions, duznosti []models.Duty, dionice []models.Section, sad time.Time) (map[string][]string, string) {
	cuvari := map[string][]string{}
	var napomena []string
	podrucja := map[int]string{}
	for _, d := range duznosti {
		area, sektor := *d.AreaID, *d.SectorID
		dio := h.nazivPodrucja(sektor, area)
		if h.izvori.Osobe != nil {
			if drugi := drugiRukovoditelji(h, perms, d, sad); drugi != "" {
				dio += " · " + drugi
			}
			if _, vidjeno := podrucja[area]; !vidjeno {
				podrucja[area] = h.vodocuvariPodrucja(sektor, area, dionice, cuvari, sad)
			}
			dio = strings.TrimSuffix(dio+" · "+podrucja[area], " · ")
		}
		if !slices.Contains(napomena, dio) {
			napomena = append(napomena, dio)
		}
	}
	return cuvari, strings.Join(napomena, "; ")
}

// vodocuvariPodrucja: „vodočuvari Ivo Proba, Pero Proba” za područje, a
// uz dionice upisuje vodočuvare kojima je dionica posebno upisana
func (h *DashboardHandler) vodocuvariPodrucja(sektor string, area int, dionice []models.Section, cuvari map[string][]string, sad time.Time) string {
	vodocuvari, _ := h.izvori.Osobe(sektor, area, models.RoleWaterGuard) // bez imenika napomena je bez vodočuvara
	for _, s := range dionice {
		cuvari[s.Code] = append(cuvari[s.Code], cuvariDionice(vodocuvari, s, sad)...)
	}
	var imena []string
	for _, v := range vodocuvari {
		if slices.ContainsFunc(models.VazecaZaduzenja(v.Duties, sad), func(d models.Duty) bool {
			return d.Role == models.RoleWaterGuard && d.AreaID != nil && *d.AreaID == area
		}) && !slices.Contains(imena, v.FullName) {
			imena = append(imena, v.FullName)
		}
	}
	switch len(imena) {
	case 0:
		return ""
	case 1:
		return "vodočuvar " + imena[0]
	}
	return "vodočuvari " + strings.Join(imena, ", ")
}

// nazivPodrucja: „BP 16 · Mali sliv Baranja”, bez registra „BP 16”
func (h *DashboardHandler) nazivPodrucja(sektor string, area int) string {
	if h.izvori.Podrucja != nil {
		podrucja, _ := h.izvori.Podrucja(sektor) // bez registra piše samo broj područja
		for _, a := range podrucja {
			if a.ID == area {
				return podrucjeNaslovne(a)
			}
		}
	}
	return podrucjeNaslovne(models.Area{ID: area})
}

// cuvariDionice su vodočuvari kojima je dionica posebno upisana u
// dužnost; vodočuvar cijelog područja stoji u napomeni kartice
func cuvariDionice(vodocuvari []models.User, s models.Section, sad time.Time) []string {
	var out []string
	for _, v := range vodocuvari {
		for _, d := range models.VazecaZaduzenja(v.Duties, sad) {
			if d.Role == models.RoleWaterGuard && len(d.Dionice()) > 0 && pokrivaDionicu(d, s) && !slices.Contains(out, v.FullName) {
				out = append(out, v.FullName)
			}
		}
	}
	return out
}

// drugiRukovoditelji: zamjenik rukovoditelja istih dionica, a zamjeniku
// rukovoditelj („zamjenik Ana Proba”)
func drugiRukovoditelji(h *DashboardHandler, perms *models.UserPermissions, moja models.Duty, sad time.Time) string {
	uloga, naziv := models.RoleSectionDeputy, "zamjenik "
	if moja.Role == models.RoleSectionDeputy {
		uloga, naziv = models.RoleSectionLeader, "rukovoditelj "
	}
	osobe, _ := h.izvori.Osobe(*moja.SectorID, *moja.AreaID, uloga) // bez imenika napomena je bez imena
	var imena []string
	for _, o := range osobe {
		if o.ID != perms.User.ID && vodiIsteDionice(o, uloga, moja, sad) && !slices.Contains(imena, o.FullName) {
			imena = append(imena, o.FullName)
		}
	}
	if len(imena) == 0 {
		return ""
	}
	return naziv + strings.Join(imena, ", ")
}

// vodiIsteDionice: osoba u ulozi vodi barem jednu dionicu dužnosti moja,
// na istom području
func vodiIsteDionice(o models.User, uloga models.Role, moja models.Duty, sad time.Time) bool {
	return slices.ContainsFunc(models.VazecaZaduzenja(o.Duties, sad), func(d models.Duty) bool {
		return d.Role == uloga && d.AreaID != nil && *d.AreaID == *moja.AreaID && dijeleDionicu(d, moja)
	})
}

// dijeleDionicu: dvije dužnosti imaju zajedničku dionicu; dužnost bez
// dionica pokriva cijelo područje
func dijeleDionicu(a, b models.Duty) bool {
	x, y := a.Dionice(), b.Dionice()
	if len(x) == 0 || len(y) == 0 {
		return true
	}
	return slices.ContainsFunc(x, func(c string) bool {
		return slices.ContainsFunc(y, func(d string) bool { return strings.EqualFold(c, d) })
	})
}

// karticaObjekataDionica: crpne stanice koje rade ili su u kvaru i ustave
// s promijenjenom zapornicom na dionicama osobe, uz šifru dionice
func karticaObjekataDionica(dionice []models.Section, letve []models.GaugeSummary, sad time.Time) KarticaNaslovne {
	k := KarticaNaslovne{ID: kObjektiDionica, Naslov: "Objekti na dionicama", HitnoNaziv: "u kvaru"}
	dionicaObjekta, stanice, ustave := objektiNaDionicama(dionice, letve)
	if len(stanice)+len(ustave) == 0 {
		k.Prazno = &PraznaKartica{Vrsta: PraznoCeka, Tekst: "Na dionicama nema crpnih stanica ni ustava."}
		return k
	}
	rade, miruje, bezStanica := crpneURadu(stanice, sad)
	promjene, ista, bezUstava := promjeneZapornica(ustave, sad)
	for _, g := range rade {
		k.Redovi = append(k.Redovi, uzDionicu(redakCrpneStanice(g, sad), dionicaObjekta[g.StructureID]))
	}
	for _, g := range promjene {
		k.Redovi = append(k.Redovi, uzDionicu(redakUstave(g, sad), dionicaObjekta[g.StructureID]))
	}
	k.Napomena = napomenaObjekata(len(k.Redovi) > 0, miruje+ista, bezStanica+bezUstava,
		[3]string{"objekt bez promjene", "objekta bez promjene", "objekata bez promjene"},
		[3]string{"objekt", "objekta", "objekata"}, "Svi objekti bez promjene.")
	return k
}

// objektiNaDionicama su crpne stanice i ustave na dionicama, iz pregleda
// letava, i dionica svakog objekta (prva na kojoj je upisan)
func objektiNaDionicama(dionice []models.Section, letve []models.GaugeSummary) (dionicaObjekta map[string]string, stanice, ustave []models.GaugeSummary) {
	dionicaObjekta = map[string]string{}
	for _, s := range dionice {
		for _, id := range s.AllStructureIDs() {
			if dionicaObjekta[id] == "" {
				dionicaObjekta[id] = s.Code
			}
		}
	}
	for _, g := range letve {
		switch {
		case dionicaObjekta[g.StructureID] == "":
		case g.Kind == models.StructureKindPumpingStation:
			stanice = append(stanice, g)
		case g.Kind == models.StructureKindSluice:
			ustave = append(ustave, g)
		}
	}
	return dionicaObjekta, stanice, ustave
}

// uzDionicu dopiše šifru dionice na početak sporednog teksta retka
func uzDionicu(r RedakKartice, dionica string) RedakKartice {
	r.Sporedno = dionica + " · " + r.Sporedno
	return r
}

// vodostajiDionica: letve mjerodavne za dionice osobe, uz šifru dionice
func (h *DashboardHandler) vodostajiDionica(ctx context.Context, dionice []models.Section, letve []models.GaugeSummary, sad time.Time) KarticaNaslovne {
	dionicaLetve := map[string]string{}
	for _, s := range dionice {
		for _, id := range s.AllStationIDs() {
			if dionicaLetve[id] == "" {
				dionicaLetve[id] = s.Code
			}
		}
	}
	var moje []models.GaugeSummary
	for _, g := range letve {
		if g.StructureID == "" && dionicaLetve[g.StationID] != "" {
			moje = append(moje, g)
		}
	}
	prije := func(models.GaugeSummary) *int { return nil }
	if h.izvori.Prije != nil {
		prije = func(g models.GaugeSummary) *int { return h.izvori.Prije(ctx, g) }
	}
	return karticaVodostajaMjesta(moje, prije, dionicaLetve, "U zadnja tri dana nema očitanja letava na dionicama.", sad)
}

// zaRijesitiNaPodrucju: listovi koji čekaju parafu i prijave s terena na
// jednom području
func (h *DashboardHandler) zaRijesitiNaPodrucju(ctx context.Context, perms *models.UserPermissions, sektor string, area int, sad time.Time) []RedakKartice {
	var out []RedakKartice
	if h.izvori.CekajuParafu != nil {
		l, _ := h.izvori.CekajuParafu(ctx, perms, area) // bez dnevnika redak izostaje
		if n := len(l); n > 0 {
			out = append(out, redakListova(l, fmt.Sprintf("%d %s", n, models.UzBrojHR(n, "dnevni list čeka parafu", "dnevna lista čekaju parafu", "dnevnih listova čeka parafu")),
				fmt.Sprintf("/vodocuvar?sektor=%s&podrucje=%d&ceka=1", sektor, area)))
		}
	}
	if h.izvori.Prijave != nil {
		p, _ := h.izvori.Prijave(ctx, perms, repository.FiltarPrijava{Sektor: sektor, AreaID: area, Status: models.PrijavaObjavljena, Limit: 2000}) // bez prijava redak izostaje
		out = append(out, redoviPrijava(p, sektor, sad)...)
	}
	return out
}

// zaRijesitiDionica: listovi koji čekaju parafu i prijave s terena na
// dionicama osobe, po područjima njezinih dužnosti
func (h *DashboardHandler) zaRijesitiDionica(ctx context.Context, perms *models.UserPermissions, duznosti []models.Duty, sad time.Time) KarticaNaslovne {
	k := glavaKartice(kZaRijesiti)
	podrucja := map[int]string{}
	for _, d := range duznosti {
		podrucja[*d.AreaID] = *d.SectorID
	}
	for _, area := range slices.Sorted(maps.Keys(podrucja)) {
		redovi := h.zaRijesitiNaPodrucju(ctx, perms, podrucja[area], area, sad)
		if len(podrucja) > 1 {
			for i := range redovi {
				redovi[i].Sporedno = strings.TrimSuffix(fmt.Sprintf("BP %d · %s", area, redovi[i].Sporedno), " · ")
			}
		}
		k.Redovi = append(k.Redovi, redovi...)
	}
	k.Broj = len(k.Redovi)
	if k.Broj == 0 {
		k.Prazno = &PraznaKartica{Vrsta: PraznoGotovo, Tekst: "Nema ništa za riješiti."}
	}
	return k
}
