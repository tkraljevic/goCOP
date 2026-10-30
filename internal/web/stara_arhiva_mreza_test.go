package web

import (
	"context"
	"database/sql"
	"testing"
)

// verzijaCvora broji verzije koje je u knjigu ovog čvora upisao čvor id
func verzijaCvora(t *testing.T, baza *sql.DB, id string) int {
	t.Helper()
	var n int
	if err := baza.QueryRow(`SELECT COUNT(*) FROM record_versions WHERE node_id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Odluke (30. 10. 2026.): dok računalo nije u mreži, stara .db arhiva se ne
// uvozi ni globalnom administratoru s ovog računala; poruka kaže zašto i što
// učiniti. Kad računalo uđe u mrežu, ista se arhiva uvozi.
func TestStaraArhivaTekUMrezi(t *testing.T) {
	stara, _ := probnaArhiva(t)
	novi, _, baza := cvorZaUvoz(t, "novi-laptop")
	novi.SetWantsAll(true)
	h := &DBMaintHandler{peers: novi}

	_, upit := preusmjerenje(t, posaljiArhivu(h, stara, "stara.db", kaoAdmin))
	if upit.Get("error") != porukaStaraBezMreze || upit.Get("success") != "" {
		t.Fatalf("stara arhiva na računalu bez mreže: %v", upit)
	}
	if n := verzijaCvora(t, baza, "laptop-baranja"); n != 0 {
		t.Fatalf("bez mreže uvezeno je %d verzija", n)
	}

	if err := novi.CreateNetwork(context.Background(), "Hrvatske vode"); err != nil {
		t.Fatal(err)
	}
	if _, upit := preusmjerenje(t, posaljiArhivu(h, stara, "stara.db", kaoAdmin)); upit.Get("success") == "" {
		t.Fatalf("stara arhiva na računalu u mreži: %v", upit)
	}
	if n := verzijaCvora(t, baza, "laptop-baranja"); n != 1 {
		t.Fatalf("u mreži uvezeno je %d verzija, očekivana 1", n)
	}
}
