package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Do 13. 10. 2026. brisanje zapisa na koji se drugi zapisi oslanjaju
// stranim ključem (naselje, općina, područje, sektor, korisnik) razilazilo
// je čvorove. Primatelj nije mogao obrisati naselje koje navodi dionica, ni
// područje u koje je sam u međuvremenu upisao objekt. Izvor je čekao zauvijek
// dijete koje je drugi čvor upisao obrisanom roditelju, a obnova površine
// pri pokretanju javljala je grešku svaki put.

// ima javlja broji li upit na površini čvora barem jedan redak
func ima(t *testing.T, c *cvor, upit string, args ...any) bool {
	t.Helper()
	return brojRedaka(t, c.db, upit, args...) > 0
}

// zivaUKnjizi javlja je li zadnja verzija zapisa u knjizi čvora živa
func zivaUKnjizi(t *testing.T, c *cvor, entitet, id string) bool {
	t.Helper()
	v, err := c.rec.Latest(ctxRaz, entitet, id)
	if errors.Is(err, ledger.ErrNoVersion) {
		return false
	}
	nuzno(t, err)
	return !v.Archived
}

// slozni traži da čvorovi imaju istu površinu kao prvi i da ništa ne čeka.
// Zatim se svaki čvor ponovno pokreće: obnova površine mora proći bez greške
// i ništa ne promijeniti (usporedba s kopijom površine prije pokretanja).
func slozni(t *testing.T, cvorovi ...*cvor) {
	t.Helper()
	if n := nedovrseniSada(); len(n) > 0 {
		t.Errorf("čeka oslonac: %v", n)
	}
	for _, c := range cvorovi[1:] {
		usporediPovrsine(t, cvorovi[0], c)
	}
	for _, c := range cvorovi {
		prije := kopijaCvora(t, c, c.ime+" prije pokretanja")
		if _, err := ponovnoPokretanje(t, c); err != nil {
			t.Errorf("%s: obnova površine pri pokretanju: %v", c.ime, err)
		}
		usporediPovrsine(t, prije, c)
	}
}

