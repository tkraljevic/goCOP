package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// primjeneMreze su čvorovi, članstva, ovlasti i opozivi. Zapisuje ih sloj
// razmjene (peers), a površinu im osvježava primjena, kao i ostalim
// entitetima. Potpis se pri primjeni ne provjerava, nego pri upotrebi
// (trusted, opozvano).
var primjeneMreze = map[string]primjena{
	"peers":       {upis: primijeniCvor, brisanje: obrisi(`DELETE FROM peers WHERE node_id = ?`)},
	"memberships": {upis: primijeniClanstvo, brisanje: obrisi(`DELETE FROM memberships WHERE node_id = ?`)},
	"ovlasti":     {upis: primijeniOvlast, brisanje: obrisi(`DELETE FROM ovlasti WHERE node_id = ?`)},
	"opozivi":     {upis: primijeniOpoziv, brisanje: ostaviOpoziv},
}

// primijeniCvor stavlja primljeni zapis o čvoru na površinu
func primijeniCvor(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var p struct {
		NodeID       string     `json:"node_id"`
		Name         string     `json:"name"`
		PublicKey    string     `json:"public_key"`
		Addresses    []string   `json:"addresses"`
		IsBootstrap  bool       `json:"is_bootstrap"`
		LastSeen     *time.Time `json:"last_seen"`
		LastSync     *time.Time `json:"last_sync"`
		LastSyncNote string     `json:"last_sync_note"`
		CreatedAt    time.Time  `json:"created_at"`
	}
	if err := json.Unmarshal(v.Payload, &p); err != nil {
		return err
	}
	addrs, _ := json.Marshal(p.Addresses)
	// bilješke o zadnjoj sinkronizaciji opisuju odnos dva čvora, ne čvor —
	// ne prepisuju se tuđima.
	// Ključ se upisuje kakav je u zapisu, i kad mijenja ključ poznatog
	// računala. Zapis piše i šalje bilo koji član, pa mu razmjena ne
	// vjeruje: ključ koji druga strana mora dokazati dolazi iz potpisanog
	// članstva (peers.kljucClana). Da se ključ ovdje zadržava, računalo
	// koje je dobilo novi ključ (zaborav i ponovno uparivanje) ostalo bi
	// trećem čvoru sa starim, ovisno o tome stigne li zaborav istom
	// razmjenom kao novi zapis.
	_, err := tx.ExecContext(ctx, `
		INSERT INTO peers (node_id, name, public_key, addresses, is_bootstrap, last_seen, last_sync, last_sync_note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, NULL, '', ?)
		ON CONFLICT(node_id) DO UPDATE SET
			name = excluded.name, public_key = excluded.public_key, addresses = excluded.addresses,
			is_bootstrap = excluded.is_bootstrap
	`, p.NodeID, p.Name, p.PublicKey, string(addrs), boolToInt(p.IsBootstrap), nullTime(p.LastSeen), p.CreatedAt.UTC())
	return err
}

