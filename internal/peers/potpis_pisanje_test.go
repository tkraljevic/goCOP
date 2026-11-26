package peers

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

const protokolProbe = "gocop-proba"

// vrijediPotpis javlja grešku kad verzija nema potpis ovog čvora koji vrijedi
func vrijediPotpis(t *testing.T, s *Service, protokol string, v ledger.Version) {
	t.Helper()
	if v.Potpis == "" || v.KljucAutora != s.node.PublicKey() {
		t.Fatalf("verzija %s/%s: potpis %q, ključ %q (čvor %q)", v.Entity, v.EntityID, v.Potpis, v.KljucAutora, s.node.PublicKey())
	}
	if err := razmjena.ProvjeriPotpisVerzije(protokol, PodaciVerzije(v), v.Potpis, v.KljucAutora); err != nil {
		t.Fatalf("verzija %s/%s: %v", v.Entity, v.EntityID, err)
	}
}

// Svaka verzija koju računalo upiše potpisana je ključem čvora, nad onim što
// je stvarno u knjizi (vrijeme, sadržaj s <, >, & i U+2028). Vlastita
// verzija je primljena kad je nastala. Bez potpisnika verzija ide bez
// potpisa, a potpisnik koji ne uspije ne ostavlja verziju u knjizi.
func TestPotpisPriUpisu(t *testing.T) {
	a := rubniCvor(t, "cop-osijek")
	a.rec.PostaviPotpisnika(a.node.PotpisnikVerzija(protokolProbe))

	kanal := "prijave/16/2026"
	if _, err := a.rec.RecordIn(ctxRub, a.db, kanal, "probni", "p1", map[string]any{"napomena": "a<b>&c d", "cm": 312}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.rec.ArchiveIn(ctxRub, a.db, kanal, "probni", "p1", map[string]any{"napomena": "obrisano"}); err != nil {
		t.Fatal(err)
	}
	povijest, err := a.rec.History(ctxRub, "probni", "p1")
	if err != nil || len(povijest) != 2 {
		t.Fatalf("povijest: %d, %v", len(povijest), err)
	}
	for _, v := range povijest {
		vrijediPotpis(t, a, protokolProbe, v)
	}
	if err := razmjena.ProvjeriPotpisVerzije("gocop", PodaciVerzije(povijest[0]), povijest[0].Potpis, povijest[0].KljucAutora); err == nil {
		t.Error("potpis iz mreže gocop-proba vrijedi i u mreži gocop")
	}
	var istoVrijeme int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM record_versions WHERE entity = 'probni' AND primljeno = created_at`).Scan(&istoVrijeme); err != nil || istoVrijeme != 2 {
		t.Errorf("vlastite verzije primljene kad su nastale: %d od 2, %v", istoVrijeme, err)
	}

	a.rec.PostaviPotpisnika(nil)
	if _, err := a.rec.Record(ctxRub, a.db, "probni", "p2", map[string]string{"x": "y"}); err != nil {
		t.Fatal(err)
	}
	if v, err := a.rec.Latest(ctxRub, "probni", "p2"); err != nil || v.Potpis != "" || v.KljucAutora != "" {
		t.Errorf("bez potpisnika: %+v, %v", v, err)
	}

	kvar := errors.New("ključ nije dostupan")
	a.rec.PostaviPotpisnika(func(ledger.Version) (string, string, error) { return "", "", kvar })
	if _, err := a.rec.Record(ctxRub, a.db, "probni", "p3", map[string]string{"x": "y"}); !errors.Is(err, kvar) {
		t.Errorf("potpisnik ne uspije: %v", err)
	}
	if _, err := a.rec.Latest(ctxRub, "probni", "p3"); !errors.Is(err, ledger.ErrNoVersion) {
		t.Errorf("verzija bez potpisa ostala u knjizi: %v", err)
	}
}

// sPotpisom su naše oznake i potpis-verzije/1, kakve će objaviti izdanje
// koje primljeni potpis provjerava
func sPotpisom() []string {
	o := append(razmjena.NaseMogucnosti(), razmjena.OznakaPotpisVerzije)
	slices.Sort(o)
	return o
}

// Potpis autora ide razmjenom samo kad obje strane imaju oznaku
// potpis-verzije/1; inače druga strana dobije verziju bez potpisa, kao
// prije. Ovo izdanje oznaku ne objavljuje, pa potpis žicom još ne ide.
// Primljeni potpis knjiga ne sprema dok ga ne provjeri (dolazi posebno).
func TestPotpisZicomSamoUzOznaku(t *testing.T) {
	if slices.Contains(razmjena.NaseMogucnosti(), razmjena.OznakaPotpisVerzije) {
		t.Fatal("oznaka potpis-verzije/1 objavljuje se prije nego što se primljeni potpis provjerava")
	}
	for _, slucaj := range []struct {
		ime     string
		oznake  func() []string
		potpis  bool
		jednaBe bool
	}{
		{"bez oznake (ovo izdanje)", nil, false, false},
		{"oznaka samo kod pošiljatelja", sPotpisom, false, true},
		{"oznaka kod obje strane", sPotpisom, true, false},
	} {
		t.Run(slucaj.ime, func(t *testing.T) {
			a, b := serverILaptop(t)
			a.rec.PostaviPotpisnika(a.node.PotpisnikVerzija(Protocol))
			if slucaj.oznake != nil {
				a.mogucnosti = slucaj.oznake
				if !slucaj.jednaBe {
					b.mogucnosti = slucaj.oznake
				}
			}
			var mu sync.Mutex
			var stiglo []ledger.Version
			b.Accept(func(v ledger.Version) bool {
				mu.Lock()
				defer mu.Unlock()
				if v.Entity == "probni" {
					stiglo = append(stiglo, v)
				}
				return true
			})
			upisiOcitanja(t, a, "ocitanja/16/2026", "potpisano")
			if v, err := a.rec.Latest(ctxRub, "probni", "potpisano"); err != nil {
				t.Fatal(err)
			} else {
				vrijediPotpis(t, a, Protocol, *v)
			}

			razgovaraj(t, b, a)
			mu.Lock()
			defer mu.Unlock()
			if len(stiglo) != 1 {
				t.Fatalf("stiglo %d verzija", len(stiglo))
			}
			if slucaj.potpis {
				vrijediPotpis(t, a, Protocol, stiglo[0])
			} else if stiglo[0].Potpis != "" || stiglo[0].KljucAutora != "" {
				t.Errorf("potpis otišao strani bez oznake: %q, %q", stiglo[0].Potpis, stiglo[0].KljucAutora)
			}
			if v, err := b.rec.Latest(ctxRub, "probni", "potpisano"); err != nil || v.Potpis != "" || v.KljucAutora != "" {
				t.Errorf("primljeni potpis upisan bez provjere: %+v, %v", v, err)
			}
		})
	}
}

// .db arhiva nosi potpis autora uz verziju. Uvoz ga čita, ali ga knjiga
// ne sprema dok ga ne provjeri; arhiva starijeg programa, bez stupaca
// potpisa, uvozi se kao i do sada.
func TestPotpisUArhivi(t *testing.T) {
	a := rubniCvor(t, "cop-osijek")
	a.rec.PostaviPotpisnika(a.node.PotpisnikVerzija(protokolProbe))
	kanal := "ocitanja/16/2026"
	upisiOcitanja(t, a, kanal, "o1")
	arhiva := filepath.Join(t.TempDir(), "arhiva.db")
	if n, err := a.ExportFile(ctxRub, arhiva, []string{kanal}); err != nil || n != 1 {
		t.Fatalf("izvoz: %d, %v", n, err)
	}
	in, err := sql.Open("sqlite", db.SamoZaCitanje(arhiva))
	if err != nil {
		t.Fatal(err)
	}
	var u ledger.Version
	u.Payload = nil
	var archived int
	var payload string
	err = in.QueryRow(`SELECT version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel, potpis, kljuc_autora
		FROM record_versions`).Scan(&u.VersionID, &u.Entity, &u.EntityID, &u.NodeID, &u.Supersedes, &archived, &payload, &u.CreatedAt, &u.SchemaVersion, &u.Channel, &u.Potpis, &u.KljucAutora)
	in.Close()
	if err != nil {
		t.Fatal(err)
	}
	u.Archived, u.Payload = archived != 0, []byte(payload)
	vrijediPotpis(t, a, protokolProbe, u)

	c := rubniCvor(t, "treci")
	if rep, err := c.ImportFile(ctxRub, arhiva); err != nil || rep.Applied != 1 {
		t.Fatalf("uvoz: %+v, %v", rep, err)
	}
	if v, err := c.rec.Latest(ctxRub, "probni", "o1"); err != nil || v.Potpis != "" {
		t.Errorf("potpis iz arhive upisan bez provjere: %+v, %v", v, err)
	}

	stara := staraArhiva(t, kanal)
	d := rubniCvor(t, "cetvrti")
	if rep, err := d.ImportFile(ctxRub, stara); err != nil || rep.Applied != 1 {
		t.Fatalf("uvoz arhive starijeg programa: %+v, %v", rep, err)
	}
}

// staraArhiva je .db arhiva kakvu izvozi program bez potpisa: knjiga bez
// stupaca potpis, kljuc_autora i primljeno, s jednom verzijom
func staraArhiva(t *testing.T, kanal string) string {
	t.Helper()
	put := filepath.Join(t.TempDir(), "stara.db")
	out, err := sql.Open("sqlite", db.Adresa(put))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE record_versions (version_id TEXT PRIMARY KEY, entity TEXT NOT NULL, entity_id TEXT NOT NULL,
			node_id TEXT NOT NULL, supersedes TEXT NOT NULL DEFAULT '', archived INTEGER NOT NULL DEFAULT 0,
			payload TEXT NOT NULL, created_at DATETIME NOT NULL, schema_version INTEGER NOT NULL DEFAULT 1,
			channel TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE archive_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`INSERT INTO archive_meta (key, value) VALUES ('program', 'goCOP'), ('node_id', 'stari')`,
	} {
		if _, err := out.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := out.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, payload, created_at, channel)
		VALUES (?, 'probni', 's1', 'stari', '{"id":"s1"}', ?, ?)`, id.String(), time.Now().UTC(), kanal); err != nil {
		t.Fatal(err)
	}
	return put
}

