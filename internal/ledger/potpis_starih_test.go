package ledger

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"gocop/internal/razmjena"
)

const protokolStarih = "gocop-proba"

// podaciStarih su podaci verzije koje potpis pokriva, kao peers.PodaciVerzije
func podaciStarih(v Version) razmjena.PodaciVerzije {
	return razmjena.PodaciVerzije{
		VersionID: v.VersionID, Entity: v.Entity, EntityID: v.EntityID, NodeID: v.NodeID,
		Supersedes: v.Supersedes, Archived: v.Archived, Channel: v.Channel,
		SchemaVersion: v.SchemaVersion, CreatedAt: v.CreatedAt, Payload: v.Payload,
	}
}

// redakStarih je verzija iz knjige s tom oznakom
func redakStarih(t *testing.T, rec *Recorder, id string) Version {
	t.Helper()
	vv, err := rec.Verzije(context.Background(), []string{id})
	if err != nil || len(vv) != 1 {
		t.Fatalf("verzija %s: %v, %v", id, vv, err)
	}
	return vv[0]
}

// Stare vlastite verzije strogih entiteta dobiju potpis koji vrijedi nad
// retkom iz knjige. Tuđi redak, nestrog entitet, već potpisan redak,
// vlastiti redak primljen od drugih i redak koji razmjena ne bi primila
// ostaju kakvi jesu. Verzije bez vremena primanja (prije 0.0.70) potpisuju
// se samo uz prijePrimanja, i ono se pozove za svaku od njih. Drugi poziv i
// poziv bez potpisnika ne rade ništa (Odluke (27. 11. 2026.)).
func TestPotpisiVlastiteStare(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	rec := New(db, "cop-osijek")
	stroge := []string{"users", "duties"}
	upisi := func(kanal, entitet, zapis string) string {
		t.Helper()
		id, err := rec.RecordIn(ctx, db, kanal, entitet, zapis, map[string]string{"ime": "Ana <Horvat> & sin "})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}

	// kao u 0.0.69: bez potpisnika; jedna verzija bez vremena primanja
	staro := upisi("", "users", "u1")
	prijeStupca := upisi("", "duties", "d1")
	if _, err := db.Exec(`UPDATE record_versions SET primljeno = NULL WHERE version_id = ?`, prijeStupca); err != nil {
		t.Fatal(err)
	}
	nestrog := upisi("", "stations", "st1")
	krivKanal := upisi("a|b", "users", "u2")
	tudja := fiksna("01950000-0000-7000-8000-000000000001", "users", "u3", "laptop-a", "", pocetak)
	primljenaVlastita := fiksna("01950000-0000-7000-8000-000000000002", "users", "u4", "cop-osijek", "", pocetak)
	if _, err := rec.Apply(ctx, []Version{tudja, primljenaVlastita}); err != nil {
		t.Fatal(err)
	}
	// tuđa verzija primljena prije 0.0.70, bez vremena primanja
	if _, err := db.Exec(`UPDATE record_versions SET primljeno = NULL WHERE version_id = ?`, tudja.VersionID); err != nil {
		t.Fatal(err)
	}
	// vlastita verzija s oznakom ispred sata (ErrVlastitaUnaprijed)
	buduca := fiksna(oznakaZa(time.Now().Add(time.Hour)), "users", "u5", "cop-osijek", "", pocetak)
	if _, err := db.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, payload, created_at, primljeno)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, buduca.VersionID, buduca.Entity, buduca.EntityID, buduca.NodeID, string(buduca.Payload), pocetak, pocetak); err != nil {
		t.Fatal(err)
	}

	var prijePrimanja []string
	zabiljezi := func(v Version) { prijePrimanja = append(prijePrimanja, v.VersionID) }
	if n, err := rec.PotpisiVlastiteStare(ctx, stroge, zabiljezi); err != nil || n != 0 {
		t.Fatalf("bez potpisnika: %d, %v", n, err)
	}
	if v := redakStarih(t, rec, staro); v.Potpis != "" {
		t.Fatalf("bez potpisnika potpisano: %+v", v)
	}

	// već potpisana verzija, drugim potpisnikom: potpis joj ostaje
	rec.PostaviPotpisnika(func(Version) (string, string, error) { return "UA==", "Sw==", nil })
	potpisan := upisi("", "users", "u6")
	_, kljuc, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rec.PostaviPotpisnika(func(v Version) (string, string, error) {
		return razmjena.PotpisiVerziju(kljuc, protokolStarih, podaciStarih(v))
	})
	prije := map[string]Version{}
	for _, id := range []string{nestrog, krivKanal, tudja.VersionID, primljenaVlastita.VersionID, buduca.VersionID, potpisan, prijeStupca} {
		prije[id] = redakStarih(t, rec, id)
	}

	if n, err := rec.PotpisiVlastiteStare(ctx, stroge, nil); err != nil || n != 1 {
		t.Fatalf("bez verzija otprije 0.0.70: potpisano %d, %v", n, err)
	}
	if v := redakStarih(t, rec, prijeStupca); v.Potpis != "" {
		t.Error("verzija bez vremena primanja potpisana bez prijePrimanja")
	}
	if n, err := rec.PotpisiVlastiteStare(ctx, stroge, zabiljezi); err != nil || n != 1 {
		t.Fatalf("s verzijama otprije 0.0.70: potpisano %d, %v", n, err)
	}
	if fmt.Sprint(prijePrimanja) != fmt.Sprint([]string{prijeStupca}) {
		t.Errorf("javljene verzije otprije 0.0.70: %v, a potpisana je %s", prijePrimanja, prijeStupca)
	}
	for _, id := range []string{staro, prijeStupca} {
		v := redakStarih(t, rec, id)
		if v.Potpis == "" {
			t.Fatalf("%s/%s nije potpisana", v.Entity, v.EntityID)
		}
		if err := razmjena.ProvjeriPotpisVerzije(protokolStarih, podaciStarih(v), v.Potpis, v.KljucAutora); err != nil {
			t.Errorf("%s/%s: potpis ne vrijedi nad retkom iz knjige: %v", v.Entity, v.EntityID, err)
		}
	}
	delete(prije, prijeStupca)
	for id, bilo := range prije {
		if sad := redakStarih(t, rec, id); fmt.Sprintf("%+v", sad) != fmt.Sprintf("%+v", bilo) {
			t.Errorf("redak se promijenio:\n%+v\n%+v", bilo, sad)
		}
	}
	if n, err := rec.PotpisiVlastiteStare(ctx, stroge, zabiljezi); err != nil || n != 0 || len(prijePrimanja) != 1 {
		t.Errorf("drugi poziv: %d, %v, javljeno %v", n, err, prijePrimanja)
	}
}

