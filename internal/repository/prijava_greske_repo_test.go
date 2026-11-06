package repository

import (
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
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
	ista, err := r.IstaNedavna(ctxRaz, "a", prva.Vrsta, prva.Opis, prva.Stranica, time.Now().Add(-time.Minute))
	nuzno(t, err)
	if ista == nil || ista.ID != druga.ID {
		t.Errorf("ista nedavna: %+v", ista)
	}
	if ista, err := r.IstaNedavna(ctxRaz, "a", prva.Vrsta, "drugi tekst", prva.Stranica, time.Now().Add(-time.Minute)); err != nil || ista != nil {
		t.Errorf("drugi tekst: %+v %v", ista, err)
	}
	if ista, err := r.IstaNedavna(ctxRaz, "a", prva.Vrsta, prva.Opis, prva.Stranica, time.Now().Add(time.Minute)); err != nil || ista != nil {
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
		if _, err := r.IstaNedavna(ctx, "a", "v", "o", "s", time.Now()); err == nil {
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
