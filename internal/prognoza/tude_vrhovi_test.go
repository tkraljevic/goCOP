package prognoza

import (
	"context"
	"database/sql"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// dunavOdWildungsmauera slaže lanac wildungsmauer → nagybajcs (3 h, nagib 1)
// s izmišljenim očitanjima do zadnji, a u tuđe prognoze upisuje austrijsku
// za Wildungsmauer izdanu u zadnjem satu: raste 10 cm na sat, 12 sati.
func dunavOdWildungsmauera(t *testing.T, zadnji time.Time) *Osvjezivac {
	t.Helper()
	ocitanja := ocitanjaLetvi(t, zadnji, probnaLetva{sifra: "wildungsmauer"}, probnaLetva{sifra: "nagybajcs"})
	baza := otvoriProbnu(t, "p.db")
	pojasi := []Pojas{{Letva: "nagybajcs", Velicina: "vodostaj", Od: -1000, Do: 1000, Rasap: 3,
		Ulazi: []Ulaz{{Letva: "wildungsmauer", Velicina: "vodostaj", PomakH: 3, Sirina: 1, Nagib: 1}}}}
	if err := Spremi(baza, pojasi, "proba"); err != nil {
		t.Fatal(err)
	}
	upisiTudu(t, baza, PodrijetloNOEL, "wildungsmauer", zadnji.Unix()/3600, 0, 12, 100, 10)
	return &Osvjezivac{Baza: baza, Ocitanja: ocitanja, Najdalje: 12, Model: ModelLanac, Iznova: true}
}

// upisiTudu upisuje tuđu prognozu letve izdanu u satu izdano, za sate od
// izdano+prvi do izdano+zadnji: od vrijednosti v0 raste korak po satu.
func upisiTudu(t *testing.T, baza *sql.DB, izvor, letva string, izdano, prvi, zadnji int64, v0, korak float64) {
	t.Helper()
	for h := prvi; h <= zadnji; h++ {
		if _, err := baza.Exec(`INSERT INTO tude (izvor, letva, izdano, ciljni, vrijednost) VALUES (?,?,?,?,?)`,
			izvor, letva, izdano, izdano+h, v0+korak*float64(h-prvi)); err != nil {
			t.Fatal(err)
		}
	}
}

// Wildungsmauer više ne slijedi austrijsku prognozu: ni kad je svježa stoji
// u bazi, Nagybajcs se računa iz zadnjeg mjerenja vrha. Ni kad je nema, uz
// izdanje nema upozorenja da vrhu fali budućnost; to je od 21. 10. 2026.,
// kad je noel.gv.at prestao davati prognozu, stajalo uz svako izdanje. Da je
// Wildungsmauer i dalje na popisu, ista bi ga prognoza vodila.
func TestWildungsmauerNeSlijediAustrijskuPrognozu(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	o := dunavOdWildungsmauera(t, zadnji)
	bezAustrijske := func(ime string, ishod *Ishod) {
		t.Helper()
		if slices.Contains(ishod.TudiVrhovi, "wildungsmauer") {
			t.Errorf("%s: Wildungsmauer je dobio tuđu budućnost: %v", ime, ishod.TudiVrhovi)
		}
		if _, kraj := izdanihLetve(ishod, "nagybajcs"); kraj == nil || kraj.Vrijednost != 100 {
			t.Errorf("%s: Nagybajcs za 12 h mora stajati na zadnjem mjerenju vrha (100): %+v", ime, kraj)
		}
		for _, u := range ishod.Upozorenja {
			if u.Letva == "wildungsmauer" || strings.Contains(u.Opis, PodrijetloNOEL) {
				t.Errorf("%s: upozorenje o austrijskoj prognozi: %v", ime, u)
			}
		}
	}

	ishod, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	bezAustrijske("austrijska u bazi", ishod)

	if _, err := o.Baza.Exec(`DELETE FROM tude`); err != nil {
		t.Fatal(err)
	}
	if ishod, err = o.Osvjezi(context.Background()); err != nil {
		t.Fatal(err)
	}
	bezAustrijske("austrijske nema", ishod)

	upisiTudu(t, o.Baza, PodrijetloNOEL, "wildungsmauer", zadnji.Unix()/3600, 0, 12, 100, 10)
	postavi(t, &VrhoviSTudomPrognozom, map[string]string{"wildungsmauer": PodrijetloNOEL})
	ishod, err = o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, kraj := izdanihLetve(ishod, "nagybajcs"); !slices.Contains(ishod.TudiVrhovi, "wildungsmauer") ||
		kraj == nil || kraj.Vrijednost != 190 {
		t.Errorf("s Wildungsmauerom na popisu Nagybajcs mora slijediti austrijsku (190): vrhovi %v, %+v",
			ishod.TudiVrhovi, kraj)
	}
}

// Vrh lanca budućnost tuđe prognoze slijedi od sata izdavanja
// (Racunalo.U). Tuđa prognoza ispred računa izdana poslije sata izdavanja,
// koja počinje tek iza njega, zato ne smije voditi letvu: srednja bi ostala
// bez računa i stajala na zadnjem mjerenju, a popis tuđih vrhova tvrdio bi
// da je vodi tuđa. Kad u dva dana prije ima izdanje koje sat izdavanja
// pokriva, uzima se ono, iako nije najnovije.
func TestTudaKojaPocinjeIzaSataIzdavanja(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	o := lanacSaSrednjom(t, zadnji)
	o.Iznova = true
	sada := zadnji.Unix() / 3600

	upisiTudu(t, o.Baza, "probni-izvor", "srednja", sada+3, 0, 12, 95, 5)
	ishod, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := izdanihLetve(ishod, "srednja"); len(ishod.TudiVrhovi) != 0 || n < 2 {
		t.Errorf("tuđa počinje iza sata izdavanja: srednja mora zadržati račun, vrhovi %v, izdanih %d",
			ishod.TudiVrhovi, n)
	}
	if !reflect.DeepEqual(ishod.BezTude, map[string]RazlogBezTude{"srednja": {Zadnje: sada + 3}}) ||
		!imaUpozorenje(ishod, VrstaBezTude, "srednja") {
		t.Errorf("srednju računa naš lanac, a uz izdanje to ne piše: bez tuđe %v, upozorenja %v", ishod.BezTude, ishod.Upozorenja)
	}

	upisiTudu(t, o.Baza, "probni-izvor", "srednja", sada-20, 0, 48, -5, 5) // u satu izdavanja 95
	ishod, err = o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ishod.TudiVrhovi) != 1 || ishod.TudiVrhovi[0] != "srednja" {
		t.Fatalf("starija tuđa pokriva sat izdavanja: srednja mora biti tuđi vrh, vrhovi %v", ishod.TudiVrhovi)
	}
	if _, kraj := izdanihLetve(ishod, "donja"); kraj == nil || kraj.Vrijednost != 95+5*10 {
		t.Errorf("donja za 12 h mora slijediti stariju tuđu prognozu srednje (145): %+v", kraj)
	}
	if len(ishod.BezTude) != 0 || imaUpozorenje(ishod, VrstaBezTude, "srednja") {
		t.Errorf("srednju vodi tuđa, a uz izdanje piše da je računa naš lanac: %v", ishod.Upozorenja)
	}
}

