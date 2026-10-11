package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Sažimanje knjige ne smije vratiti nacrt preko predanog lista ni izgubiti
// bilješku rukovoditelja, Odluke (8. 10. 2026.), pitanje 7: površina lista
// slaže se iz cijele povijesti, a sažimanje briše starije verzije. Osobe su
// izmišljene: vodočuvar Ivo Ivić, rukovoditeljica Ana Anić.

// ostariVerzije pomiče nastanak svih verzija na čvoru 200 dana unatrag, da ih
// sažimanje s rokom od 90 dana smije brisati
func ostariVerzije(t *testing.T, n *cvor) {
	t.Helper()
	_, err := n.db.Exec(`UPDATE record_versions SET created_at = ?`, time.Now().UTC().AddDate(0, 0, -200))
	nuzno(t, err)
}

// verzijeListaIve broji verzije lista Ive za 14. 10. u knjizi čvora
func verzijeListaIve(t *testing.T, n *cvor) int {
	t.Helper()
	var broj int
	nuzno(t, n.db.QueryRow(`SELECT count(*) FROM record_versions WHERE entity = ? AND entity_id = ?`,
		EntityVodocuvarski, models.IDListaVodocuvara(ivoIvic, danIve)).Scan(&broj))
	return broj
}

// Ivo preda list na laptopu, Ana na poslužitelju upiše bilješku na nacrt
// istog dana; nacrt je zadnja verzija. Poslužitelj sažme knjigu i površinu
// obnovi iz nje: list je i dalje predan, s Aninom bilješkom, a tako ga vidi
// i čvor koji se tek tada spoji i primi samo sažetu knjigu.
func TestSazimanjeNeVracaNacrtPrekoPredanogLista(t *testing.T) {
	a, b := noviCvor(t, "laptop"), noviCvor(t, "posluzitelj")
	predajIvu(t, a, "obilazak nasipa Batina")
	// laptop zatim piše i drugi list u istom kanalu, pa predan list nije
	// njegova zadnja verzija, koju sažimanje čuva zbog granice razmjene
	sutra := listIve("sutradan")
	sutra.Datum = danIve.AddDate(0, 0, 1)
	nuzno(t, NewVodocuvarRepository(a.db, a.rec).Save(ctxRaz, sutra))
	nacrtSUpisom(t, b, "pojačati noćnu ophodnju kod Batine", time.Date(2026, time.October, 14, 22, 0, 0, 0, models.Zagreb))
	posalji(t, a, b)
	ostariVerzije(t, b)

	if _, err := SazmiKnjigu(ctxRaz, b.db, b.rec, time.Now().AddDate(0, 0, -90)); err != nil {
		t.Fatal(err)
	}
	if n := verzijeListaIve(t, b); n != 1 {
		t.Errorf("nakon sažimanja lista ima %d verzija, očekivana samo zadnja", n)
	}
	if _, err := ReplaySurface(ctxRaz, b.db, b.rec); err != nil {
		t.Fatal(err)
	}
	l := jedanListIve(t, b)
	if !l.Predan() || l.Opis != "obilazak nasipa Batina" || !imaUpis(l, "pojačati noćnu ophodnju kod Batine") {
		t.Errorf("sažimanje je promijenilo list: %+v", l)
	}
	c := noviCvor(t, "novi")
	posalji(t, b, c)
	if l := jedanListIve(t, c); !l.Predan() || !imaUpis(l, "pojačati noćnu ophodnju kod Batine") {
		t.Errorf("čvor sa sažetom knjigom: %+v", l)
	}
	// list koji je već upisan kakav vrijedi ne upisuje se ponovno
	if n, err := NewVodocuvarRepository(b.db, b.rec).UpisiSlozeneListove(ctxRaz); err != nil || n != 0 {
		t.Errorf("ponovni upis: %d %v", n, err)
	}
}

