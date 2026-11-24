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
// ulogu davatelja ni polja primljenog naloga ne poznaje (ovdje skraćeno na
// ono što test mijenja)
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

// primljeniNalog sprema primljeni nalog izvođača s davateljem s popisa i
// odgovorom davatelja
func primljeniNalog(t *testing.T, c *cvorZaRazmjenu) (*JournalRepository, *models.JournalEntry) {
	t.Helper()
	r, e := nalogUObrani(t, c)
	kad := time.Date(2026, 11, 22, 14, 30, 0, 0, models.Zagreb)
	odgovor := kad.Add(time.Hour)
	p := &models.JournalEntry{JournalID: e.JournalID, SheetID: e.SheetID, Date: e.Date, Kind: models.EntryKindTask, Side: models.EntrySideContractor,
		SectionCode: "B.16.3", Text: "postaviti vreće", Status: models.TaskOpen, UserID: "u2", UserName: "Marko Markić", HappenedAt: &kad,
		DavateljID: "u1", Davatelj: "Ivo Ivić", UlogaDavatelja: "Rukovoditelj dionice",
		Odgovor: models.OdgovorNijeTako, OdgovorNapomena: "rekao sam B.16.4", OdgovorAt: &odgovor}
	nuzno(t, r.SaveEntry(context.Background(), p))
	return r, p
}

// Davatelj i odgovor primljenog naloga putuju na drugi čvor, a čvor s
// 0.0.64 ih ne briše kad nalog označi izvedenim
func TestPrimljeniNalogRazmjenomIStarijiCvor(t *testing.T) {
	ctx := context.Background()
	a, b := noviCvorZaRazmjenu(t, "a"), noviCvorZaRazmjenu(t, "b")
	r, p := primljeniNalog(t, a)
	a.posalji(t, b, "")
	isti := func(got *models.JournalEntry) bool {
		return got != nil && got.Primljen() && got.DavateljID == "u1" && got.Davatelj == "Ivo Ivić" && got.UlogaDavatelja == "Rukovoditelj dionice" &&
			got.Odgovor == models.OdgovorNijeTako && got.OdgovorNapomena == "rekao sam B.16.4" && got.OdgovorAt != nil && got.OdgovorAt.Equal(*p.OdgovorAt)
	}
	naB, err := NewJournalRepository(b.db, b.rec).GetEntry(ctx, p.ID)
	if err != nil || !isti(naB) {
		t.Fatalf("primljeni nalog na drugom čvoru: %+v, %v", naB, err)
	}

	var kanal string
	nuzno(t, b.db.QueryRow(`SELECT channel FROM record_versions WHERE entity = ? AND entity_id = ?`, EntityJournalEntries, p.ID).Scan(&kanal))
	stari := upis0064{ID: p.ID, JournalID: p.JournalID, SheetID: p.SheetID, Number: p.Number, Date: p.Date, Kind: p.Kind, Side: p.Side,
		Text: p.Text, Status: models.TaskDone, UserID: p.UserID, UserName: p.UserName, CreatedAt: p.CreatedAt, UpdatedAt: time.Now().UTC()}
	if _, err := b.rec.RecordIn(ctx, b.db, kanal, EntityJournalEntries, p.ID, stari); err != nil {
		t.Fatal(err)
	}
	b.posalji(t, a, "")
	got, err := r.GetEntry(ctx, p.ID)
	if err != nil || !isti(got) || got.Status != models.TaskDone {
		t.Fatalf("primljeni nalog nakon izmjene starijeg čvora: %+v, %v", got, err)
	}
}

// Nalozi davatelja i nalozi na dionici čitaju se iz dnevnika A.02:
// primljeni nalog davatelja dok čeka odgovor ili je otvoren, svoj nalog dok
// je otvoren, a na dionici otvoreni i oni od zadanog dana
func TestNaloziDavateljaINaDioniciIzBaze(t *testing.T) {
	ctx := context.Background()
	c := noviCvorZaRazmjenu(t, "a")
	r, primljeni := primljeniNalog(t, c)
	upisi, err := r.EntriesForSheet(ctx, primljeni.SheetID)
	if err != nil || len(upisi) != 2 {
		t.Fatalf("upisi lista: %+v, %v", upisi, err)
	}
	svoj := &upisi[0]
	nalozi, err := r.NaloziDavatelja(ctx, "u1")
	if err != nil || len(nalozi) != 2 || nalozi[0].ID != svoj.ID || nalozi[1].ID != primljeni.ID {
		t.Fatalf("nalozi davatelja: %+v, %v", nalozi, err)
	}
	if n, err := r.NaloziDavatelja(ctx, "u2"); err != nil || len(n) != 0 {
		t.Errorf("izvođač nije davatelj: %+v, %v", n, err)
	}
	svoj.Status = models.TaskDone
	nuzno(t, r.SaveEntry(ctx, svoj))
	if n, err := r.NaloziDavatelja(ctx, "u1"); err != nil || len(n) != 1 {
		t.Errorf("izveden svoj nalog ostao: %+v, %v", n, err)
	}
	if n, err := r.NaloziNaDionici(ctx, "B.16.1", svoj.Date); err != nil || len(n) != 1 || n[0].ID != svoj.ID {
		t.Errorf("izveden nalog od dana: %+v, %v", n, err)
	}
	if n, err := r.NaloziNaDionici(ctx, "B.16.1", svoj.Date.AddDate(0, 0, 1)); err != nil || len(n) != 0 {
		t.Errorf("izveden nalog prije dana: %+v, %v", n, err)
	}
	if n, err := r.NaloziNaDionici(ctx, "B.16.3", svoj.Date.AddDate(0, 0, 1)); err != nil || len(n) != 1 {
		t.Errorf("otvoren nalog bez obzira na dan: %+v, %v", n, err)
	}
}
