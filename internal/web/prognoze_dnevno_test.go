package web

import (
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"
)

// Dan daje satni lanac dok je on provjerom točniji, a dnevni model od dana
// koji kaže prognoza.DnevnaOdDana; dan se boji fazom obrane, obrubljuje kad
// prag doseže tek gornja granica, a mađarska prognoza stoji uz isti termin.
func TestCelijeDana(t *testing.T) {
	cm := func(v int) *int { return &v }
	st := models.Station{Code: "batina", Name: "Batina",
		Prep: models.Threshold{Cm: cm(300)}, Regular: models.Threshold{Cm: cm(500)}}
	izdano := time.Date(2026, 9, 23, 20, 0, 0, 0, time.UTC) // 22 h po lokalnom
	naslovi, ciljevi := daniPregleda(izdano)
	if naslovi[0] != "čet 24.9." || len(ciljevi) != prognoza.DnevniDosezi {
		t.Fatalf("dani %v", naslovi)
	}
	sat := izdano.Unix() / 3600
	l := PregledLetve{Satno: map[int64]PregledVrijednost{
		ciljevi[0]: {Vrijednost: 280, Dolje: 270, Gore: 310, BoljaOdPostojanosti: true},
		ciljevi[1]: {Vrijednost: 999, Dolje: 999, Gore: 999, BoljaOdPostojanosti: true},
	}}
	var dnevne []prognoza.DnevnaIzdana
	for k := 0; k <= prognoza.DnevniDosezi; k++ {
		v := 250 + 60*float64(k)
		dnevne = append(dnevne, prognoza.DnevnaIzdana{Letva: "batina", Izdano: sat, Dan: k,
			Ciljni: sat + int64(24*k), Vrijednost: v, Dolje: v - 20, Gore: v + 20})
	}
	tude := map[string]map[int64]TudaVrijednost{
		prognoza.Podrijetlo:       {ciljevi[0]: {Cm: 275, PlusMin: 9}},
		prognoza.PodrijetloHidmet: {ciljevi[0]: {Cm: 270}},
	}
	dani := celijeDana("batina", odDanaZa("batina", "", l), l, ciljevi, dnevne, tude, st)

	if d := dani[0]; d.Dnevna || d.Cm != "280" || d.Razina != "" || d.Moguce != "prep" {
		t.Errorf("1. dan %+v: treba satni 280, obrub pripremnog", d)
	}
	if tu := dani[0].Tude; len(tu) != 2 || tu[0].Oznaka != "HU" || tu[0].Cm != "275" || tu[0].Raspon != "±9" ||
		tu[1].Oznaka != "RS" || tu[1].Klasa != "rs" || tu[1].Raspon != "" {
		t.Errorf("tuđe prognoze %+v: treba HU 275 ±9 pa RS 270", tu)
	}
	// Batini dnevni model daje vrijednost od drugog dana, iako satni postoji.
	if d := dani[1]; !d.Dnevna || d.Cm == "999" {
		t.Errorf("2. dan %+v: treba dnevni model", d)
	}
	if d := dani[5]; !d.Dnevna || d.Razina != "regular" {
		t.Errorf("6. dan %+v: treba dnevni, redovna obrana", d)
	}
}

