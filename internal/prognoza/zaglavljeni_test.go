package prognoza

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/plato"
)

// kvaroviZadani je prepoznavač koji za letvu vraća zadane kvarove i pamti
// točke koje je dobio.
type kvaroviZadani struct {
	kvarovi []plato.Kvar
	greska  error
	dobio   []plato.Tocka
}

func (k *kvaroviZadani) Kvarovi(_ context.Context, _ string, tocke []plato.Tocka) ([]plato.Kvar, error) {
	k.dobio = tocke
	return k.kvarovi, k.greska
}

// satDB je h sati od ponoći 22. 10. 2026. po zagrebačkom satu.
func satDB(h int) time.Time { return time.Date(2026, 10, 22, h, 0, 0, 0, models.Zagreb) }

// donjiBrodStoji: Donji Brod od 22. 10. u 0 h automatski oko 400 cm, a od
// 5 do 20 h stoji na 412 cm; u 12 h je i ručno upisano očitanje.
func donjiBrodStoji(t *testing.T, k *kvaroviZadani) *Osvjezivac {
	t.Helper()
	db := praznaOcitanjaRacuna(t)
	if _, err := db.Exec(`INSERT INTO stations VALUES ('d', 'donji-brod')`); err != nil {
		t.Fatal(err)
	}
	for h := range 24 {
		cm, izvor := 400+h, models.ReadingSourceImport
		if h >= 5 && h <= 20 {
			cm = 412
		}
		kad := satDB(h)
		if h == 12 {
			if _, err := db.Exec(`INSERT INTO readings (station_id, measured_at, level_cm, source) VALUES ('d', ?, 380, ?)`,
				kad.Add(-time.Minute), models.ReadingSourceManual); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if _, err := db.Exec(`INSERT INTO readings (station_id, measured_at, level_cm, source) VALUES ('d', ?, ?, ?)`, kad, cm, izvor); err != nil {
			t.Fatal(err)
		}
	}
	return &Osvjezivac{Ocitanja: db, Kvarovi: k}
}

func satOd(t time.Time) int64 { return t.Unix() / 3600 }

// Odluke (25. 10. 2026.): kvar koji potvrđuje susjedna letva prognoza ne
// uzima: automatski sati kvara nestanu (rupu premoste postojeći mehanizmi),
// ručno upisano ostaje, a uz izdanje stoji napomena.
func TestZaglavljenSPotvrdomSusjedaNijeUPrognozi(t *testing.T) {
	k := &kvaroviZadani{kvarovi: []plato.Kvar{{Vrsta: plato.Zaglavljen, Od: satDB(5), Do: satDB(20), Cm: 412, ImaPrije: true, Prije: 404,
		Susjed: &plato.Pomak{Letva: "gornji-brod", Cm: 38}}}}
	o := donjiBrodStoji(t, k)
	ctx := sBiljeznikomKvarova(context.Background())
	sati, err := o.ucitajSirovo(ctx, "donji-brod", satDB(0))
	if err != nil {
		t.Fatal(err)
	}
	for h := 5; h <= 20; h++ {
		s, ima := sati[satOd(satDB(h))]
		if h == 12 && (!ima || !s.rucno) {
			t.Error("ručno očitanje u kvaru je izbačeno")
		}
		if h != 12 && ima {
			t.Errorf("sat %d h u kvaru je u prognozi", h)
		}
	}
	for _, h := range []int{0, 4, 21, 23} {
		if _, ima := sati[satOd(satDB(h))]; !ima {
			t.Errorf("sat %d h izvan kvara je izbačen", h)
		}
	}
	if len(k.dobio) != 23 || slices.ContainsFunc(k.dobio, func(x plato.Tocka) bool { return x.Cm == 380 }) {
		t.Errorf("prepoznavač je dobio %d točaka (i ručnu?)", len(k.dobio))
	}
	kvarovi := kvaroviIzdanja(ctx)
	if len(kvarovi) != 1 || !kvarovi[0].Izbacen() {
		t.Fatalf("kvarovi izdanja %+v", kvarovi)
	}
	// ista letva pročitana opet u napomeni stoji jednom
	if _, err := o.ucitajSirovo(ctx, "donji-brod", satDB(0)); err != nil || len(kvaroviIzdanja(ctx)) != 1 {
		t.Errorf("drugo čitanje: %v %d", err, len(kvaroviIzdanja(ctx)))
	}
	u := Upozori(&Ishod{Sada: satOd(satDB(23)), Kvarovi: kvarovi}, nil, nil, nil)
	want := Upozorenje{Vrsta: VrstaKvarMjerila, Tezina: TezinaNapomena, Letva: "donji-brod",
		Opis: "mjerenje je stajalo na 412 cm od 22. 10. u 5 h do 22. 10. u 20 h (15 h), a susjedna letva gornji-brod se u tom vremenu pomaknula za 38 cm; prognoza ga ne uzima"}
	if !slices.ContainsFunc(u, func(x Upozorenje) bool {
		return x.Vrsta == want.Vrsta && x.Opis == want.Opis && x.Letva == want.Letva && x.Tezina == want.Tezina
	}) {
		t.Errorf("upozorenja %+v", u)
	}
}

// Bez potvrde susjeda (u kvar se ušlo skokom) vrijednosti ostaju u računu,
// a napomena kaže da ih prognoza ipak uzima.
func TestZaglavljenBezSusjedaSamoNapomena(t *testing.T) {
	k := &kvaroviZadani{kvarovi: []plato.Kvar{{Vrsta: plato.Zaglavljen, Od: satDB(5), Do: satDB(20), Cm: 412, ImaPrije: true, Prije: 300, Skokom: true}}}
	o := donjiBrodStoji(t, k)
	ctx := sBiljeznikomKvarova(context.Background())
	sati, err := o.ucitajSirovo(ctx, "donji-brod", satDB(0))
	if err != nil {
		t.Fatal(err)
	}
	if _, ima := sati[satOd(satDB(10))]; !ima || len(sati) != 24 {
		t.Errorf("sati bez potvrde susjeda izbačeni: %d", len(sati))
	}
	kv := kvaroviIzdanja(ctx)
	if len(kv) != 1 || kv[0].Izbacen() || !strings.HasSuffix(opisKvara(kv[0]), "; prognoza ga ipak uzima, jer to susjedna letva ne potvrđuje") {
		t.Errorf("kvar %+v: %s", kv, opisKvara(kv[0]))
	}
}

// Kad se kvar ne da provjeriti, prognoza radi s očitanjima kakva jesu; bez
// prepoznavača nema ni provjere ni napomene.
func TestKvarSeNeDaProvjeriti(t *testing.T) {
	o := donjiBrodStoji(t, &kvaroviZadani{greska: errors.New("baza zaključana")})
	ctx := sBiljeznikomKvarova(context.Background())
	sati, err := o.ucitajSirovo(ctx, "donji-brod", satDB(0))
	if err != nil || len(sati) != 24 || len(kvaroviIzdanja(ctx)) != 0 {
		t.Errorf("greška prepoznavača: %d sati, %v", len(sati), err)
	}
	o.Kvarovi = nil
	if sati, err := o.ucitajSirovo(ctx, "donji-brod", satDB(0)); err != nil || len(sati) != 24 {
		t.Errorf("bez prepoznavača %d sati", len(sati))
	}
	if kvaroviIzdanja(context.Background()) != nil {
		t.Error("bez bilježnika ima kvarova")
	}
	zabiljeziKvar(context.Background(), KvarMjerila{Letva: "x"}) // bez bilježnika ne pada
	b := sBiljeznikomKvarova(context.Background())
	for _, k := range []KvarMjerila{{Letva: "donji-brod", Kvar: plato.Kvar{Od: satDB(9)}}, {Letva: "donji-brod", Kvar: plato.Kvar{Od: satDB(2)}}, {Letva: "brod"}} {
		zabiljeziKvar(b, k)
	}
	if kv := kvaroviIzdanja(b); len(kv) != 3 || kv[0].Letva != "brod" || !kv[1].Od.Equal(satDB(2)) {
		t.Errorf("redoslijed %+v", kv)
	}
}

// Napomena stoji dok je kvar u zadnja četiri dana izdanja, jedna po letvi,
// s kvarovima redom.
func TestUpozorenjaKvarova(t *testing.T) {
	sada := satOd(satDB(23))
	staro := KvarMjerila{Letva: "gornji-brod", Kvar: plato.Kvar{Od: satDB(-200), Do: satDB(-100), Cm: 5}}
	prvi := KvarMjerila{Letva: "donji-brod", Kvar: plato.Kvar{Vrsta: plato.Odskocio, Od: satDB(1), Do: satDB(3), Cm: 1385, Prije: -77, ImaPrije: true, Vratio: true, Natrag: satDB(4), Izvan: true}}
	drugi := KvarMjerila{Letva: "donji-brod", Kvar: plato.Kvar{Vrsta: plato.Odskocio, Od: satDB(10), Do: satDB(23), Cm: 1385, Prije: -77, ImaPrije: true, Traje: true, Izvan: true,
		Susjed: &plato.Pomak{Letva: "gornji-brod", Cm: 4}}}
	u := upozorenjaKvarova([]KvarMjerila{staro, prvi, drugi}, sada)
	if len(u) != 1 || u[0].Letva != "donji-brod" {
		t.Fatalf("upozorenja %+v", u)
	}
	want := "mjerenje je 22. 10. u 1 h skočilo s −77 na oko 1385 cm, a nakon 3 h vratilo se na staru razinu; to je daleko izvan uobičajenog raspona letve; prognoza ga ipak uzima, jer to susjedna letva ne potvrđuje; " +
		"mjerenje je 22. 10. u 10 h skočilo s −77 na oko 1385 cm i ondje se drži već 13 h, a susjedna letva gornji-brod se u tom vremenu pomaknula samo za 4 cm; to je daleko izvan uobičajenog raspona letve; prognoza ga ne uzima"
	if u[0].Opis != want {
		t.Errorf("opis\n %q\nželim\n %q", u[0].Opis, want)
	}
}

// Susjedi za kvar mjerila: druga obala u oba smjera i susjedi
// rekonstrukcije, bez ponavljanja.
func TestSusjediKvara(t *testing.T) {
	s := SusjediKvara()
	if !slices.Contains(s["komarom"], "komarno") || !slices.Contains(s["komarno"], "komarom") || !slices.Contains(s["siga"], "bezdan") {
		t.Errorf("susjedi %v", s)
	}
	for l, n := range s {
		if len(slices.Compact(slices.Sorted(slices.Values(n)))) != len(n) {
			t.Errorf("%s ponavlja susjeda: %v", l, n)
		}
	}
}

// Na dnu mjerila (najniža vrijednost iz arhive) kvar potvrđuje susjed, ali
// vrijednosti ostaju u računu: prava razina je najviše toliko.
func TestNaDnuMjerilaOstajeUPrognozi(t *testing.T) {
	k := &kvaroviZadani{kvarovi: []plato.Kvar{{Vrsta: plato.Zaglavljen, Od: satDB(5), Do: satDB(20), Cm: 412, NaDnu: true,
		Susjed: &plato.Pomak{Letva: "gornji-brod", Cm: 42}}}}
	o := donjiBrodStoji(t, k)
	ctx := sBiljeznikomKvarova(context.Background())
	sati, err := o.ucitajSirovo(ctx, "donji-brod", satDB(0))
	if err != nil || len(sati) != 24 {
		t.Fatalf("sati %d %v", len(sati), err)
	}
	kv := kvaroviIzdanja(ctx)
	if len(kv) != 1 || kv[0].Izbacen() || !strings.HasSuffix(opisKvara(kv[0]), "pa je voda možda ispod dosega mjernog uređaja; prognoza ga ipak uzima, jer je voda ispod dosega mjernog uređaja") {
		t.Errorf("kvar %+v: %s", kv, opisKvara(kv[0]))
	}
}
