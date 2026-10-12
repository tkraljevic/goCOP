package peers

// Uloge računala (preuzima vodostaje, izdaje prognozu): zapis, učitavanje,
// stanje u memoriji i što se događa kad baza ne surađuje. Uloge su lokalne,
// pa se sve vidi na jednom čvoru.

import (
	"context"
	"testing"
)

// Nova baza nema uloga: učitavanje javlja da ih nitko nije postavio, a u
// memoriji nema ničega ni prije ni poslije učitavanja.
func TestUlogeNoveBazeNisuPostavljene(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	if u := s.TrenutneUloge(); u != (Uloge{}) {
		t.Errorf("prije učitavanja: %+v", u)
	}
	u, postavljeno, err := s.UcitajUloge(ctxRub)
	if err != nil || postavljeno || u != (Uloge{}) {
		t.Errorf("nova baza: %+v postavljeno=%v err=%v", u, postavljeno, err)
	}
}

// Postavljene uloge vrijede odmah (memorija), ostaju u bazi (novo
// učitavanje ih vrati) i javljaju se onome tko prati promjenu. Ponovno
// postavljanje prepisuje staro, ne dodaje.
func TestPostavljeneUlogeVrijedeIOstaju(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	javljeno := make(chan Uloge, 2)
	s.NaPromjenuUloga(func(u Uloge) { javljeno <- u })

	for _, zeljene := range []Uloge{
		{Preuzima: true, Izdaje: true},
		{Preuzima: false, Izdaje: true},
		{},
	} {
		if err := s.PostaviUloge(ctxRub, zeljene); err != nil {
			t.Fatal(err)
		}
		if u := s.TrenutneUloge(); u != zeljene {
			t.Errorf("u memoriji %+v, postavljeno %+v", u, zeljene)
		}
		if u := <-javljeno; u != zeljene {
			t.Errorf("javljeno %+v, postavljeno %+v", u, zeljene)
		}
		drugi := &Service{db: s.db}
		u, postavljeno, err := drugi.UcitajUloge(ctxRub)
		if err != nil || !postavljeno || u != zeljene {
			t.Errorf("iz baze %+v postavljeno=%v err=%v, postavljeno %+v", u, postavljeno, err, zeljene)
		}
		if drugi.TrenutneUloge() != zeljene {
			t.Errorf("učitavanje nije osvježilo memoriju: %+v", drugi.TrenutneUloge())
		}
	}
	var redaka int
	if err := s.db.QueryRow(`SELECT count(*) FROM uloge_cvora`).Scan(&redaka); err != nil || redaka != 2 {
		t.Errorf("u tablici %d redaka (%v), očekivano 2", redaka, err)
	}
}

// Ključ koji program ne poznaje ne mijenja uloge, ali znači da je netko
// uloge zapisao; vrijednost različita od "1" je isključena uloga.
func TestNepoznatKljucUlogaSePreskace(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	if _, err := s.db.Exec(`INSERT INTO uloge_cvora (kljuc, vrijednost) VALUES ('crta', '1'), ('izdaje', 'da')`); err != nil {
		t.Fatal(err)
	}
	u, postavljeno, err := s.UcitajUloge(ctxRub)
	if err != nil || !postavljeno || u != (Uloge{}) {
		t.Errorf("%+v postavljeno=%v err=%v", u, postavljeno, err)
	}
}

// Kad se uloge ne mogu pročitati, učitavanje to javlja i ne dira uloge u
// memoriji: čvor radi po zadnjima koje je znao.
func TestUcitavanjeUlogaKadBazaNeSuradjuje(t *testing.T) {
	for ime, kvar := range map[string]string{
		"nema tablice": `DROP TABLE uloge_cvora`,
		// ključ NULL prođe u tablicu (PRIMARY KEY koji nije INTEGER), ali se ne čita kao tekst
		"ključ bez vrijednosti": `INSERT INTO uloge_cvora (kljuc, vrijednost) VALUES (NULL, '1')`,
		// prvi redak se pročita, drugi padne usred čitanja
		"čitanje pukne usred popisa": `DROP TABLE uloge_cvora;
			CREATE TABLE izvor_uloga (rb INTEGER PRIMARY KEY, kljuc TEXT, podatak TEXT);
			INSERT INTO izvor_uloga VALUES (1, 'preuzima', '"1"'), (2, 'izdaje', 'nije json');
			CREATE VIEW uloge_cvora AS SELECT kljuc, json_extract(podatak, '$') AS vrijednost FROM izvor_uloga ORDER BY rb`,
	} {
		t.Run(ime, func(t *testing.T) {
			s := rubniCvor(t, "cop-osijek")
			zadnje := Uloge{Izdaje: true}
			s.uloge.Store(&zadnje)
			if _, err := s.db.Exec(kvar); err != nil {
				t.Fatal(err)
			}
			u, postavljeno, err := s.UcitajUloge(ctxRub)
			if err == nil || postavljeno || u != (Uloge{}) {
				t.Errorf("%+v postavljeno=%v err=%v", u, postavljeno, err)
			}
			if s.TrenutneUloge() != zadnje {
				t.Errorf("neuspjelo učitavanje promijenilo je uloge u memoriji: %+v", s.TrenutneUloge())
			}
		})
	}
}

// Uloge koje se ne zapišu ne vrijede ni u memoriji i nitko ne dobije
// vijest o promjeni: bez baze, bez transakcije, kad upis padne i kad
// potvrda transakcije padne.
func TestPostavljanjeUlogaKojeSeNeZapisu(t *testing.T) {
	otkazan, otkazi := context.WithCancel(ctxRub)
	otkazi()
	for ime, s := range map[string]struct {
		ctx  context.Context
		kvar func(t *testing.T, s *Service)
	}{
		"otkazan zahtjev": {otkazan, nil},
		"upis padne": {ctxRub, func(t *testing.T, s *Service) {
			kvarUpisa(t, s, "INSERT", "uloge_cvora", "NEW.kljuc = 'izdaje'")
		}},
		// odgođeni strani ključ prođe upis, a padne tek na potvrdi transakcije
		"potvrda padne": {ctxRub, func(t *testing.T, s *Service) {
			if _, err := s.db.Exec(`DROP TABLE uloge_cvora;
				CREATE TABLE roditelj_uloge (id TEXT PRIMARY KEY);
				CREATE TABLE uloge_cvora (kljuc TEXT PRIMARY KEY, vrijednost TEXT NOT NULL,
					roditelj TEXT DEFAULT 'nema' REFERENCES roditelj_uloge(id) DEFERRABLE INITIALLY DEFERRED)`); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(ime, func(t *testing.T) {
			c := rubniCvor(t, "cop-osijek")
			if s.kvar != nil {
				s.kvar(t, c)
			}
			javljeno := false
			c.NaPromjenuUloga(func(Uloge) { javljeno = true })
			if err := c.PostaviUloge(s.ctx, Uloge{Preuzima: true, Izdaje: true}); err == nil {
				t.Fatal("uloge su postavljene iako nisu zapisane")
			}
			if u := c.TrenutneUloge(); u != (Uloge{}) {
				t.Errorf("nezapisane uloge vrijede u memoriji: %+v", u)
			}
			if javljeno {
				t.Error("promjena je javljena iako je nema")
			}
		})
	}

	bezBaze := &Service{}
	if err := bezBaze.PostaviUloge(ctxRub, Uloge{Izdaje: true}); err == nil || bezBaze.TrenutneUloge() != (Uloge{}) {
		t.Errorf("bez baze: %v, %+v", err, bezBaze.TrenutneUloge())
	}
}
