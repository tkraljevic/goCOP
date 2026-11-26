package peers

// Djelatnici i organizacija iz imenika računala koje je osnovalo mrežu
// stižu razmjenom na računalo bez imenika (Odluke (26. 11. 2026.)): s istim
// id-ovima i bez dvojnika, a globalni administrator iz imenika vrijedi
// odmah, jer ga je upisalo računalo s ključem mreže. Računalo primljeno u
// postojeću mrežu svoje punjenje ne šalje, pa ne vraća stanje iz datoteka
// preko izmjena napravljenih u mreži. Imena su izmišljena (testdata/sektor-p).

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/razmjena"
	"gocop/internal/repository"
)

// napuniBezPodataka puni bazu kao svježe računalo bez imenika i registara:
// samo račun admin
func napuniBezPodataka(t *testing.T, s *Service) {
	t.Helper()
	staraMapa, stariImenik := db.DataDir, db.ImenikPath
	defer func() { db.DataDir, db.ImenikPath = staraMapa, stariImenik }()
	db.DataDir = t.TempDir()
	db.ImenikPath = filepath.Join(db.DataDir, "imenik.json")
	if _, err := db.SeedInitialData(s.db); err != nil {
		t.Fatal(err)
	}
}

// napuniIzImenika puni bazu izmišljenim skupom (imenik i organizacija) i
// preuzme popis lozinki
func napuniIzImenika(t *testing.T, s *Service) {
	t.Helper()
	db.KoristiIzmisljenePodatke(t)
	if _, err := db.SeedInitialData(s.db); err != nil {
		t.Fatal(err)
	}
	if err := db.PocetneLozinkePreuzete(s.db); err != nil {
		t.Fatal(err)
	}
}

