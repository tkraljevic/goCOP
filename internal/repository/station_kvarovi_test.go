package repository

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// izvrsiSve izvršava upite redom i ruši test na prvoj grešci
func izvrsiSve(t *testing.T, o *okolinaPostaja, upiti ...string) {
	t.Helper()
	for _, q := range upiti {
		if _, err := o.baza.Exec(q); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
}

// vezeBezDionice zamjenjuje tablicu veza tablicom u kojoj letva Primjerovo
// ima vezu bez šifre dionice
func vezeBezDionice(o *okolinaPostaja) []string {
	return []string{
		`DROP TABLE section_stations`,
		`CREATE TABLE section_stations (id TEXT, station_id TEXT, section_code TEXT, created_at DATETIME)`,
		`INSERT INTO section_stations VALUES ('veza-x', '` + o.primjerovo.ID.String() + `', NULL, '2026-01-01 00:00:00')`,
	}
}

// Brisanje postaje koje ne uspije ni u jednom koraku ne briše ništa: letva
// ostaje u registru sa svojim vezama, a u knjizi nema spomenika.
func TestBrisanjePostajeNeuspjeloNistaNeBrise(t *testing.T) {
	for _, slucaj := range []struct {
		naziv   string
		upiti   func(o *okolinaPostaja) []string
		otkazan bool
		veze    int // veza Primjerova na površini nakon neuspjeha
	}{
		{naziv: "otkazan zahtjev", otkazan: true, veze: 3},
		{naziv: "postaja se ne da pročitati", veze: 3, upiti: func(o *okolinaPostaja) []string {
			return []string{`UPDATE stations SET created_at = 'nije vrijeme' WHERE code = 'primjerovo'`}
		}},
		{naziv: "veze se ne daju pročitati", veze: -1, upiti: func(o *okolinaPostaja) []string {
			return []string{`ALTER TABLE section_stations RENAME TO section_stations_negdje`}
		}},
		{naziv: "veza bez dionice", veze: 1, upiti: vezeBezDionice},
		{naziv: "veze se ne daju ukloniti", veze: 3, upiti: func(o *okolinaPostaja) []string {
			return []string{`CREATE TRIGGER zabrana BEFORE DELETE ON section_stations BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`}
		}},
		{naziv: "postaja se ne da obrisati", veze: 3, upiti: func(o *okolinaPostaja) []string {
			return []string{`CREATE TRIGGER zabrana BEFORE DELETE ON stations BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`}
		}},
		{naziv: "knjiga ne prima spomenik veze", veze: 3, upiti: func(o *okolinaPostaja) []string {
			return []string{zabranaKnjigeZa(EntitySectionStations)}
		}},
		{naziv: "knjiga ne prima spomenik postaje", veze: 3, upiti: func(o *okolinaPostaja) []string {
			return []string{zabranaKnjigeZa(EntityStations)}
		}},
	} {
		t.Run(slucaj.naziv, func(t *testing.T) {
			o := novaOkolinaPostaja(t)
			if slucaj.upiti != nil {
				izvrsiSve(t, o, slucaj.upiti(o)...)
			}
			ctx := context.Background()
			if slucaj.otkazan {
				ctx = otkazan()
			}
			if err := o.repo.DeleteStation(ctx, o.primjerovo.ID); err == nil {
				t.Fatal("brisanje postaje je prošlo")
			}
			id := o.primjerovo.ID.String()
			var n int
			nuzno(t, o.baza.QueryRow(`SELECT COUNT(*) FROM stations WHERE id = ?`, id).Scan(&n))
			if n != 1 {
				t.Errorf("postaja nakon neuspjelog brisanja: %d redaka", n)
			}
			if slucaj.veze >= 0 {
				nuzno(t, o.baza.QueryRow(`SELECT COUNT(*) FROM section_stations WHERE station_id = ?`, id).Scan(&n))
				if n != slucaj.veze {
					t.Errorf("veze nakon neuspjelog brisanja: %d, očekivano %d", n, slucaj.veze)
				}
			}
			nuzno(t, o.baza.QueryRow(`SELECT COUNT(*) FROM record_versions WHERE archived = 1`).Scan(&n))
			if n != 0 {
				t.Errorf("spomenika nakon neuspjelog brisanja: %d", n)
			}
		})
	}
}

// Popis samo letvi koje čekaju pregled; letva koja se ne da pročitati ruši
// popis, kao i zatvorena baza.
func TestPopisPostajaZaPregledIGreske(t *testing.T) {
	o := novaOkolinaPostaja(t)
	ctx := context.Background()
	zaPregled, err := o.repo.ListStations(ctx, "", "", "", true)
	nuzno(t, err)
	if imena := nazivi(zaPregled); len(imena) != 1 || imena[0] != "Primjerovo" {
		t.Errorf("letve za pregled: %v", imena)
	}

	izvrsiSve(t, o, `UPDATE stations SET created_at = 'nije vrijeme' WHERE code = 'kanalska'`)
	if postaje, err := o.repo.ListStations(ctx, "", "", "", false); err == nil {
		t.Errorf("popis s nečitljivom letvom bez greške: %v", nazivi(postaje))
	}
	if postaje, err := o.repo.GetStationsForSection(ctx, "P.1.1"); err != nil || len(postaje) != 2 {
		t.Errorf("vodomjeri dionice bez nečitljive letve: %v %v", nazivi(postaje), err)
	}
	izvrsiSve(t, o, `UPDATE stations SET created_at = 'nije vrijeme' WHERE code = 'probno'`)
	if postaje, err := o.repo.GetStationsForSection(ctx, "P.1.1"); err == nil {
		t.Errorf("vodomjeri dionice s nečitljivom letvom bez greške: %v", nazivi(postaje))
	}

	o.baza.Close()
	if _, err := o.repo.ListStations(ctx, "", "", "", false); err == nil || !strings.Contains(err.Error(), "greška pri dohvaćanju vodomjernih postaja") {
		t.Errorf("popis iz zatvorene baze: %v", err)
	}
	if _, err := o.repo.ListCountries(ctx); err == nil || !strings.Contains(err.Error(), "greška pri dohvaćanju država postaja") {
		t.Errorf("države iz zatvorene baze: %v", err)
	}
}

// Letva po šifri i po identifikatoru: letve koje nema nema ni greške; letva
// koja se ne da pročitati i dionice koje se ne daju pročitati jesu greška.
func TestLetvaPoSifriIIdentifikatoru(t *testing.T) {
	o := novaOkolinaPostaja(t)
	ctx := context.Background()
	if st, err := o.repo.GetStationByCode(ctx, "probno"); err != nil || st == nil || st.ID != o.probno.ID || len(st.SectionCodes) != 1 {
		t.Errorf("letva po šifri: %+v %v", st, err)
	}
	if st, err := o.repo.GetStationByCode(ctx, "nema-je"); st != nil || err != nil {
		t.Errorf("šifra koje nema: %+v %v", st, err)
	}
	if st, err := o.repo.GetStationByID(ctx, uuid.New()); st != nil || err != nil {
		t.Errorf("identifikator kojeg nema: %+v %v", st, err)
	}

	izvrsiSve(t, o, `UPDATE stations SET created_at = 'nije vrijeme' WHERE code = 'probno'`)
	if _, err := o.repo.GetStationByCode(ctx, "probno"); err == nil {
		t.Error("nečitljiva letva po šifri bez greške")
	}
	if _, err := o.repo.GetStationByID(ctx, o.probno.ID); err == nil || !strings.Contains(err.Error(), "greška pri dohvaćanju postaje") {
		t.Errorf("nečitljiva letva po identifikatoru: %v", err)
	}

	izvrsiSve(t, o, vezeBezDionice(o)...)
	if _, err := o.repo.GetSectionCodesForStation(ctx, o.primjerovo.ID); err == nil {
		t.Error("veza bez dionice bez greške")
	}
	if st, err := o.repo.GetStationByID(ctx, o.primjerovo.ID); err == nil {
		t.Errorf("letva s nečitljivom vezom bez greške: %+v", st)
	}

	izvrsiSve(t, o, `ALTER TABLE section_stations RENAME TO section_stations_negdje`)
	if _, err := o.repo.GetSectionCodesForStation(ctx, o.primjerovo.ID); err == nil || !strings.Contains(err.Error(), "greška pri dohvaćanju dionica postaje") {
		t.Errorf("dionice bez tablice veza: %v", err)
	}
}

// Veza bez letve ruši sektore i područja postaja umjesto da ih tiho skrati
func TestSektoriPostajaSVezomBezLetve(t *testing.T) {
	o := novaOkolinaPostaja(t)
	izvrsiSve(t, o,
		`DROP TABLE section_stations`,
		`CREATE TABLE section_stations (id TEXT, station_id TEXT, section_code TEXT, created_at DATETIME)`,
		`INSERT INTO section_stations VALUES ('veza-x', NULL, 'P.1.1', '2026-01-01 00:00:00')`)
	if sektori, _, err := o.repo.StationScopes(context.Background()); err == nil {
		t.Errorf("sektori s vezom bez letve bez greške: %v", sektori)
	}
}

// Izmjena koja se poslije upisa ne da pročitati ne ostavlja ni izmjenu ni
// verziju
func TestIzmjenaPostajeNecitljivaNakonUpisa(t *testing.T) {
	o := novaOkolinaPostaja(t)
	ctx := context.Background()
	izvrsiSve(t, o, `CREATE TRIGGER pokvari AFTER UPDATE OF name ON stations
		BEGIN UPDATE stations SET created_at = 'nije vrijeme' WHERE id = NEW.id; END`)
	st := *o.probno
	st.Name = "Probno novo"
	if err := o.repo.UpdateStation(ctx, &st); err == nil {
		t.Fatal("izmjena je prošla")
	}
	izvrsiSve(t, o, `DROP TRIGGER pokvari`)
	natrag, err := o.repo.GetStationByID(ctx, o.probno.ID)
	if err != nil || natrag == nil || natrag.Name != "Probno" {
		t.Errorf("postaja nakon neuspjele izmjene: %+v %v", natrag, err)
	}
	if v, err := o.rec.History(ctx, EntityStations, o.probno.ID.String()); err != nil || len(v) != 1 {
		t.Errorf("verzija nakon neuspjele izmjene: %d (%v)", len(v), err)
	}
}

// Kota ili ograda koja nije broj ne ruši upis letve: povijest kote i
// ograde se tada spremaju prazne
func TestPovijestKoteIOgradeKojeNisuBroj(t *testing.T) {
	nijeBroj := math.NaN()
	st := &models.Station{
		ZeroDatumHistory: []models.ZeroDatumChange{{ValidFrom: "2001-05-01", Datum: &nijeBroj}},
		OgradeNiza:       []models.OgradaNiza{{Izvor: "his2000", Velicina: "vodostaj", Ispod: &nijeBroj}},
	}
	if s := zeroDatumHistoryJSON(st); s != "[]" {
		t.Errorf("povijest kote koja nije broj: %s", s)
	}
	if s := ogradeNizaJSON(st); s != "[]" {
		t.Errorf("ograda koja nije broj: %s", s)
	}
	kota := 81.23
	st.ZeroDatumHistory[0].Datum = &kota
	if s := zeroDatumHistoryJSON(st); !strings.Contains(s, "81.23") {
		t.Errorf("povijest kote: %s", s)
	}
}

// Letva s identifikatorom koji nije UUID ruši popis i kaže koja je
func TestPopisPostajaSNeispravnimIdentifikatorom(t *testing.T) {
	o := novaOkolinaPostaja(t)
	izvrsiSve(t, o, `UPDATE stations SET id = 'nije-identifikator' WHERE code = 'bezimena'`)
	if _, err := o.repo.ListStations(context.Background(), "", "", "", false); err == nil ||
		!strings.Contains(err.Error(), `neispravan identifikator postaje "nije-identifikator"`) {
		t.Errorf("popis s neispravnim identifikatorom: %v", err)
	}
}
