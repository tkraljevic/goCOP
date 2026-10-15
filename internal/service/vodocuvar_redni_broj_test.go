package service

import (
	"context"
	"testing"
	"time"

	"gocop/internal/models"
)

// Redni broj lista, Odluke (8. 10. 2026.), pitanje 6: list dobiva broj kad
// počne njegov dan, a brojevi idu redom dana. Nacrt pripremljen unaprijed
// koji nitko nije spremio na njegov dan broj dobiva najkasnije kad ga dobije
// list nekog kasnijeg dana, i to prije njega. Već upisani brojevi se ne
// mijenjaju, a preneseni listovi ostaju bez broja. Vodočuvar je izmišljeni
// Pero Perić.

func TestBrojeviNacrtaRedomDana(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	u := vdVodocuvar()
	dan := func(d int) time.Time { return vdDan(time.March, d) }
	spremi := func(d int) *models.VodocuvarskiList {
		t.Helper()
		l, err := o.vs.Spremi(ctx, u, dan(d), vdUnos("plan za "+dan(d).Format("02.01.")), false)
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	prenesen := &models.VodocuvarskiList{UserID: u.ID.String(), Ime: "Pero Perić", Sektor: "P", AreaID: 1, Datum: dan(3),
		Opis: "obilazak iz stare evidencije", Rekonstrukcija: true, Izvor: "probni-zapis-1"}
	if err := o.repo.Save(ctx, prenesen); err != nil {
		t.Fatal(err)
	}
	o.naDan(dan(10))
	if l := spremi(10); l.Broj != 1 {
		t.Fatalf("današnji list: broj %d", l.Broj)
	}
	// nacrti unaprijed, upisani obrnutim redom, bez broja
	for _, d := range []int{14, 12, 11} {
		if l := spremi(d); l.Broj != 0 {
			t.Fatalf("nacrt za %d. 3.: broj %d", d, l.Broj)
		}
	}

	// 11. i 12. 3. nitko nije spremio; 13. 3. vodočuvar otvara nov list
	o.naDan(dan(13))
	if l := spremi(13); l.Broj != 4 {
		t.Errorf("list od 13. 3.: broj %d, očekivan 4", l.Broj)
	}
	for d, broj := range map[int]int{3: 0, 10: 1, 11: 2, 12: 3, 14: 0} {
		if l := o.vdListIzBaze(t, u, dan(d)); l == nil || l.Broj != broj {
			t.Errorf("list od %d. 3.: %+v, očekivan broj %d", d, l, broj)
		}
	}
	// nacrt za 14. 3. dobiva broj kad dan počne, i predaja nacrta 11. 3.
	// nakon njegova dana ne mijenja broj
	o.naDan(dan(14))
	if l := spremi(14); l.Broj != 5 {
		t.Errorf("list od 14. 3.: broj %d, očekivan 5", l.Broj)
	}
	if l, err := o.vs.Spremi(ctx, u, dan(11), vdUnos("obilazak nasipa"), true); err != nil || !l.Predan() || l.Broj != 2 {
		t.Errorf("predaja nacrta od 11. 3.: %+v %v", l, err)
	}
}

// Nacrt koji je dobio broj tek s kasnijim listom sprema se kroz knjigu
// verzija, kao svaki upis lista; dan koji već ima list s brojem, predan ili
// prenesen list ne dobiva još jedan broj, a od dva nacrta istog dana broj
// dobiva stariji.
func TestNacrtiRanijihDana(t *testing.T) {
	dan := func(d int) time.Time { return vdDan(time.March, d) }
	sad := time.Now()
	predano := sad
	knjiga := []models.VodocuvarskiList{
		{ID: "s-brojem", Datum: dan(2), Broj: 1},
		{ID: "dvojnik-dana-s-brojem", Datum: dan(2)},
		{ID: "predan", Datum: dan(4), PredanoAt: &predano},
		{ID: "prenesen", Datum: dan(5), Rekonstrukcija: true},
		{ID: "mladji", Datum: dan(7), CreatedAt: sad},
		{ID: "stariji", Datum: dan(7), CreatedAt: sad.Add(-time.Hour)},
		{ID: "raniji", Datum: dan(6)},
		{ID: "isti-dan", Datum: dan(9).Add(8 * time.Hour)},
		{ID: "kasniji", Datum: dan(12)},
	}
	l := &models.VodocuvarskiList{ID: "isti-dan-drugi", Datum: dan(9)}
	var ids []string
	for _, n := range nacrtiRanijihDana(knjiga, l) {
		ids = append(ids, n.ID)
	}
	if len(ids) != 2 || ids[0] != "raniji" || ids[1] != "stariji" {
		t.Errorf("nacrti ranijih dana: %v", ids)
	}
}

// Kad se broj nacrta ranijeg dana ne može upisati, ni list koji ga traži ne
// dobiva broj, a greška kaže koji nacrt; broj se ne daje ni kad ga knjiga
// ne može pročitati.
func TestBrojNacrtaRanijegDanaGreska(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	u := vdVodocuvar()
	o.naDan(vdDan(time.March, 10))
	if _, err := o.vs.Spremi(ctx, u, vdDan(time.March, 11), vdUnos("plan"), false); err != nil {
		t.Fatal(err)
	}
	if _, err := o.baza.Exec(`CREATE TRIGGER vd_bez_broja BEFORE UPDATE ON vodocuvarski_listovi WHEN NEW.broj > 0 BEGIN SELECT RAISE(ABORT, 'upis zabranjen'); END`); err != nil {
		t.Fatal(err)
	}
	o.naDan(vdDan(time.March, 12))
	_, err := o.vs.Spremi(ctx, u, vdDan(time.March, 12), vdUnos("obilazak"), false)
	vdGreska(t, err, "redni broj nacrta od 11.03.")
	if l := o.vdListIzBaze(t, u, vdDan(time.March, 12)); l != nil {
		t.Errorf("list bez broja nacrta ranijeg dana: %+v", l)
	}

	prekinut, odustani := context.WithCancel(context.Background())
	odustani()
	l := &models.VodocuvarskiList{UserID: u.ID.String(), Datum: vdDan(time.March, 12)}
	if err := o.vs.sljedeciBroj(prekinut, l); err == nil || l.Broj != 0 {
		t.Errorf("sljedeći broj s prekinutim upitom: broj %d, greška %v", l.Broj, err)
	}
}
