package prognoza

import (
	"context"
	"database/sql"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Osvježavanje u izdanju, na izmišljenim letvama i na nekoliko pravih šifri
// tamo gdje račun ide po njima (druga obala, prenesene i pregledne letve).
// Osvježavanje čita očitanja dva tjedna unatrag od sata računala, pa
// očitanja stoje oko sadašnjeg sata, kao u ostalim testovima osvježavanja.

// konstantniOperater sprema model ispuštanja bez ulaza koji na svim
// dosezima običnog režima predviđa v.
func konstantniOperater(t *testing.T, baza *sql.DB, letva string, v float64) *OperaterModel {
	t.Helper()
	m := konstantniModel(letva, nil, v, false)
	if err := SpremiOperatera(baza, m); err != nil {
		t.Fatal(err)
	}
	return m
}

// Osvježavanje javlja svaku bazu koja se ne da pročitati.
func TestOsvjeziGreskeBaza(t *testing.T) {
	zadnji := time.Now().UTC().Truncate(time.Hour)
	ocitanja := probneOcitanja(t, 48, zadnji)
	ctx := context.Background()

	prazna, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "prazna.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { prazna.Close() })
	if _, err := (&Osvjezivac{Baza: prazna, Ocitanja: ocitanja}).Osvjezi(ctx); err == nil || !strings.Contains(err.Error(), "pojasi") {
		t.Errorf("baza prognoza bez tablica: %v", err)
	}
	for tablica, poruka := range map[string]string{"promasaji": "promasaji", "operateri": "modeli ispuštanja: "} {
		o := probniOsvjezivac(t, ocitanja)
		if _, err := o.Baza.Exec(`DROP TABLE ` + tablica); err != nil {
			t.Fatal(err)
		}
		if _, err := o.Osvjezi(ctx); err == nil || !strings.Contains(err.Error(), poruka) {
			t.Errorf("bez tablice %s: %v", tablica, err)
		}
	}
	if _, err := probniOsvjezivac(t, prazna).Osvjezi(ctx); err == nil || !strings.Contains(err.Error(), "(vodostaj): ") || !strings.Contains(err.Error(), "readings") {
		t.Errorf("očitanja bez tablica: %v", err)
	}
}

// Izdanje u kojem vrh kasni: sat izdavanja čeka zakašnjeli vrh, kraj
// Komároma dolazi s druge obale i to piše uz izdanje, srpska letva nosi
// prenesenu prognozu, a pregledne letve svoje mjerenje, ako ne kasne više
// od ZaostatakVrha.
func TestOsvjeziZamjenaCekanjeIPregledne(t *testing.T) {
	bezIspravka(t)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	sat := zadnji.Unix() / 3600
	ocitanja := praznaOcitanjaRacuna(t)
	for h := 5; h <= 48; h++ {
		dodajOcitanje(t, ocitanja, "komarom", zadnji.Add(-time.Duration(h)*time.Hour), 100, 500.0)
	}
	stalno := func(v int) func(int) int { return func(int) int { return v } }
	// Komárno je 46 cm iznad Komároma, a zadnjih pet sati naraslo je 10 cm.
	dodajSatnaOcitanja(t, ocitanja, "komarno", zadnji.Add(-48*time.Hour), zadnji, func(h int) int {
		if h >= 44 {
			return 156
		}
		return 146
	})
	dodajSatnaOcitanja(t, ocitanja, "kasni", zadnji.Add(-48*time.Hour), zadnji.Add(-5*time.Hour), stalno(70))
	dodajSatnaOcitanja(t, ocitanja, "bezdan", zadnji.Add(-48*time.Hour), zadnji, stalno(180))
	dodajSatnaOcitanja(t, ocitanja, "bratislava", zadnji.Add(-48*time.Hour), zadnji, stalno(250))
	// Pregledne letve koje kasne: Bogojevo tri sata za satom izdavanja
	// (sat − 2), koliko smije, Bačka Palanka četiri.
	dodajSatnaOcitanja(t, ocitanja, "bogojevo", zadnji.Add(-48*time.Hour), zadnji.Add(-5*time.Hour), stalno(120))
	dodajSatnaOcitanja(t, ocitanja, "backa-palanka", zadnji.Add(-48*time.Hour), zadnji.Add(-6*time.Hour), stalno(90))
	if _, err := ocitanja.Exec(`INSERT INTO stations VALUES ('apatin', 'apatin');
		INSERT INTO readings (station_id, measured_at, level_cm) VALUES ('apatin', 'nije vrijeme', 1)`); err != nil {
		t.Fatal(err)
	}
	baza := praznaBazaPrognoza(t)
	if err := Spremi(baza, []Pojas{pojasIz("donja", "komarom", 3, -1000, 1000), pojasIz("dolje", "kasni", 3, -1000, 1000)}, "proba"); err != nil {
		t.Fatal(err)
	}
	// Model ispuštanja Komároma traži i njegov protok, ali vrh u protoku
	// Komárom nije, pa mu budućnost ne daje.
	konstantniOperater(t, baza, "komarom", 1)
	podmetniPrijenose(t, map[string]Prijenos{"bezdan": {Letva: "bezdan", Izvori: []string{"batina"}, Koef: []float64{-21, 1}, Rasap: 4, Dana: 300}})

	o := &Osvjezivac{Baza: baza, Ocitanja: ocitanja, Najdalje: 6, Model: ModelLanac}
	ishod, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ishod.Sada != sat-2 {
		t.Fatalf("sat izdavanja %d, želim %d (vrh kasni pet sati, smije tri)", ishod.Sada, sat-2)
	}
	if want := map[string]int64{"kasni": sat - 5}; !reflect.DeepEqual(ishod.Ceka, want) {
		t.Errorf("čeka %v, želim %v", ishod.Ceka, want)
	}
	if !strings.HasPrefix(ishod.KoCeka(), "kasni (zadnje ") {
		t.Errorf("tko čeka: %q", ishod.KoCeka())
	}
	if want := (Zamjena{Iz: "komarno", Pomak: -46, Sati: 5, Zajed: 44}).Opis(); ishod.Izbor["zamjena:komarom"].Opis != want {
		t.Errorf("zamjena: %+v, želim %q", ishod.Izbor["zamjena:komarom"], want)
	}
	if iz := ishod.Izbor["preneseno:bezdan"]; iz.Inacica != 1 || iz.Opis != "batina|4.0|300|0" {
		t.Errorf("prenesena: %+v", iz)
	}
	if len(ishod.OperaterVrhovi) != 0 {
		t.Errorf("Komárom nije vrh u protoku, a dobio je budućnost iz modela: %v", ishod.OperaterVrhovi)
	}
	mjerenja := map[string]Izdana{}
	for _, i := range ishod.Izdane {
		if i.Ciljni == ishod.Sada && i.Velicina == "vodostaj" {
			mjerenja[i.Letva] = i
		}
	}
	for letva, v := range map[string]float64{"bratislava": 250, "bezdan": 180, "komarno": 156, "komarom": 110, "kasni": 70, "bogojevo": 120} {
		i, ima := mjerenja[letva]
		if !ima || i.Vrijednost != v || i.Dolje != v || i.Gore != v || i.Model != ModelLanac {
			t.Errorf("%s u satu izdavanja: %+v, želim mjerenje %v", letva, i, v)
		}
	}
	if _, ima := mjerenja["apatin"]; ima {
		t.Error("nečitljiva očitanja Apatina dala su mjerenje")
	}
	if i, ima := mjerenja["backa-palanka"]; ima {
		t.Errorf("Bačka Palanka kasni četiri sata, a dobila je mjerenje: %+v", i)
	}
	// Donja se računa iz Komároma, kojemu je kraj s druge obale: tri sata
	// poslije izdavanja stoji ondje gdje je Komárno, umanjeno za 46 cm.
	if n, _ := izdanihLetve(ishod, "donja"); n != 7 {
		t.Errorf("donja: %d izdanih, želim 7 (do 6 h)", n)
	}
	if d := naDosegu(ishod, "donja", 3); d == nil || d.Vrijednost != 110 {
		t.Errorf("donja za 3 h: %+v, želim 156 − 46", d)
	}
}

// Vrh lanca s našim dnevnim modelom dobiva budućnost iz njega, ne iz tuđe
// prognoze koju također ima; dnevni bez kiše to kaže uz izdanje.
func TestOsvjeziVrhIzDnevnogModela(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	sada := zadnji.Unix() / 3600
	o := probniOsvjezivac(t, probneOcitanja(t, 120, zadnji)) // gornja: 100 cm u zadnjem satu, sat ranije 101 …
	o.Arhiva = arhivaKrivulja(t)
	upisiTuduSrednje(t, o.Baza, sada) // tuđa prognoza, svježa
	if _, err := o.Baza.Exec(`UPDATE tude SET letva = 'gornja', izvor = ?`, Podrijetlo); err != nil {
		t.Fatal(err)
	}
	c := DnevniCilj{Letva: "gornja", Slivovi: []string{"X"}}
	postavi(t, &DnevniCiljevi, []DnevniCilj{c})
	postavi(t, &VrhoviIzDnevnog, map[string]bool{"gornja": true})
	postavi(t, &VrhoviSTudomPrognozom, map[string]string{"gornja": Podrijetlo})
	postavi(t, &sadaUcenja, func() time.Time { return zadnji })
	podmetniDnevne(t, map[string][]*DnevniModel{"gornja": {{Cilj: c}, {Cilj: DnevniCilj{Letva: "gornja"}}}})

	ishod, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ishod.NasiVrhovi, []string{"gornja"}) || len(ishod.TudiVrhovi) != 0 {
		t.Fatalf("naši vrhovi %v, tuđi %v", ishod.NasiVrhovi, ishod.TudiVrhovi)
	}
	if iz := ishod.Izbor["vrh:gornja"]; iz.Inacica != 1 || !strings.Contains(iz.Opis, "iz našeg dnevnog modela") {
		t.Errorf("izbor vrha: %+v", iz)
	}
	if iz := ishod.Izbor["oborina"]; iz.Inacica != 2 || iz.Opis != "dnevni model računa bez kiše: oborine nisu uključene (registar slivova bez kišomjera)" {
		t.Errorf("izbor oborine: %+v", iz)
	}
	// Gornja ide od 100 cm prema srednjaku zadnjeg dana (111,5) u sredini
	// prvog dana; donja je gornja tri sata ranije.
	_, kraj := izdanihLetve(ishod, "donja")
	if want := 100 + 11.5*9/12; kraj == nil || math.Abs(kraj.Vrijednost-want) > 1e-9 {
		t.Errorf("donja za 12 h: %+v, želim %v", kraj, want)
	}
}

