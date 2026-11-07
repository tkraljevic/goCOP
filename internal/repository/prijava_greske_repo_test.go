package repository

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/sadrzaj"
)

func novaPrijavaGreske(userID string) *models.PrijavaGreske {
	return &models.PrijavaGreske{UserID: userID, Ime: "Mara Marić", Uloga: "vodočuvar", Vrsta: models.VrstaPrijaveGreska,
		Opis: "Graf se ne crta.", Stranica: "/prognoze?sektor=B", NazivStranice: "Prognoze · Sektor B", Izdanje: "0.0.54-alfa",
		Cvor: "teren-1", Sustav: "Windows", Preglednik: "Chrome 142", Zaslon: "1440 × 900", Greske: []string{"17:41:08 Greška"},
		Stanje: models.StanjePrijaveNova}
}

// Prijava nastane na jednom računalu, administrator odgovori na drugom, a
// osoba odgovor vidi na prvom. Sve ide zajedničkim kanalom, s oznakom UUIDv7.
func TestPrijavaGreskeIOdgovorPutujuRazmjenom(t *testing.T) {
	teren, ured := noviCvor(t, "teren"), noviCvor(t, "ured")
	rt, ru := NewPrijavaGreskeRepository(teren.db, teren.rec), NewPrijavaGreskeRepository(ured.db, ured.rec)

	p := novaPrijavaGreske("osoba-1")
	nuzno(t, rt.Save(ctxRaz, p))
	if len(p.ID) != 36 || p.ID[14] != '7' {
		t.Fatalf("oznaka nije UUIDv7: %q", p.ID)
	}
	verzije, err := teren.rec.Since(ctxRaz, "", 0)
	nuzno(t, err)
	var kanal string
	nasao := false
	for _, v := range verzije {
		if v.Entity == EntityPrijaveGresaka {
			kanal, nasao = v.Channel, true
		}
	}
	if !nasao || kanal != "" {
		t.Fatalf("verzija u knjizi: nađena %v, kanal %q", nasao, kanal)
	}

	posalji(t, teren, ured)
	stigla, err := ru.Get(ctxRaz, p.ID)
	nuzno(t, err)
	if stigla == nil || stigla.Opis != p.Opis || stigla.Stanje != models.StanjePrijaveNova || len(stigla.Greske) != 1 {
		t.Fatalf("na uredu: %+v", stigla)
	}

	odgovorena, err := ru.Odgovori(ctxRaz, p.ID, models.StanjePrijaveRijeseno, "Popravljeno.", "Ivo Ivić")
	nuzno(t, err)
	if len(odgovorena.Odgovori) != 1 || odgovorena.Odgovori[0].Odgovorio != "Ivo Ivić" {
		t.Fatalf("odgovor: %+v", odgovorena)
	}
	posalji(t, ured, teren)
	vidi, err := rt.PoKorisniku(ctxRaz, "osoba-1", 10)
	nuzno(t, err)
	if len(vidi) != 1 || tekstovi(&vidi[0]) != "Popravljeno." || vidi[0].Stanje != models.StanjePrijaveRijeseno {
		t.Fatalf("na terenu: %+v", vidi)
	}
	if vidi[0].Opis != p.Opis || vidi[0].Stranica != p.Stranica {
		t.Errorf("odgovor je promijenio prijavu: %+v", vidi[0])
	}

	// starija verzija koja stigne kasno ne vraća stanje unatrag
	stare, err := teren.rec.Since(ctxRaz, "", 0)
	nuzno(t, err)
	var prva []ledger.Version
	for _, v := range stare {
		if v.Entity == EntityPrijaveGresaka {
			prva = append(prva, v)
			break
		}
	}
	primi(t, teren, prva)
	nakon, err := rt.Get(ctxRaz, p.ID)
	nuzno(t, err)
	if nakon.Stanje != models.StanjePrijaveRijeseno || tekstovi(nakon) != "Popravljeno." {
		t.Errorf("stara verzija pregazila stanje ili odgovor: %+v", nakon)
	}
}

func tekstovi(p *models.PrijavaGreske) string {
	var t []string
	for _, o := range p.Odgovori {
		t = append(t, o.Tekst)
	}
	return strings.Join(t, " | ")
}