// Brisanje naselja ili općine koju navodi dionica stiže na čvor koji ima
// dionicu: s naseljem odlaze i njegove veze s dionicama. Dionica ostaje, a
// obnova njezina kazala vezu na obrisano naselje ili općinu preskače, pa je
// novi čvor dobije, a izvor je pri pokretanju obnovi bez greške.
func TestBrisanjeTeritorijaKojiNavodiDionica(t *testing.T) {
	for _, slucaj := range []struct {
		ime    string
		obrisi func(ter *TerritoryRepository, opcina, naselje int) error
		veza   int // veze dionice s teritorijem koje ostaju
	}{
		{"naselje", func(ter *TerritoryRepository, _, naselje int) error { return ter.DeleteSettlement(ctxRaz, naselje) }, 2},
		{"općina s naseljem", func(ter *TerritoryRepository, opcina, _ int) error { return ter.DeleteMunicipality(ctxRaz, opcina) }, 1},
	} {
		t.Run(slucaj.ime, func(t *testing.T) {
			sNedovrsenima(t)
			ured, teren := noviCvor(t, "ured"), noviCvor(t, "teren")
			ter := NewTerritoryRepository(ured.db, ured.rec)
			opcina := models.Municipality{CountyID: osnovaZupanija, Name: "Probna Općina Gornja", Type: "OPĆINA"}
			nuzno(t, ter.CreateMunicipality(ctxRaz, &opcina))
			naselje := models.Settlement{MunicipalityID: opcina.ID, CountyID: osnovaZupanija, Name: "Probno Selo"}
			nuzno(t, ter.CreateSettlement(ctxRaz, &naselje))
			ostaje := models.Settlement{MunicipalityID: osnovaOpcina, CountyID: osnovaZupanija, Name: "Selo Koje Ostaje"}
			nuzno(t, ter.CreateSettlement(ctxRaz, &ostaje))
			dionica := models.Section{Code: "B.16.9", AreaID: 16, SectorID: "B", Description: "probna", DescriptionCustom: true,
				Parts: []models.SectionPart{{Territories: []models.PartTerritory{
					{CountyID: osnovaZupanija, MunicipalityID: opcina.ID},
					{CountyID: osnovaZupanija, MunicipalityID: opcina.ID, SettlementID: &naselje.ID},
					{CountyID: osnovaZupanija, MunicipalityID: osnovaOpcina, SettlementID: &ostaje.ID},
				}}}}
			nuzno(t, NewSectionRepository(ured.db, ured.rec).SaveSection(ctxRaz, &dionica))
			posalji(t, ured, teren)
			if n := brojRedaka(t, teren.db, `SELECT count(*) FROM section_territories WHERE section_code = ?`, dionica.Code); n != 3 {
				t.Fatalf("teren ima %d veza dionice s teritorijem, a treba 3", n)
			}

			nuzno(t, slucaj.obrisi(ter, opcina.ID, naselje.ID))
			razmijeni(t, ured, teren)
			if ima(t, teren, `SELECT count(*) FROM settlements WHERE id = ?`, naselje.ID) {
				t.Error("teren i dalje ima obrisano naselje")
			}
			for _, c := range []*cvor{ured, teren} {
				if n := brojRedaka(t, c.db, `SELECT count(*) FROM section_territories WHERE section_code = ?`, dionica.Code); n != slucaj.veza {
					t.Errorf("%s: dionica ima %d veza s teritorijem, a treba %d", c.ime, n, slucaj.veza)
				}
			}
			novi := noviCvor(t, "novi")
			posalji(t, ured, novi)
			if !ima(t, novi, `SELECT count(*) FROM sections WHERE code = ?`, dionica.Code) {
				t.Error("novi čvor nema dionicu koja navodi obrisano naselje")
			}
			slozni(t, ured, teren, novi)
		})
	}
}

// Teren dodijeli dužnost korisniku kojeg ured u isto vrijeme briše. Brisanje
// ima prednost: ured dužnost ne upiše i ne čeka joj korisnika, a teren s
// korisnikom miče i dužnost. Novi čvor dobije isto, a pokretanje prolazi.
func TestDuznostKorisnikaObrisanogNaDrugomCvoru(t *testing.T) {
	sNedovrsenima(t)
	ured, teren := noviCvor(t, "ured"), noviCvor(t, "teren")
	korisnici := NewUserRepository(ured.db, ured.rec)
	ana := &models.User{Username: "ana.anic", FullName: "Ana Anić", OrgType: models.OrgHrvatskeVode, IsActive: true}
	nuzno(t, korisnici.CreateUser(ana, nil))
	posalji(t, ured, teren)

	sektor, podrucje := "B", 16
	d := models.Duty{UserID: ana.ID, Title: "Vodočuvar", Role: models.RoleAreaLeader, ScopeType: models.ScopeArea,
		SectorID: &sektor, AreaID: &podrucje}
	nuzno(t, NewUserRepository(teren.db, teren.rec).AddDuty(&d))
	nuzno(t, korisnici.DeleteUser(ana.ID))
	razmijeni(t, teren, ured)
	razmijeni(t, ured, teren)
	for _, c := range []*cvor{ured, teren} {
		if ima(t, c, `SELECT count(*) FROM users WHERE id = ?`, ana.ID.String()) ||
			ima(t, c, `SELECT count(*) FROM duties WHERE id = ?`, d.ID.String()) {
			t.Errorf("%s i dalje ima obrisanu korisnicu ili njezinu dužnost", c.ime)
		}
	}
	novi := noviCvor(t, "novi")
	posalji(t, teren, novi)
	slozni(t, ured, teren, novi)
}

