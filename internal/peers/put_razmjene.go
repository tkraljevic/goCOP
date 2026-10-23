package peers

// Put zadnje razmjene s računalom (plan WEB synca, F1b): kojim je putem
// razmjena zadnji put prošla, otkad ide tim putem, s kojim RTT-om, zašto ne
// ide boljim putem i brojači od tada. Upisuje ga bilješka o razmjeni u oba
// smjera (recordSyncState → upisiPut), a čita Status, za pločicu i stranicu
// Sinkronizacija. Sve je u peer_sync i nikad ne putuje. Adresa druge strane
// ne ide nigdje; od nje se pamti samo je li privatna, za naziv puta.
//
// Brojači kreću od nule kad se promijeni naziv puta (LAN sync, SERVER sync,
// Tailscale, WEB sync), a ne sam put: par u istoj lokalnoj mreži naizmjence
// zove preko pronalaženja (lan) i dolazi na port (adresa), a oba su LAN sync.

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"net/netip"
	"time"

	"gocop/internal/razmjena"
	"gocop/internal/tailscale"
)

// PutRazmjene je put zadnje razmjene s jednim računalom i brojači otkad
// razmjena ide tim putem (Od). Nema adresa, pa smije na pločicu.
type PutRazmjene struct {
	Put      razmjena.Put `json:"put,omitempty"`
	Privatna bool         `json:"privatna,omitempty"` // adresa druge strane je privatna
	Naziv    string       `json:"naziv,omitempty"`    // razmjena.NazivPuta
	// RTTMs je RTT zadnje veze koju je ovo računalo nazvalo, najmanje 1 ms;
	// 0 kad se ne zna (tim putem je zvalo samo drugo računalo)
	RTTMs     int                 `json:"rtt_ms,omitempty"`
	Od        *time.Time          `json:"od,omitempty"`
	Razlog    razmjena.RazlogPuta `json:"razlog,omitempty"`
	Pokusaji  int                 `json:"pokusaji"`
	Uspjesi   int                 `json:"uspjesi"`
	Neuspjesi int                 `json:"neuspjesi"`
	Primljeno int64               `json:"primljeno"` // bajtovi razmjene, iznad TLS-a
	Poslano   int64               `json:"poslano"`
	// ZadnjiZvao: „ovaj” ili „drugi”, tko je započeo zadnju razmjenu
	ZadnjiZvao string `json:"zadnji_zvao,omitempty"`
}

// uzVezu dopuni ishod onim što zna veza: put, je li adresa druge strane
// privatna, RTT i bajtove. Bez veze (nijedna adresa nije odgovorila) ishod
// ostaje kakav jest, pa put ostaje prijašnji.
func (o syncOutcome) uzVezu(c *razmjena.Conn) syncOutcome {
	if c == nil {
		return o
	}
	o.put, o.privatna, o.rtt = c.Put, privatnaAdresa(fmt.Sprint(c.RemoteAddr())), c.RTT
	o.primljeno, o.poslano = c.Bajtovi()
	return o
}

// privatnaAdresa javlja je li adresa, s portom ili bez, privatna za naziv
// puta: u privatnoj mreži izvan Tailscalea (kao privatnaIP), ili na ovom
// istom računalu. Ime domene nije adresa, pa nije privatno.
func privatnaAdresa(adresa string) bool {
	host := adresa
	if h, _, err := net.SplitHostPort(adresa); err == nil {
		host = h
	}
	ip, err := netip.ParseAddr(host)
	ip = ip.Unmap().WithZone("")
	return err == nil && (ip.IsPrivate() && !tailscale.JeTailscale(ip) || ip.IsLoopback())
}

// rttMs je RTT u milisekundama za peer_sync: najmanje 1 ms, jer rukovanje u
// lokalnoj mreži traje manje, a 0 znači da se ne zna
func rttMs(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return max(1, int(d.Milliseconds()))
}

