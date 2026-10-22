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

// Wildungsmauer više ne slijedi austrijsku prognozu (Odluke (22. 10. 2026.)):
// ni kad je svježa stoji u bazi, Nagybajcs se računa iz zadnjeg mjerenja
// vrha, a uz izdanje nema upozorenja da vrhu fali budućnost. Da je
// Wildungsmauer i dalje na popisu, ista bi ga prognoza vodila.
func TestWildungsmauerNeSlijediAustrijskuPrognozu(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	o := dunavOdWildungsmauera(t, zadnji)

	ishod, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(ishod.TudiVrhovi, "wildungsmauer") {
		t.Errorf("Wildungsmauer je dobio tuđu budućnost: %v", ishod.TudiVrhovi)
	}
	if _, kraj := izdanihLetve(ishod, "nagybajcs"); kraj == nil || kraj.Vrijednost != 100 {
		t.Errorf("Nagybajcs za 12 h mora stajati na zadnjem mjerenju vrha (100): %+v", kraj)
	}
	for _, u := range ishod.Upozorenja {
		if u.Letva == "wildungsmauer" || strings.Contains(u.Opis, PodrijetloNOEL) {
			t.Errorf("upozorenje o austrijskoj prognozi: %v", u)
		}
	}

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
	if !reflect.DeepEqual(ishod.BezTude, map[string]int64{"srednja": sada + 3}) || !imaUpozorenje(ishod, VrstaBezTude, "srednja") {
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
// ništa. Ovdje je srednja letva u ulozi Komároma.
func TestBezTudeUIzdanju(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	o := lanacSaSrednjom(t, zadnji)
	o.Iznova = true
	sada := zadnji.Unix() / 3600

	for _, tc := range []struct {
		ime     string
		izdano  int64 // 0: tuđe prognoze nema
		bezTude map[string]int64
	}{
		{"nije stigla", 0, map[string]int64{"srednja": 0}},
		{"starija od dva dana", sada - 60, map[string]int64{"srednja": sada - 60}},
		{"svježa", sada - 12, map[string]int64{}},
	} {
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
	}
}

// Kad se zadnje tuđe izdanje ne da pročitati, letva se ipak javlja, kao da
// tuđe nema; letva bez svojeg računa ne javlja se.
func TestBezTudeNecitljivaBaza(t *testing.T) {
	postavi(t, &TudaIspredRacuna, map[string]string{"srednja": "probni-izvor", "gornja": "probni-izvor"})
	baza := otvoriProbnu(t, "p.db")
	baza.Close()
	o := &Osvjezivac{Baza: baza}
	got := o.bezTude(map[string][]Pojas{"srednja": {{Letva: "srednja"}}}, 100)
	if !reflect.DeepEqual(got, map[string]int64{"srednja": 0}) {
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
