package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Do 13. 10. 2026. brisanje dionice odnosilo je njezine epizode obrane s
// površine (ON DELETE CASCADE), a u knjizi je spomenik dobila samo dionica.
// Epizoda je ostala živa u knjizi: novi čvor ju je čekao među nedovršenima
// zauvijek, a obnova površine pri pokretanju javljala je grešku svaki put,
// i na čvoru na kojem je dionica obrisana.

const obrisanaDionica = "B.16.7"

var danObrane = time.Date(2026, 9, 14, 6, 0, 0, 0, time.UTC)

func probnaDionica(opis string) models.Section {
	return models.Section{Code: obrisanaDionica, AreaID: 16, SectorID: "B", Description: opis, DescriptionCustom: true}
}

func probnaEpizoda(id, dionica string) models.DefenseEpisode {
	return models.DefenseEpisode{ID: uuid.MustParse(id), SectionCode: dionica, StartedAt: danObrane,
		Phase: models.PhaseRegular, DeclaredBy: "Ivo Probić", CreatedAt: danObrane, UpdatedAt: danObrane}
}

func epizodaNaPovrsini(t *testing.T, c *cvor, id string) bool {
	t.Helper()
	var n int
	nuzno(t, c.db.QueryRow(`SELECT count(*) FROM defense_episodes WHERE id = ?`, id).Scan(&n))
	return n == 1
}

func epizodaDioniceNaPovrsini(t *testing.T, c *cvor, dionica string) int {
	t.Helper()
	var n int
	nuzno(t, c.db.QueryRow(`SELECT count(*) FROM defense_episodes WHERE section_code = ?`, dionica).Scan(&n))
	return n
}

// Brisanje dionice daje spomenik svakoj njezinoj epizodi koja je u knjizi
// živa, i onoj koje na površini ovog čvora nema, sa zadnjom verzijom
// epizode. Epizoda druge dionice i epizoda obrisana ranije ostaju kakve jesu.
func TestBrisanjeDioniceArhiviraEpizode(t *testing.T) {
	c := noviCvor(t, "ured")
	dionice := NewSectionRepository(c.db, c.rec)
	sec := probnaDionica("dionica koja se briše")
	nuzno(t, dionice.SaveSection(ctxRaz, &sec))
	epizode := NewEpisodeRepository(c.db, c.rec)

	traje := models.DefenseEpisode{SectionCode: obrisanaDionica, StartedAt: danObrane, Phase: models.PhasePrep, DeclaredBy: "Ivo Probić"}
	nuzno(t, epizode.SaveEpisode(ctxRaz, &traje))
	zavrsena := models.DefenseEpisode{SectionCode: obrisanaDionica, StartedAt: danObrane, Phase: models.PhaseRegular, DeclaredBy: "Ivo Probić"}
	nuzno(t, epizode.SaveEpisode(ctxRaz, &zavrsena))
	kraj := danObrane.Add(30 * time.Hour)
	zavrsena.EndedAt, zavrsena.EndedBy = &kraj, "Ana Probić"
	nuzno(t, epizode.SaveEpisode(ctxRaz, &zavrsena))
	ranije := models.DefenseEpisode{SectionCode: obrisanaDionica, StartedAt: danObrane, Phase: models.PhasePrep}
	nuzno(t, epizode.SaveEpisode(ctxRaz, &ranije))
	nuzno(t, epizode.DeleteEpisode(ctxRaz, &ranije))
	tudja := models.DefenseEpisode{SectionCode: osnovaDionica, StartedAt: danObrane, Phase: models.PhasePrep, DeclaredBy: "Ana Probić"}
	nuzno(t, epizode.SaveEpisode(ctxRaz, &tudja))
	// epizoda koju je ovaj čvor primio, a na površini je nema
	sirota := probnaEpizoda("01a0951e-0000-7000-8000-0000000000a9", obrisanaDionica)
	if _, err := c.rec.Record(ctxRaz, c.db, EntityEpisodes, sirota.ID.String(), sirota); err != nil {
		t.Fatal(err)
	}

	nuzno(t, dionice.ArhivirajSekciju(ctxRaz, obrisanaDionica))

	for _, id := range []uuid.UUID{traje.ID, zavrsena.ID, sirota.ID} {
		top, err := c.rec.Latest(ctxRaz, EntityEpisodes, id.String())
		nuzno(t, err)
		if !top.Archived || top.Channel != "" {
			t.Errorf("epizoda %s: zadnja verzija arhivirana=%v u kanalu %q, a treba spomenik u zajedničkom kanalu", id, top.Archived, top.Channel)
		}
	}
	spomenik, err := c.rec.Latest(ctxRaz, EntityEpisodes, zavrsena.ID.String())
	nuzno(t, err)
	if e := teret[models.DefenseEpisode](t, spomenik.Payload); e.EndedBy != "Ana Probić" {
		t.Errorf("spomenik ne nosi zadnju verziju epizode: %+v", e)
	}
	if top, err := c.rec.Latest(ctxRaz, EntityEpisodes, tudja.ID.String()); err != nil || top.Archived {
		t.Errorf("epizoda druge dionice je arhivirana (%v)", err)
	}
	if povijest, err := c.rec.History(ctxRaz, EntityEpisodes, ranije.ID.String()); err != nil || len(povijest) != 2 {
		t.Errorf("epizoda obrisana ranije ima %d verzija, a treba upis i jedan spomenik (%v)", len(povijest), err)
	}
	if n := epizodaDioniceNaPovrsini(t, c, obrisanaDionica); n != 0 {
		t.Errorf("na površini je ostalo %d epizoda obrisane dionice", n)
	}
	if _, err := ReplaySurface(ctxRaz, c.db, c.rec); err != nil {
		t.Errorf("obnova površine na izvoru: %v", err)
	}

	// novi čvor prima sve bez greške, bez epizoda obrisane dionice i bez čekanja
	sNedovrsenima(t)
	b := noviCvor(t, "novi-ured")
	posalji(t, c, b)
	if n := epizodaDioniceNaPovrsini(t, b, obrisanaDionica); n != 0 {
		t.Errorf("novi čvor ima %d epizoda obrisane dionice", n)
	}
	if !epizodaNaPovrsini(t, b, tudja.ID.String()) {
		t.Error("novi čvor nema epizodu druge dionice")
	}
	if ceka := nedovrseniSada(); len(ceka) > 0 {
		t.Errorf("na novom čvoru čeka %v", ceka)
	}
}

