package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Karakterizacija opisa jedne verzije knjige u redak zida: za svaku vrstu
// entiteta novi zapis, izmjena, brisanje, nečitljiv zapis i zapis kojem
// nešto nedostaje. Tekst je ono što ljudi čitaju na zidu, pa se provjerava
// cijeli redak, slovo po slovo, i kad verzija na zid ne ide (opisi tada
// vraća redak napola ispunjen, a Zadnji ga odbaci).

var kadVerzijeZida = time.Date(2026, 10, 20, 9, 30, 0, 0, time.UTC)

// izmišljene osobe i letve iz predmemorije opisivača
var (
	idAne        = uuid.MustParse("0b6f3c3e-1111-4000-8000-000000000001")
	idIve        = uuid.MustParse("0b6f3c3e-1111-4000-8000-000000000002")
	idPrimjerova = uuid.MustParse("0b6f3c3e-2222-4000-8000-000000000001")
	idUzvodne    = uuid.MustParse("0b6f3c3e-2222-4000-8000-000000000002")
	idBezPragova = uuid.MustParse("0b6f3c3e-2222-4000-8000-000000000003")
)

// probaOpisa je knjiga s dvjema prethodnim verzijama inventure (zaključenom
// i nacrtom), da se vidi vraćanje u nacrt
type probaOpisa struct {
	rec                *ledger.Recorder
	zakljucena, nacrta string
}

func novaProbaOpisa(t *testing.T) probaOpisa {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "zid-opisi.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	rec := ledger.New(baza, "test")
	ctx := context.Background()
	zakljuceno := kadVerzijeZida
	zak, err := rec.Record(ctx, baza, repository.EntityMtsPopisi, "pop-1", models.Popis{ID: "pop-1", ZakljucenoAt: &zakljuceno})
	if err != nil {
		t.Fatal(err)
	}
	nac, err := rec.Record(ctx, baza, repository.EntityMtsPopisi, "pop-2", models.Popis{ID: "pop-2"})
	if err != nil {
		t.Fatal(err)
	}
	return probaOpisa{rec: rec, zakljucena: zak, nacrta: nac}
}

// opisivac bez repozitorija: sve što zid traži stoji u predmemoriji, a što
// u njoj nema ne postoji
func (p probaOpisa) opisivac() *opisivac {
	pr := func(naziv string, sektori []string) *models.Station {
		st := &models.Station{ID: uuid.New(), Name: naziv, PrateSektori: sektori}
		st.Prep, st.Regular, st.Emergency, st.State = pragCm(300), pragCm(500), pragCm(650), pragCm(800)
		return st
	}
	primjerovo, uzvodna := pr("Primjerovo", []string{"Q", "P"}), pr("Uzvodna", nil)
	primjerovo.ID, uzvodna.ID = idPrimjerova, idUzvodne
	return &opisivac{
		s:   &ZidService{rec: p.rec},
		ctx: context.Background(),
		journali: map[string]*models.Journal{
			"j-cop":     {ID: "j-cop", Kind: models.JournalKindDefense, CentarSektor: "B", Title: "Dnevnik COP-a Osijek 2026."},
			"j-gradnja": {ID: "j-gradnja", Kind: models.JournalKindMaintenanceA02, CentarSektor: "B"},
		},
		dionice: map[string]*models.Section{"B.34.1": {Code: "B.34.1", SectorID: "B", AreaID: 34}},
		vrste:   map[string]*models.VrstaSredstva{"vrece-50x80": {ID: "vrece-50x80", Naziv: "Vreće 50x80 cm", Jedinica: "kom"}},
		skladista: map[string]*models.Skladiste{
			"sk-os":  {ID: "sk-os", Sektor: "B", AreaID: 34, Naziv: "Centralno skladište Osijek"},
			"sk-bez": {ID: "sk-bez", Sektor: "B", Naziv: "Priručno skladište"},
		},
		imena: map[string]string{idAne.String(): "Ana Anić", idIve.String(): "Ivo Ivić"},
		postaje: map[string]*models.Station{
			idPrimjerova.String(): primjerovo,
			idUzvodne.String():    uzvodna,
			idBezPragova.String(): {ID: idBezPragova, Name: "Bez pragova"},
		},
		podrucja: map[int]string{34: "B", 51: "D"},
		prijave:  map[string][2]string{"pr-1": {"v-objava", "v-rjesenje"}},
	}
}

// verzijaZida je verzija entiteta s izmišljenim sadržajem; nova dok je ne
// promijeni izmjena
func verzijaZida(t *testing.T, entity string, payload any, izmjene ...func(*ledger.Version)) ledger.Version {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	v := ledger.Version{VersionID: "v-1", Entity: entity, EntityID: "e-1", NodeID: "cvor-a", Payload: b, CreatedAt: kadVerzijeZida}
	for _, f := range izmjene {
		f(&v)
	}
	return v
}

func izmjenaVerzije(v *ledger.Version)    { v.Supersedes = "v-0" }
func arhiviranaVerzija(v *ledger.Version) { v.Archived = true }
func necitljivaVerzija(v *ledger.Version) { v.Payload = json.RawMessage("{") }
func verzijaID(id string) func(*ledger.Version) {
	return func(v *ledger.Version) { v.VersionID, v.EntityID = id, "pr-1" }
}
func zamjenjuje(id string) func(*ledger.Version) {
	return func(v *ledger.Version) { v.Supersedes = id }
}