// Dva administratora rade na istoj prijavi, a računala se još nisu razmijenila.
// Promjena samo stanja na drugom računalu ne smije izbrisati odgovor s prvog,
// ni dva odgovora jedan drugoga, kojim god redom stigli.
func TestPrijavaGreskeIstodobniOdgovoriOstaju(t *testing.T) {
	for _, redom := range []string{"prvo ured", "prvo teren"} {
		t.Run(redom, func(t *testing.T) {
			teren, ured := noviCvor(t, "teren"), noviCvor(t, "ured")
			rt, ru := NewPrijavaGreskeRepository(teren.db, teren.rec), NewPrijavaGreskeRepository(ured.db, ured.rec)
			p := novaPrijavaGreske("osoba-1")
			nuzno(t, rt.Save(ctxRaz, p))
			posalji(t, teren, ured)

			_, err := ru.Odgovori(ctxRaz, p.ID, models.StanjePrijaveRijeseno, "Popravljeno u 0.0.55.", "Ivo Ivić")
			nuzno(t, err)
			_, err = rt.Odgovori(ctxRaz, p.ID, models.StanjePrijaveURadu, "", "Ana Anić")
			nuzno(t, err)
			if redom == "prvo ured" {
				posalji(t, ured, teren)
				posalji(t, teren, ured)
			} else {
				posalji(t, teren, ured)
				posalji(t, ured, teren)
			}
			na, err := rt.Get(ctxRaz, p.ID)
			nuzno(t, err)
			nb, err := ru.Get(ctxRaz, p.ID)
			nuzno(t, err)
			if tekstovi(na) != "Popravljeno u 0.0.55." || tekstovi(nb) != "Popravljeno u 0.0.55." {
				t.Errorf("odgovor nestao: teren %q, ured %q", tekstovi(na), tekstovi(nb))
			}
			if na.Stanje != nb.Stanje {
				t.Errorf("stanje se razlikuje: teren %q, ured %q", na.Stanje, nb.Stanje)
			}

			_, err = ru.Odgovori(ctxRaz, p.ID, nb.Stanje, "Još jedna napomena.", "Ivo Ivić")
			nuzno(t, err)
			_, err = rt.Odgovori(ctxRaz, p.ID, na.Stanje, "Provjerite ponovno.", "Ana Anić")
			nuzno(t, err)
			posalji(t, ured, teren)
			posalji(t, teren, ured)
			for _, r := range []*PrijavaGreskeRepository{rt, ru} {
				x, err := r.Get(ctxRaz, p.ID)
				nuzno(t, err)
				if len(x.Odgovori) != 3 {
					t.Errorf("trebaju tri odgovora: %q", tekstovi(x))
				}
			}
		})
	}
}

// Odgovor smije stići prije prijave; pokaže se čim prijava stigne
func TestPrijavaGreskeOdgovorPrijePrijave(t *testing.T) {
	teren, ured := noviCvor(t, "teren"), noviCvor(t, "ured")
	rt, ru := NewPrijavaGreskeRepository(teren.db, teren.rec), NewPrijavaGreskeRepository(ured.db, ured.rec)
	p := novaPrijavaGreske("osoba-1")
	nuzno(t, rt.Save(ctxRaz, p))
	_, err := rt.Odgovori(ctxRaz, p.ID, p.Stanje, "Primljeno.", "Ivo Ivić")
	nuzno(t, err)
	sve, err := teren.rec.Since(ctxRaz, "", 0)
	nuzno(t, err)
	var prijave, odgovori []ledger.Version
	for _, v := range sve {
		switch v.Entity {
		case EntityPrijaveGresaka:
			prijave = append(prijave, v)
		case EntityPrijaveGresakaOdgovori:
			odgovori = append(odgovori, v)
		}
	}
	primi(t, ured, odgovori)
	primi(t, ured, prijave)
	x, err := ru.Get(ctxRaz, p.ID)
	nuzno(t, err)
	if x == nil || tekstovi(x) != "Primljeno." {
		t.Errorf("%+v", x)
	}
}

