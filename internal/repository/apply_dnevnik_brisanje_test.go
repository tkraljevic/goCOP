package repository

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// oznakaU je oznaka verzije (UUIDv7) s vremenom nastanka kad; n razlikuje
// oznake istog trenutka
func oznakaU(kad time.Time, n int) string {
	ms := uint64(kad.UnixMilli())
	return fmt.Sprintf("%08x-%04x-7000-8000-%012d", ms>>16, ms&0xffff, n)
}

// dnevnikSDjecom su verzije dnevnika COP-a s upisom, listom i dežurstvom,
// ovjera na čvoru „ured” i brisanje cijelog dnevnika s čvora „laptop” koji
// ovjeru nije imao
type dnevnikSDjecom struct {
	j                  models.Journal
	pocetak, ovjera    []ledger.Version
	brisanje           []ledger.Version
	kad                time.Time
	upis, list, dezura string
}

// ovjerenSDjecom slaže dnevnikSDjecom; laptop briše dnevnik u trenutku obrisan
func ovjerenSDjecom(t *testing.T, obrisan time.Time) dnevnikSDjecom {
	t.Helper()
	kad := time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC)
	j := probniDnevnikCOP()
	d := dnevnikSDjecom{j: j, kad: kad,
		upis: "01a00000-0000-7000-8000-0000000e1001", list: "01a00000-0000-7000-8000-0000000e1002", dezura: "01a00000-0000-7000-8000-0000000e1003"}
	dan := kad.Add(-24 * time.Hour)
	upis := models.JournalEntry{ID: d.upis, JournalID: j.ID, Number: 1, Date: dan, Kind: models.EntryKindNote, Text: "vodostaj 312",
		UserID: "u-1", UserName: "Marko Marić", CreatedAt: dan, UpdatedAt: dan}
	list := models.JournalSheet{ID: d.list, JournalID: j.ID, Number: 1, Date: dan}
	dez := models.Dezurstvo{ID: d.dezura, JournalID: j.ID, UserID: "u-1", UserName: "Marko Marić", Od: dan, Do: dan.Add(8 * time.Hour),
		Opis: models.OpisiRada[0].Opis, Mjesto: models.MjestoUred, CreatedAt: dan, UpdatedAt: dan}
	d.pocetak = []ledger.Version{
		verzijaZa(t, EntityJournals, j.ID, oznakaU(dan.Add(-time.Hour), 1), j),
		verzijaZa(t, EntityJournalEntries, d.upis, oznakaU(dan, 2), upis),
		verzijaZa(t, EntityJournalSheets, d.list, oznakaU(dan, 3), list),
		verzijaZa(t, EntityDezurstva, d.dezura, oznakaU(dan, 4), dez),
	}
	o := j
	o.ZakljucioID, o.Zakljucio, o.ZakljucenoAt = "r-1", "Ana Anić", &kad
	d.ovjera = []ledger.Version{verzijaZa(t, EntityJournals, j.ID, oznakaU(kad, 5), o)}
	for i, v := range append(d.pocetak[1:], d.pocetak[0]) {
		b := v
		b.VersionID, b.NodeID, b.Archived = oznakaU(obrisan.Add(time.Duration(i)*time.Millisecond), 10+i), "laptop", true
		d.brisanje = append(d.brisanje, b)
	}
	return d
}