// Elektrana na vrhu lanca dobiva budućnost iz modela ispuštanja; model
// elektrane koja nije vrh se ne koristi.
func TestOsvjeziVrhIzModelaIspustanja(t *testing.T) {
	bezIspravka(t)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	ocitanja := praznaOcitanjaRacuna(t)
	for h := 0; h <= 48; h++ {
		kad := zadnji.Add(-time.Duration(h) * time.Hour)
		dodajOcitanje(t, ocitanja, "elektrana", kad, nil, 400.0)
		dodajOcitanje(t, ocitanja, "donja", kad, nil, 400.0)
	}
	baza := praznaBazaPrognoza(t)
	if err := Spremi(baza, []Pojas{{Letva: "donja", Velicina: "protok", Od: 0, Do: 10000, Rasap: 20,
		Ulazi: []Ulaz{{Letva: "elektrana", Velicina: "protok", PomakH: 3, Sirina: 1, Nagib: 1}}}}, "proba"); err != nil {
		t.Fatal(err)
	}
	m := konstantniOperater(t, baza, "elektrana", 500)
	konstantniOperater(t, baza, "donja", 1)
	o := &Osvjezivac{Baza: baza, Ocitanja: ocitanja, Najdalje: 12, Model: ModelLanac}
	ishod, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ishod.OperaterVrhovi, []string{"elektrana"}) {
		t.Fatalf("vrhovi iz modela ispuštanja: %v", ishod.OperaterVrhovi)
	}
	if iz := ishod.Izbor["vrh:elektrana"]; iz.Inacica != 3 || iz.Opis != OpisOperatera(m, 400) {
		t.Errorf("izbor vrha: %+v", iz)
	}
	if _, kraj := izdanihLetve(ishod, "donja"); kraj == nil || kraj.Vrijednost != 500 || kraj.Velicina != "protok" {
		t.Errorf("donja za 12 h: %+v, želim 500 m³/s iz modela elektrane", kraj)
	}
}