// imaUpozorenje javlja ima li izdanje upozorenje zadane vrste za letvu.
func imaUpozorenje(ishod *Ishod, vrsta, letva string) bool {
	return slices.ContainsFunc(ishod.Upozorenja, func(u Upozorenje) bool { return u.Vrsta == vrsta && u.Letva == letva })
}

// Kad mađarska prognoza Komároma nije stigla ili je starija od dva dana,
// Komárom računa naš lanac, i uz izdanje to piše; dok je svježa, ne piše
// ništa. Izdana točno dva dana prije sata izdanja još je svježa, sat
// starija nije. Ovdje je srednja letva u ulozi Komároma.
func TestBezTudeUIzdanju(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	o := lanacSaSrednjom(t, zadnji)
	o.Iznova = true
	sada := zadnji.Unix() / 3600

	for _, tc := range []struct {
		ime     string
		izdano  int64 // 0: tuđe prognoze nema
		bezTude map[string]RazlogBezTude
	}{
		{"nije stigla", 0, map[string]RazlogBezTude{"srednja": {}}},
		{"starija od dva dana", sada - 60, map[string]RazlogBezTude{"srednja": {Zadnje: sada - 60}}},
		{"sat starija od dva dana", sada - 49, map[string]RazlogBezTude{"srednja": {Zadnje: sada - 49}}},
		{"točno dva dana", sada - 48, map[string]RazlogBezTude{}},
		{"svježa", sada - 12, map[string]RazlogBezTude{}},
	} {
		if _, err := o.Baza.Exec(`DELETE FROM tude`); err != nil {
			t.Fatal(err)
		}
		if tc.izdano != 0 {
			upisiTudu(t, o.Baza, "probni-izvor", "srednja", tc.izdano, 0, 72, 95, 1)
		}
		ishod, err := o.Osvjezi(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(ishod.BezTude, tc.bezTude) || imaUpozorenje(ishod, VrstaBezTude, "srednja") != (len(tc.bezTude) > 0) {
			t.Errorf("%s: bez tuđe %v, upozorenja %v", tc.ime, ishod.BezTude, ishod.Upozorenja)
		}
		if vodi := slices.Contains(ishod.TudiVrhovi, "srednja"); vodi != (len(tc.bezTude) == 0) {
			t.Errorf("%s: vodi li srednju tuđa %v, vrhovi %v", tc.ime, vodi, ishod.TudiVrhovi)
		}
	}
}

// Tuđa izdana najviše šest sati iza sata izdanja vodi srednju, izdana sedam
// sati iza nje ne vodi, iako sat izdanja pokriva; uz izdanje tada piše
// kad je izdana.
func TestTudaDoSestSatiIzaSataIzdanja(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	o := lanacSaSrednjom(t, zadnji)
	o.Iznova = true
	sada := zadnji.Unix() / 3600

	upisiTudu(t, o.Baza, "probni-izvor", "srednja", sada+6, -8, 12, 95, 5) // od sada−2
	ishod, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(ishod.TudiVrhovi, "srednja") || len(ishod.BezTude) != 0 {
		t.Errorf("izdana šest sati iza: srednju mora voditi tuđa, vrhovi %v, bez tuđe %v", ishod.TudiVrhovi, ishod.BezTude)
	}

	if _, err := o.Baza.Exec(`DELETE FROM tude`); err != nil {
		t.Fatal(err)
	}
	upisiTudu(t, o.Baza, "probni-izvor", "srednja", sada+7, -8, 12, 95, 5) // od sada−1
	if ishod, err = o.Osvjezi(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n, _ := izdanihLetve(ishod, "srednja"); slices.Contains(ishod.TudiVrhovi, "srednja") || n < 2 {
		t.Errorf("izdana sedam sati iza: srednja mora zadržati račun, vrhovi %v, izdanih %d", ishod.TudiVrhovi, n)
	}
	want := "zadnja tuđa prognoza (probni-izvor) izdana je " + satOpisa(sada+7) + ", više od šest sati iza sata izdanja" + bezTudeRacuna
	if u := upozorenjeLetve(ishod, VrstaBezTude, "srednja"); u.Opis != want {
		t.Errorf("izdana sedam sati iza: opis %q, želim %q", u.Opis, want)
	}
}

// Kad je tuđa za sat izdanja svježa, a letvu ipak računa naš lanac, uzrok
// nije tuđa prognoza, nego mjerenje letve: bez njega je tuđa nema od čega
// voditi, a kad kasni, letva bi kao vrh lanca imala raniji sat izdanja, za
// koji tuđa ne vrijedi.
func TestBezTudeKadJeSvjeza(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	sada := zadnji.Unix() / 3600
	for _, tc := range []struct {
		ime     string
		mjereno time.Time // zadnje mjerenje srednje; nula: nema ga
		razlog  RazlogBezTude
		opis    string
	}{
		{"bez mjerenja", time.Time{}, RazlogBezTude{Zadnje: sada - 1, Svjeza: true},
			"tuđa prognoza (probni-izvor) je svježa, ali srednja nema mjerenja od kojeg bi ga vodila" + bezTudeRacuna},
		{"mjerenje kasni", zadnji.Add(-6 * time.Hour), RazlogBezTude{Zadnje: sada - 1, Svjeza: true, KaoVrh: sada - 6},
			"kad bi ga vodila tuđa prognoza (probni-izvor), sat izdanja bio bi " + satOpisa(sada-6) +
				", a za taj sat ona ne vrijedi" + bezTudeRacuna},
	} {
		o := lanacSaSrednjom(t, zadnji)
		o.Iznova = true
		if _, err := o.Ocitanja.Exec(`DELETE FROM readings WHERE station_id = '3' AND measured_at > ?`, tc.mjereno); err != nil {
			t.Fatal(err)
		}
		upisiTudu(t, o.Baza, "probni-izvor", "srednja", sada-1, -1, 72, 95, 1) // od sada−2
		ishod, err := o.Osvjezi(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if ishod.Sada != sada || !reflect.DeepEqual(ishod.BezTude, map[string]RazlogBezTude{"srednja": tc.razlog}) {
			t.Errorf("%s: sat izdanja %d (želim %d), bez tuđe %v", tc.ime, ishod.Sada, sada, ishod.BezTude)
		}
		if u := upozorenjeLetve(ishod, VrstaBezTude, "srednja"); u.Opis != tc.opis {
			t.Errorf("%s: opis %q\nželim %q", tc.ime, u.Opis, tc.opis)
		}
	}
}

// upozorenjeLetve je upozorenje izdanja zadane vrste za letvu.
func upozorenjeLetve(ishod *Ishod, vrsta, letva string) Upozorenje {
	i := slices.IndexFunc(ishod.Upozorenja, func(u Upozorenje) bool { return u.Vrsta == vrsta && u.Letva == letva })
	if i < 0 {
		return Upozorenje{}
	}
	return ishod.Upozorenja[i]
}

// Kad se zadnje tuđe izdanje ne da pročitati, letva se ipak javlja, kao da
// tuđe nema; letva bez svojeg računa ne javlja se.
func TestBezTudeNecitljivaBaza(t *testing.T) {
	postavi(t, &TudaIspredRacuna, map[string]string{"srednja": "probni-izvor", "gornja": "probni-izvor"})
	baza := otvoriProbnu(t, "p.db")
	baza.Close()
	o := &Osvjezivac{Baza: baza}
	got := o.bezTude(map[string][]Pojas{"srednja": {{Letva: "srednja"}}}, map[string]int64{}, 100)
	if !reflect.DeepEqual(got, map[string]RazlogBezTude{"srednja": {}}) {
		t.Errorf("bez tuđe %v", got)
	}
}

// tudaUSatu uzima najnovije izdanje u razdoblju koje ima vrijednost u satu i
// barem jednu poslije njega: izdanje koje u tom satu završava ili tek
// počinje iza njega preskače se, a bez ijednog takvog prognoze nema.
func TestTudaUSatu(t *testing.T) {
	db := otvoriProbnu(t, "p.db")
	upisiTudu(t, db, "probni-izvor", "srednja", 100, 0, 20, 50, 1) // 100–120
	upisiTudu(t, db, "probni-izvor", "srednja", 104, 0, 6, 80, 1)  // 104–110, u 110 završava
	upisiTudu(t, db, "probni-izvor", "srednja", 106, 5, 9, 90, 1)  // 111–115, počinje iza 110
	n, ok, err := tudaUSatu(db, "probni-izvor", "srednja", 90, 120, 110)
	if err != nil || !ok {
		t.Fatalf("ok %v, %v", ok, err)
	}
	if v, _ := n.U(110); v != 60 || len(n.Sati) != 21 {
		t.Errorf("u 110. satu %g iz %d sati, želim 60 iz izdanja 100 (21 sat)", v, len(n.Sati))
	}
	for _, r := range [][2]int64{{90, 99}, {104, 120}} {
		if _, ok, err := tudaUSatu(db, "probni-izvor", "srednja", r[0], r[1], 110); ok || err != nil {
			t.Errorf("izdana %d–%d ne pokrivaju 110. sat: ok %v, %v", r[0], r[1], ok, err)
		}
	}
	db.Close()
	if _, ok, err := tudaUSatu(db, "probni-izvor", "srednja", 90, 120, 110); err == nil || ok {
		t.Errorf("zatvorena baza: ok %v, %v", ok, err)
	}
}