// Pregled se dijeli po vodi. Vrh lanca ide ispred letve kojoj je ulaz
// (Komárom ispred Esztergoma, HE Dubrava i Letenye ispred Botova); letva izvan
// lanca umeće se po riječnom kilometru (Bratislava ispred Komároma, Mursko
// Središće ispred Letenyea), a Borl bez kilometra na početak Drave. Pritoke
// idu voda po voda.
func TestPoVodama(t *testing.T) {
	st := func(kod, rkm string) models.Station { return models.Station{Code: kod, Stationing: rkm} }
	postaje := map[string]models.Station{
		"bratislava": st("bratislava", "rkm 1.872,00"), "komarom": st("komarom", "rkm 1.768,35"),
		"esztergom": st("esztergom", "rkm 1.718,50"), "batina": st("batina", "rkm 1424+850"),
		"letenye": st("letenye", "rkm 35,60"), "mursko-sredisce": st("mursko-sredisce", "rkm 67,70"),
		"botovo": st("botovo", "rkm 226,83"),
	}
	letve := []LetvaPrognoze{
		{Kod: "botovo", Voda: "Drava", Racuna: "protok", Ulazi: []string{"he-dubrava", "letenye"}},
		{Kod: "esztergom", Voda: "Dunav", Racuna: "vodostaj", Ulazi: []string{"komarom"}},
		{Kod: "batina", Voda: "Dunav", Racuna: "vodostaj", Ulazi: []string{"mohacs"}},
		{Kod: "tuhovec", Voda: "Bednja", Racuna: "protok", Ulazi: []string{"zeleznica"}},
		{Kod: "jelengrad", Voda: "Vučica", Racuna: "vodostaj"},
		{Kod: "letenye", Voda: "Mura"}, {Kod: "he-dubrava", Voda: "Drava"},
		{Kod: "komarom", Voda: "Dunav"}, {Kod: "zeleznica", Voda: "Bednja"},
		{Kod: "bratislava", Voda: "Dunav", Pregledna: true},
		{Kod: "mursko-sredisce", Voda: "Mura", Pregledna: true},
		{Kod: "borl-i", Voda: "Drava", Ulaz: true},
	}
	var got []string
	for _, tb := range poVodama(letve, postaje) {
		red := tb.Naslov + ":"
		for _, l := range tb.Letve {
			red += " " + l.Kod
		}
		got = append(got, red)
	}
	want := []string{
		"Dunav: bratislava komarom esztergom batina",
		"Drava i Mura: borl-i he-dubrava mursko-sredisce letenye botovo",
		"Pritoke: zeleznica tuhovec jelengrad",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("redoslijed\n%v\numjesto\n%v", got, want)
	}
}

// Simetričan raspon piše se s ±, kao kod Mađara; onaj koji je prošao kroz
// krivulju protoka nije simetričan i piše se granicama, jer bi ± lagao.
func TestRasponUz(t *testing.T) {
	for _, c := range []struct {
		v, d, g float64
		treba   string
	}{
		{57, 49, 65, "±8"},
		{57, 49, 66, "±9"}, // razlika od centimetra je zaokruživanje
		{41, 22, 60, "±19"},
		{-315, -341, -290, "±26"},
		{43, 8, 76, "8 do 76"}, // Botovo kroz krivulju: 35 ispod, 33 iznad
		{48, 29, 67, "±19"},
		{-364, -370, -330, "-370 do -330"},
		{10, 10, 10, ""},
	} {
		if got := rasponUz(c.v, c.d, c.g); got != c.treba {
			t.Errorf("rasponUz(%g, %g, %g) = %q, a treba %q", c.v, c.d, c.g, got, c.treba)
		}
	}
}