// primijeniClanstvo stavlja primljeno članstvo čvora u mreži na površinu
func primijeniClanstvo(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var m struct {
		Network   string           `json:"network"`
		DeviceID  string           `json:"deviceId"`
		DeviceKey string           `json:"deviceKey"`
		IssuedBy  string           `json:"issuedBy"`
		IssuedAt  time.Time        `json:"issuedAt"`
		ExpiresAt time.Time        `json:"expiresAt"`
		Signature string           `json:"signature"`
		Primatelj *razmjena.Ovlast `json:"primatelj"`
	}
	if err := json.Unmarshal(v.Payload, &m); err != nil {
		return err
	}
	// ovlast potpisnika u istom obliku kao kod izvora (peers), ma u
	// kojoj zoni i kojim redom polja stigla
	primatelj := ""
	if m.Primatelj != nil {
		b, err := json.Marshal(m.Primatelj.UTC())
		if err != nil {
			return err
		}
		primatelj = string(b)
	}
	// Potpis se ne provjerava ovdje nego na vratima razmjene — ovdje se
	// samo pamti što je stiglo. Tuđa ili kriva potvrda prepiše red tog
	// imena, ali razmjena ključ računala bira iz svih članstava u knjizi,
	// samo iz onih koja vrijede (peers.kljucClana).
	_, err := tx.ExecContext(ctx, `
		INSERT INTO memberships (node_id, public_key, network, issued_by, issued_at, expires_at, signature, created_at, primatelj)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			public_key = excluded.public_key, network = excluded.network, issued_by = excluded.issued_by,
			issued_at = excluded.issued_at, expires_at = excluded.expires_at, signature = excluded.signature,
			primatelj = excluded.primatelj
	`, m.DeviceID, m.DeviceKey, m.Network, m.IssuedBy, m.IssuedAt.UTC(), m.ExpiresAt.UTC(), m.Signature, v.CreatedAt.UTC(), primatelj)
	return err
}

// primijeniOvlast stavlja primljenu ovlast za izdavanje članstva na površinu
func primijeniOvlast(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var o struct {
		Network   string    `json:"network"`
		DeviceID  string    `json:"deviceId"`
		DeviceKey string    `json:"deviceKey"`
		IssuedBy  string    `json:"issuedBy"`
		IssuedAt  time.Time `json:"issuedAt"`
		ExpiresAt time.Time `json:"expiresAt"`
		Signature string    `json:"signature"`
	}
	if err := json.Unmarshal(v.Payload, &o); err != nil {
		return err
	}
	// kao članstva: potpis se provjerava pri upotrebi, ne ovdje
	_, err := tx.ExecContext(ctx, `
		INSERT INTO ovlasti (node_id, public_key, network, issued_by, issued_at, expires_at, signature, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			public_key = excluded.public_key, network = excluded.network, issued_by = excluded.issued_by,
			issued_at = excluded.issued_at, expires_at = excluded.expires_at, signature = excluded.signature
	`, o.DeviceID, o.DeviceKey, o.Network, o.IssuedBy, o.IssuedAt.UTC(), o.ExpiresAt.UTC(), o.Signature, v.CreatedAt.UTC())
	return err
}

// primijeniOpoziv stavlja primljeni opoziv članstva ili ovlasti na površinu
func primijeniOpoziv(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var op struct {
		ID         string    `json:"id"`
		Vrsta      string    `json:"vrsta"`
		NodeID     string    `json:"nodeId"`
		PublicKey  string    `json:"publicKey"`
		IssuedAt   time.Time `json:"issuedAt"`
		OpozvanoAt time.Time `json:"opozvanoAt"`
		Opozvao    string    `json:"opozvao"`
		Potpisnik  string    `json:"potpisnik"`
		Potpis     string    `json:"potpis"`
	}
	if err := json.Unmarshal(v.Payload, &op); err != nil {
		return err
	}
	// potpis se provjerava pri upotrebi (peers.opozvano): nepotpisan ili
	// tuđe potpisan opoziv stoji u tablici, ali ništa ne opoziva
	_, err := tx.ExecContext(ctx, `
		INSERT INTO opozivi (id, vrsta, node_id, public_key, issued_at, opozvano_at, opozvao, potpisnik, potpis)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING
	`, op.ID, op.Vrsta, op.NodeID, op.PublicKey, op.IssuedAt.UTC(), op.OpozvanoAt.UTC(), op.Opozvao, op.Potpisnik, op.Potpis)
	return err
}

// ostaviOpoziv je brisanje opoziva, koje površinu ne mijenja
func ostaviOpoziv(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	// opoziv se ne poništava: arhivirana verzija (koju bi mogao poslati i
	// član s izmijenjenim programom) ga ne briše; ponovno primanje izdaje
	// novo, kasnije članstvo
	return nil
}
