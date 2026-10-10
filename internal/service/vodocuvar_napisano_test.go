package service

import (
	"context"
	"testing"
	"time"

	"gocop/internal/models"
)

// Vodočuvarov upis nosi vrijeme (Odluke (8. 10. 2026.), pitanje 7): po
// njemu nacrt koji je na drugom čvoru nastao bez vodočuvarova teksta
// (rukovoditeljev „Upiši”, akt o obrani) ne prepisuje njegov tekst. Upis iz
// akta vrijeme prenosi kakvo jest. Osobe su izmišljene: vodočuvar Pero
// Perić, rukovoditeljica Ana Anić.
func TestVodocuvarovUpisNosiVrijeme(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	upisan, users := o.vdVodocuvarUBazi(t)
	pero, err := users.GetUserByID(upisan.ID)
	if err != nil || pero == nil {
		t.Fatalf("vodočuvar iz baze: %v %v", pero, err)
	}
	dan := vdDan(time.March, 10)
	o.naDan(dan)

	prije := time.Now()
	l, err := o.vs.Spremi(ctx, pero, dan, vdUnos("obilazak nasipa"), false)
	if err != nil {
		t.Fatal(err)
	}
	if l.NapisanoAt == nil || l.NapisanoAt.Before(prije) {
		t.Fatalf("spremanje obrasca nije upisalo vrijeme vodočuvarova upisa: %v", l.NapisanoAt)
	}
	napisano := *l.NapisanoAt

	akt := &models.Akt{Sektor: "P", AreaID: 1, Broj: 4, Godina: dan.Year(), OvjerioID: "01900000-0000-7000-8000-0000000017a2", Ovjerio: "Ana Anić"}
	if upozorenja := o.vs.UpisiIzAkta(ctx, akt, dan, "redovna obrana"); len(upozorenja) != 0 {
		t.Fatalf("upis iz akta: %v", upozorenja)
	}
	iz := o.vdListIzBaze(t, pero, dan)
	if iz.NapisanoAt == nil || !iz.NapisanoAt.Equal(napisano) || iz.Opis != "obilazak nasipa" || len(iz.Upisi) != 1 {
		t.Errorf("upis iz akta promijenio je vodočuvarov upis: %+v", iz)
	}
}
