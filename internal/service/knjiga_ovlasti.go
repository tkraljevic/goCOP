package service

// Tko što smije u tuđoj knjizi vodočuvara (dnevni listovi) i s objavljenim
// prijavama s terena, po Odlukama (8. 10. 2026.), pitanje 1. Doseg je
// dužnost: operater svoj sektor, ostali svoje branjeno područje.
//
//	                             čita listove  čita prijave   parafira   bilješka   zadatak
//	dežurni operater u COP-u     da            da             ne         da         ne
//	voditelj usluga / poslovođa  ne            ne             ne         ne         ne
//	rukovoditelj BP              da            da             da         da         da
//	rukovoditelj dionice         da            uz vezu        uz vezu    uz vezu    uz vezu i obranu
//	ovlaštenik A2 / A3           da            da             da         da         da
//
// Zamjenik ima ovlasti uloge koju zamjenjuje (models.Role.OvlastKnjige).
// Uprava sektora i organizacije zadržava dosadašnje ovlasti, a vodočuvar
// čita svoju knjigu. Ovjera lista i rješavanje prijave ostaju onima koji
// ovjeravaju (SmijeOvjeriti).
//
// Rukovoditelj dionice bez upisanih dionica vodi sve dionice svog
// branjenog područja, kao i drugdje u programu (normalizeScope,
// dioniceDosega): vezan mu je svaki list i svaka prijava područja, a obrana
// za zadatak gleda se na dionicama područja iz registra koje vodočuvar čuva.
//
// Veza s dionicom rukovoditelja dionice gleda zaduženja vodočuvara koja su
// vrijedila na dan lista ili prijave, u obliku u kojem su tada stajala, po
// knjizi verzija (models.ZaduzenjaUDanu). List i prijava iz vremena
// privremenog zaduženja tako ostaju vezani uz dionicu i kad zaduženje
// istekne s krajem obrane, bude opozvano ili izmijenjeno; zaduženje nastalo
// poslije ne veže ranije dane. List i prijava s dana prije vodočuvarova
// prvog zaduženja u programu (prijenos iz ranije evidencije, uvoz iz BP16)
// vezani su uz zaduženja s kojima je u program ušao
// (models.ZaduzenjaZaZapis). Zadatak gleda samo zaduženja valjana sada.
//   - list je vezan uz njegovu dionicu kad vodočuvar lista ima takvo
//     zaduženje vodočuvara na tom branjenom području koje tu dionicu navodi,
//     ili ne navodi nijednu (čuva cijelo područje);
//   - prijava pokazuje na dionice područja iz registra koje navodi polje
//     Dionica (slobodan upis: riječ koja nije šifra dionice područja ne
//     pokazuje ni na jednu) i na dionice objekta koji navodi (registar
//     objekata, po identifikatoru ili nazivu na području prijave). Kad
//     pokazuje na ikoju dionicu, vezana je samo uz dionice na koje
//     pokazuje, i kad ih vodočuvar ne čuva; kad ne pokazuje ni na jednu
//     (kao kod uvoza iz BP16), vezana je kao list njezina vodočuvara toga
//     dana;
//   - obrana traje kad je na dionici iz ovjerenih akata proglašena redovna
//     ili izvanredna obrana ili izvanredno stanje; pripremno stanje nije
//     obrana.
//
// Doseg dužnosti pokriva list i prijavu po mjestu na kojem su napisani
// (sektor i branjeno područje vodočuvarova glavnog zaduženja), ali i po
// mjestu svakog zaduženja vodočuvara koje je vrijedilo na njihov dan:
// vodočuvar na privremenoj ispomoći na drugom branjenom području ulazi u
// doseg tog područja za listove i prijave iz vremena ispomoći, a za
// zadatke dok ispomoć vrijedi. Veza s dionicom gleda se na području
// dužnosti, a prijava pokazuje na dionice koje navodi na svakom od tih
// područja. Ovjera i rješavanje prijave ostaju području lista.