// stanjeOvjerenog javlja što od dnevnika i njegove djece nije na površini
func stanjeOvjerenog(t *testing.T, c *cvor, d dnevnikSDjecom) []string {
	t.Helper()
	var nedostaje []string
	j, err := NewJournalRepository(c.db, c.rec).GetJournal(ctxRaz, d.j.ID)
	nuzno(t, err)
	if j == nil || j.ZakljucenoAt == nil || !j.ZakljucenoAt.Equal(d.kad) {
		nedostaje = append(nedostaje, "ovjera")
	}
	for _, x := range []struct{ tablica, id string }{{"journal_entries", d.upis}, {"journal_sheets", d.list}, {"dezurstva", d.dezura}} {
		var ima bool
		nuzno(t, c.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM `+x.tablica+` WHERE id = ?)`, x.id).Scan(&ima))
		if !ima {
			nedostaje = append(nedostaje, x.tablica)
		}
	}
	return nedostaje
}

// spoji je jedan paket od više skupina verzija
func spoji(skupine ...[]ledger.Version) []ledger.Version {
	var out []ledger.Version
	for _, s := range skupine {
		out = append(out, s...)
	}
	return out
}

// Brisanje dnevnika s čvora koji ovjeru nije imao ne briše ovjeren dnevnik
// ni njegove upise, listove i dežurstva, kad je nastalo poslije ovjere i kad
// je nastalo prije nje, a stiglo kasnije; kojim god redom verzije stignu i
// nakon obnove površine. Brisanje ostaje u knjizi.
func TestBrisanjeBezOvjereNeBriseOvjerenDnevnik(t *testing.T) {
	sNedovrsenima(t)
	kad := time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC)
	for _, b := range []struct {
		opis string
		kad  time.Time
	}{{"brisanje poslije ovjere", kad.Add(time.Hour)}, {"brisanje prije ovjere", kad.Add(-time.Hour)}} {
		d := ovjerenSDjecom(t, b.kad)
		for _, s := range []struct {
			opis   string
			paketi [][]ledger.Version
		}{
			{"ovjera pa brisanje", [][]ledger.Version{spoji(d.pocetak, d.ovjera), d.brisanje}},
			{"brisanje pa ovjera", [][]ledger.Version{spoji(d.pocetak, d.brisanje), d.ovjera}},
			{"sve odjednom", [][]ledger.Version{spoji(d.brisanje, d.ovjera, d.pocetak)}},
		} {
			c := noviCvor(t, "cop-brisanje")
			for _, p := range s.paketi {
				nuzno(t, primiPaket(t, c, p...))
			}
			if n := stanjeOvjerenog(t, c, d); len(n) > 0 {
				t.Errorf("%s, %s: s površine je otišlo %v", b.opis, s.opis, n)
			}
			_, err := ReplaySurface(ctxRaz, c.db, c.rec)
			nuzno(t, err)
			_, err = ObnoviPovrsinu(ctxRaz, c.db, c.rec)
			nuzno(t, err)
			if n := stanjeOvjerenog(t, c, d); len(n) > 0 {
				t.Errorf("%s, %s: nakon obnove površine nema %v", b.opis, s.opis, n)
			}
			if povijest, err := c.rec.History(ctxRaz, EntityJournals, d.j.ID); err != nil || len(povijest) != 3 {
				t.Errorf("%s, %s: u knjizi nisu sve tri verzije zaglavlja: %d, %v", b.opis, s.opis, len(povijest), err)
			}
		}
	}
}

// Brisanje neovjerenog dnevnika briše dnevnik s upisima, listovima i
// dežurstvima, a vrijedi i brisanje koje nosi ovjeren dnevnik
func TestBrisanjeNeovjerenogDnevnikaBrise(t *testing.T) {
	sNedovrsenima(t)
	d := ovjerenSDjecom(t, time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC))
	c := noviCvor(t, "cop-brisanje")
	nuzno(t, primiPaket(t, c, d.pocetak...))
	nuzno(t, primiPaket(t, c, d.brisanje...))
	if n := stanjeOvjerenog(t, c, d); len(n) != 4 {
		t.Errorf("brisanje neovjerenog dnevnika ostavilo je dio: nedostaje samo %v", n)
	}

	e := noviCvor(t, "cop-brisanje-2")
	nuzno(t, primiPaket(t, e, spoji(d.pocetak, d.ovjera)...))
	ovjereno := d.brisanje[len(d.brisanje)-1]
	ovjereno.Payload = d.ovjera[0].Payload
	nuzno(t, primiPaket(t, e, append(d.brisanje[:len(d.brisanje)-1:len(d.brisanje)-1], ovjereno)...))
	if n := stanjeOvjerenog(t, e, d); len(n) != 4 {
		t.Errorf("brisanje ovjerenog dnevnika ostavilo je dio: nedostaje samo %v", n)
	}

	// neovjeren dnevnik koji se vrati ne vraća obrisane zapise
	vraca := d.pocetak[0]
	vraca.VersionID = oznakaU(time.Date(2026, 10, 30, 16, 0, 0, 0, time.UTC), 20)
	nuzno(t, primiPaket(t, c, vraca))
	if n := stanjeOvjerenog(t, c, d); len(n) != 4 {
		t.Errorf("vraćen neovjeren dnevnik: nedostaje samo %v", n)
	}
	if j, err := NewJournalRepository(c.db, c.rec).GetJournal(ctxRaz, d.j.ID); err != nil || j == nil {
		t.Errorf("neovjeren dnevnik se nije vratio: %+v, %v", j, err)
	}
}

// Dežurstvo iz plana ovjerenog dnevnika miče se kad ga uprava centra makne
// uz razlog i kad je maknuto prije ovjere; brisanje poslije ovjere bez
// razloga ga ne miče
func TestMicanjeDezurstvaOvjerenogPlana(t *testing.T) {
	sNedovrsenima(t)
	d := ovjerenSDjecom(t, time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC))
	dez := d.pocetak[3]
	micanje := func(kad time.Time, napomena string) ledger.Version {
		var x models.Dezurstvo
		nuzno(t, json.Unmarshal(dez.Payload, &x))
		x.Napomena = napomena
		v := verzijaZa(t, EntityDezurstva, d.dezura, oznakaU(kad, 30), x)
		v.NodeID, v.Archived = "laptop", true
		return v
	}
	razlog := models.TragIspravkaNakonOvjere("Ana Anić", d.kad.Add(2*time.Hour), "dvaput upisano")
	for _, s := range []struct {
		opis   string
		paketi [][]ledger.Version
		ostaje bool
	}{
		{"uprava centra uz razlog", [][]ledger.Version{spoji(d.pocetak, d.ovjera), {micanje(d.kad.Add(2*time.Hour), razlog)}}, false},
		{"bez razloga poslije ovjere", [][]ledger.Version{spoji(d.pocetak, d.ovjera), {micanje(d.kad.Add(2*time.Hour), "")}}, true},
		{"prije ovjere", [][]ledger.Version{d.pocetak, {micanje(d.kad.Add(-2*time.Hour), "")}, d.ovjera}, false},
		{"uz razlog, pa brisanje dnevnika s laptopa", [][]ledger.Version{spoji(d.pocetak, d.ovjera), {micanje(d.kad.Add(2*time.Hour), razlog)}, d.brisanje}, false},
	} {
		c := noviCvor(t, "cop-dezurstvo")
		for _, p := range s.paketi {
			nuzno(t, primiPaket(t, c, p...))
		}
		_, err := ObnoviPovrsinu(ctxRaz, c.db, c.rec)
		nuzno(t, err)
		var ima bool
		nuzno(t, c.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM dezurstva WHERE id = ?)`, d.dezura).Scan(&ima))
		if ima != s.ostaje {
			t.Errorf("%s: dežurstvo u planu %v, očekivano %v", s.opis, ima, s.ostaje)
		}
	}
}