// slucajOpisa: verzija, očekivani ok i redak; redak se slaže od osnove
// (verzija, modul, vrijeme, čvor) i polja koja opis upiše
type slucajOpisa struct {
	ime   string
	v     ledger.Version
	ok    bool
	modul string
	redak func(d *Dogadjaj)
}

func provjeriOpise(t *testing.T, p probaOpisa, slucajevi []slucajOpisa) {
	t.Helper()
	for _, s := range slucajevi {
		want := Dogadjaj{VersionID: s.v.VersionID, Modul: s.modul, Kad: s.v.CreatedAt, Cvor: s.v.NodeID}
		if s.redak != nil {
			s.redak(&want)
		}
		got, ok := p.opisivac().opisi(s.v)
		if ok != s.ok || !reflect.DeepEqual(got, want) {
			t.Errorf("%s:\n dobiveno %v %+v\nočekivano %v %+v", s.ime, ok, got, s.ok, want)
		}
	}
}

func pint(n int) *int { return &n }

func TestZidOpisiZapisDnevnika(t *testing.T) {
	p := novaProbaOpisa(t)
	dojava := models.JournalEntry{JournalID: "j-cop", Kind: models.EntryKindReport, Text: "Procjeđivanje na rkm 1425+100.",
		ReportedBy: "vodočuvar Batina", UserName: "Ana Anić", Podrucje: pint(34)}
	obavijest := models.JournalEntry{JournalID: "j-cop", Kind: models.EntryKindNotice, Text: "Najava vala.", UserName: "Ivo Ivić"}
	dezurstvo := models.JournalEntry{JournalID: "j-cop", Kind: models.EntryKindDuty, Text: "Preuzeta smjena.", UserName: "Ana Anić"}
	dugi := obavijest
	dugi.Text = strings.Repeat("š", 230)
	tocno := obavijest
	tocno.Text = strings.Repeat("š", 220)
	bezVrste := obavijest
	bezVrste.Kind = ""
	storniran := dojava
	storniran.Voided = true
	gradnja, nepoznat := obavijest, obavijest
	gradnja.JournalID, nepoznat.JournalID = "j-gradnja", "j-nema"
	cop := func(tko string, podrucje int) func(d *Dogadjaj) {
		return func(d *Dogadjaj) {
			d.Sektor, d.Tko, d.Link, d.AreaID = "B", tko, "/dnevnici/j-cop", podrucje
		}
	}
	e := repository.EntityJournalEntries
	provjeriOpise(t, p, []slucajOpisa{
		{"nova dojava", verzijaZida(t, e, dojava), true, ModulDnevnik, func(d *Dogadjaj) {
			cop("Ana Anić", 34)(d)
			d.Naslov, d.Tekst = "Dojava", "vodočuvar Batina: Procjeđivanje na rkm 1425+100."
		}},
		{"nova obavijest", verzijaZida(t, e, obavijest), true, ModulDnevnik, func(d *Dogadjaj) {
			cop("Ivo Ivić", 0)(d)
			d.Naslov, d.Tekst = "Obavijest", "Najava vala."
		}},
		{"zapis dežurstva", verzijaZida(t, e, dezurstvo), true, ModulDezurstva, func(d *Dogadjaj) {
			cop("Ana Anić", 0)(d)
			d.Naslov, d.Tekst = "Dežurstvo", "Preuzeta smjena."
		}},
		{"izmijenjen zapis dežurstva", verzijaZida(t, e, dezurstvo, izmjenaVerzije), false, ModulDezurstva, cop("Ana Anić", 0)},
		{"storniran", verzijaZida(t, e, storniran, izmjenaVerzije), true, ModulDnevnik, func(d *Dogadjaj) {
			cop("Ana Anić", 34)(d)
			d.Naslov, d.Tekst = "Storniran zapis", "Procjeđivanje na rkm 1425+100."
		}},
		{"izmijenjen", verzijaZida(t, e, dojava, izmjenaVerzije), false, ModulDnevnik, cop("Ana Anić", 34)},
		{"predug tekst", verzijaZida(t, e, dugi), true, ModulDnevnik, func(d *Dogadjaj) {
			cop("Ivo Ivić", 0)(d)
			d.Naslov, d.Tekst = "Obavijest", strings.Repeat("š", 220)+"…"
		}},
		{"tekst točno na granici", verzijaZida(t, e, tocno), true, ModulDnevnik, func(d *Dogadjaj) {
			cop("Ivo Ivić", 0)(d)
			d.Naslov, d.Tekst = "Obavijest", strings.Repeat("š", 220)
		}},
		{"bez vrste nema naslova", verzijaZida(t, e, bezVrste), false, ModulDnevnik, func(d *Dogadjaj) {
			cop("Ivo Ivić", 0)(d)
			d.Tekst = "Najava vala."
		}},
		{"dnevnik nije COP-a", verzijaZida(t, e, gradnja), false, ModulDnevnik, nil},
		{"nepoznat dnevnik", verzijaZida(t, e, nepoznat), false, ModulDnevnik, nil},
		{"obrisan", verzijaZida(t, e, dojava, arhiviranaVerzija), false, ModulDnevnik, nil},
		{"nečitljiv", verzijaZida(t, e, dojava, necitljivaVerzija), false, ModulDnevnik, nil},
	})
}

