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
