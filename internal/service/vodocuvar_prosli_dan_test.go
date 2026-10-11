package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// List za dan koji je prošao, Odluke (8. 10. 2026.), pitanje 6: „Ako list
// nije kreirao jučer kad je trebao, onda ga ne može ni kreirati više. To
// znači da nije radio.” Ni vodočuvar ni rukovoditeljev „Upiši” ne otvaraju
// nov list za prošli dan; list koji za taj dan već postoji i dalje se
// sprema, predaje i prima bilješke. Ni program ne otvara list za prošli
// dan: akt o obrani i prijava s terena idu na današnji list, uz napomenu o
// danu. Granica je 00:00 po hrvatskom vremenu. Osobe su izmišljene: Pero
// Perić (vodočuvar), Marko Markić (rukovoditelj), Ana Anić (ovjerila akt).

// vdVodocuvarSDuznosti je Pero Perić iz baze (vdVodocuvarUBazi), sa
// zaduženjem, kakvog ga servis vidi kad vodočuvar sam piše list
func (o *okolinaVodocuvara) vdVodocuvarSDuznosti(t *testing.T) *models.User {
	t.Helper()
	pero, users := o.vdVodocuvarUBazi(t)
	cijeli, err := users.GetUserByID(pero.ID)
	if err != nil || cijeli == nil {
		t.Fatalf("vodočuvar iz baze: %v", err)
	}
	return cijeli
}

// Dan lista prolazi u 00:00 idućeg dana, po hrvatskom vremenu, i kad je
// trenutak zadan u drugoj zoni.
func TestGranicaProslogDana(t *testing.T) {
	dan := time.Date(2026, time.October, 9, 0, 0, 0, 0, models.Zagreb)
	ponoc := dan.AddDate(0, 0, 1)
	if danProsao(dan, ponoc.Add(-time.Second)) || !danProsao(dan, ponoc) || !danProsao(dan.UTC(), ponoc.UTC()) {
		t.Error("dan lista mora proći točno u 00:00 idućeg dana po hrvatskom vremenu")
	}
	if err := provjeriNoviList(dan, ponoc.Add(-time.Second)); err != nil {
		t.Errorf("list za dan koji traje: %v", err)
	}
	if err := provjeriNoviList(ponoc.AddDate(0, 0, 3), ponoc); err != nil {
		t.Errorf("list unaprijed: %v", err)
	}
	// poruka ne upućuje vodočuvara da list priprema unaprijed: nacrt za
	// budući dan nastaje upisom rukovoditelja, akta ili prijave, kako kaže
	// pomoć
	err := provjeriNoviList(dan, ponoc)
	if !errors.Is(err, ErrProsliDan) || err.Error() != "za 09.10.2026. nema dnevnog lista, a list za dan koji je prošao više se ne otvara: dnevni list otvara se na svoj dan" {
		t.Errorf("poruka vodočuvaru: %v", err)
	}
	if !istiDan(dan.Add(23*time.Hour), dan.UTC()) || istiDan(dan, ponoc) {
		t.Error("isti dan po hrvatskom vremenu")
	}
}