func TestZidOpisiPrijavu(t *testing.T) {
	p := novaProbaOpisa(t)
	objavljeno := kadVerzijeZida.Add(-time.Hour)
	prijava := models.PrijavaSTerena{ID: "pr-1", Ime: "Marta Horvat", Sektor: "B", AreaID: 34, Broj: 7, Godina: 2026,
		Vrsta: models.PrijavaPrijava, Naslov: "Oštećena rampa", Vodotok: "Dunav", Stacionaza: "25+500",
		Status: models.PrijavaObjavljena, ObjavljenoAt: &objavljeno}
	obavijest := prijava
	obavijest.Vrsta, obavijest.Vodotok, obavijest.Stacionaza = models.PrijavaObavijest, "", ""
	rijesena := prijava
	rijesena.Status, rijesena.Rijesio = models.PrijavaRijesena, "Ivo Ivić"
	zahtjev := rijesena
	zahtjev.Vrsta, zahtjev.Rijesio = models.PrijavaZahtjev, ""
	nacrt := prijava
	nacrt.ObjavljenoAt = nil
	prenesena := prijava
	prenesena.Rekonstrukcija = true
	e := repository.EntityPrijave
	redak := func(v ledger.Version) func(d *Dogadjaj) {
		return func(d *Dogadjaj) {
			var pr models.PrijavaSTerena
			if err := json.Unmarshal(v.Payload, &pr); err != nil {
				t.Fatal(err)
			}
			d.Sektor, d.AreaID, d.Tko, d.Link, d.prijava = "B", 34, "Marta Horvat", "/prijave/pr-1", &pr
		}
	}
	vObjava := verzijaZida(t, e, prijava, verzijaID("v-objava"))
	vObavijest := verzijaZida(t, e, obavijest, verzijaID("v-objava"))
	vRjesenje := verzijaZida(t, e, rijesena, verzijaID("v-rjesenje"), izmjenaVerzije)
	vZahtjev := verzijaZida(t, e, zahtjev, verzijaID("v-rjesenje"), izmjenaVerzije)
	vUrudzba := verzijaZida(t, e, prijava, verzijaID("v-urudzba"), izmjenaVerzije)
	provjeriOpise(t, p, []slucajOpisa{
		{"objava prijave", vObjava, true, ModulPrijave, func(d *Dogadjaj) {
			redak(vObjava)(d)
			d.Naslov, d.Tekst, d.Vazno = "Prijava s terena B-T-7/2026", "Oštećena rampa · Dunav, st. 25+500", true
		}},
		{"objava obavijesti bez mjesta", vObavijest, true, ModulPrijave, func(d *Dogadjaj) {
			redak(vObavijest)(d)
			d.Naslov, d.Tekst = "Obavijest s terena B-T-7/2026", "Oštećena rampa"
		}},
		{"rješenje", vRjesenje, true, ModulPrijave, func(d *Dogadjaj) {
			redak(vRjesenje)(d)
			d.Naslov, d.Tekst, d.Tko = "Riješena prijava s terena", "B-T-7/2026: Oštećena rampa", "Ivo Ivić"
		}},
		{"rješenje zahtjeva bez rješavatelja", vZahtjev, true, ModulPrijave, func(d *Dogadjaj) {
			redak(vZahtjev)(d)
			d.Naslov, d.Tekst = "Riješen zahtjev s terena", "B-T-7/2026: Oštećena rampa"
		}},
		{"urudžba nije novi redak", vUrudzba, false, ModulPrijave, redak(vUrudzba)},
		{"nacrt", verzijaZida(t, e, nacrt, verzijaID("v-objava")), false, ModulPrijave, nil},
		{"prenesena iz BP16", verzijaZida(t, e, prenesena, verzijaID("v-objava")), false, ModulPrijave, nil},
		{"obrisana", verzijaZida(t, e, prijava, verzijaID("v-objava"), arhiviranaVerzija), false, ModulPrijave, nil},
		{"nečitljiva", verzijaZida(t, e, prijava, verzijaID("v-objava"), necitljivaVerzija), false, ModulPrijave, nil},
	})
}

func TestZidOpisiDnevnikCOPa(t *testing.T) {
	p := novaProbaOpisa(t)
	kraj := kadVerzijeZida
	otvoren := models.Journal{ID: "j-cop", Kind: models.JournalKindDefense, CentarSektor: "B", Title: "Dnevnik COP-a Osijek 2026."}
	zakljucen := otvoren
	zakljucen.EndedAt = &kraj
	gradnja := otvoren
	gradnja.Kind = models.JournalKindMaintenanceA02
	cop := func(d *Dogadjaj) { d.Sektor, d.Link = "B", "/dnevnici/j-cop" }
	e := repository.EntityJournals
	provjeriOpise(t, p, []slucajOpisa{
		{"otvoren", verzijaZida(t, e, otvoren), true, ModulDnevnik, func(d *Dogadjaj) {
			cop(d)
			d.Naslov, d.Tekst, d.Vazno = "Otvoren dnevnik COP-a", "Dnevnik COP-a Osijek 2026.", true
		}},
		{"zaključen", verzijaZida(t, e, zakljucen, izmjenaVerzije), true, ModulDnevnik, func(d *Dogadjaj) {
			cop(d)
			d.Naslov, d.Tekst = "Zaključen dnevnik COP-a", "Dnevnik COP-a Osijek 2026."
		}},
		{"izmijenjen", verzijaZida(t, e, otvoren, izmjenaVerzije), false, ModulDnevnik, cop},
		{"nije COP-a", verzijaZida(t, e, gradnja), false, ModulDnevnik, nil},
		{"obrisan", verzijaZida(t, e, otvoren, arhiviranaVerzija), false, ModulDnevnik, nil},
		{"nečitljiv", verzijaZida(t, e, otvoren, necitljivaVerzija), false, ModulDnevnik, nil},
	})
}

