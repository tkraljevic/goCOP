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
	// Program i Gradnja su izdanje i puna oznaka programa koje je drugi
	// čvor zadnji put javio; prazno = stariji program koji ih ne javlja
	Program string `json:"program,omitempty"`
	Gradnja string `json:"gradnja,omitempty"`
	// ProgramPoznat: stanje je upisao program koji bilježi inačicu, pa
	// prazan Program znači da je drugi čvor ne javlja. Redak upisan prije
	// toga (stariji program na ovom čvoru) o inačici ne govori ništa.
	ProgramPoznat bool `json:"-"`
}

// syncOutcome je ishod jedne razmjene za bilješku
type syncOutcome struct {
	applied, sent    int
	frontier         map[string]string
	program, gradnja string // što je drugi čvor javio o svom programu
	took             time.Duration
	err              error
}

// ishodRazmjene slaže bilješku iz onoga što je exchange vratio; inačica
// programa vrijedi i kad je razgovor pukao nakon što je granica stigla
func ishodRazmjene(applied, sent int, theirs frontierMsg, took time.Duration, err error) syncOutcome {
	return syncOutcome{applied: applied, sent: sent, frontier: theirs.Frontier,
		program: theirs.Program, gradnja: theirs.Gradnja, took: took, err: err}
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
			INSERT INTO peer_sync (node_id, their_frontier, last_attempt, last_ok, last_error, applied, sent, duration_ms, fails, program, gradnja, program_poznat)
			VALUES (?, ?, ?, ?, '', ?, ?, ?, 0, ?, ?, 1)
			ON CONFLICT(node_id) DO UPDATE SET their_frontier = excluded.their_frontier, last_attempt = excluded.last_attempt,
				last_ok = excluded.last_ok, last_error = '', applied = excluded.applied, sent = excluded.sent,
				duration_ms = excluded.duration_ms, fails = 0, program = excluded.program, gradnja = excluded.gradnja,
				program_poznat = 1`,
			nodeID, frontier, now, now, o.applied, o.sent, o.took.Milliseconds(), o.program, o.gradnja)
	} else {
		// Razgovor koji je pukao prije granice ne zna ništa o programu
		// drugog čvora: poznata inačica ostaje, ne briše se praznim.
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO peer_sync (node_id, their_frontier, last_attempt, last_ok, last_error, applied, sent, duration_ms, fails, program, gradnja, program_poznat)
			VALUES (?, '{}', ?, NULL, ?, 0, 0, ?, 1, ?, ?, ?)
			ON CONFLICT(node_id) DO UPDATE SET last_attempt = excluded.last_attempt, last_error = excluded.last_error,
				duration_ms = excluded.duration_ms, fails = peer_sync.fails + 1,
				program = CASE WHEN excluded.program <> '' THEN excluded.program ELSE peer_sync.program END,
				gradnja = CASE WHEN excluded.program <> '' THEN excluded.gradnja ELSE peer_sync.gradnja END,
				program_poznat = CASE WHEN excluded.program <> '' THEN 1 ELSE peer_sync.program_poznat END`,
			nodeID, now, o.err.Error(), o.took.Milliseconds(), o.program, o.gradnja, boolInt(o.program != ""))
	}
	if err != nil {
		fmt.Printf("sinkronizacija: stanje za %s nije spremljeno: %v\n", nodeID, err)
	}
}

