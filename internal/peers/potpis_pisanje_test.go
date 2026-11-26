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

// bezOznakePotpisa su naše oznake bez potpis-verzije/1, kakve objavljuje
// izdanje od prije provjere potpisa
func bezOznakePotpisa() []string {
	return slices.DeleteFunc(razmjena.NaseMogucnosti(), func(o string) bool { return o == razmjena.OznakaPotpisVerzije })
}

// Potpis autora ide razmjenom samo kad obje strane imaju oznaku
// potpis-verzije/1; inače druga strana dobije verziju bez potpisa, kao
// prije. Primatelj s oznakom potpis provjeri i spremi uz verziju.
// Namjerno promijenjeno: 3d.4; oznaka se objavljuje i primljeni potpis se
// sprema.
func TestPotpisZicomSamoUzOznaku(t *testing.T) {
	if !slices.Contains(razmjena.NaseMogucnosti(), razmjena.OznakaPotpisVerzije) {
		t.Fatal("ovo izdanje provjerava primljeni potpis, a oznaku potpis-verzije/1 ne objavljuje")
	}
	for _, slucaj := range []struct {
		ime              string
		posiljatelj, nas func() []string
		potpis           bool
	}{
		{"obje strane s oznakom (ovo izdanje)", nil, nil, true},
		{"primatelj bez oznake", nil, bezOznakePotpisa, false},
		{"pošiljatelj bez oznake", bezOznakePotpisa, nil, false},
	} {
		t.Run(slucaj.ime, func(t *testing.T) {
			a, b := serverILaptop(t)
			a.rec.PostaviPotpisnika(a.node.PotpisnikVerzija(Protocol))
			if slucaj.posiljatelj != nil {
				a.mogucnosti = slucaj.posiljatelj
			}
			if slucaj.nas != nil {
				b.mogucnosti = slucaj.nas
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
			v, err := b.rec.Latest(ctxRub, "probni", "potpisano")
			if err != nil {
				t.Fatal(err)
			}
			if slucaj.potpis {
				vrijediPotpis(t, a, Protocol, stiglo[0])
				vrijediPotpis(t, a, Protocol, *v)
				return
			}
			if stiglo[0].Potpis != "" || stiglo[0].KljucAutora != "" || v.Potpis != "" {
				t.Errorf("potpis je otišao strani bez oznake: %q, %q; u knjizi %q", stiglo[0].Potpis, stiglo[0].KljucAutora, v.Potpis)
			}
		})
	}
}

// .db arhiva nosi potpis autora uz verziju. Uvoz ga provjeri i spremi;
// potpis iz mreže s drugim imenom protokola ne vrijedi, pa takva verzija ne
// ulazi u knjigu i broji se u zapisu odbijenog. Arhiva starijeg programa,
// bez stupaca potpisa, uvozi se kao i do sada. Namjerno promijenjeno: 3d.4.
func TestPotpisUArhivi(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	kanal := "ocitanja/16/2026"
	izvezi := func(protokol, id string) (string, ledger.Version) {
		a.rec.PostaviPotpisnika(a.node.PotpisnikVerzija(protokol))
		upisiOcitanja(t, a, kanal, id)
		arhiva := filepath.Join(t.TempDir(), "arhiva.db")
		if _, err := a.ExportFile(ctxRub, arhiva, []string{kanal}); err != nil {
			t.Fatal(err)
		}
		in, err := sql.Open("sqlite", db.SamoZaCitanje(arhiva))
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		var u ledger.Version
		var archived int
		var payload string
		err = in.QueryRow(`SELECT version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel, potpis, kljuc_autora
			FROM record_versions WHERE entity_id = ?`, id).Scan(&u.VersionID, &u.Entity, &u.EntityID, &u.NodeID, &u.Supersedes, &archived, &payload, &u.CreatedAt, &u.SchemaVersion, &u.Channel, &u.Potpis, &u.KljucAutora)
		if err != nil {
			t.Fatal(err)
		}
		u.Archived, u.Payload = archived != 0, []byte(payload)
		vrijediPotpis(t, a, protokol, u)
		return arhiva, u
	}

	arhiva, _ := izvezi(Protocol, "o1")
	c := primateljOd(t, a)
	if rep, err := c.ImportFile(ctxRub, arhiva); err != nil || rep.Applied != 1 {
		t.Fatalf("uvoz: %+v, %v", rep, err)
	}
	if v, err := c.rec.Latest(ctxRub, "probni", "o1"); err != nil {
		t.Fatal(err)
	} else {
		vrijediPotpis(t, a, Protocol, *v)
	}

	tudja, _ := izvezi(protokolProbe, "o2")
	if rep, err := c.ImportFile(ctxRub, tudja); err != nil || rep.Applied != 0 {
		t.Fatalf("uvoz s potpisom druge mreže: %+v, %v", rep, err)
	}
	if _, err := c.rec.Latest(ctxRub, "probni", "o2"); !errors.Is(err, ledger.ErrNoVersion) {
		t.Errorf("verzija s krivim potpisom je u knjizi: %v", err)
	}
	if zapisi, err := c.Odbijeno(ctxRub, 1); err != nil || len(zapisi) != 1 || zapisi[0].Vrsta != VrstaVerzija ||
		zapisi[0].Razlog != RazlogKrivPotpis || zapisi[0].Broj != 1 || zapisi[0].Racunalo != "" {
		t.Errorf("zapis odbijenog: %+v, %v", zapisi, err)
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
	// namjerno promijenjeno: 3d.4; primatelj potpis provjeri i spremi
	if v, err := b.rec.Latest(ctxRub, "probni", "p1"); err != nil {
		t.Fatal(err)
	} else {
		vrijediPotpis(t, a, Protocol, *v)
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