func TestZidOpisiDezurstvo(t *testing.T) {
	p := novaProbaOpisa(t)
	z := models.Dezurstvo{ID: "dz-1", JournalID: "j-cop", UserName: "Ana Anić", Podrucje: pint(34), Mjesto: models.MjestoUred,
		Od: time.Date(2026, 10, 20, 8, 0, 0, 0, models.Zagreb), Do: time.Date(2026, 10, 20, 16, 0, 0, 0, models.Zagreb)}
	potvrdeno := z
	potvrdeno.Potvrdio = "Ivo Ivić"
	bezDnevnika := z
	bezDnevnika.JournalID, bezDnevnika.Podrucje, bezDnevnika.Mjesto = "j-nema", nil, models.MjestoTeren
	kad := "20.10. 08:00 – 20.10. 16:00"
	upisano := func(d *Dogadjaj) {
		d.Sektor, d.Tko, d.Link, d.AreaID = "B", "Ana Anić", "/dnevnici/j-cop/dezurstva", 34
	}
	e := repository.EntityDezurstva
	provjeriOpise(t, p, []slucajOpisa{
		{"upisano", verzijaZida(t, e, z), true, ModulDezurstva, func(d *Dogadjaj) {
			upisano(d)
			d.Naslov, d.Tekst = "Upisano dežurstvo", kad+" · ured"
		}},
		{"maknuto", verzijaZida(t, e, z, arhiviranaVerzija), true, ModulDezurstva, func(d *Dogadjaj) {
			upisano(d)
			d.Naslov, d.Tekst = "Maknuto dežurstvo", kad
		}},
		{"potvrđeno", verzijaZida(t, e, potvrdeno, izmjenaVerzije), true, ModulDezurstva, func(d *Dogadjaj) {
			upisano(d)
			d.Naslov, d.Tekst = "Potvrđeno dežurstvo", kad
		}},
		{"izmijenjeno, nije potvrđeno", verzijaZida(t, e, z, izmjenaVerzije), false, ModulDezurstva, upisano},
		// dnevnik obrisan ili još nije stigao: bez sektora centra redak bi
		// vidjeli i drugi sektori, pa ga nema (Odluke (30. 10. 2026.))
		{"nepoznat dnevnik", verzijaZida(t, e, bezDnevnika), false, ModulDezurstva, nil},
		{"nepoznat dnevnik, maknuto", verzijaZida(t, e, bezDnevnika, arhiviranaVerzija), false, ModulDezurstva, nil},
		{"nečitljivo", verzijaZida(t, e, z, necitljivaVerzija), false, ModulDezurstva, nil},
	})
}

func TestZidOpisiDnevnoIzvjesce(t *testing.T) {
	p := novaProbaOpisa(t)
	dan := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	prvo, drugo := kadVerzijeZida.Add(-3*time.Hour), kadVerzijeZida
	nacrt := models.DnevnoIzvjesce{ID: "iz-1", SectionCode: "B.34.1", Dan: dan, Stadij: models.PhaseRegular, Izradio: "Ana Anić",
		Sadrzaj: models.IzvjesceSadrzaj{Vodotok: "Dunav"}}
	predano := nacrt
	predano.PredanoAt = &prvo
	predano.Predao, predano.PredaoAt = "Ivo Ivić", &prvo
	ponovno := predano
	ponovno.PrvoPredanoAt, ponovno.PredanoAt, ponovno.PredaoAt = &prvo, &drugo, &drugo
	bezDionice := nacrt
	bezDionice.SectionCode, bezDionice.Stadij, bezDionice.Sadrzaj.Vodotok = "B.99.9", "", ""
	izvjesce := func(d *Dogadjaj) { d.Sektor, d.AreaID, d.Tko, d.Link = "B", 34, "Ana Anić", "/izvjesca/iz-1" }
	tekst := "B.34.1 · 20.10.2026. · stadij R · Dunav"
	e := repository.EntityDnevnaIzvjesca
	provjeriOpise(t, p, []slucajOpisa{
		{"započeto", verzijaZida(t, e, nacrt), true, ModulIzvjesca, func(d *Dogadjaj) {
			izvjesce(d)
			d.Naslov, d.Tekst = "Započeto dnevno izvješće dionice", tekst
		}},
		{"predano", verzijaZida(t, e, predano, izmjenaVerzije), true, ModulIzvjesca, func(d *Dogadjaj) {
			izvjesce(d)
			d.Naslov, d.Tekst, d.Tko = "Predano dnevno izvješće dionice", tekst, "Ivo Ivić"
		}},
		{"ponovno predano", verzijaZida(t, e, ponovno, izmjenaVerzije), true, ModulIzvjesca, func(d *Dogadjaj) {
			izvjesce(d)
			d.Naslov, d.Tekst, d.Tko = "Ponovno predano dnevno izvješće dionice", tekst, "Ivo Ivić"
		}},
		{"izmijenjen nacrt", verzijaZida(t, e, nacrt, izmjenaVerzije), false, ModulIzvjesca, izvjesce},
		{"nepoznata dionica, bez stadija i vode", verzijaZida(t, e, bezDionice), true, ModulIzvjesca, func(d *Dogadjaj) {
			d.Tko, d.Link = "Ana Anić", "/izvjesca/iz-1"
			d.Naslov, d.Tekst = "Započeto dnevno izvješće dionice", "B.99.9 · 20.10.2026. · stadij –"
		}},
		{"obrisano", verzijaZida(t, e, nacrt, arhiviranaVerzija), false, ModulIzvjesca, nil},
		{"nečitljivo", verzijaZida(t, e, nacrt, necitljivaVerzija), false, ModulIzvjesca, nil},
	})
}