// Više od jedne serije: sve se potpiše. Redak koji potpisnik ne može
// potpisati ostane bez potpisa i javi se u grešci, a ostali se potpišu.
// Cijela serija takvih redaka ne zaustavlja poziv: sljedeća serija čita
// iza zadnje pročitane oznake.
func TestPotpisiVlastiteStareUSerijama(t *testing.T) {
	for _, tc := range []struct {
		ime      string
		ukupno   int
		nevaljan func(i int) bool
	}{
		{"jedan redak se ne da potpisati", serijaStarih + 7, func(i int) bool { return i == 3 }},
		{"cijela serija se ne da potpisati", serijaStarih + 7, func(i int) bool { return i < serijaStarih+3 }},
	} {
		t.Run(tc.ime, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			db := openTestDB(t)
			rec := New(db, "cop-osijek")
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			nevaljanih := 0
			for i := range tc.ukupno {
				if _, err := rec.Record(ctx, tx, "users", fmt.Sprintf("u%d", i), map[string]int{"i": i}); err != nil {
					t.Fatal(err)
				}
				if tc.nevaljan(i) {
					nevaljanih++
				}
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			kvar := errors.New("ključ nije dostupan")
			rec.PostaviPotpisnika(func(v Version) (string, string, error) {
				var i int
				fmt.Sscanf(v.EntityID, "u%d", &i) //nolint:errcheck // oznake su iz petlje iznad
				if tc.nevaljan(i) {
					return "", "", kvar
				}
				return "UA==", "Sw==", nil
			})
			n, err := rec.PotpisiVlastiteStare(ctx, []string{"users"}, nil)
			if n != tc.ukupno-nevaljanih || !errors.Is(err, kvar) {
				t.Fatalf("potpisano %d od %d, %v", n, tc.ukupno-nevaljanih, err)
			}
			if pocetak := fmt.Sprintf("%d starih verzija ", nevaljanih); !strings.HasPrefix(err.Error(), pocetak) {
				t.Errorf("greška %q, a treba početi s %q", err, pocetak)
			}
			var bez int
			if err := db.QueryRow(`SELECT COUNT(*) FROM record_versions WHERE potpis = ''`).Scan(&bez); err != nil || bez != nevaljanih {
				t.Errorf("bez potpisa %d od %d, %v", bez, nevaljanih, err)
			}
		})
	}
}

