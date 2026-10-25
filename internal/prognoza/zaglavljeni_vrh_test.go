package prognoza

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gocop/internal/plato"
)

// kvaroviPoLetvi je prepoznavač sa zadanim kvarovima po letvi.
type kvaroviPoLetvi map[string][]plato.Kvar

func (k kvaroviPoLetvi) Kvarovi(_ context.Context, letva string, _ []plato.Tocka) ([]plato.Kvar, error) {
	return k[letva], nil
}

// lanacTriLetve: gornja (vrh lanca) → srednja (bez ijednog očitanja, pa nikad
// svježa) → donja. Jer srednja nema mjerenja, izbor inačica ne može gornju
// zaobići, i gornja ostaje vrh i kad kasni. Očitanja su satna za 48 sati do
// zadnjeg, automatska: gornja 100 + i, donja 90 + i za i sati prije
// zadnjeg.
func lanacTriLetve(t *testing.T, zadnji time.Time) *Osvjezivac {
	t.Helper()
	ocitanja := probneOcitanja(t, 48, zadnji)
	if _, err := ocitanja.Exec(`UPDATE readings SET source = 'UVOZ'; INSERT INTO stations VALUES ('3', 'srednja')`); err != nil {
		t.Fatal(err)
	}
	baza, err := Otvori(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	pojasi := []Pojas{
		{Letva: "srednja", Velicina: "vodostaj", Od: -1000, Do: 1000, Rasap: 3,
			Ulazi: []Ulaz{{Letva: "gornja", Velicina: "vodostaj", PomakH: 3, Sirina: 1, Nagib: 1}}},
		{Letva: "donja", Velicina: "vodostaj", Od: -1000, Do: 1000, Rasap: 3,
			Ulazi: []Ulaz{{Letva: "srednja", Velicina: "vodostaj", PomakH: 3, Sirina: 1, Nagib: 1}}},
	}
	if err := Spremi(baza, pojasi, "proba"); err != nil {
		t.Fatal(err)
	}
	return &Osvjezivac{Baza: baza, Ocitanja: ocitanja, Najdalje: 12, Model: ModelLanac}
}

// Vrh lanca kojem je kraj niza kvar potvrđen susjedom: prognoza sate kvara
// ne uzima, ali sat izdanja ne pada na početak kvara, nego vrh drži zadnju
// vrijednost prije kvara, uz napomenu.
func TestVrhUKvaruNeVracaSatIzdanja(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	o := lanacTriLetve(t, zadnji)
	o.Kvarovi = kvaroviPoLetvi{"gornja": {{Vrsta: plato.Zaglavljen, Od: zadnji.Add(-8 * time.Hour), Do: zadnji, Cm: 100, Traje: true,
		ImaPrije: true, Prije: 109, Susjed: &plato.Pomak{Letva: "susjedna", Cm: 40}}}}
	ishod, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ishod.Sada != zadnji.Unix()/3600 {
		t.Errorf("izdano za %d, a zadnje očitanje je %d", ishod.Sada, zadnji.Unix()/3600)
	}
	if len(ishod.Kvarovi) != 1 || !ishod.Kvarovi[0].Drzi {
		t.Fatalf("kvarovi %+v", ishod.Kvarovi)
	}
	if !slices.ContainsFunc(ishod.Upozorenja, func(u Upozorenje) bool {
		return u.Vrsta == VrstaKvarMjerila && strings.HasSuffix(u.Opis, "; prognoza ga ne uzima, nego drži zadnju vrijednost prije kvara")
	}) {
		t.Errorf("upozorenja %+v", ishod.Upozorenja)
	}
	if !slices.ContainsFunc(ishod.Izdane, func(i Izdana) bool {
		return i.Letva == "gornja" && i.Ciljni == ishod.Sada && i.Vrijednost == 109
	}) {
		t.Error("vrh ne drži vrijednost prije kvara")
	}
}

// Kad vrh lanca počne kasniti, izdanje za sat stariji od zadnjeg se ne
// računa ni ne zapisuje.
func TestNeIzdajeStarijiSat(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	o := lanacTriLetve(t, zadnji)
	ishod, err := o.Osvjezi(context.Background())
	if err != nil || ishod.Preskoceno || ishod.Sada != zadnji.Unix()/3600 {
		t.Fatalf("prvo izdanje %+v %v", ishod, err)
	}
	if err := o.Zapisi(ishod); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Ocitanja.Exec(`DELETE FROM readings WHERE station_id = '1' AND measured_at > ?`, zadnji.Add(-6*time.Hour)); err != nil {
		t.Fatal(err)
	}
	ishod, err = o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ishod.Preskoceno || ishod.Sada >= zadnji.Unix()/3600 {
		t.Errorf("stariji sat: %+v", ishod)
	}
}

// Vrijednost prije kvara: zadnja u nizu prije početka; kad je kvar počeo
// prije niza, razina prije kvara, u protoku samo kroz krivulju.
func TestPrijeKvara(t *testing.T) {
	ctx := context.Background()
	o := &Osvjezivac{}
	sat0 := satDB(10).Unix() / 3600
	n := NoviNiz(map[int64]float64{sat0 - 3: 50, sat0 - 1: 52, sat0: 400})
	k := plato.Kvar{Od: satDB(10), Do: satDB(12), ImaPrije: true, Prije: 51.6}
	if v, ima := o.prijeKvara(ctx, Izvor{Letva: "donji-brod", Velicina: "vodostaj"}, n, k); !ima || v != 52 {
		t.Errorf("iz niza %v %v", v, ima)
	}
	prazan := Niz{}
	if v, ima := o.prijeKvara(ctx, Izvor{Letva: "donji-brod", Velicina: "vodostaj"}, prazan, k); !ima || v != 52 {
		t.Errorf("razina prije %v %v", v, ima)
	}
	if _, ima := o.prijeKvara(ctx, Izvor{Letva: "donji-brod", Velicina: "protok"}, prazan, k); ima {
		t.Error("protok bez krivulje")
	}
	k.ImaPrije = false
	if _, ima := o.prijeKvara(ctx, Izvor{Letva: "donji-brod", Velicina: "vodostaj"}, prazan, k); ima {
		t.Error("bez razine prije")
	}
	arhiva, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { arhiva.Close() })
	o.Arhiva, k.ImaPrije = arhiva, true
	if _, ima := o.prijeKvara(ctx, Izvor{Letva: "donji-brod", Velicina: "protok"}, prazan, k); ima {
		t.Error("protok bez tablice krivulja")
	}
	// drži samo sate kvara bez vrijednosti
	d := drzi(n, k, 52)
	if v, _ := d.U(sat0); v != 400 {
		t.Errorf("popunjen sat prepisan: %v", v)
	}
	if v, _ := d.U(sat0 + 2); v != 52 || len(d.Sati) != 5 {
		t.Errorf("držanje %+v", d)
	}
	// vrh bez vrijednosti prije kvara ne drži ništa
	nizovi := map[Izvor]Niz{{Letva: "gornja", Velicina: "vodostaj"}: prazan}
	b := sBiljeznikomKvarova(ctx)
	zabiljeziKvar(b, KvarMjerila{Letva: "gornja", Kvar: plato.Kvar{Od: satDB(10), Do: satDB(12), Traje: true, Susjed: &plato.Pomak{}}})
	zabiljeziKvar(b, KvarMjerila{Letva: "gornja", Kvar: plato.Kvar{Od: satDB(1), Do: satDB(3), Susjed: &plato.Pomak{}}})
	o.drziVrhoveUKvaru(b, nizovi, map[Izvor]bool{{Letva: "gornja", Velicina: "vodostaj"}: true, {Letva: "druga", Velicina: "vodostaj"}: true})
	if kv := kvaroviIzdanja(b); slices.ContainsFunc(kv, func(k KvarMjerila) bool { return k.Drzi }) {
		t.Errorf("drži bez vrijednosti %+v", kv)
	}
}