// Kad spomenik epizode ne može u knjigu, dionica ostaje kakva jest
func TestBrisanjeDioniceNeUspije(t *testing.T) {
	c := noviCvor(t, "ured")
	dionice := NewSectionRepository(c.db, c.rec)
	sec := probnaDionica("dionica koja se ne da obrisati")
	nuzno(t, dionice.SaveSection(ctxRaz, &sec))
	e := models.DefenseEpisode{SectionCode: obrisanaDionica, StartedAt: danObrane, Phase: models.PhasePrep}
	nuzno(t, NewEpisodeRepository(c.db, c.rec).SaveEpisode(ctxRaz, &e))
	_, err := c.db.Exec(`CREATE TRIGGER kvar BEFORE INSERT ON record_versions
		WHEN NEW.entity = 'defense_episodes' AND NEW.archived = 1 BEGIN SELECT RAISE(ABORT, 'kvar'); END`)
	nuzno(t, err)

	if err := dionice.ArhivirajSekciju(ctxRaz, obrisanaDionica); err == nil {
		t.Fatal("brisanje je javilo uspjeh, a spomenik epizode nije upisan")
	}
	if s, _ := dionice.GetSectionByCode(obrisanaDionica); s == nil {
		t.Error("dionica je otišla s površine iako brisanje nije uspjelo")
	}
	if !epizodaNaPovrsini(t, c, e.ID.String()) {
		t.Error("epizoda je otišla s površine iako brisanje nije uspjelo")
	}
	if top, err := c.rec.Latest(ctxRaz, EntitySections, obrisanaDionica); err != nil || top.Archived {
		t.Errorf("dionica je arhivirana iako brisanje nije uspjelo (%v)", err)
	}
}