// Brisanje korisnika daje spomenik i dužnosti koja je u knjizi živa, a na
// površini je nema, sa zadnjom verzijom dužnosti. Dužnost s površine dobiva
// spomenik s retkom, kao i dosad.
func TestBrisanjeKorisnikaArhiviraDuznostIzKnjige(t *testing.T) {
	sNedovrsenima(t)
	ured := noviCvor(t, "ured")
	korisnici := NewUserRepository(ured.db, ured.rec)
	ivo := &models.User{Username: "ivo.ivic", FullName: "Ivo Ivić", OrgType: models.OrgHrvatskeVode, IsActive: true}
	nuzno(t, korisnici.CreateUser(ivo, nil))
	sektor, podrucje := "B", 16
	naPovrsini := models.Duty{UserID: ivo.ID, Title: "Vodočuvar", Role: models.RoleAreaLeader, ScopeType: models.ScopeArea,
		SectorID: &sektor, AreaID: &podrucje}
	nuzno(t, korisnici.AddDuty(&naPovrsini))
	samoUKnjizi := models.Duty{UserID: ivo.ID, Title: "Zamjenik", Role: models.RoleAreaLeader, ScopeType: models.ScopeArea,
		SectorID: &sektor, AreaID: &podrucje}
	nuzno(t, korisnici.AddDuty(&samoUKnjizi))
	_, err := ured.db.Exec(`DELETE FROM duties WHERE id = ?`, samoUKnjizi.ID.String())
	nuzno(t, err)
	drugi := &models.User{Username: "ana.anic", FullName: "Ana Anić", OrgType: models.OrgHrvatskeVode, IsActive: true}
	nuzno(t, korisnici.CreateUser(drugi, nil))
	tudja := models.Duty{UserID: drugi.ID, Title: "Vodočuvar", Role: models.RoleAreaLeader, ScopeType: models.ScopeArea,
		SectorID: &sektor, AreaID: &podrucje}
	nuzno(t, korisnici.AddDuty(&tudja))

	nuzno(t, korisnici.DeleteUser(ivo.ID))
	for _, id := range []uuid.UUID{naPovrsini.ID, samoUKnjizi.ID} {
		if zivaUKnjizi(t, ured, EntityDuties, id.String()) {
			t.Errorf("dužnost %s obrisanog korisnika živa je u knjizi", id)
		}
	}
	zadnja, err := ured.rec.Latest(ctxRaz, EntityDuties, samoUKnjizi.ID.String())
	nuzno(t, err)
	if teret[models.Duty](t, zadnja.Payload).Title != "Zamjenik" {
		t.Errorf("spomenik dužnosti iz knjige ne nosi njezinu zadnju verziju: %s", zadnja.Payload)
	}
	if !zivaUKnjizi(t, ured, EntityDuties, tudja.ID.String()) {
		t.Error("dužnost drugog korisnika dobila je spomenik")
	}
	novi := noviCvor(t, "novi")
	posalji(t, ured, novi)
	slozni(t, ured, novi)
}

// Brisanje općine daje spomenik i naselju koje je u knjizi živo, a na
// površini ga nema
func TestBrisanjeOpcineArhiviraNaseljeIzKnjige(t *testing.T) {
	sNedovrsenima(t)
	ured := noviCvor(t, "ured")
	ter := NewTerritoryRepository(ured.db, ured.rec)
	opcina := models.Municipality{CountyID: osnovaZupanija, Name: "Probna Općina Srednja", Type: "OPĆINA"}
	nuzno(t, ter.CreateMunicipality(ctxRaz, &opcina))
	naPovrsini := models.Settlement{MunicipalityID: opcina.ID, CountyID: osnovaZupanija, Name: "Probno Selo Gornje"}
	nuzno(t, ter.CreateSettlement(ctxRaz, &naPovrsini))
	samoUKnjizi := models.Settlement{MunicipalityID: opcina.ID, CountyID: osnovaZupanija, Name: "Probno Selo Donje"}
	nuzno(t, ter.CreateSettlement(ctxRaz, &samoUKnjizi))
	_, err := ured.db.Exec(`DELETE FROM settlements WHERE id = ?`, samoUKnjizi.ID)
	nuzno(t, err)

	nuzno(t, ter.DeleteMunicipality(ctxRaz, opcina.ID))
	for _, id := range []int{naPovrsini.ID, samoUKnjizi.ID} {
		if zivaUKnjizi(t, ured, EntitySettlements, strconv.Itoa(id)) {
			t.Errorf("naselje %d obrisane općine živo je u knjizi", id)
		}
	}
	novi := noviCvor(t, "novi")
	posalji(t, ured, novi)
	slozni(t, ured, novi)
}