// Stanje s novijeg računala koje ovaj program ne zna vodi se kao nova prijava
func TestPrijavaGreskeNepoznatoStanje(t *testing.T) {
	c := noviCvor(t, "ured")
	r := NewPrijavaGreskeRepository(c.db, c.rec)
	p := novaPrijavaGreske("a")
	p.Stanje = "odbijeno"
	nuzno(t, r.Save(ctxRaz, p))
	nuzno(t, r.Save(ctxRaz, novaPrijavaGreske("b")))
	broj, err := r.BrojPoStanju(ctxRaz)
	nuzno(t, err)
	if broj[models.StanjePrijaveNova] != 2 || broj["odbijeno"] != 0 {
		t.Errorf("po stanju: %v", broj)
	}
	nove, err := r.Popis(ctxRaz, models.StanjePrijaveNova)
	nuzno(t, err)
	if len(nove) != 2 {
		t.Errorf("nove: %d", len(nove))
	}
	x, err := r.Get(ctxRaz, p.ID)
	nuzno(t, err)
	if x.Stanje != "odbijeno" {
		t.Errorf("čitanje smije samo prikazati drukčije, a ne promijeniti zapis: %q", x.Stanje)
	}
}

func TestPrijavaGreskePopisiIBrojevi(t *testing.T) {
	c := noviCvor(t, "ured")
	r := NewPrijavaGreskeRepository(c.db, c.rec)
	if p, err := r.Get(ctxRaz, "nema"); err != nil || p != nil {
		t.Errorf("Get nepostojeće: %+v %v", p, err)
	}
	if p, err := r.Odgovori(ctxRaz, "nema", models.StanjePrijaveURadu, "", "x"); err != nil || p != nil {
		t.Errorf("Odgovori nepostojećoj: %+v %v", p, err)
	}
	prva, druga, treca := novaPrijavaGreske("a"), novaPrijavaGreske("a"), novaPrijavaGreske("b")
	for _, p := range []*models.PrijavaGreske{prva, druga, treca} {
		nuzno(t, r.Save(ctxRaz, p))
	}
	_, err := r.Odgovori(ctxRaz, treca.ID, models.StanjePrijaveURadu, "", "Ivo Ivić")
	nuzno(t, err)

	sve, err := r.Popis(ctxRaz, "")
	nuzno(t, err)
	if len(sve) != 3 || sve[0].ID != treca.ID || sve[2].ID != prva.ID {
		t.Errorf("popis, najnovije prve: %d", len(sve))
	}
	nove, err := r.Popis(ctxRaz, models.StanjePrijaveNova)
	nuzno(t, err)
	if len(nove) != 2 {
		t.Errorf("nove: %d", len(nove))
	}
	broj, err := r.BrojPoStanju(ctxRaz)
	nuzno(t, err)
	if broj[models.StanjePrijaveNova] != 2 || broj[models.StanjePrijaveURadu] != 1 {
		t.Errorf("po stanju: %v", broj)
	}
	mojih, err := r.PoKorisniku(ctxRaz, "a", 1)
	nuzno(t, err)
	if len(mojih) != 1 || mojih[0].ID != druga.ID {
		t.Errorf("zadnja osobe: %+v", mojih)
	}
	if n, err := r.RedniBroj(ctxRaz, druga.ID); err != nil || n != 2 {
		t.Errorf("redni broj: %d %v", n, err)
	}
	vremena, err := r.ZadnjaVremena(ctxRaz, "a", 5)
	nuzno(t, err)
	if len(vremena) != 2 || vremena[0].Before(vremena[1]) {
		t.Errorf("vremena: %v", vremena)
	}

	// isto stanje bez odgovora ne pravi novu verziju, a isti tekst ne upisuje se dvaput
	prije, err := c.rec.Since(ctxRaz, "", 0)
	nuzno(t, err)
	_, err = r.Odgovori(ctxRaz, treca.ID, models.StanjePrijaveURadu, "", "Ana Anić")
	nuzno(t, err)
	poslije, err := c.rec.Since(ctxRaz, "", 0)
	nuzno(t, err)
	if len(poslije) != len(prije) {
		t.Errorf("isti odgovor: %d verzija, a bilo %d", len(poslije), len(prije))
	}
	nakon, err := r.Get(ctxRaz, treca.ID)
	nuzno(t, err)
	if len(nakon.Odgovori) != 0 {
		t.Errorf("samo stanje, a upisan odgovor: %+v", nakon.Odgovori)
	}
	for range 2 {
		_, err = r.Odgovori(ctxRaz, treca.ID, models.StanjePrijaveURadu, "Gledamo.", "Ana Anić")
		nuzno(t, err)
	}
	nakon, err = r.Get(ctxRaz, treca.ID)
	nuzno(t, err)
	if tekstovi(nakon) != "Gledamo." {
		t.Errorf("ponovljeno slanje: %q", tekstovi(nakon))
	}

	// ista prijava iste osobe nalazi se samo unutar roka
	ista, err := r.IstaNedavna(ctxRaz, "a", prva.Vrsta, prva.Opis, prva.Stranica, "", time.Now().Add(-time.Minute))
	nuzno(t, err)
	if ista == nil || ista.ID != druga.ID {
		t.Errorf("ista nedavna: %+v", ista)
	}
	if ista, err := r.IstaNedavna(ctxRaz, "a", prva.Vrsta, "drugi tekst", prva.Stranica, "", time.Now().Add(-time.Minute)); err != nil || ista != nil {
		t.Errorf("drugi tekst: %+v %v", ista, err)
	}
	if ista, err := r.IstaNedavna(ctxRaz, "a", prva.Vrsta, prva.Opis, prva.Stranica, "", time.Now().Add(time.Minute)); err != nil || ista != nil {
		t.Errorf("izvan roka: %+v %v", ista, err)
	}
}

