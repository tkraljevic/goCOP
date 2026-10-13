package peers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Pretplate: što ovo računalo prati. Zajednički kanal (ustroj, registri,
// djelatnici) drže svi; očitanja i dnevnici idu po kanalima
// "vrsta/područje/godina" i čvor ih prima samo za pretplaćena područja i
// godine. Uredski čvor prati sve (postavka [sync] sve = true); laptop
// prati svoje područje i zadnje godine, a što više ne treba obriše s
// računala. Pretplate su lokalne: ne putuju razmjenom.

// Subscription je jedno pravilo: vrsta (prazno = obje), sektor ili
// područje (prazno i 0 = sva), godine od-do (0 = bez ograde)
type Subscription struct {
	ID       int    `json:"id"`
	Kind     string `json:"kind"`      // "", "ocitanja", "dnevnici", "prijave"
	SectorID string `json:"sector_id"` // cijeli sektor
	AreaID   int    `json:"area_id"`   // jedno područje
	YearFrom int    `json:"year_from"`
	YearTo   int    `json:"year_to"`
	// Razina kaže koliko sadržaja uz zapise čvor dohvaća: "kazalo" ništa
	// (zna da dokument postoji), "pregled" samo slike, "sve" i PDF-ove.
	// DrziDana je koliko dana primljeni sadržaj ostaje na računalu; 0 = trajno.
	Razina   string `json:"razina,omitempty"`
	DrziDana int    `json:"drzi_dana,omitempty"`
}

// Razine sadržaja, od najmanje prema najvećoj
const (
	RazinaKazalo  = "kazalo"
	RazinaPregled = "pregled"
	RazinaSve     = "sve"
)

func razinaRed(r string) int {
	switch r {
	case RazinaKazalo:
		return 0
	case RazinaPregled:
		return 1
	}
	return 2
}

// Label je pravilo ljudskim jezikom
func (s Subscription) Label() string {
	what := "sve vrste"
	switch s.Kind {
	case ledger.ChannelReadings:
		what = "očitanja"
	case ledger.ChannelJournals:
		what = "dnevnici"
	case ledger.ChannelPrijave:
		what = "prijave s terena"
	case ledger.ChannelVodocuvar:
		what = "vodočuvarski dnevnik"
	case ledger.ChannelArhiva:
		what = "arhiva vodostaja"
	}
	where := "sva područja"
	switch {
	case s.AreaID > 0:
		where = fmt.Sprintf("BP %d", s.AreaID)
	case s.SectorID != "":
		where = "sektor " + s.SectorID
	}
	when := "sve godine"
	switch {
	case s.YearFrom > 0 && s.YearTo > 0 && s.YearFrom != s.YearTo:
		when = fmt.Sprintf("%d.–%d.", s.YearFrom, s.YearTo)
	case s.YearFrom > 0 && s.YearTo > 0:
		when = fmt.Sprintf("%d.", s.YearFrom)
	case s.YearFrom > 0:
		when = fmt.Sprintf("od %d.", s.YearFrom)
	case s.YearTo > 0:
		when = fmt.Sprintf("do %d.", s.YearTo)
	}
	if s.Kind == ledger.ChannelArhiva {
		when = "cijela povijest" // paket je cijela letva, ne godina
	}
	out := what + ", " + where + ", " + when
	switch s.Razina {
	case RazinaKazalo:
		out += ", samo kazalo"
	case RazinaPregled:
		out += ", samo pregledi"
	}
	if s.DrziDana > 0 {
		out += fmt.Sprintf(", sadržaj %d %s", s.DrziDana, models.UzBrojHR(s.DrziDana, "dan", "dana", "dana"))
	}
	return out
}

// Wants je ono što čvor traži od drugoga u razmjeni: sve, ili po pravilima
type Wants struct {
	All   bool           `json:"all"`
	Rules []Subscription `json:"rules,omitempty"`
}

// matches javlja pokriva li pravilo kanal; sectorOf daje sektor područja
func (s Subscription) matches(kind string, areaID, year int, sectorOf func(int) string) bool {
	if s.Kind != "" && s.Kind != kind {
		return false
	}
	if !s.pokrivaPodrucje(areaID, sectorOf) {
		return false
	}
	// Paket arhive nosi cijelu povijest letve i kanal mu nema godine
	// (arhiva/područje/0), pa godine pravila za njega ne vrijede: ni uz
	// pravilo „arhiva”, ni uz „sve vrste” za područje ili sektor, ni uz „sve
	// vrste” za sva područja, koje tako donosi cijelu arhivu, sa stranim i
	// meteorološkim letvama (Odluke (13. 10. 2026.)). Laptop s pravilom „sve
	// vrste, BP 16, 2025.–2026.” do 0.0.38 nije dobio arhivu svog područja.
	return kind == ledger.ChannelArhiva || s.pokrivaGodinu(year)
}

// pokrivaPodrucje: područje pravila ima prednost pred sektorom, a pravilo
// bez njih pokriva svako područje
func (s Subscription) pokrivaPodrucje(areaID int, sectorOf func(int) string) bool {
	switch {
	case s.AreaID > 0:
		return s.AreaID == areaID
	case s.AreaID == 0 && s.SectorID != "":
		return sectorOf(areaID) == s.SectorID
	}
	return true
}

// pokrivaGodinu: godina 0 u pravilu znači bez ograde s te strane
func (s Subscription) pokrivaGodinu(year int) bool {
	return (s.YearFrom <= 0 || year >= s.YearFrom) && (s.YearTo <= 0 || year <= s.YearTo)
}

// Match javlja želi li čvor s ovim pretplatama taj kanal
func (w Wants) Match(channel string, sectorOf func(int) string) bool {
	if channel == "" || w.All {
		return true
	}
	kind, areaID, year := ledger.SplitChannel(channel)
	for _, r := range w.Rules {
		if r.matches(kind, areaID, year, sectorOf) {
			return true
		}
	}
	return false
}

// RazinaZa kaže koliko sadržaja čvor želi uz zapise kanala: najviša razina
// među pravilima koja kanal pokrivaju; "sve" za zajednički kanal i za čvor
// koji prati sve
func (w Wants) RazinaZa(channel string, sectorOf func(int) string) string {
	if channel == "" || w.All {
		return RazinaSve
	}
	kind, areaID, year := ledger.SplitChannel(channel)
	best := -1
	for _, r := range w.Rules {
		if r.matches(kind, areaID, year, sectorOf) && razinaRed(r.Razina) > best {
			best = razinaRed(r.Razina)
		}
	}
	switch best {
	case 0:
		return RazinaKazalo
	case 1:
		return RazinaPregled
	case 2:
		return RazinaSve
	}
	return RazinaKazalo
}

// DrziDanaZa kaže koliko dana primljeni sadržaj kanala ostaje: najdulje
// među pravilima koja kanal pokrivaju; 0 znači trajno
func (w Wants) DrziDanaZa(channel string, sectorOf func(int) string) int {
	if channel == "" || w.All {
		return 0
	}
	kind, areaID, year := ledger.SplitChannel(channel)
	najdulje, ima := 0, false
	for _, r := range w.Rules {
		if !r.matches(kind, areaID, year, sectorOf) {
			continue
		}
		if r.DrziDana == 0 {
			return 0
		}
		if !ima || r.DrziDana > najdulje {
			najdulje, ima = r.DrziDana, true
		}
	}
	return najdulje
}

// zeliSadrzaj javlja dohvaća li čvor s tim pretplatama taj sadržaj. Paket
// arhive stiže čim ga pravilo pokriva, bez obzira na razinu: to su podaci
// vodostaja, a ne PDF ili slika uz zapis (Odluke (13. 10. 2026.)).
func (w Wants) zeliSadrzaj(channel, vrsta string, sectorOf func(int) string) bool {
	if kind, _, _ := ledger.SplitChannel(channel); kind == ledger.ChannelArhiva {
		return w.Match(channel, sectorOf)
	}
	switch w.RazinaZa(channel, sectorOf) {
	case RazinaSve:
		return true
	case RazinaPregled:
		return strings.HasPrefix(vrsta, "image/")
	}
	return false
}

// ---------- što ovaj čvor prati ----------

// SetWantsAll postavlja čvor da prati sve kanale (uredski čvor)
func (s *Service) SetWantsAll(all bool) {
	s.mu.Lock()
	s.wantsAll = all
	s.mu.Unlock()
}

// WantsAll javlja prati li čvor sve
func (s *Service) WantsAll() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.wantsAll
}