// Epizoda dionice obrisane prije ovog ispravka (spomenik samo za dionicu) ne
// ide na površinu novog čvora i ne čeka, ma kojim redom stigla. Isto vrijedi
// za izmjenu epizode s čvora koji za brisanje još nije znao.
func TestEpizodaObrisaneDioniceNeCeka(t *testing.T) {
	sec := probnaDionica("obrisana dionica")
	const idEpizode = "01a0951e-0000-7000-8000-0000000000a1"
	e := probnaEpizoda(idEpizode, obrisanaDionica)
	dionica := func(n int) ledger.Version { return verzijaZa(t, EntitySections, sec.Code, vid(n), sec) }
	brisanje := func(n int) ledger.Version { return spomenikZa(t, EntitySections, sec.Code, vid(n), sec) }
	epizoda := func(n int) ledger.Version { return verzijaZa(t, EntityEpisodes, idEpizode, vid(n), e) }

	for _, slucaj := range []struct {
		ime    string
		paketi [][]ledger.Version
	}{
		{"epizoda stiže nakon brisanja", [][]ledger.Version{{dionica(1), brisanje(3)}, {epizoda(2)}}},
		{"sve u jednom paketu", [][]ledger.Version{{dionica(1), epizoda(2), brisanje(3)}}},
		{"epizoda stiže prije dionice", [][]ledger.Version{{epizoda(2)}, {dionica(1), brisanje(3)}}},
		{"redom nastanka", [][]ledger.Version{{dionica(1)}, {epizoda(2)}, {brisanje(3)}}},
		{"epizoda izmijenjena nakon brisanja", [][]ledger.Version{{dionica(1), brisanje(2)}, {epizoda(3)}}},
	} {
		t.Run(slucaj.ime, func(t *testing.T) {
			sNedovrsenima(t)
			c := noviCvor(t, "novi-ured")
			for _, p := range slucaj.paketi {
				_ = primiPaket(t, c, p...)
			}
			if epizodaNaPovrsini(t, c, idEpizode) {
				t.Error("epizoda obrisane dionice je na površini")
			}
			if ceka := nedovrseniSada(); len(ceka) > 0 {
				t.Errorf("čeka %v", ceka)
			}
			if _, err := ReplaySurface(ctxRaz, c.db, c.rec); err != nil {
				t.Errorf("obnova površine pri pokretanju: %v", err)
			}
			if epizodaNaPovrsini(t, c, idEpizode) {
				t.Error("epizoda obrisane dionice vratila se pri pokretanju")
			}
		})
	}
}

// Dionica koja još nije stigla nije obrisana: epizoda čeka nju, ne preskače se
func TestEpizodaDioniceKojaNijeStiglaCeka(t *testing.T) {
	sNedovrsenima(t)
	c := noviCvor(t, "novi-ured")
	sec := probnaDionica("dionica koja stiže kasnije")
	e := probnaEpizoda("01a0951e-0000-7000-8000-0000000000a2", obrisanaDionica)
	if err := primiPaket(t, c, verzijaZa(t, EntityEpisodes, e.ID.String(), vid(2), e)); err == nil {
		t.Fatal("epizoda bez dionice prošla je strani ključ")
	}
	if !nedovrsen(EntityEpisodes + "|" + e.ID.String()) {
		t.Error("epizoda bez dionice ne čeka među nedovršenima")
	}
	nuzno(t, primiPaket(t, c, verzijaZa(t, EntitySections, sec.Code, vid(1), sec)))
	if !epizodaNaPovrsini(t, c, e.ID.String()) {
		t.Error("epizoda nije stigla na površinu kad je stigla njezina dionica")
	}
}