func TestZidOpisiSektorskoIzvjesce(t *testing.T) {
	p := novaProbaOpisa(t)
	dan := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	prvo, drugo := kadVerzijeZida.Add(-3*time.Hour), kadVerzijeZida
	nacrt := models.SektorskoIzvjesce{ID: "sz-1", Sektor: "B", Dan: dan, Izradio: "Ana Anić"}
	nacrt.Sadrzaj.Pregled.Izvjesca = 3
	nacrt.Sadrzaj.Pregled.Vodotoci = []models.VodotokUPregledu{{Vodotok: "Dunav", Stadij: models.PhaseRegular}}
	predano := nacrt
	predano.PredanoAt = &prvo
	ponovno := nacrt
	ponovno.PrvoPredanoAt, ponovno.PredanoAt = &prvo, &drugo
	ponovno.Predao, ponovno.PredaoAt = "Ivo Ivić", &drugo
	jedno := nacrt
	jedno.Sadrzaj.Pregled = models.SektorskiPregled{Izvjesca: 1}
	izvjesce := func(d *Dogadjaj) { d.Sektor, d.Tko, d.Link = "B", "Ana Anić", "/sektorsko-izvjesce/sz-1" }
	tekst := "20.10.2026. · R · 3 izvješća dionica"
	e := repository.EntitySektorskaIzvjesca
	provjeriOpise(t, p, []slucajOpisa{
		{"sastavljeno", verzijaZida(t, e, nacrt), true, ModulIzvjesca, func(d *Dogadjaj) {
			izvjesce(d)
			d.Naslov, d.Tekst = "Sastavljeno izvješće sektora", tekst
		}},
		{"jedno izvješće, bez obrane", verzijaZida(t, e, jedno), true, ModulIzvjesca, func(d *Dogadjaj) {
			izvjesce(d)
			d.Naslov, d.Tekst = "Sastavljeno izvješće sektora", "20.10.2026. · – · 1 izvješće dionice"
		}},
		{"predano", verzijaZida(t, e, predano, izmjenaVerzije), true, ModulIzvjesca, func(d *Dogadjaj) {
			izvjesce(d)
			d.Naslov, d.Tekst, d.Vazno = "Izvješće sektora predano GCOP-u", tekst, true
		}},
		{"ponovno predano", verzijaZida(t, e, ponovno, izmjenaVerzije), true, ModulIzvjesca, func(d *Dogadjaj) {
			izvjesce(d)
			d.Naslov, d.Tekst, d.Vazno, d.Tko = "Izvješće sektora ponovno predano GCOP-u", tekst, true, "Ivo Ivić"
		}},
		{"izmijenjen nacrt", verzijaZida(t, e, nacrt, izmjenaVerzije), false, ModulIzvjesca, izvjesce},
		{"obrisano", verzijaZida(t, e, nacrt, arhiviranaVerzija), false, ModulIzvjesca, nil},
		{"nečitljivo", verzijaZida(t, e, nacrt, necitljivaVerzija), false, ModulIzvjesca, nil},
	})
}

func TestZidOpisiObranu(t *testing.T) {
	p := novaProbaOpisa(t)
	kraj := kadVerzijeZida
	proglasena := models.DefenseEpisode{SectionCode: "B.34.1", Phase: models.PhaseRegular, DeclaredBy: idAne.String()}
	prekinuta := proglasena
	prekinuta.EndedAt, prekinuta.EndedBy = &kraj, idIve.String()
	bezImena := proglasena
	bezImena.DeclaredBy, bezImena.EndedAt = "", &kraj
	nepoznat := proglasena
	nepoznat.SectionCode, nepoznat.DeclaredBy, nepoznat.Phase = "B.99.9", "nije-uuid", models.PhaseEmergency
	obrana := func(tko string) func(d *Dogadjaj) {
		return func(d *Dogadjaj) { d.Sektor, d.AreaID, d.Link, d.Vazno, d.Tko = "B", 34, "/sections/B.34.1", true, tko }
	}
	e := repository.EntityEpisodes
	provjeriOpise(t, p, []slucajOpisa{
		{"proglašena", verzijaZida(t, e, proglasena), true, ModulObrana, func(d *Dogadjaj) {
			obrana("Ana Anić")(d)
			d.Naslov, d.Tekst = "Proglašena obrana", "B.34.1 · Redovna obrana"
		}},
		{"promijenjen stadij", verzijaZida(t, e, proglasena, izmjenaVerzije), true, ModulObrana, func(d *Dogadjaj) {
			obrana("Ana Anić")(d)
			d.Naslov, d.Tekst = "Promijenjen stadij obrane", "B.34.1 · Redovna obrana"
		}},
		{"prekinuta", verzijaZida(t, e, prekinuta, izmjenaVerzije), true, ModulObrana, func(d *Dogadjaj) {
			obrana("Ivo Ivić")(d)
			d.Naslov, d.Tekst = "Prekinuta obrana", "B.34.1 · Redovna obrana"
		}},
		{"prekinuta bez imena", verzijaZida(t, e, bezImena, izmjenaVerzije), true, ModulObrana, func(d *Dogadjaj) {
			obrana("")(d)
			d.Naslov, d.Tekst = "Prekinuta obrana", "B.34.1 · Redovna obrana"
		}},
		{"nepoznata dionica i osoba", verzijaZida(t, e, nepoznat), true, ModulObrana, func(d *Dogadjaj) {
			d.Link, d.Vazno = "/sections/B.99.9", true
			d.Naslov, d.Tekst = "Proglašena obrana", "B.99.9 · Izvanredna obrana"
		}},
		{"obrisana", verzijaZida(t, e, proglasena, arhiviranaVerzija), false, ModulObrana, nil},
		{"nečitljiva", verzijaZida(t, e, proglasena, necitljivaVerzija), false, ModulObrana, nil},
	})
}