// ListSubscriptions vraća pravila ovog čvora
func (s *Service) ListSubscriptions(ctx context.Context) ([]Subscription, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, sector_id, area_id, year_from, year_to, razina, drzi_dana FROM subscriptions ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subscription
	for rows.Next() {
		var r Subscription
		if err := rows.Scan(&r.ID, &r.Kind, &r.SectorID, &r.AreaID, &r.YearFrom, &r.YearTo, &r.Razina, &r.DrziDana); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// vrstaPretplate javlja smije li pretplata imati tu vrstu; prazna su sve
// vrste. Arhiva je na popisu od 0.0.38: stranica pretplata nudila ju je od
// 0.0.7, a upis ju je odbijao.
func vrstaPretplate(kind string) bool {
	switch kind {
	case "", ledger.ChannelReadings, ledger.ChannelJournals, ledger.ChannelPrijave, ledger.ChannelVodocuvar, ledger.ChannelArhiva:
		return true
	}
	return false
}

// razinaPravila provjerava razinu sadržaja pravila; prazna je „sve”. Pravilo
// samo za arhivu pamti se sa „sve”, jer paket arhive stiže bez obzira na
// razinu (Odluke (13. 10. 2026.)), pa ga opis ne smije zvati „samo kazalo”.
func razinaPravila(kind, razina string) (string, error) {
	switch razina {
	case "", RazinaSve, RazinaKazalo, RazinaPregled:
	default:
		return razina, fmt.Errorf("Pretplata nije dodana: sadržaj uz zapise mora biti sve, samo pregledi ili samo kazalo.")
	}
	if razina == "" || kind == ledger.ChannelArhiva {
		return RazinaSve, nil
	}
	return razina, nil
}

// godineIRokPravila odbija godine obrnutim redom; rok manji od nule znači
// trajno, kao i prazan rok. Pravilo za arhivu vodostaja godine ne pamti:
// paket nosi cijelu povijest letve, a obrazac sam upisuje početnu godinu,
// pa bi se ista arhiva dala dodati dvaput, sa zadanom godinom i bez nje.
func godineIRokPravila(r *Subscription) error {
	if r.Kind == ledger.ChannelArhiva {
		r.YearFrom, r.YearTo = 0, 0
	}
	if r.YearFrom > 0 && r.YearTo > 0 && r.YearTo < r.YearFrom {
		return fmt.Errorf("Pretplata nije dodana: godina u polju „Do godine” je prije godine u polju „Od godine”.")
	}
	if r.DrziDana < 0 {
		r.DrziDana = 0
	}
	return nil
}

// AddSubscription upisuje pravilo; sljedeća razmjena donosi što mu pripada
func (s *Service) AddSubscription(ctx context.Context, r Subscription) (Subscription, error) {
	r.Kind = strings.TrimSpace(r.Kind)
	if !vrstaPretplate(r.Kind) {
		return r, fmt.Errorf("Pretplata nije dodana: vrsta mora biti očitanja, dnevnici, prijave s terena, vodočuvarski dnevnik, arhiva vodostaja ili sve vrste.")
	}
	var err error
	if r.Razina, err = razinaPravila(r.Kind, r.Razina); err != nil {
		return r, err
	}
	if err = godineIRokPravila(&r); err != nil {
		return r, err
	}
	r.SectorID = strings.ToUpper(strings.TrimSpace(r.SectorID))
	if err = s.provjeriDvostruko(ctx, r); err != nil {
		return r, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO subscriptions (kind, sector_id, area_id, year_from, year_to, razina, drzi_dana) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.Kind, r.SectorID, r.AreaID, r.YearFrom, r.YearTo, r.Razina, r.DrziDana)
	if err != nil {
		return r, fmt.Errorf("upis pretplate: %w", err)
	}
	id, _ := res.LastInsertId()
	r.ID = int(id)
	return r, nil
}

// ErrIstoPravilo javlja da je isto pravilo već na popisu: drugo ne donosi
// ništa novo, a popis čini nejasnim
var ErrIstoPravilo = errors.New("Pretplata nije dodana: isto pravilo već postoji.")

// provjeriDvostruko odbija pravilo koje se u svemu poklapa s postojećim
func (s *Service) provjeriDvostruko(ctx context.Context, r Subscription) error {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM subscriptions WHERE kind = ? AND sector_id = ? AND area_id = ?
		AND year_from = ? AND year_to = ? AND razina = ? AND drzi_dana = ?`,
		r.Kind, r.SectorID, r.AreaID, r.YearFrom, r.YearTo, r.Razina, r.DrziDana).Scan(&n)
	switch {
	case err != nil:
		return fmt.Errorf("upis pretplate: %w", err)
	case n > 0:
		return ErrIstoPravilo
	}
	return nil
}

// RemoveSubscription briše pravilo; podaci ostaju dok ih korisnik ne obriše
// (PurgeChannel)
func (s *Service) RemoveSubscription(ctx context.Context, id int) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM subscriptions WHERE id = ?`, id)
	return err
}

// CurrentWants su pretplate ovog čvora kakve idu drugome u razmjeni
func (s *Service) CurrentWants(ctx context.Context) (Wants, error) {
	if s.WantsAll() {
		return Wants{All: true}, nil
	}
	rules, err := s.ListSubscriptions(ctx)
	if err != nil {
		return Wants{}, err
	}
	return Wants{Rules: rules}, nil
}

// sectorLookup vraća sektor područja iz ustroja, s predmemorijom za jednu razmjenu
func (s *Service) sectorLookup(ctx context.Context) func(int) string {
	var once sync.Once
	cache := map[int]string{}
	return func(areaID int) string {
		once.Do(func() {
			rows, err := s.db.QueryContext(ctx, `SELECT id, sector_id FROM areas`)
			if err != nil {
				return
			}
			defer rows.Close()
			for rows.Next() {
				var id int
				var sector string
				if rows.Scan(&id, &sector) == nil {
					cache[id] = sector
				}
			}
		})
		return cache[areaID]
	}
}

// wantsFunc pretvara tuđe (ili vlastite) pretplate u ogradu za Delta; nil
// znači sve, što vrijedi za čvor koji pretplate još ne šalje
func (s *Service) wantsFunc(ctx context.Context, w *Wants) func(string) bool {
	if w == nil || w.All {
		return nil
	}
	sectorOf := s.sectorLookup(ctx)
	return func(channel string) bool { return w.Match(channel, sectorOf) }
}

// ---------- brisanje s ovog računala ----------

// ChannelStat je jedan kanal na ovom čvoru s brojem verzija i zapisa
type ChannelStat struct {
	Channel  string `json:"channel"`
	Kind     string `json:"kind"`
	AreaID   int    `json:"area_id"`
	Year     int    `json:"year"`
	Versions int    `json:"versions"`
	Wanted   bool   `json:"wanted"` // pokriva ga pretplata
}

// Channels vraća kanale koje ovaj čvor drži, s brojem verzija i je li ih
// pretplata još pokriva; što nije pokriveno, smije se obrisati
func (s *Service) Channels(ctx context.Context) ([]ChannelStat, error) {
	counts, err := s.rec.CountByChannel(ctx)
	if err != nil {
		return nil, err
	}
	wants, err := s.CurrentWants(ctx)
	if err != nil {
		return nil, err
	}
	sectorOf := s.sectorLookup(ctx)
	var out []ChannelStat
	for ch, n := range counts {
		if ch == "" {
			continue
		}
		kind, area, year := ledger.SplitChannel(ch)
		out = append(out, ChannelStat{Channel: ch, Kind: kind, AreaID: area, Year: year, Versions: n, Wanted: wants.Match(ch, sectorOf)})
	}
	sortChannels(out)
	return out, nil
}

func sortChannels(list []ChannelStat) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && channelLess(list[j], list[j-1]); j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

func channelLess(a, b ChannelStat) bool {
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	if a.AreaID != b.AreaID {
		return a.AreaID < b.AreaID
	}
	return a.Year > b.Year
}

// ZeliSadrzaj javlja hoće li ovaj čvor po svojim pretplatama dohvatiti
// sadržaj kanala te vrste. Tko bilježi što treba dohvatiti, pita prije
// bilježenja: neželjene želje bi inače zauzele mjesta u redu za dohvat.
func (s *Service) ZeliSadrzaj(ctx context.Context, kanal, vrsta string) bool {
	w, err := s.CurrentWants(ctx)
	if err != nil {
		return false
	}
	return w.zeliSadrzaj(kanal, vrsta, s.sectorLookup(ctx))
}