// Kvar baze vraća grešku pozivatelju, a ne pola posla: rad na stranici ide
// dalje jer sloj iznad grešku pokaže kao poruku
func TestPrijavaGreskeKvarBaze(t *testing.T) {
	ctx := ctxRaz
	t.Run("nema tablice odgovora", func(t *testing.T) {
		c := noviCvor(t, "ured")
		r := NewPrijavaGreskeRepository(c.db, c.rec)
		p := novaPrijavaGreske("a")
		nuzno(t, r.Save(ctx, p))
		_, err := c.db.Exec(`DROP TABLE prijave_gresaka_odgovori`)
		nuzno(t, err)
		if _, err := r.Get(ctx, p.ID); err == nil {
			t.Error("Get")
		}
		if _, err := r.PoKorisniku(ctx, "a", 5); err == nil {
			t.Error("PoKorisniku")
		}
		tx, err := c.db.Begin()
		nuzno(t, err)
		defer tx.Rollback()
		if err := r.upisiOdgovor(ctx, tx, &models.OdgovorNaPrijavu{ID: "x", PrijavaID: p.ID, Tekst: "t"}); err == nil {
			t.Error("upisiOdgovor")
		}
	})
	t.Run("nije upisan odgovor ni stanje", func(t *testing.T) {
		c := noviCvor(t, "ured")
		r := NewPrijavaGreskeRepository(c.db, c.rec)
		p := novaPrijavaGreske("a")
		nuzno(t, r.Save(ctx, p))
		_, err := c.db.Exec(`CREATE TRIGGER bez_odgovora BEFORE INSERT ON prijave_gresaka_odgovori BEGIN SELECT RAISE(ABORT, 'kvar'); END`)
		nuzno(t, err)
		if _, err := r.Odgovori(ctx, p.ID, models.StanjePrijaveRijeseno, "Tekst", "Ivo Ivić"); err == nil {
			t.Fatal("Odgovori bez greške")
		}
		x, err := r.Get(ctx, p.ID)
		nuzno(t, err)
		if x.Stanje != models.StanjePrijaveNova {
			t.Errorf("stanje upisano iako odgovor nije: %q", x.Stanje)
		}
		_, err = c.db.Exec(`CREATE TRIGGER bez_prijave BEFORE UPDATE ON prijave_gresaka BEGIN SELECT RAISE(ABORT, 'kvar'); END`)
		nuzno(t, err)
		if _, err := r.Odgovori(ctx, p.ID, models.StanjePrijaveURadu, "", "Ivo Ivić"); err == nil {
			t.Error("promjena stanja bez greške")
		}
		if err := r.Save(ctx, x); err == nil {
			t.Error("Save bez greške")
		}
	})
	t.Run("zatvorena baza", func(t *testing.T) {
		c := noviCvor(t, "ured")
		r := NewPrijavaGreskeRepository(c.db, c.rec)
		p := novaPrijavaGreske("a")
		nuzno(t, r.Save(ctx, p))
		stari := &models.PrijavaGreske{ID: p.ID, Stanje: models.StanjePrijaveNova}
		nuzno(t, c.db.Close())
		if err := r.Save(ctx, novaPrijavaGreske("a")); err == nil {
			t.Error("Save")
		}
		if err := r.upisiStanjeIOdgovor(ctx, stari, models.StanjePrijaveURadu, nil); err == nil {
			t.Error("upisiStanjeIOdgovor")
		}
		if _, err := r.Odgovori(ctx, p.ID, models.StanjePrijaveURadu, "", "x"); err == nil {
			t.Error("Odgovori")
		}
		if _, err := r.Popis(ctx, ""); err == nil {
			t.Error("Popis")
		}
		if _, err := r.PoKorisniku(ctx, "a", 5); err == nil {
			t.Error("PoKorisniku")
		}
		if _, err := r.BrojPoStanju(ctx); err == nil {
			t.Error("BrojPoStanju")
		}
		if _, err := r.ZadnjaVremena(ctx, "a", 5); err == nil {
			t.Error("ZadnjaVremena")
		}
		if _, err := r.IstaNedavna(ctx, "a", "v", "o", "s", "", time.Now()); err == nil {
			t.Error("IstaNedavna")
		}
		if _, err := r.citajOdgovore(ctx, `SELECT 1`); err == nil {
			t.Error("citajOdgovore")
		}
	})
}