// Vodočuvar ne otvara nov list za dan koji je prošao: ni pripremom
// („Današnji list” s jučerašnjim datumom u adresi), ni spremanjem, ni
// predajom, i ništa se ne upisuje. Do 23:59 svoj list još otvara, a list
// koji za prošli dan već postoji sprema i predaje kao i dosad.
func TestVodocuvarNeOtvaraListZaProsliDan(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	u := vdVodocuvar()
	dan := vdDan(time.March, 10)
	o.naDan(dan)
	postojeci, err := o.vs.Spremi(ctx, u, dan, vdUnos("obilazak nasipa"), false)
	if err != nil || postojeci.Broj != 1 {
		t.Fatalf("list na svoj dan: %+v %v", postojeci, err)
	}

	// u 23:59 dan još traje, a u 00:00 je prošao
	sutra := dan.AddDate(0, 0, 1)
	o.vs.sad = func() time.Time { return sutra.Add(-time.Minute) }
	if l, err := o.vs.Pripremi(ctx, u, dan); err != nil || l.ID != postojeci.ID {
		t.Fatalf("postojeći list u 23:59: %+v %v", l, err)
	}
	o.vs.sad = func() time.Time { return sutra.AddDate(0, 0, 1) }
	jucer := sutra
	ocekivano := "za " + jucer.Format("02.01.2006.") + " nema dnevnog lista, a list za dan koji je prošao više se ne otvara"
	_, err = o.vs.Pripremi(ctx, u, jucer)
	vdGreska(t, err, ocekivano)
	if !errors.Is(err, ErrProsliDan) {
		t.Errorf("greška nije ErrProsliDan: %v", err)
	}
	for _, predaj := range []bool{false, true} {
		_, err := o.vs.Spremi(ctx, u, jucer, vdUnos("naknadni upis"), predaj)
		vdGreska(t, err, ocekivano)
	}
	if l := o.vdListIzBaze(t, u, jucer); l != nil {
		t.Errorf("list za prošli dan je nastao: %+v", l)
	}
	if n := o.verzijeListova(t); n != 1 {
		t.Errorf("odbijeni upisi zapisali su listove: %d verzija", n)
	}

	// list koji za prošli dan postoji dopunjuje se i predaje
	if l, err := o.vs.Spremi(ctx, u, dan, vdUnos("obilazak nasipa i ustave"), true); err != nil || !l.Predan() || l.ID != postojeci.ID || l.Broj != 1 {
		t.Fatalf("predaja postojećeg lista za prošli dan: %+v %v", l, err)
	}
}

// Rukovoditeljev „Upiši” ne otvara list za dan koji je prošao: bilješka se
// za taj dan upisuje samo na list koji postoji, sa stranice lista ili s
// popisa.
func TestUpisRukovoditeljaNaProsliDan(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	pero := o.vdVodocuvarSDuznosti(t)
	rukovoditelj := vdPperic(vdDuznost(models.RoleAreaLeader, 1, true))
	rukovoditelj.Username, rukovoditelj.FullName = "mmarkic", "Marko Markić"
	perms := models.NewUserPermissions(*rukovoditelj)
	dan := vdDan(time.March, 10)
	o.naDan(dan)
	postojeci, err := o.vs.Spremi(ctx, pero, dan, vdUnos("obilazak nasipa"), true)
	if err != nil {
		t.Fatal(err)
	}
	o.naDan(dan.AddDate(0, 0, 2))
	jucer := dan.AddDate(0, 0, 1)

	_, err = o.vs.Upisi(ctx, perms, rukovoditelj, pero.ID.String(), jucer, "ophodnja nasipa")
	vdGreska(t, err, "Pero Perić nema dnevni list za "+jucer.Format("02.01.2006.")+", a list za dan koji je prošao više se ne otvara, pa se ni bilješka za taj dan ne upisuje")
	if !errors.Is(err, ErrProsliDan) {
		t.Errorf("greška nije ErrProsliDan: %v", err)
	}
	if l := o.vdListIzBaze(t, pero, jucer); l != nil {
		t.Errorf("upis je otvorio list za prošli dan: %+v", l)
	}

	l, err := o.vs.Upisi(ctx, perms, rukovoditelj, pero.ID.String(), dan, "nasip u redu")
	if err != nil || l.ID != postojeci.ID || len(l.Upisi) != 1 || l.Upisi[0].Ime != "Marko Markić" {
		t.Fatalf("upis na postojeći list za prošli dan: %+v %v", l, err)
	}
	l, err = o.vs.UpisiNaList(ctx, perms, rukovoditelj, pero.ID.String(), dan, postojeci.ID, "ustava pregledana")
	if err != nil || l.ID != postojeci.ID || len(l.Upisi) != 2 {
		t.Fatalf("upis sa stranice lista za prošli dan: %+v %v", l, err)
	}
	// druge greške ostaju kakve jesu
	if err := bezListaZaUpis(pero, jucer, ErrUnauthorized); err != ErrUnauthorized {
		t.Errorf("druga greška upisa: %v", err)
	}
}