// Rezerva se ne ispravlja promašajima glavnog računa, a letva koja u
// nepovezanom pojasu nema ni jedan sat to kaže.
func TestOsvjeziRezervaINepovezana(t *testing.T) {
	bezIspravka(t)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	ocitanja := praznaOcitanjaRacuna(t)
	stalno := func(v int) func(int) int { return func(int) int { return v } }
	dodajSatnaOcitanja(t, ocitanja, "gornja", zadnji.Add(-48*time.Hour), zadnji.Add(-10*time.Hour), stalno(100))
	dodajSatnaOcitanja(t, ocitanja, "pricuvna", zadnji.Add(-48*time.Hour), zadnji, stalno(90))
	dodajSatnaOcitanja(t, ocitanja, "donja", zadnji.Add(-48*time.Hour), zadnji, stalno(95))
	baza := praznaBazaPrognoza(t)
	rezerva := pojasIz("donja", "pricuvna", 3, -1000, 1000)
	rezerva.Inacica, rezerva.Rasap = 1, 4
	nepovezana := pojasIz("nepovezana", "pricuvna", 3, -1000, 1000)
	nepovezana.Nepovezan = true
	if err := Spremi(baza, []Pojas{pojasIz("donja", "gornja", 3, -1000, 1000), rezerva, nepovezana}, "proba"); err != nil {
		t.Fatal(err)
	}
	var promasaji []Promasaj
	for d := 1; d <= 12; d++ {
		promasaji = append(promasaji, Promasaj{Letva: "donja", Velicina: "vodostaj", DosegH: d, Pomak: 50, Rasap: 2, Slucaja: 500})
	}
	if err := SpremiPromasaje(baza, promasaji, "proba"); err != nil {
		t.Fatal(err)
	}
	o := &Osvjezivac{Baza: baza, Ocitanja: ocitanja, Najdalje: 12, Model: ModelLanac}
	ishod, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if iz := ishod.Izbor["donja"]; iz.Inacica != 1 || iz.Opis != "rezerva: pricuvna umjesto gornja" {
		t.Errorf("izbor donje: %+v", iz)
	}
	if d := naDosegu(ishod, "donja", 6); d == nil || d.Vrijednost != 90 || d.Dolje != 86 || d.Gore != 94 {
		t.Errorf("donja za 6 h: %+v, želim 90 ± 4 iz rezerve, bez ispravka", d)
	}
	if err := ishod.BezPrognoze["nepovezana"]; err == nil || err.Error() != "nema ulaza za nijedan sat unaprijed" {
		t.Errorf("nepovezana: %v", err)
	}
}