func TestPrijavaGreskePopisPoStanju(t *testing.T) {
	c := noviCvor(t, "ured")
	r := NewPrijavaGreskeRepository(c.db, c.rec)
	p := novaPrijavaGreske("a")
	nuzno(t, r.Save(ctxRaz, p))
	_, err := r.Odgovori(ctxRaz, p.ID, models.StanjePrijaveURadu, "", "x")
	nuzno(t, err)
	if u, err := r.Popis(ctxRaz, models.StanjePrijaveURadu); err != nil || len(u) != 1 {
		t.Errorf("u radu: %d %v", len(u), err)
	}
	if u, err := r.Popis(ctxRaz, models.StanjePrijaveNova); err != nil || len(u) != 0 {
		t.Errorf("nove: %d %v", len(u), err)
	}
}

// spremisteZaTest otvara spremište sadržaja; globalno je, pa ga dva čvora u testu
// naizmjence postavljaju, a na kraju se vraća prijašnje
func spremisteZaTest(t *testing.T, ime string) *sadrzaj.Spremiste {
	t.Helper()
	prije := Spremiste()
	t.Cleanup(func() { SetSpremiste(prije) })
	sp, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), ime+".db"))
	nuzno(t, err)
	t.Cleanup(func() { nuzno(t, sp.Zatvori()) })
	return sp
}