import (
	"context"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// radnjaUKnjizi je upis u tuđu knjigu vodočuvara
type radnjaUKnjizi int

const (
	radnjaParafa radnjaUKnjizi = iota
	radnjaBiljeska
	radnjaZadatak
)

// SetStanjaObrane daje servisu stanja obrane dionica sektora iz ovjerenih
// akata (AktService.StanjaSektora), za zadatke rukovoditelja dionice
func (s *VodocuvarService) SetStanjaObrane(f func(ctx context.Context, sektor string, t time.Time) (map[string]models.StanjeObrane, error)) {
	s.stanjaObrane = f
}

// pamcenjeKnjige pamti za jedan popis (vodočuvari kojima se zadaje ili u
// čije se knjige upisuje) ono što je isto za sve vodočuvare na popisu:
// stanja obrane po sektoru i dionice po području iz registra. Akti sektora
// i registar tako se čitaju jednom po popisu, a ne za svakog vodočuvara i
// svaku dionicu.
type pamcenjeKnjige struct {
	stanja  map[string]map[string]models.StanjeObrane
	dionice map[int][]string
	// mjesta su mjesta svih zaduženja vodočuvara (mozdaUDosegu), pročitana
	// jednom; bez njih (greška čitanja) provjera ide po povijesti
	mjesta          map[string][]models.Duty
	mjestaProcitana bool
}

type kljucPamcenjaKnjige struct{}

// sPamcenjem daje kontekst s novim pamćenjem za jedan popis
func sPamcenjem(ctx context.Context) context.Context {
	return context.WithValue(ctx, kljucPamcenjaKnjige{}, &pamcenjeKnjige{stanja: map[string]map[string]models.StanjeObrane{}, dionice: map[int][]string{}})
}

// pamcenjeIz je pamćenje popisa iz konteksta; nil kad ga nema
func pamcenjeIz(ctx context.Context) *pamcenjeKnjige {
	m, _ := ctx.Value(kljucPamcenjaKnjige{}).(*pamcenjeKnjige)
	return m
}

// SmijeZadati javlja smije li osoba vodočuvaru lista zadati zadatak
func (s *VodocuvarService) SmijeZadati(ctx context.Context, perms *models.UserPermissions, l *models.VodocuvarskiList) bool {
	return s.smijeRadnju(ctx, perms, l, nil, radnjaZadatak)
}

// cuvar je vodočuvar lista ili prijave (oznaka računa id): osoba i
// povijest zaduženja čitaju se tek kad zatrebaju, i to jednom
type cuvar struct {
	id        string
	v         *models.User
	ucitan    bool
	povijest  []models.VerzijaZaduzenja
	procitana bool
}

// cuvarLista je vodočuvar lista l: već poznat s popisa, ili novi, sa
// zadanom osobom v kad je pozivatelj ima; poznati ga pamti za popis (nil ne
// pamti)
func cuvarLista(l *models.VodocuvarskiList, v *models.User, poznati map[string]*cuvar) *cuvar {
	if c, ima := poznati[l.UserID]; ima {
		return c
	}
	c := &cuvar{id: l.UserID, v: v}
	if poznati != nil {
		poznati[l.UserID] = c
	}
	return c
}

// oznaka je oznaka računa vodočuvara
func (c *cuvar) oznaka() string {
	if c.v != nil {
		return c.v.ID.String()
	}
	return c.id
}

// osobaCuvara je vodočuvar sa sadašnjim zaduženjima, pročitan jednom;
// nil kad se ne zna
func (s *VodocuvarService) osobaCuvara(c *cuvar) *models.User {
	if c != nil && c.v == nil && !c.ucitan {
		c.v, c.ucitan = s.osobaPoOznaci(c.id), true
	}
	if c == nil {
		return nil
	}
	return c.v
}

// osobaPoOznaci je osoba s računom te oznake; nil uz neispravnu oznaku,
// nepoznatu osobu ili bez korisnika
func (s *VodocuvarService) osobaPoOznaci(oznaka string) *models.User {
	id, err := uuid.Parse(oznaka)
	if err != nil || s.users == nil {
		return nil
	}
	v, _ := s.users.GetUserByID(id)
	return v
}

// zaduzenjaCuvara su zaduženja vodočuvara koja su vrijedila na dan lista ili
// prijave, u obliku u kojem su tada stajala; za dan prije njegova prvog
// zaduženja u programu ona s kojima je u program ušao
// (models.ZaduzenjaZaZapis). Bez dana ona koja vrijede sada, pa se povijest
// ni ne čita. Bez povijesti (greška čitanja) dan nije vezan ni uz jedno
// zaduženje.
func (s *VodocuvarService) zaduzenjaCuvara(c *cuvar, dan time.Time) []models.Duty {
	if dan.IsZero() {
		if v := s.osobaCuvara(c); v != nil {
			return models.VazecaZaduzenja(v.Duties, time.Now())
		}
		return nil
	}
	return models.ZaduzenjaZaZapis(s.povijestCuvara(c), dan)
}

// povijestCuvara je povijest zaduženja vodočuvara, pročitana jednom; bez
// vodočuvara, izvora ili uz grešku čitanja nijedna
func (s *VodocuvarService) povijestCuvara(c *cuvar) []models.VerzijaZaduzenja {
	if c == nil {
		return nil
	}
	id, err := uuid.Parse(c.oznaka())
	if !c.procitana && err == nil && s.users != nil {
		c.povijest, _ = s.users.PovijestZaduzenja(id)
		c.procitana = true
	}
	return c.povijest
}

// cita javlja smije li osoba čitati list ili, kad je prijava zadana,
// objavljenu prijavu vodočuvara s tim listom: vodočuvar svoje, oni koji
// ovjeravaju, administracija i svaka dužnost u čijem je dosegu list, a
// rukovoditelj dionice od prijava samo one vezane uz svoju dionicu.
// poznati pamti vodočuvare prijava već pročitane (popis prijava); nil ne pamti.
func (s *VodocuvarService) cita(ctx context.Context, perms *models.UserPermissions, l *models.VodocuvarskiList, p *models.PrijavaSTerena, poznati map[string]*cuvar) bool {
	if perms == nil || l == nil || !perms.User.VidiVodocuvarskiDnevnik() {
		return false
	}
	if perms.User.ID.String() == l.UserID || s.SmijeOvjeriti(perms, l) || perms.CanAdminister(l.Sektor, l.AreaID) {
		return true
	}
	c := cuvarLista(l, nil, poznati)
	sad := time.Now()
	for _, d := range perms.User.Duties {
		if s.uDosegu(ctx, d, l, c, l.Datum, sad) && s.citaPrijavu(ctx, d, l, p, c) {
			return true
		}
	}
	return false
}

// citaPrijavu: bez prijave (čita se list) ili za ulogu koja nije
// rukovoditelj dionice nema ograničenja; rukovoditelj dionice čita prijavu
// vezanu uz svoju dionicu: onu koja pokazuje na nju, a onu koja ne
// pokazuje ni na jednu dionicu kao list njezina vodočuvara toga dana (na
// području dužnosti). Kad se ne zna na koje sve dionice prijava pokazuje
// (greška čitanja registra objekata), vezana je samo ako navodi njegovu
// dionicu.
func (s *VodocuvarService) citaPrijavu(ctx context.Context, d models.Duty, l *models.VodocuvarskiList, p *models.PrijavaSTerena, c *cuvar) bool {
	if p == nil || d.Role.OvlastKnjige() != models.KnjigaDionica {
		return true
	}
	moje := d.Dionice()
	if len(moje) == 0 {
		return true
	}
	zad := s.zaduzenjaCuvara(c, p.Datum)
	navedene, ok := s.dionicePrijave(ctx, p, zad)
	if len(navedene) > 0 || !ok {
		return imaZajednicku(moje, navedene)
	}
	return cuvaKojuOd(zad, podrucjeDuznosti(d, l), moje)
}

// dionicePrijave su dionice na koje prijava pokazuje na svakom od njezinih
// područja (podrucjaPrijave): dionice koje navodi polje Dionica
// (dioniceUpisa) i dionice objekta koji navodi, iz registra objekata (po
// identifikatoru ili nazivu). Kad se registar objekata ne da pročitati, ok
// je false, a dionice su one koje su se dale pročitati.
func (s *VodocuvarService) dionicePrijave(ctx context.Context, p *models.PrijavaSTerena, zad []models.Duty) (dionice []string, ok bool) {
	ok = true
	for _, a := range podrucjaPrijave(p.AreaID, zad) {
		objekta, err := s.dioniceObjekta(ctx, a, p.ObjektID, p.Objekt)
		dionice = append(append(dionice, s.dioniceUpisa(ctx, p.DionicaCode, a)...), objekta...)
		ok = ok && err == nil
	}
	return dionice, ok
}

// podrucjaPrijave su branjena područja prijave: ono na kojem je napisana i
// područja zaduženja vodočuvara koja su vrijedila na njezin dan (zad), npr.
// privremene ispomoći na drugom području; svako jednom
func podrucjaPrijave(areaID int, zad []models.Duty) []int {
	out := []int{areaID}
	for _, d := range zad {
		if d.Role == models.RoleWaterGuard && d.AreaID != nil && !sadrziPodrucje(out, *d.AreaID) {
			out = append(out, *d.AreaID)
		}
	}
	return out
}

// sadrziPodrucje javlja je li područje među područjima
func sadrziPodrucje(podrucja []int, areaID int) bool {
	for _, a := range podrucja {
		if a == areaID {
			return true
		}
	}
	return false
}

// dioniceObjekta su dionice objekta koji prijava navodi (identifikatorom
// iz registra ili nazivom) na području areaID, iz registra objekata. Bez
// registra nijedna.
func (s *VodocuvarService) dioniceObjekta(ctx context.Context, areaID int, id, naziv string) ([]string, error) {
	if s.objekti == nil {
		return nil, nil
	}
	return s.objekti.DioniceObjekta(ctx, areaID, id, naziv)
}

// dioniceUpisa su dionice na koje pokazuje slobodan upis polja Dionica
// prijave na području areaID: njegove riječi (rijeciUpisa) koje su šifre
// dionica tog područja iz registra, bez obzira na velika slova. Riječ koja
// nije takva šifra (pogrešno upisana šifra, veznik, dionica drugog
// područja) ne pokazuje ni na jednu. Prazan upis registar ni ne čita; kad
// registar za područje nema nijedne dionice (ili se ne da pročitati), svaka
// riječ upisa uzima se kao šifra.
func (s *VodocuvarService) dioniceUpisa(ctx context.Context, upis string, areaID int) []string {
	rijeci := rijeciUpisa(upis)
	if len(rijeci) == 0 {
		return nil
	}
	podrucja := s.dionicePodrucja(areaID)
	if len(podrucja) == 0 {
		return rijeci
	}
	var out []string
	for _, r := range rijeci {
		if sadrziDionicu(podrucja, r) {
			out = append(out, r)
		}
	}
	return out
}

// rijeciUpisa rastavlja slobodan upis na riječi koje bi mogle biti šifre
// dionica: odvaja ih sve osim slova, znamenki, točke i crtice, a točka i
// crtica na kraju ili početku riječi (kraj rečenice) otpadaju
func rijeciUpisa(upis string) []string {
	var out []string
	for _, r := range strings.FieldsFunc(upis, odvajaSifre) {
		if r = strings.Trim(r, ".-"); r != "" {
			out = append(out, r)
		}
	}
	return out
}

// odvajaSifre javlja odvaja li znak riječi upisa: sve osim slova, znamenke,
// točke i crtice
func odvajaSifre(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.' && r != '-'
}

// cuvaKojuOd javlja čuva li vodočuvar po nekom od zaduženja zad ijednu od
// dionica, izričito ili čuvajući cijelo područje
func cuvaKojuOd(zad []models.Duty, areaID int, sifre []string) bool {
	for _, k := range sifre {
		if cuvaDionicu(zad, areaID, k) {
			return true
		}
	}
	return false
}

// imaZajednicku javlja dijele li dva popisa šifri dionica ijednu
func imaZajednicku(a, b []string) bool {
	for _, k := range b {
		if sadrziDionicu(a, k) {
			return true
		}
	}
	return false
}

// sadrziDionicu javlja je li dionica među šiframa, bez obzira na velika slova
func sadrziDionicu(sifre []string, dionica string) bool {
	for _, k := range sifre {
		if strings.EqualFold(k, dionica) {
			return true
		}
	}
	return false
}

// uDosegu javlja vrijedi li dužnost (aktivna, neistekla), smije li
// njezina uloga išta u tuđoj knjizi i pokriva li njezin doseg list: mjesto
// na kojem je napisan, ili mjesto nekog zaduženja vodočuvara koje je
// vrijedilo na dan (bez dana: koje vrijedi sada), npr. privremene ispomoći
// na drugom području. Mjesta zaduženja čitaju se tek kad mjesto lista nije
// u dosegu.
func (s *VodocuvarService) uDosegu(ctx context.Context, d models.Duty, l *models.VodocuvarskiList, c *cuvar, dan, sad time.Time) bool {
	if !d.CitaVodocuvarskiDnevnik(sad) || d.Role.OvlastKnjige() == models.KnjigaNe {
		return false
	}
	if dosegPokriva(d, l.Sektor, l.AreaID) {
		return true
	}
	return s.mozdaUDosegu(ctx, d, c) && cuvaUDosegu(d, s.zaduzenjaDosega(c, dan))
}

// zaduzenjaDosega su zaduženja vodočuvara po kojima list ulazi u doseg
// dužnosti izvan mjesta na kojem je napisan: ona koja su vrijedila na dan
// (models.ZaduzenjaUDanu), a bez dana ona koja vrijede sada. Za dan prije
// vodočuvarova prvog zaduženja u programu nema ih: ne zna se gdje je tada
// čuvao, pa list ostaje u dosegu samo po mjestu na kojem je napisan.
func (s *VodocuvarService) zaduzenjaDosega(c *cuvar, dan time.Time) []models.Duty {
	if dan.IsZero() {
		return s.zaduzenjaCuvara(c, dan)
	}
	return models.ZaduzenjaUDanu(s.povijestCuvara(c), dan)
}

// dosegPokriva javlja pokriva li doseg dužnosti mjesto: dužnost s
// područjem to područje, a dužnost sektora bez područja cijeli sektor
func dosegPokriva(d models.Duty, sektor string, areaID int) bool {
	if d.AreaID != nil {
		return *d.AreaID == areaID
	}
	return d.SectorID != nil && *d.SectorID == sektor && d.ScopeType == models.ScopeSector
}

// cuvaUDosegu javlja je li neko od zaduženja zad zaduženje vodočuvara na
// mjestu u dosegu dužnosti d
func cuvaUDosegu(d models.Duty, zad []models.Duty) bool {
	for _, z := range zad {
		if z.Role == models.RoleWaterGuard && dosegPokriva(d, deref(z.SectorID), derefPodrucje(z.AreaID)) {
			return true
		}
	}
	return false
}

// deref je tekst iz pokazivača; prazan kad ga nema
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// derefPodrucje je branjeno područje iz pokazivača; 0 kad ga nema
func derefPodrucje(a *int) int {
	if a == nil {
		return 0
	}
	return *a
}

// mozdaUDosegu javlja može li vodočuvar uopće imati zaduženje u dosegu
// dužnosti: popis (kontekst s pamćenjem) jednom čita mjesta svih zaduženja
// vodočuvara koja su ikad postojala i po njima preskače vodočuvare koji
// nikad nisu bili u dosegu, bez čitanja njihove povijesti. Bez pamćenja,
// bez izvora ili uz grešku čitanja ne preskače nikoga.
func (s *VodocuvarService) mozdaUDosegu(ctx context.Context, d models.Duty, c *cuvar) bool {
	m := pamcenjeIz(ctx)
	if m == nil || s.users == nil {
		return true
	}
	if !m.mjestaProcitana {
		m.mjesta, _ = s.users.MjestaVodocuvara()
		m.mjestaProcitana = true
	}
	return m.mjesta == nil || cuvaUDosegu(d, m.mjesta[c.oznaka()])
}

// smijeRadnju javlja smije li osoba na tuđem listu parafirati, upisati
// bilješku ili zadati zadatak. v je vodočuvar lista kad ga pozivatelj već
// ima; inače se čita tek kad zatreba (rukovoditelj dionice).
func (s *VodocuvarService) smijeRadnju(ctx context.Context, perms *models.UserPermissions, l *models.VodocuvarskiList, v *models.User, r radnjaUKnjizi) bool {
	if perms == nil || l == nil || perms.User.ID.String() == l.UserID || !perms.User.VidiVodocuvarskiDnevnik() {
		return false
	}
	if s.SmijeOvjeriti(perms, l) {
		return true
	}
	c := cuvarLista(l, v, nil)
	dan, sad := danRadnje(l, r), time.Now()
	for _, d := range perms.User.Duties {
		if s.uDosegu(ctx, d, l, c, dan, sad) && s.duznostSmije(ctx, d, l, c, r) {
			return true
		}
	}
	return false
}

// danRadnje je dan po čijim se zaduženjima vodočuvara gleda doseg radnje:
// zadatak gleda zaduženja koja vrijede sada (nulti dan), parafa i bilješka
// dan lista
func danRadnje(l *models.VodocuvarskiList, r radnjaUKnjizi) time.Time {
	if r == radnjaZadatak {
		return time.Time{}
	}
	return l.Datum
}

// duznostSmije javlja daje li dužnost u čijem je dosegu list radnju
func (s *VodocuvarService) duznostSmije(ctx context.Context, d models.Duty, l *models.VodocuvarskiList, c *cuvar, r radnjaUKnjizi) bool {
	switch d.Role.OvlastKnjige() {
	case models.KnjigaSve:
		return true
	case models.KnjigaBiljeska:
		return r == radnjaBiljeska
	case models.KnjigaDionica:
		return s.dionicarSmije(ctx, d, l, c, r)
	}
	return false
}

// dionicarSmije: rukovoditelj dionice parafira i upisuje bilješku na list
// vezan uz svoju dionicu po zaduženjima vodočuvara za dan lista
// (rukovoditelju svih dionica područja vezan je svaki list u dosegu), a
// zadatak zadaje samo dok na dionici koju vodočuvar sada čuva traje obrana.
// Dionice se gledaju na području dužnosti, i kad list nosi drugo područje
// (ispomoć).
func (s *VodocuvarService) dionicarSmije(ctx context.Context, d models.Duty, l *models.VodocuvarskiList, c *cuvar, r radnjaUKnjizi) bool {
	if r != radnjaZadatak && len(d.Dionice()) == 0 {
		return true
	}
	podrucje := podrucjeDuznosti(d, l)
	if r == radnjaZadatak {
		return s.obranaNaDionicama(ctx, sektorDuznosti(d, l), s.dioniceVeze(ctx, d, s.zaduzenjaCuvara(c, time.Time{}), podrucje))
	}
	return len(s.dioniceVeze(ctx, d, s.zaduzenjaCuvara(c, l.Datum), podrucje)) > 0
}

// podrucjeDuznosti je branjeno područje na kojem dužnost gleda dionice:
// njezino, a bez njega područje lista
func podrucjeDuznosti(d models.Duty, l *models.VodocuvarskiList) int {
	if d.AreaID != nil {
		return *d.AreaID
	}
	return l.AreaID
}

// sektorDuznosti je sektor čiji se akti gledaju za obranu na dionicama
// dužnosti: njezin, a bez njega sektor lista
func sektorDuznosti(d models.Duty, l *models.VodocuvarskiList) string {
	if d.SectorID != nil {
		return *d.SectorID
	}
	return l.Sektor
}

// dioniceVeze su dionice dužnosti rukovoditelja dionice koje na branjenom
// području lista vodočuvar čuva po nekom od zaduženja zad; dužnost bez
// upisanih dionica vodi sve dionice područja iz registra
func (s *VodocuvarService) dioniceVeze(ctx context.Context, d models.Duty, zad []models.Duty, areaID int) []string {
	moje := d.Dionice()
	if len(moje) == 0 {
		moje = s.dionicePodrucjaZa(ctx, areaID)
	}
	var out []string
	for _, k := range moje {
		if cuvaDionicu(zad, areaID, k) {
			out = append(out, k)
		}
	}
	return out
}

// cuvaDionicu javlja čuva li vodočuvar dionicu na području po nekom od
// zaduženja zad
func cuvaDionicu(zad []models.Duty, areaID int, dionica string) bool {
	for _, d := range zad {
		if d.CuvaDionicu(areaID, dionica) {
			return true
		}
	}
	return false
}

// SetObjekti daje servisu registar objekata: prijava koja navodi objekt
// pokazuje na dionice tog objekta
func (s *VodocuvarService) SetObjekti(r *repository.StructureRepository) { s.objekti = r }

// SetDionice daje servisu registar dionica: rukovoditelj dionice bez
// upisanih dionica vodi sve dionice svog područja, pa se obrana za njegove
// zadatke gleda na njima
func (s *VodocuvarService) SetDionice(r *repository.SectionRepository) { s.dionice = r }

// dionicePodrucjaZa su dionicePodrucja, uz pamćenje popisa kad ga
// kontekst ima
func (s *VodocuvarService) dionicePodrucjaZa(ctx context.Context, areaID int) []string {
	m := pamcenjeIz(ctx)
	if m == nil {
		return s.dionicePodrucja(areaID)
	}
	if out, ima := m.dionice[areaID]; ima {
		return out
	}
	m.dionice[areaID] = s.dionicePodrucja(areaID)
	return m.dionice[areaID]
}

// dionicePodrucja su šifre dionica branjenog područja iz registra; bez
// registra, bez područja ili uz grešku čitanja nijedna
func (s *VodocuvarService) dionicePodrucja(areaID int) []string {
	if s.dionice == nil || areaID <= 0 {
		return nil
	}
	sve, err := s.dionice.ListSections("", areaID, "")
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(sve))
	for _, x := range sve {
		out = append(out, x.Code)
	}
	return out
}