func TestZidOpisiPrometSredstava(t *testing.T) {
	p := novaProbaOpisa(t)
	izdano := models.Promet{Vrsta: models.PrometIzdano, Sektor: "B", UserName: "Ana Anić", SkladisteID: "sk-os", VrstaID: "vrece-50x80",
		Oblik: models.OblikPunjeno, Kolicina: -4000, Preuzeo: "vodočuvar Batina", VezaID: "veza-1", AreaID: 51}
	primka := models.Promet{Vrsta: models.PrometPrimka, Sektor: "B", UserName: "Ana Anić", SkladisteID: "sk-os", VrstaID: "vrece-50x80", Kolicina: 12.5}
	naTeren := models.Promet{Vrsta: models.PrometIzdano, Sektor: "B", UserName: "Ana Anić", VrstaID: "vrece-50x80", Kolicina: 4000,
		AreaID: 34, SectionCode: "B.34.1"}
	nestalo := primka
	nestalo.SkladisteID, nestalo.VrstaID = "nestalo", "nepoznata"
	bezPodrucja := primka
	bezPodrucja.SkladisteID = "sk-bez"
	prijenos := models.Promet{Vrsta: models.PrometPrijenos, Sektor: "D", UserName: "Ana Anić", SkladisteID: "sk-os", VrstaID: "vrece-50x80",
		Kolicina: 3, VezaID: "veza-2"}
	popis := primka
	popis.Vrsta = models.PrometPopis
	bezVrste := primka
	bezVrste.Vrsta = ""
	e := repository.EntityMtsPromet
	provjeriOpise(t, p, []slucajOpisa{
		{"izdano sa skladišta", verzijaZida(t, e, izdano), true, ModulSredstva, func(d *Dogadjaj) {
			d.Sektor, d.Tko, d.Naslov, d.AreaID = "B", "Ana Anić", "Izdano na teren", 51
			d.Tekst = "4000 kom Vreće 50x80 cm (napunjeno) · Centralno skladište Osijek · preuzeo vodočuvar Batina"
			d.Link = "/sredstva/promet/veza-1/potvrda.xlsx"
		}},
		{"primka, područje skladišta", verzijaZida(t, e, primka), true, ModulSredstva, func(d *Dogadjaj) {
			d.Sektor, d.Tko, d.Naslov, d.AreaID = "B", "Ana Anić", "Primka", 34
			d.Tekst, d.Link = "12,5 kom Vreće 50x80 cm · Centralno skladište Osijek", "/sredstva/skladista/sk-os"
		}},
		{"skladište bez područja", verzijaZida(t, e, bezPodrucja), true, ModulSredstva, func(d *Dogadjaj) {
			d.Sektor, d.Tko, d.Naslov = "B", "Ana Anić", "Primka"
			d.Tekst, d.Link = "12,5 kom Vreće 50x80 cm · Priručno skladište", "/sredstva/skladista/sk-bez"
		}},
		{"na terenu", verzijaZida(t, e, naTeren), true, ModulSredstva, func(d *Dogadjaj) {
			d.Sektor, d.Tko, d.Naslov, d.AreaID = "B", "Ana Anić", "Izdano na teren", 34
			d.Tekst, d.Link = "4000 kom Vreće 50x80 cm · teren · teren · BP 34 · B.34.1", "/sredstva/na-terenu?sektor=B"
		}},
		{"nepoznato skladište i sredstvo", verzijaZida(t, e, nestalo), true, ModulSredstva, func(d *Dogadjaj) {
			d.Sektor, d.Tko, d.Naslov = "B", "Ana Anić", "Primka"
			d.Tekst, d.Link = "12,5  nepoznata · teren · skladište", "/sredstva/na-terenu?sektor=B"
		}},
		{"prijenos kojem se izlaz ne zna", verzijaZida(t, e, prijenos), true, ModulSredstva, func(d *Dogadjaj) {
			d.Sektor, d.Tko, d.Naslov, d.AreaID = "D", "Ana Anić", "Prijenos", 34
			d.Tekst, d.Link = "3 kom Vreće 50x80 cm · Centralno skladište Osijek", "/sredstva/promet/veza-2/potvrda.xlsx"
		}},
		{"bez vrste prometa nema naslova", verzijaZida(t, e, bezVrste), false, ModulSredstva, func(d *Dogadjaj) {
			d.Sektor, d.Tko, d.AreaID = "B", "Ana Anić", 34
			d.Tekst, d.Link = "12,5 kom Vreće 50x80 cm · Centralno skladište Osijek", "/sredstva/skladista/sk-os"
		}},
		{"usklađenje po inventuri", verzijaZida(t, e, popis), false, ModulSredstva, nil},
		{"izmjena retka", verzijaZida(t, e, primka, izmjenaVerzije), false, ModulSredstva, nil},
		{"obrisan", verzijaZida(t, e, primka, arhiviranaVerzija), false, ModulSredstva, nil},
		{"nečitljiv", verzijaZida(t, e, primka, necitljivaVerzija), false, ModulSredstva, nil},
	})
}