// Slika nastane uz prijavu na jednom računalu. Drugo prvo dobije prijavu i upiše
// sliku na popis za dohvat, a kad bajtovi stignu, slika je tu. Promjena stanja
// na drugom računalu ne gubi opis slike.
func TestPrijavaGreskeSlikaPutujePoOtisku(t *testing.T) {
	spT, spU := spremisteZaTest(t, "teren"), spremisteZaTest(t, "ured")
	teren, ured := noviCvor(t, "teren"), noviCvor(t, "ured")
	rt, ru := NewPrijavaGreskeRepository(teren.db, teren.rec), NewPrijavaGreskeRepository(ured.db, ured.rec)

	slika := []byte("bajtovi slike zaslona")
	otisak := sadrzaj.Otisak(slika)
	p := novaPrijavaGreske("osoba-1")
	p.SlikaOtisak, p.SlikaBajtova, p.SlikaVrsta = otisak, len(slika), models.SlikaPrijavePNG
	SetSpremiste(spT)
	nuzno(t, rt.SaveSaSlikom(ctxRaz, p, slika))
	if string(rt.CitajSliku(ctxRaz, otisak)) != string(slika) || !rt.ImaSliku(ctxRaz, otisak) {
		t.Fatal("slika nije spremljena uz prijavu")
	}
	verzije, err := teren.rec.Since(ctxRaz, "", 0)
	nuzno(t, err)
	for _, v := range verzije {
		if strings.Contains(string(v.Payload), "bajtovi slike") {
			t.Fatalf("bajtovi su u knjizi: %s", v.Entity)
		}
	}

	SetSpremiste(spU)
	posalji(t, teren, ured)
	stigla, err := ru.Get(ctxRaz, p.ID)
	nuzno(t, err)
	if stigla.SlikaOtisak != otisak || stigla.SlikaVrsta != models.SlikaPrijavePNG || stigla.SlikaBajtova != len(slika) {
		t.Fatalf("opis slike: %+v", stigla)
	}
	if ru.ImaSliku(ctxRaz, otisak) || !spU.Zeljen(ctxRaz, otisak) {
		t.Fatal("slika mora čekati na popisu za dohvat")
	}
	veza := sadrzaj.Veza{Entitet: EntityPrijaveGresaka, EntitetID: p.ID, Uloga: ulogaSlikePrijave}
	if !spU.ImaVezu(ctxRaz, otisak, veza) {
		t.Error("prijava ne drži sliku živom")
	}
	nuzno(t, spU.UpisiProvjereno(ctxRaz, otisak, models.SlikaPrijavePNG, slika, "cvor:teren"))
	if !ru.ImaSliku(ctxRaz, otisak) || spU.Zeljen(ctxRaz, otisak) {
		t.Fatal("slika je stigla, a želja je ostala")
	}

	// administrator na uredu mijenja stanje; slika ostaje u novoj verziji
	_, err = ru.Odgovori(ctxRaz, p.ID, models.StanjePrijaveURadu, "", "Ivo Ivić")
	nuzno(t, err)
	SetSpremiste(spT)
	posalji(t, ured, teren)
	vidi, err := rt.Get(ctxRaz, p.ID)
	nuzno(t, err)
	if vidi.Stanje != models.StanjePrijaveURadu || vidi.SlikaOtisak != otisak {
		t.Errorf("nakon promjene stanja: %+v", vidi)
	}
}

// Prijava bez slike ne stvara ni želju ni vezu
func TestPrijavaGreskeBezSlikeNistaNeTrazi(t *testing.T) {
	sp := spremisteZaTest(t, "ured")
	SetSpremiste(sp)
	teren, ured := noviCvor(t, "teren"), noviCvor(t, "ured")
	nuzno(t, NewPrijavaGreskeRepository(teren.db, teren.rec).Save(ctxRaz, novaPrijavaGreske("a")))
	posalji(t, teren, ured)
	if z, err := sp.Zeljeni(ctxRaz, 10); err != nil || len(z) != 0 {
		t.Errorf("želje: %+v %v", z, err)
	}
}

// Drugo računalo ne može naložiti dohvat nečega što nije slika zaslona: prijava
// stigne, ali bez slike
func TestPrijavaGreskeKrivOpisSlike(t *testing.T) {
	sp := spremisteZaTest(t, "ured")
	SetSpremiste(sp)
	ured := noviCvor(t, "ured")
	ru := NewPrijavaGreskeRepository(ured.db, ured.rec)
	dobar := strings.Repeat("a1", 32)
	for i, opis := range []struct {
		otisak, vrsta string
		bajtova       int
	}{
		{dobar, "image/svg+xml", 10},
		{dobar, "application/pdf", 10},
		{"../../tajna", models.SlikaPrijavePNG, 10},
		{dobar, models.SlikaPrijavePNG, models.NajvecaSlikaPrijave + 1},
		{dobar, models.SlikaPrijavePNG, -1},
	} {
		p := novaPrijavaGreske("a")
		p.ID = uuid.Must(uuid.NewV7()).String()
		p.CreatedAt, p.UpdatedAt = time.Now().UTC(), time.Now().UTC()
		p.SlikaOtisak, p.SlikaVrsta, p.SlikaBajtova = opis.otisak, opis.vrsta, opis.bajtova
		teret, err := json.Marshal(p)
		nuzno(t, err)
		primi(t, ured, []ledger.Version{{VersionID: uuid.Must(uuid.NewV7()).String(), NodeID: "teren", Entity: EntityPrijaveGresaka, EntityID: p.ID, Payload: teret, CreatedAt: time.Now().UTC()}})
		stigla, err := ru.Get(ctxRaz, p.ID)
		nuzno(t, err)
		if stigla == nil || stigla.SlikaOtisak != "" || stigla.SlikaVrsta != "" {
			t.Errorf("%d: %+v", i, stigla)
		}
	}
	if z, err := sp.Zeljeni(ctxRaz, 10); err != nil || len(z) != 0 {
		t.Errorf("želje: %+v %v", z, err)
	}
}