// Kad se spomenik dužnosti ili naselja ne da upisati, brisanje ne uspije i
// ništa ne ostaje napola obrisano
func TestBrisanjeSDjecomNeUspije(t *testing.T) {
	ured := noviCvor(t, "ured")
	korisnici := NewUserRepository(ured.db, ured.rec)
	ivo := &models.User{Username: "ivo.ivic", FullName: "Ivo Ivić", OrgType: models.OrgHrvatskeVode, IsActive: true}
	nuzno(t, korisnici.CreateUser(ivo, nil))
	sektor, podrucje := "B", 16
	d := models.Duty{UserID: ivo.ID, Title: "Vodočuvar", Role: models.RoleAreaLeader, ScopeType: models.ScopeArea,
		SectorID: &sektor, AreaID: &podrucje}
	nuzno(t, korisnici.AddDuty(&d))
	ter := NewTerritoryRepository(ured.db, ured.rec)
	naselje := models.Settlement{MunicipalityID: osnovaOpcina, CountyID: osnovaZupanija, Name: "Probno Selo"}
	nuzno(t, ter.CreateSettlement(ctxRaz, &naselje))
	_, err := ured.db.Exec(`CREATE TRIGGER kvar BEFORE INSERT ON record_versions
		WHEN NEW.archived = 1 AND NEW.entity IN ('duties', 'settlements') BEGIN SELECT RAISE(ABORT, 'namjerni kvar'); END`)
	nuzno(t, err)

	if err := korisnici.DeleteUser(ivo.ID); err == nil {
		t.Error("brisanje korisnika prošlo je bez spomenika dužnosti")
	}
	if !ima(t, ured, `SELECT count(*) FROM duties WHERE id = ?`, d.ID.String()) ||
		!ima(t, ured, `SELECT count(*) FROM users WHERE id = ?`, ivo.ID.String()) {
		t.Error("neuspjelo brisanje odnijelo je korisnika ili dužnost")
	}
	if err := ter.DeleteMunicipality(ctxRaz, osnovaOpcina); err == nil {
		t.Error("brisanje općine prošlo je bez spomenika naselja")
	}
	if !ima(t, ured, `SELECT count(*) FROM settlements WHERE id = ?`, naselje.ID) {
		t.Error("neuspjelo brisanje općine odnijelo je naselje")
	}
}

// slucajRoditelja: ured upiše roditelja, teren ga primi i upiše mu dijete,
// a ured za to ne zna i roditelja obriše
type slucajRoditelja struct {
	ime      string
	roditelj func(t *testing.T, ured *cvor)
	dijete   func(t *testing.T, teren *cvor) []string // upiti koji broje djecu
	obrisi   func(t *testing.T, ured *cvor)
	vrati    func(t *testing.T, ured *cvor) // roditelj se vraća pod istim identitetom; nil kad ne može
}