func TestZidOpisiInventuru(t *testing.T) {
	p := novaProbaOpisa(t)
	dan := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	zakljuceno := kadVerzijeZida
	nacrt := models.Popis{ID: "pop-1", Sektor: "B", SkladisteID: "sk-os", Dan: dan, Izradio: "Ana Anić",
		Stavke: []models.PopisnaStavka{{Utvrdjeno: 44, Knjizno: 46}, {Utvrdjeno: 3, Knjizno: 3}}}
	zakljucen := nacrt
	zakljucen.ZakljucenoAt = &zakljuceno
	nestalo := zakljucen
	nestalo.SkladisteID = "nestalo"
	inventura := func(d *Dogadjaj) { d.Sektor, d.Link, d.AreaID = "B", "/sredstva/popisi/pop-1", 34 }
	naDan := "Centralno skladište Osijek · na dan 20.10.2026."
	e := repository.EntityMtsPopisi
	provjeriOpise(t, p, []slucajOpisa{
		{"zaključena", verzijaZida(t, e, zakljucen, izmjenaVerzije), true, ModulSredstva, func(d *Dogadjaj) {
			inventura(d)
			d.Naslov, d.Tko, d.Tekst = "Zaključena inventura", "Ana Anić", naDan+" · 1 redak s razlikom"
		}},
		{"vraćena u nacrt", verzijaZida(t, e, nacrt, zamjenjuje(p.zakljucena)), true, ModulSredstva, func(d *Dogadjaj) {
			inventura(d)
			d.Naslov, d.Tekst = "Inventura vraćena u nacrt", naDan
		}},
		{"nacrt iza nacrta", verzijaZida(t, e, nacrt, zamjenjuje(p.nacrta)), false, ModulSredstva, inventura},
		{"nov nacrt", verzijaZida(t, e, nacrt), false, ModulSredstva, inventura},
		{"nepoznato skladište", verzijaZida(t, e, nestalo), true, ModulSredstva, func(d *Dogadjaj) {
			d.Sektor, d.Link = "B", "/sredstva/popisi/pop-1"
			d.Naslov, d.Tko, d.Tekst = "Zaključena inventura", "Ana Anić", "nestalo · na dan 20.10.2026. · 1 redak s razlikom"
		}},
		{"obrisana", verzijaZida(t, e, zakljucen, arhiviranaVerzija), false, ModulSredstva, nil},
		{"nečitljiva", verzijaZida(t, e, zakljucen, necitljivaVerzija), false, ModulSredstva, nil},
	})
}

func TestZidOpisiPotrebu(t *testing.T) {
	p := novaProbaOpisa(t)
	potreba := models.Potreba{SkladisteID: "sk-os", Godina: 2027, VrstaID: "vrece-50x80", Kolicina: 2000, UserName: "Ana Anić"}
	nestalo := potreba
	nestalo.SkladisteID = "nestalo"
	nula := potreba
	nula.Kolicina = 0
	upisana := func(d *Dogadjaj) {
		d.Sektor, d.AreaID, d.Tko = "B", 34, "Ana Anić"
		d.Link = "/sredstva/skladista/sk-os/potrebe?godina=2027"
		d.Naslov, d.Tekst = "Upisana potreba za nabavom", "2000 kom Vreće 50x80 cm · Centralno skladište Osijek · za 2027."
	}
	e := repository.EntityMtsPotrebe
	provjeriOpise(t, p, []slucajOpisa{
		{"upisana", verzijaZida(t, e, potreba), true, ModulSredstva, upisana},
		{"izmijenjena", verzijaZida(t, e, potreba, izmjenaVerzije), true, ModulSredstva, upisana},
		{"nepoznato skladište", verzijaZida(t, e, nestalo), true, ModulSredstva, func(d *Dogadjaj) {
			d.Tko, d.Link = "Ana Anić", "/sredstva/skladista/nestalo/potrebe?godina=2027"
			d.Naslov, d.Tekst = "Upisana potreba za nabavom", "2000 kom Vreće 50x80 cm · nestalo · za 2027."
		}},
		{"bez količine", verzijaZida(t, e, nula), false, ModulSredstva, nil},
		{"obrisana", verzijaZida(t, e, potreba, arhiviranaVerzija), false, ModulSredstva, nil},
		{"nečitljiva", verzijaZida(t, e, potreba, necitljivaVerzija), false, ModulSredstva, nil},
	})
}

