package repository

import (
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Brisanje prijave s terena i nacrta dnevnog lista ide u kanal u koji je
// zapis upisan, kao kod očitanja: laptop koji prati samo taj kanal dobije
// brisanje, a laptop drugog područja ne dobije ni spomenik. Osobe su
// izmišljene.

// posaljiKanalom šalje drugom čvoru ono što mu nedostaje, samo iz
// zajedničkog kanala i kanala koji prati, kao razmjena s njegovom pretplatom
func posaljiKanalom(t *testing.T, a, b *cvor, prati string) {
	t.Helper()
	granica, err := b.rec.Frontier(ctxRaz)
	nuzno(t, err)
	verzije, err := a.rec.Delta(ctxRaz, granica, func(kanal string) bool { return kanal == "" || kanal == prati }, 0)
	nuzno(t, err)
	primi(t, b, verzije)
}

// provjeriKanalBrisanja: svaka verzija zapisa, i spomenik, stoji u kanalu
// zapisa; čvor koji kanal ne prati nema nijednu
func provjeriKanalBrisanja(t *testing.T, a, drugdje *cvor, entitet, id, kanal string) {
	t.Helper()
	upit := `SELECT count(*) FROM record_versions WHERE entity = ? AND entity_id = ?`
	if n := brojRedaka(t, a.db, upit+` AND archived = 1 AND channel = ?`, entitet, id, kanal); n != 1 {
		t.Errorf("%s: brisanje nije u kanalu %s (spomenika u njemu: %d)", entitet, kanal, n)
	}
	if n := brojRedaka(t, a.db, upit+` AND channel <> ?`, entitet, id, kanal); n != 0 {
		t.Errorf("%s: %d verzija izvan kanala %s", entitet, n, kanal)
	}
	if n := brojRedaka(t, drugdje.db, upit, entitet, id); n != 0 {
		t.Errorf("%s: čvor koji ne prati kanal %s dobio je %d verzija", entitet, kanal, n)
	}
}

func TestBrisanjePrijaveUKanaluPrijave(t *testing.T) {
	a, b, drugdje := noviCvor(t, "posluzitelj"), noviCvor(t, "laptop-bp16"), noviCvor(t, "laptop-bp17")
	kanal, tudji := ledger.ChannelFor(ledger.ChannelPrijave, 16, 2026), ledger.ChannelFor(ledger.ChannelPrijave, 17, 2026)
	r := NewPrijavaRepository(a.db, a.rec)
	p := &models.PrijavaSTerena{UserID: osnovaKorisnik.String(), Ime: "Ivo Ivić", Sektor: "B", AreaID: 16,
		Datum: time.Date(2026, time.October, 14, 0, 0, 0, 0, models.Zagreb), Vrsta: models.PrijavaPrijava,
		Status: models.PrijavaNacrt, Naslov: "procjeđivanje kod rkm 12"}
	nuzno(t, r.Save(ctxRaz, p))
	posaljiKanalom(t, a, b, kanal)
	if x, err := NewPrijavaRepository(b.db, b.rec).Get(ctxRaz, p.ID); err != nil || x == nil {
		t.Fatalf("laptop nije dobio prijavu: %v", err)
	}

	nuzno(t, r.Delete(ctxRaz, p.ID))
	posaljiKanalom(t, a, b, kanal)
	posaljiKanalom(t, a, drugdje, tudji)
	if x, err := NewPrijavaRepository(b.db, b.rec).Get(ctxRaz, p.ID); err != nil || x != nil {
		t.Errorf("brisanje nije stiglo na laptop: %+v %v", x, err)
	}
	provjeriKanalBrisanja(t, a, drugdje, EntityPrijave, p.ID, kanal)
}

func TestBrisanjeNacrtaListaUKanaluLista(t *testing.T) {
	a, b, drugdje := noviCvor(t, "posluzitelj"), noviCvor(t, "laptop-bp16"), noviCvor(t, "laptop-bp17")
	kanal, tudji := ledger.ChannelFor(ledger.ChannelVodocuvar, 16, 2026), ledger.ChannelFor(ledger.ChannelVodocuvar, 17, 2026)
	nacrt := nacrtIve(t, a, "obilazak nasipa", time.Date(2026, time.October, 14, 9, 0, 0, 0, models.Zagreb))
	posaljiKanalom(t, a, b, kanal)
	jedanListIve(t, b)

	nuzno(t, NewVodocuvarRepository(a.db, a.rec).Delete(ctxRaz, nacrt.ID))
	posaljiKanalom(t, a, b, kanal)
	posaljiKanalom(t, a, drugdje, tudji)
	if l := listoviIve(t, b); len(l) != 0 {
		t.Errorf("brisanje nije stiglo na laptop: %+v", l)
	}
	provjeriKanalBrisanja(t, a, drugdje, EntityVodocuvarski, nacrt.ID, kanal)
}

// Nacrt prijave upisan za 2. 1. 2027. dobije laptop koji prati 2027. Zatim
// mu vodočuvar dan događaja premjesti na 31. 12. 2026., pa nacrt obriše.
// Brisanje mora stići i na laptop koji prati 2027. i na laptop koji prati
// 2026.: svaki ima svoju kopiju nacrta.
func TestBrisanjePremjestenogNacrtaPrijave(t *testing.T) {
	a, l2027, l2026 := noviCvor(t, "posluzitelj"), noviCvor(t, "laptop-2027"), noviCvor(t, "laptop-2026")
	k2027, k2026 := ledger.ChannelFor(ledger.ChannelPrijave, 16, 2027), ledger.ChannelFor(ledger.ChannelPrijave, 16, 2026)
	r := NewPrijavaRepository(a.db, a.rec)
	p := &models.PrijavaSTerena{UserID: osnovaKorisnik.String(), Ime: "Ivo Ivić", Sektor: "B", AreaID: 16,
		Datum: time.Date(2027, time.January, 2, 0, 0, 0, 0, models.Zagreb), Vrsta: models.PrijavaPrijava,
		Status: models.PrijavaNacrt, Naslov: "procjeđivanje kod rkm 12"}
	nuzno(t, r.Save(ctxRaz, p))
	posaljiKanalom(t, a, l2027, k2027)
	p.Datum = time.Date(2026, time.December, 31, 0, 0, 0, 0, models.Zagreb)
	nuzno(t, r.Save(ctxRaz, p))
	posaljiKanalom(t, a, l2026, k2026)

	nuzno(t, r.Delete(ctxRaz, p.ID))
	posaljiKanalom(t, a, l2027, k2027)
	posaljiKanalom(t, a, l2026, k2026)
	for ime, c := range map[string]*cvor{"laptop-2027": l2027, "laptop-2026": l2026} {
		if x, err := NewPrijavaRepository(c.db, c.rec).Get(ctxRaz, p.ID); err != nil || x != nil {
			t.Errorf("%s: brisanje nije stiglo, nacrt je još ondje (%v)", ime, err)
		}
	}
	for _, kanal := range []string{k2027, k2026} {
		upit := `SELECT count(*) FROM record_versions WHERE entity = ? AND entity_id = ? AND archived = 1 AND channel = ?`
		if n := brojRedaka(t, a.db, upit, EntityPrijave, p.ID, kanal); n != 1 {
			t.Errorf("spomenika u kanalu %s: %d, treba 1", kanal, n)
		}
	}
}
