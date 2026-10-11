package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Upozorenje na nepredane listove, Odluke (11. 10. 2026.): „ako vodočuvar
// nije potpisao neki list i predao ga dobije upozorenje”. List ulazi kad mu
// je dan prošao (od 00:00 idućeg dana), a nije predan, i kad je nacrt
// otvoren unaprijed; ne ulaze današnji i budući nacrti, predani i preneseni
// listovi, dan s predanim listom, knjiga arhivirana istekom godine ni tuđi
// listovi. Osobe su izmišljene: vodočuvari Pero Perić i Ivo Ivić.

// danKnjige2026 je dan u knjizi 2026.; sat servisa u ovim testovima stoji
// u listopadu 2026.
func danKnjige2026(mjesec time.Month, dan int) time.Time {
	return time.Date(2026, mjesec, dan, 0, 0, 0, 0, models.Zagreb)
}

// upisiListZaUpozorenje upisuje list izravno u repozitorij, s vlastitom
// oznakom i trenutkom nastanka; izmjene ga čine predanim ili prenesenim
func (o *okolinaVodocuvara) upisiListZaUpozorenje(t *testing.T, u *models.User, dan, nastao time.Time, izmjene ...func(*models.VodocuvarskiList)) *models.VodocuvarskiList {
	t.Helper()
	l := &models.VodocuvarskiList{ID: uuid.Must(uuid.NewV7()).String(), UserID: u.ID.String(), Ime: u.FullName, Sektor: "P", AreaID: 1,
		Datum: dan, Od: "07:00", Do: "15:00", Opis: "obilazak nasipa", CreatedAt: nastao}
	for _, izmijeni := range izmjene {
		izmijeni(l)
	}
	if err := o.repo.Save(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	return l
}

func predanList(l *models.VodocuvarskiList) {
	kad := l.CreatedAt.Add(8 * time.Hour)
	l.Broj, l.PredanoAt = 1, &kad
}

func prenesenList(l *models.VodocuvarskiList) { l.Rekonstrukcija, l.Izvor = true, "BP16" }

// idNepredanih su oznake nepredanih listova osobe, redom kako ih servis daje
func (o *okolinaVodocuvara) idNepredanih(t *testing.T, u *models.User) []string {
	t.Helper()
	listovi, err := o.vs.NepredaniListovi(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range listovi {
		out = append(out, l.ID)
	}
	return out
}

func usporediOznake(t *testing.T, sto string, dobiveno []string, ocekivano ...string) {
	t.Helper()
	if len(dobiveno) != len(ocekivano) {
		t.Fatalf("%s: %v, očekivano %v", sto, dobiveno, ocekivano)
	}
	for i := range ocekivano {
		if dobiveno[i] != ocekivano[i] {
			t.Fatalf("%s: %v, očekivano %v", sto, dobiveno, ocekivano)
		}
	}
}

func TestNepredaniListovi(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	pero := vdVodocuvar()
	ivo := vdVodocuvar()
	ivo.Username, ivo.FullName = "iivic", "Ivo Ivić"
	listopad := func(dan int) time.Time { return danKnjige2026(time.October, dan) }
	ujutro := func(dan time.Time) time.Time { return dan.Add(8 * time.Hour) }

	// dva nacrta za isti dan: upozorenje vodi na stariji, kao spremanje
	stariji := o.upisiListZaUpozorenje(t, pero, listopad(4), ujutro(listopad(4)))
	o.upisiListZaUpozorenje(t, pero, listopad(4), ujutro(listopad(4)).Add(time.Hour))
	// nacrt uz predan list istog dana se ne predaje
	o.upisiListZaUpozorenje(t, pero, listopad(5), ujutro(listopad(5)))
	o.upisiListZaUpozorenje(t, pero, listopad(5), ujutro(listopad(5)).Add(time.Hour), predanList)
	o.upisiListZaUpozorenje(t, pero, listopad(6), ujutro(listopad(6)), prenesenList)
	o.upisiListZaUpozorenje(t, pero, listopad(7), ujutro(listopad(7)), predanList)
	osmi := o.upisiListZaUpozorenje(t, pero, listopad(8), ujutro(listopad(8)))
	unaprijed := o.upisiListZaUpozorenje(t, pero, listopad(9), ujutro(listopad(1)))
	jucer := o.upisiListZaUpozorenje(t, pero, listopad(10), ujutro(listopad(10)))
	o.upisiListZaUpozorenje(t, pero, listopad(11), ujutro(listopad(11)))
	o.upisiListZaUpozorenje(t, pero, listopad(15), ujutro(listopad(1)))
	// knjiga 2025. je arhivirana: u nju se više ne upisuje ni ne predaje
	o.upisiListZaUpozorenje(t, pero, danKnjige2026(time.December, 31).AddDate(-1, 0, 0), ujutro(danKnjige2026(time.December, 31).AddDate(-1, 0, 0)))
	o.upisiListZaUpozorenje(t, ivo, listopad(8), ujutro(listopad(8)))

	// list koji je otvorio samo program (akt, „Upiši” rukovoditelja), s
	// radnim vremenom koje program sam upiše, ne upozorava: vodočuvar taj
	// dan nije radio. Njegov tekst ili objavljena prijava upozoravaju.
	samoProgram := func(l *models.VodocuvarskiList) { l.Opis, l.NapisanoAt = "", nil }
	o.upisiListZaUpozorenje(t, pero, listopad(2), ujutro(listopad(2)), samoProgram)
	sPrijavom := o.upisiListZaUpozorenje(t, pero, listopad(3), ujutro(listopad(3)), samoProgram,
		func(l *models.VodocuvarskiList) { l.Prijave = []models.PrijavaNaListu{{ID: "prijava-1"}} })

	o.vs.SetSat(func() time.Time { return listopad(11).Add(10 * time.Hour) })
	usporediOznake(t, "11. 10. u 10 h", o.idNepredanih(t, pero), sPrijavom.ID, stariji.ID, osmi.ID, unaprijed.ID, jucer.ID)

	// dan lista prolazi u 00:00 idućeg dana
	o.vs.sad = func() time.Time { return listopad(11).Add(-time.Second) }
	usporediOznake(t, "10. 10. u 23:59:59", o.idNepredanih(t, pero), sPrijavom.ID, stariji.ID, osmi.ID, unaprijed.ID)
	o.vs.sad = func() time.Time { return listopad(11) }
	usporediOznake(t, "11. 10. u 00:00", o.idNepredanih(t, pero), sPrijavom.ID, stariji.ID, osmi.ID, unaprijed.ID, jucer.ID)

	// tko ne vodi dnevnik (zaduženje vodočuvara mu je isteklo) list više
	// ne predaje, pa ni upozorenja nema
	bivsi := vdPperic(vdDuznost(models.RoleAreaLeader, 1, true))
	bivsi.ID = pero.ID
	usporediOznake(t, "bez zaduženja vodočuvara", o.idNepredanih(t, bivsi))
	usporediOznake(t, "bez osobe", o.idNepredanih(t, nil))
}

// Predajom lista upozorenje za njegov dan nestaje; nacrt otvoren na svoj
// dan ulazi u upozorenje tek kad dan prođe
func TestPredajaMiceUpozorenje(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	pero := vdVodocuvar()
	dan := vdDan(time.March, 10)
	o.naDan(dan)
	l, err := o.vs.Spremi(ctx, pero, dan, vdUnos("obilazak nasipa"), false)
	if err != nil {
		t.Fatal(err)
	}
	usporediOznake(t, "nacrt na svoj dan", o.idNepredanih(t, pero))
	o.naDan(dan.AddDate(0, 0, 1))
	usporediOznake(t, "nacrt idući dan", o.idNepredanih(t, pero), l.ID)
	if _, err := o.vs.Spremi(ctx, pero, dan, vdUnos("obilazak nasipa"), true); err != nil {
		t.Fatal(err)
	}
	usporediOznake(t, "predan list", o.idNepredanih(t, pero))
}

// Kad se knjiga ne može pročitati, servis javlja grešku, a ne prazan popis
func TestNepredaniListoviGreskaCitanja(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	o.baza.Close()
	if _, err := o.vs.NepredaniListovi(context.Background(), vdVodocuvar()); err == nil {
		t.Fatal("nepročitana knjiga bez greške")
	}
}