// Akt o obrani koji vrijedi od dana koji je prošao upisuje se na list tog
// dana kad postoji, a kad ga nema, na današnji list vodočuvara, uz napomenu
// od kada akt vrijedi. List za prošli dan ne nastaje.
func TestAktZaProsliDanNaDanasnjiList(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	pero := o.vdVodocuvarSDuznosti(t)
	radio := vdDan(time.March, 8)
	o.naDan(radio)
	postojeci, err := o.vs.Spremi(ctx, pero, radio, vdUnos("obilazak nasipa"), false)
	if err != nil {
		t.Fatal(err)
	}
	danas := vdDan(time.March, 10)
	o.naDan(danas)
	akt := func(vrijedi time.Time) *models.Akt {
		return &models.Akt{Sektor: "P", AreaID: 1, Broj: 5, Godina: vrijedi.Year(), Vrijedi: vrijedi,
			OvjerioID: "01900000-0000-7000-8000-0000000017a2", Ovjerio: "Ana Anić"}
	}

	// za 9. 3. Pero nema lista: akt je na današnjem listu, s napomenom
	nijeRadio := vdDan(time.March, 9)
	if upozorenja := o.vs.UpisiIzAkta(ctx, akt(nijeRadio.Add(14*time.Hour)), nijeRadio, "redovna obrana"); len(upozorenja) != 0 {
		t.Fatalf("akt za dan bez lista: %v", upozorenja)
	}
	if l := o.vdListIzBaze(t, pero, nijeRadio); l != nil {
		t.Errorf("akt je otvorio list za prošli dan: %+v", l)
	}
	l := o.vdListIzBaze(t, pero, danas)
	napomena := "Obrana od poplava: redovna obrana Akt vrijedi od " + nijeRadio.Format("02.01.2006.") + " u 14:00; za taj dan nema dnevnog lista, pa je upisan na današnji."
	if l == nil || l.Broj != 2 || len(l.Upisi) != 1 || l.Upisi[0].Tekst != napomena || l.Upisi[0].Ime != "Ana Anić" {
		t.Fatalf("akt na današnjem listu: %+v", l)
	}

	// za 8. 3. list postoji: akt je na njemu, bez napomene
	if upozorenja := o.vs.UpisiIzAkta(ctx, akt(radio.Add(9*time.Hour)), radio, "izvanredna obrana"); len(upozorenja) != 0 {
		t.Fatalf("akt za dan s listom: %v", upozorenja)
	}
	if l := o.vdListIzBaze(t, pero, radio); l == nil || l.ID != postojeci.ID || len(l.Upisi) != 1 || l.Upisi[0].Tekst != "Obrana od poplava: izvanredna obrana" {
		t.Errorf("akt na listu svog dana: %+v", l)
	}
	if l := o.vdListIzBaze(t, pero, danas); len(l.Upisi) != 1 {
		t.Errorf("akt za dan s listom upisan i na današnji: %+v", l.Upisi)
	}
}

