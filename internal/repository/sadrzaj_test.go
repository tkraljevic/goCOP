package repository

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/sadrzaj"
)

// Baza otprije spremišta nosi PDF-ove u tablici i u knjizi verzija. Pri
// pokretanju se tablica skloni, bajtovi presele u spremište, a verzije u
// knjizi prepišu tako da nose samo otisak — pod istim version_id. Čitanje
// nakon toga vraća isti PDF.
func TestSeljenjeIzvornikaPrijavaUSpremiste(t *testing.T) {
	dir := t.TempDir()
	baza, err := db.OpenDB(filepath.Join(dir, "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	// stara tablica s bajtovima, kakvu je imala baza prije spremišta
	for _, q := range []string{
		`DROP TABLE prijave_izvornici`,
		`CREATE TABLE prijave_izvornici (prijava_id TEXT PRIMARY KEY, pdf BLOB NOT NULL, sazetak TEXT NOT NULL DEFAULT '', updated_at DATETIME NOT NULL)`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "stari-cvor")
	pdf := []byte("%PDF-1.4\nizvornik prijave iz starih dana")
	kad := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	stari := models.IzvornikLista{ListID: "p1", PDF: pdf, Sazetak: "abc", UpdatedAt: kad}
	tx, _ := baza.Begin()
	if _, err := tx.Exec(`INSERT INTO prijave_izvornici (prijava_id, pdf, sazetak, updated_at) VALUES (?, ?, ?, ?)`, "p1", pdf, "abc", kad); err != nil {
		t.Fatal(err)
	}
	vid, err := rec.Record(context.Background(), tx, EntityPrijaveIzvornici, "p1", stari)
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.Commit()

	// pokretanje: shema skloni staru tablicu, seljenje premjesti bajtove
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	spremiste, err := sadrzaj.Otvori(filepath.Join(dir, "sadrzaj.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer spremiste.Zatvori()
	SetSpremiste(spremiste)
	defer SetSpremiste(nil)
	n, bajtova, err := PreseliIzvornikePrijava(context.Background(), baza)
	if err != nil || n != 1 || bajtova != int64(len(pdf)) {
		t.Fatalf("seljenje: %d %d %v", n, bajtova, err)
	}
	// stara tablica je otišla, verzija nosi otisak a ne bajtove, isti id
	var ima int
	_ = baza.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'prijave_izvornici_stari'`).Scan(&ima)
	if ima != 0 {
		t.Error("stara tablica je ostala")
	}
	var payload string
	if err := baza.QueryRow(`SELECT payload FROM record_versions WHERE version_id = ?`, vid).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var iz models.IzvornikLista
	_ = json.Unmarshal([]byte(payload), &iz)
	if len(iz.PDF) != 0 || iz.Otisak != sadrzaj.Otisak(pdf) || iz.Bajtova != len(pdf) || iz.Sazetak != "abc" {
		t.Errorf("verzija nakon seljenja: %+v", iz)
	}
	if len(payload) > 400 {
		t.Errorf("verzija je još velika: %d B", len(payload))
	}
	// čitanje kroz repozitorij vraća bajtove iz spremišta
	repo := NewPrijavaRepository(baza, rec)
	got, err := repo.Izvornik(context.Background(), "p1")
	if err != nil || got == nil || string(got.PDF) != string(pdf) || got.Otisak != iz.Otisak {
		t.Fatalf("čitanje: %v %+v", err, got)
	}
	// drugo pokretanje ne radi ništa
	if n, _, err := PreseliIzvornikePrijava(context.Background(), baza); err != nil || n != 0 {
		t.Errorf("ponovno seljenje: %d %v", n, err)
	}
}

// Verzija primljena sa starijeg čvora nosi bajtove: spreme se u spremište i
// dalje se vode po otisku. Verzija s novijeg čvora nosi samo otisak: zapis se
// veže, a sadržaj ide na popis za dohvat.
func TestPrimljeniIzvornikSaIBezBajtova(t *testing.T) {
	dir := t.TempDir()
	baza, err := db.OpenDB(filepath.Join(dir, "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	spremiste, _ := sadrzaj.Otvori("")
	defer spremiste.Zatvori()
	SetSpremiste(spremiste)
	defer SetSpremiste(nil)
	ctx := context.Background()
	pdf := []byte("%PDF-1.4\nsa starog čvora")
	sBajtovima, _ := json.Marshal(models.IzvornikLista{ListID: "a", PDF: pdf, Sazetak: "x", UpdatedAt: time.Now()})
	bezBajtova, _ := json.Marshal(models.IzvornikLista{ListID: "b", Otisak: "ab12", Bajtova: 5000, Vrsta: "application/pdf", UpdatedAt: time.Now()})
	for _, v := range []ledger.Version{
		{VersionID: "v1", Entity: EntityPrijaveIzvornici, EntityID: "a", Payload: sBajtovima},
		{VersionID: "v2", Entity: EntityPrijaveIzvornici, EntityID: "b", Payload: bezBajtova},
	} {
		tx, _ := baza.BeginTx(ctx, nil)
		if err := applyOne(ctx, tx, v); err != nil {
			t.Fatalf("%s: %v", v.EntityID, err)
		}
		_ = tx.Commit()
	}
	repo := NewPrijavaRepository(baza, ledger.New(baza, "ovaj"))
	a, _ := repo.Izvornik(ctx, "a")
	if a == nil || string(a.PDF) != string(pdf) || a.Otisak != sadrzaj.Otisak(pdf) {
		t.Errorf("sa bajtovima: %+v", a)
	}
	b, _ := repo.Izvornik(ctx, "b")
	if b == nil || len(b.PDF) != 0 || b.Otisak != "ab12" {
		t.Errorf("bez bajtova: %+v", b)
	}
	z, _ := spremiste.Zeljeni(ctx, 10)
	if len(z) != 1 || z[0].Otisak != "ab12" || z[0].Bajtova != 5000 {
		t.Errorf("za dohvat: %+v", z)
	}
}
