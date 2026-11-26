package repository

// Djelatnici i organizacija iz prvog punjenja ulaze u knjigu verzija
// (Odluke (26. 11. 2026.)): samo retci koje je punjenje upisalo na ovom
// računalu, tek kad je računalo u mreži, računi s popisa početnih lozinki
// tek kad se popis preuzme ili mu prođe rok, a račun admin nikad. Imena su
// izmišljena (testdata/sektor-p).

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
)

// napunjenaBaza je nova baza napunjena izmišljenim skupom, s popisom
// lozinki koji čeka preuzimanje, na računalu koje je zatim osnovalo mrežu
func napunjenaBaza(t *testing.T) (*UserRepository, *OrgRepository, *sql.DB) {
	t.Helper()
	r, org, baza := napunjenaBazaBezMreze(t)
	osnujMrezu(t, baza)
	return r, org, baza
}

// napunjenaBazaBezMreze je napunjenaBaza na računalu koje još nije ni u
// jednoj mreži
func napunjenaBazaBezMreze(t *testing.T) (*UserRepository, *OrgRepository, *sql.DB) {
	t.Helper()
	db.KoristiIzmisljenePodatke(t)
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "punjenje.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := baza.Close(); err != nil {
			t.Error(err)
		}
	})
	popis, err := db.SeedInitialData(baza)
	if err != nil {
		t.Fatal(err)
	}
	if len(popis) != 6 {
		t.Fatalf("popis lozinki ima %d računa, očekivano 6 (pet iz imenika i admin)", len(popis))
	}
	rec := ledger.New(baza, "cop-primjerovo")
	return NewUserRepository(baza, rec), NewOrgRepository(baza, rec), baza
}

