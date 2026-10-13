package repository

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/sadrzaj"
)

// primijeniUTx primjenjuje jednu verziju na površinu čvora u vlastitoj
// transakciji koja se potvrdi samo kad primjena prođe
func primijeniUTx(t *testing.T, c *cvor, v ledger.Version) error {
	t.Helper()
	tx, err := c.db.BeginTx(ctxRaz, nil)
	nuzno(t, err)
	defer tx.Rollback()
	if err := applyOne(ctxRaz, tx, v); err != nil {
		return err
	}
	nuzno(t, tx.Commit())
	return nil
}

// redakaNaPovrsini broji retke tablice na površini čvora
func redakaNaPovrsini(t *testing.T, c *cvor, tablica string) int {
	t.Helper()
	return brojRedaka(t, c.db, `SELECT COUNT(*) FROM `+tablica)
}

// zabraniUpis postavlja okidač koji odbija upis (INSERT ili DELETE) u tablicu
func zabraniUpis(t *testing.T, c *cvor, radnja, tablica string) {
	t.Helper()
	_, err := c.db.Exec(`CREATE TRIGGER zabrana_` + strings.ToLower(radnja) + `_` + tablica + ` BEFORE ` + radnja + ` ON ` + tablica + `
		BEGIN SELECT RAISE(ABORT, 'površina ne prima'); END`)
	nuzno(t, err)
}

// Primljena verzija čiji se sadržaj ne da pročitati kao zapis entiteta ne
// ide na površinu: primjena vraća grešku čitanja, a ne prazan zapis.
func TestPrimjenaOdbijaNecitljivSadrzaj(t *testing.T) {
	c := noviCvor(t, "b")
	for _, entitet := range []string{
		EntityStations, EntitySections, EntityWatercourses, EntityKisomjeri, EntitySlivovi, EntityMaintainedWaters,
		EntityWorkItems, EntityJournals, EntityDezurstva, EntityDnevnaIzvjesca, EntitySektorskaIzvjesca,
		EntityBlagdani, EntityKoeficijenti, EntityObracunPostavke,
		EntityMtsVrste, EntityMtsSkladista, EntityMtsPromet, EntityMtsPopisi, EntityMtsPotrebe,
		EntityAkti, EntityZadaci, EntityZigovi, EntityPrijave, EntityPrijaveIzvornici,
		EntityPotpisniKljucevi, EntityPotpisniIzdavatelji, EntityJournalIzvornici, EntityPotpisi, EntityPostavke,
		EntitySlanja, EntityIzvornici, EntitySluzbe, EntitySprance, EntityPrimatelji, EntityRjesenjaOznaka,
		EntityRoleModules, EntityUserModules, EntityReadings, EntityStructures, EntityUsers, EntityDuties,
		EntitySectors, EntityOrgTerms, EntityAreas, EntityContractors, EntityContractorAssignments,
		EntityCounties, EntityMunicipalities, EntitySettlements,
		"memberships", "ovlasti", "opozivi", "peers",
	} {
		err := primijeniUTx(t, c, ledger.Version{Entity: entitet, EntityID: "zapis-1", Payload: []byte(`[1, 2]`)})
		var citanje *json.UnmarshalTypeError
		if !errors.As(err, &citanje) {
			t.Errorf("%s: %v, očekivana greška čitanja sadržaja", entitet, err)
		}
	}
}