// Akt koji vrijedi od dana u knjizi arhiviranoj istekom godine ne mijenja
// list te knjige; kad za taj dan lista nema, akt ide na današnji list, kao
// za svaki prošli dan.
func TestAktIzArhiviraneGodine(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	pero, _ := o.vdVodocuvarUBazi(t)
	silvestrovo := vdDan(time.January, 1).AddDate(0, 0, -1)
	stari := &models.VodocuvarskiList{UserID: pero.ID.String(), Ime: "Pero Perić", Sektor: "P", AreaID: 1, Datum: silvestrovo, Od: "07:00", Do: "15:00", Opis: "nacrt"}
	if err := o.repo.Save(ctx, stari); err != nil {
		t.Fatal(err)
	}
	danas := vdDan(time.January, 2)
	o.naDan(danas)
	akt := &models.Akt{Sektor: "P", AreaID: 1, Broj: 1, Godina: silvestrovo.Year(), Vrijedi: silvestrovo.Add(20 * time.Hour), OvjerioID: "01900000-0000-7000-8000-0000000017a2", Ovjerio: "Ana Anić"}
	if upozorenja := o.vs.UpisiIzAkta(ctx, akt, silvestrovo, "redovna obrana"); len(upozorenja) != 0 {
		t.Fatalf("akt za dan arhivirane knjige: %v", upozorenja)
	}
	if l := o.vdListIzBaze(t, pero, silvestrovo); len(l.Upisi) != 0 {
		t.Errorf("akt je promijenio list arhivirane knjige: %+v", l.Upisi)
	}
	if l := o.vdListIzBaze(t, pero, danas); l != nil {
		t.Errorf("akt za dan s listom upisan na današnji: %+v", l)
	}

	predsilvestrovo := silvestrovo.AddDate(0, 0, -1)
	akt.Vrijedi = predsilvestrovo.Add(20 * time.Hour)
	if upozorenja := o.vs.UpisiIzAkta(ctx, akt, predsilvestrovo, "redovna obrana"); len(upozorenja) != 0 {
		t.Fatalf("akt za dan arhivirane knjige bez lista: %v", upozorenja)
	}
	if l := o.vdListIzBaze(t, pero, predsilvestrovo); l != nil {
		t.Errorf("akt je otvorio list u arhiviranoj knjizi: %+v", l)
	}
	if l := o.vdListIzBaze(t, pero, danas); l == nil || len(l.Upisi) != 1 || !strings.Contains(l.Upisi[0].Tekst, "za taj dan nema dnevnog lista, pa je upisan na današnji.") {
		t.Errorf("akt za dan bez lista u arhiviranoj knjizi: %+v", l)
	}
}

// Objavljena prijava s terena za dan događaja koji je prošao ide na list tog
// dana kad postoji, a kad ga nema, na današnji list, i uz naslov stoji dan
// događaja. List za prošli dan ne nastaje.
func TestPrijavaZaProsliDanNaDanasnjiList(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	pero, users := o.vdVodocuvarUBazi(t)
	// prijava pazi na stvarni sat (dan događaja najviše sutra), pa i dnevnik
	o.vs.sad = nil
	n := time.Now().In(models.Zagreb)
	danas := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, models.Zagreb)
	jucer, prekjucer := danas.AddDate(0, 0, -1), danas.AddDate(0, 0, -2)
	if prekjucer.Year() != danas.Year() {
		t.Skip("prekjučer je u knjizi arhiviranoj istekom godine")
	}
	radio := &models.VodocuvarskiList{UserID: pero.ID.String(), Ime: "Pero Perić", Sektor: "P", AreaID: 1, Datum: prekjucer, Od: "07:00", Do: "15:00", Broj: 1, Opis: "obilazak nasipa"}
	if err := o.repo.Save(ctx, radio); err != nil {
		t.Fatal(err)
	}
	prijave := NewPrijavaService(repository.NewPrijavaRepository(o.baza, ledger.New(o.baza, "test")), users, o.vs, "cvor-probni")
	pdf := func(*models.PrijavaSTerena) ([]byte, error) {
		return []byte("%PDF-1.4\n% probna prijava\n%%EOF\n"), nil
	}
	objavi := func(dan time.Time) *models.PrijavaSTerena {
		t.Helper()
		p, err := prijave.Spremi(ctx, pero, "", UnosPrijave{Vrsta: models.PrijavaObavijest, Naslov: "Oštećena rampa", Opis: "odrezana brava na rampi nasipa", Datum: dan})
		if err != nil {
			t.Fatal(err)
		}
		p, upozorenje, err := prijave.Objavi(ctx, pero, p.ID, pdf)
		if err != nil || upozorenje != "" {
			t.Fatalf("objava prijave za %s: %v %s", dan.Format("02.01.2006."), err, upozorenje)
		}
		return p
	}

	p := objavi(jucer)
	if l := o.vdListIzBaze(t, pero, jucer); l != nil {
		t.Errorf("prijava je otvorila list za prošli dan: %+v", l)
	}
	l := o.vdListIzBaze(t, pero, danas)
	if l == nil || p.ListID != l.ID || p.ListBroj != 2 || l.Broj != 2 || len(l.Prijave) != 1 ||
		l.Prijave[0].Naslov != "Oštećena rampa (događaj od "+jucer.Format("02.01.2006.")+")" {
		t.Fatalf("prijava za dan bez lista: prijava na listu %s br. %d, današnji list %+v", p.ListID, p.ListBroj, l)
	}

	p = objavi(prekjucer)
	if l := o.vdListIzBaze(t, pero, prekjucer); p.ListID != radio.ID || p.ListBroj != 1 || len(l.Prijave) != 1 || l.Prijave[0].Naslov != "Oštećena rampa" {
		t.Errorf("prijava za dan s listom: prijava na listu %s, list %+v", p.ListID, l)
	}
}