func slucajeviRoditelja() []slucajRoditelja {
	return []slucajRoditelja{
		{
			ime: "područje, objekt i dužnost",
			roditelj: func(t *testing.T, ured *cvor) {
				nuzno(t, NewOrgRepository(ured.db, ured.rec).SaveArea(ctxRaz, &models.Area{ID: 42, SectorID: "B", Name: "Područje 42", VgiName: "VGI 42"}))
			},
			dijete: func(t *testing.T, teren *cvor) []string {
				objekt := models.Structure{Code: "P42-CS", Name: "CS Probna", Kind: "CS", SectorID: "B", AreaID: 42}
				nuzno(t, NewStructureRepository(teren.db, teren.rec).CreateStructure(ctxRaz, &objekt))
				sektor, podrucje := "B", 42
				d := models.Duty{UserID: osnovaKorisnik, Title: "Vodočuvar", Role: models.RoleAreaLeader, ScopeType: models.ScopeArea,
					SectorID: &sektor, AreaID: &podrucje}
				nuzno(t, NewUserRepository(teren.db, teren.rec).AddDuty(&d))
				return []string{`SELECT count(*) FROM structures WHERE area_id = 42`, `SELECT count(*) FROM duties WHERE area_id = 42`}
			},
			obrisi: func(t *testing.T, ured *cvor) { nuzno(t, NewOrgRepository(ured.db, ured.rec).DeleteArea(ctxRaz, 42)) },
			vrati: func(t *testing.T, ured *cvor) {
				nuzno(t, NewOrgRepository(ured.db, ured.rec).SaveArea(ctxRaz, &models.Area{ID: 42, SectorID: "B", Name: "Područje 42 vraćeno", VgiName: "VGI 42"}))
			},
		},
		{
			ime: "sektor, područje, objekt i voda",
			roditelj: func(t *testing.T, ured *cvor) {
				nuzno(t, NewOrgRepository(ured.db, ured.rec).SaveSector(ctxRaz, &models.Sector{ID: "Q", Name: "Sektor Q", VgoName: "VGO Q", CenterCop: "COP Q", Level: 2}))
			},
			dijete: func(t *testing.T, teren *cvor) []string {
				nuzno(t, NewOrgRepository(teren.db, teren.rec).SaveArea(ctxRaz, &models.Area{ID: 43, SectorID: "Q", Name: "Područje 43", VgiName: "VGI 43"}))
				objekt := models.Structure{Code: "P43-CS", Name: "CS Probna", Kind: "CS", SectorID: "Q", AreaID: 43}
				nuzno(t, NewStructureRepository(teren.db, teren.rec).CreateStructure(ctxRaz, &objekt))
				// održavana voda zna samo područje, ne i sektor
				voda := models.MaintainedWater{AreaID: 43, Name: "Probni kanal", Program: models.ProgramA03}
				nuzno(t, NewMaintenanceRepository(teren.db, teren.rec).UpsertWater(ctxRaz, &voda))
				return []string{`SELECT count(*) FROM areas WHERE id = 43`, `SELECT count(*) FROM structures WHERE area_id = 43`,
					`SELECT count(*) FROM maintained_waters WHERE area_id = 43`}
			},
			obrisi: func(t *testing.T, ured *cvor) {
				nuzno(t, NewOrgRepository(ured.db, ured.rec).DeleteSector(ctxRaz, "Q"))
			},
			vrati: func(t *testing.T, ured *cvor) {
				nuzno(t, NewOrgRepository(ured.db, ured.rec).SaveSector(ctxRaz, &models.Sector{ID: "Q", Name: "Sektor Q vraćen", VgoName: "VGO Q", CenterCop: "COP Q", Level: 2}))
			},
		},
		{
			ime: "općina i naselje",
			roditelj: func(t *testing.T, ured *cvor) {
				nuzno(t, NewTerritoryRepository(ured.db, ured.rec).CreateMunicipality(ctxRaz, &models.Municipality{ID: 802, CountyID: osnovaZupanija, Name: "Probna Općina Nova", Type: "OPĆINA"}))
			},
			dijete: func(t *testing.T, teren *cvor) []string {
				nuzno(t, NewTerritoryRepository(teren.db, teren.rec).CreateSettlement(ctxRaz, &models.Settlement{MunicipalityID: 802, CountyID: osnovaZupanija, Name: "Probno Selo Novo"}))
				return []string{`SELECT count(*) FROM settlements WHERE municipality_id = 802`}
			},
			obrisi: func(t *testing.T, ured *cvor) {
				nuzno(t, NewTerritoryRepository(ured.db, ured.rec).DeleteMunicipality(ctxRaz, 802))
			},
		},
		{
			ime: "županija i općina",
			roditelj: func(t *testing.T, ured *cvor) {
				nuzno(t, NewTerritoryRepository(ured.db, ured.rec).CreateCounty(ctxRaz, &models.County{Code: "PRN", Name: "Probna Nova", Seat: "Sjedište"}))
			},
			dijete: func(t *testing.T, teren *cvor) []string {
				nuzno(t, NewTerritoryRepository(teren.db, teren.rec).CreateMunicipality(ctxRaz, &models.Municipality{CountyID: 902, Name: "Probna Općina Županije", Type: "OPĆINA"}))
				return []string{`SELECT count(*) FROM municipalities WHERE county_id = 902`}
			},
			obrisi: func(t *testing.T, ured *cvor) {
				nuzno(t, NewTerritoryRepository(ured.db, ured.rec).DeleteCounty(ctxRaz, 902))
			},
		},
	}
}