// Sažimanje knjige ne briše ovjeru dnevnika ni zadnju živu verziju zapisa
// koje brisanje s čvora bez ovjere nije odnijelo
func TestSazimanjeCuvaOvjerenDnevnik(t *testing.T) {
	sNedovrsenima(t)
	d := ovjerenSDjecom(t, time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC))
	c := noviCvor(t, "cop-sazimanje")
	drugi := probniDnevnikCOP()
	drugi.ID = "01a00000-0000-7000-8000-0000000d0002"
	kasnije := verzijaZa(t, EntityJournals, drugi.ID, oznakaU(d.kad.Add(3*time.Hour), 40), drugi)
	prvo := d.pocetak[0]
	prvo.VersionID = oznakaU(d.kad.Add(-72*time.Hour), 41)
	nuzno(t, primiPaket(t, c, spoji([]ledger.Version{prvo}, d.pocetak, d.ovjera, d.brisanje, []ledger.Version{kasnije})...))
	_, err := c.db.Exec(`UPDATE record_versions SET created_at = ?`, time.Now().AddDate(0, 0, -200))
	nuzno(t, err)
	obrisano, err := SazmiKnjigu(ctxRaz, c.db, c.rec, time.Now().AddDate(0, 0, -90))
	nuzno(t, err)
	if obrisano != 1 {
		t.Errorf("sažimanje je obrisalo %d verzija, a samo je prvo zaglavlje i staro i zamijenjeno", obrisano)
	}
	_, err = c.db.Exec(`DELETE FROM journals; DELETE FROM journal_entries; DELETE FROM journal_sheets; DELETE FROM dezurstva`)
	nuzno(t, err)
	_, err = ReplaySurface(ctxRaz, c.db, c.rec)
	nuzno(t, err)
	_, err = ObnoviPovrsinu(ctxRaz, c.db, c.rec)
	nuzno(t, err)
	if n := stanjeOvjerenog(t, c, d); len(n) > 0 {
		t.Errorf("nakon sažimanja i obnove površine nema %v", n)
	}
}