// Zapis o poslanoj poruci ostaje na računalu koje je slalo: ne stvara verziju i
// ne putuje, pa drugo računalo ne pokazuje poruku koju nije slalo
func TestPrijavaGreskePorukaOstajeNaRacunalu(t *testing.T) {
	teren, ured := noviCvor(t, "teren"), noviCvor(t, "ured")
	rt, ru := NewPrijavaGreskeRepository(teren.db, teren.rec), NewPrijavaGreskeRepository(ured.db, ured.rec)
	p := novaPrijavaGreske("a")
	nuzno(t, rt.Save(ctxRaz, p))
	prije, err := teren.rec.Since(ctxRaz, "", 0)
	nuzno(t, err)

	kad := time.Now()
	nuzno(t, rt.ZabiljeziPoruku(ctxRaz, p.ID, kad))
	nuzno(t, rt.ZabiljeziPoruku(ctxRaz, p.ID, kad.Add(time.Hour))) // ostaje prvi zapis
	nakon, err := teren.rec.Since(ctxRaz, "", 0)
	nuzno(t, err)
	if len(nakon) != len(prije) {
		t.Errorf("zapis poruke je stvorio verziju: %d → %d", len(prije), len(nakon))
	}
	vidi, err := rt.Get(ctxRaz, p.ID)
	nuzno(t, err)
	if vidi.PorukaPoslana == nil || !vidi.PorukaPoslana.Equal(kad.UTC().Truncate(0)) && vidi.PorukaPoslana.Sub(kad).Abs() > time.Second {
		t.Errorf("poslano: %v", vidi.PorukaPoslana)
	}
	if n, err := rt.BrojPorukaOd(ctxRaz, kad.Add(-time.Minute)); err != nil || n != 1 {
		t.Errorf("u satu: %d %v", n, err)
	}
	if n, err := rt.BrojPorukaOd(ctxRaz, kad.Add(time.Minute)); err != nil || n != 0 {
		t.Errorf("nakon: %d %v", n, err)
	}

	posalji(t, teren, ured)
	stigla, err := ru.Get(ctxRaz, p.ID)
	nuzno(t, err)
	if stigla.PorukaPoslana != nil {
		t.Errorf("poruka je otputovala: %v", stigla.PorukaPoslana)
	}
}

// Bez spremišta ni bez bajtova nema tihog spremanja slike
func TestPrijavaGreskeSlikaBezSpremista(t *testing.T) {
	prije := Spremiste()
	t.Cleanup(func() { SetSpremiste(prije) })
	SetSpremiste(nil)
	c := noviCvor(t, "teren")
	r := NewPrijavaGreskeRepository(c.db, c.rec)
	p := novaPrijavaGreske("a")
	p.SlikaOtisak, p.SlikaBajtova, p.SlikaVrsta = sadrzaj.Otisak([]byte("x")), 1, models.SlikaPrijavePNG
	if err := r.SaveSaSlikom(ctxRaz, p, []byte("x")); err == nil {
		t.Error("bez spremišta mora biti greška")
	}
	if r.ImaSliku(ctxRaz, p.SlikaOtisak) || len(r.CitajSliku(ctxRaz, p.SlikaOtisak)) != 0 {
		t.Error("bez spremišta slike nema")
	}
	sp := spremisteZaTest(t, "s")
	SetSpremiste(sp)
	q := novaPrijavaGreske("a")
	q.SlikaOtisak, q.SlikaBajtova, q.SlikaVrsta = strings.Repeat("a1", 32), 1, models.SlikaPrijavePNG
	if err := r.SaveSaSlikom(ctxRaz, q, []byte("x")); err == nil {
		t.Error("otisak se ne slaže")
	}
}