// osnujMrezu upisuje mrežu kao da ju je ovo računalo osnovalo
func osnujMrezu(t *testing.T, baza *sql.DB) {
	t.Helper()
	if _, err := baza.Exec(`INSERT INTO network (id, name, public_key, joined_at) VALUES (1, 'Primjerska mreža', 'javni-kljuc', ?)`, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}

// verzijaZapisa je broj verzija zapisa u knjizi
func verzijaZapisa(t *testing.T, baza *sql.DB, entitet, id string) int {
	t.Helper()
	var n int
	if err := baza.QueryRow(`SELECT COUNT(*) FROM record_versions WHERE entity = ? AND entity_id = ?`, entitet, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// idsTablice su id-ovi svih redaka tablice, kao tekst
func idsTablice(t *testing.T, baza *sql.DB, tablica string) []string {
	t.Helper()
	rows, err := baza.Query(`SELECT CAST(id AS TEXT) FROM ` + tablica + ` ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}

func upisiPunjenje(t *testing.T, r *UserRepository, org *OrgRepository) int {
	t.Helper()
	n, err := r.UpisiPunjenje(context.Background(), org)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

var (
	idAdmina            = db.StableID("user", "admin").String()
	tabliceOrganizacije = map[string]string{"sectors": EntitySectors, "areas": EntityAreas}
)

// Dok popis početnih lozinki čeka, računi i njihove dužnosti ne ulaze u
// knjigu, pa obnova popisa i dalje vidi sve račune kao netaknute. Sektori i
// područja ne čekaju.
func TestPunjenjeDokPopisCekaNeUpisujeRacune(t *testing.T) {
	r, org, baza := napunjenaBaza(t)

	if n := upisiPunjenje(t, r, org); n != 6 {
		t.Fatalf("upisano %d verzija, očekivano 6 (tri sektora i tri područja)", n)
	}
	for tablica, entitet := range tabliceOrganizacije {
		for _, id := range idsTablice(t, baza, tablica) {
			if n := verzijaZapisa(t, baza, entitet, id); n != 1 {
				t.Errorf("%s %s ima %d verzija, očekivana jedna", tablica, id, n)
			}
		}
	}
	if n := brojRedaka(t, baza, `SELECT COUNT(*) FROM record_versions WHERE entity IN ('users', 'duties')`); n != 0 {
		t.Fatalf("dok popis čeka, u knjigu je ušlo %d verzija računa i dužnosti", n)
	}
	popis, err := db.ObnoviPocetneLozinke(baza)
	if err != nil {
		t.Fatal(err)
	}
	if len(popis) != 6 {
		t.Fatalf("obnova popisa vidi %d netaknutih računa, očekivano 6", len(popis))
	}
}

// Nakon preuzimanja popisa svaki račun osim admina i svaka njegova dužnost
// dobiju točno jednu verziju, sa sažetkom privremene lozinke i obaveznom
// promjenom; drugi poziv ne piše ništa.
func TestPunjenjeNakonPreuzimanjaPopisa(t *testing.T) {
	r, org, baza := napunjenaBaza(t)
	if err := db.PocetneLozinkePreuzete(baza); err != nil {
		t.Fatal(err)
	}

	// 3 sektora, 3 područja, 5 računa, 7 dužnosti
	if n := upisiPunjenje(t, r, org); n != 18 {
		t.Fatalf("upisano %d verzija, očekivano 18", n)
	}
	jednaVerzijaOsimAdmina(t, baza)
	if n := brojRedaka(t, baza, `SELECT COUNT(*) FROM punjenje_za_knjigu`); n != 0 {
		t.Errorf("nakon upisa ostalo je %d oznaka", n)
	}
	if n := brojRedaka(t, baza, `SELECT COUNT(*) FROM prava_vlastite`); n != 12 {
		t.Errorf("vlastitih verzija računa i dužnosti je %d, očekivano 12", n)
	}

	provjeriVerzijuRacuna(t, r, "iivic")

	if n := upisiPunjenje(t, r, org); n != 0 {
		t.Errorf("drugi poziv upisao je još %d verzija", n)
	}
	if n := brojRedaka(t, baza, `SELECT COUNT(*) FROM record_versions`); n != 18 {
		t.Errorf("u knjizi je %d verzija, očekivano 18", n)
	}
}

// jednaVerzijaOsimAdmina: svaki račun osim admina i svaka dužnost računa
// osim admina imaju točno jednu verziju, a admin i njegova dužnost nijednu
func jednaVerzijaOsimAdmina(t *testing.T, baza *sql.DB) {
	t.Helper()
	rows, err := baza.Query(`SELECT 'users', id, id FROM users UNION ALL SELECT 'duties', id, user_id FROM duties`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type zapis struct{ entitet, id, vlasnik string }
	var zapisi []zapis
	for rows.Next() {
		var z zapis
		if err := rows.Scan(&z.entitet, &z.id, &z.vlasnik); err != nil {
			t.Fatal(err)
		}
		zapisi = append(zapisi, z)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, z := range zapisi {
		ocekivano := 1
		if z.vlasnik == idAdmina {
			ocekivano = 0
		}
		if n := verzijaZapisa(t, baza, z.entitet, z.id); n != ocekivano {
			t.Errorf("%s %s (računa %s) ima %d verzija, očekivano %d", z.entitet, z.id, z.vlasnik, n, ocekivano)
		}
	}
}

// provjeriVerzijuRacuna: verzija računa iz imenika nosi sažetak privremene
// lozinke, obaveznu promjenu i zastavicu globalnog administratora
func provjeriVerzijuRacuna(t *testing.T, r *UserRepository, korisnik string) {
	t.Helper()
	top, err := r.rec.Latest(context.Background(), EntityUsers, db.StableID("user", korisnik).String())
	if err != nil {
		t.Fatal(err)
	}
	var uv userVersion
	if err := json.Unmarshal(top.Payload, &uv); err != nil {
		t.Fatal(err)
	}
	if uv.PasswordHash == "" || !uv.MustChangePassword || !uv.IsGlobalAdmin {
		t.Errorf("verzija računa %s: sažetak %q, obavezna promjena %v, globalni administrator %v",
			korisnik, uv.PasswordHash, uv.MustChangePassword, uv.IsGlobalAdmin)
	}
}

// Dok računalo nije ni u jednoj mreži, ništa ne ulazi u knjigu i oznake
// ostaju; kad osnuje mrežu, upiše se sve.
func TestPunjenjeBezMrezeCeka(t *testing.T) {
	r, org, baza := napunjenaBazaBezMreze(t)
	if err := db.PocetneLozinkePreuzete(baza); err != nil {
		t.Fatal(err)
	}

	if n := upisiPunjenje(t, r, org); n != 0 {
		t.Fatalf("računalo bez mreže upisalo je %d verzija", n)
	}
	if n := brojRedaka(t, baza, `SELECT COUNT(*) FROM record_versions`); n != 0 {
		t.Fatalf("u knjizi je %d verzija", n)
	}
	if n := brojRedaka(t, baza, `SELECT COUNT(*) FROM punjenje_za_knjigu`); n != 18 {
		t.Fatalf("ostalo je %d oznaka, očekivano 18", n)
	}

	osnujMrezu(t, baza)
	if n := upisiPunjenje(t, r, org); n != 18 {
		t.Errorf("nakon osnivanja mreže upisano je %d verzija, očekivano 18", n)
	}
	jednaVerzijaOsimAdmina(t, baza)
}

// Postojeća baza bez oznaka (napunjena prije ovog izdanja) se ne dira, i
// kad ima retke bez verzije.
func TestPunjenjePostojecaBazaBezOznaka(t *testing.T) {
	r, org, baza := napunjenaBaza(t)
	if err := db.PocetneLozinkePreuzete(baza); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`DELETE FROM punjenje_za_knjigu`); err != nil {
		t.Fatal(err)
	}

	if n := upisiPunjenje(t, r, org); n != 0 {
		t.Fatalf("baza bez oznaka dobila je %d verzija", n)
	}
	if n := brojRedaka(t, baza, `SELECT COUNT(*) FROM record_versions`); n != 0 {
		t.Fatalf("u knjizi je %d verzija", n)
	}
}

// Račun koji je već dobio verziju (prijava) ne dobiva drugu, a njegove
// dužnosti dobiju svoju. Dužnost koje više nema samo gubi oznaku.
func TestPunjenjeRacunSVerzijomIObrisanaDuznost(t *testing.T) {
	r, org, baza := napunjenaBaza(t)
	if err := db.PocetneLozinkePreuzete(baza); err != nil {
		t.Fatal(err)
	}
	pperic := db.StableID("user", "pperic")
	if err := r.MarkLogin(pperic, time.Now()); err != nil {
		t.Fatal(err)
	}
	obrisana := db.StableID("duty", "mmarkic|0").String()
	if _, err := baza.Exec(`DELETE FROM duties WHERE id = ?`, obrisana); err != nil {
		t.Fatal(err)
	}

	if n := upisiPunjenje(t, r, org); n != 16 {
		t.Fatalf("upisano %d verzija, očekivano 16 (bez računa pperic i obrisane dužnosti)", n)
	}
	if n := verzijaZapisa(t, baza, EntityUsers, pperic.String()); n != 1 {
		t.Errorf("račun pperic ima %d verzija, očekivana samo ona od prijave", n)
	}
	if n := verzijaZapisa(t, baza, EntityDuties, db.StableID("duty", "pperic|0").String()); n != 1 {
		t.Errorf("dužnost računa pperic ima %d verzija, očekivana jedna", n)
	}
	if n := verzijaZapisa(t, baza, EntityDuties, obrisana); n != 0 {
		t.Errorf("obrisana dužnost ima %d verzija", n)
	}
	if n := brojRedaka(t, baza, `SELECT COUNT(*) FROM punjenje_za_knjigu`); n != 0 {
		t.Errorf("ostalo je %d oznaka", n)
	}
}

// Greška u bilo kojem koraku poništava cijeli upis i ostavlja oznake, pa se
// sve ponovi pri sljedećem pozivu.
func TestPunjenjeGreskaPonistavaUpis(t *testing.T) {
	r, org, baza := napunjenaBaza(t)
	if err := db.PocetneLozinkePreuzete(baza); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`CREATE TRIGGER bez_duznosti BEFORE INSERT ON record_versions
		WHEN NEW.entity = 'duties' BEGIN SELECT RAISE(ABORT, 'dužnost ne može u knjigu'); END`); err != nil {
		t.Fatal(err)
	}

	if _, err := r.UpisiPunjenje(context.Background(), org); err == nil {
		t.Fatal("upis je prošao iako dužnost ne može u knjigu")
	}
	if n := brojRedaka(t, baza, `SELECT COUNT(*) FROM record_versions`); n != 0 {
		t.Fatalf("nakon greške u knjizi je ostalo %d verzija", n)
	}
	if n := brojRedaka(t, baza, `SELECT COUNT(*) FROM punjenje_za_knjigu`); n != 18 {
		t.Fatalf("nakon greške ostalo je %d oznaka, očekivano 18", n)
	}

	if _, err := baza.Exec(`DROP TRIGGER bez_duznosti`); err != nil {
		t.Fatal(err)
	}
	if n := upisiPunjenje(t, r, org); n != 18 {
		t.Errorf("ponovljeni upis napisao je %d verzija, očekivano 18", n)
	}
}

// Baza bez tablice oznaka javlja grešku, ne prazan upis
func TestPunjenjeBezTabliceOznaka(t *testing.T) {
	r, org, baza := napunjenaBaza(t)
	if _, err := baza.Exec(`DROP TABLE punjenje_za_knjigu`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.UpisiPunjenje(context.Background(), org); err == nil {
		t.Fatal("upis bez tablice oznaka nije javio grešku")
	}
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.UpisiPunjenje(context.Background(), org); err == nil {
		t.Fatal("upis u zatvorenu bazu nije javio grešku")
	}
}