// Ured briše roditelja, a teren mu u isto vrijeme upiše dijete. Brisanje
// ima prednost: nakon razmjene u oba smjera nijedan čvor nema ni roditelja
// ni dijete, ništa ne čeka, a novi čvorovi dobiju isto. Kad se roditelj vrati
// pod istim identitetom, dijete (živo u knjizi) vraća se s njim odmah, na
// izvoru i na čvoru koji primi povratak, kao što bi ga vratilo pokretanje.
func TestBrisanjeRoditeljaDokDrugiCvorUpisujeDijete(t *testing.T) {
	for _, s := range slucajeviRoditelja() {
		t.Run(s.ime, func(t *testing.T) {
			sNedovrsenima(t)
			ured, teren := noviCvor(t, "ured"), noviCvor(t, "teren")
			s.roditelj(t, ured)
			razmijeni(t, ured, teren)
			djeca := s.dijete(t, teren)
			s.obrisi(t, ured)
			razmijeni(t, teren, ured)
			razmijeni(t, ured, teren)
			for _, c := range []*cvor{ured, teren} {
				for _, upit := range djeca {
					if ima(t, c, upit) {
						t.Errorf("%s: dijete obrisanog roditelja ostalo je na površini (%s)", c.ime, upit)
					}
				}
			}
			noviOdUreda, noviOdTerena := noviCvor(t, "novi od ureda"), noviCvor(t, "novi od terena")
			posalji(t, ured, noviOdUreda)
			posalji(t, teren, noviOdTerena)
			slozni(t, ured, teren, noviOdUreda, noviOdTerena)
			if s.vrati == nil {
				return
			}

			s.vrati(t, ured)
			for _, upit := range djeca {
				if !ima(t, ured, upit) {
					t.Errorf("ured: dijete se nije vratilo s roditeljem (%s)", upit)
				}
			}
			razmijeni(t, ured, teren)
			novi := noviCvor(t, "novi nakon povratka")
			posalji(t, ured, novi)
			slozni(t, ured, teren, novi)
		})
	}
}