func TestZidOpisiOcitanje(t *testing.T) {
	p := novaProbaOpisa(t)
	mjereno := time.Date(2026, 10, 20, 7, 0, 0, 0, time.UTC)
	ocitanje := func(st uuid.UUID, cm *int, korisnik string) models.Reading {
		return models.Reading{StationID: st.String(), LevelCm: cm, MeasuredAt: mjereno, UserID: korisnik}
	}
	redovna := ocitanje(idPrimjerova, pint(520), idAne.String())
	bezVodostaja := ocitanje(idPrimjerova, nil, idAne.String())
	bezLetve := redovna
	bezLetve.StationID = ""
	e := repository.EntityReadings
	provjeriOpise(t, p, []slucajOpisa{
		{"redovna obrana", verzijaZida(t, e, redovna), true, ModulOcitanja, func(d *Dogadjaj) {
			d.Naslov, d.Tekst = "Očitanje iznad praga", "Primjerovo: +520 cm · Redovna obrana · 20.10. 09:00"
			d.Link, d.Tko, d.Sektor, d.josSektora = "/stations/"+idPrimjerova.String(), "Ana Anić", "P", "Q"
		}},
		{"izvanredna obrana, letva bez praćenja", verzijaZida(t, e, ocitanje(idUzvodne, pint(700), "")), true, ModulOcitanja, func(d *Dogadjaj) {
			d.Naslov, d.Vazno, d.Tekst = "Očitanje iznad praga", true, "Uzvodna: +700 cm · Izvanredna obrana · 20.10. 09:00"
			d.Link = "/stations/" + idUzvodne.String()
		}},
		{"ispod praga", verzijaZida(t, e, ocitanje(idPrimjerova, pint(100), "")), false, ModulOcitanja, nil},
		{"letva bez pragova", verzijaZida(t, e, ocitanje(idBezPragova, pint(900), "")), false, ModulOcitanja, nil},
		{"nepoznata letva", verzijaZida(t, e, ocitanje(uuid.MustParse("0b6f3c3e-2222-4000-8000-0000000000ff"), pint(900), "")), false, ModulOcitanja, nil},
		{"bez vodostaja", verzijaZida(t, e, bezVodostaja), false, ModulOcitanja, nil},
		{"bez letve", verzijaZida(t, e, bezLetve), false, ModulOcitanja, nil},
		{"izmjena", verzijaZida(t, e, redovna, izmjenaVerzije), false, ModulOcitanja, nil},
		{"obrisano", verzijaZida(t, e, redovna, arhiviranaVerzija), false, ModulOcitanja, nil},
		{"nečitljivo", verzijaZida(t, e, redovna, necitljivaVerzija), false, ModulOcitanja, nil},
	})
}

func TestZidOpisiRegistar(t *testing.T) {
	p := novaProbaOpisa(t)
	idPostaje, idObjekta := uuid.MustParse("0b6f3c3e-3333-4000-8000-000000000001"), uuid.MustParse("0b6f3c3e-3333-4000-8000-000000000002")
	dionica := models.Section{Code: "B.34.2", SectorID: "B", AreaID: 34, Description: "Dunav d.o."}
	skladiste := models.Skladiste{ID: "sk-dalj", Sektor: "B", AreaID: 34, Naziv: "Skladište Dalj"}
	postaja := models.Station{ID: idPostaje, Name: "Novoselo"}
	objekt := models.Structure{ID: idObjekta, Name: "CS Draž", SectorID: "B", AreaID: 34}
	var slucajevi []slucajOpisa
	for _, r := range []struct {
		entity  string
		payload any
		redak   func(d *Dogadjaj)
	}{
		{repository.EntitySections, dionica, func(d *Dogadjaj) {
			d.Sektor, d.AreaID, d.Naslov, d.Tekst, d.Link = "B", 34, "Nova dionica u registru", "B.34.2 · Dunav d.o.", "/sections/B.34.2"
		}},
		{repository.EntityMtsSkladista, skladiste, func(d *Dogadjaj) {
			d.Sektor, d.AreaID, d.Naslov, d.Tekst, d.Link = "B", 34, "Novo skladište", "Skladište Dalj", "/sredstva/skladista/sk-dalj"
		}},
		{repository.EntityStations, postaja, func(d *Dogadjaj) {
			d.Naslov, d.Tekst, d.Link = "Nova vodomjerna postaja", "Novoselo", "/stations/"+idPostaje.String()
		}},
		{repository.EntityStructures, objekt, func(d *Dogadjaj) {
			d.Sektor, d.AreaID, d.Naslov, d.Tekst, d.Link = "B", 34, "Novi objekt u registru", "CS Draž", "/structures/"+idObjekta.String()
		}},
	} {
		slucajevi = append(slucajevi,
			slucajOpisa{r.entity + " nov", verzijaZida(t, r.entity, r.payload), true, ModulRegistar, r.redak},
			slucajOpisa{r.entity + " izmjena", verzijaZida(t, r.entity, r.payload, izmjenaVerzije), false, ModulRegistar, nil},
			slucajOpisa{r.entity + " obrisan", verzijaZida(t, r.entity, r.payload, arhiviranaVerzija), false, ModulRegistar, nil},
			slucajOpisa{r.entity + " nečitljiv", verzijaZida(t, r.entity, r.payload, necitljivaVerzija), false, ModulRegistar, nil},
		)
	}
	provjeriOpise(t, p, slucajevi)
}

// Entitet koji zid ne poznaje nije redak, a ni modul ne dobiva
func TestZidOpisiNepoznatEntitet(t *testing.T) {
	p := novaProbaOpisa(t)
	provjeriOpise(t, p, []slucajOpisa{
		{"nepoznat entitet", verzijaZida(t, "nepoznato", map[string]string{"id": "x"}), false, "", nil},
	})
}