// Redak .cop paketa nosi potpis autora uz verziju. Primatelj ga čita, ali
// ga knjiga ne sprema dok ga ne provjeri.
func TestPotpisUPaketu(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	a.rec.PostaviPotpisnika(a.node.PotpisnikVerzija(Protocol))
	kanal := "prijave/16/2026"
	upisiOcitanja(t, a, kanal, "p1")
	_, put := izvezi(t, a, CopObuhvat{Vrsta: "prijave", AreaID: 16, Od: 2026, Do: 2026}, []string{kanal}, false)

	f, err := os.Open(put)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	p, err := otvoriCop(f, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	zapisi, _, err := p.zapisi()
	if err != nil {
		t.Fatal(err)
	}
	nadjen := false
	for _, v := range zapisi {
		if v.Entity == "probni" {
			nadjen = true
			vrijediPotpis(t, a, Protocol, v)
		}
	}
	if !nadjen {
		t.Fatal("verzije nema u paketu")
	}

	b := primateljOd(t, a)
	if _, err := b.UveziCop(ctxRub, put); err != nil {
		t.Fatal(err)
	}
	if v, err := b.rec.Latest(ctxRub, "probni", "p1"); err != nil || v.Potpis != "" {
		t.Errorf("potpis iz paketa upisan bez provjere: %+v, %v", v, err)
	}
}

// Naredba bez pokrenutog čvora potpisuje ključem čvora iz datoteke uz bazu.
// Bez datoteke nema potpisnika, a datoteka koja nije ključ je greška; ključ
// se nikad ne stvara.
func TestPotpisnikIzDatoteke(t *testing.T) {
	mapa := t.TempDir()
	baza := filepath.Join(mapa, "gocop.db")
	if p, err := PotpisnikIzDatoteke(baza, Protocol); p != nil || err != nil {
		t.Fatalf("bez ključa: %v, %v", p != nil, err)
	}
	if _, err := os.Stat(filepath.Join(mapa, KeyFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ključ je stvoren: %v", err)
	}

	n, err := LoadNode(baza, "cop-osijek", "cop-osijek", "test")
	if err != nil {
		t.Fatal(err)
	}
	p, err := PotpisnikIzDatoteke(baza, Protocol)
	if err != nil || p == nil {
		t.Fatalf("s ključem: %v", err)
	}
	v := ledger.Version{VersionID: "0190a1b2-0000-7000-8000-000000000002", Entity: "users", EntityID: "u1", NodeID: "cop-osijek",
		CreatedAt: time.Now().UTC(), SchemaVersion: 1, Payload: []byte(`{"x":1}`)}
	v.Potpis, v.KljucAutora, err = p(v)
	if err != nil || v.KljucAutora != n.PublicKey() {
		t.Fatalf("potpis: ključ %q, %v", v.KljucAutora, err)
	}
	if err := razmjena.ProvjeriPotpisVerzije(Protocol, PodaciVerzije(v), v.Potpis, v.KljucAutora); err != nil {
		t.Fatal(err)
	}

	krivo := filepath.Join(t.TempDir(), "gocop.db")
	if err := os.WriteFile(filepath.Join(filepath.Dir(krivo), KeyFileName), []byte("nije ključ"), 0o600); err != nil {
		t.Fatal(err)
	}
	if p, err := PotpisnikIzDatoteke(krivo, Protocol); p != nil || err == nil {
		t.Errorf("datoteka koja nije ključ: %v, %v", p != nil, err)
	}
}
