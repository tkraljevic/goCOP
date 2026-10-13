package repository

import (
	"slices"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Brisanje letve ili objekta miče vezu iz kazala dionice, ali dionica ga i
// dalje navodi u poddionicama. Do 13. 10. 2026. obnova kazala pala je tada na
// stranom ključu: novi čvor dionicu, njezina kazala i epizode nije dobio
// (čekali su među nedovršenima), a obnova površine pri pokretanju javljala je
// grešku za dionicu na svakom čvoru, i na izvoru. Ni na izvoru se dionica
// više nije dala spremiti.

const dionicaObrisaneLetve = "B.16.8"

// izvorObrisaneLetve je čvor "ured" s dionicom koja navodi dvije letve, dva
// objekta i nasip, i epizodom obrane na njoj. Brisanje odnosi jednu letvu,
// jedan objekt i nasip.
type izvorObrisaneLetve struct {
	c                *cvor
	obrisana, ostaje models.Station
	crpka, ustava    models.Structure
	nasip            models.Structure
	dionica          models.Section
	epizoda          models.DefenseEpisode
}

func noviIzvorObrisaneLetve(t *testing.T) *izvorObrisaneLetve {
	t.Helper()
	i := &izvorObrisaneLetve{c: noviCvor(t, "ured")}
	letve := NewStationRepository(i.c.db, i.c.rec)
	i.obrisana = models.Station{Code: "B16-OBR", Name: "Probna letva koja se briše"}
	nuzno(t, letve.CreateStation(ctxRaz, &i.obrisana))
	i.ostaje = models.Station{Code: "B16-OST", Name: "Probna letva koja ostaje"}
	nuzno(t, letve.CreateStation(ctxRaz, &i.ostaje))

	objekti := NewStructureRepository(i.c.db, i.c.rec)
	objekt := func(kod, naziv, vrsta string) models.Structure {
		s := models.Structure{Code: kod, Name: naziv, Kind: vrsta, SectorID: "B", AreaID: 16}
		nuzno(t, objekti.CreateStructure(ctxRaz, &s))
		return s
	}
	i.crpka = objekt("B16-CS-OBR", "CS Probna Donja", "CS")
	i.ustava = objekt("B16-US-OST", "Ustava Probna Gornja", "USTAVA")
	i.nasip = objekt("B16-NAS-OBR", "Probni nasip", models.StructureKindEmbankment)

	i.dionica = models.Section{Code: dionicaObrisaneLetve, AreaID: 16, SectorID: "B",
		Description: "dionica s obrisanom letvom", DescriptionCustom: true,
		Parts: []models.SectionPart{{
			StationIDs: []string{i.obrisana.ID.String(), i.ostaje.ID.String()},
			Objects: []models.PartObject{{StructureID: i.crpka.ID.String(), Name: i.crpka.Name},
				{StructureID: i.ustava.ID.String(), Name: i.ustava.Name}},
			Embankments: []models.PartEmbankment{{StructureID: i.nasip.ID.String(), Name: i.nasip.Name}},
		}}}
	nuzno(t, NewSectionRepository(i.c.db, i.c.rec).SaveSection(ctxRaz, &i.dionica))
	i.epizoda = models.DefenseEpisode{SectionCode: dionicaObrisaneLetve, StationID: i.ostaje.ID.String(),
		StartedAt: danObrane, Phase: models.PhasePrep, DeclaredBy: "Ivo Probić"}
	nuzno(t, NewEpisodeRepository(i.c.db, i.c.rec).SaveEpisode(ctxRaz, &i.epizoda))
	return i
}

// obrisi briše letvu, objekt i nasip koje dionica navodi, kako to radi registar
func (i *izvorObrisaneLetve) obrisi(t *testing.T) {
	t.Helper()
	nuzno(t, NewStationRepository(i.c.db, i.c.rec).DeleteStation(ctxRaz, i.obrisana.ID))
	objekti := NewStructureRepository(i.c.db, i.c.rec)
	nuzno(t, objekti.DeleteStructure(ctxRaz, i.crpka.ID))
	nuzno(t, objekti.DeleteStructure(ctxRaz, i.nasip.ID))
}

// vezeDionice vraća poredane letve i objekte iz kazala dionice
func vezeDionice(t *testing.T, c *cvor, kod string) (letve, objekti []string) {
	t.Helper()
	citaj := func(upit string) []string {
		rows, err := c.db.Query(upit, kod)
		nuzno(t, err)
		defer rows.Close()
		var out []string
		for rows.Next() {
			var id string
			nuzno(t, rows.Scan(&id))
			out = append(out, id)
		}
		nuzno(t, rows.Err())
		slices.Sort(out)
		return out
	}
	return citaj(`SELECT station_id FROM section_stations WHERE section_code = ?`),
		citaj(`SELECT structure_id FROM section_structures WHERE section_code = ?`)
}

// provjeriDionicu traži da čvor ima dionicu s epizodom, a u kazalu samo letvu
// i objekt koji nisu obrisani
func (i *izvorObrisaneLetve) provjeriDionicu(t *testing.T, c *cvor) {
	t.Helper()
	if n := brojRedaka(t, c.db, `SELECT count(*) FROM sections WHERE code = ?`, dionicaObrisaneLetve); n != 1 {
		t.Fatalf("%s: dionice %s nema na površini", c.ime, dionicaObrisaneLetve)
	}
	letve, objekti := vezeDionice(t, c, dionicaObrisaneLetve)
	if want := []string{i.ostaje.ID.String()}; !slices.Equal(letve, want) {
		t.Errorf("%s: kazalo letvi dionice je %v, a treba %v", c.ime, letve, want)
	}
	if want := []string{i.ustava.ID.String()}; !slices.Equal(objekti, want) {
		t.Errorf("%s: kazalo objekata dionice je %v, a treba %v", c.ime, objekti, want)
	}
	if !epizodaNaPovrsini(t, c, i.epizoda.ID.String()) {
		t.Errorf("%s: epizode obrane dionice nema na površini", c.ime)
	}
}

// verzijeRegistra dijeli verzije izvora na letve i objekte (sa spomenicima i
// spomenicima veza letve s dionicom) i sve ostalo
func verzijeRegistra(t *testing.T, a *cvor) (registar, ostalo []ledger.Version) {
	t.Helper()
	sve, err := a.rec.Since(ctxRaz, "", 0)
	nuzno(t, err)
	for _, v := range sve {
		switch v.Entity {
		case EntityStations, EntityStructures, EntitySectionStations:
			registar = append(registar, v)
		default:
			ostalo = append(ostalo, v)
		}
	}
	return registar, ostalo
}

// Na izvoru dionica ostaje, kazalo bez obrisanih zapisa, obnova površine
// pri pokretanju prolazi bez greške, a dionica se i dalje da spremiti.
func TestDionicaNakonBrisanjaLetveNaIzvoru(t *testing.T) {
	sNedovrsenima(t)
	i := noviIzvorObrisaneLetve(t)
	i.obrisi(t)
	i.provjeriDionicu(t, i.c)

	if _, greska := ponovnoPokretanje(t, i.c); greska != nil {
		t.Errorf("obnova površine na izvoru: %v", greska)
	}
	i.provjeriDionicu(t, i.c)

	i.dionica.Description = "dionica izmijenjena nakon brisanja letve"
	if err := NewSectionRepository(i.c.db, i.c.rec).SaveSection(ctxRaz, &i.dionica); err != nil {
		t.Fatalf("dionica se nakon brisanja letve ne da spremiti: %v", err)
	}
	i.provjeriDionicu(t, i.c)
	if !slices.Contains(i.dionica.AllStationIDs(), i.obrisana.ID.String()) {
		t.Error("spremanje je maknulo obrisanu letvu iz dionice; to je odluka korisnika, ne obnove kazala")
	}
}

// Novi čvor dobije dionicu s kazalima i epizodom kojim god redom stigle
// dionica i brisanje, a ponovno pokretanje ne javlja grešku. Isto vrijedi za
// dionicu izmijenjenu nakon brisanja.
func TestDionicaSObrisanomLetvomNaNovomCvoru(t *testing.T) {
	sNedovrsenima(t)
	slucajevi := []struct {
		ime string
		// izmijeni: dionica dobiva novu verziju nakon brisanja, s istim
		// poveznicama
		izmijeni bool
		// cekaPrije: dionica čeka letvu dok brisanje ne stigne
		cekaPrije bool
		dostavi   func(t *testing.T, b *cvor, registar, ostalo []ledger.Version) []error
	}{
		{ime: "sve u jednom paketu", dostavi: func(t *testing.T, b *cvor, registar, ostalo []ledger.Version) []error {
			return []error{primiPaket(t, b, append(slices.Clone(registar), ostalo...)...)}
		}},
		{ime: "brisanje stiže prije dionice", dostavi: func(t *testing.T, b *cvor, registar, ostalo []ledger.Version) []error {
			return []error{primiPaket(t, b, registar...), primiPaket(t, b, ostalo...)}
		}},
		{ime: "dionica stiže prije letve i brisanja", cekaPrije: true, dostavi: func(t *testing.T, b *cvor, registar, ostalo []ledger.Version) []error {
			return []error{primiPaket(t, b, ostalo...), primiPaket(t, b, registar...)}
		}},
		{ime: "dionica izmijenjena nakon brisanja, brisanje stiže prije", izmijeni: true,
			dostavi: func(t *testing.T, b *cvor, registar, ostalo []ledger.Version) []error {
				return []error{primiPaket(t, b, registar...), primiPaket(t, b, ostalo...)}
			}},
		{ime: "dionica izmijenjena nakon brisanja, dionica stiže prije letve", izmijeni: true, cekaPrije: true,
			dostavi: func(t *testing.T, b *cvor, registar, ostalo []ledger.Version) []error {
				return []error{primiPaket(t, b, ostalo...), primiPaket(t, b, registar...)}
			}},
	}
	for _, s := range slucajevi {
		t.Run(s.ime, func(t *testing.T) {
			sNedovrsenima(t)
			i := noviIzvorObrisaneLetve(t)
			i.obrisi(t)
			if s.izmijeni {
				i.dionica.Description = "dionica izmijenjena nakon brisanja letve"
				nuzno(t, NewSectionRepository(i.c.db, i.c.rec).SaveSection(ctxRaz, &i.dionica))
			}
			registar, ostalo := verzijeRegistra(t, i.c)

			b := noviCvor(t, "novi-ured")
			greske := s.dostavi(t, b, registar, ostalo)
			for n, err := range greske {
				nuzno(t, bezDjelomicnih(err))
				ocekuje := s.cekaPrije && n == 0
				if ocekuje != (err != nil) {
					t.Errorf("paket %d: greška %v, a dionica treba čekati: %v", n+1, err, ocekuje)
				}
			}
			if ceka := nedovrseniSada(); len(ceka) > 0 {
				t.Errorf("nakon dostave čeka %v", ceka)
			}
			i.provjeriDionicu(t, b)
			usporediPovrsine(t, i.c, b)

			if _, greska := ponovnoPokretanje(t, b); greska != nil {
				t.Errorf("obnova površine pri pokretanju: %v", greska)
			}
			i.provjeriDionicu(t, b)
			usporediPovrsine(t, i.c, b)
		})
	}
}

// Čvor koji je dionicu dobio prije brisanja izgubi s brisanjem samo vezu na
// obrisane zapise, a ponovno pokretanje ne javlja grešku za dionicu.
func TestDionicaSObrisanomLetvomNaCvoruKojiJuJeImao(t *testing.T) {
	sNedovrsenima(t)
	i := noviIzvorObrisaneLetve(t)
	teren := noviCvor(t, "teren")
	posalji(t, i.c, teren)
	if letve, objekti := vezeDionice(t, teren, dionicaObrisaneLetve); len(letve) != 2 || len(objekti) != 3 {
		t.Fatalf("prije brisanja teren ima u kazalu %v i %v, a treba dvije letve i tri objekta", letve, objekti)
	}

	i.obrisi(t)
	posalji(t, i.c, teren)
	i.provjeriDionicu(t, teren)
	if _, greska := ponovnoPokretanje(t, teren); greska != nil {
		t.Errorf("obnova površine pri pokretanju: %v", greska)
	}
	i.provjeriDionicu(t, teren)
	usporediPovrsine(t, i.c, teren)
}

// Letva koja još nije stigla nije obrisana: dionica je čeka među
// nedovršenima i dođe s vezom kad letva stigne. Odlučuje zadnja verzija:
// letva obrisana pa vraćena nije obrisana ni kad je knjiga ima prije
// površine.
func TestDionicaCekaLetvuKojaNijeObrisana(t *testing.T) {
	t.Run("letva još nije stigla", func(t *testing.T) {
		sNedovrsenima(t)
		i := noviIzvorObrisaneLetve(t)
		registar, ostalo := verzijeRegistra(t, i.c)
		b := noviCvor(t, "novi-ured")
		i.dionicaCeka(t, b, ostalo)
		nuzno(t, primiPaket(t, b, registar...))
		i.dionicaDosla(t, b, i.obrisana, i.ostaje)
	})
	t.Run("letva obrisana pa vraćena", func(t *testing.T) {
		sNedovrsenima(t)
		i := noviIzvorObrisaneLetve(t)
		i.obrisi(t)
		registar, ostalo := verzijeRegistra(t, i.c)
		b := noviCvor(t, "novi-ured")
		nuzno(t, primiPaket(t, b, registar...))

		// vraćena letva je u knjizi primatelja, a na površinu još nije došla
		letva := i.obrisana
		letva.Notes = "vraćena nakon brisanja"
		vracena := verzijaZa(t, EntityStations, letva.ID.String(), uuid.Must(uuid.NewV7()).String(), letva)
		if _, err := b.rec.Apply(ctxRaz, []ledger.Version{vracena}); err != nil {
			t.Fatal(err)
		}
		i.dionicaCeka(t, b, ostalo)
		nuzno(t, primiPaket(t, b, vracena))
		i.dionicaDosla(t, b, i.obrisana, i.ostaje)
	})
}

// dionicaCeka prima dionicu i epizodu i traži da čekaju letvu
func (i *izvorObrisaneLetve) dionicaCeka(t *testing.T, b *cvor, ostalo []ledger.Version) {
	t.Helper()
	err := primiPaket(t, b, ostalo...)
	nuzno(t, bezDjelomicnih(err))
	if err == nil || !nedovrsen(EntitySections+"|"+dionicaObrisaneLetve) {
		t.Fatalf("dionica ne čeka letvu koje još nema na površini (%v)", err)
	}
}

// dionicaDosla traži da nakon dolaska letve ništa ne čeka, a u kazalu dionice
// budu obje letve
func (i *izvorObrisaneLetve) dionicaDosla(t *testing.T, b *cvor, letve ...models.Station) {
	t.Helper()
	if ceka := nedovrseniSada(); len(ceka) > 0 {
		t.Errorf("nakon dolaska letve čeka %v", ceka)
	}
	var want []string
	for _, l := range letve {
		want = append(want, l.ID.String())
	}
	slices.Sort(want)
	if got, _ := vezeDionice(t, b, dionicaObrisaneLetve); !slices.Equal(got, want) {
		t.Errorf("kazalo letvi dionice je %v, a treba %v", got, want)
	}
	if !epizodaNaPovrsini(t, b, i.epizoda.ID.String()) {
		t.Error("epizoda obrane nije došla s dionicom")
	}
}