// Redom lanca Mura stoji ispred Varaždina, a Varaždin na kraju. Po toku
// Varaždin i HE Dubrava idu prvi, Mura tik ispred Botova, u koji ulazi, a
// Siga i Petreš na Dunavu između Batine i Aljmaša.
func TestPoTokuMuraPredBotovom(t *testing.T) {
	st := func(kod, rkm string) models.Station { return models.Station{Code: kod, Stationing: rkm} }
	postaje := map[string]models.Station{
		"mursko-sredisce": st("mursko-sredisce", "rkm 67,70"), "gorican": st("gorican", "rkm 35,62"),
		"varazdin": st("varazdin", "rkm 288+185"), "he-dubrava": st("he-dubrava", "rkm 225,05"),
		"botovo": st("botovo", "rkm 226,83"), "novo-virje": st("novo-virje", "rkm 200,60"),
		"batina": st("batina", "rkm 1424+850"), "siga": st("siga", "rkm 1.412,20"),
		"petres": st("petres", "rkm 1.393,00"), "aljmas": st("aljmas", "rkm 1.380,30"),
		"bezdan": st("bezdan", "rkm 1.425,50"),
	}
	letve := []LetvaPrognoze{
		{Kod: "mursko-sredisce", Voda: "Mura", Racuna: "protok"},
		{Kod: "gorican", Voda: "Mura", Racuna: "protok", Ulazi: []string{"mursko-sredisce"}},
		{Kod: "varazdin", Voda: "Drava", Racuna: "vodostaj"},
		{Kod: "botovo", Voda: "Drava", Racuna: "protok", Ulazi: []string{"he-dubrava", "gorican"}},
		{Kod: "novo-virje", Voda: "Drava", Racuna: "protok", Ulazi: []string{"botovo"}},
		{Kod: "he-dubrava", Voda: "Drava"},
		{Kod: "batina", Voda: "Dunav", Racuna: "vodostaj"},
		{Kod: "aljmas", Voda: "Dunav", Racuna: "vodostaj", Ulazi: []string{"batina"}},
		{Kod: "siga", Voda: "Dunav", Racuna: "vodostaj", Ulazi: []string{"batina"}},
		{Kod: "petres", Voda: "Dunav", Racuna: "vodostaj", Ulazi: []string{"siga"}},
		{Kod: "bezdan", Voda: "Dunav", Pregledna: true},
	}
	var got []string
	for _, tb := range poVodama(letve, postaje) {
		red := tb.Naslov + ":"
		for _, l := range tb.Letve {
			red += " " + l.Kod
		}
		got = append(got, red)
	}
	want := "Dunav: bezdan batina siga petres aljmas|Drava i Mura: varazdin he-dubrava mursko-sredisce gorican botovo novo-virje"
	if strings.Join(got, "|") != want {
		t.Errorf("redoslijed\n%v\numjesto\n%v", strings.Join(got, "|"), want)
	}
}

// Kartica prognoze nosi pragove letve kao pločice, redom P, R, I, IS, MAX;
// prag koji letva nema ne dobiva pločicu.
func TestPragoviKartice(t *testing.T) {
	cm := func(x int) *int { return &x }
	st := models.Station{Prep: models.Threshold{Cm: cm(300)}, Regular: models.Threshold{Cm: cm(500)},
		State: models.Threshold{Cm: cm(800)}, Record: models.Threshold{Cm: cm(775)}}
	var got []string
	for _, p := range pragoviKartice(st, &Krajnost{Cm: -160, Godina: 2026}, &Krajnost{Cm: 797, Godina: 1956, Najvisi: true}) {
		got = append(got, p.Oznaka+" "+p.Vrijednost+" "+p.Klasa)
	}
	want := "P +300 cm prep|R +500 cm regular|IS +800 cm crit|MAX +775 cm record|MIN -160 cm min"
	if strings.Join(got, "|") != want {
		t.Errorf("pločice %v, želim %s", got, want)
	}
}

// Letva bez najvišeg vodostaja u registru (Kotoriba) dobiva MAX iz arhive,
// s opisom odakle je; letva s njim zadržava registar.
func TestMaxIzArhiveKadGaRegistarNema(t *testing.T) {
	cm := func(x int) *int { return &x }
	mn, mx := &Krajnost{Cm: -94, Godina: 2022}, &Krajnost{Cm: 512, Godina: 2014, Najvisi: true}
	var got []string
	for _, p := range pragoviKartice(models.Station{Prep: models.Threshold{Cm: cm(300)}}, mn, mx) {
		got = append(got, p.Oznaka+" "+p.Vrijednost+" · "+p.Naslov)
	}
	want := "P +300 cm · pripremna obrana|MAX +512 cm · najviši izmjereni vodostaj (2014.), iz arhive|MIN -94 cm · najniži izmjereni vodostaj (2022.), iz arhive"
	if strings.Join(got, "|") != want {
		t.Errorf("pločice\n%v\nželim\n%s", strings.Join(got, "|"), want)
	}
	if k := (Krajnost{Cm: -35, Godina: 2026, Od: "12. 9. 2026."}); k.Label() != "-35 cm · od 12. 9. 2026." {
		t.Errorf("krajnost iz kratke evidencije: %q", k.Label())
	}
}
