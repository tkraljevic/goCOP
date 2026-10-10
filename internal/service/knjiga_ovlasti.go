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

// cuvar je vodočuvar lista ili prijave s poviješću zaduženja, koja se
// čita tek kad zatreba, i to jednom
type cuvar struct {
	v         *models.User
	povijest  []models.VerzijaZaduzenja
	procitana bool
}

// zaduzenjaCuvara su zaduženja vodočuvara koja su vrijedila na dan lista ili
// prijave, u obliku u kojem su tada stajala; za dan prije njegova prvog
// zaduženja u programu ona s kojima je u program ušao
// (models.ZaduzenjaZaZapis). Bez dana ona koja vrijede sada, pa se povijest
// ni ne čita. Bez povijesti (greška čitanja) dan nije vezan ni uz jedno
// zaduženje.
func (s *VodocuvarService) zaduzenjaCuvara(c *cuvar, dan time.Time) []models.Duty {
	if c == nil || c.v == nil {
		return nil
	}
	if dan.IsZero() {
		return models.VazecaZaduzenja(c.v.Duties, time.Now())
	}
	if !c.procitana && s.users != nil {
		c.povijest, _ = s.users.PovijestZaduzenja(c.v.ID)
		c.procitana = true
	}
	return models.ZaduzenjaZaZapis(c.povijest, dan)
}

// cita javlja smije li osoba čitati list ili, kad je prijava zadana,
// objavljenu prijavu vodočuvara s tim listom: vodočuvar svoje, oni koji
// ovjeravaju, administracija i svaka dužnost u čijem je dosegu list, a
// rukovoditelj dionice od prijava samo one vezane uz svoju dionicu.
// poznati pamti vodočuvare prijava već pročitane (popis prijava); nil ne pamti.
func (s *VodocuvarService) cita(perms *models.UserPermissions, l *models.VodocuvarskiList, p *models.PrijavaSTerena, poznati map[string]*cuvar) bool {
	if perms == nil || l == nil || !perms.User.VidiVodocuvarskiDnevnik() {
		return false
	}
	if perms.User.ID.String() == l.UserID || s.SmijeOvjeriti(perms, l) || perms.CanAdminister(l.Sektor, l.AreaID) {
		return true
	}
	sad := time.Now()
	for _, d := range perms.User.Duties {
		if duznostUDosegu(d, l, sad) && s.citaPrijavu(d, p, poznati) {
			return true
		}
	}
	return false
}

// citaPrijavu: bez prijave (čita se list) ili za ulogu koja nije
// rukovoditelj dionice nema ograničenja; rukovoditelj dionice čita prijavu
// vezanu uz svoju dionicu: onu koja pokazuje na nju, a onu koja ne
// pokazuje ni na jednu dionicu kao list njezina vodočuvara toga dana. Kad
// se ne zna na koje sve dionice prijava pokazuje (greška čitanja registra
// objekata), vezana je samo ako navodi njegovu dionicu.
func (s *VodocuvarService) citaPrijavu(d models.Duty, p *models.PrijavaSTerena, poznati map[string]*cuvar) bool {
	if p == nil || d.Role.OvlastKnjige() != models.KnjigaDionica {
		return true
	}
	moje := d.Dionice()
	if len(moje) == 0 {
		return true
	}
	navedene, ok := s.dionicePrijave(p)
	if len(navedene) > 0 || !ok {
		return imaZajednicku(moje, navedene)
	}
	return cuvaKojuOd(s.zaduzenjaCuvara(s.vodocuvarPrijave(p, poznati), p.Datum), p.AreaID, moje)
}

// dionicePrijave su dionice na koje prijava pokazuje: dionice područja
// prijave koje navodi polje Dionica (dioniceUpisa) i dionice objekta koji
// navodi, iz registra objekata (po identifikatoru ili nazivu, na području
// prijave). Kad se registar objekata ne da pročitati, ok je false, a
// dionice su samo one iz polja Dionica.
func (s *VodocuvarService) dionicePrijave(p *models.PrijavaSTerena) (dionice []string, ok bool) {
	dionice = s.dioniceUpisa(p.DionicaCode, p.AreaID)
	if s.objekti == nil {
		return dionice, true
	}
	objekta, err := s.objekti.DioniceObjekta(context.Background(), p.AreaID, p.ObjektID, p.Objekt)
	return append(dionice, objekta...), err == nil
}