// Primjena s neispravnim zapisom, spremištem koje ne radi ili bazom koja ne prima
// javlja grešku, pa zapis čeka sljedeći pokušaj
func TestPrimjenaPrijaveGreskeKvarovi(t *testing.T) {
	c := noviCvor(t, "ured")
	tx, err := c.db.Begin()
	nuzno(t, err)
	if err := primijeniPrijavuGreske(ctxRaz, tx, ledger.Version{Payload: []byte("{")}); err == nil {
		t.Error("neispravan zapis")
	}
	nuzno(t, tx.Rollback())

	p := novaPrijavaGreske("a")
	p.ID = uuid.Must(uuid.NewV7()).String()
	p.SlikaOtisak, p.SlikaBajtova, p.SlikaVrsta = strings.Repeat("a1", 32), 10, models.SlikaPrijavePNG
	teret, err := json.Marshal(p)
	nuzno(t, err)
	// transakcija je već gotova, pa upis ne prolazi
	if err := primijeniPrijavuGreske(ctxRaz, tx, ledger.Version{Payload: teret}); err == nil {
		t.Error("upis u gotovu transakciju")
	}

	sp, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "s.db"))
	nuzno(t, err)
	prije := Spremiste()
	t.Cleanup(func() { SetSpremiste(prije) })
	SetSpremiste(sp)
	nuzno(t, sp.Zatvori())
	tx, err = c.db.Begin()
	nuzno(t, err)
	defer func() { nuzno(t, tx.Rollback()) }()
	if err := primijeniPrijavuGreske(ctxRaz, tx, ledger.Version{Payload: teret}); err == nil {
		t.Error("spremište ne radi")
	}
}

// Broje se samo prijave sa slikom, najnovije prve
func TestZadnjaVremenaSlika(t *testing.T) {
	sp := spremisteZaTest(t, "s")
	SetSpremiste(sp)
	c := noviCvor(t, "teren")
	r := NewPrijavaGreskeRepository(c.db, c.rec)
	nuzno(t, r.Save(ctxRaz, novaPrijavaGreske("a")))
	slika := []byte("slika")
	for _, korisnik := range []string{"a", "a", "b"} {
		p := novaPrijavaGreske(korisnik)
		p.SlikaOtisak, p.SlikaBajtova, p.SlikaVrsta = sadrzaj.Otisak(slika), len(slika), models.SlikaPrijavePNG
		nuzno(t, r.SaveSaSlikom(ctxRaz, p, slika))
	}
	vremena, err := r.ZadnjaVremenaSlika(ctxRaz, "a", 10)
	nuzno(t, err)
	if len(vremena) != 2 || vremena[0].Before(vremena[1]) {
		t.Errorf("%v", vremena)
	}
	if vremena, err := r.ZadnjaVremenaSlika(ctxRaz, "a", 1); err != nil || len(vremena) != 1 {
		t.Errorf("%v %v", vremena, err)
	}
}

// Prijava koja se ne upiše ne ostavlja sliku u spremištu, a ista slika uz drugu
// prijavu ostaje
func TestSlikaNeOstajeKadPrijavaNijeUpisana(t *testing.T) {
	sp := spremisteZaTest(t, "teren")
	SetSpremiste(sp)
	c := noviCvor(t, "teren")
	r := NewPrijavaGreskeRepository(c.db, c.rec)
	slika := []byte("bajtovi slike zaslona")
	otisak := sadrzaj.Otisak(slika)
	nova := func() *models.PrijavaGreske {
		p := novaPrijavaGreske("osoba-1")
		p.SlikaOtisak, p.SlikaBajtova, p.SlikaVrsta = otisak, len(slika), models.SlikaPrijavePNG
		return p
	}
	kvar := func() {
		_, err := c.db.Exec(`CREATE TRIGGER kvar BEFORE INSERT ON prijave_gresaka BEGIN SELECT RAISE(ABORT, 'kvar'); END`)
		nuzno(t, err)
	}
	kvar()
	if err := r.SaveSaSlikom(ctxRaz, nova(), slika); err == nil {
		t.Fatal("upis je morao pasti")
	}
	if r.ImaSliku(ctxRaz, otisak) {
		t.Error("slika je ostala bez prijave")
	}
	_, err := c.db.Exec(`DROP TRIGGER kvar`)
	nuzno(t, err)
	nuzno(t, r.SaveSaSlikom(ctxRaz, nova(), slika))
	kvar()
	if err := r.SaveSaSlikom(ctxRaz, nova(), slika); err == nil {
		t.Fatal("upis je morao pasti")
	}
	if !r.ImaSliku(ctxRaz, otisak) {
		t.Error("nestala je slika koju drži prva prijava")
	}
}
