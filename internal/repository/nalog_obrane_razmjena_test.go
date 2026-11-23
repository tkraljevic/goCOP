package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"gocop/internal/models"
)

// Nalog u obrani u dnevniku usluge A.02 (Odluke (22. 11. 2026.)) nosi
// ulogu onoga tko ga je dao. To je novo polje upisa dnevnika: putuje
// razmjenom, a čvor starijeg programa ga ne poznaje.

// nalogUObrani sprema dnevnik A.02 na području 16, list i nalog
// rukovoditelja dionice
func nalogUObrani(t *testing.T, c *cvorZaRazmjenu) (*JournalRepository, *models.JournalEntry) {
	t.Helper()
	ctx := context.Background()
	r := NewJournalRepository(c.db, c.rec)
	dan := time.Date(2026, 11, 22, 0, 0, 0, 0, models.Zagreb)
	j := &models.Journal{AreaID: 16, Kind: models.JournalKindMaintenanceA02, Title: "A.02", Year: 2026, Contractor: "Primjer d.o.o."}
	nuzno(t, r.SaveJournal(ctx, j))
	sh := &models.JournalSheet{JournalID: j.ID, Date: dan}
	nuzno(t, r.SaveSheet(ctx, sh))
	e := &models.JournalEntry{JournalID: j.ID, SheetID: sh.ID, Date: dan, Kind: models.EntryKindTask, Side: models.EntrySideSupervisor,
		SectionCode: "B.16.1", Text: "dovesti bager na nasip", Status: models.TaskOpen, UserID: "u1", UserName: "Ivo Ivić",
		UlogaDavatelja: "Rukovoditelj dionice"}
	nuzno(t, r.SaveEntry(ctx, e))
	return r, e
}

// Uloga davatelja putuje na drugi čvor uz nalog
func TestUlogaDavateljaPutujeRazmjenom(t *testing.T) {
	a, b := noviCvorZaRazmjenu(t, "a"), noviCvorZaRazmjenu(t, "b")
	_, e := nalogUObrani(t, a)
	a.posalji(t, b, "")
	got, err := NewJournalRepository(b.db, b.rec).GetEntry(context.Background(), e.ID)
	if err != nil || got == nil || got.UlogaDavatelja != "Rukovoditelj dionice" || got.SectionCode != "B.16.1" {
		t.Fatalf("nalog na drugom čvoru: %+v, %v", got, err)
	}
}

// upis0064 je upis dnevnika kako ga zapisuje čvor s programom 0.0.64:
// ulogu davatelja ne poznaje (ovdje skraćeno na ono što test mijenja)
type upis0064 struct {
	ID        string    `json:"id"`
	JournalID string    `json:"journal_id"`
	SheetID   string    `json:"sheet_id"`
	Number    int       `json:"number"`
	Date      time.Time `json:"date"`
	Kind      string    `json:"kind"`
	Side      string    `json:"side"`
	Text      string    `json:"text"`
	Status    string    `json:"status,omitempty"`
	UserID    string    `json:"user_id"`
	UserName  string    `json:"user_name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Čvor s 0.0.64 nalog prima i čuva, a kad ga izvođač na njemu označi
// izvedenim, knjiga ulogu davatelja prepiše iz prethodne verzije: stariji
// čvor ne briše novo polje
func TestUlogaDavateljaPrezivljavaIzmjenuStarijegCvora(t *testing.T) {
	ctx := context.Background()
	a, b := noviCvorZaRazmjenu(t, "a"), noviCvorZaRazmjenu(t, "b")
	r, e := nalogUObrani(t, a)
	a.posalji(t, b, "")

	var kanal string
	nuzno(t, b.db.QueryRow(`SELECT channel FROM record_versions WHERE entity = ? AND entity_id = ?`, EntityJournalEntries, e.ID).Scan(&kanal))
	stari := upis0064{ID: e.ID, JournalID: e.JournalID, SheetID: e.SheetID, Number: e.Number, Date: e.Date, Kind: e.Kind, Side: e.Side,
		Text: e.Text, Status: models.TaskDone, UserID: e.UserID, UserName: e.UserName, CreatedAt: e.CreatedAt, UpdatedAt: time.Now().UTC()}
	if _, err := b.rec.RecordIn(ctx, b.db, kanal, EntityJournalEntries, e.ID, stari); err != nil {
		t.Fatal(err)
	}
	v, err := b.rec.Latest(ctx, EntityJournalEntries, e.ID)
	nuzno(t, err)
	var polja map[string]json.RawMessage
	if err := json.Unmarshal(v.Payload, &polja); err != nil || string(polja["uloga_davatelja"]) != `"Rukovoditelj dionice"` {
		t.Fatalf("verzija starijeg čvora nije zadržala ulogu davatelja: %s, %v", v.Payload, err)
	}
	b.posalji(t, a, "")
	got, err := r.GetEntry(ctx, e.ID)
	if err != nil || got == nil || got.Status != models.TaskDone || got.UlogaDavatelja != "Rukovoditelj dionice" || got.SectionCode != "B.16.1" {
		t.Fatalf("nalog nakon izmjene starijeg čvora: %+v, %v", got, err)
	}
}