// upisiPut upisuje put i brojače iz bilješke o razmjeni. Redak već postoji,
// jer ga je recordSyncState upravo upisao. Kad se promijeni naziv puta,
// brojači kreću od ove razmjene; kad veze nije bilo, put ostaje prijašnji.
// RTT dolazne veze se ne zna, pa ostaje prijašnji na istom putu.
//
// Razlog piše razmjena koju je započelo ovo računalo, jer dolazna ne zna je
// li ovo moglo nazvati boljim putem. Dvije iznimke: od LAN synca boljeg puta
// nema, pa razmjena u lokalnoj mreži razlog briše u oba smjera, a uspjela
// dolazna briše „nije odgovorila nijedna adresa”, jer se računalo upravo
// javilo.
func (s *Service) upisiPut(ctx context.Context, nodeID string, o syncOutcome, sad time.Time) {
	naziv := razmjena.NazivPuta(o.put, o.privatna)
	novi := o.put != "" && naziv != s.nazivPuta(ctx, nodeID)
	_, err := s.db.ExecContext(ctx, `UPDATE peer_sync SET
		put = CASE WHEN ?1 <> '' THEN ?1 ELSE put END,
		put_privatna = CASE WHEN ?1 <> '' THEN ?2 ELSE put_privatna END,
		rtt_ms = CASE WHEN ?3 > 0 THEN ?3 WHEN ?4 THEN 0 ELSE rtt_ms END,
		put_od = CASE WHEN ?4 THEN ?5 ELSE put_od END,
		put_razlog = CASE WHEN ?13 THEN ''
			WHEN ?6 AND ?8 AND put_razlog = ?14 THEN ''
			WHEN ?6 THEN put_razlog ELSE ?7 END,
		put_pokusaji = CASE WHEN ?4 THEN 0 ELSE put_pokusaji END + 1,
		put_uspjesi = CASE WHEN ?4 THEN 0 ELSE put_uspjesi END + ?8,
		put_neuspjesi = CASE WHEN ?4 THEN 0 ELSE put_neuspjesi END + 1 - ?8,
		put_primljeno = CASE WHEN ?4 THEN 0 ELSE put_primljeno END + ?9,
		put_poslano = CASE WHEN ?4 THEN 0 ELSE put_poslano END + ?10,
		zadnji_zvao = ?11
		WHERE node_id = ?12`,
		string(o.put), boolInt(o.privatna), rttMs(o.rtt), boolInt(novi), sad, boolInt(o.dolazna), string(o.razlog),
		boolInt(o.err == nil), o.primljeno, o.poslano, map[bool]string{true: "drugi", false: "ovaj"}[o.dolazna], nodeID,
		boolInt(naziv == razmjena.NazivLAN), string(razmjena.RazlogPutaBezOdgovora))
	if err != nil {
		log.Printf("razmjena: put razmjene s %s nije spremljen: %v", nodeID, err)
	}
}

// nazivPuta je naziv puta koji je za računalo upisan; prazno dok ga nema
func (s *Service) nazivPuta(ctx context.Context, nodeID string) string {
	var put string
	var privatna bool
	_ = s.db.QueryRowContext(ctx, `SELECT put, put_privatna FROM peer_sync WHERE node_id = ?`, nodeID).Scan(&put, &privatna) // bez retka nema ni puta
	return razmjena.NazivPuta(razmjena.Put(put), privatna)
}

// razlogNakon je razlog puta kad je razmjena uspjela tek iza kandidata
// prije, koji nisu odgovorili. Kad nije odgovorio kandidat iz lokalne mreže
// (pronalaženje ili privatna adresa), računalo se ne javlja u lokalnoj
// mreži. Domena, Tailscale ili tunel koji nisu odgovorili to ne govore.
func razlogNakon(prije []kandidat) razmjena.RazlogPuta {
	for _, k := range prije {
		if k.put == razmjena.PutLAN || k.put == razmjena.PutAdresa && privatnaAdresa(k.adresa) {
			return razmjena.RazlogPutaNijeSeJavio
		}
	}
	return ""
}

// samoDolazi: ovo računalo drugo ne može nazvati, a ono se samo javlja kroz
// tunel (npr. laptop izvan kuće); vrijedi uz nedavnu uspješnu razmjenu
func samoDolazi(st SyncState) bool {
	return st.Fails > 0 && st.Put.Put == razmjena.PutKrozTunel
}

// vrijemeIz je vrijeme iz baze u UTC-u; nil kad ga nema
func vrijemeIz(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time.UTC()
	return &t
}
