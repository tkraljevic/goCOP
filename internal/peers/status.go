package peers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"gocop/internal/ledger"
)

// Stanje sinkronizacije po čvoru. Za razliku od zapisa čvora (koji putuje),
// ovo je odnos OVOG čvora s tim čvorom i ostaje lokalno: kad je zadnji put
// pokušano, kad je uspjelo, koliko je trajalo, dokle drugi zna (njegova
// granica, frontier) i koliko puta zaredom nije odgovorio. Iz toga
// nadzorna ploča zna tko je na mreži, tko zaostaje i gdje zapinje.

// SyncState je zadnje poznato stanje razmjene s jednim čvorom
type SyncState struct {
	LastAttempt *time.Time        `json:"last_attempt,omitempty"`
	LastOK      *time.Time        `json:"last_ok,omitempty"`
	LastError   string            `json:"last_error,omitempty"`
	Applied     int               `json:"applied"`     // primljeno u zadnjoj uspješnoj razmjeni
	Sent        int               `json:"sent"`        // poslano u zadnjoj uspješnoj razmjeni
	DurationMs  int               `json:"duration_ms"` // trajanje zadnje razmjene
	Fails       int               `json:"fails"`       // neuspjeli pokušaji zaredom
	Frontier    map[string]string `json:"-"`           // dokle drugi čvor zna, po autoru
}

// syncOutcome je ishod jedne razmjene za bilješku
type syncOutcome struct {
	applied, sent int
	frontier      map[string]string
	took          time.Duration
	err           error
}

// recordSyncState upisuje ishod razmjene u lokalno stanje
func (s *Service) recordSyncState(ctx context.Context, nodeID string, o syncOutcome) {
	now := time.Now().UTC()
	frontier := "{}"
	if o.frontier != nil {
		if b, err := json.Marshal(o.frontier); err == nil {
			frontier = string(b)
		}
	}
	var err error
	if o.err == nil {
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO peer_sync (node_id, their_frontier, last_attempt, last_ok, last_error, applied, sent, duration_ms, fails)
			VALUES (?, ?, ?, ?, '', ?, ?, ?, 0)
			ON CONFLICT(node_id) DO UPDATE SET their_frontier = excluded.their_frontier, last_attempt = excluded.last_attempt,
				last_ok = excluded.last_ok, last_error = '', applied = excluded.applied, sent = excluded.sent,
				duration_ms = excluded.duration_ms, fails = 0`,
			nodeID, frontier, now, now, o.applied, o.sent, o.took.Milliseconds())
	} else {
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO peer_sync (node_id, their_frontier, last_attempt, last_ok, last_error, applied, sent, duration_ms, fails)
			VALUES (?, '{}', ?, NULL, ?, 0, 0, ?, 1)
			ON CONFLICT(node_id) DO UPDATE SET last_attempt = excluded.last_attempt, last_error = excluded.last_error,
				duration_ms = excluded.duration_ms, fails = peer_sync.fails + 1`,
			nodeID, now, o.err.Error(), o.took.Milliseconds())
	}
	if err != nil {
		fmt.Printf("sinkronizacija: stanje za %s nije spremljeno: %v\n", nodeID, err)
	}
}

