package peers

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"

	"gocop/internal/ledger"
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
	Kind     string `json:"kind"`      // "", "ocitanja", "dnevnici"
	SectorID string `json:"sector_id"` // cijeli sektor
	AreaID   int    `json:"area_id"`   // jedno područje
	YearFrom int    `json:"year_from"`
	YearTo   int    `json:"year_to"`
}

// Label je pravilo ljudskim jezikom
func (s Subscription) Label() string {
	what := "očitanja i dnevnici"
	switch s.Kind {
	case ledger.ChannelReadings:
		what = "očitanja"
	case ledger.ChannelJournals:
		what = "dnevnici"
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
	return what + ", " + where + ", " + when
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
	if s.AreaID > 0 && s.AreaID != areaID {
		return false
	}
	if s.SectorID != "" && s.AreaID == 0 && sectorOf(areaID) != s.SectorID {
		return false
	}
	if s.YearFrom > 0 && year < s.YearFrom {
		return false
	}
	if s.YearTo > 0 && year > s.YearTo {
		return false
	}
	return true
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
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, sector_id, area_id, year_from, year_to FROM subscriptions ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subscription
	for rows.Next() {
		var r Subscription
		if err := rows.Scan(&r.ID, &r.Kind, &r.SectorID, &r.AreaID, &r.YearFrom, &r.YearTo); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AddSubscription upisuje pravilo; sljedeća razmjena donosi što mu pripada
func (s *Service) AddSubscription(ctx context.Context, r Subscription) (Subscription, error) {
	r.Kind = strings.TrimSpace(r.Kind)
	if r.Kind != "" && r.Kind != ledger.ChannelReadings && r.Kind != ledger.ChannelJournals {
		return r, fmt.Errorf("vrsta je očitanja, dnevnici ili oboje")
	}
	r.SectorID = strings.ToUpper(strings.TrimSpace(r.SectorID))
	if r.YearFrom > 0 && r.YearTo > 0 && r.YearTo < r.YearFrom {
		return r, fmt.Errorf("godina do ne može biti prije godine od")
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO subscriptions (kind, sector_id, area_id, year_from, year_to) VALUES (?, ?, ?, ?, ?)`,
		r.Kind, r.SectorID, r.AreaID, r.YearFrom, r.YearTo)
	if err != nil {
		return r, fmt.Errorf("upis pretplate: %w", err)
	}
	id, _ := res.LastInsertId()
	r.ID = int(id)
	return r, nil
}

// RemoveSubscription briše pravilo; podaci ostaju dok ih PurgeUnwanted ne makne
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

// PurgeChannel briše jedan kanal s ovog računala: verzije iz knjige i
// zapise s površine. Kanal koji pretplata još pokriva ne briše se, jer bi
// se vratio prvom razmjenom; prvo se makne pretplata.
func (s *Service) PurgeChannel(ctx context.Context, channel string) (int64, error) {
	wants, err := s.CurrentWants(ctx)
	if err != nil {
		return 0, err
	}
	if wants.Match(channel, s.sectorLookup(ctx)) {
		return 0, fmt.Errorf("kanal %s još je pod pretplatom; prvo maknite pretplatu", channel)
	}
	kind, _, _ := ledger.SplitChannel(channel)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	switch kind {
	case ledger.ChannelReadings:
		if _, err := tx.ExecContext(ctx, `DELETE FROM readings WHERE channel = ?`, channel); err != nil {
			return 0, err
		}
	case ledger.ChannelJournals:
		for _, stmt := range []string{
			`DELETE FROM journal_entries WHERE journal_id IN (SELECT id FROM journals WHERE channel = ?)`,
			`DELETE FROM journal_sheets WHERE journal_id IN (SELECT id FROM journals WHERE channel = ?)`,
			`DELETE FROM journals WHERE channel = ?`,
		} {
			if _, err := tx.ExecContext(ctx, stmt, channel); err != nil {
				return 0, err
			}
		}
	default:
		return 0, fmt.Errorf("nepoznata vrsta kanala %q", channel)
	}
	n, err := s.rec.PurgeChannel(ctx, tx, channel)
	if err != nil {
		return 0, err
	}
	return n, tx.Commit()
}

// PurgeUnwanted briše sve kanale koje pretplata više ne pokriva
func (s *Service) PurgeUnwanted(ctx context.Context) (map[string]int64, error) {
	list, err := s.Channels(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, c := range list {
		if c.Wanted {
			continue
		}
		n, err := s.PurgeChannel(ctx, c.Channel)
		if err != nil {
			return out, err
		}
		out[c.Channel] = n
	}
	return out, nil
}

// unused guard so sql import stays meaningful when queries move
var _ = sql.ErrNoRows
