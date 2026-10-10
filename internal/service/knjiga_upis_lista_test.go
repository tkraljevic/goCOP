package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// premjestiPeru premješta Peru Perića izmjenom zaduženja u mjestu na
// dionicu P.2.1 područja 2; zaduženje na P.1.1 stoji od prije mjesec dana
// do izmjene, sada. Vraća dan prije dva dana, kad je čuvao P.1.1; kad taj
// dan pada u prošlu godinu (knjiga arhivirana), test se preskače.
func (k *knjigaProba) premjestiPeru(t *testing.T) time.Time {
	t.Helper()
	sad := time.Now().In(models.Zagreb)
	dan := time.Date(sad.Year(), sad.Month(), sad.Day(), 0, 0, 0, 0, models.Zagreb).AddDate(0, 0, -2)
	if dan.Year() != sad.Year() {
		t.Skip("dan prije dva dana pada u prošlu godinu")
	}
	if _, err := k.o.baza.Exec(`UPDATE duties SET created_at = ? WHERE user_id = ?`, sad.AddDate(0, -1, 0).UTC(), k.pero.ID.String()); err != nil {
		t.Fatal(err)
	}
	zad, err := k.users.GetDutiesForUser(k.pero.ID)
	if err != nil || len(zad) != 1 {
		t.Fatalf("Perino zaduženje: %v %+v", err, zad)
	}
	d := zad[0]
	d.AreaID, d.SectionCodes = &k.p2, "P.2.1"
	if err := k.users.UpdateDuty(&d); err != nil {
		t.Fatal(err)
	}
	return dan
}

// Bilješka u knjigu za dan provjerava pravo na listu tog dana kad on već
// postoji, kao i list i obrazac na njemu: list nosi branjeno područje na
// kojem je napisan. Vodočuvar premješten na drugo područje izmjenom
// zaduženja: rukovoditelj područja lista upisuje u njega, a rukovoditelj
// novog područja, koji list ne čita, ne upisuje (Odluke (8. 10. 2026.),
// pitanje 1). Za dan bez lista pravo se gleda po sadašnjem zaduženju.
// Podaci su izmišljeni.
func TestBiljeskaGledaListDana(t *testing.T) {
	k := novaKnjigaProba(t)
	ctx := context.Background()
	vs := k.o.vs
	dan := k.premjestiPeru(t)
	l := &models.VodocuvarskiList{UserID: k.pero.ID.String(), Ime: k.pero.FullName, Sektor: "P", AreaID: 1, Datum: dan, Broj: 1, Od: "07:30", Do: "15:30"}
	if err := k.o.repo.Save(ctx, l); err != nil {
		t.Fatal(err)
	}
	prvi := k.osoba(models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &k.p1})
	drugi := k.osoba(models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &k.p2})
	if !vs.SmijeVidjeti(prvi, l) || !vs.SmijeUpisati(prvi, l) {
		t.Fatal("rukovoditelj područja 1 ne čita ili ne upisuje list svog područja")
	}
	if vs.SmijeVidjeti(drugi, l) || vs.SmijeUpisati(drugi, l) {
		t.Fatal("rukovoditelj područja 2 čita list područja 1 iz vremena prije premještaja")
	}
	if _, err := vs.Upisi(ctx, prvi, &prvi.User, k.pero.ID.String(), dan, "upis rukovoditelja BP 1"); err != nil {
		t.Errorf("rukovoditelj područja 1 ne upisuje u list svog područja: %v", err)
	}
	if _, err := vs.Upisi(ctx, drugi, &drugi.User, k.pero.ID.String(), dan, "upis rukovoditelja BP 2"); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("rukovoditelj područja 2 upisuje u list područja 1: %v", err)
	}
	upisano := k.o.vdListIzBaze(t, k.pero, dan)
	var tekstovi []string
	for _, u := range upisano.Upisi {
		tekstovi = append(tekstovi, u.Tekst)
	}
	if got := strings.Join(tekstovi, "; "); got != "upis rukovoditelja BP 1" {
		t.Errorf("upisi na listu: %s", got)
	}
	// danas lista nema: pravo po sadašnjem zaduženju (područje 2); sat
	// servisa je na tom danu, jer se list za dan koji je prošao više ne
	// otvara (pitanje 6)
	danas := dan.AddDate(0, 0, 2)
	k.o.naDan(danas)
	if _, err := vs.Upisi(ctx, drugi, &drugi.User, k.pero.ID.String(), danas, "danas na području 2"); err != nil {
		t.Errorf("rukovoditelj područja 2 ne upisuje za danas: %v", err)
	}
}

// Kad se list dana ne da pročitati, bilješka se ne upisuje (ni u novi list
// koji bi zamijenio postojeći).
func TestBiljeskaBezCitanjaListaDana(t *testing.T) {
	k := novaKnjigaProba(t)
	ctx := context.Background()
	if _, err := k.o.baza.Exec(`DROP TABLE vodocuvarski_listovi`); err != nil {
		t.Fatal(err)
	}
	rukovoditelj := k.osoba(models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &k.p1})
	if _, err := k.o.vs.Upisi(ctx, rukovoditelj, &rukovoditelj.User, k.pero.ID.String(), vdDan(time.March, 10), "upis"); err == nil || errors.Is(err, ErrUnauthorized) {
		t.Errorf("bez čitanja lista dana: %v", err)
	}
}