// syncStates čita stanje razmjene za sve čvorove
func (s *Service) syncStates(ctx context.Context) (map[string]SyncState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT node_id, their_frontier, last_attempt, last_ok, last_error, applied, sent, duration_ms, fails, program, gradnja, program_poznat FROM peer_sync`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]SyncState{}
	for rows.Next() {
		var id, frontier string
		var st SyncState
		var attempt, ok sql.NullTime
		if err := rows.Scan(&id, &frontier, &attempt, &ok, &st.LastError, &st.Applied, &st.Sent, &st.DurationMs, &st.Fails, &st.Program, &st.Gradnja, &st.ProgramPoznat); err != nil {
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
			// pun razgovor znači da ima još: nastavlja se odmah, ne za pet minuta
			var applied, sent, krugova int
			var err error
			for krugova < najviseKrugovaZaRedom && ctx.Err() == nil {
				a, sn, e := s.SyncWith(ctx, p.NodeID)
				applied, sent, krugova, err = applied+a, sent+sn, krugova+1, e
				if e != nil || (a < NajviseVerzijaPoRazmjeni && sn < NajviseVerzijaPoRazmjeni) {
					break
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				out[p.NodeID] = "greška: " + err.Error()
				return
			}
			out[p.NodeID] = fmt.Sprintf("primljeno %d, poslano %d", applied, sent)
			if krugova > 1 {
				out[p.NodeID] += fmt.Sprintf(" u %d razgovora", krugova)
			}
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
	// SamoDolazi: razmjena s njim uspijeva kad on nazove (npr. laptop izvan
	// kuće kroz tunel), a ovaj čvor njega ne može nazvati
	SamoDolazi bool `json:"samo_dolazi,omitempty"`
	// Program je izdanje na kojem taj čvor radi (prazno: stariji program koji
	// ga ne javlja); RazlicitProgram kaže da nije isto kao ovdje
	Program         string `json:"program,omitempty"`
	Gradnja         string `json:"gradnja,omitempty"`
	RazlicitProgram bool   `json:"razlicit_program,omitempty"`
}

// razlicitProgram javlja radi li drugi čvor na drugom izdanju: javio je
// drukčije, ili se uspješno razmijenio a ne javlja ga (stariji program).
// Redak iz vremena prije bilježenja inačice ne govori ništa.
func razlicitProgram(nas string, st SyncState) bool {
	if nas == "" {
		return false
	}
	if st.Program != "" {
		return st.Program != nas
	}
	return st.ProgramPoznat && st.LastOK != nil
}

// Status je stanje sinkronizacije ovog čvora za nadzornu ploču
type Status struct {
	NodeID        string       `json:"node_id"`
	NodeName      string       `json:"node_name"`
	Program       string       `json:"program,omitempty"` // izdanje ovog čvora
	Gradnja       string       `json:"gradnja,omitempty"`
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
	st.Program, st.Gradnja = s.Program()

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
		ps.Program, ps.Gradnja = ps.State.Program, ps.State.Gradnja
		ps.RazlicitProgram = razlicitProgram(st.Program, ps.State)
		if m, ok := members[p.NodeID]; ok {
			ps.Member, ps.MemberProblem = m.Valid, m.Problem
		} else {
			ps.MemberProblem = "nema potvrde članstva"
		}
		switch {
		case ps.State.LastOK == nil:
			ps.Reachability = "never"
		case now.Sub(*ps.State.LastOK) <= 2*every+30*time.Second:
			// Nedavna uspješna razmjena znači da je na mreži, i kad ga ovaj
			// čvor ne može nazvati: laptop u uredu sam zove kroz tunel, a
			// kućna adresa mu je nedostupna.
			ps.Reachability = "online"
			ps.SamoDolazi = ps.State.Fails > 0
			st.Online++
		default:
			ps.Reachability = "offline"
		}
		if len(ps.State.Frontier) > 0 {
			if delta, err := s.rec.Delta(ctx, ps.State.Frontier, ps.wantsOf(), NajviseVerzijaPoRazmjeni); err == nil {
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
		if st.Program != "" {
			switch {
			case p.State.Program != "" && p.State.Program != st.Program:
				out = append(out, fmt.Sprintf("Čvor %s radi na %s, ovaj na %s — ažurirajte stariji čvor.", label(p.Peer), p.State.Program, st.Program))
			case p.State.Program == "" && p.State.ProgramPoznat && p.State.LastOK != nil:
				out = append(out, fmt.Sprintf("Čvor %s radi na verziji starijoj od %s (ne javlja verziju) — ažurirajte ga.", label(p.Peer), st.Program))
			}
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