// Kad površina ne primi zapis, primjena vraća grešku baze i ne mijenja
// ništa: ni tablicu, ni teme i nazive koje primljena postavka mijenja.
func TestPrimjenaGreskaUpisaNaPovrsinu(t *testing.T) {
	prijeNaziva, prijeTeme := models.Terms(), models.TemaPrograma()
	t.Cleanup(func() { models.SetTerms(prijeNaziva); models.SetTema(prijeTeme) })

	c := noviCvor(t, "b")
	for _, tablica := range []string{"prijave", "postavke", "readings", "org_terms"} {
		zabraniUpis(t, c, "INSERT", tablica)
	}
	kad := time.Date(2026, 3, 10, 6, 0, 0, 0, time.UTC)
	cm := 152
	novaTema := models.Tema{Svijetla: models.BojeTeme{Glavna: "#112233", Naglasak: "#445566", Gumb: "#778899"}}
	tema, err := json.Marshal(novaTema)
	nuzno(t, err)
	for _, slucaj := range []struct {
		entitet, tablica string
		zapis            any
	}{
		{EntityPrijave, "prijave", models.PrijavaSTerena{ID: "prijava-1", Naslov: "Procjeđivanje kod Probnog Sela"}},
		{EntityPostavke, "postavke", Postavka{ID: PostavkaTema, Vrijednost: string(tema)}},
		{EntityReadings, "readings", models.Reading{ID: uuid.New(), StationID: osnovaLetva.String(), MeasuredAt: kad, LevelCm: &cm}},
		{EntityOrgTerms, "org_terms", models.OrgTerms{ID: models.TermsID, OrgName: "Probne vode"}},
	} {
		v := verzijaZa(t, slucaj.entitet, "zapis-1", vid(1), slucaj.zapis)
		if err := primijeniUTx(t, c, v); err == nil || !strings.Contains(err.Error(), "površina ne prima") {
			t.Errorf("%s: %v, očekivana greška upisa", slucaj.entitet, err)
		}
		if n := redakaNaPovrsini(t, c, slucaj.tablica); n != 0 {
			t.Errorf("%s: na površini %d redaka", slucaj.entitet, n)
		}
	}
	if !reflect.DeepEqual(models.Terms(), prijeNaziva) {
		t.Errorf("nazivi su promijenjeni iako nisu upisani: %+v", models.Terms())
	}
	if models.TemaPrograma() != prijeTeme {
		t.Errorf("tema je promijenjena iako nije upisana: %+v", models.TemaPrograma())
	}
}

// Kad spomenik naziva ili teme ne makne zapis s površine, vrijede i dalje
// dosadašnji nazivi i tema, a primjena vraća grešku.
func TestSpomenikNazivaITemeKojiSeNeDaUpisati(t *testing.T) {
	prijeNaziva, prijeTeme := models.Terms(), models.TemaPrograma()
	t.Cleanup(func() { models.SetTerms(prijeNaziva); models.SetTema(prijeTeme) })

	c := noviCvor(t, "b")
	nazivi := models.OrgTerms{ID: models.TermsID, OrgName: "Probne vode"}
	novaTema := models.Tema{Svijetla: models.BojeTeme{Glavna: "#112233", Naglasak: "#445566", Gumb: "#778899"}}
	tema, err := json.Marshal(novaTema)
	nuzno(t, err)
	nuzno(t, primijeniUTx(t, c, verzijaZa(t, EntityOrgTerms, models.TermsID, vid(1), nazivi)))
	nuzno(t, primijeniUTx(t, c, verzijaZa(t, EntityPostavke, PostavkaTema, vid(2), Postavka{ID: PostavkaTema, Vrijednost: string(tema)})))
	if models.Terms().OrgName != "Probne vode" || models.TemaPrograma() != novaTema {
		t.Fatalf("primljeni nazivi i tema: %q %+v", models.Terms().OrgName, models.TemaPrograma())
	}

	zabraniUpis(t, c, "DELETE", "org_terms")
	zabraniUpis(t, c, "DELETE", "postavke")
	for _, entitet := range []string{EntityOrgTerms, EntityPostavke} {
		id := models.TermsID
		if entitet == EntityPostavke {
			id = PostavkaTema
		}
		v := ledger.Version{Entity: entitet, EntityID: id, Archived: true, Payload: []byte(`{}`)}
		if err := primijeniUTx(t, c, v); err == nil || !strings.Contains(err.Error(), "površina ne prima") {
			t.Errorf("spomenik %s: %v, očekivana greška brisanja", entitet, err)
		}
	}
	if redakaNaPovrsini(t, c, "org_terms") != 1 || redakaNaPovrsini(t, c, "postavke") != 1 {
		t.Error("spomenik koji nije prošao maknuo je zapis s površine")
	}
	if models.Terms().OrgName != "Probne vode" || models.TemaPrograma() != novaTema {
		t.Errorf("nazivi i tema nakon neuspjelog spomenika: %q %+v", models.Terms().OrgName, models.TemaPrograma())
	}
}

