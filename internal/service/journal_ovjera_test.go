package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// cvorZaOvjeru otvara bazu jednog čvora s dnevnikom COP-a spremnim za ovjeru
type cvorZaOvjeru struct {
	db   *sql.DB
	rec  *ledger.Recorder
	repo *repository.JournalRepository
	s    *JournalService
}

func noviCvorZaOvjeru(t *testing.T, ime string) *cvorZaOvjeru {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), ime+".db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, ime)
	repo := repository.NewJournalRepository(baza, rec)
	return &cvorZaOvjeru{db: baza, rec: rec, repo: repo, s: NewJournalService(repo, nil, nil)}
}

// Ovjera dnevnika COP-a je jedna cjelina: stanje ovjere, potpisnik, potpisani
// PDF sa sažetkom i verzije u knjizi. Kad bilo koji dio padne, ništa se ne
// sprema; dnevnik nikad ne ostaje ovjeren bez izvornika ni izvornik bez
// ovjere. Dvaput se ne ovjerava, ovjeren putuje na drugi čvor cijeli, a
// izmijenjen ili obrisan izvornik se otkriva.
func TestOvjeraDnevnikaCOPaJeAtomska(t *testing.T) {
	ctx := context.Background()
	a := noviCvorZaOvjeru(t, "cop-osijek")
	b := "B"
	voditelj := &models.User{ID: uuid.New(), FullName: "Voditelj COP-a", Duties: []models.Duty{{Role: models.RoleCopLeader, SectorID: &b, IsActive: true}}}
	uprava := &models.UserPermissions{AdminSectors: map[string]bool{"B": true}, AllowedSectors: map[string]bool{"B": true}, User: *voditelj}
	pocetak := time.Date(2026, 9, 10, 0, 0, 0, 0, models.Zagreb)
	kraj := pocetak.AddDate(0, 0, 3)
	j := models.Journal{CentarSektor: "B", StartedAt: &pocetak, EndedAt: &kraj}
	if err := a.s.SpremiCOPDnevnik(ctx, voditelj, uprava, &j); err != nil {
		t.Fatal(err)
	}
	pdf := []byte("%PDF-1.4\npotpisani izvornik dnevnika")
	sazetak := fmt.Sprintf("%x", sha256.Sum256(pdf))

	neovjeren := func(sto string) {
		t.Helper()
		p, _ := a.s.GetJournal(ctx, j.ID)
		if p == nil || p.Ovjeren() || p.Zakljucio != "" {
			t.Fatalf("%s: dnevnik ostao ovjeren: %+v", sto, p)
		}
		if iz, _ := a.repo.IzvornikDnevnika(ctx, j.ID); iz != nil {
			t.Fatalf("%s: izvornik upisan bez ovjere", sto)
		}
		if v, _ := a.rec.Latest(ctx, repository.EntityJournalIzvornici, j.ID); v != nil {
			t.Fatalf("%s: verzija izvornika u knjizi bez ovjere", sto)
		}
		if v, _ := a.rec.Latest(ctx, repository.EntityJournals, j.ID); v == nil || strings.Contains(string(v.Payload), `"zakljuceno_at"`) {
			t.Fatalf("%s: knjiga nosi ovjeru koja nije spremljena", sto)
		}
	}

	// 1) kvar pri upisu PDF-a: baza odbije umetanje izvornika, pa se ni
	// stanje ovjere upisano ranije u istoj transakciji ne zadrži
	if _, err := a.db.Exec(`CREATE TRIGGER kvar_diska BEFORE INSERT ON journal_izvornici BEGIN SELECT RAISE(ABORT, 'disk je pun'); END;`); err != nil {
		t.Fatal(err)
	}
	priprema := j
	if err := a.s.PripremiOvjeruCOP(voditelj, uprava, &priprema, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := a.s.SpremiOvjeruCOP(ctx, &priprema, pdf); err == nil || !strings.Contains(err.Error(), "disk je pun") {
		t.Fatalf("kvar pri upisu PDF-a nije javljen: %v", err)
	}
	neovjeren("kvar pri upisu PDF-a")
	if _, err := a.db.Exec(`DROP TRIGGER kvar_diska`); err != nil {
		t.Fatal(err)
	}

	// 2) prekid između potpisa i spremanja: zahtjev je otkazan prije upisa
	otkazan, otkazi := context.WithCancel(ctx)
	otkazi()
	priprema = j
	_ = a.s.PripremiOvjeruCOP(voditelj, uprava, &priprema, time.Now())
	if err := a.s.SpremiOvjeruCOP(otkazan, &priprema, pdf); err == nil {
		t.Fatal("spremanje s prekinutim zahtjevom prošlo")
	}
	neovjeren("prekid prije spremanja")

	// 3) neispravan izvornik se ne prima; dnevnik nepripremljen također ne
	if err := a.s.SpremiOvjeruCOP(ctx, &priprema, []byte("nije pdf")); err == nil {
		t.Fatal("prihvaćen izvornik koji nije PDF")
	}
	if err := a.s.SpremiOvjeruCOP(ctx, &j, pdf); err == nil {
		t.Fatal("ovjera bez pripreme (bez potpisnika) prošla")
	}
	neovjeren("neispravan ulaz")

	// 4) uspjeh: sve u jednom; dnevnik, izvornik i obje verzije u knjizi
	priprema = j
	kad := time.Date(2026, 9, 14, 8, 30, 0, 0, models.Zagreb)
	if err := a.s.PripremiOvjeruCOP(voditelj, uprava, &priprema, kad); err != nil {
		t.Fatal(err)
	}
	if err := a.s.SpremiOvjeruCOP(ctx, &priprema, pdf); err != nil {
		t.Fatal(err)
	}
	ovjeren, _ := a.s.GetJournal(ctx, j.ID)
	if ovjeren == nil || !ovjeren.Ovjeren() || ovjeren.Zakljucio != "Voditelj COP-a" || ovjeren.ZakljucioID != voditelj.ID.String() || !ovjeren.ZakljucenoAt.Equal(kad) {
		t.Fatalf("ovjeren dnevnik: %+v", ovjeren)
	}
	if iz, _ := a.repo.IzvornikDnevnika(ctx, j.ID); iz == nil || iz.Sazetak != sazetak || string(iz.PDF) != string(pdf) {
		t.Fatalf("izvornik: %+v", iz)
	}
	if st := a.s.ProvjeriIzvornikCOP(ctx, ovjeren); !st.Ima || !st.Ispravan || st.Sazetak != sazetak {
		t.Fatalf("provjera izvornika: %+v", st)
	}
	if v, _ := a.rec.Latest(ctx, repository.EntityJournals, j.ID); v == nil || !strings.Contains(string(v.Payload), `"zakljucio":"Voditelj COP-a"`) {
		t.Fatalf("knjiga bez ovjere dnevnika: %+v", v)
	}
	if v, _ := a.rec.Latest(ctx, repository.EntityJournalIzvornici, j.ID); v == nil || !strings.Contains(string(v.Payload), sazetak) {
		t.Fatalf("knjiga bez izvornika: %+v", v)
	}

	// 5) dvostruki pokušaj: preko servisa i izravno s ustajalom kopijom
	if err := a.s.PripremiOvjeruCOP(voditelj, uprava, ovjeren, time.Now()); err == nil || !strings.Contains(err.Error(), "već ovjeren") {
		t.Fatalf("druga ovjera: %v", err)
	}
	ustajala := priprema
	drugiKad := time.Now()
	ustajala.ZakljucenoAt = &drugiKad
	if err := a.s.SpremiOvjeruCOP(ctx, &ustajala, []byte("%PDF-1.4\ndrugi pokušaj")); err == nil || !strings.Contains(err.Error(), "već ovjeren") {
		t.Fatalf("istodobna druga ovjera: %v", err)
	}
	if iz, _ := a.repo.IzvornikDnevnika(ctx, j.ID); iz == nil || iz.Sazetak != sazetak {
		t.Fatal("drugi pokušaj promijenio izvornik")
	}
	if p, _ := a.s.GetJournal(ctx, j.ID); !p.ZakljucenoAt.Equal(kad) {
		t.Fatal("drugi pokušaj promijenio vrijeme ovjere")
	}

	// 6) sinkronizacija: verzije iz knjige ulaze u knjigu drugog čvora i
	// prepisuju se na površinu, istim putem kao prava razmjena
	d := noviCvorZaOvjeru(t, "cop-zagreb")
	verzije, err := a.rec.Since(ctx, "", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.rec.Apply(ctx, verzije); err != nil {
		t.Fatal(err)
	}
	if err := repository.ApplyVersions(ctx, d.db, d.rec, verzije); err != nil {
		t.Fatal(err)
	}
	drugi, _ := d.s.GetJournal(ctx, j.ID)
	if drugi == nil || !drugi.Ovjeren() || drugi.Zakljucio != "Voditelj COP-a" || !drugi.ZakljucenoAt.Equal(kad) {
		t.Fatalf("dnevnik na drugom čvoru: %+v", drugi)
	}
	if st := d.s.ProvjeriIzvornikCOP(ctx, drugi); !st.Ispravan || st.Sazetak != sazetak {
		t.Fatalf("izvornik na drugom čvoru: %+v", st)
	}
	if err := d.s.SpremiCOPDnevnik(ctx, voditelj, uprava, drugi); err == nil {
		t.Fatal("ovjeren dnevnik se na drugom čvoru dao mijenjati")
	}

	// 7) izmijenjen izvornik: sažetak iz knjige ne odgovara; obrisan: nedostaje
	if _, err := d.db.Exec(`UPDATE journal_izvornici SET pdf = ? WHERE journal_id = ?`, []byte("%PDF-1.4\npodmetnut"), j.ID); err != nil {
		t.Fatal(err)
	}
	if st := d.s.ProvjeriIzvornikCOP(ctx, drugi); !st.Ima || st.Ispravan || !strings.Contains(st.Greska, "mijenjan") {
		t.Fatalf("izmijenjen izvornik: %+v", st)
	}
	if _, err := d.db.Exec(`DELETE FROM journal_izvornici WHERE journal_id = ?`, j.ID); err != nil {
		t.Fatal(err)
	}
	if st := d.s.ProvjeriIzvornikCOP(ctx, drugi); st.Ima || st.Ispravan || !strings.Contains(st.Greska, "nedostaje") {
		t.Fatalf("obrisan izvornik: %+v", st)
	}
	// obnova iz knjige verzija: ponovna primjena vraća izvornik
	if err := repository.ApplyVersions(ctx, d.db, d.rec, verzije); err != nil {
		t.Fatal(err)
	}
	if st := d.s.ProvjeriIzvornikCOP(ctx, drugi); !st.Ispravan {
		t.Fatalf("izvornik nakon obnove iz knjige: %+v", st)
	}
}