// Greška baze prekine poziv i javi se: kad se transakcija ne da otvoriti,
// kad se stare verzije ne daju pročitati ili kad se potpis ne da upisati.
// Ništa se ne potpiše.
func TestPotpisiVlastiteStareGreskeBaze(t *testing.T) {
	potpisnik := func(Version) (string, string, error) { return "UA==", "Sw==", nil }
	for _, tc := range []struct {
		ime     string
		pokvari func(t *testing.T, db *sql.DB)
		greska  string
	}{
		{"zatvorena baza", func(t *testing.T, db *sql.DB) { db.Close() }, "closed"},
		{"stare se ne daju pročitati", func(t *testing.T, db *sql.DB) {
			for _, upit := range []string{
				`ALTER TABLE record_versions RENAME TO prave_verzije`,
				`CREATE VIEW record_versions AS SELECT version_id, entity, entity_id, node_id, supersedes, archived,
					payload, created_at, schema_version, channel, potpis, kljuc_autora FROM prave_verzije`,
			} {
				if _, err := db.Exec(upit); err != nil {
					t.Fatal(err)
				}
			}
		}, "čitanje starih vlastitih verzija"},
		{"redak se ne da pročitati", func(t *testing.T, db *sql.DB) {
			if _, err := db.Exec(`UPDATE record_versions SET archived = 'nije broj'`); err != nil {
				t.Fatal(err)
			}
		}, "archived"},
		{"potpis se ne da upisati", func(t *testing.T, db *sql.DB) {
			if _, err := db.Exec(`CREATE TRIGGER bez_potpisa BEFORE UPDATE ON record_versions
				BEGIN SELECT RAISE(ABORT, 'upis zabranjen'); END`); err != nil {
				t.Fatal(err)
			}
		}, "upis zabranjen"},
	} {
		t.Run(tc.ime, func(t *testing.T) {
			ctx := context.Background()
			db := openTestDB(t)
			rec := New(db, "cop-osijek")
			if _, err := rec.Record(ctx, db, "users", "u1", map[string]string{"ime": "Ana Horvat"}); err != nil {
				t.Fatal(err)
			}
			rec.PostaviPotpisnika(potpisnik)
			tc.pokvari(t, db)
			n, err := rec.PotpisiVlastiteStare(ctx, []string{"users"}, nil)
			if n != 0 || err == nil || !strings.Contains(err.Error(), tc.greska) {
				t.Fatalf("potpisano %d, %v; treba greška s %q", n, err, tc.greska)
			}
		})
	}
}

// Potpisnik koji vrati prazan potpis ili ključ bez greške ne upiše ništa:
// redak ostaje bez potpisa i javi se u grešci
func TestPotpisiVlastiteStarePrazanPotpis(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	rec := New(db, "cop-osijek")
	id, err := rec.Record(ctx, db, "users", "u1", map[string]string{"ime": "Ana Horvat"})
	if err != nil {
		t.Fatal(err)
	}
	rec.PostaviPotpisnika(func(Version) (string, string, error) { return "UA==", "", nil })
	n, err := rec.PotpisiVlastiteStare(ctx, []string{"users"}, nil)
	if n != 0 || err == nil || !strings.Contains(err.Error(), "users/u1 je prazan") {
		t.Fatalf("potpisano %d, %v", n, err)
	}
	if v := redakStarih(t, rec, id); v.Potpis != "" || v.KljucAutora != "" {
		t.Errorf("prazan potpis upisan: %+v", v)
	}
}