// Izvornik s bajtovima koji ne odgovaraju otisku ne ulazi ni u spremište
// ni na površinu: primjena vraća grešku otiska za izvornik akta, dnevnika
// i prijave.
func TestPrimjenaIzvornikaSKrivimOtiskom(t *testing.T) {
	prije := Spremiste()
	t.Cleanup(func() { SetSpremiste(prije) })
	sp, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "sadrzaj.db"))
	nuzno(t, err)
	t.Cleanup(func() { sp.Zatvori() })
	SetSpremiste(sp)

	c := noviCvor(t, "b")
	krivi := sadrzaj.Otisak([]byte("neki drugi sadržaj"))
	for _, slucaj := range []struct {
		entitet, tablica string
		zapis            any
	}{
		{EntityIzvornici, "akti_izvornici", Izvornik{AktID: "akt-1", PDF: pdfProbe, Otisak: krivi}},
		{EntityJournalIzvornici, "journal_izvornici", models.IzvornikDnevnika{JournalID: osnovaDnevnik, PDF: pdfProbe, Otisak: krivi}},
		{EntityPrijaveIzvornici, "prijave_izvornici", models.IzvornikLista{ListID: "prijava-1", PDF: pdfProbe, Otisak: krivi}},
	} {
		err := primijeniUTx(t, c, verzijaZa(t, slucaj.entitet, "zapis-1", vid(1), slucaj.zapis))
		if !errors.Is(err, sadrzaj.ErrOtisak) {
			t.Errorf("%s: %v, očekivana greška otiska", slucaj.entitet, err)
		}
		if n := redakaNaPovrsini(t, c, slucaj.tablica); n != 0 {
			t.Errorf("%s: na površini %d redaka", slucaj.entitet, n)
		}
	}
	if b, _, err := sp.Citaj(ctxRaz, sadrzaj.Otisak(pdfProbe)); err == nil || len(b) > 0 {
		t.Errorf("spremište je primilo sadržaj s krivim otiskom: %d bajtova", len(b))
	}
}

// Očitanje izvan razdoblja koje čvor drži primjena preskače bez greške;
// sektor iz verzije otprije razina dobiva razinu sektora; nepoznat entitet
// ne mijenja ništa.
func TestPrimjenaPreskaceIDopunjuje(t *testing.T) {
	prijeOgrade := readingPolicy.Load()
	t.Cleanup(func() { readingPolicy.Store(prijeOgrade) })
	SetReadingHistoryPolicy(ReadingHistoryPolicy{Months: 12})

	c := noviCvor(t, "b")
	cm := 300
	staro := models.Reading{ID: uuid.New(), StationID: osnovaLetva.String(), MeasuredAt: time.Now().AddDate(-30, 0, 0), LevelCm: &cm}
	nuzno(t, primijeniUTx(t, c, verzijaZa(t, EntityReadings, staro.ID.String(), vid(1), staro)))
	if n := redakaNaPovrsini(t, c, "readings"); n != 0 {
		t.Errorf("staro očitanje nepraćene letve je na površini (%d)", n)
	}

	nuzno(t, primijeniUTx(t, c, verzijaZa(t, EntitySectors, "S", vid(2), models.Sector{ID: "S", Name: "Sektor S"})))
	nuzno(t, primijeniUTx(t, c, verzijaZa(t, EntitySectors, "T", vid(3), models.Sector{ID: "T", Name: "Sektor T", Level: 3})))
	for id, ocek := range map[string]int{"S": 2, "T": 3} {
		var razina int
		nuzno(t, c.db.QueryRow(`SELECT level FROM sectors WHERE id = ?`, id).Scan(&razina))
		if razina != ocek {
			t.Errorf("razina sektora %s: %d, očekivana %d", id, razina, ocek)
		}
	}

	prijeSektora := redakaNaPovrsini(t, c, "sectors")
	if err := primijeniUTx(t, c, ledger.Version{Entity: "nepoznato_iz_buducnosti", EntityID: "x", Payload: []byte(`[1]`)}); err != nil {
		t.Errorf("nepoznat entitet: %v", err)
	}
	if n := redakaNaPovrsini(t, c, "sectors"); n != prijeSektora {
		t.Errorf("nepoznat entitet promijenio je površinu: %d sektora, prije %d", n, prijeSektora)
	}
}