// syncStates čita stanje razmjene za sve čvorove
func (s *Service) syncStates(ctx context.Context) (map[string]SyncState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT node_id, their_frontier, last_attempt, last_ok, last_error, applied, sent, duration_ms, fails FROM peer_sync`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]SyncState{}
	for rows.Next() {
		var id, frontier string
		var st SyncState
		var attempt, ok sql.NullTime
		if err := rows.Scan(&id, &frontier, &attempt, &ok, &st.LastError, &st.Applied, &st.Sent, &st.DurationMs, &st.Fails); err != nil {
			return nil, err
		}
		if attempt.Valid {
			t := attempt.Time.UTC()
			st.LastAttempt = &t
		}
		if ok.Valid {
			t := ok.Time.UTC()
			st.LastOK = &t
		}
		_ = json.Unmarshal([]byte(frontier), &st.Frontier)
		out[id] = st
	}
	return out, rows.Err()
}

// ---------- raspored ----------

// SetInterval pamti razmak automatske sinkronizacije; iz njega slijedi što
// je "svježe", a što "šuti"
func (s *Service) SetInterval(every time.Duration) {
	s.mu.Lock()
	s.every = every
	s.mu.Unlock()
}

func (s *Service) interval() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.every <= 0 {
		return 5 * time.Minute
	}
	return s.every
}

// due javlja je li red na taj čvor: stalno izloženi i oni koji odgovaraju
// zovu se svaki put, a tko redom šuti zove se sve rjeđe (do 8 razmaka),
// da desetak ugašenih laptopa ne troši svaki krug
func due(p Peer, st SyncState, every time.Duration, now time.Time) bool {
	if p.IsBootstrap || st.Fails == 0 || st.LastAttempt == nil {
		return true
	}
	backoff := st.Fails
	if backoff > 8 {
		backoff = 8
	}
	return now.Sub(*st.LastAttempt) >= time.Duration(backoff)*every
}

// syncPeers razmjenjuje s popisom čvorova istodobno, po četiri odjednom:
// što je više dostupnih računala, to brže svi dobiju sve
func (s *Service) syncPeers(ctx context.Context, list []Peer) map[string]string {
	out := map[string]string{}
	if len(list) == 0 {
		return out
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	for _, p := range list {
		wg.Add(1)
		go func(p Peer) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			applied, sent, err := s.SyncWith(ctx, p.NodeID)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				out[p.NodeID] = "greška: " + err.Error()
				return
			}
			out[p.NodeID] = fmt.Sprintf("primljeno %d, poslano %d", applied, sent)
		}(p)
	}
	wg.Wait()
	return out
}

// SyncDue je krug automatske sinkronizacije: svi kojima je red
func (s *Service) SyncDue(ctx context.Context) map[string]string {
	list, err := s.ListPeers(ctx)
	if err != nil {
		return map[string]string{"*": err.Error()}
	}
	states, _ := s.syncStates(ctx)
	now := time.Now().UTC()
	var pick []Peer
	for _, p := range list {
		if due(p, states[p.NodeID], s.interval(), now) {
			pick = append(pick, p)
		}
	}
	return s.syncPeers(ctx, pick)
}

// ---------- nadzorna ploča ----------

// wantsOf ograđuje zaostatak na kanale koje drugi čvor uopće drži: što
// nema u granici, nije ni pretplaćen, pa mu ne nedostaje
func (ps PeerStatus) wantsOf() func(string) bool {
	held := map[string]bool{}
	for key := range ps.State.Frontier {
		_, ch := ledger.SplitFrontierKey(key)
		held[ch] = true
	}
	return func(channel string) bool { return channel == "" || held[channel] }
}

// PeerStatus je jedan čvor na nadzornoj ploči
type PeerStatus struct {
	Peer
	State         SyncState `json:"state"`
	Member        bool      `json:"member"`
	MemberProblem string    `json:"member_problem,omitempty"`
	Backlog       int       `json:"backlog"`      // naših verzija koje taj čvor još nema (po zadnjoj granici)
	Reachability  string    `json:"reachability"` // online, offline, never
}

// Status je stanje sinkronizacije ovog čvora za nadzornu ploču
type Status struct {
	NodeID        string       `json:"node_id"`
	NodeName      string       `json:"node_name"`
	Network       *Network     `json:"network,omitempty"`
	Versions      int          `json:"versions"`
	IntervalSec   int          `json:"interval_sec"`
	AutoSync      bool         `json:"auto_sync"`
	Peers         []PeerStatus `json:"peers"`
	Online        int          `json:"online"`
	Total         int          `json:"total"`
	LastOK        *time.Time   `json:"last_ok,omitempty"`
	Alerts        []string     `json:"alerts"`
	GeneratedAt   time.Time    `json:"generated_at"`
	LanDiscovered []Discovered `json:"lan,omitempty"`
}

// Status slaže nadzornu ploču: tko je na mreži, koliko ih odgovara, tko
// zaostaje i što ne štima. Uz lan=true kratko pita i lokalnu mrežu.
func (s *Service) Status(ctx context.Context, lan bool) (*Status, error) {
	now := time.Now().UTC()
	every := s.interval()
	st := &Status{NodeID: s.node.ID, NodeName: s.node.Name, Network: s.NetworkInfo(),
		IntervalSec: int(every.Seconds()), AutoSync: s.autoSync(), GeneratedAt: now}

	if counts, err := s.rec.Count(ctx); err == nil {
		for _, n := range counts {
			st.Versions += n
		}
	}
	list, err := s.ListPeers(ctx)
	if err != nil {
		return nil, err
	}
	states, err := s.syncStates(ctx)
	if err != nil {
		return nil, err
	}
	members := map[string]Member{}
	if ms, err := s.ListMembers(ctx); err == nil {
		for _, m := range ms {
			members[m.DeviceID] = m
		}
	}

	for _, p := range list {
		ps := PeerStatus{Peer: p, State: states[p.NodeID]}
		if m, ok := members[p.NodeID]; ok {
			ps.Member, ps.MemberProblem = m.Valid, m.Problem
		} else {
			ps.MemberProblem = "nema potvrde članstva"
		}
		switch {
		case ps.State.LastOK == nil:
			ps.Reachability = "never"
		case ps.State.Fails == 0 && now.Sub(*ps.State.LastOK) <= 2*every+30*time.Second:
			ps.Reachability = "online"
			st.Online++
		default:
			ps.Reachability = "offline"
		}
		if len(ps.State.Frontier) > 0 {
			if delta, err := s.rec.Delta(ctx, ps.State.Frontier, ps.wantsOf(), 5000); err == nil {
				ps.Backlog = len(delta)
			}
		}
		if ps.State.LastOK != nil && (st.LastOK == nil || ps.State.LastOK.After(*st.LastOK)) {
			t := *ps.State.LastOK
			st.LastOK = &t
		}
		st.Peers = append(st.Peers, ps)
	}
	st.Total = len(st.Peers)
	sort.SliceStable(st.Peers, func(i, j int) bool {
		rank := map[string]int{"online": 0, "offline": 1, "never": 2}
		if rank[st.Peers[i].Reachability] != rank[st.Peers[j].Reachability] {
			return rank[st.Peers[i].Reachability] < rank[st.Peers[j].Reachability]
		}
		return st.Peers[i].Name < st.Peers[j].Name
	})

	if lan && s.ports.Discovery > 0 {
		if found, err := s.Discover(ctx, 1200*time.Millisecond); err == nil {
			st.LanDiscovered = found
		}
	}

	st.Alerts = s.alerts(st, every, now)
	return st, nil
}

// alerts kaže ljudskim jezikom što ne štima
func (s *Service) alerts(st *Status, every time.Duration, now time.Time) []string {
	var out []string
	if st.Network == nil {
		out = append(out, "Ovaj čvor nije ni u jednoj mreži: nema razmjene ni s kim. Uparite ga s uredom ili osnujte mrežu.")
		return out
	}
	if st.Total == 0 {
		out = append(out, "Nema nijednog poznatog čvora. Uparite bar jedan.")
		return out
	}
	if !st.AutoSync {
		out = append(out, "Automatska sinkronizacija je isključena; podaci se razmjenjuju samo na ručni zahtjev.")
	}
	if st.LastOK == nil {
		out = append(out, "Još nijedna razmjena nije uspjela ni s jednim čvorom.")
	} else if now.Sub(*st.LastOK) > 3*every {
		out = append(out, fmt.Sprintf("Ni s kim nije bilo uspješne razmjene od %s; provjerite mrežu i adrese čvorova.", st.LastOK.Local().Format("02.01. 15:04")))
	}
	hasPublic := false
	for _, p := range st.Peers {
		if p.IsBootstrap {
			hasPublic = true
		}
		if !p.Member {
			out = append(out, fmt.Sprintf("%s: %s — razmjena s njim nije dopuštena.", label(p.Peer), p.MemberProblem))
		}
		if p.State.Fails >= 3 {
			since := "nikad nije odgovorio"
			if p.State.LastOK != nil {
				since = "ne odgovara od " + p.State.LastOK.Local().Format("02.01. 15:04")
			}
			out = append(out, fmt.Sprintf("%s: %s (%d pokušaja zaredom): %s", label(p.Peer), since, p.State.Fails, p.State.LastError))
		}
		if len(p.Addresses) == 0 {
			out = append(out, fmt.Sprintf("%s: nema nijednu poznatu adresu, pa ga ovaj čvor ne može nazvati.", label(p.Peer)))
		}
	}
	if self, err := s.SelfPeer(context.Background()); err == nil && self.IsBootstrap {
		hasPublic = true
	}
	if !hasPublic && st.Total > 0 {
		out = append(out, "Nijedan čvor nema javnu adresu: razmjena radi samo unutar lokalne mreže.")
	}
	return out
}

func label(p Peer) string {
	if p.Name != "" {
		return p.Name
	}
	return p.NodeID
}