// dioniceUpisa su dionice na koje pokazuje slobodan upis polja Dionica
// prijave na području areaID: njegove riječi (rijeciUpisa) koje su šifre
// dionica tog područja iz registra, bez obzira na velika slova. Riječ koja
// nije takva šifra (pogrešno upisana šifra, veznik, dionica drugog
// područja) ne pokazuje ni na jednu. Prazan upis registar ni ne čita; kad
// registar za područje nema nijedne dionice (ili se ne da pročitati), svaka
// riječ upisa uzima se kao šifra.
func (s *VodocuvarService) dioniceUpisa(upis string, areaID int) []string {
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

// vodocuvarPrijave je vodočuvar koji je objavio prijavu; poznati, kad je
// zadan, pamti već pročitane, da popis prijava svakoga (i njegova prošla
// zaduženja) čita jednom
func (s *VodocuvarService) vodocuvarPrijave(p *models.PrijavaSTerena, poznati map[string]*cuvar) *cuvar {
	if c, ima := poznati[p.UserID]; ima {
		return c
	}
	c := &cuvar{v: s.vodocuvarLista(probniList(p), nil)}
	if poznati != nil {
		poznati[p.UserID] = c
	}
	return c
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

// duznostUDosegu javlja vrijedi li dužnost (aktivna, neistekla), smije li
// njezina uloga išta u tuđoj knjizi i pokriva li njezin doseg list: dužnost
// s područjem to područje, a dužnost sektora bez područja cijeli sektor
func duznostUDosegu(d models.Duty, l *models.VodocuvarskiList, sad time.Time) bool {
	if !d.CitaVodocuvarskiDnevnik(sad) || d.Role.OvlastKnjige() == models.KnjigaNe {
		return false
	}
	if d.AreaID != nil {
		return *d.AreaID == l.AreaID
	}
	return d.SectorID != nil && *d.SectorID == l.Sektor && d.ScopeType == models.ScopeSector
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
	sad := time.Now()
	for _, d := range perms.User.Duties {
		if duznostUDosegu(d, l, sad) && s.duznostSmije(ctx, d, l, v, r) {
			return true
		}
	}
	return false
}

// duznostSmije javlja daje li dužnost u čijem je dosegu list radnju
func (s *VodocuvarService) duznostSmije(ctx context.Context, d models.Duty, l *models.VodocuvarskiList, v *models.User, r radnjaUKnjizi) bool {
	switch d.Role.OvlastKnjige() {
	case models.KnjigaSve:
		return true
	case models.KnjigaBiljeska:
		return r == radnjaBiljeska
	case models.KnjigaDionica:
		return s.dionicarSmije(ctx, d, l, v, r)
	}
	return false
}

// dionicarSmije: rukovoditelj dionice parafira i upisuje bilješku na list
// vezan uz svoju dionicu po zaduženjima vodočuvara za dan lista
// (rukovoditelju svih dionica područja vezan je svaki list područja), a
// zadatak zadaje samo dok na dionici koju vodočuvar sada čuva traje obrana
func (s *VodocuvarService) dionicarSmije(ctx context.Context, d models.Duty, l *models.VodocuvarskiList, v *models.User, r radnjaUKnjizi) bool {
	if r != radnjaZadatak && len(d.Dionice()) == 0 {
		return true
	}
	c := &cuvar{v: s.vodocuvarLista(l, v)}
	if r == radnjaZadatak {
		return s.obranaNaDionicama(ctx, l.Sektor, s.dioniceVeze(ctx, d, s.zaduzenjaCuvara(c, time.Time{}), l.AreaID))
	}
	return len(s.dioniceVeze(ctx, d, s.zaduzenjaCuvara(c, l.Datum), l.AreaID)) > 0
}

// vodocuvarLista je vodočuvar čiji je list: zadan, ili pročitan po listu;
// nil kad se ne zna
func (s *VodocuvarService) vodocuvarLista(l *models.VodocuvarskiList, v *models.User) *models.User {
	if v != nil || s.users == nil {
		return v
	}
	id, err := uuid.Parse(l.UserID)
	if err != nil {
		return nil
	}
	v, _ = s.users.GetUserByID(id)
	return v
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