// Ograda povijesti zadržava očitanje kojem se ne zna vrijeme mjerenja:
// bez vremena se ne može reći je li staro.
func TestOgradaZadrzavaOcitanjeBezVremena(t *testing.T) {
	prijeOgrade := readingPolicy.Load()
	t.Cleanup(func() { readingPolicy.Store(prijeOgrade) })
	SetReadingHistoryPolicy(ReadingHistoryPolicy{Months: 12})

	for naziv, sadrzaj := range map[string]string{
		"nečitljiv sadržaj": `[1]`,
		"bez vremena":       `{"station_id": "postaja-1"}`,
	} {
		if !KeepReadingVersion([]byte(sadrzaj)) {
			t.Errorf("%s: očitanje je odbačeno", naziv)
		}
	}
	staro := `{"station_id": "postaja-1", "measured_at": "1996-03-10T06:00:00Z"}`
	if KeepReadingVersion([]byte(staro)) {
		t.Error("staro očitanje nepraćene letve je zadržano")
	}
}

// Kad primjena paketa ne može ni započeti, paket ostaje među nedovršenima
// za sljedeću razmjenu, a površina se ne mijenja.
func TestPrimjenaPaketaKojaNeMozeZapoceti(t *testing.T) {
	sNedovrsenima(t)
	c := noviCvor(t, "b")
	v := verzijaZa(t, EntitySectors, "S", vid(1), models.Sector{ID: "S", Name: "Sektor S", Level: 2})
	if _, err := c.rec.Apply(ctxRaz, []ledger.Version{v}); err != nil {
		t.Fatal(err)
	}
	prije := redakaNaPovrsini(t, c, "sectors")
	if err := ApplyVersions(otkazan(), c.db, c.rec, []ledger.Version{v}); err == nil {
		t.Fatal("primjena s otkazanim zahtjevom je prošla")
	}
	if !nedovrsen(EntitySectors + "|S") {
		t.Error("paket koji nije primijenjen nije među nedovršenima")
	}
	if n := redakaNaPovrsini(t, c, "sectors"); n != prije {
		t.Errorf("površina se promijenila: %d sektora, prije %d", n, prije)
	}
}

// Verzija koje nema u knjizi ne ide na površinu: primjena javlja da je
// nema, a paket čeka među nedovršenima.
func TestPrimjenaVerzijeKojeNemaUKnjizi(t *testing.T) {
	sNedovrsenima(t)
	c := noviCvor(t, "b")
	v := verzijaZa(t, EntitySectors, "S", vid(1), models.Sector{ID: "S", Name: "Sektor S", Level: 2})
	if err := ApplyVersions(ctxRaz, c.db, c.rec, []ledger.Version{v}); !errors.Is(err, ledger.ErrNoVersion) {
		t.Fatalf("primjena verzije koje nema u knjizi: %v", err)
	}
	if !nedovrsen(EntitySectors + "|S") {
		t.Error("verzija koje nema u knjizi nije među nedovršenima")
	}
	var n int
	nuzno(t, c.db.QueryRow(`SELECT COUNT(*) FROM sectors WHERE id = 'S'`).Scan(&n))
	if n != 0 {
		t.Error("verzija koje nema u knjizi je na površini")
	}
}

// Obnova površine iz knjige koja se ne da pročitati javlja grešku i ne
// tvrdi da je išta obnovila
func TestObnovaPovrsineIzNecitljiveKnjige(t *testing.T) {
	c := noviCvor(t, "b")
	c.db.Close()
	if n, err := ReplaySurface(ctxRaz, c.db, c.rec); err == nil || n != 0 {
		t.Errorf("obnova iz zatvorene baze: %d %v", n, err)
	}
}
