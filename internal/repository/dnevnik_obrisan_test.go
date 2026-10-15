package repository

import (
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Dnevnik obrisan prije ovog ispravka dobio je spomenik samo za sebe, a
// upisi koje taj čvor nije imao na površini ostali su živi u knjizi. Na čvoru
// koji ih je imao odnijelo ih je brisanje dnevnika (ON DELETE CASCADE). Novi
// čvor koji primi spomenik dnevnika prije upisa padao je na stranom ključu
// pri svakoj razmjeni.

const obrisaniDnevnik = "01a0951e-0000-7000-8000-0000000000d1"

var danDnevnika = time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)

// spomenikZa je verzija koja zapis briše
func spomenikZa(t *testing.T, entitet, id, versionID string, sadrzaj any) ledger.Version {
	t.Helper()
	v := verzijaZa(t, entitet, id, versionID, sadrzaj)
	v.Archived = true
	return v
}

func probniDnevnik() models.Journal {
	return models.Journal{ID: obrisaniDnevnik, AreaID: 16, Kind: models.JournalKindDefense,
		Title: "Probni dnevnik", Year: 2026, CreatedAt: danDnevnika, UpdatedAt: danDnevnika}
}

func brojUDnevniku(t *testing.T, c *cvor, tablica, journalID string) int {
	t.Helper()
	var n int
	nuzno(t, c.db.QueryRow(`SELECT count(*) FROM `+tablica+` WHERE journal_id = ?`, journalID).Scan(&n))
	return n
}

func TestUpisObrisanogDnevnikaNeIdeNaPovrsinu(t *testing.T) {
	sNedovrsenima(t)
	c := noviCvor(t, "novi-ured")
	j := probniDnevnik()
	upis := models.JournalEntry{ID: "01a0951f-0000-7000-8000-0000000000e1", JournalID: obrisaniDnevnik, Number: 1,
		Date: danDnevnika, Kind: models.EntryKindNote, Text: "probni upis", CreatedAt: danDnevnika, UpdatedAt: danDnevnika}
	list := models.JournalSheet{ID: "01a0951f-0000-7000-8000-0000000000f1", JournalID: obrisaniDnevnik, Number: 1,
		Date: danDnevnika, CreatedAt: danDnevnika, UpdatedAt: danDnevnika}

	nuzno(t, primiPaket(t, c,
		verzijaZa(t, EntityJournals, j.ID, vid(1), j),
		spomenikZa(t, EntityJournals, j.ID, vid(4), j)))
	if err := primiPaket(t, c,
		verzijaZa(t, EntityJournalEntries, upis.ID, vid(2), upis),
		verzijaZa(t, EntityJournalSheets, list.ID, vid(3), list)); err != nil {
		t.Fatalf("upis i list obrisanog dnevnika: %v", err)
	}
	if n := brojUDnevniku(t, c, "journal_entries", obrisaniDnevnik) + brojUDnevniku(t, c, "journal_sheets", obrisaniDnevnik); n != 0 {
		t.Errorf("na površini je %d upisa i listova obrisanog dnevnika", n)
	}
	if nedovrsen(EntityJournalEntries+"|"+upis.ID) || nedovrsen(EntityJournalSheets+"|"+list.ID) {
		t.Error("upis ili list obrisanog dnevnika čeka među nedovršenima")
	}
}

// Dnevnik koji još nije stigao nije obrisan: upis čeka njega, ne preskače se.
func TestUpisDnevnikaKojiNijeStigaoCeka(t *testing.T) {
	sNedovrsenima(t)
	c := noviCvor(t, "novi-ured")
	j := probniDnevnik()
	upis := models.JournalEntry{ID: "01a0951f-0000-7000-8000-0000000000e2", JournalID: obrisaniDnevnik, Number: 1,
		Date: danDnevnika, Kind: models.EntryKindNote, Text: "probni upis", CreatedAt: danDnevnika, UpdatedAt: danDnevnika}
	if err := primiPaket(t, c, verzijaZa(t, EntityJournalEntries, upis.ID, vid(2), upis)); err == nil {
		t.Fatal("upis bez dnevnika prošao je strani ključ")
	}
	nuzno(t, primiPaket(t, c, verzijaZa(t, EntityJournals, j.ID, vid(1), j)))
	if n := brojUDnevniku(t, c, "journal_entries", obrisaniDnevnik); n != 1 {
		t.Errorf("upis nije stigao na površinu kad je stigao njegov dnevnik (%d)", n)
	}
}