// Nacrt obrisan na poslužitelju nakon što je list predan na laptopu: zadnja
// verzija je brisanje, a list vrijedi kao predan. Upisuje se kao nova
// verzija, pa ga sažimanje ne briše s brisanjem.
func TestSazimanjeNeBrisePredanListSBrisanjemNacrta(t *testing.T) {
	a, b := noviCvor(t, "laptop"), noviCvor(t, "posluzitelj")
	predajIvu(t, a, "obilazak nasipa")
	nacrt := listIve("nacrt na poslužitelju")
	repoB := NewVodocuvarRepository(b.db, b.rec)
	nuzno(t, repoB.Save(ctxRaz, nacrt))
	nuzno(t, repoB.Delete(ctxRaz, nacrt.ID))
	posalji(t, a, b)

	if n, err := repoB.UpisiSlozeneListove(ctxRaz); err != nil || n != 1 {
		t.Fatalf("upis lista kakav vrijedi: %d %v", n, err)
	}
	zadnja, err := b.rec.Latest(ctxRaz, EntityVodocuvarski, nacrt.ID)
	nuzno(t, err)
	if zadnja.Archived || !strings.Contains(string(zadnja.Payload), `"predano_at"`) {
		t.Errorf("zadnja verzija nije predan list: %s", zadnja.Payload)
	}
	if l := jedanListIve(t, b); !l.Predan() || l.Opis != "obilazak nasipa" {
		t.Errorf("površina nakon upisa: %+v", l)
	}
}

// List koji je pisao samo jedan čvor, i obrisan nacrt koji je zadnji, ne
// upisuju se; greška čitanja knjige zaustavlja upis i sažimanje.
func TestUpisSlozenihListovaGreske(t *testing.T) {
	a := noviCvor(t, "posluzitelj")
	repo := NewVodocuvarRepository(a.db, a.rec)
	nuzno(t, repo.Save(ctxRaz, listIve("samo ovdje")))
	upisi := func(id, cvor string, arhivirano int, payload string) {
		t.Helper()
		_, err := a.db.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel)
			VALUES (?, ?, ?, ?, '', ?, ?, ?, 1, '')`, uuid.Must(uuid.NewV7()).String(), EntityVodocuvarski, id, cvor, arhivirano, payload, time.Now().UTC())
		nuzno(t, err)
	}
	// nacrt s dva čvora koji je zatim obrisan: na površini ga nema
	obrisan := models.IDListaVodocuvara(anaAnic, danIve)
	upisi(obrisan, "laptop", 0, `{"opis":"nacrt"}`)
	upisi(obrisan, "posluzitelj", 1, `{"opis":"nacrt"}`)
	if n, err := repo.UpisiSlozeneListove(ctxRaz); err != nil || n != 0 {
		t.Errorf("bez razlike nema upisa: %d %v", n, err)
	}

	prekinut, odustani := context.WithCancel(ctxRaz)
	odustani()
	if _, err := repo.UpisiSlozeneListove(prekinut); err == nil {
		t.Error("prekinut upis nije javio grešku")
	}

	osteceni := models.IDListaVodocuvara(ivoIvic, danIve.AddDate(0, 0, 3))
	upisi(osteceni, "laptop", 0, `{}`)
	upisi(osteceni, "posluzitelj", 0, `"nije list"`)
	prije := verzijeUKnjizi(t, a)
	if _, err := SazmiKnjigu(ctxRaz, a.db, a.rec, time.Now().Add(time.Hour)); err == nil || !strings.Contains(err.Error(), "prije sažimanja") {
		t.Errorf("oštećena verzija lista nije zaustavila sažimanje: %v", err)
	}
	if poslije := verzijeUKnjizi(t, a); poslije != prije {
		t.Errorf("knjiga je sažeta unatoč grešci: %d → %d verzija", prije, poslije)
	}
}

// verzijeUKnjizi broji sve verzije u knjizi čvora
func verzijeUKnjizi(t *testing.T, n *cvor) int {
	t.Helper()
	var broj int
	nuzno(t, n.db.QueryRow(`SELECT count(*) FROM record_versions`).Scan(&broj))
	return broj
}