// Inačica bez ulaza ne može računati, pa se preskače i uzima sljedeća koja
// ima svjež ulaz; opis izbora imenuje ulaze obiju.
func TestOdaberiInacicePreskaceInacicuBezUlaza(t *testing.T) {
	bezUlaza := Pojas{Letva: "donja", Velicina: "vodostaj", Od: -1000, Do: 1000, Inacica: 1}
	treca := pojasIz("donja", "pricuvna", 1, -1000, 1000)
	treca.Inacica = 2
	inacice := map[string][][]Pojas{"donja": {
		{pojasIz("donja", "gornja", 1, -1000, 1000)}, {bezUlaza}, {treca},
	}}
	nizovi := map[Izvor]Niz{
		{Letva: "gornja", Velicina: "vodostaj"}:   ravanNiz(800, 900, 100, 0),
		{Letva: "pricuvna", Velicina: "vodostaj"}: ravanNiz(800, 1000, 90, 0),
	}
	odabrano, izbor := OdaberiInacice(inacice, nizovi, 1000)
	if odabrano["donja"][0].Inacica != 2 || izbor["donja"].Inacica != 2 || izbor["donja"].Opis != "rezerva: pricuvna umjesto gornja" {
		t.Errorf("odabrano %+v, izbor %+v", odabrano["donja"], izbor["donja"])
	}
	if got := imenaUlaza(nil); got != "" {
		t.Errorf("bez pojasa: %q", got)
	}
	dva := []Pojas{{Ulazi: []Ulaz{{Letva: "botovo"}, {Letva: "mursko-sredisce"}}}}
	if got := imenaUlaza(dva); got != "botovo + mursko-sredisce" {
		t.Errorf("dva ulaza: %q", got)
	}
}