// Preneseni listovi iz ranije evidencije (BP16, „Preneseno”) ostaju kakvi
// jesu: za njihov dan list se ne otvara ponovno, ne mijenjaju se i ne
// dobivaju broj.
func TestPreneseniListZaProsliDan(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	u := vdVodocuvar()
	stari := vdDan(time.March, 2)
	prenesen := &models.VodocuvarskiList{UserID: u.ID.String(), Ime: "Pero Perić", Sektor: "P", AreaID: 1, Datum: stari,
		Opis: "obilazak iz stare evidencije", Rekonstrukcija: true, Izvor: "probni-zapis-1"}
	if err := o.repo.Save(ctx, prenesen); err != nil {
		t.Fatal(err)
	}
	o.naDan(vdDan(time.March, 10))
	if l, err := o.vs.Pripremi(ctx, u, stari); err != nil || l.ID != prenesen.ID {
		t.Fatalf("prenesen list za prošli dan: %+v %v", l, err)
	}
	_, err := o.vs.Spremi(ctx, u, stari, vdUnos("novi opis"), false)
	vdGreska(t, err, "prenesen je iz ranije evidencije i ne mijenja se")
	if l, err := o.vs.Spremi(ctx, u, vdDan(time.March, 10), vdUnos(""), false); err != nil || l.Broj != 1 {
		t.Fatalf("današnji list: %+v %v", l, err)
	}
	if l := o.vdListIzBaze(t, u, stari); l.Broj != 0 || l.Opis != "obilazak iz stare evidencije" {
		t.Errorf("prenesen list promijenjen: %+v", l)
	}
}

// Prenesen list iz ranije evidencije (BP16, „Preneseno”) ne dobiva redni
// broj ni kad mu netko nešto upiše (Odluke (8. 10. 2026.), pitanje 6, uz
// pravilo da preneseni listovi ostaju kakvi jesu). Akt o obrani koji vrijedi
// od dana s prenesenim listom ne mijenja taj list, nego ide na današnji, uz
// napomenu od kada vrijedi i zašto je ondje. Rukovoditeljeva bilješka za
// taj dan i dalje ide na prenesen list, kao i prije odluke, ali bez broja.
func TestPreneseniListBezBrojaIAkta(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	pero := o.vdVodocuvarSDuznosti(t)
	stari := vdDan(time.March, 2)
	prenesen := &models.VodocuvarskiList{UserID: pero.ID.String(), Ime: "Pero Perić", Sektor: "P", AreaID: 1, Datum: stari,
		Opis: "obilazak iz stare evidencije", Rekonstrukcija: true, Izvor: "probni-zapis-1"}
	if err := o.repo.Save(ctx, prenesen); err != nil {
		t.Fatal(err)
	}
	danas := vdDan(time.March, 10)
	o.naDan(danas)

	akt := &models.Akt{Sektor: "P", AreaID: 1, Broj: 5, Godina: stari.Year(), Vrijedi: stari.Add(14 * time.Hour),
		OvjerioID: "01900000-0000-7000-8000-0000000017a2", Ovjerio: "Ana Anić"}
	if upozorenja := o.vs.UpisiIzAkta(ctx, akt, stari, "redovna obrana"); len(upozorenja) != 0 {
		t.Fatalf("akt za dan s prenesenim listom: %v", upozorenja)
	}
	if l := o.vdListIzBaze(t, pero, stari); l.ID != prenesen.ID || l.Broj != 0 || len(l.Upisi) != 0 {
		t.Errorf("akt je promijenio prenesen list: broj %d, upisi %+v", l.Broj, l.Upisi)
	}
	l := o.vdListIzBaze(t, pero, danas)
	napomena := "Obrana od poplava: redovna obrana Akt vrijedi od " + stari.Format("02.01.2006.") + " u 14:00; list tog dana prenesen je iz ranije evidencije, pa je akt upisan na današnji."
	if l == nil || l.Broj != 1 || len(l.Upisi) != 1 || l.Upisi[0].Tekst != napomena {
		t.Fatalf("akt na današnjem listu: %+v", l)
	}

	rukovoditelj := vdPperic(vdDuznost(models.RoleAreaLeader, 1, true))
	rukovoditelj.Username, rukovoditelj.FullName = "mmarkic", "Marko Markić"
	perms := models.NewUserPermissions(*rukovoditelj)
	for _, listID := range []string{"", prenesen.ID} {
		if l, err := o.vs.UpisiNaList(ctx, perms, rukovoditelj, pero.ID.String(), stari, listID, "obilazak potvrđen"); err != nil || l.ID != prenesen.ID || l.Broj != 0 {
			t.Fatalf("bilješka za dan s prenesenim listom (stranica %q): %+v %v", listID, l, err)
		}
	}
	if l := o.vdListIzBaze(t, pero, stari); l.Broj != 0 || len(l.Upisi) != 2 || !l.Rekonstrukcija || l.Opis != "obilazak iz stare evidencije" {
		t.Errorf("prenesen list nakon bilješke: %+v", l)
	}
}