// Ured briše dionicu, teren je ponovno upiše pod istom šifrom, a ured zatim
// otvori epizodu obrane na njoj. Do 13. 10. 2026. čvor koji je prvo primio
// uredove verzije (brisanje i epizodu), a terenov ponovni upis poslije, nije
// imao epizodu do ponovnog pokretanja.
func TestEpizodaPonovnoUpisaneDioniceStizeSDionicom(t *testing.T) {
	sNedovrsenima(t)
	ured, teren := noviCvor(t, "ured"), noviCvor(t, "teren")
	dionice := NewSectionRepository(ured.db, ured.rec)
	sec := probnaDionica("dionica")
	nuzno(t, dionice.SaveSection(ctxRaz, &sec))
	nuzno(t, dionice.ArhivirajSekciju(ctxRaz, obrisanaDionica))
	razmijeni(t, ured, teren)
	opet := probnaDionica("dionica ponovno upisana")
	nuzno(t, NewSectionRepository(teren.db, teren.rec).SaveSection(ctxRaz, &opet))
	razmijeni(t, teren, ured)
	nova := models.DefenseEpisode{SectionCode: obrisanaDionica, StartedAt: danObrane, Phase: models.PhasePrep, DeclaredBy: "Ivo Ivić"}
	nuzno(t, NewEpisodeRepository(ured.db, ured.rec).SaveEpisode(ctxRaz, &nova))

	sve, err := ured.rec.Since(ctxRaz, "", 0)
	nuzno(t, err)
	var uredove, terenove []ledger.Version
	for _, v := range sve {
		if v.NodeID == "ured" {
			uredove = append(uredove, v)
		} else {
			terenove = append(terenove, v)
		}
	}
	odjednom, uredPaTeren, terenPaUred := noviCvor(t, "odjednom"), noviCvor(t, "ured pa teren"), noviCvor(t, "teren pa ured")
	primi(t, odjednom, sve)
	primi(t, uredPaTeren, uredove)
	primi(t, uredPaTeren, terenove)
	primi(t, terenPaUred, terenove)
	primi(t, terenPaUred, uredove)
	for _, c := range []*cvor{ured, odjednom, uredPaTeren, terenPaUred} {
		if !epizodaNaPovrsini(t, c, nova.ID.String()) {
			t.Errorf("%s nema epizodu ponovno upisane dionice", c.ime)
		}
	}
	slozni(t, ured, odjednom, uredPaTeren, terenPaUred)
}

// Ured briše dionicu, a teren, koji za brisanje ne zna, otvori epizodu na
// njoj. Ured epizodu primi (dionice nema, pa ne ide na površinu) i zatim
// dionicu ponovno upiše. Epizoda dolazi s dionicom odmah, ne tek nakon
// ponovnog pokretanja.
func TestEpizodaSeVracaSDionicomNaIzvoru(t *testing.T) {
	sNedovrsenima(t)
	ured, teren := noviCvor(t, "ured"), noviCvor(t, "teren")
	dionice := NewSectionRepository(ured.db, ured.rec)
	sec := probnaDionica("dionica")
	nuzno(t, dionice.SaveSection(ctxRaz, &sec))
	razmijeni(t, ured, teren)
	nuzno(t, dionice.ArhivirajSekciju(ctxRaz, obrisanaDionica))
	e := models.DefenseEpisode{SectionCode: obrisanaDionica, StartedAt: danObrane, Phase: models.PhasePrep, DeclaredBy: "Ana Anić"}
	nuzno(t, NewEpisodeRepository(teren.db, teren.rec).SaveEpisode(ctxRaz, &e))
	razmijeni(t, teren, ured)
	if epizodaNaPovrsini(t, ured, e.ID.String()) {
		t.Fatal("epizoda obrisane dionice došla je na površinu")
	}
	opet := probnaDionica("dionica ponovno upisana")
	// Teren za brisanje ne zna, pa ponovni upis primi kao izmjenu i zadrži
	// staro vrijeme upisa dionice. Isto vrijeme upisa drži usporedbu na epizodi.
	opet.CreatedAt = sec.CreatedAt
	nuzno(t, dionice.SaveSection(ctxRaz, &opet))
	if !epizodaNaPovrsini(t, ured, e.ID.String()) {
		t.Error("epizoda se nije vratila s ponovno upisanom dionicom")
	}
	razmijeni(t, ured, teren)
	slozni(t, ured, teren)
}

