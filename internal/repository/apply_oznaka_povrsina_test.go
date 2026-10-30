package repository

import (
	"bytes"
	"encoding/json"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Spomenik zapisa s brojčanom oznakom miče zapis samo kad je oznaka broj
// zapisan kao što ga zapisuje program. Spomenik pod „016” ne miče područje
// 16 ni njegove dionice i dnevnike, ni pri obnovi površine; spomenik pod
// „16” ih miče kao i dosad.
func TestSpomenikBrojcaneOznakeSamoUObliku(t *testing.T) {
	for _, s := range []struct {
		entitet, oznaka, tablica string
		id                       int
	}{
		{EntityAreas, "016", "areas", 16},
		{EntityAreas, "+16", "areas", 16},
		{EntityCounties, "0901", "counties", osnovaZupanija},
		{EntityMunicipalities, "+801", "municipalities", osnovaOpcina},
		{EntityAreas, "nije-broj", "areas", 16},
	} {
		t.Run(s.entitet+" "+s.oznaka, func(t *testing.T) {
			c := noviCvor(t, "b")
			spomenik := verzijaZa(t, s.entitet, s.oznaka, noviVid(t), map[string]int{"id": s.id})
			spomenik.Archived = true
			primi(t, c, []ledger.Version{spomenik})
			_, err := ReplaySurface(ctxRaz, c.db, c.rec)
			nuzno(t, err)
			if n := brojRedaka(t, c.db, `SELECT COUNT(*) FROM `+s.tablica+` WHERE id = ?`, s.id); n != 1 {
				t.Errorf("zapis %d: %d na površini", s.id, n)
			}
			for _, tablica := range []string{"sections", "journals", "municipalities"} {
				if n := redakaNaPovrsini(t, c, tablica); n != 1 {
					t.Errorf("%s: %d na površini", tablica, n)
				}
			}
			if u, err := c.rec.Latest(ctxRaz, s.entitet, s.oznaka); err != nil || u.VersionID != spomenik.VersionID {
				t.Errorf("spomenik nije ostao u knjizi: %v %v", u, err)
			}
		})
	}

	c := noviCvor(t, "b")
	spomenik := verzijaZa(t, EntityAreas, "16", noviVid(t), models.Area{ID: 16, SectorID: "B"})
	spomenik.Archived = true
	primi(t, c, []ledger.Version{spomenik})
	for _, tablica := range []string{"areas", "sections", "journals"} {
		if n := redakaNaPovrsini(t, c, tablica); n != 0 {
			t.Errorf("spomenik područja 16: %s ima %d redaka", tablica, n)
		}
	}
}

// Brisanje po broju ne briše ništa kad oznaka nije broj u obliku u kojem ga
// zapisuje program, ni kad se pozove mimo provjere oznake.
func TestObrisiPoBrojuSamoUObliku(t *testing.T) {
	c := noviCvor(t, "b")
	_, err := c.db.Exec(`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (17, 'B', 'Područje 17', 'VGI')`)
	nuzno(t, err)
	tx, err := c.db.BeginTx(ctxRaz, nil)
	nuzno(t, err)
	defer tx.Rollback()
	brisanje := primjene[EntityAreas].brisanje
	for _, oznaka := range []string{"017", "+17", "nije-broj"} {
		nuzno(t, brisanje(ctxRaz, tx, ledger.Version{Entity: EntityAreas, EntityID: oznaka, Archived: true}))
	}
	var n int
	nuzno(t, tx.QueryRow(`SELECT COUNT(*) FROM areas WHERE id = 17`).Scan(&n))
	if n != 1 {
		t.Errorf("područje 17 obrisano: %d", n)
	}
	nuzno(t, brisanje(ctxRaz, tx, ledger.Version{Entity: EntityAreas, EntityID: "17", Archived: true}))
	nuzno(t, tx.QueryRow(`SELECT COUNT(*) FROM areas WHERE id = 17`).Scan(&n))
	if n != 0 {
		t.Errorf("područje 17 nije obrisano oznakom 17: %d", n)
	}
}

// Spomenik koji po oznaci ne ide na površinu ne miče ni zapis ni njegovu
// djecu, i kad stigne izravno do primjene mimo izbora verzije.
func TestPrimjenaSpomenikaProvjeravaOznaku(t *testing.T) {
	c := noviCvor(t, "b")
	tx, err := c.db.BeginTx(ctxRaz, nil)
	nuzno(t, err)
	defer tx.Rollback()
	nuzno(t, primijeniVerziju(ctxRaz, tx, ledger.Version{VersionID: vid(1), Entity: EntityAreas, EntityID: "016",
		Archived: true, Payload: json.RawMessage(`{"id":16}`)}))
	for _, tablica := range []string{"areas", "sections", "journals"} {
		var n int
		nuzno(t, tx.QueryRow(`SELECT COUNT(*) FROM `+tablica).Scan(&n))
		if n != 1 {
			t.Errorf("%s: %d na površini", tablica, n)
		}
	}
}

// Greška čitanja retka vraća se, bez verzija.
func TestZadnjeZiveSIstomOznakomGreskaCitanja(t *testing.T) {
	c := noviCvor(t, "b")
	rows, err := c.db.Query(`SELECT 1`)
	nuzno(t, err)
	if v, err := zadnjeZiveSIstomOznakom(rows, EntitySluzbe); err == nil || v != nil {
		t.Errorf("redak krivog oblika: %v %v", v, err)
	}
}

// Brojčane oznake su točno entiteti čiji je zapis na površini cijeli broj.
func TestBrojcaneOznakeZaSveBrojcaneZapise(t *testing.T) {
	zaSlucajeveRazmjene(t)
	brojcani := map[string]bool{}
	for _, s := range slucajeviRazmjene() {
		if s.broj {
			brojcani[s.entitet] = true
		}
	}
	for e := range brojcani {
		if !brojcaneOznake[e] {
			t.Errorf("%s ima brojčanu oznaku, a nije u brojcaneOznake", e)
		}
	}
	for e := range brojcaneOznake {
		if !brojcani[e] {
			t.Errorf("%s je u brojcaneOznake, a zapis mu nije broj", e)
		}
	}
}

// Kad se općina vrati pod istom oznakom, vraća se i naselje u verziji koja
// po knjizi stoji na površini, i kad mu je zadnja verzija s drugom oznakom
// u sadržaju, s istom ili drugom općinom. Naselje obrisano prije te verzije
// ostaje obrisano. Površina nakon povratka jednaka je obnovi iz knjige.
func TestDijeteSeVracaSVerzijomSIstomOznakom(t *testing.T) {
	op := models.Municipality{ID: osnovaOpcina, CountyID: osnovaZupanija, Name: "Probna Općina", Type: "OPĆINA"}
	nas := models.Settlement{ID: 5001, MunicipalityID: osnovaOpcina, CountyID: osnovaZupanija, Name: "Naselje"}
	druga := func(opcina int) models.Settlement {
		x := nas
		x.ID, x.MunicipalityID, x.Name = 5002, opcina, "Drugo"
		return x
	}
	for _, s := range []struct {
		ime      string
		verzije  []ledger.Version
		ocekuje  int
		obrisano bool
	}{
		{"druga oznaka uz istu općinu", []ledger.Version{
			verzijaZa(t, EntitySettlements, "5001", vid(1), nas),
			verzijaZa(t, EntitySettlements, "5001", vid(3), druga(osnovaOpcina)),
		}, 1, false},
		{"druga oznaka uz drugu općinu", []ledger.Version{
			verzijaZa(t, EntitySettlements, "5001", vid(1), nas),
			verzijaZa(t, EntitySettlements, "5001", vid(3), druga(802)),
		}, 1, false},
		{"obrisano prije druge oznake", []ledger.Version{
			verzijaZa(t, EntitySettlements, "5001", vid(1), nas),
			sBrisanjem(verzijaZa(t, EntitySettlements, "5001", vid(1), nas), vid(2)),
			verzijaZa(t, EntitySettlements, "5001", vid(3), druga(osnovaOpcina)),
		}, 0, true},
	} {
		t.Run(s.ime, func(t *testing.T) {
			c := noviCvor(t, "b")
			_, err := c.db.Exec(`INSERT INTO municipalities (id, county_id, name, type) VALUES (802, ?, 'Druga Općina', 'OPĆINA')`, osnovaZupanija)
			nuzno(t, err)
			for _, v := range s.verzije {
				primi(t, c, []ledger.Version{v})
			}
			naselja := func(kad string) int {
				t.Helper()
				n := brojRedaka(t, c.db, `SELECT COUNT(*) FROM settlements WHERE id = 5001 AND name = 'Naselje'`)
				if m := brojRedaka(t, c.db, `SELECT COUNT(*) FROM settlements WHERE id = 5002`); m != 0 {
					t.Errorf("%s: na površini je naselje iz sadržaja druge oznake", kad)
				}
				return n
			}
			if n := naselja("prije brisanja"); n != s.ocekuje {
				t.Fatalf("prije brisanja općine: %d naselja", n)
			}
			spomenik := verzijaZa(t, EntityMunicipalities, "801", vid(10), op)
			spomenik.Archived = true
			primi(t, c, []ledger.Version{spomenik})
			if n := naselja("bez općine"); n != 0 {
				t.Fatalf("bez općine: %d naselja", n)
			}
			primi(t, c, []ledger.Version{verzijaZa(t, EntityMunicipalities, "801", vid(11), op)})
			if n := naselja("nakon povratka"); n != s.ocekuje {
				t.Errorf("nakon povratka općine: %d naselja, očekivano %d", n, s.ocekuje)
			}
			_, err = ReplaySurface(ctxRaz, c.db, c.rec)
			nuzno(t, err)
			if n := naselja("nakon obnove"); n != s.ocekuje {
				t.Errorf("nakon obnove površine: %d naselja, očekivano %d", n, s.ocekuje)
			}
		})
	}
}

// Kad se s trenutka makne bilješka koja je ondje stajala, trenutak dobiva
// bilješku u verziji koja po knjizi stoji na površini, i kad joj je zadnja
// verzija s drugom oznakom u sadržaju. Čvor koji je verzije primao jednu po
// jednu ima istu površinu kao čvor koji ih je primio odjednom.
func TestTrenutakDobivaVerzijuSIstomOznakom(t *testing.T) {
	kad := time.Date(2026, 3, 10, 6, 0, 0, 0, time.UTC)
	biljeska := func(id uuid.UUID, tekst string, izmijenjeno time.Time) models.ArhivaBiljeska {
		return models.ArhivaBiljeska{ID: id, Letva: "proba-letva", Velicina: "vodostaj", Korak: "satni", Vrijeme: kad,
			Vrsta: "napomena", Tekst: tekst, CreatedAt: izmijenjeno, UpdatedAt: izmijenjeno}
	}
	stoji, ceka, tudja := uuid.New(), uuid.New(), uuid.New()
	prva := verzijaZa(t, EntityBiljeske, stoji.String(), vid(1), biljeska(stoji, "stoji", kad.Add(2*time.Hour)))
	starija := verzijaZa(t, EntityBiljeske, ceka.String(), vid(2), biljeska(ceka, "čeka", kad.Add(time.Hour)))
	drugaOznaka := verzijaZa(t, EntityBiljeske, ceka.String(), vid(3), biljeska(tudja, "druga", kad.Add(3*time.Hour)))
	brisanje := sBrisanjem(prva, vid(4))
	sve := []ledger.Version{prva, starija, drugaOznaka, brisanje}

	jednaPoJedna, odjednom := noviCvor(t, "a"), noviCvor(t, "b")
	for _, v := range sve {
		primi(t, jednaPoJedna, []ledger.Version{v})
	}
	primi(t, odjednom, sve)
	for ime, c := range map[string]*cvor{"jedna po jedna": jednaPoJedna, "odjednom": odjednom} {
		var id, tekst string
		err := c.db.QueryRow(`SELECT id, tekst FROM arhiva_biljeske`).Scan(&id, &tekst)
		if err != nil || id != ceka.String() || tekst != "čeka" {
			t.Errorf("%s: na trenutku %q %q (%v), očekivana bilješka %s", ime, id, tekst, err, ceka)
		}
		if n := redakaNaPovrsini(t, c, "arhiva_biljeske"); n != 1 {
			t.Errorf("%s: bilješki na površini %d", ime, n)
		}
	}
}

// Popravak pri pokretanju ne dira zapis kojem zadnja verzija nosi u
// sadržaju drugu oznaku: ne prepisuje je kao verziju ovog čvora.
func TestPopravakPreskaceVerzijuDrugeOznake(t *testing.T) {
	c, e := cvorSEpizodom(t, "a")
	id := e.ID.String()
	top, err := c.rec.Latest(ctxRaz, EntityEpisodes, id)
	nuzno(t, err)
	druga := *e
	druga.ID = uuid.New()
	sadrzaj, err := json.Marshal(druga)
	nuzno(t, err)
	tudja := *top
	tudja.VersionID, tudja.NodeID, tudja.Payload, tudja.Supersedes = noviVid(t), "ured", sadrzaj, ""
	_, err = c.rec.Apply(ctxRaz, []ledger.Version{tudja})
	nuzno(t, err)
	prije := brojVerzija(t, c, EntityEpisodes, id)
	ok, err := popraviUTx(t, c, EntityEpisodes, id, func(m map[string]json.RawMessage) (bool, error) {
		m["phase"] = json.RawMessage(`"` + string(models.PhaseEmergency) + `"`)
		return true, nil
	})
	if err != nil || ok {
		t.Errorf("popravak verzije s drugom oznakom: %v, %v", ok, err)
	}
	if n := brojVerzija(t, c, EntityEpisodes, id); n != prije {
		t.Errorf("verzija zapisa: %d, prije %d", n, prije)
	}
}

// Zapis u dnevniku programa o verziji koja ne ide na površinu: polja s
// druge strane idu u navodnicima, oznaka iz sadržaja najviše 80 znakova, a
// nakon najviseZabiljezenih verzija jedan redak kaže da se dalje ne
// bilježe. Pamti se najviše najviseZabiljezenih verzija.
func TestZapisDrugeOznakeUDnevnikuOgranicen(t *testing.T) {
	zabiljezeneOznake.Lock()
	staro, preko := zabiljezeneOznake.verzije, zabiljezeneOznake.preko
	zabiljezeneOznake.verzije, zabiljezeneOznake.preko = map[string]bool{}, false
	zabiljezeneOznake.Unlock()
	var dnevnik bytes.Buffer
	prijeIzlaz := log.Writer()
	log.SetOutput(&dnevnik)
	t.Cleanup(func() {
		log.SetOutput(prijeIzlaz)
		zabiljezeneOznake.Lock()
		zabiljezeneOznake.verzije, zabiljezeneOznake.preko = staro, preko
		zabiljezeneOznake.Unlock()
	})

	v := verzijaZa(t, EntitySluzbe, "drugi\nrazmjena: redak", noviVid(t), models.Sluzba{ID: strings.Repeat("š", 300)})
	if IstaOznaka(v) {
		t.Fatal("verzija s drugom oznakom prošla je provjeru")
	}
	if n := strings.Count(dnevnik.String(), "\n"); n != 1 {
		t.Errorf("jedna verzija dala je %d redaka: %s", n, dnevnik.String())
	}
	if strings.Contains(dnevnik.String(), strings.Repeat("š", 81)) || !strings.Contains(dnevnik.String(), strings.Repeat("š", 80)) {
		t.Errorf("oznaka iz sadržaja nije skraćena na 80 znakova: %s", dnevnik.String())
	}
	for range najviseZabiljezenih + 50 {
		istaOznaka(verzijaZa(t, EntitySluzbe, "drugi", noviVid(t), models.Sluzba{ID: "sluzba-1"}))
	}
	if n := strings.Count(dnevnik.String(), "\n"); n != najviseZabiljezenih+1 {
		t.Errorf("redaka u dnevniku %d, očekivano %d", n, najviseZabiljezenih+1)
	}
	if !strings.Contains(dnevnik.String(), "dalje se ne bilježe") {
		t.Errorf("nema retka o granici: %s", dnevnik.String()[max(0, dnevnik.Len()-300):])
	}
	zabiljezeneOznake.Lock()
	n := len(zabiljezeneOznake.verzije)
	zabiljezeneOznake.Unlock()
	if n != najviseZabiljezenih {
		t.Errorf("zapamćeno %d verzija", n)
	}
}