// Objavljena prijava s terena za dan s prenesenim listom ne mijenja taj
// list, nego ide na današnji, uz dan događaja (Odluke (8. 10. 2026.),
// pitanje 6).
func TestPrijavaZaDanSPrenesenimListom(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	pero, users := o.vdVodocuvarUBazi(t)
	// prijava pazi na stvarni sat (dan događaja najviše sutra), pa i dnevnik
	o.vs.sad = nil
	n := time.Now().In(models.Zagreb)
	danas := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, models.Zagreb)
	jucer := danas.AddDate(0, 0, -1)
	if jucer.Year() != danas.Year() {
		t.Skip("jučer je u knjizi arhiviranoj istekom godine")
	}
	prenesen := &models.VodocuvarskiList{UserID: pero.ID.String(), Ime: "Pero Perić", Sektor: "P", AreaID: 1, Datum: jucer,
		Opis: "obilazak iz stare evidencije", Rekonstrukcija: true, Izvor: "probni-zapis-1"}
	if err := o.repo.Save(ctx, prenesen); err != nil {
		t.Fatal(err)
	}
	prijave := NewPrijavaService(repository.NewPrijavaRepository(o.baza, ledger.New(o.baza, "test")), users, o.vs, "cvor-probni")
	p, err := prijave.Spremi(ctx, pero, "", UnosPrijave{Vrsta: models.PrijavaObavijest, Naslov: "Oštećena rampa", Opis: "odrezana brava na rampi nasipa", Datum: jucer})
	if err != nil {
		t.Fatal(err)
	}
	p, upozorenje, err := prijave.Objavi(ctx, pero, p.ID, func(*models.PrijavaSTerena) ([]byte, error) {
		return []byte("%PDF-1.4\n% probna prijava\n%%EOF\n"), nil
	})
	if err != nil || upozorenje != "" {
		t.Fatalf("objava prijave: %v %s", err, upozorenje)
	}
	if l := o.vdListIzBaze(t, pero, jucer); l.ID != prenesen.ID || l.Broj != 0 || len(l.Prijave) != 0 {
		t.Errorf("prijava je promijenila prenesen list: broj %d, prijave %+v", l.Broj, l.Prijave)
	}
	l := o.vdListIzBaze(t, pero, danas)
	if l == nil || p.ListID != l.ID || l.Broj != 1 || len(l.Prijave) != 1 ||
		l.Prijave[0].Naslov != "Oštećena rampa (događaj od "+jucer.Format("02.01.2006.")+")" {
		t.Fatalf("prijava za dan s prenesenim listom: prijava na listu %s, današnji list %+v", p.ListID, l)
	}
}
