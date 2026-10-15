package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Isti sat letve zna imati dvije bilješke s različitim identitetom (isti sat
// zapisan na dva čvora). Obje su žive u knjizi, a na površini stoji novija
// (mjestoNaTrenutku). Do 13. 10. 2026. brisanje bilješke tog sata davalo je
// spomenik samo bilješci s površine. Druga je ostala živa u knjizi: novi čvor
// koji ju je primio nakon spomenika imao ju je na površini, a izvor ju je
// vratio pri ponovnom pokretanju, iako je korisnik bilješku obrisao.

const letvaBiljeske = "probna-letva"

var satBiljeske = time.Date(2026, 9, 21, 5, 0, 0, 0, time.UTC)

func biljeskaSata(kad time.Time, tekst, tko string) models.ArhivaBiljeska {
	return models.ArhivaBiljeska{Letva: letvaBiljeske, Velicina: "vodostaj", Korak: "satni", Vrijeme: kad, Tekst: tekst, Tko: tko}
}

// biljeskeSata su identiteti bilježaka letve koje površina čvora drži na satu
func biljeskeSata(t *testing.T, c *cvor, kad time.Time) []string {
	t.Helper()
	rows, err := c.db.Query(`SELECT id FROM arhiva_biljeske WHERE letva = ? AND velicina = 'vodostaj' AND korak = 'satni'
		AND vrijeme = ? ORDER BY id`, letvaBiljeske, kad)
	nuzno(t, err)
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		nuzno(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	nuzno(t, rows.Err())
	return ids
}

// biljeskaKojaOstaje je bilješka čvora "ured" koju brisanje sata ne smije dirati
type biljeskaKojaOstaje struct {
	opis string
	b    models.ArhivaBiljeska
}

// izvorObrisaneBiljeske slaže bilješke na čvoru "ured" i briše dvije:
//   - za sat su dvije bilješke, uredova i novija s čvora "teren", pa na uredu
//     na površini stoji terenska, a uredova je samo u knjizi; terenska nosi
//     vrijeme po zagrebačkom satu, a brisanje po UTC-u;
//   - za sat poslije je bilješka koju je ured primio, a na površini je nema;
//   - ostaju bilješke dva sata poslije, druge letve, druge veličine i dnevnog
//     koraka.
//
// Vraća čvor ureda, čvor terena, identitete obrisanih i bilješke koje ostaju.
func izvorObrisaneBiljeske(t *testing.T) (a, teren *cvor, obrisane []string, ostaju []biljeskaKojaOstaje) {
	t.Helper()
	a = noviCvor(t, "ured")
	repo := NewBiljeskaRepository(a.db, a.rec)
	uredova := biljeskaSata(satBiljeske, "očitan maksimum", "Ivo Probić")
	ostaju = []biljeskaKojaOstaje{
		{"dva sata poslije", biljeskaSata(satBiljeske.Add(2*time.Hour), "voda stala", "Ivo Probić")},
		{"druga letva", models.ArhivaBiljeska{Letva: "druga-letva", Velicina: "vodostaj", Korak: "satni",
			Vrijeme: satBiljeske, Tekst: "očitan maksimum", Tko: "Ivo Probić"}},
		{"druga veličina", models.ArhivaBiljeska{Letva: letvaBiljeske, Velicina: "protok", Korak: "satni",
			Vrijeme: satBiljeske, Tekst: "procijenjeno", Tko: "Ivo Probić"}},
		{"dnevni korak", models.ArhivaBiljeska{Letva: letvaBiljeske, Velicina: "vodostaj", Korak: "dnevni",
			Vrijeme: satBiljeske, Tekst: "dnevni srednjak", Tko: "Ivo Probić"}},
	}
	upis := []models.ArhivaBiljeska{uredova}
	for _, o := range ostaju {
		upis = append(upis, o.b)
	}
	_, err := repo.Spremi(ctxRaz, upis)
	nuzno(t, err)
	uredova = upis[0]
	for i := range ostaju {
		ostaju[i].b = upis[i+1]
	}

	teren = noviCvor(t, "teren")
	terenska := []models.ArhivaBiljeska{biljeskaSata(satBiljeske.In(models.Zagreb), "očitan maksimum, s terena", "Ana Probić")}
	_, err = NewBiljeskaRepository(teren.db, teren.rec).Spremi(ctxRaz, terenska)
	nuzno(t, err)
	posalji(t, teren, a)
	if na := biljeskeSata(t, a, satBiljeske); len(na) != 1 || na[0] != terenska[0].ID.String() {
		t.Fatalf("prije brisanja na satu je %v, a treba stajati novija, terenska %s", na, terenska[0].ID)
	}

	// bilješka koju je ured primio, a na površini je nema
	sirota := biljeskaSata(satBiljeske.Add(time.Hour), "primljena, a nije na površini", "Ana Probić")
	sirota.ID = uuid.MustParse("01a0951e-0000-7000-8000-0000000000b1")
	sirota.CreatedAt, sirota.UpdatedAt = satBiljeske, satBiljeske
	_, err = a.rec.Record(ctxRaz, a.db, EntityBiljeske, sirota.ID.String(), sirota)
	nuzno(t, err)

	n, err := repo.Spremi(ctxRaz, []models.ArhivaBiljeska{biljeskaSata(satBiljeske, "", ""),
		biljeskaSata(satBiljeske.Add(time.Hour), "", ""), biljeskaSata(satBiljeske.Add(3*time.Hour), "", "")})
	nuzno(t, err)
	if n != 2 {
		t.Errorf("javljeno je %d obrisanih bilježaka, a obrisane su bilješke dva sata", n)
	}
	return a, teren, []string{uredova.ID.String(), terenska[0].ID.String(), sirota.ID.String()}, ostaju
}

// nemaObrisanih traži da na površini nema bilježaka obrisanih sati, a da su
// bilješke koje ostaju na svom mjestu
func nemaObrisanih(t *testing.T, c *cvor, kada string, ostaju []biljeskaKojaOstaje) {
	t.Helper()
	for _, kad := range []time.Time{satBiljeske, satBiljeske.Add(time.Hour)} {
		if na := biljeskeSata(t, c, kad); len(na) > 0 {
			t.Errorf("%s: %s na satu %s je obrisana bilješka %v", kada, c.ime, kad.Format("15:04"), na)
		}
	}
	for _, o := range ostaju {
		if n := brojRedaka(t, c.db, `SELECT count(*) FROM arhiva_biljeske WHERE id = ?`, o.b.ID.String()); n != 1 {
			t.Errorf("%s: %s nema bilješku koja ostaje (%s)", kada, c.ime, o.opis)
		}
	}
}

// Brisanje bilješke sata daje spomenik svakoj bilješci tog sata koja je u
// knjizi živa, i onoj koje na površini nema, sa zadnjom verzijom bilješke.
// Bilješke drugih sati, letava i veličina ostaju. Izvor ih ne vraća ni pri
// ponovnom pokretanju, a čvor koji je drugu bilješku upisao miče i nju.
func TestBrisanjeBiljeskeBriseSveBiljeskeSata(t *testing.T) {
	sNedovrsenima(t)
	a, teren, obrisane, ostaju := izvorObrisaneBiljeske(t)

	for _, id := range obrisane {
		top, err := a.rec.Latest(ctxRaz, EntityBiljeske, id)
		nuzno(t, err)
		if !top.Archived || top.Channel != "" {
			t.Errorf("bilješka %s: zadnja verzija arhivirana=%v u kanalu %q, a treba spomenik u zajedničkom kanalu", id, top.Archived, top.Channel)
		}
		if b := teret[models.ArhivaBiljeska](t, top.Payload); b.Tekst == "" || b.ID.String() != id {
			t.Errorf("spomenik bilješke %s ne nosi njezinu zadnju verziju: %+v", id, b)
		}
	}
	for _, o := range ostaju {
		if top, err := a.rec.Latest(ctxRaz, EntityBiljeske, o.b.ID.String()); err != nil || top.Archived {
			t.Errorf("bilješka koja ostaje (%s) je arhivirana (%v)", o.opis, err)
		}
	}
	nemaObrisanih(t, a, "nakon brisanja", ostaju)

	o, greska := ponovnoPokretanje(t, a)
	nuzno(t, greska)
	if !praznaObnova(o) {
		t.Errorf("obnova površine pri pokretanju izvora nešto je vratila: %s", o)
	}
	nemaObrisanih(t, a, "nakon ponovnog pokretanja", ostaju)

	posalji(t, a, teren)
	if na := biljeskeSata(t, teren, satBiljeske); len(na) > 0 {
		t.Errorf("čvor koji je bilješku upisao još je ima na satu: %v", na)
	}
}

// Novi čvor ne dobije obrisane bilješke ma kojim redom verzije stigle, ni
// nakon ponovnog pokretanja, i ima istu površinu kao izvor.
func TestObrisanaBiljeskaSataNaNovomCvoru(t *testing.T) {
	sNedovrsenima(t)
	a, _, _, ostaju := izvorObrisaneBiljeske(t)
	sve, err := a.rec.Since(ctxRaz, "", 0)
	nuzno(t, err)

	for _, n := range []nacinDostave{
		{ime: "redom nastanka, po jedna verzija", dostavi: redom(1, false)},
		{ime: "obrnutim redom, po jedna verzija", dostavi: redom(1, true)},
		{ime: "sve u jednom paketu", dostavi: redom(5000, false)},
		{ime: "po autoru i kanalu, po jedna verzija", dostavi: poAutoruIKanalu(1)},
	} {
		t.Run(n.ime, func(t *testing.T) {
			sNedovrsenima(t)
			d := &dostava{b: noviCvor(t, "novi-ured")}
			n.dostavi(t, d, a, sve)
			nemaObrisanih(t, d.b, "nakon dostave", ostaju)
			usporediPovrsine(t, a, d.b)

			o, greska := ponovnoPokretanje(t, d.b)
			nuzno(t, greska)
			if !praznaObnova(o) {
				t.Errorf("obnova površine pri pokretanju nešto je popravila: %s", o)
			}
			nemaObrisanih(t, d.b, "nakon ponovnog pokretanja", ostaju)
			usporediPovrsine(t, a, d.b)
		})
	}
}

// Kad brisanje ne uspije do kraja, bilješka ostaje kakva jest, na površini i
// u knjizi: spomenik uredove bilješke ne ostaje bez spomenika terenske.
func TestBrisanjeBiljeskeNeUspije(t *testing.T) {
	for _, slucaj := range []struct{ ime, kvar string }{
		{"spomenik terenske bilješke ne ide u knjigu", `CREATE TRIGGER kvar BEFORE INSERT ON record_versions
			WHEN NEW.entity = 'arhiva_biljeske' AND NEW.archived = 1
				AND NEW.entity_id IN (SELECT id FROM terenska) BEGIN SELECT RAISE(ABORT, 'kvar'); END`},
		{"površina se ne da obrisati", `CREATE TRIGGER kvar BEFORE DELETE ON arhiva_biljeske
			BEGIN SELECT RAISE(ABORT, 'kvar'); END`},
		{"knjiga se ne da pročitati", `ALTER TABLE record_versions RENAME TO knjiga_negdje`},
	} {
		t.Run(slucaj.ime, func(t *testing.T) {
			a := noviCvor(t, "ured")
			repo := NewBiljeskaRepository(a.db, a.rec)
			uredova := []models.ArhivaBiljeska{biljeskaSata(satBiljeske, "očitan maksimum", "Ivo Probić")}
			_, err := repo.Spremi(ctxRaz, uredova)
			nuzno(t, err)
			teren := noviCvor(t, "teren")
			terenska := []models.ArhivaBiljeska{biljeskaSata(satBiljeske, "s terena", "Ana Probić")}
			_, err = NewBiljeskaRepository(teren.db, teren.rec).Spremi(ctxRaz, terenska)
			nuzno(t, err)
			posalji(t, teren, a)
			_, err = a.db.Exec(`CREATE TABLE terenska (id TEXT)`)
			nuzno(t, err)
			_, err = a.db.Exec(`INSERT INTO terenska VALUES (?)`, terenska[0].ID.String())
			nuzno(t, err)
			_, err = a.db.Exec(slucaj.kvar)
			nuzno(t, err)

			if _, err := repo.Spremi(ctxRaz, []models.ArhivaBiljeska{biljeskaSata(satBiljeske, "", "")}); err == nil {
				t.Fatal("brisanje je javilo uspjeh, a nije prošlo do kraja")
			}
			if na := biljeskeSata(t, a, satBiljeske); len(na) != 1 {
				t.Errorf("na satu je %v, a bilješka je trebala ostati", na)
			}
			if slucaj.ime == "knjiga se ne da pročitati" {
				return
			}
			for _, id := range []string{uredova[0].ID.String(), terenska[0].ID.String()} {
				if top, err := a.rec.Latest(ctxRaz, EntityBiljeske, id); err != nil || top.Archived {
					t.Errorf("bilješka %s je arhivirana iako brisanje nije uspjelo (%v)", id, err)
				}
			}
		})
	}
}

// ---- primjena: kad s trenutka ode zapis koji je na njemu stajao ----

// sBrisanjem je verzija koja briše zapis iz v, s istim sadržajem
func sBrisanjem(v ledger.Version, versionID string) ledger.Version {
	v.VersionID, v.Archived = versionID, true
	return v
}

// provjeriZivogNaTrenutku dostavlja novom čvoru starijeg i novijeg zapisa
// istog trenutka i brisanje novijeg, na više načina. Na trenutku mora ostati
// stariji, jer je on u knjizi živ, i nakon ponovnog pokretanja.
func provjeriZivogNaTrenutku(t *testing.T, tablica string, starija, novija ledger.Version) {
	t.Helper()
	brisanje := sBrisanjem(novija, vid(3))
	for _, slucaj := range []struct {
		ime    string
		paketi [][]ledger.Version
	}{
		{"redom nastanka, po jedna verzija", [][]ledger.Version{{starija}, {novija}, {brisanje}}},
		{"obrnutim redom, po jedna verzija", [][]ledger.Version{{brisanje}, {novija}, {starija}}},
		{"sve u jednom paketu", [][]ledger.Version{{starija, novija, brisanje}}},
		{"oba zapisa, pa brisanje", [][]ledger.Version{{starija, novija}, {brisanje}}},
		{"noviji i brisanje, pa stariji", [][]ledger.Version{{novija, brisanje}, {starija}}},
	} {
		t.Run(slucaj.ime, func(t *testing.T) {
			sNedovrsenima(t)
			c := noviCvor(t, "novi-ured")
			for _, p := range slucaj.paketi {
				nuzno(t, primiPaket(t, c, p...))
			}
			if got := naTrenutku(t, c, tablica); len(got) != 1 || got[0] != starija.EntityID {
				t.Errorf("nakon dostave na trenutku je %v, a treba biti živi %s", got, starija.EntityID)
			}
			o, greska := ponovnoPokretanje(t, c)
			nuzno(t, greska)
			if !praznaObnova(o) {
				t.Errorf("obnova površine pri pokretanju nešto je popravila: %s", o)
			}
			if got := naTrenutku(t, c, tablica); len(got) != 1 || got[0] != starija.EntityID {
				t.Errorf("nakon ponovnog pokretanja na trenutku je %v, a treba biti živi %s", got, starija.EntityID)
			}
		})
	}

	// brisanje zapisa koji nije stajao na trenutku ne dira onoga koji stoji
	t.Run("brisanje starijeg", func(t *testing.T) {
		sNedovrsenima(t)
		c := noviCvor(t, "novi-ured")
		nuzno(t, primiPaket(t, c, starija, novija))
		nuzno(t, primiPaket(t, c, sBrisanjem(starija, vid(3))))
		if got := naTrenutku(t, c, tablica); len(got) != 1 || got[0] != novija.EntityID {
			t.Errorf("na trenutku je %v, a treba ostati noviji %s", got, novija.EntityID)
		}
	})
}

// Bilješka koja stoji na satu obrisana je na čvoru koji drugu bilješku tog
// sata nije imao, ili prije ovog ispravka. Druga je u knjizi živa, pa na sat
// dolazi ona, na svakom čvoru i ma kojim redom verzije stigle, isto kao pri
// ponovnom pokretanju. Prije je čvor koji je primio obje pa brisanje ostajao
// bez ijedne do ponovnog pokretanja, a čvor koji je primio brisanje prije
// starije bilješke imao ju je odmah.
func TestObrisanaBiljeskaSataOstavljaZivu(t *testing.T) {
	ranije := time.Date(2026, 9, 11, 7, 9, 0, 0, time.UTC)
	kasnije := time.Date(2026, 9, 11, 7, 13, 0, 0, time.UTC)
	provjeriZivogNaTrenutku(t, "arhiva_biljeske",
		biljeskaZa(t, "01a08f4c-0000-7000-8000-00000000000f", vid(1), ranije, "s ureda"),
		biljeskaZa(t, "01a08f50-0000-7000-8000-000000000001", vid(2), kasnije, "s terena"))
}

// Od više živih bilješki trenutka dolazi ona koja ima prednost: kasnije
// izmijenjena, ma kojim redom nastale njezine verzije
func TestObrisanaBiljeskaSataOstavljaNajnovijuZivu(t *testing.T) {
	kad := func(min int) time.Time { return time.Date(2026, 9, 11, 7, min, 0, 0, time.UTC) }
	verzije := []ledger.Version{
		biljeskaZa(t, "01a08f50-0000-7000-8000-0000000000d1", vid(1), kad(10), "druga po vremenu"),
		biljeskaZa(t, "01a08f50-0000-7000-8000-0000000000d2", vid(2), kad(20), "treća po vremenu"),
		biljeskaZa(t, "01a08f50-0000-7000-8000-0000000000d3", vid(3), kad(5), "prva po vremenu"),
		biljeskaZa(t, "01a08f50-0000-7000-8000-0000000000d4", vid(4), kad(30), "stoji na satu, briše se"),
	}
	sNedovrsenima(t)
	c := noviCvor(t, "novi-ured")
	nuzno(t, primiPaket(t, c, verzije...))
	nuzno(t, primiPaket(t, c, sBrisanjem(verzije[3], vid(5))))
	if got := naTrenutku(t, c, "arhiva_biljeske"); len(got) != 1 || got[0] != verzije[1].EntityID {
		t.Errorf("na trenutku je %v, a treba biti zadnja izmijenjena od živih, %s", got, verzije[1].EntityID)
	}
}

// Isto vrijedi za ispravak arhive. Ovaj program ispravak ne briše, ali
// spomenik ispravka može stići s novijeg programa.
func TestObrisanIspravakOstavljaZivog(t *testing.T) {
	ranije := time.Date(2026, 9, 11, 7, 9, 0, 0, time.UTC)
	kasnije := time.Date(2026, 9, 11, 7, 13, 0, 0, time.UTC)
	provjeriZivogNaTrenutku(t, "arhiva_ispravci",
		ispravakZa(t, "01a08f4c-0000-7000-8000-00000000000f", vid(1), ranije, 312),
		ispravakZa(t, "01a08f50-0000-7000-8000-000000000001", vid(2), kasnije, 315))
}

// Živi zapisi trenutka: sadržaj koji se ne da pročitati ne stoji ni na jednom
// trenutku, a greška čitanja knjige i tablice vraća se
func TestZiviNaTrenutkuGreske(t *testing.T) {
	c := noviCvor(t, "novi-ured")
	zivi := biljeskaZa(t, "01a08f50-0000-7000-8000-0000000000c1", vid(1), trenutakProbe, "živa")
	krivoVrijeme := ledger.Version{VersionID: vid(2), Entity: EntityBiljeske, EntityID: "01a08f50-0000-7000-8000-0000000000c2",
		NodeID: "ured", Payload: json.RawMessage(`{"letva":"probna","velicina":"vodostaj","korak":"satni","vrijeme":"nije vrijeme"}`)}
	nijeJSON := ledger.Version{VersionID: vid(3), Entity: EntityBiljeske, EntityID: "01a08f50-0000-7000-8000-0000000000c3",
		NodeID: "ured", Payload: json.RawMessage(`{"letva":`)}
	_, err := c.rec.Apply(ctxRaz, []ledger.Version{zivi, krivoVrijeme, nijeJSON})
	nuzno(t, err)

	tx, err := c.db.BeginTx(ctxRaz, nil)
	nuzno(t, err)
	defer tx.Rollback()
	t0 := trenutakNiza{"probna", "vodostaj", "satni", trenutakProbe}
	z, err := ziviNaTrenutku(ctxRaz, tx, EntityBiljeske, t0)
	nuzno(t, err)
	if len(z) != 1 || z[0].id != zivi.EntityID {
		t.Errorf("na trenutku su %+v, a treba biti samo %s", z, zivi.EntityID)
	}
	if z, err := ziviNaTrenutku(ctxRaz, tx, EntityBiljeske, trenutakNiza{"probna", "vodostaj", "satni", time.Time{}}); err != nil || len(z) != 0 {
		t.Errorf("na trenutku bez vremena su %+v (%v), a sadržaj koji se ne da pročitati ne stoji nigdje", z, err)
	}
	if err := makniSTrenutka(ctxRaz, tx, "nema_tablice", sBrisanjem(zivi, vid(4)), primijeniBiljesku); err == nil {
		t.Error("brisanje iz tablice koje nema prošlo je bez greške")
	}
	nuzno(t, primijeniBiljesku(ctxRaz, tx, zivi.Payload))
	_, err = tx.ExecContext(ctxRaz, `DROP TABLE record_versions`)
	nuzno(t, err)
	if _, err := ziviNaTrenutku(ctxRaz, tx, EntityBiljeske, t0); err == nil {
		t.Error("čitanje knjige koje nema prošlo je bez greške")
	}
	if err := makniSTrenutka(ctxRaz, tx, "arhiva_biljeske", sBrisanjem(zivi, vid(4)), primijeniBiljesku); err == nil {
		t.Error("brisanje s trenutka prošlo je iako se knjiga nije dala pročitati")
	}
}