// obranaNaDionicama javlja traje li na ijednoj od dionica obrana od
// poplava: redovna ili izvanredna obrana ili izvanredno stanje iz ovjerenih
// akata (pripremno stanje nije obrana). Bez dionica akti se ne čitaju; bez
// izvora stanja obrane, ili kad se akti ne daju pročitati, obrana se ne
// pretpostavlja.
func (s *VodocuvarService) obranaNaDionicama(ctx context.Context, sektor string, dionice []string) bool {
	if s.stanjaObrane == nil || len(dionice) == 0 {
		return false
	}
	stanja := s.stanjaSektora(ctx, sektor)
	for _, k := range dionice {
		if stanja[k].Najvisi().Severity() >= models.PhaseRegular.Severity() {
			return true
		}
	}
	return false
}

// stanjaSektora su stanja obrane dionica sektora sada, jednom po popisu kad
// kontekst ima pamćenje; uz grešku čitanja nijedno
func (s *VodocuvarService) stanjaSektora(ctx context.Context, sektor string) map[string]models.StanjeObrane {
	m := pamcenjeIz(ctx)
	if m != nil {
		if st, ima := m.stanja[sektor]; ima {
			return st
		}
	}
	st, err := s.stanjaObrane(ctx, sektor, time.Now())
	if err != nil {
		st = nil
	}
	if m != nil {
		m.stanja[sektor] = st
	}
	return st
}