// upisiPunjenjeNa upisuje punjenje računala u knjigu kao program pri
// pokretanju i nakon popisa lozinki, i vraća broj napisanih verzija
func upisiPunjenjeNa(t *testing.T, s *Service) int {
	t.Helper()
	n, err := racuniNa(s).UpisiPunjenje(ctxRub, repository.NewOrgRepository(s.db, s.rec))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// napunjenaMreza: svako računalo se najprije napuni (punjenja[i] puni
// računalo ids[i]), pa prvo osnuje mrežu i primi ostala, kao u programu.
// Osnivač pri osnivanju upiše punjenje u knjigu i potpiše stanje prava, kao
// cmd/gocop kroz NakonKljucaMreze, a primljena računala punjenje odbace.
func napunjenaMreza(t *testing.T, ids []string, punjenja ...func(*testing.T, *Service)) []*Service {
	t.Helper()
	cv := make([]*Service, len(ids))
	for i, id := range ids {
		cv[i] = rubniCvor(t, id)
		punjenja[i](t, cv[i])
	}
	k := cv[0]
	k.NakonKljucaMreze(func(ctx context.Context) {
		upisiPunjenjeNa(t, k)
		if _, err := repository.PotpisiStanjePrava(ctx, k.db, k.rec, k); err != nil {
			t.Fatal(err)
		}
	})
	spojiUMrezu(t, cv)
	return cv
}

// retciPovrsine su id-ovi i imena redaka tablice na površini, poredani
func retciPovrsine(t *testing.T, baza *sql.DB, upit string) []string {
	t.Helper()
	rows, err := baza.Query(upit)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

// upitiPovrsine čitaju djelatnike i organizaciju; račun admin svako
// računalo ima svoj, pa se ne uspoređuje
var upitiPovrsine = map[string]string{
	"računi":   `SELECT id || ' ' || username || ' ' || password_hash || ' ' || must_change_password FROM users WHERE username <> 'admin'`,
	"dužnosti": `SELECT d.id || ' ' || d.user_id || ' ' || d.role || ' ' || d.title FROM duties d JOIN users u ON u.id = d.user_id WHERE u.username <> 'admin'`,
	"sektori":  `SELECT id || ' ' || phone FROM sectors`,
	"područja": `SELECT id || ' ' || sector_id || ' ' || name FROM areas`,
}

// istaPovrsina usporedi djelatnike i organizaciju na dva računala
func istaPovrsina(t *testing.T, a, b *Service) {
	t.Helper()
	for ime, upit := range upitiPovrsine {
		ra, rb := retciPovrsine(t, a.db, upit), retciPovrsine(t, b.db, upit)
		if !slices.Equal(ra, rb) {
			t.Errorf("%s: na %s %v, na %s %v", ime, a.node.ID, ra, b.node.ID, rb)
		}
	}
}

// broj je rezultat upita COUNT(*)
func broj(t *testing.T, baza *sql.DB, upit string, args ...any) int {
	t.Helper()
	var n int
	if err := baza.QueryRow(upit, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Računalo koje je osnovalo mrežu drži ključ: računalo bez imenika dobije
// iste račune, dužnosti, sektore i područja, a globalni administrator iz
// imenika vrijedi bez čekanja
func TestPunjenjeStizeRacunaluBezImenika(t *testing.T) {
	cv := napunjenaMreza(t, []string{"cop-primjerovo", "laptop-primjerovo"}, napuniIzImenika, napuniBezPodataka)
	a, b := cv[0], cv[1]
	// 3 sektora, 3 područja, 5 računa, 7 dužnosti, upisani pri osnivanju
	if n := broj(t, a.db, `SELECT COUNT(*) FROM record_versions WHERE entity IN ('users', 'duties', 'sectors', 'areas')`); n != 18 {
		t.Fatalf("osnivač je pri osnivanju upisao %d verzija punjenja, očekivano 18", n)
	}

	razgovaraj(t, b, a)

	istaPovrsina(t, a, b)
	if n := len(retciPovrsine(t, b.db, upitiPovrsine["računi"])); n != 5 {
		t.Errorf("na računalu bez imenika je %d računa iz imenika, očekivano 5", n)
	}
	if n := len(retciPovrsine(t, b.db, upitiPovrsine["dužnosti"])); n != 7 {
		t.Errorf("na računalu bez imenika je %d dužnosti iz imenika, očekivano 7", n)
	}
	if !zastavica(t, b, db.StableID("user", "iivic")) || len(cekaNa(t, b)) != 0 {
		t.Errorf("globalni administrator s računala s ključem čeka: %+v", cekaNa(t, b))
	}
	// ponovljena razmjena ništa ne mijenja
	razgovaraj(t, b, a)
	istaPovrsina(t, a, b)
}

// izmijeniUMrezi mijenja na računalu lozinku računa mmarkic, naslov
// njegove dužnosti i telefon sektora P, kao rad u mreži poslije punjenja
func izmijeniUMrezi(t *testing.T, s *Service) {
	t.Helper()
	racuni := racuniNa(s)
	if err := racuni.ChangePassword(db.StableID("user", "mmarkic"), "nova-lozinka-mmarkic"); err != nil {
		t.Fatal(err)
	}
	d, err := racuni.GetDuty(db.StableID("duty", "mmarkic|0"))
	if err != nil {
		t.Fatal(err)
	}
	d.Title = "Preimenovana dužnost"
	if err := racuni.UpdateDuty(d); err != nil {
		t.Fatal(err)
	}
	org := repository.NewOrgRepository(s.db, s.rec)
	sektor, err := org.GetSector(ctxRub, "P")
	if err != nil {
		t.Fatal(err)
	}
	sektor.Phone = "000 000 099"
	if err := org.SaveSector(ctxRub, sektor); err != nil {
		t.Fatal(err)
	}
}

// Računalo napunjeno istim imenikom i organizacijom (npr. kopirana mapa
// data) primljeno je u mrežu: punjenje ne šalje ni pri pokretanju ni nakon
// popisa lozinki, pa izmjene iz mreže ostaju na svim računalima, a za
// globalnog administratora ne čeka nikakva promjena
func TestPunjenjePrimljenogRacunalaNeVracaDatoteke(t *testing.T) {
	cv := napunjenaMreza(t, []string{"cop-primjerovo", "cop-rubno"}, napuniIzImenika, napuniIzImenika)
	a, c := cv[0], cv[1]
	izmijeniUMrezi(t, a)

	if n := upisiPunjenjeNa(t, c); n != 0 {
		t.Fatalf("primljeno računalo upisalo je %d verzija punjenja", n)
	}
	if n := broj(t, c.db, `SELECT COUNT(*) FROM punjenje_za_knjigu`); n != 0 {
		t.Errorf("primljenom računalu ostalo je %d oznaka punjenja", n)
	}
	razgovaraj(t, c, a)

	if n := broj(t, a.db, `SELECT COUNT(*) FROM record_versions WHERE node_id = ? AND entity IN ('users', 'duties', 'sectors', 'areas')`, c.node.ID); n != 0 {
		t.Errorf("s primljenog računala stiglo je %d verzija djelatnika i organizacije", n)
	}
	for _, s := range cv {
		if n := broj(t, s.db, `SELECT COUNT(*) FROM users WHERE username = 'mmarkic' AND password_hash = 'nova-lozinka-mmarkic' AND must_change_password = 0`); n != 1 {
			t.Errorf("na %s lozinka računa mmarkic nije ona iz mreže", s.node.ID)
		}
		if n := broj(t, s.db, `SELECT COUNT(*) FROM duties WHERE id = ? AND title = 'Preimenovana dužnost'`, db.StableID("duty", "mmarkic|0").String()); n != 1 {
			t.Errorf("na %s naslov dužnosti nije onaj iz mreže", s.node.ID)
		}
		if n := broj(t, s.db, `SELECT COUNT(*) FROM sectors WHERE id = 'P' AND phone = '000 000 099'`); n != 1 {
			t.Errorf("na %s telefon sektora P nije onaj iz mreže", s.node.ID)
		}
	}
	if ceka := cekaNa(t, a); len(ceka) != 0 {
		t.Errorf("na računalu s ključem čekaju promjene prava: %+v", ceka)
	}
	istaPovrsina(t, a, c)
}

// Mreža osnovana s ključem zatečenim uz bazu (nova baza, a ključ i mapa
// data ostali) postojala je i prije: punjenje se odbaci i ne šalje
func TestOsnivanjeSaZatecenimKljucemOdbacujePunjenje(t *testing.T) {
	s := rubniCvor(t, "cop-primjerovo")
	napuniIzImenika(t, s)
	kljuc, err := razmjena.NewNetwork("Hrvatske vode")
	if err != nil {
		t.Fatal(err)
	}
	if err := razmjena.SaveKey(s.networkKeyPath(), kljuc.Private()); err != nil {
		t.Fatal(err)
	}

	if err := s.CreateNetwork(ctxRub, "Hrvatske vode"); err != nil {
		t.Fatal(err)
	}
	if n := broj(t, s.db, `SELECT COUNT(*) FROM punjenje_za_knjigu`); n != 0 {
		t.Errorf("ostalo je %d oznaka punjenja", n)
	}
	if n := upisiPunjenjeNa(t, s); n != 0 {
		t.Errorf("upisano je %d verzija punjenja", n)
	}
}

// Kad se oznake ne daju odbaciti, primanje u mrežu ne prolazi i računalo
// ostaje izvan mreže, s oznakama
func TestPrimanjeBezOdbacivanjaPunjenjaNijeUpisano(t *testing.T) {
	k := rubnaMreza(t, "cop-primjerovo")
	s := rubniCvor(t, "cop-rubno")
	napuniIzImenika(t, s)
	if _, err := s.db.Exec(`CREATE TRIGGER bez_odbacivanja BEFORE DELETE ON punjenje_za_knjigu BEGIN SELECT RAISE(ABORT, 'ne'); END`); err != nil {
		t.Fatal(err)
	}
	m, err := k.izdajClanstvo(ctxRub, s.node.ID, s.javni())
	if err != nil {
		t.Fatal(err)
	}
	n := k.NetworkInfo()
	if err := s.joinNetwork(ctxRub, welcomePack{NetworkName: n.Name, NetworkKey: n.PublicKey, ForYou: &m}); err == nil {
		t.Fatal("primanje je prošlo iako se oznake ne daju odbaciti")
	}
	if s.NetworkInfo() != nil || broj(t, s.db, `SELECT COUNT(*) FROM network`) != 0 {
		t.Error("računalo je u mreži iako oznake nisu odbačene")
	}
	if n := broj(t, s.db, `SELECT COUNT(*) FROM punjenje_za_knjigu`); n != 18 {
		t.Errorf("ostalo je %d oznaka, očekivano 18", n)
	}
}

// upisiMrezu: druga mreža na istoj bazi i baza koja se ne da otvoriti
// javljaju grešku, a oznake ostaju
func TestUpisiMrezuGreske(t *testing.T) {
	s := rubniCvor(t, "cop-primjerovo")
	napuniIzImenika(t, s)
	kad := time.Now().UTC()
	if err := s.upisiMrezu(ctxRub, "Prva", "kljuc-prvi", kad, false); err != nil {
		t.Fatal(err)
	}
	if err := s.upisiMrezu(ctxRub, "Druga", "kljuc-drugi", kad, true); err == nil {
		t.Error("druga mreža upisana je na istu bazu")
	}
	if n := broj(t, s.db, `SELECT COUNT(*) FROM punjenje_za_knjigu`); n == 0 {
		t.Error("neuspjeli upis mreže odbacio je oznake")
	}
	otkazan, otkazi := context.WithCancel(ctxRub)
	otkazi()
	if err := s.upisiMrezu(otkazan, "Treća", "kljuc-treci", kad, false); err == nil {
		t.Error("upis mreže prošao je s otkazanim kontekstom")
	}
}
