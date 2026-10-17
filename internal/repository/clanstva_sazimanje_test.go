package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
)

// Sažimanje knjige ne briše članstvo koje više nije zadnja verzija svog
// zapisa. Laptop je primio osnivač, a poslije je pod istim imenom stiglo
// članstvo drugog računala s drugim ključem. Razmjena ključ imena bira iz
// svih članstava u knjizi i vrijedi prvo izdano (peers, clanstvo_oznake.go);
// da sažimanje ostavi samo zadnju verziju, ime bi poslije pripalo drugom
// računalu. Isto članstvo upisano dvaput sažimanje i dalje svodi na jedno.
func TestSazimanjeCuvaSvaClanstva(t *testing.T) {
	ctx := context.Background()
	x := noviCvorZaRazmjenu(t, "ured-vukovar")
	clanstvo := func(cvor, kljuc, potpis string) ledger.Version {
		t.Helper()
		p, err := json.Marshal(map[string]any{"deviceId": "laptop-vinkovci", "deviceKey": kljuc, "issuedBy": cvor,
			"issuedAt": time.Now().UTC(), "signature": potpis})
		if err != nil {
			t.Fatal(err)
		}
		return ledger.Version{VersionID: uuid.Must(uuid.NewV7()).String(), Entity: "memberships", EntityID: "laptop-vinkovci",
			NodeID: cvor, Payload: p, CreatedAt: time.Now().UTC(), SchemaVersion: 1}
	}
	prvo := clanstvo("cop-osijek", "kljuc-laptopa", "potpis-osnivaca")
	ponovno := prvo
	ponovno.VersionID = uuid.Must(uuid.NewV7()).String()
	stigle := []ledger.Version{prvo, ponovno, clanstvo("ured-server", "kljuc-drugog", "potpis-primatelja")}
	// oba autora poslije upišu još nešto, pa članstva nisu zadnje što su
	// upisali (to sažimanje ionako čuva)
	for _, autor := range []string{"cop-osijek", "ured-server"} {
		v := clanstvo(autor, "kljuc-"+autor, "potpis-"+autor)
		v.EntityID = autor
		stigle = append(stigle, v)
	}
	if _, err := x.rec.Apply(ctx, stigle); err != nil {
		t.Fatal(err)
	}
	if _, err := x.db.Exec(`UPDATE record_versions SET created_at = ?`, time.Now().AddDate(0, 0, -200)); err != nil {
		t.Fatal(err)
	}
	rok := time.Now().AddDate(0, 0, -90)
	najava, err := ZaSazimanje(ctx, x.rec, rok)
	if err != nil {
		t.Fatal(err)
	}
	obrisano, err := SazmiKnjigu(ctx, x.db, x.rec, rok)
	if err != nil || obrisano != 1 || najava != 1 {
		t.Fatalf("sažimanje briše samo prvi upis istog članstva: obrisano %d, najavljeno %d, %v", obrisano, najava, err)
	}
	var potpisi []string
	rows, err := x.db.Query(`SELECT json_extract(payload, '$.signature') FROM record_versions WHERE entity_id = 'laptop-vinkovci' ORDER BY version_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatal(err)
		}
		potpisi = append(potpisi, p)
	}
	if len(potpisi) != 2 || potpisi[0] != "potpis-osnivaca" || potpisi[1] != "potpis-primatelja" {
		t.Errorf("nakon sažimanja članstva laptopa: %v", potpisi)
	}
}