// Brisanje dnevnika daje spomenik i upisu koji je u knjizi živ, a na
// površini ovog čvora ga nema. Upis drugog dnevnika ostaje.
func TestBrisanjeDnevnikaArhiviraISirocad(t *testing.T) {
	c := noviCvor(t, "ured")
	repo := NewJournalRepository(c.db, c.rec)
	j := models.Journal{AreaID: 16, Kind: models.JournalKindDefense, Title: "Probni dnevnik", Year: 2026}
	nuzno(t, repo.SaveJournal(ctxRaz, &j))
	list := models.JournalSheet{JournalID: j.ID, Date: danDnevnika}
	nuzno(t, repo.SaveSheet(ctxRaz, &list))
	upis := models.JournalEntry{JournalID: j.ID, Date: danDnevnika, Kind: models.EntryKindNote, Text: "probni upis"}
	nuzno(t, repo.SaveEntry(ctxRaz, &upis))
	dezurstvo := models.Dezurstvo{JournalID: j.ID, UserID: osnovaKorisnik.String(), UserName: "Proba Probić",
		Od: danDnevnika.Add(7 * time.Hour), Do: danDnevnika.Add(19 * time.Hour)}
	nuzno(t, repo.SaveDezurstvo(ctxRaz, &dezurstvo))
	tudi := models.JournalEntry{JournalID: osnovaDnevnik, Date: danDnevnika, Kind: models.EntryKindNote, Text: "drugi dnevnik"}
	nuzno(t, repo.SaveEntry(ctxRaz, &tudi))

	// upis koji je ovaj čvor primio, ali ga nema na površini
	kanal := journalChannel(&j)
	sirota := models.JournalEntry{ID: "01a0951f-0000-7000-8000-0000000000e9", JournalID: j.ID, Number: 2,
		Date: danDnevnika, Kind: models.EntryKindNote, Text: "upis koji ovdje nije prošao"}
	if _, err := c.rec.RecordIn(ctxRaz, c.db, kanal, EntityJournalEntries, sirota.ID, sirota); err != nil {
		t.Fatal(err)
	}

	n, err := repo.ArhivirajDnevnik(ctxRaz, &j)
	nuzno(t, err)
	if n != 2 {
		t.Errorf("javljeno je %d obrisanih zapisa i dežurstava, a s površine su otišla 2", n)
	}
	for _, z := range []struct{ entitet, id string }{
		{EntityJournalSheets, list.ID}, {EntityJournalEntries, upis.ID}, {EntityDezurstva, dezurstvo.ID},
		{EntityJournalEntries, sirota.ID}, {EntityJournals, j.ID},
	} {
		top, err := c.rec.Latest(ctxRaz, z.entitet, z.id)
		nuzno(t, err)
		if !top.Archived || top.Channel != kanal {
			t.Errorf("%s/%s: zadnja verzija arhivirana=%v u kanalu %q, a treba spomenik u kanalu %q",
				z.entitet, z.id, top.Archived, top.Channel, kanal)
		}
	}
	if top, err := c.rec.Latest(ctxRaz, EntityJournalEntries, tudi.ID); err != nil || top.Archived {
		t.Errorf("upis drugog dnevnika je arhiviran (%v)", err)
	}
	for _, tablica := range []string{"journal_entries", "journal_sheets", "dezurstva"} {
		if k := brojUDnevniku(t, c, tablica, j.ID); k != 0 {
			t.Errorf("u %s je ostalo %d zapisa obrisanog dnevnika", tablica, k)
		}
	}

	// novi čvor prima sve bez greške i bez ostataka dnevnika
	sNedovrsenima(t)
	b := noviCvor(t, "novi-ured")
	posalji(t, c, b)
	if k := brojUDnevniku(t, b, "journal_entries", j.ID); k != 0 {
		t.Errorf("novi čvor ima %d upisa obrisanog dnevnika", k)
	}
}

// Kad brisanje s površine ne uspije, dnevnik ostaje kakav jest
func TestBrisanjeDnevnikaNeUspije(t *testing.T) {
	for _, tablica := range []string{"journal_sheets", "journal_entries"} {
		t.Run(tablica, func(t *testing.T) {
			c := noviCvor(t, "ured")
			repo := NewJournalRepository(c.db, c.rec)
			j := models.Journal{AreaID: 16, Kind: models.JournalKindDefense, Title: "Probni dnevnik", Year: 2026}
			nuzno(t, repo.SaveJournal(ctxRaz, &j))
			_, err := c.db.Exec(`CREATE TRIGGER kvar BEFORE DELETE ON ` + tablica + ` BEGIN SELECT RAISE(ABORT, 'kvar'); END`)
			nuzno(t, err)
			nuzno(t, repo.SaveSheet(ctxRaz, &models.JournalSheet{JournalID: j.ID, Date: danDnevnika}))
			nuzno(t, repo.SaveEntry(ctxRaz, &models.JournalEntry{JournalID: j.ID, Date: danDnevnika, Kind: models.EntryKindNote, Text: "upis"}))
			if _, err := repo.ArhivirajDnevnik(ctxRaz, &j); err == nil {
				t.Fatal("brisanje je javilo uspjeh, a s površine nije otišlo")
			}
			if top, err := c.rec.Latest(ctxRaz, EntityJournals, j.ID); err != nil || top.Archived {
				t.Errorf("dnevnik je arhiviran iako brisanje nije uspjelo (%v)", err)
			}
		})
	}
}