// Dijete koje se vraća s roditeljem, a drugi mu oslonac još nije stigao,
// čeka među nedovršenima; roditelj ide na površinu bez njega
func TestDijeteKojeSeVracaCekaDrugiOslonac(t *testing.T) {
	sNedovrsenima(t)
	ured := noviCvor(t, "ured")
	org := NewOrgRepository(ured.db, ured.rec)
	nuzno(t, org.SaveArea(ctxRaz, &models.Area{ID: 42, SectorID: "B", Name: "Područje 42", VgiName: "VGI 42"}))
	nuzno(t, org.DeleteArea(ctxRaz, 42))
	objekt := uuid.MustParse("01900000-0000-7000-8000-0000000b0042")
	_, err := ured.rec.Record(ctxRaz, ured.db, EntityStructures, objekt.String(),
		models.Structure{ID: objekt, Code: "P42-CS", Name: "CS Probna", Kind: "CS", SectorID: "NEMA", AreaID: 42})
	nuzno(t, err)

	nuzno(t, org.SaveArea(ctxRaz, &models.Area{ID: 42, SectorID: "B", Name: "Područje 42", VgiName: "VGI 42"}))
	if !ima(t, ured, `SELECT count(*) FROM areas WHERE id = 42`) {
		t.Fatal("područje se nije vratilo")
	}
	if ima(t, ured, `SELECT count(*) FROM structures WHERE id = ?`, objekt.String()) {
		t.Error("objekt bez sektora došao je na površinu")
	}
	if !nedovrsen(EntityStructures + "|" + objekt.String()) {
		t.Errorf("objekt ne čeka sektor: %v", nedovrseniSada())
	}
}

// Identitet roditelja čita se iz teksta i broja; prazno, nula i null znače
// da roditelja nema
func TestIdentitetRoditelja(t *testing.T) {
	for polje, want := range map[string]string{
		`"Q"`: "Q", `42`: "42", `0`: "", `""`: "", `null`: "", `true`: "", ``: "",
	} {
		if got := identitetRoditelja(json.RawMessage(polje)); got != want {
			t.Errorf("identitetRoditelja(%q) = %q, a treba %q", polje, got, want)
		}
	}
}

// Greške baze u pravilima roditelja ne prolaze tiho
func TestPravilaRoditeljaGreske(t *testing.T) {
	c := noviCvor(t, "ured")
	org := NewOrgRepository(c.db, c.rec)
	nuzno(t, org.SaveArea(ctxRaz, &models.Area{ID: 42, SectorID: "B", Name: "Područje 42", VgiName: "VGI 42"}))
	podrucje, err := c.rec.Latest(ctxRaz, EntityAreas, "42")
	nuzno(t, err)
	spomenik := *podrucje
	spomenik.Archived = true

	for _, slucaj := range []struct {
		ime, kvar string
		radi      func(tx *sql.Tx) error
	}{
		{"sektor se ne da pročitati", `ALTER TABLE sectors RENAME TO sektori_drugdje`,
			func(tx *sql.Tx) error { return primijeniVerziju(ctxRaz, tx, *podrucje) }},
		{"knjiga se ne da pročitati", `ALTER TABLE record_versions RENAME TO knjiga_drugdje`,
			func(tx *sql.Tx) error { return primijeniVerziju(ctxRaz, tx, *podrucje) }},
		{"djeca se ne daju pročitati pri brisanju", `ALTER TABLE structures RENAME TO objekti_drugdje`,
			func(tx *sql.Tx) error { return primijeniVerziju(ctxRaz, tx, spomenik) }},
		{"djeca se ne daju pročitati pri povratku", `ALTER TABLE structures RENAME TO objekti_drugdje`,
			func(tx *sql.Tx) error { return vratiDjecu(ctxRaz, tx, EntityAreas, "42") }},
	} {
		t.Run(slucaj.ime, func(t *testing.T) {
			tx, err := c.db.BeginTx(ctxRaz, nil)
			nuzno(t, err)
			defer tx.Rollback()
			_, err = tx.ExecContext(ctxRaz, slucaj.kvar)
			nuzno(t, err)
			if err := slucaj.radi(tx); err == nil {
				t.Error("greška baze prošla je tiho")
			}
		})
	}
}