// Dionica obrisana prije ovog ispravka pa ponovno upisana pod istom šifrom:
// epizoda iz vremena prije brisanja ne vraća se ni na novom čvoru ni pri
// ponovnom pokretanju, a nova epizoda iste dionice ostaje.
func TestEpizodaPonovnoUpisaneDionice(t *testing.T) {
	stara := probnaDionica("dionica prije brisanja")
	nova := probnaDionica("dionica upisana ponovno")
	const idStare, idNove = "01a0951e-0000-7000-8000-0000000000a3", "01a0951e-0000-7000-8000-0000000000a4"
	verzije := []ledger.Version{
		verzijaZa(t, EntitySections, stara.Code, vid(1), stara),
		verzijaZa(t, EntityEpisodes, idStare, vid(2), probnaEpizoda(idStare, obrisanaDionica)),
		spomenikZa(t, EntitySections, stara.Code, vid(3), stara),
		verzijaZa(t, EntitySections, nova.Code, vid(4), nova),
		verzijaZa(t, EntityEpisodes, idNove, vid(5), probnaEpizoda(idNove, obrisanaDionica)),
	}
	provjeri := func(t *testing.T, c *cvor, kada string) {
		t.Helper()
		if epizodaNaPovrsini(t, c, idStare) {
			t.Errorf("%s: na površini je epizoda iz vremena prije brisanja dionice", kada)
		}
		if !epizodaNaPovrsini(t, c, idNove) {
			t.Errorf("%s: na površini nema nove epizode ponovno upisane dionice", kada)
		}
		if opis := opisDionice(t, c, obrisanaDionica); opis != nova.Description {
			t.Errorf("%s: na površini je dionica %q", kada, opis)
		}
	}

	for _, slucaj := range []struct {
		ime    string
		paketi [][]ledger.Version
	}{
		{"sve u jednom paketu", [][]ledger.Version{verzije}},
		{"redom nastanka", [][]ledger.Version{verzije[:1], verzije[1:2], verzije[2:3], verzije[3:4], verzije[4:]}},
	} {
		t.Run(slucaj.ime, func(t *testing.T) {
			sNedovrsenima(t)
			c := noviCvor(t, "novi-ured")
			for _, p := range slucaj.paketi {
				nuzno(t, primiPaket(t, c, p...))
			}
			provjeri(t, c, "nakon dostave")
			if _, err := ReplaySurface(ctxRaz, c.db, c.rec); err != nil {
				t.Errorf("obnova površine pri pokretanju: %v", err)
			}
			provjeri(t, c, "nakon ponovnog pokretanja")
		})
	}

	// brisanje stiže zaobilazno, nakon ponovnog upisa: čvor ga ne primijeni
	// jer je starije od ponovnog upisa, pa stara epizoda ostane na površini
	// do ponovnog pokretanja, kad je obnova makne
	t.Run("brisanje stiže nakon ponovnog upisa", func(t *testing.T) {
		sNedovrsenima(t)
		c := noviCvor(t, "novi-ured")
		nuzno(t, primiPaket(t, c, verzije[0], verzije[1], verzije[3], verzije[4]))
		nuzno(t, primiPaket(t, c, verzije[2]))
		if _, err := ReplaySurface(ctxRaz, c.db, c.rec); err != nil {
			t.Errorf("obnova površine pri pokretanju: %v", err)
		}
		provjeri(t, c, "nakon ponovnog pokretanja")
	})
}

// Neispravan sadržaj epizode i greška čitanja knjige vraćaju se kao greška
func TestPrimjenaEpizodeGreske(t *testing.T) {
	c := noviCvor(t, "novi-ured")
	tx, err := c.db.BeginTx(ctxRaz, nil)
	nuzno(t, err)
	defer tx.Rollback()
	if err := primijeniEpizodu(ctxRaz, tx, ledger.Version{Entity: EntityEpisodes, Payload: json.RawMessage(`{`)}); err == nil {
		t.Error("neispravan sadržaj epizode prošao je bez greške")
	}
	_, err = tx.ExecContext(ctxRaz, `DROP TABLE record_versions`)
	nuzno(t, err)
	v := verzijaZa(t, EntityEpisodes, "01a0951e-0000-7000-8000-0000000000a5", vid(1),
		probnaEpizoda("01a0951e-0000-7000-8000-0000000000a5", osnovaDionica))
	if err := primijeniEpizodu(ctxRaz, tx, v); err == nil {
		t.Error("epizoda je primijenjena iako se knjiga nije dala pročitati")
	}
}
