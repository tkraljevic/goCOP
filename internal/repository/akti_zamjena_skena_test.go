package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/sadrzaj"
)

// Akt sa zamjenom skena stigne s drugog čvora: zamijenjeni sken ostaje
// vezan uz akt i čvor ga traži, iako ga još nema. Zamjena bez zapisa ili
// bez spremišta ne upisuje se.
func TestZamijenjeniSkenPutujeSAktom(t *testing.T) {
	baza, repo := bazaAkata(t)
	ctx := context.Background()
	a := models.Akt{ID: "akt-z", Sektor: "B", AreaID: 34, Godina: 2026, Broj: 3, Radnja: models.AktUspostava, Stupanj: models.PhasePrep,
		Status: models.AktOvjeren, Vrijedi: time.Date(2026, 11, 23, 6, 0, 0, 0, models.Zagreb),
		Rucno: &models.RucniPotpis{Potpisnik: "Iva Ivić", Sazetak: "aa11"},
		ZamjeneSkena: []models.ZamjenaSkena{{StariSazetak: "aa11", StariBajtova: 12, NoviSazetak: "bb22", Zamijenio: "Iva Ivić", Razlog: "nečitko"}}}

	if err := repo.ZamijeniSken(ctx, &models.Akt{ID: "x"}, &Izvornik{AktID: "x"}); err == nil {
		t.Error("zamjena bez zapisa zamjene")
	}
	prije := Spremiste()
	SetSpremiste(nil)
	if err := repo.ZamijeniSken(ctx, &a, &Izvornik{AktID: a.ID}); err != errBezSpremista {
		t.Errorf("bez spremišta: %v", err)
	}
	SetSpremiste(prije)

	sSpremistemAkata(t)
	p, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	v := []ledger.Version{{VersionID: vid(1), Entity: EntityAkti, EntityID: a.ID, NodeID: "cop-vukovar", Payload: p, CreatedAt: time.Now(), SchemaVersion: 1}}
	if _, err := repo.rec.Apply(ctx, v); err != nil {
		t.Fatal(err)
	}
	if err := ApplyVersions(ctx, baza, repo.rec, v); err != nil {
		t.Fatal(err)
	}
	if x, err := repo.GetAkt(ctx, a.ID); err != nil || x == nil || len(x.ZamjeneSkena) != 1 || x.SazetakSkena() != "bb22" {
		t.Fatalf("akt sa zamjenom na površini: %+v %v", x, err)
	}
	if !Spremiste().ImaVezu(ctx, "aa11", sadrzaj.Veza{Entitet: EntityAkti, EntitetID: a.ID, Uloga: UlogaZamijenjenogSkena}) {
		t.Error("zamijenjeni sken nije vezan uz akt")
	}
	zelje, err := Spremiste().Zeljeni(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	trazi := false
	for _, z := range zelje {
		trazi = trazi || z.Otisak == "aa11"
	}
	if !trazi {
		t.Error("čvor ne traži zamijenjeni sken")
	}
	if repo.ZamijenjeniSken(ctx, "aa11") != nil {
		t.Error("sken kojeg čvor još nema")
	}

	// zamjena na ovom čvoru: novi izvornik i akt u istom koraku, stari sken
	// ostaje u spremištu
	stari := []byte("%PDF-1.4 stari")
	iz := &Izvornik{AktID: a.ID, PDF: stari, Sazetak: sadrzaj.Otisak(stari)}
	nuzno(t, repo.SaveIzvornik(ctx, iz))
	novi := []byte("%PDF-1.4 novi")
	b := a
	b.ZamjeneSkena = []models.ZamjenaSkena{{StariSazetak: sadrzaj.Otisak(stari), NoviSazetak: sadrzaj.Otisak(novi), Razlog: "nečitko"}}
	nuzno(t, repo.ZamijeniSken(ctx, &b, &Izvornik{AktID: a.ID, PDF: novi, Sazetak: sadrzaj.Otisak(novi)}))
	if x, err := repo.GetIzvornik(ctx, a.ID); err != nil || string(x.PDF) != string(novi) {
		t.Errorf("izvornik nakon zamjene: %v", err)
	}
	if string(repo.ZamijenjeniSken(ctx, sadrzaj.Otisak(stari))) != string(stari) {
		t.Error("stari sken nije ostao u spremištu")
	}

	// baza odbije izvornik, pa akt; ni jedno ni drugo ne ostaje napola
	for _, okidac := range []string{
		`CREATE TRIGGER zabrana BEFORE UPDATE ON akti_izvornici BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`,
		`CREATE TRIGGER zabrana BEFORE UPDATE ON akti BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`,
	} {
		_, err := baza.Exec(okidac)
		nuzno(t, err)
		treci := []byte("%PDF-1.4 treci " + okidac[:30])
		c := b
		c.ZamjeneSkena = append(b.ZamjeneSkena[:1:1], models.ZamjenaSkena{StariSazetak: sadrzaj.Otisak(novi), NoviSazetak: sadrzaj.Otisak(treci)})
		if err := repo.ZamijeniSken(ctx, &c, &Izvornik{AktID: a.ID, PDF: treci, Sazetak: sadrzaj.Otisak(treci)}); err == nil {
			t.Errorf("%s: zamjena prošla", okidac)
		}
		_, err = baza.Exec(`DROP TRIGGER zabrana`)
		nuzno(t, err)
	}
	if x, err := repo.GetIzvornik(ctx, a.ID); err != nil || string(x.PDF) != string(novi) {
		t.Errorf("izvornik nakon odbijene zamjene: %v", err)
	}

	// spremište koje ne radi: zamjena se ne upisuje, a primljeni akt čeka
	zatvoreno, err := sadrzaj.Otvori("")
	nuzno(t, err)
	nuzno(t, zatvoreno.Zatvori())
	SetSpremiste(zatvoreno)
	if err := repo.ZamijeniSken(ctx, &b, &Izvornik{AktID: a.ID, PDF: novi}); err == nil {
		t.Error("zamjena sa spremištem koje ne radi")
	}
	if err := repo.SaveIzvornik(ctx, &Izvornik{AktID: a.ID, PDF: novi}); err == nil {
		t.Error("izvornik sa spremištem koje ne radi")
	}
	v[0].VersionID = vid(2)
	p2, err := json.Marshal(b)
	nuzno(t, err)
	v[0].Payload = p2
	if _, err := repo.rec.Apply(ctx, v); err != nil {
		t.Fatal(err)
	}
	tx, err := baza.Begin()
	nuzno(t, err)
	defer tx.Rollback()
	if err := primijeniAkt(ctx, tx, v[0]); err == nil {
		t.Error("primljena zamjena sa spremištem koje ne radi")
	}
}