// Pravilo za dijete dnevnika bez povijesti dnevnika, s brisanjem ovjerenog
// dnevnika i bez žive verzije: brisanje vrijedi
func TestZivoDijeteOvjerenogRubovi(t *testing.T) {
	d := ovjerenSDjecom(t, time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC))
	upis := []ledger.Version{d.brisanje[0], d.pocetak[1]}
	dnevnik := []ledger.Version{d.ovjera[0], d.pocetak[0]}
	if _, ostaje := zivoDijeteOvjerenog(upis, dnevnik); !ostaje {
		t.Error("upis ovjerenog dnevnika je otišao s brisanjem")
	}
	obrisanOvjeren := d.ovjera[0]
	obrisanOvjeren.Archived = true
	for _, s := range []struct {
		opis    string
		dijete  []ledger.Version
		dnevnik []ledger.Version
	}{
		{"dnevnik nije u knjizi", upis, nil},
		{"dnevnik nije ovjeren", upis, d.pocetak[:1]},
		{"obrisan je ovjeren dnevnik", upis, []ledger.Version{obrisanOvjeren, d.pocetak[0]}},
		{"nema žive verzije", upis[:1], dnevnik},
	} {
		if _, ostaje := zivoDijeteOvjerenog(s.dijete, s.dnevnik); ostaje {
			t.Errorf("%s: upis je ostao, a brisanje vrijedi", s.opis)
		}
	}
	if got := dnevnikDjeteta(ledger.Version{Payload: json.RawMessage(`{`)}); got != "" {
		t.Errorf("nečitljiv sadržaj daje dnevnik %q", got)
	}
}

// Greška čitanja knjige vraća se, a ne odlučuje o površini
func TestDijeteDnevnikaGreskaCitanja(t *testing.T) {
	d := ovjerenSDjecom(t, time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC))
	c := noviCvor(t, "cop-greska")
	tx, err := c.db.Begin()
	nuzno(t, err)
	nuzno(t, tx.Rollback())
	if _, err := povijestTx(ctxRaz, tx, EntityJournals, d.j.ID); err == nil {
		t.Error("čitanje u završenoj transakciji nije javilo grešku")
	}
	nuzno(t, c.db.Close())
	if _, ide, err := dijeteDnevnikaZaPovrsinu(ctxRaz, c.rec, d.brisanje[0]); err == nil || ide {
		t.Errorf("čitanje zatvorene baze: ide %v, greška %v", ide, err)
	}
}
